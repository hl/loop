package notify

import (
	"fmt"
	"os/exec"
	"strings"
	"unicode/utf8"

	"github.com/hl/brr/internal/engine"
)

// Send dispatches a best-effort desktop notification for the given engine result.
// It returns an error if the notification could not be sent, but callers should
// treat failures as non-fatal.
func Send(result *engine.Result) error {
	title, body := format(result)
	return send(title, body)
}

// SendWorkflowError dispatches a notification for workflow errors that happen
// before the engine can return a structured stop reason.
func SendWorkflowError(err error) error {
	title, body := formatWorkflowError(err)
	return send(title, body)
}

func format(result *engine.Result) (title, body string) {
	switch result.Reason {
	case engine.ReasonComplete:
		return "brr — complete", "All tasks complete."
	case engine.ReasonFailed:
		title = "brr — failed"
		if result.FailedContent != "" {
			body = truncate(result.FailedContent, 256)
		} else {
			body = "The agent reported a failure."
		}
		return title, body
	case engine.ReasonApproval:
		title = "brr — approval needed"
		if result.ApprovalContent != "" {
			body = truncate(result.ApprovalContent, 256)
		} else {
			body = "A task needs human approval."
		}
		return title, body
	case engine.ReasonCycle:
		return "brr — cycle requested", "A workflow stage requested another cycle."
	case engine.ReasonMaxIterations:
		return "brr — max iterations", "Maximum iteration count reached."
	case engine.ReasonFailStreak:
		return "brr — stopped", "Too many consecutive failures."
	case engine.ReasonCommandFailed:
		return "brr — command failed", "A command stage exited non-zero."
	default:
		return "brr — stopped", "The loop has stopped."
	}
}

func formatWorkflowError(err error) (title, body string) {
	if err == nil {
		return "brr — workflow error", "The workflow stopped with an error."
	}
	return "brr — workflow error", truncate(err.Error(), 256)
}

// truncate shortens s to at most maxLen bytes, breaking at the last space
// boundary to avoid cutting words mid-way. Appends "…" when truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	cut := strings.LastIndex(s[:maxLen], " ")
	if cut <= 0 {
		cut = maxLen
	}
	for cut > 0 && !utf8.ValidString(s[:cut]) {
		cut--
	}
	return s[:cut] + "…"
}

// notifySendArgs builds the argv for the Linux notify-send command. The "--"
// terminates option parsing (notify-send uses GOption, which otherwise scans
// the whole argv for flags), so an agent-controlled title or body that begins
// with "-" is treated as positional text rather than a dropped option.
func notifySendArgs(title, body string) []string {
	return []string{"notify-send", "--", title, body}
}

// run executes a command and returns any error, swallowing stderr output.
func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w (%s)", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}
