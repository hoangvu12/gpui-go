package gpui

import "time"

// TestApp is a deterministic headless application: a virtual clock, the
// controlled foreground/background dispatcher (equivalent to the
// reference TestDispatcher), and the application core. It is the seam the
// conformance port fixture drives.
//
// Observable operations:
//
//   - Update runs one application update with effect flushing.
//   - RunUntilParked drains runnable tasks until the scheduler would
//     park (pending timers do not fire and do not keep it running).
//   - AdvanceClock(ms) moves the virtual clock forward, firing ready
//     timers and running their foreground continuations synchronously
//     inside the call.
//   - Tick drives one queued task to its next park point.
type TestApp struct {
	app   *App
	sched *scheduler
}

// NewTestApp creates a deterministic test application.
func NewTestApp() *TestApp {
	sched := newScheduler()
	app := newApp(sched)
	return &TestApp{app: app, sched: sched}
}

// App returns the application core.
func (ta *TestApp) App() *App { return ta.app }

// RootScope returns the application's root ownership scope, the natural
// owner for fixture-level entities.
func (ta *TestApp) RootScope() *Scope { return ta.app.RootScope() }

// Background returns the background executor.
func (ta *TestApp) Background() *BackgroundExecutor { return ta.app.Background() }

// Foreground returns the foreground executor.
func (ta *TestApp) Foreground() *ForegroundExecutor { return ta.app.Foreground() }

// Update runs f as one application update on the dispatcher goroutine;
// queued effects flush when it returns.
func (ta *TestApp) Update(f func(*App)) { ta.app.Update(f) }

// RunUntilParked runs tasks until the scheduler would park. Timers that
// have not yet fired keep their tasks parked.
func (ta *TestApp) RunUntilParked() { ta.sched.run() }

// AdvanceClock moves the virtual clock forward by ms, running ready
// timers and their foreground continuations synchronously.
func (ta *TestApp) AdvanceClock(ms int64) { ta.sched.advanceClock(ms) }

// Tick drives at most one queued task to its next park point, reporting
// whether work remained.
func (ta *TestApp) Tick() bool {
	ta.sched.guardNotUpdating()
	t := ta.sched.takeOne()
	if t == nil {
		return false
	}
	ta.sched.drive(t)
	return true
}

// NowMs reports the virtual clock in milliseconds.
func (ta *TestApp) NowMs() int64 { return ta.sched.nowMs() }

// Now reports the virtual clock since the test started.
func (ta *TestApp) Now() time.Duration {
	return time.Duration(ta.sched.nowMs()) * time.Millisecond
}
