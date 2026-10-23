package gpui

// browser-core 13's tests (key code and location, with Shift kept;
// .scratch/browser-core/issues/13-key-code-and-shift.md): a keystroke
// that carries the physical code matches a binding written in either
// form — the key form (the shifted character, no shift) or the code
// form (the unshifted physical key with shift) — and
// WithSimulatedCode gives simulated input the platform's key
// spelling. The scan-code half is gpui/keycode_win32_test.go.

import "testing"

// platformKeystrokeCtrlShiftBracketRight is the keystroke the Windows
// translation reports for Ctrl+Shift+] on a US layout: Key "}" (the
// DOM key, the pin's shifted-key conversion), Shift kept, and the
// physical code BracketRight with its standard location.
var platformKeystrokeCtrlShiftBracketRight = Keystroke{
	Modifiers: ControlShiftModifiers(),
	Key:       "}",
	Code:      "BracketRight",
	Location:  KeyLocationStandard,
}

// bindingKeystroke builds a binding keystroke from the keystroke
// syntax (the author's spelling; no code, the shape NewKeyBinding and
// the dummy mapper keep).
func bindingKeystroke(t *testing.T, source string) KeybindingKeystroke {
	t.Helper()
	keystroke, err := ParseKeystroke(source)
	if err != nil {
		t.Fatal(err)
	}
	return NewKeybindingKeystroke(keystroke)
}

// matches reports whether the typed keystroke matches a binding built
// from the keystroke syntax.
func matches(t *testing.T, typed Keystroke, binding string) bool {
	t.Helper()
	target := bindingKeystroke(t, binding)
	return typed.ShouldMatch(&target)
}

// TestKeystrokeShouldMatchEitherShiftForm: the ticket's acceptance — a
// binding written ctrl-shift-] and one written ctrl-} both match the
// same platform keystroke (Ctrl+Shift+] on a US layout), and the
// near-miss bindings stay non-matching.
func TestKeystrokeShouldMatchEitherShiftForm(t *testing.T) {
	typed := platformKeystrokeCtrlShiftBracketRight

	// The code form: the physical key with shift among the modifiers.
	if !matches(t, typed, "ctrl-shift-]") {
		t.Error("ctrl-shift-} (platform) should match the ctrl-shift-] binding (the code form)")
	}
	// The key form: the shifted character with shift folded away.
	if !matches(t, typed, "ctrl-}") {
		t.Error("ctrl-shift-} (platform) should match the ctrl-} binding (the key form)")
	}
	// Both spellings together (the exact form).
	if !matches(t, typed, "ctrl-shift-}") {
		t.Error("ctrl-shift-} (platform) should match the ctrl-shift-} binding (the exact form)")
	}

	// Near misses: the gesture's modifiers or key must still agree.
	for _, miss := range []string{
		"ctrl-]",       // the unshifted key without shift: a different gesture
		"shift-]",      // no control
		"ctrl",         // a lone modifier
		"ctrl-shift-[", // the neighboring physical key
		"ctrl-shift-a", // a different key entirely
		"ctrl-a",
		"}", // no modifiers at all
	} {
		if matches(t, typed, miss) {
			t.Errorf("ctrl-shift-} (platform) should not match the %q binding", miss)
		}
	}
}

// TestKeystrokeShouldMatchWithoutCodeKeepsExactShape: a keystroke
// without a code (parsed, or from a platform that reports none) keeps
// the pin's exact-match behavior — the shifted-character keystroke
// matches only the binding that spells it exactly.
func TestKeystrokeShouldMatchWithoutCodeKeepsExactShape(t *testing.T) {
	typed := platformKeystrokeCtrlShiftBracketRight
	typed.Code = ""
	if !matches(t, typed, "ctrl-shift-}") {
		t.Error("the code-less ctrl-shift-} keystroke should match the ctrl-shift-} binding")
	}
	for _, miss := range []string{"ctrl-}", "ctrl-shift-]", "ctrl-]"} {
		if matches(t, typed, miss) {
			t.Errorf("the code-less ctrl-shift-} keystroke should not match the %q binding", miss)
		}
	}
}

// TestKeystrokeShouldMatchShiftedLetterRequiresShift: Shift plus a
// letter does not fold (the platform reports Key "a"), so the plain
// binding does not match and the shift binding does.
func TestKeystrokeShouldMatchShiftedLetterRequiresShift(t *testing.T) {
	typed := Keystroke{Modifiers: ShiftModifiers(), Key: "a", Code: "KeyA"}
	if matches(t, typed, "a") {
		t.Error("shift-a (platform, the letter key does not fold) should not match the plain a binding")
	}
	if !matches(t, typed, "shift-a") {
		t.Error("shift-a (platform) should match the shift-a binding")
	}
}

// TestKeystrokeShouldMatchNumpadDigit: a numpad keystroke's key is the
// digit its code produces, so the digit binding matches (the pin's
// shape) while the location reports numpad.
func TestKeystrokeShouldMatchNumpadDigit(t *testing.T) {
	typed := Keystroke{Key: "1", Code: "Numpad1", Location: KeyLocationNumpad}
	if typed.Location != KeyLocationNumpad {
		t.Fatalf("Numpad1 location = %v, want numpad", typed.Location)
	}
	if !matches(t, typed, "1") {
		t.Error("numpad-1 (platform) should match the 1 binding (its key is the digit)")
	}
	if matches(t, typed, "shift-1") {
		t.Error("numpad-1 (platform, no shift) should not match the shift-1 binding")
	}
}

// TestKeymapEitherShiftFormFires: through the keymap (the dispatch
// path's BindingsForInput), the platform keystroke resolves a binding
// written in either form — one keymap per spelling, the ticket's
// "a binding written as ctrl-shift-] and one written as ctrl-} both
// fire for the same keystroke".
func TestKeymapEitherShiftFormFires(t *testing.T) {
	typed := []Keystroke{platformKeystrokeCtrlShiftBracketRight}
	for _, spelling := range []string{"ctrl-shift-]", "ctrl-}"} {
		keymap := NewKeymap([]KeyBinding{
			bindingFor(spelling, testActionAlphaAction.Box(testActionAlpha{}), ""),
		})
		bindings, pending := keymap.BindingsForInput(typed, nil)
		if pending {
			t.Errorf("%q: the match is pending a chord", spelling)
		}
		equalActions(t, bindings, testActionAlphaAction.Box(testActionAlpha{}))
	}
}

// TestKeymapEitherShiftFormChordPrefix: a chord whose first leg is
// either form stays pending, so multi-key bindings written in either
// spelling work too.
func TestKeymapEitherShiftFormChordPrefix(t *testing.T) {
	typed := []Keystroke{platformKeystrokeCtrlShiftBracketRight}
	for _, spelling := range []string{"ctrl-shift-] a", "ctrl-} a"} {
		keymap := NewKeymap([]KeyBinding{
			bindingFor(spelling, testActionAlphaAction.Box(testActionAlpha{}), ""),
		})
		bindings, pending := keymap.BindingsForInput(typed, nil)
		if len(bindings) != 0 || !pending {
			t.Errorf("%q: want a pending chord, got %d bindings pending=%v", spelling, len(bindings), pending)
		}
	}
}

// TestWithSimulatedCode: the simulated keystroke takes the platform's
// spelling — the code from the US key table (or a preset one), the
// location the code implies, and the shifted-key fold for shiftable
// keys with Shift kept. Letters never fold.
func TestWithSimulatedCode(t *testing.T) {
	cases := []struct {
		source string
		want   Keystroke
	}{
		{
			source: "shift-]",
			want:   Keystroke{Modifiers: ShiftModifiers(), Key: "}", Code: "BracketRight", Location: KeyLocationStandard},
		},
		{
			source: "shift-1",
			want:   Keystroke{Modifiers: ShiftModifiers(), Key: "!", Code: "Digit1", Location: KeyLocationStandard},
		},
		{
			source: "ctrl-shift-;",
			want:   Keystroke{Modifiers: ControlShiftModifiers(), Key: ":", Code: "Semicolon", Location: KeyLocationStandard},
		},
		{
			source: "a",
			want:   Keystroke{Key: "a", Code: "KeyA", Location: KeyLocationStandard},
		},
		{
			source: "shift-a",
			want:   Keystroke{Modifiers: ShiftModifiers(), Key: "a", Code: "KeyA", Location: KeyLocationStandard},
		},
		{
			source: "1",
			want:   Keystroke{Key: "1", Code: "Digit1", Location: KeyLocationStandard},
		},
		{
			source: "tab",
			want:   Keystroke{Key: "tab", Code: "Tab", Location: KeyLocationStandard},
		},
		{
			source: "arrowdown",
			want:   Keystroke{Key: "arrowdown", Code: "ArrowDown", Location: KeyLocationStandard},
		},
		{
			source: "]",
			want:   Keystroke{Key: "]", Code: "BracketRight", Location: KeyLocationStandard},
		},
	}
	for _, tc := range cases {
		got, err := ParseKeystroke(tc.source)
		if err != nil {
			t.Fatal(err)
		}
		simulated := got.WithSimulatedCode()
		if simulated != tc.want {
			t.Errorf("WithSimulatedCode(%q) = %+v, want %+v", tc.source, simulated, tc.want)
		}
	}

	// A preset code wins over the key-derived one and carries its own
	// location: the harness spells a numpad key this way.
	got, err := ParseKeystroke("1")
	if err != nil {
		t.Fatal(err)
	}
	got.Code = "Numpad1"
	simulated := got.WithSimulatedCode()
	want := Keystroke{Key: "1", Code: "Numpad1", Location: KeyLocationNumpad}
	if simulated != want {
		t.Errorf("WithSimulatedCode(numpad override) = %+v, want %+v", simulated, want)
	}

	// A key with no physical identity in the tables stays as it is.
	got, err = ParseKeystroke("ctrl-zoom")
	if err != nil {
		t.Fatal(err)
	}
	simulated = got.WithSimulatedCode()
	if simulated != got {
		t.Errorf("WithSimulatedCode(ctrl-zoom) = %+v, want the parsed keystroke unchanged", simulated)
	}
}

// TestKeyLocationString: the location renders its DOM value.
func TestKeyLocationString(t *testing.T) {
	cases := map[KeyLocation]string{
		KeyLocationStandard: "standard",
		KeyLocationLeft:     "left",
		KeyLocationRight:    "right",
		KeyLocationNumpad:   "numpad",
		KeyLocation(9):      "KeyLocation(9)",
	}
	for location, want := range cases {
		if got := location.String(); got != want {
			t.Errorf("KeyLocation(%d).String() = %q, want %q", uint8(location), got, want)
		}
	}
}
