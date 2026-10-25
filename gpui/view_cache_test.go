package gpui

// gpui-perf ticket 04's view-caching tests (view_cache.go, view.go): the
// differential test drives a cached and an uncached window through the
// same frames and requires identical scenes, hit testing, dispatch trees,
// cursors, click delivery and focus; the remaining tests pin the pinned
// invalidation rules (R:view.rs:430-445, R:window.rs:2210-2222, 2287-2292).

import (
	"fmt"
	"strings"
	"testing"
)

// vcPanel is one cached panel: a bordered box with a hoverable button, a
// label, a focusable field and, for panel 0, a deferred overlay.
type vcPanel struct {
	index  int
	label  string
	focus  FocusHandle
	clicks []int
}

func (p *vcPanel) Render(w *Window, cx *Context[vcPanel]) AnyElement {
	index, clicks := p.index, p.clicks
	panel := Div().ID(fmt.Sprintf("panel-%d", p.index)).KeyContext(fmt.Sprintf("Panel%d", p.index)).
		SizeFull().Flex().FlexCol().Gap(Px(4)).P(Px(4)).
		Bg(Rgb(0x1e293b)).Border1().BorderColor(Rgb(0x334155)).
		Child(Div().Flex().ID("button").W(Px(80)).H(Px(20)).Bg(Rgb(0x2563eb)).
			Hover(Styles().Bg(Rgb(0xdc2626))).CursorPointer().
			OnClick(func(*ClickEvent, *Window, *App) { clicks[index]++ })).
		Child(Text(p.label)).
		Child(Div().Flex().ID("field").KeyContext("Field").TrackFocus(p.focus).W(Px(60)).H(Px(16)).
			Bg(Rgb(0x16a34a)).Focus(Styles().Bg(Rgb(0xf8fafc))))
	if p.index == 0 {
		panel.Child(Deferred(Div().Flex().KeyContext("Overlay").Absolute().Left(Px(150)).Top(Px(10)).
			W(Px(30)).H(Px(30)).Bg(Rgb(0xeab308))).Priority(1))
	}
	return panel.IntoElement()
}

// vcRoot lays its panels out in a 4+2 grid of fixed cells, cached or not.
type vcRoot struct {
	panels  []Entity[vcPanel]
	cached  bool
	renders int
}

func (r *vcRoot) Render(w *Window, cx *Context[vcRoot]) AnyElement {
	r.renders++
	grid := Div().KeyContext("Root").SizeFull().Flex().FlexRow().FlexWrap()
	for _, panel := range r.panels {
		var child any = ViewOf(panel)
		if r.cached {
			child = CachedView(ViewOf(panel), Styles().SizeFull())
		}
		grid.Child(Div().Flex().W(Px(200)).H(Px(100)).Child(child))
	}
	return grid.IntoElement()
}

// vcWindow is one side of the differential test.
type vcWindow struct {
	window *Window
	root   Entity[vcRoot]
	panels []Entity[vcPanel]
	focus  []FocusHandle
	clicks []int
}

func newVCWindow(t *testing.T, ta *TestApp, cached, realText bool) *vcWindow {
	t.Helper()
	app := ta.App()
	window := NewTestWindow(app, Size{Width: 800, Height: 600})
	t.Cleanup(func() { CloseTestWindow(window) })
	if realText {
		if err := UseRealTextSystem(window); err != nil {
			t.Fatal(err)
		}
	}
	side := &vcWindow{window: window, clicks: make([]int, 6)}
	for i := 0; i < 6; i++ {
		focus := app.FocusHandle()
		side.focus = append(side.focus, focus)
		side.panels = append(side.panels, NewEntity(app, window.Scope(), func(p *vcPanel, _ *Context[vcPanel]) {
			p.index, p.label, p.focus, p.clicks = i, fmt.Sprintf("panel %d", i), focus, side.clicks
		}))
	}
	side.root = NewEntity(app, window.Scope(), func(r *vcRoot, _ *Context[vcRoot]) {
		r.panels, r.cached = side.panels, cached
	})
	window.SetRootView(ViewOf(side.root))
	ta.RunUntilParked()
	if WindowDrawCount(window) == 0 {
		if _, err := DrawWindowFrame(window); err != nil {
			t.Fatal(err)
		}
	}
	return side
}

// vcFrameDump renders one window's last frame into comparable text: the
// scene's primitives (draw orders included) and plan, the hitboxes in
// order, the hit test at sample points (as hitbox positions — reused
// hitboxes keep their old ids), the dispatch tree, the cursor and focus.
func vcFrameDump(t *testing.T, side *vcWindow) string {
	t.Helper()
	w := side.window
	var b strings.Builder
	scene := LastDrawnScene(w)
	quads, err := scene.Quads()
	if err != nil {
		t.Fatal(err)
	}
	shadows, _ := scene.Shadows()
	underlines, _ := scene.Underlines()
	mono, _ := scene.MonochromeSprites()
	subpixel, _ := scene.SubpixelSprites()
	for _, s := range subpixel {
		fmt.Fprintf(&b, "subpixel %+v\n", s)
	}
	poly, _ := scene.PolychromeSprites()
	commands, err := scene.Commands()
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range quads {
		fmt.Fprintf(&b, "quad order %d bounds %v mask %v bg %v border %v %v\n", q.Order, q.Bounds, q.ContentMask, q.Background, q.BorderColor, q.BorderWidths)
	}
	fmt.Fprintf(&b, "shadows %+v\nunderlines %+v\n", shadows, underlines)
	for _, s := range mono {
		fmt.Fprintf(&b, "mono %+v\n", s)
	}
	fmt.Fprintf(&b, "poly %+v\n", poly)
	for _, c := range commands {
		fmt.Fprintf(&b, "plan %+v\n", c)
	}

	rendered := focusState(w).rendered
	position := map[uint64]int{}
	for i, record := range rendered.hitboxes {
		position[record.box.id] = i
		fmt.Fprintf(&b, "hitbox %d %v mask %v behavior %v\n", i, record.box.Bounds, record.box.contentMask, record.box.behavior)
	}
	for y := float32(5); y < 220; y += 9 {
		for x := float32(5); x < 800; x += 13 {
			test := rendered.hitTestAt(Point{X: x, Y: y})
			var hits []int
			for _, id := range test.orderedIDs {
				hits = append(hits, position[id])
			}
			fmt.Fprintf(&b, "hit %v,%v %v/%d\n", x, y, hits, test.hoverHitboxCount)
		}
	}

	tree := rendered.dispatchTree
	for i, node := range tree.nodes {
		context := ""
		if node.context != nil {
			context = node.context.String()
		}
		fmt.Fprintf(&b, "node %d parent %v/%v ctx %q focus %v view %v listeners %d/%d/%d\n", i, node.parent.Index, node.hasParent,
			context, node.hasFocusID, node.hasViewID, len(node.keyListeners), len(node.actionListeners), len(node.modifiersChangedListeners))
	}
	fmt.Fprintf(&b, "mouse listeners %d tab stops %d\n", len(rendered.mouseListeners), len(rendered.tabStops.order))
	cursor, ok := WindowCursorStyle(w)
	fmt.Fprintf(&b, "cursor %v %v\n", cursor, ok)
	focused := -1
	if result := w.Focused(); result.Ok {
		for i, handle := range side.focus {
			if handle.id == result.Handle.id {
				focused = i
			}
		}
	}
	var contexts []string
	if path, _ := w.focusDispatchPath(); len(path) > 0 {
		for _, id := range path {
			if ctx := tree.nodes[id.Index].context; ctx != nil {
				contexts = append(contexts, ctx.String())
			}
		}
	}
	fmt.Fprintf(&b, "focused panel %d path %v clicks %v deferred %v\n", focused, contexts, side.clicks, drawState(w).lastDeferredPriorities)
	return b.String()
}

// TestViewCacheDifferential is ticket 04's differential test: a cached and
// an uncached window go through the same frames — a root re-render (every
// panel reused), a partial update of one panel, hovering, clicking, focus
// moves (which refresh, R:window.rs Window::focus) and keyboard focus
// traversal, a reuse with the focus inside a reused panel, and a window
// refresh — and must produce the same scene, hit testing, dispatch tree,
// cursor, focus and click delivery after every frame.
func TestViewCacheDifferential(t *testing.T) {
	t.Run("test-text", func(t *testing.T) { runViewCacheDifferential(t, false) })
	// The real text stack shapes through the window's line-layout cache
	// (reused ranges carry their lines, reuse_layouts) and paints glyph
	// sprites into the window atlas (replayed by Scene::replay).
	t.Run("real-text", func(t *testing.T) { runViewCacheDifferential(t, true) })
}

func runViewCacheDifferential(t *testing.T, realText bool) {
	ta := NewTestApp()
	app := ta.App()
	cached := newVCWindow(t, ta, true, realText)
	plain := newVCWindow(t, ta, false, realText)
	cachedCx := NewVisualTestContext(cached.window, ta)
	plainCx := NewVisualTestContext(plain.window, ta)

	both := func(f func(side *vcWindow, cx *VisualTestContext)) {
		f(cached, cachedCx)
		f(plain, plainCx)
		ta.RunUntilParked()
	}
	compare := func(step string) {
		t.Helper()
		got, want := vcFrameDump(t, cached), vcFrameDump(t, plain)
		if got != want {
			gotLines, wantLines := strings.Split(got, "\n"), strings.Split(want, "\n")
			for i := range min(len(gotLines), len(wantLines)) {
				if gotLines[i] != wantLines[i] {
					t.Fatalf("%s: cached and uncached frames differ at line %d:\ncached:   %s\nuncached: %s", step, i, gotLines[i], wantLines[i])
				}
			}
			t.Fatalf("%s: cached and uncached frames differ in length (%d vs %d lines)", step, len(gotLines), len(wantLines))
		}
	}
	compare("first frame")
	if realText {
		if _, mono, subpixel, _, err := WindowPrimitiveCounts(cached.window); err != nil || mono+subpixel == 0 {
			t.Fatalf("real text painted no glyph sprites (mono %d subpixel %d, %v)", mono, subpixel, err)
		}
	}

	// A platform click arrives after the pointer moved there: the move
	// flips the hover states (and notifies the views that own them). A
	// click with no move would leave a reused view's hover paint stale
	// exactly as in the pin, where only the views whose hover listener
	// fired are notified.
	click := func(cx *VisualTestContext, at Point) {
		cx.SimulateMouseMove(at, nil, Modifiers{})
		cx.SimulateClick(at, Modifiers{})
	}

	notifyRoot := func(side *vcWindow, _ *VisualTestContext) {
		side.root.Update(app, func(_ *vcRoot, cx *Context[vcRoot]) { cx.Notify() })
	}
	hits := drawState(cached.window).viewCacheHits
	both(notifyRoot)
	compare("root re-render")
	if got := drawState(cached.window).viewCacheHits - hits; got != 6 {
		t.Fatalf("root re-render reused %d panels, want 6", got)
	}
	both(notifyRoot)
	compare("second reuse")

	hits, misses := drawState(cached.window).viewCacheHits, drawState(cached.window).viewCacheMisses
	both(func(side *vcWindow, _ *VisualTestContext) {
		side.panels[2].Update(app, func(p *vcPanel, cx *Context[vcPanel]) {
			p.label = "panel 2 edited"
			cx.Notify()
		})
	})
	compare("partial update")
	if gotHits, gotMisses := drawState(cached.window).viewCacheHits-hits, drawState(cached.window).viewCacheMisses-misses; gotHits != 5 || gotMisses != 1 {
		t.Fatalf("partial update: %d reused, %d rendered; want 5 and 1", gotHits, gotMisses)
	}

	both(func(_ *vcWindow, cx *VisualTestContext) { cx.SimulateMouseMove(Point{X: 230, Y: 15}, nil, Modifiers{}) })
	compare("hover panel 1 button")
	both(func(_ *vcWindow, cx *VisualTestContext) { click(cx, Point{X: 430, Y: 15}) })
	compare("click panel 2 button")
	both(func(_ *vcWindow, cx *VisualTestContext) { click(cx, Point{X: 630, Y: 15}) })
	compare("click panel 3 button")
	if cached.clicks[2] != 1 || cached.clicks[3] != 1 {
		t.Fatalf("clicks = %v, want panels 2 and 3 clicked once", cached.clicks)
	}
	both(func(_ *vcWindow, cx *VisualTestContext) {
		cx.SimulateMouseMove(Point{X: 700, Y: 500}, nil, Modifiers{})
	})
	compare("hover away")

	both(func(side *vcWindow, _ *VisualTestContext) { side.focus[4].Focus(side.window) })
	compare("focus panel 4")
	hits = drawState(cached.window).viewCacheHits
	both(notifyRoot)
	compare("reuse with focus inside a reused panel")
	if got := drawState(cached.window).viewCacheHits - hits; got != 6 {
		t.Fatalf("reuse with focus: %d panels reused, want 6", got)
	}
	both(func(side *vcWindow, _ *VisualTestContext) {
		side.window.FocusNext()
	})
	compare("focus next")
	both(notifyRoot)
	compare("reuse after focus next")
	both(func(side *vcWindow, _ *VisualTestContext) { side.window.Refresh() })
	compare("window refresh")
	both(func(_ *vcWindow, cx *VisualTestContext) { click(cx, Point{X: 230, Y: 15}) })
	compare("click panel 1 after reuse")
	if cached.clicks[1] != 1 {
		t.Fatalf("clicks = %v, want panel 1 clicked once", cached.clicks)
	}
}

// TestViewCacheRefreshBypassesCache pins R:view.rs:435 / R:window.rs:
// 2287-2292: a window refresh renders every cached view afresh, a
// notification of the root alone reuses them, and a notified panel (a
// view in the rendered dispatch tree, R:window.rs:2210-2222) renders while
// its siblings are reused.
func TestViewCacheRefreshBypassesCache(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	side := newVCWindow(t, ta, true, false)
	ds := drawState(side.window)
	if ds.viewCacheMisses != 6 || ds.viewCacheHits != 0 {
		t.Fatalf("first frame: %d rendered, %d reused; want 6 and 0", ds.viewCacheMisses, ds.viewCacheHits)
	}
	side.window.Refresh()
	ta.RunUntilParked()
	if ds.viewCacheMisses != 12 || ds.viewCacheHits != 0 {
		t.Fatalf("after refresh: %d rendered, %d reused; want 12 and 0", ds.viewCacheMisses, ds.viewCacheHits)
	}
	side.root.Update(app, func(_ *vcRoot, cx *Context[vcRoot]) { cx.Notify() })
	ta.RunUntilParked()
	if ds.viewCacheMisses != 12 || ds.viewCacheHits != 6 {
		t.Fatalf("after root notify: %d rendered, %d reused; want 12 and 6", ds.viewCacheMisses, ds.viewCacheHits)
	}
	side.panels[5].Update(app, func(_ *vcPanel, cx *Context[vcPanel]) { cx.Notify() })
	ta.RunUntilParked()
	if ds.viewCacheMisses != 13 || ds.viewCacheHits != 11 {
		t.Fatalf("after panel notify: %d rendered, %d reused; want 13 and 11", ds.viewCacheMisses, ds.viewCacheHits)
	}
	if len(ds.dirtyViews) != 0 || len(ds.invalidatedViews) != 0 || ds.refreshing {
		t.Fatalf("dirty state left after the frame: dirty %v invalidated %v refreshing %v", ds.dirtyViews, ds.invalidatedViews, ds.refreshing)
	}
}

// TestViewCacheBoundsChangeRerenders pins the cache key (R:view.rs:
// 430-433): a cached view whose bounds change renders again.
func TestViewCacheBoundsChangeRerenders(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	var width float32 = 200
	window := NewTestWindow(app, Size{Width: 800, Height: 600})
	t.Cleanup(func() { CloseTestWindow(window) })
	panel := NewEntity(app, window.Scope(), func(p *vcPanel, _ *Context[vcPanel]) {
		p.label, p.focus, p.clicks = "solo", app.FocusHandle(), make([]int, 1)
	})
	root := NewEntity(app, window.Scope(), func(v *paintView, _ *Context[paintView]) {
		v.build = func(*paintView, *Window, *Context[paintView]) AnyElement {
			return Div().Flex().Child(Div().Flex().W(Px(width)).H(Px(100)).Child(CachedView(ViewOf(panel), Styles().SizeFull()))).IntoElement()
		}
	})
	window.SetRootView(ViewOf(root))
	ta.RunUntilParked()
	ds := drawState(window)
	root.Update(app, func(_ *paintView, cx *Context[paintView]) { cx.Notify() })
	ta.RunUntilParked()
	if ds.viewCacheHits != 1 {
		t.Fatalf("unchanged bounds: %d reuses, want 1", ds.viewCacheHits)
	}
	width = 240
	root.Update(app, func(_ *paintView, cx *Context[paintView]) { cx.Notify() })
	ta.RunUntilParked()
	if ds.viewCacheHits != 1 || ds.viewCacheMisses != 2 {
		t.Fatalf("changed bounds: %d reuses, %d renders; want 1 and 2", ds.viewCacheHits, ds.viewCacheMisses)
	}
}

// TestCachedViewRequiresEntity pins the pinned crate-private cached():
// only entity-backed views can be cached.
func TestCachedViewRequiresEntity(t *testing.T) {
	defer func() {
		if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), "entity-backed") {
			t.Fatalf("recover() = %v, want the entity-backed panic", r)
		}
	}()
	CachedView(statelessView{recipe: nil}, Styles().SizeFull())
}
