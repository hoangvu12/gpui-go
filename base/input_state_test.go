package base

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// inputTestRoot is TestRoot (U:input/base/state.rs:4786-4792): a full-size
// div holding the input view.
type inputTestRoot[M InputModeKind] struct {
	input gpui.Entity[InputBaseState[M]]
}

func (r *inputTestRoot[M]) Render(*gpui.Window, *gpui.Context[inputTestRoot[M]]) gpui.AnyElement {
	return gpui.Div().Flex().SizeFull().Child(gpui.ViewOf[InputBaseState[M]](r.input)).IntoElement()
}

// inputView is InputView (U:input/base/state.rs:4794-4866): a state of one
// mode open in a window, with the theme and the input bindings installed.
type inputView[M InputModeKind] struct {
	input gpui.Entity[InputBaseState[M]]
	cx    *gpui.VisualTestContext
}

// buildInput is InputView::<InputMode>::build (U:input/base/state.rs:
// 4856-4866 over build_with, 4800-4827).
func buildInput(t *testing.T, f func(InputState) InputState) *inputView[InputMode] {
	t.Helper()
	ta := gpui.NewTestApp()
	shutdownAtCleanup(t, ta)
	ta.Update(func(app *gpui.App) {
		t := DefaultTheme()
		app.SetGlobal(&t)
		inputInit(app)
	})
	var input gpui.Entity[InputState]
	_, cx := ta.AddWindowView(func(r *inputTestRoot[InputMode], w *gpui.Window, cx *gpui.Context[inputTestRoot[InputMode]]) {
		input = gpui.NewEntity(cx, w.Scope(), func(s *InputState, scx *gpui.Context[InputState]) {
			*s = f(NewInputState(w, scx))
		})
		r.input = input
	})
	return &inputView[InputMode]{input: input, cx: cx}
}

// update runs f on the state with the window, like
// `cx.update(|window, cx| input.update(cx, ...))`.
func (v *inputView[M]) update(f func(s *InputBaseState[M], w *gpui.Window, cx *gpui.Context[InputBaseState[M]])) {
	v.cx.Update(func(w *gpui.Window, app *gpui.App) {
		_ = v.input.UpdateIn(app, w, f)
	})
}

// read reads the state.
func (v *inputView[M]) read(f func(s *InputBaseState[M])) {
	v.input.Read(v.cx.App(), func(s *InputBaseState[M], _ *gpui.App) struct{} { f(s); return struct{}{} })
}

func wantValue[M InputModeKind](t *testing.T, s *InputBaseState[M], want string) {
	t.Helper()
	if got := s.Value(); got != want {
		t.Fatalf("value = %q, want %q", got, want)
	}
}

func wantSelected[M InputModeKind](t *testing.T, s *InputBaseState[M], start, end int) {
	t.Helper()
	if got := s.SelectedRange(); got != NewInputSelection(start, end) {
		t.Fatalf("selected range = %v, want %d..%d", got, start, end)
	}
}

// U:input/base/state.rs:6561-6605
func TestInputSetValueSingleLineCaretAtEndViewAtStart(t *testing.T) {
	v := buildInput(t, func(s InputState) InputState { return s })
	value := "https://example.com/v1/users?" + strings.Repeat("x=1&", 120)
	n := len(value)

	v.update(func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
		s.SetValue(value, w, cx)
		wantSelected(t, s, n, n)
		if s.deferredScrollOffset == nil || *s.deferredScrollOffset != (gpui.Point{}) {
			t.Fatalf("deferred scroll offset = %v, want the start", s.deferredScrollOffset)
		}
	})

	v.cx.RunUntilParked()
	v.read(func(s *InputState) {
		if !(s.scrollSize.Width > s.inputBounds.Size.Width) {
			t.Fatalf("scroll width %v must overflow the input width %v", s.scrollSize.Width, s.inputBounds.Size.Width)
		}
		if x := s.scrollHandle.Offset().X; x != 0 {
			t.Fatalf("scroll x = %v, want 0", x)
		}
	})
}

// Go-own: a focused input takes typed text through its element input
// handler (Window.HandleInput) and its bound actions through the keymap.
func TestInputTypingThroughTheWindowReachesTheState(t *testing.T) {
	v := buildInput(t, plainInput)
	v.update(func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) { s.Focus(w, cx) })
	v.cx.RunUntilParked()
	v.cx.SimulateInput("abc")
	v.cx.SimulateKeystrokes("backspace left")
	v.cx.SimulateInput("X")
	v.read(func(s *InputState) {
		wantValue(t, s, "aXb")
		wantSelected(t, s, 2, 2)
	})
}
