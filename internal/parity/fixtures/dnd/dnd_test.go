//go:build windows

// The dnd fixtures' discrimination test (ticket 16 acceptance): a
// deliberately wrong gpui-go view must make the Tier B transcript
// comparison FAIL with a readable diff — the view that never accepts
// a dragover loses the drop event (and the dragover entries' default
// prevention), so a passing transcript always means the sequence and
// the acceptance are right. One wrong view, one test (the harness
// rule).
package dnd_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/dnd"
)

// runWindow replays a fixture with the given window-aware view and
// returns the record and the replay.
func runWindow(t *testing.T, fixture parity.Fixture, view func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement) (*parity.Record, *parity.ReplayResult) {
	t.Helper()
	fixture.ViewWithWindow = view
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

// TestAViewThatNeverAcceptsTheDragoverFailsTheEventTranscript runs
// the across fixture with the view whose targets never accept (no
// preventDefault on the dragover): the recorded transcript has a
// drop entry the replay loses, and the dragover entries' final
// default-prevented state flips — Tier B fails with a readable diff.
func TestAViewThatNeverAcceptsTheDragoverFailsTheEventTranscript(t *testing.T) {
	record, replay := runWindow(t, dnd.AcrossFixture, dnd.NoDragoverAcceptance())
	err := parity.CompareEvents(record, replay)
	if err == nil {
		t.Fatal("the view that never accepts a dragover passed the Tier B transcript comparison")
	}
	// The readable diff names the missing drop on t3 (and the
	// unprevented dragovers).
	if !strings.Contains(err.Error(), `event 31: recorded drop:t3`) && !strings.Contains(err.Error(), `drop:t3`) {
		t.Fatalf("the transcript diff is not readable:\n%v", err)
	}
	t.Logf("Tier B fails as it should:\n%v", err)
	// Discrimination: the boxes are the same, so Tier A passes (the
	// drop never lands, so the settled frame keeps the plain target
	// colour and Tier C differs with it — the acceptance IS the drop).
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("a missing dragover acceptance must not move any box: %v", err)
	}
	if err := parity.ComparePixels(record, replay, dnd.AcrossFixture.Tolerance); err == nil {
		t.Error("the view whose drop never lands passed the Tier C pixel comparison")
	}
}
