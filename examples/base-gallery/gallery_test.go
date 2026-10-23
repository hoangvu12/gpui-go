package main

import (
	"fmt"
	"testing"

	"github.com/hoangvu12/gpui-go/base"
	"github.com/hoangvu12/gpui-go/gpui"
)

// openGallery builds the gallery in a TestApp window (no native window)
// under a base.Root, as main does.
func openGallery(t *testing.T) (*gpui.TestApp, gpui.Entity[gallery], *gpui.VisualTestContext) {
	t.Helper()
	ta := gpui.NewTestApp()
	t.Cleanup(func() { ta.App().Shutdown() })
	ta.Update(func(app *gpui.App) { base.Init(app) })

	var content gpui.Entity[gallery]
	_, cx := ta.AddWindowView(func(r *base.Root, w *gpui.Window, rcx *gpui.Context[base.Root]) {
		content = gpui.NewEntity(rcx, w.Scope(), func(g *gallery, gcx *gpui.Context[gallery]) {
			*g = newGallery(w, gcx)
		})
		base.NewRoot(r, gpui.ViewOf(content), w, rcx)
	})
	return ta, content, cx
}

// drawFrame draws one frame and fails the test on a panic or an error.
func drawFrame(t *testing.T, cx *gpui.VisualTestContext, stage string) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("%s: panic: %v", stage, r)
		}
	}()
	cx.RunUntilParked()
	if _, err := gpui.DrawWindowFrame(cx.Window()); err != nil {
		t.Fatalf("%s: %v", stage, err)
	}
	cx.RunUntilParked()
}

// showPage switches the gallery to a page, applying extra state first.
func showPage(ta *gpui.TestApp, content gpui.Entity[gallery], id string, f func(g *gallery)) {
	content.Update(ta, func(g *gallery, gcx *gpui.Context[gallery]) {
		g.show(id)
		if f != nil {
			f(g)
		}
		gcx.Notify()
	})
}

// TestEveryPageDraws draws every page in both themes, then every page
// again with its overlays open and its alternate states set.
func TestEveryPageDraws(t *testing.T) {
	ta, content, cx := openGallery(t)
	drawFrame(t, cx, "initial")

	pages := allPages()
	if len(pages) == 0 {
		t.Fatal("the gallery has no pages")
	}
	seen := map[string]bool{}
	for _, p := range pages {
		if seen[p.id] {
			t.Fatalf("duplicate page id %q", p.id)
		}
		seen[p.id] = true
	}

	for _, dark := range []bool{false, true} {
		ta.Update(func(app *gpui.App) { setTheme(app, dark) })
		for _, p := range pages {
			showPage(ta, content, p.id, func(g *gallery) { g.dark = dark })
			drawFrame(t, cx, fmt.Sprintf("%s (dark=%v)", p.id, dark))
			// A second frame settles anything the first one deferred.
			drawFrame(t, cx, fmt.Sprintf("%s (dark=%v, settled)", p.id, dark))
			expectPage(t, cx, p.id)
		}
	}

	ta.Update(func(app *gpui.App) { setTheme(app, false) })
	for _, p := range pages {
		showPage(ta, content, p.id, openEverything)
		drawFrame(t, cx, p.id+" (overlays open)")
		drawFrame(t, cx, p.id+" (overlays open, settled)")
		expectPage(t, cx, p.id)
	}
}

// expectPage fails unless the page's body was laid out with a size.
func expectPage(t *testing.T, cx *gpui.VisualTestContext, id string) {
	t.Helper()
	bounds, ok := cx.DebugBounds("page-" + id)
	if !ok {
		t.Fatalf("page %s: its body was not drawn", id)
	}
	if bounds.Size.Width <= 0 || bounds.Size.Height <= 0 {
		t.Fatalf("page %s: body bounds %+v are empty", id, bounds)
	}
}

// TestButtonClickUpdatesPage clicks a real Base button through the
// window and expects the gallery state to change.
func TestButtonClickUpdatesPage(t *testing.T) {
	ta, content, cx := openGallery(t)
	drawFrame(t, cx, "initial")
	bounds, ok := cx.DebugBounds("primary-button")
	if !ok {
		t.Fatal("the primary button was not drawn")
	}
	cx.SimulateClick(gpui.Point{X: bounds.Origin.X + bounds.Size.Width/2, Y: bounds.Origin.Y + bounds.Size.Height/2}, gpui.Modifiers{})
	drawFrame(t, cx, "after click")
	if n := content.Read(ta, func(g *gallery, _ *gpui.App) int { return g.clicks }); n != 1 {
		t.Fatalf("clicks = %d, want 1", n)
	}
}

// TestCursors hovers elements whose Base parts request a cursor and
// reads the cursor the window resolves (test windows are never hovered,
// so the platform is not asked; the resolution is what a real window
// would hand it).
func TestCursors(t *testing.T) {
	ta, content, cx := openGallery(t)
	hover := func(page, selector string, at func(b gpui.Bounds) gpui.Point) gpui.CursorStyle {
		t.Helper()
		showPage(ta, content, page, nil)
		drawFrame(t, cx, page)
		bounds, ok := cx.DebugBounds(selector)
		if !ok {
			t.Fatalf("%s: %s was not drawn", page, selector)
		}
		cx.SimulateMouseMove(at(bounds), nil, gpui.Modifiers{})
		drawFrame(t, cx, page+" hovered")
		style, ok := gpui.WindowCursorStyle(cx.Window())
		if !ok {
			t.Fatalf("%s: no cursor resolved", page)
		}
		return style
	}
	center := func(b gpui.Bounds) gpui.Point {
		return gpui.Point{X: b.Origin.X + b.Size.Width/2, Y: b.Origin.Y + b.Size.Height/2}
	}
	if got := hover("input", "input-default", center); got != gpui.CursorIBeam {
		t.Errorf("input: cursor %v, want the I-beam", got)
	}
	if got := hover("resizable", "resizable-nav", func(b gpui.Bounds) gpui.Point {
		return gpui.Point{X: b.Origin.X + b.Size.Width, Y: b.Origin.Y + b.Size.Height/2}
	}); got != gpui.CursorResizeColumn && got != gpui.CursorResizeLeftRight {
		t.Errorf("resizable handle: cursor %v, want column resize", got)
	}
	// Base sets no cursor on Button; the gallery skin adds the pointer.
	if got := hover("button", "primary-button", center); got != gpui.CursorPointingHand {
		t.Errorf("button: cursor %v, want the pointing hand", got)
	}
	if got := hover("dock", "gallery-dock-resize", center); got != gpui.CursorResizeUpDown && got != gpui.CursorResizeRow {
		t.Errorf("dock resize strip: cursor %v, want row resize", got)
	}
}

// TestSidebarSelectionOpensPage selects a leaf in the navigation tree and
// expects the gallery to follow it.
func TestSidebarSelectionOpensPage(t *testing.T) {
	ta, content, cx := openGallery(t)
	drawFrame(t, cx, "initial")

	nav := content.Read(ta, func(g *gallery, _ *gpui.App) gpui.Entity[base.TreeState] { return g.nav })
	nav.Update(ta, func(st *base.TreeState, tcx *gpui.Context[base.TreeState]) {
		st.SetSelectedIndex(st.IndexOf("slider"), true, tcx)
	})
	drawFrame(t, cx, "after selecting slider")
	if got := content.Read(ta, func(g *gallery, _ *gpui.App) string { return g.current }); got != "slider" {
		t.Fatalf("current page = %q, want slider", got)
	}
}

// openEverything sets every overlay open and every alternate state.
func openEverything(g *gallery) {
	g.italic, g.underline, g.terms = true, true, true
	g.checks = [3]bool{true, false, false}
	g.radio, g.plan, g.tab, g.pillTab = 1, 2, 2, 1
	g.accordion = [3]bool{true, true, true}
	g.collapsibleClosed = true
	g.progressTick = 3
	g.selectOpen, g.comboOpen, g.dateOpen = true, true, true
	g.popupOpen, g.popupAboveOpen, g.popoverOpen, g.tooltipPinned = true, true, true, true
	g.dialogOpen, g.alertOpen, g.sheetOpen, g.sheetSide = true, true, true, 2
	g.toasts = []toastItem{{id: 1, title: "Saved"}, {id: 2, title: "Failed", failure: true}}
	g.springRight, g.bouncyRight, g.transitionBig, g.present = true, true, true, false
	g.keyframeRun = 1
	g.streamNext = 3
}
