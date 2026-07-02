package notify

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/hl/brr/internal/engine"
)

func TestFormatComplete(t *testing.T) {
	title, body := format(&engine.Result{Reason: engine.ReasonComplete})
	if title != "brr — complete" {
		t.Errorf("unexpected title: %q", title)
	}
	if body != "All tasks complete." {
		t.Errorf("unexpected body: %q", body)
	}
}

func TestFormatApprovalWithContent(t *testing.T) {
	title, body := format(&engine.Result{
		Reason:          engine.ReasonApproval,
		ApprovalContent: "Approval details",
	})
	if title != "brr — approval needed" {
		t.Errorf("unexpected title: %q", title)
	}
	if body != "Approval details" {
		t.Errorf("unexpected body: %q", body)
	}
}

func TestFormatApprovalWithoutContent(t *testing.T) {
	title, body := format(&engine.Result{Reason: engine.ReasonApproval})
	if title != "brr — approval needed" {
		t.Errorf("unexpected title: %q", title)
	}
	if body != "A task needs human approval." {
		t.Errorf("unexpected body: %q", body)
	}
}

func TestFormatApprovalTruncatesLongContent(t *testing.T) {
	long := strings.Repeat("word ", 100) // 500 chars
	_, body := format(&engine.Result{
		Reason:          engine.ReasonApproval,
		ApprovalContent: long,
	})
	if len(body) > 260 {
		t.Errorf("expected body to be truncated, got %d bytes", len(body))
	}
	if !strings.HasSuffix(body, "…") {
		t.Error("expected truncation marker")
	}
}

func TestFormatFailed(t *testing.T) {
	title, body := format(&engine.Result{Reason: engine.ReasonFailed})
	if title != "brr — failed" {
		t.Errorf("unexpected title: %q", title)
	}
	if body != "The agent reported a failure." {
		t.Errorf("unexpected body: %q", body)
	}
}

func TestFormatFailedWithContent(t *testing.T) {
	title, body := format(&engine.Result{
		Reason:        engine.ReasonFailed,
		FailedContent: "API error: rate limited",
	})
	if title != "brr — failed" {
		t.Errorf("unexpected title: %q", title)
	}
	if body != "API error: rate limited" {
		t.Errorf("unexpected body: %q", body)
	}
}

func TestFormatMaxIterations(t *testing.T) {
	title, body := format(&engine.Result{Reason: engine.ReasonMaxIterations})
	if title != "brr — max iterations" {
		t.Errorf("unexpected title: %q", title)
	}
	if body != "Maximum iteration count reached." {
		t.Errorf("unexpected body: %q", body)
	}
}

func TestFormatFailStreak(t *testing.T) {
	title, body := format(&engine.Result{Reason: engine.ReasonFailStreak})
	if title != "brr — stopped" {
		t.Errorf("unexpected title: %q", title)
	}
	if body != "Too many consecutive failures." {
		t.Errorf("unexpected body: %q", body)
	}
}

func TestFormatCommandFailed(t *testing.T) {
	title, body := format(&engine.Result{Reason: engine.ReasonCommandFailed})
	if title != "brr — command failed" {
		t.Errorf("unexpected title: %q", title)
	}
	if body != "A command stage exited non-zero." {
		t.Errorf("unexpected body: %q", body)
	}
}

func TestFormatWorkflowError(t *testing.T) {
	title, body := formatWorkflowError(errors.New("stage 1 (prepare): prompt file not found: prepare.md"))
	if title != "brr — workflow error" {
		t.Errorf("unexpected title: %q", title)
	}
	if body != "stage 1 (prepare): prompt file not found: prepare.md" {
		t.Errorf("unexpected body: %q", body)
	}
}

func TestFormatWorkflowErrorWithoutError(t *testing.T) {
	title, body := formatWorkflowError(nil)
	if title != "brr — workflow error" {
		t.Errorf("unexpected title: %q", title)
	}
	if body != "The workflow stopped with an error." {
		t.Errorf("unexpected body: %q", body)
	}
}

func TestTruncateShort(t *testing.T) {
	s := "hello"
	if truncate(s, 256) != "hello" {
		t.Errorf("short string should not be truncated")
	}
}

func TestTruncateLong(t *testing.T) {
	s := "the quick brown fox jumps over the lazy dog"
	result := truncate(s, 15)
	if !strings.HasSuffix(result, "…") {
		t.Error("expected truncation marker")
	}
	// Should break at word boundary — "the quick brown" is 15 chars,
	// last space before index 15 is at 9, so "the quick…"
	prefix := strings.TrimSuffix(result, "…")
	if strings.ContainsRune(prefix, ' ') {
		// Ensure it doesn't end mid-word
		lastSpace := strings.LastIndex(s[:15], " ")
		if len(prefix) != lastSpace {
			t.Errorf("expected word-boundary truncation, got %q", result)
		}
	}
}

func TestTruncatePreservesUTF8(t *testing.T) {
	s := strings.Repeat("🔥", 10)
	result := truncate(s, 6)
	if !utf8.ValidString(result) {
		t.Fatalf("truncate returned invalid UTF-8: %q", result)
	}
	if !strings.HasSuffix(result, "…") {
		t.Fatal("expected truncation marker")
	}
	if strings.TrimSuffix(result, "…") != "🔥" {
		t.Fatalf("expected one complete rune before truncation, got %q", result)
	}
}
