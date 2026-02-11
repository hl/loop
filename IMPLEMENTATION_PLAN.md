# Implementation Plan

## time-greeting spec (`docs/specs/time-greeting.md`)

- [ ] **Create `index.html`** — Single file with inline JS/CSS. Time-based greeting logic using `Date` object (morning 05–11:59, afternoon 12–16:59, evening 17–20:59, night 21–04:59). Greeting centered on page. Cross-browser compatible.
  - Files: `index.html` (new)

- [ ] **Update `AGENTS.md` validation commands** — Current commands reference Elixir (`mix compile`, `mix format`, `mix test`) but the project contains only a static HTML file. Replace with appropriate validation (e.g. HTML syntax check, or note that no build step is needed).
  - Files: `AGENTS.md`
