package gpui

// Base ticket 08's img element tests (img.go, Window.PaintImage): the
// element over embedded resources (the window asset path and the
// retain-all cache) and in-memory sources (decoded RenderImage data and
// encoded Image bytes), the ports of R:elements/img.rs:836-980, the
// fallback / loading replacements and the MIME formats.

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// imgPNG encodes a w x h opaque PNG.
func imgPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.NRGBA{R: 200, G: 40, B: 10, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// imgFrames builds decoded transparent BGRA frames (the pinned
// test_image / test_image_with_size, R:img.rs:808-818).
func imgFrames(count, w, h int) *LoadedImage {
	frames := make([]ImageFrame, count)
	for i := range frames {
		frames[i] = ImageFrame{Width: w, Height: h, Pixels: make([]byte, w*h*4)}
	}
	return NewLoadedImage(&RenderImage{Frames: frames})
}

// sprites returns the finished scene's polychrome sprites.
func sprites(t *testing.T, scene *Scene) []PolychromeSprite {
	t.Helper()
	out, err := scene.PolychromeSprites()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// lastSprite returns the scene's last polychrome sprite.
func lastSprite(t *testing.T, scene *Scene, what string) PolychromeSprite {
	t.Helper()
	all := sprites(t, scene)
	if len(all) == 0 {
		t.Fatalf("%s: no polychrome sprite painted", what)
	}
	return all[len(all)-1]
}

// imgAssetFixture is a test app whose asset registry holds key → bytes.
func imgAssetFixture(t *testing.T, key string, data []byte, build func(*paintView, *Window, *Context[paintView]) AnyElement) *paintFixture {
	t.Helper()
	registry := NewAssetRegistry()
	if err := registry.Insert(key, PreLoaded(data)); err != nil {
		t.Fatal(err)
	}
	ta := NewTestApp().WithAssets(registry)
	return newPaintFixtureIn(t, ta, build)
}

// TestImgEmbeddedSourceThroughWindowAsset pins the img element over an
// embedded resource without a cache (R:img.rs:544-549 use_data →
// window.use_asset): the loading frame paints nothing; the completion
// redraws the view; the resolved frame sizes the auto axes from the
// image (R:img.rs:348-380) and paints its polychrome sprite.
func TestImgEmbeddedSourceThroughWindowAsset(t *testing.T) {
	f := imgAssetFixture(t, "icons/photo.png", imgPNG(t, 4, 2), func(*paintView, *Window, *Context[paintView]) AnyElement {
		return Div().Flex().SizeFull().Child(Img("icons/photo.png").Flex().ID("photo")).IntoElement()
	})
	scene := f.draw()
	if n := len(sprites(t, scene)); n != 0 {
		t.Fatalf("loading frame sprites = %d, want 0", n)
	}
	draws := WindowDrawCount(f.window)
	f.ta.RunUntilParked()
	if got := WindowDrawCount(f.window); got != draws+1 {
		t.Fatalf("the load completion drew %d frames, want 1", got-draws)
	}
	sprite := lastSprite(t, LastDrawnScene(f.window), "resolved frame")
	if sprite.Bounds != dev(0, 0, 8, 4) || sprite.Opacity != 1 || sprite.Grayscale {
		t.Fatalf("sprite %+v, want the intrinsic (0,0 8x4)", sprite)
	}
	if sprite.Tile.TextureKind != AtlasTexturePolychrome || sprite.Tile.BoundsW != 4 || sprite.Tile.BoundsH != 2 {
		t.Fatalf("sprite tile %+v, want a 4x2 polychrome tile", sprite.Tile)
	}
	if !ImageSourceFromString("icons/photo.png").IsAssetCached(f.app) {
		t.Fatal("the embedded resource is not in the asset cache")
	}
}

// TestImgEmbeddedSourceThroughImageCache pins the explicit image cache
// (R:img.rs:230-235, 545-546 cache.load): the cache resolves the
// resource and the element paints the cached image with grayscale and an
// explicit size.
func TestImgEmbeddedSourceThroughImageCache(t *testing.T) {
	registry := NewAssetRegistry()
	if err := registry.Insert("photo.png", PreLoaded(imgPNG(t, 4, 2))); err != nil {
		t.Fatal(err)
	}
	ta := NewTestApp().WithAssets(registry)
	cache := NewRetainAllImageCache(ta.App(), ta.RootScope())
	f := newPaintFixtureIn(t, ta, func(*paintView, *Window, *Context[paintView]) AnyElement {
		return Div().Flex().SizeFull().Child(
			Img(EmbeddedResource("photo.png")).Flex().ImageCache(AnyImageCacheOf(cache)).Grayscale(true).ObjectFit(ObjectFitFill).W(Px(40)).H(Px(10)),
		).IntoElement()
	})
	f.draw()
	f.ta.RunUntilParked()
	sprite := lastSprite(t, LastDrawnScene(f.window), "cached image")
	if sprite.Bounds != dev(0, 0, 80, 20) || !sprite.Grayscale {
		t.Fatalf("sprite %+v, want a grayscale (0,0 80x20) fill", sprite)
	}
	cache.Read(f.app, func(c *RetainAllImageCache, _ *App) struct{} {
		if c.Len() != 1 {
			t.Fatalf("cache entries = %d, want 1", c.Len())
		}
		return struct{}{}
	})
}

// TestImgInMemoryImageSource pins ImageSource::Image (R:img.rs:553 →
// the ImageDecoder asset, R:img.rs:600-614): encoded bytes with a known
// format decode through the asset system and paint once resolved.
func TestImgInMemoryImageSource(t *testing.T) {
	encoded := NewImage(ImageFormatPNG, imgPNG(t, 6, 3))
	f := newPaintFixture(t, func(*paintView, *Window, *Context[paintView]) AnyElement {
		return Div().Flex().SizeFull().Child(Img(encoded).Flex()).IntoElement()
	})
	scene := f.draw()
	if n := len(sprites(t, scene)); n != 0 {
		t.Fatalf("decoding frame sprites = %d, want 0", n)
	}
	if !ImageSourceFromImage(encoded).IsAssetCached(f.app) {
		t.Fatal("the image decode is not in the asset cache")
	}
	f.ta.RunUntilParked()
	if sprite := lastSprite(t, LastDrawnScene(f.window), "decoded image"); sprite.Bounds != dev(0, 0, 12, 6) {
		t.Fatalf("sprite bounds %v, want (0,0 12x6)", sprite.Bounds)
	}
	ImageSourceFromImage(encoded).RemoveAsset(f.app)
	if ImageSourceFromImage(encoded).IsAssetCached(f.app) {
		t.Fatal("RemoveAsset left the decode cached")
	}
}

// TestImgZeroFrameImageDoesNotPanicOnPaint ports R:img.rs:836-842.
func TestImgZeroFrameImageDoesNotPanicOnPaint(t *testing.T) {
	empty := NewLoadedImage(&RenderImage{})
	f := newPaintFixture(t, func(*paintView, *Window, *Context[paintView]) AnyElement {
		return Img(ImageSourceFromRender(empty)).Flex().IntoElement()
	})
	if n := len(sprites(t, f.draw())); n != 0 {
		t.Fatalf("zero-frame image sprites = %d, want 0", n)
	}
}

// imgAt renders element inside a 100x100 box at (x, y) (the pinned
// window.draw(origin, size(100, 100), ..)).
func imgAt(x, y float32, element func() AnyElement) func(*paintView, *Window, *Context[paintView]) AnyElement {
	return func(*paintView, *Window, *Context[paintView]) AnyElement {
		return Div().Flex().SizeFull().Child(Div().Flex().Absolute().Left(Px(x)).Top(Px(y)).W(Px(100)).H(Px(100)).Child(element())).IntoElement()
	}
}

// TestImgObjectFitCoverCropsToElementBounds ports R:img.rs:844-897: a
// 200x100 image covering a 100x100 element paints the element bounds
// with the centered half of its tile.
func TestImgObjectFitCoverCropsToElementBounds(t *testing.T) {
	data := imgFrames(1, 200, 100)
	fit := ObjectFitFill
	var origin Point
	f := newPaintFixture(t, func(v *paintView, w *Window, cx *Context[paintView]) AnyElement {
		return imgAt(origin.X, origin.Y, func() AnyElement {
			return Img(data).Flex().SizeFull().ObjectFit(fit).IntoElement()
		})(v, w, cx)
	})
	full := lastSprite(t, f.draw(), "fill image").Tile

	fit, origin = ObjectFitCover, Point{X: 10, Y: 20}
	sprite := lastSprite(t, f.draw(), "cover image")
	if sprite.Bounds != dev(20, 40, 200, 200) {
		t.Fatalf("cover sprite bounds %v, want (20,40 200x200)", sprite.Bounds)
	}
	got := [4]int32{sprite.Tile.BoundsX - full.BoundsX, sprite.Tile.BoundsY - full.BoundsY, sprite.Tile.BoundsW, sprite.Tile.BoundsH}
	if got != [4]int32{50, 0, 100, 100} {
		t.Fatalf("cover sub-tile (dx, dy, w, h) = %v, want [50 0 100 100]", got)
	}
}

// TestImgExplicitAspectRatioIsNotOverridden ports R:img.rs:899-935.
func TestImgExplicitAspectRatioIsNotOverridden(t *testing.T) {
	f := newPaintFixture(t, imgAt(0, 0, func() AnyElement {
		return Div().Flex().Size(Px(100)).OverflowHidden().Child(
			Img(imgFrames(1, 100, 200)).Flex().SizeFull().AspectSquare().ObjectFit(ObjectFitContain),
		).IntoElement()
	}))
	if sprite := lastSprite(t, f.draw(), "contained image"); sprite.Bounds != dev(50, 0, 100, 200) {
		t.Fatalf("contained sprite bounds %v, want (50,0 100x200): letterboxed horizontally", sprite.Bounds)
	}
}

// TestImgObjectFitCoverClampsCornerRadiiToVisibleBounds ports
// R:img.rs:937-960.
func TestImgObjectFitCoverClampsCornerRadiiToVisibleBounds(t *testing.T) {
	f := newPaintFixture(t, imgAt(0, 0, func() AnyElement {
		return Img(imgFrames(1, 200, 100)).Flex().SizeFull().Rounded(Px(100)).ObjectFit(ObjectFitCover).IntoElement()
	}))
	if sprite := lastSprite(t, f.draw(), "rounded cover"); sprite.CornerRadii.TopLeft != 100 {
		t.Fatalf("corner radius %v, want 50 x scale 2 = 100", sprite.CornerRadii.TopLeft)
	}
}

// TestImgStaleFrameIndexIsClampedWhenImageChanges ports
// R:img.rs:962-980: a cached frame index from a five-frame image (seeded
// through the element's retained ImgState by a sibling canvas) is
// clamped when the element's image is replaced by a one-frame image.
func TestImgStaleFrameIndexIsClampedWhenImageChanges(t *testing.T) {
	five, one := imgFrames(5, 1, 1), imgFrames(1, 1, 1)
	first := true
	f := newPaintFixture(t, func(*paintView, *Window, *Context[paintView]) AnyElement {
		if first {
			seed := Canvas(func(Bounds, *Window, *App) struct{} { return struct{}{} }, func(_ Bounds, _ struct{}, w *Window, _ *App) {
				global := &GlobalElementID{Path: append(currentElementIDs(w), NameElementID("test-img"))}
				WithElementState(w, global, func(state *ImagePlayback, _ *Window) (struct{}, ImagePlayback) {
					if state == nil {
						panic("img state should be initialized")
					}
					next := *state
					next.FrameIndex = 4
					return struct{}{}, next
				})
			}).Flex()
			return Div().Flex().Child(Img(five).Flex().ID("test-img")).Child(seed).IntoElement()
		}
		return Img(one).Flex().ID("test-img").IntoElement()
	})
	f.draw()
	first = false
	if sprite := lastSprite(t, f.draw(), "replaced image"); sprite.Tile.BoundsW != 1 {
		t.Fatalf("replaced image sprite tile %+v", sprite.Tile)
	}
}

// TestImgFallbackAndLoadingReplacements pins the replacement branches of
// R:img.rs:390-422: a failed load lays out and paints the fallback; a
// load still in flight past LOADING_DELAY paints the loading element
// and the resolved
// image then replaces it.
func TestImgFallbackAndLoadingReplacements(t *testing.T) {
	ready := false
	data := imgFrames(1, 10, 10)
	custom := func(*Window, *App) (ImageAssetResult, bool) {
		if !ready {
			return ImageAssetResult{}, false
		}
		return ImageAssetResult{Image: data}, true
	}
	f := imgAssetFixture(t, "present.png", imgPNG(t, 1, 1), func(*paintView, *Window, *Context[paintView]) AnyElement {
		return Div().Flex().SizeFull().
			Child(Img("missing.png").Flex().ID("missing").WithFallback(func() AnyElement {
				return Div().Flex().W(Px(5)).H(Px(5)).Bg(paintRed).IntoElement()
			})).
			Child(Img(custom).Flex().ID("slow").WithLoading(func() AnyElement {
				return Div().Flex().W(Px(7)).H(Px(7)).Bg(paintBlue).IntoElement()
			})).
			IntoElement()
	})
	scene := f.draw()
	if n := len(quadsOf(t, scene, paintRed)) + len(quadsOf(t, scene, paintBlue)); n != 0 {
		t.Fatalf("first frame replacements = %d, want 0 (loading, before the delay)", n)
	}
	f.ta.RunUntilParked()
	if quad := oneQuadOf(t, LastDrawnScene(f.window), paintRed, "fallback"); quad.Bounds != dev(0, 0, 10, 10) {
		t.Fatalf("fallback quad %v", quad.Bounds)
	}
	// The delay timer notifies at the deadline (200ms, where the strict
	// elapsed > LOADING_DELAY test still fails on the deterministic clock);
	// the next render past it shows the loading element.
	f.ta.AdvanceClock(int64(LoadingDelay/1e6) + 1)
	f.notify()
	if quad := oneQuadOf(t, LastDrawnScene(f.window), paintBlue, "loading replacement"); quad.Bounds.Size != (Size{Width: 14, Height: 14}) {
		t.Fatalf("loading quad %v", quad.Bounds)
	}
	ready = true
	f.notify()
	scene = LastDrawnScene(f.window)
	if n := len(quadsOf(t, scene, paintBlue)); n != 0 {
		t.Fatalf("loading replacement still painted after the image resolved (%d)", n)
	}
	if sprite := lastSprite(t, scene, "resolved slow image"); sprite.Bounds.Size != (Size{Width: 20, Height: 20}) {
		t.Fatalf("resolved sprite %v", sprite.Bounds)
	}
}

// TestImgImageFormatMimeTypes pins ImageFormat::mime_type / extension /
// from_mime_type with its aliases (platform.rs:3476-3525) and the img
// source string conversion (R:img.rs:53-81).
func TestImgImageFormatMimeTypes(t *testing.T) {
	for _, format := range imageFormatsPinnedOrder {
		mime := ImageFormatMimeType(format)
		back, ok := ImageFormatFromMimeType(mime)
		if mime == "" || !ok || back != format || ImageFormatExtension(format) == "" {
			t.Fatalf("format %v: mime %q round trip %v %v", format, mime, back, ok)
		}
	}
	for mime, want := range map[string]ImageFormat{"image/jpg": ImageFormatJPEG, "image/tif": ImageFormatTIFF, "image/svg+xml": ImageFormatSVG, "image/x-portable-anymap": ImageFormatPNM} {
		if got, ok := ImageFormatFromMimeType(mime); !ok || got != want {
			t.Fatalf("FromMimeType(%q) = %v, %v; want %v", mime, got, ok, want)
		}
	}
	if _, ok := ImageFormatFromMimeType("image/JPEG"); ok {
		t.Fatal("mime matching is exact")
	}
	if ImageFormatExtension(ImageFormatJPEG) != "jpg" || ImageFormatMimeType(ImageFormatICO) != "image/ico" {
		t.Fatal("pinned jpeg extension / ico mime")
	}
	for s, want := range map[string]ResourceKind{
		"https://example.com/a.png": ResourceURI,
		"file:///tmp/a.png":         ResourceURI,
		"icons/a.png":               ResourceEmbedded,
		"a.png":                     ResourceEmbedded,
	} {
		if got := ImageSourceFromString(s); got.Kind != ImageSourceResource || got.Resource.Kind != want {
			t.Fatalf("ImageSourceFromString(%q) = %+v, want resource kind %d", s, got, want)
		}
	}
	if got := fmt.Sprint(len(ImgExtensions())); got != "22" {
		t.Fatalf("ImgExtensions = %s entries, want 22", got)
	}
}
