//go:build windows

// The shadow fixtures' discrimination tests (the tracer's
// tracer_test.go pattern): each deliberately-wrong view must make the
// Tier C pixel comparison FAIL with a readable diff while leaving the
// Tier A box comparison passing, so a failure always means the thing
// it says. The wrong views are the pin's rules the CSS rules replace
// (σ = blur for shadows, un-grown spread radii, list-order shadows,
// the shadow outside the filter group, σ = radius/2 for filters), plus
// the backdrop fixture's native-blocked status.
package shadow_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/shadow"
)

// run replays one shadow fixture with the given view and returns the
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

// TestBlurSigmaIsBlurFailsOnlyThePixelComparison checks the blur
// fixture's discrimination: the σ = blur view (the pin's scale, twice
// as blurry as CSS) fails Tier C in the wrapper's ROI, while the boxes
// (Tier A) are unchanged.
func TestBlurSigmaIsBlurFailsOnlyThePixelComparison(t *testing.T) {
	record, replay := run(t, shadow.BlurFixture, shadow.BlurSigmaIsBlur)
	err := parity.ComparePixels(record, replay, shadow.BlurFixture.Tolerance)
	if err == nil {
		t.Fatal("the σ = blur view (the pin's shadow blur scale) passed the Tier C pixel comparison")
	}
	if !strings.Contains(err.Error(), "blur-card: mean channel diff") {
		t.Fatalf("the pixel diff is not readable:\n%v", err)
	}
	t.Logf("Tier C fails as it should:\n%v", err)
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("doubling the shadow blur must not move any box: %v", err)
	}
}

// TestSpreadPinRadiiFailsOnlyThePixelComparison checks the spread
// fixture's discrimination: the pin's un-grown radii (painted through
// Window.PaintDropShadows) fail Tier C in both wrappers' ROIs, so a
// regression from the CSS corner-shaping rule to the pin is caught.
func TestSpreadPinRadiiFailsOnlyThePixelComparison(t *testing.T) {
	record, replay := run(t, shadow.SpreadFixture, shadow.SpreadPinRadii)
	err := parity.ComparePixels(record, replay, shadow.SpreadFixture.Tolerance)
	if err == nil {
		t.Fatal("the pin-radii view (the spread does not grow the corner radii) passed the Tier C pixel comparison")
	}
	if !strings.Contains(err.Error(), "spread-wide: mean channel diff") {
		t.Fatalf("the pixel diff is not readable:\n%v", err)
	}
	t.Logf("Tier C fails as it should:\n%v", err)
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("the pin's shadow radii must not move any box: %v", err)
	}
}

// TestOrderReversedFailsOnlyThePixelComparison checks the order
// fixture's discrimination: the reversed shadow list (the pin's list
// order, the LAST shadow on top) fails Tier C in the wrapper's ROI.
func TestOrderReversedFailsOnlyThePixelComparison(t *testing.T) {
	record, replay := run(t, shadow.OrderFixture, shadow.OrderReversed)
	err := parity.ComparePixels(record, replay, shadow.OrderFixture.Tolerance)
	if err == nil {
		t.Fatal("the reversed-list view (the pin's shadow order) passed the Tier C pixel comparison")
	}
	if !strings.Contains(err.Error(), "order-card: mean channel diff") {
		t.Fatalf("the pixel diff is not readable:\n%v", err)
	}
	t.Logf("Tier C fails as it should:\n%v", err)
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("reversing the shadow list must not move any box: %v", err)
	}
}

// TestFilterShadowOutsideGroupFailsOnlyThePixelComparison checks the
// filter fixture's discrimination: the shadow outside the filter group
// (the pin's structure) stays sharp where Chrome's record blurs it.
func TestFilterShadowOutsideGroupFailsOnlyThePixelComparison(t *testing.T) {
	record, replay := run(t, shadow.FilterFixture, shadow.FilterShadowOutsideGroup)
	err := parity.ComparePixels(record, replay, shadow.FilterFixture.Tolerance)
	if err == nil {
		t.Fatal("the shadow-outside-group view (the pin's filter grouping) passed the Tier C pixel comparison")
	}
	if !strings.Contains(err.Error(), "filter-box: mean channel diff") {
		t.Fatalf("the pixel diff is not readable:\n%v", err)
	}
	t.Logf("Tier C fails as it should:\n%v", err)
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("moving the shadow outside the group must not move any box: %v", err)
	}
}

// TestFilterSigmaIsHalfRadiusFailsOnlyThePixelComparison checks the
// filter fixture's second discrimination: the σ = radius/2 view (the
// pin's filter blur scale) blurs half as much as Chrome's record.
func TestFilterSigmaIsHalfRadiusFailsOnlyThePixelComparison(t *testing.T) {
	record, replay := run(t, shadow.FilterFixture, shadow.FilterSigmaIsHalfRadius)
	err := parity.ComparePixels(record, replay, shadow.FilterFixture.Tolerance)
	if err == nil {
		t.Fatal("the σ = radius/2 view (the pin's filter blur scale) passed the Tier C pixel comparison")
	}
	if !strings.Contains(err.Error(), "filter-box: mean channel diff") {
		t.Fatalf("the pixel diff is not readable:\n%v", err)
	}
	t.Logf("Tier C fails as it should:\n%v", err)
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("halving the filter sigma must not move any box: %v", err)
	}
}

// TestBackdropSigmaIsHalfRadiusFailsOnlyThePixelComparison checks the
// backdrop fixture's discrimination (active since the rev-15.1 native
// rebuild, browser-core ticket 77): the half-sigma view (the pin's
// texel conversion without the half-resolution factor) fails Tier C
// in the panel's ROI while the boxes (Tier A) are unchanged, and the
// correct view matches Chrome within the tolerance first (0.3562 mean
// channel diff, no hot pixels — the uniform sub-threshold sigma-shape
// residual the fixture's comment records). Under rev 15 the
// backdrop-crop cbuffer upload was the ticket-41 reference bug (the
// mirror fold sampled a constant point) and even the correct view
// failed; the fixture was unregistered until this rebuild.
func TestBackdropSigmaIsHalfRadiusFailsOnlyThePixelComparison(t *testing.T) {
	fixture := shadow.BackdropFixture
	record, replay := run(t, fixture, shadow.BackdropView)
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Fatalf("the backdrop boxes must still match: %v", err)
	}
	if err := parity.ComparePixels(record, replay, fixture.Tolerance); err != nil {
		t.Fatalf("the correct backdrop view must match Chrome within the tolerance: %v", err)
	}

	record, replay = run(t, fixture, shadow.BackdropSigmaIsHalfRadius)
	err := parity.ComparePixels(record, replay, fixture.Tolerance)
	if err == nil {
		t.Fatal("the σ = half-radius view (the pin's backdrop blur scale) passed the Tier C pixel comparison")
	}
	if !strings.Contains(err.Error(), "backdrop-panel: mean channel diff") {
		t.Fatalf("the pixel diff is not readable:\n%v", err)
	}
	t.Logf("Tier C fails as it should:\n%v", err)
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("halving the backdrop sigma must not move any box: %v", err)
	}
}

// The correct views' end-to-end replay is TestFixtures in
// internal/parity/replay_test.go (every registered fixture, all three
// tiers), and their tolerance headroom is
// TestPixelTolerancesKeepHeadroom in internal/parity/measure_test.go.
