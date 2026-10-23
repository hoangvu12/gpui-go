# TextView: tables, inline HTML paragraphs, hard tabs

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: claimed
Assignee: base-port-2 orchestrator (2026-10-23)
Blocked by: none

## Question

Found by the base-gallery build:
1. In both Markdown and HTML, every table row renders on one horizontal line.
2. An HTML `<p>` with `<strong>`/`<em>`/`<code>`/`<a>` across a line break renders as an empty grey bar.
3. A hard tab draws as a missing-glyph box in the Editor and in TextView code blocks. Check what upstream and the pin do with tabs (tab expansion in shaping or in the input display map).

## Acceptance criteria

- [ ] Each fix cites upstream or the pin. Headless tests reproduce each bug, then pass.
- [ ] `go test ./base -skip Real` green.

## Comments

- 2026-10-23 — Created and claimed by the orchestrator from the gallery worker's report.
