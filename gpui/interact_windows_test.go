//go:build windows

package gpui

// Base ticket 06's real-window gate for the Win32 mouse button path
// (gpui_windows/src/events.rs:138-149, 489-559): on a real window, on
// the host thread, a synthetic WM_*BUTTONDOWN through the button handler
// captures the mouse to the window (GetCapture reports it) and the
// matching release releases it; the translation carries the logical
// position, the ClickState count (window.rs:1351-1418) and the input
// callback's handled answer. No real input is injected and the cursor
// is never moved; a window-creation failure FAILS the test.

import (
	"fmt"
	"slices"
	"testing"
	"time"
)

// buttonLParam packs client device-pixel coordinates like the system
// (MAKELPARAM with signed words).
func buttonLParam(x, y int16) uintptr {
	return uintptr(uint16(x)) | uintptr(uint16(y))<<16
}

// TestInteractWin32MouseCaptureOnRealWindow drives the button handler on
// a real window with synthetic messages.
func TestInteractWin32MouseCaptureOnRealWindow(t *testing.T) {
	h := desktopNewHost(t)
	handle, err := h.OpenWindow(WindowOptions{Title: "interact capture", Show: true})
	if err != nil {
		t.Fatalf("OpenWindow failed (real window expected): %v", err)
	}
	desktopWaitFor(t, "the window to be visible", handle.Visible)

	type step struct {
		result   uintptr
		handled  bool
		captured uintptr
	}
	var (
		steps  []step
		events []any
		hwnd   uintptr
		scale  float32
	)
	ran := h.runForegroundSync(func() {
		w := handle.record()
		if w == nil {
			return
		}
		hwnd, scale = w.hwnd, w.scale
		clicks := w.clickState()
		clock := time.Unix(1_000_000, 0)
		clicks.now = func() time.Time { return clock }
		clicks.lastClick = clock.Add(-time.Hour)
		consume := func(event any) bool { events = append(events, event); return true }
		decline := func(event any) bool { events = append(events, event); return false }
		drive := func(message uint32, wparam uintptr, x, y int16, input func(any) bool) {
			result, handled := w.handleMouseButtonMsg(message, wparam, buttonLParam(x, y), input)
			captured, _, _ := procGetCapture.Call()
			steps = append(steps, step{result: result, handled: handled, captured: captured})
		}
		drive(wmLButtonDown, 0, 20, 30, consume) // 0: captured, count 1
		drive(wmLButtonUp, 0, 21, 31, consume)   // 1: released
		clock = clock.Add(10 * time.Millisecond)
		drive(wmLButtonDown, 0, 21, 31, consume) // 2: double click, count 2
		drive(wmLButtonUp, 0, 21, 31, decline)   // 3: released, unhandled -> 1
		clock = clock.Add(time.Hour)
		drive(wmRButtonDown, 0, 21, 31, consume)               // 4: new button, count 1
		drive(wmRButtonUp, 0, 21, 31, consume)                 // 5
		drive(wmXButtonDown, uintptr(xButton2)<<16, 5, 6, nil) // 6: no callback -> 1, still captured
		drive(wmXButtonUp, uintptr(xButton2)<<16, 5, 6, consume)
		drive(wmXButtonDown, 0x0004<<16, 5, 6, consume) // 8: unknown X button -> 1
		drive(0x0200, 0, 5, 6, consume)                 // 9: WM_MOUSEMOVE is not a button message
		procReleaseCapture.Call()
	})
	if !ran {
		t.Fatal("the host stopped before the capture gate ran")
	}
	if hwnd == 0 || len(steps) != 10 {
		t.Fatalf("the gate did not run on a live window record (steps %d)", len(steps))
	}

	wantCapture := []uintptr{hwnd, 0, hwnd, 0, hwnd, 0, hwnd, 0}
	for i, want := range wantCapture {
		if steps[i].captured != want {
			t.Errorf("step %d: GetCapture = %#x, want %#x (SetCapture on press, ReleaseCapture on release)", i, steps[i].captured, want)
		}
		if !steps[i].handled {
			t.Errorf("step %d: the button message was not handled", i)
		}
	}
	wantResults := []uintptr{0, 0, 0, 1, 0, 0, 1, 0, 1}
	for i, want := range wantResults {
		if steps[i].result != want {
			t.Errorf("step %d: result = %d, want %d", i, steps[i].result, want)
		}
	}
	if !steps[8].handled || steps[9].handled {
		t.Errorf("handled flags: unknown X button %v (want true), mouse move %v (want false)", steps[8].handled, steps[9].handled)
	}

	if len(events) != 7 {
		t.Fatalf("delivered events = %d, want 7: %+v", len(events), events)
	}
	at := func(x, y float32) Point { return Point{X: x / scale, Y: y / scale} }
	down0, ok := events[0].(*MouseDownEvent)
	if !ok || down0.Button != MouseButtonLeft || down0.ClickCount != 1 || down0.Position != at(20, 30) {
		t.Errorf("event 0 = %+v, want left down x1 at %v", events[0], at(20, 30))
	}
	up1, ok := events[1].(*MouseUpEvent)
	if !ok || up1.Button != MouseButtonLeft || up1.ClickCount != 1 || up1.Position != at(21, 31) {
		t.Errorf("event 1 = %+v, want left up x1", events[1])
	}
	if down2, ok := events[2].(*MouseDownEvent); !ok || down2.ClickCount != 2 {
		t.Errorf("event 2 = %+v, want the double click (count 2)", events[2])
	}
	if up3, ok := events[3].(*MouseUpEvent); !ok || up3.ClickCount != 2 {
		t.Errorf("event 3 = %+v, want the double-click release (count 2)", events[3])
	}
	if down4, ok := events[4].(*MouseDownEvent); !ok || down4.Button != MouseButtonRight || down4.ClickCount != 1 {
		t.Errorf("event 4 = %+v, want right down x1 (another button restarts the count)", events[4])
	}
	if up6, ok := events[6].(*MouseUpEvent); !ok || up6.Button != MouseButtonButton5 {
		t.Errorf("event 6 = %+v, want the XBUTTON2 (forward) release", events[6])
	}
	if c, _, _ := procGetCapture.Call(); c != 0 {
		// GetCapture is per thread input queue; from the test goroutine it
		// must not report this window either.
		t.Errorf("capture leaked to the test thread's queue: %#x", c)
	}
}

// TestInteractWin32MouseCaptureKeptWhileButtonHeld pins the capture
// release policy of browser-core ticket 02 (research browser-parity 03
// §5 gap 4; Chromium keeps capturing while any button is down): with
// two buttons pressed, releasing one KEEPS the capture — so a second
// held button no longer loses the drag — and releasing the last button
// releases it. A release with no tracked press still releases (the
// pin's unconditional behavior for unpaired messages, events.rs:519-
// 544). The window sits offscreen; no real input is injected.
func TestInteractWin32MouseCaptureKeptWhileButtonHeld(t *testing.T) {
	h := desktopNewHost(t)
	// Far offscreen: a real window, but nothing appears on any monitor.
	handle, err := h.OpenWindow(WindowOptions{
		Title:  "interact capture multi-button",
		Bounds: Bounds{Origin: Point{X: 20000, Y: 20000}, Size: Size{Width: 120, Height: 80}},
		Show:   true,
	})
	if err != nil {
		t.Fatalf("OpenWindow failed (offscreen window expected): %v", err)
	}
	desktopWaitFor(t, "the window to be visible", handle.Visible)

	type step struct {
		message  uint32
		wparam   uintptr
		captured uintptr
	}
	var (
		steps  []step
		events []MouseButton
		hwnd   uintptr
	)
	ran := h.runForegroundSync(func() {
		w := handle.record()
		if w == nil {
			return
		}
		hwnd = w.hwnd
		consume := func(event any) bool {
			if e, ok := event.(*MouseDownEvent); ok {
				events = append(events, e.Button)
			}
			if e, ok := event.(*MouseUpEvent); ok {
				events = append(events, e.Button)
			}
			return true
		}
		drive := func(message uint32, wparam uintptr) {
			w.handleMouseButtonMsg(message, wparam, buttonLParam(20, 30), consume)
			captured, _, _ := procGetCapture.Call()
			steps = append(steps, step{message: message, wparam: wparam, captured: captured})
		}
		// Two buttons held: left then right.
		drive(wmLButtonDown, 0)
		drive(wmRButtonDown, 0)
		// Releasing one of the two keeps the capture.
		drive(wmLButtonUp, 0)
		// Releasing the last held button releases it.
		drive(wmRButtonUp, 0)
		// A release with nothing tracked still releases (unpaired path).
		drive(wmMButtonDown, 0)
		drive(wmMButtonUp, 0)
		drive(wmRButtonUp, 0)
	})
	if !ran {
		t.Fatal("the host stopped before the capture gate ran")
	}
	if hwnd == 0 || len(steps) != 7 {
		t.Fatalf("the gate did not run on a live window record (steps %d)", len(steps))
	}
	name := func(i int) string { return fmt.Sprintf("step %d (%#x)", i, steps[i].message) }
	wantCapture := []uintptr{hwnd, hwnd, hwnd, 0, hwnd, 0, 0}
	for i, want := range wantCapture {
		if steps[i].captured != want {
			t.Errorf("%s: GetCapture = %#x, want %#x", name(i), steps[i].captured, want)
		}
	}
	wantButtons := []MouseButton{
		MouseButtonLeft, MouseButtonRight, // downs
		MouseButtonLeft, MouseButtonRight, // ups
		MouseButtonMiddle, MouseButtonMiddle,
		MouseButtonRight, // the unpaired release still delivers
	}
	if !slices.Equal(events, wantButtons) {
		t.Errorf("delivered buttons = %v, want %v (the unpaired release delivers no event)", events, wantButtons)
	}
	if c, _, _ := procGetCapture.Call(); c != 0 {
		t.Errorf("capture leaked to the test thread's queue: %#x", c)
	}
}

// TestInteractWin32ClickStateSystemUpdate pins ClickState::system_update
// (window.rs:1392-1408): the double-click settings refresh from the
// system on the matching SPI codes only.
func TestInteractWin32ClickStateSystemUpdate(t *testing.T) {
	c := newWin32ClickState()
	want := *c
	c.toleranceW, c.toleranceH, c.interval = -1, -1, -1
	c.systemUpdate(31) // not a double-click setting
	if c.toleranceW != -1 || c.toleranceH != -1 || c.interval != -1 {
		t.Fatal("an unrelated SPI code refreshed the click state")
	}
	c.systemUpdate(spiSetDoubleClkWidth)
	c.systemUpdate(spiSetDoubleClkHeight)
	c.systemUpdate(spiSetDoubleClickTime)
	if c.toleranceW != want.toleranceW || c.toleranceH != want.toleranceH || c.interval != want.interval {
		t.Fatalf("refreshed state = %d/%d/%v, want %d/%d/%v", c.toleranceW, c.toleranceH, c.interval, want.toleranceW, want.toleranceH, want.interval)
	}
	if want.interval <= 0 || want.toleranceW <= 0 || want.toleranceH <= 0 {
		t.Fatalf("system double-click settings not read: %+v", want)
	}
}

// TestInteractCursorVariantsWithoutWindowsArm pins the four pinned
// CursorStyle variants moved into desktop.go (platform.rs:3204, 3208,
// 3268, 3272): the Windows loader has no arm for them, so they load the
// arrow through load_cursor's catch-all (gpui_windows/src/util.rs:114),
// and they stay distinct values after OperationNotAllowed.
func TestInteractCursorVariantsWithoutWindowsArm(t *testing.T) {
	variants := []CursorStyle{CursorClosedHand, CursorOpenHand, CursorDragCopy, CursorContextualMenu}
	for i, style := range variants {
		if style != CursorOperationNotAllowed+1+CursorStyle(i) {
			t.Errorf("variant %d = %d, want %d", i, style, CursorOperationNotAllowed+1+CursorStyle(i))
		}
		if got := cursorResourceID(style); got != idcArrow {
			t.Errorf("cursorResourceID(%d) = %d, want IDC_ARROW", style, got)
		}
		if loadCursorHandle(style) != loadCursorHandle(CursorArrow) {
			t.Errorf("style %d did not load the shared arrow cursor", style)
		}
	}
}
