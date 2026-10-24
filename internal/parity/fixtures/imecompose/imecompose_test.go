//go:build windows

// The imecompose fixture's tests: the fixture matches its Chrome
// record (TestFixtures in internal/parity/replay_test.go runs it
// through the same import), and the discrimination test — the
// deliberately-wrong view — proves the record means what it says: a
// handler without the composition callbacks loses every composition
// entry of the transcript.
package imecompose_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/imecompose"
)

// viewFn is the fixture view signature (parity.Fixture.ViewWithWindow).
type viewFn = func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement

// TestIMEComposeMatchesChromeRecord replays the fixture against its
// checked-in Chrome record: the three tiers. The Tier B transcript is
// the ticket's acceptance item — the scripted composition's event
// order (compositionstart/update/end around the mutations, the
// mid-composition keys flagged isComposing=true, the commit's end).
func TestIMEComposeMatchesChromeRecord(t *testing.T) {
	record, err := parity.LoadRecord(imecompose.Fixture.Name)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := parity.Replay(t, imecompose.Fixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("%v", err)
	}
	if err := parity.CompareEvents(record, replay); err != nil {
		t.Fatalf("%v", err)
	}
	if err := parity.ComparePixels(record, replay, imecompose.Fixture.Tolerance); err != nil {
		t.Errorf("%v", err)
	}
	// The acceptance details, spelled out beside the passing
	// comparison: the composition entries and their data, and the key
	// entries' isComposing flips.
	var compositionEntries, composingKeys, plainKeys int
	for _, e := range replay.Events {
		switch e.Kind {
		case "compositionstart", "compositionupdate", "compositionend":
			compositionEntries++
		case "keydown", "keyup":
			if e.IsComposing {
				composingKeys++
			} else {
				plainKeys++
			}
		}
	}
	if compositionEntries != 9 {
		t.Errorf("composition entries = %d, want 9 (start, 2 updates, the commit's update and end, the second session's start and update, and the cancel's update and end)", compositionEntries)
	}
	if composingKeys != 4 || plainKeys != 4 {
		t.Errorf("key entries: isComposing=true %d (want 4 — the Enter press's four listener entries mid-composition), false %d (want 4 — the a press's)", composingKeys, plainKeys)
	}
}

// TestIMEComposePlainHandlerLosesCompositionEntries is the
// deliberately-wrong view: the handler is the plain Editor (the base
// InputHandler contract — the text mutations still land in its model,
// but no composition callbacks), so the replay's transcript loses
// every composition entry and Tier B fails against the record, naming
// them. Tier A and Tier C still pass (the boxes and pixels do not
// depend on the events).
func TestIMEComposePlainHandlerLosesCompositionEntries(t *testing.T) {
	fixture := imecompose.Fixture
	fixture.ViewWithWindow = imecompose.PlainView()
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
		t.Fatal("the plain-handler view passed Tier B against the record (the composition entries must come from the callbacks)")
	}
	if !strings.Contains(err.Error(), "compositionstart:field") {
		t.Fatalf("the transcript diff does not name the lost composition entries:\n%v", err)
	}
	t.Logf("Tier B fails as it should:\n%v", err)
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("the handler's callbacks must not change any box: %v", err)
	}
	if err := parity.ComparePixels(record, replay, imecompose.Fixture.Tolerance); err != nil {
		t.Errorf("the handler's callbacks must not change any pixel: %v", err)
	}
}
