package base

import (
	"errors"
	"math"
	"testing"
)

// The keyframe tests of motion.rs's motion_track_tests (2); the windowed
// keyframe playback test is in motion_test.go.

// U:motion.rs:805-833
func TestKeyframesValidateOffsetsAndSampleEachSegmentsEasing(t *testing.T) {
	if _, err := KeyframesTryNew(NewKeyframe[float32](0.2, 0), NewKeyframe[float32](1, 1)); !errors.Is(err, KeyframeErrorMissingEndpoint) {
		t.Fatalf("error = %v, want MissingEndpoint", err)
	}
	if _, err := KeyframesTryNew(
		NewKeyframe[float32](0, 0),
		NewKeyframe[float32](0.8, 1),
		NewKeyframe[float32](0.7, 2),
		NewKeyframe[float32](1, 3),
	); !errors.Is(err, KeyframeErrorOffsetsNotMonotonic) {
		t.Fatalf("error = %v, want OffsetsNotMonotonic", err)
	}

	steps := mustEasing(t)(EasingSteps(2, StepPositionJumpEnd))
	track, err := KeyframesTryNew(
		NewKeyframe[float32](0, 0).Ease(steps),
		NewKeyframe[float32](0.5, 10).Ease(EasingLinear()),
		NewKeyframe[float32](1, 20),
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range [][2]float32{{0.2, 0}, {0.3, 5}, {0.75, 15}, {1, 20}} {
		if got := track.Sample(c[0]); got != c[1] {
			t.Fatalf("sample(%v) = %v, want %v", c[0], got, c[1])
		}
	}
}

// U:motion.rs:835-841
func TestDiscreteValuesSwitchOnlyAtTheRequestedProgress(t *testing.T) {
	value, err := NewDiscrete("old", "new").SwitchAt(0.75)
	if err != nil {
		t.Fatal(err)
	}
	if value.Sample(0.749) != "old" || value.Sample(0.75) != "new" {
		t.Fatal("switch point")
	}
	if _, err := NewDiscrete(0, 1).SwitchAt(float32(math.NaN())); err == nil {
		t.Fatal("NaN switch point accepted")
	}
}
