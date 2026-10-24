package gpui

import "fmt"

// This file is browser-core ticket 09 (default actions after key
// listeners, .scratch/browser-core/issues/09-default-actions-after-
// listeners.md, spec D5): the core's default actions for a key event —
// keyboard scrolling, Tab focus movement and element activation — run
// AFTER the element key listeners, and a listener that calls
// Window.PreventDefault skips them, the DOM's listener-then-default
// order. Key bindings keep the GPUI shape (they dispatch before the
// listeners); a per-window flag moves them after the listeners, the
// browser order.
//
// The Chromium reference (0d4dcd4f, %TEMP%\research-src\chromium):
//
//   - The default handlers run after the event's dispatch to listeners,
//     only when the event was not default-prevented:
//     third_party/blink/renderer/core/dom/events/event_dispatcher.cc:
//     474-477 ("Call default event handlers...", `!event_->defaultPrevented()
//     && !event_->DefaultHandled() && is_trusted_or_click`), invoking
//     Node::DefaultEventHandler on the target (event_dispatcher.cc:519).
//     stopPropagation does NOT suppress them: the dispatch's step 10
//     clears the stop flags before the default phase
//     (event_dispatcher.cc:397-401, the DOM §concept-event-dispatch
//     post-dispatch steps).
//   - Node::DefaultEventHandler routes keyboard events to the frame's
//     KeyboardEventManager::DefaultKeyboardEventHandler
//     (third_party/blink/renderer/core/dom/node.cc:3410-3414).
//   - DefaultKeyboardEventHandler (third_party/blink/renderer/core/
//     input/keyboard_event_manager.cc:426-490): the keydown arm runs
//     the editor commands first (:442-445, `frame_->GetEditor().
//     HandleKeyboardEvent` — an accepting text input consumes the key),
//     skips everything during IME composition (kVKeyProcessKey,
//     :447-451), then Tab (:454, DefaultTabEventHandler :574-616 —
//     ctrl/meta/alt never advance focus, Shift goes backward), Escape
//     (:456, the close-watcher stack — browser-core ticket 22), and
//     otherwise the navigation keys (:464, DefaultNavigationKey-
//     EventHandler :526-572 → MapKeyCodeForScroll :67-175).
//   - MapKeyCodeForScroll: Shift or Meta block scrolling; Ctrl only
//     keeps Home/End; arrows scroll by line, Home/End by document and
//     PageUp/PageDown by page (keyboard_event_manager.cc:133-176).
//     Space pages at keypress (DefaultSpaceEventHandler :492-523, with
//     Shift paging backward; the port runs it at keydown — the pin has
//     no keypress phase).
//   - The scroll chain: ScrollManager::BubblingScroll walks from the
//     focused node's enclosing boxes outward, scrolling the first box
//     that can scroll in the axis and chaining when it cannot
//     (third_party/blink/renderer/core/input/scroll_manager.cc:194-232
//     LogicalScroll, RecomputeScrollChain :213-260).
//   - The steps: 40 px per line (cc/input/scroll_utils.h:20
//     kPixelsPerLineStep — 40 CSS px at any device scale, measured at
//     the harness's 2.0), and the page step
//     max(length*0.875, length-40) (cc/input/scroll_utils.cc:50-55
//     CalculatePageStep, kMinFractionToStepWhenPaging 0.875 and
//     kMaxOverlapBetweenPages 40, scroll_utils.h:22-24).
//   - Activation is the focused element's default handler:
//     HTMLElement::HandleKeyboardActivation (third_party/blink/
//     renderer/core/html/html_element.cc:4313-4338) — Space keydown
//     sets the active state, Space keyup clicks when it was set, and
//     a prevented keydown leaves it unset, so the keyup never clicks.
//     Enter clicks at keypress; the pin (and this port until
//     browser-core ticket 12) clicks on keyup for both keys.
//
// Divergences toward CSS, recorded per D0: the activation click runs
// after ALL keyup listeners (the pin ran it as the focused element's
// own keyup listener, before ancestor listeners); Escape's default
// action is not implemented here (browser-core tickets 16 and 22 own
// the drag cancel and the close-watcher stack); the scroll chain
// starts at the focused node (Chromium also starts at the mouse-press
// node — browser-core ticket 15 generalizes the start node).

// KeyDispatchOrder selects when key bindings dispatch relative to the
// element key listeners (browser-core 09, spec D5).
type KeyDispatchOrder uint8

const (
	// BindingsBeforeListeners is the GPUI shape (the default): key
	// bindings dispatch before the key listeners, and a binding whose
	// action stops propagation hides the key from every listener. Core
	// default actions still run after the listeners.
	BindingsBeforeListeners KeyDispatchOrder = iota
	// ListenersBeforeBindings is the browser order: the key listeners
	// run first, then bindings and the core default actions in the
	// default-action position, all skipped when a listener prevented
	// the default.
	ListenersBeforeBindings
)

// String renders the order for diagnostics.
func (o KeyDispatchOrder) String() string {
	switch o {
	case BindingsBeforeListeners:
		return "bindings-before-listeners"
	case ListenersBeforeBindings:
		return "listeners-before-bindings"
	}
	return "unknown"
}

// KeyDispatchOrder returns the window's key dispatch order
// (WindowOptions.KeyDispatchOrder at open, or the last
// SetKeyDispatchOrder). The zero value is BindingsBeforeListeners, the
// GPUI shape.
func (w *Window) KeyDispatchOrder() KeyDispatchOrder {
	if w == nil {
		return BindingsBeforeListeners
	}
	return focusState(w).keyOrder
}

// SetKeyDispatchOrder changes the window's key dispatch order
// (browser-core 09, spec D5): ListenersBeforeBindings gives the full
// browser order — key listeners first, then bindings and the core
// default actions. It applies from the next dispatched key event.
func (w *Window) SetKeyDispatchOrder(order KeyDispatchOrder) {
	if w == nil {
		panic("gpui: SetKeyDispatchOrder requires a live window")
	}
	switch order {
	case BindingsBeforeListeners, ListenersBeforeBindings:
	default:
		panic(fmt.Sprintf("gpui: unknown key dispatch order %d", uint8(order)))
	}
	focusState(w).keyOrder = order
}

// runKeyDefaultActions runs the core default actions of one keyboard
// event (browser-core 09): the focused node's registered default
// actions (activation), then the keyboard-scroll chain from the
// focused node outward, then Tab focus movement — in Chromium's
// default-handler order (keyboard_event_manager.cc:426-490). The phase
// runs after the key listeners and only when the event was not
// default-prevented (event_dispatcher.cc:474-477); a listener's
// StopPropagation does not stop it (the DOM's step 10 clears the stop
// flags first, event_dispatcher.cc:397-401). It reports whether a core
// default acted.
func (w *Window) runKeyDefaultActions(event any, app *App) bool {
	if w == nil || w.DefaultPrevented() {
		return false
	}
	fs := focusState(w)
	tree := fs.renderedDispatchTree()
	_, nodeID := w.focusDispatchPath()

	// The focused node's registered default actions (activation): the
	// registration is focus-gated (registerDivKeyboardClick), so the
	// focused node carries exactly the pin's focused-element activation.
	// Browser-core ticket 14: the body state (no focused handle) runs
	// none — Chromium's default handlers are the focused element's, and
	// the body has no activation (the pre-ticket root fallback made this
	// moot; the body node's own registrations must not fire).
	if fs.focused != 0 {
		if node := tree.Node(nodeID); node != nil {
			for _, action := range node.defaultActions {
				if w.DefaultPrevented() {
					return true
				}
				action(event, w, app)
			}
		}
	}
	if w.DefaultPrevented() {
		return true
	}

	// The keyboard activation default (browser-core ticket 12, spec
	// D12): Enter clicks between the keydown and keyup (the keypress's
	// default — Chromium html_element.cc:4318-4324, the keypress the
	// platform key event carries as its follow-up), Space arms at the
	// keydown (the focused element's pressed state — :active while
	// held, html_element.cc:4318-4321) and clicks at the keyup
	// (:4326-4331, after the keyup listeners, the default-action
	// position). The click dispatches through the focused node's whole
	// path — the DOM's bubbling simulated click — where the pin called
	// only the focused element's own listeners.
	if w.defaultKeyboardActivation(event, app) {
		return true
	}

	switch e := event.(type) {
	case *KeyDownEvent:
		if w.defaultKeyboardScroll(e, app) {
			return true
		}
		if w.defaultTabNavigation(e) {
			return true
		}
		// The contextmenu keyboard trigger (browser-core ticket 17,
		// contextmenu.go dispatchContextMenuFromKey): Shift+F10 fires
		// on key down, after the listeners (Chromium
		// web_frame_widget_impl.cc:1125-1131).
		return w.dispatchContextMenuFromKey(e, app)
	case *KeyUpEvent:
		// The Menu key fires on key up on Windows
		// (web_frame_widget_impl.cc:1115-1118).
		return w.dispatchContextMenuFromKey(e, app)
	}
	return false
}

// nodeClickActivation is one element's keyboard-activation registration
// on its dispatch node (browser-core ticket 12): the click listeners
// the activation fires, the element's pressed-state cell (the
// Space-held :active carrier) and the bounds the KeyboardClickEvent
// reports.
type nodeClickActivation struct {
	// listeners are the element's OnClick listeners (left-button clicks;
	// keyboard activation is Enter/Space only).
	listeners []func(*ClickEvent, *Window, *App)
	// clicked is the element's pressed state (InteractiveElementState's
	// clicked cell — the Active style's carrier).
	clicked *ElementClickedState
	// bounds are the element's prepaint hitbox bounds.
	bounds Bounds
}

// defaultKeyboardActivation is the keyboard activation default action
// (browser-core ticket 12; Chromium HTMLElement::HandleKeyboardActivation
// html_element.cc:4314-4338, the button override html_button_element.cc:
// 285-296). It runs from the default-action position (after the key
// listeners, suppressed by a prevented event) on the FOCUSED node's
// registration — the pin's focus gate. Enter fires the click
// immediately (the keypress between the halves of the press); Space
// arms at the keydown — setting the focused element's pressed state,
// the :active-while-held rule — and fires the click at the keyup when
// the armed generation still matches (a focus move or an intervening
// key disarms it, the pin's guard). Any other key down or up disarms a
// pending Space (the pin's intervening-key rule — a divergence from
// Chrome's always-active SetActive kept deliberately: mixed-key
// presses stay unclicked). It reports whether the key was consumed by
// the activation logic (Chromium's SetDefaultHandled semantics).
func (w *Window) defaultKeyboardActivation(event any, app *App) bool {
	fs := focusState(w)
	is := inputState(w)
	tree := fs.renderedDispatchTree()
	path, nodeID := w.focusDispatchPath()
	node := tree.Node(nodeID)
	activation := node.clickActivation
	if fs.focused == 0 {
		// The body state (browser-core ticket 14): no element holds
		// focus, so no activation — the body node's registrations (a
		// clickable root, for instance) never fire from Enter or Space
		// (Chromium's HandleKeyboardActivation is the FOCUSED element's
		// default handler, html_element.cc:4314-4338; the body is not an
		// activation-behavior element).
		activation = nil
	}

	switch e := event.(type) {
	case *KeyDownEvent:
		stroke := e.Keystroke
		unmodifiedEnter := stroke.Key == "enter" && !stroke.Modifiers.Modified()
		unmodifiedSpace := stroke.Key == "space" && !stroke.Modifiers.Modified()
		if activation != nil && (unmodifiedEnter || unmodifiedSpace) {
			if unmodifiedEnter {
				// Enter activates at the keypress (html_element.cc:4318-
				// 4324's '\r' arm) — the follow-up event of the keydown,
				// so between the two halves of the full press. A prevented
				// keydown already returned above (the keypress never
				// happens when the keydown was canceled,
				// keyboard_event_manager.cc:367-369).
				w.dispatchKeyboardClick(path, nodeID, KeyboardButtonEnter, app)
			} else {
				// Space arms (html_element.cc:4318-4321: SetActive, no
				// default handling — the keydown stays unprevented and
				// unconsumed by this arm alone; the return true below
				// marks the press consumed so the keyboard-scroll default
				// does not also take it, the keypress ' ' arm's job in
				// Chromium, html_element.cc:4325-4329).
				generation := w.FocusGeneration()
				is.pendingKeyboardActivation = &generation
				if activation.clicked != nil && !activation.clicked.Element {
					activation.clicked.Element = true
					w.requestRefresh()
				}
			}
			return true
		}
		// Any other key down cancels a pending Space activation (the
		// pin's intervening-keydown rule).
		is.pendingKeyboardActivation = nil
		return false
	case *KeyUpEvent:
		stroke := e.Keystroke
		if activation != nil && stroke.Key == "space" && !stroke.Modifiers.Modified() {
			generation := is.pendingKeyboardActivation
			is.pendingKeyboardActivation = nil
			armed := generation != nil && *generation == w.FocusGeneration()
			// The pressed state clears with the key (Chrome's simulated
			// click resets it, event_dispatcher.cc:184-186; the state also
			// clears when the keyup lands on a different focused element —
			// the cell below is the keyup's, and the armed check already
			// refuses the click).
			if activation.clicked != nil && activation.clicked.Element {
				activation.clicked.Element = false
				w.requestRefresh()
			}
			if armed {
				w.dispatchKeyboardClick(path, nodeID, KeyboardButtonSpace, app)
			}
			// The keyup is default-handled whether or not the click fired
			// (html_element.cc:4330-4331: SetDefaultHandled after the
			// click decision).
			return true
		}
		// Releasing any other key cancels a pending activation (the
		// pin's intervening-key rule).
		is.pendingKeyboardActivation = nil
		return false
	}
	return false
}

// dispatchKeyboardClick fires the keyboard activation click through
// the focused node's path (browser-core ticket 12): the focused
// element's listeners first, then each ancestor's — the DOM's
// simulated click bubbles (Chromium DispatchSimulatedClick,
// event_dispatcher.cc:163-187; the pin called only the focused
// element's own listeners, the recorded no-bubbling divergence of
// spec D12). All listeners share one ClickEvent whose bounds are the
// focused element's, like the DOM event's single target.
func (w *Window) dispatchKeyboardClick(path []DispatchNodeID, nodeID DispatchNodeID, button KeyboardButton, app *App) {
	tree := focusState(w).renderedDispatchTree()
	node := tree.Node(nodeID)
	if node == nil || node.clickActivation == nil {
		return
	}
	click := ClickEvent{
		Kind:     ClickEventKeyboard,
		Keyboard: KeyboardClickEvent{Button: button, Bounds: node.clickActivation.bounds},
	}
	// The path is root-first; the click walks it focused-first.
	for i := len(path) - 1; i >= 0; i-- {
		ancestor := tree.Node(path[i])
		if ancestor == nil || ancestor.clickActivation == nil {
			continue
		}
		for _, listener := range ancestor.clickActivation.listeners {
			listener(&click, w, app)
		}
	}
	w.requestRefresh()
}

// defaultKeyboardScroll is the navigation-key default action
// (DefaultNavigationKeyEventHandler, keyboard_event_manager.cc:
// 526-572): the mapped key scrolls the chain of scrollable ancestors
// of the focused node, the first that can take the delta consuming it
// (BubblingScroll). It reports whether anything scrolled.
func (w *Window) defaultKeyboardScroll(e *KeyDownEvent, app *App) bool {
	stroke := e.Keystroke
	mapping, ok := mapKeyForScroll(stroke.Key, stroke.Modifiers)
	if !ok {
		return false
	}
	// The editor commands run first in Chromium
	// (keyboard_event_manager.cc:442-445): a focused input handler that
	// accepts text input consumes arrows and Space as caret movement and
	// typing, before any scrolling default.
	if fs := focusState(w); fs.inputHandler != nil && fs.inputHandler.AcceptsTextInput(w, app) {
		return false
	}
	fs := focusState(w)
	tree := fs.renderedDispatchTree()
	path, _ := w.focusDispatchPath()
	for i := len(path) - 1; i >= 0; i-- {
		node := tree.Node(path[i])
		if node == nil || node.scrollContainer == nil {
			continue
		}
		if node.scrollContainer.applyScroll(mapping, w, app) {
			return true
		}
	}
	return false
}

// defaultTabNavigation is the Tab default action
// (DefaultTabEventHandler, keyboard_event_manager.cc:574-616): Tab and
// Shift+Tab move focus to the next and previous tab stop, unless Ctrl,
// Alt or the platform modifier is held (Chromium: ctrl/meta never
// advance, alt is a system key off-Mac). It reports whether focus
// moved.
func (w *Window) defaultTabNavigation(e *KeyDownEvent) bool {
	if e.Keystroke.Key != "tab" {
		return false
	}
	m := e.Keystroke.Modifiers
	if m.Control || m.Alt || m.Platform {
		return false
	}
	if m.Shift {
		w.FocusPrev()
	} else {
		w.FocusNext()
	}
	return true
}

// keyScrollDirection is one axis direction of a keyboard scroll.
type keyScrollDirection uint8

const (
	keyScrollUp keyScrollDirection = iota
	keyScrollDown
	keyScrollLeft
	keyScrollRight
)

// keyScrollGranularity is one keyboard scroll's step size
// (ui::ScrollGranularity, bounded to the keyboard's three).
type keyScrollGranularity uint8

const (
	keyScrollByLine keyScrollGranularity = iota
	keyScrollByPage
	keyScrollByDocument
)

// keyScrollMapping is one key's scroll direction and granularity.
type keyScrollMapping struct {
	direction   keyScrollDirection
	granularity keyScrollGranularity
}

// mapKeyForScroll is the key→direction, granularity mapping of
// MapKeyCodeForScroll (keyboard_event_manager.cc:67-176) plus the
// Space page scroll of DefaultSpaceEventHandler (:492-523): Shift or
// the platform modifier never scroll (Shift selects, Meta is a browser
// shortcut); Ctrl keeps only Home/End; Alt keys are system keys on
// Windows; arrows scroll a line; Home/End jump to the document start
// or end; PageUp/PageDown and Space scroll a page, Space with Shift
// paging backward.
func mapKeyForScroll(key string, modifiers Modifiers) (keyScrollMapping, bool) {
	if modifiers.Shift && key != "space" {
		return keyScrollMapping{}, false
	}
	if modifiers.Platform {
		return keyScrollMapping{}, false
	}
	if modifiers.Alt {
		return keyScrollMapping{}, false
	}
	if modifiers.Control && key != "home" && key != "end" {
		return keyScrollMapping{}, false
	}
	switch key {
	case "arrowup", "up":
		return keyScrollMapping{direction: keyScrollUp, granularity: keyScrollByLine}, true
	case "arrowdown", "down":
		return keyScrollMapping{direction: keyScrollDown, granularity: keyScrollByLine}, true
	case "arrowleft", "left":
		return keyScrollMapping{direction: keyScrollLeft, granularity: keyScrollByLine}, true
	case "arrowright", "right":
		return keyScrollMapping{direction: keyScrollRight, granularity: keyScrollByLine}, true
	case "home":
		return keyScrollMapping{direction: keyScrollUp, granularity: keyScrollByDocument}, true
	case "end":
		return keyScrollMapping{direction: keyScrollDown, granularity: keyScrollByDocument}, true
	case "pageup":
		return keyScrollMapping{direction: keyScrollUp, granularity: keyScrollByPage}, true
	case "pagedown":
		return keyScrollMapping{direction: keyScrollDown, granularity: keyScrollByPage}, true
	case "space":
		if modifiers.Shift {
			return keyScrollMapping{direction: keyScrollUp, granularity: keyScrollByPage}, true
		}
		return keyScrollMapping{direction: keyScrollDown, granularity: keyScrollByPage}, true
	}
	return keyScrollMapping{}, false
}

// keyScrollLineStep is the line step of the keyboard scroll in logical
// (CSS) pixels: cc/input/scroll_utils.h:20 kPixelsPerLineStep = 40,
// independent of the device scale (measured 40 CSS px at the harness's
// 2.0 ratio; ScrollableArea::PixelsPerLineStep's WindowToViewport-
// Scalar is the identity in CSS pixels here).
const keyScrollLineStep float32 = 40

// keyScrollPageOverlap is cc/input/scroll_utils.cc:53's
// length - kMaxOverlapBetweenPages (kMaxOverlapBetweenPages = 40,
// scroll_utils.h:24).
const keyScrollPageOverlap float32 = 40

// keyScrollPageFraction is cc/input/scroll_utils.cc:52's
// kMinFractionToStepWhenPaging = 0.875 (scroll_utils.h:22).
const keyScrollPageFraction float32 = 0.875

// keyScrollPageStep is CalculatePageStep (cc/input/scroll_utils.cc:
// 50-55): max(length*0.875, length-40), never below 1.
func keyScrollPageStep(length float32) float32 {
	step := length * keyScrollPageFraction
	if length-keyScrollPageOverlap > step {
		step = length - keyScrollPageOverlap
	}
	if step < 1 {
		step = 1
	}
	return step
}

// keyScrollContainer is one scroll container's registration for the
// keyboard-scroll default action (browser-core 09): the element's
// shared scroll-offset cell with this frame's clamp bounds and
// overflow axes, registered on the element's dispatch node during
// prepaint (registerKeyScrollContainer) so the default action can find
// the scroller containing focus. The offset cell is the same one the
// wheel listener and the ScrollHandle write.
type keyScrollContainer struct {
	// offset is the shared scroll-offset cell (negative y scrolls down).
	offset *Point
	// maxOffset is the frame's clamped scroll maximum per axis.
	maxOffset Point
	// visible is the visible (viewport) size of the scroller, for the
	// page step.
	visible Size
	// overflowX and overflowY are the element's computed overflow axes;
	// only a scrolling axis takes a delta (CanScroll's axis gate,
	// scroll_manager.cc:176-191).
	overflowX, overflowY Overflow
	// notify refreshes the view that rendered the scroller.
	notify func(w *Window, app *App)
}

// applyScroll applies one keyboard scroll step to this container and
// reports whether the visible offset changed (BubblingScroll chains to
// the ancestor when it does not). Line steps move 40 px
// (keyScrollLineStep); page steps move keyScrollPageStep of the
// visible length; document steps jump to the axis start or end. The
// offset clamps into [-maxOffset, 0] per axis, matching the next
// prepaint's clamp.
func (c *keyScrollContainer) applyScroll(mapping keyScrollMapping, w *Window, app *App) bool {
	if c == nil || c.offset == nil {
		return false
	}
	vertical := mapping.direction == keyScrollUp || mapping.direction == keyScrollDown
	if vertical && c.overflowY != OverflowScroll {
		return false
	}
	if !vertical && c.overflowX != OverflowScroll {
		return false
	}
	old := *c.offset
	next := *c.offset
	switch mapping.granularity {
	case keyScrollByLine:
		step := keyScrollLineStep
		if vertical {
			next.Y += scrollSign(mapping.direction) * step
		} else {
			next.X += scrollSign(mapping.direction) * step
		}
	case keyScrollByPage:
		length := c.visible.Width
		if vertical {
			length = c.visible.Height
		}
		step := keyScrollPageStep(length)
		if vertical {
			next.Y += scrollSign(mapping.direction) * step
		} else {
			next.X += scrollSign(mapping.direction) * step
		}
	case keyScrollByDocument:
		// Home/End are vertical in the Chromium mapping (MapKeyCodeFor-
		// Scroll: VKEY_HOME → kScrollUpIgnoringWritingMode by document).
		if mapping.direction == keyScrollUp {
			next.Y = 0
		} else {
			next.Y = -c.maxOffset.Y
		}
	}
	if vertical {
		next.X = old.X
		next.Y = clampF32(next.Y, -c.maxOffset.Y, 0)
	} else {
		next.Y = old.Y
		next.X = clampF32(next.X, -c.maxOffset.X, 0)
	}
	if next == old {
		return false
	}
	*c.offset = next
	if c.notify != nil {
		c.notify(w, app)
	}
	return true
}

// scrollSign maps a direction onto the offset sign: scrolling down or
// right decreases the offset (the port's offsets are negative in the
// down/right direction).
func scrollSign(direction keyScrollDirection) float32 {
	if direction == keyScrollDown || direction == keyScrollRight {
		return -1
	}
	return 1
}

// registerKeyScrollContainer registers the scrolling element's scroll
// container on the active dispatch node (browser-core 09): called from
// the div's prepaint after the scroll clamp, so the keyboard-scroll
// default action can walk the focused node's ancestors and find this
// scroller. scroll is the element's resolved scroll slice (nil when it
// does not scroll); style is the frame's computed style (the overflow
// axes).
func registerKeyScrollContainer(w *Window, scroll *elementScroll, style *Style) {
	if scroll == nil || scroll.offset == nil {
		return
	}
	tree := currentDispatchTree(w)
	tree.SetKeyScrollContainer(keyScrollContainer{
		offset:    scroll.offset,
		maxOffset: scroll.maxOffset,
		visible:   scroll.visible,
		overflowX: style.OverflowX,
		overflowY: style.OverflowY,
		notify:    currentViewNotifier(w),
	})
}

// dispatchKeyEventBrowserOrder is the per-window browser order
// (browser-core 09, spec D5: a per-window flag gives the full browser
// order): the key listeners run first, then the bindings and the core
// default actions in the default-action position, all skipped when a
// listener prevented the default. Chromium's dispatch clears the
// stop-propagation flags before the default phase (event_dispatcher.
// cc:397-401): a listener's StopPropagation stops other listeners, not
// the default actions.
func (w *Window) dispatchKeyEventBrowserOrder(event any, nodeID DispatchNodeID, dispatchPath []DispatchNodeID, matchResult dispatchResult, skipBindings bool, app *App) {
	// 1. The key listeners (capture then bubble), then the modifiers-
	// changed listeners. The keystroke observers wait for the binding
	// decision below, so they fire once and carry the action when a
	// binding dispatched (window.rs dispatch_key_event's observer call
	// with Some(binding.action)); the pin's listener-stopped skip
	// (finish_dispatch_key_event's early returns) is preserved.
	w.dispatchKeyUpDownEvent(event, dispatchPath, app)
	listenersStopped := !app.propagateEvent
	if app.propagateEvent {
		w.dispatchModifiersChangedEvent(event, dispatchPath, app)
	}

	// 2. The default-action phase. Stop-propagation is not a default-
	// action suppression; the prevent-default flag is.
	app.propagateEvent = true
	if w.DefaultPrevented() {
		app.propagateEvent = false
		if !listenersStopped {
			w.dispatchKeystrokeObservers(event, BoxedAction{}, matchResult.contextStack, app)
		}
		return
	}

	// 3. Bindings dispatch in the default-action position: after the
	// listeners, skipped by preventDefault, and a binding whose action
	// stopped propagation handled the key (the core defaults then do
	// not run — the app's binding overrides them, as in the GPUI
	// shape).
	handled := false
	var action BoxedAction
	if !skipBindings {
		for _, binding := range matchResult.bindings {
			w.dispatchActionOnNode(nodeID, binding.action, app)
			if !app.propagateEvent {
				action = binding.action
				handled = true
				break
			}
		}
	}
	acted := false
	if !handled {
		acted = w.runKeyDefaultActions(event, app)
	}
	// The platform's propagation answer: a prevented, handled or acted
	// key is consumed (Chromium's ToWebInputEventResult of a
	// default-handled event).
	app.propagateEvent = !(handled || acted)
	if !listenersStopped {
		w.dispatchKeystrokeObservers(event, action, matchResult.contextStack, app)
	}
	w.pendingInputChanged(app)
}
