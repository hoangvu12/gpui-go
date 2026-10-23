package base

// A wheel-catching mask over a scroll viewport (U:scrollable_mask.rs).
//
// Go adaptations (recorded deviations):
//
//   - Default display (map gap 2b): the mask is absolutely positioned
//     without insets and covers its parent by moving up by its own height
//     (U:scrollable_mask.rs:166-175). That relies on the absolute box's
//     static position following the preceding in-flow sibling, which is
//     block layout — the default display of the gpui-ce 0.2.2 line Base
//     targets. The core pin defaults a div to Flex, where the static
//     position is the container start and the mask would cover the area
//     above its parent. horizontalScrollArea therefore sets Block on its
//     wrapper explicitly, and a caller placing a mask next to its scroller
//     must give the shared parent Block display the same way.
//   - ElementId::CodeLocation (U:scrollable_mask.rs:15-21) is "file:line".

import (
	"fmt"

	"github.com/hoangvu12/gpui-go/gpui"
)

// yellow is R:color.rs:108-110 yellow() (hue 60°, the core's Hsla hue is
// a turn fraction); the core exports no counterpart.
func yellow() gpui.Hsla { return gpui.Hsla{H: 60.0 / 360.0, S: 1, L: 0.5, A: 1} }

// transparentWhite is R:color.rs:78-80 transparent_white(); the core
// exports no counterpart.
func transparentWhite() gpui.Hsla { return gpui.Hsla{H: 0, S: 0, L: 1, A: 0} }

// horizontalScrollArea is a horizontal scroll viewport that only consumes
// horizontal wheel deltas (U:scrollable_mask.rs:23-54, crate-private).
//
// GPUI's native overflow_x_scroll maps vertical wheel input onto
// horizontal scrolling when there is no vertical overflow; this wrapper
// keeps the clipping and the scroll offset while delegating wheel input
// to a ScrollableMask, so vertical wheel events keep bubbling to the
// parent scroller. The mask is a sibling of the scrolled element, not a
// child: children prepaint under the scroll offset, which would slide the
// mask away from the viewport.
//
// Deviation (gap 2b): the wrapper and the scrolled div set Block display,
// the pinned 0.2.2 default: the mask's placement relies on it, and under
// the core's Flex default the wider child would shrink to the viewport
// (flex_shrink 1) and leave nothing to scroll. The caller's style still
// refines the scrolled div after it.
func horizontalScrollArea[I gpui.IntoElementID](id I, scrollHandle gpui.ScrollHandle, style *gpui.StyleRefinement, child any) *gpui.DivElement {
	elementID := gpui.ElementIDOf(id)
	return gpui.Div().
		Block().
		WFull().
		Relative().
		Child(StyledExtOf(gpui.Div().
			ID(elementID).
			Block().
			WFull()).
			RefineStyle(style).
			OverflowHidden().
			TrackScroll(scrollHandle).
			Child(child)).
		Child(newScrollableMaskAt(gpui.AxisHorizontal, scrollHandle, callerElementID(1)).ID(elementID))
}

// ScrollableMask covers its parent and moves a scroll handle on one axis
// from the mouse wheel (U:scrollable_mask.rs:56-81). Only one axis is
// handled.
//
// Axis-dominant wheel events are consumed in the capture phase, so the
// mask wins over ancestor scrollers (e.g. a gpui.List) that register
// their listeners after their children; events dominated by the other
// axis keep propagating. The mask stays inert while occluded.
//
// Dominance is decided per gesture: a precise (trackpad) delta locks onto
// the axis its gesture started on, so mid-swipe wobble cannot flip which
// mask consumes the events; line deltas keep the per-event comparison.
//
// At the scroll edge the axes differ, matching platform scrollers: a
// vertical mask hands the event to the ancestor scroller (CSS
// overscroll-behavior: auto chaining), while a horizontal mask keeps
// consuming it — a bubbled horizontal delta would be mapped onto a
// vertical-only ancestor by gpui's own wheel listener.
type ScrollableMask struct {
	axis         gpui.Axis
	id           gpui.ElementID
	scrollHandle ScrollbarHandle
	debug        gpui.Optional[gpui.Hsla]
}

// NewScrollableMask creates a mask over handle (any value
// ScrollbarHandleOf accepts) for axis; its id is the call site, so two
// masks built in different places never share their gesture axis lock
// (U:scrollable_mask.rs:83-93).
func NewScrollableMask[H any](axis gpui.Axis, handle H) *ScrollableMask {
	return newScrollableMaskAt(axis, handle, callerElementID(2))
}

func newScrollableMaskAt[H any](axis gpui.Axis, handle H, id gpui.ElementID) *ScrollableMask {
	return &ScrollableMask{scrollHandle: ScrollbarHandleOf(handle), axis: axis, id: id}
}

// ID sets a specific element id instead of the call site; only needed
// when one call site creates several masks of the same axis
// (U:scrollable_mask.rs:95-102).
func (m *ScrollableMask) ID[I gpui.IntoElementID](id I) *ScrollableMask {
	m.id = gpui.ElementIDOf(id)
	return m
}

// Debug enables the debug border showing the mask bounds
// (U:scrollable_mask.rs:104-109).
func (m *ScrollableMask) Debug() *ScrollableMask {
	m.debug = gpui.Some(yellow())
	return m
}

// IntoElement converts the mask into its element
// (U:scrollable_mask.rs:112-118).
func (m *ScrollableMask) IntoElement() gpui.AnyElement {
	return gpui.CustomElement[struct{}, gpui.Hitbox](scrollableMaskElement{m})
}

// scrollableMaskElement is the Element impl of ScrollableMask
// (U:scrollable_mask.rs:120-297).
type scrollableMaskElement struct{ m *ScrollableMask }

// ID keeps the gesture's axis lock across frames; the axis suffix keeps
// both masks of one scroller apart when they share an id
// (U:scrollable_mask.rs:124-133: `(id, axis).into()`, a NamedChild).
func (e scrollableMaskElement) ID() (gpui.ElementID, bool) {
	axis := "vertical"
	if e.m.axis == gpui.AxisHorizontal {
		axis = "horizontal"
	}
	return gpui.NamedChildElementID(e.m.id, axis), true
}

// SourceLocation implements gpui.Element (U:scrollable_mask.rs:135-137).
func (e scrollableMaskElement) SourceLocation() *gpui.SourceLocation { return nil }

// RequestLayout takes the parent's size as an absolutely positioned layer
// (U:scrollable_mask.rs:139-155).
func (e scrollableMaskElement) RequestLayout(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, w *gpui.Window, _ *gpui.App) (gpui.LayoutID, struct{}) {
	style := gpui.DefaultStyle()
	style.Position = gpui.PositionAbsolute
	style.FlexGrow = 1
	style.FlexShrink = 1
	style.Size.Width = gpui.DefiniteLengthOf(gpui.Fraction(1))
	style.Size.Height = gpui.DefiniteLengthOf(gpui.Fraction(1))
	id, err := gpui.RequestElementLayout(w, style)
	if err != nil {
		panic(fmt.Sprintf("base: ScrollableMask request_layout: %v", err))
	}
	return id, struct{}{}
}

// Prepaint moves up by its height to cover the parent view and inserts the
// hitbox there (U:scrollable_mask.rs:157-176).
func (e scrollableMaskElement) Prepaint(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, bounds gpui.Bounds, _ *struct{}, w *gpui.Window, _ *gpui.App) gpui.Hitbox {
	cover := gpui.Bounds{
		Origin: gpui.Point{X: bounds.Origin.X, Y: bounds.Origin.Y - bounds.Size.Height},
		Size:   bounds.Size,
	}
	return w.InsertHitbox(cover, gpui.HitboxNormal)
}

// maskOngoingScroll is the mask's retained gesture lock
// (U:scrollable_mask.rs:191-198, Rc<RefCell<OngoingScroll>>).
type maskOngoingScroll struct{ scroll *gpui.OngoingScroll }

// Paint registers the capture-phase wheel listener
// (U:scrollable_mask.rs:178-296).
func (e scrollableMaskElement) Paint(global *gpui.GlobalElementID, _ *gpui.InspectorElementID, _ gpui.Bounds, _ *struct{}, hitbox *gpui.Hitbox, w *gpui.Window, _ *gpui.App) {
	m := e.m
	isHorizontal := m.axis == gpui.AxisHorizontal
	lineHeight := w.LineHeight()
	bounds := hitbox.Bounds
	ongoing := &gpui.OngoingScroll{}
	if global != nil {
		ongoing = gpui.WithElementState(w, global, func(slot *maskOngoingScroll, _ *gpui.Window) (*gpui.OngoingScroll, maskOngoingScroll) {
			if slot == nil || slot.scroll == nil {
				fresh := maskOngoingScroll{scroll: &gpui.OngoingScroll{}}
				return fresh.scroll, fresh
			}
			return slot.scroll, *slot
		})
	}

	gpui.WithContentMaskVoid(w, &bounds, func(w *gpui.Window) {
		if color, ok := m.debug.Get(); ok {
			w.PaintQuad(gpui.PaintQuad{
				Bounds:             bounds,
				BorderWidths:       gpui.Edges{Top: 1, Right: 1, Bottom: 1, Left: 1},
				BorderColor:        gpui.SolidBackground(color),
				Background:         gpui.SolidBackground(transparentWhite()),
				BorderDashedLength: gpui.DefaultBorderDashedLength,
				BorderDashedGap:    gpui.DefaultBorderDashedGap,
			})
		}

		viewID := currentView(w)
		scrollHandle := m.scrollHandle
		box := *hitbox
		w.OnMouseEvent(func(event any, phase gpui.DispatchPhase, w *gpui.Window, app *gpui.App) {
			ev, ok := event.(*gpui.ScrollWheelEvent)
			if !ok {
				return
			}
			// Handle in the capture phase: ancestor scrollers such as a
			// list register their wheel listeners after their children
			// paint, so in the bubble phase they run first and would
			// consume the vertical component of a trackpad swipe.
			// should_handle_scroll (not a raw bounds check) keeps the mask
			// inert when it is occluded.
			if !(phase == gpui.DispatchCapture && box.ShouldHandleScroll(w)) {
				return
			}

			offset := scrollHandle.Offset()
			// Pages resolve against the mask's own visible size
			// (browser-core ticket 02; Chromium resolves page granularity at
			// the scroller, blink ScrollableArea::ResolveScrollDelta).
			delta := ev.Delta.PixelDeltaFor(lineHeight, box.Bounds.Size)

			// Lock the gesture to the axis it started on; line deltas
			// carry no touch phase.
			if ev.Delta.Precise() {
				OngoingScrollLockAxis(ongoing, &delta, ev.TouchPhase, app)
			}

			// Only one axis scrolls at a time: with both deltas, the
			// larger one wins.
			if delta.X != 0 && delta.Y != 0 {
				if abs32(delta.X) > abs32(delta.Y) {
					delta.Y = 0
				} else {
					delta.X = 0
				}
			}

			if !isHorizontal {
				// Clamp the current offset too: after a bubbled event the
				// scrolled element's own listener pushes the shared offset
				// past the edge unclamped (the div clamps at prepaint), and
				// that transient overscroll would read as room to scroll.
				// ScrollbarHandle has no max_offset; recover it from
				// content_size = viewport + max_offset.
				axisMax := max(scrollHandle.ContentSize().Height-scrollHandle.ViewportBounds().Size.Height, 0)
				current := clamp32(offset.Y, -axisMax, 0)
				newOffset := clamp32(current+delta.Y, -axisMax, 0)
				if newOffset == current {
					// At the edge or no overflow: bubble to the parent.
					return
				}
				offset.Y = newOffset
				scrollHandle.SetOffset(offset)
				app.Notify(viewID)
				app.StopPropagation()
				return
			}

			offset.X += delta.X

			// SetOffset does not clamp (the div clamps at prepaint), so
			// any non-zero horizontal-dominant delta passes this guard —
			// even at the edge the event is consumed rather than turned
			// into a parent scroll.
			if offset != scrollHandle.Offset() {
				scrollHandle.SetOffset(offset)
				app.Notify(viewID)
				app.StopPropagation()
			}
		})
	})
}
