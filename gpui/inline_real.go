package gpui

// This file is the REAL text system's inline layout (ticket15): the Go
// greedy row/box algorithm over the native shaping seam, plus the real
// shaped-text seam extensions (per-line paint, line facts and selection
// rects) and the real font metrics for the inline container.
//
// The pinned reference is GPUI-CE 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a:
// crates/gpui_ce_parley/src/text_system.rs::parley_paragraph_layout —
// the Parley backend performs box-aware shaping (push_inline_box) with
// greedy line breaking, aligns rows, expands each row's extents for its
// runs and boxes, and positions the boxes through align_inline_boxes.
//
// Go adaptation (recorded honestly): the port's native text ABI
// (internal/native, ticket09) exposes shaping without inline boxes, and
// the no-Rust constraint forbids extending it. The algorithm below
// reproduces the observable contract — greedy rows, box placement,
// vertical alignment, the alignment anchor, fragment geometry — by
// composing the native shaper per row:
//
//   - The document is split into SEGMENTS at box indices and style-range
//     boundaries (exactly the child boundaries the collector produces);
//     each segment carries its runs, font size and line height.
//   - Rows are assembled greedily: a row keeps filling until an item does
//     not fit. A text segment is shaped under the REMAINING row budget
//     and its first native line becomes the row's piece (the native
//     greedy break at that width is the same break Parley makes); the
//     remainder re-shapes on later rows under fresh budgets. A whole
//     segment that fits continues the row; one that does not fit moves
//     to a fresh row (first-word overflow only when the row is empty).
//     A box that does not fit breaks the row first (boxes never split).
//   - Row text extents expand per piece from the native ascent/descent
//     and the piece's line height (the Parley run-metrics model), then
//     align_inline_boxes (the exact port) settles the row geometry and
//     the box y positions.
//   - Text alignment is applied at layout time (Parley aligns rows via
//     layout.align); the shared anchor (inline_alignment_offset) and the
//     snapped placement follow the pin.
//
// Bounded deviations, recorded in the evidence: mid-word cluster-level
// breaking across segment boundaries defers to the shaper's word
// breaking (a segment that does not fit moves wholesale to the next
// row unless it overflows from an empty row); a box wider than the wrap
// width overflows its row; hard line breaks arrive through the native
// line ranges; row clamping drops excess rows after the budget instead
// of preserving trailing hard breaks; per-run paint styles are outside
// the port's run surface, so the paragraph paints with the ambient text
// style color.

import (
	"fmt"
	"sort"
)

// ---------------------------------------------------------------------------
// The real seam extensions
// ---------------------------------------------------------------------------

// inlineMetrics implements windowTextSystem: resolve the descriptor and
// scale the face metrics to the font size (the reference
// WindowTextSystem::ascent / descent / x_height over the resolved
// font). Unresolvable descriptors answer zero metrics (the reference
// logs the resolve failure and proceeds with the default id).
func (r *realWindowTextSystem) inlineMetrics(font FontDescriptor, fontSize float32) InlineTextMetrics {
	identity, err := r.system.ResolveFont(font)
	if err != nil {
		return InlineTextMetrics{}
	}
	metrics, err := r.system.FontMetrics(identity.FontID)
	if err != nil {
		return InlineTextMetrics{}
	}
	return InlineTextMetrics{
		Ascent:  metrics.AscentPx(fontSize),
		Descent: metrics.DescentPx(fontSize),
		XHeight: metrics.XHeightPx(fontSize),
	}
}

// paintLine implements shapedText: paint one visual line of the shaped
// document at the given origin and baseline (the inline piece paint
// path; the piece's native line supplies the fragments and glyphs). The
// shaped handle is registered for frame-end disposal.
func (s *realShapedText) paintLine(w *Window, lineIndex int, origin Point, baseline float32, color Hsla) error {
	frame := currentFrame(w)
	if frame.inlineShapedLines != nil {
		frame.inlineShapedLines[s.line] = struct{}{}
	}
	return frame.scene.PaintShapedTextLine(s.line, lineIndex, origin, baseline, color, s.system, s.atlas, frame.paint)
}

// lineAdvance implements shapedText: one visual line's shaped advance
// (the VisualLine::advance_width fact).
func (s *realShapedText) lineAdvance(lineIndex int) float32 {
	summary, err := s.line.Summary()
	if err != nil || lineIndex < 0 || lineIndex >= summary.LineCount {
		return 0
	}
	record, err := s.line.Line(lineIndex, 1)
	if err != nil {
		return 0
	}
	return record.AdvanceWidth
}

// lineRange implements shapedText: one visual line's byte range (the
// VisualLine::text_range fact).
func (s *realShapedText) lineRange(lineIndex int) (int, int) {
	summary, err := s.line.Summary()
	if err != nil || lineIndex < 0 || lineIndex >= summary.LineCount {
		return 0, 0
	}
	record, err := s.line.Line(lineIndex, 1)
	if err != nil {
		return 0, 0
	}
	return record.TextStart, record.TextEnd
}

// selectionRects implements shapedText: the native selection rectangles
// covering a byte range (the reference platform_layout.selection_bounds
// feeding inline_geometry).
func (s *realShapedText) selectionRects(start, end int, lineHeight float32) []TextRect {
	rects, err := s.line.SelectionRects(start, end, lineHeight)
	if err != nil {
		return nil
	}
	return rects
}

// ---------------------------------------------------------------------------
// The greedy row/box algorithm
// ---------------------------------------------------------------------------

// inlineSegment is one maximal text run between box indices and
// style-range boundaries: its document slice, covering runs, font size
// and line height.
type inlineSegment struct {
	text       string
	byteStart  int
	runs       []TextRun
	fontSize   float32
	lineHeight float32
}

// inlineAssemblyRow is one row under assembly: its pieces and boxes
// with the accumulated width.
type inlineAssemblyRow struct {
	pieceIndices []int
	boxIndices   []int
	width        float32
}

// layoutInline implements windowTextSystem: the Go greedy row/box
// algorithm over the native shaping seam (see the file header for the
// adaptation record).
func (r *realWindowTextSystem) layoutInline(request inlineLayoutRequest) (inlineLayout, error) {
	segments := splitInlineSegments(request)

	var rows []*inlineAssemblyRow
	pieces := []inlinePaintPiece{}
	boxes := make([]PositionedInlineBox, len(request.boxes))
	boxOrder := make([]int, len(request.boxes))
	for i := range boxOrder {
		boxOrder[i] = i
	}
	sort.SliceStable(boxOrder, func(i, j int) bool {
		return request.boxes[boxOrder[i]].Index < request.boxes[boxOrder[j]].Index
	})

	segIdx, segOffset := 0, 0
	boxCursor := 0

	currentRow := func() *inlineAssemblyRow {
		if len(rows) == 0 {
			rows = append(rows, &inlineAssemblyRow{})
		}
		return rows[len(rows)-1]
	}
	startNewRow := func() {
		if row := rows[len(rows)-1]; len(row.pieceIndices) == 0 && len(row.boxIndices) == 0 {
			return // never stack empty rows
		}
		rows = append(rows, &inlineAssemblyRow{})
	}

	for {
		// The current document byte position: the end of the last placed
		// text, or the next segment's start, or the document end.
		position := len(request.text)
		if segIdx < len(segments) {
			position = segments[segIdx].byteStart + segOffset
		}
		// Place every box whose insertion index has been reached.
		if boxCursor < len(boxOrder) && request.boxes[boxOrder[boxCursor]].Index <= position {
			row := currentRow()
			boxIndex := boxOrder[boxCursor]
			box := request.boxes[boxIndex]
			if request.wrapWidth != nil && row.width > 0 && row.width+box.Size.Width > *request.wrapWidth {
				startNewRow()
				row = currentRow()
			}
			boxes[boxIndex] = PositionedInlineBox{
				ID:        box.ID,
				LineIndex: len(rows) - 1,
				Bounds:    Bounds{Origin: Point{X: row.width}, Size: box.Size},
			}
			row.boxIndices = append(row.boxIndices, boxIndex)
			row.width += box.Size.Width
			boxCursor++
			continue
		}

		if segIdx >= len(segments) {
			break
		}
		segment := segments[segIdx]
		if segOffset >= len(segment.text) {
			segIdx++
			segOffset = 0
			continue
		}

		remaining := segment.text[segOffset:]
		// The runs must cover the REMAINING text exactly (the native
		// shaper validates run coverage): re-slice the segment's runs to
		// [segOffset, len) after a partial consumption.
		runs := sliceInlineRuns(segment.runs, segOffset, len(segment.text))
		budget := request.wrapWidth
		if budget != nil {
			remaining_budget := *request.wrapWidth - currentRow().width
			if remaining_budget < 0 {
				remaining_budget = 0
			}
			budget = &remaining_budget
		}

		shaped, err := r.shapeRuns(remaining, segment.fontSize, runs, budget, nil)
		if err != nil {
			return inlineLayout{}, err
		}
		lineCount := shaped.lineCount()
		lineStart, lineEnd := shaped.lineRange(0)
		lineAdvance := shaped.lineAdvance(0)
		_ = lineStart

		if lineCount == 1 {
			// The whole remaining segment is one line: it either fits
			// the remaining budget, or the row is empty (first-word
			// overflow), or the row breaks and the segment retries at
			// the fresh budget.
			fits := budget == nil || lineAdvance <= *budget
			row := currentRow()
			if fits || row.width == 0 {
				pieceIndex := len(pieces)
				pieces = append(pieces, inlinePaintPiece{
					row:        len(rows) - 1,
					x:          row.width,
					advance:    lineAdvance,
					byteStart:  segment.byteStart + segOffset,
					byteEnd:    segment.byteStart + len(segment.text),
					shaped:     shaped,
					lineIndex:  0,
					lineHeight: segment.lineHeight,
				})
				row.pieceIndices = append(row.pieceIndices, pieceIndex)
				row.width += lineAdvance
				segOffset = len(segment.text)
				continue
			}
			startNewRow()
			continue
		}

		// The segment wraps at the budget: its first line becomes the
		// row's piece and the row ends (the remainder continues on a
		// fresh row).
		row := currentRow()
		pieceIndex := len(pieces)
		pieces = append(pieces, inlinePaintPiece{
			row:        len(rows) - 1,
			x:          row.width,
			advance:    lineAdvance,
			byteStart:  segment.byteStart + segOffset,
			byteEnd:    segment.byteStart + segOffset + (lineEnd - lineStart),
			shaped:     shaped,
			lineIndex:  0,
			lineHeight: segment.lineHeight,
		})
		row.pieceIndices = append(row.pieceIndices, pieceIndex)
		row.width += lineAdvance
		segOffset += lineEnd - lineStart
		startNewRow()
	}

	// Drop a trailing empty row (a wrap or break that never received
	// content).
	if n := len(rows); n > 0 {
		last := rows[n-1]
		if len(last.pieceIndices) == 0 && len(last.boxIndices) == 0 {
			rows = rows[:n-1]
		}
	}

	// Row clamping: rows beyond the budget are dropped with their
	// pieces and boxes (the bounded deviation from Parley's
	// hard-break-preserving clamp).
	if request.lineClamp != nil && int(*request.lineClamp) < len(rows) {
		limit := int(*request.lineClamp)
		kept := make([]*inlineAssemblyRow, limit)
		copy(kept, rows)
		rows = kept
		keptPieces := pieces[:0]
		for _, piece := range pieces {
			if piece.row < limit {
				keptPieces = append(keptPieces, piece)
			}
		}
		pieces = keptPieces
		keptBoxes := boxes[:0]
		for _, box := range boxes {
			if box.LineIndex < limit {
				keptBoxes = append(keptBoxes, box)
			}
		}
		boxes = keptBoxes
	}

	// The row geometry: alignment within the wrap width, the text
	// extents from the pieces' native metrics, then the shared vertical
	// alignment pass.
	lines := make([]InlineVisualLine, len(rows))
	lineMetrics := make([]InlineTextMetrics, len(rows))
	textBounds := make([][2]float32, len(rows))
	maxWidth := float32(0)
	for rowIndex, row := range rows {
		width := row.width
		switch request.textAlign {
		case TextAlignRight:
			if request.wrapWidth != nil {
				lines[rowIndex].Origin.X = *request.wrapWidth - width
			}
		case TextAlignCenter:
			if request.wrapWidth != nil {
				lines[rowIndex].Origin.X = (*request.wrapWidth - width) / 2
			}
		}
		lines[rowIndex].Size = Size{Width: width, Height: request.lineHeight}
		lines[rowIndex].Baseline = request.lineHeight

		top, bottom := baseInlineLineBounds(request.textMetrics, request.lineHeight)
		metrics := request.textMetrics
		for _, pieceIndex := range row.pieceIndices {
			piece := pieces[pieceIndex]
			ascent := piece.shaped.ascent()
			descent := piece.shaped.descent()
			halfLeading := maxZero32((piece.lineHeight - ascent - descent) / 2)
			top = min32(top, -ascent-halfLeading)
			bottom = max32(bottom, descent+halfLeading)
			metrics.Ascent = max32(metrics.Ascent, ascent)
			metrics.Descent = max32(metrics.Descent, descent)
		}
		textBounds[rowIndex] = [2]float32{top, bottom}
		lineMetrics[rowIndex] = metrics
		if extent := lines[rowIndex].Origin.X + width; extent > maxWidth {
			maxWidth = extent
		}
	}

	layout := inlineLayout{
		lines:           lines,
		boxes:           boxes,
		alignmentOffset: inlineAlignmentOffset(request.textAlign, lines),
		size:            Size{Width: maxWidth, Height: 0},
		pieces:          pieces,
	}
	if len(rows) == 0 {
		layout.geometry = func(start, end int) []inlineRangeGeometry { return nil }
		return layout, nil
	}

	// The row heights and the box placements are settled by the shared
	// vertical alignment pass.
	alignInlineBoxes(layout.lines, layout.boxes, &layout.size, request.boxes, lineMetrics, textBounds, request.textMetrics, request.lineHeight)

	// The byte-range geometry source: per row, the pieces' selection
	// rectangles (their x extents) and the boxes whose insertion index
	// falls inside the range (a selection covers the boxes between its
	// endpoints; prepare_layout cuts them back out per span).
	layout.geometry = func(start, end int) []inlineRangeGeometry {
		if start >= end {
			return nil
		}
		var regions []inlineRangeGeometry
		for _, piece := range layout.pieces {
			if piece.byteEnd <= start || piece.byteStart >= end {
				continue
			}
			localStart := piece.byteStart
			if start > localStart {
				localStart = start
			}
			localStart -= piece.byteStart
			localEnd := piece.byteEnd
			if end < localEnd {
				localEnd = end
			}
			localEnd -= piece.byteStart
			rects := piece.shaped.selectionRects(localStart, localEnd, piece.lineHeight)
			for _, rect := range rects {
				if rect.W <= 0 {
					continue
				}
				line := layout.lines[piece.row]
				regions = append(regions, inlineRangeGeometry{
					bounds: Bounds{
						Origin: Point{X: line.Origin.X + piece.x + rect.X, Y: line.Origin.Y},
						Size:   Size{Width: rect.W, Height: line.Size.Height},
					},
					visualLineIndex: piece.row,
				})
			}
		}
		for _, box := range layout.boxes {
			boxRequest := request.boxes[boxIndexByID(request.boxes, box.ID)]
			if boxRequest.Index < start || boxRequest.Index >= end {
				continue
			}
			regions = append(regions, inlineRangeGeometry{bounds: box.Bounds, visualLineIndex: box.LineIndex})
		}
		sort.SliceStable(regions, func(i, j int) bool {
			if regions[i].visualLineIndex != regions[j].visualLineIndex {
				return regions[i].visualLineIndex < regions[j].visualLineIndex
			}
			return regions[i].bounds.Origin.X < regions[j].bounds.Origin.X
		})
		return regions
	}
	return layout, nil
}

// shapeRuns shapes one segment with its covering runs at the
// segment's font size (the paragraph document seam: the native
// shaping request with explicit runs).
func (r *realWindowTextSystem) shapeRuns(text string, fontSize float32, runs []TextRun, wrapWidth *float32, lineClamp *uint32) (shapedText, error) {
	line, err := r.system.ShapeText(TextLayoutRequest{
		Text:      text,
		FontSize:  fontSize,
		Runs:      runs,
		WrapWidth: wrapWidth,
		LineClamp: lineClamp,
	})
	if err != nil {
		return nil, fmt.Errorf("shapeRuns (text %q len %d, runs %+v, size %v, wrap %v): %w", text, len(text), runs, fontSize, wrapWidth, err)
	}
	return &realShapedText{line: line, system: r.system, atlas: r.atlas}, nil
}

// boxIndexByID finds a box request's index by identifier.
func boxIndexByID(requests []InlineBoxRequest, id uint64) int {
	for i := range requests {
		if requests[i].ID == id {
			return i
		}
	}
	return 0
}

// splitInlineSegments splits the document into maximal text segments at
// the box insertion indices and the style-range boundaries (the child
// boundaries the collector produces), slicing the covering runs.
func splitInlineSegments(request inlineLayoutRequest) []inlineSegment {
	points := map[int]struct{}{0: {}, len(request.text): {}}
	for _, box := range request.boxes {
		if box.Index >= 0 && box.Index <= len(request.text) {
			points[box.Index] = struct{}{}
		}
	}
	for _, style := range request.textStyles {
		if style.Range.Start >= 0 && style.Range.Start <= len(request.text) {
			points[style.Range.Start] = struct{}{}
		}
		if style.Range.End >= 0 && style.Range.End <= len(request.text) {
			points[style.Range.End] = struct{}{}
		}
	}
	boundaries := make([]int, 0, len(points))
	for point := range points {
		boundaries = append(boundaries, point)
	}
	sort.Ints(boundaries)

	var segments []inlineSegment
	for i := 0; i+1 < len(boundaries); i++ {
		start, end := boundaries[i], boundaries[i+1]
		if end <= start {
			continue
		}
		fontSize, lineHeight := request.fontSize, request.lineHeight
		for _, style := range request.textStyles {
			if style.Range.Start <= start && end <= style.Range.End && style.Range.Start != style.Range.End {
				fontSize = style.FontSize
				lineHeight = style.LineHeight
				break
			}
		}
		segments = append(segments, inlineSegment{
			text:       request.text[start:end],
			byteStart:  start,
			runs:       sliceInlineRuns(request.runs, start, end),
			fontSize:   fontSize,
			lineHeight: lineHeight,
		})
	}
	return segments
}

// sliceInlineRuns slices the document's covering runs to a byte range,
// rebasing their lengths.
func sliceInlineRuns(runs []TextRun, start, end int) []TextRun {
	var sliced []TextRun
	cursor := 0
	for _, run := range runs {
		runStart, runEnd := cursor, cursor+run.Len
		cursor = runEnd
		if runEnd <= start || runStart >= end {
			if runStart >= end {
				break
			}
			continue
		}
		from := max32(0, float32(start-runStart))
		to := min32(float32(run.Len), float32(end-runStart))
		length := int(to - from)
		if length <= 0 {
			continue
		}
		sliced = append(sliced, TextRun{Len: length, Font: run.Font, LetterSpacing: run.LetterSpacing})
	}
	if len(sliced) == 0 && end > start {
		// Runs that fail to cover the range still shape it as one run
		// of the first font (defensive; the collector's runs cover the
		// document exactly).
		font := FontDescriptor{}
		if len(runs) > 0 {
			font = runs[0].Font
		}
		sliced = append(sliced, TextRun{Len: end - start, Font: font})
	}
	return sliced
}
