package workflow

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/hl/brr/internal/engine"
	"github.com/hl/brr/internal/ui"
)

func stopReason(result *engine.Result) string {
	if result == nil {
		return ""
	}
	switch result.Reason {
	case engine.ReasonComplete:
		return "complete"
	case engine.ReasonFailed:
		return "failed"
	case engine.ReasonApproval:
		return "approval"
	case engine.ReasonCycle:
		return "cycle"
	case engine.ReasonMaxIterations:
		return "max_iterations"
	case engine.ReasonFailStreak:
		return "fail_streak"
	case engine.ReasonCommandFailed:
		return "command_failed"
	case engine.ReasonInterrupted:
		return "interrupted"
	default:
		return "unknown"
	}
}

func stageStatusFromResult(result *engine.Result, err error) string {
	if result != nil && result.Reason == engine.ReasonInterrupted {
		return "interrupted"
	}
	if err != nil {
		return "error"
	}
	if result == nil {
		return "completed"
	}
	switch result.Reason {
	case engine.ReasonFailed:
		return "failed"
	case engine.ReasonApproval:
		return "approval"
	case engine.ReasonCycle:
		return "cycle"
	default:
		return "completed"
	}
}

func printWorkflowSummary(wf Workflow, name string) {
	fmt.Fprintf(os.Stderr, "  %sworkflow:%s %s\n", ui.Dim, ui.Reset, name)
	if wf.Description != "" {
		fmt.Fprintf(os.Stderr, "  %sdescription:%s %s\n", ui.Dim, ui.Reset, wf.Description)
	}
	fmt.Fprintf(os.Stderr, "  %sstages:%s  %d\n", ui.Dim, ui.Reset, len(wf.Stages))
	if wf.Cycle != nil {
		fmt.Fprintf(os.Stderr, "  %scycle:%s   %s (max %d)\n", ui.Dim, ui.Reset, wf.Cycle.Target, wf.Cycle.Max)
	}
	fmt.Fprintln(os.Stderr)
}

func printStageHeader(num, total int, stage Stage, cycle int, wf Workflow) {
	cycleLabel := ""
	if cycle > 0 {
		cycleLabel = fmt.Sprintf(" %s[cycle %d]%s", ui.Magenta, cycle, ui.Reset)
	}
	detail := strings.Join(stage.Command, " ")
	if stage.Type == StageTypeAgent {
		detail = fmt.Sprintf("%s (max %d)", stage.Prompt, effectiveMax(wf, stage))
	}
	fmt.Fprintf(os.Stderr, "\n%s━━━%s %s%sStage %d/%d — %s%s %s▸ %s%s ━━━%s\n",
		ui.Dim, ui.Reset,
		ui.Bold, ui.Cyan, num, total, stage.ID, ui.Reset,
		ui.Dim, detail, cycleLabel, ui.Reset,
	)
}

func printRunDiagram(wf Workflow, state *State, spinner string) {
	_ = writeRunDiagram(os.Stderr, wf, state, spinner)
}

func writeRunDiagram(w io.Writer, wf Workflow, state *State, spinner string) error {
	if _, err := fmt.Fprintf(w, "  %sflow:%s ", ui.Dim, ui.Reset); err != nil {
		return err
	}
	for i, stage := range wf.Stages {
		if i > 0 {
			if _, err := fmt.Fprintf(w, " %s→%s ", ui.Dim, ui.Reset); err != nil {
				return err
			}
		}
		status := stageStatusByID(state, stage.ID)
		icon := stageStatusIcon(status.Status, spinner)
		if _, err := fmt.Fprintf(w, "%s %s", icon, stage.ID); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	if wf.Cycle != nil {
		if _, err := fmt.Fprintf(w, "  %scycle:%s %s %s↺%s %s (max %d, used %d)\n",
			ui.Dim, ui.Reset,
			workflowLastStageID(wf),
			ui.Magenta, ui.Reset,
			wf.Cycle.Target,
			wf.Cycle.Max,
			state.CycleCount,
		); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(w)
	return err
}

func stageStatusByID(state *State, id string) StageStatus {
	if state != nil {
		for _, stage := range state.Stages {
			if stage.ID == id {
				return stage
			}
		}
	}
	return StageStatus{ID: id, Status: "pending"}
}

func workflowLastStageID(wf Workflow) string {
	if len(wf.Stages) == 0 {
		return "-"
	}
	return wf.Stages[len(wf.Stages)-1].ID
}
