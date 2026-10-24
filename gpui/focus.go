package gpui

// This file ports the focus runtime of the pinned CE reference
// 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a:
//
//   - crates/gpui/src/window.rs — FocusId/FocusHandle/FocusRef (the
//     per-app focus slotmap with reference counting, tab_index and
//     tab_stop), the focused handle of a window (focus, focus_enabled,
//     focus_generation), focus/blur/disable_focus/focus_next/
//     focus_prev, focus_contains through the rendered dispatch tree,
//     the focus-lost notification when the focused handle is released,
//     and the weak handle whose upgrade fails at logical zero;
//   - crates/gpui/src/tab_stop.rs — TabStopMap (path-ordered tab stops
//     with insertion history, group begin/end, next/prev with wrap and
//     non-tab-stop skipping).
//
// Go adaptations (recorded where they alter behavior):
//
//   - Reference counting is replaced by logical release: Go ownership
//     is garbage collected, so FocusHandle.Release performs the
//     idempotent logical release the runtime ownership contract
//     specifies ("release is idempotent", "weak upgrade fails
//     irrevocably at logical zero"). A released handle never refocuses,
//     never contains, and the window's Focused reports none.
//
//   - The reference keeps one app-global focus slotmap; the port keeps
//     the per-window handle registry the window slice landed (handles
//     are window-scoped identities), with the same observable release
//     semantics.

import (
	"fmt"
	"sort"
)

// ---------------------------------------------------------------------------
// Per-window focus runtime
// ---------------------------------------------------------------------------

// focusRef is the window's record for one allocated focus identity
// (window.rs FocusRef without the reference count: see the file header
// for the release adaptation).
type focusRef struct {
	// tabIndex is the tab order index of the handle's element.
	tabIndex int64
	// tabStop reports whether the handle's element is a tab stop.
	tabStop bool
	// released is the logical-release flag (idempotent).
	released bool
}

// windowFocusState is a window's focus runtime: handle allocation,
// focus identity, focus generation and the pending-input state the
// keyboard dispatch uses. All fields are foreground-thread only.
type windowFocusState struct {
	handles map[uint64]*focusRef
	seq     uint64

	// focused is the focused handle identity, or 0 (none).
	focused uint64
	// focusEnabled gates focus changes (window.rs focus_enabled; a
	// disabled window refuses focus and blur).
	focusEnabled bool
	// focusGeneration counts focus changes (window.rs
	// focus_generation, wrapping).
	focusGeneration uint64

	// pendingInput is the chord state (window.rs PendingInput), or nil.
	pendingInput *pendingInput
	// pendingModifier tracks the lone-modifier keystroke synthesis
	// (window.rs pending_modifier).
	pendingModifier modifierTracking

	// keyOrder is the window's key dispatch order (browser-core 09,
	// spec D5; key_default_action.go): the zero value is the GPUI shape
	// (bindings before listeners), ListenersBeforeBindings the browser
	// order.
	keyOrder KeyDispatchOrder
	// focusNavStart is the sequential focus navigation starting point
	// (browser-core ticket 14): the tree-order position a Tab search
	// continues from when nothing is focused, recorded by the focus
	// fixup when the focused element leaves the frame (Chromium's
	// Document::SequentialFocusNavigationStartingPoint and the removal
	// path's Range walk, document.cc:6308-6319 and :6382-6395). Any
	// focus move or blur clears it (the focused element takes
	// precedence; the pass-out nulls it, focus_controller.cc:2220).
	focusNavStart focusNavStartingPoint

	// rendered is the last completed frame's dispatch tree and tab
	// stops (window.rs rendered_frame.dispatch_tree/tab_stops).
	rendered *renderedFrame
	// next is the frame under construction, or nil outside a draw.
	next *renderedFrame

	// inputHandler is the active input handler of the last completed
	// frame (the platform input handler seam), or nil.
	inputHandler InputHandler
	// frameInputHandlers are the focused registrations of the frame
	// under construction (window.rs next_frame.input_handlers).
	frameInputHandlers []InputHandler
	// renderedInputHandlers are the last completed frame's registrations
	// (window.rs rendered_frame.input_handlers): the list a cached view's
	// reused paint range indexes into (view_cache.go reusePaint).
	renderedInputHandlers []InputHandler

	// focusLostListeners are notified when the focused handle is
	// released (window.rs focus_lost_listeners).
	focusLostListeners []*focusLostSubscription
	// focusLostPath is the previous focus path during focus-lost
	// dispatch (window.rs focus_lost_path).
	focusLostPath []uint64

	// pendingInputObservers observe pending-input state changes
	// (window.rs pending_input_observers).
	pendingInputObservers []*pendingInputSubscription

	// focusListeners are the window focus-change listeners registered by
	// Context.OnFocus/OnFocusIn/OnBlur/OnFocusOut (window.rs:1290
	// focus_listeners, filled by new_focus_listener at 2090-2095),
	// delivered at frame finish (helpers_frame.go).
	focusListeners *subscriberSet
	// activationObservers are the Context.ObserveWindowActivation
	// registrations (window.rs:1317 activation_observers).
	activationObservers *subscriberSet
	// inactive records the last reported window activation (window.rs
	// Window.active, inverted so the zero value is an active window: test
	// windows have no platform activation source).
	inactive bool
}

// renderedFrame is one completed frame's interaction state: the
// dispatch tree and the tab-stop map (window.rs RenderedFrame's
// dispatch members, bounded to this slice), plus ticket24's mouse
// interaction members — the frame-scoped mouse listeners (window.rs
// Frame::mouse_listeners) and hitboxes (Frame::hitboxes).
type renderedFrame struct {
	dispatchTree   *DispatchTree
	tabStops       *TabStopMap
	mouseListeners []mouseListener
	hitboxes       []*hitboxRecord
	// clickListeners are the frame's click registrations (browser-core
	// ticket 12, spec D12): every id'd element with OnClick/OnAuxClick
	// listeners registers them under its element path key — the same
	// identity the hitboxes' ownerPath carries — in paint order
	// (parents first). The mouse up's click dispatch (mouseevent.go
	// dispatchMouseClick) resolves the target chain by those keys and
	// fires the listeners target-first, like a bubbling DOM click.
	clickListeners []clickListenerRegistration
	// contextMenuListeners are the frame's contextmenu registrations
	// (browser-core ticket 17, contextmenu.go): every id'd element with
	// OnContextMenu listeners registers them under its element path key
	// at paint — the mouse trigger's dispatch (the right-button up)
	// resolves the release chain by those keys.
	contextMenuListeners []contextMenuRegistration
	// overlayRegistrations are the frame's top-layer entries
	// (browser-core ticket 21, toplayer.go): every open TopLayer
	// element registers its handle, kind, invoker name and content
	// path at paint — the frame-end reconciliation
	// (finishOverlayFrame) resolves the invoker paths and updates the
	// layer, and the click dispatch's light dismiss walks the layer.
	overlayRegistrations []overlayRegistration
	// modalFocusEntries are the frame's focusable elements while a
	// modal dialog is open (browser-core ticket 23, modal.go): each
	// focusable div's path key, focus handle and autofocus marking,
	// registered at prepaint and read by the dialog focusing steps at
	// the modal entry's first registration (the entries outside the
	// modal's content are inert and never register).
	modalFocusEntries []modalFocusEntry
	// tooltipSeen are the id-path keys of the tooltip-carrying elements
	// that drew this frame (browser-core ticket 24, tooltip.go): the
	// frame-end reconciliation (finishTooltipFrame) drops the tooltip
	// records whose element stopped drawing.
	tooltipSeen []string
	// hitboxStack is the transient stack of hitboxes whose subtrees are
	// prepainting (browser-core ticket 11): each hitbox records the top
	// of this stack as its parent, which is how the capture chain
	// resolves an element's hitbox ancestors. It is empty outside
	// prepaint (withHitboxSubtree pushes and pops around the subtree).
	hitboxStack []*hitboxRecord
	// windowControlHitboxes are the frame's window control areas in
	// insertion order (window.rs:1109 Frame::window_control_hitboxes).
	windowControlHitboxes []windowControlHitbox
	// focus is the focus identity the frame was finished with, or 0
	// (window.rs:1101 Frame.focus; the focus path compares through it).
	focus uint64
	// windowActive is the window activation the frame was finished with
	// (window.rs Frame.window_active, recorded at 3198).
	windowActive bool
}

// focusPathOf returns the frame's focus path (window.rs:1219-1223
// Frame::focus_path): empty without a focus or a frame.
func (f *renderedFrame) focusPathOf() []uint64 {
	if f == nil || f.focus == 0 || f.dispatchTree == nil {
		return nil
	}
	return f.dispatchTree.focusPath(f.focus)
}

// clickListenerRegistration is one element's click and aux-click
// listeners on the frame (browser-core ticket 12): path is the owning
// element's GlobalElementID key — the identity the hitboxes'
// ownerPath and the click chain resolve by. Registered during paint
// (interact.go paintMouseListeners), consumed by the up's click
// dispatch (mouseevent.go dispatchMouseClick).
type clickListenerRegistration struct {
	// path is the owning element's global id key (the hitbox's
	// ownerPath).
	path string
	// click are the left-button click listeners.
	click []func(*ClickEvent, *Window, *App)
	// auxClick are the non-primary-button listeners.
	auxClick []func(*ClickEvent, *Window, *App)
}

// focusNavStartingPoint is the sequential focus navigation starting
// point's port (browser-core ticket 14): the stops adjacent to where
// the focused element sat in tree order when the focus fixup blurred
// it. A forward search starts AT prev (its successor is the answer);
// a backward search starts AT next (its predecessor is the answer) —
// Chromium's walk of the removed position (document.cc:6382-6395: for
// kForward the PREVIOUS element becomes the start; the mirror for
// kBackward), so Tab after a removal continues from the removed
// element's position instead of restarting the cycle.
type focusNavStartingPoint struct {
	// prev is the nearest stop before the removed focused element in
	// tree order (nil at the tree start).
	prev *uint64
	// next is the nearest stop after it (nil at the tree end).
	next *uint64
}

// focusState returns (creating when absent) the window's focus
// runtime. Foreground thread only.
func focusState(w *Window) *windowFocusState {
	if w == nil {
		panic("gpui: focus runtime requires a live window")
	}
	if w.focus == nil {
		w.focus = &windowFocusState{
			handles:      make(map[uint64]*focusRef),
			focusEnabled: true,
		}
	}
	return w.focus
}

// focusRefOf returns the window's focus record for a handle identity,
// or nil when absent or released.
func (w *Window) focusRefOf(id uint64) *focusRef {
	if isAppFocusID(id) {
		// App-level handles (App.FocusHandle) live in the app registry and
		// are valid in every window of their application.
		return appFocusRefOf(w.app, id)
	}
	fs := focusState(w)
	ref, ok := fs.handles[id]
	if !ok || ref.released {
		return nil
	}
	return ref
}

// ---------------------------------------------------------------------------
// FocusHandle (window.rs FocusHandle)
// ---------------------------------------------------------------------------

// FocusHandle tracks and manipulates the focused element in a window.
// Handles are value identities into their window's focus registry (the
// reference holds the shared focus map on the handle; the port holds the
// owning window pointer, which stays valid as a logical identity after
// close); Release performs the logical release.
type FocusHandle struct {
	id     uint64
	window *Window
	// app is the owning application of an app-level handle
	// (App.FocusHandle, app.rs:2796); nil for window-allocated handles.
	app      *App
	tabIndex int64
	tabStop  bool
}

// ID returns the focus identity within its window.
func (h FocusHandle) ID() uint64 { return h.id }

// WindowID returns the owning window identity.
func (h FocusHandle) WindowID() WindowID { return h.windowID() }

func (h FocusHandle) windowID() WindowID {
	if h.window == nil {
		return 0
	}
	return h.window.id
}

// TabIndex sets the tab order index of this handle's element (window.rs
// FocusHandle::tab_index): positive indices follow 0 in ascending
// order; negative indices precede it.
func (h FocusHandle) TabIndex(index int64) FocusHandle {
	if ref := h.windowRef(); ref != nil {
		ref.tabIndex = index
	}
	out := h
	out.tabIndex = index
	return out
}

// TabStop sets whether this handle's element is a tab stop (window.rs
// FocusHandle::tab_stop): non-tab-stop handles are skipped by
// focus_next/focus_prev.
func (h FocusHandle) TabStop(tabStop bool) FocusHandle {
	if ref := h.windowRef(); ref != nil {
		ref.tabStop = tabStop
	}
	out := h
	out.tabStop = tabStop
	return out
}

// windowRef returns the window's live focus record for this handle, or
// nil when absent or released.
func (h FocusHandle) windowRef() *focusRef {
	if isAppFocusID(h.id) {
		if app := h.ownerApp(); app != nil {
			return appFocusRefOf(app, h.id)
		}
		return nil
	}
	if h.window == nil {
		return nil
	}
	return h.window.focusRefOf(h.id)
}

// ownerApp resolves the application of a handle (app-level handles carry
// it; window handles reach it through their window).
func (h FocusHandle) ownerApp() *App {
	if h.app != nil {
		return h.app
	}
	if h.window != nil {
		return h.window.app
	}
	return nil
}

// Release performs the logical release of this handle (the ownership
// contract's idempotent release). A released handle stops matching
// focus queries, cannot be focused again, and a focused handle's
// release clears the window focus and dispatches focus-lost listeners
// (window.rs notify_focus_lost).
func (h FocusHandle) Release() {
	if isAppFocusID(h.id) {
		// App-level handle: release the shared record, then clear the
		// focus of every window of the application that holds it.
		app := h.ownerApp()
		if app == nil {
			return
		}
		ref := appFocusRefOf(app, h.id)
		if ref == nil {
			return // idempotent
		}
		ref.released = true
		for _, id := range app.Windows() {
			if w, ok := app.Window(id); ok {
				w.releaseFocused(h.id)
			}
		}
		return
	}
	w := h.window
	if w == nil {
		return
	}
	fs := focusState(w)
	ref, exists := fs.handles[h.id]
	if !exists || ref.released {
		return // idempotent
	}
	ref.released = true
	w.releaseFocused(h.id)
}

// releaseFocused clears this window's focus when it holds the released
// identity and dispatches the focus-lost listeners (the release half of
// FocusHandle.Release).
func (w *Window) releaseFocused(id uint64) {
	fs := focusState(w)
	h := FocusHandle{id: id, window: w}
	if fs.focused == h.id {
		fs.focused = 0
		fs.focusGeneration++
		// Record the lost path from the rendered frame (window.rs
		// notify_focus_lost: focus_lost_path = previous focus path).
		fs.focusLostPath = nil
		if fs.rendered != nil {
			fs.focusLostPath = fs.rendered.dispatchTree.focusPath(h.id)
		}
		fs.pendingInput = nil
		listeners := append([]*focusLostSubscription{}, fs.focusLostListeners...)
		for _, sub := range listeners {
			if sub.callback != nil {
				sub.callback(w)
			}
		}
		fs.focusLostPath = nil
	}
}

// Focus moves focus to this handle's element (FocusHandle::focus).
func (h FocusHandle) Focus(w *Window) {
	if w == nil {
		panic("gpui: FocusHandle.Focus requires a live window")
	}
	w.Focus(h)
}

// IsFocused reports whether this handle's element is focused.
func (h FocusHandle) IsFocused(w *Window) bool {
	if w == nil {
		return false
	}
	fs := focusState(w)
	return fs.focused == h.id && w.focusRefOf(h.id) != nil
}

// ContainsFocused reports whether this handle contains the focused
// element or is itself focused (window.rs contains_focused).
func (h FocusHandle) ContainsFocused(w *Window) bool {
	if w == nil {
		return false
	}
	focused := w.Focused()
	if !focused.Ok {
		return false
	}
	return h.Contains(focused.Handle, w)
}

// WithinFocused reports whether this handle is contained within the
// focused element or is itself focused (window.rs within_focused).
func (h FocusHandle) WithinFocused(w *Window) bool {
	if w == nil {
		return false
	}
	focused := w.Focused()
	if !focused.Ok {
		return false
	}
	return focused.Handle.Contains(h, w)
}

// Contains reports whether this handle contains the other handle in
// the most recently rendered frame (window.rs contains, through the
// dispatch tree's focus_contains).
func (h FocusHandle) Contains(other FocusHandle, w *Window) bool {
	if w == nil {
		return false
	}
	fs := focusState(w)
	if fs.rendered == nil {
		return h.id == other.id
	}
	return fs.rendered.dispatchTree.focusContains(h.id, other.id)
}

// DispatchAction dispatches an action on the element that rendered
// this focus handle (FocusHandle::dispatch_action).
func (h FocusHandle) DispatchAction(action BoxedAction, w *Window, cx AppContext) {
	if w == nil {
		panic("gpui: DispatchAction requires a live window")
	}
	app := appFrom(cx)
	fs := focusState(w)
	nodeID := dispatchNodeIDOfRoot(fs)
	if fs.rendered != nil {
		if id, ok := fs.rendered.dispatchTree.focusableNodeID(h.id); ok {
			nodeID = id
		}
	}
	w.dispatchActionOnNode(nodeID, action, app)
}

// Downgrade converts this handle into a weak variant which does not
// keep the focus registry entry live (FocusHandle::downgrade).
func (h FocusHandle) Downgrade() WeakFocusHandle {
	return WeakFocusHandle{handle: h}
}

// WeakFocusHandle is a focus handle reference whose upgrade fails
// once the strong handle has been released (WeakFocusHandle).
type WeakFocusHandle struct {
	handle FocusHandle
}

// Upgrade returns the strong handle, or false when it was released.
func (w WeakFocusHandle) Upgrade() (FocusHandle, bool) {
	if w.handle.windowRef() == nil {
		return FocusHandle{}, false
	}
	return w.handle, true
}

// ID returns the focus identity within its window.
func (w WeakFocusHandle) ID() uint64 { return w.handle.id }

// ---------------------------------------------------------------------------
// Window focus API (window.rs focus methods)
// ---------------------------------------------------------------------------

// FocusedResult is the focused-handle query result.
type FocusedResult struct {
	// Handle is the focused handle.
	Handle FocusHandle
	// Ok reports whether any handle is focused.
	Ok bool
}

// Focus moves focus to the given handle (Window::focus): no-op when
// focus is disabled or the handle already holds focus; clears pending
// keystrokes and bumps the focus generation. App code calls this — a
// script focus in the DOM's terms, which keeps the window's focus
// origin and keyboard state (browser-core ticket 12).
func (w *Window) Focus(handle FocusHandle) {
	w.focusWithOrigin(handle, focusOriginUnset)
}

// focusWithOrigin moves focus recording the focus origin
// (browser-core ticket 12, spec D12): mouse (the mouse-down focus
// transfer), keyboard (the Tab default action) or unset (app code —
// Chromium's document.cc:6068-6070 ignores kScript/kNone when tracking
// LastFocusType, so a script focus keeps the document's origin). A
// mouse-type focus also clears the keyboard-event flag
// (element.cc:8867-8868), which is what keeps a click's focus from
// showing the focus-visible ring until the next unmodified key.
func (w *Window) focusWithOrigin(handle FocusHandle, origin focusOriginKind) {
	if w == nil {
		panic("gpui: Focus requires a live window")
	}
	fs := focusState(w)
	if !fs.focusEnabled || fs.focused == handle.id {
		return
	}
	if w.focusRefOf(handle.id) == nil {
		// Released (or foreign) handles are not focusable (the weak
		// upgrade failure of the reference).
		return
	}
	fs.focused = handle.id
	fs.focusGeneration++
	fs.focusNavStart = focusNavStartingPoint{}
	w.clearPendingKeystrokes()

	// The origin bookkeeping (Chromium's LastFocusType and
	// had_keyboard_event_, selector_checker.cc:3888-3901 reads both).
	is := inputState(w)
	switch origin {
	case focusOriginMouse:
		is.focusOrigin = focusOriginMouse
		is.hadKeyboardEvent = false
	case focusOriginKeyboard:
		is.focusOrigin = focusOriginKeyboard
	}

	// The reference refreshes the window (window.rs Window::focus →
	// refresh): the frame loop redraws it.
	w.requestRefresh()
}

// Focused returns the handle focused in this window, when any
// (Window::focused: a released handle reports none).
func (w *Window) Focused() FocusedResult {
	if w == nil {
		return FocusedResult{}
	}
	fs := focusState(w)
	if fs.focused == 0 || w.focusRefOf(fs.focused) == nil {
		return FocusedResult{}
	}
	return FocusedResult{Handle: w.handleFor(fs.focused), Ok: true}
}

// Blur removes focus from all elements in this window (Window::blur).
func (w *Window) Blur() {
	if w == nil {
		return
	}
	fs := focusState(w)
	w.clearPendingKeystrokes()
	if !fs.focusEnabled {
		return
	}
	if fs.focused != 0 {
		fs.focusGeneration++
	}
	fs.focused = 0
	w.requestRefresh()
}

// blurWithOrigin removes focus from all elements in this window
// recording the focus origin (browser-core ticket 14, spec D4's "a
// press on empty space blurs"): Chromium's mouse-focus default moves
// focus to the document's body when the press chain has no focusable
// element — MouseEventManager::HandleMouseFocus's SetFocusedElement(
// nullptr, …FocusType::kMouse) (mouse_event_manager.cc:615-621: "We
// call setFocusedElement even with !element in order to blur current
// focus element when a link is clicked") — a mouse-type focus change
// to nothing, which clears the keyboard-event flag like focusWithOrigin
// (element.cc:8867-8868). The port's body equivalent is no handle.
func (w *Window) blurWithOrigin(origin focusOriginKind) {
	if w == nil {
		return
	}
	fs := focusState(w)
	if !fs.focusEnabled || fs.focused == 0 {
		return
	}
	w.clearPendingKeystrokes()
	fs.focused = 0
	fs.focusGeneration++
	fs.focusNavStart = focusNavStartingPoint{}
	is := inputState(w)
	switch origin {
	case focusOriginMouse:
		is.focusOrigin = focusOriginMouse
		is.hadKeyboardEvent = false
	case focusOriginKeyboard:
		is.focusOrigin = focusOriginKeyboard
	}
	w.requestRefresh()
}

// DisableFocus blurs the window and refuses further focus
// (Window::disable_focus).
func (w *Window) DisableFocus() {
	if w == nil {
		return
	}
	w.Blur()
	focusState(w).focusEnabled = false
}

// FocusNext moves focus to the next tab stop (Window::focus_next;
// browser-core ticket 14 gives it the browser's boundary: past the
// last stop the sequential search reports nothing and focus passes
// out of the page — Chromium's CanTakeFocus/TakeFocus arm,
// focus_controller.cc:2216-2231, where Chrome takes the focus at the
// document scope's end — so the port blurs, the body equivalent; a
// later Tab restarts from the first stop, and Shift+Tab returns to
// the last, both sides' recorded answers).
func (w *Window) FocusNext() {
	if w == nil {
		return
	}
	fs := focusState(w)
	if !fs.focusEnabled {
		return
	}
	if fs.rendered == nil {
		return
	}
	focused := w.focusSearchStart(false)
	if handle, ok := fs.rendered.tabStops.Next(focused, w); ok {
		// The Tab default action's focus is a keyboard focus
		// (browser-core ticket 12): the moved-to element shows the
		// focus-visible styles (Chromium's FocusType::kForward is not
		// kMouse, so !last_focus_from_mouse holds).
		w.focusWithOrigin(handle, focusOriginKeyboard)
	} else {
		// The pass-out (browser-core ticket 14): past the last stop
		// (or with no stop after an excluded focused element), focus
		// moves to the body — no handle.
		w.blurWithOrigin(focusOriginKeyboard)
	}
}

// focusSearchStart resolves the sequential search's start: the focused
// handle, or — with nothing focused — the sequential focus navigation
// starting point the focus fixup recorded (browser-core ticket 14;
// Chromium's Document::SequentialFocusNavigationStartingPoint,
// document.cc:6333-6395: the search continues from where the removed
// focused element sat). Forward searches start at the point's previous
// stop, backward searches at its next stop (the removal path's walk).
func (w *Window) focusSearchStart(backward bool) *uint64 {
	fs := focusState(w)
	if fs.focused != 0 {
		id := fs.focused
		return &id
	}
	if backward {
		return fs.focusNavStart.next
	}
	return fs.focusNavStart.prev
}

// FocusPrev moves focus to the previous tab stop (Window::focus_prev;
// browser-core ticket 14's boundary like FocusNext: before the first
// stop, the backward pass-out — the blur, the body equivalent).
func (w *Window) FocusPrev() {
	if w == nil {
		return
	}
	fs := focusState(w)
	if !fs.focusEnabled {
		return
	}
	if fs.rendered == nil {
		return
	}
	focused := w.focusSearchStart(true)
	if handle, ok := fs.rendered.tabStops.Prev(focused, w); ok {
		// Shift+Tab is a keyboard focus like Tab (focusOriginKeyboard).
		w.focusWithOrigin(handle, focusOriginKeyboard)
	} else {
		// The backward pass-out (browser-core ticket 14): before the
		// first stop, focus moves to the body — no handle.
		w.blurWithOrigin(focusOriginKeyboard)
	}
}

// FocusGeneration returns the window's focus generation counter
// (diagnostics; window.rs focus_generation).
func (w *Window) FocusGeneration() uint64 {
	if w == nil {
		return 0
	}
	return focusState(w).focusGeneration
}

// fixupFocusedElement is the end-of-frame focus fixup (browser-core
// ticket 14, spec D4's "focus moves off an element that disappears at
// the end of the frame"): when the focused handle is no longer in the
// completed frame's dispatch tree — the element was removed — or its
// node registered while suppressed (hidden or inert — unfocusable),
// focus moves to the body equivalent (no handle). It runs after the
// completed frame swapped in (endDispatchFrame) and before the focus
// phase, so the phase's path diff delivers the blur listeners and the
// frame's recorded focus follows the fixup. It reports whether focus
// moved.
//
// The Chromium reference: removal is the synchronous node-remove
// focus fixup (Document::RemoveFocusedElementOfSubtree, document.cc:
// 5809-5866 — "node remove focus fixup", https://html.spec.whatwg.org/C/
// #node-remove-focus-fixup — ClearFocusedElement during the removal);
// becoming unfocusable (hidden, display:none, inert) is the deferred
// check Document::ClearFocusedElementIfNeeded (document.cc:3261-3277:
// a one-shot timer when focused_element_->IsFocusable() turns false,
// blurring at ClearFocusedElementTimerFired), driven from the layout
// update (local_frame_view.cc:3694). The port's frame completion is
// the equivalent moment — the element's disappearance is visible
// exactly when the frame that dropped it completes — and the next
// input event sees no focus on both.
func (w *Window) fixupFocusedElement() bool {
	if w == nil {
		return false
	}
	fs := focusState(w)
	if fs.focused == 0 || !fs.focusEnabled || fs.next == nil {
		return false
	}
	// The completed frame's tree (finishFrame calls this before the
	// swap, so the old frame's tab stops are still reachable for the
	// starting point below).
	tree := fs.next.dispatchTree
	if nodeID, ok := tree.focusableNodeID(fs.focused); ok {
		if nodeID.Index < len(tree.nodes) && !tree.nodes[nodeID.Index].focusSuppressed {
			return false
		}
	}
	// The focused element disappeared or became unfocusable: blur to
	// the body equivalent, and record where it sat — the sequential
	// focus navigation starting point (the removal path,
	// document.cc:6308-6319/:6382-6395), so the next Tab continues
	// from the removed position instead of restarting the cycle.
	// The fixup answers REMOVAL, not absence: only a handle the
	// PREVIOUS frame's tree carried — the element was rendered and left
	// it (or its node turned suppressed) — blurs. A handle no frame ever
	// carried (the port's state-only handles: app-level and entity-state
	// targets the questionnaire harness and hidden inputs focus) keeps
	// the pin's behavior — nothing blurs it — since Chromium's focus
	// lives on DOM elements and the pin's handles may outlive any
	// element (document.cc:5809-5866 reacts to the removal of a node
	// that was in the tree, never to one that was not).
	if fs.rendered == nil || fs.rendered.dispatchTree == nil {
		return false
	}
	if _, wasRendered := fs.rendered.dispatchTree.focusableNodeID(fs.focused); !wasRendered {
		return false
	}
	fs.recordFocusNavStart()
	fs.focused = 0
	fs.focusGeneration++
	fs.pendingInput = nil
	fs.next.focus = 0
	w.clearPendingKeystrokes()
	return true
}

// recordFocusNavStart records the sequential focus navigation starting
// point for the focused element that is about to leave (the removal
// fixup's half): the stops adjacent to fs.focused in the OLD frame's
// tree order (browser-core ticket 14; Chromium's Range over the
// removed position, document.cc:6382-6395 — the forward search starts
// at the previous element, the backward search at the next one). The
// neighbors come from the old frame's tab stops: the completed frame
// no longer contains the element, and its neighbors are exactly the
// elements around the hole it left.
func (fs *windowFocusState) recordFocusNavStart() {
	if fs.rendered == nil || fs.rendered.tabStops == nil {
		return
	}
	stops := fs.rendered.tabStops
	node, ok := stops.byID[fs.focused]
	if !ok {
		// The element was not a stop of the old frame (a suppressed or
		// foreign handle): its tree position is unknowable here, so no
		// starting point — the search restarts at the cycle's ends
		// (Chromium's hidden/inert timer path blurs without one too,
		// document.cc:3271-3277).
		return
	}
	// Walk the tree-ordered stops around the removed element: the
	// nearest stop before it (prev) and after it (next).
	var point focusNavStartingPoint
	for _, candidate := range stops.inserted {
		if candidate.id == fs.focused || !candidate.isStop() {
			continue
		}
		id := candidate.id
		switch {
		case candidate.seq < node.seq:
			point.prev = &id
		case candidate.seq > node.seq:
			if point.next == nil {
				point.next = &id
			}
		}
	}
	fs.focusNavStart = point
}

// FocusPath returns the focus path of a handle in the rendered frame,
// from the root to the handle (window.rs focus_path).
func (w *Window) FocusPath(handle FocusHandle) []FocusHandle {
	if w == nil {
		return nil
	}
	fs := focusState(w)
	if fs.rendered == nil {
		return []FocusHandle{handle}
	}
	ids := fs.rendered.dispatchTree.focusPath(handle.id)
	out := make([]FocusHandle, 0, len(ids))
	for _, id := range ids {
		out = append(out, w.handleFor(id))
	}
	return out
}

// OnFocusLost subscribes to focus-lost events: the focused handle was
// released (window.rs Window::on_focus_lost). The returned
// subscription cancels the listener.
func (w *Window) OnFocusLost(callback func(w *Window)) *focusLostSubscription {
	if w == nil {
		panic("gpui: OnFocusLost requires a live window")
	}
	fs := focusState(w)
	sub := &focusLostSubscription{owner: fs, callback: callback}
	fs.focusLostListeners = append(fs.focusLostListeners, sub)
	return sub
}

// focusLostSubscription is one focus-lost listener registration.
type focusLostSubscription struct {
	owner    *windowFocusState
	callback func(w *Window)
}

// Cancel removes this listener (the subscription semantics).
func (s *focusLostSubscription) Cancel() {
	if s == nil || s.owner == nil {
		return
	}
	for i, existing := range s.owner.focusLostListeners {
		if existing == s {
			s.owner.focusLostListeners = append(s.owner.focusLostListeners[:i], s.owner.focusLostListeners[i+1:]...)
			break
		}
	}
	s.callback = nil
	s.owner = nil
}

// requestRefresh records that this window's content changed and a new
// frame should be drawn: it is Window::refresh (window.rs:2287-2292) —
// the window is marked dirty and its frame source wakes on the
// clean→dirty edge (frameloop.go markFrameDirty, Base ticket 33). Like
// the reference it has no effect while the window's frame is being drawn
// (the completed frame clears the request anyway).
func (w *Window) requestRefresh() {
	if w == nil || w.isDrawing() {
		return
	}
	w.markRefreshing()
	w.markFrameDirty()
}

// RefreshRequested reports whether a refresh was requested since the
// last completed draw (test support). With the frame loop on by default,
// a test window's request is normally served by the next effect flush,
// which draws it; the flag is then clear again.
func (w *Window) RefreshRequested() bool {
	if w == nil {
		return false
	}
	if ds, ok := windowDrawStates[w]; ok {
		return ds.refreshRequested
	}
	return false
}

// ---------------------------------------------------------------------------
// TabStopMap (tab_stop.rs TabStopMap)
// ---------------------------------------------------------------------------

// tabStopPath is a node's path: the group indices from the root,
// ending with the handle's tab index (tab_stop.rs TabStopPath).
type tabStopPath []int64

// compare orders paths lexicographically.
func (p tabStopPath) compare(other tabStopPath) int {
	for i := 0; i < len(p) && i < len(other); i++ {
		if p[i] != other[i] {
			if p[i] < other[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(p) < len(other):
		return -1
	case len(p) > len(other):
		return 1
	}
	return 0
}

// TabStopOperation records one tab-stop insertion or group boundary
// for replay (tab_stop.rs TabStopOperation).
type TabStopOperation struct {
	// Kind is the operation kind.
	Kind TabStopOperationKind
	// Handle is the inserted handle (Insert only).
	Handle FocusHandle
	// GroupIndex is the group's tab index (Group only).
	GroupIndex int64
}

// TabStopOperationKind discriminates tab-stop operations.
type TabStopOperationKind uint8

const (
	// TabStopInsert inserts a focus handle.
	TabStopInsert TabStopOperationKind = iota
	// TabStopGroup begins a tab group.
	TabStopGroup
	// TabStopGroupEnd ends a tab group.
	TabStopGroupEnd
)

// tabStopNode is one ordered tab-stop node (tab_stop.rs TabStopNode).
type tabStopNode struct {
	path  tabStopPath
	index int // insertion-history order (group operations included)
	// seq is the node's position in the insertion (prepaint = tree)
	// order of the frame's handles (TabStopMap.inserted), the scan
	// order of the excluded-element arms (browser-core 14).
	seq      int
	id       uint64
	tabIndex int64
	tabStop  bool
	window   *Window
	app      *App
}

// isStop reports whether the node is a stop of the sequential cycle
// (browser-core ticket 14, D4): a tab stop whose raw tab index is not
// negative. Negative indexes are never reached by Tab (an element
// excluded from the cycle — Chromium's AdjustedTabIndex keeps the
// attribute value while ShouldVisit's focusability gate decides,
// focus_controller.cc:1255-1283).
func (n tabStopNode) isStop() bool { return n.tabStop && n.tabIndex >= 0 }

// TabStopMap is the frame's collection of tab stops (tab_stop.rs
// TabStopMap, bounded to a sorted slice instead of a SumTree — the
// observable next/prev order is preserved), with browser-core ticket
// 14's HTML sequential order (spec D4, Chromium's ScopedFocusNavigation
// contract, focus_controller.cc:748-752: "elements with tab indexes
// first (from lowest to highest), and then elements without tab indexes
// (in document order)"): the ordered slice sorts positive tab indexes
// ascending (htmlTabRank), then every zero and negative index in
// insertion (tree) order — tabZeroRank sorts after every positive rank.
// Negative indexes are never reached by Tab (D4); from such a focused
// element the next/prev search runs in TREE order (NextFocusableElement's
// negative arm, focus_controller.cc:1354-1366: "If an element is
// excluded from the normal tabbing cycle, the next focusable element is
// determined by tree order"), which the insertion history reproduces.
type TabStopMap struct {
	currentPath      tabStopPath
	insertionHistory []TabStopOperation
	byID             map[uint64]tabStopNode
	order            []tabStopNode // kept sorted by (path, index)
	// inserted is the nodes in insertion (prepaint = tree) order, the
	// scan order of the excluded-element arms (browser-core 14).
	inserted []tabStopNode
}

// newTabStopMap creates an empty tab-stop map.
func newTabStopMap() *TabStopMap {
	return &TabStopMap{byID: make(map[uint64]tabStopNode)}
}

// tabZeroRank is the rank of every zero and negative tab index
// (browser-core ticket 14, spec D4): it sorts after every positive rank
// in the ordered slice, so the cycle is [positive ascending, then the
// rest in tree order] — Chromium's sequential order
// (focus_controller.cc:748-752).
const tabZeroRank int64 = 1 << 62

// htmlTabRank maps a raw tab index onto its ordered-slice rank
// (browser-core ticket 14): positive indexes keep their value (the
// ascending positive bucket), zero and negative indexes share
// tabZeroRank (the tree-order bucket — a negative index is never a
// stop, but its tree position is what a focused excluded element's
// Tab search walks).
func htmlTabRank(tabIndex int64) int64 {
	if tabIndex > 0 {
		return tabIndex
	}
	return tabZeroRank
}

// Insert inserts a focus handle at the current group path (TabStopMap::insert).
func (t *TabStopMap) Insert(handle FocusHandle, tabIndex int64, tabStop bool) {
	t.insertionHistory = append(t.insertionHistory, TabStopOperation{Kind: TabStopInsert, Handle: handle})
	path := append(append(tabStopPath{}, t.currentPath...), htmlTabRank(tabIndex))
	node := tabStopNode{
		path:     path,
		index:    len(t.insertionHistory) - 1,
		seq:      len(t.inserted),
		id:       handle.id,
		tabIndex: tabIndex,
		tabStop:  tabStop,
		window:   handle.window,
		app:      handle.app,
	}
	t.byID[handle.id] = node
	t.inserted = append(t.inserted, node)
	t.insertOrdered(node)
}

func (t *TabStopMap) insertOrdered(node tabStopNode) {
	pos := sort.Search(len(t.order), func(i int) bool {
		existing := t.order[i]
		cmp := existing.path.compare(node.path)
		if cmp != 0 {
			return cmp > 0
		}
		return existing.index > node.index
	})
	t.order = append(t.order, tabStopNode{})
	copy(t.order[pos+1:], t.order[pos:])
	t.order[pos] = node
}

// BeginGroup begins a tab group with the given index (TabStopMap::begin_group).
// The group's path segment takes the same htmlTabRank as an inserted
// handle's (browser-core 14): a raw 0 would sort a default group
// (tab index 0) before every zero-bucket sibling, which rank at
// tabZeroRank.
func (t *TabStopMap) BeginGroup(tabIndex int64) {
	t.insertionHistory = append(t.insertionHistory, TabStopOperation{Kind: TabStopGroup, GroupIndex: tabIndex})
	t.currentPath = append(t.currentPath, htmlTabRank(tabIndex))
}

// EndGroup ends the current tab group (TabStopMap::end_group).
func (t *TabStopMap) EndGroup() {
	t.insertionHistory = append(t.insertionHistory, TabStopOperation{Kind: TabStopGroupEnd})
	if len(t.currentPath) > 0 {
		t.currentPath = t.currentPath[:len(t.currentPath)-1]
	}
}

// Replay replays recorded operations (TabStopMap::replay).
func (t *TabStopMap) Replay(operations []TabStopOperation, tabIndexOf func(FocusHandle) (int64, bool)) {
	for _, op := range operations {
		switch op.Kind {
		case TabStopInsert:
			tabIndex, tabStop := int64(0), false
			if tabIndexOf != nil {
				if idx, ok := tabIndexOf(op.Handle); ok {
					tabIndex = idx
				}
			}
			if ref, ok := t.lookupRef(op.Handle); ok {
				tabIndex, tabStop = ref.tabIndex, ref.tabStop
			}
			t.Insert(op.Handle, tabIndex, tabStop)
		case TabStopGroup:
			t.BeginGroup(op.GroupIndex)
		case TabStopGroupEnd:
			t.EndGroup()
		}
	}
}

// lookupRef resolves a handle's tab properties from its window
// registry.
func (t *TabStopMap) lookupRef(handle FocusHandle) (*focusRef, bool) {
	if ref := handle.windowRef(); ref != nil {
		return ref, true
	}
	return nil, false
}

// Next returns the next tab stop after the focused identity
// (TabStopMap::next; browser-core ticket 14 gives it Chromium's
// NextFocusableElement semantics, focus_controller.cc:1347-1409: the
// successor in the sequential order — positive indexes ascending, then
// tree order — for a stop of the cycle; for an element excluded from
// the cycle (a negative tab index or a non-tab-stop), the next stop in
// TREE order, focus_controller.cc:1354-1366). The search does NOT
// wrap: past the last stop (or with nothing after an excluded element
// in tree order) it reports false — Chromium's nullptr answer — and
// the caller passes focus out of the page (Window.FocusNext's blur;
// AdvanceFocusInDocumentOrder's CanTakeFocus arm, focus_controller.cc:
// 2216-2231: Chrome takes the focus at the document scope's end —
// headless records the body, and the port's body equivalent is no
// handle). From no focus (the body), the first stop of the cycle:
// NextElementWithGreaterTabIndex(0) then FindElementWithExactTabIndex(0)
// (focus_controller.cc:1392-1407).
func (t *TabStopMap) Next(focusedID *uint64, w *Window) (FocusHandle, bool) {
	if len(t.order) == 0 {
		return FocusHandle{}, false
	}
	if focusedID != nil {
		if node, ok := t.byID[*focusedID]; ok {
			if node.isStop() {
				// The successor in the ordered slice (the sequential
				// cycle), no wrap: Chromium's FindElementWithExact-
				// TabIndex → NextElementWithGreaterTabIndex →
				// FindElementWithExactTabIndex(0) chain (the last stop's
				// T=0 arm returns nullptr, focus_controller.cc:1376-1381).
				start := t.slicePosition(node)
				for i := start + 1; i < len(t.order); i++ {
					if t.order[i].isStop() {
						return t.handleFor(t.order[i]), true
					}
				}
				return FocusHandle{}, false
			}
			// An excluded element: the next stop in tree (insertion)
			// order from its position — focus_controller.cc:1354-1366.
			// seq is the tree-order position (index counts group
			// operations too, which would skew the scan).
			for i := node.seq + 1; i < len(t.inserted); i++ {
				if t.inserted[i].isStop() {
					return t.handleFor(t.inserted[i]), true
				}
			}
			// Nothing after it in tree order: nullptr — the pass-out,
			// not the wrap (focus_controller.cc:1362-1366: "Falling
			// through would wrap around to the start of the scope
			// instead").
			return FocusHandle{}, false
		}
	}
	// No focus (or the focused handle left the frame): the first stop
	// of the cycle — Tab from the body.
	return t.firstStop()
}

// firstStop returns the first stop of the sequential cycle.
func (t *TabStopMap) firstStop() (FocusHandle, bool) {
	for _, node := range t.order {
		if node.isStop() {
			return t.handleFor(node), true
		}
	}
	return FocusHandle{}, false
}

// lastStop returns the last stop of the sequential cycle.
func (t *TabStopMap) lastStop() (FocusHandle, bool) {
	for i := len(t.order) - 1; i >= 0; i-- {
		if t.order[i].isStop() {
			return t.handleFor(t.order[i]), true
		}
	}
	return FocusHandle{}, false
}

// Prev returns the previous tab stop before the focused identity
// (TabStopMap::prev; browser-core ticket 14 gives it Chromium's
// PreviousFocusableElement semantics, focus_controller.cc:1411-1443:
// the predecessor in the sequential order for a stop of the cycle; for
// an excluded element, the previous stop in TREE order (:1417-1430)).
// Like Next it does not wrap: before the first stop it reports false
// (the pass-out), and from no focus (the body) it returns the LAST
// stop — Chromium's MoveToLast + FindElementWithExactTabIndex(0,
// kBackward), the last zero-index element, which is the cycle's end
// because the zero bucket is last (focus_controller.cc:1411-1431).
func (t *TabStopMap) Prev(focusedID *uint64, w *Window) (FocusHandle, bool) {
	if len(t.order) == 0 {
		return FocusHandle{}, false
	}
	if focusedID != nil {
		if node, ok := t.byID[*focusedID]; ok {
			if node.isStop() {
				// The predecessor in the ordered slice, no wrap:
				// Chromium's FindElementWithExactTabIndex(kBackward) →
				// PreviousElementWithLowerTabIndex chain, nullptr before
				// the first stop (focus_controller.cc:1411-1443).
				start := t.slicePosition(node)
				for i := start - 1; i >= 0; i-- {
					if t.order[i].isStop() {
						return t.handleFor(t.order[i]), true
					}
				}
				return FocusHandle{}, false
			}
			// An excluded element: the previous stop in tree (insertion)
			// order — focus_controller.cc:1417-1430 (seq: tree order).
			for i := node.seq - 1; i >= 0; i-- {
				if t.inserted[i].isStop() {
					return t.handleFor(t.inserted[i]), true
				}
			}
			// Nothing before it in tree order: nullptr — the pass-out.
			return FocusHandle{}, false
		}
	}
	// No focus (or the focused handle left the frame): the last stop of
	// the cycle — Shift+Tab from the body.
	return t.lastStop()
}

// slicePosition finds the node's position in the ordered slice.
func (t *TabStopMap) slicePosition(node tabStopNode) int {
	for i, existing := range t.order {
		if existing.id == node.id {
			return i
		}
	}
	return -1
}

func (t *TabStopMap) handleFor(node tabStopNode) FocusHandle {
	return FocusHandle{id: node.id, window: node.window, app: node.app}
}

// TabStopCount returns the number of tab stops (tab_stop.rs
// tab_stop_count; browser-core 14: a stop of the sequential cycle —
// negative tab indexes are excluded).
func (t *TabStopMap) TabStopCount() int {
	count := 0
	for _, node := range t.order {
		if node.isStop() {
			count++
		}
	}
	return count
}

// PaintIndex returns the insertion history length (the reuse seam).
func (t *TabStopMap) PaintIndex() int { return len(t.insertionHistory) }

// History returns the recorded operations (replay source).
func (t *TabStopMap) History() []TabStopOperation {
	return append([]TabStopOperation{}, t.insertionHistory...)
}

// String renders the map for diagnostics.
func (t *TabStopMap) String() string {
	return fmt.Sprintf("TabStopMap(%d stops, %d operations)", t.TabStopCount(), len(t.insertionHistory))
}
