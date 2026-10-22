package base

// Port of U:input/editor/display_map/inline_line.rs: a visual row whose
// offsets stay in source UTF-8 bytes. A row is either one shaped line
// (Content::Text) or, when inline tokens show, a run of fragments: shaped
// text pieces and the fixed-width boxes of token elements
// (Content::Inline). The Rust enum is the fragments slice being non-nil.

import "github.com/hoangvu12/gpui-go/gpui"

// inlineFragment is one piece of an inline row: its local byte range, x,
// width, and the shaped text (nil for a token box)
// (U:input/editor/display_map/inline_line.rs:5-10).
type inlineFragment struct {
	rng   InputSelection
	x     float32
	width float32
	text  *gpui.ShapedLine
}

// inputLine is InputLine (U:input/editor/display_map/inline_line.rs:12-24).
type inputLine struct {
	len   int
	width float32
	text  string
	// shaped is Content::Text; fragments is Content::Inline.
	shaped    *gpui.ShapedLine
	fragments []inlineFragment
	inline    bool
}

// inputLineFromShaped is From<ShapedLine>
// (U:input/editor/display_map/inline_line.rs:25-34).
func inputLineFromShaped(line *gpui.ShapedLine) *inputLine {
	return &inputLine{len: line.Len(), width: line.Width(), text: line.Text, shaped: line}
}

// newInlineInputLine is InputLine::inline: the width ends at the last
// fragment (U:input/editor/display_map/inline_line.rs:36-44).
func newInlineInputLine(text string, fragments []inlineFragment) *inputLine {
	var width float32
	if n := len(fragments); n > 0 {
		width = fragments[n-1].x + fragments[n-1].width
	}
	return &inputLine{len: len(text), width: width, text: text, fragments: fragments, inline: true}
}

// xForIndex is U:input/editor/display_map/inline_line.rs:45-61: a token
// box is 0 at or before its start and its full width inside it.
func (l *inputLine) xForIndex(ix int) float32 {
	if !l.inline {
		return l.shaped.XForIndex(ix)
	}
	for _, f := range l.fragments {
		if ix < f.rng.End {
			if f.text != nil {
				return f.x + f.text.XForIndex(max(ix-f.rng.Start, 0))
			}
			if ix <= f.rng.Start {
				return f.x
			}
			return f.x + f.width
		}
	}
	return l.width
}

// closestIndexForX snaps inside a token box to its nearer edge
// (U:input/editor/display_map/inline_line.rs:62-81).
func (l *inputLine) closestIndexForX(x float32) int {
	if !l.inline {
		return l.shaped.ClosestIndexForX(x)
	}
	for _, f := range l.fragments {
		if x <= f.x+f.width {
			if f.text != nil {
				return f.rng.Start + f.text.ClosestIndexForX(x-f.x)
			}
			if x-f.x < f.width/2 {
				return f.rng.Start
			}
			return f.rng.End
		}
	}
	return l.len
}

// indexForX is U:input/editor/display_map/inline_line.rs:82-89.
func (l *inputLine) indexForX(x float32) (int, bool) {
	if !l.inline {
		return l.shaped.IndexForX(x)
	}
	if x >= 0 && x <= l.width {
		return l.closestIndexForX(x), true
	}
	return 0, false
}

// paint is U:input/editor/display_map/inline_line.rs:90-101.
func (l *inputLine) paint(pos gpui.Point, height float32, align gpui.TextAlign, width *float32, w *gpui.Window) {
	l.paintPass(pos, height, align, width, false, w)
}

// paintBackground is U:input/editor/display_map/inline_line.rs:102-113.
func (l *inputLine) paintBackground(pos gpui.Point, height float32, align gpui.TextAlign, width *float32, w *gpui.Window) {
	l.paintPass(pos, height, align, width, true, w)
}

// paintPass paints the text pieces; an inline row aligns itself and paints
// its pieces left-aligned (U:input/editor/display_map/inline_line.rs:114-155).
func (l *inputLine) paintPass(pos gpui.Point, height float32, align gpui.TextAlign, width *float32, background bool, w *gpui.Window) {
	paint := func(line *gpui.ShapedLine, pos gpui.Point, align gpui.TextAlign, width *float32) {
		if background {
			_ = line.PaintBackground(pos, height, align, width, w)
		} else {
			_ = line.Paint(pos, height, align, width, w)
		}
	}
	if !l.inline {
		paint(l.shaped, pos, align, width)
		return
	}
	available := l.width
	if width != nil {
		available = *width
	}
	remaining := max(available-l.width, 0)
	var offset float32
	switch align {
	case gpui.TextAlignRight:
		offset = remaining
	case gpui.TextAlignCenter:
		offset = remaining / 2
	}
	for _, f := range l.fragments {
		if f.text != nil {
			paint(f.text, gpui.Point{X: pos.X + offset + f.x, Y: pos.Y}, gpui.TextAlignLeft, nil)
		}
	}
}
