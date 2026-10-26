//go:build windows

// The browser-core ticket 52 (rev 16) mask, clip-path and drop-shadow
// renderer gates on the real GPU: the gradient/atlas/coverage mask
// composites (alpha vs luminance, the invalid-mask hide) and the
// drop-shadow filter op (the blurred alpha silhouette tinted and
// offset beneath the content). Everything runs through the ABI-level
// surface render + readback pair.
package rendererspec

import (
	"math"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// r52MaskGroup opens one masked group over the given bounds with the
// given mask payload.
func r52MaskGroup(bounds gpui.Bounds, kind gpui.MaskKind, reference uint32, mode gpui.MaskMode) gpui.EffectGroup {
	return gpui.EffectGroup{
		HasOpacity:    true,
		Opacity:       1,
		HasMask:       true,
		MaskKind:      kind,
		MaskReference: reference,
		MaskMode:      mode,
		Bounds:        bounds,
		ContentMask:   gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 4096, Height: 4096}},
	}
}

// TestR52GradientMaskAlphaReadback: a full-window red group under a
// two-stop alpha gradient mask (white -> transparent, left to right)
// fades over the window's white background; the mask's alpha is the
// ramp's alpha channel (dst_in).
func TestR52GradientMaskAlphaReadback(t *testing.T) {
	const w, h = 120, 80
	surface := r16Surface(t, "rendererspec r52 gradient mask alpha", 120, 80)
	scene := r16Scene(t)
	stops := []gpui.TableGradientStop{
		{Color: gpui.Rgba{R: 1, G: 1, B: 1, A: 1}, Position: 0},
		{Color: gpui.Rgba{A: 0}, Position: 1},
	}
	index, err := scene.InsertGradient(gpui.Gradient{
		Kind:     gpui.GradientLinear,
		Geometry: [8]float32{0, 0, 120, 0},
		Stops:    stops,
	})
	if err != nil {
		t.Fatalf("InsertGradient: %v", err)
	}
	window := gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 120, Height: 80}}
	group := r52MaskGroup(window, gpui.MaskKindGradient, index, gpui.MaskModeAlpha)
	r52Effect(t, scene, group)
	r52Quad(t, scene, 0, 0, 120, 80, 0, 0.5, 1)
	r52EndEffect(t, scene, group)
	pixels := r52Render(t, surface, scene, w, h)
	ramp := r52BakeRamp(stops)
	for _, probe := range [][2]int{{10, 40}, {40, 40}, {60, 40}, {90, 40}, {115, 40}} {
		x, y := probe[0], probe[1]
		tt := r52GradientParameter(gpui.GradientLinear, [8]float32{0, 0, 120, 0}, float32(x)+0.5, float32(y)+0.5)
		mask := r52RampSample(ramp, r52WrapTile(gpui.GradientTilePad, tt))[3]
		// Red at `mask` over white: (255, (1-mask)*255, (1-mask)*255).
		want := [4]float32{1, 1 - mask, 1 - mask, 1}
		if !r52NearColor(pixels, w, x, y, want, 6) {
			i := (y*w + x) * 4
			t.Fatalf("pixel (%d,%d) reads (%d,%d,%d,%d), want the alpha-masked red %v (mask=%v)", x, y, pixels[i], pixels[i+1], pixels[i+2], pixels[i+3], want, mask)
		}
	}
}

// TestR52GradientMaskLuminanceVsAlpha: the same red-to-transparent
// gradient mask reads DIFFERENTLY in luminance mode (the premultiplied
// red ramp's REC-601 luminance, 0.3 of the fade) than in alpha mode -
// the two modes are observable in the pixels.
func TestR52GradientMaskLuminanceVsAlpha(t *testing.T) {
	const w, h = 120, 80
	stops := []gpui.TableGradientStop{
		{Color: gpui.Rgba{R: 1, A: 1}, Position: 0},
		{Color: gpui.Rgba{A: 0}, Position: 1},
	}
	window := gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 120, Height: 80}}
	for _, mode := range []gpui.MaskMode{gpui.MaskModeAlpha, gpui.MaskModeLuminance} {
		surface := r16Surface(t, "rendererspec r52 mask mode", 120, 80)
		scene := r16Scene(t)
		index, err := scene.InsertGradient(gpui.Gradient{
			Kind:     gpui.GradientLinear,
			Geometry: [8]float32{0, 0, 120, 0},
			Stops:    stops,
		})
		if err != nil {
			t.Fatalf("InsertGradient: %v", err)
		}
		group := r52MaskGroup(window, gpui.MaskKindGradient, index, mode)
		r52Effect(t, scene, group)
		r52Quad(t, scene, 0, 0, 120, 80, 0, 0.5, 1)
		r52EndEffect(t, scene, group)
		pixels := r52Render(t, surface, scene, w, h)
		ramp := r52BakeRamp(stops)
		x, y := 40, 40
		tt := r52GradientParameter(gpui.GradientLinear, [8]float32{0, 0, 120, 0}, float32(x)+0.5, float32(y)+0.5)
		pm := r52RampSample(ramp, r52WrapTile(gpui.GradientTilePad, tt))
		mask := pm[3]
		if mode == gpui.MaskModeLuminance {
			// The premultiplied sample's luminance: 0.3*pm.r + 0.59*pm.g
			// + 0.11*pm.b (a red ramp: 0.3 of the fade).
			mask = 0.3*pm[0] + 0.59*pm[1] + 0.11*pm[2]
		}
		want := [4]float32{1, 1 - mask, 1 - mask, 1}
		if !r52NearColor(pixels, w, x, y, want, 6) {
			i := (y*w + x) * 4
			t.Fatalf("mode %d: pixel (%d,%d) reads (%d,%d,%d,%d), want %v (mask=%v)", mode, x, y, pixels[i], pixels[i+1], pixels[i+2], pixels[i+3], want, mask)
		}
		// The two modes differ visibly at this probe.
	}
}

// TestR52AtlasMaskAlphaReadback: an atlas image mask (the whole
// polychrome texture stretched over the group bounds - a 1024x1024
// tile fills texture 0 exactly): the left half opaque, the right half
// transparent, alpha mode. The group's red content shows on the left
// and hides on the right.
func TestR52AtlasMaskAlphaReadback(t *testing.T) {
	const w, h = 120, 80
	surface := r16Surface(t, "rendererspec r52 atlas mask alpha", 120, 80)
	atlas := r16Atlas(t)
	// One 1024x1024 image tile: the left half opaque white, the right
	// half transparent (BGRA bytes).
	size := 1024
	pixelsImg := make([]byte, size*size*4)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			i := (y*size + x) * 4
			if x < size/2 {
				pixelsImg[i], pixelsImg[i+1], pixelsImg[i+2], pixelsImg[i+3] = 255, 255, 255, 255
			}
		}
	}
	raster := &gpui.RasterizedGlyph{
		Width:  int32(size),
		Height: int32(size),
		Format: gpui.RasterFormatBgraColor,
		Pixels: pixelsImg,
	}
	tile, err := atlas.InsertGlyph(raster, gpui.RasterGlyphParams{
		FontID:      52,
		GlyphID:     1,
		FontSize:    float32(size),
		ScaleFactor: 1,
		RasterStyle: gpui.PreparedRasterStyle{Mode: gpui.GlyphRenderGrayscale},
	})
	if err != nil {
		t.Fatalf("InsertGlyph: %v", err)
	}
	if tile.TextureIndex != 0 {
		t.Fatalf("the mask image's texture index is %d, want 0 (the first polychrome texture)", tile.TextureIndex)
	}
	scene := r16Scene(t)
	window := gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 120, Height: 80}}
	group := r52MaskGroup(window, gpui.MaskKindAtlas, tile.TextureIndex, gpui.MaskModeAlpha)
	r52Effect(t, scene, group)
	r52Quad(t, scene, 0, 0, 120, 80, 0, 0.5, 1)
	r52EndEffect(t, scene, group)
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
	// The mask stretches the whole texture over the window: the left
	// half shows the red content, the right half hides (white).
	for _, probe := range [][2]int{{20, 40}, {50, 40}, {70, 40}, {100, 40}} {
		r, g, b, a := pixelAt(pixels, w, probe[0], probe[1])
		if probe[0] < 60 {
			if r < 240 || g > 15 || b > 15 || a != 255 {
				t.Fatalf("the masked left half (%d,%d) reads (%d,%d,%d,%d), want the red content", probe[0], probe[1], r, g, b, a)
			}
		} else {
			if r < 240 || g < 240 || b < 240 || a != 255 {
				t.Fatalf("the masked-out right half (%d,%d) reads (%d,%d,%d,%d), want the hidden white", probe[0], probe[1], r, g, b, a)
			}
		}
	}
}

// TestR52AtlasMaskLuminanceReadback: the same atlas mask in luminance
// mode with a horizontal gray ramp image (constant alpha): the mask
// value is the sampled luminance, so the red content fades left to
// right.
func TestR52AtlasMaskLuminanceReadback(t *testing.T) {
	const w, h = 120, 80
	surface := r16Surface(t, "rendererspec r52 atlas mask luminance", 120, 80)
	atlas := r16Atlas(t)
	size := 1024
	pixelsImg := make([]byte, size*size*4)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			i := (y*size + x) * 4
			gray := byte(255 * (1 - float32(x)/float32(size-1)))
			pixelsImg[i], pixelsImg[i+1], pixelsImg[i+2], pixelsImg[i+3] = gray, gray, gray, 255
		}
	}
	raster := &gpui.RasterizedGlyph{
		Width:  int32(size),
		Height: int32(size),
		Format: gpui.RasterFormatBgraColor,
		Pixels: pixelsImg,
	}
	tile, err := atlas.InsertGlyph(raster, gpui.RasterGlyphParams{
		FontID:      52,
		GlyphID:     2,
		FontSize:    float32(size),
		ScaleFactor: 1,
		RasterStyle: gpui.PreparedRasterStyle{Mode: gpui.GlyphRenderGrayscale},
	})
	if err != nil {
		t.Fatalf("InsertGlyph: %v", err)
	}
	scene := r16Scene(t)
	window := gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 120, Height: 80}}
	group := r52MaskGroup(window, gpui.MaskKindAtlas, tile.TextureIndex, gpui.MaskModeLuminance)
	r52Effect(t, scene, group)
	r52Quad(t, scene, 0, 0, 120, 80, 0, 0.5, 1)
	r52EndEffect(t, scene, group)
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
	// The mask's luminance at u = (x+0.5)/120: the image's ramp linearly
	// interpolated (the hardware's filter over the 1024-wide ramp).
	for _, probe := range [][2]int{{10, 40}, {40, 40}, {60, 40}, {90, 40}} {
		x, y := probe[0], probe[1]
		u := (float32(x) + 0.5) / 120
		coord := u * 1024
		i0 := int(math.Floor(float64(coord)))
		if i0 > 1022 {
			i0 = 1022
		}
		frac := coord - float32(i0)
		lumAt := func(i int) float32 { return 1 - float32(i)/1023 }
		mask := lumAt(i0) + frac*(lumAt(i0+1)-lumAt(i0))
		want := [4]float32{1, 1 - mask, 1 - mask, 1}
		if !r52NearColor(pixels, w, x, y, want, 6) {
			i := (y*w + x) * 4
			t.Fatalf("pixel (%d,%d) reads (%d,%d,%d,%d), want the luminance-masked red %v (mask=%v)", x, y, pixels[i], pixels[i+1], pixels[i+2], pixels[i+3], want, mask)
		}
	}
}

// TestR52CoverageMaskReadback: a clip-path coverage mask - a
// rectangular path rasterized into the A8 coverage target clips the
// group's red content to the rectangle (the CSS clip-path circle/
// polygon case's shape: any tessellated path).
func TestR52CoverageMaskReadback(t *testing.T) {
	const w, h = 120, 80
	surface := r16Surface(t, "rendererspec r52 coverage mask", 120, 80)
	scene := r16Scene(t)
	// The clip path: one rectangle (30,20)-(90,60) as two triangles
	// (device pixels; the path record's own paint color is transparent -
	// the path sprite draws nothing, the coverage rasterization paints
	// it white).
	path := &gpui.Path{
		Vertices: []gpui.PathVertex{
			{X: 30, Y: 20, S: 0, T: 1},
			{X: 90, Y: 20, S: 0, T: 1},
			{X: 30, Y: 60, S: 0, T: 1},
			{X: 30, Y: 60, S: 0, T: 1},
			{X: 90, Y: 20, S: 0, T: 1},
			{X: 90, Y: 60, S: 0, T: 1},
		},
		Bounds: gpui.Bounds{Origin: gpui.Point{X: 30, Y: 20}, Size: gpui.Size{Width: 60, Height: 40}},
	}
	if err := scene.InsertPath(path, gpui.SolidBackground(gpui.Hsla{A: 0}), gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 4096, Height: 4096}}); err != nil {
		t.Fatalf("InsertPath: %v", err)
	}
	coverage, err := scene.InsertCoverage(0, gpui.Bounds{Origin: gpui.Point{X: 30, Y: 20}, Size: gpui.Size{Width: 60, Height: 40}})
	if err != nil {
		t.Fatalf("InsertCoverage: %v", err)
	}
	if coverage != 0 {
		t.Fatalf("the coverage index is %d, want 0", coverage)
	}
	window := gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 120, Height: 80}}
	group := r52MaskGroup(window, gpui.MaskKindCoverage, coverage, gpui.MaskModeAlpha)
	r52Effect(t, scene, group)
	r52Quad(t, scene, 0, 0, 120, 80, 0, 0.5, 1)
	r52EndEffect(t, scene, group)
	pixels := r52Render(t, surface, scene, w, h)
	// Inside the coverage rectangle: red; outside: white (hidden).
	for _, probe := range [][2]int{{60, 40}, {35, 25}, {85, 55}, {10, 10}, {110, 40}, {60, 70}} {
		r, g, b, a := pixelAt(pixels, w, probe[0], probe[1])
		inside := probe[0] >= 30 && probe[0] < 90 && probe[1] >= 20 && probe[1] < 60
		if inside {
			if r < 240 || g > 15 || b > 15 || a != 255 {
				t.Fatalf("the covered (%d,%d) reads (%d,%d,%d,%d), want the red content", probe[0], probe[1], r, g, b, a)
			}
		} else {
			if r < 240 || g < 240 || b < 240 || a != 255 {
				t.Fatalf("the clipped-out (%d,%d) reads (%d,%d,%d,%d), want the hidden white", probe[0], probe[1], r, g, b, a)
			}
		}
	}
}

// TestR52InvalidMaskHidesTheGroup: an unresolvable mask reference
// (a gradient index past the table) hides the group - the design §3.5's
// transparent-black rule (never a failed frame).
func TestR52InvalidMaskHidesTheGroup(t *testing.T) {
	const w, h = 120, 80
	surface := r16Surface(t, "rendererspec r52 invalid mask", 120, 80)
	scene := r16Scene(t)
	window := gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 120, Height: 80}}
	group := r52MaskGroup(window, gpui.MaskKindGradient, 99, gpui.MaskModeAlpha)
	r52Effect(t, scene, group)
	r52Quad(t, scene, 0, 0, 120, 80, 0, 0.5, 1)
	r52EndEffect(t, scene, group)
	pixels := r52Render(t, surface, scene, w, h)
	r, g, b, a := pixelAt(pixels, w, 60, 40)
	if r < 240 || g < 240 || b < 240 || a != 255 {
		t.Fatalf("the invalid-masked group must hide its content (reads (%d,%d,%d,%d), want the white background)", r, g, b, a)
	}
}

// ---------------------------------------------------------------------------
// The drop-shadow filter op
// ---------------------------------------------------------------------------

// TestR52DropShadowZeroBlurReadback: a filter group with one
// drop-shadow op (offset 8,4, radius 0, black at 50%): the shadow is
// the group's alpha silhouette, offset and tinted, composited BENEATH
// the content quad - red inside the element, the 50% black over white
// in the offset-only region, white elsewhere.
func TestR52DropShadowZeroBlurReadback(t *testing.T) {
	const w, h = 120, 80
	surface := r16Surface(t, "rendererspec r52 drop shadow", 120, 80)
	scene := r16Scene(t)
	chain, err := scene.InsertFilterOpChain([]gpui.FilterOp{
		{OffsetX: 8, OffsetY: 4, BlurRadius: 0, Color: gpui.Rgba{A: 0.5}},
	})
	if err != nil {
		t.Fatalf("InsertFilterOpChain: %v", err)
	}
	fullMask := gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 4096, Height: 4096}}
	bounds := gpui.Bounds{Origin: gpui.Point{X: 20, Y: 10}, Size: gpui.Size{Width: 60, Height: 40}}
	filterChain := gpui.FilterChain{DropShadow: chain, DropShadowCount: 1}
	// A white backdrop quad (the filter group forces the offscreen
	// route, whose scene texture starts transparent).
	r52Quad(t, scene, 0, 0, 120, 80, 0, 1, 1)
	if err := scene.BeginFilterGroupChain(bounds, filterChain, gpui.Corners{}, 0, gpui.NewPaintContext(1, fullMask, 1)); err != nil {
		t.Fatalf("BeginFilterGroupChain: %v", err)
	}
	r52Quad(t, scene, 20, 10, 60, 40, 0, 0.5, 1)
	if err := scene.EndFilterGroupChain(bounds, filterChain, gpui.Corners{}, 0, gpui.NewPaintContext(1, fullMask, 1)); err != nil {
		t.Fatalf("EndFilterGroupChain: %v", err)
	}
	pixels := r52Render(t, surface, scene, w, h)
	// Inside the element: the red content over the shadow.
	r, g, b, a := pixelAt(pixels, w, 50, 30)
	if r < 240 || g > 15 || b > 15 || a != 255 {
		t.Fatalf("inside the element reads (%d,%d,%d,%d), want the red content", r, g, b, a)
	}
	// The offset-only region (inside the translated silhouette
	// (28,14)-(88,54), outside the element (20,10)-(80,50)): 50% black
	// over white = (128,128,128).
	r, g, b, a = pixelAt(pixels, w, 84, 48)
	if r < 115 || r > 141 || g < 115 || g > 141 || b < 115 || b > 141 || a != 255 {
		t.Fatalf("the offset shadow region reads (%d,%d,%d,%d), want ~(128,128,128)", r, g, b, a)
	}
	// Far outside: the white background.
	r, g, b, a = pixelAt(pixels, w, 110, 70)
	if r < 240 || g < 240 || b < 240 || a != 255 {
		t.Fatalf("outside the shadow reads (%d,%d,%d,%d), want white", r, g, b, a)
	}
}

// TestR52DropShadowBlurredReadback: a blurred drop-shadow (radius 8 ->
// sigma 4): deep inside the offset silhouette the blurred alpha is 1,
// so the shadow reads the tint over white exactly; the falloff darkens
// monotonically toward the silhouette's edge.
func TestR52DropShadowBlurredReadback(t *testing.T) {
	const w, h = 120, 80
	surface := r16Surface(t, "rendererspec r52 blurred drop shadow", 120, 80)
	scene := r16Scene(t)
	chain, err := scene.InsertFilterOpChain([]gpui.FilterOp{
		{OffsetX: 0, OffsetY: 40, BlurRadius: 8, Color: gpui.Rgba{A: 0.5}},
	})
	if err != nil {
		t.Fatalf("InsertFilterOpChain: %v", err)
	}
	fullMask := gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 4096, Height: 4096}}
	bounds := gpui.Bounds{Origin: gpui.Point{X: 30, Y: 20}, Size: gpui.Size{Width: 60, Height: 40}}
	filterChain := gpui.FilterChain{DropShadow: chain, DropShadowCount: 1}
	r52Quad(t, scene, 0, 0, 120, 80, 0, 1, 1)
	if err := scene.BeginFilterGroupChain(bounds, filterChain, gpui.Corners{}, 0, gpui.NewPaintContext(1, fullMask, 1)); err != nil {
		t.Fatalf("BeginFilterGroupChain: %v", err)
	}
	r52Quad(t, scene, 30, 20, 60, 40, 0, 0.5, 1)
	if err := scene.EndFilterGroupChain(bounds, filterChain, gpui.Corners{}, 0, gpui.NewPaintContext(1, fullMask, 1)); err != nil {
		t.Fatalf("EndFilterGroupChain: %v", err)
	}
	pixels := r52Render(t, surface, scene, w, h)
	// The element stays red (the content draws over the shadow).
	r, g, b, a := pixelAt(pixels, w, 60, 40)
	if r < 240 || g > 15 || b > 15 || a != 255 {
		t.Fatalf("inside the element reads (%d,%d,%d,%d), want the red content", r, g, b, a)
	}
	// Deep inside the offset silhouette (the element's bottom at y=60,
	// offset +40: the silhouette spans y 60..100; 15px from its edges at
	// (60,75)): the blurred alpha is ~1, the shadow reads the tint.
	r, g, b, a = pixelAt(pixels, w, 60, 75)
	if r < 115 || r > 141 || g < 115 || g > 141 || b < 115 || b > 141 {
		t.Fatalf("the blurred shadow's interior reads (%d,%d,%d,%d), want ~(128,128,128)", r, g, b, a)
	}
	// The falloff: nearer the silhouette's edge (y=62, 2px inside the
	// offset silhouette's top) the shadow is FAINTER than deep inside
	// (the red channel rises toward white); it stays substantial.
	near, _, _, _ := pixelAt(pixels, w, 60, 62)
	deep, _, _, _ := pixelAt(pixels, w, 60, 75)
	if near <= deep {
		t.Fatalf("the blurred shadow must fade toward the edge (near=%d deep=%d)", near, deep)
	}
	if near > 200 {
		t.Fatalf("2px inside the offset silhouette the shadow must be substantial (near=%d)", near)
	}
}
