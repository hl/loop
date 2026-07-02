package workflow

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sync/atomic"
	"time"

	"github.com/hl/brr/internal/engine"
	"github.com/hl/brr/internal/ui"
)

// Run executes the workflow. The caller must hold the exclusive lock.
func Run(opts Options) (*engine.Result, error) {
	if err := validateName(opts.Name); err != nil {
		return nil, err
	}
	if err := ValidateRuntime(opts.Workflow, opts.Config, opts.ProfileFlag, nil); err != nil {
		return nil, err
	}

	// Scrub signal files left over from a previous run (e.g. after kill -9 or
	// power loss, where engine.Run's deferred cleanup never ran). Command stages
	// only inspect signal files after running and agent stages' engine.Run honors
	// pre-existing signals — so a stale file would decide a stage's outcome before
	// it even executes. Only regular files are removed (cleanupSignalFiles guards).
	cleanupSignalFiles()

	store := store{name: opts.Name}
	if opts.Reset {
		store.delete()
	}
	stageIdx, state, resumed := initialRunState(opts, store)

	printWorkflowSummary(opts.Workflow, opts.Name)
	startEvent := Event{RunID: state.RunID, Workflow: opts.Name, Time: time.Now().UTC(), Type: "workflow_started"}
	if resumed {
		// A resume reuses the original RunID; emitting workflow_started again would
		// make the event log show one run "started" N times. Record it as a distinct
		// workflow_resumed carrying the stage the run picks up from.
		startEvent.Type = "workflow_resumed"
		startEvent.StageID = state.NextStageID
	}
	store.appendEvent(startEvent)
	store.save(state)
	printRunDiagram(opts.Workflow, state, "")

	for stageIdx < len(opts.Workflow.Stages) {
		stage := opts.Workflow.Stages[stageIdx]
		result, err := runStage(opts, state, store, stage, stageIdx)
		if err != nil {
			if result != nil && result.Reason == engine.ReasonInterrupted {
				return result, err
			}
			store.appendEvent(Event{RunID: state.RunID, Workflow: opts.Name, Time: time.Now().UTC(), Type: "workflow_error", StageID: stage.ID, Reason: stopReason(result), Message: err.Error()})
			return result, err
		}

		nextIdx, stop, err := handleStageResult(opts, state, store, stage, result)
		if stop || err != nil {
			return result, err
		}
		if nextIdx >= 0 {
			stageIdx = nextIdx
			continue
		}

		stageIdx++
		saveNextStage(opts.Workflow, state, store, stageIdx)
	}

	store.appendEvent(Event{RunID: state.RunID, Workflow: opts.Name, Time: time.Now().UTC(), Type: "workflow_complete"})
	store.deleteAll()
	fmt.Fprintf(os.Stderr, "\n  %s%sWorkflow complete%s\n", ui.Bold, ui.Green, ui.Reset)
	if opts.Notify != nil {
		opts.Notify()
	}
	return &engine.Result{Reason: engine.ReasonComplete}, nil
}

func initialRunState(opts Options, store store) (int, *State, bool) {
	now := time.Now().UTC()
	state := &State{
		SchemaVersion: SchemaVersion,
		Workflow:      opts.Name,
		RunID:         newRunID(),
		StartedAt:     now,
		UpdatedAt:     now,
		StartSHA:      gitHEAD(),
		NextStageID:   opts.Workflow.Stages[0].ID,
		Stages:        initialStageStatus(opts.Workflow),
	}

	if !opts.Reset {
		if saved, err := store.load(); err == nil && validResumeState(saved, opts.Workflow, opts.Name) {
			fmt.Fprintf(os.Stderr, "  %sresuming:%s stage %s", ui.Dim, ui.Reset, saved.NextStageID)
			if saved.CycleCount > 0 {
				fmt.Fprintf(os.Stderr, " (cycle %d)", saved.CycleCount)
			}
			fmt.Fprintln(os.Stderr)
			return stageIndexByID(opts.Workflow, saved.NextStageID), saved, true
		}
	}
	detail := "no saved state"
	if opts.Reset {
		detail = "discarded saved state"
	}
	fmt.Fprintf(os.Stderr, "  %sstarting fresh:%s %s\n", ui.Dim, ui.Reset, detail)
	return 0, state, false
}

func handleStageResult(opts Options, state *State, store store, stage Stage, result *engine.Result) (int, bool, error) {
	switch result.Reason {
	case engine.ReasonFailed:
		fmt.Fprintf(os.Stderr, "\n  %s%sWorkflow stopped — stage %q failed%s\n", ui.Bold, ui.Red, stage.ID, ui.Reset)
		fmt.Fprintf(os.Stderr, "  %sRe-run to resume.%s\n", ui.Dim, ui.Reset)
		return -1, true, nil
	case engine.ReasonApproval:
		fmt.Fprintf(os.Stderr, "\n  %s%sWorkflow paused — stage %q needs approval%s\n", ui.Bold, ui.Yellow, stage.ID, ui.Reset)
		fmt.Fprintf(os.Stderr, "  %sResolve the issue and re-run to resume.%s\n", ui.Dim, ui.Reset)
		return -1, true, nil
	case engine.ReasonCycle:
		return handleCycle(opts, state, store, stage, result)
	default:
		return -1, false, nil
	}
}

func handleCycle(opts Options, state *State, store store, stage Stage, result *engine.Result) (int, bool, error) {
	if opts.Workflow.Cycle == nil {
		err := errors.New("requested a workflow cycle, but no cycle target is configured")
		store.appendEvent(Event{RunID: state.RunID, Workflow: opts.Name, Time: time.Now().UTC(), Type: "workflow_error", StageID: stage.ID, Reason: "cycle", Message: err.Error()})
		return -1, false, fmt.Errorf("stage %s: %w", stage.ID, err)
	}
	if state.CycleCount >= opts.Workflow.Cycle.Max {
		state.UpdatedAt = time.Now().UTC()
		store.save(state)
		msg := fmt.Sprintf("cycle.max %d reached; advancing past %s", opts.Workflow.Cycle.Max, stage.ID)
		store.appendEvent(Event{RunID: state.RunID, Workflow: opts.Name, Time: state.UpdatedAt, Type: "cycle_skipped", StageID: stage.ID, Reason: "cycle_max_reached", Message: msg})
		fmt.Fprintf(os.Stderr, "\n  %s%sCycle limit reached%s %s— stage %q requested another cycle, but cycle.max %d was already used. Advancing without looping back.%s\n",
			ui.Bold, ui.Yellow, ui.Reset,
			ui.Dim, stage.ID, opts.Workflow.Cycle.Max, ui.Reset,
		)
		return -1, false, nil
	}
	state.CycleCount++
	nextIdx := stageIndexByID(opts.Workflow, opts.Workflow.Cycle.Target)
	state.NextStageID = opts.Workflow.Stages[nextIdx].ID
	state.UpdatedAt = time.Now().UTC()
	store.save(state)
	store.appendEvent(Event{RunID: state.RunID, Workflow: opts.Name, Time: state.UpdatedAt, Type: "cycle", StageID: stage.ID, Message: "restarting from " + state.NextStageID})
	fmt.Fprintf(os.Stderr, "\n%s━━━%s %s%sCycle %d/%d%s %s▸ restarting from %s ━━━%s\n",
		ui.Dim, ui.Reset,
		ui.Bold, ui.Magenta, state.CycleCount, opts.Workflow.Cycle.Max, ui.Reset,
		ui.Dim, state.NextStageID, ui.Reset,
	)
	printRunDiagram(opts.Workflow, state, "")
	return nextIdx, false, nil
}

func runStage(opts Options, state *State, store store, stage Stage, idx int) (*engine.Result, error) {
	printStageHeader(idx+1, len(opts.Workflow.Stages), stage, state.CycleCount, opts.Workflow)
	start := time.Now()
	updateStageStatus(state, stage, "running", "", 0)
	state.NextStageID = stage.ID
	state.UpdatedAt = time.Now().UTC()
	store.save(state)
	store.appendEvent(Event{RunID: state.RunID, Workflow: opts.Name, Time: state.UpdatedAt, Type: "stage_started", StageID: stage.ID})
	printRunDiagram(opts.Workflow, state, statusSpinnerFrames[idx%len(statusSpinnerFrames)])

	result, err := executeStage(opts, stage)
	duration := time.Since(start)
	updateStageStatus(state, stage, stageStatusFromResult(result, err), stopReason(result), duration)
	state.UpdatedAt = time.Now().UTC()
	store.save(state)
	store.appendEvent(Event{RunID: state.RunID, Workflow: opts.Name, Time: state.UpdatedAt, Type: "stage_finished", StageID: stage.ID, Reason: stopReason(result)})
	printRunDiagram(opts.Workflow, state, "")

	if err != nil {
		return result, fmt.Errorf("stage %s: %w", stage.ID, err)
	}
	return result, nil
}

func executeStage(opts Options, stage Stage) (*engine.Result, error) {
	switch stage.Type {
	case StageTypeAgent:
		return runAgentStage(opts, stage)
	case StageTypeCommand:
		return runCommandStage(stage)
	default:
		return nil, fmt.Errorf("unknown stage type %q", stage.Type)
	}
}

func runAgentStage(opts Options, stage Stage) (*engine.Result, error) {
	promptText, err := opts.ResolvePrompt(stage.Prompt)
	if err != nil {
		return nil, err
	}
	command, _, err := resolveStageProfile(stage, opts.Workflow, opts.ProfileFlag, opts.Config)
	if err != nil {
		return nil, err
	}
	return engine.Run(engine.Options{
		Prompt:   promptText,
		Max:      effectiveMax(opts.Workflow, stage),
		Command:  command,
		SkipLock: true,
	})
}

func runCommandStage(stage Stage) (*engine.Result, error) {
	cmd := exec.Command(stage.Command[0], stage.Command[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Start(); err != nil {
		if sig := detectSignalFiles(); sig != nil {
			cleanupSignalFiles()
			return sig, nil
		}
		return &engine.Result{Reason: engine.ReasonCommandFailed}, err
	}

	var interrupted atomic.Bool
	sigCh := make(chan os.Signal, 2)
	done := make(chan struct{})
	signal.Notify(sigCh, commandStageSignals()...)
	defer signal.Stop(sigCh)
	go func() {
		for {
			select {
			case <-done:
				return
			case sig := <-sigCh:
				interrupted.Store(true)
				// The command-stage child shares brr's foreground process group
				// (no setProcAttr), so the terminal already delivered Ctrl+C
				// (SIGINT) to it directly. Re-sending SIGINT would be a double
				// interrupt that hard-aborts tools with escalating interrupt
				// semantics (pytest, npm, coding agents). Only forward signals
				// that are NOT tty-broadcast to the group, i.e. SIGTERM.
				if sig != os.Interrupt && cmd.Process != nil {
					_ = cmd.Process.Signal(sig)
				}
			}
		}
	}()

	err := cmd.Wait()
	close(done)

	// close(done) races with a signal still buffered in sigCh: the goroutine may
	// exit via <-done without recording it. The tty delivers Ctrl+C to the shared
	// group, so a fast-dying child can return from Wait before the forwarder runs.
	// Drain the channel, and also classify a child that died from an interrupt
	// signal as an interrupt — otherwise the same keypress is intermittently
	// reported as a stage failure instead of an interrupt.
	drainSignals(sigCh, &interrupted)
	if !interrupted.Load() && exitedFromInterrupt(err) {
		interrupted.Store(true)
	}

	// Interrupt wins over any signal file the stage wrote on its way out (e.g. a
	// .brr-cycle): the user asked to stop, so stop with exit 130 and preserved
	// state (workflow.md item 29) rather than acting on the transient signal.
	if interrupted.Load() {
		cleanupSignalFiles()
		return &engine.Result{Reason: engine.ReasonInterrupted}, engine.ErrInterrupted
	}
	if sig := detectSignalFiles(); sig != nil {
		cleanupSignalFiles()
		return sig, nil
	}
	if err != nil {
		return &engine.Result{Reason: engine.ReasonCommandFailed}, err
	}
	return &engine.Result{Reason: engine.ReasonComplete}, nil
}

// drainSignals non-blockingly consumes any signals still buffered in ch,
// recording that an interrupt occurred for each one.
func drainSignals(ch <-chan os.Signal, interrupted *atomic.Bool) {
	for {
		select {
		case <-ch:
			interrupted.Store(true)
		default:
			return
		}
	}
}

func saveNextStage(wf Workflow, state *State, store store, stageIdx int) {
	if stageIdx < len(wf.Stages) {
		state.NextStageID = wf.Stages[stageIdx].ID
	} else {
		state.NextStageID = ""
	}
	state.UpdatedAt = time.Now().UTC()
	store.save(state)
}
