package gpui

// This file ports the DOM key-code tables browser-core ticket 13 needs
// (key code and location, with Shift kept; .scratch/browser-core/
// issues/13-key-code-and-shift.md) from Chromium's
// ui/events/keycodes/dom:
//
//   - The location table is DomCodeToLocation
//     (keycode_converter.cc:351-400): the numpad codes report numpad,
//     the left/right modifier codes report left/right, everything
//     else is standard.
//
//   - The US character pairs of the writing-system keys are the
//     dom_code_data.inc comments themselves ("BracketRight // ]}",
//     "Digit1 // 1!", "Semicolon // ;:"; ui/events/keycodes/dom/
//     dom_code_data.inc:171-208) — the physical key each code names
//     and the character Shift produces on the US layout. The foldable
//     set (digits and OEM punctuation) mirrors the Windows
//     translation's need_to_convert_to_shifted_key
//     (gpui/keyboard_win32.go): those are the keys whose Key folds to
//     the shifted character; letters never fold (the pin reports
//     Key "a" for Shift+A).
//
// The scan-code half of the translation — lParam bits 16-23 plus the
// extended bit 24 to a DOM code — lives in
// gpui/keycode_win32.go. Code strings here are layout-independent DOM
// facts, so this file carries no build tag.

import "fmt"

// ---------------------------------------------------------------------------
// The DOM code tables
// ---------------------------------------------------------------------------

// domCodeUSUnshifted maps the writing-system DOM codes onto their US
// unshifted characters (dom_code_data.inc's code comments; KeyA → "a",
// Digit1 → "1", BracketRight → "]"). Matching either binding form
// (Keystroke.ShouldMatch) resolves a typed code's physical key through
// it, and WithSimulatedCode folds a shifted shiftable key with it.
var domCodeUSUnshifted = map[string]string{}

// domCodeUSShifted maps the foldable DOM codes onto their US shifted
// characters (Digit1 → "!", BracketRight → "}"): exactly the keys the
// Windows translation folds (need_to_convert_to_shifted_key's OEM and
// digit virtual keys). Letters are absent on purpose — the platform
// keeps Key "a" under Shift.
var domCodeUSShifted = map[string]string{}

// usDomCodeOfKeyLookup maps a keystroke key onto its DOM code: the US
// unshifted characters ("]" → "BracketRight") and the immutable names
// the keystroke grammar spells ("tab" → "Tab", "arrowdown" →
// "ArrowDown"). WithSimulatedCode uses it to give a parsed keystroke
// the code the platform would report.
var usDomCodeOfKeyLookup = map[string]string{}

// domCodeLocations maps the codes with a non-standard location
// (keycode_converter.cc DomCodeToLocation's kLocations table). Codes
// absent from the map are standard.
var domCodeLocations = map[string]KeyLocation{}

func init() {
	// The letter, digit and numpad-digit runs (DomCodeToCodeString
	// generates the same runs; keycode_converter.cc:323-337).
	for i := 0; i < 26; i++ {
		code := string(rune('A' + i)) // "A".."Z" → the KeyA..KeyZ suffix
		key := string(rune('a' + i))
		domCodeUSUnshifted["Key"+code] = key
		usDomCodeOfKeyLookup[key] = "Key" + code
	}
	for i := 0; i < 10; i++ {
		digit := string(rune('0' + i))
		domCodeUSUnshifted["Digit"+digit] = digit
		domCodeUSUnshifted["Numpad"+digit] = digit
		usDomCodeOfKeyLookup[digit] = "Digit" + digit
	}
	for i := 1; i <= 24; i++ {
		usDomCodeOfKeyLookup[fmt.Sprintf("f%d", i)] = fmt.Sprintf("F%d", i)
	}

	// The US punctuation pairs (dom_code_data.inc:188-208) — every
	// entry folds under Shift, matching the Windows translation's
	// shiftable set.
	for _, pair := range [][2]string{
		{"Backquote", "`"}, {"Minus", "-"}, {"Equal", "="},
		{"BracketLeft", "["}, {"BracketRight", "]"}, {"Backslash", "\\"},
		{"Semicolon", ";"}, {"Quote", "'"}, {"Comma", ","},
		{"Period", "."}, {"Slash", "/"},
	} {
		domCodeUSUnshifted[pair[0]] = pair[1]
		usDomCodeOfKeyLookup[pair[1]] = pair[0]
	}
	for _, pair := range [][2]string{
		{"Backquote", "~"}, {"Minus", "_"}, {"Equal", "+"},
		{"BracketLeft", "{"}, {"BracketRight", "}"}, {"Backslash", "|"},
		{"Semicolon", ":"}, {"Quote", "\""}, {"Comma", "<"},
		{"Period", ">"}, {"Slash", "?"},
	} {
		domCodeUSShifted[pair[0]] = pair[1]
	}
	// The digits' shifted characters ("1!" through "0)").
	for i, shifted := range [10]string{")", "!", "@", "#", "$", "%", "^", "&", "*", "("} {
		domCodeUSShifted["Digit"+string(rune('0'+i))] = shifted
	}

	// Space and the remaining numpad characters.
	domCodeUSUnshifted["Space"] = " "
	usDomCodeOfKeyLookup[" "] = "Space"
	for _, pair := range [][2]string{
		{"NumpadDecimal", "."}, {"NumpadAdd", "+"}, {"NumpadSubtract", "-"},
		{"NumpadMultiply", "*"}, {"NumpadDivide", "/"}, {"NumpadEqual", "="},
	} {
		domCodeUSUnshifted[pair[0]] = pair[1]
	}

	// The immutable key names the keystroke grammar spells, mapped to
	// their codes (dom_code_data.inc's win column, the scan codes the
	// Windows translation resolves).
	for _, pair := range [][2]string{
		{"tab", "Tab"}, {"enter", "Enter"}, {"escape", "Escape"},
		{"space", "Space"}, {"backspace", "Backspace"}, {"delete", "Delete"},
		{"insert", "Insert"}, {"home", "Home"}, {"end", "End"},
		{"pageup", "PageUp"}, {"pagedown", "PageDown"},
		{"arrowup", "ArrowUp"}, {"arrowdown", "ArrowDown"},
		{"arrowleft", "ArrowLeft"}, {"arrowright", "ArrowRight"},
		{"back", "BrowserBack"}, {"forward", "BrowserForward"},
		{"menu", "ContextMenu"}, {"printscreen", "PrintScreen"},
		{"pause", "Pause"}, {"capslock", "CapsLock"},
		{"numlock", "NumLock"}, {"scrolllock", "ScrollLock"},
		{"shift", "ShiftLeft"}, {"control", "ControlLeft"}, {"alt", "AltLeft"},
	} {
		usDomCodeOfKeyLookup[pair[0]] = pair[1]
	}

	// The non-standard locations (keycode_converter.cc:355-400,
	// kLocations): the numpad keys and the left/right modifier keys.
	for _, code := range []string{
		"ControlLeft", "ShiftLeft", "AltLeft", "MetaLeft",
	} {
		domCodeLocations[code] = KeyLocationLeft
	}
	for _, code := range []string{
		"ControlRight", "ShiftRight", "AltRight", "MetaRight",
	} {
		domCodeLocations[code] = KeyLocationRight
	}
	for _, code := range []string{
		"NumpadDivide", "NumpadMultiply", "NumpadSubtract",
		"NumpadAdd", "NumpadEnter", "NumpadDecimal", "NumpadEqual",
		"NumpadComma",
	} {
		domCodeLocations[code] = KeyLocationNumpad
	}
	// The numpad digits' run ("Numpad0".."Numpad9").
	for i := 0; i < 10; i++ {
		domCodeLocations["Numpad"+string(rune('0'+i))] = KeyLocationNumpad
	}
}

// unshiftedUSKeyOfCode returns the US unshifted character of a DOM
// code ("BracketRight" → "]", "" is never a key). It reports false for
// an unknown code or an empty one, which is how ShouldMatch keeps
// code-less keystrokes on the exact-match path.
func unshiftedUSKeyOfCode(code string) (string, bool) {
	key, ok := domCodeUSUnshifted[code]
	return key, ok
}

// shiftedUSKeyOfCode returns the US shifted character of a foldable
// DOM code ("BracketRight" → "}"). Only the digits and OEM punctuation
// fold; letters report false.
func shiftedUSKeyOfCode(code string) (string, bool) {
	key, ok := domCodeUSShifted[code]
	return key, ok
}

// usDomCodeOfKey maps a keystroke key onto its DOM code: the US
// unshifted characters and the immutable names. It returns "" for
// keys with no physical identity in the tables.
func usDomCodeOfKey(key string) string {
	return usDomCodeOfKeyLookup[key]
}

// domKeyLocationOfCode returns the location a DOM code implies
// (DomCodeToLocation): numpad for the keypad codes, left/right for the
// split modifiers, standard for everything else.
func domKeyLocationOfCode(code string) KeyLocation {
	if location, ok := domCodeLocations[code]; ok {
		return location
	}
	return KeyLocationStandard
}
