package base

// The multi-line half of the U:input/base/state.rs test harness
// (InputView::<TextareaMode>::build_textarea, U:input/base/state.rs:
// 4845-4854) and the textarea component.

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// buildTextarea is InputView::<TextareaMode>::build_textarea.
func buildTextarea(t *testing.T, f func(TextareaState) TextareaState) *inputView[TextareaMode] {
	t.Helper()
	ta := gpui.NewTestApp()
	shutdownAtCleanup(t, ta)
	ta.Update(func(app *gpui.App) {
		theme := DefaultTheme()
		app.SetGlobal(&theme)
		inputInit(app)
	})
	var input gpui.Entity[TextareaState]
	_, cx := ta.AddWindowView(func(r *inputTestRoot[TextareaMode], w *gpui.Window, cx *gpui.Context[inputTestRoot[TextareaMode]]) {
		input = gpui.NewEntity(cx, w.Scope(), func(s *TextareaState, scx *gpui.Context[TextareaState]) {
			*s = f(NewTextareaState(w, scx))
		})
		r.input = input
	})
	return &inputView[TextareaMode]{input: input, cx: cx}
}

// draw is `window.draw(cx).clear(cx)`: the input repaints.
func (v *inputView[M]) draw() {
	v.cx.Update(func(w *gpui.Window, app *gpui.App) {
		v.input.Update(app, func(_ *InputBaseState[M], cx *gpui.Context[InputBaseState[M]]) { cx.Notify() })
	})
	v.cx.RunUntilParked()
}

func plainTextarea(s TextareaState) TextareaState { return s }

// Go-own: Textarea installs its token presentation and renders the state.
func TestTextareaRendersItsState(t *testing.T) {
	v := buildTextarea(t, func(s TextareaState) TextareaState { return s.DefaultValue("a\nb") })
	v.cx.Update(func(w *gpui.Window, app *gpui.App) {
		NewTextarea(v.input).Token(func(*InlineTokenContext, *gpui.Window, *gpui.App) gpui.AnyElement {
			return gpui.Div().Block().IntoElement()
		}).RenderOnce(w, app)
	})
	v.read(func(s *TextareaState) {
		if s.tokenPresentation.renderer == nil {
			t.Fatal("the textarea must install its token renderer")
		}
		if !s.IsMultiLine() || s.IsCodeEditor() {
			t.Fatal("a textarea is multi-line plain text")
		}
	})
}
