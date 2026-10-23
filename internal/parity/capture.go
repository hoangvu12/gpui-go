//go:build windows

// The offscreen capture half of the harness (ticket 01's third prior
// art: examples/zterm TestCapture). A hidden, never-shown host window
// placed off screen owns the render surface; the test window's scene
// (device pixels, at the test window's scale factor) is rendered
// through it without presenting, and staged back as owned RGBA bytes
// (Renderer.RenderToImage). No window is ever shown or activated.
package parity

import (
	"fmt"
	"image"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

// offscreenOrigin is where the hidden capture window lives: far off
// screen, like zterm's capture window (never shown, never activated).
var offscreenOrigin = gpui.Point{X: -32000, Y: -32000}

// captureHost is one replay's offscreen render surface: a real app +
// host whose only window is hidden, and the renderer that renders the
// test window's scenes through it. It follows the bootApp/onHost
// pattern of examples/zterm TestCapture and internal/headlessspec.
type captureHost struct {
	t        testing.TB
	app      *gpui.App
	host     *gpui.Host
	runDone  chan error
	renderer *gpui.Renderer

	mu     sync.Mutex
	window *gpui.Window
	extent [2]uint32
}

// newCaptureHost boots the hidden capture app. The host is stopped and
// the renderer closed at the test's end.
func newCaptureHost(t testing.TB) (*captureHost, error) {
	app := gpui.NewApp()
	host := gpui.NewHost()
	if err := app.Attach(host); err != nil {
		return nil, fmt.Errorf("attach: %w", err)
	}
	if err := host.Start(); err != nil {
		return nil, fmt.Errorf("host start: %w", err)
	}
	runDone := make(chan error, 1)
	go func() { runDone <- app.Run(host) }()
	renderer, err := gpui.NewRendererManual()
	if err != nil {
		_ = host.Stop()
		<-runDone
		return nil, fmt.Errorf("renderer: %w", err)
	}
	h := &captureHost{t: t, app: app, host: host, runDone: runDone, renderer: renderer}
	t.Cleanup(func() {
		h.post(func() {
			if h.window != nil && h.window.Alive() {
				h.window.Close()
			}
		})
		if err := renderer.Close(); err != nil {
			t.Errorf("parity: renderer close: %v", err)
		}
		if err := host.Stop(); err != nil {
			t.Errorf("parity: host stop: %v", err)
		}
		<-runDone
	})
	return h, nil
}

// post runs f on the host's foreground thread and waits for it (the
// onHost pattern; generous timeout, failing the test on a hang).
func (h *captureHost) post(f func()) {
	done := make(chan struct{})
	if !h.host.Post(func() {
		defer close(done)
		f()
	}) {
		close(done)
	}
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		h.t.Errorf("parity: timed out waiting for the capture host thread")
	}
}

// hiddenWindowAt returns the hidden capture window, resized so its
// client extent is exactly width x height device pixels (the
// surfaceAtDeviceExtent pattern: the logical size is chosen inside the
// device cell that truncates to the wanted extent, and the surface's
// extent is verified).
func (h *captureHost) hiddenWindowAt(width, height uint32) (*gpui.Window, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.window != nil && h.window.Alive() && h.extent[0] == width && h.extent[1] == height {
		return h.window, nil
	}
	if h.window == nil {
		window, err := h.app.OpenWindow(gpui.WindowOptions{
			Title:             "parity capture (hidden)",
			Resizable:         true,
			DirectComposition: true,
			Show:              false,
			Activate:          false,
			Bounds: gpui.Bounds{
				Origin: offscreenOrigin,
				// The size is corrected below, once the window's scale
				// factor is known.
				Size: gpui.Size{Width: float32(width) / 2, Height: float32(height) / 2},
			},
		})
		if err != nil {
			return nil, fmt.Errorf("opening the hidden capture window: %w", err)
		}
		h.window = window
	}
	// Size the client area to the exact device extent: the logical size
	// that truncates to it at the window's scale factor.
	var scale float32
	h.post(func() {
		scale, _ = h.window.ScaleFactor()
	})
	if scale <= 0 {
		return nil, fmt.Errorf("capture window scale factor %v", scale)
	}
	logicalW := (float32(width) + 0.5) / scale
	logicalH := (float32(height) + 0.5) / scale
	h.post(func() {
		h.window.SetBounds(gpui.Bounds{Origin: offscreenOrigin, Size: gpui.Size{Width: logicalW, Height: logicalH}})
	})
	deadline := time.Now().Add(10 * time.Second)
	for {
		var bounds gpui.Bounds
		var err error
		h.post(func() { bounds, err = h.window.Bounds() })
		if err == nil && abs32(bounds.Size.Width-logicalW) < 1 && abs32(bounds.Size.Height-logicalH) < 1 {
			break
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("the hidden capture window never reached %vx%v logical (at %v): %+v", logicalW, logicalH, scale, bounds)
		}
		time.Sleep(20 * time.Millisecond)
	}
	var infoWidth, infoHeight uint32
	var surface *gpui.Surface
	var surfaceErr error
	h.post(func() {
		surface, surfaceErr = h.renderer.GetOrCreateSurface(h.window)
		if surfaceErr != nil {
			return
		}
		info, err := surface.Info()
		if err != nil {
			surfaceErr = err
			return
		}
		infoWidth, infoHeight = info.Width, info.Height
	})
	if surfaceErr != nil {
		return nil, fmt.Errorf("capture surface: %w", surfaceErr)
	}
	if infoWidth != width || infoHeight != height {
		return nil, fmt.Errorf("capture surface extent %dx%d, want exactly %dx%d (scale %v, logical %vx%v)",
			infoWidth, infoHeight, width, height, scale, logicalW, logicalH)
	}
	h.extent = [2]uint32{width, height}
	return h.window, nil
}

// renderScene renders the test window's scene (with its glyph atlas)
// through the hidden window's surface and returns the owned image as
// an opaque RGBA image. The scene's device-pixel extent must match the
// surface's exactly, or the render would silently clip.
func (h *captureHost) renderScene(scene *gpui.Scene, atlas *gpui.Atlas, width, height uint32) (*image.RGBA, error) {
	window, err := h.hiddenWindowAt(width, height)
	if err != nil {
		return nil, err
	}
	var img *gpui.RenderedImage
	var renderErr error
	h.post(func() {
		img, renderErr = h.renderer.RenderToImage(window, scene, atlas, gpui.WindowBackgroundOpaque)
	})
	if renderErr != nil {
		return nil, fmt.Errorf("offscreen render: %w", renderErr)
	}
	if img.Width() != width || img.Height() != height {
		return nil, fmt.Errorf("offscreen capture %dx%d, want %dx%d", img.Width(), img.Height(), width, height)
	}
	return imageFromRendered(img), nil
}

// imageFromRendered copies the owned capture bytes into a standard
// image.RGBA (the capture's packed rows are already RGBA8; the copy
// drops the read-only aliasing on the native buffer).
func imageFromRendered(img *gpui.RenderedImage) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, int(img.Width()), int(img.Height())))
	stride := img.BytesPerRow()
	pixels := img.Pixels()
	for y := 0; y < int(img.Height()); y++ {
		copy(out.Pix[y*out.Stride:y*out.Stride+int(img.Width())*4], pixels[y*stride:])
	}
	return out
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
