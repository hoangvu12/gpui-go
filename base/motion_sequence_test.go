package base

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// The sequence tests of motion.rs (9), on the virtual clock.

// sequenceFixture is U:motion.rs:1182-1251 (SequenceView / SequenceFixture).
type sequenceFixture struct {
	*motionWindow
	t       *testing.T
	steps   []SequenceStep[float32]
	samples []SequenceSample[float32]
}

func openSequenceFixture(t *testing.T, ta *gpui.TestApp, steps []SequenceStep[float32]) *sequenceFixture {
	f := &sequenceFixture{t: t, steps: steps}
	f.motionWindow = openMotionWindow(t, ta, func(w *gpui.Window, cx *gpui.App) gpui.AnyElement {
		f.samples = append(f.samples, NewSequence("sequence-test", float32(0)).WithSteps(f.steps...).Sample(w, cx))
		return gpui.AnyElement{}
	})
	return f
}

// linearTransition is SequenceFixture::linear (U:motion.rs:1225-1227).
func linearTransition(millis int64) Transition { return NewTransition(ms(millis)).Ease(linear) }

func (f *sequenceFixture) last() SequenceSample[float32] { return f.samples[len(f.samples)-1] }

func (f *sequenceFixture) render() SequenceSample[float32] {
	f.refresh()
	return f.last()
}

func (f *sequenceFixture) advance(millis int64) SequenceSample[float32] {
	f.ta.AdvanceClock(millis)
	return f.render()
}

// assertSample is U:motion.rs:1253-1258.
func (f *sequenceFixture) assertSample(sample SequenceSample[float32], value float32, step int, status MotionStatus) {
	f.t.Helper()
	if sample.Value() != value || sample.Step() != step || sample.Status() != status {
		f.t.Fatalf("sample = (%v, %d, %v), want (%v, %d, %v)", sample.Value(), sample.Step(), sample.Status(), value, step, status)
	}
}

// U:motion.rs:1260-1281
func TestSequenceStepsAdvanceInOrderAtTheirBoundaries(t *testing.T) {
	f := openSequenceFixture(t, gpui.NewTestApp(), []SequenceStep[float32]{
		NewSequenceStep[float32](10, linearTransition(100)),
		NewSequenceStep[float32](20, linearTransition(100).Delay(ms(50))),
	})
	f.assertSample(f.last(), 0, 0, MotionStatusRunning)
	f.assertSample(f.advance(50), 5, 0, MotionStatusRunning)

	// The first step ends and the second begins within one frame; nothing
	// reports the first step as finished in between.
	f.assertSample(f.advance(50), 10, 1, MotionStatusDelayed)
	f.assertSample(f.advance(50), 10, 1, MotionStatusRunning)
	f.assertSample(f.advance(50), 15, 1, MotionStatusRunning)
	f.assertSample(f.advance(50), 20, 1, MotionStatusFinished)
}

// U:motion.rs:1283-1298
func TestASequenceStartsItsNextStepWhereThePreviousEndedNotAtTheFrame(t *testing.T) {
	f := openSequenceFixture(t, gpui.NewTestApp(), []SequenceStep[float32]{
		NewSequenceStep[float32](10, linearTransition(100)),
		NewSequenceStep[float32](20, linearTransition(100)),
	})
	// One frame at 50 ms, the next at 175 ms: the boundary at 100 ms fell
	// between them, and the second step is sampled as if it started there.
	f.advance(50)
	f.assertSample(f.advance(125), 17.5, 1, MotionStatusRunning)
}

// U:motion.rs:1300-1312
func TestZeroDurationStepsCompleteWithinTheFrameThatReachesThem(t *testing.T) {
	f := openSequenceFixture(t, gpui.NewTestApp(), []SequenceStep[float32]{
		NewSequenceStep[float32](5, linearTransition(0)),
		NewSequenceStep[float32](6, linearTransition(0)),
		NewSequenceStep[float32](10, linearTransition(100)),
	})
	f.assertSample(f.last(), 6, 2, MotionStatusRunning)
	f.assertSample(f.advance(50), 8, 2, MotionStatusRunning)
}

// U:motion.rs:1314-1344
func TestASequenceReportsFinishedOnceAndThenStopsRequestingFrames(t *testing.T) {
	ta := gpui.NewTestApp()
	f := openSequenceFixture(t, ta, []SequenceStep[float32]{
		NewSequenceStep[float32](10, linearTransition(100)),
		NewSequenceStep[float32](20, linearTransition(100)),
	})
	if n := f.pendingFrame(); n != 1 {
		t.Fatalf("pending frames = %d, want 1", n)
	}
	ta.RunUntilParked()

	ta.AdvanceClock(100)
	if n := f.pendingFrame(); n != 1 {
		t.Fatalf("pending frames = %d, want 1", n)
	}
	ta.RunUntilParked()
	if s := f.last().Status(); s != MotionStatusRunning {
		t.Fatalf("status = %v, want Running", s)
	}
	for _, s := range f.samples {
		if s.IsFinished() {
			t.Fatal("the first step's end must not read as the sequence finishing")
		}
	}

	f.assertSample(f.advance(100), 20, 1, MotionStatusFinished)
	f.pendingFrame()
	ta.RunUntilParked()
	if n := f.pendingFrame(); n != 0 {
		t.Fatalf("pending frames = %d, want 0", n)
	}
	f.assertSample(f.advance(1000), 20, 1, MotionStatusFinished)
}

// U:motion.rs:1346-1360
func TestReducedMotionAdoptsASequencesLastTargetWithoutRequestingAFrame(t *testing.T) {
	ta := gpui.NewTestApp()
	reduceMotion(ta)
	f := openSequenceFixture(t, ta, []SequenceStep[float32]{
		NewSequenceStep[float32](10, linearTransition(100)),
		NewSequenceStep[float32](20, linearTransition(100)),
	})
	f.assertSample(f.last(), 20, 1, MotionStatusFinished)
	if n := f.pendingFrame(); n != 0 {
		t.Fatalf("pending frames = %d, want 0", n)
	}
}

// U:motion.rs:1362-1386
func TestAChangedStepTargetRestartsTheSequenceFromTheSampledValue(t *testing.T) {
	f := openSequenceFixture(t, gpui.NewTestApp(), []SequenceStep[float32]{
		NewSequenceStep[float32](10, linearTransition(100)),
		NewSequenceStep[float32](20, linearTransition(100)),
	})
	f.advance(150)
	f.assertSample(f.last(), 15, 1, MotionStatusRunning)

	f.steps = []SequenceStep[float32]{
		NewSequenceStep[float32](10, linearTransition(100)),
		NewSequenceStep[float32](35, linearTransition(100)),
	}
	f.assertSample(f.render(), 15, 0, MotionStatusRunning)
	f.assertSample(f.advance(50), 12.5, 0, MotionStatusRunning)
	f.assertSample(f.advance(100), 22.5, 1, MotionStatusRunning)

	// Fewer steps than the one being played is a different sequence too.
	f.steps = []SequenceStep[float32]{NewSequenceStep[float32](0, linearTransition(100))}
	f.assertSample(f.render(), 22.5, 0, MotionStatusRunning)
	f.assertSample(f.advance(100), 0, 0, MotionStatusFinished)
}

// U:motion.rs:1388-1404
func TestAChangeToAStepNotBeingPlayedDoesNotRestartTheSequence(t *testing.T) {
	f := openSequenceFixture(t, gpui.NewTestApp(), []SequenceStep[float32]{
		NewSequenceStep[float32](10, linearTransition(100)),
		NewSequenceStep[float32](20, linearTransition(100)),
	})
	f.advance(50)
	f.steps = []SequenceStep[float32]{
		NewSequenceStep[float32](10, linearTransition(100)),
		NewSequenceStep[float32](30, linearTransition(100)),
	}
	f.assertSample(f.render(), 5, 0, MotionStatusRunning)
	f.assertSample(f.advance(100), 20, 1, MotionStatusRunning)
}

// U:motion.rs:1406-1456 (SingleStepView)
func TestASingleStepSequenceMatchesAPlainTransition(t *testing.T) {
	ta := gpui.NewTestApp()
	armed := false
	type pair struct {
		sequence SequenceSample[float32]
		plain    MotionValue[float32]
	}
	var samples []pair
	m := openMotionWindow(t, ta, func(w *gpui.Window, cx *gpui.App) gpui.AnyElement {
		policy := NewTransition(ms(100)).Delay(ms(20)).Easing(EasingEaseInOut())
		// A plain transition adopts its first target where a sequence plays
		// from `from` at once, so the transition is primed at 0.0 and both
		// leave for 10.0 on the frame that arms the view.
		if !armed {
			TransitionWithStatus("plain", float32(0), policy, w, cx)
			return gpui.AnyElement{}
		}
		sequence := NewSequence("single-step", float32(0)).WithStep(10, policy).Sample(w, cx)
		plain := TransitionWithStatus("plain", float32(10), policy, w, cx)
		samples = append(samples, pair{sequence, plain})
		return gpui.AnyElement{}
	})
	armed = true
	for _, millis := range []int64{0, 10, 30, 70, 120} {
		ta.AdvanceClock(millis)
		m.refresh()
		last := samples[len(samples)-1]
		if last.sequence.Value() != last.plain.Value {
			t.Fatalf("value after %d ms: %v vs %v", millis, last.sequence.Value(), last.plain.Value)
		}
		if last.sequence.Status() != last.plain.Status {
			t.Fatalf("status after %d ms: %v vs %v", millis, last.sequence.Status(), last.plain.Status)
		}
		if last.sequence.Step() != 0 {
			t.Fatalf("step = %d", last.sequence.Step())
		}
	}
}

// U:motion.rs:1458-1463
func TestAnEmptySequenceIdlesAtItsStartingValue(t *testing.T) {
	f := openSequenceFixture(t, gpui.NewTestApp(), nil)
	f.assertSample(f.last(), 0, 0, MotionStatusIdle)
	if n := f.pendingFrame(); n != 0 {
		t.Fatalf("pending frames = %d, want 0", n)
	}
}
