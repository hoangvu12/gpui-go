//go:build windows

package gpui

import (
	"sync/atomic"
	"testing"
	"time"
)

// TestFrameLoopRealWindowNotifyRepaints is the Base ticket 03 real-window
// acceptance: a REAL window enrolled in the frame loop (it has a present
// hook) is drawn and presented by its WM_PAINT frame requests
// (events.rs draw_window -> window.rs request_frame 1715-1843), paced by
// the vblank frame source (vsync.rs 40-55). A cx.Notify on the rendered
// view repaints the window without any explicit DrawWindowFrame call, and
// an animation-frame loop (window.rs 2633-2636) draws exactly the frames
// it requests and then goes idle.
func TestFrameLoopRealWindowNotifyRepaints(t *testing.T) {
	h := desktopNewHost(t)
	app := NewApp()
	if err := app.Attach(h); err != nil {
		t.Fatalf("app attach: %v", err)
	}

	var presents atomic.Int64
	var entity Entity[frameLoopView]
	window, err := app.OpenWindowView(WindowOptions{
		Title:    "gpui frame loop",
		Bounds:   Bounds{Origin: Point{X: 60, Y: 60}, Size: Size{Width: 240, Height: 160}},
		Show:     true,
		Activate: false,
		OnRedraw: func() { presents.Add(1) },
	}, func(w *Window, app *App) View {
		entity = NewEntity(app, w.Scope(), func(*frameLoopView, *Context[frameLoopView]) {})
		return ViewOf(entity)
	})
	if err != nil {
		t.Fatalf("OpenWindowView failed: %v — this session is expected to create real windows", err)
	}
	t.Cleanup(func() {
		if window.Alive() {
			window.Close()
			desktopWaitFor(t, "the window to be destroyed", func() bool { return !window.Alive() })
		}
	})

	// onHost reads frame-loop facts on the foreground thread.
	type facts struct {
		draws    uint64
		renders  int
		demand   bool
		pending  int
		presents int64
	}
	read := func() facts {
		var f facts
		if !h.runForegroundSync(func() {
			f.draws = WindowDrawCount(window)
			f.renders = frameLoopRenders(app, entity)
			f.demand = window.hasFrameDemand()
			f.pending = len(frameStateOf(window).nextFrameCallbacks)
		}) {
			t.Fatal("the host stopped")
		}
		f.presents = presents.Load()
		return f
	}

	// The frame drawn at open is presented by a frame request: no
	// explicit draw or present call anywhere in this test.
	desktopWaitFor(t, "the opening frame to be presented", func() bool { return read().presents >= 1 })
	desktopWaitFor(t, "the window to go idle after opening", func() bool { return !read().demand })
	base := read()

	// Notify the rendered view: the frame loop repaints.
	h.runForegroundSync(func() {
		entity.Update(app, func(_ *frameLoopView, cx *Context[frameLoopView]) { cx.Notify() })
	})
	desktopWaitFor(t, "the notified view to be redrawn and presented", func() bool {
		f := read()
		return f.draws > base.draws && f.presents > base.presents && f.renders > base.renders && !f.demand
	})
	afterNotify := read()
	if afterNotify.draws != base.draws+1 {
		t.Errorf("draws after one notify = %d, want %d", afterNotify.draws, base.draws+1)
	}

	// An animation-frame loop: the notify frame plus 4 requested frames,
	// then idle.
	h.runForegroundSync(func() {
		entity.Update(app, func(v *frameLoopView, cx *Context[frameLoopView]) {
			v.animate = 4
			cx.Notify()
		})
	})
	want := afterNotify.draws + 5
	desktopWaitFor(t, "the animation frames to be drawn", func() bool { return read().draws >= want })
	desktopWaitFor(t, "the animation loop to go idle", func() bool {
		f := read()
		return !f.demand && f.pending == 0
	})
	// Idle means idle: no further frames are drawn.
	settled := read()
	time.Sleep(250 * time.Millisecond)
	final := read()
	if final.draws != want || settled.draws != want {
		t.Fatalf("animation draws = %d (settled %d), want exactly %d", final.draws-afterNotify.draws, settled.draws-afterNotify.draws, 5)
	}
	if final.presents != settled.presents {
		t.Fatalf("an idle window kept presenting (%d -> %d)", settled.presents, final.presents)
	}
	if final.pending != 0 || final.demand {
		t.Fatalf("idle window still has frame demand (pending callbacks %d, demand %v)", final.pending, final.demand)
	}
}

// TestFrameLoopRealWindowWithoutPresentHookKeepsExplicitDraws checks the
// port's enrollment rule: a real window without a present hook is not
// redrawn by notifications (its frames are drawn explicitly), while its
// next-frame callbacks are still delivered by WM_PAINT frame requests.
func TestFrameLoopRealWindowWithoutPresentHookKeepsExplicitDraws(t *testing.T) {
	h := desktopNewHost(t)
	app := NewApp()
	if err := app.Attach(h); err != nil {
		t.Fatalf("app attach: %v", err)
	}
	var entity Entity[frameLoopView]
	window, err := app.OpenWindowView(WindowOptions{
		Title:  "gpui frame loop (explicit)",
		Bounds: Bounds{Origin: Point{X: 80, Y: 80}, Size: Size{Width: 200, Height: 120}},
		Show:   true,
	}, func(w *Window, app *App) View {
		entity = NewEntity(app, w.Scope(), func(*frameLoopView, *Context[frameLoopView]) {})
		return ViewOf(entity)
	})
	if err != nil {
		t.Fatalf("OpenWindowView failed: %v — this session is expected to create real windows", err)
	}
	t.Cleanup(func() {
		if window.Alive() {
			window.Close()
			desktopWaitFor(t, "the window to be destroyed", func() bool { return !window.Alive() })
		}
	})

	var ran atomic.Bool
	h.runForegroundSync(func() {
		entity.Update(app, func(_ *frameLoopView, cx *Context[frameLoopView]) { cx.Notify() })
		window.OnNextFrame(func(*Window, *App) { ran.Store(true) })
	})
	desktopWaitFor(t, "the next-frame callback to run", ran.Load)
	time.Sleep(100 * time.Millisecond)
	var draws uint64
	h.runForegroundSync(func() { draws = WindowDrawCount(window) })
	if draws != 1 {
		t.Fatalf("draw count = %d, want 1 (only the opening draw; no present hook, no frame-loop draws)", draws)
	}
}
