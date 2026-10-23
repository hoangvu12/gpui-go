# TextView: tables, inline HTML paragraphs, hard tabs

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
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

## Answer

**Tables.** This was map gap 2b: upstream's table frame and row track are plain divs, which are Block under 0.2.2. They now use `textBlockDiv()`. Scrollable views also wrap each item in a full-width block, so tables and code backgrounds fill the width.

**HTML paragraphs.** Upstream gives every paragraph without a key the same id, `"p"`. HTML nodes have no spans, so those paragraphs shared one stored layout, and the last one rendered painted over the others. Paragraphs without a key now get their own id, numbered in render order within each top-level block. This is a recorded deviation.

**Tabs.** Not fixed. The pin paints `\t` as the missing glyph, while Base's 0.2.2 line (DirectWrite) drew blank space. The fix belongs in gpui shaping, so it's the user's decision.

Orchestrator re-run on a snapshot: `go test ./base -skip Real`, the 4 new tests and the gallery are ok. Evidence: [ticket41-textview-tables-html-tabs.json](../../../evidence/base/ticket41-textview-tables-html-tabs.json).
