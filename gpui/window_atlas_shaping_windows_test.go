//go:build windows

package gpui

// Base port ticket 40's hidden-window tests (.scratch/gpui-base/issues/
// 40-core-image-sprites-shaping-cap.md). Both bugs were found by the
// base-gallery offscreen capture and both reproduce here on ONE hidden
// native window: created without WS_VISIBLE, never shown, never
// activated, parked off-screen — the gallery capture's own path. No
// visible window, no input injection, no focus change.
//
//  1. Image and SVG sprites must resolve against the window's sprite
//     atlas: the pin paints glyphs, SVGs and images into the ONE
//     per-window sprite atlas (window.rs:4865 / 5009 / 5104,
//     self.sprite_atlas), and the renderer resolves every sprite batch's
//     texture through that atlas (gpui_windows directx_renderer.rs
//     draw_*_sprites → self.atlas.get_texture_view). A present loop that
//     passes Window.GlyphAtlas() must therefore draw img() and svg()
//     sprites.
//  2. More distinct shaped texts than the native live-handle bound (256,
//     process-wide) across frames, and in one frame, must keep drawing:
//     the line-layout cache rotates (line_layout.rs finish_frame) and the
//     Go side evicts cache-owned handles instead of latching a failure.

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

// hiddenAppWindow is one started host plus one hidden, off-screen app
// window and a helper running work on the host's foreground thread.
type hiddenAppWindow struct {
	app    *App
	host   *Host
	window *Window
}

// onHost runs f on the host's foreground thread and waits for it.
func (h *hiddenAppWindow) onHost(t *testing.T, f func()) {
	t.Helper()
	done := make(chan struct{})
	if !h.host.Post(func() {
		defer close(done)
		f()
	}) {
		t.Fatalf("foreground post was dropped: the host loop exited")
	}
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatalf("timed out waiting for the host thread")
	}
}

// openHiddenAppWindow boots an app on a started host and opens one
// hidden window (Show false: never shown or activated) at an off-screen
// origin. Cleanup closes the window, stops the host and waits for the
// run loop — a panic on the stop path fails the process, which is the
// ticket's second symptom.
func openHiddenAppWindow(t *testing.T, title string, size Size, build func(*Window, *App) View) *hiddenAppWindow {
	t.Helper()
	app := NewApp()
	host := NewHost()
	if err := app.Attach(host); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if err := host.Start(); err != nil {
		t.Fatalf("host start: %v", err)
	}
	runDone := make(chan error, 1)
	go func() { runDone <- app.Run(host) }()
	h := &hiddenAppWindow{app: app, host: host}
	var openErr error
	h.onHost(t, func() {
		h.window, openErr = app.OpenWindowView(WindowOptions{
			Title:  title,
			Show:   false,
			Bounds: Bounds{Origin: Point{X: -32000, Y: -32000}, Size: size},
		}, build)
	})
	t.Cleanup(func() {
		h.onHost(t, func() {
			if h.window != nil && h.window.Alive() {
				h.window.Close()
			}
		})
		if err := host.Stop(); err != nil {
			t.Errorf("host stop: %v", err)
		}
		select {
		case <-runDone:
		case <-time.After(30 * time.Second):
			t.Errorf("timed out waiting for the app run loop to exit")
		}
	})
	if openErr != nil {
		t.Fatalf("OpenWindowView(%q): %v", title, openErr)
	}
	return h
}

// ---------------------------------------------------------------------------
// 1. Image and SVG sprites present through the window's sprite atlas
// ---------------------------------------------------------------------------

// spriteAtlasView paints the three sprite kinds side by side: glyphs
// (the window text), a raster img() (red), an img() of SVG bytes (green,
// rendered to a polychrome image) and an svg() element (a monochrome
// mask tinted blue).
type spriteAtlasView struct {
	raster  *LoadedImage
	svgImg  Image
	svgMask []byte
}

// Render implements Render[spriteAtlasView].
func (v *spriteAtlasView) Render(w *Window, cx *Context[spriteAtlasView]) AnyElement {
	return Div().SizeFull().Bg(White()).Flex().FlexRow().Gap(Px(12)).
		Child(Text("glyph sprites share the window atlas")).
		Child(Img(ImageSourceFromRender(v.raster)).Flex().W(Px(32)).H(Px(32))).
		Child(Img(ImageSourceFromImage(v.svgImg)).Flex().W(Px(32)).H(Px(32))).
		Child(Div().Flex().TextColor(Rgb(0x0000ff)).Child(Svg().Data(v.svgMask).Size(PxLength(32), PxLength(32)))).
		IntoElement()
}

// solidBGRA returns a w×h frame of one BGRA color.
func solidBGRA(w, h int, b, g, r byte) *LoadedImage {
	pixels := make([]byte, w*h*4)
	for i := 0; i < len(pixels); i += 4 {
		pixels[i], pixels[i+1], pixels[i+2], pixels[i+3] = b, g, r, 0xff
	}
	return NewLoadedImage(&RenderImage{Frames: []ImageFrame{{Width: w, Height: h, Pixels: pixels}}})
}

// TestHiddenWindowImageAndSvgSpritesDrawFromTheWindowAtlas reproduces
// ticket 40 bug 1: an img() and an svg() painted into a real window's
// frame must render through the atlas a present loop passes —
// Window.GlyphAtlas(), the window's sprite atlas. Pre-fix the images
// went into the application's image atlas and the capture failed with
// "polychrome sprite batch needs texture 0" (and SVG masks resolved
// against the wrong atlas's monochrome texture).
func TestHiddenWindowImageAndSvgSpritesDrawFromTheWindowAtlas(t *testing.T) {
	view := &spriteAtlasView{
		raster:  solidBGRA(32, 32, 0x00, 0x00, 0xff),
		svgImg:  NewImage(ImageFormatSVG, []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32"><rect width="32" height="32" fill="#00ff00"/></svg>`)),
		svgMask: []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32"><rect width="32" height="32" fill="#000000"/></svg>`),
	}
	h := openHiddenAppWindow(t, "ticket40 sprite atlas", Size{Width: 520, Height: 120}, func(w *Window, app *App) View {
		return ViewOf(NewEntity(app, w.Scope(), func(v *spriteAtlasView, cx *Context[spriteAtlasView]) {
			*v = *view
		}))
	})
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	t.Cleanup(func() { h.onHost(t, func() { renderer.Close() }) })

	// Draw until both images are painted (the SVG img() decodes on a
	// worker; its completion redraws) and the svg() mask landed.
	var scene *Scene
	deadline := time.Now().Add(15 * time.Second)
	for {
		var poly, mono int
		var drawErr error
		h.onHost(t, func() {
			scene, drawErr = DrawWindowFrame(h.window)
			if drawErr != nil {
				return
			}
			var quadsN, sub int
			quadsN, mono, sub, poly, drawErr = WindowPrimitiveCounts(h.window)
			_, _ = quadsN, sub
		})
		if drawErr != nil {
			t.Fatalf("DrawWindowFrame: %v", drawErr)
		}
		// Two images (polychrome) and the svg mask (monochrome; the
		// window text rasterizes as subpixel or monochrome glyphs).
		if poly >= 2 && mono >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the images never painted: %d polychrome, %d monochrome sprites", poly, mono)
		}
		time.Sleep(30 * time.Millisecond)
	}

	var image *RenderedImage
	var captureErr, presentErr error
	h.onHost(t, func() {
		atlas, err := h.window.GlyphAtlas()
		if err != nil {
			captureErr = err
			return
		}
		image, captureErr = renderer.RenderToImage(h.window, scene, atlas, WindowBackgroundOpaque)
		if captureErr != nil {
			return
		}
		// The present loop's own call: DrawScene with the window atlas
		// onto the hidden window's surface.
		surface, err := renderer.GetOrCreateSurface(h.window)
		if err != nil {
			presentErr = err
			return
		}
		_, presentErr = surface.DrawScene(scene, atlas, BackgroundOpaque)
	})
	if captureErr != nil {
		t.Fatalf("RenderToImage with the window atlas: %v", captureErr)
	}
	if presentErr != nil {
		t.Fatalf("DrawScene with the window atlas: %v", presentErr)
	}

	pixels := image.Pixels()
	stride := image.BytesPerRow()
	var red, green, blue int
	for y := 0; y < int(image.Height()); y++ {
		for x := 0; x < int(image.Width()); x++ {
			p := pixels[y*stride+x*4:]
			r, g, b := p[0], p[1], p[2]
			switch {
			case r > 200 && g < 60 && b < 60:
				red++
			case g > 200 && r < 60 && b < 60:
				green++
			case b > 200 && r < 60 && g < 60:
				blue++
			}
		}
	}
	// Each 32-logical-pixel sprite covers at least 32×32 device pixels.
	const minPixels = 32 * 32 / 2
	if red < minPixels {
		t.Errorf("the raster img() drew %d red pixels, want >= %d", red, minPixels)
	}
	if green < minPixels {
		t.Errorf("the SVG img() drew %d green pixels, want >= %d", green, minPixels)
	}
	if blue < minPixels {
		t.Errorf("the svg() mask drew %d blue pixels, want >= %d", blue, minPixels)
	}

	// drop_image reaches the window's sprite atlas (app.rs:2815-2825
	// walks every window): the painted frames leave it.
	var before, after bool
	var dropErr error
	h.onHost(t, func() {
		atlas, err := h.window.GlyphAtlas()
		if err != nil {
			dropErr = err
			return
		}
		before = atlas.HasImageAtlasEntry(view.raster)
		dropErr = h.window.DropImage(view.raster)
		after = atlas.HasImageAtlasEntry(view.raster)
	})
	if dropErr != nil {
		t.Fatalf("DropImage: %v", dropErr)
	}
	if !before || after {
		t.Errorf("the window atlas held the raster before DropImage = %v, after = %v; want true, false", before, after)
	}
}

// ---------------------------------------------------------------------------
// 2. More shaped texts than the native handle bound
// ---------------------------------------------------------------------------

// shapingCapView renders count distinct texts of one page.
type shapingCapView struct {
	page, count int
}

// Render implements Render[shapingCapView].
func (v *shapingCapView) Render(w *Window, cx *Context[shapingCapView]) AnyElement {
	column := Div().Flex().FlexCol()
	for i := 0; i < v.count; i++ {
		column = column.Child(Text(fmt.Sprintf("page %d, distinct line %d", v.page, i)))
	}
	return Div().Flex().SizeFull().Child(column).IntoElement()
}

// TestHiddenWindowShapedTextsPastTheHandleBoundKeepDrawing reproduces
// ticket 40 bug 2: a window whose frames shape more distinct texts than
// the native live shaping-handle bound (256) — across page switches
// (the current plus previous frame's lines) and inside one frame — keeps
// drawing, never latches "measure callback failed", stays under the
// bound and stops cleanly. Pre-fix the first frame past the bound failed
// in layout and every later frame failed the same way.
func TestHiddenWindowShapedTextsPastTheHandleBoundKeepDrawing(t *testing.T) {
	system, err := DefaultTextSystem()
	if err != nil {
		t.Fatalf("DefaultTextSystem: %v", err)
	}
	bound := system.MaxShapingHandles()
	baseline := system.LiveShapingHandles()

	var page Entity[shapingCapView]
	h := openHiddenAppWindow(t, "ticket40 shaping cap", Size{Width: 480, Height: 360}, func(w *Window, app *App) View {
		page = NewEntity(app, w.Scope(), func(v *shapingCapView, cx *Context[shapingCapView]) {
			v.count = 10
		})
		return ViewOf(page)
	})

	maxLive := 0
	show := func(p, count int) {
		t.Helper()
		for draw := 0; draw < 2; draw++ {
			var drawErr error
			h.onHost(t, func() {
				if draw == 0 {
					page.Update(h.app, func(v *shapingCapView, cx *Context[shapingCapView]) {
						v.page, v.count = p, count
						cx.Notify()
					})
				}
				_, drawErr = DrawWindowFrame(h.window)
			})
			if drawErr != nil {
				t.Fatalf("page %d (%d texts) draw %d: %v", p, count, draw, drawErr)
			}
			if live := system.LiveShapingHandles(); live > maxLive {
				maxLive = live
			}
		}
	}

	// Page switches: every page's 150 texts are distinct, so the current
	// and previous frame together hold ~300 distinct lines.
	for p := 0; p < 6; p++ {
		show(p, 150)
	}
	// One frame past the bound on its own.
	show(100, 400)
	// Back to a small page: the window recovered, nothing latched.
	show(200, 10)

	if maxLive > bound {
		t.Errorf("max LiveShapingHandles = %d exceeds the native bound %d", maxLive, bound)
	}
	probeText := "a fresh shape after the window ran past the bound"
	probe, err := system.ShapeText(TextLayoutRequest{
		Text:     probeText,
		FontSize: 16,
		Runs:     []TextRun{{Len: len(probeText), Font: DefaultFontDescriptor()}},
	})
	if err != nil {
		t.Fatalf("a fresh ShapeText after the run: %v", err)
	}
	_ = probe.Dispose()
	t.Logf("baseline %d, max %d live shaping handles (bound %d), final %d", baseline, maxLive, bound, system.LiveShapingHandles())
}

// TestHiddenWindowOpenedWhileHandlesExhaustedRecovers pins the stop-path
// symptom of ticket 40: a window opened while every shaping handle is
// held outside the line-layout caches (so nothing can be parked) fails
// its initial draw. That failure used to panic inside the host's window
// trampoline and resurface as a Host.Stop failure; it now logs like any
// later failed frame, and the window draws once handles are free.
func TestHiddenWindowOpenedWhileHandlesExhaustedRecovers(t *testing.T) {
	system, err := DefaultTextSystem()
	if err != nil {
		t.Fatalf("DefaultTextSystem: %v", err)
	}
	// Caller-held lines are never parked: shape until the bound refuses
	// (cache-owned lines of earlier tests are parked on the way).
	var held []*WrappedLine
	release := func() {
		for _, line := range held {
			_ = line.Dispose()
		}
		held = nil
	}
	t.Cleanup(release)
	for i := 0; ; i++ {
		text := fmt.Sprintf("caller-held line %d", i)
		line, err := system.ShapeText(TextLayoutRequest{Text: text, FontSize: 16, Runs: []TextRun{{Len: len(text), Font: DefaultFontDescriptor()}}})
		if errors.Is(err, ErrTextShapingLimit) {
			break
		}
		if err != nil {
			t.Fatalf("ShapeText %d: %v", i, err)
		}
		held = append(held, line)
		if i > system.MaxShapingHandles() {
			t.Fatalf("shaped %d caller-held lines past the bound %d", i, system.MaxShapingHandles())
		}
	}

	h := openHiddenAppWindow(t, "ticket40 exhausted open", Size{Width: 320, Height: 200}, func(w *Window, app *App) View {
		return ViewOf(NewEntity(app, w.Scope(), func(v *shapingCapView, cx *Context[shapingCapView]) {
			v.count = 5
		}))
	})
	if got := WindowDrawCount(h.window); got != 0 {
		t.Fatalf("the initial draw completed with every handle held (draws = %d)", got)
	}

	release()
	var drawErr error
	h.onHost(t, func() { _, drawErr = DrawWindowFrame(h.window) })
	if drawErr != nil {
		t.Fatalf("DrawWindowFrame after the handles were released: %v", drawErr)
	}
	// The cleanup's Host.Stop reports any panic the host recorded.
}
