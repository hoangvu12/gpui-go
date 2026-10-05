//go:build windows

package authorspec

import (
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"gpui-go/gpui"
)

// This file is the ticket11 acceptance slice: a REAL two-window counter
// through the Win32 host and the renderer — real windows sharing one
// counter model, increments via entity updates from each window in
// turn, both windows re-rendering (the scene is rebuilt with the new
// text), views with independent lifetimes (closing one window preserves
// the independently retained model and the other window keeps working;
// closing the last owner releases the model), the Styled fluent API
// composing through the real render path, and each redraw producing a
// present + retirement through the real GPU ledger.
//
// Environment expectation (the winhostspec pattern): this session runs
// interactively with a D3D11-capable adapter — window creation failures
// fail the test instead of skipping.

// realCounterView is the real window's root view: it observes the
// counter entity (the retained lease of the window that owns it) and
// renders the counter tree with observables (debug-selector bounds and
// the styled count label).
type realCounterView struct {
	counter  gpui.Entity[Counter]
	lease    gpui.Entity[Counter]
	renders  int
	notified int
}

// Render implements gpui.Render[realCounterView]: the counter tree with
// the styled count label (the CountLabel recipe composing the authoring
// Styled helper) and the increment div.
func (v *realCounterView) Render(w *gpui.Window, cx *gpui.Context[realCounterView]) gpui.AnyElement {
	v.renders++
	value := v.counter.Read(cx, func(c *Counter, _ *gpui.App) int { return c.count })
	return gpui.Div().
		ID("counter-root").
		DebugSelector("root").
		Flex().
		Gap2().
		Justify(gpui.AlignContentCenter).
		Bg(gpui.RgbaToHsla(0xdc2626ff)).
		SizeFull().
		TextSize(gpui.RemsOf(1.25)).
		Child(gpui.Div().
			ID("counter-label").
			DebugSelector("label").
			Flex().
			Child(NewCountLabel(value).Px3().Prefix("Count: "))).
		Child(gpui.Div().
			ID("increment").
			Child("Increment")).
		IntoElement()
}

// realCounterFacts reads the view entity's render and notification
// counts.
func realCounterFacts(app *gpui.App, view gpui.Entity[realCounterView]) (renders, notified int) {
	type facts struct{ renders, notified int }
	f := view.Read(app, func(v *realCounterView, _ *gpui.App) facts {
		return facts{renders: v.renders, notified: v.notified}
	})
	return f.renders, f.notified
}

// realCounterApp bundles the real-application test state.
type realCounterApp struct {
	app      *gpui.App
	host     *gpui.Host
	renderer *gpui.Renderer
	windowA  *gpui.Window
	windowB  *gpui.Window
	viewA    gpui.Entity[realCounterView]
	viewB    gpui.Entity[realCounterView]
	counter  gpui.Entity[Counter]
	// readLease is the test's independent read lease of the counter (a
	// lease in the app root scope, released before the last owner
	// closes so the model's release is observable).
	readLease gpui.Entity[Counter]
	released  atomic.Bool
	// sync runs f on the foreground (host) thread (all application
	// access is marshaled there).
	sync func(f func())
}

// bootRealCounterApp boots the real application: a started host, a
// manual renderer (no pacing worker), and two windows with root views
// over one shared counter model (window A's lease, window B's retained
// lease — the two-owner shape of the authoring decision round).
func bootRealCounterApp(t *testing.T) *realCounterApp {
	t.Helper()
	before := runtime.NumGoroutine()
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
		waitGoroutinesNear(t, before)
	})

	renderer, err := gpui.NewRendererManual()
	if err != nil {
		t.Fatalf("NewRendererManual: %v", err)
	}
	t.Cleanup(func() {
		if err := renderer.Close(); err != nil {
			t.Errorf("renderer close: %v", err)
		}
	})

	state := &realCounterApp{app: app, host: host, renderer: renderer}
	// All application access is marshaled to the foreground (host)
	// thread.
	state.sync = func(f func()) {
		done := make(chan struct{})
		if !host.Post(func() {
			defer close(done)
			f()
		}) {
			t.Fatalf("foreground post was dropped: the host loop exited")
		}
		<-done
	}

	// Open the two windows with root views (the reference open_window
	// shape: the window draws its first frame before the open returns).
	state.sync(func() {
		var openErr error
		state.windowA, openErr = app.OpenWindowView(gpui.WindowOptions{
			Title:     "authorspec counter A",
			Show:      true,
			Resizable: true,
			Bounds:    gpui.Bounds{Size: gpui.Size{Width: 360, Height: 240}},
		}, func(w *gpui.Window, app *gpui.App) gpui.View {
			// The counter model is created in window A's ownership
			// domain, with a release hook recording its retirement.
			state.counter = NewCounter(app, w.Scope(), w, 8)
			state.counter.Update(app, func(c *Counter, cx *gpui.Context[Counter]) {
				cx.OnRelease(func(_ *Counter, _ *gpui.App) {
					state.released.Store(true)
				}).Detach()
			})
			// The test's own read lease: independent of either window's
			// lease, so closing window A does not invalidate the test's
			// reads (the shared window-A lease would).
			state.readLease = state.counter.RetainInto(app.RootScope())
			state.viewA = gpui.NewEntity(app, w.Scope(), func(v *realCounterView, cx *gpui.Context[realCounterView]) {
				v.counter = state.counter
				v.lease = state.counter
				cx.Observe(state.counter, func(_ *realCounterView, _ gpui.Entity[Counter], cx *gpui.Context[realCounterView]) {
					v.notified++
					cx.Notify()
				}).Detach()
			})
			return gpui.ViewOf(state.viewA)
		})
		if openErr != nil {
			t.Fatalf("OpenWindowView A: %v — this session is expected to create real windows", openErr)
		}
		state.windowB, openErr = app.OpenWindowView(gpui.WindowOptions{
			Title:     "authorspec counter B",
			Show:      true,
			Resizable: true,
			Bounds:    gpui.Bounds{Size: gpui.Size{Width: 300, Height: 200}},
		}, func(w *gpui.Window, app *gpui.App) gpui.View {
			// Window B holds an independent retained lease of the shared
			// model (assignment would alias A's lease).
			counterB := state.counter.RetainInto(w.Scope())
			state.viewB = gpui.NewEntity(app, w.Scope(), func(v *realCounterView, cx *gpui.Context[realCounterView]) {
				v.counter = counterB
				v.lease = counterB
				cx.Observe(counterB, func(_ *realCounterView, _ gpui.Entity[Counter], cx *gpui.Context[realCounterView]) {
					v.notified++
					cx.Notify()
				}).Detach()
			})
			return gpui.ViewOf(state.viewB)
		})
		if openErr != nil {
			t.Fatalf("OpenWindowView B: %v — this session is expected to create real windows", openErr)
		}
	})
	t.Cleanup(func() {
		for _, window := range []*gpui.Window{state.windowA, state.windowB} {
			if window != nil && window.Alive() {
				window.Close()
				waitForCond(t, 10*time.Second, "the window to be destroyed", func() bool { return !window.Alive() })
			}
		}
	})
	return state
}

// incrementFrom applies one increment from the given window inside one
// update (the simulated click: entity update + notify, coalesced within
// the update).
func (state *realCounterApp) incrementFrom(t *testing.T, window *gpui.Window, clicks int) {
	t.Helper()
	state.sync(func() {
		state.app.Update(func(app *gpui.App) {
			for i := 0; i < clicks; i++ {
				state.readLease.UpdateIn(app, window, func(c *Counter, _ *gpui.Window, cx *gpui.Context[Counter]) {
					c.increment(cx)
				})
			}
		})
	})
}

// redrawAndPresent redraws the window (one full frame through the
// element phases) and presents it through the renderer, retiring the
// submission. It reports the drawn scene's sprite and quad counts.
func (state *realCounterApp) redrawAndPresent(t *testing.T, window *gpui.Window) (quads, mono, sub, poly int) {
	t.Helper()
	state.sync(func() {
		scene, err := gpui.DrawWindowFrame(window)
		if err != nil {
			t.Fatalf("DrawWindowFrame: %v", err)
		}
		if scene == nil {
			t.Fatalf("DrawWindowFrame produced no scene")
		}
		quads, mono, sub, poly, err = gpui.WindowPrimitiveCounts(window)
		if err != nil {
			t.Fatalf("WindowPrimitiveCounts: %v", err)
		}
		submission, err := gpui.PresentWindow(window, state.renderer, gpui.Color{R: 1, G: 1, B: 1, A: 1})
		if err != nil {
			t.Fatalf("PresentWindow: %v", err)
		}
		// One poll records the state, then wait for completion and retire
		// (acceptance is not completion; retirement needs a completed
		// poll).
		if _, err := submission.Poll(); err != nil {
			t.Fatalf("submission poll: %v", err)
		}
		if _, err := submission.WaitForCompletion(10 * time.Second); err != nil {
			t.Fatalf("submission completion: %v", err)
		}
		if _, err := submission.Retire(); err != nil {
			t.Fatalf("submission retire: %v", err)
		}
	})
	return quads, mono, sub, poly
}

// TestRealTwoWindowCounterSharedModel is the ticket11 acceptance: two
// REAL windows sharing one counter model, incremented from each window
// in turn, both re-rendering with the new text, one window closing
// while the model survives through the other's lease, and the last
// owner's closure releasing the model.
func TestRealTwoWindowCounterSharedModel(t *testing.T) {
	state := bootRealCounterApp(t)

	// Both windows opened and drew their first frame (the reference
	// open_window draws before returning).
	for _, window := range []*gpui.Window{state.windowA, state.windowB} {
		if got := gpui.WindowDrawCount(window); got != 1 {
			t.Errorf("initial draw count = %d, want 1 (the window draws once at open)", got)
		}
	}
	rendersA, _ := realCounterFacts(state.app, state.viewA)
	rendersB, _ := realCounterFacts(state.app, state.viewB)
	if rendersA != 1 || rendersB != 1 {
		t.Errorf("initial renders = %d/%d, want 1/1", rendersA, rendersB)
	}
	if got := state.readCount(); got != 8 {
		t.Fatalf("initial count = %d, want 8", got)
	}

	// The initial label bounds (the styled count label at count 8).
	labelA0, ok := gpui.WindowDebugBound(state.windowA, "label")
	if !ok {
		t.Fatalf("no debug bounds recorded for the label selector of window A")
	}
	if labelA0.Size.Width <= 0 {
		t.Fatalf("label A bounds = %+v, want a content-sized label", labelA0)
	}

	// Increment from window A (simulated clicks, entity updates): both
	// windows' views are notified (the shared model) and both re-render
	// with the new text.
	state.incrementFrom(t, state.windowA, 2)
	_, notifiedA := realCounterFacts(state.app, state.viewA)
	_, notifiedB := realCounterFacts(state.app, state.viewB)
	if notifiedA != 1 || notifiedB != 1 {
		t.Errorf("notifications after one increment = A %d, B %d; want 1/1 (the shared model notifies both views, coalesced)", notifiedA, notifiedB)
	}
	quadsA, monoA, subA, polyA := state.redrawAndPresent(t, state.windowA)
	quadsB, monoB, subB, polyB := state.redrawAndPresent(t, state.windowB)
	if quadsA != 1 || quadsB != 1 {
		t.Errorf("quad counts = A %d, B %d; want 1/1 (the root background)", quadsA, quadsB)
	}
	// The REAL text stack rasterizes the label's glyphs into the scene
	// (unlike the deterministic test text system, whose rasters are
	// empty): the sprites carry the new text. The platform's recommended
	// rendering mode decides the sprite class (subpixel on this
	// machine), so any text sprite class proves the paint.
	if monoA == 0 && subA == 0 && polyA == 0 {
		t.Errorf("text sprite counts = A %d/%d/%d, want a non-zero class (the real text stack paints the count text)", monoA, subA, polyA)
	}
	if monoB == 0 && subB == 0 && polyB == 0 {
		t.Errorf("text sprite counts = B %d/%d/%d, want a non-zero class (the real text stack paints the count text)", monoB, subB, polyB)
	}
	rendersA, _ = realCounterFacts(state.app, state.viewA)
	rendersB, _ = realCounterFacts(state.app, state.viewB)
	if rendersA != 2 || rendersB != 2 {
		t.Errorf("renders after the first increment = A %d, B %d, want 2/2 (both windows re-rendered)", rendersA, rendersB)
	}
	// The count text grew ("Count: 8" -> "Count: 10"): the label's
	// content width follows the text.
	labelA1, ok := gpui.WindowDebugBound(state.windowA, "label")
	if !ok {
		t.Fatalf("no debug bounds recorded for the label selector of window A after the increment")
	}
	if labelA1.Size.Width <= labelA0.Size.Width {
		t.Errorf("label A width = %f after the increment, want > %f (the count text grew)", labelA1.Size.Width, labelA0.Size.Width)
	}
	if got := state.readCount(); got != 10 {
		t.Errorf("count after two increments = %d, want 10", got)
	}

	// Increment from window B in turn: the same shared-model behavior
	// from the other side.
	state.incrementFrom(t, state.windowB, 1)
	if got := state.readCount(); got != 11 {
		t.Errorf("count after window B's increment = %d, want 11", got)
	}
	state.redrawAndPresent(t, state.windowA)
	state.redrawAndPresent(t, state.windowB)
	rendersB, _ = realCounterFacts(state.app, state.viewB)
	if rendersB != 3 {
		t.Errorf("window B renders = %d, want 3", rendersB)
	}

	// The renderer ledger: every redraw produced a present and a
	// retirement (6 presents so far: 2 initial? no — the opens draw
	// without presenting; 4 presented redraws).
	_, accepted, retired, quarantined, inFlight := state.renderer.Ledger()
	if accepted != 4 || retired != 4 || quarantined != 0 || inFlight != 0 {
		t.Errorf("ledger = accepted %d, retired %d, quarantined %d, in-flight %d; want 4/4/0/0 (a present+retire per redrawn window per increment)", accepted, retired, quarantined, inFlight)
	}

	// Close window A: its scope retires (view A and its observer go
	// away), but the model survives through window B's retained lease
	// and window B keeps working.
	state.windowA.Close()
	waitForCond(t, 10*time.Second, "window A to be destroyed", func() bool { return !state.windowA.Alive() })
	if state.released.Load() {
		t.Fatal("the counter model was released while window B still holds an independent lease")
	}
	if got := state.readCount(); got != 11 {
		t.Errorf("count after closing window A = %d, want 11 (the model survives the closure)", got)
	}

	// Window B still works: increment, re-render and present.
	state.incrementFrom(t, state.windowB, 3)
	if got := state.readCount(); got != 14 {
		t.Errorf("count after window B's increments = %d, want 14", got)
	}
	state.redrawAndPresent(t, state.windowB)
	rendersB, notifiedB = realCounterFacts(state.app, state.viewB)
	if rendersB != 4 {
		t.Errorf("window B renders after the closure = %d, want 4", rendersB)
	}
	// The observer of the closed window A no longer fires; window B's
	// does (one delivery per increment batch).
	if notifiedB != 3 {
		t.Errorf("window B notifications = %d, want 3 (one per increment batch)", notifiedB)
	}

	// Closing the last owner releases the model (the dependent lease
	// count reaches zero): release the test's read lease first so only
	// the windows' leases remain.
	state.sync(func() { state.readLease.Release() })
	state.windowB.Close()
	waitForCond(t, 10*time.Second, "window B to be destroyed", func() bool { return !state.windowB.Alive() })
	waitForCond(t, 10*time.Second, "the counter model to release", func() bool { return state.released.Load() })
}

// readCount reads the counter's value on the foreground thread through
// the test's independent lease.
func (state *realCounterApp) readCount() int {
	var value int
	state.sync(func() {
		value = state.readLease.Read(state.app, func(c *Counter, _ *gpui.App) int { return c.count })
	})
	return value
}

// waitGoroutinesNear waits for the goroutine count to fall back near the
// baseline.
func waitGoroutinesNear(t *testing.T, before int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= before+2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("goroutines did not drain back near the baseline: before=%d now=%d", before, runtime.NumGoroutine())
}

// waitForCond waits for a condition with a timeout.
func waitForCond(t *testing.T, timeout time.Duration, what string, cond func() bool) {
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
