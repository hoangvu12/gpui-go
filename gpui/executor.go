package gpui

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// scheduler is the deterministic dispatcher behind both executors. It
// owns the virtual clock and a FIFO drive queue. Each task body runs on
// its own goroutine, but the scheduler drives at most one task at a time
// from its park point to its next park point (timer sleep, await or
// foreground update), waiting for the task's next request in between:
// observable ordering therefore stays deterministic even though task
// bodies execute on real goroutines.
//
// The scheduler must be driven from the application's dispatcher
// goroutine (TestApp.RunUntilParked, Tick, AdvanceClock). Running it
// while an application update is active is programmer misuse and panics,
// mirroring the reference's RefCell borrow behavior.
//
// Determinism policy (ticket04):
//
//   - Default lock-step FIFO: the ready queue drains in FIFO order, at
//     most one worker body runs between park points, and the dispatcher
//     waits for each body's next request, so observable ordering is
//     deterministic. Timers fire in deadline order (registration order
//     on ties) and pending timers alone never keep run() going.
//   - Seeded interleaved schedule (TestApp.SetScheduleSeed): the next
//     task to drive is a deterministic uniform splitmix64 choice over
//     the ready queue, and every drive is recorded as a ScheduleStep.
//     The same seed, the same spawn order and the same timer schedule
//     reproduce the identical interleaving, recorded trace and
//     observable event order. Seeds select interleaving ORDER, not
//     parallelism: worker bodies still run one at a time between park
//     points, mirroring the reference test dispatcher's controlled
//     runnable selection. Real concurrent workers belong to the
//     production executors.
//   - The scheduler never advances the virtual clock implicitly and
//     refuses to run while an application update is active.
type scheduler struct {
	app     *App
	clockMs int64
	timers  []*timerEntry
	queue   []*taskRun

	// mu guards the queue slice and the queued flags: in real-app mode
	// (ticket05) pushes can arrive from any goroutine (App.Spawn from
	// background threads, like the reference's thread-safe
	// dispatch_on_main_thread), while only the foreground dispatcher
	// dequeues. Test mode keeps the same FIFO ordering; the lock never
	// spans a drive (drives block on task requests).
	mu sync.Mutex

	// taskSeq assigns stable task identities (diagnostics, schedule
	// traces).
	taskSeq uint64
	// seeded selects the recorded interleaved schedule mode; rng drives
	// its choices and steps records every drive. Empty in the default
	// FIFO mode.
	seeded bool
	rng    *schedRng
	steps  []ScheduleStep
	// executions tracks in-flight workers until the foreground
	// acknowledges their completion (runtime ownership contract,
	// "Subscriptions and tasks"). Ordered by worker start.
	executions []*executionRecord
	// execSeq orders execution records.
	execSeq uint64
	// panicHook contains worker panics when installed
	// (App.SetTaskPanicHook).
	panicHook func(TaskPanic)
	// shuttingDown marks the app as rejecting new work while terminal
	// completion messages keep being accepted.
	shuttingDown bool
	// acks counts worker acknowledgments ever; App.Shutdown diffs it
	// around its drain.
	acks int
}

func newScheduler() *scheduler { return &scheduler{} }

// ScheduleStep is one recorded scheduler drive in seeded schedule mode.
type ScheduleStep struct {
	// Step is the 1-based drive ordinal.
	Step int
	// TaskID is the stable identity of the driven task.
	TaskID uint64
	// ClockMs is the virtual clock in milliseconds at the drive.
	ClockMs int64
}

// schedRng is the splitmix64 generator behind seeded schedules: fixed
// arithmetic and no map iteration or ambient state, so choices are
// reproducible.
type schedRng struct{ state uint64 }

func (r *schedRng) next() uint64 {
	r.state += 0x9E3779B97F4A7C15
	z := r.state
	z ^= z >> 30
	z *= 0xBF58476D1CE4E5B9
	z ^= z >> 27
	z *= 0x94D049BB133111EB
	z ^= z >> 31
	return z
}

// timerEntry is one registered virtual-clock timer.
type timerEntry struct {
	deadline int64
	task     *taskRun
}

// taskRequest is one request a task goroutine sends to the dispatcher.
type taskRequest struct {
	kind   int
	dur    time.Duration
	target *taskRun
	fn     func(*App)
	result any
	panic  any
}

const (
	reqDone = iota + 1
	reqSleep
	reqAwait
	reqUpdate
	reqPanic
)

// taskRun is one running task: its goroutine, its park state and its
// shared Task-handle state. All taskRun fields are owned by the
// dispatcher goroutine; the task body communicates only through the
// request channel.
type taskRun struct {
	sched      *scheduler
	requests   chan taskRequest
	resume     chan struct{}
	started    bool
	done       bool
	cancelled  bool
	queued     bool
	detached   bool
	foreground bool
	// failed records a contained worker panic; panicValue holds the
	// recovered value.
	failed     bool
	panicValue any
	timer      *timerEntry
	awaiting   *taskRun
	body       func() any
	result     any
	waiters    []*taskRun
	owner      *Scope
	// id is the stable scheduler identity of the task.
	id uint64
	// execution is the app-tracked execution record while the worker is
	// in flight; it is released only at acknowledgment (see
	// task_ownership.go).
	execution *executionRecord
	// execScope is the task's execution scope (declared owned
	// dependencies), retained until actual worker termination even when
	// the receiving/result scope is cancelled. Delivery tasks only.
	execScope *Scope
	// delivery is the result-delivery token: cancellation or worker
	// failure closes it; the handoff consults it exactly once.
	delivery *deliverySpec
}

// newTask creates a parked task whose body starts when the dispatcher
// first drives it.
func (s *scheduler) newTask(foreground bool) *taskRun {
	s.taskSeq++
	return &taskRun{
		id:         s.taskSeq,
		sched:      s,
		requests:   make(chan taskRequest, 1),
		resume:     make(chan struct{}, 1),
		foreground: foreground,
	}
}

// guardNotUpdating panics when the scheduler would run while an update is
// active.
func (s *scheduler) guardNotUpdating() {
	if s.app != nil && s.app.updateDepth > 0 {
		panic("gpui: cannot run the scheduler while an application update is active")
	}
}

// push schedules one drive of t, unless it is finished or already queued.
func (s *scheduler) push(t *taskRun) {
	s.mu.Lock()
	if t.done || t.queued {
		s.mu.Unlock()
		return
	}
	t.queued = true
	s.queue = append(s.queue, t)
	// Real-app mode (ticket05): the host's wake mechanism drives the
	// scheduler on the foreground thread, so runnable work wakes it.
	// Test applications keep platformWake nil and drive the scheduler
	// themselves (RunUntilParked, Tick, AdvanceClock).
	wake := s.app != nil && s.app.platformWake != nil
	s.mu.Unlock()
	if wake {
		s.app.platformWake()
	}
}

// takeOne dequeues the next task to drive: index 0 in the default
// lock-step FIFO mode, the seeded choice in interleaved schedule mode.
// It reports nil when the queue is empty.
func (s *scheduler) takeOne() *taskRun {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queue) == 0 {
		return nil
	}
	idx := s.pick()
	t := s.queue[idx]
	s.queue = append(s.queue[:idx], s.queue[idx+1:]...)
	t.queued = false
	return t
}

// run drains the drive queue until quiet: every task that can make
// progress runs to its next park point. Pending timers do not fire and
// do not keep the loop going, matching the reference run_until_parked.
// In seeded schedule mode the next task is the rng's choice (see the
// determinism policy on the scheduler type).
func (s *scheduler) run() {
	s.guardNotUpdating()
	for {
		t := s.takeOne()
		if t == nil {
			return
		}
		s.recordStep(t)
		s.drive(t)
	}
}

// pick selects the next task to drive: index 0 in the default lock-step
// FIFO mode, a deterministic uniform choice over the ready queue in
// seeded schedule mode.
func (s *scheduler) pick() int {
	if !s.seeded || len(s.queue) <= 1 {
		return 0
	}
	return int(s.rng.next() % uint64(len(s.queue)))
}

// recordStep records one drive in seeded schedule mode.
func (s *scheduler) recordStep(t *taskRun) {
	if !s.seeded {
		return
	}
	s.steps = append(s.steps, ScheduleStep{
		Step:    len(s.steps) + 1,
		TaskID:  t.id,
		ClockMs: s.clockMs,
	})
}

// rejectIfShuttingDown panics with the new-work rejection diagnostic
// after App.Shutdown began: terminal completion messages keep being
// accepted, new application work is not.
func (s *scheduler) rejectIfShuttingDown(op string) {
	if s.shuttingDown {
		panic("gpui: " + op + " rejected: the application is shutting down")
	}
}

// drive runs t from its current park point to its next park point (or
// completion). A fresh task's goroutine starts here; the dispatcher waits
// on the task's request channel while the body runs, which keeps
// observable ordering deterministic.
func (s *scheduler) drive(t *taskRun) {
	if t.done {
		return
	}
	if t.cancelled && !t.started {
		// A cancelled task never starts: cancellation of an unstarted
		// body retires it at cancel time, so this is a stale queue slot.
		return
	}
	if !t.started {
		t.started = true
		t.beginExecution()
		go t.wrapper()
	} else {
		t.resume <- struct{}{}
	}
	for {
		req := <-t.requests
		switch req.kind {
		case reqDone:
			// The worker posted completion: the foreground acknowledges
			// it, running the owned-message handoff for delivery tasks
			// before the execution record releases.
			t.workerDone(req.result)
			return
		case reqPanic:
			// A panic in the task body is contained first: the task fails,
			// its execution resources are acknowledged and released, and
			// the panic is then surfaced to the app's panic hook or
			// re-raised on the dispatcher goroutine. No further tasks run
			// while it unwinds.
			t.workerPanicked(req.panic)
			return
		case reqSleep:
			// A cancelled task may still park: cancellation is cooperative,
			// and a worker that keeps sleeping after observing it stays
			// unresolved and tracked rather than being force-killed.
			s.addTimer(t, req.dur)
			return
		case reqAwait:
			if t.cancelled || req.target == nil || req.target.done || req.target.cancelled {
				t.resume <- struct{}{}
				continue
			}
			t.awaiting = req.target
			req.target.waiters = append(req.target.waiters, t)
			return
		case reqUpdate:
			// Foreground update from a task body: run it inline on the
			// dispatcher goroutine with full update/flush semantics.
			s.app.Update(req.fn)
			t.resume <- struct{}{}
			continue
		}
	}
}

// wrapper is the task goroutine entry: it runs the body and reports
// completion, converting a panic into a request that re-raises it on the
// dispatcher.
func (t *taskRun) wrapper() {
	defer func() {
		if r := recover(); r != nil {
			t.requests <- taskRequest{kind: reqPanic, panic: r}
		}
	}()
	t.requests <- taskRequest{kind: reqDone, result: t.body()}
}

// finish is the acknowledgment: it records the task result, releases the
// execution record, wakes the task's waiters and removes the task from
// its registries (owner scope, detached-task registry). Cancellation set
// flags earlier; every retained execution resource is released HERE,
// when the foreground acknowledges the worker's completion (contract:
// "Execution resources remain registered until the foreground
// acknowledges completion").
func (t *taskRun) finish(result any) {
	t.done = true
	t.result = result
	t.removeTimer()
	t.releaseExecution()
	waiters := t.waiters
	t.waiters = nil
	for _, w := range waiters {
		w.awaiting = nil
		t.sched.push(w)
	}
	if t.detached {
		t.sched.app.removeDetachedTask(t)
	}
	if t.owner != nil {
		t.owner.removeTask(t)
		t.owner = nil
	}
	t.sched.acks++
}

// cancel signals cancellation and closes result delivery without waiting
// for the worker to exit (contract: cancellation is cooperative). Waiters
// are released with no result; a parked goroutine is woken so a
// well-behaved body can observe cancellation and exit.
//
// Execution resources are NOT released here: the task stays registered
// with its owner scope, the detached-task registry keeps its entry, and
// the app's execution record stays tracked until the worker acknowledges
// completion — cancellation never implies worker completion. A task
// whose body never started retires immediately: no worker existed, so
// the acknowledgment is vacuous.
func (t *taskRun) cancel() {
	if t.done || t.cancelled {
		return
	}
	t.cancelled = true
	t.deliveryClose()
	t.removeTimer()
	if t.awaiting != nil {
		t.awaiting.removeWaiter(t)
		t.awaiting = nil
	}
	waiters := t.waiters
	t.waiters = nil
	for _, w := range waiters {
		w.awaiting = nil
		t.sched.push(w)
	}
	if !t.started {
		t.workerDone(nil)
		return
	}
	t.sched.push(t)
}

// removeWaiter detaches one waiting task.
func (t *taskRun) removeWaiter(w *taskRun) {
	for i, candidate := range t.waiters {
		if candidate == w {
			t.waiters = append(t.waiters[:i], t.waiters[i+1:]...)
			return
		}
	}
}

// addTimer registers a virtual-clock timer for t at now + d. Timers with
// equal deadlines fire in registration order.
func (s *scheduler) addTimer(t *taskRun, d time.Duration) {
	e := &timerEntry{deadline: s.clockMs + d.Milliseconds(), task: t}
	t.timer = e
	i := sort.Search(len(s.timers), func(i int) bool {
		return s.timers[i].deadline > e.deadline
	})
	s.timers = append(s.timers, nil)
	copy(s.timers[i+1:], s.timers[i:])
	s.timers[i] = e
}

// removeTimer unregisters the task's timer.
func (t *taskRun) removeTimer() {
	if t.timer == nil {
		return
	}
	timers := t.sched.timers
	for i, e := range timers {
		if e == t.timer {
			timers = append(timers[:i], timers[i+1:]...)
			break
		}
	}
	t.sched.timers = timers
	t.timer = nil
}

// fireReady fires every timer whose deadline passed the clock, waking
// their tasks.
func (s *scheduler) fireReady() {
	n := 0
	for n < len(s.timers) && s.timers[n].deadline <= s.clockMs {
		n++
	}
	fired := s.timers[:n]
	s.timers = append([]*timerEntry(nil), s.timers[n:]...)
	for _, e := range fired {
		if e.task.timer == e {
			e.task.timer = nil
			s.push(e.task)
		}
	}
}

// advanceClock moves the virtual clock forward by ms, repeatedly running
// all runnable work and then firing the earliest timer whose deadline is
// within the advance, until the clock reaches its target. Ready timers
// and their foreground continuations run synchronously inside this call,
// matching the reference TestDispatcher::advance_clock.
func (s *scheduler) advanceClock(ms int64) {
	s.guardNotUpdating()
	target := s.clockMs + ms
	for {
		s.run()
		if len(s.timers) > 0 && s.timers[0].deadline <= target {
			s.clockMs = s.timers[0].deadline
			s.fireReady()
		} else {
			break
		}
	}
	s.clockMs = target
}

// nowMs reports the virtual clock in milliseconds.
func (s *scheduler) nowMs() int64 { return s.clockMs }

// Task is the handle of one running task. Handle copies alias the same
// task.
//
// Go adaptation (contract: "Subscriptions and tasks"): Go has no drop,
// so "dropping the handle cancels" becomes explicit Cancel; an ignored
// handle neither cancels nor retains the task. Detach transfers
// cancellation ownership to the app's detached-task registry, which
// removes the task on completion. Cancel and Detach must be called on
// the dispatcher goroutine. Full task ownership and result-delivery
// semantics are ticket04; this slice keeps the minimal real behavior.
type Task[T any] struct {
	t *taskRun
}

// Detach transfers cancellation ownership to the app registry: the task
// keeps running with a weak endpoint until completion or app shutdown.
// The registry reports completion weakly: an entry is removed when the
// foreground acknowledges the worker, and a cancelled-but-running task
// keeps its entry until that acknowledgment. Detach does not remove a
// bound result-delivery gate (SpawnDelivering): a detached delivery task
// still discards its result when the receiving scope closed.
func (task Task[T]) Detach() {
	if task.t == nil {
		panic("gpui: Detach on a zero task")
	}
	t := task.t
	if t.detached || t.done || t.cancelled {
		return
	}
	if t.owner != nil {
		t.owner.removeTask(t)
		t.owner = nil
	}
	t.detached = true
	t.sched.app.detachedTasks = append(t.sched.app.detachedTasks, t)
}

// Cancel signals cancellation and closes result delivery without waiting
// for the worker to exit. Awaiting tasks are released with no result; a
// parked worker is woken to observe cancellation cooperatively.
// Execution resources stay tracked until the worker acknowledges (see
// task_ownership.go); cancellation never implies worker completion.
func (task Task[T]) Cancel() {
	if task.t == nil {
		panic("gpui: Cancel on a zero task")
	}
	task.t.cancel()
}

// TransferInto moves the task's cancellation ownership to another live
// scope, mirroring Subscription.TransferInto (contract: "Tasks likewise
// belong to an explicit scope").
func (task Task[T]) TransferInto(scope *Scope) {
	if task.t == nil {
		panic("gpui: TransferInto on a zero task")
	}
	if scope == nil {
		panic("gpui: Task.TransferInto requires a non-nil owner scope")
	}
	t := task.t
	if scope.app != t.sched.app {
		panic("gpui: Task.TransferInto owner scope belongs to a different application")
	}
	if t.done || t.cancelled {
		return
	}
	scope.requireOpen()
	if t.detached {
		for i, candidate := range t.sched.app.detachedTasks {
			if candidate == t {
				t.sched.app.detachedTasks = append(t.sched.app.detachedTasks[:i], t.sched.app.detachedTasks[i+1:]...)
				break
			}
		}
		t.detached = false
	}
	if t.owner != nil {
		t.owner.removeTask(t)
	}
	t.owner = scope
	scope.addTask(t)
}

// Done reports whether the task completed.
func (task Task[T]) Done() bool {
	return task.t != nil && task.t.done
}

// Cancelled reports whether the task was cancelled.
func (task Task[T]) Cancelled() bool {
	return task.t != nil && task.t.cancelled
}

// Failed reports whether the task's worker panicked: the panic was
// contained (execution resources acknowledged and released) and surfaced
// to the app's panic hook or re-raised on the dispatcher goroutine.
func (task Task[T]) Failed() bool {
	return task.t != nil && task.t.failed
}

// PanicValue returns the recovered panic value of a failed task, or nil
// when the task did not fail.
func (task Task[T]) PanicValue() any {
	if task.t == nil {
		return nil
	}
	return task.t.panicValue
}

// resultValue returns the task result, or the zero value when the task
// was cancelled or has not completed.
func (task Task[T]) resultValue() T {
	if task.t == nil || task.t.cancelled || !task.t.done || task.t.result == nil {
		var zero T
		return zero
	}
	return task.t.result.(T)
}

// TaskRun is the capability handle passed to a background task body: it
// provides the virtual-clock timer await and task awaits. Bodies must
// only touch the application through these capabilities.
type TaskRun struct {
	t *taskRun
}

// Sleep blocks the task until the virtual clock passes d. It returns
// immediately for non-positive durations. A cancelled task may still
// sleep: cancellation is cooperative, so a worker that keeps sleeping
// after observing it is ignoring cancellation — it stays parked,
// unresolved and tracked (never force-killed) — while a well-behaved
// worker wakes from the cancellation wake-up and returns.
func (r *TaskRun) Sleep(d time.Duration) {
	if d <= 0 {
		return
	}
	r.t.requests <- taskRequest{kind: reqSleep, dur: d}
	<-r.t.resume
}

// Cancelled reports whether the task received a cancellation signal.
func (r *TaskRun) Cancelled() bool { return r.t.cancelled }

// Await parks the current task until task completes, returning its
// result (the zero value when it was cancelled).
func (r *TaskRun) Await[T any](task Task[T]) T {
	if task.t == nil {
		panic("gpui: Await called with a zero task")
	}
	if r.t.cancelled {
		return task.resultValue()
	}
	if !task.t.done && !task.t.cancelled {
		r.t.requests <- taskRequest{kind: reqAwait, target: task.t}
		<-r.t.resume
	}
	return task.resultValue()
}

// BackgroundExecutor spawns work on goroutines under the deterministic
// scheduler. Its virtual clock is the scheduler's clock.
type BackgroundExecutor struct {
	sched *scheduler
}

// Spawn runs f on its own goroutine under the deterministic scheduler.
// The returned task is handle-owned: the body starts when the dispatcher
// next drains the queue (run, tick or clock advance), not at spawn time.
// The body receives a TaskRun for timers and awaits; workers never touch
// application state directly.
func (b *BackgroundExecutor) Spawn[T any](f func(run *TaskRun) T) Task[T] {
	if f == nil {
		panic("gpui: Spawn called with a nil body")
	}
	b.sched.rejectIfShuttingDown("Spawn")
	t := b.sched.newTask(false)
	t.body = func() any {
		run := &TaskRun{t: t}
		return f(run)
	}
	b.sched.push(t)
	return Task[T]{t: t}
}

// NowMs returns the virtual clock in milliseconds.
func (b *BackgroundExecutor) NowMs() int64 { return b.sched.nowMs() }

// ForegroundExecutor queues work that must run on the dispatcher
// goroutine. Its queue is the scheduler's drive queue; only the
// dispatcher drains it.
type ForegroundExecutor struct {
	sched *scheduler
	app   *App
}

// Spawn runs an asynchronous closure on the foreground executor: see
// App.Spawn.
func (f *ForegroundExecutor) Spawn[T any](body func(cx *AsyncApp) T) Task[T] {
	if f.app == nil {
		panic("gpui: ForegroundExecutor has no owning application")
	}
	return f.app.Spawn(body)
}

// AsyncApp is the context handed to foreground task bodies: it posts
// application updates (executed inline on the dispatcher with full
// update/flush semantics) and awaits other tasks by parking the current
// task.
type AsyncApp struct {
	app *App
	run *taskRun
}

// App returns the application this context posts to.
func (a *AsyncApp) App() *App { return a.app }

// Update posts one application update and waits for it to complete.
func (a *AsyncApp) Update(f func(*App)) {
	if f == nil {
		panic("gpui: AsyncApp.Update called with a nil callback")
	}
	if a.run == nil {
		panic("gpui: AsyncApp.Update called outside a task body")
	}
	a.run.requests <- taskRequest{kind: reqUpdate, fn: f}
	<-a.run.resume
}

// Await parks the current task until task completes, returning its
// result (the zero value when it was cancelled).
func (a *AsyncApp) Await[T any](task Task[T]) T {
	if task.t == nil {
		panic("gpui: Await called with a zero task")
	}
	if a.run == nil {
		panic("gpui: AsyncApp.Await called outside a task body")
	}
	if !task.t.done && !task.t.cancelled {
		a.run.requests <- taskRequest{kind: reqAwait, target: task.t}
		<-a.run.resume
	}
	return task.resultValue()
}

// Spawn runs an asynchronous closure on the foreground executor. The
// body receives an AsyncApp; awaiting inside it parks the task instead of
// blocking the dispatcher. The returned task defaults to the app's root
// scope for cancellation; Detach moves it to the app's detached-task
// registry.
func (a *App) Spawn[T any](f func(cx *AsyncApp) T) Task[T] {
	if f == nil {
		panic("gpui: Spawn called with a nil body")
	}
	a.sched.rejectIfShuttingDown("Spawn")
	t := a.sched.newTask(true)
	t.owner = a.rootScope
	a.rootScope.addTask(t)
	t.body = func() any {
		cx := &AsyncApp{app: a, run: t}
		return f(cx)
	}
	a.sched.push(t)
	return Task[T]{t: t}
}

// String renders the task for diagnostics.
func (t *taskRun) String() string {
	state := "parked"
	if t.done {
		state = "done"
	} else if t.cancelled {
		state = "cancelled"
	} else if t.queued {
		state = "queued"
	}
	return fmt.Sprintf("task %d (%s)", t.id, state)
}

// SetScheduleSeed switches the scheduler to the recorded interleaved
// schedule mode: the next task to drive becomes a deterministic uniform
// splitmix64 choice over the ready queue and every drive is recorded
// (see ScheduleTrace). The determinism contract: the same seed, the same
// spawn order and the same timer schedule reproduce the identical
// interleaving, recorded trace and observable event order. Seeds select
// interleaving order, not parallelism — worker bodies still run one at a
// time between park points. Seed 0 restores the default lock-step FIFO
// mode and clears the recorded trace.
func (ta *TestApp) SetScheduleSeed(seed uint64) {
	if seed == 0 {
		ta.sched.seeded = false
		ta.sched.rng = nil
		ta.sched.steps = nil
		return
	}
	ta.sched.seeded = true
	ta.sched.rng = &schedRng{state: seed}
	ta.sched.steps = nil
}

// ScheduleTrace returns the drives recorded in seeded schedule mode, in
// drive order. It is empty in the default FIFO mode.
func (ta *TestApp) ScheduleTrace() []ScheduleStep {
	return append([]ScheduleStep(nil), ta.sched.steps...)
}
