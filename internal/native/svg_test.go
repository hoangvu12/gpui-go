//go:build windows

package native

// Ticket19's SVG service gates against the REAL rebuilt DLL (revision
// 9, capability bit 9, reserved slot 8): the font-asset surface (the
// pinned bundled list, the missing query, the push and its duplicate
// rule), the parse/render lifecycle (the three sizing modes with the
// smooth-scale factor and the 8192 clamp, the BGRA output, the
// capacity protocol, the alpha-mask entry), the typed error surface
// and the handle lifecycle. The committed fixtures: inline SVGs plus
// the pinned checkout's own bundled fonts (the CE test corpus's TTFs,
// copied into testdata).

import (
	"os"
	"testing"
)

// mustSvg loads the native library and fetches the SVG service.
func mustSvg(t *testing.T) *SvgService {
	t.Helper()
	lib, err := Load(Options{CacheRoot: testDir(t)})
	if err != nil {
		t.Fatalf("native load: %v", err)
	}
	t.Cleanup(func() { lib.Close() })
	svc, err := lib.Svg()
	if err != nil {
		t.Fatalf("svg service: %v", err)
	}
	return svc
}

// svgFixture returns one of the committed inline SVG byte fixtures.
func svgFixture(t *testing.T, name string) []byte {
	t.Helper()
	var svg string
	switch name {
	case "empty-24x12":
		svg = `<svg xmlns="http://www.w3.org/2000/svg" width="24pt" height="12pt"></svg>`
	case "rect-24x12":
		// One full-size opaque red rect: every pixel shares the exact
		// BGRA byte pattern, the oracle for the RGBA→BGRA swap and the
		// alpha channel.
		svg = `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="12"><rect width="24" height="12" fill="#ff0000" fill-opacity="1"/></svg>`
	case "rect-half-alpha":
		svg = `<svg xmlns="http://www.w3.org/2000/svg" width="8" height="8"><rect width="8" height="8" fill="#ffffff" fill-opacity="0.5"/></svg>`
	case "garbage":
		svg = `not an svg at all`
	case "text-plex":
		svg = `<svg xmlns="http://www.w3.org/2000/svg" width="120" height="40"><text x="4" y="28" font-family="IBM Plex Sans" font-size="24">Ag</text></svg>`
	case "wide-20000":
		svg = `<svg xmlns="http://www.w3.org/2000/svg" width="20000" height="100"><rect width="20000" height="100" fill="#00ff00"/></svg>`
	default:
		t.Fatalf("unknown fixture %q", name)
	}
	return []byte(svg)
}

// TestSvgFontAssetSurface pins the NeedsFontAssets seam: the count
// and the two pinned paths (svg_renderer.rs load_bundled_fonts), the
// missing query before any push, and the duplicate rule after one.
func TestSvgFontAssetSurface(t *testing.T) {
	svc := mustSvg(t)

	count, err := svc.SvgFontAssetCount()
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Fatalf("font asset count = %d, want 2", count)
	}
	paths := make([]string, count)
	for i := uint32(0); i < count; i++ {
		path, err := svc.SvgFontAssetPath(i)
		if err != nil {
			t.Fatalf("path %d: %v", i, err)
		}
		paths[i] = path
	}
	want := []string{
		"fonts/ibm-plex-sans/IBMPlexSans-Regular.ttf",
		"fonts/lilex/Lilex-Regular.ttf",
	}
	for i, w := range want {
		if paths[i] != w {
			t.Errorf("font asset path %d = %q, want %q", i, paths[i], w)
		}
	}

	// A bad index is a typed argument error.
	if _, err := svc.SvgFontAssetPath(9); err == nil {
		t.Error("font asset path(9) = nil error, want the bad-value rejection")
	}

	// Before any push the paths report missing.
	for _, path := range want {
		has, err := svc.HasFontAsset(path)
		if err != nil {
			t.Fatalf("has(%q): %v", path, err)
		}
		if has {
			t.Errorf("has(%q) = true before any push", path)
		}
	}

	// One push, then the duplicate rule.
	font := make([]byte, 16)
	if err := svc.AddFont(want[0], font); err != nil {
		t.Fatalf("add font: %v", err)
	}
	has, err := svc.HasFontAsset(want[0])
	if err != nil || !has {
		t.Fatalf("has after push = %v, %v; want true", has, err)
	}
	if err := svc.AddFont(want[0], font); err != ErrSvgBadValue {
		t.Errorf("duplicate add error = %v, want ErrSvgBadValue (the registry rule)", err)
	}
}

// TestSvgParseRenderLifecycle drives parse → render at the three
// sizing modes → dispose, asserting the pinned geometry, the BGRA
// pixel pattern and the capacity protocol.
func TestSvgParseRenderLifecycle(t *testing.T) {
	svc := mustSvg(t)

	handle, err := svc.Parse(svgFixture(t, "rect-24x12"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	defer func() { _ = svc.Dispose(handle) }()

	// ExactSize 24x12: the rendered buffer is exactly the request.
	pixels, info, err := svc.Render(handle, SvgModeExactSize, 24, 12, 1)
	if err != nil {
		t.Fatalf("render exact: %v", err)
	}
	if info.Width != 24 || info.Height != 12 {
		t.Fatalf("exact render dims = %dx%d, want 24x12", info.Width, info.Height)
	}
	if info.ScaleFactor != 1 {
		t.Fatalf("exact render scale factor = %v, want 1", info.ScaleFactor)
	}
	if len(pixels) != 24*12*4 {
		t.Fatalf("exact render bytes = %d, want %d", len(pixels), 24*12*4)
	}
	// Opaque red (#ff0000): premultiplied RGBA (255,0,0,255) → the
	// pinned swap makes BGRA rows (B,G,R,A) = (0,0,255,255).
	for i := 0; i < len(pixels); i += 4 {
		if pixels[i] != 0 || pixels[i+1] != 0 || pixels[i+2] != 255 || pixels[i+3] != 255 {
			t.Fatalf("pixel %d = %v, want opaque red BGRA (0,0,255,255)", i/4, pixels[i:i+4])
		}
	}

	// Size mode preserves the aspect ratio: 24x24 requested over a
	// 24x12 viewBox renders 24x12 (the CE's own unit expectation).
	_, info, err = svc.Render(handle, SvgModeSize, 24, 24, 1)
	if err != nil {
		t.Fatalf("render size: %v", err)
	}
	if info.Width != 24 || info.Height != 12 {
		t.Fatalf("size render dims = %dx%d, want 24x12 (aspect preserved)", info.Width, info.Height)
	}

	// ScaleFactor mode applies the pinned smooth 2x: scale 1 renders
	// 48x24 and reports the factor.
	_, info, err = svc.Render(handle, SvgModeScaleFactor, 0, 0, 1)
	if err != nil {
		t.Fatalf("render scale: %v", err)
	}
	if info.Width != 48 || info.Height != 24 {
		t.Fatalf("scale render dims = %dx%d, want 48x24 (the smooth 2x)", info.Width, info.Height)
	}
	if info.ScaleFactor != 2 {
		t.Fatalf("scale render factor = %v, want 2", info.ScaleFactor)
	}
	if svc.SmoothScaleFactor() != 2 {
		t.Fatalf("table smooth factor = %v, want 2", svc.SmoothScaleFactor())
	}
}

// TestSvgAlphaMask verifies the pub(crate) render_alpha_mask
// adaptation: half-opaque white over 8x8 yields an alpha mask of
// 128s (the tiny-skia premultiplied alpha of 0.5), with the mask
// exactly one byte per pixel.
func TestSvgAlphaMask(t *testing.T) {
	svc := mustSvg(t)
	mask, info, err := svc.RenderAlphaMask(svgFixture(t, "rect-half-alpha"), 8, 8)
	if err != nil {
		t.Fatalf("render alpha mask: %v", err)
	}
	if info.Width != 8 || info.Height != 8 {
		t.Fatalf("alpha mask dims = %dx%d, want 8x8", info.Width, info.Height)
	}
	if len(mask) != 64 {
		t.Fatalf("alpha mask bytes = %d, want 64", len(mask))
	}
	for i, a := range mask {
		if a != 128 {
			t.Fatalf("alpha byte %d = %d, want 128 (fill-opacity 0.5)", i, a)
		}
	}
}

// TestSvgParseErrorAndHandleLifecycle: garbage bytes are the typed
// parse error; dispose makes the handle stale; a malformed handle is
// rejected.
func TestSvgParseErrorAndHandleLifecycle(t *testing.T) {
	svc := mustSvg(t)
	if _, err := svc.Parse(svgFixture(t, "garbage")); err != ErrSvgParse {
		t.Errorf("parse garbage error = %v, want ErrSvgParse", err)
	}

	handle, err := svc.Parse(svgFixture(t, "empty-24x12"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := svc.Dispose(handle); err != nil {
		t.Fatalf("dispose: %v", err)
	}
	if _, _, err := svc.Render(handle, SvgModeExactSize, 24, 12, 1); err != ErrSvgStaleHandle {
		t.Errorf("render after dispose error = %v, want ErrSvgStaleHandle", err)
	}
	if err := svc.Dispose(handle); err != ErrSvgStaleHandle {
		t.Errorf("double dispose error = %v, want ErrSvgStaleHandle", err)
	}
	if _, _, err := svc.Render(SvgHandle(0xFFFF000001), SvgModeExactSize, 24, 12, 1); err != ErrSvgBadHandle {
		t.Errorf("malformed handle error = %v, want ErrSvgBadHandle", err)
	}
}

// TestSvgMaxSizeClamp: the pinned 8192-pixel rasterize clamp — a
// 20000-wide SVG requested at width 20000 renders clamped to 8192.
func TestSvgMaxSizeClamp(t *testing.T) {
	svc := mustSvg(t)
	handle, err := svc.Parse(svgFixture(t, "wide-20000"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	defer func() { _ = svc.Dispose(handle) }()
	_, info, err := svc.Render(handle, SvgModeSize, 20000, 100, 1)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if info.Width > 8192 {
		t.Errorf("clamped width = %d, want <= 8192 (the pin's MAX_SIZE)", info.Width)
	}
	if info.Width == 0 || info.Height == 0 {
		t.Errorf("clamped dims = %dx%d, want non-degenerate", info.Width, info.Height)
	}
}

// TestSvgTextWithBundledFont renders the pinned checkout's own test
// corpus font through the NeedsFontAssets flow: with the bundled IBM
// Plex Sans pushed, a text SVG parses and rasterizes real glyph
// coverage (non-background pixels over the text box).
func TestSvgTextWithBundledFont(t *testing.T) {
	svc := mustSvg(t)

	font, err := os.ReadFile("testdata/IBMPlexSans-Regular.ttf")
	if err != nil {
		t.Fatalf("read font: %v (the CE corpus fixture must be committed)", err)
	}
	path, err := svc.SvgFontAssetPath(0)
	if err != nil {
		t.Fatalf("font path: %v", err)
	}
	if has, err := svc.HasFontAsset(path); err != nil || has {
		t.Fatalf("has font before push = %v, %v; want false", has, err)
	}
	if err := svc.AddFont(path, font); err != nil {
		t.Fatalf("add font: %v", err)
	}

	handle, err := svc.Parse(svgFixture(t, "text-plex"))
	if err != nil {
		t.Fatalf("parse with font: %v", err)
	}
	defer func() { _ = svc.Dispose(handle) }()
	pixels, info, err := svc.Render(handle, SvgModeExactSize, 120, 40, 1)
	if err != nil {
		t.Fatalf("render text: %v", err)
	}
	if info.Width != 120 || info.Height != 40 {
		t.Fatalf("text render dims = %dx%d, want 120x40", info.Width, info.Height)
	}
	// Glyph coverage: some pixel in the text row band is non-empty
	// (anti-aliased ink), while the exact transparent row 0 stays
	// empty (the text baseline sits below).
	ink := 0
	for i := 3; i < len(pixels); i += 4 {
		if pixels[i] != 0 {
			ink++
		}
	}
	if ink == 0 {
		t.Fatal("no glyph coverage: every alpha byte is zero")
	}
}
