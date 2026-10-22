package base

// Port of U:input/editor/display_map/display_map.rs and mod.rs: the
// public facade of the layered display mapping
//
//	Buffer (rope) → wrapMap (soft wrap) → foldMap (folding) → display rows
//
// The engine works with buffer and display positions only. The point
// types are pub upstream but no Go caller outside the engine needs them,
// so they stay unexported; Option<T> results are (T, bool) and methods
// that re-wrap take the window in place of `cx: &mut App`
// (input_text_wrapper.go).

import (
	"strings"

	"github.com/hoangvu12/gpui-go/base/rope"
	"github.com/hoangvu12/gpui-go/gpui"
)

// bufferPoint is a logical line and byte column
// (U:input/editor/display_map/mod.rs:25-39).
type bufferPoint struct{ line, col int }

// wrapPoint is a position after soft wrapping, before folding
// (U:input/editor/display_map/mod.rs:41-52).
type wrapPoint struct{ row, col int }

// displayPoint is the final visible row and column
// (U:input/editor/display_map/mod.rs:54-68).
type displayPoint struct{ row, col int }

// inputDisplayMap is DisplayMap (U:input/editor/display_map/display_map.rs:
// 29-32): the wrap map and the fold map.
type inputDisplayMap struct {
	wrap  wrapMap
	folds foldMap
}

// newInputDisplayMap is DisplayMap::new
// (U:input/editor/display_map/display_map.rs:35-40).
func newInputDisplayMap(font gpui.FontDescriptor, fontSize float32, wrapWidth *float32) inputDisplayMap {
	return inputDisplayMap{wrap: newWrapMap(font, fontSize, wrapWidth), folds: newFoldMap()}
}

// bufferPointToDisplayPoint is buffer_pos_to_display_pos: a folded
// position lands at column 0 of the nearest visible row
// (U:input/editor/display_map/display_map.rs:45-57).
func (m *inputDisplayMap) bufferPointToDisplayPoint(pos bufferPoint) displayPoint {
	wp := m.wrap.bufferPosToWrapPos(pos)
	if row, ok := m.folds.wrapRowToDisplayRow(wp.row); ok {
		return displayPoint{row: row, col: wp.col}
	}
	return displayPoint{row: m.folds.nearestVisibleDisplayRow(wp.row)}
}

// bufferPosToDisplayPos is bufferPointToDisplayPoint over (row, column).
func (m *inputDisplayMap) bufferPosToDisplayPos(row, column int) (int, int) {
	p := m.bufferPointToDisplayPoint(bufferPoint{line: row, col: column})
	return p.row, p.col
}

// displayPosToBufferPos is U:input/editor/display_map/display_map.rs:60-67.
func (m *inputDisplayMap) displayPosToBufferPos(pos displayPoint) bufferPoint {
	wrapRow, _ := m.folds.displayRowToWrapRow(pos.row)
	return m.wrap.wrapPosToBufferPos(wrapPoint{row: wrapRow, col: pos.col})
}

// displayRowCount is U:input/editor/display_map/display_map.rs:70-73.
func (m *inputDisplayMap) displayRowCount() int { return m.folds.displayRowCount() }

// displayRowToBufferLine is U:input/editor/display_map/display_map.rs:76-85.
func (m *inputDisplayMap) displayRowToBufferLine(displayRow int) int {
	wrapRow, _ := m.folds.displayRowToWrapRow(displayRow)
	return m.wrap.wrapRowToBufferLine(wrapRow)
}

// bufferLineToDisplayRowRange is false when the line is completely hidden
// (U:input/editor/display_map/display_map.rs:89-111).
func (m *inputDisplayMap) bufferLineToDisplayRowRange(line int) (InputSelection, bool) {
	r := m.wrap.bufferLineToWrapRowRange(line)
	first, last, found := 0, 0, false
	for wr := r.Start; wr < r.End; wr++ {
		if dr, ok := m.folds.wrapRowToDisplayRow(wr); ok {
			if !found {
				first = dr
				found = true
			}
			last = dr
		}
	}
	if !found {
		return InputSelection{}, false
	}
	return NewInputSelection(first, last+1), true
}

// bufferLineToDisplayRowStart is the start of bufferLineToDisplayRowRange.
func (m *inputDisplayMap) bufferLineToDisplayRowStart(line int) (int, bool) {
	r, ok := m.bufferLineToDisplayRowRange(line)
	return r.Start, ok
}

// isBufferLineHidden is U:input/editor/display_map/display_map.rs:115-117.
func (m *inputDisplayMap) isBufferLineHidden(line int) bool {
	_, ok := m.bufferLineToDisplayRowRange(line)
	return !ok
}

// bufferLineToDisplayRow is the first display row of a line, or the
// nearest visible one when it is folded
// (U:input/editor/display_map/display_map.rs:121-129).
func (m *inputDisplayMap) bufferLineToDisplayRow(line int) int {
	if r, ok := m.bufferLineToDisplayRowRange(line); ok {
		return r.Start
	}
	return m.folds.nearestVisibleDisplayRow(m.wrap.bufferLineToFirstWrapRow(line))
}

// setFoldCandidates is U:input/editor/display_map/display_map.rs:132-135.
func (m *inputDisplayMap) setFoldCandidates(candidates []FoldRange) {
	m.folds.setCandidates(candidates)
	m.rebuildFoldProjection()
}

// setFolded is U:input/editor/display_map/display_map.rs:138-141.
func (m *inputDisplayMap) setFolded(startLine int, folded bool) {
	m.folds.setFolded(startLine, folded)
	m.rebuildFoldProjection()
}

// toggleFold is U:input/editor/display_map/display_map.rs:144-147.
func (m *inputDisplayMap) toggleFold(startLine int) {
	m.folds.toggleFold(startLine)
	m.rebuildFoldProjection()
}

// isFoldedAt is U:input/editor/display_map/display_map.rs:151-153.
func (m *inputDisplayMap) isFoldedAt(startLine int) bool { return m.folds.isFoldedAt(startLine) }

// isFoldCandidate is U:input/editor/display_map/display_map.rs:157-159.
func (m *inputDisplayMap) isFoldCandidate(startLine int) bool {
	return m.folds.isFoldCandidate(startLine)
}

// foldedRanges is U:input/editor/display_map/display_map.rs:163-165.
func (m *inputDisplayMap) foldedRanges() []FoldRange { return m.folds.foldedRanges() }

// clearFolds is U:input/editor/display_map/display_map.rs:168-171.
func (m *inputDisplayMap) clearFolds() {
	m.folds.clearFolds()
	m.rebuildFoldProjection()
}

// adjustFoldsForEdit adjusts folds and candidates for an edit, called
// with the old text before the wrap map updates
// (U:input/editor/display_map/display_map.rs:179-193).
func (m *inputDisplayMap) adjustFoldsForEdit(oldText rope.Rope, rng InputSelection, newText string) {
	if len(m.folds.foldedRanges()) == 0 && len(m.folds.foldCandidates()) == 0 {
		return
	}
	ext := RopeExtOf(&oldText)
	editStartLine := ext.OffsetToPoint(rng.Start).Row
	editEndLine := ext.OffsetToPoint(min(rng.End, oldText.Len())).Row
	oldLines := max(editEndLine-editStartLine, 0)
	newLines := strings.Count(newText, "\n")
	m.folds.adjustFoldsForEdit(editStartLine, editEndLine, newLines-oldLines)
}

// updateFoldCandidatesForEdit merges the candidates extracted from the
// edited byte range (U:input/editor/display_map/display_map.rs:199-213).
func (m *inputDisplayMap) updateFoldCandidatesForEdit(extract func(InputSelection, *rope.Rope) []FoldRange, editRange InputSelection, newText rope.Rope) {
	ext := RopeExtOf(&newText)
	startLine := ext.OffsetToPoint(editRange.Start).Row
	endLine := ext.OffsetToPoint(min(editRange.End, newText.Len())).Row
	m.folds.mergeCandidatesForEdit(startLine, endLine, extract(editRange, &newText))
}

// onTextChanged is U:input/editor/display_map/display_map.rs:216-226.
func (m *inputDisplayMap) onTextChanged(changedText rope.Rope, rng InputSelection, newText string, w *gpui.Window) {
	m.wrap.onTextChanged(changedText, rng, rope.FromStr(newText), w)
	m.rebuildFoldProjection()
}

// setInlineMetrics is U:input/editor/display_map/display_map.rs:229-236.
func (m *inputDisplayMap) setInlineMetrics(metrics []inlineMetric, w *gpui.Window) {
	m.wrap.setInlineMetrics(metrics, w)
	m.rebuildFoldProjection()
}

// onLayoutChanged is U:input/editor/display_map/display_map.rs:238-241.
func (m *inputDisplayMap) onLayoutChanged(wrapWidth *float32, w *gpui.Window) {
	m.wrap.onLayoutChanged(wrapWidth, w)
	m.rebuildFoldProjection()
}

// setWrappingIndent is U:input/editor/display_map/display_map.rs:244-247.
func (m *inputDisplayMap) setWrappingIndent(indent WrappingIndent, w *gpui.Window) {
	m.wrap.setWrappingIndent(indent, w)
	m.rebuildFoldProjection()
}

// setFont is U:input/editor/display_map/display_map.rs:250-253.
func (m *inputDisplayMap) setFont(font gpui.FontDescriptor, fontSize float32, w *gpui.Window) {
	m.wrap.setFont(font, fontSize, w)
	m.rebuildFoldProjection()
}

// ensureTextPrepared is U:input/editor/display_map/display_map.rs:256-261.
func (m *inputDisplayMap) ensureTextPrepared(text rope.Rope, w *gpui.Window) {
	if m.wrap.ensureTextPrepared(text, w) {
		m.rebuildFoldProjection()
	}
}

// setText is U:input/editor/display_map/display_map.rs:264-267.
func (m *inputDisplayMap) setText(text rope.Rope, w *gpui.Window) {
	m.wrap.setText(text, w)
	m.rebuildFoldProjection()
}

// rebuildFoldProjection rebuilds only with folds; otherwise the identity
// mapping keeps just the wrap row count
// (U:input/editor/display_map/display_map.rs:273-282).
func (m *inputDisplayMap) rebuildFoldProjection() {
	if len(m.folds.foldedRanges()) > 0 {
		m.folds.rebuild(&m.wrap)
		return
	}
	m.folds.markDirtyWithWrapCount(m.wrap.wrapRowCount())
}

// offsetToWrapDisplayPoint is U:input/editor/display_map/display_map.rs:288-290.
func (m *inputDisplayMap) offsetToWrapDisplayPoint(offset int) wrapDisplayPoint {
	return m.wrap.wrapper.offsetToDisplayPoint(offset)
}

// offsetToWrapDisplayPointWithAffinity is U:input/editor/display_map/display_map.rs:295-303.
func (m *inputDisplayMap) offsetToWrapDisplayPointWithAffinity(offset int, lineEndAffinity bool) wrapDisplayPoint {
	return m.wrap.wrapper.offsetToDisplayPointWithAffinity(offset, lineEndAffinity)
}

// wrapDisplayPointToOffset is U:input/editor/display_map/display_map.rs:307-309.
func (m *inputDisplayMap) wrapDisplayPointToOffset(p wrapDisplayPoint) int {
	return m.wrap.wrapper.displayPointToOffset(p)
}

// wrapDisplayPointToPoint is U:input/editor/display_map/display_map.rs:313-315.
func (m *inputDisplayMap) wrapDisplayPointToPoint(p wrapDisplayPoint) InputPoint {
	return m.wrap.wrapper.displayPointToPoint(p)
}

// wrapRowToDisplayRow is false for a folded row
// (U:input/editor/display_map/display_map.rs:320-322).
func (m *inputDisplayMap) wrapRowToDisplayRow(wrapRow int) (int, bool) {
	return m.folds.wrapRowToDisplayRow(wrapRow)
}

// nearestVisibleDisplayRow is U:input/editor/display_map/display_map.rs:326-328.
func (m *inputDisplayMap) nearestVisibleDisplayRow(wrapRow int) int {
	return m.folds.nearestVisibleDisplayRow(wrapRow)
}

// displayRowToWrapRow is U:input/editor/display_map/display_map.rs:332-334.
func (m *inputDisplayMap) displayRowToWrapRow(displayRow int) (int, bool) {
	return m.folds.displayRowToWrapRow(displayRow)
}

// displayRowColumnToOffset is U:input/editor/display_map/display_map.rs:336-344.
func (m *inputDisplayMap) displayRowColumnToOffset(displayRow, column int) int {
	wrapRow, _ := m.folds.displayRowToWrapRow(displayRow)
	return m.wrap.wrapper.displayPointToOffset(newWrapDisplayPoint(wrapRow, 0, column))
}

// longestRow is U:input/editor/display_map/display_map.rs:348-350.
func (m *inputDisplayMap) longestRow() int { return m.wrap.wrapper.longestRow() }

// line is the line item of a buffer row
// (U:input/editor/display_map/display_map.rs:356-358).
func (m *inputDisplayMap) line(row int) (lineItem, bool) { return m.wrap.line(row) }

// text is U:input/editor/display_map/display_map.rs:362-364.
func (m *inputDisplayMap) text() *rope.Rope { return m.wrap.wrapper.textRope() }

// visibleWrapRowCountForBufferLine is U:input/editor/display_map/display_map.rs:368-371.
func (m *inputDisplayMap) visibleWrapRowCountForBufferLine(line int) int {
	return m.wrap.visibleWrapRowCountForLine(line, &m.folds)
}

// wrapRowCount is U:input/editor/display_map/display_map.rs:375-377.
func (m *inputDisplayMap) wrapRowCount() int { return m.wrap.wrapRowCount() }

// bufferLineCount is U:input/editor/display_map/display_map.rs:381-383.
func (m *inputDisplayMap) bufferLineCount() int { return m.wrap.bufferLineCount() }

// wrappedRanges is a buffer line's visual lines as local byte ranges
// (LineItem.wrapped_lines).
func (m *inputDisplayMap) wrappedRanges(line int) []InputSelection {
	if item, ok := m.line(line); ok {
		return item.wrappedLines
	}
	return nil
}
