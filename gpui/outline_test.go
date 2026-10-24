package gpui

import (
	"testing"
)

// The outline style's paint geometry (browser-core ticket 48; CSS
// css-ui-4 §outline, ported from Chromium core/paint/
// outline_painter.cc @ 0d4dcd4f). The outline is a frame painted
// above the content: a solid or dashed outline's ink band is
// [offset, offset+width] around the border box (ComplexOutlinePainter
// fills between the outer path outset offset+width and the inner path
// outset offset), and the auto focus ring is two strokes of width 2
// whose ink is [offset-1, offset+1] in the inner colour under
// [offset+1, offset+2] in the outer colour (PaintFocusRing:878-916).

// outlineTestBox is the shared element: a 160x80 border box at
// (100, 50).
func outlineTestBox() Bounds {
	return Bounds{Origin: Point{X: 100, Y: 50}, Size: Size{Width: 160, Height: 80}}
}

// outsetBox returns the test box outset by amount on every side.
func outsetBox(amount float32) Bounds {
	return dilate(outlineTestBox(), amount)
}

func TestOutlineSolidFrameQuadGeometry(t *testing.T) {
	green := RingColorOf(SolidBackground(Rgb(0x22c55e)))
	tests := []struct {
		name       string
		style      OutlineStyle
		wantBounds Bounds
		wantRadii  Corners
	}{
		{
			name:       "offset 6 width 3",
			style:      OutlineStyle{Kind: OutlineSolid, Width: 3, Color: green, Offset: 6},
			wantBounds: outsetBox(9),
			wantRadii:  Corners{},
		},
		{
			name:       "offset 0 width 2",
			style:      OutlineStyle{Kind: OutlineSolid, Width: 2, Color: green},
			wantBounds: outsetBox(2),
			wantRadii:  Corners{},
		},
		{
			name:       "offset -6 width 3 draws inside",
			style:      OutlineStyle{Kind: OutlineSolid, Width: 3, Color: green, Offset: -6},
			wantBounds: outsetBox(-3),
			wantRadii:  Corners{},
		},
		{
			name:       "offset 4 width 1",
			style:      OutlineStyle{Kind: OutlineSolid, Width: 1, Color: green, Offset: 4},
			wantBounds: outsetBox(5),
			wantRadii:  Corners{},
		},
		{
			name:       "rounded radius 16 offset 4 width 3",
			style:      OutlineStyle{Kind: OutlineSolid, Width: 3, Color: green, Offset: 4},
			wantBounds: outsetBox(7),
			wantRadii:  Corners{TopLeft: 16 + 7, TopRight: 16 + 7, BottomRight: 16 + 7, BottomLeft: 16 + 7},
		},
	}
	for _, tt := range tests {
		radii := Corners{TopLeft: 16, TopRight: 16, BottomRight: 16, BottomLeft: 16}
		if tt.wantRadii == (Corners{}) {
			radii = Corners{}
		}
		quads := tt.style.paintQuads(outlineTestBox(), radii, Hsla{}, Edges{})
		if len(quads) != 1 {
			t.Fatalf("%s: got %d quads, want 1", tt.name, len(quads))
		}
		quad := quads[0]
		if quad.Bounds != tt.wantBounds {
			t.Errorf("%s: bounds = %+v, want %+v", tt.name, quad.Bounds, tt.wantBounds)
		}
		if quad.CornerRadii != tt.wantRadii {
			t.Errorf("%s: radii = %+v, want %+v", tt.name, quad.CornerRadii, tt.wantRadii)
		}
		wantWidth := tt.style.Width
		if quad.BorderWidths != (Edges{Top: wantWidth, Right: wantWidth, Bottom: wantWidth, Left: wantWidth}) {
			t.Errorf("%s: border widths = %+v, want %v all around", tt.name, quad.BorderWidths, wantWidth)
		}
		if quad.BorderStyle != BorderStyleSolid {
			t.Errorf("%s: border style = %v, want solid", tt.name, quad.BorderStyle)
		}
		if quad.BorderColor != green.Color {
			t.Errorf("%s: border color = %+v, want the outline colour", tt.name, quad.BorderColor)
		}
		if !quad.Background.IsTransparent() {
			t.Errorf("%s: quad interior must be transparent, got %+v", tt.name, quad.Background)
		}
	}
}

// TestOutlineDashedFrameQuadPortsTheKind: the dashed kind lowers to
// the same frame with the native dashed border style (the scene's
// border records are solid or dashed; dotted, double, groove, ridge,
// inset and outset have no native spelling — a recorded limitation).
func TestOutlineDashedFrameQuadPortsTheKind(t *testing.T) {
	style := OutlineStyle{Kind: OutlineDashed, Width: 2, Color: RingColorOf(SolidBackground(Rgb(0x334155))), Offset: 4}
	quads := style.paintQuads(outlineTestBox(), Corners{}, Hsla{}, Edges{})
	if len(quads) != 1 {
		t.Fatalf("got %d quads, want 1", len(quads))
	}
	if quads[0].BorderStyle != BorderStyleDashed {
		t.Errorf("border style = %v, want dashed", quads[0].BorderStyle)
	}
	if quads[0].Bounds != outsetBox(6) {
		t.Errorf("bounds = %+v, want the frame outset by offset+width", quads[0].Bounds)
	}
}

// TestOutlineNoneAndZeroWidthPaintNothing: outline-style: none never
// paints, a zero width (outline-width: 0) paints nothing, and the
// unmaterialized auto kind — the CSS initial value, "not authored" —
// paints nothing either: only the div's computeStyleInternal turns it
// into the focus ring (so elements that paint a Style without that
// computation, like images, draw no ring).
func TestOutlineNoneAndZeroWidthPaintNothing(t *testing.T) {
	for _, style := range []OutlineStyle{
		{Kind: OutlineNone, Width: 3},
		{Kind: OutlineSolid, Width: 0},
		{Kind: OutlineDashed, Width: 0},
		{Kind: OutlineAuto, Width: 3},
	} {
		if quads := style.paintQuads(outlineTestBox(), Corners{}, Hsla{}, Edges{}); len(quads) != 0 {
			t.Errorf("kind %v width %v: got %d quads, want none", style.Kind, style.Width, len(quads))
		}
	}
}

// TestOutlineCurrentColorResolves: a non-explicit outline colour is
// currentColor (css-ui-4's outline-color initial).
func TestOutlineCurrentColorResolves(t *testing.T) {
	current := Rgb(0x0ea5e9)
	style := OutlineStyle{Kind: OutlineSolid, Width: 2, Offset: 4}
	quads := style.paintQuads(outlineTestBox(), Corners{}, current, Edges{})
	if len(quads) != 1 {
		t.Fatalf("got %d quads, want 1", len(quads))
	}
	if quads[0].BorderColor != SolidBackground(current) {
		t.Errorf("border color = %+v, want currentColor %+v", quads[0].BorderColor, current)
	}
}

// TestOutlineFocusRingQuads: the materialized default focus ring
// (Chromium PaintFocusRing, outline_painter.cc): one width-2 border
// frame centred on the border box — the band [offset-1, offset+1],
// 1px inside the border edge and 1px outside — in the outline colour
// or the default #101010 (the recorded Chrome's resolved
// -webkit-focus-ring-color; web_view_impl.cc:3836's renderer-
// preference override — the in-tree default is the amber #E59700 of
// the newer two-tone ring, a recorded divergence toward upstream
// main). The corner radii follow the element's radius plus the
// offset with a 3px floor (GetFocusRingCornerRadii:764-781) — square
// elements get the 3px rounding too.
func TestOutlineFocusRingQuads(t *testing.T) {
	t.Run("borderless, square", func(t *testing.T) {
		quads := OutlineStyle{Kind: OutlineFocusRing}.paintQuads(outlineTestBox(), Corners{}, Hsla{}, Edges{})
		if len(quads) != 1 {
			t.Fatalf("got %d quads, want the single focus ring stroke", len(quads))
		}
		quad := quads[0]
		if quad.Bounds != outsetBox(1) {
			t.Errorf("bounds = %+v, want the stroke centred on the border box (outset 1)", quad.Bounds)
		}
		if quad.BorderColor != SolidBackground(Rgb(0x101010)) {
			t.Errorf("colour = %+v, want the default focus ring colour #101010", quad.BorderColor)
		}
		if quad.BorderWidths != (Edges{Top: 2, Right: 2, Bottom: 2, Left: 2}) {
			t.Errorf("ring widths = %+v, want 2 all around", quad.BorderWidths)
		}
		if quad.CornerRadii != (Corners{TopLeft: 4, TopRight: 4, BottomRight: 4, BottomLeft: 4}) {
			t.Errorf("ring radii = %+v, want the 3px default grown by the half stroke", quad.CornerRadii)
		}
		if quad.BorderStyle != BorderStyleSolid {
			t.Errorf("ring style = %v, want solid", quad.BorderStyle)
		}
	})

	t.Run("explicit outline colour is the ring", func(t *testing.T) {
		red := RingColorOf(SolidBackground(Rgb(0xdc2626)))
		quads := OutlineStyle{Kind: OutlineFocusRing, Color: red}.paintQuads(outlineTestBox(), Corners{}, Hsla{}, Edges{})
		if quads[0].BorderColor != red.Color {
			t.Errorf("colour = %+v, want the outline colour", quads[0].BorderColor)
		}
	})

	t.Run("a border pulls the ring one pixel onto it", func(t *testing.T) {
		quads := OutlineStyle{Kind: OutlineFocusRing}.paintQuads(outlineTestBox(), Corners{}, Hsla{}, Edges{Top: 2, Right: 2, Bottom: 2, Left: 2})
		if quads[0].Bounds != outlineTestBox() {
			t.Errorf("bounds = %+v, want the border box itself (the stroke centred on it)", quads[0].Bounds)
		}
	})

	t.Run("radii follow the element radius plus the offset, floored at 3", func(t *testing.T) {
		radii := Corners{TopLeft: 16, TopRight: 16, BottomRight: 16, BottomLeft: 16}
		quads := OutlineStyle{Kind: OutlineFocusRing, Offset: 2}.paintQuads(outlineTestBox(), radii, Hsla{}, Edges{})
		if quads[0].CornerRadii != (Corners{TopLeft: 16 + 2 + 1, TopRight: 16 + 2 + 1, BottomRight: 16 + 2 + 1, BottomLeft: 16 + 2 + 1}) {
			t.Errorf("ring radii = %+v, want radius+offset+half-stroke", quads[0].CornerRadii)
		}
		// The stroke sits on the border box outset by the offset.
		if quads[0].Bounds != outsetBox(3) {
			t.Errorf("ring bounds = %+v, want outset 3", quads[0].Bounds)
		}
	})
}

// TestOutlineInkOutset: the outline's outside extent
// (OutlineOutsetExtent, outline_painter.cc:1073-1082) — the auto
// ring's offset (border-adjusted) plus 2, a solid or dashed outline's
// offset plus width floored at zero — feeds the filter group's bounds
// and the scroll ink like the ring's width does.
func TestOutlineInkOutset(t *testing.T) {
	tests := []struct {
		style  OutlineStyle
		border Edges
		want   float32
	}{
		{OutlineStyle{Kind: OutlineSolid, Width: 3, Offset: 6}, Edges{}, 9},
		{OutlineStyle{Kind: OutlineSolid, Width: 2, Offset: -6}, Edges{}, 0},
		{OutlineStyle{Kind: OutlineSolid, Width: 3}, Edges{}, 3},
		{OutlineStyle{Kind: OutlineDashed, Width: 2, Offset: 4}, Edges{}, 6},
		{OutlineStyle{Kind: OutlineFocusRing}, Edges{}, 1},
		{OutlineStyle{Kind: OutlineFocusRing, Offset: 4}, Edges{}, 5},
		{OutlineStyle{Kind: OutlineFocusRing}, Edges{Top: 1, Right: 1, Bottom: 1, Left: 1}, 0},
		{OutlineStyle{Kind: OutlineNone, Width: 3, Offset: 6}, Edges{}, 0},
	}
	for _, tt := range tests {
		if got := tt.style.inkOutset(tt.border); got != tt.want {
			t.Errorf("kind %v width %v offset %v border %v: ink outset = %v, want %v",
				tt.style.Kind, tt.style.Width, tt.style.Offset, tt.border, got, tt.want)
		}
	}
}

// TestOutlineRefinement: the flattened outline refinement fields
// write into the style's Outline and merge like every other field.
func TestOutlineRefinement(t *testing.T) {
	style := DefaultStyle()
	blue := RingColorOf(SolidBackground(Rgb(0x0ea5e9)))
	style.Refine(&StyleRefinement{
		OutlineWidth:  Some(float32(2)),
		OutlineColor:  Some(blue),
		OutlineStyle:  Some(OutlineSolid),
		OutlineOffset: Some(float32(4)),
	})
	if style.Outline != (OutlineStyle{Kind: OutlineSolid, Width: 2, Color: blue, Offset: 4}) {
		t.Errorf("outline = %+v, want the refinement's fields", style.Outline)
	}

	// A second refinement replaces each present field (the derive's
	// per-field merge).
	style.Refine(&StyleRefinement{OutlineOffset: Some(float32(-6))})
	if style.Outline.Offset != -6 || style.Outline.Width != 2 {
		t.Errorf("outline = %+v, want only the offset replaced", style.Outline)
	}

	// The zero value is outline-style: auto — the CSS initial value,
	// and the UA focus rule's materialization point.
	if (OutlineStyle{}).Kind != OutlineAuto {
		t.Errorf("zero OutlineStyle kind = %v, want auto", (OutlineStyle{}).Kind)
	}
}
