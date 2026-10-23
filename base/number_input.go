package base

// Port of U:number_input.rs: the Increment / Decrement actions, the
// NumberStep strategy, InputState's number stepping, the unstyled
// NumberInput spinbutton root and step_value.
//
// The NumberStep the stepping reads is InputState's number_step field
// (U:input/base/state.rs:401, default Some(Fixed(1.)) at :732, set by
// step / step_by / set_step at :9743-9835). Those live in the input
// engine (input_state.go, another ticket's file); until they land there,
// inputNumberStep (number_input_step.go) returns the upstream default.

import (
	"strconv"
	"strings"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Increment and Decrement are the number_input actions
// (U:number_input.rs:13 actions!(number_input, [Increment, Decrement])).
type (
	// Increment steps the value up.
	Increment struct{}
	// Decrement steps the value down.
	Decrement struct{}
)

// Descriptors of the number_input actions (U:number_input.rs:13).
var (
	IncrementAction = gpui.DefineUnitAction[Increment]("number_input::Increment")
	DecrementAction = gpui.DefineUnitAction[Decrement]("number_input::Decrement")
)

// numberInputContext is the NumberInput key context
// (U:number_input.rs:15).
const numberInputContext = "NumberInput"

// numberInputInit binds the step keys (U:number_input.rs:17-22).
func numberInputInit(app *gpui.App) {
	ctx := numberInputContext
	app.BindKeys(
		gpui.NewKeyBinding("up", IncrementAction.Box(Increment{}), &ctx),
		gpui.NewKeyBinding("down", DecrementAction.Box(Decrement{}), &ctx),
	)
}

// StepAction is the direction of a step (U:number_input.rs:24-28).
type StepAction uint8

const (
	// StepActionDecrement steps down.
	StepActionDecrement StepAction = iota
	// StepActionIncrement steps up.
	StepActionIncrement
)

// NumberStep is the strategy numeric editors use when stepping their
// value (U:number_input.rs:30-35): a fixed step, or a step computed from
// the current value and direction.
type NumberStep struct {
	fixed   float64
	byValue func(current float64, action StepAction, app *gpui.App) float64
}

// NumberStepFixed is NumberStep::Fixed, also `From<f64>`
// (U:number_input.rs:33, 50-54).
func NumberStepFixed(step float64) NumberStep { return NumberStep{fixed: step} }

// NumberStepByValue is NumberStep::by_value (U:number_input.rs:38-40).
func NumberStepByValue(f func(current float64, action StepAction, app *gpui.App) float64) NumberStep {
	if f == nil {
		panic("base: NumberStepByValue requires a function")
	}
	return NumberStep{byValue: f}
}

// Value is the step to apply from current in direction action
// (U:number_input.rs:42-47).
func (s NumberStep) Value(current float64, action StepAction, app *gpui.App) float64 {
	if s.byValue != nil {
		return s.byValue(current, action, app)
	}
	return s.fixed
}

// NumberInputEvent is emitted by an InputState whose stepping is
// caller-controlled (U:number_input.rs:56-59).
type NumberInputEvent struct {
	// Step is the requested direction (NumberInputEvent::Step).
	Step StepAction
}

// NumberInputEvents is InputState's NumberInputEvent emitter
// (`impl EventEmitter<NumberInputEvent> for InputState`,
// U:number_input.rs:60).
var NumberInputEvents = gpui.DefineEvent[InputState, NumberInputEvent]()

// inputApplyNumberStep applies a number-input step, or emits a step event
// when stepping is caller-controlled (U:number_input.rs:62-91
// InputState::apply_number_step). Go cannot declare a method on the
// instantiated InputBaseState[InputMode], so it is a function.
func inputApplyNumberStep(s *InputState, action StepAction, w *gpui.Window, cx *gpui.Context[InputState]) {
	if s.disabled {
		return
	}
	if step, ok := inputNumberStep(s); ok {
		value := s.UnmaskValue()
		current, ok := parseRustF64(strings.TrimSpace(value))
		if !ok {
			current = 0
		}
		stepBy := step.Value(current, action, cx.App())
		newValue, ok := StepValue(value, action, stepBy, s.numberMin, s.numberMax)
		if !ok {
			return
		}
		if s.isValidInput(newValue, cx) {
			rng := s.rangeToUTF16(NewInputSelection(0, len(s.Value())))
			s.replaceTextInRangeSilent(&rng, newValue, w, cx)
			return
		}
	}
	NumberInputEvents.Emit(cx, NumberInputEvent{Step: action})
}

// StepHandler handles a step request (U:number_input.rs:93).
type StepHandler = func(action StepAction, w *gpui.Window, app *gpui.App)

// NumberInput is an unstyled spinbutton root composed from the
// foundational InputBase frame (U:number_input.rs:96-108).
type NumberInput struct {
	gpui.Styled[*NumberInput]
	gpui.Parent[*NumberInput]

	disabled        bool
	state           gpui.Entity[InputState]
	onStep          StepHandler
	decrementButton func(*Button) *Button
	incrementButton func(*Button) *Button
	input           any
	controlsRight   bool
}

// NumberInputText is the built-in text region of a NumberInput
// (U:number_input.rs:110-118). Applications provide the editor itself as
// a child and can style this region without recreating the number
// input's fixed three-part structure.
type NumberInputText struct {
	gpui.Styled[*NumberInputText]
	gpui.Parent[*NumberInputText]
}

// NewNumberInputText is NumberInputText::new (U:number_input.rs:120-133).
func NewNumberInputText() *NumberInputText {
	return gpui.BindParent(gpui.BindStyled(&NumberInputText{}))
}

// RenderOnce implements gpui.RenderOnce (U:number_input.rs:147-155).
func (t *NumberInputText) RenderOnce(*gpui.Window, *gpui.App) gpui.AnyElement {
	style := t.Style()
	// Gap 2b: the plain upstream div() is a block.
	d := gpui.Div().Block().MinW0().Flex1()
	d.Extend(t.TakeChildren())
	return StyledExtOf(d).RefineStyle(&style).IntoElement()
}

// NewNumberInput is NumberInput::new (U:number_input.rs:158-170).
func NewNumberInput(state gpui.Entity[InputState]) *NumberInput {
	return gpui.BindParent(gpui.BindStyled(&NumberInput{state: state}))
}

// Disabled is U:number_input.rs:172-175.
func (n *NumberInput) Disabled(disabled bool) *NumberInput {
	n.disabled = disabled
	return n
}

// OnStep replaces the built-in stepping with handler
// (U:number_input.rs:177-183).
func (n *NumberInput) OnStep(handler StepHandler) *NumberInput {
	n.onStep = handler
	return n
}

// DecrementButton decorates the built-in decrement button with
// application-owned content and styles (U:number_input.rs:185-189).
func (n *NumberInput) DecrementButton(decorate func(*Button) *Button) *NumberInput {
	n.decrementButton = decorate
	return n
}

// IncrementButton decorates the built-in increment button with
// application-owned content and styles (U:number_input.rs:191-195).
func (n *NumberInput) IncrementButton(decorate func(*Button) *Button) *NumberInput {
	n.incrementButton = decorate
	return n
}

// Input decorates the built-in text region with an editor, adornments
// and styles (U:number_input.rs:197-201). input is any element value a
// parent accepts as a child.
func (n *NumberInput) Input(input any) *NumberInput {
	n.input = input
	return n
}

// ControlsRight stacks both step buttons on the right side of the text
// region (U:number_input.rs:203-207).
func (n *NumberInput) ControlsRight() *NumberInput {
	n.controlsRight = true
	return n
}

// RenderOnce implements gpui.RenderOnce (U:number_input.rs:222-327).
func (n *NumberInput) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	disabled := n.disabled
	controlsRight := n.controlsRight
	onStep := n.onStep
	if onStep == nil {
		state := n.state
		onStep = func(action StepAction, w *gpui.Window, app *gpui.App) {
			state.Update(app, func(s *InputState, cx *gpui.Context[InputState]) {
				s.Focus(w, cx)
				inputApplyNumberStep(s, action, w, cx)
			})
		}
	}
	n.state.Update(app, func(s *InputState, _ *gpui.Context[InputState]) { s.EnsureNumberMask() })
	valueText := n.state.Read(app, func(s *InputState, _ *gpui.App) string { return s.Value() })
	value, hasValue := parseRustF64(valueText)

	decrement := NewButton("decrement")
	if n.decrementButton != nil {
		decrement = n.decrementButton(decrement)
	}
	increment := NewButton("increment")
	if n.incrementButton != nil {
		increment = n.incrementButton(increment)
	}
	text := NewNumberInputText()
	if n.input != nil {
		text.Child(n.input)
	}

	// Stepping is driven from the focused editor, so the buttons never take
	// focus themselves; otherwise every press would pull the focus ring off
	// the frame and hand it straight back (U:number_input.rs:247-271).
	stepButton := func(b *Button, action StepAction) *Button {
		if controlsRight {
			b.Flex1().MinH0()
		} else {
			b.FlexNone()
		}
		return b.Focusable(false).
			Disabled(disabled).
			OnClick(func(_ *gpui.ClickEvent, w *gpui.Window, app *gpui.App) { onStep(action, w, app) })
	}
	decrement = stepButton(decrement, StepActionDecrement)
	increment = stepButton(increment, StepActionIncrement)

	text.Extend(n.TakeChildren())
	var content *gpui.DivElement
	if controlsRight {
		content = gpui.Div().Flex().ItemsCenter().SizeFull().
			Child(text).
			Child(gpui.Div().HFull().Flex().FlexCol().
				Child(increment).
				Child(decrement))
	} else {
		content = gpui.Div().Flex().ItemsCenter().SizeFull().
			Child(decrement).
			Child(text).
			Child(increment)
	}

	focusHandle := n.state.Read(app, func(s *InputState, app *gpui.App) gpui.FocusHandle { return s.FocusHandle(app) })
	root := NewInputBase(gpui.NamedIntegerElementID("number-input", uint64(n.state.EntityID()))).
		TrackFocus(focusHandle).
		Flex().
		ItemsCenter().
		Disabled(disabled).
		Role(RoleOverrideRole(gpui.RoleSpinButton))
	if hasValue {
		root.Interactivity().AriaNumericValue(value)
	}
	root.KeyContext(numberInputContext).
		OnAction(IncrementAction, func(_ *Increment, w *gpui.Window, app *gpui.App) {
			if disabled {
				app.Propagate()
			} else {
				onStep(StepActionIncrement, w, app)
			}
		}).
		OnAction(DecrementAction, func(_ *Decrement, w *gpui.Window, app *gpui.App) {
			if disabled {
				app.Propagate()
			} else {
				onStep(StepActionDecrement, w, app)
			}
		}).
		Child(content)
	style := n.Style()
	StyledExtOf(root).RefineStyle(&style)
	return root.RenderOnce(w, app)
}

// StepValue steps a numeric string while preserving decimal precision and
// range direction (U:number_input.rs:330-372 step_value). ok is false
// when the step would not move the value in its direction.
func StepValue(value string, action StepAction, step float64, min, max *float64) (string, bool) {
	fractionDigits := func(value string) int {
		_, fraction, found := strings.Cut(value, ".")
		if !found {
			return 0
		}
		return len(fraction)
	}

	current, hasCurrent := parseRustF64(strings.TrimSpace(value))
	base := 0.0
	if hasCurrent {
		base = current
	}
	var newValue float64
	switch action {
	case StepActionIncrement:
		newValue = base + step
	default:
		newValue = base - step
	}
	digits := max0(fractionDigits(value), fractionDigits(rustFloatString(step)))
	if min != nil && newValue < *min {
		newValue = *min
		digits = max0(digits, fractionDigits(rustFloatString(*min)))
	}
	if max != nil && newValue > *max {
		newValue = *max
		digits = max0(digits, fractionDigits(rustFloatString(*max)))
	}

	if hasCurrent {
		moved := newValue < current
		if action == StepActionIncrement {
			moved = newValue > current
		}
		if !moved {
			return "", false
		}
	}
	return strconv.FormatFloat(newValue, 'f', digits, 64), true
}

func max0(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// rustFloatString is f64's Display (`step.to_string()`): the shortest
// round-tripping decimal, never in exponent form.
func rustFloatString(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// parseRustF64 is `str::parse::<f64>()` (core::num::dec2flt): decimal
// digits with an optional sign, fraction and exponent, or inf / infinity
// / nan in any case. Go's ParseFloat also accepts hexadecimal mantissas
// and underscores, which Rust rejects, so those are refused first.
func parseRustF64(s string) (float64, bool) {
	body := strings.TrimLeft(s, "+-")
	if len(s)-len(body) > 1 || strings.ContainsAny(body, "_xXpP") {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		// Out-of-range literals parse to ±inf in Rust; ParseFloat
		// reports them with the infinity it rounded to.
		if ne, ok := err.(*strconv.NumError); ok && ne.Err == strconv.ErrRange {
			return v, true
		}
		return 0, false
	}
	return v, true
}
