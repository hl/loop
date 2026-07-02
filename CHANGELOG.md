# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Fixed

- The fail-streak circuit breaker is no longer permanently disabled by a dirty working tree. The engine now snapshots the tree before each iteration and only resets the streak when the tree actually *changed* during a failing iteration (real progress). A tree that is merely dirty but unchanged — the normal state when running a coding agent — now counts toward the streak, so a deterministically failing agent stops after three attempts instead of respawning forever.
- `brr run` with `--max` no longer exits as a failure when an earlier iteration failed but the final iteration succeeded. The engine now clears the tracked last error on a successful iteration, so the "last iteration failed" error is only returned when the final iteration actually fails. This also prevents workflow stages from aborting with status "error" after a successful recovery.

## [0.6.0] "Encore Performance" - 2026-05-25

### Added

- `brr workflow reset <name>` discards a workflow's saved state and event log without starting a run. Use it to abandon a paused or failed run cleanly.
- `brr workflow run` now prints a `starting fresh: no saved state` line when no resume state applies, mirroring the existing `resuming:` line. Makes the lifecycle visible so users don't reach for `--reset` after a successful run.

### Changed

- Successful workflow completion now clears both `.brr/state/workflows/<name>.json` and `.brr/state/workflows/<name>.events.jsonl`. Previously the event log was preserved across runs. Pause, failure, error, and interrupt still preserve both files for resume and inspection.

## [0.5.0] "Cycle Therapy" - 2026-05-23

### Changed

- Workflow cycle exhaustion now degrades gracefully: when a stage requests another cycle but `cycle.max` is already used, brr prints a warning, logs a `cycle_skipped` event, and advances to the next sequential stage instead of failing the workflow. This removes a stuck-state where re-running a workflow would loop on the same cycle-max error indefinitely.

## [0.4.0] "Stage Fright" - 2026-05-20

### Added

- Workflow V2 schema with explicit stage IDs, agent and command stage types, top-level cycle configuration, strict validation, per-workflow state, JSONL event logs, and workflow `run`, `validate`, `status`, and `init` subcommands.
- `brr workflow status --watch`, which redraws saved workflow state with an animated running-stage marker.
- `brr workflow run` now prints a live workflow flow line with stage state markers and cycle-back context.
- `brr instructions`, which prints agent-facing setup guidance for creating project-local brr prompts, workflows, and config from an installed binary.
- Project-local Codex release skill for preparing and verifying brr releases.

### Changed

- Hardened `.brr.lock` symlink protection on Unix and Windows using no-follow opens, `os.Lstat`, and `os.SameFile` validation to prevent symlink traversal and TOCTOU races.
- Realigned signal file descriptions in `README.md`, `docs/index.html`, and CLI pause/resume output to reflect the automatic cleanup of signal files rather than instructing users to manually delete them.
- Windows interrupt handling now preserves graceful second Ctrl+C behavior by sending a console break event before falling back to direct child termination.
- Workflow state now lives under `.brr/state/workflows/`, and `brr init` creates and gitignores `.brr/state/`.
- Workflow status output now renders a compact stage-by-stage pipeline view instead of a raw state field dump.
- The bundled `ship` workflow now uses Workflow V2 and includes a deterministic `make check` command gate.

### Removed

- Legacy unversioned workflow files and per-stage `cycle: true` are no longer supported.

### Fixed

- Resolved a process leak on Windows where `reapGroup` failed to terminate orphaned background child processes (such as LSPs/MCP servers) once the main agent exited, using a pure Go recursive Toolhelp snapshot API traversal instead of an external `taskkill` call.
- Landing page documentation now reflects the current CLI usage, workflow notification behavior, resume state, and safety guidance.

## [0.3.6] "Early Warnings" - 2026-05-03

### Fixed

- Workflow `--notify` now reports startup and pre-engine errors, and `--reset` can clear an unsafe symlinked workflow state path without following it.

## [0.3.5] "Agnostic Cycles" - 2026-05-01

### Added

- `.brr-cycle` signal file for workflows, allowing prompts to request another pass without brr inspecting task markdown files
- `make check` now runs `govulncheck` for reachable dependency vulnerabilities

### Fixed

- Corrupt workflow state with invalid stage or cycle indexes is ignored instead of panicking during resume
- Prompt file resolution now rejects unsafe direct prompt files consistently and applies the 10 MiB prompt size limit to named prompts
- Plain `brr <prompt>` runs now reject `.brr-cycle` instead of treating workflow-only cycle requests as successful stops
- The repository `.gitignore` now matches `brr init` output for local brr signal, lock, and workflow state files

## [0.3.4] "Clean Runners" - 2026-04-26

### Fixed

- GitHub Actions workflows now use Node.js 24-compatible action majors directly, removing Node.js 20 deprecation annotations from CI, release, and Pages runs

## [0.3.3] "Node Ahead" - 2026-04-26

### Fixed

- GitHub Actions CI and release workflows now opt into the Node.js 24 action runtime, matching the Pages workflow and avoiding Node.js 20 deprecation annotations

## [0.3.2] "Hard Edges" - 2026-04-26

### Changed

- Build prompt failure handling now creates `.brr-failed` with validation details instead of discarding local changes after repeated validation failures

### Fixed

- Project config, named prompt, workflow file, and workflow state handling now reject symlinks and other non-regular files instead of following or overwriting them
- `brr init` now writes `.gitignore` atomically and rejects symlinked `.brr/prompts` and `.brr/workflows` paths
- Terminal colors are now enabled based on stderr TTY detection, matching where brr writes its own UI
- Notifications now truncate UTF-8 content safely, and workflow `--notify` now sends a failure notification when `.brr-failed` stops a stage
- README, specs, and landing page now document `.brr-failed` consistently

## [0.3.1] "Red Light, Green Light" - 2026-04-13

### Added

- `.brr-failed` signal file — agents can now distinguish failure from needing approval. brr stops the loop, prints the file contents, and in workflows preserves state for resume. Previously, failures had to be shoehorned into `.brr-needs-approval`.

### Fixed

- Homebrew install now uses a formula instead of a cask, matching the expected install path for CLI binaries

## [0.3.0] "Pipes Not Words" - 2026-04-10

### Added

- `brr workflow <name>` command — orchestrates multi-stage pipelines defined in `.brr/workflows/<name>.yaml`, with sequential stage execution, per-stage profile overrides, and automatic cycle-back when verify/review stages find issues
- New prompts: `spec` (requirements to structured spec), `verify` (check implementation against acceptance criteria), `review` (autonomous code review of recent changes)
- Example `ship` workflow: spec → plan → build → verify → review with up to 3 fix cycles
- Workflow resume: progress is saved to `.brr-workflow-state.json` after each stage — interrupted or paused workflows resume from the last checkpoint on re-run (`--reset` to start fresh)
- `brr init` now scaffolds `.brr/workflows/` directory

### Fixed

- All brr output now writes to stderr, keeping stdout clean for agent piping and composition
- `brr init` no longer gitignores `COMPILE.md` files outside the repo root

## [0.2.2] "Still Warm" - 2026-04-01

### Fixed

- Crash with dirty working tree no longer counts toward the consecutive failure streak — agents that make progress before dying (context exhaustion, timeout) are retried instead of stopped
- Orphaned child processes (MCP servers, language servers) are now reaped between iterations to prevent process table exhaustion during long runs

## [0.2.1] "Read the Signs" - 2026-03-29

### Changed

- Expanded signal file documentation (`.brr-complete`, `.brr-needs-approval`, `.brr.lock`) across CLI `--help`, landing page, and README with clearer descriptions and usage guidance

## [0.2.0] "Ding When Done" - 2026-03-29

### Added

- Desktop notifications on loop termination via `--notify` / `-n` flag — sends OS-native notifications for completion, approval needed, max iterations, and fail streak events (macOS via osascript, Linux via notify-send)
- Structured `StopReason` in engine results, distinguishing all five exit conditions (complete, approval, max-iterations, fail-streak, interrupted)
- Hardened default prompts: dirty state recovery, repeated failure detection with auto-escalation, retry limits, and combined review phase with fallbacks
- New `audit` prompt for autonomous codebase auditing with parallel agents and severity gating

## [0.1.3] "Locks Changed" - 2026-03-28

### Added

- GitHub Pages landing page styled after `brr --help`, including the ASCII banner, CLI color palette, install and usage docs, and a workflow to deploy `docs/` from `main`

### Fixed

- Lock file no longer deleted on release, preventing a race where two processes could both acquire the lock
- Lock errors now show the real cause (e.g. permission denied) instead of always blaming a concurrent instance
- `Run()` now returns an error when max iterations reached but the last iteration failed (previously exited 0)
- Signal file cleanup no longer deletes directories or symlinks that happen to share signal file names
- Prompt resolution rejects symlinks and FIFOs instead of following them
- Prompt files larger than 10 MiB are rejected with a clear error
- `rejectSymlink` no longer swallows non-ENOENT errors from `Lstat`
- Scaffold rollback reports errors instead of silently discarding them, and cleans up empty `.brr/` directories

## [0.1.2] "Thoroughly Frisked" - 2026-03-27

### Added

- Lockfile (`.brr.lock`) to prevent concurrent runs from racing on signal files

### Fixed

- `resolvePrompt` now distinguishes permission errors from "file not found" instead of misreporting all stat failures
- Fail-streak error now includes the underlying cause (last child error) instead of a generic message
- Scaffold `Init` correctly identifies permission errors vs missing directories during rollback
- Config validation errors no longer hardcode `.brr.yaml` when the config may come from the global path
- Signal handling now logs errors when killing child processes instead of silently discarding them

### Changed

- `brr init` now adds `.brr.lock` to `.gitignore`

## [0.1.1] "Helpful Graffiti" - 2026-03-27

### Added

- ASCII art banner, tagline, and GitHub link in `--help` output
- `/release` slash command for cutting releases from Claude Code

## [0.1.0] - 2026-03-27

### Added

- Loop engine: run any AI agent prompt in a loop with fresh sessions
- Agent-agnostic profiles via `.brr.yaml` (Claude Code, Codex, or custom)
- Prompt resolution: file path, named prompt (`.brr/prompts/`), or inline text
- `brr init` scaffolding for project config and example prompts
- Signal file control: `.brr-complete` and `.brr-needs-approval`
- Graceful Ctrl+C handling (finish current / interrupt child / force kill)
- Auto-stop after three consecutive failures
- `--max` flag to bound iterations
- `--version` flag with embedded build info
- Cross-platform support: Linux, macOS, Windows (amd64, arm64)
