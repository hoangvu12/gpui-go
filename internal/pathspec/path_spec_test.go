// Package pathspec tests the ticket16 scene semantics: the path
// builder (the pinned PathBuilder tessellation behind the native scene
// service), the filter-group plan records at nesting depths beyond two,
// the paired surface opacity, the scene-kernel regression against the
// pre-path output, and the real-window composite draw (a filled path +
// a text sprite + a filter group rendered to a real surface, presented
// and retired through the renderer ledger).
//
// Environment expectation: this machine runs interactively with a
// D3D11-capable adapter — window creation failures fail the tests
// instead of skipping (the winhost contract).
package pathspec

import (
	"errors"
	"math"
	"runtime"
	"testing"

	"gpui-go/gpui"
	"gpui-go/internal/native"
)

// ---------------------------------------------------------------------------
// Path builder semantics (the pinned PathBuilder through the native
// tessellation entry)
// ---------------------------------------------------------------------------

// fillSquare builds a closed square script (the fixture's first path).
func fillSquare(x, y, w, h float32) *gpui.PathBuilder {
	b := gpui.NewPathBuilder()
	b.MoveTo(x, y)
	b.LineTo(x+w, y)
	b.LineTo(x+w, y+h)
	b.LineTo(x, y+h)
	b.Close()
	return b
}

func TestPathBuilderFillSemantics(t *testing.T) {
	path, err := fillSquare(4, 4, 26, 24).Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := len(path.Vertices); got != 6 {
		t.Fatalf("vertex count = %d, want 6 (a closed square fills as two triangles)", got)
	}
	if path.Bounds.Origin.X != 4 || path.Bounds.Origin.Y != 4 || path.Bounds.Size.Width != 26 || path.Bounds.Size.Height != 24 {
		t.Errorf("bounds = %+v, want (4, 4, 26, 24)", path.Bounds)
	}
	for i, vertex := range path.Vertices {
		if vertex.S != 0 || vertex.T != 1 {
			t.Errorf("vertex %d st = (%v, %v), want the pinned constant (0, 1)", i, vertex.S, vertex.T)
		}
	}
	// Every vertex lies inside the bounds.
	for i, vertex := range path.Vertices {
		if vertex.X < path.Bounds.Origin.X || vertex.X > path.Bounds.Origin.X+path.Bounds.Size.Width ||
			vertex.Y < path.Bounds.Origin.Y || vertex.Y > path.Bounds.Origin.Y+path.Bounds.Size.Height {
			t.Errorf("vertex %d (%v, %v) lies outside the bounds %+v", i, vertex.X, vertex.Y, path.Bounds)
		}
	}
}

func TestPathBuilderStrokeCapsJoinsAndDash(t *testing.T) {
	// A butt-capped stroke.
	stroke := gpui.NewPathBuilder()
	stroke = stroke.WithStyle(gpui.StrokeStyle(gpui.DefaultStrokeOptions()))
	stroke.MoveTo(36, 4)
	stroke.LineTo(60, 4)
	path, err := stroke.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := len(path.Vertices); got < 3 {
		t.Fatalf("stroke vertex count = %d, want at least one triangle", got)
	}

	// Round caps + round joins add cap/join geometry.
	roundOptions := gpui.DefaultStrokeOptions()
	roundOptions.LineWidth = 4
	roundOptions.StartCap = gpui.LineCapRound
	roundOptions.EndCap = gpui.LineCapRound
	roundOptions.LineJoin = gpui.LineJoinRound
	round := gpui.NewPathBuilder()
	round = round.WithStyle(gpui.StrokeStyle(roundOptions))
	round.MoveTo(70, 4)
	round.LineTo(90, 14)
	round.LineTo(110, 4)
	roundPath, err := round.Build()
	if err != nil {
		t.Fatalf("Build (round): %v", err)
	}
	if got := len(roundPath.Vertices); got <= len(path.Vertices) {
		t.Errorf("round caps/joins vertex count = %d, want more than the butt-capped %d", got, len(path.Vertices))
	}

	// The dash array (the pinned odd-length repetition: 5,3,2 behaves as
	// 5,3,2,5,3,2).
	dashed := gpui.NewPathBuilder()
	dashed = dashed.WithStyle(gpui.StrokeStyle(gpui.DefaultStrokeOptions()))
	dashed = dashed.DashArray([]float32{4, 2})
	dashed.MoveTo(36, 34)
	dashed.LineTo(80, 34)
	dashedPath, err := dashed.Build()
	if err != nil {
		t.Fatalf("Build (dashed): %v", err)
	}
	if got := len(dashedPath.Vertices); got == 0 {
		t.Fatalf("dashed stroke vertex count = 0, want dash segments")
	}

	// Fill options: tolerance and the non-zero rule are accepted.
	filled := gpui.NewPathBuilder()
	options := gpui.DefaultFillOptions()
	options.Tolerance = 0.05
	options.FillRule = gpui.FillRuleNonZero
	filled = filled.WithStyle(gpui.FillStyle(options))
	filled.MoveTo(4, 34)
	filled.CurveTo(24, 34, 14, 54)
	curvePath, err := filled.Build()
	if err != nil {
		t.Fatalf("Build (curve): %v", err)
	}
	if got := len(curvePath.Vertices); got < 3 {
		t.Fatalf("quadratic fill vertex count = %d, want at least one triangle", got)
	}
}

func TestPathBuilderTransforms(t *testing.T) {
	// Translate then scale: the pinned builder composes with euclid's
	// post-composition (`then_scale` applies the scale AFTER the
	// accumulated transform), so (0,0) -> (2,62) -> (3,93) and
	// (20,20) -> (22,82) -> (33,123).
	b := gpui.NewPathBuilder()
	b = b.Translate(2, 62)
	b = b.Scale(1.5)
	b.MoveTo(0, 0)
	b.LineTo(20, 0)
	b.LineTo(20, 20)
	b.Close()
	path, err := b.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if path.Bounds.Origin.X != 3 || path.Bounds.Origin.Y != 93 {
		t.Errorf("bounds origin = (%v, %v), want (3, 93)", path.Bounds.Origin.X, path.Bounds.Origin.Y)
	}
	if path.Bounds.Size.Width != 30 || path.Bounds.Size.Height != 30 {
		t.Errorf("bounds size = (%v, %v), want (30, 30)", path.Bounds.Size.Width, path.Bounds.Size.Height)
	}
}

func TestPathBuilderEmptyTessellation(t *testing.T) {
	// A stroke with a single move + close tessellates to nothing: the
	// pinned build_path fallback returns a zero path.
	b := gpui.NewPathBuilder()
	b = b.WithStyle(gpui.StrokeStyle(gpui.DefaultStrokeOptions()))
	b.MoveTo(0, 0)
	b.Close()
	path, err := b.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := len(path.Vertices); got != 0 {
		t.Errorf("empty tessellation vertex count = %d, want 0", got)
	}
	if path.Bounds.Origin.X != 0 || path.Bounds.Origin.Y != 0 || path.Bounds.Size.Width != 0 || path.Bounds.Size.Height != 0 {
		t.Errorf("empty tessellation bounds = %+v, want the zero bounds", path.Bounds)
	}
}

func TestPathBuilderDeterministicBits(t *testing.T) {
	build := func() ([]uint32, error) {
		path, err := fillSquare(4, 4, 26, 24).Build()
		if err != nil {
			return nil, err
		}
		bits := make([]uint32, 0, len(path.Vertices)*4)
		for _, vertex := range path.Vertices {
			bits = append(bits, math.Float32bits(vertex.X), math.Float32bits(vertex.Y), math.Float32bits(vertex.S), math.Float32bits(vertex.T))
		}
		return bits, nil
	}
	first, err := build()
	if err != nil {
		t.Fatalf("first build: %v", err)
	}
	for i := 0; i < 3; i++ {
		again, err := build()
		if err != nil {
			t.Fatalf("build %d: %v", i, err)
		}
		if len(again) != len(first) {
			t.Fatalf("build %d vertex word count = %d, want %d", i, len(again), len(first))
		}
		for j := range first {
			if again[j] != first[j] {
				t.Fatalf("build %d word %d = %#x, want %#x (the tessellation must be deterministic)", i, j, again[j], first[j])
			}
		}
	}
}

// ---------------------------------------------------------------------------
// PaintPath: the pinned record construction (scale, mask, opacity)
// ---------------------------------------------------------------------------

func TestPaintPathScalesIntoDevicePixels(t *testing.T) {
	scene, err := gpui.NewScene()
	if err != nil {
		t.Fatalf("NewScene: %v", err)
	}
	defer scene.Dispose()

	path, err := fillSquare(10, 10, 20, 20).Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	mask := gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 200, Height: 200}}
	ctx := gpui.NewPaintContext(2.0, mask, 1.0)
	red := gpui.SolidBackground(gpui.RgbaToHsla(0xdc2626ff))
	if err := scene.PaintPath(path, red, ctx); err != nil {
		t.Fatalf("PaintPath: %v", err)
	}
	if err := scene.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	records, vertices, err := scene.Paths()
	if err != nil {
		t.Fatalf("Paths: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("path count = %d, want 1", len(records))
	}
	record := records[0]
	if record.Order != 1 {
		t.Errorf("order = %d, want 1", record.Order)
	}
	// The logical (10,10,20,20) scales to device (20,20,40,40).
	if record.Bounds.Origin.X != 20 || record.Bounds.Origin.Y != 20 || record.Bounds.Size.Width != 40 || record.Bounds.Size.Height != 40 {
		t.Errorf("device bounds = %+v, want (20, 20, 40, 40)", record.Bounds)
	}
	// The content mask scales too (the pinned Path::scale).
	if record.ContentMask.Origin.X != 0 || record.ContentMask.Size.Width != 400 {
		t.Errorf("device mask = %+v, want the scaled case mask", record.ContentMask)
	}
	// The vertices scale; the st coordinates do not.
	if len(vertices[0]) != 6 {
		t.Fatalf("vertex count = %d, want 6", len(vertices[0]))
	}
	for i, vertex := range vertices[0] {
		if math.Float32bits(vertex.X)%2 != 0 || vertex.S != 0 || vertex.T != 1 {
			t.Errorf("vertex %d = %+v, want even device coordinates and the st constant", i, vertex)
			break
		}
	}
	// The plan: one path batch with the summed vertex count and the
	// per-path sprite count.
	commands, err := scene.Commands()
	if err != nil {
		t.Fatalf("Commands: %v", err)
	}
	if len(commands) != 1 || commands[0].BatchKind != gpui.SceneBatchPaths {
		t.Fatalf("commands = %+v, want one paths batch", commands)
	}
	if commands[0].RasterizationVertexCount != 6 {
		t.Errorf("rasterizationVertexCount = %d, want 6", commands[0].RasterizationVertexCount)
	}
	if commands[0].SpriteCount != 1 {
		t.Errorf("spriteCount = %d, want 1", commands[0].SpriteCount)
	}
	requirements, err := scene.Requirements()
	if err != nil {
		t.Fatalf("Requirements: %v", err)
	}
	if requirements.UsesPathTarget != true || requirements.PathSpriteCount != 1 || requirements.PathRasterizationVertexCount != 6 {
		t.Errorf("requirements = %+v, want uses_path_target with 6 vertices and 1 sprite", requirements)
	}
	if requirements.InstanceBatchCount != 2 {
		t.Errorf("instanceBatchCount = %d, want 2 (the pinned Paths arm counts rasterization + sprite)", requirements.InstanceBatchCount)
	}
}

func TestPaintPathEmptyClippedPathDrops(t *testing.T) {
	scene, err := gpui.NewScene()
	if err != nil {
		t.Fatalf("NewScene: %v", err)
	}
	defer scene.Dispose()

	// A path whose scaled bounds fall entirely outside the content mask
	// drops (the pinned empty-clipped primitive rule).
	path, err := fillSquare(400, 400, 20, 20).Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	mask := gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 200, Height: 200}}
	ctx := gpui.NewPaintContext(2.0, mask, 1.0)
	if err := scene.PaintPath(path, gpui.SolidBackground(gpui.RgbaToHsla(0xff0000ff)), ctx); err != nil {
		t.Fatalf("PaintPath: %v", err)
	}
	if err := scene.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	records, _, err := scene.Paths()
	if err != nil {
		t.Fatalf("Paths: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("path count = %d, want 0 (the clipped path drops)", len(records))
	}
	meta, err := scene.Meta()
	if err != nil {
		t.Fatalf("Meta: %v", err)
	}
	if meta.OpCount != 0 {
		t.Errorf("op count = %d, want 0 (no log entry either)", meta.OpCount)
	}
}

// ---------------------------------------------------------------------------
// Filter-group plan records at nesting depth 3+
// ---------------------------------------------------------------------------

func TestFilterGroupPlanRecordsBeyondDepthTwo(t *testing.T) {
	scene, err := gpui.NewScene()
	if err != nil {
		t.Fatalf("NewScene: %v", err)
	}
	defer scene.Dispose()

	// Three nested groups with OVERLAPPING bounds (the whole scene): the
	// pinned plan isolates the first two (targets 0 and 1) and renders
	// the third inline — no invented third isolation target.
	full := gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 100, Height: 100}}
	ctx := gpui.NewPaintContext(1.0, full, 1.0)
	radii := gpui.Corners{}

	if err := scene.BeginFilterGroup(full, 3, radii, 0, ctx); err != nil {
		t.Fatalf("begin 1: %v", err)
	}
	if err := scene.BeginFilterGroup(full, 2, radii, 0, ctx); err != nil {
		t.Fatalf("begin 2: %v", err)
	}
	if err := scene.BeginFilterGroup(full, 2, radii, 0, ctx); err != nil {
		t.Fatalf("begin 3: %v", err)
	}
	if err := scene.EndFilterGroup(full, 2, radii, 0, ctx); err != nil {
		t.Fatalf("end 3: %v", err)
	}
	if err := scene.EndFilterGroup(full, 2, radii, 0, ctx); err != nil {
		t.Fatalf("end 2: %v", err)
	}
	if err := scene.EndFilterGroup(full, 3, radii, 0, ctx); err != nil {
		t.Fatalf("end 1: %v", err)
	}
	if err := scene.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	commands, err := scene.Commands()
	if err != nil {
		t.Fatalf("Commands: %v", err)
	}
	// The expected command stream: begin Isolated(0), begin Isolated(1),
	// begin Inline, end Inline, end Isolated(1), end Isolated(0).
	want := []struct {
		kind     gpui.SceneCommandKind
		isolated bool
		index    uint32
	}{
		{gpui.SceneCommandKindBeginFilter, true, 0},
		{gpui.SceneCommandKindBeginFilter, true, 1},
		{gpui.SceneCommandKindBeginFilter, false, 0},
		{gpui.SceneCommandKindEndFilter, false, 0},
		{gpui.SceneCommandKindEndFilter, true, 1},
		{gpui.SceneCommandKindEndFilter, true, 0},
	}
	if len(commands) != len(want) {
		t.Fatalf("command count = %d, want %d (%+v)", len(commands), len(want), commands)
	}
	for i, expected := range want {
		if commands[i].Kind != expected.kind {
			t.Fatalf("command %d kind = %d, want %d", i, commands[i].Kind, expected.kind)
		}
		if commands[i].Target.Isolated != expected.isolated {
			t.Errorf("command %d isolated = %v, want %v", i, commands[i].Target.Isolated, expected.isolated)
		}
		if expected.isolated && commands[i].Target.Index != expected.index {
			t.Errorf("command %d target index = %d, want %d", i, commands[i].Target.Index, expected.index)
		}
	}
	// The end commands carry their matched start's boundary index.
	if commands[4].BoundaryIndex != commands[1].BoundaryIndex {
		t.Errorf("end Isolated(1) boundaryIndex = %d, want the matched begin's %d", commands[4].BoundaryIndex, commands[1].BoundaryIndex)
	}
	if commands[5].BoundaryIndex != commands[0].BoundaryIndex {
		t.Errorf("end Isolated(0) boundaryIndex = %d, want the matched begin's %d", commands[5].BoundaryIndex, commands[0].BoundaryIndex)
	}

	requirements, err := scene.Requirements()
	if err != nil {
		t.Fatalf("Requirements: %v", err)
	}
	if requirements.IsolatedTargetCount != 2 {
		t.Errorf("isolatedTargetCount = %d, want 2 (MAX_FILTER_GROUP_DEPTH)", requirements.IsolatedTargetCount)
	}
	if requirements.IsolatedFilterCount != 2 {
		t.Errorf("isolatedFilterCount = %d, want 2", requirements.IsolatedFilterCount)
	}
	if !requirements.UsesOffscreenTarget {
		t.Errorf("usesOffscreenTarget = false, want true")
	}
}

func TestFilterGroupDepthFourStillInline(t *testing.T) {
	scene, err := gpui.NewScene()
	if err != nil {
		t.Fatalf("NewScene: %v", err)
	}
	defer scene.Dispose()

	full := gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 100, Height: 100}}
	ctx := gpui.NewPaintContext(1.0, full, 1.0)
	radii := gpui.Corners{}
	for i := 0; i < 4; i++ {
		if err := scene.BeginFilterGroup(full, 2, radii, 0, ctx); err != nil {
			t.Fatalf("begin %d: %v", i, err)
		}
	}
	for i := 0; i < 4; i++ {
		if err := scene.EndFilterGroup(full, 2, radii, 0, ctx); err != nil {
			t.Fatalf("end %d: %v", i, err)
		}
	}
	if err := scene.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	requirements, err := scene.Requirements()
	if err != nil {
		t.Fatalf("Requirements: %v", err)
	}
	// Groups three and four render inline; the target pool stays at two.
	if requirements.IsolatedTargetCount != 2 {
		t.Errorf("isolatedTargetCount = %d, want 2 even at depth 4", requirements.IsolatedTargetCount)
	}
	commands, err := scene.Commands()
	if err != nil {
		t.Fatalf("Commands: %v", err)
	}
	isolated, inline := 0, 0
	for _, command := range commands {
		if command.Target.Isolated {
			isolated++
		} else {
			inline++
		}
	}
	if isolated != 4 || inline != 4 {
		t.Errorf("isolated/inline commands = %d/%d, want 4/4 (begins and ends of two isolated groups, two inline)", isolated, inline)
	}
}

// ---------------------------------------------------------------------------
// Paired surface opacity
// ---------------------------------------------------------------------------

func TestSurfaceOpacityPairsSurviveFinishAndReplay(t *testing.T) {
	scene, err := gpui.NewScene()
	if err != nil {
		t.Fatalf("NewScene: %v", err)
	}
	defer scene.Dispose()

	full := gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 100, Height: 100}}
	// Two overlapping surfaces with different opacities: the pairs stay
	// together through the finish sort (the pinned zip/sort/unzip).
	if err := scene.InsertSurface(full, full, gpui.SurfaceSourceNone, 0.9); err != nil {
		t.Fatalf("InsertSurface 1: %v", err)
	}
	if err := scene.InsertSurface(full, full, gpui.SurfaceSourceNone, 0.25); err != nil {
		t.Fatalf("InsertSurface 2: %v", err)
	}
	if err := scene.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	surfaces, opacities, err := scene.Surfaces()
	if err != nil {
		t.Fatalf("Surfaces: %v", err)
	}
	if len(surfaces) != 2 || len(opacities) != 2 {
		t.Fatalf("surface/opacity counts = %d/%d, want 2/2", len(surfaces), len(opacities))
	}
	if opacities[0] != 0.9 || opacities[1] != 0.25 {
		t.Errorf("opacities = %v, want [0.9 0.25] in draw order", opacities)
	}
	if surfaces[0].Order != 1 || surfaces[1].Order != 2 {
		t.Errorf("orders = %d/%d, want 1/2", surfaces[0].Order, surfaces[1].Order)
	}

	// Replay preserves the pairing.
	replay, err := gpui.NewScene()
	if err != nil {
		t.Fatalf("NewScene (replay): %v", err)
	}
	defer replay.Dispose()
	if err := replay.Replay(0, 2, scene); err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if err := replay.Finish(); err != nil {
		t.Fatalf("Finish (replay): %v", err)
	}
	_, replayOpacities, err := replay.Surfaces()
	if err != nil {
		t.Fatalf("Surfaces (replay): %v", err)
	}
	if len(replayOpacities) != 2 || replayOpacities[0] != 0.9 || replayOpacities[1] != 0.25 {
		t.Errorf("replay opacities = %v, want [0.9 0.25]", replayOpacities)
	}
}

// ---------------------------------------------------------------------------
// Scene regression: paths do not reorder the pre-path scene output
// ---------------------------------------------------------------------------

func TestPathsDoNotReorderExistingSceneOutput(t *testing.T) {
	// The pinned PrimitiveKind discriminant order decides same-order
	// emission; paths sit between quads and underlines and cannot
	// reorder quads relative to each other.
	scene, err := gpui.NewScene()
	if err != nil {
		t.Fatalf("NewScene: %v", err)
	}
	defer scene.Dispose()

	full := gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 100, Height: 100}}
	ctx := gpui.NewPaintContext(1.0, full, 1.0)
	redHsla := gpui.RgbaToHsla(0xdc2626ff)
	greenHsla := gpui.RgbaToHsla(0x16a34aff)
	red := gpui.SolidBackground(redHsla)
	green := gpui.SolidBackground(greenHsla)

	// A quad, a path, an underline, another quad — all overlapping (the
	// orders step 1..4).
	if err := scene.PaintQuad(gpui.PaintQuad{Bounds: full, Background: red}, 0, ctx); err != nil {
		t.Fatalf("PaintQuad 1: %v", err)
	}
	path, err := fillSquare(10, 10, 20, 20).Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := scene.PaintPath(path, green, ctx); err != nil {
		t.Fatalf("PaintPath: %v", err)
	}
	if err := scene.PaintUnderline(gpui.Point{X: 10, Y: 50}, 80, gpui.UnderlineStyle{Thickness: 1, Color: &redHsla}, ctx); err != nil {
		t.Fatalf("PaintUnderline: %v", err)
	}
	if err := scene.PaintQuad(gpui.PaintQuad{Bounds: full, Background: green}, 0, ctx); err != nil {
		t.Fatalf("PaintQuad 2: %v", err)
	}
	if err := scene.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	commands, err := scene.Commands()
	if err != nil {
		t.Fatalf("Commands: %v", err)
	}
	// The expected stream: quad 0..1, path 0..1, underline 0..1, quad
	// 1..2 (orders 1..4; the path draws between the quad and the
	// underline per the discriminant order, and after them by order).
	var kinds []gpui.SceneBatchKind
	for _, command := range commands {
		if command.Kind == gpui.SceneCommandKindBatch {
			kinds = append(kinds, command.BatchKind)
		}
	}
	want := []gpui.SceneBatchKind{gpui.SceneBatchQuads, gpui.SceneBatchPaths, gpui.SceneBatchUnderlines, gpui.SceneBatchQuads}
	if len(kinds) != len(want) {
		t.Fatalf("batch kinds = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("batch kinds = %v, want %v", kinds, want)
		}
	}
	quads, err := scene.Quads()
	if err != nil {
		t.Fatalf("Quads: %v", err)
	}
	// The underline (y 50..51) does not overlap the path (y 10..30), so
	// it reuses order 2 and the final full-bounds quad steps to 3.
	if quads[0].Order != 1 || quads[1].Order != 3 {
		t.Errorf("quad orders = %d/%d, want 1/3", quads[0].Order, quads[1].Order)
	}
}

// ---------------------------------------------------------------------------
// Forced GC + stale native handles
// ---------------------------------------------------------------------------

func TestSceneHandlesSurviveForcedGC(t *testing.T) {
	scene, err := gpui.NewScene()
	if err != nil {
		t.Fatalf("NewScene: %v", err)
	}
	defer scene.Dispose()

	full := gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 100, Height: 100}}
	ctx := gpui.NewPaintContext(1.0, full, 1.0)
	path, err := fillSquare(10, 10, 20, 20).Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := scene.PaintPath(path, gpui.SolidBackground(gpui.RgbaToHsla(0xff0000ff)), ctx); err != nil {
		t.Fatalf("PaintPath: %v", err)
	}
	// The native scene handle must survive garbage collections (the
	// native registry owns it; Go references do not pin it).
	runtime.GC()
	runtime.GC()
	if err := scene.Finish(); err != nil {
		t.Fatalf("Finish after GC: %v", err)
	}
	runtime.GC()
	meta, err := scene.Meta()
	if err != nil {
		t.Fatalf("Meta after GC: %v", err)
	}
	if meta.PathCount != 1 {
		t.Errorf("path count after GC = %d, want 1", meta.PathCount)
	}
	// A disposed scene rejects further use with the typed closed error
	// (not a crash); the native slot generation keeps the raw handle
	// stale too.
	if err := scene.Dispose(); err != nil {
		t.Fatalf("Dispose: %v", err)
	}
	if err := scene.Finish(); !errors.Is(err, gpui.ErrSceneClosed) {
		t.Errorf("Finish after Dispose = %v, want ErrSceneClosed", err)
	}
	lib, err := native.Load(native.Options{})
	if err != nil {
		t.Fatalf("native.Load: %v", err)
	}
	svc, err := lib.Scene()
	if err != nil {
		t.Fatalf("lib.Scene: %v", err)
	}
	if err := svc.PanicProbe(); err != nil {
		t.Fatalf("scene panic probe (service health after GC): %v", err)
	}
}
