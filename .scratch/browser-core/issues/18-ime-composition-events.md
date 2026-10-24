# 18 - IME composition events

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 8 (Claude Code)
Blocked by: 01
Triage: ready-for-agent

**What to build:** Input handlers get composition start, update and end callbacks and an IsComposing flag on key events, so apps can ignore keys during composition.

**Blocked by:** 01 (Browser-parity harness (tracer))

**Status:** ready-for-agent

- [x] A failing test first shows keydowns dropped during composition (spec risk), then passes.
- [x] A Tier B transcript of a scripted composition matches the recorded Chrome event order.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer

- **API:** `gpui.IMECompositionHandler` (an optional `InputHandler` extension) adds `CompositionStart/Update/End(data string, w, app)` delivered to the focused element's handler in Chromium's order around the IME text mutations: start + update before the first mark mutation, update before every later mutation, end after the commit's/cancel's mutation. `KeyDownEvent.IsComposing` / `KeyUpEvent.IsComposing` (DOM `KeyboardEvent.isComposing`) are stamped at dispatch time (`Window.dispatchKeyEvent`) from the window's composition state, so apps can ignore keys during composition — keydowns are delivered flagged, not dropped. `Window.SimulateIMEComposition(IMECompositionMessage, cx)` is the test-window seam of the WM_IME_COMPOSITION driver (GCS flags, composition/result strings, cursor, attrs).
- **Files:** `gpui/ime.go` (the `IMECompositionHandler` interface, event delivery + start-data helpers, `imeCompositionActive`/`markKeyEventComposition`, `SimulateIMEComposition` with the `windowIMESeam`/`simulatedIMECompositionSource`, and the driver's event derivation in `handleComposition`); `gpui/keystroke.go` (`IsComposing` on `KeyDownEvent`/`KeyUpEvent`); `gpui/key_dispatch.go` (one `markKeyEventComposition` call in `dispatchKeyEvent`); `gpui/ime_win32.go` (the shared `attrsUseIMECursorPosition` adjacency helper); `internal/parity/fixture.go` (`ImeSet`/`ImeCommit` steps, `EventRecord.Data`/`IsComposing`, `EventLog.AppendComposition`, the composition kinds' vocabulary); `internal/parity/recorder.go` (CDP `Input.imeSetComposition`/`Input.insertText` steps, data/isComposing instrumentation); `internal/parity/replay.go` (the simulated composition steps); `internal/parity/fixtures/imecompose` + `internal/parity/testdata/imecompose` (the fixture and its Chrome record).
- **Tests:** `gpui/ime_composition_test.go` (the event order interleaved with the mutations, start data = the selection text, commit without a session, the zero-lParam cancel, the plain-handler skip, keydowns during composition flagged not dropped with a commit ending the session, a plain `InputHandler` never composing, the no-handler/plain-handler fallthroughs); `internal/parity/fixtures/imecompose/imecompose_test.go` (Tier B matches the recorded Chrome order, plus the one deliberately-wrong view — `PlainView`, a handler without the callbacks — which must fail Tier B naming the lost composition entries). Test-first: the new tests failed (undefined `IsComposing`/`IMECompositionHandler`/`SimulateIMEComposition`) before the implementation landed.
- **Chrome evidence:** recorded through the recorder with CDP `Input.imeSetComposition`/`Input.insertText` (Chrome 155.0.8059.85): compositionstart then compositionupdate(text) before each mutation; the commit = compositionupdate(committed) then `compositionend`(committed); the empty set = compositionupdate("") then `compositionend("")`; insertText without a session fires no composition events; keydown/keyup during a session carry `isComposing=true` and after the commit `false`; compositionstart's data is the selection text (probed with a 1..3 selection -> "bc"). Citations: `third_party/blink/renderer/core/editing/ime/input_method_controller.cc:935-948` (start, data = `SelectedText()`), `:391` (update before the DOM mutation), `:718`/`:728` (commit: update before, end after - "No DOM update after 'compositionend'"), `:1020-1022` (start when `!HasComposition() && !text.isEmpty()`), `:1003-1015` (the empty-text cancel), `:920-932` (`CancelComposition`); `core/events/keyboard_event.cc:72-78` (`HasCurrentComposition` -> `InputMethodController::HasComposition`), `:122` (`is_composing_` at construction), `:100` (`isComposing`); `platform/windows_keyboard_codes.h:316` (`VK_PROCESSKEY 0xE5` = keyCode 229) and `ui/events/keycodes/dom/dom_key_data.inc:204` ("Process") for the real-IME keydown identity; `content/browser/devtools/protocol/input_handler.cc:1316`/`:1290` (the CDP composition methods).
- **Limitations:** real IME sessions cannot run here (US-English input profile; headless-only rules) — the Windows-side flow is exercised through the recorded-data seams (the `imeCompositionDriver` and the simulated message) plus `internal/imespec`'s real-window routing, as before. A real IME's keydowns arrive as `WM_KEYDOWN` with `VK_PROCESSKEY` (0xE5 -> DOM key "Process", keyCode 229); both the pin and the port's `win32TranslateKeyDown` leave that virtual key untranslated (the IME owns the message), so a real composition's key events are not dispatched — the `IsComposing` surface is observable through the simulated/CDP paths and any non-IME key arriving mid-session. The div-level `OnComposition*` listener surface, the TSF text store and beforeinput/input events are later tickets (spec story 61). The fixture does not pixel-compare the composed text (the port has no editor element; the page's input hides its value) — the strings are pinned by the transcript. `Window.DispatchKeystroke`'s key-char delivery during an active composition is unchanged (a test-only path; a real IME session routes text through composition instead).
