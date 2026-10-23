# 10 - Hover: pointer enter/leave, window leave, survives typing

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: resolved
Assignee: orchestrator 6 (Claude Code); worker chat 9fc1818d
Blocked by: 01
Triage: ready-for-agent

**What to build:** Elements get OnPointerEnter and OnPointerLeave from a hover diff, leaving the window clears hover on every element, and key presses no longer clear hover (D12).

**Blocked by:** 01 (Browser-parity harness (tracer))

**Status:** ready-for-agent

- [x] Event-log fixtures for enter/leave across nested elements match Chrome's order.
- [x] Moving the pointer out of the window clears hover styles (simulated WM_MOUSELEAVE).
- [x] Typing while hovering keeps hover styles.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

- 2026-10-24, orchestrator 6: verified after merging onto main. I resolved the conflicts in the fixture registries and the harness guide (kept the 08 text paragraph and added the 10 paragraph). `go test ./gpui/... ./base ./internal/parity/...` is ok, and the hover tests passed 3 runs in a row. The real-window test is off-screen. The citation (boundary_event_dispatcher.cc: leave child-to-parent, enter parent-to-child) matches. D12 behaviour change: typing no longer clears hover. The two pin-era tests were updated with notes.

## Answer

### API

- `DivElement.OnPointerEnter(f func(*PointerEnterEvent, *Window, *App))` and `DivElement.OnPointerLeave(f func(*PointerLeaveEvent, *Window, *App))` (`gpui/interact.go:994-1023`): the DOM pointerenter/pointerleave boundary events, delivered in Chromium's order. Both require an element ID (like `OnHover`): the retained hover state that diffs transitions across frames keys on it.
- `PointerEnterEvent`/`PointerLeaveEvent` (`gpui/hoverboundary.go:73-93`): `Position` (window-local, the transition's pointer position) and `Modifiers`.
- `MouseExitEvent` (`gpui/hoverboundary.go:58-71`): the pin's own "mouse exit event from the platform, generated when the mouse leaves the window" (`reference/ce-source/crates/gpui/src/interactive.rs:666-684`), now actually produced by the Windows host on WM_MOUSELEAVE/WM_NCMOUSELEAVE. Its position carries the window's last tracked position (best effort; the Windows leave reports no coordinates). It also simulates as a platform input (`gpui/testsim.go:104`).

### What changed

- **Hover boundary sweeps** (`gpui/hoverboundary.go`, new file): every mouse move (and window exit) runs two sweeps over the rendered frame's mouse listeners before the move's own capture/bubble traversal — a leave sweep in reverse registration order (listeners register parent-before-child, so reverse is innermost-first) and an enter sweep in registration order (outermost-first). Each element's boundary listener (`paintBoundaryListeners`, `gpui/hoverboundary.go:171-220`, registered from `paintMouseListeners` at `gpui/interact.go:1556`) checks its own hover transition and fires its enter/leave listeners, so the transcript order is exactly Chromium's: leaves child-to-parent, then enters parent-to-child, all before the move. The sweeps are called from `dispatchMouseEvent` (`gpui/mouseevent.go:678-683`). A stationary mouse whose layout moves under it gets the same ordered transitions through one deferred sweep per frame (`deferHoverBoundarySweep`, `gpui/hoverboundary.go:152-169`) — the OnHover defer pattern (`div.rs:3629-3657`).
- **Window leave clears hover**: the host's WM_MOUSELEAVE/WM_NCMOUSELEAVE handler dispatches a `MouseExitEvent` through the mouse input callback (`gpui/desktop_windows.go:1810`), and the dispatch's translation arm moves the tracked mouse position off window (`gpui/filedrop.go:419-438`, the sentinel at `gpui/hoverboundary.go:112`), so the hit test comes up empty — hover clears on every element, like Chromium's null-target leave. The hover-style, group-hover and OnHover trackers now also react to the exit event (`gpui/interact.go:1305-1335, 1336-1358, 1547-1553`), the last being the pin's own `div.rs:3674-3680` arm that the port previously could not wire ("the port's platform input has no mouse-exited event yet").
- **Key presses no longer clear hover** (D12, a deliberate divergence from the pin under D0): `HitboxID.IsHovered` drops its `lastInputWasKeyboard` early-false (`gpui/mouseevent.go:342-357`), and the retained hover state's `elementIsActive`/`groupIsActive` drop theirs (`gpui/interact.go:293-305`). The modality is still tracked and still drives `FocusVisible` (the pin's `:focus-visible` seam, unchanged).
- New div fields `pointerEnterListeners`/`pointerLeaveListeners` (`gpui/div.go:773-779`), the retained `hoverBoundaryState` (`gpui/interact.go:324`), and both counts joined `requiresElementState` and `shouldInsertHitbox` (`gpui/interact.go:1060, 1076-1077`), so a div with boundary listeners inserts a hitbox and requires an ID.

**Intended behaviour changes** (recorded per D0): hover styles, OnHover, group-hover and the new boundary events no longer clear when a key is pressed; hover no longer sticks at the window edge after the pointer leaves (WM_MOUSELEAVE now reaches element dispatch). Two pin-era assertions were updated with comments naming the change: `TestInteractHoverActiveFocusStyles` (`gpui/interact_test.go:633-656`, hover stays blue after keyboard input) and `TestTestAppWindowSimulation` (`gpui/visualtest_test.go:536-544`, no `hover(false)` after keystrokes).

### Harness extension

- The Tier B event vocabulary gained `pointerenter` and `pointerleave` (`internal/parity/fixture.go:244-247`, helpers at `:216-223`); the vocabulary error message and the EventKinds doc were updated. The recorder needed no changes: its instrumentation binds any kind the fixture lists via addEventListener, and a `Move` step already existed. Kept small and additive for merge with ticket 09's key-event extension.
- `docs/browser-parity-harness.md` documents the new kinds, the boundary order guarantee and the out-of-viewport move as the DOM-visible half of the window leave, and moves the fixed pin behaviours to a "fixed by ticket 10" note.

### Fixtures and tests

- `internal/parity/fixtures/hover` (registered in `internal/parity/replay_test.go` and `cmd/parityrecord/main.go`): body flex-centering a 260x120 parent holding two 100x80 siblings; the pointer moves outside→child, child→sibling, back out, and finally beyond the viewport's bottom-right corner (the DOM-visible window leave). Recorded with Chrome 155.0.8059.85 into `internal/parity/testdata/hover-enter-leave/` (record.json with 8 events, chrome.png, page.html): `enter root` · `enter p, enter c1` · `leave c1, enter c2` · `leave c2, leave p` · `leave root` — Chrome's exact order, all three tiers green. Tier C measures 0.0000 (four solid rectangles), tolerance kept at the tracer's house values.
- Discrimination test `TestMissingEnterListenersFailsOnlyTheEventTranscript` (`internal/parity/fixtures/hover/hover_test.go`): a view without enter listeners must fail Tier B with a readable diff while boxes and pixels still pass.
- Go tests (`gpui/hoverboundary_test.go`): `TestHoverBoundaryOrderAcrossNestedElements` (the sweep order at unit level), `TestHoverBoundaryLeaveOnWindowExit` (exit fires leaves innermost-first, clears hover styles, tracked position goes off-window, re-enter works), `TestHoverSurvivesKeystrokes` (hover style survives key down/repeat/up), `TestHoverBoundaryLayoutChangeUnderStationaryMouse` (the deferred sweep).
- Real-window gate (`gpui/hoverboundary_windows_test.go`): `TestWin32MouseLeaveClearsHoverOnOffscreenWindow` — a real, shown window at the off-screen origin (20000, 20000); a synthetic WM_MOUSEMOVE applies the hover style and fires `enter:button` + `hover(true)`, a synthetic WM_MOUSELEAVE (the message the armed TrackMouseEvent delivers) clears the hover style and fires `leave:button` + `hover(false)`. Headless for the user (off screen), no real input, no cursor move.

### Chromium citations (verified against %TEMP%\research-src\chromium @ 0d4dcd4f)

- Leave order (child-to-parent): `third_party/blink/renderer/core/input/boundary_event_dispatcher.cc:190-193` ("Dispatch leave events, in child-to-parent order"; `exited_ancestors[0]` is the exited target, the loop stops below the common ancestor).
- Enter order (parent-to-child): `boundary_event_dispatcher.cc:211-214` ("Dispatch enter events, in parent-to-child order", from the element just below the common ancestor down to the entered target).
- The out/over targets and the common-ancestor walk: `boundary_event_dispatcher.cc:143-147` (out to the exited target), `:196-199` (over to the entered target), `:59-86` (`BuildAncestorChainsAndFindCommonAncestors`, index 0 = the target).
- Boundary events precede the move: `core/input/pointer_event_manager.cc:1353-1365` (`ProcessCaptureAndPositionOfPointerEvent`: `SetElementUnderPointer` — the pointer boundary events — before `SetElementUnderMouse`, both inside `SendMousePointerEvent` at `:1107-1163` which then dispatches pointermove/mousemove).
- Leaving the window: `core/input/event_handler.cc:1158-1165` (kMouseLeave: "We don't want to do a hit-test in MouseLeave scenarios … So we avoid the hit test but still clear the hover/active state", `UpdateHoverActiveState(…, nullptr)`); the leave's boundary events flow through `DispatchMousePointerEvent(kPointerMove, mev.InnerPossiblyPseudoElement(), …)` at `:1250` with the null hit target.
- Hover is never cleared by key input: no link exists between keyboard dispatch and hover in `core/input` (the report's §1 finding; the only keyboard hook into hover styling is `:focus-visible`, `core/css/selector_checker.cc:3861-3900`).
