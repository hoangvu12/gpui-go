//go:build windows

// The textstyle fixtures' discrimination tests (the tracer's pattern,
// fixtures/tracer/tracer_test.go): every deliberately-wrong view must
// FAIL the comparison its difference lives in (Tier C, the pixels) with
// a readable diff, and the comparisons it does not target must keep
// passing — so a green fixture always means the thing it says. The
// wrong views each reproduce the port's PRE-FIX behaviour of their
// ticket item.
package textstyle_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/textstyle"
)

// run replays one fixture with the given view and returns the record
// and the replay.
func run(t *testing.T, f parity.Fixture, view func(*parity.EventLog) gpui.AnyElement) (*parity.Record, *parity.ReplayResult) {
	t.Helper()
	fixture := f
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

// assertFailsPixels runs the wrong view and requires Tier C to fail
// (the wrong views change pixels, not boxes or events: the rows are
// fixed-size and none of them has handlers).
func assertFailsPixels(t *testing.T, f parity.Fixture, view func(*parity.EventLog) gpui.AnyElement) {
	t.Helper()
	record, replay := run(t, f, view)
	err := parity.ComparePixels(record, replay, f.Tolerance)
	if err == nil {
		t.Fatalf("the wrong view passed the Tier C pixel comparison: a real defect is hidden by the tolerance")
	}
	if !strings.Contains(err.Error(), "mean channel diff") && !strings.Contains(err.Error(), "hot pixel fraction") {
		t.Fatalf("the pixel diff is not readable:\n%v", err)
	}
	t.Logf("Tier C fails as it should:\n%v", err)
	// The rows are fixed-size and uninstrumented: the other tiers keep
	// passing, so the failure always means the pixels.
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("the wrong view must not move any box: %v", err)
	}
	if err := parity.CompareEvents(record, replay); err != nil {
		t.Errorf("the wrong view must not change the event transcript: %v", err)
	}
}

// The transform fixture: without the transform, the raw source text
// renders (the port's pre-fix behaviour).
func TestWrongTransformFailsOnlyThePixelComparison(t *testing.T) {
	assertFailsPixels(t, textstyle.TransformFixture, textstyle.WrongTransformView)
}

// The tracking fixture: the spaced row with liga forced back on is the
// pre-fix behaviour (letter spacing alone never disabled ligatures).
func TestWrongTrackingFailsOnlyThePixelComparison(t *testing.T) {
	assertFailsPixels(t, textstyle.TrackingFixture, textstyle.WrongTrackingView)
}

// The underline fixture: the pin's fixed 1px thickness instead of
// Chrome's auto geometry.
func TestWrongUnderlineFailsOnlyThePixelComparison(t *testing.T) {
	assertFailsPixels(t, textstyle.UnderlineFixture, textstyle.WrongUnderlineView)
}

// The line-clamp fixture: no clamp, so the third line shows and no
// ellipsis appears.
func TestWrongLineClampFailsOnlyThePixelComparison(t *testing.T) {
	assertFailsPixels(t, textstyle.LineClampFixture, textstyle.WrongLineClampView)
}

// The tabular-nums fixture: the tnum row without the feature renders
// Segoe UI's default proportional figures (the class was rejected
// before the fix).
func TestWrongNumericFailsOnlyThePixelComparison(t *testing.T) {
	assertFailsPixels(t, textstyle.NumericFixture, textstyle.WrongNumericView)
}

// The families fixture: every row in the default family — the generic
// keywords resolved to nothing and the classes were rejected before the
// fix.
func TestWrongFamiliesFailsOnlyThePixelComparison(t *testing.T) {
	assertFailsPixels(t, textstyle.FamiliesFixture, textstyle.WrongFamiliesView)
}
