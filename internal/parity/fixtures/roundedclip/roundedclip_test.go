//go:build windows

// The roundedclip fixture's discrimination tests (the tracer's
// tracer_test.go pattern): the deliberately-wrong view must make the
// Tier C pixel comparison FAIL with a readable diff while leaving the
// Tier A box comparison passing, so a failure always means the thing
// it says. The wrong view is the rectangular overflow clip - the
// pre-R16-styling rendering the rounded_clip_only group replaces.
package roundedclip_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/roundedclip"
)

// run replays the fixture with the given view.
func run(t *testing.T, view func(*parity.EventLog) gpui.AnyElement) (*parity.Record, *parity.ReplayResult) {
	t.Helper()
	fixture := roundedclip.Fixture
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

// TestRectClipFailsOnlyThePixelComparison checks the rounded-clip
// discrimination: the rectangular-clip view (the image's corners
// escape the rounded arc - the pre-R16 rect-only mask) fails Tier C,
// so a regression of the rounded overflow clip to the rectangular
// mask is caught. The boxes (Tier A) are unchanged.
func TestRectClipFailsOnlyThePixelComparison(t *testing.T) {
	record, replay := run(t, roundedclip.WrongRectClipView)
	err := parity.ComparePixels(record, replay, roundedclip.Fixture.Tolerance)
	if err == nil {
		t.Fatal("the rectangular-clip view passed the Tier C pixel comparison - the rounded clip is not being discriminated")
	}
	if !strings.Contains(err.Error(), "card: mean channel diff") {
		t.Fatalf("the pixel diff is not readable:\n%v", err)
	}
	t.Logf("Tier C fails as it should:\n%v", err)
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("dropping the clip's corner radii must not move any box: %v", err)
	}
}
