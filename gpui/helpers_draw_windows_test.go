package gpui

import (
	"errors"
	"testing"
)

// Ticket 09 tests for the window-bound helpers, drawn through real test
// windows (the native layout service; windows build like the other
// element-pipeline suites). Each test cites the pinned behavior
// (reference/ce-source/crates/gpui/src, CE 254b5dbd).

// drawHelperFrame draws one frame or fails the test.
func drawHelperFrame(t *testing.T, w *Window) {
	t.Helper()
	if _, err := DrawWindowFrame(w); err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}
}

// ---------------------------------------------------------------------------
// UseKeyedState / UseState (window.rs:4090-4132)
// ---------------------------------------------------------------------------

type keyedCounter struct{ n int }

type keyedHost struct {
	show   bool
	inits  int
	seen   []EntityID
	handle Entity[keyedCounter]
}

func (h *keyedHost) Render(w *Window, cx *Context[keyedHost]) AnyElement {
	if h.show {
		h.handle = w.UseKeyedState(NameElementID("counter"), cx, func(w *Window, ecx *Context[keyedCounter]) keyedCounter {
			h.inits++
			return keyedCounter{n: 7}
		})
		h.seen = append(h.seen, h.handle.EntityID())
	}
	return Div().Flex().IntoElement()
}

// TestHelperUseKeyedStateLifecycle pins use_keyed_state: the entity is
// built once and kept across consecutive frames, and frame-end eviction
// (Frame::finish 1225-1231 + the swap's drop) releases it so that a
// reinsertion starts fresh.
func TestHelperUseKeyedStateLifecycle(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	w := NewTestWindow(app, Size{Width: 200, Height: 100})
	defer CloseTestWindow(w)
	host := NewEntity(app, w.Scope(), func(h *keyedHost, _ *Context[keyedHost]) { h.show = true })
	w.SetRootView(ViewOf(host))

	drawHelperFrame(t, w)
	drawHelperFrame(t, w)
	var first EntityID
	var weak WeakEntity[keyedCounter]
	host.Read(app, func(h *keyedHost, _ *App) struct{} {
		if h.inits != 1 || len(h.seen) != 2 || h.seen[0] != h.seen[1] {
			t.Fatalf("inits=%d seen=%v, want one init and one stable entity", h.inits, h.seen)
		}
		first = h.seen[0]
		weak = h.handle.Downgrade()
		return struct{}{}
	})
	if got := ReadKeyed(t, app, weak); got != 7 {
		t.Fatalf("keyed state = %d, want 7", got)
	}
	// The facade cannot release the slot's lease.
	host.Read(app, func(h *keyedHost, _ *App) struct{} {
		defer func() {
			if recover() == nil {
				t.Error("releasing the keyed-state facade did not panic")
			}
		}()
		h.handle.Release()
		return struct{}{}
	})

	host.Update(app, func(h *keyedHost, _ *Context[keyedHost]) { h.show = false })
	drawHelperFrame(t, w)
	if weak.IsUpgradable() {
		t.Fatal("keyed state survived a frame where its element did not draw")
	}

	host.Update(app, func(h *keyedHost, _ *Context[keyedHost]) { h.show = true })
	drawHelperFrame(t, w)
	host.Read(app, func(h *keyedHost, _ *App) struct{} {
		if h.inits != 2 {
			t.Fatalf("inits after reinsertion = %d, want 2", h.inits)
		}
		if h.seen[len(h.seen)-1] == first {
			t.Fatal("reinsertion reused the evicted entity")
		}
		return struct{}{}
	})
}

// ReadKeyed reads the counter through a weak handle.
func ReadKeyed(t *testing.T, app *App, weak WeakEntity[keyedCounter]) int {
	t.Helper()
	n, err := weak.TryRead(app, func(c *keyedCounter, _ *App) int { return c.n })
	if err != nil {
		t.Fatalf("TryRead: %v", err)
	}
	return n
}

// TestHelperUseKeyedStateNotifiesView pins the observer installed by
// use_keyed_state (window.rs:4105-4108): notifying the keyed entity
// notifies the view that rendered it.
func TestHelperUseKeyedStateNotifiesView(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	w := NewTestWindow(app, Size{Width: 200, Height: 100})
	defer CloseTestWindow(w)
	host := NewEntity(app, w.Scope(), func(h *keyedHost, _ *Context[keyedHost]) { h.show = true })
	w.SetRootView(ViewOf(host))
	viewNotified := 0
	NewEntity(app, app.RootScope(), func(_ *helperCounter, cx *Context[helperCounter]) {
		cx.Observe(host, func(*helperCounter, Entity[keyedHost], *Context[helperCounter]) { viewNotified++ })
	})
	drawHelperFrame(t, w)

	var keyed Entity[keyedCounter]
	host.Read(app, func(h *keyedHost, _ *App) struct{} { keyed = h.handle; return struct{}{} })
	keyed.Update(app, func(c *keyedCounter, cx *Context[keyedCounter]) {
		c.n++
		cx.Notify()
	})
	if viewNotified != 1 {
		t.Fatalf("view notifications = %d, want 1", viewNotified)
	}
}

type useStateHost struct {
	a, b []EntityID
}

func (h *useStateHost) Render(w *Window, cx *Context[useStateHost]) AnyElement {
	init := func(*Window, *Context[keyedCounter]) keyedCounter { return keyedCounter{} }
	h.a = append(h.a, w.UseState(cx, init).EntityID())
	h.b = append(h.b, w.UseState(cx, init).EntityID())
	return Div().Flex().IntoElement()
}

// TestHelperUseStateKeysByCallSite pins use_state (window.rs:4116-4132):
// each call site keeps its own state across frames.
func TestHelperUseStateKeysByCallSite(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	w := NewTestWindow(app, Size{Width: 200, Height: 100})
	defer CloseTestWindow(w)
	host := NewEntity(app, w.Scope(), func(*useStateHost, *Context[useStateHost]) {})
	w.SetRootView(ViewOf(host))
	drawHelperFrame(t, w)
	drawHelperFrame(t, w)
	host.Read(app, func(h *useStateHost, _ *App) struct{} {
		if h.a[0] != h.a[1] || h.b[0] != h.b[1] {
			t.Fatalf("UseState not stable across frames: a=%v b=%v", h.a, h.b)
		}
		if h.a[0] == h.b[0] {
			t.Fatal("two call sites share one UseState entity")
		}
		return struct{}{}
	})
}

// ---------------------------------------------------------------------------
// Focus listeners (app/context.rs:547-668; window.rs:3233-3274)
// ---------------------------------------------------------------------------

type focusForwarder struct {
	a, b FocusHandle
}

func (f *focusForwarder) Render(w *Window, cx *Context[focusForwarder]) AnyElement {
	return Div().Flex().
		Child(Div().Flex().W(PxLength(50)).H(PxLength(50)).TrackFocus(f.a)).
		Child(Div().Flex().W(PxLength(50)).H(PxLength(50)).TrackFocus(f.b)).
		IntoElement()
}

// TestHelperFocusMovedByFocusListenerIsDispatched ports window.rs:
// 9073-9109 test_focus_moved_by_focus_listener_is_dispatched with
// app-level focus handles (cx.focus_handle(), app.rs:2796): a listener
// on a moves focus to b, and b's listener runs exactly once.
//
// Migrated by Base ticket 33 to the pin's shape: the focus update's flush
// draws the frame whose focus phase runs a's listener; the listener's
// focus move refreshes the window after the frame is published
// (window.rs:3279-3285), so the window is dirty again and
// run_until_parked draws the follow-up frame that dispatches b's focus.
// (Before, the refresh was cleared by the completing frame and the
// follow-up frame was drawn explicitly.)
func TestHelperFocusMovedByFocusListenerIsDispatched(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	w := NewTestWindow(app, Size{Width: 200, Height: 100})
	defer CloseTestWindow(w)
	bFocusCount := 0
	view := NewEntity(app, w.Scope(), func(f *focusForwarder, cx *Context[focusForwarder]) {
		f.a = cx.App().FocusHandle()
		f.b = cx.App().FocusHandle()
		cx.OnFocus(f.a, w, func(this *focusForwarder, w *Window, cx *Context[focusForwarder]) {
			w.Focus(this.b)
		})
		cx.OnFocus(f.b, w, func(*focusForwarder, *Window, *Context[focusForwarder]) {
			bFocusCount++
		})
	})
	w.SetRootView(ViewOf(view))
	drawHelperFrame(t, w)

	var a, b FocusHandle
	view.Read(app, func(f *focusForwarder, _ *App) struct{} { a, b = f.a, f.b; return struct{}{} })
	app.Update(func(*App) { w.Focus(a) }) // the flush draws: a's listener forwards focus to b
	if !w.isFrameDirty() {
		t.Fatal("the focus listener's refresh was lost by the completing frame")
	}
	ta.RunUntilParked() // the follow-up frame dispatches b's focus

	if !b.IsFocused(w) {
		t.Fatal("b is not focused after the forwarding listener")
	}
	if bFocusCount != 1 {
		t.Fatalf("b focus count = %d, want 1", bFocusCount)
	}
}

type focusTree struct {
	parent, child FocusHandle
	log           *[]string
}

func (f *focusTree) Render(w *Window, cx *Context[focusTree]) AnyElement {
	return Div().Flex().TrackFocus(f.parent).
		Child(Div().Flex().W(PxLength(10)).H(PxLength(10)).TrackFocus(f.child)).
		IntoElement()
}

// TestHelperFocusInOutAndBlur pins on_focus / on_focus_in / on_blur /
// on_focus_out against the focus-path comparison (window.rs:333-339 and
// app/context.rs:547-668), including the FocusOutEvent's blurred handle.
func TestHelperFocusInOutAndBlur(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	w := NewTestWindow(app, Size{Width: 200, Height: 100})
	defer CloseTestWindow(w)
	var log []string
	view := NewEntity(app, w.Scope(), func(f *focusTree, cx *Context[focusTree]) {
		f.parent = NewFocusHandle(w)
		f.child = NewFocusHandle(w)
		f.log = &log
		rec := func(s string) func(*focusTree, *Window, *Context[focusTree]) {
			return func(*focusTree, *Window, *Context[focusTree]) { log = append(log, s) }
		}
		cx.OnFocus(f.parent, w, rec("focus:parent"))
		cx.OnFocus(f.child, w, rec("focus:child"))
		cx.OnFocusIn(f.parent, w, rec("in:parent"))
		cx.OnBlur(f.parent, w, rec("blur:parent"))
		cx.OnBlur(f.child, w, rec("blur:child"))
		child := f.child
		parent := f.parent
		cx.OnFocusOut(f.parent, w, func(_ *focusTree, e FocusOutEvent, _ *Window, _ *Context[focusTree]) {
			name := "?"
			switch e.Blurred.ID() {
			case child.ID():
				name = "child"
			case parent.ID():
				name = "parent"
			}
			log = append(log, "out:parent(blurred="+name+")")
		})
		cx.OnFocusOut(f.child, w, func(_ *focusTree, e FocusOutEvent, _ *Window, _ *Context[focusTree]) {
			log = append(log, "out:child")
		})
	})
	w.SetRootView(ViewOf(view))
	drawHelperFrame(t, w)

	var parent, child FocusHandle
	view.Read(app, func(f *focusTree, _ *App) struct{} { parent, child = f.parent, f.child; return struct{}{} })

	step := func(name string, f func(), want ...string) {
		t.Helper()
		log = nil
		app.Update(func(*App) { f() })
		drawHelperFrame(t, w)
		requireLog(t, name, log, want...)
	}
	// [] -> [parent, child]: child focused, parent focused-in.
	step("focus child", func() { w.Focus(child) }, "focus:child", "in:parent")
	// [parent, child] -> [parent]: child blurred and focused-out.
	step("focus parent", func() { w.Focus(parent) }, "focus:parent", "blur:child", "out:child")
	// [parent] -> []: parent blurred and focused-out.
	step("blur", func() { w.Blur() }, "blur:parent", "out:parent(blurred=parent)")
	// No change: no events.
	step("no change", func() {})
}

// requireLog asserts an exact ordered log (registration order is the
// delivery order).
func requireLog(t *testing.T, step string, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: log = %v, want %v", step, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s: log = %v, want %v", step, got, want)
		}
	}
}

// TestHelperWindowActivation pins observe_window_activation
// (app/context.rs:444-458; window.rs:1887-1902) and the focus phase's
// activation blanking (window.rs:3253-3262): deactivating runs the
// observers and the next frame blurs the focused handle.
func TestHelperWindowActivation(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	w := NewTestWindow(app, Size{Width: 200, Height: 100})
	defer CloseTestWindow(w)
	var log []string
	view := NewEntity(app, w.Scope(), func(f *focusTree, cx *Context[focusTree]) {
		f.parent = NewFocusHandle(w)
		f.child = NewFocusHandle(w)
		cx.ObserveWindowActivation(w, func(_ *focusTree, w *Window, _ *Context[focusTree]) {
			if w.isWindowActiveCached() {
				log = append(log, "active")
			} else {
				log = append(log, "inactive")
			}
		})
		cx.OnBlur(f.child, w, func(*focusTree, *Window, *Context[focusTree]) { log = append(log, "blur:child") })
		cx.OnFocus(f.child, w, func(*focusTree, *Window, *Context[focusTree]) { log = append(log, "focus:child") })
	})
	w.SetRootView(ViewOf(view))
	var child FocusHandle
	view.Read(app, func(f *focusTree, _ *App) struct{} { child = f.child; return struct{}{} })
	app.Update(func(*App) { w.Focus(child) })
	drawHelperFrame(t, w)
	requireLog(t, "first frame", log, "focus:child")

	log = nil
	w.activationChanged(false, app)
	requireLog(t, "deactivate", log, "inactive")
	if !w.RefreshRequested() {
		t.Fatal("activation change did not refresh the window")
	}
	drawHelperFrame(t, w)
	requireLog(t, "inactive frame", log, "inactive", "blur:child")

	log = nil
	w.activationChanged(true, app)
	drawHelperFrame(t, w)
	requireLog(t, "reactivate", log, "active", "focus:child")
}

// ---------------------------------------------------------------------------
// Deferral, subscriptions, spawn and listeners
// ---------------------------------------------------------------------------

// TestHelperWindowDeferAndDeferIn pins Window::defer (window.rs:
// 2510-2515) and Context::defer_in (app/context.rs:305-317): both run at
// the end of the current effect cycle with the window, and are skipped
// once the window closed (or the entity released).
func TestHelperWindowDeferAndDeferIn(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	w := NewTestWindow(app, Size{Width: 200, Height: 100})
	var log []string
	entity := NewEntity(app, app.RootScope(), func(*helperCounter, *Context[helperCounter]) {})

	app.Update(func(a *App) {
		w.Defer(a, func(got *Window, _ *App) {
			if got != w {
				t.Error("Window.Defer passed another window")
			}
			log = append(log, "window")
		})
		entity.Update(a, func(c *helperCounter, cx *Context[helperCounter]) {
			cx.DeferIn(w, func(c *helperCounter, got *Window, _ *Context[helperCounter]) {
				c.n++
				log = append(log, "entity")
			})
		})
		log = append(log, "update-end")
	})
	requireLog(t, "deferred", log, "update-end", "window", "entity")

	log = nil
	app.Update(func(a *App) {
		w.Defer(a, func(*Window, *App) { log = append(log, "window") })
		entity.Update(a, func(_ *helperCounter, cx *Context[helperCounter]) {
			cx.DeferIn(w, func(*helperCounter, *Window, *Context[helperCounter]) { log = append(log, "entity") })
		})
		a.removeWindow(w.id)
	})
	requireLog(t, "closed window", log)
	CloseTestWindow(w)
}

type helperEmitter struct{}

var helperEvent = DefineEvent[helperEmitter, string]()

// TestHelperSubscribeIn pins subscribe_in (app/context.rs:355-388): the
// callback receives the window with the payload and is skipped once the
// window closed.
func TestHelperSubscribeIn(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	w := NewTestWindow(app, Size{Width: 200, Height: 100})
	emitter := NewEntity(app, app.RootScope(), func(*helperEmitter, *Context[helperEmitter]) {})
	var got []string
	NewEntity(app, app.RootScope(), func(_ *helperCounter, cx *Context[helperCounter]) {
		helperEvent.SubscribeIn(cx, emitter, w, func(c *helperCounter, _ Entity[helperEmitter], p *string, gotW *Window, _ *Context[helperCounter]) {
			if gotW != w {
				t.Error("SubscribeIn passed another window")
			}
			got = append(got, *p)
		})
	})
	emitter.Update(app, func(_ *helperEmitter, cx *Context[helperEmitter]) { helperEvent.Emit(cx, "one") })
	CloseTestWindow(w)
	emitter.Update(app, func(_ *helperEmitter, cx *Context[helperEmitter]) { helperEvent.Emit(cx, "two") })
	requireLog(t, "SubscribeIn", got, "one")
}

// TestHelperSpawnIn pins spawn_in (app/context.rs:676-683 over
// Window::spawn, window.rs:2656-2666): the body gets a weak entity and
// a window-bound async context whose update reaches the window, and
// reports the closed window afterwards (AsyncWindowContext::update,
// app/async_context.rs:337-340).
func TestHelperSpawnIn(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	w := NewTestWindow(app, Size{Width: 200, Height: 100})
	entity := NewEntity(app, app.RootScope(), func(*helperCounter, *Context[helperCounter]) {})
	var task Task[int]
	entity.Update(app, func(_ *helperCounter, cx *Context[helperCounter]) {
		task = cx.SpawnIn(w, func(weak WeakEntity[helperCounter], acx *AsyncWindowContext) int {
			if acx.Window() != w {
				t.Error("AsyncWindowContext bound to another window")
			}
			if err := acx.Update(func(got *Window, a *App) {
				_ = weak.TryUpdate(a, func(c *helperCounter, _ *Context[helperCounter]) { c.n = 5 })
			}); err != nil {
				t.Errorf("Update on an open window: %v", err)
			}
			return 1
		})
	})
	ta.RunUntilParked()
	if !task.Done() {
		t.Fatal("SpawnIn task did not complete")
	}
	if n := entity.Read(app, func(c *helperCounter, _ *App) int { return c.n }); n != 5 {
		t.Fatalf("entity n = %d, want 5", n)
	}

	CloseTestWindow(w)
	var updateErr error
	ran := false
	entity.Update(app, func(_ *helperCounter, cx *Context[helperCounter]) {
		cx.SpawnIn(w, func(_ WeakEntity[helperCounter], acx *AsyncWindowContext) int {
			updateErr = acx.Update(func(*Window, *App) { ran = true })
			return 0
		})
	})
	ta.RunUntilParked()
	if ran || !errors.Is(updateErr, ErrWindowClosed) {
		t.Fatalf("closed-window update ran=%v err=%v, want ErrWindowClosed", ran, updateErr)
	}
}

// TestHelperListenerFor pins listener_for (window.rs:6868-6877): the
// adapter updates the entity, and is a no-op once it released.
func TestHelperListenerFor(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	w := NewTestWindow(app, Size{Width: 200, Height: 100})
	defer CloseTestWindow(w)
	scope := app.RootScope().Child()
	entity := NewEntity(app, scope, func(*helperCounter, *Context[helperCounter]) {})
	weak := entity.Downgrade()
	listener := w.ListenerFor(entity, func(c *helperCounter, e *int, got *Window, _ *Context[helperCounter]) {
		c.n += *e
	})
	delta := 3
	app.Update(func(a *App) { listener(&delta, w, a) })
	if n, _ := weak.TryRead(app, func(c *helperCounter, _ *App) int { return c.n }); n != 3 {
		t.Fatalf("n = %d, want 3", n)
	}
	app.Update(func(*App) { scope.Close() })
	app.Update(func(a *App) { listener(&delta, w, a) }) // released: no-op
}

// TestHelperWindowRootDowncast pins Window::root (window.rs:2272-2279)
// over AnyView::downcast (view.rs:53-61).
func TestHelperWindowRootDowncast(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	w := NewTestWindow(app, Size{Width: 200, Height: 100})
	defer CloseTestWindow(w)
	if _, ok := w.Root[keyedHost](); ok {
		t.Fatal("Root reported a root before SetRootView")
	}
	host := NewEntity(app, w.Scope(), func(*keyedHost, *Context[keyedHost]) {})
	w.SetRootView(ViewOf(host))
	got, ok := w.Root[keyedHost]()
	if !ok || got.EntityID() != host.EntityID() {
		t.Fatalf("Root[keyedHost] = %v, %v", got.EntityID(), ok)
	}
	got.Update(app, func(h *keyedHost, _ *Context[keyedHost]) { h.inits = 9 })
	if _, ok := w.Root[useStateHost](); ok {
		t.Fatal("Root downcast to the wrong state type")
	}
	if _, ok := DowncastView[keyedHost](recipeView{}); ok {
		t.Fatal("a stateless recipe downcast to an entity")
	}
}

type recipeView struct{}

func (recipeView) EntityID() (EntityID, bool)                { return 0, false }
func (recipeView) RenderOnce(w *Window, app *App) AnyElement { return Div().Flex().IntoElement() }

type lineHeightHost struct{ inFrame float32 }

func (h *lineHeightHost) Render(w *Window, cx *Context[lineHeightHost]) AnyElement {
	h.inFrame = w.LineHeight()
	return Div().Flex().IntoElement()
}

// TestHelperLineHeightAndModifiers pins Window::line_height
// (window.rs:2983-2985 → style.rs:719-721 rounds the result) and
// Window::modifiers (window.rs:3127-3129, tracked from dispatched
// input). Browser-parity D1: the default text style's line height is
// CSS `normal`, resolved from the window's text seam — the test
// window's modeled face (ascent 1025, descent 275, line gap 0 over
// 1000 upem): round(16.4) + round(4.4) + 0 = 16 + 4 + 0 = 20 at 16px
// (Chromium's FontMetrics::LineSpacing rule, simple_font_data.cc:
// 158-165). The golden-ratio 26 was the pre-D1 default.
func TestHelperLineHeightAndModifiers(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	w := NewTestWindow(app, Size{Width: 200, Height: 100})
	defer CloseTestWindow(w)
	if got := w.LineHeight(); got != 20 {
		t.Fatalf("LineHeight outside a frame = %v, want 20", got)
	}
	host := NewEntity(app, w.Scope(), func(*lineHeightHost, *Context[lineHeightHost]) {})
	w.SetRootView(ViewOf(host))
	drawHelperFrame(t, w)
	if got := host.Read(app, func(h *lineHeightHost, _ *App) float32 { return h.inFrame }); got != 20 {
		t.Fatalf("LineHeight in a frame = %v, want 20", got)
	}

	if w.Modifiers().Modified() {
		t.Fatal("modifiers set before any input")
	}
	w.DispatchInput(&MouseMoveEvent{Position: Point{X: 1, Y: 1}, Modifiers: Modifiers{Shift: true}}, app)
	if m := w.Modifiers(); !m.Shift || m.Control {
		t.Fatalf("Modifiers = %+v, want shift", m)
	}
}

// TestHelperAppFocusHandleInWindow pins app-level handles (app.rs:2796)
// working in a window: tracked, focused, then released (the focus
// clears like a window handle's release).
func TestHelperAppFocusHandleInWindow(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	w := NewTestWindow(app, Size{Width: 200, Height: 100})
	defer CloseTestWindow(w)
	view := NewEntity(app, w.Scope(), func(f *focusForwarder, cx *Context[focusForwarder]) {
		f.a = cx.App().FocusHandle()
		f.b = cx.App().FocusHandle()
	})
	w.SetRootView(ViewOf(view))
	var a FocusHandle
	view.Read(app, func(f *focusForwarder, _ *App) struct{} { a = f.a; return struct{}{} })
	app.Update(func(*App) { w.Focus(a) })
	drawHelperFrame(t, w)
	if !a.IsFocused(w) || !w.Focused().Ok || w.Focused().Handle.ID() != a.ID() {
		t.Fatal("app-level handle not focused in the window")
	}
	if !a.ContainsFocused(w) {
		t.Fatal("app-level handle does not contain its own focus")
	}
	a.Release()
	if w.Focused().Ok {
		t.Fatal("releasing the app-level handle kept the window focus")
	}
}
