//go:build windows

// The tracer's discrimination tests (ticket 01 acceptance 4): a
// deliberately wrong gpui-go view must make each of the three
// comparisons FAIL with a readable diff, and each wrong view must
// leave the comparisons it does not target passing — so a failure
// always means the thing it says.
package tracer_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/tracer"
)

// run replays the tracer fixture with the given view and returns the
// record and the replay.
func run(t *testing.T, view func(*parity.EventLog) gpui.AnyElement) (*parity.Record, *parity.ReplayResult) {
	t.Helper()
	fixture := tracer.Fixture
	fixture.View = view
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

func TestAWrongBoxFailsTheBoxComparison(t *testing.T) {
	record, replay := run(t, tracer.WrongBoxSize)
	err := parity.CompareBoxes(record, replay)
	if err == nil {
		t.Fatal("the wrong-sized box (220x90) passed the Tier A box comparison")
	}
	if !strings.Contains(err.Error(), "box: recorded") || !strings.Contains(err.Error(), "replayed") {
		t.Fatalf("the box diff is not readable:\n%v", err)
	}
	t.Logf("Tier A fails as it should:\n%v", err)
	// The wrong size also changes the pixels (a bigger box paints
	// more); only the click transcript is unaffected.
	if err := parity.CompareEvents(record, replay); err != nil {
		t.Errorf("the wrong size must not change the event transcript: %v", err)
	}
}

func TestAMissingClickHandlerFailsOnlyTheEventTranscript(t *testing.T) {
	record, replay := run(t, tracer.WithoutClick)
	err := parity.CompareEvents(record, replay)
	if err == nil {
		t.Fatal("the view without a click handler passed the Tier B transcript comparison")
	}
	// The readable diff names the missing click events.
	if !strings.Contains(err.Error(), `event 3: recorded click:box`) {
		t.Fatalf("the transcript diff is not readable:\n%v", err)
	}
	t.Logf("Tier B fails as it should:\n%v", err)
	// Discrimination: the view renders identically, so the other two
	// comparisons pass.
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("a missing click handler must not move any box: %v", err)
	}
	if err := parity.ComparePixels(record, replay, tracer.Fixture.Tolerance); err != nil {
		t.Errorf("a missing click handler must not change any pixel: %v", err)
	}
}

func TestWrongPaddingFailsOnlyThePixelComparison(t *testing.T) {
	record, replay := run(t, tracer.WrongPadding)
	err := parity.ComparePixels(record, replay, tracer.Fixture.Tolerance)
	if err == nil {
		t.Fatal("the 24px-padding view (the text moved) passed the Tier C pixel comparison")
	}
	// The readable diff reports the measured statistics against the
	// tolerance, in the box's ROI.
	if !strings.Contains(err.Error(), "box: mean channel diff") {
		t.Fatalf("the pixel diff is not readable:\n%v", err)
	}
	t.Logf("Tier C fails as it should:\n%v", err)
	// Discrimination: the box is the same size at the same place and
	// the handlers are the same, so the other two comparisons pass.
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("a padding change inside a fixed-size box must not move the box: %v", err)
	}
	if err := parity.CompareEvents(record, replay); err != nil {
		t.Errorf("a padding change must not change the event transcript: %v", err)
	}
}

// The replay comparison of the correct view is TestFixtures in
// internal/parity/replay_test.go (every registered fixture, all three
// tiers).
