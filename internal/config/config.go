package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hl/brr/internal/fsutil"
	"github.com/spf13/viper"
)

// Profile defines a named agent configuration.
type Profile struct {
	Command string   `mapstructure:"command"`
	Args    []string `mapstructure:"args"`
}

// Config holds all brr configuration.
type Config struct {
	Default  string             `mapstructure:"default"`
	Profiles map[string]Profile `mapstructure:"profiles"`
}

// Load reads config from files and returns a merged Config.
// Priority: .brr.yaml > ~/.config/brr/config.yaml.
// Returns an error if no config is found or if a config file is malformed.
func Load() (Config, error) {
	var global, project Config
	globalFound, projectFound := false, false

	// Layer 1: user global config. Parse into its own Config so profiles stay
	// distinct from the project layer and can be replaced as atomic units.
	if configDir, err := os.UserConfigDir(); err == nil {
		globalPath := filepath.Join(configDir, "brr", "config.yaml")
		v := viper.New()
		v.SetConfigType("yaml")
		v.SetConfigFile(globalPath)
		if err := v.ReadInConfig(); err == nil {
			if err := v.Unmarshal(&global); err != nil {
				return Config{}, fmt.Errorf("reading %s: %w", globalPath, err)
			}
			globalFound = true
		} else if !isConfigNotFound(err) {
			return Config{}, fmt.Errorf("reading %s: %w", globalPath, err)
		}
	}

	// Layer 2: project config. Read through fsutil so project config never
	// follows symlinks or other non-regular files, then parse it into its own
	// Config so a project profile does not inherit fields it omits.
	if data, err := fsutil.ReadRegularFile(".brr.yaml"); err == nil {
		v := viper.New()
		v.SetConfigType("yaml")
		if err := v.ReadConfig(bytes.NewReader(data)); err != nil {
			return Config{}, fmt.Errorf("reading .brr.yaml: %w", err)
		}
		if err := v.Unmarshal(&project); err != nil {
			return Config{}, fmt.Errorf("reading .brr.yaml: %w", err)
		}
		projectFound = true
	} else if !isConfigNotFound(err) {
		return Config{}, fmt.Errorf("reading .brr.yaml: %w", err)
	}

	if !globalFound && !projectFound {
		configHint := "<config-dir>/brr/config.yaml"
		if configDir, err := os.UserConfigDir(); err == nil {
			configHint = filepath.Join(configDir, "brr", "config.yaml")
		}
		return Config{}, fmt.Errorf("no config found (looked in .brr.yaml and %s) — run 'brr init'", configHint)
	}

	cfg := mergeConfigs(global, project)

	if len(cfg.Profiles) == 0 {
		return cfg, fmt.Errorf("no profiles defined in config — add at least one profile to your config file")
	}

	if cfg.Default == "" {
		return cfg, fmt.Errorf("no default profile set in config — add 'default: <name>' to your config file")
	}

	if _, ok := cfg.Profiles[cfg.Default]; !ok {
		return cfg, fmt.Errorf("default profile %q not found in profiles", cfg.Default)
	}

	return cfg, nil
}

// mergeConfigs overlays project settings onto global ones. Profiles are merged
// as ATOMIC units: a project profile replaces the whole same-named global
// profile, so a project profile that sets `command` but omits `args` does not
// silently inherit the global profile's args. Profile names are matched
// case-insensitively (viper lowercases config map keys), and the project
// `default` wins when set.
func mergeConfigs(global, project Config) Config {
	merged := Config{
		Profiles: make(map[string]Profile, len(global.Profiles)+len(project.Profiles)),
	}
	for name, p := range global.Profiles {
		merged.Profiles[strings.ToLower(name)] = p
	}
	for name, p := range project.Profiles {
		merged.Profiles[strings.ToLower(name)] = p
	}
	merged.Default = global.Default
	if project.Default != "" {
		merged.Default = project.Default
	}
	merged.Default = strings.ToLower(merged.Default)
	return merged
}

// isConfigNotFound returns true if the error indicates the config file doesn't exist.
func isConfigNotFound(err error) bool {
	var notFound viper.ConfigFileNotFoundError
	if errors.As(err, &notFound) {
		return true
	}
	return os.IsNotExist(err)
}

// ResolveProfile returns the command slice for the given profile name.
// If profileName is empty, the default profile is used.
func (c Config) ResolveProfile(profileName string) ([]string, string, error) {
	name := profileName
	if name == "" {
		name = c.Default
	}
	// Profile names are case-insensitive; viper stores keys lowercased, so
	// normalize the requested name (including the `-p` flag value) to match.
	name = strings.ToLower(name)

	p, ok := c.Profiles[name]
	if !ok {
		available := make([]string, 0, len(c.Profiles))
		for k := range c.Profiles {
			available = append(available, k)
		}
		sort.Strings(available)
		return nil, name, fmt.Errorf("profile %q not found (available: %v)", name, available)
	}

	if p.Command == "" {
		return nil, name, fmt.Errorf("profile %q has no command", name)
	}

	return append([]string{p.Command}, p.Args...), name, nil
}
