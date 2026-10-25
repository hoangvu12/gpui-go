package gpui

// Base ticket 08's animation element tests (animation.go): the ports of
// the duration-animation tests of R:elements/animation.rs:536-1030 and
// the motion sampler's pinned cases (motion.rs).

import (
	"fmt"
	"testing"
	"time"
)

// animationFixture renders build in a 100x100 test window (the pinned
// open_test_window, R:animation.rs:650-670).
func animationFixture(t *testing.T, ta *TestApp, build func(*paintView, *Window, *Context[paintView]) AnyElement) *paintFixture {
	t.Helper()
	app := ta.App()
	window := NewTestWindow(app, Size{Width: 100, Height: 100})
	view := NewEntity(app, window.Scope(), func(v *paintView, _ *Context[paintView]) { v.build = build })
	window.SetRootView(ViewOf(view))
	f := &paintFixture{t: t, ta: ta, app: app, window: window, view: view}
	t.Cleanup(func() {
		CloseTestWindow(window)
		releaseAppImageCodec(app)
	})
	f.draw()
	ta.RunUntilParked()
	return f
}

// simulateNextFrame is the pinned simulate_next_frame helper
// (R:animation.rs:672-678): deliver the next-frame callbacks, then run
// until parked (which draws the notified window).
func (f *paintFixture) simulateNextFrame() int {
	n := f.window.SimulateNextFrame(f.app)
	f.ta.RunUntilParked()
	return n
}

// recordingAnimation renders the pinned AnimationTestView
// (R:animation.rs:611-631): a repeating one-second animation recording
// every rendered delta, optionally synced and throttled.
func recordingAnimation(deltas *[]float32, maxFPS *float32) func(*paintView, *Window, *Context[paintView]) AnyElement {
	return func(*paintView, *Window, *Context[paintView]) AnyElement {
		animation := NewAnimation(NewMotion(time.Second))
		if maxFPS != nil {
			animation = animation.RepeatSynced().WithMaxFPS(*maxFPS)
		} else {
			animation = animation.Repeat()
		}
		return Div().Flex().SizeFull().Child(WithAnimation(Div().Flex(), "repeating-animation", animation, func(d *DivElement, delta float32) *DivElement {
			*deltas = append(*deltas, delta)
			return d
		})).IntoElement()
	}
}

// TestAnimationWrappersAcceptChildren ports
// R:animation.rs:680-701 test_animation_wrappers_accept_children (the
// animation half; MapElement is the port's ParentElement forwarding).
func TestAnimationWrappersAcceptChildren(t *testing.T) {
	element := WithAnimation(Div().Flex().ID("id"), "animation", NewAnimation(time.Second), func(d *DivElement, _ float32) *DivElement { return d }).
		MapElement(func(d *DivElement) *DivElement { return d.Child(Div().Flex()) })
	if element.element.ChildCount() != 1 {
		t.Fatalf("mapped element children = %d, want 1", element.element.ChildCount())
	}
}

// TestAnimationRepeatingSchedulesAnimationFrames ports
// R:animation.rs:934-944.
func TestAnimationRepeatingSchedulesAnimationFrames(t *testing.T) {
	var deltas []float32
	f := animationFixture(t, NewTestApp(), recordingAnimation(&deltas, nil))
	if len(deltas) != 1 {
		t.Fatalf("deltas after opening = %v, want one", deltas)
	}
	for expected := 2; expected <= 3; expected++ {
		if n := f.simulateNextFrame(); n != 1 {
			t.Fatalf("frame callbacks = %d, want 1", n)
		}
		if len(deltas) != expected {
			t.Fatalf("deltas = %v, want %d", deltas, expected)
		}
	}
}

// TestAnimationRespectsReducedMotion ports the duration half of
// R:animation.rs:896-917 test_animations_respect_reduced_motion: a
// repeating animation renders its resting (start) progress and schedules
// no frame. (The spring half is not applicable: springs are not ported.)
func TestAnimationRespectsReducedMotion(t *testing.T) {
	ta := NewTestApp()
	ta.App().SetReduceMotion(true)
	var deltas []float32
	f := animationFixture(t, ta, recordingAnimation(&deltas, nil))
	if fmt.Sprint(deltas) != "[0]" {
		t.Fatalf("deltas = %v, want [0]", deltas)
	}
	if n := f.simulateNextFrame(); n != 0 {
		t.Fatalf("frame callbacks under reduce motion = %d, want 0", n)
	}
}

// sampleRecord is one (animation index, delta) sample.
type sampleRecord struct {
	ix    int
	delta float32
}

// TestAnimationZeroDurationSequenceAdvancesWithoutNaN ports
// R:animation.rs:919-932.
func TestAnimationZeroDurationSequenceAdvancesWithoutNaN(t *testing.T) {
	var samples []sampleRecord
	f := animationFixture(t, NewTestApp(), func(*paintView, *Window, *Context[paintView]) AnyElement {
		return WithAnimations(Div().Flex(), "animation-sequence", []Animation{NewAnimation(time.Duration(0)), NewAnimation(time.Duration(0))}, func(d *DivElement, ix int, delta float32) *DivElement {
			samples = append(samples, sampleRecord{ix, delta})
			return d
		}).IntoElement()
	})
	if fmt.Sprint(samples) != "[{0 1}]" {
		t.Fatalf("samples = %v, want [(0, 1.0)]", samples)
	}
	if n := f.simulateNextFrame(); n != 1 {
		t.Fatalf("frame callbacks = %d, want 1", n)
	}
	if fmt.Sprint(samples) != "[{0 1} {1 1}]" {
		t.Fatalf("samples = %v, want [(0, 1.0), (1, 1.0)]", samples)
	}
	if n := f.simulateNextFrame(); n != 0 {
		t.Fatalf("frame callbacks after the chain = %d, want 0", n)
	}
}

// TestAnimationMaxFPSSchedulesTimerDrivenFrames ports
// R:animation.rs:946-976. Deviation: the port's deterministic scheduler
// delivers a timer's continuation at its deadline inside AdvanceClock
// (executor.go advanceClock), so the throttled re-render samples at
// 100ms / 200ms where the pin's run_until_parked after a 105ms advance
// samples at 105ms / 210ms.
func TestAnimationMaxFPSSchedulesTimerDrivenFrames(t *testing.T) {
	var deltas []float32
	fps := float32(10)
	f := animationFixture(t, NewTestApp(), recordingAnimation(&deltas, &fps))
	approx := func(want ...float32) {
		t.Helper()
		if len(deltas) != len(want) {
			t.Fatalf("deltas = %v, want %v", deltas, want)
		}
		for i := range want {
			if d := deltas[i] - want[i]; d > 1e-4 || d < -1e-4 {
				t.Fatalf("deltas = %v, want %v", deltas, want)
			}
		}
	}
	approx(0)
	if n := f.simulateNextFrame(); n != 0 {
		t.Fatalf("frame callbacks = %d, want 0 (re-renders are timer-driven)", n)
	}
	approx(0)
	f.ta.AdvanceClock(105)
	approx(0, 0.1)
	f.ta.AdvanceClock(105)
	approx(0, 0.1, 0.2)
}

// TestAnimationSyncedAnimationsSharePhaseAcrossElements ports
// R:animation.rs:978-1029.
func TestAnimationSyncedAnimationsSharePhaseAcrossElements(t *testing.T) {
	var first, second []float32
	showSecond := false
	record := func(deltas *[]float32) func(*DivElement, float32) *DivElement {
		return func(d *DivElement, delta float32) *DivElement {
			*deltas = append(*deltas, delta)
			return d
		}
	}
	f := animationFixture(t, NewTestApp(), func(*paintView, *Window, *Context[paintView]) AnyElement {
		root := Div().Flex().SizeFull().Child(WithAnimation(Div().Flex(), "first-synced-animation", NewAnimation(time.Second).RepeatSynced(), record(&first)))
		if showSecond {
			root = root.Child(WithAnimation(Div().Flex(), "second-synced-animation", NewAnimation(time.Second).RepeatSynced(), record(&second)))
		}
		return root.IntoElement()
	})
	if fmt.Sprint(first) != "[0]" {
		t.Fatalf("first deltas = %v, want [0]", first)
	}
	f.ta.AdvanceClock(250)
	f.simulateNextFrame()
	if fmt.Sprint(first) != "[0 0.25]" {
		t.Fatalf("first deltas = %v, want [0 0.25]", first)
	}

	// The second element mounts a quarter through the cycle, yet renders
	// the shared phase rather than starting at zero.
	showSecond = true
	f.notify()
	f.ta.RunUntilParked()
	f.ta.AdvanceClock(250)
	f.simulateNextFrame()
	if got := second[len(second)-1]; got != 0.5 {
		t.Fatalf("second delta = %v, want 0.5", got)
	}
	if first[len(first)-1] != second[len(second)-1] {
		t.Fatalf("first %v and second %v out of phase", first, second)
	}
	for _, d := range second {
		if d <= 0 {
			t.Fatalf("second deltas %v include a zero phase", second)
		}
	}

	// The phase wraps around each full cycle.
	f.ta.AdvanceClock(2250)
	f.simulateNextFrame()
	if got := first[len(first)-1]; got != 0.75 {
		t.Fatalf("first delta after wrapping = %v, want 0.75", got)
	}

	// Sub-second precision survives months of uptime.
	f.ta.AdvanceClock((300*24*time.Hour + 500*time.Millisecond).Milliseconds())
	f.simulateNextFrame()
	if got := first[len(first)-1]; got != 0.25 {
		t.Fatalf("first delta after months = %v, want 0.25", got)
	}
}

// TestAnimationMotionSampling pins the duration Motion sampler
// (motion.rs:652-742): one-shot completion at the endpoint, custom
// easing, the restarting-pass boundary, delay, alternate parity and the
// resting progress.
func TestAnimationMotionSampling(t *testing.T) {
	m := NewMotion(time.Second)
	if s := m.Sample(250 * time.Millisecond); s.Progress != 0.25 || !s.IsActive || s.Phase != MotionPlaying {
		t.Fatalf("one-shot 250ms = %+v", s)
	}
	if s := m.Sample(2 * time.Second); s.Progress != 1 || s.IsActive || !s.IsComplete() {
		t.Fatalf("one-shot done = %+v", s)
	}
	if s := m.WithEasing(Quadratic).Sample(500 * time.Millisecond); s.Progress != 0.25 {
		t.Fatalf("quadratic 500ms = %+v", s)
	}
	repeat := m.RepeatForever()
	if s := repeat.Sample(time.Second); s.Progress != 1 || s.Iteration != 0 || !s.IsActive {
		t.Fatalf("restart boundary = %+v, want the endpoint of iteration 0", s)
	}
	if s := repeat.Sample(1250 * time.Millisecond); s.Progress != 0.25 || s.Iteration != 1 {
		t.Fatalf("second iteration = %+v", s)
	}
	if s := m.WithDelay(time.Second).Sample(500 * time.Millisecond); s.Phase != MotionDelayed || s.Progress != 0 || !s.IsActive {
		t.Fatalf("delayed = %+v", s)
	}
	alt := m.Alternate().Iterations(2)
	if s := alt.Sample(1250 * time.Millisecond); s.Direction != PassReverse || s.Progress != 0.75 {
		t.Fatalf("alternate reverse leg = %+v", s)
	}
	if alt.restingProgress() != ProgressStart || m.restingProgress() != ProgressEnd || repeat.restingProgress() != ProgressStart {
		t.Fatal("resting progress")
	}
	if s := NewMotion(0).RepeatForever().Sample(time.Second); s.Phase != MotionInactive || s.IsActive {
		t.Fatalf("zero-length infinite motion = %+v, want inactive", s)
	}
	if EaseInOut(0.25) != 0.125 || Bounce(Linear)(0.75) != 0.5 || EaseOutQuint()(1) != 1 {
		t.Fatal("easing functions")
	}
}
