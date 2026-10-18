//go:build windows

package gpui

import (
	"testing"
	"time"
)

// Real-host wall-clock timer tests (ticket base/04). They boot the real
// Win32 host (newAppWithHost); a host that cannot start FAILS the test.

// onForeground runs f on the host thread and waits for it.
func onForeground(t *testing.T, h *Host, f func()) {
	t.Helper()
	done := make(chan struct{})
	if !h.Post(func() { defer close(done); f() }) {
		t.Fatal("host rejected the foreground post")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("foreground post did not run within 5s")
	}
}

type wallSleepResult struct {
	elapsed  time.Duration
	nowStart time.Duration
	nowEnd   time.Duration
}

// TestClockRealHostSleepWakes: a 50ms TaskRun.Sleep in a real app wakes
// on wall time within tolerance, its foreground awaiter is woken onto the
// host thread, and App.Now advanced by at least the sleep (reference
// BackgroundExecutor::timer, executor.rs:193-202, completing through the
// Windows dispatch_after threadpool timer, gpui_windows/src/
// dispatcher.rs:73-89,147-149).
func TestClockRealHostSleepWakes(t *testing.T) {
	app, h := newAppWithHost(t)
	if !app.sched.realtime() {
		t.Fatal("attached app scheduler is not on the wall clock")
	}
	results := make(chan wallSleepResult, 1)
	onForeground(t, h, func() {
		bg := app.Background().Spawn(func(run *TaskRun) time.Duration {
			start := time.Now()
			run.Sleep(50 * time.Millisecond)
			return time.Since(start)
		})
		nowStart := app.Now()
		app.Spawn(func(cx *AsyncApp) struct{} {
			elapsed := cx.Await(bg)
			var nowEnd time.Duration
			cx.Update(func(a *App) {
				if !h.IsForegroundThread() {
					t.Error("timer continuation update ran off the host thread")
				}
				nowEnd = a.Now()
			})
			results <- wallSleepResult{elapsed: elapsed, nowStart: nowStart, nowEnd: nowEnd}
			return struct{}{}
		}).Detach()
	})
	var r wallSleepResult
	select {
	case r = <-results:
	case <-time.After(5 * time.Second):
		t.Fatal("50ms sleep never woke on the real host (timer driver did not fire)")
	}
	const tolerance = 250 * time.Millisecond
	if r.elapsed < 50*time.Millisecond || r.elapsed > 50*time.Millisecond+tolerance {
		t.Fatalf("50ms sleep took %v, want within [50ms, %v]", r.elapsed, 50*time.Millisecond+tolerance)
	}
	if adv := r.nowEnd - r.nowStart; adv < 50*time.Millisecond {
		t.Fatalf("App.Now advanced %v across a 50ms sleep (start %v, end %v)", adv, r.nowStart, r.nowEnd)
	}
}

// TestClockRealHostNowAdvances: App.Now follows wall time in a real app
// with no pending timers at all (PlatformClock::now -> Instant::now,
// platform_scheduler.rs:177-190, platform.rs:1168-1170).
func TestClockRealHostNowAdvances(t *testing.T) {
	app, h := newAppWithHost(t)
	var first, second time.Duration
	onForeground(t, h, func() { first = app.Now() })
	time.Sleep(40 * time.Millisecond)
	onForeground(t, h, func() { second = app.Now() })
	if second-first < 40*time.Millisecond {
		t.Fatalf("App.Now advanced %v across 40ms of wall time", second-first)
	}
}

// TestClockRealHostTimersFireInRealTime ports the reference
// timers_fire_in_real_time (gpui/src/platform/threaded_dispatcher.rs:
// 672-694): a 10ms timer fires on its own, with nothing driving the
// clock, well before a 10s deadline. Timers with different deadlines fire
// in deadline order.
func TestClockRealHostTimersFireInRealTime(t *testing.T) {
	app, h := newAppWithHost(t)
	order := make(chan int, 2)
	onForeground(t, h, func() {
		for _, ms := range []int{30, 10} {
			ms := ms
			app.Background().Spawn(func(run *TaskRun) int {
				run.Sleep(time.Duration(ms) * time.Millisecond)
				order <- ms
				return ms
			}).Detach()
		}
	})
	deadline := time.After(10 * time.Second)
	var got []int
	for len(got) < 2 {
		select {
		case ms := <-order:
			got = append(got, ms)
		case <-deadline:
			t.Fatalf("timers did not fire in real time: fired %v", got)
		}
	}
	if got[0] != 10 || got[1] != 30 {
		t.Fatalf("timer fire order = %v, want [10 30]", got)
	}
}

// TestClockRealHostCancelWakesSleeper: cancelling a task parked on a long
// timer wakes it without waiting for the deadline (the reference
// cancel_pending_timers_wakes_waiters_without_waiting_for_deadline,
// threaded_dispatcher.rs:696, adapted: Go cancellation is the explicit
// Task.Cancel that removes the timer and resumes the worker).
func TestClockRealHostCancelWakesSleeper(t *testing.T) {
	app, h := newAppWithHost(t)
	woke := make(chan bool, 1)
	var task Task[int]
	onForeground(t, h, func() {
		task = app.Background().Spawn(func(run *TaskRun) int {
			run.Sleep(10 * time.Second)
			woke <- run.Cancelled()
			return 0
		})
	})
	time.Sleep(20 * time.Millisecond)
	start := time.Now()
	onForeground(t, h, func() { task.Cancel() })
	select {
	case cancelled := <-woke:
		if !cancelled {
			t.Fatal("sleeper woke without observing cancellation")
		}
		if since := time.Since(start); since > 2*time.Second {
			t.Fatalf("cancel woke the sleeper only after %v", since)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled sleeper was not woken before its 10s deadline")
	}
}
