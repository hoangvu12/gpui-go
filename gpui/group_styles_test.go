package gpui_test

import (
	"testing"

	gpui "github.com/hoangvu12/gpui-go/gpui"
)

// browser-core ticket 55 (D9 + the rounded overflow clip + the border
// order): the style-level group emission, pinned at the scene seam.
// The border-order and opacity-group record tests live in
// styled_test.go and transition_test.go; these pin the rounded clip:
// the mask's radii, the rounded_clip_only effect group the paint path
// brackets an overflowing rounded box's children in, the nested case,
// and the backdrop-filter opacity exception. The helpers (drawStyled,
// quadsOf, corners, dev, painted, red, blue, green) come from
// styled_test.go.

// TestOverflowMaskCarriesInnerBorderRadii pins Style.OverflowMask's
// rounded clip (rev 16, ticket 55): the rect is the padding box and the
// radii are the INNER border radii - the outer radii shrunk by the
// border widths per corner, clamped at zero (Chromium's
// ContouredInnerBorder: the border rrect inset by the border widths,
// contoured_border_geometry.cc:209-219, applied through
// UpdateInnerBorderRadiusClip, paint_property_tree_builder.cc:3257-
// 3293). One axis visible: the region is NOT rounded (css-overflow
// §corner-clipping; NeedsInnerBorderRadiusClip,
// paint_property_tree_builder.cc:3108-3112). Overflow visible on both
// axes: no mask at all (the pin's rule).
func TestOverflowMaskCarriesInnerBorderRadii(t *testing.T) {
	style := func() *gpui.Style {
		s := gpui.DefaultStyle()
		s.OverflowX = gpui.OverflowHidden
		s.OverflowY = gpui.OverflowHidden
		s.CornerRadii = gpui.AbsoluteLengthCorners{
			TopLeft:     gpui.Px(12),
			TopRight:    gpui.Px(12),
			BottomRight: gpui.Px(12),
			BottomLeft:  gpui.Px(12),
		}
		return &s
	}
	bounds := gpui.Bounds{Origin: gpui.Point{X: 10, Y: 10}, Size: gpui.Size{Width: 100, Height: 80}}

	// Borderless: the mask is the border box with the element radii.
	mask := style().OverflowMask(bounds, 16)
	if mask == nil {
		t.Fatal("overflow hidden x/y: mask = nil")
	}
	if mask.Bounds != (gpui.Bounds{Origin: gpui.Point{X: 10, Y: 10}, Size: gpui.Size{Width: 100, Height: 80}}) {
		t.Errorf("borderless mask rect = %v", mask.Bounds)
	}
	if got, want := mask.CornerRadii, (gpui.Corners{TopLeft: 12, TopRight: 12, BottomRight: 12, BottomLeft: 12}); got != want {
		t.Errorf("borderless mask radii = %v, want %v", got, want)
	}
	if !mask.IsRounded() {
		t.Error("borderless mask should be rounded")
	}

	// A 4px border insets the rect by 4 and shrinks every corner by 4
	// (max of the adjacent border widths, the inset shadow's rule).
	bordered := style()
	bordered.BorderWidths = gpui.AbsoluteLengthEdges{
		Top:    gpui.Px(4),
		Right:  gpui.Px(4),
		Bottom: gpui.Px(4),
		Left:   gpui.Px(4),
	}
	mask = bordered.OverflowMask(bounds, 16)
	if mask.Bounds != (gpui.Bounds{Origin: gpui.Point{X: 14, Y: 14}, Size: gpui.Size{Width: 92, Height: 72}}) {
		t.Errorf("bordered mask rect = %v, want the padding box", mask.Bounds)
	}
	if got, want := mask.CornerRadii, (gpui.Corners{TopLeft: 8, TopRight: 8, BottomRight: 8, BottomLeft: 8}); got != want {
		t.Errorf("bordered mask radii = %v, want the inner border radii %v", got, want)
	}

	// The smoothing rides along.
	smoothed := style()
	smoothing := float32(0.6)
	smoothed.CornerSmoothing = &smoothing
	if mask := smoothed.OverflowMask(bounds, 16); mask.CornerSmoothing != 0.6 {
		t.Errorf("mask smoothing = %v, want 0.6", mask.CornerSmoothing)
	}

	// One axis visible: the region is not rounded (the pinned axis
	// selection, and css-overflow's "not rounded" rule).
	xVisible := style()
	xVisible.OverflowX = gpui.OverflowVisible
	if mask := xVisible.OverflowMask(bounds, 16); mask.IsRounded() {
		t.Errorf("x-visible mask radii = %v, want none (css-overflow §corner-clipping)", mask.CornerRadii)
	}

	// Both axes visible: no mask.
	visible := style()
	visible.OverflowX = gpui.OverflowVisible
	visible.OverflowY = gpui.OverflowVisible
	if mask := visible.OverflowMask(bounds, 16); mask != nil {
		t.Errorf("visible overflow mask = %v, want nil", mask)
	}

	// Radii smaller than the border: the inner radii clamp at zero,
	// never negative.
	fat := style()
	fat.CornerRadii = gpui.AbsoluteLengthCorners{
		TopLeft:     gpui.Px(2),
		TopRight:    gpui.Px(2),
		BottomRight: gpui.Px(2),
		BottomLeft:  gpui.Px(2),
	}
	fat.BorderWidths = gpui.AbsoluteLengthEdges{
		Top:    gpui.Px(8),
		Right:  gpui.Px(8),
		Bottom: gpui.Px(8),
		Left:   gpui.Px(8),
	}
	if mask := fat.OverflowMask(bounds, 16); mask.CornerRadii != (gpui.Corners{}) {
		t.Errorf("oversized radii = %v, want clamped to zero", mask.CornerRadii)
	}
}

// TestRoundedOverflowClipEmitsEffectGroup: a rounded box with hidden
// overflow brackets its children in a rounded_clip_only effect group -
// the group's bounds are the padding box (the mask rect), its radii
// the inner border radii, its content mask the intersected rect - and
// the children keep the plain rectangular inline mask (the composite
// is the single rounded clip; carrying the radii on the records too
// would double the corner coverage). A rectangular overflow (no radii)
// emits no group at all: the pre-R16 scene exactly.
func TestRoundedOverflowClipEmitsEffectGroup(t *testing.T) {
	card := gpui.Div().Flex().W(gpui.Px(100)).H(gpui.Px(80)).Rounded(gpui.Px(16)).
		Border2().BorderColor(blue).Bg(red).OverflowHidden().
		Child(gpui.Div().Flex().Size(gpui.Px(100)).H(gpui.Px(80)).Bg(green).FlexShrink(0))
	_, scene := drawStyled(t, gpui.Div().Flex().P(gpui.Px(20)).Child(card).IntoElement())

	groups, err := scene.EffectGroups()
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 {
		t.Fatalf("effect groups = %d, want a matched pair: %+v", len(groups), groups)
	}
	start, end := groups[0], groups[1]
	if !start.IsStart || end.IsStart {
		t.Fatalf("groups = %+v %+v, want start then end", start, end)
	}
	if !start.ClipToBounds || !start.RoundedClipOnly || start.HasOpacity || start.HasTransform {
		t.Errorf("start group flags = %+v, want a rounded-clip-only group", start)
	}
	// The padding box (the 100x80 box at (20,20), 2px border, test scale
	// 2): the device bounds (44,44,192x152) with the inner radii
	// 16-2=14 logical -> 28 device.
	if start.Bounds != dev(22, 22, 96, 76) {
		t.Errorf("group bounds = %v, want the padding box", start.Bounds)
	}
	if got, want := start.CornerRadii, corners(14); got != want {
		t.Errorf("group radii = %v, want the inner border radii %v", got, want)
	}
	// The children keep the rectangular inline mask (the intersection
	// with the window - the unclipped test window).
	quads := quadsOf(t, scene)
	var child gpui.Quad
	for _, q := range quads {
		if q.Background == painted(green) {
			child = q
		}
	}
	if child.Order == 0 {
		t.Fatal("the child quad was not found")
	}
	if child.MaskRadii != (gpui.Corners{}) {
		t.Errorf("child inline mask radii = %v, want none (the group is the single rounded clip)", child.MaskRadii)
	}
}

// TestRectOverflowClipEmitsNoGroup: a plain overflow: hidden box (no
// corner radii) emits no effect group - the pre-R16 scene exactly (the
// rect mask clips every primitive inline).
func TestRectOverflowClipEmitsNoGroup(t *testing.T) {
	box := gpui.Div().Flex().W(gpui.Px(100)).H(gpui.Px(80)).Bg(red).OverflowHidden().
		Child(gpui.Div().Flex().Size(gpui.Px(120)).H(gpui.Px(90)).Bg(green).FlexShrink(0))
	_, scene := drawStyled(t, gpui.Div().Flex().P(gpui.Px(20)).Child(box).IntoElement())
	groups, err := scene.EffectGroups()
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 0 {
		t.Fatalf("effect groups = %d, want none for a rectangular clip: %+v", len(groups), groups)
	}
}

// TestNestedRoundedClipsNestGroups: a rounded clip inside a rounded
// clip opens a group inside the outer group's bracket (the composite of
// the outer clips the inner group's composite: every ancestor's rrect
// applies, the design's "ancestors' rounded clips become groups").
func TestNestedRoundedClipsNestGroups(t *testing.T) {
	outer := gpui.Div().Flex().W(gpui.Px(120)).H(gpui.Px(100)).Rounded(gpui.Px(20)).OverflowHidden().
		Child(gpui.Div().Flex().Size(gpui.Px(60)).H(gpui.Px(50)).Rounded(gpui.Px(10)).OverflowHidden().
			Child(gpui.Div().Flex().Size(gpui.Px(90)).H(gpui.Px(70)).Bg(green).FlexShrink(0)))
	_, scene := drawStyled(t, gpui.Div().Flex().P(gpui.Px(20)).Child(outer).IntoElement())
	groups, err := scene.EffectGroups()
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 4 {
		t.Fatalf("effect groups = %d, want two matched pairs: %+v", len(groups), groups)
	}
	// Start-outer, start-inner, end-inner, end-outer (the bracket order).
	if !(groups[0].IsStart && groups[1].IsStart && !groups[2].IsStart && !groups[3].IsStart) {
		t.Fatalf("group order = %+v, want outer start, inner start, inner end, outer end", groups)
	}
	outerStart, innerStart := groups[0], groups[1]
	if outerStart.Bounds != dev(20, 20, 120, 100) || outerStart.CornerRadii != corners(20) {
		t.Errorf("outer group = bounds %v radii %v, want the outer padding box and radii", outerStart.Bounds, outerStart.CornerRadii)
	}
	// The inner clip sits inside the outer's first child at (20,20): its
	// group bounds are its own padding box.
	if innerStart.Bounds != dev(20, 20, 60, 50) || innerStart.CornerRadii != corners(10) {
		t.Errorf("inner group = bounds %v radii %v, want the inner padding box and radii", innerStart.Bounds, innerStart.CornerRadii)
	}
}

// TestOpacityGroupBracketsOverlappingChildren: a translucent element
// whose subtree draws more than one primitive brackets the whole style
// paint (the box AND the children) in one opacity group, and the
// children's records keep their un-multiplied colours - the group
// composite applies the opacity once (D9). The single-primitive fold
// is pinned by TestDivPaintRadiiClampAndOpacity and the pixels by the
// parity fixtures.
func TestOpacityGroupBracketsOverlappingChildren(t *testing.T) {
	card := gpui.Div().Flex().W(gpui.Px(100)).H(gpui.Px(80)).Opacity(0.5).Bg(red).
		Child(gpui.Div().Flex().W(gpui.Px(60)).H(gpui.Px(80)).Bg(green).FlexShrink(0)).
		Child(gpui.Div().Flex().W(gpui.Px(60)).H(gpui.Px(80)).Bg(blue).FlexShrink(0))
	_, scene := drawStyled(t, gpui.Div().Flex().P(gpui.Px(20)).Child(card).IntoElement())

	groups, err := scene.EffectGroups()
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 {
		t.Fatalf("effect groups = %d, want one matched pair: %+v", len(groups), groups)
	}
	start := groups[0]
	if !start.HasOpacity || start.Opacity != 0.5 || start.ClipToBounds || start.RoundedClipOnly {
		t.Errorf("group = %+v, want a pure opacity group at 0.5", start)
	}
	// Every record inside the bracket keeps the un-multiplied colour.
	quads := quadsOf(t, scene)
	if len(quads) != 3 {
		t.Fatalf("quads = %d, want the box and two children", len(quads))
	}
	for _, q := range quads {
		if q.Background.IsTransparent() || q.Background.Solid.A < 1-1e-6 {
			t.Errorf("quad background = %+v, want the un-multiplied colour (the group owns the opacity)", q.Background)
		}
	}
}

// TestBackdropFilteredElementKeepsOpacityMultiply: an element with a
// backdrop-filter chain and a non-identity opacity keeps the
// per-primitive multiply (the native backdrop pass samples the current
// target; inside a group's bucket it would blur the empty bucket - the
// recorded gap): no effect group, the records carry the multiplied
// colours, exactly the pre-R16 behaviour.
func TestBackdropFilteredElementKeepsOpacityMultiply(t *testing.T) {
	card := gpui.Div().Flex().W(gpui.Px(100)).H(gpui.Px(80)).Opacity(0.5).Bg(red).
		BackdropBlur(4)
	_, scene := drawStyled(t, gpui.Div().Flex().P(gpui.Px(20)).Child(card).IntoElement())

	groups, err := scene.EffectGroups()
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 0 {
		t.Fatalf("effect groups = %d, want none (the backdrop-filter exception): %+v", len(groups), groups)
	}
	quads := quadsOf(t, scene)
	if len(quads) != 1 {
		t.Fatalf("quads = %d, want the box", len(quads))
	}
	if got, want := quads[0].Background, gpui.SolidBackground(red).Opacity(0.5); got != want {
		t.Errorf("quad background = %+v, want the multiplied %+v", got, want)
	}
}
