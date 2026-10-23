# 47 - Preferred frame interval per window

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: resolved
Assignee: orchestrator 6 (Claude Code); worker chat 8cdf9fd8
Blocked by: none
Triage: ready-for-agent

**What to build:** A window can ask for a lower frame rate (for example half rate) for its animations.

**Blocked by:** None - can start immediately

**Status:** ready-for-agent

- [x] Frame-loop test: a half-rate window draws every other vblank during an animation.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

- 2026-10-24, orchestrator 6: verified on core/31-47-small before merge. Re-ran vet, gofmt, `go build ./...` and `go test ./gpui/... ./base -count=1` (ok). All 16 new tests pass with none skipped. The half-rate test and the hidden-window test passed 10 times in a row (`-count=10`, 239 Hz display). The Chromium citations (type_ahead.cc timeout and cycling, external_begin_frame_source_win.cc OnVSync and SetPreferredInterval) match the source.

- 2026-11-30, worker (core/31-47-small): test-first — the frame-loop test was written and failed red (ratio 0.99, "the window drew on consecutive vblanks") before the pacer change; the API round-trip and unit tests came with it. The real-window tests park both windows far offscreen (origin 20000,20000); `GPUI_REAL_INPUT_TESTS` was never set.

## Answer

Implemented on `core/31-47-small`. All Go; no native rebuild. The port of viz's half-rate preferred interval — research browser-parity 02 §8: "A preferred interval can halve the rate by skipping every other vsync" (`components/viz/service/frame_sinks/external_begin_frame_source_win.cc:53-62, :94-106`), and "the analogue of `AnimationElement`'s `MaxFPS` timer, but vsync-aligned".

**The public API** (both a `WindowOptions` field and `Window` methods, as suggested):

- `WindowOptions.PreferredFrameInterval time.Duration` (`gpui/window.go:126-141`): zero (the default) is the display rate — every vblank. A value greater than twice the observed vblank interval minus half a millisecond (about 33 ms on a 60 Hz display) selects HALF RATE: the frame source skips every other vblank, vsync-aligned. Values in between keep the full rate — like Chromium, only full and half rate exist. A negative value is rejected at open (`gpui/window.go:738-740`), and the option lands in the window's frame state at open (`gpui/window.go:955`).
- `Window.PreferredFrameInterval()` and `Window.SetPreferredFrameInterval(interval)` (`gpui/frameloop.go:324-329, :331-361`) change it live; the setter panics on a negative interval. Test windows ignore the interval (their frames arrive through `SimulateFrameRequest`), documented on the setter.
- The state lives on `windowFrameState` (`gpui/frameloop.go:131` the atomic preferred interval; `:139` the last half-rate-granted slot), set through `newWindowFrameState` (`gpui/frameloop.go:146-152`).

**The pacer half-rate rule** (`gpui/frameloop_windows.go`):

- The engage decision is Chromium's `SetPreferredInterval` exactly (`external_begin_frame_source_win.cc:96-110`): `preferred > 2*vsyncInterval - 500µs` (`gpui/frameloop_windows.go:282-296`, the tolerance constant at `:56-68`). The `vsyncInterval` is the pacer's own slot cadence — the MEDIAN gap between recent frame slots (`observeSlot`, `:220-253`, ring at `:94-103`) — because the pacer never queries `DwmGetCompositionTimingInfo` (the file's documented default-interval deviation); until slots are measured it falls back to the reference `DEFAULT_VSYNC_INTERVAL` 16.666 ms (`:255-262`).
- The skip itself is the per-window form of the source's `skip_next_vsync_` alternation (`external_begin_frame_source_win.cc:53-62`): a half-rate window is granted every OTHER pacer slot, counted structurally by a monotonic slot counter (`gpui/frameloop_windows.go:88-93, :170-171, :265-301`) rather than by elapsed time — vblank and DwmFlush jitter cannot break the cadence, and the rule self-corrects exactly where Chromium resets the flag: a demand starting after idle is served at once (the missed begin frame, `:70-92`), a window whose WM_PAINT is still in flight at a slot gets the next one instead of falling behind, and a live mode change is served at the next slot (`:107`). A skipped slot keeps the window's demand pending in the pacer and re-requests it (`:192-199`) — the same pattern ticket 03 established for hidden windows, so no frame is ever lost and the animation completes.
- Ticket 03's behaviour is untouched: the visibility gate runs before the interval gate, `TestFrameLoopMinimizedWindowSkipsFramesUntilVisible` still passes.

**Tests** (all green):

- `gpui/frameloop_windows_test.go:279-446` `TestFrameLoopPreferredFrameIntervalDrawsEveryOtherVblank` — the ticket's frame-loop test: a full-rate window and a `PreferredFrameInterval: 40ms` window, both real but parked far offscreen, run 40-step animations; the full-rate window doubles as the pacer's vblank clock (its draw gaps measure the interval), so the assertions hold at any display refresh rate (this machine: 239 Hz, ~4.2 ms vblanks). Asserts: the half-rate median draw gap is about twice the full-rate one (ratio 2.0 observed), at most a couple of half-rate gaps fall under 1.25× the vblank (no systematic drawing on consecutive vblanks; the structural rule is pinned by the unit test), the half-rate window drew about half as many frames while the full-rate one finished its animation, every animation frame of both windows was drawn (the skipped slots keep demand pending, none lost), and both windows go idle. Was red before the pacer change: ratio 0.99, "the window drew on consecutive vblanks".
- `gpui/frameloop_windows_test.go:471-541` `TestFramePacerGrantFrameSlotAlternates` — the unit half of the rule: slot alternation (grant at 1, skip at 2, grant at 3…), a window that missed one slot is granted the next one, a session restarting long after idle is served at once, a below-threshold interval keeps the full rate, and the unmeasured pacer's default interval still engages 40 ms.
- `gpui/frameloop_test.go:61-82` `TestWindowPreferredFrameIntervalSetAndGet` — the live API: default zero, set/get round trip, restore to zero, negative panics.
- `gpui/frameloop_windows_test.go:452-461` `TestWindowOpenRejectsNegativePreferredFrameInterval` — the open-time guard, failing before any window exists.
- The frame-loop gates still pass: `TestFrameLoopMinimizedWindowSkipsFramesUntilVisible` (ticket 03's hidden-window gate), `TestFrameLoopRealWindowNotifyRepaints`, `TestFrameLoopRealWindowWithoutPresentHookDraws`, `TestFrameLatencyWaitableRealWindow`, and every cross-platform frameloop test.

Gates: `go vet ./...`, `gofmt -l gpui base` (clean), `go build ./...`, `go test ./gpui/... -count=1` (14.3 s, ok), `go test ./base -count=1` (2.0 s, ok), plus `-count=3` on the new real-window test and the hidden-window test (stable).

**Limitations** (documented in the code):

- Only full and half rate exist, as in Chromium — a request between the display interval and the half-rate threshold runs at full rate, and the effective rate is always a whole number of vblanks.
- The engage threshold uses the pacer's measured slot cadence, not the compositor's reported interval; with a waitable surface presenting, slot gaps can run slower than the raw vblank, and until about five slots are observed the reference default (16.666 ms) decides — early half-rate frames on displays faster than 60 Hz can arrive a few vblanks late until the estimate settles. The skipping itself is structural and unaffected.
- The skip is counted in pacer slots; with a waitable surface in flight the slot cadence itself can exceed one vblank, in which case "every other slot" is every other waitable-cadence slot — still half of what the same window would draw at full rate under the same load.
