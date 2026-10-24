//go:build windows

// The filters fixtures' discrimination tests (the tracer and boxpaint
// pattern): each deliberately-wrong view must make the Tier C pixel
// comparison FAIL with a readable diff while leaving the Tier A box
// comparison passing, so a failure always means the thing it says. The
// wrong views are the CSS rules this ticket replaced (no filter, the
// chain composed in the wrong order, the default centered position, the
// smooth sampler).
package filters_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/filters"
)

// run replays one filters fixture with the given view and returns the
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

// failPixelComparison asserts the wrong view fails Tier C with a
// readable diff naming the given box, and that Tier A still passes.
func failPixelComparison(t *testing.T, fixture parity.Fixture, view func(*parity.EventLog) gpui.AnyElement, roi string) {
	t.Helper()
	record, replay := run(t, fixture, view)
	err := parity.ComparePixels(record, replay, fixture.Tolerance)
	if err == nil {
		t.Fatalf("the wrong view passed the Tier C pixel comparison (want %s to differ)", roi)
	}
	if !strings.Contains(err.Error(), roi+": mean channel diff") {
		t.Fatalf("the pixel diff is not readable:\n%v", err)
	}
	t.Logf("Tier C fails as it should:\n%v", err)
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("the wrong view must not move any box: %v", err)
	}
}

// TestColorsUnfilteredFailsOnlyThePixelComparison checks the colour
// fixture's discrimination: with no filter applied, every filtered
// swatch differs (grayscale, sepia, invert and the rest), while the
// swatches' boxes are unchanged. Active since the rev-15.1 native
// rebuild (the extension-cbuffer upload fix); under rev 15 the matrix
// was a GPU no-op and the correct and wrong views rendered
// identically, so the test skipped behind SkipPixels.
func TestColorsUnfilteredFailsOnlyThePixelComparison(t *testing.T) {
	failPixelComparison(t, filters.ColorsFixture, filters.ColorsUnfiltered, "grayscale")
}

// TestChainSwappedFailsOnlyThePixelComparison checks the chain
// fixture's discrimination: composing the two functions in the wrong
// order fails Tier C, so a regression of the CSS chain order (first
// function applied first) is caught. Active since the rev-15.1 native
// rebuild (the per-function matrix payload — the clamp between the
// functions is what makes the two orders differ on the record's
// stripes; a composed single matrix would still discriminate, but the
// correct view would not match Chrome).
func TestChainSwappedFailsOnlyThePixelComparison(t *testing.T) {
	failPixelComparison(t, filters.ChainFixture, filters.ChainSwapped, "sepia-hue")
}

// TestImageUnfilteredFailsOnlyThePixelComparison checks the image
// fixture's discrimination: the img elements without their filters
// differ from the filtered record. Active since the rev-15.1 native
// rebuild.
func TestImageUnfilteredFailsOnlyThePixelComparison(t *testing.T) {
	failPixelComparison(t, filters.ImageFixture, filters.ImageUnfiltered, "img-grayscale")
}

// TestObjectPositionCenteredFailsOnlyThePixelComparison checks the
// object-position fixture's discrimination: centering every image (the
// CSS default) moves all the positioned images, while the frames' boxes
// are unchanged.
func TestObjectPositionCenteredFailsOnlyThePixelComparison(t *testing.T) {
	failPixelComparison(t, filters.ObjectPositionFixture, filters.ObjectPositionCentered, "none-left-top")
}

// TestPixelatedSmoothFailsOnlyThePixelComparison checks the pixelated
// fixture's discrimination: drawing both upscaled images smooth (no
// point sampling) fails the pixelated cell's ROI, so a regression of
// image-rendering: pixelated is caught.
func TestPixelatedSmoothFailsOnlyThePixelComparison(t *testing.T) {
	failPixelComparison(t, filters.PixelatedFixture, filters.PixelatedSmooth, "pixelated")
}

// The correct views' end-to-end replay is TestFixtures in
// internal/parity/replay_test.go (every registered fixture, all three
// tiers), and their tolerance headroom is
// TestPixelTolerancesKeepHeadroom in internal/parity/measure_test.go.
