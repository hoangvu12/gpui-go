package base

// Port of U:input/mod.rs (MASK_CHAR, init) and U:input/input/mod.rs (the
// unstyled single-line Input). InputState itself is the InputMode
// instantiation of the engine (input_state.go).
//
// Token renderers return gpui.AnyElement (InlineTokenRenderer) where
// upstream takes any `R: IntoElement`.

import (
	"unicode/utf8"

	"github.com/hoangvu12/gpui-go/gpui"
)

// inputMaskChar is MASK_CHAR, the character masked modes display
// (U:input/mod.rs:10-11).
const inputMaskChar = '•'

// inputMaskCharLen is MASK_CHAR.len_utf8().
var inputMaskCharLen = utf8.RuneLen(inputMaskChar)

// InputInit binds the input key bindings (U:input/mod.rs:77-79 init →
// state::init). Base's Init calls it (U:lib.rs:241).
func InputInit(app *gpui.App) { inputInit(app) }

// Input is an unstyled single-line text input over an InputState entity
// (U:input/input/mod.rs:12-20). Applications that need a styled control
// wrap the state with their own presentation.
type Input struct {
	presentation inlineTokenPresentation
	state        gpui.Entity[InputState]
}

// NewInput is Input::new (U:input/input/mod.rs:22-28).
func NewInput(state gpui.Entity[InputState]) *Input { return &Input{state: state} }

// Token sets the element each atomic token renders as; the input keeps
// editing and history (U:input/input/mod.rs:29-36).
func (i *Input) Token(render InlineTokenRenderer) *Input {
	i.presentation = i.presentation.token(render)
	return i
}

// OnTokenClick opens a reference after a completed, unconsumed token click
// (U:input/input/mod.rs:37-44).
func (i *Input) OnTokenClick(listener InlineTokenClickListener) *Input {
	i.presentation = i.presentation.onTokenClick(listener)
	return i
}

// RenderOnce implements gpui.RenderOnce: the input renders as its state
// view after installing its token presentation (U:input/input/mod.rs:47-54).
func (i *Input) RenderOnce(_ *gpui.Window, app *gpui.App) gpui.AnyElement {
	i.state.Update(app, func(s *InputState, _ *gpui.Context[InputState]) { s.setTokenPresentation(i.presentation) })
	element, err := gpui.TryIntoElement(gpui.ViewOf[InputState](i.state))
	if err != nil {
		panic("base: Input render: " + err.Error())
	}
	return element
}
