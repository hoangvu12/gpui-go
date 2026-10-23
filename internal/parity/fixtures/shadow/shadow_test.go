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

// TestBackdropFixtureBlockedOnNativeBackdropBlur pins what the
// backdrop fixture CAN check today: the boxes (Tier A) match Chrome
// exactly, and the pixel comparison fails because the rev-15 native
// backdrop blur is broken — the crop path's composite samples the
// window-sized blur texture with crop-relative coordinates
// (renderer/draw.rs dx_blur_and_composite builds
// BlurUniforms::composite(..., blur_size, source_size) with
// source_size = the crop's total, while the blurred result lives in
// the window-sized ping texture), so a partial-region backdrop renders
// its blurred input zoomed, and a full-viewport backdrop (whose crop
// region IS the window) still shows a neighbor-averaged un-blurred
// input. Native code is out of this ticket's scope: this test skips
// with that reason until the native fix lands, then it becomes the
// fixture's gate (re-register the fixture in shadow.go's init to run
// it in TestFixtures).
func TestBackdropFixtureBlockedOnNativeBackdropBlur(t *testing.T) {
	fixture := shadow.BackdropFixture
	record, replay := run(t, fixture, shadow.BackdropView)
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Fatalf("the backdrop boxes must still match: %v", err)
	}
	err := parity.ComparePixels(record, replay, fixture.Tolerance)
	if err == nil {
		t.Log("the backdrop pixel comparison passes: the native backdrop blur is fixed — re-register the fixture in TestFixtures")
		return
	}
	t.Skipf("blocked on the native rev-15 backdrop blur (not this ticket's Go-side σ conversion, which the record-level test pins): %v", err)
}

// The correct views' end-to-end replay is TestFixtures in
// internal/parity/replay_test.go (every registered fixture, all three
// tiers), and their tolerance headroom is
// TestPixelTolerancesKeepHeadroom in internal/parity/measure_test.go.
