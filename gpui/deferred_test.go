package gpui

// Base ticket 08's deferred-draw tests (deferred.go): priority ordering
// over the scene's order floor, auto priorities, overlay clipping, and
// the port of R:elements/deferred.rs:214-264
// test_nested_deferred_draws_with_reused_views.

import (
	"fmt"
	"strings"
	"testing"
)

// overlayBox is an absolutely positioned colored box.
func overlayBox(color Hsla, x, y, w, h float32) *DivElement {
	return Div().Absolute().Left(Px(x)).Top(Px(y)).W(Px(w)).H(Px(h)).Bg(color)
}

// TestDeferredPriorityOrdering pins R:window.rs:3661-3710: the deferred
// draws paint after the whole main tree above a raised order floor —
// even a later main-tree sibling sorts below them — in ascending priority
// order regardless of tree order (stable for equal priorities).
func TestDeferredPriorityOrdering(t *testing.T) {
	f := newPaintFixture(t, func(_ *paintView, w *Window, _ *Context[paintView]) AnyElement {
		return Div().SizeFull().
			Child(Deferred(overlayBox(paintRed, 0, 0, 100, 100)).Priority(2)).
			Child(Deferred(overlayBox(paintBlue, 10, 10, 100, 100)).Priority(0)).
			Child(Deferred(overlayBox(paintGreen, 20, 20, 100, 100)).Priority(1)).
			Child(overlayBox(paintGray, 30, 30, 100, 100)).
			IntoElement()
	})
	scene := f.draw()
	main := oneQuadOf(t, scene, paintGray, "main sibling")
	p0 := oneQuadOf(t, scene, paintBlue, "priority 0")
	p1 := oneQuadOf(t, scene, paintGreen, "priority 1")
	p2 := oneQuadOf(t, scene, paintRed, "priority 2")
	if !(main.Order < p0.Order && p0.Order < p1.Order && p1.Order < p2.Order) {
		t.Fatalf("orders main %d p0 %d p1 %d p2 %d, want ascending (scene %s)", main.Order, p0.Order, p1.Order, p2.Order, quadTranscript(t, scene))
	}
	// The deferred children keep their layout in the tree: each paints at
	// its own absolute position.
	if p1.Bounds != dev(40, 40, 200, 200) {
		t.Fatalf("priority-1 overlay at %v", p1.Bounds)
	}
	if got := drawState(f.window).lastDeferredPriorities; fmt.Sprint(got) != "[2 0 1]" {
		t.Fatalf("deferred draws (push order) = %v, want [2 0 1]", got)
	}
}

// TestDeferredPriorityAuto pins DeferredPriority::Auto
// (R:elements/deferred.rs:94-135): 1 for a deferred element caused by
// non-deferred elements, the causing deferred draw's priority plus one
// for a nested one (the DeferredPriorityStackCache during the next
// prepaint round), and nested draws paint above their causes.
func TestDeferredPriorityAuto(t *testing.T) {
	f := newPaintFixture(t, func(_ *paintView, w *Window, _ *Context[paintView]) AnyElement {
		return Div().SizeFull().
			Child(Deferred(
				Div().Child(overlayBox(paintRed, 0, 0, 50, 50)).
					Child(Deferred(overlayBox(paintBlue, 10, 10, 50, 50)).PriorityAuto()),
			).Priority(5)).
			Child(Deferred(overlayBox(paintGreen, 20, 20, 50, 50)).PriorityAuto()).
			IntoElement()
	})
	scene := f.draw()
	if got := drawState(f.window).lastDeferredPriorities; fmt.Sprint(got) != "[5 1 6]" {
		t.Fatalf("deferred priorities = %v, want [5 1 6] (explicit 5, top-level auto 1, nested auto 5+1)", got)
	}
	green := oneQuadOf(t, scene, paintGreen, "auto 1")
	red := oneQuadOf(t, scene, paintRed, "priority 5")
	blue := oneQuadOf(t, scene, paintBlue, "nested auto 6")
	if !(green.Order < red.Order && red.Order < blue.Order) {
		t.Fatalf("orders auto1 %d p5 %d nested %d, want ascending", green.Order, red.Order, blue.Order)
	}
}

// TestDeferredEscapesAncestorClipAndHonorsContentMask pins the overlay
// clipping of R:window.rs:3661-3703: a deferred draw paints under the
// window's base content mask (its ancestors' overflow masks are not
// active when paint_deferred_draws runs) unless it carries its own
// content mask (R:elements/deferred.rs:102-107).
func TestDeferredEscapesAncestorClipAndHonorsContentMask(t *testing.T) {
	f := newPaintFixture(t, func(_ *paintView, w *Window, _ *Context[paintView]) AnyElement {
		return Div().SizeFull().
			Child(Div().W(Px(50)).H(Px(50)).OverflowHidden().
				Child(overlayBox(paintGray, 0, 0, 100, 100)).
				Child(Deferred(overlayBox(paintRed, 0, 0, 100, 100)))).
			Child(Deferred(overlayBox(paintBlue, 0, 0, 100, 100)).ContentMask(logical(0, 0, 30, 40))).
			IntoElement()
	})
	scene := f.draw()
	if clipped := oneQuadOf(t, scene, paintGray, "clipped main child"); clipped.ContentMask != dev(0, 0, 100, 100) {
		t.Fatalf("main child mask %v, want the parent's overflow (0,0 100x100)", clipped.ContentMask)
	}
	if overlay := oneQuadOf(t, scene, paintRed, "deferred overlay"); overlay.ContentMask != dev(0, 0, 1600, 1200) {
		t.Fatalf("deferred overlay mask %v, want the window (0,0 1600x1200)", overlay.ContentMask)
	}
	if masked := oneQuadOf(t, scene, paintBlue, "masked overlay"); masked.ContentMask != dev(0, 0, 60, 80) {
		t.Fatalf("masked overlay mask %v, want (0,0 60x80)", masked.ContentMask)
	}
}

// deferredPanelView is the pinned PanelView (R:deferred.rs:164-191): a
// popover (deferred draw) whose content opens another popover.
type deferredPanelView struct{}

func (v *deferredPanelView) Render(w *Window, cx *Context[deferredPanelView]) AnyElement {
	return Div().KeyContext("Panel").SizeFull().Child(
		Deferred(
			Anchored().Position(Point{X: 10, Y: 10}).Child(
				Div().KeyContext("Popover").W(Px(200)).H(Px(200)).Child(
					Deferred(
						Anchored().Position(Point{X: 30, Y: 30}).Child(
							Div().KeyContext("NestedMenu").DebugSelector("NESTED_MENU").W(Px(50)).H(Px(50)),
						),
					).Priority(2),
				),
			),
		).Priority(1),
	).IntoElement()
}

// deferredRootView is the pinned RootView (R:deferred.rs:193-205).
type deferredRootView struct {
	panel Entity[deferredPanelView]
}

func (v *deferredRootView) Render(w *Window, cx *Context[deferredRootView]) AnyElement {
	return Div().KeyContext("Root").SizeFull().Child(ViewOf(v.panel)).IntoElement()
}

// TestDeferredNestedDrawsWithReusedViews ports R:deferred.rs:214-264
// test_nested_deferred_draws_with_reused_views. Deviation: the port has
// no view caching (`.cached(..)`), so the panel re-renders with every
// root notification instead of reusing its subtree; the test still pins
// the nested draws' bounds and count across repeated frames, and that a
// nested popover's dispatch node is parented under the popover that
// deferred it (the pinned set_active_node(parent_node)).
func TestDeferredNestedDrawsWithReusedViews(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	window := NewTestWindow(app, Size{Width: 800, Height: 600})
	t.Cleanup(func() { CloseTestWindow(window) })
	panel := NewEntity(app, window.Scope(), func(*deferredPanelView, *Context[deferredPanelView]) {})
	root := NewEntity(app, window.Scope(), func(v *deferredRootView, _ *Context[deferredRootView]) { v.panel = panel })
	window.SetRootView(ViewOf(root))
	ta.RunUntilParked()
	if WindowDrawCount(window) == 0 {
		if _, err := DrawWindowFrame(window); err != nil {
			t.Fatal(err)
		}
	}

	check := func(step string) {
		t.Helper()
		bounds, ok := WindowDebugBound(window, "NESTED_MENU")
		if !ok {
			t.Fatalf("%s: NESTED_MENU debug bounds not found", step)
		}
		if bounds.Size != (Size{Width: 50, Height: 50}) || bounds.Origin != (Point{X: 30, Y: 30}) {
			t.Fatalf("%s: NESTED_MENU bounds %v, want (30,30 50x50)", step, bounds)
		}
		if got := len(drawState(window).lastDeferredPriorities); got != 2 {
			t.Fatalf("%s: deferred draws = %d, want 2", step, got)
		}
	}
	check("first frame")

	for i := 0; i < 2; i++ {
		root.Update(app, func(_ *deferredRootView, cx *Context[deferredRootView]) { cx.Notify() })
		ta.RunUntilParked()
		check(fmt.Sprintf("root re-render %d", i+1))
	}
	panel.Update(app, func(_ *deferredPanelView, cx *Context[deferredPanelView]) { cx.Notify() })
	ta.RunUntilParked()
	check("panel re-render")

	// The nested popover's key context path runs through the popover that
	// deferred it and the panel (dispatch tree parenting).
	tree := focusState(window).rendered.dispatchTree
	var nested DispatchNodeID
	found := false
	for i := range tree.nodes {
		if ctx := tree.nodes[i].context; ctx != nil && ctx.String() == "NestedMenu" {
			nested, found = DispatchNodeID{Index: i}, true
		}
	}
	if !found {
		t.Fatalf("no NestedMenu dispatch node in %s", tree)
	}
	var path []string
	for _, id := range tree.DispatchPath(nested) {
		if ctx := tree.nodes[id.Index].context; ctx != nil {
			path = append(path, ctx.String())
		}
	}
	if fmt.Sprint(path) != "[Root Panel Popover NestedMenu]" {
		t.Fatalf("NestedMenu context path = %v, want [Root Panel Popover NestedMenu]", path)
	}
}

// TestDeferredDepthLimit pins the pinned nesting guard
// (R:window.rs:3600-3601): deferred draws spawning deferred draws more
// than ten rounds deep panic. The window is abandoned after the panic.
func TestDeferredDepthLimit(t *testing.T) {
	var nest func(depth int) any
	nest = func(depth int) any {
		if depth == 0 {
			return Div().W(Px(1)).H(Px(1))
		}
		return Deferred(nest(depth - 1))
	}
	app := NewTestApp().App()
	window := NewTestWindow(app, Size{Width: 100, Height: 100})
	view := NewEntity(app, window.Scope(), func(v *paintView, _ *Context[paintView]) {
		v.build = func(*paintView, *Window, *Context[paintView]) AnyElement {
			return Div().Child(nest(11)).IntoElement()
		}
	})
	window.SetRootView(ViewOf(view))
	defer func() {
		r := recover()
		if r == nil || !strings.Contains(fmt.Sprint(r), "Exceeded maximum (10) deferred depth") {
			t.Fatalf("recover() = %v, want the deferred depth panic", r)
		}
	}()
	_, _ = DrawWindowFrame(window)
}
