package gpui

// This file is the scroll slice of the pinned Interactivity (Base ticket
// 07), shared by the div (div.go) and the uniform list (list.go):
//
//   - the overflow-scroll / restrict-scroll / track-scroll builders of
//     StatefulInteractiveElement (R:elements/div.rs:1746-1777) and
//     Interactivity::set_scroll_offset (R:div.rs:2642-2667);
//   - the scroll-offset resolution at request layout: a tracked handle's
//     shared offset cell, else the element state's (R:div.rs:2735-2755);
//   - clamp_scroll_position at prepaint, which also writes the tracked
//     handle's overflow, max offset and bounds (R:div.rs:2913-2976);
//   - paint_scroll_listener, the default wheel listener: pixel / line
//     deltas, the restrict_scroll_to_axis gesture filter, the
//     cross-axis remap and the concurrent-scroll arbitration, applied
//     UNCLAMPED to the offset cell (R:div.rs:3816-3879) — the clamp is
//     the next prepaint's;
//   - Style::scroll_mask (R:style.rs:857-871), Window::line_height
//     (R:window.rs:2983-2985) and ScrollDelta::coalesce
//     (R:interactive.rs:621-661), which the list's wheel listener uses.
//
// R: = reference/ce-source/crates/gpui/src (pin 254b5dbd).

import "math"

// elementScroll is one element's resolved scroll slice for the frame
// (R:div.rs:2582-2585 tracked_scroll_handle / scroll_offset /
// ongoing_scroll). A nil *elementScroll is the pinned scroll_offset None:
// the element does not scroll.
type elementScroll struct {
	// offset is the shared offset cell (the handle's or the element
	// state's).
	offset *Point
	// ongoing is the shared gesture axis tracker.
	ongoing *OngoingScroll
	// handle is the tracked scroll handle, when one is tracked.
	handle *ScrollHandle
	// maxOffset is the scroll maximum per axis of the frame's clamp
	// (browser-core 09: the keyboard-scroll default action reads it —
	// the same value clamp writes to the tracked handle).
	maxOffset Point
	// visible is the scroller's visible size of the frame's clamp (the
	// page-step length of the keyboard scroll, browser-core 09).
	visible Size
	// auto is the overflow: auto element's shown-scrollbar flags
	// (browser-core 36, scrollbar.go): resolved from the same element
	// state as the offset cell, read at request time (which axes
	// reserve) and written at prepaint (what this frame's overflow
	// shows). Nil unless an axis is auto.
	auto *divAutoScrollbarFlags
}

// divScrollElementState is the scroll part of the pinned
// InteractiveElementState (R:div.rs:4182-4183 scroll_offset /
// ongoing_scroll) for an identified overflow-scroll div without a
// tracked handle. The port keys it as its own element-state type next to
// InteractiveElementState (interact.go) under the same global id.
type divScrollElementState struct {
	offset  *Point
	ongoing *OngoingScroll
	// auto holds the overflow: auto shown-scrollbar flags
	// (browser-core 36, scrollbar.go) — persistent across frames so a
	// frame's request walk reserves what the previous frame's prepaint
	// saw overflowing.
	auto *divAutoScrollbarFlags
}

// baseOverflowScrolls is the pinned base_style.overflow.x/y ==
// Some(Overflow::Scroll) test (R:div.rs:2658, 2739-2740), extended by
// browser-core 36 to the overflow: auto axes: an auto element needs
// the same scroll state (the offset cell, the gesture tracker and the
// auto flags) as a scroll one.
func baseOverflowScrolls(base *StyleRefinement) bool {
	x, okX := base.OverflowX.Get()
	y, okY := base.OverflowY.Get()
	return (okX && (x == OverflowScroll || x == OverflowAuto)) ||
		(okY && (y == OverflowScroll || y == OverflowAuto))
}

// baseOverflowAuto reports whether the base style sets overflow: auto
// on an axis (the div's auto-scrollbar flags only resolve then).
func baseOverflowAuto(base *StyleRefinement) bool {
	x, okX := base.OverflowX.Get()
	y, okY := base.OverflowY.Get()
	return (okX && x == OverflowAuto) || (okY && y == OverflowAuto)
}

// resolveElementScroll is R:div.rs:2735-2755: a tracked handle shares its
// offset and gesture cells; otherwise an identified element whose base
// style scrolls on either axis keeps them in its element state; any other
// element does not scroll. Browser-core 36: an overflow: auto axis needs
// the same cells (and the auto flags) — the wheel listener and the clamp
// then keep it honest (an auto axis with no overflow has range zero).
func resolveElementScroll(handle *ScrollHandle, base *StyleRefinement, global *GlobalElementID, w *Window) *elementScroll {
	if handle != nil {
		es := &elementScroll{offset: handle.state.offset, ongoing: handle.state.ongoingScroll, handle: handle}
		if global != nil && baseOverflowAuto(base) {
			es.auto = withAutoScrollbarFlags(global, w)
		}
		return es
	}
	if global == nil || !baseOverflowScrolls(base) {
		return nil
	}
	return WithOptionalElementState(w, global, func(slot *divScrollElementState, w *Window) (*elementScroll, *divScrollElementState) {
		if slot == nil {
			slot = &divScrollElementState{}
		}
		if slot.offset == nil {
			slot.offset = &Point{}
		}
		if slot.ongoing == nil {
			slot.ongoing = &OngoingScroll{}
		}
		if slot.auto == nil && baseOverflowAuto(base) {
			slot.auto = &divAutoScrollbarFlags{}
		}
		return &elementScroll{offset: slot.offset, ongoing: slot.ongoing, auto: slot.auto}, slot
	})
}

// withAutoScrollbarFlags resolves (creating on first use) an
// identified element's auto-scrollbar flag cell.
func withAutoScrollbarFlags(global *GlobalElementID, w *Window) *divAutoScrollbarFlags {
	return WithOptionalElementState(w, global, func(slot *divScrollElementState, w *Window) (*divAutoScrollbarFlags, *divScrollElementState) {
		if slot == nil {
			slot = &divScrollElementState{}
		}
		if slot.auto == nil {
			slot.auto = &divAutoScrollbarFlags{}
		}
		return slot.auto, slot
	})
}

// roundToTwoDecimals is clamp_scroll_position's round_to_two_decimals
// (R:div.rs:2920-2923): (pixels * 100).round() / 100 in f32.
func roundToTwoDecimals(v float32) float32 {
	scaled := v * 100
	return float32(math.Round(float64(scaled))) / 100
}

// clamp is R:div.rs:2913-2976 clamp_scroll_position with the
// browser-core 07 record-derived scroll range: the maximum is the
// caller's maxOffset — the layout engine's content size less the
// padding box, plus the in-flow children's end margins and the
// scrollbar reservation (ScrollLayoutFacts; the pin computed it from
// the element-tree children's bounds plus the style-resolved padding,
// which missed descendant overflow, trailing margins and the
// percentage basis) — clamped at zero; the offset clamps into
// [-max, 0] per axis (a pending scroll_to_bottom pins y to -max), and
// a tracked handle records the overflow, the max offset and the bounds.
// A nil receiver (no scroll) returns the zero offset.
func (s *elementScroll) clamp(bounds Bounds, style *Style, maxOffset Point, w *Window) Point {
	if s == nil {
		return Point{}
	}
	scrollToBottom := false
	if s.handle != nil {
		state := s.handle.state
		state.overflowX, state.overflowY = style.OverflowX, style.OverflowY
		scrollToBottom = state.scrollToBottom
		state.scrollToBottom = false
	}

	scrollMax := Point{
		X: max32(maxOffset.X, 0),
		Y: max32(maxOffset.Y, 0),
	}
	// browser-core 09: record the frame's clamp for the keyboard-scroll
	// default action (the handle's state keeps its own copy below).
	s.maxOffset = scrollMax
	s.visible = bounds.Size

	offset := s.offset
	offset.X = clampF32(offset.X, -scrollMax.X, 0)
	if scrollToBottom {
		offset.Y = -scrollMax.Y
	} else {
		offset.Y = clampF32(offset.Y, -scrollMax.Y, 0)
	}

	if s.handle != nil {
		s.handle.state.maxOffset = scrollMax
		s.handle.state.bounds = bounds
	}
	return *offset
}

// paintListener is R:div.rs:3816-3879 paint_scroll_listener: one bubble
// wheel listener gated on the hitbox's should_handle_scroll. The delta is
// the event's pixel delta at the window line height; under
// restrict_scroll_to_axis a precise delta passes the gesture filter; an
// axis that does not scroll takes nothing, a scrolling axis with no
// delta of its own takes the other axis's delta unless scrolling is
// restricted or the other axis scrolls too; without
// allow_concurrent_scroll only the larger axis moves. The offset cell
// moves unclamped and a change notifies the view that was rendering at
// registration. A nil receiver registers nothing. Page deltas resolve
// against the hitbox's own visible size (browser-core ticket 02).
func (s *elementScroll) paintListener(hitbox Hitbox, style *Style, w *Window) {
	if s == nil {
		return
	}
	offset := s.offset
	ongoing := s.ongoing
	overflowX, overflowY := style.OverflowX, style.OverflowY
	// browser-core 36: an auto axis wheels like a scroll one; the clamp
	// keeps an empty range from actually scrolling (overflow: auto with
	// content that fits never moves).
	scrollsX, scrollsY := overflowAxisUserScrolls(overflowX), overflowAxisUserScrolls(overflowY)
	allowConcurrent := style.AllowConcurrentScroll
	restrict := style.RestrictScrollToAxis
	lineHeight := windowLineHeight(w)
	box := hitbox
	notify := currentViewNotifier(w)
	w.OnMouseEvent(func(event any, phase DispatchPhase, w *Window, app *App) {
		e, ok := event.(*ScrollWheelEvent)
		if !ok || phase != DispatchBubble || !box.ShouldHandleScroll(w) {
			return
		}
		old := *offset
		delta := e.Delta.PixelDeltaFor(lineHeight, box.Bounds.Size)
		if restrict && e.Delta.Precise() && ongoing != nil {
			ongoing.Filter(&delta, e.TouchPhase, app)
		}

		var deltaX, deltaY float32
		switch {
		case scrollsX && delta.X != 0:
			deltaX = delta.X
		case scrollsX && !restrict && !scrollsY:
			deltaX = delta.Y
		}
		switch {
		case scrollsY && delta.Y != 0:
			deltaY = delta.Y
		case scrollsY && !restrict && !scrollsX:
			deltaY = delta.X
		}
		if !allowConcurrent && deltaX != 0 && deltaY != 0 {
			if abs32(deltaX) > abs32(deltaY) {
				deltaY = 0
			} else {
				deltaX = 0
			}
		}
		offset.Y += deltaY
		offset.X += deltaX
		if *offset != old {
			notify(w, app)
		}
	})
}

// windowLineHeight is R:window.rs:2983-2985 Window::line_height: the
// current text style's line height in pixels, rounded
// (R:style.rs:719-721 line_height_in_pixels).
func windowLineHeight(w *Window) float32 {
	style := WindowTextStyle(w)
	rem := RemSize(w)
	return float32(math.Round(float64(style.LineHeightPixels(style.FontSizePixels(rem), rem))))
}

// scrollMaskOf is R:style.rs:857-871 Style::scroll_mask: the content mask
// of a list-like scroll container — the vertical axis always clips, the
// horizontal axis follows overflow.x, inset by a visible border.
func scrollMaskOf(style *Style, bounds Bounds, rem float32) *Bounds {
	scroll := DefaultStyle()
	scroll.OverflowX = style.OverflowX
	scroll.OverflowY = OverflowScroll
	scroll.BorderColor = style.BorderColor
	scroll.BorderWidths = style.BorderWidths
	scroll.OverflowFade = style.OverflowFade
	return scroll.OverflowMask(bounds, rem)
}

// signum32 is f32::signum: 1 for +0 and positives, -1 for -0 and
// negatives, NaN for NaN.
func signum32(v float32) float32 {
	if v != v {
		return v
	}
	if math.Signbit(float64(v)) {
		return -1
	}
	return 1
}

// coalesceScrollDelta is R:interactive.rs:621-661 ScrollDelta::coalesce:
// same-variant deltas add per axis when the signs agree, otherwise the
// newer delta wins; mixed variants yield the newer delta. The port's
// Pages variant follows the same rules (browser-core ticket 02).
func coalesceScrollDelta(a, b ScrollDelta) ScrollDelta {
	kind := func(d ScrollDelta) int {
		switch {
		case d.IsPixels:
			return 1
		case d.IsPages:
			return 2
		default:
			return 0
		}
	}
	if kind(a) != kind(b) {
		return b
	}
	combine := func(x, y float32) float32 {
		if signum32(x) == signum32(y) {
			return x + y
		}
		return y
	}
	switch kind(b) {
	case 1:
		return ScrollDelta{Pixels: Point{X: combine(a.Pixels.X, b.Pixels.X), Y: combine(a.Pixels.Y, b.Pixels.Y)}, IsPixels: true}
	case 2:
		return ScrollDelta{Pages: Point{X: combine(a.Pages.X, b.Pages.X), Y: combine(a.Pages.Y, b.Pages.Y)}, IsPages: true}
	default:
		return ScrollDelta{Lines: Point{X: combine(a.Lines.X, b.Lines.X), Y: combine(a.Lines.Y, b.Lines.Y)}}
	}
}

// withElementOffsetVoid is the void form of WithElementOffset.
func withElementOffsetVoid(w *Window, offset Point, f func(*Window)) {
	WithElementOffset(w, offset, func(w *Window) struct{} {
		f(w)
		return struct{}{}
	})
}

// ---------------------------------------------------------------------------
// The div's scroll builders (R:div.rs:1746-1777, StatefulInteractiveElement)
// ---------------------------------------------------------------------------

// OverflowScroll sets the overflow on both axes to scroll
// (R:div.rs:1746-1751 overflow_scroll). The div keeps its scroll offset in
// its element state unless it tracks a handle, and scrolls with the wheel.
// The pin restricts it to stateful elements: the div needs an ID.
func (d *DivElement) OverflowScroll() *DivElement {
	style := d.StyleMut()
	style.OverflowX = Some(OverflowScroll)
	style.OverflowY = Some(OverflowScroll)
	d.scrollStateful = true
	return d
}

// OverflowXScroll sets the horizontal overflow to scroll
// (R:div.rs:1753-1757 overflow_x_scroll). Requires an ID.
func (d *DivElement) OverflowXScroll() *DivElement {
	d.StyleMut().OverflowX = Some(OverflowScroll)
	d.scrollStateful = true
	return d
}

// OverflowYScroll sets the vertical overflow to scroll
// (R:div.rs:1759-1763 overflow_y_scroll). Requires an ID.
func (d *DivElement) OverflowYScroll() *DivElement {
	d.StyleMut().OverflowY = Some(OverflowScroll)
	d.scrollStateful = true
	return d
}

// OverflowAuto sets the overflow on both axes to auto (browser-core
// 36, spec story 14): the element is a scroll container whose
// scrollbar — and the layout space it reserves — appears on an axis
// only while that axis' content overflows, resolved by the element's
// prepaint and reserved from the next frame's layout. Requires an ID
// (the auto policy keeps its shown-scrollbar flags in the element
// state, like the pin's scroll offset).
func (d *DivElement) OverflowAuto() *DivElement {
	style := d.StyleMut()
	style.OverflowX = Some(OverflowAuto)
	style.OverflowY = Some(OverflowAuto)
	d.scrollStateful = true
	return d
}

// OverflowXAuto sets the horizontal overflow to auto
// (browser-core 36). Requires an ID.
func (d *DivElement) OverflowXAuto() *DivElement {
	d.StyleMut().OverflowX = Some(OverflowAuto)
	d.scrollStateful = true
	return d
}

// OverflowYAuto sets the vertical overflow to auto
// (browser-core 36). Requires an ID.
func (d *DivElement) OverflowYAuto() *DivElement {
	d.StyleMut().OverflowY = Some(OverflowAuto)
	d.scrollStateful = true
	return d
}

// RestrictScrollToAxis restricts scrolling of this element to the axis of
// the input gesture (R:div.rs:1765-1771 restrict_scroll_to_axis; the
// style flag is R:style.rs:253-278): a wheel delta on a non-scrolling axis
// is no longer redirected to the scrolling one, and precise deltas lock
// to the gesture's dominant axis. Requires an ID.
func (d *DivElement) RestrictScrollToAxis() *DivElement {
	d.StyleMut().RestrictScrollToAxis = Some(true)
	d.scrollStateful = true
	return d
}

// TrackScroll tracks the scroll state of this element with the given
// handle (R:div.rs:1773-1777 track_scroll): the element scrolls the
// handle's offset and writes its bounds, max offset and child bounds at
// every prepaint. Requires an ID.
func (d *DivElement) TrackScroll(handle ScrollHandle) *DivElement {
	if handle.state == nil {
		panic("gpui: TrackScroll requires a handle from NewScrollHandle")
	}
	d.trackedScrollHandle = &handle
	d.scrollStateful = true
	return d
}

// SetScrollOffset assigns the current scroll offset of an identified
// element whose base style scrolls on either axis; a no-op otherwise
// (R:div.rs:2642-2667 Interactivity::set_scroll_offset). Call it during
// request layout with the element's global id.
func (d *DivElement) SetScrollOffset(global *GlobalElementID, w *Window, point Point) {
	if global == nil || !baseOverflowScrolls(d.Styled.storage()) {
		return
	}
	WithOptionalElementState(w, global, func(slot *divScrollElementState, w *Window) (struct{}, *divScrollElementState) {
		if slot == nil {
			slot = &divScrollElementState{}
		}
		if slot.offset == nil {
			slot.offset = &Point{}
		}
		*slot.offset = point
		return struct{}{}, slot
	})
}

// scrollLayoutFactsOf queries the div's own node's record-derived
// scroll facts (browser-core 07), panicking on engine failure like the
// bounds queries.
func (d *DivElement) scrollLayoutFactsOf(layout *divFrameState, w *Window) ScrollLayoutFacts {
	ctx := layoutContextOf(w)
	facts, err := layout.layoutID.engine.ScrollLayoutFacts(&ctx, layout.layoutID)
	if err != nil {
		panic("gpui: DivElement scroll facts: " + err.Error())
	}
	return facts
}

// inlineScrollMaxOf is the inline scroller's range (the standalone
// paragraph geometry the inline collector places — the layout record
// carries no inline content extent), keeping the pin's arithmetic on
// the RECORD-resolved padding (browser-core 07: the percentage basis
// follows the layout, not the scroller's own height).
func inlineScrollMaxOf(contentSize Size, padding Edges, bounds Bounds) Point {
	return Point{
		X: max32(roundToTwoDecimals(contentSize.Width+padding.Left+padding.Right-bounds.Size.Width), 0),
		Y: max32(roundToTwoDecimals(contentSize.Height+padding.Top+padding.Bottom-bounds.Size.Height), 0),
	}
}

// trackChildBounds records the direct element-tree children's layout
// bounds on the tracked handle (the R:div.rs:2394-2399 half of the old
// content-size union — the range itself comes from the layout record
// now, browser-core 07).
func (d *DivElement) trackChildBounds(childLayoutIDs []LayoutID, w *Window) {
	if d.trackedScrollHandle == nil {
		return
	}
	tracked := make([]Bounds, 0, len(childLayoutIDs))
	for _, id := range childLayoutIDs {
		child, err := layoutBoundsOf(w, id)
		if err != nil {
			panic("gpui: DivElement scroll child bounds: " + err.Error())
		}
		tracked = append(tracked, child)
	}
	d.trackedScrollHandle.state.childBounds = tracked
}

// trackInlineChildBounds is the scroll-handle half of the inline
// prepare_layout (R:elements/div/inline.rs:432-436): a tracked handle
// records every child's layout bounds.
func (d *DivElement) trackInlineChildBounds(childLayoutIDs []LayoutID, w *Window) {
	if d.trackedScrollHandle == nil {
		return
	}
	tracked := make([]Bounds, 0, len(childLayoutIDs))
	for _, id := range childLayoutIDs {
		child, err := layoutBoundsOf(w, id)
		if err != nil {
			panic("gpui: DivElement inline scroll child bounds: " + err.Error())
		}
		tracked = append(tracked, child)
	}
	d.trackedScrollHandle.state.childBounds = tracked
}
