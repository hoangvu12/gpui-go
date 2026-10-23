//go:build windows

package rasterprobe

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// TestChromeAAMode answers the first question of the probe: what text
// antialiasing does headless Chrome use under the recorder's default
// flags, and does --disable-lcd-text change it? It measures the two
// raster records against each other and reports the channel spread
// (the subpixel fringe detector) of each.
func TestChromeAAMode(t *testing.T) {
	_, defaultImg := loadFixtureRecord(t, "textpaint-raster")
	_, grayImg := loadFixtureRecord(t, "textpaint-raster-gray")

	diff := diffImages(defaultImg, grayImg)
	t.Logf("default vs --disable-lcd-text: mean %.4f, max %d, differing pixels %d/%d",
		diff.Mean, diff.Max, diff.DifferCount, diff.Pixels)

	// The fringe detector over each record's dark cells: a grayscale
	// raster has (max-min) channel coverage 0; ClearType fringes do
	// not. A single dark cell at 16px is representative.
	for _, name := range []string{"textpaint-raster", "textpaint-raster-gray"} {
		record, img := loadFixtureRecord(t, name)
		total := parity.InkStats{}
		count := 0
		for _, cell := range rasterCells {
			if cell.Color != "dark" {
				continue
			}
			box := record.Boxes[cell.cellID()]
			rect := parity.ROIOfDevice(box, parity.DeviceScaleFactor, img.Bounds().Dx(), img.Bounds().Dy())
			stats := parity.MeasureInk(img, rect, cell.FgHex, cell.BgHex)
			total.MeanChannelSpread += stats.MeanChannelSpread
			count++
		}
		t.Logf("%s: dark cells' mean channel spread %.4f (0 = grayscale edges, >0.1 = LCD fringes)",
			name, total.MeanChannelSpread/float64(count))
	}
}

// TestEnvironment records the rasterization environment both sides
// run in: gpui-go's DirectWrite facts (gamma, contrast, the OS
// ClearType state and the recommended mode) and the registry key that
// would make Chrome read DirectWrite's values instead of Skia's
// compile-time defaults.
func TestEnvironment(t *testing.T) {
	ts, err := gpui.DefaultTextSystem()
	if err != nil {
		t.Fatal(err)
	}
	info, err := ts.RasterBackend()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("gpui raster backend: %d (0 = DirectWrite+Swash, 1 = Swash only), variable factory %v, gamma %.4f, grayscale enhanced contrast %.4f, system subpixel %v",
		info.Backend, info.VariableFactory, info.Gamma, info.GrayscaleEnhancedContrast, info.SystemSubpixelRendering)
	mode, err := ts.RecommendedRenderingMode()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("gpui recommended rendering mode: %v (1 = subpixel/ClearType, 2 = grayscale)", mode)
	registryState(t, info.Gamma)
}
