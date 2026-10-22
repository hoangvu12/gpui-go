package base

// Port of U:input/base/layout.rs: LastLayout and WhitespaceIndicators.
// The line layouts it holds are the display map's LineLayout
// (inputLineLayout, input_text_wrapper.go).
//
// Geometry goes through the ticket-10 legacy text facade
// (gpui.ShapedLine x<->index; map.md gap 2).

import (
	"sort"

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
	wrappingIndent         WrappingIndent
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

// roundHalfAway is f32::round (half away from zero).
func roundHalfAway(v float32) float32 {
	if v < 0 {
		return -float32(int(-v + 0.5))
	}
	return float32(int(v + 0.5))
}
