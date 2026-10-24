//go:build windows

package rasterprobe

import (
	"image"
	"math"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/textpaint"
)

// slantFit is the measured ink shear of one text row.
type slantFit struct {
	// Shear is dx/dy of the per-row ink centroid (positive = the ink
	// leans right going down, the italic direction).
	Shear float64
	// AngleDeg is atan(Shear) in degrees.
	AngleDeg float64
	// Rows is the number of ink rows the fit used.
	Rows int
	// Residual is the fit's RMS residual in pixels (how stem-like the
	// measured ink was).
	Residual float64
	// TopX/BottomX are the centroid x at the first and last ink row.
	TopX, BottomX float64
}

// fitSlant measures the ink shear of one region of a captured frame:
// the per-row ink centroid (x weighted by coverage, coverage derived
// from the given fg/bg hex), then a least-squares line x(y).
func fitSlant(img *image.RGBA, region image.Rectangle, fgHex, bgHex uint32) slantFit {
	fg, bg := hexChannels(fgHex), hexChannels(bgHex)
	type rowInk struct {
		y    int
		xBar float64
	}
	var rows []rowInk
	for y := region.Min.Y; y < region.Max.Y; y++ {
		sum, weighted := 0.0, 0.0
		for x := region.Min.X; x < region.Max.X; x++ {
			i := y*img.Stride + x*4
			c := 0.0
			for ch := 0; ch < 3; ch++ {
				c += coverage(float64(img.Pix[i+ch]), fg[ch], bg[ch])
			}
			c /= 3
			sum += c
			weighted += c * float64(x)
		}
		// Skip rows with barely any ink (descender tips, AA noise):
		// their centroids jitter.
		if sum > 1.0 {
			rows = append(rows, rowInk{y: y, xBar: weighted / sum})
		}
	}
	fit := slantFit{Rows: len(rows)}
	if len(rows) < 2 {
		return fit
	}
	// Least squares x = a + b*y.
	n := float64(len(rows))
	var sy, syy, sx, sxy float64
	for _, r := range rows {
		sy += float64(r.y)
		syy += float64(r.y) * float64(r.y)
		sx += r.xBar
		sxy += float64(r.y) * r.xBar
	}
	denominator := n*syy - sy*sy
	if denominator == 0 {
		return fit
	}
	b := (n*sxy - sy*sx) / denominator
	a := (sx - b*sy) / n
	var residual float64
	for _, r := range rows {
		d := r.xBar - (a + b*float64(r.y))
		residual += d * d
	}
	fit.Shear = b
	fit.AngleDeg = math.Atan(b) * 180 / math.Pi
	fit.Residual = math.Sqrt(residual / n)
	fit.TopX = a + b*float64(rows[0].y)
	fit.BottomX = a + b*float64(rows[len(rows)-1].y)
	return fit
}

// hexChannels splits a 0xRRGGBB hex into channels.
func hexChannels(hex uint32) [3]float64 {
	return [3]float64{
		float64((hex >> 16) & 0xFF),
		float64((hex >> 8) & 0xFF),
		float64(hex & 0xFF),
	}
}

// coverage maps one channel's pixel value onto [0, 1] (analysis.go's
// local twin, kept for the slant fit's independence from the parity
// helper's internals).
func coverage(pixel, fg, bg float64) float64 {
	if fg == bg {
		return 0
	}
	c := (pixel - bg) / (fg - bg)
	if c < 0 {
		return 0
	}
	if c > 1 {
		return 1
	}
	return c
}

// TestItalicSlant is the synthetic-italic probe (ticket 08's second
// half): Chrome synthesizes an italic slant on a family without an
// italic face as a -1/4 x-shear (font_platform_data_win.cc:37-38),
// gpui-go through DirectWrite's oblique simulation (glyph.rs
// NativeFace::new). The measurement: the per-row ink centroid's shear
// on both sides of the recorded fixture.
func TestItalicSlant(t *testing.T) {
	fixture := textpaint.ItalicFixture
	record, chrome, gpuiImg := replayFixture(t, fixture)

	rows := []struct {
		id string
		fg uint32
		bg uint32
	}{
		{"tahoma-upright", 0x000000, 0xffffff},
		{"tahoma-italic", 0x000000, 0xffffff},
		{"segoe-italic", 0x000000, 0xffffff},
	}
	for _, row := range rows {
		box := record.Boxes[row.id]
		rect := parity.ROIOfDevice(box, parity.DeviceScaleFactor, chrome.Bounds().Dx(), chrome.Bounds().Dy())
		chromeFit := fitSlant(chrome, rect, row.fg, row.bg)
		gpuiFit := fitSlant(gpuiImg, rect, row.fg, row.bg)
		t.Logf("%s: chrome shear %.4f (%.2f deg, %d rows, residual %.2f px, dx %.1f px); gpui shear %.4f (%.2f deg, %d rows, residual %.2f px, dx %.1f px)",
			row.id,
			chromeFit.Shear, chromeFit.AngleDeg, chromeFit.Rows, chromeFit.Residual, chromeFit.BottomX-chromeFit.TopX,
			gpuiFit.Shear, gpuiFit.AngleDeg, gpuiFit.Rows, gpuiFit.Residual, gpuiFit.BottomX-gpuiFit.TopX)
	}

	// rev 15.1 (ticket 77): the shear gate. Chrome's synthetic italic is
	// the constant -1/4 (Skia setSkewX(-SK_Scalar1/4)); the record is
	// fixed, so the Chrome side pins it to four decimals. gpui-go now
	// applies the SAME constant in the raster transform (glyph.rs
	// synthetic_italic_shear, replacing the DirectWrite oblique
	// simulation that measured -0.3399 in rev 15), so the synthetic row
	// must measure -0.25 on both sides, the upright control 0, and the
	// real-italic control (a genuine italic face — no synthesis) must
	// keep matching Chrome.
	fits := make(map[string]slantFit)
	gfits := make(map[string]slantFit)
	for _, row := range rows {
		box := record.Boxes[row.id]
		rect := parity.ROIOfDevice(box, parity.DeviceScaleFactor, chrome.Bounds().Dx(), chrome.Bounds().Dy())
		fits[row.id] = fitSlant(chrome, rect, row.fg, row.bg)
		gfits[row.id] = fitSlant(gpuiImg, rect, row.fg, row.bg)
	}
	if c := fits["tahoma-italic"].Shear; c < -0.26 || c > -0.24 {
		t.Errorf("chrome tahoma-italic shear = %.4f, want -0.25 (Skia's constant; the record changed?)", c)
	}
	if g := gfits["tahoma-italic"].Shear; g < -0.27 || g > -0.23 {
		t.Errorf("gpui tahoma-italic shear = %.4f, want -0.25 (rev 15.1 applies Chrome's exact -1/4 shear; -0.34 means the DirectWrite oblique simulation returned)", g)
	}
	if g := gfits["tahoma-upright"].Shear; g < -0.01 || g > 0.01 {
		t.Errorf("gpui tahoma-upright shear = %.4f, want 0 (upright control)", g)
	}
	if d := gfits["segoe-italic"].Shear - fits["segoe-italic"].Shear; d < -0.02 || d > 0.02 {
		t.Errorf("segoe-italic (a real italic face) shears differ: gpui %.4f vs chrome %.4f — the fix must not touch real italic faces", gfits["segoe-italic"].Shear, fits["segoe-italic"].Shear)
	}

	// The report's crop: the three rows, Chrome above gpui-go, at 4x
	// for the slant to read at a glance.
	var chromeCrop, gpuiCrop *image.RGBA
	for _, row := range rows {
		box := record.Boxes[row.id]
		rect := parity.ROIOfDevice(box, parity.DeviceScaleFactor, chrome.Bounds().Dx(), chrome.Bounds().Dy())
		chromeCrop = vstack(chromeCrop, subImage(chrome, rect))
		gpuiCrop = vstack(gpuiCrop, subImage(gpuiImg, rect))
	}
	if err := writePNG(cropsPath("textpaint-italic.png"), vstack(chromeCrop, gpuiCrop)); err != nil {
		t.Fatal(err)
	}
	// And a 4x crop of the two synthetic rows alone (the money shot).
	var chromeItalic, gpuiItalic *image.RGBA
	for _, row := range rows {
		if row.id == "tahoma-upright" {
			continue
		}
		box := record.Boxes[row.id]
		rect := parity.ROIOfDevice(box, parity.DeviceScaleFactor, chrome.Bounds().Dx(), chrome.Bounds().Dy())
		chromeItalic = vstack(chromeItalic, subImage(chrome, rect))
		gpuiItalic = vstack(gpuiItalic, subImage(gpuiImg, rect))
	}
	if err := writePNG(cropsPath("textpaint-italic-4x.png"), vstack(scaleUp(chromeItalic, 4), scaleUp(gpuiItalic, 4))); err != nil {
		t.Fatal(err)
	}

	// The resolve sanity: Tahoma must resolve to a real Tahoma face
	// (not the service fallback) on the gpui side, or the measurement
	// compares the wrong fonts.
	ts, err := gpui.DefaultTextSystem()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range []string{"Tahoma", ".SystemUIFont"} {
		identity, err := ts.ResolveFont(gpui.FontDescriptor{Family: family, Weight: 400, Style: gpui.FontStyleItalic})
		if err != nil {
			t.Fatalf("resolving %s italic: %v", family, err)
		}
		t.Logf("gpui resolve %s italic: family found %v, style %d, weight %.0f", family, identity.FamilyFound, identity.Style, identity.Weight)
	}
}
