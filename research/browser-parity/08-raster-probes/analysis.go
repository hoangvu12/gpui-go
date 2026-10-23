//go:build windows

// Package rasterprobe is ticket 08's measurement code: the analysis
// behind research/browser-parity/08-raster-probes/README.md. It reads
// the harness's recorded Chrome pages (internal/parity/testdata/
// textpaint-*), replays the same views through the harness, and
// measures the two sides' text ink — mean ink, coverage histograms,
// edge darkness, subpixel fringe spread, and the synthetic-italic
// slant — plus gpui-go's raw glyph coverage through the public
// TextSystem raster API.
//
// Everything here is measurement, not a gate: the regression gates for
// these fixtures live in internal/parity's TestFixtures. The probe's
// output is the report's numbers and its checked-in PNG crops.
package rasterprobe

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hoangvu12/gpui-go/internal/parity"
)

// sampleCell names one probed cell of the raster fixtures: a color
// column ("dark", "light", "grey") at a logical size.
type sampleCell struct {
	Color string
	Size  float32
	FgHex uint32
	BgHex uint32
}

// rasterCells lists the raster fixture's cells in id order.
var rasterCells = []sampleCell{
	{"dark", 12, 0x000000, 0xffffff},
	{"light", 12, 0xffffff, 0x000000},
	{"grey", 12, 0x808080, 0xffffff},
	{"dark", 16, 0x000000, 0xffffff},
	{"light", 16, 0xffffff, 0x000000},
	{"grey", 16, 0x808080, 0xffffff},
	{"dark", 24, 0x000000, 0xffffff},
	{"light", 24, 0xffffff, 0x000000},
	{"grey", 24, 0x808080, 0xffffff},
	{"dark", 32, 0x000000, 0xffffff},
	{"light", 32, 0xffffff, 0x000000},
	{"grey", 32, 0x808080, 0xffffff},
}

// cellID is the cell's data-id in the fixture pages and views.
func (c sampleCell) cellID() string {
	return fmt.Sprintf("%s-%d", c.Color, int(c.Size))
}

// diffStats is the plain per-channel difference between two same-size
// images (the same statistic compare.go's pixelDiff computes, reported
// here for the two Chrome records against each other).
type diffStats struct {
	Mean        float64
	Max         byte
	DifferCount int
	Pixels      int
}

// diffImages computes diffStats over two same-size RGBA images.
func diffImages(a, b *image.RGBA) diffStats {
	stats := diffStats{Pixels: a.Bounds().Dx() * a.Bounds().Dy()}
	for y := 0; y < a.Bounds().Dy(); y++ {
		for x := 0; x < a.Bounds().Dx(); x++ {
			i := y*a.Stride + x*4
			var sum byte
			max := byte(0)
			for ch := 0; ch < 3; ch++ {
				d := a.Pix[i+ch]
				if b.Pix[i+ch] > d {
					d = b.Pix[i+ch] - d
				} else {
					d = d - b.Pix[i+ch]
				}
				if d > max {
					max = d
				}
				sum += d / 3
			}
			stats.Mean += float64(sum)
			if max > 0 {
				stats.DifferCount++
			}
			if max > stats.Max {
				stats.Max = max
			}
		}
	}
	if stats.Pixels > 0 {
		stats.Mean /= float64(stats.Pixels)
	}
	return stats
}

// capturesDir is the report's checked-in capture directory, resolved
// against the package directory (go test runs with the package
// directory as the working directory).
var capturesDir = "captures"

// writePNG writes an image as a PNG file (the report's crops).
func writePNG(path string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return png.Encode(file, img)
}

// scaleUp upscales an image by an integer factor with nearest
// neighbor sampling (the report's magnified crops: the device pixels
// stay crisp).
func scaleUp(img *image.RGBA, factor int) *image.RGBA {
	if factor <= 1 || img == nil {
		return img
	}
	out := image.NewRGBA(image.Rect(0, 0, img.Bounds().Dx()*factor, img.Bounds().Dy()*factor))
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			i := y*img.Stride + x*4
			for dy := 0; dy < factor; dy++ {
				for dx := 0; dx < factor; dx++ {
					j := (y*factor+dy)*out.Stride + (x*factor+dx)*4
					copy(out.Pix[j:j+4], img.Pix[i:i+4])
				}
			}
		}
	}
	return out
}

// loadFixtureRecord loads one fixture's record and image.
func loadFixtureRecord(t TB, name string) (*parity.Record, *image.RGBA) {
	t.Helper()
	record, err := parity.LoadRecord(name)
	if err != nil {
		t.Fatal(err)
	}
	img, err := record.Image()
	if err != nil {
		t.Fatal(err)
	}
	return record, img
}

// TB is the subset of testing.TB the probe uses (kept minimal so the
// package compiles without importing testing outside tests).
type TB interface {
	Helper()
	Logf(format string, args ...any)
	Fatalf(format string, args ...any)
	Fatal(args ...any)
}

// registryState reports whether the per-display ClearType tuner key
// exists that makes Chrome read DirectWrite's gamma/contrast instead
// of Skia's compile-time defaults (ui/gfx/font_util_win.cc
// GetTextParameters: the values are applied "[o]nly ... if the user has
// the appropriate registry keys set"). The key Chrome opens is
// HKCU\SOFTWARE\Microsoft\Avalon.Graphics\<primary display name>; the
// parent existing is not enough.
func registryState(t TB, dwriteGamma float32) {
	t.Helper()
	out, err := exec.Command("powershell", "-NoProfile", "-Command",
		"@(Get-ChildItem 'HKCU:\\SOFTWARE\\Microsoft\\Avalon.Graphics' -ErrorAction SilentlyContinue).Count").Output()
	if err != nil {
		t.Logf("Chrome gamma/contrast registry key check failed (%v); treating the per-display key as absent - Chrome uses Skia's compile-time SK_GAMMA_* defaults", err)
		return
	}
	count := strings.TrimSpace(string(out))
	if count == "" || count == "0" || strings.EqualFold(count, "false") {
		t.Logf("HKCU\\SOFTWARE\\Microsoft\\Avalon.Graphics has no per-display subkeys (count %q): Chrome's FontUtilWin::GetTextSettingsRegistryKey finds nothing, so Chrome keeps Skia's compile-time gamma/contrast (sRGB gamma, SK_GAMMA_CONTRAST) instead of DirectWrite's gamma %.2f", count, dwriteGamma)
		return
	}
	t.Logf("HKCU\\SOFTWARE\\Microsoft\\Avalon.Graphics has %s subkeys: Chrome would read DirectWrite's gamma/contrast through the per-display key (the ClearType tuner ran)", count)
}
