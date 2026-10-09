//go:build windows

package gpui

// This file ports the Windows keyboard translation of the pinned CE
// reference 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a:
//
//   - crates/gpui_windows/src/keyboard.rs — the immutable-key names,
//     the shifted-key conversion for OEM/digit virtual keys, the
//     key-from-vkey mapping through MapVirtualKeyW(MAPVK_VK_TO_CHAR),
//     and the WindowsKeyboardLayout id/name lookup
//     (GetKeyboardLayoutNameW + the HKLM keyboard-layouts registry
//     "Layout Text" value);
//   - crates/gpui_windows/src/events.rs — process_key (the dead-key
//     and AltGr detection through ToUnicode with the live keyboard
//     state, the with/without-modifiers comparison that yields
//     prefer_character_input), parse_normal_key, the modifier-key
//     ModifiersChanged synthesis with capslock, current_modifiers via
//     GetKeyState, and parse_char_message's UTF-16 surrogate assembly.
//
// All functions run on the host (foreground) thread, exactly like the
// reference's window procedure.

import (
	"fmt"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

// ---------------------------------------------------------------------------
// Win32 keyboard bindings
// ---------------------------------------------------------------------------

var (
	procGetKeyboardState       = modUser32.NewProc("GetKeyboardState")
	procToUnicode              = modUser32.NewProc("ToUnicode")
	procMapVirtualKeyW         = modUser32.NewProc("MapVirtualKeyW")
	procGetKeyState            = modUser32.NewProc("GetKeyState")
	procGetKeyboardLayoutNameW = modUser32.NewProc("GetKeyboardLayoutNameW")

	modAdvapi32          = syscall.NewLazyDLL("advapi32.dll")
	procRegOpenKeyExW    = modAdvapi32.NewProc("RegOpenKeyExW")
	procRegQueryValueExW = modAdvapi32.NewProc("RegQueryValueExW")
	procRegCloseKey      = modAdvapi32.NewProc("RegCloseKey")
)

// Win32 keyboard constants (winuser.h).
const (
	wmKeyDown         = 0x0100
	wmKeyUp           = 0x0101
	wmChar            = 0x0102
	wmDeadChar        = 0x0103
	wmSysKeyDown      = 0x0104
	wmSysKeyUp        = 0x0105
	wmSysChar         = 0x0106
	wmSysDeadChar     = 0x0107
	wmInputLangChange = 0x0051

	// Reference message ids (events.rs consts).
	wmGPUIKeyDown               = wmUser + 8 // WM_GPUI_KEYDOWN
	wmGPUIKeyboardLayoutChanged = wmUser + 6 // WM_GPUI_KEYBOARD_LAYOUT_CHANGED

	mapVKVKToVSC  = 0 // MAPVK_VK_TO_VSC
	mapVKVKToChar = 2 // MAPVK_VK_TO_CHAR

	klNameLength = 9 // KL_NAMELENGTH (includes the null terminator)

	// hklmRoot is HKEY_LOCAL_MACHINE's predefined-handle value
	// (0x80000002). RegOpenKeyExW requires the real handle: passing
	// NULL fails with ERROR_INVALID_HANDLE (latent in readLayoutText
	// until ticket20's IME availability enumeration read the same tree
	// and exposed it — every layout-name lookup failed and fell back
	// to "unknown").
	hklmRoot = uintptr(0x80000002)

	hklmKeyRead = 0x20019 // KEY_READ
	rrfRegSz    = 0x00002 // RRF_RT_REG_SZ (the query returns REG_SZ data)
)

// Virtual-key codes used by the translation (winuser.h VK_*).
const (
	vkShift          = 0x10
	vkControl        = 0x11
	vkMenu           = 0x12
	vkCapital        = 0x14
	vkEscape         = 0x1B
	vkSpace          = 0x20
	vkPrior          = 0x21
	vkNext           = 0x22
	vkEnd            = 0x23
	vkHome           = 0x24
	vkLeft           = 0x25
	vkUp             = 0x26
	vkRight          = 0x27
	vkDown           = 0x28
	vkInsert         = 0x2D
	vkDelete         = 0x2E
	vkLwin           = 0x5B
	vkRwin           = 0x5C
	vkApps           = 0x5D
	vkNumlock        = 0x90
	vkScroll         = 0x91
	vkLshift         = 0xA0
	vkRshift         = 0xA1
	vkLcontrol       = 0xA2
	vkRcontrol       = 0xA3
	vkLmenu          = 0xA4
	vkRmenu          = 0xA5
	vkBrowserBack    = 0xA6
	vkBrowserForward = 0xA7
	vkPacket         = 0xE7

	vkOem1      = 0xBA // ;:
	vkOemPlus   = 0xBB // =+
	vkOemComma  = 0xBC // ,<
	vkOemMinus  = 0xBD // -_
	vkOemPeriod = 0xBE // .>
	vkOem2      = 0xBF // /?
	vkOem3      = 0xC0 // `~
	vkAbntC1    = 0xC1
	vkOem4      = 0xDB // [{
	vkOem5      = 0xDC // \|
	vkOem6      = 0xDD // ]}
	vkOem7      = 0xDE // '"
	vkOem8      = 0xDF
	vkOem102    = 0xE2
)

// ---------------------------------------------------------------------------
// Keyboard layout (keyboard.rs WindowsKeyboardLayout)
// ---------------------------------------------------------------------------

// KeyboardLayoutInfo identifies the active keyboard layout
// (WindowsKeyboardLayout: the KLID and its registry layout text).
type KeyboardLayoutInfo struct {
	// ID is the keyboard layout id (GetKeyboardLayoutNameW, e.g.
	// "00000409").
	ID string
	// Name is the layout's display name (the HKLM keyboard-layouts
	// "Layout Text" value).
	Name string
}

// unknownKeyboardLayout is the fallback when the layout cannot be read
// (WindowsKeyboardLayout::unknown).
func unknownKeyboardLayout() KeyboardLayoutInfo {
	return KeyboardLayoutInfo{ID: "unknown", Name: "unknown"}
}

// currentKeyboardLayout reads the active keyboard layout
// (WindowsKeyboardLayout::new: GetKeyboardLayoutNameW + the registry
// lookup; the unknown fallback on failure).
func currentKeyboardLayout() KeyboardLayoutInfo {
	buffer := make([]uint16, klNameLength)
	ret, _, _ := procGetKeyboardLayoutNameW.Call(uintptr(unsafe.Pointer(&buffer[0])))
	if ret == 0 {
		return unknownKeyboardLayout()
	}
	id := strings.TrimRight(string(utf16.Decode(buffer)), "\x00")
	if id == "" {
		return unknownKeyboardLayout()
	}
	name := readLayoutText(id)
	if name == "" {
		return unknownKeyboardLayout()
	}
	return KeyboardLayoutInfo{ID: id, Name: name}
}

// readLayoutText reads a KLID's "Layout Text" from the HKLM
// keyboard-layouts registry (advapi32 via syscalls; the repo stays
// dependency-free).
func readLayoutText(id string) string {
	path := fmt.Sprintf("System\\CurrentControlSet\\Control\\Keyboard Layouts\\%s", id)
	pathUTF16, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return ""
	}
	var key uintptr
	ret, _, _ := procRegOpenKeyExW.Call(
		hklmRoot, // HKEY_LOCAL_MACHINE
		uintptr(unsafe.Pointer(pathUTF16)),
		0, hklmKeyRead, uintptr(unsafe.Pointer(&key)))
	if ret != 0 || key == 0 {
		return ""
	}
	defer procRegCloseKey.Call(key)

	valueUTF16, err := syscall.UTF16PtrFromString("Layout Text")
	if err != nil {
		return ""
	}
	var dataType uint32
	var dataSize uint32
	ret, _, _ = procRegQueryValueExW.Call(
		key,
		uintptr(unsafe.Pointer(valueUTF16)),
		0,
		uintptr(unsafe.Pointer(&dataType)),
		0, // NULL: query the size only
		uintptr(unsafe.Pointer(&dataSize)))
	if ret != 0 || dataSize == 0 {
		return ""
	}
	data := make([]byte, dataSize)
	ret, _, _ = procRegQueryValueExW.Call(
		key,
		uintptr(unsafe.Pointer(valueUTF16)),
		0,
		0,
		uintptr(unsafe.Pointer(&data[0])),
		uintptr(unsafe.Pointer(&dataSize)))
	if ret != 0 {
		return ""
	}
	// REG_SZ: UTF-16 code units.
	units := make([]uint16, dataSize/2)
	for i := range units {
		units[i] = uint16(data[2*i]) | uint16(data[2*i+1])<<8
	}
	return strings.TrimRight(string(utf16.Decode(units)), "\x00")
}

// ---------------------------------------------------------------------------
// Modifiers (events.rs current_modifiers / current_capslock)
// ---------------------------------------------------------------------------

// win32CurrentModifiers reads the live modifier state (GetKeyState's
// high bit).
func win32CurrentModifiers() Modifiers {
	return Modifiers{
		Control:  isVirtualKeyPressed(vkControl),
		Alt:      isVirtualKeyPressed(vkMenu),
		Shift:    isVirtualKeyPressed(vkShift),
		Platform: isVirtualKeyPressed(vkLwin) || isVirtualKeyPressed(vkRwin),
		Function: false,
	}
}

// win32CurrentCapslock reads the capslock toggle state.
func win32CurrentCapslock() Capslock {
	state, _, _ := procGetKeyState.Call(vkCapital)
	return Capslock{On: state&1 > 0}
}

func isVirtualKeyPressed(vkey uint8) bool {
	state, _, _ := procGetKeyState.Call(uintptr(vkey))
	return state&0x8000 > 0
}

// ---------------------------------------------------------------------------
// Keystroke translation (events.rs handle_key_event / parse_normal_key
// / process_key, keyboard.rs get_keystroke_key)
// ---------------------------------------------------------------------------

// win32ModifierEvent reports the ModifiersChangedEvent a
// modifier-key message synthesizes, or nil when the state did not
// change (the reference's last_reported_modifiers/capslock dedup).
func win32ModifierEvent(vkey uint8, lastModifiers *Modifiers, lastCapslock *Capslock) *ModifiersChangedEvent {
	modifiers := win32CurrentModifiers()
	switch vkey {
	case vkShift, vkControl, vkMenu, vkLmenu, vkRmenu, vkLwin, vkRwin:
		if lastModifiers != nil && *lastModifiers == modifiers {
			return nil
		}
		if lastModifiers != nil {
			*lastModifiers = modifiers
		}
		return &ModifiersChangedEvent{Modifiers: modifiers, Capslock: win32CurrentCapslock()}
	case vkPacket:
		return nil
	case vkCapital:
		capslock := win32CurrentCapslock()
		if lastCapslock != nil && *lastCapslock == capslock {
			return nil
		}
		if lastCapslock != nil {
			*lastCapslock = capslock
		}
		return &ModifiersChangedEvent{Modifiers: modifiers, Capslock: capslock}
	}
	return nil
}

// win32TranslateKeyDown translates a WM_GPUI_KEYDOWN/WM_KEYUP message
// into a keystroke plus its prefer-character-input flag
// (parse_normal_key + process_key's second return), or returns !ok
// when the virtual key produces no keystroke (the reference's Option
// path).
func win32TranslateKeyDown(vkey uint8, lparam uintptr) (keystroke Keystroke, preferCharacterInput bool, ok bool) {
	modifiers := win32CurrentModifiers()
	keyChar, preferCharacterInput := processKey(vkey, uint8(hiword(lparam)&0xFF))
	key, ok := keystrokeKeyOf(vkey, uint8(hiword(lparam)&0xFF), &modifiers)
	if !ok {
		return Keystroke{}, false, false
	}
	return Keystroke{Modifiers: modifiers, Key: key, KeyChar: keyChar}, preferCharacterInput, true
}

// keystrokeKeyOf resolves the keystroke key: the immutable names first,
// then the layout-dependent key with shifted-key conversion
// (parse_immutable + get_keystroke_key).
func keystrokeKeyOf(vkey uint8, scanCode uint8, modifiers *Modifiers) (string, bool) {
	if key, ok := parseImmutableKey(vkey); ok {
		return key, true
	}
	if modifiers.Shift && needToConvertToShiftedKey(vkey) {
		if shifted, ok := getShiftedKey(vkey, uint32(scanCode)); ok {
			modifiers.Shift = false
			return shifted, true
		}
		return "", false
	}
	return keyFromVKey(vkey)
}

// parseImmutableKey maps immutable virtual keys to their names
// (parse_immutable).
func parseImmutableKey(vkey uint8) (string, bool) {
	switch vkey {
	case vkSpace:
		return "space", true
	case 0x08:
		return "backspace", true
	case 0x0D:
		return "enter", true
	case 0x09:
		return "tab", true
	case vkUp:
		return "up", true
	case vkDown:
		return "down", true
	case vkRight:
		return "right", true
	case vkLeft:
		return "left", true
	case vkHome:
		return "home", true
	case vkEnd:
		return "end", true
	case vkPrior:
		return "pageup", true
	case vkNext:
		return "pagedown", true
	case vkBrowserBack:
		return "back", true
	case vkBrowserForward:
		return "forward", true
	case vkEscape:
		return "escape", true
	case vkInsert:
		return "insert", true
	case vkDelete:
		return "delete", true
	case vkApps:
		return "menu", true
	case 0x30 + 0, 0x30 + 1, 0x30 + 2, 0x30 + 3, 0x30 + 4,
		0x30 + 5, 0x30 + 6, 0x30 + 7, 0x30 + 8, 0x30 + 9:
		// Digits are immutable in name (the shifted conversion runs in
		// get_keystroke_key through need_to_convert_to_shifted_key; the
		// reference's parse_immutable does NOT cover digits, so digits
		// fall through to the layout mapping).
	}
	if vkey >= 0x70 && vkey <= 0x87 { // VK_F1..VK_F24
		return fmt.Sprintf("f%d", int(vkey)-0x70+1), true
	}
	return "", false
}

// needToConvertToShiftedKey reports whether the shifted form should be
// resolved (need_to_convert_to_shifted_key).
func needToConvertToShiftedKey(vkey uint8) bool {
	switch vkey {
	case vkOem3, vkOemMinus, vkOemPlus, vkOem4, vkOem5, vkOem6,
		vkOem1, vkOem7, vkOemComma, vkOemPeriod, vkOem2, vkOem102,
		vkOem8, vkAbntC1,
		0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39:
		return true
	}
	return false
}

// getShiftedKey resolves the shifted key of a virtual key
// (get_shifted_key: ToUnicode with only shift pressed).
func getShiftedKey(vkey uint8, scanCode uint32) (string, bool) {
	return generateKeyChar(vkey, scanCode, false, true, false)
}

// keyFromVKey maps the virtual key to its layout character
// (get_key_from_vkey: MapVirtualKeyW(MAPVK_VK_TO_CHAR), dead-key flag
// in the high word dropped, lowercased).
func keyFromVKey(vkey uint8) (string, bool) {
	keyData, _, _ := procMapVirtualKeyW.Call(uintptr(vkey), mapVKVKToChar)
	if keyData == 0 {
		return "", false
	}
	r := rune(keyData & 0xFFFF)
	if r == 0 {
		return "", false
	}
	return strings.ToLower(string(r)), true
}

// processKey resolves the typed character and whether text input
// should win over bindings (process_key): dead keys report
// prefer_character_input; AltGr-like combos (the key char changes when
// the modifiers are removed) report it too.
func processKey(vkey uint8, scanCode uint8) (keyChar string, preferCharacterInput bool) {
	keyboardState := make([]byte, 256)
	ret, _, _ := procGetKeyboardState.Call(uintptr(unsafe.Pointer(&keyboardState[0])))
	if ret == 0 {
		return "", false
	}

	buffer := make([]uint16, 8)
	resultC, _, _ := procToUnicode.Call(
		uintptr(vkey), uintptr(scanCode),
		uintptr(unsafe.Pointer(&keyboardState[0])),
		uintptr(unsafe.Pointer(&buffer[0])), 0x4)

	if resultC == 0 {
		return "", false
	}
	count := int(resultC)
	if count < 0 {
		count = -count
	}
	keyChar = utf16String(buffer[:count])
	if keyChar == "" || isControlString(keyChar) {
		keyChar = ""
	}

	if resultC < 0 {
		// A dead key: prefer the character input so composition works.
		return keyChar, true
	}
	if keyChar == "" {
		return "", false
	}

	ctrlDown := keyboardState[vkControl]&0x80 != 0
	altDown := keyboardState[vkMenu]&0x80 != 0
	winDown := keyboardState[vkLwin]&0x80 != 0 || keyboardState[vkRwin]&0x80 != 0
	if !ctrlDown && !altDown && !winDown {
		return keyChar, false
	}

	// Re-translate with the modifiers cleared: when the result differs,
	// the character depended on the modifiers (AltGr), so text input
	// wins over the ctrl/alt binding.
	stateNoModifiers := append([]byte{}, keyboardState...)
	stateNoModifiers[vkControl] = 0
	stateNoModifiers[vkLcontrol] = 0
	stateNoModifiers[vkRcontrol] = 0
	stateNoModifiers[vkMenu] = 0
	stateNoModifiers[vkLmenu] = 0
	stateNoModifiers[vkRmenu] = 0
	stateNoModifiers[vkLwin] = 0
	stateNoModifiers[vkRwin] = 0

	bufferNoModifiers := make([]uint16, 8)
	resultNoModifiers, _, _ := procToUnicode.Call(
		uintptr(vkey), uintptr(scanCode),
		uintptr(unsafe.Pointer(&stateNoModifiers[0])),
		uintptr(unsafe.Pointer(&bufferNoModifiers[0])), 0x4)

	countNoModifiers := int(resultNoModifiers)
	if countNoModifiers < 0 {
		countNoModifiers = -countNoModifiers
	}
	noModifiers := utf16String(bufferNoModifiers[:countNoModifiers])
	return keyChar, resultC != resultNoModifiers || keyChar != noModifiers
}

// generateKeyChar translates a key with explicit modifier state
// (keyboard.rs generate_key_char, used for shifted keys).
func generateKeyChar(vkey uint8, scanCode uint32, control, shift, alt bool) (string, bool) {
	state := make([]byte, 256)
	if control {
		state[vkControl] = 0x80
	}
	if shift {
		state[vkShift] = 0x80
	}
	if alt {
		state[vkMenu] = 0x80
	}
	buffer := make([]uint16, 8)
	length, _, _ := procToUnicode.Call(
		uintptr(vkey), uintptr(scanCode),
		uintptr(unsafe.Pointer(&state[0])),
		uintptr(unsafe.Pointer(&buffer[0])), 0x5)
	count := int(length)
	if count > 0 {
		text := utf16String(buffer[:count])
		if text != "" && !isControlString(text) {
			return text, true
		}
		return "", false
	}
	if count < 0 {
		// A dead key: the reference returns the buffer contents.
		return utf16String(buffer[:-count]), true
	}
	return "", false
}

func utf16String(units []uint16) string {
	return string(utf16.Decode(units))
}

func isControlString(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7F {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// The Windows keyboard mapper (keyboard.rs WindowsKeyboardMapper)
// ---------------------------------------------------------------------------

// WindowsKeyboardMapper maps user-facing keybindings through the
// actual keyboard layout (PlatformKeyboardMapper::map_key_equivalent):
// binding keystrokes written for one layout resolve to the physical
// keys of the active layout, with Windows display fields (a ctrl-@
// binding maps to display ctrl-shift-2 on a US layout).
type WindowsKeyboardMapper struct {
	keyToVKey     map[string]vkeyEntry
	vkeyToKey     map[uint8]string
	vkeyToShifted map[uint8]string
}

// vkeyEntry maps a key name to its virtual key and shifted-ness.
type vkeyEntry struct {
	vkey    uint8
	shifted bool
}

// candidateVKeys are the layout-dependent keys the mapper resolves
// (keyboard.rs CANDIDATE_VKEYS).
var candidateVKeys = []uint8{
	vkOem3, vkOemMinus, vkOemPlus, vkOem4, vkOem5, vkOem6,
	vkOem1, vkOem7, vkOemComma, vkOemPeriod, vkOem2, vkOem102,
	vkOem8, vkAbntC1,
	0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39,
}

// NewWindowsKeyboardMapper builds the mapper from the active layout
// (WindowsKeyboardMapper::new: MapVirtualKeyW VK_TO_CHAR for the base
// keys and ToUnicode with shift for the shifted keys).
func NewWindowsKeyboardMapper() *WindowsKeyboardMapper {
	keyToVKey := make(map[string]vkeyEntry)
	vkeyToKey := make(map[uint8]string)
	vkeyToShifted := make(map[uint8]string)
	for _, vkey := range candidateVKeys {
		if key, ok := keyFromVKey(vkey); ok {
			keyToVKey[key] = vkeyEntry{vkey: vkey, shifted: false}
			vkeyToKey[vkey] = key
		}
		scanCode, _, _ := procMapVirtualKeyW.Call(uintptr(vkey), mapVKVKToVSC)
		if scanCode == 0 {
			continue
		}
		if shifted, ok := getShiftedKey(vkey, uint32(scanCode)); ok {
			keyToVKey[shifted] = vkeyEntry{vkey: vkey, shifted: true}
			vkeyToShifted[vkey] = shifted
		}
	}
	return &WindowsKeyboardMapper{
		keyToVKey:     keyToVKey,
		vkeyToKey:     vkeyToKey,
		vkeyToShifted: vkeyToShifted,
	}
}

// MapKeyEquivalent implements KeyboardMapper
// (WindowsKeyboardMapper::map_key_equivalent): a binding key resolves
// to its physical virtual key; a shifted binding key folds into the
// display modifiers (ctrl-$ displays as ctrl-shift-2 with the inner
// keystroke ctrl-$).
func (m *WindowsKeyboardMapper) MapKeyEquivalent(keystroke Keystroke, useKeyEquivalents bool) KeybindingKeystroke {
	entry, ok := m.keyToVKey[keystroke.Key]
	if !ok && useKeyEquivalents {
		entry, ok = vkeyFromKeyWithUSLayout(keystroke.Key)
	}
	if !ok {
		return NewKeybindingKeystroke(keystroke)
	}
	shift := entry.shifted || keystroke.Modifiers.Shift
	inner := keystroke
	inner.Modifiers.Shift = false

	key, ok := m.vkeyToKey[entry.vkey]
	if !ok {
		return NewKeybindingKeystroke(keystroke)
	}
	// The inner key takes the shifted form when shift is involved; the
	// display key is the physical (unshifted) key (map_key_equivalent:
	// keystroke.key = shifted_key / key, display = key).
	innerKey := key
	if shift {
		shifted, ok := m.vkeyToShifted[entry.vkey]
		if !ok {
			return NewKeybindingKeystroke(keystroke)
		}
		innerKey = shifted
	}
	inner.Key = innerKey
	displayModifiers := Modifiers{
		Control:  inner.Modifiers.Control,
		Alt:      inner.Modifiers.Alt,
		Platform: inner.Modifiers.Platform,
		Function: inner.Modifiers.Function,
		Shift:    shift,
	}
	return NewKeybindingKeystrokeWithDisplay(inner, displayModifiers, key)
}

// vkeyFromKeyWithUSLayout maps a key through the fixed US layout table
// (keyboard.rs get_vkey_from_key_with_us_layout).
func vkeyFromKeyWithUSLayout(key string) (vkeyEntry, bool) {
	usLayout := map[string]vkeyEntry{
		"`": {vkOem3, false}, "~": {vkOem3, true},
		"1": {0x31, false}, "!": {0x31, true},
		"2": {0x32, false}, "@": {0x32, true},
		"3": {0x33, false}, "#": {0x33, true},
		"4": {0x34, false}, "$": {0x34, true},
		"5": {0x35, false}, "%": {0x35, true},
		"6": {0x36, false}, "^": {0x36, true},
		"7": {0x37, false}, "&": {0x37, true},
		"8": {0x38, false}, "*": {0x38, true},
		"9": {0x39, false}, "(": {0x39, true},
		"0": {0x30, false}, ")": {0x30, true},
		"-": {vkOemMinus, false}, "_": {vkOemMinus, true},
		"=": {vkOemPlus, false}, "+": {vkOemPlus, true},
		"[": {vkOem4, false}, "{": {vkOem4, true},
		"]": {vkOem6, false}, "}": {vkOem6, true},
		"\\": {vkOem5, false}, "|": {vkOem5, true},
		";": {vkOem1, false}, ":": {vkOem1, true},
		"'": {vkOem7, false}, "\"": {vkOem7, true},
		",": {vkOemComma, false}, "<": {vkOemComma, true},
		".": {vkOemPeriod, false}, ">": {vkOemPeriod, true},
		"/": {vkOem2, false}, "?": {vkOem2, true},
	}
	entry, ok := usLayout[key]
	return entry, ok
}

// ---------------------------------------------------------------------------
// WM_CHAR parsing (events.rs parse_char_message)
// ---------------------------------------------------------------------------

// parseCharMessage assembles WM_CHAR text with surrogate pairing
// (parse_char_message: a high surrogate parks, a low surrogate
// combines, anything else resets the pending surrogate; control
// characters are dropped).
func parseCharMessage(wparam uintptr, pendingSurrogate *uint16) (string, bool) {
	codeUnit := uint16(wparam)
	switch codeUnit {
	case 0:
		return "", false
	}
	if codeUnit >= 0xD800 && codeUnit <= 0xDBFF {
		// High surrogate: wait for the low surrogate.
		*pendingSurrogate = codeUnit
		return "", false
	}
	if codeUnit >= 0xDC00 && codeUnit <= 0xDFFF {
		high := *pendingSurrogate
		*pendingSurrogate = 0
		if high == 0 {
			// Invalid low surrogate without a preceding high surrogate.
			return "", false
		}
		return string(utf16.Decode([]uint16{high, codeUnit})), true
	}
	*pendingSurrogate = 0
	r := rune(codeUnit)
	if r < 0x20 || r == 0x7F {
		return "", false
	}
	return string(r), true
}
