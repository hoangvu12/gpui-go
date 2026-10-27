// The gradientstops fixture's discrimination tests (the tracer's
// pattern): the deliberately-wrong view must make the Tier C pixel
// comparison FAIL with a readable diff while leaving the Tier A box
// comparison passing. The wrong view collapses both gradients to
// the two-stop linear form - the R15 record's shape.

package gradientstops_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/gradientstops"
)

// run replays the fixture with the given view.
func run(t *testing.T, view func(*parity.EventLog) gpui.AnyElement) (*parity.Record, *parity.ReplayResult) {
	t.Helper()
	fixture := gradientstops.Fixture
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

// TestTwoStopViewFailsOnlyThePixelComparison checks the
// discrimination: the mask dropped (the fully opaque box) fails Tier C
// — the record's lower half fades to white, the wrong view's stays
// blue — while the boxes (Tier A) are unchanged.
func TestTwoStopViewFailsOnlyThePixelComparison(t *testing.T) {
	record, replay := run(t, gradientstops.WrongTwoStopView)
	err := parity.ComparePixels(record, replay, gradientstops.Fixture.Tolerance)
	if err == nil {
		t.Fatal("the wrong view passed the Tier C pixel comparison - the fixture's feature is not being discriminated")
	}
	if !strings.Contains(err.Error(), "linear: mean channel diff") {
		t.Fatalf("the pixel diff is not readable:\n%v", err)
	}
	t.Logf("Tier C fails as it should:\n%v", err)
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("the wrong view must not move any box: %v", err)
	}
}
