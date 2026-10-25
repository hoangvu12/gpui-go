//go:build windows

package gpui

// Browser-core ticket 10's real-window gate for the window leave: on a
// real Win32 window placed far off screen (never visible to the user,
// the capture-host pattern at a positive off-screen origin), a
// synthetic WM_MOUSEMOVE over a hover-styled button applies the hover
// style and fires pointerenter, and the WM_MOUSELEAVE the system's
// TrackMouseEvent produces (dispatched here synthetically, exactly the
// message the armed tracking delivers) clears the hover style on every
// element and fires pointerleave innermost-first, like Chromium's
// kMouseLeave (core/input/event_handler.cc:1158-1165: no hit test, hover
// and active state cleared with a null target). No real input is
// injected and the real cursor is never moved; a window-creation
// failure FAILS the test.

import (
	"sync"
	"testing"
)

// offscreenWindowOrigin is where ticket 10's leave-gate window lives:
// far off screen at a positive origin, so it is a real, shown, drawing
// window that the user never sees.
var offscreenWindowOrigin = Point{X: 20000, Y: 20000}

// hoverLeaveWindow is the leave-gate fixture: a real window off screen
// whose root view renders one hover-styled button, with a log of the
// boundary and hover events its listeners observed.
type hoverLeaveWindow struct {
	t      *testing.T
	h      *Host
	app    *App
	window *Window
	scale  float32

	// sending is true while the test's own SendMessageW runs (host
	// thread only): the OnMouseInput interceptor consumes every other
	// mouse event — a real cursor move over the off-screen rect cannot
	// happen, but the system synthesizes moves at show time, and the
	// armed TrackMouseEvent may deliver a real WM_MOUSELEAVE because
	// the real cursor sits outside the off-screen rect — so only the
	// synthetic messages reach element dispatch (the win32 input gate
	// pattern).
	sending bool

	mu  sync.Mutex
	log []string
}

func (f *hoverLeaveWindow) add(entry string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log = append(f.log, entry)
}

func (f *hoverLeaveWindow) takeLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.log
	f.log = nil
	return out
}

// openHoverLeaveWindow opens the off-screen leave-gate window.
func openHoverLeaveWindow(t *testing.T) *hoverLeaveWindow {
	t.Helper()
	h := desktopNewHost(t)
	app := NewApp()
	if err := app.Attach(h); err != nil {
		t.Fatalf("app attach: %v", err)
	}
	f := &hoverLeaveWindow{t: t, h: h, app: app}
	window, err := app.OpenWindowView(WindowOptions{
		Title: "hover leave (offscreen)",
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
				return Div().Flex().ID("root").SizeFull().Relative().Child(
					Div().Flex().ID("button").
						Absolute().Left(Px(20)).Top(Px(20)).Size(Px(80)).
						Bg(interactRed).
						Hover(func(s StyleRefinement) StyleRefinement { return Div().Flex().Refine(s).Bg(interactBlue).Style() }).
						OnPointerEnter(func(e *PointerEnterEvent, w *Window, app *App) { f.add("enter:button") }).
						OnPointerLeave(func(e *PointerLeaveEvent, w *Window, app *App) { f.add("leave:button") }).
						OnHover(func(hovered *bool, w *Window, app *App) {
							f.add(map[bool]string{true: "hover(true)", false: "hover(false)"}[*hovered])
						}).
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
func (f *hoverLeaveWindow) onHost(fn func()) {
	f.t.Helper()
	if !f.h.runForegroundSync(fn) {
		f.t.Fatal("the host stopped")
	}
}

// send delivers one synthetic message to the window with SendMessageW
// on the host thread.
func (f *hoverLeaveWindow) send(message uint32, wparam, lparam uintptr) {
	f.t.Helper()
	f.onHost(func() {
		f.sending = true
		defer func() { f.sending = false }()
		_, _, _ = procSendMessageW.Call(f.window.Handle().Hwnd(), uintptr(message), wparam, lparam)
	})
}

// at packs a window-local logical point as the client device-pixel
// lparam of the mouse messages.
func (f *hoverLeaveWindow) at(x, y float32) uintptr {
	return buttonLParam(int16(x*f.scale+0.5), int16(y*f.scale+0.5))
}

// colors counts the button quads of the last drawn scene on the host
// thread.
func (f *hoverLeaveWindow) colors() (red, blue int) {
	f.onHost(func() {
		red, blue = sceneQuadColorCount(f.window, interactRed), sceneQuadColorCount(f.window, interactBlue)
	})
	return red, blue
}

// draws reads the window's draw count.
func (f *hoverLeaveWindow) draws() (n uint64) {
	f.onHost(func() { n = WindowDrawCount(f.window) })
	return n
}

// wantLog asserts the exact event log.
func (f *hoverLeaveWindow) wantLog(what string, want ...string) {
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

// TestWin32MouseLeaveClearsHoverOnOffscreenWindow drives the whole
// WM_MOUSELEAVE path on a real window: the leave message dispatches a
// MouseExitEvent through the mouse input callback into element
// dispatch, which clears the hover hit test, fires the pointer leave
// events innermost-first and repaints the hover styles away.
func TestWin32MouseLeaveClearsHoverOnOffscreenWindow(t *testing.T) {
	f := openHoverLeaveWindow(t)

	// The un-hovered opening frame.
	if red, blue := f.colors(); red != 1 || blue != 0 {
		t.Fatalf("opening colors red %d blue %d, want the un-hovered button", red, blue)
	}

	// WM_MOUSEMOVE over the button: the hover style applies, and
	// pointerenter fires before the OnHover hover start.
	before := f.draws()
	f.send(wmMouseMove, 0, f.at(50, 50))
	desktopWaitFor(t, "the hovered button to be repainted", func() bool {
		if f.draws() <= before {
			return false
		}
		red, blue := f.colors()
		return red == 0 && blue == 1
	})
	f.wantLog("the move over the button", "enter:button", "hover(true)")

	// WM_MOUSELEAVE: the hover style clears on every element and the
	// leave events fire (pointerleave before the hover end).
	before = f.draws()
	f.send(wmMouseLeave, 0, 0)
	desktopWaitFor(t, "the leave to clear the hover style", func() bool {
		if f.draws() <= before {
			return false
		}
		red, blue := f.colors()
		return red == 1 && blue == 0
	})
	f.wantLog("the window leave", "leave:button", "hover(false)")
}
