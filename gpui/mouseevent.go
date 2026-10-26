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
// suppression): the window's last hit-test answer, which browser-core
// ticket 11 retargets while an element holds the pointer capture (the
// capture chain, Chromium's GetEffectiveTargetForPointerEvent:
// pointer_event_manager.cc:232-239) — so the captured element and its
// ancestors report hovered wherever the pointer is, and every other
// element does not. An id-only capture (the hitbox's element had no
// id) keeps the pin's exact-id early-true. Browser-core ticket 10
// (spec D12: hover survives typing) removed the early-false on the
// keyboard modality — Chromium's :hover never clears on key input (the
// only keyboard hook into hover styling is :focus-visible, which
// reads the modality through FocusVisible instead) — a deliberate
// divergence toward the browser under D0.
func (id HitboxID) IsHovered(w *Window) bool {
	if w == nil {
		return false
	}
	is := inputState(w)
	if is.activePointerCapture.hitboxID == uint64(id) && is.activePointerCapture.path == "" {
		// Pin compat (window.rs:889-891): the pin's captured hitbox is
		// always hovered. A resolved capture answers from the retargeted
		// hit test instead.
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
	if is.activePointerCapture.hitboxID == uint64(id) && is.activePointerCapture.path == "" {
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
// Frame::hitboxes entries). Browser-core ticket 11 added the capture
// retargeting's ancestry: parent is the enclosing hitbox at insertion
// (the frame's hitbox stack, pushed while the hitbox's subtree
// prepaints), and ownerPath is the owning element's GlobalElementID
// key — both stable identities for the hitbox within the frame, so a
// pointer capture set in one frame can retarget the hit test in the
// next. Browser-core ticket 14 added the inert flag: the hitbox of an
// inert element (or an element inside one) is skipped by the hit test
// — the port's pointer-events: none.
type hitboxRecord struct {
	// box is the hitbox value (id, bounds, mask, behavior).
	box Hitbox
	// parent is the hitbox whose subtree prepainted this one, or nil at
	// the tree top: the capture chain walks it (the DOM propagation
	// path of the captured element's events).
	parent *hitboxRecord
	// ownerPath is the owning element's GlobalElementID key (the
	// element-id stack at insertion; an id-less element shares its
	// nearest id'd ancestor's path, so only id'd elements own a
	// distinguishing path).
	ownerPath string
	// inert marks the hitbox of an inert subtree (browser-core ticket
	// 14): the hit test skips it — no hover, no press, no click, no
	// cursor — like Chromium's UsedPointerEvents kNone (inert →
	// pointer-events: none, computed_style.h:1862-1867, so
	// VisibleToHitTesting fails, computed_style.h:2052-2056).
	inert bool
	// focusHandle is the focus handle of the hitbox's owning element
	// (browser-core ticket 14): the mouse-focus default's ancestor walk
	// reads it (defaultMouseFocus — the flat-tree walk Chromium's
	// HandleMouseFocus runs, mouse_event_manager.cc:551-559), which the
	// hit test's occlusion semantics must not block. Nil for elements
	// that track no focus or are suppressed (hidden — IsMouseFocusable
	// requires visibility, computed_style.h:1894-1896).
	focusHandle *FocusHandle
	// hidden marks the hitbox of a hidden subtree (own
	// visibility:hidden or a hidden ancestor): the mouse-focus default's
	// ancestor walk skips it (browser-core ticket 14 — a hidden element
	// is not mouse-focusable), while the hit test still answers it (the
	// pin's shape: hidden hitboxes stay in the hit test).
	hidden bool
	// clipPath is the owning element's clip-path (browser-core ticket
	// 57): the hit test answers point-in-shape against it — the
	// clip-path shapes pointer events (css-masking-1), so a position
	// inside the bounds but outside the shape misses the element and
	// passes through to whatever is behind. Nil (the common case) keeps
	// the plain bounds+mask test.
	clipPath *ClipPath
}

// hitboxOf returns the frame's hitbox record for a hit-test id, or nil.
func (f *renderedFrame) hitboxOf(id uint64) *hitboxRecord {
	if f == nil {
		return nil
	}
	for _, record := range f.hitboxes {
		if record.box.id == id {
			return record
		}
	}
	return nil
}

// contains is Hitbox::contains (with the clip-path's point-in-shape
// test when the owning element clips its shape, browser-core ticket
// 57).
func (r *hitboxRecord) contains(point Point) bool {
	if !r.box.Contains(point) {
		return false
	}
	if r.clipPath == nil {
		return true
	}
	// The shape is element-local: the reference box is the hitbox's own
	// bounds (the element's border box).
	local := Point{X: point.X - r.box.Bounds.Origin.X, Y: point.Y - r.box.Bounds.Origin.Y}
	return r.clipPath.Contains(local, r.box.Bounds.Size)
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
// blocking starts (window.rs HitTest::new). Browser-core ticket 14:
// an inert hitbox is skipped entirely — it neither contains nor
// occludes (pointer-events: none — the hit passes through to whatever
// is behind, computed_style.h:1862-1867/2052-2056).
func (f *renderedFrame) hitTestAt(position Point) hitTest {
	numUntilMouseBlocked := -1
	var ids []uint64
	foundHitOcclusion := false
	for i := len(f.hitboxes) - 1; i >= 0; i-- {
		record := f.hitboxes[i]
		if record.inert {
			continue
		}
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
// The click chain (browser-core ticket 12, spec D12)
// ---------------------------------------------------------------------------

// chainPathsOf resolves a hit test's mouse path to element paths,
// innermost first, deduplicated: each hit-test id maps to the frame's
// hitbox record, whose ownerPath is the owning (or nearest id'd
// ancestor) element's global id key. Id-less in-between elements
// share their nearest id'd ancestor's path, so consecutive equal
// paths collapse — the chain is the sequence of distinct element
// identities under the pointer, the DOM propagation path's stand-in
// (the flat-tree walk of event_handling_util.cc:88-90
// ParentForClickEvent). Only the ids that support mouse count: a
// block-mouse hitbox (an inert or occluding element) ends the
// propagation path — the elements behind it never see the click, the
// same way they never see hover (hitTest.contains,
// window.rs:887-897's mouse-path half).
func chainPathsOf(frame *renderedFrame, test hitTest) []string {
	if frame == nil {
		return nil
	}
	count := test.hoverHitboxCount
	if count > len(test.orderedIDs) {
		count = len(test.orderedIDs)
	}
	var paths []string
	seen := map[string]bool{}
	for _, id := range test.orderedIDs[:count] {
		var path string
		for _, record := range frame.hitboxes {
			if record.box.id == id {
				path = record.ownerPath
				break
			}
		}
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		paths = append(paths, path)
	}
	return paths
}

// commonAncestorClickChain computes the click's target chain from the
// press and release chains (browser-core ticket 12): the deepest
// element on both chains — Chromium's click target,
// mouse_event_manager.cc:358-367 `mouse_release_target->CommonAncestor(
// *mousedown_element_, ParentForClickEvent)` — plus its ancestors, so
// the click bubbles from the common ancestor to the root. The release
// chain is walked innermost-first and the first path the press chain
// also contains is the common ancestor: press on a and release on b
// (siblings under p) yields [p, root], press and release on the same
// element yields that element's whole chain. An empty answer (nothing
// shared, or an empty release chain — the pointer left every hitbox)
// is no click, like Chromium's null click target.
func commonAncestorClickChain(frame *renderedFrame, pressPaths []string, release hitTest) []string {
	if len(pressPaths) == 0 {
		return nil
	}
	releasePaths := chainPathsOf(frame, release)
	press := map[string]bool{}
	for _, path := range pressPaths {
		press[path] = true
	}
	for i, path := range releasePaths {
		if press[path] {
			return releasePaths[i:]
		}
	}
	return nil
}

// appendClickListenerRegistration registers one element's click and
// aux-click listeners on the frame under construction (browser-core
// ticket 12): the up's click dispatch resolves its target chain by the
// registration paths. Registered during paint, so the completed frame
// carries them.
func appendClickListenerRegistration(w *Window, registration clickListenerRegistration) {
	fs := focusState(w)
	if fs.next == nil {
		return
	}
	fs.next.clickListeners = append(fs.next.clickListeners, registration)
}

// armMouseClick records one element on the press's armed chain
// (browser-core ticket 12): its mousedown-bubble arm ran while
// hovered — the element saw the press. The arms run inside-out, so
// the chain is innermost-first like a propagation path; a stopped
// mousedown (an inert element's stopPropagation, Base's disabled
// controls) or a block-mouse overlay keeps the later ancestors from
// arming, which keeps them out of the click's target set — the pin's
// per-element pending semantics, carried into the window-level
// dispatch. The up's dispatchMouseClick computes the common-ancestor
// target from this chain.
func armMouseClick(w *Window, path string) {
	is := inputState(w)
	is.pressClickChain = append(is.pressClickChain, path)
}

// dispatchMouseClick synthesizes and dispatches the click of a mouse
// up (browser-core ticket 12, spec D12): after the up's listeners and
// the pointer-capture release, before the boundary settling — the
// DOM's order (pointer_event_manager.cc:1277-1296:
// ReleasePointerCapture → ProcessPendingPointerCapture →
// DispatchMouseClickIfNeeded, with the boundary events settling after
// it at :1299-1313). The click's target chain is the captured
// element's chain when a pointer capture was active at the up
// (:1237-1241 captured_click_target — GetEffectiveTargetForPointer-
// Event before the release), and otherwise the common ancestor of
// the press and release chains (mouse_event_manager.cc:358-367). The
// listeners fire target-first then ancestor order, like a bubbling
// DOM click; skipDrag suppresses the click of an up that ended a drag
// or dropped one (Chromium's skip_click_dispatch — a drag's release
// never clicks).
func (w *Window) dispatchMouseClick(up *MouseUpEvent, capturedChain []string, skipDrag bool, app *App) {
	fs := focusState(w)
	is := inputState(w)
	down := is.pendingMouseClick
	is.pendingMouseClick = nil
	pressChain := is.pressClickChain
	is.pressClickChain = nil
	if skipDrag || app.HasActiveDrag() {
		// A drag's release does not click (the drag consumed the press;
		// a drop listener may also have stopped the drag mid-bubble, in
		// which case HasActiveDrag already reports false and the
		// caller's flag covers it).
		return
	}
	if fs.rendered == nil || down == nil {
		return
	}
	// The popover light dismiss (browser-core ticket 21): Chromium runs
	// HandlePopoverLightDismissForClick while creating the click event,
	// right before the listeners (mouse_event_manager.cc:262-267),
	// comparing the popovers related to the press and the release
	// targets — the pair rule that keeps a drag from inside a popover
	// from dismissing it. It runs for every click dispatch, including a
	// click on empty space (the DOM's body-targeted click, which the
	// port's empty click chain stands for), which is what closes every
	// popover on an outside click. Keyboard-activation clicks never come
	// through here, matching Chromium's null pointer targets.
	w.lightDismissFromClick(app)
	// The dialog light dismiss (browser-core ticket 23): the same
	// pre-click spot, right after the popover walk
	// (HandleDialogLightDismissForClick, mouse_event_manager.cc:262-267)
	// — a click outside the topmost closedby=any modal requests its
	// close (modal.go lightDismissModalFromClick).
	w.lightDismissModalFromClick(app)
	chain := capturedChain
	if chain == nil {
		chain = commonAncestorClickChain(fs.rendered, pressChain, is.mouseHitTest)
	}
	if len(chain) == 0 {
		return
	}
	pressed := *down
	click := ClickEvent{Kind: ClickEventMouse, Mouse: MouseClickEvent{Down: pressed, Up: *up}}
	registrations := fs.rendered.clickListeners
	for _, path := range chain {
		for i := range registrations {
			if registrations[i].path != path {
				continue
			}
			if pressed.Button == MouseButtonLeft {
				for _, listener := range registrations[i].click {
					listener(&click, w, app)
				}
			} else {
				for _, listener := range registrations[i].auxClick {
					listener(&click, w, app)
				}
			}
		}
	}
	w.requestRefresh()
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

// focusOriginKind is the origin of the last user focus move
// (browser-core ticket 12, spec D12 — Chromium's mojom::blink::FocusType
// behind document.cc:6068-6070 SetLastFocusType): mouse (the mouse-down
// focus transfer), keyboard (the Tab default action) or unset (no user
// focus yet, or the last one came from script, which Chromium does not
// track — kScript/kNone keep the previous origin). The :focus-visible
// rule reads it: a mouse focus shows no ring until an unmodified key
// arrives, a keyboard focus shows it (selector_checker.cc:3888-3901's
// !last_focus_from_mouse).
type focusOriginKind uint8

const (
	focusOriginUnset focusOriginKind = iota
	focusOriginMouse
	focusOriginKeyboard
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
	// lastInputModality is the last input's modality (window.rs
	// InputModality). After browser-core ticket 12 its only remaining
	// style consumer is gone (hover no longer reads it since ticket 10,
	// focus-visible reads the origin and keyboard-event flag below); it
	// stays tracked for window.rs parity.
	lastInputModality inputModality
	// lastInputWasKeyboard caches the keyboard modality answer
	// (Window::last_input_was_keyboard).
	lastInputWasKeyboard bool
	// focusOrigin is the origin of the last user focus move
	// (browser-core ticket 12; see focusOriginKind).
	focusOrigin focusOriginKind
	// hadKeyboardEvent is Chromium's document.had_keyboard_event_
	// (document.h:1152-1155, the :focus-visible flag): an unmodified
	// keydown sets it (keyboard_event_manager.cc:354-357 — Ctrl, Alt and
	// Meta exclude the key, Shift does not: Ctrl+S after a click is not
	// keyboard focus) and a mouse-type focus clears it
	// (element.cc:8867-8868, through focusWithOrigin).
	hadKeyboardEvent bool
	// focusTransferClaimed is the mousedown focus transfer's
	// innermost-wins signal (browser-core ticket 12): the innermost
	// focusable element's arm sets it so a focusable ancestor's arm
	// skips. It replaces the pin's PreventDefault call (interact.go,
	// div.rs:3279's comment), which reported a default-prevented
	// mousedown Chrome never delivers — Chrome's mouse-focus runs after
	// the listeners as their default action and leaves the event
	// unprevented (event_handler.cc:998-1001 → mouse_event_manager.cc
	// HandleMouseFocus :537-636). Reset at the head of every dispatch.
	focusTransferClaimed bool
	// pendingMouseClick is the press the up's click dispatch pairs with
	// (browser-core ticket 12): recorded at the mousedown with
	// pressClickChain, dropped by a drag start or a pointer cancel, and
	// consumed by dispatchMouseClick at the up.
	pendingMouseClick *MouseDownEvent
	// pressClickChain is the press hit-test chain's element paths
	// (innermost first, deduplicated — hitbox ownerPaths): the click
	// target is the common ancestor of this chain and the release chain
	// (mouse_event_manager.cc:358-367 CommonAncestor).
	pressClickChain []string
	// pressHitPaths is the press's own hit-test chain paths
	// (browser-core ticket 21): the pointer-down target's ancestor
	// walk, recorded at the down dispatch, that the click's light
	// dismiss pairs with the release's chain — Chromium's
	// pointer_down_target (mouse_event_manager.cc:265).
	pressHitPaths []string
	// pendingKeyboardActivation is the focus generation at an
	// unmodified Space keydown (browser-core ticket 12: the armed
	// activation — the keyup clicks only when the generation still
	// matches, so a focus move or an intervening key disarms it).
	pendingKeyboardActivation *uint64
	// capturedHitbox is replaced by activePointerCapture (browser-core
	// ticket 11): the pin's captured_hitbox (window.rs:1330) survives as
	// its hitboxID half, with the element path that keeps the capture
	// alive across frames.
	//
	// activePointerCapture is the effective pointer capture (the spec's
	// pointer capture target override), pendingPointerCapture the
	// requested one (the pending override: equal values mean no pending
	// change, the zero value a pending release), and
	// pointerButtonsPressed the tracked held buttons (the
	// dispatch-side twin of the host's mouseButtonsHeld; Chromium's
	// IsActiveButtonsState).
	activePointerCapture  pointerCapture
	pendingPointerCapture pointerCapture
	pointerButtonsPressed uint8
	// nextHitboxID assigns hitbox identities (window.rs
	// next_hitbox_id).
	nextHitboxID uint64
	// wheelLatch is the latched scroller of the current wheel gesture
	// (browser-core ticket 15, wheel_scroll.go): one gesture scrolls
	// one scroller, the innermost that can consume the gesture's first
	// delta; the latch expires with the 500ms latching transaction on
	// the application's clock.
	wheelLatch *wheelScrollLatch
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
// window's tracked mouse state at dispatch time. The record keeps the
// enclosing hitbox and the owning element's id path (browser-core
// ticket 11's capture chain); the caller wraps its subtree's prepaint
// in withHitboxSubtree so descendants record this hitbox as their
// parent (div.go's prepaint and the list element).
func (w *Window) InsertHitbox(bounds Bounds, behavior HitboxBehavior) Hitbox {
	return w.insertHitboxRecord(bounds, behavior).box
}

// insertHitboxRecord is InsertHitbox's record-returning core: the
// element prepaints use the record to wrap their subtree
// (withHitboxSubtree).
func (w *Window) insertHitboxRecord(bounds Bounds, behavior HitboxBehavior) *hitboxRecord {
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
	record := &hitboxRecord{
		box:       box,
		parent:    hitboxStackTop(fs.next),
		ownerPath: (&GlobalElementID{Path: currentElementIDs(w)}).key(),
	}
	fs.next.hitboxes = append(fs.next.hitboxes, record)
	return record
}

// insertHitboxRecord is InsertHitbox's record-returning core: the
// element prepaints use the record to wrap their subtree
// (withHitboxSubtree). insertInertHitboxRecord is the inert-marked
// form (browser-core ticket 14): an inert element or an element under
// one inserts its hitbox through this entry so the hit test skips it.
func (w *Window) insertInertHitboxRecord(bounds Bounds, behavior HitboxBehavior, inert bool) *hitboxRecord {
	record := w.insertHitboxRecord(bounds, behavior)
	if record != nil && inert {
		record.inert = true
	}
	return record
}

// withHitboxSubtree runs f with record on the frame's hitbox stack, so
// the hitboxes f's subtree inserts record record as their parent (the
// capture chain's ancestry, browser-core ticket 11). A nil record (the
// element inserted no hitbox) is a plain call.
func (w *Window) withHitboxSubtree(record *hitboxRecord, f func()) {
	if record == nil {
		f()
		return
	}
	fs := focusState(w)
	fs.next.hitboxStack = append(fs.next.hitboxStack, record)
	defer func() { fs.next.hitboxStack = fs.next.hitboxStack[:len(fs.next.hitboxStack)-1] }()
	f()
}

// hitboxStackTop returns the frame's innermost hitbox whose subtree is
// prepainting.
func hitboxStackTop(frame *renderedFrame) *hitboxRecord {
	if frame == nil || len(frame.hitboxStack) == 0 {
		return nil
	}
	return frame.hitboxStack[len(frame.hitboxStack)-1]
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
// dispatch_mouse_event, extended by browser-core ticket 11 (spec D12)
// with the pointer-capture rules Chromium's PointerEventManager
// implements (pointer_event_manager.cc, cited inline): recompute the
// effective hit test — positional, or the capture chain while an
// element holds the pointer capture — and, when it changed, reset the
// platform cursor from the rendered frame's cursor requests, then run
// the rendered frame's mouse listeners — capture in registration
// order, bubble in reverse — stopping at propagation breaks, and
// finish with the active-drag bookkeeping (refresh on move, cancel on
// up) and the pointer-capture release on mouse up (the up tail).
func (w *Window) dispatchMouseEvent(event any, app *App) {
	fs := focusState(w)
	is := inputState(w)

	// A move with no button held releases the capture before the move's
	// own processing (pointer_event_manager.cc:1144-1149: "This is for
	// when the mouse is released outside of the page"). The tracked
	// held-button set answers "no button held" (the event's own pressed
	// button, when set, counts as held too).
	if move, isMove := event.(*MouseMoveEvent); isMove &&
		is.activePointerCapture != (pointerCapture{}) &&
		is.pointerButtonsPressed == 0 && move.PressedButton == nil {
		is.pendingPointerCapture = (pointerCapture{})
		w.processPendingPointerCapture(event, app)
	}

	// The pending pointer capture processes before every event, before
	// the boundary events and the event itself (the spec's process
	// pending pointer capture steps; Chromium's
	// ProcessCaptureAndPositionOfPointerEvent runs them first, :1357).
	w.processPendingPointerCapture(event, app)

	// The effective hit test: positional at the tracked mouse, or the
	// capture chain (GetEffectiveTargetForPointerEvent :232-239 — the
	// event target, and the element under the pointer, is the capture
	// target while captured).
	if fs.rendered != nil {
		if hit := w.currentHitTest(fs.rendered); !hit.equal(&is.mouseHitTest) {
			is.mouseHitTest = hit
			w.resetCursorStyle(app)
		}
	}
	// The press records its event (browser-core ticket 12): the up's
	// click dispatch pairs this press with the release. The press CHAIN
	// is not recorded here — it is the armed chain (the elements whose
	// mousedown-bubble arms ran, interact.go armMouseClick: innermost-
	// first, exactly the elements that saw the press, so a stopped
	// mousedown or a block-mouse overlay leaves the un-armed ancestors
	// out of the click's target set, like the pin's per-element pending
	// state).
	if down, isDown := event.(*MouseDownEvent); isDown {
		pressed := *down
		is.pendingMouseClick = &pressed
		is.pressClickChain = nil
		// The press's own hit chain (browser-core ticket 21): the
		// pointer-down target's ancestor walk — the light-dismiss pair's
		// down half, mirroring Chromium's pointer_down_target
		// (mouse_event_manager.cc:262-267).
		if fs.rendered != nil {
			is.pressHitPaths = chainPathsOf(fs.rendered, is.mouseHitTest)
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
	// cc:190-214). While a capture is active the sweeps run against the
	// retargeted hit test, so the captured chain reports no transitions
	// as the pointer moves outside it - exactly Chrome's boundary
	// behaviour under capture (SetElementUnderPointer tracks the capture
	// target, :1362).
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

	// Browser-core ticket 16: the drag tail's up/cancel arms need the
	// drag state as it was when the dispatch began — a drop listener's
	// StopDrag runs during the bubble traversal below, and the DOM
	// drop/dragend still fire after it (PerformDragAndDrop runs after
	// the up's listeners). The move arm re-reads the state after the
	// traversal (the drag starts mid-bubble).
	dragBeforeListeners := app.HasActiveDrag()

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
	// the drag preview can track the cursor; an up or a pointer cancel
	// ends the drag (a drop listener's StopDrag ran first when a drop
	// landed). Browser-core ticket 11: the cancel arm mirrors
	// Chromium's pointercancel, which ends the drag when the stream is
	// suppressed (:1316-1321). Browser-core ticket 16: the moves drive
	// the DOM drag sequence (drag, dragenter, dragleave, dragover, in
	// Chromium's order — event_handler.cc:1375-1478) and the up ends it
	// (drop on an accepting target, then dragend with the final
	// effect; PerformDragAndDrop + DragSourceEndedAt); the cancels
	// fire dragleave and dragend with no drop (CancelDragAndDrop). The
	// move arm consults the drag state AFTER the traversal (the drag
	// starts mid-bubble — its first update is this move's tail), while
	// the up/cancel arms need it as it was BEFORE the traversal (a
	// drop listener's StopDrag runs mid-bubble and the DOM drop/dragend
	// still fire after it).
	dragAtDispatch := app.HasActiveDrag()
	if dragAtDispatch || dragBeforeListeners {
		switch e := event.(type) {
		case *MouseMoveEvent:
			if dragAtDispatch {
				w.updateDragSession(e, app)
			}
			w.requestRefresh()
		case *MouseUpEvent:
			if dragBeforeListeners {
				w.endDragSessionFromUp(e, app)
				app.cancelActiveDrag()
			}
			w.requestRefresh()
		case *PointerCancelEvent:
			if dragBeforeListeners {
				w.cancelDragSession(app)
				app.cancelActiveDrag()
			}
			w.requestRefresh()
		}
	}

	// The mouse-focus default (browser-core ticket 14, spec D4's "a press
	// on empty space blurs"): Chromium runs HandleMouseFocus after the
	// mousedown listeners, only when the event was not default-prevented
	// (event_handler.cc:998-1001: `if (event_result ==
	// kNotHandled) HandleMouseFocus(...)`), and it moves focus to the
	// nearest mouse-focusable ancestor of the press target — or to the
	// document body when the chain has none, blurring the focused
	// element (mouse_event_manager.cc:551-559's flat-tree walk, :615-621's
	// SetFocusedElement(nullptr) "even with !element"). The port's focus
	// transfer is the innermost focusable element's bubble-phase arm
	// (interact.go, which claims focusTransferClaimed); this tail is the
	// walk's other half — the focusable ANCESTORS the hit test's
	// occlusion hides (a popup's BlockMouse eats the host's hover but
	// not the DOM walk), and the blur when nothing focusable contains
	// the press. A prevented mousedown suppresses both halves
	// (kNotHandled's meaning).
	if down, isDown := event.(*MouseDownEvent); isDown && !w.DefaultPrevented() && !is.focusTransferClaimed {
		w.defaultMouseFocus(down, app)
	}

	// The pointerup tail (browser-core ticket 11, re-ordered by
	// browser-core ticket 12 to Chromium's pointer_event_manager.cc:
	// 1272-1313): the implicit capture release — "immediately after
	// firing the pointerup … clear the pending pointer capture target
	// override … and then run process pending pointer capture steps to
	// fire lostpointercapture if necessary" (spec) — then the click
	// (DispatchMouseClickIfNeeded :1283-1296, after lostpointercapture,
	// before the boundary settling — the pin fired its click inside
	// the up's listener traversal, before mouseup listeners higher up
	// and before lostpointercapture; the recorded divergence is closed
	// by spec D12), then the release hit test and the boundary events
	// (Chromium re-hit-tests on the release, :1268-1281, and settles the
	// element under the pointer after the click, :1299-1313, which is
	// where the captured element's pointerleave and the new target's
	// pointerenter come from). The click's release chain is the
	// effective hit test at the up — the pre-release capture chain when
	// captured (Chromium computes click_mouse_target and
	// captured_click_target before releasing, :1231-1241).
	if up, isUp := event.(*MouseUpEvent); isUp {
		hadCapture := is.activePointerCapture != (pointerCapture{})
		var capturedChain []string
		if hadCapture {
			capturedChain = chainPathsOf(fs.rendered, is.mouseHitTest)
			is.pendingPointerCapture = (pointerCapture{})
			w.processPendingPointerCapture(up, app)
		}
		w.dispatchMouseClick(up, capturedChain, dragAtDispatch, app)
		if hadCapture {
			if fs.rendered != nil {
				if hit := fs.rendered.hitTestAt(is.mousePosition); !hit.equal(&is.mouseHitTest) {
					is.mouseHitTest = hit
					w.resetCursorStyle(app)
				}
			}
			w.dispatchHoverBoundaryEvents(up.Position, up.Modifiers, app)
		}
		// The contextmenu trigger (browser-core ticket 17, contextmenu.
		// go dispatchContextMenuFromMouse): Chromium's right-button up
		// dispatches the contextmenu event after the whole up tail —
		// pointerup, lostpointercapture, click/auxclick, the boundary
		// settling — regardless of the click having been swallowed
		// (WebFrameWidgetImpl::HandleMouseUp, web_frame_widget_impl.cc:
		// 1253-1260, the Windows context_menu_on_mouse_up default,
		// web_preferences.h:64).
		w.dispatchContextMenuFromMouse(up, app)
	}

	// The pointercancel tail: the release fires lostpointercapture but
	// no boundary events ("Don't send boundary events in this case as it
	// is a little tricky", pointer_event_manager.cc:1316-1321); the
	// pending presses were cleared by the listener arms above, and the
	// hit test reverts to positional at the next dispatch or paint.
	// Browser-core ticket 12: a cancelled press never releases, so the
	// pending click goes with it (Chromium's pointercancel ends the
	// click sequence too).
	if _, isCancel := event.(*PointerCancelEvent); isCancel {
		is.pendingMouseClick = nil
		is.pressClickChain = nil
		is.pressHitPaths = nil
		is.pendingKeyboardActivation = nil
		if is.activePointerCapture != (pointerCapture{}) {
			is.pendingPointerCapture = (pointerCapture{})
			w.processPendingPointerCapture(event, app)
		}
	}
	// The overlay toggle notifications drain at the input task's end
	// (browser-core ticket 21): the DOM queues popover toggle events as
	// tasks, so they land after the whole dispatch's listeners —
	// after the click's own entries in the transcript, exactly where
	// the recorder reads them (toplayer.go drainOverlayToggles).
	w.drainOverlayToggles(app)
}

// defaultMouseFocus is the mousedown's focus default when no focusable
// element claimed the transfer (browser-core ticket 14, spec D4): the
// flat-tree ancestor walk of Chromium's MouseEventManager::HandleMouseFocus
// (mouse_event_manager.cc:551-559) — from the press's innermost hitbox up
// the enclosing-hitbox chain (the DOM's ancestor chain), the first
// focus-tracking element takes the mouse focus; nothing focusable on the
// chain blurs the window (the body equivalent, :615-621's
// SetFocusedElement(nullptr) "even with !element in order to blur current
// focus element when a link is clicked"). The walk ignores the hit test's
// occlusion (BlockMouse hides hover, not the DOM ancestor walk) and skips
// inert elements (an inert ancestor is not mouse-focusable; the walk
// continues above it — IsMouseFocusable's inert gate, computed_style.h:
// 1894-1896).
func (w *Window) defaultMouseFocus(down *MouseDownEvent, app *App) {
	if w == nil {
		return
	}
	fs := focusState(w)
	if !fs.focusEnabled {
		return
	}
	is := inputState(w)
	if fs.rendered != nil && len(is.mouseHitTest.orderedIDs) > 0 {
		record := fs.rendered.hitboxOf(is.mouseHitTest.orderedIDs[0])
		for ; record != nil; record = record.parent {
			if record.inert || record.hidden || record.focusHandle == nil {
				continue
			}
			w.focusWithOrigin(*record.focusHandle, focusOriginMouse)
			return
		}
	}
	w.blurWithOrigin(focusOriginMouse)
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
