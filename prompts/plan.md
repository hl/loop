Spawn a Task agent (subagent_type: Explore, model: sonnet) to study `docs/specs/*`, `AGENTS.md`, `IMPLEMENTATION_PLAN.md` (if present), and source code (`lib/*`, `test/*`). Don't assume functionality is missing — confirm with code search first.

Compare current source code (`lib/*`, `test/*`) against `docs/specs/*`. Identify gaps: TODOs, minimal implementations, placeholders, skipped/flaky tests, and inconsistent patterns.

Create or update IMPLEMENTATION_PLAN.md as a prioritized bullet list:
- Priority: blockers/dependencies first, then core functionality, then refinements
- Size: each task should be completable in one build iteration
- Format: note which files/modules each task touches (the build phase uses this to assess parallelism)
- Approval: tag tasks that require human approval per AGENTS.md Decision Authority (new migrations, new routes, architectural changes, new runtime deps) with `[APPROVAL]`
- Hygiene: remove completed tasks to keep the list actionable

Stage and commit: `git add IMPLEMENTATION_PLAN.md && git commit -m "docs(plan): update implementation plan"`. If there are no changes to commit, skip this step.

IMPORTANT: Plan only. Do NOT implement anything.
