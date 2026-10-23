//go:build windows

// The textpaint fixtures' discrimination tests (the tracer's pattern):
// a deliberately wrong gpui-go view must make the Tier C pixel
// comparison FAIL with a readable diff while Tier A boxes still pass,
// so a failure always means the thing it says. The tolerances are
// calibrated from the measured correct-view statistics (the probe
// report carries the numbers); TestCorrectViewStats prints them.
package textpaint_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/textpaint"
)

// run replays one fixture with the given view and returns the record
// and the replay.
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

// TestCorrectViewStats reports the measured correct-view Tier C
// statistics the tolerances are calibrated from (the numbers behind
// the fixtures' Tolerance fields; see the probe report).
func TestCorrectViewStats(t *testing.T) {
	for _, fixture := range []parity.Fixture{textpaint.RasterFixture, textpaint.RasterGrayFixture, textpaint.ItalicFixture} {
		fixture := fixture
		t.Run(fixture.Name, func(t *testing.T) {
			record, replay := run(t, fixture, fixture.View)
			whole, rois, err := parity.MeasurePixels(record, replay)
			if err != nil {
				t.Fatal(err)
			}
			worstROI, worstMean, worstHot := "", 0.0, 0.0
			for id, stats := range rois {
				if stats.Mean > worstMean {
					worstROI, worstMean = id, stats.Mean
				}
				if stats.HotFraction > worstHot {
					worstHot = stats.HotFraction
				}
			}
			t.Logf("correct view: whole mean %.4f hot %.5f; worst ROI %s mean %.4f; worst ROI hot %.5f",
				whole.Mean, whole.HotFraction, worstROI, worstMean, worstHot)
			if err := parity.ComparePixels(record, replay, fixture.Tolerance); err != nil {
				t.Errorf("the correct view must pass its own tolerance (recalibrate): %v", err)
			}
		})
	}
}

// TestWrongTextSizeFailsOnlyThePixelComparison checks the raster
// fixtures' discrimination: the wrong view (every sample at 1.5x the
// size inside the same fixed cells) changes the ink where the boxes
// stay identical.
func TestWrongTextSizeFailsOnlyThePixelComparison(t *testing.T) {
	for _, fixture := range []parity.Fixture{textpaint.RasterFixture, textpaint.RasterGrayFixture} {
		fixture := fixture
		t.Run(fixture.Name, func(t *testing.T) {
			record, replay := run(t, fixture, textpaint.RasterWrongSize)
			if err := parity.CompareBoxes(record, replay); err != nil {
				t.Errorf("the wrong text size must not move any box: %v", err)
			}
			err := parity.ComparePixels(record, replay, fixture.Tolerance)
			if err == nil {
				t.Fatal("the wrong-size view passed the Tier C pixel comparison")
			}
			if !strings.Contains(err.Error(), "mean channel diff") {
				t.Fatalf("the pixel diff is not readable:\n%v", err)
			}
			t.Logf("Tier C fails as it should:\n%v", err)
		})
	}
}

// TestWrongItalicTextFailsOnlyThePixelComparison checks the italic
// fixture's discrimination: the italic rows drawing dense M stems
// instead of the fixture's l stems inside the same fixed rows.
func TestWrongItalicTextFailsOnlyThePixelComparison(t *testing.T) {
	record, replay := run(t, textpaint.ItalicFixture, textpaint.ItalicWrongText)
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("the wrong italic text must not move any box: %v", err)
	}
	err := parity.ComparePixels(record, replay, textpaint.ItalicFixture.Tolerance)
	if err == nil {
		t.Fatal("the wrong-text italic view passed the Tier C pixel comparison")
	}
	if !strings.Contains(err.Error(), "mean channel diff") && !strings.Contains(err.Error(), "hot pixel fraction") {
		t.Fatalf("the pixel diff is not readable:\n%v", err)
	}
	t.Logf("Tier C fails as it should:\n%v", err)
}
