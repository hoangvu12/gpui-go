//go:build windows

package pathspec

import (
	"math"
	"runtime"
	"testing"
	"time"

	"gpui-go/gpui"
	"gpui-go/internal/native"
)

// The real-window composite draw of ticket16: a scene with a background
// quad, a nested filter group, a filled path and a REAL text sprite
// (rasterized through the text system, inserted into the atlas) rendered
// to a real window surface, verified through the staging readback,
// presented and retired through the renderer ledger. Environment
// expectation: this session creates real windows and owns a D3D11
// adapter — failures fail the tests, never skip.

// bootRealWindow opens a shown app window on a started host (the
// rendererspec boot pattern).
func bootRealWindow(t *testing.T, title string, w, h float32) (*gpui.App, *gpui.Window) {
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
			waitUntil(t, 10*time.Second, "the window to be destroyed", func() bool { return !window.Alive() })
		}
	})
	return app, window
}

func waitUntil(t *testing.T, timeout time.Duration, what string, cond func() bool) {
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

// buildCompositeScene paints the ticket16 composite: a background quad,
// a content-filter group over the left half (with a green box inside),
// a filled red path on the right half, and one REAL text glyph through
// the text system + atlas.
func buildCompositeScene(t *testing.T, ts *gpui.TextSystem, atlas *gpui.Atlas, scale float32, windowSize gpui.Size) (*gpui.Scene, gpui.FontID) {
	t.Helper()
	scene, err := gpui.NewScene()
	if err != nil {
		t.Fatalf("NewScene: %v", err)
	}
	mask := gpui.Bounds{Origin: gpui.Point{}, Size: windowSize}
	ctx := gpui.NewPaintContext(scale, mask, 1.0)

	// 1. The background quad: dark slate over the whole window.
	background := gpui.RgbaToHsla(0x334155ff)
	if err := scene.PaintQuad(gpui.PaintQuad{Bounds: mask, Background: gpui.SolidBackground(background)}, 0, ctx); err != nil {
		t.Fatalf("PaintQuad (background): %v", err)
	}

	// 2. The content-filter group over the left half with a green box
	// inside (the blur target and the group's content).
	groupBounds := gpui.Bounds{Origin: gpui.Point{X: 8, Y: 8}, Size: gpui.Size{Width: 140, Height: 100}}
	if err := scene.BeginFilterGroup(groupBounds, 4, gpui.Corners{TopLeft: 8, TopRight: 8, BottomRight: 8, BottomLeft: 8}, 0, ctx); err != nil {
		t.Fatalf("BeginFilterGroup: %v", err)
	}
	green := gpui.RgbaToHsla(0x16a34aff)
	inside := gpui.Bounds{Origin: gpui.Point{X: 16, Y: 20}, Size: gpui.Size{Width: 60, Height: 40}}
	if err := scene.PaintQuad(gpui.PaintQuad{Bounds: inside, Background: gpui.SolidBackground(green)}, 0, ctx); err != nil {
		t.Fatalf("PaintQuad (group content): %v", err)
	}
	if err := scene.EndFilterGroup(groupBounds, 4, gpui.Corners{TopLeft: 8, TopRight: 8, BottomRight: 8, BottomLeft: 8}, 0, ctx); err != nil {
		t.Fatalf("EndFilterGroup: %v", err)
	}

	// 3. The filled path: a red triangle on the right half.
	red := gpui.RgbaToHsla(0xdc2626ff)
	builder := gpui.NewPathBuilder()
	builder.MoveTo(170, 20)
	builder.LineTo(240, 20)
	builder.LineTo(170, 90)
	builder.Close()
	path, err := builder.Build()
	if err != nil {
		t.Fatalf("PathBuilder.Build: %v", err)
	}
	if len(path.Vertices) != 3 {
		t.Fatalf("triangle vertex count = %d, want 3", len(path.Vertices))
	}
	if err := scene.PaintPath(path, gpui.SolidBackground(red), ctx); err != nil {
		t.Fatalf("PaintPath: %v", err)
	}

	// 4. The REAL text sprite: resolve the system UI font, map the
	// glyph, rasterize and insert through PaintGlyph.
	identity, err := ts.ResolveFont(gpui.FontDescriptor{Family: ".SystemUIFont"})
	if err != nil {
		t.Fatalf("ResolveFont: %v", err)
	}
	glyphID, hasGlyph, err := ts.GlyphForChar(identity.FontID, 'G')
	if err != nil {
		t.Fatalf("GlyphForChar: %v", err)
	}
	if !hasGlyph {
		t.Fatalf("the system UI font has no glyph for 'G'")
	}
	white := gpui.RgbaToHsla(0xffffffff)
	if err := scene.PaintGlyph(gpui.Point{X: 170, Y: 160}, identity.FontID, glyphID, 32, white, ts, atlas, ctx, gpui.PaintTextOptions{}); err != nil {
		t.Fatalf("PaintGlyph: %v", err)
	}

	if err := scene.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	// The compiled plan must require the path target and the offscreen
	// target (the filter group).
	requirements, err := scene.Requirements()
	if err != nil {
		t.Fatalf("Requirements: %v", err)
	}
	if !requirements.UsesPathTarget {
		t.Errorf("the composite's plan does not use the path target")
	}
	if !requirements.UsesOffscreenTarget {
		t.Errorf("the composite's plan does not use the offscreen target (the filter group)")
	}
	meta, err := scene.Meta()
	if err != nil {
		t.Fatalf("Meta: %v", err)
	}
	// The text sprite lands in the monochrome or the subpixel pool
	// depending on the platform's recommended rendering mode (this
	// machine's ClearType default routes 'G' to the subpixel pool).
	if meta.PathCount != 1 || meta.MonochromeSpriteCount+meta.SubpixelSpriteCount != 1 {
		t.Errorf("composite meta: pathCount = %d, sprite counts = mono %d + sub %d, want 1 path and 1 sprite", meta.PathCount, meta.MonochromeSpriteCount, meta.SubpixelSpriteCount)
	}
	return scene, identity.FontID
}

// pixelAt reads one RGBA pixel from the readback buffer.
func pixelAt(pixels []byte, width, x, y int) (r, g, b byte) {
	offset := (y*width + x) * 4
	return pixels[offset], pixels[offset+1], pixels[offset+2]
}

func closeColor(actual, wanted byte) bool {
	diff := int(actual) - int(wanted)
	if diff < 0 {
		diff = -diff
	}
	return diff <= 8
}

// TestRealWindowCompositeDrawPresentsAndRetires is the ticket16
// real-window gate: the composite scene (path + text sprite + filter
// group + quads) drawn to a real surface, the pixels verified through
// the staging readback, the frame presented and retired through the
// ledger, with forced collections around the GPU work and the stale
// native scene handle rejected with the typed renderer error.
func TestRealWindowCompositeDrawPresentsAndRetires(t *testing.T) {
	_, window := bootRealWindow(t, "pathspec composite draw", 320, 220)

	renderer, err := gpui.NewRenderer()
	if err != nil {
		t.Fatalf("gpui.NewRenderer: %v", err)
	}
	t.Cleanup(func() {
		if err := renderer.Close(); err != nil {
			t.Errorf("renderer close: %v", err)
		}
	})

	surface, err := renderer.GetOrCreateSurfaceMode(window, native.SurfaceModeDCompPremultiplied)
	if err != nil {
		t.Fatalf("GetOrCreateSurfaceMode: %v", err)
	}
	info, err := surface.Info()
	if err != nil {
		t.Fatalf("surface.Info: %v", err)
	}
	scale := float32FromBits(info.ScaleBits)
	width := int(info.Width)
	height := int(info.Height)
	if width == 0 || height == 0 {
		t.Fatalf("surface extent = %dx%d, want non-zero", width, height)
	}

	// The real text system and atlas for the glyph sprite.
	ts, err := gpui.DefaultTextSystem()
	if err != nil {
		t.Fatalf("DefaultTextSystem: %v", err)
	}
	atlas, err := gpui.NewAtlas()
	if err != nil {
		t.Fatalf("NewAtlas: %v", err)
	}
	t.Cleanup(func() { _ = atlas.Dispose() })

	windowSize := gpui.Size{Width: float32(width) / scale, Height: float32(height) / scale}
	scene, _ := buildCompositeScene(t, ts, atlas, scale, windowSize)
	sceneHandle := scene.Handle()
	t.Cleanup(func() { _ = scene.Dispose() })

	// Forced collection around the GPU work: the native handles must
	// survive (the native registry owns them).
	runtime.GC()
	runtime.GC()

	// Render without presenting, then read the pixels back (the pinned
	// render_to_image path).
	if err := surface.RenderScene(scene, atlas, gpui.BackgroundOpaque); err != nil {
		t.Fatalf("RenderScene: %v", err)
	}
	runtime.GC()
	pixels, err := surface.ReadPixels()
	if err != nil {
		t.Fatalf("ReadPixels: %v", err)
	}
	if len(pixels) != width*height*4 {
		t.Fatalf("readback size = %d bytes, want %d (w=%d h=%d)", len(pixels), width*height*4, width, height)
	}

	// Pixel verification (device coordinates = logical * scale):
	// the top-right corner is the background color (dark slate,
	// blurred group edge excluded).
	// The background color's RGB: dark slate #334155 = (51, 65, 85).
	wantR, wantG, wantB := byte(0x33), byte(0x41), byte(0x55)
	r, g, b := pixelAt(pixels, width, width-8, 8)
	if !closeColor(r, wantR) || !closeColor(g, wantG) || !closeColor(b, wantB) {
		t.Errorf("background pixel = (%d,%d,%d), want the dark slate (%d,%d,%d)", r, g, b, wantR, wantG, wantB)
	}

	// The filled red path's interior.
	pathX, pathY := int(200*scale), int(40*scale)
	r, g, b = pixelAt(pixels, width, pathX, pathY)
	if !closeColor(r, 0xdc) || !closeColor(g, 0x26) || !closeColor(b, 0x26) {
		t.Errorf("path pixel at (%d,%d) = (%d,%d,%d), want red (220,38,38)", pathX, pathY, r, g, b)
	}

	// The text glyph: white text on the dark background — the glyph
	// area must be significantly brighter than the background. Sample a
	// row through the glyph's bounds and require at least one bright
	// pixel.
	bright := 0
	for dy := -2; dy <= 30; dy++ {
		for dx := -2; dx <= 30; dx++ {
			x := int(172*scale) + dx
			y := int(148*scale) + dy
			if x < 0 || y < 0 || x >= width || y >= height {
				continue
			}
			r, g, b := pixelAt(pixels, width, x, y)
			if r > 200 && g > 200 && b > 200 {
				bright++
			}
		}
	}
	if bright == 0 {
		t.Errorf("no bright pixels found in the glyph region — the text sprite did not draw")
	} else {
		t.Logf("glyph region: %d bright pixels (the text sprite drew through the atlas)", bright)
	}

	// The filter group's blurred green content: the group's center is
	// not pure background (the blurred green box lightens it).
	groupX, groupY := int(46*scale), int(40*scale)
	r, g, b = pixelAt(pixels, width, groupX, groupY)
	if closeColor(r, wantR) && closeColor(g, wantG) && closeColor(b, wantB) {
		t.Errorf("group center pixel = the background color; the blurred group content did not draw")
	}

	// Present + retire through the ledger.
	runtime.GC()
	sub, err := surface.DrawScene(scene, atlas, gpui.BackgroundOpaque)
	if err != nil {
		t.Fatalf("DrawScene: %v", err)
	}
	if err := sub.WaitForRetirement(30 * time.Second); err != nil {
		t.Fatalf("the composite submission did not retire: %v", err)
	}
	events, accepted, retired, quarantined, inFlight := renderer.Ledger()
	if accepted != 1 || retired != 1 || quarantined != 0 || inFlight != 0 {
		t.Fatalf("ledger counters accepted=%d retired=%d quarantined=%d in-flight=%d, want 1/1/0/0", accepted, retired, quarantined, inFlight)
	}
	if len(events) == 0 {
		t.Fatalf("ledger trace is empty")
	}
	t.Logf("composite draw retired through the ledger: %d events (accepted %d, retired %d)", len(events), accepted, retired)

	// The stale native scene handle: after Dispose, a native draw entry
	// rejects the handle with the typed scene error (ERR_SCENE).
	if err := scene.Dispose(); err != nil {
		t.Fatalf("Dispose: %v", err)
	}
	lib, err := native.Load(native.Options{})
	if err != nil {
		t.Fatalf("native.Load: %v", err)
	}
	svc, err := lib.Renderer()
	if err != nil {
		t.Fatalf("lib.Renderer: %v", err)
	}
	if _, err := svc.DrawScene(surface.NativeHandle(), native.SceneHandle(sceneHandle), native.BackgroundOpaque, 0); !isNativeSceneError(err) {
		t.Errorf("draw with the disposed scene handle = %v, want the typed scene-handle rejection", err)
	}

	// An unfinished scene is rejected the same way (the plan is
	// required before any draw).
	fresh, err := gpui.NewScene()
	if err != nil {
		t.Fatalf("NewScene (unfinished): %v", err)
	}
	defer fresh.Dispose()
	if _, err := svc.DrawScene(surface.NativeHandle(), native.SceneHandle(fresh.Handle()), native.BackgroundOpaque, 0); !isNativeSceneError(err) {
		t.Errorf("draw with the unfinished scene = %v, want the typed scene-handle rejection", err)
	}
}

func isNativeSceneError(err error) bool {
	if err == nil {
		return false
	}
	var statusErr *native.RendererStatusError
	if ok := asRendererStatus(err, &statusErr); ok {
		return statusErr.Code == -12
	}
	return false
}

func asRendererStatus(err error, target **native.RendererStatusError) bool {
	for err != nil {
		if typed, ok := err.(*native.RendererStatusError); ok {
			*target = typed
			return true
		}
		unwrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapper.Unwrap()
	}
	return false
}

func float32FromBits(bits uint32) float32 {
	return math.Float32frombits(bits)
}
