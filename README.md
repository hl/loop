# brr

Your AI agent, but unhinged.

brr runs a prompt in a loop, spinning up a fresh session for each iteration. Each run gets a clean context, so tasks stay focused and agents stay sharp. Point it at a list, a directory, an API, or anything that produces work — brr just keeps going.

Works with any AI coding agent that accepts prompts on stdin — [Claude Code](https://docs.anthropic.com/en/docs/claude-code), [Codex](https://openai.com/index/codex/), or whatever comes next. Based on [The Ralph Playbook](https://github.com/ClaytonFarr/ralph-playbook).

## Install

```bash
brew install hl/tap/brr

# or
go install github.com/hl/brr/cmd/brr@latest

# or build from source
git clone https://github.com/hl/brr && cd brr && make build
```

## Usage

```bash
# Run a prompt file in a loop
brr ./my-prompt.md --max 20

# Named prompt (resolves to .brr/prompts/task.md)
brr task --max 3

# Inline prompt
brr "Fix all TODO comments in src/" --max 5

# Use a different profile
brr task --max 10 -p opus

# Scaffold a project
brr init

# Create and run the bundled ship workflow
brr workflow init ship
brr workflow validate ship
brr workflow run ship
brr workflow status ship

# Print agent-facing setup instructions
brr instructions
```

## Configuration

`brr init` generates a `.brr.yaml` with agent profiles:

```yaml
default: claude

profiles:
  claude:
    command: claude
    args: [-p, --dangerously-skip-permissions, --model, sonnet, --max-turns, "200"]

  opus:
    command: claude
    args: [-p, --dangerously-skip-permissions, --model, opus, --max-turns, "200"]

  codex:
    command: codex
    args: [exec, --ephemeral, --dangerously-bypass-approvals-and-sandbox, --model, gpt-5.4, -]
```

Each profile defines a `command` and its `args`. The prompt is piped to stdin. Switch profiles with `-p`:

```bash
brr task --max 10            # uses default (claude)
brr task --max 10 -p opus    # uses opus
brr task --max 10 -p codex   # uses codex
```

Add your own profiles for any agent or configuration you want. Profile names are matched case-insensitively, so `-p opus` and `-p Opus` select the same profile.

**Priority:** `.brr.yaml` > `<os-config-dir>/brr/config.yaml` (e.g. `~/.config/brr/` on Linux, `~/Library/Application Support/brr/` on macOS, `%AppData%\brr\` on Windows).

## Writing prompts

brr doesn't care what your prompt says — it just runs it over and over. Each iteration gets a fresh context window, so the prompt needs to be self-contained: orient, pick work, do it, commit.

The repo includes example prompts under [`prompts/`](prompts/). Copy them to `.brr/prompts/` and adapt, or write your own from scratch for anything:

- Triage a backlog of issues
- Run a migration across microservices
- Review and fix lint violations one file at a time
- Process items from a queue or API

A good loop prompt follows this shape:

```markdown
You are one iteration of a loop. Do one unit of work, then exit.

1. Read the state (a file, an API, a queue)
2. Pick one item
3. Do the work
4. Update the state (mark it done, commit, etc.)
5. If nothing left, create `.brr-complete` and exit
```

Put prompts in `.brr/prompts/` (per-project) or `<os-config-dir>/brr/prompts/` (global). Then `brr task` resolves to `.brr/prompts/task.md`. Or point at any file: `brr ./my-prompt.md`.

Agents that need to create project-local brr prompts, workflows, or config can run `brr instructions` from any installed binary. The same guidance is also available in [`docs/agent-instructions.md`](docs/agent-instructions.md).

## Workflows

Workflows are versioned YAML pipelines in `.brr/workflows/`. They combine agent stages with deterministic command gates:

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

Run workflows with `brr workflow run <name>`, which prints the stage flow and current state as the run advances. Use `brr workflow validate <name>` before a long run, `brr workflow status [name]` to inspect saved progress, `brr workflow status <name> --watch` to follow the saved state live, `brr workflow reset <name>` to discard saved progress without starting a run, and `brr workflow init <name> --template ship` to copy the bundled requirements-to-review workflow.

Workflow progress is stored in `.brr/state/workflows/<name>.json`; event history is appended to `.brr/state/workflows/<name>.events.jsonl`. Successful runs clear both files so the next `brr workflow run <name>` starts fresh — no `--reset` needed. When a run pauses, fails, or is interrupted, both files are preserved so the next invocation can resume.

## How it works

brr pipes the same prompt to the configured command, once per iteration. Each run gets a fresh process with a clean context window.

The loop is controlled by signal files in the working directory:

- **`.brr-complete`** — the agent creates this when all work is finished. brr detects it, stops the loop, and removes the file.
- **`.brr-failed`** — the agent creates this when it hits a blocker or cannot recover after retrying. brr stops the loop and prints the file contents (up to 4 KiB). Investigate the failure and re-run.
- **`.brr-needs-approval`** — the agent creates this when it needs a human decision. brr stops the loop and prints the file contents (up to 4 KiB). Resolve the issue and re-run.
- **`.brr-cycle`** — inside `brr workflow run`, the agent creates this when a later stage found more work and the workflow should restart from `cycle.target`.
- **`.brr.lock`** — prevents multiple brr instances from running in the same directory. Acquired on start, released on exit. The file stays on disk between runs (this is intentional). Added to `.gitignore` by `brr init`.
- **`.brr/state/workflows/`** — workflow resume state and event logs. Both files are cleared on successful completion and preserved on pause, failure, or interrupt so the next invocation resumes.

Three consecutive failures also stop the loop automatically.

## Safety

Most agent CLIs have a "skip permissions" flag for a reason — brr is designed to use it. But:

- Run in isolated environments (Docker, VM, devcontainer)
- Minimum viable access — only the API keys you need
- Set `--max` to bound iterations
- `Ctrl+C` stops gracefully (1st: finish current iteration; 2nd: interrupt child; 3rd: force kill)

## References

- [The Ralph Playbook](https://github.com/ClaytonFarr/ralph-playbook)
- [Original Ralph Post](https://ghuntley.com/ralph/) by Geoff Huntley
