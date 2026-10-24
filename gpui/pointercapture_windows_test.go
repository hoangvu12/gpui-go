//go:build windows

package gpui

// Browser-core ticket 11's real-window gate for the capture change: on
// a real Win32 window placed far off screen (never visible to the user,
// the capture-host pattern at a positive off-screen origin), a
// synthetic WM_LBUTTONDOWN presses and captures (the window's own
// SetCapture runs through the real message handler), a synthetic
// WM_MOUSEMOVE outside the element fires gotpointercapture while the
// retargeted dispatch keeps the press alive, and the WM_CAPTURECHANGED
// a foreign window's takeover delivers (dispatched here synthetically
// with the foreign handle in lParam, exactly the message the system
// sends when another window calls SetCapture) fires lostpointercapture,
// clears the pressed style, drops the pending press (the later
// WM_LBUTTONUP clicks nothing) and clears the held-button tracking (the
// OS capture releases cleanly afterwards — no button stuck). No real
// input is injected and the real cursor is never moved; a
// window-creation failure FAILS the test.
//
// Chromium's rule this gates (research browser-parity 03 §5, verified
// in %TEMP%\research-src\chromium @ 0d4dcd4f): a pointercancel —
// "the user agent MUST suppress a pointer event stream" — releases the
// capture (:1244-1246), clears the press state and ends the drag
// without boundary events (:1316-1321, "Don't send boundary events in
// this case as it is a little tricky").

import (
	"sync"
	"testing"
)

// foreignCaptureWindow is the fake foreign hwnd the synthetic
// WM_CAPTURECHANGED carries: any non-null handle other than the
// window's own.
const foreignCaptureWindow = uintptr(0x1234ABCD)

// captureChangeWindow is the gate fixture: a real window off screen
// whose root view renders one press-capturing, active-styled button,
// with a log of the capture and click events its listeners observed.
type captureChangeWindow struct {
	t      *testing.T
	h      *Host
	app    *App
	window *Window
	scale  float32

	// sending is true while the test's own SendMessageW runs (host
	// thread only): the OnMouseInput interceptor consumes every other
	// mouse event (the win32 input gate pattern, hoverboundary_windows_
	// test.go) — the system synthesizes moves at show time and the armed
	// TrackMouseEvent may deliver a real WM_MOUSELEAVE — so only the
	// synthetic messages reach element dispatch.
	sending bool

	mu  sync.Mutex
	log []string
}

func (f *captureChangeWindow) add(entry string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log = append(f.log, entry)
}

func (f *captureChangeWindow) takeLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.log
	f.log = nil
	return out
}

// logLen reports the log's length without draining it.
func (f *captureChangeWindow) logLen() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.log)
}

// openCaptureChangeWindow opens the off-screen gate window.
func openCaptureChangeWindow(t *testing.T) *captureChangeWindow {
	t.Helper()
	h := desktopNewHost(t)
	app := NewApp()
	if err := app.Attach(h); err != nil {
		t.Fatalf("app attach: %v", err)
	}
	f := &captureChangeWindow{t: t, h: h, app: app}
	window, err := app.OpenWindowView(WindowOptions{
		Title: "capture change (offscreen)",
		Bounds: Bounds{
			Origin: offscreenWindowOrigin,
			Size:   Size{Width: 400, Height: 300},
		},
		Show: true,
		OnMouseInput: func(event any) bool {
			if !f.sending {
				return true // a foreign event: consume it
			}
			return false
		},
	}, func(w *Window, app *App) View {
		return ViewOf(NewEntity(app, w.Scope(), func(v *interactView, _ *Context[interactView]) {
			v.build = func(v *interactView, w *Window, cx *Context[interactView]) AnyElement {
				return Div().ID("root").SizeFull().Relative().Child(
					Div().ID("button").
						Absolute().Left(Px(20)).Top(Px(20)).Size(Px(80)).
						Bg(interactRed).
						Active(func(s StyleRefinement) StyleRefinement {
							return Div().Refine(s).Bg(interactBlue).Style()
						}).
						OnMouseDownAll(func(e *MouseDownEvent, phase DispatchPhase, hitbox *Hitbox, w *Window, app *App) {
							if phase == DispatchBubble && e.Button == MouseButtonLeft && hitbox.IsHovered(w) {
								w.CapturePointer(hitbox.ID())
							}
						}).
						OnGotPointerCapture(func(e *GotPointerCaptureEvent, w *Window, app *App) {
							f.add("got:button")
						}).
						OnLostPointerCapture(func(e *LostPointerCaptureEvent, w *Window, app *App) {
							f.add("lost:button")
						}).
						OnClick(func(*ClickEvent, *Window, *App) { f.add("click:button") }).
						IntoElement(),
				).IntoElement()
			}
		}))
	})
	if err != nil {
		t.Fatalf("OpenWindowView failed (real window expected): %v", err)
	}
	f.window = window
	t.Cleanup(func() {
		if window.Alive() {
			window.Close()
			desktopWaitFor(t, "the window to be destroyed", func() bool { return !window.Alive() })
		}
	})
	desktopWaitFor(t, "the window to be visible", window.Handle().Visible)
	if !h.runForegroundSync(func() {
		f.scale, _ = window.ScaleFactor()
	}) {
		t.Fatal("the host stopped before reading the scale")
	}
	if f.scale <= 0 {
		t.Fatalf("scale factor = %v", f.scale)
	}
	return f
}

// onHost runs fn on the host thread.
func (f *captureChangeWindow) onHost(fn func()) {
	f.t.Helper()
	if !f.h.runForegroundSync(fn) {
		f.t.Fatal("the host stopped")
	}
}

// send delivers one synthetic message to the window with SendMessageW
// on the host thread.
func (f *captureChangeWindow) send(message uint32, wparam, lparam uintptr) {
	f.t.Helper()
	f.onHost(func() {
		f.sending = true
		defer func() { f.sending = false }()
		_, _, _ = procSendMessageW.Call(f.window.Handle().Hwnd(), uintptr(message), wparam, lparam)
	})
}

// at packs a window-local logical point as the client device-pixel
// lparam of the mouse messages.
func (f *captureChangeWindow) at(x, y float32) uintptr {
	return buttonLParam(int16(x*f.scale+0.5), int16(y*f.scale+0.5))
}

// colors counts the button quads of the last drawn scene on the host
// thread.
func (f *captureChangeWindow) colors() (red, blue int) {
	f.onHost(func() {
		red, blue = sceneQuadColorCount(f.window, interactRed), sceneQuadColorCount(f.window, interactBlue)
	})
	return red, blue
}

// draws reads the window's draw count.
func (f *captureChangeWindow) draws() (n uint64) {
	f.onHost(func() { n = WindowDrawCount(f.window) })
	return n
}

// osCapture reports the hwnd that holds the OS mouse capture.
func (f *captureChangeWindow) osCapture() (hwnd uintptr) {
	f.onHost(func() { hwnd, _, _ = procGetCapture.Call() })
	return hwnd
}

// wantLog asserts the exact event log.
func (f *captureChangeWindow) wantLog(what string, want ...string) {
	f.t.Helper()
	got := f.takeLog()
	if len(got) != len(want) {
		f.t.Fatalf("%s: events = %v, want %v", what, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			f.t.Fatalf("%s: events = %v, want %v", what, got, want)
		}
	}
}

// waitFor draws the wait out of desktopWaitFor with a color predicate.
func (f *captureChangeWindow) waitForColors(what string, before uint64, wantRed, wantBlue int) {
	f.t.Helper()
	desktopWaitFor(f.t, what, func() bool {
		if f.draws() <= before {
			return false
		}
		red, blue := f.colors()
		return red == wantRed && blue == wantBlue
	})
}

// TestWin32CaptureChangedCancelsCleanlyOnOffscreenWindow drives the
// whole WM_CAPTURECHANGED path on a real window: the foreign takeover
// mid-press dispatches a PointerCancelEvent through the mouse input
// callback into element dispatch, which fires lostpointercapture,
// clears the pressed style, drops the pending press and clears the
// held-button tracking — the release that arrives afterwards clicks
// nothing, and the OS capture releases with it (no button stuck). A
// fresh press afterwards works end to end.
func TestWin32CaptureChangedCancelsCleanlyOnOffscreenWindow(t *testing.T) {
	f := openCaptureChangeWindow(t)

	// The un-pressed opening frame.
	if red, blue := f.colors(); red != 1 || blue != 0 {
		t.Fatalf("opening colors red %d blue %d, want the un-pressed button", red, blue)
	}

	// A move over the button sets the hit test, then the press: the
	// button's active style applies (the pressed state), the window
	// takes the OS capture (handleMouseDownMsg's SetCapture) and the
	// capture request goes pending.
	f.send(wmMouseMove, 0, f.at(50, 50))
	f.send(wmMouseMove, 0, f.at(50, 50))
	before := f.draws()
	f.send(wmLButtonDown, mkLButton, f.at(50, 50))
	f.waitForColors("the pressed button to be repainted", before, 0, 1)
	if got := f.osCapture(); got != f.window.Handle().Hwnd() {
		t.Fatalf("OS capture after the press = %#x, want the window's own (%#x)", got, f.window.Handle().Hwnd())
	}
	f.wantLog("the press") // no events yet: the capture is pending

	// The move outside the button processes the pending capture:
	// gotpointercapture fires and the retargeted dispatch keeps the
	// press alive (the active style stays).
	f.send(wmMouseMove, mkLButton, f.at(150, 150))
	desktopWaitFor(t, "the gotpointercapture event", func() bool {
		return f.logLen() > 0
	})
	f.wantLog("the captured move", "got:button")
	// The press is still alive: the active style is still applied.
	if red, blue := f.colors(); red != 0 || blue != 1 {
		t.Fatalf("captured colors red %d blue %d, want the button still pressed (the retargeted chain)", red, blue)
	}

	// WM_CAPTURECHANGED with a foreign window in lParam: the takeover.
	// lostpointercapture fires, the pressed style clears, the pending
	// press drops and the held-button tracking clears.
	// The synthetic message does not itself take the OS capture away
	// (the system would, calling SetCapture on the foreign window); it
	// only routes the cancel. The tracking that matters is the
	// held-button set: the next release must release the OS capture
	// instead of holding it forever.
	before = f.draws()
	f.send(wmCaptureChanged, 0, foreignCaptureWindow)
	f.waitForColors("the cancel to clear the pressed style", before, 1, 0)
	f.wantLog("the capture change", "lost:button")

	// The release that the real window would never see (the other
	// window owns it now): it clicks NOTHING — the press was cancelled —
	// and because the cancel cleared the held-button tracking, the
	// handler releases the OS capture instead of holding it forever.
	f.send(wmLButtonUp, 0, f.at(150, 150))
	f.wantLog("the stray release") // no click: the press was cancelled
	if got := f.osCapture(); got != 0 {
		t.Fatalf("OS capture after the release = %#x, want none (no button stuck)", got)
	}

	// A fresh press works end to end: capture, click, release.
	before = f.draws()
	f.send(wmMouseMove, 0, f.at(50, 50))
	f.send(wmLButtonDown, mkLButton, f.at(50, 50))
	f.waitForColors("the fresh press", before, 0, 1)
	f.send(wmMouseMove, mkLButton, f.at(50, 50))
	f.wantLog("the fresh capture", "got:button")
	f.send(wmLButtonUp, 0, f.at(50, 50))
	// Browser-core ticket 12 (divergence toward Chrome, spec D12): the
	// click now fires after the up's listeners AND after the capture
	// release — pointerup, lostpointercapture, click,
	// pointer_event_manager.cc:1283-1296 — where the pin fired the
	// click inside the up's listener traversal (click before lost).
	// The expectation changed with the ticket.
	f.wantLog("the fresh click", "lost:button", "click:button")
	if got := f.osCapture(); got != 0 {
		t.Fatalf("OS capture after the fresh release = %#x, want none", got)
	}
}
