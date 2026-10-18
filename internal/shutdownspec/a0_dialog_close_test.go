//go:build windows

// The modal-dialog half of ticket30's real corpus: a modal prompt
// pumping its nested loop while a FULL close cascade runs (both
// windows closed through WM_CLOSE during the pump), the prompt's
// eventual result discarded (the receiving window scope closed before
// completion), the host's quit-on-last posted during the pump, and the
// app/host teardown landing cleanly afterwards.
//
// Driver choice, recorded honestly: the pumping modal loop here is the
// application's dialog-driver seam (App.SetDialogDriver with a driver
// that pumps the thread's messages until dismissed — the dialogspec
// pumpingDriver model), not the real Win32 IFileDialog. The REAL
// dialog's modal-pump evidence (CoCreateInstance, SetOptions, Show,
// the programmatic dismissal) is pinned by internal/dialogspec's real
// tests, which run the COM dialog without this package's a11y-adapter
// and Windows-graphics-capture churn: mixing the real IFileDialog with
// that churn in one process crosses the pre-existing native COM-churn
// corruption threshold (see TRACES.md — reproduced on the unmodified
// tree). The native DLL is frozen in this slice, so this package keeps
// the shutdown integration (the close cascade, the discard, the quit,
// the drain) on the driver seam and cites the real-dialog evidence
// next door.
package shutdownspec

import (
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"gpui-go/gpui"
)

// Win32 message-pump bindings (the dialogspec real corpus pattern:
// tests bring their own).
var (
	dialogModUser32       = syscall.NewLazyDLL("user32.dll")
	dialogProcPeekMessage = dialogModUser32.NewProc("PeekMessageW")
	dialogProcTranslate   = dialogModUser32.NewProc("TranslateMessage")
	dialogProcDispatch    = dialogModUser32.NewProc("DispatchMessageW")
)

// dialogPMRemove is PeekMessageW's PM_REMOVE (winuser.h).
const dialogPMRemove = 0x0001

// dialogMsg is the MSG record the pump reads and dispatches.
type dialogMsg struct {
	hwnd     uintptr
	message  uint32
	wParam   uintptr
	lParam   uintptr
	time     uint32
	ptX, ptY int32
}

// cascadeDriver is the pumping modal-dialog driver: its Prompt calls
// block in a nested message loop (posting and dispatching, like a
// modal dialog loop) until the registered dismissal releases it. Posted
// foreground runnables queued during the pump wait for the loop to end
// (the wake protocol's documented behavior during modal pumping);
// raw window messages — the close cascade's WM_CLOSE — dispatch inside
// the loop. The driver records the request it served.
type cascadeDriver struct {
	mu      sync.Mutex
	showing bool
	release chan struct{}
	once    sync.Once
	// served reports the driver call completed.
	served chan struct{}
}

func newCascadeDriver() *cascadeDriver {
	return &cascadeDriver{
		release: make(chan struct{}),
		served:  make(chan struct{}),
	}
}

func (d *cascadeDriver) markShowing() {
	d.mu.Lock()
	d.showing = true
	d.mu.Unlock()
}

func (d *cascadeDriver) isShowing() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.showing
}

// dismiss releases the pump (the DialogDismissal seam: Cancel and
// DismissDialog end the modal loop from any goroutine).
func (d *cascadeDriver) dismiss() {
	d.once.Do(func() { close(d.release) })
}

// pump is the nested modal loop.
func (d *cascadeDriver) pump() {
	var m dialogMsg
	for {
		select {
		case <-d.release:
			return
		default:
		}
		r, _, _ := dialogProcPeekMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0, dialogPMRemove)
		if r != 0 {
			dialogProcTranslate.Call(uintptr(unsafe.Pointer(&m)))
			dialogProcDispatch.Call(uintptr(unsafe.Pointer(&m)))
			continue
		}
		// Nothing pending: wait briefly (the dismissal's release channel
		// or the timer re-checks the queue).
		select {
		case <-d.release:
			return
		case <-time.After(time.Millisecond):
		}
	}
}

// PromptForPaths implements gpui.DialogDriver: pump the modal loop and
// report the cancelled outcome (the dismissal path).
func (d *cascadeDriver) PromptForPaths(owner uintptr, options gpui.PathPromptOptions, dismiss func(gpui.DialogDismissal)) gpui.PathsOutcome {
	d.markShowing()
	dismiss(d.dismiss)
	d.pump()
	d.mu.Lock()
	d.served <- struct{}{}
	d.mu.Unlock()
	return gpui.PathsOutcome{Cancelled: true}
}

// PromptForNewPath implements gpui.DialogDriver (unused by this test).
func (d *cascadeDriver) PromptForNewPath(owner uintptr, directory, suggestedName string, dismiss func(gpui.DialogDismissal)) gpui.PathOutcome {
	d.markShowing()
	dismiss(d.dismiss)
	d.pump()
	d.mu.Lock()
	d.served <- struct{}{}
	d.mu.Unlock()
	return gpui.PathOutcome{Cancelled: true}
}

// CanSelectMixedFilesAndDirs implements gpui.DialogDriver (the test
// platform's answer, like the hostless queue driver).
func (d *cascadeDriver) CanSelectMixedFilesAndDirs() bool { return true }

// TestModalDialogPumpingDuringCloseCascade drives the modal prompt
// through the full close cascade:
//
//   - The prompt's receiving scope defaults to the OWNER window's
//     scope (dialogs.go startPrompt), so the cascade's WM_DESTROY of
//     the owner closes the delivery gate before the prompt finishes.
//   - BOTH windows close DURING the modal pump: WM_CLOSE is dispatched
//     by the nested modal loop (posted foreground closures defer to
//     after the modal loop — the wake protocol's documented behavior);
//     each window's WM_DESTROY runs its scope close on the message
//     thread.
//   - The prompt ends through the dismissal seam; its outcome is
//     DISCARDED, never delivered; the delivery callback never runs.
//   - The host's quit-on-last-window, posted during the pump, exits
//     the message loop cleanly once the modal loop ends; App.Run
//     returns without error, and a final Shutdown drain records the
//     task layer's terminal state.
func TestModalDialogPumpingDuringCloseCascade(t *testing.T) {
	f := bootRealApp(t)
	app := f.app

	driver := newCascadeDriver()
	app.SetDialogDriver(driver)

	evA := newCloseEvidence(&releaseLog{})
	evB := newCloseEvidence(&releaseLog{})
	wa := f.openShown(t, "shutdownspec dialog A", 300, 220, evA.onClose)
	wb := f.openShown(t, "shutdownspec dialog B", 300, 220, evB.onClose)

	deliveries := 0
	outcomes := make(chan gpui.PathsOutcome, 1)
	handle := app.PromptForPaths(wa, nil, gpui.PathPromptOptions{
		Files:  true,
		Prompt: "close cascade",
	}, func(outcome gpui.PathsOutcome, cx *gpui.App) {
		deliveries++
		outcomes <- outcome
	})

	// Wait for the modal loop to come up.
	waitFor(t, 10*time.Second, "the modal pump to start", driver.isShowing)

	// The close cascade DURING the modal pump: both windows receive
	// WM_CLOSE; the nested modal loop dispatches them. The WM_DESTROY
	// hook channels are the destruction evidence — a host-marshaled
	// window query (Alive) would itself need the wake whose coalescing
	// is held open by the pump's enclosing drain (the dialogspec corpus
	// keeps to channel evidence during the pump for the same reason).
	postMessage(wa.Handle().Hwnd(), wmClose, 0, 0)
	postMessage(wb.Handle().Hwnd(), wmClose, 0, 0)
	recvWithin(t, 10*time.Second, "window A's WM_DESTROY during the modal pump", evA.closed)
	recvWithin(t, 10*time.Second, "window B's WM_DESTROY during the modal pump", evB.closed)
	// The cascade closed both scopes (the delivery gate is the owner's
	// window scope).
	if got := wa.Scope().State(); got != gpui.ScopeClosed {
		t.Fatalf("the prompt owner's window scope after the cascade = %v, want closed", got)
	}
	if got := wb.Scope().State(); got != gpui.ScopeClosed {
		t.Fatalf("window B's scope after the cascade = %v, want closed", got)
	}

	// End the modal loop through the dismissal seam: the outcome must
	// be discarded, never delivered.
	handle.DismissDialog()
	recvWithin(t, 10*time.Second, "the modal loop to end after the dismissal", driver.served)
	waitFor(t, 10*time.Second, "the prompt request to complete after the cascade", func() bool {
		return !handle.Pending()
	})
	if handle.Delivered() {
		t.Fatalf("the prompt's result delivered after the receiving window closed: %d deliveries", deliveries)
	}
	if !handle.Discarded() {
		t.Fatalf("handle state after the cascade: delivered=%v discarded=%v pending=%v, want discarded",
			handle.Delivered(), handle.Discarded(), handle.Pending())
	}
	if deliveries != 0 {
		t.Fatalf("the delivery callback ran %d times after window closure, want 0", deliveries)
	}
	select {
	case outcome := <-outcomes:
		t.Fatalf("a late prompt result arrived after window closure: %+v", outcome)
	case <-time.After(200 * time.Millisecond):
	}

	// The host quit on the last window during the pump: the loop exits
	// cleanly once the modal pump ends, and App.Run returns the loop's
	// nil terminal state.
	if err := f.host.Stop(); err != nil {
		t.Logf("host stop after the quit-on-last: %v (the loop may already have exited)", err)
	}
	if err := f.waitRun(); err != nil {
		t.Fatalf("App.Run after the close cascade and the modal pump: %v", err)
	}

	// The application is still coherent after the cascade: the task
	// layer still drains and records its terminal state (a final
	// Shutdown from the joined test thread — the loop has exited, so
	// the drain is uncontended).
	report := app.Shutdown()
	for _, u := range report.Unresolved {
		t.Logf("unresolved at the post-cascade shutdown: %s", u.String())
	}
}
