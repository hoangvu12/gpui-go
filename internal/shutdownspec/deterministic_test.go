//go:build windows

// Deterministic adversarial close/cancellation schedules (the
// headless half of ticket30's corpus): the window-close cascade that
// crosses services (scope close → lease release → focus release →
// element-runtime teardown), scope cancellation racing a result handoff
// whose receiving scope is a WINDOW scope, the shutdown drain's effect
// flushing and unresolved-execution records, and abandoned frame builds
// composed with window closure.
//
// The bare-scope and task-layer traces those schedules build on are
// pinned by internal/portfixture/ownership_test.go (flush-loop order,
// release idempotence, two-scope leases, notify coalescing, arena-tail
// retirement) and internal/taskspec (delivery gates, discard
// semantics, the shutdown drain's accounting). What this file adds is
// their composition with window closure and app shutdown — the
// acceptance item "deterministic adversarial schedules for shared
// entities, scope cancellation, result handoff, effect flushing, focus
// release and abandoned frames ACROSS completed services".
package shutdownspec

import (
	"runtime"
	"testing"
	"time"

	"gpui-go/gpui"
)

// ---------------------------------------------------------------------------
// The window-close cascade (scope close → lease release → focus release
// → element-runtime teardown)
// ---------------------------------------------------------------------------

// TestWindowCloseCascadeReleasesFocusAndRuntime pins the cross-service
// close cascade through one window: an entity leased by the window
// scope whose release hook releases the window's FOCUSED handle, so the
// WM_CLOSE-equivalent (CloseTestWindow: registry removal, scope close,
// element-runtime drop) provably reaches focus release through the
// entity lease release, at the effect-flush boundary (never mid-close).
//
// Reference-preserved trace: the release hook runs only after the scope
// finished closing (runtime ownership contract, "Logical ownership":
// "Mark its dependent scope closing and cancel work before callbacks";
// the flush boundary is app.rs 1743-1830's flush_effects loop, where
// release_dropped_entities precedes every applied effect). The focus
// release clears the window focus and dispatches focus-lost exactly
// once (window.rs notify_focus_lost; gpui/focus.go's Release).
func TestWindowCloseCascadeReleasesFocusAndRuntime(t *testing.T) {
	if testing.Short() {
		t.Skip("draws through the native layout service")
	}
	testApp := gpui.NewTestApp()
	app := testApp.App()
	w := gpui.NewTestWindow(app, gpui.Size{Width: 320, Height: 240})

	// The focus handle the entity owns; focused before the close.
	focus := gpui.NewFocusHandle(w)
	log := &releaseLog{}
	type owner struct {
		focus gpui.FocusHandle
	}
	gpui.NewEntity(app, w.Scope(), func(s *owner, cx *gpui.Context[owner]) {
		s.focus = focus
		cx.OnRelease(func(s *owner, a *gpui.App) {
			// The hook runs at the flush boundary: the window scope is
			// already CLOSED here (the close finished before the
			// release processed) — the pinned ordering.
			if w.Scope().State() == gpui.ScopeClosed {
				log.record("release-hook:scope-closed")
			} else {
				log.record("release-hook:scope-open")
			}
			s.focus.Release()
		})
	})

	w.Focus(focus)
	if got := w.Focused(); !got.Ok || got.Handle.ID() != focus.ID() {
		t.Fatalf("focused before close = %+v, want handle %d", got, focus.ID())
	}
	focusLost := 0
	w.OnFocusLost(func(w *gpui.Window) {
		focusLost++
		log.record("focus-lost")
		if w.Focused().Ok {
			t.Error("focus still set inside the focus-lost dispatch")
		}
	})

	// The close cascade: the window leaves the registry, its scope
	// closes, the lease release is queued, the flush at the update
	// boundary runs the release hook, which releases the focused handle
	// (focus-lost fires once), and the element runtime drops.
	gpui.CloseTestWindow(w)

	if _, ok := app.Window(w.ID()); ok {
		t.Fatal("the closed window is still in the application registry")
	}
	if got := w.Scope().State(); got != gpui.ScopeClosed {
		t.Fatalf("window scope state after close = %v, want closed", got)
	}
	if focusLost != 1 {
		t.Fatalf("focus-lost notifications through the close cascade = %d, want 1 (exactly once)", focusLost)
	}
	events := log.snapshot()
	if len(events) != 2 || events[0] != "release-hook:scope-closed" || events[1] != "focus-lost" {
		t.Fatalf("close-cascade event order = %v, want [release-hook:scope-closed focus-lost] (the hook runs at the flush boundary, after the scope closed, and releases the handle; focus-lost follows)", events)
	}
	// The focused release cleared the window focus; a released handle
	// never refocuses (the weak upgrade failure is pinned by
	// focusspec's TestFocusHandleLifecycle).
	w.Focus(focus)
	if w.Focused().Ok {
		t.Fatal("the released handle refocused after the window closed")
	}
	// The element runtime retired with the window (the teardown now
	// wired into removeWindow — the reference window-removal trail,
	// app.rs 1978-1985).
	if got := gpui.WindowDrawCount(w); got != 0 {
		t.Fatalf("draw count after close = %d, want 0 (the element runtime dropped)", got)
	}
	if _, ok := w.RootView(); ok {
		t.Fatal("the root view survived the window close")
	}

	// Exactly-once: a second close is a no-op.
	gpui.CloseTestWindow(w)
	if focusLost != 1 {
		t.Fatalf("focus-lost after the double close = %d, want 1", focusLost)
	}
	if got := w.Scope().State(); got != gpui.ScopeClosed {
		t.Fatalf("window scope state after the double close = %v", got)
	}
}

// TestTwoWindowSharedEntityCloseCascade pins the independent-lease
// corpus across two windows closing in sequence: closing one window
// releases only its own lease (the entity survives for the other
// window), closing the second reaches logical zero and the release
// hook runs EXACTLY ONCE, and the double close is idempotent.
//
// Reference-preserved trace: the reference's window removal drops the
// window's tracked entities' invalidators without destroying shared
// entities (app.rs 1978-1990's trail: "window_handles.remove,
// windows.remove, tracked_entities cleanup" — no entity teardown); the
// entity releases only when every lease is gone. The port's
// equivalent is scope-tree lease release at removeWindow.
func TestTwoWindowSharedEntityCloseCascade(t *testing.T) {
	if testing.Short() {
		t.Skip("draws through the native layout service")
	}
	testApp := gpui.NewTestApp()
	app := testApp.App()
	root := app.RootScope()
	wa := gpui.NewTestWindow(app, gpui.Size{Width: 320, Height: 240})
	wb := gpui.NewTestWindow(app, gpui.Size{Width: 320, Height: 240})

	releases := 0
	shared := gpui.NewEntity(app, root, func(s *sharedModel, cx *gpui.Context[sharedModel]) {
		cx.OnRelease(func(s *sharedModel, a *gpui.App) { releases++ })
	})
	leaseA := shared.RetainInto(wa.Scope())
	leaseB := shared.RetainInto(wb.Scope())
	weak := shared.Downgrade()
	shared.Release() // the creation lease: A and B now hold it independently
	_ = leaseA
	_ = leaseB

	if !weak.IsUpgradable() {
		t.Fatal("the shared entity must be live under its two window leases")
	}

	// Close window A: its lease releases, the entity survives for B.
	gpui.CloseTestWindow(wa)
	if releases != 0 {
		t.Fatalf("release hook ran %d times after closing one of two leasing windows, want 0", releases)
	}
	if !weak.IsUpgradable() {
		t.Fatal("the shared entity died with window A while window B still leases it")
	}
	if _, ok := app.Window(wb.ID()); !ok {
		t.Fatal("closing window A destroyed independent window B")
	}

	// Close window B: logical zero, exactly one release.
	gpui.CloseTestWindow(wb)
	if releases != 1 {
		t.Fatalf("release hook ran %d times across both window closes, want 1 (exactly-once logical release)", releases)
	}
	if weak.IsUpgradable() {
		t.Fatal("the weak handle upgraded after logical zero (zero is irrevocable)")
	}
	// The double close of B changes nothing.
	gpui.CloseTestWindow(wb)
	if releases != 1 {
		t.Fatalf("release hook ran %d times after the double close, want 1", releases)
	}
}

// ---------------------------------------------------------------------------
// Scope cancellation racing result handoff (through WINDOW scopes)
// ---------------------------------------------------------------------------

// TestScopeCancellationRacingResultHandoffThroughWindowClose pins the
// adversarial schedule whose receiving scope is a window scope: the
// worker parks mid-flight, the window close cancels the receiving scope
// (closing the delivery token), and the worker's late completion is
// discarded — with the declared dependency retained until the actual
// acknowledgment (cancellation never releases execution resources) and
// released exactly at the discard.
//
// The bare-scope discard traces are pinned by internal/taskspec
// (TestResultDiscardedAfterScopeClose,
// TestDeliveryDependencyRetainedAcrossOwnerScopeClose); this is the
// window-close composition. Reference-preserved semantics: the
// runtime ownership contract, "Subscriptions and tasks" — "Closing a
// scope suppresses late delivery even if the target entity has another
// live owner" and "Execution resources remain registered until the
// foreground acknowledges completion".
func TestScopeCancellationRacingResultHandoffThroughWindowClose(t *testing.T) {
	if testing.Short() {
		t.Skip("draws through the native layout service")
	}
	testApp := gpui.NewTestApp()
	app := testApp.App()
	root := app.RootScope()
	wa := gpui.NewTestWindow(app, gpui.Size{Width: 320, Height: 240})
	wb := gpui.NewTestWindow(app, gpui.Size{Width: 320, Height: 240})

	// The shared dependency: leased independently by window B, so the
	// worker's execution scope owns one lease and B keeps a second.
	dep := gpui.NewEntity(app, root, func(s *int, cx *gpui.Context[int]) {})
	dep.RetainInto(wb.Scope())
	weak := dep.Downgrade()

	delivered := 0
	task := app.SpawnDelivering(wa.Scope(),
		func(run *gpui.TaskRun) int {
			run.Sleep(50 * time.Millisecond)
			return 42
		},
		func(result int, cx *gpui.App) { delivered++ },
		dep) // the creation lease transfers into the execution scope

	// Drive the worker to its park point.
	testApp.RunUntilParked()
	if !weak.IsUpgradable() {
		t.Fatal("the dependency died while the worker is still in flight (execution resources must be retained)")
	}

	// The window close cancels the receiving scope mid-flight.
	gpui.CloseTestWindow(wa)
	if !task.Cancelled() {
		t.Fatal("the window close did not cancel the window-scope task")
	}
	if delivered != 0 {
		t.Fatalf("deliveries before the worker completed = %d, want 0", delivered)
	}
	// Cancellation did NOT release the execution resources: the worker
	// has not acknowledged (contract: cancellation is cooperative).
	if !weak.IsUpgradable() {
		t.Fatal("the dependency was released at cancellation, before the worker acknowledged")
	}

	// The worker's late completion is discarded (the receiving window
	// scope closed), and the dependency releases at the acknowledgment.
	testApp.AdvanceClock(100)
	if delivered != 0 {
		t.Fatalf("a late result delivered after the window scope closed: %d deliveries", delivered)
	}
	if !task.Done() {
		t.Fatal("the worker did not complete after its cancellation wake-up")
	}
	if unresolved := app.UnresolvedExecutions(); len(unresolved) != 0 {
		t.Fatalf("unresolved executions after the discard = %v, want none", unresolved)
	}
	// Window B's independent lease keeps the shared dependency alive
	// (closing the receiving scope does not free another owner's
	// memory).
	if !weak.IsUpgradable() {
		t.Fatal("the shared dependency died although window B still leases it")
	}

	// The accepted-before-close schedule through the second window: a
	// delivery that lands while the window is open is delivered exactly
	// once, and the close afterwards never re-delivers.
	deliveredB := 0
	taskB := app.SpawnDelivering(wb.Scope(),
		func(run *gpui.TaskRun) int { return 7 },
		func(result int, cx *gpui.App) { deliveredB++ })
	testApp.RunUntilParked()
	if deliveredB != 1 {
		t.Fatalf("deliveries into the open window scope = %d, want 1", deliveredB)
	}
	gpui.CloseTestWindow(wb)
	testApp.RunUntilParked()
	if deliveredB != 1 {
		t.Fatalf("deliveries after the window closed = %d, want 1 (no re-delivery)", deliveredB)
	}
	if !taskB.Done() {
		t.Fatal("the delivered task did not finish")
	}
	// Both windows closed: the dependency reached logical zero and
	// released exactly once through the close cascade.
	if weak.IsUpgradable() {
		t.Fatal("the shared dependency outlived both of its leasing windows")
	}

	// New registrations into a closed window scope are rejected (the
	// scope gate; the misuse diagnostic is the pinned contract text).
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("spawning into a closed window scope did not panic")
			}
		}()
		app.SpawnDelivering(wa.Scope(), func(run *gpui.TaskRun) int { return 0 }, func(int, *gpui.App) {})
	}()
}

// ---------------------------------------------------------------------------
// The shutdown drain: effect flushing, unresolved records, rejection
// ---------------------------------------------------------------------------

// TestShutdownDrainFlushesEffectsAndTracksUnresolved pins the drain's
// effect flushing and its unresolved-execution record:
//
//   - Effects queued by a worker's update flush AT THE UPDATE BOUNDARY
//     inside the drain (observers run, deferred callbacks run, FIFO) —
//     the pinned flush loop (app.rs 1743-1830) reached through
//     App.Shutdown's scheduler drain.
//   - A worker that observes cancellation and keeps parking stays
//     unresolved and TRACKED: neither GC nor elapsed time releases it
//     (the report is state, not timing), and the scheduler keeps
//     accepting its terminal completion message afterwards.
//   - New application work is rejected after Shutdown began; Shutdown
//     itself is idempotent.
func TestShutdownDrainFlushesEffectsAndTracksUnresolved(t *testing.T) {
	testApp := gpui.NewTestApp()
	app := testApp.App()
	root := app.RootScope()

	// The observer pair for the drain's flush trace.
	type sig struct{}
	source := gpui.NewEntity(app, root, func(s *sig, cx *gpui.Context[sig]) {})
	observed := 0
	_ = gpui.NewEntity(app, root, func(o *sig, cx *gpui.Context[sig]) {
		cx.Observe(source, func(*sig, gpui.Entity[sig], *gpui.Context[sig]) { observed++ })
	})

	// The worker that ignores cancellation: it parks, and after the
	// cancellation wake-up parks again (a cooperative worker may ignore
	// cancellation forever — the unresolved record).
	_ = app.Background().Spawn(func(run *gpui.TaskRun) string {
		run.Sleep(time.Hour)
		if run.Cancelled() {
			run.Sleep(24 * time.Hour)
		}
		return "done"
	})

	// The completing worker, parked mid-body so it is in flight when
	// shutdown begins: it awaits a parked background worker, and the
	// cancellation wake-up drives both during the drain — the
	// foreground body's update then flushes its queued effects at the
	// update boundary INSIDE the drain (the flush loop of app.rs
	// 1743-1830 reached through Shutdown's scheduler drain).
	park := app.Background().Spawn(func(run *gpui.TaskRun) int {
		run.Sleep(time.Millisecond)
		return 1
	})
	deferRan := false
	_ = app.Spawn(func(cx *gpui.AsyncApp) struct{} {
		cx.Await(park)
		cx.Update(func(a *gpui.App) {
			source.Update(a, func(s *sig, cx *gpui.Context[sig]) { cx.Notify() })
			a.Defer(func(a *gpui.App) { deferRan = true })
		})
		return struct{}{}
	})
	testApp.RunUntilParked()

	// The drain: shutdown without any further drive.
	report := app.Shutdown()
	if observed != 1 {
		t.Fatalf("observer deliveries during the drain = %d, want 1 (the update boundary flushed the queued effect)", observed)
	}
	if !deferRan {
		t.Fatal("the deferred callback queued by the worker's update did not run during the drain")
	}
	if report.Acknowledged < 2 {
		t.Fatalf("report.Acknowledged = %d, want >= 2 (both workers acknowledged during the drain)", report.Acknowledged)
	}
	if len(report.Unresolved) == 0 {
		t.Fatal("the cancellation-ignoring worker was not reported unresolved")
	}
	found := false
	for _, u := range report.Unresolved {
		if u.Cancelled && u.Parked != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no unresolved record reports a cancelled parked worker: %+v", report.Unresolved)
	}

	// Neither GC nor elapsed time substitutes for the logical release:
	// the unresolved record is state, and it survives both.
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	if got := app.UnresolvedExecutions(); len(got) != len(report.Unresolved) {
		t.Fatalf("unresolved executions after GC and elapsed time = %v, want the same tracked record %v", got, report.Unresolved)
	}

	// New application work is rejected (the pinned diagnostic).
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("Spawn after Shutdown did not panic")
			}
		}()
		app.Spawn(func(cx *gpui.AsyncApp) struct{} { return struct{}{} })
	}()

	// The scheduler keeps accepting the terminal completion: the
	// virtual clock's advance drives the parked worker awake, it
	// returns, and the acknowledgment releases the tracked record.
	testApp.AdvanceClock(int64((25 * time.Hour) / time.Millisecond))
	if got := app.UnresolvedExecutions(); len(got) != 0 {
		t.Fatalf("unresolved executions after the late completion = %v, want none (terminal messages stay accepted)", got)
	}

	// Shutdown is idempotent.
	second := app.Shutdown()
	if second.Cancelled != 0 {
		t.Fatalf("second Shutdown cancelled %d new tasks, want 0 (idempotent)", second.Cancelled)
	}
	if len(second.Unresolved) != 0 {
		t.Fatalf("second Shutdown reported unresolved executions %v, want none", second.Unresolved)
	}

	// Effects queued without an update boundary are not flushed by
	// Shutdown itself: the flush boundary stays available and drains
	// them at the next update (the port's honest boundary — Shutdown
	// drains the task layer; the effect queue belongs to the update
	// machinery).
	lateDefer := false
	app.Defer(func(a *gpui.App) { lateDefer = true })
	if lateDefer {
		t.Fatal("an unflushed defer ran without an update boundary")
	}
	app.Update(func(a *gpui.App) {})
	if !lateDefer {
		t.Fatal("the deferred effect queued before the boundary update did not flush at it")
	}
}

// ---------------------------------------------------------------------------
// Abandoned frame builds composed with window closure
// ---------------------------------------------------------------------------

// TestAbandonedFrameBuildAndWindowClose pins the abandoned-build
// contract composed with the close cascade: a build that panics
// mid-frame leaves the previously published frame valid (draw count,
// last scene, root view) and never publishes, a following good frame
// draws normally, and the close after it drops the element runtime
// exactly once.
//
// Reference-preserved trace: the abandoned-build guarantee is the
// runtime ownership contract's "An abandoned build closes uncommitted
// resources and keeps the previously published artifacts valid" (a
// documented Go adaptation of the pin's draw path — window.rs draw
// swaps rendered_frame only on success; the port's render.go holds the
// same gate at its `completed` flag). The window-close teardown is the
// removal trail pinned above.
func TestAbandonedFrameBuildAndWindowClose(t *testing.T) {
	if testing.Short() {
		t.Skip("draws through the native layout service")
	}
	testApp := gpui.NewTestApp()
	app := testApp.App()
	w := gpui.NewTestWindow(app, gpui.Size{Width: 320, Height: 240})
	view := &switchView{}
	w.SetRootView(view)

	scene1, err := gpui.DrawWindowFrame(w)
	if err != nil {
		t.Fatalf("the first frame: %v", err)
	}
	if got := gpui.WindowDrawCount(w); got != 1 {
		t.Fatalf("draw count after the first frame = %d, want 1", got)
	}

	// The abandoned build: the view flips to the panicking element; the
	// panic propagates out of DrawWindowFrame.
	view.panicNext = true
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("the panicking build did not propagate")
			}
		}()
		_, _ = gpui.DrawWindowFrame(w)
	}()

	// The published artifacts are unchanged: draw count, last scene,
	// root view.
	if got := gpui.WindowDrawCount(w); got != 1 {
		t.Fatalf("draw count after the abandoned build = %d, want 1 (an abandoned frame never publishes)", got)
	}
	if got := gpui.LastDrawnScene(w); got != scene1 {
		t.Fatalf("the abandoned build replaced the published scene (want the preserved frame)")
	}
	if _, ok := w.RootView(); !ok {
		t.Fatal("the abandoned build dropped the window's root view")
	}

	// The window still draws: a good frame publishes normally.
	view.panicNext = false
	scene2, err := gpui.DrawWindowFrame(w)
	if err != nil {
		t.Fatalf("the frame after the abandoned build: %v", err)
	}
	if got := gpui.WindowDrawCount(w); got != 2 {
		t.Fatalf("draw count after the recovered frame = %d, want 2", got)
	}
	if scene2 == scene1 {
		t.Fatal("the recovered frame republished the abandoned cycle's scene")
	}

	// The close cascade after the abandonment: the element runtime
	// drops exactly once and the double close is a no-op.
	gpui.CloseTestWindow(w)
	if got := gpui.WindowDrawCount(w); got != 0 {
		t.Fatalf("draw count after the close = %d, want 0 (the element runtime dropped)", got)
	}
	gpui.CloseTestWindow(w)
	if _, ok := w.RootView(); ok {
		t.Fatal("the root view survived the close")
	}
	if got := w.Scope().State(); got != gpui.ScopeClosed {
		t.Fatalf("window scope state after close = %v, want closed", got)
	}
}
