//go:build windows

// The opacitygroup fixture's discrimination tests (the tracer's
// tracer_test.go pattern): the deliberately-wrong view must make the
// Tier C pixel comparison FAIL with a readable diff while leaving the
// Tier A box comparison passing, so a failure always means the thing
// it says. The wrong view is the pre-D9 per-primitive multiply - the
// pin's opacity handling the group replaces.
package opacitygroup_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/opacitygroup"
)

// run replays the fixture with the given view.
func run(t *testing.T, view func(*parity.EventLog) gpui.AnyElement) (*parity.Record, *parity.ReplayResult) {
	t.Helper()
	fixture := opacitygroup.Fixture
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

// TestPerPrimitiveMultiplyFailsOnlyThePixelComparison checks the D9
// discrimination: the pre-D9 view (each child carrying its own 0.5
// opacity, the parent's colour pre-multiplied) fails Tier C - the
// overlap band blends twice (alpha 0.75) where the record's group
// blends once (0.5) - while the boxes (Tier A) are unchanged.
func TestPerPrimitiveMultiplyFailsOnlyThePixelComparison(t *testing.T) {
	record, replay := run(t, opacitygroup.WrongPerPrimitiveView)
	err := parity.ComparePixels(record, replay, opacitygroup.Fixture.Tolerance)
	if err == nil {
		t.Fatal("the per-primitive multiply view passed the Tier C pixel comparison - the group's single 0.5 blend is not being discriminated")
	}
	if !strings.Contains(err.Error(), "parent: mean channel diff") {
		t.Fatalf("the pixel diff is not readable:\n%v", err)
	}
	t.Logf("Tier C fails as it should:\n%v", err)
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("moving the opacity to the children must not move any box: %v", err)
	}
}
