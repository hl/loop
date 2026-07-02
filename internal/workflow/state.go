package workflow

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/hl/brr/internal/fsutil"
)

// maxStateFileSize bounds how much of a workflow state JSON file brr will read.
// State holds per-stage status for one run; 4 MiB is far beyond any real state
// file yet small enough that a planted multi-GB file cannot exhaust memory in an
// otherwise read-only command like `brr workflow status`.
const maxStateFileSize = 4 << 20 // 4 MiB

type store struct {
	name string
}

func (s store) statePath() string {
	return filepath.Join(StateDir, s.name+".json")
}

func (s store) eventsPath() string {
	return filepath.Join(StateDir, s.name+".events.jsonl")
}

func (s store) load() (*State, error) {
	data, err := fsutil.ReadRegularFileCapped(s.statePath(), maxStateFileSize)
	if err != nil {
		return nil, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func (s store) save(state *State) {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not save workflow state: %v\n", err)
		return
	}
	if err := ensureStateDir(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not create workflow state dir: %v\n", err)
		return
	}
	if err := atomicWriteRegularFile(s.statePath(), append(data, '\n'), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not save workflow state: %v\n", err)
	}
}

func (s store) delete() {
	removeStatePath(s.statePath())
}

func (s store) deleteAll() {
	removeStatePath(s.statePath())
	removeStatePath(s.eventsPath())
}

// Reset discards the saved state and event log for the named workflow.
// It reports whether any files were removed so callers can distinguish a
// no-op from an actual cleanup.
func Reset(name string) (bool, error) {
	if err := validateName(name); err != nil {
		return false, err
	}
	s := store{name: name}
	hadState := pathExists(s.statePath())
	hadEvents := pathExists(s.eventsPath())
	s.deleteAll()
	return hadState || hadEvents, nil
}

func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func (s store) appendEvent(event Event) {
	if err := ensureStateDir(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not create workflow event dir: %v\n", err)
		return
	}
	data, err := json.Marshal(event)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not encode workflow event: %v\n", err)
		return
	}
	if err := appendRegularFile(s.eventsPath(), append(data, '\n'), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not write workflow event: %v\n", err)
	}
}

func initialStageStatus(wf Workflow) []StageStatus {
	statuses := make([]StageStatus, 0, len(wf.Stages))
	for _, stage := range wf.Stages {
		statuses = append(statuses, stageStatus(stage, "pending", "", 0))
	}
	return statuses
}

func updateStageStatus(state *State, stage Stage, status, reason string, duration time.Duration) {
	for i := range state.Stages {
		if state.Stages[i].ID == stage.ID {
			state.Stages[i] = stageStatus(stage, status, reason, duration)
			return
		}
	}
	state.Stages = append(state.Stages, stageStatus(stage, status, reason, duration))
}

func stageStatus(stage Stage, status, reason string, duration time.Duration) StageStatus {
	return StageStatus{
		ID:       stage.ID,
		Type:     stage.Type,
		Status:   status,
		Reason:   reason,
		Duration: duration,
		Profile:  stage.Profile,
		Prompt:   stage.Prompt,
		Command:  append([]string(nil), stage.Command...),
	}
}

func validResumeState(s *State, wf Workflow, name string) bool {
	return s.SchemaVersion == SchemaVersion &&
		s.Workflow == name &&
		s.RunID != "" &&
		s.CycleCount >= 0 &&
		(wf.Cycle != nil || s.CycleCount == 0) &&
		(wf.Cycle == nil || s.CycleCount <= wf.Cycle.Max) &&
		s.NextStageID != "" &&
		stageIndexByID(wf, s.NextStageID) >= 0
}

func stageIndexByID(wf Workflow, id string) int {
	for i, stage := range wf.Stages {
		if stage.ID == id {
			return i
		}
	}
	return -1
}

func removeStatePath(path string) {
	fi, err := os.Lstat(path)
	if err != nil {
		return
	}
	if fi.Mode().IsRegular() || fi.Mode()&os.ModeSymlink != 0 {
		_ = os.Remove(path)
	}
}

func gitHEAD() string {
	out, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func newRunID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
