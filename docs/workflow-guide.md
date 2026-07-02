# How the brr workflow works

A **workflow** is a versioned YAML pipeline that chains agent runs and deterministic command gates into one repeatable sequence. Where `brr <prompt>` loops a single agent until it signals done, `brr workflow run <name>` walks a list of named stages — running an agent for some, a shell command for others — and persists progress so the run can be resumed, inspected, and debugged.

This guide explains the mental model, the file format, and the full execution lifecycle. For the formal requirements see [`specs/workflow.md`](specs/workflow.md); for a quick orientation see the [README](../README.md#workflows).

---

## Mental model

```
            ┌─ agent stage ─┐   ┌─ command gate ─┐   ┌─ agent stage ─┐
REQUIREMENTS│ run the loop  │ → │ make check     │ → │ run the loop  │ → done
            │ engine with a │   │ (argv, exit≠0  │   │ engine with a │
            │ prompt        │   │  stops the run)│   │ prompt        │
            └───────────────┘   └────────────────┘   └───────────────┘
                    ▲                                         │
                    └────────── .brr-cycle restarts ──────────┘
                              from cycle.target (≤ cycle.max)
```

- **Stages run strictly in order**, one at a time. A stage must finish before the next begins.
- **Agent stages** run the same loop engine as a bare `brr` run: the prompt is piped to the configured command once per iteration, each iteration in a fresh process with a clean context. The stage ends when the agent drops a signal file or hits its `max` iteration count.
- **Command stages** run an argv array directly (no shell). They are the deterministic gates — tests, linters, builds. A non-zero exit stops the whole workflow.
- **Cycles** let a late stage send the pipeline back to an earlier one (e.g. review found more work → return to build), bounded by `cycle.max`.
- **State is persisted after every stage**, so an interrupted or failed run resumes from where it stopped on the next invocation.

The workflow itself is dumb on purpose: it sequences stages, watches for signal files, and records state. All the judgment lives in the prompts the agent stages run.

---

## The workflow file

Workflow files are YAML, resolved in this order (first match wins):

1. `.brr/workflows/<name>.yaml` (project-local)
2. `<os-config-dir>/brr/workflows/<name>.yaml` (user-global)

Names must be non-empty and contain no path separators or `..`. Resolution never follows symlinks.

### Schema (`version: 2`)

```yaml
version: 2                       # required — must be the integer 2
description: "short summary"     # optional

defaults:                        # optional
  profile: claude                #   default profile for agent stages
  max: 3                         #   default iteration cap for agent stages (≥ 0)

cycle:                           # optional — omit for a straight-line pipeline
  target: build                  #   stage id to restart from on .brr-cycle (required if cycle set)
  max: 3                         #   max number of cycles (required if cycle set, ≥ 1)

stages:                          # required — at least one
  - id: spec                     #   unique, non-empty, no "/" "\" ".."
    type: agent                  #   "agent" | "command"
    prompt: spec                 #   agent only — resolved via prompt resolution
    profile: claude              #   agent only — optional, overrides defaults.profile
    max: 3                       #   agent only — optional (≥ 1), overrides defaults.max

  - id: check
    type: command
    command: ["make", "check"]   #   command only — argv array, run without a shell
```

> Only `version: 2` is accepted. Unversioned or older files are rejected with a migration-style error — there is no legacy execution path.

### Field reference

| Key | Where | Required | Notes |
|-----|-------|----------|-------|
| `version` | top level | yes | Must be `2`. |
| `description` | top level | no | Free text, shown in the run summary. |
| `defaults.profile` | top level | no | Fallback profile for agent stages. |
| `defaults.max` | top level | no | Fallback iteration cap for agent stages (≥ 0). |
| `cycle.target` | top level | if `cycle` set | Stage id to jump to on `.brr-cycle`; must name a real stage. |
| `cycle.max` | top level | if `cycle` set | Maximum cycles before the pipeline gives up and advances (≥ 1). |
| `id` | stage | yes | Unique; no `/`, `\`, or `..`. |
| `type` | stage | yes | `agent` or `command`. |
| `prompt` | agent stage | yes | Resolved as file → named prompt → inline text. |
| `profile` | agent stage | no | Overrides `defaults.profile` for this stage. |
| `max` | agent stage | no | Per-stage iteration cap (≥ 1); overrides `defaults.max`. |
| `command` | command stage | yes | Non-empty argv array; run directly, inherits stdio. Command stages reject `prompt`, `profile`, and `max`. |

### Resolution rules

- **Profile** (agent stages) resolves as: stage `profile` → `--profile` flag → `defaults.profile` → config default. A profile maps to a `command` + optional `args` in `.brr.yaml`.
- **Effective max** (agent stages): stage `max` → `defaults.max`.
- **Prompt**: an existing file path wins; otherwise `.brr/prompts/<name>.md`, then `<os-config-dir>/brr/prompts/<name>.md`; otherwise the value is used as inline prompt text. (See [`specs/prompt-resolution.md`](specs/prompt-resolution.md).)

---

## The bundled `ship` workflow

`brr workflow init ship` writes this requirements-to-reviewed-code pipeline. It's the canonical example of mixing agent stages with a command gate plus a cycle.

```yaml
version: 2
description: "requirements -> verified, reviewed code"

defaults:
  profile: claude
  max: 3

cycle:
  target: build
  max: 3

stages:
  - id: spec
    type: agent
    prompt: spec
    max: 3

  - id: plan
    type: agent
    prompt: plan
    max: 5

  - id: build
    type: agent
    prompt: build
    max: 100

  - id: check
    type: command
    command: ["make", "check"]

  - id: verify
    type: agent
    prompt: verify
    max: 3

  - id: review
    type: agent
    prompt: review
    max: 1
```

Read it as a sentence: turn `REQUIREMENTS.md` into a **spec**, draft a **plan**, **build** it, run `make check` as a hard gate, **verify** behaviour, then **review**. If `check`, `verify`, or `review` decides more work is needed, it drops `.brr-cycle` and the pipeline returns to `build` — up to 3 times.

---

## What you see when it runs

`brr workflow run` prints a summary, then a live flow diagram that updates as stages advance, plus a header before each stage:

```
  workflow: ship
  description: requirements -> verified, reviewed code
  stages:  6
  cycle:   build (max 3)

  flow: ✓ spec → ✓ plan → ▶ build → ○ check → ○ verify → ○ review
  cycle: ↺ build (max 3, used 0)

━━━ Stage 3/6 — build ▸ build (max 100) ━━━
```

Status icons in the flow line:

| Icon | Meaning |
|------|---------|
| `○` | pending |
| `▶` / spinner | running |
| `✓` | completed |
| `✗` | failed or errored |
| `⏸` | paused for approval |
| `↻` | cycled |
| `■` | interrupted |

---

## Execution lifecycle

For each stage, in order:

1. **Agent stage** → resolve prompt and profile, then run the loop engine with `SkipLock: true` (the workflow already holds the exclusive lock). The engine loops until a signal file appears, `max` iterations is hit, or three consecutive failures occur.
2. **Command stage** → run the argv array directly, inheriting stdin/stdout/stderr. A non-zero exit stops the workflow.
3. **Record state** → write `.brr/state/workflows/<name>.json` and append one line to `.brr/state/workflows/<name>.events.jsonl`.
4. **Decide what's next** based on how the stage ended (below).

### How a stage's outcome steers the run

| Stage outcome | What the workflow does | Exit code |
|---------------|------------------------|-----------|
| Completes normally / `.brr-complete` | Advance to the next stage | — |
| `.brr-cycle`, cycle configured, cycles remain | Increment `cycle_count`, jump to `cycle.target` | — |
| `.brr-cycle`, cycle configured, `cycle.max` reached | Log `cycle_skipped`, print a warning, advance past this stage | — |
| `.brr-cycle`, no cycle configured | Stop with an error | 1 |
| `.brr-failed` | Stop, preserve state, print the file contents (≤ 4 KiB) | 0 |
| `.brr-needs-approval` | Stop, preserve state, print the file contents (≤ 4 KiB) | 0 |
| Command stage non-zero exit | Stop, preserve state | 1 |
| Interrupt (Ctrl-C) | Stop, preserve state | 130 |

Signal files (`.brr-complete`, `.brr-failed`, `.brr-needs-approval`, `.brr-cycle`) are detected and removed automatically after each stage. See [`specs/signal-files.md`](specs/signal-files.md).

---

## State, resume, and follow-up runs

Progress lives under `.brr/state/workflows/`:

- `<name>.json` — resume state: `schema_version`, `workflow`, `run_id`, `started_at`, `updated_at`, `start_sha`, `next_stage_id`, `cycle_count`, and a per-stage status entry (status, reason, duration, prompt/profile/command metadata).
- `<name>.events.jsonl` — append-only event log: `workflow_started` (fresh run) or `workflow_resumed` (resume, with the stage id it picks up from), `stage_started`, `stage_finished`, `cycle`, `cycle_skipped`, `workflow_error`, `workflow_complete`.

Both are runtime state — `brr init` gitignores `.brr/state/`. Writes reject symlinks and other non-regular files.

**Resume.** On startup, if a valid state file exists for the current workflow, the run resumes from `next_stage_id`. State that doesn't match (wrong schema, wrong workflow name, out-of-range cycle count, unknown next stage) is silently ignored and a fresh run starts.

**Follow-up runs are frictionless.** On *successful* completion both files are deleted, so the next `brr workflow run <name>` starts clean with no `--reset` needed. When a run *pauses, fails, errors, or is interrupted*, both files are preserved so the next invocation resumes and the failure can be inspected.

**Two ways to start over.** Use `--reset` on a run to discard saved state and start from the first stage (the event log is kept for inspection). Use `brr workflow reset <name>` to discard both files without starting a run — a no-op if there's nothing saved.

---

## CLI reference

| Command | What it does |
|---------|--------------|
| `brr workflow run <name>` | Run (or resume) the workflow. Flags: `--profile <p>` default profile for agent stages, `--notify` desktop notification on success/`.brr-failed`/error, `--reset` discard saved state and start fresh. |
| `brr workflow validate <name>` | Load and check the workflow without running anything: schema, profile references, prompt resolution, and command executables. Use before a long run. Accepts `--profile`. |
| `brr workflow status [name]` | Print saved state as a stage-by-stage view. With no name, list all workflows that have saved state. `--watch` redraws live (with `--interval`, default 1s) until the state clears. |
| `brr workflow init <name>` | Create `.brr/workflows/<name>.yaml` from a template. `--template ship` (the default and currently only template). |
| `brr workflow reset <name>` | Delete the saved state file and event log for the workflow. No-op message if none exist. |

A typical first run:

```bash
brr workflow init ship          # write .brr/workflows/ship.yaml
brr workflow validate ship      # schema, profiles, prompts, commands — no execution
brr workflow run ship           # run the pipeline, live flow shown
brr workflow status ship --watch  # follow a long run from another terminal
```

---

## Constraints worth knowing

- The exclusive lock is acquired once before the first stage and held until the workflow exits; agent stages run with `SkipLock: true` so they don't re-acquire it.
- Command stages never go through a shell — always argv arrays — so there's no shell expansion or word splitting.
- The workflow never inspects or edits prompt-owned task state (e.g. `IMPLEMENTATION_PLAN.md`); that belongs to the prompts.
- Workflows run on Linux, macOS, and Windows.

## Related specs

- [`specs/workflow.md`](specs/workflow.md) — formal requirements and acceptance criteria
- [`specs/loop-engine.md`](specs/loop-engine.md) — how agent stages iterate
- [`specs/signal-files.md`](specs/signal-files.md) — the `.brr-*` protocol
- [`specs/prompt-resolution.md`](specs/prompt-resolution.md) — prompt lookup order
- [`specs/configuration.md`](specs/configuration.md) — profiles and config loading
- [`specs/concurrent-run-prevention.md`](specs/concurrent-run-prevention.md) — locking
