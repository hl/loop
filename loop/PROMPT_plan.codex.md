You are one iteration of an autonomous planning loop. Plan only. Do NOT implement anything.

Read:
- `docs/specs/*` — all spec files
- `IMPLEMENTATION_PLAN.md` — task list and status (if present)
- Source code: `lib/*`, `test/*` (use `rg` to confirm what exists)

Compare current source code (`lib/*`, `test/*`) against `docs/specs/*`. Identify gaps: TODOs, minimal implementations, placeholders, skipped/flaky tests, and inconsistent patterns.

Create or update IMPLEMENTATION_PLAN.md as a prioritized bullet list:
- Priority: blockers/dependencies first, then core functionality, then refinements
- Size: each task should be completable in one build iteration
- Format: note which files/modules each task touches (the build phase uses this to assess parallelism)
- Hygiene: remove completed tasks to keep the list actionable

Stage and commit: `git add IMPLEMENTATION_PLAN.md && git commit -m "update implementation plan"`. If there are no changes to commit, skip this step.
