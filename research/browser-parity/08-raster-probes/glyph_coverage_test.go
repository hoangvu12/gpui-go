//go:build windows

package rasterprobe

import (
	"fmt"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// TestGpuiGlyphCoverage measures gpui-go's raw glyph coverage through
// the public TextSystem raster API (the same call the window paint
// path makes), in both render modes, for the probe's sizes: the
// raster-side half of the A/B. Chrome's half is the recorded pages'
// pixel coverage; the pin's shader correction (the Terminal gamma
// table, enhanced contrast) is applied between this raw coverage and
// the composite the harness captures.
func TestGpuiGlyphCoverage(t *testing.T) {
	ts, err := gpui.DefaultTextSystem()
	if err != nil {
		t.Fatal(err)
	}
	identity, err := ts.ResolveFont(gpui.FontDescriptor{Family: ".SystemUIFont", Weight: 400, Style: gpui.FontStyleNormal})
	if err != nil {
		t.Fatal(err)
	}
	if !identity.FamilyFound {
		t.Fatal("the system UI font did not resolve")
	}
	glyphID, ok, err := ts.GlyphForChar(identity.FontID, 'H')
	if err != nil || !ok {
		t.Fatalf("glyph for 'H': %v ok=%v", err, ok)
	}

	for _, size := range []float32{12, 16, 24, 32} {
		for _, mode := range []struct {
			name  string
			value gpui.GlyphRenderMode
		}{
			{"grayscale", gpui.GlyphRenderGrayscale},
			{"subpixel", gpui.GlyphRenderSubpixel},
		} {
			// Black text: the preblend color the pin quantizes for
			// dark text.
			style, err := ts.PrepareRasterStyle(gpui.Rgba{R: 0, G: 0, B: 0, A: 1}, mode.value)
			if err != nil {
				t.Fatal(err)
			}
			glyph, err := ts.RasterizeGlyph(gpui.RasterGlyphParams{
				FontID:      identity.FontID,
				GlyphID:     glyphID,
				FontSize:    size,
				SubpixelX:   0,
				SubpixelY:   0,
				ScaleFactor: gpui.TestScaleFactor,
				RasterStyle: style,
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%s %gpx %s: effect tag %d (preblend %v), format %d, bounds %dx%d",
				"H", size, mode.name, style.Effect, style.Effect == gpui.RasterEffectPreblend, glyph.Format, glyph.Width, glyph.Height)
			switch glyph.Format {
			case gpui.RasterFormatAlphaMask:
				profile, partial, full, ink := a8Stats(glyph)
				t.Logf("  A8: ink %d px, full %d px, partial %d px, partial mean %.4f; edge profile %s",
					ink, full, partial, partialMean(glyph.Pixels, glyph.Width), profile)
			case gpui.RasterFormatBgraSubpixelMask:
				rows := subpixelProfile(glyph)
				for _, row := range rows {
					t.Logf("  BGRA row %d: %s", row.y, row.text)
				}
			}
		}
	}
}

// a8Stats summarizes an A8 raster: the ink/full/partial counts and the
// middle band's per-row coverage text.
func a8Stats(glyph *gpui.RasterizedGlyph) (profile string, partial, full, ink int) {
	mid := int(glyph.Height) / 2
	width := int(glyph.Width)
	for x := 0; x < width; x++ {
		v := glyph.Pixels[mid*width+x]
		if v > 12 {
			ink++
			if v >= 242 {
				full++
			} else {
				partial++
			}
		}
		profile += fmt.Sprintf("%3d ", v)
	}
	return profile, partial, full, ink
}

// partialMean computes the mean coverage of the A8 mask's partial
// pixels (the AA edges).
func partialMean(pixels []byte, width int32) float64 {
	sum, count := 0.0, 0
	for _, v := range pixels {
		if v > 12 && v < 242 {
			sum += float64(v) / 255
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

// subpixelProfile prints the BGRA subpixel raster's middle rows.
func subpixelProfile(glyph *gpui.RasterizedGlyph) []struct {
	y    int
	text string
} {
	var out []struct {
		y    int
		text string
	}
	for _, y := range []int{int(glyph.Height) / 4, int(glyph.Height) / 2, 3 * int(glyph.Height) / 4} {
		if y >= int(glyph.Height) {
			continue
		}
		text := ""
		for x := 0; x < int(glyph.Width); x++ {
			i := (y*int(glyph.Width) + x) * 4
			text += fmt.Sprintf("%3d/%3d/%3d ", glyph.Pixels[i+2], glyph.Pixels[i+1], glyph.Pixels[i])
		}
		out = append(out, struct {
			y    int
			text string
		}{y, text})
	}
	return out
}
