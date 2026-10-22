package base

import "testing"

// The timing tests of motion.rs's css_timing_tests (2).

// U:motion.rs:763-773
func TestNegativeDelayStartsInsideTheActiveInterval(t *testing.T) {
	timing := NewTiming(ms(100)).Delay(SignedDurationNegative(ms(25)))
	sample := timing.Sample(0)
	if sample.Phase != MotionPhaseActive || abs32(sample.DirectedProgress-0.25) >= epsilonF32 || !sample.Active || sample.Finished {
		t.Fatalf("sample = %+v", sample)
	}
}

// U:motion.rs:775-793
func TestAlternateDirectionReversesOddIterations(t *testing.T) {
	timing := NewTiming(ms(100)).
		Iterations(IterationCountFinite(2)).
		Direction(PlaybackDirectionAlternate).
		Ease(EasingLinear())
	first := timing.Sample(ms(25))
	second := timing.Sample(ms(125))
	finished := timing.Sample(ms(200))
	if first.Iteration != 0 || first.DirectedProgress != 0.25 {
		t.Fatalf("first = %+v", first)
	}
	if second.Iteration != 1 || second.DirectedProgress != 0.75 {
		t.Fatalf("second = %+v", second)
	}
	if finished.Phase != MotionPhaseAfter || finished.DirectedProgress != 0 || !finished.Finished {
		t.Fatalf("finished = %+v", finished)
	}
}
