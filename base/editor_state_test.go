package base

// Ported editor tests of U:input/base/state.rs (the 61 tickets 20 and 21
// left for the editor), in upstream order; harness in
// textarea_state_test.go. Not applicable here: the diagnostics test
// (U:input/base/state.rs:4898-4955, ticket 24), the cached-view consumer
// test (U:input/base/state.rs:4957-5019, no AnyView::cached in the Go
// core) and the native context-menu test (U:input/base/state.rs:
// 5251-5282, native menus not ported).

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/base/rope"
	"github.com/hoangvu12/gpui-go/gpui"
)

type editorFn = func(s *EditorState, w *gpui.Window, cx *inputCx[EditorMode])

func editorEnter(s *EditorState, w *gpui.Window, cx *inputCx[EditorMode]) { s.enter(&Enter{}, w, cx) }

func editorType(text string) editorFn {
	return func(s *EditorState, w *gpui.Window, cx *inputCx[EditorMode]) { s.ReplaceTextInRange(nil, text, w, cx) }
}

func editorBackspace(s *EditorState, w *gpui.Window, cx *inputCx[EditorMode]) {
	s.backspace(&Backspace{}, w, cx)
}

func editorUndo(s *EditorState, w *gpui.Window, cx *inputCx[EditorMode]) { s.undo(&Undo{}, w, cx) }
func editorRedo(s *EditorState, w *gpui.Window, cx *inputCx[EditorMode]) { s.redo(&Redo{}, w, cx) }

// testLanguages is set_test_syntax_provider's TestLanguages
// (U:input/base/state.rs:4770-4784).
type testLanguages struct {
	BaseLanguageProvider
	provider SyntaxContextProvider
}

func (l *testLanguages) SyntaxContextProvider(string) SyntaxContextProvider { return l.provider }

func setTestSyntaxProvider(provider SyntaxContextProvider, app *gpui.App) {
	SetLanguageProvider(&testLanguages{provider: provider}, app)
}

// commentAllProvider / stringAllProvider are U:input/base/state.rs:8870-8882.
type commentAllProvider struct{}

func (commentAllProvider) ContextAt(rope.Rope, int) SyntaxContext { return SyntaxContextComment }

type stringAllProvider struct{}

func (stringAllProvider) ContextAt(rope.Rope, int) SyntaxContext { return SyntaxContextString }

// syntaxFunc adapts a function to SyntaxContextProvider.
type syntaxFunc func(text rope.Rope, offset int) SyntaxContext

func (f syntaxFunc) ContextAt(text rope.Rope, offset int) SyntaxContext { return f(text, offset) }

// U:input/base/state.rs:5470-5473
func TestEditorNextSearchMatchRevealsWithPaddingAfterManualScroll(t *testing.T) {
	assertSearchRevealsWithPaddingAfterManualScroll(t, false)
}

// U:input/base/state.rs:5475-5480
func TestEditorPreviousSearchMatchRevealsWithPaddingAfterManualScroll(t *testing.T) {
	assertSearchRevealsWithPaddingAfterManualScroll(t, true)
}

// assertSearchRevealsWithPaddingAfterManualScroll is
// U:input/base/state.rs:5482-5553.
func assertSearchRevealsWithPaddingAfterManualScroll(t *testing.T, previous bool) {
	v := buildEditor(t)
	rows := make([]string, 0, 160)
	for row := range 160 {
		if row == 20 || row == 60 || row == 100 {
			rows = append(rows, fmt.Sprintf("match on row %d", row))
		} else {
			rows = append(rows, fmt.Sprintf("line %d", row))
		}
	}
	text := strings.Join(rows, "\n")
	first := strings.Index(text, "match")
	start := first + 5 + strings.Index(text[first+5:], "match")
	expected := NewInputSelection(start, start+len("match"))
	three := 3
	v.update(func(s *EditorState, w *gpui.Window, cx *inputCx[EditorMode]) {
		s.SetCursorSurroundingLines(&three, w, cx)
		s.SetValue(text, w, cx)
		s.SetSearchQuery("match", true, cx)
		if previous {
			s.searchSession.Matcher.Next()
			s.searchSession.Matcher.Next()
		}
	})
	v.cx.RunUntilParked()
	v.update(func(s *EditorState, _ *gpui.Window, cx *inputCx[EditorMode]) {
		y := float32(0)
		if !previous {
			y = -s.lastLayout.lineHeight * 80
		}
		s.SetScrollOffset(gpui.Point{Y: y}, cx)
	})
	v.cx.RunUntilParked()
	v.read(func(s *EditorState) {
		visible, _ := s.VisibleRowRange()
		if previous && visible.End > 60 {
			t.Fatalf("target must be below the viewport: %v", visible)
		}
		if !previous && visible.Start <= 60 {
			t.Fatalf("target must be above the viewport: %v", visible)
		}
	})
	v.update(func(s *EditorState, _ *gpui.Window, cx *inputCx[EditorMode]) {
		var rng InputSelection
		var ok bool
		if previous {
			rng, ok = s.PreviousSearchMatch(cx)
		} else {
			rng, ok = s.NextSearchMatch(cx)
		}
		if !ok || rng != expected {
			t.Fatalf("match = %v %v, want %v", rng, ok, expected)
		}
		if got := s.searchSession.Matcher.Label(); got != "2/3" {
			t.Fatalf("label = %q", got)
		}
	})
	v.cx.RunUntilParked()
	v.read(func(s *EditorState) {
		visible, _ := s.VisibleRowRange()
		if !visible.Contains(60) {
			t.Fatalf("visible %v must contain row 60", visible)
		}
		lh := s.lastLayout.lineHeight
		targetY := lh*60 + s.scrollHandle.Offset().Y
		if targetY < lh*2-0.1 {
			t.Fatalf("target y %v above the padding", targetY)
		}
		if targetY+lh*3 > s.lastBounds.Size.Height+0.1 {
			t.Fatalf("search must preserve the configured surrounding-line padding: y %v height %v", targetY, s.lastBounds.Size.Height)
		}
	})
}

// searchHost wants the search shortcut for its own search UI
// (U:input/base/state.rs:5555-5571).
type searchHost struct {
	editor   gpui.Entity[EditorState]
	requests *int
}

func (h *searchHost) Render(*gpui.Window, *gpui.Context[searchHost]) gpui.AnyElement {
	requests := h.requests
	return gpui.Div().SizeFull().
		OnAction(SearchAction, func(*Search, *gpui.Window, *gpui.App) { *requests++ }).
		Child(gpui.ViewOf[EditorState](h.editor)).IntoElement()
}

// pressSearchShortcut opens a focused editor inside searchHost and presses
// the search shortcut once (U:input/base/state.rs:5573-5607).
func pressSearchShortcut(t *testing.T, searchable bool) (*inputView[EditorMode], *int) {
	t.Helper()
	ta := gpui.NewTestApp()
	shutdownAtCleanup(t, ta)
	ta.Update(func(app *gpui.App) {
		theme := DefaultTheme()
		app.SetGlobal(&theme)
		inputInit(app)
	})
	requests := new(int)
	var editor gpui.Entity[EditorState]
	_, cx := ta.AddWindowView(func(h *searchHost, w *gpui.Window, cx *gpui.Context[searchHost]) {
		editor = gpui.NewEntity(cx, w.Scope(), func(s *EditorState, scx *gpui.Context[EditorState]) {
			*s = NewEditorState(w, scx).Searchable(searchable)
		})
		h.editor, h.requests = editor, requests
	})
	v := &inputView[EditorMode]{input: editor, cx: cx}
	v.update(func(s *EditorState, w *gpui.Window, cx *inputCx[EditorMode]) { s.Focus(w, cx) })
	v.cx.RunUntilParked()
	if isMacOS() {
		v.cx.SimulateKeystrokes("cmd-f")
	} else {
		v.cx.SimulateKeystrokes("ctrl-f")
	}
	v.cx.RunUntilParked()
	return v, requests
}

// U:input/base/state.rs:5609-5617
func TestEditorSearchShortcutReachesTheHostWhenNotSearchable(t *testing.T) {
	v, requests := pressSearchShortcut(t, false)
	if *requests != 1 {
		t.Fatalf("host requests = %d, want 1", *requests)
	}
	v.read(func(s *EditorState) {
		if s.SearchSession().Open || s.SearchSession().IsActive() {
			t.Fatal("a non-searchable editor must not open its search")
		}
	})
}

// U:input/base/state.rs:5619-5627
func TestEditorSearchShortcutOpensThePanelWhenSearchable(t *testing.T) {
	v, requests := pressSearchShortcut(t, true)
	if *requests != 0 {
		t.Fatalf("host requests = %d, want 0", *requests)
	}
	v.read(func(s *EditorState) {
		if !s.SearchSession().Open || !s.SearchSession().IsActive() {
			t.Fatal("a searchable editor opens its search panel")
		}
	})
}

// U:input/base/state.rs:5629-5653
func TestEditorSetSearchQueryHighlightsWithoutThePanel(t *testing.T) {
	v := buildEditorWith(t, gpui.Size{}, func(s EditorState) EditorState { return s.Searchable(false) })
	v.update(func(s *EditorState, w *gpui.Window, cx *inputCx[EditorMode]) {
		s.SetValue("foo bar foo", w, cx)
		s.SetSearchQuery("foo", true, cx)
	})
	v.cx.RunUntilParked()
	v.read(func(s *EditorState) {
		session := s.SearchSession()
		if !session.IsActive() || session.Open || session.Matcher.Len() != 2 {
			t.Fatalf("active %v open %v matches %d", session.IsActive(), session.Open, session.Matcher.Len())
		}
	})
	v.update(func(s *EditorState, _ *gpui.Window, cx *inputCx[EditorMode]) { s.CloseSearch(cx) })
	v.read(func(s *EditorState) {
		if s.SearchSession().IsActive() {
			t.Fatal("closing ends the search")
		}
	})
}

// U:input/base/state.rs:5655-5676
func TestEditorClosedSearchResyncsMatchesAfterEdits(t *testing.T) {
	v := buildEditorWith(t, gpui.Size{}, func(s EditorState) EditorState { return s.Searchable(false) })
	v.update(func(s *EditorState, w *gpui.Window, cx *inputCx[EditorMode]) {
		s.SetValue("foo bar foo", w, cx)
		s.SetSearchQuery("foo", true, cx)
		s.CloseSearch(cx)

		s.SetValue("bar foo", w, cx)
		if rng, ok := s.NextSearchMatch(cx); !ok || rng != NewInputSelection(4, 7) {
			t.Fatalf("next match = %v %v, want 4..7", rng, ok)
		}

		s.SetValue("foo foo foo", w, cx)
		s.SetSearchQuery("foo", true, cx)
		if n := s.SearchSession().Matcher.Len(); n != 3 {
			t.Fatalf("matches = %d, want 3", n)
		}
	})
}

// U:input/base/state.rs:5678-5715
func TestEditorSearchRevealsOffscreenWrappedMatch(t *testing.T) {
	v := buildEditor(t)
	text := "match\n" + strings.Repeat("line\n", 80) + "\n" + strings.Repeat("wrapped text ", 500) + "match\n" + strings.Repeat("line\n", 80)
	three := 3
	v.update(func(s *EditorState, w *gpui.Window, cx *inputCx[EditorMode]) {
		s.SetCursorSurroundingLines(&three, w, cx)
		s.SetValue(text, w, cx)
		s.SetSearchQuery("match", true, cx)
	})
	v.cx.RunUntilParked()
	v.update(func(s *EditorState, _ *gpui.Window, cx *inputCx[EditorMode]) {
		if _, ok := s.NextSearchMatch(cx); !ok {
			t.Fatal("a next match")
		}
	})
	v.cx.RunUntilParked()
	v.read(func(s *EditorState) {
		rng := s.searchSession.Matcher.MatchedRanges()[1]
		_, _, position, ok := s.lineAndPositionForOffset(rng.End)
		if !ok {
			t.Fatal("wrapped match must be laid out")
		}
		lh := s.lastLayout.lineHeight
		y := position.Y + s.scrollHandle.Offset().Y
		if y < lh*2-0.1 || y+lh*3 > s.lastBounds.Size.Height+0.1 {
			t.Fatalf("wrapped match must retain surrounding display rows: y %v height %v", y, s.lastBounds.Size.Height)
		}
	})
}

// recordingHighlighter records the texts each update hands it
// (U:input/base/state.rs:6875-6915).
type recordingHighlighter struct{ updates *[][]string }

func (h *recordingHighlighter) Language() string { return "recording" }
func (h *recordingHighlighter) Update(_ *InputEdit, text rope.Rope, _ bool, _ *gpui.Window, _ *gpui.Context[EditorState]) {
	*h.updates = append(*h.updates, []string{text.String()})
}
func (h *recordingHighlighter) UpdateBatch(edits []InputEditWithText, _ bool, _ *gpui.Window, _ *gpui.Context[EditorState]) {
	texts := make([]string, 0, len(edits))
	for _, e := range edits {
		texts = append(texts, e.Text.String())
	}
	*h.updates = append(*h.updates, texts)
}
func (h *recordingHighlighter) Styles(rng gpui.TextRange, _ HighlightStyleResolver) []gpui.HighlightRange {
	return []gpui.HighlightRange{{Range: rng}}
}
func (h *recordingHighlighter) FoldRanges(rope.Rope) []FoldRange { return nil }

// U:input/base/state.rs:6866-6946
func TestEditorReplaceTextInRangesDrivesTheHighlighterOnce(t *testing.T) {
	var updates [][]string
	v := buildEditor(t)
	v.update(func(s *EditorState, w *gpui.Window, cx *inputCx[EditorMode]) {
		s.SetValue("aaa bbb ccc", w, cx)
		s.SetHighlighterFactory(func(string) InputHighlighter { return &recordingHighlighter{updates: &updates} }, cx)
		s.replaceTextInRanges([]inputTextEdit{{rng: NewInputSelection(0, 3), text: "X"}, {rng: NewInputSelection(8, 11), text: "Y"}}, w, cx)
	})
	want := [][]string{{"aaa bbb Y", "X bbb Y"}}
	if len(updates) != 1 || !slices.Equal(updates[0], want[0]) {
		t.Fatalf("updates = %q, want %q", updates, want)
	}
}

// U:input/base/state.rs:7393-7440 (deviation: the hover-popover half
// needs present_hover, an LSP type of ticket 24; the decoration half runs).
func TestEditorBlurKeepsDecorations(t *testing.T) {
	v := buildEditor(t)
	v.update(func(s *EditorState, w *gpui.Window, cx *inputCx[EditorMode]) {
		s.SetValue("select 1", w, cx)
		bold := float32(700)
		s.CreateDecorationsCollection([]TextDecoration{NewTextDecoration(NewInputSelection(0, 6), gpui.HighlightStyle{FontWeight: &bold})}, cx)
		s.onBlur(w, cx)
		nonEmpty := false
		for _, layer := range s.extras.decorationLayers() {
			nonEmpty = nonEmpty || len(layer) > 0
		}
		if !nonEmpty {
			t.Fatal("blur must not discard decorations")
		}
	})
}

// U:input/base/state.rs:7979-7991
func TestTextareaOutdentUnindentedUnicodeIsUnchanged(t *testing.T) {
	v := buildTextarea(t, plainTextarea)
	setupCursors(v, "|你好\n|世界")
	v.update(func(s *TextareaState, w *gpui.Window, cx *inputCx[TextareaMode]) {
		s.outdent(false, w, cx)
		s.outdent(true, w, cx)
	})
	assertCursors(t, v, "|你好\n|世界")
}

// U:input/base/state.rs:8015-8035
func TestEditorDecorationsFollowTyping(t *testing.T) {
	v := buildEditor(t)
	v.update(func(s *EditorState, w *gpui.Window, cx *inputCx[EditorMode]) {
		s.SetValue("abc def", w, cx)
		s.CreateDecorationsCollection([]TextDecoration{NewTextDecoration(NewInputSelection(4, 7), gpui.HighlightStyle{})}, cx)
		s.SetSelectedRange(NewInputSelection(0, 0), cx)
		s.ReplaceTextInRange(nil, "X", w, cx)
		layers := s.extras.decorationLayers()
		if got := layers[0][0].Range; got != NewInputSelection(5, 8) {
			t.Fatalf("decoration = %v, want 5..8", got)
		}
	})
}

// U:input/base/state.rs:8037-8049
func TestEditorBackspacePreservesEscapedQuoteTerminator(t *testing.T) {
	v := buildEditor(t)
	setupCursors(v, `{"s":"\"|"}`)
	v.update(func(s *EditorState, w *gpui.Window, cx *inputCx[EditorMode]) {
		setTestSyntaxProvider(stringAllProvider{}, cx.App())
		s.backspace(&Backspace{}, w, cx)
	})
	assertCursors(t, v, `{"s":"\|"}`)
}

// U:input/base/state.rs:8051-8071
func TestEditorOrdinaryTypingDoesNotQuerySyntax(t *testing.T) {
	v := buildEditor(t)
	setupCursors(v, "|")
	v.update(func(s *EditorState, w *gpui.Window, cx *inputCx[EditorMode]) {
		setTestSyntaxProvider(syntaxFunc(func(rope.Rope, int) SyntaxContext {
			panic("ordinary typing must not query syntax")
		}), cx.App())
		for _, c := range []string{"a", "b", "c"} {
			s.ReplaceTextInRange(nil, c, w, cx)
		}
	})
	assertCursors(t, v, "abc|")
}

// U:input/base/state.rs:8073-8091
func TestEditorAutoCloseIsAtomicAtHistoryLimit(t *testing.T) {
	v := buildEditor(t)
	setupCursors(v, "|")
	v.update(func(s *EditorState, w *gpui.Window, cx *inputCx[EditorMode]) {
		for range 999 {
			s.ReplaceTextInRange(nil, "a", w, cx)
		}
		s.ReplaceTextInRange(nil, "(", w, cx)
		s.undo(&Undo{}, w, cx)
		if strings.ContainsAny(s.text.String(), "()") {
			t.Fatalf("undo must remove the whole pair: %q", s.text.String()[990:])
		}
		s.redo(&Redo{}, w, cx)
	})
	assertCursors(t, v, strings.Repeat("a", 999)+"(|)")
}

// U:input/base/state.rs:8093-8105
func TestEditorCommentCloserIsInsertedLiterally(t *testing.T) {
	v := buildEditor(t)
	setupCursors(v, "// (|)")
	v.update(func(s *EditorState, w *gpui.Window, cx *inputCx[EditorMode]) {
		setTestSyntaxProvider(commentAllProvider{}, cx.App())
		s.ReplaceTextInRange(nil, ")", w, cx)
	})
	assertCursors(t, v, "// ()|)")
}

// U:input/base/state.rs:8107-8130
func TestEditorExplicitRangeSkipDoesNotDeleteExistingCloserOnUndo(t *testing.T) {
	v := buildEditor(t)
	setupCursors(v, "(a|)")
	var before string
	v.read(func(s *EditorState) { before = s.text.String() })
	v.update(func(s *EditorState, w *gpui.Window, cx *inputCx[EditorMode]) {
		s.ReplaceTextInRange(utf16Range(2, 2), ")", w, cx)
	})
	assertCursors(t, v, "(a)|")
	v.update(editorUndo)
	v.read(func(s *EditorState) {
		if s.text.String() != before {
			t.Fatalf("skip must not remove an existing closer on undo: %q", s.text.String())
		}
	})
}

// U:input/base/state.rs:8132-8150
func TestEditorPairRedoRestoresInteriorCursor(t *testing.T) {
	v := buildEditor(t)
	setupCursors(v, "|")
	v.update(editorType("("))
	assertCursors(t, v, "(|)")
	v.update(func(s *EditorState, w *gpui.Window, cx *inputCx[EditorMode]) {
		s.undo(&Undo{}, w, cx)
		s.redo(&Redo{}, w, cx)
	})
	assertCursors(t, v, "(|)")
}

// U:input/base/state.rs:8152-8181
func TestEditorPairEnterRedoRestoresInteriorCursor(t *testing.T) {
	v := buildEditor(t)
	setupCursors(v, "{|}")
	v.update(editorEnter)
	assertCursors(t, v, "{\n  |\n}")
	v.update(editorUndo)
	assertCursors(t, v, "{|}")
	v.update(editorRedo)
	assertCursors(t, v, "{\n  |\n}")
}

// U:input/base/state.rs:8183-8201
func TestEditorAutoCloseParens(t *testing.T) {
	v := buildEditor(t)
	setupCursors(v, "|")
	v.update(editorType("("))
	assertCursors(t, v, "(|)")
	// Collapsed: typing next inserts, not replaces.
	v.update(editorType("x"))
	assertCursors(t, v, "(x|)")
}

// U:input/base/state.rs:8203-8219
func TestEditorAutoCloseSkipOverCloser(t *testing.T) {
	v := buildEditor(t)
	setupCursors(v, "(|)")
	v.update(editorType(")"))
	assertCursors(t, v, "()|")
	v.read(func(s *EditorState) {
		if !s.activeSelection().isEmpty() {
			t.Fatal("the selection must be collapsed")
		}
	})
}

// U:input/base/state.rs:8221-8233
func TestEditorAutoCloseNoPairInsideWord(t *testing.T) {
	v := buildEditor(t)
	setupCursors(v, "don|")
	v.update(editorType("'"))
	assertCursors(t, v, "don'|")
}

// U:input/base/state.rs:8235-8248
func TestEditorAutoCloseQuoteAfterCjk(t *testing.T) {
	v := buildEditor(t)
	setupCursors(v, "中|")
	v.update(editorType("'"))
	assertCursors(t, v, "中'|")
}

// U:input/base/state.rs:8250-8265
func TestEditorAutoCloseCloserSkipsAtStringEnd(t *testing.T) {
	v := buildEditor(t)
	setupCursors(v, "\"hello|\"")
	v.update(editorType("\""))
	assertCursors(t, v, "\"hello\"|")
	v.read(func(s *EditorState) {
		if !s.activeSelection().isEmpty() {
			t.Fatal("the selection must be collapsed")
		}
	})
}

// U:input/base/state.rs:8267-8278
func TestEditorBackspacePairWithCjkPrefix(t *testing.T) {
	v := buildEditor(t)
	setupCursors(v, "中(|)abc")
	v.update(editorBackspace)
	assertCursors(t, v, "中|abc")
}

// U:input/base/state.rs:8280-8300
func TestEditorEnterSplitRespectsSmartIndentOff(t *testing.T) {
	v := buildEditor(t)
	setupCursors(v, "{|}")
	v.update(func(s *EditorState, w *gpui.Window, cx *inputCx[EditorMode]) {
		s.SetSmartIndent(false, w, cx)
		s.enter(&Enter{}, w, cx)
	})
	assertCursors(t, v, "{\n|}")
}

// U:input/base/state.rs:8302-8322
func TestEditorEnterSplitIsIndependentOfAutoClose(t *testing.T) {
	v := buildEditor(t)
	setupCursors(v, "{|}")
	v.update(func(s *EditorState, w *gpui.Window, cx *inputCx[EditorMode]) {
		s.SetAutoClose(false, w, cx)
		s.enter(&Enter{}, w, cx)
	})
	assertCursors(t, v, "{\n  |\n}")
}

// U:input/base/state.rs:8324-8361
func TestEditorLanguageConfigAppliesBeforeRenderAndOnLanguageChange(t *testing.T) {
	v := buildEditor(t)
	v.cx.Update(func(w *gpui.Window, app *gpui.App) {
		SetLanguageConfig("alpha", DefaultLanguageConfig().WithAutoClosingPairs(NewAutoClosingPair("«", "»")), app)
		SetLanguageConfig("beta", DefaultLanguageConfig().WithAutoClosingPairs(NewAutoClosingPair("‹", "›")), app)
		first := gpui.NewEntity(app, w.Scope(), func(s *EditorState, cx *gpui.Context[EditorState]) {
			*s = NewEditorState(w, cx).Language("ALPHA")
		})
		second := gpui.NewEntity(app, w.Scope(), func(s *EditorState, cx *gpui.Context[EditorState]) {
			*s = NewEditorState(w, cx).Language("beta")
		})
		_ = first.UpdateIn(app, w, func(s *EditorState, w *gpui.Window, cx *inputCx[EditorMode]) {
			s.ReplaceTextInRange(nil, "«", w, cx)
			wantValue(t, s, "«»")
			s.SetValue("", w, cx)
			s.SetSmartIndent(false, w, cx)
			s.SetHighlighter("beta", cx)
			if s.mode.isSmartIndent() {
				t.Fatal("a language change keeps the smart-indent preference")
			}
			s.ReplaceTextInRange(nil, "‹", w, cx)
			wantValue(t, s, "‹›")
			s.SetValue("", w, cx)
			s.SetHighlighter("unknown", cx)
			s.ReplaceTextInRange(nil, "(", w, cx)
			wantValue(t, s, "()")
		})
		_ = second.UpdateIn(app, w, func(s *EditorState, w *gpui.Window, cx *inputCx[EditorMode]) {
			s.ReplaceTextInRange(nil, "‹", w, cx)
			wantValue(t, s, "‹›")
		})
	})
}

// stringLanguages counts the syntax providers it creates
// (U:input/base/state.rs:8366-8372).
type stringLanguages struct {
	BaseLanguageProvider
	creations *int
}

func (l *stringLanguages) SyntaxContextProvider(string) SyntaxContextProvider {
	*l.creations++
	return stringAllProvider{}
}

// U:input/base/state.rs:8363-8396
func TestEditorLanguageServiceReplacementUpdatesSyntaxWithoutRender(t *testing.T) {
	v := buildEditor(t)
	v.cx.Update(func(w *gpui.Window, app *gpui.App) {
		creations := 0
		SetLanguageProvider(&stringLanguages{creations: &creations}, app)
		_ = v.input.UpdateIn(app, w, func(s *EditorState, w *gpui.Window, cx *inputCx[EditorMode]) {
			s.ReplaceTextInRange(nil, "(", w, cx)
			s.ReplaceTextInRange(nil, "[", w, cx)
			wantValue(t, s, "([")
			if creations != 1 {
				t.Fatalf("retain the document provider between edits: %d creations", creations)
			}
		})
		SetLanguageProvider(&BaseLanguageProvider{}, app)
		_ = v.input.UpdateIn(app, w, func(s *EditorState, w *gpui.Window, cx *inputCx[EditorMode]) {
			s.ReplaceTextInRange(nil, "{", w, cx)
			wantValue(t, s, "([{}")
		})
	})
}

// U:input/base/state.rs:8398-8425
func TestEditorLanguageConfigUpdatesExistingEditors(t *testing.T) {
	v := buildEditor(t)
	setupCursors(v, "|")
	v.cx.Update(func(w *gpui.Window, app *gpui.App) {
		_ = v.input.UpdateIn(app, w, func(s *EditorState, w *gpui.Window, cx *inputCx[EditorMode]) {
			s.SetHighlighter("CUSTOM", cx)
			s.SetAutoClose(false, w, cx)
			s.SetSmartIndent(false, w, cx)
		})
		SetLanguageConfig("custom", DefaultLanguageConfig().WithAutoClosingPairs(NewAutoClosingPair("«", "»")), app)
		// Batched registrations must update every affected language.
		SetLanguageConfig("other", DefaultLanguageConfig(), app)
		_ = v.input.UpdateIn(app, w, func(s *EditorState, w *gpui.Window, cx *inputCx[EditorMode]) {
			if s.mode.isAutoClose() || s.mode.isSmartIndent() {
				t.Fatal("registrations keep the editor preferences")
			}
			s.SetAutoClose(true, w, cx)
			s.ReplaceTextInRange(nil, "«", w, cx)
		})
	})
	assertCursors(t, v, "«|»")
}

// U:input/base/state.rs:8427-8450
func TestEditorAutoCloseBeforeIsLanguageConfigurable(t *testing.T) {
	v := buildEditor(t)
	setupCursors(v, "|word")
	v.update(editorType("("))
	assertCursors(t, v, "(|word")
	setupCursors(v, "|word")
	v.update(func(s *EditorState, w *gpui.Window, cx *inputCx[EditorMode]) {
		SetLanguageConfig(s.LanguageName(), DefaultLanguageConfig().WithAutoCloseBefore("w"), cx.App())
		s.ReplaceTextInRange(nil, "(", w, cx)
	})
	assertCursors(t, v, "(|)word")
}

// U:input/base/state.rs:8452-8474
func TestEditorPairContextRestrictionsArePerPair(t *testing.T) {
	v := buildEditor(t)
	setupCursors(v, "|")
	v.update(func(s *EditorState, w *gpui.Window, cx *inputCx[EditorMode]) {
		setTestSyntaxProvider(commentAllProvider{}, cx.App())
		SetLanguageConfig(s.LanguageName(), DefaultLanguageConfig().WithAutoClosingPairs(
			NewAutoClosingPair("(", ")").WithNotIn(SyntaxContextString),
			NewAutoClosingPair("[", "]").WithNotIn(SyntaxContextComment),
		), cx.App())
		s.ReplaceTextInRange(nil, "(", w, cx)
		s.ReplaceTextInRange(nil, "[", w, cx)
	})
	assertCursors(t, v, "([|)")
}

// U:input/base/state.rs:8476-8520
func TestEditorMulticharDelimitersInsertDeleteAndSkip(t *testing.T) {
	blockComment := syntaxFunc(func(text rope.Rope, _ int) SyntaxContext {
		if strings.Contains(text.String(), "/*") {
			return SyntaxContextComment
		}
		return SyntaxContextCode
	})
	v := buildEditor(t)
	setupCursors(v, "|")
	v.update(func(s *EditorState, w *gpui.Window, cx *inputCx[EditorMode]) {
		setTestSyntaxProvider(blockComment, cx.App())
		SetLanguageConfig(s.LanguageName(), DefaultLanguageConfig().WithAutoClosingPairs(
			NewAutoClosingPair("/*", "*/").WithNotIn(SyntaxContextString, SyntaxContextComment),
		), cx.App())
		s.ReplaceTextInRange(nil, "/", w, cx)
		s.ReplaceTextInRange(nil, "*", w, cx)
	})
	assertCursors(t, v, "/*|*/")
	v.update(editorBackspace)
	assertCursors(t, v, "|")
	v.update(func(s *EditorState, w *gpui.Window, cx *inputCx[EditorMode]) {
		s.undo(&Undo{}, w, cx)
		s.ReplaceTextInRange(nil, "*", w, cx)
		s.ReplaceTextInRange(nil, "/", w, cx)
	})
	assertCursors(t, v, "/**/|")
}

// U:input/base/state.rs:8522-8543
func TestEditorAutoClosedPairsFallbackAndExplicitDisable(t *testing.T) {
	v := buildEditor(t)
	setupCursors(v, "|")
	v.update(func(s *EditorState, w *gpui.Window, cx *inputCx[EditorMode]) {
		rules := DefaultLanguageConfig().WithBrackets(NewBracketPair("«", "»"))
		rules.AutoClosingPairs = nil
		SetLanguageConfig(s.LanguageName(), rules, cx.App())
		s.ReplaceTextInRange(nil, "«", w, cx)
		SetLanguageConfig(s.LanguageName(), rules.WithAutoClosingPairs(), cx.App())
		s.ReplaceTextInRange(nil, "«", w, cx)
	})
	assertCursors(t, v, "««|»")
}

// U:input/base/state.rs:8545-8571
func TestEditorUndoOpenCompositionPreservesGeneratedPairs(t *testing.T) {
	v := buildEditor(t)
	setupCursors(v, "|")
	v.update(func(s *EditorState, w *gpui.Window, cx *inputCx[EditorMode]) {
		SetLanguageConfig(s.LanguageName(), DefaultLanguageConfig().WithAutoClosingPairs(
			NewAutoClosingPair("/*", "*/").WithNotIn(SyntaxContextComment),
		), cx.App())
		s.ReplaceTextInRange(nil, "/", w, cx)
		s.ReplaceTextInRange(nil, "*", w, cx)
		setTestSyntaxProvider(commentAllProvider{}, cx.App())
		s.ReplaceAndMarkTextInRange(nil, "x", utf16Range(1, 1), w, cx)
		s.undo(&Undo{}, w, cx)
		s.ReplaceTextInRange(nil, "*", w, cx)
		s.ReplaceTextInRange(nil, "/", w, cx)
	})
	assertCursors(t, v, "/**/|")
}

// U:input/base/state.rs:8573-8596
func TestEditorUndoDoesNotPromoteLiteralCommentDelimiters(t *testing.T) {
	v := buildEditor(t)
	setupCursors(v, "/*|*/")
	v.update(func(s *EditorState, w *gpui.Window, cx *inputCx[EditorMode]) {
		setTestSyntaxProvider(commentAllProvider{}, cx.App())
		SetLanguageConfig(s.LanguageName(), DefaultLanguageConfig().WithAutoClosingPairs(
			NewAutoClosingPair("/*", "*/").WithNotIn(SyntaxContextComment),
		), cx.App())
		s.deleteToEndOfLine(&DeleteToEndOfLine{}, w, cx)
		s.undo(&Undo{}, w, cx)
		s.backspace(&Backspace{}, w, cx)
	})
	assertCursors(t, v, "/|*/")
}

// U:input/base/state.rs:8598-8648
func TestEditorMultipleGeneratedPairsRetainTheirIdentity(t *testing.T) {
	commentScope := syntaxFunc(func(text rope.Rope, offset int) SyntaxContext {
		if end := strings.Index(text.String(), "*/"); end >= 0 && offset < end+2 {
			return SyntaxContextComment
		}
		return SyntaxContextCode
	})
	v := buildEditor(t)
	setupCursors(v, "|")
	v.update(func(s *EditorState, w *gpui.Window, cx *inputCx[EditorMode]) {
		setTestSyntaxProvider(commentScope, cx.App())
		SetLanguageConfig(s.LanguageName(), DefaultLanguageConfig().WithAutoClosingPairs(
			NewAutoClosingPair("/*", "*/").WithNotIn(SyntaxContextComment),
			NewAutoClosingPair("(", ")").WithNotIn(SyntaxContextComment),
		), cx.App())
		s.ReplaceTextInRange(nil, "/", w, cx)
		s.ReplaceTextInRange(nil, "*", w, cx)
		s.SetSelectedRange(NewInputSelection(4, 4), cx)
		s.ReplaceTextInRange(nil, "(", w, cx)
		s.SetSelectedRange(NewInputSelection(2, 2), cx)
		s.ReplaceTextInRange(nil, "*", w, cx)
		s.ReplaceTextInRange(nil, "/", w, cx)
	})
	assertCursors(t, v, "/**/|()")
	v.update(func(s *EditorState, w *gpui.Window, cx *inputCx[EditorMode]) {
		s.SetSelectedRange(NewInputSelection(2, 2), cx)
		s.backspace(&Backspace{}, w, cx)
		s.undo(&Undo{}, w, cx)
		s.redo(&Redo{}, w, cx)
	})
	assertCursors(t, v, "|()")
}

// U:input/base/state.rs:8650-8674
func TestEditorReplacingGeneratedOpenerInvalidatesThePair(t *testing.T) {
	v := buildEditor(t)
	setupCursors(v, "|")
	v.update(func(s *EditorState, w *gpui.Window, cx *inputCx[EditorMode]) {
		SetLanguageConfig(s.LanguageName(), DefaultLanguageConfig().WithAutoClosingPairs(
			NewAutoClosingPair("/*", "*/").WithNotIn(SyntaxContextComment),
		), cx.App())
		s.ReplaceTextInRange(nil, "/", w, cx)
		s.ReplaceTextInRange(nil, "*", w, cx)
		s.ReplaceTextInRange(utf16Range(0, 2), "//", w, cx)
		setTestSyntaxProvider(commentAllProvider{}, cx.App())
		s.ReplaceTextInRange(nil, "*", w, cx)
	})
	assertCursors(t, v, "//*|*/")
}

// U:input/base/state.rs:8676-8706 (the compiled regex carries its
// case-insensitivity as Go's `(?i)` flag, the RegexBuilder option).
func TestEditorConfigurationPreservesCompiledRegexOptions(t *testing.T) {
	v := buildEditor(t)
	setupCursors(v, "BEGIN|")
	v.update(func(s *EditorState, w *gpui.Window, cx *inputCx[EditorMode]) {
		rules := func(insensitive bool) LanguageConfig {
			pattern := "begin$"
			if insensitive {
				pattern = "(?i)" + pattern
			}
			return DefaultLanguageConfig().WithIndentationRules(NewIndentationRules(regexp.MustCompile(pattern), regexp.MustCompile("^end")))
		}
		SetLanguageConfig(s.LanguageName(), rules(false), cx.App())
		SetLanguageConfig(s.LanguageName(), rules(true), cx.App())
		s.enter(&Enter{}, w, cx)
	})
	assertCursors(t, v, "BEGIN\n  |")
}

// U:input/base/state.rs:8708-8734
func TestEditorIndentPatternsAreAppliedOnEnter(t *testing.T) {
	v := buildEditor(t)
	rules := DefaultLanguageConfig().WithIndentationRules(NewIndentationRules(
		regexp.MustCompile(`\bbegin\s*$`), regexp.MustCompile(`^\s*end\b`)))
	for _, step := range []struct{ before, expected string }{{"begin|", "begin\n  |"}, {"  |end", "  \n|end"}} {
		setupCursors(v, step.before)
		v.update(func(s *EditorState, w *gpui.Window, cx *inputCx[EditorMode]) {
			SetLanguageConfig(s.LanguageName(), rules, cx.App())
			s.enter(&Enter{}, w, cx)
		})
		assertCursors(t, v, step.expected)
	}
}

// U:input/base/state.rs:8736-8768
func TestEditorCustomLanguageConfig(t *testing.T) {
	v := buildEditor(t)
	v.update(func(s *EditorState, _ *gpui.Window, cx *inputCx[EditorMode]) {
		SetLanguageConfig(s.LanguageName(), DefaultLanguageConfig().
			WithBrackets(NewBracketPair("«", "»")).
			WithAutoClosingPairs(NewAutoClosingPair("«", "»")), cx.App())
	})
	setupCursors(v, "|")
	v.update(editorType("«"))
	assertCursors(t, v, "«|»")
	// The default `(` no longer pairs under the custom rules.
	v.update(func(s *EditorState, w *gpui.Window, cx *inputCx[EditorMode]) {
		s.SetValue("\n", w, cx)
		s.SetSelectedRange(NewInputSelection(0, 0), cx)
		s.ReplaceTextInRange(nil, "(", w, cx)
	})
	assertCursors(t, v, "(|")
}

// U:input/base/state.rs:8770-8795
func TestEditorSkipRecordsNoHistory(t *testing.T) {
	v := buildEditor(t)
	setupCursors(v, "(a|)")
	v.update(editorType("b"))
	assertCursors(t, v, "(ab|)")
	v.update(editorType(")"))
	assertCursors(t, v, "(ab)|")
	v.update(editorUndo)
	assertCursors(t, v, "(a|)")
}

// U:input/base/state.rs:8797-8814
func TestEditorPairInsertUndoRemovesBoth(t *testing.T) {
	v := buildEditor(t)
	setupCursors(v, "|")
	v.update(editorType("("))
	assertCursors(t, v, "(|)")
	v.update(editorUndo)
	assertCursors(t, v, "|")
}

// U:input/base/state.rs:8816-8828
func TestEditorEscapedQuoteDoesNotSkip(t *testing.T) {
	v := buildEditor(t)
	setupCursors(v, "\"hello\\|\"")
	v.update(editorType("\""))
	assertCursors(t, v, "\"hello\\\"|\"")
}

// U:input/base/state.rs:8830-8842
func TestEditorEvenBackslashesStillSkip(t *testing.T) {
	v := buildEditor(t)
	setupCursors(v, "\"hello\\\\|\"")
	v.update(editorType("\""))
	assertCursors(t, v, "\"hello\\\\\"|")
}

// U:input/base/state.rs:8844-8868
func TestEditorEditorOptionsSurviveConfigurationReplacement(t *testing.T) {
	v := buildEditor(t)
	v.update(func(s *EditorState, w *gpui.Window, cx *inputCx[EditorMode]) {
		s.SetSmartIndent(false, w, cx)
		SetLanguageConfig(s.LanguageName(), DefaultLanguageConfig().WithBrackets(NewBracketPair("«", "»")), cx.App())
	})
	v.read(func(s *EditorState) {
		if s.mode.isSmartIndent() || !s.mode.isAutoClose() {
			t.Fatal("editor options are preserved")
		}
		rules, _ := s.mode.languageConfig()
		if len(rules.Brackets) != 1 || rules.Brackets[0].Open != "«" {
			t.Fatalf("pairs follow the configuration: %v", rules.Brackets)
		}
	})
}

// U:input/base/state.rs:8884-8908
func TestEditorSmartIndentSuppressedInStrings(t *testing.T) {
	v := buildEditor(t)
	v.cx.Update(func(_ *gpui.Window, app *gpui.App) { setTestSyntaxProvider(stringAllProvider{}, app) })
	setupCursors(v, "  x = {|")
	v.update(editorEnter)
	assertCursors(t, v, "  x = {\n  |")
}

// U:input/base/state.rs:8910-8934
func TestEditorSplitSuppressedInStrings(t *testing.T) {
	v := buildEditor(t)
	v.cx.Update(func(_ *gpui.Window, app *gpui.App) { setTestSyntaxProvider(stringAllProvider{}, app) })
	setupCursors(v, "{|}")
	v.update(editorEnter)
	assertCursors(t, v, "{\n|}")
}

// U:input/base/state.rs:8936-8953
func TestEditorCommentContextDisablesPairing(t *testing.T) {
	v := buildEditor(t)
	v.cx.Update(func(_ *gpui.Window, app *gpui.App) { setTestSyntaxProvider(commentAllProvider{}, app) })
	setupCursors(v, "|")
	v.update(editorType("("))
	assertCursors(t, v, "(|")
}

// U:input/base/state.rs:8955-8965
func TestEditorBackspaceDeletesEmptyPair(t *testing.T) {
	v := buildEditor(t)
	setupCursors(v, "{|}")
	v.update(editorBackspace)
	assertCursors(t, v, "|")
}

// U:input/base/state.rs:8967-8978
func TestEditorBackspaceKeepsCloserWithContent(t *testing.T) {
	v := buildEditor(t)
	setupCursors(v, "{d|}")
	v.update(editorBackspace)
	assertCursors(t, v, "{|}")
}

// U:input/base/state.rs:8980-9008
func TestEditorEnterSplitsBrackets(t *testing.T) {
	v := buildEditor(t)
	setupCursors(v, "{|}")
	v.update(editorEnter)
	assertCursors(t, v, "{\n  |\n}")
	v.read(func(s *EditorState) {
		if !s.activeSelection().isEmpty() {
			t.Fatal("the selection must be collapsed")
		}
	})
	v.update(editorType("x"))
	assertCursors(t, v, "{\n  x|\n}")
}

// U:input/base/state.rs:9010-9028
func TestEditorEnterSmartIndentAfterBrace(t *testing.T) {
	v := buildEditor(t)
	setupCursors(v, "fn main() {|")
	v.update(editorEnter)
	assertCursors(t, v, "fn main() {\n  |")
}

// U:input/base/state.rs:9030-9045
func TestTextareaBlockIndentTracksAllPrecedingEdits(t *testing.T) {
	v := buildTextarea(t, plainTextarea)
	setupCursors(v, "|ab\n|cd\n|ef")
	v.update(func(s *TextareaState, w *gpui.Window, cx *inputCx[TextareaMode]) { s.indent(true, w, cx) })
	assertCursors(t, v, "  |ab\n  |cd\n  |ef")
	v.update(func(s *TextareaState, w *gpui.Window, cx *inputCx[TextareaMode]) { s.outdent(true, w, cx) })
	assertCursors(t, v, "|ab\n|cd\n|ef")
}

// U:input/base/state.rs:9047-9057
func TestTextareaBlockOutdentClampsCursorInsideIndent(t *testing.T) {
	v := buildTextarea(t, plainTextarea)
	setupCursors(v, "ab\n | cd")
	v.update(func(s *TextareaState, w *gpui.Window, cx *inputCx[TextareaMode]) { s.outdent(true, w, cx) })
	assertCursors(t, v, "ab\n|cd")
}

// U:input/base/state.rs:9346-9361
func TestTextareaMultiCursorIndentIsOneUndo(t *testing.T) {
	v := buildTextarea(t, plainTextarea)
	setupCursors(v, "|1\n|2\n|3")
	v.update(func(s *TextareaState, w *gpui.Window, cx *inputCx[TextareaMode]) {
		s.indentInline(&IndentInline{}, w, cx)
		if n := s.undoManager.undoCount(); n != 1 {
			t.Fatalf("undo count = %d, want 1", n)
		}
		s.undo(&Undo{}, w, cx)
	})
	assertCursors(t, v, "|1\n|2\n|3")
}

// U:input/base/state.rs:9363-9384
func TestTextareaMultiCursorIndentThenOutdentRoundtrips(t *testing.T) {
	v := buildTextarea(t, plainTextarea)
	setupCursors(v, "|1\n|2\n|3")
	v.update(func(s *TextareaState, w *gpui.Window, cx *inputCx[TextareaMode]) { s.indent(false, w, cx) })
	assertCursors(t, v, "  |1\n  |2\n  |3")
	v.update(func(s *TextareaState, w *gpui.Window, cx *inputCx[TextareaMode]) { s.outdent(false, w, cx) })
	assertCursors(t, v, "|1\n|2\n|3")
}

// U:input/base/state.rs:9386-9417
func TestTextareaInlineOutdentOnlyRemovesLineIndentation(t *testing.T) {
	v := buildTextarea(t, plainTextarea)
	indent := func(s *TextareaState, w *gpui.Window, cx *inputCx[TextareaMode]) { s.indent(false, w, cx) }
	outdent := func(s *TextareaState, w *gpui.Window, cx *inputCx[TextareaMode]) { s.outdent(false, w, cx) }
	setupCursors(v, "1|2\n1|2")
	v.update(indent)
	assertCursors(t, v, "1  |2\n1  |2")
	v.update(outdent)
	assertCursors(t, v, "1  |2\n1  |2")
	setupCursors(v, "  1|2\n  1|2")
	v.update(outdent)
	assertCursors(t, v, "1|2\n1|2")
}

// U:input/base/state.rs:9458-9492
func TestTextareaBlockIndentOutdentWithSelection(t *testing.T) {
	v := buildTextarea(t, plainTextarea)
	v.update(func(s *TextareaState, w *gpui.Window, cx *inputCx[TextareaMode]) {
		s.SetValue("line1\nline2\nline3", w, cx)
		s.selections.replaceAll([]cursorSelection{newCursorSelection(s.selections.generateID(), 0, 17)})
		cx.Notify()
	})
	v.update(func(s *TextareaState, w *gpui.Window, cx *inputCx[TextareaMode]) { s.indent(true, w, cx) })
	v.read(func(s *TextareaState) { wantValue(t, s, "  line1\n  line2\n  line3") })
	v.update(func(s *TextareaState, w *gpui.Window, cx *inputCx[TextareaMode]) { s.outdent(true, w, cx) })
	v.read(func(s *TextareaState) { wantValue(t, s, "line1\nline2\nline3") })
}
