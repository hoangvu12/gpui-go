//go:build windows

package rasterprobe

import (
	"strconv"
	"testing"

	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/textpaint"
)

// TestCoverageHistograms dumps the coverage histogram of the 16px
// cells, both sides, for the report's coverage-curve figures.
func TestCoverageHistograms(t *testing.T) {
	for _, fixture := range []parity.Fixture{textpaint.RasterFixture, textpaint.RasterGrayFixture} {
		fixture := fixture
		t.Run(fixture.Name, func(t *testing.T) {
			record, chrome, gpuiImg := replayFixture(t, fixture)
			for _, color := range []string{"dark", "light", "grey"} {
				cell := sampleCell{Color: color, Size: 16, FgHex: fgOf(color), BgHex: bgOf(color)}
				box := record.Boxes[cell.cellID()]
				rect := parity.ROIOfDevice(box, parity.DeviceScaleFactor, chrome.Bounds().Dx(), chrome.Bounds().Dy())
				chromeStats := parity.MeasureInk(chrome, rect, cell.FgHex, cell.BgHex)
				gpuiStats := parity.MeasureInk(gpuiImg, rect, cell.FgHex, cell.BgHex)
				t.Logf("chrome %s-16: %s\n%s\ngpui   %s-16: %s\n%s",
					color, chromeStats.String(), formatHistogram(chromeStats.Histogram),
					color, gpuiStats.String(), formatHistogram(gpuiStats.Histogram))
			}
		})
	}
}

// formatHistogram formats one coverage histogram as 16 fractions.
func formatHistogram(h [parity.InkBuckets]float64) string {
	out := "  hist:"
	for _, f := range h {
		out += " " + strconv.FormatFloat(f, 'f', 4, 64)
	}
	return out
}
