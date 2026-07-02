# Deep Audit — 2026-07-02

Baseline: `main` @ `69c384b`. Six parallel auditors (engine, workflow core, workflow support, CLI, config/platform, adversarial security), findings individually traced end-to-end; viper and prompt-traversal findings reproduced empirically. The stale-`lastErr` engine bug found earlier today is excluded — already fixed in PR #5.

Severity: **critical** = data loss / wrong results silently; **high** = core guarantee broken; **medium** = user-visible misbehavior; **low** = edge case, hardening, or misreporting.

Status values: `open` → `fixed (<commit>)` / `wontfix (<reason>)`.

| ID | Sev | Conf | Area | Title | Status |
|----|-----|------|------|-------|--------|
| E1 | high | confirmed | engine | Dirty-tree heuristic permanently disables fail-streak breaker | fixed |
| S1 | high | confirmed | workflow | Stale signal files override command-stage results | fixed |
| F1 | high | confirmed | config | Viper lowercases profile names; uppercase profiles unreachable | open |
| W1 | medium | confirmed | engine+workflow | Interrupt swallowed when stage ends via signal file or max iterations | fixed |
| S2 | medium | confirmed | workflow | Double SIGINT delivered to command-stage child | fixed |
| S3 | medium | likely | workflow | Race misclassifies Ctrl+C as stage failure | fixed |
| C1 | medium | confirmed | cli | Empty resolved prompt accepted for workflow agent stages | fixed |
| C2 | medium | confirmed | cli | Workflow error notifications misreport the terminal event | fixed |
| F2 | medium | confirmed | config | Project profile deep-merges with same-named global profile | open |
| E2 | low | confirmed | engine | Signals during Start→publish window dropped yet consume escalation level | fixed |
| E3 | low | likely | engine | Windows terminateTree can recurse infinitely on PID cycles | fixed |
| W2 | low | confirmed | workflow | Single command failure recorded as `fail_streak` | fixed |
| W3 | low | confirmed | workflow | Duplicate `workflow_started` events on resume | fixed |
| W4 | low | confirmed | workflow | Negative per-stage `max` silently falls back to default | fixed |
| S4 | low | confirmed | workflow | WatchStatus wipes final frame when state file vanishes | fixed |
| S5 | low | confirmed | workflow | Run diagram always draws cycle edge from last stage | fixed |
| S6 | low | likely | workflow | atomicWriteRegularFile renames without fsync | fixed |
| C3 | low | confirmed | cli | `--notify` skipped for `.brr-cycle` stop in root command | open |
| F3 | low | likely | notify | notify-send body starting with `-` parsed as options (Linux) | open |
| F4 | low | likely | scaffold | Init stage-2 TOCTOU: `.brr` parent symlink not re-verified | open |
| X1 | low | confirmed | security | Workflow `prompt:` allows arbitrary out-of-tree file read | fixed |
| X2 | low | confirmed | security | Unbounded `ReadRegularFile` enables OOM via planted files | open |

---

## Engine

### E1 — Dirty-tree heuristic permanently disables the fail-streak circuit breaker
- **Severity:** high · confirmed · `internal/engine/engine.go:268`
- On a nonzero child exit, `gitTreeDirty()` checks whether the tree is *currently* dirty, not whether it *became* dirty during the failing iteration, and resets `failStreak` to 0 when it is. Pre-existing uncommitted changes (the normal state when running a coding agent), or a single productive edit in iteration 1, keep the tree dirty forever after, so `failStreak` can never reach `maxFailStreak`.
- **Failure:** `brr run` with default `--max 0` in a repo with uncommitted changes and an agent that fails deterministically (expired API key, bad flag) → logs "dirty tree detected, progress was made. Retrying." and respawns forever; the ReasonFailStreak safety stop never triggers on an unattended, billed run.
- **Fix:** snapshot tree state before each iteration (e.g. hash of `git status --porcelain` output) and reset the streak only if the state *changed* during that iteration.

### E2 — Signals arriving between `cmd.Start()` and `currentCmd` publication are dropped yet consume escalation levels
- **Severity:** low · confirmed · `internal/engine/engine.go:216-236`
- Between `cmd.Start()` returning and `currentCmd = cmd`, the signal goroutine sees `currentCmd == nil` and forwards nothing, but `sigCount` still increments (SIGINT) and the SIGTERM path claims it forwarded without doing so.
- **Failure:** two rapid Ctrl+C presses as an iteration starts: press 2 lands in the window, no SIGINT reaches the child, and a third press jumps straight to SIGKILL — the graceful-interrupt level is skipped. `timeout N brr ...` whose SIGTERM lands in the window prints "forwarding" but the child never receives it.
- **Fix:** after publishing `currentCmd`, reconcile pending signal state (TERM flag → forward SIGTERM; `sigCount >= 2` → forward SIGINT/SIGKILL).

### E3 — Windows `terminateTree` can recurse infinitely on Toolhelp parent-PID cycles
- **Severity:** low · likely · `internal/engine/process_windows.go:52`
- The parent→children map is built from a Toolhelp snapshot with no visited set. Windows reuses PIDs and records `ParentProcessID` at creation, so stale links plus PID reuse can form a cycle → unbounded recursion → stack exhaustion mid-cleanup, leaving orphans running.
- **Fix:** track visited PIDs and skip already-visited nodes (also covers pid==parent self-loops).

## Workflow

### W1 — Interrupt swallowed when a stage ends via signal file or max iterations
- **Severity:** medium · confirmed · `internal/workflow/run.go:237`, `internal/engine/engine.go:249,294`
- Both stage runners check signal files before the interrupt flag, and `engine.Run`'s max-iterations exit never re-checks `stopping`. A Ctrl+C during a command stage that already wrote `.brr-cycle` returns `ReasonCycle` and the workflow loops back and keeps running; a Ctrl+C during an agent stage's final iteration yields `ReasonMaxIterations` and the workflow silently advances. Violates `docs/specs/workflow.md` item 29 ("Interrupts stop the workflow with exit code 130 and preserve state").
- **Fix:** in `runCommandStage` check `interrupted.Load()` before `detectSignalFiles()`; have `engine.Run` surface a pending interrupt at loop exit and on signal-file returns so `workflow.Run` stops (state is already saved; resume works).

### W2 — Single command failure recorded as `fail_streak`
- **Severity:** low · confirmed · `internal/workflow/run.go:212,245`
- `runCommandStage` maps any non-zero exit (and Start failure) to `engine.ReasonFailStreak` ("3 consecutive failures"), which is persisted to the event log, state file, and status output for a single deterministic gate failure.
- **Fix:** introduce a distinct reason (e.g. `command_failed`) instead of reusing `ReasonFailStreak`.

### W3 — Duplicate `workflow_started` events on resume
- **Severity:** low · confirmed · `internal/workflow/run.go:32`
- `Run` appends `workflow_started` unconditionally, including on resume with the original RunID, so the event log shows one run started N times with no way to distinguish resumes.
- **Fix:** emit `workflow_started` only for fresh state; emit `workflow_resumed` (with stage id) on the resume path.

### W4 — Negative per-stage `max` silently falls back to default
- **Severity:** low · confirmed · `internal/workflow/load.go:126,162`
- `effectiveMax` treats `stage.Max <= 0` as "unset", so `max: -1` passes validation whenever `defaults.max >= 1` and is silently replaced — neither rejected nor honored, diverging from the documented "≥ 1" rule.
- **Fix:** reject `stage.Max < 0` explicitly in `validateStage`.

### S1 — Stale signal files silently override command-stage results
- **Severity:** high · confirmed · `internal/workflow/run.go:208,237`
- `runCommandStage` checks signal files only *after* the command runs, and neither it nor `workflow.Run` scrubs stale files at entry (`engine.Run` pre-cleans for exactly this reason, but its deferred cleanup never runs on SIGKILL/power loss). A stale file discards the command's real exit status. Note: a stale `.brr-complete` also short-circuits the next *agent* stage via `engine.Run`'s pre-existing-signal check — a workflow-entry scrub fixes both.
- **Failure:** stale `.brr-complete` survives a `kill -9`; on the next `brr workflow run`, `make check` fails (exit 1) but the stage is recorded "completed" and the workflow advances past a failing gate. A stale `.brr-failed` conversely fails a passing gate.
- **Fix:** scrub (or explicitly honor) pre-existing signal files once at `workflow.Run` entry, before any stage starts, so post-Wait detection only sees files created by the stage itself.

### S2 — Double SIGINT delivered to command-stage child on Ctrl+C
- **Severity:** medium · confirmed · `internal/workflow/run.go:228`
- The command-stage child shares brr's foreground process group (no `setProcAttr`), so terminal Ctrl+C reaches it directly — and the forwarding goroutine sends a second SIGINT. Tools with escalating interrupt semantics (pytest, npm, coding agents) treat it as a double interrupt and hard-abort.
- **Fix:** don't re-send `os.Interrupt` to a same-group child (record it only; still forward SIGTERM, which is not tty-broadcast), or move the child to its own process group and forward via `killGroup` for engine parity.

### S3 — Race can misclassify Ctrl+C as stage failure instead of interrupt
- **Severity:** medium · likely · `internal/workflow/run.go:235-241`
- After `cmd.Wait()`, `close(done)` races with a SIGINT still sitting in `sigCh`; the goroutine may exit via `<-done` without setting `interrupted`. Realistic because the tty delivers SIGINT to the child directly, so a fast-dying child returns from `Wait` before the forwarder runs → stage intermittently reported as "error (fail_streak)" instead of "interrupted" for the same keypress.
- **Fix:** after `close(done)`, drain `sigCh` non-blockingly and set `interrupted` for any pending signal; optionally also inspect the wait status for death-by-signal.

### S4 — WatchStatus wipes the final frame when the state file vanishes
- **Severity:** low · confirmed · `internal/workflow/status.go:84`
- Workflow completion calls `store.deleteAll()`, and `--reset` deletes state before the first re-save, so the file legitimately disappears; watch mode clears the screen and replaces the all-green final frame with "No state found", or exits mid-run in the reset window.
- **Fix:** on `IsNotExist`, keep the last rendered frame (with a footer) and tolerate at least one absent tick before exiting.

### S5 — Run diagram always draws the cycle edge from the last stage
- **Severity:** low · confirmed · `internal/workflow/output.go:113`
- The cycle line renders `<last-stage> ↺ <target>` regardless of which stage actually requested the cycle — a structurally wrong flow rendering when a middle stage cycles.
- **Fix:** render the edge from the requesting stage when known, or drop the source label (`↺ <target> (max N, used M)`).

### S6 — `atomicWriteRegularFile` renames without fsync
- **Severity:** low · likely · `internal/workflow/files.go:31`
- No `Sync()` before `os.Rename`; on power loss the rename can become durable before the data blocks, leaving a zero-byte state file. `store.load` then fails to parse and the workflow silently "starts fresh", repeating hours of completed stages.
- **Fix:** `tmp.Sync()` before close (optionally fsync the directory after rename).

## CLI

### C1 — Empty resolved prompt accepted for workflow agent stages
- **Severity:** medium · confirmed · `internal/cli/workflow.go:132`, `internal/cli/root.go:101-103`
- The non-empty check required by `docs/specs/prompt-resolution.md` req 10 lives only in `run()`, not in `resolvePrompt`, so workflow stages (run and validate) never get it. An empty `.brr/prompts/build.md` validates cleanly and then loops an instruction-less agent (ship template: `max: 100`).
- **Fix:** move the trimmed-empty check into `resolvePrompt` itself (erroring with the resolved source); drop the redundant check in `run()`.

### C2 — Workflow error notifications misreport the terminal event
- **Severity:** medium · confirmed · `internal/cli/workflow.go:144`
- On error returns from `workflow.Run`, the CLI notifies with `notify.Send(result)` using the stage's stop reason instead of the actual error: a single command-stage failure notifies "Too many consecutive failures", and a cycle-without-config error notifies "cycle requested" as if the run were continuing. Violates `docs/specs/notifications.md` req 2.
- **Fix:** in the `runErr != nil` branch notify with the real wrapped error (e.g. `notify.SendWorkflowError(runErr)`); pairs with the W2 reason fix.

### C3 — `--notify` skipped for `.brr-cycle` stop in the root command
- **Severity:** low · confirmed · `internal/cli/root.go:119`
- `run()` returns the ".brr-cycle is only supported by 'brr workflow'" error before the notification block, so `brr <prompt> -n` never notifies on a cycle stop — the only req-1 event without a ping.
- **Fix:** dispatch the notification before returning the cycle error.

## Config / platform

### F1 — Viper lowercases profile names; uppercase profiles unreachable
- **Severity:** high · confirmed (reproduced with viper v1.21.0 and the built binary) · `internal/config/config.go:79,103`
- Viper stores map keys lowercased, but the `default:` value and `-p` flag are compared exactly. `default: MyAgent` + `profiles: MyAgent:` fails every invocation with "default profile \"MyAgent\" not found in profiles"; `-p MyAgent` fails the same way.
- **Fix:** viper cannot preserve key case (spf13/viper#1014). Either make profile names explicitly case-insensitive (normalize `cfg.Default` and the `ResolveProfile` lookup, document it) or parse the config layers with `yaml.v3` into case-preserving maps. Prefer whichever keeps the project's viper convention coherent; document the decision.

### F2 — Project profile deep-merges with a same-named global profile, leaking args
- **Severity:** medium · confirmed (reproduced) · `internal/config/config.go:51`
- `MergeConfig` merges recursively, so a project profile that sets `command` but omits `args` silently inherits the global profile's `args` — e.g. `--dangerously-skip-permissions --model opus` leaking into a command that never asked for them.
- **Fix:** treat profiles as atomic units: unmarshal each layer separately and replace whole `Profile` entries from the project layer over the global layer.

### F3 — notify-send body starting with `-` parsed as options (Linux)
- **Severity:** low · likely · `internal/notify/notify_linux.go`
- The agent-controlled body is passed as positional argv to `notify-send`, which parses options anywhere in argv. A body starting with `-` (e.g. `--version mismatch...`) is consumed as an option; the notification silently doesn't render.
- **Fix:** insert `--` before the positional args.

### F4 — Init stage-2 TOCTOU: `.brr` parent symlink not re-verified
- **Severity:** low · likely · `internal/scaffold/scaffold.go:62`
- Stage 2 re-verifies only the leaf `.brr/prompts`; `Lstat` follows a symlinked intermediate `.brr`. Racing `brr init` with a `.brr -> elsewhere` swap after pre-flight makes `MkdirAll` create the tree (and later runs write state) outside the repo.
- **Fix:** re-verify `.brr` itself at stage 2 (or `os.Mkdir` + `Lstat`/`SameFile` instead of `MkdirAll` through a possibly-symlinked parent).

## Security

### X1 — Workflow `prompt:` allows arbitrary out-of-tree file reads
- **Severity:** low · confirmed (reproduced) · `internal/cli/root.go:139` via `internal/workflow/run.go:185` and `load.go:71`
- The traversal guard in `resolvePrompt` protects only the named-prompt branch; the direct-file branch reads any existing regular file at an absolute or `../` path. A cloned repo shipping `prompt: /home/user/.aws/credentials` gets that file read (up to 10 MiB) and piped into the agent's stdin — including by the nominally side-effect-free `brr workflow validate`, whose error text doubles as a file-existence oracle. (Command stages are by-design arbitrary execution, but agent-only workflows and `validate` should not be file-read primitives.)
- **Fix:** resolve workflow-sourced prompts through a restricted resolver: allow named prompts and repo-relative regular files; reject absolute paths and `..`.

### X2 — Unbounded `ReadRegularFile` enables OOM via planted files
- **Severity:** low · confirmed · `internal/fsutil/fsutil.go:45`, call sites `config.go:50`, `load.go:40,50`, `state.go:30`, `scaffold.go:206`
- Prompt and signal reads are deliberately capped (10 MiB / 4 KiB) but config, workflow YAML, state JSON, and `.gitignore` reads use uncapped `io.ReadAll`. A multi-GB planted state file OOMs even read-only commands like `brr workflow status`.
- **Fix:** add a bounded variant of `ReadRegularFile` (`maxBytes` + `io.LimitReader`) and apply sane caps at these call sites.

---

## Verified sound (no findings)

Traced and explicitly cleared by the auditors: the Unix/Windows lock TOCTOU dance (Lstat→O_NOFOLLOW open→SameFile chain, no fd leaks); `checkSignalFiles` fd handling and branch priority; `currentCmd` mutex/atomic discipline; `readCappedFromFile` UTF-8 boundary logic; per-iteration stdin readers; resume/NextStageID bookkeeping and `validResumeState` corruption fallback; ShipTemplate vs loader validation; `appendRegularFile` O_EXCL/SameFile handling; prompt-resolution precedence vs spec; exit-code table vs spec (including approval→0 and interrupt→130); JSONL event encoding (no newline injection); osascript escaping on darwin; ANSI gating on stderr TTY.
