# Core: app-atlas image sprites and the shaped-run cap latch

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: claimed
Assignee: base-port-2 orchestrator (2026-10-23)
Blocked by: none

## Question

Found by the base-gallery build (examples/base-gallery):
1. An image retained in the app's image atlas (an SVG task-list tick in Markdown, and likely any img()) fails at present: "polychrome sprite batch needs texture 0". The present loop passes only the window's glyph atlas. The pin draws sprites from any atlas texture.
2. A window can hold only about 256 live shaped text runs across the current and previous frame. Going over that makes every later frame fail permanently ("measure callback failed (latched)"), and stopping the host then panics. The native shaping-handle cap is frozen (rev 11). The Go side must not exceed it in normal use (dispose superseded lines, evict, reuse) and must never latch a permanent failure.

## Acceptance criteria

- [ ] Pinned behaviour cited; no native change.
- [ ] Headless tests reproduce both, then pass. No visible windows (the user is gaming).
- [ ] gpui and the spec packages that present or draw text stay green.

## Comments

- 2026-10-23 — Created and claimed by the orchestrator from the gallery worker's report.
