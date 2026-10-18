package gpui

import "testing"

// frameLoopView is a root view for the frame-loop tests: it counts
// renders and requests animation frames while animate > 0.
type frameLoopView struct {
	renders int
	animate int
}

func (v *frameLoopView) Render(w *Window, cx *Context[frameLoopView]) AnyElement {
	v.renders++
	if v.animate > 0 {
		v.animate--
		RequestAnimationFrame(w)
	}
	return Div().SizeFull().IntoElement()
}

// frameLoopModel is a non-view entity (never rendered).
type frameLoopModel struct{}

// openFrameLoopTestWindow opens a test window whose root view is a
// frameLoopView, drawn once.
func openFrameLoopTestWindow(t *testing.T, ta *TestApp) (*Window, Entity[frameLoopView]) {
	t.Helper()
	app := ta.App()
	w := NewTestWindow(app, Size{Width: 200, Height: 100})
	var entity Entity[frameLoopView]
	app.Update(func(app *App) {
		entity = NewEntity(app, w.Scope(), func(*frameLoopView, *Context[frameLoopView]) {})
		w.SetRootView(ViewOf(entity))
	})
	if _, err := DrawWindowFrame(w); err != nil {
		t.Fatalf("initial draw: %v", err)
	}
	return w, entity
}

func frameLoopRenders(app *App, e Entity[frameLoopView]) int {
	return e.Read(app, func(v *frameLoopView, _ *App) int { return v.renders })
}

// TestFrameWakerFiresOnFrameDemand ports window.rs:8456-8507
// (test_frame_waker_fires_on_frame_demand): opening wakes the frame
// source, idle windows do not wake on clean frames or non-notifying
// updates, notifying a rendered view wakes it, serving the demand
// returns to idle, and a next-frame callback wakes an idle window.
func TestFrameWakerFiresOnFrameDemand(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	w, entity := openFrameLoopTestWindow(t, ta)

	if w.FrameWakeCount() < 1 {
		t.Fatal("opening a window must wake the frame source for the initial frame")
	}

	// Serve outstanding demand (present the frame drawn at open).
	w.SimulateFrameRequest()

	baseline := w.FrameWakeCount()
	w.SimulateFrameRequest()
	entity.Update(app, func(*frameLoopView, *Context[frameLoopView]) {})
	if got := w.FrameWakeCount(); got != baseline {
		t.Fatalf("clean frames and non-notifying updates woke the frame source (%d -> %d)", baseline, got)
	}

	entity.Update(app, func(_ *frameLoopView, cx *Context[frameLoopView]) { cx.Notify() })
	if w.FrameWakeCount() <= baseline {
		t.Fatal("notifying a view in an idle window must wake the frame source")
	}

	// Serving that demand returns to idle without further wakes.
	w.SimulateFrameRequest()
	baseline = w.FrameWakeCount()
	w.SimulateFrameRequest()
	if got := w.FrameWakeCount(); got != baseline {
		t.Fatalf("serving demand must return the window to idle (%d -> %d)", baseline, got)
	}

	// Next-frame callbacks create demand without dirtying the window.
	app.Update(func(*App) { w.OnNextFrame(func(*Window, *App) {}) })
	if w.FrameWakeCount() <= baseline {
		t.Fatal("scheduling a next-frame callback in an idle window must wake the frame source")
	}
	if w.isFrameDirty() {
		t.Fatal("a next-frame callback must not dirty the window")
	}
}

// TestPendingNextFrameCallbacksAreNotStranded ports window.rs:8514-8543:
// a frame request with pending next-frame callbacks runs them (the port
// has no inactive-window throttle, so the request always serves them).
func TestPendingNextFrameCallbacksAreNotStranded(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	w, _ := openFrameLoopTestWindow(t, ta)
	w.SimulateFrameRequest()

	ran := false
	app.Update(func(*App) { w.OnNextFrame(func(*Window, *App) { ran = true }) })
	baseline := w.FrameWakeCount()
	w.SimulateFrameRequest()
	if !(w.FrameWakeCount() > baseline || ran) {
		t.Fatal("a frame request with pending next-frame callbacks must either run them or re-arm the frame source")
	}
	if !ran {
		t.Fatal("the unthrottled frame request did not run the pending next-frame callback")
	}
}

// TestFrameLoopNotifyRedrawsRenderedView checks the notify → dirty →
// redraw path (App::notify app.rs:2920-2953, invalidate_view
// window.rs:189-214, request_frame window.rs:1805-1819): a notification
// of the rendered root view dirties the window and the next frame
// request redraws it without an explicit DrawWindowFrame; a model that
// was never rendered does not dirty the window.
func TestFrameLoopNotifyRedrawsRenderedView(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	w, entity := openFrameLoopTestWindow(t, ta)
	w.SimulateFrameRequest()
	if w.isFrameDirty() {
		t.Fatal("the window is dirty after its frame was served")
	}

	model := NewEntity(app, app.RootScope(), func(*frameLoopModel, *Context[frameLoopModel]) {})
	model.Update(app, func(_ *frameLoopModel, cx *Context[frameLoopModel]) { cx.Notify() })
	if w.isFrameDirty() || w.RefreshRequested() {
		t.Fatal("notifying an entity the window never rendered dirtied the window")
	}

	draws := WindowDrawCount(w)
	renders := frameLoopRenders(app, entity)
	entity.Update(app, func(_ *frameLoopView, cx *Context[frameLoopView]) { cx.Notify() })
	if !w.isFrameDirty() || !w.RefreshRequested() {
		t.Fatal("notifying the rendered view did not dirty its window")
	}
	w.SimulateFrameRequest()
	if got := WindowDrawCount(w); got != draws+1 {
		t.Fatalf("draw count after the frame request = %d, want %d", got, draws+1)
	}
	if got := frameLoopRenders(app, entity); got != renders+1 {
		t.Fatalf("renders after the frame request = %d, want %d", got, renders+1)
	}
	if w.isFrameDirty() {
		t.Fatal("the window stayed dirty after the redraw")
	}
}

// frameLoopParent is a root view rendering a nested child view.
type frameLoopParent struct {
	child View
}

func (p *frameLoopParent) Render(w *Window, cx *Context[frameLoopParent]) AnyElement {
	return Div().SizeFull().Child(p.child).IntoElement()
}

// TestFrameLoopTracksNestedRenderedViews checks that a nested view
// rendered inside the frame is tracked by its window (the port of the
// accessed-entity tracking, window.rs:3302-3314 / app.rs:1189-1216): its
// notification dirties the window.
func TestFrameLoopTracksNestedRenderedViews(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	w := NewTestWindow(app, Size{Width: 200, Height: 100})
	var child Entity[frameLoopView]
	app.Update(func(app *App) {
		child = NewEntity(app, w.Scope(), func(*frameLoopView, *Context[frameLoopView]) {})
		parent := NewEntity(app, w.Scope(), func(p *frameLoopParent, _ *Context[frameLoopParent]) {
			p.child = ViewOfIn(w.Scope(), child)
		})
		w.SetRootView(ViewOf(parent))
	})
	if _, err := DrawWindowFrame(w); err != nil {
		t.Fatalf("draw: %v", err)
	}
	w.SimulateFrameRequest()
	if _, tracked := frameStateOf(w).renderedViews[child.EntityID()]; !tracked {
		t.Fatal("the nested view rendered by the frame is not tracked by its window")
	}
	renders := frameLoopRenders(app, child)
	child.Update(app, func(_ *frameLoopView, cx *Context[frameLoopView]) { cx.Notify() })
	if !w.isFrameDirty() {
		t.Fatal("notifying the nested rendered view did not dirty its window")
	}
	w.SimulateFrameRequest()
	if got := frameLoopRenders(app, child); got != renders+1 {
		t.Fatalf("nested view renders = %d, want %d", got, renders+1)
	}
}

// TestRequestAnimationFrameLoopStopsWhenIdle checks the animation-frame
// pacing (request_animation_frame window.rs:2633-2636 over on_next_frame
// :2613-2619 and request_frame :1787-1843): every frame the view
// requests is delivered on the next frame request, and once the view
// stops requesting the window goes idle — no callbacks, not dirty, no
// further wakes.
func TestRequestAnimationFrameLoopStopsWhenIdle(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	w, entity := openFrameLoopTestWindow(t, ta)
	w.SimulateFrameRequest()

	draws := WindowDrawCount(w)
	entity.Update(app, func(v *frameLoopView, cx *Context[frameLoopView]) {
		v.animate = 3
		cx.Notify()
	})
	// Frame 1 draws (animate 3 -> 2, requests), frames 2-4 are driven by
	// the animation-frame callbacks; frame 4 renders with animate 0 and
	// stops requesting.
	for i := 0; i < 10 && w.hasFrameDemand(); i++ {
		w.SimulateFrameRequest()
	}
	if w.hasFrameDemand() {
		t.Fatal("the animation loop did not settle within 10 frames")
	}
	if got := WindowDrawCount(w); got != draws+4 {
		t.Fatalf("animation draws = %d, want %d (one notify frame + 3 animation frames)", got-draws, 4)
	}
	if n := len(frameStateOf(w).nextFrameCallbacks); n != 0 {
		t.Fatalf("pending next-frame callbacks after the loop = %d, want 0", n)
	}
	baseline := w.FrameWakeCount()
	w.SimulateFrameRequest()
	app.Update(func(*App) {})
	if got := w.FrameWakeCount(); got != baseline {
		t.Fatalf("an idle window woke its frame source (%d -> %d)", baseline, got)
	}
	if got := WindowDrawCount(w); got != draws+4 {
		t.Fatalf("an idle frame request drew (draws %d, want %d)", got-draws, 4)
	}
}

// TestWindowRefreshAndRefreshWindows checks Window::refresh
// (window.rs:2287-2292: dirty unless drawing) and
// App::apply_refresh_effect (app.rs:1896-1903: every window dirty).
func TestWindowRefreshAndRefreshWindows(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	w1, _ := openFrameLoopTestWindow(t, ta)
	w2, _ := openFrameLoopTestWindow(t, ta)
	w1.SimulateFrameRequest()
	w2.SimulateFrameRequest()

	app.Update(func(*App) { w1.Refresh() })
	if !w1.isFrameDirty() || w2.isFrameDirty() {
		t.Fatalf("Refresh dirtied w1=%v w2=%v, want true/false", w1.isFrameDirty(), w2.isFrameDirty())
	}
	w1.SimulateFrameRequest()

	app.Update(func(app *App) { app.RefreshWindows() })
	if !w1.isFrameDirty() || !w2.isFrameDirty() {
		t.Fatal("RefreshWindows did not dirty every window")
	}

	// Refresh is suppressed while the window draws.
	w1.SimulateFrameRequest()
	w2.SimulateFrameRequest()
	ds := drawState(w1)
	ds.frame = &frameDrawState{}
	w1.Refresh()
	ds.frame = nil
	if w1.isFrameDirty() {
		t.Fatal("Refresh during a draw dirtied the window")
	}
}

// TestContextOnNextFrameAndSimulateNextFrame checks Context::on_next_frame
// (app/context.rs:292-301) delivered by Window::simulate_next_frame
// (window.rs:2643-2650).
func TestContextOnNextFrameAndSimulateNextFrame(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	w, entity := openFrameLoopTestWindow(t, ta)
	var seen []string
	entity.Update(app, func(_ *frameLoopView, cx *Context[frameLoopView]) {
		cx.OnNextFrame(w, func(v *frameLoopView, got *Window, cx *Context[frameLoopView]) {
			if got != w || cx.EntityID() != entity.EntityID() {
				t.Error("the next-frame callback received the wrong window or entity")
			}
			seen = append(seen, "a")
		})
		cx.OnNextFrame(w, func(*frameLoopView, *Window, *Context[frameLoopView]) { seen = append(seen, "b") })
	})
	if len(seen) != 0 {
		t.Fatal("next-frame callbacks ran before the next frame")
	}
	if n := w.SimulateNextFrame(app); n != 2 {
		t.Fatalf("SimulateNextFrame ran %d callbacks, want 2", n)
	}
	if len(seen) != 2 || seen[0] != "a" || seen[1] != "b" {
		t.Fatalf("callback order = %v, want [a b]", seen)
	}
	if n := w.SimulateNextFrame(app); n != 0 {
		t.Fatalf("a second SimulateNextFrame ran %d callbacks, want 0", n)
	}
}

// TestTestAppFrameLoopDrawsDirtyWindows checks the opt-in test frame loop
// (the test-support flush draw, app.rs:1800-1812): RunUntilParked and
// AdvanceClock draw dirty test windows; without the loop they do not.
func TestTestAppFrameLoopDrawsDirtyWindows(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	w, entity := openFrameLoopTestWindow(t, ta)
	draws := WindowDrawCount(w)

	entity.Update(app, func(_ *frameLoopView, cx *Context[frameLoopView]) { cx.Notify() })
	ta.RunUntilParked()
	if got := WindowDrawCount(w); got != draws {
		t.Fatalf("RunUntilParked drew without the frame loop (%d -> %d)", draws, got)
	}

	ta.SetFrameLoop(true)
	ta.RunUntilParked()
	if got := WindowDrawCount(w); got != draws+1 {
		t.Fatalf("RunUntilParked with the frame loop drew %d frames, want 1", got-draws)
	}
	if w.RefreshRequested() {
		t.Fatal("the window stayed dirty after the frame loop drew it")
	}

	entity.Update(app, func(_ *frameLoopView, cx *Context[frameLoopView]) { cx.Notify() })
	ta.AdvanceClock(16)
	if got := WindowDrawCount(w); got != draws+2 {
		t.Fatalf("AdvanceClock with the frame loop drew %d frames total, want 2", got-draws)
	}

	// An animation loop still parks: the next-frame callbacks wait for a
	// frame request.
	entity.Update(app, func(v *frameLoopView, cx *Context[frameLoopView]) {
		v.animate = 2
		cx.Notify()
	})
	ta.RunUntilParked()
	if got := WindowDrawCount(w); got != draws+3 {
		t.Fatalf("draws after the animated notify = %d, want 3", got-draws)
	}
	if n := w.SimulateNextFrame(app); n != 1 {
		t.Fatalf("pending animation callbacks = %d, want 1", n)
	}
	ta.RunUntilParked()
	if got := WindowDrawCount(w); got != draws+4 {
		t.Fatalf("draws after the animation frame = %d, want 4", got-draws)
	}
}
