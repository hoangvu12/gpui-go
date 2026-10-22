package base

// Port of U:input/mod.rs (MASK_CHAR, init) and U:input/input/mod.rs (the
// unstyled single-line Input). InputState itself is the InputMode
// instantiation of the engine (input_state.go).
//
// Input's inline-token presentation (token / on_token_click,
// U:input/input/mod.rs:29-44, and the set_token_presentation call of its
// render) joins with the inline-tokens port (token_presentation.rs).

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
	state gpui.Entity[InputState]
}

// NewInput is Input::new (U:input/input/mod.rs:22-28).
func NewInput(state gpui.Entity[InputState]) *Input { return &Input{state: state} }

// RenderOnce implements gpui.RenderOnce: the input renders as its state
// view (U:input/input/mod.rs:47-54).
func (i *Input) RenderOnce(*gpui.Window, *gpui.App) gpui.AnyElement {
	element, err := gpui.TryIntoElement(gpui.ViewOf[InputState](i.state))
	if err != nil {
		panic("base: Input render: " + err.Error())
	}
	return element
}
