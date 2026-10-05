package gpui

// Keystroke vocabulary tests: the parse grammar, unparse round-trips,
// the IME helpers and the Windows should-match branch
// (platform/keystroke.rs of the pinned CE source; the documented
// behaviors the keymap and dispatch rely on).

import "testing"

func parseOK(t *testing.T, source string) Keystroke {
	t.Helper()
	keystroke, err := ParseKeystroke(source)
	if err != nil {
		t.Fatalf("ParseKeystroke(%q): %v", source, err)
	}
	return keystroke
}

func TestParseKeystrokeModifiers(t *testing.T) {
	cases := []struct {
		source    string
		modifiers Modifiers
		key       string
	}{
		{"ctrl-a", Modifiers{Control: true}, "a"},
		{"alt-a", Modifiers{Alt: true}, "a"},
		{"shift-a", Modifiers{Shift: true}, "a"},
		{"fn-a", Modifiers{Function: true}, "a"},
		{"cmd-a", Modifiers{Platform: true}, "a"},
		{"super-a", Modifiers{Platform: true}, "a"},
		{"win-a", Modifiers{Platform: true}, "a"},
		// secondary means control on non-macOS platforms.
		{"secondary-a", Modifiers{Control: true}, "a"},
		{"ctrl-alt-delete", Modifiers{Control: true, Alt: true}, "delete"},
	}
	for _, tc := range cases {
		got := parseOK(t, tc.source)
		if got.Modifiers != tc.modifiers || got.Key != tc.key {
			t.Errorf("parse(%q) = %+v, want modifiers=%+v key=%q", tc.source, got, tc.modifiers, tc.key)
		}
	}
}

func TestParseKeystrokeUppercaseAndCaseInsensitive(t *testing.T) {
	// A single ASCII uppercase key converts to shift + the lowercase.
	got := parseOK(t, "A")
	if !got.Modifiers.Shift || got.Key != "a" {
		t.Errorf("parse(A) = %+v, want shift+a", got)
	}
	// Named keys are accepted case-insensitively.
	if got := parseOK(t, "TAB"); got.Key != "tab" {
		t.Errorf("parse(TAB).Key = %q, want tab", got.Key)
	}
	if got := parseOK(t, "Enter"); got.Key != "enter" {
		t.Errorf("parse(Enter).Key = %q, want enter", got.Key)
	}
}

func TestParseKeystrokeKeyChar(t *testing.T) {
	// The "->key_char" grammar sets the key char.
	got := parseOK(t, "s->ß")
	if got.Key != "s" || got.KeyChar != "ß" {
		t.Errorf("parse(s->ß) = %+v, want key s key_char ß", got)
	}
	// A trailing component that is neither empty nor ">..." is invalid.
	if _, err := ParseKeystroke("ctrl-a-x"); err == nil {
		t.Error("parse(ctrl-a-x) succeeded, want an invalid-keystroke error")
	}
}

func TestParseKeystrokeDashKey(t *testing.T) {
	// "ctrl--" is ctrl plus the "-" key: the empty component before the
	// trailing dash is consumed as the key.
	got := parseOK(t, "ctrl--")
	if !got.Modifiers.Control || got.Key != "-" {
		t.Errorf("parse(ctrl--) = %+v, want ctrl+-", got)
	}
}

func TestParseKeystrokeModifierAsKey(t *testing.T) {
	// A lone modifier name resolves to that modifier as the key with
	// the modifier flag cleared.
	cases := map[string]string{
		"ctrl":  "control",
		"shift": "shift",
		"alt":   "alt",
		"cmd":   "platform",
		"fn":    "function",
	}
	for source, key := range cases {
		got := parseOK(t, source)
		if got.Key != key || got.Modifiers.Modified() {
			t.Errorf("parse(%q) = %+v, want key %q and no modifiers", source, got, key)
		}
	}
}

func TestKeystrokeUnparseRoundTrip(t *testing.T) {
	// The unparse order is fn, ctrl, alt, win, shift, key; a parse →
	// unparse round-trip is stable.
	cases := []string{"ctrl-a", "ctrl-shift-a", "alt-a", "win-a", "fn-ctrl-alt-shift-a", "a", "-"}
	for _, source := range cases {
		if got := parseOK(t, source).Unparse(); got != source {
			t.Errorf("unparse(parse(%q)) = %q, want %q", source, got, source)
		}
	}
}

func TestKeystrokeIsIMEInProgress(t *testing.T) {
	// A printable key with no key char, no modifiers and no IME
	// completion reports IME in progress.
	cases := []struct {
		keystroke Keystroke
		want      bool
	}{
		{Keystroke{Key: "a"}, true},
		{Keystroke{Key: ""}, true},
		{Keystroke{Key: "a", KeyChar: "a"}, false},
		{Keystroke{Key: "f5"}, false},
		{Keystroke{Key: "a", Modifiers: Modifiers{Control: true}}, false},
		{Keystroke{Key: "a", Modifiers: Modifiers{Alt: true}}, false},
	}
	for _, tc := range cases {
		if got := tc.keystroke.IsIMEInProgress(); got != tc.want {
			t.Errorf("IsIMEInProgress(%+v) = %v, want %v", tc.keystroke, got, tc.want)
		}
	}
}

func TestKeystrokeWithSimulatedIME(t *testing.T) {
	cases := []struct {
		keystroke Keystroke
		wantChar  string
	}{
		{Keystroke{Key: "space"}, " "},
		{Keystroke{Key: "tab"}, "\t"},
		{Keystroke{Key: "enter"}, "\n"},
		{Keystroke{Key: "a"}, "a"},
		{Keystroke{Key: "a", Modifiers: Modifiers{Shift: true}}, "A"},
		{Keystroke{Key: "f5"}, ""},
		{Keystroke{Key: "a", KeyChar: "x"}, "x"},
		{Keystroke{Key: "a", Modifiers: Modifiers{Control: true}}, ""},
	}
	for _, tc := range cases {
		got := tc.keystroke.WithSimulatedIME()
		if got.KeyChar != tc.wantChar {
			t.Errorf("WithSimulatedIME(%+v).KeyChar = %q, want %q", tc.keystroke, got.KeyChar, tc.wantChar)
		}
	}
}

func TestKeystrokeShouldMatchWindows(t *testing.T) {
	// The Windows branch: a typed keystroke with a distinct key_char
	// matches a binding with no modifiers and the key_char as its key.
	typed := Keystroke{Key: "s", KeyChar: "ß"}
	binding := NewKeybindingKeystroke(Keystroke{Key: "ß"})
	if !typed.ShouldMatch(&binding) {
		t.Error("typed s/ß should match the no-modifier ß binding")
	}
	// Without a key char the exact key must match.
	typed = Keystroke{Key: "s", Modifiers: Modifiers{Control: true}}
	binding = NewKeybindingKeystroke(Keystroke{Key: "s", Modifiers: Modifiers{Control: true}})
	if !typed.ShouldMatch(&binding) {
		t.Error("ctrl-s should match the ctrl-s binding")
	}
	binding = NewKeybindingKeystroke(Keystroke{Key: "s"})
	if typed.ShouldMatch(&binding) {
		t.Error("ctrl-s should not match the plain s binding")
	}
}

func TestParseKeystrokeInvalid(t *testing.T) {
	if _, err := ParseKeystroke("ctrl-a-x"); err == nil {
		t.Fatal("expected an error for a trailing component")
	}
	var invalid *InvalidKeystrokeError
	if _, err := ParseKeystroke("ctrl-a-x"); !errorAs(err, &invalid) {
		t.Fatalf("error = %T, want *InvalidKeystrokeError", err)
	}
}

func errorAs(err error, target any) bool {
	if err == nil {
		return false
	}
	switch t := target.(type) {
	case **InvalidKeystrokeError:
		*t, _ = err.(*InvalidKeystrokeError)
		return *t != nil
	}
	return false
}
