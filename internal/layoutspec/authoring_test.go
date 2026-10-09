package layoutspec

import (
	"testing"

	"gpui-go/authoring"
	"gpui-go/gpui"
)

// This file is ticket15's authoring-path layout capability slice: the
// exposed flex/grid/absolute/content-size style combinations exercised
// through the real element pipeline (request → compute → prepaint)
// with the deterministic test windows, observed through the
// debug-selector bounds (logical pixels, scale 2.0, rem 16). The exact
// expectations derive from the CSS Grid/flexbox semantics the pinned
// Taffy 0.13.0 lock implements.

// drawAuthoringCorpus builds one test window over the given root view
// and draws one frame, returning the window.
func drawAuthoringCorpus(t *testing.T, root gpui.AnyElement) *gpui.Window {
	t.Helper()
	ta := gpui.NewTestApp()
	app := ta.App()
	window := gpui.NewTestWindow(app, gpui.Size{Width: 400, Height: 300})
	view := gpui.NewEntity(app, window.Scope(), func(v *staticView, cx *gpui.Context[staticView]) {
		v.root = root
	})
	window.SetRootView(gpui.ViewOf(view))
	if _, err := gpui.DrawWindowFrame(window); err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}
	return window
}

// staticView renders a fixed root element.
type staticView struct {
	root gpui.AnyElement
}

// Render implements Render[staticView].
func (v *staticView) Render(w *gpui.Window, cx *gpui.Context[staticView]) gpui.AnyElement {
	return v.root
}

// gridChild is one fixed-size grid cell with a debug selector.
func gridChild(name string) *gpui.DivElement {
	return gpui.Div().Size(gpui.PxLength(40), gpui.PxLength(30)).DebugSelector(name)
}

// TestAuthoringGridPlacesCellsByTemplate checks the grid display with
// explicit track templates: a 2x2 grid over 200x100 spaces the four
// fixed children into 100x50 cells, auto-placed in row order.
func TestAuthoringGridPlacesCellsByTemplate(t *testing.T) {
	window := drawAuthoringCorpus(t, gpui.Div().
		Grid().
		W(gpui.PxLength(200)).
		H(gpui.PxLength(100)).
		GridTemplateCols(gpui.GridTemplate{Repeat: 2, MinSize: gpui.GridTemplateMinZero}).
		GridTemplateRows(gpui.GridTemplate{Repeat: 2, MinSize: gpui.GridTemplateMinZero}).
		Child(gridChild("g00")).
		Child(gridChild("g01")).
		Child(gridChild("g10")).
		Child(gridChild("g11")).
		IntoElement())

	want := map[string]gpui.Bounds{
		"g00": {Origin: gpui.Point{X: 0, Y: 0}, Size: gpui.Size{Width: 40, Height: 30}},
		"g01": {Origin: gpui.Point{X: 100, Y: 0}, Size: gpui.Size{Width: 40, Height: 30}},
		"g10": {Origin: gpui.Point{X: 0, Y: 50}, Size: gpui.Size{Width: 40, Height: 30}},
		"g11": {Origin: gpui.Point{X: 100, Y: 50}, Size: gpui.Size{Width: 40, Height: 30}},
	}
	for name, bounds := range want {
		got, ok := gpui.WindowDebugBound(window, name)
		if !ok {
			t.Fatalf("grid cell %q was not placed", name)
		}
		if got.Origin != bounds.Origin || got.Size != bounds.Size {
			t.Fatalf("grid cell %q = %+v, want %+v", name, got, bounds)
		}
	}
}

// TestAuthoringGridAtPlacesByLine checks the explicit grid placement:
// a child placed at row line 2, column line 2 lands in the second
// cell regardless of auto-flow order.
func TestAuthoringGridAtPlacesByLine(t *testing.T) {
	window := drawAuthoringCorpus(t, gpui.Div().
		Grid().
		W(gpui.PxLength(200)).
		H(gpui.PxLength(100)).
		GridTemplateCols(gpui.GridTemplate{Repeat: 2, MinSize: gpui.GridTemplateMinZero}).
		GridTemplateRows(gpui.GridTemplate{Repeat: 2, MinSize: gpui.GridTemplateMinZero}).
		Child(gridChild("auto-1")).
		Child(gpui.Div().
			Size(gpui.PxLength(40), gpui.PxLength(30)).
			DebugSelector("placed").
			GridAt(gpui.GridLocation{
				Row:    gpui.GridPlacementRange{Start: gpui.LineGridPlacement(2), End: gpui.AutoGridPlacement()},
				Column: gpui.GridPlacementRange{Start: gpui.LineGridPlacement(2), End: gpui.AutoGridPlacement()},
			})).
		IntoElement())

	placed, ok := gpui.WindowDebugBound(window, "placed")
	if !ok {
		t.Fatal("the placed grid child was not placed")
	}
	if placed.Origin != (gpui.Point{X: 100, Y: 50}) {
		t.Fatalf("placed child origin = %+v, want (100,50) (row line 2, column line 2)", placed.Origin)
	}
}

// TestAuthoringAbsoluteOffsetsFromAncestor checks absolute positioning
// through the authoring path: an absolutely-positioned child offsets
// from its positioned ancestor's origin and leaves the flow (the flow
// sibling below it starts at the parent's top).
func TestAuthoringAbsoluteOffsetsFromAncestor(t *testing.T) {
	window := drawAuthoringCorpus(t, gpui.Div().
		W(gpui.PxLength(200)).
		H(gpui.PxLength(100)).
		Child(gpui.Div().
			Absolute().
			Left(gpui.PxLength(10)).
			Top(gpui.PxLength(20)).
			Size(gpui.PxLength(30), gpui.PxLength(30)).
			DebugSelector("abs")).
		Child(gpui.Div().Size(gpui.PxLength(40), gpui.PxLength(30)).DebugSelector("flow")).
		IntoElement())

	abs, ok := gpui.WindowDebugBound(window, "abs")
	if !ok {
		t.Fatal("the absolute child was not placed")
	}
	if abs.Origin != (gpui.Point{X: 10, Y: 20}) || abs.Size != (gpui.Size{Width: 30, Height: 30}) {
		t.Fatalf("absolute child = %+v, want origin (10,20) size 30x30", abs)
	}
	flow, ok := gpui.WindowDebugBound(window, "flow")
	if !ok {
		t.Fatal("the flow sibling was not placed")
	}
	if flow.Origin.Y != 0 {
		t.Fatalf("flow sibling y = %v, want 0 (the absolute child leaves the flow)", flow.Origin.Y)
	}
}

// TestAuthoringContentSizedElement checks content-based sizing: a div
// with auto width/height wraps its fixed child exactly.
func TestAuthoringContentSizedElement(t *testing.T) {
	window := drawAuthoringCorpus(t, gpui.Div().
		W(gpui.PxLength(100)).
		H(gpui.PxLength(80)).
		Child(gpui.Div().
			Size(gpui.PxLength(50), gpui.PxLength(40)).
			DebugSelector("content")).
		IntoElement())

	content, ok := gpui.WindowDebugBound(window, "content")
	if !ok {
		t.Fatal("the content-sized child was not placed")
	}
	if content.Origin != (gpui.Point{}) || content.Size != (gpui.Size{Width: 50, Height: 40}) {
		t.Fatalf("content-sized child = %+v, want origin (0,0) size 50x40", content)
	}
}

// TestAuthoringRefinementOrder checks the refinement order in the real
// authoring path: a refinement applied after the builder's own style
// wins (Block refined to Flex lays the children in a row).
func TestAuthoringRefinementOrder(t *testing.T) {
	window := drawAuthoringCorpus(t, gpui.Div().
		Block().
		Refine(gpui.StyleRefinement{Display: authoring.Some(authoring.DisplayFlex)}).
		Child(gpui.Div().Size(gpui.PxLength(50), gpui.PxLength(40)).DebugSelector("r1")).
		Child(gpui.Div().Size(gpui.PxLength(50), gpui.PxLength(40)).DebugSelector("r2")).
		IntoElement())

	first, ok := gpui.WindowDebugBound(window, "r1")
	if !ok {
		t.Fatal("child 1 was not placed")
	}
	second, ok := gpui.WindowDebugBound(window, "r2")
	if !ok {
		t.Fatal("child 2 was not placed")
	}
	if second.Origin.X < first.Origin.X+first.Size.Width {
		t.Fatalf("refined-to-flex children must sit in a row: r1 %+v r2 %+v", first, second)
	}
}
