package base

// Port of U:input/base/mode.rs: how the input lays its text out.
//
// layoutMode is pub(crate) upstream and stays unexported. The CodeEditor
// variant keeps its layout flags (rows, line numbers, indent guides,
// folding, auto close, smart indent); its language, highlighter,
// diagnostics, tab size and auto-closed pairs belong to the editor ports
// (indent.rs TabSize, language.rs EditorLanguage, highlighting.rs,
// diagnostics.rs, auto_close.rs) and join with them, together with
// language_config / syntax_context_at / auto_closed_pairs /
// update_highlighter (U:input/base/mode.rs:112-198, 268-372) and the
// PlainText tab field (U:input/base/mode.rs:31).
//
// update_auto_grow takes the display map's wrap row count instead of the
// map itself (the display map is the textarea ticket's).

import "github.com/hoangvu12/gpui-go/base/rope"

// layoutModeKind discriminates layoutMode (U:input/base/mode.rs:29-55).
type layoutModeKind uint8

const (
	layoutPlainText layoutModeKind = iota
	layoutAutoGrow
	layoutCodeEditor
)

// layoutMode is LayoutMode (U:input/base/mode.rs:23-55): rows, growth and
// the code-editor extras.
type layoutMode struct {
	kind layoutModeKind
	rows int
	// AutoGrow.
	minRows int
	maxRows int
	// CodeEditor.
	lineNumber   bool
	indentGuides bool
	folding      bool
	autoClose    bool
	smartIndent  bool
}

// layoutModePlainText is LayoutMode::plain_text and Default
// (U:input/base/mode.rs:57-71).
func layoutModePlainText() layoutMode {
	return layoutMode{kind: layoutPlainText, rows: 1}
}

// layoutModeCodeEditor is LayoutMode::code_editor's layout fields
// (U:input/base/mode.rs:73-91).
func layoutModeCodeEditor() layoutMode {
	return layoutMode{
		kind:         layoutCodeEditor,
		rows:         2,
		lineNumber:   true,
		indentGuides: true,
		folding:      true,
		autoClose:    true,
		smartIndent:  true,
	}
}

// layoutModeAutoGrow is LayoutMode::auto_grow (U:input/base/mode.rs:93-100).
func layoutModeAutoGrow(minRows, maxRows int) layoutMode {
	return layoutMode{kind: layoutAutoGrow, rows: minRows, minRows: minRows, maxRows: maxRows}
}

// isFolding reports a code editor with folding enabled
// (U:input/base/mode.rs:102-110; the wasm exclusion does not apply).
func (m *layoutMode) isFolding() bool { return m.kind == layoutCodeEditor && m.folding }

// isAutoClose is U:input/base/mode.rs:130-138.
func (m *layoutMode) isAutoClose() bool { return m.kind == layoutCodeEditor && m.autoClose }

// isSmartIndent is U:input/base/mode.rs:140-148.
func (m *layoutMode) isSmartIndent() bool { return m.kind == layoutCodeEditor && m.smartIndent }

// setAutoClose is U:input/base/mode.rs:150-154.
func (m *layoutMode) setAutoClose(enabled bool) {
	if m.kind == layoutCodeEditor {
		m.autoClose = enabled
	}
}

// setSmartIndent is U:input/base/mode.rs:156-160.
func (m *layoutMode) setSmartIndent(enabled bool) {
	if m.kind == layoutCodeEditor {
		m.smartIndent = enabled
	}
}

// isAutoGrow is U:input/base/mode.rs:199-202.
func (m *layoutMode) isAutoGrow() bool { return m.kind == layoutAutoGrow }

// setRows is U:input/base/mode.rs:204-220: auto grow clamps to its range.
func (m *layoutMode) setRows(newRows int) {
	if m.kind == layoutAutoGrow {
		m.rows = min(max(newRows, m.minRows), m.maxRows)
		return
	}
	m.rows = newRows
}

// updateAutoGrow grows the row count to fit the content
// (U:input/base/mode.rs:222-229). Callers gate it on a multi-line input.
func (m *layoutMode) updateAutoGrow(wrapRowCount int) { m.setRows(wrapRowCount) }

// rowsCount is rows(): at least 1 (U:input/base/mode.rs:231-239).
func (m *layoutMode) rowsCount() int { return max(m.rows, 1) }

// minRowsCount is min_rows(): at least 1 (U:input/base/mode.rs:241-249).
func (m *layoutMode) minRowsCount() int {
	if m.kind == layoutAutoGrow {
		return max(m.minRows, 1)
	}
	return 1
}

// maxRowsCount is max_rows() (U:input/base/mode.rs:251-257); usize::MAX
// is math.MaxInt.
func (m *layoutMode) maxRowsCount() int {
	if m.kind == layoutAutoGrow {
		return m.maxRows
	}
	return int(^uint(0) >> 1)
}

// showLineNumber is line_number(): false outside a code editor
// (U:input/base/mode.rs:259-266).
func (m *layoutMode) showLineNumber() bool { return m.kind == layoutCodeEditor && m.lineNumber }

// replacementInputEdit builds the tree-sitter edit for a text replacement
// (U:input/base/mode.rs:391-413): start/old end from oldText, new end from
// the post-edit newText.
func replacementInputEdit(oldText, newText rope.Rope, selectedRange InputSelection, changeText string) InputEdit {
	oldExt, newExt := RopeExtOf(&oldText), RopeExtOf(&newText)
	startByte := min(selectedRange.Start, oldText.Len())
	oldEndByte := max(min(selectedRange.End, oldText.Len()), startByte)
	newEndByte := min(startByte+len(changeText), newText.Len())
	return InputEdit{
		StartByte:      startByte,
		OldEndByte:     oldEndByte,
		NewEndByte:     newEndByte,
		StartPosition:  oldExt.OffsetToPoint(startByte),
		OldEndPosition: oldExt.OffsetToPoint(oldEndByte),
		NewEndPosition: newExt.OffsetToPoint(newEndByte),
	}
}
