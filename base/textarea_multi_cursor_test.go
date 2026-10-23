package base

// Ported multi-line undo and multi-cursor tests of U:input/base/state.rs
// (harness in textarea_state_test.go).

import (
	"slices"
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

type textareaEditFn = func(s *TextareaState, w *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]])

var plainEnter = &Enter{}

// U:input/base/state.rs:6052-6079
func TestTextareaUndoManagerEnterIsAtomic(t *testing.T) {
	v := buildTextarea(t, plainTextarea)
	v.update(func(s *TextareaState, w *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
		s.ReplaceTextInRange(nil, "a", w, cx)
		s.enter(plainEnter, w, cx)
		s.ReplaceTextInRange(nil, "b", w, cx)
		undoAndWant(t, s, w, cx, "a\n", "a", "")
	})
}

// U:input/base/state.rs:6113-6139
func TestTextareaUndoManagerSubmitOnEnterCommitsTheTextareaSession(t *testing.T) {
	v := buildTextarea(t, func(s TextareaState) TextareaState { return s.SubmitOnEnter(true) })
	v.update(func(s *TextareaState, w *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
		s.ReplaceTextInRange(nil, "before submit", w, cx)
		s.enter(plainEnter, w, cx)
		s.ReplaceTextInRange(nil, " after submit", w, cx)
		wantValue(t, s, "before submit after submit")
		undoAndWant(t, s, w, cx, "before submit", "")
	})
}

// U:input/base/state.rs:6163-6189
func TestTextareaUndoManagerKeepsRapidLinesInDistinctTransactions(t *testing.T) {
	v := buildTextarea(t, plainTextarea)
	v.update(func(s *TextareaState, w *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
		s.ReplaceTextInRange(nil, "a", w, cx)
		s.enter(plainEnter, w, cx)
		s.ReplaceTextInRange(nil, "b", w, cx)
		s.enter(plainEnter, w, cx)
		s.ReplaceTextInRange(nil, "c", w, cx)
		wantValue(t, s, "a\nb\nc")
		undoAndWant(t, s, w, cx, "a\nb\n", "a\nb", "a\n", "a", "")
	})
}

// U:input/base/state.rs:6190-6217
func TestTextareaUndoManagerCoalescesLongUnicodeTypingWithoutATimer(t *testing.T) {
	v := buildTextarea(t, plainTextarea)
	parts := []string{"The ", "quick ", "brown fox, ", "你好，世界 ", "🦀 jumps over 13 lazy dogs."}
	expected := strings.Join(parts, "")
	v.update(func(s *TextareaState, w *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
		typeText(s, w, cx, parts...)
		wantValue(t, s, expected)
		undoAndWant(t, s, w, cx, "")
		redoAndWant(t, s, w, cx, expected)
	})
}

// splitInclusive is str::split_inclusive(' ').
func splitInclusive(s string) []string {
	var out []string
	for s != "" {
		i := strings.IndexByte(s, ' ')
		if i < 0 {
			out = append(out, s)
			break
		}
		out = append(out, s[:i+1])
		s = s[i+1:]
	}
	return out
}

// U:input/base/state.rs:6219-6267
func TestTextareaUndoManagerLongMultilineSequenceHasStructuralBoundaries(t *testing.T) {
	v := buildTextarea(t, plainTextarea)
	v.update(func(s *TextareaState, w *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
		lines := []string{"first line with punctuation!", "第二行包含 Unicode 🦀", "third line has several words"}
		for i, line := range lines {
			typeText(s, w, cx, splitInclusive(line)...)
			if i < 2 {
				s.enter(plainEnter, w, cx)
			}
		}
		wantValue(t, s, strings.Join(lines, "\n"))
		undoAndWant(t, s, w, cx,
			"first line with punctuation!\n第二行包含 Unicode 🦀\n",
			"first line with punctuation!\n第二行包含 Unicode 🦀",
			"first line with punctuation!\n")
	})
}

// U:input/base/state.rs:6397-6418
func TestTextareaUndoManagerWordAndLineDeletesDoNotCoalesce(t *testing.T) {
	v := buildTextarea(t, plainTextarea)
	v.update(func(s *TextareaState, w *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
		s.SetValue("one two three\nfour five", w, cx)
		s.SetSelectedRange(sel(13, 13), cx)
		s.deletePreviousWord(&DeleteToPreviousWordStart{}, w, cx)
		wantValue(t, s, "one two \nfour five")
		s.deleteToEndOfLine(&DeleteToEndOfLine{}, w, cx)
		wantValue(t, s, "one two four five")
		undoAndWant(t, s, w, cx, "one two \nfour five", "one two three\nfour five")
	})
}

// U:input/base/state.rs:6420-6440
func TestTextareaUndoManagerMultilineReplacementIsOneAtomicTransaction(t *testing.T) {
	v := buildTextarea(t, plainTextarea)
	v.update(func(s *TextareaState, w *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
		s.ReplaceTextInRange(nil, "before", w, cx)
		s.SetSelectedRange(sel(0, 6), cx)
		s.ReplaceTextInRange(nil, "line one\nline two\n第三行", w, cx)
		wantValue(t, s, "line one\nline two\n第三行")
		undoAndWant(t, s, w, cx, "before")
		wantSelected(t, s, 0, 6)
		redoAndWant(t, s, w, cx, "line one\nline two\n第三行")
	})
}

// U:input/base/state.rs:9078-9095
func TestTextareaNoopDoesNotChangeRedoSelection(t *testing.T) {
	v := buildTextarea(t, plainTextarea)
	v.update(func(s *TextareaState, w *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
		s.SetValue("abc", w, cx)
		s.SetSelectedRange(sel(1, 1), cx)
		s.ReplaceTextInRange(nil, "X", w, cx)
		s.SetSelectedRange(sel(0, 0), cx)
		s.ReplaceTextInRange(nil, "", w, cx)
		s.undo(&Undo{}, w, cx)
		s.redo(&Redo{}, w, cx)
		wantValue(t, s, "aXbc")
		wantSelected(t, s, 2, 2)
	})
}

func typeX(text string) textareaEditFn {
	return func(s *TextareaState, w *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
		s.ReplaceTextInRange(nil, text, w, cx)
	}
}

func backspaceOnce(s *TextareaState, w *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
	s.backspace(&Backspace{}, w, cx)
}

func undoOnce(s *TextareaState, w *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
	s.undo(&Undo{}, w, cx)
}

func redoOnce(s *TextareaState, w *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
	s.redo(&Redo{}, w, cx)
}

func wantUndoCount(t *testing.T, v *inputView[TextareaMode], n int) {
	t.Helper()
	v.read(func(s *TextareaState) {
		if got := s.undoManager.undoCount(); got != n {
			t.Fatalf("undo count = %d, want %d", got, n)
		}
	})
}

// U:input/base/state.rs:9097-9110
func TestTextareaMultiCursorInsertText(t *testing.T) {
	v := buildTextarea(t, plainTextarea)
	setupCursors(v, "|hello |world|")
	v.update(typeX(">>>"))
	assertCursors(t, v, ">>>|hello >>>|world>>>|")
}

// U:input/base/state.rs:9112-9126
func TestTextareaMultiCursorDeleteBackward(t *testing.T) {
	v := buildTextarea(t, plainTextarea)
	setupCursors(v, "|islands| cars|")
	v.update(backspaceOnce)
	assertCursors(t, v, "|island| car|")
}

// U:input/base/state.rs:9128-9142
func TestTextareaMultiCursorDeleteForwardMerges(t *testing.T) {
	v := buildTextarea(t, plainTextarea)
	setupCursors(v, "hello| |world")
	v.update(func(s *TextareaState, w *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
		s.delete(&Delete{}, w, cx)
	})
	assertCursors(t, v, "hello|orld")
}

// U:input/base/state.rs:9144-9169
func TestTextareaMultiCursorMultilineInsertAndDelete(t *testing.T) {
	v := buildTextarea(t, plainTextarea)
	setupCursors(v, "|1\n|2\n|3")
	v.update(typeX("a"))
	assertCursors(t, v, "a|1\na|2\na|3")
	wantUndoCount(t, v, 1)
	v.update(backspaceOnce)
	assertCursors(t, v, "|1\n|2\n|3")
}

// U:input/base/state.rs:9171-9184
func TestTextareaAddCursorBelowPreservesColumn(t *testing.T) {
	v := buildTextarea(t, plainTextarea)
	setupCursors(v, "ab|cd\nabcd")
	v.update(func(s *TextareaState, w *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
		s.addCursorBelow(&AddCursorBelow{}, w, cx)
	})
	assertCursors(t, v, "ab|cd\nab|cd")
}

// U:input/base/state.rs:9186-9203
func TestTextareaAddCursorAtRejectsDuplicates(t *testing.T) {
	v := buildTextarea(t, plainTextarea)
	setupCursors(v, "he|llo")
	v.update(func(s *TextareaState, _ *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
		s.addCursorAt(2, cx)
		if s.selections.len() != 1 {
			t.Fatalf("a duplicate cursor must be rejected: %d", s.selections.len())
		}
		s.addCursorAt(4, cx)
		if s.selections.len() != 2 {
			t.Fatalf("a distinct offset adds a cursor: %d", s.selections.len())
		}
	})
}

// U:input/base/state.rs:9205-9232
func TestTextareaMultiCursorUndoRedoRestoresSelections(t *testing.T) {
	v := buildTextarea(t, plainTextarea)
	setupCursors(v, "|1\n|2\n|3")
	v.update(typeX("a"))
	assertCursors(t, v, "a|1\na|2\na|3")
	v.update(undoOnce)
	assertCursors(t, v, "|1\n|2\n|3")
	v.update(redoOnce)
	assertCursors(t, v, "a|1\na|2\na|3")
}

// U:input/base/state.rs:9234-9261
func TestTextareaMultiCursorUndoRedoDifferentLineLengths(t *testing.T) {
	v := buildTextarea(t, plainTextarea)
	setupCursors(v, "abc123|\nabc12345|\nabc1234567|")
	v.update(typeX("a"))
	assertCursors(t, v, "abc123a|\nabc12345a|\nabc1234567a|")
	v.update(undoOnce)
	assertCursors(t, v, "abc123|\nabc12345|\nabc1234567|")
	v.update(redoOnce)
	assertCursors(t, v, "abc123a|\nabc12345a|\nabc1234567a|")
}

// U:input/base/state.rs:9263-9294
func TestTextareaMultiCursorUndoMultipleInserts(t *testing.T) {
	v := buildTextarea(t, plainTextarea)
	setupCursors(v, "|1\n|2\n|3")
	for _, ch := range []string{"a", "b", "c"} {
		v.update(typeX(ch))
	}
	assertCursors(t, v, "abc|1\nabc|2\nabc|3")
	wantUndoCount(t, v, 1)
	v.update(undoOnce)
	assertCursors(t, v, "|1\n|2\n|3")
	v.update(redoOnce)
	assertCursors(t, v, "abc|1\nabc|2\nabc|3")
}

// U:input/base/state.rs:9296-9319
func TestTextareaMultiCursorBackspaceRunIsOneUndo(t *testing.T) {
	v := buildTextarea(t, plainTextarea)
	setupCursors(v, "abc|1\nabc|2\nabc|3")
	for range 3 {
		v.update(backspaceOnce)
	}
	assertCursors(t, v, "|1\n|2\n|3")
	wantUndoCount(t, v, 1)
	v.update(undoOnce)
	assertCursors(t, v, "abc|1\nabc|2\nabc|3")
}

// U:input/base/state.rs:9321-9344
func TestTextareaAddingACursorSplitsTheTypingGesture(t *testing.T) {
	v := buildTextarea(t, plainTextarea)
	setupCursors(v, "|1\n2\n3")
	v.update(func(s *TextareaState, w *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
		s.ReplaceTextInRange(nil, "a", w, cx)
		s.addCursorBelow(&AddCursorBelow{}, w, cx)
		s.ReplaceTextInRange(nil, "b", w, cx)
	})
	assertCursors(t, v, "ab|1\n2b|\n3")
	v.update(undoOnce)
	assertCursors(t, v, "a|1\n2|\n3")
}

// U:input/base/state.rs:9419-9437
func TestTextareaReadonlyMultiCursorCommandsLeaveStateUnchanged(t *testing.T) {
	v := buildTextarea(t, plainTextarea)
	setupCursors(v, "a|b\nc|d")
	v.update(func(s *TextareaState, w *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
		before := slices.Collect(s.selections.iter())
		s.SetReadonly(true, cx)
		s.backspace(&Backspace{}, w, cx)
		s.indentInline(&IndentInline{}, w, cx)
		wantValue(t, s, "ab\ncd\n")
		if !slices.Equal(slices.Collect(s.selections.iter()), before) {
			t.Fatal("readonly commands leave the selections alone")
		}
	})
}

// U:input/base/state.rs:9439-9456
func TestTextareaMultiCursorEditPreservesTheActiveCursor(t *testing.T) {
	v := buildTextarea(t, plainTextarea)
	setupCursors(v, "x|\n|y")
	v.update(func(s *TextareaState, w *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
		sels := slices.Collect(s.selections.iter())
		sels[0], sels[1] = sels[1], sels[0]
		s.selections.replaceAll(sels)
		activeID := s.activeSelection().id
		s.ReplaceTextInRange(nil, "!", w, cx)
		if s.activeSelection().id != activeID {
			t.Fatal("the active cursor must survive the edit")
		}
	})
}

func cursorOffsets(s *TextareaState) []int {
	var out []int
	for c := range s.selections.iter() {
		out = append(out, c.cursorOffset())
	}
	return out
}

// U:input/base/state.rs:9494-9548
func TestTextareaMultiCursorWordMovement(t *testing.T) {
	v := buildTextarea(t, plainTextarea)
	setupCursors(v, "on|e two three\none t|wo three\non|e two three")
	v.update(func(s *TextareaState, w *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
		s.moveToNextWord(&MoveToNextWord{}, w, cx)
	})
	assertCursors(t, v, "one| two three\none two| three\none| two three")
	v.update(func(s *TextareaState, w *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
		s.moveToPreviousWord(&MoveToPreviousWord{}, w, cx)
	})
	assertCursors(t, v, "|one two three\none |two three\n|one two three")
	v.update(func(s *TextareaState, w *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
		s.moveToEnd(&MoveToEnd{}, w, cx)
		if got := cursorOffsets(s); !slices.Equal(got, []int{s.text.Len()}) {
			t.Fatalf("move to end = %v", got)
		}
		s.moveToStart(&MoveToStart{}, w, cx)
		if got := cursorOffsets(s); !slices.Equal(got, []int{0}) {
			t.Fatalf("move to start = %v", got)
		}
	})
}

// U:input/base/state.rs:9550-9587
func TestTextareaMultiCursorSelectionCommands(t *testing.T) {
	v := buildTextarea(t, plainTextarea)
	setupCursors(v, "on|e two three\none t|wo three\non|e two three")
	v.update(func(s *TextareaState, w *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
		s.selectToStartOfLine(&SelectToStartOfLine{}, w, cx)
	})
	assertCursors(t, v, "|one two three\n|one two three\n|one two three")
	setupCursors(v, "on|e two three\none t|wo three\non|e two three")
	v.update(func(s *TextareaState, w *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
		s.selectToStart(&SelectToStart{}, w, cx)
		if got := cursorOffsets(s); !slices.Equal(got, []int{0}) {
			t.Fatalf("select to start = %v", got)
		}
	})
}

// U:input/base/state.rs:9589-9612
func TestTextareaMultiCursorReplaceSelection(t *testing.T) {
	v := buildTextarea(t, plainTextarea)
	setupCursors(v, "|a\n|b\n|c")
	v.update(func(s *TextareaState, w *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
		s.selectRight(&SelectRight{}, w, cx)
		if got := selectionRanges(s); !slices.Equal(got, []InputSelection{sel(0, 1), sel(2, 3), sel(4, 5)}) {
			t.Fatalf("ranges = %v", got)
		}
	})
	v.update(typeX("x"))
	assertCursors(t, v, "x|\nx|\nx|")
}

// U:input/base/state.rs:9614-9629
func TestTextareaMultiCursorEscapeCollapses(t *testing.T) {
	v := buildTextarea(t, plainTextarea)
	setupCursors(v, "|a\n|b\n|c")
	v.update(func(s *TextareaState, w *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
		s.escape(&Escape{}, w, cx)
		if s.selections.len() != 1 {
			t.Fatalf("selections = %d, want 1", s.selections.len())
		}
	})
}

// U:input/base/state.rs:9631-9650
func TestTextareaBuildColumnarSelection(t *testing.T) {
	v := buildTextarea(t, plainTextarea)
	setupCursors(v, "abcd\nabcd\nabcd")
	v.update(func(s *TextareaState, _ *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
		s.buildColumnarSelection(columnarPoint{offset: 1}, columnarPoint{offset: 13}, cx)
		if got := selectionRanges(s); !slices.Equal(got, []InputSelection{sel(1, 3), sel(6, 8), sel(11, 13)}) {
			t.Fatalf("ranges = %v", got)
		}
	})
}

// U:input/base/state.rs:9652-9672
func TestTextareaColumnarSelectionKeepsWidthOverShortRows(t *testing.T) {
	v := buildTextarea(t, plainTextarea)
	setupCursors(v, "abcdef\nab\nabcdef")
	v.update(func(s *TextareaState, _ *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
		s.buildColumnarSelection(columnarPoint{offset: 1}, columnarPoint{offset: 9, columnsPastLineEnd: 3}, cx)
		if got := selectionRanges(s); !slices.Equal(got, []InputSelection{sel(1, 5), sel(8, 9)}) {
			t.Fatalf("ranges = %v", got)
		}
	})
}

// U:input/base/state.rs:9674-9691 (TestApp has no clipboard: the text
// goes through the paste path's insertClipboard, as in the single-line
// clipboard tests).
func TestTextareaMultiCursorPasteDistributesLines(t *testing.T) {
	v := buildTextarea(t, plainTextarea)
	setupCursors(v, "|1\n|2\n|3")
	v.update(func(s *TextareaState, w *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
		s.insertClipboard(clipboardText("x\ny\nz"), w, cx)
	})
	assertCursors(t, v, "x|1\ny|2\nz|3")
}
