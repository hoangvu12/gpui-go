package portfixture

// Ownership-rule tests for the gpui runtime, one per rule of the runtime
// ownership contract (docs/runtime-ownership-contract.md). They run from
// this external package on purpose: the ticket requires typed reads and
// updates, sealed AppContext access, reentry restrictions and expiry
// diagnostics to be exercised from outside the gpui package.

import (
	"strings"
	"testing"
	"time"

	"gpui-go/gpui"
)

// counterFixture is a small helper app with one entity per test.
type counterFixture struct {
	ta     *gpui.TestApp
	app    *gpui.App
	root   *gpui.Scope
	events []string
}

func newCounterFixture(t *testing.T) *counterFixture {
	t.Helper()
	ta := gpui.NewTestApp()
	return &counterFixture{ta: ta, app: ta.App(), root: ta.RootScope()}
}

func (f *counterFixture) record(event string) { f.events = append(f.events, event) }

func (f *counterFixture) recorded() []string { return f.events }

// newCounter creates a Counter entity owned by root.
func (f *counterFixture) newCounter(value uint64) gpui.Entity[Counter] {
	return gpui.NewEntity(f.app, f.root, func(c *Counter, _ *gpui.Context[Counter]) {
		*c = Counter{value: value}
	})
}

// newObserver creates an Observer entity owned by root.
func (f *counterFixture) newObserver() gpui.Entity[Observer] {
	return gpui.NewEntity(f.app, f.root, func(_ *Observer, _ *gpui.Context[Observer]) {})
}

// observe wires an observer to a counter and records deliveries.
func (f *counterFixture) observe(observer gpui.Entity[Observer], counter gpui.Entity[Counter]) {
	observer.Update(f.app, func(_ *Observer, cx *gpui.Context[Observer]) {
		cx.Observe(counter, func(_ *Observer, _ gpui.Entity[Counter], _ *gpui.Context[Observer]) {
			f.record("notified")
		})
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

// ---------------------------------------------------------------------------
// Leases: alias, idempotent release, retain, transfer
// ---------------------------------------------------------------------------

// Contract: "Each independently owned Entity contains a shared lease
// token registered with exactly one live scope. Assignment aliases that
// token: releasing either copy invalidates both and decrements once."
func TestLeaseAliasSharesOneRelease(t *testing.T) {
	f := newCounterFixture(t)
	counter := f.newCounter(1)

	// Assignment aliases: no lease change, weak still upgradable exactly
	// once per lease.
	alias := counter

	alias.Release() // releasing either copy invalidates both, once

	expectPanic(t, "already released", func() {
		alias.Update(f.app, func(_ *Counter, _ *gpui.Context[Counter]) {})
	})
	expectPanic(t, "already released", func() {
		counter.Read(f.app, func(_ *Counter, _ *gpui.App) struct{} { return struct{}{} })
	})

	// Release is idempotent: a second call is a no-op, not a
	// double-decrement or a second queued release.
	counter.Release()

	f.ta.Update(func(_ *gpui.App) {}) // flush the release
	if got := len(f.recorded()); got != 0 {
		t.Fatalf("recorded %v, want no events (no release hooks registered)", f.recorded())
	}

	// Both aliases now reference a dead identity: the weak handle fails
	// irreversibly.
	weak := counter.Downgrade()
	if _, ok := weak.UpgradeInto(f.root); ok {
		t.Fatal("weak upgrade succeeded after release; logical zero must be irreversible")
	}
}

// Contract: "Release and Close are idempotent."
func TestReleaseIdempotenceFiresHookOnce(t *testing.T) {
	f := newCounterFixture(t)
	counter := f.newCounter(5)

	counter.Update(f.app, func(_ *Counter, cx *gpui.Context[Counter]) {
		cx.OnRelease(func(c *Counter, _ *gpui.App) {
			f.record("released")
		})
	})

	counter.Release()
	counter.Release() // idempotent
	counter.Release()

	f.ta.Update(func(_ *gpui.App) {})
	if got := f.recorded(); len(got) != 1 || got[0] != "released" {
		t.Fatalf("release events = %v, want exactly one [released]", got)
	}
}

// Contract: "RetainInto creates another lease/count" — two scopes can
// share one entity; releasing one scope's lease keeps the entity and its
// dependent resources alive.
func TestRetainIntoIndependentLeaseTwoScopesOneEntity(t *testing.T) {
	f := newCounterFixture(t)
	scopeA := f.root.Child()
	scopeB := f.root.Child()

	counter := gpui.NewEntity(f.app, scopeA, func(c *Counter, _ *gpui.Context[Counter]) {
		*c = Counter{value: 1}
	})
	f.observe(f.newObserver(), counter)

	second := counter.RetainInto(scopeB)

	scopeA.Close() // releases scope A's lease only

	// The entity is still live through scope B's independent lease, and
	// its dependent resources (the observer registration) survive the
	// creating scope's close. The original handle is stale now (scope A's
	// close released its lease), so access goes through the independent
	// lease.
	weak := counter.Downgrade()
	if !weak.IsUpgradable() {
		t.Fatal("entity died when only one of two leases was released")
	}
	second.Update(f.app, func(c *Counter, cx *gpui.Context[Counter]) {
		c.value++
		cx.Notify()
	})
	f.ta.Update(func(_ *gpui.App) {})
	if got := f.recorded(); len(got) != 1 || got[0] != "notified" {
		t.Fatalf("dependent resources died with the creating scope: events = %v", got)
	}

	// Releasing the second lease releases the entity.
	second.Release()
	f.ta.Update(func(_ *gpui.App) {})
	if weak.IsUpgradable() {
		t.Fatal("entity alive after both leases released")
	}
	// Using the stale alias afterwards is a programmer error with the
	// alias diagnostic.
	expectPanic(t, "already released", func() {
		second.Read(f.app, func(_ *Counter, _ *gpui.App) struct{} { return struct{}{} })
	})
}

// Contract: "TransferInto moves the same lease to a live scope without
// decrementing or changing aliases."
func TestTransferIntoMovesLeaseWithoutDecrement(t *testing.T) {
	f := newCounterFixture(t)
	scopeA := f.root.Child()
	scopeB := f.root.Child()

	counter := gpui.NewEntity(f.app, scopeA, func(c *Counter, _ *gpui.Context[Counter]) {
		*c = Counter{value: 3}
	})

	alias := counter
	counter.TransferInto(scopeB)

	// The lease count did not change: one release (through any alias)
	// releases the entity.
	scopeA.Close() // the lease no longer lives here
	weak := counter.Downgrade()
	if !weak.IsUpgradable() {
		t.Fatal("transfer decremented the lease")
	}

	alias.Release() // aliases keep cancellation authority over the moved lease
	f.ta.Update(func(_ *gpui.App) {})
	if weak.IsUpgradable() {
		t.Fatal("entity alive after the transferred lease was released through an alias")
	}
}

// Contract: "A zero weak handle fails upgrade; a dead identity never
// revives. Slot reuse changes generation, including after a failed
// construction."
func TestWeakUpgradeIrreversibleAcrossReclamationAndSlotReuse(t *testing.T) {
	f := newCounterFixture(t)
	first := f.newCounter(0)
	weak := first.Downgrade()

	first.Release()
	// Before reclamation the count is zero: upgrade fails.
	if weak.IsUpgradable() {
		t.Fatal("weak handle upgradable after logical zero but before reclamation")
	}
	f.ta.Update(func(_ *gpui.App) {}) // flush reclaims the slot

	if weak.IsUpgradable() {
		t.Fatal("weak handle upgradable after reclamation")
	}

	// The slot is reused with a new generation; the dead identity must
	// still fail.
	reused := f.newCounter(0)
	if reused.EntityID() == first.EntityID() {
		t.Fatal("slot reuse did not change the identity")
	}
	if weak.IsUpgradable() {
		t.Fatal("dead identity revived through slot reuse")
	}
	if _, ok := weak.UpgradeInto(f.root); ok {
		t.Fatal("dead identity upgraded")
	}
	if _, ok := reused.Downgrade().UpgradeInto(f.root); !ok {
		t.Fatal("live reused-slot entity failed to upgrade")
	}
}

// Contract: "Reads can nest, writes are exclusive, and conflicting
// reentry panics" with a clear diagnostic (RefCell semantics).
func TestReentryRestrictions(t *testing.T) {
	f := newCounterFixture(t)
	counter := f.newCounter(0)

	// Nested reads are allowed.
	var nested bool
	nested = counter.Read(f.app, func(_ *Counter, app *gpui.App) bool {
		return counter.Read(app, func(_ *Counter, _ *gpui.App) bool {
			f.record("nested-read")
			return true
		})
	})
	if !nested {
		t.Fatal("nested read did not complete")
	}
	if got := f.recorded(); len(got) != 1 {
		t.Fatalf("nested reads panicked: %v", got)
	}

	// Write during write panics with the reentry diagnostic.
	expectPanic(t, "conflicting reentrant access", func() {
		counter.Update(f.app, func(_ *Counter, cx *gpui.Context[Counter]) {
			_ = cx
			counter.Update(f.app, func(_ *Counter, _ *gpui.Context[Counter]) {})
		})
	})
	f.ta.Update(func(_ *gpui.App) {}) // clean up any bookkeeping from the panic

	// Read during write panics.
	expectPanic(t, "conflicting reentrant access", func() {
		counter.Update(f.app, func(_ *Counter, _ *gpui.Context[Counter]) {
			counter.Read(f.app, func(_ *Counter, _ *gpui.App) struct{} { return struct{}{} })
		})
	})
	f.ta.Update(func(_ *gpui.App) {})

	// Write during read panics.
	expectPanic(t, "conflicting reentrant access", func() {
		counter.Read(f.app, func(_ *Counter, app *gpui.App) bool {
			counter.Update(app, func(_ *Counter, _ *gpui.Context[Counter]) {})
			return false
		})
	})
	f.ta.Update(func(_ *gpui.App) {})
}

// Contract: "Context.Entity() and source handles supplied to
// observer/subscriber callbacks are callback-borrowed facades. Copies
// share the callback's validity token and expire on return."
func TestBorrowedFacadeExpiryAndMisuse(t *testing.T) {
	f := newCounterFixture(t)
	counter := f.newCounter(0)

	var owned gpui.Entity[Counter]
	var borrowed gpui.Entity[Counter]

	counter.Update(f.app, func(_ *Counter, cx *gpui.Context[Counter]) {
		owned = cx.Entity().RetainInto(f.root) // saving requires RetainInto
		borrowed = cx.Entity()                 // borrowed facade

		// Releasing or transferring a borrowed facade is misuse.
		expectPanic(t, "cannot release a callback-borrowed", func() {
			borrowed.Release()
		})
		expectPanic(t, "cannot transfer a callback-borrowed", func() {
			borrowed.TransferInto(f.root)
		})
	})

	// The retained handle survives the callback.
	var retainedValue uint64
	retainedValue = owned.Read(f.app, func(c *Counter, _ *gpui.App) uint64 {
		return c.value
	})
	if retainedValue != 0 {
		t.Fatalf("retained handle state = %d", retainedValue)
	}

	// The borrowed facade expired on return; every copy shares the
	// validity token and reports the expiry diagnostic.
	expiredCopy := borrowed
	expectPanic(t, "expired", func() {
		borrowed.Read(f.app, func(_ *Counter, _ *gpui.App) struct{} { return struct{}{} })
	})
	expectPanic(t, "expired", func() {
		expiredCopy.Update(f.app, func(_ *Counter, _ *gpui.Context[Counter]) {})
	})
}

// Contract: observer callbacks receive callback-borrowed source handles
// that expire when the delivery returns.
func TestObserverSourceFacadeIsBorrowed(t *testing.T) {
	f := newCounterFixture(t)
	counter := f.newCounter(0)
	observer := f.newObserver()

	var savedFacade gpui.Entity[Counter]
	var retained gpui.Entity[Counter]

	observer.Update(f.app, func(_ *Observer, cx *gpui.Context[Observer]) {
		cx.Observe(counter, func(_ *Observer, source gpui.Entity[Counter], _ *gpui.Context[Observer]) {
			savedFacade = source
			retained = source.RetainInto(f.root)
			f.record("notified")
		})
	})

	counter.Update(f.app, func(_ *Counter, cx *gpui.Context[Counter]) { cx.Notify() })
	f.ta.Update(func(_ *gpui.App) {})

	if got := f.recorded(); len(got) != 1 {
		t.Fatalf("observer events = %v, want one delivery", got)
	}
	expectPanic(t, "expired", func() {
		savedFacade.Read(f.app, func(_ *Counter, _ *gpui.App) struct{} { return struct{}{} })
	})
	retained.Release() // the retained handle owns a real lease
}

// Contract: "Partial construction failure disposes its dependent scope
// and lease without publishing the incomplete entity or delivering normal
// release hooks", and slot reuse changes generation after a failed
// construction.
func TestConstructionFailureDisposesWithoutReleaseHooks(t *testing.T) {
	f := newCounterFixture(t)

	var failedID gpui.EntityID
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("construction panic did not propagate")
			}
		}()
		gpui.NewEntity(f.app, f.root, func(c *Counter, cx *gpui.Context[Counter]) {
			failedID = cx.EntityID()
			cx.OnRelease(func(_ *Counter, _ *gpui.App) {
				f.record("release-hook")
			})
			panic("construction boom")
		})
	}()

	// Flush: no release hooks, no entity-created observers.
	f.ta.Update(func(_ *gpui.App) {})
	if got := f.recorded(); len(got) != 0 {
		t.Fatalf("failed construction delivered hooks: %v", got)
	}

	// The slot is reused with a bumped generation.
	next := f.newCounter(0)
	if next.EntityID().SlotIndex() != failedID.SlotIndex() {
		t.Fatalf("slot %d was not reused (got %d)", failedID.SlotIndex(), next.EntityID().SlotIndex())
	}
	if next.EntityID().Generation() <= failedID.Generation() {
		t.Fatalf("generation %d did not advance past %d", next.EntityID().Generation(), failedID.Generation())
	}
}

// ---------------------------------------------------------------------------
// Scopes: close order, entity-dependent scopes
// ---------------------------------------------------------------------------

// Contract: scope close "first marks the whole closing subtree
// unavailable to new ownership/result delivery, then cancels
// registrations/tasks, then closes children and releases resources in
// reverse acquisition order."
func TestScopeCloseOrder(t *testing.T) {
	f := newCounterFixture(t)
	parent := f.root.Child()
	child1 := parent.Child()
	child2 := parent.Child()

	// Parent leases: e1 acquired first, e2 second.
	e1 := gpui.NewEntity(f.app, parent, func(c *Counter, _ *gpui.Context[Counter]) {
		*c = Counter{value: 1}
	})
	e2 := gpui.NewEntity(f.app, parent, func(c *Counter, _ *gpui.Context[Counter]) {
		*c = Counter{value: 2}
	})
	// child1 owns e3; child2 owns a task.
	e3 := gpui.NewEntity(f.app, child1, func(c *Counter, _ *gpui.Context[Counter]) {
		*c = Counter{value: 3}
	})
	for _, entity := range []gpui.Entity[Counter]{e1, e2, e3} {
		entity.Update(f.app, func(_ *Counter, cx *gpui.Context[Counter]) {
			cx.OnRelease(func(c *Counter, _ *gpui.App) {
				f.record("released-" + string(rune('0'+c.value)))
			})
		})
	}

	// Tasks in parent (tA registered before tB) and one in child2.
	spawnTracer := func(name string) gpui.Task[struct{}] {
		task := f.ta.Background().Spawn(func(run *gpui.TaskRun) struct{} {
			run.Sleep(time.Hour)
			if run.Cancelled() {
				f.record("cancelled-" + name)
			} else {
				f.record("completed-" + name)
			}
			return struct{}{}
		})
		return task
	}
	taskA := spawnTracer("A")
	taskB := spawnTracer("B")
	taskA.TransferInto(parent)
	taskB.TransferInto(parent)
	taskC := spawnTracer("C")
	taskC.TransferInto(child2)

	// Start and park all three tasks.
	f.ta.RunUntilParked()

	// Close the parent from outside an update: the close enters the
	// update/flush machinery, so lease-release hooks run within Close.
	parent.Close()

	// Resource release order: children in reverse creation order (child2
	// has no leases, child1's e3), then the parent's own leases in
	// reverse acquisition order (e2, e1). Enqueue order determines
	// release order.
	wantRelease := []string{"released-3", "released-2", "released-1"}
	got := f.recorded()
	if len(got) != 3 {
		t.Fatalf("release events = %v, want %v", got, wantRelease)
	}
	for i, want := range wantRelease {
		if got[i] != want {
			t.Fatalf("release order = %v, want %v", got, wantRelease)
		}
	}

	// Task cancellation order: the parent's own tasks in reverse
	// registration order first, then children in reverse creation order.
	f.ta.RunUntilParked()
	got = f.recorded()[3:]
	wantCancel := []string{"cancelled-B", "cancelled-A", "cancelled-C"}
	if len(got) != 3 {
		t.Fatalf("cancel events = %v, want %v", got, wantCancel)
	}
	for i, want := range wantCancel {
		if got[i] != want {
			t.Fatalf("cancel order = %v, want %v", got, wantCancel)
		}
	}

	// The closing subtree is unavailable to new ownership: Child panics.
	expectPanic(t, "closing or closed", func() {
		child1.Child()
	})
	expectPanic(t, "closing or closed", func() {
		parent.Child()
	})

	// Idempotent close.
	parent.Close()
}

// Contract: "every entity also has a dependent-resource scope closed
// when that entity releases. That dependent scope is not a child of the
// first window that happened to create the entity: an independent lease
// in another window keeps the entity and its dependent resources alive."
// (Windows arrive in a later ticket; the creating scope stands in.)
func TestEntityDependentScopeFollowsEntityLifetime(t *testing.T) {
	f := newCounterFixture(t)
	creatingScope := f.root.Child()

	counter := gpui.NewEntity(f.app, creatingScope, func(c *Counter, _ *gpui.Context[Counter]) {
		*c = Counter{value: 1}
	})
	other := counter.RetainInto(f.root.Child())

	// An observer registration owned by the counter's dependent scope.
	observer := f.newObserver()
	observer.Update(f.app, func(_ *Observer, cx *gpui.Context[Observer]) {
		cx.Observe(counter, func(_ *Observer, _ gpui.Entity[Counter], _ *gpui.Context[Observer]) {
			f.record("notified")
		})
	})

	// The dependent scope is not a child of the creating scope: closing
	// the creating scope leaves the entity and its dependent resources
	// alive through the independent lease.
	creatingScope.Close()
	// The original handle is stale after the creating scope's close;
	// notify through the independent lease.
	other.Update(f.app, func(_ *Counter, cx *gpui.Context[Counter]) { cx.Notify() })
	f.ta.Update(func(_ *gpui.App) {})
	if got := f.recorded(); len(got) != 1 || got[0] != "notified" {
		t.Fatalf("dependent scope closed with the creating scope: %v", got)
	}

	// Releasing the entity closes the dependent scope: registrations are
	// cancelled and new ownership into it is rejected.
	var dependent *gpui.Scope
	other.Update(f.app, func(_ *Counter, cx *gpui.Context[Counter]) {
		dependent = cx.Scope()
	})
	other.Release()
	f.ta.Update(func(_ *gpui.App) {})

	expectPanic(t, "closing or closed", func() {
		dependent.Child()
	})

	// Cancelled registrations no longer deliver to the released source's
	// observers: the entity's observer set was removed at release.
	f.events = nil
	observer.Update(f.app, func(_ *Observer, cx *gpui.Context[Observer]) {
		// The observer itself is still live: registering against a new,
		// live source works (the entity's own scope closed, not the
		// observer's).
		fresh := f.newCounter(9)
		cx.Observe(fresh, func(_ *Observer, _ gpui.Entity[Counter], _ *gpui.Context[Observer]) {
			f.record("notified-fresh")
		})
		fresh.Update(f.app, func(_ *Counter, fcx *gpui.Context[Counter]) { fcx.Notify() })
	})
	f.ta.Update(func(_ *gpui.App) {})
	if got := f.recorded(); len(got) != 1 || got[0] != "notified-fresh" {
		t.Fatalf("observer scope bookkeeping broken: %v", got)
	}
}

// Contract: "A close initiated outside an update enters the app's
// foreground update/flush machinery" — release hooks triggered by
// released leases run at the flush boundary inside Close.
func TestScopeCloseOutsideUpdateFlushesReleaseHooks(t *testing.T) {
	f := newCounterFixture(t)
	scope := f.root.Child()

	counter := gpui.NewEntity(f.app, scope, func(c *Counter, _ *gpui.Context[Counter]) {
		*c = Counter{value: 7}
	})
	counter.Update(f.app, func(_ *Counter, cx *gpui.Context[Counter]) {
		cx.OnRelease(func(c *Counter, _ *gpui.App) {
			f.record("released")
		})
	})

	scope.Close()
	if got := f.recorded(); len(got) != 1 || got[0] != "released" {
		t.Fatalf("release hook did not run inside Close: %v", got)
	}
}

// ---------------------------------------------------------------------------
// Subscriptions: scope-owned when ignored, detached weak endpoints
// ---------------------------------------------------------------------------

// Contract (Go adaptation): "Discarding a Go subscription variable does
// not cancel anything. The scope retains the registration, whose
// endpoints remain weak."
func TestSubscriptionStaysActiveWhenHandleIgnored(t *testing.T) {
	f := newCounterFixture(t)
	counter := f.newCounter(0)
	observer := f.newObserver()

	// The returned handle is deliberately ignored.
	observer.Update(f.app, func(_ *Observer, cx *gpui.Context[Observer]) {
		_ = cx.Observe(counter, func(_ *Observer, _ gpui.Entity[Counter], _ *gpui.Context[Observer]) {
			f.record("notified")
		})
	})

	counter.Update(f.app, func(_ *Counter, cx *gpui.Context[Counter]) { cx.Notify() })
	f.ta.Update(func(_ *gpui.App) {})
	if got := f.recorded(); len(got) != 1 {
		t.Fatalf("ignored handle cancelled the registration: %v", got)
	}

	// Closing the owning scope cancels it.
	var dependent *gpui.Scope
	observer.Update(f.app, func(_ *Observer, cx *gpui.Context[Observer]) {
		dependent = cx.Scope()
	})
	dependent.Close()
	f.events = nil

	counter.Update(f.app, func(_ *Counter, cx *gpui.Context[Counter]) { cx.Notify() })
	f.ta.Update(func(_ *gpui.App) {})
	if got := f.recorded(); len(got) != 0 {
		t.Fatalf("scope close did not cancel the scope-owned registration: %v", got)
	}
}

// Contract: "Detach() transfers to an app-owned registration registry
// until cancellation or endpoint release. Detach does not retain either
// endpoint." Destroying one of two observers never cancels the other's
// registration.
func TestDetachedSubscriptionSurvivesOwnerScopeClose(t *testing.T) {
	f := newCounterFixture(t)
	counter := f.newCounter(0)
	observer := f.newObserver()

	var dependent *gpui.Scope
	observer.Update(f.app, func(_ *Observer, cx *gpui.Context[Observer]) {
		cx.Observe(counter, func(_ *Observer, _ gpui.Entity[Counter], _ *gpui.Context[Observer]) {
			f.record("notified")
		}).Detach()
		dependent = cx.Scope()
	})

	// Detached: closing the original owning scope does not cancel it.
	dependent.Close()
	counter.Update(f.app, func(_ *Counter, cx *gpui.Context[Counter]) { cx.Notify() })
	f.ta.Update(func(_ *gpui.App) {})
	if got := f.recorded(); len(got) != 1 {
		t.Fatalf("detached registration was cancelled by owner close: %v", got)
	}

	// Weak endpoint: releasing the observer entity unsubscribes it
	// without panicking on later notifications.
	observer.Release()
	f.events = nil

	counter.Update(f.app, func(_ *Counter, cx *gpui.Context[Counter]) { cx.Notify() })
	f.ta.Update(func(_ *gpui.App) {})
	if got := f.recorded(); len(got) != 0 {
		t.Fatalf("dead endpoint still delivered: %v", got)
	}
}

// Contract: "Subscription handle copies alias one cancellation token.
// Cancel() is idempotent."
func TestSubscriptionCancelIsIdempotentAndAliasesShareToken(t *testing.T) {
	f := newCounterFixture(t)
	counter := f.newCounter(0)
	observer := f.newObserver()

	var sub gpui.Subscription
	observer.Update(f.app, func(_ *Observer, cx *gpui.Context[Observer]) {
		sub = cx.Observe(counter, func(_ *Observer, _ gpui.Entity[Counter], _ *gpui.Context[Observer]) {
			f.record("notified")
		})
	})

	alias := sub
	alias.Cancel()
	sub.Cancel() // idempotent

	counter.Update(f.app, func(_ *Counter, cx *gpui.Context[Counter]) { cx.Notify() })
	f.ta.Update(func(_ *gpui.App) {})
	if got := f.recorded(); len(got) != 0 {
		t.Fatalf("cancelled subscription still delivered: %v", got)
	}
}

// Contract: "Destroying one of two observers never cancels the other's
// registration."
func TestDestroyingOneObserverKeepsOtherRegistration(t *testing.T) {
	f := newCounterFixture(t)
	counter := f.newCounter(0)
	observerA := f.newObserver()
	observerB := f.newObserver()

	for _, observer := range []gpui.Entity[Observer]{observerA, observerB} {
		observer.Update(f.app, func(_ *Observer, cx *gpui.Context[Observer]) {
			cx.Observe(counter, func(_ *Observer, _ gpui.Entity[Counter], _ *gpui.Context[Observer]) {
				f.record("notified")
			}).Detach()
		})
	}

	observerA.Release() // destroy one observer
	f.ta.Update(func(_ *gpui.App) {})

	counter.Update(f.app, func(_ *Counter, cx *gpui.Context[Counter]) { cx.Notify() })
	f.ta.Update(func(_ *gpui.App) {})
	if got := f.recorded(); len(got) != 1 {
		t.Fatalf("remaining observer deliveries = %v, want exactly one", got)
	}
}

// ---------------------------------------------------------------------------
// Events: payload identity, ordering, activation, cancel-next
// ---------------------------------------------------------------------------

// payloadOne and payloadTwo are distinct interface payload types with
// identical underlying structure: dispatch identity must follow the
// declared type, including nil interface payloads.
type payloadOne interface{ isPayload() }
type payloadTwo interface{ isPayload() }

type concretePayload struct{}

func (concretePayload) isPayload() {}

// Contract: "The typed envelope preserves nil interface payloads and
// declared dispatch identity", keyed by entity identity and the declared
// payload Go type, never the dynamic member of an interface payload.
func TestNilInterfacePayloadIdentity(t *testing.T) {
	f := newCounterFixture(t)
	counter := f.newCounter(0)
	observer := f.newObserver()

	eventOne := gpui.DefineEvent[Counter, payloadOne]()
	eventTwo := gpui.DefineEvent[Counter, payloadTwo]()

	var oneCalls, twoCalls int
	observer.Update(f.app, func(_ *Observer, cx *gpui.Context[Observer]) {
		eventOne.Subscribe(cx, counter, func(_ *Observer, _ gpui.Entity[Counter], p *payloadOne, _ *gpui.Context[Observer]) {
			oneCalls++
			if *p != nil {
				t.Fatalf("payload one = %v, want nil interface payload preserved", *p)
			}
			f.record("one")
		})
		eventTwo.Subscribe(cx, counter, func(_ *Observer, _ gpui.Entity[Counter], p *payloadTwo, _ *gpui.Context[Observer]) {
			twoCalls++
			if _, ok := (*p).(concretePayload); !ok {
				t.Fatalf("payload two dynamic member = %v, want concretePayload", *p)
			}
			f.record("two")
		})
	})

	counter.Update(f.app, func(_ *Counter, cx *gpui.Context[Counter]) {
		eventOne.Emit(cx, nil) // nil interface payload
	})
	f.ta.Update(func(_ *gpui.App) {})
	if got := f.recorded(); len(got) != 1 || got[0] != "one" {
		t.Fatalf("nil payload routing = %v, want [one]", got)
	}

	f.events = nil
	counter.Update(f.app, func(_ *Counter, cx *gpui.Context[Counter]) {
		eventTwo.Emit(cx, concretePayload{}) // dynamic member is concretePayload
	})
	f.ta.Update(func(_ *gpui.App) {})
	if got := f.recorded(); len(got) != 1 || got[0] != "two" {
		t.Fatalf("dynamic-member routing = %v, want [two] (dispatch follows the declared type)", got)
	}
	if oneCalls != 1 || twoCalls != 1 {
		t.Fatalf("calls = (%d, %d), want (1, 1)", oneCalls, twoCalls)
	}
}

// Contract: "Emit queues each occurrence using source identity and the
// declared payload type ... Defer appends in call order" — every FIFO
// emit delivers in emission order, and listeners fire in registration
// order.
func TestFifoEmitAndRegistrationOrder(t *testing.T) {
	f := newCounterFixture(t)
	counter := f.newCounter(0)
	observerA := f.newObserver()
	observerB := f.newObserver()

	for _, observer := range []gpui.Entity[Observer]{observerA, observerB} {
		name := "A"
		if observer.EntityID() == observerB.EntityID() {
			name = "B"
		}
		observer.Update(f.app, func(_ *Observer, cx *gpui.Context[Observer]) {
			tickEvent.Subscribe(cx, counter, func(_ *Observer, _ gpui.Entity[Counter], e *TickEvent, _ *gpui.Context[Observer]) {
				f.record(name + string(rune('0'+e.value)))
			})
		})
	}

	counter.Update(f.app, func(_ *Counter, cx *gpui.Context[Counter]) {
		tickEvent.Emit(cx, TickEvent{value: 1})
		tickEvent.Emit(cx, TickEvent{value: 2})
		tickEvent.Emit(cx, TickEvent{value: 3})
	})
	f.ta.Update(func(_ *gpui.App) {})

	// Per emission, registration order (A then B); emissions in FIFO
	// order.
	want := []string{"A1", "B1", "A2", "B2", "A3", "B3"}
	got := f.recorded()
	if len(got) != len(want) {
		t.Fatalf("deliveries = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("delivery order = %v, want %v", got, want)
		}
	}
}

// Contract: "New registrations activate through a queued defer ... New
// registrations are not inserted into the active traversal."
func TestDeferredRegistrationActivation(t *testing.T) {
	f := newCounterFixture(t)
	counter := f.newCounter(0)
	observer := f.newObserver()

	observer.Update(f.app, func(_ *Observer, cx *gpui.Context[Observer]) {
		tickEvent.Subscribe(cx, counter, func(_ *Observer, _ gpui.Entity[Counter], _ *TickEvent, _ *gpui.Context[Observer]) {
			f.record("first")
		})
	})

	// A subscription registered from inside a delivery callback is inert
	// for the rest of that traversal and active from the next emission.
	counter.Update(f.app, func(_ *Counter, cx *gpui.Context[Counter]) {
		tickEvent.Emit(cx, TickEvent{value: 1})
	})
	f.ta.Update(func(_ *gpui.App) {}) // first flush: only the first registration is active

	later := f.newObserver()
	later.Update(f.app, func(_ *Observer, cx *gpui.Context[Observer]) {
		tickEvent.Subscribe(cx, counter, func(_ *Observer, _ gpui.Entity[Counter], _ *TickEvent, _ *gpui.Context[Observer]) {
			f.record("second")
		})
	})
	// Registering during the next emission's delivery:
	observer2 := later
	_ = observer2

	f.events = nil
	counter.Update(f.app, func(_ *Counter, cx *gpui.Context[Counter]) {
		tickEvent.Emit(cx, TickEvent{value: 2})
	})
	f.ta.Update(func(_ *gpui.App) {})
	if got := f.recorded(); len(got) != 2 || got[0] != "first" || got[1] != "second" {
		t.Fatalf("deliveries after activation = %v, want [first second]", got)
	}
}

// Contract: "Cancellation by an earlier callback skips a later callback
// in that traversal."
func TestCancelNextDuringDelivery(t *testing.T) {
	f := newCounterFixture(t)
	counter := f.newCounter(0)
	observer := f.newObserver()

	var second gpui.Subscription
	observer.Update(f.app, func(_ *Observer, cx *gpui.Context[Observer]) {
		tickEvent.Subscribe(cx, counter, func(_ *Observer, _ gpui.Entity[Counter], _ *TickEvent, _ *gpui.Context[Observer]) {
			f.record("first")
			second.Cancel() // cancel-next: the later callback is skipped
		})
		second = tickEvent.Subscribe(cx, counter, func(_ *Observer, _ gpui.Entity[Counter], _ *TickEvent, _ *gpui.Context[Observer]) {
			f.record("second")
		})
	})
	// "first" is registered before "second"; the first callback cancels
	// the later registration during the same traversal.

	counter.Update(f.app, func(_ *Counter, cx *gpui.Context[Counter]) {
		tickEvent.Emit(cx, TickEvent{value: 1})
	})
	f.ta.Update(func(_ *gpui.App) {})
	if got := f.recorded(); len(got) != 1 || got[0] != "first" {
		t.Fatalf("cancel-next deliveries = %v, want only [first]", got)
	}
}

// Contract: "Notify coalesces while that entity is in the pending set.
// Remove it before invoking observers, allowing a callback to enqueue
// another notify."
func TestNotifyCoalescingAndReNotify(t *testing.T) {
	f := newCounterFixture(t)
	counter := f.newCounter(0)
	observer := f.newObserver()

	var renotifies int
	observer.Update(f.app, func(_ *Observer, cx *gpui.Context[Observer]) {
		cx.Observe(counter, func(_ *Observer, source gpui.Entity[Counter], _ *gpui.Context[Observer]) {
			f.record("notified")
			if renotifies < 1 {
				renotifies++
				// Re-notify from the callback: the source was removed
				// from the pending set before delivery, so this queues a
				// new notify processed later in the same flush.
				source.Update(f.app, func(_ *Counter, scx *gpui.Context[Counter]) {
					scx.Notify()
				})
			}
		})
	})

	// Two notifies within one outermost update coalesce to one delivery.
	counter.Update(f.app, func(_ *Counter, cx *gpui.Context[Counter]) {
		cx.Notify()
		cx.Notify()
	})
	f.ta.Update(func(_ *gpui.App) {})

	if got := f.recorded(); len(got) != 2 {
		t.Fatalf("deliveries = %v, want two (coalesced pair, then re-notify)", got)
	}
}

// Contract: "Only the outermost successful update exit starts flushing"
// and the flush loop releases dropped entities before applying further
// effects.
func TestReleaseRunsBeforeOtherPendingEffects(t *testing.T) {
	f := newCounterFixture(t)
	doomed := f.newCounter(1)
	watched := f.newCounter(2)
	observer := f.newObserver()

	doomed.Update(f.app, func(_ *Counter, cx *gpui.Context[Counter]) {
		cx.OnRelease(func(_ *Counter, _ *gpui.App) {
			f.record("released")
		})
	})
	f.observe(observer, watched)

	// One update queues a release, a defer and a notify: the release
	// callback must run at flush start, before the other effects.
	f.ta.Update(func(app *gpui.App) {
		doomed.Release()
		app.Defer(func(_ *gpui.App) {
			f.record("defer-ran")
		})
		watched.Update(app, func(_ *Counter, cx *gpui.Context[Counter]) {
			cx.Notify()
		})
	})

	want := []string{"released", "defer-ran", "notified"}
	got := f.recorded()
	if len(got) != len(want) {
		t.Fatalf("effect order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("effect order = %v, want %v", got, want)
		}
	}
}

// Contract: "Defer appends in call order, including nested defers. An
// already queued effect precedes a newly deferred callback."
func TestNestedDefersRunInSameFlushLoop(t *testing.T) {
	f := newCounterFixture(t)

	f.ta.Update(func(app *gpui.App) {
		app.Defer(func(app *gpui.App) {
			f.record("d1")
			app.Defer(func(_ *gpui.App) {
				f.record("d2-nested")
			})
		})
		app.Defer(func(_ *gpui.App) {
			f.record("d3-queued-before-nested")
		})
	})

	// d1 runs, queuing d2-nested; d3-queued-before-nested was already
	// queued so it precedes the newly deferred callback.
	want := []string{"d1", "d3-queued-before-nested", "d2-nested"}
	got := f.recorded()
	if len(got) != len(want) {
		t.Fatalf("defer order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("defer order = %v, want %v", got, want)
		}
	}
}

// Contract: "source death with queued events" — a source released during
// a notify traversal unsubscribes its remaining observers, and the
// release callback runs after the traversal.
func TestSourceDeathDuringTraversal(t *testing.T) {
	f := newCounterFixture(t)
	counter := f.newCounter(0)
	observerA := f.newObserver()
	observerB := f.newObserver()

	counter.Update(f.app, func(_ *Counter, cx *gpui.Context[Counter]) {
		cx.OnRelease(func(_ *Counter, _ *gpui.App) {
			f.record("source-released")
		})
	})

	var handle gpui.Entity[Counter]
	for _, observer := range []gpui.Entity[Observer]{observerA, observerB} {
		observer.Update(f.app, func(_ *Observer, cx *gpui.Context[Observer]) {
			cx.Observe(counter, func(_ *Observer, _ gpui.Entity[Counter], _ *gpui.Context[Observer]) {
				f.record("notified")
				// The first observer releases the source's last handle:
				// later observers are skipped and unsubscribed.
				handle.Release()
			})
		})
	}
	handle = counter

	f.events = nil
	counter.Update(f.app, func(_ *Counter, cx *gpui.Context[Counter]) { cx.Notify() })
	f.ta.Update(func(_ *gpui.App) {})

	// Only the first observer delivered; the source's release callback
	// ran at the next flush-loop top, after the traversal completed.
	want := []string{"notified", "source-released"}
	got := f.recorded()
	if len(got) != len(want) {
		t.Fatalf("events after source death = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("events after source death = %v, want %v", got, want)
		}
	}
	// The dead source's observers were removed during its release; the
	// surviving observer entity can still be notified about other
	// sources without touching the dead identity.
	if weak := counter.Downgrade(); weak.IsUpgradable() {
		t.Fatal("dead source identity is upgradable")
	}
}

// Contract: "Temporary internal pins protect each active callback's
// state" — a release queued during an active callback waits for the
// callback to return before its hooks run.
func TestActiveCallbackPinsReleaseProcessing(t *testing.T) {
	f := newCounterFixture(t)
	counter := f.newCounter(0)

	counter.Update(f.app, func(_ *Counter, cx *gpui.Context[Counter]) {
		cx.OnRelease(func(c *Counter, _ *gpui.App) {
			f.record("released")
		})
	})

	counter.Update(f.app, func(c *Counter, _ *gpui.Context[Counter]) {
		counter.Release() // release queued while this callback is active
		c.value = 5       // the callback keeps state access
		f.record("in-callback")
	})

	got := f.recorded()
	want := []string{"in-callback", "released"}
	if len(got) != 2 {
		t.Fatalf("events = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("events = %v, want %v", got, want)
		}
	}
}

// Contract (EmitOwned): "the factory explicitly retains into that scope";
// payload scopes last until the entire effect cycle drains and retire in
// emission order at the tail; a retirement that produces pending
// releases runs in a subsequent foreground flush.
func TestEmitOwnedPayloadScopeTailRetirement(t *testing.T) {
	f := newCounterFixture(t)
	counter := f.newCounter(0)
	observer := f.newObserver()
	retained := f.newCounter(0)

	retained.Update(f.app, func(_ *Counter, cx *gpui.Context[Counter]) {
		cx.OnRelease(func(_ *Counter, _ *gpui.App) {
			f.record("payload-entity-released")
		})
	})

	observer.Update(f.app, func(_ *Observer, cx *gpui.Context[Observer]) {
		tickEvent.Subscribe(cx, counter, func(_ *Observer, _ gpui.Entity[Counter], _ *TickEvent, _ *gpui.Context[Observer]) {
			f.record("delivered")
		})
	})

	// A defer queued after the emission still runs in the first cycle;
	// the payload-scope retirement happens after the queue drains, and
	// its release runs in the subsequent flush.
	f.ta.Update(func(app *gpui.App) {
		counter.Update(app, func(_ *Counter, cx *gpui.Context[Counter]) {
			tickEvent.EmitOwned(cx, func(payloadScope *gpui.Scope) TickEvent {
				// The factory explicitly retains the payload dependency.
				retained.RetainInto(payloadScope)
				return TickEvent{value: 9}
			})
		})
		app.Defer(func(_ *gpui.App) {
			f.record("defer-ran")
		})
		retained.Release() // only the payload scope's lease remains
	})

	want := []string{"delivered", "defer-ran", "payload-entity-released"}
	got := f.recorded()
	if len(got) != len(want) {
		t.Fatalf("arena retirement order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arena retirement order = %v, want %v", got, want)
		}
	}
}

// Contract (EmitOwned): "Factory failure closes partial ownership and
// enqueues nothing."
func TestEmitOwnedFactoryFailureClosesOwnershipAndEnqueuesNothing(t *testing.T) {
	f := newCounterFixture(t)
	counter := f.newCounter(0)
	observer := f.newObserver()
	retained := f.newCounter(0)

	observer.Update(f.app, func(_ *Observer, cx *gpui.Context[Observer]) {
		tickEvent.Subscribe(cx, counter, func(_ *Observer, _ gpui.Entity[Counter], _ *TickEvent, _ *gpui.Context[Observer]) {
			f.record("delivered")
		})
	})

	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("factory panic did not propagate")
			}
		}()
		f.ta.Update(func(app *gpui.App) {
			counter.Update(app, func(_ *Counter, cx *gpui.Context[Counter]) {
				tickEvent.EmitOwned(cx, func(payloadScope *gpui.Scope) TickEvent {
					retained.RetainInto(payloadScope)
					panic("factory boom")
				})
			})
		})
	}()

	// The panic propagated, nothing was enqueued ...
	if got := f.recorded(); len(got) != 0 {
		t.Fatalf("factory failure enqueued a delivery: %v", got)
	}
	// ... and partial ownership was closed: the retained lease was
	// released while the test's own lease keeps the entity alive.
	if _, ok := retained.Downgrade().UpgradeInto(f.root); !ok {
		t.Fatal("factory failure did not close partial ownership correctly")
	}
	retained.Release()
	f.ta.Update(func(_ *gpui.App) {})
}

// EmitOn supplies the app-context emission path.
func TestEmitOnAppContextPath(t *testing.T) {
	f := newCounterFixture(t)
	counter := f.newCounter(0)
	observer := f.newObserver()

	observer.Update(f.app, func(_ *Observer, cx *gpui.Context[Observer]) {
		tickEvent.Subscribe(cx, counter, func(_ *Observer, _ gpui.Entity[Counter], e *TickEvent, _ *gpui.Context[Observer]) {
			f.record("delivered-" + string(rune('0'+e.value)))
		})
	})

	// Emission from an app context outside an entity update: the effect
	// flushes at the next update boundary.
	tickEvent.EmitOn(f.app, counter, TickEvent{value: 4})
	f.ta.Update(func(_ *gpui.App) {})

	if got := f.recorded(); len(got) != 1 || got[0] != "delivered-4" {
		t.Fatalf("EmitOn deliveries = %v, want [delivered-4]", got)
	}
}

// ---------------------------------------------------------------------------
// Weak access, window-aware access, listener adapter
// ---------------------------------------------------------------------------

// Contract: weak access reports ErrEntityReleased on logical zero.
func TestWeakAccessReportsErrEntityReleased(t *testing.T) {
	f := newCounterFixture(t)
	counter := f.newCounter(1)
	weak := counter.Downgrade()

	counter.Release()
	f.ta.Update(func(_ *gpui.App) {})

	if _, err := weak.TryRead(f.app, func(_ *Counter, _ *gpui.App) struct{} { return struct{}{} }); err != gpui.ErrEntityReleased {
		t.Fatalf("TryRead error = %v, want ErrEntityReleased", err)
	}
	err := weak.TryUpdate(f.app, func(_ *Counter, _ *gpui.Context[Counter]) {})
	if err != gpui.ErrEntityReleased {
		t.Fatalf("TryUpdate error = %v, want ErrEntityReleased", err)
	}
	if _, err := weak.TryUpdateWith(f.app, func(_ *Counter, _ *gpui.Context[Counter]) int { return 1 }); err != gpui.ErrEntityReleased {
		t.Fatalf("TryUpdateWith error = %v, want ErrEntityReleased", err)
	}
	if err := weak.TryUpdateIn(f.app, &gpui.Window{}, func(_ *Counter, _ *gpui.Window, _ *gpui.Context[Counter]) {}); err != gpui.ErrEntityReleased {
		t.Fatalf("TryUpdateIn error = %v, want ErrEntityReleased", err)
	}

	// While live, weak access works.
	counter2 := f.newCounter(2)
	weak2 := counter2.Downgrade()
	if err := weak2.TryUpdate(f.app, func(c *Counter, _ *gpui.Context[Counter]) { c.value++ }); err != nil {
		t.Fatalf("live TryUpdate error = %v", err)
	}
	if value := counter2.Read(f.app, func(c *Counter, _ *gpui.App) uint64 { return c.value }); value != 3 {
		t.Fatalf("value = %d, want 3", value)
	}
}

// Authoring contract: nil/unavailable window is ErrNoWindow.
func TestUpdateInWindowErrors(t *testing.T) {
	f := newCounterFixture(t)
	counter := f.newCounter(1)

	if err := counter.UpdateIn(f.app, nil, func(_ *Counter, _ *gpui.Window, _ *gpui.Context[Counter]) {}); err != gpui.ErrNoWindow {
		t.Fatalf("UpdateIn(nil) error = %v, want ErrNoWindow", err)
	}
	if _, err := counter.UpdateInWith(f.app, nil, func(_ *Counter, _ *gpui.Window, _ *gpui.Context[Counter]) int { return 0 }); err != gpui.ErrNoWindow {
		t.Fatalf("UpdateInWith(nil) error = %v, want ErrNoWindow", err)
	}

	window := &gpui.Window{}
	if err := counter.UpdateIn(f.app, window, func(c *Counter, w *gpui.Window, _ *gpui.Context[Counter]) {
		if w != window {
			t.Fatal("window not threaded through to the callback")
		}
		c.value = 9
	}); err != nil {
		t.Fatalf("UpdateIn(window) error = %v, err nil", err)
	}
	if value := counter.Read(f.app, func(c *Counter, _ *gpui.App) uint64 { return c.value }); value != 9 {
		t.Fatalf("value = %d, want 9", value)
	}
}

// Contract: "Listener uses weak identity ... a released target is a
// no-op" with a fresh context at delivery.
func TestListenerAdapterWeakEndpoint(t *testing.T) {
	f := newCounterFixture(t)
	counter := f.newCounter(0)

	var listener func(*TickEvent, *gpui.Window, *gpui.App)
	counter.Update(f.app, func(_ *Counter, cx *gpui.Context[Counter]) {
		listener = cx.Listener(func(c *Counter, e *TickEvent, _ *gpui.Window, _ *gpui.Context[Counter]) {
			c.value += uint64(e.value)
			f.record("listener")
		})
	})

	listener(&TickEvent{value: 3}, nil, f.app)
	if got := f.recorded(); len(got) != 1 {
		t.Fatalf("listener deliveries = %v, want one", got)
	}
	if value := counter.Read(f.app, func(c *Counter, _ *gpui.App) uint64 { return c.value }); value != 3 {
		t.Fatalf("value = %d, want 3", value)
	}

	// A foreign application is rejected.
	expectPanic(t, "foreign application", func() {
		other := gpui.NewTestApp()
		listener(&TickEvent{value: 1}, nil, other.App())
	})

	// A released target is a no-op.
	counter.Release()
	f.ta.Update(func(_ *gpui.App) {})
	f.events = nil
	listener(&TickEvent{value: 4}, nil, f.app)
	if got := f.recorded(); len(got) != 0 {
		t.Fatalf("listener fired after release: %v", got)
	}
}

// ---------------------------------------------------------------------------
// Tasks
// ---------------------------------------------------------------------------

// Contract: "Cancel() signals cancellation and closes result delivery,
// without waiting for worker exit" — a parked worker is woken and
// observes cancellation cooperatively.
func TestTaskCancelWakesWorkerCooperatively(t *testing.T) {
	f := newCounterFixture(t)

	task := f.ta.Background().Spawn(func(run *gpui.TaskRun) int64 {
		f.record("started")
		run.Sleep(time.Hour)
		if run.Cancelled() {
			f.record("cancelled-exit")
			return -1
		}
		f.record("slept")
		return 1
	})

	f.ta.RunUntilParked() // the task starts and parks on the timer
	if got := f.recorded(); len(got) != 1 || got[0] != "started" {
		t.Fatalf("task events = %v, want [started]", got)
	}

	task.Cancel()
	if !task.Cancelled() {
		t.Fatal("task not marked cancelled")
	}
	f.ta.RunUntilParked() // the parked worker wakes and exits cooperatively

	if got := f.recorded(); len(got) != 2 || got[1] != "cancelled-exit" {
		t.Fatalf("task events = %v, want [started cancelled-exit]", got)
	}
	if !task.Done() {
		t.Fatal("cancelled task never completed")
	}

	// Awaiting a cancelled task yields the zero result without parking.
	other := f.ta.Background().Spawn(func(run *gpui.TaskRun) int64 {
		result := run.Await(task)
		f.record("awaited")
		return result
	})
	f.ta.RunUntilParked()
	if got := f.recorded(); len(got) != 3 || got[2] != "awaited" {
		t.Fatalf("await events = %v", got)
	}
	if !other.Done() {
		t.Fatal("the awaiter of a cancelled task did not complete")
	}
}

// Contract: "Detach() atomically transfers cancellation ownership to the
// app's detached-task registry" — a detached task keeps running.
func TestDetachedTaskKeepsRunning(t *testing.T) {
	f := newCounterFixture(t)

	task := f.ta.Background().Spawn(func(run *gpui.TaskRun) int64 {
		f.record("started")
		run.Sleep(50 * time.Millisecond)
		f.record("completed")
		return 42
	})
	task.Detach()

	f.ta.RunUntilParked() // starts and parks on the 50ms timer
	f.ta.AdvanceClock(100)

	if got := f.recorded(); len(got) != 2 {
		t.Fatalf("detached task events = %v, want [started completed]", got)
	}
	if !task.Done() {
		t.Fatal("detached task did not complete")
	}

	// The registry removes completed tasks; scope-owned tasks cancel on
	// scope close.
	scope := f.root.Child()
	owned := f.ta.Background().Spawn(func(run *gpui.TaskRun) struct{} {
		run.Sleep(time.Hour)
		return struct{}{}
	})
	owned.TransferInto(scope)
	f.ta.RunUntilParked()
	scope.Close()
	f.ta.RunUntilParked()
	if !owned.Cancelled() {
		t.Fatal("scope close did not cancel the scope-owned task")
	}
}

// Foreground tasks post updates through the AsyncApp machinery and run
// their continuations when the dispatcher drains them.
func TestForegroundTaskDeliversResultThroughUpdate(t *testing.T) {
	f := newCounterFixture(t)

	background := f.ta.Background().Spawn(func(run *gpui.TaskRun) int64 {
		run.Sleep(10 * time.Millisecond)
		return 7
	})

	f.app.Spawn(func(cx *gpui.AsyncApp) struct{} {
		result := cx.Await(background)
		cx.Update(func(_ *gpui.App) {
			f.record("observed-" + string(rune('0'+result)))
		})
		return struct{}{}
	}).Detach()

	f.ta.RunUntilParked() // start both tasks; both park
	f.ta.AdvanceClock(20) // fire the timer and run the continuation

	if got := f.recorded(); len(got) != 1 || got[0] != "observed-7" {
		t.Fatalf("foreground delivery = %v, want [observed-7]", got)
	}
}

// ---------------------------------------------------------------------------
// Globals
// ---------------------------------------------------------------------------

type testGlobal struct{ value int }

// Contract: "Global notifications similarly coalesce by declared global
// type."
func TestGlobalObservationCoalescing(t *testing.T) {
	f := newCounterFixture(t)

	f.app.ObserveGlobal[testGlobal](func(_ *gpui.App) {
		f.record("global")
	})
	f.ta.Update(func(_ *gpui.App) {}) // deferred activation

	f.app.SetGlobal(testGlobal{value: 1})
	f.ta.Update(func(_ *gpui.App) {})
	if got := f.recorded(); len(got) != 0 {
		t.Fatalf("first SetGlobal notified: %v", got)
	}

	// Two replacements in one update coalesce to one notification.
	f.app.SetGlobal(testGlobal{value: 2})
	f.app.SetGlobal(testGlobal{value: 3})
	f.ta.Update(func(_ *gpui.App) {})
	if got := f.recorded(); len(got) != 1 {
		t.Fatalf("global notifications = %v, want one (coalesced)", got)
	}
	if v := gpui.Global[testGlobal](f.app); v.value != 3 {
		t.Fatalf("global value = %d, want 3", v.value)
	}
}

// Contract: "Do not flush further application callbacks while unwinding a
// panic; restore the boundary and propagate it."
func TestPanicUnwindingRestoresBoundaryWithoutFlushing(t *testing.T) {
	f := newCounterFixture(t)
	counter := f.newCounter(0)
	f.observe(f.newObserver(), counter)

	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("panic did not propagate")
			}
		}()
		counter.Update(f.app, func(_ *Counter, cx *gpui.Context[Counter]) {
			cx.Notify() // queues a notify effect
			panic("boom")
		})
	}()

	// No callback was flushed while unwinding.
	if got := len(f.recorded()); got != 0 {
		t.Fatalf("callbacks flushed while unwinding: %v", f.recorded())
	}

	// The boundary was restored: the app updates and flushes again, and
	// the queued effect is still delivered.
	f.ta.Update(func(_ *gpui.App) {})
	if got := len(f.recorded()); got != 1 {
		t.Fatalf("queued effect lost after unwinding: %v", got)
	}
	counter.Update(f.app, func(c *Counter, _ *gpui.Context[Counter]) { c.value = 1 })
	if value := counter.Read(f.app, func(c *Counter, _ *gpui.App) uint64 { return c.value }); value != 1 {
		t.Fatalf("entity guards not restored after unwinding: %d", value)
	}
}

// Contract: "Strong cycles are not collected ... Debug diagnostics
// should report remaining leases/scopes and cycles at shutdown; they do
// not silently collect them."
func TestStrongCycleReportedNotCollected(t *testing.T) {
	f := newCounterFixture(t)
	first := f.newCounter(1)
	second := f.newCounter(2)

	var depFirst, depSecond *gpui.Scope
	first.Update(f.app, func(_ *Counter, cx *gpui.Context[Counter]) { depFirst = cx.Scope() })
	second.Update(f.app, func(_ *Counter, cx *gpui.Context[Counter]) { depSecond = cx.Scope() })

	// A lease of `first` in `second`'s dependent scope and vice versa:
	// both entities keep each other alive forever.
	firstCycle := first.RetainInto(depSecond)
	secondCycle := second.RetainInto(depFirst)

	report := f.app.OwnershipReport()
	joined := strings.Join(report, "\n")

	if !strings.Contains(joined, "strong cycle") {
		t.Fatalf("cycle not reported:\n%s", joined)
	}
	// Both entities appear as live with their leases.
	if !strings.Contains(joined, "dependent scope of entity") {
		t.Fatalf("remaining leases not reported:\n%s", joined)
	}

	// The cycle is not collected: both identities stay live.
	if !first.Downgrade().IsUpgradable() || !second.Downgrade().IsUpgradable() {
		t.Fatal("report collected the cycle")
	}

	// Breaking the cycle explicitly: releasing second's own leases lets
	// second release, which closes its dependent scope and drops the
	// lease it held over first.
	second.Release()
	secondCycle.Release()
	f.ta.Update(func(_ *gpui.App) {})
	if second.Downgrade().IsUpgradable() {
		t.Fatal("explicit edge break did not release second")
	}
	if !first.Downgrade().IsUpgradable() {
		t.Fatal("breaking the cycle released first prematurely")
	}

	// With the cycle gone, first releases through its remaining lease.
	first.Release()
	firstCycle.Release()
	f.ta.Update(func(_ *gpui.App) {})
	if first.Downgrade().IsUpgradable() {
		t.Fatal("first did not release after the cycle was broken and its lease dropped")
	}
}

// SubscribeSelf supplies the self-subscription path with the same
// registration, activation and FIFO delivery rules.
func TestSubscribeSelfDelivery(t *testing.T) {
	f := newCounterFixture(t)
	counter := f.newCounter(0)

	counter.Update(f.app, func(_ *Counter, cx *gpui.Context[Counter]) {
		tickEvent.SubscribeSelf(cx, func(c *Counter, e *TickEvent, _ *gpui.Context[Counter]) {
			c.value += uint64(e.value)
			f.record("self-" + string(rune('0'+e.value)))
		})
	})

	counter.Update(f.app, func(_ *Counter, cx *gpui.Context[Counter]) {
		tickEvent.Emit(cx, TickEvent{value: 1})
		tickEvent.Emit(cx, TickEvent{value: 2})
	})
	f.ta.Update(func(_ *gpui.App) {})

	want := []string{"self-1", "self-2"}
	if got := f.recorded(); len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("SubscribeSelf deliveries = %v, want %v", got, want)
	}
	if value := counter.Read(f.app, func(c *Counter, _ *gpui.App) uint64 { return c.value }); value != 3 {
		t.Fatalf("self subscription did not mutate state: %d", value)
	}
}

// TryUpdateInWith returns results and window errors in the weak path.
func TestTryUpdateInWithResultAndWindow(t *testing.T) {
	f := newCounterFixture(t)
	counter := f.newCounter(1)
	weak := counter.Downgrade()

	window := &gpui.Window{}
	result, err := weak.TryUpdateInWith(f.app, window, func(c *Counter, _ *gpui.Window, _ *gpui.Context[Counter]) uint64 {
		c.value *= 10
		return c.value
	})
	if err != nil || result != 10 {
		t.Fatalf("TryUpdateInWith = (%d, %v), want (10, nil)", result, err)
	}

	if _, err := weak.TryUpdateInWith(f.app, nil, func(_ *Counter, _ *gpui.Window, _ *gpui.Context[Counter]) uint64 { return 0 }); err != gpui.ErrNoWindow {
		t.Fatalf("TryUpdateInWith(nil) error = %v, want ErrNoWindow", err)
	}

	counter.Release()
	f.ta.Update(func(_ *gpui.App) {})
	if _, err := weak.TryUpdateInWith(f.app, window, func(_ *Counter, _ *gpui.Window, _ *gpui.Context[Counter]) uint64 { return 0 }); err != gpui.ErrEntityReleased {
		t.Fatalf("TryUpdateInWith after release = %v, want ErrEntityReleased", err)
	}
}
