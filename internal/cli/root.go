package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/hl/brr/internal/config"
	"github.com/hl/brr/internal/engine"
	"github.com/hl/brr/internal/fsutil"
	"github.com/hl/brr/internal/notify"
	"github.com/hl/brr/internal/ui"
	"github.com/spf13/cobra"
)

const exitCodeSIGINT = 130 // 128 + SIGINT(2)

const maxPromptFileSize = 10 * 1024 * 1024 // 10 MiB

var rootCmd = &cobra.Command{
	Use:   "brr <prompt> [flags]",
	Short: "Your AI agent, but unhinged",
	Long: `brr runs a prompt in a loop, spinning up a fresh session for each iteration.

Signal Files:
  .brr-complete              The agent creates this when all work is finished.
                             brr stops the loop and removes the file.

  .brr-failed                The agent creates this when it encounters a failure.
                             brr stops and prints the file contents (up to 4 KiB).

  .brr-needs-approval        The agent creates this when it needs a human decision.
                             brr stops and prints the file contents (up to 4 KiB).

  .brr-cycle                 The agent creates this inside a workflow stage when
                             another pass is needed from cycle.target.

  .brr.lock                  Prevents concurrent brr instances in the same directory.
                             Acquired on start, released on exit. Stays on disk.

  .brr/state/workflows/      Tracks workflow progress and event history for
                             resume, status, and debugging. Written by
                             'brr workflow run'. State is deleted on completion.
                             Use --reset to discard and start fresh.`,
	Args:         cobra.ExactArgs(1),
	RunE:         run,
	SilenceUsage: true,
}

func init() {
	rootCmd.Flags().IntP("max", "m", 0, "max iterations (0 = unlimited)")
	rootCmd.Flags().StringP("profile", "p", "", "agent profile from .brr.yaml (uses 'default' if omitted)")
	rootCmd.Flags().BoolP("notify", "n", false, "send a desktop notification when the loop stops")

	defaultHelp := rootCmd.HelpFunc()
	rootCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		if !cmd.HasParent() {
			printBanner()
			fmt.Fprintf(os.Stderr, "  %shttps://github.com/hl/brr%s\n\n", ui.Dim, ui.Reset)
		}
		defaultHelp(cmd, args)
	})
}

// SetVersion configures the version string shown by --version.
func SetVersion(version, commit string) {
	rootCmd.Version = fmt.Sprintf("%s (%s)", version, commit)
}

func run(cmd *cobra.Command, args []string) error {
	max, err := cmd.Flags().GetInt("max")
	if err != nil {
		return fmt.Errorf("reading --max flag: %w", err)
	}
	if max < 0 {
		return fmt.Errorf("--max must be >= 0, got %d", max)
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	profileName, err := cmd.Flags().GetString("profile")
	if err != nil {
		return fmt.Errorf("reading --profile flag: %w", err)
	}
	command, resolvedName, err := cfg.ResolveProfile(profileName)
	if err != nil {
		return err
	}

	promptText, err := resolvePrompt(args[0])
	if err != nil {
		return err
	}

	doNotify, err := cmd.Flags().GetBool("notify")
	if err != nil {
		return fmt.Errorf("reading --notify flag: %w", err)
	}

	printBanner()
	printConfig(args[0], resolvedName, command, max)

	result, runErr := engine.Run(engine.Options{
		Prompt:  promptText,
		Max:     max,
		Command: command,
	})

	if result != nil && result.Reason == engine.ReasonCycle {
		return fmt.Errorf(".brr-cycle is only supported by 'brr workflow'")
	}

	// Send notification (best-effort — failure is logged but does not affect exit code)
	if doNotify && result != nil && result.Reason != engine.ReasonInterrupted {
		if nErr := notify.Send(result); nErr != nil {
			fmt.Fprintf(os.Stderr, "warning: notification failed: %v\n", nErr)
		}
	}

	if errors.Is(runErr, engine.ErrInterrupted) {
		cmd.SilenceErrors = true
	}
	return runErr
}

// resolvePrompt reads a prompt from a file path, .brr/prompts/<name>.md, or
// returns it as inline text. The final resolved prompt must be non-empty after
// trimming (prompt-resolution.md requirement 10); an empty result errors and
// names the source it came from.
func resolvePrompt(nameOrPath string) (string, error) {
	return resolvePromptRestricted(nameOrPath, false)
}

// resolveWorkflowPrompt resolves a prompt referenced by a workflow stage. A
// cloned repo's .brr/workflows/*.yaml is untrusted, so a workflow prompt must
// not become an arbitrary file-read primitive (even `brr workflow validate`
// reads it). Only named prompts and relative paths inside the working tree are
// allowed; absolute paths and ".." traversal are rejected. Inline prompt text
// still passes through.
func resolveWorkflowPrompt(nameOrPath string) (string, error) {
	return resolvePromptRestricted(nameOrPath, true)
}

func resolvePromptRestricted(nameOrPath string, restrict bool) (string, error) {
	text, source, err := resolvePromptText(nameOrPath, restrict)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("prompt is empty: %s", source)
	}
	return text, nil
}

// resolvePromptText resolves nameOrPath to prompt text and a human-readable
// description of where it came from, without checking emptiness. When restrict
// is set, path-like arguments that are absolute or escape the working tree via
// ".." are rejected before any filesystem access (inline text is unaffected).
func resolvePromptText(nameOrPath string, restrict bool) (text, source string, err error) {
	if restrict && looksLikeFilePath(nameOrPath) && (filepath.IsAbs(nameOrPath) || hasDotDotSegment(nameOrPath)) {
		return "", "", fmt.Errorf("workflow prompt %q must be a named prompt or a relative path inside the working tree (no absolute paths or \"..\")", nameOrPath)
	}
	// If it's an existing regular file, read it directly (rejects symlinks, FIFOs, etc.)
	if fi, statErr := os.Lstat(nameOrPath); statErr == nil {
		if fi.IsDir() {
			// Don't treat directories as prompt files — fall through to named prompt lookup
		} else if text, err := readPromptFile(nameOrPath); err == nil {
			return text, fmt.Sprintf("prompt file %s", nameOrPath), nil
		} else {
			return "", "", fmt.Errorf("reading prompt file %s: %w", nameOrPath, err)
		}
	} else if looksLikeFilePath(nameOrPath) {
		// It looks like a file path — distinguish "not found" from other stat errors
		if os.IsNotExist(statErr) {
			return "", "", fmt.Errorf("prompt file not found: %s", nameOrPath)
		}
		return "", "", fmt.Errorf("accessing prompt file %s: %w", nameOrPath, statErr)
	}

	// For bare names (no spaces), try named prompt resolution
	if !strings.Contains(nameOrPath, " ") {
		name := strings.TrimSuffix(nameOrPath, ".md")

		// Reject path traversal attempts
		if strings.Contains(name, "..") {
			return "", "", fmt.Errorf("invalid prompt name: %q", name)
		}

		// Try .brr/prompts/<name>.md
		projectPath := filepath.Join(".brr", "prompts", name+".md")
		if text, err := readPromptFile(projectPath); err == nil {
			return text, fmt.Sprintf("prompt %s", projectPath), nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", "", fmt.Errorf("reading %s: %w", projectPath, err)
		}

		// Try user config dir prompts/<name>.md
		if configDir, err := os.UserConfigDir(); err == nil {
			userPath := filepath.Join(configDir, "brr", "prompts", name+".md")
			if text, err := readPromptFile(userPath); err == nil {
				return text, fmt.Sprintf("prompt %s", userPath), nil
			} else if !errors.Is(err, os.ErrNotExist) {
				return "", "", fmt.Errorf("reading %s: %w", userPath, err)
			}
		}
	}

	// Treat as inline prompt text
	return nameOrPath, "inline prompt text", nil
}

func readPromptFile(path string) (string, error) {
	f, err := fsutil.OpenRegularFile(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	data, err := io.ReadAll(io.LimitReader(f, maxPromptFileSize+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxPromptFileSize {
		return "", fmt.Errorf("prompt file is too large (max %d bytes)", maxPromptFileSize)
	}
	return string(data), nil
}

// looksLikeFilePath returns true if s looks like a file path rather than inline prompt text.
func looksLikeFilePath(s string) bool {
	hasSep := strings.ContainsRune(s, filepath.Separator) || strings.ContainsRune(s, '/')
	hasPromptExt := isPromptExtension(filepath.Ext(s))

	if strings.Contains(s, " ") {
		// With spaces: only a file path if it has BOTH a separator and a recognized extension
		// e.g. "docs/Build Plan.md" → file path; "Fix stuff in src/" → inline text
		return hasSep && hasPromptExt
	}
	// Without spaces: separator alone or recognized extension alone → file path
	return hasSep || hasPromptExt
}

// hasDotDotSegment reports whether p contains a ".." path segment (as opposed to
// ".." appearing inside a filename or inline text like "wait...").
func hasDotDotSegment(p string) bool {
	for _, seg := range strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == filepath.Separator }) {
		if seg == ".." {
			return true
		}
	}
	return false
}

func isPromptExtension(ext string) bool {
	switch ext {
	case ".md", ".txt", ".prompt":
		return true
	}
	return false
}

func printBanner() {
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "  %s%s██████╗ ██████╗ ██████╗%s\n", ui.Bold, ui.Cyan, ui.Reset)
	fmt.Fprintf(os.Stderr, "  %s%s██╔══██╗██╔══██╗██╔══██╗%s\n", ui.Bold, ui.Blue, ui.Reset)
	fmt.Fprintf(os.Stderr, "  %s%s██████╔╝██████╔╝██████╔╝%s\n", ui.Bold, ui.Magenta, ui.Reset)
	fmt.Fprintf(os.Stderr, "  %s%s██╔══██╗██╔══██╗██╔══██╗%s\n", ui.Bold, ui.Red, ui.Reset)
	fmt.Fprintf(os.Stderr, "  %s%s██████╔╝██║  ██║██║  ██║%s\n", ui.Bold, ui.Yellow, ui.Reset)
	fmt.Fprintf(os.Stderr, "  %s%s╚═════╝ ╚═╝  ╚═╝╚═╝  ╚═╝%s\n", ui.Bold, ui.Green, ui.Reset)
	fmt.Fprintf(os.Stderr, "  %syour AI agent, but unhinged%s\n", ui.Dim, ui.Reset)
	fmt.Fprintln(os.Stderr)
}

func printConfig(promptName string, profileName string, command []string, max int) {
	maxLabel := "unlimited"
	if max > 0 {
		maxLabel = fmt.Sprintf("%d", max)
	}
	fmt.Fprintf(os.Stderr, "  %sprompt:%s  %s\n", ui.Dim, ui.Reset, promptName)
	fmt.Fprintf(os.Stderr, "  %sprofile:%s %s %s(%s)%s\n", ui.Dim, ui.Reset, profileName, ui.Dim, command[0], ui.Reset)
	fmt.Fprintf(os.Stderr, "  %smax:%s     %s\n", ui.Dim, ui.Reset, maxLabel)
	fmt.Fprintln(os.Stderr)
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		if errors.Is(err, engine.ErrInterrupted) {
			os.Exit(exitCodeSIGINT)
		}
		os.Exit(1)
	}
}
