package base

// Ported inline-token tests of U:input/base/state.rs:4517-4763.

import (
	"errors"
	"reflect"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

func mustToken[M InputModeKind](t *testing.T, s *InputBaseState[M], rng InputSelection, token InlineToken, w *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
	t.Helper()
	if err := s.ReplaceRangeWithToken(rng, token, w, cx); err != nil {
		t.Fatalf("replace %v with token: %v", rng, err)
	}
}

func fixedWidthToken(width func() float32) InlineTokenRenderer {
	return func(*InlineTokenContext, *gpui.Window, *gpui.App) gpui.AnyElement {
		return gpui.Div().Block().W(gpui.Px(width())).IntoElement()
	}
}

// U:input/base/state.rs:4517-4557
func TestInputInlineTokenWrapAndSizeRefresh(t *testing.T) {
	width := float32(4000)
	v := buildTextarea(t, func(s TextareaState) TextareaState { return s.Rows(4).DefaultValue("@a@b") })
	v.update(func(s *TextareaState, w *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
		mustToken(t, s, sel(0, 2), NewInlineToken("a", "@a"), w, cx)
		mustToken(t, s, sel(2, 4), NewInlineToken("b", "@b"), w, cx)
		s.setTokenPresentation(inlineTokenPresentation{}.token(fixedWidthToken(func() float32 { return width })))
	})
	v.draw()
	v.read(func(s *TextareaState) {
		if n := s.displayMap.wrapRowCount(); n != 2 {
			t.Fatalf("wrap rows = %d, want 2", n)
		}
		first, ok1 := s.RangeToBounds(sel(0, 2))
		second, ok2 := s.RangeToBounds(sel(2, 4))
		if !ok1 || !ok2 || !(second.Origin.Y > first.Origin.Y) {
			t.Fatalf("the second token must wrap below the first: %v %v", first, second)
		}
		if n := s.lastLayout.lines[0].wrappedLines[0].len; n != 2 {
			t.Fatalf("first visual line len = %d, want 2", n)
		}
	})
	width = 100
	v.draw()
	v.read(func(s *TextareaState) {
		if n := s.displayMap.wrapRowCount(); n != 1 {
			t.Fatalf("wrap rows = %d, want 1", n)
		}
	})
}

// U:input/base/state.rs:4559-4604
func TestInputInlineTokenGeometryAndReentrantActivation(t *testing.T) {
	v := buildInput(t, func(s InputState) InputState { return s.DefaultValue("before @alice after") })
	target := v.input
	v.update(func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
		mustToken(t, s, sel(7, 13), NewInlineToken("a", "@alice"), w, cx)
		s.setTokenPresentation(inlineTokenPresentation{}.
			token(func(*InlineTokenContext, *gpui.Window, *gpui.App) gpui.AnyElement {
				return gpui.Div().Block().W(gpui.Px(100)).H(gpui.Px(20)).IntoElement()
			}).
			onTokenClick(func(_ *InlineTokenClickEvent, w *gpui.Window, app *gpui.App) {
				_ = target.UpdateIn(app, w, func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
					s.SetValue("activated", w, cx)
				})
			}))
		s.Focus(w, cx)
		s.SetSelectedRange(sel(7, 13), cx)
	})
	v.draw()
	v.read(func(s *InputState) {
		bounds, ok := s.RangeToBounds(sel(7, 13))
		if !ok || abs32(bounds.Size.Width-100) >= 1 {
			t.Fatalf("token bounds = %v, %v; want 100 wide", bounds, ok)
		}
		left, _ := s.indexForMousePosition(gpui.Point{X: bounds.Origin.X + 10, Y: bounds.Origin.Y + 5})
		right, _ := s.indexForMousePosition(gpui.Point{X: bounds.Origin.X + 90, Y: bounds.Origin.Y + 5})
		if left != 7 || right != 13 {
			t.Fatalf("hits = (%d, %d), want (7, 13)", left, right)
		}
	})
	v.cx.DispatchAction(ActivateTokenAction.Box(ActivateToken{}))
	v.cx.RunUntilParked()
	v.read(func(s *InputState) { wantValue(t, s, "activated") })
}

// U:input/base/state.rs:4606-4636
func TestInputInlineTokenClickSelectsIt(t *testing.T) {
	v := buildInput(t, func(s InputState) InputState { return s.DefaultValue("before @alice after") })
	v.update(func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
		mustToken(t, s, sel(7, 13), NewInlineToken("a", "@alice"), w, cx)
		s.SetSelectedRange(sel(0, 0), cx)
	})
	v.draw()
	var bounds gpui.Bounds
	v.read(func(s *InputState) {
		b, ok := s.RangeToBounds(sel(7, 13))
		if !ok {
			t.Fatal("token not laid out")
		}
		bounds = b
	})
	v.cx.SimulateClick(bounds.Center(), gpui.Modifiers{})
	v.cx.RunUntilParked()
	v.read(func(s *InputState) { wantSelected(t, s, 7, 13) })
}

// U:input/base/state.rs:4638-4688
func TestInputInlineTokenEditHistoryAndValidation(t *testing.T) {
	v := buildInput(t, func(s InputState) InputState { return s.DefaultValue("问 @alice!") })
	v.update(func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
		token := NewInlineToken("alice-1", "@alice").WithLabel("Alice")
		mustToken(t, s, sel(4, 10), token, w, cx)
		wantValue(t, s, "问 @alice!")
		if r := s.Tokens()[0].Range(); r != sel(4, 10) {
			t.Fatalf("token range = %v", r)
		}
		if s.nextEndOfWordAt(4) != 10 || s.previousStartOfWordAt(10) != 4 {
			t.Fatalf("word motion must skip the token: %d %d", s.nextEndOfWordAt(4), s.previousStartOfWordAt(10))
		}
		s.undo(&Undo{}, w, cx)
		if len(s.Tokens()) != 0 {
			t.Fatal("identity-only association is undoable")
		}
		wantValue(t, s, "问 @alice!")
		s.redo(&Redo{}, w, cx)
		if s.Tokens()[0].Token() != token {
			t.Fatal("redo restores the token")
		}
		s.SetSelectedRange(sel(6, 7), cx)
		wantSelected(t, s, 4, 10)
		s.Replace("", w, cx)
		wantValue(t, s, "问 !")
		if len(s.Tokens()) != 0 {
			t.Fatal("deleting the token removes it")
		}
		s.undo(&Undo{}, w, cx)
		if s.Tokens()[0].Token() != token {
			t.Fatal("undo restores the deleted token")
		}
		s.SetSelectedRange(sel(0, 0), cx)
		s.Replace("🙂", w, cx)
		if r := s.Tokens()[0].Range(); r != sel(8, 14) {
			t.Fatalf("shifted token range = %v, want 8..14", r)
		}
		s.undo(&Undo{}, w, cx)
		if r := s.Tokens()[0].Range(); r != sel(4, 10) {
			t.Fatalf("token range after undo = %v, want 4..10", r)
		}
		before := s.Content()
		if err := s.ReplaceRangeWithToken(sel(0, 0), token, w, cx); err != nil {
			t.Fatalf("the same reference may occur twice: %v", err)
		}
		wantValue(t, s, "@alice问 @alice!")
		if len(s.Tokens()) != 2 || s.Tokens()[1].Token() != token {
			t.Fatalf("tokens = %v", s.Tokens())
		}
		s.undo(&Undo{}, w, cx)
		if !reflect.DeepEqual(s.Content(), before) {
			t.Fatalf("content = %v, want %v", s.Content(), before)
		}
		s.SetValue(before.Text(), w, cx)
		if len(s.Tokens()) != 0 || s.undoManager.lastUndo() != nil {
			t.Fatal("a plain value drops tokens and history")
		}
	})
}

// U:input/base/state.rs:4690-4763
func TestInputInlineTokenTextareaImeAndModes(t *testing.T) {
	v := buildTextarea(t, plainTextarea)
	v.update(func(s *TextareaState, w *gpui.Window, cx *gpui.Context[InputBaseState[TextareaMode]]) {
		content, err := NewInputContent("@a@b\n后面").WithToken(sel(0, 2), NewInlineToken("a", "@a"))
		if err == nil {
			content, err = content.WithToken(sel(2, 4), NewInlineToken("b", "@b"))
		}
		if err != nil {
			t.Fatal(err)
		}
		s.SetContent(content, w, cx)
		s.SetSelectedRange(sel(2, 2), cx)
		if s.previousBoundary(2) != 0 || s.nextBoundary(2) != 4 {
			t.Fatalf("boundaries = %d, %d; want 0, 4", s.previousBoundary(2), s.nextBoundary(2))
		}
		rng := gpui.UTF16Range{Start: 1, End: 2}
		mark := gpui.UTF16Range{Start: 1, End: 1}
		s.ReplaceAndMarkTextInRange(&rng, "中", &mark, w, cx)
		wantValue(t, s, "中@b\n后面")
		if len(s.Tokens()) != 1 {
			t.Fatalf("tokens = %v", s.Tokens())
		}
		if err := s.ReplaceWithToken(NewInlineToken("x", "x"), w, cx); !errors.Is(err, InlineTokenErrorCompositionActive) {
			t.Fatalf("err = %v, want CompositionActive", err)
		}
		s.ReplaceTextInRange(nil, "中文", w, cx)
		s.undo(&Undo{}, w, cx)
		if !reflect.DeepEqual(s.Content(), content) {
			t.Fatalf("content = %v, want %v", s.Content(), content)
		}
		s.redo(&Redo{}, w, cx)
		wantValue(t, s, "中文@b\n后面")
		s.undo(&Undo{}, w, cx)
		s.ReplaceAll("plain", w, cx)
		if len(s.Tokens()) != 0 {
			t.Fatal("replace_all drops the tokens")
		}
		s.undo(&Undo{}, w, cx)
		if !reflect.DeepEqual(s.Content(), content) {
			t.Fatalf("content = %v, want %v", s.Content(), content)
		}
	})

	in := buildInput(t, plainInput)
	in.update(func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
		if _, err := NewInputContent("á").WithToken(sel(0, 1), NewInlineToken("bad", "a")); !errors.Is(err, InlineTokenErrorInvalidBoundary) {
			t.Fatalf("err = %v, want InvalidBoundary", err)
		}
		c, err := NewInputContent("@a@a").WithToken(sel(0, 2), NewInlineToken("a", "@a"))
		if err == nil {
			_, err = c.WithToken(sel(1, 3), NewInlineToken("x", "a@"))
		}
		if !errors.Is(err, InlineTokenErrorOverlappingTokens) {
			t.Fatalf("err = %v, want OverlappingTokens", err)
		}
		if err := s.ReplaceWithToken(NewInlineToken("a", "@a"), w, cx); err != nil {
			t.Fatal(err)
		}
		before := s.Content()
		s.SetMasked(true, w, cx)
		if s.tokensVisible() {
			t.Fatal("a masked input hides its tokens")
		}
		if err := s.ReplaceWithToken(NewInlineToken("b", "b"), w, cx); !errors.Is(err, InlineTokenErrorUnsupportedMode) {
			t.Fatalf("err = %v, want UnsupportedMode", err)
		}
		if !reflect.DeepEqual(s.Content(), before) {
			t.Fatal("a rejected token leaves the content alone")
		}
		s.undo(&Undo{}, w, cx)
		if len(s.Tokens()) != 0 {
			t.Fatal("undo removes the token")
		}
		s.redo(&Redo{}, w, cx)
		if !reflect.DeepEqual(s.Content(), before) || s.tokensVisible() {
			t.Fatal("redo restores the hidden token")
		}
	})
}
