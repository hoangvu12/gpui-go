//go:build windows

// The capture half of ticket30's real corpus: the capture stream
// stopping and destroying while a scene still references its current
// frame. Stop is not release (the published frame stays drawable), the
// destroy releases it and every later draw of the referencing scene is
// the typed bad-handle record — never a silent blank surface — and
// neither GC nor elapsed time substitutes for the logical release.
//
// Slot discipline (the native capture slot registry): this test keeps
// at most ONE live stream and destroys it before finishing.
package shutdownspec

import (
	"errors"
	"math"
	"runtime"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/native"
)

// TestCaptureStreamStopDestroyUnderLiveSceneReference drives the real
// close-order record over the capture service:
//
//   - A scene painted with the stream's current frame renders while the
//     stream is live.
//   - GC and elapsed time do not release the frame (the stream's
//     logical ownership does — assert the state, not the timing).
//   - Stop stops the session (the pin's CaptureStream drop ->
//     capture.stop()) while the published frame stays resolvable: late
//     draws keep working, and stopping an already-stopped stream is a
//     no-op (the exactly-once guard).
//   - Destroy drops the session, pool and item and releases every
//     published frame: the scene's next render is the typed
//     bad-handle/stale-handle record (renderer draw.rs
//     frame_texture_for_draw's slot check — an explicit error, never a
//     silent blank), a present through the same scene is refused with
//     no submission accepted, and every later stream call reports the
//     typed stale-handle record (the double-destroy guard).
func TestCaptureStreamStopDestroyUnderLiveSceneReference(t *testing.T) {
	f := bootRealApp(t)
	app := f.app
	window := f.openShown(t, "shutdownspec capture", 320, 220, nil)

	renderer, err := gpui.NewRenderer()
	if err != nil {
		t.Fatalf("gpui.NewRenderer: %v", err)
	}
	t.Cleanup(func() {
		if err := renderer.Close(); err != nil && !isRendererClosedError(err) {
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
	scale := math.Float32frombits(info.ScaleBits)
	if width == 0 || height == 0 || scale == 0 {
		t.Fatalf("surface extent = %dx%d scale %v, want non-zero", width, height, scale)
	}

	// Distinct content on the monitor first (the captured frame
	// deterministically includes this window's pixels).
	blue, err := renderer.PresentClear(window, gpui.Color{R: 0.1, G: 0.3, B: 0.9, A: 1})
	if err != nil {
		t.Fatalf("the blue present: %v", err)
	}
	if err := blue.WaitForRetirement(15 * time.Second); err != nil {
		t.Fatalf("the blue present did not retire: %v", err)
	}

	// The capture stream on the renderer's device (the same-device
	// producer), created and polled on the foreground thread.
	var stream *gpui.ScreenCaptureStream
	f.sync(t, func() {
		sources, err := app.ScreenCaptureSources()
		if err != nil {
			t.Fatalf("app.ScreenCaptureSources: %v", err)
		}
		var main gpui.ScreenCaptureSource
		found := false
		for _, source := range sources {
			if source.IsMain {
				main, found = source, true
				break
			}
		}
		if !found {
			t.Fatalf("none of the %d enumerated sources is the main monitor — this machine is expected to capture", len(sources))
		}
		stream, err = app.ScreenCaptureStream(main)
		if err != nil {
			t.Fatalf("app.ScreenCaptureStream(main): %v", err)
		}
	})
	time.Sleep(300 * time.Millisecond) // let the composition settle into the frame pool
	var frame gpui.ScreenCaptureFrame
	f.sync(t, func() {
		var err error
		frame, err = stream.PollForFrame(0, 10*time.Second)
		if err != nil {
			t.Fatalf("PollForFrame: %v", err)
		}
	})
	t.Logf("captured frame handle=%#x pool=%dx%d content=%dx%d",
		frame.Handle, frame.Width, frame.Height, frame.ContentWidth, frame.ContentHeight)

	// The scene that references the frame: a slate background with the
	// capture surface on the left half (the capturespec draw pattern).
	logicalWidth, logicalHeight := float32(width)/scale, float32(height)/scale
	windowLogical := gpui.Bounds{Size: gpui.Size{Width: logicalWidth, Height: logicalHeight}}
	leftHalf := gpui.Bounds{Size: gpui.Size{Width: logicalWidth / 2, Height: logicalHeight}}
	ctx := gpui.NewPaintContext(scale, windowLogical, 1.0)
	slate := gpui.RgbaToHsla(0x334155ff)
	scene, err := gpui.NewScene()
	if err != nil {
		t.Fatalf("NewScene: %v", err)
	}
	t.Cleanup(func() { _ = scene.Dispose() })
	if err := scene.PaintQuad(gpui.PaintQuad{Bounds: windowLogical, Background: gpui.SolidBackground(slate)}, 0, ctx); err != nil {
		t.Fatalf("PaintQuad: %v", err)
	}
	if err := scene.PaintCaptureSurface(leftHalf, frame, ctx); err != nil {
		t.Fatalf("PaintCaptureSurface with the live frame: %v", err)
	}
	if err := scene.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	// Live: the referencing scene renders.
	if err := surface.RenderScene(scene, nil, gpui.BackgroundOpaque); err != nil {
		t.Fatalf("RenderScene while the stream is live: %v", err)
	}

	// Neither GC nor elapsed time releases the frame: the stream's
	// logical ownership is the release (assert the state, not the
	// timing).
	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	if err := surface.RenderScene(scene, nil, gpui.BackgroundOpaque); err != nil {
		t.Fatalf("RenderScene after GC and elapsed time = %v, want nil (the stream still owns the published frame)", err)
	}

	// Stop: the session stops; the current published frame stays
	// resolvable, so the referencing scene keeps drawing (stop is not
	// release — the pin's CaptureStream::drop stops the session; the
	// destroy below is the teardown).
	if err := stream.Stop(); err != nil {
		t.Fatalf("stream.Stop(): %v", err)
	}
	if err := stream.Stop(); err != nil {
		t.Fatalf("the second stream.Stop() = %v, want the no-op (the exactly-once guard)", err)
	}
	if err := surface.RenderScene(scene, nil, gpui.BackgroundOpaque); err != nil {
		t.Fatalf("RenderScene after Stop = %v, want nil (the published frame stays resolvable after the session stops)", err)
	}

	// Destroy: the frames release; the referencing scene's next render
	// is the typed bad-handle record (the explicit draw error, never a
	// silent blank surface).
	if err := stream.Destroy(); err != nil {
		t.Fatalf("stream.Destroy(): %v", err)
	}
	renderErr := surface.RenderScene(scene, nil, gpui.BackgroundOpaque)
	if !errors.Is(renderErr, native.ErrRendererStaleHandle) && !errors.Is(renderErr, native.ErrRendererBadHandle) {
		t.Fatalf("RenderScene after Destroy = %v, want the typed stale/bad-handle record (the released frame is an explicit error)", renderErr)
	}
	t.Logf("the released frame's draw record: %v", renderErr)

	// A present through the same scene is refused with no submission
	// accepted (the ledger is unchanged — the failure is not a tracked
	// partial submit).
	_, accepted, retired, _, inFlight := renderer.Ledger()
	drawSub, drawErr := surface.DrawScene(scene, nil, gpui.BackgroundOpaque)
	if drawErr == nil {
		t.Fatal("DrawScene after the stream destroyed succeeded, want the typed refusal")
	}
	if drawSub != nil {
		t.Fatal("the refused DrawScene returned a submission token")
	}
	if !errors.Is(drawErr, native.ErrRendererStaleHandle) && !errors.Is(drawErr, native.ErrRendererBadHandle) {
		t.Fatalf("DrawScene after Destroy = %v, want the typed stale/bad-handle record", drawErr)
	}
	t.Logf("the released frame's present record: %v", drawErr)
	_, accepted2, retired2, _, inFlight2 := renderer.Ledger()
	if accepted2 != accepted || retired2 != retired || inFlight2 != inFlight {
		t.Fatalf("ledger changed across the refused draw: accepted %d->%d retired %d->%d in-flight %d->%d, want unchanged",
			accepted, accepted2, retired, retired2, inFlight, inFlight2)
	}

	// Every later stream call reports the typed stale-handle record
	// (the double-destroy guard and the post-destroy poll).
	if err := stream.Destroy(); !errors.Is(err, gpui.ErrCaptureStaleHandle) {
		t.Fatalf("the second stream.Destroy() = %v, want the typed stale-handle record", err)
	}
	if _, err := stream.Poll(); !errors.Is(err, gpui.ErrCaptureStaleHandle) {
		t.Fatalf("Poll after Destroy = %v, want the typed stale-handle record", err)
	}
	if _, ok := stream.CurrentFrame(); ok {
		t.Fatal("CurrentFrame after Destroy still advertises a frame")
	}
}
