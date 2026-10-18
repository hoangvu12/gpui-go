//go:build windows

package a11yspec

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Win32 bindings the corpus brings itself (the desktopspec pattern:
// tests bring their own; the package under test never exposes them).
var (
	modUser32 = syscall.NewLazyDLL("user32.dll")
	procSend  = modUser32.NewProc("SendMessageW")
	procIsWnd = modUser32.NewProc("IsWindow")
	procGetFg = modUser32.NewProc("GetForegroundWindow")
)

// Message ids and object ids (winuser.h; UiaRootObjectId from
// oleacc/uiautomation — the accesskit_windows convention).
const (
	wmGetObject = 0x003D

	objidClient     = uintptr(^uintptr(3))  // OBJID_CLIENT = -4
	objidCaret      = uintptr(^uintptr(7))  // OBJID_CARET = -8
	uiaRootObjectId = uintptr(^uintptr(24)) // UiaRootObjectId = -25
)

// waitFor polls a condition until the timeout (fail, never skip).
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

// a11yFixture is one booted host-backed application with one real
// a11y corpus window.
type a11yFixture struct {
	app    *gpui.App
	host   *gpui.Host
	window *gpui.Window
	model  gpui.Entity[a11yModel]
}

// bootA11yFixture boots a fresh host, attaches the application and
// opens one real (but HIDDEN) window whose root view is the corpus.
// Window-creation failures FAIL (interactive-session expectation). The
// window stays hidden so ambient assistive technology (a real screen
// reader connected to this session) cannot sweep it with UIA queries:
// the deterministic gates assert the no-AT state, and only the
// Narrator evidence test shows a window.
func bootA11yFixture(t *testing.T, title string) *a11yFixture {
	t.Helper()
	host := gpui.NewHost()
	if err := host.Start(); err != nil {
		t.Fatalf("host start: %v — this session is expected to create real windows", err)
	}
	t.Cleanup(func() {
		if err := host.Stop(); err != nil {
			t.Errorf("host stop: %v", err)
		}
	})
	app := gpui.NewApp()
	if err := app.Attach(host); err != nil {
		t.Fatalf("app attach: %v", err)
	}
	var window *gpui.Window
	var model gpui.Entity[a11yModel]
	done := make(chan error, 1)
	if !host.Post(func() {
		defer close(done)
		var err error
		window, err = app.OpenWindowView(gpui.WindowOptions{
			Title:  title,
			Bounds: gpui.Bounds{Origin: gpui.Point{X: 40, Y: 40}, Size: gpui.Size{Width: 400, Height: 300}},
			Show:   false,
		}, func(w *gpui.Window, app *gpui.App) gpui.View {
			model = newA11yModel(app, w)
			return &a11yRootView{model: model}
		})
		if err != nil {
			done <- err
		}
	}) {
		t.Fatalf("foreground post was dropped: the host loop exited")
	}
	if err := <-done; err != nil {
		t.Fatalf("OpenWindowView(%q) failed — this session is expected to create real windows: %v", title, err)
	}
	t.Cleanup(func() {
		if window.Alive() {
			window.Close()
			waitFor(t, 10*time.Second, "the window to be destroyed", func() bool { return !window.Alive() })
		}
	})
	return &a11yFixture{app: app, host: host, window: window, model: model}
}

// sync drives f on the foreground (host) thread and waits for it.
func (f *a11yFixture) sync(t *testing.T, run func()) {
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

// drawAndPublish draws one frame and publishes the accessibility tree,
// both on the foreground thread (the pin's end-of-frame publication).
func (f *a11yFixture) drawAndPublish(t *testing.T) *gpui.A11ySnapshot {
	t.Helper()
	var snapshot *gpui.A11ySnapshot
	var err error
	f.sync(t, func() {
		if _, err = gpui.DrawWindowFrame(f.window); err != nil {
			return
		}
		snapshot, err = gpui.PublishWindowA11y(f.window)
	})
	if err != nil {
		t.Fatalf("draw and publish: %v", err)
	}
	if snapshot == nil {
		t.Fatal("draw and publish returned no snapshot")
	}
	return snapshot
}

// publishOnly publishes without drawing (the no-new-frame path).
func (f *a11yFixture) publishOnly(t *testing.T) *gpui.A11ySnapshot {
	t.Helper()
	var snapshot *gpui.A11ySnapshot
	var err error
	f.sync(t, func() {
		snapshot, err = gpui.PublishWindowA11y(f.window)
	})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	return snapshot
}

// provider reads the provider diagnostics.
func (f *a11yFixture) provider(t *testing.T) (gpui.A11yProviderRecord, bool) {
	t.Helper()
	rec, ok := gpui.A11yProviderState(f.window)
	return rec, ok
}

// query sends one synthetic WM_GETOBJECT with the object id and
// returns the window procedure's answer.
func (f *a11yFixture) query(objid uintptr) uintptr {
	return queryWindow(f.window.Handle().Hwnd(), objid)
}

// queryWindow sends one synthetic WM_GETOBJECT to a specific window.
func queryWindow(hwnd uintptr, objid uintptr) uintptr {
	r, _, _ := procSend.Call(hwnd, uintptr(wmGetObject), 0, objid)
	return r
}

// nodeByAuthor finds a published node by its author id.
func nodeByAuthor(t *testing.T, snapshot *gpui.A11ySnapshot, author string) gpui.A11ySnapshotNode {
	t.Helper()
	for _, node := range snapshot.Nodes() {
		if node.AuthorID == author {
			return node
		}
	}
	t.Fatalf("no published node with author id %q in %s", author, snapshot.Trace())
	return gpui.A11ySnapshotNode{}
}

// ---------------------------------------------------------------------------
// Provider setup (a11y_init at window setup)
// ---------------------------------------------------------------------------

// TestProviderSetupAtActivation pins the pin's a11y_init shape: the
// adapter is created when the accessibility runtime activates, on the
// message thread, carrying the window title as the initial tree's
// label; before activation WM_GETOBJECT falls to DefWindowProc (no
// provider to route to); a hostless test window never gets one.
func TestA11yProviderSetupAtActivation(t *testing.T) {
	f := bootA11yFixture(t, "Provider Setup")

	// Before activation: the query falls to DefWindowProc (the pin's
	// a11y.as_mut()? None arm) and no provider record exists.
	if r := f.query(uiaRootObjectId); r != 0 {
		t.Logf("DefWindowProc answered %#x for the unactivated window", r)
	}
	if _, ok := f.provider(t); ok {
		t.Fatal("a provider record exists before the accessibility runtime activated")
	}
	if ok, _, _ := procIsWnd.Call(f.window.Handle().Hwnd()); ok == 0 {
		t.Fatal("the window died during the unactivated query")
	}

	minimal := gpui.ActivateWindowA11y(f.window)
	if minimal == nil {
		t.Fatal("activation returned no snapshot")
	}
	rec, ok := f.provider(t)
	if !ok {
		t.Fatal("no provider record after activation")
	}
	if !rec.Available {
		t.Fatalf("provider unavailable after activation: %+v", rec)
	}
	if rec.Err != "" {
		t.Errorf("provider error after creation: %s", rec.Err)
	}
	if rec.Hwnd == 0 || rec.Hwnd != f.window.Handle().Hwnd() {
		t.Errorf("provider hwnd = %x, want the window's %x", rec.Hwnd, f.window.Handle().Hwnd())
	}
	if rec.Title != "Provider Setup" {
		t.Errorf("provider initial-tree title = %q, want the window title", rec.Title)
	}
	if rec.NativeHandle == 0 {
		t.Error("provider native handle is zero")
	}
	if rec.Active || rec.Activated {
		t.Errorf("provider active/activated before any WM_GETOBJECT = %v/%v, want false/false", rec.Active, rec.Activated)
	}
	if rec.Destroyed {
		t.Error("provider reports destroyed at creation")
	}
}

// TestHostlessWindowHasNoProvider pins the neutral path: test windows
// (no host lease) keep the pure ticket-21 Go snapshot model — no
// provider, no provider error, and the poll entry reports it.
func TestA11yHostlessWindowHasNoProvider(t *testing.T) {
	testApp := gpui.NewTestApp()
	app := testApp.App()
	w := gpui.NewTestWindow(app, gpui.Size{Width: 320, Height: 240})
	model := newA11yModel(app, w)
	w.SetRootView(&a11yRootView{model: model})

	gpui.ActivateWindowA11y(w)
	rec, ok := gpui.A11yProviderState(w)
	if !ok {
		t.Fatal("the hostless window's accessibility runtime is missing")
	}
	if rec.Available {
		t.Error("a hostless test window has a native provider")
	}
	if rec.Err != "" {
		t.Errorf("hostless provider error: %s", rec.Err)
	}
	if _, err := gpui.PollWindowA11yActions(w); err == nil {
		t.Error("polling a hostless window must fail")
	} else if err != gpui.ErrA11yNoProvider {
		t.Errorf("hostless poll error = %v, want ErrA11yNoProvider", err)
	}
	if _, err := gpui.DrawWindowFrame(w); err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}
	if snapshot, err := gpui.PublishWindowA11y(w); err != nil {
		t.Fatalf("PublishWindowA11y: %v", err)
	} else if snapshot.NodeCount() == 0 {
		t.Fatal("the hostless publication produced no nodes")
	}
	gpui.CloseTestWindow(w)
}

// ---------------------------------------------------------------------------
// WM_GETOBJECT routing and activation (events.rs 855-871)
// ---------------------------------------------------------------------------

// TestWmGetObjectActivationBeforeCompleteTree pins the
// activation-before-complete-tree flow: the complete tree is published
// only after the assistive technology connects; until then the native
// publication is skipped (the pin's factory-not-called behavior), the
// synthetic UIA root query activates the adapter (the initial minimal
// tree is served natively), the activation forces a redraw, and the
// NEXT frame's publication reaches the live tree.
func TestA11yWmGetObjectActivationBeforeCompleteTree(t *testing.T) {
	f := bootA11yFixture(t, "Activation Flow")
	gpui.ActivateWindowA11y(f.window)

	// One complete frame published while NO assistive technology is
	// connected: the Go snapshot exists, the native publication is
	// skipped (update_if_active never decodes).
	snapshot := f.drawAndPublish(t)
	if snapshot.NodeCount() < 4 {
		t.Fatalf("the complete tree has %d nodes, want the corpus tree (>= 4)", snapshot.NodeCount())
	}
	rec, _ := f.provider(t)
	if rec.Active {
		t.Fatal("the provider is active before any WM_GETOBJECT")
	}
	if rec.Publishes != 0 {
		t.Fatalf("native publications before activation = %d, want 0 (update_if_active is a no-op while inactive)", rec.Publishes)
	}
	if rec.Err != "" {
		t.Fatalf("provider error before activation: %s", rec.Err)
	}

	// The synthetic assistive technology: the UIA root object query.
	// The window procedure routes it to the adapter (the native side
	// converts the LRESULT outside every lock; a nested WM_GETOBJECT
	// delivered by that conversion re-enters this same routing safely).
	lresult := f.query(uiaRootObjectId)
	rec, _ = f.provider(t)
	if rec.WMGetObjects < 1 {
		t.Errorf("routed WM_GETOBJECT count = %d, want at least 1 (the root query)", rec.WMGetObjects)
	}
	if !rec.Activated || !rec.Active {
		t.Fatalf("provider activated/active after the root query = %v/%v, want true/true (the activation edge)", rec.Activated, rec.Active)
	}
	if rec.Err != "" {
		t.Errorf("provider error after the root query: %s", rec.Err)
	}
	t.Logf("wm_getobject lresult = %#x (nested re-entrant queries included in the count: %d)", lresult, rec.WMGetObjects)

	// The activation edge forces a redraw (the pin's forced frame so
	// the complete tree reaches the client).
	if !f.window.RefreshRequested() {
		t.Error("no redraw was requested at the activation edge (the pin forces a frame)")
	}

	// The next frame's publication reaches the live tree.
	snapshot = f.drawAndPublish(t)
	rec, _ = f.provider(t)
	if rec.Publishes != 1 {
		t.Fatalf("native publications after activation = %d, want 1", rec.Publishes)
	}
	if !rec.LastActive || !rec.LastApplied {
		t.Fatalf("last publish record active/applied = %v/%v, want true/true (the activation record)", rec.LastActive, rec.LastApplied)
	}
	t.Logf("activation record: events-raised=%v", rec.LastEventsRaised)
	if snapshot.NodeCount() < 4 {
		t.Fatalf("the post-activation tree has %d nodes, want the complete corpus tree", snapshot.NodeCount())
	}

	// OBJID_CLIENT is answered by the adapter too (accesskit_windows
	// serves the client-area object id; the LRESULT conversion may
	// deliver nested re-entrant queries that also route).
	before := rec.WMGetObjects
	if lresult := f.query(objidClient); lresult == 0 {
		t.Logf("OBJID_CLIENT query returned 0")
	}
	rec, _ = f.provider(t)
	if rec.WMGetObjects < before+1 {
		t.Errorf("routed WM_GETOBJECT count = %d, want at least %d (the client query routes too)", rec.WMGetObjects, before+1)
	}

	// A negative object id the adapter declines (OBJID_CARET) still
	// routes through the provider (the native service flips its active
	// flag on any routed query) and falls to DefWindowProc.
	before = rec.WMGetObjects
	f.query(objidCaret)
	rec, _ = f.provider(t)
	if rec.WMGetObjects < before+1 {
		t.Errorf("routed WM_GETOBJECT count after the declined object id = %d, want at least %d", rec.WMGetObjects, before+1)
	}
	if !rec.Active {
		t.Error("the provider de-activated after a declined object id")
	}
}

// ---------------------------------------------------------------------------
// Publication semantics (window.rs 1161-1176 a11y_tree_update)
// ---------------------------------------------------------------------------

// TestPublicationSemantics pins the per-frame publication semantics:
// only completed frames publish (a publication with no new frame sends
// nothing), every completed frame publishes exactly once while the
// assistive technology is connected, and the activation record is
// observable.
func TestA11yPublicationSemantics(t *testing.T) {
	f := bootA11yFixture(t, "Publish Semantics")
	gpui.ActivateWindowA11y(f.window)
	gpui.SetA11yWindowTitle(f.window, "Publish Semantics")

	// Activate through the synthetic query first.
	f.query(uiaRootObjectId)

	// Frame 1.
	f.drawAndPublish(t)
	rec, _ := f.provider(t)
	if rec.Publishes != 1 {
		t.Fatalf("publications after frame 1 = %d, want 1", rec.Publishes)
	}
	seq := f.publishOnly(t)
	if seq == nil {
		t.Fatal("the no-new-frame publication returned no snapshot")
	}
	rec, _ = f.provider(t)
	if rec.Publishes != 1 {
		t.Fatalf("publications after a no-new-frame publish = %d, want 1 (only completed frames publish)", rec.Publishes)
	}

	// Frame 2 after a model mutation (the tree changes; the native
	// update applies and raises events outside the lock).
	f.model.Update(f.app, func(m *a11yModel, cx *gpui.Context[a11yModel]) {
		m.clickRuns++
		cx.Notify()
	})
	f.drawAndPublish(t)
	rec, _ = f.provider(t)
	if rec.Publishes != 2 {
		t.Fatalf("publications after frame 2 = %d, want 2", rec.Publishes)
	}
	if !rec.LastApplied {
		t.Error("the last publication did not apply")
	}
	if !rec.LastEventsRaised {
		t.Log("the last publication raised no change events (honest record)")
	}

	// The refreshed title reaches the published root label (the
	// initial-tree label was baked at creation; frame publications
	// carry the current title).
	snapshot := f.publishOnly(t)
	root, ok := snapshot.Node(gpui.RootA11yNodeID)
	if !ok || root.Label != "Publish Semantics" {
		t.Errorf("published root label = %q, want the refreshed title", root.Label)
	}
}

// ---------------------------------------------------------------------------
// Action routing (window.rs 7130-7200 handle_a11y_action)
// ---------------------------------------------------------------------------

// TestActionRoutingListenersAndBuiltins pins the drain path's routing:
// listeners first (Click/SetValue/SetTextSelection with the decoded
// payloads), then the built-ins (Focus -> the bound handle, Blur ->
// the window, Click -> synthetic mouse events at the node bounds
// center through the real input dispatch).
func TestA11yActionRoutingListenersAndBuiltins(t *testing.T) {
	f := bootA11yFixture(t, "Action Routing")
	gpui.ActivateWindowA11y(f.window)
	f.query(uiaRootObjectId)
	snapshot := f.drawAndPublish(t)

	button := nodeByAuthor(t, snapshot, "increment")
	editor := nodeByAuthor(t, snapshot, "editor")
	status := nodeByAuthor(t, snapshot, "status")

	// Listeners first: Click on the button (with the value payload).
	if err := gpui.DispatchWindowA11yRequest(f.window, button.ID, gpui.A11yActionClick, &gpui.A11yActionData{Value: "voice"}); err != nil {
		t.Fatalf("dispatching Click on the button: %v", err)
	}
	facts := readModelFacts(f.model, f.app)
	if facts.clickRuns != 1 {
		t.Errorf("click listener runs = %d, want 1", facts.clickRuns)
	}
	if facts.lastValue != "voice" {
		t.Errorf("click listener last value = %q, want the decoded payload", facts.lastValue)
	}

	// Increment through its listener.
	if err := gpui.DispatchWindowA11yRequest(f.window, button.ID, gpui.A11yActionIncrement, nil); err != nil {
		t.Fatalf("dispatching Increment: %v", err)
	}
	if facts = readModelFacts(f.model, f.app); facts.incrementRuns != 1 {
		t.Errorf("increment listener runs = %d, want 1", facts.incrementRuns)
	}

	// SetValue with the value payload.
	if err := gpui.DispatchWindowA11yRequest(f.window, editor.ID, gpui.A11yActionSetValue, &gpui.A11yActionData{Value: "typed"}); err != nil {
		t.Fatalf("dispatching SetValue: %v", err)
	}
	if facts = readModelFacts(f.model, f.app); facts.valueRuns != 1 || facts.lastValue != "typed" {
		t.Errorf("SetValue listener runs/value = %d/%q, want 1/typed", facts.valueRuns, facts.lastValue)
	}

	// SetTextSelection with the selection payload.
	selection := &gpui.A11yTextSelection{AnchorNode: editor.ID, AnchorChar: 2, FocusNode: editor.ID, FocusChar: 7}
	if err := gpui.DispatchWindowA11yRequest(f.window, editor.ID, gpui.A11yActionSetTextSelection, &gpui.A11yActionData{TextSelection: selection}); err != nil {
		t.Fatalf("dispatching SetTextSelection: %v", err)
	}
	if facts = readModelFacts(f.model, f.app); facts.selectionRuns != 1 {
		t.Fatalf("SetTextSelection listener runs = %d, want 1", facts.selectionRuns)
	} else if facts.lastSelection != *selection {
		t.Errorf("delivered selection = %+v, want %+v", facts.lastSelection, *selection)
	}

	// The Focus built-in: the button has a focus binding and no Focus
	// listener, so the request focuses the bound handle.
	if err := gpui.DispatchWindowA11yRequest(f.window, button.ID, gpui.A11yActionFocus, nil); err != nil {
		t.Fatalf("dispatching Focus: %v", err)
	}
	if focused := f.window.Focused(); !focused.Ok || focused.Handle.ID() != facts.incFocus.ID() {
		t.Errorf("focused handle after the Focus built-in = %+v, want the button's binding", focused)
	}
	// Blur clears it.
	if err := gpui.DispatchWindowA11yRequest(f.window, editor.ID, gpui.A11yActionBlur, nil); err != nil {
		t.Fatalf("dispatching Blur: %v", err)
	}
	if focused := f.window.Focused(); focused.Ok {
		t.Error("the window is still focused after the Blur built-in")
	}

	// The Click built-in fallback: the status label has no listeners,
	// so Click synthesizes MouseDown/MouseUp at the node bounds center
	// through the real input dispatch (the tracked position is the
	// observable; no real cursor is involved).
	if status.Bounds.Size.Width <= 0 || status.Bounds.Size.Height <= 0 {
		t.Fatalf("the status label node has no bounds: %+v", status.Bounds)
	}
	wantCenter := gpui.Point{
		X: status.Bounds.Origin.X + status.Bounds.Size.Width/2,
		Y: status.Bounds.Origin.Y + status.Bounds.Size.Height/2,
	}
	if err := gpui.DispatchWindowA11yRequest(f.window, status.ID, gpui.A11yActionClick, nil); err != nil {
		t.Fatalf("dispatching the built-in Click: %v", err)
	}
	if got := f.window.TrackedMousePosition(); got != wantCenter {
		t.Errorf("tracked mouse position after the built-in Click = (%.1f,%.1f), want the node bounds center (%.1f,%.1f)", got.X, got.Y, wantCenter.X, wantCenter.Y)
	}
	if facts = readModelFacts(f.model, f.app); facts.clickRuns != 1 {
		t.Errorf("click listener runs after the built-in Click = %d, want 1 (no listener ran for the label)", facts.clickRuns)
	}

	// An action with no listener and no built-in is a typed error.
	if err := gpui.DispatchWindowA11yRequest(f.window, button.ID, gpui.A11yActionDecrement, nil); err != gpui.ErrA11yNoHandler {
		t.Errorf("unhandled action error = %v, want ErrA11yNoHandler", err)
	}

	// The on-demand poll entry: the native queue is empty here (no
	// real UIA invocation happened), so nothing routes and no error is
	// reported.
	if routed, err := gpui.PollWindowA11yActions(f.window); err != nil {
		t.Fatalf("polling the provider: %v", err)
	} else if routed != 0 {
		t.Errorf("routed count on the empty queue = %d, want 0", routed)
	}
}

// ---------------------------------------------------------------------------
// Staleness and generations
// ---------------------------------------------------------------------------

// TestStaleRequestsAndNativeHandle pins the generation-checked request
// path: a request for a removed node is rejected before any listener
// runs; closing the accessibility runtime rejects everything; the
// destroyed adapter's native handle reports stale through the native
// client (slot generation guard).
func TestA11yStaleRequestsAndNativeHandle(t *testing.T) {
	f := bootA11yFixture(t, "Stale")
	gpui.ActivateWindowA11y(f.window)
	f.query(uiaRootObjectId)
	snapshot := f.drawAndPublish(t)
	editor := nodeByAuthor(t, snapshot, "editor")

	// Remove the editor node from the tree and republish.
	f.model.Update(f.app, func(m *a11yModel, cx *gpui.Context[a11yModel]) {
		m.showEditor = false
		cx.Notify()
	})
	f.drawAndPublish(t)
	if err := gpui.DispatchWindowA11yRequest(f.window, editor.ID, gpui.A11yActionSetValue, &gpui.A11yActionData{Value: "late"}); err != gpui.ErrA11yNodeRemoved {
		t.Errorf("request for the removed node = %v, want ErrA11yNodeRemoved", err)
	}
	if facts := readModelFacts(f.model, f.app); facts.valueRuns != 0 {
		t.Errorf("the removed node's listener ran %d times, want 0", facts.valueRuns)
	}

	// Retire the runtime: every later request is rejected, the adapter
	// is destroyed, and the native handle goes stale.
	rec, _ := f.provider(t)
	oldHandle := rec.NativeHandle
	gpui.CloseWindowA11y(f.window)
	if _, ok := f.provider(t); ok {
		t.Error("the provider record survives CloseWindowA11y")
	}
	if err := gpui.DispatchWindowA11yRequest(f.window, editor.ID, gpui.A11yActionClick, nil); err != gpui.ErrA11yWindowGone {
		t.Errorf("request after the runtime closed = %v, want ErrA11yWindowGone", err)
	}
	if _, err := gpui.ProbeNativeA11yAdapter(f.app, oldHandle); err == nil {
		t.Error("the destroyed adapter's native handle still answers")
	} else if err != gpui.ErrA11yNativeStaleHandle {
		t.Errorf("destroyed native handle error = %v, want the stale-handle error", err)
	}
}

// TestCloseReopenAdapterLifetime pins close/reopen: destroying the
// closed window's adapter releases its native slot (the new window's
// adapter gets a distinct handle; the old handle is stale) and the
// new window activates and publishes normally.
func TestA11yCloseReopenAdapterLifetime(t *testing.T) {
	f := bootA11yFixture(t, "Reopen A")
	gpui.ActivateWindowA11y(f.window)
	f.query(uiaRootObjectId)
	f.drawAndPublish(t)

	rec, _ := f.provider(t)
	handleA := rec.NativeHandle
	if handleA == 0 {
		t.Fatal("window A's provider has no native handle")
	}

	// Open a second window (also host-backed, also hidden) so closing the
	// first does not end the host's message loop.
	windowB, err := f.app.OpenWindowView(gpui.WindowOptions{
		Title:  "Reopen B",
		Bounds: gpui.Bounds{Origin: gpui.Point{X: 80, Y: 80}, Size: gpui.Size{Width: 400, Height: 300}},
		Show:   false,
	}, func(w *gpui.Window, app *gpui.App) gpui.View {
		model := newA11yModel(app, w)
		return &a11yRootView{model: model}
	})
	if err != nil {
		t.Fatalf("OpenWindowView(Reopen B): %v — this session is expected to create real windows", err)
	}
	gpui.ActivateWindowA11y(windowB)
	queryWindow(windowB.Handle().Hwnd(), uiaRootObjectId)
	recB, ok := gpui.A11yProviderState(windowB)
	if !ok || !recB.Available {
		t.Fatalf("window B's provider is unavailable: %+v", recB)
	}
	if recB.NativeHandle == handleA {
		t.Errorf("window B's native handle %d equals the still-live window A handle", recB.NativeHandle)
	}

	// Close window A: WM_DESTROY retires its scope, the release
	// sentinel destroys the adapter on the message thread.
	f.window.Close()
	waitFor(t, 10*time.Second, "window A to be destroyed", func() bool { return !f.window.Alive() })
	rec, ok = gpui.A11yProviderState(f.window)
	if !ok {
		t.Fatal("window A's a11y runtime disappeared without CloseWindowA11y")
	}
	if !rec.Destroyed {
		t.Fatalf("window A's provider was not destroyed at window close: %+v", rec)
	}
	if rec.Err != "" {
		t.Errorf("window A's provider error at destroy: %s", rec.Err)
	}
	// The destroyed handle is stale through the native client.
	if _, err := gpui.ProbeNativeA11yAdapter(f.app, handleA); err == nil {
		t.Error("window A's destroyed handle still answers")
	} else if err != gpui.ErrA11yNativeStaleHandle {
		t.Errorf("window A's destroyed handle error = %v, want the stale-handle error", err)
	}

	// Window B (the reopened surface) still activates and publishes.
	done := make(chan struct{})
	if !f.host.Post(func() {
		defer close(done)
		if _, err := gpui.DrawWindowFrame(windowB); err != nil {
			t.Errorf("drawing window B: %v", err)
			return
		}
		if _, err := gpui.PublishWindowA11y(windowB); err != nil {
			t.Errorf("publishing window B: %v", err)
		}
	}) {
		t.Fatalf("foreground post was dropped")
	}
	<-done
	recB, _ = gpui.A11yProviderState(windowB)
	if recB.Publishes != 1 || !recB.LastApplied {
		t.Fatalf("window B publications after its frame = %d applied=%v, want 1 applied=true (the reopened adapter serves the new window)", recB.Publishes, recB.LastApplied)
	}
}

// ---------------------------------------------------------------------------
// Concurrency (serving queries from the published snapshot)
// ---------------------------------------------------------------------------

// TestConcurrentQueriesUpdatesAndActions pins the concurrent slice:
// WM_GETOBJECT queries (routed on the message thread, answering from
// the native tree without touching application state), provider-state
// reads and native is-active probes run concurrently with frame
// publications and action requests; no deadlock, no panic, and the
// final publication applies.
func TestA11yConcurrentQueriesUpdatesAndActions(t *testing.T) {
	f := bootA11yFixture(t, "Concurrent")
	gpui.ActivateWindowA11y(f.window)
	f.query(uiaRootObjectId)
	snapshot := f.drawAndPublish(t)
	button := nodeByAuthor(t, snapshot, "increment")

	rec, _ := f.provider(t)
	handle := rec.NativeHandle
	if handle == 0 {
		t.Fatal("the provider has no native handle")
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})
	objids := []uintptr{uiaRootObjectId, objidClient, objidCaret, uiaRootObjectId}
	for i := 0; i < len(objids); i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			objid := objids[i]
			for j := 0; j < 25; j++ {
				select {
				case <-stop:
					return
				default:
				}
				procSend.Call(f.window.Handle().Hwnd(), uintptr(wmGetObject), 0, objid)
				gpui.A11yProviderState(f.window)
				if _, err := gpui.ProbeNativeA11yAdapter(f.app, handle); err != nil {
					t.Errorf("concurrent is_active: %v", err)
					return
				}
			}
		}(i)
	}

	// The main goroutine drives frame publications and action requests
	// (the actions drain through the publication seam).
	for frame := 0; frame < 5; frame++ {
		f.model.Update(f.app, func(m *a11yModel, cx *gpui.Context[a11yModel]) {
			m.clickRuns++
			cx.Notify()
		})
		if err := gpui.DispatchWindowA11yRequest(f.window, button.ID, gpui.A11yActionClick, nil); err != nil {
			t.Errorf("concurrent action request: %v", err)
		}
		f.drawAndPublish(t)
	}
	close(stop)
	wg.Wait()

	rec, _ = f.provider(t)
	if rec.Err != "" {
		t.Errorf("provider error after the concurrent slice: %s", rec.Err)
	}
	if rec.WMGetObjects < 100 {
		t.Errorf("routed WM_GETOBJECT count = %d, want >= 100 (the concurrent queries)", rec.WMGetObjects)
	}
	if !rec.LastActive || !rec.LastApplied {
		t.Errorf("final publication record active/applied = %v/%v, want true/true", rec.LastActive, rec.LastApplied)
	}
	t.Logf("concurrent slice: wmgetobjects=%d publishes=%d actions-routed=%d", rec.WMGetObjects, rec.Publishes, rec.ActionsRouted)
}

// ---------------------------------------------------------------------------
// The real Narrator evidence test (opt-in)
// ---------------------------------------------------------------------------

// TestNarratorActivationEvidence starts the real Windows Narrator and
// records whether it activates the provider through a real UIA client
// connection. Opt-in (GPUI_A11Y_NARRATOR=1) because a screen reader
// changes global machine state; the honest record is attached to the
// test log either way.
//
// Evidence semantics: the reader performs its UIA sweep of the
// foreground window WHEN IT STARTS, so the real activation evidence is
// captured when no Narrator instance is already running. An
// already-running instance is a protected process (taskkill: access
// denied) that will not re-sweep, and this host's Activate cannot
// overcome the foreground lock to bring the window forward (the
// documented port deviation — no synthetic keyboard injection); those
// runs record the unavailability honestly.
// bootShownA11yFixture is the Narrator evidence boot: the window is
// SHOWN and ACTIVATED so the real reader's foreground sweep can reach
// it (the deterministic corpus keeps its windows hidden).
func bootShownA11yFixture(t *testing.T, title string) *a11yFixture {
	t.Helper()
	host := gpui.NewHost()
	if err := host.Start(); err != nil {
		t.Fatalf("host start: %v — this session is expected to create real windows", err)
	}
	t.Cleanup(func() {
		if err := host.Stop(); err != nil {
			t.Errorf("host stop: %v", err)
		}
	})
	app := gpui.NewApp()
	if err := app.Attach(host); err != nil {
		t.Fatalf("app attach: %v", err)
	}
	var window *gpui.Window
	var model gpui.Entity[a11yModel]
	done := make(chan error, 1)
	if !host.Post(func() {
		defer close(done)
		var err error
		window, err = app.OpenWindowView(gpui.WindowOptions{
			Title:     title,
			Bounds:    gpui.Bounds{Origin: gpui.Point{X: 40, Y: 40}, Size: gpui.Size{Width: 400, Height: 300}},
			Show:      true,
			Activate:  true,
			Resizable: true,
		}, func(w *gpui.Window, app *gpui.App) gpui.View {
			model = newA11yModel(app, w)
			return &a11yRootView{model: model}
		})
		if err != nil {
			done <- err
		}
	}) {
		t.Fatalf("foreground post was dropped: the host loop exited")
	}
	if err := <-done; err != nil {
		t.Fatalf("OpenWindowView(%q) failed — this session is expected to create real windows: %v", title, err)
	}
	t.Cleanup(func() {
		if window.Alive() {
			window.Close()
			waitFor(t, 10*time.Second, "the window to be destroyed", func() bool { return !window.Alive() })
		}
	})
	return &a11yFixture{app: app, host: host, window: window, model: model}
}

func TestA11yNarratorActivationEvidence(t *testing.T) {
	if os.Getenv("GPUI_A11Y_NARRATOR") != "1" {
		t.Skip("opt-in evidence test: set GPUI_A11Y_NARRATOR=1 to run the real Narrator session")
	}
	f := bootShownA11yFixture(t, "Narrator Evidence")
	gpui.ActivateWindowA11y(f.window)
	f.drawAndPublish(t)

	narratorPath := filepath.Join(os.Getenv("SystemRoot"), "System32", "Narrator.exe")
	if _, err := os.Stat(narratorPath); err != nil {
		t.Fatalf("narrator.exe unavailable at %s: %v (honest record: no real Narrator session)", narratorPath, err)
	}
	// Narrator is a GUI application: Start launches it detached.
	if err := startDetached(narratorPath); err != nil {
		t.Fatalf("starting narrator.exe: %v (honest record: no real Narrator session)", err)
	}
	defer func() {
		// Best-effort cleanup; the evidence is already recorded. Narrator
		// is a protected process: a non-elevated shell CANNOT terminate it
		// (taskkill reports access denied), so the reader may stay
		// running for the session — recorded honestly by the log.
		if out, err := stopNarrator(); err != nil {
			t.Logf("stopping Narrator: %v: %s (protected process; left running)", err, strings.TrimSpace(string(out)))
		}
	}()

	// Give Narrator time to attach (it reads the FOREGROUND window's
	// UIA tree; re-asserting this window's activation during the wait
	// makes the evidence robust whether Narrator was just started or is
	// already running from an earlier session).
	deadline := time.Now().Add(20 * time.Second)
	activated := false
	for time.Now().Before(deadline) {
		if rec, ok := f.provider(t); ok && rec.Activated {
			activated = true
			break
		}
		f.window.Activate()
		time.Sleep(250 * time.Millisecond)
	}
	// One frame published after the real activation: the complete tree
	// reaches the live UIA client.
	f.drawAndPublish(t)
	rec, _ := f.provider(t)
	foreground, _, _ := procGetFg.Call()
	t.Logf("narrator evidence: activated=%v active=%v wmgetobjects=%d publishes=%d applied=%v events-raised=%v foreground-ours=%v (foreground=%#x ourshwnd=%#x)",
		activated, rec.Active, rec.WMGetObjects, rec.Publishes, rec.LastApplied, rec.LastEventsRaised,
		foreground == f.window.Handle().Hwnd(), foreground, f.window.Handle().Hwnd())
	if !activated {
		// Honest record: Narrator ran but did not connect to this
		// window within the window (it may follow a different foreground
		// window on this interactive session).
		t.Logf("Narrator did not query this window within 20s (interactive-session variance); the activation evidence is unavailable in this run")
	}
}

// startDetached launches a GUI executable detached from the test
// process. Narrator.exe refuses direct fork/exec (it manifests an
// elevation requirement when launched as a child), so the launch goes
// through the shell (cmd /c start, the App Paths resolution) — the
// same path a user's Run dialog takes.
func startDetached(path string) error {
	cmd := exec.Command("cmd", "/c", "start", "", "Narrator")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("cmd start Narrator: %v: %s (direct path %s)", err, strings.TrimSpace(string(out)), path)
	}
	return nil
}

// stopNarrator terminates Narrator best-effort (the session's screen
// reader). It reports the command's outcome: Narrator is a protected
// process and a non-elevated shell's taskkill is denied.
func stopNarrator() ([]byte, error) {
	out, err := exec.Command("taskkill", "/F", "/IM", "Narrator.exe").CombinedOutput()
	return out, err
}
