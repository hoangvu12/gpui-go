//go:build windows

// The keycode fixtures' tests (the keyorder_test.go pattern): the
// shifted fixture's transcript carries the physical code and location
// Chrome reports, and the two binding fixtures prove the ticket's
// acceptance — a binding written ctrl-shift-] and one written ctrl-}
// both fire for the same Ctrl+Shift+] keystroke.
package keycode_test

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/keycode"
)

// replayFixture loads the fixture's record and replays it.
func replayFixture(t *testing.T, fixture parity.Fixture) (*parity.Record, *parity.ReplayResult) {
	t.Helper()
	record, err := parity.LoadRecord(fixture.Name)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := parity.Replay(t, fixture)
	if err != nil {
		t.Fatal(err)
	}
	return record, replay
}

// TestShiftedFixtureMatchesChrome: the shifted fixture's replay
// matches its record on all three tiers (also asserted by
// TestFixtures), and the transcript both sides produce carries the
// ticket's payload: every key entry has the physical code, the shifted
// keys report the shifted character, and the numpad step reports the
// numpad location.
func TestShiftedFixtureMatchesChrome(t *testing.T) {
	record, replay := replayFixture(t, keycode.ShiftedFixture)
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("Tier A: %v", err)
	}
	if err := parity.CompareEvents(record, replay); err != nil {
		t.Fatalf("Tier B: %v", err)
	}
	if err := parity.ComparePixels(record, replay, keycode.ShiftedFixture.Tolerance); err != nil {
		t.Errorf("Tier C: %v", err)
	}

	// The recorded transcript carries the code (the recorder sent
	// Input.dispatchKeyEvent's code param and Chrome reported
	// e.code): if it were empty, the Tier B comparison above would
	// have failed already, and this pinpoints which field.
	for i, entry := range record.Events {
		if entry.Kind != "keydown" && entry.Kind != "keyup" {
			continue // the leading click entries
		}
		if entry.Code == "" {
			t.Errorf("event %d (%s:%s): the record's key entry has no code", i+1, entry.Kind, entry.Target)
		}
	}
	// The shifted entries: key "}", "!", ":" with Shift kept — the
	// replay's own entries (the record's match by Tier B).
	wantKeys := map[string]string{
		"BracketRight": "}",
		"Digit1":       "!",
		"Semicolon":    ":",
		"KeyA":         "a",
		"Numpad1":      "1",
	}
	seenLocations := map[string]int{}
	for _, entry := range replay.Events {
		if entry.Kind != "keydown" {
			continue // the click and keyup entries
		}
		if want, ok := wantKeys[entry.Code]; !ok || want != entry.Key {
			t.Errorf("keydown %s: key = %q, want %q for that code", entry.Code, entry.Key, want)
		}
		seenLocations[entry.Code] = entry.Location
	}
	if seenLocations["Numpad1"] != int(gpui.KeyLocationNumpad) {
		t.Errorf("the numpad step's keydown location = %d, want %d (numpad)", seenLocations["Numpad1"], gpui.KeyLocationNumpad)
	}
	if seenLocations["BracketRight"] != int(gpui.KeyLocationStandard) {
		t.Errorf("the shifted punctuation keydown location = %d, want 0 (standard)", seenLocations["BracketRight"])
	}
}

// TestBindingFormsBothFire: the ticket's acceptance — the same
// Ctrl+Shift+] keystroke fires a binding written ctrl-shift-] (the
// code form) and one written ctrl-} (the key form). One fixture per
// spelling; each must fire its action exactly once, with the record's
// transcript intact (the listeners ran before the binding under the
// browser order).
func TestBindingFormsBothFire(t *testing.T) {
	for _, fixture := range []parity.Fixture{keycode.BindingCodeFixture, keycode.BindingCharFixture} {
		t.Run(fixture.Name, func(t *testing.T) {
			keycode.FiredActions() // reset
			record, replay := replayFixture(t, fixture)
			if err := parity.CompareEvents(record, replay); err != nil {
				t.Fatalf("the browser-order view must match its record: %v", err)
			}
			if err := parity.ComparePixels(record, replay, fixture.Tolerance); err != nil {
				t.Errorf("a binding firing must not change any pixel: %v", err)
			}
			if fired := keycode.FiredActions(); fired != 1 {
				t.Fatalf("the %q binding fired %d time(s), want exactly 1 for the ctrl-shift-] keystroke", bindingSpelling(fixture), fired)
			}
		})
	}
}

// bindingSpelling reports the binding spelling a fixture's view binds
// (the discrimination label).
func bindingSpelling(fixture parity.Fixture) string {
	switch fixture.Name {
	case keycode.BindingCodeFixture.Name:
		return "ctrl-shift-]"
	case keycode.BindingCharFixture.Name:
		return "ctrl-}"
	}
	return fixture.Name
}

// TestBindingCodeFormNeedsTheEitherFormMatch: replaying the code-form
// record against a view whose binding is the plain ctrl-] spelling (a
// different gesture) must fire nothing and keep the transcript intact
// — the ctrl-shift-] binding firing is the either-form matching, not a
// loose match.
func TestBindingCodeFormNeedsTheEitherFormMatch(t *testing.T) {
	keycode.FiredActions() // reset
	fixture := keycode.BindingCodeFixture
	fixture.ViewWithWindow = keycode.BindingView("ctrl-]")
	record, replay := replayFixture(t, fixture)
	if err := parity.CompareEvents(record, replay); err != nil {
		t.Fatalf("a non-matching binding must not disturb the transcript: %v", err)
	}
	if fired := keycode.FiredActions(); fired != 0 {
		t.Fatalf("the ctrl-] binding fired %d time(s) for the ctrl-shift-] keystroke, want 0 (a different gesture)", fired)
	}
}
