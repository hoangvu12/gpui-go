# Redraw windows automatically on notify, refresh and animation frames

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: base-port orchestrator (wave 1a)
Blocked by: none

## Question

A view that calls `cx.Notify()` or requests an animation frame is redrawn by the real Win32 host without the app driving `DrawWindowFrame` by hand.

## Scope

- Today `RequestAnimationFrame`/`requestRefresh` only set `refreshRequested` (`gpui/focus.go:525`), `effRefresh` discards `DirtyWindows()` (`gpui/app.go:501-506`), `Notify` never dirties a window, and no production `InvalidateRect` exists.
- Port: rendered-view tracking per window → notify marks dirty; `Window.Refresh` (R:window.rs:2287), `OnNextFrame` (R:window.rs:2613), `RequestAnimationFrame` pacing (R:window.rs:2633), `Context.OnNextFrame` (R:app/context.rs:292); the host consumes the dirty set (InvalidateRect / foreground redraw).
- Test side: `RunUntilParked`/`AdvanceClock` redraw dirty test windows (the `simulate_next_frame` contract, R:window.rs:2643).

## Acceptance criteria

- [x] A real-window test: notify → repaint observed without an explicit draw call; an animation-frame loop stops requesting frames when idle.
- [x] `examples/hello` no longer needs its manual redraw loop (or documents why it keeps it).
- [x] Every ported behavior cites the pinned Base source (`upstream-base/ce/crates/base/src/...`, c0820693) or the core pin (`reference/ce-source/crates/gpui/src/...`, 254b5dbd); no invented APIs.
- [x] Upstream tests in scope are ported (count ported / deviated / not-applicable recorded with reasons); real-window tests FAIL rather than skip.
- [x] No native artifact change (rev 11 stays frozen); any genuine native need is stopped and surfaced to the user with evidence.
- [x] `CGO_ENABLED=0 go test` for the touched packages is green, `go vet` + gofmt clean, and the core regression packages stay green; executed evidence in `evidence/base/ticketNN-*.json`.

## Research

- [slice-01-infra-overlays.md §C1](../research/slice-01-infra-overlays.md)
- [A-infra-overlays.md §0.1](../research/A-infra-overlays.md)

## Comments

- 2026-10-19 — Created by the GPUI Base port chat from the merged slice inventories (R: = `reference/ce-source/crates/gpui/src`).

## Answer

Resolved 2026-10-19 (wave 1a worker, verified by the orchestrator: 10 tests incl. 2 real-window, plus assetspec/svgspec/authorspec/accessspec/inspectorspec and the examples build). Notify on a rendered view dirties and wakes its window; Refresh/OnNextFrame/RequestAnimationFrame/RefreshWindows follow the pin; WM_PAINT runs the pinned frame request; vblank-paced InvalidateRect. **Deviation:** auto-redraw is opt-in (real windows with an `OnRedraw` present hook; `TestApp.SetFrameLoop(true)`), so existing manual-redraw corpora stay valid — [ticket 33](33-frame-loop-default.md) makes it the default and wires the view/focus/entity hooks ticket 09's files hold. Evidence: [evidence/base/ticket03-frame-loop.json](../../../evidence/base/ticket03-frame-loop.json).
