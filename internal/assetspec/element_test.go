package assetspec

import (
	"fmt"
	"testing"

	"gpui-go/gpui"
)

// This file drives the asset surfaces from REAL elements through the
// deterministic test window's full phase pipeline
// (request_layout/prepaint/paint), matching the pinned img element's
// cache-driving half: the three load states during request_layout, the
// completion notification that redraws the current view, the
// loading-delay state machine, the animation frame advance, and the
// two-consumer/window-close flows. The interactive img element itself
// (interactivity, object fit, sprite painting) belongs to the element
// and sprite tickets.

// imageView is the view state rendering one image element.
type imageView struct {
	resource gpui.Resource
	cache    gpui.AnyImageCache
	// useWindowAsset selects the app-level use_asset path (no cache).
	useWindowAsset bool
	states         *[]string
}

// Render implements gpui.Render: one image element.
func (v *imageView) Render(w *gpui.Window, cx *gpui.Context[imageView]) gpui.AnyElement {
	return gpui.CustomElement[imageElementLayout, struct{}](&imageElement{
		resource:       v.resource,
		cache:          v.cache,
		useWindowAsset: v.useWindowAsset,
		states:         v.states,
	})
}

// imageElementLayout is the element's request-layout state: the frame
// index chosen by the playback advance.
type imageElementLayout struct{ frameIndex int }

// imageElement is the image element driving the cache during its
// phases (the img element's request_layout load/advance/fallback
// branches, img.rs lines 298-420).
type imageElement struct {
	resource       gpui.Resource
	cache          gpui.AnyImageCache
	useWindowAsset bool
	states         *[]string
}

// ID implements Element: the element is identified so it retains
// playback state and can request animation frames (the pinned
// global_id conditions).
func (e *imageElement) ID() (gpui.ElementID, bool) {
	return gpui.NameElementID("img"), true
}

// SourceLocation implements Element.
func (e *imageElement) SourceLocation() *gpui.SourceLocation { return nil }

// record appends one observed state.
func (e *imageElement) record(state string) {
	*e.states = append(*e.states, state)
}

// RequestLayout implements Element: the pinned request_layout load
// branch — the cache resolves the resource (or the app-level
// use_asset), the loading branch starts the delay tracker, the error
// branch resolves, and the loaded branch advances the playback.
func (e *imageElement) RequestLayout(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, w *gpui.Window, app *gpui.App) (gpui.LayoutID, imageElementLayout) {
	var layout imageElementLayout
	gpui.WithElementState(w, global, func(state *gpui.ImagePlayback, w *gpui.Window) (imageElementLayout, gpui.ImagePlayback) {
		playback := gpui.ImagePlayback{}
		if state != nil {
			playback = *state
		}
		var result gpui.ImageAssetResult
		var done bool
		if e.cache != nil {
			result, done = e.cache.LoadImage(e.resource, w, app)
		} else {
			result, done = w.UseImageAsset(e.resource, app)
		}
		policy := gpui.ImagePlaybackPolicy{ReduceMotion: app.ReduceMotion(), WindowActive: true}
		switch {
		case !done:
			// The pinned None branch: the loading replacement appears only
			// after LOADING_DELAY, and a first sight starts the timer.
			e.record("loading")
			if playback.ShowLoadingReplacement(app.Now()) {
				e.record("loading-replacement")
			} else {
				view, _ := gpui.CurrentView(w)
				playback.StartLoadingDelay(w, view, app)
			}
		case result.Err != nil:
			// The pinned Some(Err) branch: the fallback renders (recorded
			// here) and the loading tracker resolves.
			e.record("error:" + result.Err.Error())
			playback.Resolved()
		default:
			// The pinned Some(Ok) branch: advance the rational playback
			// and request the next animation frame.
			layout.frameIndex = playback.Advance(result.Image.RenderImage(), app.Now(), policy)
			e.record(fmt.Sprintf("loaded:%d", layout.frameIndex))
			playback.Resolved()
			if gpui.NeedsAnimationFrame(result.Image.RenderImage(), true, policy) {
				gpui.RequestAnimationFrame(w)
			}
		}
		return layout, playback
	})

	style := gpui.DefaultStyle()
	style.Size = gpui.LengthSize{Width: gpui.PxLength(10), Height: gpui.PxLength(10)}
	layoutID, err := gpui.RequestElementLayout(w, style)
	if err != nil {
		panic("imageElement request_layout: " + err.Error())
	}
	return layoutID, layout
}

// Prepaint implements Element.
func (e *imageElement) Prepaint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *imageElementLayout, w *gpui.Window, app *gpui.App) struct{} {
	return struct{}{}
}

// Paint implements Element: the pinned paint branch records the frame
// it would draw (the sprite painting belongs to the sprite tickets).
func (e *imageElement) Paint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *imageElementLayout, prepaint *struct{}, w *gpui.Window, app *gpui.App) {
}

// elementFixture is one window with an image view rendering through a
// cache (or the app-level asset path).
type elementFixture struct {
	assetFixture
	view          gpui.Entity[imageView]
	cache         gpui.Entity[gpui.RetainAllImageCache]
	notifications int
}

// newElementFixture builds the app, the window and the image view. The
// view is retained by the app's root scope (ViewOf outside a draw), so
// it survives window closure — the discard observable. resource names
// the path the element loads; the registry entry is registered under
// path (they differ in the error-path tests).
func newElementFixture(t *testing.T, path, resource string, data []byte, useWindowAsset bool) *elementFixture {
	t.Helper()
	f := &elementFixture{}
	f.assetFixture = *newAssetFixture(t, path, data)
	states := &[]string{}
	var cache gpui.AnyImageCache
	if !useWindowAsset {
		f.cache = gpui.NewRetainAllImageCache(f.app, f.ta.RootScope())
		cache = gpui.AnyImageCacheOf(f.cache)
	}
	f.view = gpui.NewEntity(f.app, f.ta.RootScope(), func(v *imageView, cx *gpui.Context[imageView]) {
		v.resource = gpui.EmbeddedResource(resource)
		v.cache = cache
		v.useWindowAsset = useWindowAsset
		v.states = states
		cx.Observe(cx.Entity(), func(_ *imageView, _ gpui.Entity[imageView], _ *gpui.Context[imageView]) {
			f.notifications++
		})
	})
	f.window.SetRootView(gpui.ViewOf(f.view))
	return f
}

// states returns the recorded element states.
func (f *elementFixture) states() []string {
	f.view.Read(f.app, func(v *imageView, _ *gpui.App) struct{} { return struct{}{} })
	var out []string
	f.view.Read(f.app, func(v *imageView, _ *gpui.App) struct{} {
		out = append(out, *v.states...)
		return struct{}{}
	})
	return out
}

// draw runs one window frame.
func (f *elementFixture) draw(t *testing.T) {
	t.Helper()
	if _, err := gpui.DrawWindowFrame(f.window); err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}
}

// TestImageElementAsyncLoadStates pins the complete observable path in
// a real element (the ticket's question): the first frame renders the
// loading state, the completion notification redraws the view, and the
// second frame renders the decoded image — through the real codec and
// the deterministic scheduler.
func TestImageElementAsyncLoadStates(t *testing.T) {
	f := newElementFixture(t, "img.png", "img.png", pngBytes(t, 4, 2), false)
	atlas := mustAtlas(t, f.app)

	f.draw(t)
	if got := f.states(); len(got) != 1 || got[0] != "loading" {
		t.Fatalf("first frame states = %v, want [loading]", got)
	}

	// The load runs and the completion notifies the view (the pinned
	// on_next_frame + notify) and marks the window's redraw seam.
	f.ta.RunUntilParked()
	if *f.loads != 1 {
		t.Fatalf("asset loads = %d, want 1", *f.loads)
	}
	if f.notifications != 1 {
		t.Fatalf("view notifications = %d, want 1", f.notifications)
	}
	if !f.window.RefreshRequested() {
		t.Fatal("the completion did not mark the window for redraw")
	}
	if got := tileCount(t, atlas); got != 0 {
		t.Fatalf("atlas tiles before the resolving frame = %d, want 0", got)
	}

	// The redraw resolves and retains the decoded frames.
	f.draw(t)
	if got := f.states(); len(got) != 2 || got[1] != "loaded:0" {
		t.Fatalf("second frame states = %v, want [loading loaded:0]", got)
	}
	if got := tileCount(t, atlas); got != 1 {
		t.Fatalf("atlas tiles = %d, want 1", got)
	}
	// A third frame keeps the cached image without a new load.
	f.draw(t)
	if *f.loads != 1 || f.notifications != 1 {
		t.Fatalf("loads = %d, notifications = %d after the cached frame", *f.loads, f.notifications)
	}
	if got := f.states(); len(got) != 3 || got[2] != "loaded:0" {
		t.Fatalf("third frame states = %v, want [loading loaded:0 loaded:0]", got)
	}
}

// TestImageElementErrorState pins the error branch in a real element
// (the pinned fallback path): the missing embedded asset resolves to
// the Asset error and the element renders the error state.
func TestImageElementErrorState(t *testing.T) {
	// The registry holds img.png; the element requests missing.png.
	f := newElementFixture(t, "img.png", "missing.png", pngBytes(t, 2, 2), false)

	f.draw(t)
	if got := f.states(); len(got) != 1 || got[0] != "loading" {
		t.Fatalf("first frame states = %v, want [loading]", got)
	}
	f.ta.RunUntilParked()
	f.draw(t)
	states := f.states()
	if len(states) != 2 || states[1] != "error:asset error: Embedded resource not found: missing.png" {
		t.Fatalf("error frame states = %v", states)
	}
	// The error stays cached: no second load, no loading state.
	f.draw(t)
	if *f.loads != 0 {
		t.Fatalf("asset loads = %d, want 0 (the path never existed)", *f.loads)
	}
	if got := f.states(); len(got) != 3 || got[2] != states[1] {
		t.Fatalf("cached error states = %v", got)
	}
}

// TestImageElementTwoWindowsShareAppAsset pins two consumers sharing
// one app-level asset (App::fetch_asset over use_asset, window.rs
// lines 4013-4033): one load serves both windows; the first requester
// receives the completion notification, the second resolves on its
// next render.
func TestImageElementTwoWindowsShareAppAsset(t *testing.T) {
	f := newElementFixture(t, "img.png", "img.png", pngBytes(t, 2, 2), true)

	// A second window with its own image view over the same resource.
	secondStates := &[]string{}
	secondView := gpui.NewEntity(f.app, f.ta.RootScope(), func(v *imageView, cx *gpui.Context[imageView]) {
		v.resource = gpui.EmbeddedResource("img.png")
		v.useWindowAsset = true
		v.states = secondStates
	})
	secondWindow := gpui.NewTestWindow(f.app, gpui.Size{Width: 100, Height: 100})
	secondWindow.SetRootView(gpui.ViewOf(secondView))

	// Both windows render the loading state.
	f.draw(t)
	if got := f.states(); len(got) != 1 || got[0] != "loading" {
		t.Fatalf("window 1 states = %v, want [loading]", got)
	}
	if _, err := gpui.DrawWindowFrame(secondWindow); err != nil {
		t.Fatalf("DrawWindowFrame(2): %v", err)
	}
	if got := *secondStates; len(got) != 1 || got[0] != "loading" {
		t.Fatalf("window 2 states = %v, want [loading]", got)
	}

	// One load serves both windows.
	f.ta.RunUntilParked()
	if *f.loads != 1 {
		t.Fatalf("asset loads = %d, want 1 (shared across both windows)", *f.loads)
	}
	if f.notifications != 1 {
		t.Fatalf("view notifications = %d, want 1 (only the first requester registers)", f.notifications)
	}

	// The second consumer resolves on its next render without a new
	// load (the pinned now_or_never over the shared task).
	if _, err := gpui.DrawWindowFrame(secondWindow); err != nil {
		t.Fatalf("DrawWindowFrame(2): %v", err)
	}
	if got := *secondStates; len(got) != 2 || got[1] != "loaded:0" {
		t.Fatalf("window 2 resolved states = %v, want [loading loaded:0]", got)
	}
	if *f.loads != 1 {
		t.Fatalf("asset loads after both resolves = %d, want 1", *f.loads)
	}
}

// TestImageElementWindowCloseDiscardsLateResult pins the scope-owned
// completion notification (the ticket04 delivery adaptation): closing
// the window while the load is in flight cancels the notification, so
// the late result never reaches the view — zero notifications, and no
// panic when the worker completes.
func TestImageElementWindowCloseDiscardsLateResult(t *testing.T) {
	f := newElementFixture(t, "img.png", "img.png", pngBytes(t, 2, 2), false)

	f.draw(t)
	if got := f.states(); len(got) != 1 || got[0] != "loading" {
		t.Fatalf("first frame states = %v, want [loading]", got)
	}

	// Close the window before the load task runs.
	gpui.CloseTestWindow(f.window)
	f.ta.RunUntilParked()
	if *f.loads != 1 {
		t.Fatalf("asset loads = %d, want 1 (the worker still completed)", *f.loads)
	}
	if f.notifications != 0 {
		t.Fatalf("view notifications = %d, want 0 (the delivery was discarded)", f.notifications)
	}
}

// TestImageElementAnimatedPlayback pins the animation scheduling in a
// real element over the committed animated WebP: the rational delays
// drive the frame advance (40ms, 100ms, 250ms) with wraparound, under
// the deterministic virtual clock.
func TestImageElementAnimatedPlayback(t *testing.T) {
	f := newElementFixture(t, "anim.webp", "anim.webp", fixture(t, "tiny-anim.webp"), false)

	f.draw(t)
	if got := f.states(); len(got) != 1 || got[0] != "loading" {
		t.Fatalf("first frame states = %v, want [loading]", got)
	}
	f.ta.RunUntilParked()
	f.draw(t)
	if got := f.states(); len(got) != 2 || got[1] != "loaded:0" {
		t.Fatalf("resolved states = %v, want [loading loaded:0]", got)
	}

	// The 40ms first frame delay elapses: frame 1.
	f.ta.AdvanceClock(40)
	f.draw(t)
	if got := f.states(); len(got) != 3 || got[2] != "loaded:1" {
		t.Fatalf("states at 40ms = %v, want loaded:1", got)
	}
	// 100ms more: frame 2.
	f.ta.AdvanceClock(100)
	f.draw(t)
	if got := f.states(); len(got) != 4 || got[3] != "loaded:2" {
		t.Fatalf("states at 140ms = %v, want loaded:2", got)
	}
	// 250ms more: wraparound to frame 0.
	f.ta.AdvanceClock(250)
	f.draw(t)
	if got := f.states(); len(got) != 5 || got[4] != "loaded:0" {
		t.Fatalf("states at 390ms = %v, want loaded:0 (wraparound)", got)
	}

	// The load ran once for all frames.
	if *f.loads != 1 {
		t.Fatalf("asset loads = %d, want 1", *f.loads)
	}
}

// TestImageElementReduceMotionFreezesAnimation pins the reduce-motion
// policy in the element: the frames do not advance while the app
// reduces motion.
func TestImageElementReduceMotionFreezesAnimation(t *testing.T) {
	f := newElementFixture(t, "anim.webp", "anim.webp", fixture(t, "tiny-anim.webp"), false)
	f.app.SetReduceMotion(true)

	f.draw(t)
	f.ta.RunUntilParked()
	f.draw(t)
	if got := f.states(); len(got) != 2 || got[1] != "loaded:0" {
		t.Fatalf("resolved states = %v, want [loading loaded:0]", got)
	}
	f.ta.AdvanceClock(1000)
	f.draw(t)
	if got := f.states(); len(got) != 3 || got[2] != "loaded:0" {
		t.Fatalf("states under reduce motion = %v, want frame 0 frozen", got)
	}
}

// TestImageElementLoadingDelayTimer pins the loading state machinery in
// a real element: a first frame of a still-loading image starts the
// 200ms timer, and advancing the virtual clock to the deadline fires
// the notification. The load also completes during the same clock
// advance (the scheduler runs the queued worker first), so BOTH pinned
// notification sources fire — the awaiter's completion notify and the
// loading-delay timer — and the resolved image wins the next frame,
// exactly the pinned outcome when an asset resolves within the delay.
// The > LOADING_DELAY replacement decision itself is pinned by the
// state-machine tests (TestLoadingDelayStateMachine).
func TestImageElementLoadingDelayTimer(t *testing.T) {
	f := newElementFixture(t, "img.png", "img.png", pngBytes(t, 2, 2), false)

	f.draw(t)
	if got := f.states(); len(got) != 1 || got[0] != "loading" {
		t.Fatalf("first frame states = %v, want [loading]", got)
	}
	if f.notifications != 0 {
		t.Fatalf("view notifications before any completion = %d, want 0", f.notifications)
	}

	// Advancing to the 200ms deadline runs the queued load (the
	// completion notify) and then fires the loading-delay timer (the
	// second notify).
	f.ta.AdvanceClock(200)
	if f.notifications != 2 {
		t.Fatalf("view notifications = %d, want 2 (the load completion and the loading-delay timer)", f.notifications)
	}
	if *f.loads != 1 {
		t.Fatalf("asset loads = %d, want 1", *f.loads)
	}

	// The resolved image wins the next frame; the loading replacement
	// never appears because the load beat the delay.
	f.draw(t)
	if got := f.states(); len(got) != 2 || got[1] != "loaded:0" {
		t.Fatalf("states after the delay = %v, want [loading loaded:0]", got)
	}
}
