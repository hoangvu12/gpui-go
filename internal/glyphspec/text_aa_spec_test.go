//go:build windows

// Ticket 06 (browser-core): text on a NON-OPAQUE window renders with
// grayscale antialiasing. This is the scene/glyph spec test the ticket
// asks for (no browser-parity fixture: the effect is a paint-mode
// selection, not layout or pixels):
//
//   - the rule is the pin's should_use_subpixel_rendering first check
//     (ce:crates/gpui/src/window.rs:4926-4931: a transparent window
//     background returns false — grayscale — before the per-window
//     override or the platform recommendation is consulted);
//   - Chrome's equivalent gate is cc's LCD-text disallow list
//     (cc/raster/lcd_text_disallowed_reason.h:17-30:
//     kBackgroundColorNotOpaque / kContentsNotOpaque force grayscale);
//   - the port implements it in PaintTextOptions.Background
//     (gpui/draw_text.go shouldUseSubpixelRendering), fed from the
//     frame's PaintContext by DrawWindowFrame.
//
// The observable is the sprite class the paint path inserts: grayscale
// rasters (AlphaMask format) become MonochromeSprite records, subpixel
// (BgraSubpixelMask) become SubpixelSprite records
// (gpui/draw_text.go paintGlyphFromAtlas).
//
// Environment expectation: the Windows AMD64 native artifact (the
// glyph and text services), as the rest of this package.

package glyphspec

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// TestTextPaintModeFollowsWindowBackground paints one shaped line under
// each background appearance and checks the sprite class: a transparent
// (plain, blurred, or Mica) window background forces grayscale —
// MonochromeSprite records only — while the opaque default follows the
// platform recommendation (Subpixel sprites on a ClearType machine,
// whose recommended mode the fx-0005 recorded trace pins for this
// machine). An explicit subpixel override on a transparent background
// still renders grayscale: the background check precedes the override,
// as in the pin.
func TestTextPaintModeFollowsWindowBackground(t *testing.T) {
	ts, err := gpui.DefaultTextSystem()
	if err != nil {
		t.Fatalf("DefaultTextSystem: %v", err)
	}
	const text = "Ag"
	const fontSize = float32(24)
	line, err := ts.ShapeText(gpui.TextLayoutRequest{
		Text:     text,
		FontSize: fontSize,
		Runs:     []gpui.TextRun{{Len: len(text), Font: gpui.DefaultFontDescriptor()}},
	})
	if err != nil {
		t.Fatalf("shaping %q: %v", text, err)
	}
	t.Cleanup(func() { _ = line.Dispose() })
	summary, err := line.Summary()
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if summary.GlyphCount == 0 {
		t.Fatal("the shaped line has no glyphs")
	}

	// recommended picks the expectation for the OPAQUE default: this
	// machine's recommended mode (Subpixel on a ClearType machine —
	// the fx-0005 recorded trace pins it; grayscale otherwise).
	recommended, err := ts.RecommendedRenderingMode()
	if err != nil {
		t.Fatalf("recommended rendering mode: %v", err)
	}
	opaqueWantsSubpixel := recommended == gpui.TextRenderingSubpixel

	paint := func(t *testing.T, opts gpui.PaintTextOptions) (monochrome, subpixel int) {
		t.Helper()
		scene := newTestScene(t)
		atlas, err := gpui.NewAtlas()
		if err != nil {
			t.Fatalf("NewAtlas: %v", err)
		}
		t.Cleanup(func() { _ = atlas.Dispose() })
		ctx := gpui.NewPaintContext(2, fullMask(), 1)
		if err := scene.PaintShapedText(line, gpui.Point{X: 8, Y: 8}, gpui.DefaultLineHeight(fontSize),
			gpui.Hsla{L: 0, A: 1}, ts, atlas, ctx, opts); err != nil {
			t.Fatalf("PaintShapedText: %v", err)
		}
		if err := scene.Finish(); err != nil {
			t.Fatalf("scene finish: %v", err)
		}
		monos, err := scene.MonochromeSprites()
		if err != nil {
			t.Fatalf("dump monochrome sprites: %v", err)
		}
		subs, err := scene.SubpixelSprites()
		if err != nil {
			t.Fatalf("dump subpixel sprites: %v", err)
		}
		return len(monos), len(subs)
	}

	cases := []struct {
		name         string
		opts         gpui.PaintTextOptions
		wantMono     bool
		wantSubpixel bool
	}{
		{"opaque default follows the recommendation", gpui.PaintTextOptions{}, !opaqueWantsSubpixel, opaqueWantsSubpixel},
		{"plain transparent forces grayscale", gpui.PaintTextOptions{Background: gpui.WindowBackgroundTransparent}, true, false},
		{"blurred forces grayscale", gpui.PaintTextOptions{Background: gpui.WindowBackgroundBlurred}, true, false},
		{"mica forces grayscale", gpui.PaintTextOptions{Background: gpui.WindowBackgroundMica}, true, false},
		{"mica alt forces grayscale", gpui.PaintTextOptions{Background: gpui.WindowBackgroundMicaAlt}, true, false},
		{"explicit grayscale override", gpui.PaintTextOptions{RenderingMode: gpui.TextRenderingGrayscale}, true, false},
		// The background check precedes the override (the pin returns
		// false at window.rs:4927-4931 before reading the mode).
		{"explicit subpixel override cannot beat a transparent background",
			gpui.PaintTextOptions{RenderingMode: gpui.TextRenderingSubpixel, Background: gpui.WindowBackgroundTransparent}, true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			monos, subs := paint(t, c.opts)
			if (monos > 0) != c.wantMono {
				t.Errorf("monochrome sprites = %d, want %v", monos, c.wantMono)
			}
			if (subs > 0) != c.wantSubpixel {
				t.Errorf("subpixel sprites = %d, want %v", subs, c.wantSubpixel)
			}
			if monos == 0 && subs == 0 {
				t.Fatal("no text sprites at all: the paint path inserted nothing")
			}
		})
	}
}
