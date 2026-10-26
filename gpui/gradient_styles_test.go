package gpui

import (
	"math"
	"testing"
)

// The CSS-gradient resolution tests of browser-core ticket 57: the
// color-stop fixup (Chromium css_gradient_value.cc:658-788), the
// geometry words each shape resolves to (the linear gradient line,
// the radial end shape, the conic sweep) and the paint seam that
// swaps a CSS-gradient Background for the table reference.

func stopAt(c Hsla, position float32) GradientStop {
	return GradientStop{Color: c, Position: position}
}

// stopUnset is an unspecified-position stop (CSS's omitted position).
func stopUnset(c Hsla) GradientStop {
	return GradientStop{Color: c, Position: float32(math.NaN())}
}

var gradientTestRed = Hsla{H: 0, S: 1, L: 0.5, A: 1}
var gradientTestGreen = Hsla{H: 1.0 / 3.0, S: 1, L: 0.5, A: 1}
var gradientTestBlue = Hsla{H: 2.0 / 3.0, S: 1, L: 0.5, A: 1}

func TestFixGradientStopsAppliesCSSStopFixup(t *testing.T) {
	// Unspecified ends resolve to 0 and 1, unspecified middles
	// distribute evenly, and a descending specified position clamps to
	// the largest specified position before it.
	fixed := fixGradientStops([]GradientStop{
		stopUnset(gradientTestRed),
		{Color: gradientTestGreen, Position: 0.8},
		{Color: gradientTestBlue, Position: 0.6},
		{Color: gradientTestRed, Position: 1},
	})
	want := []float32{0, 0.8, 0.8, 1}
	if len(fixed.stops) != len(want) {
		t.Fatalf("fixGradientStops produced %d stops, want %d: %+v", len(fixed.stops), len(want), fixed.stops)
	}
	for i, position := range want {
		if fixed.stops[i].Position != position {
			t.Fatalf("stop %d at %v, want %v (stops: %+v)", i, fixed.stops[i].Position, position, fixed.stops)
		}
	}
	if fixed.first != 0 || fixed.last != 1 {
		t.Fatalf("span [%v, %v], want [0, 1]", fixed.first, fixed.last)
	}

	// A specified first/last stop keeps its own position.
	fixed = fixGradientStops([]GradientStop{
		{Color: gradientTestRed, Position: 0.25},
		{Color: gradientTestBlue, Position: 0.75},
	})
	if fixed.first != 0.25 || fixed.last != 0.75 {
		t.Fatalf("specified ends kept [%v, %v], want [0.25, 0.75]", fixed.first, fixed.last)
	}

	// Every stop unspecified in the middle (three stops: 0, 1/2, 1).
	fixed = fixGradientStops([]GradientStop{
		stopUnset(gradientTestRed),
		stopUnset(gradientTestGreen),
		stopUnset(gradientTestBlue),
	})
	for i, position := range []float32{0, 0.5, 1} {
		if fixed.stops[i].Position != position {
			t.Fatalf("even distribution: stop %d at %v, want %v", i, fixed.stops[i].Position, position)
		}
	}
}

func TestResolveCssLinearGradientGeometry(t *testing.T) {
	// to bottom (0deg, wait: to bottom is 180deg) over a 100x50 box at
	// scale 2: the line runs down the box's vertical center line,
	// 100 device px long.
	box := Bounds{Origin: Point{X: 10, Y: 20}, Size: Size{Width: 100, Height: 50}}
	gradient, err := resolveCssGradient(CssGradient{
		Kind:      CssGradientLinear,
		Direction: GradientDirectionToBottom,
		Stops: []GradientStop{
			stopAt(gradientTestRed, 0),
			stopUnset(gradientTestGreen),
			stopAt(gradientTestBlue, 1),
		},
	}, box, 2)
	if err != nil {
		t.Fatalf("resolveCssGradient: %v", err)
	}
	if gradient.Kind != GradientLinear || gradient.Tile != GradientTilePad {
		t.Fatalf("kind/tile: %v/%v", gradient.Kind, gradient.Tile)
	}
	x0, y0 := gradient.Geometry[0], gradient.Geometry[1]
	x1, y1 := gradient.Geometry[2], gradient.Geometry[3]
	if !approxEqual(x0, 120) || !approxEqual(x1, 120) || !approxEqual(y0, 40) || !approxEqual(y1, 140) {
		t.Fatalf("linear geometry (%v,%v)-(%v,%v), want (120,40)-(120,140) — the box's vertical center line, 100 device px long", x0, y0, x1, y1)
	}
	// The via stop distributes to 1/2 (the fixup above).
	if gradient.Stops[1].Position != 0.5 {
		t.Fatalf("via stop at %v, want 0.5", gradient.Stops[1].Position)
	}

	// to right (90deg): the horizontal center line, 200 device px.
	gradient, err = resolveCssGradient(CssGradient{
		Kind:      CssGradientLinear,
		Direction: GradientDirectionToRight,
		Stops:     []GradientStop{stopAt(gradientTestRed, 0), stopAt(gradientTestBlue, 1)},
	}, box, 2)
	if err != nil {
		t.Fatalf("resolveCssGradient: %v", err)
	}
	x0, y0, x1, y1 = gradient.Geometry[0], gradient.Geometry[1], gradient.Geometry[2], gradient.Geometry[3]
	if !approxEqual(y0, 90) || !approxEqual(y1, 90) || !approxEqual(x0, 20) || !approxEqual(x1, 220) {
		t.Fatalf("linear geometry (%v,%v)-(%v,%v), want (20,90)-(220,90)", x0, y0, x1, y1)
	}

	// The 45deg line over a 100x100 box: the length is |W.sin|+|H.cos|
	// = 100*sqrt(2) device px, the direction (sin45, -cos45).
	square := Bounds{Origin: Point{}, Size: Size{Width: 100, Height: 100}}
	gradient, err = resolveCssGradient(CssGradient{
		Kind:      CssGradientLinear,
		Direction: GradientDirectionAngle,
		Angle:     45,
		Stops:     []GradientStop{stopAt(gradientTestRed, 0), stopAt(gradientTestBlue, 1)},
	}, square, 1)
	if err != nil {
		t.Fatalf("resolveCssGradient: %v", err)
	}
	x0, y0, x1, y1 = gradient.Geometry[0], gradient.Geometry[1], gradient.Geometry[2], gradient.Geometry[3]
	half := float32(100 * math.Sqrt(2) / 2)
	diagonal := float32(math.Sqrt(0.5))
	if !approxEqual(x0, 50-half*diagonal) || !approxEqual(y0, 50+half*diagonal) {
		t.Fatalf("45deg start (%v,%v), want the box's bottom-left exit", x0, y0)
	}
	if !approxEqual(x1, 50+half*diagonal) || !approxEqual(y1, 50-half*diagonal) {
		t.Fatalf("45deg end (%v,%v), want the box's top-right exit", x1, y1)
	}
}

func TestResolveCssGradientContractsToStopSpan(t *testing.T) {
	// Stops at 20%..80%: the stops normalize to 0..1 and the line's
	// domain contracts to the same span (Chromium's
	// AdjustedGradientDomainForOffsetRange) — the 20% mark of the box
	// becomes the ramp's start.
	box := Bounds{Origin: Point{}, Size: Size{Width: 100, Height: 100}}
	gradient, err := resolveCssGradient(CssGradient{
		Kind:      CssGradientLinear,
		Direction: GradientDirectionToBottom,
		Stops:     []GradientStop{stopAt(gradientTestRed, 0.2), stopAt(gradientTestBlue, 0.8)},
	}, box, 1)
	if err != nil {
		t.Fatalf("resolveCssGradient: %v", err)
	}
	if gradient.Stops[0].Position != 0 || gradient.Stops[1].Position != 1 {
		t.Fatalf("normalized stops %v, want [0, 1]", gradient.Stops)
	}
	y0, y1 := gradient.Geometry[1], gradient.Geometry[3]
	if !approxEqual(y0, 20) || !approxEqual(y1, 80) {
		t.Fatalf("contracted line y [%v, %v], want [20, 80]", y0, y1)
	}
	if gradient.Tile != GradientTilePad {
		t.Fatalf("tile %v, want pad", gradient.Tile)
	}

	// The repeating form keeps the same span but wraps.
	gradient, err = resolveCssGradient(CssGradient{
		Kind:      CssGradientLinear,
		Direction: GradientDirectionToBottom,
		Repeating: true,
		Stops:     []GradientStop{stopAt(gradientTestRed, 0.2), stopAt(gradientTestBlue, 0.8)},
	}, box, 1)
	if err != nil {
		t.Fatalf("resolveCssGradient: %v", err)
	}
	if gradient.Tile != GradientTileRepeat {
		t.Fatalf("tile %v, want repeat", gradient.Tile)
	}
}

func TestResolveCssRadialGradientGeometry(t *testing.T) {
	// The default (ellipse farthest-corner at center) on a square box
	// is the circle through the corner: r = 50*sqrt(2), aspect 1.
	square := Bounds{Origin: Point{}, Size: Size{Width: 100, Height: 100}}
	background := RadialGradientBackground(RadialShapeEllipse, RadialExtentFarthestCorner,
		stopAt(gradientTestRed, 0), stopAt(gradientTestBlue, 1))
	gradient, err := resolveCssGradient(*background.CssGradient, square, 1)
	if err != nil {
		t.Fatalf("resolveCssGradient: %v", err)
	}
	if background.CssGradient.CenterX != 0.5 || background.CssGradient.CenterY != 0.5 {
		t.Fatalf("the constructor's center default (%v, %v), want (0.5, 0.5)", background.CssGradient.CenterX, background.CssGradient.CenterY)
	}
	cx, cy, r0, r1, aspect := gradient.Geometry[0], gradient.Geometry[1], gradient.Geometry[2], gradient.Geometry[3], gradient.Geometry[4]
	if cx != 50 || cy != 50 || r0 != 0 {
		t.Fatalf("radial center/r0 (%v,%v,%v), want (50,50,0)", cx, cy, r0)
	}
	if !approxEqual(r1, 50*math.Sqrt2) {
		t.Fatalf("farthest-corner radius %v, want 50*sqrt(2)", r1)
	}
	if aspect != 1 {
		t.Fatalf("square aspect %v, want 1", aspect)
	}

	// A 200x100 box: the closest-SIDE aspect (100x50 radii) scaled to
	// the farthest corner — EllipseRadius(corner, 2): a = sqrt(100² +
	// 50²·4) = 100·sqrt(2), b = a/2.
	wide := Bounds{Origin: Point{}, Size: Size{Width: 200, Height: 100}}
	gradient, err = resolveCssGradient(*RadialGradientBackground(RadialShapeEllipse, RadialExtentFarthestCorner,
		stopAt(gradientTestRed, 0), stopAt(gradientTestBlue, 1)).CssGradient, wide, 1)
	if err != nil {
		t.Fatalf("resolveCssGradient: %v", err)
	}
	r1, aspect = gradient.Geometry[3], gradient.Geometry[4]
	if !approxEqual(r1, 50*math.Sqrt2) {
		t.Fatalf("farthest-corner ellipse ry %v, want 50*sqrt(2)", r1)
	}
	if !approxEqual(aspect, 2) {
		t.Fatalf("farthest-corner ellipse aspect %v, want 2", aspect)
	}

	// closest-side on the wide box: rx=100, ry=50, aspect 2.
	gradient, err = resolveCssGradient(*RadialGradientBackground(RadialShapeEllipse, RadialExtentClosestSide,
		stopAt(gradientTestRed, 0), stopAt(gradientTestBlue, 1)).CssGradient, wide, 1)
	if err != nil {
		t.Fatalf("resolveCssGradient: %v", err)
	}
	r1, aspect = gradient.Geometry[3], gradient.Geometry[4]
	if r1 != 50 || aspect != 2 {
		t.Fatalf("closest-side radii (r1=%v, aspect=%v), want (50, 2)", r1, aspect)
	}

	// circle closest-side on the wide box: the circle to the closest
	// side, r = 50.
	gradient, err = resolveCssGradient(*RadialGradientBackground(RadialShapeCircle, RadialExtentClosestSide,
		stopAt(gradientTestRed, 0), stopAt(gradientTestBlue, 1)).CssGradient, wide, 1)
	if err != nil {
		t.Fatalf("resolveCssGradient: %v", err)
	}
	r1, aspect = gradient.Geometry[3], gradient.Geometry[4]
	if r1 != 50 || aspect != 1 {
		t.Fatalf("circle closest-side (r1=%v, aspect=%v), want (50, 1)", r1, aspect)
	}

	// A non-center position: at 25% 25% of the square, the farthest
	// side radii are (75, 75) so the aspect stays 1 and the ellipse
	// passes through (100,100): r1 = 75*sqrt(2).
	gradient, err = resolveCssGradient(CssGradient{
		Kind:    CssGradientRadial,
		CenterX: 0.25,
		CenterY: 0.25,
		Stops:   []GradientStop{stopAt(gradientTestRed, 0), stopAt(gradientTestBlue, 1)},
	}, square, 1)
	if err != nil {
		t.Fatalf("resolveCssGradient: %v", err)
	}
	if gradient.Geometry[0] != 25 || gradient.Geometry[1] != 25 {
		t.Fatalf("center (%v, %v), want (25, 25)", gradient.Geometry[0], gradient.Geometry[1])
	}
	if !approxEqual(gradient.Geometry[3], 75*math.Sqrt2) {
		t.Fatalf("farthest-corner radius from (25,25) is %v, want 75*sqrt(2)", gradient.Geometry[3])
	}
	if !approxEqual(gradient.Geometry[4], 1) {
		t.Fatalf("farthest-corner aspect %v, want 1 (the farthest side radii 75/75)", gradient.Geometry[4])
	}
}

func TestResolveCssConicGradientGeometry(t *testing.T) {
	box := Bounds{Origin: Point{X: 5, Y: 10}, Size: Size{Width: 100, Height: 50}}
	gradient, err := resolveCssGradient(*ConicGradientBackground(90,
		stopAt(gradientTestRed, 0), stopAt(gradientTestBlue, 1)).CssGradient, box, 2)
	if err != nil {
		t.Fatalf("resolveCssGradient: %v", err)
	}
	cx, cy := gradient.Geometry[0], gradient.Geometry[1]
	start, rotation := gradient.Geometry[2], gradient.Geometry[3]
	if cx != 110 || cy != 70 {
		t.Fatalf("conic center (%v, %v), want (110, 70)", cx, cy)
	}
	if !approxEqual(start, math.Pi/2) {
		t.Fatalf("conic start angle %v, want pi/2 (90deg from up)", start)
	}
	if !approxEqual(rotation, 2*math.Pi) {
		t.Fatalf("conic rotation %v, want 2pi", rotation)
	}

	// A 25%..75% stop span: the sweep starts 1/4 turn past the from
	// angle and one period is half a turn.
	gradient, err = resolveCssGradient(CssGradient{
		Kind:      CssGradientConic,
		FromAngle: 0,
		Stops:     []GradientStop{stopAt(gradientTestRed, 0.25), stopAt(gradientTestBlue, 0.75)},
	}, box, 1)
	if err != nil {
		t.Fatalf("resolveCssGradient: %v", err)
	}
	start, rotation = gradient.Geometry[2], gradient.Geometry[3]
	if !approxEqual(start, math.Pi/2) {
		t.Fatalf("spanned conic start %v, want pi/2", start)
	}
	if !approxEqual(rotation, math.Pi) {
		t.Fatalf("spanned conic rotation %v, want pi", rotation)
	}
}

func TestResolveCssGradientDegenerate(t *testing.T) {
	box := Bounds{Origin: Point{}, Size: Size{Width: 100, Height: 100}}
	if _, err := resolveCssGradient(CssGradient{Kind: CssGradientLinear, Direction: GradientDirectionToBottom}, box, 1); err == nil {
		t.Fatal("an empty stop list must fail")
	}
	// All stops coincident: the padded form keeps both end colors at
	// the same position (a hard edge), the repeating form is solid.
	gradient, err := resolveCssGradient(CssGradient{
		Kind:      CssGradientLinear,
		Direction: GradientDirectionToBottom,
		Stops:     []GradientStop{stopAt(gradientTestRed, 0.5), stopAt(gradientTestBlue, 0.5)},
	}, box, 1)
	if err != nil {
		t.Fatalf("resolveCssGradient: %v", err)
	}
	if gradient.Stops[0].Position != 0.5 || gradient.Stops[1].Position != 0.5 {
		t.Fatalf("coincident stops at %v, want both 0.5", gradient.Stops)
	}
	if gradient.Stops[0].Color != hslaToRgba(gradientTestRed) {
		t.Fatalf("coincident first color %v", gradient.Stops[0].Color)
	}
	gradient, err = resolveCssGradient(CssGradient{
		Kind:      CssGradientLinear,
		Direction: GradientDirectionToBottom,
		Repeating: true,
		Stops:     []GradientStop{stopAt(gradientTestRed, 0.5), stopAt(gradientTestBlue, 0.5)},
	}, box, 1)
	if err != nil {
		t.Fatalf("resolveCssGradient: %v", err)
	}
	blue := hslaToRgba(gradientTestBlue)
	if gradient.Stops[0].Color != blue || gradient.Stops[1].Color != blue {
		t.Fatalf("coincident repeating stops %v, want solid blue", gradient.Stops)
	}
}

func TestPaintQuadResolvesCssGradientThroughTable(t *testing.T) {
	scene, err := NewScene()
	if err != nil {
		t.Fatalf("NewScene: %v", err)
	}
	defer scene.Dispose()
	ctx := NewPaintContext(2, Bounds{Origin: Point{X: -1e6, Y: -1e6}, Size: Size{Width: 2e6, Height: 2e6}}, 1)
	background := LinearGradientToStopsBackground(GradientDirectionToRight,
		stopAt(gradientTestRed, 0), stopUnset(gradientTestGreen), stopAt(gradientTestBlue, 1))
	if err := scene.PaintQuad(PaintQuad{
		Bounds:     Bounds{Origin: Point{X: 10, Y: 10}, Size: Size{Width: 50, Height: 40}},
		Background: background,
	}, 0, ctx); err != nil {
		t.Fatalf("PaintQuad: %v", err)
	}
	if err := scene.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	meta, err := scene.Meta()
	if err != nil {
		t.Fatalf("Meta: %v", err)
	}
	if meta.GradientCount != 1 {
		t.Fatalf("gradient table count %d, want 1", meta.GradientCount)
	}
	if meta.QuadCount != 1 {
		t.Fatalf("quad count %d, want 1", meta.QuadCount)
	}
	quads, err := scene.Quads()
	if err != nil {
		t.Fatalf("Quads: %v", err)
	}
	if len(quads) != 1 {
		t.Fatalf("%d quads, want 1", len(quads))
	}
	if quads[0].Background.Kind != BackgroundKindGradientTable || quads[0].Background.GradientTable != 0 {
		t.Fatalf("quad background %v, want the table reference 0", quads[0].Background)
	}
}

func TestPaintQuadRejectsUnresolvedCssGradientAtTheKernel(t *testing.T) {
	scene, err := NewScene()
	if err != nil {
		t.Fatalf("NewScene: %v", err)
	}
	defer scene.Dispose()
	if err := scene.InsertQuad(Quad{Bounds: Bounds{Origin: Point{}, Size: Size{Width: 10, Height: 10}}, Background: LinearGradientToStopsBackground(GradientDirectionToRight, stopAt(gradientTestRed, 0), stopAt(gradientTestBlue, 1))}); err == nil {
		t.Fatal("InsertQuad must reject the unresolved CSS-gradient arm (resolve through PaintQuad)")
	}
}

func TestCssGradientBackgroundOpacityAndTransparency(t *testing.T) {
	background := LinearGradientToStopsBackground(GradientDirectionToRight,
		stopAt(gradientTestRed, 0), stopAt(Hsla{A: 0}, 1))
	if background.IsTransparent() {
		t.Fatal("a gradient with a non-transparent stop is not transparent")
	}
	half := background.Opacity(0.5)
	if half.CssGradient.Stops[0].Color.A != 0.5 {
		t.Fatalf("opacity applied to stop alpha: %v", half.CssGradient.Stops[0].Color.A)
	}
	transparent := LinearGradientToStopsBackground(GradientDirectionToRight,
		stopAt(Hsla{A: 0}, 0), stopAt(Hsla{A: 0}, 1))
	if !transparent.IsTransparent() {
		t.Fatal("every stop at alpha 0 is transparent")
	}
	// Repeating wraps the same arm.
	if !background.Repeating().CssGradient.Repeating {
		t.Fatal("Repeating sets the repeating tile")
	}
	// The two-stop R15 arm keeps its own behavior untouched.
	twoStop := LinearGradientBackground(90, stopAt(gradientTestRed, 0), stopAt(gradientTestBlue, 1))
	if twoStop.Kind != BackgroundKindLinearGradient {
		t.Fatalf("the two-stop arm kind %v", twoStop.Kind)
	}
	if twoStop.Repeating().Kind != BackgroundKindLinearGradient {
		t.Fatal("Repeating leaves non-CSS arms alone")
	}
}

func approxEqual(a, b float32) bool {
	if b == 0 {
		return math.Abs(float64(a)) < 1e-4
	}
	return math.Abs(float64(a-b))/math.Abs(float64(b)) < 1e-4
}
