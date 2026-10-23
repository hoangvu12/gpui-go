package base

// inputNumberStep is InputState's number_step (U:input/base/state.rs:401):
// None makes the number input emit a step event instead of stepping.
func inputNumberStep(s *InputState) (NumberStep, bool) {
	if s.numberStep == nil {
		return NumberStep{}, false
	}
	return *s.numberStep, true
}
