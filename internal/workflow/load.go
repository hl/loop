package workflow

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/hl/brr/internal/config"
	"github.com/hl/brr/internal/fsutil"
)

// maxWorkflowFileSize bounds how much of a workflow YAML file brr will read.
// Workflows are short stage lists; 1 MiB is generous while preventing a planted
// oversized file from exhausting memory.
const maxWorkflowFileSize = 1 << 20 // 1 MiB

func Load(data []byte) (Workflow, error) {
	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return Workflow{}, fmt.Errorf("parsing workflow: %w", err)
	}
	if _, ok := raw["version"]; !ok {
		return Workflow{}, fmt.Errorf("workflow schema version is required; legacy unversioned workflows are no longer supported (add version: 2 and use stages with id/type plus cycle.target)")
	}

	var wf Workflow
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&wf); err != nil {
		return wf, fmt.Errorf("parsing workflow: %w", err)
	}
	return wf, validate(wf)
}

func Resolve(name string) ([]byte, error) {
	if err := validateName(name); err != nil {
		return nil, err
	}

	projectPath := filepath.Join(".brr", "workflows", name+".yaml")
	if data, err := fsutil.ReadRegularFileCapped(projectPath, maxWorkflowFileSize); err == nil {
		return data, nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading %s: %w", projectPath, err)
	}

	configHint := "<config-dir>/brr/workflows/" + name + ".yaml"
	if configDir, err := os.UserConfigDir(); err == nil {
		userPath := filepath.Join(configDir, "brr", "workflows", name+".yaml")
		configHint = userPath
		if data, err := fsutil.ReadRegularFileCapped(userPath, maxWorkflowFileSize); err == nil {
			return data, nil
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("reading %s: %w", userPath, err)
		}
	}

	return nil, fmt.Errorf("workflow %q not found (looked in %s and %s)", name, projectPath, configHint)
}

func ValidateRuntime(wf Workflow, cfg config.Config, profileFlag string, resolvePrompt func(string) (string, error)) error {
	if _, _, err := resolveWorkflowProfile(wf.Defaults.Profile, profileFlag, cfg); err != nil {
		return fmt.Errorf("defaults.profile: %w", err)
	}
	for _, stage := range wf.Stages {
		switch stage.Type {
		case StageTypeAgent:
			if _, _, err := resolveStageProfile(stage, wf, profileFlag, cfg); err != nil {
				return fmt.Errorf("stages.%s.profile: %w", stage.ID, err)
			}
			if resolvePrompt != nil {
				if _, err := resolvePrompt(stage.Prompt); err != nil {
					return fmt.Errorf("stages.%s.prompt: %w", stage.ID, err)
				}
			}
		case StageTypeCommand:
			if _, err := exec.LookPath(stage.Command[0]); err != nil {
				return fmt.Errorf("stages.%s.command: %w", stage.ID, err)
			}
		}
	}
	return nil
}

func validate(wf Workflow) error {
	if wf.Version != SchemaVersion {
		return fmt.Errorf("workflow schema version must be %d, got %d", SchemaVersion, wf.Version)
	}
	if len(wf.Stages) == 0 {
		return fmt.Errorf("stages: at least one stage is required")
	}
	if wf.Defaults.Max < 0 {
		return fmt.Errorf("defaults.max must be >= 0, got %d", wf.Defaults.Max)
	}
	seen := make(map[string]bool)
	for i, stage := range wf.Stages {
		path := fmt.Sprintf("stages[%d]", i)
		if strings.TrimSpace(stage.ID) == "" {
			return fmt.Errorf("%s.id is required", path)
		}
		if strings.ContainsAny(stage.ID, `/\`) || strings.Contains(stage.ID, "..") {
			return fmt.Errorf("%s.id %q is invalid", path, stage.ID)
		}
		if seen[stage.ID] {
			return fmt.Errorf("%s.id %q is duplicated", path, stage.ID)
		}
		seen[stage.ID] = true
		if stage.Type != StageTypeAgent && stage.Type != StageTypeCommand {
			return fmt.Errorf("%s.type must be %q or %q", path, StageTypeAgent, StageTypeCommand)
		}
		if err := validateStage(wf, stage, path); err != nil {
			return err
		}
	}
	return validateCycle(wf, seen)
}

func validateStage(wf Workflow, stage Stage, path string) error {
	switch stage.Type {
	case StageTypeAgent:
		if strings.TrimSpace(stage.Prompt) == "" {
			return fmt.Errorf("%s.prompt is required for agent stages", path)
		}
		if len(stage.Command) > 0 {
			return fmt.Errorf("%s.command is only valid for command stages", path)
		}
		// A negative max is neither "unset" (which falls back to defaults.max) nor a
		// valid iteration count, so reject it explicitly rather than silently
		// substituting the default.
		if stage.Max < 0 {
			return fmt.Errorf("%s.max must be >= 1 for agent stages, got %d", path, stage.Max)
		}
		if effectiveMax(wf, stage) < 1 {
			return fmt.Errorf("%s.max must be >= 1 for agent stages", path)
		}
	case StageTypeCommand:
		if len(stage.Command) == 0 || strings.TrimSpace(stage.Command[0]) == "" {
			return fmt.Errorf("%s.command must be a non-empty argv array", path)
		}
		if strings.TrimSpace(stage.Prompt) != "" {
			return fmt.Errorf("%s.prompt is only valid for agent stages", path)
		}
		if stage.Max != 0 {
			return fmt.Errorf("%s.max is only valid for agent stages", path)
		}
		if strings.TrimSpace(stage.Profile) != "" {
			return fmt.Errorf("%s.profile is only valid for agent stages", path)
		}
	}
	return nil
}

func validateCycle(wf Workflow, stageIDs map[string]bool) error {
	if wf.Cycle == nil {
		return nil
	}
	if strings.TrimSpace(wf.Cycle.Target) == "" {
		return fmt.Errorf("cycle.target is required")
	}
	if !stageIDs[wf.Cycle.Target] {
		return fmt.Errorf("cycle.target %q does not match any stage id", wf.Cycle.Target)
	}
	if wf.Cycle.Max < 1 {
		return fmt.Errorf("cycle.max must be >= 1, got %d", wf.Cycle.Max)
	}
	return nil
}

func effectiveMax(wf Workflow, stage Stage) int {
	if stage.Max > 0 {
		return stage.Max
	}
	return wf.Defaults.Max
}

func resolveWorkflowProfile(stageProfile, flagProfile string, cfg config.Config) ([]string, string, error) {
	name := stageProfile
	if name == "" {
		name = flagProfile
	}
	return cfg.ResolveProfile(name)
}

func resolveStageProfile(stage Stage, wf Workflow, flagProfile string, cfg config.Config) ([]string, string, error) {
	name := stage.Profile
	if name == "" {
		name = flagProfile
	}
	if name == "" {
		name = wf.Defaults.Profile
	}
	return cfg.ResolveProfile(name)
}

func validateName(name string) error {
	if strings.TrimSpace(name) == "" || strings.Contains(name, "..") || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("invalid workflow name: %q", name)
	}
	return nil
}
