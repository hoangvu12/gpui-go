//go:build windows

package gpui

import (
	"sync"
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

	var presents, activations atomic.Int64
	var entity Entity[frameLoopView]
	window, err := app.OpenWindowView(WindowOptions{
		Title:      "gpui frame loop",
		Bounds:     Bounds{Origin: Point{X: 60, Y: 60}, Size: Size{Width: 240, Height: 160}},
		Show:       true,
		Activate:   false,
		OnRedraw:   func() { presents.Add(1) },
		OnActivate: func(bool) { activations.Add(1) },
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
	activationsBefore := activations.Load()
	desktopWaitFor(t, "the animation frames to be drawn", func() bool { return read().draws >= want })
	desktopWaitFor(t, "the animation loop to go idle", func() bool {
		f := read()
		return !f.demand && f.pending == 0
	})
	// Idle means idle: no further frames are drawn.
	settled := read()
	time.Sleep(250 * time.Millisecond)
	final := read()
	// An OS activation change refreshes the window (window.rs:1887-1902,
	// forwarded by OpenWindow since Base ticket 33), so a window activated
	// by the shell mid-test may draw one extra frame per activation report.
	extra := uint64(activations.Load() - activationsBefore)
	if final.draws != settled.draws || final.draws < want || final.draws > want+extra {
		t.Fatalf("animation draws = %d (settled %d), want exactly %d (+%d activation refreshes)", final.draws-afterNotify.draws, settled.draws-afterNotify.draws, 5, extra)
	}
	if final.presents != settled.presents {
		t.Fatalf("an idle window kept presenting (%d -> %d)", settled.presents, final.presents)
	}
	if final.pending != 0 || final.demand {
		t.Fatalf("idle window still has frame demand (pending callbacks %d, demand %v)", final.pending, final.demand)
	}
}

// TestFrameLoopRealWindowWithoutPresentHookDraws checks the default-on
// frame loop for a real window without a present hook (Base ticket 33;
// request_frame window.rs:1715-1843 runs for every window): a
// notification of the rendered view redraws the window through its
// WM_PAINT frame request with no explicit DrawWindowFrame, and the
// window's next-frame callbacks are delivered by the same requests.
//
// Migrated from TestFrameLoopRealWindowWithoutPresentHookKeepsExplicitDraws
// (ticket 03's opt-in rule: no hook, no frame-loop draws). It still
// proves the next-frame delivery without a hook and that the window draws
// exactly the demanded frames (the opening draw plus the notify frame),
// now under the upstream draw-without-hook semantics.
func TestFrameLoopRealWindowWithoutPresentHookDraws(t *testing.T) {
	h := desktopNewHost(t)
	app := NewApp()
	if err := app.Attach(h); err != nil {
		t.Fatalf("app attach: %v", err)
	}
	var activations atomic.Int64
	var entity Entity[frameLoopView]
	window, err := app.OpenWindowView(WindowOptions{
		Title:      "gpui frame loop (no present hook)",
		Bounds:     Bounds{Origin: Point{X: 80, Y: 80}, Size: Size{Width: 200, Height: 120}},
		Show:       true,
		OnActivate: func(bool) { activations.Add(1) },
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
	idle := func() bool {
		var demand bool
		h.runForegroundSync(func() { demand = window.hasFrameDemand() })
		return !demand
	}
	desktopWaitFor(t, "the window to go idle after opening", idle)
	before := activations.Load()

	var ran atomic.Bool
	h.runForegroundSync(func() {
		entity.Update(app, func(_ *frameLoopView, cx *Context[frameLoopView]) { cx.Notify() })
		window.OnNextFrame(func(*Window, *App) { ran.Store(true) })
	})
	desktopWaitFor(t, "the next-frame callback to run", ran.Load)
	desktopWaitFor(t, "the window to go idle after the notify frame", idle)
	time.Sleep(100 * time.Millisecond)
	var draws uint64
	var renders int
	h.runForegroundSync(func() {
		draws = WindowDrawCount(window)
		renders = frameLoopRenders(app, entity)
	})
	extra := uint64(activations.Load() - before)
	if draws < 2 || draws > 2+extra {
		t.Fatalf("draw count = %d, want 2 (the opening draw + the notify frame; +%d activation refreshes)", draws, extra)
	}
	if renders != int(draws) {
		t.Fatalf("renders = %d, want one per drawn frame (%d)", renders, draws)
	}
}

// activationLog is a root view recording the window activation observer.
type activationLog struct {
	log *[]string
}

func (a *activationLog) Render(w *Window, cx *Context[activationLog]) AnyElement {
	return Div().SizeFull().IntoElement()
}

// TestRealWindowActivationReachesObservers checks the OS activation
// forwarding (Base ticket 33 for ticket 09; the platform
// on_active_status_change callback, window.rs:1887-1902): a WM_ACTIVATE
// report on a REAL window updates the window's cached activation, runs
// the ObserveWindowActivation observers and refreshes the window, before
// the user's OnActivate hook runs. The report is delivered with
// SendMessage(WM_ACTIVATE) to the window itself — no foreground change, no
// input simulation.
func TestRealWindowActivationReachesObservers(t *testing.T) {
	h := desktopNewHost(t)
	app := NewApp()
	if err := app.Attach(h); err != nil {
		t.Fatalf("app attach: %v", err)
	}
	var mu sync.Mutex
	var log []string
	record := func(s string) {
		mu.Lock()
		defer mu.Unlock()
		log = append(log, s)
	}
	snapshot := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), log...)
	}
	window, err := app.OpenWindowView(WindowOptions{
		Title:  "gpui activation forwarding",
		Bounds: Bounds{Origin: Point{X: 100, Y: 100}, Size: Size{Width: 200, Height: 120}},
		Show:   true,
		OnActivate: func(active bool) {
			if active {
				record("hook:active")
			} else {
				record("hook:inactive")
			}
		},
	}, func(w *Window, app *App) View {
		return ViewOf(NewEntity(app, w.Scope(), func(_ *activationLog, cx *Context[activationLog]) {
			cx.ObserveWindowActivation(w, func(_ *activationLog, w *Window, _ *Context[activationLog]) {
				if w.isWindowActiveCached() {
					record("observer:active")
				} else {
					record("observer:inactive")
				}
			})
		}))
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
	// Let any shell-driven activation reports of the opening settle.
	time.Sleep(150 * time.Millisecond)

	send := func(state uintptr) []string {
		mu.Lock()
		log = nil
		mu.Unlock()
		var draws uint64
		h.runForegroundSync(func() { draws = WindowDrawCount(window) })
		h.runForegroundSync(func() {
			procSendMessageW.Call(window.Handle().Hwnd(), uintptr(wmActivate), state, 0)
		})
		desktopWaitFor(t, "the activation report to reach the hook", func() bool { return len(snapshot()) >= 2 })
		// The refresh redraws the window through its frame request.
		desktopWaitFor(t, "the activation refresh to redraw the window", func() bool {
			var now uint64
			h.runForegroundSync(func() { now = WindowDrawCount(window) })
			return now > draws
		})
		return snapshot()
	}

	// pairs checks the log is observer-then-hook pairs of one state (the
	// shell may follow a WA_ACTIVE with its own activation report).
	pairs := func(got []string, state string) bool {
		if len(got) < 2 || len(got)%2 != 0 {
			return false
		}
		for i := 0; i < len(got); i += 2 {
			if got[i] != "observer:"+state || got[i+1] != "hook:"+state {
				return false
			}
		}
		return true
	}
	got := send(waInactive)
	if !pairs(got, "inactive") {
		t.Fatalf("deactivation log = %v, want [observer:inactive hook:inactive]", got)
	}
	var active bool
	h.runForegroundSync(func() { active = window.isWindowActiveCached() })
	if active {
		t.Fatal("the window's cached activation did not follow the deactivation")
	}
	got = send(1) // WA_ACTIVE
	if !pairs(got, "active") {
		t.Fatalf("activation log = %v, want [observer:active hook:active]", got)
	}
}
