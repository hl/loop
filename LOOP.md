# Loop

Autonomous AI coding loop based on [The Ralph Playbook](https://github.com/ClaytonFarr/ralph-playbook).

## Quick Start

**See [Safety](#safety) before running.**

```bash
chmod +x loop.sh
./loop.sh plan    # Generate implementation plan
./loop.sh plan 3  # Refine the plan (each iteration reviews and improves)
./loop.sh         # Build from plan (unlimited)
./loop.sh 20      # Build from plan (max 20 iterations)
```

## Flow

```text
 ┌─────────────────────────────────────────────────────────┐
 │  1. DEFINE                                              │
 │     Human writes specs in docs/specs/                   │
 └────────────────────────┬────────────────────────────────┘
                          │
                          ▼
 ┌─────────────────────────────────────────────────────────┐
 │  2. PLAN                    ./loop.sh plan [N]          │
 │                                                         │
 │  Each iteration:                                        │
 │    • Reads specs + source + existing plan               │
 │    • Identifies gaps between spec and code              │
 │    • Creates/refines IMPLEMENTATION_PLAN.md             │
 │    • Commits the updated plan                           │
 │                                                         │
 │  Multiple iterations refine the plan — each one         │
 │  reviews the previous plan against specs and source,    │
 │  tightening task descriptions, correcting priorities,   │
 │  and catching gaps the prior iteration missed.          │
 └────────────────────────┬────────────────────────────────┘
                          │
                          ▼
 ┌─────────────────────────────────────────────────────────┐
 │  3. BUILD                   ./loop.sh [N]               │
 │                                                         │
 │  Each iteration (fresh context window):                 │
 │    Phase 1: Orient    — read specs, plan, AGENTS.md     │
 │    Phase 2: Select    — pick task, search codebase      │
 │    Phase 3: Implement — single or parallel workers      │
 │    Phase 4: Validate  — run compile/lint/test           │
 │    Phase 5: Review    — code review (blocking issues)   │
 │    Phase 6: Finalize  — update plan, commit             │
 │                                                         │
 │  Loop exits when:                                       │
 │    • All tasks complete (.loop-complete sentinel)       │
 │    • Max iterations reached                             │
 │    • 3 consecutive stalls (no commits)                  │
 └─────────────────────────────────────────────────────────┘
```

## How It Works

Each iteration gets a fresh context window — no degradation over long projects. The bash loop provides crash recovery (just re-run), stall detection (3 consecutive no-commit iterations), and iteration logging.

### Loop Modes

| Command | Mode | Prompt File | Purpose |
| ------- | ---- | ----------- | ------- |
| `./loop.sh plan` | Planning | loop/PROMPT_plan.claude.md | Gap analysis, create/update plan |
| `./loop.sh plan 5` | Planning | loop/PROMPT_plan.claude.md | Planning with max 5 iterations |
| `./loop.sh` | Building | loop/PROMPT_build.claude.md | Implement from plan (unlimited) |
| `./loop.sh 20` | Building | loop/PROMPT_build.claude.md | Implement with max 20 iterations |

### Choose the CLI (Claude or Codex)

By default the loop runs `claude` (per `loop/config.ini`). To use Codex, select a profile in `loop/config.ini`.

```bash
# Claude (default)
./loop.sh

# Codex via profile
./loop.sh --profile codex-fast

# Claude via profile
./loop.sh --profile claude-strong
```

### Profiles

Use `--profile NAME` to load settings from `loop/config.ini`. This keeps friendly, reusable presets without relying on env vars.

```bash
./loop.sh --profile codex-fast plan 3
./loop.sh --profile claude-strong 10
```

`loop/config.ini` format (INI-style):

```ini
[defaults]
cli=claude
model=opus
max_turns=200
max_retries=3
max_stalls=3
log_dir=loop/logs
prompt_plan=loop/PROMPT_plan.claude.md
prompt_build=loop/PROMPT_build.claude.md

[codex-fast]
cli=codex
model=gpt-5.2-codex
cli_flags=--full-auto -s workspace-write
reasoning_effort=medium
prompt_plan=loop/PROMPT_plan.codex.md
prompt_build=loop/PROMPT_build.codex.md
```

Precedence (highest → lowest): profile values, defaults, built-ins. Profiles can override prompt paths and log_dir.

### Multi-model workflow

You can switch models between runs by selecting different profiles. This lets you use a faster/cheaper model for planning and a stronger model for building.

```bash
# Plan with a faster model, build with a stronger one
./loop.sh --profile codex-fast plan 3
./loop.sh --profile claude-strong

# Mix CLIs and models across runs
./loop.sh --profile claude-strong plan
./loop.sh --profile codex-fast 10
```

### Plan Refinement

Running the plan loop multiple times is intentional. Each iteration reviews the previous plan against the specs and source code, producing incremental improvements:

- First iteration: initial gap analysis, creates the plan
- Second iteration: tightens task descriptions, fixes sizing, corrects priority order
- Third+ iterations: catches edge cases, adds missing acceptance criteria

Run `./loop.sh plan 3` for a well-refined plan. Diminishing returns after 3-4 iterations for most projects.

### CLI Flags Used (Claude default)

| Flag | Purpose |
| ---- | ------- |
| `-p` | Headless mode (non-interactive) |
| `--dangerously-skip-permissions` | Auto-approve tool calls |
| `--output-format=stream-json` | JSON output for log parsing (not human-readable) |
| `--model opus` | Complex reasoning (set via config/profile) |
| `--max-turns N` | Cap tool-use rounds per iteration (set via config/profile) |
| `--verbose` | Detailed execution logging |

### CLI Flags Used (Codex)

| Flag | Purpose |
| ---- | ------- |
| `exec` | Run Codex non-interactively |
| `--dangerously-bypass-approvals-and-sandbox` | Skip approvals and sandboxing |
| `--json` | Emit JSONL events to stdout |
| `--model o3` | Select model (set via config/profile) |

Note: `--dangerously-bypass-approvals-and-sandbox` is only added when no `cli_flags` are set (to avoid conflicts with `--full-auto`).

## Prerequisites

1. **Prompt files** — Create before running:
   - `loop/PROMPT_plan.claude.md` — Planning mode instructions (Claude)
   - `loop/PROMPT_build.claude.md` — Building mode instructions (Claude)
   - `loop/PROMPT_plan.codex.md` — Planning mode instructions (Codex)
   - `loop/PROMPT_build.codex.md` — Building mode instructions (Codex)

2. **Specs** — Requirements in `docs/specs/` (see [Writing Specs](#writing-specs))

3. **AGENTS.md** — Build/test/lint commands for your project. Minimum content is a `## Validation` section listing your commands

4. **`.gitignore`** — The build prompt stages and commits files; without a `.gitignore`, build artifacts, `node_modules/`, `.env`, `loop/logs/`, etc. will be committed

## File Structure

```text
project-root/
├── loop.sh                    # Loop runner
├── loop/PROMPT_build.claude.md # Building mode instructions (Claude)
├── loop/PROMPT_plan.claude.md  # Planning mode instructions (Claude)
├── loop/PROMPT_build.codex.md  # Building mode instructions (Codex)
├── loop/PROMPT_plan.codex.md   # Planning mode instructions (Codex)
├── AGENTS.md                  # Operational guide (build/test commands)
├── IMPLEMENTATION_PLAN.md     # Task list (generated)
├── loop/config.ini            # Loop config + profiles
├── loop/logs/                 # Iteration logs (gitignore this)
├── lib/                       # Source code (adjust path to your project)
└── docs/specs/                # Requirement specifications
```

## Writing Specs

Specs are the primary input to the system. Place one file per topic in `docs/specs/`.

Each spec should cover:

- **What** the feature does (behavior, not implementation)
- **Why** it exists (user need, business reason)
- **Acceptance criteria** — concrete conditions that define "done"
- **Constraints** — performance, compatibility, security requirements
- **Out of scope** — what this spec intentionally does not cover

Keep specs focused. A spec for "user authentication" and a separate spec for "password reset" is better than one spec covering both. The agent reads all specs each iteration, so smaller files mean faster comprehension.

## Prompt Templates

The prompts live in `loop/PROMPT_plan.claude.md` and `loop/PROMPT_build.claude.md` (Claude) and `loop/PROMPT_plan.codex.md` and `loop/PROMPT_build.codex.md` (Codex). Edit those files directly. Below is a summary of what each does and the patterns they use.

### loop/PROMPT_plan.claude.md

Uses parallel Task agents to study specs, existing source code, and the current plan. Compares implementation against specs to identify gaps. Outputs a prioritized IMPLEMENTATION_PLAN.md with each task noting which files/modules it touches (so the build phase can assess parallelism). Commits the updated plan. Does not implement anything.

### loop/PROMPT_build.claude.md

Each build iteration runs through six phases:

```text
Phase 1: Orient    — parallel read-only agents study specs, plan, and AGENTS.md
Phase 2: Select    — pick task, search codebase for related code, assess parallelism
Phase 3: Implement — single worker (3A) or parallel workers (3B)
Phase 4: Validate  — dedicated agent runs AGENTS.md commands, fixes failures
Phase 5: Review    — code review via /pr-review-toolkit:review-pr (only if validation passed)
Phase 6: Finalize  — update plan, commit (only if review passed)
```

#### Single worker vs. parallel workers (Phase 3)

Most tasks use **Phase 3A** — a single Task agent implements the task. This is simpler, avoids coordination overhead, and prevents write conflicts.

**Phase 3B** spawns multiple Task agents in parallel when a task clearly decomposes into independent sub-parts that touch different files. Workers must not modify the same files — if file-disjoint work can't be guaranteed, 3A is used instead.

#### Completion detection

When all tasks in IMPLEMENTATION_PLAN.md are complete, the build agent creates a `.loop-complete` sentinel file on disk (not committed — it's a signal to the loop script, not part of the repo). The loop script checks for the file after each iteration and exits cleanly (exit 0). The sentinel file is deleted on exit.

**Both prompts should specify your source code path** (e.g., `lib/*`, `test/*`, `src/*`) so the agent knows where to look.

### Patterns used in the prompts

- **"don't assume not implemented"** — forces code search before writing new code
- **"parallel Task agents"** — concurrent read-only work via the Task tool
- **"single worker by default"** — prevents concurrent write conflicts for most tasks
- **"file-disjoint parallel workers"** — parallel implementation when sub-parts touch separate files
- **"dedicated validation agent"** — keeps test output out of the orchestrator's context; receives task description and changed file list for informed fixes
- **"single-pass code review"** — review once after validation, fix once, don't re-review; unresolvable issues become follow-up tasks rather than blocking the loop
- **".loop-complete sentinel"** — clean exit when all tasks complete

## Backpressure

The loop self-corrects through backpressure:

1. AGENTS.md lists validation commands (compile, lint, test)
2. The build prompt tells the agent to run those commands after changes
3. When validation fails, the agent sees error output in tool results
4. The agent reacts to failures — fixing code, adjusting approach, or documenting blockers

## Failure Modes

The loop script uses **git commits as the progress signal**. If no new commit is created for 3 consecutive iterations, the script exits (stall detection).

| Scenario | What happens | Mitigation |
| -------- | ------------ | ---------- |
| Tests fail repeatedly | Agent retries within the iteration; if stuck, discards broken code, documents blocker, commits only the blocker note | Next fresh iteration gets a clean slate to try a different approach |
| Parallel worker conflict | Two workers modify the same file | Build prompt enforces file-disjoint constraint; falls back to single worker (3A) when unsure |
| Claude CLI crash | API error, rate limit, or network failure | Retry with exponential backoff (30s, 60s, 90s); exits after 3 consecutive failures |
| No progress (3 stalls) | Script exits with error (exit 1) after 3 consecutive no-commit iterations | Built into `loop.sh`; no action needed |
| All tasks complete | Build agent creates `.loop-complete` sentinel file on disk; script detects it and exits cleanly (exit 0) | Built into `loop.sh` and build prompt |
| Runaway iteration | Single iteration uses excessive tool calls | `--max-turns` caps rounds per iteration (set via config/profile) |

## Safety

Running `--dangerously-skip-permissions` bypasses Claude's permission system entirely.

**Recommended:**

- Run in isolated environments (Docker, VM)
- Minimum viable access (only required API keys)
- Always set a max iteration count to avoid runaway loops
- Escape hatches: `Ctrl+C` stops the loop; `git reset --hard` reverts uncommitted changes

## When to Regenerate the Plan

The plan is disposable. Regenerate when:

- The agent implements wrong things or duplicates work
- Plan feels stale or doesn't match current state
- Too much clutter from completed items
- Significant spec changes occurred

```bash
./loop.sh plan  # Regenerate
```

## Tips

1. **Seed AGENTS.md with your validation commands** — the agent adds operational learnings as it goes
2. **Watch initial loops** — observe failure patterns, then add guardrails
3. **One task per iteration** — fresh context = full context window utilization
4. **Plan is cheap** — run `./loop.sh plan 3` for a well-refined plan
5. **Check logs when debugging** — each iteration writes to `loop/logs/` with timestamped JSON
6. **Tune `max_turns`** — lower for simple projects (edit `loop/config.ini`), keep high for complex ones

## References

- [The Ralph Playbook](https://github.com/ClaytonFarr/ralph-playbook)
- [Original Ralph Post](https://ghuntley.com/ralph/) by Geoff Huntley
