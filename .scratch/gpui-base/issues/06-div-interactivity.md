# Deliver clicks, hover, mouse-down and drag sources from divs

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: base-port orchestrator (wave 1b)
Blocked by: 05

## Question

Div `OnClick` fires for mouse and keyboard (Enter/Space) clicks; divs support mouse-down/out/up-out listeners, hover + active/focus-visible/group styles, drag sources with previews, pointer capture and per-hitbox cursors.

## Scope

- `clickListeners` are stored but never dispatched (`gpui/div.go:620-622,944`); `ClickEvent` is `{Button}` only (`gpui/div.go:542`).
- Port: click pairing + keyboard click (R:elements/div.rs:1814,3253,3452-3560; R:interactive.rs:220-290); `on_mouse_down`/`on_any_mouse_down`/`on_mouse_down_out`/`on_mouse_up_out` (R:elements/div.rs:996,1039,1101,1128); `on_hover` (:1899); hover/active/group styles (:901,980); `on_drag`/`on_drag_move`/`DragMoveEvent` (:703,1194,1844) + preview paint (R:window.rs:3439-3456); per-hitbox `set_cursor_style` (R:window.rs:3844) + div cursor builders; Win32 `SetCapture`/`ReleaseCapture` (Go user32 syscall, not the DLL).

## Acceptance criteria

- [x] Deterministic dispatch transcripts for click/hover/drag ordering; real-window synthetic-message tests (never the real mouse).
- [x] Every ported behavior cites the pinned Base source (`upstream-base/ce/crates/base/src/...`, c0820693) or the core pin (`reference/ce-source/crates/gpui/src/...`, 254b5dbd); no invented APIs.
- [x] Upstream tests in scope are ported (count ported / deviated / not-applicable recorded with reasons); real-window tests FAIL rather than skip.
- [x] No native artifact change (rev 11 stays frozen); any genuine native need is stopped and surfaced to the user with evidence.
- [x] `CGO_ENABLED=0 go test` for the touched packages is green, `go vet` + gofmt clean, and the core regression packages stay green; executed evidence in `evidence/base/ticketNN-*.json`.

## Research

- [slice-02-simple-controls.md §2-3](../research/slice-02-simple-controls.md)
- [slice-04-scroll-lists-dock.md G7,G10,G11,G13](../research/slice-04-scroll-lists-dock.md)

## Comments

- 2026-10-19 — Created by the GPUI Base port chat from the merged slice inventories (R: = `reference/ce-source/crates/gpui/src`).

## Answer

Resolved 2026-10-19 (wave 1b worker, verified by the orchestrator: 21 TestInteract tests incl. a real-window capture test + dropspec/focusspec/scenespec/layoutspec). The pinned ClickEvent (mouse pairing + Enter/Space keyboard click), the full mouse-listener/hover/active/group/focus-visible builder set, drag sources with the 2px threshold and DragMoveEvent, state-style computation, per-hitbox cursor + pointer capture (SetCapture), the double-click counter, and a fixed dispatch-node pop bug. Upstream tests: 10 ported, 2 deviated, 4 n/a (tooltips). **Finding:** real windows never route mouse button messages into element dispatch; that and the remaining render/key/window wiring are [ticket 34](34-real-window-input.md). Evidence: [evidence/base/ticket06-div-interactivity.json](../../../evidence/base/ticket06-div-interactivity.json).
