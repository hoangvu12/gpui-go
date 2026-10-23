package main

import (
	"github.com/hoangvu12/gpui-go/base"
	"github.com/hoangvu12/gpui-go/gpui"
)

// The dock follows examples/base-showcase/dock.go: a DockArea with a
// split center and a bottom dock, drawn by an application skin (Base's
// own renderer draws the layout and nothing else).

const dockTabBarHeight float32 = 28

// dockPanel is one dock panel (Panel: Render + PanelName + FocusHandle).
type dockPanel struct {
	name        string
	body        string
	focusHandle gpui.FocusHandle
}

func newDockPanel(name, body string, app *gpui.App) gpui.Entity[dockPanel] {
	return gpui.NewEntity(app, app.RootScope(), func(p *dockPanel, _ *gpui.Context[dockPanel]) {
		*p = dockPanel{name: name, body: body, focusHandle: app.FocusHandle()}
	})
}

func (p *dockPanel) PanelName() string                      { return p.name }
func (p *dockPanel) FocusHandle(*gpui.App) gpui.FocusHandle { return p.focusHandle }

func (p *dockPanel) Render(*gpui.Window, *gpui.Context[dockPanel]) gpui.AnyElement {
	return gpui.Div().SizeFull().Flex().FlexCol().Gap1().P3().TextXs().
		Bg(pal.surface).TextColor(pal.fg).
		Child(gpui.Div().FontWeight(600).Child(p.name)).
		Child(gpui.Div().TextColor(pal.muted).Child(p.body)).
		IntoElement()
}

// dragPreview is the view following the cursor while a tab is dragged.
type dragPreview struct{ title string }

func (d *dragPreview) Render(*gpui.Window, *gpui.Context[dragPreview]) gpui.AnyElement {
	return gpui.Div().Px2().Py1().TextXs().Bg(pal.surface).TextColor(pal.accent).
		Border1().BorderColor(pal.accent).RoundedSm().Child(d.title).IntoElement()
}

// dockSkin is the application's dock appearance.
type dockSkin struct{}

func (dockSkin) TabGroupRenderer() base.TabGroupRenderer { return dockSkin{} }

func (dockSkin) Frame(*base.TabGroupContext, *gpui.Window, *gpui.App) *gpui.DivElement {
	return gpui.Div().ID("gallery-tab-group").SizeFull().Flex().FlexCol().MinH(gpui.Px(0)).OverflowHidden().
		Bg(pal.surface).Border1().BorderColor(pal.border)
}

func (dockSkin) ContentFrame(*base.TabGroupContext, *gpui.Window, *gpui.App) *gpui.DivElement {
	// Relative, because the drop indicator is positioned against it.
	return gpui.Div().ID("gallery-tab-content").Relative().Flex1().MinH(gpui.Px(0)).OverflowHidden()
}

func (dockSkin) RenderTabBar(group *base.TabGroupContext, _ *gpui.Window, app *gpui.App) gpui.AnyElement {
	bar := gpui.Div().Flex().FlexRow().ItemsCenter().H(gpui.Px(dockTabBarHeight)).FlexNone().OverflowHidden().
		Bg(pal.elevated).BorderB1().BorderColor(pal.border)
	for ix, panel := range group.Panels() {
		if !panel.Visible(app) {
			continue
		}
		title := panel.PanelName(app)
		tab := gpui.Div().ID(gpui.NamedIntegerElementID("gallery-dock-tab", uint64(ix))).
			Px3().HFull().Flex().ItemsCenter().TextXs().CursorPointer().
			OnClick(func(_ *gpui.ClickEvent, w *gpui.Window, app *gpui.App) { group.SelectTab(ix, w, app) }).
			Child(title)
		if ix == group.ActiveIx() {
			tab = tab.Bg(pal.surface).TextColor(pal.fg).FontWeight(600)
		} else {
			tab = tab.TextColor(pal.muted)
		}
		if drag, ok := group.DragPanel(ix, app); ok {
			tab = tab.OnDrag(drag, func(drag *base.DragPanel, offset gpui.Point, w *gpui.Window, app *gpui.App) gpui.View {
				drag.SetDragOffset(offset)
				return gpui.ViewOf(gpui.NewEntity(app, w.Scope(), func(v *dragPreview, _ *gpui.Context[dragPreview]) {
					v.title = title
				}))
			})
		}
		bar = bar.Child(tab)
	}
	return bar.IntoElement()
}

// RenderDropIndicator paints where a dragged tab would land; Base
// resolves the target.
func (dockSkin) RenderDropIndicator(indicator base.DropIndicator, _ *gpui.Window, _ *gpui.App) (gpui.AnyElement, bool) {
	to := indicator.To()
	return gpui.Div().Absolute().
		Left(gpui.Px(to.Origin().X)).Top(gpui.Px(to.Origin().Y)).
		W(gpui.Px(to.Size().Width)).H(gpui.Px(to.Size().Height)).
		Bg(pal.accent.Opacity(0.2)).IntoElement(), true
}

// dockAreaSkin draws the area around the tab groups: the frame, the
// center column and each dock's resize strip (upstream's
// examples/showcase/components/dock.rs ShowcaseDockSkin). Base keeps the
// layout; the strip only reports the pointer back through DockContext.
type dockAreaSkin struct {
	// resizing is the dock a resize drag is sizing, captured on mouse
	// down. The drag follows the pointer anywhere in the area, so the
	// area frame tracks it.
	resizing *dockResize
}

type dockResize struct{ dock *base.DockContext }

const dockResizeStrip float32 = 6

func (dockAreaSkin) TabGroupRenderer() base.TabGroupRenderer { return dockSkin{} }

func (s dockAreaSkin) Frame(*gpui.Window, *gpui.App) *gpui.DivElement {
	resizing := s.resizing
	return gpui.Div().ID("gallery-dock").SizeFull().Flex().FlexRow().OverflowHidden().
		Bg(pal.elevated).
		OnMouseMove(func(event *gpui.MouseMoveEvent, w *gpui.Window, app *gpui.App) {
			if dock := resizing.dock; dock != nil {
				dock.ResizeTo(event.Position, w, app)
			}
		}).
		OnMouseUp(gpui.MouseButtonLeft, func(_ *gpui.MouseUpEvent, w *gpui.Window, app *gpui.App) {
			if dock := resizing.dock; dock != nil {
				resizing.dock = nil
				dock.EndResize(w, app)
			}
		})
}

func (dockAreaSkin) CenterFrame(*gpui.Window, *gpui.App) *gpui.DivElement {
	return gpui.Div().ID("gallery-dock-center").Flex().Flex1().FlexCol().OverflowHidden()
}

func (s dockAreaSkin) RenderDock(dock *base.DockContext, content gpui.AnyElement, _ *gpui.Window, _ *gpui.App) gpui.AnyElement {
	if !dock.IsOpen() {
		return gpui.Div().IntoElement()
	}
	frame := gpui.Div().Flex().FlexNone().Relative().OverflowHidden()
	line := gpui.Div().Bg(pal.border)
	strip := gpui.Div().Absolute().Flex().ItemsCenter().JustifyCenter()
	switch dock.Placement() {
	case base.DockPlacementBottom:
		frame = frame.WFull().H(gpui.Px(dock.Size())).FlexCol()
		strip = strip.Top0().Left0().WFull().H(gpui.Px(dockResizeStrip)).CursorRowResize()
		line = line.H(gpui.Px(1)).WFull()
	case base.DockPlacementLeft:
		frame = frame.HFull().W(gpui.Px(dock.Size())).FlexRow()
		strip = strip.Top0().Right0().HFull().W(gpui.Px(dockResizeStrip)).CursorColResize()
		line = line.W(gpui.Px(1)).HFull()
	default:
		frame = frame.HFull().W(gpui.Px(dock.Size())).FlexRow()
		strip = strip.Top0().Left0().HFull().W(gpui.Px(dockResizeStrip)).CursorColResize()
		line = line.W(gpui.Px(1)).HFull()
	}
	captured := *dock
	resizing := s.resizing
	strip = strip.ID(gpui.NamedIntegerElementID("gallery-dock-resize", uint64(dock.Placement()))).
		DebugSelector("gallery-dock-resize").
		OnMouseDown(gpui.MouseButtonLeft, func(_ *gpui.MouseDownEvent, _ *gpui.Window, app *gpui.App) {
			app.StopPropagation()
			resizing.dock = &captured
		}).
		Child(line)
	return frame.Child(content).Child(strip).IntoElement()
}

// buildDock builds the area once (a DockArea is an entity; rebuilding it
// every frame would discard the layout the viewer arranged).
func buildDock(w *gpui.Window, app *gpui.App) gpui.Entity[base.DockArea] {
	explorer := newDockPanel("Explorer", "Drag this tab into the other group to move it there.", app)
	search := newDockPanel("Search", "Two panels share this tab group. Click a tab to switch.", app)
	editor := newDockPanel("Editor", "Drag a tab towards an edge of this group to split there.", app)
	preview := newDockPanel("Preview", "A second panel in the editor group.", app)
	terminal := newDockPanel("Terminal", "The bottom dock shares the column with the center region.", app)
	problems := newDockPanel("Problems", "Nothing here.", app)

	version := uint64(1)
	area := gpui.NewEntity(app, w.Scope(), func(a *base.DockArea, cx *gpui.Context[base.DockArea]) {
		*a = base.NewDockArea("gallery-dock", &version, w, cx)
		a.WithRenderer(dockAreaSkin{resizing: &dockResize{}})
	})
	explorerSize := float32(190)
	area.Update(app, func(a *base.DockArea, cx *gpui.Context[base.DockArea]) {
		a.SetCenter(base.DockLayoutHSplit().
			Child(base.DockLayoutTabs().Panel(explorer).Panel(search), &explorerSize).
			Child(base.DockLayoutTabs().Panel(editor).Panel(preview), nil), w, cx)
		a.SetDock(base.DockPlacementBottom, base.DockLayoutTabs().Panel(terminal).Panel(problems), w, cx)
		a.SetDockSize(base.DockPlacementBottom, 120, w, cx)
	})
	return area
}
