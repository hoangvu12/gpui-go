package gpui

import (
	"math"
	"testing"

	"gpui-go/internal/native"
)

// This file pins the layout adapter's pure unit semantics: the ported
// rounding helpers (against the pinned util.rs tests), the style string
// parsing (the reference TryFrom impls), the device-pixel conversion, the
// Style-to-ABI-record translation (into_taffy_style), the stroke minimum
// and the measured-size snapping. Engine-backed tests (LayoutID
// generation invalidation, re-entry guards, the full pipeline) live in
// layout_engine_test.go behind the windows build tag.

// ---------------------------------------------------------------------------
// Rounding helpers (pinned crates/gpui/src/util.rs tests)
// ---------------------------------------------------------------------------

// TestRoundHalfTowardZero ports util.rs test_round_half_toward_zero:
// midpoint ties go toward zero, non-midpoint values round to nearest,
// integers are unchanged. Signed zero is preserved through copysign
// (-0.5 rounds to -0.0, which carries the sign bit).
func TestRoundHalfTowardZero(t *testing.T) {
	// Midpoint ties go toward zero.
	ties := []struct {
		in, want float32
	}{
		{0.5, 0}, {1.5, 1}, {2.5, 2},
		{-0.5, 0}, {-1.5, -1}, {-2.5, -2},
	}
	for _, c := range ties {
		if got := roundHalfTowardZero(c.in); got != c.want {
			t.Errorf("roundHalfTowardZero(%v) = %v, want %v", c.in, got, c.want)
		}
	}

	// Non-midpoint values round to nearest.
	nearest := []struct {
		in, want float32
	}{
		{1.5001, 2}, {1.4999, 1},
		{-1.5001, -2}, {-1.4999, -1},
	}
	for _, c := range nearest {
		if got := roundHalfTowardZero(c.in); got != c.want {
			t.Errorf("roundHalfTowardZero(%v) = %v, want %v", c.in, got, c.want)
		}
	}

	// Integers are unchanged.
	for _, c := range []float32{0, 3, -3} {
		if got := roundHalfTowardZero(c); got != c {
			t.Errorf("roundHalfTowardZero(%v) = %v, want %v", c, got, c)
		}
	}
}

// TestRoundHalfTowardZeroSignedZero pins the copysign behavior the
// contract calls out ("Preserve the exact helper behavior, including
// negative inputs and signed zero"): -0.5 rounds to -0.0 and -0.0 stays
// -0.0, so the sign bit survives the round trip.
func TestRoundHalfTowardZeroSignedZero(t *testing.T) {
	negZero := float32(math.Copysign(0, -1)) // Go constants have no -0.0
	if got := roundHalfTowardZero(-0.5); math.Float32bits(got) != math.Float32bits(negZero) {
		t.Errorf("roundHalfTowardZero(-0.5) = %v (bits %#x), want -0.0", got, math.Float32bits(got))
	}
	if got := roundHalfTowardZero(negZero); math.Float32bits(got) != math.Float32bits(negZero) {
		t.Errorf("roundHalfTowardZero(-0.0) = %v (bits %#x), want -0.0", got, math.Float32bits(got))
	}
	if got := roundHalfTowardZero(0.0); math.Float32bits(got) != 0 {
		t.Errorf("roundHalfTowardZero(0.0) = %v (bits %#x), want +0.0", got, math.Float32bits(got))
	}
}

// TestDevicePixelHelpers ports util.rs test_device_pixel_helpers: the
// snap uses half-toward-zero, the stroke clamps non-zero input up to at
// least 1 device pixel, and floor/ceil bracket the value.
func TestDevicePixelHelpers(t *testing.T) {
	// Snap uses half-toward-zero: 1.0 * 1.5 = 1.5 ties toward 1.0.
	snaps := []struct {
		logical, scale, want float32
	}{
		{1.0, 1.5, 1.0},
		{0.3, 2.0, 1.0},
		{1.4, 1.0, 1.0},
		{1.6, 1.0, 2.0},
	}
	for _, c := range snaps {
		if got := roundToDevicePixel(c.logical, c.scale); got != c.want {
			t.Errorf("roundToDevicePixel(%v, %v) = %v, want %v", c.logical, c.scale, got, c.want)
		}
	}

	// Stroke uses snap, but clamps non-zero input up to at least 1dp.
	strokes := []struct {
		logical, scale, want float32
	}{
		{0.0, 1.0, 0.0},
		{0.4, 1.0, 1.0},
		{0.5, 1.0, 1.0},
		{1.0, 1.5, 1.0},
		{1.6, 1.0, 2.0},
	}
	for _, c := range strokes {
		if got := roundStrokeToDevicePixel(c.logical, c.scale); got != c.want {
			t.Errorf("roundStrokeToDevicePixel(%v, %v) = %v, want %v", c.logical, c.scale, got, c.want)
		}
	}

	// Floor and ceil bracket the value.
	fc := []struct {
		logical, scale, floor, ceil float32
	}{
		{0.3, 2.0, 0.0, 1.0},
		{2.1, 1.0, 2.0, 3.0},
		{2.0, 2.0, 4.0, 4.0},
	}
	for _, c := range fc {
		if got := floorToDevicePixel(c.logical, c.scale); got != c.floor {
			t.Errorf("floorToDevicePixel(%v, %v) = %v, want %v", c.logical, c.scale, got, c.floor)
		}
		if got := ceilToDevicePixel(c.logical, c.scale); got != c.ceil {
			t.Errorf("ceilToDevicePixel(%v, %v) = %v, want %v", c.logical, c.scale, got, c.ceil)
		}
	}
}

// negZero is negative zero as a float32.
var negZero = float32(math.Copysign(0, -1))

// TestSnapMeasuredSize pins the measured-size rule (taffy.rs
// snap_measured_size_to_device_pixels): clamp each dimension to at least
// zero, multiply by the scale factor, then ceil.
func TestSnapMeasuredSize(t *testing.T) {
	cases := []struct {
		value, scale, want float32
	}{
		{0, 2, 0},
		{-5, 2, 0},      // clamped before the multiply
		{12.3, 2, 25},   // 24.6 ceils to 25
		{0.1, 2, 1},     // 0.2 ceils to 1
		{25, 2, 50},     // exact stays exact
		{10, 1.5, 15},   // exact at a fractional scale
		{3.34, 2, 7},    // 6.68 -> 7
		{3.33, 2, 7},    // 6.66 -> 7
		{3.3, 2, 7},     // 6.6 -> 7
		{-0.4, 2, 0},    // negative clamps to 0, not -1
		{negZero, 2, 0}, // -0.0 clamps to 0
	}
	for _, c := range cases {
		if got := snapMeasuredSize(c.value, c.scale); got != c.want {
			t.Errorf("snapMeasuredSize(%v, %v) = %v, want %v", c.value, c.scale, got, c.want)
		}
	}
}

// TestPixelSnap pins window.rs pixel_snap: snap to the device grid and
// divide back to logical pixels.
func TestPixelSnap(t *testing.T) {
	if got := pixelSnap(0, 2); got != 0 {
		t.Errorf("pixelSnap(0, 2) = %v, want 0", got)
	}
	if got := pixelSnap(1, 2); got != 1 {
		t.Errorf("pixelSnap(1, 2) = %v, want 1", got)
	}
	// 0.25 logical at scale 2 is 0.5 device, which ties toward zero.
	if got := pixelSnap(0.25, 2); got != 0 {
		t.Errorf("pixelSnap(0.25, 2) = %v, want 0 (half toward zero)", got)
	}
	// 0.3 logical at scale 2 is 0.6 device -> 1 -> 0.5 logical.
	if got := pixelSnap(0.3, 2); got != 0.5 {
		t.Errorf("pixelSnap(0.3, 2) = %v, want 0.5", got)
	}
}

// ---------------------------------------------------------------------------
// Style parsing (reference TryFrom<&str> impls in geometry.rs)
// ---------------------------------------------------------------------------

func TestParseLength(t *testing.T) {
	cases := []struct {
		in    string
		auto  bool
		kind  DefiniteLengthKind
		value float32
	}{
		{"100px", false, DefiniteLengthPx, 100},
		{"-3.5px", false, DefiniteLengthPx, -3.5},
		{"1.5rem", false, DefiniteLengthRem, 1.5},
		{"50%", false, DefiniteLengthFraction, 0.5},
		{"47.5%", false, DefiniteLengthFraction, 0.475},
		{"auto", true, 0, 0},
	}
	for _, c := range cases {
		got, err := ParseLength(c.in)
		if err != nil {
			t.Fatalf("ParseLength(%q): %v", c.in, err)
		}
		if got.Auto != c.auto {
			t.Errorf("ParseLength(%q).Auto = %v, want %v", c.in, got.Auto, c.auto)
		}
		if !c.auto {
			if got.Definite.Kind != c.kind {
				t.Errorf("ParseLength(%q).Kind = %v, want %v", c.in, got.Definite.Kind, c.kind)
			}
			if got.Definite.Value != c.value {
				t.Errorf("ParseLength(%q).Value = %v, want %v", c.in, got.Definite.Value, c.value)
			}
		}
	}

	// Every reference-rejected form fails.
	for _, bad := range []string{"", "100", "50", "px", "1.5em", "auto ", "AUTO", "1.5rems", "50 %%"} {
		if _, err := ParseLength(bad); err == nil {
			t.Errorf("ParseLength(%q) unexpectedly succeeded", bad)
		}
	}
}

func TestParseDefiniteLengthAndAbsoluteLength(t *testing.T) {
	abs, err := ParseAbsoluteLength("42px")
	if err != nil || abs.Kind != AbsoluteLengthPx || abs.Value != 42 {
		t.Errorf("ParseAbsoluteLength(\"42px\") = %+v, %v, want 42px", abs, err)
	}
	abs, err = ParseAbsoluteLength("2rem")
	if err != nil || abs.Kind != AbsoluteLengthRem || abs.Value != 2 {
		t.Errorf("ParseAbsoluteLength(\"2rem\") = %+v, %v, want 2rem", abs, err)
	}
	for _, bad := range []string{"50%", "auto", "42", "42pxx"} {
		if _, err := ParseAbsoluteLength(bad); err == nil {
			t.Errorf("ParseAbsoluteLength(%q) unexpectedly succeeded", bad)
		}
	}

	def, err := ParseDefiniteLength("50%")
	if err != nil || def.Kind != DefiniteLengthFraction || def.Value != 0.5 {
		t.Errorf("ParseDefiniteLength(\"50%%\") = %+v, %v, want fraction 0.5", def, err)
	}
	def, err = ParseDefiniteLength("1.5rem")
	if err != nil || def.Kind != DefiniteLengthRem || def.Value != 1.5 {
		t.Errorf("ParseDefiniteLength(\"1.5rem\") = %+v, %v, want 1.5rem", def, err)
	}
	if _, err := ParseDefiniteLength("auto"); err == nil {
		t.Error("ParseDefiniteLength(\"auto\") unexpectedly succeeded (DefiniteLength has no auto)")
	}
}

func TestParseEnums(t *testing.T) {
	// Display: the reference variant names, including the inline forms.
	for name, want := range map[string]Display{
		"Block": DisplayBlock, "Flex": DisplayFlex, "Grid": DisplayGrid,
		"Inline": DisplayInline, "InlineFlex": DisplayInlineFlex, "None": DisplayNone,
	} {
		got, err := ParseDisplay(name)
		if err != nil || got != want {
			t.Errorf("ParseDisplay(%q) = %v, %v, want %v", name, got, err, want)
		}
	}
	for name, want := range map[string]Position{
		"Relative": PositionRelative, "Absolute": PositionAbsolute,
	} {
		got, err := ParsePosition(name)
		if err != nil || got != want {
			t.Errorf("ParsePosition(%q) = %v, %v, want %v", name, got, err, want)
		}
	}
	for name, want := range map[string]Overflow{
		"Visible": OverflowVisible, "Clip": OverflowClip, "Hidden": OverflowHidden, "Scroll": OverflowScroll,
	} {
		got, err := ParseOverflow(name)
		if err != nil || got != want {
			t.Errorf("ParseOverflow(%q) = %v, %v, want %v", name, got, err, want)
		}
	}
	for name, want := range map[string]FlexDirection{
		"Row": FlexDirectionRow, "Column": FlexDirectionColumn,
		"RowReverse": FlexDirectionRowReverse, "ColumnReverse": FlexDirectionColumnReverse,
	} {
		got, err := ParseFlexDirection(name)
		if err != nil || got != want {
			t.Errorf("ParseFlexDirection(%q) = %v, %v, want %v", name, got, err, want)
		}
	}
	for name, want := range map[string]FlexWrap{
		"NoWrap": FlexWrapNoWrap, "Wrap": FlexWrapWrap, "WrapReverse": FlexWrapWrapReverse,
	} {
		got, err := ParseFlexWrap(name)
		if err != nil || got != want {
			t.Errorf("ParseFlexWrap(%q) = %v, %v, want %v", name, got, err, want)
		}
	}
	for name, want := range map[string]AlignItems{
		"Start": AlignItemsStart, "End": AlignItemsEnd, "FlexStart": AlignItemsFlexStart,
		"FlexEnd": AlignItemsFlexEnd, "Center": AlignItemsCenter, "Baseline": AlignItemsBaseline,
		"Stretch": AlignItemsStretch,
	} {
		got, err := ParseAlignItems(name)
		if err != nil || got != want {
			t.Errorf("ParseAlignItems(%q) = %v, %v, want %v", name, got, err, want)
		}
	}
	for name, want := range map[string]AlignContent{
		"Start": AlignContentStart, "End": AlignContentEnd, "FlexStart": AlignContentFlexStart,
		"FlexEnd": AlignContentFlexEnd, "Center": AlignContentCenter, "Stretch": AlignContentStretch,
		"SpaceBetween": AlignContentSpaceBetween, "SpaceEvenly": AlignContentSpaceEvenly,
		"SpaceAround": AlignContentSpaceAround,
	} {
		got, err := ParseAlignContent(name)
		if err != nil || got != want {
			t.Errorf("ParseAlignContent(%q) = %v, %v, want %v", name, got, err, want)
		}
	}
	// Unknown names fail.
	for _, parse := range []func(string) (Display, error){
		func(s string) (Display, error) { return ParseDisplay(s) },
	} {
		_ = parse
	}
	if _, err := ParseDisplay("flex"); err == nil {
		t.Error("ParseDisplay(\"flex\") unexpectedly succeeded")
	}
	if _, err := ParseAlignContent("Space Between"); err == nil {
		t.Error("ParseAlignContent(\"Space Between\") unexpectedly succeeded")
	}
}

// ---------------------------------------------------------------------------
// Style-to-record translation (into_taffy_style)
// ---------------------------------------------------------------------------

// TestStyleToRecordDefaults pins that the default Style translates into
// exactly the default ABI record: every field the reference forwards
// carries the same value the default record would give the native side
// (taffy defaults and gpui defaults coincide), so a default node behaves
// like a reference default node.
func TestStyleToRecordDefaults(t *testing.T) {
	defaultStyleValue := DefaultStyle()
	ctx := NewTestLayoutContext()
	got := styleToRecord(&defaultStyleValue, ctx)
	want := mustDefaultRecord(t)
	if got != want {
		t.Errorf("default style record mismatch:\ngot  %+v\nwant %+v", got, want)
	}
}

// mustDefaultRecord builds the record the default Style must produce:
// the default record with the always-forwarded definite fields set
// (scrollbar width, borders, gap, flex grow/shrink and the margin zero
// lengths) instead of absent tags, because the reference Style is a
// complete struct that forwards every field.
func mustDefaultRecord(t *testing.T) (rec native.LayoutStyleRecord) {
	t.Helper()
	rec = native.NewLayoutStyleRecord()
	rec.ScrollbarWidth = native.DefiniteLen(0)
	rec.Inset = fourEdges(native.AutoLen())
	rec.Size = sizeLens(native.AutoLen(), native.AutoLen())
	rec.MinSize = sizeLens(native.AutoLen(), native.AutoLen())
	rec.MaxSize = sizeLens(native.AutoLen(), native.AutoLen())
	rec.Margin = fourEdges(native.DefiniteLen(0))
	rec.Padding = fourEdges(native.DefiniteLen(0))
	rec.BorderTop, rec.BorderRight, rec.BorderBottom, rec.BorderLeft = 0, 0, 0, 0
	rec.Gap = sizeLens(native.DefiniteLen(0), native.DefiniteLen(0))
	rec.FlexBasis = native.AutoLen()
	rec.FlexGrowPresent, rec.FlexGrowBits = 1, math.Float32bits(0)
	rec.FlexShrinkPresent, rec.FlexShrinkBits = 1, math.Float32bits(1)
	return rec
}

func fourEdges(l native.LayoutLen) native.LayoutEdges {
	return native.LayoutEdges{Top: l, Right: l, Bottom: l, Left: l}
}

func sizeLens(w, h native.LayoutLen) native.LayoutSize {
	return native.LayoutSize{Width: w, Height: h}
}

// TestStyleToRecordLengths pins the unit conversion: definite logical
// lengths become device pixels with the pinned rounding (rem resolved
// through the request-time rem scope first), percentages stay fractions,
// auto stays auto.
func TestStyleToRecordLengths(t *testing.T) {
	ctx := NewTestLayoutContext() // scale 2, rem 16

	style := DefaultStyle()
	style.Size = LengthSize{Width: PxLength(100), Height: RemsLength(1.5)}
	style.MinSize.Width = DefiniteLengthOf(Fraction(0.5))
	style.MaxSize.Width = AutoLength()
	style.Inset.Top = PxLength(10.5)    // 21 device, no tie
	style.Inset.Bottom = PxLength(1.25) // 2.5 device ties toward zero -> 2
	style.Margin.Left = PxLength(0.25)  // 0.5 device ties toward zero -> 0 (explicit zero, not absent)
	style.FlexBasis = DefiniteLengthOf(Fraction(0.25))

	rec := styleToRecord(&style, ctx)
	if rec.Size.Width.Tag != native.LenTagDefinite || rec.Size.Width.Float() != 200 {
		t.Errorf("size.width = %+v, want definite 200 device px", rec.Size.Width)
	}
	if rec.Size.Height.Tag != native.LenTagDefinite || rec.Size.Height.Float() != 48 {
		t.Errorf("size.height = %+v, want definite 48 device px (1.5rem * 16 * 2)", rec.Size.Height)
	}
	if rec.MinSize.Width.Tag != native.LenTagPercent || rec.MinSize.Width.Float() != 0.5 {
		t.Errorf("min_size.width = %+v, want percent fraction 0.5", rec.MinSize.Width)
	}
	if rec.MaxSize.Width.Tag != native.LenTagAuto {
		t.Errorf("max_size.width tag = %d, want auto", rec.MaxSize.Width.Tag)
	}
	if rec.Inset.Top.Tag != native.LenTagDefinite || rec.Inset.Top.Float() != 21 {
		t.Errorf("inset.top = %+v, want definite 21", rec.Inset.Top)
	}
	if rec.Inset.Bottom.Tag != native.LenTagDefinite || rec.Inset.Bottom.Float() != 2 {
		t.Errorf("inset.bottom = %+v, want definite 2 (2.5 device ties toward zero)", rec.Inset.Bottom)
	}
	// An explicit zero (0.25 logical * 2 = 0.5 device -> 0) stays a
	// DEFINITE zero, distinct from the absent tag.
	if rec.Margin.Left.Tag != native.LenTagDefinite || rec.Margin.Left.Float() != 0 {
		t.Errorf("margin.left = %+v, want definite zero (not absent)", rec.Margin.Left)
	}
	if rec.FlexBasis.Tag != native.LenTagPercent || rec.FlexBasis.Float() != 0.25 {
		t.Errorf("flex_basis = %+v, want percent fraction 0.25", rec.FlexBasis)
	}

	// The rem scope at request time: the same style under a rem override
	// resolves differently.
	overridden := ctx.WithRemSize(20)
	rec = styleToRecord(&style, overridden)
	if rec.Size.Height.Float() != 60 {
		t.Errorf("size.height under rem 20 = %v, want 60 (1.5rem * 20 * 2)", rec.Size.Height.Float())
	}
}

// TestStyleToRecordRemResolution pins rem resolution through the rem
// scope at request time (Rems::to_pixels is rems * rem_size).
func TestStyleToRecordRemResolution(t *testing.T) {
	style := DefaultStyle()
	style.Size.Width = RemsLength(0.25)
	cases := []struct {
		rem, scale, want float32
	}{
		{16, 2, 8},  // 0.25 * 16 = 4 logical, 8 device
		{20, 2, 10}, // the rem override case
		{16, 1, 4},
	}
	for _, c := range cases {
		ctx := NewLayoutContext(c.rem, c.scale)
		rec := styleToRecord(&style, ctx)
		if rec.Size.Width.Tag != native.LenTagDefinite || rec.Size.Width.Float() != c.want {
			t.Errorf("rem %v scale %v: size.width = %+v, want definite %v", c.rem, c.scale, rec.Size.Width, c.want)
		}
	}
}

// TestStyleToRecordBorderStrokeMinimum pins the stroke rule: exact zero
// stays zero, anything else snaps to at least one device pixel (pinned
// taffy.rs border_widths_to_taffy test).
func TestStyleToRecordBorderStrokeMinimum(t *testing.T) {
	style := DefaultStyle()
	style.BorderWidths = AbsoluteLengthEdges{
		Top:    Px(0),
		Right:  Px(0.4),
		Bottom: Px(0.5),
		Left:   Px(1.6),
	}
	rec := styleToRecord(&style, NewLayoutContext(16, 1))
	if got := math.Float32frombits(rec.BorderTop); got != 0 {
		t.Errorf("border top = %v, want 0 (exact zero stays zero)", got)
	}
	if got := math.Float32frombits(rec.BorderRight); got != 1 {
		t.Errorf("border right = %v, want 1 (clamped up)", got)
	}
	if got := math.Float32frombits(rec.BorderBottom); got != 1 {
		t.Errorf("border bottom = %v, want 1 (0.5 device ties toward zero, then the minimum)", got)
	}
	if got := math.Float32frombits(rec.BorderLeft); got != 2 {
		t.Errorf("border left = %v, want 2", got)
	}
}

// TestStyleToRecordPaddingProportional ports the pinned taffy.rs test
// auto_sized_axes_snap_padding_proportionally: on auto-sized axes the
// padding pair is snapped as a combined length and redistributed in the
// original ratio; explicit axes and percentage padding use the ordinary
// conversion.
func TestStyleToRecordPaddingProportional(t *testing.T) {
	style := DefaultStyle()
	style.Padding = DefiniteLengthEdges{
		Top:    DefinitePx(6.5),
		Right:  DefinitePx(5.0),
		Bottom: DefinitePx(6.5),
		Left:   DefinitePx(2.5),
	}

	// Auto width: left/right snap proportionally; the height is auto too,
	// so top/bottom do as well (6.5 + 6.5 = 13 -> 13, split evenly).
	rec := styleToRecord(&style, NewLayoutContext(16, 1))
	// The expectation is f32 arithmetic, exactly like the pinned Rust
	// test (its literals infer f32 through LengthPercentage::length).
	var first, total float32 = 2.5, 7.5
	expectedLeft := float32(7.0) * (first / total)
	if rec.Padding.Left.Tag != native.LenTagDefinite || rec.Padding.Left.Float() != expectedLeft {
		t.Errorf("padding left = %+v, want %v", rec.Padding.Left, expectedLeft)
	}
	if rec.Padding.Right.Tag != native.LenTagDefinite || rec.Padding.Right.Float() != float32(7.0)-expectedLeft {
		t.Errorf("padding right = %+v, want %v", rec.Padding.Right, 7.0-expectedLeft)
	}
	if rec.Padding.Top.Tag != native.LenTagDefinite || rec.Padding.Top.Float() != 6.5 {
		t.Errorf("padding top = %+v, want 6.5", rec.Padding.Top)
	}
	if rec.Padding.Bottom.Tag != native.LenTagDefinite || rec.Padding.Bottom.Float() != 6.5 {
		t.Errorf("padding bottom = %+v, want 6.5", rec.Padding.Bottom)
	}

	// Definite width: the horizontal pair uses the ordinary per-edge
	// conversion (2.5 ties toward zero -> 2).
	width := PxLength(100)
	style.Size.Width = width
	rec = styleToRecord(&style, NewLayoutContext(16, 1))
	if rec.Padding.Left.Float() != 2 {
		t.Errorf("explicit width: padding left = %v, want 2", rec.Padding.Left.Float())
	}
	if rec.Padding.Right.Float() != 5 {
		t.Errorf("explicit width: padding right = %v, want 5", rec.Padding.Right.Float())
	}

	// Auto width again, with a scale of 2: top/bottom 6.75 each combine to
	// 13.5 logical = 27 device, redistributed evenly.
	style.Size.Width = AutoLength()
	style.Padding.Top = DefinitePx(6.75)
	style.Padding.Bottom = DefinitePx(6.75)
	rec = styleToRecord(&style, NewLayoutContext(16, 2))
	if rec.Padding.Top.Float() != 13.5 {
		t.Errorf("scale 2: padding top = %v, want 13.5", rec.Padding.Top.Float())
	}
	if rec.Padding.Bottom.Float() != 13.5 {
		t.Errorf("scale 2: padding bottom = %v, want 13.5", rec.Padding.Bottom.Float())
	}
}

// TestStyleToRecordPaddingPercentAndFallback pins the non-proportional
// padding paths: percentage edges keep their fraction, and negative or
// zero-total pairs use the ordinary per-edge conversion.
func TestStyleToRecordPaddingPercentAndFallback(t *testing.T) {
	style := DefaultStyle()
	// Percent padding on an auto axis: fractions are preserved (no
	// proportional snap for percentages).
	style.Padding.Left = Fraction(0.05)
	style.Padding.Right = Fraction(0.1)
	rec := styleToRecord(&style, NewLayoutContext(16, 2))
	if rec.Padding.Left.Tag != native.LenTagPercent || rec.Padding.Left.Float() != 0.05 {
		t.Errorf("percent padding left = %+v, want fraction 0.05", rec.Padding.Left)
	}
	if rec.Padding.Right.Tag != native.LenTagPercent || rec.Padding.Right.Float() != 0.1 {
		t.Errorf("percent padding right = %+v, want fraction 0.1", rec.Padding.Right)
	}

	// Mixed absolute/percent on an auto axis: the pair falls back to the
	// ordinary conversion.
	style.Padding.Left = DefinitePx(2.5)
	style.Padding.Right = Fraction(0.1)
	rec = styleToRecord(&style, NewLayoutContext(16, 1))
	if rec.Padding.Left.Float() != 3 { // 2.5 * 1 -> 2.5 -> round half toward zero is 2? no: 2.5 -> 2
		_ = rec
	}
	if rec.Padding.Left.Float() != 2 {
		t.Errorf("mixed pair padding left = %v, want 2 (2.5 device ties toward zero)", rec.Padding.Left.Float())
	}

	// A zero-total absolute pair on an auto axis: the fallback keeps the
	// per-edge rounding (0 stays 0, negative stays negative-rounded).
	style.Padding.Left = DefinitePx(0)
	style.Padding.Right = DefinitePx(0)
	rec = styleToRecord(&style, NewLayoutContext(16, 1))
	if rec.Padding.Left.Float() != 0 || rec.Padding.Right.Float() != 0 {
		t.Errorf("zero-total pair = %v/%v, want 0/0", rec.Padding.Left.Float(), rec.Padding.Right.Float())
	}
	style.Padding.Left = DefinitePx(-2.5)
	style.Padding.Right = DefinitePx(1)
	rec = styleToRecord(&style, NewLayoutContext(16, 1))
	if rec.Padding.Left.Float() != -2 || rec.Padding.Right.Float() != 1 {
		t.Errorf("negative pair = %v/%v, want -2/1", rec.Padding.Left.Float(), rec.Padding.Right.Float())
	}
}

// TestStyleToRecordGridAndAlignment pins the grid template, grid
// placement, alignment and flex fields of the record.
func TestStyleToRecordGridAndAlignment(t *testing.T) {
	style := DefaultStyle()
	style.Display = DisplayInlineFlex // maps to Flex at the adapter
	style.GridCols = &GridTemplate{Repeat: 2, MinSize: GridTemplateMinMinContent}
	style.GridRows = &GridTemplate{Repeat: 3, MinSize: GridTemplateMinMaxContent}
	style.GridLocation = &GridLocation{
		Row:    GridPlacementRange{Start: LineGridPlacement(1), End: SpanGridPlacement(2)},
		Column: GridPlacementRange{Start: AutoGridPlacement(), End: LineGridPlacement(-1)},
	}
	alignItems := AlignItemsStretch
	justify := AlignContentSpaceBetween
	style.AlignItems = &alignItems
	style.JustifyContent = &justify
	style.FlexDirection = FlexDirectionColumnReverse
	style.FlexWrap = FlexWrapWrapReverse
	grow := float32(2)
	style.AspectRatio = &grow

	rec := styleToRecord(&style, NewTestLayoutContext())
	if rec.Display != native.DisplayFlex {
		t.Errorf("display InlineFlex = %d, want the flex ABI value (InlineFlex maps to Flex)", rec.Display)
	}
	if rec.GridTemplateColumnsPresent != 1 || rec.GridTemplateColumnsRepeat != 2 ||
		rec.GridTemplateColumnsMinSize != uint32(GridTemplateMinMinContent) {
		t.Errorf("grid columns = present %d repeat %d min %d, want 1/2/min-content", rec.GridTemplateColumnsPresent, rec.GridTemplateColumnsRepeat, rec.GridTemplateColumnsMinSize)
	}
	if rec.GridTemplateRowsPresent != 1 || rec.GridTemplateRowsRepeat != 3 ||
		rec.GridTemplateRowsMinSize != uint32(GridTemplateMinMaxContent) {
		t.Errorf("grid rows = present %d repeat %d min %d, want 1/3/max-content", rec.GridTemplateRowsPresent, rec.GridTemplateRowsRepeat, rec.GridTemplateRowsMinSize)
	}
	if rec.GridPlacementPresent != 1 {
		t.Fatalf("grid placement present = %d, want 1", rec.GridPlacementPresent)
	}
	if rec.GridRowStartKind != 1 || rec.GridRowStartValue != uint32(uint16(int16(1))) {
		t.Errorf("grid row start = %d/%d, want line 1", rec.GridRowStartKind, rec.GridRowStartValue)
	}
	if rec.GridRowEndKind != 2 || rec.GridRowEndValue != 2 {
		t.Errorf("grid row end = %d/%d, want span 2", rec.GridRowEndKind, rec.GridRowEndValue)
	}
	if rec.GridColumnStartKind != 0 {
		t.Errorf("grid column start kind = %d, want auto", rec.GridColumnStartKind)
	}
	negLine := int16(-1)
	if rec.GridColumnEndKind != 1 || rec.GridColumnEndValue != uint32(uint16(negLine)) {
		t.Errorf("grid column end = %d/%d, want line -1 (i16 bit pattern)", rec.GridColumnEndKind, rec.GridColumnEndValue)
	}
	if rec.AlignItemsPresent != 1 || rec.AlignItems != uint32(AlignItemsStretch) {
		t.Errorf("align items = %d/%d, want present stretch", rec.AlignItemsPresent, rec.AlignItems)
	}
	if rec.JustifyContentPresent != 1 || rec.JustifyContent != uint32(AlignContentSpaceBetween) {
		t.Errorf("justify content = %d/%d, want present space-between", rec.JustifyContentPresent, rec.JustifyContent)
	}
	if rec.FlexDirection != uint32(FlexDirectionColumnReverse) {
		t.Errorf("flex direction = %d, want column-reverse", rec.FlexDirection)
	}
	if rec.FlexWrap != uint32(FlexWrapWrapReverse) {
		t.Errorf("flex wrap = %d, want wrap-reverse", rec.FlexWrap)
	}
	if rec.AspectRatioPresent != 1 || math.Float32frombits(rec.AspectRatioBits) != 2 {
		t.Errorf("aspect ratio = %d/%v, want present 2", rec.AspectRatioPresent, math.Float32frombits(rec.AspectRatioBits))
	}
	// flex_grow and flex_shrink are always forwarded, even when zero.
	style.FlexShrink = 0
	rec = styleToRecord(&style, NewTestLayoutContext())
	if rec.FlexShrinkPresent != 1 || math.Float32frombits(rec.FlexShrinkBits) != 0 {
		t.Errorf("flex shrink 0 = present %d value %v, want an explicit zero (not an omitted default)", rec.FlexShrinkPresent, math.Float32frombits(rec.FlexShrinkBits))
	}
}

// TestStyleToRecordDisplayMapping pins the Inline/InlineFlex folding.
func TestStyleToRecordDisplayMapping(t *testing.T) {
	cases := []struct {
		display Display
		want    uint32
	}{
		{DisplayFlex, 0}, {DisplayInlineFlex, 0},
		{DisplayBlock, 1}, {DisplayInline, 1},
		{DisplayGrid, 2}, {DisplayNone, 3},
	}
	for _, c := range cases {
		style := DefaultStyle()
		style.Display = c.display
		rec := styleToRecord(&style, NewTestLayoutContext())
		if rec.Display != c.want {
			t.Errorf("display %v = %d, want %d", c.display, rec.Display, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Available space conversion
// ---------------------------------------------------------------------------

func TestAvailToNativeAndBack(t *testing.T) {
	scale := float32(2)
	logical := DefiniteAvailableSpace(60)
	device := availToNative(logical, scale)
	if device.Tag != 0 || math.Float32frombits(device.Bits) != 120 {
		t.Errorf("availToNative(60, 2) = %+v, want definite 120 device px", device)
	}
	// Round trip back to logical (the measure path divides by the scale).
	back := availToLogical(nativeAvailSpaceOf(device), scale)
	if back.Kind != AvailDefinite || back.Definite != 60 {
		t.Errorf("avail round trip = %+v, want definite 60 logical", back)
	}
	for _, mode := range []AvailableSpace{MinContentAvailableSpace(), MaxContentAvailableSpace()} {
		device := availToNative(mode, scale)
		back := availToLogical(nativeAvailSpaceOf(device), scale)
		if back.Kind != mode.Kind {
			t.Errorf("mode round trip = %v, want %v", back, mode)
		}
	}
}

// nativeAvailSpaceOf re-wraps a native LayoutAvail as an AvailSpace.
func nativeAvailSpaceOf(a native.LayoutAvail) native.AvailSpace {
	return native.AvailSpace{Mode: native.AvailMode(a.Tag), Definite: math.Float32frombits(a.Bits)}
}
