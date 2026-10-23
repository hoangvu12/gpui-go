//go:build windows

package rasterprobe

import (
	"fmt"
	"testing"

	"github.com/hoangvu12/gpui-go/internal/parity"
)

// TestChromeSizeEdge inspects Chrome's rasterization across the probed
// sizes: a horizontal scanline through each dark cell's first stem,
// printing the per-channel coverage, plus the per-cell fringe stats.
// This is the diagnostic behind the "which AA at which size" question.
func TestChromeSizeEdge(t *testing.T) {
	for _, name := range []string{"textpaint-raster", "textpaint-raster-gray"} {
		record, img := loadFixtureRecord(t, name)
		t.Logf("== %s", name)
		for _, cell := range rasterCells {
			if cell.Color != "dark" {
				continue
			}
			box := record.Boxes[cell.cellID()]
			rect := parity.ROIOfDevice(box, parity.DeviceScaleFactor, img.Bounds().Dx(), img.Bounds().Dy())
			stats := parity.MeasureInk(img, rect, cell.FgHex, cell.BgHex)
			// Find the ink row with the most ink (the stem band's
			// middle) and print its coverage profile.
			bestRow, bestSum := 0, -1.0
			for y := rect.Min.Y; y < rect.Max.Y; y++ {
				sum := 0.0
				for x := rect.Min.X; x < rect.Max.X; x++ {
					i := y*img.Stride + x*4
					c := (float64(255-img.Pix[i]) + float64(255-img.Pix[i+1]) + float64(255-img.Pix[i+2])) / (3 * 255)
					sum += c
				}
				if sum > bestSum {
					bestSum, bestRow = sum, y
				}
			}
			profile := ""
			for x := rect.Min.X; x < rect.Min.X+40; x++ {
				i := bestRow*img.Stride + x*4
				profile += fmt.Sprintf("%3d/%3d/%3d ", 255-img.Pix[i], 255-img.Pix[i+1], 255-img.Pix[i+2])
			}
			t.Logf("%-9s mean %.4f partial %.4f partial-ink %.4f spread %.4f; row %d ink %.1f:\n%s",
				cell.cellID(), stats.MeanInk, stats.PartialFraction, stats.MeanPartialInk, stats.MeanChannelSpread,
				bestRow-rect.Min.Y, bestSum, profile)
		}
	}
}

func fgOf(color string) uint32 {
	switch color {
	case "dark":
		return 0x000000
	case "light":
		return 0xffffff
	default:
		return 0x808080
	}
}

func bgOf(color string) uint32 {
	switch color {
	case "light":
		return 0x000000
	default:
		return 0xffffff
	}
}
