package base

// Port of U:input/editor/display_map/wrap_map.rs: the soft-wrapping layer
// (buffer → wrap rows) over the textWrapper. Methods that re-wrap take the
// window in place of `cx: &mut App` (see input_text_wrapper.go).

import (
	"github.com/hoangvu12/gpui-go/base/rope"
	"github.com/hoangvu12/gpui-go/gpui"
)

// wrapMap is WrapMap (U:input/editor/display_map/wrap_map.rs:20-23).
type wrapMap struct {
	wrapper textWrapper
}

// newWrapMap is U:input/editor/display_map/wrap_map.rs:26-30.
func newWrapMap(font gpui.FontDescriptor, fontSize float32, wrapWidth *float32) wrapMap {
	return wrapMap{wrapper: newTextWrapper(font, fontSize, wrapWidth)}
}

// wrapRowCount is U:input/editor/display_map/wrap_map.rs:34-36.
func (m *wrapMap) wrapRowCount() int { return m.wrapper.len() }

// bufferLineCount is U:input/editor/display_map/wrap_map.rs:40-42.
func (m *wrapMap) bufferLineCount() int { return m.wrapper.linesCount() }

// bufferPosToWrapPos is U:input/editor/display_map/wrap_map.rs:45-66.
func (m *wrapMap) bufferPosToWrapPos(pos bufferPoint) wrapPoint {
	line := min(pos.line, max(m.bufferLineCount()-1, 0))
	col := 0
	if item, ok := m.wrapper.line(line); ok {
		col = min(pos.col, item.len())
	}
	offset := RopeExtOf(m.wrapper.textRope()).LineStartOffset(line) + col
	dp := m.wrapper.offsetToDisplayPoint(offset)
	return wrapPoint{row: dp.row, col: dp.column}
}

// wrapPosToBufferPos is U:input/editor/display_map/wrap_map.rs:69-85.
func (m *wrapMap) wrapPosToBufferPos(pos wrapPoint) bufferPoint {
	row := min(pos.row, max(m.wrapRowCount()-1, 0))
	offset := m.wrapper.displayPointToOffset(newWrapDisplayPoint(row, 0, pos.col))
	ext := RopeExtOf(m.wrapper.textRope())
	point := ext.OffsetToPoint(offset)
	return bufferPoint{line: point.Row, col: max(offset-ext.LineStartOffset(point.Row), 0)}
}

// wrapRowToBufferLine is U:input/editor/display_map/wrap_map.rs:88-90.
func (m *wrapMap) wrapRowToBufferLine(wrapRow int) int { return m.wrapper.wrapRowToBufferLine(wrapRow) }

// bufferLineToFirstWrapRow is U:input/editor/display_map/wrap_map.rs:93-95.
func (m *wrapMap) bufferLineToFirstWrapRow(line int) int {
	return m.wrapper.bufferLineToFirstWrapRow(line)
}

// bufferLineToWrapRowRange is U:input/editor/display_map/wrap_map.rs:98-100.
func (m *wrapMap) bufferLineToWrapRowRange(line int) InputSelection {
	return m.wrapper.bufferLineToWrapRowRange(line)
}

// onTextChanged is U:input/editor/display_map/wrap_map.rs:103-112.
func (m *wrapMap) onTextChanged(changedText rope.Rope, rng InputSelection, newText rope.Rope, w *gpui.Window) {
	m.wrapper.adjustInlineMetrics(rng, newText.Len())
	m.wrapper.update(changedText, rng, newText, w)
}

// setInlineMetrics is U:input/editor/display_map/wrap_map.rs:115-121.
func (m *wrapMap) setInlineMetrics(metrics []inlineMetric, w *gpui.Window) {
	m.wrapper.setInlineMetrics(metrics, w)
}

// onLayoutChanged is U:input/editor/display_map/wrap_map.rs:123-125.
func (m *wrapMap) onLayoutChanged(wrapWidth *float32, w *gpui.Window) {
	m.wrapper.setWrapWidth(wrapWidth, w)
}

// setWrappingIndent is U:input/editor/display_map/wrap_map.rs:128-130.
func (m *wrapMap) setWrappingIndent(indent WrappingIndent, w *gpui.Window) {
	m.wrapper.setWrappingIndent(indent, w)
}

// setFont is U:input/editor/display_map/wrap_map.rs:133-135.
func (m *wrapMap) setFont(font gpui.FontDescriptor, fontSize float32, w *gpui.Window) {
	m.wrapper.setFont(font, fontSize, w)
}

// setTabSize sets the columns a hard tab expands to (Base ticket 44,
// input_tab_map.go; no upstream counterpart).
func (m *wrapMap) setTabSize(tabSize int, w *gpui.Window) bool {
	return m.wrapper.setTabSize(tabSize, w)
}

// ensureTextPrepared is U:input/editor/display_map/wrap_map.rs:138-140.
func (m *wrapMap) ensureTextPrepared(text rope.Rope, w *gpui.Window) bool {
	return m.wrapper.prepareIfNeed(text, w)
}

// setText is U:input/editor/display_map/wrap_map.rs:143-146.
func (m *wrapMap) setText(text rope.Rope, w *gpui.Window) {
	m.wrapper.setDefaultText(text)
	m.wrapper.prepareIfNeed(text, w)
}

// line is U:input/editor/display_map/wrap_map.rs:155-157.
func (m *wrapMap) line(row int) (lineItem, bool) { return m.wrapper.line(row) }

// visibleWrapRowCountForLine counts a buffer line's unfolded wrap rows
// (U:input/editor/display_map/wrap_map.rs:165-170).
func (m *wrapMap) visibleWrapRowCountForLine(line int, folds *foldMap) int {
	r := m.bufferLineToWrapRowRange(line)
	count := 0
	for wr := r.Start; wr < r.End; wr++ {
		if _, ok := folds.wrapRowToDisplayRow(wr); ok {
			count++
		}
	}
	return count
}
