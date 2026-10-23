//go:build windows

package gpui

import (
	"runtime"
	"sort"
	"strings"
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
	// then idle. Every frame-source wake from here on is attributed to its
	// origin (Base ticket 34's hardening): frames the test did not cause —
	// an OS activation report (window.rs:1887-1902), a WM_SIZE refresh, a
	// real mouse over the window, a UIA query's activation edge, all
	// arriving through the window procedure — are tolerated one frame per
	// attributed wake; a wake of any other origin while idle fails.
	ledger := newFrameWakeLedger()
	if !h.runForegroundSync(func() { frameStateOf(window).onWake = ledger.record }) {
		t.Fatal("the host stopped")
	}
	t.Cleanup(func() { h.runForegroundSync(func() { frameStateOf(window).onWake = nil }) })
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
	// Idle means idle: no further frames are drawn unless the platform
	// caused them.
	settled := read()
	idleMark := ledger.mark()
	time.Sleep(250 * time.Millisecond)
	final := read()
	idleExternal, idleOther := ledger.since(idleMark)
	if len(idleOther) != 0 {
		t.Fatalf("an idle window was woken by a non-platform origin: %v", idleOther)
	}
	if idle := final.draws - settled.draws; idle > uint64(idleExternal) {
		t.Fatalf("an idle window drew %d frames, only %d attributable to platform events", idle, idleExternal)
	}
	if idle := final.presents - settled.presents; idle > int64(idleExternal) {
		t.Fatalf("an idle window kept presenting (%d -> %d, %d platform wakes)", settled.presents, final.presents, idleExternal)
	}
	external, _ := ledger.since(0)
	if settled.draws < want || settled.draws > want+uint64(external) {
		t.Fatalf("animation draws = %d, want exactly %d (+%d platform-caused wakes: %v)", settled.draws-afterNotify.draws, 5, external, ledger.origins())
	}
	if final.pending != 0 || final.demand {
		t.Fatalf("idle window still has frame demand (pending callbacks %d, demand %v)", final.pending, final.demand)
	}
}

// TestFrameLoopMinimizedWindowSkipsFramesUntilVisible is browser-core
// ticket 03's hidden-window frame gate (research browser-parity 02
// §8 gap 1; Chromium's display scheduler draws only for visible
// windows, display_scheduler.cc:602-607): a minimized window SCHEDULES
// NO FRAMES while its frame demand is pending, and an animation that
// started while hidden RESUMES correctly — every remaining frame drawn
// — once the window is visible again.
//
// The window is real but parked far offscreen (no visible window on
// any monitor), and the minimize/restore pair is driven with synthetic
// WM_SIZE messages on the host thread — the exact message a real
// minimize delivers (events.rs handle_size_msg). No real input.
func TestFrameLoopMinimizedWindowSkipsFramesUntilVisible(t *testing.T) {
	h := desktopNewHost(t)
	app := NewApp()
	if err := app.Attach(h); err != nil {
		t.Fatalf("app attach: %v", err)
	}
	var presents atomic.Int64
	var entity Entity[frameLoopView]
	window, err := app.OpenWindowView(WindowOptions{
		Title:    "gpui frame loop minimized",
		Bounds:   Bounds{Origin: Point{X: 20000, Y: 20000}, Size: Size{Width: 240, Height: 160}},
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

	type facts struct {
		draws    uint64
		renders  int
		demand   bool
		presents int64
	}
	read := func() facts {
		var f facts
		if !h.runForegroundSync(func() {
			f.draws = WindowDrawCount(window)
			f.renders = frameLoopRenders(app, entity)
			f.demand = window.hasFrameDemand()
		}) {
			t.Fatal("the host stopped")
		}
		f.presents = presents.Load()
		return f
	}

	// The opening frame is drawn and presented; the window goes idle.
	desktopWaitFor(t, "the opening frame to be presented", func() bool { return presents.Load() >= 1 })
	desktopWaitFor(t, "the window to go idle after opening", func() bool { return !read().demand })
	base := read()

	// Minimize: the WM_SIZE a real minimize delivers.
	h.runForegroundSync(func() {
		w := window.Handle().record()
		if w == nil {
			t.Fatal("no live window record")
		}
		procSendMessageW.Call(w.hwnd, uintptr(wmSize), uintptr(sizeMinimized), 0)
	})

	// An animation starts while the window is minimized.
	h.runForegroundSync(func() {
		entity.Update(app, func(v *frameLoopView, cx *Context[frameLoopView]) {
			v.animate = 4
			cx.Notify()
		})
	})

	// Hidden means no frames: the demand stays pending, nothing draws
	// or presents.
	time.Sleep(400 * time.Millisecond)
	hidden := read()
	if hidden.draws != base.draws {
		t.Errorf("a minimized window drew %d frame(s) (draws %d -> %d)", hidden.draws-base.draws, base.draws, hidden.draws)
	}
	if hidden.presents != base.presents {
		t.Errorf("a minimized window presented %d frame(s)", hidden.presents-base.presents)
	}
	if !hidden.demand {
		t.Fatal("the window has no pending frame demand; the animation was lost while minimized")
	}

	// Restore: the WM_SIZE a real restore delivers, with the client
	// size the minimize kept.
	h.runForegroundSync(func() {
		w := window.Handle().record()
		if w == nil {
			t.Fatal("no live window record")
		}
		procSendMessageW.Call(w.hwnd, uintptr(wmSize), 0,
			uintptr(uint16(int16(w.clientW)))|uintptr(uint16(int16(w.clientH)))<<16)
	})

	// The animation resumes: every remaining frame is drawn, then the
	// window goes idle again.
	desktopWaitFor(t, "the animation to resume after the restore", func() bool {
		f := read()
		return f.draws >= base.draws+4 && f.renders >= base.renders+4
	})
	desktopWaitFor(t, "the window to go idle after resuming", func() bool { return !read().demand })
	final := read()
	if final.presents <= base.presents {
		t.Errorf("the resumed animation never presented (presents %d -> %d)", base.presents, final.presents)
	}
	if final.renders < base.renders+4 {
		t.Fatalf("the animation completed only %d of 4 frames after the restore", final.renders-base.renders)
	}
}

// TestFrameLoopPreferredFrameIntervalDrawsEveryOtherVblank is
// browser-core ticket 47's per-window preferred frame interval (research
// browser-parity 02 §8, the port of viz's half-rate preferred interval,
// components/viz/service/frame_sinks/external_begin_frame_source_win.cc:
// SetPreferredInterval :96-110 deciding half rate from the requested
// interval, OnVSyncOnSequence :53-62 skipping every other vsync while
// it runs): a window that asks for a lower rate draws every OTHER
// vblank during an animation, while a full-rate window on the same
// pacer keeps drawing every vblank — both windows are real but parked
// far offscreen (no visible window on any monitor), the animations are
// driven purely by frame requests, and the full-rate window doubles as
// the pacer's vblank clock (its draw gaps measure the interval the
// half-rate gaps are compared against, so the assertion holds at any
// display refresh rate). No real input.
func TestFrameLoopPreferredFrameIntervalDrawsEveryOtherVblank(t *testing.T) {
	h := desktopNewHost(t)
	app := NewApp()
	if err := app.Attach(h); err != nil {
		t.Fatalf("app attach: %v", err)
	}

	// drawTimes records when a window's present hook ran.
	type drawTimes struct {
		mu    sync.Mutex
		times []time.Time
	}
	hook := func(l *drawTimes) func() {
		return func() {
			l.mu.Lock()
			l.times = append(l.times, time.Now())
			l.mu.Unlock()
		}
	}
	times := func(l *drawTimes) []time.Time {
		l.mu.Lock()
		defer l.mu.Unlock()
		return append([]time.Time(nil), l.times...)
	}
	gaps := func(l *drawTimes, since time.Time) []time.Duration {
		var ts []time.Time
		for _, at := range times(l) {
			if !at.Before(since) {
				ts = append(ts, at)
			}
		}
		var out []time.Duration
		for i := 1; i < len(ts); i++ {
			out = append(out, ts[i].Sub(ts[i-1]))
		}
		return out
	}
	median := func(ds []time.Duration) time.Duration {
		sorted := append([]time.Duration(nil), ds...)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
		return sorted[len(sorted)/2]
	}

	controlLog, halfLog := &drawTimes{}, &drawTimes{}
	var controlEntity, halfEntity Entity[frameLoopView]
	open := func(title string, originX, originY float32, preferred time.Duration, log *drawTimes, entity *Entity[frameLoopView]) *Window {
		window, err := app.OpenWindowView(WindowOptions{
			Title:  title,
			Bounds: Bounds{Origin: Point{X: originX, Y: originY}, Size: Size{Width: 240, Height: 160}},
			Show:   true,
			// A preferred interval above twice the observed vblank interval
			// minus half a millisecond (about 33 ms on a 60 Hz display)
			// selects half rate (external_begin_frame_source_win.cc:97-101).
			Activate:               false,
			PreferredFrameInterval: preferred,
			OnRedraw:               hook(log),
		}, func(w *Window, app *App) View {
			*entity = NewEntity(app, w.Scope(), func(*frameLoopView, *Context[frameLoopView]) {})
			return ViewOf(*entity)
		})
		if err != nil {
			t.Fatalf("OpenWindowView failed: %v — this session is expected to create real windows", err)
		}
		return window
	}
	control := open("gpui frame loop full rate", 20000, 20000, 0, controlLog, &controlEntity)
	half := open("gpui frame loop half rate", 20060, 20060, 40*time.Millisecond, halfLog, &halfEntity)
	t.Cleanup(func() {
		for _, window := range []*Window{control, half} {
			if window.Alive() {
				window.Close()
				desktopWaitFor(t, "the window to be destroyed", func() bool { return !window.Alive() })
			}
		}
	})

	type facts struct {
		controlDraws, halfDraws uint64
		demand                  bool
	}
	read := func() facts {
		var f facts
		if !h.runForegroundSync(func() {
			f.controlDraws = WindowDrawCount(control)
			f.halfDraws = WindowDrawCount(half)
			f.demand = control.hasFrameDemand() || half.hasFrameDemand()
		}) {
			t.Fatal("the host stopped")
		}
		return f
	}

	// Both opening frames are presented; both windows go idle.
	desktopWaitFor(t, "both opening frames to be presented", func() bool {
		return len(times(controlLog)) >= 1 && len(times(halfLog)) >= 1
	})
	desktopWaitFor(t, "both windows to go idle after opening", func() bool { return !read().demand })
	base := read()

	// Both windows animate: 40 animation steps make 41 drawn frames.
	const animationFrames = 41
	animStart := time.Now()
	h.runForegroundSync(func() {
		controlEntity.Update(app, func(v *frameLoopView, cx *Context[frameLoopView]) {
			v.animate = 40
			cx.Notify()
		})
		halfEntity.Update(app, func(v *frameLoopView, cx *Context[frameLoopView]) {
			v.animate = 40
			cx.Notify()
		})
	})

	// The full-rate window finishing is the observation point: by then a
	// half-rate window has drawn about half as many frames.
	desktopWaitFor(t, "the full-rate animation to finish", func() bool {
		return read().controlDraws >= base.controlDraws+animationFrames
	})
	halfAtFullRateEnd := read().halfDraws - base.halfDraws

	// The half-rate animation completes too — the skipped slots keep its
	// demand pending, so no frame is lost — and both windows go idle.
	desktopWaitFor(t, "the half-rate animation to finish", func() bool {
		return read().halfDraws >= base.halfDraws+animationFrames
	})
	desktopWaitFor(t, "both windows to go idle after the animations", func() bool { return !read().demand })

	controlGaps := gaps(controlLog, animStart)
	halfGaps := gaps(halfLog, animStart)
	if len(controlGaps) < animationFrames-1 || len(halfGaps) < animationFrames-1 {
		t.Fatalf("not enough animation frames recorded: %d full-rate and %d half-rate gaps (want %d each)",
			len(controlGaps), len(halfGaps), animationFrames-1)
	}
	controlMedian := median(controlGaps)
	halfMedian := median(halfGaps)

	// The full-rate window is the pacer's vblank clock: its frames are
	// paced (an unpaced loop measured 0.23 ms/frame) but not throttled.
	if controlMedian < time.Millisecond || controlMedian > 50*time.Millisecond {
		t.Errorf("full-rate median frame gap = %v, want between 1ms and 50ms (paced by the vblank source)", controlMedian)
	}
	// Half rate: the median gap doubles, and no two half-rate draws ever
	// land on consecutive vblanks.
	ratio := float64(halfMedian) / float64(controlMedian)
	if ratio < 1.6 || ratio > 2.6 {
		t.Errorf("half-rate median gap = %v, full-rate median gap = %v (ratio %.2f, want about 2)", halfMedian, controlMedian, ratio)
	}
	// No two half-rate draws land on consecutive vblanks: a gap at or
	// under about one vblank (the control's median, with room for a fast
	// slot pair) can only be measurement noise, so at most a couple may
	// appear — a window actually drawing at full rate would show about
	// half of its gaps there. The structural every-other-slot rule is
	// pinned exactly by TestFramePacerGrantFrameSlotAlternates.
	closeGaps := 0
	for _, g := range halfGaps {
		if g < controlMedian*5/4 {
			closeGaps++
		}
	}
	if closeGaps > 2 {
		t.Errorf("%d of %d half-rate gaps are under 1.25x the vblank interval (%v): the window drew on consecutive vblanks", closeGaps, len(halfGaps), controlMedian)
	}
	// Over the full-rate window's animation the half-rate window drew
	// about half as many frames.
	if halfAtFullRateEnd < 12 || halfAtFullRateEnd > 28 {
		t.Errorf("the half-rate window drew %d frames while the full-rate one drew %d, want about half", halfAtFullRateEnd, animationFrames)
	}
}

// TestWindowOpenRejectsNegativePreferredFrameInterval pins the open-time
// guard of WindowOptions.PreferredFrameInterval (browser-core ticket
// 47): a negative interval is rejected before any window is created —
// the call fails without ever touching the screen.
func TestWindowOpenRejectsNegativePreferredFrameInterval(t *testing.T) {
	h := desktopNewHost(t)
	app := NewApp()
	if err := app.Attach(h); err != nil {
		t.Fatalf("app attach: %v", err)
	}
	if _, err := app.OpenWindow(WindowOptions{PreferredFrameInterval: -time.Millisecond}); err == nil {
		t.Fatal("OpenWindow must reject a negative PreferredFrameInterval")
	}
}

// TestFramePacerGrantFrameSlotAlternates is the unit half of ticket 47's
// frame rule (the real-window half is
// TestFrameLoopPreferredFrameIntervalDrawsEveryOtherVblank): with a
// measured vblank interval, a preferred interval above twice it minus
// half a millisecond engages half rate (external_begin_frame_source_
// win.cc:97-101) and grants alternate SLOTS — skip, grant, skip — while
// the first frame of a session and a window that missed a slot are
// served at once. Full-rate windows are never held back.
func TestFramePacerGrantFrameSlotAlternates(t *testing.T) {
	ta := NewTestApp()
	half, _ := openFrameLoopTestWindow(t, ta)
	full, _ := openFrameLoopTestWindow(t, ta)
	half.SetPreferredFrameInterval(40 * time.Millisecond)

	p := &framePacer{}
	p.vsyncEstimate.Store(int64(4 * time.Millisecond))

	type step struct {
		slot     int64
		halfWant bool
		fullWant bool
	}
	// Slots 1..6: the half-rate window is granted at 1, skipped at 2,
	// granted at 3, and so on; the full-rate window every slot.
	steps := []step{
		{1, true, true}, {2, false, true}, {3, true, true},
		{4, false, true}, {5, true, true}, {6, false, true},
	}
	for _, s := range steps {
		if got := p.grantFrameSlot(half, s.slot); got != s.halfWant {
			t.Errorf("half-rate grant at slot %d = %v, want %v", s.slot, got, s.halfWant)
		}
		if got := p.grantFrameSlot(full, s.slot); got != s.fullWant {
			t.Errorf("full-rate grant at slot %d = %v, want %v", s.slot, got, s.fullWant)
		}
	}

	// A window that missed a slot mid-flight (slot 7, not pending) does
	// not fall a slot behind: the next slot it asks for is granted.
	if got := p.grantFrameSlot(half, 9); !got {
		t.Error("a half-rate window that missed one slot must be granted the next one")
	}
	// A session restarting long after idle (slot 40): granted at once,
	// like the source's missed begin frame.
	if got := p.grantFrameSlot(half, 40); !got {
		t.Error("the first slot of a new session must be granted")
	}
	if got := p.grantFrameSlot(half, 41); got {
		t.Error("the slot right after a grant must be skipped")
	}

	// A preferred interval under the half-rate threshold keeps the full
	// rate (only full and half rate exist,
	// external_begin_frame_source_win.cc:96-110).
	under := &framePacer{}
	under.vsyncEstimate.Store(int64(4 * time.Millisecond))
	slow, _ := openFrameLoopTestWindow(t, ta)
	slow.SetPreferredFrameInterval(5 * time.Millisecond)
	for slot := int64(1); slot <= 4; slot++ {
		if got := under.grantFrameSlot(slow, slot); !got {
			t.Errorf("a below-threshold interval was skipped at slot %d", slot)
		}
	}

	// Without a measured interval the reference default (16.666 ms)
	// decides: 40 ms is above 2x16.666 ms - 0.5 ms, still half rate.
	fresh := &framePacer{}
	if got := fresh.vsyncInterval(); got != frameDefaultVsyncInterval {
		t.Fatalf("an unmeasured pacer's interval = %v, want the reference default", got)
	}
	def, _ := openFrameLoopTestWindow(t, ta)
	def.SetPreferredFrameInterval(40 * time.Millisecond)
	if got := fresh.grantFrameSlot(def, 1); !got {
		t.Error("the first slot must be granted before any skipping")
	}
	if got := fresh.grantFrameSlot(def, 2); got {
		t.Error("the default-interval threshold must engage half rate too")
	}
}

// frameWakeLedger attributes a real window's frame-source wakes to their
// origin by the host-thread call stack (frameloop.go's onWake seam):
//   - "frame": the frame loop re-arming itself (Window.requestFrame);
//   - "activation": the OS activation forwarding (Window.activationChanged,
//     posted by the host's WM_ACTIVATE handler);
//   - "platform": any other wake raised while the window procedure handles
//     a message (WM_SIZE, mouse input, WM_GETOBJECT, ...);
//   - anything else is the test's own (or an unexplained) wake.
type frameWakeLedger struct {
	mu      sync.Mutex
	entries []string
}

func newFrameWakeLedger() *frameWakeLedger { return &frameWakeLedger{} }

// record classifies one wake; it runs on the host thread.
func (l *frameWakeLedger) record() {
	pcs := make([]uintptr, 64)
	n := runtime.Callers(2, pcs)
	frames := runtime.CallersFrames(pcs[:n])
	origin := ""
	var top []string
	for {
		frame, more := frames.Next()
		fn := frame.Function
		if len(top) < 4 {
			top = append(top, fn[strings.LastIndex(fn, "/")+1:])
		}
		switch {
		case strings.HasSuffix(fn, ".(*Window).requestFrame"):
			origin = "frame"
		case strings.HasSuffix(fn, ".(*Window).activationChanged"):
			origin = "activation"
		case strings.HasSuffix(fn, ".windowWndProc") && origin == "":
			origin = "platform"
		}
		if origin == "frame" || origin == "activation" || !more {
			break
		}
	}
	if origin == "" {
		origin = "other:" + strings.Join(top, "<")
	}
	l.mu.Lock()
	l.entries = append(l.entries, origin)
	l.mu.Unlock()
}

// mark returns the ledger position.
func (l *frameWakeLedger) mark() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.entries)
}

// since counts the platform-caused wakes (activation + platform) after
// the mark and returns the unexplained ones.
func (l *frameWakeLedger) since(mark int) (external int, other []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, origin := range l.entries[mark:] {
		switch {
		case origin == "activation" || origin == "platform":
			external++
		case strings.HasPrefix(origin, "other:"):
			other = append(other, origin)
		}
	}
	return external, other
}

// origins returns every recorded origin.
func (l *frameWakeLedger) origins() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.entries...)
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
