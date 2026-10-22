package base

// inputNumberStep is InputState's number_step (U:input/base/state.rs:401).
// The field and its builders (step / step_by / set_step,
// U:input/base/state.rs:9743-9835) belong in the input engine
// (input_state.go), which this ticket does not own. Until they land there
// it returns the upstream default, Some(NumberStep::Fixed(1.))
// (U:input/base/state.rs:732), so every state steps internally by 1.
//
// Once the engine carries the field this becomes:
//
//	if s.numberStep == nil { return NumberStep{}, false }
//	return *s.numberStep, true
func inputNumberStep(*InputState) (NumberStep, bool) {
	return NumberStepFixed(1), true
}
