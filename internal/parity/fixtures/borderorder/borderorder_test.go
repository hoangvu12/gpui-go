//go:build windows

// The borderorder fixture's discrimination tests (the tracer's
// tracer_test.go pattern): the deliberately-wrong view must make the
// Tier C pixel comparison FAIL with a readable diff while leaving the
// Tier A box comparison passing, so a failure always means the thing
// it says. The wrong view is the pin's paint order - the border
// painted over the overflowing child (reproduced by a border-only
// overlay painted last) - the order the background-phase border
// replaces.
package borderorder_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/borderorder"
)

// run replays the fixture with the given view.
func run(t *testing.T, view func(*parity.EventLog) gpui.AnyElement) (*parity.Record, *parity.ReplayResult) {
	t.Helper()
	fixture := borderorder.Fixture
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

// TestBorderOverChildFailsOnlyThePixelComparison checks the border
// order's discrimination: the pin's order (the border covering the
// overflowing child, reproduced by the border-only overlay painted
// last) fails Tier C, so a regression of the border to the
// paint-after-children position is caught. The boxes (Tier A) are
// unchanged - the overlay carries no id.
func TestBorderOverChildFailsOnlyThePixelComparison(t *testing.T) {
	record, replay := run(t, borderorder.WrongBorderOverChildView)
	err := parity.ComparePixels(record, replay, borderorder.Fixture.Tolerance)
	if err == nil {
		t.Fatal("the border-over-child view passed the Tier C pixel comparison - the border order is not being discriminated")
	}
	if !strings.Contains(err.Error(), "box: mean channel diff") && !strings.Contains(err.Error(), "poker: mean channel diff") {
		t.Fatalf("the pixel diff is not readable:\n%v", err)
	}
	t.Logf("Tier C fails as it should:\n%v", err)
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("repainting the border over the child must not move any box: %v", err)
	}
}
