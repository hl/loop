# Configuration

## Purpose

Configuration defines how brr discovers, loads, and validates its settings. It provides a layered config system where project-local settings override user-global defaults, and exposes named profiles that map to executable commands. This is the contract between the user's `.brr.yaml` files and the rest of the system.

## Requirements

1. brr loads configuration from two layers: a user-global config directory and a project-local `.brr.yaml` file. Project-local settings take precedence over global settings.
2. The global config path follows OS conventions: `~/.config/brr/config.yaml` on Linux, `~/Library/Application Support/brr/config.yaml` on macOS, `%AppData%\brr\config.yaml` on Windows.
3. At least one config source must exist. If neither global nor project-local config is found, brr reports an error identifying both searched paths.
4. A valid config must contain a `default` key naming the default profile, and a `profiles` map with at least one entry.
5. The default profile name must reference a profile that exists in the `profiles` map. Profile names are matched case-insensitively (`default: MyAgent` resolves `profiles.myagent`).
6. Each profile must have a non-empty `command` field and an optional `args` list of strings.
7. When resolving a profile by name, the system returns the full command as `[command] + args`. An empty profile name resolves to the default profile. Name matching is case-insensitive, so `-p MyAgent` and `-p myagent` select the same profile.
8. Requesting a profile that does not exist in the `profiles` map produces an error naming the missing profile.
9. Error messages for config loading failures reference the specific config file path. Validation errors identify the invalid field but not the source file, since settings are merged from multiple sources.

## Constraints

- Config parsing must not introduce dependencies beyond the existing viper/cobra stack.
- Config loading must not follow symlinks or read non-regular files for the project-local config path.
- The config format is YAML only.

## Dependencies

- No dependencies on other specs.
- Uses viper for YAML parsing and config merging.

## Acceptance Criteria

- [ ] Project-local config overrides global config values.
- [ ] Missing config produces a clear error with both searched paths.
- [ ] Invalid config (missing default, empty profiles, dangling default reference) produces specific validation errors.
- [ ] Profile resolution returns the correct command slice for both explicit and default profiles.
- [ ] Project-local `.brr.yaml` symlinks and other non-regular files are rejected.
- [ ] Loading error messages include the relevant file path; validation errors identify the invalid field.
- [ ] All requirements have corresponding tests that pass.
- [ ] Existing tests continue to pass.
