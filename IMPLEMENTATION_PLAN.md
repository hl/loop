# Implementation Plan

## time-greeting spec (`docs/specs/time-greeting.md`)

- [ ] **Update `AGENTS.md` validation commands** — Current commands reference Elixir (`mix compile`, `mix format`, `mix test`) but the project contains only a static HTML file. Replace with appropriate validation (e.g. HTML syntax check, or note that no build step is needed).
  - Files: `AGENTS.md`
  - Note: HTML Tidy can validate HTML syntax if available on the system
