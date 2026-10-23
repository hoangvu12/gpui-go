# Sheet clipping inside scroll areas; glyph fallback for symbols

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: claimed
Assignee: base-port-2 orchestrator (2026-10-23)
Blocked by: none

## Question

Found by the base-gallery build:
1. A Sheet inside a scroll area is clipped by it. Dialog is deferred and Sheet isn't. Compare with upstream sheet.rs and dialog.rs, and port whatever upstream does.
2. ▸ ▾ ▴ ⌕ render as missing glyphs in the default UI font. Find whether the pinned text stack does font fallback (system fallback fonts) and whether the frozen native text service supports it. If it's a native gap, record it for the user rather than changing the DLL.

## Acceptance criteria

- [ ] Findings cited; fixes only where the Go side can make them; tests headless.

## Comments

- 2026-10-23 — Created and claimed by the orchestrator from the gallery worker's report.
