//go:build windows

package gpui

// Port of keyboard.rs test_keyboard_mapper (gpui_windows): the
// WindowsKeyboardMapper maps binding keystrokes through the machine's
// active layout. The cases mirror the reference test; on a non-US
// layout the letter-key cases still hold because letters map
// identically.

import "testing"

func TestWindowsKeyboardMapper(t *testing.T) {
	mapper := NewWindowsKeyboardMapper()

	// Normal case: ctrl-a maps identity (letters are not layout
	// candidates; the mapper passes them through).
	keystroke := Keystroke{Modifiers: ControlModifiers(), Key: "a"}
	mapped := mapper.MapKeyEquivalent(keystroke, true)
	if mapped.Inner() != (Keystroke{Modifiers: ControlModifiers(), Key: "a"}) {
		t.Errorf("mapped inner = %+v, want ctrl-a", mapped.Inner())
	}
	if mapped.Key() != "a" || mapped.Modifiers() != ControlModifiers() {
		t.Errorf("mapped display = %q %+v, want a ctrl", mapped.Key(), mapped.Modifiers())
	}

	// Shifted case through the US layout table: ctrl-$ displays as
	// ctrl-4 with the inner ctrl-$ (the reference's assert set).
	keystroke = Keystroke{Modifiers: ControlModifiers(), Key: "$"}
	mapped = mapper.MapKeyEquivalent(keystroke, true)
	if mapped.Inner() != (Keystroke{Modifiers: ControlModifiers(), Key: "$"}) {
		t.Errorf("mapped inner = %+v, want ctrl-$", mapped.Inner())
	}
	if mapped.Key() != "4" {
		t.Errorf("mapped display key = %q, want 4", mapped.Key())
	}
	if !mapped.Modifiers().Control || !mapped.Modifiers().Shift {
		t.Errorf("mapped display modifiers = %+v, want ctrl-shift", mapped.Modifiers())
	}

	// Windows style: ctrl-shift-4 folds into the inner ctrl-$ with the
	// display ctrl-4.
	keystroke = Keystroke{Modifiers: ControlShiftModifiers(), Key: "4"}
	mapped = mapper.MapKeyEquivalent(keystroke, true)
	if mapped.Inner().Modifiers != ControlModifiers() {
		t.Errorf("mapped inner modifiers = %+v, want ctrl (shift folded)", mapped.Inner().Modifiers)
	}
	if mapped.Inner().Key != "$" {
		t.Errorf("mapped inner key = %q, want $", mapped.Inner().Key)
	}
	if mapped.Key() != "4" {
		t.Errorf("mapped display key = %q, want 4", mapped.Key())
	}
	if !mapped.Modifiers().Control || !mapped.Modifiers().Shift {
		t.Errorf("mapped display modifiers = %+v, want ctrl-shift", mapped.Modifiers())
	}
}
