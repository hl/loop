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
	var cfg Config
	found := false

	v := viper.New()
	v.SetConfigType("yaml")

	// Layer 1: user global config
	if configDir, err := os.UserConfigDir(); err == nil {
		globalPath := filepath.Join(configDir, "brr", "config.yaml")
		v.SetConfigFile(globalPath)
		if err := v.MergeInConfig(); err == nil {
			found = true
		} else if !isConfigNotFound(err) {
			return cfg, fmt.Errorf("reading %s: %w", globalPath, err)
		}
	}

	// Layer 2: project config. Read through fsutil so project config never
	// follows symlinks or other non-regular files.
	if data, err := fsutil.ReadRegularFile(".brr.yaml"); err == nil {
		if err := v.MergeConfig(bytes.NewReader(data)); err != nil {
			return cfg, fmt.Errorf("reading .brr.yaml: %w", err)
		}
		found = true
	} else if !isConfigNotFound(err) {
		return cfg, fmt.Errorf("reading .brr.yaml: %w", err)
	}

	if !found {
		configHint := "<config-dir>/brr/config.yaml"
		if configDir, err := os.UserConfigDir(); err == nil {
			configHint = filepath.Join(configDir, "brr", "config.yaml")
		}
		return cfg, fmt.Errorf("no config found (looked in .brr.yaml and %s) — run 'brr init'", configHint)
	}

	if err := v.Unmarshal(&cfg); err != nil {
		return cfg, err
	}

	// Profile names are matched case-insensitively. viper already lowercases
	// map keys internally, so the default name and every lookup must be
	// lowercased to reach an uppercase or mixed-case profile (e.g. `default:
	// MyAgent` with `profiles: { MyAgent: ... }`).
	cfg.Default = strings.ToLower(cfg.Default)

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
