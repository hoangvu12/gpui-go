//go:build windows

package authorspec

// The consumer-gap 08 acceptance on a REAL window
// (.scratch/gpui-consumer-gaps/issues/08-drag-preview-text-offset.md):
// a drag preview view whose card carries a text child shows the text
// INSIDE the preview card, following the cursor, verified in a surface
// readback (glyph pixels inside the preview's debug-selector bounds).
// The drag is driven through the window's input dispatch on the host
// thread (no real mouse is moved), the frame is drawn by the
// real frame loop, and the readback renders the window's last drawn
// scene through the public RenderScene + ReadPixels pair with the
// window's glyph atlas — the consumer present loop's own path.

import (
	"math"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

// previewCardView is the drag preview: a light card with one text
// child (the element tree's text path — the text element's
// parent-relative placement under the preview root's element offset).
type previewCardView struct{}

func (v *previewCardView) Render(w *gpui.Window, cx *gpui.Context[previewCardView]) gpui.AnyElement {
	return gpui.Div().
		W(gpui.Px(200)).
		H(gpui.Px(40)).
		Bg(gpui.SolidBackground(gpui.Hsla{H: 0, S: 0, L: 0.92, A: 1})).
		Child("Drag preview label").
		DebugSelector("preview-card").
		IntoElement()
}

// dragPreviewRoot is the window's root: a dark surface with the drag
// source at the origin.
type dragPreviewRoot struct{}

func (v *dragPreviewRoot) Render(w *gpui.Window, cx *gpui.Context[dragPreviewRoot]) gpui.AnyElement {
	source := gpui.Div().
		ID("drag-source").
		Absolute().
		Left(gpui.Px(0)).
		Top(gpui.Px(0)).
		Size(gpui.Px(50)).
		CursorGrab().
		OnDrag("payload", func(value *string, offset gpui.Point, w *gpui.Window, app *gpui.App) gpui.View {
			return gpui.ViewOf(gpui.NewEntity(app, w.Scope(), func(v *previewCardView, cx *gpui.Context[previewCardView]) {}))
		})
	return gpui.Div().ID("root").Flex().SizeFull().
		Bg(gpui.SolidBackground(gpui.Hsla{H: 0, S: 0, L: 0.1, A: 1})).
		Child(source).
		IntoElement()
}

func TestDragPreviewTextPaintsInsideCardOnRealWindow(t *testing.T) {
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

	var window *gpui.Window
	sync(func() {
		var openErr error
		window, openErr = app.OpenWindowView(gpui.WindowOptions{
			Title:     "authorspec drag preview text",
			Show:      true,
			Resizable: true,
			Bounds:    gpui.Bounds{Size: gpui.Size{Width: 400, Height: 300}},
		}, func(w *gpui.Window, app *gpui.App) gpui.View {
			return gpui.ViewOf(gpui.NewEntity(app, w.Scope(), func(v *dragPreviewRoot, cx *gpui.Context[dragPreviewRoot]) {}))
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
		// Closing the last window quits the host loop, so this poll can
		// run after the loop exited: Alive (false on a stopped host) is
		// polled directly, never through the post-or-fail sync.
		waitForCond(t, 10*time.Second, "the window to be destroyed", func() bool { return !window.Alive() })
	})

	// Let the window draw its first frame.
	time.Sleep(200 * time.Millisecond)

	// Start the drag: hover, press, the threshold-crossing move that
	// starts it (the drag's cursor offset is the start position), then a
	// follow-up move that carries the preview to (100, 80). The events go
	// through the window's input dispatch on the host thread, not
	// synthetic WM_MOUSEMOVE messages: a WM_MOUSEMOVE arms the real
	// leave tracking (TrackMouseEvent), and with the real cursor away
	// from the window (under go test it opens off the desktop) Windows
	// answers with WM_MOUSELEAVE, whose exit moves the tracked pointer
	// off the window and the preview with it (browser-core ticket 10).
	left := gpui.MouseButtonLeft
	dispatch := func(event any) {
		sync(func() { window.DispatchInput(event, app) })
	}
	dispatch(&gpui.MouseMoveEvent{Position: gpui.Point{X: 10, Y: 10}})
	dispatch(&gpui.MouseDownEvent{Button: left, Position: gpui.Point{X: 10, Y: 10}, ClickCount: 1})
	dispatch(&gpui.MouseMoveEvent{Position: gpui.Point{X: 11, Y: 10}, PressedButton: &left})   // inside the threshold
	dispatch(&gpui.MouseMoveEvent{Position: gpui.Point{X: 120, Y: 100}, PressedButton: &left}) // starts the drag
	dispatch(&gpui.MouseMoveEvent{Position: gpui.Point{X: 220, Y: 180}, PressedButton: &left}) // carries the preview

	// Wait for a completed frame whose preview card sits at mouse -
	// cursor offset = (100, 80).
	var card gpui.Bounds
	waitForCond(t, 10*time.Second, "the drag preview at (100,80)", func() bool {
		var b gpui.Bounds
		var ok bool
		sync(func() { b, ok = gpui.WindowDebugBound(window, "preview-card") })
		if ok && b.Origin.X == 100 && b.Origin.Y == 80 && b.Size.Width == 200 && b.Size.Height == 40 {
			card = b
			return true
		}
		return false
	})

	// The consumer present path: the window's last drawn scene through
	// the surface with the window's glyph atlas, read back as pixels.
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
	var atlas *gpui.Atlas
	sync(func() { atlas, err = window.GlyphAtlas() })
	if err != nil {
		t.Fatalf("GlyphAtlas: %v", err)
	}
	var scene *gpui.Scene
	sync(func() { scene = gpui.LastDrawnScene(window) })
	if scene == nil {
		t.Fatal("the window has no drawn scene")
	}
	if err := surface.RenderScene(scene, atlas, gpui.BackgroundOpaque); err != nil {
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
	width, scale := int(info.Width), math.Float32frombits(info.ScaleBits)
	if scale == 0 {
		t.Fatalf("surface scale = 0")
	}

	// Glyph ink inside the preview card: pixels meaningfully darker than
	// the card's 0.92 background (the default dark text), counted in the
	// card's inner region (clear of the card edges).
	ink := 0
	for y := int((card.Origin.Y + 4) * scale); y < int((card.Origin.Y+card.Size.Height-4)*scale); y++ {
		for x := int((card.Origin.X + 4) * scale); x < int((card.Origin.X+card.Size.Width-4)*scale); x++ {
			i := (y*width + x) * 4
			if i+2 < len(pixels) {
				if pixels[i] < 0xB0 || pixels[i+1] < 0xB0 || pixels[i+2] < 0xB0 {
					ink++
				}
			}
		}
	}
	if ink < 30 {
		t.Fatalf("the drag preview carries only %d text ink pixels inside its card (want >= 30: the preview's text child paints inside the card, not at twice the drag offset)", ink)
	}

	// The text must stay INSIDE the card: the band below the card is the
	// window's flat dark background (L=0.1, about 0x1A), so any pixel
	// deviating from it is escaped content (the doubled offset landed
	// the glyphs there before the fix).
	below := 0
	for y := int((card.Origin.Y + card.Size.Height + 8) * scale); y < int((card.Origin.Y+card.Size.Height+28)*scale); y++ {
		for x := int((card.Origin.X + 4) * scale); x < int((card.Origin.X+card.Size.Width-4)*scale); x++ {
			i := (y*width + x) * 4
			if i+2 < len(pixels) {
				if abs8(pixels[i], 0x1A) > 6 || abs8(pixels[i+1], 0x1A) > 6 || abs8(pixels[i+2], 0x1A) > 6 {
					below++
				}
			}
		}
	}
	if below > 0 {
		t.Errorf("%d pixels deviate from the background below the preview card (the text escaped the card)", below)
	}
}

// abs8 returns the absolute difference of two bytes.
func abs8(a, b byte) int {
	if a > b {
		return int(a) - int(b)
	}
	return int(b) - int(a)
}
