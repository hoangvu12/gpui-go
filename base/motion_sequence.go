package base

// Chained value transitions (U:motion/sequence.rs).

import (
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

// SequenceStep is one target of a Sequence and the transition that
// reaches it (U:motion/sequence.rs:5-24).
type SequenceStep[T any] struct {
	target     T
	transition Transition
}

// NewSequenceStep is U:motion/sequence.rs:13-15.
func NewSequenceStep[T any](target T, transition Transition) SequenceStep[T] {
	return SequenceStep[T]{target: target, transition: transition}
}

// Target is the step's target (U:motion/sequence.rs:17-19).
func (s SequenceStep[T]) Target() T { return s.target }

// Transition is the step's transition (U:motion/sequence.rs:21-23).
func (s SequenceStep[T]) Transition() Transition { return s.transition }

// SequenceSample is what a Sequence reports for the current frame
// (U:motion/sequence.rs:26-64).
type SequenceSample[T any] struct {
	value  T
	step   int
	status MotionStatus
}

// Value is the interpolated value for this frame (U:motion/sequence.rs:35-38).
func (s SequenceSample[T]) Value() T { return s.value }

// IntoValue is Value (U:motion/sequence.rs:40-42).
func (s SequenceSample[T]) IntoValue() T { return s.value }

// Step is the index of the step being played. A finished sequence reports
// its last step; an empty one reports 0 (U:motion/sequence.rs:44-48).
func (s SequenceSample[T]) Step() int { return s.step }

// Status is the status of the step being played. Finished is reported
// only once the last step has completed: a step that hands over to the
// next one reports that next step's Delayed or Running instead
// (U:motion/sequence.rs:50-55).
func (s SequenceSample[T]) Status() MotionStatus { return s.status }

// IsFinished reports the Finished status (U:motion/sequence.rs:57-59).
func (s SequenceSample[T]) IsFinished() bool { return s.status == MotionStatusFinished }

// Sequence is a chain of value transitions, each starting when the
// previous one ends (U:motion/sequence.rs:66-96).
//
// A sequence begins at from on the frame it is first sampled and runs its
// steps in order. A step ends at an absolute instant — its start plus its
// delay and duration — and the next step starts at that same instant, so
// a frame that lands past a boundary samples the next step where it would
// have been rather than starting it late. Steps of zero duration complete
// within the frame that reaches them.
//
// State is keyed by id exactly as MotionTransition keys its own, and the
// sequence plays once per key; to replay, include an application-owned
// generation in the id.
//
// A step's target and transition are captured when the step starts.
// Handing the step being played a different target restarts the sequence
// from its first step at the value sampled at that instant; steps other
// than the one being played are read when the sequence reaches them. A
// sequence does not reverse.
//
// Under reduced motion the sequence adopts its last target at once,
// reports Finished, and requests no frame.
type Sequence[T comparable] struct {
	id    TransitionID
	from  T
	steps []SequenceStep[T]
}

// NewSequence starts a sequence at from, with no steps yet
// (U:motion/sequence.rs:102-109).
func NewSequence[I IntoTransitionID, T comparable](id I, from T) Sequence[T] {
	return Sequence[T]{id: TransitionIDOf(id), from: from}
}

// WithStep appends a step that transitions to target once the previous
// step ends (U:motion/sequence.rs:111-115).
func (s Sequence[T]) WithStep(target T, transition Transition) Sequence[T] {
	return s.WithSteps(NewSequenceStep(target, transition))
}

// WithSteps appends steps built elsewhere, in order
// (U:motion/sequence.rs:117-121).
func (s Sequence[T]) WithSteps(steps ...SequenceStep[T]) Sequence[T] {
	s.steps = append(append([]SequenceStep[T](nil), s.steps...), steps...)
	return s
}

// From is the starting value (U:motion/sequence.rs:123-125).
func (s Sequence[T]) From() T { return s.from }

// Steps are the steps (U:motion/sequence.rs:127-129).
func (s Sequence[T]) Steps() []SequenceStep[T] { return s.steps }

// Sample samples the sequence and requests a frame while a step is active
// (U:motion/sequence.rs:131-208). The steps are played by the core's
// Transition policies (Progress, Sample, FinishesAfter, InterpolateValue);
// the chaining state is Base's. Reduced motion comes from the core's
// single source (App.MotionReduced).
func (s Sequence[T]) Sample(w *gpui.Window, cx *gpui.App) SequenceSample[T] {
	if len(s.steps) == 0 {
		return SequenceSample[T]{value: s.from, step: 0, status: MotionStatusIdle}
	}
	lastStep := len(s.steps) - 1
	last := s.steps[lastStep]

	key := gpui.NamedChildElementID(s.id.Key(), "__sequence")
	now := cx.Now()
	state := w.UseKeyedState[sequenceState[T]](key, cx, func(*gpui.Window, *gpui.Context[sequenceState[T]]) sequenceState[T] {
		return startSequenceState(0, s.from, s.steps, now)
	})
	snapshot := state.Read(cx, func(st *sequenceState[T], _ *gpui.App) sequenceState[T] { return *st })
	write := func(next sequenceState[T]) {
		state.Update(cx, func(st *sequenceState[T], _ *gpui.Context[sequenceState[T]]) { *st = next })
	}

	if cx.MotionReduced() {
		if snapshot.step != lastStep || snapshot.from != last.target {
			write(startSequenceState(lastStep, last.target, s.steps, now))
		}
		return SequenceSample[T]{value: last.target, step: lastStep, status: MotionStatusFinished}
	}

	progress, status := snapshot.progress(now)
	retargeted := snapshot.step > lastStep || s.steps[snapshot.step].target != snapshot.target
	handingOver := status == MotionStatusFinished && snapshot.step < lastStep

	if !retargeted && !handingOver {
		sample := SequenceSample[T]{value: snapshot.value(progress), step: snapshot.step, status: status}
		if status.IsActive() {
			gpui.RequestAnimationFrame(w)
		}
		return sample
	}

	// Only a boundary or a restart writes state. A restart begins the new
	// sequence's first step from the value the old one had reached; a
	// hand-over starts the next step at the instant the finished one
	// ended, not at now (U:motion/sequence.rs:180-189).
	var next sequenceState[T]
	if retargeted {
		next = startSequenceState(0, snapshot.value(progress), s.steps, now)
	} else {
		next = snapshot.handOver(s.steps)
	}
	var value T
	for {
		p, st := next.progress(now)
		if st == MotionStatusFinished && next.step < lastStep {
			next = next.handOver(s.steps)
			continue
		}
		value, status = next.value(p), st
		break
	}
	sample := SequenceSample[T]{value: value, step: next.step, status: status}
	write(next)
	if status.IsActive() {
		gpui.RequestAnimationFrame(w)
	}
	return sample
}

// sequenceState is the step a sequence is playing, complete enough to
// sample without the caller's steps (U:motion/sequence.rs:211-220).
type sequenceState[T any] struct {
	step         int
	from, target T
	transition   Transition
	startedAt    time.Duration
}

// startSequenceState is SequenceState::start (U:motion/sequence.rs:223-231).
func startSequenceState[T any](step int, from T, steps []SequenceStep[T], startedAt time.Duration) sequenceState[T] {
	return sequenceState[T]{
		step:       step,
		from:       from,
		target:     steps[step].target,
		transition: steps[step].transition,
		startedAt:  startedAt,
	}
}

// progress is U:motion/sequence.rs:233-238. The clock is monotonic and
// a state's start is a previous sample's now (or a hand-over instant the
// finished step already reached), so the subtraction cannot go negative.
func (s sequenceState[T]) progress(now time.Duration) (float32, MotionStatus) {
	return s.transition.Progress(now-s.startedAt, s.transition.Duration())
}

// value is U:motion/sequence.rs:240-243.
func (s sequenceState[T]) value(progress float32) T {
	return InterpolateValue(s.from, s.target, s.transition.Sample(progress))
}

// handOver starts the next step where this finished one ended
// (U:motion/sequence.rs:245-255).
func (s sequenceState[T]) handOver(steps []SequenceStep[T]) sequenceState[T] {
	return startSequenceState(s.step+1, s.target, steps, s.startedAt+s.transition.FinishesAfter())
}
