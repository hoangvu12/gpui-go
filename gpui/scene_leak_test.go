package gpui_test

import (
	"errors"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// perfFailingRoot renders the cached div scene, or panics while fail is
// set (an abandoned frame build).
type perfFailingRoot struct {
	inner gpui.View
	fail  bool
}

func (r *perfFailingRoot) Render(w *gpui.Window, cx *gpui.Context[perfFailingRoot]) gpui.AnyElement {
	if r.fail {
		panic("abandoned frame (gpui-perf ticket 07 test)")
	}
	return gpui.Div().SizeFull().Child(r.inner).IntoElement()
}

// TestLiveScenesStayFlatAcrossFrames (gpui-perf ticket 07): every drawn
// frame creates a native scene; the replaced one is freed after the next
// frame's cached views replayed from it, an abandoned frame's scene is
// freed, and the last one is freed with the window. The live count stays
// at one per open window however many frames are drawn.
func TestLiveScenesStayFlatAcrossFrames(t *testing.T) {
	before := gpui.LiveSceneCount()
	p := openPerfCachedDivs(true, perfChangePartial)(t)
	if got := gpui.LiveSceneCount() - before; got != 1 {
		t.Fatalf("after the first frame %d scenes are live, want 1", got)
	}
	for i := 0; i < 60; i++ {
		p.changedFrame(t) // cached panes replay from the previous scene
	}
	for i := 0; i < 20; i++ {
		p.refreshFrame(t)
	}
	if got := gpui.LiveSceneCount() - before; got != 1 {
		t.Fatalf("after 80 frames %d scenes are live, want 1", got)
	}
	// The published scene stays readable until the next frame replaces it;
	// the replaced one reports ErrSceneClosed.
	previous := gpui.LastDrawnScene(p.window)
	if _, err := previous.Quads(); err != nil {
		t.Fatalf("the last drawn scene is not readable: %v", err)
	}
	p.changedFrame(t)
	if _, err := previous.Quads(); !errors.Is(err, gpui.ErrSceneClosed) {
		t.Fatalf("a replaced scene reads %v, want ErrSceneClosed", err)
	}
	if quads, _ := p.sceneCounts(t); quads < perfPanes*perfDivsPerPane {
		t.Fatalf("the cached frame after disposal painted %d quads", quads)
	}

	// An abandoned frame: its scene is freed, the published one kept.
	ta := gpui.NewTestApp()
	window := gpui.NewTestWindow(ta.App(), gpui.Size{Width: 800, Height: 600})
	t.Cleanup(func() {
		gpui.CloseTestWindow(window)
		ta.App().Shutdown()
	})
	var root gpui.Entity[perfFailingRoot]
	ta.Update(func(app *gpui.App) {
		pane := gpui.NewEntity(app, window.Scope(), func(*perfDivPane, *gpui.Context[perfDivPane]) {})
		root = gpui.NewEntity(app, window.Scope(), func(r *perfFailingRoot, _ *gpui.Context[perfFailingRoot]) {
			r.inner = gpui.ViewOf[perfDivPane](pane)
		})
		window.SetRootView(gpui.ViewOf[perfFailingRoot](root))
	})
	if _, err := gpui.DrawWindowFrame(window); err != nil {
		t.Fatal(err)
	}
	withWindow := gpui.LiveSceneCount()
	published := gpui.LastDrawnScene(window)
	ta.Update(func(app *gpui.App) {
		root.Update(app, func(r *perfFailingRoot, _ *gpui.Context[perfFailingRoot]) { r.fail = true })
	})
	func() {
		defer func() { _ = recover() }()
		_, _ = gpui.DrawWindowFrame(window)
	}()
	if got := gpui.LiveSceneCount(); got != withWindow {
		t.Fatalf("after an abandoned frame %d scenes are live, want %d", got, withWindow)
	}
	if gpui.LastDrawnScene(window) != published {
		t.Fatal("the abandoned frame replaced the published scene")
	}
	if _, err := published.Quads(); err != nil {
		t.Fatalf("the published scene was freed by the abandoned frame: %v", err)
	}
	ta.Update(func(app *gpui.App) {
		root.Update(app, func(r *perfFailingRoot, _ *gpui.Context[perfFailingRoot]) { r.fail = false })
	})
	if _, err := gpui.DrawWindowFrame(window); err != nil {
		t.Fatal(err)
	}
	if got := gpui.LiveSceneCount(); got != withWindow {
		t.Fatalf("after recovering %d scenes are live, want %d", got, withWindow)
	}

	// Closing the window frees its last scene.
	gpui.CloseTestWindow(window)
	if got := gpui.LiveSceneCount(); got != withWindow-1 {
		t.Fatalf("after closing the window %d scenes are live, want %d", got, withWindow-1)
	}
}
