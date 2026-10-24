# 16 - The full drag and drop sequence

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 8 (Claude Code)
Blocked by: 09, 10, 11
Triage: ready-for-agent

**What to build:** Drag and drop follows the HTML sequence: drag start, drag, enter, over (with an effect), leave, drop and end. Escape and window deactivation cancel a drag, and files dragged from other apps show 'none' over elements that won't accept them.

**Blocked by:** 09 (Default actions run after key listeners), 10 (Hover: pointer enter/leave, window leave, survives typing), 11 (Pointer capture retargeting and local position)

**Status:** ready-for-agent

- [ ] Event-log fixtures for an internal drag across three targets match Chrome's sequence.
- [ ] Escape mid-drag fires leave and end with no drop.
- [ ] OLE drag-over reports none over a non-accepting target (dropspec test).
- [ ] zterm's drag workarounds can be removed (a follow-up note, not required here).

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer

**API** — the DOM drag events layer on the pin's drag machinery (`gpui/dragevent.go`), dispatched by a per-window drag session:
- `OnDragStart`/`OnDragEvent`/`OnDragEnd` (source chain; a prevented dragstart cancels the start), `OnDragEnter`/`OnDragOverEvent`/`OnDragLeave`/`OnDropEvent` (target chain). Names with the `Event` suffix avoid the pin's `OnDragOver` (style) and `OnDrop` (value delivery). `AnyDrag.EffectAllowed` (zero = all, the DOM "uninitialized"), `DragEffect`/`DragEffects`, mixin + TSX-free.
- Event order (Chromium `event_handler.cc:1375-1478`): the threshold move fires dragstart, drag, dragenter, dragover (the CDP controller's DragEnter-then-DragOver pair, `input_handler.cc:1044-1059`); same-target moves fire drag + dragover; a change fires drag, dragenter (new), dragleave (old) — no dragover; the move after it fires dragover alone; the up fires drop (accepting target only) then dragend. A refused release is the cancel sequence: drag, dragleave, dragend-none (the `dnd-drop-refused` record).
- Effect negotiation (`drag_controller.cc:918-926`, `data_transfer.cc`): enter/over start at the effectAllowed-derived default (every→copy, move, copy, link), a listener may set `e.DropEffect`, acceptance = a cancelled dragover/dragenter or the pin's drop-listener query; the negotiated op is masked against the source. A dragenter that fired with a dragleave reads `none` after the dispatch (the shared DataTransfer reset, `mouse_event_manager.cc:1176`). Cadence: per drag-loop update (per mouse move) — no renderer timer (TryDHTMLDrag runs per DragTargetDragOver).
- Cancel paths: Escape is consumed by the drag machinery before any listener/binding/default action (the OLE loop owns the keyboard; `consumeDragCancelKey` in `key_dispatch.go` — composes with ticket 22's close-watcher default, the drag wins); window deactivation cancels (`helpers_focus.go activationChanged`); pointercancel; all fire drag, dragleave, dragend-none, no drop.
- OLE: `IDropTarget::DragEnter/DragOver/Drop` now report the session's acceptance (COPY over a target that takes the drop, NONE otherwise — `dragdrop_windows.go` `deliver` returns the answer through `hostWindow.onFileDrop`, `window.go` reads `oleDropEffectAnswer`). The pin's unconditional COPY (spec story 56's divergence) is gone.

**Files**: `gpui/dragevent.go` (the events, session, dispatch, builders), `gpui/dragevent_test.go`; `gpui/interact.go:1453` (dragstart arm), `gpui/interact.go:1471` (the drop arm's dragenter/dragover acceptance), `gpui/interact.go:1696` (`paintDragEventListeners`); `gpui/mouseevent.go:1100` (the drag tail); `gpui/key_dispatch.go:826` (Escape); `gpui/helpers_focus.go:326` (deactivation); `gpui/filedrop.go:163` (AnyDrag fields, the OLE session in `translateFileDrop`); `gpui/dragdrop_windows.go` (the effect answers); `gpui/interact_mixin.go:158` (forwarders).

**Fixtures** (headless Chrome 155, CDP mouse events + `Input.cancelDragging`): `internal/parity/fixtures/dnd` — `dnd-across` (34 events, the full order incl. drop:move/dragend:move), `dnd-drop-refused`, `dnd-escape` (drag, dragleave, dragend, no drop, no key entries); one wrong-view test (no dragover acceptance → Tier B fails, Tier A passes). Harness: the drag kinds with the `effect` field, `log.FinalizeDragEntries` (the events' final state), `parity.CancelDrag()` (recorder: `Input.cancelDragging`; replay: the consumed Escape keydown). dropspec: `TestOLEDragOverNegotiatesNoneOverNonAcceptingTarget` (NONE over the plain region, COPY over the accepting, no payload on the refused drop).

**Key citations**: event order — `third_party/blink/renderer/core/input/event_handler.cc:1375-1478`; dragstart/dragend/cancelable/defaults/reset — `core/input/mouse_event_manager.cc:1051, 1120-1140, 1176, 1181-1198`; effects — `core/page/drag_controller.cc:870-889, 918-926` + `core/clipboard/data_transfer.cc` (uninitialized→every; every→copy) + `public/common/page/drag_operation.h:43-47` (copy 1, link 2, move 16); CDP drag — `content/browser/devtools/protocol/input_handler.cc:928-991` (HandleMouseEvent drives it), `:1670-1683` (CancelDragging), `Input.pdl:230`.

**zterm workarounds** (removable now, a follow-up note — `examples/zterm/drag.go`'s header): (1) the root takes every `OnDragMove`, hit-finds the target from laid-out bounds and runs `State.DragOver`, which also emulates dragleave; (2) the root's capture-phase mouse up defers `State.EndDrag` — no drag-end callback existed. Both map onto `OnDragEnter`/`OnDragOverEvent`/`OnDragLeave`/`OnDragEnd` now.

**Limitations**: the drag threshold stays the pin's 2px Euclidean (`div.rs:65`), not Chrome's 4px-per-axis (`mouse_event_manager.cc:117-126`) — not fixture-observable; dragstart/drag carry no dropEffect field (the record reads none for them); external drags still accept CF_HDROP only (the pin's QueryGetData arm — text/URL/HTML stay refused); the pin's OnDrop value delivery still runs in the up's bubble (the DOM drop event fires after it, still before dragend); no stationary-mouse dragover timer (per-move, like Chromium's per-DragOver dispatch).
