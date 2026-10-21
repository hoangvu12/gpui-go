package gpui

import (
	"sync"
	"time"
)

// Production wall-clock timers (ticket base/04).
//
// The scheduler has two clocks, mirroring the reference's split between
// the test dispatcher and the platform scheduler:
//
//   - Test applications (TestApp) keep the deterministic virtual clock:
//     it moves only through TestApp.AdvanceClock (the reference
//     BackgroundExecutor::advance_clock, executor.rs:210-214, "In tests,
//     move time forward") and real time never leaks into it. wall stays
//     nil and every path below is a no-op.
//   - Real applications (Host.attach) switch the scheduler onto a
//     wallClock: the clock reads monotonic wall time (the reference
//     PlatformClock::now -> PlatformDispatcher::now -> Instant::now,
//     platform_scheduler.rs:177-190, platform.rs:1168-1170), and pending
//     timers are driven by one runtime timer armed for the earliest
//     deadline whose expiry wakes the foreground dispatcher (the
//     reference PlatformScheduler::timer dispatches its completion
//     runnable through dispatch_after, platform_scheduler.rs:128-151;
//     the Windows dispatcher arms a threadpool timer for the relative
//     delay, gpui_windows/src/dispatcher.rs:73-89,147-149, and the
//     completion wakes the awaiting task back onto the main thread
//     through dispatch_on_main_thread, dispatcher.rs:118-132).
//
// Go adaptation: the Windows threadpool timer becomes a runtime
// time.AfterFunc whose callback calls the dispatcher wake (the same
// coalesced host Post that runnable work uses), and the timer firing
// plus the woken continuation run inside the host-thread drain
// (scheduler.runRealtime). Timer bookkeeping (the deadline-ordered
// timer list) stays owned by the foreground thread exactly as in test
// mode; only the wake crosses threads.

// wallClock is the real-application scheduler clock: monotonic wall time
// in whole milliseconds since attach, continuing from the virtual clock
// reading at attach, plus the single timer driver that wakes the
// foreground dispatcher at the earliest pending deadline.
type wallClock struct {
	// origin is the monotonic instant that corresponds to baseMs.
	origin time.Time
	// baseMs is the scheduler clock reading at attach, so the clock never
	// moves backwards across the switch.
	baseMs int64
	// wake is the foreground dispatcher wake (platformDispatcher.wake).
	wake func()

	// mu guards the timer driver state: arming happens on the foreground
	// thread, firing on a runtime timer goroutine.
	mu sync.Mutex
	// timer is the single driver timer (nil until first armed).
	timer *time.Timer
	// armed reports whether timer is pending for armedDeadline.
	armed         bool
	armedDeadline int64
	// stopped disables the driver (App.Shutdown).
	stopped bool
}

// elapsed reports the exact monotonic time since origin.
func (w *wallClock) elapsed() time.Duration { return time.Since(w.origin) }

// nowMs reports the wall clock in whole milliseconds (floor). Safe from
// any goroutine.
func (w *wallClock) nowMs() int64 { return w.baseMs + w.elapsed().Milliseconds() }

// deadlineAfter reports the scheduler deadline for a sleep of d starting
// now, rounded UP to the next whole millisecond so the timer never
// completes before d elapsed (the reference timer completes "after the
// given duration ... the elapsed duration may be longer than requested",
// executor.rs:193-202; the Windows dispatcher arms the full relative
// delay, gpui_windows/src/dispatcher.rs:79-86).
func (w *wallClock) deadlineAfter(d time.Duration) int64 {
	end := w.elapsed() + d
	ms := int64(end / time.Millisecond)
	if end%time.Millisecond != 0 {
		ms++
	}
	return w.baseMs + ms
}

// until reports the real delay before the clock reaches deadline.
func (w *wallClock) until(deadline int64) time.Duration {
	return time.Until(w.origin.Add(time.Duration(deadline-w.baseMs) * time.Millisecond))
}

// fire is the driver timer callback: it wakes the foreground dispatcher,
// whose drain fires every ready timer (the threadpool timer callback
// running the completion runnable, gpui_windows/src/dispatcher.rs:183-191).
func (w *wallClock) fire() {
	w.mu.Lock()
	w.armed = false
	stopped := w.stopped
	wake := w.wake
	w.mu.Unlock()
	if !stopped && wake != nil {
		wake()
	}
}

// useWallClock switches the scheduler onto the production wall clock,
// waking the foreground dispatcher through wake when a timer is due. It
// is called once by Host.attach before the application is used from
// other goroutines; test applications never call it.
func (s *scheduler) useWallClock(wake func()) {
	if s.wall != nil {
		return
	}
	s.wall = &wallClock{origin: time.Now(), baseMs: s.clockMs, wake: wake}
	s.armWallTimer()
}

// realtime reports whether the scheduler runs on the production wall
// clock.
func (s *scheduler) realtime() bool { return s.wall != nil }

// runRealtime is the production foreground drain: it brings the
// scheduler clock up to wall time, fires every timer whose deadline
// passed (deadline order, registration order on ties, as fireReady), runs
// all runnable work to its next park point, and re-arms the driver for
// the earliest remaining deadline. Without a wall clock it is exactly
// run(). After App.Shutdown began, timers no longer fire (Shutdown drains
// without advancing the clock; workers that keep sleeping stay parked and
// tracked).
func (s *scheduler) runRealtime() {
	s.guardNotUpdating()
	if s.wall != nil && !s.shuttingDown {
		if now := s.wall.nowMs(); now > s.clockMs {
			s.clockMs = now
		}
		s.fireReady()
	}
	s.run()
	s.armWallTimer()
}

// armWallTimer (re)arms the driver timer for the earliest pending
// deadline, or stops it when no timer is pending or the app is shutting
// down. Foreground thread only. No-op without a wall clock.
func (s *scheduler) armWallTimer() {
	w := s.wall
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if s.shuttingDown {
		w.stopped = true
	}
	if w.stopped || len(s.timers) == 0 {
		if w.timer != nil {
			w.timer.Stop()
		}
		w.armed = false
		return
	}
	deadline := s.timers[0].deadline
	if w.armed && w.armedDeadline == deadline {
		return
	}
	delay := w.until(deadline)
	if delay < 0 {
		delay = 0
	}
	if w.timer == nil {
		w.timer = time.AfterFunc(delay, w.fire)
	} else {
		w.timer.Reset(delay)
	}
	w.armed = true
	w.armedDeadline = deadline
}
