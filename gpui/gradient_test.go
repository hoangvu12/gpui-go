package gpui

import (
	"errors"
	"math"
	"strings"
	"testing"
)

// The gradient Background's unit tests (browser-core ticket 40): the
// public constructors, the pinned Background::opacity and
// ::is_transparent semantics over the gradient arm, the refinement
// equality, and the scene round trip through the native kernel,
// including the CSS corner-keyword resolution (the magic-corner rule).

func TestGradientBackgroundOpacityScalesBothStops(t *testing.T) {
	gradient := LinearGradientBackground(90,
		GradientStop{Color: Rgb(0xef4444), Position: 0},
		GradientStop{Color: Rgb(0x3b82f6).Alpha(0.5), Position: 1})
	faded := gradient.Opacity(0.5)
	if faded.Kind != BackgroundKindLinearGradient {
		t.Fatalf("Opacity kept the gradient arm: %+v", faded)
	}
	from, to := faded.Gradient.From.Color, faded.Gradient.To.Color
	if from.A != 0.5 || to.A != 0.25 {
		t.Errorf("the stops' alphas did not scale: from a=%v to a=%v (the pinned Background::opacity multiplies each LinearColorStop's alpha)", from.A, to.A)
	}
	// The hue, saturation and lightness are untouched (alpha only, the
	// pinned LinearColorStop::opacity).
	if from.H != Rgb(0xef4444).H || from.S != Rgb(0xef4444).S || from.L != Rgb(0xef4444).L {
		t.Errorf("from's HSL changed under Opacity: %+v", from)
	}
	if gradient.Gradient.From.Color.A != 1 {
		t.Errorf("Opacity mutated the receiver: %+v", gradient)
	}
}

func TestGradientBackgroundIsTransparentOnlyWhenBothStopsAre(t *testing.T) {
	cases := []struct {
		name string
		bg   Background
		want bool
	}{
		{"both stops transparent", LinearGradientBackground(0, GradientStop{Position: 0}, GradientStop{Position: 1}), true},
		{"opaque from", LinearGradientBackground(0, GradientStop{Color: Hsla{A: 1}, Position: 0}, GradientStop{Position: 1}), false},
		{"opaque to", LinearGradientBackground(0, GradientStop{Position: 0}, GradientStop{Color: Hsla{A: 0.5}, Position: 1}), false},
		{"solid transparent", SolidBackground(Hsla{}), true},
		{"solid opaque", SolidBackground(Hsla{A: 1}), false},
	}
	for _, tc := range cases {
		if got := tc.bg.IsTransparent(); got != tc.want {
			t.Errorf("%s: IsTransparent() = %v, want %v (the pinned Background::is_transparent)", tc.name, got, tc.want)
		}
	}
}

func TestEqualBackgroundComparesGradients(t *testing.T) {
	stop := func(hex uint32, at float32) GradientStop {
		return GradientStop{Color: Rgb(hex), Position: at}
	}
	same := func() (a, b Background) {
		return LinearGradientBackground(45, stop(0xef4444, 0), stop(0x3b82f6, 1)),
			LinearGradientBackground(45, stop(0xef4444, 0), stop(0x3b82f6, 1))
	}
	a, b := same()
	if !equalBackground(a, b) {
		t.Error("two equal gradients are not equal")
	}
	different := []Background{
		LinearGradientBackground(46, stop(0xef4444, 0), stop(0x3b82f6, 1)),
		LinearGradientBackground(45, stop(0xef4445, 0), stop(0x3b82f6, 1)),
		LinearGradientBackground(45, stop(0xef4444, 0.25), stop(0x3b82f6, 1)),
		LinearGradientToBackground(GradientDirectionToTopRight, stop(0xef4444, 0), stop(0x3b82f6, 1)),
		SolidBackground(Rgb(0xef4444)),
	}
	for _, other := range different {
		if equalBackground(a, other) {
			t.Errorf("gradient %+v equals %+v", a, other)
		}
	}
}

// gradientScene opens a scene disposed at cleanup.
func gradientScene(t *testing.T) *Scene {
	t.Helper()
	scene, err := NewScene()
	if err != nil {
		t.Fatalf("NewScene: %v", err)
	}
	t.Cleanup(func() { _ = scene.Dispose() })
	return scene
}

func TestGradientBackgroundRoundTripsThroughTheScene(t *testing.T) {
	scene := gradientScene(t)
	gradient := LinearGradientBackground(135,
		GradientStop{Color: Rgb(0xef4444), Position: 0.25},
		GradientStop{Color: Rgb(0x3b82f6), Position: 0.75})
	if err := scene.PaintQuad(PaintQuad{
		Bounds:     Bounds{Origin: Point{X: 10, Y: 10}, Size: Size{Width: 160, Height: 100}},
		Background: gradient,
	}, 0, NewPaintContext(2, Bounds{Size: Size{Width: 1000, Height: 1000}}, 1)); err != nil {
		t.Fatalf("PaintQuad(gradient): %v", err)
	}
	if err := scene.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	quads, err := scene.Quads()
	if err != nil {
		t.Fatalf("Quads: %v", err)
	}
	if len(quads) != 1 {
		t.Fatalf("the scene holds %d quads, want 1", len(quads))
	}
	got := quads[0].Background
	if got.Kind != BackgroundKindLinearGradient {
		t.Fatalf("the dumped background is %+v, want the gradient arm", got)
	}
	if got.Gradient.Angle != 135 || got.Gradient.Direction != GradientDirectionAngle {
		t.Errorf("the angle did not round-trip: %+v", got.Gradient)
	}
	want := gradient.Gradient
	if !equalGradientStop(got.Gradient.From, want.From) || !equalGradientStop(got.Gradient.To, want.To) {
		t.Errorf("the stops did not round-trip:\n got %+v/%+v\nwant %+v/%+v",
			got.Gradient.From, got.Gradient.To, want.From, want.To)
	}
}

func TestGradientEqualStopsAreRejectedByTheKernel(t *testing.T) {
	scene := gradientScene(t)
	err := scene.PaintQuad(PaintQuad{
		Bounds: Bounds{Size: Size{Width: 10, Height: 10}},
		Background: LinearGradientBackground(90,
			GradientStop{Color: Red(), Position: 0.5},
			GradientStop{Color: Rgb(0x3b82f6), Position: 0.5}),
	}, 0, NewPaintContext(1, Bounds{}, 1))
	if err == nil || !strings.Contains(err.Error(), "scene insert quad") {
		t.Fatalf("a hard stop (equal positions) must fail the insert with the kernel's bad-value error, got %v", err)
	}
}

func TestGradientCornerDirectionsResolveAgainstTheBounds(t *testing.T) {
	// The magic-corner rule: for a box of w x h, `to top right` points
	// at the bearing angle atan2(h, w) — the 50% line passes through
	// the other two corners (Chromium css_gradient_value.cc:1425-1431:
	// bearing = 90 - atan2(width, height)). On a 160x100 box that is
	// 32.005 degrees, not 45.
	cases := []struct {
		direction GradientDirection
		want      float32
	}{
		{GradientDirectionToTopRight, 32.00538},
		{GradientDirectionToBottomRight, 180 - 32.00538},
		{GradientDirectionToBottomLeft, 180 + 32.00538},
		{GradientDirectionToTopLeft, -32.00538},
		{GradientDirectionToTop, 0},
		{GradientDirectionToRight, 90},
		{GradientDirectionToBottom, 180},
		{GradientDirectionToLeft, 270},
	}
	const w, h = float32(160), float32(100)
	for _, tc := range cases {
		g := &LinearGradient{Direction: tc.direction, From: GradientStop{Position: 0}, To: GradientStop{Position: 1}}
		got := resolveGradientAngle(g, w, h)
		if math.Abs(float64(got-tc.want)) > 1e-4 {
			t.Errorf("direction %d on 160x100 resolved to %v degrees, want %v", tc.direction, got, tc.want)
		}
	}
	// A square box resolves every corner to its diagonal.
	g := &LinearGradient{Direction: GradientDirectionToTopRight}
	if got := resolveGradientAngle(g, 100, 100); math.Abs(float64(got-45)) > 1e-4 {
		t.Errorf("to top right on a square resolved to %v, want 45", got)
	}
}

func TestGradientCornerResolvesInSceneQuad(t *testing.T) {
	scene := gradientScene(t)
	background := LinearGradientToBackground(GradientDirectionToTopRight,
		GradientStop{Color: Red(), Position: 0},
		GradientStop{Color: Rgb(0x3b82f6), Position: 1})
	// Device bounds 320x200 (a 160x100 logical box at scale 2): the
	// aspect ratio matches, so the magic corner is the same angle.
	err := scene.InsertQuad(Quad{
		Bounds:      Bounds{Size: Size{Width: 320, Height: 200}},
		ContentMask: Bounds{Size: Size{Width: 1000, Height: 1000}},
		Background:  background,
	})
	if err != nil {
		t.Fatalf("InsertQuad: %v", err)
	}
	if err := scene.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	quads, err := scene.Quads()
	if err != nil || len(quads) != 1 {
		t.Fatalf("Quads: %v (%d)", err, len(quads))
	}
	got := quads[0].Background.Gradient
	if got.Direction != GradientDirectionAngle {
		t.Fatalf("the corner did not resolve to an angle: %+v", got)
	}
	if math.Abs(float64(got.Angle-32.00538)) > 1e-4 {
		t.Errorf("the corner resolved to %v degrees, want the 32.005 magic corner of 320x200", got.Angle)
	}
}

// TestGradientErrorsAreNamed checks the two sentinels the class mapper
// wraps (errors.Is through CheckClasses).
func TestGradientErrorsAreNamed(t *testing.T) {
	if !strings.Contains(ErrGradientViaUnsupported.Error(), "ticket 52") {
		t.Errorf("ErrGradientViaUnsupported does not point at ticket 52: %v", ErrGradientViaUnsupported)
	}
	if !strings.Contains(ErrGradientInterpolationUnsupported.Error(), "ticket 52") {
		t.Errorf("ErrGradientInterpolationUnsupported does not point at ticket 52: %v", ErrGradientInterpolationUnsupported)
	}
	err := CheckClasses("bg-linear-to-r via-orange-400")
	if !errors.Is(err, ErrGradientViaUnsupported) {
		t.Errorf("CheckClasses(via) = %v; want errors.Is ErrGradientViaUnsupported", err)
	}
	err = CheckClasses("bg-linear-to-r/oklab from-red-500 to-blue-500")
	if !errors.Is(err, ErrGradientInterpolationUnsupported) {
		t.Errorf("CheckClasses(/oklab) = %v; want errors.Is ErrGradientInterpolationUnsupported", err)
	}
}

// TestGradientBgAcceptsAGradientBackground checks the styled surface:
// Bg takes the gradient, the refinement carries it, and a later solid
// replaces it.
func TestGradientBgAcceptsAGradientBackground(t *testing.T) {
	gradient := LinearGradientBackground(90,
		GradientStop{Color: Rgb(0xef4444), Position: 0},
		GradientStop{Color: Rgb(0x3b82f6), Position: 1})
	div := Div().Flex().Bg(gradient)
	style := div.StyleMut()
	background, ok := style.Background.Get()
	if !ok || !equalBackground(background, gradient) {
		t.Fatalf("Bg did not store the gradient: %+v", background)
	}
	// A solid Bg replaces it (one background, no CSS background-color
	// under background-image layer).
	div = Div().Flex().Bg(gradient).Bg(Rgb(0xffffff))
	background, _ = div.StyleMut().Background.Get()
	if background.Kind != BackgroundKindSolid {
		t.Fatalf("a later solid Bg did not replace the gradient: %+v", background)
	}
}
