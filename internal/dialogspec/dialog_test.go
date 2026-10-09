// Package dialogspec holds ticket25's dialogs and credentials corpus:
// deterministic headless prompt/credential semantics (the reference
// TestPlatform's pending prompt queues and simulated responses) in this
// file, plus real Win32 evidence (COM probe, nested modal pumping, real
// dialogs closed programmatically, real Credential Manager round trips)
// in dialog_real_test.go on windows.
//
// Environment expectation: the deterministic tests are hostless
// (TestApp) and never touch the OS dialog or credential machinery; the
// hostless default credential store is the in-memory one. The real
// tests create real windows in the user's interactive session and FAIL
// (never skip) when that fails, like internal/winhostspec.
package dialogspec

import (
	"errors"
	"strings"
	"testing"

	"gpui-go/gpui"
)

// errBoom is the injected platform failure.
var errBoom = errors.New("boom")

// pathDelivery records one paths-prompt delivery.
type pathDelivery struct {
	outcome gpui.PathsOutcome
	count   int
}

func newPathsRecorder() (*pathDelivery, func(gpui.PathsOutcome, *gpui.App)) {
	d := &pathDelivery{}
	return d, func(outcome gpui.PathsOutcome, cx *gpui.App) {
		if cx == nil {
			panic("deliver callback received a nil App context")
		}
		d.outcome = outcome
		d.count++
	}
}

// newDelivery records one new-path-prompt delivery.
type newPathDelivery struct {
	outcome gpui.PathOutcome
	count   int
}

func newNewPathRecorder() (*newPathDelivery, func(gpui.PathOutcome, *gpui.App)) {
	d := &newPathDelivery{}
	return d, func(outcome gpui.PathOutcome, cx *gpui.App) {
		if cx == nil {
			panic("deliver callback received a nil App context")
		}
		d.outcome = outcome
		d.count++
	}
}

// ---------------------------------------------------------------------------
// Path prompts: queue, options, delivery, FIFO
// ---------------------------------------------------------------------------

// TestPromptForPathsQueueDeliveryAndOptions checks the pending queue
// (did_prompt_for_paths), the recorded options round trip, and the
// delivered selection — the TestAppContext prompt semantics
// (crates/gpui/src/app/test_context.rs:1341-1351 and
// crates/gpui/src/platform/test/platform.rs:468-480).
func TestPromptForPathsQueueDeliveryAndOptions(t *testing.T) {
	ta := gpui.NewTestApp()
	app := ta.App()

	if app.DidPromptForPaths() {
		t.Fatal("a fresh app must not have pending path prompts")
	}

	options := gpui.PathPromptOptions{Files: true, Multiple: false, Prompt: "Open a file"}
	delivery, deliver := newPathsRecorder()
	handle := app.PromptForPaths(nil, nil, options, deliver)

	if !app.DidPromptForPaths() {
		t.Fatal("the prompt must be pending until answered")
	}
	if !handle.Pending() {
		t.Fatal("the prompt handle must report pending before the response")
	}

	var recorded gpui.PathPromptOptions
	app.SimulatePathPromptResponse(func(opts gpui.PathPromptOptions) ([]string, bool) {
		recorded = opts
		return []string{`C:\tmp\a.txt`}, false
	})

	if recorded != options {
		t.Fatalf("simulated selector recorded %+v, want %+v", recorded, options)
	}
	if delivery.count != 1 {
		t.Fatalf("delivery ran %d times, want exactly once", delivery.count)
	}
	if len(delivery.outcome.Paths) != 1 || delivery.outcome.Paths[0] != `C:\tmp\a.txt` {
		t.Fatalf("delivered outcome = %+v, want the single selected path", delivery.outcome)
	}
	if delivery.outcome.Cancelled || delivery.outcome.Err != nil {
		t.Fatalf("delivered outcome = %+v, want a clean selection", delivery.outcome)
	}
	if app.DidPromptForPaths() {
		t.Fatal("the prompt must leave the queue once answered")
	}
	if !handle.Delivered() || handle.Discarded() || handle.Pending() {
		t.Fatalf("handle state after delivery: delivered=%v discarded=%v pending=%v",
			handle.Delivered(), handle.Discarded(), handle.Pending())
	}
}

// TestPromptForPathsCancelledOutcome checks the cancelled (Ok(None))
// delivery.
func TestPromptForPathsCancelledOutcome(t *testing.T) {
	ta := gpui.NewTestApp()
	app := ta.App()

	delivery, deliver := newPathsRecorder()
	app.PromptForPaths(nil, nil, gpui.PathPromptOptions{Files: true}, deliver)

	app.SimulatePathPromptResponse(func(gpui.PathPromptOptions) ([]string, bool) {
		return nil, true
	})

	if delivery.count != 1 {
		t.Fatalf("delivery ran %d times, want exactly once", delivery.count)
	}
	if !delivery.outcome.Cancelled || len(delivery.outcome.Paths) != 0 || delivery.outcome.Err != nil {
		t.Fatalf("delivered outcome = %+v, want the cancelled result", delivery.outcome)
	}
}

// TestPromptMultipleSelectionValidation checks the reference's panic
// when a single-selection prompt is answered with several paths
// (crates/gpui/src/platform/test/platform.rs:186-195).
func TestPromptMultipleSelectionValidation(t *testing.T) {
	ta := gpui.NewTestApp()
	app := ta.App()

	app.PromptForPaths(nil, nil, gpui.PathPromptOptions{Files: true, Multiple: false}, func(gpui.PathsOutcome, *gpui.App) {})

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("answering a single-selection prompt with two paths must panic")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "does not allow multiple selection") {
			t.Fatalf("panic = %v, want the multiple-selection diagnostic", r)
		}
	}()
	app.SimulatePathPromptResponse(func(gpui.PathPromptOptions) ([]string, bool) {
		return []string{`C:\a`, `C:\b`}, false
	})
}

// TestPromptForPathsFIFOOrdering checks that responses answer the
// oldest pending prompt first (the reference deque's pop_front).
func TestPromptForPathsFIFOOrdering(t *testing.T) {
	ta := gpui.NewTestApp()
	app := ta.App()

	first, firstDeliver := newPathsRecorder()
	second, secondDeliver := newPathsRecorder()
	app.PromptForPaths(nil, nil, gpui.PathPromptOptions{Files: true}, firstDeliver)
	app.PromptForPaths(nil, nil, gpui.PathPromptOptions{Files: true}, secondDeliver)

	app.SimulatePathPromptResponse(func(gpui.PathPromptOptions) ([]string, bool) {
		return []string{`C:\first`}, false
	})
	if first.count != 1 || second.count != 0 {
		t.Fatalf("first delivery count=%d second=%d, want 1/0 (FIFO)", first.count, second.count)
	}
	if len(first.outcome.Paths) != 1 || first.outcome.Paths[0] != `C:\first` {
		t.Fatalf("first outcome = %+v", first.outcome)
	}

	app.SimulatePathPromptResponse(func(gpui.PathPromptOptions) ([]string, bool) {
		return []string{`C:\second`}, false
	})
	if first.count != 1 || second.count != 1 {
		t.Fatalf("first delivery count=%d second=%d, want 1/1", first.count, second.count)
	}
	if len(second.outcome.Paths) != 1 || second.outcome.Paths[0] != `C:\second` {
		t.Fatalf("second outcome = %+v", second.outcome)
	}
}

// TestSimulateWithoutPendingPromptPanics mirrors the reference's
// expect("no pending paths prompt").
func TestSimulateWithoutPendingPromptPanics(t *testing.T) {
	ta := gpui.NewTestApp()
	app := ta.App()

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("simulating a response without a pending prompt must panic")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "no pending paths prompt") {
			t.Fatalf("panic = %v, want the no-pending-prompt diagnostic", r)
		}
	}()
	app.SimulatePathPromptResponse(func(gpui.PathPromptOptions) ([]string, bool) {
		return nil, false
	})
}

// TestPromptCancelDiscardsQueuedResult checks Cancel as the reference's
// dropped receiver: the eventual response is discarded, never delivered.
func TestPromptCancelDiscardsQueuedResult(t *testing.T) {
	ta := gpui.NewTestApp()
	app := ta.App()

	delivery, deliver := newPathsRecorder()
	handle := app.PromptForPaths(nil, nil, gpui.PathPromptOptions{Files: true}, deliver)
	handle.Cancel()

	app.SimulatePathPromptResponse(func(gpui.PathPromptOptions) ([]string, bool) {
		return []string{`C:\late`}, false
	})

	if delivery.count != 0 {
		t.Fatalf("delivery ran %d times after Cancel, want none", delivery.count)
	}
	if !handle.Discarded() || handle.Delivered() {
		t.Fatalf("handle state after cancelled completion: delivered=%v discarded=%v",
			handle.Delivered(), handle.Discarded())
	}
	if handle.Pending() {
		t.Fatal("the request must be completed after the simulated response")
	}
}

// TestPromptScopeCloseDiscardsLateResult is the deterministic
// late-result trace: the receiving scope closes before the response
// arrives, and the eventual result is discarded (the runtime ownership
// contract's generation-gated delivery).
func TestPromptScopeCloseDiscardsLateResult(t *testing.T) {
	ta := gpui.NewTestApp()
	app := ta.App()
	target := ta.RootScope().Child()

	delivery, deliver := newPathsRecorder()
	handle := app.PromptForPaths(nil, target, gpui.PathPromptOptions{Files: true}, deliver)

	target.Close()
	if target.State() == gpui.ScopeOpen {
		t.Fatal("the target scope must be closed")
	}

	app.SimulatePathPromptResponse(func(gpui.PathPromptOptions) ([]string, bool) {
		return []string{`C:\late`}, false
	})

	if delivery.count != 0 {
		t.Fatalf("delivery ran %d times after scope closure, want none", delivery.count)
	}
	if !handle.Discarded() || handle.Delivered() {
		t.Fatalf("handle state after scope-closed completion: delivered=%v discarded=%v",
			handle.Delivered(), handle.Discarded())
	}
}

// TestPromptForNewPathQueueDelivery checks the save prompt queue: the
// recorded directory and suggested name, delivery and cancellation.
func TestPromptForNewPathQueueDelivery(t *testing.T) {
	ta := gpui.NewTestApp()
	app := ta.App()

	if app.DidPromptForNewPath() {
		t.Fatal("a fresh app must not have pending new-path prompts")
	}

	delivery, deliver := newNewPathRecorder()
	app.PromptForNewPath(nil, nil, `C:\docs`, "report.txt", deliver)

	if !app.DidPromptForNewPath() {
		t.Fatal("the new-path prompt must be pending until answered")
	}

	var directory string
	app.SimulateNewPathSelection(func(dir string) (string, bool) {
		directory = dir
		return `C:\docs\report.txt`, false
	})

	if directory != `C:\docs` {
		t.Fatalf("simulated selector recorded directory %q, want C:\\docs", directory)
	}
	if delivery.count != 1 || delivery.outcome.Path != `C:\docs\report.txt` {
		t.Fatalf("delivery count=%d outcome=%+v, want the selected path once", delivery.count, delivery.outcome)
	}
	if delivery.outcome.Cancelled || delivery.outcome.Err != nil {
		t.Fatalf("delivered outcome = %+v, want a clean selection", delivery.outcome)
	}
}

// TestPromptForNewPathCancelledAndPanic mirrors the cancelled outcome
// and the no-pending-prompt panic for the save prompt.
func TestPromptForNewPathCancelledAndPanic(t *testing.T) {
	ta := gpui.NewTestApp()
	app := ta.App()

	delivery, deliver := newNewPathRecorder()
	app.PromptForNewPath(nil, nil, `C:\docs`, "a.txt", deliver)
	app.SimulateNewPathSelection(func(string) (string, bool) {
		return "", true
	})
	if delivery.count != 1 || !delivery.outcome.Cancelled {
		t.Fatalf("outcome = %+v, want the cancelled result", delivery.outcome)
	}

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("simulating a new-path selection without a pending prompt must panic")
		}
	}()
	app.SimulateNewPathSelection(func(string) (string, bool) { return "", false })
}

// TestCanSelectMixedFilesAndDirsTestPlatform checks the hostless answer
// mirrors the reference test platform (true).
func TestCanSelectMixedFilesAndDirsTestPlatform(t *testing.T) {
	ta := gpui.NewTestApp()
	if !ta.App().CanSelectMixedFilesAndDirs() {
		t.Fatal("the hostless (test platform) answer must be true, like TestPlatform")
	}
}

// TestPromptAfterShutdownRejected checks the shutdown rejection: new
// prompt work panics after App.Shutdown, like new scoped-task spawns.
func TestPromptAfterShutdownRejected(t *testing.T) {
	ta := gpui.NewTestApp()
	app := ta.App()
	app.Shutdown()

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("prompting after shutdown must panic")
		}
	}()
	app.PromptForPaths(nil, nil, gpui.PathPromptOptions{Files: true}, func(gpui.PathsOutcome, *gpui.App) {})
}

// ---------------------------------------------------------------------------
// Credentials: scoped delivery, round trip, absence, errors, discard
// ---------------------------------------------------------------------------

// TestCredentialsRoundTripThroughScopedTasks runs write/read/delete
// through the scoped task seam with the hostless memory store,
// delivering results as foreground updates.
func TestCredentialsRoundTripThroughScopedTasks(t *testing.T) {
	ta := gpui.NewTestApp()
	app := ta.App()

	writeDone := make(chan error, 1)
	task := app.WriteCredentials(nil, "https://gpui-go.test/cred", "user", []byte("secret"), func(err error, cx *gpui.App) {
		if cx == nil {
			t.Error("deliver callback received a nil App context")
		}
		writeDone <- err
	})
	ta.RunUntilParked()
	if !task.Done() {
		t.Fatal("the write task must complete during RunUntilParked")
	}
	if err := <-writeDone; err != nil {
		t.Fatalf("write delivered error: %v", err)
	}

	readDone := make(chan gpui.CredentialsResult, 1)
	app.ReadCredentials(nil, "https://gpui-go.test/cred", func(result gpui.CredentialsResult, cx *gpui.App) {
		readDone <- result
	})
	ta.RunUntilParked()
	result := <-readDone
	if result.Err != nil || !result.Found {
		t.Fatalf("read result = %+v, want found with no error", result)
	}
	if result.Username != "user" || string(result.Password) != "secret" {
		t.Fatalf("read result = %+v, want the written credential", result)
	}

	deleteDone := make(chan error, 1)
	app.DeleteCredentials(nil, "https://gpui-go.test/cred", func(err error, cx *gpui.App) {
		deleteDone <- err
	})
	ta.RunUntilParked()
	if err := <-deleteDone; err != nil {
		t.Fatalf("delete delivered error: %v", err)
	}

	// Absence after delete.
	readDone = make(chan gpui.CredentialsResult, 1)
	app.ReadCredentials(nil, "https://gpui-go.test/cred", func(result gpui.CredentialsResult, cx *gpui.App) {
		readDone <- result
	})
	ta.RunUntilParked()
	result = <-readDone
	if result.Err != nil || result.Found {
		t.Fatalf("read after delete = %+v, want the absence result", result)
	}
}

// TestReadCredentialsAbsenceBeforeWrite checks the absence result
// before anything was written.
func TestReadCredentialsAbsenceBeforeWrite(t *testing.T) {
	ta := gpui.NewTestApp()
	app := ta.App()

	readDone := make(chan gpui.CredentialsResult, 1)
	app.ReadCredentials(nil, "https://gpui-go.test/absent", func(result gpui.CredentialsResult, cx *gpui.App) {
		readDone <- result
	})
	ta.RunUntilParked()
	result := <-readDone
	if result.Err != nil || result.Found || result.Username != "" || result.Password != nil {
		t.Fatalf("absence read = %+v, want Found=false with nil error", result)
	}
}

// TestDeleteAbsentCredentialsIsError checks the propagated delete
// failure for an absent entry (the memory store mirrors the platform's
// behavior).
func TestDeleteAbsentCredentialsIsError(t *testing.T) {
	ta := gpui.NewTestApp()
	app := ta.App()

	deleteDone := make(chan error, 1)
	app.DeleteCredentials(nil, "https://gpui-go.test/absent", func(err error, cx *gpui.App) {
		deleteDone <- err
	})
	ta.RunUntilParked()
	if err := <-deleteDone; err == nil {
		t.Fatal("deleting an absent credential must deliver an error")
	}
}

// failingStore is a CredentialStore that fails every operation.
type failingStore struct{}

func (failingStore) Write(url, username string, password []byte) error {
	return errBoom
}

func (failingStore) Read(url string) (string, []byte, bool, error) {
	return "", nil, false, errBoom
}

func (failingStore) Delete(url string) error { return errBoom }

// TestCredentialErrorPropagation checks that platform failures deliver
// through the scoped task seam instead of swallowing.
func TestCredentialErrorPropagation(t *testing.T) {
	ta := gpui.NewTestApp()
	app := ta.App()
	app.SetCredentialStore(failingStore{})

	writeDone := make(chan error, 1)
	app.WriteCredentials(nil, "https://gpui-go.test/boom", "user", nil, func(err error, cx *gpui.App) {
		writeDone <- err
	})
	ta.RunUntilParked()
	if err := <-writeDone; err != errBoom {
		t.Fatalf("write delivered %v, want the platform failure", err)
	}

	readDone := make(chan gpui.CredentialsResult, 1)
	app.ReadCredentials(nil, "https://gpui-go.test/boom", func(result gpui.CredentialsResult, cx *gpui.App) {
		readDone <- result
	})
	ta.RunUntilParked()
	if result := <-readDone; result.Err != errBoom {
		t.Fatalf("read delivered %+v, want the platform failure", result)
	}
}

// TestCredentialScopeCloseDiscardsDelivery closes the receiving scope
// before the task is driven: the task is cancelled (never started),
// nothing is delivered, and the store never sees the write — the
// scoped-task discard trace.
func TestCredentialScopeCloseDiscardsDelivery(t *testing.T) {
	ta := gpui.NewTestApp()
	app := ta.App()

	deliveries := 0
	target := ta.RootScope().Child()
	task := app.WriteCredentials(target, "https://gpui-go.test/discarded", "user", []byte("x"), func(err error, cx *gpui.App) {
		deliveries++
	})
	target.Close()

	ta.RunUntilParked()

	if deliveries != 0 {
		t.Fatalf("delivery ran %d times after scope closure, want none", deliveries)
	}
	if !task.Done() {
		t.Fatal("the cancelled task must retire during RunUntilParked")
	}
	if !task.Cancelled() {
		t.Fatal("the task must report cancelled")
	}

	// The worker never ran: the entry must be absent.
	readDone := make(chan gpui.CredentialsResult, 1)
	app.ReadCredentials(nil, "https://gpui-go.test/discarded", func(result gpui.CredentialsResult, cx *gpui.App) {
		readDone <- result
	})
	ta.RunUntilParked()
	if result := <-readDone; result.Found {
		t.Fatalf("the cancelled write must not have stored anything, got %+v", result)
	}
}

// TestCredentialTaskAwait composes credential tasks like the reference
// composes the returned Task: a foreground task awaits the read result.
func TestCredentialTaskAwait(t *testing.T) {
	ta := gpui.NewTestApp()
	app := ta.App()

	app.WriteCredentials(nil, "https://gpui-go.test/await", "user", []byte("pw"), func(err error, cx *gpui.App) {
		if err != nil {
			t.Errorf("write delivered error: %v", err)
		}
	})

	readDone := make(chan gpui.CredentialsResult, 1)
	app.Spawn(func(cx *gpui.AsyncApp) error {
		task := app.ReadCredentials(nil, "https://gpui-go.test/await", func(gpui.CredentialsResult, *gpui.App) {})
		readDone <- cx.Await(task)
		return nil
	})
	ta.RunUntilParked()

	result := <-readDone
	if result.Err != nil || !result.Found || result.Username != "user" {
		t.Fatalf("awaited read = %+v, want the written credential", result)
	}
}
