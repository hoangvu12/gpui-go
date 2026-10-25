package base

// Ported single-line tests of U:input/base/state.rs (see
// input_state_test.go for the harness).

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// inputObserver records an input's notifications (the pinned cx.observe).
type inputObserver struct{}

// observeInput counts the input's notifications from now on.
func observeInput[M InputModeKind](cx *gpui.VisualTestContext, input gpui.Entity[InputBaseState[M]]) *int {
	count := new(int)
	cx.Update(func(_ *gpui.Window, app *gpui.App) {
		observer := gpui.NewEntity(app, cx.RootScope(), func(*inputObserver, *gpui.Context[inputObserver]) {})
		observer.Update(app, func(_ *inputObserver, ocx *gpui.Context[inputObserver]) {
			ocx.Observe(input, func(*inputObserver, gpui.Entity[InputBaseState[M]], *gpui.Context[inputObserver]) { *count++ })
		})
	})
	cx.RunUntilParked()
	return count
}

func replaceAt[M InputModeKind](s *InputBaseState[M], start, end int, text string, w *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
	rng := s.rangeToUTF16(NewInputSelection(start, end))
	s.ReplaceTextInRange(&rng, text, w, cx)
}

func typeText[M InputModeKind](s *InputBaseState[M], w *gpui.Window, cx *gpui.Context[InputBaseState[M]], parts ...string) {
	for _, p := range parts {
		s.ReplaceTextInRange(nil, p, w, cx)
	}
}

func undoAndWant[M InputModeKind](t *testing.T, s *InputBaseState[M], w *gpui.Window, cx *gpui.Context[InputBaseState[M]], values ...string) {
	t.Helper()
	for _, v := range values {
		s.undo(&Undo{}, w, cx)
		wantValue(t, s, v)
	}
}

func redoAndWant[M InputModeKind](t *testing.T, s *InputBaseState[M], w *gpui.Window, cx *gpui.Context[InputBaseState[M]], values ...string) {
	t.Helper()
	for _, v := range values {
		s.redo(&Redo{}, w, cx)
		wantValue(t, s, v)
	}
}

func clipboardText(text string) gpui.ClipboardItem { return gpui.NewClipboardStringItem(text) }

type inputUpdate = func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]])

func plainInput(s InputState) InputState { return s }

func numberInput(s InputState) InputState {
	return s.MaskPattern(MaskPattern{Kind: MaskPatternKindNumber})
}

// inputFrame is the Frame view of single_line_is_centered_in_a_taller_frame.
type inputFrame struct{ input gpui.Entity[InputState] }

func (f *inputFrame) Render(w *gpui.Window, cx *gpui.Context[inputFrame]) gpui.AnyElement {
	return NewInputBase("frame").H(gpui.Px(60)).Child(NewInput(f.input)).RenderOnce(w, cx.App())
}

// U:input/base/state.rs:4870-4896
func TestInputSingleLineIsCenteredInATallerFrame(t *testing.T) {
	ta := gpui.NewTestApp()
	shutdownAtCleanup(t, ta)
	ta.Update(func(app *gpui.App) { inputInit(app) })
	var input gpui.Entity[InputState]
	opts := gpui.WindowOptions{Bounds: gpui.Bounds{Size: gpui.Size{Width: 400, Height: 100}}}
	tw := ta.OpenWindowWithOptions[inputFrame](opts, func(f *inputFrame, w *gpui.Window, cx *gpui.Context[inputFrame]) {
		input = gpui.NewEntity(cx, w.Scope(), func(s *InputState, scx *gpui.Context[InputState]) {
			*s = NewInputState(w, scx).DefaultValue("a")
		})
		f.input = input
	})
	tw.Draw()
	input.Read(ta.App(), func(s *InputState, _ *gpui.App) struct{} {
		if y := s.inputBounds.Center().Y; y != 30 {
			t.Fatalf("line %v center y = %v, want 30", s.inputBounds, y)
		}
		return struct{}{}
	})
}

// inputEmptyRoot is gpui::EmptyView holding an input it does not render.
type inputEmptyRoot struct{ input gpui.Entity[InputState] }

func (*inputEmptyRoot) Render(*gpui.Window, *gpui.Context[inputEmptyRoot]) gpui.AnyElement {
	return gpui.Div().Flex().IntoElement()
}

// buildUnrenderedInput opens a window whose root is empty and creates an
// input state in it, like the upstream tests over gpui::EmptyView.
func buildUnrenderedInput(t *testing.T) *inputView[InputMode] {
	t.Helper()
	ta := gpui.NewTestApp()
	shutdownAtCleanup(t, ta)
	ta.Update(func(app *gpui.App) { inputInit(app) })
	var input gpui.Entity[InputState]
	_, cx := ta.AddWindowView(func(r *inputEmptyRoot, w *gpui.Window, cx *gpui.Context[inputEmptyRoot]) {
		input = gpui.NewEntity(cx, w.Scope(), func(s *InputState, scx *gpui.Context[InputState]) {
			*s = NewInputState(w, scx)
		})
		r.input = input
	})
	return &inputView[InputMode]{input: input, cx: cx}
}

// U:input/base/state.rs:5021-5081
func TestInputScrollOffsetNotifiesOnlyOnChange(t *testing.T) {
	v := buildUnrenderedInput(t)
	v.update(func(s *InputState, _ *gpui.Window, _ *gpui.Context[InputBaseState[InputMode]]) {
		s.inputBounds = gpui.Bounds{Size: gpui.Size{Width: 100, Height: 20}}
		s.scrollSize = gpui.Size{Width: 300, Height: 20}
	})
	notifications := observeInput(v.cx, v.input)
	*notifications = 0
	for _, target := range []*gpui.Point{nil, {}, {X: 20, Y: 50}} {
		v.update(func(s *InputState, _ *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
			s.updateScrollOffset(target, cx)
		})
		if *notifications != 0 {
			t.Fatalf("an unchanged clamped offset notified (%v)", target)
		}
	}
	steps := []struct {
		target *gpui.Point
		width  float32
		want   gpui.Point
		count  int
	}{
		{&gpui.Point{X: -40}, 300, gpui.Point{X: -40}, 1},
		{&gpui.Point{X: -400, Y: 50}, 300, gpui.Point{X: -200}, 2},
		// Clamping to the current offset stays quiet.
		{&gpui.Point{X: -500}, 300, gpui.Point{X: -200}, 2},
		// A smaller scroll range clamps and notifies.
		{nil, 110, gpui.Point{X: -10}, 3},
	}
	for i, step := range steps {
		v.update(func(s *InputState, _ *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
			s.scrollSize.Width = step.width
			s.updateScrollOffset(step.target, cx)
			if o := s.scrollHandle.Offset(); o != step.want {
				t.Fatalf("step %d: offset = %v, want %v", i, o, step.want)
			}
		})
		if *notifications != step.count {
			t.Fatalf("step %d: notifications = %d, want %d", i, *notifications, step.count)
		}
	}
}

// U:input/base/state.rs:5083-5121
func TestInputSetValueOnUnfocusedInputStaysQuiet(t *testing.T) {
	v := buildUnrenderedInput(t)
	notifications := observeInput(v.cx, v.input)

	// Seeding is a write and notifies; an unfocused input draws no caret.
	v.update(func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
		s.SetValue("seeded", w, cx)
	})
	v.cx.RunUntilParked()
	settled := *notifications

	v.cx.AdvanceClock(3000)
	v.cx.RunUntilParked()
	if *notifications != settled {
		t.Fatalf("an unfocused input is blinking: %d -> %d notifications", settled, *notifications)
	}
}

// U:input/base/state.rs:5233-5249 (the single-line half; the textarea half
// joins with ticket 21)
func TestInputOnlyAMultiLineInputPaintsScrollbars(t *testing.T) {
	v := buildInput(t, plainInput)
	v.read(func(s *InputState) {
		if s.showsScrollbar() {
			t.Fatal("a single-line input paints a scrollbar")
		}
	})
}

// U:input/base/state.rs:5745-5802
func TestInputNumberInputNormalization(t *testing.T) {
	v := buildInput(t, numberInput)

	// Full-width digits and the ideographic full stop normalize; the
	// cursor is at the end in normalized bytes.
	v.update(func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
		s.ReplaceTextInRange(nil, "12。5", w, cx)
	})
	v.cx.RunUntilParked()
	v.read(func(s *InputState) {
		wantValue(t, s, "12.5")
		wantSelected(t, s, 4, 4)
	})

	// Non-numeric input is rejected.
	v.update(func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
		s.ReplaceTextInRange(nil, "abc", w, cx)
	})
	v.read(func(s *InputState) { wantValue(t, s, "12.5") })

	// A bare leading dot stays editable.
	v.update(func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
		replaceAt(s, 0, s.text.Len(), "。", w, cx)
	})
	v.read(func(s *InputState) {
		wantValue(t, s, ".")
		wantSelected(t, s, 1, 1)
	})
}

// U:input/base/state.rs:5804-5827
func TestInputNumberInputNormalizationWithSeparator(t *testing.T) {
	v := buildInput(t, func(s InputState) InputState {
		m := MaskPatternNumber(',')
		m.Fraction, m.HasFraction = 2, true
		return s.MaskPattern(m)
	})
	v.update(func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
		s.ReplaceTextInRange(nil, "1234", w, cx)
	})
	v.read(func(s *InputState) {
		wantValue(t, s, "1,234")
		if got := s.UnmaskValue(); got != "1234" {
			t.Fatalf("unmasked = %q", got)
		}
	})
}

// runInput runs body on a fresh single-line state built by build.
func runInput(t *testing.T, build func(InputState) InputState, body inputUpdate) {
	t.Helper()
	buildInput(t, build).update(body)
}

// U:input/base/state.rs:5829-5858
func TestInputNumberInputClampOnBlur(t *testing.T) {
	runInput(t, func(s InputState) InputState { return numberInput(s).Min(10).Max(100) },
		func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
			// Out-of-range values are allowed while typing, clamped on blur.
			typeText(s, w, cx, "1000")
			wantValue(t, s, "1000")
			s.clampNumberValue(w, cx)
			wantValue(t, s, "100")

			replaceAt(s, 0, s.text.Len(), "1", w, cx)
			wantValue(t, s, "1")
			s.clampNumberValue(w, cx)
			wantValue(t, s, "10")
		})
}

// U:input/base/state.rs:5860-5893
func TestInputNumberInputUndoWithMask(t *testing.T) {
	runInput(t, func(s InputState) InputState { return s.MaskPattern(MaskPatternNumber(',')) },
		func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
			typeText(s, w, cx, "1234")
			wantValue(t, s, "1,234")
			typeText(s, w, cx, "5")
			wantValue(t, s, "12,345")
			// Each whole-document mask rewrite is an atomic undo step.
			undoAndWant(t, s, w, cx, "1,234", "")
			redoAndWant(t, s, w, cx, "1,234", "12,345")
		})
}

// U:input/base/state.rs:5895-5911
func TestInputUndoManagerCoalescesAdjacentTypingTransactions(t *testing.T) {
	runInput(t, plainInput, func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
		typeText(s, w, cx, "a", "b")
		wantValue(t, s, "ab")
		undoAndWant(t, s, w, cx, "")
	})
}

// U:input/base/state.rs:5913-5933
func TestInputUndoManagerCursorMovementSplitsTyping(t *testing.T) {
	runInput(t, plainInput, func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
		typeText(s, w, cx, "a", "b")
		s.left(&MoveLeft{}, w, cx)
		typeText(s, w, cx, "x")
		wantValue(t, s, "axb")
		undoAndWant(t, s, w, cx, "ab", "")
	})
}

// U:input/base/state.rs:5935-5955
func TestInputUndoManagerSplitsBackwardAndForwardDelete(t *testing.T) {
	runInput(t, plainInput, func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
		s.SetValue("abcd", w, cx)
		s.SetSelectedRange(NewInputSelection(2, 2), cx)
		s.backspace(&Backspace{}, w, cx)
		s.delete(&Delete{}, w, cx)
		wantValue(t, s, "ad")
		undoAndWant(t, s, w, cx, "acd", "abcd")
	})
}

// U:input/base/state.rs:5957-5983
func TestInputUndoManagerCoalescesDirectionalCharacterDeletes(t *testing.T) {
	runInput(t, plainInput, func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
		s.SetValue("abcd", w, cx)
		s.backspace(&Backspace{}, w, cx)
		s.backspace(&Backspace{}, w, cx)
		wantValue(t, s, "ab")
		undoAndWant(t, s, w, cx, "abcd")
		wantSelected(t, s, 4, 4)

		s.SetValue("abcd", w, cx)
		s.SetSelectedRange(NewInputSelection(1, 1), cx)
		s.delete(&Delete{}, w, cx)
		s.delete(&Delete{}, w, cx)
		wantValue(t, s, "ad")
		undoAndWant(t, s, w, cx, "abcd")
		wantSelected(t, s, 1, 1)
	})
}

// U:input/base/state.rs:5985-6007. Deviation: the Go TestApp has no
// clipboard (App.ReadClipboard reports ErrNoHost), so the paste goes
// through insert_clipboard, the body of paste after the clipboard read.
func TestInputUndoManagerAtomicPasteIsolatedFromTyping(t *testing.T) {
	runInput(t, plainInput, func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
		typeText(s, w, cx, "a")
		s.insertClipboard(clipboardText("P"), w, cx)
		typeText(s, w, cx, "b")
		wantValue(t, s, "aPb")
		undoAndWant(t, s, w, cx, "aP", "a", "")
	})
}

// U:input/base/state.rs:6009-6028
func TestInputUndoManagerProgrammaticInsertIsAtomic(t *testing.T) {
	runInput(t, plainInput, func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
		typeText(s, w, cx, "a")
		s.Insert("P", w, cx)
		typeText(s, w, cx, "b")
		wantValue(t, s, "aPb")
		undoAndWant(t, s, w, cx, "aP", "a")
	})
}

// U:input/base/state.rs:6030-6050
func TestInputUndoManagerSelectionRoundTripSplitsTyping(t *testing.T) {
	runInput(t, plainInput, func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
		typeText(s, w, cx, "a", "b")
		s.SelectAll(w, cx)
		s.Unselect(w, cx)
		typeText(s, w, cx, "c")
		undoAndWant(t, s, w, cx, "ab", "")
	})
}

// U:input/base/state.rs:6081-6111
func TestInputUndoManagerSingleLineReturnCommitsTheTypingSession(t *testing.T) {
	runInput(t, plainInput, func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
		typeText(s, w, cx, "a", "b", "c")
		s.enter(&Enter{}, w, cx)
		typeText(s, w, cx, "d", "e", "f")
		wantValue(t, s, "abcdef")
		undoAndWant(t, s, w, cx, "abc", "")
	})
}

// U:input/base/state.rs:6141-6161
func TestInputUndoManagerBlurCommitsTheTypingSession(t *testing.T) {
	runInput(t, plainInput, func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
		typeText(s, w, cx, "before blur")
		s.onBlur(w, cx)
		s.onFocus(w, cx)
		typeText(s, w, cx, " after focus")
		wantValue(t, s, "before blur after focus")
		undoAndWant(t, s, w, cx, "before blur", "")
	})
}

// U:input/base/state.rs:6269-6305. Deviation: the Go TestApp has no
// clipboard to read back, so the test asserts what masked copy and cut
// leave observable (nothing deleted) and that IsCopyable, which gates
// both clipboard writes, flips with the mask.
func TestInputMaskedInputKeepsItsValueOutOfTheClipboard(t *testing.T) {
	runInput(t, plainInput, func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
		s.SetValue("hunter2", w, cx)
		s.SetMasked(true, w, cx)
		s.SelectAll(w, cx)
		if s.IsCopyable() {
			t.Fatal("a masked selection is copyable")
		}
		s.copy(&Copy{}, w, cx)
		// Cut neither copies nor deletes.
		s.cut(&Cut{}, w, cx)
		wantValue(t, s, "hunter2")
		// Revealing the value restores both.
		s.SetMasked(false, w, cx)
		if !s.IsCopyable() {
			t.Fatal("an unmasked selection is not copyable")
		}
	})
}

// U:input/base/state.rs:6307-6345
func TestInputMaskedInputCollapsesWordBoundaries(t *testing.T) {
	runInput(t, plainInput, func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
		s.SetValue("aaa bbb ccc", w, cx)
		s.SetMasked(true, w, cx)
		s.SetSelectedRange(NewInputSelection(7, 7), cx)

		// The mask hides word boundaries: a word delete takes everything
		// before the caret.
		s.deletePreviousWord(&DeleteToPreviousWordStart{}, w, cx)
		wantValue(t, s, " ccc")
		wantSelected(t, s, 0, 0)

		s.deleteNextWord(&DeleteToNextWordEnd{}, w, cx)
		wantValue(t, s, "")

		// A double click takes the whole value.
		s.SetValue("aaa bbb ccc", w, cx)
		s.selectWord(9, w, cx)
		wantSelected(t, s, 0, 11)

		// Unmasked, the same delete only takes one word.
		s.SetMasked(false, w, cx)
		s.SetValue("aaa bbb ccc", w, cx)
		s.SetSelectedRange(NewInputSelection(11, 11), cx)
		s.deletePreviousWord(&DeleteToPreviousWordStart{}, w, cx)
		wantValue(t, s, "aaa bbb ")

		s.SetValue("aaa bbb ccc", w, cx)
		s.selectWord(9, w, cx)
		wantSelected(t, s, 8, 11)
	})
}

// U:input/base/state.rs:6347-6366
func TestInputMaskedInputDisablesTheCopyContextMenuItems(t *testing.T) {
	runInput(t, plainInput, func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
		s.SetValue("hunter2", w, cx)
		s.SelectAll(w, cx)
		if !s.ContextMenuCapabilities().IsCopyable() {
			t.Fatal("not copyable")
		}
		s.SetMasked(true, w, cx)
		c := s.ContextMenuCapabilities()
		if !c.IsMasked() || !c.HasSelection() || c.IsCopyable() {
			t.Fatalf("masked capabilities = %+v", c)
		}
	})
}

// U:input/base/state.rs:6368-6395. Deviation: no TestApp clipboard; the
// cut text ("beta") is pasted through insert_clipboard.
func TestInputUndoManagerCutAndRepeatedPastesAreDistinctTransactions(t *testing.T) {
	runInput(t, plainInput, func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
		typeText(s, w, cx, "alpha beta gamma")
		s.SetSelectedRange(NewInputSelection(6, 10), cx)
		cut := s.SelectedValue()
		s.cut(&Cut{}, w, cx)
		wantValue(t, s, "alpha  gamma")

		s.insertClipboard(clipboardText(cut), w, cx)
		s.insertClipboard(clipboardText(cut), w, cx)
		wantValue(t, s, "alpha betabeta gamma")

		undoAndWant(t, s, w, cx, "alpha beta gamma", "alpha  gamma", "alpha beta gamma")
	})
}

// U:input/base/state.rs:6442-6466
func TestInputUndoManagerCompositionIsolatedFromLongTyping(t *testing.T) {
	runInput(t, plainInput, func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
		typeText(s, w, cx, "prefix ")
		s.ReplaceAndMarkTextInRange(nil, "n", nil, w, cx)
		s.ReplaceAndMarkTextInRange(nil, "ni", nil, w, cx)
		s.ReplaceAndMarkTextInRange(nil, "你", nil, w, cx)
		s.UnmarkText(w, cx)
		typeText(s, w, cx, " suffix")
		wantValue(t, s, "prefix 你 suffix")
		undoAndWant(t, s, w, cx, "prefix 你", "prefix ", "")
	})
}

// U:input/base/state.rs:6468-6490
func TestInputUndoManagerSelectedReplacementIsAtomic(t *testing.T) {
	runInput(t, plainInput, func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
		typeText(s, w, cx, "abc")
		s.SetSelectedRange(NewInputSelection(1, 2), cx)
		typeText(s, w, cx, "X", "z")
		wantValue(t, s, "aXzc")
		undoAndWant(t, s, w, cx, "aXc", "abc", "")
	})
}

// U:input/base/state.rs:6492-6521
func TestInputNumberInputLeadingDotEditable(t *testing.T) {
	runInput(t, numberInput, func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
		typeText(s, w, cx, "1.2")
		// Deleting the integer part keeps the leading dot.
		replaceAt(s, 0, 1, "", w, cx)
		wantValue(t, s, ".2")
		wantSelected(t, s, 0, 0)
		// A new integer part can be typed.
		s.ReplaceTextInRange(&gpui.UTF16Range{}, "3", w, cx)
		wantValue(t, s, "3.2")
	})
}

// U:input/base/state.rs:6523-6555
func TestInputNumberInputEscapeInvalidText(t *testing.T) {
	runInput(t, func(s InputState) InputState { return numberInput(s).DefaultValue("1,234") },
		func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
			// The pending text is still invalid, but so was the old text.
			replaceAt(s, 4, 5, "", w, cx)
			wantValue(t, s, "1,23")
			// Once valid, validation works as usual.
			replaceAt(s, 1, 2, "", w, cx)
			wantValue(t, s, "123")
			typeText(s, w, cx, "a")
			wantValue(t, s, "123")
		})
}

// U:input/base/state.rs:6607-6662
func TestInputReplaceAllSingleLine(t *testing.T) {
	v := buildInput(t, plainInput)
	value := "https://example.com/v1/users?" + strings.Repeat("x=1&", 120)
	n := len(value)
	v.update(func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
		s.SetValue("hello", w, cx)
		s.ReplaceAll(value, w, cx)
		wantValue(t, s, value)
		wantSelected(t, s, n, n)
		if o := s.scrollHandle.Offset(); o != (gpui.Point{}) {
			t.Fatalf("scroll offset = %v", o)
		}
		if s.deferredScrollOffset == nil || *s.deferredScrollOffset != (gpui.Point{}) {
			t.Fatalf("deferred scroll offset = %v", s.deferredScrollOffset)
		}
	})
	v.cx.RunUntilParked()
	v.read(func(s *InputState) {
		if !(s.scrollSize.Width > s.inputBounds.Size.Width) {
			t.Fatalf("value must overflow the input width")
		}
		if x := s.scrollHandle.Offset().X; x != 0 {
			t.Fatalf("scroll x = %v", x)
		}
	})
}

// U:input/base/state.rs:6664-6691. Deviation: the paste goes through
// insert_clipboard (no TestApp clipboard).
func TestInputSingleLineRemovesNewlines(t *testing.T) {
	runInput(t, func(s InputState) InputState { return s.DefaultValue("default\nvalue") },
		func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
			wantValue(t, s, "defaultvalue")
			s.SetValue("first\nsecond\r\nthird\rfourth", w, cx)
			wantValue(t, s, "firstsecondthirdfourth")
			s.SetValue("", w, cx)
			s.Insert("a\nb", w, cx)
			wantValue(t, s, "ab")

			s.SetValue("", w, cx)
			s.insertClipboard(clipboardText("a\r\nb\nc\rd"), w, cx)
			wantValue(t, s, "abcd")
		})
}

// U:input/base/state.rs:6725-6759
func TestInputReplaceAllPreservesUndoHistory(t *testing.T) {
	runInput(t, plainInput, func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
		s.SetValue("first", w, cx)
		if len(s.undoManager.undoTransactions) != 0 {
			t.Fatal("history should be empty after set_value")
		}
		s.ReplaceAll("second", w, cx)
		wantValue(t, s, "second")
		if len(s.undoManager.undoTransactions) == 0 {
			t.Fatal("replace_all should record an undo step")
		}
		undoAndWant(t, s, w, cx, "first")
		redoAndWant(t, s, w, cx, "second")
	})
}

// U:input/base/state.rs:6785-6805
func TestInputSetSelectedRange(t *testing.T) {
	runInput(t, func(s InputState) InputState { return s.DefaultValue("hello world") },
		func(s *InputState, _ *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
			s.SetSelectedRange(NewInputSelection(0, 5), cx)
			wantSelected(t, s, 0, 5)
			if got := s.SelectedText().String(); got != "hello" {
				t.Fatalf("selected text = %q", got)
			}
			s.SetSelectedRange(NewInputSelection(6, 11), cx)
			if got := s.SelectedText().String(); got != "world" {
				t.Fatalf("selected text = %q", got)
			}
			// Clamped and collapsed.
			s.SetSelectedRange(NewInputSelection(100, 100), cx)
			wantSelected(t, s, 11, 11)
		})
}

// U:input/base/state.rs:7025-7041
func TestInputSetSelectedRangeClipsToUTF8Boundaries(t *testing.T) {
	runInput(t, func(s InputState) InputState { return s.DefaultValue("éx") },
		func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
			s.SetSelectedRange(NewInputSelection(0, 1), cx)
			wantSelected(t, s, 0, 2)
			s.copy(&Copy{}, w, cx)
			s.SetSelectedRange(NewInputSelection(1, 1), cx)
			wantSelected(t, s, 0, 0)
		})
}

// U:input/base/state.rs:7043-7060
func TestInputImeSelectionIsRelativeToReplacementStart(t *testing.T) {
	runInput(t, func(s InputState) InputState { return s.DefaultValue("你好 ") },
		func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
			s.SetSelectedRange(NewInputSelection(7, 7), cx)
			s.ReplaceAndMarkTextInRange(nil, "s", &gpui.UTF16Range{Start: 1, End: 1}, w, cx)
			s.ReplaceAndMarkTextInRange(nil, "sh", &gpui.UTF16Range{Start: 2, End: 2}, w, cx)
			wantValue(t, s, "你好 sh")
			wantSelected(t, s, 9, 9)
			if m := s.imeMarkedRange; m == nil || m.start != 7 || m.end != 9 {
				t.Fatalf("marked range = %+v, want 7..9", m)
			}
		})
}

// U:input/base/state.rs:7062-7082
func TestInputUndoManagerCompositionIsOneUndoGroup(t *testing.T) {
	runInput(t, plainInput, func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
		s.SetValue("a", w, cx)
		s.ReplaceAndMarkTextInRange(nil, "s", nil, w, cx)
		s.ReplaceAndMarkTextInRange(nil, "sh", nil, w, cx)
		typeText(s, w, cx, "是")
		wantValue(t, s, "a是")
		undoAndWant(t, s, w, cx, "a")
		redoAndWant(t, s, w, cx, "a是")
	})
}

// U:input/base/state.rs:7084-7120
func TestInputUndoManagerConsecutiveCompositionsAreSeparateGroups(t *testing.T) {
	runInput(t, plainInput, func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
		s.ReplaceAndMarkTextInRange(nil, "j", nil, w, cx)
		s.ReplaceAndMarkTextInRange(nil, "jin", nil, w, cx)
		typeText(s, w, cx, "今天")
		s.ReplaceAndMarkTextInRange(nil, "w", nil, w, cx)
		s.ReplaceAndMarkTextInRange(nil, "wo", nil, w, cx)
		typeText(s, w, cx, "我们")
		wantValue(t, s, "今天我们")
		wantSelected(t, s, 12, 12)

		undoAndWant(t, s, w, cx, "今天")
		wantSelected(t, s, 6, 6)
		undoAndWant(t, s, w, cx, "")
		wantSelected(t, s, 0, 0)
		redoAndWant(t, s, w, cx, "今天")
		wantSelected(t, s, 6, 6)
		redoAndWant(t, s, w, cx, "今天我们")
		wantSelected(t, s, 12, 12)
	})
}

// U:input/base/state.rs:7122-7145
func TestInputUndoManagerTypingAfterCompositionIsASeparateGroup(t *testing.T) {
	runInput(t, plainInput, func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
		s.ReplaceAndMarkTextInRange(nil, "n", nil, w, cx)
		typeText(s, w, cx, "你")
		s.undoManager.setPendingIntent(editIntentTyping)
		typeText(s, w, cx, "a")
		s.undoManager.setPendingIntent(editIntentTyping)
		typeText(s, w, cx, "b")
		wantValue(t, s, "你ab")
		undoAndWant(t, s, w, cx, "你", "")
	})
}

// U:input/base/state.rs:7147-7163
func TestInputUndoManagerCompositionCancelLeavesNoEntry(t *testing.T) {
	runInput(t, plainInput, func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
		s.SetValue("a", w, cx)
		s.ReplaceAndMarkTextInRange(nil, "s", nil, w, cx)
		s.ReplaceAndMarkTextInRange(nil, "", nil, w, cx)
		wantValue(t, s, "a")
		if len(s.undoManager.undoTransactions) != 0 {
			t.Fatal("a canceled composition left an undo entry")
		}
	})
}

// U:input/base/state.rs:7165-7186
func TestInputUndoManagerSelectionRestoredByUndoAndRedo(t *testing.T) {
	runInput(t, plainInput, func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
		s.SetValue("abc", w, cx)
		s.SetSelectedRange(NewInputSelection(1, 2), cx)
		typeText(s, w, cx, "X")
		undoAndWant(t, s, w, cx, "abc")
		wantSelected(t, s, 1, 2)
		redoAndWant(t, s, w, cx, "aXc")
		wantSelected(t, s, 2, 2)
	})
}

// U:input/base/state.rs:7188-7205
func TestInputUndoManagerForwardDeleteRestoresCursor(t *testing.T) {
	runInput(t, plainInput, func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
		s.SetValue("abc", w, cx)
		s.SetSelectedRange(NewInputSelection(1, 1), cx)
		s.delete(&Delete{}, w, cx)
		undoAndWant(t, s, w, cx, "abc")
		wantSelected(t, s, 1, 1)
	})
}

// U:input/base/state.rs:7207-7227
func TestInputUndoManagerSelectionMovementPreservesRedo(t *testing.T) {
	runInput(t, plainInput, func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
		typeText(s, w, cx, "ab")
		s.undo(&Undo{}, w, cx)
		s.SetSelectedRange(NewInputSelection(0, 0), cx)
		redoAndWant(t, s, w, cx, "ab")

		s.undo(&Undo{}, w, cx)
		typeText(s, w, cx, "x")
		redoAndWant(t, s, w, cx, "x")
	})
}

// U:input/base/state.rs:7229-7247
func TestInputUndoManagerNoopEditPreservesRedo(t *testing.T) {
	runInput(t, plainInput, func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
		typeText(s, w, cx, "a")
		s.moveTo(0, nil, cx)
		typeText(s, w, cx, "")
		undoAndWant(t, s, w, cx, "")
		redoAndWant(t, s, w, cx, "a")
		if c := s.Cursor(); c != 1 {
			t.Fatalf("cursor = %d, want 1", c)
		}
	})
}

// U:input/base/state.rs:7249-7268
func TestInputCursorRoundTripStopsTypingCoalescing(t *testing.T) {
	runInput(t, plainInput, func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
		typeText(s, w, cx, "a")
		s.left(&MoveLeft{}, w, cx)
		s.right(&MoveRight{}, w, cx)
		typeText(s, w, cx, "b")
		undoAndWant(t, s, w, cx, "a", "")
	})
}

// U:input/base/state.rs:7270-7291
func TestInputUndoManagerNoopEditBreaksCoalescingWithoutClearingHistory(t *testing.T) {
	runInput(t, plainInput, func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
		typeText(s, w, cx, "alpha", "", "beta")
		wantValue(t, s, "alphabeta")
		undoAndWant(t, s, w, cx, "alpha", "")
	})
}

// U:input/base/state.rs:7293-7317
func TestInputUndoManagerMaskedRedoRestoresActualCursor(t *testing.T) {
	runInput(t, func(s InputState) InputState { return s.MaskPattern(MaskPatternNumber(',')) },
		func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
			s.SetValue("12345", w, cx)
			s.SetSelectedRange(NewInputSelection(2, 2), cx)
			typeText(s, w, cx, "9")
			after := s.SelectedRange()
			if after.End == len(s.Value()) {
				t.Fatalf("selection %v sits at the end of %q", after, s.Value())
			}
			s.undo(&Undo{}, w, cx)
			s.redo(&Redo{}, w, cx)
			if got := s.SelectedRange(); got != after {
				t.Fatalf("selection after redo = %v, want %v", got, after)
			}
		})
}

// U:input/base/state.rs:9059-9076
func TestInputImeRestoresOriginalSelection(t *testing.T) {
	runInput(t, plainInput, func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
		s.SetValue("abc", w, cx)
		s.SetSelectedRange(NewInputSelection(1, 2), cx)
		s.ReplaceAndMarkTextInRange(nil, "ni", nil, w, cx)
		typeText(s, w, cx, "你")
		undoAndWant(t, s, w, cx, "abc")
		wantSelected(t, s, 1, 2)
		s.redo(&Redo{}, w, cx)
		wantSelected(t, s, 4, 4)
	})
}

// U:input/base/state.rs:9693-9719. Deviation: the image-only clipboard
// item goes through insert_clipboard (no TestApp clipboard).
func TestInputPasteWithoutTextLeavesTheSelectionAlone(t *testing.T) {
	runInput(t, plainInput, func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
		s.SetValue("hello world", w, cx)
		s.SelectAll(w, cx)
		image := gpui.NewClipboardImageItem(gpui.Image{Format: gpui.ImageFormatPNG, Bytes: []byte{0x89, 'P', 'N', 'G'}})
		s.insertClipboard(image, w, cx)
		wantValue(t, s, "hello world")
		wantSelected(t, s, 0, 11)
	})
}
