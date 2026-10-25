//go:build windows

// The position-default fixture's discrimination tests (the tracer's
// tracer_test.go pattern): the deliberately-wrong view — every
// unpositioned parent pinned at the port's OLD default (relative),
// the exact pre-76 pin — must FAIL the Tier A box comparison with a
// readable diff naming the four moved absolute boxes, while every
// in-flow box still matches. That is what makes the record a real
// gate: a regression back to the relative default (the direct-parent
// resolution) cannot pass it.
package positiondefault_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/positiondefault"
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

// TestWrongRelativeFailsTheBoxComparison checks the fixture's
// discrimination: the pre-76 relative default (the wrong view) fails
// Tier A on the four moved absolute boxes, with a readable diff.
func TestWrongRelativeFailsTheBoxComparison(t *testing.T) {
	record, replay := run(t, positiondefault.Fixture, positiondefault.WrongRelative)
	err := parity.CompareBoxes(record, replay)
	if err == nil {
		t.Fatal("the relative-default view (the pre-76 pin) passed the Tier A box comparison")
	}
	// The four absolute children that moved: the two under the
	// unpositioned parents (resolved against the parents instead of
	// the grandparent) and the two under the unpositioned chain
	// (resolved against the inner div instead of the window root).
	for _, id := range []string{"child-a", "child-b", "root-abs-br", "root-abs-bl"} {
		if !strings.Contains(err.Error(), id+":") {
			t.Fatalf("the box diff does not report %q:\n%v", id, err)
		}
	}
	// Every in-flow box still matches.
	for _, id := range []string{"root", "grandparent", "parent", "parent2", "chain-outer", "chain-inner"} {
		if strings.Contains(err.Error(), id+":") {
			t.Fatalf("the box diff wrongly reports the unchanged %q:\n%v", id, err)
		}
	}
	t.Logf("Tier A fails as it should:\n%v", err)
}

// TestWrongRelativeFailsThePixelComparison checks that the wrong view
// also fails Tier C (the moved boxes paint in the wrong places).
func TestWrongRelativeFailsThePixelComparison(t *testing.T) {
	record, replay := run(t, positiondefault.Fixture, positiondefault.WrongRelative)
	err := parity.ComparePixels(record, replay, positiondefault.Fixture.Tolerance)
	if err == nil {
		t.Fatal("the relative-default view (the pre-76 pin) passed the Tier C pixel comparison")
	}
	t.Logf("Tier C fails as it should:\n%v", err)
}

// TestWrongRelativeStillPassesEvents checks that the wrong view's
// transcript still matches (the fixture binds no events; a layout-only
// regression must not manufacture events).
func TestWrongRelativeStillPassesEvents(t *testing.T) {
	record, replay := run(t, positiondefault.Fixture, positiondefault.WrongRelative)
	if err := parity.CompareEvents(record, replay); err != nil {
		t.Fatalf("the wrong view's transcript diverged:\n%v", err)
	}
}
