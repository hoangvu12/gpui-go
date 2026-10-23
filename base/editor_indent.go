package base

// Port of U:input/editor/indent.rs: the tab size, indent guides and the
// indent / outdent actions.
//
// Go adaptations: the methods restricted to one mode upstream (the
// EditorMode indent-guide builders, the MultiLineMode tab size) exist on
// every instantiation and panic on the wrong mode (input_kind.go).

import (
	"slices"
	"sort"
	"strings"

	"github.com/hoangvu12/gpui-go/base/rope"
	"github.com/hoangvu12/gpui-go/gpui"
)

// TabSize is the indentation unit (U:input/editor/indent.rs:12-18).
type TabSize struct {
	// TabSize is the indent width in columns; default 2.
	TabSize int
	// HardTabs indents with `\t`; default false.
	HardTabs bool
}

// DefaultTabSize is `impl Default for TabSize` (U:input/editor/indent.rs:20-27).
func DefaultTabSize() TabSize { return TabSize{TabSize: 2} }

// String is TabSize::to_string: one indent level
// (U:input/editor/indent.rs:30-36).
func (t TabSize) String() string {
	if t.HardTabs {
		return "\t"
	}
	return strings.Repeat(" ", t.TabSize)
}

// IndentCount counts the line's indentation in spaces, a tab counting as
// TabSize (U:input/editor/indent.rs:38-49).
func (t TabSize) IndentCount(line rope.RopeSlice) int {
	count := 0
	it := line.Chars()
	for c, ok := it.Next(); ok; c, ok = it.Next() {
		switch c {
		case '\t':
			count += t.TabSize
		case ' ':
			count++
		default:
			return count
		}
	}
	return count
}

// isIndentable is U:input/editor/indent.rs:53-63; callers gate it on a
// multi-line input.
func (m *layoutMode) isIndentable() bool {
	return m.kind == layoutPlainText || m.kind == layoutCodeEditor
}

// hasIndentGuides is U:input/editor/indent.rs:65-71.
func (m *layoutMode) hasIndentGuides() bool { return m.kind == layoutCodeEditor && m.indentGuides }

// tabSize is U:input/editor/indent.rs:73-80.
func (m *layoutMode) tabSize() TabSize {
	if m.kind == layoutPlainText || m.kind == layoutCodeEditor {
		return m.tab
	}
	return DefaultTabSize()
}

// measureIndentWidth is the width of column spaces in the text style's
// font (U:input/editor/indent.rs:84-103).
func measureIndentWidth(style gpui.TextStyle, column int, w *gpui.Window) float32 {
	fontSize := style.FontSizePixels(gpui.RemSize(w))
	text := strings.Repeat(" ", column)
	return mustShapeLine(w, text, fontSize, []gpui.TextRun{{Len: column, Font: style.FontDescriptorOf()}}).Width()
}

// layoutIndentGuides strokes one guide per indent level of every visible
// line; a blank line continues the guides above it
// (U:input/editor/indent.rs:105-164).
func layoutIndentGuides[M InputModeKind](state *InputBaseState[M], bounds gpui.Bounds, layout *lastLayout, textStyle gpui.TextStyle, w *gpui.Window) *gpui.Path {
	if !state.IsMultiLine() || !state.mode.hasIndentGuides() {
		return nil
	}
	tab := state.mode.tabSize()
	if tab.TabSize <= 0 {
		return nil
	}
	indentWidth := measureIndentWidth(textStyle, tab.TabSize, w)
	lineHeight := layout.lineHeight
	b := gpui.StrokePathBuilder(1)
	offsetY := layout.visibleTop
	var lastIndents []float32
	ext := state.ext()
	for i, bufferLine := range layout.visibleBufferLines {
		if i >= len(layout.lines) {
			break
		}
		line := ext.SliceLine(bufferLine)
		var current []float32
		if line.Len() > 0 {
			indentCount := tab.IndentCount(line)
			for offset := 0; offset < indentCount; offset += tab.TabSize {
				x := indentWidth * float32(offset) / float32(tab.TabSize)
				px, py := x+layout.lineNumberWidth, offsetY
				b.MoveTo(px, py)
				b.LineTo(px, py+lineHeight)
				current = append(current, px)
			}
		} else if len(lastIndents) > 0 {
			for _, x := range lastIndents {
				b.MoveTo(x, offsetY)
				b.LineTo(x, offsetY+lineHeight)
			}
			current = slices.Clone(lastIndents)
		}
		offsetY += float32(len(layout.lines[i].wrappedLines)) * lineHeight
		lastIndents = current
	}
	b.Translate(bounds.Origin.X, bounds.Origin.Y)
	path, err := b.Build()
	if err != nil {
		// An empty builder has nothing to stroke.
		return nil
	}
	return path
}

// IndentGuides sets whether to show indent guides, default true
// (U:input/editor/indent.rs:169-179, doc(hidden)). EditorState only.
func (s InputBaseState[M]) IndentGuides(indentGuides bool) InputBaseState[M] {
	s.requireEditorMode("IndentGuides")
	s.mode.indentGuides = indentGuides
	return s
}

// SetIndentGuides is U:input/editor/indent.rs:181-195. EditorState only.
func (s *InputBaseState[M]) SetIndentGuides(indentGuides bool, _ *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
	s.requireEditorMode("SetIndentGuides")
	s.mode.indentGuides = indentGuides
	cx.Notify()
}

// indentInline is Tab: accept an inline completion, else indent
// (U:input/editor/indent.rs:199-210).
func (s *InputBaseState[M]) indentInline(_ *IndentInline, w *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
	if s.hooks.acceptInlineCompletion(s, w, cx) {
		return
	}
	s.indent(false, w, cx)
}

// indentBlock is U:input/editor/indent.rs:212-214.
func (s *InputBaseState[M]) indentBlock(_ *Indent, w *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
	s.indent(true, w, cx)
}

// outdentInline is U:input/editor/indent.rs:216-223.
func (s *InputBaseState[M]) outdentInline(_ *OutdentInline, w *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
	s.outdent(false, w, cx)
}

// outdentBlock is U:input/editor/indent.rs:225-232.
func (s *InputBaseState[M]) outdentBlock(_ *Outdent, w *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
	s.outdent(true, w, cx)
}

// indent is U:input/editor/indent.rs:234-236.
func (s *InputBaseState[M]) indent(block bool, w *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
	s.applyIndent(indentDirectionIndent, block, w, cx)
}

// outdent is U:input/editor/indent.rs:238-240.
func (s *InputBaseState[M]) outdent(block bool, w *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
	s.applyIndent(indentDirectionOutdent, block, w, cx)
}

// indentDirection is IndentDirection (U:input/editor/indent.rs:439-443).
type indentDirection uint8

const (
	indentDirectionIndent indentDirection = iota
	indentDirectionOutdent
)

// applyIndent indents or outdents every selection as one batch edit, one
// undo transaction restoring the multi-selection extents
// (U:input/editor/indent.rs:242-287).
func (s *InputBaseState[M]) applyIndent(direction indentDirection, block bool, w *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
	if !s.IsEditable() || !s.IsMultiLine() || !s.mode.isIndentable() {
		cx.Propagate()
		return
	}
	tabIndent := s.mode.tabSize().String()
	tabLen := len(tabIndent)

	// Non-collapsed selections and explicit block operations indent whole
	// lines.
	useBlock := block
	for sel := range s.selections.iter() {
		if !sel.isCollapsed() {
			useBlock = true
		}
	}
	before := slices.Collect(s.selections.iter())

	var edits []inputTextEdit
	var next []cursorSelection
	if useBlock {
		edits, next = s.computeBlockIndent(direction, tabIndent, tabLen)
	} else {
		edits, next = s.computeInlineIndent(direction, tabLen)
	}
	if len(edits) == 0 {
		return
	}

	s.undoManager.beginTransaction()
	s.replaceTextInRanges(edits, w, cx)
	s.selections.replaceAll(next)
	s.undoManager.recordSelections(before, slices.Collect(s.selections.iter()))
	s.undoManager.commitTransaction()

	s.scrollTo(s.Cursor(), nil, cx)
	cx.Notify()
}

// lineStartsWithIndent is `text.slice(start..).chars().take(n).eq(indent)`.
func (s *InputBaseState[M]) lineStartsWithIndent(start int, indent string) bool {
	return s.textAfterMatches(start, indent)
}

// computeBlockIndent builds the per-line edits and resulting selections of
// a block indent/outdent (U:input/editor/indent.rs:289-357).
func (s *InputBaseState[M]) computeBlockIndent(direction indentDirection, tabIndent string, tabLen int) ([]inputTextEdit, []cursorSelection) {
	ext := s.ext()
	rowSet := map[int]bool{}
	for sel := range s.selections.iter() {
		startRow := ext.OffsetToPoint(sel.start).Row
		endRow := ext.OffsetToPoint(sel.end).Row
		for row := startRow; row <= endRow; row++ {
			rowSet[row] = true
		}
	}
	rows := make([]int, 0, len(rowSet))
	for row := range rowSet {
		rows = append(rows, row)
	}
	sort.Ints(rows)

	var edits []inputTextEdit
	for _, row := range rows {
		lineStart := ext.LineStartOffset(row)
		switch direction {
		case indentDirectionIndent:
			edits = append(edits, inputTextEdit{rng: NewInputSelection(lineStart, lineStart), text: tabIndent})
		case indentDirectionOutdent:
			if s.lineStartsWithIndent(lineStart, tabIndent) {
				edits = append(edits, inputTextEdit{rng: NewInputSelection(lineStart, lineStart+tabLen)})
			}
		}
	}

	// Map both endpoints through every earlier edit, including other
	// cursors'. A point inside removed indentation stays on its line.
	mapOffset := func(offset int) int {
		if direction == indentDirectionIndent {
			n := sort.Search(len(edits), func(i int) bool { return edits[i].rng.Start > offset })
			return offset + n*tabLen
		}
		preceding := sort.Search(len(edits), func(i int) bool { return edits[i].rng.End > offset })
		partial := 0
		if preceding < len(edits) {
			partial = max(offset-edits[preceding].rng.Start, 0)
		}
		return offset - preceding*tabLen - partial
	}
	var next []cursorSelection
	for sel := range s.selections.iter() {
		sel.start = mapOffset(sel.start)
		sel.end = mapOffset(sel.end)
		sel.columnAnchor = cursorColumnAnchor{}
		next = append(next, sel)
	}
	return edits, next
}

// computeInlineIndent builds the per-cursor edits and resulting cursors of
// a collapsed inline indent/outdent (U:input/editor/indent.rs:359-436).
func (s *InputBaseState[M]) computeInlineIndent(direction indentDirection, tabLen int) ([]inputTextEdit, []cursorSelection) {
	tabIndent := s.mode.tabSize().String()
	ext := s.ext()

	// The edit range for each cursor: an insertion point for indent, the
	// removed range for a removable outdent.
	var ranges []InputSelection
	for sel := range s.selections.iter() {
		cursor := sel.cursorOffset()
		switch direction {
		case indentDirectionIndent:
			ranges = append(ranges, NewInputSelection(cursor, cursor))
		case indentDirectionOutdent:
			start := ext.LineStartOffset(ext.OffsetToPoint(cursor).Row)
			if s.lineStartsWithIndent(start, tabIndent) {
				ranges = append(ranges, NewInputSelection(start, start+tabLen))
			}
		}
	}

	// Disjoint edits, dropping any that would overlap a previous one.
	sort.SliceStable(ranges, func(i, j int) bool { return ranges[i].Start < ranges[j].Start })
	var edits []inputTextEdit
	lastEnd, hasLast := 0, false
	for _, r := range ranges {
		if hasLast && r.Start < lastEnd {
			continue
		}
		lastEnd, hasLast = r.End, true
		text := ""
		if direction == indentDirectionIndent {
			text = tabIndent
		}
		edits = append(edits, inputTextEdit{rng: r, text: text})
	}

	// Shift every cursor by the surviving edits before (or at) it.
	var next []cursorSelection
	for sel := range s.selections.iter() {
		cursor := sel.cursorOffset()
		offset := cursor
		switch direction {
		case indentDirectionIndent:
			for _, e := range edits {
				if e.rng.Start <= cursor {
					offset += tabLen
				}
			}
		case indentDirectionOutdent:
			for _, e := range edits {
				offset -= min(e.rng.End, cursor) - min(e.rng.Start, cursor)
			}
		}
		next = append(next, newCursorSelection(sel.id, offset, offset))
	}
	return edits, next
}

// TabSize sets the tab size (U:input/editor/indent.rs:493-503,
// doc(hidden)). Multi-line modes only.
func (s InputBaseState[M]) TabSize(tab TabSize) InputBaseState[M] {
	s.requireMultiLineMode("TabSize")
	if s.mode.kind == layoutPlainText || s.mode.kind == layoutCodeEditor {
		s.mode.tab = tab
	}
	return s
}

// SetTabSize is U:input/editor/indent.rs:505-512. Multi-line modes only.
func (s *InputBaseState[M]) SetTabSize(tab TabSize, cx *gpui.Context[InputBaseState[M]]) {
	s.requireMultiLineMode("SetTabSize")
	if s.mode.kind == layoutPlainText || s.mode.kind == layoutCodeEditor {
		s.mode.tab = tab
	}
	cx.Notify()
}
