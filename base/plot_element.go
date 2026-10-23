package base

// The element behind every Plot (U:plot/element.rs): layout, appear and
// hover tracking and the overlay the plot returns from Plot.Tooltip.
//
// Core seams (R: = reference/ce-source/crates/gpui/src):
//
//   - window.request_layout → gpui.RequestElementLayout
//     (R:window.rs:5237), with Size::full();
//   - window.with_element_state → gpui.WithElementState
//     (R:window.rs:4140) for the shared tooltip-cursor cell;
//   - window.insert_hitbox / Hitbox::is_hovered → Window.InsertHitbox
//     (R:window.rs:5426) / Hitbox.IsHovered;
//   - window.mouse_position → Window.TrackedMousePosition
//     (R:window.rs:3088; the dispatch-tracked position, not the OS query);
//   - window.on_mouse_event::<MouseMoveEvent> → Window.OnMouseEvent
//     (R:window.rs:5578) with a *gpui.MouseMoveEvent type switch;
//   - window.current_view + cx.notify(view) → gpui.CurrentView
//     (R:window.rs:5497) + App.Notify(EntityID);
//   - window.request_animation_frame → gpui.RequestAnimationFrame
//     (R:window.rs:2633);
//   - AnyElement::prepaint_as_root → AnyElement.PrepaintAsRoot.
//
// Go adaptation: `PlotElement<P>` is generic over the plot type upstream;
// the Go PlotElement holds the Plot interface. The `Rc<Cell<Option<
// Point>>>` cursor cell is a pointer to a struct in element state.

import "github.com/hoangvu12/gpui-go/gpui"

// PlotElement paints a Plot filling its container, with appear tracking
// when the plot has an ID, and hover tracking and the plot's tooltip
// overlay when it is also Interactive (U:plot/element.rs:12-29). A plot
// becomes an element through it:
//
//	func (s *Sparkline) IntoElement() gpui.AnyElement {
//		return base.NewPlotElement(s).IntoElement()
//	}
type PlotElement struct {
	plot Plot
}

// NewPlotElement wraps plot (U:plot/element.rs:32-34).
func NewPlotElement(plot Plot) *PlotElement { return &PlotElement{plot: plot} }

// plotCursorCell is the last cursor position (plot-relative), shared by
// prepaint and paint (U:plot/element.rs:36-45).
type plotCursorCell struct {
	pos gpui.Point
	set bool
}

func (c *plotCursorCell) get() (gpui.Point, bool) { return c.pos, c.set }

// tooltipCursor returns the element-state cursor cell
// (U:plot/element.rs:36-45).
func plotTooltipCursor(global *gpui.GlobalElementID, w *gpui.Window) *plotCursorCell {
	return gpui.WithElementState(w, global, func(prev **plotCursorCell, _ *gpui.Window) (*plotCursorCell, *plotCursorCell) {
		cell := &plotCursorCell{}
		if prev != nil && *prev != nil {
			cell = *prev
		}
		return cell, cell
	})
}

// IntoElement implements gpui.IntoElement (U:plot/element.rs:48-54).
func (e *PlotElement) IntoElement() gpui.AnyElement {
	return gpui.CustomElement[struct{}, plotPrepaintState](e)
}

// plotPrepaintState is the occlusion-aware hitbox, the plot's prepainted
// children and the prepainted tooltip overlay, carried from prepaint to
// paint (U:plot/element.rs:59-61).
type plotPrepaintState struct {
	hitbox     gpui.Hitbox
	hasHitbox  bool
	children   []gpui.AnyElement
	overlay    gpui.AnyElement
	hasOverlay bool
}

// ID implements gpui.Element (U:plot/element.rs:62-66): an id gives the
// plot element state — appear motion and, when interactive, tooltips.
func (e *PlotElement) ID() (gpui.ElementID, bool) { return e.plot.ID() }

// SourceLocation implements gpui.Element (U:plot/element.rs:68-70).
func (e *PlotElement) SourceLocation() *gpui.SourceLocation { return nil }

// RequestLayout implements gpui.Element: the plot fills its container
// (U:plot/element.rs:72-84).
func (e *PlotElement) RequestLayout(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, w *gpui.Window, _ *gpui.App) (gpui.LayoutID, struct{}) {
	style := gpui.DefaultStyle()
	style.Size.Width = gpui.DefiniteLengthOf(gpui.Fraction(1))
	style.Size.Height = gpui.DefiniteLengthOf(gpui.Fraction(1))
	id, err := gpui.RequestElementLayout(w, style)
	if err != nil {
		panic("base: PlotElement request_layout: " + err.Error())
	}
	return id, struct{}{}
}

// Prepaint implements gpui.Element (U:plot/element.rs:86-142).
func (e *PlotElement) Prepaint(global *gpui.GlobalElementID, _ *gpui.InspectorElementID, bounds gpui.Bounds, _ *struct{}, w *gpui.Window, app *gpui.App) plotPrepaintState {
	// Children are laid out here, before the early return, so plots
	// without an id still get them.
	state := plotPrepaintState{children: e.plot.Prepaint(bounds, w, app)}
	if global == nil {
		return state
	}

	appear := PlotAppearComplete()
	if generation, ok := e.plot.AppearGeneration(); ok {
		appear = plotTrackAppear(global, generation, w, app)
	}
	appearing := appear.IsAppearing()
	e.plot.Appear(appear, w, app)

	if !e.plot.Interactive() {
		return state
	}

	// Hitbox.IsHovered is false while an open popup or modal covers the
	// plot.
	state.hitbox = w.InsertHitbox(bounds, gpui.HitboxNormal)
	state.hasHitbox = true

	// The cell only gates visibility; the position comes from the live
	// mouse and this frame's bounds so the tooltip does not lag a frame
	// while scrolling.
	var cursor *gpui.Point
	if _, ok := plotTooltipCursor(global, w).get(); ok {
		mouse := w.TrackedMousePosition()
		if bounds.Contains(mouse) {
			c := gpui.Point{X: mouse.X - bounds.Origin.X, Y: mouse.Y - bounds.Origin.Y}
			cursor = &c
		}
	}
	// No tooltip while the marks draw in: its dots would land on data not
	// painted yet.
	var live *TooltipState
	if cursor != nil && !appearing {
		if s, ok := e.plot.TooltipState(*cursor, bounds, app); ok {
			live = &s
		}
	}

	// The datum under the cursor, or the last one while its hover fades.
	hover, hoverCursor := plotTrackHover(live, cursor, w, app)
	e.plot.Hover(hover, w, app)

	if hover != nil {
		if overlay, ok := e.plot.Tooltip(hover.State(), hoverCursor, bounds, w, app); ok {
			overlay.PrepaintAsRoot(bounds.Origin, gpui.AvailableSize{
				Width:  gpui.DefiniteAvailableSpace(bounds.Size.Width),
				Height: gpui.DefiniteAvailableSpace(bounds.Size.Height),
			}, w, app)
			state.overlay, state.hasOverlay = overlay, true
		}
	}
	return state
}

// Paint implements gpui.Element (U:plot/element.rs:144-199).
func (e *PlotElement) Paint(global *gpui.GlobalElementID, _ *gpui.InspectorElementID, bounds gpui.Bounds, _ *struct{}, prepaint *plotPrepaintState, w *gpui.Window, app *gpui.App) {
	e.plot.Paint(bounds, w, app)

	for _, child := range prepaint.children {
		child.Paint(w, app)
	}

	if global != nil && prepaint.hasHitbox {
		cell := plotTooltipCursor(global, w)
		hitbox := prepaint.hitbox
		// Notify only the view painting this plot: a window refresh would
		// drop every sibling view's cache on each pixel of mouse movement.
		view, hasView := gpui.CurrentView(w)

		// Relayout can move the plot under a still cursor without a move
		// event, so re-derive hover each frame; only a visibility flip
		// needs a corrective frame.
		next := plotCursorCell{}
		if hitbox.IsHovered(w) {
			mouse := w.TrackedMousePosition()
			next = plotCursorCell{pos: gpui.Point{X: mouse.X - bounds.Origin.X, Y: mouse.Y - bounds.Origin.Y}, set: true}
		}
		if *cell != next {
			visibilityChanged := cell.set != next.set
			*cell = next
			if visibilityChanged {
				gpui.RequestAnimationFrame(w)
			}
		}

		w.OnMouseEvent(func(event any, _ gpui.DispatchPhase, w *gpui.Window, app *gpui.App) {
			ev, ok := event.(*gpui.MouseMoveEvent)
			if !ok {
				return
			}
			next := plotCursorCell{}
			if hitbox.IsHovered(w) {
				next = plotCursorCell{pos: gpui.Point{X: ev.Position.X - bounds.Origin.X, Y: ev.Position.Y - bounds.Origin.Y}, set: true}
			}
			if *cell != next {
				*cell = next
				if hasView {
					app.Notify(view)
				}
			}
		})
	}

	// Crosshair and dots paint above the plot; a deferred box paints
	// above everything.
	if prepaint.hasOverlay {
		prepaint.overlay.Paint(w, app)
	}
}
