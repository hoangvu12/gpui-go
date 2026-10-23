//go:build windows

package parity_test

import (
	"fmt"
	"testing"

	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/tracer"
)

// TestPixelTolerancesKeepHeadroom pins the provisional Tier C numbers
// to the measured correct-view statistics: every fixture's measured
// diff must stay at or below half its tolerance, so a tolerance always
// has 2x headroom over the real correct-vs-correct noise. When Chrome
// or the native artifact changes text rasterization, this fails first
// and names the drift, instead of the fixture failing mysteriously
// later (the guide's recalibration step).
func TestPixelTolerancesKeepHeadroom(t *testing.T) {
	fixtures := parity.All()
	if len(fixtures) == 0 {
		t.Fatal("no registered fixtures")
	}
	for _, f := range fixtures {
		f := f
		t.Run(f.Name, func(t *testing.T) {
			record, err := parity.LoadRecord(f.Name)
			if err != nil {
				t.Fatal(err)
			}
			replay, err := parity.Replay(t, f)
			if err != nil {
				t.Fatal(err)
			}
			whole, rois, err := parity.MeasurePixels(record, replay)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("measured: whole mean %.4f hot %.5f; %s", whole.Mean, whole.HotFraction, formatROIs(rois))
			if whole.Mean > f.Tolerance.MaxMeanChannelDiff/2 {
				t.Errorf("whole-image mean channel diff %.4f exceeds half the tolerance %.4f: recalibrate (docs/browser-parity-harness.md)",
					whole.Mean, f.Tolerance.MaxMeanChannelDiff)
			}
			if whole.HotFraction > f.Tolerance.MaxHotPixelFraction/2 {
				t.Errorf("whole-image hot pixel fraction %.5f exceeds half the tolerance %.5f: recalibrate",
					whole.HotFraction, f.Tolerance.MaxHotPixelFraction)
			}
			for id, stats := range rois {
				if stats.Mean > f.Tolerance.MaxROIMeanChannelDiff/2 {
					t.Errorf("%s: ROI mean channel diff %.4f exceeds half the tolerance %.4f: recalibrate",
						id, stats.Mean, f.Tolerance.MaxROIMeanChannelDiff)
				}
				if stats.HotFraction > f.Tolerance.MaxHotPixelFraction/2 {
					t.Errorf("%s: ROI hot pixel fraction %.5f exceeds half the tolerance %.5f: recalibrate",
						id, stats.HotFraction, f.Tolerance.MaxHotPixelFraction)
				}
			}
		})
	}
}

func formatROIs(rois map[string]parity.PixelStats) string {
	parts := make([]string, 0, len(rois))
	for id, stats := range rois {
		parts = append(parts, fmt.Sprintf("%s mean %.4f hot %.5f", id, stats.Mean, stats.HotFraction))
	}
	out := ""
	for i, part := range parts {
		if i > 0 {
			out += "; "
		}
		out += part
	}
	return out
}

// The tracer fixture is registered by its package's import above; the
// wrong-view discrimination tests live beside it
// (internal/parity/fixtures/tracer/tracer_test.go).
var _ = tracer.Fixture.Name
