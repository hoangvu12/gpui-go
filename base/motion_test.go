package base

import (
	"math"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

// motion.rs tests (43/43), split by module: the transition, keyframe
// playback and spring tests and the track/geometry tests are here; the
// easing, timing, keyframe, stagger, presence and sequence tests live in
// the matching motion_*_test.go files.
//
// Every windowed test runs on the deterministic test window under the
// TestApp virtual clock: cx.executor().advance_clock is TestApp.AdvanceClock,
// window.refresh + run_until_parked is motionWindow.refresh, and
// window.simulate_next_frame is motionWindow.pendingFrame
// (Window.SimulateNextFrame, R:window.rs:2643-2650).

// motionView renders its closure (the upstream test views, which render
// Empty after sampling).
type motionView struct {
	render func(w *gpui.Window, cx *gpui.App) gpui.AnyElement
}

func (v *motionView) Render(w *gpui.Window, cx *gpui.Context[motionView]) gpui.AnyElement {
	if el := v.render(w, cx.App()); el != (gpui.AnyElement{}) {
		return el
	}
	return gpui.Empty()
}

// motionWindow is the upstream `cx.open_window(size(px(100.), px(100.)), ..)`
// fixture handle.
type motionWindow struct {
	t  *testing.T
	ta *gpui.TestApp
	w  *gpui.Window
}

// openMotionWindow opens a 100x100 test window and runs until parked.
func openMotionWindow(t *testing.T, ta *gpui.TestApp, render func(w *gpui.Window, cx *gpui.App) gpui.AnyElement) *motionWindow {
	t.Helper()
	win := ta.OpenWindowWithOptions[motionView](gpui.WindowOptions{Bounds: gpui.Bounds{Size: gpui.Size{Width: 100, Height: 100}}},
		func(v *motionView, _ *gpui.Window, _ *gpui.Context[motionView]) { v.render = render })
	return &motionWindow{t: t, ta: ta, w: win.Window()}
}

// refresh is `window.update(cx, |_, window, _| window.refresh())` then
// `cx.run_until_parked()`.
func (m *motionWindow) refresh() {
	m.ta.Update(func(*gpui.App) { m.w.Refresh() })
	m.ta.RunUntilParked()
}

// pendingFrame is `window.simulate_next_frame(cx)`: it runs the pending
// next-frame callbacks and reports how many ran.
func (m *motionWindow) pendingFrame() int { return m.w.SimulateNextFrame(m.ta.App()) }

func (m *motionWindow) visual() *gpui.VisualTestContext { return gpui.NewVisualTestContext(m.w, m.ta) }

func ms(n int64) time.Duration { return time.Duration(n) * time.Millisecond }

func linear(t float32) float32 { return t }

func reduceMotion(ta *gpui.TestApp) { ta.Update(func(app *gpui.App) { app.SetReduceMotion(true) }) }

// U:motion.rs:857-889
func TestCommonGPUIGeometryInterpolatesChannelByChannel(t *testing.T) {
	fromSize := gpui.Size{Width: 10, Height: 20}
	toSize := gpui.Size{Width: 30, Height: 60}
	if got := InterpolateValue(fromSize, toSize, 0.25); got != (gpui.Size{Width: 15, Height: 30}) {
		t.Fatalf("size = %v", got)
	}
	from := gpui.Bounds{Origin: gpui.Point{X: 0, Y: 10}, Size: fromSize}
	to := gpui.Bounds{Origin: gpui.Point{X: 40, Y: 50}, Size: toSize}
	if got := InterpolateValue(from, to, 0.5); got != (gpui.Bounds{Origin: gpui.Point{X: 20, Y: 30}, Size: gpui.Size{Width: 20, Height: 40}}) {
		t.Fatalf("bounds = %v", got)
	}
	transform := InterpolateValue(MotionTransformIdentity(), MotionTransform{
		Translation:     gpui.Point{X: 20, Y: 40},
		Scale:           gpui.Point{X: 2, Y: 0.5},
		RotationRadians: math.Pi,
		Opacity:         0,
	}, 0.5)
	if transform.Translation != (gpui.Point{X: 10, Y: 20}) || transform.Scale != (gpui.Point{X: 1.5, Y: 0.75}) ||
		transform.RotationRadians != float32(math.Pi/2) || transform.Opacity != 0.5 {
		t.Fatalf("transform = %+v", transform)
	}
}

// U:motion.rs:1465-1477
func TestTransitionIDsAcceptElementLikeScalarsAndNamedChannels(t *testing.T) {
	if !TransitionIDOf("opacity").Equal(TransitionIDOf(gpui.NameElementID("opacity"))) {
		t.Fatal("a name and its element id differ")
	}
	if TransitionIDWithChannel("terms", "fill").Equal(TransitionIDWithChannel("terms", "mark-opacity")) {
		t.Fatal("two channels of one element are equal")
	}
	_ = TransitionIDOf(7)
	_ = TransitionIDOf(int32(7))
}

// statusFixture is U:motion.rs:904-962 (StatusView / StatusFixture).
type statusFixture struct {
	*motionWindow
	target  float32
	samples []MotionValue[float32]
}

func openStatusFixture(t *testing.T, ta *gpui.TestApp, policy Transition) *statusFixture {
	f := &statusFixture{}
	f.motionWindow = openMotionWindow(t, ta, func(w *gpui.Window, cx *gpui.App) gpui.AnyElement {
		f.samples = append(f.samples, TransitionWithStatus(TransitionIDWithChannel("status-test", "value"), f.target, policy, w, cx))
		return gpui.AnyElement{}
	})
	return f
}

func (f *statusFixture) render(target float32) MotionValue[float32] {
	f.target = target
	f.refresh()
	return f.samples[len(f.samples)-1]
}

// U:motion.rs:964-976
func TestStatusTransitionReportsDelayRunningAndFinished(t *testing.T) {
	ta := gpui.NewTestApp()
	f := openStatusFixture(t, ta, NewTransition(ms(100)).Delay(ms(20)))
	if s := f.render(1).Status; s != MotionStatusDelayed {
		t.Fatalf("status = %v, want Delayed", s)
	}
	ta.AdvanceClock(20)
	if s := f.render(1).Status; s != MotionStatusRunning {
		t.Fatalf("status = %v, want Running", s)
	}
	ta.AdvanceClock(100)
	if s := f.render(1).Status; s != MotionStatusFinished {
		t.Fatalf("status = %v, want Finished", s)
	}
}

// U:motion.rs:978-989
func TestNegativeDelaySamplesATargetChangeInsideItsInterval(t *testing.T) {
	ta := gpui.NewTestApp()
	f := openStatusFixture(t, ta, NewTransition(ms(100)).Delay(SignedDurationNegative(ms(25))).Ease(linear))
	sample := f.render(1)
	if sample.Status != MotionStatusRunning || sample.Value != 0.25 {
		t.Fatalf("sample = %+v, want 0.25 Running", sample)
	}
}

// U:motion.rs:991-1000
func TestADirectReversalShortensTheReturnTransition(t *testing.T) {
	ta := gpui.NewTestApp()
	f := openStatusFixture(t, ta, NewTransition(ms(100)).Ease(linear))
	if v := f.render(1).Value; v != 0 {
		t.Fatalf("value = %v, want 0", v)
	}
	ta.AdvanceClock(50)
	if v := f.render(0).Value; v != 0.5 {
		t.Fatalf("value = %v, want 0.5", v)
	}
	ta.AdvanceClock(25)
	if v := f.render(0).Value; v != 0.25 {
		t.Fatalf("value = %v, want 0.25", v)
	}
}

// U:motion.rs:1025-1071
func TestKeyedKeyframesFollowTimingAndStopAfterCompletion(t *testing.T) {
	ta := gpui.NewTestApp()
	track, err := KeyframesTryNew(NewKeyframe[float32](0, 0), NewKeyframe[float32](1, 10))
	if err != nil {
		t.Fatal(err)
	}
	var samples []MotionValue[float32]
	m := openMotionWindow(t, ta, func(w *gpui.Window, cx *gpui.App) gpui.AnyElement {
		samples = append(samples, AnimateKeyframes("keyframe-test", track, NewTiming(ms(100)), w, cx))
		return gpui.AnyElement{}
	})
	last := func() MotionValue[float32] { return samples[len(samples)-1] }
	if l := last(); l.Value != 0 || l.Status != MotionStatusRunning {
		t.Fatalf("first sample = %+v", l)
	}

	ta.AdvanceClock(50)
	if n := m.pendingFrame(); n != 1 {
		t.Fatalf("pending frames = %d, want 1", n)
	}
	ta.RunUntilParked()
	if v := last().Value; v != 5 {
		t.Fatalf("value = %v, want 5", v)
	}

	ta.AdvanceClock(50)
	m.refresh()
	if s := last().Status; s != MotionStatusFinished {
		t.Fatalf("status = %v, want Finished", s)
	}
	m.pendingFrame()
	ta.RunUntilParked()
	if n := m.pendingFrame(); n != 0 {
		t.Fatalf("pending frames = %d, want 0", n)
	}
}

// fixture is U:motion.rs:1479-1567 (TestView / Fixture).
type fixture struct {
	*motionWindow
	target  float32
	samples []float32
}

func openFixture(t *testing.T, ta *gpui.TestApp, duration time.Duration) *fixture {
	f := &fixture{}
	f.motionWindow = openMotionWindow(t, ta, func(w *gpui.Window, cx *gpui.App) gpui.AnyElement {
		f.samples = append(f.samples, MotionTransition(TransitionIDWithChannel("test", "value"), f.target, NewTransition(duration).Ease(linear), w, cx))
		return gpui.AnyElement{}
	})
	return f
}

func (f *fixture) render(target float32) float32 {
	f.target = target
	f.refresh()
	return f.samples[len(f.samples)-1]
}

func (f *fixture) last() float32 { return f.samples[len(f.samples)-1] }

// U:motion.rs:1569-1573
func TestAZeroDurationTargetChangeIsImmediate(t *testing.T) {
	f := openFixture(t, gpui.NewTestApp(), 0)
	if v := f.render(1); v != 1 {
		t.Fatalf("value = %v, want 1", v)
	}
}

// U:motion.rs:1575-1583
func TestAChangedTargetTransitionsOverTime(t *testing.T) {
	ta := gpui.NewTestApp()
	f := openFixture(t, ta, ms(100))
	if v := f.render(10); v != 0 {
		t.Fatalf("value = %v, want 0", v)
	}
	ta.AdvanceClock(50)
	if v := f.render(10); v != 5 {
		t.Fatalf("value = %v, want 5", v)
	}
}

// U:motion.rs:1585-1596: the requested animation frame alone redraws the
// view at the advanced clock — no manual refresh.
func TestRequestedAnimationFramesResampleWithoutManualRefresh(t *testing.T) {
	ta := gpui.NewTestApp()
	f := openFixture(t, ta, ms(100))
	if v := f.render(10); v != 0 {
		t.Fatalf("value = %v, want 0", v)
	}
	ta.AdvanceClock(50)
	if n := f.pendingFrame(); n != 1 {
		t.Fatalf("pending frames = %d, want 1", n)
	}
	ta.RunUntilParked()
	if v := f.last(); v != 5 {
		t.Fatalf("value = %v, want 5", v)
	}
}

// U:motion.rs:1598-1608
func TestReversingUsesTheCurrentSampleAndShortensTheReturn(t *testing.T) {
	ta := gpui.NewTestApp()
	f := openFixture(t, ta, ms(100))
	if v := f.render(10); v != 0 {
		t.Fatalf("value = %v, want 0", v)
	}
	ta.AdvanceClock(50)
	if v := f.render(0); v != 5 {
		t.Fatalf("value = %v, want 5", v)
	}
	ta.AdvanceClock(25)
	if v := f.render(0); v != 2.5 {
		t.Fatalf("value = %v, want 2.5", v)
	}
}

// U:motion.rs:1610-1635 (DelayedView, U:motion.rs:1502-1524)
func TestDelayHoldsThePreviousValueBeforeInterpolation(t *testing.T) {
	ta := gpui.NewTestApp()
	var target float32
	var samples []float32
	m := openMotionWindow(t, ta, func(w *gpui.Window, cx *gpui.App) gpui.AnyElement {
		samples = append(samples, MotionTransition(TransitionIDWithChannel("delayed-test", "value"), target,
			NewTransition(ms(100)).Delay(ms(50)).Ease(linear), w, cx))
		return gpui.AnyElement{}
	})
	last := func() float32 { return samples[len(samples)-1] }

	target = 10
	m.refresh()
	if v := last(); v != 0 {
		t.Fatalf("value = %v, want 0", v)
	}
	ta.AdvanceClock(50)
	m.refresh()
	if v := last(); v != 0 {
		t.Fatalf("value = %v, want 0", v)
	}
	ta.AdvanceClock(50)
	m.refresh()
	if v := last(); v != 5 {
		t.Fatalf("value = %v, want 5", v)
	}
}

// U:motion.rs:1637-1649
func TestACompletedTransitionStopsRequestingFrames(t *testing.T) {
	ta := gpui.NewTestApp()
	f := openFixture(t, ta, ms(100))
	f.render(1)
	if n := f.pendingFrame(); n != 1 {
		t.Fatalf("pending frames = %d, want 1", n)
	}
	ta.AdvanceClock(100)
	if v := f.render(1); v != 1 {
		t.Fatalf("value = %v, want 1", v)
	}
	f.pendingFrame()
	ta.RunUntilParked()
	if n := f.pendingFrame(); n != 0 {
		t.Fatalf("pending frames = %d, want 0", n)
	}
}

// U:motion.rs:1651-1658
func TestReducedMotionAdoptsTheTargetWithoutRequestingAFrame(t *testing.T) {
	ta := gpui.NewTestApp()
	reduceMotion(ta)
	f := openFixture(t, ta, ms(100))
	if v := f.render(1); v != 1 {
		t.Fatalf("value = %v, want 1", v)
	}
	if n := f.pendingFrame(); n != 0 {
		t.Fatalf("pending frames = %d, want 0", n)
	}
}

// springFixture is U:motion.rs:1660-1733 (SpringView / SpringFixture).
type springFixture struct {
	*motionWindow
	target  float32
	policy  Spring
	samples []float32
}

func openSpringFixture(t *testing.T, ta *gpui.TestApp, policy Spring) *springFixture {
	f := &springFixture{policy: policy}
	f.motionWindow = openMotionWindow(t, ta, func(w *gpui.Window, cx *gpui.App) gpui.AnyElement {
		f.samples = append(f.samples, MotionSpring(TransitionIDWithChannel("spring-test", "value"), f.target, f.policy, w, cx))
		return gpui.AnyElement{}
	})
	return f
}

func (f *springFixture) render(target float32) float32 {
	f.target = target
	f.refresh()
	return f.samples[len(f.samples)-1]
}

func (f *springFixture) advance(millis int64, target float32) float32 {
	f.ta.AdvanceClock(millis)
	return f.render(target)
}

// U:motion.rs:1735-1739
func TestASpringAdoptsItsFirstTargetImmediately(t *testing.T) {
	f := openSpringFixture(t, gpui.NewTestApp(), NewSpring(ms(300)))
	if f.samples[0] != 0 {
		t.Fatalf("first sample = %v, want 0", f.samples[0])
	}
}

// U:motion.rs:1741-1752
func TestASpringTravelsTowardItsTargetOverTime(t *testing.T) {
	f := openSpringFixture(t, gpui.NewTestApp(), NewSpring(ms(300)))
	if v := f.render(1); v != 0 {
		t.Fatalf("value = %v, want 0", v)
	}
	early := f.advance(50, 1)
	late := f.advance(50, 1)
	if !(0 < early && early < late && late < 1) {
		t.Fatalf("expected monotonic approach, got %v then %v", early, late)
	}
}

// U:motion.rs:1754-1771
func TestAReversedSpringKeepsItsMomentumBeforeTurningAround(t *testing.T) {
	f := openSpringFixture(t, gpui.NewTestApp(), NewSpring(ms(300)))
	f.render(1)
	reversedAt := f.advance(100, 1)

	// Retarget mid-flight: the spring remains continuous and has not
	// reached the new target on the next frame.
	if v := f.render(0); v != reversedAt {
		t.Fatalf("retarget moved the value: %v -> %v", reversedAt, v)
	}
	next := f.advance(16, 0)
	if !(next > 0 && next < reversedAt) {
		t.Fatalf("expected a continuous turn from %v toward zero, got %v", reversedAt, next)
	}
	if v := f.advance(1000, 0); v != 0 {
		t.Fatalf("value = %v, want 0", v)
	}
}

// U:motion.rs:1773-1791
func TestABouncySpringOvershootsItsTarget(t *testing.T) {
	f := openSpringFixture(t, gpui.NewTestApp(), NewSpring(ms(350)).WithDamping(0.7))
	f.render(1)
	for range 30 {
		f.advance(16, 1)
	}
	peak := float32(-math.MaxFloat32)
	for _, s := range f.samples {
		peak = max(peak, s)
	}
	if peak <= 1 {
		t.Fatalf("expected an overshoot past 1.0, got %v", peak)
	}
}

// U:motion.rs:1793-1803
func TestASettledSpringStopsRequestingFrames(t *testing.T) {
	f := openSpringFixture(t, gpui.NewTestApp(), NewSpring(ms(300)))
	f.render(1)
	if n := f.pendingFrame(); n != 1 {
		t.Fatalf("pending frames = %d, want 1", n)
	}
	if v := f.advance(2000, 1); v != 1 {
		t.Fatalf("value = %v, want 1", v)
	}
	f.pendingFrame()
	f.ta.RunUntilParked()
	if n := f.pendingFrame(); n != 0 {
		t.Fatalf("pending frames = %d, want 0", n)
	}
}

// U:motion.rs:1805-1823
func TestASpringThatIsNotTravellingAdoptsItsTargetOnTheSpot(t *testing.T) {
	travelling := NewSpring(ms(300))
	f := openSpringFixture(t, gpui.NewTestApp(), travelling.WithTravel(false))
	if v := f.render(1); v != 1 {
		t.Fatalf("value = %v, want 1", v)
	}
	if n := f.pendingFrame(); n != 0 {
		t.Fatalf("pending frames = %d, want 0", n)
	}
	if v := f.advance(100, 5); v != 5 {
		t.Fatalf("value = %v, want 5", v)
	}

	// Travel resumes from the value the suspension left behind.
	f.policy = travelling
	if v := f.render(6); v != 5 {
		t.Fatalf("value = %v, want 5", v)
	}
	if next := f.advance(50, 6); !(5 < next && next < 6) {
		t.Fatalf("expected travel to resume from 5.0, got %v", next)
	}
}

// U:motion.rs:1825-1830
func TestAZeroResponseSpringResolvesInsteadOfDividingByItsPeriod(t *testing.T) {
	f := openSpringFixture(t, gpui.NewTestApp(), NewSpring(0))
	if v := f.render(1); v != 1 {
		t.Fatalf("value = %v, want 1", v)
	}
	if n := f.pendingFrame(); n != 0 {
		t.Fatalf("pending frames = %d, want 0", n)
	}
}

// U:motion.rs:1832-1852
func TestSpringRejectsNonFiniteOrNegativePhysicalParameters(t *testing.T) {
	spring := NewSpring(ms(300))
	nan := float32(math.NaN())
	inf := float32(math.Inf(1))
	if _, err := spring.TryWithDamping(nan); err != SpringErrorInvalidDamping {
		t.Fatalf("NaN damping: %v", err)
	}
	if _, err := spring.TryWithDamping(-0.1); err != SpringErrorInvalidDamping {
		t.Fatalf("negative damping: %v", err)
	}
	if _, err := spring.TryWithEpsilon(inf); err != SpringErrorInvalidEpsilon {
		t.Fatalf("infinite epsilon: %v", err)
	}
	if _, err := spring.TryWithEpsilon(-0.1); err != SpringErrorInvalidEpsilon {
		t.Fatalf("negative epsilon: %v", err)
	}
}

// U:motion.rs:1854-1861
func TestSpringReportsItsUnitSpecificSettlingTolerance(t *testing.T) {
	normalized := NewSpring(ms(180))
	pixels := NewSpring(ms(180)).WithEpsilon(0.1)
	if !(normalized.Epsilon() < 0.01) || pixels.Epsilon() != 0.1 {
		t.Fatalf("epsilons = %v, %v", normalized.Epsilon(), pixels.Epsilon())
	}
}

// U:motion.rs:1863-1872
func TestReducedMotionAdoptsTheSpringTargetWithoutRequestingAFrame(t *testing.T) {
	ta := gpui.NewTestApp()
	reduceMotion(ta)
	f := openSpringFixture(t, ta, NewSpring(ms(350)).WithDamping(0.7))
	if v := f.render(1); v != 1 {
		t.Fatalf("value = %v, want 1", v)
	}
	if n := f.pendingFrame(); n != 0 {
		t.Fatalf("pending frames = %d, want 0", n)
	}
}
