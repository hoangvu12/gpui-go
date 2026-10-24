# 11 - Pointer capture retargeting and local position

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 6 (Claude Code)
Blocked by: 01
Triage: ready-for-agent

**What to build:** While an element holds pointer capture, pointer events go to it alone, with got/lost-capture callbacks; losing capture to another window cancels cleanly; events carry an element-local position (D12).

**Blocked by:** 01 (Browser-parity harness (tracer))

**Status:** ready-for-agent

- [x] Event-log fixtures with setPointerCapture match Chrome.
- [x] A simulated capture change fires lost-capture and leaves no button stuck.
- [x] A local-position test replaces the hitbox-bounds workaround in one example.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer

### API

- **`Window.CapturePointer(id HitboxID) bool`** (`gpui/pointercapture.go:195`) — the pin's entry point (window.rs:3098 capture_pointer), extended to the browser's pending-capture semantics instead of a parallel API: the request is recorded as the *pending* capture, accepted only while a button is held (returns false otherwise — Chromium's IsActiveButtonsState gate), and it takes effect — firing `gotpointercapture` — at the start of the next dispatched pointer event. While it holds, pointer events go to the captured element **and its ancestors** alone: the window's hit test is retargeted to the capture chain (Chromium's GetEffectiveTargetForPointerEvent), so `HitboxID.IsHovered` (`gpui/mouseevent.go:348`) answers true for the chain wherever the pointer is and false for every other element — hover styles and listeners included. The capture's cross-frame identity is the element's GlobalElementID path, so a re-render mid-drag keeps it alive (the pin's hitbox id is per-frame and died with the frame); an id-less element's capture keeps the pin's id-only semantics and, like the pin, survives only the frame it was set in.
- **`Window.ReleasePointer()`** (`gpui/pointercapture.go:220`) — clears the pending capture; `lostpointercapture` fires at the next pointer event's pending processing (the DOM releasePointerCapture model).
- **`Window.CapturedHitbox() (HitboxID, bool)`** (`gpui/pointercapture.go:233`) — reports the *active* capture (a request this event becomes visible, with its gotpointercapture, at the next event).
- **`DivElement.OnGotPointerCapture(f)` / `DivElement.OnLostPointerCapture(f)`** (`gpui/interact.go:1022-1046`) with **`GotPointerCaptureEvent` / `LostPointerCaptureEvent`** (`gpui/pointercapture.go:77-100`): the DOM capture events, delivered target-first then ancestors (they bubble), with the causing event's window-local Position and Modifiers.
- **`PointerCancelEvent`** (`gpui/pointercapture.go:107`) — the DOM pointercancel's port: dispatching it releases the capture, drops every pending press and cancels an active drag (the WM_CAPTURECHANGED translation; no builder listener — the event is the platform's, like `MouseExitEvent`).
- **`MouseMoveEvent.LocalPosition(hitbox) / MouseDownEvent / MouseUpEvent`** (`gpui/pointercapture.go:437-452`, helper at `:421`) — the element-local position (spec D12, research browser-parity 03 §5): the pointer position relative to the hitbox's bounds origin, so sliders and dividers stop subtracting bounds origins. While captured, the value runs past the bounds exactly like the DOM's offsetX under capture.

### What changed

- **The capture state** (`gpui/mouseevent.go:550-566`): the pin's `capturedHitbox` became `activePointerCapture` + `pendingPointerCapture` (the spec's override/pending-override pair, compared for change) plus `pointerButtonsPressed`, the dispatch-side twin of the host's `mouseButtonsHeld`, tracked in `dispatchInput`'s translation arm (`gpui/filedrop.go:413-445`, which also fills the cancel's best-effort position and clears the held buttons).
- **The dispatch flow** (`gpui/mouseevent.go:731-846` `dispatchMouseEvent`): a no-buttons move releases the capture first (Chromium :1144-1149); `processPendingPointerCapture` (`gpui/pointercapture.go:274`) runs before every event — firing lost to the old target, then got to the new one with the boundary sweep settled first (Chromium's SetElementUnderPointer at :1398 precedes the got); the hit test refresh is capture-aware (`currentHitTest`, `gpui/pointercapture.go:394`); the up tail (replacing the pin's unconditional auto-release) releases the capture, fires lostpointercapture after the up's listeners, re-hit-tests as a release hit test and runs the boundary sweeps (leave the captured element, enter the hit-test target — Chromium :1244-1313); the cancel tail fires lost with no boundary events (:1316-1321); the drag bookkeeping gains a cancel arm.
- **The capture chain** (`gpui/pointercapture.go:368` `captureChainOf`): each `hitboxRecord` now carries its enclosing hitbox (`parent`) and its owning element's id path (`ownerPath`) — recorded at `insertHitboxRecord` (`gpui/mouseevent.go:630`) from the element-id stack, with the frame's transient `hitboxStack` (`gpui/focus.go:126-131`) pushed by `withHitboxSubtree` (`gpui/mouseevent.go:659`) around the div's (`gpui/div.go:1639`) and the list's (`gpui/listelement.go:1031`) children prepaint. `ensurePaintHitTest` (`gpui/interact.go:519`) retargets at paint time, so the captured element keeps its hover style wherever the pointer is.
- **The got/lost delivery** (`gpui/pointercapture.go:319` `dispatchPointerCaptureEvent` + `paintCaptureListeners` at `:463`, registered from `gpui/interact.go:1595`): a reverse-registration sweep over the frame's mouse listeners (child-first = the DOM's bubbling order), each element firing when its hitbox is on the marker's capture chain. Both listener counts joined `shouldInsertHitbox` (`gpui/interact.go:1098-1099`).
- **WM_CAPTURECHANGED** (`gpui/desktop_input_windows.go:326` `handleCaptureChangedMsg`, routed from `gpui/win32host_windows.go:892-897`, constant `gpui/desktop_windows.go:117`): a foreign window taking the mouse capture mid-press (lParam names it; our own ReleaseCapture's echo is ignored) clears the host's held-button set — so ticket 02's release rule runs on the next up instead of sticking — and dispatches a `PointerCancelEvent` through the mouse input callback. The cancel's arms in `paintMouseListeners` (`gpui/interact.go:1453-1460, 1605-1611`) drop the pending press and the active style in the capture phase, and `PointerCancelEvent` joined the simulated-input vocabulary (`gpui/testsim.go`).
- **Intended behaviour changes** (recorded per D0): a capture request without a button held is now refused (the pin accepted it); the capture becomes active at the next pointer event rather than immediately (gotpointercapture timing); while captured, other elements no longer report hovered (the pin left the hit test untouched, so siblings under the pointer kept events and hover); `ReleasePointer`'s loss fires at the next event rather than silently; a foreign capture takeover now cancels the press, the active style and the drag (the pin left them stuck until some later mouse up). Two pin-era assertions were updated with comments: `TestInteractPointerCapture` (`gpui/interact_test.go:854-898`, the pending timing and the active-after-next-event check).

### Harness extension

The Tier B vocabulary gained `pointerdown`, `pointermove`, `pointerup`, `gotpointercapture` and `lostpointercapture` (`internal/parity/fixture.go:288-300` vocabulary, helpers at `:234-269`, the EventKinds doc and the vocabulary error message updated). The recorder needed no changes — its instrumentation binds any listed kind via addEventListener — keeping the change small and additive for the merge with ticket 09's key-event extension. `docs/browser-parity-harness.md` documents the new kinds, Chromium's capture order under Tier B, and the click-vs-lost divergence note.

### Fixtures and tests

- `internal/parity/fixtures/capture` (registered in `internal/parity/replay_test.go` and `cmd/parityrecord/main.go`): body flex-centering p (260x120, 20px padding) holding cap and other (100x80 each); cap's pointerdown handler calls setPointerCapture (the view's OnMouseDownAll calls `Window.CapturePointer`) and the pointer moves root → cap → down → other (outside cap) → back → up over other. Recorded with Chrome 155.0.8059.85 into `internal/parity/testdata/capture-drag-outside/` (record.json with 27 events, chrome.png, page.html): enter/move chain entries, `gotpointercapture:cap,p,root` firing before the first captured move, the moves targeting **cap/p/root alone** while other (under the pointer) gets nothing and no boundary events fire, the ups retargeted, `lostpointercapture` after the ups, then `pointerleave:cap` + `pointerenter:other` — Chrome's exact order, all three tiers green (Tier C 0.0000, four solid rectangles; tolerance at the tracer's house values). The click kind is deliberately not instrumented (the pin's click-before-mouseup order, owned by the input bundle — the same reason the tracer skips mouseup).
- Discrimination test `TestWithoutCaptureFailsOnlyTheEventTranscript` (`internal/parity/fixtures/capture/capture_test.go`): the view without the capture call must fail Tier B with a readable diff (event 11, the first gotpointercapture) while boxes and pixels still pass.
- Go tests (`gpui/pointercapture_test.go`): `TestPointerCaptureRetargetsEventsToTheCapturedChain` (the full transcript incl. the captured click), `TestPointerCaptureRequiresHeldButton` (the IsActiveButtonsState gate), `TestPointerCaptureLocalPosition` (exact local positions, including the captured drag's past-bounds values), `TestPointerCancelClearsPressesAndDrag` (no click after the cancel, a fresh press works), `TestPointerCancelCancelsActiveDrag`, `TestPointerCaptureDropsWhenElementLeavesTree`, `TestPointerCaptureHoverStylesFollowTheChain`.
- Real-window gate (`gpui/pointercapture_windows_test.go`): `TestWin32CaptureChangedCancelsCleanlyOnOffscreenWindow` — a real, shown window at the off-screen origin (20000, 20000); synthetic WM_MOUSEMOVE/WM_LBUTTONDOWN/WM_MOUSEMOVE through the real message handlers (SetCapture, the press, gotpointercapture, the retargeted dispatch keeping the active style), then a synthetic WM_CAPTURECHANGED carrying a foreign hwnd: lostpointercapture fires, the pressed style clears, the later WM_LBUTTONUP clicks nothing and the OS capture releases (GetCapture, the held-button tracking cleared — no button stuck), and a fresh press works end to end. Headless for the user (off screen), no real input, no cursor move.
- The example replacement: `examples/zterm/panes.go:242-245` — the pane's grip-zone computation uses `e.LocalPosition(hb).Y` instead of `e.Position.Y - hb.Bounds.Origin.Y` (the ticket's local-position test; the zterm suite passes).

### Chromium citations (verified against %TEMP%\research-src\chromium @ 0d4dcd4f)

- setPointerCapture is pending-only and requires active buttons: `third_party/blink/renderer/core/input/pointer_event_manager.cc:1440-1447` (`if (pointer_event_factory_->IsActiveButtonsState(pointer_id)) { pending_pointer_capture_target_.Set(...) }`).
- The pending capture processes before every pointer event, before the boundary events and the event dispatch: `pointer_event_manager.cc:1353-1365` (`ProcessCaptureAndPositionOfPointerEvent`: `ProcessPendingPointerCapture(pointer_event)` at `:1357`, then `GetEffectiveTargetForPointerEvent` and `SetElementUnderPointer` at `:1359-1362`).
- `ProcessPendingPointerCapture` (`:1369-1414`): lostpointercapture to the old target first (re-targeted to the document when removed, `:1387-1393`), then `SetElementUnderPointer(pending)` before the got dispatch (`:1398`), then gotpointercapture (`:1397-1402`), and a lost to the document if the element left during the got (`:1405-1412`).
- The event target is the capture target — for dispatch and boundary tracking alike: `GetEffectiveTargetForPointerEvent` (`:232-239`) returns the capture target when set; this is why no pointerleave fires for the captured element while captured (ticket 10's "the captured hitbox reports hovered" note is now the exact Chrome behaviour).
- The implicit release on pointerup/pointercancel: `:1244-1246` (ReleasePointerCapture right after the up's dispatch), the release hit test `:1268-1281` (`HitTestRequest::kRelease`), lostpointercapture before the click at `:1277`, and the boundary events of the release at `:1300-1313` (the final `ProcessCaptureAndPositionOfPointerEvent`). A pointercancel sends no boundary events: `:1316-1321` ("Don't send boundary events in this case as it is a little tricky").
- The no-buttons move release: `:1144-1149` ("This is for when the mouse is released outside of the page").
- gotpointercapture/lostpointercapture bubble and clone the causing event's coordinates: `core/events/pointer_event_factory.cc:287-296` (`SetEventSpecificFields`: `setBubbles(!is_pointer_enter_or_leave)` — only pointerenter/pointerleave are non-bubbling) and `CreatePointerCaptureEvent` (`CreatePointerEventFrom` copies the pointer event's coordinates).
- The W3C Pointer Events spec agrees (fetched this session): the process pending pointer capture steps run when firing any pointer event that is not gotpointercapture/lostpointercapture/click/auxclick/contextmenu; every event targets the capture target override while one is set; and "immediately after firing the pointerup or pointercancel events, the user agent MUST clear the pending pointer capture target override … and then run process pending pointer capture steps to fire lostpointercapture if necessary."

### Known divergences (documented in code and docs/browser-parity-harness.md)

- The port fires the click inside the up's listener traversal, so click precedes the up listeners where the browser delivers pointerup → lostpointercapture → click. The capture fixture avoids instrumenting click for that reason (the tracer's mouseup skip is the same precedent); the capture order this ticket owns (lost after the ups, boundary settling last) is transcript-verified.
- On a pointercancel the port's repaint hover diff settles hover styles immediately (the deferred boundary sweep), where Chromium leaves them stale until the next pointer event — a difference in when, not whether, hover settles.
- A captured element that leaves the tree drops the capture silently; Chromium re-targets the lostpointercapture to the document (`:1387-1393`), for which the port has no document-level listener.
- `LocalPosition` is hitbox-bounds (border-box) relative where the DOM's offsetX is padding-edge relative — the box the port's hit testing and drag math already use.
