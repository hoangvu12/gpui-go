//go:build windows

package gpui

// The Windows scan-code half of browser-core 13 (key code and
// location, with Shift kept): synthetic WM_KEYDOWN/WM_KEYUP messages —
// wParam is the virtual key, lParam carries the scan code in bits
// 16-23 and the extended-key flag in bit 24 — resolve to the DOM code
// Chromium reports (ui/events/win/events_win_utils.cc:490-495
// GetScanCodeFromLParam, keyboard_code_conversion_win.cc:31-49
// CodeForWindowsScanCode, dom_code_data.inc's win column). No real
// keyboard is involved: the scan-code table is layout-independent, and
// the layout-dependent tests below skip unless the active layout is US
// (the ticket's stated US assumption), so no ToUnicode injection is
// needed for the table itself.

import "testing"

// win32KeyLParam builds a WM_KEYDOWN/WM_KEYUP lParam: the scan code
// in bits 16-23, the extended-key flag in bit 24, a repeat count of 1
// and (in the held case) the repeat bit 30, exactly the bits Windows
// packs (msdn ms644984; the translation must read only bits 16-24).
func win32KeyLParam(scanCode uint16, extended, held bool) uintptr {
	lp := uintptr(scanCode) << 16
	lp |= 1 // repeat count
	if extended {
		lp |= 1 << 24
	}
	if held {
		lp |= 1 << 30
	}
	return lp
}

// TestWin32ScanCodeOfLParam: the scan code is lParam bits 16-23 with
// the extended flag (bit 24) as the 0xE000 prefix, and nothing else in
// the message leaks in (the repeat bit, the repeat count).
func TestWin32ScanCodeOfLParam(t *testing.T) {
	cases := []struct {
		lp   uintptr
		want uint16
	}{
		{win32KeyLParam(0x1B, false, false), 0x001B}, // ] (US)
		{win32KeyLParam(0x02, false, false), 0x0002}, // 1 (US)
		{win32KeyLParam(0x4F, false, false), 0x004F}, // numpad 1
		{win32KeyLParam(0x1B, false, true), 0x001B},  // an auto-repeat press: bit 30 is not the extended flag
		{win32KeyLParam(0x35, true, false), 0xE035},  // numpad divide (extended)
		{win32KeyLParam(0x1D, true, false), 0xE01D},  // right control (extended)
		{uintptr(0x00FF0000), 0x00FF},                // the full byte, mask-tight
		{uintptr(0x01000000), 0xE000},                // bit 24 alone: extended with scan code 0
		{uintptr(0), 0x0000},
	}
	for _, tc := range cases {
		if got := win32ScanCodeOfLParam(tc.lp); got != tc.want {
			t.Errorf("win32ScanCodeOfLParam(%#010x) = %#06x, want %#06x", tc.lp, got, tc.want)
		}
	}
}

// TestWin32KeyCodeOfMessage: synthetic key messages (wParam virtual
// key + lParam scan code and extended flag) resolve to Chromium's DOM
// code and location. The code comes from the scan code, never from the
// virtual key — the wrong-pairing cases prove it.
func TestWin32KeyCodeOfMessage(t *testing.T) {
	cases := []struct {
		name     string
		vkey     uint8
		scanCode uint16
		extended bool
		wantCode string
		wantLoc  KeyLocation
	}{
		{"BracketRight with its virtual key", vkOem6, 0x1B, false, "BracketRight", KeyLocationStandard},
		{"BracketRight with a bogus virtual key", 0x41, 0x1B, false, "BracketRight", KeyLocationStandard},
		{"Digit1", 0x31, 0x02, false, "Digit1", KeyLocationStandard},
		{"KeyA", 0x41, 0x1E, false, "KeyA", KeyLocationStandard},
		{"KeyA with a punctuation virtual key", vkOem6, 0x1E, false, "KeyA", KeyLocationStandard},
		{"Semicolon", vkOem1, 0x27, false, "Semicolon", KeyLocationStandard},
		{"Space", vkSpace, 0x39, false, "Space", KeyLocationStandard},
		{"Tab", 0x09, 0x0F, false, "Tab", KeyLocationStandard},
		{"Enter", 0x0D, 0x1C, false, "Enter", KeyLocationStandard},
		{"ShiftLeft", vkLshift, 0x2A, false, "ShiftLeft", KeyLocationLeft},
		{"ShiftRight", vkRshift, 0x36, false, "ShiftRight", KeyLocationRight},
		{"ControlRight", vkRcontrol, 0x1D, true, "ControlRight", KeyLocationRight},
		{"AltRight", vkRmenu, 0x38, true, "AltRight", KeyLocationRight},
		{"MetaLeft", vkLwin, 0x5B, true, "MetaLeft", KeyLocationLeft},
		{"Numpad1", 0x61, 0x4F, false, "Numpad1", KeyLocationNumpad},
		{"Numpad1 with the digit's virtual key", 0x31, 0x4F, false, "Numpad1", KeyLocationNumpad},
		{"NumpadDivide", 0x6F, 0x35, true, "NumpadDivide", KeyLocationNumpad},
		{"NumpadEnter", 0x0D, 0x1C, true, "NumpadEnter", KeyLocationNumpad},
		{"NumpadDecimal", 0x6E, 0x53, false, "NumpadDecimal", KeyLocationNumpad},
		{"Home (extended)", 0x24, 0x47, true, "Home", KeyLocationStandard},
		{"ArrowDown (extended)", vkDown, 0x50, true, "ArrowDown", KeyLocationStandard},
		{"ContextMenu", vkApps, 0x5D, true, "ContextMenu", KeyLocationStandard},
		{"F1", 0x70, 0x3B, false, "F1", KeyLocationStandard},
		{"PrintScreen (Alt+PrintScreen's scan code 0x54)", 0x2C, 0x54, false, "PrintScreen", KeyLocationStandard},
		{"Pause (Ctrl+Pause's extended scan code 0x46)", 0x13, 0x46, true, "Pause", KeyLocationStandard},
		{"an unknown scan code has no code", 0x41, 0x00, false, "", KeyLocationStandard},
		{"a scan code the table does not cover", 0x41, 0x0090, false, "", KeyLocationStandard},
	}
	for _, tc := range cases {
		scanCode := win32ScanCodeOfLParam(win32KeyLParam(tc.scanCode, tc.extended, false))
		if got := domCodeOfScanCode(scanCode); got != tc.wantCode {
			t.Errorf("%s: domCodeOfScanCode(%#06x) = %q, want %q", tc.name, scanCode, got, tc.wantCode)
			continue
		}
		if tc.wantCode == "" {
			continue
		}
		if got := domKeyLocationOfCode(tc.wantCode); got != tc.wantLoc {
			t.Errorf("%s: domKeyLocationOfCode(%q) = %v, want %v", tc.name, tc.wantCode, got, tc.wantLoc)
		}
	}
}

// requireUSLayout skips the layout-dependent tests unless the active
// keyboard layout is US (00000409): the shifted-key conversion and the
// layout character go through ToUnicode/MapVirtualKey with the live
// layout, and these tests state the US assumption instead of injecting
// one (the ticket's rule).
func requireUSLayout(t *testing.T) {
	t.Helper()
	if layout := currentKeyboardLayout(); layout.ID != "00000409" {
		t.Skipf("the active keyboard layout is %s (%s); the shifted-key conversion needs US", layout.ID, layout.Name)
	}
}

// TestWin32TranslateKeyDownCarriesCodeAndLocation: the whole
// translation reports the DOM's key/code pair with the location — Key
// "]", Code "BracketRight", standard location — for a plain US
// BracketRight press. The virtual key is passed as the message's
// wParam; the code still comes from the lParam's scan code.
func TestWin32TranslateKeyDownCarriesCodeAndLocation(t *testing.T) {
	requireUSLayout(t)
	keystroke, preferCharacterInput, ok := win32TranslateKeyDown(vkOem6, win32KeyLParam(0x1B, false, false))
	if !ok {
		t.Fatal("the BracketRight translation reported no keystroke")
	}
	want := Keystroke{Key: "]", KeyChar: "]", Code: "BracketRight", Location: KeyLocationStandard}
	if keystroke != want {
		t.Errorf("win32TranslateKeyDown(BracketRight) = %+v, want %+v", keystroke, want)
	}
	if preferCharacterInput {
		t.Error("a plain BracketRight press must not prefer character input")
	}
}

// TestWin32KeystrokeKeyOfKeepsShift: the shifted-key conversion
// replaces the key but no longer clears Shift (browser-core 13's
// divergence from the pin) — Ctrl+Shift+] arrives as ctrl-shift-} and
// matches both binding forms (TestKeystrokeShouldMatchEitherShiftForm).
func TestWin32KeystrokeKeyOfKeepsShift(t *testing.T) {
	requireUSLayout(t)
	modifiers := ControlShiftModifiers()
	key, ok := keystrokeKeyOf(vkOem6, 0x1B, modifiers)
	if !ok {
		t.Fatal("the shifted BracketRight conversion reported no key")
	}
	if key != "}" {
		t.Errorf("shifted BracketRight key = %q, want }", key)
	}
	if !modifiers.Shift || !modifiers.Control {
		t.Errorf("the conversion must keep the modifiers: %+v (want shift and control still set)", modifiers)
	}
}
