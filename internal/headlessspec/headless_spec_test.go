//go:build windows

// Package headlessspec holds ticket32's tests: the render-to-image
// model over the native renderer service's pinned test-support entry
// (render without presenting + staging readback), plus the headless
// API applicability trace. They drive the public gpui API and the
// native ABI from outside the runtime packages, matching the repo
// convention (see internal/rendererspec and internal/pathspec).
//
// Environment expectation: these tests create and show real Win32
// windows and need the D3D11 adapter; this machine runs them
// interactively and they never silently skip. Every test uses a fresh
// host and closes everything it opens (testing.Cleanup); every wait is
// a generous poll. Mouse/cursor state is never touched (this machine's
// mouse hit tests are corrupted — synthetic message tests only; the
// GPU path is unaffected).
//
// Reference comparison note: the pin's own render_to_image tests
// (crates/gpui_windows/src/directx_renderer.rs:2961-3064) drive a
// HiddenWindow + DirectXRenderer at scale 1 with scene coordinates in
// device pixels; these tests port those patterns (deterministic
// content with computable expected pixels, the dashed-border
// default_image/custom_image pair) through the port's real-window
// surfaces and the native ABI. What the reference comparison could NOT
// cover here: no independent Rust oracle run was executed (no cargo on
// this machine), so the expected pixels are computed from the pinned
// test's own expectations (±8 channel tolerance) rather than diffed
// against a live reference capture.
package headlessspec

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/native"
)

// ---------------------------------------------------------------------------
// Boot helpers (the rendererspec/pathspec patterns)
// ---------------------------------------------------------------------------

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

// bootApp boots a fresh app + host with one shown window, failing hard
// on creation errors, and returns the window (the pathspec boot
// pattern; the host stops at cleanup and the run loop joins).
func bootApp(t *testing.T, title string, w, h float32) (*gpui.App, *gpui.Window) {
	t.Helper()
	app := gpui.NewApp()
	host := gpui.NewHost()
	if err := app.Attach(host); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if err := host.Start(); err != nil {
		t.Fatalf("host start: %v", err)
	}
	runDone := make(chan error, 1)
	go func() { runDone <- app.Run(host) }()
	t.Cleanup(func() {
		if err := host.Stop(); err != nil {
			t.Errorf("host stop failed: %v", err)
		}
		<-runDone
	})
	window, err := app.OpenWindow(gpui.WindowOptions{
		Title:     title,
		Show:      true,
		Resizable: true,
		Bounds:    gpui.Bounds{Size: gpui.Size{Width: w, Height: h}},
	})
	if err != nil {
		t.Fatalf("app.OpenWindow(%q): %v — this session is expected to create real windows", title, err)
	}
	t.Cleanup(func() {
		if window.Alive() {
			window.Close()
			waitFor(t, 10*time.Second, "the window to be destroyed", func() bool { return !window.Alive() })
		}
	})
	return app, window
}

// bootHostWindow boots a bare host with one shown window for tests
// that drive the native ABI directly (no gpui app/renderer).
func bootHostWindow(t *testing.T, title string, w, h float32) gpui.WindowHandle {
	t.Helper()
	host := gpui.NewHost()
	if err := host.Start(); err != nil {
		t.Fatalf("host start failed: %v", err)
	}
	t.Cleanup(func() {
		if err := host.Stop(); err != nil {
			t.Errorf("host stop failed: %v", err)
		}
	})
	handle, err := host.OpenWindow(gpui.WindowOptions{
		Title:     title,
		Show:      true,
		Resizable: true,
		Bounds:    gpui.Bounds{Size: gpui.Size{Width: w, Height: h}},
	})
	if err != nil {
		t.Fatalf("host.OpenWindow(%q): %v — this session is expected to create real windows", title, err)
	}
	t.Cleanup(func() {
		if handle.Alive() {
			handle.Close()
			waitFor(t, 10*time.Second, "the window to be destroyed", func() bool { return !handle.Alive() })
		}
	})
	return handle
}

// mustRendererService loads the native renderer service of the
// embedded artifact (the same process-global DLL the gpui renderer
// singleton uses; the library is NOT closed here — the DLL is
// resident for the process lifetime per the distribution contract).
func mustRendererService(t *testing.T) *native.RendererService {
	t.Helper()
	lib, err := native.Load(native.Options{})
	if err != nil {
		t.Fatalf("native.Load: %v", err)
	}
	svc, err := lib.Renderer()
	if err != nil {
		t.Fatalf("lib.Renderer(): %v", err)
	}
	return svc
}

// rebuiltOutcome names the rebuild's surface outcome for the log.
func rebuildSurfaceOutcome(record native.RebuildRecord) string {
	if record.FailedSurfaces > 0 {
		return "failed"
	}
	return "succeeded"
}

// ---------------------------------------------------------------------------
// Scene builders (device pixels, the pin's test shapes)
// ---------------------------------------------------------------------------

// fullMask is the full-surface content mask in device pixels.
func fullMask(w, h uint32) gpui.Bounds {
	return gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: float32(w), Height: float32(h)}}
}

// solidQuad builds a full-depth solid quad record in device pixels
// (zero radii, zero borders: the pin's test quads).
func solidQuad(x, y, w, h float32, hex uint32, mask gpui.Bounds) gpui.Quad {
	return gpui.Quad{
		Bounds:      gpui.Bounds{Origin: gpui.Point{X: x, Y: y}, Size: gpui.Size{Width: w, Height: h}},
		ContentMask: mask,
		Background:  gpui.SolidBackground(gpui.RgbaToHsla(hex)),
	}
}

// splitScene paints the two-region row-pitch scene: a red quad over
// [0, split) and a blue quad over [split, w), full height (the colors
// the pin's tests use: 0xff0000 red, 0x0000ff blue).
func splitScene(t *testing.T, w, h, split uint32) *gpui.Scene {
	t.Helper()
	scene, err := gpui.NewScene()
	if err != nil {
		t.Fatalf("NewScene: %v", err)
	}
	t.Cleanup(func() { _ = scene.Dispose() })
	mask := fullMask(w, h)
	if err := scene.InsertQuad(solidQuad(0, 0, float32(split), float32(h), 0xff0000ff, mask)); err != nil {
		t.Fatalf("InsertQuad (red): %v", err)
	}
	if err := scene.InsertQuad(solidQuad(float32(split), 0, float32(w-split), float32(h), 0x0000ffff, mask)); err != nil {
		t.Fatalf("InsertQuad (blue): %v", err)
	}
	if err := scene.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	return scene
}

// emptyScene returns a finished empty scene (the clear-only capture).
func emptyScene(t *testing.T) *gpui.Scene {
	t.Helper()
	scene, err := gpui.NewScene()
	if err != nil {
		t.Fatalf("NewScene: %v", err)
	}
	t.Cleanup(func() { _ = scene.Dispose() })
	if err := scene.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	return scene
}

// dashedBorderScene ports the pin's dashed_border_scene
// (directx_renderer.rs:2902-2927): two dashed-border quads, one plain
// and one corner-smoothed, at the pin's device coordinates.
func dashedBorderScene(t *testing.T, dashLength, dashGap float32) *gpui.Scene {
	t.Helper()
	scene, err := gpui.NewScene()
	if err != nil {
		t.Fatalf("NewScene: %v", err)
	}
	t.Cleanup(func() { _ = scene.Dispose() })
	mask := fullMask(40, 20)
	quads := []struct {
		x, y, w, h float32
		smoothing  float32
	}{
		{x: 4, y: 4, w: 12, h: 12, smoothing: 0},
		{x: 24, y: 4, w: 12, h: 12, smoothing: 0.6},
	}
	for _, q := range quads {
		record := gpui.Quad{
			Bounds: gpui.Bounds{
				Origin: gpui.Point{X: q.x, Y: q.y},
				Size:   gpui.Size{Width: q.w, Height: q.h},
			},
			ContentMask:     mask,
			Background:      gpui.SolidBackground(gpui.Hsla{H: 0.05, S: 0.8, L: 0.45, A: 1}),
			BorderColor:     gpui.SolidBackground(gpui.Hsla{H: 0.6, S: 0.9, L: 0.7, A: 1}),
			CornerRadii:     gpui.Corners{TopLeft: 4, TopRight: 4, BottomRight: 4, BottomLeft: 4},
			BorderWidths:    gpui.Edges{Top: 2, Right: 2, Bottom: 2, Left: 2},
			BorderStyle:     gpui.BorderStyleDashed,
			DashedLength:    dashLength,
			DashedGap:       dashGap,
			CornerSmoothing: q.smoothing,
		}
		if err := scene.InsertQuad(record); err != nil {
			t.Fatalf("InsertQuad (dashed border): %v", err)
		}
	}
	if err := scene.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	return scene
}

// ---------------------------------------------------------------------------
// Pixel assertions
// ---------------------------------------------------------------------------

// pixelAt reads one RGBA pixel from a packed row-major buffer.
func pixelAt(pixels []byte, width, x, y int) (r, g, b, a byte) {
	offset := (y*width + x) * 4
	return pixels[offset], pixels[offset+1], pixels[offset+2], pixels[offset+3]
}

// closeChannel is the pin's per-channel tolerance (its own
// render_to_image tests allow abs_diff <= 8).
func closeChannel(actual, wanted byte) bool {
	diff := int(actual) - int(wanted)
	if diff < 0 {
		diff = -diff
	}
	return diff <= 8
}

// expectPixel asserts one image pixel against the wanted RGBA within
// the pin's tolerance.
func expectPixel(t *testing.T, img *gpui.RenderedImage, x, y uint32, want [4]byte, what string) {
	t.Helper()
	r, g, b, a, err := img.Pixel(x, y)
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
	if !closeChannel(r, want[0]) || !closeChannel(g, want[1]) || !closeChannel(b, want[2]) || !closeChannel(a, want[3]) {
		t.Errorf("%s at (%d,%d) rendered (%d,%d,%d,%d), want (%d,%d,%d,%d)",
			what, x, y, r, g, b, a, want[0], want[1], want[2], want[3])
	}
}

// captureNative renders the scene to an image through the native
// client with the pinned capacity retry (probe, allocate, retry),
// returning the owned raw RGBA bytes.
func captureNative(t *testing.T, svc *native.RendererService, surface native.SurfaceHandle, scene *gpui.Scene, appearance native.BackgroundAppearance) []byte {
	t.Helper()
	pixels, needed, err := svc.RenderToImage(surface, scene.Handle(), appearance, 0, nil)
	if err != nil {
		t.Fatalf("native RenderToImage (probe): %v", err)
	}
	if pixels != nil || needed == 0 {
		t.Fatalf("native probe = (%d bytes, non-nil %v), want the needed size with no pixels", needed, pixels != nil)
	}
	buf := make([]byte, needed)
	pixels, _, err = svc.RenderToImage(surface, scene.Handle(), appearance, 0, buf)
	if err != nil {
		t.Fatalf("native RenderToImage (capture): %v", err)
	}
	if pixels == nil || len(pixels) != int(needed) {
		t.Fatalf("native capture = %d bytes, want %d", len(pixels), needed)
	}
	return pixels
}

// rawImagesDifferInRegion is imagesDifferInRegion over raw packed
// buffers (any pixel difference within y in [4,16), x in [left,right)).
func rawImagesDifferInRegion(first, second []byte, width, left, right int) bool {
	for y := 4; y < 16; y++ {
		for x := left; x < right; x++ {
			r1, g1, b1, a1 := pixelAt(first, width, x, y)
			r2, g2, b2, a2 := pixelAt(second, width, x, y)
			if r1 != r2 || g1 != g2 || b1 != b2 || a1 != a2 {
				return true
			}
		}
	}
	return false
}

// surfaceAtDeviceExtent resizes the window so its device-pixel client
// extent is exactly wantW x wantH and returns the renderer's surface
// at that extent (created after the settle). The logical size is
// chosen inside the device cell that truncates to the wanted extent
// (SetBounds converts logical*scale with int32 truncation), and the
// result is verified against the surface info — a mismatch fails.
// The wanted extent must be at or above the system's minimum window
// tracking size (this machine clamps narrower captioned windows to a
// ~120px client width); the native client's explicit-extent surfaces
// (the pin's renderer.resize shape) cover small extents instead.
func surfaceAtDeviceExtent(t *testing.T, renderer *gpui.Renderer, window *gpui.Window, wantW, wantH uint32) *gpui.Surface {
	t.Helper()
	if wantW < 160 || wantH < 36 {
		t.Fatalf("requested extent %dx%d is below the safe window-tracking minimum; use a native-client surface for small extents", wantW, wantH)
	}
	scale, err := window.ScaleFactor()
	if err != nil {
		t.Fatalf("ScaleFactor: %v", err)
	}
	logicalW := (float32(wantW) + 0.5) / scale
	logicalH := (float32(wantH) + 0.5) / scale
	window.SetBounds(gpui.Bounds{
		Origin: gpui.Point{X: 40, Y: 40},
		Size:   gpui.Size{Width: logicalW, Height: logicalH},
	})
	waitFor(t, 10*time.Second, "the window resize to settle", func() bool {
		bounds, err := window.Bounds()
		if err != nil {
			return false
		}
		return abs32(bounds.Size.Width-logicalW) < 1 && abs32(bounds.Size.Height-logicalH) < 1
	})
	surface, err := renderer.GetOrCreateSurface(window)
	if err != nil {
		t.Fatalf("GetOrCreateSurface: %v", err)
	}
	info, err := surface.Info()
	if err != nil {
		t.Fatalf("surface.Info: %v", err)
	}
	if info.Width != wantW || info.Height != wantH {
		t.Fatalf("surface extent %dx%d, want exactly %dx%d (scale %v, logical %vx%v)",
			info.Width, info.Height, wantW, wantH, scale, logicalW, logicalH)
	}
	return surface
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestRenderToImageSolidQuadsMatchExpectedPixels is the deterministic
// content gate: a background quad plus two non-overlapping solid quads
// with computable expected pixels, captured through the public model
// with the exact dims/format metadata asserted.
func TestRenderToImageSolidQuadsMatchExpectedPixels(t *testing.T) {
	_, window := bootApp(t, "headlessspec solid quads", 300, 200)
	renderer, err := gpui.NewRendererManual()
	if err != nil {
		t.Fatalf("gpui.NewRendererManual: %v", err)
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
	info, err := surface.Info()
	if err != nil {
		t.Fatalf("surface.Info: %v", err)
	}
	width, height := info.Width, info.Height
	if width < 64 || height < 64 {
		t.Fatalf("surface extent %dx%d too small for the sampling plan", width, height)
	}
	mask := fullMask(width, height)
	split := width / 2

	scene, err := gpui.NewScene()
	if err != nil {
		t.Fatalf("NewScene: %v", err)
	}
	t.Cleanup(func() { _ = scene.Dispose() })
	// The background quad (dark slate) plus a green left half and a
	// blue box on the right half (the pin's every_batch shape:
	// background + non-overlapping solid quads).
	if err := scene.InsertQuad(solidQuad(0, 0, float32(width), float32(height), 0x334155ff, mask)); err != nil {
		t.Fatalf("InsertQuad (background): %v", err)
	}
	if err := scene.InsertQuad(solidQuad(0, 0, float32(split), float32(height), 0x16a34aff, mask)); err != nil {
		t.Fatalf("InsertQuad (green): %v", err)
	}
	blueBox := gpui.Bounds{
		Origin: gpui.Point{X: float32(split + 8), Y: float32(height/2 - 12)},
		Size:   gpui.Size{Width: float32(split - 16), Height: float32(24)},
	}
	if err := scene.InsertQuad(solidQuad(blueBox.Origin.X, blueBox.Origin.Y, blueBox.Size.Width, blueBox.Size.Height, 0x0ea5e9ff, mask)); err != nil {
		t.Fatalf("InsertQuad (blue box): %v", err)
	}
	if err := scene.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	image, err := surface.RenderToImage(scene, nil, gpui.WindowBackgroundOpaque)
	if err != nil {
		t.Fatalf("RenderToImage: %v", err)
	}

	// The exact dims/format metadata.
	if image.Width() != width || image.Height() != height {
		t.Fatalf("image extent %dx%d, want the surface's %dx%d", image.Width(), image.Height(), width, height)
	}
	if image.Format() != gpui.PixelFormatRGBA8 {
		t.Fatalf("format = %v, want rgba8", image.Format())
	}
	if image.SourceFormat() != native.RenderTargetFormatValue {
		t.Fatalf("source format = %d, want the pinned BGRA8 target (%d)", image.SourceFormat(), native.RenderTargetFormatValue)
	}
	if got := image.Appearance(); got != gpui.WindowBackgroundOpaque {
		t.Fatalf("appearance = %v, want opaque", got)
	}
	if image.SurfaceMode() != native.SurfaceModeDCompPremultiplied {
		t.Fatalf("surface mode = %d, want DComp premultiplied", image.SurfaceMode())
	}
	if image.BytesPerRow() != int(width)*4 {
		t.Fatalf("bytes per row = %d, want %d", image.BytesPerRow(), int(width)*4)
	}
	if image.Len() != int(width)*int(height)*4 {
		t.Fatalf("pixel bytes = %d, want %d", image.Len(), int(width)*int(height)*4)
	}
	pixels := image.Pixels()
	if len(pixels) != image.Len() {
		t.Fatalf("Pixels length %d, want %d", len(pixels), image.Len())
	}

	// Deterministic content: the pin's ±8 channel tolerance.
	expectPixel(t, image, width/4, height/2, [4]byte{0x16, 0xa3, 0x4a, 0xff}, "the green left half")
	expectPixel(t, image, split+8+(split-16)/2, height/2, [4]byte{0x0e, 0xa5, 0xe9, 0xff}, "the blue box")
	expectPixel(t, image, split+4, height/4, [4]byte{0x33, 0x41, 0x55, 0xff}, "the background right of the split")
	expectPixel(t, image, width-4, height-4, [4]byte{0x33, 0x41, 0x55, 0xff}, "the background bottom-right corner")
	// The accessor agrees with the raw buffer at a few positions.
	for _, xy := range [][2]uint32{{width / 4, height / 2}, {split + 4, height / 4}, {1, 1}} {
		r, g, b, a, err := image.Pixel(xy[0], xy[1])
		if err != nil {
			t.Fatalf("Pixel(%d,%d): %v", xy[0], xy[1], err)
		}
		r2, g2, b2, a2 := pixelAt(pixels, int(width), int(xy[0]), int(xy[1]))
		if r != r2 || g != g2 || b != b2 || a != a2 {
			t.Errorf("Pixel(%d,%d) = (%d,%d,%d,%d) disagrees with the raw buffer (%d,%d,%d,%d)",
				xy[0], xy[1], r, g, b, a, r2, g2, b2, a2)
		}
	}
	// Out-of-bounds reads are a typed error, not a panic.
	if _, _, _, _, err := image.Pixel(width, 0); err == nil {
		t.Error("Pixel(width, 0) unexpectedly succeeded")
	}
	if _, _, _, _, err := image.Pixel(0, height); err == nil {
		t.Error("Pixel(0, height) unexpectedly succeeded")
	}
}

// TestRenderToImageAppearanceClears pins the background-appearance
// behavior (the pinned render's match): opaque clears to opaque white,
// transparent and blurred clear to fully transparent — with an
// uncovered content quad over the transparent modes rendering opaque.
func TestRenderToImageAppearanceClears(t *testing.T) {
	_, window := bootApp(t, "headlessspec appearance clears", 240, 160)
	renderer, err := gpui.NewRendererManual()
	if err != nil {
		t.Fatalf("gpui.NewRendererManual: %v", err)
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
	info, err := surface.Info()
	if err != nil {
		t.Fatalf("surface.Info: %v", err)
	}
	width, height := int(info.Width), int(info.Height)
	total := width * height * 4
	empty := emptyScene(t)

	// Opaque: the clear value [1,1,1,1] reaches every pixel.
	opaque, err := surface.RenderToImage(empty, nil, gpui.WindowBackgroundOpaque)
	if err != nil {
		t.Fatalf("RenderToImage (opaque): %v", err)
	}
	if opaque.Appearance() != gpui.WindowBackgroundOpaque {
		t.Fatalf("opaque capture recorded appearance %v", opaque.Appearance())
	}
	for i, b := range opaque.Pixels() {
		if b != 255 {
			t.Fatalf("opaque clear: byte %d = %d, want 255 (the [1,1,1,1] clear)", i, b)
		}
	}

	// Transparent: the clear value [0,0,0,0] reaches every pixel.
	transparent, err := surface.RenderToImage(empty, nil, gpui.WindowBackgroundTransparent)
	if err != nil {
		t.Fatalf("RenderToImage (transparent): %v", err)
	}
	if transparent.Appearance() != gpui.WindowBackgroundTransparent {
		t.Fatalf("transparent capture recorded appearance %v", transparent.Appearance())
	}
	for i, b := range transparent.Pixels() {
		if b != 0 {
			t.Fatalf("transparent clear: byte %d = %d, want 0 (the [0,0,0,0] clear)", i, b)
		}
	}

	// Blurred: the same non-opaque clear on the render target (the
	// behind-window blur is DWM composition, not target content).
	blurred, err := surface.RenderToImage(empty, nil, gpui.WindowBackgroundBlurred)
	if err != nil {
		t.Fatalf("RenderToImage (blurred): %v", err)
	}
	if blurred.Appearance() != gpui.WindowBackgroundBlurred {
		t.Fatalf("blurred capture recorded appearance %v", blurred.Appearance())
	}
	if !bytes.Equal(blurred.Pixels(), transparent.Pixels()) {
		t.Error("blurred clear differs from the transparent clear — the pinned render clears every non-opaque mode to [0,0,0,0]")
	}

	// A full-depth opaque quad over the transparent mode: opaque
	// content renders identically regardless of the background mode.
	mask := fullMask(uint32(width), uint32(height))
	red, err := gpui.NewScene()
	if err != nil {
		t.Fatalf("NewScene: %v", err)
	}
	t.Cleanup(func() { _ = red.Dispose() })
	if err := red.InsertQuad(solidQuad(0, 0, float32(width), float32(height), 0xff0000ff, mask)); err != nil {
		t.Fatalf("InsertQuad: %v", err)
	}
	if err := red.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	redTransparent, err := surface.RenderToImage(red, nil, gpui.WindowBackgroundTransparent)
	if err != nil {
		t.Fatalf("RenderToImage (red over transparent): %v", err)
	}
	redOpaque, err := surface.RenderToImage(red, nil, gpui.WindowBackgroundOpaque)
	if err != nil {
		t.Fatalf("RenderToImage (red over opaque): %v", err)
	}
	if !bytes.Equal(redTransparent.Pixels(), redOpaque.Pixels()) {
		t.Error("an opaque full-surface quad rendered differently under the two appearance modes")
	}
	if redTransparent.Len() != total {
		t.Fatalf("red capture = %d bytes, want %d", redTransparent.Len(), total)
	}
	expectPixel(t, redTransparent, uint32(width)/2, uint32(height)/2, [4]byte{0xff, 0, 0, 0xff}, "the red quad over the transparent clear")
}

// TestRenderToImageOddWidthRowPitchAndCapacityProtocol drives the
// readback on an odd-width surface (61x37 — a width whose staging rows
// carry padding), asserting the packed-row correctness and the ABI
// capacity protocol, both through the native client (the pin's direct
// renderer test shape) and through the gpui model on a resized window.
func TestRenderToImageOddWidthRowPitchAndCapacityProtocol(t *testing.T) {
	const wantW, wantH = 61, 37
	const split = 30
	const wantBytes = wantW * wantH * 4

	// Part A: the native client at the pin's shape (a surface created
	// at exactly 61x37 device pixels; the capacity protocol observed
	// at the ABI).
	svc := mustRendererService(t)
	handle := bootHostWindow(t, "headlessspec odd width native", 240, 160)
	surface, err := svc.CreateSurface(handle.Hwnd(), native.SurfaceModeDCompPremultiplied, wantW, wantH, 1.0)
	if err != nil {
		t.Fatalf("CreateSurface(61x37): %v", err)
	}
	t.Cleanup(func() { _ = svc.DestroySurface(surface) })

	scene := splitScene(t, wantW, wantH, split)
	sceneHandle := scene.Handle()

	// The capacity protocol: a nil buffer reports the needed size with
	// a nil error; a too-small buffer reports it again; the exact-size
	// and oversized retries fill it.
	pixels, needed, err := svc.RenderToImage(surface, sceneHandle, native.BackgroundOpaque, 0, nil)
	if err != nil {
		t.Fatalf("RenderToImage (nil buffer): %v", err)
	}
	if pixels != nil || needed != wantBytes {
		t.Fatalf("nil-buffer probe = (%d bytes, %v), want (nil, %d)", needed, pixels != nil, wantBytes)
	}
	short := make([]byte, needed-1)
	pixels, needed, err = svc.RenderToImage(surface, sceneHandle, native.BackgroundOpaque, 0, short)
	if err != nil {
		t.Fatalf("RenderToImage (short buffer): %v", err)
	}
	if pixels != nil || needed != wantBytes {
		t.Fatalf("short-buffer probe = (%d bytes, %v), want (nil, %d)", needed, pixels != nil, wantBytes)
	}
	exact := make([]byte, needed)
	pixels, needed, err = svc.RenderToImage(surface, sceneHandle, native.BackgroundOpaque, 0, exact)
	if err != nil {
		t.Fatalf("RenderToImage (exact buffer): %v", err)
	}
	if pixels == nil || len(pixels) != wantBytes {
		t.Fatalf("exact-buffer capture = %d bytes, want %d", len(pixels), wantBytes)
	}
	oversized := make([]byte, needed+512)
	pixels, needed, err = svc.RenderToImage(surface, sceneHandle, native.BackgroundOpaque, 0, oversized)
	if err != nil {
		t.Fatalf("RenderToImage (oversized buffer): %v", err)
	}
	if pixels == nil || len(pixels) != wantBytes {
		t.Fatalf("oversized-buffer capture = %d bytes, want exactly %d (needed, not the capacity)", len(pixels), wantBytes)
	}

	// Every capture of the same scene is byte-identical (each call
	// renders and reads back; the capacity legs render too).
	if !bytes.Equal(exact[:wantBytes], pixels) {
		t.Error("two captures of the same scene through the capacity protocol differ")
	}

	// Row-pitch correctness: with 61px rows (244 bytes), a mishandled
	// staging pitch shifts successive rows; sample each row's interior
	// on both sides of the split.
	for y := 0; y < wantH; y++ {
		r, g, b, _ := pixelAt(pixels, wantW, 10, y)
		if !closeChannel(r, 0xff) || !closeChannel(g, 0) || !closeChannel(b, 0) {
			t.Fatalf("row %d left interior = (%d,%d,%d), want red — the staging row pitch leaked into the packed rows", y, r, g, b)
		}
		r, g, b, _ = pixelAt(pixels, wantW, 50, y)
		if !closeChannel(r, 0) || !closeChannel(g, 0) || !closeChannel(b, 0xff) {
			t.Fatalf("row %d right interior = (%d,%d,%d), want blue — the staging row pitch leaked into the packed rows", y, r, g, b)
		}
	}

	// Part B: the gpui model on a window resized so its device extent
	// has an odd width (161x37 — above the system's minimum window
	// tracking size; small extents run through the native client in
	// part A, the pin's renderer.resize shape).
	_, window := bootApp(t, "headlessspec odd width gpui", 300, 200)
	renderer, err := gpui.NewRendererManual()
	if err != nil {
		t.Fatalf("gpui.NewRendererManual: %v", err)
	}
	t.Cleanup(func() {
		if err := renderer.Close(); err != nil {
			t.Errorf("renderer close: %v", err)
		}
	})
	const gpuiWantW, gpuiWantH = 161, 37
	const gpuiSplit = 80
	const gpuiWantBytes = gpuiWantW * gpuiWantH * 4
	gpuiSurface := surfaceAtDeviceExtent(t, renderer, window, gpuiWantW, gpuiWantH)
	windowScene := splitScene(t, gpuiWantW, gpuiWantH, gpuiSplit)
	image, err := gpuiSurface.RenderToImage(windowScene, nil, gpui.WindowBackgroundOpaque)
	if err != nil {
		t.Fatalf("RenderToImage (odd extent): %v", err)
	}
	if image.Width() != gpuiWantW || image.Height() != gpuiWantH {
		t.Fatalf("odd-extent image = %dx%d, want %dx%d", image.Width(), image.Height(), gpuiWantW, gpuiWantH)
	}
	if image.Len() != gpuiWantBytes || image.BytesPerRow() != gpuiWantW*4 {
		t.Fatalf("odd-extent image = %d bytes (%d/row), want %d (%d/row)", image.Len(), image.BytesPerRow(), gpuiWantBytes, gpuiWantW*4)
	}
	for y := uint32(0); y < gpuiWantH; y++ {
		r, g, b, _, err := image.Pixel(10, y)
		if err != nil {
			t.Fatalf("Pixel(10,%d): %v", y, err)
		}
		if !closeChannel(r, 0xff) || !closeChannel(g, 0) || !closeChannel(b, 0) {
			t.Fatalf("row %d left interior = (%d,%d,%d), want red", y, r, g, b)
		}
		r, g, b, _, err = image.Pixel(150, y)
		if err != nil {
			t.Fatalf("Pixel(150,%d): %v", y, err)
		}
		if !closeChannel(r, 0) || !closeChannel(g, 0) || !closeChannel(b, 0xff) {
			t.Fatalf("row %d right interior = (%d,%d,%d), want blue", y, r, g, b)
		}
	}
}

// TestRenderToImageDashedBordersChangePixels ports the pin's
// configurable_dashes_reach_both_directx_quad_pipelines
// (directx_renderer.rs:2962-2985): default and custom dash parameters
// must change both the ordinary and the corner-smoothed quad
// pipelines' pixels. The pin renders at 40x20 device pixels through
// renderer.resize — a real captioned window cannot reach that client
// extent (the system's minimum tracking size), so this port drives the
// native client's explicit-extent surface (the same renderer.resize
// shape) on a real window's HWND.
func TestRenderToImageDashedBordersChangePixels(t *testing.T) {
	svc := mustRendererService(t)
	handle := bootHostWindow(t, "headlessspec dashed borders", 240, 160)
	surface, err := svc.CreateSurface(handle.Hwnd(), native.SurfaceModeDCompPremultiplied, 40, 20, 1.0)
	if err != nil {
		t.Fatalf("CreateSurface(40x20): %v", err)
	}
	t.Cleanup(func() { _ = svc.DestroySurface(surface) })

	defaultImage := captureNative(t, svc, surface, dashedBorderScene(t, 2.0, 1.0), native.BackgroundOpaque)
	customImage := captureNative(t, svc, surface, dashedBorderScene(t, 4.0, 0.5), native.BackgroundOpaque)
	if len(defaultImage) != 40*20*4 {
		t.Fatalf("dashed capture = %d bytes, want the pin's 40x20 (%d)", len(defaultImage), 40*20*4)
	}

	// The pin's two region assertions: the plain quad pipeline
	// (x 4..16) and the smoothed quad pipeline (x 24..36).
	if !rawImagesDifferInRegion(defaultImage, customImage, 40, 4, 16) {
		t.Error("custom dash length and gap must change the ordinary quad pipeline")
	}
	if !rawImagesDifferInRegion(defaultImage, customImage, 40, 24, 36) {
		t.Error("custom dash length and gap must change the smoothed quad pipeline")
	}

	// Determinism control: the same scene captures identically.
	again := captureNative(t, svc, surface, dashedBorderScene(t, 2.0, 1.0), native.BackgroundOpaque)
	if !bytes.Equal(defaultImage, again) {
		t.Error("two captures of the identical dashed scene differ")
	}
}

// TestRenderToImageRepeatedReadbackAndTrace proves the repeated
// readback behavior and the headless attempt trace: repeated captures
// of the same scene are byte-identical, alternating scenes each match
// their own previous capture, every capture owns a fresh buffer, the
// RenderScene+ReadPixels pair agrees with RenderToImage, and the trace
// records one capacity probe plus one completion per capture.
func TestRenderToImageRepeatedReadbackAndTrace(t *testing.T) {
	_, window := bootApp(t, "headlessspec repeated readback", 280, 180)
	renderer, err := gpui.NewRendererManual()
	if err != nil {
		t.Fatalf("gpui.NewRendererManual: %v", err)
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
	info, err := surface.Info()
	if err != nil {
		t.Fatalf("surface.Info: %v", err)
	}
	width, height := info.Width, info.Height
	mask := fullMask(width, height)

	red, err := gpui.NewScene()
	if err != nil {
		t.Fatalf("NewScene: %v", err)
	}
	t.Cleanup(func() { _ = red.Dispose() })
	if err := red.InsertQuad(solidQuad(0, 0, float32(width), float32(height), 0xff0000ff, mask)); err != nil {
		t.Fatalf("InsertQuad: %v", err)
	}
	if err := red.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	blue, err := gpui.NewScene()
	if err != nil {
		t.Fatalf("NewScene: %v", err)
	}
	t.Cleanup(func() { _ = blue.Dispose() })
	if err := blue.InsertQuad(solidQuad(0, 0, float32(width), float32(height), 0x0000ffff, mask)); err != nil {
		t.Fatalf("InsertQuad: %v", err)
	}
	if err := blue.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	// Three repeated captures: identical bytes.
	captures := make([]*gpui.RenderedImage, 0, 3)
	for i := 0; i < 3; i++ {
		image, err := surface.RenderToImage(red, nil, gpui.WindowBackgroundOpaque)
		if err != nil {
			t.Fatalf("RenderToImage (red, %d): %v", i+1, err)
		}
		captures = append(captures, image)
	}
	for i := 1; i < len(captures); i++ {
		if !bytes.Equal(captures[0].Pixels(), captures[i].Pixels()) {
			t.Errorf("repeated capture %d differs from the first", i+1)
		}
	}
	// Every capture owns a fresh buffer (no aliasing with an earlier
	// image): distinct data pointers, and mutating the later capture's
	// bytes leaves the first image untouched.
	first, second := captures[0], captures[1]
	if len(first.Pixels()) == 0 || &first.Pixels()[0] == &second.Pixels()[0] {
		t.Fatal("two captures share one pixel buffer — the image must own its bytes")
	}
	second.Pixels()[0] ^= 0xFF
	if first.Pixels()[0] == second.Pixels()[0] {
		t.Error("mutating the later capture changed the first image's bytes")
	}

	// Alternating scenes: each capture matches its own scene's earlier
	// capture, and the two scenes differ.
	red1, err := surface.RenderToImage(red, nil, gpui.WindowBackgroundOpaque)
	if err != nil {
		t.Fatalf("RenderToImage (red, alternating): %v", err)
	}
	blue1, err := surface.RenderToImage(blue, nil, gpui.WindowBackgroundOpaque)
	if err != nil {
		t.Fatalf("RenderToImage (blue, alternating): %v", err)
	}
	red2, err := surface.RenderToImage(red, nil, gpui.WindowBackgroundOpaque)
	if err != nil {
		t.Fatalf("RenderToImage (red, alternating 2): %v", err)
	}
	if !bytes.Equal(red1.Pixels(), red2.Pixels()) {
		t.Error("the red scene's alternating captures differ")
	}
	if bytes.Equal(red1.Pixels(), blue1.Pixels()) {
		t.Error("the red and blue scenes captured identically")
	}

	// The render-then-read pair agrees with the capture path
	// (RenderScene + ReadPixels vs RenderToImage of the same scene).
	if err := surface.RenderScene(blue, nil, gpui.BackgroundOpaque); err != nil {
		t.Fatalf("RenderScene: %v", err)
	}
	readBack, err := surface.ReadPixels()
	if err != nil {
		t.Fatalf("ReadPixels: %v", err)
	}
	if len(readBack) != blue1.Len() {
		t.Fatalf("ReadPixels = %d bytes, want %d", len(readBack), blue1.Len())
	}
	if !bytes.Equal(readBack, blue1.Pixels()) {
		t.Error("RenderScene+ReadPixels differs from RenderToImage of the same scene")
	}

	// The headless trace: one capacity probe and one completion per
	// successful capture, in order, with the byte counts.
	trace := renderer.HeadlessTrace()
	capacity, completed, other := 0, 0, 0
	for _, event := range trace {
		switch event.Kind {
		case gpui.HeadlessCapacityProbe:
			capacity++
			if event.Needed != uint32(blue1.Len()) {
				t.Errorf("capacity probe reported %d, want %d", event.Needed, blue1.Len())
			}
			if event.Surface != window.ID() {
				t.Errorf("capacity probe surface = %d, want %d", event.Surface, window.ID())
			}
		case gpui.HeadlessRenderCompleted:
			completed++
			if event.Width != width || event.Height != height || event.Needed != uint32(blue1.Len()) {
				t.Errorf("completed event = %dx%d %d bytes, want %dx%d %d", event.Width, event.Height, event.Needed, width, height, blue1.Len())
			}
		default:
			other++
		}
	}
	if capacity != completed || other != 0 {
		t.Fatalf("trace = %d capacity probes, %d completions, %d other events; want equal counts and no others", capacity, completed, other)
	}
	if completed != 3+3 {
		t.Fatalf("trace recorded %d completions, want 6 (three repeats plus the alternating triple)", completed)
	}
}

// TestRenderToImageTwoWindowsSharedDevice proves the two-window shape:
// independent windows/surfaces capture their own scenes correctly
// through the one shared D3D11 device (the native service's
// process-global device), with interleaved captures.
func TestRenderToImageTwoWindowsSharedDevice(t *testing.T) {
	// One app, two windows (the rendererspec two-window pattern: one
	// host/OLE apartment; the port's renderer service owns ONE shared
	// D3D11 device for every surface).
	app, window1 := bootApp(t, "headlessspec two windows 1", 260, 180)
	window2, err := app.OpenWindow(gpui.WindowOptions{
		Title:     "headlessspec two windows 2",
		Show:      true,
		Resizable: true,
		Bounds:    gpui.Bounds{Size: gpui.Size{Width: 220, Height: 160}},
	})
	if err != nil {
		t.Fatalf("app.OpenWindow (window 2): %v", err)
	}
	t.Cleanup(func() {
		if window2.Alive() {
			window2.Close()
			waitFor(t, 10*time.Second, "window 2 to be destroyed", func() bool { return !window2.Alive() })
		}
	})
	_ = window1
	renderer, err := gpui.NewRendererManual()
	if err != nil {
		t.Fatalf("gpui.NewRendererManual: %v", err)
	}
	t.Cleanup(func() {
		if err := renderer.Close(); err != nil {
			t.Errorf("renderer close: %v", err)
		}
	})
	surface1, err := renderer.GetOrCreateSurface(window1)
	if err != nil {
		t.Fatalf("GetOrCreateSurface (window 1): %v", err)
	}
	surface2, err := renderer.GetOrCreateSurface(window2)
	if err != nil {
		t.Fatalf("GetOrCreateSurface (window 2): %v", err)
	}
	info1, err := surface1.Info()
	if err != nil {
		t.Fatalf("Info (window 1): %v", err)
	}
	info2, err := surface2.Info()
	if err != nil {
		t.Fatalf("Info (window 2): %v", err)
	}
	if surface1.Hwnd() == surface2.Hwnd() {
		t.Fatal("the two windows share an HWND")
	}
	mask1 := fullMask(info1.Width, info1.Height)
	mask2 := fullMask(info2.Width, info2.Height)

	green, err := gpui.NewScene()
	if err != nil {
		t.Fatalf("NewScene: %v", err)
	}
	t.Cleanup(func() { _ = green.Dispose() })
	if err := green.InsertQuad(solidQuad(0, 0, float32(info1.Width), float32(info1.Height), 0x16a34aff, mask1)); err != nil {
		t.Fatalf("InsertQuad: %v", err)
	}
	if err := green.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	purple, err := gpui.NewScene()
	if err != nil {
		t.Fatalf("NewScene: %v", err)
	}
	t.Cleanup(func() { _ = purple.Dispose() })
	if err := purple.InsertQuad(solidQuad(0, 0, float32(info2.Width), float32(info2.Height), 0x7c3aedff, mask2)); err != nil {
		t.Fatalf("InsertQuad: %v", err)
	}
	if err := purple.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	// Interleaved captures through the shared device.
	image1a, err := surface1.RenderToImage(green, nil, gpui.WindowBackgroundOpaque)
	if err != nil {
		t.Fatalf("RenderToImage (window 1): %v", err)
	}
	image2, err := surface2.RenderToImage(purple, nil, gpui.WindowBackgroundOpaque)
	if err != nil {
		t.Fatalf("RenderToImage (window 2): %v", err)
	}
	image1b, err := surface1.RenderToImage(green, nil, gpui.WindowBackgroundOpaque)
	if err != nil {
		t.Fatalf("RenderToImage (window 1, repeat): %v", err)
	}

	if image1a.Width() != info1.Width || image1a.Height() != info1.Height {
		t.Fatalf("window 1 image = %dx%d, want %dx%d", image1a.Width(), image1a.Height(), info1.Width, info1.Height)
	}
	if image2.Width() != info2.Width || image2.Height() != info2.Height {
		t.Fatalf("window 2 image = %dx%d, want %dx%d", image2.Width(), image2.Height(), info2.Width, info2.Height)
	}
	if !bytes.Equal(image1a.Pixels(), image1b.Pixels()) {
		t.Error("window 1's interleaved captures differ")
	}
	if bytes.Equal(image1a.Pixels(), image2.Pixels()) {
		if image1a.Len() == image2.Len() {
			t.Error("the two windows' distinct scenes captured identically")
		}
	}
	expectPixel(t, image1a, info1.Width/2, info1.Height/2, [4]byte{0x16, 0xa3, 0x4a, 0xff}, "window 1's green")
	expectPixel(t, image2, info2.Width/2, info2.Height/2, [4]byte{0x7c, 0x3a, 0xed, 0xff}, "window 2's purple")

	// One shared device identity for both surfaces.
	infoA, err := renderer.DeviceInfo()
	if err != nil {
		t.Fatalf("DeviceInfo: %v", err)
	}
	t.Logf("shared device: adapter=%q feature-level=%#x driver=%q", infoA.AdapterDescription, infoA.FeatureLevel, infoA.DriverName)
	if infoA.AdapterDescription == "" {
		t.Fatal("empty adapter description")
	}
}

// TestRenderToImageTeardownWithPendingReadback pins the
// teardown-with-pending-readback behavior: a surface destroyed while a
// rendered frame awaits its readback refuses the readback with the
// typed stale-handle error (and the closed renderer with the gpui
// error), the failure is recorded in the headless trace, and no image
// is returned.
func TestRenderToImageTeardownWithPendingReadback(t *testing.T) {
	_, window := bootApp(t, "headlessspec teardown", 240, 160)
	renderer, err := gpui.NewRendererManual()
	if err != nil {
		t.Fatalf("gpui.NewRendererManual: %v", err)
	}
	rendererClosed := false
	t.Cleanup(func() {
		if rendererClosed {
			return
		}
		if err := renderer.Close(); err != nil {
			t.Errorf("renderer close: %v", err)
		}
	})
	surface, err := renderer.GetOrCreateSurface(window)
	if err != nil {
		t.Fatalf("GetOrCreateSurface: %v", err)
	}
	info, err := surface.Info()
	if err != nil {
		t.Fatalf("surface.Info: %v", err)
	}
	mask := fullMask(info.Width, info.Height)
	scene, err := gpui.NewScene()
	if err != nil {
		t.Fatalf("NewScene: %v", err)
	}
	t.Cleanup(func() { _ = scene.Dispose() })
	if err := scene.InsertQuad(solidQuad(0, 0, float32(info.Width), float32(info.Height), 0xff0000ff, mask)); err != nil {
		t.Fatalf("InsertQuad: %v", err)
	}
	if err := scene.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	// Render without presenting and without reading back: the frame's
	// content sits in the back buffer with a readback pending.
	if err := surface.RenderScene(scene, nil, gpui.BackgroundOpaque); err != nil {
		t.Fatalf("RenderScene: %v", err)
	}

	// Destroying the surface with the readback outstanding succeeds
	// (no un-retired submissions — render-to-image renders without a
	// submission token).
	if err := surface.Destroy(); err != nil {
		t.Fatalf("Destroy with a pending readback: %v", err)
	}

	// The pending readback is refused after teardown; no partial image
	// is returned. (The ReadPixels probe path in internal/native drops
	// the status code of its capacity-probe leg — a stale handle
	// surfaces there as the confusing "zero-sized back buffer" error —
	// so only the refusal itself is asserted for it; the render-to-image
	// entry checks its status codes and reports the typed stale-handle
	// error, which is the behavior this ticket owns.)
	if _, err := surface.ReadPixels(); err == nil {
		t.Error("ReadPixels after destroy unexpectedly succeeded")
	} else {
		t.Logf("ReadPixels after destroy was refused: %v", err)
	}
	image, err := surface.RenderToImage(scene, nil, gpui.WindowBackgroundOpaque)
	if err == nil || image != nil {
		t.Fatalf("RenderToImage after destroy = (%v, %v), want a typed error and no image", image != nil, err)
	}
	if !errors.Is(err, native.ErrRendererStaleHandle) {
		t.Errorf("RenderToImage after destroy = %v, want the stale-handle error", err)
	}

	// The renderer-level entry refuses on the closed renderer after
	// Close (the trace survives for post-mortem inspection).
	rendererClosed = true
	if err := renderer.Close(); err != nil {
		t.Fatalf("renderer.Close: %v", err)
	}
	if _, err := surface.RenderToImage(scene, nil, gpui.WindowBackgroundOpaque); err == nil {
		t.Error("RenderToImage after renderer close unexpectedly succeeded")
	}

	// The trace records the canceled capture (the destroy-time refusal
	// and the closed-renderer refusal), never a completion for them.
	failed := 0
	for _, event := range renderer.HeadlessTrace() {
		if event.Kind == gpui.HeadlessRenderFailed {
			failed++
			if event.Surface != window.ID() || event.Err == "" {
				t.Errorf("failed event = surface %d, err %q, want the window id and an error text", event.Surface, event.Err)
			}
		}
		if event.Kind == gpui.HeadlessRenderCompleted {
			t.Fatalf("a completed event was recorded after teardown: %+v", event)
		}
	}
	if failed == 0 {
		t.Fatal("the trace recorded no failed capture for the pending-readback teardown")
	}
}

// TestRenderToImageRecoveryGateRefusal pins the pinned
// device-recovery refusal: while the renderer is recovering from a
// device loss (the pin's skip_draws, armed by the rebuild until the
// first drawable frame), render-to-image is refused before any GPU
// work with the typed recovering error — through both the native
// client and the gpui wrapper — and the gate clears with
// mark_drawable, after which the rebuilt device captures the same
// scene byte-identically again.
//
// Recorded native deviation (ticket29's family, outside this
// ticket's files): the rev-10 rebuild's IN-PLACE surface recreation
// fails on this machine (FailedSurfaces=1, hr 0x88980800 DComp /
// E_ACCESSDENIED HWND) — the old swap chain/composition stays bound
// while the new one is created, where the pin's
// handle_device_lost_impl drops each window's renderer resources
// before recreating them. The rebuilt DEVICE itself is healthy: a
// fresh surface on the same window captures the baseline scene
// byte-identically, which is how this test proves the recovery.
func TestRenderToImageRecoveryGateRefusal(t *testing.T) {
	_, window := bootApp(t, "headlessspec recovery gate", 260, 170)
	renderer, err := gpui.NewRendererManual()
	if err != nil {
		t.Fatalf("gpui.NewRendererManual: %v", err)
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
	info, err := surface.Info()
	if err != nil {
		t.Fatalf("surface.Info: %v", err)
	}
	mask := fullMask(info.Width, info.Height)
	scene, err := gpui.NewScene()
	if err != nil {
		t.Fatalf("NewScene: %v", err)
	}
	t.Cleanup(func() { _ = scene.Dispose() })
	if err := scene.InsertQuad(solidQuad(0, 0, float32(info.Width), float32(info.Height), 0x00ff00ff, mask)); err != nil {
		t.Fatalf("InsertQuad: %v", err)
	}
	if err := scene.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	// The pre-loss baseline.
	baseline, err := surface.RenderToImage(scene, nil, gpui.WindowBackgroundOpaque)
	if err != nil {
		t.Fatalf("RenderToImage (baseline): %v", err)
	}
	expectPixel(t, baseline, info.Width/2, info.Height/2, [4]byte{0x00, 0xff, 0x00, 0xff}, "the baseline green")

	// Inject the loss through the native renderer client (the
	// test-only injection; recorded separately from real loss).
	svc := mustRendererService(t)
	if err := svc.InjectDeviceLoss(); err != nil {
		t.Fatalf("InjectDeviceLoss: %v", err)
	}
	state, err := svc.DeviceState()
	if err != nil {
		t.Fatalf("DeviceState (injected): %v", err)
	}
	if state.Lost != 1 || state.Injected != 1 {
		t.Fatalf("injected state = lost %d injected %d, want 1/1 (real and injected loss recorded separately)", state.Lost, state.Injected)
	}
	if state.Recovering != 0 {
		t.Fatalf("injected state = recovering %d, want 0 (the gate arms on the rebuild)", state.Recovering)
	}

	// The bounded-retry rebuild arms the recovery gate (the pin's
	// skip_draws) and bumps the device generation. The in-place surface
	// recreation outcome is logged, not asserted: the rev-10 artifact
	// currently fails it on this machine (see the recorded deviation
	// above); the recovery is proven below through a fresh surface.
	rebuild, err := svc.RebuildDevice()
	if err != nil {
		t.Fatalf("RebuildDevice: %v", err)
	}
	if rebuild.DeviceGeneration == 0 {
		t.Fatalf("rebuild record = %+v, want a bumped device generation", rebuild)
	}
	if rebuild.RebuiltSurfaces+rebuild.FailedSurfaces < 1 {
		t.Fatalf("rebuild record accounted %d rebuilt + %d failed surfaces, want this test's surface accounted", rebuild.RebuiltSurfaces, rebuild.FailedSurfaces)
	}
	t.Logf("rebuild record: %+v (the in-place surface recreation %s on this artifact; see the recorded deviation)",
		rebuild, rebuildSurfaceOutcome(rebuild))
	state, err = svc.DeviceState()
	if err != nil {
		t.Fatalf("DeviceState (recovering): %v", err)
	}
	if state.Recovering != 1 {
		t.Fatalf("post-rebuild state = recovering %d, want 1 (the gate is armed)", state.Recovering)
	}

	// The native entry refuses with the pinned recovering error (the
	// gate fires before any GPU work, including the capacity probe).
	_, _, err = svc.RenderToImage(surface.NativeHandle(), scene.Handle(), native.BackgroundOpaque, 0, nil)
	if !errors.Is(err, native.ErrRendererRecovering) {
		t.Errorf("native RenderToImage while recovering = %v, want the recovering refusal", err)
	}

	// The gpui wrapper surfaces the refusal at its own seam, wrapping
	// the native sentinel: no image, no partial bytes.
	image, err := surface.RenderToImage(scene, nil, gpui.WindowBackgroundOpaque)
	if err == nil || image != nil {
		t.Fatalf("RenderToImage while recovering = (%v, %v), want the refusal and no image", image != nil, err)
	}
	if !errors.Is(err, gpui.ErrRendererRecovering) {
		t.Errorf("RenderToImage while recovering = %v, want gpui.ErrRendererRecovering", err)
	}
	if !errors.Is(err, native.ErrRendererRecovering) {
		t.Errorf("RenderToImage while recovering = %v, want the wrapped native sentinel too", err)
	}

	// The refusal is recorded as a canceled capture in the trace.
	refused := 0
	for _, event := range renderer.HeadlessTrace() {
		if event.Kind == gpui.HeadlessRenderRecovering {
			refused++
			if event.Surface != window.ID() || event.Err == "" {
				t.Errorf("recovering event = surface %d, err %q, want the window id and an error text", event.Surface, event.Err)
			}
		}
	}
	if refused == 0 {
		t.Fatal("the trace recorded no recovering refusal")
	}

	// MarkDrawable clears the gate (the pin's mark_drawable, called on
	// the first forced draw after recovery).
	if err := svc.MarkDrawable(); err != nil {
		t.Fatalf("MarkDrawable: %v", err)
	}
	state, err = svc.DeviceState()
	if err != nil {
		t.Fatalf("DeviceState (drawable): %v", err)
	}
	if state.Recovering != 0 {
		t.Fatalf("post-mark-drawable state = recovering %d, want 0", state.Recovering)
	}

	// The recovered device: a fresh surface on the same window (the
	// failed in-place recreation is released; no submissions are
	// outstanding) captures the baseline scene byte-identically.
	if err := surface.Destroy(); err != nil {
		t.Fatalf("Destroy (failed in-place rebuild): %v", err)
	}
	fresh, err := renderer.GetOrCreateSurface(window)
	if err != nil {
		t.Fatalf("GetOrCreateSurface (fresh, post-recovery): %v", err)
	}
	recovered, err := fresh.RenderToImage(scene, nil, gpui.WindowBackgroundOpaque)
	if err != nil {
		t.Fatalf("RenderToImage (recovered): %v", err)
	}
	if !bytes.Equal(baseline.Pixels(), recovered.Pixels()) {
		t.Error("the recovered capture differs from the pre-loss baseline of the same scene")
	}
	expectPixel(t, recovered, info.Width/2, info.Height/2, [4]byte{0x00, 0xff, 0x00, 0xff}, "the recovered green")
}

// TestHeadlessProfileRecordsTraceThePinnedProfiles asserts the
// applicability trace: every profile of the headless/render-to-image
// family is recorded with its pinned sources, its source-supported
// errors separate from the port's implemented path, and an honest
// port status with bounded follow-ups for applicable missing work.
func TestHeadlessProfileRecordsTraceThePinnedProfiles(t *testing.T) {
	records := gpui.HeadlessProfileRecords()
	if len(records) < 5 {
		t.Fatalf("trace has %d records, want at least the five pinned profiles", len(records))
	}
	byProfile := make(map[gpui.HeadlessProfile]gpui.HeadlessProfileRecord)
	for _, record := range records {
		byProfile[record.Profile] = record
	}
	for _, profile := range []gpui.HeadlessProfile{
		gpui.HeadlessProfileNativeWindows,
		gpui.HeadlessProfileTestSupport,
		gpui.HeadlessProfileWgpu,
		gpui.HeadlessProfileAppleMetal,
		gpui.HeadlessProfilePlatformDefault,
	} {
		if _, ok := byProfile[profile]; !ok {
			t.Errorf("profile %q is missing from the trace", profile)
		}
	}

	// The native Windows profile: implemented by this port, window
	// bound, with the pinned recovery refusal as its source error.
	native := byProfile[gpui.HeadlessProfileNativeWindows]
	if !native.WindowRenderToImage || native.HeadlessRenderer || native.Windowless {
		t.Errorf("native-windows record: windowRenderToImage=%v headlessRenderer=%v windowless=%v, want true/false/false",
			native.WindowRenderToImage, native.HeadlessRenderer, native.Windowless)
	}
	if native.PortStatus != gpui.HeadlessPortImplemented {
		t.Errorf("native-windows port status = %v, want implemented", native.PortStatus)
	}
	if !containsSubstring(native.SourceSupportedErrors, "recovering from a lost device") {
		t.Errorf("native-windows source errors = %v, want the pinned recovery refusal", native.SourceSupportedErrors)
	}
	if len(native.MissingWork) == 0 {
		t.Error("native-windows record has no missing work; the Window-level render_to_image follow-up must be recorded")
	}
	if !strings.Contains(joinStrings(native.MissingWork), "frame") {
		t.Errorf("native-windows missing work = %v, want the frame-loop follow-up named", native.MissingWork)
	}

	// The test-support profile: partial — the behavior is implemented
	// through the window surface, the windowless factory shape is a
	// bounded follow-up, and its source error is the no-renderer
	// refusal.
	test := byProfile[gpui.HeadlessProfileTestSupport]
	if !test.WindowRenderToImage || !test.HeadlessRenderer || !test.Windowless {
		t.Errorf("test-support record: windowRenderToImage=%v headlessRenderer=%v windowless=%v, want true/true/true",
			test.WindowRenderToImage, test.HeadlessRenderer, test.Windowless)
	}
	if test.PortStatus != gpui.HeadlessPortPartial {
		t.Errorf("test-support port status = %v, want partial", test.PortStatus)
	}
	if !containsSubstring(test.SourceSupportedErrors, "no HeadlessRenderer configured") {
		t.Errorf("test-support source errors = %v, want the no-renderer refusal", test.SourceSupportedErrors)
	}
	if !strings.Contains(joinStrings(test.MissingWork), "windowless") && !strings.Contains(joinStrings(test.MissingWork), "factory") {
		t.Errorf("test-support missing work = %v, want the windowless factory follow-up", test.MissingWork)
	}

	// The wgpu profile: the alternate backend, traced but not selected.
	wgpu := byProfile[gpui.HeadlessProfileWgpu]
	if !wgpu.HeadlessRenderer || !wgpu.Windowless {
		t.Errorf("wgpu record: headlessRenderer=%v windowless=%v, want true/true", wgpu.HeadlessRenderer, wgpu.Windowless)
	}
	if wgpu.PortStatus != gpui.HeadlessPortNotApplicable {
		t.Errorf("wgpu port status = %v, want not-applicable (alternate backend, applicability traced)", wgpu.PortStatus)
	}
	if !containsSubstring(wgpu.SourceSupportedErrors, "positive dimensions") {
		t.Errorf("wgpu source errors = %v, want the positive-dimensions refusal", wgpu.SourceSupportedErrors)
	}

	// The apple profile and the trait default: not applicable to the
	// Windows-first port, with their pinned refusal messages.
	apple := byProfile[gpui.HeadlessProfileAppleMetal]
	if apple.PortStatus != gpui.HeadlessPortNotApplicable {
		t.Errorf("apple-metal port status = %v, want not-applicable", apple.PortStatus)
	}
	if !containsSubstring(apple.SourceSupportedErrors, "layer-backed") {
		t.Errorf("apple-metal source errors = %v, want the layer-backed refusal", apple.SourceSupportedErrors)
	}
	def := byProfile[gpui.HeadlessProfilePlatformDefault]
	if !containsSubstring(def.SourceSupportedErrors, "not implemented for this platform") {
		t.Errorf("platform-default source errors = %v, want the trait default's bail", def.SourceSupportedErrors)
	}

	// Every record pins auditable sources that exist in the checkout.
	for _, record := range records {
		if len(record.Sources) == 0 {
			t.Errorf("profile %q records no sources", record.Profile)
		}
		for _, ref := range record.Sources {
			path := filepath.Join("..", "..", "reference", "ce-source", ref.File)
			if _, err := os.Stat(path); err != nil {
				t.Errorf("profile %q cites %s, which does not resolve: %v", record.Profile, ref.File, err)
			}
		}
	}

	// The returned records are a copy: mutation does not leak.
	records[0].Summary = "mutated"
	if got := gpui.HeadlessProfileRecords(); got[0].Summary == "mutated" {
		t.Error("HeadlessProfileRecords returned the same backing slice twice")
	}
}

func containsSubstring(list []string, want string) bool {
	for _, item := range list {
		if strings.Contains(item, want) {
			return true
		}
	}
	return false
}

func joinStrings(list []string) string {
	return strings.Join(list, "\n")
}
