//go:build windows

package gpui

// Ticket26's character-palette path, ported from the pinned CE
// reference 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a,
// crates/gpui_windows/src/window.rs show_character_palette and
// character_palette_inputs/character_palette_key:
//
// Windows exposes its emoji/character picker through Win+period. The
// palette must not inherit the caller's held modifiers (e.g. the
// Ctrl+Space that triggered it), so the input batch releases them,
// sends Win+period, and restores them in the same batch. A partial
// SendInput must not leave the synthetic Win/period keys down or the
// caller's modifiers released: the cleanup batch presses period up,
// releases the synthetic Win (unless the user really held it) and
// re-presses the caller's modifiers.
//
// SendInput injects real keyboard input, so nothing in the test corpus
// runs the real send: the pure input planning (ported with the
// reference's own unit vectors), the partial-send cleanup construction
// and the inactive-window guard are exercised instead, with the send
// itself behind a seam whose default is the real syscall.
//
// Base ticket 06 adds the client-area mouse button messages
// (gpui_windows/src/events.rs:138-149, 489-559): SetCapture on a press
// and ReleaseCapture on a release (through user32, not the native
// artifact), the ClickState double-click counter (window.rs:1351-1418)
// and the translation into MouseDownEvent / MouseUpEvent delivered to
// an input callback.

import (
	"fmt"
	"time"
	"unsafe"
)

var (
	procSendInput           = modUser32.NewProc("SendInput")
	procGetAsyncKeyState    = modUser32.NewProc("GetAsyncKeyState")
	procGetForegroundWindow = modUser32.NewProc("GetForegroundWindow")

	// Base ticket 06's pointer capture and click counting (user32
	// through the Go host's lazy DLL, not the native artifact).
	procSetCapture            = modUser32.NewProc("SetCapture")
	procReleaseCapture        = modUser32.NewProc("ReleaseCapture")
	procGetCapture            = modUser32.NewProc("GetCapture")
	procGetDoubleClickTime    = modUser32.NewProc("GetDoubleClickTime")
	procGetSystemMetricsInput = modUser32.NewProc("GetSystemMetrics")
)

// Mouse button messages and metrics (winuser.h).
const (
	wmLButtonDown = 0x0201 // WM_LBUTTONDOWN
	wmLButtonUp   = 0x0202 // WM_LBUTTONUP
	wmRButtonDown = 0x0204 // WM_RBUTTONDOWN
	wmRButtonUp   = 0x0205 // WM_RBUTTONUP
	wmMButtonDown = 0x0207 // WM_MBUTTONDOWN
	wmMButtonUp   = 0x0208 // WM_MBUTTONUP
	wmXButtonDown = 0x020B // WM_XBUTTONDOWN
	wmXButtonUp   = 0x020C // WM_XBUTTONUP

	xButton1 = 0x0001 // XBUTTON1
	xButton2 = 0x0002 // XBUTTON2

	smCxDoubleClk = 36 // SM_CXDOUBLECLK
	smCyDoubleClk = 37 // SM_CYDOUBLECLK

	spiSetDoubleClkWidth  = 29 // SPI_SETDOUBLECLKWIDTH
	spiSetDoubleClkHeight = 30 // SPI_SETDOUBLECLKHEIGHT
	spiSetDoubleClickTime = 32 // SPI_SETDOUBLECLICKTIME
)

// ---------------------------------------------------------------------------
// Mouse button messages (gpui_windows/src/events.rs:138-149, 489-559)
// ---------------------------------------------------------------------------

// win32ClickState is the pinned ClickState (gpui_windows/src/window.rs:
// 1351-1418): the last pressed button, time and device-pixel position,
// the system double-click tolerances and interval, and the running
// click count. Host thread only.
type win32ClickState struct {
	button       MouseButton
	lastClick    time.Time
	lastX, lastY int32
	toleranceW   int32
	toleranceH   int32
	interval     time.Duration
	currentCount int
	// now is the clock (time.Now; tests pin it).
	now func() time.Time
}

// newWin32ClickState is ClickState::new (window.rs:1362-1376): the
// tolerances from GetSystemMetrics(SM_CX/CYDOUBLECLK) and the interval
// from GetDoubleClickTime.
func newWin32ClickState() *win32ClickState {
	w, _, _ := procGetSystemMetricsInput.Call(smCxDoubleClk)
	h, _, _ := procGetSystemMetricsInput.Call(smCyDoubleClk)
	ms, _, _ := procGetDoubleClickTime.Call()
	return &win32ClickState{
		button:     MouseButtonLeft,
		lastClick:  time.Now(),
		toleranceW: int32(w),
		toleranceH: int32(h),
		interval:   time.Duration(ms) * time.Millisecond,
		now:        time.Now,
	}
}

// update is ClickState::update (window.rs:1379-1390): the same button
// pressed again inside the interval and tolerances continues the count,
// anything else restarts it at 1.
func (c *win32ClickState) update(button MouseButton, x, y int32) int {
	now := c.now()
	if c.button == button && c.isDoubleClick(now, x, y) {
		c.currentCount++
	} else {
		c.currentCount = 1
	}
	c.lastClick = now
	c.lastX, c.lastY = x, y
	c.button = button
	return c.currentCount
}

// systemUpdate is ClickState::system_update (window.rs:1392-1408), the
// WM_SETTINGCHANGE refresh of the tolerances and the interval
// (events.rs:1220).
func (c *win32ClickState) systemUpdate(wparam uintptr) {
	switch wparam {
	case spiSetDoubleClkWidth:
		w, _, _ := procGetSystemMetricsInput.Call(smCxDoubleClk)
		c.toleranceW = int32(w)
	case spiSetDoubleClkHeight:
		h, _, _ := procGetSystemMetricsInput.Call(smCyDoubleClk)
		c.toleranceH = int32(h)
	case spiSetDoubleClickTime:
		ms, _, _ := procGetDoubleClickTime.Call()
		c.interval = time.Duration(ms) * time.Millisecond
	}
}

// isDoubleClick is ClickState::is_double_click (window.rs:1411-1417).
func (c *win32ClickState) isDoubleClick(now time.Time, x, y int32) bool {
	dx, dy := c.lastX-x, c.lastY-y
	if dx < 0 {
		dx = -dx
	}
	if dy < 0 {
		dy = -dy
	}
	return now.Sub(c.lastClick) < c.interval && dx <= c.toleranceW && dy <= c.toleranceH
}

// win32ClickStates holds each window's click state (the pinned
// WindowsWindowState::click_state), kept beside the host record because
// the record's struct belongs to the host slice. Host thread only; the
// host's WM_NCDESTROY retirement is expected to call
// forgetWin32ClickState.
var win32ClickStates = map[*hostWindow]*win32ClickState{}

// clickState returns (creating) the window's click state.
func (w *hostWindow) clickState() *win32ClickState {
	if c, ok := win32ClickStates[w]; ok {
		return c
	}
	c := newWin32ClickState()
	win32ClickStates[w] = c
	return c
}

// forgetWin32ClickState drops a destroyed window's click state.
func forgetWin32ClickState(w *hostWindow) { delete(win32ClickStates, w) }

// win32LogicalPoint is util.rs:147-152 logical_point.
func win32LogicalPoint(x, y int32, scale float32) Point {
	if scale <= 0 {
		scale = 1
	}
	return Point{X: float32(x) / scale, Y: float32(y) / scale}
}

// handleMouseDownMsg is events.rs:489-517 handle_mouse_down_msg: capture
// the mouse to this window (SetCapture, so the release and the drag
// moves arrive here even outside the client area), then translate the
// press — device-pixel position to logical, the live modifiers, the
// click count — and deliver it through the input callback. It returns 0
// when the application handled the event and 1 otherwise; a missing
// callback reports 1 after the capture, like the pin.
func (w *hostWindow) handleMouseDownMsg(button MouseButton, lp uintptr, input func(event any) bool) uintptr {
	procSetCapture.Call(w.hwnd)
	if input == nil {
		return 1
	}
	x, y := signedLoword(lp), signedHiword(lp)
	clickCount := w.clickState().update(button, x, y)
	event := &MouseDownEvent{
		Button:     button,
		Position:   win32LogicalPoint(x, y, w.scale),
		Modifiers:  win32CurrentModifiers(),
		ClickCount: clickCount,
	}
	if input(event) {
		return 0
	}
	return 1
}

// handleMouseUpMsg is events.rs:519-544 handle_mouse_up_msg: release the
// mouse capture, then translate and deliver the release with the
// current click count.
func (w *hostWindow) handleMouseUpMsg(button MouseButton, lp uintptr, input func(event any) bool) uintptr {
	procReleaseCapture.Call()
	if input == nil {
		return 1
	}
	x, y := signedLoword(lp), signedHiword(lp)
	event := &MouseUpEvent{
		Button:     button,
		Position:   win32LogicalPoint(x, y, w.scale),
		Modifiers:  win32CurrentModifiers(),
		ClickCount: w.clickState().currentCount,
	}
	if input(event) {
		return 0
	}
	return 1
}

// handleMouseButtonMsg routes the client-area button messages
// (events.rs:138-149, with handle_xbutton_msg at 546-559: XBUTTON1 is
// the back navigation button, XBUTTON2 forward — the port's Button4 and
// Button5; another X button is not handled and reports 1). It reports
// false for any other message. This is the single call the host window
// procedure needs.
func (w *hostWindow) handleMouseButtonMsg(message uint32, wparam, lp uintptr, input func(event any) bool) (uintptr, bool) {
	switch message {
	case wmLButtonDown:
		return w.handleMouseDownMsg(MouseButtonLeft, lp, input), true
	case wmRButtonDown:
		return w.handleMouseDownMsg(MouseButtonRight, lp, input), true
	case wmMButtonDown:
		return w.handleMouseDownMsg(MouseButtonMiddle, lp, input), true
	case wmLButtonUp:
		return w.handleMouseUpMsg(MouseButtonLeft, lp, input), true
	case wmRButtonUp:
		return w.handleMouseUpMsg(MouseButtonRight, lp, input), true
	case wmMButtonUp:
		return w.handleMouseUpMsg(MouseButtonMiddle, lp, input), true
	case wmXButtonDown, wmXButtonUp:
		var button MouseButton
		switch hiword(wparam) {
		case xButton1:
			button = MouseButtonButton4
		case xButton2:
			button = MouseButtonButton5
		default:
			return 1, true
		}
		if message == wmXButtonDown {
			return w.handleMouseDownMsg(button, lp, input), true
		}
		return w.handleMouseUpMsg(button, lp, input), true
	}
	return 0, false
}

// Input constants (winuser.h).
const (
	inputKeyboard     = 1 // INPUT_KEYBOARD
	keyeventfKeyUp    = 0x0002
	keyeventfExtended = 0x0001

	vkOemPeriodByte = 0xBE // VK_OEM_PERIOD
	vkRcontrolByte  = 0xA3 // VK_RCONTROL
	vkRmenuByte     = 0xA5 // VK_RMENU
)

// keyboardInput is Win32 KEYBDINPUT (24 bytes on amd64).
type keyboardInput struct {
	wVk         uint16
	wScan       uint16
	dwFlags     uint32
	time        uint32
	_           uint32
	dwExtraInfo uintptr
}

// sendInputRecord is Win32 INPUT (40 bytes on amd64): the type
// discriminator, alignment padding and the union sized to MOUSEINPUT.
// Only the keyboard arm is used; _tail keeps the union at 32 bytes.
type sendInputRecord struct {
	inputType uint32
	_         uint32
	ki        keyboardInput
	_tail     [8]byte
}

// characterPaletteKey builds one synthetic key event
// (window.rs character_palette_key): the extended-key flag marks the
// right-hand Windows/Alt/Control keys and both Windows keys so the
// input matches their physical scan codes.
func characterPaletteKey(vkey uint8, keyUp bool) sendInputRecord {
	var flags uint32
	if keyUp {
		flags |= keyeventfKeyUp
	}
	switch vkey {
	case vkLwin, vkRwin, vkRcontrolByte, vkRmenuByte:
		flags |= keyeventfExtended
	}
	return sendInputRecord{
		inputType: inputKeyboard,
		ki:        keyboardInput{wVk: uint16(vkey), dwFlags: flags},
	}
}

// characterPaletteInputs plans the palette input batch
// (window.rs character_palette_inputs): release the held modifiers,
// press and release Win+period (skipping the synthetic Win press when
// the user really holds a Windows key), then restore the modifiers.
func characterPaletteInputs(heldModifiers []uint8, winHeld bool) []sendInputRecord {
	inputs := make([]sendInputRecord, 0, len(heldModifiers)*2+4)
	for _, key := range heldModifiers {
		inputs = append(inputs, characterPaletteKey(key, true))
	}
	if !winHeld {
		inputs = append(inputs, characterPaletteKey(vkLwin, false))
	}
	inputs = append(inputs, characterPaletteKey(vkOemPeriodByte, false))
	inputs = append(inputs, characterPaletteKey(vkOemPeriodByte, true))
	if !winHeld {
		inputs = append(inputs, characterPaletteKey(vkLwin, true))
	}
	for _, key := range heldModifiers {
		inputs = append(inputs, characterPaletteKey(key, false))
	}
	return inputs
}

// characterPaletteCleanupInputs plans the partial-send cleanup batch
// (window.rs show_character_palette failure branch): release the
// synthetic period, release the synthetic Win unless the user holds it,
// and re-press the caller's modifiers.
func characterPaletteCleanupInputs(heldModifiers []uint8, winHeld bool) []sendInputRecord {
	cleanup := []sendInputRecord{characterPaletteKey(vkOemPeriodByte, true)}
	if !winHeld {
		cleanup = append(cleanup, characterPaletteKey(vkLwin, true))
	}
	for _, key := range heldModifiers {
		cleanup = append(cleanup, characterPaletteKey(key, false))
	}
	return cleanup
}

// sendInputs is the SendInput seam: the default performs the real
// call; tests substitute recorders so no synthetic input is ever
// injected. It reports how many events were accepted.
var sendInputs = realSendInputs

// realSendInputs injects the events through SendInput.
func realSendInputs(inputs []sendInputRecord) int {
	if len(inputs) == 0 {
		return 0
	}
	r, _, _ := procSendInput.Call(
		uintptr(len(inputs)),
		uintptr(unsafe.Pointer(&inputs[0])),
		uintptr(unsafe.Sizeof(inputs[0])))
	return int(r)
}

// characterPaletteModifierKeys are the modifiers the palette releases
// (window.rs MODIFIER_KEYS): both controls, both shifts and both alts.
var characterPaletteModifierKeys = [6]uint8{
	vkLcontrol, vkRcontrolByte, vkLshift, vkRshift, vkLmenu, vkRmenuByte,
}

// heldCharacterPaletteModifiers reads the held modifier keys through
// GetAsyncKeyState (negative = held).
func heldCharacterPaletteModifiers() []uint8 {
	var held []uint8
	for _, key := range characterPaletteModifierKeys {
		state, _, _ := procGetAsyncKeyState.Call(uintptr(key))
		if int16(uint16(state)) < 0 {
			held = append(held, key)
		}
	}
	return held
}

// winKeyHeld reports whether either Windows key is held.
func winKeyHeld() bool {
	for _, key := range [2]uint8{vkLwin, vkRwin} {
		state, _, _ := procGetAsyncKeyState.Call(uintptr(key))
		if int16(uint16(state)) < 0 {
			return true
		}
	}
	return false
}

// ShowCharacterPalette opens the Windows character palette for this
// window (window.rs show_character_palette). SendInput targets the
// foreground window, so an inactive window refuses instead of opening
// the picker for another application. A partial send triggers the
// cleanup batch and reports the failure; a successful batch reports
// nil.
func (wh WindowHandle) ShowCharacterPalette() error {
	if wh.host == nil {
		return ErrWindowClosed
	}
	_, err := queryForeground(wh.host, func() (struct{}, error) {
		if wh.record() == nil {
			return struct{}{}, ErrWindowClosed
		}
		foreground, _, _ := procGetForegroundWindow.Call()
		if foreground != wh.hwnd {
			return struct{}{}, fmt.Errorf("gpui: cannot show the character palette for an inactive window")
		}
		held := heldCharacterPaletteModifiers()
		winHeld := winKeyHeld()
		inputs := characterPaletteInputs(held, winHeld)
		sent := sendInputs(inputs)
		if sent != len(inputs) {
			// Partial send: the synthetic keys must not stay down and
			// the caller's modifiers must be restored.
			sendInputs(characterPaletteCleanupInputs(held, winHeld))
			return struct{}{}, fmt.Errorf("gpui: failed to open the Windows character palette: sent %d of %d input events", sent, len(inputs))
		}
		return struct{}{}, nil
	})
	return err
}
