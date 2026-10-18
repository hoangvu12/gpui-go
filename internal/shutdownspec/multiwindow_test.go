//go:build windows

// Real multiwindow close/reopen cases (ticket30's real half): the close
// cascade through WM_CLOSE/WM_DESTROY with a shared entity leased by
// two windows, delayed GPU completion (submissions retiring after
// window removal — the ledger's post-close behavior), callbacks
// arriving after window removal (the generation-checked refusals), and
// the app shutdown drain accepting renderer retirement after new
// application work was rejected.
//
// Environment expectation: real Win32 windows in the user's interactive
// session, a real D3D11 adapter; FAIL never skip. All window
// interaction is the public API and synthetic messages (PostMessage of
// WM_CLOSE through Window.Close) — never real cursor input.
package shutdownspec

import (
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

// closeEvidence is one real window's close-cascade record.
type closeEvidence struct {
	// closed fires from the WM_DESTROY path (the OnClose hook).
	closed chan struct{}
	// log is the shared cross-thread event log.
	log *releaseLog
}

// newCloseEvidence builds the evidence pair for one window.
func newCloseEvidence(log *releaseLog) *closeEvidence {
	return &closeEvidence{closed: make(chan struct{}, 1), log: log}
}

// onClose is the WindowOptions.OnClose hook (foreground thread, after
// the OS window is destroyed, before the host retires the registry
// entry).
func (e *closeEvidence) onClose() {
	select {
	case e.closed <- struct{}{}:
	default:
	}
	e.log.record("wm-destroy")
}

// TestMultiwindowCloseReopenDelayedGPUCompletion drives the full real
// close/reopen cascade:
//
//   - Two real windows sharing one entity through independent
//     window-scope leases: closing one window releases only its lease
//     (the entity survives for the other window, exactly as the
//     reference's window-removal trail drops invalidators without
//     destroying shared entities, app.rs 1978-1990); closing the
//     second reaches logical zero and the release hook runs exactly
//     once.
//   - Delayed GPU completion: a submission accepted before the close
//     retires AFTER the window's removal (the manual renderer holds the
//     completion until the poll) — the ledger's post-close behavior is
//     the exactly-once terminal record, never a drop.
//   - Callbacks arriving after window removal: every window-lease
//     operation is refused through the stale-lease generation check
//     (ErrWindowClosed), the double close is a no-op, and a present
//     through the removed window is the typed renderer refusal.
//   - Reopen: a new window through the same renderer presents and
//     retires normally after the earlier window's removal.
//   - Exactly-once teardown: the surface of the removed window destroys
//     only after its submission retired (the native ERR_PENDING guard
//     is the close ordering: drain, then detach), and the renderer's
//     Close is guarded against a second call.
func TestMultiwindowCloseReopenDelayedGPUCompletion(t *testing.T) {
	f := bootRealApp(t)
	app := f.app

	log := &releaseLog{}
	evA := newCloseEvidence(log)
	evB := newCloseEvidence(log)
	wa := f.openShown(t, "shutdownspec multiwindow A", 280, 200, evA.onClose)
	wb := f.openShown(t, "shutdownspec multiwindow B", 280, 200, evB.onClose)
	// The keeper window stays open through the whole test: closing the
	// test's own windows must not fire the host's quit-on-last while the
	// remaining assertions still need the foreground thread (the quit
	// cascade itself is the modal-dialog test's record).
	wk := f.openShown(t, "shutdownspec multiwindow keeper", 200, 120, nil)
	_ = wk

	// The shared entity, leased independently by both windows (created
	// and leased on the foreground thread).
	var weak gpui.WeakEntity[sharedModel]
	f.sync(t, func() {
		shared := gpui.NewEntity(app, app.RootScope(), func(s *sharedModel, cx *gpui.Context[sharedModel]) {
			cx.OnRelease(func(s *sharedModel, a *gpui.App) {
				// The hook runs at the flush boundary of the WM_DESTROY
				// update: the window scope is already closed here.
				if wb.Scope().State() == gpui.ScopeClosed {
					log.record("release:scope-closed")
				} else {
					log.record("release:scope-open")
				}
			})
		})
		shared.RetainInto(wa.Scope())
		shared.RetainInto(wb.Scope())
		shared.Release() // the creation lease
		weak = shared.Downgrade()
	})

	// The manual renderer: completion is under the test's control, so
	// the retirement provably happens after the window removal.
	renderer, err := gpui.NewRendererManual()
	if err != nil {
		t.Fatalf("gpui.NewRendererManual: %v", err)
	}
	t.Cleanup(func() {
		if err := renderer.Close(); err != nil && !isRendererClosedError(err) {
			t.Errorf("renderer close: %v", err)
		}
	})
	surfaceA, err := renderer.GetOrCreateSurface(wa)
	if err != nil {
		t.Fatalf("GetOrCreateSurface(A): %v", err)
	}
	subA, err := surfaceA.PresentClear(gpui.Color{R: 0.2, G: 0.4, B: 0.9, A: 1})
	if err != nil {
		t.Fatalf("PresentClear(A): %v", err)
	}
	surfaceB, err := renderer.GetOrCreateSurface(wb)
	if err != nil {
		t.Fatalf("GetOrCreateSurface(B): %v", err)
	}
	subB, err := surfaceB.PresentClear(gpui.Color{R: 0.9, G: 0.4, B: 0.2, A: 1})
	if err != nil {
		t.Fatalf("PresentClear(B): %v", err)
	}

	// Close window A through the WM_CLOSE veto path (no veto callback:
	// DefWindowProcW destroys, events.rs handle_close_msg's None arm).
	removedAt := time.Now()
	wa.Close()
	recvWithin(t, 10*time.Second, "window A's WM_DESTROY during the close cascade", evA.closed)
	waitFor(t, 10*time.Second, "window A to be destroyed", func() bool { return !wa.Alive() })

	// The close cascade retired the window: registry removal, scope
	// close, element-runtime teardown (WindowDrawCount 0), exactly one
	// WM_DESTROY record.
	var registryA bool
	f.sync(t, func() {
		_, registryA = app.Window(wa.ID())
	})
	if registryA {
		t.Fatal("the destroyed window is still in the application registry")
	}
	if got := wa.Scope().State(); got != gpui.ScopeClosed {
		t.Fatalf("window A's scope after close = %v, want closed", got)
	}
	if got := gpui.WindowDrawCount(wa); got != 0 {
		t.Fatalf("window A's draw count after close = %d, want 0 (the element runtime dropped)", got)
	}
	// The shared entity survives window A's removal through window B's
	// independent lease.
	var upgradable bool
	f.sync(t, func() { upgradable = weak.IsUpgradable() })
	if !upgradable {
		t.Fatal("the shared entity died with window A while window B still leases it")
	}

	// Callbacks arriving after window removal: the stale-lease
	// generation check refuses every value query; fire-and-forget
	// operations (including a second Close) are no-ops; a present
	// through the removed window is the typed renderer refusal.
	if _, err := wa.Title(); !errors.Is(err, gpui.ErrWindowClosed) {
		t.Fatalf("Title() after removal = %v, want ErrWindowClosed", err)
	}
	if _, err := wa.Bounds(); !errors.Is(err, gpui.ErrWindowClosed) {
		t.Fatalf("Bounds() after removal = %v, want ErrWindowClosed", err)
	}
	wa.Close() // the double close: a stale-lease no-op
	time.Sleep(100 * time.Millisecond)
	if wa.Alive() {
		t.Fatal("the double close resurrected window A")
	}
	select {
	case <-evA.closed:
		t.Fatal("the double close delivered a second WM_DESTROY")
	default:
	}
	if !wb.Alive() {
		t.Fatal("closing window A destroyed the independent window B")
	}
	if _, err := renderer.PresentClear(wa, gpui.Color{}); !errors.Is(err, gpui.ErrRendererWindowClosed) {
		t.Fatalf("PresentClear through the removed window = %v, want the typed renderer window-closed refusal", err)
	}

	// Delayed GPU completion: the submission accepted before the removal
	// completes and retires AFTER it (the manual renderer held the
	// completion). The ledger's post-close behavior is the exactly-once
	// terminal record.
	if _, err := subA.WaitForCompletion(15 * time.Second); err != nil {
		t.Fatalf("the delayed submission did not complete after the window removal: %v", err)
	}
	record, err := subA.Retire()
	if err != nil {
		t.Fatalf("Retire after the window removal: %v", err)
	}
	entry, ok := renderer.LedgerEntry(subA.ID())
	if !ok || entry.State != "retired" {
		t.Fatalf("the delayed submission's ledger entry = %+v, want state retired", entry)
	}
	if !entry.RetiredAt.After(removedAt) {
		t.Fatalf("the submission retired at %v, before the window removal at %v", entry.RetiredAt, removedAt)
	}
	_ = record // the terminal value is the completed retire (native retire_terminal::COMPLETED)

	// Reopen: a new window presents and retires through the same
	// renderer after the earlier removal.
	wc := f.openShown(t, "shutdownspec multiwindow C", 280, 200, nil)
	subC, err := renderer.PresentClear(wc, gpui.Color{R: 0.3, G: 0.8, B: 0.3, A: 1})
	if err != nil {
		t.Fatalf("PresentClear(C) after the reopen: %v", err)
	}
	if _, err := subC.WaitForCompletion(15 * time.Second); err != nil {
		t.Fatalf("the reopened window's submission did not complete: %v", err)
	}
	if _, err := subC.Retire(); err != nil {
		t.Fatalf("Retire(C): %v", err)
	}
	wc.Close()
	waitFor(t, 10*time.Second, "the reopened window to close", func() bool { return !wc.Alive() })

	// Close window B: the shared entity reaches logical zero through the
	// same cascade and the release hook runs exactly once, at the flush
	// boundary (the scope already closed).
	wb.Close()
	recvWithin(t, 10*time.Second, "window B's WM_DESTROY during the close cascade", evB.closed)
	waitFor(t, 10*time.Second, "window B to be destroyed", func() bool { return !wb.Alive() })
	events := log.snapshot()
	releases := 0
	for _, e := range events {
		if e == "release:scope-closed" {
			releases++
		}
	}
	if releases != 1 {
		t.Fatalf("the shared entity's release hook ran %d times across both window closes, want exactly 1 (at the flush boundary)", releases)
	}
	f.sync(t, func() { upgradable = weak.IsUpgradable() })
	if upgradable {
		t.Fatal("the weak handle upgraded after the entity's logical zero")
	}

	// Window B's submission retires after its removal too (the delayed
	// completion again).
	if _, err := subB.WaitForCompletion(15 * time.Second); err != nil {
		t.Fatalf("window B's delayed submission did not complete: %v", err)
	}
	if _, err := subB.Retire(); err != nil {
		t.Fatalf("Retire(B) after the removal: %v", err)
	}

	// The surface teardown ordering: a surface with un-retired
	// submissions refuses to be destroyed; after the retire it detaches
	// cleanly (the native ERR_PENDING close guard, renderer.rs
	// surface_destroy_body).
	if err := surfaceA.Destroy(); err != nil {
		t.Fatalf("Destroy(A) after its submission retired: %v", err)
	}
	if err := surfaceB.Destroy(); err != nil {
		t.Fatalf("Destroy(B) after its submission retired: %v", err)
	}

	// The renderer's own close is guarded against a second call.
	if err := renderer.Close(); err != nil {
		t.Fatalf("renderer.Close(): %v", err)
	}
	if err := renderer.Close(); !isRendererClosedError(err) {
		t.Fatalf("the second renderer.Close() = %v, want the already-closed refusal", err)
	}

	// The ledger reconciles: every accepted submission has a terminal
	// record, none dropped, none in flight.
	_, accepted, retired, quarantined, inFlight := renderer.Ledger()
	if accepted != 3 || retired != 3 || quarantined != 0 || inFlight != 0 {
		t.Fatalf("ledger counters accepted=%d retired=%d quarantined=%d in-flight=%d, want 3/3/0/0 (A, B and the reopened C)", accepted, retired, quarantined, inFlight)
	}
}

// isRendererClosedError reports the renderer's already-closed refusal.
func isRendererClosedError(err error) bool {
	return err != nil && err.Error() == "gpui: renderer already closed"
}

// TestRealAppShutdownAcceptsRendererRetirementAfterShutdown pins the
// app-shutdown half: App.Shutdown rejects new application work while
// the renderer's terminal retirement messages keep being accepted —
// a submission presented after the shutdown began still retires through
// the pacing worker without panicking the worker on the Spawn
// rejection, the ledger records the terminal state, the
// cancellation-ignoring worker stays unresolved and TRACKED (neither GC
// nor elapsed time releases it), and the host teardown afterwards
// completes cleanly.
func TestRealAppShutdownAcceptsRendererRetirementAfterShutdown(t *testing.T) {
	f := bootRealApp(t)
	app := f.app

	evA := newCloseEvidence(&releaseLog{})
	wa := f.openShown(t, "shutdownspec shutdown A", 280, 200, evA.onClose)
	wb := f.openShown(t, "shutdownspec shutdown B", 280, 200, nil)

	// The pacing renderer (the default real-app shape).
	renderer, err := gpui.NewRenderer()
	if err != nil {
		t.Fatalf("gpui.NewRenderer: %v", err)
	}
	t.Cleanup(func() {
		if err := renderer.Close(); err != nil && !isRendererClosedError(err) {
			t.Errorf("renderer close: %v", err)
		}
	})
	subA, err := renderer.PresentClear(wa, gpui.Color{R: 0.5, G: 0.5, B: 0.5, A: 1})
	if err != nil {
		t.Fatalf("PresentClear(A): %v", err)
	}

	// The cancellation-ignoring worker: parks, and after the
	// cancellation wake-up parks again (real mode never advances the
	// virtual clock — the honest unresolved record, see TRACES.md).
	_ = app.Background().Spawn(func(run *gpui.TaskRun) string {
		run.Sleep(time.Hour)
		if run.Cancelled() {
			run.Sleep(24 * time.Hour)
		}
		return "done"
	})

	// Close window A while its submission may still be in flight (the
	// close cascade and the GPU work overlap).
	wa.Close()
	recvWithin(t, 10*time.Second, "window A's WM_DESTROY during the shutdown schedule", evA.closed)
	waitFor(t, 10*time.Second, "window A to be destroyed", func() bool { return !wa.Alive() })

	// The app shutdown, on the foreground thread: new application work
	// is rejected from here on.
	var report gpui.ShutdownReport
	f.sync(t, func() { report = app.Shutdown() })
	if len(report.Unresolved) == 0 {
		t.Fatal("the cancellation-ignoring worker was not reported unresolved at shutdown")
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

	// New application work is rejected (the pinned diagnostic).
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("Spawn after the real-app Shutdown did not panic")
			}
		}()
		app.Spawn(func(cx *gpui.AsyncApp) struct{} { return struct{}{} })
	}()

	// Neither GC nor elapsed time releases the unresolved execution:
	// the tracked record is state.
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	if got := app.UnresolvedExecutions(); len(got) == 0 {
		t.Fatal("the unresolved execution record disappeared after GC and elapsed time (logical release only)")
	}

	// Terminal retirement stays accepted after new work was rejected: a
	// submission presented after the shutdown began (the renderer's own
	// gate is Renderer.Close — the app-shutdown/renderer-close
	// integration is recorded honestly in TRACES.md) retires through
	// the pacing worker, whose dispatch lands directly in the ledger
	// instead of a rejected App.Spawn.
	subB, err := renderer.PresentClear(wb, gpui.Color{R: 0.25, G: 0.75, B: 0.25, A: 1})
	if err != nil {
		t.Fatalf("PresentClear(B) after the app shutdown began: %v", err)
	}
	if err := subB.WaitForRetirement(15 * time.Second); err != nil {
		t.Fatalf("the post-shutdown submission did not retire: %v", err)
	}
	entryB, ok := renderer.LedgerEntry(subB.ID())
	if !ok || entryB.State != "retired" {
		t.Fatalf("the post-shutdown submission's ledger entry = %+v, want state retired", entryB)
	}
	// The pre-shutdown submission (window A's) is terminal too — the
	// retirement raced the shutdown either way, and neither outcome
	// panicked the pacing worker.
	if err := subA.WaitForRetirement(15 * time.Second); err != nil {
		t.Fatalf("window A's submission did not retire across the shutdown: %v", err)
	}
	_, accepted, retired, quarantined, inFlight := renderer.Ledger()
	if accepted != 2 || retired != 2 || quarantined != 0 || inFlight != 0 {
		t.Fatalf("ledger counters accepted=%d retired=%d quarantined=%d in-flight=%d, want 2/2/0/0", accepted, retired, quarantined, inFlight)
	}

	// The renderer closes after the app shut down but while the host
	// thread (and its COM apartment) is still alive — the native surface
	// teardown releases apartment-affine composition objects, so it
	// must not run after the apartment died (the contract's ordering:
	// app shutdown drains work BEFORE destroying native owners, with
	// the host outliving the renderer).
	if err := renderer.Close(); err != nil {
		t.Fatalf("renderer.Close() after the app shutdown: %v", err)
	}
	_, accepted, retired, _, inFlight = renderer.Ledger()
	if accepted != retired || inFlight != 0 {
		t.Fatalf("ledger after the renderer close: accepted=%d retired=%d in-flight=%d, want accepted==retired and none in flight", accepted, retired, inFlight)
	}

	// The host teardown completes cleanly with the app shut down (the
	// remaining window's WM_DESTROY runs its scope close; the drain
	// already rejected nothing it needs).
	if err := f.host.Stop(); err != nil {
		t.Fatalf("host stop after the app shutdown: %v", err)
	}
	if err := f.waitRun(); err != nil {
		t.Fatalf("App.Run after the host stop: %v", err)
	}
}
