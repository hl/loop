package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadValidWorkflowV2(t *testing.T) {
	wf, err := Load([]byte(`
version: 2
description: ship it
defaults:
  profile: claude
  max: 3
cycle:
  target: build
  max: 2
stages:
  - id: spec
    type: agent
    prompt: spec
  - id: build
    type: agent
    prompt: build
    max: 10
  - id: check
    type: command
    command: ["true"]
`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wf.Version != 2 {
		t.Fatalf("expected version 2, got %d", wf.Version)
	}
	if wf.Cycle == nil || wf.Cycle.Target != "build" || wf.Cycle.Max != 2 {
		t.Fatalf("unexpected cycle config: %#v", wf.Cycle)
	}
	if got := effectiveMax(wf, wf.Stages[0]); got != 3 {
		t.Fatalf("expected default max 3, got %d", got)
	}
}

func TestLoadRejectsLegacyWorkflow(t *testing.T) {
	_, err := Load([]byte(`
stages:
  - prompt: build
    max: 10
    cycle: true
max_cycles: 3
`))
	if err == nil {
		t.Fatal("expected legacy schema error")
	}
	if !strings.Contains(err.Error(), "legacy unversioned workflows are no longer supported") {
		t.Fatalf("expected migration-style error, got: %v", err)
	}
}

func TestLoadValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "duplicate id",
			yaml: `
version: 2
defaults: {max: 1}
stages:
  - id: build
    type: agent
    prompt: build
  - id: build
    type: command
    command: ["true"]
`,
			want: "duplicated",
		},
		{
			name: "unknown type",
			yaml: `
version: 2
stages:
  - id: build
    type: magic
`,
			want: "type must be",
		},
		{
			name: "missing prompt",
			yaml: `
version: 2
defaults: {max: 1}
stages:
  - id: build
    type: agent
`,
			want: "prompt is required",
		},
		{
			name: "command is shell string",
			yaml: `
version: 2
stages:
  - id: check
    type: command
    command: []
`,
			want: "argv array",
		},
		{
			name: "negative stage max",
			yaml: `
version: 2
defaults: {max: 1}
stages:
  - id: build
    type: agent
    prompt: build
    max: -1
`,
			want: "max must be >= 1",
		},
		{
			name: "bad cycle target",
			yaml: `
version: 2
defaults: {max: 1}
cycle:
  target: missing
  max: 1
stages:
  - id: build
    type: agent
    prompt: build
`,
			want: "cycle.target",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load([]byte(tt.yaml))
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %q in error, got: %v", tt.want, err)
			}
		})
	}
}

func TestResolveProjectWorkflowSymlinkRejected(t *testing.T) {
	t.Chdir(t.TempDir())
	dir := filepath.Join(".brr", "workflows")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "example.yaml")
	if err := os.WriteFile(target, []byte("version: 2\nstages: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "example.yaml")); err != nil {
		t.Skip("symlinks not supported")
	}
	_, err := Resolve("example")
	if err == nil {
		t.Fatal("expected symlinked workflow to be rejected")
	}
}

func TestValidateRuntimeProfileAndPrompt(t *testing.T) {
	wf := testWorkflow([]Stage{{ID: "build", Type: StageTypeAgent, Prompt: "build", Profile: "missing", Max: 1}}, nil)
	err := ValidateRuntime(wf, testConfig(echoCmd()), "", func(string) (string, error) {
		return "go", nil
	})
	if err == nil {
		t.Fatal("expected missing profile error")
	}
	if !strings.Contains(err.Error(), "profile") {
		t.Fatalf("expected profile error, got: %v", err)
	}
}

func TestResolveStageProfileUsesWorkflowDefault(t *testing.T) {
	wf := testWorkflow([]Stage{{ID: "build", Type: StageTypeAgent, Prompt: "build", Max: 1}}, nil)
	wf.Defaults.Profile = "special"
	cfg := testConfig(echoCmd())
	cfg.Profiles["special"] = cfg.Profiles["test"]

	_, resolved, err := resolveStageProfile(wf.Stages[0], wf, "", cfg)
	if err != nil {
		t.Fatalf("unexpected profile error: %v", err)
	}
	if resolved != "special" {
		t.Fatalf("expected workflow default profile, got %q", resolved)
	}
}

func TestResolveStageProfileFlagOverridesWorkflowDefault(t *testing.T) {
	wf := testWorkflow([]Stage{{ID: "build", Type: StageTypeAgent, Prompt: "build", Max: 1}}, nil)
	wf.Defaults.Profile = "special"
	cfg := testConfig(echoCmd())
	cfg.Profiles["special"] = cfg.Profiles["test"]

	_, resolved, err := resolveStageProfile(wf.Stages[0], wf, "test", cfg)
	if err != nil {
		t.Fatalf("unexpected profile error: %v", err)
	}
	if resolved != "test" {
		t.Fatalf("expected CLI profile override, got %q", resolved)
	}
}
