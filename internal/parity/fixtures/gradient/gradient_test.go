//go:build windows

// The gradient fixtures' discrimination tests (the tracer's
// tracer_test.go pattern): each deliberately-wrong view must make the
// Tier C pixel comparison FAIL with a readable diff while leaving the
// Tier A box comparison passing, so a failure always means the thing it
// says. The wrong views are the pinned rules the CSS rules replaced
// (the squashed dominant-axis diagonal, the naive corner reading, the
// darkening fade) and each is documented at its site in gradient.go.
package gradient_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/gradient"
)

// run replays one gradient fixture with the given view and returns the
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

// TestAnglesPinnedDiagonalFailsOnlyThePixelComparison checks the angles
// fixture's discrimination: the pin's squashed, dominant-axis-normalized
// diagonal fails Tier C in the diagonal boxes' ROIs (0, 90 and 180 are
// axis-aligned, where the pin matches CSS), while the boxes (Tier A)
// are unchanged.
func TestAnglesPinnedDiagonalFailsOnlyThePixelComparison(t *testing.T) {
	record, replay := run(t, gradient.AnglesFixture, gradient.AnglesPinnedDiagonal)
	err := parity.ComparePixels(record, replay, gradient.AnglesFixture.Tolerance)
	if err == nil {
		t.Fatal("the pinned-diagonal view (the pin's squashed, dominant-axis diagonal) passed the Tier C pixel comparison")
	}
	if !strings.Contains(err.Error(), "grad-45: mean channel diff") {
		t.Fatalf("the pixel diff is not readable:\n%v", err)
	}
	t.Logf("Tier C fails as it should:\n%v", err)
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("the pinned diagonal must not move any box: %v", err)
	}
}

// TestTransparentThroughBlackFailsOnlyThePixelComparison checks the
// transparent fixture's discrimination: the darkening fade (the naive
// non-premultiplied mix's visible signature) fails Tier C in the
// fading boxes' ROIs, while the boxes (Tier A) are unchanged.
func TestTransparentThroughBlackFailsOnlyThePixelComparison(t *testing.T) {
	record, replay := run(t, gradient.TransparentFixture, gradient.TransparentThroughBlack)
	err := parity.ComparePixels(record, replay, gradient.TransparentFixture.Tolerance)
	if err == nil {
		t.Fatal("the through-black view (the naive fade's grey fringe) passed the Tier C pixel comparison")
	}
	if !strings.Contains(err.Error(), "fade-down: mean channel diff") {
		t.Fatalf("the pixel diff is not readable:\n%v", err)
	}
	t.Logf("Tier C fails as it should:\n%v", err)
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("the darkening fade must not move any box: %v", err)
	}
}

// TestClassesCornerAsDiagonalFailsOnlyThePixelComparison checks the
// class fixture's discrimination: reading the corner keyword as the
// square's 45deg (ignoring the magic-corner rule) fails Tier C in the
// corner box's ROI only, while the boxes (Tier A) are unchanged.
func TestClassesCornerAsDiagonalFailsOnlyThePixelComparison(t *testing.T) {
	record, replay := run(t, gradient.ClassesFixture, gradient.ClassesCornerAsDiagonal)
	err := parity.ComparePixels(record, replay, gradient.ClassesFixture.Tolerance)
	if err == nil {
		t.Fatal("the corner-as-45deg view passed the Tier C pixel comparison")
	}
	if !strings.Contains(err.Error(), "class-corner: mean channel diff") {
		t.Fatalf("the pixel diff is not readable:\n%v", err)
	}
	t.Logf("Tier C fails as it should:\n%v", err)
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("the corner reading must not move any box: %v", err)
	}
}

// The correct views' end-to-end replay is TestFixtures in
// internal/parity/replay_test.go (every registered fixture, all three
// tiers), and their tolerance headroom is
// TestPixelTolerancesKeepHeadroom in internal/parity/measure_test.go.
