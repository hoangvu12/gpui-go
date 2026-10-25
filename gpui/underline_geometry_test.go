package gpui

// Ticket 06 (browser-core): underlines use Chrome's thickness and
// offset. The rules (verified in the Chromium checkout):
//
//   - auto thickness = font size / 10, floored at 1 CSS px:
//     third_party/blink/renderer/core/style/text_decoration_thickness.cc
//     :29-31 (TextDecorationThickness::Resolve's auto path) plus the
//     floor in core/paint/text_decoration_info.cc:715-719
//     (TextDecorationInfo::ComputeThickness's std::max(1.0f, thickness));
//   - the underline's top sits max(1, ceil(thickness / 2)) below the
//     alphabetic baseline: core/layout/text_decoration_offset.cc:16-35
//     (ComputeUnderlineOffsetAuto's gap) — the offset is measured from
//     the line top as ascent + gap, i.e. baseline + gap, and the
//     decoration rect starts there with height = thickness
//     (core/paint/text_decoration_info.cc:496-503, DecorationGeometry's
//     rect from local_origin + line_offset).
//
// A divergence toward CSS over the pin's 1px at baseline +
// descent*0.618 (rich_paint.go's recorded note).

import (
	"math"
	"strconv"
	"testing"
)

// TestUnderlineAutoThicknessAndOffset pins the formulas.
func TestUnderlineAutoThicknessAndOffset(t *testing.T) {
	cases := []struct {
		fontSize      float32
		wantThickness float32
		wantOffset    float32
	}{
		// max(1, size/10) and max(1, ceil(thickness/2)).
		{8, 1, 1},
		{16, 1.6, 1}, // ceil(1.6/2) = ceil(0.8) = 1
		{24, 2.4, 2}, // ceil(2.4/2) = ceil(1.2) = 2
		{40, 4, 2},
		{50, 5, 3}, // ceil(5/2) = 3
	}
	for _, c := range cases {
		style := UnderlineStyle{AutoThickness: true}
		thickness := resolvedDecorationThickness(style, c.fontSize)
		if thickness != c.wantThickness {
			t.Errorf("size %v: auto thickness = %v, want %v", c.fontSize, thickness, c.wantThickness)
		}
		offset := chromeUnderlineOffset(thickness)
		if offset != c.wantOffset {
			t.Errorf("size %v: underline offset = %v, want %v", c.fontSize, offset, c.wantOffset)
		}
	}
	// An explicit thickness passes through; the pin's stroke snapping
	// (underlineFor) applies downstream.
	explicit := UnderlineStyle{Thickness: 3}
	if got := resolvedDecorationThickness(explicit, 40); got != 3 {
		t.Errorf("explicit thickness = %v, want 3", got)
	}
}

// TestTextElementUnderlineRecordFollowsChrome draws one underlined
// text child (the real text system in the test window, scale 2) and
// checks the scene's underline record: the stroke-snapped auto
// thickness and the origin at baseline + max(1, ceil(thickness/2)),
// with the baseline derived from the same shaped summary the painter
// reads. Also pins that a plain text child paints its underline at all
// (the pin's document-carried runs; a port gap this ticket fixes —
// before it only StyledText painted decorations).
func TestTextElementUnderlineRecordFollowsChrome(t *testing.T) {
	const text = "underlined words"
	for _, fontSize := range []float32{16, 40} {
		t.Run(fontSizeLabel(fontSize), func(t *testing.T) {
			ts, err := DefaultTextSystem()
			if err != nil {
				t.Fatalf("text system: %v", err)
			}
			line, err := ts.ShapeText(TextLayoutRequest{
				Text:     text,
				FontSize: fontSize,
				Runs:     []TextRun{{Len: len(text), Font: DefaultFontDescriptor()}},
			})
			if err != nil {
				t.Fatalf("shaping: %v", err)
			}
			t.Cleanup(func() { _ = line.Dispose() })
			summary, err := line.Summary()
			if err != nil {
				t.Fatalf("summary: %v", err)
			}

			ta := NewTestApp()
			app := ta.App()
			window := NewTestWindow(app, Size{Width: 600, Height: 300})
			t.Cleanup(func() { CloseTestWindow(window) })
			if err := UseRealTextSystem(window); err != nil {
				t.Fatalf("real text system: %v", err)
			}
			window.SetRootView(transformRoot{build: func() AnyElement {
				return Div().Flex().Underline().TextSize(Px(fontSize)).Child(text).IntoElement()
			}})
			scene, err := DrawWindowFrame(window)
			if err != nil {
				t.Fatalf("DrawWindowFrame: %v", err)
			}
			records, err := scene.Underlines()
			if err != nil {
				t.Fatalf("scene underlines: %v", err)
			}
			if len(records) != 1 {
				t.Fatalf("underline records = %d, want 1 (a plain text child paints its underline)", len(records))
			}
			record := records[0]

			// The element sits at the origin, so the baseline is the row's.
			// Browser-parity D1: the default line height is CSS `normal` —
			// the font's line spacing (rounded ascent + rounded descent +
			// rounded line gap) — and the row's baseline is the ROUNDED
			// ascent with the floored half leading below it (Chrome's
			// CalculateLeadingSpace, core/layout/inline/line_utils.cc:34-43;
			// Segoe UI has line gap 0, so the leading is 0 and the
			// baseline is the rounded ascent: 17 at 16px, 43 at 40px).
			lineHeight, err := ts.NormalLineHeight(DefaultFontDescriptor(), fontSize)
			if err != nil {
				t.Fatalf("normal line height: %v", err)
			}
			roundedAscent := roundToNearest(summary.Ascent)
			roundedDescent := roundToNearest(summary.Descent)
			ascentLeading := float32(math.Floor(float64((lineHeight - roundedAscent - roundedDescent) / 2)))
			baseline := roundedAscent + ascentLeading

			wantThickness := snapStroke(max32(1, fontSize/10), TestScaleFactor)
			if record.Thickness != wantThickness || record.Bounds.Size.Height != wantThickness {
				t.Errorf("underline thickness/height = %v/%v, want %v (max(1, %v/10) stroke-snapped at scale %v)",
					record.Thickness, record.Bounds.Size.Height, wantThickness, fontSize, TestScaleFactor)
			}
			wantY := roundToDevicePixel(baseline+chromeUnderlineOffset(max32(1, fontSize/10)), TestScaleFactor)
			if record.Bounds.Origin.Y != wantY {
				t.Errorf("underline y = %v, want %v (baseline %v + Chrome's offset %v, device-snapped)",
					record.Bounds.Origin.Y, wantY, baseline, chromeUnderlineOffset(max32(1, fontSize/10)))
			}
			// The old pin geometry (baseline + descent*0.618) must not
			// match: this is the divergence the ticket records.
			oldY := roundToDevicePixel(baseline+summary.Descent*0.618, TestScaleFactor)
			if record.Bounds.Origin.Y == oldY && wantY != oldY {
				t.Errorf("underline y = %v equals the pin's descent*0.618 geometry %v; the Chrome offset is not applied", record.Bounds.Origin.Y, oldY)
			}
			if record.Wavy {
				t.Errorf("a plain underline is not wavy")
			}
		})
	}
}

// fontSizeLabel renders a font size for subtest names.
func fontSizeLabel(size float32) string {
	return strconv.FormatFloat(float64(size), 'f', -1, 32) + "px"
}
