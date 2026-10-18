# Make automatic redraw the default and finish the frame-loop wiring

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
Blocked by: 03, 09

## Question

Every window (real and test) redraws automatically like upstream, without opting in through `OnRedraw` or `TestApp.SetFrameLoop(true)`.

## Scope

- Ticket 03's recorded follow-ups: in `gpui/view.go` `withRenderedView`, call `recordRenderedView(w)` after pushing onto `frame.renderedViews` and drop the `activeFrame`/`recordRenderedView` block in `App.Update`; `gpui/focus.go` `requestRefresh` calls `w.markFrameDirty()`; accessed-entity tracking during draws in `gpui/entity.go` fed into `completeFrameTracking` (R:window.rs:189-240).
- Default-on: real windows draw+present without a user hook (R:window.rs:1715-1843); the test flush draws dirty windows (R:app.rs:1800-1812). Migrate the corpora that assume manual redraws (`internal/authorspec` counter, `internal/assetspec`/`internal/svgspec` RefreshRequested asserts) to the upstream semantics, preserving what they prove.

- Ticket 09's recorded follow-ups: `render.go` clears `refreshRequested` after the frame-end hook (a focus-listener refresh is lost) — call the frame-finish hook explicitly after `endDispatchFrame`; `window.go` `OpenWindow` forwards OS activation to `window.activationChanged` before the user callback (else `ObserveWindowActivation` never fires on real windows); `key_dispatch.go` updates `inputState(w).modifiers` on KeyDown/KeyUp/ModifiersChanged; `accessibility.go` `canonicalElementID` covers NamedInteger/NamedChild/CodeLocation; `Div.ID` accepts the new id kinds.

## Acceptance criteria

- [ ] Upstream draw-on-flush semantics with no opt-in; the migrated corpora still prove their original properties.
- [ ] The full corpus green apart from the documented environmental classes; evidence in `evidence/base/ticket33-*.json`.

## Comments

- 2026-10-19 — Opened by the orchestrator from ticket 03's honest deviations.
