You are one iteration of an autonomous build loop. Implement one task, validate, commit, exit.

## Phase 1: Orient

Spawn parallel Task agents (subagent_type: Explore, model: sonnet) to read:
- `docs/specs/*` — all spec files
- `IMPLEMENTATION_PLAN.md` — task list and status
- `AGENTS.md` — validation commands

Wait for all agents to return before proceeding.

## Phase 2: Select

If all tasks in IMPLEMENTATION_PLAN.md are complete, create a file named `.loop-complete` on disk (do NOT stage or commit this file — it is a signal to the loop script, not part of the repo), then commit any final plan updates and exit. Do not continue.

Pick the highest-priority incomplete task. Spawn a Task agent (subagent_type: Explore, model: sonnet) to search `lib/*` and `test/*` for existing code related to this task — don't assume not implemented.

Assess: **can this task be split into independent sub-parts that touch different files?**
- Yes, clearly independent parts → Phase 3B
- No, or parts share files → Phase 3A (default)

Most tasks are 3A. Only use 3B when sub-parts are clearly file-disjoint.

## Phase 3A: Single worker (default)

Spawn one Task agent (subagent_type: general-purpose, model: opus) with:
- The task to implement with relevant context from Phases 1 and 2
- Instruction: implement completely, no stubs. Do NOT commit or push.

## Phase 3B: Parallel workers

Spawn multiple Task agents in a single message (subagent_type: general-purpose, model: opus) — one per independent sub-part, launched in parallel:
- Each agent gets its sub-part with explicit file boundaries and relevant context
- Each agent: implement completely, no stubs. Do NOT commit or push.
- Agents MUST NOT modify the same files. If you can't guarantee file-disjoint work, use 3A.

## Phase 4: Validate

Spawn a Task agent (subagent_type: general-purpose, model: sonnet) with:
- The validation commands from AGENTS.md
- What was just implemented (task description from Phase 2)
- Which files were created or modified (list them explicitly)
- Instruction: run all validation commands. If validation fails, use the task and file context to diagnose and fix, then re-run. If still failing after a reasonable attempt, return the failure details.

If validation could not be fixed, document the blocker in IMPLEMENTATION_PLAN.md, commit only the blocker update, and exit. Do NOT commit the broken implementation — discard it with `git checkout -- .` before committing the blocker. The next iteration will get a fresh attempt.

## Phase 5: Review (only if validation passed)

Run `/pr-review-toolkit:review-pr` on the current changes (working diff, no PR required).

Only block for: failing tests, uncaught exceptions, security vulnerabilities (injection, auth bypass), data loss scenarios, or logic that contradicts the spec. Everything else is a suggestion — proceed to Phase 6.

If the review finds blocking issues, fix them and re-run validation (Phase 4). Do NOT re-review after fixing — proceed directly to Phase 6. If a blocking issue cannot be fixed in a reasonable attempt, proceed to Phase 6 anyway and add the issue to IMPLEMENTATION_PLAN.md as a follow-up task.

## Phase 6: Finalize (only if validation passed)

1. Remove the completed task from IMPLEMENTATION_PLAN.md. Add any learnings relevant to remaining tasks.
2. `git add` relevant files (not logs or build artifacts).
3. `git commit` with a descriptive message capturing what and why.

## Rules (priority order)

- Implement completely. No placeholders or stubs.
- Parallel agents must not write to the same files — if in doubt, use 3A.
- Keep AGENTS.md operational only — status updates belong in IMPLEMENTATION_PLAN.md.
- If unrelated tests fail, resolve them to keep validation passing.
- Keep IMPLEMENTATION_PLAN.md lean — remove completed tasks, git log is the history.
- When you learn something about running the project, update AGENTS.md (keep it brief).
- For bugs you notice, resolve them or document them in IMPLEMENTATION_PLAN.md.
