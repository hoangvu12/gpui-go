package gpui_test

import (
	"math"
	"reflect"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// These tests run outside package gpui: the vocabulary and the mixins
// must work for third-party component types.

// styledView renders a fixed root element.
type styledView struct{ root gpui.AnyElement }

func (v *styledView) Render(w *gpui.Window, cx *gpui.Context[styledView]) gpui.AnyElement {
	return v.root
}

// drawStyled draws root in a 400x300 test window and returns the window
// and its scene.
func drawStyled(t *testing.T, root gpui.AnyElement) (*gpui.Window, *gpui.Scene) {
	t.Helper()
	ta := gpui.NewTestApp()
	app := ta.App()
	window := gpui.NewTestWindow(app, gpui.Size{Width: 400, Height: 300})
	view := gpui.NewEntity(app, window.Scope(), func(v *styledView, cx *gpui.Context[styledView]) { v.root = root })
	window.SetRootView(gpui.ViewOf(view))
	scene, err := gpui.DrawWindowFrame(window)
	if err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}
	return window, scene
}

func quadsOf(t *testing.T, scene *gpui.Scene) []gpui.Quad {
	t.Helper()
	quads, err := scene.Quads()
	if err != nil {
		t.Fatal(err)
	}
	return quads
}

func shadowsOf(t *testing.T, scene *gpui.Scene) []gpui.Shadow {
	t.Helper()
	shadows, err := scene.Shadows()
	if err != nil {
		t.Fatal(err)
	}
	return shadows
}

var (
	red   = gpui.Hsla{H: 0, S: 1, L: 0.5, A: 1}
	blue  = gpui.Hsla{H: 0.5, S: 1, L: 0.5, A: 1}
	green = gpui.Hsla{H: 0.25, S: 1, L: 0.5, A: 1}
)

// testScale is the test window's scale factor (testwindow.go): scene
// geometry is in device pixels.
const testScale = gpui.TestScaleFactor

// dev converts logical bounds to the test window's device pixels.
func dev(x, y, w, h float32) gpui.Bounds {
	return gpui.Bounds{Origin: gpui.Point{X: x * testScale, Y: y * testScale}, Size: gpui.Size{Width: w * testScale, Height: h * testScale}}
}

// corners and edges in device pixels.
func corners(r float32) gpui.Corners {
	r *= testScale
	return gpui.Corners{TopLeft: r, TopRight: r, BottomRight: r, BottomLeft: r}
}

func edges(w float32) gpui.Edges {
	w *= testScale
	return gpui.Edges{Top: w, Right: w, Bottom: w, Left: w}
}

// union is the bounding box of quads (a border-only quad paints as
// strips around its empty interior, window.rs paint_quad).
func union(quads []gpui.Quad) gpui.Bounds {
	b := quads[0].Bounds
	for _, q := range quads[1:] {
		minX, minY := min(b.Origin.X, q.Bounds.Origin.X), min(b.Origin.Y, q.Bounds.Origin.Y)
		maxX, maxY := max(b.Right(), q.Bounds.Right()), max(b.Bottom(), q.Bounds.Bottom())
		b = gpui.Bounds{Origin: gpui.Point{X: minX, Y: minY}, Size: gpui.Size{Width: maxX - minX, Height: maxY - minY}}
	}
	return b
}

// painted is a background as the paint path stores it (the pinned
// Background::opacity round trip at element opacity 1).
func painted(c gpui.Hsla) gpui.Background { return gpui.SolidBackground(c).Opacity(1) }

// TestStyledMacroSemantics checks representative generated methods
// against the styles.rs tables (field paths, units, negation, fractions).
func TestStyledMacroSemantics(t *testing.T) {
	d := gpui.Div().Flex().W4().HNeg1_2().MinW1_3().MaxHFull().MNeg0().Px2().PyPx().
		Inset0().TopAuto().GapX0p5().RoundedSm().RoundedTlFull().BorderX2().BorderT(gpui.Px(5)).
		Ring2().InsetRing8().RoundedSmoothing0p6().CursorPointer().CursorGrab().Invisible().OverflowXHidden().Relative()
	s := d.Style()
	expect := func(name string, got, want any) {
		t.Helper()
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %+v, want %+v", name, got, want)
		}
	}
	get := func(o gpui.Optional[gpui.Length]) gpui.Length { v, _ := o.Get(); return v }
	getD := func(o gpui.Optional[gpui.DefiniteLength]) gpui.DefiniteLength { v, _ := o.Get(); return v }
	getA := func(o gpui.Optional[gpui.AbsoluteLength]) gpui.AbsoluteLength { v, _ := o.Get(); return v }
	expect("w_4", get(s.SizeWidth), gpui.RemsLength(1))
	expect("h_neg_1_2", get(s.SizeHeight), gpui.DefiniteLengthOf(gpui.Fraction(-0.5)))
	third := get(s.MinSizeWidth).Definite.Value
	if third != float32(1)/float32(3) {
		t.Errorf("min_w_1_3 = %v, want the f32 quotient 1/3", third)
	}
	expect("max_h_full", get(s.MaxSizeHeight), gpui.DefiniteLengthOf(gpui.Fraction(1)))
	negZero := get(s.MarginTop).Definite.Value
	if negZero != 0 || !math.Signbit(float64(negZero)) {
		t.Errorf("m_neg_0 = %v, want IEEE negative zero (-px(0.))", negZero)
	}
	expect("m_neg_0 covers all edges", []bool{s.MarginRight.IsSome(), s.MarginBottom.IsSome(), s.MarginLeft.IsSome()}, []bool{true, true, true})
	expect("px_2 left", getD(s.PaddingLeft), gpui.DefiniteRem(0.5))
	expect("px_2 right", getD(s.PaddingRight), gpui.DefiniteRem(0.5))
	expect("py_px top", getD(s.PaddingTop), gpui.DefinitePx(1))
	expect("py_px bottom", getD(s.PaddingBottom), gpui.DefinitePx(1))
	expect("inset_0 left", get(s.InsetLeft), gpui.PxLength(0))
	expect("top_auto", get(s.InsetTop), gpui.AutoLength())
	expect("gap_x_0p5", getD(s.GapWidth), gpui.DefiniteRem(0.125))
	expect("gap_x leaves height", s.GapHeight.IsSome(), false)
	expect("rounded_sm tr", getA(s.CornerRadiiTopRight), gpui.RemsOf(0.25))
	expect("rounded_tl_full", getA(s.CornerRadiiTopLeft), gpui.Px(9999))
	expect("border_x_2", []gpui.AbsoluteLength{getA(s.BorderWidthsLeft), getA(s.BorderWidthsRight)}, []gpui.AbsoluteLength{gpui.Px(2), gpui.Px(2)})
	expect("border_t custom", getA(s.BorderWidthsTop), gpui.Px(5))
	expect("ring_2", s.RingWidth, gpui.Some(float32(2)))
	expect("inset_ring_8", s.InsetRingWidth, gpui.Some(float32(8)))
	expect("rounded_smoothing_0p6", s.CornerSmoothing, gpui.Some(float32(0.6)))
	expect("cursor_grab (last wins)", s.MouseCursor, gpui.Some(gpui.CursorOpenHand))
	expect("invisible", s.Visibility, gpui.Some(gpui.VisibilityHidden))
	expect("overflow_x_hidden", []bool{s.OverflowX.IsSome(), s.OverflowY.IsSome()}, []bool{true, false})
	expect("relative", s.Position, gpui.Some(gpui.PositionRelative))

	// The custom value setters accept every Into form.
	s = gpui.Div().Flex().W(gpui.Px(3)).H(gpui.Fraction(0.5)).P(gpui.RemsOf(2)).Size(gpui.AutoLength()).Gap(gpui.Px(4)).Style()
	expect("size one-arg width", get(s.SizeWidth), gpui.AutoLength())
	expect("p from AbsoluteLength", getD(s.PaddingBottom), gpui.DefiniteRem(2))
	expect("gap one-arg", []gpui.DefiniteLength{getD(s.GapWidth), getD(s.GapHeight)}, []gpui.DefiniteLength{gpui.DefinitePx(4), gpui.DefinitePx(4)})
	s = gpui.Div().Flex().Size(gpui.PxLength(10), gpui.PxLength(20)).Style()
	expect("size two-arg", []gpui.Length{get(s.SizeWidth), get(s.SizeHeight)}, []gpui.Length{gpui.PxLength(10), gpui.PxLength(20)})

	// shadow_md (styles.rs:904).
	s = gpui.Div().Flex().ShadowMd().Style()
	shadows, _ := s.BoxShadow.Get()
	expect("shadow_md", shadows, []gpui.BoxShadow{
		gpui.NewBoxShadow(0, 4, gpui.SolidBackground(gpui.Hsla{A: 0.1})).WithBlurRadius(6).WithSpreadRadius(-1),
		gpui.NewBoxShadow(0, 2, gpui.SolidBackground(gpui.Hsla{A: 0.1})).WithBlurRadius(4).WithSpreadRadius(-2),
	})
	s = gpui.Div().Flex().ShadowMd().ShadowNone().Style()
	shadows, present := s.BoxShadow.Get()
	expect("shadow_none is an explicit empty list", []any{present, len(shadows)}, []any{true, 0})
}

// TestStyledHandWrittenSemantics checks styled.rs's own methods.
func TestStyledHandWrittenSemantics(t *testing.T) {
	s := gpui.Div().Flex().Flex1().ItemsStart().SelfStart().ContentEnd().JustifyBetween().Truncate().
		LineClamp(2).TextDecorationWavy().TextDecorationColor(red).TextDecoration2().
		ColSpanFull().RowStart(2).Blur(3).Blur(4).BorderDashedLength(-1).BorderDashedGap(0.5).
		Font(gpui.FontDescriptor{Family: "Inter", Weight: 600}).Style()
	if v, _ := s.FlexBasis.Get(); v != gpui.DefiniteLengthOf(gpui.Fraction(0)) {
		t.Errorf("flex_1 basis = %v", v)
	}
	if v, _ := s.AlignItems.Get(); v != gpui.AlignItemsFlexStart {
		t.Errorf("items_start = %v, want FlexStart (styled.rs:439)", v)
	}
	if v, _ := s.AlignSelf.Get(); v != gpui.AlignItemsStart {
		t.Errorf("self_start = %v, want Start (styled.rs:474)", v)
	}
	if v, _ := s.AlignContent.Get(); v != gpui.AlignContentFlexEnd {
		t.Errorf("content_end = %v", v)
	}
	if v, _ := s.OverflowY.Get(); v != gpui.OverflowHidden {
		t.Errorf("truncate overflow = %v", v)
	}
	if v, _ := s.Text.TextOverflow.Get(); v != (gpui.TextOverflow{Kind: gpui.TextOverflowTruncate, Ellipsis: "…"}) {
		t.Errorf("truncate overflow = %+v", v)
	}
	if v, _ := s.Text.WhiteSpace.Get(); v != gpui.WhiteSpaceNowrap {
		t.Errorf("truncate white space = %v", v)
	}
	if v, _ := s.Text.LineClamp.Get(); v != 2 {
		t.Errorf("line_clamp = %v", v)
	}
	u, _ := s.Text.Underline.Get()
	if !u.Wavy || u.Thickness != 2 || u.Color == nil || *u.Color != red {
		t.Errorf("underline = %+v", u)
	}
	loc, _ := s.GridLocation.Get()
	if loc.Column.Start != gpui.LineGridPlacement(1) || loc.Column.End != gpui.LineGridPlacement(-1) || loc.Row.Start != gpui.LineGridPlacement(2) || loc.Row.End != gpui.AutoGridPlacement() {
		t.Errorf("grid location = %+v", loc)
	}
	if f, _ := s.Filter.Get(); !reflect.DeepEqual(f, []gpui.Filter{gpui.Blur(3), gpui.Blur(4)}) {
		t.Errorf("blur appends: %+v", f)
	}
	if v, _ := s.BorderDashedLength.Get(); v != 0 {
		t.Errorf("border_dashed_length clamps at zero: %v", v)
	}
	if s.Text.FontFallbacks.IsSome() {
		t.Error("font with no fallbacks must leave the fallback refinement absent (styled.rs:899)")
	}
	if v, _ := s.Text.FontWeight.Get(); v != 600 {
		t.Errorf("font weight = %v", v)
	}
	// content_normal and text_decoration_none reset to absent.
	s = gpui.Div().Flex().ContentCenter().ContentNormal().Underline().TextDecorationNone().Style()
	if s.AlignContent.IsSome() || s.Text.Underline.IsSome() {
		t.Error("content_normal / text_decoration_none must make the refinement absent")
	}
}

// TestStyleRefinementPinnedTests ports style.rs:1618-1630
// border_color_accepts_backgrounds_and_solid_colors (solid backgrounds:
// the port has no gradients), :1806-1821 test_text_style_refinement and
// :1823-1848 dashed_border_pattern_has_legacy_defaults_and_refines_independently.
func TestStyleRefinementPinnedTests(t *testing.T) {
	bg := gpui.SolidBackground(blue)
	if v, _ := gpui.Div().Flex().BorderColor(bg).Style().BorderColor.Get(); v != bg {
		t.Errorf("border_color(Background) = %+v", v)
	}
	if v, _ := gpui.Div().Flex().BorderColor(red).Style().BorderColor.Get(); v != gpui.SolidBackground(red) {
		t.Errorf("border_color(Hsla) = %+v", v)
	}

	style := gpui.DefaultStyle()
	size := gpui.Div().Flex().TextSize(gpui.Px(20)).Style()
	style.Refine(&size)
	weight := gpui.Div().Flex().FontWeight(600).Style()
	style.Refine(&weight)
	text := style.TextStyle()
	if text == nil {
		t.Fatal("text_style() = None")
	}
	if v, _ := text.FontSize.Get(); v != gpui.Px(20) {
		t.Errorf("font size = %v", v)
	}
	if v, _ := text.FontWeight.Get(); v != 600 {
		t.Errorf("font weight = %v", v)
	}

	style = gpui.DefaultStyle()
	if style.BorderDashedLength != gpui.DefaultBorderDashedLength || style.BorderDashedGap != gpui.DefaultBorderDashedGap {
		t.Fatalf("dash defaults = %v/%v", style.BorderDashedLength, style.BorderDashedGap)
	}
	dash := gpui.Div().Flex().BorderDashedLength(4).BorderDashedGap(0.5).Style()
	style.Refine(&dash)
	if style.BorderDashedLength != 4 || style.BorderDashedGap != 0.5 || style.BorderStyle != gpui.BorderStyleSolid {
		t.Errorf("dash refine = %v/%v/%v", style.BorderDashedLength, style.BorderDashedGap, style.BorderStyle)
	}
	dashed := gpui.Div().Flex().BorderDashed().Style()
	style.Refine(&dashed)
	if style.BorderStyle != gpui.BorderStyleDashed {
		t.Error("border_dashed")
	}
}

// TestStyleSnapshotIsIndependent checks that Style() deep-copies the
// slice fields.
func TestStyleSnapshotIsIndependent(t *testing.T) {
	d := gpui.Div().Flex().Shadow([]gpui.BoxShadow{gpui.NewBoxShadow(1, 1, gpui.SolidBackground(red))}).Blur(2)
	snap := d.Style()
	shadows, _ := snap.BoxShadow.Get()
	shadows[0].OffsetX = 99
	filters, _ := snap.Filter.Get()
	filters[0].BlurRadius = 99
	again := d.Style()
	if s, _ := again.BoxShadow.Get(); s[0].OffsetX != 1 {
		t.Error("snapshot shadow mutation escaped")
	}
	if f, _ := again.Filter.Get(); f[0].BlurRadius != 2 {
		t.Error("snapshot filter mutation escaped")
	}
	d.Blur(3)
	if len(filters) != 1 {
		t.Error("appending after a snapshot reached the snapshot")
	}
}

// TestDivPaintsTheBoxInPinnedOrder is the scene transcript of
// style.rs:894-1015 paint_box: drop shadows, the outer ring, the
// background, the inset ring and inset shadows, the border, then the
// children, with the clamped corner radii shared by every primitive.
// The border-before-children order is CSS's (rev 16, browser-core
// ticket 55): the background phase paints backgrounds AND borders
// before the foreground (paint_phase.h:33-35: "Background phase:
// backgrounds and borders of all blocks are painted", the phase before
// the foreground; box_fragment_painter.cc's
// PaintBoxDecorationBackgroundWithRectImpl paints the border as the
// decoration phase's last step). The pin painted the border after the
// children, so a child poking over the border was covered by it.
func TestDivPaintsTheBoxInPinnedOrder(t *testing.T) {
	inset := gpui.NewBoxShadow(1, 1, gpui.SolidBackground(green)).WithInset()
	drop := gpui.NewBoxShadow(0, 2, gpui.SolidBackground(blue)).WithBlurRadius(4)
	box := gpui.Div().Flex().W(gpui.Px(100)).H(gpui.Px(40)).Bg(red).Rounded(gpui.Px(6)).
		Border2().BorderColor(blue).Shadow([]gpui.BoxShadow{inset, drop}).
		Ring(3).RingColor(green).InsetRing2().InsetRingColor(green).
		// The child fills the whole padding box (it pokes over none of
		// the border bands - the z-order gate over the border lives in
		// TestBorderPaintsBeforeTheOverflowingChild below).
		Child(gpui.Div().Flex().Size(gpui.Px(96)).H(gpui.Px(36)).Bg(green))
	_, scene := drawStyled(t, gpui.Div().Flex().P(gpui.Px(20)).Child(box).IntoElement())

	quads := quadsOf(t, scene)
	shadows := shadowsOf(t, scene)
	// The ring and the border are border-only quads: each paints as four
	// strips (window.rs paint_quad's split).
	if len(quads) != 10 || len(shadows) != 3 {
		t.Fatalf("quads = %d, shadows = %d; want 4 ring strips, bg, 4 border strips, the child and 3 shadows", len(quads), len(shadows))
	}
	elem := dev(20, 20, 100, 40)
	r6 := corners(6)
	// The dump is the SORTED stream (bounds-tree z-order): with the
	// border painting before the child the strips and the child
	// interleave, so identify the roles instead of slicing - the ring
	// strips (border-only, 3px, green), the background (red), the border
	// strips (border-only, 2px, blue) and the child (solid green fill).
	var ringStrips, borderStrips []gpui.Quad
	var bg, child gpui.Quad
	for _, q := range quads {
		switch {
		case q.BorderWidths == edges(3):
			ringStrips = append(ringStrips, q)
		case q.BorderWidths == edges(2):
			borderStrips = append(borderStrips, q)
		case q.Background == painted(red):
			bg = q
		case q.Background == painted(green) && q.BorderWidths == (gpui.Edges{}):
			child = q
		}
	}
	if len(ringStrips) != 4 || len(borderStrips) != 4 || bg.Order == 0 || child.Order == 0 {
		t.Fatalf("quads by role: ring %d border %d bg %+v child %+v", len(ringStrips), len(borderStrips), bg, child)
	}
	dropShadow, insetRing, insetShadow := shadows[0], shadows[1], shadows[2]

	// Scene orders are the kernel's bounds-tree z-orders (a primitive
	// orders above every earlier one it overlaps), so the paint sequence
	// shows as non-decreasing orders between overlapping primitives: the
	// decoration stack rises (shadows, ring, background, insets) and the
	// whole decoration phase sits below the child - including the border,
	// which now paints BEFORE the child (CSS's background phase), so the
	// child's order exceeds every border strip's (the strips overlap the
	// child's insertion footprint).
	maxRing, maxBorder := ringStrips[0].Order, borderStrips[0].Order
	for _, q := range ringStrips {
		maxRing = max(maxRing, q.Order)
	}
	for _, q := range borderStrips {
		maxBorder = max(maxBorder, q.Order)
	}
	if !(dropShadow.Order < bg.Order && maxRing < bg.Order && bg.Order < insetRing.Order &&
		insetRing.Order < insetShadow.Order && maxBorder < child.Order) {
		t.Errorf("paint order: drop %d ring %d bg %d insetRing %d inset %d border <=%d child %d",
			dropShadow.Order, ringStrips[0].Order, bg.Order, insetRing.Order, insetShadow.Order, maxBorder, child.Order)
	}
	if dropShadow.Inset || dropShadow.Bounds != dev(20, 22, 100, 40) || dropShadow.CornerRadii != r6 {
		t.Errorf("drop shadow = %+v", dropShadow)
	}
	// Recorded divergence toward CSS (spec D8): the box-shadow blur's σ
	// is half the CSS blur radius (css-backgrounds-3 §shadow-blur;
	// Chromium ShadowData::BlurAsSigma, shadow_data.h:76-88), so the
	// record carries CSS blur 4 as σ 2 logical → 4 device pixels; the
	// pin passed the blur through as σ (8 device pixels here).
	if dropShadow.BlurRadius != 4*testScale/2 {
		t.Errorf("drop shadow blur = %v, want CSS σ = blur/2 = %v device (divergence toward CSS)", dropShadow.BlurRadius, 4*testScale/2)
	}
	for _, ring := range ringStrips {
		if ring.BorderWidths != edges(3) || ring.CornerRadii != corners(9) || !ring.Background.IsTransparent() || ring.BorderColor != painted(green) {
			t.Errorf("ring strip = %+v", ring)
		}
	}
	if got := union(ringStrips); got != dev(17, 17, 106, 46) {
		t.Errorf("ring bounds = %+v, want the element dilated by 3", got)
	}
	if bg.Bounds != elem || bg.CornerRadii != r6 || bg.Background != painted(red) || bg.BorderWidths != (gpui.Edges{}) || !bg.BorderColor.IsTransparent() {
		t.Errorf("background = %+v", bg)
	}
	// The inset ring and inset shadow cast from the PADDING box
	// (consumer gap 09, the recorded CSS deviation from the pin): the
	// element bounds inset by the 2px border — (22,22,96x36) — with the
	// radii reduced by the border (6-2=4), so the ring's spread-2 hole
	// is (24,24,92x32) with 2px radii and the shadow's (1,1) offset hole
	// is (23,23,96x36).
	if !insetRing.Inset || insetRing.Bounds != dev(24, 24, 92, 32) || insetRing.CornerRadii != corners(2) || insetRing.Color != painted(green) {
		t.Errorf("inset ring = %+v", insetRing)
	}
	if !insetShadow.Inset || insetShadow.Bounds != dev(23, 23, 96, 36) || insetShadow.CornerRadii != corners(4) {
		t.Errorf("inset shadow = %+v", insetShadow)
	}
	if child.Bounds != dev(22, 22, 96, 36) {
		t.Errorf("child = %+v (the padding box)", child)
	}
	for _, border := range borderStrips {
		if border.BorderWidths != edges(2) || border.BorderColor != painted(blue) || !border.Background.IsTransparent() || border.CornerRadii != r6 {
			t.Errorf("border strip = %+v", border)
		}
	}
	if got := union(borderStrips); got != elem {
		t.Errorf("border bounds = %+v, want the element bounds", got)
	}
}

// TestBorderPaintsBeforeTheOverflowingChild pins the CSS border order
// (rev 16, browser-core ticket 55): a child that pokes over the
// element's border paints ABOVE it (the background phase - backgrounds
// and borders - precedes the foreground; paint_phase.h:33-35), the
// z-order gate of the change. The child covers the border box's right
// and bottom bands entirely (overflow: visible, no clip), so its
// bounds-tree order must exceed every border strip's.
func TestBorderPaintsBeforeTheOverflowingChild(t *testing.T) {
	box := gpui.Div().Flex().W(gpui.Px(100)).H(gpui.Px(40)).
		Border2().BorderColor(blue).Bg(red).
		// Flex children do not shrink past the box by default
		// (flex-shrink 0 in the fixture vocabulary): the 120x60 child
		// pokes 20 logical px past the box's right and bottom edges.
		Child(gpui.Div().Flex().Size(gpui.Px(120)).H(gpui.Px(60)).Bg(green).FlexShrink(0))
	_, scene := drawStyled(t, gpui.Div().Flex().P(gpui.Px(20)).Child(box).IntoElement())
	quads := quadsOf(t, scene)
	var child gpui.Quad
	var borderStrips []gpui.Quad
	for _, q := range quads {
		switch {
		case q.Background == painted(green):
			child = q
		case q.BorderWidths == edges(2):
			borderStrips = append(borderStrips, q)
		}
	}
	if child.Order == 0 || len(borderStrips) != 4 {
		t.Fatalf("quads by role: border %d child %+v", len(borderStrips), child)
	}
	for _, strip := range borderStrips {
		if child.Order <= strip.Order {
			t.Fatalf("the overflowing child orders %d below a border strip %d (bounds %v) - CSS paints the border below the foreground (paint_phase.h:33-35)",
				child.Order, strip.Order, strip.Bounds)
		}
	}
}

// TestDivPaintRadiiClampAndOpacity checks the radius clamp
// (geometry.rs:2511) and the element opacity's R16 encoding (D9,
// browser-core ticket 55): a single-primitive translucent element
// brackets its subtree in an effect group carrying the opacity (the
// native plan folds the single primitive into the per-primitive colour
// multiply at snapshot time, so the pixels are the multiply's); the
// STORED record keeps the un-multiplied colour - the group owns the
// opacity. The effect-group dump carries HasOpacity and the opacity
// value.
func TestDivPaintRadiiClampAndOpacity(t *testing.T) {
	_, scene := drawStyled(t, gpui.Div().Flex().W(gpui.Px(60)).H(gpui.Px(20)).RoundedFull().Opacity(0.5).Bg(red).IntoElement())
	quads := quadsOf(t, scene)
	if len(quads) != 1 {
		t.Fatalf("quads = %d", len(quads))
	}
	if quads[0].CornerRadii != corners(10) {
		t.Errorf("rounded_full radii = %+v, want clamped to 10", quads[0].CornerRadii)
	}
	if quads[0].Background != painted(red) {
		t.Errorf("opacity background = %+v, want the un-multiplied colour (the group carries the opacity)", quads[0].Background)
	}
	groups, err := scene.EffectGroups()
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 {
		t.Fatalf("effect groups = %d, want a matched pair", len(groups))
	}
	start, end := groups[0], groups[1]
	if !start.IsStart || end.IsStart {
		t.Errorf("effect groups = start %v end %v, want a start/end pair", start.IsStart, end.IsStart)
	}
	if !start.HasOpacity || !end.HasOpacity {
		t.Errorf("effect groups = %+v %+v, want the opacity flag on the pair", start, end)
	}
	if d := start.Opacity - 0.5; d < -1e-6 || d > 1e-6 {
		t.Errorf("group opacity = %v, want 0.5", start.Opacity)
	}
}

// TestInvisibleDivKeepsLayoutAndPaintsNothing checks visibility: hidden
// (div.rs:3021): no primitives for the element or its children, but its
// layout space is kept.
func TestInvisibleDivKeepsLayoutAndPaintsNothing(t *testing.T) {
	window, scene := drawStyled(t, gpui.Div().Flex().
		Child(gpui.Div().Flex().Size(gpui.Px(30)).Bg(red).Invisible().Child(gpui.Div().Flex().Size(gpui.Px(5)).Bg(blue))).
		Child(gpui.Div().Flex().Size(gpui.Px(30)).DebugSelector("after")).IntoElement())
	if quads := quadsOf(t, scene); len(quads) != 0 {
		t.Errorf("invisible div painted %d quads", len(quads))
	}
	if b, ok := gpui.WindowDebugBound(window, "after"); !ok || b.Origin.X != 30 {
		t.Errorf("sibling bounds = %+v, want x = 30 (space kept)", b)
	}
}

// TestOverflowMaskInsetsByBorderWidths checks style.rs:793-850
// overflow_mask: clipped overflow masks the children to the bounds
// inset by the border widths — always, whatever the border color
// (CSS clips at the padding box: Chromium's LayoutBox::OverflowClipRect
// contracts the border box by BorderOutsets unconditionally,
// layout_box.cc:2288-2302; a recorded divergence toward CSS from the
// pin, which insets only a visible border — style.rs:812-820). The
// transparent-border case is the observable one: its border still
// reserves its space and still insets the clip.
func TestOverflowMaskInsetsByBorderWidths(t *testing.T) {
	styled := func(borderColor gpui.Hsla) []gpui.Quad {
		_, scene := drawStyled(t, gpui.Div().Flex().W(gpui.Px(50)).H(gpui.Px(50)).OverflowHidden().Border4().BorderColor(borderColor).
			Child(gpui.Div().Flex().Size(gpui.Px(100)).Bg(red)).IntoElement())
		return quadsOf(t, scene)
	}
	want := dev(4, 4, 42, 42)
	for name, tc := range map[string]struct {
		borderColor gpui.Hsla
		quads       int
	}{
		// A visible border paints its four strips on top of the child.
		"visible border": {blue, 5},
		// A transparent border paints nothing itself (is_border_visible)
		// but still reserves its space and still insets the clip.
		"transparent border": {gpui.TransparentBlack(), 1},
	} {
		quads := styled(tc.borderColor)
		if len(quads) != tc.quads {
			t.Fatalf("%s: quads = %d, want %d", name, len(quads), tc.quads)
		}
		// The dump is the SORTED stream and the border paints before the
		// child now (rev 16, ticket 55 - CSS's background-phase border),
		// so find the CHILD by its solid red fill instead of slicing.
		var child *gpui.Quad
		for i := range quads {
			if quads[i].Background == painted(red) && quads[i].BorderWidths == (gpui.Edges{}) {
				child = &quads[i]
				break
			}
		}
		if child == nil {
			t.Fatalf("%s: no child quad in %+v", name, quads)
		}
		if child.ContentMask != want {
			t.Errorf("%s: child mask = %+v, want %+v (the padding box, inset by the border widths)", name, child.ContentMask, want)
		}
	}
}

// TestDebugOutlineAndFilters checks the debug outline (style.rs:907) and
// the filter / backdrop-filter primitives (style.rs:932-1009).
func TestDebugOutlineAndFilters(t *testing.T) {
	_, scene := drawStyled(t, gpui.Div().Flex().Size(gpui.Px(20)).Debug().BackdropBlur(8).Blur(0).Blur(3).Bg(red).IntoElement())
	quads := quadsOf(t, scene)
	if len(quads) != 5 {
		t.Fatalf("quads = %d, want 4 outline strips + background", len(quads))
	}
	for _, q := range quads[:4] {
		if q.BorderWidths != edges(1) || q.BorderColor != painted(gpui.Red()) {
			t.Errorf("debug outline strip = %+v", q)
		}
	}
	if union(quads[:4]) != dev(0, 0, 20, 20) || quads[4].Background != painted(red) {
		t.Errorf("outline/background = %+v / %+v", union(quads[:4]), quads[4])
	}
	backdrops, err := scene.BackdropFilters()
	// The filter and backdrop blur radii pass through the pin's record
	// unchanged: the kernel's σ = 0.5 × the record radius in
	// half-resolution texels, so the effective device-pixel σ IS the
	// record radius — the CSS σ (see div.go paintBox; no divergence).
	if err != nil || len(backdrops) != 1 || backdrops[0].BlurRadius != 8*testScale {
		t.Errorf("backdrop filters = %+v, %v", backdrops, err)
	}
	groups, err := scene.FilterBoundaries()
	if err != nil || len(groups) != 2 || !groups[0].IsStart || groups[1].IsStart || groups[0].BlurRadius != 3*testScale {
		t.Errorf("filter groups = %+v, %v (the identity blur(0) runs inline)", groups, err)
	}
}

// TestGridColsMaxContentSizesColumnsToContent ports div.rs:6098-6115
// grid_cols_max_content_sizes_columns_to_their_content.
func TestGridColsMaxContentSizesColumnsToContent(t *testing.T) {
	widths := []float32{100, 200, 50}
	cells := make([]any, 0, len(widths))
	for i, w := range widths {
		cells = append(cells, gpui.Div().Flex().DebugSelector([]string{"cell-0", "cell-1", "cell-2"}[i]).W(gpui.Px(w)).H(gpui.Px(10)))
	}
	window, _ := drawStyled(t, gpui.Div().Flex().SizeFull().Child(
		gpui.Div().WFull().Grid().GridColsMaxContent(uint16(len(widths))).Children(cells...)).IntoElement())
	for name, x := range map[string]float32{"cell-0": 0, "cell-1": 100, "cell-2": 300} {
		b, ok := gpui.WindowDebugBound(window, name)
		if !ok || b.Origin.X != x {
			t.Errorf("%s = %+v, want x = %v", name, b, x)
		}
	}
}
