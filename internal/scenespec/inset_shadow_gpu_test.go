//go:build windows

package scenespec

// The consumer-gap 09 acceptance on a real window and GPU
// (.scratch/gpui-consumer-gaps/issues/09-inset-shadow-padding-edge.md):
// a bordered, rounded panel with `inset 0 1px 0` paints the hard 1px
// band in the row just inside its top border (CSS's padding-edge
// inner shadow — where the reference frames draw the highlight), the
// same shadow with blur still paints its gradient, and a borderless
// element's inset shadow paints exactly as before (its padding edge IS
// its border box). The frame is drawn by the real element path
// (DrawWindowFrame) and read back through the public RenderScene +
// ReadPixels pair.

import (
	"math"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

// insetHighlightView renders one panel with mordbard's frame shape:
// dark background, 1px border, 12px radii, and one inset shadow.
type insetHighlightView struct {
	border float32
	blur   float32
}

func (v *insetHighlightView) Render(w *gpui.Window, cx *gpui.Context[insetHighlightView]) gpui.AnyElement {
	panel := gpui.Div().W(gpui.Px(160)).H(gpui.Px(80)).
		Bg(gpui.SolidBackground(gpui.Hsla{H: 0, S: 0, L: 0.15, A: 1})).
		Rounded(gpui.Px(12)).
		Shadow([]gpui.BoxShadow{{
			Color:     gpui.SolidBackground(gpui.Hsla{H: 0, S: 0, L: 1, A: 1}),
			OffsetY:   1,
			BlurRadius: v.blur,
			Inset:     true,
		}}).
		DebugSelector("inset-panel")
	if v.border > 0 {
		panel = panel.Border(gpui.Px(v.border)).
			BorderColor(gpui.SolidBackground(gpui.Hsla{H: 0, S: 0, L: 0.25, A: 1}))
	}
	return gpui.Div().SizeFull().Flex().Child(panel).IntoElement()
}

// renderInsetPanel opens a real (non-activating) window with one inset
// panel variant, draws one frame through the element path and returns
// the readback pixels plus the panel's logical bounds.
func renderInsetPanel(t *testing.T, border, blur float32) ([]byte, gpui.Bounds, int, float32) {
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
			t.Errorf("host stop: %v", err)
		}
		<-runDone
	})

	var window *gpui.Window
	sync := func(f func()) {
		done := make(chan struct{})
		if !host.Post(func() {
			defer close(done)
			f()
		}) {
			t.Fatalf("foreground post was dropped: the host loop exited")
		}
		<-done
	}
	sync(func() {
		var openErr error
		window, openErr = app.OpenWindowView(gpui.WindowOptions{
			Title:     "scenespec inset highlight",
			Show:      true, // shown WITHOUT activating: no focus is taken
			Resizable: true,
			Bounds:    gpui.Bounds{Size: gpui.Size{Width: 240, Height: 160}},
		}, func(w *gpui.Window, app *gpui.App) gpui.View {
			return gpui.ViewOf(gpui.NewEntity(app, w.Scope(), func(v *insetHighlightView, cx *gpui.Context[insetHighlightView]) {
				v.border = border
				v.blur = blur
			}))
		})
		if openErr != nil {
			t.Fatalf("OpenWindowView: %v — this session is expected to create real windows", openErr)
		}
	})
	t.Cleanup(func() {
		sync(func() {
			if window != nil && window.Alive() {
				window.Close()
			}
		})
		waitForScene(t, 10*time.Second, "the window to be destroyed", func() bool {
			alive := true
			sync(func() { alive = window.Alive() })
			return !alive
		})
	})
	time.Sleep(150 * time.Millisecond)

	renderer, err := gpui.NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	t.Cleanup(func() { _ = renderer.Close() })
	var surface *gpui.Surface
	sync(func() { surface, err = renderer.GetOrCreateSurface(window) })
	if err != nil || surface == nil {
		t.Fatalf("GetOrCreateSurface: %v (%v)", surface, err)
	}
	black, err := renderer.PresentClear(window, gpui.Color{R: 0, G: 0, B: 0, A: 1})
	if err != nil {
		t.Fatalf("black present: %v", err)
	}
	if err := black.WaitForRetirement(15 * time.Second); err != nil {
		t.Fatalf("black present retire: %v", err)
	}

	var scene *gpui.Scene
	sync(func() {
		var drawErr error
		scene, drawErr = gpui.DrawWindowFrame(window)
		if drawErr != nil {
			t.Fatalf("DrawWindowFrame: %v", drawErr)
		}
	})
	shadows, err := scene.Shadows()
	if err != nil {
		t.Fatalf("Shadows: %v", err)
	}
	if len(shadows) != 1 {
		t.Fatalf("the panel cast %d shadows, want 1", len(shadows))
	}
	if !shadows[0].Inset {
		t.Fatal("the shadow record is not inset")
	}

	panel, ok := gpui.WindowDebugBound(window, "inset-panel")
	if !ok {
		t.Fatal("no panel debug bounds")
	}
	if err := surface.RenderScene(scene, nil, gpui.BackgroundOpaque); err != nil {
		t.Fatalf("RenderScene: %v", err)
	}
	pixels, err := surface.ReadPixels()
	if err != nil {
		t.Fatalf("ReadPixels: %v", err)
	}
	info, err := surface.Info()
	if err != nil {
		t.Fatalf("surface.Info: %v", err)
	}
	return pixels, panel, int(info.Width), math.Float32frombits(info.ScaleBits)
}

// rowColor reads one pixel row's average channel value across a band
// of the panel's width (clear of the corners).
func rowColor(pixels []byte, width int, scale float32, band gpui.Bounds, row int) int {
	sum, count := 0, 0
	x0 := int((band.Origin.X + 20) * scale)
	x1 := int((band.Origin.X + band.Size.Width - 20) * scale)
	for x := x0; x < x1; x++ {
		i := (row*width + x) * 4
		if i+2 < len(pixels) {
			sum += int(pixels[i]) + int(pixels[i+1]) + int(pixels[i+2])
			count += 3
		}
	}
	if count == 0 {
		return -1
	}
	return sum / count
}

// TestInsetShadowBlurZeroPaintsInsideBorderOnRealGPU: with the 1px
// border and `inset 0 1px 0`, the row just inside the top border
// carries the highlight (brighter than the panel background), while
// the row under the border itself is the border's darker color.
func TestInsetShadowBlurZeroPaintsInsideBorderOnRealGPU(t *testing.T) {
	pixels, panel, width, scale := renderInsetPanel(t, 1, 0)

	// The rows: y=0 is the border (L=0.25), y=1 is the band (background
	// 0.15 + the full-white shadow), y=3 is the plain background.
	borderRow := rowColor(pixels, width, scale, panel, int((panel.Origin.Y+0)*scale))
	bandRow := rowColor(pixels, width, scale, panel, int((panel.Origin.Y+1.5)*scale))
	backgroundRow := rowColor(pixels, width, scale, panel, int((panel.Origin.Y+4)*scale))
	t.Logf("rows: border=%d band=%d background=%d (scale %v)", borderRow, bandRow, backgroundRow, scale)

	if bandRow <= backgroundRow+8 {
		t.Fatalf("the row inside the top border reads %d, want >= %d+8: the hard inset band paints just inside the border like CSS (background %d)", bandRow, backgroundRow, backgroundRow)
	}
	if borderRow >= bandRow {
		t.Errorf("the border row (%d) is not darker than the band row (%d)", borderRow, bandRow)
	}
}

// TestInsetShadowBlurPaintsFromPaddingEdgeOnRealGPU: with blur 3 the
// gradient still paints (the rows inside the border carry ink).
func TestInsetShadowBlurPaintsFromPaddingEdgeOnRealGPU(t *testing.T) {
	pixels, panel, width, scale := renderInsetPanel(t, 1, 3)

	backgroundRow := rowColor(pixels, width, scale, panel, int((panel.Origin.Y+8)*scale))
	bandRow := rowColor(pixels, width, scale, panel, int((panel.Origin.Y+1.5)*scale))
	t.Logf("blur rows: band=%d background=%d", bandRow, backgroundRow)
	if bandRow <= backgroundRow+8 {
		t.Fatalf("the blurred inset band reads %d, want >= %d+8: the gradient paints from the padding edge", bandRow, backgroundRow)
	}
}

// TestInsetShadowBorderlessUnchangedOnRealGPU: without a border the
// inset shadow paints at the element edge exactly as the pinned
// geometry always did (the first row inside the element carries the
// band).
func TestInsetShadowBorderlessUnchangedOnRealGPU(t *testing.T) {
	pixels, panel, width, scale := renderInsetPanel(t, 0, 0)

	bandRow := rowColor(pixels, width, scale, panel, int((panel.Origin.Y+0.5)*scale))
	backgroundRow := rowColor(pixels, width, scale, panel, int((panel.Origin.Y+4)*scale))
	t.Logf("borderless rows: band=%d background=%d", bandRow, backgroundRow)
	if bandRow <= backgroundRow+8 {
		t.Fatalf("the borderless band row reads %d, want >= %d+8: the borderless inset shadow keeps its edge band", bandRow, backgroundRow)
	}
}

// waitForScene polls cond until it holds or the timeout expires.
func waitForScene(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
