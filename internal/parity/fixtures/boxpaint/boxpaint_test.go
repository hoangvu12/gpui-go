//go:build windows

// The boxpaint fixtures' discrimination tests (the tracer's
// tracer_test.go pattern): each deliberately-wrong view must make the
// Tier C pixel comparison FAIL with a readable diff while leaving the
// Tier A box comparison passing, so a failure always means the thing
// it says. The wrong views are the pin's rules the CSS rules replace
// (per-corner radius clamp, no clip) and the crude wrong fit (fill).
package boxpaint_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/boxpaint"
)

// run replays one boxpaint fixture with the given view and returns the
// record and the replay.
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

// TestClipUnclippedFailsOnlyThePixelComparison checks the clip
// fixture's discrimination: the unclipped wrong view (the
// transparent-border box showing its overflow) fails Tier C in the
// box's ROI, while the boxes (Tier A) are unchanged.
func TestClipUnclippedFailsOnlyThePixelComparison(t *testing.T) {
	record, replay := run(t, boxpaint.ClipFixture, boxpaint.ClipUnclipped)
	err := parity.ComparePixels(record, replay, boxpaint.ClipFixture.Tolerance)
	if err == nil {
		t.Fatal("the unclipped view (the child paints past the padding box) passed the Tier C pixel comparison")
	}
	if !strings.Contains(err.Error(), "clip: mean channel diff") {
		t.Fatalf("the pixel diff is not readable:\n%v", err)
	}
	t.Logf("Tier C fails as it should:\n%v", err)
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("removing the clip must not move any box: %v", err)
	}
}

// TestRadiiPerCornerClampFailsOnlyThePixelComparison checks the radii
// fixture's discrimination: the pin's per-corner clamp fails Tier C,
// so a regression from the CSS factor rule to the pin is caught. The
// boxes (Tier A) are unchanged.
func TestRadiiPerCornerClampFailsOnlyThePixelComparison(t *testing.T) {
	record, replay := run(t, boxpaint.RadiiFixture, boxpaint.RadiiPerCornerClamp)
	err := parity.ComparePixels(record, replay, boxpaint.RadiiFixture.Tolerance)
	if err == nil {
		t.Fatal("the per-corner-clamp view (the pin's radius rule) passed the Tier C pixel comparison")
	}
	if !strings.Contains(err.Error(), "radii-uneven: mean channel diff") {
		t.Fatalf("the pixel diff is not readable:\n%v", err)
	}
	t.Logf("Tier C fails as it should:\n%v", err)
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("the clamped radii must not move any box: %v", err)
	}
}

// TestObjectFitFillFailsOnlyThePixelComparison checks the object-fit
// fixture's discrimination: the fill-everything wrong view fails Tier
// C in every image's ROI, while the frames (Tier A) are unchanged.
func TestObjectFitFillFailsOnlyThePixelComparison(t *testing.T) {
	record, replay := run(t, boxpaint.ObjectFitFixture, boxpaint.ObjectFitFill)
	err := parity.ComparePixels(record, replay, boxpaint.ObjectFitFixture.Tolerance)
	if err == nil {
		t.Fatal("the fill view (no centring, letterboxing or cropping) passed the Tier C pixel comparison")
	}
	if !strings.Contains(err.Error(), "img-none: mean channel diff") {
		t.Fatalf("the pixel diff is not readable:\n%v", err)
	}
	t.Logf("Tier C fails as it should:\n%v", err)
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("the fill fit must not move any frame: %v", err)
	}
}

// The correct views' end-to-end replay is TestFixtures in
// internal/parity/replay_test.go (every registered fixture, all three
// tiers), and their tolerance headroom is
// TestPixelTolerancesKeepHeadroom in internal/parity/measure_test.go.
