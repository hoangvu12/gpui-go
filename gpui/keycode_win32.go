//go:build windows

package gpui

// This file ports the Windows half of DOM KeyboardEvent.code
// (browser-core 13, key code and location with Shift kept;
// .scratch/browser-core/issues/13-key-code-and-shift.md). Chromium
// derives the code from the message's scan code, never from the
// virtual key:
//
//   - ui/events/win/events_win_utils.cc:490-495
//     (GetScanCodeFromLParam): lParam bits 16-23 carry the scan code,
//     bit 24 the extended-key flag, and the extended flag becomes the
//     0xE000 prefix of a 16-bit scan code.
//
//   - ui/events/win/events_win_utils.cc:369-372 (CodeFromMSG) and
//     ui/events/keycodes/keyboard_code_conversion_win.cc:31-49
//     (CodeForWindowsScanCode): the prefixed scan code maps onto the
//     DomCode, with four scan codes that share a physical key
//     normalized first (Alt+PrintScreen and Ctrl+Pause, the Korean
//     right-Alt Hangul/Hanja forms).
//
//   - ui/events/keycodes/dom/dom_code_data.inc: the win column is the
//     scan-code half of that map; the table below is the port of it
//     (155 entries, 0x0000/0xffff excluded, checked against the
//     original with a generator).
//
// The location is derived from the resulting code
// (gpui/keycode.go, DomCodeToLocation), and the keystroke carries both
// (Keystroke.Code / Keystroke.Location) with Shift kept
// (gpui/keyboard_win32.go).

// win32ScanCodeOfLParam extracts a WM_KEYDOWN/WM_KEYUP/WM_SYSKEYDOWN
// lParam's scan code: bits 16-23 plus the extended-key flag (bit 24)
// as the 0xE000 prefix (events_win_utils.cc GetScanCodeFromLParam).
func win32ScanCodeOfLParam(lparam uintptr) uint16 {
	scanCode := uint16(lparam >> 16 & 0xFF)
	if lparam&(1<<24) != 0 {
		scanCode |= 0xE000
	}
	return scanCode
}

// domCodeOfScanCode maps a Windows scan code onto its DOM code string
// (CodeForWindowsScanCode: the shared-key scan codes normalize first,
// then the table lookup decides). An unknown scan code reports ""
// (DomCode::NONE: the keystroke carries no code and matching keeps
// its exact-only shape).
func domCodeOfScanCode(scanCode uint16) string {
	switch scanCode {
	case 0x0054: // Alt+PrintScreen (kAltPrintScreenScanCode)
		return scanCodeToDomCode[0xE037]
	case 0xE046: // Ctrl+Pause (kCtrlPauseScanCode)
		return scanCodeToDomCode[0x0045]
	case 0xE0F1: // Korean Hanja via right Alt (kHanjaKeyKoreanLayoutScanCode)
		return scanCodeToDomCode[0x0071]
	case 0xE0F2: // Korean Han/Yeong via right Alt (kHanYeongKeyKoreanLayoutScanCode)
		return scanCodeToDomCode[0x0072]
	}
	return scanCodeToDomCode[scanCode]
}

// scanCodeToDomCode is the win column of
// ui/events/keycodes/dom/dom_code_data.inc: every Windows scan code
// (0xE000-prefixed when the key is extended) onto its DOM code string.
var scanCodeToDomCode = map[uint16]string{
	// 155 entries (dom_code_data.inc, the win column; 0x0000 and
	// 0xffff excluded).
	0x0001: "Escape",
	0x0002: "Digit1",
	0x0003: "Digit2",
	0x0004: "Digit3",
	0x0005: "Digit4",
	0x0006: "Digit5",
	0x0007: "Digit6",
	0x0008: "Digit7",
	0x0009: "Digit8",
	0x000A: "Digit9",
	0x000B: "Digit0",
	0x000C: "Minus",
	0x000D: "Equal",
	0x000E: "Backspace",
	0x000F: "Tab",
	0x0010: "KeyQ",
	0x0011: "KeyW",
	0x0012: "KeyE",
	0x0013: "KeyR",
	0x0014: "KeyT",
	0x0015: "KeyY",
	0x0016: "KeyU",
	0x0017: "KeyI",
	0x0018: "KeyO",
	0x0019: "KeyP",
	0x001A: "BracketLeft",
	0x001B: "BracketRight",
	0x001C: "Enter",
	0x001D: "ControlLeft",
	0x001E: "KeyA",
	0x001F: "KeyS",
	0x0020: "KeyD",
	0x0021: "KeyF",
	0x0022: "KeyG",
	0x0023: "KeyH",
	0x0024: "KeyJ",
	0x0025: "KeyK",
	0x0026: "KeyL",
	0x0027: "Semicolon",
	0x0028: "Quote",
	0x0029: "Backquote",
	0x002A: "ShiftLeft",
	0x002B: "Backslash",
	0x002C: "KeyZ",
	0x002D: "KeyX",
	0x002E: "KeyC",
	0x002F: "KeyV",
	0x0030: "KeyB",
	0x0031: "KeyN",
	0x0032: "KeyM",
	0x0033: "Comma",
	0x0034: "Period",
	0x0035: "Slash",
	0x0036: "ShiftRight",
	0x0037: "NumpadMultiply",
	0x0038: "AltLeft",
	0x0039: "Space",
	0x003A: "CapsLock",
	0x003B: "F1",
	0x003C: "F2",
	0x003D: "F3",
	0x003E: "F4",
	0x003F: "F5",
	0x0040: "F6",
	0x0041: "F7",
	0x0042: "F8",
	0x0043: "F9",
	0x0044: "F10",
	0x0045: "Pause",
	0x0046: "ScrollLock",
	0x0047: "Numpad7",
	0x0048: "Numpad8",
	0x0049: "Numpad9",
	0x004A: "NumpadSubtract",
	0x004B: "Numpad4",
	0x004C: "Numpad5",
	0x004D: "Numpad6",
	0x004E: "NumpadAdd",
	0x004F: "Numpad1",
	0x0050: "Numpad2",
	0x0051: "Numpad3",
	0x0052: "Numpad0",
	0x0053: "NumpadDecimal",
	0x0056: "IntlBackslash",
	0x0057: "F11",
	0x0058: "F12",
	0x0059: "NumpadEqual",
	0x0064: "F13",
	0x0065: "F14",
	0x0066: "F15",
	0x0067: "F16",
	0x0068: "F17",
	0x0069: "F18",
	0x006A: "F19",
	0x006B: "F20",
	0x006C: "F21",
	0x006D: "F22",
	0x006E: "F23",
	0x0070: "KanaMode",
	0x0071: "Lang2",
	0x0072: "Lang1",
	0x0073: "IntlRo",
	0x0076: "F24",
	0x0077: "Lang4",
	0x0078: "Lang3",
	0x0079: "Convert",
	0x007B: "NonConvert",
	0x007D: "IntlYen",
	0x007E: "NumpadComma",
	0xE008: "Undo",
	0xE00A: "Paste",
	0xE010: "MediaTrackPrevious",
	0xE017: "Cut",
	0xE018: "Copy",
	0xE019: "MediaTrackNext",
	0xE01C: "NumpadEnter",
	0xE01D: "ControlRight",
	0xE020: "AudioVolumeMute",
	0xE021: "LaunchApp2",
	0xE022: "MediaPlayPause",
	0xE024: "MediaStop",
	0xE02C: "Eject",
	0xE02E: "AudioVolumeDown",
	0xE030: "AudioVolumeUp",
	0xE032: "BrowserHome",
	0xE035: "NumpadDivide",
	0xE037: "PrintScreen",
	0xE038: "AltRight",
	0xE03B: "Help",
	0xE045: "NumLock",
	0xE047: "Home",
	0xE048: "ArrowUp",
	0xE049: "PageUp",
	0xE04B: "ArrowLeft",
	0xE04D: "ArrowRight",
	0xE04F: "End",
	0xE050: "ArrowDown",
	0xE051: "PageDown",
	0xE052: "Insert",
	0xE053: "Delete",
	0xE05B: "MetaLeft",
	0xE05C: "MetaRight",
	0xE05D: "ContextMenu",
	0xE05E: "Power",
	0xE05F: "Sleep",
	0xE063: "WakeUp",
	0xE065: "BrowserSearch",
	0xE066: "BrowserFavorites",
	0xE067: "BrowserRefresh",
	0xE068: "BrowserStop",
	0xE069: "BrowserForward",
	0xE06A: "BrowserBack",
	0xE06B: "LaunchApp1",
	0xE06C: "LaunchMail",
	0xE06D: "MediaSelect",
}
