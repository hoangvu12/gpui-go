//go:build windows

// The browser-core ticket 53 (rev 16) renderer gates for the retained
// frame, the damage redraw and the dirty-rect present: a full-damage
// draw is readback-identical to the legacy path; a damaged redraw is
// bit-exact with a full redraw over a scripted sequence (a background
// change, a scroll, a transform tick through scene_update_property and
// a blur group straddling the damage edge); pixels outside the damage
// are unchanged; Present1 is exercised in both surface modes (or the
// full-present fallback fires); the blur-group damage expansion is
// observable through the damage fraction; and the state-bind counter
// drops with the pixels unchanged.
package rendererspec

import (
	"math"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/native"
)

// r53DrawDamage draws one damage frame (draining the submission so the
// surface stays destroyable) and returns the retained frame's pixels.
func r53DrawDamage(t *testing.T, surface *gpui.Surface, scene *gpui.Scene, rects []native.DamageRect, forceFull bool, w, h int) []byte {
	t.Helper()
	if err := scene.FinishIfNeeded(); err != nil {
		t.Fatalf("FinishIfNeeded: %v", err)
	}
	sub, err := surface.DrawSceneDamage(scene, nil, gpui.BackgroundOpaque, rects, forceFull)
	if err != nil {
		t.Fatalf("DrawSceneDamage: %v", err)
	}
	if err := sub.WaitForRetirement(15 * time.Second); err != nil {
		t.Fatalf("the damage submission did not retire: %v", err)
	}
	pixels, err := surface.ReadFramePixels()
	if err != nil {
		t.Fatalf("ReadFramePixels: %v", err)
	}
	if len(pixels) != w*h*4 {
		t.Fatalf("read back %d bytes, want %d", len(pixels), w*h*4)
	}
	return pixels
}

// r53SurfaceMode opens one shown window and attaches a surface in the
// requested composition mode.
func r53SurfaceMode(t *testing.T, title string, w, h float32, mode native.SurfaceMode) *gpui.Surface {
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
		_ = host.Stop()
		<-runDone
	})
	window, err := app.OpenWindowView(gpui.WindowOptions{
		Title:     title,
		Show:      true,
		Resizable: true,
		Bounds:    gpui.Bounds{Size: gpui.Size{Width: w, Height: h}},
	}, func(w *gpui.Window, app *gpui.App) gpui.View {
		return gpui.ViewOf(gpui.NewEntity(app, w.Scope(), func(v *r16BlankView, cx *gpui.Context[r16BlankView]) {
		}))
	})
	if err != nil {
		t.Fatalf("OpenWindowView: %v — this session is expected to create real windows", err)
	}
	t.Cleanup(func() {
		if window.Alive() {
			window.Close()
			waitFor(t, 10*time.Second, "the window to be destroyed", func() bool { return !window.Alive() })
		}
	})
	waitFor(t, 10*time.Second, "the window to be visible", window.Handle().Visible)
	renderer, err := gpui.NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	surface, err := renderer.GetOrCreateSurfaceMode(window, mode)
	if err != nil {
		t.Fatalf("GetOrCreateSurfaceMode: %v", err)
	}
	return surface
}

// r53BlurGroup opens one blurred content-filter group (the R15 chain
// shape) with a red quad inside.
func r53BlurGroup(t *testing.T, scene *gpui.Scene, x, y, w, h, radius float32) {
	t.Helper()
	fullMask := gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 4096, Height: 4096}}
	bounds := gpui.Bounds{Origin: gpui.Point{X: x, Y: y}, Size: gpui.Size{Width: w, Height: h}}
	chain := gpui.FilterChain{BlurRadius: radius}
	ctx := gpui.NewPaintContext(1, fullMask, 1)
	if err := scene.BeginFilterGroupChain(bounds, chain, gpui.Corners{}, 0, ctx); err != nil {
		t.Fatalf("BeginFilterGroupChain: %v", err)
	}
	r52Quad(t, scene, x, y, w, h, 0, 0.5, 1)
	if err := scene.EndFilterGroupChain(bounds, chain, gpui.Corners{}, 0, ctx); err != nil {
		t.Fatalf("EndFilterGroupChain: %v", err)
	}
}

// TestR53FullDamageMatchesLegacy: a full-damage draw through the
// retained target is readback-identical to the legacy path (the
// group-free direct route and the offscreen blur route alike).
func TestR53FullDamageMatchesLegacy(t *testing.T) {
	const w, h = 320, 240
	// The group-free scene: the legacy path renders directly into the
	// swap chain, the damage path routes through the retained target
	// and copies - the bytes must agree.
	surface := r16Surface(t, "rendererspec r53 full damage group-free", 320, 240)
	scene := r16Scene(t)
	r52Quad(t, scene, 0, 0, 320, 240, 0.58, 0.9, 1)
	r52Quad(t, scene, 40, 30, 120, 80, 0, 0.5, 1)
	legacy := r52Render(t, surface, scene, w, h)
	fullDamage := r53DrawDamage(t, surface, scene, nil, true, w, h)
	for i := range legacy {
		if legacy[i] != fullDamage[i] {
			t.Fatalf("full-damage byte %d differs from the legacy path: %d vs %d (group-free scene)", i, legacy[i], fullDamage[i])
		}
	}

	// The blur scene: the legacy path renders through the offscreen
	// scene target and dx_blit; the damage path through the retained
	// target and the raw copy - both bit-exact copies of the same
	// render.
	blurScene := r16Scene(t)
	r52Quad(t, blurScene, 0, 0, 320, 240, 0.58, 0.9, 1)
	r53BlurGroup(t, blurScene, 100, 60, 140, 100, 12)
	legacyBlur := r52Render(t, surface, blurScene, w, h)
	fullDamageBlur := r53DrawDamage(t, surface, blurScene, nil, true, w, h)
	for i := range legacyBlur {
		if legacyBlur[i] != fullDamageBlur[i] {
			t.Fatalf("full-damage byte %d differs from the legacy path: %d vs %d (blur scene)", i, legacyBlur[i], fullDamageBlur[i])
		}
	}
}

// r53ScriptedScene builds the scripted-sequence scene: a full-window
// background, a banner (the background change), a content block (the
// scroll), a transform property-node group and a blur group straddling
// the banner's edge.
func r53ScriptedScene(t *testing.T, bannerLight float32, scroll float32, rotationDeg float32) *gpui.Scene {
	t.Helper()
	scene := r16Scene(t)
	fullMask := gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 4096, Height: 4096}}
	// The white background.
	r52Quad(t, scene, 0, 0, 320, 240, 0, 1, 1)
	// The banner at (100,50,200,100): its color change is the first
	// damage (the "background change" of the scripted sequence).
	r52Quad(t, scene, 100, 50, 200, 100, 0, bannerLight, 1)
	// The scrolled content block (three stripes): the scroll is the
	// second damage (old ∪ new positions).
	for i := 0; i < 3; i++ {
		r52Quad(t, scene, 130, 170+float32(i)*16+scroll, 180, 12, 0.33, 0.4, 1)
	}
	// The transform property-node group at (80, 360): far from the
	// banner/blur region (its own damage comes from the property tick).
	rotationAbout := func(degrees float64) gpui.TransformationMatrix {
		angle := degrees * math.Pi / 180
		return r16MatrixAbout(
			[2][2]float32{
				{float32(math.Cos(angle)), float32(-math.Sin(angle))},
				{float32(math.Sin(angle)), float32(math.Cos(angle))},
			},
			[2]float32{30, 30},
			[2]float32{80, 360},
		)
	}
	group := gpui.EffectGroup{
		HasTransform: true,
		PropertyNode: true,
		NodeID:       7,
		Opacity:      1,
		Transform:    rotationAbout(float64(rotationDeg)),
		// The transform group's bounds are LOCAL-frame (the subtree's
		// records are local; the matrix places them at (80,360)) - the
		// mapped record bounds the update reports are then the composite
		// quad itself.
		Bounds:      gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 60, Height: 60}},
		ContentMask: fullMask,
	}
	if err := scene.BeginEffectGroup(group); err != nil {
		t.Fatalf("BeginEffectGroup: %v", err)
	}
	r16LocalQuads(t, scene)
	end := group
	end.IsStart = false
	if err := scene.EndEffectGroup(end); err != nil {
		t.Fatalf("EndEffectGroup: %v", err)
	}
	// The blur group at (250,80,120,120) straddling the banner's right
	// edge (300): the banner's damage intersects it, so the expansion
	// pulls the group's whole dilated region into the damage.
	r53BlurGroup(t, scene, 250, 80, 120, 120, 10)
	return scene
}

// r53Rect builds one damage rect.
func r53Rect(x, y, w, h int) native.DamageRect {
	return native.DamageRect{X: int32(x), Y: int32(y), W: int32(w), H: int32(h)}
}

// TestR53DamagedRedrawMatchesFullRedraw: the scripted sequence - a
// background change (a rebuilt scene), a scroll (a rebuilt scene) and
// a transform tick (scene_update_property on the same handle) - each
// damaged redraw is bit-exact with a full redraw of the same scene,
// and the pixels outside the damage are unchanged.
func TestR53DamagedRedrawMatchesFullRedraw(t *testing.T) {
	const w, h = 320, 480
	surface := r16Surface(t, "rendererspec r53 damaged vs full", 320, 480)

	// Frame 1: the initial scene, drawn FULL (the retained target is
	// fresh - the first damage draw on a new surface).
	scene1 := r53ScriptedScene(t, 0.3, 0, 0)
	frame1 := r53DrawDamage(t, surface, scene1, nil, false, w, h)

	// Frame 2: the banner brightens and the content scrolls by 8px. The
	// damage: the banner rect (dilated by 2 for the batch edges) and
	// the scroll stripes' old ∪ new span. The banner's rect intersects
	// the blur group's dilated region, so the native expansion adds it.
	scene2 := r53ScriptedScene(t, 0.7, 8, 0)
	damage := []native.DamageRect{
		r53Rect(98, 48, 204, 104),
		r53Rect(128, 168, 184, 76),
	}
	frame2Partial := r53DrawDamage(t, surface, scene2, damage, false, w, h)
	frame2Full := r53DrawDamage(t, surface, scene2, nil, true, w, h)
	for i := range frame2Partial {
		if frame2Partial[i] != frame2Full[i] {
			t.Fatalf("the damaged redraw's byte %d differs from the full redraw (%d vs %d) after the background change + scroll", i, frame2Partial[i], frame2Full[i])
		}
	}

	// Pixels outside the damage union are unchanged from frame 1 (the
	// scene outside the damage is the same content).
	damageUnion := func(x, y int) bool {
		for _, rect := range damage {
			if x >= int(rect.X)-2 && x < int(rect.X)+int(rect.W)+2 &&
				y >= int(rect.Y)-2 && y < int(rect.Y)+int(rect.H)+2 {
				return true
			}
		}
		// The blur group's EXPANDED region (the group (250,80,120,120)
		// dilated by 3*radius = 30): the native expansion pulls it in.
		if x >= 220 && x < 400 && y >= 50 && y < 230 {
			return true
		}
		return false
	}
	dirtyOutside := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if damageUnion(x, y) {
				continue
			}
			i := (y*w + x) * 4
			if frame1[i] != frame2Partial[i] || frame1[i+1] != frame2Partial[i+1] ||
				frame1[i+2] != frame2Partial[i+2] || frame1[i+3] != frame2Partial[i+3] {
				dirtyOutside++
			}
		}
	}
	if dirtyOutside != 0 {
		t.Fatalf("%d pixels changed OUTSIDE the damage union (the expanded banner/scroll/blur regions)", dirtyOutside)
	}

	// Frame 3: the transform tick on the SAME handle (the property-only
	// flow): the update's old ∪ new mapped bounds are the damage.
	oldBounds, newBounds, err := scene2.UpdateEffectMatrix(7, r16MatrixAbout(
		[2][2]float32{
			{float32(math.Cos(0.6)), float32(-math.Sin(0.6))},
			{float32(math.Sin(0.6)), float32(math.Cos(0.6))},
		},
		[2]float32{30, 30},
		[2]float32{80, 360},
	))
	if err != nil {
		t.Fatalf("UpdateEffectMatrix: %v", err)
	}
	tickDamage := []native.DamageRect{
		r53Rect(int(oldBounds.Origin.X)-2, int(oldBounds.Origin.Y)-2, int(oldBounds.Size.Width)+4, int(oldBounds.Size.Height)+4),
		r53Rect(int(newBounds.Origin.X)-2, int(newBounds.Origin.Y)-2, int(newBounds.Size.Width)+4, int(newBounds.Size.Height)+4),
	}
	frame3Partial := r53DrawDamage(t, surface, scene2, tickDamage, false, w, h)
	frame3Full := r53DrawDamage(t, surface, scene2, nil, true, w, h)
	for i := range frame3Partial {
		if frame3Partial[i] != frame3Full[i] {
			t.Fatalf("the damaged redraw's byte %d differs from the full redraw (%d vs %d) after the transform tick", i, frame3Partial[i], frame3Full[i])
		}
	}
	// Outside the tick's damage union, frame 3 equals frame 2 (the
	// property tick touched nothing else).
	tickUnion := func(x, y int) bool {
		for _, b := range []gpui.Bounds{oldBounds, newBounds} {
			if x >= int(b.Origin.X)-3 && x < int(b.Origin.X+b.Size.Width)+3 &&
				y >= int(b.Origin.Y)-3 && y < int(b.Origin.Y+b.Size.Height)+3 {
				return true
			}
		}
		return false
	}
	dirtyOutside = 0
	minX, minY, maxX, maxY := w, h, 0, 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if tickUnion(x, y) {
				continue
			}
			i := (y*w + x) * 4
			if frame2Full[i] != frame3Partial[i] || frame2Full[i+1] != frame3Partial[i+1] ||
				frame2Full[i+2] != frame3Partial[i+2] || frame2Full[i+3] != frame3Partial[i+3] {
				dirtyOutside++
				if x < minX {
					minX = x
				}
				if y < minY {
					minY = y
				}
				if x > maxX {
					maxX = x
				}
				if y > maxY {
					maxY = y
				}
			}
		}
	}
	if dirtyOutside != 0 {
		t.Fatalf("%d pixels changed OUTSIDE the transform tick's damage union, bounding box (%d,%d)-(%d,%d)", dirtyOutside, minX, minY, maxX, maxY)
	}
}

// TestR53DamageRegionCapFallsBackToFull: more than 16 disjoint damage
// regions (zgui's MAX_REGIONS cap) redraw fully - the full-present
// counter and the 1000-per-mille damage fraction record it.
func TestR53DamageRegionCapFallsBackToFull(t *testing.T) {
	const w, h = 320, 240
	surface := r16Surface(t, "rendererspec r53 region cap", 320, 240)
	scene := r16Scene(t)
	r52Quad(t, scene, 0, 0, 320, 240, 0.58, 0.9, 1)
	// Prime the retained target (a full first draw).
	r53DrawDamage(t, surface, scene, nil, true, w, h)
	stats, err := surface.DebugStats()
	if err != nil {
		t.Fatalf("DebugStats: %v", err)
	}
	fullBefore := stats.FullPresentCount
	partialBefore := stats.PartialPresentCount

	// 20 disjoint 10x10 rects spread across the window: the merge
	// cannot bring them under the 16-region cap, so the entry redraws
	// fully (a FULL present, no partial).
	rects := make([]native.DamageRect, 0, 20)
	for i := 0; i < 20; i++ {
		rects = append(rects, r53Rect(8+(i%5)*64, 8+(i/5)*48, 10, 10))
	}
	pixels := r53DrawDamage(t, surface, scene, rects, false, w, h)
	stats, err = surface.DebugStats()
	if err != nil {
		t.Fatalf("DebugStats: %v", err)
	}
	if stats.FullPresentCount != fullBefore+1 {
		t.Fatalf("the >16-region damage presented full %d times, want 1 (the cap fallback)", stats.FullPresentCount-fullBefore)
	}
	if stats.PartialPresentCount != partialBefore {
		t.Fatalf("the >16-region damage presented partial %d times, want 0", stats.PartialPresentCount-partialBefore)
	}
	if stats.DamageFraction != 1000 {
		t.Fatalf("the damage fraction after the capped fallback = %d, want 1000 (full)", stats.DamageFraction)
	}
	// The frame is still the correct full content.
	r, g, b, a := pixelAt(pixels, w, 160, 120)
	if r < 200 || g < 130 || a != 255 {
		t.Fatalf("the fallback frame's pixel (160,120) = (%d,%d,%d,%d), want the background", r, g, b, a)
	}
}

// TestR53ForceFullPresent: the flags' force-full bit presents full
// without a partial attempt.
func TestR53ForceFullPresent(t *testing.T) {
	const w, h = 320, 240
	surface := r16Surface(t, "rendererspec r53 force full", 320, 240)
	scene := r16Scene(t)
	r52Quad(t, scene, 0, 0, 320, 240, 0.58, 0.9, 1)
	r53DrawDamage(t, surface, scene, nil, true, w, h)
	stats, err := surface.DebugStats()
	if err != nil {
		t.Fatalf("DebugStats: %v", err)
	}
	fullBefore := stats.FullPresentCount
	partialBefore := stats.PartialPresentCount
	// A partial-sized damage with forceFull: the full route.
	r53DrawDamage(t, surface, scene, []native.DamageRect{r53Rect(10, 10, 40, 40)}, true, w, h)
	stats, err = surface.DebugStats()
	if err != nil {
		t.Fatalf("DebugStats: %v", err)
	}
	if stats.FullPresentCount != fullBefore+1 {
		t.Fatalf("the forced-full present count = %d, want +1", stats.FullPresentCount-fullBefore)
	}
	if stats.PartialPresentCount != partialBefore {
		t.Fatalf("the forced-full draw presented partial %d times, want 0", stats.PartialPresentCount-partialBefore)
	}
}

// TestR53Present1InBothSurfaceModes: a partial damage draw on a DComp
// (mode 0) and an hwnd (mode 1) surface - Present1 must be exercised
// in both (the partial count increments) or the full-present fallback
// must fire (the full count increments instead; in DComp mode the
// one-shot readback verification decides, the design's Q5 answer).
func TestR53Present1InBothSurfaceModes(t *testing.T) {
	const w, h = 320, 240
	for _, tc := range []struct {
		mode native.SurfaceMode
		name string
	}{
		{native.SurfaceModeDCompPremultiplied, "dcomp"},
		{native.SurfaceModeHwndAlphaIgnore, "hwnd"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			surface := r53SurfaceMode(t, "rendererspec r53 present1 "+tc.name, 320, 240, tc.mode)
			if surface.Mode() != tc.mode {
				t.Fatalf("surface mode = %v, want %v", surface.Mode(), tc.mode)
			}
			scene := r16Scene(t)
			r52Quad(t, scene, 0, 0, 320, 240, 0.58, 0.9, 1)
			r52Quad(t, scene, 40, 30, 100, 60, 0, 0.5, 1)
			// Prime: the first draw is full (the retained target is
			// fresh).
			r53DrawDamage(t, surface, scene, nil, true, w, h)
			stats, err := surface.DebugStats()
			if err != nil {
				t.Fatalf("DebugStats: %v", err)
			}
			fullBefore := stats.FullPresentCount
			partialBefore := stats.PartialPresentCount
			// The partial damage draw: Present1 with the dirty rect (or
			// the documented fallback).
			r53DrawDamage(t, surface, scene, []native.DamageRect{r53Rect(40, 30, 100, 60)}, false, w, h)
			stats, err = surface.DebugStats()
			if err != nil {
				t.Fatalf("DebugStats: %v", err)
			}
			partial := stats.PartialPresentCount - partialBefore
			full := stats.FullPresentCount - fullBefore
			if partial == 0 && full == 0 {
				t.Fatalf("the partial damage draw neither presented partial nor fell back (partial %d, full %d)", partial, full)
			}
			if partial > 1 || full > 1 {
				t.Fatalf("the partial damage draw presented too often (partial %d, full %d)", partial, full)
			}
			// hwnd mode is Chromium's own software path: Present1 must
			// be accepted there (no fallback expected).
			if tc.mode == native.SurfaceModeHwndAlphaIgnore && partial != 1 {
				t.Fatalf("the hwnd surface's Present1 was not accepted (partial %d, full %d)", partial, full)
			}
			outcome := "fell back to full"
			if partial == 1 {
				outcome = "accepted"
			}
			t.Logf("%s mode: partial=%d full=%d (Present1 %s)", tc.name, partial, full, outcome)
			// The damage fraction reflects the 100x60 rect over the
			// 320x240 window (~78 per-mille).
			if stats.DamageFraction < 60 || stats.DamageFraction > 100 {
				t.Fatalf("the damage fraction = %d, want ~78 (the 100x60 rect over 320x240)", stats.DamageFraction)
			}
		})
	}
}

// TestR53BlurDamageExpansionBounds: damage that intersects a blur
// group's region expands to the group's whole dilated region (3σ) -
// observable through the damage fraction (far above the raw rect's)
// and through the redraw's bit-exactness at the group's far edge.
func TestR53BlurDamageExpansionBounds(t *testing.T) {
	const w, h = 320, 240
	surface := r16Surface(t, "rendererspec r53 blur expansion", 320, 240)
	scene := r16Scene(t)
	r52Quad(t, scene, 0, 0, 320, 240, 0, 1, 1)
	// The blur group at (200,60,100,100), radius 12: the dilated region
	// (200-36, 60-36, 100+72, 100+72) = (164,24,172,172).
	r53BlurGroup(t, scene, 200, 60, 100, 100, 12)
	r53DrawDamage(t, surface, scene, nil, true, w, h)

	// A small damage rect INSIDE the group's bounds (nowhere near the
	// group's dilated edge): the expansion must still cover the whole
	// dilated region - the group's blur mixes the group's entire
	// content, so repainting part of it would show a seam.
	rect := r53Rect(230, 90, 20, 20)
	r53DrawDamage(t, surface, scene, []native.DamageRect{rect}, false, w, h)
	stats, err := surface.DebugStats()
	if err != nil {
		t.Fatalf("DebugStats: %v", err)
	}
	// The raw rect is 400px; the expanded region is 172*172 = 29584px
	// over the 76800px window = ~385 per-mille.
	// The damage fraction reflects the expanded region CLAMPED to the
	// window: 172x172 at x 164 (clamped to 320) = 156x172 = 26832px
	// over 76800 = ~349 per-mille - far above the raw rect's 5.
	if stats.DamageFraction < 340 {
		t.Fatalf("the damage fraction = %d, want >= 350 (the blur group's dilated region, not the raw 400px rect = 5)", stats.DamageFraction)
	}
	// The far edge of the dilated region was repainted correctly: the
	// damaged redraw equals a full redraw (the group straddles the
	// damage edge by construction - the raw rect is deep inside it).
	full := r53DrawDamage(t, surface, scene, nil, true, w, h)
	// (The full redraw just ran; the comparison of the partial frame
	// against it happened inside the draw helper's retained readback -
	// re-draw the partial and compare.)
	partial := r53DrawDamage(t, surface, scene, []native.DamageRect{rect}, false, w, h)
	for i := range partial {
		if partial[i] != full[i] {
			t.Fatalf("the blur-group damaged redraw's byte %d differs from the full redraw (%d vs %d)", i, partial[i], full[i])
		}
	}
}

// TestR53StateBindCounterDropsWithPixelsUnchanged: a scene of many
// same-kind batches (the smoothing split) re-renders with far fewer
// state binds than the batch count would cost without the tracker, and
// the pixels are byte-identical across the frames.
func TestR53StateBindCounterDropsWithPixelsUnchanged(t *testing.T) {
	const w, h = 320, 240
	surface := r16Surface(t, "rendererspec r53 state binds", 320, 240)
	scene := r16Scene(t)
	fullMask := gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 4096, Height: 4096}}
	// 16 quads alternating corner smoothing: the plan splits the runs,
	// so the walk draws 16 BATCH commands of the same pipeline family
	// (the smoothed/plain variants).
	for i := 0; i < 16; i++ {
		quad := gpui.Quad{
			Bounds:      gpui.Bounds{Origin: gpui.Point{X: float32(8 + i*18), Y: 20}, Size: gpui.Size{Width: 16, Height: 200}},
			ContentMask: fullMask,
			Background:  gpui.SolidBackground(gpui.Hsla{H: float32(i) / 32, S: 1, L: 0.5, A: 1}),
		}
		if i%2 == 1 {
			quad.CornerSmoothing = 0.5
			quad.CornerRadii = gpui.Corners{TopRight: 8, BottomRight: 8, TopLeft: 8, BottomLeft: 8}
		}
		if err := scene.InsertQuad(quad); err != nil {
			t.Fatalf("InsertQuad: %v", err)
		}
	}
	first := r52Render(t, surface, scene, w, h)
	stats, err := surface.DebugStats()
	if err != nil {
		t.Fatalf("DebugStats: %v", err)
	}
	before := stats.StateBindCount
	second := r52Render(t, surface, scene, w, h)
	stats, err = surface.DebugStats()
	if err != nil {
		t.Fatalf("DebugStats: %v", err)
	}
	delta := stats.StateBindCount - before
	// Without the tracker, each of the 16 batch draws rebinds its whole
	// state (topology, viewport, VS, PS, the cbuffer pair, the draw
	// constants, the blend state) - 16 * 7 = 112 binds. With it, the
	// consecutive same-family batches rebind only the changed VS/PS
	// pair (~2 each): the frame stays far below the batch count's
	// untracked cost.
	if delta >= 112 {
		t.Fatalf("the state-bind count for 16 same-family batches = %d, want the tracker's savings (< 112 untracked)", delta)
	}
	if delta == 0 {
		t.Fatalf("the state-bind counter did not advance (nothing drew?)")
	}
	// The pixels are unchanged by the tracking (byte-identical frames).
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("the re-render's byte %d changed under the tracker: %d vs %d", i, first[i], second[i])
		}
	}
}
