package gpui_test

import (
	"math"
	"testing"

	gpui "github.com/hoangvu12/gpui-go/gpui"
)

// The CSS-value parsers the TSX style keys take (browser-core ticket
// 57): the clip-path shapes, the mask-image gradient and the
// drop-shadow values.

func TestParseClipPath(t *testing.T) {
	cases := []struct {
		value string
		check func(gpui.ClipPath) error
	}{
		{"circle(40px)", func(c gpui.ClipPath) error {
			if !c.Contains(gpui.Point{X: 50, Y: 40}, gpui.Size{Width: 100, Height: 80}) {
				return errOf("the center of circle(40px) should be inside")
			}
			if c.Contains(gpui.Point{X: 2, Y: 2}, gpui.Size{Width: 100, Height: 80}) {
				return errOf("the corner should be outside circle(40px)")
			}
			return nil
		}},
		{"circle(50%)", func(c gpui.ClipPath) error {
			if !c.Contains(gpui.Point{X: 50, Y: 40}, gpui.Size{Width: 100, Height: 80}) {
				return errOf("the center of circle(50%) should be inside")
			}
			return nil
		}},
		{"circle(30px at 25% 25%)", func(c gpui.ClipPath) error {
			if !c.Contains(gpui.Point{X: 25, Y: 20}, gpui.Size{Width: 100, Height: 80}) {
				return errOf("the circle(30px at 25% 25%) center should be inside")
			}
			if c.Contains(gpui.Point{X: 90, Y: 70}, gpui.Size{Width: 100, Height: 80}) {
				return errOf("the far corner should be outside circle(30px at 25% 25%)")
			}
			return nil
		}},
		{"ellipse(30% 50%)", func(c gpui.ClipPath) error {
			if !c.Contains(gpui.Point{X: 50, Y: 40}, gpui.Size{Width: 100, Height: 80}) {
				return errOf("the ellipse center should be inside")
			}
			return nil
		}},
		{"inset(10px 20px)", func(c gpui.ClipPath) error {
			if !c.Contains(gpui.Point{X: 50, Y: 40}, gpui.Size{Width: 100, Height: 80}) {
				return errOf("the inset middle should be inside")
			}
			if c.Contains(gpui.Point{X: 5, Y: 40}, gpui.Size{Width: 100, Height: 80}) {
				return errOf("the left inset band (20px) should be outside")
			}
			return nil
		}},
		{"inset(10px 20px 30px 40px)", func(c gpui.ClipPath) error {
			if c.Contains(gpui.Point{X: 30, Y: 40}, gpui.Size{Width: 100, Height: 80}) {
				return errOf("the left inset band (40px) should be outside")
			}
			if !c.Contains(gpui.Point{X: 45, Y: 40}, gpui.Size{Width: 100, Height: 80}) {
				return errOf("the middle should be inside")
			}
			return nil
		}},
		{"polygon(50% 0, 100% 100%, 0 100%)", func(c gpui.ClipPath) error {
			if !c.Contains(gpui.Point{X: 50, Y: 70}, gpui.Size{Width: 100, Height: 80}) {
				return errOf("the triangle's lower middle should be inside")
			}
			if c.Contains(gpui.Point{X: 10, Y: 5}, gpui.Size{Width: 100, Height: 80}) {
				return errOf("the top-left should be outside the triangle")
			}
			return nil
		}},
	}
	for _, tc := range cases {
		clip, err := gpui.ParseClipPath(tc.value)
		if err != nil {
			t.Errorf("ParseClipPath(%q): %v", tc.value, err)
			continue
		}
		if err := tc.check(clip); err != nil {
			t.Errorf("ParseClipPath(%q): %v", tc.value, err)
		}
	}
	for _, bad := range []string{"", "circle", "circle()", "circle(40em)", "ellipse(10%)", "inset(10px 20px 30px 40px 50px)", "polygon(0 0, 100% 100%)", "path(M0 0)"} {
		if _, err := gpui.ParseClipPath(bad); err == nil {
			t.Errorf("ParseClipPath(%q) should fail", bad)
		}
	}
}

func TestParseMaskImage(t *testing.T) {
	mask, err := gpui.ParseMaskImage("linear-gradient(to bottom, #000000, #00000000)")
	if err != nil {
		t.Fatalf("ParseMaskImage: %v", err)
	}
	if mask == nil || mask.Mode != gpui.MaskModeAlpha {
		t.Fatalf("mask = %+v, want the alpha default", mask)
	}
	stops := mask.Gradient.Stops
	if len(stops) != 2 {
		t.Fatalf("stops = %d, want 2", len(stops))
	}
	if stops[0].Color.A != 1 || stops[1].Color.A != 0 {
		t.Fatalf("the stops' alphas = %v, %v, want 1 → 0", stops[0].Color.A, stops[1].Color.A)
	}
	if mask.Gradient.Direction != gpui.GradientDirectionToBottom {
		t.Errorf("direction = %v, want to bottom", mask.Gradient.Direction)
	}

	// An angle and positioned stops.
	mask, err = gpui.ParseMaskImage("linear-gradient(90deg, #ffffff00 25%, #ffffffff 75%)")
	if err != nil {
		t.Fatalf("ParseMaskImage: %v", err)
	}
	stops = mask.Gradient.Stops
	if len(stops) != 2 || stops[0].Position != 0.25 || stops[1].Position != 0.75 {
		t.Fatalf("stops = %+v, want positions 0.25 and 0.75", stops)
	}
	if mask.Gradient.Direction != gpui.GradientDirectionAngle || mask.Gradient.Angle != 90 {
		t.Errorf("direction = (%v, %v), want the 90deg angle", mask.Gradient.Direction, mask.Gradient.Angle)
	}
	if !math.IsNaN(float64(mask.Gradient.Stops[0].Color.H)) && mask.Gradient.Stops[0].Color.A != 0 {
		// the alpha is the meaningful half for a mask; nothing to check
		// beyond the parse succeeding.
	}

	for _, bad := range []string{"", "radial-gradient(#000, #fff)", "linear-gradient(to sideways, #000, #fff)", "linear-gradient(#000, red)"} {
		if _, err := gpui.ParseMaskImage(bad); err == nil {
			t.Errorf("ParseMaskImage(%q) should fail", bad)
		}
	}
}

func TestParseDropShadowValues(t *testing.T) {
	filters, err := gpui.ParseDropShadowValues("0 4px 6px #00000040")
	if err != nil {
		t.Fatalf("ParseDropShadowValues: %v", err)
	}
	if len(filters) != 1 {
		t.Fatalf("filters = %d, want 1", len(filters))
	}
	f := filters[0]
	if f.Function != gpui.FilterFunctionDropShadow || f.OffsetX != 0 || f.OffsetY != 4 || f.BlurRadius != 6 {
		t.Fatalf("filter = %+v, want drop-shadow(0 4px 6px …)", f)
	}
	if f.Color != gpui.ParseHexOrPanic("#00000040") {
		t.Fatalf("color = %+v", f.Color)
	}

	// Two shadows, comma-separated, the default color for the second.
	filters, err = gpui.ParseDropShadowValues("0 1px 2px #0000001a, 0 4px 6px")
	if err != nil {
		t.Fatalf("ParseDropShadowValues: %v", err)
	}
	if len(filters) != 2 {
		t.Fatalf("filters = %d, want 2", len(filters))
	}
	if filters[1].Color.A != 0.25 {
		t.Fatalf("the default shadow color = %+v, want 25%% black", filters[1].Color)
	}

	for _, bad := range []string{"", "0 4px", "0 4px 6px red", "0 4px 6px #00000040, 1px"} {
		if _, err := gpui.ParseDropShadowValues(bad); err == nil {
			t.Errorf("ParseDropShadowValues(%q) should fail", bad)
		}
	}
}

func errOf(message string) error { return &staticError{message: message} }

type staticError struct{ message string }

func (e *staticError) Error() string { return e.message }
