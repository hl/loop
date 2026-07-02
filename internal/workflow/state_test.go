package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStatusPrintsSavedState(t *testing.T) {
	t.Chdir(t.TempDir())
	wf := testWorkflow([]Stage{{ID: "build", Type: StageTypeCommand, Command: echoCmd()}}, nil)
	(store{name: "ship"}).save(&State{
		SchemaVersion: SchemaVersion,
		Workflow:      "ship",
		RunID:         "abc",
		StartedAt:     testTime(),
		UpdatedAt:     testTime(),
		NextStageID:   "build",
		Stages:        initialStageStatus(wf),
	})
	var out strings.Builder
	if err := Status("ship", &out); err != nil {
		t.Fatalf("status error: %v", err)
	}
	if !strings.Contains(out.String(), "workflow ship") || !strings.Contains(out.String(), "next:") || !strings.Contains(out.String(), "build") {
		t.Fatalf("unexpected status output: %s", out.String())
	}
}

func TestStatusPrintsVisualStageState(t *testing.T) {
	t.Chdir(t.TempDir())
	state := &State{
		SchemaVersion: SchemaVersion,
		Workflow:      "ship",
		RunID:         "abc",
		StartedAt:     testTime(),
		UpdatedAt:     testTime(),
		NextStageID:   "check",
		Stages: []StageStatus{
			{ID: "spec", Type: StageTypeAgent, Status: "completed", Duration: 2 * time.Second, Prompt: "spec", Profile: "codex"},
			{ID: "check", Type: StageTypeCommand, Status: "running", Command: []string{"make", "check"}},
			{ID: "review", Type: StageTypeAgent, Status: "pending", Prompt: "review"},
		},
	}
	(store{name: "ship"}).save(state)

	var out strings.Builder
	if err := Status("ship", &out); err != nil {
		t.Fatalf("status error: %v", err)
	}
	got := out.String()
	for _, want := range []string{"✓ spec", "▶ check", "○ review", "agent spec via codex", "make check"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in status output:\n%s", want, got)
		}
	}
}

func TestStatusFrameUsesSpinnerForRunningStage(t *testing.T) {
	state := &State{
		Workflow:    "ship",
		RunID:       "abc",
		NextStageID: "build",
		Stages: []StageStatus{
			{ID: "build", Type: StageTypeCommand, Status: "running", Command: []string{"make", "check"}},
		},
	}
	var out strings.Builder
	if err := writeStatusFrame(&out, state, "*"); err != nil {
		t.Fatalf("status frame error: %v", err)
	}
	if !strings.Contains(out.String(), "* build") {
		t.Fatalf("expected spinner frame in status output:\n%s", out.String())
	}
}

func TestAtomicWriteRegularFileRoundTrips(t *testing.T) {
	t.Chdir(t.TempDir())
	path := filepath.Join("sub", "state.json")
	payload := []byte(`{"schema_version":2}` + "\n")
	if err := atomicWriteRegularFile(path, payload, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("content mismatch after sync+rename: got %q want %q", got, payload)
	}
	// Overwrite exercises the rename-over-existing path after the fsync reorder.
	payload2 := []byte("second\n")
	if err := atomicWriteRegularFile(path, payload2, 0o644); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	got, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload2) {
		t.Fatalf("overwrite content mismatch: got %q want %q", got, payload2)
	}
}

func TestWatchStatusKeepsFinalFrameWhenStateVanishes(t *testing.T) {
	t.Chdir(t.TempDir())
	(store{name: "ship"}).save(&State{
		SchemaVersion: SchemaVersion,
		Workflow:      "ship",
		RunID:         "abc",
		NextStageID:   "build",
		Stages: []StageStatus{
			{ID: "build", Type: StageTypeCommand, Status: "completed", Command: []string{"make", "check"}},
		},
	})

	var out strings.Builder
	sw := &statusWatcher{name: "ship", w: &out}

	// Tick 1: state present → renders a frame.
	if done, err := sw.tick(); err != nil || done {
		t.Fatalf("first tick: done=%v err=%v", done, err)
	}
	if !strings.Contains(out.String(), "ship") {
		t.Fatalf("expected a rendered frame, got:\n%s", out.String())
	}

	// State vanishes (workflow completion / --reset window).
	(store{name: "ship"}).delete()

	// Tick 2: the first absent tick is tolerated, keeping the last frame.
	if done, err := sw.tick(); err != nil || done {
		t.Fatalf("watcher must tolerate one absent tick: done=%v err=%v", done, err)
	}
	before := out.String()

	// Tick 3: still absent → conclude without wiping the final frame.
	done, err := sw.tick()
	if err != nil {
		t.Fatalf("third tick error: %v", err)
	}
	if !done {
		t.Fatal("expected watcher to conclude after a second absent tick")
	}
	final := out.String()
	if strings.Contains(final, "No state found") {
		t.Fatalf("must not replace the final frame with a no-state message:\n%s", final)
	}
	if !strings.Contains(final, "state cleared") {
		t.Fatalf("expected a closing 'state cleared' line, got:\n%s", final)
	}
	if strings.Contains(strings.TrimPrefix(final, before), "\033[2J") {
		t.Fatalf("closing tick must not clear the screen: %q", strings.TrimPrefix(final, before))
	}
}

func TestWatchStatusReportsNoStateWhenNeverSeen(t *testing.T) {
	t.Chdir(t.TempDir())
	var out strings.Builder
	sw := &statusWatcher{name: "ship", w: &out}
	done, err := sw.tick()
	if err != nil {
		t.Fatalf("tick error: %v", err)
	}
	if !done {
		t.Fatal("expected watcher to conclude when no state ever existed")
	}
	if !strings.Contains(out.String(), "No state found") {
		t.Fatalf("expected no-state message, got:\n%s", out.String())
	}
}

func TestRunDiagramShowsFlowAndCycleState(t *testing.T) {
	wf := testWorkflow([]Stage{
		{ID: "build", Type: StageTypeAgent, Prompt: "build"},
		{ID: "check", Type: StageTypeCommand, Command: []string{"make", "check"}},
		{ID: "review", Type: StageTypeAgent, Prompt: "review"},
	}, &Cycle{Target: "build", Max: 3})
	state := &State{
		CycleCount: 1,
		Stages: []StageStatus{
			{ID: "build", Status: "completed"},
			{ID: "check", Status: "running"},
			{ID: "review", Status: "pending"},
		},
	}

	var out strings.Builder
	if err := writeRunDiagram(&out, wf, state, "*"); err != nil {
		t.Fatalf("run diagram error: %v", err)
	}
	got := out.String()
	for _, want := range []string{"flow:", "✓ build", "* check", "○ review", "↺ build", "used 1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in run diagram:\n%s", want, got)
		}
	}
	// The cycle target here is the first stage ("build"), so the diagram must not
	// claim the last stage ("review") is the one looping back.
	if strings.Contains(got, "review ↺") {
		t.Fatalf("cycle edge must not be sourced from the last stage:\n%s", got)
	}
}

func TestInitTemplateWritesShipWorkflow(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := InitTemplate("ship", "ship"); err != nil {
		t.Fatalf("init template: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(".brr", "workflows", "ship.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	wf, err := Load(data)
	if err != nil {
		t.Fatalf("template should load: %v", err)
	}
	if len(wf.Stages) == 0 || wf.Cycle == nil || wf.Cycle.Target != "build" {
		t.Fatalf("unexpected template workflow: %#v", wf)
	}
}

func TestInitTemplateRejectsSymlinkedWorkflowDir(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir(".brr", 0o755); err != nil {
		t.Fatal(err)
	}
	targetDir := filepath.Join(t.TempDir(), "outside-workflows")
	if err := os.Mkdir(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(targetDir, filepath.Join(".brr", "workflows")); err != nil {
		t.Skip("symlinks not supported")
	}

	err := InitTemplate("ship", "ship")
	if err == nil {
		t.Fatal("expected symlinked workflow dir to be rejected")
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected symlink error, got: %v", err)
	}
	if _, err := os.Stat(filepath.Join(targetDir, "ship.yaml")); !os.IsNotExist(err) {
		t.Fatalf("template write followed symlink, stat err: %v", err)
	}
}

func TestStateWriteRejectsSymlink(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(StateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target-state")
	if err := os.WriteFile(target, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(StateDir, "ship.json")); err != nil {
		t.Skip("symlinks not supported")
	}
	state := &State{SchemaVersion: SchemaVersion, Workflow: "ship", RunID: "abc", NextStageID: "build"}
	(store{name: "ship"}).save(state)
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "keep" {
		t.Fatalf("state write followed symlink and modified target: %q", data)
	}
}

func TestResetClearsStateAndEvents(t *testing.T) {
	t.Chdir(t.TempDir())
	s := store{name: "ship"}
	s.save(&State{
		SchemaVersion: SchemaVersion,
		Workflow:      "ship",
		RunID:         "abc",
		StartedAt:     testTime(),
		UpdatedAt:     testTime(),
		NextStageID:   "build",
	})
	s.appendEvent(Event{RunID: "abc", Workflow: "ship", Time: testTime(), Type: "stage_started", StageID: "build"})

	removed, err := Reset("ship")
	if err != nil {
		t.Fatalf("Reset error: %v", err)
	}
	if !removed {
		t.Fatal("expected Reset to report files removed")
	}
	if _, err := os.Stat(filepath.Join(StateDir, "ship.json")); !os.IsNotExist(err) {
		t.Fatalf("state file not removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(StateDir, "ship.events.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("events file not removed: %v", err)
	}
}

func TestResetNoStateReportsNotRemoved(t *testing.T) {
	t.Chdir(t.TempDir())
	removed, err := Reset("ship")
	if err != nil {
		t.Fatalf("Reset error: %v", err)
	}
	if removed {
		t.Fatal("expected Reset to report nothing removed when no state exists")
	}
}

func TestResetRejectsInvalidName(t *testing.T) {
	if _, err := Reset(""); err == nil {
		t.Fatal("expected empty name to be rejected")
	}
	if _, err := Reset("a/b"); err == nil {
		t.Fatal("expected path separator to be rejected")
	}
}

func TestEventWriteRejectsSymlink(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(StateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target-events")
	if err := os.WriteFile(target, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(StateDir, "ship.events.jsonl")); err != nil {
		t.Skip("symlinks not supported")
	}
	(store{name: "ship"}).appendEvent(Event{RunID: "abc", Workflow: "ship", Type: "test", Time: testTime()})
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "keep" {
		t.Fatalf("event write followed symlink and modified target: %q", data)
	}
}
