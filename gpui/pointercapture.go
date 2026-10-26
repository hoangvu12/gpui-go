package gpui

// This file is browser-core ticket 11 (pointer capture retargeting and
// the element-local position, spec D12): while an element holds the
// pointer capture, pointer events go to it alone — the hit test is
// retargeted to the capturing element and its ancestors, like Chrome's
// setPointerCapture — with got/lost-capture callbacks, a clean cancel
// when another window takes the OS capture (WM_CAPTURECHANGED), and an
// element-local position on the mouse events. The pin's CapturePointer
// API is kept and extended, not replaced (the pin's captured_hitbox,
// window.rs:1330, only forced IsHovered true for one hitbox and was
// auto-released on the next mouse up, mouseevent.go's old tail).
//
// The Chromium rules this ports (verified in %TEMP%\research-src\
// chromium @ 0d4dcd4f, third_party/blink/renderer/core/input/
// pointer_event_manager.cc):
//
//   - setPointerCapture is pending-only and succeeds only while a
//     button is held: SetPointerCapture checks IsActiveButtonsState
//     (:1440-1447). The gotpointercapture event fires when the pending
//     capture is processed, not when it is set.
//   - The pending capture is processed before every pointer event, and
//     before the boundary events and the event itself:
//     ProcessCaptureAndPositionOfPointerEvent calls
//     ProcessPendingPointerCapture first (:1357), then resolves the
//     effective target and updates the element under the pointer
//     (:1359-1362) — which is where the boundary (enter/leave) events
//     come from, so they track the CAPTURE target while captured.
//   - ProcessPendingPointerCapture (:1369-1414) fires lostpointercapture
//     at the old target (re-targeted to the document when the element
//     left the tree, :1387-1393), then gotpointercapture at the pending
//     target, setting the element under the pointer to the pending
//     target first (:1398) so any boundary events precede the got.
//   - The event target is the capture target, for the dispatch and for
//     the boundary tracking alike: GetEffectiveTargetForPointerEvent
//     returns the capture target when one is set (:232-239).
//   - Pointerup and pointercancel release the capture: the pending
//     override is erased right after the up's dispatch (:1244-1246),
//     the target is re-hit-tested as a release hit test (:1268-1281),
//     lostpointercapture fires before the click (:1277), and the
//     boundary events follow the release (:1300-1313). A pointercancel
//     sends no boundary events (:1316-1321, "Don't send boundary
//     events in this case as it is a little tricky").
//   - A move with no buttons held releases the capture before the
//     move's own processing (:1144-1149, "This is for when the mouse is
//     released outside of the page").
//   - gotpointercapture and lostpointercapture bubble
//     (core/events/pointer_event_factory.cc:287-296
//     SetEventSpecificFields: only pointerenter/pointerleave are
//     non-bubbling), and clone the causing event's coordinates
//     (CreatePointerCaptureEvent, core/events/pointer_event_factory.cc).
//
// The W3C Pointer Events spec agrees: the process pending pointer
// capture steps run when firing any pointer event that is not
// gotpointercapture/lostpointercapture/click/auxclick/contextmenu;
// every event targets the capture target override while one is set;
// and "immediately after firing the pointerup or pointercancel events,
// the user agent MUST clear the pending pointer capture target
// override … and then run process pending pointer capture steps to
// fire lostpointercapture if necessary."

import (
	"slices"
)

// ---------------------------------------------------------------------------
// The events
// ---------------------------------------------------------------------------

// GotPointerCaptureEvent is the DOM gotpointercapture event: it fires
// on an element when it receives the pointer capture, at the start of
// the first pointer event dispatched after the capture was requested
// (the pending capture's processing). It bubbles (like the DOM event),
// so ancestors receive it after the target. Position is the causing
// event's window-local position (Chromium clones the dispatching
// pointer event's coordinates into the capture event).
type GotPointerCaptureEvent struct {
	// Position is the causing event's window-local position.
	Position Point
	// Modifiers were held on the causing event.
	Modifiers Modifiers
}

// LostPointerCaptureEvent is the DOM lostpointercapture event: it
// fires on an element when it loses the pointer capture — after the
// pointerup that implicitly released it, on a pointer cancel, or when
// another element takes the capture. It bubbles, and its Position is
// the causing event's window-local position.
type LostPointerCaptureEvent struct {
	// Position is the causing event's window-local position.
	Position Point
	// Modifiers were held on the causing event.
	Modifiers Modifiers
}

// PointerCancelEvent is the DOM pointercancel event's port: the
// pointer stream was cancelled — on Windows, another window took the
// mouse capture mid-press (WM_CAPTURECHANGED), which the pin never
// observed (research browser-parity 03 §5: the pending press, the
// active style and the drag stayed stuck). Dispatching it releases the
// pointer capture, drops every pending press so no button stays stuck,
// and cancels an active drag. The Windows message carries no
// coordinates, so Position carries the window's last tracked position
// (best effort, like MouseExitEvent); its default-prevented state and
// any stopped propagation have no meaning (the event is not
// cancelable in the DOM either).
type PointerCancelEvent struct {
	// Position is the last known pointer position, best effort.
	Position Point
	// Modifiers were held when the stream was cancelled.
	Modifiers Modifiers
}

// pointerCaptureEvent is the internal marker the capture dispatch
// sends through the rendered frame's mouse listeners: each element's
// capture listener (paintCaptureListeners) fires its got/lost
// listeners when its hitbox is on the marker's capture chain. It is
// never seen by user listeners.
type pointerCaptureEvent struct {
	// got selects the gotpointercapture sweep over the lost one.
	got bool
	// chain is the capture chain's hit test (the captured hitbox and
	// its hitbox ancestors): a listener fires only when its hitbox is
	// on the chain, which is the DOM's bubbling propagation path of a
	// capture event at the captured element.
	chain hitTest
	// position is the causing event's position.
	position Point
	// modifiers were held on the causing event.
	modifiers Modifiers
}

// ---------------------------------------------------------------------------
// The capture state (window.rs:1330 captured_hitbox, extended)
// ---------------------------------------------------------------------------

// pointerCapture is one pointer-capture target: the hitbox identity as
// captured plus the owning element's GlobalElementID path, which is
// stable across frames (hitbox ids are per-frame; window.rs
// next_hitbox_id never resets). The zero value is "no capture".
type pointerCapture struct {
	// hitboxID is the hitbox id as captured (the frame where
	// CapturePointer ran; diagnostics and the pin-compat id-only match).
	hitboxID uint64
	// path is the captured element's GlobalElementID key. Empty means
	// the hitbox's owner could not be resolved (an id-less element):
	// the capture then keeps the pin's semantics — the exact hitbox id
	// reports hovered — and, like the pin, dies with the frame it was
	// set in.
	path string
}

// pointerButtonBit maps a MouseButton to its bit of the window's
// tracked held-button set (the dispatch-side twin of the host's
// mouseButtonsHeld, desktop_input_windows.go: the buttons Chromium's
// IsActiveButtonsState reads). Buttons outside the known set report 0.
func pointerButtonBit(button MouseButton) uint8 {
	switch button {
	case MouseButtonLeft:
		return 1 << 0
	case MouseButtonRight:
		return 1 << 1
	case MouseButtonMiddle:
		return 1 << 2
	case MouseButtonButton4:
		return 1 << 3
	case MouseButtonButton5:
		return 1 << 4
	}
	return 0
}

// ---------------------------------------------------------------------------
// The API (window.rs:3098-3110, extended toward the browser)
// ---------------------------------------------------------------------------

// CapturePointer captures the pointer for the hitbox (window.rs:3098
// capture_pointer), with the browser's pending-capture semantics
// (browser-core ticket 11, spec D12): the request is recorded as the
// pending capture, it takes effect — firing gotpointercapture — at the
// start of the next dispatched pointer event, and while it holds,
// pointer events go to the captured element and its ancestors alone
// (the hit test is retargeted; IsHovered answers for the capture
// chain). It reports whether the capture was accepted: like
// Chromium's SetPointerCapture, the request only succeeds while a
// button is held (pointer_event_manager.cc:1440-1447
// IsActiveButtonsState). The capture releases implicitly after the
// next pointerup (or pointercancel), and explicitly through
// ReleasePointer.
//
// The hitbox's element should carry an ID (Div().ID(...)): the
// capture's cross-frame identity is the element's id path, so an
// id-less element's capture — like the pin's — only survives until the
// frame re-renders.
func (w *Window) CapturePointer(id HitboxID) bool {
	if w == nil {
		panic("gpui: CapturePointer requires a live window")
	}
	is := inputState(w)
	if is.pointerButtonsPressed == 0 {
		// Chromium rejects the capture when no button is active
		// (pointer_event_manager.cc:1440-1447); a request with no
		// button held would also be released by the next no-buttons
		// move (:1144-1149) anyway.
		return false
	}
	is.pendingPointerCapture = pointerCapture{
		hitboxID: uint64(id),
		path:     w.hitboxOwnerPath(uint64(id)),
	}
	return true
}

// ReleasePointer releases any active pointer capture (window.rs:3103
// release_pointer): the pending capture target is cleared, and the
// active capture's lostpointercapture fires at the start of the next
// dispatched pointer event — the pending-capture model the DOM's
// releasePointerCapture follows (the lost event fires when the pending
// override is processed, not synchronously).
func (w *Window) ReleasePointer() {
	if w == nil {
		panic("gpui: ReleasePointer requires a live window")
	}
	is := inputState(w)
	is.pendingPointerCapture = (pointerCapture{})
}

// CapturedHitbox returns the hitbox that holds the pointer capture, if
// any (window.rs:3108): the ACTIVE capture — a capture requested this
// event (CapturePointer) becomes active, and CapturedHitbox reports
// it, only once the next pointer event has processed the pending
// capture, which is when Chromium delivers gotpointercapture too.
func (w *Window) CapturedHitbox() (HitboxID, bool) {
	if w == nil {
		return 0, false
	}
	is := inputState(w)
	return HitboxID(is.activePointerCapture.hitboxID), is.activePointerCapture != (pointerCapture{})
}

// hitboxOwnerPath resolves a hitbox id's owning element path from the
// frame under construction or the rendered frame (CapturePointer runs
// inside a listener dispatch or, unusually, during a paint).
func (w *Window) hitboxOwnerPath(id uint64) string {
	fs := focusState(w)
	for _, frame := range []*renderedFrame{fs.next, fs.rendered} {
		if frame == nil {
			continue
		}
		for _, record := range frame.hitboxes {
			if record.box.id == id {
				return record.ownerPath
			}
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// The pending-capture processing (spec process pending pointer capture;
// Chromium ProcessPendingPointerCapture, :1369-1414)
// ---------------------------------------------------------------------------

// processPendingPointerCapture runs the spec's process pending pointer
// capture steps for the event about to dispatch: when the pending
// capture differs from the active one, the old target's
// lostpointercapture fires, then the new target's gotpointercapture —
// retargeting the hit test to the new capture chain and settling the
// boundary events first, the way Chromium's SetElementUnderPointer at
// :1398 precedes the got dispatch — and the active capture becomes the
// pending one. A pure release (pending zero, active set) fires only
// the lost event: the boundary events belong to the caller's later
// hit-test update (the pointerup tail, or the move's own processing).
func (w *Window) processPendingPointerCapture(event any, app *App) {
	is := inputState(w)
	if is.pendingPointerCapture == is.activePointerCapture {
		return
	}
	fs := focusState(w)
	if fs.rendered == nil {
		// No completed frame: nothing can receive the capture events.
		// The pending request stays for a frame to arrive.
		return
	}

	// Step 1 (spec): fire lostpointercapture at the old override.
	if old := is.activePointerCapture; old != (pointerCapture{}) {
		is.activePointerCapture = (pointerCapture{})
		if chain, ok := captureChainOf(fs.rendered, old); ok {
			w.dispatchPointerCaptureEvent(false, chain, event, app)
		}
	}

	// Step 2-3 (spec): the pending override becomes the capture.
	if next := is.pendingPointerCapture; next != (pointerCapture{}) {
		is.activePointerCapture = next
		if chain, ok := captureChainOf(fs.rendered, next); ok {
			// Chromium's ProcessPendingPointerCapture sets the element
			// under the pointer to the pending target before dispatching
			// gotpointercapture (:1398), so the boundary events of the
			// transition precede the got event.
			is.mouseHitTest = chain
			position, modifiers := pointerEventPositionModifiers(event)
			w.dispatchHoverBoundaryEvents(position, modifiers, app)
			w.dispatchPointerCaptureEvent(true, chain, event, app)
		} else if next.path == "" {
			// An id-only (pin-compat) capture has no chain to retarget:
			// the pin's exact-hitbox IsHovered early-true carries it.
		}
	}
}

// dispatchPointerCaptureEvent delivers one gotpointercapture or
// lostpointercapture to the capture chain, target first then
// ancestors (the events bubble, pointer_event_factory.cc:287-296): the
// frame's mouse listeners register parent-before-child, so reverse
// registration order is the bubbling order. Every chain listener is
// invoked regardless of propagation breaks, like the boundary sweeps.
func (w *Window) dispatchPointerCaptureEvent(got bool, chain hitTest, cause any, app *App) {
	fs := focusState(w)
	if fs.rendered == nil || len(chain.orderedIDs) == 0 {
		return
	}
	position, modifiers := pointerEventPositionModifiers(cause)
	marker := &pointerCaptureEvent{got: got, chain: chain, position: position, modifiers: modifiers}
	listeners := append([]mouseListener(nil), fs.rendered.mouseListeners...)
	for i := len(listeners) - 1; i >= 0; i-- {
		listeners[i](marker, DispatchBubble, w, app)
	}
}

// pointerEventPositionModifiers reads the causing event's position and
// modifiers (Chromium's capture events clone the dispatching pointer
// event's coordinates).
func pointerEventPositionModifiers(event any) (Point, Modifiers) {
	switch e := event.(type) {
	case *MouseMoveEvent:
		return e.Position, e.Modifiers
	case *MouseDownEvent:
		return e.Position, e.Modifiers
	case *MouseUpEvent:
		return e.Position, e.Modifiers
	case *MouseExitEvent:
		return e.Position, e.Modifiers
	case *PointerCancelEvent:
		return e.Position, e.Modifiers
	case *ScrollWheelEvent:
		return e.Position, e.Modifiers
	case *PinchEvent:
		return e.Position, e.Modifiers
	}
	return Point{}, Modifiers{}
}

// ---------------------------------------------------------------------------
// The capture chain (Chromium's event-path retargeting)
// ---------------------------------------------------------------------------

// captureChainOf resolves a capture target's hitbox in a frame by its
// element path and builds the capture chain: the captured hitbox plus
// its hitbox ancestors (the frame's hitbox stack recorded each hitbox's
// enclosing hitbox at insertion), which is the DOM propagation path
// the captured element's events follow. The chain's members all report
// hovered (hitTest's mouse count covers the whole chain: occlusion is
// a position's property, and the capture target is retargeted, not
// hit-tested). It reports false when the captured element is not in
// the frame's tree.
func captureChainOf(frame *renderedFrame, capture pointerCapture) (hitTest, bool) {
	if capture.path == "" || frame == nil {
		return hitTest{}, false
	}
	for _, record := range frame.hitboxes {
		if record.ownerPath != capture.path {
			continue
		}
		ids := make([]uint64, 0, 8)
		for ancestor := record; ancestor != nil; ancestor = ancestor.parent {
			ids = append(ids, ancestor.box.id)
		}
		return hitTest{orderedIDs: ids, hoverHitboxCount: len(ids)}, true
	}
	return hitTest{}, false
}

// currentHitTest returns the frame's effective hit test: the capture
// chain while a resolved capture is active (Chromium's
// GetEffectiveTargetForPointerEvent + SetElementUnderPointer: the
// element under the pointer IS the capture target, :232-239, :1362),
// and otherwise the positional hit test at the tracked mouse. A
// captured element that left the frame's tree drops the capture
// silently — Chromium re-targets the loss to the document
// (:1387-1393); the port has no document-level listener, so nothing
// can receive it.
func (w *Window) currentHitTest(frame *renderedFrame) hitTest {
	is := inputState(w)
	if capture := is.activePointerCapture; capture.path != "" && frame != nil {
		if chain, ok := captureChainOf(frame, capture); ok {
			return chain
		}
		is.activePointerCapture = (pointerCapture{})
		if is.pendingPointerCapture.path == capture.path {
			is.pendingPointerCapture = (pointerCapture{})
		}
	}
	if frame == nil {
		return is.mouseHitTest
	}
	return frame.hitTestAt(is.mousePosition)
}

// ---------------------------------------------------------------------------
// The element-local position (spec D12, research browser-parity 03 §5)
// ---------------------------------------------------------------------------

// localPositionOf subtracts the hitbox's bounds origin from a
// window-local position: the element-local position for the hitbox's
// element, the listener's view of the DOM's offsetX/offsetY (the DOM
// computes it against the target's padding edge; the port computes it
// against the hitbox's bounds — the element's border box — which is
// the box the port's hit testing and drag math use).
func localPositionOf(position Point, hitbox *Hitbox) Point {
	if hitbox == nil {
		return Point{}
	}
	// An element transform maps the element-local space too
	// (browser-core ticket 56): the DOM's offsetX/offsetY of a
	// transformed element are the pre-image of the pointer (the same
	// inverse mapping Hitbox.Contains applies), so a slider inside a
	// rotated parent still reads coordinates in the element's own
	// space.
	if hitbox.transform != nil {
		if inverse, ok := invertTransformation(*hitbox.transform); ok {
			position = applyTransformation(inverse, position)
		}
	}
	return Point{
		X: position.X - hitbox.Bounds.Origin.X,
		Y: position.Y - hitbox.Bounds.Origin.Y,
	}
}

// LocalPosition returns the pointer position of this move relative to
// the hitbox's bounds origin: the element-local position (spec D12),
// so sliders and dividers need not subtract the hitbox origin
// themselves. While this element holds the pointer capture the value
// runs past the bounds (negative, or past the size) exactly like the
// DOM's offsetX during a captured drag.
func (e *MouseMoveEvent) LocalPosition(hitbox *Hitbox) Point {
	return localPositionOf(e.Position, hitbox)
}

// LocalPosition returns the pointer position of this press relative to
// the hitbox's bounds origin (the element-local position, spec D12).
func (e *MouseDownEvent) LocalPosition(hitbox *Hitbox) Point {
	return localPositionOf(e.Position, hitbox)
}

// LocalPosition returns the pointer position of this release relative
// to the hitbox's bounds origin (the element-local position, spec D12).
func (e *MouseUpEvent) LocalPosition(hitbox *Hitbox) Point {
	return localPositionOf(e.Position, hitbox)
}

// ---------------------------------------------------------------------------
// The element's capture listeners (interact.go paintMouseListeners)
// ---------------------------------------------------------------------------

// paintCaptureListeners is one element's gotpointercapture/
// lostpointercapture registration on the frame under construction
// (called from paintMouseListeners): the element's hitbox and its
// got/lost listeners. The closure reacts to the internal capture
// marker only, firing when its hitbox is on the marker's capture chain
// (the target or one of its ancestors — the events bubble).
func (d *DivElement) paintCaptureListeners(hitbox Hitbox, w *Window, app *App) {
	if len(d.gotPointerCaptureListeners) == 0 && len(d.lostPointerCaptureListeners) == 0 {
		return
	}
	gotListeners := slices.Clone(d.gotPointerCaptureListeners)
	lostListeners := slices.Clone(d.lostPointerCaptureListeners)
	box := hitbox
	w.OnMouseEvent(func(event any, phase DispatchPhase, w *Window, app *App) {
		marker, ok := event.(*pointerCaptureEvent)
		if !ok {
			return
		}
		if !marker.chain.contains(box.id) {
			return
		}
		if marker.got {
			e := &GotPointerCaptureEvent{Position: marker.position, Modifiers: marker.modifiers}
			for _, listener := range gotListeners {
				listener(e, w, app)
			}
		} else {
			e := &LostPointerCaptureEvent{Position: marker.position, Modifiers: marker.modifiers}
			for _, listener := range lostListeners {
				listener(e, w, app)
			}
		}
	})
}
