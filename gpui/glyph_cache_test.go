package gpui

import (
	"bytes"
	"fmt"
	"testing"
)

// glyphCacheTestView paints text in several sizes, colors and subpixel
// offsets (gpui-perf ticket 02's differential test).
type glyphCacheTestView struct{}

func (*glyphCacheTestView) Render(w *Window, cx *Context[glyphCacheTestView]) AnyElement {
	rows := make([]any, 0, 24)
	colors := []Hsla{Rgb(0x000000), Rgb(0xd03030), Rgb(0x3060c0)}
	for i := 0; i < 24; i++ {
		row := Div().Pl(Px(float32(i) * 0.37)).TextColor(colors[i%len(colors)]).
			Child(fmt.Sprintf("%02d Sphinx of black quartz, judge my vow! 0123456789 ÅÉß", i))
		switch i % 3 {
		case 1:
			row = row.TextXs()
		case 2:
			row = row.TextXl()
		}
		rows = append(rows, row)
	}
	return Div().SizeFull().Flex().FlexCol().Children(rows...).IntoElement()
}

// drawGlyphCacheWindow draws frames of the test view on a real-text test
// window and returns every frame's monochrome, subpixel and polychrome
// sprites.
func drawGlyphCacheWindow(t *testing.T, frames int) [][]any {
	t.Helper()
	ta := NewTestApp()
	window := NewTestWindow(ta.App(), Size{Width: 900, Height: 900})
	t.Cleanup(func() {
		CloseTestWindow(window)
		ta.App().Shutdown()
	})
	if err := UseRealTextSystem(window); err != nil {
		t.Skipf("real text system: %v", err)
	}
	ta.Update(func(app *App) {
		view := NewEntity(app, window.Scope(), func(*glyphCacheTestView, *Context[glyphCacheTestView]) {})
		window.SetRootView(ViewOf[glyphCacheTestView](view))
	})
	var out [][]any
	for i := 0; i < frames; i++ {
		scene, err := DrawWindowFrame(window)
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		mono, err := scene.MonochromeSprites()
		if err != nil {
			t.Fatal(err)
		}
		sub, err := scene.SubpixelSprites()
		if err != nil {
			t.Fatal(err)
		}
		poly, err := scene.PolychromeSprites()
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, []any{mono, sub, poly})
	}
	return out
}

// TestGlyphPaintCachesMatchUncachedPaint: frames painted through the raster
// metadata and atlas tile caches carry exactly the sprites (bounds, masks,
// colors, tiles, order) of frames painted the pre-ticket-02 way, where
// every glyph rasterizes and goes through the native atlas insert.
func TestGlyphPaintCachesMatchUncachedPaint(t *testing.T) {
	glyphPaintCachesOff = true
	reference := drawGlyphCacheWindow(t, 2)
	glyphPaintCachesOff = false
	cached := drawGlyphCacheWindow(t, 3)

	count := 0
	for _, kind := range reference[0] {
		count += sliceLen(kind)
	}
	if count < 24*30 {
		t.Fatalf("the reference frame painted %d sprites, want at least %d", count, 24*30)
	}
	for i, frame := range cached {
		if want := fmt.Sprintf("%+v", reference[0]); fmt.Sprintf("%+v", frame) != want {
			t.Errorf("cached frame %d differs from the uncached reference", i)
		}
	}
	if fmt.Sprintf("%+v", reference[1]) != fmt.Sprintf("%+v", reference[0]) {
		t.Error("the uncached reference is not stable across frames")
	}
}

func sliceLen(v any) int {
	switch s := v.(type) {
	case []MonochromeSprite:
		return len(s)
	case []SubpixelSprite:
		return len(s)
	case []PolychromeSprite:
		return len(s)
	}
	return 0
}

// TestRasterizeIntoBufferMatchesRasterize: the pooled-buffer raster (one
// native call when it fits, the exact-buffer retry when it does not)
// returns the bytes and metadata of the plain two-call raster.
func TestRasterizeIntoBufferMatchesRasterize(t *testing.T) {
	ts, err := DefaultTextSystem()
	if err != nil {
		t.Skipf("text system: %v", err)
	}
	identity, err := ts.ResolveFont(FontDescriptor{Family: "Segoe UI", Weight: DefaultFontWeight})
	if err != nil {
		t.Fatal(err)
	}
	modes := []GlyphRenderMode{GlyphRenderGrayscale, GlyphRenderSubpixel}
	sizes := []float32{12, 24, 200} // 200 at 2x overflows the pooled buffer
	small := make([]byte, 16)
	for _, ch := range "Ag@ W" {
		glyphID, ok, err := ts.GlyphForChar(identity.FontID, ch)
		if err != nil || !ok {
			t.Fatalf("GlyphForChar(%q) = %v, %v", ch, ok, err)
		}
		for _, mode := range modes {
			style, err := ts.PrepareRasterStyle(Rgba{A: 1}, mode)
			if err != nil {
				t.Fatal(err)
			}
			for _, size := range sizes {
				for sub := uint8(0); sub < subpixelVariantsX; sub++ {
					params := RasterGlyphParams{FontID: identity.FontID, GlyphID: glyphID, FontSize: size, SubpixelX: sub, ScaleFactor: 2, RasterStyle: style}
					want, err := ts.RasterizeGlyph(params)
					if err != nil {
						t.Fatal(err)
					}
					for _, buf := range [][]byte{make([]byte, rasterBufferSize), small, nil} {
						got, err := ts.rasterizeGlyphInto(params, buf)
						if err != nil {
							t.Fatal(err)
						}
						if rasterMetadataOf(got) != rasterMetadataOf(want) || !bytes.Equal(got.Pixels, want.Pixels) {
							t.Errorf("%q size %v mode %v sub %d buf %d: raster differs", ch, size, mode, sub, len(buf))
						}
					}
				}
			}
		}
	}
}

// TestGlyphTileCacheFollowsAtlasRemovalAndDeviceLoss: the Go-side tile map
// forgets a removed key and everything on device loss and disposal, so a
// cached tile is never one the native atlas no longer holds.
func TestGlyphTileCacheFollowsAtlasRemovalAndDeviceLoss(t *testing.T) {
	ts, err := DefaultTextSystem()
	if err != nil {
		t.Skipf("text system: %v", err)
	}
	atlas, err := NewAtlas()
	if err != nil {
		t.Skipf("atlas: %v", err)
	}
	defer atlas.Dispose()
	identity, err := ts.ResolveFont(FontDescriptor{Family: "Segoe UI", Weight: DefaultFontWeight})
	if err != nil {
		t.Fatal(err)
	}
	glyphID, _, err := ts.GlyphForChar(identity.FontID, 'Q')
	if err != nil {
		t.Fatal(err)
	}
	style, err := ts.PrepareRasterStyle(Rgba{A: 1}, GlyphRenderGrayscale)
	if err != nil {
		t.Fatal(err)
	}
	params := RasterGlyphParams{FontID: identity.FontID, GlyphID: glyphID, FontSize: 16, ScaleFactor: 2, RasterStyle: style}
	raster, err := ts.RasterizeGlyph(params)
	if err != nil {
		t.Fatal(err)
	}
	insert := func() AtlasTile {
		tile, err := atlas.InsertGlyph(raster, params)
		if err != nil {
			t.Fatal(err)
		}
		return tile
	}
	tile := insert()
	if got, ok := atlas.cachedGlyphTile(params, raster.Format); !ok || got != tile {
		t.Fatalf("inserted tile not cached: %+v %v", got, ok)
	}
	if err := atlas.RemoveGlyph(params, raster.Format); err != nil {
		t.Fatal(err)
	}
	if _, ok := atlas.cachedGlyphTile(params, raster.Format); ok {
		t.Error("removed tile still cached")
	}
	insert()
	if err := atlas.NotifyDeviceLost(); err != nil {
		t.Fatal(err)
	}
	if _, ok := atlas.cachedGlyphTile(params, raster.Format); ok {
		t.Error("tile cached across device loss")
	}
	if _, found, err := atlas.GlyphTile(params, raster.Format); err != nil || found {
		t.Errorf("native atlas after device loss: found %v err %v", found, err)
	}
	fresh := insert()
	if got, ok := atlas.cachedGlyphTile(params, raster.Format); !ok || got != fresh || fresh.Generation == tile.Generation {
		t.Errorf("re-inserted tile after device loss: cached %+v %v, fresh %+v, old %+v", got, ok, fresh, tile)
	}
	if err := atlas.Dispose(); err != nil {
		t.Fatal(err)
	}
	if _, ok := atlas.cachedGlyphTile(params, raster.Format); ok {
		t.Error("tile cached after Dispose")
	}
}
