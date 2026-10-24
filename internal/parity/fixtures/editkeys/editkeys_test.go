//go:build windows

// The editkeys fixtures' tests: the fixtures match their Chrome records
// (TestFixtures in internal/parity/replay_test.go runs them through the
// same import), and the discrimination test — the deliberately-wrong
// view — proves the record means what it says: a handler without the
// edit-action callbacks loses every editing event of the transcript.
package editkeys_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/editkeys"
)

// TestEditKeysMatchesChromeRecord replays the command fixture against
// its checked-in Chrome record: the three tiers. The Tier B transcript
// is the ticket's acceptance item — Chrome's editing-command event
// order for an input (a disabled copy, the SelectAll command's select
// between the halves of the press, the copy and cut commands' events,
// the cut command's beforeinput mutation preview).
func TestEditKeysMatchesChromeRecord(t *testing.T) {
	record, err := parity.LoadRecord(editkeys.Fixture.Name)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := parity.Replay(t, editkeys.Fixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("%v", err)
	}
	if err := parity.CompareEvents(record, replay); err != nil {
		t.Fatalf("%v", err)
	}
	if err := parity.ComparePixels(record, replay, editkeys.Fixture.Tolerance); err != nil {
		t.Errorf("%v", err)
	}
	// The acceptance details, spelled out beside the passing
	// comparison: the disabled copy (a key pair with no command
	// events), the select between the keydown and keyup of the
	// Ctrl+A press, and the cut's beforeinput carrying deleteByCut.
	var selectEntries, copyEntries, cutEntries, keyPairs int
	sawDeleteByCut := false
	for _, e := range replay.Events {
		switch e.Kind {
		case "select":
			selectEntries++
		case "copy":
			copyEntries++
		case "cut":
			cutEntries++
		case "beforeinput":
			if e.InputType == "deleteByCut" {
				sawDeleteByCut = true
			}
		case "keydown":
			keyPairs++
		}
	}
	if keyPairs != 8 {
		t.Errorf("keydown entries = %d, want 8 (one per scripted key press, logged on the field and the root)", keyPairs)
	}
	if selectEntries != 1 {
		t.Errorf("select entries = %d, want 1 (the SelectAll command's)", selectEntries)
	}
	// The first Ctrl+Insert (collapsed caret) fires copy too — Chrome
	// fires the clipboard events regardless of the selection's state
	// (the record's shape); only the write gates on the selection.
	if copyEntries != 2 {
		t.Errorf("copy entries = %d, want 2 (the disabled one still fires the event)", copyEntries)
	}
	if cutEntries != 1 || !sawDeleteByCut {
		t.Errorf("cut entries = %d (want 1), sawDeleteByCut = %v (the cut command's mutation preview)", cutEntries, sawDeleteByCut)
	}
}

// TestEditKeysPasteMatchesChromeRecord replays the paste fixture: the
// Shift+Insert press fires the paste command's event pair (paste, then
// beforeinput with insertFromPaste) in Chrome's order. The clipboard
// data itself is deliberately absent from the record (see the fixture
// package's comment) — the real insertion is pinned by
// internal/clipspec.
func TestEditKeysPasteMatchesChromeRecord(t *testing.T) {
	record, err := parity.LoadRecord(editkeys.PasteFixture.Name)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := parity.Replay(t, editkeys.PasteFixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("%v", err)
	}
	if err := parity.CompareEvents(record, replay); err != nil {
		t.Fatalf("%v", err)
	}
	if err := parity.ComparePixels(record, replay, editkeys.PasteFixture.Tolerance); err != nil {
		t.Errorf("%v", err)
	}
	sawPaste, sawInsertFromPaste := false, false
	for _, e := range replay.Events {
		switch {
		case e.Kind == "paste":
			sawPaste = true
		case e.Kind == "beforeinput" && e.InputType == "insertFromPaste":
			sawInsertFromPaste = true
		}
	}
	if !sawPaste || !sawInsertFromPaste {
		t.Errorf("paste entry = %v, insertFromPaste entry = %v; both must be present", sawPaste, sawInsertFromPaste)
	}
}

// TestEditKeysPlainHandlerLosesEditEvents is the deliberately-wrong
// view: the handler is the plain Editor (the base InputHandler
// contract — the text mutations still land in its model, but no
// edit-action callbacks), so the replay's transcript loses every
// select/copy/cut/beforeinput entry and Tier B fails against the
// record, naming them. Tier A and Tier C still pass (the boxes and
// pixels do not depend on the events).
func TestEditKeysPlainHandlerLosesEditEvents(t *testing.T) {
	fixture := editkeys.Fixture
	fixture.ViewWithWindow = editkeys.PlainView()
	record, err := parity.LoadRecord(fixture.Name)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := parity.Replay(t, fixture)
	if err != nil {
		t.Fatal(err)
	}
	err = parity.CompareEvents(record, replay)
	if err == nil {
		t.Fatal("the plain-handler view passed Tier B against the record (the editing events must come from the callbacks)")
	}
	if !strings.Contains(err.Error(), "copy:field") || !strings.Contains(err.Error(), "select:field") {
		t.Fatalf("the transcript diff does not name the lost editing entries:\n%v", err)
	}
	t.Logf("Tier B fails as it should:\n%v", err)
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("the handler's callbacks must not change any box: %v", err)
	}
	if err := parity.ComparePixels(record, replay, editkeys.Fixture.Tolerance); err != nil {
		t.Errorf("the handler's callbacks must not change any pixel: %v", err)
	}
}
