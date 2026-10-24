//go:build windows

// The anchor fixtures' discrimination tests (the tracer's
// tracer_test.go pattern): each deliberately-wrong view must make the
// comparison it targets FAIL with a readable diff — the wrong-side and
// no-flip views move boxes, so Tier A fails naming the moved element;
// the still-visible hidden view keeps every box and fails only Tier C,
// where the record's pixels show nothing where the wrong view paints
// the popup. That is what makes a passing fixture mean the thing it
// says.
package anchor_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/anchor"
)

// run replays one anchor fixture with the given view and returns the
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

// runWindowed replays one window-aware anchor fixture with the given
// view.
func runWindowed(t *testing.T, fixture parity.Fixture, view func(*parity.EventLog, *gpui.Window, *gpui.App) gpui.AnyElement) (*parity.Record, *parity.ReplayResult) {
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

// TestSidesWrongSideFailsTheBoxComparison checks the sides fixture's
// discrimination: placing every popup on the bottom side moves the
// top/left/right popups, so Tier A fails naming them — a regression
// that stops honoring Side surfaces as moved boxes.
func TestSidesWrongSideFailsTheBoxComparison(t *testing.T) {
	record, replay := run(t, anchor.SidesFixture, anchor.SidesWrongSide)
	err := parity.CompareBoxes(record, replay)
	if err == nil {
		t.Fatal("the wrong-side view (every popup on the bottom) passed the Tier A box comparison")
	}
	for _, id := range []string{"p-top", "p-left", "p-right"} {
		if !strings.Contains(err.Error(), id+": recorded") {
			t.Fatalf("the box diff is not readable (missing %s):\n%v", id, err)
		}
	}
	t.Logf("Tier A fails as it should:\n%v", err)
	if err := parity.ComparePixels(record, replay, anchor.SidesFixture.Tolerance); err == nil {
		t.Errorf("the wrong-side view passed the Tier C pixel comparison (the popups moved)")
	}
}

// TestFlipNoFallbackFailsTheBoxComparison checks the flip fixture's
// discrimination: a popup without fallbacks keeps its overflowing base
// placement below the window, so Tier A fails naming it — dropping
// TryFallbacks surfaces as the unflipped box.
func TestFlipNoFallbackFailsTheBoxComparison(t *testing.T) {
	record, replay := run(t, anchor.FlipFixture, anchor.FlipNoFallback)
	err := parity.CompareBoxes(record, replay)
	if err == nil {
		t.Fatal("the no-fallback view (the popup left below the window edge) passed the Tier A box comparison")
	}
	if !strings.Contains(err.Error(), "p-flip: recorded") {
		t.Fatalf("the box diff is not readable (missing p-flip):\n%v", err)
	}
	t.Logf("Tier A fails as it should:\n%v", err)
	if err := parity.ComparePixels(record, replay, anchor.FlipFixture.Tolerance); err == nil {
		t.Errorf("the no-fallback view passed the Tier C pixel comparison (the popup paints outside the window)")
	}
}

// TestHiddenAlwaysVisibleFailsOnlyThePixelComparison checks the hidden
// fixture's discrimination: a popup that never hides keeps every box
// (the scroller), so only Tier C fails — the record's pixels show the
// scroller alone where the wrong view paints a 100x50 popup at (30,8).
func TestHiddenAlwaysVisibleFailsOnlyThePixelComparison(t *testing.T) {
	record, replay := runWindowed(t, anchor.HiddenFixture, anchor.HiddenAlwaysVisible)
	err := parity.ComparePixels(record, replay, anchor.HiddenFixture.Tolerance)
	if err == nil {
		t.Fatal("the always-visible view (the clipped anchor's popup still painted) passed the Tier C pixel comparison")
	}
	if !strings.Contains(err.Error(), "whole-image mean channel diff") && !strings.Contains(err.Error(), "mean channel diff") {
		t.Fatalf("the pixel diff is not readable:\n%v", err)
	}
	t.Logf("Tier C fails as it should:\n%v", err)
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("the always-visible view must not move any box: %v", err)
	}
	if err := parity.CompareEvents(record, replay); err != nil {
		t.Errorf("the always-visible view must not change the transcript: %v", err)
	}
}
