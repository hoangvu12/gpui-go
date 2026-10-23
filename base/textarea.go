package base

// Port of U:input/textarea/mod.rs: the unstyled multi-line Textarea over a
// TextareaState entity (the TextareaMode instantiation of the engine,
// input_state.go). Token renderers return gpui.AnyElement
// (InlineTokenRenderer) where upstream takes any `R: IntoElement`.

import "github.com/hoangvu12/gpui-go/gpui"

// Textarea is an unstyled ordinary multi-line text input
// (U:input/textarea/mod.rs:12-17).
type Textarea struct {
	presentation inlineTokenPresentation
	state        gpui.Entity[TextareaState]
}

// NewTextarea is Textarea::new (U:input/textarea/mod.rs:19-25).
func NewTextarea(state gpui.Entity[TextareaState]) *Textarea { return &Textarea{state: state} }

// Token sets the element each atomic token renders as; the input keeps
// editing and history (U:input/textarea/mod.rs:26-33).
func (t *Textarea) Token(render InlineTokenRenderer) *Textarea {
	t.presentation = t.presentation.token(render)
	return t
}

// OnTokenClick opens a reference after a completed, unconsumed token click
// (U:input/textarea/mod.rs:34-41).
func (t *Textarea) OnTokenClick(listener InlineTokenClickListener) *Textarea {
	t.presentation = t.presentation.onTokenClick(listener)
	return t
}

// RenderOnce implements gpui.RenderOnce: the textarea renders as its state
// view after installing its token presentation
// (U:input/textarea/mod.rs:44-51).
func (t *Textarea) RenderOnce(_ *gpui.Window, app *gpui.App) gpui.AnyElement {
	t.state.Update(app, func(s *TextareaState, _ *gpui.Context[TextareaState]) {
		s.setTokenPresentation(t.presentation)
	})
	element, err := gpui.TryIntoElement(gpui.ViewOf[TextareaState](t.state))
	if err != nil {
		panic("base: Textarea render: " + err.Error())
	}
	return element
}
