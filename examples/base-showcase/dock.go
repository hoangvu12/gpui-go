package main

import (
	"github.com/hoangvu12/gpui-go/base"
	"github.com/hoangvu12/gpui-go/gpui"
)

// The dock follows the upstream showcase's dock.rs: a DockArea with a
// split center and a bottom dock, drawn by an application skin (Base's
// own renderer draws the layout and nothing else).

var (
	dockChrome = gpui.Rgb(0xf4f4f5)
	dockBorder = gpui.Rgb(0xd4d4d8)
	dockMuted  = gpui.Rgb(0x71717a)
	dockDrop   = gpui.RgbaToHsla(0x2563eb33)
)

const dockTabBarHeight float32 = 26

// showcasePanel is one dock panel (Panel: Render + PanelName + FocusHandle).
type showcasePanel struct {
	name        string
	body        string
	focusHandle gpui.FocusHandle
}

func newShowcasePanel(name, body string, app *gpui.App) gpui.Entity[showcasePanel] {
	return gpui.NewEntity(app, app.RootScope(), func(p *showcasePanel, _ *gpui.Context[showcasePanel]) {
		*p = showcasePanel{name: name, body: body, focusHandle: app.FocusHandle()}
	})
}

func (p *showcasePanel) PanelName() string                      { return p.name }
func (p *showcasePanel) FocusHandle(*gpui.App) gpui.FocusHandle { return p.focusHandle }

func (p *showcasePanel) Render(*gpui.Window, *gpui.Context[showcasePanel]) gpui.AnyElement {
	return gpui.Div().SizeFull().Flex().FlexCol().Gap1().P3().TextXs().
		Child(p.name).
		Child(gpui.Div().TextColor(dockMuted).Child(p.body)).
		IntoElement()
}

// dragPreview is the view following the cursor while a tab is dragged.
type dragPreview struct{ title string }

func (d *dragPreview) Render(*gpui.Window, *gpui.Context[dragPreview]) gpui.AnyElement {
	return gpui.Div().Px2().Py1().TextXs().Bg(colSurface).TextColor(colAccent).
		Border1().BorderColor(colAccent).Child(d.title).IntoElement()
}

// dockSkin is the application's dock appearance.
type dockSkin struct{}

func (dockSkin) TabGroupRenderer() base.TabGroupRenderer { return dockSkin{} }

func (dockSkin) Frame(*base.TabGroupContext, *gpui.Window, *gpui.App) *gpui.DivElement {
	return gpui.Div().ID("showcase-tab-group").SizeFull().Flex().FlexCol().MinH(gpui.Px(0)).OverflowHidden().Bg(colSurface)
}

func (dockSkin) ContentFrame(*base.TabGroupContext, *gpui.Window, *gpui.App) *gpui.DivElement {
	// Relative, because the drop indicator is positioned against it.
	return gpui.Div().ID("showcase-tab-content").Relative().Flex1().MinH(gpui.Px(0)).OverflowHidden()
}

func (dockSkin) RenderTabBar(group *base.TabGroupContext, _ *gpui.Window, app *gpui.App) gpui.AnyElement {
	bar := gpui.Div().Flex().FlexRow().ItemsCenter().H(gpui.Px(dockTabBarHeight)).FlexNone().OverflowHidden().
		Bg(dockChrome).BorderB1().BorderColor(dockBorder)
	for ix, panel := range group.Panels() {
		if !panel.Visible(app) {
			continue
		}
		title := panel.PanelName(app)
		tab := gpui.Div().ID(gpui.NamedIntegerElementID("showcase-tab", uint64(ix))).
			Px2().HFull().Flex().ItemsCenter().TextXs().CursorPointer().
			OnClick(func(_ *gpui.ClickEvent, w *gpui.Window, app *gpui.App) { group.SelectTab(ix, w, app) }).
			Child(title)
		if ix == group.ActiveIx() {
			tab = tab.Bg(colSurface).TextColor(colAccent)
		} else {
			tab = tab.TextColor(dockMuted)
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
		Bg(dockDrop).IntoElement(), true
}

// buildDock builds the area once (a DockArea is an entity; rebuilding it
// every frame would discard the layout the viewer arranged).
func buildDock(w *gpui.Window, app *gpui.App) gpui.Entity[base.DockArea] {
	explorer := newShowcasePanel("Explorer", "Drag this tab into the other group to move it there.", app)
	search := newShowcasePanel("Search", "Two panels share this tab group. Click a tab to switch.", app)
	editor := newShowcasePanel("Editor", "Drag a tab towards an edge of this group to split there.", app)
	terminal := newShowcasePanel("Terminal", "The bottom dock shares the column with the center region.", app)
	problems := newShowcasePanel("Problems", "Nothing here.", app)

	version := uint64(1)
	area := gpui.NewEntity(app, w.Scope(), func(a *base.DockArea, cx *gpui.Context[base.DockArea]) {
		*a = base.NewDockArea("showcase-dock", &version, w, cx)
		a.WithRenderer(dockSkin{})
	})
	explorerSize := float32(170)
	area.Update(app, func(a *base.DockArea, cx *gpui.Context[base.DockArea]) {
		a.SetCenter(base.DockLayoutHSplit().
			Child(base.DockLayoutTabs().Panel(explorer).Panel(search), &explorerSize).
			Child(base.DockLayoutTabs().Panel(editor), nil), w, cx)
		a.SetDock(base.DockPlacementBottom, base.DockLayoutTabs().Panel(terminal).Panel(problems), w, cx)
		a.SetDockSize(base.DockPlacementBottom, 110, w, cx)
	})
	return area
}
