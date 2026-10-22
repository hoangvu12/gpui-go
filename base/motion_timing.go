package base

// CSS-like animation timing (U:motion/timing.rs): a signed delay, an
// iteration count, a playback direction and an easing sampled at an
// elapsed time.
//
// Go adaptation: Duration is time.Duration (int64 nanoseconds) where Rust
// uses u128 nanosecond arithmetic; durations are expected non-negative.

import (
	"math"
	"time"
)

// SignedDuration is a delay that may start an animation inside its
// active interval (U:motion/timing.rs:5-28).
type SignedDuration struct {
	d        time.Duration
	negative bool
}

// SignedDurationZero is SignedDuration::ZERO (U:motion/timing.rs:12).
var SignedDurationZero = SignedDuration{}

// SignedDurationPositive is a delay before the animation starts
// (U:motion/timing.rs:14-16).
func SignedDurationPositive(d time.Duration) SignedDuration { return SignedDuration{d: d} }

// SignedDurationNegative starts the animation d into its active interval
// (U:motion/timing.rs:18-20).
func SignedDurationNegative(d time.Duration) SignedDuration {
	return SignedDuration{d: d, negative: true}
}

// DefaultSignedDuration is SignedDuration::ZERO (U:motion/timing.rs:30-34).
func DefaultSignedDuration() SignedDuration { return SignedDurationZero }

// Duration reports the magnitude and whether the delay is negative.
func (s SignedDuration) Duration() (time.Duration, bool) { return s.d, s.negative }

// activeElapsed is U:motion/timing.rs:22-27: a positive delay holds the
// animation until it elapsed (checked_sub; false is Rust's None), a
// negative one advances it (saturating_add).
func (s SignedDuration) activeElapsed(elapsed time.Duration) (time.Duration, bool) {
	if s.negative {
		return satAddDuration(elapsed, s.d), true
	}
	if elapsed < s.d {
		return 0, false
	}
	return elapsed - s.d, true
}

func satAddDuration(a, b time.Duration) time.Duration {
	if b > 0 && a > math.MaxInt64-b {
		return math.MaxInt64
	}
	return a + b
}

func satSubDuration(a, b time.Duration) time.Duration {
	if a < b {
		return 0
	}
	return a - b
}

// IterationCount is the number of iterations, finite or infinite
// (U:motion/timing.rs:36-46).
type IterationCount struct {
	count    uint64
	infinite bool
}

// IterationCountFinite is Finite(count).
func IterationCountFinite(count uint64) IterationCount { return IterationCount{count: count} }

// IterationCountInfinite is Infinite.
func IterationCountInfinite() IterationCount { return IterationCount{infinite: true} }

// DefaultIterationCount is Finite(1) (U:motion/timing.rs:42-46).
func DefaultIterationCount() IterationCount { return IterationCountFinite(1) }

// Finite reports the finite count, false for Infinite.
func (c IterationCount) Finite() (uint64, bool) { return c.count, !c.infinite }

// PlaybackDirection is the direction of successive iterations
// (U:motion/timing.rs:48-55). The zero value is Normal, the default.
type PlaybackDirection uint8

const (
	PlaybackDirectionNormal PlaybackDirection = iota
	PlaybackDirectionReverse
	PlaybackDirectionAlternate
	PlaybackDirectionAlternateReverse
)

// MotionPhase is the phase a timing sample falls in
// (U:motion/timing.rs:57-62).
type MotionPhase uint8

const (
	MotionPhaseBefore MotionPhase = iota
	MotionPhaseActive
	MotionPhaseAfter
)

// TimingSample is a sampled timing (U:motion/timing.rs:64-71).
type TimingSample struct {
	Phase            MotionPhase
	DirectedProgress float32
	Iteration        uint64
	Active           bool
	Finished         bool
}

// Timing is a CSS-like animation timing (U:motion/timing.rs:73-80).
type Timing struct {
	delay      SignedDuration
	duration   time.Duration
	iterations IterationCount
	direction  PlaybackDirection
	easing     Easing
}

// NewTiming is one linear iteration over duration (U:motion/timing.rs:83-91).
func NewTiming(duration time.Duration) Timing {
	return Timing{
		duration:   duration,
		iterations: IterationCountFinite(1),
		direction:  PlaybackDirectionNormal,
		easing:     EasingLinear(),
	}
}

// Delay sets the delay (U:motion/timing.rs:93-96).
func (t Timing) Delay(delay SignedDuration) Timing {
	t.delay = delay
	return t
}

// Iterations sets the iteration count (U:motion/timing.rs:98-101).
func (t Timing) Iterations(iterations IterationCount) Timing {
	t.iterations = iterations
	return t
}

// Direction sets the playback direction (U:motion/timing.rs:103-106).
func (t Timing) Direction(direction PlaybackDirection) Timing {
	t.direction = direction
	return t
}

// Ease sets the easing (U:motion/timing.rs:108-111).
func (t Timing) Ease(easing Easing) Timing {
	t.easing = easing
	return t
}

// Sample evaluates the timing after elapsed (U:motion/timing.rs:113-152).
func (t Timing) Sample(elapsed time.Duration) TimingSample {
	active, ok := t.delay.activeElapsed(elapsed)
	if !ok {
		return TimingSample{
			Phase:            MotionPhaseBefore,
			DirectedProgress: t.easing.Sample(t.directed(0, 0)),
		}
	}

	finite := !t.iterations.infinite
	count := t.iterations.count
	if t.duration == 0 || (finite && count == 0) {
		if !finite {
			count = 1
		}
		return t.afterSample(count)
	}

	durationNanos := uint64(t.duration.Nanoseconds())
	elapsedNanos := uint64(active.Nanoseconds())
	if finite {
		// duration_nanos.saturating_mul(count)
		total := uint64(math.MaxUint64)
		if count <= math.MaxUint64/durationNanos {
			total = durationNanos * count
		}
		if elapsedNanos >= total {
			return t.afterSample(count)
		}
	}

	iteration := elapsedNanos / durationNanos
	iterationNanos := elapsedNanos % durationNanos
	progress := float64(iterationNanos) / float64(durationNanos)
	return TimingSample{
		Phase:            MotionPhaseActive,
		DirectedProgress: t.easing.Sample(t.directed(iteration, float32(progress))),
		Iteration:        iteration,
		Active:           true,
	}
}

// afterSample is U:motion/timing.rs:154-163.
func (t Timing) afterSample(count uint64) TimingSample {
	iteration := count
	if iteration > 0 {
		iteration--
	}
	return TimingSample{
		Phase:            MotionPhaseAfter,
		DirectedProgress: t.easing.Sample(t.directed(iteration, 1)),
		Iteration:        iteration,
		Finished:         true,
	}
}

// directed is U:motion/timing.rs:165-173.
func (t Timing) directed(iteration uint64, progress float32) float32 {
	reverse := false
	switch t.direction {
	case PlaybackDirectionReverse:
		reverse = true
	case PlaybackDirectionAlternate:
		reverse = iteration%2 == 1
	case PlaybackDirectionAlternateReverse:
		reverse = iteration%2 == 0
	}
	if reverse {
		return 1 - progress
	}
	return progress
}
