package base

// Port of U:input/base/layout.rs (LastLayout, WhitespaceIndicators), plus
// the two display-map pieces the engine reads it through:
//
//   - inputLineLayout is the display map's LineLayout
//     (U:input/editor/display_map/text_wrapper.rs:635-987) over the
//     shaped-text variant of InputLine
//     (U:input/editor/display_map/inline_line.rs:12-35). The inline-token
//     variant (InputLine::Inline) joins with the inline-tokens port.
//   - inputDisplayMap is a stand-in for U:input/editor/display_map: the
//     map of an unwrapped, unfolded document, where every buffer line is
//     one display row. It answers the queries the engine makes with those
//     identities; the textarea ticket replaces it with the wrapping,
//     folding DisplayMap.
//
// Geometry goes through the ticket-10 legacy text facade
// (gpui.ShapedLine x<->index; map.md gap 2): XForIndex,
// ClosestIndexForX, IndexForX, Width, Len, Paint, PaintBackground.

import (
	"sort"
	"unicode/utf8"

	"github.com/hoangvu12/gpui-go/base/rope"
	"github.com/hoangvu12/gpui-go/gpui"
)

// whitespaceIndicators are the shaped space and tab markers
// (U:input/base/layout.rs:7-11).
type whitespaceIndicators struct {
	space *gpui.ShapedLine
	tab   *gpui.ShapedLine
}

// lastLayout is what the last paint laid out (U:input/base/layout.rs:13-33).
type lastLayout struct {
	visibleRange           InputSelection
	visibleBufferLines     []int
	visibleLineByteOffsets []int
	visibleTop             float32
	visibleRangeOffset     InputSelection
	lines                  []*inputLineLayout
	lineHeight             float32
	wrapWidth              *float32
	lineNumberWidth        float32
	// spaceWidth is the width of one space in the editor font, the step a
	// pointer past the end of a line is measured in
	// (U:input/base/layout.rs:25-29).
	spaceWidth   float32
	cursorBounds *gpui.Bounds
	textAlign    gpui.TextAlign
	contentWidth float32
}

// line returns the layout of a visible buffer row
// (U:input/base/layout.rs:36-39).
func (l *lastLayout) line(row int) (*inputLineLayout, bool) {
	pos := sort.SearchInts(l.visibleBufferLines, row)
	if pos >= len(l.visibleBufferLines) || l.visibleBufferLines[pos] != row || pos >= len(l.lines) {
		return nil, false
	}
	return l.lines[pos], true
}

// alignmentOffset is U:input/base/layout.rs:41-47.
func (l *lastLayout) alignmentOffset(lineWidth float32) float32 {
	switch l.textAlign {
	case gpui.TextAlignCenter:
		return max((l.contentWidth-lineWidth)/2, 0)
	case gpui.TextAlignRight:
		return max(l.contentWidth-lineWidth, 0)
	}
	return 0
}

// clone copies the layout (`#[derive(Clone)]`; lines are shared like the
// Rc<Vec<LineLayout>>).
func (l *lastLayout) clone() *lastLayout {
	out := *l
	return &out
}

// inputLineLayout is one buffer line with its soft-wrapped visual lines
// (U:input/editor/display_map/text_wrapper.rs:635-651).
type inputLineLayout struct {
	// length is the total byte length of the line.
	length       int
	wrappedLines []*gpui.ShapedLine
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

// newInputLineLayout is LineLayout::new + lines
// (U:input/editor/display_map/text_wrapper.rs:654-703).
func newInputLineLayout(wrappedLines []*gpui.ShapedLine) *inputLineLayout {
	l := &inputLineLayout{wrappedLines: wrappedLines}
	for _, line := range wrappedLines {
		l.length += line.Len()
		l.longestWidth = max(l.longestWidth, line.Width())
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
		for offset, c := range wrapped.Text {
			if c != ' ' && c != '\t' {
				continue
			}
			startX := wrapped.XForIndex(offset)
			endX := wrapped.XForIndex(offset + utf8.RuneLen(c))
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
		case line.Len() == 0:
			// Empty visual lines still own their boundary offset.
			matches = offset == accLen
		case isLast || lineEndAffinity:
			// Inclusive: the cursor can sit at the end of this line.
			matches = offset >= accLen && offset <= accLen+line.Len()
		default:
			// Exclusive: the boundary belongs to the next visual line.
			matches = offset >= accLen && offset < accLen+line.Len()
		}
		if matches {
			x := line.XForIndex(max(offset-accLen, 0)) + xOffset + l.lineIndent(i)
			return gpui.Point{X: x, Y: offsetY}, true
		}
		// The last line gets +1 so the cursor fits after its final char.
		if isLast {
			accLen += line.Len() + 1
		} else {
			accLen += line.Len()
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
		if x <= indent+line.Width() {
			return accLen + line.ClosestIndexForX(x-indent)
		}
		accLen += line.Len()
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
		offset += line.Len()
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
	ix := line.ClosestIndexForX(x)
	lineEndAffinity := i+1 < len(l.wrappedLines) && ix == line.Len()
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
	pastEnd := x - l.wrappedLines[i].Width()
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
	ix, ok := l.wrappedLines[i].IndexForX(x)
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
			width = max(width, line.Width()+l.lineIndent(i))
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
		_ = line.PaintBackground(origin, lineHeight, align, alignWidth, w)
	}
}

// paint paints the lines and their whitespace markers
// (U:input/editor/display_map/text_wrapper.rs:949-986).
func (l *inputLineLayout) paint(pos gpui.Point, lineHeight float32, align gpui.TextAlign, alignWidth *float32, w *gpui.Window) {
	for i, line := range l.wrappedLines {
		origin := gpui.Point{X: pos.X + l.lineIndent(i), Y: pos.Y + float32(i)*lineHeight}
		_ = line.Paint(origin, lineHeight, align, alignWidth, w)
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

// roundHalfAway is f32::round (half away from zero).
func roundHalfAway(v float32) float32 {
	if v < 0 {
		return -float32(int(-v + 0.5))
	}
	return float32(int(v + 0.5))
}

// inputDisplayPoint is a wrap display point: the wrap row, the visual
// row within the buffer line, and the column
// (U:input/editor/display_map/text_wrapper.rs:619-633).
type inputDisplayPoint struct {
	row      int
	localRow int
	column   int
}

// inputDisplayMap is the stand-in for the display map of an unwrapped,
// unfolded document (see the file comment): one display row per buffer
// line. The engine calls the U:input/editor/display_map API names.
type inputDisplayMap struct {
	text rope.Rope
}

func (m *inputDisplayMap) ext() RopeExt { return RopeExtOf(&m.text) }

// setText tracks the document (the map's on_text_changed / set_text).
func (m *inputDisplayMap) setText(text rope.Rope) { m.text = text }

// wrapRowCount is the number of wrap rows: one per buffer line.
func (m *inputDisplayMap) wrapRowCount() int { return m.ext().LinesLen() }

// displayRowCount is the number of display rows (no folds).
func (m *inputDisplayMap) displayRowCount() int { return m.wrapRowCount() }

// bufferLineCount is the number of buffer lines.
func (m *inputDisplayMap) bufferLineCount() int { return m.wrapRowCount() }

// longestRow is the buffer row with the most bytes.
func (m *inputDisplayMap) longestRow() int {
	ext := m.ext()
	best, bestLen := 0, -1
	for row := range ext.LinesLen() {
		if n := ext.LineLen(row); n > bestLen {
			best, bestLen = row, n
		}
	}
	return best
}

// offsetToWrapDisplayPoint maps an offset to its display point.
func (m *inputDisplayMap) offsetToWrapDisplayPoint(offset int) inputDisplayPoint {
	p := m.ext().OffsetToPoint(offset)
	return inputDisplayPoint{row: p.Row, column: p.Column}
}

// offsetToWrapDisplayPointWithAffinity ignores the affinity: an unwrapped
// line has no wrap boundary to disambiguate.
func (m *inputDisplayMap) offsetToWrapDisplayPointWithAffinity(offset int, _ bool) inputDisplayPoint {
	return m.offsetToWrapDisplayPoint(offset)
}

// wrapDisplayPointToOffset maps a display point back to an offset.
func (m *inputDisplayMap) wrapDisplayPointToOffset(p inputDisplayPoint) int {
	ext := m.ext()
	row := min(p.row, max(ext.LinesLen()-1, 0))
	return ext.LineStartOffset(row) + min(p.column, ext.LineLen(row))
}

// wrapDisplayPointToPoint maps a display point to a buffer point.
func (m *inputDisplayMap) wrapDisplayPointToPoint(p inputDisplayPoint) InputPoint {
	return NewInputPoint(p.row, p.column)
}

// wrapRowToDisplayRow is the identity (no folds).
func (m *inputDisplayMap) wrapRowToDisplayRow(row int) (int, bool) { return row, true }

// displayRowToWrapRow is the identity (no folds).
func (m *inputDisplayMap) displayRowToWrapRow(row int) (int, bool) { return row, true }

// nearestVisibleDisplayRow is the identity (no folds).
func (m *inputDisplayMap) nearestVisibleDisplayRow(row int) int { return row }

// bufferLineToDisplayRow is the identity (no wraps, no folds).
func (m *inputDisplayMap) bufferLineToDisplayRow(row int) int { return row }

// bufferPosToDisplayPos maps a buffer point to its display point.
func (m *inputDisplayMap) bufferPosToDisplayPos(row, column int) (int, int) { return row, column }

// isBufferLineHidden is always false (no folds).
func (m *inputDisplayMap) isBufferLineHidden(int) bool { return false }

// displayRowToBufferLine is the identity (no wraps, no folds).
func (m *inputDisplayMap) displayRowToBufferLine(row int) int { return row }

// bufferLineToDisplayRowStart is the first display row of a buffer line:
// the line itself.
func (m *inputDisplayMap) bufferLineToDisplayRowStart(line int) (int, bool) {
	return line, line < m.bufferLineCount()
}

// visibleWrapRowCountForBufferLine is one row per existing line.
func (m *inputDisplayMap) visibleWrapRowCountForBufferLine(line int) int {
	if line < m.bufferLineCount() {
		return 1
	}
	return 0
}

// displayRowColumnToOffset maps a display row and column to an offset,
// clamped to the row's end.
func (m *inputDisplayMap) displayRowColumnToOffset(row, column int) int {
	return m.wrapDisplayPointToOffset(inputDisplayPoint{row: row, column: column})
}

// wrappedRanges is a buffer line's visual lines as local byte ranges: the
// whole line, unwrapped (the display map's LineItem.wrapped_lines).
func (m *inputDisplayMap) wrappedRanges(line int) []InputSelection {
	return []InputSelection{NewInputSelection(0, m.ext().LineLen(line))}
}
