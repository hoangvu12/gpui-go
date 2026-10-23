package script

import (
	"context"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Scheduler is how the host runs deferred work on the thread that
// renders. goja is single-threaded, so nothing that runs script does
// so from another goroutine: timer callbacks, fetch and async-action
// completions all marshal through here, onto the host thread.
//
// AppScheduler adapts a gpui application (its foreground executor and
// its clock — the virtual clock under TestApp, wall time in a real
// app, so timer tests are deterministic). A host without a scheduler
// still loads and runs handlers; the globals that need one
// (setTimeout, setInterval, fetch, call on an async action) raise a
// named error telling the script what is missing.
type Scheduler interface {
	// Post runs fn on the host thread as soon as the scheduler can.
	Post(fn func())
	// After runs fn on the host thread once d has passed on the
	// scheduler's clock (virtual under the test app), returning its
	// cancellation. Equal deadlines fire in registration order.
	After(d time.Duration, fn func()) (cancel func())
	// Run runs work on a goroutine the scheduler owns — the only place
	// a script's blocking work (an HTTP round trip, a slow Go action)
	// may run — and calls done on the host thread after it returns.
	// done never runs before work returns; a cancelled ctx is the
	// caller's signal to stop. The returned cancel cancels ctx.
	Run(ctx context.Context, work func(ctx context.Context), done func()) (cancel func())
	// Now returns the scheduler's clock in milliseconds since the host
	// app's origin (performance.now's time origin; the virtual clock
	// under the test app).
	Now() int64
}

// AppScheduler adapts a gpui application to the host's Scheduler.
//
//	sched := script.AppScheduler(app)
//	host := script.New(script.WithScheduler(sched))
//
// Timers ride the app's task machinery (SpawnDelivering: the worker
// parks on TaskRun.Sleep immediately, so the dispatcher never blocks
// waiting for one); Post is a foreground spawn; Run does the work on a
// goroutine and marshals the completion back — a scheduler task whose
// body blocked would freeze the dispatcher, which "drives a task to
// its next park point" inline on the host thread.
func AppScheduler(app *gpui.App) Scheduler {
	if app == nil {
		panic("script: AppScheduler(nil)")
	}
	return appScheduler{app: app}
}

type appScheduler struct{ app *gpui.App }

// Post implements Scheduler: a foreground task whose update runs fn.
func (s appScheduler) Post(fn func()) {
	postOnForeground(s.app, fn)
}

// postOnForeground runs fn as one foreground task update. A spawn
// after the app began shutting down is rejected with a panic (the
// scheduler's shutdown guard); a late completion has nothing left to
// tell, so it is dropped instead of taking the app down with it.
func postOnForeground(app *gpui.App, fn func()) {
	defer func() { _ = recover() }()
	app.Spawn(func(cx *gpui.AsyncApp) struct{} {
		cx.Update(func(*gpui.App) { fn() })
		return struct{}{}
	})
}

// After implements Scheduler: a delivery task whose worker sleeps for
// d on the app's (virtual under TestApp) clock and whose delivery runs
// fn as one app update. The worker parks on its first request, so
// driving it never blocks the dispatcher; Task.Cancel wakes the parked
// worker and discards the delivery, which is what clearTimeout needs.
func (s appScheduler) After(d time.Duration, fn func()) func() {
	task := s.app.SpawnDelivering(s.app.RootScope(),
		func(run *gpui.TaskRun) struct{} {
			run.Sleep(d)
			return struct{}{}
		},
		func(struct{}, *gpui.App) { fn() })
	return task.Cancel
}

// Run implements Scheduler: work runs on a goroutine (never a
// scheduler task: the dispatcher drives task bodies inline to their
// first park point, and an HTTP round trip must not park the UI), and
// done marshals back through Post.
func (s appScheduler) Run(ctx context.Context, work func(ctx context.Context), done func()) func() {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		defer func() { _ = recover() }()
		work(ctx)
		postOnForeground(s.app, done)
	}()
	return cancel
}

// Now implements Scheduler: the app's clock, which the test app
// advances deterministically (TestApp.AdvanceClock) and a real app
// reads from the platform.
func (s appScheduler) Now() int64 {
	return s.app.Now().Milliseconds()
}
