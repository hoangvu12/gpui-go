package gpui

// Tests for the Transition style property and the per-element style
// diff (browser-core ticket 44). The timing numbers are pinned against
// Chromium's formulas (the citations live in transition.go and
// motion_easing.go's file comments):
//   - steps: floor(steps*t + offset) clamped to [0, jumps] over jumps
//     (ui/gfx/animation/keyframe/timing_function.cc:98-133);
//   - the reversing shortening factor: eased progress × old factor +
//     (1 − old factor) (css_animations.cc:2890-2912);
//   - the easing keyword curves against Chromium's published samples.

import (
	"testing"
	"time"
)

// transitionBoxView is the root view of the transition tests: one div
// whose width, opacity and background the test drives through the view
// state, re-rendered on Refresh like any view.
type transitionBoxView struct {
	width  float32
	opaque bool
	redBG  bool
	rules  []TransitionRule
}

// Render implements Render: the box with the test's current state.
func (v *transitionBoxView) Render(w *Window, cx *Context[transitionBoxView]) AnyElement {
	opacity := float32(1)
	if !v.opaque {
		opacity = 0
	}
	bg := Rgb(0xFF0000)
	if !v.redBG {
		bg = Rgb(0x0000FF)
	}
	el := Div().Flex().ID("box").DebugSelector("box").
		Size(PxLength(v.width), PxLength(40)).
		Opacity(opacity).
		Bg(bg)
	if v.rules != nil {
		el = el.Transition(v.rules...)
	}
	return el.IntoElement()
}

// msT is milliseconds as a duration.
func msT(n int64) time.Duration { return time.Duration(n) * time.Millisecond }

// openTransitionWindow opens a 400x100 test window rendering one
// 40px-tall div (id "box", debug selector "box") whose width, opacity
// and background read the returned view state, with the given rules on
// its style.
func openTransitionWindow(t *testing.T, rules []TransitionRule) (*TestApp, *Window, *transitionBoxView) {
	t.Helper()
	ta := NewTestApp()
	v := &transitionBoxView{width: 100, opaque: true, redBG: true, rules: rules}
	win := ta.OpenWindowWithOptions[transitionBoxView, *transitionBoxView](WindowOptions{Bounds: Bounds{Size: Size{Width: 400, Height: 100}}},
		func(state *transitionBoxView, _ *Window, _ *Context[transitionBoxView]) {
			*state = *v
			v = state
		})
	_ = win
	return ta, win.Window(), v
}

// boxWidth reads the box's debug-selector width from the last
// completed frame.
func boxWidthOf(t *testing.T, ta *TestApp, w *Window) float32 {
	t.Helper()
	cx := NewVisualTestContext(w, ta)
	b, ok := cx.DebugBounds("box")
	if !ok {
		t.Fatal("the box has no debug bounds")
	}
	return b.Size.Width
}

func refreshWindow(ta *TestApp, w *Window) {
	ta.Update(func(*App) { w.Refresh() })
	ta.RunUntilParked()
}

// TestStepsEasingMatchesChromiumFormula pins steps() against
// gfx::StepsTimingFunction::GetValue (ui/gfx/animation/keyframe/
// timing_function.cc:98-133): floor(steps*t + offset)/jumps, with jumps
// and offset from the step position.
func TestStepsEasingMatchesChromiumFormula(t *testing.T) {
	cases := []struct {
		count    uint32
		position StepPosition
		progress float32
		want     float32
	}{
		{4, StepPositionJumpEnd, 0.5, 0.5},             // floor(4*0.5)/4 = 2/4
		{4, StepPositionJumpEnd, 0.4999, 0.25},         // floor(4*0.4999)/4 = 1/4
		{4, StepPositionJumpStart, 0.5, 0.75},          // (floor(2)+1)/4
		{4, StepPositionJumpBoth, 0.5, 0.6},            // (floor(2)+1)/5
		{4, StepPositionJumpNone, 0.5, float32(2) / 3}, // floor(2)/3
		{4, StepPositionJumpEnd, 1, 1},                 // the end clamps
		{4, StepPositionJumpStart, 0, 0.25},            // the start jumps
	}
	for _, c := range cases {
		easing, err := EasingSteps(c.count, c.position)
		if err != nil {
			t.Fatalf("EasingSteps(%d, %v): %v", c.count, c.position, err)
		}
		if got := easing.Sample(c.progress); abs32(got-c.want) > 1e-6 {
			t.Errorf("steps(%d, %v)(%v) = %v, want %v", c.count, c.position, c.progress, got, c.want)
		}
	}
}

// TestEasingKeywordCurvesMatchChromiumSamples pins the CSS keyword
// curves against Chromium's published reference samples (the table
// base's easing tests pin: ease(0.5) = 0.802, …).
func TestEasingKeywordCurvesMatchChromiumSamples(t *testing.T) {
	samples := []struct {
		easing Easing
		input  float32
		want   float32
	}{
		{EasingEase(), 0.2, 0.295},
		{EasingEase(), 0.5, 0.802},
		{EasingEase(), 0.8, 0.976},
		{EasingEaseIn(), 0.2, 0.062},
		{EasingEaseIn(), 0.5, 0.315},
		{EasingEaseIn(), 0.8, 0.692},
		{EasingEaseOut(), 0.2, 0.308},
		{EasingEaseOut(), 0.5, 0.685},
		{EasingEaseOut(), 0.8, 0.938},
		{EasingEaseInOut(), 0.2, 0.082},
		{EasingEaseInOut(), 0.5, 0.5},
		{EasingEaseInOut(), 0.8, 0.918},
	}
	for _, s := range samples {
		if got := s.easing.Sample(s.input); abs32(got-s.want) >= 0.002 {
			t.Errorf("%v(%v) = %v, want %v", s.easing, s.input, got, s.want)
		}
	}
}

// TestTransitionStyleAnimatesWidthLinearly drives a width transition
// end-to-end through the per-element style diff: 100 → 300 px over 400ms
// linear samples 150 at 100ms, 200 at 200ms and 300 at 400ms; the frame
// request keeps arriving while it runs.
func TestTransitionStyleAnimatesWidthLinearly(t *testing.T) {
	ta, w, v := openTransitionWindow(t, []TransitionRule{
		NewTransitionRule(TransitionWidth, msT(400)).WithEasing(EasingLinear()),
	})
	if got := boxWidthOf(t, ta, w); got != 100 {
		t.Fatalf("initial width = %v, want 100", got)
	}

	// The change starts the transition at the next frame.
	v.width = 300
	refreshWindow(ta, w)
	if got := boxWidthOf(t, ta, w); got != 100 {
		t.Fatalf("width at the change = %v, want 100 (the transition starts at the before-change value)", got)
	}
	if n := w.SimulateNextFrame(ta.App()); n != 1 {
		t.Fatalf("the running transition requested %v frames, want 1", n)
	}

	ta.AdvanceClock(100)
	w.SimulateNextFrame(ta.App())
	if got := boxWidthOf(t, ta, w); got != 150 {
		t.Fatalf("width at 100ms = %v, want 150", got)
	}
	ta.AdvanceClock(100)
	w.SimulateNextFrame(ta.App())
	if got := boxWidthOf(t, ta, w); got != 200 {
		t.Fatalf("width at 200ms = %v, want 200", got)
	}

	// Finished: the target holds and no more frames are requested.
	ta.AdvanceClock(200)
	w.SimulateNextFrame(ta.App())
	ta.RunUntilParked()
	if got := boxWidthOf(t, ta, w); got != 300 {
		t.Fatalf("width at 400ms = %v, want 300", got)
	}
	if n := w.SimulateNextFrame(ta.App()); n != 0 {
		t.Fatalf("a finished transition requested %v frames, want 0", n)
	}
}

// TestTransitionStyleSteps samples a stepped width transition: 800ms
// steps(4, jump-end) at 200ms is floor(4*0.25)/4 = 1/4 of the way.
func TestTransitionStyleSteps(t *testing.T) {
	steps, err := EasingSteps(4, StepPositionJumpEnd)
	if err != nil {
		t.Fatal(err)
	}
	ta, w, v := openTransitionWindow(t, []TransitionRule{
		NewTransitionRule(TransitionWidth, msT(800)).WithEasing(steps),
	})
	v.width = 200
	refreshWindow(ta, w)
	ta.AdvanceClock(200)
	w.SimulateNextFrame(ta.App())
	if got := boxWidthOf(t, ta, w); got != 125 {
		t.Fatalf("width at 200ms of steps(4) = %v, want 125", got)
	}
}

// TestTransitionStyleEase differs from linear at the same instant: the
// eased curve is at ease(0.5) = 0.802 of the way at 200 of 400ms.
func TestTransitionStyleEase(t *testing.T) {
	ta, w, v := openTransitionWindow(t, []TransitionRule{
		NewTransitionRule(TransitionWidth, msT(400)),
	})
	v.width = 300
	refreshWindow(ta, w)
	ta.AdvanceClock(200)
	w.SimulateNextFrame(ta.App())
	want := 100 + 200*EasingEase().Sample(0.5)
	// The layout width is device-pixel-snapped at the test window's 2.0
	// scale (0.5 logical px granularity).
	if got := boxWidthOf(t, ta, w); abs32(got-want) > 0.6 {
		t.Fatalf("width at 200ms of ease = %v, want %v", got, want)
	}
}

// TestTransitionStyleDelayHoldsTheStartValue: a positive delay keeps
// the before-change value until it elapses.
func TestTransitionStyleDelayHoldsTheStartValue(t *testing.T) {
	ta, w, v := openTransitionWindow(t, []TransitionRule{
		NewTransitionRule(TransitionWidth, msT(400)).WithEasing(EasingLinear()).
			WithDelay(SignedDurationPositive(msT(100))),
	})
	v.width = 300
	refreshWindow(ta, w)
	ta.AdvanceClock(50)
	w.SimulateNextFrame(ta.App())
	if got := boxWidthOf(t, ta, w); got != 100 {
		t.Fatalf("width during the delay = %v, want 100", got)
	}
	ta.AdvanceClock(100)
	w.SimulateNextFrame(ta.App())
	// 50ms into the 400ms interval.
	if got := boxWidthOf(t, ta, w); got != 125 {
		t.Fatalf("width at 50ms past the delay = %v, want 125", got)
	}
}

// TestTransitionStyleReversalAppliesTheShorteningRule pins the CSS
// reversing rule (css_animations.cc:2890-2912): a 400ms linear
// transition 100 → 300 reversed at 200ms (progress 0.5) shortens to
// 200ms and runs from the current value 200 back to 100, so at 100ms
// later the value is 150.
func TestTransitionStyleReversalAppliesTheShorteningRule(t *testing.T) {
	ta, w, v := openTransitionWindow(t, []TransitionRule{
		NewTransitionRule(TransitionWidth, msT(400)).WithEasing(EasingLinear()),
	})
	v.width = 300
	refreshWindow(ta, w)
	ta.AdvanceClock(200)
	w.SimulateNextFrame(ta.App())
	if got := boxWidthOf(t, ta, w); got != 200 {
		t.Fatalf("width at 200ms = %v, want 200", got)
	}

	// Reverse back to the start: the shortening factor is 0.5.
	v.width = 100
	refreshWindow(ta, w)
	ta.AdvanceClock(100)
	w.SimulateNextFrame(ta.App())
	if got := boxWidthOf(t, ta, w); got != 150 {
		t.Fatalf("width at 100ms of the reversed (shortened) run = %v, want 150", got)
	}

	// The shortened run finishes at 200ms, not 400ms.
	ta.AdvanceClock(100)
	w.SimulateNextFrame(ta.App())
	if got := boxWidthOf(t, ta, w); got != 100 {
		t.Fatalf("width at the shortened run's end = %v, want 100", got)
	}
}

// TestTransitionStyleRetargetRunsFromTheCurrentValue: a change to a
// NEW value (not the reversing-adjusted start) starts a fresh full
// duration from the current value.
func TestTransitionStyleRetargetRunsFromTheCurrentValue(t *testing.T) {
	ta, w, v := openTransitionWindow(t, []TransitionRule{
		NewTransitionRule(TransitionWidth, msT(400)).WithEasing(EasingLinear()),
	})
	v.width = 300
	refreshWindow(ta, w)
	ta.AdvanceClock(200)
	w.SimulateNextFrame(ta.App())

	// Retarget to 500 (not 100, so no reversal shortening).
	v.width = 500
	refreshWindow(ta, w)
	ta.AdvanceClock(100)
	w.SimulateNextFrame(ta.App())
	if got := boxWidthOf(t, ta, w); got != 275 {
		t.Fatalf("width at 100ms of the retargeted run = %v, want 275 (200 + 0.25*(500-200))", got)
	}
}

// TestTransitionStyleReducedMotionCompletesImmediately: with reduce
// motion on, the target applies at the change with no run.
func TestTransitionStyleReducedMotionCompletesImmediately(t *testing.T) {
	ta, w, v := openTransitionWindow(t, []TransitionRule{
		NewTransitionRule(TransitionWidth, msT(400)).WithEasing(EasingLinear()),
	})
	ta.Update(func(app *App) { app.SetReduceMotion(true) })
	v.width = 300
	refreshWindow(ta, w)
	if got := boxWidthOf(t, ta, w); got != 300 {
		t.Fatalf("width under reduced motion = %v, want 300", got)
	}
	if n := w.SimulateNextFrame(ta.App()); n != 0 {
		t.Fatalf("reduced motion requested %v frames, want 0", n)
	}
}

// TestTransitionStyleRequiresAnElementID: the per-element diff keys its
// state on element identity; an id-less element with a Transition panics
// with the documented message.
func TestTransitionStyleRequiresAnElementID(t *testing.T) {
	defer func() {
		r := recover()
		msg, _ := r.(string)
		if msg == "" || !containsStr(msg, "Transition style requires an element ID") {
			t.Fatalf("panic = %v, want the missing-element-ID message", r)
		}
	}()
	ta := NewTestApp()
	ta.OpenWindowWithOptions[idlessBoxView, *idlessBoxView](WindowOptions{Bounds: Bounds{Size: Size{Width: 400, Height: 100}}}, nil)
	ta.RunUntilParked()
}

// TestTransitionStyleZeroCostWithoutRules: an element without a
// Transition requests no animation frames (the per-element diff is one
// length check).
func TestTransitionStyleZeroCostWithoutRules(t *testing.T) {
	ta, w, v := openTransitionWindow(t, nil)
	v.width = 300
	refreshWindow(ta, w)
	if got := boxWidthOf(t, ta, w); got != 300 {
		t.Fatalf("width without transitions = %v, want 300 (applied at once)", got)
	}
	if n := w.SimulateNextFrame(ta.App()); n != 0 {
		t.Fatalf("an element without transitions requested %v frames, want 0", n)
	}
}

// TestAnimatedColorLerpsThroughSRGB pins the colour interpolation space
// (CSS Color 4's legacy sRGB interpolation, the rgb() channelwise
// midpoint): red → blue at 0.5 is the gamma-encoded midpoint, dark
// purple, not HSL's bright magenta.
func TestAnimatedColorLerpsThroughSRGB(t *testing.T) {
	red := animatedValue{color: RgbaToHsla(0xFF0000FF)}
	blue := animatedValue{color: RgbaToHsla(0x0000FFFF)}
	mid := red.lerp(TransitionBackground, blue, 0.5)
	rgba := hslaToRgba(mid.color)
	if abs32(rgba.R-0.5) > 1e-3 || rgba.G > 1e-3 || abs32(rgba.B-0.5) > 1e-3 || rgba.A < 1 {
		t.Fatalf("red→blue at 0.5 = rgba(%v, %v, %v, %v), want the sRGB midpoint (0.5, 0, 0.5, 1)",
			rgba.R, rgba.G, rgba.B, rgba.A)
	}
}

// TestMatchTransitionRuleLastWinsAndCombinedGates: the last matching
// rule wins (css_animations.cc:2870-2873) and a rule whose combined
// delay and duration is not positive never transitions (:2878-2885).
func TestMatchTransitionRuleLastWinsAndCombinedGates(t *testing.T) {
	rules := []TransitionRule{
		NewTransitionRule(TransitionWidth, msT(100)),
		NewTransitionRule(TransitionWidth, msT(300)),
	}
	rule, ok := matchTransitionRule(rules, TransitionWidth)
	if !ok || rule.Duration != msT(300) {
		t.Fatalf("matching rule = %+v, %v; want the last (300ms)", rule, ok)
	}
	if all, ok := matchTransitionRule([]TransitionRule{NewTransitionRule(TransitionAll, msT(50))}, TransitionOpacity); !ok || all.Duration != msT(50) {
		t.Fatal("transition-all did not cover opacity")
	}
	if _, ok := matchTransitionRule(rules, TransitionOpacity); ok {
		t.Fatal("a width-only list must not cover opacity")
	}
	if zero := NewTransitionRule(TransitionWidth, 0); zero.combined() > 0 {
		t.Fatal("a zero-duration rule's combined time must gate the transition off")
	}
	neg := TransitionRule{Property: TransitionWidth, Delay: SignedDurationNegative(msT(100))}
	if neg.combined() > 0 {
		t.Fatal("a negative-delay zero-duration rule must still not transition")
	}
}

// TestTransitionStyleInterpolatesOpacityAndBackground drives the paint
// properties through the same diff: opacity 1 → 0 and red → blue,
// sampled at the 200ms midpoint, land on the scene's painted quad as
// alpha 0.5 and the sRGB midpoint colour.
func TestTransitionStyleInterpolatesOpacityAndBackground(t *testing.T) {
	ta, w, v := openTransitionWindow(t, []TransitionRule{
		NewTransitionRule(TransitionOpacity, msT(400)).WithEasing(EasingLinear()),
		NewTransitionRule(TransitionBackground, msT(400)).WithEasing(EasingLinear()),
	})
	v.opaque = false
	v.redBG = false
	refreshWindow(ta, w)
	ta.AdvanceClock(200)
	w.SimulateNextFrame(ta.App())

	scene := LastDrawnScene(w)
	if scene == nil {
		t.Fatal("no drawn scene")
	}
	quads, err := scene.Quads()
	if err != nil {
		t.Fatal(err)
	}
	// The test window's device scale is 2: the 100x40 box is 200x80.
	var found bool
	for _, q := range quads {
		if q.Bounds.Size.Width == 200 && q.Bounds.Origin.X == 0 {
			rgba := hslaToRgba(q.Background.Solid)
			if abs32(rgba.A-0.5) > 1e-3 {
				t.Fatalf("painted alpha at the midpoint = %v, want 0.5", rgba.A)
			}
			if abs32(rgba.R-0.5) > 1e-3 || rgba.G > 1e-3 || abs32(rgba.B-0.5) > 1e-3 {
				t.Fatalf("painted background at the midpoint = rgba(%v,%v,%v), want (0.5, 0, 0.5)",
					rgba.R, rgba.G, rgba.B)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("the box's quad was not in the scene")
	}
}

// TestTransitionStyleNegativeDelayStartsInsideTheInterval: a negative
// delay starts the transition partway through (SignedDuration's
// semantics, U:motion/timing.rs:18-27; CSS transition-delay < 0).
func TestTransitionStyleNegativeDelayStartsInsideTheInterval(t *testing.T) {
	ta, w, v := openTransitionWindow(t, []TransitionRule{
		NewTransitionRule(TransitionWidth, msT(400)).WithEasing(EasingLinear()).
			WithDelay(SignedDurationNegative(msT(200))),
	})
	v.width = 300
	refreshWindow(ta, w)
	// The run starts 200ms in: the value at the change is already 200.
	if got := boxWidthOf(t, ta, w); got != 200 {
		t.Fatalf("width with a -200ms delay at the change = %v, want 200", got)
	}
}

// idlessBoxView renders a div with a Transition and no ID: the
// per-element style diff cannot key its state.
type idlessBoxView struct{}

func (v *idlessBoxView) Render(w *Window, cx *Context[idlessBoxView]) AnyElement {
	return Div().Flex().Size(PxLength(100), PxLength(40)).
		Transition(NewTransitionRule(TransitionWidth, msT(400))).
		IntoElement()
}

// containsStr is strings.Contains without the import.
func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
