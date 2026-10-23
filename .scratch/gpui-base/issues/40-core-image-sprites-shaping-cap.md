# Core: app-atlas image sprites and the shaped-run cap latch

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
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

## Answer

**Sprites.** Images and SVG now go into the window's own sprite atlas, which the present loop already passes, as the pin does (window.rs:4865/5009/5104). `DropImage` removes the image from every window's atlas.

**Shaping cap.** The native handle bound is 256 per process. Cache-owned lines now sit in an LRU and are parked when that bound is hit, then re-shaped when next queried. The live count stays at or under 256, and a failed frame no longer latches. A failed first draw in `OpenWindowView` is logged and retried, where it used to panic the host.

Headless tests cover all three. Orchestrator re-run on a snapshot: gpui (real-host tests skipped), 6 spec packages, base and the gallery are ok. Real-window checks are left until the machine is free. Evidence: [ticket40-core-image-sprites-shaping-cap.json](../../../evidence/base/ticket40-core-image-sprites-shaping-cap.json).
