package base

// Port of U:input/editor/display_map/text_wrapper.rs: the soft-wrap rows
// of every buffer line (TextWrapper over a SumTree of LineItem) and the
// layout of one painted buffer line (LineLayout).
//
// Go adaptations:
//
//   - The core pin has no LineWrapper (Parley wraps inside layout_text);
//     wrapping goes through the ticket-10 legacy text facade
//     (gpui/line_wrapper.go: WindowTextSystem.LineWrapper, WrapLine,
//     LineFragmentText / LineFragmentElement, Boundary). Upstream reaches
//     the text system through `cx: &mut App` (cx.text_system()); the Go
//     text system belongs to a window, so the re-wrapping methods take the
//     window instead. The wrapper is cached per font and size like the
//     legacy pool (text_system.rs:332). A re-wrap with no window and no
//     cached wrapper leaves the lines unwrapped; a wrap width is only ever
//     set from a paint, which has the window, so this only happens for
//     edits made before the first paint, which the paint re-wraps.
//   - Ranges are InputSelection (Start..End); Option<Pixels> is *float32.
//   - The SumTree is base/sumtree; tree.cursor::<D>() is sumtree.NewCursor[D].

import (
	"slices"
	"sort"
	"unicode/utf8"

	"github.com/hoangvu12/gpui-go/base/rope"
	"github.com/hoangvu12/gpui-go/base/sumtree"
	"github.com/hoangvu12/gpui-go/gpui"
)

// WrappingIndent controls how soft-wrapped continuation lines are indented
// (U:input/editor/display_map/text_wrapper.rs:19-27). The zero value is the
// upstream default, WrappingIndentSame.
type WrappingIndent uint8

const (
	// WrappingIndentSame keeps the first line's indentation on
	// continuation lines (the default).
	WrappingIndentSame WrappingIndent = iota
	// WrappingIndentNone starts continuation lines flush-left at the full
	// editor width.
	WrappingIndentNone
)

// lineItem is one buffer line with its soft-wrapped visual lines
// (U:input/editor/display_map/text_wrapper.rs:29-57).
type lineItem struct {
	// length is the byte length of the line, without the `\n`.
	length int
	// indent is the leading characters reserved as indentation for
	// continuation lines under WrappingIndentSame; 0 otherwise.
	indent uint32
	// wrappedLines are the visual lines' local byte ranges (0..length),
	// first line included.
	wrappedLines []InputSelection
}

// len is U:input/editor/display_map/text_wrapper.rs:46-50.
func (l lineItem) len() int { return l.length }

// linesLen is U:input/editor/display_map/text_wrapper.rs:52-56.
func (l lineItem) linesLen() int { return len(l.wrappedLines) }

// lineSummary summarizes a subtree of lineItems
// (U:input/editor/display_map/text_wrapper.rs:59-97).
type lineSummary struct {
	bufferRows int
	wrapRows   int
	bytes      int
	maxLineLen int
	longestRow int
}

// AddSummary keeps the leftmost row of a strictly greater length
// (U:input/editor/display_map/text_wrapper.rs:87-96).
func (s lineSummary) AddSummary(o lineSummary) lineSummary {
	if o.maxLineLen > s.maxLineLen {
		s.longestRow = s.bufferRows + o.longestRow
		s.maxLineLen = o.maxLineLen
	}
	s.bufferRows += o.bufferRows
	s.wrapRows += o.wrapRows
	s.bytes += o.bytes
	return s
}

// Summary is U:input/editor/display_map/text_wrapper.rs:99-111.
func (l lineItem) Summary() lineSummary {
	return lineSummary{bufferRows: 1, wrapRows: l.linesLen(), bytes: l.len(), maxLineLen: l.len()}
}

// wrapBufferRows is the BufferRows cursor dimension
// (U:input/editor/display_map/text_wrapper.rs:113-125).
type wrapBufferRows int

func (b wrapBufferRows) AddSummary(s lineSummary) wrapBufferRows {
	return b + wrapBufferRows(s.bufferRows)
}

func (b wrapBufferRows) Cmp(o wrapBufferRows) int { return compareInts(int(b), int(o)) }

// wrapWrapRows is the WrapRows cursor dimension
// (U:input/editor/display_map/text_wrapper.rs:127-139).
type wrapWrapRows int

func (w wrapWrapRows) AddSummary(s lineSummary) wrapWrapRows { return w + wrapWrapRows(s.wrapRows) }

func (w wrapWrapRows) Cmp(o wrapWrapRows) int { return compareInts(int(w), int(o)) }

type (
	wrapLines   = sumtree.SumTree[lineItem, lineSummary]
	bufThenWrap = sumtree.Dimensions[lineSummary, wrapBufferRows, wrapWrapRows]
	wrapThenBuf = sumtree.Dimensions[lineSummary, wrapWrapRows, wrapBufferRows]
)

func seekBufferRow(row int) sumtree.SeekTarget[bufThenWrap] {
	return sumtree.FirstDimension[lineSummary, wrapBufferRows, wrapWrapRows](wrapBufferRows(row))
}

func seekWrapRow(row int) sumtree.SeekTarget[wrapThenBuf] {
	return sumtree.FirstDimension[lineSummary, wrapWrapRows, wrapBufferRows](wrapWrapRows(row))
}

// inlineMetric is one (token range, width) of the inline metrics the
// wrapper reserves for inline token elements.
type inlineMetric struct {
	rng   InputSelection
	width float32
}

// wrapLineFunc is the wrap_line closure: the boundaries of a line of text
// starting at lineStart in the document.
type wrapLineFunc func(line string, wrapWidth float32, lineStart int) []gpui.Boundary

// textWrapper prepares the soft-wrapped lines the editor displays
// (U:input/editor/display_map/text_wrapper.rs:141-156).
type textWrapper struct {
	text     rope.Rope
	font     gpui.FontDescriptor
	fontSize float32
	// wrapWidth nil means the text is not wrapped.
	wrapWidth      *float32
	wrappingIndent WrappingIndent
	lines          wrapLines

	inlineMetrics []inlineMetric
	initialized   bool

	// lineWrapper caches the facade wrapper for font and fontSize.
	lineWrapper *gpui.LineWrapper
}

// newTextWrapper is TextWrapper::new
// (U:input/editor/display_map/text_wrapper.rs:160-171).
func newTextWrapper(font gpui.FontDescriptor, fontSize float32, wrapWidth *float32) textWrapper {
	return textWrapper{font: font, fontSize: fontSize, wrapWidth: wrapWidth}
}

// setDefaultText is U:input/editor/display_map/text_wrapper.rs:173-176.
func (t *textWrapper) setDefaultText(text rope.Rope) { t.text = text }

// textRope is text() (U:input/editor/display_map/text_wrapper.rs:178-182).
func (t *textWrapper) textRope() *rope.Rope { return &t.text }

// len is the number of wrap rows
// (U:input/editor/display_map/text_wrapper.rs:184-188).
func (t *textWrapper) len() int { return t.lines.Summary().wrapRows }

// linesCount is the number of buffer lines
// (U:input/editor/display_map/text_wrapper.rs:190-194).
func (t *textWrapper) linesCount() int { return t.lines.Summary().bufferRows }

// longestRow is the row of the longest line by bytes
// (U:input/editor/display_map/text_wrapper.rs:196-200).
func (t *textWrapper) longestRow() int { return t.lines.Summary().longestRow }

// line is the line item of a buffer row
// (U:input/editor/display_map/text_wrapper.rs:202-208).
func (t *textWrapper) line(row int) (lineItem, bool) {
	c := sumtree.NewCursor[wrapBufferRows](t.lines)
	c.Seek(wrapBufferRows(row), sumtree.Right)
	return c.Item()
}

// iterLines is U:input/editor/display_map/text_wrapper.rs:210-214.
func (t *textWrapper) iterLines() []lineItem { return t.lines.Items() }

// bufferLineToFirstWrapRow is the total wrap row count past the last row
// (U:input/editor/display_map/text_wrapper.rs:216-222).
func (t *textWrapper) bufferLineToFirstWrapRow(row int) int {
	c := sumtree.NewCursor[bufThenWrap](t.lines)
	c.Seek(seekBufferRow(row), sumtree.Right)
	return int(c.Start().V1)
}

// bufferLineToWrapRowRange is U:input/editor/display_map/text_wrapper.rs:224-231.
func (t *textWrapper) bufferLineToWrapRowRange(row int) InputSelection {
	c := sumtree.NewCursor[bufThenWrap](t.lines)
	c.Seek(seekBufferRow(row), sumtree.Right)
	start := int(c.Start().V1)
	n := 0
	if item, ok := c.Item(); ok {
		n = item.linesLen()
	}
	return NewInputSelection(start, start+n)
}

// wrapRowToBufferLine clamps to the last line
// (U:input/editor/display_map/text_wrapper.rs:233-241).
func (t *textWrapper) wrapRowToBufferLine(wrapRow int) int {
	c := sumtree.NewCursor[wrapThenBuf](t.lines)
	c.Seek(seekWrapRow(wrapRow), sumtree.Right)
	if _, ok := c.Item(); ok {
		return int(c.Start().V1)
	}
	return max(t.linesCount()-1, 0)
}

// setWrapWidth is U:input/editor/display_map/text_wrapper.rs:243-250.
func (t *textWrapper) setWrapWidth(wrapWidth *float32, w *gpui.Window) {
	if equalF32Ptr(wrapWidth, t.wrapWidth) {
		return
	}
	if wrapWidth != nil {
		width := *wrapWidth
		wrapWidth = &width
	}
	t.wrapWidth = wrapWidth
	t.updateAll(t.text, w)
}

// setWrappingIndent is U:input/editor/display_map/text_wrapper.rs:252-259.
func (t *textWrapper) setWrappingIndent(indent WrappingIndent, w *gpui.Window) {
	if indent == t.wrappingIndent {
		return
	}
	t.wrappingIndent = indent
	t.updateAll(t.text, w)
}

// setFont is U:input/editor/display_map/text_wrapper.rs:261-269.
func (t *textWrapper) setFont(font gpui.FontDescriptor, fontSize float32, w *gpui.Window) {
	if fontDescriptorEqual(t.font, font) && t.fontSize == fontSize {
		return
	}
	t.font = font
	t.fontSize = fontSize
	t.lineWrapper = nil
	t.updateAll(t.text, w)
}

// prepareIfNeed is U:input/editor/display_map/text_wrapper.rs:271-278.
func (t *textWrapper) prepareIfNeed(text rope.Rope, w *gpui.Window) bool {
	if t.initialized {
		return false
	}
	t.initialized = true
	t.updateAll(text, w)
	return true
}

// facadeWrapper returns the cached legacy-facade wrapper, creating it
// from the window's text system (cx.text_system().line_wrapper(font, size)).
func (t *textWrapper) facadeWrapper(w *gpui.Window) *gpui.LineWrapper {
	if t.lineWrapper == nil && w != nil {
		if lw, err := w.TextSystem().LineWrapper(t.font, t.fontSize); err == nil {
			t.lineWrapper = lw
		}
	}
	return t.lineWrapper
}

// update re-wraps the lines an edit touched
// (U:input/editor/display_map/text_wrapper.rs:280-338): changedText is the
// new document, rng the replaced range of the old one, newText the
// inserted text. Inline-token metrics become element fragments.
func (t *textWrapper) update(changedText rope.Rope, rng InputSelection, newText rope.Rope, w *gpui.Window) {
	lineWrapper := t.facadeWrapper(w)
	metrics := t.inlineMetrics
	t.updateWith(changedText, rng, newText, func(line string, wrapWidth float32, lineStart int) []gpui.Boundary {
		if lineWrapper == nil {
			return nil
		}
		var fragments []gpui.LineFragment
		offset := 0
		first := sort.Search(len(metrics), func(i int) bool { return metrics[i].rng.End > lineStart })
		for _, m := range metrics[first:] {
			if m.rng.Start >= lineStart+len(line) {
				break
			}
			if m.rng.Start < lineStart || m.rng.End > lineStart+len(line) {
				continue
			}
			start, end := m.rng.Start-lineStart, m.rng.End-lineStart
			if !isCharBoundaryStr(line, start) || !isCharBoundaryStr(line, end) {
				continue
			}
			if offset < start {
				fragments = append(fragments, gpui.LineFragmentText(line[offset:start]))
			}
			fragments = append(fragments, gpui.LineFragmentElement(m.width, end-start))
			offset = end
		}
		if len(fragments) == 0 {
			return lineWrapper.WrapLine([]gpui.LineFragment{gpui.LineFragmentText(line)}, wrapWidth)
		}
		if offset < len(line) {
			fragments = append(fragments, gpui.LineFragmentText(line[offset:]))
		}
		return lineWrapper.WrapLine(fragments, wrapWidth)
	})
}

// isCharBoundaryStr is str::is_char_boundary.
func isCharBoundaryStr(s string, i int) bool {
	if i == 0 || i == len(s) {
		return true
	}
	if i < 0 || i > len(s) {
		return false
	}
	return utf8.RuneStart(s[i])
}

// adjustInlineMetrics drops the metrics an edit overlaps and shifts those
// after it (U:input/editor/display_map/text_wrapper.rs:340-360).
func (t *textWrapper) adjustInlineMetrics(rng InputSelection, newLen int) {
	if len(t.inlineMetrics) == 0 {
		return
	}
	shift := newLen - rng.Len()
	out := make([]inlineMetric, 0, len(t.inlineMetrics))
	for _, m := range t.inlineMetrics {
		if m.rng.Start < rng.End && rng.Start < m.rng.End {
			continue
		}
		if m.rng.Start >= rng.End {
			if m.rng.Start+shift < 0 || m.rng.End+shift < 0 {
				continue
			}
			m.rng = NewInputSelection(m.rng.Start+shift, m.rng.End+shift)
		}
		out = append(out, m)
	}
	t.inlineMetrics = out
}

// setInlineMetrics re-wraps only the rows whose element geometry changed
// (U:input/editor/display_map/text_wrapper.rs:362-415).
func (t *textWrapper) setInlineMetrics(metrics []inlineMetric, w *gpui.Window) {
	if slices.Equal(t.inlineMetrics, metrics) {
		return
	}
	var affected []InputSelection
	oldM, newM := t.inlineMetrics, metrics
	i, j := 0, 0
	for i < len(oldM) || j < len(newM) {
		switch {
		case i < len(oldM) && j < len(newM) && oldM[i] == newM[j]:
			i++
			j++
		case i < len(oldM) && j < len(newM) && oldM[i].rng.Start <= newM[j].rng.Start:
			affected = append(affected, oldM[i].rng)
			i++
		case j < len(newM):
			affected = append(affected, newM[j].rng)
			j++
		default:
			affected = append(affected, oldM[i].rng)
			i++
		}
	}
	t.inlineMetrics = slices.Clone(metrics)
	text := t.text
	ext := RopeExtOf(&text)
	rows := make([]int, 0, len(affected))
	for _, r := range affected {
		rows = append(rows, ext.OffsetToPoint(min(r.Start, text.Len())).Row)
	}
	slices.Sort(rows)
	rows = slices.Compact(rows)
	for _, row := range rows {
		start, end := ext.LineStartOffset(row), ext.LineEndOffset(row)
		t.update(text, NewInputSelection(start, end), rope.FromSlice(text.Slice(start, end)), w)
	}
}

// updateWith is _update: replace the old changed rows with the wrapped
// rows of the new text (U:input/editor/display_map/text_wrapper.rs:417-515).
func (t *textWrapper) updateWith(changedText rope.Rope, rng InputSelection, newText rope.Rope, wrapLine wrapLineFunc) {
	oldExt := RopeExtOf(&t.text)
	bufferLineCount := t.linesCount()
	startRow := min(oldExt.OffsetToPoint(rng.Start).Row, max(bufferLineCount-1, 0))
	endRow := min(oldExt.OffsetToPoint(rng.End).Row, max(bufferLineCount-1, 0))

	newExt := RopeExtOf(&changedText)
	newStartRow := newExt.OffsetToPoint(rng.Start).Row
	newEndRow := newExt.OffsetToPoint(rng.Start + newText.Len()).Row

	newLines := make([]lineItem, 0, max(newEndRow-newStartRow, 0)+1)
	wrapWidth := t.wrapWidth
	for row := newStartRow; row <= newEndRow; row++ {
		line := newExt.SliceLine(row)
		lineLen := line.Len()
		var wrapped []InputSelection
		prevBoundary := 0
		var indentChars uint32

		if wrapWidth != nil {
			lineStr := line.String()
			lineStart := newExt.LineStartOffset(row)
			switch t.wrappingIndent {
			case WrappingIndentSame:
				// Only wrapped lines here: no wrap yields no boundary.
				for _, b := range wrapLine(lineStr, *wrapWidth, lineStart) {
					wrapped = append(wrapped, NewInputSelection(prevBoundary, b.Ix))
					prevBoundary = b.Ix
					indentChars = b.NextIndent
				}
			case WrappingIndentNone:
				// The first visual line keeps the line's leading
				// indentation, so it is wrapped as is.
				if boundaries := wrapLine(lineStr, *wrapWidth, lineStart); len(boundaries) > 0 {
					firstIx := boundaries[0].Ix
					wrapped = append(wrapped, NewInputSelection(prevBoundary, firstIx))
					prevBoundary = firstIx
					for _, b := range wrapLine(lineStr[firstIx:], *wrapWidth, lineStart+firstIx) {
						ix := firstIx + b.Ix
						wrapped = append(wrapped, NewInputSelection(prevBoundary, ix))
						prevBoundary = ix
					}
				}
			}
		}

		// The rest of the line.
		if prevBoundary < lineLen || prevBoundary == 0 {
			wrapped = append(wrapped, NewInputSelection(prevBoundary, lineLen))
		}
		newLines = append(newLines, lineItem{length: lineLen, indent: indentChars, wrappedLines: wrapped})
	}

	if t.lines.IsEmpty() {
		t.lines = sumtree.FromIter[lineItem](newLines)
	} else {
		c := sumtree.NewCursor[wrapBufferRows](t.lines)
		tree := c.Slice(wrapBufferRows(startRow), sumtree.Right)
		// Skip the replaced rows.
		c.SeekForward(wrapBufferRows(endRow+1), sumtree.Right)
		tree.Extend(newLines)
		// Untouched rows after the edit.
		tree.Append(c.Suffix())
		t.lines = tree
	}
	t.text = changedText
}

// updateAll re-wraps the whole text
// (U:input/editor/display_map/text_wrapper.rs:517-522).
func (t *textWrapper) updateAll(text rope.Rope, w *gpui.Window) {
	t.update(text, NewInputSelection(0, text.Len()), text, w)
}

// offsetToDisplayPoint is U:input/editor/display_map/text_wrapper.rs:524-529.
func (t *textWrapper) offsetToDisplayPoint(offset int) wrapDisplayPoint {
	return t.offsetToDisplayPointWithAffinity(offset, false)
}

// offsetToDisplayPointWithAffinity honours the caret's line-end affinity:
// a soft wrap boundary closes the current row instead of opening the next
// (U:input/editor/display_map/text_wrapper.rs:531-572).
func (t *textWrapper) offsetToDisplayPointWithAffinity(offset int, lineEndAffinity bool) wrapDisplayPoint {
	ext := RopeExtOf(&t.text)
	row := ext.OffsetToPoint(offset).Row
	start := ext.LineStartOffset(row)

	c := sumtree.NewCursor[bufThenWrap](t.lines)
	c.Seek(seekBufferRow(row), sumtree.Right)
	wrappedRow := int(c.Start().V1)
	line, ok := c.Item()
	if !ok {
		return newWrapDisplayPoint(wrappedRow, 0, 0)
	}

	localOffset := max(offset-start, 0)
	for ix, r := range line.wrappedLines {
		if r.Contains(localOffset) || (lineEndAffinity && localOffset == r.End) {
			return newWrapDisplayPoint(wrappedRow+ix, ix, max(localOffset-r.Start, 0))
		}
	}
	// Otherwise the end of the line.
	last := NewInputSelection(0, 0)
	if n := len(line.wrappedLines); n > 0 {
		last = line.wrappedLines[n-1]
	}
	ix := max(line.linesLen()-1, 0)
	return newWrapDisplayPoint(wrappedRow+ix, ix, last.Len())
}

// displayPointToOffset is U:input/editor/display_map/text_wrapper.rs:574-595.
func (t *textWrapper) displayPointToOffset(p wrapDisplayPoint) int {
	c := sumtree.NewCursor[wrapThenBuf](t.lines)
	c.Seek(seekWrapRow(p.row), sumtree.Right)
	line, ok := c.Item()
	if !ok {
		return t.text.Len()
	}
	wrappedRow := int(c.Start().V0)
	row := int(c.Start().V1)

	lineStart := RopeExtOf(&t.text).LineStartOffset(row)
	localRow := max(p.row-wrappedRow, 0)
	if localRow < len(line.wrappedLines) {
		r := line.wrappedLines[localRow]
		return lineStart + min(r.Start+p.column, r.End)
	}
	return lineStart + line.len()
}

// displayPointToPoint is U:input/editor/display_map/text_wrapper.rs:597-600.
func (t *textWrapper) displayPointToPoint(p wrapDisplayPoint) InputPoint {
	return RopeExtOf(&t.text).OffsetToPoint(t.displayPointToOffset(p))
}

// pointToDisplayPoint is U:input/editor/display_map/text_wrapper.rs:602-605.
func (t *textWrapper) pointToDisplayPoint(p InputPoint) wrapDisplayPoint {
	return t.offsetToDisplayPoint(RopeExtOf(&t.text).PointToOffset(p))
}

// wrapDisplayPoint is a position after soft wrapping: the wrap row, the
// visual row within its buffer line (only meaningful when returned from
// offsetToDisplayPoint) and the byte column in the visual line
// (U:input/editor/display_map/text_wrapper.rs:608-633).
type wrapDisplayPoint struct {
	row      int
	localRow int
	column   int
}

// newWrapDisplayPoint is WrapDisplayPoint::new.
func newWrapDisplayPoint(row, localRow, column int) wrapDisplayPoint {
	return wrapDisplayPoint{row: row, localRow: localRow, column: column}
}

// fontDescriptorEqual is Font's PartialEq.
func fontDescriptorEqual(a, b gpui.FontDescriptor) bool {
	return a.Family == b.Family && a.Weight == b.Weight && a.Style == b.Style &&
		slices.Equal(a.Features, b.Features) && slices.Equal(a.Fallbacks, b.Fallbacks)
}

// inputLineLayout is one buffer line with its soft-wrapped visual lines
// (U:input/editor/display_map/text_wrapper.rs:635-651).
type inputLineLayout struct {
	// length is the total byte length of the line.
	length       int
	wrappedLines []*inputLine
	// wrapIndent is the extra left offset of continuation lines.
	wrapIndent           float32
	longestWidth         float32
	whitespaceIndicators *whitespaceIndicators
	// whitespaceChars are (line index, x, is tab) markers.
	whitespaceChars []whitespaceChar
	hasBackground   bool
}

type whitespaceChar struct {
	lineIndex int
	x         float32
	isTab     bool
}

// newInputLineLayout is LineLayout::new().lines(..)
// (U:input/editor/display_map/text_wrapper.rs:653-703).
func newInputLineLayout(wrappedLines []*gpui.ShapedLine) *inputLineLayout {
	l := &inputLineLayout{}
	l.setWrappedLines(wrappedLines)
	return l
}

// setWrappedLines is U:input/editor/display_map/text_wrapper.rs:694-703.
func (l *inputLineLayout) setWrappedLines(wrappedLines []*gpui.ShapedLine) {
	l.length, l.longestWidth = 0, 0
	l.wrappedLines = make([]*inputLine, 0, len(wrappedLines))
	for _, line := range wrappedLines {
		l.length += line.Len()
		l.longestWidth = max(l.longestWidth, line.Width())
		l.wrappedLines = append(l.wrappedLines, inputLineFromShaped(line))
	}
}

// newInlineLineLayout is LineLayout::new().inline_lines(..)
// (U:input/editor/display_map/text_wrapper.rs:705-714).
func newInlineLineLayout(lines []*inputLine) *inputLineLayout {
	l := &inputLineLayout{wrappedLines: lines}
	for _, line := range lines {
		l.length += line.len
		l.longestWidth = max(l.longestWidth, line.width)
	}
	return l
}

// withBackground is U:input/editor/display_map/text_wrapper.rs:666-670.
func (l *inputLineLayout) withBackground(has bool) *inputLineLayout {
	l.hasBackground = has
	return l
}

// withWrapIndent is U:input/editor/display_map/text_wrapper.rs:672-676.
func (l *inputLineLayout) withWrapIndent(indent float32) *inputLineLayout {
	l.wrapIndent = indent
	return l
}

// withWhitespaces records the whitespace marker positions
// (U:input/editor/display_map/text_wrapper.rs:716-742).
func (l *inputLineLayout) withWhitespaces(indicators *whitespaceIndicators) *inputLineLayout {
	l.whitespaceIndicators = indicators
	if indicators == nil {
		return l
	}
	spaceIndicatorOffset := indicators.space.Width() / 2
	for lineIndex, wrapped := range l.wrappedLines {
		for offset, c := range wrapped.text {
			if c != ' ' && c != '\t' {
				continue
			}
			startX := wrapped.xForIndex(offset)
			endX := wrapped.xForIndex(offset + utf8.RuneLen(c))
			x := startX
			if c == ' ' {
				// Center the indicator in the character's space.
				x = (startX+endX)/2 - spaceIndicatorOffset
			}
			l.whitespaceChars = append(l.whitespaceChars, whitespaceChar{lineIndex: lineIndex, x: x, isTab: c == '\t'})
		}
	}
	return l
}

// lineIndent is the indent of a visual line: only continuation lines are
// indented (U:input/editor/display_map/text_wrapper.rs:678-687).
func (l *inputLineLayout) lineIndent(lineIndex int) float32 {
	if lineIndex == 0 {
		return 0
	}
	return l.wrapIndent
}

// len is the line's byte length (U:input/editor/display_map/text_wrapper.rs:744-747).
func (l *inputLineLayout) len() int { return l.length }

// positionForIndex returns the position of a local byte index relative to
// the line layout's top-left corner. With lineEndAffinity an offset on a
// soft wrap boundary stays at the end of the current visual line
// (U:input/editor/display_map/text_wrapper.rs:749-794).
func (l *inputLineLayout) positionForIndex(offset int, layout *lastLayout, lineEndAffinity bool) (gpui.Point, bool) {
	accLen := 0
	offsetY := float32(0)
	xOffset := layout.alignmentOffset(l.longestWidth)
	for i, line := range l.wrappedLines {
		isLast := i+1 == len(l.wrappedLines)
		var matches bool
		switch {
		case line.len == 0:
			// Empty visual lines still own their boundary offset.
			matches = offset == accLen
		case isLast || lineEndAffinity:
			// Inclusive: the cursor can sit at the end of this line.
			matches = offset >= accLen && offset <= accLen+line.len
		default:
			// Exclusive: the boundary belongs to the next visual line.
			matches = offset >= accLen && offset < accLen+line.len
		}
		if matches {
			x := line.xForIndex(max(offset-accLen, 0)) + xOffset + l.lineIndent(i)
			return gpui.Point{X: x, Y: offsetY}, true
		}
		// The last line gets +1 so the cursor fits after its final char.
		if isLast {
			accLen += line.len + 1
		} else {
			accLen += line.len
		}
		offsetY += layout.lineHeight
	}
	return gpui.Point{}, false
}

// closestIndexForX returns the closest index for x, ignoring y: only
// meaningful for a layout known to occupy one visual line
// (U:input/editor/display_map/text_wrapper.rs:796-815).
func (l *inputLineLayout) closestIndexForX(x float32, layout *lastLayout) int {
	accLen := 0
	x -= layout.alignmentOffset(l.longestWidth)
	for i, line := range l.wrappedLines {
		indent := l.lineIndent(i)
		if x <= indent+line.width {
			return accLen + line.closestIndexForX(x-indent)
		}
		accLen += line.len
	}
	return accLen
}

// wrappedLineAt resolves pos to the wrapped sub-line under it: its index,
// the byte offset it starts at, and pos.X in its own coordinate space
// (U:input/editor/display_map/text_wrapper.rs:817-841).
func (l *inputLineLayout) wrappedLineAt(pos gpui.Point, layout *lastLayout) (int, int, float32, bool) {
	offset := 0
	lineTop := float32(0)
	xOffset := layout.alignmentOffset(l.longestWidth)
	for i, line := range l.wrappedLines {
		lineBottom := lineTop + layout.lineHeight
		if pos.Y >= lineTop && pos.Y < lineBottom {
			return i, offset, pos.X - xOffset - l.lineIndent(i), true
		}
		offset += line.len
		lineTop = lineBottom
	}
	return 0, 0, 0, false
}

// closestIndexForPosition returns the local byte index under pos with the
// caret affinity to use for it
// (U:input/editor/display_map/text_wrapper.rs:843-864).
func (l *inputLineLayout) closestIndexForPosition(pos gpui.Point, layout *lastLayout) (int, bool, bool) {
	i, offset, x, ok := l.wrappedLineAt(pos, layout)
	if !ok {
		return 0, false, false
	}
	line := l.wrappedLines[i]
	ix := line.closestIndexForX(x)
	lineEndAffinity := i+1 < len(l.wrappedLines) && ix == line.len
	return offset + ix, lineEndAffinity, true
}

// columnsPastLineEnd is how many whole spaces pos sits past the end of the
// line under it (U:input/editor/display_map/text_wrapper.rs:866-895).
func (l *inputLineLayout) columnsPastLineEnd(pos gpui.Point, layout *lastLayout) int {
	i, _, x, ok := l.wrappedLineAt(pos, layout)
	if !ok {
		return 0
	}
	if i+1 < len(l.wrappedLines) || layout.spaceWidth <= 0 {
		return 0
	}
	pastEnd := x - l.wrappedLines[i].width
	if pastEnd <= 0 {
		return 0
	}
	return int(roundHalfAway(pastEnd / layout.spaceWidth))
}

// indexForPosition is U:input/editor/display_map/text_wrapper.rs:897-905.
func (l *inputLineLayout) indexForPosition(pos gpui.Point, layout *lastLayout) (int, bool) {
	i, offset, x, ok := l.wrappedLineAt(pos, layout)
	if !ok {
		return 0, false
	}
	ix, ok := l.wrappedLines[i].indexForX(x)
	if !ok {
		return 0, false
	}
	return offset + ix, true
}

// size is U:input/editor/display_map/text_wrapper.rs:907-916.
func (l *inputLineLayout) size(lineHeight float32) gpui.Size {
	width := l.longestWidth
	if len(l.wrappedLines) > 0 {
		width = 0
		for i, line := range l.wrappedLines {
			width = max(width, line.width+l.lineIndent(i))
		}
	}
	return gpui.Size{Width: width, Height: float32(len(l.wrappedLines)) * lineHeight}
}

// paintBackground paints only the glyph background quads; a line without
// a background run skips the pass
// (U:input/editor/display_map/text_wrapper.rs:918-947).
func (l *inputLineLayout) paintBackground(pos gpui.Point, lineHeight float32, align gpui.TextAlign, alignWidth *float32, w *gpui.Window) {
	if !l.hasBackground {
		return
	}
	for i, line := range l.wrappedLines {
		origin := gpui.Point{X: pos.X + l.lineIndent(i), Y: pos.Y + float32(i)*lineHeight}
		line.paintBackground(origin, lineHeight, align, alignWidth, w)
	}
}

// paint paints the lines and their whitespace markers
// (U:input/editor/display_map/text_wrapper.rs:949-986).
func (l *inputLineLayout) paint(pos gpui.Point, lineHeight float32, align gpui.TextAlign, alignWidth *float32, w *gpui.Window) {
	for i, line := range l.wrappedLines {
		origin := gpui.Point{X: pos.X + l.lineIndent(i), Y: pos.Y + float32(i)*lineHeight}
		line.paint(origin, lineHeight, align, alignWidth, w)
	}
	if l.whitespaceIndicators == nil {
		return
	}
	for _, ws := range l.whitespaceChars {
		invisible := l.whitespaceIndicators.space
		if ws.isTab {
			invisible = l.whitespaceIndicators.tab
		}
		origin := gpui.Point{
			X: pos.X + ws.x + l.lineIndent(ws.lineIndex),
			Y: pos.Y + float32(ws.lineIndex)*lineHeight,
		}
		_ = invisible.Paint(origin, lineHeight, align, alignWidth, w)
	}
}
