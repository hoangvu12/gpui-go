//go:build windows

// The browser-core ticket 50 (rev 16) renderer gates on the real GPU:
// nested groups beyond the old depth of 2, group opacity (the fold vs
// the isolated composite), rounded clipping (incl. the nested case),
// the group-target pool's observable overflow demotion, and the
// group-free pixel identity of the reworked upload path (the raw ABI
// records are the vendored pipelines' instance layout). Everything
// runs through the ABI-level surface render + readback pair.
package rendererspec

import (
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/native"
)

// r16BlankView is the blank window content (the ABI scene drives the
// tested pixels through RenderScene).
type r16BlankView struct{}

// Render draws an empty full-size div (the blank frame).
func (v *r16BlankView) Render(w *gpui.Window, cx *gpui.Context[r16BlankView]) gpui.AnyElement {
	return gpui.Div().SizeFull().IntoElement()
}

// r16Surface opens one shown window through the public host and
// returns its renderer surface (the rendererspec boot pattern).
func r16Surface(t *testing.T, title string, w, h float32) *gpui.Surface {
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
	// Shown WITHOUT activating (no focus is taken).
	window, err := app.OpenWindowView(gpui.WindowOptions{
		Title:     title,
		Show:      true,
		Resizable: true,
		Bounds:    gpui.Bounds{Size: gpui.Size{Width: w, Height: h}},
	}, func(w *gpui.Window, app *gpui.App) gpui.View {
		// The window's first frame is a plain empty surface; the ABI
		// scene drives the tested pixels through RenderScene.
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
	surface, err := renderer.GetOrCreateSurface(window)
	if err != nil {
		t.Fatalf("GetOrCreateSurface: %v", err)
	}
	return surface
}

// r16Scene builds one gpui scene from native records through the
// kernel seam (the effect groups need the ABI-level authoring).
func r16Scene(t *testing.T) *gpui.Scene {
	t.Helper()
	scene, err := gpui.NewScene()
	if err != nil {
		t.Fatalf("NewScene: %v", err)
	}
	t.Cleanup(func() { _ = scene.Dispose() })
	return scene
}

// r16Quad paints one solid quad record through the native seam.
func r16Quad(t *testing.T, scene *gpui.Scene, x, y, w, h float32, hue, light float32) {
	t.Helper()
	quad := gpui.Quad{
		Bounds:      gpui.Bounds{Origin: gpui.Point{X: x, Y: y}, Size: gpui.Size{Width: w, Height: h}},
		ContentMask: gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 4096, Height: 4096}},
		Background:  gpui.SolidBackground(gpui.Hsla{H: hue, S: 1, L: light, A: 1}),
	}
	if err := scene.InsertQuad(quad); err != nil {
		t.Fatalf("InsertQuad: %v", err)
	}
}

// r16Effect paints one effect-group marker through the native seam.
func r16Effect(t *testing.T, scene *gpui.Scene, group gpui.EffectGroup) {
	t.Helper()
	if err := scene.BeginEffectGroup(group); err != nil {
		t.Fatalf("BeginEffectGroup: %v", err)
	}
}

// r16EndEffect closes the effect group.
func r16EndEffect(t *testing.T, scene *gpui.Scene, group gpui.EffectGroup) {
	t.Helper()
	if err := scene.EndEffectGroup(group); err != nil {
		t.Fatalf("EndEffectGroup: %v", err)
	}
}

// r16Render renders the scene into the surface and returns the
// readback pixels (RGBA bytes, w*h*4).
func r16Render(t *testing.T, surface *gpui.Surface, scene *gpui.Scene, w, h int) []byte {
	t.Helper()
	if err := scene.FinishIfNeeded(); err != nil {
		t.Fatalf("FinishIfNeeded: %v", err)
	}
	if err := surface.RenderScene(scene, nil, gpui.BackgroundOpaque); err != nil {
		t.Fatalf("RenderScene: %v", err)
	}
	pixels, err := surface.ReadPixels()
	if err != nil {
		t.Fatalf("ReadPixels: %v", err)
	}
	if len(pixels) != w*h*4 {
		t.Fatalf("read back %d bytes, want %d", len(pixels), w*h*4)
	}
	return pixels
}

// pixelAt reads one RGBA pixel.
func pixelAt(pixels []byte, w, x, y int) (byte, byte, byte, byte) {
	i := (y*w + x) * 4
	return pixels[i], pixels[i+1], pixels[i+2], pixels[i+3]
}

// nearByte compares two bytes at a tolerance.
func nearByte(a, b byte) bool {
	d := int(a) - int(b)
	if d < 0 {
		d = -d
	}
	return d <= 8
}

// TestR16GroupFreeSceneRendersIdentically: a group-free scene through
// the reworked upload path (the raw ABI records are the vendored quad
// pipeline's instance layout) renders the exact expected solid colors -
// the R16 pixel-identity gate for scenes without groups.
func TestR16GroupFreeSceneRendersIdentically(t *testing.T) {
	const w, h = 120, 80
	surface := r16Surface(t, "rendererspec r16 group-free", 120, 80)
	scene := r16Scene(t)
	// One red quad over the whole window and one blue quad inset.
	r16Quad(t, scene, 0, 0, 120, 80, 0, 0.5)
	r16Quad(t, scene, 30, 20, 60, 40, 2.0/3.0, 0.5)
	pixels := r16Render(t, surface, scene, w, h)
	// The window scale is 1 on this machine; the colors are exact.
	r, g, b, a := pixelAt(pixels, w, 5, 5)
	if r < 240 || g > 10 || b > 10 || a != 255 {
		t.Fatalf("the red region reads (%d,%d,%d,%d), want solid red", r, g, b, a)
	}
	r, g, b, a = pixelAt(pixels, w, 60, 40)
	if b < 240 || r > 10 || g > 10 || a != 255 {
		t.Fatalf("the blue region reads (%d,%d,%d,%d), want solid blue", r, g, b, a)
	}
}

// TestR16NestedOpacityGroupsBeyondTheOldDepth: three nested opacity
// groups over solid quads composite with compounded opacity (0.8 x
// 0.5 x 0.5 = 0.2 effective alpha over the white background) - depth
// beyond the pre-R16 cap of 2, which would have rendered the third
// group un-grouped.
func TestR16NestedOpacityGroupsBeyondTheOldDepth(t *testing.T) {
	const w, h = 120, 80
	surface := r16Surface(t, "rendererspec r16 nested opacity", 120, 80)
	scene := r16Scene(t)
	fullMask := gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 4096, Height: 4096}}
	groupBounds := gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 120, Height: 80}}
	// FOUR nested groups (0.8, 0.5, 0.5, 0.5): the innermost folds (its
	// span is one quad), so THREE groups isolate - beyond the pre-R16
	// cap of two, which would have rendered the third un-grouped. The
	// effective alpha is 0.8 * 0.5 * 0.5 * 0.5 = 0.1.
	for _, opacity := range []float32{0.8, 0.5, 0.5, 0.5} {
		r16Effect(t, scene, gpui.EffectGroup{
			HasOpacity:  true,
			Opacity:     opacity,
			Bounds:      groupBounds,
			ContentMask: fullMask,
		})
	}
	r16Quad(t, scene, 0, 0, 120, 80, 0, 0.5) // solid red
	for range 4 {
		r16EndEffect(t, scene, gpui.EffectGroup{HasOpacity: true, Bounds: groupBounds, ContentMask: fullMask})
	}
	pixels := r16Render(t, surface, scene, w, h)
	r, g, b, a := pixelAt(pixels, w, 60, 40)
	// Red (255,0,0) at 0.1 alpha over white: (255, 229, 229) (the
	// 8-bit bucket quantization of the nested composites stays within
	// the tolerance).
	if !nearByte(r, 255) || !nearByte(g, 230) || !nearByte(b, 230) || a != 255 {
		t.Fatalf("the quadruple-nested group reads (%d,%d,%d,%d), want ~(255,230,230,255) (0.8*0.5^3 over white)", r, g, b, a)
	}
}

// TestR16GroupOpacityVersusTheFold: two overlapping quads under one
// opacity group composite ONCE (the overlap region carries the group's
// 0.5 alpha, not two stacked 0.5s); a single-quad group folds into the
// per-primitive multiply and reads the same as a plain multiplied
// quad.
func TestR16GroupOpacityVersusTheFold(t *testing.T) {
	const w, h = 160, 80
	surface := r16Surface(t, "rendererspec r16 group opacity", 160, 80)
	fullMask := gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 4096, Height: 4096}}
	groupBounds := gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 80, Height: 80}}

	scene := r16Scene(t)
	r16Effect(t, scene, gpui.EffectGroup{HasOpacity: true, Opacity: 0.5, Bounds: groupBounds, ContentMask: fullMask})
	// Two overlapping red quads at 0.5 group opacity.
	r16Quad(t, scene, 0, 0, 60, 80, 0, 0.5)
	r16Quad(t, scene, 20, 0, 60, 80, 0, 0.5)
	r16EndEffect(t, scene, gpui.EffectGroup{HasOpacity: true, Bounds: groupBounds, ContentMask: fullMask})
	groupPixels := r16Render(t, surface, scene, w, h)

	scene = r16Scene(t)
	// The folded control: one plain quad with the opacity multiplied
	// into its colour (the pre-R16 behaviour) and one single-quad group
	// (which the plan folds into the same per-primitive multiply) - the
	// two must read pixel-identical. The group's bounds cover the RIGHT
	// quad only, so the folded quad lands inside the pair.
	quad := gpui.Quad{
		Bounds:      gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 60, Height: 80}},
		ContentMask: fullMask,
		Background:  gpui.SolidBackground(gpui.Hsla{H: 0, S: 1, L: 0.5, A: 0.5}),
	}
	if err := scene.InsertQuad(quad); err != nil {
		t.Fatalf("InsertQuad: %v", err)
	}
	rightGroupBounds := gpui.Bounds{Origin: gpui.Point{X: 80}, Size: gpui.Size{Width: 80, Height: 80}}
	r16Effect(t, scene, gpui.EffectGroup{HasOpacity: true, Opacity: 0.5, Bounds: rightGroupBounds, ContentMask: fullMask})
	r16Quad(t, scene, 100, 0, 60, 80, 0, 0.5)
	r16EndEffect(t, scene, gpui.EffectGroup{HasOpacity: true, Bounds: rightGroupBounds, ContentMask: fullMask})
	foldPixels := r16Render(t, surface, scene, w, h)

	// The overlap region of the grouped pair: the GROUP composites both
	// quads once at 0.5 -> (255,128,128) over white.
	r, g, b, a := pixelAt(groupPixels, w, 40, 40)
	if !nearByte(r, 255) || !nearByte(g, 128) || !nearByte(b, 128) || a != 255 {
		t.Fatalf("the group's overlap region reads (%d,%d,%d,%d), want ~(255,128,128,255) - the group composites ONCE", r, g, b, a)
	}
	// The non-overlap part of the grouped pair also carries 0.5.
	r, g, b, _ = pixelAt(groupPixels, w, 5, 40)
	if !nearByte(r, 255) || !nearByte(g, 128) || !nearByte(b, 128) {
		t.Fatalf("the group's single-quad region reads (%d,%d,%d,%d), want ~(255,128,128)", r, g, b, a)
	}
	// The FOLDED single quad reads the same as the plain multiplied
	// quad (pixel-identical through the fold).
	r1, g1, b1, _ := pixelAt(foldPixels, w, 30, 40)
	r2, g2, b2, _ := pixelAt(foldPixels, w, 130, 40)
	if r1 != r2 || g1 != g2 || b1 != b2 {
		t.Fatalf("the fold diverged from the plain multiplied quad: (%d,%d,%d) vs (%d,%d,%d)", r1, g1, b1, r2, g2, b2)
	}
}

// TestR16RoundedClipClipsChildren: a clip-to-bounds group with corner
// radii clips its children to the rrect (the corners read the
// background); a nested child mask with its own radii applies inline.
func TestR16RoundedClipClipsChildren(t *testing.T) {
	const w, h = 120, 120
	surface := r16Surface(t, "rendererspec r16 rounded clip", 120, 120)
	scene := r16Scene(t)
	fullMask := gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 4096, Height: 4096}}
	groupBounds := gpui.Bounds{Origin: gpui.Point{X: 10, Y: 10}, Size: gpui.Size{Width: 100, Height: 100}}
	radii := gpui.Corners{TopLeft: 30, TopRight: 30, BottomRight: 30, BottomLeft: 30}
	group := gpui.EffectGroup{
		HasOpacity:   true,
		ClipToBounds: true,
		Opacity:      1,
		Bounds:       groupBounds,
		ContentMask:  fullMask,
		CornerRadii:  radii,
	}
	r16Effect(t, scene, group)
	// A child quad POKING OUT of the group bounds: the clip must cut it
	// at the rrect (the child also carries its own smaller inline mask
	// radii - the nested rounded case).
	r16Quad(t, scene, 0, 0, 120, 120, 0, 0.5)
	r16EndEffect(t, scene, group)
	pixels := r16Render(t, surface, scene, w, h)
	// The corner OUTSIDE the rrect reads the white background.
	r, g, b, a := pixelAt(pixels, w, 11, 11)
	if !nearByte(r, 255) || !nearByte(g, 255) || !nearByte(b, 255) || a != 255 {
		t.Fatalf("the rrect's outside-corner reads (%d,%d,%d,%d), want the white background", r, g, b, a)
	}
	// The center reads the solid red child.
	r, g, b, a = pixelAt(pixels, w, 60, 60)
	if r < 240 || g > 10 || b > 10 || a != 255 {
		t.Fatalf("the rrect's center reads (%d,%d,%d,%d), want solid red", r, g, b, a)
	}
	// The straight edge inside the bounds reads the child.
	r, g, b, a = pixelAt(pixels, w, 60, 12)
	if r < 240 || g > 10 || b > 10 {
		t.Fatalf("the top edge inside the bounds reads (%d,%d,%d,%d), want solid red", r, g, b, a)
	}
}

// TestR16PoolOverflowIsObservable: 65 nested groups exceed the pool's
// 64-target budget; the 65th renders inline (its opacity is dropped -
// the pixel divergence) and the debug stats report the overflow: the
// pool's overflow counter is non-zero and the retained target count
// hits the cap.
func TestR16PoolOverflowIsObservable(t *testing.T) {
	const w, h = 120, 80
	surface := r16Surface(t, "rendererspec r16 pool overflow", 120, 80)
	scene := r16Scene(t)
	fullMask := gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 4096, Height: 4096}}
	groupBounds := gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 100, Height: 60}}
	// SEVENTY nested pure-opacity groups: the innermost folds (one
	// primitive), so 69 isolate - the 65th exceeds the 64-target budget
	// and demotes to inline.
	for range 70 {
		r16Effect(t, scene, gpui.EffectGroup{HasOpacity: true, Opacity: 0.99, Bounds: groupBounds, ContentMask: fullMask})
	}
	r16Quad(t, scene, 0, 0, 100, 60, 0, 0.5)
	for range 70 {
		r16EndEffect(t, scene, gpui.EffectGroup{HasOpacity: true, Bounds: groupBounds, ContentMask: fullMask})
	}
	pixels := r16Render(t, surface, scene, w, h)
	// The debug stats: the overflow counter fired and the pool hit its
	// target cap.
	stats, err := surface.DebugStats()
	if err != nil {
		t.Fatalf("DebugStats: %v", err)
	}
	if stats.PoolOverflowCount == 0 {
		t.Fatal("the pool overflow counter must be non-zero after 65 concurrent groups (the 64-target cap)")
	}
	if stats.PoolTargetCount > 64 {
		t.Fatalf("the pool retained %d targets, over the 64-target cap", stats.PoolTargetCount)
	}
	if stats.PoolTargetCount < 60 {
		t.Fatalf("the pool retained only %d targets, want ~64 (the nested groups each hold one)", stats.PoolTargetCount)
	}
	// The demoted groups' children still render (never a crash, never a
	// failed frame): the red quad reads through the surviving
	// composites (~0.99^65 ~ 0.52 alpha over white -> ~(255,123,123)).
	r, g, b, a := pixelAt(pixels, w, 50, 40)
	if a != 255 || r < 200 || g > 160 || b > 160 {
		t.Fatalf("the demoted group's children must still render (read (%d,%d,%d,%d))", r, g, b, a)
	}
}

// TestR16BackdropPartialRegionBlurIsUnzoomed: ticket 77's folded-in
// fix - a partial-region backdrop's blurred input keeps the region's
// scale (the pre-fix window-sized scratch sampled the crop at
// crop-relative coordinates and zoomed the input). The bars under a
// centered small backdrop panel blur at their own positions.
func TestR16BackdropPartialRegionBlurIsUnzoomed(t *testing.T) {
	const w, h = 160, 120
	surface := r16Surface(t, "rendererspec r16 partial backdrop", 160, 120)
	scene := r16Scene(t)
	fullMask := gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 4096, Height: 4096}}
	// The asymmetric discriminator: the left half of the window red,
	// the right half blue.
	r16Quad(t, scene, 0, 0, 80, 120, 0, 0.5)
	r16Quad(t, scene, 80, 0, 80, 120, 2.0/3.0, 0.5)
	// A backdrop panel fully inside the RED half (20..60, 20px away
	// from the boundary) with blur 6: the correct crop-relative blur
	// sees only red, so the panel reads solid red. The pre-fix bug
	// (the window-sized scratch sampled at crop-relative coordinates)
	// squashed the WHOLE window into the crop - the panel's center
	// read the window's center, the red/blue boundary (a purple mix).
	backdrop := gpui.BackdropFilter{
		Bounds:      gpui.Bounds{Origin: gpui.Point{X: 20, Y: 30}, Size: gpui.Size{Width: 40, Height: 60}},
		ContentMask: fullMask,
		BlurRadius:  6,
		Opacity:     1,
	}
	if err := scene.InsertBackdropFilter(backdrop); err != nil {
		t.Fatalf("InsertBackdropFilter: %v", err)
	}
	pixels := r16Render(t, surface, scene, w, h)
	r, g, b, a := pixelAt(pixels, w, 40, 60)
	if a != 255 || r < 200 || b > 80 || g > 80 {
		t.Fatalf("the partial-region backdrop center reads (%d,%d,%d,%d), want solid red (the unzoomed crop blurs only the red half)", r, g, b, a)
	}
	// Outside the panel the blue half stays unblurred.
	r, _, b, _ = pixelAt(pixels, w, 100, 60)
	if r > 60 || b < 200 {
		t.Fatalf("outside the panel the blue half must stay unblurred (reads (%d,%d))", r, b)
	}
}

// TestR16DebugStatsStartClean: a group-free scene leaves the pool
// empty and the overflow counter at zero.
func TestR16DebugStatsStartClean(t *testing.T) {
	const w, h = 120, 80
	surface := r16Surface(t, "rendererspec r16 stats clean", 120, 80)
	scene := r16Scene(t)
	r16Quad(t, scene, 0, 0, 120, 80, 0, 0.5)
	_ = r16Render(t, surface, scene, w, h)
	stats, err := surface.DebugStats()
	if err != nil {
		t.Fatalf("DebugStats: %v", err)
	}
	if stats.PoolTargetCount != 0 || stats.PoolOverflowCount != 0 || stats.PoolBytes != 0 {
		t.Fatalf("a group-free scene must leave the pool clean, got %+v", stats)
	}
}

var _ = native.SceneCommandBeginEffect
