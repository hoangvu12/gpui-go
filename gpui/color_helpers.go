package gpui

import (
	"fmt"
	"strconv"
	"strings"
)

// Color helpers for authoring (ADR 0001). Rgb and the Hsla methods
// mirror the pinned Rust names (gpui::rgb, Hsla::opacity, Hsla::alpha);
// Hex and ParseHex accept the CSS hex notation web code uses.

// Rgb returns the opaque color 0xRRGGBB (the pinned rgb(hex)):
// Rgb(0x1e293b) is RgbaToHsla(0x1e293bff).
func Rgb(hex uint32) Hsla { return RgbaToHsla(hex<<8 | 0xFF) }

// Hex returns the color written in CSS hex notation: "#rgb", "#rgba",
// "#rrggbb" or "#rrggbbaa" (the leading # is optional). It panics on
// malformed input; use ParseHex for input that is not a literal.
func Hex(css string) Hsla {
	c, err := ParseHex(css)
	if err != nil {
		panic(err)
	}
	return c
}

// ParseHex parses a color in CSS hex notation, as Hex does.
func ParseHex(css string) (Hsla, error) {
	digits := strings.TrimPrefix(css, "#")
	switch len(digits) {
	case 3, 4:
		// Short form: each digit doubles (#abc is #aabbcc).
		var long strings.Builder
		for _, d := range digits {
			long.WriteRune(d)
			long.WriteRune(d)
		}
		digits = long.String()
	case 6, 8:
	default:
		return Hsla{}, fmt.Errorf("gpui: %q is not a CSS hex color (#rgb, #rgba, #rrggbb or #rrggbbaa)", css)
	}
	if len(digits) == 6 {
		digits += "ff"
	}
	v, err := strconv.ParseUint(digits, 16, 32)
	if err != nil {
		return Hsla{}, fmt.Errorf("gpui: %q is not a CSS hex color (#rgb, #rgba, #rrggbb or #rrggbbaa)", css)
	}
	return RgbaToHsla(uint32(v)), nil
}

// Opacity returns the color with its alpha multiplied by factor,
// clamped to [0, 1] (the pinned Hsla::opacity): Black.Opacity(0.5) is
// half-transparent black, and a half-transparent color at Opacity(0.5)
// is a quarter opaque.
func (c Hsla) Opacity(factor float32) Hsla {
	c.A *= clamp01(factor)
	return c
}

// Alpha returns the color with its alpha set to a, clamped to [0, 1]
// (the pinned Hsla::alpha). It is the Tailwind opacity modifier:
// bg-slate-900/50 is Bg(tw.Slate900.Alpha(0.5)).
func (c Hsla) Alpha(a float32) Hsla {
	c.A = clamp01(a)
	return c
}
