package gpui

import (
	"testing"
	"time"
)

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
// TestWindowPreferredFrameIntervalSetAndGet covers the live half of the
// preferred-frame-interval API (browser-core ticket 47): the default is
// zero (the display rate), SetPreferredFrameInterval changes it, zero
// restores it, and a negative interval is rejected. The pacing itself
// is the Windows frame source's job
// (TestFrameLoopPreferredFrameIntervalDrawsEveryOtherVblank); a test
// window records the interval even though its frames arrive through
// SimulateFrameRequest.
func TestWindowPreferredFrameIntervalSetAndGet(t *testing.T) {
	ta := NewTestApp()
	w, _ := openFrameLoopTestWindow(t, ta)

	if got := w.PreferredFrameInterval(); got != 0 {
		t.Fatalf("a fresh window's preferred interval = %v, want 0 (the display rate)", got)
	}
	w.SetPreferredFrameInterval(40 * time.Millisecond)
	if got := w.PreferredFrameInterval(); got != 40*time.Millisecond {
		t.Fatalf("after SetPreferredFrameInterval(40ms) = %v, want 40ms", got)
	}
	w.SetPreferredFrameInterval(0)
	if got := w.PreferredFrameInterval(); got != 0 {
		t.Fatalf("after SetPreferredFrameInterval(0) = %v, want 0", got)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("SetPreferredFrameInterval(-1ms) must panic")
		}
	}()
	w.SetPreferredFrameInterval(-time.Millisecond)
}

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
// of the rendered root view dirties the window and redraws it without an
// explicit DrawWindowFrame; a model the window never accessed does not
// dirty it.
//
// Migrated by Base ticket 33: the test-support flush now draws dirty test
// windows (app.rs:1800-1812), so a notifying update redraws the window
// itself. The dirty half is observed on a notify raised outside an update
// (no flush yet) and served by a frame request; the update half proves
// the draw-on-flush default.
func TestFrameLoopNotifyRedrawsRenderedView(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	w, entity := openFrameLoopTestWindow(t, ta)
	w.SimulateFrameRequest()
	if w.isFrameDirty() {
		t.Fatal("the window is dirty after its frame was served")
	}

	model := NewEntity(app, app.RootScope(), func(*frameLoopModel, *Context[frameLoopModel]) {})
	draws := WindowDrawCount(w)
	app.notify(model.EntityID())
	if w.isFrameDirty() || w.RefreshRequested() {
		t.Fatal("notifying an entity the window never accessed dirtied the window")
	}
	app.Update(func(*App) {}) // apply the queued Notify effect
	model.Update(app, func(_ *frameLoopModel, cx *Context[frameLoopModel]) { cx.Notify() })
	if got := WindowDrawCount(w); got != draws {
		t.Fatalf("notifying an unaccessed model redrew the window (%d -> %d)", draws, got)
	}

	// The dirty half: a notify outside an update (no effect flush yet).
	renders := frameLoopRenders(app, entity)
	app.notify(entity.EntityID())
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
	app.Update(func(*App) {}) // apply the queued Notify effect

	// The default: a notifying update redraws at its flush.
	entity.Update(app, func(_ *frameLoopView, cx *Context[frameLoopView]) { cx.Notify() })
	if got := WindowDrawCount(w); got != draws+2 {
		t.Fatalf("draw count after the notifying update = %d, want %d", got, draws+2)
	}
	if got := frameLoopRenders(app, entity); got != renders+2 {
		t.Fatalf("renders after the notifying update = %d, want %d", got, renders+2)
	}
	if w.isFrameDirty() || w.RefreshRequested() {
		t.Fatal("the window stayed dirty after the flush drew it")
	}
}

// frameLoopReader is a root view that reads a model while rendering.
type frameLoopReader struct {
	model   Entity[frameLoopCounter]
	renders int
}

// frameLoopCounter is a model a view reads.
type frameLoopCounter struct{ value int }

func (r *frameLoopReader) Render(w *Window, cx *Context[frameLoopReader]) AnyElement {
	r.renders++
	_ = r.model.Read(cx, func(c *frameLoopCounter, _ *App) int { return c.value })
	return Div().SizeFull().IntoElement()
}

// TestFrameLoopTracksAccessedEntities checks the accessed-entity tracking
// (EntityMap::read records accessed entities, app/entity_map.rs:156-159;
// Window::record_entities_accessed, window.rs:3302-3314; App::notify
// invalidates the windows tracking the entity, app.rs:2920-2953): a model
// the view only READS during its render is tracked by the window, so its
// notification redraws the window with no observer wiring; a read outside
// a frame is not tracked.
func TestFrameLoopTracksAccessedEntities(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	w := NewTestWindow(app, Size{Width: 200, Height: 100})
	var view Entity[frameLoopReader]
	var model Entity[frameLoopCounter]
	app.Update(func(app *App) {
		model = NewEntity(app, w.Scope(), func(*frameLoopCounter, *Context[frameLoopCounter]) {})
		view = NewEntity(app, w.Scope(), func(r *frameLoopReader, _ *Context[frameLoopReader]) { r.model = model })
		w.SetRootView(ViewOf(view))
	})
	if got := WindowDrawCount(w); got != 1 {
		t.Fatalf("the flush did not draw the new root view (draws %d, want 1)", got)
	}
	if _, tracked := frameStateOf(w).trackedEntities[model.EntityID()]; !tracked {
		t.Fatal("the model read during the frame is not tracked by the window")
	}
	rendersOf := func() int { return view.Read(app, func(r *frameLoopReader, _ *App) int { return r.renders }) }
	renders := rendersOf()
	model.Update(app, func(c *frameLoopCounter, cx *Context[frameLoopCounter]) {
		c.value++
		cx.Notify()
	})
	if got := rendersOf(); got != renders+1 {
		t.Fatalf("notifying the read model rendered the view %d times, want 1", got-renders)
	}
	other := NewEntity(app, w.Scope(), func(*frameLoopCounter, *Context[frameLoopCounter]) {})
	_ = other.Read(app, func(*frameLoopCounter, *App) int { return 0 })
	if _, tracked := frameStateOf(w).trackedEntities[other.EntityID()]; tracked {
		t.Fatal("a read outside the frame was tracked")
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
// notification dirties the window and redraws it.
//
// Migrated by Base ticket 33: the dirty half is observed through a notify
// outside an update; the notifying update redraws at its flush.
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
	if _, tracked := frameStateOf(w).trackedEntities[child.EntityID()]; !tracked {
		t.Fatal("the nested view rendered by the frame is not tracked by its window")
	}
	renders := frameLoopRenders(app, child)
	app.notify(child.EntityID())
	if !w.isFrameDirty() {
		t.Fatal("notifying the nested rendered view did not dirty its window")
	}
	w.SimulateFrameRequest()
	if got := frameLoopRenders(app, child); got != renders+1 {
		t.Fatalf("nested view renders = %d, want %d", got, renders+1)
	}
	app.Update(func(*App) {})
	child.Update(app, func(_ *frameLoopView, cx *Context[frameLoopView]) { cx.Notify() })
	if got := frameLoopRenders(app, child); got != renders+2 {
		t.Fatalf("nested view renders after the notifying update = %d, want %d", got, renders+2)
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
//
// Migrated by Base ticket 33: the dirty flags are observed before any
// flush (Refresh called outside an update, the refresh effect applied
// directly); the updates then prove the flush draws exactly the
// refreshed windows.
func TestWindowRefreshAndRefreshWindows(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	w1, _ := openFrameLoopTestWindow(t, ta)
	w2, _ := openFrameLoopTestWindow(t, ta)
	w1.SimulateFrameRequest()
	w2.SimulateFrameRequest()

	w1.Refresh()
	if !w1.isFrameDirty() || w2.isFrameDirty() {
		t.Fatalf("Refresh dirtied w1=%v w2=%v, want true/false", w1.isFrameDirty(), w2.isFrameDirty())
	}
	w1.SimulateFrameRequest()

	d1, d2 := WindowDrawCount(w1), WindowDrawCount(w2)
	app.Update(func(*App) { w1.Refresh() })
	if WindowDrawCount(w1) != d1+1 || WindowDrawCount(w2) != d2 {
		t.Fatalf("Refresh in an update drew w1 %d / w2 %d frames, want 1/0", WindowDrawCount(w1)-d1, WindowDrawCount(w2)-d2)
	}

	app.applyEffect(effect{kind: effRefresh})
	if !w1.isFrameDirty() || !w2.isFrameDirty() {
		t.Fatal("RefreshWindows did not dirty every window")
	}
	d1, d2 = WindowDrawCount(w1), WindowDrawCount(w2)
	app.Update(func(app *App) { app.RefreshWindows() })
	if WindowDrawCount(w1) != d1+1 || WindowDrawCount(w2) != d2+1 {
		t.Fatalf("RefreshWindows drew w1 %d / w2 %d frames, want 1/1", WindowDrawCount(w1)-d1, WindowDrawCount(w2)-d2)
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

// TestTestAppFrameLoopDrawsDirtyWindows checks the default test frame
// loop (the test-support flush draw, app.rs:1800-1812): every update's
// flush draws the dirty test windows, and RunUntilParked/AdvanceClock
// draw the windows dirtied outside an update.
//
// Migrated by Base ticket 33 from the opt-in SetFrameLoop: the "without
// the loop" half became "the flush already drew it"; the RunUntilParked
// and AdvanceClock halves use dirtiness raised outside an update.
func TestTestAppFrameLoopDrawsDirtyWindows(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	w, entity := openFrameLoopTestWindow(t, ta)
	draws := WindowDrawCount(w)

	entity.Update(app, func(_ *frameLoopView, cx *Context[frameLoopView]) { cx.Notify() })
	if got := WindowDrawCount(w); got != draws+1 {
		t.Fatalf("the notifying update's flush drew %d frames, want 1", got-draws)
	}
	ta.RunUntilParked()
	if got := WindowDrawCount(w); got != draws+1 {
		t.Fatalf("RunUntilParked redrew a clean window (%d frames)", got-draws)
	}

	w.Refresh() // outside an update: no flush
	ta.RunUntilParked()
	if got := WindowDrawCount(w); got != draws+2 {
		t.Fatalf("RunUntilParked drew %d frames total, want 2", got-draws)
	}
	if w.RefreshRequested() {
		t.Fatal("the window stayed dirty after the frame loop drew it")
	}

	w.Refresh()
	ta.AdvanceClock(16)
	if got := WindowDrawCount(w); got != draws+3 {
		t.Fatalf("AdvanceClock drew %d frames total, want 3", got-draws)
	}

	// An animation loop still parks: the next-frame callbacks wait for a
	// frame request.
	entity.Update(app, func(v *frameLoopView, cx *Context[frameLoopView]) {
		v.animate = 2
		cx.Notify()
	})
	ta.RunUntilParked()
	if got := WindowDrawCount(w); got != draws+4 {
		t.Fatalf("draws after the animated notify = %d, want 4", got-draws)
	}
	if n := w.SimulateNextFrame(app); n != 1 {
		t.Fatalf("pending animation callbacks = %d, want 1", n)
	}
	ta.RunUntilParked()
	if got := WindowDrawCount(w); got != draws+5 {
		t.Fatalf("draws after the animation frame = %d, want 5", got-draws)
	}
}

// TestKeyDispatchTracksModifiers checks dispatch_event's
// ModifiersChanged arm (window.rs:5787-5790): the window's modifier state
// follows the dispatched event.
func TestKeyDispatchTracksModifiers(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	w := NewTestWindow(app, Size{Width: 200, Height: 100})
	app.Update(func(app *App) {
		w.dispatchKeyEvent(&ModifiersChangedEvent{Modifiers: Modifiers{Control: true, Shift: true}}, app)
	})
	if got := w.Modifiers(); !got.Control || !got.Shift || got.Alt {
		t.Fatalf("modifiers after ModifiersChanged = %+v, want control+shift", got)
	}
	app.Update(func(app *App) { w.dispatchKeyEvent(&ModifiersChangedEvent{}, app) })
	if got := w.Modifiers(); got.NumberOfModifiers() != 0 {
		t.Fatalf("modifiers after the release = %+v, want none", got)
	}
}

// TestCanonicalElementIDCoversEveryKind checks the accessibility path
// encoding distinguishes every ElementId variant the port carries
// (window.rs:7655-7675): equal-looking ids of different kinds, and the
// labeled child's parent, never collide.
func TestCanonicalElementIDCoversEveryKind(t *testing.T) {
	ids := []ElementID{
		NameElementID("a7"),
		IntegerElementID(7),
		ViewElementID(7),
		NamedIntegerElementID("a", 7),
		NamedIntegerElementID("a7", 0),
		NamedChildElementID(NameElementID("a"), "7"),
		NamedChildElementID(IntegerElementID(7), "a"),
		NamedChildElementID(NameElementID("x"), "7"),
		codeLocationElementID("a7"),
	}
	seen := map[string]int{}
	for i, id := range ids {
		key := canonicalElementID(id)
		if j, dup := seen[key]; dup {
			t.Fatalf("ids %d (%v) and %d (%v) share the canonical encoding %q", j, ids[j], i, id, key)
		}
		seen[key] = i
	}
}
