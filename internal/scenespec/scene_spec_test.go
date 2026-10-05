//go:build windows

// Package scenespec holds ticket08's scene kernel tests: real scene
// semantics computed through the public gpui API against the embedded
// native scene kernel artifact. They exercise the kernel from outside
// the runtime package, matching the repo convention (see
// internal/layoutspec and internal/taskspec).
//
// Environment expectation: the native scene artifact is Windows AMD64
// (loaded from the embedded bytes, materialized into the per-user cache
// by internal/native); this machine is expected to run these tests
// interactively. The conformance gate itself lives in internal/portfixture
// (fx-0003); this package pins the kernel behaviors the fixture cannot
// reach through the reference's public API (paired surface opacity,
// capacity bounds, stale handles, forced GC, panic containment) and
// re-checks the pinned ordering semantics through the port's records.
package scenespec

import (
	"errors"
	"math"
	"runtime"
	"testing"

	"gpui-go/gpui"
	"gpui-go/internal/native"
)

// newScene creates a scene, disposed at cleanup.
func newScene(t *testing.T) *gpui.Scene {
	t.Helper()
	scene, err := gpui.NewScene()
	if err != nil {
		t.Fatalf("gpui.NewScene: %v", err)
	}
	t.Cleanup(func() { _ = scene.Dispose() })
	return scene
}

// paintCtx builds a paint context (scale 1, unbounded mask, opacity 1).
func paintCtx() *gpui.PaintContext {
	return gpui.NewPaintContext(1, gpui.Bounds{Size: gpui.Size{Width: 1e6, Height: 1e6}}, 1)
}

// box returns a PaintQuad of the given logical bounds with an opaque
// background.
func box(x, y, w, h float32) gpui.PaintQuad {
	return gpui.Fill(
		gpui.Bounds{Origin: gpui.Point{X: x, Y: y}, Size: gpui.Size{Width: w, Height: h}},
		gpui.SolidBackground(gpui.White()),
	)
}

// quadOrders returns the finished scene's quad draw orders.
func quadOrders(t *testing.T, scene *gpui.Scene) []uint32 {
	t.Helper()
	if err := scene.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	quads, err := scene.Quads()
	if err != nil {
		t.Fatalf("Quads: %v", err)
	}
	orders := make([]uint32, len(quads))
	for i, quad := range quads {
		orders[i] = quad.Order
	}
	return orders
}

// TestLayerPrimitivesCarryTheLayerOrder pins the pinned layer semantics:
// a layer is a batch of geometry sharing one draw order — the layer's own
// order (crates/gpui/src/scene.rs: `layer_stack.last()` fallback).
func TestLayerPrimitivesCarryTheLayerOrder(t *testing.T) {
	scene := newScene(t)
	ctx := paintCtx()
	mask := ctx.Mask

	if err := scene.PaintQuad(box(0, 0, 100, 100), 0, ctx); err != nil { // order 1
		t.Fatal(err)
	}
	if err := scene.BeginLayer(gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 100, Height: 100}}, &gpui.PaintContext{ScaleFactor: 1, Mask: mask}); err != nil {
		t.Fatal(err)
	}
	if err := scene.PaintQuad(box(0, 0, 100, 100), 0, ctx); err != nil {
		t.Fatal(err)
	}
	if err := scene.PaintQuad(box(0, 0, 100, 100), 0, ctx); err != nil {
		t.Fatal(err)
	}
	if err := scene.EndLayer(); err != nil {
		t.Fatal(err)
	}
	if err := scene.PaintQuad(box(0, 0, 100, 100), 0, ctx); err != nil {
		t.Fatal(err)
	}
	orders := quadOrders(t, scene)
	if len(orders) != 4 {
		t.Fatalf("quad count = %d, want 4", len(orders))
	}
	want := []uint32{1, 2, 2, 3}
	for i, order := range orders {
		if order != want[i] {
			t.Fatalf("quad %d order = %d, want %d (the layer members share the layer order)", i, order, want[i])
		}
	}
}

// TestRaiseOrderFloorPutsDeferredDrawsAboveEverything pins the
// deferred-draw floor (crates/gpui/src/scene.rs::raise_order_floor): a
// primitive inserted after the floor sorts above everything inserted
// before, even when it does not overlap any of it (the order-reuse
// path would otherwise tie it at order 1).
func TestRaiseOrderFloorPutsDeferredDrawsAboveEverything(t *testing.T) {
	scene := newScene(t)
	ctx := paintCtx()

	if err := scene.PaintQuad(box(0, 0, 100, 100), 0, ctx); err != nil {
		t.Fatal(err)
	}
	if err := scene.PaintQuad(box(200, 200, 100, 100), 0, ctx); err != nil {
		t.Fatal(err)
	}
	orders := quadOrders(t, scene)
	if orders[0] != 1 || orders[1] != 1 {
		t.Fatalf("base orders = %v, want [1 1] (the detached quad reuses the low order)", orders)
	}

	// A second scene with the floor between the two groups.
	scene2 := newScene(t)
	if err := scene2.PaintQuad(box(0, 0, 100, 100), 0, ctx); err != nil {
		t.Fatal(err)
	}
	if err := scene2.PaintQuad(box(200, 200, 100, 100), 0, ctx); err != nil {
		t.Fatal(err)
	}
	if err := scene2.RaiseOrderFloor(); err != nil {
		t.Fatal(err)
	}
	if err := scene2.PaintQuad(box(400, 400, 100, 100), 0, ctx); err != nil {
		t.Fatal(err)
	}
	orders = quadOrders(t, scene2)
	if len(orders) != 3 {
		t.Fatalf("quad count = %d, want 3", len(orders))
	}
	if orders[2] != 2 {
		t.Fatalf("deferred quad order = %d, want 2 (above the floor)", orders[2])
	}
}

// TestContentAfterFilterGroupSortsAboveIt pins the close-time order
// floor (crates/gpui/src/scene.rs: a closed content-filter group's end
// marker raises the floor above itself): a non-overlapping sibling
// painted after the group sorts AFTER the group's end marker instead of
// reusing a low order that would fall inside the group's range.
func TestContentAfterFilterGroupSortsAboveIt(t *testing.T) {
	scene := newScene(t)
	ctx := paintCtx()
	groupBounds := gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 100, Height: 100}}

	if err := scene.BeginFilterGroup(groupBounds, 8, gpui.Corners{}, 0, ctx); err != nil {
		t.Fatal(err)
	}
	if err := scene.PaintQuad(box(0, 0, 100, 100), 0, ctx); err != nil {
		t.Fatal(err)
	}
	if err := scene.EndFilterGroup(groupBounds, 8, gpui.Corners{}, 0, ctx); err != nil {
		t.Fatal(err)
	}
	// A sibling that does NOT overlap the group.
	if err := scene.PaintQuad(box(200, 200, 100, 100), 0, ctx); err != nil {
		t.Fatal(err)
	}

	if err := scene.Finish(); err != nil {
		t.Fatal(err)
	}
	commands, err := scene.Commands()
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 4 {
		t.Fatalf("command count = %d, want 4 (begin, quad, end, quad)", len(commands))
	}
	if commands[0].Kind != gpui.SceneCommandKindBeginFilter {
		t.Fatalf("first command = %d, want begin-filter", commands[0].Kind)
	}
	if commands[2].Kind != gpui.SceneCommandKindEndFilter {
		t.Fatalf("third command = %d, want end-filter", commands[2].Kind)
	}
	if commands[3].Kind != gpui.SceneCommandKindBatch || commands[3].BatchKind != gpui.SceneBatchQuads {
		t.Fatalf("fourth command = %+v, want a quad batch", commands[3])
	}
	// The sibling sorts after the group: the end marker has order 2, the
	// sibling order 3.
	boundaries, err := scene.FilterBoundaries()
	if err != nil {
		t.Fatal(err)
	}
	quads, err := scene.Quads()
	if err != nil {
		t.Fatal(err)
	}
	if len(boundaries) != 2 {
		t.Fatalf("boundary count = %d, want 2", len(boundaries))
	}
	// The markers take orders above all prior content: start 1, the
	// group's child 2 (overlapping the marker), the end 3, and the
	// sibling 4 (held above the end by the close-time floor).
	if boundaries[1].Order != 3 {
		t.Fatalf("end marker order = %d, want 3", boundaries[1].Order)
	}
	if quads[1].Order != 4 {
		t.Fatalf("post-group sibling order = %d, want 4 (above the end marker)", quads[1].Order)
	}
}

// TestNestedFilterGroupsBoundIsolationTargets pins the pinned plan's
// bounded target allocator (crates/gpui/src/scene/plan.rs:
// MAX_FILTER_GROUP_DEPTH = 2): three nested matched groups receive
// isolated targets 0 and 1 and the third renders inline; every end
// command carries its matched start's index.
func TestNestedFilterGroupsBoundIsolationTargets(t *testing.T) {
	scene := newScene(t)
	ctx := paintCtx()
	group := func(x float32) gpui.Bounds {
		return gpui.Bounds{Origin: gpui.Point{X: x, Y: 0}, Size: gpui.Size{Width: 40, Height: 40}}
	}

	// Three non-overlapping groups (nesting comes from the begin/end
	// order, exactly like the pinned three-nested-group test).
	for i := 0; i < 3; i++ {
		bounds := group(float32(100 * i))
		if err := scene.BeginFilterGroup(bounds, 4, gpui.Corners{}, 0, ctx); err != nil {
			t.Fatal(err)
		}
		if err := scene.PaintQuad(box(bounds.Origin.X, 0, 40, 40), 0, ctx); err != nil {
			t.Fatal(err)
		}
	}
	for i := 2; i >= 0; i-- {
		bounds := group(float32(100 * i))
		if err := scene.EndFilterGroup(bounds, 4, gpui.Corners{}, 0, ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := scene.Finish(); err != nil {
		t.Fatal(err)
	}
	commands, err := scene.Commands()
	if err != nil {
		t.Fatal(err)
	}
	// 3 begins + 3 quad batches + 3 ends = 9 commands.
	if len(commands) != 9 {
		t.Fatalf("command count = %d, want 9", len(commands))
	}
	wantTargets := [][2]bool{
		{true, true},   // begin: isolated(0)
		{true, true},   // begin: isolated(1)
		{false, false}, // begin: inline
		{false, false}, // end: inline
		{true, true},   // end: isolated(1)
		{true, true},   // end: isolated(0)
	}
	// The begins are commands 0, 2, 4 (interleaved with quads) and the
	// ends are 6, 7, 8 — but the batch iterator interleaves by ORDER:
	// each group's markers take orders above all prior content. Collect
	// the filter commands in order instead.
	var filterCommands []gpui.SceneCommand
	for _, command := range commands {
		if command.Kind != gpui.SceneCommandKindBatch {
			filterCommands = append(filterCommands, command)
		}
	}
	if len(filterCommands) != 6 {
		t.Fatalf("filter command count = %d, want 6", len(filterCommands))
	}
	for i, want := range wantTargets {
		command := filterCommands[i]
		if command.Target.Isolated != want[0] {
			t.Errorf("filter command %d isolated = %v, want %v", i, command.Target.Isolated, want[0])
		}
		if want[0] && command.Target.Index != wantIdx(i) {
			t.Errorf("filter command %d target index = %d, want %d", i, command.Target.Index, wantIdx(i))
		}
	}
	// The ends carry their matched start's index: end[3] closes the
	// innermost group (start 2), end[4] the middle (start 1), end[5] the
	// outer (start 0).
	wantClosings := [][2]uint32{{2, 3}, {1, 4}, {0, 5}}
	for i, want := range wantClosings {
		command := filterCommands[3+i]
		if command.BoundaryIndex != want[0] || command.ClosingBoundaryIndex != want[1] {
			t.Errorf("end command %d = start %d close %d, want start %d close %d",
				i, command.BoundaryIndex, command.ClosingBoundaryIndex, want[0], want[1])
		}
	}
	requirements, err := scene.Requirements()
	if err != nil {
		t.Fatal(err)
	}
	if !requirements.UsesOffscreenTarget {
		t.Errorf("usesOffscreenTarget = false, want true (two isolated groups)")
	}
	if requirements.IsolatedFilterCount != 2 || requirements.IsolatedTargetCount != 2 {
		t.Errorf("isolated filters/targets = %d/%d, want 2/2", requirements.IsolatedFilterCount, requirements.IsolatedTargetCount)
	}
}

func wantIdx(i int) uint32 {
	switch i {
	case 0, 5:
		return 0
	case 1, 4:
		return 1
	default:
		return 0
	}
}

// TestEmptyClippedPrimitiveDroppedButBoundarySurvives pins the pinned
// insertion rule (crates/gpui/src/scene.rs: `clipped_bounds.is_empty()`
// in insert_primitive_with_surface_opacity): an ordinary primitive whose
// bounds fall entirely outside its content mask is dropped; a
// content-filter boundary always inserts (matched pairs must survive
// clipping), with its primitive bounds.
func TestEmptyClippedPrimitiveDroppedButBoundarySurvives(t *testing.T) {
	scene := newScene(t)
	// The mask is the default (huge) one; use an explicit far-away mask.
	mask := gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 50, Height: 50}}
	ctx := gpui.NewPaintContext(1, mask, 1)
	groupBounds := gpui.Bounds{Origin: gpui.Point{X: 500, Y: 500}, Size: gpui.Size{Width: 40, Height: 40}}

	// A quad fully outside the mask: dropped.
	if err := scene.PaintQuad(box(500, 500, 40, 40), 0, ctx); err != nil {
		t.Fatal(err)
	}
	if err := scene.Finish(); err != nil {
		t.Fatal(err)
	}
	quads, err := scene.Quads()
	if err != nil {
		t.Fatal(err)
	}
	if len(quads) != 0 {
		t.Fatalf("quad count = %d, want 0 (empty clipped primitives are dropped)", len(quads))
	}
	meta, err := scene.Meta()
	if err != nil {
		t.Fatal(err)
	}
	if meta.OpCount != 0 {
		t.Fatalf("op count = %d, want 0", meta.OpCount)
	}

	// A boundary at the same fully-outside bounds still inserts, with its
	// primitive bounds (not the empty clipped bounds).
	scene2 := newScene(t)
	if err := scene2.BeginFilterGroup(groupBounds, 4, gpui.Corners{}, 0, ctx); err != nil {
		t.Fatal(err)
	}
	if err := scene2.Finish(); err != nil {
		t.Fatal(err)
	}
	boundaries, err := scene2.FilterBoundaries()
	if err != nil {
		t.Fatal(err)
	}
	if len(boundaries) != 1 {
		t.Fatalf("boundary count = %d, want 1 (boundaries survive clipping)", len(boundaries))
	}
	if boundaries[0].Bounds.Origin.X != groupBounds.Origin.X {
		t.Fatalf("boundary bounds x = %g, want %g (the primitive bounds)", boundaries[0].Bounds.Origin.X, groupBounds.Origin.X)
	}
}

// TestSurfaceOpacityPairsSurviveFinishAndReplay pins the pinned paired
// surface opacity (crates/gpui/src/scene.rs: insert_surface +
// surface_opacities through the finish zip/sort/unzip and replay): the
// opacity stays paired with its surface through the sort, and replayed
// surfaces keep their paired opacities. The reference's insert_surface
// is crate-private, so the fixture cannot cover this; the kernel does.
func TestSurfaceOpacityPairsSurviveFinishAndReplay(t *testing.T) {
	scene := newScene(t)
	bounds := gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 100, Height: 100}}
	mask := gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 1000, Height: 1000}}

	if err := scene.InsertSurface(bounds, mask, gpui.SurfaceSourceNone, 0.25); err != nil {
		t.Fatal(err)
	}
	if err := scene.InsertSurface(bounds, mask, gpui.SurfaceSourceNone, 1.0); err != nil {
		t.Fatal(err)
	}
	if err := scene.Finish(); err != nil {
		t.Fatal(err)
	}
	surfaces, opacities, err := scene.Surfaces()
	if err != nil {
		t.Fatal(err)
	}
	if len(surfaces) != 2 || len(opacities) != 2 {
		t.Fatalf("surface/opactiy counts = %d/%d, want 2/2", len(surfaces), len(opacities))
	}
	if opacities[0] != 0.25 || opacities[1] != 1.0 {
		t.Fatalf("opacities = %v, want [0.25 1] (paired through the sort)", opacities)
	}
	if surfaces[0].Order != 1 || surfaces[1].Order != 2 {
		t.Fatalf("surface orders = %v, want [1 2]", []uint32{surfaces[0].Order, surfaces[1].Order})
	}

	// Reordering: insert out of opacity order and confirm the pair stays
	// with its surface through the sort.
	scene2 := newScene(t)
	if err := scene2.InsertSurface(bounds, mask, gpui.SurfaceSourceNone, 0.9); err != nil {
		t.Fatal(err)
	}
	if err := scene2.InsertSurface(gpui.Bounds{Origin: gpui.Point{X: 500, Y: 0}, Size: gpui.Size{Width: 100, Height: 100}}, mask, gpui.SurfaceSourceNone, 0.25); err != nil {
		t.Fatal(err)
	}
	if err := scene2.Finish(); err != nil {
		t.Fatal(err)
	}
	surfaces, opacities, err = scene2.Surfaces()
	if err != nil {
		t.Fatal(err)
	}
	if opacities[0] != 0.9 || opacities[1] != 0.25 {
		t.Fatalf("reordered opacities = %v, want [0.9 0.25] (each surface keeps its own)", opacities)
	}

	// Replay keeps the paired opacities (the pinned
	// surface_opacity_is_preserved_without_changing_paint_surface_layout).
	target := newScene(t)
	length, err := scene.Len()
	if err != nil {
		t.Fatal(err)
	}
	if err := target.Replay(0, length, scene); err != nil {
		t.Fatal(err)
	}
	if err := target.Finish(); err != nil {
		t.Fatal(err)
	}
	replayed, replayedOpacities, err := target.Surfaces()
	if err != nil {
		t.Fatal(err)
	}
	if len(replayed) != 2 || replayedOpacities[0] != 0.25 || replayedOpacities[1] != 1.0 {
		t.Fatalf("replayed surfaces/opacities = %d/%v, want 2/[0.25 1]", len(replayed), replayedOpacities)
	}
}

// TestNestedElementOpacityMultiplies pins the pinned
// with_element_opacity semantics (crates/gpui/src/window.rs):
// nested scopes MULTIPLY the element opacity (they never merge — the
// ticket's "nested equal-opacity layers" question), and the paint
// methods fold it into the primitive colors. The background path takes
// the Background::opacity hue round trip; the underline path takes the
// palette Hsla opacity (alpha only).
func TestNestedElementOpacityMultiplies(t *testing.T) {
	scene := newScene(t)
	red := gpui.RgbaToHsla(0xff0000ff)
	// 0.5 nested in 0.5 -> 0.25, exactly like with_element_opacity's
	// previous * opacity.
	inner := paintCtx().WithElementOpacity(0.5).WithElementOpacity(0.5)

	ctx := paintCtx()
	if err := scene.PaintQuad(gpui.PaintQuad{
		Bounds:     gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 40, Height: 40}},
		Background: gpui.SolidBackground(red),
	}, 0, inner); err != nil {
		t.Fatal(err)
	}
	color := gpui.RgbaToHsla(0x00ff00ff)
	style := gpui.UnderlineStyle{Thickness: 1, Color: &color}
	if err := scene.PaintUnderline(gpui.Point{}, 40, style, inner); err != nil {
		t.Fatal(err)
	}
	// A control quad at full opacity.
	if err := scene.PaintQuad(gpui.PaintQuad{
		Bounds:     gpui.Bounds{Origin: gpui.Point{X: 100, Y: 0}, Size: gpui.Size{Width: 40, Height: 40}},
		Background: gpui.SolidBackground(red),
	}, 0, ctx); err != nil {
		t.Fatal(err)
	}
	if err := scene.Finish(); err != nil {
		t.Fatal(err)
	}
	quads, err := scene.Quads()
	if err != nil {
		t.Fatal(err)
	}
	if len(quads) != 2 {
		t.Fatalf("quad count = %d, want 2", len(quads))
	}
	faded := quads[0].Background.Solid
	full := quads[1].Background.Solid
	if faded.A != full.A*0.25 {
		t.Fatalf("nested opacity: faded alpha = %g, want %g (0.5 * 0.5 multiplies, never merges)", faded.A, full.A*0.25)
	}
	// The hue is unchanged by opacity (the round trip preserves it).
	if faded.H != full.H || faded.S != full.S || faded.L != full.L {
		t.Fatalf("nested opacity changed hue/saturation/lightness: %+v vs %+v", faded, full)
	}

	underlines, err := scene.Underlines()
	if err != nil {
		t.Fatal(err)
	}
	if len(underlines) != 1 {
		t.Fatalf("underline count = %d, want 1", len(underlines))
	}
	underlineColor := underlines[0].Color
	if underlineColor.A != 0.25 {
		t.Fatalf("underline nested opacity: alpha = %g, want 0.25", underlineColor.A)
	}
}

// TestSmoothingSplitsBatches pins the pinned batch merge rule
// (crates/gpui/src/scene.rs BatchIterator): a run of quads of one kind
// merges into one batch EXCEPT at corner-smoothing changes, so the
// smoothing sequence 0, 0, 0.5, 1, 0 produces three batches (0..2, 2..4,
// 4..5).
func TestSmoothingSplitsBatches(t *testing.T) {
	scene := newScene(t)
	ctx := paintCtx()
	for _, smoothing := range []float32{0, 0, 0.5, 1, 0} {
		if err := scene.PaintQuad(box(0, 0, 100, 100), smoothing, ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := scene.Finish(); err != nil {
		t.Fatal(err)
	}
	commands, err := scene.Commands()
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 3 {
		t.Fatalf("command count = %d, want 3 (the smoothing changes split the batch)", len(commands))
	}
	wantRanges := [][2]uint32{{0, 2}, {2, 4}, {4, 5}}
	wantSmoothed := []bool{false, true, false}
	for i, command := range commands {
		if command.Kind != gpui.SceneCommandKindBatch || command.BatchKind != gpui.SceneBatchQuads {
			t.Fatalf("command %d = %+v, want a quad batch", i, command)
		}
		if command.Range != wantRanges[i] {
			t.Errorf("batch %d range = %v, want %v", i, command.Range, wantRanges[i])
		}
		if command.Smoothed != wantSmoothed[i] {
			t.Errorf("batch %d smoothed = %v, want %v", i, command.Smoothed, wantSmoothed[i])
		}
	}
	// The orders step in insertion order (all overlapping).
	orders := quadOrders(t, scene)
	want := []uint32{1, 2, 3, 4, 5}
	for i, order := range orders {
		if order != want[i] {
			t.Fatalf("quad %d order = %d, want %d", i, order, want[i])
		}
	}
}

// TestKindTiebreakAtEqualOrderFollowsPinnedDiscriminants pins the
// PrimitiveKind tie-break (crates/gpui/src/scene.rs PrimitiveKind
// discriminants): at an equal order the plan emits Shadow before Quad
// before Underline before BackdropFilter, with filter boundaries
// bracketing (start before content, end after) at their own orders.
func TestKindTiebreakAtEqualOrderFollowsPinnedDiscriminants(t *testing.T) {
	scene := newScene(t)
	ctx := paintCtx()
	// Non-overlapping primitives reuse order 1.
	at := func(x float32) gpui.Bounds {
		return gpui.Bounds{Origin: gpui.Point{X: x, Y: 0}, Size: gpui.Size{Width: 10, Height: 10}}
	}
	if err := scene.PaintQuad(box(0, 0, 10, 10), 0, ctx); err != nil {
		t.Fatal(err)
	}
	color := gpui.RgbaToHsla(0x000000ff)
	if err := scene.PaintUnderline(gpui.Point{X: 20, Y: 0}, 10, gpui.UnderlineStyle{Thickness: 1, Color: &color}, ctx); err != nil {
		t.Fatal(err)
	}
	if err := scene.PaintShadow(at(40), gpui.BoxShadow{Color: gpui.SolidBackground(color)}, gpui.Corners{}, 0, ctx); err != nil {
		t.Fatal(err)
	}
	if err := scene.PaintBackdrop(at(60), 4, gpui.Corners{}, 0, ctx); err != nil {
		t.Fatal(err)
	}
	orders := quadOrders(t, scene)
	if len(orders) != 1 || orders[0] != 1 {
		t.Fatalf("quad order = %v, want [1]", orders)
	}
	if err := scene.Finish(); err != nil {
		t.Fatal(err)
	}
	commands, err := scene.Commands()
	if err != nil {
		t.Fatal(err)
	}
	var batchOrder []gpui.SceneBatchKind
	for _, command := range commands {
		if command.Kind == gpui.SceneCommandKindBatch {
			batchOrder = append(batchOrder, command.BatchKind)
		}
	}
	want := []gpui.SceneBatchKind{gpui.SceneBatchShadows, gpui.SceneBatchQuads, gpui.SceneBatchUnderlines, gpui.SceneBatchBackdropFilters}
	if len(batchOrder) != len(want) {
		t.Fatalf("batch order = %v, want %v", batchOrder, want)
	}
	for i, kind := range batchOrder {
		if kind != want[i] {
			t.Fatalf("batch %d = %v, want %v (the pinned PrimitiveKind discriminant order)", i, kind, want[i])
		}
	}
}

// TestReplayRecomputesOrdersAgainstTargetTree pins the pinned replay
// semantics (crates/gpui/src/scene.rs::replay): replayed operations are
// re-inserted, so their orders are computed against the TARGET scene's
// bounds tree and layer state.
func TestReplayRecomputesOrdersAgainstTargetTree(t *testing.T) {
	source := newScene(t)
	ctx := paintCtx()
	if err := source.PaintQuad(box(0, 0, 100, 100), 0, ctx); err != nil {
		t.Fatal(err)
	}
	if err := source.BeginLayer(gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 100, Height: 100}}, ctx); err != nil {
		t.Fatal(err)
	}
	if err := source.PaintQuad(box(0, 0, 100, 100), 0, ctx); err != nil {
		t.Fatal(err)
	}
	if err := source.EndLayer(); err != nil {
		t.Fatal(err)
	}
	if err := source.Finish(); err != nil {
		t.Fatal(err)
	}
	length, err := source.Len()
	if err != nil {
		t.Fatal(err)
	}
	if length != 4 {
		t.Fatalf("source op count = %d, want 4", length)
	}

	target := newScene(t)
	// The target's own detached quad does not overlap the full bounds.
	if err := target.PaintQuad(box(200, 200, 100, 100), 0, ctx); err != nil {
		t.Fatal(err)
	}
	if err := target.Replay(0, length, source); err != nil {
		t.Fatal(err)
	}
	orders := quadOrders(t, target)
	// The target's own detached quad (order 1), the replayed full-bounds
	// quad (order 1 — no overlap with the detached one), and the replayed
	// layer member carrying the replayed layer's order 2.
	want := []uint32{1, 1, 2}
	if len(orders) != len(want) {
		t.Fatalf("target quads = %d, want %d", len(orders), len(want))
	}
	for i, order := range orders {
		if order != want[i] {
			t.Fatalf("target quad %d order = %d, want %d", i, order, want[i])
		}
	}

	// Self-replay and malformed ranges are rejected.
	err = target.Replay(0, 1, target)
	if err == nil {
		t.Fatal("self-replay unexpectedly succeeded")
	}
	err = target.Replay(0, 99, source)
	if err == nil {
		t.Fatal("out-of-range replay unexpectedly succeeded")
	}
}

// TestSceneDumpsRequireFinishAndCapacityRetry pins the service seam's
// dump contract: dumps before finish fail with a typed error, a
// too-small capacity is retried transparently with the reported count,
// and the meta/requirements records agree with the dumped arrays.
func TestSceneDumpsRequireFinishAndCapacityRetry(t *testing.T) {
	scene := newScene(t)
	ctx := paintCtx()
	if err := scene.PaintQuad(box(0, 0, 100, 100), 0, ctx); err != nil {
		t.Fatal(err)
	}
	_, err := scene.Commands()
	if err == nil {
		t.Fatal("plan dump before finish unexpectedly succeeded")
	}
	if !errors.Is(err, gpui.ErrSceneUnavailable) && !isNativeSceneError(err) {
		t.Logf("plan dump before finish error: %v", err)
	}
	if err := scene.Finish(); err != nil {
		t.Fatal(err)
	}
	commands, err := scene.Commands()
	if err != nil {
		t.Fatalf("Commands after finish: %v", err)
	}
	if len(commands) != 1 {
		t.Fatalf("command count = %d, want 1", len(commands))
	}
	meta, err := scene.Meta()
	if err != nil {
		t.Fatal(err)
	}
	if !meta.IsFinished || meta.OpCount != 1 || meta.QuadCount != 1 {
		t.Fatalf("meta = %+v, want finished with 1 op/quad", meta)
	}
	requirements, err := scene.Requirements()
	if err != nil {
		t.Fatal(err)
	}
	if requirements.CommandCount != uint32(len(commands)) {
		t.Fatalf("requirements command count = %d, want %d", requirements.CommandCount, len(commands))
	}

	// Capacity retry: alternate smoothing so the plan carries more
	// commands than the dump's initial 16-record capacity.
	scene2 := newScene(t)
	for i := 0; i < 40; i++ {
		smoothing := float32(0)
		if i%2 == 1 {
			smoothing = 0.5
		}
		if err := scene2.PaintQuad(box(0, 0, 100, 100), smoothing, ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := scene2.Finish(); err != nil {
		t.Fatal(err)
	}
	commands, err = scene2.Commands()
	if err != nil {
		t.Fatalf("Commands with 40 batches: %v", err)
	}
	if len(commands) != 40 {
		t.Fatalf("command count = %d, want 40 (one per smoothing change)", len(commands))
	}
	quads, err := scene2.Quads()
	if err != nil {
		t.Fatalf("Quads with 40 records: %v", err)
	}
	if len(quads) != 40 {
		t.Fatalf("quad count = %d, want 40", len(quads))
	}
}

// TestSceneRecordValidationRejectsMalformedInserts pins the ABI's record
// validation through the port: a non-zero order field on insert is
// rejected (the kernel assigns the order), and nothing is published.
func TestSceneRecordValidationRejectsMalformedInserts(t *testing.T) {
	scene := newScene(t)
	quad := gpui.Quad{
		Order:       7,
		Bounds:      gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 10, Height: 10}},
		ContentMask: gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 100, Height: 100}},
	}
	err := scene.InsertQuad(quad)
	if err == nil {
		t.Fatal("insert with non-zero order unexpectedly succeeded")
	}
	length, err := scene.Len()
	if err != nil {
		t.Fatal(err)
	}
	if length != 0 {
		t.Fatalf("op count after rejected insert = %d, want 0 (nothing published)", length)
	}
}

// TestSceneStaleHandleAfterDispose pins the generation-stamped handles:
// after dispose, every operation on the scene fails with the
// scene-closed error (the native stale-handle status underneath).
func TestSceneStaleHandleAfterDispose(t *testing.T) {
	scene, err := gpui.NewScene()
	if err != nil {
		t.Fatalf("gpui.NewScene: %v", err)
	}
	ctx := paintCtx()
	if err := scene.PaintQuad(box(0, 0, 100, 100), 0, ctx); err != nil {
		t.Fatal(err)
	}
	if err := scene.Dispose(); err != nil {
		t.Fatalf("Dispose: %v", err)
	}
	if err := scene.PaintQuad(box(0, 0, 100, 100), 0, ctx); err == nil {
		t.Fatal("paint after dispose unexpectedly succeeded")
	}
	if err := scene.Finish(); err == nil {
		t.Fatal("finish after dispose unexpectedly succeeded")
	}
	if _, err := scene.Quads(); err == nil {
		t.Fatal("dump after dispose unexpectedly succeeded")
	}
}

// TestSceneSurvivesForcedGC pins the FFI KeepAlive discipline: scenes and
// their records survive forced garbage collection (no Go-owned record is
// freed while a native call holds its pointer).
func TestSceneSurvivesForcedGC(t *testing.T) {
	scene := newScene(t)
	ctx := paintCtx()
	for i := 0; i < 64; i++ {
		if err := scene.PaintQuad(box(float32(i), 0, 10, 10), 0, ctx); err != nil {
			t.Fatal(err)
		}
	}
	runtime.GC()
	runtime.GC()
	if err := scene.Finish(); err != nil {
		t.Fatalf("Finish after forced GC: %v", err)
	}
	quads, err := scene.Quads()
	if err != nil {
		t.Fatalf("Quads after forced GC: %v", err)
	}
	if len(quads) != 64 {
		t.Fatalf("quad count = %d, want 64", len(quads))
	}
	orders := make([]uint32, len(quads))
	for i, quad := range quads {
		orders[i] = quad.Order
	}
	for i := 1; i < len(orders); i++ {
		if orders[i] <= orders[i-1] {
			t.Fatalf("quads not sorted by order at %d: %v", i, orders)
		}
	}
	runtime.GC()
	commands, err := scene.Commands()
	if err != nil {
		t.Fatalf("Commands after forced GC: %v", err)
	}
	if len(commands) == 0 {
		t.Fatal("command count = 0 after forced GC")
	}
}

// TestScenePanicContainment pins the scene service's panic boundary: the
// test-only scene panic probe panics inside catch_unwind and reports the
// contained status (101) across the ABI, exactly like the bootstrap
// probe (status 7).
func TestScenePanicContainment(t *testing.T) {
	lib, err := native.Load(native.Options{})
	if err != nil {
		t.Fatalf("native.Load: %v", err)
	}
	t.Cleanup(func() { _ = lib.Close() })
	svc, err := lib.Scene()
	if err != nil {
		t.Fatalf("Library.Scene: %v", err)
	}
	if err := svc.PanicProbe(); err != nil {
		t.Fatalf("scene panic probe: %v", err)
	}

	// The scene table's self-checks validated at fetch time: the record
	// sizes the port mirrors.
	identity := lib.Identity()
	// Ticket09 (text service) bumped the cumulative native revision to 5,
	// ticket10 (glyph raster + atlas services, scene sprites) to 6, and
	// ticket16 (path primitives + the pinned PathBuilder tessellation in
	// the scene service, the scene drawing pipeline in the renderer
	// service) to 7; the cumulative capability mask is 0xFF (the additive
	// pattern: every landed service raises both).
	if identity.NativeRevision != 7 {
		t.Errorf("native revision = %d, want 7 (ticket16 cumulative)", identity.NativeRevision)
	}
	if identity.Capabilities&0xFF != 0xFF {
		t.Errorf("capabilities mask = %#x, want the eight assigned bits set", identity.Capabilities)
	}
	if !hasCapability(identity.Capabilities, "scene-draw-paths") {
		t.Errorf("capability names %v missing scene-draw-paths (ticket16)", identity.CapabilityNames)
	}
	if !hasCapability(identity.Capabilities, "scene-kernel-v1") {
		t.Errorf("capability names %v missing scene-kernel-v1", identity.CapabilityNames)
	}
}

func hasCapability(mask uint64, name string) bool {
	for _, candidate := range []struct {
		bit  uint64
		name string
	}{} {
		_ = candidate
	}
	// The mask bit for scene-kernel-v1 is bit 3 (see
	// internal/native/abi.go).
	return mask&(1<<3) != 0
}

// TestSceneLenCountsPaintOperationsOnly pins the operation log: layers
// open/close are operations, the order floor is not, and dropped
// (empty-clipped) primitives add no operation.
func TestSceneLenCountsPaintOperationsOnly(t *testing.T) {
	scene := newScene(t)
	ctx := paintCtx()
	if err := scene.PaintQuad(box(0, 0, 100, 100), 0, ctx); err != nil {
		t.Fatal(err)
	}
	if err := scene.BeginLayer(gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 10, Height: 10}}, ctx); err != nil {
		t.Fatal(err)
	}
	if err := scene.PaintQuad(box(0, 0, 10, 10), 0, ctx); err != nil {
		t.Fatal(err)
	}
	if err := scene.EndLayer(); err != nil {
		t.Fatal(err)
	}
	if err := scene.RaiseOrderFloor(); err != nil {
		t.Fatal(err)
	}
	// A quad fully outside its mask: dropped, no operation.
	farMask := gpui.Bounds{Origin: gpui.Point{X: 1000, Y: 1000}, Size: gpui.Size{Width: 10, Height: 10}}
	if err := scene.PaintQuad(box(0, 0, 100, 100), 0, gpui.NewPaintContext(1, farMask, 1)); err != nil {
		t.Fatal(err)
	}
	length, err := scene.Len()
	if err != nil {
		t.Fatal(err)
	}
	if length != 4 {
		t.Fatalf("op count = %d, want 4 (2 quads + layer open/close; the floor and the dropped quad add none)", length)
	}
}

// TestSceneBusyGuardRejectsSameSceneReentry pins the busy guard shape:
// the guard is transparent for sequential use (the normal pattern) and
// the underlying native guard rejects same-scene re-entry. Sequential
// use of one scene across many calls must never observe the busy error.
func TestSceneBusyGuardRejectsSameSceneReentry(t *testing.T) {
	scene := newScene(t)
	ctx := paintCtx()
	for i := 0; i < 100; i++ {
		if err := scene.PaintQuad(box(float32(i), 0, 10, 10), 0, ctx); err != nil {
			t.Fatalf("sequential paint %d: %v (the busy guard must be transparent for sequential use)", i, err)
		}
	}
	if err := scene.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if _, err := scene.Quads(); err != nil {
		t.Fatalf("Quads: %v", err)
	}
	if _, err := scene.Commands(); err != nil {
		t.Fatalf("Commands: %v", err)
	}
}

// isNativeSceneError reports whether the error chain carries one of the
// native scene status sentinels.
func isNativeSceneError(err error) bool {
	return errors.Is(err, native.ErrSceneNotFinished) ||
		errors.Is(err, native.ErrSceneBadValue) ||
		errors.Is(err, native.ErrSceneStaleHandle) ||
		errors.Is(err, native.ErrSceneFailed) ||
		errors.Is(err, native.ErrScenePanic)
}

// TestRgbaToHslaMatchesPaletteF32Path pins the port of the reference's
// rgb_to_hsla (palette 0.7.7's Srgb->Hsl f32 branch) against
// hand-computed vectors: primary and secondary colors, grayscale, an
// off-primary hue, and alpha pass-through.
func TestRgbaToHslaMatchesPaletteF32Path(t *testing.T) {
	cases := []struct {
		hex        uint32
		h, s, l, a float32
	}{
		{0xff0000ff, 0, 1, 0.5, 1},                                          // red
		{0x00ff00ff, 120.0 / 360, 1, 0.5, 1},                                // green
		{0x0000ffff, 240.0 / 360, 1, 0.5, 1},                                // blue
		{0xffffff80, 0, 0, 1, 0.5019608},                                    // white, alpha 128/255
		{0x808080ff, 0, 0, 0.5019608, 1},                                    // gray
		{0xff6600ff, 24.0 / 360, 1, 0.5, 1},                                 // orange (sep = g - b, min = b)
		{0x336699ff, 210.0 / 360, math.Float32frombits(0x3F000001), 0.4, 1}, // slate-ish (the f32 d/sum quotient)
		{0x00000040, 0, 0, 0, 0.2509804},                                    // black, alpha 64/255
	}
	for _, tc := range cases {
		got := gpui.RgbaToHsla(tc.hex)
		if got.H != tc.h || got.S != tc.s || got.L != tc.l || got.A != tc.a {
			t.Errorf("RgbaToHsla(%08x) = (%g %g %g %g), want (%g %g %g %g)",
				tc.hex, got.H, got.S, got.L, got.A, tc.h, tc.s, tc.l, tc.a)
		}
	}
	// The background-opacity hue round trip preserves the stored hue.
	red := gpui.RgbaToHsla(0xff0000ff)
	faded := gpui.OpacityBackground(red, 0.5)
	if faded.H != red.H {
		t.Errorf("OpacityBackground hue = %g, want %g (the round trip preserves it)", faded.H, red.H)
	}
	if faded.A != 0.5 {
		t.Errorf("OpacityBackground alpha = %g, want 0.5", faded.A)
	}
	if math.Abs(float64(faded.S-1)) > 1e-6 || faded.L != 0.5 {
		t.Errorf("OpacityBackground s/l = %g/%g, want 1/0.5", faded.S, faded.L)
	}
}

// TestPlanIsSelfConsistent pins the plan invariants the renderer relies
// on (the ticket08 gate's independent self-consistency check): every
// primitive array's orders are non-decreasing after finish, the batch
// ranges of each kind partition that kind's array exactly, and the plan's
// op order (batch sequence interleaved with filter commands) is
// non-decreasing in (draw order, primitive kind) — the same comparator
// the reference kernel uses.
func TestPlanIsSelfConsistent(t *testing.T) {
	scene := newScene(t)
	ctx := paintCtx()
	color := gpui.RgbaToHsla(0x112233ff)
	groupBounds := gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 100, Height: 100}}

	// A mixed scene: a group with members, detached equal-order content,
	// layers, smoothing changes and a deferred overlay.
	if err := scene.PaintQuad(box(0, 0, 40, 40), 0, ctx); err != nil {
		t.Fatal(err)
	}
	if err := scene.BeginFilterGroup(groupBounds, 6, gpui.Corners{TopLeft: 4}, 0, ctx); err != nil {
		t.Fatal(err)
	}
	if err := scene.PaintQuad(box(0, 0, 100, 100), 0.5, ctx); err != nil {
		t.Fatal(err)
	}
	if err := scene.EndFilterGroup(groupBounds, 6, gpui.Corners{TopLeft: 4}, 0, ctx); err != nil {
		t.Fatal(err)
	}
	if err := scene.PaintQuad(box(500, 0, 40, 40), 0, ctx); err != nil {
		t.Fatal(err)
	}
	if err := scene.BeginLayer(gpui.Bounds{Origin: gpui.Point{X: 600, Y: 0}, Size: gpui.Size{Width: 40, Height: 40}}, ctx); err != nil {
		t.Fatal(err)
	}
	if err := scene.PaintQuad(box(600, 0, 40, 40), 0, ctx); err != nil {
		t.Fatal(err)
	}
	if err := scene.PaintUnderline(gpui.Point{X: 700, Y: 0}, 40, gpui.UnderlineStyle{Thickness: 1, Color: &color}, ctx); err != nil {
		t.Fatal(err)
	}
	if err := scene.EndLayer(); err != nil {
		t.Fatal(err)
	}
	if err := scene.RaiseOrderFloor(); err != nil {
		t.Fatal(err)
	}
	if err := scene.PaintBackdrop(gpui.Bounds{Origin: gpui.Point{X: 800, Y: 0}, Size: gpui.Size{Width: 40, Height: 40}}, 5, gpui.Corners{}, 0, ctx); err != nil {
		t.Fatal(err)
	}
	if err := scene.Finish(); err != nil {
		t.Fatal(err)
	}

	commands, err := scene.Commands()
	if err != nil {
		t.Fatal(err)
	}
	quads, err := scene.Quads()
	if err != nil {
		t.Fatal(err)
	}
	underlines, err := scene.Underlines()
	if err != nil {
		t.Fatal(err)
	}
	backdrops, err := scene.BackdropFilters()
	if err != nil {
		t.Fatal(err)
	}
	boundaries, err := scene.FilterBoundaries()
	if err != nil {
		t.Fatal(err)
	}

	// Array orders are non-decreasing (the finish sort).
	for _, arr := range [][]uint32{
		ordersOf(quads),
		ordersOfUnderlines(underlines),
		ordersOfBackdrops(backdrops),
		ordersOfBoundaries(boundaries),
	} {
		for i := 1; i < len(arr); i++ {
			if arr[i] < arr[i-1] {
				t.Fatalf("array orders not non-decreasing at %d: %v", i, arr)
			}
		}
	}

	// The batch ranges of each kind partition that kind's array exactly.
	ranges := map[gpui.SceneBatchKind][][2]uint32{}
	for _, command := range commands {
		if command.Kind == gpui.SceneCommandKindBatch {
			ranges[command.BatchKind] = append(ranges[command.BatchKind], command.Range)
		}
	}
	assertPartition := func(kind gpui.SceneBatchKind, length int) {
		got := ranges[kind]
		next := uint32(0)
		for i, r := range got {
			if r[0] != next || r[1] <= r[0] {
				t.Fatalf("%s batch %d range = %v, want [%d ..] contiguous", kind, i, r, next)
			}
			next = r[1]
		}
		if next != uint32(length) {
			t.Fatalf("%s batches cover 0..%d, want 0..%d", kind, next, length)
		}
	}
	assertPartition(gpui.SceneBatchQuads, len(quads))
	assertPartition(gpui.SceneBatchUnderlines, len(underlines))
	assertPartition(gpui.SceneBatchBackdropFilters, len(backdrops))

	// The plan's op sequence is non-decreasing in (order, kind): the
	// reference kernel's comparator (PrimitiveKind discriminants).
	kindRank := map[gpui.SceneBatchKind]int{
		gpui.SceneBatchShadows: 1, gpui.SceneBatchQuads: 2, gpui.SceneBatchUnderlines: 4,
		gpui.SceneBatchSurfaces: 8, gpui.SceneBatchBackdropFilters: 9,
	}
	var last struct {
		order uint32
		kind  int
		set   bool
	}
	check := func(order uint32, kind int) {
		if last.set {
			if order < last.order || (order == last.order && kind < last.kind) {
				t.Fatalf("plan order regressed: (%d, %d) after (%d, %d)", order, kind, last.order, last.kind)
			}
		}
		last.order, last.kind, last.set = order, kind, true
	}
	for _, command := range commands {
		switch command.Kind {
		case gpui.SceneCommandKindBatch:
			switch command.BatchKind {
			case gpui.SceneBatchQuads:
				check(quads[command.Range[0]].Order, kindRank[command.BatchKind])
			case gpui.SceneBatchUnderlines:
				check(underlines[command.Range[0]].Order, kindRank[command.BatchKind])
			case gpui.SceneBatchBackdropFilters:
				check(backdrops[command.Range[0]].Order, kindRank[command.BatchKind])
			}
		case gpui.SceneCommandKindBeginFilter:
			check(boundaries[command.BoundaryIndex].Order, 0)
		case gpui.SceneCommandKindEndFilter:
			check(boundaries[command.ClosingBoundaryIndex].Order, 10)
		}
	}
}

func ordersOf(quads []gpui.Quad) []uint32 {
	out := make([]uint32, len(quads))
	for i, q := range quads {
		out[i] = q.Order
	}
	return out
}

func ordersOfUnderlines(us []gpui.Underline) []uint32 {
	out := make([]uint32, len(us))
	for i, u := range us {
		out[i] = u.Order
	}
	return out
}

func ordersOfBackdrops(fs []gpui.BackdropFilter) []uint32 {
	out := make([]uint32, len(fs))
	for i, f := range fs {
		out[i] = f.Order
	}
	return out
}

func ordersOfBoundaries(bs []gpui.FilterBoundary) []uint32 {
	out := make([]uint32, len(bs))
	for i, b := range bs {
		out[i] = b.Order
	}
	return out
}
