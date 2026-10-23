package gpui

// Base ticket 38's img SVG tests: SVG bytes through img() rasterize
// through the App's SvgRenderer (svg.go) exactly like the pin — resource
// bytes the codec cannot sniff take the img.rs:737-740 else branch
// (svg_renderer.render_single_frame(&bytes, 1.0)), and an
// ImageSource::Image of ImageFormat::Svg takes platform.rs:3659-3663.
// The rendered image keeps the smooth 2x scale factor (svg_renderer.rs
// render_parsed), so render_size (assets.rs:267-270) lays it out at the
// SVG's logical size.

import "testing"

// imgSvgRect24x12 is an opaque red 24x12 SVG.
const imgSvgRect24x12 = `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="12"><rect width="24" height="12" fill="#ff0000"/></svg>`

// checkSvgImage asserts the loaded SVG image: one 48x24 frame (the 2x
// smooth factor over 24x12), scale factor 2, opaque red BGRA pixels.
func checkSvgImage(t *testing.T, image *LoadedImage) {
	t.Helper()
	if image == nil {
		t.Fatal("no loaded SVG image")
	}
	if n := image.FrameCount(); n != 1 {
		t.Fatalf("SVG frames = %d, want 1", n)
	}
	if w, h := image.Size(0); w != 48 || h != 24 {
		t.Fatalf("SVG frame = %dx%d, want 48x24 (the smooth 2x render)", w, h)
	}
	if got := image.ScaleFactor(); got != SmoothSvgScaleFactor {
		t.Fatalf("SVG scale factor = %v, want %v", got, SmoothSvgScaleFactor)
	}
	if got := renderSize(image, 0); got != (Size{Width: 24, Height: 12}) {
		t.Fatalf("SVG render size = %+v, want 24x12", got)
	}
	pixels := image.RenderImage().Frames[0].Pixels
	for i := 0; i < len(pixels); i += 4 {
		if pixels[i] != 0 || pixels[i+1] != 0 || pixels[i+2] != 255 || pixels[i+3] != 255 {
			t.Fatalf("SVG pixel %d = %v, want opaque red BGRA (0,0,255,255)", i/4, pixels[i:i+4])
		}
	}
}

// checkSvgSprite asserts the painted sprite: the 24x12 logical size at
// the test window's 2x scale, over a 48x24 polychrome tile.
func checkSvgSprite(t *testing.T, scene *Scene) {
	t.Helper()
	sprite := lastSprite(t, scene, "SVG image")
	if sprite.Bounds != dev(0, 0, 48, 24) {
		t.Fatalf("SVG sprite bounds %+v, want (0,0 48x24)", sprite.Bounds)
	}
	if sprite.Tile.TextureKind != AtlasTexturePolychrome || sprite.Tile.BoundsW != 48 || sprite.Tile.BoundsH != 24 {
		t.Fatalf("SVG sprite tile %+v, want a 48x24 polychrome tile", sprite.Tile)
	}
}

// TestImgSvgEmbeddedAssetRenders pins the resource loader's SVG
// fallback (img.rs:737-740): an embedded .svg asset the codec cannot
// sniff renders through the SvgRenderer and paints a sprite — no
// "svg error".
func TestImgSvgEmbeddedAssetRenders(t *testing.T) {
	f := imgAssetFixture(t, "icons/dot.svg", []byte(imgSvgRect24x12), func(*paintView, *Window, *Context[paintView]) AnyElement {
		return Div().SizeFull().Child(Img("icons/dot.svg").ID("dot")).IntoElement()
	})
	f.draw()
	f.ta.RunUntilParked()
	result, done := f.window.GetImageAsset(EmbeddedResource("icons/dot.svg"), f.app)
	if !done || result.Err != nil {
		t.Fatalf("the SVG asset load = (%+v, %v)", result, done)
	}
	checkSvgImage(t, result.Image)
	checkSvgSprite(t, LastDrawnScene(f.window))
}

// TestImgSvgImageSourceRenders pins ImageSource::Image with
// ImageFormat::Svg (platform.rs:3659-3663 through the ImageDecoder
// asset, R:img.rs:607-613).
func TestImgSvgImageSourceRenders(t *testing.T) {
	encoded := NewImage(ImageFormatSVG, []byte(imgSvgRect24x12))
	f := newPaintFixture(t, func(*paintView, *Window, *Context[paintView]) AnyElement {
		return Div().SizeFull().Child(Img(encoded)).IntoElement()
	})
	f.draw()
	f.ta.RunUntilParked()
	result, done := ImageSourceFromImage(encoded).GetData(nil, f.window, f.app)
	if !done || result.Err != nil {
		t.Fatalf("the SVG image decode = (%+v, %v)", result, done)
	}
	checkSvgImage(t, result.Image)
	checkSvgSprite(t, LastDrawnScene(f.window))
}

// TestImgSvgParseErrorsKeepThePinnedVariants pins the error mapping:
// un-sniffable resource bytes that fail the SVG parse are the Usvg
// variant (img.rs:737-740, From<usvg::Error> at img.rs:788-792); an
// ImageFormat::Svg image that fails is Other (to_image_data's anyhow
// result, R:img.rs:612).
func TestImgSvgParseErrorsKeepThePinnedVariants(t *testing.T) {
	garbage := []byte("not an svg at all")
	f := imgAssetFixture(t, "broken.svg", garbage, func(*paintView, *Window, *Context[paintView]) AnyElement {
		return Div().SizeFull().Child(Img("broken.svg")).Child(Img(NewImage(ImageFormatSVG, garbage))).IntoElement()
	})
	f.draw()
	f.ta.RunUntilParked()
	result, done := f.window.GetImageAsset(EmbeddedResource("broken.svg"), f.app)
	if !done || result.Image != nil || result.Err == nil || result.Err.Kind != ImageCacheErrorSVG {
		t.Fatalf("the broken SVG asset = (%+v, %v), want the Usvg variant", result, done)
	}
	result, done = ImageSourceFromImage(NewImage(ImageFormatSVG, garbage)).GetData(nil, f.window, f.app)
	if !done || result.Image != nil || result.Err == nil || result.Err.Kind != ImageCacheErrorOther {
		t.Fatalf("the broken SVG image = (%+v, %v), want the Other variant", result, done)
	}
}
