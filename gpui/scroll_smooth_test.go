package gpui

// browser-core ticket 45 (.scratch/browser-core/issues/
// 45-smooth-scrolling.md), spec D13: the smooth-scroll engine's
// library-level gate. The curve itself is Chromium's formula set,
// fitted from the source:
//
//   - the durations (cc/animation/scroll_offset_animation_curve.cc:
//     EaseInOutSegmentDuration :148-178): 150ms constant for the
//     keyboard (kConstantDuration 9/60), the inverse-delta ramp for the
//     wheel (12/60s at 120px down to 6/60s at 480px, clamped
//     [100ms, 200ms]) and sqrt(|delta|)/60 capped at 1500ms for
//     programmatic scrolls;
//   - the bezier: ease-in-out (0.42, 0, 0.58, 1) for wheel and keyboard
//     (:28-34), the tweaked programmatic curve (0.4, 0, 0, 1,
//     cc/base/features.cc:258-266) for programmatic ones;
//   - the retarget (UpdateTarget :337-396): the new segment starts at
//     the current animated position, its duration follows the source
//     rule bounded by the velocity, and the new bezier's first control
//     point scales to preserve the velocity;
//   - the retarget target: the running animation's target plus the
//     delta, clamped (ScrollOffsetAnimationImpl::ScrollAnimationUpdate-
//     Target, scroll_offset_animations_impl.cc:118-135);
//   - precise deltas never animate (InputHandler::ShouldAnimateScroll,
//     input_handler.cc:2484-2492) and cancel a running animation
//     (ScrollBegin's precise arm, :149-161);
//   - reduced motion turns every scroll instant (the D13 gate — the
//     renderer pref Chromium gates on SPI_GETCLIENTAREAANIMATION,
//     web_contents_impl.cc:4042-4046, animation_win.cc:17-44).
//
// The deterministic clock is the TestApp's: advance it, deliver one
// frame (Window.SimulateNextFrame) and the tick samples the curve at
// exactly that instant — the same shape the parity harness's Wait step
// gives the replay.

import (
	"math"
	"testing"
	"time"
)

// newSmoothScrollFixture is newScrollFixture with smooth scrolling on
// (the TestApp default is off — the recorder's Chrome shape).
func newSmoothScrollFixture(t *testing.T, size Size, build func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement) *scrollFixture {
	t.Helper()
	ta := NewTestApp().WithSmoothScrolling(true)
	app := ta.App()
	window := NewTestWindow(app, size)
	view := NewEntity(app, window.Scope(), func(v *scrollView, cx *Context[scrollView]) { v.build = build })
	window.SetRootView(ViewOf(view))
	f := &scrollFixture{t: t, ta: ta, app: app, window: window, view: view}
	f.draw()
	return f
}

// newSmoothKeyboardFixture is newSmoothScrollFixture plus a focusable
// target row inside the scroller (the keyboard scroll's start node —
// the focused element's chain, scroll_manager.cc:216-232).
func newSmoothKeyboardFixture(t *testing.T) (f *scrollFixture, handle ScrollHandle, focus FocusHandle) {
	t.Helper()
	ta := NewTestApp().WithSmoothScrolling(true)
	app := ta.App()
	window := NewTestWindow(app, Size{Width: 400, Height: 300})
	handle = NewScrollHandle()
	focus = app.FocusHandle().TabStop(true)
	view := NewEntity(app, window.Scope(), func(v *scrollView, cx *Context[scrollView]) {
		v.build = func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
			scroller := Div().Flex().ID("scroller").W(Px(100)).H(Px(100)).FlexCol().OverflowYScroll().TrackScroll(handle).
				Child(Div().Flex().W(Px(100)).H(Px(300)).FlexShrink0()).
				Child(Div().Flex().ID("target").W(Px(100)).H(Px(50)).FlexShrink0().TrackFocus(focus))
			return scroller.IntoElement()
		}
	})
	window.SetRootView(ViewOf(view))
	f = &scrollFixture{t: t, ta: ta, app: app, window: window, view: view}
	f.draw()
	window.Focus(focus)
	ta.RunUntilParked()
	f.draw()
	return f, handle, focus
}

// advance ticks the animation: advance the virtual clock by ms, deliver
// the pending next-frame callbacks (the tick) and park, so the frame
// state and the handle read the curve's sample at that instant.
func (f *scrollFixture) advance(ms int64) {
	f.t.Helper()
	f.ta.AdvanceClock(ms)
	f.window.SimulateNextFrame(f.app)
	f.ta.RunUntilParked()
}

// independentBezierY evaluates the unit cubic bezier's y at progress x
// with its own Newton solver — an implementation independent of the
// engine's (the discrimination a solver bug needs).
func independentBezierY(x1, y1, x2, y2, x float64) float64 {
	cx, cy := 3*x1, 3*y1
	bx, by := 3*(x2-x1)-cx, 3*(y2-y1)-cy
	ax, ay := 1-cx-bx, 1-cy-by
	sampleX := func(t float64) float64 { return ((ax*t+bx)*t + cx) * t }
	sampleY := func(t float64) float64 { return ((ay*t+by)*t + cy) * t }
	s := x
	for i := 0; i < 40; i++ {
		err := sampleX(s) - x
		if math.Abs(err) < 1e-9 {
			break
		}
		d := 3*ax*s*s + 2*bx*s + cx
		if math.Abs(d) < 1e-9 {
			break
		}
		s -= err / d
		if s < 0 {
			s = 0
		}
		if s > 1 {
			s = 1
		}
	}
	return sampleY(s)
}

// TestSmoothScrollWheelCurveAndDuration pins the wheel path: a
// non-precise 100px line delta (one Windows notch, three lines at
// 100/3 px) animates to clamp(current + delta) over the inverse-delta
// duration — 200ms at 100px (the 12/60 clamp) — along ease-in-out,
// whose symmetric midpoint is exactly half the delta, and the animation
// lands on the target with no frames requested past its end.
func TestSmoothScrollWheelCurveAndDuration(t *testing.T) {
	handle := NewScrollHandle()
	f := newSmoothScrollFixture(t, Size{Width: 400, Height: 300}, func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
		return divScroller(handle, 100, 300, func(d *DivElement) *DivElement { return d.OverflowYScroll() }).IntoElement()
	})
	if handle.MaxOffset() != (Point{Y: 200}) {
		t.Fatalf("max offset = %v, want (0,200)", handle.MaxOffset())
	}

	// One notch: three lines at 100/3 px = 100px, the latched scroller
	// animates toward clamp(0 + (-100)) = -100.
	f.wheel(50, 50, ScrollDeltaLines(0, -3), TouchPhaseMoved)
	// Nothing moved yet: the first visible change is the first tick.
	if handle.Offset() != (Point{}) {
		t.Fatalf("offset before the first tick = %v, want 0 (the animation has not sampled)", handle.Offset())
	}
	// The first tick samples at t=0: still the initial value.
	if n := f.window.SimulateNextFrame(f.app); n != 1 {
		t.Fatalf("pending frames after the wheel = %d, want 1", n)
	}
	f.ta.RunUntilParked()
	if handle.Offset() != (Point{}) {
		t.Fatalf("offset at t=0 = %v, want 0", handle.Offset())
	}

	// The midpoint of a 200ms animation: ease-in-out's symmetric
	// midpoint is exactly 0.5, so exactly -50.
	f.advance(100)
	if got := handle.Offset().Y; got != -50 {
		t.Fatalf("offset at t=100ms (midpoint) = %v, want -50 exactly", got)
	}

	// A later sample against an independent bezier evaluation
	// (progress 149/200 of 100px).
	f.advance(49)
	want := float32(independentBezierY(0.42, 0, 0.58, 1, 149.0/200.0) * 100)
	if diff := abs32(handle.Offset().Y - (-want)); diff > 0.01 {
		t.Fatalf("offset at t=149ms = %v, want -%.4f (the independent bezier sample)", handle.Offset().Y, want)
	}

	// Past the end: exactly the target, and no frames requested anymore.
	f.advance(100) // t = 249ms > 200ms
	if handle.Offset() != (Point{Y: -100}) {
		t.Fatalf("offset after the animation = %v, want (0,-100)", handle.Offset())
	}
	if n := f.window.SimulateNextFrame(f.app); n != 0 {
		t.Fatalf("pending frames after the animation = %d, want 0 (frames only while running)", n)
	}
}

// TestSmoothScrollWheelInverseDeltaRamp pins the duration rule per
// delta: 100px and 120px clamp to 200ms (12/60), 240px takes 10/60s and
// 480px+ floors at 100ms (6/60) — the "snappy fling" ramp
// (kInverseDeltaRampStartPx/EndPx, scroll_offset_animation_curve.cc:
// 44-54, 166-170).
func TestSmoothScrollWheelInverseDeltaRamp(t *testing.T) {
	cases := []struct {
		delta float32
		want  time.Duration
	}{
		{100, 200 * time.Millisecond},
		{120, 200 * time.Millisecond},
		{240, 10 * time.Second / 60},
		{480, 100 * time.Millisecond},
		{2000, 100 * time.Millisecond},
	}
	for _, c := range cases {
		if got := smoothScrollSegmentDuration(smoothScrollWheel, Point{Y: c.delta}, 0); got != c.want {
			t.Fatalf("wheel duration(%vpx) = %v, want %v", c.delta, got, c.want)
		}
	}
	// The keyboard's constant duration and the programmatic sqrt rule.
	if got := smoothScrollSegmentDuration(smoothScrollKeyboard, Point{Y: 40}, 0); got != 150*time.Millisecond {
		t.Fatalf("keyboard duration = %v, want 150ms", got)
	}
	if got := smoothScrollSegmentDuration(smoothScrollProgrammatic, Point{Y: 400}, 0); got != 20*time.Second/60 {
		t.Fatalf("programmatic duration(400px) = %v, want sqrt(400)/60 = 333.33ms", got)
	}
	if got := smoothScrollSegmentDuration(smoothScrollProgrammatic, Point{Y: 90000}, 0); got != 1500*time.Millisecond {
		t.Fatalf("programmatic duration(90000px) = %v, want the 1500ms cap", got)
	}
}

// TestSmoothScrollKeyboardCurve pins the keyboard path: an ArrowDown
// (the focused chain's scroller, 40px per line step) animates over the
// constant 150ms, the offset at the midpoint is exactly half the step,
// and the animation lands on the step.
func TestSmoothScrollKeyboardCurve(t *testing.T) {
	f, handle, _ := newSmoothKeyboardFixture(t)
	f.window.DispatchKeyEvent(&KeyDownEvent{Keystroke: Keystroke{Key: "arrowdown"}}, f.app)
	if handle.Offset() != (Point{}) {
		t.Fatalf("offset before the first tick = %v, want 0", handle.Offset())
	}
	// Half of 150ms: ease-in-out's midpoint is exactly 0.5 — 20px.
	f.advance(75)
	if handle.Offset() != (Point{Y: -20}) {
		t.Fatalf("offset at the midpoint = %v, want (0,-20) exactly", handle.Offset())
	}
	f.advance(75)
	if handle.Offset() != (Point{Y: -40}) {
		t.Fatalf("offset after the animation = %v, want (0,-40)", handle.Offset())
	}
}

// TestSmoothScrollWheelRetarget pins UpdateTarget's shape on the wheel
// path: a second notch while the animation runs aims at the running
// target plus the delta (scroll_offset_animations_impl.cc:118-135),
// with the new segment starting at the current animated position and
// the source's duration (the velocity bound is infinite at rest).
func TestSmoothScrollWheelRetarget(t *testing.T) {
	handle := NewScrollHandle()
	f := newSmoothScrollFixture(t, Size{Width: 400, Height: 300}, func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
		return divScroller(handle, 100, 300, func(d *DivElement) *DivElement { return d.OverflowYScroll() }).IntoElement()
	})
	// First notch: 100px over 200ms (t=0 start).
	f.wheel(50, 50, ScrollDeltaLines(0, -3), TouchPhaseMoved)
	f.advance(100)
	if handle.Offset() != (Point{Y: -50}) {
		t.Fatalf("midpoint = %v, want (0,-50)", handle.Offset())
	}
	// The retarget at t=100ms: the running animation's target (-100)
	// plus the notch (-100) = -200; the new segment starts at the
	// current position (-50) with the wheel duration of the new delta
	// (150px: 14 − 150/60 = 11.5 sixtieths = 191.667ms, not bounded by
	// the velocity — 862px/s bounds it at 435ms).
	f.wheel(50, 50, ScrollDeltaLines(0, -3), TouchPhaseMoved)
	an := handle.state.animator
	if an == nil || an.anim == nil {
		t.Fatal("the retarget dropped the animation")
	}
	if an.anim.target != (Point{Y: -200}) {
		t.Fatalf("retargeted target = %v, want (0,-200) (the running target plus the delta)", an.anim.target)
	}
	if an.anim.initial != (Point{Y: -50}) {
		t.Fatalf("retargeted initial = %v, want (0,-50) (the current animated position)", an.anim.initial)
	}
	segment := 11.5 / 60 * float64(time.Second) // the 150px wheel segment
	wantDur := 100*time.Millisecond + time.Duration(segment)
	if an.anim.duration != wantDur {
		t.Fatalf("retargeted duration = %v, want %v (t=100 plus the 150px segment)", an.anim.duration, wantDur)
	}
	// The retargeted animation runs out to its target.
	f.advance(250)
	if handle.Offset() != (Point{Y: -200}) {
		t.Fatalf("the retargeted animation's end = %v, want (0,-200)", handle.Offset())
	}
	if n := f.window.SimulateNextFrame(f.app); n != 0 {
		t.Fatalf("pending frames after the animation = %d, want 0", n)
	}
}

// TestSmoothScrollPreciseDeltasNeverAnimate pins the precise rule: a
// pixel (touchpad) delta scrolls instantly — no animation, no frame
// requests — and a precise delta during a running animation cancels it
// (InputHandler::ScrollBegin's precise arm, input_handler.cc:149-161).
func TestSmoothScrollPreciseDeltasNeverAnimate(t *testing.T) {
	handle := NewScrollHandle()
	f := newSmoothScrollFixture(t, Size{Width: 400, Height: 300}, func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
		return divScroller(handle, 100, 300, func(d *DivElement) *DivElement { return d.OverflowYScroll() }).IntoElement()
	})
	// A precise delta lands at once.
	f.wheel(50, 50, ScrollDeltaPixels(0, -30), TouchPhaseMoved)
	if handle.Offset() != (Point{Y: -30}) {
		t.Fatalf("precise delta offset = %v, want (0,-30) instantly", handle.Offset())
	}
	if n := f.window.SimulateNextFrame(f.app); n != 0 {
		t.Fatalf("pending frames after a precise delta = %d, want 0", n)
	}
	// A precise delta during a running animation cancels it: the
	// animation's next tick must not fight the instant write.
	f.wheel(50, 50, ScrollDeltaLines(0, -3), TouchPhaseMoved)
	f.advance(100) // halfway to -30-100 = the animation toward -130, at -80
	if handle.Offset().Y != -80 {
		t.Fatalf("mid-animation offset = %v, want (0,-80)", handle.Offset())
	}
	f.wheel(50, 50, ScrollDeltaPixels(0, -10), TouchPhaseMoved)
	if handle.Offset() != (Point{Y: -90}) {
		t.Fatalf("precise cancel offset = %v, want (0,-90) instantly", handle.Offset())
	}
	f.advance(200)
	if handle.Offset() != (Point{Y: -90}) {
		t.Fatalf("offset after the cancelled animation's time = %v, want (0,-90) (the animation was cancelled, not resumed)", handle.Offset())
	}
}

// TestSmoothScrollReducedMotionIsInstant pins D13: under reduced motion
// (the one source, App.MotionReduced — the App flag or the pinned
// system preference) every scroll jumps at once and no frames are
// requested; an animation already running when the gate flips adopts
// its target at the next tick.
func TestSmoothScrollReducedMotionIsInstant(t *testing.T) {
	// The pinned system preference (ticket 74's seam).
	pinned := NewTestApp().WithSmoothScrolling(true).WithSystemPreferences(SystemPreferences{ReducedMotion: true})
	app := pinned.App()
	window := NewTestWindow(app, Size{Width: 400, Height: 300})
	handle := NewScrollHandle()
	focus := app.FocusHandle().TabStop(true)
	view := NewEntity(app, window.Scope(), func(v *scrollView, cx *Context[scrollView]) {
		v.build = func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
			scroller := Div().Flex().ID("scroller").W(Px(100)).H(Px(100)).FlexCol().OverflowYScroll().TrackScroll(handle).
				Child(Div().Flex().W(Px(100)).H(Px(300)).FlexShrink0()).
				Child(Div().Flex().ID("target").W(Px(100)).H(Px(50)).FlexShrink0().TrackFocus(focus))
			return scroller.IntoElement()
		}
	})
	window.SetRootView(ViewOf(view))
	pinned.RunUntilParked()
	window.Focus(focus)
	pinned.RunUntilParked()
	window.DispatchInput(&ScrollWheelEvent{
		Position:   Point{X: 50, Y: 50},
		Delta:      ScrollDeltaLines(0, -3),
		TouchPhase: TouchPhaseMoved,
	}, app)
	if handle.Offset() != (Point{Y: -100}) {
		t.Fatalf("wheel under reduced motion = %v, want (0,-100) instantly", handle.Offset())
	}
	window.DispatchKeyEvent(&KeyDownEvent{Keystroke: Keystroke{Key: "arrowdown"}}, app)
	if handle.Offset() != (Point{Y: -140}) {
		t.Fatalf("keyboard under reduced motion = %v, want (0,-140) instantly", handle.Offset())
	}
	handle.ScrollTo(Point{Y: -180}, ScrollBehaviorSmooth, window, app)
	if handle.Offset() != (Point{Y: -180}) {
		t.Fatalf("programmatic under reduced motion = %v, want (0,-180) instantly", handle.Offset())
	}
	if n := window.SimulateNextFrame(app); n != 0 {
		t.Fatalf("pending frames under reduced motion = %d, want 0", n)
	}

	// The gate flips mid-animation: the running animation adopts its
	// target at the next tick (the motion engine's rule).
	flipping := NewTestApp().WithSmoothScrolling(true)
	fapp := flipping.App()
	fwindow := NewTestWindow(fapp, Size{Width: 400, Height: 300})
	fhandle := NewScrollHandle()
	fview := NewEntity(fapp, fwindow.Scope(), func(v *scrollView, cx *Context[scrollView]) {
		v.build = func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
			return divScroller(fhandle, 100, 300, func(d *DivElement) *DivElement { return d.OverflowYScroll() }).IntoElement()
		}
	})
	fwindow.SetRootView(ViewOf(fview))
	flipping.RunUntilParked()
	fwindow.DispatchInput(&ScrollWheelEvent{
		Position:   Point{X: 50, Y: 50},
		Delta:      ScrollDeltaLines(0, -3),
		TouchPhase: TouchPhaseMoved,
	}, fapp)
	flipping.AdvanceClock(100)
	fwindow.SimulateNextFrame(fapp)
	if fhandle.Offset().Y != -50 {
		t.Fatalf("mid-animation offset = %v, want (0,-50)", fhandle.Offset())
	}
	fapp.SetReduceMotion(true)
	flipping.AdvanceClock(10)
	fwindow.SimulateNextFrame(fapp)
	if fhandle.Offset() != (Point{Y: -100}) {
		t.Fatalf("offset after the gate flipped = %v, want (0,-100) (the animation adopted its target)", fhandle.Offset())
	}
	if n := fwindow.SimulateNextFrame(fapp); n != 0 {
		t.Fatalf("pending frames after adopting the target = %d, want 0", n)
	}
}

// TestScrollHandleScrollToBehavior pins the programmatic API: smooth
// animates with the programmatic curve (the sqrt duration and the
// tweaked bezier), auto and instant jump, a smooth scroll replaces a
// running animation, and the reduced-motion and disabled gates fall
// back to the instant write.
func TestScrollHandleScrollToBehavior(t *testing.T) {
	handle := NewScrollHandle()
	f := newSmoothScrollFixture(t, Size{Width: 400, Height: 300}, func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
		return divScroller(handle, 100, 300, func(d *DivElement) *DivElement { return d.OverflowYScroll() }).IntoElement()
	})
	// Auto (and instant) jump, adjusting nothing (no animation runs).
	handle.ScrollTo(Point{Y: -40}, ScrollBehaviorAuto, f.window, f.app)
	if handle.Offset() != (Point{Y: -40}) {
		t.Fatalf("auto offset = %v, want (0,-40)", handle.Offset())
	}
	// Smooth: an 81px delta animates over sqrt(81)/60 = 150ms exactly.
	handle.ScrollTo(Point{Y: -121}, ScrollBehaviorSmooth, f.window, f.app)
	if handle.Offset() != (Point{Y: -40}) {
		t.Fatalf("smooth scroll before the first tick = %v, want (0,-40) (nothing sampled yet)", handle.Offset())
	}
	an := handle.state.animator
	if an == nil || an.anim == nil {
		t.Fatal("the smooth programmatic scroll did not start an animation")
	}
	if an.anim.target != (Point{Y: -121}) {
		t.Fatalf("target = %v, want (0,-121)", an.anim.target)
	}
	if an.anim.duration != 150*time.Millisecond {
		t.Fatalf("duration = %v, want sqrt(81)/60 = 150ms", an.anim.duration)
	}
	// The tweaked bezier's midpoint is not 0.5 (x1=0.4, x2=0): sample
	// it independently at the half time.
	half := independentBezierY(0.4, 0, 0, 1, 0.5) * 81
	f.advance(75)
	if diff := abs32(handle.Offset().Y - (-40 - float32(half))); diff > 0.02 {
		t.Fatalf("midpoint = %v, want -%.4f (the independent tweaked-bezier sample)", handle.Offset().Y, 40+half)
	}
	// The end lands on the target.
	f.advance(75)
	if handle.Offset() != (Point{Y: -121}) {
		t.Fatalf("end offset = %v, want (0,-121)", handle.Offset())
	}
	// A smooth scroll replaces a running animation (Chromium cancels the
	// user animation first, scrollable_area.cc:519-525): start a wheel
	// animation, then scroll programmatically — the new animation starts
	// at the current animated position and aims at the ScrollTo target.
	f.wheel(50, 50, ScrollDeltaLines(0, -3), TouchPhaseMoved)
	f.advance(100)
	if handle.Offset().Y != -160.5 {
		t.Fatalf("mid-animation offset = %v, want (0,-160.5)", handle.Offset())
	}
	handle.ScrollTo(Point{Y: -60}, ScrollBehaviorSmooth, f.window, f.app)
	if an := handle.state.animator; an == nil || an.anim == nil || an.anim.target != (Point{Y: -60}) || an.anim.initial.Y != -160.5 {
		t.Fatalf("the smooth programmatic scroll did not replace the running animation: target %v initial %v", an.anim.target, an.anim.initial)
	}
	f.advance(300)
	if handle.Offset() != (Point{Y: -60}) {
		t.Fatalf("offset after the replaced animation = %v, want (0,-60)", handle.Offset())
	}
	// The smooth-scrolling gate off: instant.
	f.app.SetSmoothScrolling(false)
	handle.ScrollTo(Point{Y: -100}, ScrollBehaviorSmooth, f.window, f.app)
	if handle.Offset() != (Point{Y: -100}) {
		t.Fatalf("smooth with the pref off = %v, want (0,-100) instantly", handle.Offset())
	}
}

// TestSmoothScrollClampsTheTargetIntoTheRange pins the range clamp on
// the animated path (browser-core 07's range + ticket 15's latch): a
// gesture whose delta runs past the end animates to -max and the outer
// scroller does not move (no chaining mid-gesture).
func TestSmoothScrollClampsTheTargetIntoTheRange(t *testing.T) {
	inner := NewScrollHandle()
	outer := NewScrollHandle()
	f := newSmoothScrollFixture(t, Size{Width: 300, Height: 300}, func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
		return Div().Flex().ID("root").SizeFull().Child(nestedWheelScrollers(outer, inner, nil)).IntoElement()
	})
	// One notch of 100px over the inner scroller (range 200): the target
	// clamps at -200 and the animation lands there; the outer never
	// moves.
	f.wheel(100, 100, ScrollDeltaLines(0, -6), TouchPhaseMoved)
	an := inner.state.animator
	if an == nil || an.anim == nil {
		t.Fatal("the wheel did not start an animation")
	}
	if an.anim.target != (Point{Y: -200}) {
		t.Fatalf("target = %v, want (0,-200) (the clamped end)", an.anim.target)
	}
	f.advance(250)
	if inner.Offset() != (Point{Y: -200}) {
		t.Fatalf("inner offset = %v, want (0,-200)", inner.Offset())
	}
	if outer.Offset() != (Point{}) {
		t.Fatalf("outer offset = %v, want 0 (no chaining mid-gesture)", outer.Offset())
	}
}

// TestSmoothScrollAnchoringAdjustsARunningAnimation pins browser-core
// 46's requirement that scroll anchoring keeps working with animated
// offsets: an anchoring adjustment (a row inserted above the viewport)
// shifts the offset AND the running animation's curve
// (ApplyAdjustment, scroll_offset_animation_curve.cc:300-305), so the
// animation continues to the shifted target instead of snapping back.
func TestSmoothScrollAnchoringAdjustsARunningAnimation(t *testing.T) {
	handle := NewScrollHandle()
	inserted := false
	f := newSmoothScrollFixture(t, Size{Width: 400, Height: 300}, func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
		scroller := Div().Flex().
			ID("scroller").
			W(Px(100)).
			H(Px(100)).
			TrackScroll(handle).
			OverflowYScroll().
			FlexCol()
		if inserted {
			scroller = scroller.Child(Div().Flex().ID("new").W(Px(100)).H(Px(50)).FlexShrink0().DebugSelector("new"))
		}
		for i := 0; i < 6; i++ {
			scroller = scroller.Child(Div().Flex().ID(rowID(i)).W(Px(100)).H(Px(50)).FlexShrink0().DebugSelector(rowID(i)))
		}
		return scroller.IntoElement()
	})
	// Start an animation from the top toward -100 (one notch).
	f.wheel(50, 50, ScrollDeltaLines(0, -3), TouchPhaseMoved)
	f.advance(100)
	if handle.Offset() != (Point{Y: -50}) {
		t.Fatalf("mid-animation offset = %v, want (0,-50)", handle.Offset())
	}
	// Insert a 50px row above the viewport: the anchoring shifts the
	// offset to -100 (keeping the visible content in place) and the
	// animation's target to -150.
	inserted = true
	f.update(func(v *scrollView) {})
	if handle.Offset() != (Point{Y: -100}) {
		t.Fatalf("offset after the anchoring = %v, want (0,-100)", handle.Offset())
	}
	an := handle.state.animator
	if an == nil || an.anim == nil {
		t.Fatal("the anchoring dropped the animation")
	}
	if an.anim.target != (Point{Y: -150}) {
		t.Fatalf("adjusted target = %v, want (0,-150) (the curve shifted with the offset)", an.anim.target)
	}
	if an.anim.initial != (Point{Y: -50}) {
		t.Fatalf("adjusted initial = %v, want (0,-50) (the whole curve shifted)", an.anim.initial)
	}
	// The animation runs out to the shifted target.
	f.advance(100)
	if handle.Offset() != (Point{Y: -150}) {
		t.Fatalf("offset after the animation = %v, want (0,-150) (the shifted target)", handle.Offset())
	}
}

// TestSmoothScrollSetOffsetAdjustsARunningAnimation pins the instant
// programmatic write's rule (crbug.com/1264266): SetOffset under a
// running animation adjusts the curve — the write stands and the
// animation continues from the shifted curve — instead of the next tick
// rolling it back.
func TestSmoothScrollSetOffsetAdjustsARunningAnimation(t *testing.T) {
	handle := NewScrollHandle()
	f := newSmoothScrollFixture(t, Size{Width: 400, Height: 300}, func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
		return divScroller(handle, 100, 300, func(d *DivElement) *DivElement { return d.OverflowYScroll() }).IntoElement()
	})
	f.wheel(50, 50, ScrollDeltaLines(0, -3), TouchPhaseMoved)
	f.advance(100)
	if handle.Offset() != (Point{Y: -50}) {
		t.Fatalf("mid-animation offset = %v, want (0,-50)", handle.Offset())
	}
	// An instant write of -80: the whole curve shifts by the write's
	// delta (-30), so the initial moves 0 → -30, the target -100 → -130,
	// and the curve still passes through the written offset at t=100
	// (the shifted curve's value there is -80) — the animation continues
	// to -130 instead of the next tick rolling back to -100.
	handle.SetOffset(Point{Y: -80})
	an := handle.state.animator
	if an == nil || an.anim == nil {
		t.Fatal("the instant write cancelled the animation")
	}
	if an.anim.initial != (Point{Y: -30}) || an.anim.target != (Point{Y: -130}) {
		t.Fatalf("adjusted curve = initial %v target %v, want (0,-30) and (0,-130)", an.anim.initial, an.anim.target)
	}
	if an.anim.valueAt(100*time.Millisecond) != (Point{Y: -80}) {
		t.Fatalf("the shifted curve at t=100 = %v, want (0,-80) (it passes through the written offset)", an.anim.valueAt(100*time.Millisecond))
	}
	f.advance(150)
	if handle.Offset() != (Point{Y: -130}) {
		t.Fatalf("offset after the animation = %v, want (0,-130)", handle.Offset())
	}
}

// TestSmoothScrollListWheelCurve pins the uniform list's smooth wheel
// path: a non-precise 100px line notch animates the list's pixel scroll
// top along the same wheel curve the div runs (200ms at 100px, the
// ease-in-out midpoint exactly half), the list's own scroll body
// applies each tick (the logical top rebases onto items), and a
// precise delta stays instant and cancels a running animation.
func TestSmoothScrollListWheelCurve(t *testing.T) {
	state := NewListState(10, 0).MeasureAll()
	handlerFired := 0
	state.SetScrollHandler(func(e *ListScrollEvent, w *Window, app *App) { handlerFired++ })
	f := newSmoothScrollFixture(t, Size{Width: 100, Height: 100}, func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
		return List(state, listRows(20)).SizeFull().IntoElement()
	})
	// One notch (three lines = 100px): the list's range is 100 (10 rows
	// of 20px in a 100px viewport), so the target clamps at the end.
	f.wheel(50, 50, ScrollDeltaLines(0, -3), TouchPhaseMoved)
	if got := state.ScrollPxOffsetForScrollbar(); got != (Point{}) {
		t.Fatalf("list px offset before the first tick = %v, want 0", got)
	}
	// Half of the 200ms wheel animation: the eased midpoint is exactly
	// half of the 100px delta → the pixel offset -50.
	f.advance(100)
	if got := state.ScrollPxOffsetForScrollbar(); got.Y != -50 {
		t.Fatalf("list px offset at the midpoint = %v, want (0,-50) exactly", got)
	}
	// The logical top rebased onto an item: -50px of 20px rows → item 2,
	// offset in item 10.
	if o := state.LogicalScrollTop(); o.ItemIx != 2 || o.OffsetInItem != 10 {
		t.Fatalf("logical scroll top at the midpoint = %+v, want (2, 10)", o)
	}
	// Past the end: the clamped target, the handler fired once per
	// moving tick, and no frames requested anymore.
	f.advance(100)
	if got := state.ScrollPxOffsetForScrollbar(); got != (Point{Y: -100}) {
		t.Fatalf("list px offset after the animation = %v, want (0,-100)", got)
	}
	if n := f.window.SimulateNextFrame(f.app); n != 0 {
		t.Fatalf("pending frames after the animation = %d, want 0", n)
	}
	moving := handlerFired
	// A precise delta during a running animation cancels it and scrolls
	// instantly. Scroll back up first (the list sits at its end): an
	// up-notch animates toward 0, and halfway through it a precise
	// delta cancels the animation and jumps.
	f.wheel(50, 50, ScrollDeltaLines(0, 3), TouchPhaseMoved)
	f.advance(100)
	if state.ScrollPxOffsetForScrollbar().Y != -50 {
		t.Fatalf("mid-animation list offset = %v, want (0,-50)", state.ScrollPxOffsetForScrollbar())
	}
	f.wheel(50, 50, ScrollDeltaPixels(0, -10), TouchPhaseMoved)
	if got := state.ScrollPxOffsetForScrollbar(); got != (Point{Y: -60}) {
		t.Fatalf("precise list offset = %v, want (0,-60) instantly", got)
	}
	f.advance(200)
	if got := state.ScrollPxOffsetForScrollbar(); got != (Point{Y: -60}) {
		t.Fatalf("list offset after the cancelled animation's time = %v, want (0,-60)", got)
	}
	_ = moving
}

// TestSmoothScrollSmallWheelDeltaScrollsInstantly pins the create
// threshold: a delta at or under 0.1px on both axes scrolls instantly
// (LayerTreeHostImpl::ScrollAnimationCreate's kEpsilon,
// layer_tree_host_impl.cc:4687-4694).
func TestSmoothScrollSmallWheelDeltaScrollsInstantly(t *testing.T) {
	handle := NewScrollHandle()
	f := newSmoothScrollFixture(t, Size{Width: 400, Height: 300}, func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
		return divScroller(handle, 100, 300, func(d *DivElement) *DivElement { return d.OverflowYScroll() }).IntoElement()
	})
	f.wheel(50, 50, ScrollDelta{Lines: Point{Y: -0.05 / (100.0 / 3.0)}}, TouchPhaseMoved)
	if diff := abs32(handle.Offset().Y - (-0.05)); diff > 1e-4 {
		t.Fatalf("small-delta offset = %v, want (0,-0.05) instantly", handle.Offset())
	}
	if n := f.window.SimulateNextFrame(f.app); n != 0 {
		t.Fatalf("pending frames after a small delta = %d, want 0", n)
	}
}
