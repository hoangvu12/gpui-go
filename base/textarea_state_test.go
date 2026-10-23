package base

// Ported multi-line tests of U:input/base/state.rs (the ones the single-line
// ticket left), in upstream order. The editor-only ones are
// editor_state_test.go's.

import (
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/hoangvu12/gpui-go/gpui"
)

// buildEditor is InputView::<EditorMode>::new: EditorState::new with the
// "sql" language (U:input/base/state.rs:4829-4843).
func buildEditor(t *testing.T) *inputView[EditorMode] {
	t.Helper()
	return buildEditorWith(t, gpui.Size{}, func(s EditorState) EditorState { return s })
}

func buildEditorSized(t *testing.T, size gpui.Size) *inputView[EditorMode] {
	t.Helper()
	return buildEditorWith(t, size, func(s EditorState) EditorState { return s })
}

// buildEditorWith is InputView::build_editor (U:input/base/state.rs:
// 4835-4842).
func buildEditorWith(t *testing.T, size gpui.Size, f func(EditorState) EditorState) *inputView[EditorMode] {
	t.Helper()
	return buildModeView(t, size, func(w *gpui.Window, cx *gpui.Context[EditorState]) EditorState {
		return f(NewEditorState(w, cx).Language("sql"))
	})
}

// buildModeView is InputView::build_with (U:input/base/state.rs:4801-4826)
// in a window of size (the default test display when zero).
func buildModeView[M InputModeKind](t *testing.T, size gpui.Size, make func(*gpui.Window, *gpui.Context[InputBaseState[M]]) InputBaseState[M]) *inputView[M] {
	t.Helper()
	ta := gpui.NewTestApp()
	shutdownAtCleanup(t, ta)
	ta.Update(func(app *gpui.App) {
		theme := DefaultTheme()
		app.SetGlobal(&theme)
		inputInit(app)
	})
	var input gpui.Entity[InputBaseState[M]]
	build := func(r *inputTestRoot[M], w *gpui.Window, cx *gpui.Context[inputTestRoot[M]]) {
		input = gpui.NewEntity(cx, w.Scope(), func(s *InputBaseState[M], scx *gpui.Context[InputBaseState[M]]) { *s = make(w, scx) })
		r.input = input
	}
	if size == (gpui.Size{}) {
		_, cx := ta.AddWindowView(build)
		return &inputView[M]{input: input, cx: cx}
	}
	opts := gpui.WindowOptions{Bounds: gpui.Bounds{Size: size}}
	tw := ta.OpenWindowWithOptions[inputTestRoot[M]](opts, build)
	return &inputView[M]{input: input, cx: gpui.NewVisualTestContext(tw.Window(), ta)}
}

// parseCursorSpec is parse_cursor_spec: non-empty lines joined with `\n`
// plus a trailing `\n`; `|` marks a cursor (U:input/base/state.rs:7476-7507).
func parseCursorSpec(input string) (string, []int) {
	var full strings.Builder
	var cursors []int
	var lines []string
	for _, l := range strings.Split(input, "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	for i, line := range lines {
		if i > 0 {
			full.WriteByte('\n')
		}
		start := full.Len()
		text := ""
		for _, ch := range line {
			if ch == '|' {
				cursors = append(cursors, start+len(text))
			} else {
				text += string(ch)
			}
		}
		full.WriteString(text)
	}
	full.WriteByte('\n')
	return full.String(), cursors
}

// setupCursors is setup_cursors (U:input/base/state.rs:7514-7534).
func setupCursors[M InputModeKind](v *inputView[M], spec string) {
	text, offsets := parseCursorSpec(spec)
	v.update(func(s *InputBaseState[M], w *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
		s.SetValue(text, w, cx)
		sels := make([]cursorSelection, 0, len(offsets))
		for _, o := range offsets {
			sels = append(sels, newCursorSelection(s.selections.generateID(), o, o))
		}
		s.selections.replaceAll(sels)
		cx.Notify()
	})
	v.cx.RunUntilParked()
}

// assertCursors is assert_cursors (U:input/base/state.rs:7536-7566).
func assertCursors[M InputModeKind](t *testing.T, v *inputView[M], spec string) {
	t.Helper()
	wantText, want := parseCursorSpec(spec)
	slices.Sort(want)
	v.read(func(s *InputBaseState[M]) {
		var got []int
		for sel := range s.selections.iter() {
			got = append(got, sel.cursorOffset())
		}
		slices.Sort(got)
		if s.text.String() != wantText {
			t.Fatalf("text = %q, want %q", s.text.String(), wantText)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("cursors = %v, want %v", got, want)
		}
	})
}

func selectionRanges[M InputModeKind](s *InputBaseState[M]) []InputSelection {
	var out []InputSelection
	for sel := range s.selections.iter() {
		out = append(out, NewInputSelection(sel.start, sel.end))
	}
	return out
}

// U:input/base/state.rs:5194-5231
func TestTextareaCursorTreatsCrlfAsOneNewline(t *testing.T) {
	v := buildTextarea(t, plainTextarea)
	v.update(func(s *TextareaState, w *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
		for _, original := range []string{"\r\nlast", "\ufefffirst\r\nlast\n", "first\nlast\r\n"} {
			s.SetValue(original, w, cx)
			newline := strings.IndexByte(original, '\n')
			end := newline
			if original[max(newline-1, 0)] == '\r' {
				end = newline - 1
			}
			s.end(&MoveEnd{}, w, cx)
			if s.Cursor() != end {
				t.Fatalf("%q: end = %d, want %d", original, s.Cursor(), end)
			}
			s.right(&MoveRight{}, w, cx)
			if s.Cursor() != newline+1 {
				t.Fatalf("%q: right = %d, want %d", original, s.Cursor(), newline+1)
			}
			s.left(&MoveLeft{}, w, cx)
			if s.Cursor() != end {
				t.Fatalf("%q: left = %d, want %d", original, s.Cursor(), end)
			}
			s.selectToEndOfLine(&SelectToEndOfLine{}, w, cx)
			wantSelected(t, s, end, end)
			s.moveToEnd(&MoveToEnd{}, w, cx)
			if s.Cursor() != len(original) {
				t.Fatalf("%q: move to end = %d", original, s.Cursor())
			}
			s.moveToStart(&MoveToStart{}, w, cx)
			if s.Cursor() != 0 {
				t.Fatalf("%q: move to start = %d", original, s.Cursor())
			}
			wantValue(t, s, original)
		}
		// A lone CR is an ordinary character.
		s.SetValue("a\rb", w, cx)
		s.right(&MoveRight{}, w, cx)
		if s.Cursor() != 1 {
			t.Fatalf("cursor = %d, want 1", s.Cursor())
		}
		s.right(&MoveRight{}, w, cx)
		if s.Cursor() != 2 {
			t.Fatalf("cursor = %d, want 2", s.Cursor())
		}
	})
}

// U:input/base/state.rs:5284-5340
func TestEditorReadonlyRejectsUserEditsOnly(t *testing.T) {
	v := buildEditor(t)
	v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
		s.SetValue("hello", w, cx)
		s.SetReadonly(true, cx)
	})
	v.read(func(s *EditorState) {
		if s.IsEditable() || s.IsReplaceable() {
			t.Fatal("readonly is neither editable nor replaceable")
		}
	})
	v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
		s.ReplaceTextInRange(nil, " world", w, cx)
		s.ReplaceAndMarkTextInRange(nil, "あ", nil, w, cx)
	})
	v.read(func(s *EditorState) { wantValue(t, s, "hello") })
	v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
		s.Insert(" world", w, cx)
		s.SetValue("changed", w, cx)
	})
	v.read(func(s *EditorState) { wantValue(t, s, "changed") })
	v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
		s.SetReadonly(false, cx)
		s.ReplaceTextInRange(nil, "!", w, cx)
	})
	v.read(func(s *EditorState) {
		if !s.IsEditable() {
			t.Fatal("editable again")
		}
		wantValue(t, s, "!changed")
	})
}

// U:input/base/state.rs:5342-5402
func TestTextareaEditRevealsFarOffscreenCaret(t *testing.T) {
	v := buildModeView(t, gpui.Size{Width: 720, Height: 400}, func(w *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) TextareaState {
		return NewTextareaState(w, cx).AutoGrow(1, 6)
	})
	lines := make([]string, 0, 100)
	for i := 1; i <= 100; i++ {
		lines = append(lines, "line "+strconv.Itoa(i))
	}
	v.update(func(s *TextareaState, w *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
		s.SetValue(strings.Join(lines, "\n"), w, cx)
		end := s.text.Len()
		s.setSelection(end, end)
	})
	v.cx.RunUntilParked()
	v.update(func(s *TextareaState, _ *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
		s.scrollHandle.SetOffset(gpui.Point{})
		cx.Notify()
	})
	v.cx.RunUntilParked()
	v.update(func(s *TextareaState, w *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
		s.ReplaceTextInRange(nil, "X", w, cx)
	})
	v.cx.RunUntilParked()
	v.read(func(s *TextareaState) {
		if !strings.HasSuffix(s.Value(), "line 100X") {
			t.Fatalf("value tail = %q", s.Value()[len(s.Value())-12:])
		}
		caret, _, ok := s.CursorLayout()
		if !ok {
			t.Fatal("caret painted")
		}
		viewport := s.InputBounds()
		scrollY := s.scrollHandle.Offset().Y
		top := caret.Origin.Y - viewport.Origin.Y + scrollY
		if top < 0 || top+caret.Size.Height > viewport.Size.Height {
			t.Fatalf("caret top %v outside viewport height %v (scroll %v)", top, viewport.Size.Height, scrollY)
		}
	})
}

func itoa(i int) string { return strings.TrimSpace(strings.Repeat(" ", 0) + fmtInt(i)) }

func fmtInt(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for ; i > 0; i /= 10 {
		b = append([]byte{byte('0' + i%10)}, b...)
	}
	return string(b)
}

// U:input/base/state.rs:5404-5468
func TestEditorScrollToEobDoesNotOvershootSafeRange(t *testing.T) {
	v := buildEditor(t)
	one := 1
	v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
		s.SetScrollBeyondLastLine(&one, w, cx)
		s.SetCursorSurroundingLines(&one, w, cx)
		lines := make([]string, 0, 50)
		for i := 1; i <= 50; i++ {
			lines = append(lines, "line "+strconv.Itoa(i))
		}
		s.SetValue(strings.Join(lines, "\n"), w, cx)
	})
	v.cx.RunUntilParked()
	v.read(func(s *EditorState) {
		if !(s.scrollSize.Height > 0) || !(s.inputBounds.Size.Height > 0) {
			t.Fatalf("paint must populate scroll size %v and input bounds %v", s.scrollSize, s.inputBounds)
		}
	})
	v.update(func(s *EditorState, _ *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
		down := moveDirectionDown
		s.moveTo(s.text.Len(), &down, cx)
		if s.deferredScrollOffset == nil {
			t.Fatal("scroll_to should populate deferred_scroll_offset")
		}
		safeYMin := min(-s.scrollSize.Height+s.inputBounds.Size.Height, 0)
		if s.deferredScrollOffset.Y < safeYMin {
			t.Fatalf("deferred y %v below safe y min %v", s.deferredScrollOffset.Y, safeYMin)
		}
	})
}

// U:input/base/state.rs:6696-6722
func TestTextareaReplaceAllMultiLine(t *testing.T) {
	v := buildTextarea(t, plainTextarea)
	v.update(func(s *TextareaState, w *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
		s.SetValue("foo\nbar", w, cx)
		s.ReplaceAll("baz\nqux", w, cx)
		wantValue(t, s, "baz\nqux")
		wantSelected(t, s, 0, 0)
		if s.scrollHandle.Offset() != (gpui.Point{}) {
			t.Fatal("the scroll offset should be reset to the start")
		}
		if s.deferredScrollOffset != nil {
			t.Fatal("multi-line should not set a deferred scroll offset")
		}
	})
}

// U:input/base/state.rs:6763-6783
func TestEditorReplaceAllCodeEditor(t *testing.T) {
	v := buildEditor(t)
	v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
		s.SetValue("select 1", w, cx)
		s.pendingUpdate = false
		s.ReplaceAll("select 2", w, cx)
		wantValue(t, s, "select 2")
		if !s.pendingUpdate {
			t.Fatal("replace_all on a code editor should request a pending update")
		}
	})
}

// U:input/base/state.rs:6807-6826
func TestTextareaReplaceTextInRangesSingleEdit(t *testing.T) {
	v := buildTextarea(t, func(s TextareaState) TextareaState { return s.DefaultValue("hello world") })
	v.update(func(s *TextareaState, w *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
		s.replaceTextInRanges([]inputTextEdit{{rng: sel(0, 5), text: "HELLO"}}, w, cx)
		wantValue(t, s, "HELLO world")
		undoAndWant(t, s, w, cx, "hello world")
		redoAndWant(t, s, w, cx, "HELLO world")
	})
}

// U:input/base/state.rs:6828-6866
func TestTextareaReplaceTextInRangesMultiEditTransaction(t *testing.T) {
	v := buildTextarea(t, func(s TextareaState) TextareaState { return s.DefaultValue("aaa bbb ccc") })
	v.update(func(s *TextareaState, w *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
		s.replaceTextInRanges([]inputTextEdit{{rng: sel(0, 3), text: "X"}, {rng: sel(8, 11), text: "Y"}}, w, cx)
		wantValue(t, s, "X bbb Y")
		var cursors []int
		for sel := range s.selections.iter() {
			cursors = append(cursors, sel.cursorOffset())
		}
		if !slices.Equal(cursors, []int{1, 7}) {
			t.Fatalf("cursors = %v", cursors)
		}
		if n := s.undoManager.undoCount(); n != 1 {
			t.Fatalf("undo count = %d, want 1", n)
		}
		undoAndWant(t, s, w, cx, "aaa bbb ccc")
		redoAndWant(t, s, w, cx, "X bbb Y")
	})
}

func utf16Range(start, end int) *gpui.UTF16Range { return &gpui.UTF16Range{Start: start, End: end} }

// U:input/base/state.rs:6949-6972
func TestTextareaImeCompositionUndoesAsOneUnit(t *testing.T) {
	v := buildTextarea(t, func(s TextareaState) TextareaState { return s.DefaultValue("") })
	v.update(func(s *TextareaState, w *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
		s.ReplaceAndMarkTextInRange(nil, "n", utf16Range(1, 1), w, cx)
		s.ReplaceAndMarkTextInRange(nil, "ni", utf16Range(2, 2), w, cx)
		s.ReplaceTextInRange(nil, "你", w, cx)
		wantValue(t, s, "你")
		if n := s.undoManager.undoCount(); n != 1 {
			t.Fatalf("undo count = %d, want 1", n)
		}
		undoAndWant(t, s, w, cx, "")
		redoAndWant(t, s, w, cx, "你")
	})
}

// U:input/base/state.rs:6974-7001
func TestTextareaEditAfterCompositionIsSeparateUndo(t *testing.T) {
	v := buildTextarea(t, func(s TextareaState) TextareaState { return s.DefaultValue("") })
	v.update(func(s *TextareaState, w *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
		s.ReplaceAndMarkTextInRange(nil, "n", utf16Range(1, 1), w, cx)
		s.ReplaceTextInRange(nil, "你", w, cx)
		wantValue(t, s, "你")
		if n := s.undoManager.undoCount(); n != 1 {
			t.Fatalf("undo count = %d, want 1", n)
		}
		s.ReplaceTextInRange(nil, "x", w, cx)
		wantValue(t, s, "你x")
		if n := s.undoManager.undoCount(); n != 2 {
			t.Fatalf("undo count = %d, want 2", n)
		}
		undoAndWant(t, s, w, cx, "你", "")
	})
}

// U:input/base/state.rs:7003-7023
func TestTextareaCompositionCancelViaUnmarkDoesNotLeak(t *testing.T) {
	v := buildTextarea(t, func(s TextareaState) TextareaState { return s.DefaultValue("") })
	v.update(func(s *TextareaState, w *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
		s.ReplaceAndMarkTextInRange(nil, "n", utf16Range(1, 1), w, cx)
		s.UnmarkText(w, cx)
		afterCancel := s.undoManager.undoCount()
		s.ReplaceTextInRange(nil, "x", w, cx)
		if n := s.undoManager.undoCount(); n != afterCancel+1 {
			t.Fatalf("undo count = %d, want %d", n, afterCancel+1)
		}
	})
}

// U:input/base/state.rs:7442-7455
func TestTextareaKindDoesNotFollowTheRowCount(t *testing.T) {
	v := buildTextarea(t, func(s TextareaState) TextareaState { return s.AutoGrow(1, 1) })
	v.read(func(s *TextareaState) {
		if !s.IsMultiLine() || s.IsSingleLine() || s.IsCodeEditor() {
			t.Fatal("the mode marker decides the kind")
		}
	})
}

// U:input/base/state.rs:7457-7474
func TestTextareaSoftWrapIsEnabledByDefault(t *testing.T) {
	buildTextarea(t, plainTextarea).read(func(s *TextareaState) {
		if !s.softWrap {
			t.Fatal("textarea soft wrap is on by default")
		}
	})
	buildEditor(t).read(func(s *EditorState) {
		if !s.softWrap {
			t.Fatal("editor soft wrap is on by default")
		}
	})
}

// columnPosition is the window position of a row and column of the
// painted layout, vertically centered in the row.
func columnPosition[M InputModeKind](s *InputBaseState[M], row, col int, rowFraction float32) gpui.Point {
	layout := s.lastLayout
	local, _ := layout.lines[row].positionForIndex(col, layout, false)
	return gpui.Point{
		X: s.lastBounds.Origin.X + layout.lineNumberWidth + local.X,
		Y: s.lastBounds.Origin.Y + layout.lineHeight*rowFraction,
	}
}

// U:input/base/state.rs:7568-7642. The cached Ctrl-hover definition
// (extras.hover_definition, an LSP type) joins with the editor LSP ticket,
// so that step is not set up.
func TestEditorAltDragSelectsABlockAndReplacesEachRow(t *testing.T) {
	v := buildEditor(t)
	mods := []gpui.Modifiers{{Alt: true}, {Alt: true, Shift: true}}
	if isLinux() {
		mods = append(mods, gpui.Modifiers{Alt: true, Control: true})
	}
	for _, modifiers := range mods {
		setupCursors(v, "|abcd\nabcd\nabcd")
		v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) { s.Focus(w, cx) })
		var start, end gpui.Point
		v.read(func(s *EditorState) {
			start = columnPosition(s, 0, 1, 0.5)
			end = columnPosition(s, 2, 3, 2.5)
			// Land inside the third column, not on its trailing boundary.
			end.X -= 0.5
		})
		v.cx.SimulateMouseDown(start, gpui.MouseButtonLeft, modifiers)
		left := gpui.MouseButtonLeft
		v.cx.SimulateMouseMove(end, &left, modifiers)
		v.cx.SimulateMouseUp(end, gpui.MouseButtonLeft, modifiers)
		v.read(func(s *EditorState) {
			if got := selectionRanges(s); !slices.Equal(got, []InputSelection{sel(1, 3), sel(6, 8), sel(11, 13)}) {
				t.Fatalf("%+v: ranges = %v", modifiers, got)
			}
		})
		// Moving after release must leave the block intact.
		v.cx.SimulateMouseMove(start, nil, modifiers)
		v.cx.SimulateKeystrokes("x")
		assertCursors(t, v, "ax|d\nax|d\nax|d")
	}
}

// U:input/base/state.rs:7644-7673
func TestEditorAltDragExtendsUpwardFromAnExistingCursor(t *testing.T) {
	v := buildEditor(t)
	setupCursors(v, "abcd\nabcd\na|bcd")
	v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) { s.Focus(w, cx) })
	var start, end gpui.Point
	v.read(func(s *EditorState) {
		start = columnPosition(s, 0, 1, 2.5)
		end = columnPosition(s, 0, 1, 0.5)
	})
	modifiers := gpui.Modifiers{Alt: true}
	left := gpui.MouseButtonLeft
	v.cx.SimulateMouseDown(start, left, modifiers)
	v.cx.SimulateMouseMove(end, &left, modifiers)
	v.cx.SimulateMouseUp(end, left, modifiers)
	assertCursors(t, v, "a|bcd\na|bcd\na|bcd")
}

// U:input/base/state.rs:7675-7715
func TestEditorAltDragOverAShortRowKeepsTheBlockWidth(t *testing.T) {
	v := buildEditor(t)
	setupCursors(v, "abcdef\nab\nabcdef")
	v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) { s.Focus(w, cx) })
	var start, end gpui.Point
	v.read(func(s *EditorState) {
		start = columnPosition(s, 0, 1, 0.5)
		end = columnPosition(s, 0, 5, 1.5)
	})
	modifiers := gpui.Modifiers{Alt: true, Shift: true}
	left := gpui.MouseButtonLeft
	v.cx.SimulateMouseDown(start, left, modifiers)
	v.cx.SimulateMouseMove(end, &left, modifiers)
	v.cx.SimulateMouseUp(end, left, modifiers)
	v.read(func(s *EditorState) {
		if got := selectionRanges(s); !slices.Equal(got, []InputSelection{sel(1, 5), sel(8, 9)}) {
			t.Fatalf("ranges = %v, want [1..5 8..9]", got)
		}
	})
}

// U:input/base/state.rs:7717-7738
func TestEditorAltMouseReleaseOutsideEditorEndsColumnSelection(t *testing.T) {
	v := buildEditor(t)
	setupCursors(v, "|abcd\nabcd")
	var position gpui.Point
	v.read(func(s *EditorState) {
		position = gpui.Point{X: s.lastBounds.Origin.X + s.lastLayout.lineNumberWidth + 2, Y: s.lastBounds.Origin.Y + s.lastLayout.lineHeight*0.5}
	})
	modifiers := gpui.Modifiers{Alt: true}
	v.cx.SimulateMouseDown(position, gpui.MouseButtonLeft, modifiers)
	v.cx.SimulateMouseUp(gpui.Point{X: -100, Y: -100}, gpui.MouseButtonLeft, modifiers)
	v.read(func(s *EditorState) {
		if s.selecting || s.columnSelectStart != nil {
			t.Fatal("a release outside the editor ends the column selection")
		}
	})
}

func caretVisible[M InputModeKind](v *inputView[M]) bool {
	visible := false
	v.read(func(s *InputBaseState[M]) {
		visible = s.blinkCursor.Read(v.cx.App(), func(b *blinkCursor, _ *gpui.App) bool { return b.isVisible() })
	})
	return visible
}

func copyKeystroke() string {
	if isMacOS() {
		return "cmd-c"
	}
	return "ctrl-c"
}

// U:input/base/state.rs:7740-7772
func TestEditorConsumedKeystrokesKeepCursorVisible(t *testing.T) {
	v := buildEditor(t)
	setupCursors(v, "a|b")
	v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
		s.Focus(w, cx)
		s.pauseBlinkCursor(cx)
	})
	v.cx.RunUntilParked()
	v.cx.AdvanceClock(300)
	v.cx.RunUntilParked()
	if caretVisible(v) {
		t.Fatal("the cursor blinks off after the pause")
	}
	for i := range 5 {
		v.cx.SimulateKeystrokes(copyKeystroke())
		v.cx.RunUntilParked()
		v.cx.AdvanceClock(200)
		v.cx.RunUntilParked()
		if !caretVisible(v) {
			t.Fatalf("copy %d: a consumed keystroke keeps the cursor visible", i)
		}
	}
}

// U:input/base/state.rs:7774-7810
func TestEditorMultiCursorActionsRevealHiddenCarets(t *testing.T) {
	v := buildEditor(t)
	setupCursors(v, "ab\na|b\nab")
	for action := range 6 {
		v.update(func(s *EditorState, _ *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
			s.blinkCursor = gpui.NewEntity(cx, cx.Scope(), func(b *blinkCursor, _ *gpui.Context[blinkCursor]) { *b = newBlinkCursor() })
			s.blinkCursor.Update(cx, func(b *blinkCursor, bcx *gpui.Context[blinkCursor]) { b.start(bcx) })
		})
		v.cx.RunUntilParked()
		v.cx.AdvanceClock(500)
		v.cx.RunUntilParked()
		if caretVisible(v) {
			t.Fatalf("action %d: hidden phase expected", action)
		}
		v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
			switch action {
			case 0:
				s.addCursorAbove(&AddCursorAbove{}, w, cx)
			case 1:
				s.addCursorBelow(&AddCursorBelow{}, w, cx)
			case 2:
				s.selectUp(&SelectUp{}, w, cx)
			case 3:
				s.selectDown(&SelectDown{}, w, cx)
			case 4:
				s.ReplaceTextInRange(nil, "x", w, cx)
			default:
				s.backspace(&Backspace{}, w, cx)
			}
			if !s.blinkCursor.Read(cx.App(), func(b *blinkCursor, _ *gpui.App) bool { return b.isVisible() }) {
				t.Fatalf("action %d reveals the carets", action)
			}
		})
	}
}

func addCursorKeystrokes(up bool) string {
	dir := "down"
	if up {
		dir = "up"
	}
	switch {
	case isMacOS():
		return "cmd-alt-" + dir
	case isWindows():
		return "ctrl-alt-" + dir
	}
	return "alt-shift-" + dir
}

// U:input/base/state.rs:7812-7838
func TestEditorMultiCursorKeyboardDispatch(t *testing.T) {
	v := buildEditor(t)
	setupCursors(v, "ab\na|b\nab")
	v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) { s.Focus(w, cx) })
	v.cx.SimulateKeystrokes(addCursorKeystrokes(true))
	v.read(func(s *EditorState) {
		if s.selections.len() != 2 {
			t.Fatalf("selections = %d, want 2", s.selections.len())
		}
	})
	v.cx.SimulateKeystrokes(addCursorKeystrokes(false))
	v.read(func(s *EditorState) {
		if s.selections.len() != 3 {
			t.Fatalf("selections = %d, want 3", s.selections.len())
		}
	})
	v.cx.SimulateKeystrokes("x")
	assertCursors(t, v, "ax|b\nax|b\nax|b")
}

// U:input/base/state.rs:7840-7861
func TestEditorMultiCursorPlatformWordSelectionDispatch(t *testing.T) {
	v := buildEditor(t)
	setupCursors(v, "one |two\none |two")
	v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) { s.Focus(w, cx) })
	word := "ctrl-shift-"
	if isMacOS() || isLinux() {
		word = "alt-shift-"
	}
	v.cx.SimulateKeystrokes(word + "right")
	v.cx.SimulateKeystrokes("x")
	assertCursors(t, v, "one x|\none x|")
	setupCursors(v, "one two|\none two|")
	v.cx.SimulateKeystrokes(word + "left")
	v.cx.SimulateKeystrokes("x")
	assertCursors(t, v, "one x|\none x|")
}

// U:input/base/state.rs:7863-7906 (not on macOS upstream either).
func TestEditorMultiCursorHorizontalSelectionDispatch(t *testing.T) {
	if isMacOS() {
		t.Skip("cfg(not(target_os = \"macos\")) upstream")
	}
	v := buildEditor(t)
	setupCursors(v, "ab\na|b")
	v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) { s.Focus(w, cx) })
	v.cx.SimulateKeystrokes(addCursorKeystrokes(true))
	right, left := "alt-shift-right", "alt-shift-left alt-shift-left"
	if isLinux() {
		right, left = "shift-right", "shift-left shift-left"
	}
	v.cx.SimulateKeystrokes(right)
	v.read(func(s *EditorState) {
		if got := selectionRanges(s); !slices.Equal(got, []InputSelection{sel(4, 5), sel(1, 2)}) {
			t.Fatalf("ranges = %v, want [4..5 1..2]", got)
		}
	})
	v.cx.SimulateKeystrokes(left)
	v.read(func(s *EditorState) {
		if got := selectionRanges(s); !slices.Equal(got, []InputSelection{sel(3, 4), sel(0, 1)}) {
			t.Fatalf("ranges = %v, want [3..4 0..1]", got)
		}
	})
	v.cx.SimulateKeystrokes("x")
	assertCursors(t, v, "x|b\nx|b")
}

// U:input/base/state.rs:7908-7931
func TestEditorMultiCursorAltClickDispatch(t *testing.T) {
	v := buildEditor(t)
	setupCursors(v, "a|b\nab\nab")
	v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) { s.Focus(w, cx) })
	var position gpui.Point
	v.read(func(s *EditorState) {
		position = gpui.Point{X: s.lastBounds.Origin.X + s.lastLayout.lineNumberWidth + 2, Y: s.lastBounds.Origin.Y + s.lastLayout.lineHeight*1.5}
	})
	v.cx.SimulateClick(position, gpui.Modifiers{Alt: true})
	v.read(func(s *EditorState) {
		if s.selections.len() != 2 {
			t.Fatalf("selections = %d, want 2", s.selections.len())
		}
	})
}

// U:input/base/state.rs:7933-7945
func TestTextareaWordDeleteUndoRestoresCaret(t *testing.T) {
	v := buildTextarea(t, plainTextarea)
	setupCursors(v, "hello|")
	v.update(func(s *TextareaState, w *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
		s.deletePreviousWord(&DeleteToPreviousWordStart{}, w, cx)
		s.undo(&Undo{}, w, cx)
		wantSelected(t, s, 5, 5)
	})
}

// U:input/base/state.rs:7947-7959
func TestTextareaMergedDeleteUndoRestoresAllCarets(t *testing.T) {
	v := buildTextarea(t, plainTextarea)
	setupCursors(v, "a|b|c")
	v.update(func(s *TextareaState, w *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
		s.backspace(&Backspace{}, w, cx)
		s.undo(&Undo{}, w, cx)
	})
	assertCursors(t, v, "a|b|c")
}

// U:input/base/state.rs:7961-7977
func TestEditorShiftEndRespectsSoftWrapEnd(t *testing.T) {
	v := buildEditor(t)
	v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
		s.SetValue(strings.Repeat("abcdef ", 30), w, cx)
		width := float32(60)
		s.displayMap.onLayoutChanged(&width, w)
		line, _ := s.displayMap.line(0)
		if len(line.wrappedLines) <= 1 {
			t.Fatalf("the line must wrap: %v", line.wrappedLines)
		}
		boundary := line.wrappedLines[0].End
		s.moveToWithAffinity(boundary, nil, true, cx)
		s.selectToEndOfLine(&SelectToEndOfLine{}, w, cx)
		wantSelected(t, s, boundary, s.text.Len())
	})
}

// U:input/base/state.rs:7993-8013
func TestTextareaColumnSelectionStaysOnUnicodeBoundaries(t *testing.T) {
	v := buildTextarea(t, plainTextarea)
	setupCursors(v, "ab\n你好\ncd")
	v.update(func(s *TextareaState, w *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
		s.buildColumnarSelection(columnarPoint{offset: 1}, columnarPoint{offset: 11}, cx)
		text := s.Value()
		for sel := range s.selections.iter() {
			if !utf8.RuneStart(byteAt(text, sel.start)) || !utf8.RuneStart(byteAt(text, sel.end)) {
				t.Fatalf("selection %d..%d splits a character", sel.start, sel.end)
			}
		}
		s.ReplaceTextInRange(nil, "X", w, cx)
	})
}

// byteAt is the byte at i, or an ASCII byte at the end (a boundary).
func byteAt(s string, i int) byte {
	if i >= len(s) {
		return 'a'
	}
	return s[i]
}

func isMacOS() bool   { return runtime.GOOS == "darwin" }
func isWindows() bool { return runtime.GOOS == "windows" }

// U:input/base/state.rs:7319-7390
func TestEditorUnfoldAt(t *testing.T) {
	v := buildEditor(t)
	v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
		s.SetValue("a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl", w, cx)
		s.ApplyHighlighterFoldCandidates([]FoldRange{NewFoldRange(0, 5), NewFoldRange(2, 4), NewFoldRange(7, 10)}, cx)
		s.displayMap.setFolded(0, true)
		s.displayMap.setFolded(2, true)
		s.displayMap.setFolded(7, true)
	})
	for _, line := range []uint32{0, 5} {
		v.update(func(s *EditorState, _ *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
			if s.displayMap.isBufferLineHidden(int(line)) || s.UnfoldAt(NewInputPosition(line, 0), cx) {
				t.Fatalf("line %d is a visible fold edge", line)
			}
			if !s.displayMap.isFoldedAt(0) || !s.displayMap.isFoldedAt(2) || !s.displayMap.isFoldedAt(7) {
				t.Fatal("no fold opens")
			}
		})
	}
	v.update(func(s *EditorState, _ *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
		if !s.UnfoldAt(NewInputPosition(3, 0), cx) {
			t.Fatal("line 3 is hidden")
		}
		if s.displayMap.isBufferLineHidden(3) || s.displayMap.isFoldedAt(0) || s.displayMap.isFoldedAt(2) || !s.displayMap.isFoldedAt(7) {
			t.Fatal("the outer and nested folds open, the sibling does not")
		}
		if !s.displayMap.isFoldCandidate(0) || !s.displayMap.isFoldCandidate(2) {
			t.Fatal("opened ranges stay candidates")
		}
		if s.UnfoldAt(NewInputPosition(3, 0), cx) {
			t.Fatal("a second call is a no-op")
		}
	})
}

// Go-own: a painted textarea soft-wraps a long line through the legacy
// LineWrapper facade, and vertical motion steps through its visual rows.
func TestTextareaSoftWrapsAndMovesThroughVisualRows(t *testing.T) {
	v := buildModeView(t, gpui.Size{Width: 200, Height: 300}, func(w *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) TextareaState {
		return NewTextareaState(w, cx).DefaultValue(strings.Repeat("word ", 40) + "\nend")
	})
	v.draw()
	v.read(func(s *TextareaState) {
		if s.displayMap.bufferLineCount() != 2 || s.displayMap.wrapRowCount() <= 2 {
			t.Fatalf("buffer lines = %d, wrap rows = %d; want the first line wrapped", s.displayMap.bufferLineCount(), s.displayMap.wrapRowCount())
		}
		if n := len(s.lastLayout.lines[0].wrappedLines); n < 2 {
			t.Fatalf("painted visual lines = %d", n)
		}
	})
	v.update(func(s *TextareaState, w *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
		s.setCursorTo(0)
		s.down(&MoveDown{}, w, cx)
		item, _ := s.displayMap.line(0)
		second := item.wrappedLines[1]
		if c := s.Cursor(); c < second.Start || c > second.End {
			t.Fatalf("cursor %d is not on the second visual row %v", c, second)
		}
	})
}
