# Add keyed state, globals, notify-by-id and window-bound context helpers

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: base-port orchestrator (wave 1a)
Blocked by: none

## Question

Base can use the small pinned App/Window/Context helpers it calls everywhere.

## Scope

- `UseKeyedState`/`UseState` with eviction release (R:window.rs:4093,4122,4140); `App.Notify(EntityID)` (R:app.rs:2920; `gpui/app.go:521` unexported); `HasGlobal/TryGlobal/DefaultGlobal` (R:app.rs:2106-2139); `SubscribeIn`, `SpawnIn`, `DeferIn`, `Window.Defer` (R:app/context.rs:305,355,676; R:window.rs:2510); `OnFocus/OnBlur/OnFocusIn/Out`, `ObserveWindowActivation` (R:app/context.rs:444,547-642); `ListenerFor` (R:window.rs:6868); app-level focus handles (R:app.rs:2796); view downcast + `Window.Root[T]` (R:view.rs:53; R:window.rs:2272); `Window.Modifiers/LineHeight` (R:window.rs:3127,2983); NamedChild/NamedInteger element ids (R:window.rs:7667-7782); `Axis`, `Anchor`, `Bounds.Contains/FromAnchorAndSize`, `MouseMoveEvent.Dragging`, `ScrollDelta.PixelDelta/Precise` (R:geometry.rs:26,868,1465; R:interactive.rs:515,606,614).

## Acceptance criteria

- [x] Unit tests per helper derived from pinned semantics; keyed state releases on frame-end eviction.
- [x] Every ported behavior cites the pinned Base source (`upstream-base/ce/crates/base/src/...`, c0820693) or the core pin (`reference/ce-source/crates/gpui/src/...`, 254b5dbd); no invented APIs.
- [x] Upstream tests in scope are ported (count ported / deviated / not-applicable recorded with reasons); real-window tests FAIL rather than skip.
- [x] No native artifact change (rev 11 stays frozen); any genuine native need is stopped and surfaced to the user with evidence.
- [x] `CGO_ENABLED=0 go test` for the touched packages is green, `go vet` + gofmt clean, and the core regression packages stay green; executed evidence in `evidence/base/ticketNN-*.json`.

## Research

- [slice-01-infra-overlays.md §C7,C10](../research/slice-01-infra-overlays.md)
- [slice-03-composite-controls.md C4-C6,C11,C12,C16](../research/slice-03-composite-controls.md)
- [slice-05-input-editor.md §4,6](../research/slice-05-input-editor.md)

## Comments

- 2026-10-19 — Created by the GPUI Base port chat from the merged slice inventories (R: = `reference/ce-source/crates/gpui/src`).

## Answer

Resolved 2026-10-19 (wave 1a worker, verified by the orchestrator: 21 TestHelper tests + focusspec/listspec/actionspec/dropspec). All listed helpers landed with pinned citations; keyed state releases at frame end and on window close; window-free focus handles work across windows. Cross-file wiring the worker could not touch is carried by [ticket 33](33-frame-loop-default.md). Evidence: [evidence/base/ticket09-state-context-helpers.json](../../../evidence/base/ticket09-state-context-helpers.json).
