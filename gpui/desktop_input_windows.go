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

import (
	"fmt"
	"unsafe"
)

var (
	procSendInput           = modUser32.NewProc("SendInput")
	procGetAsyncKeyState    = modUser32.NewProc("GetAsyncKeyState")
	procGetForegroundWindow = modUser32.NewProc("GetForegroundWindow")
)

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
