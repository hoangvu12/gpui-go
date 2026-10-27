//go:build windows

// The maskimage fixture's discrimination tests (the tracer's pattern):
// the deliberately-wrong view must make the Tier C pixel comparison
// FAIL with a readable diff while leaving the Tier A box comparison
// passing, so a failure always means the thing it says. The wrong view
// drops the mask — the fully opaque box.
package maskimage_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/maskimage"
)

// run replays the fixture with the given view.
func run(t *testing.T, view func(*parity.EventLog) gpui.AnyElement) (*parity.Record, *parity.ReplayResult) {
	t.Helper()
	fixture := maskimage.Fixture
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

// TestUnmaskedViewFailsOnlyThePixelComparison checks the
// discrimination: the mask dropped (the fully opaque box) fails Tier C
// — the record's lower half fades to white, the wrong view's stays
// blue — while the boxes (Tier A) are unchanged.
func TestUnmaskedViewFailsOnlyThePixelComparison(t *testing.T) {
	record, replay := run(t, maskimage.WrongUnmaskedView)
	err := parity.ComparePixels(record, replay, maskimage.Fixture.Tolerance)
	if err == nil {
		t.Fatal("the unmasked view passed the Tier C pixel comparison - the gradient mask is not being discriminated")
	}
	if !strings.Contains(err.Error(), "masked: mean channel diff") {
		t.Fatalf("the pixel diff is not readable:\n%v", err)
	}
	t.Logf("Tier C fails as it should:\n%v", err)
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("dropping the mask must not move any box: %v", err)
	}
}
