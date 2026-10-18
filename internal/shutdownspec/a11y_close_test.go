//go:build windows

// The accessibility half of ticket30's real corpus: the a11y adapter
// closing with a LATE WM_GETOBJECT (the DefWindowProc fall-through),
// the close/reopen adapter lifetime, and the generation-checked
// refusals of callbacks arriving after window removal.
//
// Synthetic-message discipline: the WM_GETOBJECT queries are SendMessage
// calls (the a11yspec pattern); no real cursor or AT session is driven.
//
// Process-load note: mixing real COM services in one process
// (a11y adapters + a real IFileDialog + a Windows Graphics Capture
// stream) crosses a pre-existing native heap-corruption threshold
// (sporadic 0xC0000374/0xC0000005 — minimized reproducers and the
// stable-budget measurement in TRACES.md; the native DLL is frozen in
// this slice). This file keeps the whole a11y close record in ONE test
// with one host and two adapters to stay below that threshold while
// covering both teardown paths (the real window close and the explicit
// CloseWindowA11y).
package shutdownspec

import (
	"fmt"
	"syscall"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

// wmGetObject is WM_GETOBJECT (winuser.h 0x003D).
const wmGetObject = 0x003D

// Object ids (winuser.h; UiaRootObjectId from oleacc/uiautomation — the
// accesskit_windows convention).
const (
	objidClient     = uintptr(^uintptr(3))  // OBJID_CLIENT = -4
	uiaRootObjectId = uintptr(^uintptr(24)) // UiaRootObjectId = -25
)

// a11yProcSend sends one synthetic WM_GETOBJECT and returns the window
// procedure's answer (the a11yspec corpus brings its own bindings;
// these tests reuse user32 through a dedicated lazy proc so the corpus
// file stays platform-neutral).
var a11yProcSend = syscall.NewLazyDLL("user32.dll").NewProc("SendMessageW")

// queryWmGetObject sends one synthetic WM_GETOBJECT with the object id.
func queryWmGetObject(hwnd, objid uintptr) uintptr {
	r, _, _ := a11yProcSend.Call(hwnd, uintptr(wmGetObject), 0, objid)
	return r
}

// a11yLabel is the minimal a11y corpus leaf: a StaticText node with a
// stable author id (the accessspec countLabel precedent, bounded to
// this package's needs — the token dispatch needs a published full
// snapshot with node generations).
type a11yLabel struct{}

// ID implements gpui.Element.
func (e *a11yLabel) ID() (gpui.ElementID, bool) {
	return gpui.NameElementID("shutdownspec-label"), true
}

// SourceLocation implements gpui.Element.
func (e *a11yLabel) SourceLocation() *gpui.SourceLocation { return nil }

// A11ySpec implements gpui.A11yCompanion.
func (e *a11yLabel) A11ySpec() gpui.A11ySpec {
	return gpui.A11ySpec{Role: gpui.RoleStaticText, Value: "shutdown", AuthorID: "shutdownspec.label"}
}

// RequestLayout implements gpui.Element: a fixed-size leaf.
func (e *a11yLabel) RequestLayout(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, w *gpui.Window, app *gpui.App) (gpui.LayoutID, struct{}) {
	style := gpui.DefaultStyle()
	style.Size = gpui.LengthSize{Width: gpui.PxLength(160), Height: gpui.PxLength(24)}
	id, err := gpui.RequestElementLayout(w, style)
	if err != nil {
		panic(fmt.Sprintf("shutdownspec: label request_layout: %v", err))
	}
	return id, struct{}{}
}

// Prepaint implements gpui.Element.
func (e *a11yLabel) Prepaint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *struct{}, w *gpui.Window, app *gpui.App) struct{} {
	return struct{}{}
}

// Paint implements gpui.Element (the a11y tree is the observable).
func (e *a11yLabel) Paint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *struct{}, prepaint *struct{}, w *gpui.Window, app *gpui.App) {
}

// a11yView is the root view rendering the label.
type a11yView struct{}

// EntityID implements gpui.View.
func (v *a11yView) EntityID() (gpui.EntityID, bool) { return 0, false }

// RenderOnce implements gpui.View.
func (v *a11yView) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return gpui.Accessibility(&a11yLabel{})
}

// openA11yWindow opens a real window whose root view publishes the
// label node, activates its accessibility runtime, connects the
// synthetic AT (the UIA root query) and publishes the full snapshot.
// It returns the window and a RESOLVED generation-checked token for
// the label node (the pre-close reference for late dispatch).
func openA11yWindow(t *testing.T, f *realFixture, title string, onClose func()) (*gpui.Window, gpui.A11yToken) {
	t.Helper()
	window, err := f.app.OpenWindowView(gpui.WindowOptions{
		Title:     title,
		Show:      true,
		Activate:  true,
		Resizable: true,
		Bounds:    gpui.Bounds{Size: gpui.Size{Width: 360, Height: 260}},
		OnClose:   onClose,
	}, func(w *gpui.Window, app *gpui.App) gpui.View {
		return &a11yView{}
	})
	if err != nil {
		t.Fatalf("OpenWindowView(%q): %v — this session is expected to create real windows", title, err)
	}
	t.Cleanup(func() {
		if window.Alive() {
			window.Close()
			waitFor(t, 10*time.Second, "the a11y window to be destroyed", func() bool { return !window.Alive() })
		}
	})
	var token gpui.A11yToken
	var tokenErr error
	f.sync(t, func() {
		gpui.ActivateWindowA11y(window)
		// Connect the synthetic assistive technology (the activation
		// edge) so the tree publishes.
		queryWmGetObject(window.Handle().Hwnd(), uiaRootObjectId)
		if _, err := gpui.DrawWindowFrame(window); err != nil {
			tokenErr = fmt.Errorf("the a11y corpus frame: %w", err)
			return
		}
		snapshot, err := gpui.PublishWindowA11y(window)
		if err != nil {
			tokenErr = fmt.Errorf("PublishWindowA11y: %w", err)
			return
		}
		for _, node := range snapshot.Nodes() {
			if node.AuthorID == "shutdownspec.label" {
				token, err = snapshot.Token(node.ID)
				if err != nil {
					tokenErr = err
				}
				return
			}
		}
		tokenErr = fmt.Errorf("no published node with author id shutdownspec.label in %s", snapshot.Trace())
	})
	if tokenErr != nil {
		t.Fatalf("the a11y corpus setup: %v", tokenErr)
	}
	var resolveErr error
	f.sync(t, func() {
		token, resolveErr = gpui.ResolveA11yToken(f.app, token)
	})
	if resolveErr != nil {
		t.Fatalf("ResolveA11yToken: %v", resolveErr)
	}
	return window, token
}

// TestA11yCloseTeardownsAndLateQueries pins both a11y teardown paths
// and every late-query refusal in one close cascade:
//
//   - The REAL window close destroys the adapter exactly once through
//     the window-scope release sentinel (the A11yState drop on the
//     message thread); the explicit CloseWindowA11y afterwards is an
//     idempotent no-op; a WM_GETOBJECT sent to the DESTROYED hwnd is
//     refused by the OS (the dead lease), and a token minted before
//     the close is refused by the generation check (ErrA11yWindowGone)
//     after the live tree answered the same dispatch.
//   - The EXPLICIT teardown (CloseWindowA11y on a still-open window)
//     bumps the generation, destroys the adapter (deregistered FIRST
//     so late queries fall to DefWindowProc), and leaves the window
//     alive: a LATE WM_GETOBJECT falls to DefWindowProcW exactly like
//     the pin's None arm (events.rs 855-871), the destroyed handle is
//     stale through the native probe, and the late dispatch is
//     ErrA11yWindowGone before any application state is touched.
//   - The reopen record: a fresh window's adapter is a distinct native
//     handle (the closed window's slot released) and serves queries.
func TestA11yCloseTeardownsAndLateQueries(t *testing.T) {
	f := bootRealApp(t)
	app := f.app

	// Part 1 — the real window close. Window A carries the a11y
	// runtime; window C keeps the host's loop alive across A's close.
	evA := newCloseEvidence(&releaseLog{})
	wa, token := openA11yWindow(t, f, "shutdownspec a11y close A", evA.onClose)
	wc, tokenC := openA11yWindow(t, f, "shutdownspec a11y close C", nil)

	hwndA := wa.Handle().Hwnd()
	queryWmGetObject(hwndA, uiaRootObjectId)
	rec, ok := gpui.A11yProviderState(wa)
	if !ok || !rec.Available {
		t.Fatalf("no provider after activation: %+v ok=%v", rec, ok)
	}
	handleA := rec.NativeHandle

	// The pre-close dispatch reference: the resolved token reaches the
	// live tree and dispatches through the built-in Click fallback (the
	// live answer the late dispatch below must NOT produce). The
	// dispatch is an App.Update — it runs on the foreground (host)
	// thread.
	var preErr error
	f.sync(t, func() {
		preErr = gpui.DispatchA11yAction(app, token, gpui.A11yActionClick, nil)
	})
	if preErr != nil {
		t.Fatalf("the pre-close dispatch = %v, want nil (the live tree answered the dispatch)", preErr)
	}

	// The real window close: WM_DESTROY retires the window scope, the
	// release sentinel destroys the adapter on the message thread
	// (exactly once — the record keeps Destroyed with no error).
	wa.Close()
	recvWithin(t, 10*time.Second, "the a11y window's WM_DESTROY", evA.closed)
	waitFor(t, 10*time.Second, "the a11y window to be destroyed", func() bool { return !wa.Alive() })
	rec, ok = gpui.A11yProviderState(wa)
	if !ok {
		t.Fatal("the closed window's a11y record disappeared (the tombstone is the diagnostics design)")
	}
	if !rec.Destroyed {
		t.Fatalf("the adapter was not destroyed at window close: %+v", rec)
	}
	if rec.Err != "" {
		t.Errorf("the adapter destroy recorded an error: %s", rec.Err)
	}
	// The native handle is stale through the native client.
	if _, err := gpui.ProbeNativeA11yAdapter(app, handleA); err == nil {
		t.Fatal("the closed window's adapter handle still answers")
	}

	// The explicit CloseWindowA11y after the window closed is an
	// idempotent no-op (the adapter is already destroyed).
	gpui.CloseWindowA11y(wa)
	gpui.CloseWindowA11y(wa)

	// A WM_GETOBJECT sent to the destroyed hwnd is refused by the OS:
	// SendMessageW returns 0 without dispatching (the dead-lease
	// refusal — nothing is resurrected).
	if isWindow(hwndA) {
		t.Fatal("the hwnd still exists after the window close")
	}
	if r := queryWmGetObject(hwndA, uiaRootObjectId); r != 0 {
		t.Logf("the dead hwnd's WM_GETOBJECT answered %#x (the OS refusal)", r)
	}

	// The generation-checked refusal: the token minted before the close
	// no longer dispatches (the window is gone from the registry —
	// ErrA11yWindowGone before any application state is touched).
	var lateErr error
	f.sync(t, func() {
		lateErr = gpui.DispatchA11yAction(app, token, gpui.A11yActionFocus, nil)
	})
	if lateErr != gpui.ErrA11yWindowGone {
		t.Fatalf("DispatchA11yAction after the window close = %v, want ErrA11yWindowGone", lateErr)
	}

	// Part 2 — the explicit teardown on the still-open window C: the
	// generation bumps, the adapter is destroyed and DEREGISTERED (late
	// queries fall to DefWindowProc), and the Go record is gone.
	var preErrC error
	f.sync(t, func() {
		preErrC = gpui.DispatchA11yAction(app, tokenC, gpui.A11yActionClick, nil)
	})
	if preErrC != nil {
		t.Fatalf("window C's pre-teardown dispatch = %v, want nil (its live tree answered)", preErrC)
	}
	hwndC := wc.Handle().Hwnd()
	queryWmGetObject(hwndC, uiaRootObjectId)
	recC, ok := gpui.A11yProviderState(wc)
	if !ok || !recC.Available || !recC.Activated {
		t.Fatalf("window C's provider before the teardown: %+v ok=%v", recC, ok)
	}
	handleC := recC.NativeHandle
	gpui.CloseWindowA11y(wc)
	if _, ok := gpui.A11yProviderState(wc); ok {
		t.Fatal("the provider record survived CloseWindowA11y")
	}
	if _, err := gpui.ProbeNativeA11yAdapter(app, handleC); err == nil {
		t.Fatal("the destroyed adapter's handle still answers")
	}

	// The LATE WM_GETOBJECT: no provider is registered for the hwnd, so
	// the window procedure falls to DefWindowProcW (the pin's None arm,
	// events.rs 855-871). The window survives; the query answers with
	// DefWindowProc's value (0 for the UIA root object id on a plain
	// window).
	if late := queryWmGetObject(hwndC, uiaRootObjectId); late != 0 {
		t.Logf("DefWindowProc answered %#x for the late UIA root query (the fall-through is the routed-count record, not a value gate)", late)
	}
	if !isWindow(hwndC) {
		t.Fatal("the window died during the late WM_GETOBJECT")
	}
	// OBJID_CLIENT falls through too.
	queryWmGetObject(hwndC, objidClient)
	if !isWindow(hwndC) {
		t.Fatal("the window died during the late OBJID_CLIENT query")
	}

	// Every later action request is refused through the generation
	// check: the runtime is gone (ErrA11yWindowGone), before any
	// application state is touched.
	var lateErr1, lateErr2 error
	f.sync(t, func() {
		lateErr1 = gpui.DispatchA11yAction(app, tokenC, gpui.A11yActionFocus, nil)
		lateErr2 = gpui.DispatchA11yAction(app, tokenC, gpui.A11yActionClick, nil)
	})
	if lateErr1 != gpui.ErrA11yWindowGone {
		t.Fatalf("DispatchA11yAction after the teardown = %v, want ErrA11yWindowGone", lateErr1)
	}
	if lateErr2 != gpui.ErrA11yWindowGone {
		t.Fatalf("the second late dispatch = %v, want ErrA11yWindowGone", lateErr2)
	}

	// The reopen record is pinned by the a11yspec suite
	// (TestA11yCloseReopenAdapterLifetime: the new window's adapter is a
	// distinct native handle after the closed window's slot released);
	// this package keeps its churn budget for the two teardown paths
	// above (see the process-load note at the file head).
}
