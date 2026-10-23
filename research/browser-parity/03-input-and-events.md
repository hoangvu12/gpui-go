# 03 · Input and events: how the browser behaves, and how gpui-go differs

Ticket: `.scratch/browser-parity/issues/03-input-and-events.md`. Researched 2026-10-23. Read only, nothing built.

Sources and pins:
- **Chromium** `0d4dcd4f` (sparse checkout, `%TEMP%\research-src\chromium`). Paths below are relative to `third_party/blink/renderer/` unless they start with `cc/` or `ui/`.
- **gpui-go**, this worktree (`research/browser-parity` @ `8f5ad4d`): `gpui/`, plus `base/` where Base supplies a behaviour the core lacks. All input handling is Go: nothing in `reference/native/src` (renderer, text, layout, a11y) touches input.
- **Specs.** UI Events, Pointer Events 3, HTML (dnd, interaction), Selectors 4, CSS Overscroll. A web-fetch helper read the spec pages. Its verbatim quotes are marked [V]. Points it could only summarise, or that I know but didn't re-read, are [I]. Mouse and wheel events now live in Pointer Events; the UI Events page says so.
- **Earlier findings** this report revisits: `research/zgui-terminal/parity-report.md` §2, limitations #9-12, 20-21, 25-26 and 29.

Markers: **[V]** means I read it in source. **[I]** means it's inferred and not traced end to end. Sizes: S ≤ 2 days, M ≤ 1 week, L ≤ 3 weeks, XL more than that. All "Go" work is in `gpui/`. Nothing here needs a native (Rust) rebuild.

---

## Ranked gaps

| # | Gap | Who notices | Work | Size |
|---|-----|-------------|------|------|
| 1 | Hover sticks when the pointer leaves the window. Any key press clears every hover. There are no enter/leave events per element. | Every hover style, tooltip and menu | Go | M |
| 2 | Default focus behaviour isn't in the core. Tab navigation lives in Base. tabindex order is reversed from HTML. Clicking empty space doesn't blur. Focus survives its element disappearing. No inert. | Every keyboard user, and every app without Base | Go | M |
| 3 | Drag and drop has no per-target enter/leave/over, no drag-end, no drop-effect negotiation, and no Escape or deactivation cancel. OLE always reports COPY. | Every drag UI (zterm #9-11) | Go | M-L |
| 4 | Key events carry no physical `code`, and Shift is folded into the key (`ctrl-shift-]` becomes `ctrl-}`). Bindings run *before* key listeners. | Shortcut authors, non-US layouts (zterm #25-26) | Go | M |
| 5 | Pointer capture only makes the captured hitbox *also* hovered. There's no retargeting, no got/lost events, and no recovery from a lost OS capture. | Sliders, dividers, any drag (zterm #20-21) | Go | S-M |
| 6 | Wheel: no scroll latching or chaining (nested scrollers move together). `WHEEL_PAGESCROLL` breaks. Pixels per notch differ. No keyboard scrolling. | Every nested scroll view | Go | M |
| 7 | `:focus-visible` follows the *current* input modality, not how focus arrived. A mouse move hides the ring, and Ctrl+key shows it. | Keyboard users | Go | S |
| 8 | Text isn't selectable in the core. Selection, I-beam and double/triple-click live in Base's TextView. | Every label, log and error message | Go | L |
| 9 | IME is IMM32. Chromium is TSF-only. No composition events reach listeners, and keydowns during composition are dropped. | CJK users, Win+H voice typing, the touch keyboard | Go (COM) | L |
| 10 | Click needs press and release on the *same* element. The browser uses their common ancestor. Enter activates on key-up instead of key-down. | Buttons with moving children; key-repeat activation | Go | S |
| 11 | Pointer types: no WM_POINTER, so no pen or touch identity, pressure or multi-touch. Mouse moves aren't coalesced per frame. | Pen and touch screens; heavy drag handlers | Go | M-L |
| 12 | Cursors: `grab`, `grabbing`, `copy` and `context-menu` fall back to the arrow (zterm #23). There's no automatic I-beam over text. | Drag handles, text | Go + cursor assets | S |

#1, #2 and #5 are the base the others build on. Drag enter/leave (#3) reuses the boundary walk from #1. Default Tab and keyboard scrolling (#2, #6) need the "default action after listeners" order from #4. Start with #7, #10 and the `WHEEL_PAGESCROLL` fix in #6; each is a few lines.

---

## 1. Hover, boundary events and leaving the window

**Spec.** Pointer Events, which now holds the mouse and wheel sections: leaving fires out → leave events (child to parent), entering fires over → enter (parent to child). For hover-less devices the order is at [Pointer Events §mapping-for-devices-that-do-not-support-hover](https://w3c.github.io/pointerevents/#mapping-for-devices-that-do-not-support-hover) [I]. Nothing in any spec turns hover off because a key was pressed [I].

**Chromium.**
- `core/input/boundary_event_dispatcher.cc:135-216` [V]. It sends `out` to the old target, then `leave` up the exited chain to the common ancestor (child to parent). Then `over` to the new target, then `enter` down the entered chain (parent to child). The common ancestor comes from walking both chains (`:59-86`).
- A mouse that leaves the widget becomes a `kMouseLeave` event. It skips the hit test but clears hover and active state: `UpdateHoverActiveState(..., nullptr)` (`core/input/event_handler.cc:1158-1165`) [V].
- Hover isn't cleared by keyboard input. The only keyboard link is `:focus-visible`, in §7.

**gpui-go.**
- Hover is a per-frame hit-test answer. `HitboxID.IsHovered` (`gpui/mouseevent.go:277-289`) [V] returns true for the captured hitbox, **false whenever the last input was the keyboard**, and otherwise checks the last hit test. Every `KeyDownEvent` sets the keyboard modality (`gpui/interact.go:466-481`) [V]. Typing into a field therefore drops every hover style in the window until the mouse moves.
- WM_MOUSELEAVE only sets the window's `hovered` flag and calls an optional hook (`gpui/desktop_windows.go:1744-1751`, `gpui/win32host_windows.go:876-879`) [V]. Nothing reaches element dispatch.
  - The tracked mouse position keeps its last in-window value, so the element at the edge stays hovered after the pointer leaves.
  - `OnHover` says so itself: "the port's platform input has no mouse-exited event yet" (`gpui/interact.go:1495-1496`) [V].
- Element callbacks: `OnHover(bool)` exists but requires an id and is suppressed while a press is pending or a drag is active (`gpui/interact.go:1460-1497`) [V]. Styles via `Hover`/`GroupHover` work. There are no out/over pairs with a related target.
- **Matches:** hover is recomputed when layout moves under a still mouse. `ensurePaintHitTest` (`gpui/interact.go:513-525`) [V] does what Chromium's `RecomputeMouseHoverStateIfNeeded` (`core/input/mouse_event_manager.cc:381`) [V] does. Occlusion via `HitboxBlockMouse` (`gpui/mouseevent.go:392-416`) [V] is a reasonable stand-in for top-most hit testing.

**Gap.**
1. Turn WM_MOUSELEAVE into a synthetic "pointer left" dispatch: hit test empty, hover listeners false, cursor reset.
2. Stop suppressing hover on keyboard input. Keep the suppression only for `:focus-visible` (§7).
3. Add `OnPointerEnter`/`OnPointerLeave` per hitbox. Diff the previous and current frames' hovered-id lists, walking ancestors the way Chromium's common-ancestor walk does.

Go, M. The same diff gives drag enter/leave (§3) for free.

---

## 2. Focus: Tab order, defaults, fixup, inert

**Spec.**
- [HTML §the-tabindex-attribute](https://html.spec.whatwg.org/multipage/interaction.html#the-tabindex-attribute) [I]: positive values come first, in ascending order. Then 0 in tree order. Negative values are focusable but never reached by Tab.
- Inert nodes ([HTML §inert-subtrees](https://html.spec.whatwg.org/multipage/interaction.html#inert-subtrees)) [V, quoted]: "Hit-testing must act as if the 'pointer-events' CSS property were set to 'none'" and "Inert nodes generally cannot be focused."
- The [sequential focus navigation starting point](https://html.spec.whatwg.org/multipage/interaction.html#sequential-focus-navigation-starting-point) is initially unset, and a click may move it [I].
- The focus fixup rule [I] (the helper couldn't reach it): when the focused element stops being focusable, focus goes back to the body.

**Chromium.**
- **Tab is a default action.** It runs only if the page didn't cancel keydown, and not with Ctrl, Meta or Alt held: `DefaultTabEventHandler` → `FocusController::AdvanceFocus` (`core/input/keyboard_event_manager.cc:453-454, 574-616`) [V].
- **Order.** Exact tabindex first, then the next-greater, then 0 (`core/page/focus_controller.cc:1280-1407`) [V].
- **Mouse focus.** On mousedown, if `pointerdown`/`mousedown` weren't cancelled (`core/input/event_handler.cc:998-1001`) [V], Chromium walks up to the nearest mouse-focusable ancestor. If there's none, it calls `SetFocusedElement(nullptr)`: "We call setFocusedElement even with !element in order to blur current focus element" (`core/input/mouse_event_manager.cc:552-562, 626-634`) [V].
- **Fixup.** If the focused element is no longer focusable (hidden, disabled, inert), a zero-delay timer blurs it (`core/dom/document.cc:3261-3275`) [V].

**gpui-go.**
- **No default Tab in the core.** `Window.FocusNext`/`FocusPrev` exist (`gpui/focus.go:498-541`) [V], but only Base binds Tab (`base/root.go:47-48, 273-279`) [V]. An app without Base has no keyboard navigation.
  - Because it's a key binding, it runs *before* the app's key listeners (§4). That's the zterm #26 workaround.
- **Order is reversed.** `TabIndex` is documented as "positive indices follow 0 in ascending order; negative indices precede it" (`gpui/focus.go:209-219`) [V]. Paths sort as plain numbers (`gpui/focus.go:638-656`) [V], so -1 < 0 < 1.
  - Handles default to `tabStop: false` (`gpui/helpers_focus.go:88`) [V], so an element is reachable only when it opts in.
  - A negative index *is* reachable if `TabStop(true)` is set.
- **Mouse focus.** It walks to the nearest focusable ancestor and honours `PreventDefault`, which matches. The innermost focusable div focuses itself in bubble and calls `PreventDefault` so its parents don't (`gpui/interact.go:1231-1247`) [V].
  - But a press on non-focusable space does **nothing**. The focused input stays focused.
- **No fixup.** `Window.Focused` reports the handle even when its element wasn't rendered this frame (`gpui/focus.go:458-469`) [V].
  - Key dispatch quietly falls back to the root path (`gpui/key_dispatch.go:888-898`) [V].
  - The focus listeners see a focus-out, because the rendered focus path is empty (`gpui/helpers_focus.go:128-136`) [V]. When the element comes back, so does focus.
- **No inert or starting point.** Modal traps are left to components. Tab after a click starts from the focused handle, or from the first stop (`gpui/focus.go:785-807`) [V].
- **No scroll into view.** `Window.Focus` never scrolls the element into view (`gpui/focus.go:436-456`) [V]. `Element.focus()` does unless `preventScroll` is set [I].

**Gap.**
1. Move Tab and Shift+Tab into the core as a default action after key listeners (needs §4).
2. Use HTML tabindex order.
3. Blur on a press that finds no focusable ancestor.
4. At the end of each frame, blur a focused handle whose element wasn't rendered or is inside an inert subtree.
5. Add an `Inert()` div flag that blocks hit testing (like `BlockMouse`) and focus.
6. Optionally, scroll focus into view.

Go, M. This is the most important core piece for "a browser-like core without shipping components".

---

## 3. Drag and drop

**Spec.** [HTML §drag-and-drop-processing-model](https://html.spec.whatwg.org/multipage/dnd.html#drag-and-drop-processing-model) [I, helper's summary]:
- `drag` fires on the source every tick.
- When the target changes, `dragenter` goes to the new target first, then `dragleave` to the old one.
- `dragover` goes to the current target. **If it isn't cancelled, the operation becomes "none"** (editable text targets excepted).
- On drop, `drop` goes to the target. On Escape or cancel, `dragleave` goes to the target and the operation becomes "none".
- `dragend` goes to the source, with the final `dropEffect`.

**Chromium.**
- **Target tracking.** `EventHandler::UpdateDragAndDrop` (`core/input/event_handler.cc:1375-1478`) [V]. `drag` on the source comes first. On a target change, `dragenter` goes to the new target and then `dragleave` to the old ("conforms to section 7.9.4 of the HTML 5 spec", `:1415-1446`); later moves send `dragover`.
- **Cancel and drop.** Cancel sends `dragleave` (`:1480-1494`); drop sends `drop` (`:1496-1510`). Both clear the drag state and stop autoscroll (`:1512-1518`). Drag autoscroll follows the target (`:1409-1413`).
- **Effect negotiation.** If the page set no `dropEffect`, `DefaultOperationForDrag` picks move, then copy, then link from what the source allows. A `dropEffect` outside the source's mask becomes none (`core/page/drag_controller.cc:870-889, 918-926`) [V]. The OS cursor shows the outcome.
- **Thresholds.** Drag starts after 4 px on either axis (3 on Mac), with a TODO to read the OS value (`core/input/mouse_event_manager.cc:117-126`) [V]. Every page drag, even inside one page, goes through the OS drag loop (`DoSystemDrag`, `core/page/drag_controller.cc:1529`) [V]. Escape and the cursor therefore come from the OS.

**gpui-go.**
- **Internal drags.** `OnDrag` starts once the pointer has moved more than 2 px in straight-line distance (`gpui/interact.go:48, 1404-1417`) [V].
  - Targets get the `OnDragOver` *style*, `OnDragMove` (every move, inside or outside, per type: `:951-968`) and `OnDrop` on mouse-up (`:1328-1364`) [V].
  - There's **no drag-leave by design** (`gpui/drop_listener.go:10-14`) [V], **no drag-end**, and nothing tells the source whether the drop landed.
  - A mouse-up with no taker just drops the drag (`gpui/mouseevent.go:633-640`) [V].
  - Escape, window deactivation and a lost OS capture never cancel a drag. Nothing outside `filedrop.go` and `mouseevent.go` touches `activeDrag` [V].
  - No autoscroll near scroller edges.
- **OS drops (OLE target).**
  - DragEnter accepts only `CF_HDROP` (files) and negotiates COPY or NONE (`gpui/dragdrop_windows.go:538-554`) [V].
  - DragOver and Drop **always** return COPY (`:585, :641`) [V], whether or not an element would take the drop. The user sees "copy" over targets that will ignore it.
  - Text, URL and HTML drags from other apps aren't accepted.
- **Matches.** `CanDrop` predicates with fall-through to outer targets (`gpui/drop_listener.go:113-124`) [V] are close to "first element that cancels dragover". Group drag-over styles exist. Dragging files out to the OS works (`DoDragDrop`, `gpui/dragdrop_windows.go:1286-1306`) [V].

**Gap.**
1. Add `OnDragEnter`/`OnDragLeave` per target (from the §1 boundary diff), with an element-local position.
2. Add `OnDragEnd(dropped bool, effect)` on the source.
3. Cancel on Escape (a core default action), on deactivation, and on WM_CAPTURECHANGED.
4. Answer OLE `DragOver` from the hovered target's `CanDrop` instead of a constant COPY.
5. Optionally, accept text and URL formats, and autoscroll the latched scroller.

This closes zterm #9, #10 and #11. Go, M-L.

---

## 4. Keyboard events: `key` vs `code`, and dispatch order

**Spec.**
- [UI Events §keys-modifiers](https://w3c.github.io/uievents/#keys-modifiers) [V, summary quote]: "In some cases, modifier keys change the `key` value for a key event."
- Chromium cites the keys guidelines when it picks `key`: [UI Events §keys-guidelines](https://w3c.github.io/uievents/#keys-guidelines) [V, as cited at `ui/events/keycodes/platform_key_map_win.cc:308-311`].
- `code` names the physical key and ignores the layout: [UI Events KeyboardEvent code](https://w3c.github.io/uievents-code/) [I].

**Chromium.**
- **`key`.** It looks the printable key up with the full modifiers, then with only Shift and CapsLock, then with none (`ui/events/keycodes/platform_key_map_win.cc:294-336`) [V].
  - For **Ctrl+Shift+]** on US that gives `key = "}"`, with `shiftKey = true` still set and `ctrlKey = true`.
  - Right Alt on an AltGr layout reports `AltGraph` (`:296-299`).
- **`code`.** From the scan code plus the extended bit 0xE000 (`ui/events/win/events_win_utils.cc:369-371, 484-495`) [V], so it's `BracketRight`. The repeat flag is read at `:138` [V].
- **Order.** Keydown goes to the focused element (or body) first. Access keys can mark it handled (`core/input/keyboard_event_manager.cc:330-363`) [V].
  - Only if it wasn't cancelled does the default handler run: the editor, then Tab, Escape, Enter or arrow and page scrolling (`:426-490`) [V].
  - Keypress follows if keydown wasn't cancelled and produced text (`:373-411`) [V].
  - While an IME is composing (keyCode 229), default actions are skipped (`:447-451`) [V].

**gpui-go.**
- **Translation.** `win32TranslateKeyDown` uses only the low byte of the scan code and drops the extended bit (`gpui/keyboard_win32.go:292-300`) [V]. `Keystroke` has `Key`, `KeyChar` and `Modifiers`, with **no `code` and no location** (`gpui/keystroke.go:132-146`) [V].
- **Shift folding.** For shiftable OEM and digit keys, the shifted character replaces the key and **Shift is cleared** (`gpui/keyboard_win32.go:309-312`) [V]. Ctrl+Shift+] becomes `ctrl-}`, so `ctrl-shift-]` never matches (zterm #25).
- **Dispatch order.** Interceptors, then chord matching, then **actions from key bindings, then key listeners** (capture, then bubble) (`gpui/key_dispatch.go:807-880, 939-973`) [V]. An action that stops propagation hides the key from every listener. That's the reverse of the browser, where page listeners come before defaults (zterm #26).
- **IME.** `VK_PROCESSKEY` (0xE5) has no mapping (`keyFromVKey`, `gpui/keyboard_win32.go:394-404`) [V]. Keydowns during composition aren't dispatched at all [I]. They can't be observed with an `isComposing` flag.
- **Matches.**
  - Dead-key and AltGr detection: `PreferCharacterInput` lets text win over a binding (`gpui/keyboard_win32.go:410-475`, `gpui/key_dispatch.go:859-867`) [V]. That's Chromium's AltGraph handling by another route.
  - Auto-repeat is reported (`IsHeld`, `gpui/win32host_windows.go:1265`) [V].
  - A consumed WM_KEYDOWN skips `TranslateMessage`, so no stray WM_CHAR follows (`gpui/win32host_windows.go:1616-1623`) [V]. That's the browser's "cancelled keydown suppresses keypress".

**Gap.**
1. Add `Code` (from scan code plus extended bit) and `Location` to `Keystroke`.
2. Keep Shift in `Modifiers` and keep both forms. The keymap should match `ctrl-shift-]` (code form) or `ctrl-}` (key form).
3. Add a dispatch mode where key listeners run first and bindings and core defaults (Tab, Escape-cancels-drag, keyboard scroll) run only if nobody stopped the event.

The keymap is GPUI-shaped, so the ordering change needs a decision in 07. A per-window or per-context flag is the gentlest form. Go, M.

---

## 5. Pointer capture

**Spec.**
- Pointer Events [V, quoted]: "If the pointer capture target override has been set for the pointer, set the target to pointer capture target override object." Every event is retargeted to the capturing element.
- Capture is released implicitly after `pointerup` and `pointercancel` [I].
- `gotpointercapture` and `lostpointercapture` fire from "process pending pointer capture" [V, quoted step list].
- See [Pointer Events §pointer-capture](https://w3c.github.io/pointerevents/#pointer-capture) [I, anchor].

**Chromium.**
- `SetPointerCapture` succeeds only while a button is down (`core/input/pointer_event_manager.cc:1440-1447`) [V].
- Pending capture is processed before each event: lost to the old target, got to the new one (re-targeted to the document if the element was removed) (`:1369-1414`) [V].
- Pointerup and pointercancel release capture (`:1244-1246`) [V].
- A click during capture targets the capturing element (`:1237-1242`, `core/input/mouse_event_manager.cc:359-360`) [V].

**gpui-go.**
- `Window.CapturePointer(id)` (`gpui/interact.go:547-561`) [V] makes `IsHovered` true for that hitbox (`gpui/mouseevent.go:282-284`) [V]. The hit test is unchanged, so **other hitboxes under the pointer are hovered and get events too**.
- Release happens on any mouse-up (`gpui/mouseevent.go:642-645`) [V].
- There are no got/lost callbacks and no element-local position (zterm #21).
- The OS side: `SetCapture` on any press and `ReleaseCapture` on any release, even with another button still held (`gpui/desktop_input_windows.go:191-229`) [V].
- **WM_CAPTURECHANGED isn't handled.** No match in `gpui/` [V]. If something else takes capture mid-press, the pending press, the active style and the drag stay stuck until the next mouse-up.

**Gap.**
1. While captured, filter the hit test to the captured hitbox and its ancestors.
2. Add `OnGotPointerCapture`/`OnLostPointerCapture`.
3. Treat WM_CAPTURECHANGED and deactivation as pointercancel: release capture, clear presses, cancel the drag.
4. Only `ReleaseCapture` once no button is held.
5. Add `MouseEvent.LocalPosition(hitbox)`.

Go, S-M. This closes zterm #20 and #21; the divider can stop using `OnMouseMoveAll`.

---

## 6. Wheel, touchpad and keyboard scrolling

**Spec.**
- Scroll chaining (in [CSS Overscroll §scroll-chaining-and-boundary-default-actions](https://drafts.csswg.org/css-overscroll-1/#scroll-chaining-and-boundary-default-actions)) [V, quoted] is when "scrolling is propagated from one scroll container to an ancestor scroll container following the scroll chain".
- `overscroll-behavior: contain | none` stops chaining (`#valdef-overscroll-behavior-contain`) [I].

**Chromium.**
- **Wheel to pixels.** Notches × `SPI_GETWHEELSCROLLLINES` × **100/3 px** per line. `WHEEL_PAGESCROLL` becomes a page-granularity scroll (`ui/events/blink/web_input_event_builders_win.cc:297-336`) [V].
- **Latching.** cc latches a gesture to the first scroller on the chain that can take the delta. One with non-auto `overscroll-behavior` cuts the chain (`cc/input/input_handler.cc:2341-2397`) [V].
- **Keyboard.** Arrows, PageUp/PageDown, Space and Shift+Space scroll the focused scroller as a default action (`core/input/keyboard_event_manager.cc:492-572`) [V].
- **Pacing.** Mouse-move, wheel and touch-move events are coalesced to animation frames (`platform/widget/input/main_thread_event_queue.cc:890-909`) [V].

**gpui-go.**
- **Pixels per notch.** Wheel delta is lines × the window's text line height (`gpui/win32input_windows.go:81-98`, `gpui/scroll_element.go:155, 197-204`) [V]. At a 20 px line height that's 60 px per notch, against Chromium's 100.
- **`WHEEL_PAGESCROLL` breaks.** `WheelScrollLines` is a `uint32` and is multiplied directly (`gpui/win32input_windows.go:90`, `gpui/desktop.go:373`) [V]. 0xFFFFFFFF lines makes a ~4×10⁹-line jump.
- **No latching.** Each scroll container's listener runs in bubble whenever `ShouldHandleScroll` (the pointer is anywhere inside it) and never stops propagation (`gpui/scroll_element.go:158-194`; the list does the same, `gpui/listelement.go:1156-1159`) [V].
  - Nested scrollers both move on the same tick [I: from the code, not run].
  - The offset moves unclamped and is clamped at prepaint, so an inner scroller at its limit still "eats" the delta while the outer also scrolls [I].
- **Touchpad matches.** Precise deltas and phases come through Direct Manipulation (`gpui/directmanipulation_windows.go`) [V], and pinch is already ahead of the zgui pin.
- **Not paced.** No keyboard scrolling in the core [V: no `pageup`/`pagedown` handling outside key naming]. Every WM_MOUSEMOVE dispatches immediately, with no frame coalescing.

**Gap.**
1. Latch the wheel to the innermost scroller that can move in that direction. Keep the latch for the gesture, or for a short timeout on a mouse wheel. Stop propagation once consumed. Add an `OverscrollContain` style.
2. Special-case `WHEEL_PAGESCROLL`.
3. Consider 100/3 px per line for parity.
4. Add keyboard scrolling as a core default action (§4).
5. Optionally, coalesce moves to frames and keep the dropped ones as `CoalescedEvents`.

Go, M.

---

## 7. `:focus-visible`

**Spec.** [Selectors 4 §the-focus-visible-pseudo](https://drafts.csswg.org/selectors-4/#the-focus-visible-pseudo). The helper couldn't reach this section. From memory [I]: the heuristics say show it after keyboard focus, after script focus that didn't follow a mouse focus, and always on text inputs.

**Chromium.**
- `SelectorChecker::MatchesFocusVisiblePseudoClass` (`core/css/selector_checker.cc:3861-3900`) [V] matches when any of these hold:
  - the "always show focus" setting is on
  - the element is a text input
  - **the last focus did not come from the mouse**
  - a keyboard event happened since
- Keyboard events count only if no Ctrl, Alt or Meta was held (`core/input/keyboard_event_manager.cc:354-358`) [V].
- A mouse move doesn't change any of these.

**gpui-go.**
- `FocusVisible` applies while focused **and** `lastInputWasKeyboard` (`gpui/interact.go:1076`) [V].
- Any `MouseMoveEvent` switches back to mouse modality (`gpui/interact.go:472-475`) [V], so after Tab-focusing a button, nudging the mouse hides the ring.
- Ctrl+S counts as keyboard, so it shows the ring.
- `Window.Focus` from code shows no ring when the last input was the mouse.

**Gap.** Record a focus *origin* (keyboard, mouse or programmatic) on `Window.Focus`, and set a sticky "had keyboard event" flag that ignores modified keys. Add an `AlwaysFocusVisible` handle flag for text inputs. Go, S.

---

## 8. Text selection and the default cursor

**Spec.** Text is selectable by default ([CSS UI §content-selection](https://drafts.csswg.org/css-ui/#content-selection), `user-select`) [I]. With `cursor: auto` the UA shows an I-beam over selectable text ([CSS UI §cursor](https://drafts.csswg.org/css-ui/#cursor)) [I].

**Chromium.**
- `SelectionController` starts a selection on mousedown and extends it on drag. `HandleDoubleClick` selects a word and `HandleTripleClick` a paragraph (`core/editing/selection_controller.cc:1077, 1110, 1164`) [V], all respecting `user-select` (`:96-216`) [V].
- The auto cursor is an I-beam when `ShouldShowIBeamForNode` (`core/input/event_handler.cc:839-847`) [V].

**gpui-go.** The class mapper says it outright: "text in a div is not selectable, so select-* has no effect" (`gpui/class.go:1108`) [V]. Selection, word and line logic, touch handles and copy are in `base/text_selection*.go` and `base/text_view.go` [V]. The cursor defaults to the arrow unless set.

**Gap.** This is a core-versus-component call for 07. A browser-like core would own a selection model over rendered text runs: hit-test to character, click count to word or line, drag to extend, Ctrl+C. It would also need a `UserSelect` style and an automatic I-beam. Go, L. The text runs and the shaped-line hit testing already exist (`gpui/shaped_line.go`, `CharacterIndexForPoint` in `gpui/ime.go:163`) [I].

---

## 9. IME and composition events

**Spec.**
- [UI Events §events-composition-key-events](https://w3c.github.io/uievents/#events-composition-key-events) [V, summary quote]: key events during composition "MUST have the isComposing attribute set to true".
- compositionstart, compositionupdate and compositionend surround the session [I].

**Chromium.**
- On Windows the IME layer is **TSF only**. `ui/base/ime/win/BUILD.gn:12-27` lists `input_method_win_tsf`, `tsf_bridge` and `tsf_text_store`, with no IMM32 input method in the tree [V].
- Keydowns during composition are delivered with keyCode 229, and their default actions are skipped (`core/input/keyboard_event_manager.cc:53, 431, 447-451`) [V].

**gpui-go.**
- IMM32 through `ImmGetCompositionStringW`, positioned at the caret (`gpui/ime_win32.go:5-26, 735-752`) [V]. WM_IME_REQUEST (reconversion, char position) isn't handled (`gpui/ime_win32.go:741-747`) [V].
- The handler is the macOS-style `IMEInputHandler` (marked text, `BoundsForRange`) (`gpui/ime.go:135-166`) [V]. Nothing tells key listeners or the app that a composition started or ended.

**Gap.**
1. Short term: add composition start/update/end callbacks on the input handler, and a `KeyDownEvent.IsComposing` flag (deliver the 0xE5 key rather than dropping it).
2. Long term: a TSF text store. It brings the touch keyboard's suggestions, Win+H dictation, reconversion and modern IMEs that run degraded through IMM32 [I].

Go (COM), L.

---

## 10. Click synthesis and keyboard activation

**Spec.** Pointer Events, which now holds the mouse sections, covers the click target. Per the earlier UI Events text [I], the target of `click` is the nearest common inclusive ancestor of the mousedown and mouseup targets. `dblclick` follows a click with detail 2 [I].

**Chromium.**
- **Click target.** `DispatchMouseClickIfNeeded` targets `mouse_release_target->CommonAncestor(*mousedown_element_)`, or the capture target. Left button gives `click`, others `auxclick` (`core/input/mouse_event_manager.cc:332-379`) [V].
- **dblclick** fires right after a click with detail 2 (`core/events/mouse_event.cc:431-449`) [V].
- **Keyboard activation.** Enter activates on **keypress** (so it auto-repeats). Space sets `:active` on keydown and clicks on keyup (`core/html/html_element.cc:4314-4344`) [V].

**gpui-go.**
- **Click target.** A click fires only on the element that took the press, and only if that same hitbox is hovered at release (`gpui/interact.go:1420-1457`) [V].
  - A parent button whose child moved, or a press on child A and release on sibling B, gives no click. The browser would click the common parent.
  - Aux clicks go to `OnAuxClick` (matches).
- **Click count** uses the system double-click time and rectangle (`gpui/desktop_input_windows.go:97-154`) [V], which matches. There's no separate dblclick (use `ClickCount()`).
- **Keyboard click** needs both Enter and Space released on the focused element, with no focus change between (`gpui/interact.go:1160-1224`) [V]. Space is right. Enter fires on key-up and doesn't repeat. There's no `:active` while Space is held.

**Gap.**
1. Keep the down target's ancestor chain. On up, click the deepest clickable ancestor shared with the up target.
2. Fire Enter on key-down.
3. Set the active state while Space is held.

Go, S.

---

## 11. Pointer types, pressure, coalesced events

**Spec.** `pointerType`, `pressure`, tilt and `getCoalescedEvents()` ([Pointer Events](https://w3c.github.io/pointerevents/); the IDL quoted [V], section anchor not reached).

**Chromium.**
- Blink has a full pointer pipeline with coalesced and predicted events (`core/input/pointer_event_manager.cc:960-990, 1107-1140`) [V], and it pairs moves to frames (§6).
- The Windows WM_POINTER host code (`ui/views/win`) isn't in the sparse checkout. That Chromium reads pen and touch through WM_POINTER is [I].

**gpui-go.**
- Only `WM_MOUSEMOVE`, the buttons and the wheel are handled (`gpui/win32host_windows.go:835-845`) [V]. Pen and touch arrive as promoted mouse messages.
- `TouchClickEvent` is never produced: "The Windows port has no touch input source" (`gpui/interact.go:84-95`) [V]. `lastInputWasTouch` is always false (`gpui/interact.go:454-458`) [V].
- Direct Manipulation does handle touchpad pan and pinch [V].

**Gap.** Add a `PointerEvent` family (id, type, pressure, tilt, coalesced points) from WM_POINTER, with mouse events derived from it. Go, M-L. Low priority for desktop-first apps.

---

## 12. Cursors

**Spec.** [CSS UI §cursor](https://drafts.csswg.org/css-ui/#cursor) [I] defines `grab`, `grabbing`, `copy`, `context-menu` and others; the UA supplies images where the OS lacks them.

**Chromium.** Blink maps `cursor: grab` and `grabbing` to `ui::Cursor` types (`core/input/event_handler.cc:831-834`) [V]. The Windows cursor images come from Chromium's own resources [I]; `ui/resources` isn't in the sparse checkout.

**gpui-go.**
- `CursorClosedHand`, `CursorOpenHand`, `CursorDragCopy` and `CursorContextualMenu` fall back to `IDC_ARROW` on Windows (`gpui/desktop.go:80-95`, `gpui/desktop_windows.go:440-463`) [V]. That's zterm #23.
- Matches: resolving the cursor per hitbox, newest request first (`gpui/interact.go:619-649`) [V], is a fine stand-in for CSS `cursor` inheritance.

**Gap.** Bundle four `.cur` resources, or build them with `CreateIconIndirect`. Go plus assets, S.

---

## Where gpui-go already matches the browser

- **Event phases.** Capture then bubble, with `StopPropagation` and `PreventDefault`. Mousedown-to-focus honours `PreventDefault`, as Chromium only runs `HandleMouseFocus` when the press wasn't cancelled (`gpui/interact.go:1231-1247` vs `core/input/event_handler.cc:998-1001`) [V].
- **Mouse focus target.** Focus goes to the nearest focusable ancestor of the press (same walk as `core/input/mouse_event_manager.cc:552-562`) [V].
- **Hover recompute.** Hover updates when layout moves under a still mouse (§1) [V].
- **Implicit OS capture.** Drags keep working outside the window (`SetCapture`, `gpui/desktop_input_windows.go:191-193`) [V].
- **Click count** from the system double-click time and rectangle [V].
- **Text-producing keys.** AltGr and dead keys yield text instead of bindings, and a consumed keydown never produces WM_CHAR [V].
- **Focus events.** Focus, blur, focus-in and focus-out (`gpui/helpers_focus.go:181-240`), and `InFocus` (`:focus-within`, `gpui/interact.go:1070-1072`) [V].
- **Touchpad.** Precise pan and pinch with phases through Direct Manipulation [V].
- **OS drags.** File drop in, and drag out to the OS [V].

## How the parity report's §2 limitations come out

| zterm # | Limitation | Browser behaviour | Here |
|---|---|---|---|
| 9 | No drag-leave | `dragleave` on target change and cancel | §3: add, from the §1 boundary diff |
| 10 | No per-target drag-over position | `dragover` with client and offset coordinates | §3 + §5 local position |
| 11 | No drag-end | `dragend` on the source with `dropEffect` | §3 |
| 12 | Drag preview inherits no text style | The browser's preview is a snapshot of the source element, so it inherits naturally [I] | Not input. The preview is prepainted as a root (`gpui/interact.go:724-736`) [V]; inheriting the source's text style is a small Go change. |
| 20 | No element pointer capture | Retargeted capture | §5: the API exists (CapturePointer) but doesn't retarget |
| 21 | No element-local pointer position | `offsetX`/`offsetY` | §5 |
| 25 | `ctrl-shift-]` never fires | `key "}"` + `shiftKey` + `code "BracketRight"` | §4 |
| 26 | Base's Tab binding runs before listeners | Tab is a default action after listeners | §2 + §4 |
| 29 | Overlay keys only fire on the focused path | Same in the browser: keys target the focused element. Overlays listen on `document` or `window`, which is always on the path. | By design. The core equivalent is a root-level listener, or the app-level interceptors (`gpui/key_dispatch.go:1259-1274`) [V]. Document it rather than change dispatch. |
