package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadNoConfig(t *testing.T) {
	t.Chdir(t.TempDir())

	_, err := Load()
	if err == nil {
		t.Error("expected error when no config exists")
	}
}

func TestLoadWithProfiles(t *testing.T) {
	t.Chdir(t.TempDir())

	yaml := `default: myagent
profiles:
  myagent:
    command: myagent
    args: [--fast, --no-confirm]
  other:
    command: other
    args: [--verbose]
`
	if err := os.WriteFile(".brr.yaml", []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Default != "myagent" {
		t.Errorf("expected default 'myagent', got %q", cfg.Default)
	}
	if len(cfg.Profiles) != 2 {
		t.Errorf("expected 2 profiles, got %d", len(cfg.Profiles))
	}

	p := cfg.Profiles["myagent"]
	if p.Command != "myagent" {
		t.Errorf("expected command 'myagent', got %q", p.Command)
	}
	if len(p.Args) != 2 || p.Args[0] != "--fast" {
		t.Errorf("expected args [--fast, --no-confirm], got %v", p.Args)
	}
}

func TestLoadProfileNamesCaseInsensitive(t *testing.T) {
	t.Chdir(t.TempDir())

	// viper lowercases map keys, so an uppercase `default` and `-p` value must
	// still reach a mixed-case profile.
	yaml := `default: MyAgent
profiles:
  MyAgent:
    command: myagent
    args: [--fast]
`
	if err := os.WriteFile(".brr.yaml", []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// Default profile resolves despite the uppercase name.
	cmd, name, err := cfg.ResolveProfile("")
	if err != nil {
		t.Fatalf("resolving default profile: %v", err)
	}
	if cmd[0] != "myagent" {
		t.Errorf("expected command 'myagent', got %q", cmd[0])
	}
	if name != "myagent" {
		t.Errorf("expected resolved name 'myagent', got %q", name)
	}

	// A mixed-case `-p` value resolves to the same profile.
	if _, _, err := cfg.ResolveProfile("MYAGENT"); err != nil {
		t.Errorf("expected mixed-case profile lookup to succeed, got: %v", err)
	}
}

func TestLoadProjectConfigSymlinkRejected(t *testing.T) {
	t.Chdir(t.TempDir())

	target := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(target, []byte(`default: linked
profiles:
  linked:
    command: linked
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, ".brr.yaml"); err != nil {
		t.Skip("symlinks not supported")
	}

	_, err := Load()
	if err == nil {
		t.Fatal("expected symlinked project config to be rejected")
	}
	if !strings.Contains(err.Error(), ".brr.yaml") {
		t.Errorf("expected error to mention .brr.yaml, got: %v", err)
	}
}

func TestLoadProjectConfigDirectoryRejected(t *testing.T) {
	t.Chdir(t.TempDir())

	if err := os.Mkdir(".brr.yaml", 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := Load()
	if err == nil {
		t.Fatal("expected directory project config to be rejected")
	}
	if !strings.Contains(err.Error(), ".brr.yaml") {
		t.Errorf("expected error to mention .brr.yaml, got: %v", err)
	}
}

func TestLoadNoProfiles(t *testing.T) {
	t.Chdir(t.TempDir())

	if err := os.WriteFile(".brr.yaml", []byte("default: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load()
	if err == nil {
		t.Error("expected error when no profiles defined")
	}
}

func TestLoadNoDefault(t *testing.T) {
	t.Chdir(t.TempDir())

	yaml := `profiles:
  claude:
    command: claude
    args: [-p]
`
	if err := os.WriteFile(".brr.yaml", []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load()
	if err == nil {
		t.Error("expected error when no default set")
	}
}

func TestLoadDefaultProfileNotFound(t *testing.T) {
	t.Chdir(t.TempDir())

	yaml := `default: nonexistent
profiles:
  claude:
    command: claude
    args: [-p]
`
	if err := os.WriteFile(".brr.yaml", []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load()
	if err == nil {
		t.Error("expected error when default profile not in profiles map")
	}
}

func TestResolveProfileDefault(t *testing.T) {
	cfg := Config{
		Default: "claude",
		Profiles: map[string]Profile{
			"claude": {Command: "claude", Args: []string{"-p", "--model", "sonnet"}},
		},
	}

	cmd, name, err := cfg.ResolveProfile("")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if name != "claude" {
		t.Errorf("expected name 'claude', got %q", name)
	}
	if cmd[0] != "claude" || len(cmd) != 4 {
		t.Errorf("unexpected command: %v", cmd)
	}
}

func TestResolveProfileExplicit(t *testing.T) {
	cfg := Config{
		Default: "claude",
		Profiles: map[string]Profile{
			"claude": {Command: "claude", Args: []string{"-p"}},
			"codex":  {Command: "codex", Args: []string{"exec"}},
		},
	}

	cmd, name, err := cfg.ResolveProfile("codex")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if name != "codex" {
		t.Errorf("expected name 'codex', got %q", name)
	}
	if cmd[0] != "codex" {
		t.Errorf("expected command 'codex', got %q", cmd[0])
	}
}

func TestResolveProfileNotFound(t *testing.T) {
	cfg := Config{
		Default:  "claude",
		Profiles: map[string]Profile{"claude": {Command: "claude"}},
	}

	_, _, err := cfg.ResolveProfile("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent profile")
	}
}

func TestResolveProfileEmptyCommand(t *testing.T) {
	cfg := Config{
		Default:  "broken",
		Profiles: map[string]Profile{"broken": {Command: "", Args: []string{"-p"}}},
	}

	_, _, err := cfg.ResolveProfile("")
	if err == nil {
		t.Error("expected error for empty command")
	}
}
