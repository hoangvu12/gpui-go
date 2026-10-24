package base

// Port of U:input/base/element.rs: TextElement, the element that lays out
// and paints the input's text, selections and carets.
//
// Core seams (R: = reference/ce-source/crates/gpui/src; legacy text API
// through the ticket-10 facade, gpui/shaped_line.go):
//
//   - window.text_system().shape_line / layout_width →
//     Window.TextSystem().ShapeLine / LayoutWidth;
//   - window.text_style / rem_size / line_height → gpui.WindowTextStyle,
//     gpui.RemSize, Window.LineHeight;
//   - window.request_layout → gpui.RequestElementLayout;
//   - window.handle_input + ElementInputHandler → Window.HandleInput with
//     inputElementHandler (input_state_edit.go);
//   - PathBuilder::fill/stroke + paint_path → gpui.FillPathBuilder /
//     StrokePathBuilder + Window.PaintPath; paint_quad(fill(..)) →
//     Window.PaintQuad(gpui.Fill(..)).
//
// The code-editor paths (editor scrollbar, gutter, fold icons, indent
// guides, ghost lines, search-match / hover / range-decoration paths and
// syntax highlighting) are editor_element.go and editor_indent.go; the
// diagnostic underline style is editor_diagnostics.go's and the
// semantic-token styles and hover-definition hitbox come from
// editor_lsp*.go.

import (
	"math"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/hoangvu12/gpui-go/base/rope"
	"github.com/hoangvu12/gpui-go/gpui"
)

const (
	// bottomMarginRows is BOTTOM_MARGIN_ROWS (U:input/base/element.rs:49).
	bottomMarginRows = 3
	// inputRightMargin is RIGHT_MARGIN (U:input/base/element.rs:50).
	inputRightMargin float32 = 10
	// lineNumberRightMargin is LINE_NUMBER_RIGHT_MARGIN
	// (U:input/base/element.rs:51).
	lineNumberRightMargin float32 = 6
	// foldIconHitboxWidth is FOLD_ICON_HITBOX_WIDTH
	// (U:input/base/element.rs:53).
	foldIconHitboxWidth float32 = 18
	// minLineNumberDigits / maxLineNumberDigits / maxDisplayedLineNumber
	// are U:input/base/element.rs:55-57.
	minLineNumberDigits    = 3
	maxLineNumberDigits    = 7
	maxDisplayedLineNumber = 9_999_999
)

// lineNumberLen is U:input/base/element.rs:59-61.
func lineNumberLen(totalLines int) int {
	return min(max(len(strconv.Itoa(totalLines)), minLineNumberDigits), maxLineNumberDigits)
}

// displayedLineNumber is U:input/base/element.rs:63-65.
func displayedLineNumber(number int) int { return min(number, maxDisplayedLineNumber) }

// composeDecorations layers application decorations over styles within
// the visible range; nil when nothing styles it
// (U:input/base/element.rs:69-93).
func composeDecorations(styles, decorations []gpui.HighlightRange, visible gpui.TextRange) []gpui.HighlightRange {
	var clipped []gpui.HighlightRange
	for _, d := range decorations {
		r := gpui.TextRange{Start: max(d.Range.Start, visible.Start), End: min(d.Range.End, visible.End)}
		if r.Start < r.End {
			clipped = append(clipped, gpui.HighlightRange{Range: r, Style: d.Style})
		}
	}
	if len(clipped) == 0 {
		if len(styles) == 0 {
			return nil
		}
		return styles
	}
	if len(styles) == 0 {
		styles = []gpui.HighlightRange{{Range: visible}}
	}
	// Decorations are application overrides and win over syntax and
	// semantic highlighting.
	return gpui.CombineHighlights(styles, clipped)
}

// clampAutoGrowVerticalScrollOffset is U:input/base/element.rs:294-305.
func clampAutoGrowVerticalScrollOffset(mode *layoutMode, scrollTop, scrollHeight, inputHeight float32) float32 {
	if mode.isAutoGrow() {
		return clampF(scrollTop, min(inputHeight-scrollHeight, 0), 0)
	}
	return scrollTop
}

// maskedDisplayOffset converts an offset in the text to one in the masked
// display string (U:input/base/element.rs:327-334).
func maskedDisplayOffset(text *rope.Rope, offset int) int {
	return RopeExtOf(text).OffsetToCharIndex(offset) * inputMaskCharLen
}

// imeMarkedDisplayRange moves the marked range into the display text's
// coordinates (U:input/base/element.rs:336-350).
func imeMarkedDisplayRange(text *rope.Rope, marked *gpui.TextRange, masked bool) *gpui.TextRange {
	if marked == nil {
		return nil
	}
	if masked {
		return &gpui.TextRange{Start: maskedDisplayOffset(text, marked.Start), End: maskedDisplayOffset(text, marked.End)}
	}
	out := *marked
	return &out
}

// viewportVisibleLines is the viewport capacity in display rows
// (U:input/base/element.rs:352-359).
func viewportVisibleLines(viewportHeight, lineHeight float32) int {
	return int(viewportHeight / lineHeight)
}

// cursorSurroundingPadding is the clearance kept between the cursor and
// the viewport edges (U:input/base/element.rs:361-393).
func cursorSurroundingPadding(isAutoGrow bool, overrideLines *int, visibleLines int, lineHeight float32) float32 {
	if isAutoGrow {
		return lineHeight
	}
	var raw float32
	switch {
	case overrideLines != nil:
		raw = float32(*overrideLines) * lineHeight
	case visibleLines < bottomMarginRows*8:
		raw = lineHeight
	default:
		raw = bottomMarginRows * lineHeight
	}
	// Saturate against half the viewport.
	return min(raw, float32(visibleLines)*lineHeight/2)
}

// emptyBottomHeight is the empty area below the last line
// (U:input/base/element.rs:395-414).
func emptyBottomHeight(isCodeEditor bool, overrideRows *int, viewportHeight, lineHeight float32) float32 {
	if !isCodeEditor {
		return 0
	}
	if overrideRows != nil {
		return float32(*overrideRows) * lineHeight
	}
	return max(viewportHeight/2, bottomMarginRows*lineHeight)
}

// textElement is TextElement (U:input/base/element.rs:424-441).
type textElement[M InputModeKind] struct {
	state       gpui.Entity[InputBaseState[M]]
	placeholder string
}

// newTextElement is TextElement::new (U:input/base/element.rs:430-436).
func newTextElement[M InputModeKind](state gpui.Entity[InputBaseState[M]]) *textElement[M] {
	return &textElement[M]{state: state}
}

// withPlaceholder is TextElement::placeholder (U:input/base/element.rs:437-441).
func (e *textElement[M]) withPlaceholder(placeholder string) *textElement[M] {
	e.placeholder = placeholder
	return e
}

// IntoElement implements gpui.IntoElement (U:input/base/element.rs:2202-2208).
func (e *textElement[M]) IntoElement() gpui.AnyElement {
	return gpui.CustomElement[struct{}, textPrepaintState](e)
}

func (e *textElement[M]) read(app *gpui.App) *InputBaseState[M] {
	return e.state.Read(app, func(s *InputBaseState[M], _ *gpui.App) *InputBaseState[M] { return s })
}

// paintMouseListeners extends a drag and ends it on release
// (U:input/base/element.rs:443-471).
func (e *textElement[M]) paintMouseListeners(w *gpui.Window) {
	state := e.state
	w.OnMouseEvent(func(event any, phase gpui.DispatchPhase, w *gpui.Window, app *gpui.App) {
		switch ev := event.(type) {
		case *gpui.MouseMoveEvent:
			if ev.PressedButton != nil && *ev.PressedButton == gpui.MouseButtonLeft {
				_ = state.UpdateIn(app, w, func(s *InputBaseState[M], w *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
					s.onDragMove(ev, w, cx)
				})
			}
		case *gpui.MouseUpEvent:
			if phase != gpui.DispatchBubble {
				return
			}
			// Stop the autoscroll and the selecting on release.
			state.Update(app, func(s *InputBaseState[M], _ *gpui.Context[InputBaseState[M]]) {
				s.autoScroll.Stop()
				s.selecting = false
				s.columnSelectStart = nil
			})
		}
	})
}

// cursorRenderInfo is one caret's layout (U:input/base/element.rs:2142-2147).
type cursorRenderInfo struct {
	bounds   gpui.Bounds
	isActive bool
}

// layoutCursors lays out the carets and updates the scroll position for
// the active one (U:input/base/element.rs:473-672).
func (e *textElement[M]) layoutCursors(layout *lastLayout, bounds *gpui.Bounds, scrollSize gpui.Size, app *gpui.App) ([]cursorRenderInfo, gpui.Point, int, bool) {
	state := e.read(app)

	lineHeight := layout.lineHeight
	visibleRange := layout.visibleRange
	lineNumberWidth := layout.lineNumberWidth

	activeID := state.activeSelection().id
	scroll := state.scrollHandle.Offset()
	currentRow, hasCurrentRow := 0, false
	infos := make([]cursorRenderInfo, 0, state.selections.len())

	topBottomMargin := cursorSurroundingPadding(state.mode.isAutoGrow(), state.cursorSurroundingLines,
		viewportVisibleLines(bounds.Size.Height, lineHeight), lineHeight)

	// A cursor or selection endpoint in content space.
	caretFor := func(row, offset int, affinity bool) gpui.Point {
		top := lineHeight * float32(state.displayMap.bufferLineToDisplayRow(row))
		for vi, bufferLine := range layout.visibleBufferLines {
			if bufferLine != row {
				continue
			}
			local := max(offset-layout.visibleLineByteOffsets[vi], 0)
			if pos, ok := layout.lines[vi].positionForIndex(local, layout, affinity); ok {
				return gpui.Point{X: pos.X, Y: top + pos.Y}
			}
			break
		}
		return gpui.Point{Y: top}
	}

	cursorHeight := 0.85 * lineHeight
	ext := state.ext()

	for sel := range state.selections.iter() {
		isActive := sel.id == activeID

		selected := NewInputSelection(sel.start, sel.end)
		cursor := sel.cursorOffset()
		if isActive && state.imeMarkedRange != nil {
			selected = NewInputSelection(state.imeMarkedRange.end, state.imeMarkedRange.end)
			cursor = state.imeMarkedRange.end
		}
		isSelectedAll := selected.Len() == state.text.Len()

		cursorRow := ext.OffsetToPoint(cursor).Row
		// Inactive cursors far outside the visible range are skipped.
		if !isActive && (cursorRow+2 < visibleRange.Start || cursorRow > visibleRange.End+2) {
			continue
		}

		selStartRow := ext.OffsetToPoint(selected.Start).Row
		selEndRow := ext.OffsetToPoint(selected.End).Row
		if state.masked {
			selected.Start = maskedDisplayOffset(&state.text, selected.Start)
			selected.End = maskedDisplayOffset(&state.text, selected.End)
			cursor = maskedDisplayOffset(&state.text, cursor)
		}

		affinity := isActive && state.cursorLineEndAffinity
		cursorPos := caretFor(cursorRow, cursor, affinity)
		cursorStart := caretFor(selStartRow, selected.Start, false)
		cursorEnd := caretFor(selEndRow, selected.End, false)

		if isActive {
			currentRow, hasCurrentRow = cursorRow, true

			selectionChanged := state.lastSelectedRange == nil ||
				state.lastSelectedRange.start != selected.Start || state.lastSelectedRange.end != selected.End
			autoScrolling := state.autoScroll.IsActive()
			if selectionChanged && !isSelectedAll {
				// Right alignment uses no margin: the caret is clamped to
				// the bounds separately.
				var safetyMargin float32
				switch layout.textAlign {
				case gpui.TextAlignLeft:
					safetyMargin = inputRightMargin
				case gpui.TextAlignCenter:
					safetyMargin = cursorWidth
				}

				switch {
				case scroll.X+cursorPos.X > bounds.Size.Width-lineNumberWidth-safetyMargin:
					// Out of the right.
					scroll.X = bounds.Size.Width - lineNumberWidth - safetyMargin - cursorPos.X
				case scroll.X+cursorPos.X < 0:
					// Out of the left.
					scroll.X = scroll.X - cursorPos.X
				}

				// Vertical follow is suppressed while autoscroll manages y.
				if !autoScrolling {
					switch {
					case scroll.Y+cursorPos.Y > bounds.Size.Height-topBottomMargin:
						scroll.Y = bounds.Size.Height - topBottomMargin - cursorPos.Y
					case scroll.Y+cursorPos.Y < topBottomMargin:
						scroll.Y = min(topBottomMargin-cursorPos.Y, 0)
					}
				}

				// The moving end of a selection scrolls too.
				if sel.reversed {
					if scroll.X+cursorStart.X < 0 {
						scroll.X = -cursorStart.X
					}
					if !autoScrolling && scroll.Y+cursorStart.Y < 0 {
						scroll.Y = -cursorStart.Y
					}
				} else {
					if scroll.X+cursorEnd.X <= 0 {
						scroll.X = -cursorEnd.X
					}
					if !autoScrolling && scroll.Y+cursorEnd.Y <= 0 {
						scroll.Y = -cursorEnd.Y
					}
				}
			}
		}

		// The caret follows the deferred scroll target the text paints at.
		cursorScrollX := scroll.X
		if state.deferredScrollOffset != nil {
			cursorScrollX = state.deferredScrollOffset.X
		}

		// Right alignment clamps the caret inside the right edge.
		cursorX := bounds.Left() + cursorPos.X + lineNumberWidth + cursorScrollX
		if layout.textAlign == gpui.TextAlignRight {
			cursorX = min(cursorX, bounds.Right()-cursorWidth)
		}
		infos = append(infos, cursorRenderInfo{
			bounds: gpui.Bounds{
				Origin: gpui.Point{X: cursorX, Y: bounds.Top() + cursorPos.Y + (lineHeight-cursorHeight)/2},
				Size:   gpui.Size{Width: cursorWidth, Height: cursorHeight},
			},
			isActive: isActive,
		})
	}

	if state.deferredScrollOffset != nil {
		scroll = *state.deferredScrollOffset
	}
	scroll.Y = clampAutoGrowVerticalScrollOffset(&state.mode, scroll.Y, scrollSize.Height, bounds.Size.Height)

	bounds.Origin = gpui.Point{X: bounds.Origin.X + scroll.X, Y: bounds.Origin.Y + scroll.Y}
	return infos, scroll, currentRow, hasCurrentRow
}

// pointCorners is Corners<Point<Pixels>>, the four corners of a line
// rectangle.
type pointCorners struct {
	topLeft, topRight, bottomLeft, bottomRight gpui.Point
}

// layoutMatchRange lays a range out as a filled path
// (U:input/base/element.rs:674-690).
func layoutMatchRange(start, end int, layout *lastLayout, bounds gpui.Bounds) (*gpui.Path, bool) {
	corners := layoutRangeCorners(start, end, layout)
	if len(corners) == 0 {
		return nil, false
	}
	points := frameOutlinePoints(corners)
	if len(points) == 0 {
		return nil, false
	}
	ox, oy := bounds.Origin.X+layout.lineNumberWidth, bounds.Origin.Y
	b := gpui.FillPathBuilder().MoveTo(ox+points[0].X, oy+points[0].Y)
	for _, p := range points[1:] {
		b.LineTo(ox+p.X, oy+p.Y)
	}
	b.Close()
	path, err := b.Build()
	if err != nil {
		return nil, false
	}
	return path, true
}

// layoutRangeCorners projects a range through the visible shaped lines;
// half-open intersections give soft-wrap ends their trailing affinity
// (U:input/base/element.rs:692-744).
func layoutRangeCorners(start, end int, layout *lastLayout) []pointCorners {
	start = max(start, layout.visibleRangeOffset.Start)
	end = min(end, layout.visibleRangeOffset.End)
	if start >= end {
		return nil
	}

	y := layout.visibleTop
	var corners []pointCorners
	for i, line := range layout.lines {
		if i >= len(layout.visibleLineByteOffsets) {
			break
		}
		offset := layout.visibleLineByteOffsets[i]
		alignment := layout.alignmentOffset(line.longestWidth)
		for row, shaped := range line.wrappedLines {
			lineEnd := offset + shaped.len
			startIx := max(start, offset)
			endIx := min(end, lineEnd)
			isLast := row+1 == len(line.wrappedLines)
			// A selected newline has a one-space cell, including on empty
			// lines; a wrap boundary does not.
			newline := isLast && start <= lineEnd && end > lineEnd
			if startIx < endIx || newline {
				indent := float32(0)
				if row != 0 {
					indent = line.wrapIndent
				}
				left := alignment + indent + shaped.xForIndex(min(startIx, lineEnd)-offset)
				right := alignment + indent
				if newline {
					right += shaped.width + layout.spaceWidth
				} else {
					right += shaped.xForIndex(endIx - offset)
				}
				corners = append(corners, pointCorners{
					topLeft:     gpui.Point{X: left, Y: y},
					topRight:    gpui.Point{X: right, Y: y},
					bottomLeft:  gpui.Point{X: left, Y: y + layout.lineHeight},
					bottomRight: gpui.Point{X: right, Y: y + layout.lineHeight},
				})
			}
			offset = lineEnd
			y += layout.lineHeight
		}
	}
	return corners
}

// layoutSelections lays out every selection's highlight; nothing while
// unfocused (U:input/base/element.rs:880-960). Inline tokens join with
// their port.
func (e *textElement[M]) layoutSelections(layout *lastLayout, bounds gpui.Bounds, w *gpui.Window, app *gpui.App) []*gpui.Path {
	state := e.read(app)
	if !state.focusHandle.IsFocused(w) {
		return nil
	}

	activeID := state.activeSelection().id
	var paths []*gpui.Path
	for sel := range state.selections.iter() {
		selected := NewInputSelection(sel.start, sel.end)
		// IME composition replaces the active selection with a caret.
		if sel.id == activeID && state.imeMarkedRange != nil && !state.imeMarkedRange.isEmpty() {
			selected = NewInputSelection(state.imeMarkedRange.end, state.imeMarkedRange.end)
		}
		if selected.IsEmpty() {
			continue
		}
		if state.masked {
			selected.Start = maskedDisplayOffset(&state.text, selected.Start)
			selected.End = maskedDisplayOffset(&state.text, selected.End)
		}
		startIx, endIx := min(selected.Start, selected.End), max(selected.Start, selected.End)
		start := max(startIx, layout.visibleRangeOffset.Start)
		end := min(endIx, layout.visibleRangeOffset.End)
		if start >= end {
			continue
		}
		if path, ok := layoutMatchRange(start, end, layout, bounds); ok {
			paths = append(paths, path)
		}
	}
	return paths
}

// calculateVisibleRange is the visible buffer-line range, the visible
// buffer lines and the top of the first one
// (U:input/base/element.rs:962-1033).
func calculateVisibleRange[M InputModeKind](state *InputBaseState[M], lineHeight, inputHeight float32) (InputSelection, []int, float32) {
	// Extra rows avoid showing empty space when scrolled to the bottom.
	const extraRows = 1
	if state.IsSingleLine() {
		return NewInputSelection(0, 1), []int{0}, 0
	}

	totalLines := state.displayMap.wrapRowCount()
	displayCount := state.displayMap.displayRowCount()
	bufferLineCount := state.displayMap.bufferLineCount()
	if displayCount == 0 || bufferLineCount == 0 {
		return NewInputSelection(0, 0), nil, 0
	}

	scrollTop := state.scrollHandle.Offset().Y
	if state.deferredScrollOffset != nil {
		scrollTop = state.deferredScrollOffset.Y
	}
	scrollTop = clampAutoGrowVerticalScrollOffset(&state.mode, scrollTop, lineHeight*float32(totalLines), inputHeight)

	viewportTop := max(-scrollTop, 0)
	viewportBottom := viewportTop + inputHeight
	firstDisplay := min(int(math.Floor(float64(viewportTop/lineHeight))), displayCount-1)
	lastDisplay := min(int(math.Ceil(float64(viewportBottom/lineHeight))), displayCount-1)

	startLine := state.displayMap.displayRowToBufferLine(firstDisplay)
	endLine := state.displayMap.displayRowToBufferLine(lastDisplay)

	visibleTop := lineHeight * float32(firstDisplay)
	if start, ok := state.displayMap.bufferLineToDisplayRowStart(startLine); ok {
		visibleTop = lineHeight * float32(start)
	}

	visibleRange := NewInputSelection(startLine, min(endLine+1+extraRows, bufferLineCount))
	lines := make([]int, 0, visibleRange.Len())
	for ix := visibleRange.Start; ix < visibleRange.End; ix++ {
		if state.displayMap.visibleWrapRowCountForBufferLine(ix) > 0 {
			lines = append(lines, ix)
		}
	}
	return visibleRange, lines, visibleTop
}

// layoutLineNumbers is the line-number column width and digit count
// (U:input/base/element.rs:1035-1077).
func layoutLineNumbers[M InputModeKind](state *InputBaseState[M], text *rope.Rope, fontSize float32, style gpui.TextStyle, w *gpui.Window) (float32, int) {
	digits := lineNumberLen(RopeExtOf(text).LinesLen())
	var width float32
	switch {
	case state.mode.showLineNumber():
		empty := mustShapeLine(w, strings.Repeat("+", digits), fontSize, []gpui.TextRun{{
			Len: digits, Font: style.FontDescriptorOf(), Color: gpui.Black(),
		}})
		width = empty.Width() + lineNumberRightMargin
	case state.IsCodeEditor():
		width = lineNumberRightMargin
	}
	if state.mode.isFolding() {
		// Extra space for fold icons.
		width += foldIconHitboxWidth
	}
	return width, digits
}

// layoutWhitespaceIndicators shapes the space and tab markers
// (U:input/base/element.rs:1079-1134).
func layoutWhitespaceIndicators[M InputModeKind](state *InputBaseState[M], textSize float32, style gpui.TextStyle, w *gpui.Window) *whitespaceIndicators {
	if !state.showWhitespaces {
		return nil
	}
	invisible := state.editorStyle.MutedForeground
	if state.editorStyle.EditorInvisible != nil {
		invisible = *state.editorStyle.EditorInvisible
	}
	run := func(text string) []gpui.TextRun {
		return []gpui.TextRun{{Len: len(text), Font: style.FontDescriptorOf(), Color: invisible}}
	}
	return &whitespaceIndicators{
		space: mustShapeLine(w, "•", textSize/2, run("•")),
		tab:   mustShapeLine(w, "→", textSize, run("→")),
	}
}

// mustShapeLine shapes one line; the legacy shape_line is infallible
// (legacy text_system.rs:430-469), so a facade error panics. The legacy
// call takes an empty run list for an empty line (an empty input with no
// placeholder, an empty buffer line); the facade requires one run
// covering the text, so an empty run list becomes one zero-length run in
// the window's font.
func mustShapeLine(w *gpui.Window, text string, fontSize float32, runs []gpui.TextRun) *gpui.ShapedLine {
	if len(runs) == 0 && text == "" {
		runs = []gpui.TextRun{{Font: gpui.WindowTextStyle(w).FontDescriptorOf()}}
	}
	line, err := w.TextSystem().ShapeLine(text, fontSize, runs, nil)
	if err != nil {
		panic("base: input shape_line: " + err.Error())
	}
	return line
}

// layoutLines shapes the visible lines (U:input/base/element.rs:1867-1987).
func layoutLines[M InputModeKind](state *InputBaseState[M], displayText *rope.Rope, layout *lastLayout, fontSize float32, runs []gpui.TextRun, bgSegments []inputColoredRange, indicators *whitespaceIndicators, w *gpui.Window) []*inputLineLayout {
	if state.tokensVisible() {
		return layoutTokenLines(state, layout, fontSize, runs, w)
	}
	// Hard tabs draw as blank columns (input_tab_map.go).
	tabs := state.displayTabs(w)
	if state.IsSingleLine() {
		text := displayText.String()
		lineRuns := runs
		if aligned, ok := alignRunsToCharBoundaries(text, runs); ok {
			lineRuns = aligned
		}
		shaped := tabs.shapeLine(w, text, 0, fontSize, lineRuns)
		return []*inputLineLayout{newInputLineLayout([]*gpui.ShapedLine{shaped}).
			withBackground(hasBackground(lineRuns)).
			withWhitespaces(indicators)}
	}

	// Empty: the placeholder, which is not in the wrapper map.
	if state.text.Len() == 0 {
		var lines []*gpui.ShapedLine
		lineHasBackground := false
		for _, pl := range placeholderLineRuns(displayText.String(), runs) {
			lines = append(lines, tabs.shapeLine(w, pl.line, 0, fontSize, pl.runs))
			lineHasBackground = lineHasBackground || hasBackground(pl.runs)
		}
		return []*inputLineLayout{newInputLineLayout(lines).withBackground(lineHasBackground).withWhitespaces(indicators)}
	}

	lines := make([]*inputLineLayout, 0, len(layout.visibleBufferLines))
	runOffset := 0
	ext := RopeExtOf(displayText)
	for vi, bufferLine := range layout.visibleBufferLines {
		lineText := ext.SliceLine(bufferLine).String()
		var wrapped []*gpui.ShapedLine
		lineHasBackground := false
		for _, rng := range state.displayMap.wrappedRanges(bufferLine) {
			lineRuns := runsForRange(runs, runOffset, rng)
			if len(bgSegments) > 0 {
				lineRuns = splitRunsByBgSegments(layout.visibleLineByteOffsets[vi]+rng.Start, lineRuns, bgSegments)
			}
			sub := lineText[rng.Start:rng.End]
			if aligned, ok := alignRunsToCharBoundaries(sub, lineRuns); ok {
				lineRuns = aligned
			}
			// Tab columns count from the buffer line's start.
			col := 0
			if strings.Contains(sub, "\t") {
				col = tabs.columnAfter(lineText[:rng.Start], 0)
			}
			wrapped = append(wrapped, tabs.shapeLine(w, sub, col, fontSize, lineRuns))
			lineHasBackground = lineHasBackground || hasBackground(lineRuns)
		}
		// Continuation lines take the first visual line's indentation width.
		var wrapIndent float32
		if item, ok := state.displayMap.line(bufferLine); ok && item.indent > 0 && len(wrapped) > 1 {
			wrapIndent = wrapped[0].XForIndex(nthCharByteIndex(lineText, int(item.indent)))
		}
		lines = append(lines, newInputLineLayout(wrapped).withWrapIndent(wrapIndent).withBackground(lineHasBackground).withWhitespaces(indicators))
		// +1 for the `\n`.
		runOffset += len(lineText) + 1
	}
	return lines
}

// highlightLines is the composed highlight styles of the visible lines:
// the highlighter's styles per contiguous group of visible lines with the
// hover-definition style, composed with the semantic tokens, the
// application decorations, then the diagnostics; nil when nothing styles
// them (U:input/base/element.rs:1989-2139).
func (e *textElement[M]) highlightLines(state *InputBaseState[M], visibleBufferLines []int, visible gpui.TextRange, app *gpui.App) []gpui.HighlightRange {
	highlighter := state.mode.currentHighlighter()
	if highlighter == nil {
		if state.masked {
			return nil
		}
		// Spellcheck markers (browser-core ticket 30): composed over a
		// full-cover base so the returned styles partition the visible
		// text — the run assembly treats the style list as the whole run
		// list (the diagnostics never hit this path: a code editor has a
		// highlighter).
		spellcheckStyles := state.spellcheckHighlightStyles(visible)
		if len(spellcheckStyles) == 0 {
			return composeDecorationCollections(nil, state.extras.decorationLayers(), visible)
		}
		base := []gpui.HighlightRange{{Range: visible}}
		return composeDecorationCollections(gpui.CombineHighlights(base, spellcheckStyles), state.extras.decorationLayers(), visible)
	}
	resolver := state.editorStyle.HighlightStyles
	if resolver == nil {
		resolver = noHighlightStyles{}
	}
	ext := state.ext()
	multiLine := state.IsMultiLine()
	var styles []gpui.HighlightRange
	// The byte ranges of the flushed groups: the composed styles are
	// clipped to them so the runs cover exactly the visible lines' bytes.
	var groups []gpui.TextRange
	flush := func(startLine, endLine int, skip bool) {
		byteStart := ext.LineStartOffset(startLine)
		byteEnd := ext.LineEndOffset(endLine)
		if multiLine {
			// +1 for the `\n`.
			byteEnd = ext.LineStartOffset(endLine + 1)
		}
		rng := gpui.TextRange{Start: byteStart, End: byteEnd}
		if skip {
			styles = append(styles, gpui.HighlightRange{Range: rng})
		} else {
			styles = append(styles, highlighter.Styles(rng, resolver)...)
		}
		groups = append(groups, rng)
	}
	rangeStart, hasStart := 0, false
	for i, line := range visibleBufferLines {
		if ext.SliceLine(line).Len() > maxHighlightLineLength {
			if hasStart {
				flush(rangeStart, line-1, false)
				hasStart = false
			}
			flush(line, line, true)
			continue
		}
		if !hasStart {
			rangeStart, hasStart = line, true
		}
		if i+1 < len(visibleBufferLines) && visibleBufferLines[i+1] == line+1 {
			continue
		}
		flush(rangeStart, line, false)
		hasStart = false
	}

	var diagnosticStyles []gpui.HighlightRange
	if diagnostics := state.mode.diagnosticSet(); diagnostics != nil {
		for entry := range diagnostics.Range(NewInputSelection(visible.Start, visible.End)) {
			diagnosticStyles = append(diagnosticStyles, gpui.HighlightRange{
				Range: gpui.TextRange{Start: entry.ByteRange.Start, End: entry.ByteRange.End},
				Style: diagnosticHighlightStyle(entry.Severity, state.editorStyle.Diagnostics),
			})
		}
	}

	// Range semantic tokens from the LSP provider's cached result, resolved
	// through the active highlight theme: the same color vocabulary as the
	// syntax highlighter. Empty when no provider is set.
	customStyles := state.extras.semanticTokenStyles(state.text, NewInputSelection(visible.Start, visible.End), resolver)

	if hover, ok := state.hooks.hoverDefinitionStyle(state, app); ok {
		styles = append(styles, hover)
	}

	// Compose syntax, semantic, application, then diagnostic styles.
	// Spellcheck markers (browser-core ticket 30) compose like the
	// diagnostics (input_spellcheck.go); without them the composition
	// is byte-for-byte the previous one.
	spellcheckStyles := state.spellcheckHighlightStyles(visible)
	styles = gpui.CombineHighlights(customStyles, styles)
	if !state.masked {
		styles = composeDecorationCollections(styles, state.extras.decorationLayers(), visible)
	}
	styles = gpui.CombineHighlights(diagnosticStyles, styles)
	if len(spellcheckStyles) > 0 {
		styles = gpui.CombineHighlights(spellcheckStyles, styles)
	}

	// Some sources reach outside the flushed groups (a diagnostic
	// straddling the viewport edge, any range inside a fold); clip the
	// composed styles back to the groups.
	return clipStylesToRanges(styles, groups)
}

// textPrepaintState is PrepaintState (U:input/base/element.rs:2149-2200)
// for the ported paths.
type textPrepaintState struct {
	lastLayout         *lastLayout
	scrollSize         gpui.Size
	cursorInfos        []cursorRenderInfo
	cursorScrollOffset gpui.Point
	currentRow         int
	hasCurrentRow      bool
	selectionPaths     []*gpui.Path
	tokenElements      []gpui.AnyElement
	documentColorPaths []coloredPath
	hitbox             gpui.Hitbox
	bounds             gpui.Bounds
	// The code-editor layout (U:input/base/element.rs:2149-2183).
	lineNumbers           []lineNumberLayout
	hoverHighlightPath    *gpui.Path
	searchMatchPaths      []searchMatchPath
	rangeDecorationFills  []coloredPath
	rangeDecorationFrames []coloredPath
	indentGuidesPath      *gpui.Path
	foldIconLayout        foldIconLayout
	ghostLines            []*gpui.ShapedLine
	ghostFirstLine        *gpui.ShapedLine
	ghostLinesHeight      float32
	// hoverDefinitionHitbox makes a Cmd-hovered symbol clickable
	// (U:input/base/element.rs:2170).
	hoverDefinitionHitbox *gpui.Hitbox
}

type coloredPath struct {
	path  *gpui.Path
	color gpui.Hsla
}

// cursorInfosWithScroll is every caret adjusted for the vertical scroll
// (U:input/base/element.rs:2185-2200).
func (p *textPrepaintState) cursorInfosWithScroll() []cursorRenderInfo {
	out := make([]cursorRenderInfo, len(p.cursorInfos))
	for i, info := range p.cursorInfos {
		info.bounds.Origin.Y += p.cursorScrollOffset.Y
		out[i] = info
	}
	return out
}

// frameOutlinePoints is the closed outline of stacked line rectangles
// (U:input/base/element.rs:2242-2274).
func frameOutlinePoints(corners []pointCorners) []gpui.Point {
	type rect struct{ tl, br gpui.Point }
	rects := make([]rect, len(corners))
	for i, c := range corners {
		rects[i] = rect{c.topLeft, c.bottomRight}
	}
	points := make([]gpui.Point, 0, len(rects)*4)
	first, last := rects[0], rects[len(rects)-1]
	points = append(points, first.tl, gpui.Point{X: first.br.X, Y: first.tl.Y})
	for i := 0; i+1 < len(rects); i++ {
		current, next := rects[i], rects[i+1]
		points = append(points,
			current.br,
			gpui.Point{X: next.br.X, Y: current.br.Y},
			gpui.Point{X: next.br.X, Y: next.br.Y})
	}
	if points[len(points)-1] != last.br {
		points = append(points, last.br)
	}
	points = append(points, gpui.Point{X: last.tl.X, Y: last.br.Y})
	for i := len(rects) - 2; i >= 0; i-- {
		current, next := rects[i+1], rects[i]
		points = append(points,
			gpui.Point{X: current.tl.X, Y: current.tl.Y},
			gpui.Point{X: next.tl.X, Y: current.tl.Y},
			gpui.Point{X: next.tl.X, Y: next.tl.Y})
	}
	if points[len(points)-1] != points[0] {
		points = append(points, points[0])
	}
	return points
}

// padFrameCorners widens each rectangle horizontally
// (U:input/base/element.rs:2276-2298).
func padFrameCorners(corners []pointCorners, padding float32) []pointCorners {
	out := make([]pointCorners, len(corners))
	for i, c := range corners {
		out[i] = pointCorners{
			topLeft:     gpui.Point{X: c.topLeft.X - padding, Y: c.topLeft.Y},
			topRight:    gpui.Point{X: c.topRight.X + padding, Y: c.topRight.Y},
			bottomLeft:  gpui.Point{X: c.bottomLeft.X - padding, Y: c.bottomLeft.Y},
			bottomRight: gpui.Point{X: c.bottomRight.X + padding, Y: c.bottomRight.Y},
		}
	}
	return out
}

// snapFrameOutline aligns a stroke to physical pixels
// (U:input/base/element.rs:2300-2323).
func snapFrameOutline(points []gpui.Point, strokeWidth, scaleFactor float32) (float32, []gpui.Point) {
	physical := float32(math.Max(math.Ceil(math.Abs(float64(strokeWidth*scaleFactor))-0.5), 1))
	width := physical / scaleFactor
	centerOffset := float32(0.5)
	if math.Mod(float64(physical), 2) == 0 {
		centerOffset = 0
	}
	snap := func(v float32) float32 {
		return (float32(math.Round(float64(v*scaleFactor-centerOffset))) + centerOffset) / scaleFactor
	}
	out := make([]gpui.Point, len(points))
	for i, p := range points {
		out[i] = gpui.Point{X: snap(p.X), Y: snap(p.Y)}
	}
	return width, out
}

// clampFrameToContentMask keeps the vertical edges inside the content mask
// (U:input/base/element.rs:2325-2337).
func clampFrameToContentMask(points []gpui.Point, strokeWidth float32, mask gpui.Bounds) []gpui.Point {
	half := strokeWidth / 2
	minX := mask.Left() + half
	maxX := max(mask.Right()-half, minX)
	out := make([]gpui.Point, len(points))
	for i, p := range points {
		out[i] = gpui.Point{X: min(max(p.X, minX), maxX), Y: p.Y}
	}
	return out
}

// ID implements gpui.Element (U:input/base/element.rs:2343-2345).
func (e *textElement[M]) ID() (gpui.ElementID, bool) { return gpui.ElementID{}, false }

// SourceLocation implements gpui.Element (U:input/base/element.rs:2347-2349).
func (e *textElement[M]) SourceLocation() *gpui.SourceLocation { return nil }

// RequestLayout implements gpui.Element: full width; a single line is one
// line tall, a multi-line input grows (U:input/base/element.rs:2351-2379).
func (e *textElement[M]) RequestLayout(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, w *gpui.Window, app *gpui.App) (gpui.LayoutID, struct{}) {
	state := e.read(app)
	lineHeight := w.LineHeight()

	style := gpui.DefaultStyle()
	style.Size.Width = gpui.DefiniteLengthOf(gpui.Fraction(1))
	if state.IsMultiLine() {
		style.FlexGrow = 1
		style.Size.Height = gpui.DefiniteLengthOf(gpui.Fraction(1))
		if state.mode.isAutoGrow() {
			// Grow to the rows, not past the maximum.
			rows := min(state.mode.maxRowsCount(), state.mode.rowsCount())
			style.MinSize.Height = gpui.PxLength(float32(rows) * lineHeight)
		} else {
			style.MinSize.Height = gpui.PxLength(lineHeight)
		}
	} else {
		// A single line is exactly one line tall.
		style.Size.Height = gpui.PxLength(lineHeight)
	}
	id, err := gpui.RequestElementLayout(w, style)
	if err != nil {
		panic("base: TextElement request_layout: " + err.Error())
	}
	return id, struct{}{}
}

// Prepaint implements gpui.Element (U:input/base/element.rs:2381-2850).
func (e *textElement[M]) Prepaint(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, bounds gpui.Bounds, _ *struct{}, w *gpui.Window, app *gpui.App) textPrepaintState {
	textStyle := gpui.WindowTextStyle(w)
	font := textStyle.FontDescriptorOf()
	textSize := textStyle.FontSizePixels(gpui.RemSize(w))
	// Past the end of a line a pointer is measured in spaces.
	spaceWidth := w.TextSystem().LayoutWidth(font, textSize, ' ')

	_ = e.state.UpdateIn(app, w, func(s *InputBaseState[M], w *gpui.Window, _ *gpui.Context[InputBaseState[M]]) {
		s.displayMap.setFont(font, textSize, w)
		s.displayMap.setTabSize(max(s.mode.tabSize().TabSize, 1), w)
		s.displayMap.ensureTextPrepared(s.text, w)
	})

	state := e.read(app)
	multiLine := state.IsMultiLine()
	text := state.text
	isEmpty := text.Len() == 0

	disabled := state.disabled
	dim := func(c gpui.Hsla) gpui.Hsla {
		if disabled {
			return c.Opacity(0.5)
		}
		return c
	}
	fg := dim(textStyle.Color)
	displayText, textColor := text, fg
	switch {
	case isEmpty:
		displayText, textColor = rope.FromStr(e.placeholder), dim(state.editorStyle.MutedForeground)
	case state.masked:
		displayText = rope.FromStr(strings.Repeat(string(inputMaskChar), text.LenChars()))
	}

	lineNumberWidth, lineNumberDigits := layoutLineNumbers(state, &text, textSize, textStyle, w)

	// Spellcheck (browser-core ticket 30): queue a background check
	// when the document changed since the last request — one hook at
	// prepaint covers typing, programmatic sets and the initial value
	// (input_spellcheck.go).
	e.requestSpellcheck(state, app)

	var wrapWidth *float32
	if multiLine && state.softWrap {
		width := bounds.Size.Width - lineNumberWidth - inputRightMargin
		wrapWidth = &width
	}

	wrappingIndent := state.wrappingIndent
	wrapWidthChanged := state.lastLayout == nil || !equalF32Ptr(state.lastLayout.wrapWidth, wrapWidth)
	wrappingIndentChanged := state.lastLayout == nil || state.lastLayout.wrappingIndent != wrappingIndent
	if wrapWidthChanged || wrappingIndentChanged {
		_ = e.state.UpdateIn(app, w, func(s *InputBaseState[M], w *gpui.Window, _ *gpui.Context[InputBaseState[M]]) {
			s.displayMap.onLayoutChanged(wrapWidth, w)
			s.displayMap.setWrappingIndent(wrappingIndent, w)
		})
	}

	lineHeight := w.LineHeight()
	measuredTokens := e.measureTokens(max(bounds.Size.Width-lineNumberWidth-inputRightMargin, 1), lineHeight, bounds.Size.Height, w, app)
	visibleRange, visibleBufferLines, visibleTop := calculateVisibleRange(state, lineHeight, bounds.Size.Height)
	ext := state.ext()
	visibleStartOffset := ext.LineStartOffset(visibleRange.Start)
	visibleEndOffset := ext.LineEndOffset(max(visibleRange.End-1, 0))

	highlightStyles := e.highlightLines(state, visibleBufferLines, gpui.TextRange{Start: visibleStartOffset, End: visibleEndOffset}, app)

	lineByteOffsets := make([]int, len(visibleBufferLines))
	for i, bl := range visibleBufferLines {
		lineByteOffsets[i] = ext.LineStartOffset(bl)
	}
	// A masked input works in masked display offsets.
	visibleRangeOffset := NewInputSelection(visibleStartOffset, visibleEndOffset)
	if state.masked {
		for i, o := range lineByteOffsets {
			lineByteOffsets[i] = maskedDisplayOffset(&text, o)
		}
		visibleRangeOffset = NewInputSelection(maskedDisplayOffset(&text, visibleStartOffset), maskedDisplayOffset(&text, visibleEndOffset))
	}

	layout := &lastLayout{
		visibleRange:           visibleRange,
		visibleBufferLines:     visibleBufferLines,
		visibleLineByteOffsets: lineByteOffsets,
		visibleTop:             visibleTop,
		visibleRangeOffset:     visibleRangeOffset,
		lineHeight:             lineHeight,
		wrapWidth:              wrapWidth,
		wrappingIndent:         wrappingIndent,
		lineNumberWidth:        lineNumberWidth,
		spaceWidth:             spaceWidth,
		textAlign:              state.textAlign,
		contentWidth:           bounds.Size.Width,
	}

	run := gpui.TextRun{Len: displayText.Len(), Font: font, Color: textColor}
	markedUnderline := &gpui.UnderlineStyle{Thickness: 1, Color: &textColor}

	var marked *gpui.TextRange
	if state.imeMarkedRange != nil {
		marked = &gpui.TextRange{Start: state.imeMarkedRange.start, End: state.imeMarkedRange.end}
	}
	imeMarked := imeMarkedDisplayRange(&text, marked, state.masked)

	var runs []gpui.TextRun
	if !isEmpty && highlightStyles != nil {
		for _, h := range highlightStyles {
			r := textStyle.Highlight(h.Style).ToRun(h.Range.End - h.Range.Start)
			if disabled {
				r.Color = r.Color.Opacity(0.5)
			}
			runs = append(runs, splitRunForImeUnderline(r, h.Range, imeMarked, markedUnderline)...)
		}
	} else {
		runs = splitRunForImeUnderline(run, gpui.TextRange{Start: 0, End: displayText.Len()}, imeMarked, markedUnderline)
	}

	documentColors := state.extras.documentColorSwatches(text, visibleRange)
	indicators := layoutWhitespaceIndicators(state, textSize, textStyle, w)
	lines := layoutLines(state, &displayText, layout, textSize, runs, documentColors, indicators, w)

	var longestLineWidth float32
	if wrapWidth != nil {
		longestLineWidth = *wrapWidth
	}
	// A single line, or a multi-line input with soft wrap off.
	if state.IsSingleLine() || !state.softWrap {
		longestRow := state.displayMap.longestRow()
		longestLine := ext.SliceLine(longestRow)
		key := longestLineCache{
			documentRevision: state.documentRevision,
			row:              longestRow,
			length:           longestLine.Len(),
			font:             font,
			textSize:         textSize,
			wrapWidth:        wrapWidth,
		}
		cached := state.longestLineWidth
		state.longestLineWidth = nil
		if cached != nil && cached.sameKey(key) {
			longestLineWidth = cached.width
		} else {
			lineText := longestLine.String()
			longestLineWidth = state.displayTabs(w).shapeLine(w, lineText, 0, textSize, []gpui.TextRun{{
				Len: len(lineText), Font: font, Color: gpui.Black(),
			}}).Width()
		}
		key.width = longestLineWidth
		state.longestLineWidth = &key
	}
	if state.tokensVisible() {
		switch {
		case wrapWidth != nil:
			longestLineWidth = *wrapWidth
		case state.tokenLayoutCache != nil:
			longestLineWidth = state.tokenLayoutCache.unwrappedWidth
		default:
			longestLineWidth = 0
		}
	}
	layout.lines = lines

	ghostFirstLine, ghostLines := layoutInlineCompletion(state, layout.visibleRange, textSize, w)
	ghostLinesHeight := float32(len(ghostLines)) * lineHeight

	totalWrappedLines := state.displayMap.wrapRowCount()
	emptyBottom := emptyBottomHeight(state.IsCodeEditor(), state.scrollBeyondLastLine, bounds.Size.Height, lineHeight)

	scrollWidth := longestLineWidth
	if longestLineWidth+lineNumberWidth+inputRightMargin > bounds.Size.Width {
		scrollWidth = longestLineWidth + lineNumberWidth + inputRightMargin
	}
	// Empty bottom and ghost lines both describe extra height past the last
	// row, so take the max rather than summing.
	scrollSize := gpui.Size{
		Width:  scrollWidth,
		Height: max(float32(totalWrappedLines)*lineHeight+max(emptyBottom, ghostLinesHeight), bounds.Size.Height),
	}
	if layout.textAlign == gpui.TextAlignRight || layout.textAlign == gpui.TextAlignCenter {
		scrollSize.Width = longestLineWidth + lineNumberWidth
	}

	// Fold icons and their hitboxes use the unscrolled x, so they stay fixed
	// in the gutter during horizontal scroll.
	inputBounds := bounds
	originalX := bounds.Origin.X
	infos, cursorScrollOffset, currentRow, hasCurrentRow := e.layoutCursors(layout, &bounds, scrollSize, app)
	// Menus position at the active caret.
	for _, info := range infos {
		if info.isActive {
			b := info.bounds
			layout.cursorBounds = &b
			break
		}
	}

	searchMatchPaths := layoutSearchMatches(state, layout, bounds)
	selectionPaths := e.layoutSelections(layout, bounds, w, app)
	hoverHighlightPath := layoutHoverHighlight(state, layout, bounds)
	var documentColorPaths []coloredPath
	for _, dc := range documentColors {
		if path, ok := layoutMatchRange(dc.rng.Start, dc.rng.End, layout, bounds); ok {
			documentColorPaths = append(documentColorPaths, coloredPath{path, dc.color})
		}
	}
	scaleFactor, err := w.ScaleFactor()
	if err != nil || scaleFactor <= 0 {
		scaleFactor = 1
	}
	rangeFills, rangeFrames := layoutRangeDecorations(state, layout, bounds, scaleFactor, gpui.ContentMask(w))

	lineNumbers := layoutLineNumberLines(state, layout, lineNumberDigits, textSize, font, currentRow, hasCurrentRow, w)
	// U:input/base/element.rs:2810.
	var hoverDefinitionHitbox *gpui.Hitbox
	if hitbox, ok := state.hooks.hoverDefinitionHitbox(state, w, app); ok {
		hoverDefinitionHitbox = &hitbox
	}
	indentGuidesPath := layoutIndentGuides(state, bounds, layout, textStyle, w)
	_ = e.state.UpdateIn(app, w, func(s *InputBaseState[M], _ *gpui.Window, _ *gpui.Context[InputBaseState[M]]) {
		s.editorScrollbarSnapshot = &editorScrollbarSnapshot{
			layout:             newEditorScrollbarLayout(inputBounds, layout.lineNumberWidth, scrollSize, s.editorPaddings),
			cursorScrollOffset: cursorScrollOffset,
			softWrap:           s.softWrap,
		}
	})
	foldIcons := e.layoutFoldIcons(originalX, bounds, layout, w, app)

	hitbox := w.InsertHitbox(inputBounds, gpui.HitboxNormal)
	tokenElements := e.prepaintTokens(layout, bounds, measuredTokens, w, app)
	return textPrepaintState{
		lineNumbers:           lineNumbers,
		hoverHighlightPath:    hoverHighlightPath,
		searchMatchPaths:      searchMatchPaths,
		rangeDecorationFills:  rangeFills,
		rangeDecorationFrames: rangeFrames,
		indentGuidesPath:      indentGuidesPath,
		foldIconLayout:        foldIcons,
		ghostLines:            ghostLines,
		ghostFirstLine:        ghostFirstLine,
		ghostLinesHeight:      ghostLinesHeight,
		hoverDefinitionHitbox: hoverDefinitionHitbox,
		lastLayout:            layout,
		scrollSize:            scrollSize,
		cursorInfos:           infos,
		cursorScrollOffset:    cursorScrollOffset,
		currentRow:            currentRow,
		hasCurrentRow:         hasCurrentRow,
		selectionPaths:        selectionPaths,
		tokenElements:         tokenElements,
		documentColorPaths:    documentColorPaths,
		hitbox:                hitbox,
		bounds:                bounds,
	}
}

// Paint implements gpui.Element (U:input/base/element.rs:2852-3202).
func (e *textElement[M]) Paint(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, inputBounds gpui.Bounds, _ *struct{}, prepaint *textPrepaintState, w *gpui.Window, app *gpui.App) {
	state := e.read(app)
	focusHandle := state.focusHandle
	showCursor := state.showCursor(w, app)
	selected := state.activeSelection()
	editorStyle := state.editorStyle

	focused := focusHandle.IsFocused(w)
	bounds := prepaint.bounds
	layout := prepaint.lastLayout
	textAlign := layout.textAlign

	w.HandleInput(focusHandle, &inputElementHandler[M]{view: e.state, elementBounds: bounds})

	lineHeight := w.LineHeight()
	origin := bounds.Origin
	topPadding := layout.visibleTop
	disabled := state.disabled
	var activeLineColor *gpui.Hsla
	if editorStyle.EditorActiveLine != nil {
		c := *editorStyle.EditorActiveLine
		if disabled {
			c = c.Opacity(0.5)
		}
		activeLineColor = &c
	}
	editorBackground := editorStyle.Background
	if disabled {
		editorBackground = editorBackground.Opacity(0.5)
	}
	isCurrent := func(row int) bool { return prepaint.hasCurrentRow && prepaint.currentRow == row }

	// The active line background (U:input/base/element.rs:2897-2921).
	if prepaint.lineNumbers != nil {
		offsetY := topPadding
		for i, ln := range prepaint.lineNumbers {
			height := lineHeight * float32(ln.rows)
			if isCurrent(layout.visibleBufferLines[i]) && activeLineColor != nil {
				w.PaintQuad(gpui.Fill(gpui.Bounds{
					Origin: gpui.Point{X: inputBounds.Origin.X, Y: origin.Y + offsetY},
					Size:   gpui.Size{Width: bounds.Size.Width, Height: height},
				}, gpui.SolidBackground(*activeLineColor)))
			}
			offsetY += height
		}
	}

	// Keep the scrollbar offset positive, starting from the left.
	var scrollOffset float32
	switch textAlign {
	case gpui.TextAlignRight:
		scrollOffset = max(prepaint.scrollSize.Width-bounds.Size.Width, 0)
	case gpui.TextAlignCenter:
		scrollOffset = max((prepaint.scrollSize.Width-bounds.Size.Width)/2, 0)
	}
	contentWidth := layout.contentWidth

	// Glyph backgrounds first, so a highlighted range sits under the
	// selections and the text.
	offsetY := topPadding
	for i, line := range layout.lines {
		p := gpui.Point{X: origin.X + layout.lineNumberWidth + scrollOffset, Y: origin.Y + offsetY}
		line.paintBackground(p, lineHeight, textAlign, &contentWidth, w)
		offsetY += line.size(lineHeight).Height
		// Ghost lines shift every later line down.
		if i < len(layout.visibleBufferLines) && isCurrent(layout.visibleBufferLines[i]) {
			offsetY += prepaint.ghostLinesHeight
		}
	}

	// Indent guides.
	if prepaint.indentGuidesPath != nil {
		w.PaintPath(prepaint.indentGuidesPath, gpui.SolidBackground(editorStyle.Border.Opacity(0.85)))
	}

	// Application decorations sit below the user's selection.
	for _, cp := range prepaint.rangeDecorationFills {
		w.PaintPath(cp.path, gpui.SolidBackground(cp.color))
	}
	for _, cp := range prepaint.rangeDecorationFrames {
		w.PaintPath(cp.path, gpui.SolidBackground(cp.color))
	}

	// Search matches, selections, the hover highlight.
	if windowIsActive(w) {
		secondary := editorStyle.Selection
		secondary.S = 0.1
		for _, m := range prepaint.searchMatchPaths {
			w.PaintPath(m.path, gpui.SolidBackground(secondary))
			if m.active {
				w.PaintPath(m.path, gpui.SolidBackground(editorStyle.Selection))
			}
		}
		for _, path := range prepaint.selectionPaths {
			w.PaintPath(path, gpui.SolidBackground(editorStyle.Selection))
		}
		if prepaint.hoverHighlightPath != nil {
			w.PaintPath(prepaint.hoverHighlightPath, gpui.SolidBackground(secondary))
		}
	}

	// Document colors.
	for _, dc := range prepaint.documentColorPaths {
		c := dc.color
		if state.disabled {
			c = c.Opacity(0.5)
		}
		w.PaintPath(dc.path, gpui.SolidBackground(c))
	}

	// Text, with the inline-completion ghost lines after the cursor row.
	offsetY = topPadding
	cursorRowY, hasCursorRowY := float32(0), false
	for i, line := range layout.lines {
		lineY := origin.Y + offsetY
		p := gpui.Point{X: origin.X + layout.lineNumberWidth + scrollOffset, Y: lineY}
		line.paint(p, lineHeight, textAlign, &contentWidth, w)
		offsetY += line.size(lineHeight).Height
		if i >= len(layout.visibleBufferLines) || !isCurrent(layout.visibleBufferLines[i]) {
			continue
		}
		cursorRowY, hasCursorRowY = lineY, true
		for _, ghost := range prepaint.ghostLines {
			gp := gpui.Point{X: origin.X + layout.lineNumberWidth, Y: origin.Y + offsetY}
			w.PaintQuad(gpui.Fill(gpui.Bounds{Origin: gp, Size: gpui.Size{Width: bounds.Size.Width - layout.lineNumberWidth, Height: lineHeight}}, gpui.SolidBackground(editorBackground)))
			_ = ghost.Paint(gp, lineHeight, textAlign, &contentWidth, w)
			offsetY += lineHeight
		}
	}

	// Token elements paint over the text (U:input/base/element.rs:3076-3078).
	for _, element := range prepaint.tokenElements {
		element.Paint(w, app)
	}

	// Carets (one blink state for every caret).
	if focused && showCursor {
		for _, info := range prepaint.cursorInfosWithScroll() {
			w.PaintQuad(gpui.Fill(info.bounds, gpui.SolidBackground(editorStyle.Caret)))
		}
	}

	// Line numbers: a fixed overlay above horizontally scrolling text,
	// opaque over the whole gutter (U:input/base/element.rs:3087-3139).
	if prepaint.lineNumbers != nil {
		gutterBg := editorBackground
		if editorStyle.EditorGutterBackground != nil {
			gutterBg = *editorStyle.EditorGutterBackground
		}
		gutter := editorGutterBounds(inputBounds, layout.lineNumberWidth, prepaint.ghostLinesHeight, state.editorPaddings)
		w.PaintQuad(gpui.Fill(gutter, gpui.SolidBackground(gutterBg)))
		offsetY := topPadding
		for i, ln := range prepaint.lineNumbers {
			p := gpui.Point{X: inputBounds.Origin.X, Y: origin.Y + offsetY}
			height := lineHeight * float32(ln.rows)
			current := isCurrent(layout.visibleBufferLines[i])
			if current && activeLineColor != nil {
				w.PaintQuad(gpui.Fill(gpui.Bounds{
					Origin: gpui.Point{X: gutter.Origin.X, Y: p.Y},
					Size:   gpui.Size{Width: gutter.Size.Width, Height: height},
				}, gpui.SolidBackground(*activeLineColor)))
			}
			_ = ln.number.Paint(p, lineHeight, gpui.TextAlignLeft, nil, w)
			offsetY += height
			if len(prepaint.ghostLines) > 0 && current {
				offsetY += prepaint.ghostLinesHeight
			}
		}
	}

	// Fold icons: on gutter hover, the current line, or folded.
	e.paintFoldIcons(&prepaint.foldIconLayout, prepaint.currentRow, prepaint.hasCurrentRow, w, app)

	_ = e.state.UpdateIn(app, w, func(s *InputBaseState[M], w *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
		geometryChanged := s.lastBounds == nil || *s.lastBounds != bounds ||
			s.inputBounds != inputBounds ||
			s.scrollSize != prepaint.scrollSize ||
			s.lastLayout == nil ||
			!equalBoundsPtr(s.lastLayout.cursorBounds, layout.cursorBounds) ||
			s.lastLayout.lineHeight != layout.lineHeight
		s.lastLayout = layout.clone()
		b := bounds
		s.lastBounds = &b
		cursor := s.Cursor()
		s.lastCursor = &cursor
		s.setInputBounds(inputBounds, w, cx)
		sel := selected
		s.lastSelectedRange = &sel
		s.scrollSize = prepaint.scrollSize
		offset := prepaint.cursorScrollOffset
		s.updateScrollOffset(&offset, cx)
		s.deferredScrollOffset = nil

		// Layout consumers need changed geometry, not a notification for
		// every paint of an unchanged input.
		if geometryChanged {
			cx.Notify()
		}
	})

	// U:input/base/element.rs:3173-3177.
	if prepaint.hoverDefinitionHitbox != nil && !w.Modifiers().Alt {
		w.SetCursorStyle(gpui.CursorPointingHand, prepaint.hoverDefinitionHitbox)
	}

	// The inline completion's first line, after the cursor on its row.
	if focused && prepaint.ghostFirstLine != nil && hasCursorRowY {
		for _, info := range prepaint.cursorInfosWithScroll() {
			if !info.isActive {
				continue
			}
			p := gpui.Point{X: info.bounds.Origin.X + info.bounds.Size.Width, Y: cursorRowY}
			w.PaintQuad(gpui.Fill(gpui.Bounds{Origin: p, Size: gpui.Size{Width: prepaint.ghostFirstLine.Width() + 4, Height: lineHeight}}, gpui.SolidBackground(editorBackground)))
			_ = prepaint.ghostFirstLine.Paint(p, lineHeight, textAlign, nil, w)
			break
		}
	}

	e.paintMouseListeners(w)
}

func equalBoundsPtr(a, b *gpui.Bounds) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// placeholderLine is one display line of a placeholder with its runs.
type placeholderLine struct {
	line string
	runs []gpui.TextRun
}

// placeholderLineRuns splits placeholder text into lines and trims the
// runs to each (U:input/base/element.rs:3205-3225).
func placeholderLineRuns(displayText string, runs []gpui.TextRun) []placeholderLine {
	var result []placeholderLine
	lineOffset := 0
	for _, line := range strings.Split(displayText, "\n") {
		result = append(result, placeholderLine{line: line, runs: runsForRange(runs, lineOffset, NewInputSelection(0, len(line)))})
		// Advance over the separator too.
		lineOffset += len(line) + 1
	}
	return result
}

// runsForRange is the runs of a wrapped line's byte range
// (U:input/base/element.rs:3227-3264).
func runsForRange(runs []gpui.TextRun, lineOffset int, rng InputSelection) []gpui.TextRun {
	var result []gpui.TextRun
	start, end := lineOffset+rng.Start, lineOffset+rng.End
	cursor := 0
	for _, run := range runs {
		runStart, runEnd := cursor, cursor+run.Len
		if runEnd <= start {
			cursor = runEnd
			continue
		}
		if runStart >= end {
			break
		}
		length := min(end, runEnd) - max(start, runStart)
		if length > 0 {
			r := run
			r.Len = length
			result = append(result, r)
		}
		cursor = runEnd
	}
	return result
}

// clipStylesToRanges clips sorted styles to sorted disjoint ranges
// (U:input/base/element.rs:3266-3292).
func clipStylesToRanges(styles []gpui.HighlightRange, ranges []gpui.TextRange) []gpui.HighlightRange {
	var result []gpui.HighlightRange
	rangeIx := 0
	for _, s := range styles {
		for rangeIx < len(ranges) && ranges[rangeIx].End <= s.Range.Start {
			rangeIx++
		}
		for ix := rangeIx; ix < len(ranges) && ranges[ix].Start < s.Range.End; ix++ {
			start, end := max(s.Range.Start, ranges[ix].Start), min(s.Range.End, ranges[ix].End)
			if start < end {
				result = append(result, gpui.HighlightRange{Range: gpui.TextRange{Start: start, End: end}, Style: s.Style})
			}
		}
	}
	return result
}

// alignRunsToCharBoundaries snaps run ends to char boundaries and caps
// them at the text length; ok is false when the runs are already aligned
// (U:input/base/element.rs:3294-3329).
func alignRunsToCharBoundaries(text string, runs []gpui.TextRun) ([]gpui.TextRun, bool) {
	isBoundary := func(i int) bool { return i == len(text) || (i < len(text) && utf8.RuneStart(text[i])) }
	end := 0
	aligned := true
	for _, run := range runs {
		end += run.Len
		if end > len(text) || !isBoundary(end) {
			aligned = false
			break
		}
	}
	if aligned {
		return nil, false
	}

	result := make([]gpui.TextRun, 0, len(runs))
	cursor, rawEnd := 0, 0
	for _, run := range runs {
		rawEnd = min(rawEnd+run.Len, len(text))
		end := rawEnd
		for !isBoundary(end) {
			end++
		}
		if end > cursor {
			r := run
			r.Len = end - cursor
			result = append(result, r)
			cursor = end
		}
	}
	return result, true
}

// splitRunForImeUnderline underlines the part of a run inside the IME
// marked range (U:input/base/element.rs:3331-3369).
func splitRunForImeUnderline(run gpui.TextRun, runRange gpui.TextRange, marked *gpui.TextRange, underline *gpui.UnderlineStyle) []gpui.TextRun {
	if run.Len == 0 {
		return nil
	}
	if marked == nil {
		return []gpui.TextRun{run}
	}
	start, end := max(runRange.Start, marked.Start), min(runRange.End, marked.End)
	if start >= end {
		return []gpui.TextRun{run}
	}
	before, inside, after := run, run, run
	before.Len = start - runRange.Start
	inside.Len = end - start
	inside.Underline = underline
	after.Len = runRange.End - end
	var out []gpui.TextRun
	for _, r := range []gpui.TextRun{before, inside, after} {
		if r.Len > 0 {
			out = append(out, r)
		}
	}
	return out
}

// hasBackground reports a run painting a glyph background
// (U:input/base/element.rs:3371-3375).
func hasBackground(runs []gpui.TextRun) bool {
	for _, r := range runs {
		if r.BackgroundColor != nil {
			return true
		}
	}
	return false
}

// splitRunsByBgSegments recolors the parts of runs over document-color
// swatches for contrast (U:input/base/element.rs:3377-3437).
func splitRunsByBgSegments(startOffset int, runs []gpui.TextRun, segments []inputColoredRange) []gpui.TextRun {
	var result []gpui.TextRun
	cursor := startOffset
	with := func(run gpui.TextRun, length int) gpui.TextRun {
		run.Len = length
		return run
	}
	for _, run := range runs {
		runStart := cursor
		runEnd := cursor + run.Len
		for _, seg := range segments {
			if runEnd <= seg.rng.Start || runStart >= seg.rng.End {
				continue
			}
			if runStart < seg.rng.Start {
				result = append(result, with(run, seg.rng.Start-runStart))
			}
			overlapStart, overlapEnd := max(runStart, seg.rng.Start), min(runEnd, seg.rng.End)
			textColor := gpui.White()
			if seg.color.L >= 0.5 {
				textColor = gpui.Black()
			}
			if n := max(overlapEnd-overlapStart, 0); n > 0 {
				r := with(run, n)
				r.Color = textColor
				result = append(result, r)
				cursor = seg.rng.End
				runStart = cursor
			}
		}
		if runEnd > cursor {
			result = append(result, with(run, runEnd-cursor))
		}
		cursor = runEnd
	}
	return result
}

// sameKey compares the cache keys (LongestLineKey's PartialEq,
// U:input/base/element.rs:126-134).
func (c *longestLineCache) sameKey(o longestLineCache) bool {
	return c.documentRevision == o.documentRevision && c.row == o.row && c.length == o.length &&
		c.textSize == o.textSize && reflect.DeepEqual(c.font, o.font) && equalF32Ptr(c.wrapWidth, o.wrapWidth)
}

func equalF32Ptr(a, b *float32) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
