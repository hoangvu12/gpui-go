# Sheet clipping inside scroll areas; glyph fallback for symbols

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
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

## Answer

Both behaviours match upstream.
- Sheet: upstream's base Sheet isn't deferred either (sheet.rs:123), so callers wrap it in `gpui.Deferred`. A test now documents this.
- Symbol glyphs: the pinned Windows text stack has no per-character system fallback, so ▸ ▾ ⌕ are missing upstream too. Naming `Fallbacks: ["Segoe UI Symbol"]` on the element fixes it with the rev-11 DLL. Automatic fallback would need a native change, which is recorded for the user.

Evidence: [ticket42-sheet-glyph-fallback.json](../../../evidence/base/ticket42-sheet-glyph-fallback.json).
