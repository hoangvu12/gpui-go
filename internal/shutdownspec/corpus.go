// Package shutdownspec holds ticket30's cross-service close,
// cancellation and shutdown corpus: the deterministic adversarial
// schedules over shared entities, scopes, tasks, effects, focus and
// abandoned frames, and the real multiwindow close/reopen/modal/
// provider/capture cases with delayed GPU completion and callbacks
// arriving after window removal. They exercise the public gpui API from
// outside the runtime package, matching the repo convention (see
// internal/taskspec and internal/winhostspec).
//
// Environment expectation: the deterministic tests draw through the
// native layout/scene services; the real tests create and show real
// Win32 windows in the user's interactive session, drive the real
// D3D11/capture/UIA/dialog services and FAIL (never skip) when creation
// fails. Every real test boots a fresh host and closes everything it
// opens (testing.Cleanup); every wait is a generous poll.
//
// Mouse note (this machine's corrupted post-fullscreen shell state):
// no test drives or asserts the real cursor. Window interactions are
// synthetic messages (PostMessage/SendMessage) and explicit API calls
// only.
//
// Scope discipline against the existing suites: the task-layer
// handoff/cancellation schedules (internal/taskspec), the entity/lease
// and effect-loop traces (internal/portfixture/ownership_test.go), the
// a11y staleness matrix (internal/a11yspec) and the dialog modal-pump
// basics (internal/dialogspec) already exist. This package owns their
// SHUTDOWN composition: the close cascade that crosses services (window
// removal → scope close → lease release → focus release → element
// runtime teardown), the shutdown drain's effect flushing and
// unresolved records, delayed GPU retirement after window removal, the
// capture stream stopping/destroying under a live scene reference, the
// late WM_GETOBJECT fall-through and generation-checked refusals, and
// the modal dialog pumping during a full close cascade. Where a behavior
// is already pinned by an earlier suite, the test cites it instead of
// duplicating it; trace notes (reference-preserved vs documented Go
// adaptation, with pin citations) live in TRACES.md.
package shutdownspec

import (
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

// ---------------------------------------------------------------------------
// Real-host boot (the capturespec/dialogspec pattern)
// ---------------------------------------------------------------------------

// Win32 bindings the corpus brings itself (the a11yspec pattern: tests
// bring their own; the package under test never exposes them).
var (
	modUser32 = syscall.NewLazyDLL("user32.dll")
	procPost  = modUser32.NewProc("PostMessageW")
	procSend  = modUser32.NewProc("SendMessageW")
	procIsWnd = modUser32.NewProc("IsWindow")
)

// Message ids (winuser.h).
const (
	wmClose = 0x0010
)

// postMessage posts one message to a window (the synthetic-message
// discipline: no real cursor input anywhere).
func postMessage(hwnd uintptr, message uint32, wparam, lparam uintptr) bool {
	ret, _, _ := procPost.Call(hwnd, uintptr(message), wparam, lparam)
	return ret != 0
}

// sendMessage sends one message synchronously and returns the window
// procedure's answer.
func sendMessage(hwnd uintptr, message uint32, wparam, lparam uintptr) uintptr {
	ret, _, _ := procSend.Call(hwnd, uintptr(message), wparam, lparam)
	return ret
}

// isWindow reports whether the HWND still exists.
func isWindow(hwnd uintptr) bool {
	ret, _, _ := procIsWnd.Call(hwnd)
	return ret != 0
}

// waitFor polls cond until it holds or the timeout expires (fail, never
// skip).
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
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

// recvWithin receives one value from ch, failing after the timeout.
func recvWithin[T any](t *testing.T, timeout time.Duration, what string, ch chan T) T {
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

// realFixture is one booted real application: the host thread running
// the message loop, the application attached, and the Run goroutine
// joined at cleanup.
type realFixture struct {
	app     *gpui.App
	host    *gpui.Host
	runDone chan error
	runOnce sync.Once
	runErr  error
}

// waitRun joins the App.Run goroutine once (idempotent: the test and
// the cleanup can both wait).
func (f *realFixture) waitRun() error {
	f.runOnce.Do(func() { f.runErr = <-f.runDone })
	return f.runErr
}

// bootRealApp boots a fresh host-backed application running through
// App.Run, stopping both at cleanup (LIFO: a renderer created after this
// cleans up first).
func bootRealApp(t *testing.T) *realFixture {
	t.Helper()
	before := runtime.NumGoroutine()
	app := gpui.NewApp()
	host := gpui.NewHost()
	if err := app.Attach(host); err != nil {
		t.Fatalf("app attach: %v", err)
	}
	if err := host.Start(); err != nil {
		t.Fatalf("host start: %v — this session is expected to create real windows", err)
	}
	f := &realFixture{app: app, host: host, runDone: make(chan error, 1)}
	go func() { f.runDone <- app.Run(host) }()
	t.Cleanup(func() {
		if err := host.Stop(); err != nil {
			t.Errorf("host stop: %v", err)
		}
		_ = f.waitRun()
		waitFor(t, 5*time.Second, "goroutines to drain back near the baseline", func() bool {
			return runtime.NumGoroutine() <= before+2
		})
	})
	return f
}

// sync drives f on the foreground (host) thread and waits for it.
func (f *realFixture) sync(t *testing.T, run func()) {
	t.Helper()
	done := make(chan struct{})
	if !f.host.Post(func() {
		defer close(done)
		run()
	}) {
		t.Fatalf("foreground post was dropped: the host loop exited")
	}
	<-done
}

// openShown opens one shown app window, failing on creation errors,
// with an optional close hook fired from the WM_DESTROY path.
func (f *realFixture) openShown(t *testing.T, title string, w, h float32, onClose func()) *gpui.Window {
	t.Helper()
	window, err := f.app.OpenWindow(gpui.WindowOptions{
		Title:     title,
		Show:      true,
		Activate:  true,
		Resizable: true,
		Bounds:    gpui.Bounds{Size: gpui.Size{Width: w, Height: h}},
		OnClose:   onClose,
	})
	if err != nil {
		t.Fatalf("app.OpenWindow(%q): %v — this session is expected to create real windows", title, err)
	}
	t.Cleanup(func() {
		if window.Alive() {
			window.Close()
			waitFor(t, 10*time.Second, "the window to be destroyed", func() bool { return !window.Alive() })
		}
	})
	waitFor(t, 10*time.Second, "the window to be visible", window.Handle().Visible)
	return window
}

// ---------------------------------------------------------------------------
// The deterministic corpus elements
// ---------------------------------------------------------------------------

// switchView is a stateless root view whose render flips between a
// plain element and a panicking one (the abandoned-frame schedule; the
// inspectorspec/listspec precedent). The flip field is plain: the
// deterministic tests are single-goroutine.
type switchView struct {
	panicNext bool
}

// EntityID implements gpui.View: stateless recipe.
func (v *switchView) EntityID() (gpui.EntityID, bool) { return 0, false }

// RenderOnce implements gpui.View.
func (v *switchView) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	if v.panicNext {
		return panickingElement()
	}
	return gpui.Empty()
}

// panickingElement returns an element whose paint panics (the abandoned
// build; the listspec/inspectorspec precedent).
func panickingElement() gpui.AnyElement {
	return gpui.CustomElement[struct{}, struct{}](&panicElement{})
}

type panicElement struct{}

// ID implements Element.
func (e *panicElement) ID() (gpui.ElementID, bool) { return gpui.ElementID{}, false }

// SourceLocation implements Element.
func (e *panicElement) SourceLocation() *gpui.SourceLocation { return nil }

// RequestLayout implements Element.
func (e *panicElement) RequestLayout(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, w *gpui.Window, app *gpui.App) (gpui.LayoutID, struct{}) {
	id, err := gpui.RequestElementLayout(w, gpui.DefaultStyle())
	if err != nil {
		panic(err)
	}
	return id, struct{}{}
}

// Prepaint implements Element.
func (e *panicElement) Prepaint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *struct{}, w *gpui.Window, app *gpui.App) struct{} {
	return struct{}{}
}

// Paint implements Element: the build fails here.
func (e *panicElement) Paint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *struct{}, prepaint *struct{}, w *gpui.Window, app *gpui.App) {
	panic("gpui: abandoned build (shutdownspec)")
}

// LayoutNodeStyle implements the root-stretch report.
func (e *panicElement) LayoutNodeStyle() (gpui.Style, bool) { return gpui.DefaultStyle(), true }

// ---------------------------------------------------------------------------
// The shared-entity corpus (the close-cascade model)
// ---------------------------------------------------------------------------

// sharedModel is the cross-window corpus state: one entity independently
// leased by two window scopes whose release hook records the exactly-once
// logical release (the "Independent leases in two windows" corpus item
// composed with the window close cascade).
type sharedModel struct {
	// Releases counts OnRelease hook runs.
	Releases int
	// HookScopeClosed records whether the window scope was already
	// closed when the hook ran (the flush-boundary evidence).
	HookScopeClosed bool
}

// releaseLog records ordered close-cascade events from hooks and
// listeners (deterministic ordering assertions).
type releaseLog struct {
	mu     sync.Mutex
	events []string
}

func (l *releaseLog) record(event string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, event)
}

func (l *releaseLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string{}, l.events...)
}
