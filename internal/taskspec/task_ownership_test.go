// Package taskspec holds ticket04's scoped-task ownership tests: the
// deterministic scheduling seams, execution-resource retention until
// worker acknowledgment, owned result-message handoff with exactly one
// delivery or discard, generation gating of late results, the detached
// task registry, panic containment, the shutdown drain and the
// seeded/recorded schedule mode. They run from this external package on
// purpose, matching the repo convention of exercising the public gpui
// API from outside the runtime package.
package taskspec

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"gpui-go/gpui"
)

// box is the entity state used by the delivery tests.
type box struct{ value int64 }

// taskFixture is a small deterministic app with an event log.
type taskFixture struct {
	ta   *gpui.TestApp
	app  *gpui.App
	root *gpui.Scope
	log  []string
}

func newTaskFixture(t *testing.T) *taskFixture {
	t.Helper()
	ta := gpui.NewTestApp()
	return &taskFixture{ta: ta, app: ta.App(), root: ta.RootScope()}
}

func (f *taskFixture) record(event string) { f.log = append(f.log, event) }

func (f *taskFixture) recorded() []string { return f.log }

// newBox creates a box entity owned by scope.
func (f *taskFixture) newBox(scope *gpui.Scope, value int64) gpui.Entity[box] {
	return gpui.NewEntity(f.app, scope, func(b *box, _ *gpui.Context[box]) {
		*b = box{value: value}
	})
}

// expectPanic runs f and asserts it panics with a message containing
// want.
func expectPanic(t *testing.T, want string, f func()) {
	t.Helper()
	defer func() {
		t.Helper()
		r := recover()
		if r == nil {
			t.Fatalf("expected panic containing %q, got none", want)
		}
		msg, ok := r.(string)
		if !ok {
			if err, isErr := r.(error); isErr {
				msg = err.Error()
			} else {
				t.Fatalf("panic value is not a string: %v", r)
			}
		}
		if !strings.Contains(msg, want) {
			t.Fatalf("panic %q does not contain %q", msg, want)
		}
	}()
	f()
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

// ---------------------------------------------------------------------------
// Execution-resource retention until worker acknowledgment
// ---------------------------------------------------------------------------

// Contract: "Cancel() signals cancellation and closes result delivery,
// without waiting for worker exit ... Execution resources remain
// registered until the foreground acknowledges completion" — a cancelled
// worker mid-flight keeps its execution record tracked until it
// acknowledges; resources release at the acknowledgment, never at the
// cancellation.
func TestCancelledWorkerKeepsExecutionUntilAck(t *testing.T) {
	f := newTaskFixture(t)

	task := f.ta.Background().Spawn(func(run *gpui.TaskRun) int64 {
		f.record("started")
		run.Sleep(time.Hour)
		f.record("worker-exited")
		return 1
	})
	f.ta.RunUntilParked() // the worker starts and parks on the 1h timer

	task.Cancel()
	if task.Done() {
		t.Fatal("cancellation must not imply worker completion: task reported done before the worker exited")
	}

	unresolved := f.app.UnresolvedExecutions()
	if len(unresolved) != 1 {
		t.Fatalf("unresolved executions after cancel = %v, want exactly the cancelled worker's record", unresolved)
	}
	if !unresolved[0].Cancelled {
		t.Fatal("the unresolved record must report the cancellation signal")
	}
	if !strings.Contains(unresolved[0].String(), "cancelled, awaiting worker acknowledgment") {
		t.Fatalf("record diagnostic = %q", unresolved[0].String())
	}

	f.ta.RunUntilParked() // the worker wakes, observes cancellation, exits → acknowledgment

	if !task.Done() {
		t.Fatal("worker did not acknowledge after cancellation")
	}
	if got := f.recorded(); len(got) != 2 || got[1] != "worker-exited" {
		t.Fatalf("worker events = %v, want [started worker-exited]", got)
	}
	if got := f.app.UnresolvedExecutions(); len(got) != 0 {
		t.Fatalf("execution record released at cancel instead of at acknowledgment: %v", got)
	}
}

// Contract: cancellation of a task whose body never started retires it
// immediately (the acknowledgment is vacuous: no worker existed).
func TestCancelledBeforeStartRetiresImmediately(t *testing.T) {
	f := newTaskFixture(t)
	scope := f.root.Child()

	task := f.ta.Background().Spawn(func(run *gpui.TaskRun) int64 {
		f.record("started")
		return 1
	})
	task.TransferInto(scope)
	task.Cancel()

	if !task.Cancelled() || !task.Done() {
		t.Fatal("a cancelled-before-start task must retire immediately")
	}
	if got := f.recorded(); len(got) != 0 {
		t.Fatalf("cancelled-before-start body ran: %v", got)
	}
	if got := f.app.UnresolvedExecutions(); len(got) != 0 {
		t.Fatalf("no worker existed, yet executions remain unresolved: %v", got)
	}

	f.ta.RunUntilParked()
	scope.Close()
	if got := f.recorded(); len(got) != 0 {
		t.Fatalf("retired task ran after scope close: %v", got)
	}
}

// Foreground tasks are tracked the same way: an awaiter cancelled
// mid-flight keeps its record until it acknowledges.
func TestForegroundTaskExecutionRetentionUntilAck(t *testing.T) {
	f := newTaskFixture(t)

	background := f.ta.Background().Spawn(func(run *gpui.TaskRun) int64 {
		run.Sleep(time.Hour)
		return 1
	})
	foreground := f.app.Spawn(func(cx *gpui.AsyncApp) int64 {
		result := cx.Await(background)
		f.record("foreground-done")
		return result
	})
	f.ta.RunUntilParked() // both start; the awaiter parks on the worker

	foreground.Cancel()

	unresolved := f.app.UnresolvedExecutions()
	if len(unresolved) != 2 {
		t.Fatalf("unresolved executions after cancel = %v, want the cancelled awaiter and the worker", unresolved)
	}

	f.ta.RunUntilParked() // the awaiter wakes (released with no result) and acknowledges
	if !foreground.Done() {
		t.Fatal("the cancelled foreground task did not acknowledge")
	}
	if got := f.recorded(); len(got) != 1 || got[0] != "foreground-done" {
		t.Fatalf("foreground events = %v, want [foreground-done]", got)
	}

	// The uncancelled worker is still tracked and acknowledges when its
	// timer fires.
	unresolved = f.app.UnresolvedExecutions()
	if len(unresolved) != 1 || unresolved[0].Cancelled {
		t.Fatalf("worker record = %v, want one uncancelled record", unresolved)
	}
	f.ta.AdvanceClock(3600 * 1000)
	if got := f.app.UnresolvedExecutions(); len(got) != 0 {
		t.Fatalf("worker did not release its record at acknowledgment: %v", got)
	}
	if !background.Done() {
		t.Fatal("background worker did not complete")
	}
}

// A cancelled delivery task's execution scope keeps its declared
// dependencies alive until the worker acknowledges; they are released
// at the acknowledgment, not at the cancellation.
func TestCancelledDeliveryReleasesResourcesAtAck(t *testing.T) {
	f := newTaskFixture(t)
	target := f.root.Child()
	holder := f.root.Child()
	dep := f.newBox(holder, 9)
	weak := dep.Downgrade()

	task := f.app.SpawnDelivering(target,
		func(run *gpui.TaskRun) int64 {
			run.Sleep(time.Hour)
			return 1
		},
		func(result int64, _ *gpui.App) { f.record("delivered") },
		dep)
	f.ta.RunUntilParked() // the worker starts and parks

	task.Cancel()

	// Cancel released nothing: the execution scope still owns the
	// dependency's lease and the record is still tracked.
	if !weak.IsUpgradable() {
		t.Fatal("the dependency was released at cancellation: the execution scope must retain it while the worker is in flight")
	}
	if got := f.app.UnresolvedExecutions(); len(got) != 1 {
		t.Fatalf("execution record released at cancellation: %v", got)
	}

	f.ta.RunUntilParked() // the worker wakes and acknowledges → discard

	if !task.Done() {
		t.Fatal("cancelled delivery worker did not acknowledge")
	}
	if got := f.recorded(); len(got) != 0 {
		t.Fatalf("a cancelled delivery must not deliver: %v", got)
	}
	if got := f.app.UnresolvedExecutions(); len(got) != 0 {
		t.Fatalf("execution record not released at acknowledgment: %v", got)
	}
	f.ta.Update(func(_ *gpui.App) {}) // flush the queued release
	if weak.IsUpgradable() {
		t.Fatal("the dependency must release at the worker's acknowledgment")
	}
}

// ---------------------------------------------------------------------------
// Owned result-message handoff: exactly one delivery or discard
// ---------------------------------------------------------------------------

// Contract: "A result accepted before close executes normally" — the
// worker posts, the message is delivered into the still-open receiving
// scope, and both the message's dependencies and the execution record
// release afterwards.
func TestResultDeliveredBeforeClose(t *testing.T) {
	f := newTaskFixture(t)
	target := f.root.Child()
	dep := f.newBox(f.root, 3)
	weak := dep.Downgrade()

	deliveries := 0
	task := f.app.SpawnDelivering(target,
		func(run *gpui.TaskRun) int64 {
			f.record("worked")
			run.Sleep(50 * time.Millisecond)
			return 42
		},
		func(result int64, cx *gpui.App) {
			deliveries++
			f.record("delivered-" + itoa(result))
			cx.Defer(func(*gpui.App) { f.record("deferred-in-cycle") })
		},
		dep)
	f.ta.RunUntilParked() // starts and parks
	if got := f.app.UnresolvedExecutions(); len(got) != 1 {
		t.Fatalf("in-flight worker not tracked: %v", got)
	}

	f.ta.AdvanceClock(100) // fires the timer; the post delivers inline

	if got := f.recorded(); len(got) != 3 || got[0] != "worked" || got[1] != "delivered-42" || got[2] != "deferred-in-cycle" {
		t.Fatalf("delivery events = %v, want [worked delivered-42 deferred-in-cycle]", got)
	}
	if deliveries != 1 {
		t.Fatalf("deliveries = %d, want exactly one", deliveries)
	}
	if !task.Done() || task.Cancelled() {
		t.Fatal("delivered task must be done and not cancelled")
	}
	if got := f.app.UnresolvedExecutions(); len(got) != 0 {
		t.Fatalf("execution record not released after delivery: %v", got)
	}
	f.ta.Update(func(_ *gpui.App) {})
	if weak.IsUpgradable() {
		t.Fatal("the result message's dependency must release after delivery")
	}
}

// Contract: "Closing a scope suppresses late delivery ... The result
// scope closes after delivery or discard" — closing the receiving scope
// before the result discards the message and releases its dependencies
// at the worker's acknowledgment.
func TestResultDiscardedAfterScopeClose(t *testing.T) {
	f := newTaskFixture(t)
	target := f.root.Child()
	dep := f.newBox(f.root, 7)
	weak := dep.Downgrade()

	task := f.app.SpawnDelivering(target,
		func(run *gpui.TaskRun) int64 {
			f.record("worked")
			run.Sleep(50 * time.Millisecond)
			return 9
		},
		func(result int64, _ *gpui.App) { f.record("delivered") },
		dep)
	f.ta.RunUntilParked()

	target.Close() // cancels the task and closes the delivery gate

	f.ta.AdvanceClock(100) // the worker wakes and acknowledges → discard

	if got := f.recorded(); len(got) != 1 || got[0] != "worked" {
		t.Fatalf("events = %v, want the worker to run without delivering", got)
	}
	if !task.Done() || !task.Cancelled() {
		t.Fatal("the discarded task must be done and cancelled")
	}
	if got := f.app.UnresolvedExecutions(); len(got) != 0 {
		t.Fatalf("discard must release the execution record: %v", got)
	}
	f.ta.Update(func(_ *gpui.App) {})
	if weak.IsUpgradable() {
		t.Fatal("discard must release the message's dependencies")
	}
}

// Contract: "Delivery checks the current task delivery token, target
// generation and optional window token immediately before obtaining
// access ... Closing a scope suppresses late delivery even if the target
// entity has another live owner." The task's cancellation ownership was
// moved away, so the discard comes from the generation gate alone.
func TestLateResultDiscardedWhenScopeClosedEntityRetained(t *testing.T) {
	f := newTaskFixture(t)
	windowA := f.root.Child() // the receiving scope
	windowB := f.root.Child() // the other owner
	entity := f.newBox(windowA, 1)
	retained := entity.RetainInto(windowB)
	weak := entity.Downgrade()

	task := f.app.SpawnDeliveringInto(windowA, entity,
		func(run *gpui.TaskRun) int64 {
			run.Sleep(50 * time.Millisecond)
			return 5
		},
		func(state *box, result int64, _ *gpui.Context[box]) { f.record("delivered") })
	// Move cancellation ownership away: closing windowA must not cancel
	// the task, so the discard can only come from the delivery gate.
	task.TransferInto(windowB)
	f.ta.RunUntilParked()

	windowA.Close() // releases A's lease; the entity survives via B

	if !weak.IsUpgradable() {
		t.Fatal("the entity must survive windowA's close through windowB's independent lease")
	}

	f.ta.AdvanceClock(100)

	if got := f.recorded(); len(got) != 0 {
		t.Fatalf("late result delivered into a closed scope: %v", got)
	}
	if !task.Done() || task.Cancelled() {
		t.Fatal("the gated task must complete (not cancel) and discard")
	}
	if !weak.IsUpgradable() {
		t.Fatal("the other owner's lease must keep the entity alive after the discard")
	}
	if got := f.app.UnresolvedExecutions(); len(got) != 0 {
		t.Fatalf("the discard must release the execution record: %v", got)
	}
	retained.Release()
}

// Contract: a released entity target discards the late result even
// while the receiving scope stays open ("discard (owner closed/
// released)"): logical zero is irreversible, so the identity generation
// gate fails the delivery.
func TestLateResultDiscardedWhenEntityReleased(t *testing.T) {
	f := newTaskFixture(t)
	target := f.root.Child()
	holder := f.root.Child()
	entity := f.newBox(holder, 2)

	task := f.app.SpawnDeliveringInto(target, entity,
		func(run *gpui.TaskRun) int64 {
			run.Sleep(50 * time.Millisecond)
			return 5
		},
		func(state *box, result int64, _ *gpui.Context[box]) { f.record("delivered") })
	task.Detach() // cancellation ownership moves to the app registry
	f.ta.RunUntilParked()

	holder.Close() // releases the entity's only lease
	f.ta.Update(func(_ *gpui.App) {})

	f.ta.AdvanceClock(100)

	if got := f.recorded(); len(got) != 0 {
		t.Fatalf("late result delivered to a released entity: %v", got)
	}
	if !task.Done() || task.Cancelled() {
		t.Fatal("the detached task must complete (not cancel) and discard")
	}
	if target.State() != gpui.ScopeOpen {
		t.Fatal("the receiving scope was never closed")
	}
}

// Entity-target delivery updates the state through a fresh context
// inside one application update.
func TestDeliverIntoEntityUpdatesStateThroughFreshContext(t *testing.T) {
	f := newTaskFixture(t)
	target := f.root.Child()
	targetEntity := f.newBox(target, 0)

	task := f.app.SpawnDeliveringInto(target, targetEntity,
		func(run *gpui.TaskRun) int64 {
			run.Sleep(10 * time.Millisecond)
			return 42
		},
		func(state *box, result int64, cx *gpui.Context[box]) {
			if cx == nil {
				t.Fatal("delivery did not issue a fresh context")
			}
			state.value = result
			f.record("delivered")
		})
	f.ta.RunUntilParked()
	f.ta.AdvanceClock(20)

	if got := f.recorded(); len(got) != 1 || got[0] != "delivered" {
		t.Fatalf("delivery events = %v", got)
	}
	if v := targetEntity.Read(f.app, func(b *box, _ *gpui.App) int64 { return b.value }); v != 42 {
		t.Fatalf("entity value = %d, want 42", v)
	}
	if !task.Done() {
		t.Fatal("delivery task did not complete")
	}
}

// Contract: "exactly one of delivery ... or discard ... happens; both
// paths release the message and the execution record; idempotent" —
// after delivery, later churn (cancel, close, more driving) can neither
// deliver again nor run a discard.
func TestExactlyOnceDelivery(t *testing.T) {
	f := newTaskFixture(t)
	target := f.root.Child()

	deliveries := 0
	task := f.app.SpawnDelivering(target,
		func(run *gpui.TaskRun) int64 { return 1 },
		func(result int64, _ *gpui.App) { deliveries++ })

	f.ta.RunUntilParked()
	if deliveries != 1 {
		t.Fatalf("deliveries = %d, want 1", deliveries)
	}
	if !task.Done() {
		t.Fatal("task did not complete")
	}

	// Every subsequent churn must be inert.
	task.Cancel()
	task.Cancel()
	target.Close()
	f.ta.RunUntilParked()
	f.ta.AdvanceClock(1000)
	f.ta.RunUntilParked()

	if deliveries != 1 {
		t.Fatalf("deliveries = %d after churn, want exactly 1", deliveries)
	}
}

// The execution scope retains the task's dependencies while the worker
// runs even when the scope that originally owned them closes, and the
// result message releases them after delivery.
func TestDeliveryDependencyRetainedAcrossOwnerScopeClose(t *testing.T) {
	f := newTaskFixture(t)
	owner := f.root.Child()
	target := f.root.Child()
	dep := f.newBox(owner, 5)
	weak := dep.Downgrade()

	task := f.app.SpawnDelivering(target,
		func(run *gpui.TaskRun) int64 {
			run.Sleep(50 * time.Millisecond)
			return 1
		},
		func(result int64, _ *gpui.App) { f.record("delivered") },
		dep)
	f.ta.RunUntilParked()

	owner.Close() // the original owner closes; the execution scope holds the lease
	f.ta.Update(func(_ *gpui.App) {})

	if !weak.IsUpgradable() {
		t.Fatal("canceling the original owner's scope must not free memory the worker still uses")
	}

	f.ta.AdvanceClock(100)
	if got := f.recorded(); len(got) != 1 || got[0] != "delivered" {
		t.Fatalf("events = %v, want [delivered]", got)
	}
	f.ta.Update(func(_ *gpui.App) {})
	if weak.IsUpgradable() {
		t.Fatal("the dependency must release after delivery")
	}
	if !task.Done() {
		t.Fatal("task did not complete")
	}
}

// Contract: "Detach() atomically transfers cancellation ownership to the
// app's detached-task registry; it does not remove an explicitly
// window-bound result gate" — a detached delivery task delivers while
// the receiving scope is open, and still discards when it closed.
func TestDetachedDeliveryTaskGateSurvivesDetach(t *testing.T) {
	f := newTaskFixture(t)
	open := f.root.Child()   // receiving scope that stays open
	closed := f.root.Child() // receiving scope that will close

	delivered := f.app.SpawnDelivering(open,
		func(run *gpui.TaskRun) int64 {
			run.Sleep(10 * time.Millisecond)
			return 1
		},
		func(result int64, _ *gpui.App) { f.record("delivered") })
	delivered.Detach()

	discarded := f.app.SpawnDelivering(closed,
		func(run *gpui.TaskRun) int64 {
			run.Sleep(10 * time.Millisecond)
			return 2
		},
		func(result int64, _ *gpui.App) { f.record("delivered") })
	discarded.Detach()

	f.ta.RunUntilParked()
	closed.Close() // does not cancel the detached tasks
	f.ta.AdvanceClock(100)

	if got := f.recorded(); len(got) != 1 || got[0] != "delivered" {
		t.Fatalf("events = %v, want exactly one delivery (open scope) and one silent discard", got)
	}
	if !delivered.Done() || !discarded.Done() {
		t.Fatal("both detached tasks must complete")
	}
	if discarded.Cancelled() {
		t.Fatal("a detached task must not be cancelled by the receiving scope's close")
	}
}

// Misuse diagnostics: nil worker/delivery, closed receiving scope,
// callback-borrowed dependency, and post-shutdown spawn.
func TestSpawnDeliveringMisusePanics(t *testing.T) {
	f := newTaskFixture(t)
	target := f.root.Child()
	work := func(run *gpui.TaskRun) int64 { return 1 }
	deliver := func(result int64, _ *gpui.App) {}

	expectPanic(t, "nil worker", func() {
		f.app.SpawnDelivering(target, nil, deliver)
	})
	expectPanic(t, "nil delivery callback", func() {
		f.app.SpawnDelivering(target, work, nil)
	})
	expectPanic(t, "non-nil receiving scope", func() {
		f.app.SpawnDelivering(nil, work, deliver)
	})
	target.Close()
	expectPanic(t, "closing or closed", func() {
		f.app.SpawnDelivering(target, work, deliver)
	})

	entity := f.newBox(f.root, 1)
	entity.Update(f.app, func(_ *box, cx *gpui.Context[box]) {
		borrowed := cx.Entity()
		expectPanic(t, "callback-borrowed facades cannot be transferred", func() {
			f.app.SpawnDelivering(f.root, work, deliver, borrowed)
		})
	})

	f.app.Shutdown()
	expectPanic(t, "shutting down", func() {
		f.app.SpawnDelivering(f.root, work, deliver)
	})
}

// ---------------------------------------------------------------------------
// Panic containment
// ---------------------------------------------------------------------------

// Contract (ticket04): a worker panic fails the task, the execution
// record is still acknowledged and released, and the panic is surfaced
// to the panic hook — never a stuck record, no propagation.
func TestWorkerPanicContainedWithHook(t *testing.T) {
	f := newTaskFixture(t)
	target := f.root.Child()
	dep := f.newBox(f.root, 1)
	weak := dep.Downgrade()

	var panics []gpui.TaskPanic
	f.app.SetTaskPanicHook(func(p gpui.TaskPanic) {
		panics = append(panics, p)
		f.record("hook")
	})

	task := f.app.SpawnDelivering(target,
		func(run *gpui.TaskRun) int64 {
			f.record("worker")
			panic("worker boom")
		},
		func(result int64, _ *gpui.App) { f.record("delivered") },
		dep)

	f.ta.RunUntilParked() // the panic is contained; the hook reports it

	if len(panics) != 1 || panics[0].Value != "worker boom" {
		t.Fatalf("hook panics = %v, want one report of the recovered value", panics)
	}
	if panics[0].TaskID == 0 {
		t.Fatal("the hook must identify the failed task")
	}
	if !task.Failed() || !task.Done() {
		t.Fatal("a panicked worker fails the task and acknowledges completion")
	}
	if task.PanicValue() != "worker boom" {
		t.Fatalf("PanicValue = %v", task.PanicValue())
	}
	if got := f.recorded(); len(got) != 2 || got[0] != "worker" || got[1] != "hook" {
		t.Fatalf("events = %v, want [worker hook] with no delivery", got)
	}
	if got := f.app.UnresolvedExecutions(); len(got) != 0 {
		t.Fatalf("a contained panic must not leave a stuck record: %v", got)
	}
	f.ta.Update(func(_ *gpui.App) {})
	if weak.IsUpgradable() {
		t.Fatal("execution resources must release on a contained panic")
	}
}

// Without a hook the contained panic is re-raised on the dispatcher
// goroutine after the bookkeeping is consistent: the task is failed and
// done, and no record sticks.
func TestWorkerPanicReRaisedWithoutHook(t *testing.T) {
	f := newTaskFixture(t)
	target := f.root.Child()

	task := f.app.SpawnDelivering(target,
		func(run *gpui.TaskRun) int64 { panic("boom") },
		func(result int64, _ *gpui.App) { f.record("delivered") })

	func() {
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("worker panic was not surfaced without a hook")
			}
			if r != "boom" {
				t.Fatalf("surfaced panic = %v", r)
			}
		}()
		f.ta.RunUntilParked()
	}()

	if !task.Failed() || !task.Done() {
		t.Fatal("the re-raised task must still be failed and acknowledged")
	}
	if got := f.app.UnresolvedExecutions(); len(got) != 0 {
		t.Fatalf("no stuck record after a re-raised panic: %v", got)
	}
	if got := f.recorded(); len(got) != 0 {
		t.Fatalf("a failed worker must not deliver: %v", got)
	}
}

// Plain (non-delivery) workers keep the ticket03 re-raise behavior and
// additionally acknowledge their execution record.
func TestPlainWorkerPanicStillAcknowledges(t *testing.T) {
	f := newTaskFixture(t)

	task := f.ta.Background().Spawn(func(run *gpui.TaskRun) int64 {
		panic("plain boom")
	})
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("plain worker panic was not re-raised")
			}
		}()
		f.ta.RunUntilParked()
	}()

	if !task.Failed() || !task.Done() {
		t.Fatal("the plain panicked task must be failed and acknowledged")
	}
	if got := f.app.UnresolvedExecutions(); len(got) != 0 {
		t.Fatalf("no stuck record after a plain worker panic: %v", got)
	}
}

// A panic in the foreground delivery callback is application code: it
// propagates, but the handoff's bookkeeping still releases the result
// message and acknowledges the execution record on the way out — never
// a stuck record.
func TestDeliveryCallbackPanicLeavesNoStuckRecord(t *testing.T) {
	f := newTaskFixture(t)
	target := f.root.Child()
	dep := f.newBox(f.root, 1)
	weak := dep.Downgrade()

	task := f.app.SpawnDelivering(target,
		func(run *gpui.TaskRun) int64 { return 42 },
		func(result int64, _ *gpui.App) { panic("deliver boom") },
		dep)

	func() {
		defer func() {
			if recover() != "deliver boom" {
				t.Fatal("the delivery callback's panic did not propagate")
			}
		}()
		f.ta.RunUntilParked()
	}()

	if !task.Done() || task.Failed() {
		t.Fatal("the worker completed normally; only its delivery callback panicked")
	}
	if got := f.app.UnresolvedExecutions(); len(got) != 0 {
		t.Fatalf("a panicking delivery callback left a stuck record: %v", got)
	}
	f.ta.Update(func(_ *gpui.App) {})
	if weak.IsUpgradable() {
		t.Fatal("the result message's dependencies must release even when the delivery callback panics")
	}
}

// ---------------------------------------------------------------------------
// Nested scheduling: tasks spawning tasks
// ---------------------------------------------------------------------------

// A delivery callback (foreground code) spawns another delivery task;
// the nested task delivers its own result through the same machinery.
func TestNestedDeliveryTaskScheduling(t *testing.T) {
	f := newTaskFixture(t)
	target := f.root.Child()

	outer := f.app.SpawnDelivering(target,
		func(run *gpui.TaskRun) int64 {
			run.Sleep(5 * time.Millisecond)
			return 1
		},
		func(result int64, cx *gpui.App) {
			f.record("outer-delivered")
			f.app.SpawnDelivering(target,
				func(run *gpui.TaskRun) int64 {
					run.Sleep(5 * time.Millisecond)
					return result + 1
				},
				func(nested int64, _ *gpui.App) {
					f.record("nested-" + itoa(nested))
				})
		})

	f.ta.RunUntilParked()  // the outer task starts and parks
	f.ta.AdvanceClock(100) // outer completes+delivers; nested spawns, parks, completes+delivers

	if got := f.recorded(); len(got) != 2 || got[0] != "outer-delivered" || got[1] != "nested-2" {
		t.Fatalf("nested events = %v, want [outer-delivered nested-2]", got)
	}
	if !outer.Done() {
		t.Fatal("the outer task did not complete")
	}
	if got := f.app.UnresolvedExecutions(); len(got) != 0 {
		t.Fatalf("all nested tasks must acknowledge: %v", got)
	}
}

// A foreground task nests by spawning and awaiting background workers,
// and observes their results through foreground updates.
func TestNestedAwaitScheduling(t *testing.T) {
	f := newTaskFixture(t)

	workers := make([]gpui.Task[int64], 0, 3)
	f.app.Spawn(func(cx *gpui.AsyncApp) struct{} {
		sum := int64(0)
		for _, worker := range workers {
			sum += cx.Await(worker)
		}
		cx.Update(func(_ *gpui.App) { f.record("sum-" + itoa(sum)) })
		return struct{}{}
	}).Detach()

	for i := int64(1); i <= 3; i++ {
		value := i
		workers = append(workers, f.ta.Background().Spawn(func(run *gpui.TaskRun) int64 {
			run.Sleep(time.Duration(value) * 10 * time.Millisecond)
			return value
		}))
	}

	f.ta.RunUntilParked()
	f.ta.AdvanceClock(100)

	if got := f.recorded(); len(got) != 1 || got[0] != "sum-6" {
		t.Fatalf("nested await events = %v, want [sum-6]", got)
	}
	if got := f.app.UnresolvedExecutions(); len(got) != 0 {
		t.Fatalf("all nested tasks must acknowledge: %v", got)
	}
}

// ---------------------------------------------------------------------------
// Shutdown drain
// ---------------------------------------------------------------------------

// Contract: "app shutdown drains acknowledged tasks while unresolved
// execution resources remain TRACKED (reported, not silently dropped);
// never substitute finalizers or a timeout for release acknowledgment."
// A well-behaved worker acknowledges during the drain; a worker that
// ignores cancellation stays parked (no clock advance, no
// force-completion) and is reported; the scheduler keeps accepting its
// terminal completion afterwards.
func TestShutdownDrainsAcknowledgedAndTracksUnresolved(t *testing.T) {
	f := newTaskFixture(t)

	good := f.ta.Background().Spawn(func(run *gpui.TaskRun) int64 {
		f.record("good-started")
		run.Sleep(time.Hour)
		f.record("good-acknowledged")
		return 0
	})
	ignoring := f.ta.Background().Spawn(func(run *gpui.TaskRun) int64 {
		attempts := 3
		for attempts > 0 {
			run.Sleep(time.Hour) // observes cancellation and keeps re-sleeping
			f.record("ignoring-wake")
			attempts--
		}
		f.record("ignoring-done")
		return 0
	})
	// The registry path: a detached task is cancelled at shutdown through
	// the app's detached-task registry and keeps its entry until the
	// worker's late acknowledgment removes it.
	ignoring.Detach()
	target := f.root.Child()
	dep := f.newBox(f.root, 4)
	weak := dep.Downgrade()
	delivery := f.app.SpawnDelivering(target,
		func(run *gpui.TaskRun) int64 {
			f.record("delivery-worked")
			run.Sleep(time.Hour)
			return 1
		},
		func(result int64, _ *gpui.App) { f.record("delivered") },
		dep)

	f.ta.RunUntilParked() // all three start and park
	// An unstarted task spawned after the drain-point: it must be
	// cancelled and retired by shutdown without its body ever running.
	unstarted := f.ta.Background().Spawn(func(run *gpui.TaskRun) int64 {
		f.record("never")
		return 0
	})

	report := f.app.Shutdown()

	if report.Cancelled != 4 {
		t.Fatalf("cancelled = %d, want 4 (good, ignoring, delivery, unstarted)", report.Cancelled)
	}
	// Acknowledged during the drain: unstarted (vacuous retirement),
	// good and delivery workers.
	if report.Acknowledged != 3 {
		t.Fatalf("acknowledged = %d, want 3", report.Acknowledged)
	}
	if len(report.Unresolved) != 1 {
		t.Fatalf("unresolved = %v, want exactly the cancellation-ignoring worker", report.Unresolved)
	}
	u := report.Unresolved[0]
	if !u.Cancelled || !strings.Contains(u.Parked, "parked on timer") {
		t.Fatalf("unresolved record = %v", u)
	}
	for _, want := range []string{"good-acknowledged", "delivery-worked"} {
		if !strings.Contains(strings.Join(f.recorded(), " "), want) {
			t.Fatalf("drain did not run %q: %v", want, f.recorded())
		}
	}
	if strings.Contains(strings.Join(f.recorded(), " "), "delivered") {
		t.Fatalf("cancelled delivery must discard, not deliver: %v", f.recorded())
	}
	if strings.Contains(strings.Join(f.recorded(), " "), "never") {
		t.Fatalf("unstarted task body ran during shutdown: %v", f.recorded())
	}
	if strings.Contains(strings.Join(f.recorded(), " "), "ignoring-done") {
		t.Fatalf("shutdown must not force-complete an ignoring worker (no timeout substitution): %v", f.recorded())
	}

	// The unresolved resource stays tracked after shutdown, not dropped.
	if got := f.app.UnresolvedExecutions(); len(got) != 1 || got[0].TaskID != u.TaskID {
		t.Fatalf("unresolved executions after shutdown = %v", got)
	}
	// The dependency of the discarded delivery released at its
	// acknowledgment.
	f.ta.Update(func(_ *gpui.App) {})
	if weak.IsUpgradable() {
		t.Fatal("the discarded delivery's dependency must release at acknowledgment")
	}

	// Terminal completion messages are still accepted: the ignoring
	// worker eventually acknowledges through the clock (the test harness
	// driving completions, not a timeout the runtime substitutes).
	f.ta.AdvanceClock(3 * 3600 * 1000)
	if !strings.Contains(strings.Join(f.recorded(), " "), "ignoring-done") {
		t.Fatalf("the ignoring worker never completed: %v", f.recorded())
	}
	if got := f.app.UnresolvedExecutions(); len(got) != 0 {
		t.Fatalf("the late acknowledgment must release the record: %v", got)
	}
	if !ignoring.Done() || !good.Done() || !delivery.Done() || !unstarted.Done() {
		t.Fatal("all shutdown tasks must end done")
	}

	// Idempotent: a second shutdown reports the same terminal state.
	if second := f.app.Shutdown(); len(second.Unresolved) != 0 || second.Cancelled != 0 {
		t.Fatalf("second shutdown = %+v, want an idempotent empty report", second)
	}
}

// Contract: "Shutdown continues accepting terminal completion/retirement
// messages after rejecting new application work."
func TestShutdownRejectsNewWorkButAcceptsCompletions(t *testing.T) {
	f := newTaskFixture(t)

	worker := f.ta.Background().Spawn(func(run *gpui.TaskRun) int64 {
		run.Sleep(10 * time.Millisecond)
		f.record("completed")
		return 0
	})
	f.app.Shutdown()

	expectPanic(t, "shutting down", func() {
		f.app.Spawn(func(cx *gpui.AsyncApp) struct{} { return struct{}{} })
	})
	expectPanic(t, "shutting down", func() {
		f.ta.Background().Spawn(func(run *gpui.TaskRun) struct{} { return struct{}{} })
	})
	expectPanic(t, "shutting down", func() {
		f.app.SpawnDelivering(f.root,
			func(run *gpui.TaskRun) struct{} { return struct{}{} },
			func(struct{}, *gpui.App) {})
	})
	// The worker was cancelled and acknowledged during the drain.
	if !worker.Done() {
		t.Fatal("the drained worker did not acknowledge")
	}
	// Foreground updates still run: completion/retirement machinery.
	f.app.Update(func(_ *gpui.App) { f.record("updated") })
	if !strings.Contains(strings.Join(f.recorded(), " "), "updated") {
		t.Fatalf("updates after shutdown = %v", f.recorded())
	}
}

// ---------------------------------------------------------------------------
// Seeded/recorded schedules
// ---------------------------------------------------------------------------

// runSeededScenario builds one deterministic app under the given seed:
// five workers that start, park on equal timers and complete, so both
// the drive order and the observable event order depend on the seed.
func runSeededScenario(t *testing.T, seed uint64) ([]gpui.ScheduleStep, []string) {
	t.Helper()
	f := newTaskFixture(t)
	f.ta.SetScheduleSeed(seed)
	for i := 0; i < 5; i++ {
		label := strconv.Itoa(i)
		f.ta.Background().Spawn(func(run *gpui.TaskRun) int64 {
			f.record("start-" + label)
			run.Sleep(10 * time.Millisecond)
			f.record("done-" + label)
			return 0
		})
	}
	f.ta.RunUntilParked()
	f.ta.AdvanceClock(100)
	return f.ta.ScheduleTrace(), f.recorded()
}

// Determinism policy: the same seed, the same spawn order and the same
// timer schedule reproduce the identical recorded interleaving and
// observable event order.
func TestSeededScheduleIsReproducible(t *testing.T) {
	traceA, eventsA := runSeededScenario(t, 7)
	traceB, eventsB := runSeededScenario(t, 7)

	if len(traceA) == 0 {
		t.Fatal("seeded schedule recorded no drives")
	}
	if !reflect.DeepEqual(traceA, traceB) {
		t.Fatalf("same seed produced different schedules:\n%v\n%v", traceA, traceB)
	}
	if !reflect.DeepEqual(eventsA, eventsB) {
		t.Fatalf("same seed produced different event orders:\n%v\n%v", eventsA, eventsB)
	}
	for i, step := range traceA {
		if step.Step != i+1 {
			t.Fatalf("step %d has ordinal %d", i, step.Step)
		}
		if step.TaskID == 0 {
			t.Fatalf("step %d references a zero task identity", i)
		}
	}
	// Seed 0 restores the default FIFO mode and records nothing.
	f := newTaskFixture(t)
	f.ta.SetScheduleSeed(1)
	f.ta.Background().Spawn(func(run *gpui.TaskRun) struct{} { return struct{}{} })
	f.ta.RunUntilParked()
	f.ta.SetScheduleSeed(0)
	f.ta.Background().Spawn(func(run *gpui.TaskRun) struct{} { return struct{}{} })
	f.ta.RunUntilParked()
	if got := f.ta.ScheduleTrace(); len(got) != 0 {
		t.Fatalf("FIFO mode recorded drives: %v", got)
	}
}

// Different seeds select genuinely different interleavings.
func TestSeededScheduleDiffersAcrossSeeds(t *testing.T) {
	traceA, eventsA := runSeededScenario(t, 7)
	traceB, eventsB := runSeededScenario(t, 8)

	if reflect.DeepEqual(traceA, traceB) {
		t.Fatalf("seeds 7 and 8 produced identical schedules: %v", traceA)
	}
	if reflect.DeepEqual(eventsA, eventsB) {
		t.Fatalf("seeds 7 and 8 produced identical event orders: %v", eventsA)
	}
}

// Seeded interleavings preserve the ownership invariants: exactly-once
// delivery, discard on close, and no stuck execution records.
func TestSeededSchedulePreservesDeliveryInvariants(t *testing.T) {
	f := newTaskFixture(t)
	f.ta.SetScheduleSeed(42)

	// Round 1: a closed receiving scope discards every late result.
	closed := f.root.Child()
	var deliveries int
	tasks := make([]gpui.Task[int64], 0, 4)
	for i := 0; i < 4; i++ {
		delay := time.Duration(10+i*5) * time.Millisecond
		tasks = append(tasks, f.app.SpawnDelivering(closed,
			func(run *gpui.TaskRun) int64 {
				run.Sleep(delay)
				return int64(i)
			},
			func(result int64, _ *gpui.App) { deliveries++ }))
	}
	f.ta.RunUntilParked()
	tasks[1].Cancel() // one cancelled mid-flight
	closed.Close()
	f.ta.AdvanceClock(100)

	if deliveries != 0 {
		t.Fatalf("closed scope delivered %d results under a seeded schedule", deliveries)
	}
	for i, task := range tasks {
		if !task.Done() {
			t.Fatalf("task %d did not acknowledge under a seeded schedule", i)
		}
	}
	if got := f.app.UnresolvedExecutions(); len(got) != 0 {
		t.Fatalf("seeded scheduling left unresolved executions: %v", got)
	}

	// Round 2: an open scope delivers each result exactly once.
	open := f.root.Child()
	for i := 0; i < 4; i++ {
		delay := time.Duration(10+i*5) * time.Millisecond
		result := int64(i)
		f.app.SpawnDelivering(open,
			func(run *gpui.TaskRun) int64 {
				run.Sleep(delay)
				return result
			},
			func(value int64, _ *gpui.App) { deliveries++ })
	}
	f.ta.RunUntilParked()
	f.ta.AdvanceClock(100)

	if deliveries != 4 {
		t.Fatalf("deliveries = %d, want exactly 4", deliveries)
	}
	if got := f.app.UnresolvedExecutions(); len(got) != 0 {
		t.Fatalf("seeded scheduling left unresolved executions: %v", got)
	}
}
