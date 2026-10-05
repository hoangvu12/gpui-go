package gpui

// This file is ticket04's task ownership and delivery layer on top of
// ticket03's scheduler (executor.go): execution-resource retention until
// worker acknowledgment, owned result-message handoff with exactly one
// delivery or discard, generation gating of late results, the
// detached-task registry's weak completion reporting, panic containment
// and the app shutdown drain. See docs/runtime-ownership-contract.md,
// "Subscriptions and tasks".
//
// Go adaptations, documented at each seam:
//
//   - Cancellation is cooperative and never implies worker completion:
//     execution resources (owner-scope registration, detached-registry
//     entry, execution record, execution scope) are released only when
//     the foreground acknowledges the worker — never by finalizers or
//     timeouts.
//   - A worker result post is processed to delivery-or-discard
//     synchronously at the dispatcher's drive point, so this port has no
//     queued-result window in which cancellation could leave a message
//     pointing into freed execution storage; the discard path still
//     releases the message and the execution record, idempotently.
//   - A task's declared dependencies form one set (the contract's owned
//     inputs and declared output dependencies): held by the separate
//     execution scope while the worker runs, transferred into the
//     message-owned result scope at the post, released after delivery or
//     discard.
//   - Window generation gating is reserved for the window tickets; the
//     delivery token carries the receiving scope's close generation and
//     the entity target's identity generation (slot generation plus
//     lease count) now.

import "fmt"

// OwnedDependency is an owned entity handle whose lease can be
// transferred into a task's execution scope: the task's declared
// dependencies. While the worker runs they are held by the execution
// scope — canceling the receiving scope does not free memory the worker
// still uses. At the result post they transfer into the message-owned
// result scope and release after delivery or discard.
type OwnedDependency interface {
	// dependencyLease exposes the owned lease for execution/result-scope
	// transfer. Entity[S] implements it; the method is unexported so
	// only this package can satisfy the interface.
	dependencyLease() *entityLease
}

// dependencyLease implements OwnedDependency for entity handles.
func (h Entity[S]) dependencyLease() *entityLease { return h.lease }

// leaseTransfer moves one owned lease registration to target without
// decrementing it (the Entity[S].TransferInto mechanics, used by the
// task ownership path).
func leaseTransfer(lease *entityLease, target *Scope) {
	if lease.owner != nil {
		lease.owner.removeLease(lease)
	}
	lease.owner = target
	target.addLease(lease)
}

// ---------------------------------------------------------------------------
// Execution records: retention until worker acknowledgment
// ---------------------------------------------------------------------------

// executionRecord is one in-flight worker's tracked execution resource:
// registered when the body starts, released only when the foreground
// acknowledges completion (cancellation sets flags; it never releases).
type executionRecord struct {
	task *taskRun
	// seq orders records by worker start.
	seq uint64
}

// beginExecution registers the execution record: the worker is in flight
// and stays tracked until the foreground acknowledges its completion.
func (t *taskRun) beginExecution() {
	if t.execution != nil {
		return
	}
	s := t.sched
	s.execSeq++
	rec := &executionRecord{task: t, seq: s.execSeq}
	t.execution = rec
	s.executions = append(s.executions, rec)
}

// releaseExecution removes the execution record: the foreground
// acknowledged the worker's termination.
func (t *taskRun) releaseExecution() {
	if t.execution == nil {
		return
	}
	s := t.sched
	for i, rec := range s.executions {
		if rec == t.execution {
			s.executions = append(s.executions[:i], s.executions[i+1:]...)
			break
		}
	}
	t.execution = nil
}

// parkedDescription reports the worker's park point for diagnostics.
func (t *taskRun) parkedDescription() string {
	switch {
	case t.awaiting != nil:
		return fmt.Sprintf("awaiting task %d", t.awaiting.id)
	case t.timer != nil:
		return fmt.Sprintf("parked on timer until +%dms", t.timer.deadline-t.sched.clockMs)
	case t.queued:
		return "queued to run"
	default:
		return ""
	}
}

// ---------------------------------------------------------------------------
// Result delivery: token, handoff, containment
// ---------------------------------------------------------------------------

// deliverySpec is one task's result-delivery token: the receiving scope
// with its close generation at registration, the optional entity target
// with its identity generation, and the typed delivery closure. Cancel
// and worker failure close the token; the handoff consults it exactly
// once, so exactly one of delivery or discard happens (idempotent).
type deliverySpec struct {
	// scope is the receiving scope of the result message.
	scope *Scope
	// scopeGen is the receiving scope's close generation recorded at
	// registration.
	scopeGen uint64
	// entity is the optional entity target (zero for scope-level
	// delivery).
	entity EntityID
	// closed marks the token closed (cancellation or worker failure).
	closed bool
	// update delivers the typed result into the owner; it runs on the
	// dispatcher goroutine inside one application update, only after
	// every gate passed.
	update func(app *App, result any)
}

// open reports whether the delivery gate still accepts delivery: the
// token is open, the receiving scope has not closed since registration
// (close generation), and an entity target is still logically live under
// the same identity generation. Closing the receiving scope suppresses
// late delivery even when the target entity has another live owner.
func (d *deliverySpec) open(app *App) bool {
	if d == nil || d.closed {
		return false
	}
	if d.scope.state != ScopeOpen || d.scope.closeGen != d.scopeGen {
		return false
	}
	if d.entity != 0 {
		slot := app.entities.slot(d.entity)
		if slot == nil || slot.count <= 0 {
			return false
		}
	}
	return true
}

// deliveryClose closes the result-delivery token: late posts discard
// instead of delivering.
func (t *taskRun) deliveryClose() {
	if t.delivery != nil {
		t.delivery.closed = true
	}
}

// workerDone processes the worker's completion post: the foreground
// acknowledgment. Delivery tasks run the owned-message handoff first;
// the execution record releases even when the delivery callback panics,
// so a panicking foreground callback never leaves a stuck record either.
func (t *taskRun) workerDone(result any) {
	defer t.finish(result)
	if t.execScope != nil {
		t.handoffResult(result)
	}
}

// workerPanicked contains a worker panic: the task fails, its execution
// resources are acknowledged and released FIRST (a panicked worker never
// leaves a stuck record), then the panic is surfaced — to the app's
// panic hook when one is installed, otherwise re-raised on the
// dispatcher goroutine after the bookkeeping is consistent.
func (t *taskRun) workerPanicked(p any) {
	t.deliveryClose()
	if t.execScope != nil {
		// No result message is created for a failed worker; the declared
		// dependencies release with the execution scope.
		closeScopeTree(t.execScope, nil)
		t.execScope = nil
	}
	t.failed = true
	t.panicValue = p
	t.finish(nil)
	if hook := t.sched.panicHook; hook != nil {
		hook(TaskPanic{TaskID: t.id, Value: p})
		return
	}
	panic(p)
}

// handoffResult performs the contract's owned-message handoff for one
// worker result post: transfer the declared dependencies into a
// message-owned result scope, close execution ownership, then deliver or
// discard through the gates, then close the result scope. Exactly one of
// delivery or discard runs; both release the message. When cancellation
// closed the token before the post, no result message is created at all,
// so no queued result can point into freed execution storage.
func (t *taskRun) handoffResult(result any) {
	app := t.sched.app
	spec := t.delivery
	if t.cancelled || spec == nil || spec.closed {
		closeScopeTree(t.execScope, nil)
		t.execScope = nil
		return
	}
	msg := newScope(app, app.rootScope)
	// The result scope closes after delivery or discard — including when
	// the delivery callback panics: the message is released while the
	// panic propagates.
	defer closeScopeTree(msg, nil)
	// Transfer the declared dependencies: message-owned from here.
	leases := append([]*entityLease(nil), t.execScope.leases...)
	for _, lease := range leases {
		leaseTransfer(lease, msg)
	}
	// Close execution ownership: the worker has terminated.
	closeScopeTree(t.execScope, nil)
	t.execScope = nil
	if spec.open(app) {
		spec.update(app, result)
	}
}

// ---------------------------------------------------------------------------
// Spawn-with-delivery API
// ---------------------------------------------------------------------------

// SpawnDelivering runs work on a background worker under the
// deterministic scheduler and delivers its result as an owned result
// message into the receiving scope target.
//
// Ownership and delivery semantics (runtime ownership contract,
// "Subscriptions and tasks"):
//
//   - The task's cancellation ownership defaults to target: closing
//     target cancels it. TransferInto/Detach move cancellation ownership
//     elsewhere — the delivery gate stays bound to target.
//   - deps are transferred (foreground, at spawn) into a separate
//     execution scope retained until actual worker termination:
//     canceling the receiving scope does not free memory the worker
//     still uses. At the result post they transfer into the
//     message-owned result scope and release after delivery or discard.
//   - Exactly one of delivery (a foreground update into the still-open
//     target) or discard (target closed, entity released, task
//     cancelled or failed) happens; both release the message and the
//     execution record. The handoff is idempotent.
//   - The worker body receives only a TaskRun (posting capability,
//     cancellation context, timers, awaits) — never escaped UI state,
//     contexts or windows. A panic in the body fails the task and is
//     contained (see SetTaskPanicHook); the execution record is still
//     acknowledged and released.
func (a *App) SpawnDelivering[T any](target *Scope, work func(run *TaskRun) T, deliver func(result T, cx *App), deps ...OwnedDependency) Task[T] {
	if work == nil {
		panic("gpui: SpawnDelivering called with a nil worker")
	}
	if deliver == nil {
		panic("gpui: SpawnDelivering called with a nil delivery callback")
	}
	spec := a.newDeliverySpec(target, "SpawnDelivering")
	var zero T
	spec.update = func(app *App, result any) {
		value := zero
		if result != nil {
			value = result.(T)
		}
		app.Update(func(cx *App) { deliver(value, cx) })
	}
	t := a.newDeliveryTask(spec, target, deps, "SpawnDelivering")
	t.body = func() any { return work(&TaskRun{t: t}) }
	a.sched.push(t)
	return Task[T]{t: t}
}

// SpawnDeliveringInto is SpawnDelivering with an entity target: the
// result message is delivered as one exclusive update of entity (fresh
// Context, effects flushed at the update boundary) while entity is still
// logically live under the identity generation recorded at spawn. The
// receiving scope target remains the delivery gate and the default
// cancellation owner: closing target discards a late result even when
// another owner keeps entity alive; releasing entity (logical zero is
// irreversible) discards it even while target stays open.
func (a *App) SpawnDeliveringInto[S, T any](target *Scope, entity Entity[S], work func(run *TaskRun) T, deliver func(state *S, result T, cx *Context[S]), deps ...OwnedDependency) Task[T] {
	if work == nil {
		panic("gpui: SpawnDeliveringInto called with a nil worker")
	}
	if deliver == nil {
		panic("gpui: SpawnDeliveringInto called with a nil delivery callback")
	}
	lease := entityLeaseOf(entity, "SpawnDeliveringInto")
	if lease.app != a {
		panic("gpui: SpawnDeliveringInto target entity belongs to a different application")
	}
	spec := a.newDeliverySpec(target, "SpawnDeliveringInto")
	spec.entity = entity.id
	var zero T
	spec.update = func(app *App, result any) {
		value := zero
		if result != nil {
			value = result.(T)
		}
		withEntityUpdate[S](app, spec.entity, func(state *S, cx *Context[S]) {
			deliver(state, value, cx)
		})
	}
	t := a.newDeliveryTask(spec, target, deps, "SpawnDeliveringInto")
	t.body = func() any { return work(&TaskRun{t: t}) }
	a.sched.push(t)
	return Task[T]{t: t}
}

// newDeliverySpec validates the receiving scope and records its close
// generation as the delivery gate.
func (a *App) newDeliverySpec(target *Scope, op string) *deliverySpec {
	a.sched.rejectIfShuttingDown(op)
	if target == nil {
		panic(fmt.Sprintf("gpui: %s requires a non-nil receiving scope", op))
	}
	if target.app != a {
		panic(fmt.Sprintf("gpui: %s receiving scope belongs to a different application", op))
	}
	target.requireOpen()
	return &deliverySpec{scope: target, scopeGen: target.closeGen}
}

// newDeliveryTask builds the task with its execution scope and delivery
// token, registered for cancellation with owner.
func (a *App) newDeliveryTask(spec *deliverySpec, owner *Scope, deps []OwnedDependency, op string) *taskRun {
	leases := make([]*entityLease, 0, len(deps))
	for _, dep := range deps {
		leases = append(leases, a.dependencyLeaseFor(dep, op))
	}
	execScope := newScope(a, a.rootScope)
	for _, lease := range leases {
		leaseTransfer(lease, execScope)
	}
	t := a.sched.newTask(false)
	t.execScope = execScope
	t.delivery = spec
	t.owner = owner
	owner.addTask(t)
	return t
}

// dependencyLeaseFor validates one owned dependency: an owned, live
// lease of this application. Borrowed facades and released handles are
// programmer errors.
func (a *App) dependencyLeaseFor(dep OwnedDependency, op string) *entityLease {
	if dep == nil {
		panic(fmt.Sprintf("gpui: %s called with a nil dependency", op))
	}
	lease := dep.dependencyLease()
	if lease == nil {
		panic(fmt.Sprintf("gpui: %s dependency is a zero entity handle", op))
	}
	if lease.kind != leaseOwned {
		panic(fmt.Sprintf("gpui: %s dependency must be an owned entity handle; callback-borrowed facades cannot be transferred into a task scope", op))
	}
	if lease.dead {
		panic(fmt.Sprintf("gpui: %s dependency entity handle is already released", op))
	}
	if lease.app != a {
		panic(fmt.Sprintf("gpui: %s dependency belongs to a different application", op))
	}
	return lease
}

// ---------------------------------------------------------------------------
// Panic surfacing
// ---------------------------------------------------------------------------

// TaskPanic reports one contained worker panic.
type TaskPanic struct {
	// TaskID is the stable scheduler identity of the failed task.
	TaskID uint64
	// Value is the recovered panic value.
	Value any
}

// SetTaskPanicHook installs the hook invoked on the dispatcher goroutine
// when a task worker panics. With a hook installed the panic is
// contained: the task fails, its execution resources are acknowledged
// and released, and the hook reports the panic instead of it
// propagating. Without a hook the contained panic is re-raised on the
// dispatcher goroutine after the bookkeeping is consistent. The hook
// runs outside any application update.
func (a *App) SetTaskPanicHook(hook func(TaskPanic)) {
	a.sched.panicHook = hook
}

// ---------------------------------------------------------------------------
// Unresolved executions and shutdown drain
// ---------------------------------------------------------------------------

// UnresolvedExecution describes one execution resource whose worker has
// not yet acknowledged completion: whether cancellation was signaled,
// and the worker's park point (empty while the body is running between
// park points).
type UnresolvedExecution struct {
	// TaskID is the stable scheduler identity of the task.
	TaskID uint64
	// Cancelled reports whether cancellation was signaled: the worker
	// has not necessarily observed it, since cancellation never implies
	// worker completion.
	Cancelled bool
	// Parked is the worker's current park point: "awaiting task N",
	// "parked on timer until +Nms", "queued to run", or empty while the
	// body is running.
	Parked string
}

// String renders the diagnostic line.
func (u UnresolvedExecution) String() string {
	status := "worker running"
	if u.Parked != "" {
		status = u.Parked
	}
	ack := "awaiting acknowledgment"
	if u.Cancelled {
		ack = "cancelled, awaiting worker acknowledgment"
	}
	return fmt.Sprintf("task %d: %s, %s", u.TaskID, ack, status)
}

// UnresolvedExecutions snapshots every execution resource whose worker
// has not acknowledged completion, in worker-start order. It is the
// parking diagnostic the contract's verification obligations require,
// and the tracked-unresolved report shutdown builds on. Dispatcher
// goroutine only.
func (a *App) UnresolvedExecutions() []UnresolvedExecution {
	out := make([]UnresolvedExecution, 0, len(a.sched.executions))
	for _, rec := range a.sched.executions {
		t := rec.task
		out = append(out, UnresolvedExecution{
			TaskID:    t.id,
			Cancelled: t.cancelled,
			Parked:    t.parkedDescription(),
		})
	}
	return out
}

// ShutdownReport is the result of App.Shutdown.
type ShutdownReport struct {
	// Cancelled counts tasks that received their cancellation signal
	// during this shutdown.
	Cancelled int
	// Acknowledged counts tasks whose workers acknowledged completion
	// during the drain, including the immediate retirements of tasks
	// whose bodies never started.
	Acknowledged int
	// Unresolved lists the execution resources whose workers never
	// acknowledged: they remain TRACKED and reported — never silently
	// dropped, and never force-completed by a timeout or finalizer. The
	// scheduler keeps accepting their terminal completion messages.
	Unresolved []UnresolvedExecution
}

// Shutdown tears the task layer down deterministically: it rejects new
// application work (Spawn and the delivery spawns panic), cancels every
// live task — scope-owned, queued, detached and in-flight — then drains
// the scheduler WITHOUT advancing the virtual clock: workers that
// acknowledge after cancellation complete during the drain; workers that
// ignore cancellation stay parked, unresolved and tracked. Calling
// Shutdown again is idempotent.
//
// Scope, entity and window teardown (closing delivery scopes, tearing
// down windows, native retirement) belongs to the shutdown-integration
// ticket; this slice drains the task layer only.
func (a *App) Shutdown() ShutdownReport {
	s := a.sched
	s.shuttingDown = true
	before := s.acks
	cancelled := a.cancelAllTasksForShutdown()
	s.run()
	return ShutdownReport{
		Cancelled:    cancelled,
		Acknowledged: s.acks - before,
		Unresolved:   a.UnresolvedExecutions(),
	}
}

// cancelAllTasksForShutdown cancels every live task reachable from the
// scope tree, the scheduler queue, the detached-task registry and the
// execution records, in a deterministic order (scope-tree creation
// order, then detached registration order, then queue order, then
// worker-start order).
func (a *App) cancelAllTasksForShutdown() int {
	seen := make(map[*taskRun]bool)
	var order []*taskRun
	add := func(t *taskRun) {
		if t == nil || t.done || seen[t] {
			return
		}
		seen[t] = true
		order = append(order, t)
	}
	var walk func(s *Scope)
	walk = func(s *Scope) {
		for _, t := range s.tasks {
			add(t)
		}
		for _, child := range s.children {
			walk(child)
		}
	}
	walk(a.rootScope)
	for _, t := range a.detachedTasks {
		add(t)
	}
	for _, t := range append([]*taskRun(nil), a.sched.queue...) {
		add(t)
	}
	for _, rec := range a.sched.executions {
		add(rec.task)
	}
	cancelled := 0
	for _, t := range order {
		if !t.cancelled {
			cancelled++
		}
		t.cancel()
	}
	return cancelled
}
