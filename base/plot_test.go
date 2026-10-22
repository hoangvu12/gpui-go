package base

import (
	"math"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// The grid (4/4), axis (2/2), path cache (2/2) and hover (1/1) tests of
// U:plot/{grid,axis,path_cache,hover}.rs. The path-cache test builds paths
// through the native tessellator, so it needs the native library.

func segmentXs(segments []plotSegment) [][2]float32 {
	out := make([][2]float32, len(segments))
	for i, s := range segments {
		out[i] = [2]float32{s.start.X, s.end.X}
	}
	return out
}

func equalPairs(a, b [][2]float32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func pt(x, y float32) gpui.Point { return gpui.Point{X: x, Y: y} }

// U:plot/grid.rs:162-168
func TestGridSolidLineIsOneSegment(t *testing.T) {
	want := [][2]float32{{0, 10}}
	if got := segmentXs(plotDashSegments(pt(0, 5), pt(10, 5), nil, false)); !equalPairs(got, want) {
		t.Fatalf("None = %v", got)
	}
	if got := segmentXs(plotDashSegments(pt(0, 5), pt(10, 5), []float32{}, true)); !equalPairs(got, want) {
		t.Fatalf("Some(&[]) = %v", got)
	}
}

// U:plot/grid.rs:170-178
func TestGridDashesAlternateAndClipAtTheEnd(t *testing.T) {
	got := segmentXs(plotDashSegments(pt(0, 5), pt(11, 5), []float32{4, 2}, true))
	if want := [][2]float32{{0, 4}, {6, 10}}; !equalPairs(got, want) {
		t.Fatalf("segments = %v, want %v", got, want)
	}
}

// U:plot/grid.rs:180-193
func TestGridOddDashArrayRepeatsLikeSVG(t *testing.T) {
	// 5,3,2 is 5 on, 3 off, 2 on, 5 off, 3 on, 2 off.
	segments := plotDashSegments(pt(0, 0), pt(0, 20), []float32{5, 3, 2}, true)
	ys := make([][2]float32, len(segments))
	for i, s := range segments {
		ys[i] = [2]float32{s.start.Y, s.end.Y}
	}
	if want := [][2]float32{{0, 5}, {8, 10}, {15, 18}}; !equalPairs(ys, want) {
		t.Fatalf("segments = %v, want %v", ys, want)
	}
}

// U:plot/grid.rs:195-203
func TestGridLineBoxIsOnePixelCentredOnTheCoordinate(t *testing.T) {
	vertical := plotLineBounds(pt(10, 0), pt(10, 40))
	if vertical.Origin != pt(9.5, 0) || vertical.Size != (gpui.Size{Width: 1, Height: 40}) {
		t.Fatalf("vertical = %v", vertical)
	}
	horizontal := plotLineBounds(pt(40, 7), pt(0, 7))
	if horizontal.Origin != pt(0, 6.5) || horizontal.Size != (gpui.Size{Width: 40, Height: 1}) {
		t.Fatalf("horizontal = %v", horizontal)
	}
}

// U:plot/axis.rs:260-265
func plotAxisLabels() []AxisText {
	return []AxisText{
		NewAxisText("a", 10, gpui.Hsla{}),
		NewAxisText("b", 20, gpui.Hsla{}).WithAlign(gpui.TextAlignRight),
	}
}

type textOrigin struct {
	text   string
	origin gpui.Point
}

func plotTextOrigins(texts []PlotText) []textOrigin {
	out := make([]textOrigin, len(texts))
	for i, t := range texts {
		out[i] = textOrigin{t.Text, t.Origin}
	}
	return out
}

func equalOrigins(a, b []textOrigin) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// U:plot/axis.rs:271-301
func TestAxisBuilderOrderDoesNotMoveLabels(t *testing.T) {
	labelsFirst := NewPlotAxis().
		XLabel(plotAxisLabels()...).X(50).XLabelSide(AxisLabelSideStart).
		YLabel(plotAxisLabels()...).Y(30).YLabelSide(AxisLabelSideStart)
	labelsLast := NewPlotAxis().
		XLabelSide(AxisLabelSideStart).X(50).XLabel(plotAxisLabels()...).
		YLabelSide(AxisLabelSideStart).Y(30).YLabel(plotAxisLabels()...)

	if !equalOrigins(plotTextOrigins(labelsFirst.xTexts(50)), plotTextOrigins(labelsLast.xTexts(50))) {
		t.Fatal("x labels moved with the builder order")
	}
	if !equalOrigins(plotTextOrigins(labelsFirst.yTexts(30)), plotTextOrigins(labelsLast.yTexts(30))) {
		t.Fatal("y labels moved with the builder order")
	}
	// The side set after the labels still applies to them.
	if y := labelsFirst.xTexts(50)[0].Origin.Y; y != 50-PlotTextGap-PlotTextHeight {
		t.Fatalf("x label y = %v", y)
	}
}

// U:plot/axis.rs:303-306
func TestAxisGutterFitsDefaultLabels(t *testing.T) {
	if g := AxisGutter(PlotTextSize); g != 18 {
		t.Fatalf("gutter = %v", g)
	}
}

// diagonal is U:plot/path_cache.rs:139-144.
func plotDiagonal() (*gpui.Path, bool) {
	b := gpui.StrokePathBuilder(2)
	b.MoveTo(0, 0)
	b.LineTo(10, 10)
	path, err := b.Build()
	if err != nil {
		return nil, false
	}
	return path, true
}

// U:plot/path_cache.rs:146-177 (native tessellator).
func TestPathCacheBuildsOncePerKeyAndMovesToEachOrigin(t *testing.T) {
	if _, ok := plotDiagonal(); !ok {
		_, err := gpui.StrokePathBuilder(2).MoveTo(0, 0).LineTo(10, 10).Build()
		t.Fatalf("the native tessellator is unavailable: %v", err)
	}
	var cache PathCache
	builds := 0
	build := func() (*gpui.Path, bool) {
		builds++
		return plotDiagonal()
	}
	first, ok1 := cache.Get(1, pt(100, 50), build)
	second, ok2 := cache.Get(1, pt(200, 50), build)
	if !ok1 || !ok2 {
		t.Fatal("cache returned None")
	}
	if builds != 1 {
		t.Fatalf("builds = %d, want 1", builds)
	}
	if len(first.Vertices) != len(second.Vertices) {
		t.Fatalf("vertex counts %d != %d", len(first.Vertices), len(second.Vertices))
	}
	for i := range first.Vertices {
		a, b := first.Vertices[i], second.Vertices[i]
		if b.X-a.X != 100 || b.Y != a.Y || a.S != b.S || a.T != b.T {
			t.Fatalf("vertex %d: %+v -> %+v", i, a, b)
		}
	}
	if second.Bounds.Origin.X-first.Bounds.Origin.X != 100 || first.Bounds.Size != second.Bounds.Size {
		t.Fatalf("bounds %v -> %v", first.Bounds, second.Bounds)
	}
	cache.Get(2, pt(0, 0), build)
	if builds != 2 {
		t.Fatalf("builds = %d, want 2", builds)
	}
}

// U:plot/path_cache.rs:179-196
func TestPathCacheKeysFollowPointsAndExtras(t *testing.T) {
	type extra struct {
		curve string
		width uint32
	}
	key := func(width float32, p gpui.Point) uint64 {
		return NewShapeKey(extra{"linear", math.Float32bits(width)}).Point(p).Finish()
	}
	a := key(1, pt(1, 2))
	same := key(1, pt(1, 2))
	moved := key(1, pt(1, 3))
	thicker := key(2, pt(1, 2))
	if a != same || a == moved || a == thicker {
		t.Fatalf("keys a=%x same=%x moved=%x thicker=%x", a, same, moved, thicker)
	}
}

// U:plot/hover.rs:232-260
func TestPlotHoverReaders(t *testing.T) {
	state := NewTooltipState(2, pt(10, 20), nil)
	hover := PlotHover{state: state, progress: 1, hovered: true}
	if hover.State().Index != 2 || !hover.IsHovered() {
		t.Fatalf("hover = %+v", hover)
	}
	// Fully faded in: a pointer keeps travelling rather than snapping.
	if hover.IsEntering() {
		t.Fatal("fully faded in hover is entering")
	}
	// The first hovered frame, before the fade has started.
	entering := hover
	entering.progress = 0
	if !entering.IsEntering() {
		t.Fatal("first hovered frame is not entering")
	}
	// Fading out after the cursor left: neither hovered nor entering.
	lingering := hover
	lingering.progress, lingering.hovered = 0.4, false
	if lingering.IsHovered() || lingering.IsEntering() {
		t.Fatal("lingering hover is hovered or entering")
	}
}
