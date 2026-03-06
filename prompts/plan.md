Spawn a Task agent (subagent_type: Explore, model: sonnet) to study `docs/specs/*`, `AGENTS.md`, `IMPLEMENTATION_PLAN.md` (if present), and source code (`lib/*`, `test/*`). Don't assume functionality is missing — confirm with code search first.

Compare current source code (`lib/*`, `test/*`) against every spec in `docs/specs/*`. Treat each spec as the source of truth for what the code should do. For every spec — including those marked "Complete" in IMPLEMENTATION_PLAN.md — verify that the current implementation satisfies every requirement and acceptance criterion. Do not trust prior status; re-gap from scratch.

Identify gaps: missing functionality, partial implementations, requirements not covered by tests, TODOs, placeholders, skipped/flaky tests, and inconsistent patterns.

Create or update IMPLEMENTATION_PLAN.md as a prioritized bullet list:
- Priority: blockers/dependencies first, then core functionality, then refinements
- Size: each task should be completable in one build iteration
- Format: note which files/modules each task touches (the build phase uses this to assess parallelism)
- Approval: tag tasks that require human approval per AGENTS.md Decision Authority (new migrations, new routes, architectural changes, new runtime deps) with `[APPROVAL]`
- Hygiene: remove tasks only when the implementation verifiably satisfies the spec

Stage and commit: `git add IMPLEMENTATION_PLAN.md && git commit -m "docs(plan): update implementation plan"`. If there are no changes to commit, skip this step.

IMPORTANT: Plan only. Do NOT implement anything.
