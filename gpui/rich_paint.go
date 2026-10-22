package gpui

// Base ticket 10 (rich text): the per-run styled text painter, ticket
// 02's decided fallback for the frozen rev-11 native ABI.
//
// The pinned paint path (R:text_system/line.rs:247-464 at 254b5dbd:
// paint_visual_text / paint_visual_background → paint_visual_line →
// paint_text_fragment + paint_fragment_decorations_at) paints every
// PaintFragment with the fragment's own PaintStyle: glyphs in
// style.color, a background quad over the fragment's x range and row,
// an underline at baseline + underline_offset and a strikethrough at
// baseline + strikethrough_offset. The native run, fragment and glyph
// records carry no color, decoration or byte index
// (reference/native/src/text.rs:327,518-553,1099-1109), so the port —
// per ticket 02's answer — shapes the document ONCE (the runs' fonts
// still shape together, keeping kerning and ligatures across style
// boundaries) and paints each run clipped to its selection rectangles
// (WrappedLine.SelectionRects, the pinned selection_bounds):
//
//   - background pass: one Fill quad per run rectangle (the rectangle
//     is the run's x extent on the row with the row's line height — the
//     pin's fragment x_range by line_height, line.rs:455-463);
//   - foreground pass: per run rectangle the underline and
//     strikethrough (line.rs:431-454), then the row's glyphs once per
//     color span under a content mask limited to that span's x extent.
//     A span edge at the row's outer edge is NOT clipped, so a
//     single-color row paints exactly like the pin (overhanging ink
//     included).
//
// Recorded deviations (ticket 02):
//
//   - a ligature spanning a color boundary stays ONE glyph (the runs
//     are shaped together; the pin instead carries the paint style on
//     each Parley fragment, R:text_system/line_layout.rs:418-435); the
//     glyph is painted once
//     per touching color span and clipped at the boundary's
//     selection-rectangle x. Parley assigns each ligature component a
//     share of the advance, so the boundary falls inside the glyph and
//     each side shows its own run's color (observed with Calibri's
//     "ffi", richtextspec TestRealRichText); a backend that reported the
//     ligature as one atomic cluster would give the whole glyph the
//     color of the run owning that rectangle (ticket 02's "one color");
//   - the decoration offsets are derived, not native: the pin reads
//     Parley's per-run underline/strikethrough offsets
//     (gpui_ce_parley text_system.rs:1250-1265), which do not cross the
//     ABI; the port uses the pin's own fallbacks for an absent offset —
//     underline at baseline + descent * 0.618 (line.rs:437) and
//     strikethrough at baseline - ascent * 0.5 (line.rs:448, the same
//     half-ascent HarfBuzz default Parley applies without an OS/2
//     strikeout record, parley-0.11.1 layout/data.rs:426-432).

import (
	"fmt"
	"sort"
)

// textPaintPass selects the background or the foreground pass (the
// pinned TextPaintPass, R:text_system/line.rs:180-184).
type textPaintPass uint8

const (
	// textPaintBackground paints the run backgrounds.
	textPaintBackground textPaintPass = iota
	// textPaintForeground paints the glyphs and decorations.
	textPaintForeground
)

// runSegment is one run's rectangle on one visual row.
type runSegment struct {
	run  TextRun
	left float32
	w    float32
}

// alignedVisualOriginX is the pinned aligned_visual_origin_x
// (R:text_system/line.rs:466-477).
func alignedVisualOriginX(originX, alignWidth, lineWidth float32, align TextAlign) float32 {
	switch align {
	case TextAlignCenter:
		return (originX*2.0 + alignWidth - lineWidth) / 2.0
	case TextAlignRight:
		return originX + alignWidth - lineWidth
	default:
		return originX
	}
}

// paintStyledDocument paints one shaped document's runs (the pinned
// paint_visual_text / paint_visual_background, R:text_system/line.rs:
// 321-419, with the per-run fallback described in the file header).
// Runs cover the document in logical order; alignWidth nil aligns each
// row within its own advance.
func paintStyledDocument(w *Window, doc shapedText, runs []TextRun, origin Point, lineHeight float32, align TextAlign, alignWidth *float32, pass textPaintPass) error {
	if doc == nil {
		return fmt.Errorf("gpui: paint styled text: no shaped document")
	}
	rows := doc.lineCount()
	if rows == 0 {
		return nil
	}
	paintWidth := float32(0)
	for i := 0; i < rows; i++ {
		paintWidth = max32(paintWidth, doc.lineAdvance(i))
	}
	lineBounds := Bounds{Origin: origin, Size: Size{Width: paintWidth, Height: lineHeight * float32(rows)}}
	ascent, descent := doc.ascent(), doc.descent()
	paddingTop := (lineHeight - ascent - descent) / 2

	var paintErr error
	w.PaintLayer(lineBounds, func(w *Window) {
		for row := 0; row < rows && paintErr == nil; row++ {
			advance := doc.lineAdvance(row)
			width := advance
			if alignWidth != nil {
				width = *alignWidth
			}
			lineOrigin := Point{
				X: alignedVisualOriginX(origin.X, width, advance, align),
				Y: origin.Y + float32(row)*lineHeight,
			}
			baseline := lineOrigin.Y + paddingTop + ascent
			segments := rowSegments(doc, runs, row, lineHeight)
			if pass == textPaintBackground {
				for _, segment := range segments {
					if segment.run.BackgroundColor == nil {
						continue
					}
					w.PaintQuad(Fill(Bounds{
						Origin: Point{X: lineOrigin.X + segment.left, Y: lineOrigin.Y},
						Size:   Size{Width: segment.w, Height: lineHeight},
					}, SolidBackground(*segment.run.BackgroundColor)))
				}
				continue
			}
			for _, segment := range segments {
				paintRunDecorations(w, segment, lineOrigin.X, baseline, ascent, descent)
			}
			paintErr = paintRowGlyphs(w, doc, row, segments, lineOrigin, baseline)
		}
	})
	return paintErr
}

// rowSegments returns the paint-style runs' rectangles on one visual
// row in visual (x) order, adjacent same-style rectangles merged (the
// pin's fragments split only where the paint style changes).
func rowSegments(doc shapedText, runs []TextRun, row int, lineHeight float32) []runSegment {
	rowStart, rowEnd := doc.lineRange(row)
	rowTop := float32(row) * lineHeight
	var segments []runSegment
	offset := 0
	for _, run := range runs {
		start, end := offset, offset+run.Len
		offset = end
		if start < rowStart {
			start = rowStart
		}
		if end > rowEnd {
			end = rowEnd
		}
		if start >= end {
			continue
		}
		for _, rect := range doc.selectionRects(start, end, lineHeight) {
			// Keep only this row's rectangles (rows sit at
			// row*lineHeight, R:text_system/line_layout.rs:897-905).
			if rect.Y < rowTop-lineHeight/2 || rect.Y >= rowTop+lineHeight/2 || rect.W <= 0 {
				continue
			}
			segments = append(segments, runSegment{run: run, left: rect.X, w: rect.W})
		}
	}
	sort.SliceStable(segments, func(i, j int) bool { return segments[i].left < segments[j].left })
	var merged []runSegment
	for _, segment := range segments {
		if n := len(merged); n > 0 && samePaintStyle(merged[n-1].run, segment.run) &&
			abs32(merged[n-1].left+merged[n-1].w-segment.left) < 0.01 {
			merged[n-1].w = segment.left + segment.w - merged[n-1].left
			continue
		}
		merged = append(merged, segment)
	}
	return merged
}

// paintRunDecorations paints one run rectangle's underline and
// strikethrough (R:text_system/line.rs:431-454): the decoration color
// defaults to the run color; the offsets are the pin's fallbacks (file
// header).
func paintRunDecorations(w *Window, segment runSegment, lineOriginX, baseline, ascent, descent float32) {
	x := lineOriginX + segment.left
	if segment.run.Underline != nil {
		underline := *segment.run.Underline
		if underline.Color == nil {
			color := segment.run.Color
			underline.Color = &color
		}
		w.PaintUnderline(Point{X: x, Y: baseline + descent*0.618}, segment.w, underline)
	}
	if segment.run.Strikethrough != nil {
		strike := *segment.run.Strikethrough
		if strike.Color == nil {
			color := segment.run.Color
			strike.Color = &color
		}
		w.PaintStrikethrough(Point{X: x, Y: baseline + -ascent*0.5}, segment.w, strike)
	}
}

// paintRowGlyphs paints one visual row's glyphs once per color span
// (adjacent same-color segments merge), each under a content mask
// limited to the span's x extent; spans at the row's outer edges stay
// open on that side (the current content mask's edge). A single-color
// row paints without a mask.
func paintRowGlyphs(w *Window, doc shapedText, row int, segments []runSegment, lineOrigin Point, baseline float32) error {
	type colorSpan struct {
		color       Hsla
		left, right float32
	}
	var spans []colorSpan
	for _, segment := range segments {
		left, right := segment.left, segment.left+segment.w
		if n := len(spans); n > 0 && spans[n-1].color == segment.run.Color && abs32(spans[n-1].right-left) < 0.01 {
			spans[n-1].right = right
			continue
		}
		spans = append(spans, colorSpan{color: segment.run.Color, left: left, right: right})
	}
	if len(spans) == 0 {
		return nil
	}
	if len(spans) == 1 {
		return doc.paintLine(w, row, lineOrigin, baseline, spans[0].color)
	}
	rowLeft, rowRight := spans[0].left, spans[0].right
	for _, span := range spans {
		rowLeft = min32(rowLeft, span.left)
		rowRight = max32(rowRight, span.right)
	}
	// An open span edge (and the vertical extent) is the window's current
	// content mask edge: the span mask only ever narrows the x range.
	current := ContentMask(w)
	var err error
	for _, span := range spans {
		left := lineOrigin.X + span.left
		if span.left <= rowLeft {
			left = current.Left()
		}
		right := lineOrigin.X + span.right
		if span.right >= rowRight {
			right = current.Right()
		}
		mask := Bounds{
			Origin: Point{X: left, Y: current.Top()},
			Size:   Size{Width: max32(right-left, 0), Height: current.Size.Height},
		}
		color := span.color
		WithContentMaskVoid(w, &mask, func(w *Window) {
			if err == nil {
				err = doc.paintLine(w, row, lineOrigin, baseline, color)
			}
		})
	}
	return err
}
