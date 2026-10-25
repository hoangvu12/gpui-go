package base

// Port of U:resizable/resize_handle.rs: the resize handle element.

import (
	"github.com/hoangvu12/gpui-go/gpui"
)

const (
	// handlePadding is HANDLE_PADDING (U:resizable/resize_handle.rs:12).
	handlePadding float32 = 4
	// handleSize is HANDLE_SIZE (U:resizable/resize_handle.rs:13).
	handleSize float32 = 1
)

// ResizeHandleRenderer draws the visible part of a resize handle
// (U:resizable/resize_handle.rs:24-29). Returning false keeps the built-in
// line, so a renderer can override some handles and leave the rest alone.
type ResizeHandleRenderer func(handle *ResizeHandleContext, w *gpui.Window, app *gpui.App) (gpui.AnyElement, bool)

// ResizeHandleContext is what a ResizeHandleRenderer is told about the
// handle it is drawing (U:resizable/resize_handle.rs:31-39). The hit area,
// the cursor and the drag stay with the handle.
type ResizeHandleContext struct {
	axis  gpui.Axis
	edge  *HandleEdge
	state ResizeHandleState
}

// Axis is the axis the handle resizes along: horizontal for a vertical
// divider between two side-by-side panels
// (U:resizable/resize_handle.rs:42-46).
func (c *ResizeHandleContext) Axis() gpui.Axis { return c.axis }

// Edge is the edge of its container this handle hugs; false for a handle
// straddling the boundary it resizes (U:resizable/resize_handle.rs:48-56).
func (c *ResizeHandleContext) Edge() (HandleEdge, bool) {
	if c.edge == nil {
		return 0, false
	}
	return *c.edge, true
}

// IsActive reports whether the pointer currently owns this handle
// (U:resizable/resize_handle.rs:58-61).
func (c *ResizeHandleContext) IsActive() bool { return c.state.IsActive() }

// State is how far the pointer has gone with this handle
// (U:resizable/resize_handle.rs:63-66).
func (c *ResizeHandleContext) State() ResizeHandleState { return c.state }

// ResizeHandleState is how far the pointer has gone with a resize handle
// (U:resizable/resize_handle.rs:69-86). A drag takes the pointer out of the
// handle's band within a pixel or two, so hover cannot stand in for
// Dragging; Base tracks the progression instead.
type ResizeHandleState uint8

const (
	// ResizeHandleIdle: the pointer is somewhere else.
	ResizeHandleIdle ResizeHandleState = iota
	// ResizeHandleHovered: the pointer is over the handle's band.
	ResizeHandleHovered
	// ResizeHandlePressed: the pointer went down on the handle and has not
	// moved since.
	ResizeHandlePressed
	// ResizeHandleDragging: the handle is being dragged.
	ResizeHandleDragging
)

// String names the state (the derived Debug).
func (s ResizeHandleState) String() string {
	switch s {
	case ResizeHandleIdle:
		return "Idle"
	case ResizeHandleHovered:
		return "Hovered"
	case ResizeHandlePressed:
		return "Pressed"
	case ResizeHandleDragging:
		return "Dragging"
	}
	return "ResizeHandleState(?)"
}

// IsActive reports whether the pointer owns the handle: pressed on it, or
// dragging it (U:resizable/resize_handle.rs:88-93).
func (s ResizeHandleState) IsActive() bool {
	return s == ResizeHandlePressed || s == ResizeHandleDragging
}

// HandleEdge is which edge of its own container a handle hugs
// (U:resizable/resize_handle.rs:95-125). A handle with no edge straddles
// the boundary it resizes; naming the edge moves the whole band inside the
// container, with the hairline on the container's outermost pixel.
type HandleEdge uint8

const (
	// HandleEdgeLeading is where the axis starts: the left edge of a
	// horizontal handle's container, the top edge of a vertical one's.
	HandleEdgeLeading HandleEdge = iota
	// HandleEdgeTrailing is where the axis ends.
	HandleEdgeTrailing
)

// String names the edge (the derived Debug).
func (e HandleEdge) String() string {
	if e == HandleEdgeLeading {
		return "Leading"
	}
	return "Trailing"
}

// ResizeHandle is a resize handle for a resizable panel
// (U:resizable/resize_handle.rs:127-135, #[doc(hidden)]). T is the drag
// payload; the upstream drag view type E is erased to gpui.View.
type ResizeHandle[T any] struct {
	id         gpui.ElementID
	axis       gpui.Axis
	dragValue  *T
	edge       *HandleEdge
	onDrag     func(value *T, position gpui.Point, w *gpui.Window, app *gpui.App) gpui.View
	appearance ResizeHandleRenderer
}

// NewResizeHandle creates a resize handle (U:resizable/resize_handle.rs:
// 15-22 resize_handle, 137-148).
func NewResizeHandle[T any, I gpui.IntoElementID](id I, axis gpui.Axis) *ResizeHandle[T] {
	return &ResizeHandle[T]{id: gpui.ElementIDOf(id), axis: axis}
}

// WithAppearance hands the painted part of this handle to appearance
// (U:resizable/resize_handle.rs:150-154).
func (h *ResizeHandle[T]) WithAppearance(appearance ResizeHandleRenderer) *ResizeHandle[T] {
	h.appearance = appearance
	return h
}

// OnDrag makes the handle a drag source for value
// (U:resizable/resize_handle.rs:156-167).
func (h *ResizeHandle[T]) OnDrag(value T, f func(value *T, position gpui.Point, w *gpui.Window, app *gpui.App) gpui.View) *ResizeHandle[T] {
	h.dragValue = &value
	h.onDrag = f
	return h
}

// Inside keeps the whole handle inside its container, hugging edge,
// instead of straddling the boundary it resizes
// (U:resizable/resize_handle.rs:169-174).
func (h *ResizeHandle[T]) Inside(edge HandleEdge) *ResizeHandle[T] {
	h.edge = &edge
	return h
}

// IntoElement implements gpui.IntoElement (U:resizable/resize_handle.rs:
// 203-208).
func (h *ResizeHandle[T]) IntoElement() gpui.AnyElement {
	return gpui.CustomElement[gpui.AnyElement, gpui.Hitbox](h)
}

// sharedHandleState is one handle's ResizeHandleState, shared between the
// element and the mouse listeners it registers
// (U:resizable/resize_handle.rs:177-201). The pointer is load-bearing: a
// listener holding a copy of a bare value would write its progress into
// that copy and the stored state would never leave Idle.
type sharedHandleState struct {
	state *ResizeHandleState
}

func newSharedHandleState() sharedHandleState {
	return sharedHandleState{state: new(ResizeHandleState)}
}

// get returns the state.
func (s sharedHandleState) get() ResizeHandleState { return *s.state }

// set stores state and reports whether it changed, so a listener repaints
// the window only when there is something new to paint.
func (s sharedHandleState) set(state ResizeHandleState) bool {
	changed := *s.state != state
	*s.state = state
	return changed
}

// sharedHandleStateOf unwraps retained element state (unwrap_or_default).
func sharedHandleStateOf(state *sharedHandleState) sharedHandleState {
	if state == nil || state.state == nil {
		return newSharedHandleState()
	}
	return *state
}

// ID implements gpui.Element (U:resizable/resize_handle.rs:216-218).
func (h *ResizeHandle[T]) ID() (gpui.ElementID, bool) { return h.id, true }

// SourceLocation implements gpui.Element.
func (h *ResizeHandle[T]) SourceLocation() *gpui.SourceLocation { return nil }

// RequestLayout builds and lays out the handle's band
// (U:resizable/resize_handle.rs:224-346).
func (h *ResizeHandle[T]) RequestLayout(global *gpui.GlobalElementID, _ *gpui.InspectorElementID, w *gpui.Window, app *gpui.App) (gpui.LayoutID, gpui.AnyElement) {
	negOffset := -handlePadding
	axis, edge := h.axis, h.edge
	// Sizes are border-box: the extent has to name the whole band, padding
	// included, or the content box resolves to zero and the hairline
	// overflows into the padding.
	hugExtent := handleSize + handlePadding
	straddleExtent := handleSize + handlePadding*2

	type layout struct {
		id gpui.LayoutID
		el gpui.AnyElement
	}
	out := gpui.WithElementState(w, global, func(retained *sharedHandleState, w *gpui.Window) (layout, sharedHandleState) {
		state := sharedHandleStateOf(retained)
		bgColor := handleColor(activeTheme(app), state.get().IsActive())

		d := gpui.Div().Flex().ID(h.id).Occlude().Absolute().FlexShrink0().Group("handle")
		if h.onDrag != nil {
			onDrag := h.onDrag
			d.OnDrag(*h.dragValue, func(value *T, position gpui.Point, w *gpui.Window, app *gpui.App) gpui.View {
				return onDrag(value, position, w, app)
			})
		}
		switch {
		// Hugging an edge: the whole band is inside the container, padded
		// on the inner side only, so the hairline is the container's
		// outermost pixel.
		case edge != nil && *edge == HandleEdgeTrailing && axis == gpui.AxisHorizontal:
			d.CursorColResize().Top0().Right0().HFull().W(gpui.Px(hugExtent)).Pl(gpui.Px(handlePadding))
		case edge != nil && *edge == HandleEdgeLeading && axis == gpui.AxisHorizontal:
			d.CursorColResize().Top0().Left0().HFull().W(gpui.Px(hugExtent)).Pr(gpui.Px(handlePadding))
		case edge != nil && *edge == HandleEdgeTrailing && axis == gpui.AxisVertical:
			d.CursorRowResize().Bottom0().Left0().WFull().H(gpui.Px(hugExtent)).Pt(gpui.Px(handlePadding))
		case edge != nil && *edge == HandleEdgeLeading && axis == gpui.AxisVertical:
			d.CursorRowResize().Top0().Left0().WFull().H(gpui.Px(hugExtent)).Pb(gpui.Px(handlePadding))
		// Straddling the boundary: half the band on either side.
		case axis == gpui.AxisHorizontal:
			d.CursorColResize().Top0().Left(gpui.Px(negOffset)).HFull().W(gpui.Px(straddleExtent)).Px(gpui.Px(handlePadding))
		default:
			d.CursorRowResize().Top(gpui.Px(negOffset)).Left0().WFull().H(gpui.Px(straddleExtent)).Py(gpui.Px(handlePadding))
		}

		// A renderer that declines, or is absent, leaves the built-in line.
		// What it paints stays in tree order, under the container's own
		// mask.
		var child gpui.AnyElement
		drawn := false
		if h.appearance != nil {
			child, drawn = h.appearance(&ResizeHandleContext{axis: axis, edge: edge, state: state.get()}, w, app)
		}
		if !drawn {
			// The line fills the handle's content box exactly, so it has
			// nothing to give: a shrinkable child collapses with it.
			line := gpui.Div().Flex().FlexNone().Bg(bgColor).GroupHover("handle", gpui.Styles().Bg(bgColor))
			if AxisIsHorizontal(axis) {
				line.HFull().W(gpui.Px(handleSize))
			}
			if AxisIsVertical(axis) {
				line.WFull().H(gpui.Px(handleSize))
			}
			child = line.IntoElement()
		}
		el := d.Child(child).IntoElement()
		return layout{id: el.RequestLayout(w, app), el: el}, state
	})
	return out.id, out.el
}

// Prepaint prepaints the band, then inserts the handle's own hitbox on top
// of it (U:resizable/resize_handle.rs:348-365). After the child,
// deliberately: the band occludes, and a hit test stops at the first
// hitbox that does.
func (h *ResizeHandle[T]) Prepaint(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, bounds gpui.Bounds, layout *gpui.AnyElement, w *gpui.Window, app *gpui.App) gpui.Hitbox {
	layout.Prepaint(w, app)
	return w.InsertHitbox(bounds, gpui.HitboxNormal)
}

// Paint paints the band and registers the state listeners
// (U:resizable/resize_handle.rs:367-449). Hovered and pressed are answered
// by the hitbox, not the bounds, so an occluding element over the handle
// answers for it.
func (h *ResizeHandle[T]) Paint(global *gpui.GlobalElementID, _ *gpui.InspectorElementID, _ gpui.Bounds, layout *gpui.AnyElement, hitbox *gpui.Hitbox, w *gpui.Window, app *gpui.App) {
	layout.Paint(w, app)
	box := *hitbox

	gpui.WithElementState(w, global, func(retained *sharedHandleState, w *gpui.Window) (struct{}, sharedHandleState) {
		state := sharedHandleStateOf(retained)

		w.OnMouseEvent(func(event any, phase gpui.DispatchPhase, w *gpui.Window, _ *gpui.App) {
			if _, ok := event.(*gpui.MouseDownEvent); !ok {
				return
			}
			if phase == gpui.DispatchBubble && box.IsHovered(w) && state.set(ResizeHandlePressed) {
				w.Refresh()
			}
		})

		w.OnMouseEvent(func(event any, phase gpui.DispatchPhase, w *gpui.Window, _ *gpui.App) {
			if _, ok := event.(*gpui.MouseMoveEvent); !ok || phase != gpui.DispatchBubble {
				return
			}
			// A press that moves is a drag, and stays one until the button
			// comes back up.
			var next ResizeHandleState
			switch {
			case state.get().IsActive():
				next = ResizeHandleDragging
			case box.IsHovered(w):
				next = ResizeHandleHovered
			default:
				next = ResizeHandleIdle
			}
			if state.set(next) {
				w.Refresh()
			}
		})

		w.OnMouseEvent(func(event any, _ gpui.DispatchPhase, w *gpui.Window, _ *gpui.App) {
			if _, ok := event.(*gpui.MouseUpEvent); !ok || !state.get().IsActive() {
				return
			}
			// Releasing over the handle leaves it hovered.
			next := ResizeHandleIdle
			if box.IsHovered(w) {
				next = ResizeHandleHovered
			}
			if state.set(next) {
				w.Refresh()
			}
		})
		return struct{}{}, state
	})
}

// handleColor is what a resize handle paints, given the active theme
// (U:resizable/resize_handle.rs:451-467): projected colors win, else the
// border token at rest and the ring token while active.
func handleColor(theme Theme, active bool) gpui.Hsla {
	if active {
		if c, ok := theme.Resizable.ActiveHandle.Get(); ok {
			return c
		}
		return theme.Tokens.Colors.Ring
	}
	if c, ok := theme.Resizable.Handle.Get(); ok {
		return c
	}
	return theme.Tokens.Colors.Border
}
