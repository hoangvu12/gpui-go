package tw_test

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/tw"
)

// TestPaletteValues checks palette variables against Tailwind's hex
// values and gpui's named colors.
func TestPaletteValues(t *testing.T) {
	for _, tc := range []struct {
		name      string
		got, want gpui.Hsla
	}{
		{"Slate800", tw.Slate800, gpui.Rgb(0x1e293b)},
		{"Rose950", tw.Rose950, gpui.Rgb(0x4c0519)},
		{"Red500", tw.Red500, gpui.Rgb(0xef4444)},
		{"Blue500", tw.Blue500, gpui.Rgb(0x3b82f6)},
		{"White", tw.White, gpui.White()},
		{"Black", tw.Black, gpui.Black()},
		{"Transparent", tw.Transparent, gpui.TransparentBlack()},
	} {
		if tc.got != tc.want {
			t.Errorf("tw.%s = %+v, want %+v", tc.name, tc.got, tc.want)
		}
	}
	if tw.Transparent.A != 0 {
		t.Errorf("tw.Transparent alpha = %v, want 0", tw.Transparent.A)
	}
}

// TestPaletteMatchesTailwindColor checks that the class color names
// gpui.TailwindColor resolves give the package's values.
func TestPaletteMatchesTailwindColor(t *testing.T) {
	for name, want := range map[string]gpui.Hsla{
		"slate-800":   tw.Slate800,
		"zinc-50":     tw.Zinc50,
		"amber-300":   tw.Amber300,
		"emerald-400": tw.Emerald400,
		"blue-500":    tw.Blue500,
		"rose-950":    tw.Rose950,
		"white":       tw.White,
		"black":       tw.Black,
		"transparent": tw.Transparent,
	} {
		got, ok := gpui.TailwindColor(name)
		if !ok || got != want {
			t.Errorf("gpui.TailwindColor(%q) = %+v, %v; want %+v, true", name, got, ok, want)
		}
	}
}
