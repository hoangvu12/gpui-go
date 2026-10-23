package base

// Port of U:resizable/panel.rs: the resizable panel group and its panels.

import (
	"github.com/hoangvu12/gpui-go/gpui"
)

// resizableDragPanel is the drag payload of a panel's handle
// (U:resizable/panel.rs:21-27 DragPanel).
type resizableDragPanel struct{}

// Render renders the empty drag preview (U:resizable/panel.rs:23-27).
func (*resizableDragPanel) Render(*gpui.Window, *gpui.Context[resizableDragPanel]) gpui.AnyElement {
	return gpui.Empty()
}

// ResizeCallback is called when the panels are resized
// (U:resizable/panel.rs:37, 106-117).
type ResizeCallback func(state gpui.Entity[ResizableState], w *gpui.Window, app *gpui.App)

// ResizablePanelGroup is a group of resizable panels
// (U:resizable/panel.rs:29-39).
type ResizablePanelGroup struct {
	id               gpui.ElementID
	state            *gpui.Entity[ResizableState]
	axis             gpui.Axis
	size             *float32
	children         []*ResizablePanel
	onResize         ResizeCallback
	handleAppearance ResizeHandleRenderer
}

// NewResizablePanelGroup is ResizablePanelGroup::new
// (U:resizable/panel.rs:41-53).
func NewResizablePanelGroup[I gpui.IntoElementID](id I) *ResizablePanelGroup {
	return &ResizablePanelGroup{
		id:       gpui.ElementIDOf(id),
		axis:     gpui.AxisHorizontal,
		onResize: func(gpui.Entity[ResizableState], *gpui.Window, *gpui.App) {},
	}
}

// WithHandleAppearance hands the painted part of every divider in this
// group to appearance (U:resizable/panel.rs:55-62). The hit area, the
// cursor and the drag stay here; a renderer that declines a handle leaves
// the built-in line on it.
func (g *ResizablePanelGroup) WithHandleAppearance(appearance ResizeHandleRenderer) *ResizablePanelGroup {
	g.handleAppearance = appearance
	return g
}

// WithState binds the group to a caller-owned state entity; without it
// the group keeps its own keyed state (U:resizable/panel.rs:64-70).
func (g *ResizablePanelGroup) WithState(state gpui.Entity[ResizableState]) *ResizablePanelGroup {
	g.state = &state
	return g
}

// Axis sets the resize axis; the default is horizontal
// (U:resizable/panel.rs:72-76).
func (g *ResizablePanelGroup) Axis(axis gpui.Axis) *ResizablePanelGroup {
	g.axis = axis
	return g
}

// Child adds a panel (U:resizable/panel.rs:78-86). A value that is not a
// *ResizablePanel becomes a panel's only child, like upstream's
// From<impl Into<AnyElement>> and From<ResizablePanelGroup>
// (U:resizable/panel.rs:120-133).
func (g *ResizablePanelGroup) Child(panel any) *ResizablePanelGroup {
	g.children = append(g.children, intoResizablePanel(panel))
	return g
}

// Children replaces the panels (U:resizable/panel.rs:88-95).
func (g *ResizablePanelGroup) Children(panels ...any) *ResizablePanelGroup {
	g.children = g.children[:0:0]
	for _, p := range panels {
		g.children = append(g.children, intoResizablePanel(p))
	}
	return g
}

func intoResizablePanel(value any) *ResizablePanel {
	if p, ok := value.(*ResizablePanel); ok {
		return p
	}
	return NewResizablePanel().Child(value)
}

// Size sets the group's cross-axis size: the height of a horizontal group,
// the width of a vertical one (U:resizable/panel.rs:97-104).
func (g *ResizablePanelGroup) Size(size float32) *ResizablePanelGroup {
	g.size = &size
	return g
}

// OnResize sets the callback run when the panels are resized
// (U:resizable/panel.rs:106-117).
func (g *ResizablePanelGroup) OnResize(onResize ResizeCallback) *ResizablePanelGroup {
	g.onResize = onResize
	return g
}

// RenderOnce implements gpui.RenderOnce (U:resizable/panel.rs:137-208).
//
// Go adaptation: the keyed state is created only when no state was
// supplied (upstream's unwrap_or evaluates use_keyed_state eagerly, which
// keeps an unused entity under the key; nothing observes it).
func (g *ResizablePanelGroup) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	var state gpui.Entity[ResizableState]
	if g.state != nil {
		state = *g.state
	} else {
		state = w.UseKeyedState(g.id, app, func(*gpui.Window, *gpui.Context[ResizableState]) ResizableState {
			return NewResizableState()
		})
	}
	axis := g.axis
	var container *gpui.DivElement
	if AxisIsHorizontal(axis) {
		container = HFlex()
	} else {
		container = VFlex()
	}

	// Sync panels to the state.
	panelsCount := len(g.children)
	state.Update(app, func(s *ResizableState, cx *gpui.Context[ResizableState]) {
		s.duringDraw(func() { s.syncPanelsCount(axis, panelsCount, cx) })
		if !s.tracked {
			// The frame drawing the state for the first time tracks it
			// from its end on (see drawNotify).
			w.Defer(app, func(_ *gpui.Window, app *gpui.App) {
				state.Update(app, func(s *ResizableState, _ *gpui.Context[ResizableState]) { s.tracked = true })
			})
		}
	})

	d := container.ID(g.id).SizeFull()
	// The group only distributes space along its own axis, so a caller
	// supplied size can only mean the cross axis.
	if g.size != nil {
		if axis == gpui.AxisHorizontal {
			d.H(gpui.Px(*g.size))
		} else {
			d.W(gpui.Px(*g.size))
		}
	}
	for ix, panel := range g.children {
		panel.panelIx = ix
		panel.axis = axis
		panel.state = &state
		panel.handleAppearance = g.handleAppearance
		d.Child(panel)
	}
	d = ElementExtOf(d).OnPrepaint(func(bounds gpui.Bounds, w *gpui.Window, app *gpui.App) {
		state.Update(app, func(s *ResizableState, cx *gpui.Context[ResizableState]) {
			sizeChanged := s.bounds.Size.Along(axis) != bounds.Size.Along(axis)
			s.bounds = bounds
			if sizeChanged {
				s.duringDraw(func() { s.adjustToContainerSize(cx) })
				// The adjustment lands after this frame's layout has already
				// been computed, and a notify raised during a draw only
				// records the view as dirty without scheduling a frame for
				// it. Defer the notify so it runs once the draw has finished
				// and can schedule the settling frame
				// (U:resizable/panel.rs:186-197).
				w.Defer(app, func(_ *gpui.Window, app *gpui.App) {
					state.Update(app, func(_ *ResizableState, cx *gpui.Context[ResizableState]) { cx.Notify() })
				})
			}
		})
	})
	d.Child(gpui.CustomElement[struct{}, struct{}](&resizePanelGroupElement{
		state:    state,
		axis:     axis,
		onResize: g.onResize,
	}))
	return d.IntoElement()
}

// ResizablePanel is a resizable panel inside a ResizablePanelGroup
// (U:resizable/panel.rs:210-250).
//
// Caller style overrides apply between the panel's flex defaults and its
// size management: FlexNone cancels the internal flex_grow 1, while the
// runtime min/max and flex_basis driven by ResizableState always win. Do
// not set flex_basis, absolute or overflow_hidden from outside.
type ResizablePanel struct {
	gpui.Styled[*ResizablePanel]
	gpui.Parent[*ResizablePanel]

	axis    gpui.Axis
	panelIx int
	state   *gpui.Entity[ResizableState]
	// initialSize is the size the panel has when it is created.
	initialSize *float32
	// sizeRange limits the panel's resize.
	sizeRange        PixelsRange
	visible          bool
	handleAppearance ResizeHandleRenderer
}

// NewResizablePanel creates a panel (U:resizable/mod.rs:26-29
// resizable_panel; U:resizable/panel.rs:253-266).
func NewResizablePanel() *ResizablePanel {
	return gpui.BindParent(gpui.BindStyled(&ResizablePanel{
		sizeRange: PixelsRange{PanelMinSize, pixelsMax},
		axis:      gpui.AxisHorizontal,
		visible:   true,
	}))
}

// Visible sets the visibility of the panel; the default is true
// (U:resizable/panel.rs:268-272).
func (p *ResizablePanel) Visible(visible bool) *ResizablePanel {
	p.visible = visible
	return p
}

// Size sets the initial size of the panel (U:resizable/panel.rs:274-278).
// It shadows the promoted Styled.Size, as the inherent method does
// upstream.
func (p *ResizablePanel) Size(size float32) *ResizablePanel {
	p.initialSize = &size
	return p
}

// SizeRange sets the size range limiting a resize; the default is
// PanelMinSize to Pixels::MAX (U:resizable/panel.rs:280-286).
func (p *ResizablePanel) SizeRange(start, end float32) *ResizablePanel {
	p.sizeRange = PixelsRange{start, end}
	return p
}

// RenderOnce implements gpui.RenderOnce (U:resizable/panel.rs:301-380).
func (p *ResizablePanel) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	id := gpui.NamedIntegerElementID("resizable-panel", uint64(p.panelIx))
	if !p.visible {
		// Gap 2b: the plain upstream div() is a block.
		return gpui.Div().ID(id).Block().IntoElement()
	}

	if p.state == nil {
		panic("BUG: The `state` in ResizablePanel should be present.")
	}
	state := *p.state
	panelIx, axis := p.panelIx, p.axis
	panelState := state.Read(app, func(s *ResizableState, _ *gpui.App) resizablePanelState {
		if panelIx >= len(s.panels) {
			panic("BUG: The `index` of ResizablePanel should be one of in `state`.")
		}
		return s.panels[panelIx]
	})
	sizeRange := p.sizeRange

	d := gpui.Div().ID(id).Flex().FlexGrow1().SizeFull().Relative()
	// Apply caller style overrides here, between the flex defaults above
	// and the size management below (U:resizable/panel.rs:323-330).
	style := p.Style()
	d = StyledExtOf(d).RefineStyle(&style)
	if AxisIsVertical(axis) {
		d.MinH(gpui.Px(sizeRange.Start)).MaxH(gpui.Px(sizeRange.End))
	}
	if AxisIsHorizontal(axis) {
		d.MinW(gpui.Px(sizeRange.Start)).MaxW(gpui.Px(sizeRange.End))
	}
	// 1. initial_size is None: auto size.
	// 2. initial_size is Some and size is None: the initial size for the
	//    first render.
	// 3. initial_size is Some and size is Some: size.
	if p.initialSize == nil {
		d.FlexShrink1()
	} else {
		initialSize := *p.initialSize
		// No size yet means the initial size, so keep it with flex_none.
		if panelState.size == nil && initialSize != 0 {
			d.FlexNone()
		}
		d.FlexBasis(gpui.Px(initialSize))
	}
	if panelState.size != nil {
		d.FlexBasis(gpui.Px(max(min(*panelState.size, sizeRange.End), sizeRange.Start)))
	}
	d = ElementExtOf(d).OnPrepaint(func(bounds gpui.Bounds, _ *gpui.Window, app *gpui.App) {
		state.Update(app, func(s *ResizableState, cx *gpui.Context[ResizableState]) {
			s.duringDraw(func() { s.updatePanelSize(panelIx, bounds, sizeRange, cx) })
		})
	})
	d.Extend(p.TakeChildren())
	if panelIx > 0 {
		ix := panelIx - 1
		handle := NewResizeHandle[resizableDragPanel](gpui.NamedIntegerElementID("resizable-handle", uint64(ix)), axis)
		if p.handleAppearance != nil {
			handle.WithAppearance(p.handleAppearance)
		}
		handle.OnDrag(resizableDragPanel{}, func(drag *resizableDragPanel, _ gpui.Point, w *gpui.Window, app *gpui.App) gpui.View {
			app.StopPropagation()
			// Set the current resizing panel ix.
			state.Update(app, func(s *ResizableState, _ *gpui.Context[ResizableState]) {
				s.resizingPanelIx, s.hasResizingIx = ix, true
			})
			value := *drag
			return gpui.ViewOf(gpui.NewEntity(app, w.Scope(), func(v *resizableDragPanel, _ *gpui.Context[resizableDragPanel]) { *v = value }))
		})
		d.Child(handle)
	}
	return d.IntoElement()
}

// resizePanelGroupElement drives an in-progress drag and ends it on mouse
// up (U:resizable/panel.rs:383-489).
type resizePanelGroupElement struct {
	state    gpui.Entity[ResizableState]
	onResize ResizeCallback
	axis     gpui.Axis
}

// ID implements gpui.Element (U:resizable/panel.rs:401-403).
func (e *resizePanelGroupElement) ID() (gpui.ElementID, bool) { return gpui.ElementID{}, false }

// SourceLocation implements gpui.Element (U:resizable/panel.rs:405-407).
func (e *resizePanelGroupElement) SourceLocation() *gpui.SourceLocation { return nil }

// RequestLayout requests a default-styled node
// (U:resizable/panel.rs:409-417).
func (e *resizePanelGroupElement) RequestLayout(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, w *gpui.Window, _ *gpui.App) (gpui.LayoutID, struct{}) {
	id, err := gpui.RequestElementLayout(w, gpui.DefaultStyle())
	if err != nil {
		panic(err)
	}
	return id, struct{}{}
}

// Prepaint implements gpui.Element (U:resizable/panel.rs:419-429).
func (e *resizePanelGroupElement) Prepaint(*gpui.GlobalElementID, *gpui.InspectorElementID, gpui.Bounds, *struct{}, *gpui.Window, *gpui.App) struct{} {
	return struct{}{}
}

// Paint registers the drag and release listeners
// (U:resizable/panel.rs:431-488). The resizing panel is read at paint, so
// a drag that starts takes effect from the next frame.
func (e *resizePanelGroupElement) Paint(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, _ gpui.Bounds, _ *struct{}, _ *struct{}, w *gpui.Window, app *gpui.App) {
	state, axis, onResize := e.state, e.axis, e.onResize
	type resizing struct {
		ix int
		ok bool
	}
	current := state.Read(app, func(s *ResizableState, _ *gpui.App) resizing {
		return resizing{s.resizingPanelIx, s.hasResizingIx}
	})

	w.OnMouseEvent(func(event any, phase gpui.DispatchPhase, w *gpui.Window, app *gpui.App) {
		ev, ok := event.(*gpui.MouseMoveEvent)
		if !ok || phase != gpui.DispatchBubble || !current.ok {
			return
		}
		ix := current.ix
		state.Update(app, func(s *ResizableState, cx *gpui.Context[ResizableState]) {
			if ix >= len(s.panels) {
				panic("BUG: invalid panel index")
			}
			panel := s.panels[ix]
			if axis == gpui.AxisHorizontal {
				s.resizePanelAtHandle(ix, ev.Position.X-panel.bounds.Left(), w, cx)
			} else {
				s.resizePanelAtHandle(ix, ev.Position.Y-panel.bounds.Top(), w, cx)
			}
			cx.Notify()
		})
	})

	// When any mouse up, stop dragging.
	w.OnMouseEvent(func(event any, phase gpui.DispatchPhase, w *gpui.Window, app *gpui.App) {
		if _, ok := event.(*gpui.MouseUpEvent); !ok || !current.ok {
			return
		}
		if phase == gpui.DispatchBubble {
			state.Update(app, func(s *ResizableState, cx *gpui.Context[ResizableState]) { s.doneResizing(cx) })
			onResize(state, w, app)
		}
	})
}
