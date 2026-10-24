package gpui

import (
	"math"
	"testing"
)

// TestClampLineWidth pins Chromium's computed border-width rule
// (browser-core ticket 07): the width is an integer of CSS px —
// anything strictly between 0 and 1 becomes 1 (a sub-pixel hairline
// keeps one pixel), anything at or above 1 floors, and an exact zero
// stays zero (StyleBuilderConverter::ConvertBorderWidth ->
// ClampLineWidth, core/css/resolver/style_builder_converter.cc:
// 1971-1987).
func TestClampLineWidth(t *testing.T) {
	cases := []struct {
		in, want float32
	}{
		{0, 0},
		{0.5, 1},
		{0.999, 1},
		{1, 1},
		{1.3, 1},
		{1.999, 1},
		{2, 2},
		{2.6, 2},
		{5, 5},
		{-0.5, 0},
	}
	for _, c := range cases {
		if got := clampLineWidth(c.in); got != c.want {
			t.Errorf("clampLineWidth(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestClampLineWidthToDeviceCarriesTheFlooredCSSWidth pins the record
// translation (browser-core 07): the device value is the floored CSS
// width times the scale factor, UNROUNDED — a 1px border at a 1.75
// device scale lays out at exactly 1 CSS px (1.75 device), which the
// measured Chrome 155 record confirms (getBoundingClientRect insets
// the content by 1 CSS px and the painted strip is an antialiased
// 1.75 device px). The pin's round_stroke_to_device_pixel would have
// made the layout inset 2 device px there.
func TestClampLineWidthToDeviceCarriesTheFlooredCSSWidth(t *testing.T) {
	cases := []struct {
		logical, scale, want float32
	}{
		{1, 1.75, 1.75},
		{1, 2, 2},
		{1.3, 2, 2},
		{0.5, 1, 1},
		{0.5, 2, 2},
		{2.6, 1, 2},
		{0, 2, 0},
	}
	for _, c := range cases {
		if got := clampLineWidthToDevice(c.logical, c.scale); got != c.want {
			t.Errorf("clampLineWidthToDevice(%v, %v) = %v, want %v", c.logical, c.scale, got, c.want)
		}
	}
}

// TestStyleToRecordBordersUseTheFloorRule reads the translated record
// back through the native mirror: every border edge carries the
// clamp-lined width scaled without device rounding.
func TestStyleToRecordBordersUseTheFloorRule(t *testing.T) {
	ctx := NewLayoutContext(DefaultRemSize, 1.75)
	style := DefaultStyle()
	style.BorderWidths = AbsoluteLengthEdges{
		Top:    Px(1),
		Right:  Px(1.3),
		Bottom: Px(0.5),
		Left:   Px(2.6),
	}
	record := styleToRecord(&style, ctx)
	fromBits := func(bits uint32) float32 { return math.Float32frombits(bits) }
	if got, want := fromBits(record.BorderTop), float32(1.75); got != want {
		t.Errorf("border top = %v, want %v (1 CSS px at scale 1.75)", got, want)
	}
	if got, want := fromBits(record.BorderRight), float32(1.75); got != want {
		t.Errorf("border right = %v, want %v (1.3px floors to 1 CSS px)", got, want)
	}
	if got, want := fromBits(record.BorderBottom), float32(1.75); got != want {
		t.Errorf("border bottom = %v, want %v (0.5px keeps 1 CSS px)", got, want)
	}
	if got, want := fromBits(record.BorderLeft), float32(3.5); got != want {
		t.Errorf("border left = %v, want %v (2.6px floors to 2 CSS px)", got, want)
	}
}

// TestSnapBorderWidthsFloorsThenStrokeSnaps pins the paint side
// (browser-core 07): the border quad's edges are clamp-lined first (the
// integer CSS px), then the pinned stroke rule converts to the whole
// device thickness a hard-edged strip needs — a 1px border at 1.75
// paints 2 device px (the nearest hard approximation of Chrome's
// antialiased 1.75), and a 0.5px hairline at 100% paints 1.
func TestSnapBorderWidthsFloorsThenStrokeSnaps(t *testing.T) {
	edges := Edges{Top: 1, Right: 1.3, Bottom: 0.5, Left: 2.6}
	snapped := snapBorderWidths(edges, 1.75)
	if got, want := snapped.Top, float32(2); got != want {
		t.Errorf("top = %v, want %v (1 CSS px x 1.75 rounds to 2 device px)", got, want)
	}
	if got, want := snapped.Right, float32(2); got != want {
		t.Errorf("right = %v, want %v", got, want)
	}
	if got, want := snapped.Bottom, float32(2); got != want {
		t.Errorf("bottom = %v, want %v (0.5px keeps 1 CSS px, 1.75 device rounded)", got, want)
	}
	if got, want := snapped.Left, float32(3); got != want {
		t.Errorf("left = %v, want %v (2.6px floors to 2 CSS px, 3.5 device ties toward zero)", got, want)
	}
	hairline := snapBorderWidths(Edges{Top: 0.5}, 1)
	if got, want := hairline.Top, float32(1); got != want {
		t.Errorf("hairline = %v, want %v (one device pixel at scale 1)", got, want)
	}
}
