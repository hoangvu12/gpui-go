//go:build windows

package gpui

// Base ticket 34: the client-area mouse move and wheel messages of a real
// Win32 window, translated into the platform input events the window's
// mouse input callback (WindowOptions.OnMouseInput; App.OpenWindow routes
// it into Window.dispatchInput) delivers to element dispatch. Ported from
// the pinned CE reference crates/gpui_windows/src/events.rs:
//
//   - :114 / :371-398 WM_MOUSEMOVE handle_mouse_move_msg: start the leave
//     tracking, restore a hidden cursor, then deliver a MouseMoveEvent
//     whose pressed button is the first of MK_LBUTTON, MK_RBUTTON,
//     MK_MBUTTON, MK_XBUTTON1, MK_XBUTTON2 present in LOWORD(wparam);
//     Some(1) without an input callback, else 0 when handled and 1 when
//     not (the message never reaches DefWindowProc).
//   - :150-151 / :562-613 WM_MOUSEWHEEL handle_mouse_wheel_msg: the
//     distance is (signed HIWORD(wparam) / WHEEL_DELTA) times the wheel
//     scroll lines, or the wheel scroll chars while Shift is held — in
//     which case the distance moves to the x axis; the screen position in
//     lparam goes through ScreenToClient; a line delta in the Moved phase.
//   - :615-651 WM_MOUSEHWHEEL handle_mouse_horizontal_wheel_msg: the
//     negated distance times the wheel scroll chars, on the x axis.
//
// The button messages are desktop_input_windows.go's handleMouseButtonMsg
// (events.rs:138-149, 489-559). Non-client button messages
// (events.rs:119-137) are not routed: the port's windows keep the system
// title bar and the non-client hit testing is not ported.

import "unsafe"

// Mouse key-state flags in WM_MOUSEMOVE's wparam and the wheel unit
// (winuser.h).
const (
	mkLButton  = 0x0001 // MK_LBUTTON
	mkRButton  = 0x0002 // MK_RBUTTON
	mkMButton  = 0x0010 // MK_MBUTTON
	mkXButton1 = 0x0020 // MK_XBUTTON1
	mkXButton2 = 0x0040 // MK_XBUTTON2

	wheelDelta = 120 // WHEEL_DELTA

	wmMouseWheel  = 0x020A // WM_MOUSEWHEEL
	wmMouseHWheel = 0x020E // WM_MOUSEHWHEEL
)

// win32PressedButton is events.rs:378-388: the first held button in the
// pin's match order, or nil.
func win32PressedButton(wparam uintptr) *MouseButton {
	flags := uint32(loword(wparam))
	var button MouseButton
	switch {
	case flags&mkLButton != 0:
		button = MouseButtonLeft
	case flags&mkRButton != 0:
		button = MouseButtonRight
	case flags&mkMButton != 0:
		button = MouseButtonMiddle
	case flags&mkXButton1 != 0:
		button = MouseButtonButton4 // Navigate(Back)
	case flags&mkXButton2 != 0:
		button = MouseButtonButton5 // Navigate(Forward)
	default:
		return nil
	}
	return &button
}

// win32MouseMoveEvent is the translation half of events.rs:371-398.
func win32MouseMoveEvent(wparam, lp uintptr, scale float32, modifiers Modifiers) *MouseMoveEvent {
	return &MouseMoveEvent{
		Position:      win32LogicalPoint(signedLoword(lp), signedHiword(lp), scale),
		PressedButton: win32PressedButton(wparam),
		Modifiers:     modifiers,
	}
}

// win32WheelEvent is the translation half of events.rs:562-613 (vertical)
// and :615-651 (horizontal): clientX/clientY are the ScreenToClient
// converted device-pixel position.
func win32WheelEvent(horizontal bool, wparam uintptr, clientX, clientY int32, scale float32, modifiers Modifiers, settings MouseWheelSettings) *ScrollWheelEvent {
	notches := float32(signedHiword(wparam)) / wheelDelta
	var delta ScrollDelta
	switch {
	case horizontal:
		delta = ScrollDeltaLines(-notches*float32(settings.WheelScrollChars), 0)
	case modifiers.Shift:
		delta = ScrollDeltaLines(notches*float32(settings.WheelScrollChars), 0)
	default:
		delta = ScrollDeltaLines(0, notches*float32(settings.WheelScrollLines))
	}
	return &ScrollWheelEvent{
		Position:   win32LogicalPoint(clientX, clientY, scale),
		Delta:      delta,
		Modifiers:  modifiers,
		TouchPhase: TouchPhaseMoved,
	}
}

// win32InputResult is the pin's `if handled { Some(0) } else { Some(1) }`.
func win32InputResult(handled bool) uintptr {
	if handled {
		return 0
	}
	return 1
}

// handleMouseMoveMsg is events.rs:371-398 handle_mouse_move_msg.
func (w *hostWindow) handleMouseMoveMsg(wparam, lp uintptr) uintptr {
	w.startMouseLeaveTracking(tmeLeave)
	w.restoreCursorAfterHide()
	input := w.onMouseInput
	if input == nil {
		return 1
	}
	return win32InputResult(input(win32MouseMoveEvent(wparam, lp, w.scale, win32CurrentModifiers())))
}

// handleMouseWheelMsg is events.rs:562-613 handle_mouse_wheel_msg
// (horizontal false) and :615-651 handle_mouse_horizontal_wheel_msg
// (horizontal true).
func (w *hostWindow) handleMouseWheelMsg(horizontal bool, wparam, lp uintptr) uintptr {
	modifiers := win32CurrentModifiers()
	input := w.onMouseInput
	if input == nil {
		return 1
	}
	cursor := point{x: signedLoword(lp), y: signedHiword(lp)}
	if ok, _, _ := procScreenToClient.Call(w.hwnd, uintptr(unsafe.Pointer(&cursor))); ok == 0 {
		// The pin logs the failure and continues with the screen point
		// (ScreenToClient(..).ok().log_err()).
		tracef("wheel ScreenToClient failed hwnd=%x", w.hwnd)
	}
	event := win32WheelEvent(horizontal, wparam, cursor.x, cursor.y, w.scale, modifiers, w.host.desktop.systemSettings)
	return win32InputResult(input(event))
}

// handleMouseInputMsg routes the client-area mouse messages of the pinned
// dispatch table (events.rs:114, 138-151). It reports false for any other
// message.
func (w *hostWindow) handleMouseInputMsg(message uint32, wparam, lp uintptr) (uintptr, bool) {
	switch message {
	case wmMouseMove:
		return w.handleMouseMoveMsg(wparam, lp), true
	case wmMouseWheel:
		return w.handleMouseWheelMsg(false, wparam, lp), true
	case wmMouseHWheel:
		return w.handleMouseWheelMsg(true, wparam, lp), true
	}
	return w.handleMouseButtonMsg(message, wparam, lp, w.onMouseInput)
}
