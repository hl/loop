package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/hl/brr/internal/engine"
)

func TestRunSequentialAgentAndCommandStages(t *testing.T) {
	t.Chdir(t.TempDir())
	logFile := filepath.Join(".", "stage-log")
	agentCmd := catToFileCmd(logFile)
	commandCmd := appendTextCmd(logFile, "check\n")

	wf := testWorkflow([]Stage{
		{ID: "first", Type: StageTypeAgent, Prompt: "first", Max: 1},
		{ID: "check", Type: StageTypeCommand, Command: commandCmd},
		{ID: "second", Type: StageTypeAgent, Prompt: "second", Max: 1},
	}, nil)

	result, err := Run(Options{
		Name:     "ship",
		Workflow: wf,
		Config:   testConfig(agentCmd),
		ResolvePrompt: func(name string) (string, error) {
			return name + "\n", nil
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil || result.Reason != engine.ReasonComplete {
		t.Fatalf("expected complete result, got %#v", result)
	}
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	i1 := strings.Index(content, "first")
	i2 := strings.Index(content, "check")
	i3 := strings.Index(content, "second")
	if i1 < 0 || i2 < 0 || i3 < 0 || i1 >= i2 || i2 >= i3 {
		t.Fatalf("stages did not run in order: %q", content)
	}
	if _, err := os.Stat(filepath.Join(StateDir, "ship.json")); !os.IsNotExist(err) {
		t.Fatalf("expected state file to be deleted on completion, got: %v", err)
	}
	if _, err := os.Stat(filepath.Join(StateDir, "ship.events.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("expected event log to be deleted on completion, got: %v", err)
	}
}

func TestRunCommandStageFailurePreservesState(t *testing.T) {
	t.Chdir(t.TempDir())
	wf := testWorkflow([]Stage{{ID: "check", Type: StageTypeCommand, Command: failCmd()}}, nil)

	result, err := Run(Options{
		Name:     "ship",
		Workflow: wf,
		Config:   testConfig(echoCmd()),
		ResolvePrompt: func(name string) (string, error) {
			return name, nil
		},
	})
	if err == nil {
		t.Fatal("expected command stage failure")
	}
	if result == nil || result.Reason != engine.ReasonCommandFailed {
		t.Fatalf("expected command_failed result, got %#v", result)
	}
	state := readState(t, "ship")
	if state.NextStageID != "check" {
		t.Fatalf("expected resume at failed stage, got %q", state.NextStageID)
	}
	if state.Stages[0].Status != "error" {
		t.Fatalf("expected stage status error, got %q", state.Stages[0].Status)
	}
	// A single deterministic gate failure must not be mislabeled as a fail streak.
	if state.Stages[0].Reason != "command_failed" {
		t.Fatalf("expected reason command_failed, got %q", state.Stages[0].Reason)
	}
}

func TestRunCycleFromCommandStage(t *testing.T) {
	t.Chdir(t.TempDir())
	counter := filepath.Join(".", "counter")
	wf := testWorkflow([]Stage{
		{ID: "build", Type: StageTypeCommand, Command: appendTextCmd(counter, "x\n")},
		{ID: "review", Type: StageTypeCommand, Command: cycleOnceCmd(counter)},
	}, &Cycle{Target: "build", Max: 2})

	_, err := Run(Options{
		Name:     "ship",
		Workflow: wf,
		Config:   testConfig(echoCmd()),
		ResolvePrompt: func(name string) (string, error) {
			return name, nil
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(counter)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected build to run twice, got %d (%q)", len(lines), data)
	}
}

func TestRunCycleLimitAdvancesPastStage(t *testing.T) {
	t.Chdir(t.TempDir())
	counter := filepath.Join(".", "counter")
	wf := testWorkflow([]Stage{
		{ID: "build", Type: StageTypeCommand, Command: alwaysCycleAndCountCmd(counter)},
		{ID: "review", Type: StageTypeCommand, Command: appendTextCmd(counter, "review\n")},
	}, &Cycle{Target: "build", Max: 1})

	result, err := Run(Options{
		Name:     "ship",
		Workflow: wf,
		Config:   testConfig(echoCmd()),
		ResolvePrompt: func(name string) (string, error) {
			return name, nil
		},
	})
	if err != nil {
		t.Fatalf("expected workflow to complete despite cycle.max, got error: %v", err)
	}
	if result == nil || result.Reason != engine.ReasonComplete {
		t.Fatalf("expected complete result, got %#v", result)
	}
	data, err := os.ReadFile(counter)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(string(data))
	// max=1 → build runs initially, cycles back once, then on its 2nd cycle
	// request the limit is reached and the workflow advances to review.
	want := "x\nx\nreview"
	if got != want {
		t.Fatalf("expected counter %q, got %q", want, got)
	}
	if _, err := os.Stat(filepath.Join(StateDir, "ship.json")); !os.IsNotExist(err) {
		t.Fatalf("expected state file to be deleted on completion, got: %v", err)
	}
	if _, err := os.Stat(filepath.Join(StateDir, "ship.events.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("expected event log to be deleted on completion, got: %v", err)
	}
}

func TestRunCommandStageFailurePreservesEvents(t *testing.T) {
	t.Chdir(t.TempDir())
	wf := testWorkflow([]Stage{{ID: "check", Type: StageTypeCommand, Command: failCmd()}}, nil)

	_, _ = Run(Options{
		Name:     "ship",
		Workflow: wf,
		Config:   testConfig(echoCmd()),
		ResolvePrompt: func(name string) (string, error) {
			return name, nil
		},
	})
	if _, err := os.Stat(filepath.Join(StateDir, "ship.events.jsonl")); err != nil {
		t.Fatalf("expected event log to remain after failure: %v", err)
	}
}

func TestResumeUsesPerWorkflowState(t *testing.T) {
	t.Chdir(t.TempDir())
	logFile := filepath.Join(".", "stage-log")
	cmd := catToFileCmd(logFile)
	wf := testWorkflow([]Stage{
		{ID: "first", Type: StageTypeAgent, Prompt: "first", Max: 1},
		{ID: "second", Type: StageTypeAgent, Prompt: "second", Max: 1},
	}, nil)
	state := &State{
		SchemaVersion: SchemaVersion,
		Workflow:      "ship",
		RunID:         "abc",
		StartedAt:     testTime(),
		UpdatedAt:     testTime(),
		NextStageID:   "second",
		Stages:        initialStageStatus(wf),
	}
	(store{name: "ship"}).save(state)

	_, err := Run(Options{
		Name:     "ship",
		Workflow: wf,
		Config:   testConfig(cmd),
		ResolvePrompt: func(name string) (string, error) {
			return name + "\n", nil
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "first") || !strings.Contains(string(data), "second") {
		t.Fatalf("expected only second stage to run, got %q", data)
	}
}

func TestResumeIgnoresMismatchedWorkflowState(t *testing.T) {
	t.Chdir(t.TempDir())
	logFile := filepath.Join(".", "stage-log")
	cmd := catToFileCmd(logFile)
	wf := testWorkflow([]Stage{
		{ID: "first", Type: StageTypeAgent, Prompt: "first", Max: 1},
		{ID: "second", Type: StageTypeAgent, Prompt: "second", Max: 1},
	}, nil)
	state := &State{
		SchemaVersion: SchemaVersion,
		Workflow:      "other",
		RunID:         "abc",
		StartedAt:     testTime(),
		UpdatedAt:     testTime(),
		NextStageID:   "second",
		Stages:        initialStageStatus(wf),
	}
	(store{name: "ship"}).save(state)

	_, err := Run(Options{
		Name:     "ship",
		Workflow: wf,
		Config:   testConfig(cmd),
		ResolvePrompt: func(name string) (string, error) {
			return name + "\n", nil
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "first") || !strings.Contains(string(data), "second") {
		t.Fatalf("expected mismatched state to be ignored, got %q", data)
	}
}

func TestRunScrubsStaleSignalFilesAtEntry(t *testing.T) {
	t.Chdir(t.TempDir())
	// A stale .brr-complete survives a kill -9 of a previous run. Without the
	// entry scrub, the failing command stage's post-Wait detection would see it
	// and record the stage "completed", advancing past a failing gate.
	if err := os.WriteFile(engine.SignalComplete, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	wf := testWorkflow([]Stage{{ID: "check", Type: StageTypeCommand, Command: failCmd()}}, nil)

	result, err := Run(Options{
		Name:     "ship",
		Workflow: wf,
		Config:   testConfig(echoCmd()),
		ResolvePrompt: func(name string) (string, error) {
			return name, nil
		},
	})
	if err == nil {
		t.Fatal("expected the real command failure to surface, not a stale .brr-complete")
	}
	if result == nil || result.Reason == engine.ReasonComplete {
		t.Fatalf("stale signal masked the failure, got %#v", result)
	}
	if _, statErr := os.Stat(engine.SignalComplete); statErr == nil {
		t.Error("expected stale .brr-complete to be scrubbed at entry")
	}
	state := readState(t, "ship")
	if state.Stages[0].Status != "error" {
		t.Fatalf("expected failing stage status error, got %q", state.Stages[0].Status)
	}
}

func TestRunCommandStageInterruptPreservesState(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.Interrupt signaling is not reliable for this process-level test on Windows")
	}
	t.Chdir(t.TempDir())
	wf := testWorkflow([]Stage{{ID: "check", Type: StageTypeCommand, Command: sleepCmd()}}, nil)

	go func() {
		time.Sleep(200 * time.Millisecond)
		proc, err := os.FindProcess(os.Getpid())
		if err == nil {
			_ = proc.Signal(os.Interrupt)
		}
	}()

	result, err := Run(Options{
		Name:     "ship",
		Workflow: wf,
		Config:   testConfig(echoCmd()),
		ResolvePrompt: func(name string) (string, error) {
			return name, nil
		},
	})
	if !errors.Is(err, engine.ErrInterrupted) {
		t.Fatalf("expected interrupted error, got result=%#v err=%v", result, err)
	}
	if result == nil || result.Reason != engine.ReasonInterrupted {
		t.Fatalf("expected interrupted result, got %#v", result)
	}
	state := readState(t, "ship")
	if state.NextStageID != "check" {
		t.Fatalf("expected resume at interrupted stage, got %q", state.NextStageID)
	}
	if state.Stages[0].Status != "interrupted" {
		t.Fatalf("expected stage status interrupted, got %q", state.Stages[0].Status)
	}
}
