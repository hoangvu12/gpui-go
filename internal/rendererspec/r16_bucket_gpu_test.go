//go:build windows

// The browser-core ticket 51 renderer gates on the real GPU: the
// bounds-sized group-target buckets (finishing 50's handed-off gap)
// with the PINNED sprite pipelines drawing inside them - 50 recorded
// that a polychrome sprite batch drew nothing into a sub-window pool
// target. Everything runs through the ABI-level surface render +
// readback pair.
package rendererspec

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// r16ColorTile inserts one solid BGRA color tile into the atlas's
// polychrome pool and returns it (the pinned color-glyph shape).
func r16ColorTile(t *testing.T, atlas *gpui.Atlas, size int, b, g, r byte) gpui.AtlasTile {
	t.Helper()
	pixels := make([]byte, size*size*4)
	for i := 0; i < size*size; i++ {
		pixels[i*4+0] = b
		pixels[i*4+1] = g
		pixels[i*4+2] = r
		pixels[i*4+3] = 255
	}
	raster := &gpui.RasterizedGlyph{
		Width:  int32(size),
		Height: int32(size),
		Format: gpui.RasterFormatBgraColor,
		Pixels: pixels,
	}
	tile, err := atlas.InsertGlyph(raster, gpui.RasterGlyphParams{
		FontID:      1,
		GlyphID:     2,
		FontSize:    float32(size),
		ScaleFactor: 1,
		RasterStyle: gpui.PreparedRasterStyle{Mode: gpui.GlyphRenderGrayscale},
	})
	if err != nil {
		t.Fatalf("InsertGlyph: %v", err)
	}
	return tile
}

// r16Atlas opens a fresh atlas (the rendererspec pattern).
func r16Atlas(t *testing.T) *gpui.Atlas {
	t.Helper()
	atlas, err := gpui.NewAtlas()
	if err != nil {
		t.Fatalf("NewAtlas: %v", err)
	}
	t.Cleanup(func() { _ = atlas.Dispose() })
	return atlas
}

// r16PolySprite paints one polychrome sprite record through the native
// seam.
func r16PolySprite(t *testing.T, scene *gpui.Scene, tile gpui.AtlasTile, x, y, w, h float32) {
	t.Helper()
	sprite := gpui.PolychromeSprite{
		Opacity:     1,
		Bounds:      gpui.Bounds{Origin: gpui.Point{X: x, Y: y}, Size: gpui.Size{Width: w, Height: h}},
		ContentMask: gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 4096, Height: 4096}},
		Tile:        tile,
	}
	if err := scene.InsertPolychromeSprite(sprite); err != nil {
		t.Fatalf("InsertPolychromeSprite: %v", err)
	}
}

// r16RenderAt renders with the scene's atlas (the sprite scenes need
// it) and reads the pixels back.
func r16RenderAt(t *testing.T, surface *gpui.Surface, scene *gpui.Scene, atlas *gpui.Atlas, w, h int) []byte {
	t.Helper()
	if err := scene.FinishIfNeeded(); err != nil {
		t.Fatalf("FinishIfNeeded: %v", err)
	}
	if err := surface.RenderScene(scene, atlas, gpui.BackgroundOpaque); err != nil {
		t.Fatalf("RenderScene: %v", err)
	}
	pixels, err := surface.ReadPixels()
	if err != nil {
		t.Fatalf("ReadPixels: %v", err)
	}
	if len(pixels) != w*h*4 {
		t.Fatalf("read back %d bytes, want %d", len(pixels), w*h*4)
	}
	return pixels
}

// TestR16PolychromeSpriteInSubWindowBucket: a polychrome sprite batch
// inside an isolated group whose extent is far smaller than the window
// (the bounds-sized bucket, offset origin) draws and composites - 50
// recorded the pinned sprite pipeline drawing NOTHING into a sub-window
// pool target.
func TestR16PolychromeSpriteInSubWindowBucket(t *testing.T) {
	const w, h = 640, 480
	surface := r16Surface(t, "rendererspec r16 sprite bucket", 640, 480)
	atlas := r16Atlas(t)
	tile := r16ColorTile(t, atlas, 64, 255, 0, 0) // solid blue in BGRA
	scene := r16Scene(t)
	fullMask := gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 4096, Height: 4096}}
	// The group's extent: two sprites at (200,150) and (216,166), each
	// 64x64 - a 80x80 region far inside the 640x480 window, so the
	// bucket quantizes to 256x256 at origin (200,150) (offset, not the
	// window-sized R15 shape).
	groupBounds := gpui.Bounds{Origin: gpui.Point{X: 200, Y: 150}, Size: gpui.Size{Width: 80, Height: 80}}
	r16Effect(t, scene, gpui.EffectGroup{HasOpacity: true, Opacity: 1, Bounds: groupBounds, ContentMask: fullMask})
	r16PolySprite(t, scene, tile, 200, 150, 64, 64)
	r16PolySprite(t, scene, tile, 216, 166, 64, 64)
	r16EndEffect(t, scene, gpui.EffectGroup{HasOpacity: true, Bounds: groupBounds, ContentMask: fullMask})
	pixels := r16RenderAt(t, surface, scene, atlas, w, h)
	// The sprite area reads solid blue; the bucket's untouched area and
	// the window outside read the opaque white clear.
	r, g, b, a := pixelAt(pixels, w, 220, 170)
	if b < 240 || r > 10 || g > 10 || a != 255 {
		t.Fatalf("the sprite region reads (%d,%d,%d,%d), want solid blue - the pinned sprite pipeline drew nothing into the sub-window bucket", r, g, b, a)
	}
	r, g, b, _ = pixelAt(pixels, w, 10, 10)
	if r < 245 {
		t.Fatalf("the clear region reads (%d,%d,%d), want the opaque white clear", r, g, b)
	}
}

// TestR16QuadInSubWindowBucket: the same sub-window, offset bucket with
// the vendored quad pipeline (the control 50 verified working - the
// offset viewport and the composite's region-relative sampling).
func TestR16QuadInSubWindowBucket(t *testing.T) {
	const w, h = 640, 480
	surface := r16Surface(t, "rendererspec r16 quad bucket", 640, 480)
	scene := r16Scene(t)
	fullMask := gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 4096, Height: 4096}}
	groupBounds := gpui.Bounds{Origin: gpui.Point{X: 200, Y: 150}, Size: gpui.Size{Width: 80, Height: 80}}
	r16Effect(t, scene, gpui.EffectGroup{HasOpacity: true, Opacity: 1, Bounds: groupBounds, ContentMask: fullMask})
	r16Quad(t, scene, 200, 150, 60, 60, 0, 0.5)
	r16Quad(t, scene, 220, 170, 60, 60, 2.0/3.0, 0.5)
	r16EndEffect(t, scene, gpui.EffectGroup{HasOpacity: true, Bounds: groupBounds, ContentMask: fullMask})
	pixels := r16RenderAt(t, surface, scene, nil, w, h)
	r, g, b, a := pixelAt(pixels, w, 210, 160)
	if r < 240 || g > 10 || b > 10 || a != 255 {
		t.Fatalf("the red quad region reads (%d,%d,%d,%d), want solid red", r, g, b, a)
	}
	r, g, b, _ = pixelAt(pixels, w, 265, 215)
	if b < 240 || r > 10 || g > 10 {
		t.Fatalf("the blue quad region reads (%d,%d,%d), want solid blue", r, g, b)
	}
	r, g, b, _ = pixelAt(pixels, w, 400, 300)
	if r < 245 {
		t.Fatalf("the region outside the bucket reads (%d,%d,%d), want the opaque white clear", r, g, b)
	}
}
