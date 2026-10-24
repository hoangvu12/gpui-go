//go:build windows

// The three comparisons of the browser-parity harness
// (.scratch/browser-parity/spec.md, seam 1):
//
//   - Tier A, CompareBoxes: exact equality of the recorded element
//     boxes against the replayed debug bounds, per compared id, no
//     tolerance.
//   - Tier B, CompareEvents: exact equality of the event transcript
//     (kinds, targets, order, default-prevented state).
//   - Tier C, ComparePixels: approximate equality under the fixture's
//     provisional tolerance, with per-element ROI statistics so one
//     badly wrong element cannot hide inside a whole-image average
//     (research 19 §1.2; no masks — missing content is a failure).
package parity

import (
	"fmt"
	"image"
	"sort"
	"strings"
)

// CompareBoxes checks Tier A: every compared element id's replayed box
// equals the recorded Chrome box exactly. Recorded values are compared
// at the replay's float32 precision (Chrome reports doubles; a fixture
// whose geometry is not representable in float32 must be redesigned,
// not tolerated).
func CompareBoxes(record *Record, replay *ReplayResult) error {
	var problems []string
	for id, want := range record.Boxes {
		got, ok := replay.Boxes[id]
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: no replayed box (the element is missing from the view, or has no id)", id))
			continue
		}
		if float32(want.X) != got.X || float32(want.Y) != got.Y ||
			float32(want.Width) != got.Width || float32(want.Height) != got.Height {
			problems = append(problems, fmt.Sprintf("%s: recorded %s, replayed %s", id, want, got))
		}
	}
	for id := range replay.Boxes {
		if _, recorded := record.Boxes[id]; !recorded {
			problems = append(problems, fmt.Sprintf("%s: replayed but not recorded (the page has no data-id %q)", id, id))
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("Tier A boxes differ:\n\t%s", strings.Join(problems, "\n\t"))
	}
	return nil
}

// CompareEvents checks Tier B: the replayed transcript equals the
// recorded one exactly, in order.
func CompareEvents(record *Record, replay *ReplayResult) error {
	want, got := record.Events, replay.Events
	var diff []string
	i := 0
	for ; i < len(want) && i < len(got); i++ {
		if want[i] != got[i] {
			diff = append(diff, fmt.Sprintf("event %d: recorded %s, replayed %s", i+1, formatEvent(want[i]), formatEvent(got[i])))
		}
	}
	for ; i < len(want); i++ {
		diff = append(diff, fmt.Sprintf("event %d: recorded %s, replayed (missing)", i+1, formatEvent(want[i])))
	}
	for ; i < len(got); i++ {
		diff = append(diff, fmt.Sprintf("event %d: recorded (missing), replayed %s", i+1, formatEvent(got[i])))
	}
	if len(diff) > 0 {
		return fmt.Errorf("Tier B event transcript differs:\n\t%s", strings.Join(diff, "\n\t"))
	}
	return nil
}

func formatEvent(e EventRecord) string {
	prevented := ""
	if e.DefaultPrevented {
		prevented = " (defaultPrevented)"
	}
	// The key and composition fields ride along readably (browser-core
	// tickets 13 and 18): a key entry that differs only in isComposing,
	// or a composition entry that differs only in data, names it.
	fields := ""
	if e.Key != "" || e.Code != "" || e.Location != 0 || e.Focus != "" || e.IsComposing {
		fields = fmt.Sprintf(" key=%q code=%q location=%d focus=%q isComposing=%v", e.Key, e.Code, e.Location, e.Focus, e.IsComposing)
	}
	if e.Data != "" {
		fields += fmt.Sprintf(" data=%q", e.Data)
	}
	return fmt.Sprintf("%s:%s%s%s", e.Kind, e.Target, prevented, fields)
}

// PixelStats is one region's Tier C statistics: the mean per-channel
// difference over the region's pixels, and the fraction of pixels
// whose mean channel difference exceeds the hot threshold.
type PixelStats struct {
	Width, Height int
	Mean          float64
	HotFraction   float64
}

// hotThreshold is the per-pixel mean channel difference above which a
// pixel counts as hot: clearly-ink differences, not antialiasing
// noise. Provisional like every Tier C number here (research 19).
const hotThreshold = 32.0

// ComparePixels checks Tier C: the replayed frame approximates the
// recorded Chrome screenshot within the fixture's provisional
// tolerance, image-wide and within every compared element's box. A
// size mismatch is a hard failure (not a tolerance question).
func ComparePixels(record *Record, replay *ReplayResult, tolerance PixelTolerance) error {
	want, err := record.Image()
	if err != nil {
		return err
	}
	got := replay.Image
	if got == nil {
		return fmt.Errorf("Tier C: the replay captured no image")
	}
	if want.Bounds() != got.Bounds() {
		return fmt.Errorf("Tier C: capture size %v, recorded %v (the device pixel ratio or window size differs between the recording and the replay)", got.Bounds().Dx(), want.Bounds().Dx())
	}

	var problems []string
	whole := pixelDiff(want, got, image.Rect(0, 0, want.Bounds().Dx(), want.Bounds().Dy()))
	if whole.Mean > tolerance.MaxMeanChannelDiff {
		problems = append(problems, fmt.Sprintf("whole image: mean channel diff %.3f > %.3f", whole.Mean, tolerance.MaxMeanChannelDiff))
	}
	if whole.HotFraction > tolerance.MaxHotPixelFraction {
		problems = append(problems, fmt.Sprintf("whole image: hot pixel fraction %.4f > %.4f (pixels differing by more than %.0f per channel)", whole.HotFraction, tolerance.MaxHotPixelFraction, hotThreshold))
	}

	// Per-element ROI statistics, in the fixture's box order.
	scale := replay.Scale
	for _, id := range sortedBoxIDs(record) {
		box := record.Boxes[id]
		rect := image.Rect(
			int(box.X*scale+0.5), int(box.Y*scale+0.5),
			int((box.X+box.Width)*scale+0.5), int((box.Y+box.Height)*scale+0.5))
		if rect.Min.X < 0 {
			rect.Min.X = 0
		}
		if rect.Min.Y < 0 {
			rect.Min.Y = 0
		}
		if rect.Max.X > want.Bounds().Dx() {
			rect.Max.X = want.Bounds().Dx()
		}
		if rect.Max.Y > want.Bounds().Dy() {
			rect.Max.Y = want.Bounds().Dy()
		}
		if rect.Empty() {
			continue
		}
		roi := pixelDiff(want, got, rect)
		if roi.Mean > tolerance.MaxROIMeanChannelDiff {
			problems = append(problems, fmt.Sprintf("%s: mean channel diff %.3f > %.3f", id, roi.Mean, tolerance.MaxROIMeanChannelDiff))
		}
		if roi.HotFraction > tolerance.MaxHotPixelFraction {
			problems = append(problems, fmt.Sprintf("%s: hot pixel fraction %.4f > %.4f", id, roi.HotFraction, tolerance.MaxHotPixelFraction))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("Tier C pixels differ beyond the provisional tolerance (measured at %dx%d):\n\t%s",
			want.Bounds().Dx(), want.Bounds().Dy(), strings.Join(problems, "\n\t"))
	}
	return nil
}

// sortedBoxIDs lists the record's box ids in a stable order for the
// ROI report.
func sortedBoxIDs(record *Record) []string {
	ids := make([]string, 0, len(record.Boxes))
	for id := range record.Boxes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// pixelDiff computes the Tier C statistics of two same-size RGBA
// images over a region: the mean over pixels of the mean absolute
// per-channel difference (RGB only; both sides are opaque), and the
// fraction of pixels whose mean channel difference exceeds the hot
// threshold. The region clamps into the image (a scrolled-out element
// can record a box with negative origin).
func pixelDiff(want, got *image.RGBA, region image.Rectangle) PixelStats {
	if region.Min.X < 0 {
		region.Min.X = 0
	}
	if region.Min.Y < 0 {
		region.Min.Y = 0
	}
	if region.Max.X > want.Bounds().Dx() {
		region.Max.X = want.Bounds().Dx()
	}
	if region.Max.Y > want.Bounds().Dy() {
		region.Max.Y = want.Bounds().Dy()
	}
	if region.Empty() {
		return PixelStats{}
	}
	var sum float64
	var hot int
	pixels := 0
	for y := region.Min.Y; y < region.Max.Y; y++ {
		for x := region.Min.X; x < region.Max.X; x++ {
			i := y*want.Stride + x*4
			dr := absDiff(want.Pix[i], got.Pix[i])
			dg := absDiff(want.Pix[i+1], got.Pix[i+1])
			db := absDiff(want.Pix[i+2], got.Pix[i+2])
			mean := float64(dr+dg+db) / 3
			sum += mean
			if mean > hotThreshold {
				hot++
			}
			pixels++
		}
	}
	return PixelStats{
		Width:       region.Dx(),
		Height:      region.Dy(),
		Mean:        sum / float64(pixels),
		HotFraction: float64(hot) / float64(pixels),
	}
}

func absDiff(a, b byte) byte {
	if a > b {
		return a - b
	}
	return b - a
}

// MeasurePixels reports the Tier C statistics of a replay against its
// record without gating on any tolerance: the fixture author's tool
// for choosing a provisional tolerance from measured numbers (the
// recorded-vs-replayed diff, image-wide and per box, plus the
// deliberately-wrong variants as the defect calibration of research
// 19 §1.4).
func MeasurePixels(record *Record, replay *ReplayResult) (whole PixelStats, rois map[string]PixelStats, err error) {
	want, err := record.Image()
	if err != nil {
		return PixelStats{}, nil, err
	}
	got := replay.Image
	if got == nil || want.Bounds() != got.Bounds() {
		return PixelStats{}, nil, fmt.Errorf("no comparable capture (recorded %v, replayed %v)", want.Bounds(), got.Bounds())
	}
	whole = pixelDiff(want, got, image.Rect(0, 0, want.Bounds().Dx(), want.Bounds().Dy()))
	rois = map[string]PixelStats{}
	scale := replay.Scale
	for _, id := range sortedBoxIDs(record) {
		box := record.Boxes[id]
		rect := image.Rect(
			int(box.X*scale+0.5), int(box.Y*scale+0.5),
			int((box.X+box.Width)*scale+0.5), int((box.Y+box.Height)*scale+0.5))
		rois[id] = pixelDiff(want, got, rect)
	}
	return whole, rois, nil
}
