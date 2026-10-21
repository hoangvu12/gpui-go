# Make automatic redraw the default and finish the frame-loop wiring

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: base-port orchestrator (wave 1b)
Blocked by: 03, 09

## Question

Every window (real and test) redraws automatically like upstream, without opting in through `OnRedraw` or `TestApp.SetFrameLoop(true)`.

## Scope

- Ticket 03's recorded follow-ups: in `gpui/view.go` `withRenderedView`, call `recordRenderedView(w)` after pushing onto `frame.renderedViews` and drop the `activeFrame`/`recordRenderedView` block in `App.Update`; `gpui/focus.go` `requestRefresh` calls `w.markFrameDirty()`; accessed-entity tracking during draws in `gpui/entity.go` fed into `completeFrameTracking` (R:window.rs:189-240).
- Default-on: real windows draw+present without a user hook (R:window.rs:1715-1843); the test flush draws dirty windows (R:app.rs:1800-1812). Migrate the corpora that assume manual redraws (`internal/authorspec` counter, `internal/assetspec`/`internal/svgspec` RefreshRequested asserts) to the upstream semantics, preserving what they prove.

- Ticket 09's recorded follow-ups: `render.go` clears `refreshRequested` after the frame-end hook (a focus-listener refresh is lost) — call the frame-finish hook explicitly after `endDispatchFrame`; `window.go` `OpenWindow` forwards OS activation to `window.activationChanged` before the user callback (else `ObserveWindowActivation` never fires on real windows); `key_dispatch.go` updates `inputState(w).modifiers` on KeyDown/KeyUp/ModifiersChanged; `accessibility.go` `canonicalElementID` covers NamedInteger/NamedChild/CodeLocation; `Div.ID` accepts the new id kinds.

## Acceptance criteria

- [x] Ticket 09's Div.ID item is handled by ticket 05; upstream draw-on-flush semantics with no opt-in; the migrated corpora still prove their original properties.
- [x] The full corpus green apart from the documented environmental classes; evidence in `evidence/base/ticket33-*.json`.

## Comments

- 2026-10-19 — Opened by the orchestrator from ticket 03's honest deviations.

## Answer

Resolved 2026-10-19 (wave 1b worker, verified by the orchestrator on the combined 06+33 tree: `go test ./gpui` ok, every internal/* + authoring + conformance package ok except desktopspec `TestCursorStyleAndVisibilityAppLevel`, which fails identically on the pre-wave commit 5684a70 — the documented environmental cursor class). Automatic redraw is the default like upstream (test flush draws dirty windows; WM_PAINT draws any dirty window); accessed-entity tracking; all ticket 03/09 cross-file wiring; corpora migrated with their properties preserved (listed in the evidence); a pre-existing real-window text-atlas slot leak fixed. Deviations: no default present without an app-owned renderer hook; the flush draws only test windows; load completions notify immediately. Evidence: [evidence/base/ticket33-frame-loop-default.json](../../../evidence/base/ticket33-frame-loop-default.json).
