package workflow

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hl/brr/internal/ui"
)

var statusSpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func Status(name string, w io.Writer) error {
	if name != "" {
		return printStatus(name, w)
	}
	entries, err := os.ReadDir(StateDir)
	if err != nil {
		if os.IsNotExist(err) {
			_, err := fmt.Fprintln(w, "No workflow state found.")
			return err
		}
		return fmt.Errorf("reading %s: %w", StateDir, err)
	}
	found := false
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		found = true
		name := strings.TrimSuffix(entry.Name(), ".json")
		if err := printStatus(name, w); err != nil {
			return err
		}
	}
	if !found {
		if _, err := fmt.Fprintln(w, "No workflow state found."); err != nil {
			return err
		}
	}
	return nil
}

func printStatus(name string, w io.Writer) error {
	if err := validateName(name); err != nil {
		return err
	}
	state, err := (store{name: name}).load()
	if err != nil {
		if os.IsNotExist(err) {
			_, err := fmt.Fprintf(w, "No state found for workflow %q.\n", name)
			return err
		}
		return fmt.Errorf("reading workflow state: %w", err)
	}
	return writeStatus(w, state)
}

func writeStatus(w io.Writer, state *State) error {
	return writeStatusFrame(w, state, "")
}

func WatchStatus(name string, w io.Writer, interval time.Duration) error {
	if name == "" {
		return fmt.Errorf("workflow name is required for watch mode")
	}
	if err := validateName(name); err != nil {
		return err
	}
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	watcher := &statusWatcher{name: name, w: w}
	for {
		done, err := watcher.tick()
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		<-ticker.C
	}
}

// statusWatcher renders successive frames of a workflow's saved state. It is
// split out from WatchStatus so the frame/miss transitions can be unit-tested
// without real timers.
type statusWatcher struct {
	name     string
	w        io.Writer
	sawState bool
	misses   int
	frame    int
}

// tick renders a single frame. It returns done=true when the watch should stop.
func (sw *statusWatcher) tick() (bool, error) {
	state, err := (store{name: sw.name}).load()
	if err != nil {
		if !os.IsNotExist(err) {
			return true, fmt.Errorf("reading workflow state: %w", err)
		}
		if !sw.sawState {
			// Never rendered a frame — the state was already gone at startup, so the
			// original "no state found" message is the correct terminal output.
			_, err := fmt.Fprintf(sw.w, "\033[H\033[2JNo state found for workflow %q.\n", sw.name)
			return true, err
		}
		sw.misses++
		if sw.misses <= 1 {
			// Tolerate a single absent tick: workflow completion and `--reset` both
			// delete the state file in a window where it legitimately reappears (or
			// stays gone because the run finished). Keep the last frame on screen.
			return false, nil
		}
		// The state is durably gone. Leave the final all-green frame intact (no
		// screen clear) and print a clear closing line beneath it.
		_, err := fmt.Fprintf(sw.w, "  %sstate cleared — workflow finished or was reset%s\n", ui.Dim, ui.Reset)
		return true, err
	}
	sw.misses = 0
	sw.sawState = true
	if _, err := fmt.Fprint(sw.w, "\033[H\033[2J"); err != nil {
		return true, err
	}
	if err := writeStatusFrame(sw.w, state, statusSpinnerFrames[sw.frame%len(statusSpinnerFrames)]); err != nil {
		return true, err
	}
	sw.frame++
	return false, nil
}

func writeStatusFrame(w io.Writer, state *State, spinner string) error {
	if _, err := fmt.Fprintf(w, "%s%sworkflow%s %s\n", ui.Bold, ui.Cyan, ui.Reset, state.Workflow); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  %srun:%s     %s\n", ui.Dim, ui.Reset, state.RunID); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  %snext:%s    %s\n", ui.Dim, ui.Reset, emptyLabel(state.NextStageID)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  %scycle:%s   %d\n", ui.Dim, ui.Reset, state.CycleCount); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  %sstarted:%s %s\n", ui.Dim, ui.Reset, formatStatusTime(state.StartedAt)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  %supdated:%s %s\n\n", ui.Dim, ui.Reset, formatStatusTime(state.UpdatedAt)); err != nil {
		return err
	}

	idWidth := maxStageIDWidth(state.Stages)
	for _, stage := range state.Stages {
		icon := stageStatusIcon(stage.Status, spinner)
		if _, err := fmt.Fprintf(w, "  %s %-*s  %s%-11s%s", icon, idWidth, stage.ID, stageStatusColor(stage.Status), stage.Status, ui.Reset); err != nil {
			return err
		}
		if stage.Duration > 0 {
			if _, err := fmt.Fprintf(w, "  %8s", stage.Duration.Round(time.Second)); err != nil {
				return err
			}
		} else {
			if _, err := fmt.Fprint(w, "          "); err != nil {
				return err
			}
		}
		if detail := stageStatusDetail(stage); detail != "" {
			if _, err := fmt.Fprintf(w, "  %s%s%s", ui.Dim, detail, ui.Reset); err != nil {
				return err
			}
		}
		if stage.Reason != "" {
			if _, err := fmt.Fprintf(w, "  %s(%s)%s", ui.Dim, stage.Reason, ui.Reset); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	return nil
}

func formatStatusTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format(time.RFC3339)
}

func emptyLabel(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func maxStageIDWidth(stages []StageStatus) int {
	width := 1
	for _, stage := range stages {
		if len(stage.ID) > width {
			width = len(stage.ID)
		}
	}
	return width
}

func stageStatusIcon(status, spinner string) string {
	switch status {
	case "completed":
		return ui.Green + "✓" + ui.Reset
	case "running":
		if spinner != "" {
			return ui.Cyan + spinner + ui.Reset
		}
		return ui.Cyan + "▶" + ui.Reset
	case "failed", "error":
		return ui.Red + "✗" + ui.Reset
	case "approval":
		return ui.Yellow + "⏸" + ui.Reset
	case "cycle":
		return ui.Magenta + "↻" + ui.Reset
	case "interrupted":
		return ui.Yellow + "■" + ui.Reset
	default:
		return ui.Dim + "○" + ui.Reset
	}
}

func stageStatusColor(status string) string {
	switch status {
	case "completed":
		return ui.Green
	case "running":
		return ui.Cyan
	case "failed", "error":
		return ui.Red
	case "approval", "interrupted":
		return ui.Yellow
	case "cycle":
		return ui.Magenta
	default:
		return ui.Dim
	}
}

func stageStatusDetail(stage StageStatus) string {
	switch stage.Type {
	case StageTypeAgent:
		if stage.Profile != "" {
			return fmt.Sprintf("agent %s via %s", stage.Prompt, stage.Profile)
		}
		return "agent " + stage.Prompt
	case StageTypeCommand:
		return strings.Join(stage.Command, " ")
	default:
		return stage.Type
	}
}
