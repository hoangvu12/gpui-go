package gpui

// browser-core ticket 58's damage-rule tests (damage.go): the reused
// view ranges at the same bounds and mask are undamaged (Chrome's
// chunk rule, raster_invalidator.cc:242-249 — the cached view is the
// cached subsequence), everything else damages its old and new bounds
// (raster_invalidator.cc:268-275), disappeared content damages its old
// bounds (raster_invalidator.cc:308-325), and any frame whose damage
// the rule cannot prove falls back to a full redraw (the in-order rule
// of raster_invalidator.cc:51-66; the first frame; geometry changes;
// log drift). The pixel-identity half (a damaged present equal to a
// full redraw) runs on the real GPU in internal/rendererspec
// (r58_damage_frame_test.go); these tests pin the rule itself on
// deterministic test windows.

import (
	"fmt"
	"testing"
)

// dmPanel is one cached panel: a hoverable colored box (the hover style
// drives the notify, exactly like the pin's cx.notify(current_view)).
type dmPanel struct {
	index int
	light float32
}

func (p *dmPanel) Render(w *Window, cx *Context[dmPanel]) AnyElement {
	light := p.light
	hue := float32(p.index%7) / 7
	return Div().
		ID(fmt.Sprintf("dm-panel-%d", p.index)).
		SizeFull().
		Bg(Hsla{H: hue, S: 0.6, L: light, A: 1}).
		Hover(Styles().Bg(Hsla{H: hue, S: 0.6, L: light + 0.2, A: 1})).
		IntoElement()
}

// dmRoot lays its panels out in a 4+2 grid of fixed cells, cached or
// not, with a removal/reorder switch the tests flip.
type dmRoot struct {
	panels  []Entity[dmPanel]
	cached  bool
	drop    int // the panel index to leave out (-1 for none)
	reorder bool
}

func (r *dmRoot) Render(w *Window, cx *Context[dmRoot]) AnyElement {
	grid := Div().ID("dm-root").SizeFull().Flex().FlexRow().FlexWrap()
	panels := append([]Entity[dmPanel](nil), r.panels...)
	if r.drop >= 0 && r.drop < len(panels) {
		panels = append(panels[:r.drop], panels[r.drop+1:]...)
	}
	if r.reorder && len(panels) >= 2 {
		panels = append([]Entity[dmPanel]{panels[1], panels[0]}, panels[2:]...)
	}
	for _, panel := range panels {
		var child any = ViewOf(panel)
		if r.cached {
			child = CachedView(ViewOf(panel), Styles().SizeFull())
		} else {
			// browser-core 59: the plain side of the damage corpus is the
			// reuse-opt-out side — without it the automatic reuse replays
			// the unchanged panels (the ticket's own change), and the
			// "no reused ranges" premise this test pins (the conservative
			// rule's arm) needs a window that really has none.
			child = WithoutViewReuse(ViewOf(panel))
		}
		grid.Child(Div().Flex().W(Px(200)).H(Px(100)).Child(child))
	}
	return grid.IntoElement()
}

// dmWindow is one damage test window.
type dmWindow struct {
	window *Window
	root   Entity[dmRoot]
	panels []Entity[dmPanel]
}

func newDMWindow(t *testing.T, ta *TestApp, cached bool) *dmWindow {
	t.Helper()
	app := ta.App()
	window := NewTestWindow(app, Size{Width: 800, Height: 600})
	t.Cleanup(func() { CloseTestWindow(window) })
	side := &dmWindow{window: window}
	for i := 0; i < 6; i++ {
		side.panels = append(side.panels, NewEntity(app, window.Scope(), func(p *dmPanel, _ *Context[dmPanel]) {
			p.index, p.light = i, 0.5
		}))
	}
	side.root = NewEntity(app, window.Scope(), func(r *dmRoot, _ *Context[dmRoot]) {
		r.panels, r.cached, r.drop = side.panels, cached, -1
	})
	window.SetRootView(ViewOf(side.root))
	ta.RunUntilParked()
	return side
}

// dmPanelRect is panel i's expected device damage rect: the 200x100
// cell in the 4+2 wrap grid (the window is 800x600 logical; the two
// wrap lines stretch to 300 logical each, so the second line starts
// at y=300), scaled by the test window's 2.0 factor.
func dmPanelRect(i int) DamageRect {
	return DamageRect{
		X: int32((i % 4) * 400),
		Y: int32((i / 4) * 600),
		W: 400,
		H: 200,
	}
}

// requireDamage asserts the window's last frame damaged exactly one
// rect (not a full redraw) and reports it.
func requireDamage(t *testing.T, w *Window, step string) DamageRect {
	t.Helper()
	rects, full := w.LastFrameDamage()
	if full {
		t.Fatalf("%s: damage is a full redraw, want a partial one", step)
	}
	if len(rects) != 1 {
		t.Fatalf("%s: damage is %d rects %v, want exactly 1", step, len(rects), rects)
	}
	return rects[0]
}

// TestDamageFirstFrameIsFull: the first frame of a window has no
// previous damage state to diff against — a full redraw.
func TestDamageFirstFrameIsFull(t *testing.T) {
	ta := NewTestApp()
	side := newDMWindow(t, ta, true)
	rects, full := side.window.LastFrameDamage()
	if !full || rects != nil {
		t.Fatalf("first frame damage = (%v, full=%v), want (nil, true)", rects, full)
	}
}

// TestDamageHoverChangeDamagesOnlyHoveredPanel: an idle hover change
// (the mouse moving onto a panel — the pin's cx.notify(current_view))
// damages only the hovered panel's bounds; every other panel replayed
// its range. Moving out damages the same bounds again.
func TestDamageHoverChangeDamagesOnlyHoveredPanel(t *testing.T) {
	ta := NewTestApp()
	side := newDMWindow(t, ta, true)
	cx := NewVisualTestContext(side.window, ta)

	draws := WindowDrawCount(side.window)
	cx.SimulateMouseMove(Point{X: 500, Y: 50}, nil, Modifiers{})
	if WindowDrawCount(side.window) != draws+1 {
		t.Fatalf("hover-in drew %d frames, want 1", WindowDrawCount(side.window)-draws)
	}
	if got := requireDamage(t, side.window, "hover in"); got != dmPanelRect(2) {
		t.Fatalf("hover-in damage = %v, want panel 2's bounds %v", got, dmPanelRect(2))
	}

	// Hover out: the same panel's hover style drops — its own bounds
	// again.
	draws = WindowDrawCount(side.window)
	cx.SimulateMouseMove(Point{X: 500, Y: 550}, nil, Modifiers{})
	if WindowDrawCount(side.window) != draws+1 {
		t.Fatalf("hover-out drew %d frames, want 1", WindowDrawCount(side.window)-draws)
	}
	if got := requireDamage(t, side.window, "hover out"); got != dmPanelRect(2) {
		t.Fatalf("hover-out damage = %v, want panel 2's bounds %v", got, dmPanelRect(2))
	}
}

// TestDamageNotifiedPanelDamagesItsBounds: a notification of one
// panel's entity (a state change inside it) re-renders that panel and
// replays every sibling — the damage is the notified panel's bounds.
func TestDamageNotifiedPanelDamagesItsBounds(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	side := newDMWindow(t, ta, true)
	draws := WindowDrawCount(side.window)
	side.panels[5].Update(app, func(p *dmPanel, cx *Context[dmPanel]) {
		p.light = 0.8
		cx.Notify()
	})
	if WindowDrawCount(side.window) != draws+1 {
		t.Fatalf("panel notify drew %d frames, want 1", WindowDrawCount(side.window)-draws)
	}
	if got := requireDamage(t, side.window, "panel notify"); got != dmPanelRect(5) {
		t.Fatalf("panel notify damage = %v, want panel 5's bounds %v", got, dmPanelRect(5))
	}
}

// TestDamageRootNotifyReplaysEverything: a notification of the root
// alone re-renders nothing but the root — every cached panel replays,
// no op changed anywhere, and the frame's damage is empty (the present
// step has nothing to repaint).
func TestDamageRootNotifyReplaysEverything(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	side := newDMWindow(t, ta, true)
	draws := WindowDrawCount(side.window)
	side.root.Update(app, func(r *dmRoot, cx *Context[dmRoot]) {
		cx.Notify()
	})
	if WindowDrawCount(side.window) != draws+1 {
		t.Fatalf("root notify drew %d frames, want 1", WindowDrawCount(side.window)-draws)
	}
	rects, full := side.window.LastFrameDamage()
	if full || len(rects) != 0 {
		t.Fatalf("root notify damage = (%v, full=%v), want an empty partial one", rects, full)
	}
}

// TestDamageRemovalDamagesOldBounds: a panel that leaves the tree
// damages the bounds it painted last frame (the trailing sweep of
// raster_invalidator.cc:308-325); the remaining panels replay.
func TestDamageRemovalDamagesOldBounds(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	side := newDMWindow(t, ta, true)
	// First a full root re-render so every panel's range is current
	// (the first frame's fresh render).
	side.root.Update(app, func(r *dmRoot, cx *Context[dmRoot]) { cx.Notify() })
	draws := WindowDrawCount(side.window)
	side.root.Update(app, func(r *dmRoot, cx *Context[dmRoot]) {
		r.drop = 5
		cx.Notify()
	})
	if WindowDrawCount(side.window) != draws+1 {
		t.Fatalf("removal drew %d frames, want 1", WindowDrawCount(side.window)-draws)
	}
	if got := requireDamage(t, side.window, "panel removal"); got != dmPanelRect(5) {
		t.Fatalf("panel removal damage = %v, want the removed panel's old bounds %v", got, dmPanelRect(5))
	}
}

// TestDamageReorderSwapsDamageBothBounds: a swapped tree re-renders
// the two moved panels (their cells moved, so their cache keys missed)
// and replays every other panel — the damage is the swapped pair's old
// and new bounds, the union of the two cells (Chromium's moved-chunk
// answer: the jumped-over chunk takes old ∪ new,
// raster_invalidator.cc:268-275).
func TestDamageReorderSwapsDamageBothBounds(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	side := newDMWindow(t, ta, true)
	side.root.Update(app, func(r *dmRoot, cx *Context[dmRoot]) { cx.Notify() })
	side.root.Update(app, func(r *dmRoot, cx *Context[dmRoot]) {
		r.reorder = true
		cx.Notify()
	})
	got := requireDamage(t, side.window, "reorder swap")
	want := DamageRect{X: 0, Y: 0, W: 800, H: 200}
	if got != want {
		t.Fatalf("reorder swap damage = %v, want the swapped pair's cells %v", got, want)
	}
}

// TestDamageOutOfOrderReplayFallsBackToFull: two absolutely-positioned
// cached views keep their bounds when the tree reorders them (the
// cache keys still match, so both ranges REPLAY) — but the later range
// now replays first, the in-order rule of MatchNewChunkToOldChunk
// (raster_invalidator.cc:51-66) is broken, and a frame whose damage the
// rule cannot prove falls back to a full redraw.
func TestDamageOutOfOrderReplayFallsBackToFull(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	window := NewTestWindow(app, Size{Width: 800, Height: 600})
	t.Cleanup(func() { CloseTestWindow(window) })
	panels := make([]Entity[dmPanel], 2)
	for i := range panels {
		panels[i] = NewEntity(app, window.Scope(), func(p *dmPanel, _ *Context[dmPanel]) {
			p.index, p.light = i, 0.5
		})
	}
	root := NewEntity(app, window.Scope(), func(r *dmReorderRoot, _ *Context[dmReorderRoot]) {
		r.panels = panels
	})
	window.SetRootView(ViewOf(root))
	ta.RunUntilParked()
	if _, full := window.LastFrameDamage(); !full {
		t.Fatal("the first frame must be a full redraw")
	}
	root.Update(app, func(r *dmReorderRoot, cx *Context[dmReorderRoot]) { cx.Notify() })
	if rects, full := window.LastFrameDamage(); full || len(rects) != 0 {
		t.Fatalf("in-order replay damage = (%v, full=%v), want an empty partial one", rects, full)
	}
	// Reorder the two absolute views: both keys still match (their
	// bounds are Left/Top-pinned), both ranges replay — out of order.
	// The rule cannot prove this frame; it must fall back to full.
	root.Update(app, func(r *dmReorderRoot, cx *Context[dmReorderRoot]) {
		r.swapped = true
		cx.Notify()
	})
	if rects, full := window.LastFrameDamage(); !full {
		t.Fatalf("out-of-order replay damage = (%v, full=false), want a full redraw", rects)
	}
}

// dmReorderRoot renders two absolutely-positioned cached views whose
// tree order the test swaps. Each panel keeps its OWN Left/Top (the
// wrapper position follows the panel, not the tree slot), so the swap
// keeps every cache key matching while the paint order reverses.
type dmReorderRoot struct {
	panels  []Entity[dmPanel]
	swapped bool
}

func (r *dmReorderRoot) Render(w *Window, cx *Context[dmReorderRoot]) AnyElement {
	root := Div().ID("dm-reorder-root").Relative().SizeFull()
	slots := []int{0, 1}
	if r.swapped {
		slots = []int{1, 0}
	}
	for _, i := range slots {
		left := float32(i * 400)
		top := float32(i * 300)
		root.Child(Div().Flex().Absolute().
			Left(Px(left)).Top(Px(top)).W(Px(200)).H(Px(100)).
			Child(CachedView(ViewOf(r.panels[i]), Styles().SizeFull())))
	}
	return root.IntoElement()
}

// TestDamageUncachedWindowDamagesAllContent: a window without view
// reuse (browser-core 59's per-view opt-out — the pre-59 "uncached"
// shape) re-renders everything, so the damage covers every panel's
// bounds (the conservative rule — the savings come from the reused
// ranges; a no-reuse window has none). The unpainted gaps between the
// wrap lines never change, so the damage stays the two painted bands.
func TestDamageUncachedWindowDamagesAllContent(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	side := newDMWindow(t, ta, false)
	side.panels[2].Update(app, func(p *dmPanel, cx *Context[dmPanel]) {
		p.light = 0.8
		cx.Notify()
	})
	rects, full := side.window.LastFrameDamage()
	if full {
		t.Fatalf("uncached window damage is flagged full, want the painted bands %v", rects)
	}
	want := []DamageRect{
		{X: 0, Y: 0, W: 1600, H: 200},
		{X: 0, Y: 600, W: 800, H: 200},
	}
	if len(rects) != len(want) {
		t.Fatalf("uncached window damage = %v, want %v", rects, want)
	}
	for i := range want {
		if rects[i] != want[i] {
			t.Fatalf("uncached window damage = %v, want %v", rects, want)
		}
	}
}

// TestDamageResizeFallsBackToFull: a viewport change re-resolves every
// cached view's key and the whole target — a full redraw.
func TestDamageResizeFallsBackToFull(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	side := newDMWindow(t, ta, true)
	// The test window's geometry lives in its draw state; a resize
	// writes it (the real window's WM_SIZE path).
	drawState(side.window).viewport = Size{Width: 640, Height: 480}
	side.root.Update(app, func(r *dmRoot, cx *Context[dmRoot]) { cx.Notify() })
	rects, full := side.window.LastFrameDamage()
	if !full {
		t.Fatalf("resize damage = (%v, full=false), want a full redraw", rects)
	}
}

// dmScrollRow is one cached row of the scroll test's column.
type dmScrollRow struct {
	index int
}

func (r *dmScrollRow) Render(w *Window, cx *Context[dmScrollRow]) AnyElement {
	return Div().
		ID(fmt.Sprintf("dm-row-%d", r.index)).
		SizeFull().
		Bg(Hsla{H: float32(r.index) / 16, S: 0.5, L: 0.4, A: 1}).
		IntoElement()
}

// dmScrollRoot is a scrollable column of cached rows.
type dmScrollRoot struct {
	rows   []Entity[dmScrollRow]
	scroll ScrollHandle
}

func (r *dmScrollRoot) Render(w *Window, cx *Context[dmScrollRoot]) AnyElement {
	column := Div().ID("dm-scroller").Flex().FlexCol().OverflowYScroll().TrackScroll(r.scroll).
		SizeFull()
	for _, row := range r.rows {
		column.Child(Div().Flex().FlexShrink0().W(Px(300)).H(Px(50)).Child(CachedView(ViewOf(row), Styles().SizeFull())))
	}
	return column.IntoElement()
}

// TestDamageScrollDamagesScroller: scrolling moves the column's cached
// rows (every row's bounds change — cache misses, old ∪ new bounds);
// the merged damage stays inside the scroller's bounds instead of the
// whole window.
func TestDamageScrollDamagesScroller(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	window := NewTestWindow(app, Size{Width: 400, Height: 300})
	t.Cleanup(func() { CloseTestWindow(window) })
	root := NewEntity(app, window.Scope(), func(r *dmScrollRoot, _ *Context[dmScrollRoot]) {
		for i := 0; i < 12; i++ {
			r.rows = append(r.rows, NewEntity(app, window.Scope(), func(row *dmScrollRow, _ *Context[dmScrollRow]) {
				row.index = i
			}))
		}
		r.scroll = NewScrollHandle()
	})
	window.SetRootView(ViewOf(root))
	ta.RunUntilParked()
	// The first frame is a full redraw; the scroller fills the window.
	if _, full := window.LastFrameDamage(); !full {
		t.Fatal("the scroll window's first frame must be a full redraw")
	}

	root.Update(app, func(r *dmScrollRoot, cx *Context[dmScrollRoot]) {
		r.scroll.SetOffset(Point{X: 0, Y: -60})
		cx.Notify()
	})
	rects, full := window.LastFrameDamage()
	if full {
		t.Fatal("scroll damage is a full redraw, want the scroller's region")
	}
	if len(rects) == 0 {
		t.Fatal("scroll damage is empty")
	}
	// Every rect inside the scroller (the window IS the scroller here —
	// 400x300 logical, 800x600 device) and the union substantial: the
	// rows moved 60 logical pixels, so the merged damage must cover a
	// visible slice of the column.
	union := rects[0]
	for _, r := range rects[1:] {
		union = damageRectUnion(union, r)
	}
	if union.X < 0 || union.Y < 0 || union.X+union.W > 800 || union.Y+union.H > 600 {
		t.Fatalf("scroll damage union %v escapes the scroller's device bounds (800x600)", union)
	}
	if union.H < 200 {
		t.Fatalf("scroll damage union %v is too small; the moved rows' old and new bounds should cover a large slice", union)
	}
}

// TestPaintFlashingTintsDamage: with the overlay on, each frame's
// damage rects are tinted into the scene (one quad per merged rect,
// alternating red and green; a full-redraw frame tints the whole
// viewport) — and the tints do not enter the frame's own damage.
func TestPaintFlashingTintsDamage(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	// The window is built by hand so the overlay is on before the first
	// (full) frame draws.
	window := NewTestWindow(app, Size{Width: 800, Height: 600})
	t.Cleanup(func() { CloseTestWindow(window) })
	window.SetPaintFlashing(true)
	if !window.PaintFlashing() {
		t.Fatal("PaintFlashing() = false after SetPaintFlashing(true)")
	}
	side := &dmWindow{window: window}
	for i := 0; i < 6; i++ {
		side.panels = append(side.panels, NewEntity(app, window.Scope(), func(p *dmPanel, _ *Context[dmPanel]) {
			p.index, p.light = i, 0.5
		}))
	}
	side.root = NewEntity(app, window.Scope(), func(r *dmRoot, _ *Context[dmRoot]) {
		r.panels, r.cached, r.drop = side.panels, true, -1
	})
	window.SetRootView(ViewOf(side.root))
	ta.RunUntilParked()

	// The first frame is a full redraw: one viewport-sized tint quad on
	// top of the panels' repaint.
	rects, full := window.LastFrameDamage()
	if !full {
		t.Fatalf("first frame damage = (%v, full=false), want a full redraw", rects)
	}
	quads, err := LastDrawnScene(window).Quads()
	if err != nil {
		t.Fatal(err)
	}
	if len(quads) != 7 {
		t.Fatalf("full-redraw flash painted %d quads, want the 6 panels plus one viewport-sized tint", len(quads))
	}
	if q := quads[6]; q.Bounds != (Bounds{Origin: Point{}, Size: Size{Width: 1600, Height: 1200}}) {
		t.Fatalf("full-redraw flash tint bounds = %v, want the device viewport", q.Bounds)
	}
	first := quads[6].Background

	// A hover change: the damage is the hovered panel's bounds and the
	// tint quad sits exactly on it, in the alternating color.
	cx := NewVisualTestContext(window, ta)
	cx.SimulateMouseMove(Point{X: 500, Y: 50}, nil, Modifiers{})
	if got := requireDamage(t, window, "hover with flashing"); got != dmPanelRect(2) {
		t.Fatalf("hover damage with flashing = %v, want panel 2's bounds %v", got, dmPanelRect(2))
	}
	quads, err = LastDrawnScene(window).Quads()
	if err != nil {
		t.Fatal(err)
	}
	// The scene always carries every panel's ops (the replayed ranges
	// copy them in); the OVERLAY is the one quad at the damage rect in
	// the alternating tint color.
	want := Bounds{
		Origin: Point{X: 800, Y: 0},
		Size:   Size{Width: 400, Height: 200},
	}
	var tint Quad
	tints := 0
	for _, q := range quads {
		if q.Bounds == want && q.Background.Solid.A > 0.05 && q.Background.Solid.A < 0.9 {
			tint = q
			tints++
		}
	}
	if tints != 1 {
		t.Fatalf("hover flash painted %d tint quads at the damage rect, want 1 (quads: %v)", tints, quads)
	}
	if tint.Background == first {
		t.Fatal("the flash tint did not alternate between frames")
	}
}

// TestDamageCaptureSurfaceAlwaysDamages: a capture-backed scene
// surface's content updates outside the scene's knowledge, so its
// bounds damage even inside a fully replayed range (Chromium's foreign
// layers self-invalidate — raster_invalidator.cc:168-171; the port's
// surface record cannot see the frame update, so it always damages).
func TestDamageCaptureSurfaceAlwaysDamages(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	window := NewTestWindow(app, Size{Width: 400, Height: 300})
	t.Cleanup(func() { CloseTestWindow(window) })

	capture := NewEntity(app, window.Scope(), func(s *dmCaptureView, _ *Context[dmCaptureView]) {})
	root := NewEntity(app, window.Scope(), func(r *dmCaptureRoot, _ *Context[dmCaptureRoot]) {
		r.capture = capture
	})
	window.SetRootView(ViewOf(root))
	ta.RunUntilParked()
	if _, full := window.LastFrameDamage(); !full {
		t.Fatal("the capture window's first frame must be a full redraw")
	}

	// A root notification replays the cached capture view (nothing
	// changes) — the surface op is inside the replayed range, and its
	// bounds still damage.
	root.Update(app, func(r *dmCaptureRoot, cx *Context[dmCaptureRoot]) { cx.Notify() })
	rects, full := window.LastFrameDamage()
	if full {
		t.Fatalf("root notify damage = full, want the capture surface's bounds")
	}
	want := DamageRect{X: 0, Y: 0, W: 800, H: 600}
	if len(rects) != 1 || rects[0] != want {
		t.Fatalf("root notify damage = %v, want the capture surface's bounds %v", rects, want)
	}
}

// dmCaptureRoot renders one cached capture view in a fixed cell (the
// root itself paints nothing, so a root notification replays the
// cached range and nothing else can damage).
type dmCaptureRoot struct {
	capture Entity[dmCaptureView]
}

func (r *dmCaptureRoot) Render(w *Window, cx *Context[dmCaptureRoot]) AnyElement {
	return Div().ID("dm-capture-root").SizeFull().Flex().
		Child(Div().Flex().W(Px(400)).H(Px(300)).
			Child(CachedView(ViewOf(r.capture), Styles().SizeFull()))).
		IntoElement()
}

// dmCaptureView renders one capture-backed surface (the pinned
// Surface element with a WindowsScreenCaptureFrame source).
type dmCaptureView struct{}

func (s *dmCaptureView) Render(w *Window, cx *Context[dmCaptureView]) AnyElement {
	frame := ScreenCaptureFrame{Handle: 0x1234, Width: 640, Height: 480, ContentWidth: 640, ContentHeight: 480}
	return NewSurfaceElement(CaptureSurfaceSource(frame)).
		ObjectFit(ObjectFitFill).
		Size(PxLength(400), PxLength(300)).
		IntoElement()
}

// TestDamageLogMatchesNativeOpCount pins the kernel's Go-side damage
// log against the native paint-op count across every op kind — the
// log's index space must stay aligned (the native insertion core's
// drop rule replicated at every tracked insert; a dropped op leaves no
// array entry and no log entry), or every damage range read from it
// would be untrustworthy (damageLogDrifted's full-redraw fallback).
func TestDamageLogMatchesNativeOpCount(t *testing.T) {
	atlas, err := NewAtlas()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = atlas.Dispose() })
	// One tile per pool: alpha (monochrome), BGRA subpixel, BGRA color.
	tileOf := func(format RasterFormat, pixels []byte) AtlasTile {
		t.Helper()
		tile, err := atlas.InsertGlyph(&RasterizedGlyph{
			Width:  4,
			Height: 4,
			Format: format,
			Pixels: pixels,
		}, RasterGlyphParams{FontID: 1, GlyphID: 2, FontSize: 4, ScaleFactor: 1, RasterStyle: PreparedRasterStyle{Mode: GlyphRenderGrayscale}})
		if err != nil {
			t.Fatalf("InsertGlyph: %v", err)
		}
		return tile
	}
	alpha := make([]byte, 16)
	for i := range alpha {
		alpha[i] = 200
	}
	monoTile := tileOf(RasterFormatAlphaMask, alpha)
	subTile := tileOf(RasterFormatBgraSubpixelMask, make([]byte, 64))
	colorTile := tileOf(RasterFormatBgraColor, make([]byte, 64))

	full := Bounds{Origin: Point{}, Size: Size{Width: 4096, Height: 4096}}
	thin := Bounds{Origin: Point{X: 100, Y: 100}, Size: Size{Width: 5, Height: 5}}
	solid := SolidBackground(Hsla{H: 0, S: 1, L: 0.5, A: 1})

	scene, err := NewScene()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = scene.Dispose() })
	quad := func(x, y, w, h float32, mask Bounds) {
		t.Helper()
		if err := scene.InsertQuad(Quad{Bounds: Bounds{Origin: Point{X: x, Y: y}, Size: Size{Width: w, Height: h}}, ContentMask: mask, Background: solid}); err != nil {
			t.Fatalf("InsertQuad: %v", err)
		}
	}
	// Positioned, droppable ops.
	quad(0, 0, 10, 10, full)
	// Dropped: the clipped rect is empty (no array entry, no log entry).
	quad(0, 0, 10, 10, thin)
	quad(0, 0, 0, 10, full)
	if err := scene.InsertShadow(Shadow{Bounds: Bounds{Origin: Point{}, Size: Size{Width: 20, Height: 20}}, ContentMask: full, Color: solid}); err != nil {
		t.Fatalf("InsertShadow: %v", err)
	}
	if err := scene.InsertUnderline(Underline{Bounds: Bounds{Origin: Point{}, Size: Size{Width: 30, Height: 2}}, ContentMask: full, Color: Hsla{A: 1}}); err != nil {
		t.Fatalf("InsertUnderline: %v", err)
	}
	if err := scene.InsertBackdropFilter(BackdropFilter{Bounds: Bounds{Origin: Point{}, Size: Size{Width: 40, Height: 40}}, ContentMask: full}); err != nil {
		t.Fatalf("InsertBackdropFilter: %v", err)
	}
	if err := scene.InsertSurface(Bounds{Origin: Point{}, Size: Size{Width: 50, Height: 50}}, full, SurfaceSourceNone, 1); err != nil {
		t.Fatalf("InsertSurface: %v", err)
	}
	spriteBounds := Bounds{Origin: Point{X: 60, Y: 60}, Size: Size{Width: 8, Height: 8}}
	if err := scene.InsertMonochromeSprite(MonochromeSprite{Bounds: spriteBounds, ContentMask: full, Color: Hsla{A: 1}, Tile: monoTile}); err != nil {
		t.Fatalf("InsertMonochromeSprite: %v", err)
	}
	if err := scene.InsertSubpixelSprite(SubpixelSprite{Bounds: spriteBounds, ContentMask: full, Color: Hsla{A: 1}, Tile: subTile}); err != nil {
		t.Fatalf("InsertSubpixelSprite: %v", err)
	}
	if err := scene.InsertPolychromeSprite(PolychromeSprite{Bounds: spriteBounds, ContentMask: full, Tile: colorTile, Opacity: 1}); err != nil {
		t.Fatalf("InsertPolychromeSprite: %v", err)
	}
	builder := NewPathBuilder()
	builder.MoveTo(0, 0).LineTo(10, 0).LineTo(10, 10).Close()
	path, err := builder.Build()
	if err != nil {
		t.Fatalf("PathBuilder.Build: %v", err)
	}
	if err := scene.InsertPath(path, solid, full); err != nil {
		t.Fatalf("InsertPath: %v", err)
	}
	// Never-dropped markers and table rows: they count but paint
	// nowhere.
	if err := scene.BeginFilterGroup(Bounds{Origin: Point{}, Size: Size{Width: 80, Height: 80}}, 4, Corners{}, 0, NewPaintContext(1, full, 1)); err != nil {
		t.Fatalf("BeginFilterGroup: %v", err)
	}
	quad(120, 0, 10, 10, full)
	if err := scene.EndFilterGroup(Bounds{Origin: Point{}, Size: Size{Width: 80, Height: 80}}, 4, Corners{}, 0, NewPaintContext(1, full, 1)); err != nil {
		t.Fatalf("EndFilterGroup: %v", err)
	}
	if err := scene.BeginEffectGroup(EffectGroup{HasOpacity: true, Opacity: 0.5, Bounds: Bounds{Origin: Point{X: 200}, Size: Size{Width: 40, Height: 40}}, ContentMask: full}); err != nil {
		t.Fatalf("BeginEffectGroup: %v", err)
	}
	quad(200, 0, 10, 10, full)
	if err := scene.EndEffectGroup(EffectGroup{HasOpacity: true, Opacity: 0.5, Bounds: Bounds{Origin: Point{X: 200}, Size: Size{Width: 40, Height: 40}}, ContentMask: full}); err != nil {
		t.Fatalf("EndEffectGroup: %v", err)
	}
	if err := scene.BeginLayer(Bounds{Origin: Point{X: 300}, Size: Size{Width: 100, Height: 100}}, NewPaintContext(1, full, 1)); err != nil {
		t.Fatalf("BeginLayer: %v", err)
	}
	quad(300, 0, 10, 10, full)
	if err := scene.EndLayer(); err != nil {
		t.Fatalf("EndLayer: %v", err)
	}
	if _, err := scene.InsertGradient(Gradient{Kind: GradientLinear, Geometry: [8]float32{0, 0, 10, 0}, Stops: []TableGradientStop{{Position: 0, Color: Rgba{A: 1}}, {Position: 1, Color: Rgba{R: 1, A: 1}}}}); err != nil {
		t.Fatalf("InsertGradient: %v", err)
	}
	if _, err := scene.InsertCoverage(0, Bounds{Origin: Point{}, Size: Size{Width: 10, Height: 10}}); err != nil {
		t.Fatalf("InsertCoverage: %v", err)
	}
	if _, err := scene.InsertFilterOpChain([]FilterOp{{OffsetX: 2, OffsetY: 2, BlurRadius: 3, Color: Rgba{A: 1}}}); err != nil {
		t.Fatalf("InsertFilterOpChain: %v", err)
	}
	// A capture-backed surface (the always-damage rule's marker).
	paintCtx := NewPaintContext(1, full, 1)
	if err := scene.PaintCaptureSurface(Bounds{Origin: Point{X: 400}, Size: Size{Width: 30, Height: 30}}, ScreenCaptureFrame{Handle: 0x1234, Width: 30, Height: 30, ContentWidth: 30, ContentHeight: 30}, paintCtx); err != nil {
		t.Fatalf("PaintCaptureSurface: %v", err)
	}

	requireAligned := func(step string) {
		t.Helper()
		length, err := scene.Len()
		if err != nil {
			t.Fatalf("%s: Len: %v", step, err)
		}
		if got := uint32(len(scene.kernel.damageLog)); got != length {
			t.Fatalf("%s: the damage log has %d entries, the native op count is %d — the log drifted", step, got, length)
		}
	}
	requireAligned("before finish")
	if err := scene.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	requireAligned("after finish")
	// The capture marker landed on the right entry.
	foundCapture := false
	for _, op := range scene.kernel.damageLog {
		if op.capture {
			foundCapture = true
			if op.bounds.Origin.X != 400 {
				t.Fatalf("the capture surface's log bounds = %v, want x 400", op.bounds)
			}
		}
	}
	if !foundCapture {
		t.Fatal("no capture-surface entry in the damage log")
	}

	// Replay copies the log range with the ops: the target's count and
	// bounds stay aligned.
	target, err := NewScene()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = target.Dispose() })
	if err := target.Replay(2, 12, scene); err != nil {
		t.Fatalf("Replay: %v", err)
	}
	length, err := target.Len()
	if err != nil {
		t.Fatalf("target Len: %v", err)
	}
	if got := uint32(len(target.kernel.damageLog)); got != length {
		t.Fatalf("the replay target's damage log has %d entries, the native op count is %d", got, length)
	}
	for i := uint32(0); i < length; i++ {
		if target.kernel.damageLog[i].bounds != scene.kernel.damageLog[2+i].bounds {
			t.Fatalf("the replayed entry %d copied bounds %v, want the source's %v", i, target.kernel.damageLog[i].bounds, scene.kernel.damageLog[2+i].bounds)
		}
	}
}
