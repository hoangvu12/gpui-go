//go:build windows

// Real-host evidence for ticket25: the no-interaction dialog COM probe,
// nested modal pumping with a pumping dialog driver (no real dialog, no
// user input), late-result discard after real owner window closure, a
// REAL IFileDialog closed programmatically (deterministic cancellation
// without user input), and real Windows Credential Manager round trips
// through the scoped task seam with caller-supplied temporary entries.
//
// Environment expectation: the user's interactive Windows session, like
// internal/winhostspec; failures are failures, never skips. The tests
// never enumerate or print unrelated credentials — only per-run
// temporary test entries, cleaned up exactly as created.
package dialogspec

import (
	"bytes"
	crand "crypto/rand"
	"encoding/hex"
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"gpui-go/gpui"
)

// ---------------------------------------------------------------------------
// Real-host helpers
// ---------------------------------------------------------------------------

func waitForReal(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out after %v waiting for %s", timeout, what)
}

func recvWithinReal[T any](t *testing.T, timeout time.Duration, what string, ch chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(timeout):
		t.Fatalf("timed out after %v waiting for %s", timeout, what)
		var zero T
		return zero
	}
}

// newRealHost boots a fresh host, stopping it (and checking the clean
// thread join — the balanced OleUninitialize is the apartment-release
// evidence after the COM work) at cleanup.
func newRealHost(t *testing.T) *gpui.Host {
	t.Helper()
	before := runtime.NumGoroutine()
	h := gpui.NewHost()
	if err := h.Start(); err != nil {
		t.Fatalf("host start failed: %v", err)
	}
	t.Cleanup(func() {
		if err := h.Stop(); err != nil {
			t.Errorf("host stop failed: %v (the OLE apartment must release cleanly after the COM work)", err)
		}
		waitForReal(t, 5*time.Second, "goroutines to drain back near the baseline", func() bool {
			return runtime.NumGoroutine() <= before+2
		})
	})
	return h
}

// newRealApp attaches a fresh real application to the host.
func newRealApp(t *testing.T, h *gpui.Host) *gpui.App {
	t.Helper()
	app := gpui.NewApp()
	if err := app.Attach(h); err != nil {
		t.Fatalf("app attach failed: %v", err)
	}
	return app
}

// realWindowOptions opens one shown window; onClose (optional) observes
// the WM_DESTROY path.
func openShownReal(t *testing.T, app *gpui.App, title string, onClose func()) *gpui.Window {
	t.Helper()
	w, err := app.OpenWindow(gpui.WindowOptions{
		Title:     title,
		Bounds:    gpui.Bounds{Size: gpui.Size{Width: 240, Height: 140}},
		Show:      true,
		OnClose:   onClose,
		Resizable: true,
	})
	if err != nil {
		t.Fatalf("OpenWindow(%q) failed: %v — this session is expected to create real windows", title, err)
	}
	return w
}

// newTestCredentialURL mints a unique per-run credential key; the caller
// must register its own cleanup.
func newTestCredentialURL(t *testing.T) string {
	t.Helper()
	var b [8]byte
	if _, err := crand.Read(b[:]); err != nil {
		t.Fatalf("crypto/rand: %v", err)
	}
	return "https://gpui-go.dialogspec.test/" + hex.EncodeToString(b[:])
}

// deliverErr and deliverResult adapt the scoped-task delivery callbacks
// to channels for the waiting tests.
func deliverErr(ch chan error) func(error, *gpui.App) {
	return func(err error, cx *gpui.App) {
		if cx == nil {
			ch <- fmt.Errorf("deliver callback received a nil App context")
			return
		}
		ch <- err
	}
}

func deliverResult(ch chan gpui.CredentialsResult) func(gpui.CredentialsResult, *gpui.App) {
	return func(result gpui.CredentialsResult, cx *gpui.App) {
		if cx == nil {
			ch <- gpui.CredentialsResult{Err: fmt.Errorf("deliver callback received a nil App context")}
			return
		}
		ch <- result
	}
}

// ---------------------------------------------------------------------------
// The pumping dialog driver (deterministic, no real dialog)
// ---------------------------------------------------------------------------

var (
	realModUser32        = syscall.NewLazyDLL("user32.dll")
	realPeekMessageW     = realModUser32.NewProc("PeekMessageW")
	realTranslateMessage = realModUser32.NewProc("TranslateMessage")
	realDispatchMessageW = realModUser32.NewProc("DispatchMessageW")
	realPostMessageW     = realModUser32.NewProc("PostMessageW")
)

const (
	realPMRemove = 0x0001
	realWMClose  = 0x0010
)

type realPoint struct{ x, y int32 }

// realMsg is Win32 MSG (48 bytes on amd64).
type realMsg struct {
	hwnd    uintptr
	message uint32
	wparam  uintptr
	lparam  uintptr
	time    uint32
	pt      realPoint
	_pad    uint32
}

// pumpingDriver emulates IFileDialog::Show's nested modal loop without
// showing any dialog: it pumps the host thread's message queue (the
// nested modal pump) until released through its dismissal or release
// channel, then reports the cancelled outcome. Its recorded requests
// give the tests their trace.
type pumpingDriver struct {
	mu      sync.Mutex
	showing bool
	release chan struct{}
	once    sync.Once
	// Recorded request trace.
	pathsOwner   uintptr
	pathsOptions gpui.PathPromptOptions
	newPathReq   int
}

func newPumpingDriver() *pumpingDriver {
	return &pumpingDriver{release: make(chan struct{})}
}

func (d *pumpingDriver) markShowing() {
	d.mu.Lock()
	d.showing = true
	d.mu.Unlock()
}

func (d *pumpingDriver) isShowing() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.showing
}

// dismiss releases the pump; registered as the DialogDismissal so
// Cancel/DismissDialog end the modal loop from any goroutine.
func (d *pumpingDriver) dismiss() {
	d.once.Do(func() { close(d.release) })
}

// pump is the nested modal loop: pump the thread's messages (posting
// and dispatching, like a modal dialog loop) until released. Posted
// foreground runnables queued during the pump wait for the loop to end —
// the wake protocol's documented behavior during modal pumping, matching
// the reference dispatcher; raw window messages (WM_CLOSE from a user
// click, our dismissal message) dispatch inside the loop.
func (d *pumpingDriver) pump() {
	var m realMsg
	for {
		select {
		case <-d.release:
			return
		default:
		}
		r, _, _ := realPeekMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0, realPMRemove)
		if r != 0 {
			realTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
			realDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
			continue
		}
		time.Sleep(time.Millisecond)
	}
}

func (d *pumpingDriver) CanSelectMixedFilesAndDirs() bool { return false }

func (d *pumpingDriver) PromptForPaths(owner uintptr, options gpui.PathPromptOptions, dismiss func(gpui.DialogDismissal)) gpui.PathsOutcome {
	d.mu.Lock()
	d.pathsOwner = owner
	d.pathsOptions = options
	d.mu.Unlock()
	d.markShowing()
	dismiss(d.dismiss)
	d.pump()
	return gpui.PathsOutcome{Cancelled: true}
}

func (d *pumpingDriver) PromptForNewPath(owner uintptr, directory, suggestedName string, dismiss func(gpui.DialogDismissal)) gpui.PathOutcome {
	d.mu.Lock()
	d.newPathReq++
	d.mu.Unlock()
	d.markShowing()
	dismiss(d.dismiss)
	d.pump()
	return gpui.PathOutcome{Cancelled: true}
}

// errorDriver fails without showing anything (error-path evidence on a
// real host).
type errorDriver struct{}

func (errorDriver) CanSelectMixedFilesAndDirs() bool { return false }

func (errorDriver) PromptForPaths(owner uintptr, options gpui.PathPromptOptions, dismiss func(gpui.DialogDismissal)) gpui.PathsOutcome {
	return gpui.PathsOutcome{Err: fmt.Errorf("dialogspec: injected dialog failure")}
}

func (errorDriver) PromptForNewPath(owner uintptr, directory, suggestedName string, dismiss func(gpui.DialogDismissal)) gpui.PathOutcome {
	return gpui.PathOutcome{Err: fmt.Errorf("dialogspec: injected dialog failure")}
}

// ---------------------------------------------------------------------------
// The COM probe
// ---------------------------------------------------------------------------

// TestDialogProbeCOMRoundTrip runs the no-interaction dialog COM probe
// on the host's STA thread: both dialog classes are created, the
// FILEOPENDIALOGOPTIONS round trip is verified against Windows, the
// save dialog is configured from a real directory item whose
// GetDisplayName allocation is freed with CoTaskMemFree, and every
// interface is Released. No dialog is shown; the clean host Stop in the
// cleanup is the apartment-release evidence.
func TestDialogProbeCOMRoundTrip(t *testing.T) {
	h := newRealHost(t)
	dir := t.TempDir()

	report := gpui.ProbeFileDialogs(h, dir)
	if report.Err != nil {
		t.Fatalf("dialog COM probe failed: %v\nsteps: %v", report.Err, report.Steps)
	}

	// FOS_FILEMUSTEXIST|FOS_ALLOWMULTISELECT|FOS_PICKFOLDERS = 0x1220.
	if report.OptionsRoundTrip != 0x1220 {
		t.Fatalf("options round trip = 0x%x, want 0x1220", report.OptionsRoundTrip)
	}

	wantSteps := []string{
		"cocreate FileOpenDialog",
		"set-options 0x1220",
		"get-options 0x1220 (round trip verified)",
		"set-ok-button-label",
		"cocreate FileSaveDialog",
		"shcreateitem-from-parsing-name",
		"set-folder",
		"set-file-name",
		"set-file-types All files *.*",
		"release IShellItem",
		"release FileSaveDialog",
		"release FileOpenDialog",
	}
	for _, want := range wantSteps {
		found := false
		for _, step := range report.Steps {
			if step == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("probe steps missing %q; got %v", want, report.Steps)
		}
	}
	// Every GetDisplayName allocation is CoTaskMemFree'd (recorded).
	freed := false
	for _, step := range report.Steps {
		if len(step) > len("get-display-name") && step[:len("get-display-name")] == "get-display-name" &&
			bytes.Contains([]byte(step), []byte("freed with CoTaskMemFree")) {
			freed = true
		}
	}
	if !freed {
		t.Errorf("probe did not record a CoTaskMemFree'd display name: %v", report.Steps)
	}
}

// TestPromptErrorOutcomeDeliveredOnRealHost checks the failure result
// delivery through the host dispatch seam without any dialog.
func TestPromptErrorOutcomeDeliveredOnRealHost(t *testing.T) {
	h := newRealHost(t)
	app := newRealApp(t, h)
	app.SetDialogDriver(errorDriver{})

	outcomes := make(chan gpui.PathsOutcome, 1)
	handle := app.PromptForPaths(nil, nil, gpui.PathPromptOptions{Files: true}, func(outcome gpui.PathsOutcome, cx *gpui.App) {
		outcomes <- outcome
	})
	outcome := recvWithinReal(t, 10*time.Second, "the error outcome", outcomes)
	if outcome.Err == nil || outcome.Cancelled || len(outcome.Paths) != 0 {
		t.Fatalf("outcome = %+v, want the injected failure", outcome)
	}
	if !handle.Delivered() {
		t.Fatal("the handle must report delivery")
	}
}

// ---------------------------------------------------------------------------
// Nested modal pumping
// ---------------------------------------------------------------------------

// TestPromptNestedModalPumpingKeepsWindowResponsive demonstrates the
// modal pumping contract: while a dialog pumps on the host thread, an
// independent window processes its own messages (a WM_CLOSE, as from a
// user clicking its close button, destroys it DURING the nested modal
// loop) and the prompt result still delivers afterwards.
func TestPromptNestedModalPumpingKeepsWindowResponsive(t *testing.T) {
	h := newRealHost(t)
	app := newRealApp(t, h)
	aClosed := make(chan struct{})
	wA := openShownReal(t, app, "dialogspec pump A", func() { close(aClosed) })
	bClosed := make(chan struct{})
	wB := openShownReal(t, app, "dialogspec pump B", func() { close(bClosed) })

	driver := newPumpingDriver()
	app.SetDialogDriver(driver)

	outcomes := make(chan gpui.PathsOutcome, 1)
	handle := app.PromptForPaths(wA, nil, gpui.PathPromptOptions{Files: true, Prompt: "pump"}, func(outcome gpui.PathsOutcome, cx *gpui.App) {
		outcomes <- outcome
	})
	waitForReal(t, 10*time.Second, "the dialog driver to start pumping", driver.isShowing)

	// Window B stays responsive: its WM_CLOSE is processed inside the
	// nested modal loop (the pumping dialog dispatches thread messages).
	// No host-marshaled query is used here: posted foreground closures
	// wait for the modal loop to end (the wake protocol's documented
	// behavior, matching the reference dispatcher during dialog
	// pumping), so the evidence must be the window's own message
	// processing.
	realPostMessageW.Call(wB.Handle().Hwnd(), realWMClose, 0, 0)
	recvWithinReal(t, 10*time.Second, "window B's close to run during the modal pump", bClosed)

	// Window A (the dialog owner) was not destroyed by B's close.
	select {
	case <-aClosed:
		t.Fatal("window A must survive the other window's close")
	default:
	}
	if app.CanSelectMixedFilesAndDirs() {
		t.Fatal("the real Windows answer must be false (FOS_PICKFOLDERS toggles files/folders)")
	}

	// End the dialog deterministically through the dismissal seam (no
	// user input): the cancelled outcome delivers afterwards.
	handle.DismissDialog()
	outcome := recvWithinReal(t, 10*time.Second, "the prompt outcome after the pump", outcomes)
	if !outcome.Cancelled || outcome.Err != nil {
		t.Fatalf("outcome = %+v, want the cancelled result", outcome)
	}
	if !handle.Delivered() || handle.Discarded() {
		t.Fatalf("handle state: delivered=%v discarded=%v", handle.Delivered(), handle.Discarded())
	}

	// The driver saw the owner window and the options.
	driver.mu.Lock()
	owner := driver.pathsOwner
	options := driver.pathsOptions
	driver.mu.Unlock()
	if owner != wA.Handle().Hwnd() {
		t.Fatalf("dialog owner = %#x, want window A's hwnd %#x", owner, wA.Handle().Hwnd())
	}
	if options.Prompt != "pump" || !options.Files {
		t.Fatalf("dialog options = %+v, want the requested options", options)
	}

	// After the dialog ended, the host processes posted work again and
	// window A is still alive.
	if !wA.Alive() {
		t.Fatal("the owner window must survive the dialog and the other window's close")
	}
	wA.Close()
	waitForReal(t, 10*time.Second, "window A to close after the dialog ended", func() bool { return !wA.Alive() })
}

// TestPromptResultDiscardedAfterOwnerWindowClose is the real late-result
// trace: the owner window is closed (WM_CLOSE processed inside the
// nested modal loop, like a user clicking X while the dialog is up), its
// scope closes, and the dialog's eventual result is discarded — never
// delivered.
func TestPromptResultDiscardedAfterOwnerWindowClose(t *testing.T) {
	h := newRealHost(t)
	app := newRealApp(t, h)
	aClosed := make(chan struct{})
	wA := openShownReal(t, app, "dialogspec late A", func() { close(aClosed) })

	driver := newPumpingDriver()
	app.SetDialogDriver(driver)

	deliveries := 0
	outcomes := make(chan gpui.PathsOutcome, 1)
	handle := app.PromptForPaths(wA, nil, gpui.PathPromptOptions{Files: true}, func(outcome gpui.PathsOutcome, cx *gpui.App) {
		deliveries++
		outcomes <- outcome
	})
	waitForReal(t, 10*time.Second, "the dialog driver to start pumping", driver.isShowing)

	// Close the owner window while the dialog pumps: WM_CLOSE is
	// dispatched by the nested modal loop, the window is destroyed and
	// its scope closes on the host thread.
	realPostMessageW.Call(wA.Handle().Hwnd(), realWMClose, 0, 0)
	recvWithinReal(t, 10*time.Second, "the owner window's close during the modal pump", aClosed)

	// End the dialog: its result must be discarded, not delivered.
	handle.DismissDialog()
	waitForReal(t, 10*time.Second, "the prompt request to complete as discarded", func() bool {
		return handle.Discarded()
	})
	if handle.Delivered() || handle.Pending() {
		t.Fatalf("handle state: delivered=%v pending=%v, want discarded", handle.Delivered(), handle.Pending())
	}
	select {
	case outcome := <-outcomes:
		t.Fatalf("late result was delivered after owner closure: %+v", outcome)
	case <-time.After(200 * time.Millisecond):
	}
	if deliveries != 0 {
		t.Fatalf("delivery ran %d times, want none", deliveries)
	}
}

// TestRealDialogProgrammaticDismissDeliversCancelled drives the REAL
// Win32 open dialog (the default driver, no override) and closes it
// programmatically through the registered dismissal: the dialog's modal
// loop dispatches the dismissal message, Show reports cancellation and
// the cancelled outcome delivers — deterministically, without any user
// interaction. This exercises the full real path (CoCreateInstance,
// SetOptions, SetOkButtonLabel, Show) of file_open_dialog.
func TestRealDialogProgrammaticDismissDeliversCancelled(t *testing.T) {
	h := newRealHost(t)
	app := newRealApp(t, h)
	w := openShownReal(t, app, "dialogspec real dismiss", nil)

	outcomes := make(chan gpui.PathsOutcome, 1)
	handle := app.PromptForPaths(w, nil, gpui.PathPromptOptions{Files: true, Prompt: "gpui-go probe"}, func(outcome gpui.PathsOutcome, cx *gpui.App) {
		outcomes <- outcome
	})

	// Wait for the real dialog to come up (the dismissal registers
	// immediately before Show), then close it programmatically.
	waitForReal(t, 30*time.Second, "the real dialog to register its dismissal", func() bool {
		return handle.Dismissible()
	})
	handle.DismissDialog()

	outcome := recvWithinReal(t, 30*time.Second, "the dismissed real dialog's outcome", outcomes)
	if outcome.Err != nil {
		t.Fatalf("the real dialog returned an error: %v", outcome.Err)
	}
	if !outcome.Cancelled {
		t.Fatalf("outcome = %+v, want the cancelled result after the programmatic close", outcome)
	}
	if !handle.Delivered() || handle.Discarded() {
		t.Fatalf("handle state: delivered=%v discarded=%v", handle.Delivered(), handle.Discarded())
	}

	// The app and host stay healthy after the real COM dialog round.
	w.Close()
	waitForReal(t, 10*time.Second, "the window to close after the dialog", func() bool { return !w.Alive() })
}

// TestRealSaveDialogProgrammaticDismissDeliversCancelled drives the REAL
// Win32 save dialog with a real directory and suggested name (the
// canonicalize + SHCreateItemFromParsingName + SetFolder + SetFileName
// + SetFileTypes configuration of file_save_dialog) and closes it
// programmatically — no user interaction, the cancelled outcome
// delivers.
func TestRealSaveDialogProgrammaticDismissDeliversCancelled(t *testing.T) {
	h := newRealHost(t)
	app := newRealApp(t, h)
	w := openShownReal(t, app, "dialogspec real save dismiss", nil)
	dir := t.TempDir()

	outcomes := make(chan gpui.PathOutcome, 1)
	handle := app.PromptForNewPath(w, nil, dir, "probe.txt", func(outcome gpui.PathOutcome, cx *gpui.App) {
		outcomes <- outcome
	})

	waitForReal(t, 30*time.Second, "the real save dialog to register its dismissal", func() bool {
		return handle.Dismissible()
	})
	handle.DismissDialog()

	outcome := recvWithinReal(t, 30*time.Second, "the dismissed save dialog's outcome", outcomes)
	if outcome.Err != nil {
		t.Fatalf("the real save dialog returned an error: %v", outcome.Err)
	}
	if !outcome.Cancelled {
		t.Fatalf("outcome = %+v, want the cancelled result after the programmatic close", outcome)
	}
	if !handle.Delivered() || handle.Discarded() {
		t.Fatalf("handle state: delivered=%v discarded=%v", handle.Delivered(), handle.Discarded())
	}

	w.Close()
	waitForReal(t, 10*time.Second, "the window to close after the save dialog", func() bool { return !w.Alive() })
}

// ---------------------------------------------------------------------------
// Real credentials through the scoped task seam
// ---------------------------------------------------------------------------

// TestRealCredentialsRoundTripThroughScopedTasks round-trips REAL
// Windows Credential Manager entries through App.WriteCredentials/
// ReadCredentials/DeleteCredentials (host-attached app: the real store
// default), with caller-supplied temporary entries only, explicit
// absence results, the max-blob-size rejection, and cleanup of exactly
// the entries created. Values are never printed (only lengths and
// booleans are compared).
func TestRealCredentialsRoundTripThroughScopedTasks(t *testing.T) {
	h := newRealHost(t)
	app := newRealApp(t, h)
	url := newTestCredentialURL(t)
	username := "gpui-go dialogspec 测试 ✓"
	password := []byte{0x00, 0x01, 's', 'e', 'c', 'r', 'e', 't', 0xff, 0x00}

	// Cleanup exactly this test's entry (before the host stops: cleanup
	// runs LIFO and the host cleanup was registered first).
	t.Cleanup(func() {
		ch := make(chan error, 1)
		app.DeleteCredentials(nil, url, deliverErr(ch))
		select {
		case <-ch:
		case <-time.After(10 * time.Second):
			t.Errorf("cleanup delete for the test entry timed out")
		}
	})

	// Absence before the write (an explicit absence result, not an error).
	ch := make(chan gpui.CredentialsResult, 1)
	app.ReadCredentials(nil, url, deliverResult(ch))
	result := recvWithinReal(t, 10*time.Second, "the initial absence read", ch)
	if result.Err != nil || result.Found {
		t.Fatalf("initial read = %+v, want the absence result", result)
	}

	// Write, read back, compare.
	wch := make(chan error, 1)
	app.WriteCredentials(nil, url, username, password, deliverErr(wch))
	if err := recvWithinReal(t, 10*time.Second, "the write", wch); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	ch = make(chan gpui.CredentialsResult, 1)
	app.ReadCredentials(nil, url, deliverResult(ch))
	result = recvWithinReal(t, 10*time.Second, "the read after write", ch)
	if result.Err != nil || !result.Found {
		t.Fatalf("read after write = %+v, want found with no error", result)
	}
	if result.Username != username {
		t.Fatalf("username round trip mismatch (lengths %d vs %d)", len(result.Username), len(username))
	}
	if !bytes.Equal(result.Password, password) {
		t.Fatalf("blob round trip mismatch (%d vs %d bytes)", len(result.Password), len(password))
	}

	// Delete, then absence again.
	dch := make(chan error, 1)
	app.DeleteCredentials(nil, url, deliverErr(dch))
	if err := recvWithinReal(t, 10*time.Second, "the delete", dch); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	ch = make(chan gpui.CredentialsResult, 1)
	app.ReadCredentials(nil, url, deliverResult(ch))
	result = recvWithinReal(t, 10*time.Second, "the read after delete", ch)
	if result.Err != nil || result.Found {
		t.Fatalf("read after delete = %+v, want the absence result", result)
	}

	// Deleting the now-absent entry is an explicit error (the reference
	// propagates CredDeleteW failures).
	dch = make(chan error, 1)
	app.DeleteCredentials(nil, url, deliverErr(dch))
	if err := recvWithinReal(t, 10*time.Second, "the delete of the absent entry", dch); err == nil {
		t.Fatal("deleting an absent credential must deliver an error")
	}
}

// TestRealCredentialsBlobSizeRejectedThroughApp checks the max-blob-size
// rejection through the app seam with the real store: a 2561-byte blob
// fails with the reference's clear message and no entry is created.
func TestRealCredentialsBlobSizeRejectedThroughApp(t *testing.T) {
	h := newRealHost(t)
	app := newRealApp(t, h)
	url := newTestCredentialURL(t)
	t.Cleanup(func() {
		ch := make(chan error, 1)
		app.DeleteCredentials(nil, url, deliverErr(ch))
		select {
		case <-ch:
		case <-time.After(10 * time.Second):
			t.Errorf("cleanup delete for the test entry timed out")
		}
	})

	bigBlob := bytes.Repeat([]byte{0x33}, 2561)
	wch := make(chan error, 1)
	app.WriteCredentials(nil, url, "blob-test", bigBlob, deliverErr(wch))
	err := recvWithinReal(t, 10*time.Second, "the oversized write", wch)
	if err == nil {
		t.Fatal("the oversized write must fail")
	}
	if want := "exceeds the Windows Credential Manager limit of 2560 bytes"; !bytes.Contains([]byte(err.Error()), []byte(want)) {
		t.Fatalf("error = %q, want it to contain %q", err.Error(), want)
	}

	// The rejected write must not have created an entry.
	ch := make(chan gpui.CredentialsResult, 1)
	app.ReadCredentials(nil, url, deliverResult(ch))
	result := recvWithinReal(t, 10*time.Second, "the absence read after the rejection", ch)
	if result.Err != nil || result.Found {
		t.Fatalf("read after rejection = %+v, want the absence result", result)
	}
}
