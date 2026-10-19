//go:build windows

// Package presentspec holds the consumer-gap ticket 01 tests: the
// README's documented present loop (OnRedraw → LastDrawnScene →
// Surface.DrawScene) presenting a window whose root view contains Text
// elements through the window's glyph atlas, the staging readback of
// the element-tree text (RenderScene + ReadPixels), the typed
// ErrSceneSpriteAtlas refusals for a nil or foreign atlas (never a raw
// native bad-handle failure), and the sprite-free present that keeps
// working exactly as before (the hello example's no-atlas shape). They
// exercise the paths from outside the runtime package, matching the
// repo convention (see internal/rendererspec and internal/pathspec).
//
// Environment expectation: these tests create and show real Win32
// windows and need a D3D11-capable adapter; this machine runs them
// interactively and they never silently skip.
package presentspec

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

// labelRootView is the root view of the presented window: a dark
// full-bounds background with white text (the readiness spike's text
// probe shape — the element tree shapes, rasterizes and paints the
// glyphs; the test presents them).
type labelRootView struct{}

// EntityID implements gpui.View: stateless recipe.
func (v *labelRootView) EntityID() (gpui.EntityID, bool) { return 0, false }

// RenderOnce implements gpui.View. The text child is an explicit
// Text element (the reference Text/SharedString element; a plain
// string child would convert to the same element).
func (v *labelRootView) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return gpui.Div().
		Flex().
		SizeFull().
		Justify(gpui.AlignContentCenter).
		AlignItems(gpui.AlignItemsCenter).
		Bg(gpui.RgbaToHsla(0x1e293bff)).
		TextColor(gpui.RgbaToHsla(0xffffffff)).
		TextXl().
		Child(gpui.Text("Hello, gpui!")).
		IntoElement()
}

// TestConsumerPresentLoopShowsElementTreeText is the ticket's
// acceptance: a real window whose root view contains Text elements,
// presented through the README's own present loop (the OnRedraw hook →
// LastDrawnScene → GetOrCreateSurface → Surface.DrawScene with the
// window's GlyphAtlas), the glyphs verified on screen (the presented
// submission retires through the real GPU ledger) and in a staging
// readback (RenderScene + ReadPixels), the nil and foreign atlas
// refusals typed and actionable (ErrSceneSpriteAtlas, not the native
// bad-handle status), and a sprite-free scene presenting with a nil
// atlas exactly as before.
func TestConsumerPresentLoopShowsElementTreeText(t *testing.T) {
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

	// The pacing renderer (the hello example's shape): OnRedraw's
	// submissions are polled and retired by the background worker.
	renderer, err := gpui.NewRenderer()
	if err != nil {
		t.Fatalf("gpui.NewRenderer: %v", err)
	}
	t.Cleanup(func() {
		if err := renderer.Close(); err != nil {
			t.Errorf("renderer close: %v", err)
		}
	})

	// The README's present loop, verbatim shape: the OnRedraw hook hands
	// the last drawn scene to the window's surface, passing the atlas
	// that holds the window's glyphs.
	var window *gpui.Window
	var presents atomic.Int32
	var presentErr atomic.Pointer[error]
	present := func() {
		if window == nil || !window.Alive() {
			return
		}
		scene := gpui.LastDrawnScene(window)
		if scene == nil {
			return
		}
		target, err := renderer.GetOrCreateSurface(window)
		if err != nil {
			presentErr.Store(&err)
			return
		}
		atlas, err := window.GlyphAtlas()
		if err != nil {
			presentErr.Store(&err)
			return
		}
		if _, err := target.DrawScene(scene, atlas, gpui.BackgroundOpaque); err != nil {
			presentErr.Store(&err)
			return
		}
		presents.Add(1)
	}

	// The window is opened on the host's foreground thread (the
	// authorspec sync pattern); its first frame (with the shaped text)
	// draws before the open returns.
	sync := func(f func()) {
		t.Helper()
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
			Title:     "presentspec element-tree text",
			Show:      true,
			Resizable: true,
			Bounds:    gpui.Bounds{Size: gpui.Size{Width: 480, Height: 320}},
			OnRedraw:  present,
		}, func(w *gpui.Window, app *gpui.App) gpui.View {
			return &labelRootView{}
		})
		if openErr != nil {
			t.Errorf("app.OpenWindowView: %v — this session is expected to create real windows", openErr)
			return
		}
	})
	if window == nil {
		t.Fatalf("the window did not open")
	}
	t.Cleanup(func() {
		if window.Alive() {
			window.Close()
			waitFor(t, 10*time.Second, "the window to be destroyed", func() bool { return !window.Alive() })
		}
	})

	// The frame loop draws the first frame and runs the present hook;
	// both must happen without an error in the loop itself.
	waitFor(t, 15*time.Second, "the present hook to present the first frame", func() bool {
		return presents.Load() >= 1 || presentErr.Load() != nil
	})
	if err := presentErr.Load(); err != nil {
		t.Fatalf("the present loop errored: %v", *err)
	}
	if got := gpui.WindowDrawCount(window); got < 1 {
		t.Fatalf("window draw count = %d, want >= 1", got)
	}

	// The element tree's text: the scene carries glyph sprites and the
	// window's atlas holds their tiles.
	var (
		scene                         *gpui.Scene
		atlas                         *gpui.Atlas
		surface                       *gpui.Surface
		quads, mono, sub, poly        int
		readbackPixels                []byte
		readbackWidth, readbackHeight uint32
	)
	sync(func() {
		var err error
		scene = gpui.LastDrawnScene(window)
		if scene == nil {
			t.Fatalf("the drawn frame produced no scene")
		}
		quads, mono, sub, poly, err = gpui.WindowPrimitiveCounts(window)
		if err != nil {
			t.Fatalf("WindowPrimitiveCounts: %v", err)
		}
		atlas, err = window.GlyphAtlas()
		if err != nil {
			t.Fatalf("window.GlyphAtlas: %v", err)
		}
		tiles, err := atlas.TileCount()
		if err != nil {
			t.Fatalf("atlas.TileCount: %v", err)
		}
		if tiles == 0 {
			t.Errorf("the window's atlas holds no tiles after drawing text")
		}
		surface, err = renderer.GetOrCreateSurface(window)
		if err != nil {
			t.Fatalf("GetOrCreateSurface: %v", err)
		}
		// The staging readback: render without presenting, read the
		// pixels back (the pinned render_to_image path).
		if err := surface.RenderScene(scene, atlas, gpui.BackgroundOpaque); err != nil {
			t.Fatalf("RenderScene with the window atlas: %v", err)
		}
		readbackWidth, readbackHeight = surface.Size()
		readbackPixels, err = surface.ReadPixels()
		if err != nil {
			t.Fatalf("ReadPixels: %v", err)
		}
	})
	if mono+sub+poly == 0 {
		t.Fatalf("the element tree drew no glyph sprites (quads=%d mono=%d sub=%d poly=%d) — the text did not shape", quads, mono, sub, poly)
	}
	if want := int(readbackWidth) * int(readbackHeight) * 4; len(readbackPixels) != want {
		t.Fatalf("readback size = %d bytes, want %d (w=%d h=%d)", len(readbackPixels), want, readbackWidth, readbackHeight)
	}
	t.Logf("element-tree text: %d quads, %d glyph sprites (%d mono + %d subpixel + %d polychrome)",
		quads, mono+sub+poly, mono, sub, poly)

	// The glyphs are visible in the readback: white text on the dark
	// background — count the bright pixels across the frame.
	bright := 0
	for offset := 0; offset+3 < len(readbackPixels); offset += 4 {
		if readbackPixels[offset] > 200 && readbackPixels[offset+1] > 200 && readbackPixels[offset+2] > 200 {
			bright++
		}
	}
	if bright < 30 {
		t.Errorf("bright glyph pixels = %d, want >= 30 — the element-tree text did not render into the readback", bright)
	} else {
		t.Logf("readback: %d bright glyph pixels across %dx%d device pixels", bright, readbackWidth, readbackHeight)
	}

	// The glyphs are visible on screen: presenting the same scene with
	// the window's atlas submits, completes and retires through the real
	// GPU ledger (acceptance is not completion; the wait runs off the
	// foreground thread so the pacing worker's retire dispatches can
	// land).
	var onScreen *gpui.Submission
	sync(func() {
		var err error
		onScreen, err = surface.DrawScene(scene, atlas, gpui.BackgroundOpaque)
		if err != nil {
			t.Fatalf("DrawScene with the window atlas: %v", err)
		}
	})
	if onScreen == nil {
		t.Fatalf("the on-screen DrawScene returned no submission")
	}
	if err := onScreen.WaitForRetirement(15 * time.Second); err != nil {
		t.Fatalf("the text submission did not retire: %v", err)
	}

	// The nil atlas refusal is typed and actionable (the readiness
	// spike's failure: the native bad-handle status): no submission is
	// accepted and the error tells the consumer what to pass. The
	// ledger snapshots bracket the refusals inside one foreground unit
	// so an activation-driven hook present (another window stealing
	// focus under the parallel suite) cannot land between them.
	var nilRenderErr, nilDrawErr error
	var nilDrawSub *gpui.Submission
	var acceptedNilBefore, acceptedNilAfter int
	sync(func() {
		_, acceptedNilBefore, _, _, _ = renderer.Ledger()
		nilRenderErr = surface.RenderScene(scene, nil, gpui.BackgroundOpaque)
		nilDrawSub, nilDrawErr = surface.DrawScene(scene, nil, gpui.BackgroundOpaque)
		_, acceptedNilAfter, _, _, _ = renderer.Ledger()
	})
	for _, err := range []error{nilRenderErr, nilDrawErr} {
		if !errors.Is(err, gpui.ErrSceneSpriteAtlas) {
			t.Errorf("nil-atlas draw error = %v, want the typed ErrSceneSpriteAtlas", err)
		}
		if err != nil && !strings.Contains(err.Error(), "GlyphAtlas") {
			t.Errorf("nil-atlas error is not actionable (no GlyphAtlas guidance): %v", err)
		}
	}
	if nilDrawSub != nil {
		t.Errorf("the refused nil-atlas DrawScene returned a submission token")
	}
	if acceptedNilAfter != acceptedNilBefore {
		t.Fatalf("ledger across the nil-atlas refusals: accepted %d -> %d — a refusal must not accept anything", acceptedNilBefore, acceptedNilAfter)
	}

	// The foreign atlas refusal is the same typed error: a fresh public
	// atlas never received the window's sprite tiles (the guard names the
	// pool and index mismatch before any GPU work is accepted).
	foreign, err := gpui.NewAtlas()
	if err != nil {
		t.Fatalf("NewAtlas (foreign): %v", err)
	}
	t.Cleanup(func() { _ = foreign.Dispose() })
	var foreignRenderErr, foreignDrawErr error
	var foreignDrawSub *gpui.Submission
	var acceptedForeignBefore, acceptedForeignAfter int
	sync(func() {
		_, acceptedForeignBefore, _, _, _ = renderer.Ledger()
		foreignRenderErr = surface.RenderScene(scene, foreign, gpui.BackgroundOpaque)
		foreignDrawSub, foreignDrawErr = surface.DrawScene(scene, foreign, gpui.BackgroundOpaque)
		_, acceptedForeignAfter, _, _, _ = renderer.Ledger()
	})
	for _, err := range []error{foreignRenderErr, foreignDrawErr} {
		if !errors.Is(err, gpui.ErrSceneSpriteAtlas) {
			t.Errorf("foreign-atlas draw error = %v, want the typed ErrSceneSpriteAtlas", err)
		}
		if err != nil && !strings.Contains(err.Error(), "GlyphAtlas") {
			t.Errorf("foreign-atlas error is not actionable (no GlyphAtlas guidance): %v", err)
		}
	}
	if foreignDrawSub != nil {
		t.Errorf("the refused foreign-atlas DrawScene returned a submission token")
	}
	if acceptedForeignAfter != acceptedForeignBefore {
		t.Fatalf("ledger across the foreign-atlas refusals: accepted %d -> %d — a refusal must not accept anything", acceptedForeignBefore, acceptedForeignAfter)
	}

	// A sprite-free scene keeps presenting exactly as today (the hello
	// example's pre-text shape: no atlas, no guard). The paint mask is
	// window-local (origin zeroed) exactly like the repo's real-window
	// paint helpers.
	quadScene, err := gpui.NewScene()
	if err != nil {
		t.Fatalf("NewScene (quad): %v", err)
	}
	t.Cleanup(func() { _ = quadScene.Dispose() })
	var quadSub *gpui.Submission
	sync(func() {
		bounds, err := window.Bounds()
		if err != nil {
			t.Fatalf("window bounds: %v", err)
		}
		scale, err := window.ScaleFactor()
		if err != nil {
			t.Fatalf("window scale: %v", err)
		}
		mask := gpui.Bounds{Size: bounds.Size}
		ctx := gpui.NewPaintContext(scale, mask, 1.0)
		slate := gpui.RgbaToHsla(0x334155ff)
		if err := quadScene.PaintQuad(gpui.PaintQuad{Bounds: mask, Background: gpui.SolidBackground(slate)}, 0, ctx); err != nil {
			t.Fatalf("PaintQuad: %v", err)
		}
		if err := quadScene.Finish(); err != nil {
			t.Fatalf("quad scene finish: %v", err)
		}
		if err := surface.RenderScene(quadScene, nil, gpui.BackgroundOpaque); err != nil {
			t.Fatalf("RenderScene (sprite-free, nil atlas): %v", err)
		}
		pixels, err := surface.ReadPixels()
		if err != nil {
			t.Fatalf("ReadPixels (sprite-free): %v", err)
		}
		r, g, b := pixelAt(pixels, int(readbackWidth), 8, 8)
		if !closeColor(r, 0x33) || !closeColor(g, 0x41) || !closeColor(b, 0x55) {
			t.Errorf("sprite-free background pixel = (%d,%d,%d), want the slate (%d,%d,%d)", r, g, b, 0x33, 0x41, 0x55)
		}
		submission, err := surface.DrawScene(quadScene, nil, gpui.BackgroundOpaque)
		if err != nil {
			t.Fatalf("DrawScene (sprite-free, nil atlas): %v", err)
		}
		quadSub = submission
	})
	if err := quadSub.WaitForRetirement(15 * time.Second); err != nil {
		t.Fatalf("the sprite-free submission did not retire: %v", err)
	}
}

// TestGlyphAtlasTestWindowTypedRefusal pins the typed refusal for
// windows whose text system owns no atlas: deterministic test windows
// never rasterize glyphs (their scenes carry no sprites), so the atlas
// accessor answers ErrNoGlyphAtlas instead of a nil atlas.
func TestGlyphAtlasTestWindowTypedRefusal(t *testing.T) {
	app := gpui.NewApp()
	window := gpui.NewTestWindow(app, gpui.Size{Width: 120, Height: 80})
	_, err := window.GlyphAtlas()
	if !errors.Is(err, gpui.ErrNoGlyphAtlas) {
		t.Fatalf("test-window GlyphAtlas error = %v, want the typed ErrNoGlyphAtlas", err)
	}
	var nilWindow *gpui.Window
	_, err = nilWindow.GlyphAtlas()
	if err == nil || !strings.Contains(err.Error(), "requires a window") {
		t.Fatalf("nil-window GlyphAtlas error = %v, want the nil-window refusal", err)
	}
}

// pixelAt reads one RGBA pixel from a readback buffer.
func pixelAt(pixels []byte, width, x, y int) (r, g, b byte) {
	offset := (y*width + x) * 4
	return pixels[offset], pixels[offset+1], pixels[offset+2]
}

// closeColor reports whether a channel is within a small tolerance.
func closeColor(actual, wanted byte) bool {
	diff := int(actual) - int(wanted)
	if diff < 0 {
		diff = -diff
	}
	return diff <= 8
}

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
