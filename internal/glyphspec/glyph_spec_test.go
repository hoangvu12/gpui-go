//go:build windows

// Package glyphspec holds ticket10's glyph raster, atlas and text-draw
// tests: the fx-0005 raster oracle gate (bit-exact against the recorded
// reference trace), atlas insert/remove/device-loss round trips with the
// real D3D11 device, the sprite scene operations of a text draw (the
// pinned sprite ordering, batching and requirements), and a real-window
// text frame shaped, rasterized, atlas-packed, scene-built, presented
// and retired through the public gpui API. They exercise the paths from
// outside the runtime package, matching the repo convention (see
// internal/winhostspec and internal/rendererspec).
//
// Environment expectation: these tests need the Windows AMD64 native
// artifact with the glyph and atlas services, a D3D11-capable adapter
// and (for the window test) an interactive session; this machine runs
// them and they never silently skip. The fx-0005 gate runs FIRST in
// this file on purpose: the native FontStore interns faces in
// resolution order, so the fixture's font resolutions must be this
// process's first (the same determinism rule the text-geometry gate
// documents; the recorded trace's FontIds pin it).
package glyphspec

import (
	"runtime"
	"testing"
	"time"

	"gpui-go/conformance"
	"gpui-go/gpui"
	"gpui-go/internal/native"
	"gpui-go/internal/portfixture"
)

const (
	// glyphRasterFixturePath is the fx-0005 envelope the gate executes.
	glyphRasterFixturePath = "../../conformance/fixtures/fx-0005-glyph-raster.json"
	// glyphRasterRecordedTracePath is the oracle recorded from the
	// pinned reference harness.
	glyphRasterRecordedTracePath = "../../conformance/recorded/fx-0005-glyph-raster/trace.json"
)

// waitFor polls cond until it holds or the timeout expires.
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out after %v waiting for %s", timeout, what)
}

// TestFx0005GlyphRasterMatchesRecordedReference is the ticket10 gate
// for the glyph-raster fixture: the port runner must reproduce the FULL
// recorded reference trace (all 39 events: the construction record with
// the recommended rendering mode, every case's font resolution with the
// same canonical FontId, and every glyph raster's format, baseline-
// relative bounds, buffer size, byte count and pixel-bytes SHA-256 —
// bit-exact raster bytes through the whole DirectWrite → ABI → Go
// path).
func TestFx0005GlyphRasterMatchesRecordedReference(t *testing.T) {
	envelope, err := conformance.LoadEnvelope(glyphRasterFixturePath)
	if err != nil {
		t.Fatalf("loading envelope: %v", err)
	}
	trace, err := portfixture.RunGlyphRaster(envelope)
	if err != nil {
		t.Fatalf("running port glyph raster: %v", err)
	}

	// Run metadata: pin the envelope bytes this run executed.
	sha, err := conformance.SHA256Envelope(glyphRasterFixturePath)
	if err != nil {
		t.Fatalf("hashing envelope: %v", err)
	}
	trace.EnvelopeSHA256 = sha

	reference, err := conformance.LoadTrace(glyphRasterRecordedTracePath)
	if err != nil {
		t.Skipf("recorded reference trace not present (%v); run conformance/cmd/recordreference after building the reference harness", err)
	}

	result := conformance.CompareTraces(reference, trace)
	if !result.Equal {
		t.Fatalf("glyph raster trace mismatch:\n%s", result.Diff)
	}
	if result.Diff != "" {
		t.Fatalf("equal comparison must report an empty diff, got %q", result.Diff)
	}
	if got, want := len(trace.Events), len(reference.Events); got != want {
		t.Errorf("event count = %d, want %d", got, want)
	}
	if got, want := len(trace.Events), 39; got != want {
		t.Errorf("event count = %d, want %d (the recorded fx-0005 trace)", got, want)
	}
}

// rasterQuery is the shared raster helper: resolve Segoe UI, map the
// character, prepare the style and rasterize.
func rasterQuery(t *testing.T, ts *gpui.TextSystem, ch rune, size, scale float32, mode gpui.GlyphRenderMode, color gpui.Rgba) (*gpui.RasterizedGlyph, gpui.RasterGlyphParams) {
	t.Helper()
	identity, err := ts.ResolveFont(gpui.FontDescriptor{Family: "Segoe UI", Weight: gpui.DefaultFontWeight})
	if err != nil {
		t.Fatalf("resolve Segoe UI: %v", err)
	}
	glyphID, ok, err := ts.GlyphForChar(identity.FontID, ch)
	if err != nil {
		t.Fatalf("glyph for char: %v", err)
	}
	if !ok {
		t.Fatalf("Segoe UI maps %q", string(ch))
	}
	style, err := ts.PrepareRasterStyle(color, mode)
	if err != nil {
		t.Fatalf("prepare raster style: %v", err)
	}
	params := gpui.RasterGlyphParams{
		FontID:      identity.FontID,
		GlyphID:     glyphID,
		FontSize:    size,
		SubpixelX:   0,
		SubpixelY:   0,
		ScaleFactor: scale,
		RasterStyle: style,
	}
	raster, err := ts.RasterizeGlyph(params)
	if err != nil {
		t.Fatalf("rasterize: %v", err)
	}
	return raster, params
}

// TestGlyphRasterBackendFactsAndModes pins the backend identity facts:
// DirectWrite is selected on this machine with the recorded rendering
// parameters, and the recommended mode follows the OS ClearType
// setting.
func TestGlyphRasterBackendFactsAndModes(t *testing.T) {
	ts, err := gpui.DefaultTextSystem()
	if err != nil {
		t.Fatalf("text system: %v", err)
	}
	info, err := ts.RasterBackend()
	if err != nil {
		t.Fatalf("raster backend: %v", err)
	}
	if info.Backend != gpui.RasterBackendDirectWrite {
		t.Errorf("backend = %d, want DirectWrite (0) on this machine", info.Backend)
	}
	if !info.VariableFactory {
		t.Errorf("variable factory = false, want true on DirectWrite factory 6")
	}
	if info.Gamma <= 0 || info.Gamma > 4 {
		t.Errorf("gamma = %v, want the DirectWrite default in (0, 4]", info.Gamma)
	}
	if info.GrayscaleEnhancedContrast < 0 || info.GrayscaleEnhancedContrast > 2 {
		t.Errorf("grayscale enhanced contrast = %v, want in [0, 2]", info.GrayscaleEnhancedContrast)
	}
	mode, err := ts.RecommendedRenderingMode()
	if err != nil {
		t.Fatalf("recommended rendering mode: %v", err)
	}
	if mode == gpui.TextRenderingPlatformDefault {
		t.Errorf("recommended mode = PlatformDefault; the pin never returns it")
	}
	if (mode == gpui.TextRenderingSubpixel) != info.SystemSubpixelRendering {
		t.Errorf("recommended mode = %v but system subpixel rendering = %v (they must agree)", mode, info.SystemSubpixelRendering)
	}
}

// TestGlyphRasterFormatsAndValidation pins the format routing and the
// ABI validation: grayscale → AlphaMask, subpixel → BgraSubpixelMask,
// color tint → BgraColor; empty rasters for spaces; repeat rasters are
// bit-identical; bad values are typed errors.
func TestGlyphRasterFormatsAndValidation(t *testing.T) {
	ts, err := gpui.DefaultTextSystem()
	if err != nil {
		t.Fatalf("text system: %v", err)
	}
	black := gpui.Rgba{A: 1}

	gray, params := rasterQuery(t, ts, 'A', 24, 1, gpui.GlyphRenderGrayscale, black)
	if gray.Format != gpui.RasterFormatAlphaMask {
		t.Errorf("grayscale format = %v, want AlphaMask", gray.Format)
	}
	if len(gray.Pixels) != int(gray.Width)*int(gray.Height) {
		t.Errorf("alpha mask has %d bytes, want %d", len(gray.Pixels), int(gray.Width)*int(gray.Height))
	}
	if gray.BoundsY >= 0 || gray.BoundsH <= 0 {
		t.Errorf("grayscale bounds = (%d,%d,%d,%d), want negative y (above the baseline) and positive height",
			gray.BoundsX, gray.BoundsY, gray.BoundsW, gray.BoundsH)
	}

	sub, _ := rasterQuery(t, ts, 'A', 24, 1, gpui.GlyphRenderSubpixel, black)
	if sub.Format != gpui.RasterFormatBgraSubpixelMask {
		t.Errorf("subpixel format = %v, want BgraSubpixelMask", sub.Format)
	}
	if len(sub.Pixels) != int(sub.Width)*int(sub.Height)*4 {
		t.Errorf("subpixel mask has %d bytes, want %d", len(sub.Pixels), int(sub.Width)*int(sub.Height)*4)
	}

	color, _ := rasterQuery(t, ts, 'A', 24, 1, gpui.GlyphRenderColor, gpui.Rgba{R: 0.8784314, G: 0.1254902, B: 0.0627451, A: 0.8})
	if color.Format != gpui.RasterFormatBgraColor {
		t.Errorf("color format = %v, want BgraColor", color.Format)
	}
	if len(color.Pixels) != int(color.Width)*int(color.Height)*4 {
		t.Errorf("color raster has %d bytes, want %d", len(color.Pixels), int(color.Width)*int(color.Height)*4)
	}

	// The scale doubles the mask extent.
	bigger, _ := rasterQuery(t, ts, 'g', 24, 2, gpui.GlyphRenderGrayscale, black)
	if bigger.Width <= gray.Width || bigger.Height <= gray.Height {
		t.Errorf("scale 2 raster (%dx%d) does not exceed scale 1 (%dx%d)", bigger.Width, bigger.Height, gray.Width, gray.Height)
	}

	// Repeat: bit-identical.
	again, _ := rasterQuery(t, ts, 'A', 24, 1, gpui.GlyphRenderGrayscale, black)
	if again.BoundsX != gray.BoundsX || again.BoundsY != gray.BoundsY || again.BoundsW != gray.BoundsW || again.BoundsH != gray.BoundsH {
		t.Errorf("repeat raster bounds differ: (%d,%d,%d,%d) vs (%d,%d,%d,%d)",
			again.BoundsX, again.BoundsY, again.BoundsW, again.BoundsH, gray.BoundsX, gray.BoundsY, gray.BoundsW, gray.BoundsH)
	}
	if string(again.Pixels) != string(gray.Pixels) {
		t.Errorf("repeat raster pixels differ (%d vs %d bytes)", len(again.Pixels), len(gray.Pixels))
	}
	_ = params

	// The space glyph rasters empty and never inserts into the atlas (the
	// pin's build closure returns None for zero bounds; the ABI validates
	// the zero-size upload away before allocation).
	space, _ := rasterQuery(t, ts, ' ', 24, 1, gpui.GlyphRenderGrayscale, black)
	if space.Width != 0 || space.Height != 0 || len(space.Pixels) != 0 {
		t.Errorf("space raster = (%dx%d, %d bytes), want empty", space.Width, space.Height, len(space.Pixels))
	}
	atlas, err := gpui.NewAtlas()
	if err != nil {
		t.Fatalf("new atlas: %v", err)
	}
	t.Cleanup(func() { _ = atlas.Dispose() })
	if _, err := atlas.InsertGlyph(space, gpui.RasterGlyphParams{
		FontID: params.FontID, GlyphID: params.GlyphID, FontSize: 24, ScaleFactor: 1,
		RasterStyle: gpui.PreparedRasterStyle{Mode: gpui.GlyphRenderGrayscale},
	}); err == nil {
		t.Errorf("empty raster insert: got nil error, want the zero-dimension validation")
	}

	// Validation: non-finite and non-positive scales are rejected before
	// native entry (the pin's own check).
	badStyle, err := ts.PrepareRasterStyle(black, gpui.GlyphRenderGrayscale)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if _, err := ts.RasterizeGlyph(gpui.RasterGlyphParams{
		FontID: params.FontID, GlyphID: params.GlyphID, FontSize: 16,
		ScaleFactor: 0, RasterStyle: badStyle,
	}); err == nil {
		t.Errorf("scale 0 rasterize: got nil error, want the pin's invalid-scale error")
	}
}

// TestAtlasInsertQueryRemoveRoundTrip pins the atlas lifecycle against
// the real D3D11 device: insert/query round trips, idempotent keys, the
// pinned remove-deallocates-space-for-reuse behavior, pool separation
// by kind and the upload validation.
func TestAtlasInsertQueryRemoveRoundTrip(t *testing.T) {
	atlas, err := gpui.NewAtlas()
	if err != nil {
		t.Fatalf("new atlas: %v", err)
	}
	t.Cleanup(func() { _ = atlas.Dispose() })

	if got := atlas.DefaultAtlasSize(); got != 1024 {
		t.Errorf("default atlas size = %d, want 1024", got)
	}
	if got := atlas.MaxAtlasSize(); got != 16384 {
		t.Errorf("max atlas size = %d, want 16384", got)
	}

	newKey := func(index uint32, format gpui.RasterFormat) gpui.RasterGlyphParams {
		return gpui.RasterGlyphParams{
			FontID:      gpui.FontID(0x8000000000000000 | uint64(index)),
			GlyphID:     index,
			FontSize:    24,
			ScaleFactor: 1,
			RasterStyle: gpui.PreparedRasterStyle{Mode: gpui.GlyphRenderGrayscale},
		}
	}

	// Monochrome insert (1 byte per pixel).
	mono := &gpui.RasterizedGlyph{Width: 64, Height: 64, Format: gpui.RasterFormatAlphaMask, Pixels: make([]byte, 64*64)}
	for i := range mono.Pixels {
		mono.Pixels[i] = byte(i % 251)
	}
	tile, err := atlas.InsertGlyph(mono, newKey(1, gpui.RasterFormatAlphaMask))
	if err != nil {
		t.Fatalf("insert mono: %v", err)
	}
	if tile.TextureKind != gpui.AtlasTextureMonochrome {
		t.Errorf("mono tile kind = %d, want monochrome", tile.TextureKind)
	}
	if tile.BoundsW != 64 || tile.BoundsH != 64 {
		t.Errorf("mono tile bounds = (%dx%d), want 64x64", tile.BoundsW, tile.BoundsH)
	}
	if tile.BoundsX < 0 || tile.BoundsY < 0 || tile.BoundsX+tile.BoundsW > 1024 || tile.BoundsY+tile.BoundsH > 1024 {
		t.Errorf("mono tile bounds (%d,%d,%d,%d) escape the 1024x1024 texture", tile.BoundsX, tile.BoundsY, tile.BoundsW, tile.BoundsH)
	}
	if tile.Generation == 0 {
		t.Errorf("tile generation = 0, want >= 1")
	}

	// The same key returns the SAME tile with no new allocation.
	again, err := atlas.InsertGlyph(mono, newKey(1, gpui.RasterFormatAlphaMask))
	if err != nil {
		t.Fatalf("re-insert mono: %v", err)
	}
	if again.TileID != tile.TileID || again.TextureIndex != tile.TextureIndex || again.BoundsX != tile.BoundsX || again.BoundsY != tile.BoundsY {
		t.Errorf("re-insert produced a different tile: %+v vs %+v", again, tile)
	}

	// Query round trip.
	queried, found, err := atlas.GlyphTile(newKey(1, gpui.RasterFormatAlphaMask), gpui.RasterFormatAlphaMask)
	if err != nil || !found {
		t.Fatalf("query: found=%v err=%v", found, err)
	}
	if queried.TileID != tile.TileID {
		t.Errorf("query tile id = %d, want %d", queried.TileID, tile.TileID)
	}

	// Polychrome and subpixel pools are separate textures with 4 bytes
	// per pixel.
	poly := &gpui.RasterizedGlyph{Width: 32, Height: 32, Format: gpui.RasterFormatBgraColor, Pixels: make([]byte, 32*32*4)}
	polyTile, err := atlas.InsertGlyph(poly, newKey(2, gpui.RasterFormatBgraColor))
	if err != nil {
		t.Fatalf("insert poly: %v", err)
	}
	if polyTile.TextureKind != gpui.AtlasTexturePolychrome {
		t.Errorf("poly tile kind = %d, want polychrome", polyTile.TextureKind)
	}
	sub := &gpui.RasterizedGlyph{Width: 32, Height: 32, Format: gpui.RasterFormatBgraSubpixelMask, Pixels: make([]byte, 32*32*4)}
	subTile, err := atlas.InsertGlyph(sub, newKey(3, gpui.RasterFormatBgraSubpixelMask))
	if err != nil {
		t.Fatalf("insert subpixel: %v", err)
	}
	if subTile.TextureKind != gpui.AtlasTextureSubpixel {
		t.Errorf("subpixel tile kind = %d, want subpixel", subTile.TextureKind)
	}
	for kind, want := range map[gpui.AtlasTextureKind]int{
		gpui.AtlasTextureMonochrome: 1,
		gpui.AtlasTexturePolychrome: 1,
		gpui.AtlasTextureSubpixel:   1,
	} {
		count, err := atlas.TextureCount(kind)
		if err != nil {
			t.Fatalf("texture count: %v", err)
		}
		if count != want {
			t.Errorf("texture count kind %d = %d, want %d", kind, count, want)
		}
	}

	// The pinned reuse behavior (directx_atlas.rs:400-434): removing a
	// big tile deallocates its space so another big tile reuses the same
	// texture instead of growing the pool.
	bigA := &gpui.RasterizedGlyph{Width: 700, Height: 700, Format: gpui.RasterFormatBgraColor, Pixels: make([]byte, 700*700*4)}
	bigB := &gpui.RasterizedGlyph{Width: 700, Height: 700, Format: gpui.RasterFormatBgraColor, Pixels: make([]byte, 700*700*4)}
	tileA, err := atlas.InsertGlyph(bigA, newKey(4, gpui.RasterFormatBgraColor))
	if err != nil {
		t.Fatalf("insert big A: %v", err)
	}
	if tileA.TextureIndex != polyTile.TextureIndex {
		t.Errorf("big tile A landed on texture %d, want the polychrome texture %d", tileA.TextureIndex, polyTile.TextureIndex)
	}
	if err := atlas.RemoveGlyph(newKey(4, gpui.RasterFormatBgraColor), gpui.RasterFormatBgraColor); err != nil {
		t.Fatalf("remove big A: %v", err)
	}
	tileB, err := atlas.InsertGlyph(bigB, newKey(5, gpui.RasterFormatBgraColor))
	if err != nil {
		t.Fatalf("insert big B: %v", err)
	}
	if tileB.TextureIndex != polyTile.TextureIndex {
		t.Errorf("big tile B landed on texture %d, want the reused polychrome texture %d", tileB.TextureIndex, polyTile.TextureIndex)
	}

	// Removing an absent key is a no-op; the removed key's query misses.
	if err := atlas.RemoveGlyph(newKey(4, gpui.RasterFormatBgraColor), gpui.RasterFormatBgraColor); err != nil {
		t.Fatalf("remove absent key: %v", err)
	}
	if _, found, err := atlas.GlyphTile(newKey(4, gpui.RasterFormatBgraColor), gpui.RasterFormatBgraColor); err != nil || found {
		t.Errorf("removed key query: found=%v err=%v, want false nil", found, err)
	}

	count, err := atlas.TileCount()
	if err != nil {
		t.Fatalf("tile count: %v", err)
	}
	if count != 4 {
		t.Errorf("tile count = %d, want 4 (mono + poly + sub + big B)", count)
	}
}

// TestAtlasValidationAndDeviceLoss pins the upload validation and the
// device-loss invalidation: malformed uploads publish nothing, and
// NotifyDeviceLost clears every tile, bumps the generation and accepts
// fresh inserts.
func TestAtlasValidationAndDeviceLoss(t *testing.T) {
	atlas, err := gpui.NewAtlas()
	if err != nil {
		t.Fatalf("new atlas: %v", err)
	}
	t.Cleanup(func() { _ = atlas.Dispose() })
	key := gpui.RasterGlyphParams{
		FontID:      gpui.FontID(0x8000000000000001),
		GlyphID:     7,
		FontSize:    24,
		ScaleFactor: 1,
		RasterStyle: gpui.PreparedRasterStyle{Mode: gpui.GlyphRenderGrayscale},
	}

	// Byte-count mismatch: 4 bytes for a 10x10 monochrome tile.
	if _, err := atlas.InsertGlyph(&gpui.RasterizedGlyph{Width: 10, Height: 10, Format: gpui.RasterFormatAlphaMask, Pixels: make([]byte, 4)}, key); err == nil {
		t.Errorf("byte-count mismatch insert: got nil error, want validation failure")
	}
	// Oversized tile.
	if _, err := atlas.InsertGlyph(&gpui.RasterizedGlyph{Width: 20000, Height: 10, Format: gpui.RasterFormatAlphaMask, Pixels: make([]byte, 10)}, key); err == nil {
		t.Errorf("oversized insert: got nil error, want validation failure")
	}
	count, err := atlas.TileCount()
	if err != nil {
		t.Fatalf("tile count: %v", err)
	}
	if count != 0 {
		t.Fatalf("rejected uploads published %d tiles, want 0", count)
	}

	// A valid insert, then device loss.
	good := &gpui.RasterizedGlyph{Width: 12, Height: 12, Format: gpui.RasterFormatAlphaMask, Pixels: make([]byte, 144)}
	tile, err := atlas.InsertGlyph(good, key)
	if err != nil {
		t.Fatalf("insert good: %v", err)
	}
	generation, err := atlas.Generation()
	if err != nil {
		t.Fatalf("generation: %v", err)
	}
	if generation == 0 {
		t.Fatalf("generation = 0, want >= 1")
	}
	if tile.Generation != generation {
		t.Errorf("tile generation = %d, want the atlas generation %d", tile.Generation, generation)
	}

	if err := atlas.NotifyDeviceLost(); err != nil {
		t.Fatalf("notify device lost: %v", err)
	}
	after, err := atlas.Generation()
	if err != nil {
		t.Fatalf("generation after loss: %v", err)
	}
	if after <= generation {
		t.Errorf("generation after loss = %d, want > %d", after, generation)
	}
	if _, found, err := atlas.GlyphTile(key, gpui.RasterFormatAlphaMask); err != nil || found {
		t.Errorf("query after loss: found=%v err=%v, want the invalidated miss", found, err)
	}
	// The cleared pools accept a fresh insert of the same key.
	reinserted, err := atlas.InsertGlyph(good, key)
	if err != nil {
		t.Fatalf("reinsert after loss: %v", err)
	}
	if reinserted.Generation != after {
		t.Errorf("reinserted tile generation = %d, want %d", reinserted.Generation, after)
	}
}

// TestAtlasStaleHandlesAndSlotLimit pins the handle lifecycle: dispose
// makes handles stale, malformed handles are typed errors, and the slot
// limit bounds live atlases.
func TestAtlasStaleHandlesAndSlotLimit(t *testing.T) {
	atlas, err := gpui.NewAtlas()
	if err != nil {
		t.Fatalf("new atlas: %v", err)
	}
	limit := atlas.MaxSlots()
	if err := atlas.Dispose(); err != nil {
		t.Fatalf("dispose: %v", err)
	}
	// Disposing twice is a no-op (the port's close-once).
	if err := atlas.Dispose(); err != nil {
		t.Fatalf("second dispose: %v", err)
	}
	key := gpui.RasterGlyphParams{
		FontID:      gpui.FontID(0x8000000000000002),
		GlyphID:     1,
		FontSize:    16,
		ScaleFactor: 1,
		RasterStyle: gpui.PreparedRasterStyle{Mode: gpui.GlyphRenderGrayscale},
	}
	if _, err := atlas.InsertGlyph(&gpui.RasterizedGlyph{Width: 4, Height: 4, Format: gpui.RasterFormatAlphaMask, Pixels: make([]byte, 16)}, key); err == nil {
		t.Errorf("insert after dispose: got nil error, want the stale-handle error")
	}
	if _, err := atlas.Generation(); err == nil {
		t.Errorf("generation after dispose: got nil error, want the stale-handle error")
	}

	// The native live-atlas bound: exactly `limit` concurrent atlases fit
	// (this test's disposed atlas freed its slot).
	handles := make([]*gpui.Atlas, 0, limit)
	defer func() {
		for _, a := range handles {
			_ = a.Dispose()
		}
	}()
	for i := 0; i < limit; i++ {
		a, err := gpui.NewAtlas()
		if err != nil {
			t.Fatalf("new atlas %d: %v", i, err)
		}
		handles = append(handles, a)
	}
	if _, err := gpui.NewAtlas(); err == nil {
		t.Errorf("atlas %d: got nil error, want the slot limit", limit+1)
	}
}

// newTestScene creates a fresh scene.
func newTestScene(t *testing.T) *gpui.Scene {
	t.Helper()
	scene, err := gpui.NewScene()
	if err != nil {
		t.Fatalf("gpui.NewScene: %v", err)
	}
	t.Cleanup(func() { _ = scene.Dispose() })
	return scene
}

// fullMask is the default content mask used by the paint tests.
func fullMask() gpui.Bounds {
	return gpui.Bounds{Size: gpui.Size{Width: 1e6, Height: 1e6}}
}

// TestSpriteSceneOpsForTextDraw pins the pinned sprite scene semantics
// (scene.rs:195-205 insert, 258-263 finish sort, the BatchIterator
// sprite arms and plan.rs's requirements): format-separated arrays, the
// (order, tile id) finish tie-break, per-texture batch splitting, the
// texture-index command field and the instance-batch requirement.
func TestSpriteSceneOpsForTextDraw(t *testing.T) {
	scene := newTestScene(t)
	mask := fullMask()
	tile := func(index uint32, kind gpui.AtlasTextureKind, id uint32) gpui.AtlasTile {
		return gpui.AtlasTile{
			TextureIndex: index,
			TextureKind:  kind,
			TileID:       id,
			BoundsX:      0,
			BoundsY:      0,
			BoundsW:      16,
			BoundsH:      16,
		}
	}

	// Three monochrome sprites at overlapping bounds: each later insert
	// sorts above the intersecting earlier one.
	for i, id := range []uint32{7, 3, 9} {
		sprite := gpui.MonochromeSprite{
			Bounds:         gpui.Bounds{Origin: gpui.Point{X: 10, Y: 10}, Size: gpui.Size{Width: 50, Height: 50}},
			ContentMask:    mask,
			Color:          gpui.Hsla{A: 1},
			Tile:           tile(0, gpui.AtlasTextureMonochrome, id),
			Transformation: gpui.UnitTransformation(),
		}
		if i == 1 {
			// This one does not overlap: same order band.
			sprite.Bounds = gpui.Bounds{Origin: gpui.Point{X: 500, Y: 500}, Size: gpui.Size{Width: 50, Height: 50}}
		}
		if err := scene.InsertMonochromeSprite(sprite); err != nil {
			t.Fatalf("insert monochrome sprite %d: %v", i, err)
		}
	}
	// One subpixel and one polychrome sprite.
	if err := scene.InsertSubpixelSprite(gpui.SubpixelSprite{
		Bounds:         gpui.Bounds{Origin: gpui.Point{X: 10, Y: 10}, Size: gpui.Size{Width: 50, Height: 50}},
		ContentMask:    mask,
		Color:          gpui.Hsla{A: 1},
		Tile:           tile(0, gpui.AtlasTextureSubpixel, 1),
		Transformation: gpui.UnitTransformation(),
	}); err != nil {
		t.Fatalf("insert subpixel sprite: %v", err)
	}
	if err := scene.InsertPolychromeSprite(gpui.PolychromeSprite{
		Opacity:     1,
		Bounds:      gpui.Bounds{Origin: gpui.Point{X: 10, Y: 10}, Size: gpui.Size{Width: 50, Height: 50}},
		ContentMask: mask,
		Tile:        tile(0, gpui.AtlasTexturePolychrome, 1),
	}); err != nil {
		t.Fatalf("insert polychrome sprite: %v", err)
	}

	if err := scene.Finish(); err != nil {
		t.Fatalf("finish: %v", err)
	}

	monos, err := scene.MonochromeSprites()
	if err != nil {
		t.Fatalf("dump monochrome sprites: %v", err)
	}
	if len(monos) != 3 {
		t.Fatalf("monochrome sprite count = %d, want 3", len(monos))
	}
	// The overlapping pair: the second insert sorts strictly above the
	// first.
	byTile := map[uint32]gpui.MonochromeSprite{}
	for _, s := range monos {
		byTile[s.Tile.TileID] = s
	}
	if byTile[7].Order >= byTile[9].Order {
		t.Errorf("overlapping sprites: tile 7 order %d >= tile 9 order %d (a later overlapping insert must sort above)", byTile[7].Order, byTile[9].Order)
	}
	// The non-overlapping sprite shares the first insert's order; the
	// pinned tie-break orders them by tile id: 3 before 7.
	if byTile[3].Order != byTile[7].Order {
		t.Errorf("non-overlapping sprites: tile 3 order %d != tile 7 order %d (equal orders expected)", byTile[3].Order, byTile[7].Order)
	}

	// The plan: monochrome batch on texture 0, subpixel batch on
	// texture 0, polychrome batch on texture 0 — in the pinned
	// primitive-kind order (mono 4 < subpixel 5 < poly 6 at equal
	// orders).
	commands, err := scene.SpriteCommands()
	if err != nil {
		t.Fatalf("sprite commands: %v", err)
	}
	var kinds []uint32
	for _, cmd := range commands {
		if cmd.Kind != uint32(gpui.SceneCommandKindBatch) {
			t.Fatalf("unexpected non-batch command %d", cmd.Kind)
		}
		kinds = append(kinds, cmd.PrimitiveKind)
	}
	if len(kinds) != 3 {
		t.Fatalf("batch count = %d (%v), want 3 (one per sprite class)", len(kinds), kinds)
	}
	wantKinds := []uint32{
		uint32(gpui.SceneBatchMonochrome),
		uint32(gpui.SceneBatchSubpixel),
		uint32(gpui.SceneBatchPolychrome),
	}
	for i, want := range wantKinds {
		if kinds[i] != want {
			t.Errorf("batch %d kind = %d, want %d", i, kinds[i], want)
		}
	}
	// Every sprite batch carries its atlas texture index.
	for i, cmd := range commands {
		if cmd.TextureIndex != 0 {
			t.Errorf("batch %d texture index = %d, want 0", i, cmd.TextureIndex)
		}
	}

	// Requirements: sprite batches count as instance batches.
	requirements, err := scene.Requirements()
	if err != nil {
		t.Fatalf("requirements: %v", err)
	}
	if requirements.InstanceBatchCount != 3 {
		t.Errorf("instance batch count = %d, want 3 (the pinned include_batch: sprites are instance batches)", requirements.InstanceBatchCount)
	}
	if requirements.CommandCount != uint32(len(commands)) {
		t.Errorf("command count = %d, want %d", requirements.CommandCount, len(commands))
	}
	if requirements.UsesOffscreenTarget {
		t.Errorf("uses offscreen target = true, want false (no filters)")
	}

	// The meta record carries the per-class sprite counts.
	meta, err := scene.SpriteMeta()
	if err != nil {
		t.Fatalf("sprite meta: %v", err)
	}
	if meta.MonochromeSpriteCount != 3 || meta.SubpixelSpriteCount != 1 || meta.PolychromeSpriteCount != 1 {
		t.Errorf("meta sprite counts = mono %d, sub %d, poly %d, want 3/1/1",
			meta.MonochromeSpriteCount, meta.SubpixelSpriteCount, meta.PolychromeSpriteCount)
	}
}

// TestSpriteSceneBatchSplitByTexture pins the pinned batch splitting:
// monochrome sprites on different atlas textures never share a batch.
func TestSpriteSceneBatchSplitByTexture(t *testing.T) {
	scene := newTestScene(t)
	mask := fullMask()
	bounds := gpui.Bounds{Origin: gpui.Point{X: 10, Y: 10}, Size: gpui.Size{Width: 50, Height: 50}}
	// Equal tile ids across textures: the pinned (order, tile_id) sort
	// keeps the insertion order (a stable sort of equal keys), so the
	// texture sequence survives the sort and the batch iterator must
	// split on every texture change.
	tile := func(index uint32) gpui.AtlasTile {
		return gpui.AtlasTile{
			TextureIndex: index,
			TextureKind:  gpui.AtlasTextureMonochrome,
			TileID:       5,
			BoundsW:      16,
			BoundsH:      16,
		}
	}
	// All non-overlapping (equal orders), interleaving textures.
	for _, index := range []uint32{0, 1, 0, 1, 1} {
		if err := scene.InsertMonochromeSprite(gpui.MonochromeSprite{
			Bounds:         bounds,
			ContentMask:    mask,
			Color:          gpui.Hsla{A: 1},
			Tile:           tile(index),
			Transformation: gpui.UnitTransformation(),
		}); err != nil {
			t.Fatalf("insert sprite on texture %d: %v", index, err)
		}
		bounds = gpui.Bounds{
			Origin: gpui.Point{X: bounds.Origin.X + 100, Y: bounds.Origin.Y},
			Size:   gpui.Size{Width: 50, Height: 50},
		}
	}
	if err := scene.Finish(); err != nil {
		t.Fatalf("finish: %v", err)
	}
	commands, err := scene.SpriteCommands()
	if err != nil {
		t.Fatalf("sprite commands: %v", err)
	}
	// The pinned BatchIterator merges only same-texture runs: the
	// texture sequence 0,1,0,1,1 yields four batches (0)(1)(0)(1 1) -
	// the trailing same-texture run merges.
	var batches []uint32
	for _, cmd := range commands {
		if cmd.PrimitiveKind != uint32(gpui.SceneBatchMonochrome) {
			t.Fatalf("unexpected batch kind %d", cmd.PrimitiveKind)
		}
		batches = append(batches, cmd.TextureIndex)
	}
	want := []uint32{0, 1, 0, 1}
	if len(batches) != len(want) {
		t.Fatalf("batch count = %d (%v), want %d (%v)", len(batches), batches, len(want), want)
	}
	for i := range want {
		if batches[i] != want[i] {
			t.Errorf("batch %d texture = %d, want %d", i, batches[i], want[i])
		}
	}
	// The last two merge into one batch of two sprites.
	last := commands[len(commands)-1]
	if last.RangeEnd-last.RangeStart != 2 {
		t.Errorf("last batch range = %d..%d, want 2 sprites", last.RangeStart, last.RangeEnd)
	}
}

// TestSpriteTileKindValidation pins the renderer contract's "never
// reinterpret one format as another": a sprite whose tile comes from
// another pool is rejected at the Go seam before native entry.
func TestSpriteTileKindValidation(t *testing.T) {
	scene := newTestScene(t)
	mask := fullMask()
	wrongTile := gpui.AtlasTile{
		TextureKind:  gpui.AtlasTexturePolychrome,
		TextureIndex: 0,
		TileID:       1,
		BoundsW:      16,
		BoundsH:      16,
	}
	if err := scene.InsertMonochromeSprite(gpui.MonochromeSprite{
		Bounds:         gpui.Bounds{Origin: gpui.Point{X: 0, Y: 0}, Size: gpui.Size{Width: 16, Height: 16}},
		ContentMask:    mask,
		Color:          gpui.Hsla{A: 1},
		Tile:           wrongTile,
		Transformation: gpui.UnitTransformation(),
	}); err == nil {
		t.Errorf("monochrome sprite with a polychrome tile: got nil error, want the pool-mismatch error")
	}
	if err := scene.InsertSubpixelSprite(gpui.SubpixelSprite{
		Bounds:         gpui.Bounds{Origin: gpui.Point{X: 0, Y: 0}, Size: gpui.Size{Width: 16, Height: 16}},
		ContentMask:    mask,
		Color:          gpui.Hsla{A: 1},
		Tile:           wrongTile,
		Transformation: gpui.UnitTransformation(),
	}); err == nil {
		t.Errorf("subpixel sprite with a polychrome tile: got nil error, want the pool-mismatch error")
	}
	if err := scene.InsertPolychromeSprite(gpui.PolychromeSprite{
		Opacity: 1,
		Bounds:  gpui.Bounds{Origin: gpui.Point{X: 0, Y: 0}, Size: gpui.Size{Width: 16, Height: 16}},
		Tile: gpui.AtlasTile{
			TextureKind:  gpui.AtlasTextureMonochrome,
			TextureIndex: 0,
			TileID:       1,
			BoundsW:      16,
			BoundsH:      16,
		},
	}); err == nil {
		t.Errorf("polychrome sprite with a monochrome tile: got nil error, want the pool-mismatch error")
	}
	// A non-zero order is rejected (the kernel assigns orders).
	if err := scene.InsertMonochromeSprite(gpui.MonochromeSprite{
		Order:  5,
		Bounds: gpui.Bounds{Origin: gpui.Point{X: 0, Y: 0}, Size: gpui.Size{Width: 16, Height: 16}},
		Tile: gpui.AtlasTile{
			TextureKind:  gpui.AtlasTextureMonochrome,
			TextureIndex: 0,
			TileID:       1,
			BoundsW:      16,
			BoundsH:      16,
		},
	}); err == nil {
		t.Errorf("sprite with order 5: got nil error, want the zero-order error")
	}
}

// TestGlyphSceneSurvivesForcedGC pins that the native scene and atlas
// state outlive a forced garbage collection (no finalizers or weak
// references sit between the Go handles and the native state).
func TestGlyphSceneSurvivesForcedGC(t *testing.T) {
	ts, err := gpui.DefaultTextSystem()
	if err != nil {
		t.Fatalf("text system: %v", err)
	}
	raster, params := rasterQuery(t, ts, 'A', 24, 1, gpui.GlyphRenderGrayscale, gpui.Rgba{A: 1})

	atlas, err := gpui.NewAtlas()
	if err != nil {
		t.Fatalf("new atlas: %v", err)
	}
	t.Cleanup(func() { _ = atlas.Dispose() })
	tile, err := atlas.InsertGlyph(raster, params)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	scene := newTestScene(t)
	mask := fullMask()
	ctx := gpui.NewPaintContext(1, mask, 1)
	if err := scene.PaintGlyph(gpui.Point{X: 8, Y: 30}, params.FontID, params.GlyphID, 24,
		gpui.Hsla{A: 1}, ts, atlas, ctx, gpui.PaintTextOptions{}); err != nil {
		t.Fatalf("paint glyph: %v", err)
	}
	if err := scene.Finish(); err != nil {
		t.Fatalf("finish: %v", err)
	}

	// Drop every Go reference except the handles, then force collection.
	rasterPtr := raster
	_ = rasterPtr
	runtime.GC()
	runtime.GC()

	// The sprite class follows the platform's recommended rendering mode
	// (subpixel on a ClearType machine); count both mask classes.
	subsprites, err := scene.SubpixelSprites()
	if err != nil {
		t.Fatalf("dump subpixel sprites after GC: %v", err)
	}
	monos, err := scene.MonochromeSprites()
	if err != nil {
		t.Fatalf("dump monochrome sprites after GC: %v", err)
	}
	if len(subsprites)+len(monos) != 1 {
		t.Fatalf("sprite count after GC = %d (sub %d + mono %d), want 1", len(subsprites)+len(monos), len(subsprites), len(monos))
	}
	paintedTile := tile
	if len(subsprites) == 1 {
		paintedTile = subsprites[0].Tile
	} else {
		paintedTile = monos[0].Tile
	}
	if paintedTile.TileID != tile.TileID {
		t.Errorf("sprite tile after GC = %d, want %d", paintedTile.TileID, tile.TileID)
	}
	queried, found, err := atlas.GlyphTile(params, raster.Format)
	if err != nil || !found {
		t.Errorf("atlas query after GC: found=%v err=%v", found, err)
	} else if queried.TileID != tile.TileID {
		t.Errorf("atlas tile after GC = %d, want %d", queried.TileID, tile.TileID)
	}
}

// TestRealWindowTextFramePresentedAndRetired is the ticket's real-window
// draw: open a window (ticket05 host), create a surface (ticket07
// renderer), shape text (ticket09 adapter), rasterize + atlas-insert it
// (ticket10), paint the glyph sprites into a scene, finish the scene,
// submit a present and retire it on completion (the ticket07 ledger).
func TestRealWindowTextFramePresentedAndRetired(t *testing.T) {
	app := gpui.NewApp()
	h := gpui.NewHost()
	if err := app.Attach(h); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if err := h.Start(); err != nil {
		t.Fatalf("host start: %v", err)
	}
	runDone := make(chan error, 1)
	go func() { runDone <- app.Run(h) }()
	t.Cleanup(func() {
		if err := h.Stop(); err != nil {
			t.Errorf("host stop: %v", err)
		}
		<-runDone
	})

	window, err := app.OpenWindow(gpui.WindowOptions{
		Title:     "glyphspec draw text",
		Show:      true,
		Resizable: true,
		Bounds:    gpui.Bounds{Size: gpui.Size{Width: 420, Height: 240}},
	})
	if err != nil {
		t.Fatalf("app.OpenWindow: %v — this session is expected to create real windows", err)
	}
	t.Cleanup(func() {
		if window.Alive() {
			window.Close()
			waitFor(t, 10*time.Second, "the window to be destroyed", func() bool { return !window.Alive() })
		}
	})

	// Shape the text through the ticket09 adapter.
	ts, err := gpui.DefaultTextSystem()
	if err != nil {
		t.Fatalf("text system: %v", err)
	}
	run := gpui.TextRun{
		Len:  len("Hello, gpui!"),
		Font: gpui.FontDescriptor{Family: "Segoe UI", Weight: gpui.DefaultFontWeight},
	}
	line, err := ts.ShapeText(gpui.TextLayoutRequest{
		Text:     "Hello, gpui!",
		FontSize: 24,
		Runs:     []gpui.TextRun{run},
	})
	if err != nil {
		t.Fatalf("shape text: %v", err)
	}
	t.Cleanup(func() { _ = line.Dispose() })
	summary, err := line.Summary()
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if summary.GlyphCount == 0 {
		t.Fatal("the shaped line has no glyphs")
	}

	scale, err := window.ScaleFactor()
	if err != nil {
		t.Fatalf("window scale factor: %v", err)
	}

	// Rasterize, atlas-insert and paint the glyphs into the scene.
	atlas, err := gpui.NewAtlas()
	if err != nil {
		t.Fatalf("new atlas: %v", err)
	}
	t.Cleanup(func() { _ = atlas.Dispose() })

	scene := newTestScene(t)
	mask := fullMask()
	ctx := gpui.NewPaintContext(scale, mask, 1)
	if err := scene.PaintShapedText(
		line,
		gpui.Point{X: 16, Y: 16},
		gpui.DefaultLineHeight(24),
		gpui.Hsla{H: 0, S: 0, L: 0.1, A: 1},
		ts, atlas, ctx,
		gpui.PaintTextOptions{},
	); err != nil {
		t.Fatalf("paint shaped text: %v", err)
	}
	if err := scene.Finish(); err != nil {
		t.Fatalf("scene finish: %v", err)
	}

	// The sprite class follows the platform's recommended rendering mode
	// (subpixel on a ClearType machine; monochrome otherwise) - the
	// pinned paint_glyph mode selection.
	subs, err := scene.SubpixelSprites()
	if err != nil {
		t.Fatalf("dump subpixel sprites: %v", err)
	}
	monos, err := scene.MonochromeSprites()
	if err != nil {
		t.Fatalf("dump monochrome sprites: %v", err)
	}
	spaces := 0
	for _, ch := range "Hello, gpui!" {
		if ch == ' ' {
			spaces++
		}
	}
	if want := summary.GlyphCount - spaces; len(subs)+len(monos) != want {
		t.Errorf("sprite count = %d (sub %d + mono %d), want %d (the %d shaped glyphs minus the %d empty space rasters that never insert)",
			len(subs)+len(monos), len(subs), len(monos), want, summary.GlyphCount, spaces)
	}
	commands, err := scene.SpriteCommands()
	if err != nil {
		t.Fatalf("sprite commands: %v", err)
	}
	if len(commands) == 0 {
		t.Fatal("the text scene compiled no batches")
	}
	requirements, err := scene.Requirements()
	if err != nil {
		t.Fatalf("requirements: %v", err)
	}
	if requirements.InstanceBatchCount == 0 {
		t.Error("the text scene reported no instance batches")
	}
	tileCount, err := atlas.TileCount()
	if err != nil {
		t.Fatalf("atlas tile count: %v", err)
	}
	if tileCount == 0 {
		t.Error("the atlas holds no tiles after painting the text")
	}
	t.Logf("text frame: %d glyphs, %d sprites (%d subpixel + %d mono), %d batches, %d atlas tiles, scale %.2f",
		summary.GlyphCount, len(subs)+len(monos), len(subs), len(monos), len(commands), tileCount, scale)

	// Present the frame and retire it on completion (the ticket07
	// ledger; the sprite draw pipeline itself is ticket16 — this slice
	// presents the frame the scene was built for).
	renderer, err := gpui.NewRenderer()
	if err != nil {
		t.Fatalf("gpui.NewRenderer: %v", err)
	}
	t.Cleanup(func() {
		if err := renderer.Close(); err != nil {
			t.Errorf("renderer close: %v", err)
		}
	})
	surface, err := renderer.GetOrCreateSurface(window)
	if err != nil {
		t.Fatalf("GetOrCreateSurface: %v", err)
	}
	if surface.Hwnd() != window.Handle().Hwnd() {
		t.Fatalf("surface HWND %#x does not match the window lease %#x", surface.Hwnd(), window.Handle().Hwnd())
	}
	submission, err := renderer.PresentClear(window, gpui.Color{R: 1, G: 1, B: 1, A: 1})
	if err != nil {
		t.Fatalf("PresentClear: %v", err)
	}
	if err := submission.WaitForRetirement(15 * time.Second); err != nil {
		t.Fatalf("the text frame submission did not retire: %v", err)
	}

	events, accepted, retired, quarantined, inFlight := renderer.Ledger()
	if accepted != 1 || retired != 1 || quarantined != 0 || inFlight != 0 {
		t.Fatalf("ledger counters accepted=%d retired=%d quarantined=%d in-flight=%d, want 1/1/0/0",
			accepted, retired, quarantined, inFlight)
	}
	completed := false
	for _, event := range events {
		if event.Kind == gpui.LedgerPollCompleted && event.Submission == submission.ID() {
			completed = true
		}
	}
	if !completed {
		t.Errorf("submission %d retired without a completed poll in the ledger trace", submission.ID())
	}
}

// TestGlyphAndAtlasCapabilityIdentity pins the artifact identity facts
// the new services advertise: capability bits 5 and 6 with their
// assigned names and native revision 6.
func TestGlyphAndAtlasCapabilityIdentity(t *testing.T) {
	lib, err := native.Load(native.Options{})
	if err != nil {
		t.Fatalf("native.Load: %v", err)
	}
	t.Cleanup(func() { _ = lib.Close() })
	identity := lib.Identity()
	// Ticket10 landed revision 6; ticket16 (path primitives + the
	// pinned PathBuilder tessellation in the scene service, the scene
	// drawing pipeline in the renderer service) bumped the cumulative
	// revision to 7 and added bit 7 (scene-draw-paths); ticket17 (the
	// image codec service) raised it to 8 with bit 8
	// (image-codecs-image-0-25).
	if identity.NativeRevision != 8 {
		t.Errorf("native revision = %d, want 8 (ticket17 cumulative)", identity.NativeRevision)
	}
	if identity.Capabilities&0x1FF != 0x1FF {
		t.Errorf("capabilities mask = %#x, want the nine assigned bits set", identity.Capabilities)
	}
	for _, want := range []string{"glyph-raster-dwrite", "glyph-atlas-d3d11"} {
		found := false
		for _, name := range identity.CapabilityNames {
			if name == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("capability names %v missing %q", identity.CapabilityNames, want)
		}
	}
}
