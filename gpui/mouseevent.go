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
// Base ticket 06 completes the element side: pointer capture
// (window.rs:3098-3110 capture_pointer / release_pointer /
// captured_hitbox, interact.go), click synthesis, hover/active styling
// and the cursor-style reset on a hit-test change (window.rs:6083-6086
// → reset_cursor_style, window.rs:5684-5693). Mouse-pressure, touch,
// long-press and mouse-exited events belong to the mouse/touch input
// tickets.

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

// Dragging reports whether the left mouse button is held during the
// move (interactive.rs:513-518 MouseMoveEvent::dragging).
func (e MouseMoveEvent) Dragging() bool {
	return e.PressedButton != nil && *e.PressedButton == MouseButtonLeft
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
// ScrollDelta): exact pixels or inexact lines — plus the port's page
// granularity (browser-core ticket 02, a deliberate divergence toward
// the browser under D0): the Windows wheel's WHEEL_PAGESCROLL sentinel
// carries one page per notch the way Chromium's kScrollByPage events do
// (ui/events/blink/web_input_event_builders_win.cc:312-318), and the
// consuming scroller resolves pages against its own visible size
// (blink ScrollableArea::ResolveScrollDelta → PageStep,
// scrollable_area.cc:213-229).
type ScrollDelta struct {
	// Pixels is the exact pixel delta (the Pixels variant).
	Pixels Point
	// Lines is the inexact line-based delta (the Lines variant).
	Lines Point
	// Pages is the page-granularity delta (the Pages variant), in pages
	// per notch like Chromium's kScrollByPage delta.
	Pages Point
	// IsPixels reports the active variant (ScrollDelta's default is
	// Lines; Direct Manipulation always reports Pixels).
	IsPixels bool
	// IsPages reports the page-granularity variant (browser-core
	// ticket 02). At most one of IsPixels and IsPages is set; the
	// constructors keep the invariant.
	IsPages bool
}

// ScrollDeltaPixels builds the Pixels variant.
func ScrollDeltaPixels(dx, dy float32) ScrollDelta {
	return ScrollDelta{Pixels: Point{X: dx, Y: dy}, IsPixels: true}
}

// ScrollDeltaLines builds the Lines variant.
func ScrollDeltaLines(dx, dy float32) ScrollDelta {
	return ScrollDelta{Lines: Point{X: dx, Y: dy}}
}

// ScrollDeltaPages builds the Pages variant (browser-core ticket 02):
// the delta is whole pages, one per wheel notch under
// WHEEL_PAGESCROLL.
func ScrollDeltaPages(dx, dy float32) ScrollDelta {
	return ScrollDelta{Pages: Point{X: dx, Y: dy}, IsPages: true}
}

// Precise reports whether this is an exact pixel delta
// (interactive.rs:605-611 ScrollDelta::precise).
func (d ScrollDelta) Precise() bool { return d.IsPixels }

// PixelDelta converts the delta into exact pixels: pixel deltas pass
// through, line deltas scale by lineHeight (interactive.rs:613-619
// ScrollDelta::pixel_delta). Page deltas need the scroller's visible
// size and are NOT converted here — see PixelDeltaFor; a page delta
// resolves to the zero delta through this method.
func (d ScrollDelta) PixelDelta(lineHeight float32) Point {
	if d.IsPixels {
		return d.Pixels
	}
	if d.IsPages {
		return Point{}
	}
	return Point{X: lineHeight * d.Lines.X, Y: lineHeight * d.Lines.Y}
}

// PixelDeltaFor converts the delta into exact pixels for a scroller
// with the given line height and visible size (browser-core ticket 02):
// pixel and line deltas resolve exactly like PixelDelta, while page
// deltas scale by Chromium's page step over the scroller's own visible
// length — max(0.875×length, length−40), floored at 1
// (cc/input/scroll_utils.cc:51-56 CalculatePageStep: 87.5px on a 100px
// scroller, so consecutive page notches overlap slightly instead of
// skipping content).
func (d ScrollDelta) PixelDeltaFor(lineHeight float32, viewport Size) Point {
	if d.IsPages {
		return Point{
			X: d.Pages.X * ScrollPageStep(viewport.Width),
			Y: d.Pages.Y * ScrollPageStep(viewport.Height),
		}
	}
	return d.PixelDelta(lineHeight)
}

// ScrollPageStep is Chromium's page step for one page-granularity unit
// over a scroller whose visible length is length (cc/input/scroll_utils.cc
// 51-56 CalculatePageStep with kMinFractionToStepWhenPaging 0.875 and
// kMaxOverlapBetweenPages 40, scroll_utils.h:22-24): the step leaves a
// small overlap so paging never skips content, and is at least one
// pixel so a degenerate scroller still moves.
func ScrollPageStep(length float32) float32 {
	step := length * 0.875
	if length-40 > step {
		step = length - 40
	}
	if step < 1 {
		step = 1
	}
	return step
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

// HitboxID is a hitbox identity (window.rs:867 HitboxId; Hitbox's
// public `id` field). Pointer capture and group hover track hitboxes by
// identity.
type HitboxID uint64

// ID returns the hitbox identity (window.rs Hitbox::id).
func (h *Hitbox) ID() HitboxID { return HitboxID(h.id) }

// IsHovered reports whether the hitbox with this identity is hovered
// (window.rs:887-897 HitboxId::is_hovered, without the pin's keyboard
// suppression): true when it captured the pointer, otherwise the
// window's last hit-test answer. Browser-core ticket 10 (spec D12:
// hover survives typing) removed the early-false on the keyboard
// modality — Chromium's :hover never clears on key input (the only
// keyboard hook into hover styling is :focus-visible, which reads the
// modality through FocusVisible instead) — a deliberate divergence
// toward the browser under D0.
func (id HitboxID) IsHovered(w *Window) bool {
	if w == nil {
		return false
	}
	is := inputState(w)
	if is.capturedHitbox == uint64(id) {
		return true
	}
	return is.mouseHitTest.contains(uint64(id))
}

// isHoveredIgnoringLastInput is window.rs:902-908
// HitboxId::is_hovered_ignoring_last_input. After browser-core
// ticket 10 it agrees with IsHovered (the keyboard suppression it
// ignored is gone); it stays as the cursor resolution's named seam.
func (id HitboxID) isHoveredIgnoringLastInput(w *Window) bool {
	if w == nil {
		return false
	}
	is := inputState(w)
	if is.capturedHitbox == uint64(id) {
		return true
	}
	return is.mouseHitTest.contains(uint64(id))
}

// IsHovered reports whether this hitbox is currently hovered: true
// when this hitbox captured the pointer, and otherwise the hit-test
// answer (Hitbox::is_hovered). Browser-core ticket 10 removed the
// pin's keyboard suppression (spec D12: hover survives typing); see
// HitboxID.IsHovered.
func (h *Hitbox) IsHovered(w *Window) bool { return HitboxID(h.id).IsHovered(w) }

// IsHoveredIgnoringLastInput is IsHovered without the keyboard
// suppression (HitboxId::is_hovered_ignoring_last_input).
func (h *Hitbox) IsHoveredIgnoringLastInput(w *Window) bool {
	return HitboxID(h.id).isHoveredIgnoringLastInput(w)
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
	// lastInputModality drives the focus-visible styles (the only
	// hover-adjacent consumer left after browser-core ticket 10 removed
	// the hover suppression, spec D12).
	lastInputModality inputModality
	// lastInputWasKeyboard caches the keyboard modality answer
	// (Window::last_input_was_keyboard).
	lastInputWasKeyboard bool
	// capturedHitbox is the hitbox that captured the pointer, or 0
	// (window.rs:1330 captured_hitbox; set by Window.CapturePointer,
	// interact.go, and auto-released on mouse up here).
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

// dispatchMouseEvent is the port of window.rs:6081-6136
// dispatch_mouse_event: recompute the hit test and, when it changed,
// reset the platform cursor from the rendered frame's cursor requests
// (window.rs:6083-6086), then run the rendered frame's mouse listeners —
// capture in registration order, bubble in reverse — stopping at
// propagation breaks, and finish with the active-drag bookkeeping
// (refresh on move, cancel on up) and the pointer-capture auto-release
// on mouse up.
func (w *Window) dispatchMouseEvent(event any, app *App) {
	fs := focusState(w)
	is := inputState(w)
	if fs.rendered != nil {
		if hit := fs.rendered.hitTestAt(is.mousePosition); !hit.equal(&is.mouseHitTest) {
			is.mouseHitTest = hit
			w.resetCursorStyle(app)
		}
	}
	// Inspector picking replaces the normal mouse dispatch while active
	// (window.rs:6088-6092: handle_inspector_mouse_event after the hit
	// test; all other mouse handling is skipped when it reports
	// picking). Wired by the orchestrator at the slice's recorded seam
	// (inspector.go's InspectorDispatchMouseEvent).
	if InspectorDispatchMouseEvent(w, app, event) {
		return
	}
	// The pointer boundary events (browser-core ticket 10): a move (or
	// the window exit, whose translation already moved the tracked
	// position off window so the hit test above came up empty) fires
	// the enter/leave sweeps before the move's own listeners, matching
	// Chromium's order - the boundary events precede the move
	// (pointer_event_manager.cc:1353-1365, boundary_event_dispatcher.
	// cc:190-214).
	switch e := event.(type) {
	case *MouseMoveEvent:
		w.dispatchHoverBoundaryEvents(e.Position, e.Modifiers, app)
	case *MouseExitEvent:
		w.dispatchHoverBoundaryEvents(e.Position, e.Modifiers, app)
	}
	if fs.rendered == nil {
		// No completed frame: nothing is listening (the pin dispatches
		// against the rendered frame's listener list).
		return
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
