package svgspec

// This file holds ticket19's SVG model gates through the application
// renderer (svg_renderer.rs's public surface): parse/render at the
// three sizing modes with the smooth-scale math (the CE's own unit
// expectations), the premultiplied-RGBA → straight-BGRA oracle
// (swap, truncating unpremultiply, transparent pixels), the
// render_alpha_mask semantics (the coverage oracle, the registry
// fallback, the pinned Ok(None) miss, the zero-size rejection), the
// typed parse error on garbage, render_single_frame, resource
// retirement (native handles disposed, atlas entries removed exactly
// once) and the into_matrix composition.
//
// These gates are order-tolerant: parseResolved handles both the
// immediate and the suspended font-orchestration paths.

import (
	"errors"
	"testing"

	"gpui-go/gpui"
)

// The shape fixtures (the native suite's oracle documents; pixel
// dimensions so the smooth-factor math reads directly).
const (
	empty24x12    = `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="12"></svg>`
	rect24x12     = `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="12"><rect width="24" height="12" fill="#ff0000" fill-opacity="1"/></svg>`
	rectHalfAlpha = `<svg xmlns="http://www.w3.org/2000/svg" width="8" height="8"><rect width="8" height="8" fill="#ffffff" fill-opacity="0.5"/></svg>`
	garbage       = `not an svg at all`
)

// TestSvgParseRenderThreeModes pins the pinned SvgSize semantics
// through the application renderer (svg_renderer.rs's own unit
// expectations: renders_parsed_svg_at_requested_size and
// preserves_aspect_ratio_for_width_constrained_size, plus the
// render_parsed ScaleFactor arm with SMOOTH_SVG_SCALE_FACTOR).
func TestSvgParseRenderThreeModes(t *testing.T) {
	f := newSvgFixture(t, bothFonts(t))
	svg := f.parseResolved(t, []byte(empty24x12))
	defer func() { _ = svg.Dispose() }()

	// ExactSize: the rendered buffer is exactly the request.
	image, err := f.renderer.RenderParsed(svg, gpui.ExactSizeSvg(24, 12))
	if err != nil {
		t.Fatalf("render exact: %v", err)
	}
	if w, h := image.Image.Size(0); w != 24 || h != 12 {
		t.Fatalf("exact render = %dx%d, want 24x12", w, h)
	}
	if image.ScaleFactor != 1 {
		t.Fatalf("exact render scale factor = %v, want 1", image.ScaleFactor)
	}
	if got := len(image.Image.Frames[0].Pixels); got != 24*12*4 {
		t.Fatalf("exact render bytes = %d, want %d", got, 24*12*4)
	}
	if d := image.Image.Delay(0); d != 0 {
		t.Fatalf("the static frame delay = %v, want 0 (the pinned Frame::new)", d)
	}

	// Size preserves the aspect ratio: a 24x24 request over the 24x12
	// document renders 24x12.
	_, size, _ := renderSizeOf(t, f, svg, gpui.SizeSvg(24, 24))
	if size[0] != 24 || size[1] != 12 {
		t.Fatalf("size render = %dx%d, want 24x12 (aspect preserved)", size[0], size[1])
	}

	// ScaleFactor applies the pinned smooth 2x: scale 1 renders 48x24
	// and reports the factor.
	_, size, factor := renderSizeOf(t, f, svg, gpui.ScaleFactorSvg(1))
	if size[0] != 48 || size[1] != 24 {
		t.Fatalf("scale render = %dx%d, want 48x24 (the smooth 2x)", size[0], size[1])
	}
	if factor != 2 {
		t.Fatalf("scale render factor = %v, want 2", factor)
	}
	// A factor of 2 doubles again.
	_, size, factor = renderSizeOf(t, f, svg, gpui.ScaleFactorSvg(2))
	if size[0] != 96 || size[1] != 48 {
		t.Fatalf("scale-2 render = %dx%d, want 96x48", size[0], size[1])
	}
	if factor != 2 {
		t.Fatalf("scale-2 render factor = %v, want 2", factor)
	}
	if got := f.renderer.SmoothScaleFactor(); got != 2 {
		t.Fatalf("the renderer smooth factor = %v, want 2 (the pinned constant)", got)
	}

	// render_single_frame: parse + render at a factor, one frame.
	single, err := f.renderer.RenderSingleFrame([]byte(empty24x12), 1)
	if err != nil {
		t.Fatalf("RenderSingleFrame: %v", err)
	}
	if w, h := single.Image.Size(0); w != 48 || h != 24 {
		t.Fatalf("RenderSingleFrame = %dx%d, want 48x24", w, h)
	}
	if single.ScaleFactor != 2 {
		t.Fatalf("RenderSingleFrame factor = %v, want 2", single.ScaleFactor)
	}
}

// renderSizeOf renders at a size and returns the image, dimensions and
// scale factor.
func renderSizeOf(t *testing.T, f *svgFixture, svg *gpui.ParsedSvg, size gpui.SvgSize) (*gpui.SvgImage, [2]int, float32) {
	t.Helper()
	image, err := f.renderer.RenderParsed(svg, size)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	w, h := image.Image.Size(0)
	return image, [2]int{w, h}, image.ScaleFactor
}

// TestSvgRenderedPixelOracle pins the pinned conversion from
// premultiplied RGBA to straight BGRA (swap_rgba_pa_to_bgra, color.rs
// lines 38-47): the opaque red rect swaps to (0, 0, 255, 255), the
// half-alpha white rect unpremultiplies with truncation to
// (255, 255, 255, 128), and transparent pixels stay zeroed.
func TestSvgRenderedPixelOracle(t *testing.T) {
	f := newSvgFixture(t, bothFonts(t))

	red, err := f.renderer.RenderSingleFrame([]byte(rect24x12), 1)
	if err != nil {
		t.Fatalf("render red: %v", err)
	}
	pixels := red.Image.Frames[0].Pixels
	for i := 0; i < len(pixels); i += 4 {
		if pixels[i] != 0 || pixels[i+1] != 0 || pixels[i+2] != 255 || pixels[i+3] != 255 {
			t.Fatalf("red pixel %d = %v, want the swapped opaque BGRA (0, 0, 255, 255)", i/4, pixels[i:i+4])
		}
	}

	// Half-opaque white: the premultiplied (128, 128, 128, 128) row
	// unpremultiplies through the pinned f32 arithmetic with
	// truncation — 128 / (128/255) is 254.99998 in f32, so the `as u8`
	// truncation yields 254, not 255 (the alpha stays 128).
	// (Renders at the exact size; the ScaleFactor mode would double it.)
	halfSvg := f.parseResolved(t, []byte(rectHalfAlpha))
	half, err := f.renderer.RenderParsed(halfSvg, gpui.ExactSizeSvg(8, 8))
	if err != nil {
		t.Fatalf("render half: %v", err)
	}
	_ = halfSvg.Dispose()
	pixels = half.Image.Frames[0].Pixels
	if len(pixels) != 8*8*4 {
		t.Fatalf("half-alpha bytes = %d, want %d", len(pixels), 8*8*4)
	}
	for i := 0; i < len(pixels); i += 4 {
		want := []byte{254, 254, 254, 128}
		if pixels[i] != want[0] || pixels[i+1] != want[1] || pixels[i+2] != want[2] || pixels[i+3] != want[3] {
			t.Fatalf("half-alpha pixel %d = %v, want the truncating unpremultiply %v", i/4, pixels[i:i+4], want)
		}
	}

	// Transparent pixels: the empty document renders fully transparent
	// zeros (the conversion's alpha-zero guard).
	empty, err := f.renderer.RenderSingleFrame([]byte(empty24x12), 1)
	if err != nil {
		t.Fatalf("render empty: %v", err)
	}
	pixels = empty.Image.Frames[0].Pixels
	for i := 0; i < len(pixels); i += 4 {
		if pixels[i] != 0 || pixels[i+1] != 0 || pixels[i+2] != 0 || pixels[i+3] != 0 {
			t.Fatalf("transparent pixel %d = %v, want zeros", i/4, pixels[i:i+4])
		}
	}
}

// TestSvgAlphaMask pins the render_alpha_mask adaptation: the coverage
// oracle (fill-opacity 0.5 → alpha 128, one byte per pixel), the
// renderer's registry fallback for a nil bytes slice, the pinned
// Ok(None) on a registry miss, and the zero-size rejection.
func TestSvgAlphaMask(t *testing.T) {
	f := newSvgFixture(t, bothFonts(t))

	// The coverage oracle: premultiplied alpha 0.5 is 128.
	mask, width, height, err := f.renderer.RenderAlphaMask(gpui.RenderSvgParams{
		Path: "half",
		Size: gpui.Size{Width: 8, Height: 8},
	}, []byte(rectHalfAlpha))
	if err != nil {
		t.Fatalf("render alpha mask: %v", err)
	}
	if width != 8 || height != 8 {
		t.Fatalf("alpha mask = %dx%d, want 8x8", width, height)
	}
	if len(mask) != 64 {
		t.Fatalf("alpha mask bytes = %d, want 64 (one byte per pixel)", len(mask))
	}
	for i, a := range mask {
		if a != 128 {
			t.Fatalf("alpha byte %d = %d, want 128 (fill-opacity 0.5)", i, a)
		}
	}

	// The registry fallback: nil bytes resolve the params' path through
	// the renderer's asset registry.
	if err := f.app.Assets().Insert("icons/half.svg", gpui.PreLoaded([]byte(rectHalfAlpha))); err != nil {
		t.Fatalf("registry insert: %v", err)
	}
	mask, width, height, err = f.renderer.RenderAlphaMask(gpui.RenderSvgParams{
		Path: "icons/half.svg",
		Size: gpui.Size{Width: 8, Height: 8},
	}, nil)
	if err != nil {
		t.Fatalf("registry alpha mask: %v", err)
	}
	if mask == nil || width != 8 || height != 8 || len(mask) != 64 {
		t.Fatalf("the registry fallback mask = (%v, %d, %d), want the 64-byte oracle", mask == nil, width, height)
	}

	// The pinned Ok(None): a registry miss reports no mask and no
	// error — nothing to draw.
	mask, _, _, err = f.renderer.RenderAlphaMask(gpui.RenderSvgParams{
		Path: "icons/absent.svg",
		Size: gpui.Size{Width: 8, Height: 8},
	}, nil)
	if err != nil || mask != nil {
		t.Fatalf("the registry miss = (%v, %v), want (nil, nil) — the pinned Ok(None)", mask != nil, err)
	}

	// The zero-size rejection (the pin's "can't render at a zero size").
	_, _, _, err = f.renderer.RenderAlphaMask(gpui.RenderSvgParams{
		Path: "half",
		Size: gpui.Size{},
	}, []byte(rectHalfAlpha))
	if !errors.Is(err, gpui.ErrSvgBadValue) {
		t.Fatalf("the zero-size error = %v, want the typed bad-value rejection", err)
	}
}

// TestSvgParseGarbage pins the typed parse error: garbage bytes fail
// the parse (the pinned usvg::Error surface).
func TestSvgParseGarbage(t *testing.T) {
	f := newSvgFixture(t, bothFonts(t))
	outcome, done := f.renderer.ParseSvg(f.app, f.ta.RootScope(), []byte(garbage), func(o gpui.SvgParseOutcome, cx *gpui.App) {
		t.Errorf("garbage delivered asynchronously: %+v", o)
	})
	if !done {
		t.Fatal("the garbage parse did not finish")
	}
	if !errors.Is(outcome.Err, gpui.ErrSvgParse) {
		t.Fatalf("the garbage parse error = %v, want the typed svg parse error", outcome.Err)
	}
}

// TestSvgResourceRetirement pins the resource retirement: parsed
// handles dispose and go stale (the native's typed error), and the
// atlas's SVG mask entry is removed exactly once (the first removal
// deallocates, the absent-key removal is the pinned no-op, the query
// misses).
func TestSvgResourceRetirement(t *testing.T) {
	f := newSvgFixture(t, bothFonts(t))
	svg := f.parseResolved(t, []byte(textSvg))
	defer func() { _ = svg.Dispose() }()

	// The mask renders and uploads under the params identity.
	params := gpui.RenderSvgParams{Path: "text", Size: gpui.Size{Width: 120, Height: 40}}
	mask, width, height, err := f.renderer.RenderAlphaMask(params, []byte(textSvg))
	if err != nil {
		t.Fatalf("render alpha mask: %v", err)
	}
	atlas := f.app.ImageAtlas()
	if atlas == nil {
		t.Fatal("the application image atlas is unavailable")
	}
	tile, err := atlas.InsertSvgMask(params, width, height, mask)
	if err != nil {
		t.Fatalf("InsertSvgMask: %v", err)
	}
	if tile.TextureKind != gpui.AtlasTextureMonochrome {
		t.Fatalf("the mask tile kind = %d, want the monochrome pool", tile.TextureKind)
	}
	count, err := atlas.TileCount()
	if err != nil {
		t.Fatalf("tile count: %v", err)
	}
	if count != 1 {
		t.Fatalf("tile count after the insert = %d, want 1", count)
	}
	// The cache hit: the same identity returns the same tile.
	again, err := atlas.InsertSvgMask(params, width, height, mask)
	if err != nil {
		t.Fatalf("InsertSvgMask (cached): %v", err)
	}
	if again.TileID != tile.TileID || again.TextureIndex != tile.TextureIndex {
		t.Fatalf("the cached tile = %+v, want the same identity as %+v", again, tile)
	}

	// The handle disposal: stale afterwards.
	if err := svg.Dispose(); err != nil {
		t.Fatalf("dispose: %v", err)
	}
	if _, err := f.renderer.RenderParsed(svg, gpui.ExactSizeSvg(120, 40)); err == nil {
		t.Fatal("rendering after dispose must fail")
	}

	// The removal: exactly once — the first deallocates, the second is
	// the pinned absent-key no-op, and the query misses afterwards.
	if err := atlas.RemoveSvgMask(params); err != nil {
		t.Fatalf("RemoveSvgMask: %v", err)
	}
	if err := atlas.RemoveSvgMask(params); err != nil {
		t.Fatalf("the second RemoveSvgMask = %v, want the absent-key no-op", err)
	}
	if _, found, err := atlas.SvgMaskTile(params); err != nil || found {
		t.Fatalf("the removed identity's query = (%v, %v), want (false, nil)", found, err)
	}
	count, err = atlas.TileCount()
	if err != nil {
		t.Fatalf("tile count: %v", err)
	}
	if count != 0 {
		t.Fatalf("tile count after the removal = %d, want 0", count)
	}
}

// TestSvgTransformationIntoMatrix pins the pinned into_matrix
// composition (svg.rs lines 294-312): reading the multiplications from
// the bottom, translate by -center*scale, scale, rotate, then
// translate by (center + translation)*scale.
func TestSvgTransformationIntoMatrix(t *testing.T) {
	center := gpui.Point{X: 10, Y: 5}

	// The unit transformation around the center is the identity.
	if m := gpui.SvgTransformationDefault().IntoMatrix(center, 2); m != gpui.UnitTransformation() {
		t.Fatalf("the default transformation = %+v, want the unit matrix", m)
	}

	// A translation composes to the scaled translation (the matrix
	// translation is A + D with a unit rotation/scale).
	m := gpui.SvgTranslateTransformation(gpui.Point{X: 3, Y: 1}).IntoMatrix(center, 2)
	if m.Translation != [2]float32{6, 2} {
		t.Fatalf("the translation matrix = %v, want [6 2]", m.Translation)
	}
	if m.RotationScale != [2][2]float32{{1, 0}, {0, 1}} {
		t.Fatalf("the translation matrix rotation/scale = %v, want the identity", m.RotationScale)
	}

	// A scale of 2x2 around the center at scale factor 2: the center
	// maps to the origin (p - 20,10 scaled then re-translated):
	// rotation/scale 2I, translation (20, 10) + 2·(-20, -10) =
	// (-20, -10).
	m = gpui.SvgScaleTransformation(gpui.Size{Width: 2, Height: 2}).IntoMatrix(center, 2)
	if m.RotationScale != [2][2]float32{{2, 0}, {0, 2}} {
		t.Fatalf("the scale matrix rotation/scale = %v, want 2I", m.RotationScale)
	}
	if m.Translation != [2]float32{-20, -10} {
		t.Fatalf("the scale matrix translation = %v, want [-20 -10]", m.Translation)
	}

	// A rotation of pi/2 around the center at scale factor 1: the
	// affine translation is A + R·D = (10, 5) + (5, -10) = (15, -5) —
	// the center maps to itself under the composed matrix — and the
	// rotation/scale approaches the quarter turn [[0, -1], [1, 0]] (the
	// f32 trig's last-ulp deviation from Rust's f32::cos is the
	// recorded adaptation).
	m = gpui.SvgRotateTransformation(float32(math_Pi/2)).IntoMatrix(center, 1)
	if m.Translation != [2]float32{15, -5} {
		t.Fatalf("the pure rotation translation = %v, want [15 -5] (A + R·D)", m.Translation)
	}
	near := func(got, want float32) bool {
		diff := got - want
		if diff < 0 {
			diff = -diff
		}
		return diff <= 1e-6
	}
	if !near(m.RotationScale[0][0], 0) || !near(m.RotationScale[0][1], -1) ||
		!near(m.RotationScale[1][0], 1) || !near(m.RotationScale[1][1], 0) {
		t.Fatalf("the rotation matrix = %v, want the pi/2 quarter turn", m.RotationScale)
	}
}

// math_Pi is math.Pi (kept local to avoid importing math for two
// constants).
const math_Pi = 3.141592653589793

// TestSvgTextModelRender pins the text render at the model level: with
// the bundled fonts pushed, the text document's render carries real
// glyph coverage (ink in the text band, empty above the baseline).
func TestSvgTextModelRender(t *testing.T) {
	f := newSvgFixture(t, bothFonts(t))
	svg := f.parseResolved(t, []byte(textSvg))
	defer func() { _ = svg.Dispose() }()
	image, err := f.renderer.RenderParsed(svg, gpui.ExactSizeSvg(120, 40))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if w, h := image.Image.Size(0); w != 120 || h != 40 {
		t.Fatalf("the text render = %dx%d, want 120x40", w, h)
	}
	pixels := image.Image.Frames[0].Pixels
	ink := 0
	for i := 3; i < len(pixels); i += 4 {
		if pixels[i] != 0 {
			ink++
		}
	}
	if ink == 0 {
		t.Fatal("the text render carries no glyph coverage")
	}
	// The text baseline sits at y=28: row 0 (above the text) is empty.
	for x := 0; x < 120; x++ {
		i := x * 4
		if pixels[i] != 0 || pixels[i+1] != 0 || pixels[i+2] != 0 || pixels[i+3] != 0 {
			t.Fatalf("the row above the text carries ink at x=%d", x)
		}
	}
}
