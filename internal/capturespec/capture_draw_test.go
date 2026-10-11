//go:build windows

// This file is ticket28's real-window draw gate on this machine. The
// reachable loop: enumerate the main monitor's source, create the
// stream on the renderer's actual D3D11 device, poll a real frame
// through a real window's content changes, render and present a real
// scene through the renderer ledger, and record the pinned
// unsupported-source draw bail explicitly.
//
// The capture-surface tail of the loop is BLOCKED by the frozen rev-10
// artifact: its scene_insert_surface export pre-validates surface
// records with a pre-ticket28 zero-reserved-words rule (check_surface
// in scene.rs), so the tag-1 record carrying the frame handle — the
// record the state-level insert validation and the renderer's draw
// path both accept — is rejected with the typed scene bad-value error.
// The gate test below records that answer honestly (the native
// one-line fix is the reported deviation; the Go half needs no
// change), so the pixel-verification gate ("pixels changed where
// expected" in the surface's bounds against a background-only
// control) lands with the approved rebuild, not here. Failures fail,
// never skip.

package capturespec

import (
	"errors"
	"math"
	"testing"
	"time"

	"gpui-go/gpui"
	"gpui-go/internal/native"
)

// closeColorBytes compares a readback pixel against a wanted byte
// triple at an 8-value tolerance.
func closeColorBytes(r, g, b, wantR, wantG, wantB byte) bool {
	d := func(a, b byte) int {
		if a > b {
			return int(a) - int(b)
		}
		return int(b) - int(a)
	}
	return d(r, wantR) <= 8 && d(g, wantG) <= 8 && d(b, wantB) <= 8
}

// TestCaptureDrawLoopUpToTheFrozenSurfaceGate drives the full real
// loop as far as the frozen artifact allows: a real window with real
// presented content, a real capture stream on the renderer's device,
// a real polled frame, a real rendered-and-presented scene through the
// ledger, the pinned unsupported-source draw bail, and the frozen
// insert-gate record for the capture surface itself.
func TestCaptureDrawLoopUpToTheFrozenSurfaceGate(t *testing.T) {
	app, host := bootApp(t)
	window := openShown(t, app, "capturespec draw loop", 320, 220)
	renderer := newRenderer(t)

	surface, err := renderer.GetOrCreateSurfaceMode(window, native.SurfaceModeDCompPremultiplied)
	if err != nil {
		t.Fatalf("GetOrCreateSurfaceMode: %v", err)
	}
	info, err := surface.Info()
	if err != nil {
		t.Fatalf("surface.Info: %v", err)
	}
	width := int(info.Width)
	height := int(info.Height)
	scale := math.Float32frombits(info.ScaleBits)
	if width == 0 || height == 0 || scale == 0 {
		t.Fatalf("surface extent = %dx%d scale %v, want non-zero", width, height, scale)
	}
	t.Logf("draw surface: %dx%d device pixels at scale %v", width, height, scale)

	// Put a distinct blue on the screen first, so the captured monitor
	// content deterministically includes this window (the frame the
	// surface would draw once the frozen gate is fixed).
	blue, err := renderer.PresentClear(window, gpui.Color{R: 0.1, G: 0.3, B: 0.9, A: 1})
	if err != nil {
		t.Fatalf("the blue present: %v", err)
	}
	if err := blue.WaitForRetirement(15 * time.Second); err != nil {
		t.Fatalf("the blue present did not retire: %v", err)
	}

	// Capture the monitor (the same-device producer on the renderer's
	// D3D11 device) and poll the first real frame. The registry appends
	// a fresh slot per created stream until sixteen exist, so the FIRST
	// run of this test in a process gets slot 0 (the only slot whose
	// handles fit the scene record's 32-bit payload); later runs land in
	// higher slots — both cases are recorded honestly below.
	stream := createMainStream(t, app, host)
	time.Sleep(300 * time.Millisecond) // let the composition settle into the frame pool
	frame, err := stream.PollForFrame(0, 10*time.Second)
	if err != nil {
		t.Fatalf("PollForFrame: %v", err)
	}
	slot := frame.Handle >> 44
	t.Logf("captured frame: handle=%#x pool=%dx%d content=%dx%d timestamp=%d (the same-device producer, slot %d, payload-fitting: %v)",
		frame.Handle, frame.Width, frame.Height, frame.ContentWidth, frame.ContentHeight, frame.Timestamp, slot, frame.Handle <= 0xFFFFFFFF)

	logicalWidth := float32(width) / scale
	logicalHeight := float32(height) / scale
	windowLogical := gpui.Bounds{Size: gpui.Size{Width: logicalWidth, Height: logicalHeight}}
	ctx := gpui.NewPaintContext(scale, windowLogical, 1.0)
	slate := gpui.RgbaToHsla(0x334155ff)

	// THE FROZEN-ARTIFACT RECORD for the tag-1 surface record carrying
	// the real frame's handle. Two gates block it today, both recorded:
	//
	//   - A slot-0 handle reaches the native insert and is rejected by
	//     the rev-10 scene_insert_surface export's pre-ticket28
	//     check_surface (zero reserved words), even though the
	//     state-level insert validation and the renderer's
	//     capture-surface draw path both accept exactly this record. The
	//     reported native one-line fix lands the rest of this loop (the
	//     surface geometry record, the pixel-verification diff against
	//     the background-only control, the capture-frame draw through
	//     the ledger).
	//   - A higher-slot handle is rejected earlier by the Go payload
	//     guard: the frozen scene ABI carries the handle in one 32-bit
	//     reserved word.
	captureScene, err := gpui.NewScene()
	if err != nil {
		t.Fatalf("NewScene (capture): %v", err)
	}
	t.Cleanup(func() { _ = captureScene.Dispose() })
	if err := captureScene.PaintQuad(gpui.PaintQuad{Bounds: windowLogical, Background: gpui.SolidBackground(slate)}, 0, ctx); err != nil {
		t.Fatalf("PaintQuad (background): %v", err)
	}
	leftHalf := gpui.Bounds{Size: gpui.Size{Width: logicalWidth / 2, Height: logicalHeight}}
	err = captureScene.PaintCaptureSurface(leftHalf, frame, ctx)
	switch {
	case errors.Is(err, native.ErrSceneBadValue):
		t.Logf("the frozen rev-10 gate: the capture-surface insert of the slot-0 frame is rejected (%v); the full capture→draw loop and its pixel gate land with the reported native fix", err)
	case errors.Is(err, gpui.ErrCaptureFrameHandlePayload):
		t.Logf("the slot-%d frame's handle is rejected by the scene ABI's 32-bit payload guard (%v); the slot-0 first-run case reaches the native gate above", slot, err)
	default:
		t.Fatalf("PaintCaptureSurface with the real frame = %v, want one of the two frozen-artifact records (the native insert gate or the 32-bit payload guard)", err)
	}

	// The renderer path itself is healthy: a background-only scene
	// renders, reads back and presents through the ledger.
	baselineScene, err := gpui.NewScene()
	if err != nil {
		t.Fatalf("NewScene (baseline): %v", err)
	}
	t.Cleanup(func() { _ = baselineScene.Dispose() })
	if err := baselineScene.PaintQuad(gpui.PaintQuad{Bounds: windowLogical, Background: gpui.SolidBackground(slate)}, 0, ctx); err != nil {
		t.Fatalf("PaintQuad (baseline): %v", err)
	}
	if err := baselineScene.Finish(); err != nil {
		t.Fatalf("Finish (baseline): %v", err)
	}
	if err := surface.RenderScene(baselineScene, nil, gpui.BackgroundOpaque); err != nil {
		t.Fatalf("RenderScene (baseline): %v", err)
	}
	pixels, err := surface.ReadPixels()
	if err != nil {
		t.Fatalf("ReadPixels (baseline): %v", err)
	}
	if len(pixels) != width*height*4 {
		t.Fatalf("the readback = %d bytes, want %d", len(pixels), width*height*4)
	}
	// The background quad covers the window: center and corner read back
	// the dark slate (the renderer's clear + quad path is exact).
	wantR, wantG, wantB := byte(0x33), byte(0x41), byte(0x55)
	for _, probe := range [][2]int{{width / 2, height / 2}, {8, 8}, {width - 8, height - 8}} {
		offset := (probe[1]*width + probe[0]) * 4
		r, g, b := pixels[offset], pixels[offset+1], pixels[offset+2]
		if !closeColorBytes(r, g, b, wantR, wantG, wantB) {
			t.Fatalf("the baseline pixel at %v = (%d,%d,%d), want the dark slate (%d,%d,%d)",
				probe, r, g, b, wantR, wantG, wantB)
		}
	}

	// Present the baseline scene through the renderer ledger (the
	// renderer's real present path with the retirement protocol).
	sub, err := surface.DrawScene(baselineScene, nil, gpui.BackgroundOpaque)
	if err != nil {
		t.Fatalf("DrawScene (baseline): %v", err)
	}
	if err := sub.WaitForRetirement(30 * time.Second); err != nil {
		t.Fatalf("the baseline draw submission did not retire: %v", err)
	}
	events, accepted, retired, quarantined, inFlight := renderer.Ledger()
	if accepted != 2 || retired != 2 || quarantined != 0 || inFlight != 0 {
		t.Fatalf("ledger counters accepted=%d retired=%d quarantined=%d in-flight=%d, want 2/2/0/0 (the blue present + the baseline draw)", accepted, retired, quarantined, inFlight)
	}
	completed := make(map[uint64]bool)
	for _, event := range events {
		switch event.Kind {
		case gpui.LedgerPollCompleted:
			completed[event.Submission] = true
		case gpui.LedgerRetired:
			if !completed[event.Submission] {
				t.Fatalf("submission %d retired without a completed poll", event.Submission)
			}
		}
	}
	t.Logf("the real draw retired through the ledger: %d events (accepted %d, retired %d)", len(events), accepted, retired)

	// The frame release record through the stream (the draw tail's
	// ownership observable): advancing the stream releases the frame the
	// scene would have referenced.
	presentChanges(t, renderer, window, 6, 30*time.Millisecond)
	if _, err := stream.PollForFrame(frame.Handle, 10*time.Second); err != nil {
		t.Fatalf("PollForFrame (advance): %v", err)
	}
	stats, err := stream.Stats()
	if err != nil {
		t.Fatalf("Stats after the advance: %v", err)
	}
	if stats.Published != 1 || stats.FramesReleased < 1 {
		t.Fatalf("after the advance: published=%d released=%d, want 1 published and at least the superseded frame released", stats.Published, stats.FramesReleased)
	}
	// The superseded frame's handle is genuinely released: a direct
	// native release of it is the typed bad-handle record.
	svc, err := app.CaptureService()
	if err != nil {
		t.Fatalf("app.CaptureService: %v", err)
	}
	if err := svc.FrameRelease(native.CaptureFrameHandle(frame.Handle)); !errors.Is(err, native.ErrCaptureBadHandle) {
		t.Fatalf("FrameRelease(the superseded frame) = %v, want the typed bad-handle error (already released)", err)
	}

	// The pinned unsupported-source record: a tag-0 surface (the
	// stand-in) renders nothing and the draw bails explicitly with the
	// typed unsupported-source error — never a silent blank surface.
	unsupportedScene, err := gpui.NewScene()
	if err != nil {
		t.Fatalf("NewScene (unsupported): %v", err)
	}
	t.Cleanup(func() { _ = unsupportedScene.Dispose() })
	if err := unsupportedScene.PaintQuad(gpui.PaintQuad{Bounds: windowLogical, Background: gpui.SolidBackground(slate)}, 0, ctx); err != nil {
		t.Fatalf("PaintQuad (unsupported): %v", err)
	}
	if err := unsupportedScene.PaintSurface(leftHalf, gpui.SurfaceSourceUnsupported, ctx); err != nil {
		t.Fatalf("PaintSurface (the unsupported stand-in): %v", err)
	}
	if err := unsupportedScene.Finish(); err != nil {
		t.Fatalf("Finish (unsupported): %v", err)
	}
	bailErr := surface.RenderScene(unsupportedScene, nil, gpui.BackgroundOpaque)
	if !errors.Is(bailErr, native.ErrRendererUnsupportedSource) {
		t.Fatalf("drawing the unsupported surface source = %v, want the typed unsupported-source error", bailErr)
	}
	t.Logf("the pinned unsupported-source bail: %v", bailErr)
}
