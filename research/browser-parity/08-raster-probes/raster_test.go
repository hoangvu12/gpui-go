//go:build windows

package rasterprobe

import (
	"fmt"
	"image"
	"testing"

	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/textpaint"
)

// cellInk is one side's measurement of one probed cell.
type cellInk struct {
	Cell  sampleCell
	Stats parity.InkStats
}

// replayFixture replays a fixture through the harness and returns the
// record, the record's image and the replay's capture.
func replayFixture(t *testing.T, fixture parity.Fixture) (*parity.Record, *image.RGBA, *image.RGBA) {
	t.Helper()
	record, err := parity.LoadRecord(fixture.Name)
	if err != nil {
		t.Fatal(err)
	}
	chrome, err := record.Image()
	if err != nil {
		t.Fatal(err)
	}
	replay, err := parity.Replay(t, fixture)
	if err != nil {
		t.Fatal(err)
	}
	return record, chrome, replay.Image
}

// measureCells measures every probed cell's ink on both images of a
// recorded fixture pair.
func measureCells(record *parity.Record, chrome, gpuiImg *image.RGBA) (chromeCells, gpuiCells []cellInk) {
	for _, cell := range rasterCells {
		box := record.Boxes[cell.cellID()]
		rect := parity.ROIOfDevice(box, parity.DeviceScaleFactor, chrome.Bounds().Dx(), chrome.Bounds().Dy())
		chromeCells = append(chromeCells, cellInk{cell, parity.MeasureInk(chrome, rect, cell.FgHex, cell.BgHex)})
		gpuiCells = append(gpuiCells, cellInk{cell, parity.MeasureInk(gpuiImg, rect, cell.FgHex, cell.BgHex)})
	}
	return chromeCells, gpuiCells
}

// formatCellTable formats the two sides' measurements as the report's
// table: one line per cell with the ink ratios.
func formatCellTable(chromeCells, gpuiCells []cellInk) string {
	out := "cell            | chrome mean | gpui mean | ratio | chrome partial-ink | gpui partial-ink | chrome spread | gpui spread\n"
	for i := range chromeCells {
		c, g := chromeCells[i], gpuiCells[i]
		ratio := 0.0
		if c.Stats.MeanInk > 0 {
			ratio = g.Stats.MeanInk / c.Stats.MeanInk
		}
		out += fmt.Sprintf("%-15s | %10.4f | %9.4f | %5.3f | %18.4f | %16.4f | %13.4f | %11.4f\n",
			c.Cell.cellID(), c.Stats.MeanInk, g.Stats.MeanInk, ratio,
			c.Stats.MeanPartialInk, g.Stats.MeanPartialInk,
			c.Stats.MeanChannelSpread, g.Stats.MeanChannelSpread)
	}
	return out
}

// TestRasterInkComparison is the probe's headline measurement: gpui-go
// text against Chrome text, dark/light/grey at four sizes, under
// Chrome's default (LCD) and grayscale (--disable-lcd-text) records.
func TestRasterInkComparison(t *testing.T) {
	for _, fixture := range []parity.Fixture{textpaint.RasterFixture, textpaint.RasterGrayFixture} {
		fixture := fixture
		t.Run(fixture.Name, func(t *testing.T) {
			record, chrome, gpuiImg := replayFixture(t, fixture)
			chromeCells, gpuiCells := measureCells(record, chrome, gpuiImg)
			t.Logf("%s (Chrome flags %v):\n%s", fixture.Name, fixture.ChromeFlags, formatCellTable(chromeCells, gpuiCells))
			writeCellCrops(t, fixture.Name, record, chrome, gpuiImg)
		})
	}
}

// writeCellCrops writes the report's crops: one crop per cell row
// (all four sizes of one color), Chrome above gpui-go, at 4x the
// device pixels' natural size for the report's magnified figures.
func writeCellCrops(t *testing.T, fixtureName string, record *parity.Record, chrome, gpuiImg *image.RGBA) {
	t.Helper()
	for _, color := range []string{"dark", "light", "grey"} {
		var chromeCrop, gpuiCrop *image.RGBA
		for _, cell := range rasterCells {
			if cell.Color != color {
				continue
			}
			box := record.Boxes[cell.cellID()]
			rect := parity.ROIOfDevice(box, parity.DeviceScaleFactor, chrome.Bounds().Dx(), chrome.Bounds().Dy())
			chromeCrop = hstack(chromeCrop, subImage(chrome, rect))
			gpuiCrop = hstack(gpuiCrop, subImage(gpuiImg, rect))
		}
		name := fmt.Sprintf("%s-%s.png", fixtureName, color)
		if err := writePNG(cropsPath(name), vstack(chromeCrop, gpuiCrop)); err != nil {
			t.Fatalf("writing the %s crop: %v", name, err)
		}
	}
	// One 4x magnified cell (the 16px dark sample) per fixture: the
	// edge profile figure.
	box := record.Boxes["dark-16"]
	rect := parity.ROIOfDevice(box, parity.DeviceScaleFactor, chrome.Bounds().Dx(), chrome.Bounds().Dy())
	name := fmt.Sprintf("%s-dark-16-4x.png", fixtureName)
	if err := writePNG(cropsPath(name), vstack(scaleUp(subImage(chrome, rect), 4), scaleUp(subImage(gpuiImg, rect), 4))); err != nil {
		t.Fatalf("writing the %s crop: %v", name, err)
	}
}

// cropsPath joins the captures directory.
func cropsPath(name string) string {
	return capturesDir + "/" + name
}

// subImage copies a rectangle out of an RGBA image.
func subImage(img *image.RGBA, rect image.Rectangle) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	for y := 0; y < rect.Dy(); y++ {
		for x := 0; x < rect.Dx(); x++ {
			i := (rect.Min.Y+y)*img.Stride + (rect.Min.X+x)*4
			copy(out.Pix[y*out.Stride+x*4:y*out.Stride+x*4+4], img.Pix[i:i+4])
		}
	}
	return out
}

// hstack joins two images horizontally (nil is the zero element).
func hstack(a, b *image.RGBA) *image.RGBA {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	out := image.NewRGBA(image.Rect(0, 0, a.Bounds().Dx()+b.Bounds().Dx(), maxInt(a.Bounds().Dy(), b.Bounds().Dy())))
	drawImage(out, a, 0, 0)
	drawImage(out, b, a.Bounds().Dx(), 0)
	return out
}

// vstack joins two images vertically.
func vstack(a, b *image.RGBA) *image.RGBA {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	out := image.NewRGBA(image.Rect(0, 0, maxInt(a.Bounds().Dx(), b.Bounds().Dx()), a.Bounds().Dy()+b.Bounds().Dy()))
	drawImage(out, a, 0, 0)
	drawImage(out, b, 0, a.Bounds().Dy())
	return out
}

// drawImage blits src into dst at (dx, dy).
func drawImage(dst, src *image.RGBA, dx, dy int) {
	for y := 0; y < src.Bounds().Dy(); y++ {
		for x := 0; x < src.Bounds().Dx(); x++ {
			i := y*src.Stride + x*4
			j := (y+dy)*dst.Stride + (x+dx)*4
			copy(dst.Pix[j:j+4], src.Pix[i:i+4])
		}
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
