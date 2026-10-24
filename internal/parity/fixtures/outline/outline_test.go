//go:build windows

// The outline fixtures' discrimination test (ticket 48 acceptance): a
// deliberately wrong gpui-go view must make the pixel comparison FAIL
// with a readable diff while the comparisons it does not target keep
// passing — so a failure always means the thing it says. The wrong
// view opts the Tab-focused box out of the default ring: the record
// carries Chrome's ring, so the settled frame differs on the box's
// edge band, and only there — the boxes (an outline never affects
// layout) and the transcript (the Tab press and its focus move are
// unchanged) stay passing.
package outline_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/outline"
)

// runWindow replays a fixture with the given window-aware view and
// returns the record and the replay.
func runWindow(t *testing.T, fixture parity.Fixture, view func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement) (*parity.Record, *parity.ReplayResult) {
	t.Helper()
	fixture.ViewWithWindow = view
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

// RinglessView is the deliberately-wrong view for the Tier C
// comparison: the Tab-focused box opts out of the default focus ring
// (OutlineNone), so the settled frame loses the recorded ring — the
// one thing the fixture is about.
func RinglessView() func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return outline.FocusView(true)
}

// TestOutlineARinglessViewFailsOnlyThePixelComparison is ticket 48's
// one deliberately-wrong-view test: the ring-less view must fail the
// Tier C pixel comparison against the ring's record with a readable
// diff on the focused box, while Tier A (the boxes: an outline never
// affects layout) and Tier B (the Tab transcript and its focus move)
// keep passing.
func TestOutlineARinglessViewFailsOnlyThePixelComparison(t *testing.T) {
	record, replay := runWindow(t, outline.FocusRingTabFixture, RinglessView())
	err := parity.ComparePixels(record, replay, outline.FocusRingTabFixture.Tolerance)
	if err == nil {
		t.Fatal("the view without the default focus ring passed the Tier C pixel comparison")
	}
	if !strings.Contains(err.Error(), "box: mean channel diff") && !strings.Contains(err.Error(), "whole image: mean channel diff") {
		t.Fatalf("the pixel diff is not readable:\n%v", err)
	}
	t.Logf("Tier C fails as it should:\n%v", err)
	// Discrimination: the boxes and the transcript are unchanged by
	// the opt-out.
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("an opted-out focus ring must not move any box: %v", err)
	}
	if err := parity.CompareEvents(record, replay); err != nil {
		t.Errorf("an opted-out focus ring must not change the event transcript: %v", err)
	}
}
