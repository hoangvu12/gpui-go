//go:build windows

// Package winhostspec holds ticket05's Win32 host tests: real windows
// opened and closed in the user's interactive session. They exercise
// the public gpui API from outside the runtime package, matching the
// repo convention (see internal/taskspec).
//
// Environment expectation: these tests create and show real Win32
// windows. If CreateWindowExW fails (for example a non-interactive
// session), the tests FAIL with that error — this machine is expected
// to run them interactively; they never silently skip. Every test uses
// a fresh host and closes everything it opens (testing.Cleanup), and
// every wait is a generous poll: the CI machine is the user's PC.
package winhostspec

import (
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gpui-go/gpui"
)

// waitFor polls cond until it holds or the timeout expires. Conditions
// are cheap, non-blocking probes.
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
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

// chanReady reports whether ch has a pending value, consuming it.
// Use it only for one-shot events whose value is not read later.
func chanReady[T any](ch chan T) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// recvWithin receives one value from ch, failing the test after the
// timeout. Unlike chanReady + a separate receive, it never consumes a
// value that a later receive needs.
func recvWithin[T any](t *testing.T, timeout time.Duration, what string, ch chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(timeout):
		t.Fatalf("timed out after %v waiting for %s", timeout, what)
		var zero T
		return zero
	}
}

// expectNoLeakedGoroutines waits for the goroutine count to fall back
// near before, proving the host thread joined and no worker leaked.
// A small slack covers transient runtime/test-harness goroutines.
func expectNoLeakedGoroutines(t *testing.T, before int) {
	t.Helper()
	waitFor(t, 5*time.Second, "goroutines to drain back near the baseline", func() bool {
		return runtime.NumGoroutine() <= before+2
	})
}

// newHost boots a fresh host and stops it at cleanup, checking the
// thread join and goroutine baseline.
func newHost(t *testing.T) *gpui.Host {
	t.Helper()
	before := runtime.NumGoroutine()
	h := gpui.NewHost()
	if err := h.Start(); err != nil {
		t.Fatalf("host start failed: %v", err)
	}
	t.Cleanup(func() {
		if err := h.Stop(); err != nil {
			t.Errorf("host stop failed: %v", err)
		}
		expectNoLeakedGoroutines(t, before)
	})
	return h
}

// openShown opens one shown window, failing the test on creation errors
// (interactive-session expectation).
func openShown(t *testing.T, h *gpui.Host, opts gpui.WindowOptions) gpui.WindowHandle {
	t.Helper()
	handle, err := h.OpenWindow(opts)
	if err != nil {
		t.Fatalf("OpenWindow(%q) failed: %v — this session is expected to create real windows", opts.Title, err)
	}
	return handle
}

// TestHostBootDPIAwarenessAndShutdown checks the foreground thread
// boot, the recorded PerMonitorV2 establishment before window creation
// (the effective context is the executable mode; the set call itself
// can fail honestly when the process context was already fixed) and a
// clean stop.
func TestHostBootDPIAwarenessAndShutdown(t *testing.T) {
	h := newHost(t)

	if h.ThreadID() == 0 {
		t.Fatal("host thread id was not recorded")
	}
	if h.IsForegroundThread() {
		t.Fatal("the test goroutine must not be the foreground (host) thread")
	}

	record := h.DPIAwareness()
	t.Logf("DPI awareness record: %q", record)
	if !strings.Contains(record, "effective=per-monitor-v2") {
		t.Fatalf("process DPI awareness is not PerMonitorV2: %q", record)
	}

	if err := h.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := h.Wait(); err != nil {
		t.Fatalf("wait after stop: %v", err)
	}
}

// TestTwoWindowsAreDistinctAliveAndTitled opens two windows on one
// shared message loop, pumps until both are visible, checks distinct
// HWNDs and identities, sets and reads back titles through the OS, and
// records the machine's actual DPI scale.
func TestTwoWindowsAreDistinctAliveAndTitled(t *testing.T) {
	h := newHost(t)

	w1 := openShown(t, h, gpui.WindowOptions{
		Title:     "winhostspec one",
		Show:      true,
		Resizable: true,
		Bounds:    gpui.Bounds{Size: gpui.Size{Width: 320, Height: 200}},
	})
	w2 := openShown(t, h, gpui.WindowOptions{
		Title:  "winhostspec two",
		Show:   true,
		Bounds: gpui.Bounds{Size: gpui.Size{Width: 280, Height: 180}},
	})

	if w1.Hwnd() == 0 || w2.Hwnd() == 0 {
		t.Fatalf("zero HWND: %d %d", w1.Hwnd(), w2.Hwnd())
	}
	if w1.Hwnd() == w2.Hwnd() {
		t.Fatalf("two windows share one HWND: %d", w1.Hwnd())
	}
	if w1.ID() == w2.ID() {
		t.Fatalf("two windows share one host window id: %d", w1.ID())
	}

	// Pump until both are visible (the show commands marshal through
	// the wake queue, so by the time the queries return, the shows ran).
	waitFor(t, 10*time.Second, "window one to become visible", w1.Visible)
	waitFor(t, 10*time.Second, "window two to become visible", w2.Visible)
	if !w1.Alive() || !w2.Alive() {
		t.Fatalf("windows are not alive after show: %v %v", w1.Alive(), w2.Alive())
	}

	// Titles round-trip through the OS (SetWindowTextW/GetWindowTextW).
	w1.SetTitle("winhostspec one retitled")
	waitFor(t, 10*time.Second, "title to round-trip", func() bool {
		title, err := w1.Title()
		return err == nil && title == "winhostspec one retitled"
	})
	title2, err := w2.Title()
	if err != nil || title2 != "winhostspec two" {
		t.Fatalf("window two title: %q, err %v", title2, err)
	}

	// Bounds and scale factor: record the machine's actual values (no
	// specific number is asserted; a sane range is).
	scale1, err := w1.ScaleFactor()
	if err != nil {
		t.Fatalf("scale factor: %v", err)
	}
	t.Logf("observed window scale factor: %v (device pixels per logical pixel)", scale1)
	if scale1 <= 0 || scale1 > 10 {
		t.Fatalf("implausible scale factor: %v", scale1)
	}
	bounds1, err := w1.Bounds()
	if err != nil {
		t.Fatalf("bounds: %v", err)
	}
	t.Logf("observed window bounds: %+v", bounds1)
	if bounds1.Size.Width < 1 || bounds1.Size.Height < 1 {
		t.Fatalf("implausible bounds size: %+v", bounds1)
	}

	// Close both through the graceful (veto-less) path.
	w1.Close()
	w2.Close()
	waitFor(t, 10*time.Second, "window one to be destroyed", func() bool { return !w1.Alive() })
	waitFor(t, 10*time.Second, "window two to be destroyed", func() bool { return !w2.Alive() })

	// The last window's close quits the loop: Wait returns promptly.
	waitDone := make(chan error, 1)
	go func() { waitDone <- h.Wait() }()
	if err := recvWithin(t, 10*time.Second, "the host loop to quit after the last window closed", waitDone); err != nil {
		t.Fatalf("host Wait after quit: %v", err)
	}
}

// TestPostedWorkRunsFIFOOnForegroundThread posts a batch of work from a
// background goroutine and from the test goroutine, checking FIFO
// ordering and that every closure ran on the host (foreground) thread
// (IsForegroundThread decides through GetCurrentThreadId).
func TestPostedWorkRunsFIFOOnForegroundThread(t *testing.T) {
	h := newHost(t)

	var mu sync.Mutex
	var order []int
	var onForeground []bool

	post := func(i int) {
		h.Post(func() {
			fg := h.IsForegroundThread()
			mu.Lock()
			order = append(order, i)
			onForeground = append(onForeground, fg)
			mu.Unlock()
		})
	}

	// From a background goroutine: the wake mechanism must marshal each
	// closure onto the host thread.
	backgroundDone := make(chan struct{})
	go func() {
		defer close(backgroundDone)
		for i := 1; i <= 6; i++ {
			post(i)
		}
	}()
	<-backgroundDone
	// From the test goroutine: same queue, same FIFO position rules.
	post(7)
	post(8)

	waitFor(t, 10*time.Second, "all posted work to run", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(order) == 8
	})

	mu.Lock()
	defer mu.Unlock()
	for i, got := range order {
		if got != i+1 {
			t.Fatalf("posted work ran out of FIFO order: %v", order)
		}
	}
	for i, fg := range onForeground {
		if !fg {
			t.Fatalf("posted closure %d ran off the foreground (host) thread", i+1)
		}
	}
}

// TestCloseVetoAndIndependentWindows exercises the graceful close path
// with a veto: the first close request is vetoed (the window stays
// open), a later one is allowed (the window is destroyed through the
// veto path), and the second window keeps pumping work the whole time
// (two HWNDs, one shared message loop, no cross-window blocking). The
// final close quits the host cleanly.
func TestCloseVetoAndIndependentWindows(t *testing.T) {
	h := newHost(t)

	var allowClose atomic.Bool
	var vetoCount atomic.Int32
	vetoSeen := make(chan struct{}, 8)
	closed1 := make(chan struct{}, 8)
	closed2 := make(chan struct{}, 8)

	w1 := openShown(t, h, gpui.WindowOptions{
		Title: "winhostspec vetoed",
		Show:  true,
		Bounds: gpui.Bounds{Size: gpui.Size{
			Width:  300,
			Height: 180,
		}},
		ShouldClose: func() bool {
			vetoCount.Add(1)
			vetoSeen <- struct{}{}
			return allowClose.Load()
		},
		OnClose: func() { closed1 <- struct{}{} },
	})
	w2 := openShown(t, h, gpui.WindowOptions{
		Title: "winhostspec independent",
		Show:  true,
		Bounds: gpui.Bounds{Size: gpui.Size{
			Width:  300,
			Height: 180,
		}},
		OnClose: func() { closed2 <- struct{}{} },
	})

	// First close: vetoed, the window must stay open.
	w1.Close()
	waitFor(t, 10*time.Second, "the veto callback to run", func() bool { return chanReady(vetoSeen) })
	waitFor(t, 10*time.Second, "the vetoed window to stay alive", func() bool { return w1.Alive() })
	if !w2.Alive() {
		t.Fatal("the independent window died during a vetoed close")
	}

	// The other window stays responsive: work posted from a background
	// goroutine keeps running on the host thread.
	responsive := make(chan struct{}, 4)
	postWork := func() {
		go func() { h.Post(func() { responsive <- struct{}{} }) }()
		waitFor(t, 10*time.Second, "posted work during the veto standoff", func() bool {
			return chanReady(responsive)
		})
	}
	postWork()

	// Allow the close now: the window is destroyed through the same
	// WM_CLOSE path, OnClose fires and the lease goes stale.
	allowClose.Store(true)
	w1.Close()
	waitFor(t, 10*time.Second, "the allowed close to destroy the window", func() bool { return !w1.Alive() })
	waitFor(t, 10*time.Second, "OnClose to fire", func() bool { return chanReady(closed1) })

	// The surviving window is still responsive and alive.
	if !w2.Alive() {
		t.Fatal("the independent window died after the other window closed")
	}
	postWork()

	// A stale lease reports closed instead of pretending to work.
	if _, err := w1.Bounds(); err != gpui.ErrWindowClosed {
		t.Fatalf("stale lease Bounds error = %v, want ErrWindowClosed", err)
	}
	if _, err := w1.ScaleFactor(); err != gpui.ErrWindowClosed {
		t.Fatalf("stale lease ScaleFactor error = %v, want ErrWindowClosed", err)
	}

	// Close the last window: the host quits by itself and Wait returns.
	w2.Close()
	waitFor(t, 10*time.Second, "the last window to be destroyed", func() bool { return !w2.Alive() })
	waitFor(t, 10*time.Second, "OnClose to fire for window two", func() bool { return chanReady(closed2) })

	waitDone := make(chan error, 1)
	go func() { waitDone <- h.Wait() }()
	if err := recvWithin(t, 10*time.Second, "the host loop to quit after the last window closed", waitDone); err != nil {
		t.Fatalf("host Wait after quit: %v", err)
	}
}

// TestWindowScaleFactorRecordsActualDPI reads the created window's
// scale factor and records it; the test never asserts a specific
// number, only that a positive, plausible value is reported (the
// machine's actual per-monitor DPI).
func TestWindowScaleFactorRecordsActualDPI(t *testing.T) {
	h := newHost(t)
	w := openShown(t, h, gpui.WindowOptions{
		Title:  "winhostspec dpi probe",
		Show:   true,
		Bounds: gpui.Bounds{Size: gpui.Size{Width: 240, Height: 160}},
	})

	scale, err := w.ScaleFactor()
	if err != nil {
		t.Fatalf("scale factor: %v", err)
	}
	t.Logf("machine DPI scale factor: %v (%.0f DPI)", scale, scale*96)
	if scale <= 0 || scale > 10 {
		t.Fatalf("implausible scale factor: %v", scale)
	}
	if got := h.DPIAwareness(); !strings.Contains(got, "per-monitor-v2") {
		t.Logf("warning: DPI record is %q", got)
	}

	// Hide/show round-trip keeps the lease working.
	w.Hide()
	waitFor(t, 10*time.Second, "window to hide", func() bool { return !w.Visible() })
	w.Show()
	waitFor(t, 10*time.Second, "window to show again", func() bool { return w.Visible() })
}

// mark is the global type used to prove that window events route into
// the application's foreground dispatch with effect flushing.
type mark struct{ n int }

// TestPostedWorkStressNoLostWakes is the lost-wake regression: one
// thousand posts from four goroutines while the host drains
// concurrently. Every closure must run: a lost wake-up (a post
// coalesced behind a wake message that is never dispatched, or a
// stranded queue entry) leaves executed below N and fails the wait.
func TestPostedWorkStressNoLostWakes(t *testing.T) {
	h := newHost(t)

	const (
		total   = 1000
		workers = 4
	)
	var executed atomic.Int64
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < total/workers; i++ {
				h.Post(func() { executed.Add(1) })
			}
		}()
	}
	wg.Wait()
	waitFor(t, 15*time.Second, "all posted closures to execute on the host thread", func() bool {
		return executed.Load() == total
	})
	if got := executed.Load(); got != total {
		t.Fatalf("executed %d of %d posts: lost wake-up", got, total)
	}
}

// TestAppWindowLifecycleThroughHost drives the real-application boot
// path: NewApp + Attach + Start + Run in a goroutine; windows open
// through the host; a resize event routes through App.Update (a global
// observer fires inside the same flush); a foreground task spawned from
// a background goroutine runs its App.Update on the host thread;
// closing one window retires only that window's scope and registry
// entry while the other stays alive; the last close returns Run.
func TestAppWindowLifecycleThroughHost(t *testing.T) {
	before := runtime.NumGoroutine()
	app := gpui.NewApp()
	h := gpui.NewHost()
	if err := app.Attach(h); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if err := h.Start(); err != nil {
		t.Fatalf("host start: %v", err)
	}
	t.Cleanup(func() {
		if err := h.Stop(); err != nil {
			t.Errorf("host stop: %v", err)
		}
		expectNoLeakedGoroutines(t, before)
	})

	runDone := make(chan error, 1)
	go func() { runDone <- app.Run(h) }()

	// App access is marshaled to the foreground thread (the host
	// thread); the application is single-foreground-thread by design.
	// A dropped post fails loudly instead of hanging the waiter.
	appSync := func(f func()) {
		done := make(chan struct{})
		if !h.Post(func() {
			defer close(done)
			f()
		}) {
			t.Fatalf("appSync post was dropped: the host loop exited")
		}
		<-done
	}
	windowIDs := func() []gpui.WindowID {
		var out []gpui.WindowID
		appSync(func() { out = app.Windows() })
		return out
	}

	// Register a global whose replacement (from a window event) proves
	// event routing through the app's foreground update machinery.
	var observerFired atomic.Bool
	appSync(func() {
		app.SetGlobal(mark{0})
		app.ObserveGlobal[mark](func(*gpui.App) { observerFired.Store(true) })
	})

	resized := make(chan struct{}, 8)
	w1, err := app.OpenWindow(gpui.WindowOptions{
		Title:     "winhostspec app one",
		Show:      true,
		Resizable: true,
		Bounds:    gpui.Bounds{Size: gpui.Size{Width: 360, Height: 240}},
		OnResize: func(size gpui.Size, scale float32) {
			app.SetGlobal(mark{1})
			resized <- struct{}{}
		},
	})
	if err != nil {
		t.Fatalf("app.OpenWindow: %v — this session is expected to create real windows", err)
	}
	w2, err := app.OpenWindow(gpui.WindowOptions{
		Title:  "winhostspec app two",
		Show:   true,
		Bounds: gpui.Bounds{Size: gpui.Size{Width: 300, Height: 200}},
	})
	if err != nil {
		t.Fatalf("app.OpenWindow(second): %v", err)
	}

	if w1.ID() == w2.ID() {
		t.Fatalf("app windows share an identity: %d", w1.ID())
	}
	ids := windowIDs()
	if len(ids) != 2 || ids[0] != w1.ID() || ids[1] != w2.ID() {
		t.Fatalf("app window registry = %v, want [%d %d]", ids, w1.ID(), w2.ID())
	}
	scopeState := func(w *gpui.Window) gpui.ScopeState {
		var st gpui.ScopeState
		appSync(func() { st = w.Scope().State() })
		return st
	}
	if got := scopeState(w1); got != gpui.ScopeOpen {
		t.Fatalf("window one scope state = %v, want open", got)
	}

	// Focus handle stub storage (ticket13 seam).
	fh := gpui.NewFocusHandle(w1)
	if fh.WindowID() != w1.ID() {
		t.Fatalf("focus handle window = %d, want %d", fh.WindowID(), w1.ID())
	}
	if _, ok := w1.FocusHandleByID(fh.ID()); !ok {
		t.Fatal("focus handle not stored in its window")
	}

	// Window events route through the app's foreground dispatch: the
	// resize callback runs inside App.Update and its SetGlobal flushes
	// to the observer in the same cycle. Creation-time resize events
	// (from the initial placement) are drained first so the wait below
	// observes the SetBounds-triggered one.
	for chanReady(resized) {
	}
	w1.SetBounds(gpui.Bounds{Size: gpui.Size{Width: 420, Height: 300}})
	waitFor(t, 10*time.Second, "the resize event to route into the app", func() bool {
		return chanReady(resized)
	})
	if !observerFired.Load() {
		t.Fatal("the global observer did not fire during the resize event's update")
	}

	// Foreground task spawned from a background goroutine (this test
	// goroutine): the scheduler wakes the host and the body's
	// App.Update executes on the host thread.
	var updateOnForeground atomic.Bool
	task := app.Spawn(func(cx *gpui.AsyncApp) int {
		cx.Update(func(*gpui.App) { updateOnForeground.Store(h.IsForegroundThread()) })
		return 7
	})
	waitFor(t, 10*time.Second, "the foreground task to complete", task.Done)
	if !updateOnForeground.Load() {
		t.Fatal("the spawned task's App.Update did not run on the host (foreground) thread")
	}
	if task.Failed() || task.PanicValue() != nil {
		t.Fatalf("foreground task failed: %v", task.PanicValue())
	}

	// Close window one through the veto-less path: its scope closes and
	// its registry entry is removed, while window two is untouched.
	w1.Close()
	waitFor(t, 10*time.Second, "app window one to be destroyed", func() bool { return !w1.Alive() })
	waitFor(t, 10*time.Second, "window one's scope to close", func() bool {
		return scopeState(w1) == gpui.ScopeClosed
	})
	waitFor(t, 10*time.Second, "the registry to drop window one", func() bool {
		return len(windowIDs()) == 1
	})
	if !w2.Alive() {
		t.Fatal("app window two died when window one closed")
	}

	// The surviving app window's host keeps pumping work.
	workRan := make(chan struct{}, 2)
	go func() { h.Post(func() { workRan <- struct{}{} }) }()
	waitFor(t, 10*time.Second, "the host to keep pumping after one app window closed", func() bool {
		return chanReady(workRan)
	})

	// Close the last window: the app quits and Run returns nil.
	w2.Close()
	waitFor(t, 10*time.Second, "app window two to be destroyed", func() bool { return !w2.Alive() })
	if err := recvWithin(t, 10*time.Second, "Run to return after the last window closed", runDone); err != nil {
		t.Fatalf("app.Run returned %v", err)
	}
}
