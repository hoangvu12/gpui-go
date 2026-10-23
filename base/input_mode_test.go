package base

import (
	"math"
	"testing"

	"github.com/hoangvu12/gpui-go/base/rope"
)

// Ported tests of U:input/base/mode.rs (4/4).

// U:input/base/mode.rs:422-435
func TestInputModeReplacementInputEditBackspaceAtEndUsesOldRange(t *testing.T) {
	oldText := rope.FromStr("-=")
	text := rope.FromStr("-")
	edit := replacementInputEdit(oldText, text, NewInputSelection(1, 2), "")

	if edit.StartByte != 1 || edit.OldEndByte != 2 || edit.NewEndByte != 1 {
		t.Fatalf("bytes = %d %d %d, want 1 2 1", edit.StartByte, edit.OldEndByte, edit.NewEndByte)
	}
	if edit.StartPosition != NewInputPoint(0, 1) || edit.OldEndPosition != NewInputPoint(0, 2) || edit.NewEndPosition != NewInputPoint(0, 1) {
		t.Fatalf("positions = %v %v %v", edit.StartPosition, edit.OldEndPosition, edit.NewEndPosition)
	}
}

// U:input/base/mode.rs:437-466
func TestInputModeCodeEditor(t *testing.T) {
	mode := layoutModeCodeEditor(nil)
	if !mode.showLineNumber() || !mode.hasIndentGuides() || mode.maxRowsCount() != math.MaxInt || mode.minRowsCount() != 1 || !mode.isFolding() {
		t.Fatalf("code editor defaults = %+v", mode)
	}

	mode = layoutMode{kind: layoutCodeEditor, language: &editorLanguage{}, highlighter: &highlighterCell{}}
	if mode.showLineNumber() || mode.hasIndentGuides() || mode.minRowsCount() != 1 || mode.isFolding() || mode.isAutoClose() || mode.isSmartIndent() {
		t.Fatalf("code editor with everything off = %+v", mode)
	}
}

// U:input/base/mode.rs:468-482
func TestInputModePlain(t *testing.T) {
	mode := layoutMode{kind: layoutPlainText, rows: 5}
	if mode.showLineNumber() || mode.rowsCount() != 5 || mode.maxRowsCount() != math.MaxInt || mode.minRowsCount() != 1 {
		t.Fatalf("plain text rows 5 = %+v", mode)
	}

	mode = layoutModePlainText()
	if mode.showLineNumber() || mode.rowsCount() != 1 || mode.minRowsCount() != 1 {
		t.Fatalf("plain text = %+v", mode)
	}
}

// U:input/base/mode.rs:484-500
func TestInputModeAutoGrow(t *testing.T) {
	mode := layoutModeAutoGrow(2, 5)
	if mode.showLineNumber() || mode.rowsCount() != 2 || mode.maxRowsCount() != 5 || mode.minRowsCount() != 2 {
		t.Fatalf("auto grow = %+v", mode)
	}
	for _, step := range []struct{ set, want int }{{4, 4}, {1, 2}, {10, 5}} {
		mode.setRows(step.set)
		if got := mode.rowsCount(); got != step.want {
			t.Fatalf("setRows(%d): rows = %d, want %d", step.set, got, step.want)
		}
	}
}
