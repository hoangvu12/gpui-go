package base

// Port of U:input/base/mode.rs: how the input lays its text out.
//
// layoutMode is pub(crate) upstream and stays unexported. The CodeEditor
// variant carries its layout flags (rows, line numbers, indent guides,
// folding, auto close, smart indent), its tab size, language,
// highlighter cell and factory, and the auto-closed pairs. Its
// DiagnosticSet (U:input/base/mode.rs:49, diagnostics/diagnostics_mut)
// belongs to the diagnostics ticket (24) and joins with it.
//
// Go adaptations: the enum is one struct with a kind; Box<EditorLanguage>
// and the Rc<RefCell<..>> highlighter are pointers, so copies of the mode
// share them as clones of the Rust enum do. update_auto_grow takes the
// display map's wrap row count. update_highlighter / _batch are the
// state's updateHighlighter / updateHighlighterBatch (input_state_edit.go):
// they dispatch through the mode hooks, which take the state.

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
	// PlainText and CodeEditor.
	tab TabSize
	// AutoGrow.
	minRows int
	maxRows int
	// CodeEditor.
	lineNumber         bool
	language           *editorLanguage
	indentGuides       bool
	folding            bool
	highlighter        *highlighterCell
	highlighterFactory InputHighlighterFactory
	// autoClosed are the automatic delimiter ranges, adjusted with edits.
	autoClosed  autoClosedPairs
	autoClose   bool
	smartIndent bool
}

// layoutModePlainText is LayoutMode::plain_text and Default
// (U:input/base/mode.rs:57-71).
func layoutModePlainText() layoutMode {
	return layoutMode{kind: layoutPlainText, rows: 1, tab: DefaultTabSize()}
}

// layoutModeCodeEditor is LayoutMode::code_editor; the state sets the
// language through its own builder (U:input/base/mode.rs:73-91). A nil
// language is Default::default() (its own settings).
func layoutModeCodeEditor(language *editorLanguage) layoutMode {
	if language == nil {
		language = &editorLanguage{syntax: &languageSyntaxCell{}}
	}
	return layoutMode{
		kind:         layoutCodeEditor,
		rows:         2,
		tab:          DefaultTabSize(),
		language:     language,
		highlighter:  &highlighterCell{},
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

// languageConfig is U:input/base/mode.rs:112-117.
func (m *layoutMode) languageConfig() (*LanguageConfig, bool) {
	if m.kind != layoutCodeEditor {
		return nil, false
	}
	return m.language.config(), true
}

// syntaxContextAt is U:input/base/mode.rs:119-128.
func (m *layoutMode) syntaxContextAt(text rope.Rope, offset int) SyntaxContext {
	if m.kind != layoutCodeEditor {
		return SyntaxContextCode
	}
	return m.language.contextAt(text, offset)
}

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

// autoClosedPairsSnapshot is auto_closed_pairs().clone(); outside a code
// editor it is the empty set (U:input/base/mode.rs:162-169).
func (m *layoutMode) autoClosedPairsSnapshot() autoClosedPairs {
	if m.kind != layoutCodeEditor {
		return autoClosedPairs{}
	}
	return m.autoClosed.clone()
}

// restoreAutoClosedPairs is U:input/base/mode.rs:171-178.
func (m *layoutMode) restoreAutoClosedPairs(pairs autoClosedPairs) {
	if m.kind == layoutCodeEditor {
		m.autoClosed = pairs
	}
}

// trackAutoClosedPair is U:input/base/mode.rs:180-187.
func (m *layoutMode) trackAutoClosedPair(open, close InputSelection) {
	if m.kind == layoutCodeEditor {
		m.autoClosed.record(open, close)
	}
}

// adjustAutoClosedPair is U:input/base/mode.rs:189-197.
func (m *layoutMode) adjustAutoClosedPair(edit InputSelection, newLen int) {
	if m.kind == layoutCodeEditor {
		m.autoClosed.adjust(edit, newLen)
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

// currentHighlighter is highlighter() with the cell read: the installed
// highlighter, nil outside a code editor or before one is created
// (U:input/base/mode.rs:342-348).
func (m *layoutMode) currentHighlighter() InputHighlighter {
	if m.kind != layoutCodeEditor || m.highlighter == nil {
		return nil
	}
	return m.highlighter.h
}

// setHighlighterFactory installs a factory and drops the highlighter
// (U:input/base/mode.rs:350-360).
func (m *layoutMode) setHighlighterFactory(factory InputHighlighterFactory) {
	if m.kind == layoutCodeEditor {
		m.highlighterFactory = factory
		m.highlighter.h = nil
	}
}

// ensureHighlighterFactory installs a factory unless one is set
// (U:input/base/mode.rs:362-372).
func (m *layoutMode) ensureHighlighterFactory(factory InputHighlighterFactory) {
	if m.kind == layoutCodeEditor && m.highlighterFactory == nil {
		m.highlighterFactory = factory
	}
}

// ensureHighlighter creates the highlighter from the factory if there is
// none yet and reports whether one is installed
// (U:input/base/mode.rs:375-389).
func (m *layoutMode) ensureHighlighter() bool {
	if m.kind != layoutCodeEditor {
		return false
	}
	if m.highlighter.h == nil && m.highlighterFactory != nil {
		m.highlighter.h = m.highlighterFactory(m.language.name)
	}
	return m.highlighter.h != nil
}

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
