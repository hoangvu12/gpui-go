# 02 - Wheel page-scroll setting and capture release

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: resolved
Assignee: orchestrator 6 (Claude Code); worker chat ef20e162
Blocked by: none
Triage: ready-for-agent

**What to build:** Scrolling works when Windows is set to scroll one screen per notch, and the window's mouse capture is released only once no button is held, so a second held button no longer loses the drag.

**Blocked by:** None - can start immediately

**Status:** ready-for-agent

- [x] The WHEEL_PAGESCROLL value (0xFFFFFFFF lines) scrolls one page per notch instead of an enormous distance; covered by a unit test of the wheel translation.
- [x] With two buttons pressed, releasing one keeps capture; releasing the last releases it (simulated-message test).
- [x] No visible windows; GPUI_REAL_INPUT_TESTS is not used.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

- 2026-10-24, orchestrator 6: verified on core/02-03-small before merge. Re-ran `go vet ./gpui/... ./base`, gofmt, `go build ./...`, `go test ./gpui/... ./base -count=1` (ok) and the nine new tests with -v (all PASS, none skipped). The Chromium citations (web_input_event_builders_win.cc:312-318, scroll_utils.cc CalculatePageStep, display_scheduler.cc ShouldDraw) match the source.

## Answer

Implemented on `core/02-03-small` (commit: see the browser-core branch history). All Go; no native rebuild.

**WHEEL_PAGESCROLL (one page per notch).**

- `gpui/mouseevent.go:146-185` — `ScrollDelta` gains the `Pages` variant (`Pages Point`, `IsPages bool`, `ScrollDeltaPages`), a deliberate divergence toward the browser under D0: the Windows wheel's `WHEEL_PAGESCROLL` sentinel carries one page per notch the way Chromium's `kScrollByPage` events do (`ui/events/blink/web_input_event_builders_win.cc:312-38`). `PixelDelta` (`:203-213`) documents that pages are not converted there (zero).
- `gpui/mouseevent.go:216-239` — `PixelDeltaFor(lineHeight, viewport)` and exported `ScrollPageStep(length)`: pages resolve against the scroller's own visible length with Chromium's page step `max(0.875×length, length−40)`, floored at 1 (`cc/input/scroll_utils.cc:51-56` `CalculatePageStep`; `blink ScrollableArea::ResolveScrollDelta`, `scrollable_area.cc:213-229`) — 87.5px on a 100px scroller, so consecutive page notches overlap slightly instead of skipping content.
- `gpui/win32input_windows.go:43-52, 99-109` — the `wheelPageScroll` (0xFFFFFFFF) sentinel: the vertical arm emits `ScrollDeltaPages(0, notches)` instead of `notches × 0xFFFFFFFF` lines (the pin multiplies the sentinel, `events.rs:589-595`; Chromium keeps the delta at the notch count, `web_input_event_builders_win.cc:312-318`). Shift and horizontal arms still use the chars count, which has no page sentinel.
- Consumers resolve pages at the scroller, like Chromium: `gpui/scroll_element.go:165` (the div scroller, viewport = its hitbox bounds), `gpui/listelement.go:1162` (the list, viewport = its bounds), `base/scrollable_mask.go:224` and `base/input_state_mouse.go:216-224` (the same conversion so Base elements keep scrolling under the page setting). `gpui/scroll_element.go:235-265` — `coalesceScrollDelta` handles the third variant (same-variant accumulation, mixes yield the newer delta).
- Known limitation: the inspector's wheel zoom (`gpui/inspector.go:1122-1132`) reads `Lines` directly, so a page delta does not zoom the inspector tree; left as-is (debug tooling, out of the ticket's scroll surfaces).

**Capture released only when no button is held.**

- `gpui/desktop_input_windows.go:184-199, 212-215, 240-251` — `mouseHeldBit` plus `hostWindow.mouseButtonsHeld` (`gpui/win32host_windows.go:989-996`, host thread): a press sets its bit, a release clears it, and `handleMouseUpMsg` calls `ReleaseCapture` only when the set is empty — releasing one of two held buttons keeps the drag's capture; releasing the last releases it. An unpaired release (nothing tracked) still releases, preserving the pin's behavior for messages without a seen press (e.g. the move-loop's synthesized release, `gpui/titlebar_windows.go:341-347`).

**Tests** (all green: `go vet ./gpui ./base`; `go test ./gpui -run 'TestScroll|TestWin32|TestInteract' -count=1`; `go test ./base -count=1`):

- `gpui/win32input_windows_test.go:70-105` — `TestWin32WheelPageScrollTranslation`: the unit test of the wheel translation — one page per notch (±), shared position/modifiers/phase, Shift and horizontal still chars, plain line counts unaffected. (Was red: the sentinel was multiplied into lines.)
- `gpui/scroll_test.go:346-381` — `TestScrollDivWheelPageScroll`: the div scroller steps 87.5px per page notch, unclamped in dispatch, clamped at prepaint, horizontal pages remapped on a y-only scroller.
- `gpui/scroll_test.go:825-848` — `TestScrollListWheelPageScroll`: the list resolves pages against its own height and clamps eagerly.
- `gpui/scroll_test.go:245-258` — `ScrollDelta::coalesce` cases for pages.
- `gpui/interact_windows_test.go:145-215` — `TestInteractWin32MouseCaptureKeptWhileButtonHeld`: simulated-message test on an offscreen real window — two buttons pressed, releasing one keeps capture (`GetCapture` stays the window), releasing the last releases it, an unpaired release still releases; verified red against the pre-fix code (`step 2: GetCapture = 0x0, want the window`).
- `gpui/win32input_windows_test.go:480-503` — the existing real-window wheel gate now accepts the page-scroll setting and asserts the pages delta end to end.
- No visible windows: the one new real-window test opens offscreen (origin 20000,20000); `GPUI_REAL_INPUT_TESTS` was never set.
