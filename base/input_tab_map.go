package base

// Hard tabs in the input display (Base ticket 44): Zed's tab map rule.
//
// Upstream Base does nothing with a '\t' in the buffer: it shapes each
// visual row as is (U:input/base/element.rs:1867-1987) and lets the
// platform draw it. Its TabSize only drives what an indent inserts
// (U:input/editor/indent.rs:29-36) and the indent geometry, which
// already counts a tab as tab_size columns of one space each
// (indent.rs:38-49 indent_count; the guides at indent_width * column /
// tab_size, indent.rs:117-137). Under the pin a tab draws the font's
// missing-glyph box (gpui/text_tabs.go has the facts); under 0.2.2 it
// was DirectWrite's 4 em stop, which does not line up with those guides.
//
// GPUI's own editor, Zed, expands tabs before shaping: its display map
// puts a TabMap between folds and wraps (crates/editor/src/display_map/
// tab_map.rs, from Zed's source; no Zed tree is checked out here), which
// turns a tab at expanded column c into tab_size - c % tab_size columns
// of blank, columns counted in characters from the buffer line's start
// with earlier tabs expanded. That matches Base's own indent geometry, so
// the input display follows it: a tab spans its columns at the width of
// a space in the input font.
//
// Go adaptation: Zed rewrites the text (tabs become spaces) and maps
// every point between the buffer and the expanded text. Here a tab keeps
// its one byte: the row hands the shaper a space in a one-byte run of
// its own whose letter spacing widens it to its columns, so buffer
// offsets, byte columns, cursor moves, selections and hit tests need no
// mapping. The tab is one cluster: the caret steps over it at once (as
// Zed's clip_point keeps a cursor out of an expanded tab), and a click
// inside it lands on its nearer edge, as upstream Base's hit test did on
// 0.2.2's one wide DirectWrite tab cluster (Zed's click clips to the
// tab's start; recorded deviation). The wrapper measures a tab at the
// same width (gpui.LineFragmentTab). Columns are counted from the buffer
// line's start on every visual row, as Zed's tab map sits before its
// wrap map.

import (
	"strings"
	"unicode/utf8"

	"github.com/hoangvu12/gpui-go/gpui"
)

// inputTabs is the input display's tab rule: the tab size in columns and
// the advance of one column (a space in the input font).
type inputTabs struct {
	// size is the tab size in columns (TabSize.TabSize, at least 1).
	size int
	// spaceWidth is the advance of a space in font at the input's size.
	spaceWidth float32
	// font is the input font, the font a tab's blank is shaped in.
	font gpui.FontDescriptor
}

// newInputTabs builds the rule for a tab size; a size below 1 spans one
// column per tab.
func newInputTabs(size int, spaceWidth float32, font gpui.FontDescriptor) inputTabs {
	return inputTabs{size: max(size, 1), spaceWidth: spaceWidth, font: font}
}

// active reports whether the rule can expand tabs (a measured space).
func (t inputTabs) active() bool { return t.spaceWidth > 0 && t.size > 0 }

// span is the columns a tab at expanded column col covers (Zed's
// tab_size - column % tab_size).
func (t inputTabs) span(col int) int { return t.size - col%t.size }

// columnAfter is the expanded column after text that starts at column
// col: one column per character, a tab to the next tab stop.
func (t inputTabs) columnAfter(text string, col int) int {
	for _, c := range text {
		if c == '\t' {
			col += t.span(col)
		} else {
			col++
		}
	}
	return col
}

// tabWidth is the advance of a tab at expanded column col.
func (t inputTabs) tabWidth(col int) float32 { return float32(t.span(col)) * t.spaceWidth }

// expand rewrites a row that starts at expanded column col for the
// shaper: every tab becomes a space in a one-byte run cut from the run
// that held it, in the input font, whose letter spacing (on top of the
// run's own) makes it the tab's width. The text keeps its length, so the
// runs' byte ranges are unchanged. Runs that do not cover the text are
// returned as they are (the shaper reports them).
func (t inputTabs) expand(text string, col int, runs []gpui.TextRun) (string, []gpui.TextRun) {
	total := 0
	for _, run := range runs {
		total += run.Len
	}
	if total != len(text) {
		return text, runs
	}
	out := make([]gpui.TextRun, 0, len(runs)+2*strings.Count(text, "\t"))
	offset := 0
	for _, run := range runs {
		start, end := offset, offset+run.Len
		offset = end
		pieceStart := start
		for i := start; i < end; {
			c, size := utf8.DecodeRuneInString(text[i:end])
			if c != '\t' {
				col++
				i += size
				continue
			}
			if i > pieceStart {
				piece := run
				piece.Len = i - pieceStart
				out = append(out, piece)
			}
			var own float32
			if run.LetterSpacing != nil {
				own = *run.LetterSpacing
			}
			n := t.span(col)
			spacing := float32(n-1)*t.spaceWidth + own
			tab := run
			tab.Len = 1
			tab.Font = t.font
			tab.LetterSpacing = &spacing
			out = append(out, tab)
			col += n
			i++
			pieceStart = i
		}
		if end > pieceStart || run.Len == 0 {
			piece := run
			piece.Len = end - pieceStart
			out = append(out, piece)
		}
	}
	return strings.ReplaceAll(text, "\t", " "), out
}

// shapeLine shapes one visual row whose first character sits at expanded
// column col, its tabs drawn as blank columns. The shaped line reports
// the row's own text (tabs included), so whitespace markers still find
// the tabs.
func (t inputTabs) shapeLine(w *gpui.Window, text string, col int, fontSize float32, runs []gpui.TextRun) *gpui.ShapedLine {
	if !t.active() || !strings.Contains(text, "\t") {
		return mustShapeLine(w, text, fontSize, runs)
	}
	spaced, spacedRuns := t.expand(text, col, runs)
	line := mustShapeLine(w, spaced, fontSize, spacedRuns)
	line.Text = text
	return line
}

// wrapFragments splits a text fragment at its tabs for the wrapper: the
// text between tabs stays text, each tab becomes a LineFragmentTab of its
// width. col is the expanded column of the text's first character; the
// column after it is returned.
func (t inputTabs) wrapFragments(out []gpui.LineFragment, text string, col int) ([]gpui.LineFragment, int) {
	if !t.active() || !strings.Contains(text, "\t") {
		return append(out, gpui.LineFragmentText(text)), t.columnAfter(text, col)
	}
	start := 0
	for i, c := range text {
		if c != '\t' {
			col++
			continue
		}
		if i > start {
			out = append(out, gpui.LineFragmentText(text[start:i]))
		}
		out = append(out, gpui.LineFragmentTab(t.tabWidth(col)))
		col += t.span(col)
		start = i + 1
	}
	if start < len(text) {
		out = append(out, gpui.LineFragmentText(text[start:]))
	}
	return out, col
}

// displayTabs is the state's tab rule for the window's input font: its
// mode's tab size (TabSize.TabSize; single-line and other modes take the
// default) and the font's space, measured by the display map.
func (s *InputBaseState[M]) displayTabs(w *gpui.Window) inputTabs {
	return s.displayMap.wrap.wrapper.tabsFor(w)
}
