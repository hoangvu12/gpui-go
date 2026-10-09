package gpui

// This file is ticket25's platform-neutral dialogs and credentials layer,
// ported from the pinned CE 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a:
//
//   - crates/gpui/src/platform.rs (platform.rs:215-224) — the
//     Platform::prompt_for_paths / prompt_for_new_path /
//     can_select_mixed_files_and_dirs trait surface and PathPromptOptions
//     (platform.rs:3110-3122).
//   - crates/gpui/src/app.rs (app.rs:1567-1688) — the App-level
//     write_credentials / read_credentials / delete_credentials /
//     prompt_for_paths / prompt_for_new_path forwards.
//   - crates/gpui_windows/src/platform.rs (platform.rs:601-632,
//     829-926) — the Windows routing: prompts spawn the dialog on the
//     FOREGROUND executor with the owner window captured at call time
//     (find_current_active_window), credentials run as foreground tasks
//     (Go adaptation: scoped delivery tasks, see below).
//   - crates/gpui/src/platform/test/platform.rs (platform.rs:468-526,
//     163-208) — the deterministic test-platform semantics this port's
//     hostless apps mirror: pending prompt queues answered through
//     SimulatePathPromptResponse / SimulateNewPathSelection with FIFO
//     popping and the multi-select validation panic.
//
// Threading contract (windows platform contract, "Thread, loop and window
// lifetime"): dialogs are potentially-pumping operations that start after
// the current update unwinds. On a real application the driver runs in a
// posted host closure on the foreground (STA) thread, where the modal
// dialog's nested message loop keeps pumping wake messages and window
// messages, so other windows stay responsive. The result is delivered
// through the same delivery gate ticket04 built for scoped tasks
// (task_ownership.go deliverySpec): the receiving scope's close
// generation discards late results after owner closure.
//
// Go adaptation (documented deviation): the reference stores the prompt
// and credential platform seams on the Platform/App structs; this slice
// may only add new files, so the per-app dialog runtime lives in a
// package-level registry keyed by *App.

import (
	"fmt"
	"sync"
)

// ---------------------------------------------------------------------------
// Prompt options and outcomes (crates/gpui/src/platform.rs)
// ---------------------------------------------------------------------------

// PathPromptOptions mirrors PathPromptOptions
// (crates/gpui/src/platform.rs:3110). The reference has no Default
// derive; callers construct it fully, and the zero value here mirrors an
// explicit all-false construction.
type PathPromptOptions struct {
	// Files reports whether files may be selected.
	Files bool
	// Directories reports whether directories may be selected.
	Directories bool
	// Multiple reports whether multiple entries may be selected.
	Multiple bool
	// Prompt is the OK-button label shown when selecting a path (the
	// reference's Option<SharedString>; empty means None).
	Prompt string
}

// PathsOutcome is the open-dialog result, the reference trichotomy
// Result<Option<Vec<PathBuf>>>: Err set (failure), Cancelled (Ok(None)),
// or Paths (Ok(Some)).
type PathsOutcome struct {
	// Paths is the selected list (the reference's Some(Vec<PathBuf>)).
	Paths []string
	// Cancelled reports the user-cancelled/Ok(None) result.
	Cancelled bool
	// Err reports the failure result.
	Err error
}

// PathOutcome is the save-dialog result, the reference trichotomy
// Result<Option<PathBuf>>.
type PathOutcome struct {
	// Path is the selected path (the reference's Some(PathBuf)).
	Path string
	// Cancelled reports the user-cancelled/Ok(None) result.
	Cancelled bool
	// Err reports the failure result.
	Err error
}

// CredentialsResult is the read-credentials result, the reference
// Task<Result<Option<(String, Vec<u8>)>>> trichotomy: Err (failure),
// !Found (Ok(None)), or Username/Password (Ok(Some)).
type CredentialsResult struct {
	// Err reports the failure result.
	Err error
	// Found reports the Ok(Some) case; false with nil Err is Ok(None).
	Found bool
	// Username is the stored username when Found.
	Username string
	// Password is the stored blob when Found.
	Password []byte
}

// ---------------------------------------------------------------------------
// Platform seams: dialog driver and credential store
// ---------------------------------------------------------------------------

// DialogDismissal programmatically closes one shown dialog. It must be
// safe to call from ANY goroutine: implementations marshal to the
// dialog's own thread themselves (the Win32 implementation posts a
// private message that the dialog's nested modal loop dispatches, the
// way the reference's platform window handles its private posted
// messages). The reference has no App-level dismissal API — this seam
// exists so tests and cancellation can close a real dialog
// deterministically without user input. Implementations must be
// idempotent.
type DialogDismissal func()

// DialogDriver is the platform dialog seam behind
// Platform::prompt_for_paths / prompt_for_new_path
// (crates/gpui/src/platform.rs:215-223). Its methods run on the
// foreground (STA) thread of a real application. Before showing, the
// driver may register a dismissal through dismiss so a pending prompt
// can be closed programmatically.
type DialogDriver interface {
	// PromptForPaths shows the open dialog. The owner HWND is 0 when the
	// prompt has no owner window.
	PromptForPaths(owner uintptr, options PathPromptOptions, dismiss func(d DialogDismissal)) PathsOutcome
	// PromptForNewPath shows the save dialog. An empty directory or
	// empty suggestedName mirrors the reference's empty path / None.
	PromptForNewPath(owner uintptr, directory, suggestedName string, dismiss func(d DialogDismissal)) PathOutcome
	// CanSelectMixedFilesAndDirs mirrors
	// Platform::can_select_mixed_files_and_dirs.
	CanSelectMixedFilesAndDirs() bool
}

// CredentialStore is the platform keychain seam behind
// Platform::write_credentials / read_credentials / delete_credentials
// (crates/gpui/src/platform.rs:359-361).
type CredentialStore interface {
	// Write stores the credential, overwriting an existing entry for the
	// same url (CredWriteW semantics).
	Write(url, username string, password []byte) error
	// Read returns the stored credential. found=false with a nil error is
	// the absence result (the reference's ERROR_NOT_FOUND -> Ok(None)).
	Read(url string) (username string, password []byte, found bool, err error)
	// Delete removes the credential; deleting an absent entry is an
	// explicit error, as the reference's propagated CredDeleteW failure.
	Delete(url string) error
}

// MemoryCredentialStore is a functional in-memory CredentialStore: the
// hostless/test-application default, standing in for the reference
// TestPlatform's trivial keychain with real round-trip semantics. It
// applies no blob-size limit (the Windows Credential Manager limit is a
// platform property; see the real store).
type MemoryCredentialStore struct {
	mu      sync.Mutex
	entries map[string]memoryCredential
}

type memoryCredential struct {
	username string
	password []byte
}

// NewMemoryCredentialStore creates an empty memory store.
func NewMemoryCredentialStore() *MemoryCredentialStore {
	return &MemoryCredentialStore{entries: make(map[string]memoryCredential)}
}

// Write stores the entry.
func (m *MemoryCredentialStore) Write(url, username string, password []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries[url] = memoryCredential{username: username, password: append([]byte(nil), password...)}
	return nil
}

// Read returns the entry or the absence result.
func (m *MemoryCredentialStore) Read(url string) (string, []byte, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.entries[url]
	if !ok {
		return "", nil, false, nil
	}
	return entry.username, append([]byte(nil), entry.password...), true, nil
}

// Delete removes the entry; an absent entry is an explicit error,
// mirroring the platform's propagated delete failure.
func (m *MemoryCredentialStore) Delete(url string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.entries[url]; !ok {
		return fmt.Errorf("no credential stored for %s", url)
	}
	delete(m.entries, url)
	return nil
}

// ---------------------------------------------------------------------------
// The per-app dialog runtime
// ---------------------------------------------------------------------------

// dialogRuntime holds one application's dialog/credential seam state. It
// is reached through a package-level registry because the App struct
// belongs to other tickets' files (see the file comment).
type dialogRuntime struct {
	mu sync.Mutex
	// driverSet/storeSet are explicit overrides (tests and drivers).
	driverSet DialogDriver
	storeSet  CredentialStore
	// memoryStore is the lazily-created hostless default keychain.
	memoryStore *MemoryCredentialStore
	// pendingPaths/pendingNewPath are the hostless pending prompt queues
	// (the reference TestPlatform's prompts.paths / prompts.new_path
	// deques, FIFO).
	pendingPaths   []*promptRequest
	pendingNewPath []*promptRequest
}

var (
	dialogRegistryMu sync.Mutex
	dialogRegistry   = map[*App]*dialogRuntime{}
)

// dialogRuntimeFor returns (creating once) the app's dialog runtime.
func dialogRuntimeFor(a *App) *dialogRuntime {
	dialogRegistryMu.Lock()
	defer dialogRegistryMu.Unlock()
	rt := dialogRegistry[a]
	if rt == nil {
		rt = &dialogRuntime{}
		dialogRegistry[a] = rt
	}
	return rt
}

// driver resolves the effective dialog driver: an explicit override, the
// real platform driver for host-attached (real) applications, or the
// pending-prompt queue semantics for hostless test applications.
func (rt *dialogRuntime) driver(a *App) DialogDriver {
	rt.mu.Lock()
	set := rt.driverSet
	rt.mu.Unlock()
	if set != nil {
		return set
	}
	if a.host != nil {
		return defaultDialogDriver()
	}
	return queueDialogDriver{}
}

// credentialStore resolves the effective keychain: an explicit override,
// the real Windows Credential Manager store for host-attached (real)
// applications, or the memory store for hostless test applications.
func (rt *dialogRuntime) credentialStore(a *App) CredentialStore {
	rt.mu.Lock()
	set := rt.storeSet
	store := rt.memoryStore
	rt.mu.Unlock()
	if set != nil {
		return set
	}
	if a.host != nil {
		return defaultCredentialStore()
	}
	if store == nil {
		store = NewMemoryCredentialStore()
		rt.mu.Lock()
		rt.memoryStore = store
		rt.mu.Unlock()
	}
	return store
}

// SetDialogDriver overrides the platform dialog driver. Explicit drivers
// only take effect on host-attached applications; hostless applications
// always use the pending-prompt queue semantics (the TestPlatform
// analogue).
func (a *App) SetDialogDriver(driver DialogDriver) {
	rt := dialogRuntimeFor(a)
	rt.mu.Lock()
	rt.driverSet = driver
	rt.mu.Unlock()
}

// SetCredentialStore overrides the platform keychain.
func (a *App) SetCredentialStore(store CredentialStore) {
	rt := dialogRuntimeFor(a)
	rt.mu.Lock()
	rt.storeSet = store
	rt.mu.Unlock()
}

// CanSelectMixedFilesAndDirs mirrors
// Platform::can_select_mixed_files_and_dirs (crates/gpui/src/platform.rs:224):
// the Windows answer is false ("The FOS_PICKFOLDERS flag toggles between
// only files and only folders", crates/gpui_windows/src/platform.rs:634-637);
// the reference test platform answers true.
func (a *App) CanSelectMixedFilesAndDirs() bool {
	return dialogRuntimeFor(a).driver(a).CanSelectMixedFilesAndDirs()
}

// ---------------------------------------------------------------------------
// Prompt requests: delivery gate, cancellation, dismissal
// ---------------------------------------------------------------------------

type promptKind int

const (
	promptKindPaths promptKind = iota
	promptKindNewPath
)

// promptRequest is one in-flight prompt: the delivery token bound to the
// receiving scope (ticket04's deliverySpec reused through the
// package-internal constructor), the captured owner HWND and request
// parameters, and the completion/cancellation state. All fields except
// the immutable parameters are guarded by mu; the delivery gate
// mutations (spec.closed, spec.update) happen on the foreground thread
// only, like the task ownership layer's deliveryClose.
type promptRequest struct {
	app           *App
	kind          promptKind
	spec          *deliverySpec
	owner         uintptr
	options       PathPromptOptions
	directory     string
	suggestedName string

	mu        sync.Mutex
	completed bool
	cancelled bool
	delivered bool
	discarded bool
	dismissal DialogDismissal
}

// registerDismissal is the dismiss hook handed to the driver; it runs on
// the foreground thread inside the driver call, before Show.
func (r *promptRequest) registerDismissal(d DialogDismissal) {
	r.mu.Lock()
	r.dismissal = d
	r.mu.Unlock()
}

// complete delivers or discards the eventual result, exactly once. The
// delivery update runs on the foreground thread (the host thread of a
// real app, the dispatcher goroutine of a test app): the driver call and
// this completion run there together. A cancelled request or a receiving
// scope closed since registration discards the result — the Go port of
// the reference's dropped oneshot receiver, whose send fails silently
// when the awaiting owner is gone.
func (r *promptRequest) complete(outcome any) {
	r.mu.Lock()
	if r.completed {
		r.mu.Unlock()
		return
	}
	r.completed = true
	cancelled := r.cancelled
	r.dismissal = nil
	r.mu.Unlock()
	if cancelled || !r.spec.open(r.app) {
		r.mu.Lock()
		r.discarded = true
		r.mu.Unlock()
		return
	}
	r.mu.Lock()
	r.delivered = true
	r.mu.Unlock()
	r.spec.update(r.app, outcome)
}

// discard completes the request without a result and without delivery:
// the dialog never ran (the host loop exited or the application is
// shutting down). Safe from any goroutine.
func (r *promptRequest) discard() {
	r.mu.Lock()
	if r.completed {
		r.mu.Unlock()
		return
	}
	r.completed = true
	r.dismissal = nil
	r.discarded = true
	r.mu.Unlock()
}

// cancelRequest marks the request cancelled (its eventual result
// discards) and best-effort-dismisses a shown dialog. The dismissal is
// invoked directly from the calling goroutine: DialogDismissal
// implementations marshal to the dialog's thread themselves, so a
// cancellation racing a live modal loop never waits for the loop to end
// (posted foreground closures would: the wake protocol defers them to
// after the modal loop, exactly like the reference's dispatcher during
// dialog pumping).
func (r *promptRequest) cancelRequest() {
	r.mu.Lock()
	if !r.completed && !r.cancelled {
		r.cancelled = true
	}
	dismissal := r.dismissal
	r.mu.Unlock()
	if dismissal != nil {
		dismissal()
	}
}

// PromptHandle controls one in-flight prompt request: the reference's
// returned oneshot Receiver plus its drop semantics. Handle copies alias
// the same request.
type PromptHandle struct {
	req *promptRequest
}

// Cancel discards the eventual result: closing the handle mirrors
// dropping the reference receiver (the dialog still runs to completion;
// its result is discarded). A shown dialog is dismissed best-effort
// through its registered dismissal, which marshals to the dialog's own
// thread, so Cancel is safe from any goroutine. The delivery gate state
// is mutex-guarded for exactly this cross-thread cancellation.
func (h PromptHandle) Cancel() {
	r := h.req
	if r == nil {
		return
	}
	r.cancelRequest()
}

// DismissDialog programmatically closes the shown dialog (the Win32
// IFileDialog::Close seam). The eventual result still delivers —
// normally the cancelled outcome — so tests can finish a real dialog
// deterministically without user input. Safe from any goroutine.
func (h PromptHandle) DismissDialog() {
	r := h.req
	if r == nil {
		return
	}
	r.mu.Lock()
	dismissal := r.dismissal
	r.mu.Unlock()
	if dismissal != nil {
		dismissal()
	}
}

// Pending reports whether the request is still awaiting its outcome.
func (h PromptHandle) Pending() bool {
	r := h.req
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return !r.completed
}

// Dismissible reports whether the shown dialog registered a dismissal
// (so DismissDialog/Cancel can close it programmatically). It lets tests
// wait for a real dialog to come up without user interaction.
func (h PromptHandle) Dismissible() bool {
	r := h.req
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.dismissal != nil
}

// Cancelled reports whether the request's eventual result was cancelled
// (its delivery token closed before completion).
func (h PromptHandle) Cancelled() bool {
	r := h.req
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cancelled
}

// Delivered reports whether the result was delivered to the receiving
// scope's update.
func (h PromptHandle) Delivered() bool {
	r := h.req
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.delivered
}

// Discarded reports whether the eventual result was discarded (a
// cancelled request, a closed receiving scope, or a dialog that never
// ran).
func (h PromptHandle) Discarded() bool {
	r := h.req
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.discarded
}

// ---------------------------------------------------------------------------
// App-level prompts (crates/gpui/src/app.rs:1665-1688)
// ---------------------------------------------------------------------------

// startPrompt builds and dispatches one prompt request. Real mode posts
// the driver invocation to the host queue: the wake message is only
// dispatched once the current message handler returns, so the dialog
// starts after the current update unwinds (windows platform contract,
// "Potentially pumping operations such as dialogs and drag/drop start
// after the current update unwinds"). Hostless mode queues the request
// for SimulatePathPromptResponse / SimulateNewPathSelection.
func (a *App) startPrompt(kind promptKind, owner *Window, target *Scope, options PathPromptOptions, directory, suggestedName string, deliver func(outcome any, cx *App)) *promptRequest {
	op := "PromptForPaths"
	if kind == promptKindNewPath {
		op = "PromptForNewPath"
	}
	a.sched.rejectIfShuttingDown(op)
	if deliver == nil {
		panic("gpui: " + op + " requires a delivery callback")
	}
	// The receiving scope defaults to the owner window's scope, so late
	// results are discarded after owner closure (the scope closes when
	// the OS window is destroyed); without an owner it is the app root.
	if target == nil {
		target = a.rootScope
		if owner != nil {
			target = owner.scope
		}
	}
	var ownerHWND uintptr
	if owner != nil {
		ownerHWND = owner.handle.Hwnd()
	}
	spec := a.newDeliverySpec(target, op)
	spec.update = func(app *App, result any) {
		app.Update(func(cx *App) { deliver(result, cx) })
	}
	req := &promptRequest{
		app:           a,
		kind:          kind,
		spec:          spec,
		owner:         ownerHWND,
		options:       options,
		directory:     directory,
		suggestedName: suggestedName,
	}

	if host := a.host; host != nil {
		posted := host.Post(func() {
			// The host loop already exited (this closure was drained by
			// the shutdown drain) or the application is shutting down:
			// never show a dialog then; discard instead.
			if hostLoopExited(host) || a.sched.shuttingDown {
				req.discard()
				return
			}
			ownerHWND := req.owner
			if ownerHWND == 0 {
				// find_current_active_window: the active window of the
				// foreground thread at dialog start
				// (crates/gpui_windows/src/platform.rs:294, 606).
				ownerHWND = getActiveWindowHWND()
			}
			rt := dialogRuntimeFor(a)
			var outcome any
			if kind == promptKindPaths {
				outcome = rt.driver(a).PromptForPaths(ownerHWND, options, req.registerDismissal)
			} else {
				outcome = rt.driver(a).PromptForNewPath(ownerHWND, directory, suggestedName, req.registerDismissal)
			}
			req.complete(outcome)
		})
		if !posted {
			// The host no longer accepts work; the closure was dropped.
			req.discard()
		}
		return req
	}

	rt := dialogRuntimeFor(a)
	rt.mu.Lock()
	if kind == promptKindPaths {
		rt.pendingPaths = append(rt.pendingPaths, req)
	} else {
		rt.pendingNewPath = append(rt.pendingNewPath, req)
	}
	rt.mu.Unlock()
	return req
}

// PromptForPaths displays the platform modal for selecting paths
// (App::prompt_for_paths, crates/gpui/src/app.rs:1665; the outcome
// trichotomy is Result<Option<Vec<PathBuf>>>). When one or more paths
// are selected they are relayed asynchronously through deliver; a
// cancelled dialog relays the Cancelled outcome; failures relay Err.
// The dialog parents itself to owner's window when owner is non-nil.
//
// target is the receiving scope for the result delivery (the reference
// composes this by awaiting the returned receiver inside a window-owned
// task): a nil target defaults to the owner window's scope, or the app
// root without an owner. The dialog runs on the foreground thread of a
// real application (after the current update unwinds) and its result is
// delivered as one application update while target is still open: a late
// result after owner/target closure is discarded, never delivered.
// Hostless test applications queue the prompt for
// SimulatePathPromptResponse.
func (a *App) PromptForPaths(owner *Window, target *Scope, options PathPromptOptions, deliver func(outcome PathsOutcome, cx *App)) PromptHandle {
	if deliver == nil {
		panic("gpui: PromptForPaths requires a delivery callback")
	}
	req := a.startPrompt(promptKindPaths, owner, target, options, "", "", func(outcome any, cx *App) {
		deliver(outcome.(PathsOutcome), cx)
	})
	return PromptHandle{req: req}
}

// PromptForNewPath displays the platform modal for selecting a new path
// where a file can be saved (App::prompt_for_new_path,
// crates/gpui/src/app.rs:1677). The directory sets the initial location
// and suggestedName pre-fills the file name. owner and target follow
// PromptForPaths; delivery, owner closure and hostless semantics match
// it too.
func (a *App) PromptForNewPath(owner *Window, target *Scope, directory, suggestedName string, deliver func(outcome PathOutcome, cx *App)) PromptHandle {
	if deliver == nil {
		panic("gpui: PromptForNewPath requires a delivery callback")
	}
	req := a.startPrompt(promptKindNewPath, owner, target, PathPromptOptions{}, directory, suggestedName, func(outcome any, cx *App) {
		deliver(outcome.(PathOutcome), cx)
	})
	return PromptHandle{req: req}
}

// ---------------------------------------------------------------------------
// The hostless pending-prompt queue (the TestPlatform analogue)
// ---------------------------------------------------------------------------

// queueDialogDriver is the hostless "platform": it never shows a dialog
// and answers the mixed-selection capability like the reference test
// platform (true). Its methods are only reachable through
// CanSelectMixedFilesAndDirs; the actual prompt requests are queued by
// startPrompt and answered by the Simulate methods below.
type queueDialogDriver struct{}

// CanSelectMixedFilesAndDirs mirrors the reference test platform's true.
func (queueDialogDriver) CanSelectMixedFilesAndDirs() bool { return true }

// PromptForPaths is unreachable on hostless apps (startPrompt queues
// instead of driving); it answers a failure honestly if called.
func (queueDialogDriver) PromptForPaths(owner uintptr, options PathPromptOptions, dismiss func(d DialogDismissal)) PathsOutcome {
	return PathsOutcome{Err: fmt.Errorf("gpui: hostless applications queue path prompts instead of showing dialogs")}
}

// PromptForNewPath mirrors PromptForPaths.
func (queueDialogDriver) PromptForNewPath(owner uintptr, directory, suggestedName string, dismiss func(d DialogDismissal)) PathOutcome {
	return PathOutcome{Err: fmt.Errorf("gpui: hostless applications queue new-path prompts instead of showing dialogs")}
}

// DidPromptForPaths reports whether a path selection dialog is pending
// (TestAppContext::did_prompt_for_paths / TestPlatform, crates/gpui/src
// platform/test/platform.rs:202-204).
func (a *App) DidPromptForPaths() bool {
	rt := dialogRuntimeFor(a)
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return len(rt.pendingPaths) > 0
}

// DidPromptForNewPath reports whether a new-path prompt is pending
// (TestAppContext::did_prompt_for_new_path).
func (a *App) DidPromptForNewPath() bool {
	rt := dialogRuntimeFor(a)
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return len(rt.pendingNewPath) > 0
}

// SimulatePathPromptResponse answers the oldest pending paths prompt
// (TestAppContext::simulate_path_prompt_response,
// crates/gpui/src/platform/test/platform.rs:178-198): selectPaths
// receives the recorded options and returns the selection, or cancelled
// for the reference's None. Selecting more than one path for a prompt
// that does not allow multiple selection panics, exactly like the
// reference. Must be called from the dispatcher goroutine (the delivery
// update runs inline).
func (a *App) SimulatePathPromptResponse(selectPaths func(options PathPromptOptions) (paths []string, cancelled bool)) {
	rt := dialogRuntimeFor(a)
	rt.mu.Lock()
	if len(rt.pendingPaths) == 0 {
		rt.mu.Unlock()
		panic("gpui: no pending paths prompt")
	}
	req := rt.pendingPaths[0]
	rt.pendingPaths = rt.pendingPaths[1:]
	rt.mu.Unlock()
	if selectPaths == nil {
		panic("gpui: SimulatePathPromptResponse requires a selector")
	}
	paths, cancelled := selectPaths(req.options)
	if !cancelled && !req.options.Multiple && len(paths) > 1 {
		panic(fmt.Sprintf("gpui: selected %d paths for a prompt that does not allow multiple selection", len(paths)))
	}
	outcome := PathsOutcome{Cancelled: cancelled, Paths: paths}
	req.complete(outcome)
}

// SimulateNewPathSelection answers the oldest pending new-path prompt
// (TestAppContext::simulate_new_path_selection,
// crates/gpui/src/platform/test/platform.rs:163-174): selectPath
// receives the recorded directory and returns the selected path, or
// cancelled for the reference's None.
func (a *App) SimulateNewPathSelection(selectPath func(directory string) (path string, cancelled bool)) {
	rt := dialogRuntimeFor(a)
	rt.mu.Lock()
	if len(rt.pendingNewPath) == 0 {
		rt.mu.Unlock()
		panic("gpui: no pending new path prompt")
	}
	req := rt.pendingNewPath[0]
	rt.pendingNewPath = rt.pendingNewPath[1:]
	rt.mu.Unlock()
	if selectPath == nil {
		panic("gpui: SimulateNewPathSelection requires a selector")
	}
	path, cancelled := selectPath(req.directory)
	outcome := PathOutcome{Cancelled: cancelled, Path: path}
	req.complete(outcome)
}

// ---------------------------------------------------------------------------
// App-level credentials (crates/gpui/src/app.rs:1567-1584)
// ---------------------------------------------------------------------------

// WriteCredentials writes a credential to the platform keychain
// (App::write_credentials). The work runs as a scoped task owned by
// target (nil defaults to the app root scope) through ticket04's
// SpawnDelivering seam, so closing target cancels it and discards the
// eventual result exactly like any scoped task; the delivered error
// carries the platform failure, including the credential-blob-size
// rejection. Go adaptation: the reference returns the platform's
// foreground Task directly; this port routes through the scoped-task
// seam, and the blob-size rejection is reported by the store in the task
// body instead of a ready-completed task (no ready-task constructor
// exists here).
func (a *App) WriteCredentials(target *Scope, url, username string, password []byte, deliver func(err error, cx *App)) Task[error] {
	if deliver == nil {
		panic("gpui: WriteCredentials requires a delivery callback")
	}
	if target == nil {
		target = a.rootScope
	}
	store := dialogRuntimeFor(a).credentialStore(a)
	return a.SpawnDelivering(target, func(run *TaskRun) error {
		return store.Write(url, username, password)
	}, func(err error, cx *App) {
		deliver(err, cx)
	})
}

// ReadCredentials reads a credential from the platform keychain
// (App::read_credentials). The absence result is Found=false with a nil
// error. Scoped-task semantics match WriteCredentials.
func (a *App) ReadCredentials(target *Scope, url string, deliver func(result CredentialsResult, cx *App)) Task[CredentialsResult] {
	if deliver == nil {
		panic("gpui: ReadCredentials requires a delivery callback")
	}
	if target == nil {
		target = a.rootScope
	}
	store := dialogRuntimeFor(a).credentialStore(a)
	return a.SpawnDelivering(target, func(run *TaskRun) CredentialsResult {
		username, password, found, err := store.Read(url)
		return CredentialsResult{Err: err, Found: found, Username: username, Password: password}
	}, func(result CredentialsResult, cx *App) {
		deliver(result, cx)
	})
}

// DeleteCredentials deletes a credential from the platform keychain
// (App::delete_credentials); deleting an absent entry is an explicit
// error. Scoped-task semantics match WriteCredentials.
func (a *App) DeleteCredentials(target *Scope, url string, deliver func(err error, cx *App)) Task[error] {
	if deliver == nil {
		panic("gpui: DeleteCredentials requires a delivery callback")
	}
	if target == nil {
		target = a.rootScope
	}
	store := dialogRuntimeFor(a).credentialStore(a)
	return a.SpawnDelivering(target, func(run *TaskRun) error {
		return store.Delete(url)
	}, func(err error, cx *App) {
		deliver(err, cx)
	})
}
