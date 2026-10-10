package gpui

// This file is ticket24's mouse-event surface: the platform input
// vocabulary the file-drop and gesture paths dispatch through
// (interactive.rs MouseMove/MouseUp/MouseDown/ScrollWheel/Pinch and
// their payload shapes), the hitbox/hit-test model (window.rs
// Hitbox/HitboxId/HitTest) and the mouse-listener dispatch
// (window.rs dispatch_mouse_event), ported from the pinned CE
// 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a:
//
//   - crates/gpui/src/interactive.rs:88-100 — TouchPhase;
//     :522-534 — ScrollWheelEvent; :554-565 — ScrollDelta;
//     :570-585 — PinchEvent; the mouse event shapes (MouseMoveEvent,
//     MouseUpEvent, MouseDownEvent) the dispatch consumes.
//   - crates/gpui/src/window.rs:867-980 — HitboxId (is_hovered /
//     is_hovered_ignoring_last_input / should_handle_scroll /
//     hit_test) and Hitbox (id, bounds, content mask, behavior,
//     contains).
//   - crates/gpui/src/window.rs:757-830 — HitTest::new (the reverse
//     insertion-order occlusion walk: hover ids stop at the first
//     mouse-blocking box, scroll ids stop only at full occlusion).
//   - crates/gpui/src/window.rs:989-1015 — HitboxBehavior (Normal /
//     BlockMouse / BlockMouseExceptScroll with blocks_mouse /
//     occludes semantics).
//   - crates/gpui/src/window.rs:5426-5448 — Window::insert_hitbox
//     (bounds + behavior, the content mask captured at insertion).
//   - crates/gpui/src/window.rs:5578-5590 — Window::on_mouse_event:
//     frame-scoped listener registrations.
//   - crates/gpui/src/window.rs:6081-6136 — dispatch_mouse_event: the
//     hit test, capture then bubble over the rendered frame's mouse
//     listeners (propagation stops between listeners), the active-drag
//     refresh on move / cancel on up, and the pointer-capture release
//     on mouse up.
//   - crates/gpui/src/window.rs:3061-3071 — prevent_default /
//     default_prevented; App::stop_propagation maps to the existing
//     propagateEvent flag (key_dispatch.go).
//
// Bounded to ticket24's needs (deliberate, separate tickets): mouse
// capture (window.rs captured_hitbox is carried but only the
// auto-release-on-up rule is wired; SetCapture arrives with the mouse
// input ticket), click synthesis (ClickEvent's MouseUp/Keyboard
// dispatch — ticket13 landed the keyboard click path), hover styling
// and cursor-style reset. Mouse-pressure, touch, long-press and
// mouse-exited events belong to the mouse/touch input tickets.

// ---------------------------------------------------------------------------
// Mouse event vocabulary (interactive.rs)
// ---------------------------------------------------------------------------

// MouseButton is a physical mouse button (the reference MouseButton;
// the Windows platform produces Left/Right/Middle and the X buttons).
type MouseButton uint8

// Mouse buttons.
const (
	// MouseButtonLeft is the primary button.
	MouseButtonLeft MouseButton = iota
	// MouseButtonRight is the secondary button.
	MouseButtonRight
	// MouseButtonMiddle is the wheel button.
	MouseButtonMiddle
	// MouseButtonButton4 is the first extended button.
	MouseButtonButton4
	// MouseButtonButton5 is the second extended button.
	MouseButtonButton5
)

// TouchPhase is the phase of a touch or gesture (interactive.rs
// TouchPhase). Direct Manipulation scroll and pinch events carry it.
type TouchPhase uint8

// String renders the phase for diagnostics and transcripts.
func (p TouchPhase) String() string {
	switch p {
	case TouchPhaseStarted:
		return "Started"
	case TouchPhaseMoved:
		return "Moved"
	case TouchPhaseEnded:
		return "Ended"
	case TouchPhaseCancelled:
		return "Cancelled"
	}
	return "Unknown"
}

// Touch phases.
const (
	// TouchPhaseStarted is the start of the gesture.
	TouchPhaseStarted TouchPhase = iota
	// TouchPhaseMoved is an in-progress gesture update (the default).
	TouchPhaseMoved
	// TouchPhaseEnded is a completed gesture.
	TouchPhaseEnded
	// TouchPhaseCancelled is a gesture the system took over; consumers
	// must unwind any in-progress interaction.
	TouchPhaseCancelled
)

// MouseMoveEvent reports a mouse movement (interactive.rs
// MouseMoveEvent).
type MouseMoveEvent struct {
	// Position is the position of the mouse relative to the window.
	Position Point
	// PressedButton is the button held during the move, when any (the
	// reference Option<MouseButton>; a file-drag move presses Left).
	PressedButton *MouseButton
	// Modifiers were held down when the mouse moved.
	Modifiers Modifiers
}

// MouseDownEvent reports a mouse press (interactive.rs
// MouseDownEvent).
type MouseDownEvent struct {
	// Button is the pressed button.
	Button MouseButton
	// Position is the position of the mouse relative to the window.
	Position Point
	// Modifiers were held down when the button was pressed.
	Modifiers Modifiers
	// ClickCount is the click number within the multi-click window.
	ClickCount int
	// FirstMouse is the first click inside the window (macOS semantic;
	// the Windows platform leaves it false).
	FirstMouse bool
}

// MouseUpEvent reports a mouse release (interactive.rs MouseUpEvent).
type MouseUpEvent struct {
	// Button is the released button.
	Button MouseButton
	// Position is the position of the mouse relative to the window.
	Position Point
	// Modifiers were held down when the button was released.
	Modifiers Modifiers
	// ClickCount is the click number within the multi-click window.
	ClickCount int
}

// ScrollDelta is the change in scroll position (interactive.rs
// ScrollDelta): exact pixels or inexact lines.
type ScrollDelta struct {
	// Pixels is the exact pixel delta (the Pixels variant).
	Pixels Point
	// Lines is the inexact line-based delta (the Lines variant).
	Lines Point
	// IsPixels reports the active variant (ScrollDelta's default is
	// Lines; Direct Manipulation always reports Pixels).
	IsPixels bool
}

// ScrollDeltaPixels builds the Pixels variant.
func ScrollDeltaPixels(dx, dy float32) ScrollDelta {
	return ScrollDelta{Pixels: Point{X: dx, Y: dy}, IsPixels: true}
}

// ScrollDeltaLines builds the Lines variant.
func ScrollDeltaLines(dx, dy float32) ScrollDelta {
	return ScrollDelta{Lines: Point{X: dx, Y: dy}}
}

// ScrollWheelEvent reports a scroll (interactive.rs
// ScrollWheelEvent): the mouse wheel and precision-touchpad pans.
type ScrollWheelEvent struct {
	// Position is the mouse position relative to the window.
	Position Point
	// Delta is the scroll change.
	Delta ScrollDelta
	// Modifiers were held down when the wheel moved.
	Modifiers Modifiers
	// TouchPhase is the gesture phase (wheel scrolls report Moved).
	TouchPhase TouchPhase
}

// PinchEvent reports a pinch-to-zoom gesture (interactive.rs
// PinchEvent).
type PinchEvent struct {
	// Position is the pinch center relative to the window.
	Position Point
	// Delta is the zoom delta: 0.1 is a 10% zoom increase.
	Delta float32
	// Modifiers were held down during the pinch.
	Modifiers Modifiers
	// Phase is the gesture phase.
	Phase TouchPhase
}

// ---------------------------------------------------------------------------
// Hitboxes (window.rs Hitbox / HitboxId / HitboxBehavior)
// ---------------------------------------------------------------------------

// HitboxBehavior changes how a hitbox affects hitboxes behind it
// (window.rs HitboxBehavior).
type HitboxBehavior uint8

// Hitbox behaviors.
const (
	// HitboxNormal does not affect mouse handling for other hitboxes.
	HitboxNormal HitboxBehavior = iota
	// HitboxBlockMouse makes hitboxes behind it report both
	// IsHovered == false and ShouldHandleScroll == false
	// (BlockMouse; InteractiveElement::occlude).
	HitboxBlockMouse
	// HitboxBlockMouseExceptScroll makes hitboxes behind it report
	// IsHovered == false while ShouldHandleScroll stays true
	// (BlockMouseExceptScroll).
	HitboxBlockMouseExceptScroll
)

// blocksMouse reports whether the behavior blocks mouse handling for
// hitboxes behind it (HitboxBehavior::blocks_mouse: BlockMouse or
// BlockMouseExceptScroll).
func (b HitboxBehavior) blocksMouse() bool {
	return b == HitboxBlockMouse || b == HitboxBlockMouseExceptScroll
}

// occludes reports whether the behavior fully occludes hitboxes behind
// it (HitboxBehavior::occludes: BlockMouse).
func (b HitboxBehavior) occludes() bool {
	return b == HitboxBlockMouse
}

// Hitbox is a rectangular region that potentially blocks hitboxes
// inserted before it (window.rs Hitbox). Hitboxes are inserted during
// prepaint in parent-before-child order; the hit test walks them
// child-first.
type Hitbox struct {
	// id is the unique hitbox identity (HitboxId).
	id uint64
	// Bounds is the hitbox's window-local bounds.
	Bounds Bounds
	// contentMask is the content mask active at insertion (the
	// reference Hitbox::content_mask; contains tests against it).
	contentMask Bounds
	// behavior is the hitbox behavior.
	behavior HitboxBehavior
}

// Contains tests the actual region, including the content mask,
// without occlusion checks (Hitbox::contains).
func (h *Hitbox) Contains(point Point) bool {
	return boundsContain(h.contentMask, point) && boundsContain(h.Bounds, point)
}

// IsHovered reports whether this hitbox is currently hovered: false
// while the last input modality is keyboard (so keyboard navigation
// suppresses hover highlights), true when this hitbox captured the
// pointer, and otherwise the hit-test answer (Hitbox::is_hovered).
func (h *Hitbox) IsHovered(w *Window) bool {
	if w == nil {
		return false
	}
	is := inputState(w)
	if is.capturedHitbox == h.id {
		return true
	}
	if is.lastInputWasKeyboard {
		return false
	}
	return is.mouseHitTest.contains(h.id)
}

// IsHoveredIgnoringLastInput is IsHovered without the keyboard
// suppression (HitboxId::is_hovered_ignoring_last_input).
func (h *Hitbox) IsHoveredIgnoringLastInput(w *Window) bool {
	if w == nil {
		return false
	}
	is := inputState(w)
	if is.capturedHitbox == h.id {
		return true
	}
	return is.mouseHitTest.contains(h.id)
}

// ShouldHandleScroll reports whether this hitbox contains the mouse
// and should handle scroll events: unlike IsHovered this ignores the
// keyboard modality and is not blocked by BlockMouseExceptScroll
// (HitboxId::should_handle_scroll).
func (h *Hitbox) ShouldHandleScroll(w *Window) bool {
	if w == nil {
		return false
	}
	return inputState(w).mouseHitTest.containsScrollable(h.id)
}

// hitboxRecord is the frame's stored hitbox (window.rs
// Frame::hitboxes entries).
type hitboxRecord struct {
	// box is the hitbox value (id, bounds, mask, behavior).
	box Hitbox
}

// contains is Hitbox::contains.
func (r *hitboxRecord) contains(point Point) bool {
	return r.box.Contains(point)
}

// hitTest is the occlusion test through hitboxes at a mouse position
// (window.rs HitTest): the ordered non-occluded hitbox ids and the
// count of them that support mouse (everything after it only supports
// scroll).
type hitTest struct {
	// orderedIDs are the hitbox ids in the test's click path,
	// front-most first; the last is the box that caused occlusion.
	orderedIDs []uint64
	// hoverHitboxCount is the number of leading ordered ids that
	// support mouse.
	hoverHitboxCount int
}

// contains reports whether id is among the non-occluded mouse-path
// ids (HitTest::iter_hovered).
func (t *hitTest) contains(id uint64) bool {
	for i := 0; i < t.hoverHitboxCount && i < len(t.orderedIDs); i++ {
		if t.orderedIDs[i] == id {
			return true
		}
	}
	return false
}

// containsScrollable reports whether id is among the ids that were
// not blocking scroll (HitTest::iter_scrollable).
func (t *hitTest) containsScrollable(id uint64) bool {
	for _, candidate := range t.orderedIDs {
		if candidate == id {
			return true
		}
	}
	return false
}

// equal compares two hit tests (the pin's PartialEq guard before a
// cursor-style reset).
func (t *hitTest) equal(other *hitTest) bool {
	if t.hoverHitboxCount != other.hoverHitboxCount || len(t.orderedIDs) != len(other.orderedIDs) {
		return false
	}
	for i, id := range t.orderedIDs {
		if other.orderedIDs[i] != id {
			return false
		}
	}
	return true
}

// hitTestAt computes the hit test at a position: iterate the frame's
// hitboxes reverse (index 0 = closest to the viewer), collect the
// containing ids until full occlusion, and remember where mouse
// blocking starts (window.rs HitTest::new).
func (f *renderedFrame) hitTestAt(position Point) hitTest {
	numUntilMouseBlocked := -1
	var ids []uint64
	foundHitOcclusion := false
	for i := len(f.hitboxes) - 1; i >= 0; i-- {
		record := f.hitboxes[i]
		if foundHitOcclusion {
			continue
		}
		if !record.contains(position) {
			continue
		}
		ids = append(ids, record.box.id)
		if numUntilMouseBlocked < 0 && record.box.behavior.blocksMouse() {
			numUntilMouseBlocked = len(ids)
		}
		if record.box.behavior.occludes() {
			foundHitOcclusion = true
		}
	}
	if numUntilMouseBlocked < 0 {
		numUntilMouseBlocked = len(ids)
	}
	return hitTest{orderedIDs: ids, hoverHitboxCount: numUntilMouseBlocked}
}

// boundsContain reports whether bounds contains point (Bounds::contains
// over window-local logical pixels: origin-inclusive, extent-exclusive).
func boundsContain(b Bounds, point Point) bool {
	return point.X >= b.Origin.X && point.X < b.Origin.X+b.Size.Width &&
		point.Y >= b.Origin.Y && point.Y < b.Origin.Y+b.Size.Height
}

// ---------------------------------------------------------------------------
// The window input state (window.rs Window mouse members)
// ---------------------------------------------------------------------------

// inputModality is the last input modality (window.rs InputModality).
type inputModality uint8

const (
	inputModalityMouse inputModality = iota
	inputModalityKeyboard
	inputModalityTouch
)

// windowInputState is the window's tracked input state, touched only
// on the foreground thread (window.rs Window: mouse_position,
// mouse_hit_test, modifiers, capslock, default_prevented,
// last_input_modality, captured_hitbox).
type windowInputState struct {
	// mousePosition is the tracked mouse position in window-local
	// logical pixels (the pin tracks it instead of querying the
	// platform, which is main-thread only).
	mousePosition Point
	// mouseHitTest is the last computed hit test.
	mouseHitTest hitTest
	// modifiers is the tracked modifier state.
	modifiers Modifiers
	// defaultPrevented is the current dispatch's prevent-default flag
	// (window.rs default_prevented).
	defaultPrevented bool
	// lastInputModality suppresses hover during keyboard navigation.
	lastInputModality inputModality
	// lastInputWasKeyboard caches the keyboard modality answer
	// (Window::last_input_was_keyboard).
	lastInputWasKeyboard bool
	// capturedHitbox is the hitbox that captured the pointer, or 0
	// (window.rs captured_hitbox; the capture setter arrives with the
	// mouse input ticket, the auto-release rule is wired here).
	capturedHitbox uint64
	// nextHitboxID assigns hitbox identities (window.rs
	// next_hitbox_id).
	nextHitboxID uint64
}

// inputState returns (creating when absent) the window's input state.
// Foreground thread only.
func inputState(w *Window) *windowInputState {
	if w == nil {
		panic("gpui: input state requires a live window")
	}
	if w.input == nil {
		w.input = &windowInputState{mousePosition: Point{X: -1, Y: -1}}
	}
	return w.input
}

// ---------------------------------------------------------------------------
// Frame-scoped registrations (window.rs on_mouse_event / insert_hitbox)
// ---------------------------------------------------------------------------

// mouseListener is one frame-scoped mouse listener (window.rs
// AnyMouseListener): it receives every dispatched mouse-family event
// (type-asserted by the registration closure) in both phases.
type mouseListener func(event any, phase DispatchPhase, w *Window, app *App)

// OnMouseEvent registers one mouse listener on the frame under
// construction (window.rs Window::on_mouse_event). Listeners run on
// the completed frame: capture phase in registration order
// (outside-in) then bubble phase in reverse (inside-out); a listener
// stops the traversal with StopPropagation.
//
// event is one of *MouseMoveEvent, *MouseDownEvent, *MouseUpEvent,
// *ScrollWheelEvent, *PinchEvent or *FileDropEvent (the file-drop
// Exited/Ended variants ride the mouse-event path because the pin's
// FileDropEvent implements MouseEvent — interactive.rs:767).
func (w *Window) OnMouseEvent(f func(event any, phase DispatchPhase, w *Window, app *App)) {
	if w == nil {
		panic("gpui: OnMouseEvent requires a live window")
	}
	if f == nil {
		panic("gpui: OnMouseEvent requires a non-nil listener")
	}
	fs := focusState(w)
	if fs.next == nil {
		panic("gpui: OnMouseEvent requires a frame under construction (prepaint/paint)")
	}
	fs.next.mouseListeners = append(fs.next.mouseListeners, f)
}

// InsertHitbox inserts a hitbox for the frame under construction
// (window.rs Window::insert_hitbox): the bounds plus the content mask
// active at insertion. The returned hitbox's hover answers read the
// window's tracked mouse state at dispatch time.
func (w *Window) InsertHitbox(bounds Bounds, behavior HitboxBehavior) Hitbox {
	if w == nil {
		panic("gpui: InsertHitbox requires a live window")
	}
	fs := focusState(w)
	if fs.next == nil {
		panic("gpui: InsertHitbox requires a frame under construction (prepaint)")
	}
	is := inputState(w)
	is.nextHitboxID++
	box := Hitbox{
		id:          is.nextHitboxID,
		Bounds:      bounds,
		contentMask: currentContentMask(w),
		behavior:    behavior,
	}
	fs.next.hitboxes = append(fs.next.hitboxes, &hitboxRecord{box: box})
	return box
}

// currentContentMask returns the content mask active in the frame
// under construction (the frame's paint mask).
func currentContentMask(w *Window) Bounds {
	frame := currentFrame(w)
	return frame.contentMask
}

// ---------------------------------------------------------------------------
// Dispatch (window.rs dispatch_mouse_event)
// ---------------------------------------------------------------------------

// PreventDefault prevents the default action for the current
// dispatched event (window.rs Window::prevent_default): mouse-down
// focus transfer consults it.
func (w *Window) PreventDefault() {
	if w == nil {
		return
	}
	inputState(w).defaultPrevented = true
}

// DefaultPrevented reports whether the current dispatch's default
// action was prevented (window.rs Window::default_prevented).
func (w *Window) DefaultPrevented() bool {
	if w == nil {
		return false
	}
	return inputState(w).defaultPrevented
}

// TrackedMousePosition returns the dispatch-side mouse position in
// window-local logical pixels (window.rs Window::mouse_position): the
// position tracked from dispatched events, not a live platform query
// (Window.MousePosition is the platform query).
func (w *Window) TrackedMousePosition() Point {
	if w == nil {
		return Point{}
	}
	return inputState(w).mousePosition
}

// dispatchMouseEvent is the port of window.rs dispatch_mouse_event:
// recompute the hit test (recording the change like the pin, whose
// cursor-style reset arrives with the cursor ticket), then run the
// rendered frame's mouse listeners — capture in registration order,
// bubble in reverse — stopping at propagation breaks, and finish with
// the active-drag bookkeeping (refresh on move, cancel on up) and the
// pointer-capture auto-release on mouse up.
func (w *Window) dispatchMouseEvent(event any, app *App) {
	// Inspector picking replaces the normal mouse dispatch while active
	// (window.rs dispatch_mouse_event 6088-6092: handle_inspector_mouse
	// _event first; all other mouse handling is skipped when it reports
	// picking). Wired by the orchestrator at the slice's recorded seam
	// (inspector.go's InspectorDispatchMouseEvent).
	if InspectorDispatchMouseEvent(w, app, event) {
		return
	}
	fs := focusState(w)
	if fs.rendered == nil {
		// No completed frame: nothing is listening (the pin dispatches
		// against the rendered frame's listener list).
		return
	}
	is := inputState(w)
	if hit := fs.rendered.hitTestAt(is.mousePosition); !hit.equal(&is.mouseHitTest) {
		is.mouseHitTest = hit
		// reset_cursor_style (window.rs) arrives with the cursor input
		// ticket; the change is still recorded so hover answers flip.
	}

	listeners := append([]mouseListener(nil), fs.rendered.mouseListeners...)

	// Capture phase, events bubble from back to front: registration
	// order is parents-first, so capture runs outside-in.
	for _, listener := range listeners {
		listener(event, DispatchCapture, w, app)
		if !app.propagateEvent {
			break
		}
	}

	// Bubble phase, where most normal handlers do their work.
	if app.propagateEvent {
		for i := len(listeners) - 1; i >= 0; i-- {
			listeners[i](event, DispatchBubble, w, app)
			if !app.propagateEvent {
				break
			}
		}
	}

	// The active drag follows the mouse: a move refreshes the window so
	// the drag preview can track the cursor; an up ends the drag (a
	// drop listener's StopDrag ran first when a drop landed).
	if app.HasActiveDrag() {
		if _, isMove := event.(*MouseMoveEvent); isMove {
			w.requestRefresh()
		} else if _, isUp := event.(*MouseUpEvent); isUp {
			app.cancelActiveDrag()
			w.requestRefresh()
		}
	}

	// Auto-release pointer capture on mouse up.
	if _, isUp := event.(*MouseUpEvent); isUp && is.capturedHitbox != 0 {
		is.capturedHitbox = 0
	}
}

// hitTestForDiagnostics returns the ordered hitbox ids under the
// tracked mouse position for diagnostics and tests (window.rs
// Window::mouse_hit_test).
func (w *Window) hitTestForDiagnostics() []uint64 {
	if w == nil {
		return nil
	}
	is := inputState(w)
	return append([]uint64(nil), is.mouseHitTest.orderedIDs...)
}
