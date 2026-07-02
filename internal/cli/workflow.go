package cli

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/hl/brr/internal/config"
	"github.com/hl/brr/internal/engine"
	"github.com/hl/brr/internal/ui"
	"github.com/hl/brr/internal/workflow"
	"github.com/spf13/cobra"
)

var workflowCmd = &cobra.Command{
	Use:   "workflow",
	Short: "Manage YAML workflows",
	Long: `Manage versioned workflows defined in .brr/workflows/<name>.yaml.

Workflows are YAML orchestration files with explicit stage IDs and stage
types. Agent stages run brr's loop engine with a prompt. Command stages run a
deterministic argv command as a gate. Workflow state is saved under
.brr/state/workflows/ for resume, status, and debugging.`,
}

var workflowRunCmd = &cobra.Command{
	Use:          "run <name>",
	Short:        "Run a multi-stage workflow",
	Args:         cobra.ExactArgs(1),
	RunE:         runWorkflow,
	SilenceUsage: true,
}

var workflowValidateCmd = &cobra.Command{
	Use:          "validate <name>",
	Short:        "Validate a workflow without running it",
	Args:         cobra.ExactArgs(1),
	RunE:         validateWorkflow,
	SilenceUsage: true,
}

var workflowStatusCmd = &cobra.Command{
	Use:          "status [name]",
	Short:        "Show saved workflow state",
	Args:         cobra.MaximumNArgs(1),
	RunE:         statusWorkflow,
	SilenceUsage: true,
}

var workflowInitCmd = &cobra.Command{
	Use:          "init <name>",
	Short:        "Create a workflow from a template",
	Args:         cobra.ExactArgs(1),
	RunE:         initWorkflow,
	SilenceUsage: true,
}

var workflowResetCmd = &cobra.Command{
	Use:          "reset <name>",
	Short:        "Discard saved state and event log for a workflow",
	Args:         cobra.ExactArgs(1),
	RunE:         resetWorkflow,
	SilenceUsage: true,
}

func init() {
	workflowRunCmd.Flags().StringP("profile", "p", "", "default profile for agent stages")
	workflowRunCmd.Flags().BoolP("notify", "n", false, "send a desktop notification when the workflow completes")
	workflowRunCmd.Flags().Bool("reset", false, "discard saved progress and start from the first stage")

	workflowValidateCmd.Flags().StringP("profile", "p", "", "default profile for agent stages")
	workflowStatusCmd.Flags().BoolP("watch", "w", false, "redraw saved workflow state until it is cleared")
	workflowStatusCmd.Flags().Duration("interval", time.Second, "refresh interval for --watch")
	workflowInitCmd.Flags().String("template", "ship", "workflow template to copy")

	workflowCmd.AddCommand(workflowRunCmd)
	workflowCmd.AddCommand(workflowValidateCmd)
	workflowCmd.AddCommand(workflowStatusCmd)
	workflowCmd.AddCommand(workflowInitCmd)
	workflowCmd.AddCommand(workflowResetCmd)
	rootCmd.AddCommand(workflowCmd)
}

func runWorkflow(cmd *cobra.Command, args []string) error {
	name := args[0]
	doNotify, err := cmd.Flags().GetBool("notify")
	if err != nil {
		return fmt.Errorf("reading --notify flag: %w", err)
	}
	returnWorkflowError := func(err error) error {
		if doNotify {
			if nErr := notifyWorkflowError(err); nErr != nil {
				fmt.Fprintf(os.Stderr, "warning: notification failed: %v\n", nErr)
			}
		}
		return err
	}

	wf, cfg, profileFlag, err := loadWorkflowForRun(cmd, name, true)
	if err != nil {
		return returnWorkflowError(err)
	}
	reset, err := cmd.Flags().GetBool("reset")
	if err != nil {
		return returnWorkflowError(fmt.Errorf("reading --reset flag: %w", err))
	}

	printBanner()
	lf, err := engine.AcquireLock()
	if err != nil {
		return returnWorkflowError(err)
	}
	defer engine.ReleaseLock(lf)

	var notifyFn func()
	if doNotify {
		notifyFn = func() {
			result := &engine.Result{Reason: engine.ReasonComplete}
			if nErr := notifySend(result); nErr != nil {
				fmt.Fprintf(os.Stderr, "warning: notification failed: %v\n", nErr)
			}
		}
	}

	result, runErr := workflow.Run(workflow.Options{
		Name:          name,
		Workflow:      wf,
		Config:        cfg,
		ProfileFlag:   profileFlag,
		ResolvePrompt: resolveWorkflowPrompt,
		Notify:        notifyFn,
		Reset:         reset,
	})

	if runErr != nil {
		interrupted := result != nil && result.Reason == engine.ReasonInterrupted
		if interrupted {
			cmd.SilenceErrors = true
		}
		// Notify with the actual error, which names the terminal event (e.g.
		// "stage build: exit status 1" or the cycle-without-config error), rather
		// than the stage's stop reason — which misreports a single command failure
		// as "Too many consecutive failures" and a cycle error as if the run were
		// continuing. Interrupts never notify (workflow.md req 30).
		if doNotify && !interrupted {
			if nErr := notifyWorkflowError(runErr); nErr != nil {
				fmt.Fprintf(os.Stderr, "warning: notification failed: %v\n", nErr)
			}
		}
	} else if doNotify && result != nil && result.Reason == engine.ReasonFailed {
		if nErr := notifySend(result); nErr != nil {
			fmt.Fprintf(os.Stderr, "warning: notification failed: %v\n", nErr)
		}
	}

	if errors.Is(runErr, engine.ErrInterrupted) {
		return engine.ErrInterrupted
	}
	return runErr
}

func validateWorkflow(cmd *cobra.Command, args []string) error {
	name := args[0]
	if _, _, _, err := loadWorkflowForRun(cmd, name, true); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "  %s%sWorkflow %q is valid%s\n", ui.Bold, ui.Green, name, ui.Reset)
	return nil
}

func statusWorkflow(cmd *cobra.Command, args []string) error {
	name := ""
	if len(args) == 1 {
		name = args[0]
	}
	watch, err := cmd.Flags().GetBool("watch")
	if err != nil {
		return fmt.Errorf("reading --watch flag: %w", err)
	}
	if watch {
		interval, err := cmd.Flags().GetDuration("interval")
		if err != nil {
			return fmt.Errorf("reading --interval flag: %w", err)
		}
		return workflow.WatchStatus(name, os.Stderr, interval)
	}
	return workflow.Status(name, os.Stderr)
}

func resetWorkflow(cmd *cobra.Command, args []string) error {
	name := args[0]
	removed, err := workflow.Reset(name)
	if err != nil {
		return err
	}
	if removed {
		fmt.Fprintf(os.Stderr, "  %s%sCleared%s saved state for workflow %q\n", ui.Bold, ui.Green, ui.Reset, name)
	} else {
		fmt.Fprintf(os.Stderr, "  %sno saved state for workflow %q%s\n", ui.Dim, name, ui.Reset)
	}
	return nil
}

func initWorkflow(cmd *cobra.Command, args []string) error {
	template, err := cmd.Flags().GetString("template")
	if err != nil {
		return fmt.Errorf("reading --template flag: %w", err)
	}
	if err := workflow.InitTemplate(args[0], template); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "  Created .brr/workflows/%s.yaml from %s template\n", args[0], template)
	return nil
}

func loadWorkflowForRun(cmd *cobra.Command, name string, resolvePrompts bool) (workflow.Workflow, config.Config, string, error) {
	data, err := workflow.Resolve(name)
	if err != nil {
		return workflow.Workflow{}, config.Config{}, "", err
	}
	wf, err := workflow.Load(data)
	if err != nil {
		return workflow.Workflow{}, config.Config{}, "", err
	}
	cfg, err := config.Load()
	if err != nil {
		return workflow.Workflow{}, config.Config{}, "", fmt.Errorf("loading config: %w", err)
	}
	profileFlag, err := cmd.Flags().GetString("profile")
	if err != nil {
		return workflow.Workflow{}, config.Config{}, "", fmt.Errorf("reading --profile flag: %w", err)
	}
	var resolver func(string) (string, error)
	if resolvePrompts {
		resolver = resolveWorkflowPrompt
	}
	if err := workflow.ValidateRuntime(wf, cfg, profileFlag, resolver); err != nil {
		return workflow.Workflow{}, config.Config{}, "", err
	}
	return wf, cfg, profileFlag, nil
}
