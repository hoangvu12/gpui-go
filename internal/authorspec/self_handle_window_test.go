//go:build windows

package authorspec

// The consumer-gap 05 acceptance on a REAL window
// (.scratch/gpui-consumer-gaps/issues/05-strong-self-entity-handle.md):
// a view whose OnClick listener updates the view through the handle it
// captured in Render, with the click delivered after Render returned,
// through the real Win32 input path (a synthetic WM_LBUTTONDOWN/UP pair
// through SendMessageW on the host thread — the win32input corpus's
// delivery shape; no real mouse is moved), and the ownership contract's
// release semantics intact: the self-handle lease is the window's, so
// closing the window releases the view and the leak detector stays
// green.
//
// The click travels the whole platform path: the window procedure's
// translation, the mouse input callback OpenWindow installs
// (window.dispatchInput inside an update), the element listeners, and
// the frame loop's redraw of the notified window.

import (
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Win32 bindings for the controlled-event delivery (tests bring their
// own; the package under test never exposes them — the desktopspec
// convention).
var (
	selfModUser32 = syscall.NewLazyDLL("user32.dll")
	selfProcSend  = selfModUser32.NewProc("SendMessageW")
)

// Message ids and key state of the synthetic click (winuser.h).
const (
	selfWMLButtonDown = 0x0201 // WM_LBUTTONDOWN
	selfWMLButtonUp   = 0x0202 // WM_LBUTTONUP
	selfMKLButton     = 0x0001 // MK_LBUTTON
)

// selfLParam packs client device-pixel coordinates like the system
// (MAKELPARAM with signed words).
func selfLParam(x, y int16) uintptr {
	return uintptr(uint16(x)) | uintptr(uint16(y))<<16
}

// selfClickView is the consumer pattern's view: Render captures the
// strong self-handle and the increment div's OnClick listener updates
// the view through that handle — the exact pattern that panicked with
// the borrowed cx.Entity() facade before this ticket.
type selfClickView struct {
	count   int
	clicks  int
	renders int
	// self is the strong self-handle read in Render (a facade over the
	// window-owned lease; storing it in the view's own state creates no
	// cycle).
	self gpui.Entity[selfClickView]
}

// Render implements gpui.Render[selfClickView]: the counter tree with
// the increment div whose click listener updates the view through the
// handle captured during this render.
func (v *selfClickView) Render(w *gpui.Window, cx *gpui.Context[selfClickView]) gpui.AnyElement {
	v.renders++
	v.self = cx.StrongSelf()
	self := v.self
	return gpui.Div().
		ID("self-root").
		Flex().
		SizeFull().
		Child(gpui.Div().
			ID("increment").
			Flex().
			SizeFull().
			OnClick(func(_ *gpui.ClickEvent, _ *gpui.Window, app *gpui.App) {
				self.Update(app, func(v *selfClickView, cx *gpui.Context[selfClickView]) {
					v.clicks++
					v.count++
					cx.Notify()
				})
			}).
			Child("Increment")).
		IntoElement()
}

// selfClickApp is the real-application fixture: a started host with no
// windows yet (each test opens its own), then one window with the
// self-click view and a second window that keeps the message loop alive
// across the first window's close (the release assertions run on the
// foreground thread afterwards).
type selfClickApp struct {
	app     *gpui.App
	host    *gpui.Host
	windowA *gpui.Window
	windowB *gpui.Window
	view    gpui.Entity[selfClickView]
	scale   float32
	// released records the view's release hook.
	released atomic.Bool
	// sync runs f on the foreground (host) thread.
	sync func(f func())
}

// bootSelfClickApp boots the real application (host started, run loop
// draining) without opening windows.
func bootSelfClickApp(t *testing.T) *selfClickApp {
	t.Helper()
	app := gpui.NewApp()
	host := gpui.NewHost()
	if err := app.Attach(host); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if err := host.Start(); err != nil {
		t.Fatalf("host start: %v", err)
	}
	runDone := make(chan error, 1)
	go func() { runDone <- app.Run(host) }()
	t.Cleanup(func() {
		if err := host.Stop(); err != nil {
			t.Errorf("host stop: %v", err)
		}
		<-runDone
	})

	state := &selfClickApp{app: app, host: host}
	state.sync = func(f func()) {
		done := make(chan struct{})
		if !host.Post(func() {
			defer close(done)
			f()
		}) {
			t.Fatalf("foreground post was dropped: the host loop exited")
		}
		<-done
	}
	return state
}

// openWindows opens the two windows: window A carries the self-click
// view, window B (hidden) keeps the message loop alive across A's
// close. The view is built the natural way (NewEntity in the window's
// scope, ViewOf in the OpenWindowView build), which this ticket aligns
// with the reference: the window owns the root placement's lease
// (open_window's window_update_stack boundary).
func (state *selfClickApp) openWindows(t *testing.T) {
	t.Helper()
	state.sync(func() {
		var openErr error
		state.windowA, openErr = state.app.OpenWindowView(gpui.WindowOptions{
			Title:     "authorspec self-handle A",
			Show:      true,
			Resizable: true,
			Bounds:    gpui.Bounds{Size: gpui.Size{Width: 360, Height: 240}},
		}, func(w *gpui.Window, app *gpui.App) gpui.View {
			state.view = gpui.NewEntity(app, w.Scope(), func(v *selfClickView, cx *gpui.Context[selfClickView]) {
				cx.OnRelease(func(_ *selfClickView, _ *gpui.App) {
					state.released.Store(true)
				}).Detach()
			})
			return gpui.ViewOf(state.view)
		})
		if openErr != nil {
			t.Fatalf("OpenWindowView A: %v — this session is expected to create real windows", openErr)
		}
		state.windowB, openErr = state.app.OpenWindowView(gpui.WindowOptions{
			Title:  "authorspec self-handle B",
			Show:   false,
			Bounds: gpui.Bounds{Size: gpui.Size{Width: 200, Height: 120}},
		}, func(_ *gpui.Window, _ *gpui.App) gpui.View {
			return selfKeeperView{}
		})
		if openErr != nil {
			t.Fatalf("OpenWindowView B: %v — this session is expected to create real windows", openErr)
		}
	})
	t.Cleanup(func() {
		for _, window := range []*gpui.Window{state.windowA, state.windowB} {
			if window != nil && window.Alive() {
				window.Close()
				waitSelfCond(t, 10*time.Second, "the window to be destroyed", func() bool { return !window.Alive() })
			}
		}
	})
	scale, err := state.windowA.ScaleFactor()
	if err != nil || scale <= 0 {
		t.Fatalf("ScaleFactor = %v, %v", scale, err)
	}
	state.scale = scale
}

// selfKeeperView is window B's stateless root (no entity).
type selfKeeperView struct{}

// EntityID implements gpui.View: the keeper carries no identity.
func (selfKeeperView) EntityID() (gpui.EntityID, bool) { return 0, false }

// RenderOnce implements gpui.View.
func (selfKeeperView) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return gpui.Div().Flex().ID("keeper").SizeFull().IntoElement()
}

// clickAt delivers a synthetic left click at the window-local logical
// position through the real Win32 path (press then release, on the host
// thread).
func (state *selfClickApp) clickAt(t *testing.T, x, y float32) {
	t.Helper()
	lparam := selfLParam(int16(x*state.scale+0.5), int16(y*state.scale+0.5))
	state.sync(func() {
		// SendMessageW's Call always reports a pseudo-error (GetLastError
		// 0); the result is the message's LRESULT.
		selfProcSend.Call(state.windowA.Handle().Hwnd(), selfWMLButtonDown, selfMKLButton, lparam)
		selfProcSend.Call(state.windowA.Handle().Hwnd(), selfWMLButtonUp, 0, lparam)
	})
}

// facts reads the view's counters on the foreground thread.
func (state *selfClickApp) facts() (count, clicks, renders int) {
	type triple struct{ count, clicks, renders int }
	state.sync(func() {
		got := state.view.Read(state.app, func(v *selfClickView, _ *gpui.App) triple {
			return triple{count: v.count, clicks: v.clicks, renders: v.renders}
		})
		count, clicks, renders = got.count, got.clicks, got.renders
	})
	return
}

// draws reads window A's draw count.
func (state *selfClickApp) draws() uint64 {
	var n uint64
	state.sync(func() { n = gpui.WindowDrawCount(state.windowA) })
	return n
}

// TestRealWindowStrongSelfHandleClick is the ticket's first acceptance
// criterion: a view whose OnClick listener updates the view through the
// handle it captured in Render works when the click fires after Render
// returns, in a real window.
func TestRealWindowStrongSelfHandleClick(t *testing.T) {
	state := bootSelfClickApp(t)
	state.openWindows(t)

	// The opening draw rendered the view once and captured the
	// self-handle (a frame completed between the render and the click).
	if count, clicks, renders := state.facts(); count != 0 || clicks != 0 || renders != 1 {
		t.Fatalf("after the opening draw: count = %d, clicks = %d, renders = %d; want 0/0/1", count, clicks, renders)
	}
	openingDraws := state.draws()

	// The click, after Render returned: the listener updates the view
	// through the render-captured handle (the pattern that panicked with
	// the borrowed cx.Entity() facade), the notify redraws the window.
	state.clickAt(t, 50, 50)
	waitSelfCond(t, 10*time.Second, "the notified window to redraw", func() bool {
		return state.draws() > openingDraws
	})
	count, clicks, renders := state.facts()
	if count != 1 || clicks != 1 {
		t.Fatalf("after the click: count = %d, clicks = %d; want 1/1 (the listener updated the view through the handle captured in Render)", count, clicks)
	}
	if renders < 2 {
		t.Fatalf("renders after the click = %d, want >= 2 (the window redrew with the new count)", renders)
	}

	// A second click through the same handle: each render's listener
	// captures an alias of the one window-owned lease.
	drawsBefore := state.draws()
	state.clickAt(t, 120, 80)
	waitSelfCond(t, 10*time.Second, "the window to redraw again", func() bool {
		return state.draws() > drawsBefore
	})
	if count, clicks, _ := state.facts(); count != 2 || clicks != 2 {
		t.Fatalf("after the second click: count = %d, clicks = %d; want 2/2", count, clicks)
	}
}

// TestRealWindowStrongSelfHandleRelease is the ticket's second
// acceptance criterion: the ownership contract's release semantics stay
// intact — the self-handle lease belongs to the rendering window's
// scope, so closing the window releases the view (construction,
// placement and self-handle leases together), and the leak detector
// reports no new leaks.
func TestRealWindowStrongSelfHandleRelease(t *testing.T) {
	state := bootSelfClickApp(t)

	// The snapshot predates the view: any live entity afterwards is a
	// leak. (Taken on the foreground thread like the detector itself.)
	var snapshot *gpui.LeakDetectorSnapshot
	state.sync(func() { snapshot = state.app.LeakDetectorSnapshot() })
	if snapshot.EntityCount() != 0 {
		t.Fatalf("a freshly booted app snapshot has %d entities, want 0", snapshot.EntityCount())
	}

	state.openWindows(t)

	// Clicks through the self-handle, then close window A: its scope
	// releases the view's construction, placement and self-handle
	// leases; the flush at the update boundary runs the release hook.
	state.clickAt(t, 50, 50)
	waitSelfCond(t, 10*time.Second, "the notified window to redraw", func() bool { return state.draws() >= 2 })
	viewID := state.view.EntityID()

	state.windowA.Close()
	waitSelfCond(t, 10*time.Second, "window A to be destroyed", func() bool { return !state.windowA.Alive() })
	waitSelfCond(t, 10*time.Second, "the view to release with its window", func() bool { return state.released.Load() })

	var releasedLines, leakLines []string
	state.sync(func() {
		releasedLines = state.app.AssertEntityReleased(viewID)
		leakLines = state.app.AssertNoNewLeaks(snapshot)
	})
	if len(releasedLines) > 0 {
		t.Errorf("the closed window's view still has leases:\n%s", strings.Join(releasedLines, "\n"))
	}
	if len(leakLines) > 0 {
		t.Errorf("window closure leaked entities:\n%s", strings.Join(leakLines, "\n"))
	}

	// Window B keeps the loop alive and stays healthy after A's close
	// (independent placement, independent lifetime).
	if !state.windowB.Alive() {
		t.Fatal("window B must survive window A's close")
	}
}

// waitSelfCond waits for a condition with a timeout.
func waitSelfCond(t *testing.T, timeout time.Duration, what string, cond func() bool) {
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
