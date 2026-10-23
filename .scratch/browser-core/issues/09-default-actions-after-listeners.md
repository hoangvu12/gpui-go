# 09 - Default actions run after key listeners

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: resolved
Assignee: orchestrator 6 (Claude Code); worker chat 6a03afb2
Blocked by: 01
Triage: ready-for-agent

**What to build:** Core default actions (Tab, Escape, keyboard scrolling, activation) run after element key listeners and are skipped when a listener calls PreventDefault. Key bindings still run before listeners (GPUI shape). A per-window flag gives the full browser order (D5).

**Blocked by:** 01 (Browser-parity harness (tracer))

**Status:** ready-for-agent

- [x] Harness event-log fixtures show listener-then-default order and PreventDefault suppression.
- [x] Key bindings still win over listeners by default; the per-window flag reverses that and matches Chrome's order.
- [x] Existing keybinding tests pass.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

- 2026-10-24, orchestrator 6: verified after merging onto main. I merged the harness conflicts with ticket 10 by hand: both event families are in the vocabulary, the recorder keeps per-fixture flags plus `--disable-smooth-scrolling`, and the guide keeps both sections and drops 09's stale raster note. `go test ./gpui/... ./base ./internal/parity/...` is ok, and the 7 keyorder fixtures pass. The out-of-scope fix is correct: Go called `dispatchKeystrokeObservers` both inside `finishDispatchKeyEvent` and again after it, while the reference window.rs calls it once, inside `finish_dispatch_key_event`. Watch item: the worker saw a one-off "text shaping handle limit exceeded" flake, which also happens on the parent commit. It is the known native shaping-handle cap; as fixtures grow, the parity run may approach it.

- 2026-10-24, worker (core/09-default-actions): implemented and self-verified. `go vet ./...`, `gofmt -l` (clean on touched files), `go build ./...`, `go test ./internal/parity/... ./gpui/... ./base -count=1` all green (the pre-existing `internal/portfixture` TestFx0004 font-count failure and the `internal/fontspec` gofmt flag are untouched). All seven fixtures recorded with Chrome/155.0.8059.85 at device scale 2; discrimination tests verify each suppressed/order claim fails when the behaviour is absent. Status left as claimed for the orchestrator's verification.

## Answer

Implemented on `core/09-default-actions` (commit: see the branch history). All Go; no native rebuild. Spec D5: core default actions (Tab focus movement, keyboard scrolling, activation) run after the element key listeners and honour `Window.PreventDefault`; key bindings keep the GPUI shape (before listeners); `KeyDispatchOrder.ListenersBeforeBindings` — a per-window flag — gives the full browser order. Escape's default action is the close-watcher stack (ticket 22) and the drag cancel (ticket 16); the mechanism is in place, those actions land with their tickets.

**The default-action phase (the mechanism).**

- `gpui/key_dispatch.go:830-836` — the key-up dispatch path (and the no-keystroke path) ends in `runKeyDefaultActions`, and `gpui/key_dispatch.go:923-931` — the key-down path runs it after `finishDispatchKeyEvent` (all key listeners, capture then bubble). The phase is skipped when `Window.DefaultPrevented` is set — the DOM's rule that default handlers run after the dispatch and only when the event was not default-prevented (`third_party/blink/renderer/core/dom/events/event_dispatcher.cc:474-477`, invoking `Node::DefaultEventHandler` at `:519`); a listener's `StopPropagation` does *not* skip it (the dispatch's step 10 clears the stop flags before the default phase, `event_dispatcher.cc:397-401`).
- `gpui/key_default_action.go:138-175` — `runKeyDefaultActions`: the focused node's registered default actions (activation), then the keyboard-scroll chain, then Tab navigation — Chromium's default-handler order (`core/input/keyboard_event_manager.cc:426-490`). Key events reach the frame's default handler through `Node::DefaultEventHandler`'s keyboard arm (`core/dom/node.cc:3410-3414` → `core/input/event_handler.cc:2474-2477`).
- A binding that stops propagation still returns before the phase (the app's binding overrides the core defaults, GPUI shape preserved); `gpui/key_default_action.go:458-507` — `dispatchKeyEventBrowserOrder` for the flag's order: listeners first, then bindings and the core defaults in the default-action position, all skipped by `preventDefault`, with the stop flags reset before the default phase (`event_dispatcher.cc:397-401`).

**Tab focus movement (the default action; the HTML tabindex order itself is ticket 14's).**

- `gpui/key_default_action.go:212-233` — `defaultTabNavigation`: Tab/Shift+Tab move focus through the existing `Window.FocusNext`/`FocusPrev`, unless Ctrl, Alt or the platform modifier is held — `DefaultTabEventHandler` (`core/input/keyboard_event_manager.cc:574-616`: ctrl/meta never advance, alt is a system key off Mac, Shift goes backward).

**Keyboard scrolling (minimal; latching, the hover/press start node and the full key set are ticket 15's).**

- `gpui/key_default_action.go:262-304` — `mapKeyForScroll`: `MapKeyCodeForScroll`'s rules (`keyboard_event_manager.cc:67-176`) — Shift (selection) and Meta never scroll, Ctrl keeps only Home/End, Alt keys are system keys; arrows scroll by line, Home/End by document, PageUp/PageDown and Space by page (Space with Shift pages backward, `DefaultSpaceEventHandler`, `keyboard_event_manager.cc:492-523` — the port runs Space at keydown; the pin has no keypress phase).
- `gpui/key_default_action.go:306-335` — the steps: 40 logical px per line (`cc/input/scroll_utils.h:20` kPixelsPerLineStep — measured 40 CSS px at the harness's 2.0 device scale in the recorded Chrome) and the page step `max(0.875×length, length−40)` (`cc/input/scroll_utils.cc:50-55` CalculatePageStep, 0.875 and 40 from `scroll_utils.h:22-24`).
- `gpui/key_default_action.go:178-210` — `defaultKeyboardScroll`: `BubblingScroll`'s chain (`core/input/scroll_manager.cc:194-232`) — walk the focused node's ancestor dispatch nodes, the first scroller that can take the delta consumes it — with the editor-first rule (`keyboard_event_manager.cc:442-445`): a focused input handler that accepts text input consumes the key before any scroll default.
- `gpui/scroll_element.go:41-45, 127-131` — the frame's clamp records the scroll maximum and visible size on the resolved `elementScroll`; `gpui/div.go:1613-1618` registers the scroll container on the div's dispatch node during prepaint (`gpui/key_default_action.go:337-455` — the `keyScrollContainer` record, its clamped `applyScroll`, and the registration); `gpui/key_dispatch.go:105-116, 322-339` — the node fields and tree registration methods.

**Activation as a default action (the timing change from the pin).**

- `gpui/interact.go:1177-1233` — `registerDivKeyboardClick` now registers one default-action callback instead of two key listeners: the keydown arm arms the pending activation (focus generation) and the keyup arm fires the keyboard click after *all* keyup listeners. Chromium runs activation from the focused element's default handler (`HTMLElement::HandleKeyboardActivation`, `core/html/html_element.cc:4313-4338`), so an ancestor's listener can now preventDefault the activation, and a prevented keydown leaves the pending unset so the keyup never clicks (`html_element.cc:4319-4322`: the Space keydown sets the active state, the Space keyup clicks when it was set). The pin's own-listener ordering is the recorded divergence; Enter's keypress timing stays the pin's keyup (ticket 12).

**The per-window flag (D5).**

- `gpui/key_default_action.go:75-133` — `KeyDispatchOrder` (`BindingsBeforeListeners` default, `ListenersBeforeBindings`), `Window.KeyDispatchOrder`/`SetKeyDispatchOrder`; `gpui/focus.go:74-80` (the storage), `gpui/window.go:143-153, 966-971` (`WindowOptions.KeyDispatchOrder`, applied at open). Under the browser order, a listener's `preventDefault` also suppresses bindings, and a binding whose action stopped propagation still overrides the core default (`gpui/key_default_action_test.go:533-567`). Keystroke observers still fire once per keydown, carrying the action when a binding dispatched (`gpui/key_default_action_test.go:594-628`).
- Drive-by fix: the tail of `dispatchKeyEventInner` (`gpui/key_dispatch.go:923-931`) no longer makes a second `dispatchKeystrokeObservers` call on the normal key-down path — the port notified observers twice (the reference's `window.rs` `dispatch_key_event` tail has no second call; `finish_dispatch_key_event` at `window.rs:6309-6326` is the only one).

**The harness extension (ticket 01's seam, extended for keys).**

- `internal/parity/fixture.go:175-195` — `EventRecord` gains `Key` (the DOM key name) and `Focus` (the focused element's id at listener time — `document.activeElement`, body normalized to `""`): the order probe, since a listener that still sees the old focus ran before the default action changed it. `:241-291` — the `KeyDown`/`KeyUp`/`KeyDownPreventDefault` helpers and `TrackFocus`; `:145-153` — `ViewWithWindow` (the window-aware view form for focus handles, window flags and key bindings); the vocabulary grows `keydown`/`keyup` (`:353-361`).
- `internal/parity/recorder.go:30-72` — the instrumentation binds the kinds per `[data-id]` element, records the key and focus fields for key entries (listener-time snapshots; `defaultPrevented` still reads the final state after the whole dispatch), and honours a per-element `data-events` attribute that narrows an element's bound kinds; `:225-233` — the recorder's Chrome launches with `--disable-smooth-scrolling` (with the setting on, keyboard scrolls land as nondeterministic compositor animations — `core/paint/paint_layer_scrollable_area.cc:771-777` reads the setting — which no recorded fixture can compare against the port's instant scrolls; ticket 45 re-records when the port scrolls smoothly); `:439-449` — single-character keys dispatch with their typed case.
- `internal/parity/replay.go:205-224` — the key step is one full press on both sides (the recorder's CDP `keyDown`+`keyUp` pair; the replay's `KeyDownEvent`+`KeyUpEvent` through the platform dispatch), each half's default-prevented state recorded; `:95-102` — `ViewWithWindow` support.
- `internal/parity/compare.go:174-196` — `pixelDiff` clamps regions into the image (a scrolled-out element records a negative-origin box; `MeasurePixels` used to panic).
- `docs/browser-parity-harness.md` — the guide documents the vocabulary, the key/focus fields, `ViewWithWindow` (with the window-keyed closure-state rule), `data-events`, the key step, the recorder's smooth-scrolling flag, and five new known divergences (mousedown's defaultPrevented on the pin's focus transfer, the non-bubbling keyboard click, Enter's keypress timing, Chrome's default focus ring pinned off with `outline: none`, plus the existing ones).

**The fixtures** (`internal/parity/fixtures/keyorder/keyorder.go`, records in `internal/parity/testdata/keyorder-*/`, recorded with Chrome/155.0.8059.85 at device scale 2):

- `keyorder-tab` (`:103`) / `keyorder-tab-prevented` (`:128`): click a, Tab. The keydown entries see focus a (listener-then-default order), the keyup entries see focus b, and the pixels show b (a) focused; the prevented variant keeps focus on a with `defaultPrevented` keydown entries.
- `keyorder-scroll` (`:156`) / `keyorder-scroll-prevented` (`:183`): click the target inside the scroller, then ArrowDown(s). The content's box ends at −80 (two 40 px line steps — Tier A) and the scrolled pixels match; the prevented variant stays at 0.
- `keyorder-activate` (`:210`) / `keyorder-activate-prevented` (`:237`): click the button, Space. The click entry lands after every keyup entry (Tier B); the prevented variant clicks nothing.
- `keyorder-binding-order` (`:267`): the window set to the browser order with "j" bound: the keydown entries are present (the listeners ran before the binding — under the GPUI shape the binding consumes them, which the discrimination test asserts).
- `internal/parity/fixtures/keyorder/keyorder_test.go:44-158` — the discrimination tests: each suppressed/order claim fails its tier when the behaviour is absent (the non-preventing views against the prevented records; the GPUI-shape view against the browser-order record), and the binding-order action counter pins that the action fires in both orders.
- Registered in `internal/parity/replay_test.go:10-13` and `cmd/parityrecord/main.go:19-22`; the tracer's record was re-recorded (identical events; new `recordedAt` — the instrumentation changed).

**Unit tests** — `gpui/key_default_action_test.go:170-628`: Tab after listeners (with the keyup-on-new-focus transcript), Shift+Tab, the modifier rules, preventDefault suppression, the stopPropagation-ignored DOM rule, the scroll steps (40/175/0/800, chaining when clamped, the modifier rules, the text-input guard), activation after keyup listeners and its keydown-prevented suppression, the binding order in both shapes (GPUI shape consumes the key; browser order runs listeners first; preventDefault suppresses the binding; the binding overrides the core default; observers notified once), and the flag surface. All existing keybinding tests pass unchanged (`go test ./gpui -count=1`).

**Limitations.** Escape has no default action yet (tickets 16/22); the keyboard scroll starts at the focused node only (Chromium also starts at the mouse-press node — ticket 15 generalizes it, and the list element's scroller is not yet a keyboard-scroll target); activation keeps the pin's keyup timing for Enter; the activation click still does not bubble and mousedown still reports `defaultPrevented` on the pin's focus transfer (both D12, recorded in the guide); the fixtures pin Chrome's default focus ring off (`outline: none`) until ticket 48 ships the port's ring.
