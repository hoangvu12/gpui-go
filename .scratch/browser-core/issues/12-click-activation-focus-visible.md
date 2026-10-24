# 12 - Click on the common ancestor, activation and focus-visible

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 8 (Claude Code)
Blocked by: 01
Triage: ready-for-agent

**What to build:** A click whose press and release land on different children goes to their common ancestor; Enter activates on key-down; Space keeps :active while held; :focus-visible follows the focus origin, and modified keys like Ctrl+S don't count as keyboard focus (D12).

**Blocked by:** 01 (Browser-parity harness (tracer))

**Status:** ready-for-agent

- [x] Event-log fixtures for each rule match Chrome.
- [x] focus-visible fixtures: click focus shows no ring, Tab focus shows it, Ctrl+S after a click doesn't.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer

**Behaviour (all four rules, Chrome-shaped, spec D12):**

- **Click on the common ancestor.** The mouse up now dispatches a click as its own event — after every mouseup listener, after the implicit pointer-capture release (lostpointercapture), before the boundary settling — whose target chain is the deepest element on both the press and release chains (the captured element's chain while a pointer capture was active). A stopped mousedown (Base's disabled controls) or a block-mouse overlay keeps the un-armed ancestors out of the target set. Keyboard activation clicks and mouse clicks share the bubbling shape (target first, then ancestors).
- **Enter activates at the key-down's default action** (the DOM keypress rides the platform key event): the click lands between the keydown and keyup entries and bubbles to ancestors. A prevented keydown suppresses it (the keypress never happens).
- **Space keeps :active while held:** the keydown's default arms the activation and sets the focused element's pressed state (the `Active` style); the keyup's default clears it and fires the click after the keyup listeners, only when the armed focus generation still matches. The pin's intervening-key disarm rule survives.
- **:focus-visible follows the focus origin:** the focus-visible style applies when the focus origin is not the mouse (a Tab or script focus) or an unmodified key has arrived since the mouse focus. Ctrl/Alt/Meta keydowns don't set the flag (Ctrl+S after a click: no ring); Shift is allowed; a mouse move never clears the ring; a mouse-type focus clears the flag.
- **Mouse-down focus no longer reports defaultPrevented:** the innermost-wins signal between focusable elements is a window-internal flag, and the focus transfer records a mouse-origin focus.

**What changed (file:line):**

- `gpui/mouseevent.go` — the click dispatch: `chainPathsOf` (:531, the hit-test mouse path → element paths, mouse-blocking aware), `commonAncestorClickChain` (:570), `armMouseClick` (:610, the press's armed chain), `dispatchMouseClick` (:630, the up-tail click), the up tail (:1061-1097, capture release → click → boundary settling, Chromium's order), the pointercancel tail (drops the pending click); `windowInputState`'s new fields (:733-765: focusOrigin, hadKeyboardEvent, focusTransferClaimed, pendingMouseClick, pressClickChain, pendingKeyboardActivation).
- `gpui/interact.go` — `beginPlatformInputDispatch` (:467, the unmodified-key rule), `focusVisibleMatches` (:501), `claimFocusTransfer` (:516), `computeStyleInternal`'s focus-visible condition (:1174), the mouse-down focus arm (:1313, origin mouse, no PreventDefault), the click-arm restructure in `paintMouseListeners` (the frame registration + the armed chain, no per-element click firing), `registerDivKeyboardClick` (:1275, the dispatch-node registration).
- `gpui/key_default_action.go` — `defaultKeyboardActivation` (:218, the Enter/Space default) and `dispatchKeyboardClick` (:299, the bubbling click through the focused node's path).
- `gpui/key_dispatch.go` — `dispatchNode.clickActivation` + `SetClickActivation` (:348).
- `gpui/focus.go` — `focusWithOrigin` (:486), `FocusNext`/`FocusPrev` (:581, :605, keyboard origin), `renderedFrame.clickListeners` (:139) + `clickListenerRegistration`.
- `internal/parity/fixture.go` / `replay.go` / `recorder.go` — the `KeyDown`/`KeyUp` half-press steps and the Enter `text: "\r"` (without it Chrome generates no keypress and no activation).
- `internal/parity/fixtures/activation/` — the fixture set + discrimination tests; registered in `internal/parity/replay_test.go` and `cmd/parityrecord/main.go`.

**Fixtures (Chrome 155.0.8059.85, all three tiers pass; tolerances 0.05/0.25/0.003 with measured correct-view statistics 0.0/0.0):**

- `activation-click-ancestor` — press on a, release on b: mousedown a,p,root; mouseup b,p,root; click p,root (a and b's click listeners stay silent); p's background flips in the pixels.
- `activation-enter` — click, Enter: the activation click between keydown and keyup, bubbling to root; mousedown unprevented.
- `activation-space-held` — click, Space held (KeyDown-only step): the button :active (blue) at the settle, no keyup, no click.
- `activation-space-release` — the released hold: click after the keyup listeners, active cleared.
- `focus-visible-click` / `focus-visible-tab` / `focus-visible-ctrl-s` / `focus-visible-tab-move` — click: focus colour, no ring; Tab: b ringed (the keyup entries already see b focused); Ctrl+S after a click: no ring; the ring survives a mouse move.

**Chromium citations:**

- Click target = common ancestor: `core/input/mouse_event_manager.cc:358-367` (`mouse_release_target->CommonAncestor(*mousedown_element_, ParentForClickEvent)`; `ParentForClickEvent` at `core/input/event_handling_util.cc:88-90`); the captured override and the up-tail order (release → pending-capture processing → click → boundary settling): `core/input/pointer_event_manager.cc:1237-1241, 1272-1313`.
- Enter/Space activation: `core/html/html_element.cc:4314-4338` (`HandleKeyboardActivation`: keydown " " → SetActive; keypress '\r' → simulated click; keypress ' ' → default-handled; keyup " " → click if active), the button override `core/html/forms/html_button_element.cc:285-296`; the keypress only exists when the platform key event carries text: `core/input/keyboard_event_manager.cc:330-389` (the `text[0] == 0` early return).
- :focus-visible: `core/css/selector_checker.cc:3888-3901` (`!last_focus_from_mouse || had_keyboard_event`), the origin set at focus (`core/dom/document.cc:6068-6070`), had_keyboard_event set on unmodified keydown (`core/input/keyboard_event_manager.cc:354-357` → `core/dom/node.cc:3470-3482`) and cleared on mouse focus (`core/dom/element.cc:8867-8868`).
- Mouse-down focus after the listeners, unprevented: `core/input/event_handler.cc:998-1001` → `core/input/mouse_event_manager.cc:537-636` (HandleMouseFocus).

**Tests:** `go test -p 1 -count=1 ./gpui/ ./internal/parity/... ./base ./internal/authorspec ./internal/inspectorspec ./internal/dropspec ./internal/richtextspec ./examples/zterm` — all pass (four pin-era expectations updated with divergence-toward-Chrome notes, never silently: interact_test's click-after-mouseup order and the Enter timing subtests, the two capture order tests, the visualtest keydown-only Enter rule, and base's disabled-control tests kept their meaning through the armed-chain fix).

**Limitations:**

- The activation-element model is still the pin's (any element with OnClick listeners activates on Enter/Space); Chrome activates only elements with activation behaviour (buttons, form controls) — a plain div with a click listener would not. Out of this ticket's scope.
- The keyboard :active state stays set on an element whose focus moved mid-press (Chrome's SetActive also stays until a mouse event clears it); the click itself is suppressed by the generation guard.
- The mixed-key disarm rule (releasing another key mid-Space cancels the pending click) is the pin's, kept deliberately: Chrome's active state would still click.
- Text inputs' always-ring arm and the accessibility always-show override of `MatchesFocusVisiblePseudoClass` are future work; the mouse-focus transfer still runs mid-bubble (the pin's position — Chrome runs it after the listeners; unobservable in the current fixtures because mouse entries carry no focus field).
- The Space scroll suppression: the armed activation consumes the key so the keyboard-scroll default does not run (Chrome's keypress '\r'/' ' arms SetDefaultHandled); a focused non-clickable element still scrolls on Space as before.
