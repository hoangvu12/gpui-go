package gpui_test

import (
	"testing"

	gpui "github.com/hoangvu12/gpui-go/gpui"
)

// browser-core ticket 57's group scopes: the mask-image, clip-path,
// mix-blend-mode and isolation emissions at the scene seam, and the
// clip-path shapes' point-in-shape hit test. The helpers (drawStyled,
// quadsOf, dev, painted, red, blue, green) come from styled_test.go.

func TestMaskImageEmitsGradientMaskGroup(t *testing.T) {
	// CSS mask-image: linear-gradient(to bottom, black, transparent) —
	// the element's whole rendering composites dst_in against the
	// gradient's alpha ramp.
	mask := gpui.Div().Flex().W(gpui.Px(100)).H(gpui.Px(80)).Bg(red).
		MaskImage(gpui.GradientMaskImage(gpui.CssGradient{
			Kind:      gpui.CssGradientLinear,
			Direction: gpui.GradientDirectionToBottom,
			Stops: []gpui.GradientStop{
				{Color: gpui.Hsla{A: 1}},
				{Color: gpui.Hsla{A: 0}, Position: 1},
			},
		}))
	_, scene := drawStyled(t, gpui.Div().Flex().P(gpui.Px(20)).Child(mask).IntoElement())

	groups, err := scene.EffectGroups()
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 {
		t.Fatalf("effect groups = %d, want a matched pair: %+v", len(groups), groups)
	}
	start, end := groups[0], groups[1]
	if !start.IsStart || end.IsStart {
		t.Fatalf("groups = %+v, want start then end", groups)
	}
	if !start.HasMask || start.MaskKind != gpui.MaskKindGradient {
		t.Errorf("start group mask = kind %v, want the gradient mask", start.MaskKind)
	}
	if start.MaskMode != gpui.MaskModeAlpha {
		t.Errorf("start group mask mode = %v, want alpha", start.MaskMode)
	}
	if !start.ClipToBounds {
		t.Error("the mask group clips to the mask's bounding box (the border box)")
	}
	// The masked content is the element's whole rendering (the
	// background quad lands INSIDE the group's bracket).
	if start.Bounds != dev(20, 20, 100, 80) {
		t.Errorf("group bounds = %v, want the element's border box", start.Bounds)
	}
	// One gradient table entry (the mask's gradient), one quad.
	meta, err := scene.Meta()
	if err != nil {
		t.Fatal(err)
	}
	if meta.GradientCount != 1 {
		t.Errorf("gradient table count = %d, want 1 (the mask's gradient)", meta.GradientCount)
	}
	if meta.QuadCount != 1 {
		t.Errorf("quad count = %d, want 1 (the element's background)", meta.QuadCount)
	}
	quads := quadsOf(t, scene)
	if len(quads) != 1 || quads[0].Order == 0 {
		t.Fatalf("the background quad should exist inside the mask group")
	}
}

func TestClipPathEmitsCoverageGroup(t *testing.T) {
	// CSS clip-path: circle(36px): the shape tessellates into a scene
	// path record (a transparent fill) plus a coverage entry, and the
	// element brackets in a coverage-mask effect group.
	clipped := gpui.Div().Flex().W(gpui.Px(100)).H(gpui.Px(80)).Bg(red).
		ClipPath(gpui.ClipPathCircle(gpui.ClipPx(36)))
	_, scene := drawStyled(t, gpui.Div().Flex().P(gpui.Px(20)).Child(clipped).IntoElement())

	groups, err := scene.EffectGroups()
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 {
		t.Fatalf("effect groups = %d, want a matched pair: %+v", len(groups), groups)
	}
	start := groups[0]
	if !start.HasMask || start.MaskKind != gpui.MaskKindCoverage {
		t.Errorf("start group mask = kind %v, want the coverage mask", start.MaskKind)
	}
	if !start.ClipToBounds {
		t.Error("the clip-path group clips to the reference box")
	}
	if start.Bounds != dev(20, 20, 100, 80) {
		t.Errorf("group bounds = %v, want the border box (the reference box)", start.Bounds)
	}
	meta, err := scene.Meta()
	if err != nil {
		t.Fatal(err)
	}
	if meta.CoverageCount != 1 {
		t.Errorf("coverage count = %d, want 1", meta.CoverageCount)
	}
	if meta.PathCount != 1 {
		t.Errorf("path count = %d, want the coverage path record (transparent fill)", meta.PathCount)
	}
}

func TestMixBlendModeEmitsBlendGroup(t *testing.T) {
	// CSS mix-blend-mode: multiply — one effect group carrying the
	// blend; a translucent blended element carries the opacity on the
	// SAME group (Chromium's effect node holds both).
	blended := gpui.Div().Flex().W(gpui.Px(100)).H(gpui.Px(80)).Bg(red).MixBlendMode(gpui.BlendMultiply)
	_, scene := drawStyled(t, gpui.Div().Flex().P(gpui.Px(20)).Child(blended).IntoElement())
	groups, err := scene.EffectGroups()
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 {
		t.Fatalf("effect groups = %d, want a matched pair: %+v", len(groups), groups)
	}
	if !groups[0].HasBlend || groups[0].BlendMode != gpui.BlendMultiply {
		t.Errorf("start group = %+v, want a multiply blend group", groups[0])
	}
	if groups[0].HasOpacity {
		t.Error("an opaque blended element carries no opacity flag")
	}

	translucent := gpui.Div().Flex().W(gpui.Px(100)).H(gpui.Px(80)).Bg(red).
		Opacity(0.5).MixBlendMode(gpui.BlendPlusLighter)
	_, scene = drawStyled(t, gpui.Div().Flex().P(gpui.Px(20)).Child(translucent).IntoElement())
	groups, err = scene.EffectGroups()
	if err != nil {
		t.Fatal(err)
	}
	// ONE pair: the blend group carries the opacity (not two nested
	// groups).
	if len(groups) != 2 {
		t.Fatalf("effect groups = %d, want ONE pair (the blend group carries the opacity): %+v", len(groups), groups)
	}
	if !groups[0].HasBlend || groups[0].BlendMode != gpui.BlendPlusLighter {
		t.Errorf("start group = %+v, want a plus-lighter blend group", groups[0])
	}
	if !groups[0].HasOpacity || groups[0].Opacity != 0.5 {
		t.Errorf("start group opacity = (%v, %v), want the element's 0.5 on the blend group", groups[0].HasOpacity, groups[0].Opacity)
	}
}

func TestIsolationEmitsEmptyGroup(t *testing.T) {
	isolated := gpui.Div().Flex().W(gpui.Px(100)).H(gpui.Px(80)).Bg(red).Isolate()
	_, scene := drawStyled(t, gpui.Div().Flex().P(gpui.Px(20)).Child(isolated).IntoElement())
	groups, err := scene.EffectGroups()
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 {
		t.Fatalf("effect groups = %d, want the isolation pair: %+v", len(groups), groups)
	}
	start := groups[0]
	if start.HasBlend || start.HasOpacity || start.HasMask || start.HasTransform || start.RoundedClipOnly {
		t.Errorf("the isolation group carries feature flags: %+v", start)
	}
}

func TestClipPathShapesContain(t *testing.T) {
	size := gpui.Size{Width: 100, Height: 80}

	// circle(40px) at the center: the center is inside, the corner
	// outside.
	circle := gpui.ClipPathCircle(gpui.ClipPx(40))
	if !circle.Contains(gpui.Point{X: 50, Y: 40}, size) {
		t.Error("the circle's center should be inside")
	}
	if circle.Contains(gpui.Point{X: 2, Y: 2}, size) {
		t.Error("the box's corner should be outside the circle")
	}
	// The analytic edge: exactly on the radius.
	if !circle.Contains(gpui.Point{X: 90, Y: 40}, size) {
		t.Error("a point on the circle's right edge should be inside")
	}

	// ellipse(30% 50%) at 25% 25%.
	ellipse := gpui.ClipPathEllipseAt(gpui.ClipFraction(0.3), gpui.ClipFraction(0.5),
		gpui.ClipFraction(0.25), gpui.ClipFraction(0.25))
	if !ellipse.Contains(gpui.Point{X: 25, Y: 20}, size) {
		t.Error("the ellipse's center should be inside")
	}
	if ellipse.Contains(gpui.Point{X: 60, Y: 60}, size) {
		t.Error("a point outside the ellipse should miss")
	}

	// inset(10px 20px 10px 20px).
	inset := gpui.ClipPathInset(gpui.ClipPx(10), gpui.ClipPx(20), gpui.ClipPx(10), gpui.ClipPx(20))
	if !inset.Contains(gpui.Point{X: 50, Y: 40}, size) {
		t.Error("the inset's middle should be inside")
	}
	if inset.Contains(gpui.Point{X: 5, Y: 40}, size) {
		t.Error("a point in the left inset band should miss")
	}

	// polygon(50% 0, 100% 100%, 0 100%) — the bottom triangle.
	polygon := gpui.ClipPathPolygon(
		gpui.ClipPointFraction(0.5, 0),
		gpui.ClipPointFraction(1, 1),
		gpui.ClipPointFraction(0, 1),
	)
	if !polygon.Contains(gpui.Point{X: 50, Y: 70}, size) {
		t.Error("the triangle's lower middle should be inside")
	}
	if polygon.Contains(gpui.Point{X: 10, Y: 5}, size) {
		t.Error("a point left of the triangle's upper edge should miss")
	}
	if polygon.Contains(gpui.Point{X: 2, Y: 2}, size) {
		t.Error("the top-left corner should be outside the triangle")
	}
}
