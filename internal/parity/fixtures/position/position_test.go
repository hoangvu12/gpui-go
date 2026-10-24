//go:build windows

// The position: static fixture's discrimination tests (the tracer's
// tracer_test.go pattern): the deliberately-wrong view — every static
// parent left at the port's default relative, so the absolute children
// resolve against their direct parent, exactly the pre-ticket-33 pin —
// must FAIL the Tier A box comparison with a readable diff naming the
// moved boxes, while every in-flow box and the untouched control case
// still match. That is what makes the record a real gate: a regression
// back to the pin's direct-parent resolution cannot pass it.
package position_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/position"
)

// run replays the fixture with the given view and returns the record
// and the replay.
func run(t *testing.T, fixture parity.Fixture, view func(*parity.EventLog) gpui.AnyElement) (*parity.Record, *parity.ReplayResult) {
	t.Helper()
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

// TestWrongDirectParentFailsTheBoxComparison checks the fixture's
// discrimination: the pin's direct-parent resolution (the wrong view)
// fails Tier A on the five moved absolute boxes, with a readable diff.
func TestWrongDirectParentFailsTheBoxComparison(t *testing.T) {
	record, replay := run(t, position.Fixture, position.WrongDirectParent)
	err := parity.CompareBoxes(record, replay)
	if err == nil {
		t.Fatal("the direct-parent view (the pin's rule, before ticket 33) passed the Tier A box comparison")
	}
	// The five absolute children that moved: the three under the static
	// parent (resolved against it instead of the ancestor) and the two
	// under the static chain (resolved against the inner static div
	// instead of the window root).
	for _, id := range []string{"abs-tl", "abs-br", "abs-pct", "root-abs-br", "root-abs-bl"} {
		if !strings.Contains(err.Error(), id+":") {
			t.Fatalf("the box diff does not report %q:\n%v", id, err)
		}
	}
	t.Logf("Tier A fails as it should:\n%v", err)
	// Every in-flow box and the untouched control case still match.
	for _, id := range []string{"root", "ancestor", "static-parent", "control", "abs-ctl", "chain-s1", "chain-s2"} {
		if strings.Contains(err.Error(), id+":") {
			t.Fatalf("the box diff wrongly reports the unchanged %q:\n%v", id, err)
		}
	}
}

// TestWrongDirectParentFailsThePixelComparison checks that the wrong
// view also fails Tier C (the moved boxes paint in the wrong places).
func TestWrongDirectParentFailsThePixelComparison(t *testing.T) {
	record, replay := run(t, position.Fixture, position.WrongDirectParent)
	err := parity.ComparePixels(record, replay, position.Fixture.Tolerance)
	if err == nil {
		t.Fatal("the direct-parent view (the pin's rule, before ticket 33) passed the Tier C pixel comparison")
	}
	t.Logf("Tier C fails as it should:\n%v", err)
}

// TestWrongDirectParentStillPassesEvents checks that the wrong view's
// transcript still matches (the fixture binds no events; a layout-only
// regression must not manufacture events).
func TestWrongDirectParentStillPassesEvents(t *testing.T) {
	record, replay := run(t, position.Fixture, position.WrongDirectParent)
	if err := parity.CompareEvents(record, replay); err != nil {
		t.Errorf("the direct-parent view must not change the transcript: %v", err)
	}
}

// The correct view's end-to-end replay is TestFixtures in
// internal/parity/replay_test.go (all three tiers), and its tolerance
// headroom is TestPixelTolerancesKeepHeadroom in
// internal/parity/measure_test.go.
