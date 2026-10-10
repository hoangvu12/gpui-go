package native

import (
	"testing"
)

// TestGlyphManifestIdentity pins the embedded manifest's glyph
// resolution record (ticket10): the renderer contract's etagere 0.2.15
// pin, the rasterizer's windows-numerics 0.3.1 and the error-type
// anyhow resolution, plus the cumulative capability mask 0x3FF and
// native revision 9 (ticket16 added bit 7, scene-draw-paths; ticket17
// added bit 8, image-codecs-image-0-25; ticket19 added bit 9,
// svg-resvg-0-48).
func TestGlyphManifestIdentity(t *testing.T) {
	auth := testAuthority(t)
	wantMask := capBootstrapBufferRoundTrip | capLayoutTaffy | capRendererD3D11 |
		capSceneKernel | capTextParley | capGlyphRaster | capAtlasD3D11 | capSceneDrawPaths | capImageCodecs | capSvgResvg
	if auth.CapabilitiesMask != wantMask {
		t.Errorf("manifest capabilities mask = %#x, want %#x (bootstrap + layout + renderer + scene + text + glyph + atlas + scene-draw + image + svg)", auth.CapabilitiesMask, wantMask)
	}
	if auth.NativeRevision != nativeRevision {
		t.Errorf("manifest native revision = %d, want %d", auth.NativeRevision, nativeRevision)
	}
	if auth.Glyph == nil {
		t.Fatal("manifest missing the glyph resolution record")
	}
	if auth.Glyph.Etagere.Version != "0.2.15" || !auth.Glyph.Etagere.PinSatisfied {
		t.Errorf("manifest etagere version = %q pin=%v, want 0.2.15 true (the renderer contract pin)", auth.Glyph.Etagere.Version, auth.Glyph.Etagere.PinSatisfied)
	}
	if auth.Glyph.WindowsNumerics.Version != "0.3.1" || !auth.Glyph.WindowsNumerics.PinSatisfied {
		t.Errorf("manifest windows-numerics version = %q pin=%v, want 0.3.1 true", auth.Glyph.WindowsNumerics.Version, auth.Glyph.WindowsNumerics.PinSatisfied)
	}
	if auth.Glyph.Anyhow.Version == "" {
		t.Errorf("manifest anyhow resolution is empty")
	}
}

// TestGlyphAndAtlasServiceFetch pins the service-table fetch path: the
// capability gate rejects artifacts without the bits, and the tables
// validate against the Go mirrors (the record-size self-checks).
func TestGlyphAndAtlasServiceFetch(t *testing.T) {
	lib, err := Load(Options{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	t.Cleanup(func() { _ = lib.Close() })

	glyph, err := lib.Glyph()
	if err != nil {
		t.Fatalf("Library.Glyph: %v", err)
	}
	if got := glyph.MaxRasterBytes(); got != 64<<20 {
		t.Errorf("max raster bytes = %d, want %d", got, 64<<20)
	}
	if x, y := glyph.SubpixelVariants(); x != 4 || y != 1 {
		t.Errorf("subpixel variants = (%d, %d), want (4, 1)", x, y)
	}
	if err := glyph.PanicProbe(); err != nil {
		t.Fatalf("glyph panic probe: %v", err)
	}

	atlas, err := lib.Atlas()
	if err != nil {
		t.Fatalf("Library.Atlas: %v", err)
	}
	if got := atlas.DefaultAtlasSize(); got != 1024 {
		t.Errorf("default atlas size = %d, want 1024", got)
	}
	if got := atlas.MaxAtlasSize(); got != 16384 {
		t.Errorf("max atlas size = %d, want 16384", got)
	}
	if got := atlas.MaxAtlasHandles(); got != 8 {
		t.Errorf("max atlas handles = %d, want 8", got)
	}
	if err := atlas.PanicProbe(); err != nil {
		t.Fatalf("atlas panic probe: %v", err)
	}
}
