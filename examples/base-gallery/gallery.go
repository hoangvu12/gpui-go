package main

import (
	"github.com/hoangvu12/gpui-go/base"
	"github.com/hoangvu12/gpui-go/gpui"
)

// page is one gallery page: a component shown in a few states.
type page struct {
	id     string
	title  string
	module string // the upstream module, relative to crates/base/src
	render func(g *gallery, w *gpui.Window, cx *gpui.Context[gallery]) any
}

// group is one sidebar folder.
type group struct {
	name  string
	pages []page
}

// groups lists every page in sidebar order. Each group's pages live in
// their own file (pages_simple.go, pages_inputs.go, ...).
var groups = []group{
	{"Simple", simplePages},
	{"Inputs", inputPages},
	{"Overlays", overlayPages},
	{"Layout", layoutPages},
	{"Scrolling", scrollingPages},
	{"Text", textPages},
	{"Charts", chartPages},
	{"Foundations", foundationPages},
}

// allPages returns every page in sidebar order.
func allPages() []page {
	var out []page
	for _, grp := range groups {
		out = append(out, grp.pages...)
	}
	return out
}

func pageByID(id string) (page, bool) {
	for _, p := range allPages() {
		if p.id == id {
			return p, true
		}
	}
	return page{}, false
}

const firstPage = "button"

// startPage is the page shown first; main sets it from the command line.
var startPage = firstPage

// gallery is the root content view. It owns the sidebar, the current
// page and every page's state, so a page keeps its state while another
// one is shown.
type gallery struct {
	current string
	dark    bool

	nav    gpui.Entity[base.TreeState]
	scroll gpui.ScrollHandle

	simpleState
	inputState
	overlayState
	layoutState
	scrollingState
	textState
	motionState
}

func newGallery(w *gpui.Window, cx *gpui.Context[gallery]) gallery {
	app := cx.App()
	start := firstPage
	if _, ok := pageByID(startPage); ok {
		start = startPage
	}
	g := gallery{current: start, scroll: gpui.NewScrollHandle()}

	var folders []base.TreeItem
	for _, grp := range groups {
		folder := base.NewTreeItem("group:"+grp.name, grp.name).Expanded(true)
		for _, p := range grp.pages {
			folder = folder.Child(base.NewTreeItem(p.id, p.title))
		}
		folders = append(folders, folder)
	}
	g.nav = gpui.NewEntity(app, w.Scope(), func(st *base.TreeState, tcx *gpui.Context[base.TreeState]) {
		*st = base.NewTreeState(app).Items(folders)
		st.SetSelectedIndex(st.IndexOf(start), true, tcx)
	})
	// The tree owns selection (mouse and arrow keys); a selected leaf
	// opens its page.
	cx.Observe(g.nav, func(g *gallery, nav gpui.Entity[base.TreeState], cx *gpui.Context[gallery]) {
		id := nav.Read(cx.App(), func(st *base.TreeState, _ *gpui.App) string {
			if item := st.SelectedItem(); item != nil && !item.IsFolder() {
				return item.ID
			}
			return ""
		})
		if id != "" && id != g.current {
			g.show(id)
		}
		cx.Notify()
	})

	g.initSimple(w, cx)
	g.initInputs(w, cx)
	g.initOverlays(w, cx)
	g.initLayout(w, cx)
	g.initScrolling(w, cx)
	g.initText(w, cx)
	g.initMotion(w, cx)
	return g
}

// show switches to a page and scrolls it to the top.
func (g *gallery) show(id string) {
	g.current = id
	g.scroll.SetOffset(gpui.Point{})
}

// update returns a click handler that mutates the gallery and redraws.
func (g *gallery) update(cx *gpui.Context[gallery], f func(g *gallery)) func(*gpui.ClickEvent, *gpui.Window, *gpui.App) {
	this := cx.Entity().Downgrade()
	return func(_ *gpui.ClickEvent, _ *gpui.Window, app *gpui.App) {
		set(this, app, f)
	}
}

// set mutates the gallery from any handler and redraws.
func set(this gpui.WeakEntity[gallery], app *gpui.App, f func(g *gallery)) {
	_ = this.TryUpdate(app, func(g *gallery, cx *gpui.Context[gallery]) {
		f(g)
		cx.Notify()
	})
}

// setTheme switches the Base theme global. Every gallery colour, and
// every input's unset colour, resolves from these tokens on the next
// frame.
func setTheme(app *gpui.App, dark bool) {
	theme := base.ThemeGlobalMut(app)
	if dark {
		theme.Appearance = base.ThemeAppearanceDark
		theme.Tokens.Colors = base.DarkColorTokens()
	} else {
		theme.Appearance = base.ThemeAppearanceLight
		theme.Tokens.Colors = base.LightColorTokens()
	}
}

func (g *gallery) Render(w *gpui.Window, cx *gpui.Context[gallery]) gpui.AnyElement {
	pal = paletteFrom(base.ThemeGlobal(cx.App()))
	return gpui.Div().
		ID("gallery").
		SizeFull().
		Flex().FlexCol().
		Bg(pal.canvas).
		TextColor(pal.fg).
		TextSm().
		Child(g.header(cx)).
		Child(gpui.Div().Flex1().MinH(gpui.Px(0)).
			Child(base.HResizable("gallery-split").
				Child(base.NewResizablePanel().Size(236).SizeRange(180, 380).Child(g.sidebar())).
				Child(base.NewResizablePanel().Child(g.content(w, cx))))).
		IntoElement()
}

func (g *gallery) header(cx *gpui.Context[gallery]) any {
	this := cx.Entity().Downgrade()
	return gpui.Div().
		H12().FlexNone().Px4().
		Flex().ItemsCenter().JustifyBetween().
		BorderB1().BorderColor(pal.border).
		Bg(pal.surface).
		Child(gpui.Div().Flex().ItemsCenter().Gap2().
			Child(gpui.Div().FontWeight(600).Child("GPUI Base gallery")).
			Child(gpui.Div().TextXs().TextColor(pal.muted).
				Child(itoa(len(allPages())) + " pages · github.com/hoangvu12/gpui-go/base"))).
		Child(gpui.Div().Flex().ItemsCenter().Gap2().TextXs().
			Child(gpui.Div().TextColor(pal.muted).Child("Dark theme")).
			Child(themedSwitch("theme-switch", g.dark, false, func(next bool, app *gpui.App) {
				setTheme(app, next)
				set(this, app, func(g *gallery) { g.dark = next })
			})))
}

// sidebar is the navigation tree (a virtualized base.Tree).
func (g *gallery) sidebar() any {
	return gpui.Div().SizeFull().Flex().FlexCol().
		Bg(pal.surface).BorderR1().BorderColor(pal.border).
		Child(base.NewTree(g.nav).
			Flex1().MinH(gpui.Px(0)).Py2().TextSm().
			ListStyle(gpui.Styles().SizeFull().Refinement()).
			Item(func(_ int, entry *base.TreeEntry, state base.TreeEntryState, _ *gpui.Window, _ *gpui.App) gpui.AnyElement {
				row := gpui.Div().H7().Mx2().Px2().Flex().ItemsCenter().Gap1().RoundedMd().CursorPointer().
					Hover(gpui.Styles().Bg(pal.hover))
				if entry.IsFolder() {
					return row.Mt1().TextXs().FontWeight(600).TextColor(pal.muted).
						Child(folderChevron(entry.IsExpanded())).
						Child(entry.Item().Label).
						IntoElement()
				}
				if state.IsSelected() {
					row = row.Bg(pal.selected).FontWeight(500)
				}
				return row.Pl(gpui.Px(24)).Child(entry.Item().Label).IntoElement()
			}))
}

// content is the current page in a scroll area with a Base scrollbar.
func (g *gallery) content(w *gpui.Window, cx *gpui.Context[gallery]) any {
	p, ok := pageByID(g.current)
	if !ok {
		p, _ = pageByID(firstPage)
	}
	return gpui.Div().
		ID("page-scroll").
		Relative().
		SizeFull().
		OverflowYScroll().
		TrackScroll(g.scroll).
		Child(gpui.Div().
			ID(gpui.NamedChildElementID(gpui.NameElementID("page"), p.id)).
			DebugSelector("page-" + p.id).
			P6().Pb(gpui.Px(48)).
			Flex().FlexCol().Gap5().
			Child(gpui.Div().Flex().FlexCol().Gap1().
				Child(gpui.Div().TextXl().FontWeight(600).Child(p.title)).
				Child(gpui.Div().TextXs().TextColor(pal.muted).
					Child("Upstream module: gpui-component crates/base/src/" + p.module))).
			Child(p.render(g, w, cx))).
		Child(base.NewScrollbar(g.scroll))
}
