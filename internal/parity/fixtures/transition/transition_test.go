//go:build windows

// The transition fixtures' discrimination tests (the tracer's
// tracer_test.go pattern): a deliberately-wrong view must make the
// comparison FAIL with a readable diff, so a green fixture means the
// thing it says. The wrong view is a timing bug — the linear box's
// transition running 800ms instead of 400ms — which only the sampled
// mid-flight state can catch: after the transition finishes both views
// agree.
package transition_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/transition"
)

// run replays the transition fixture with the given view and returns
// the record and the replay.
func run(t *testing.T, view func(*parity.EventLog) gpui.AnyElement) (*parity.Record, *parity.ReplayResult) {
	t.Helper()
	fixture := transition.Fixture
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

// TestWrongDurationFailsTheBoxComparison checks the fixture's
// discrimination at the sampled instant: the 800ms-linear wrong view
// holds the linear box at 125px where the record (and the correct
// view) holds 150px, so the exact Tier A box comparison fails with a
// readable diff — a wrong duration, easing or reversing factor all
// land here the same way. Tier C fails on the same box (a quarter of
// its green is missing); Tier B is untouched.
func TestWrongDurationFailsTheBoxComparison(t *testing.T) {
	record, replay := run(t, transition.WrongDurationView)
	err := parity.CompareBoxes(record, replay)
	if err == nil {
		t.Fatal("the 800ms-linear view (the linear box a quarter of the way at the sample) passed the Tier A box comparison")
	}
	if !strings.Contains(err.Error(), "width-linear") || !strings.Contains(err.Error(), "150") {
		t.Fatalf("the box diff is not readable:\n%v", err)
	}
	t.Logf("Tier A fails as it should:\n%v", err)
	if err := parity.CompareEvents(record, replay); err != nil {
		t.Errorf("the wrong duration must not change the transcript: %v", err)
	}
}
