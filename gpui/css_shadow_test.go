package gpui

// Ticket 04 (CSS shadow and blur geometry, spec decision D8): the
// Style paint path's shadow and filter records follow CSS Backgrounds 3
// and filter-effects, each a recorded divergence from the pin (see the
// notes at div.go paintBox / Scene.PaintBoxShadow):
//
//   - the box-shadow blur's σ is half the CSS blur radius
//     (css-backgrounds-3 §shadow-blur; Chromium ShadowData::
//     BlurAsSigma, core/style/shadow_data.h:76-88 @ 0d4dcd4f, fed to
//     Skia whose sigma IS the standard deviation);
//   - the `filter`/`backdrop-filter` blur's σ IS the CSS length
//     (filter-effects-1 §funcdef-filter-blur; Chromium
//     FEGaussianBlur(raw length), filter_effect_builder.cc:284-291);
//     the pin's kernel takes σ = radius × 0.5, so the CSS radius is
//     doubled at the call;
//   - a spread grows the shadow shape's corner radii by §corner-shaping
//     (Chromium OutsetWithCornerCorrection via ApplySpreadToShadowShape,
//     box_painter_base.cc:90-95);
//   - the shadow list paints in reverse so the first shadow is on top
//     (Chromium shadow_list.h:46-47);
//   - the drop shadows and the ring paint inside the filter group, whose
//     bounds are the element's ink bounds (the box dilated by the shadow
//     ink — Chromium's filter reference box is the layer's visual
//     overflow, paint_layer.cc:2199-2205).
//
// These tests pin the record geometry through the element path; the
// Chrome-pixel fixtures live in internal/parity/fixtures/shadow.

import (
	"math"
	"testing"
)

// cssDev converts logical bounds to the test window's device pixels
// (the same helpers styled_test.go uses, restated for the internal
// test package).
func cssDev(x, y, w, h float32) Bounds {
	return Bounds{Origin: Point{X: x * TestScaleFactor, Y: y * TestScaleFactor}, Size: Size{Width: w * TestScaleFactor, Height: h * TestScaleFactor}}
}

// cssCorners is a uniform radius in device pixels.
func cssCorners(r float32) Corners {
	r *= TestScaleFactor
	return Corners{TopLeft: r, TopRight: r, BottomRight: r, BottomLeft: r}
}

// cssShadowScene draws one frame of a root with the given element and
// returns the scene (the element path: request → layout → paint).
func cssShadowScene(t *testing.T, element *DivElement) *Scene {
	t.Helper()
	ta := NewTestApp()
	app := ta.App()
	window := NewTestWindow(app, Size{Width: 300, Height: 220})
	view := NewEntity(app, window.Scope(), func(v *cssShadowView, cx *Context[cssShadowView]) {
		v.element = element
	})
	window.SetRootView(ViewOf(view))
	if _, err := DrawWindowFrame(window); err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}
	scene := LastDrawnScene(window)
	if scene == nil {
		t.Fatal("no drawn scene")
	}
	return scene
}

// cssShadowView renders one fixed element.
type cssShadowView struct{ element *DivElement }

func (v *cssShadowView) Render(w *Window, cx *Context[cssShadowView]) AnyElement {
	return Div().P(Px(20)).Child(v.element).IntoElement()
}

// TestBoxShadowBlurSigmaIsHalfTheCSSRadius: a 24px CSS blur casts a
// shadow whose record blur (the shader's σ) is 12 logical pixels — 24
// device pixels at the test scale 2. The pin passed 24 through as σ
// (48 device), twice as blurry as CSS.
func TestBoxShadowBlurSigmaIsHalfTheCSSRadius(t *testing.T) {
	box := Div().W(Px(100)).H(Px(60)).
		Shadow([]BoxShadow{NewBoxShadow(0, 0, SolidBackground(Black())).WithBlurRadius(24)})
	scene := cssShadowScene(t, box)
	shadows, err := scene.Shadows()
	if err != nil {
		t.Fatal(err)
	}
	if len(shadows) != 1 {
		t.Fatalf("shadows = %d, want 1", len(shadows))
	}
	if got, want := shadows[0].BlurRadius, 24*0.5*TestScaleFactor; got != want {
		t.Errorf("blur = %v, want σ = blur/2 = %v device (CSS σ = half the blur radius)", got, want)
	}
}

// TestBoxShadowSpreadGrowsCornerRadii: on a 100x40 box with 6px radii,
// a 4px spread gives the shadow shape 10px radii (r > spread →
// r + spread; Chromium OutsetWithCornerCorrection,
// float_rounded_rect.cc:138-152) while the element's clip-out shape
// keeps the element's 6px radii. The pin kept 6px radii for both.
func TestBoxShadowSpreadGrowsCornerRadii(t *testing.T) {
	box := Div().W(Px(100)).H(Px(60)).Rounded(Px(6)).
		Shadow([]BoxShadow{NewBoxShadow(0, 0, SolidBackground(Black())).WithSpreadRadius(4)})
	scene := cssShadowScene(t, box)
	shadows, err := scene.Shadows()
	if err != nil {
		t.Fatal(err)
	}
	if len(shadows) != 1 {
		t.Fatalf("shadows = %d, want 1", len(shadows))
	}
	got := shadows[0]
	if got.CornerRadii != cssCorners(10) {
		t.Errorf("shadow radii = %+v, want 10px (spread-grown; CSS corner-shaping), device 20", got.CornerRadii)
	}
	if got.ElementCornerRadii != cssCorners(6) {
		t.Errorf("element clip-out radii = %+v, want the element's 6px radii", got.ElementCornerRadii)
	}
	// The shadow box is the border box dilated by the spread:
	// (20,20,100,60) ⊕ 4 = (16,16,108,68) logical, cover bounds at
	// scale 2 → device (32,32,216x136).
	if got.Bounds != cssDev(16, 16, 108, 68) {
		t.Errorf("shadow bounds = %v, want the spread-dilated box", got.Bounds)
	}
	if got.BlurRadius != 0 {
		t.Errorf("blur = %v, want 0", got.BlurRadius)
	}
}

// TestBoxShadowSpreadRatioRule: a 4px radius smaller than the 10px
// spread grows by the cubic ratio rule, not by r + spread: 4 +
// 10*(1 - (1-0.4)^3 * (1-coverage^3)) with coverage =
// 2*min(4/120, 4/70) ≈ 0.0667 → ≈ 11.84, where the naive r + spread
// gives 14 and the pin kept 4. Chromium AdjustedRadiusDimension
// (float_rounded_rect.cc:138-152) is the same formula.
func TestBoxShadowSpreadRatioRule(t *testing.T) {
	box := Div().W(Px(120)).H(Px(70)).Rounded(Px(4)).
		Shadow([]BoxShadow{NewBoxShadow(0, 0, SolidBackground(Black())).WithSpreadRadius(10)})
	scene := cssShadowScene(t, box)
	shadows, err := scene.Shadows()
	if err != nil {
		t.Fatal(err)
	}
	if len(shadows) != 1 {
		t.Fatalf("shadows = %d, want 1", len(shadows))
	}
	radius := shadows[0].CornerRadii.TopLeft / TestScaleFactor
	if math.Abs(float64(radius)-11.8406) > 0.01 {
		t.Errorf("ratio-rule radius = %v, want ≈ 11.8406 (not the naive 14, not the pin's 4)", radius)
	}
}

// TestBoxShadowListPaintsFirstOnTop: two overlapping drop shadows paint
// in reverse list order, so the FIRST shadow's record carries the
// HIGHER draw order. The pin painted them in list order (the last on
// top).
func TestBoxShadowListPaintsFirstOnTop(t *testing.T) {
	first := NewBoxShadow(0, 0, SolidBackground(Black())).WithBlurRadius(6)
	second := NewBoxShadow(4, 4, SolidBackground(Black())).WithBlurRadius(6)
	box := Div().W(Px(100)).H(Px(60)).Shadow([]BoxShadow{first, second})
	scene := cssShadowScene(t, box)
	shadows, err := scene.Shadows()
	if err != nil {
		t.Fatal(err)
	}
	if len(shadows) != 2 {
		t.Fatalf("shadows = %d, want 2", len(shadows))
	}
	// The scene arrays sort by draw order, so [0] is the first PAINTED
	// (the second list entry) and [1] the last (the first list entry,
	// on top).
	if !(shadows[0].Order < shadows[1].Order) {
		t.Errorf("orders = %d, %d; the first list shadow must paint on top (the higher order)", shadows[0].Order, shadows[1].Order)
	}
}

// TestFilterBlurSigmaIsTheCSSRadius: `filter: blur(3px)` records 3
// logical (6 device) so the kernel's σ = 0.5 × the record radius in
// half-resolution TEXELS lands on the CSS σ = 3 logical — 6 device
// pixels (the pin's blur passes run on the downsampled ping/pong
// textures, so one texel is two device pixels: the effective
// device-pixel σ IS the record radius). Verified against Chrome by the
// shadow-filter fixture (0.42 mean channel diff with this value; 9.73
// with the radius doubled). No divergence from the pin: its value is
// already the CSS σ (the spec's D8 note "the pin uses σ = radius/2"
// read blur.rs:17 without the half-resolution factor).
func TestFilterBlurSigmaIsTheCSSRadius(t *testing.T) {
	box := Div().W(Px(100)).H(Px(60)).Bg(SolidBackground(White())).Blur(3)
	scene := cssShadowScene(t, box)
	boundaries, err := scene.FilterBoundaries()
	if err != nil {
		t.Fatal(err)
	}
	if len(boundaries) != 2 { // start + end of one group
		t.Fatalf("boundaries = %d, want 2 (start and end)", len(boundaries))
	}
	if got, want := boundaries[0].BlurRadius, 3*TestScaleFactor; got != want {
		t.Errorf("filter blur = %v, want the CSS radius = %v device (σ = the CSS length via the half-res kernel)", got, want)
	}
	// Without shadows or a ring the group bounds stay the element's.
	if boundaries[0].Bounds != cssDev(20, 20, 100, 60) {
		t.Errorf("filter group bounds = %v, want the element bounds when there is no ink", boundaries[0].Bounds)
	}
}

// TestFilterGroupCoversTheShadowInk: a filtered element with a box
// shadow (offset 12,10, blur 18, spread 0) dilates the group bounds by
// the shadow's ink outsets — ceil(3σ) ± offset per side, σ = blur/2:
// ceil(27)=27 → left 15, top 17, right 39, bottom 37 — so the group's
// composite covers the whole blurred shadow (Chromium's filter
// reference box is the layer's visual overflow, which the shadow ink
// feeds). The pin passed the element bounds and cut the shadow at
// bounds + 3σ.
func TestFilterGroupCoversTheShadowInk(t *testing.T) {
	box := Div().W(Px(100)).H(Px(60)).Bg(SolidBackground(White())).Blur(3).
		Shadow([]BoxShadow{NewBoxShadow(12, 10, SolidBackground(Black())).WithBlurRadius(18)})
	scene := cssShadowScene(t, box)
	boundaries, err := scene.FilterBoundaries()
	if err != nil {
		t.Fatal(err)
	}
	if len(boundaries) != 2 {
		t.Fatalf("boundaries = %d, want 2", len(boundaries))
	}
	// (20,20,100,60) dilated per side by (15, 39, 37, 17) =
	// (5, 3, 154, 114) logical — even values snap exactly at scale 2.
	if got, want := boundaries[0].Bounds, cssDev(5, 3, 154, 114); got != want {
		t.Errorf("filter group bounds = %v, want the ink-dilated %v", got, want)
	}
}

// TestBackdropFilterBlurSigmaIsTheCSSRadius: `backdrop-filter:
// blur(4px)` records 4 logical (8 device); the half-res kernel's
// σ = 0.5 × the record radius in texels is the CSS σ = 4 logical in
// device pixels (see TestFilterBlurSigmaIsTheCSSRadius).
func TestBackdropFilterBlurSigmaIsTheCSSRadius(t *testing.T) {
	box := Div().W(Px(100)).H(Px(60)).BackdropBlur(4)
	scene := cssShadowScene(t, box)
	backdrops, err := scene.BackdropFilters()
	if err != nil {
		t.Fatal(err)
	}
	if len(backdrops) != 1 {
		t.Fatalf("backdrops = %d, want 1", len(backdrops))
	}
	if got, want := backdrops[0].BlurRadius, 4*TestScaleFactor; got != want {
		t.Errorf("backdrop blur = %v, want the CSS radius = %v device (σ = the CSS length via the half-res kernel)", got, want)
	}
}
