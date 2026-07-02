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

// writeGlobalConfig points os.UserConfigDir at a temp HOME and writes a global
// brr config there, returning the project working directory to chdir into.
func writeGlobalConfig(t *testing.T, globalYAML string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("UserConfigDir: %v", err)
	}
	brrDir := filepath.Join(configDir, "brr")
	if err := os.MkdirAll(brrDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(brrDir, "config.yaml"), []byte(globalYAML), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadProjectProfileDoesNotInheritGlobalArgs(t *testing.T) {
	writeGlobalConfig(t, `default: claude
profiles:
  claude:
    command: claude
    args: [--dangerously-skip-permissions, --model, opus]
`)

	t.Chdir(t.TempDir())
	// The project profile sets a command but no args; it must NOT inherit the
	// global profile's dangerous args.
	if err := os.WriteFile(".brr.yaml", []byte(`profiles:
  claude:
    command: echo
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	cmd, _, err := cfg.ResolveProfile("claude")
	if err != nil {
		t.Fatalf("resolving claude: %v", err)
	}
	if len(cmd) != 1 || cmd[0] != "echo" {
		t.Errorf("expected project profile to replace global atomically ([echo]), got %v", cmd)
	}
}

func TestLoadProjectProfileMergesAtomicallyWithDistinctGlobal(t *testing.T) {
	writeGlobalConfig(t, `default: claude
profiles:
  claude:
    command: claude
    args: [--global-only]
`)

	t.Chdir(t.TempDir())
	// Project adds a new profile and does not touch the global one.
	if err := os.WriteFile(".brr.yaml", []byte(`profiles:
  local:
    command: local
    args: [--local]
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// Global default and profile survive.
	if cfg.Default != "claude" {
		t.Errorf("expected default 'claude', got %q", cfg.Default)
	}
	claude, _, err := cfg.ResolveProfile("claude")
	if err != nil || len(claude) != 2 || claude[1] != "--global-only" {
		t.Errorf("expected global claude profile intact, got %v (err %v)", claude, err)
	}
	// Project profile is available.
	local, _, err := cfg.ResolveProfile("local")
	if err != nil || local[0] != "local" {
		t.Errorf("expected project 'local' profile, got %v (err %v)", local, err)
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

func TestLoadRejectsOversizedConfig(t *testing.T) {
	t.Chdir(t.TempDir())

	// A planted, oversized .brr.yaml must be rejected rather than read whole.
	big := make([]byte, maxConfigFileSize+1)
	for i := range big {
		big[i] = 'a'
	}
	if err := os.WriteFile(".brr.yaml", big, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for oversized config file")
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
