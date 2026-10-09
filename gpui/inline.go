package gpui

// This file is the port's inline layout layer (ticket15): the GPUI-owned
// orchestration that lays shaped text and embedded element boxes out in
// paragraphs, places their fragments, and drives painting, clipping and
// hit-testing from the same fragment geometry.
//
// The pinned reference is GPUI-CE 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a:
//
//   - crates/gpui/src/elements/div/inline.rs — InlineContent
//     (Text/Container/Atomic), the InlineParagraphCollector (text,
//     nested inline spans and atomic boxes collected into paragraphs,
//     block children finishing paragraphs), InlineDivFrameState
//     (request_layout / prepare_layout / prepaint_children /
//     paint_children) and merge_fragments;
//   - crates/gpui/src/text_system/line_layout.rs — InlineLayout,
//     InlineVisualLine, PositionedInlineBox, InlineRangeGeometry,
//     InlineTextMetrics, base_inline_line_bounds, expand_inline_line_for_box,
//     aligned_inline_box_y and align_inline_boxes (the CSS-like vertical
//     alignment, ported arithmetic-for-arithmetic);
//   - crates/gpui/src/text_system.rs — InlineBoxRequest, InlineTextStyle,
//     InlineLayoutRequest, and TextSystem::layout_inline delegating to the
//     platform text system;
//   - crates/gpui/src/elements/text.rs — TextLayout::layout publishing
//     InlineContent::Text and skipping its own prepaint/paint inside a
//     paragraph (window.current_inline_fragments);
//   - crates/gpui/src/window.rs — publish_inline_content,
//     inline_content, inline_fragments, place_inline,
//     layout_display_and_position and layout_vertical_align over the
//     engine's maps;
//   - crates/gpui/src/taffy.rs — place_inline (detached box placement
//     invalidating descendant caches) and the engine's inline maps;
//   - crates/gpui/src/elements/div.rs — the Block/Inline div adopting
//     InlineDivFrameState, publishing Container content, the
//     contents_in_parent_paragraph gate and the paint path;
//   - crates/gpui/src/element.rs — the Drawable scoping
//     window.current_inline_fragments around every element's
//     prepaint/paint.
//
// Go adaptation (honest, recorded): the pinned Parley backend performs
// box-aware shaping inside layout_inline (push_inline_box +
// break_lines). The port's native text ABI (ticket09) exposes shaping
// without boxes and cannot be extended without a DLL rebuild, so the
// Go seam owns the inline algorithm on every stack: the TEST text
// system ports the reference TestTextSystem::layout_inline exactly
// (platform.rs:1716 — deterministic), and the REAL text system
// composes the native shaping seam with a Go greedy row/box algorithm
// documented at realWindowTextSystem.layoutInline. Both produce the
// same InlineLayout contract: rows (InlineVisualLine), positioned
// boxes, a shared alignment anchor, the natural size, paintable text
// pieces per row, and byte-range geometry (the reference
// platform_layout.inline_geometry surface). Per-run paint styles
// (backgrounds, underline, strikethrough) are outside the port's run
// surface (the text seam carries fonts and letter spacing), so the
// paragraph paints foreground glyphs with the ambient text style
// color, like the port's text element; that bound is shared with
// ticket09/10 and recorded in the evidence.

import (
	"fmt"
	"sort"
)

// ---------------------------------------------------------------------------
// Inline content records (div/inline.rs InlineContent)
// ---------------------------------------------------------------------------

// inlineContentRecord is the inline content one element publishes for
// its layout node at request_layout (the reference InlineContent). The
// records are frame-scoped: they live in the window's layout engine
// until it resets (every frame).
type inlineContentRecord interface {
	// inlineKind names the record for diagnostics.
	inlineKind() string
}

// inlineTextContent is shaped text contributed to a paragraph (the
// reference InlineContent::Text: the text, its style runs, the resolved
// font size and line height).
type inlineTextContent struct {
	text       string
	runs       []TextRun
	fontSize   float32
	lineHeight float32
}

// inlineKind implements inlineContentRecord.
func (c *inlineTextContent) inlineKind() string { return "text" }

// inlineContainerContent marks a node whose children participate in
// inline collection (the reference InlineContent::Container).
type inlineContainerContent struct {
	children []LayoutID
}

// inlineKind implements inlineContentRecord.
func (c *inlineContainerContent) inlineKind() string { return "container" }

// inlineAtomicContent marks content with its own interaction and text
// layout (the reference InlineContent::Atomic, InteractiveText): the
// element lays out independently instead of contributing to a
// paragraph's document.
type inlineAtomicContent struct{}

// inlineKind implements inlineContentRecord.
func (c *inlineAtomicContent) inlineKind() string { return "atomic" }

// publishInlineContent records the inline content of a requested node
// on the window's layout engine (the reference
// window.publish_inline_content). Returns false when no engine is
// available (never in this port's draw path).
func publishInlineContent(w *Window, id LayoutID, record inlineContentRecord) bool {
	ds := drawState(w)
	if err := ds.engine.PublishInlineContent(id, record); err != nil {
		panic(fmt.Sprintf("gpui: publishing inline content: %v", err))
	}
	return true
}

// inlineContentOf returns the node's published inline content.
func inlineContentOf(w *Window, id LayoutID) inlineContentRecord {
	record, _ := drawState(w).engine.InlineContent(id)
	return record
}

// layoutDisplayAndPositionOf returns the node's recorded display and
// position (the reference window.layout_display_and_position). Absent
// records answer the Flex/Relative default.
func layoutDisplayAndPositionOf(w *Window, id LayoutID) (Display, Position) {
	display, position, _ := drawState(w).engine.DisplayPositionOf(id)
	return display, position
}

// layoutVerticalAlignOf returns the node's recorded vertical alignment
// (the reference window.layout_vertical_align; Baseline when
// unrecorded).
func layoutVerticalAlignOf(w *Window, id LayoutID) VerticalAlign {
	align, _ := drawState(w).engine.VerticalAlignOf(id)
	return align
}

// inlineFragmentsOf returns the node's placed fragment regions in
// window-local logical pixels (the reference window.inline_fragments:
// the stored regions shifted by the pixel-snapped element offset). A
// nil answer means the node was never placed; a non-nil empty slice
// means it was placed with no drawable regions (still in a paragraph).
func inlineFragmentsOf(w *Window, id LayoutID) []Bounds {
	frame := currentFrame(w)
	fragments, ok := drawState(w).engine.InlineFragments(id, frame.scale, frame.elementOffset)
	if !ok {
		return nil
	}
	return fragments
}

// placeInlineAt places a detached inline box or a span's union bounds
// with its fragment regions (the reference window.place_inline): the
// pixel-snapped element offset is subtracted from every origin before
// the engine records the placement.
func placeInlineAt(w *Window, id LayoutID, bounds Bounds, fragments []Bounds) {
	frame := currentFrame(w)
	offset := pixelSnapPoint(frame.elementOffset, frame.scale)
	shifted := Bounds{
		Origin: Point{X: bounds.Origin.X - offset.X, Y: bounds.Origin.Y - offset.Y},
		Size:   bounds.Size,
	}
	var stored []Bounds
	if fragments != nil {
		stored = make([]Bounds, len(fragments))
		for i, region := range fragments {
			stored[i] = Bounds{
				Origin: Point{X: region.Origin.X - offset.X, Y: region.Origin.Y - offset.Y},
				Size:   region.Size,
			}
		}
	}
	if err := drawState(w).engine.PlaceInline(id, shifted, stored, frame.scale); err != nil {
		panic(fmt.Sprintf("gpui: placing inline content: %v", err))
	}
}

// ---------------------------------------------------------------------------
// The inline layout contract (text_system.rs / line_layout.rs types)
// ---------------------------------------------------------------------------

// InlineTextMetrics are the font metrics that construct the vertical
// extents of an inline line (the reference InlineTextMetrics).
type InlineTextMetrics struct {
	// Ascent is the distance above the text baseline.
	Ascent float32
	// Descent is the distance below the text baseline.
	Descent float32
	// XHeight is the lowercase-x height of the container's font.
	XHeight float32
}

// InlineTextStyle is the resolved font size and line height for one
// byte range of an inline document (the reference InlineTextStyle).
type InlineTextStyle struct {
	// Range is the UTF-8 byte range receiving the metrics.
	Range TextRange
	// FontSize is the range's font size.
	FontSize float32
	// LineHeight is the range's absolute line height.
	LineHeight float32
}

// InlineBoxRequest is one atomic element box inserted at a UTF-8
// boundary (the reference InlineBoxRequest).
type InlineBoxRequest struct {
	// ID is the caller-provided identifier returned with the positioned
	// box (the document's box index).
	ID uint64
	// Index is the UTF-8 byte index at which the box is inserted.
	Index int
	// Size is the measured size of the element.
	Size Size
	// VerticalAlign is the alignment within the line containing the
	// box.
	VerticalAlign VerticalAlign
}

// InlineVisualLine is one row of an inline layout (the reference
// InlineVisualLine: the row origin relative to the layout, the row size
// before placement, and the baseline relative to origin.y).
type InlineVisualLine struct {
	// Origin is the row origin relative to the inline layout.
	Origin Point
	// Size is the row size before it is placed in its container.
	Size Size
	// Baseline is the baseline offset from Origin.Y.
	Baseline float32
}

// PositionedInlineBox is the position assigned to an embedded element
// (the reference PositionedInlineBox).
type PositionedInlineBox struct {
	// ID is the caller-provided box identifier.
	ID uint64
	// LineIndex is the visual line containing the box.
	LineIndex int
	// Bounds are the box bounds relative to the complete inline layout.
	Bounds Bounds
}

// inlineRangeGeometry is the geometry occupied by a text range on one
// visual line (the reference InlineRangeGeometry).
type inlineRangeGeometry struct {
	// Bounds are relative to the complete text layout.
	bounds Bounds
	// visualLineIndex is the row containing the bounds.
	visualLineIndex int
}

// inlineLayoutRequest is one inline formatting context request (the
// reference InlineLayoutRequest).
type inlineLayoutRequest struct {
	// text is the UTF-8 source.
	text string
	// runs are the style runs covering text exactly.
	runs []TextRun
	// textStyles are the resolved font sizes and line heights.
	textStyles []InlineTextStyle
	// boxes are the atomic boxes inserted into the text.
	boxes []InlineBoxRequest
	// fontSize is the base font size.
	fontSize float32
	// lineHeight is the requested height of an ordinary text row.
	lineHeight float32
	// textMetrics are the container base font's metrics.
	textMetrics InlineTextMetrics
	// wrapWidth is the optional soft-wrap width.
	wrapWidth *float32
	// lineClamp is the optional maximum row count.
	lineClamp *uint32
	// textAlign is the horizontal alignment within wrapWidth.
	textAlign TextAlign
}

// inlinePaintPiece is one paintable text piece placed on a row (the Go
// shape of the reference LineLayout paint fragments: a piece is one
// visual line of one shaped text, positioned on its row).
type inlinePaintPiece struct {
	// row is the piece's row index.
	row int
	// x is the piece's row-relative x origin.
	x float32
	// advance is the piece's shaped advance on the row.
	advance float32
	// byteStart and byteEnd are the piece's document byte range.
	byteStart, byteEnd int
	// shaped is the piece's shaped document; its line lineIndex holds
	// the piece's glyphs and fragment geometry.
	shaped shapedText
	// lineIndex is the piece's line within shaped.
	lineIndex int
	// lineHeight is the piece's resolved line height (its style range's
	// line height, for its half-leading).
	lineHeight float32
}

// inlineLayout is the seam result (the reference InlineLayout): the
// rows, the positioned boxes, the shared alignment anchor, the natural
// size, the paintable pieces and the byte-range geometry source.
type inlineLayout struct {
	// lines are the rows in paint order.
	lines []InlineVisualLine
	// boxes are the positioned element boxes.
	boxes []PositionedInlineBox
	// alignmentOffset is the shared horizontal anchor aligning every
	// row (inline_alignment_offset).
	alignmentOffset float32
	// size is the natural size of the complete layout.
	size Size
	// pieces are the paintable text pieces per row.
	pieces []inlinePaintPiece
	// geometry answers the native geometry of a byte range (the
	// reference platform_layout.inline_geometry; empty ranges answer
	// none, like the pin's None).
	geometry func(start, end int) []inlineRangeGeometry
}

// ---------------------------------------------------------------------------
// Vertical alignment (line_layout.rs base_inline_line_bounds /
// expand_inline_line_for_box / aligned_inline_box_y / align_inline_boxes)
// ---------------------------------------------------------------------------

// baseInlineLineBounds returns the text-bounds pair of one row before
// boxes expand it (the pinned base_inline_line_bounds: the half leading
// is ((line_height - ascent - descent) / 2).max(0); the pair is
// (-ascent - half_leading, descent + half_leading) around the
// baseline).
func baseInlineLineBounds(metrics InlineTextMetrics, lineHeight float32) (top, bottom float32) {
	halfLeading := maxZero32((lineHeight - metrics.Ascent - metrics.Descent) / 2)
	return -metrics.Ascent - halfLeading, metrics.Descent + halfLeading
}

// expandInlineLineForBox expands one row's vertical extents for a box
// (the pinned expand_inline_line_for_box): baseline-aligned boxes only
// extend upward (their bottom sits on the baseline), middle-aligned
// boxes center on the x-height midpoint, top- and bottom-aligned boxes
// grow the dedicated top/bottom box heights settled after the pass.
func expandInlineLineForBox(top, bottom, topBoxHeight, bottomBoxHeight *float32, height float32, metrics InlineTextMetrics, align VerticalAlign) {
	switch align {
	case VerticalAlignBaseline:
		*top = min32(*top, -height)
	case VerticalAlignMiddle:
		middle := -metrics.XHeight / 2
		*top = min32(*top, middle-height/2)
		*bottom = max32(*bottom, middle+height/2)
	case VerticalAlignTop:
		*topBoxHeight = max32(*topBoxHeight, height)
	case VerticalAlignBottom:
		*bottomBoxHeight = max32(*bottomBoxHeight, height)
	}
}

// alignedInlineBoxY returns a box's y offset within its row (the pinned
// aligned_inline_box_y).
func alignedInlineBoxY(line InlineVisualLine, metrics InlineTextMetrics, height float32, align VerticalAlign) float32 {
	switch align {
	case VerticalAlignBaseline:
		return line.Baseline - height
	case VerticalAlignMiddle:
		return line.Baseline - metrics.XHeight/2 - height/2
	case VerticalAlignTop:
		return 0
	default: // VerticalAlignBottom
		return line.Size.Height - height
	}
}

// alignInlineBoxes applies the CSS-like vertical alignment to boxes
// after the rows assign them (the pinned align_inline_boxes, ported
// operation for operation): every row's extents start from its text
// bounds (or the base bounds), expand for its boxes, settle the
// top/bottom box heights, then receive its final origin.y, height and
// baseline, and its boxes receive their aligned y positions. The
// document height becomes the accumulated row heights.
func alignInlineBoxes(lines []InlineVisualLine, boxes []PositionedInlineBox, size *Size, requests []InlineBoxRequest, lineMetrics []InlineTextMetrics, textBounds [][2]float32, fallbackMetrics InlineTextMetrics, lineHeight float32) {
	placements := make([]struct {
		lineIndex int
		placed    bool
		align     VerticalAlign
	}, len(boxes))
	for i := range boxes {
		verticalAlign := VerticalAlignBaseline
		for _, request := range requests {
			if request.ID == boxes[i].ID {
				verticalAlign = request.VerticalAlign
				break
			}
		}
		placements[i].placed = boxes[i].LineIndex < len(lines)
		placements[i].lineIndex = boxes[i].LineIndex
		placements[i].align = verticalAlign
	}
	lineY := float32(0)
	for lineIndex := range lines {
		line := &lines[lineIndex]
		metrics := fallbackMetrics
		if lineIndex < len(lineMetrics) {
			metrics = lineMetrics[lineIndex]
		}
		top, bottom := baseInlineLineBounds(metrics, lineHeight)
		if lineIndex < len(textBounds) {
			top, bottom = textBounds[lineIndex][0], textBounds[lineIndex][1]
		}
		topBoxHeight := float32(0)
		bottomBoxHeight := float32(0)
		for i := range boxes {
			if !placements[i].placed || placements[i].lineIndex != lineIndex {
				continue
			}
			expandInlineLineForBox(&top, &bottom, &topBoxHeight, &bottomBoxHeight, boxes[i].Bounds.Size.Height, metrics, placements[i].align)
		}
		bottom = max32(bottom, top+topBoxHeight)
		top = min32(top, bottom-bottomBoxHeight)
		line.Origin.Y = lineY
		line.Size.Height = bottom - top
		line.Baseline = -top
		for i := range boxes {
			if !placements[i].placed || placements[i].lineIndex != lineIndex {
				continue
			}
			boxes[i].Bounds.Origin.Y = lineY + alignedInlineBoxY(*line, metrics, boxes[i].Bounds.Size.Height, placements[i].align)
		}
		lineY += line.Size.Height
	}
	size.Height = lineY
}

// inlineAlignmentOffset returns the shared horizontal anchor of the
// layout (the pinned inline_alignment_offset: the FIRST row's center
// or right edge; Left anchors at zero).
func inlineAlignmentOffset(textAlign TextAlign, lines []InlineVisualLine) float32 {
	if len(lines) == 0 {
		return 0
	}
	line := lines[0]
	switch textAlign {
	case TextAlignCenter:
		return line.Origin.X + line.Size.Width/2
	case TextAlignRight:
		return line.Origin.X + line.Size.Width
	default:
		return 0
	}
}

// ---------------------------------------------------------------------------
// The inline paragraph document (div/inline.rs InlineDocument /
// InlineSpan / InlineParagraph / InlineDivFrameState)
// ---------------------------------------------------------------------------

// inlineSpan is one element's byte-range participation in a paragraph
// (the reference InlineSpan).
type inlineSpan struct {
	layoutID LayoutID
	// textRange is the span's text range in the document.
	textRange TextRange
	// boxRange is the span's box range in the document.
	boxStart, boxEnd int
}

// inlineDocument is one paragraph's collected content (the reference
// InlineDocument).
type inlineDocument struct {
	text         string
	runs         []TextRun
	textStyles   []InlineTextStyle
	boxes        []InlineBoxRequest
	boxLayoutIDs []LayoutID
	spans        []inlineSpan
}

// inlineParagraph is one measured paragraph (the reference
// InlineParagraph): the measured layout node, the document, the
// measurement cache (keyed by wrap width) and the paint origin.
type inlineParagraph struct {
	layoutID LayoutID
	document *inlineDocument
	// measurement is the cached (wrapWidth, layout) pair; nil before
	// the first measure.
	measurement *inlineParagraphMeasurement
	// paintOrigin is committed by prepaintChildren.
	paintOrigin Point
}

// inlineParagraphMeasurement is the paragraph's cached layout under
// one wrap width (the reference InlineParagraphMeasurement).
type inlineParagraphMeasurement struct {
	wrapWidth *float32
	layout    *inlineLayout
}

// inlineDivFrameState is a Block/Inline div's inline frame state (the
// reference InlineDivFrameState).
type inlineDivFrameState struct {
	// paragraphs are the collected paragraphs in document order. The
	// slice holds pointers: the measure closure mutates the paragraph's
	// cached measurement while the engine computes, after the paragraph
	// was collected (the reference shares the RefCell<Option<..>>
	// measurement the same way).
	paragraphs []*inlineParagraph
	// spanLayoutIDs are the text and inline-container nodes whose
	// bounds come from paragraph fragments.
	spanLayoutIDs []LayoutID
	// flowChildLayoutIDs are the anonymous paragraph nodes and separate
	// children, including absolute children.
	flowChildLayoutIDs []LayoutID
}

// inlineParagraphCollector collects text, nested inline spans and
// atomic boxes into paragraphs (the reference InlineParagraphCollector).
type inlineParagraphCollector struct {
	frameState        *inlineDivFrameState
	currentDocument   *inlineDocument
	openSpanLayoutIDs []LayoutID
	textStyle         TextStyle
	window            *Window
}

// collectElement classifies one child node (the reference
// collect_element): display-none nodes are skipped, absolute nodes stay
// separate flow children, published text joins the current document,
// inline containers recurse into their children, other non-inline
// nodes finish the paragraph and stay separate, and inline/inline-flex
// nodes are measured as atomic boxes at their text position.
func (c *inlineParagraphCollector) collectElement(layoutID LayoutID) {
	display, position := layoutDisplayAndPositionOf(c.window, layoutID)
	if display == DisplayNone {
		return
	}
	if position == PositionAbsolute {
		c.frameState.flowChildLayoutIDs = append(c.frameState.flowChildLayoutIDs, layoutID)
		return
	}

	content := inlineContentOf(c.window, layoutID)
	// The reference match order: published text joins the document
	// first; a Container with Inline display recurses as a span; every
	// other case falls to the shared tail — a node that is neither
	// Inline nor InlineFlex finishes the paragraph and stays a separate
	// flow child (an InlineFlex Container included falls through to the
	// atomic-box arm), and the remaining Inline/InlineFlex nodes are
	// measured as atomic boxes at their text position.
	if record, ok := content.(*inlineTextContent); ok {
		c.frameState.spanLayoutIDs = append(c.frameState.spanLayoutIDs, layoutID)
		textStart := len(c.currentDocument.text)
		boxStart := len(c.currentDocument.boxes)
		c.currentDocument.text += record.text
		c.currentDocument.runs = append(c.currentDocument.runs, record.runs...)
		c.currentDocument.textStyles = append(c.currentDocument.textStyles, InlineTextStyle{
			Range:      TextRange{Start: textStart, End: len(c.currentDocument.text)},
			FontSize:   record.fontSize,
			LineHeight: record.lineHeight,
		})
		c.recordSpanRanges(layoutID, textStart, boxStart)
		c.recordOpenSpanRanges(textStart, boxStart)
	} else if record, ok := content.(*inlineContainerContent); ok && display == DisplayInline {
		c.frameState.spanLayoutIDs = append(c.frameState.spanLayoutIDs, layoutID)
		c.openSpanLayoutIDs = append(c.openSpanLayoutIDs, layoutID)
		for _, child := range record.children {
			c.collectElement(child)
		}
		c.openSpanLayoutIDs = c.openSpanLayoutIDs[:len(c.openSpanLayoutIDs)-1]
	} else if display != DisplayInline && display != DisplayInlineFlex {
		c.finishParagraphFor(layoutID)
	} else {
		// Measure atomic contents before any layout compute enters its
		// measure callbacks (the reference computes the subtree
		// standalone under max-content space and reads its bounds; the
		// port engine is idle during request_layout, so the same
		// standalone compute is safe here).
		available := AvailableSize{
			Width:  MaxContentAvailableSpace(),
			Height: MaxContentAvailableSpace(),
		}
		if err := computeLayoutOf(c.window, layoutID, available); err != nil {
			panic(fmt.Sprintf("gpui: inline atomic measurement: %v", err))
		}
		bounds, err := layoutBoundsOf(c.window, layoutID)
		if err != nil {
			panic(fmt.Sprintf("gpui: inline atomic bounds: %v", err))
		}
		boxStart := len(c.currentDocument.boxes)
		textStart := len(c.currentDocument.text)
		c.currentDocument.boxes = append(c.currentDocument.boxes, InlineBoxRequest{
			ID:            uint64(boxStart),
			Index:         textStart,
			Size:          bounds.Size,
			VerticalAlign: layoutVerticalAlignOf(c.window, layoutID),
		})
		c.currentDocument.boxLayoutIDs = append(c.currentDocument.boxLayoutIDs, layoutID)
		c.recordOpenSpanRanges(textStart, boxStart)
	}
}

// finishParagraphFor finishes the current paragraph for a non-inline
// child and records the child as a separate flow child (the reference
// match arm).
func (c *inlineParagraphCollector) finishParagraphFor(layoutID LayoutID) {
	c.finishParagraph()
	c.frameState.flowChildLayoutIDs = append(c.frameState.flowChildLayoutIDs, layoutID)
}

// recordOpenSpanRanges extends every open span's ranges to the current
// document position (the reference record_open_span_ranges).
func (c *inlineParagraphCollector) recordOpenSpanRanges(textStart, boxStart int) {
	for i := range c.openSpanLayoutIDs {
		c.recordSpanRanges(c.openSpanLayoutIDs[i], textStart, boxStart)
	}
}

// recordSpanRanges records or extends one span's text and box ranges
// (the reference record_span_ranges).
func (c *inlineParagraphCollector) recordSpanRanges(layoutID LayoutID, textStart, boxStart int) {
	textEnd := len(c.currentDocument.text)
	boxEnd := len(c.currentDocument.boxes)
	for i := range c.currentDocument.spans {
		span := &c.currentDocument.spans[i]
		if span.layoutID == layoutID {
			span.textRange.End = textEnd
			span.boxEnd = boxEnd
			return
		}
	}
	c.currentDocument.spans = append(c.currentDocument.spans, inlineSpan{
		layoutID:  layoutID,
		textRange: TextRange{Start: textStart, End: textEnd},
		boxStart:  boxStart,
		boxEnd:    boxEnd,
	})
}

// finishParagraph closes the current document into a measured
// paragraph node (the reference finish_paragraph): the ambient text
// style resolves the paragraph font size, line height and base font
// metrics, and the measure closure lays the document out through the
// window text system's inline layout under the evaluated wrap width,
// caching the layout per wrap width.
func (c *inlineParagraphCollector) finishParagraph() {
	document := c.currentDocument
	if len(document.text) == 0 && len(document.boxes) == 0 {
		return
	}

	textStyle := c.textStyle
	rem := RemSize(c.window)
	fontSize := textStyle.FontSizePixels(rem)
	lineHeight := PixelSnap(c.window, textStyle.LineHeightPixels(fontSize, rem))
	textMetrics := frameTextSystem(c.window).inlineMetrics(textStyle.FontDescriptorOf(), fontSize)

	measuredDocument := document

	paragraph := &inlineParagraph{document: document}

	paragraphStyle := DefaultStyle()
	// The pinned paragraph node style: Style { display: Block, ..Default }.
	paragraphStyle.Display = DisplayBlock
	layoutID, err := requestMeasuredLayoutOf(c.window, paragraphStyle, func(req MeasureRequest) Size {
		// The pinned wrap evaluation: Nowrap never wraps; Normal wraps
		// at the known width, else the definite available width, else
		// not at all (content-based measurement).
		var wrapWidth *float32
		if textStyle.WhiteSpace == WhiteSpaceNormal {
			if req.KnownWidthPresent {
				width := req.KnownWidth
				wrapWidth = &width
			} else if req.AvailWidth.Kind == AvailDefinite {
				width := req.AvailWidth.Definite
				wrapWidth = &width
			}
		}
		if paragraph.measurement != nil && wrapWidthsEqual(paragraph.measurement.wrapWidth, wrapWidth) {
			return paragraph.measurement.layout.size
		}
		layout, err := frameTextSystem(c.window).layoutInline(inlineLayoutRequest{
			text:        measuredDocument.text,
			runs:        measuredDocument.runs,
			textStyles:  measuredDocument.textStyles,
			boxes:       measuredDocument.boxes,
			fontSize:    fontSize,
			lineHeight:  lineHeight,
			textMetrics: textMetrics,
			wrapWidth:   wrapWidth,
			lineClamp:   nil,
			textAlign:   textStyle.TextAlign,
		})
		if err != nil {
			panic(fmt.Sprintf("gpui: inline paragraph layout: %v", err))
		}
		paragraph.measurement = &inlineParagraphMeasurement{wrapWidth: wrapWidth, layout: &layout}
		return layout.size
	})
	if err != nil {
		panic(fmt.Sprintf("gpui: inline paragraph request_layout: %v", err))
	}
	paragraph.layoutID = layoutID

	c.frameState.flowChildLayoutIDs = append(c.frameState.flowChildLayoutIDs, layoutID)
	c.frameState.paragraphs = append(c.frameState.paragraphs, paragraph)
	c.currentDocument = &inlineDocument{}
}

// wrapWidthsEqual compares two optional wrap widths (pointer-nil aware;
// equal finite values compare equal).
func wrapWidthsEqual(a, b *float32) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// requestInlineDivFrame is InlineDivFrameState::request_layout: collect
// the children into paragraphs, then request this element's layout node
// over the flow children.
func requestInlineDivFrame(w *Window, style Style, children []LayoutID, app *App) (LayoutID, *inlineDivFrameState) {
	collector := &inlineParagraphCollector{
		frameState:      &inlineDivFrameState{},
		currentDocument: &inlineDocument{},
		textStyle:       WindowTextStyle(w),
		window:          w,
	}
	for _, child := range children {
		collector.collectElement(child)
	}
	collector.finishParagraph()

	layoutID, err := requestLayoutOf(w, style, collector.frameState.flowChildLayoutIDs...)
	if err != nil {
		panic(fmt.Sprintf("gpui: inline div request_layout: %v", err))
	}
	return layoutID, collector.frameState
}

// prepareLayout places the paragraphs' boxes and span fragments
// (the reference InlineDivFrameState::prepare_layout): every paragraph
// with a measurement anchors at its computed layout origin (snapped
// through the alignment anchor), its boxes are placed through
// place_inline with pixel-snapped origins, and every span's fragment
// regions — the byte-range geometry minus the row's boxes, plus the
// span's own boxes — are snapped corner-wise, merged per row, and
// placed with their union. Returns the flow children's content size.
func (f *inlineDivFrameState) prepareLayout(bounds Bounds, children []LayoutID, w *Window) Size {
	fragments := make(map[LayoutID][]Bounds, len(f.spanLayoutIDs))
	for _, id := range f.spanLayoutIDs {
		fragments[id] = nil
	}

	for _, paragraph := range f.paragraphs {
		if paragraph.measurement == nil {
			continue
		}
		layout := paragraph.measurement.layout
		origin, err := layoutBoundsOf(w, paragraph.layoutID)
		if err != nil {
			panic(fmt.Sprintf("gpui: inline paragraph bounds: %v", err))
		}
		placement := placeInlineLayout(origin.Origin, layout.alignmentOffset, w)
		paragraphOrigin := Point{X: origin.Origin.X + placement.delta.X, Y: origin.Origin.Y + placement.delta.Y}

		for _, inlineBox := range layout.boxes {
			boxID := int(inlineBox.ID)
			if boxID < 0 || boxID >= len(paragraph.document.boxLayoutIDs) {
				continue
			}
			boxOrigin := Point{
				X: paragraphOrigin.X + inlineBox.Bounds.Origin.X,
				Y: paragraphOrigin.Y + inlineBox.Bounds.Origin.Y,
			}
			placeInlineAt(w, paragraph.document.boxLayoutIDs[boxID], Bounds{
				Origin: pixelSnapPoint(boxOrigin, currentFrame(w).scale),
				Size:   inlineBox.Bounds.Size,
			}, nil)
		}

		for _, span := range paragraph.document.spans {
			regions := fragments[span.layoutID]
			geometry := layout.geometry(span.textRange.Start, span.textRange.End)
			for _, region := range geometry {
				if region.visualLineIndex < 0 || region.visualLineIndex >= len(layout.lines) {
					continue
				}
				line := layout.lines[region.visualLineIndex]
				// Selection geometry can include boxes attached to a
				// neighboring cluster: remove every box of the row
				// first, then add back exactly the boxes this span owns.
				xRanges := [][2]float32{{region.bounds.Origin.X, region.bounds.Right()}}
				for _, inlineBox := range layout.boxes {
					if inlineBox.LineIndex != region.visualLineIndex {
						continue
					}
					left := inlineBox.Bounds.Origin.X
					right := inlineBox.Bounds.Right()
					var split [][2]float32
					for _, xRange := range xRanges {
						if xRange[0] < left {
							end := min32(xRange[1], left)
							if end > xRange[0] {
								split = append(split, [2]float32{xRange[0], end})
							}
						}
						if xRange[1] > right {
							start := max32(xRange[0], right)
							if xRange[1] > start {
								split = append(split, [2]float32{start, xRange[1]})
							}
						}
					}
					xRanges = split
				}
				for _, xRange := range xRanges {
					if xRange[1] <= xRange[0] {
						continue
					}
					regions = append(regions, Bounds{
						Origin: Point{
							X: paragraphOrigin.X + xRange[0],
							Y: paragraphOrigin.Y + line.Origin.Y,
						},
						Size: Size{
							Width:  xRange[1] - xRange[0],
							Height: line.Size.Height,
						},
					})
				}
			}
			for _, inlineBox := range layout.boxes {
				if span.boxStart <= int(inlineBox.ID) && int(inlineBox.ID) < span.boxEnd {
					regions = append(regions, Bounds{
						Origin: Point{
							X: paragraphOrigin.X + inlineBox.Bounds.Origin.X,
							Y: paragraphOrigin.Y + inlineBox.Bounds.Origin.Y,
						},
						Size: inlineBox.Bounds.Size,
					})
				}
			}
			fragments[span.layoutID] = regions
		}
	}

	for id, regions := range fragments {
		for i := range regions {
			scale := currentFrame(w).scale
			regions[i] = boundsFromCorners(
				pixelSnapPoint(regions[i].Origin, scale),
				pixelSnapPoint(Point{X: regions[i].Right(), Y: regions[i].Bottom()}, scale),
			)
		}
		mergeFragments(regions)

		union := Bounds{Origin: bounds.Origin, Size: Size{}}
		for _, region := range regions {
			union = unionBounds(union, region)
		}
		// A placed span with no drawable regions still records the
		// placement (a non-nil empty fragment list), so its children
		// know they live inside a paragraph.
		placed := regions
		if placed == nil {
			placed = []Bounds{}
		}
		placeInlineAt(w, id, union, placed)
	}

	var contentSize Size
	for _, id := range f.flowChildLayoutIDs {
		childBounds, err := layoutBoundsOf(w, id)
		if err != nil {
			panic(fmt.Sprintf("gpui: inline flow child bounds: %v", err))
		}
		contentSize = unionSize(contentSize, childBounds.Size)
	}
	return contentSize
}

// prepaintChildren prepaints the children of an inline container (the
// reference InlineDivFrameState::prepaint_children): each paragraph
// records its paint origin from its computed bounds, then the children
// prepaint in order under the element offset (zero in this slice —
// scrolling is ticket14).
func (f *inlineDivFrameState) prepaintChildren(children []AnyElement, w *Window, app *App) {
	for _, paragraph := range f.paragraphs {
		bounds, err := layoutBoundsOf(w, paragraph.layoutID)
		if err != nil {
			panic(fmt.Sprintf("gpui: inline paragraph prepaint bounds: %v", err))
		}
		paragraph.paintOrigin = bounds.Origin
	}
	for i := range children {
		children[i].Prepaint(w, app)
	}
}

// paintChildren paints the children then the paragraphs (the reference
// InlineDivFrameState::paint_children): the children's text paints
// nothing (their fragments are placed), then every measured paragraph
// paints its inline layout at its committed origin with the ambient
// text style color.
func (f *inlineDivFrameState) paintChildren(children []AnyElement, w *Window, app *App) {
	for i := range children {
		children[i].Paint(w, app)
	}
	color := WindowTextStyle(w).Color
	for _, paragraph := range f.paragraphs {
		if paragraph.measurement == nil {
			continue
		}
		if err := paintInlineLayout(w, paragraph.measurement.layout, paragraph.paintOrigin, color); err != nil {
			panic(fmt.Sprintf("gpui: inline paragraph paint: %v", err))
		}
		recordInlineFacts(w, paragraph)
	}
}

// mergeFragments merges horizontally-adjacent regions of the same row
// (the reference merge_fragments: sort by (y, x); a region joins the
// previous one when it shares y and height and starts at or before its
// right edge).
func mergeFragments(regions []Bounds) {
	if len(regions) < 2 {
		return
	}
	sort.SliceStable(regions, func(i, j int) bool {
		if regions[i].Origin.Y != regions[j].Origin.Y {
			return regions[i].Origin.Y < regions[j].Origin.Y
		}
		return regions[i].Origin.X < regions[j].Origin.X
	})
	merged := make([]Bounds, 0, len(regions))
	for _, region := range regions {
		if n := len(merged); n > 0 &&
			merged[n-1].Origin.Y == region.Origin.Y &&
			merged[n-1].Size.Height == region.Size.Height &&
			region.Origin.X <= merged[n-1].Right() {
			merged[n-1] = unionBounds(merged[n-1], region)
		} else {
			merged = append(merged, region)
		}
	}
	copy(regions, merged)
}

// inlineLayoutPlacement is the snapped anchor placement of one inline
// layout (the reference InlineLayoutPlacement).
type inlineLayoutPlacement struct {
	origin Point
	delta  Point
}

// placeInlineLayout snaps the layout's alignment anchor to the device
// grid (the pinned place_inline_layout: the anchor is the content
// origin plus (alignment_offset, 0); the placed anchor is
// pixel_snap_point(anchor); the delta snaps every row of the layout
// with it).
func placeInlineLayout(contentOrigin Point, alignmentOffset float32, w *Window) inlineLayoutPlacement {
	anchor := Point{X: contentOrigin.X + alignmentOffset, Y: contentOrigin.Y}
	placedAnchor := pixelSnapPoint(anchor, currentFrame(w).scale)
	return inlineLayoutPlacement{
		origin: Point{X: contentOrigin.X + (placedAnchor.X - anchor.X), Y: contentOrigin.Y + (placedAnchor.Y - anchor.Y)},
		delta:  Point{X: placedAnchor.X - anchor.X, Y: placedAnchor.Y - anchor.Y},
	}
}

// paintInlineLayout paints one paragraph's inline layout at its origin
// (the pinned paint_inline_layout): the whole layout is wrapped in one
// paint layer of its natural size placed at the snapped anchor, and
// every row's pieces paint at their row origins with the row baseline.
func paintInlineLayout(w *Window, layout *inlineLayout, origin Point, color Hsla) error {
	if len(layout.lines) == 0 {
		return nil
	}
	frame := currentFrame(w)
	placement := placeInlineLayout(origin, layout.alignmentOffset, w)
	layerBounds := Bounds{Origin: placement.origin, Size: layout.size}
	if err := frame.scene.BeginLayer(layerBounds, frame.paint); err != nil {
		return err
	}
	for _, piece := range layout.pieces {
		if piece.row < 0 || piece.row >= len(layout.lines) {
			continue
		}
		line := layout.lines[piece.row]
		lineOrigin := Point{
			X: origin.X + line.Origin.X + placement.delta.X + piece.x,
			Y: origin.Y + line.Origin.Y + placement.delta.Y,
		}
		baselineY := lineOrigin.Y + line.Baseline
		if err := piece.shaped.paintLine(w, piece.lineIndex, lineOrigin, baselineY, color); err != nil {
			_ = frame.scene.EndLayer()
			return err
		}
	}
	return frame.scene.EndLayer()
}

// ---------------------------------------------------------------------------
// Shared bounds helpers (Bounds union / corners)
// ---------------------------------------------------------------------------

// unionBounds returns the smallest bounds covering a and b.
func unionBounds(a, b Bounds) Bounds {
	if isEmptyBounds(a) {
		return b
	}
	if isEmptyBounds(b) {
		return a
	}
	left := min32(a.Origin.X, b.Origin.X)
	top := min32(a.Origin.Y, b.Origin.Y)
	right := max32(a.Right(), b.Right())
	bottom := max32(a.Bottom(), b.Bottom())
	return Bounds{Origin: Point{X: left, Y: top}, Size: Size{Width: right - left, Height: bottom - top}}
}

// unionSize returns the component-wise maximum extent.
func unionSize(a, b Size) Size {
	return Size{Width: max32(a.Width, b.Width), Height: max32(a.Height, b.Height)}
}

// boundsFromCorners builds bounds from its near and far corners
// (Bounds::from_corners).
func boundsFromCorners(near, far Point) Bounds {
	return Bounds{
		Origin: Point{X: min32(near.X, far.X), Y: min32(near.Y, far.Y)},
		Size:   Size{Width: max32(near.X, far.X) - min32(near.X, far.X), Height: max32(near.Y, far.Y) - min32(near.Y, far.Y)},
	}
}

// ---------------------------------------------------------------------------
// Public inline observables (the port's test-support surface; the
// reference exposes none of these — same honest pattern as
// WindowDebugBounds / WindowPrimitiveCounts)
// ---------------------------------------------------------------------------

// InlineBoxFacts are the observable placement facts of one positioned
// inline box of the last completed frame.
type InlineBoxFacts struct {
	// ID is the box's document identifier (its box index).
	ID uint64
	// LineIndex is the row containing the box.
	LineIndex int
	// Bounds are the placed bounds relative to the paragraph origin.
	Bounds Bounds
}

// InlineSpanFacts are the observable fragment facts of one span (a
// text element or inline container) of the last completed frame.
type InlineSpanFacts struct {
	// Range is the span's byte range in the paragraph document.
	Range TextRange
	// Text is the span's text contribution (empty for container spans
	// whose children contributed directly).
	Text string
	// Fragments are the span's placed fragment regions relative to the
	// paragraph origin (snapped and merged).
	Fragments []Bounds
	// Bounds are the span's placed union bounds relative to the
	// paragraph origin.
	Bounds Bounds
}

// InlineParagraphFacts are the observable facts of one paragraph of
// the last completed frame.
type InlineParagraphFacts struct {
	// Origin is the paragraph's window-local paint origin.
	Origin Point
	// Text is the paragraph document's text.
	Text string
	// Size is the paragraph layout's natural size.
	Size Size
	// WrapWidth is the wrap width the paragraph was measured under
	// (nil when unwrapped).
	WrapWidth *float32
	// AlignmentOffset is the paragraph's shared alignment anchor.
	AlignmentOffset float32
	// Rows are the paragraph's visual rows.
	Rows []InlineVisualLine
	// Boxes are the paragraph's positioned boxes.
	Boxes []InlineBoxFacts
	// Spans are the paragraph's spans with their fragment geometry.
	Spans []InlineSpanFacts
}

// WindowInlineFacts returns the last completed frame's inline
// paragraph facts in document order (the test-support observable of
// the inline layer: rows, boxes, fragments and sizes of every placed
// paragraph). A window without inline content returns nil.
func WindowInlineFacts(w *Window) []InlineParagraphFacts {
	ds, ok := windowDrawStates[w]
	if !ok {
		return nil
	}
	return ds.lastInlineFacts
}

// WindowInlineSpanAt resolves a window-local point to the text of the
// span whose placed fragment contains it (the hit-region observable of
// the inline layer: the same fragment geometry the paint and selection
// read; the later hitbox/input tickets attach the same regions to
// interactive hitboxes). Later paragraphs win (paint order).
func WindowInlineSpanAt(w *Window, x, y float32) (string, bool) {
	facts := WindowInlineFacts(w)
	hit := ""
	found := false
	for _, paragraph := range facts {
		px, py := x-paragraph.Origin.X, y-paragraph.Origin.Y
		for _, span := range paragraph.Spans {
			for _, fragment := range span.Fragments {
				if px >= fragment.Origin.X && px < fragment.Origin.X+fragment.Size.Width &&
					py >= fragment.Origin.Y && py < fragment.Origin.Y+fragment.Size.Height {
					hit, found = span.Text, true
				}
			}
		}
	}
	return hit, found
}

// recordInlineFacts captures one paragraph's observable facts into the
// frame under construction (swapped into the window state when the
// frame completes).
func recordInlineFacts(w *Window, paragraph *inlineParagraph) {
	frame := currentFrame(w)
	layout := paragraph.measurement.layout
	facts := InlineParagraphFacts{
		Origin:          paragraph.paintOrigin,
		Text:            paragraph.document.text,
		Size:            layout.size,
		WrapWidth:       paragraph.measurement.wrapWidth,
		AlignmentOffset: layout.alignmentOffset,
		Rows:            append([]InlineVisualLine{}, layout.lines...),
	}
	for _, box := range layout.boxes {
		facts.Boxes = append(facts.Boxes, InlineBoxFacts{ID: box.ID, LineIndex: box.LineIndex, Bounds: box.Bounds})
	}
	for _, span := range paragraph.document.spans {
		text := ""
		if span.textRange.Start <= span.textRange.End && span.textRange.End <= len(paragraph.document.text) {
			text = paragraph.document.text[span.textRange.Start:span.textRange.End]
		}
		// The stored regions are placement-shifted by the element offset
		// at place time (zero in this slice); recover the
		// paragraph-relative origin by subtracting the paragraph's paint
		// origin.
		regions, placed := drawState(w).engine.InlineFragments(span.layoutID, frame.scale, Point{})
		spanFacts := InlineSpanFacts{Range: span.textRange, Text: text}
		if placed {
			for _, region := range regions {
				relative := Bounds{
					Origin: Point{X: region.Origin.X - paragraph.paintOrigin.X, Y: region.Origin.Y - paragraph.paintOrigin.Y},
					Size:   region.Size,
				}
				spanFacts.Fragments = append(spanFacts.Fragments, relative)
				spanFacts.Bounds = unionBounds(spanFacts.Bounds, relative)
			}
		}
		facts.Spans = append(facts.Spans, spanFacts)
	}
	frame.inlineFacts = append(frame.inlineFacts, facts)
}
