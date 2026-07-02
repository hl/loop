package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// newTestRootCmd creates a fresh cobra command with the same flags as rootCmd.
// Using a fresh command per test avoids global state leakage.
func newTestRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "brr <prompt> [flags]",
		Args:         cobra.ExactArgs(1),
		RunE:         run,
		SilenceUsage: true,
	}
	cmd.Flags().IntP("max", "m", 0, "max iterations")
	cmd.Flags().StringP("profile", "p", "", "profile name")
	cmd.Flags().BoolP("notify", "n", false, "send a desktop notification when the loop stops")
	return cmd
}

func newTestWorkflowRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "run <name>",
		Args:         cobra.ExactArgs(1),
		RunE:         runWorkflow,
		SilenceUsage: true,
	}
	cmd.Flags().StringP("profile", "p", "", "profile name")
	cmd.Flags().BoolP("notify", "n", false, "send a desktop notification when the workflow completes")
	cmd.Flags().Bool("reset", false, "discard saved progress and start from the first stage")
	return cmd
}

func newTestWorkflowValidateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "validate <name>",
		Args:         cobra.ExactArgs(1),
		RunE:         validateWorkflow,
		SilenceUsage: true,
	}
	cmd.Flags().StringP("profile", "p", "", "profile name")
	return cmd
}

func newTestWorkflowStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "status [name]",
		Args:         cobra.MaximumNArgs(1),
		RunE:         statusWorkflow,
		SilenceUsage: true,
	}
	cmd.Flags().BoolP("watch", "w", false, "redraw saved workflow state until it is cleared")
	cmd.Flags().Duration("interval", time.Second, "refresh interval for --watch")
	return cmd
}

func newTestWorkflowInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "init <name>",
		Args:         cobra.ExactArgs(1),
		RunE:         initWorkflow,
		SilenceUsage: true,
	}
	cmd.Flags().String("template", "ship", "workflow template to copy")
	return cmd
}

func writeTestConfig(t *testing.T) {
	t.Helper()
	var yaml string
	if runtime.GOOS == "windows" {
		yaml = `default: test
profiles:
  test:
    command: cmd
    args: ["/c", "exit 0"]
  failing:
    command: cmd
    args: ["/c", "exit 1"]
`
	} else {
		yaml = `default: test
profiles:
  test:
    command: "true"
    args: []
  failing:
    command: "false"
    args: []
`
	}
	if err := os.WriteFile(".brr.yaml", []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeCycleConfig(t *testing.T) {
	t.Helper()
	var yaml string
	if runtime.GOOS == "windows" {
		yaml = `default: test
profiles:
  test:
    command: cmd
    args: ["/c", "echo again > .brr-cycle"]
`
	} else {
		yaml = `default: test
profiles:
  test:
    command: "touch"
    args: [".brr-cycle"]
`
	}
	if err := os.WriteFile(".brr.yaml", []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeWorkflow(t *testing.T) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(".brr", "workflows"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(".brr", "workflows", "ship.yaml"), []byte(`version: 2
stages:
  - id: check
    type: command
    command: `+commandYAML()+`
`), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commandYAML() string {
	if runtime.GOOS == "windows" {
		return `["cmd", "/c", "exit 0"]`
	}
	return `["true"]`
}

func TestRunIntegrationSuccess(t *testing.T) {
	t.Chdir(t.TempDir())
	writeTestConfig(t)

	cmd := newTestRootCmd()
	cmd.SetArgs([]string{"hello world", "-m", "1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunIntegrationMultipleIterations(t *testing.T) {
	t.Chdir(t.TempDir())
	writeTestConfig(t)

	cmd := newTestRootCmd()
	cmd.SetArgs([]string{"hello world", "-m", "3"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunIntegrationExplicitProfile(t *testing.T) {
	t.Chdir(t.TempDir())
	writeTestConfig(t)

	cmd := newTestRootCmd()
	cmd.SetArgs([]string{"hello world", "-m", "1", "-p", "test"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunIntegrationBadProfile(t *testing.T) {
	t.Chdir(t.TempDir())
	writeTestConfig(t)

	cmd := newTestRootCmd()
	cmd.SetArgs([]string{"hello world", "-m", "1", "-p", "nonexistent"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent profile")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

func TestRunIntegrationMissingConfig(t *testing.T) {
	t.Chdir(t.TempDir())

	cmd := newTestRootCmd()
	cmd.SetArgs([]string{"hello world", "-m", "1"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when no config exists")
	}
	if !strings.Contains(err.Error(), "no config found") {
		t.Errorf("expected 'no config found' error, got: %v", err)
	}
}

func TestRunIntegrationEmptyPrompt(t *testing.T) {
	t.Chdir(t.TempDir())
	writeTestConfig(t)

	cmd := newTestRootCmd()
	cmd.SetArgs([]string{"   ", "-m", "1"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for whitespace-only prompt")
	}
	if !strings.Contains(err.Error(), "prompt is empty") {
		t.Errorf("expected 'prompt is empty' error, got: %v", err)
	}
}

func TestRunIntegrationNegativeMax(t *testing.T) {
	t.Chdir(t.TempDir())
	writeTestConfig(t)

	cmd := newTestRootCmd()
	cmd.SetArgs([]string{"hello", "-m", "-1"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for negative max")
	}
	if !strings.Contains(err.Error(), "--max must be >= 0") {
		t.Errorf("expected '--max must be >= 0' error, got: %v", err)
	}
}

func TestRunIntegrationFailingCommand(t *testing.T) {
	t.Chdir(t.TempDir())
	writeTestConfig(t)

	cmd := newTestRootCmd()
	cmd.SetArgs([]string{"hello", "-m", "10", "-p", "failing"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for failing command")
	}
	if !strings.Contains(err.Error(), "consecutive failures") {
		t.Errorf("expected consecutive failures error, got: %v", err)
	}
}

func TestRunIntegrationPromptFromFile(t *testing.T) {
	t.Chdir(t.TempDir())
	writeTestConfig(t)

	if err := os.WriteFile("task.md", []byte("do the thing"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newTestRootCmd()
	cmd.SetArgs([]string{"task.md", "-m", "1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunIntegrationCycleSignalWithoutWorkflowErrors(t *testing.T) {
	t.Chdir(t.TempDir())
	writeCycleConfig(t)

	cmd := newTestRootCmd()
	cmd.SetArgs([]string{"hello", "-m", "1"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for .brr-cycle outside workflow")
	}
	if !strings.Contains(err.Error(), "only supported by 'brr workflow'") {
		t.Fatalf("expected workflow-only cycle error, got: %v", err)
	}
}

func TestRunIntegrationNoArgs(t *testing.T) {
	t.Chdir(t.TempDir())
	writeTestConfig(t)

	cmd := newTestRootCmd()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing args")
	}
}

func TestRunIntegrationNotifyFlag(t *testing.T) {
	t.Chdir(t.TempDir())
	writeTestConfig(t)

	cmd := newTestRootCmd()
	cmd.SetArgs([]string{"hello world", "-m", "1", "-n"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error with --notify: %v", err)
	}
}

func TestWorkflowValidateIntegration(t *testing.T) {
	t.Chdir(t.TempDir())
	writeTestConfig(t)
	writeWorkflow(t)

	cmd := newTestWorkflowValidateCmd()
	cmd.SetArgs([]string{"ship"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestWorkflowValidateRejectsEmptyPrompt(t *testing.T) {
	t.Chdir(t.TempDir())
	writeTestConfig(t)
	if err := os.MkdirAll(filepath.Join(".brr", "workflows"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(".brr", "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(".brr", "prompts", "build.md"), []byte("   \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wf := "version: 2\ndefaults: {max: 1}\nstages:\n  - id: build\n    type: agent\n    prompt: build\n"
	if err := os.WriteFile(filepath.Join(".brr", "workflows", "ship.yaml"), []byte(wf), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newTestWorkflowValidateCmd()
	cmd.SetArgs([]string{"ship"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected validate to reject an empty resolved stage prompt")
	}
	if !strings.Contains(err.Error(), "prompt is empty") {
		t.Fatalf("expected 'prompt is empty' error, got: %v", err)
	}
}

func TestWorkflowValidateRejectsAbsolutePrompt(t *testing.T) {
	t.Chdir(t.TempDir())
	writeTestConfig(t)
	secret := filepath.Join(t.TempDir(), "secret.md")
	if err := os.WriteFile(secret, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(".brr", "workflows"), 0o755); err != nil {
		t.Fatal(err)
	}
	wf := fmt.Sprintf("version: 2\ndefaults: {max: 1}\nstages:\n  - id: build\n    type: agent\n    prompt: %q\n", secret)
	if err := os.WriteFile(filepath.Join(".brr", "workflows", "ship.yaml"), []byte(wf), 0o644); err != nil {
		t.Fatal(err)
	}

	// `brr workflow validate` reads the stage prompt today, so the restriction
	// must also close the file-existence oracle it would otherwise expose.
	cmd := newTestWorkflowValidateCmd()
	cmd.SetArgs([]string{"ship"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected validate to reject an absolute stage prompt path")
	}
	if !strings.Contains(err.Error(), "working tree") {
		t.Fatalf("expected working-tree restriction error, got: %v", err)
	}
}

func TestWorkflowRunIntegration(t *testing.T) {
	t.Chdir(t.TempDir())
	writeTestConfig(t)
	writeWorkflow(t)

	cmd := newTestWorkflowRunCmd()
	cmd.SetArgs([]string{"ship"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected workflow run error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(".brr", "state", "workflows", "ship.json")); !os.IsNotExist(err) {
		t.Fatalf("expected workflow state to be cleared on success, got: %v", err)
	}
	if _, err := os.Stat(filepath.Join(".brr", "state", "workflows", "ship.events.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("expected workflow events to be cleared on success, got: %v", err)
	}
}

func newTestWorkflowResetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "reset <name>",
		Args:         cobra.ExactArgs(1),
		RunE:         resetWorkflow,
		SilenceUsage: true,
	}
	return cmd
}

func TestWorkflowResetIntegration(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(filepath.Join(".brr", "state", "workflows"), 0o755); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(".brr", "state", "workflows", "ship.json")
	eventsPath := filepath.Join(".brr", "state", "workflows", "ship.events.jsonl")
	if err := os.WriteFile(statePath, []byte(`{"schema_version":2,"workflow":"ship","run_id":"abc","next_stage_id":"check"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(eventsPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newTestWorkflowResetCmd()
	cmd.SetArgs([]string{"ship"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected reset error: %v", err)
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("expected state to be removed, got: %v", err)
	}
	if _, err := os.Stat(eventsPath); !os.IsNotExist(err) {
		t.Fatalf("expected events to be removed, got: %v", err)
	}
}

func TestWorkflowStatusIntegration(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(filepath.Join(".brr", "state", "workflows"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(".brr", "state", "workflows", "ship.json"), []byte(`{
  "schema_version": 2,
  "workflow": "ship",
  "run_id": "abc",
  "started_at": "2024-01-01T00:00:00Z",
  "updated_at": "2024-01-01T00:00:00Z",
  "next_stage_id": "check",
  "stages": [{"id": "check", "type": "command", "status": "pending"}]
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newTestWorkflowStatusCmd()
	cmd.SetArgs([]string{"ship"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected status error: %v", err)
	}
}

func TestWorkflowInitIntegration(t *testing.T) {
	t.Chdir(t.TempDir())

	cmd := newTestWorkflowInitCmd()
	cmd.SetArgs([]string{"ship"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected init error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(".brr", "workflows", "ship.yaml")); err != nil {
		t.Fatalf("expected workflow file: %v", err)
	}
}

func TestSetVersionFormat(t *testing.T) {
	SetVersion("1.2.3", "abc123")
	expected := "1.2.3 (abc123)"
	if rootCmd.Version != expected {
		t.Errorf("expected version %q, got %q", expected, rootCmd.Version)
	}
}

func TestPrintConfigFormatting(t *testing.T) {
	// Capture stderr
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	printConfig("task", "claude", []string{"claude", "-p"}, 0)

	w.Close()
	os.Stderr = old

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	if !strings.Contains(output, "task") {
		t.Error("expected prompt name in output")
	}
	if !strings.Contains(output, "claude") {
		t.Error("expected profile name in output")
	}
	if !strings.Contains(output, "unlimited") {
		t.Error("expected 'unlimited' for max=0")
	}
}

func TestPrintConfigWithMax(t *testing.T) {
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	printConfig("task", "claude", []string{"claude"}, 5)

	w.Close()
	os.Stderr = old

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	if !strings.Contains(output, fmt.Sprintf("%d", 5)) {
		t.Error("expected max count in output")
	}
}
