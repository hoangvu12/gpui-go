package base

import (
	"errors"
	"math"
	"testing"
)

// motion/easing.rs tests (1/1) and the easing tests of motion.rs's
// css_timing_tests (3).

// U:motion/easing.rs:223-233
func TestBezierErrorsNameTheInvalidControlPoint(t *testing.T) {
	_, err := EasingCubicBezier(0.2, float32(math.NaN()), 0.8, 1)
	if !errors.Is(err, EasingErrorInvalidBezierControlPoint) {
		t.Fatalf("error = %v", err)
	}
	if err.Error() != "cubic Bézier control points must be finite and x must be within 0..=1" {
		t.Fatalf("message = %q", err.Error())
	}
	var _ error = EasingErrorInvalidBezierControlPoint
}

// U:motion.rs:705-721
func TestCSSKeywordEasingMatchesPublishedReferenceSamples(t *testing.T) {
	for _, c := range []struct {
		easing  Easing
		samples [3][2]float32
	}{
		{EasingEase(), [3][2]float32{{0.2, 0.295}, {0.5, 0.802}, {0.8, 0.976}}},
		{EasingEaseIn(), [3][2]float32{{0.2, 0.062}, {0.5, 0.315}, {0.8, 0.692}}},
		{EasingEaseOut(), [3][2]float32{{0.2, 0.308}, {0.5, 0.685}, {0.8, 0.938}}},
		{EasingEaseInOut(), [3][2]float32{{0.2, 0.082}, {0.5, 0.5}, {0.8, 0.918}}},
	} {
		for _, s := range c.samples {
			if actual := c.easing.Sample(s[0]); abs32(actual-s[1]) >= 0.002 {
				t.Fatalf("%v(%v) = %v, expected %v", c.easing, s[0], actual, s[1])
			}
		}
	}
}

// mustEasing is the upstream `.unwrap()`.
func mustEasing(t *testing.T) func(Easing, error) Easing {
	return func(e Easing, err error) Easing {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
}

// U:motion.rs:723-744
func TestStepEasingObservesCSSJumpPositions(t *testing.T) {
	start := mustEasing(t)(EasingSteps(4, StepPositionJumpStart))
	end := mustEasing(t)(EasingSteps(4, StepPositionJumpEnd))
	for _, c := range []struct {
		e        Easing
		in, want float32
	}{
		{start, 0, 0.25}, {start, 0.24, 0.25}, {start, 0.25, 0.5},
		{end, 0, 0}, {end, 0.24, 0}, {end, 0.25, 0.25},
	} {
		if got := c.e.Sample(c.in); got != c.want {
			t.Fatalf("%v(%v) = %v, want %v", c.e, c.in, got, c.want)
		}
	}
	if _, err := EasingSteps(0, StepPositionJumpEnd); err == nil {
		t.Fatal("zero steps accepted")
	}

	none := mustEasing(t)(EasingSteps(4, StepPositionJumpNone))
	both := mustEasing(t)(EasingSteps(4, StepPositionJumpBoth))
	if none.Sample(0) != 0 || abs32(none.Sample(0.5)-2.0/3.0) >= epsilonF32 || none.Sample(1) != 1 {
		t.Fatal("jump-none samples")
	}
	if both.Sample(0) != 0.2 || both.Sample(1) != 1 {
		t.Fatal("jump-both samples")
	}
	if _, err := EasingSteps(1, StepPositionJumpNone); err == nil {
		t.Fatal("a single jump-none step accepted")
	}
}

// U:motion.rs:746-761
func TestLinearStopsFillOmittedPositionsBeforeSampling(t *testing.T) {
	easing := mustEasing(t)(EasingLinearStops(
		LinearStopAt(0, 0),
		NewLinearStop(0.2),
		NewLinearStop(0.8),
		LinearStopAt(1, 1),
	))
	if abs32(easing.Sample(1.0/3.0)-0.2) >= 1e-6 || abs32(easing.Sample(0.5)-0.5) >= 1e-6 {
		t.Fatalf("samples = %v, %v", easing.Sample(1.0/3.0), easing.Sample(0.5))
	}
	if _, err := EasingLinearStops(LinearStopAt(0, 0.8), LinearStopAt(1, 0.2)); err == nil {
		t.Fatal("decreasing stop inputs accepted")
	}
}

// epsilonF32 is f32::EPSILON.
const epsilonF32 = 1.1920929e-07
