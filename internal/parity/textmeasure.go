//go:build windows

// The text-ink measurement helper of the browser-parity harness: the
// statistics a text probe compares beyond the plain per-pixel diff of
// compare.go — mean ink (total coverage), the coverage histogram and
// the partial-coverage (antialiasing) distribution, and the channel
// spread that distinguishes subpixel (LCD) fringes from grayscale
// edges. Ticket 08's rasterization probe is the first consumer; the
// numbers it reports are what a Tier C text tolerance is calibrated
// from.
//
// Coverage is derived per channel from the two colors the fixture
// painted with: c = (pixel - background) / (foreground - background),
// clamped to [0, 1] — 0 is pure background, 1 is fully covered
// foreground, and the antialiased edge in between is the coverage the
// rasterizer chose. For dark-on-light and light-on-dark alike the
// formula is sign-correct; a grey-on-grey sample works too, as long as
// foreground and background differ. Colors are given as 0xRRGGBB hex,
// the fixtures' own constants.
package parity

import (
	"fmt"
	"image"
)

// InkBuckets is the coverage histogram's bucket count: coverage [0, 1]
// is split into InkBuckets equal bands.
const InkBuckets = 16

// inkThreshold is the coverage above which a pixel counts as ink (the
// footprint), and fullThreshold the coverage at or above which a pixel
// counts as a stem interior. Partial pixels are the ink between the
// two: the antialiased edges.
const (
	inkThreshold  = 0.05
	fullThreshold = 0.95
)

// InkStats is one region's text-ink statistics.
type InkStats struct {
	// Pixels is the measured pixel count.
	Pixels int
	// MeanInk is the mean coverage over the whole region (total ink
	// divided by the area): the sample's overall darkness on its
	// background. A heavier or lighter rasterization moves it.
	MeanInk float64
	// InkFraction is the fraction of pixels with coverage above
	// inkThreshold (the ink footprint, including antialiased edges).
	InkFraction float64
	// FullFraction is the fraction of pixels with coverage at or above
	// fullThreshold (the stems' interiors).
	FullFraction float64
	// PartialFraction is the fraction of pixels with coverage strictly
	// between the two thresholds (the antialiased edges).
	PartialFraction float64
	// MeanPartialInk is the mean coverage over the partial pixels —
	// the edge darkness. Gamma/contrast corrections move this without
	// necessarily moving the footprint.
	MeanPartialInk float64
	// MeanChannelSpread is the mean over ink pixels of (max channel
	// coverage - min channel coverage): 0 for grayscale edges, large
	// for LCD fringes. It is the subpixel-AA detector.
	MeanChannelSpread float64
	// Histogram is the fraction of the region's pixels per coverage
	// band: band i is [i/InkBuckets, (i+1)/InkBuckets), with the last
	// band closed at 1. Two rasters with the same mean can differ in
	// the curve's shape; this is the shape.
	Histogram [InkBuckets]float64
}

// String formats the stats as one report line.
func (s InkStats) String() string {
	return fmt.Sprintf("mean %.4f ink %.4f full %.4f partial %.4f partial-ink %.4f spread %.4f",
		s.MeanInk, s.InkFraction, s.FullFraction, s.PartialFraction, s.MeanPartialInk, s.MeanChannelSpread)
}

// ROIOfDevice converts a recorded logical-pixel box into the device
// rectangle to measure, at the replay's device pixel ratio (the same
// rounding compare.go applies to its ROI statistics).
func ROIOfDevice(box Box, scale float32, width, height int) image.Rectangle {
	rect := image.Rect(
		int(box.X*scale+0.5), int(box.Y*scale+0.5),
		int((box.X+box.Width)*scale+0.5), int((box.Y+box.Height)*scale+0.5))
	if rect.Min.X < 0 {
		rect.Min.X = 0
	}
	if rect.Min.Y < 0 {
		rect.Min.Y = 0
	}
	if rect.Max.X > width {
		rect.Max.X = width
	}
	if rect.Max.Y > height {
		rect.Max.Y = height
	}
	return rect
}

// MeasureInk measures one region of a captured frame (the recorded
// Chrome screenshot or the replayed gpui capture, both opaque RGBA at
// device resolution) as text ink between the fixture's foreground and
// background colors, given as 0xRRGGBB hex. See the package comment
// for the coverage derivation.
func MeasureInk(img *image.RGBA, region image.Rectangle, foreground, background uint32) InkStats {
	stats := InkStats{}
	if region.Empty() {
		return stats
	}
	fg := [3]float64{
		float64((foreground >> 16) & 0xFF),
		float64((foreground >> 8) & 0xFF),
		float64(foreground & 0xFF),
	}
	bg := [3]float64{
		float64((background >> 16) & 0xFF),
		float64((background >> 8) & 0xFF),
		float64(background & 0xFF),
	}

	var sum, partialSum, spreadSum float64
	var ink, full, partial, spreadCount int
	pixels := 0
	for y := region.Min.Y; y < region.Max.Y; y++ {
		for x := region.Min.X; x < region.Max.X; x++ {
			i := y*img.Stride + x*4
			mean := 0.0
			minC, maxC := 1.0, 0.0
			for ch := 0; ch < 3; ch++ {
				c := coverage(float64(img.Pix[i+ch]), fg[ch], bg[ch])
				mean += c
				if c < minC {
					minC = c
				}
				if c > maxC {
					maxC = c
				}
			}
			mean /= 3
			sum += mean
			stats.Histogram[histogramBucket(mean)]++
			if mean > inkThreshold {
				ink++
				spreadSum += maxC - minC
				spreadCount++
				if mean >= fullThreshold {
					full++
				} else {
					partial++
					partialSum += mean
				}
			}
			pixels++
		}
	}
	stats.Pixels = pixels
	if pixels > 0 {
		stats.MeanInk = sum / float64(pixels)
		stats.InkFraction = float64(ink) / float64(pixels)
		stats.FullFraction = float64(full) / float64(pixels)
		stats.PartialFraction = float64(partial) / float64(pixels)
		for i := range stats.Histogram {
			stats.Histogram[i] /= float64(pixels)
		}
	}
	if partial > 0 {
		stats.MeanPartialInk = partialSum / float64(partial)
	}
	if spreadCount > 0 {
		stats.MeanChannelSpread = spreadSum / float64(spreadCount)
	}
	return stats
}

// coverage maps one channel's pixel value onto [0, 1] between the
// background and the foreground channel value.
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

// histogramBucket maps a coverage in [0, 1] onto its histogram band.
func histogramBucket(c float64) int {
	b := int(c * InkBuckets)
	if b >= InkBuckets {
		b = InkBuckets - 1
	}
	if b < 0 {
		b = 0
	}
	return b
}
