package gpui

// The smooth-scroll engine (browser-core ticket 45, spec D13): wheel,
// keyboard and programmatic scrolls animate toward their target with
// Chromium's curve by default, and jump instantly under reduced motion
// or when smooth scrolling is off.
//
// The engine is the port of cc::ScrollOffsetAnimationCurve
// (cc/animation/scroll_offset_animation_curve.cc) plus its animator:
//
//   - the curve is one cubic bezier — ease-in-out (0.42, 0, 0.58, 1,
//     scroll_offset_animation_curve.cc:28-34) for wheel and keyboard
//     scrolls, and the tweaked programmatic bezier (0.4, 0, 0, 1 with a
//     1500ms cap, cc/base/features.cc:258-266's
//     kProgrammaticScrollAnimationOverride defaults on every platform
//     but Android) for programmatic ones;
//   - the duration follows the source: kConstant (9/60s = 150ms) for the
//     keyboard, the inverse-delta ramp for the wheel (12/60s at 120px
//     down to 6/60s at 480px, clamped to [100ms, 200ms],
//     scroll_offset_animation_curve.cc:44-54, 166-170) and sqrt(|delta|)/60
//     capped at 1500ms for programmatic scrolls (:158-163);
//   - a new input during the animation retargets it (UpdateTarget,
//     :337-396): the new segment starts at the CURRENT animated
//     position, its duration is the source rule bounded by the current
//     velocity ((new delta / velocity) × 2.5, :81-99), and the new
//     bezier's first control point is scaled to preserve the velocity
//     (EaseInOutWithInitialSlope, :70-82);
//   - the wheel path takes Chromium's shapes: the create path animates
//     to clamp(current + delta) (LayerTreeHostImpl::ScrollAnimationCreate,
//     layer_tree_host_impl.cc:4684-4722, deltas under 0.1px scroll
//     instantly), the update path retargets to clamp(target + delta)
//     (ScrollOffsetAnimationImpl::ScrollAnimationUpdateTarget,
//     scroll_offset_animations_impl.cc:118-135), and a precise
//     (pixel/touchpad) delta never animates — it cancels the running
//     animation and scrolls instantly (InputHandler::ScrollBegin's
//     precise arm, input_handler.cc:149-161, and ShouldAnimateScroll,
//     :2484-2492);
//   - the keyboard path retargets the same way from blink
//     (ScrollAnimator::UserScroll computes the clamped target,
//     scroll_animator.cc:88-140, and UpdateCompositorAnimations calls
//     UpdateTarget, :337-341);
//   - an anchoring or instant-programmatic offset write during an
//     animation SHIFTS the running curve instead of cancelling it
//     (ApplyAdjustment, scroll_offset_animation_curve.cc:300-305;
//     blink's "adjust (but not cancel)" rule, scrollable_area.cc:
//     567-570, crbug.com/1264266);
//   - smooth scrolling is a pref on by default that reduced motion
//     turns off entirely: the browser side gates the renderer pref on
//     the flag pair and the system's rich-animation setting
//     (content/browser/web_contents/web_contents_impl.cc:4042-4046),
//     which on Windows is SPI_GETCLIENTAREAANIMATION — off means no
//     client-area animation, i.e. reduced motion
//     (ui/gfx/animation/animation_win.cc:17-44), and every consumer
//     honours it: the blink user animator (scroll_animator.cc:99-103)
//     and the programmatic one (scrollable_area.cc:519-521,
//     behavior kSmooth && ScrollAnimatorEnabled).
//
// The port keeps one animator per scroller, next to the shared scroll
// offset cell (Chromium has one ScrollAnimator per ScrollableArea; the
// wheel latch's identity is the same cell). The animator drives itself
// through the window's next-frame callbacks — the tick samples the
// curve at the application's clock (App.Now: the TestApp virtual clock,
// monotonic wall time in real applications), writes the shared offset
// cell, notifies the view that rendered the scroller, and requests the
// next frame only while the animation runs (the ticket-44 rule). The
// millisecond scheduler clock makes the Tick sampling deterministic in
// tests: one Wait-style clock advance plus one frame delivery samples
// the curve at exactly that instant.
//
// Deviations, both noted in the ticket's Answer:
//
//   - Chromium's compositor-side wheel animations begin one frame in
//     (animation_start_offset, layer_tree_host_impl.cc:4710-4714); the
//     port's first sample is the next delivered frame, which is the
//     same shape without the explicit offset.
//   - Chromium splits the animator into blink (user/programmatic) and
//     cc (compositor) halves with a takeover handoff; the port runs
//     everything on the application's frame callbacks.

import (
	"math"
	"time"
)

// smoothScrollSource is one scroll animation's input source (cc's
// ScrollType, bounded to the port's three: the autoscroll linear curve
// and the find-in-page kUser scrolls are not ported).
type smoothScrollSource uint8

const (
	// smoothScrollWheel is the mouse-wheel source (kMouseWheel): the
	// inverse-delta duration and the ease-in-out bezier.
	smoothScrollWheel smoothScrollSource = iota
	// smoothScrollKeyboard is the keyboard source (kKeyboard): the
	// constant duration and the ease-in-out bezier.
	smoothScrollKeyboard
	// smoothScrollProgrammatic is the programmatic source (kProgrammatic):
	// the delta-based duration and the tweaked bezier.
	smoothScrollProgrammatic
)

// The curve constants (scroll_offset_animation_curve.cc:17-64).
const (
	// smoothScrollConstantSixtieths is kConstantDuration (9.0), the
	// keyboard duration in sixtieths of a second: 150ms.
	smoothScrollConstantSixtieths = 9.0
	// smoothScrollDurationDivisor is kDurationDivisor (60.0): durations
	// are computed in sixtieths of a second and divided by it.
	smoothScrollDurationDivisor = 60.0
	// The inverse-delta ramp (kInverseDeltaRampStartPx/EndPx and
	// Min/MaxDuration, :44-47): the wheel duration falls from
	// 12/60s at a 120px delta to 6/60s at 480px.
	smoothScrollInverseRampStartPx  = 120.0
	smoothScrollInverseRampEndPx    = 480.0
	smoothScrollInverseMinSixtieths = 6.0
	smoothScrollInverseMaxSixtieths = 12.0
	// smoothScrollEpsilon is kEpsilon (0.01): the retarget's
	// same-target and at-target threshold, in pixels.
	smoothScrollEpsilon = 0.01
	// smoothScrollCreateEpsilon is LayerTreeHostImpl::ScrollAnimation-
	// Create's kEpsilon (0.1f, layer_tree_host_impl.cc:4689): deltas at
	// or below it scroll instantly instead of animating.
	smoothScrollCreateEpsilon = 0.1
	// smoothScrollMaxProgrammatic is features.cc's
	// kMaxAnimationDuration default for the programmatic curve on
	// non-Android platforms (1500ms; Android's is 700ms).
	smoothScrollMaxProgrammatic = 1500 * time.Millisecond
	// smoothScrollSlopeClamp is EaseInOutWithInitialSlope's clamp
	// (±1000, scroll_offset_animation_curve.cc:74-76).
	smoothScrollSlopeClamp = 1000.0
)

// smoothScrollBezier is the curve's cubic bezier control points.
type smoothScrollBezier struct{ x1, y1, x2, y2 float32 }

// The two curves: Chrome's ease-in-out (0.42, 0, 0.58, 1,
// scroll_offset_animation_curve.cc:28-34) for wheel and keyboard
// scrolls, and the tweaked programmatic bezier (0.4, 0, 0, 1) for
// programmatic ones — features.cc:258-266's
// kProgrammaticScrollAnimationOverride defaults on every platform but
// Android.
var (
	smoothScrollEaseInOut          = smoothScrollBezier{0.42, 0, 0.58, 1}
	smoothScrollProgrammaticBezier = smoothScrollBezier{0.4, 0, 0, 1}
)

// sample eases progress x through the bezier (the y at the curve
// parameter whose x is the progress).
func (b smoothScrollBezier) sample(x float32) float32 {
	sample, _ := cubicBezierParts(b.x1, b.y1, b.x2, b.y2)
	return sample(x)
}

// slopeAt is the bezier's dy/dx at progress x
// (CubicBezierTimingFunction::Velocity, timing_function.cc:67-69).
func (b smoothScrollBezier) slopeAt(x float32) float32 {
	_, slope := cubicBezierParts(b.x1, b.y1, b.x2, b.y2)
	return slope(x)
}

// withInitialSlope is EaseInOutWithInitialSlope
// (scroll_offset_animation_curve.cc:70-82): the first control point's y
// scales by the slope (y1 = x1 × slope, clamped to ±1000), preserving
// the velocity of the animation the retarget interrupts.
func (b smoothScrollBezier) withInitialSlope(slope float64) smoothScrollBezier {
	if slope > smoothScrollSlopeClamp {
		slope = smoothScrollSlopeClamp
	}
	if slope < -smoothScrollSlopeClamp {
		slope = -smoothScrollSlopeClamp
	}
	return smoothScrollBezier{x1: b.x1, y1: float32(float64(b.x1) * slope), x2: b.x2, y2: b.y2}
}

// smoothScroll is one running scroll-offset animation: the port of
// ScrollOffsetAnimationCurve's mutable state. The value at elapsed time
// e is the bezier-eased lerp from initial toward target over the
// segment [retarget, duration] (GetValue, scroll_offset_animation_
// curve.cc:252-266); e ≥ duration is the target.
type smoothScroll struct {
	// source selects the duration rule and the base bezier.
	source smoothScrollSource
	// timing is the segment's bezier (replaced at a retarget).
	timing smoothScrollBezier
	// initial and target are the segment's endpoints.
	initial Point
	target  Point
	// start is the application clock when the animation began.
	start time.Duration
	// retarget is the elapsed time at the last retarget (0 at creation).
	retarget time.Duration
	// duration is the total animation time (start + duration is the end).
	duration time.Duration
}

// smoothScrollSegmentDuration is EaseInOutSegmentDuration
// (scroll_offset_animation_curve.cc:148-178) for the source's duration
// behavior: kConstant (150ms) for the keyboard, the inverse-delta ramp
// for the wheel, and sqrt(|delta|) in sixtieths (capped at the 1500ms
// programmatic maximum) for programmatic scrolls. delayedBy subtracts
// like the original (the port never delays a scroll animation, so it is
// always zero).
func smoothScrollSegmentDuration(source smoothScrollSource, delta Point, delayedBy time.Duration) time.Duration {
	sixtieths := smoothScrollConstantSixtieths
	switch source {
	case smoothScrollWheel:
		slope := (smoothScrollInverseMinSixtieths - smoothScrollInverseMaxSixtieths) /
			(smoothScrollInverseRampEndPx - smoothScrollInverseRampStartPx)
		offset := smoothScrollInverseMaxSixtieths - smoothScrollInverseRampStartPx*slope
		sixtieths = offset + float64(smoothScrollMaxDimensionAbs(delta))*slope
		sixtieths = clampF64(sixtieths, smoothScrollInverseMinSixtieths, smoothScrollInverseMaxSixtieths)
	case smoothScrollProgrammatic:
		sixtieths = mathSqrt(float64(smoothScrollMaxDimensionAbs(delta)))
		if max := smoothScrollMaxProgrammatic.Seconds() * smoothScrollDurationDivisor; sixtieths > max {
			sixtieths = max
		}
	}
	d := time.Duration(sixtieths/smoothScrollDurationDivisor*float64(time.Second)) - delayedBy
	if d < 0 {
		return 0
	}
	return d
}

// valueAt is GetValue (scroll_offset_animation_curve.cc:252-266): the
// eased lerp of the current segment, the target past its end and the
// initial before its start.
func (a *smoothScroll) valueAt(elapsed time.Duration) Point {
	dur := a.duration - a.retarget
	t := elapsed - a.retarget
	if dur <= 0 || t >= dur {
		return a.target
	}
	if t <= 0 {
		return a.initial
	}
	progress := secsF32(t) / secsF32(dur)
	eased := a.timing.sample(progress)
	return Point{
		X: lerpMotion32(a.initial.X, a.target.X, eased),
		Y: lerpMotion32(a.initial.Y, a.target.Y, eased),
	}
}

// velocityAt is CalculateVelocity (scroll_offset_animation_curve.cc:
// 224-240): the timing function's slope scaled by the segment's pixel
// delta over its seconds — pixels per second, signed by the larger
// dimension (MaximumDimension keeps the sign).
func (a *smoothScroll) velocityAt(elapsed time.Duration) float64 {
	dur := a.duration - a.retarget
	if dur <= 0 {
		return 0
	}
	t := elapsed - a.retarget
	if t < 0 {
		t = 0
	}
	if t > dur {
		t = dur
	}
	slope := a.timing.slopeAt(clampMotion32(secsF32(t)/secsF32(dur), 0, 1))
	delta := pointSub(a.target, a.initial)
	dim := smoothScrollMaxDimension(delta)
	return float64(slope) * (float64(dim) / secsF64(dur))
}

// velocityBound is VelocityBasedDurationBound
// (scroll_offset_animation_curve.cc:81-99): the time the new delta
// would take at the present velocity, times 2.5 (the ease-out fudge),
// infinite when the velocity cannot serve the bound.
func (a *smoothScroll) velocityBound(elapsed time.Duration, newDelta Point) time.Duration {
	dim := smoothScrollMaxDimensionAbs(newDelta)
	if dim < smoothScrollEpsilon {
		return 0
	}
	velocity := a.velocityAt(elapsed)
	if absF64(velocity) < smoothScrollEpsilon {
		return time.Duration(math.MaxInt64)
	}
	return smoothScrollVelocityBound((float64(dim) / velocity) * 2.5)
}

// retargetTo is UpdateTarget (scroll_offset_animation_curve.cc:
// 337-396): aim the running animation at newTarget from the current
// animated position. A target within 0.01px of the current one only
// updates it (the ease-in-out duration is inverse to the distance, so a
// same-distance restart would run longer than what is already running);
// a target the animation already sits at stops it; otherwise the new
// segment starts at the current position with the source's duration
// bounded by the velocity bound, and the bezier's initial slope is
// scaled to preserve the velocity. elapsed is the time since the
// animation's start.
func (a *smoothScroll) retargetTo(elapsed time.Duration, newTarget Point) {
	// The same-target no-op (:355-363).
	if smoothScrollMaxDimensionAbs(pointSub(a.target, newTarget)) < smoothScrollEpsilon {
		a.target = newTarget
		return
	}
	current := a.valueAt(elapsed)
	newDelta := pointSub(newTarget, current)
	// Already at the new target: stop animating (:365-370).
	if smoothScrollMaxDimensionAbs(newDelta) < smoothScrollEpsilon {
		a.retarget = elapsed
		a.duration = elapsed
		a.target = newTarget
		return
	}
	// The last segment was of zero duration (:372-379): restart the
	// segment from here.
	oldDuration := a.duration - a.retarget
	if oldDuration <= 0 {
		a.initial = current
		a.target = newTarget
		a.retarget = elapsed
		a.duration = elapsed + smoothScrollSegmentDuration(a.source, newDelta, 0)
		a.timing = smoothScrollCurveFor(a.source)
		return
	}
	newDuration := smoothScrollSegmentDuration(a.source, newDelta, 0)
	if bound := a.velocityBound(elapsed, newDelta); bound < newDuration {
		newDuration = bound
	}
	// A duration at or under kEpsilon seconds stops the animation
	// (:389-393).
	if secsF64(newDuration) < smoothScrollEpsilon {
		a.target = newTarget
		a.duration = elapsed
		a.retarget = elapsed
		return
	}
	// Preserve the velocity: the new curve's initial slope (:395-396).
	velocity := a.velocityAt(elapsed)
	slope := velocity * (secsF64(newDuration) / float64(smoothScrollMaxDimension(newDelta)))
	a.timing = smoothScrollCurveFor(a.source).withInitialSlope(slope)
	a.initial = current
	a.target = newTarget
	a.duration = elapsed + newDuration
	a.retarget = elapsed
}

// adjust is ApplyAdjustment (scroll_offset_animation_curve.cc:
// 300-305): shift the whole curve by v without touching its shape or
// timing — the scroll-anchoring and instant-programmatic-scroll rule
// (blink's "adjust (but not cancel)", scrollable_area.cc:567-570).
func (a *smoothScroll) adjust(v Point) {
	a.initial.X += v.X
	a.initial.Y += v.Y
	a.target.X += v.X
	a.target.Y += v.Y
}

// smoothScrollCurveFor is GetEasingFunction
// (scroll_offset_animation_curve.cc:322-336): ease-in-out for wheel and
// keyboard scrolls, the tweaked bezier for programmatic ones.
func smoothScrollCurveFor(source smoothScrollSource) smoothScrollBezier {
	if source == smoothScrollProgrammatic {
		return smoothScrollProgrammaticBezier
	}
	return smoothScrollEaseInOut
}

// smoothScrollAnimator is one scroller's persistent smooth-scroll
// state (the port's ScrollAnimator): the running animation, and the
// flag that keeps exactly one tick scheduled per scroller. It lives
// next to the shared scroll offset cell — the handle's state or the
// element's — so it survives the frame rebuilds like the offset does.
type smoothScrollAnimator struct {
	// anim is the running animation, nil at rest.
	anim *smoothScroll
	// pending reports that a next-frame tick is scheduled.
	pending bool
}

// startAt is the convention-agnostic create-or-retarget core
// (start's body without the offset-cell clamp): current is the
// scroller's present position in the caller's convention (the element
// cells' negative-down; the list's negated pixel scroll top), target is
// the already-clamped animation target. A zero-distance target
// reports false without writing (Chromium's ScrollOffsetIsNoop arm).
func (an *smoothScrollAnimator) startAt(current Point, target Point, source smoothScrollSource, now time.Duration) bool {
	if anim := an.anim; anim != nil && now-anim.start < anim.duration {
		anim.retargetTo(now-anim.start, target)
		return true
	}
	delta := pointSub(target, current)
	if smoothScrollMaxDimensionAbs(delta) < smoothScrollEpsilon {
		an.anim = nil
		return false
	}
	an.anim = &smoothScroll{
		source:  source,
		timing:  smoothScrollCurveFor(source),
		initial: current,
		target:  target,
		start:   now,
	}
	an.anim.duration = smoothScrollSegmentDuration(source, delta, 0)
	return true
}

// start creates or retargets the animation toward target
// (smoothScrollAnimated): a running animation retargets from its
// current animated position; a finished or absent one starts fresh from
// the scroller's current clamped offset. A zero-distance target is a
// plain instant write (Chromium's ScrollOffsetIsNoop arm,
// scrollable_area.cc:325-328) — an animation that would not move.
func (an *smoothScrollAnimator) start(offset *Point, maxOffset Point, target Point, source smoothScrollSource, now time.Duration) bool {
	current := *offset
	current.X = clampF32(current.X, -maxOffset.X, 0)
	current.Y = clampF32(current.Y, -maxOffset.Y, 0)
	if an.startAt(current, target, source, now) {
		return true
	}
	*offset = target
	return false
}

// writeInstant is the instant-offset write under a running animation:
// Chromium adjusts (but does not cancel) the running curve by the write's
// delta (scrollable_area.cc:567-570, crbug.com/1264266).
func (an *smoothScrollAnimator) writeInstant(offset *Point, value Point) {
	old := *offset
	*offset = value
	if an.anim != nil {
		an.anim.adjust(pointSub(value, old))
	}
}

// requestSmoothScrollTick schedules the animator's next-frame tick if
// none is scheduled: the tick closure holds the animator (a stable
// pointer next to the offset cell), the shared offset cell and the
// view notifier captured when the input ran.
func requestSmoothScrollTick(w *Window, an *smoothScrollAnimator, offset *Point, notify func(*Window, *App)) {
	if w == nil || an == nil || an.pending {
		return
	}
	an.pending = true
	w.OnNextFrame(func(w *Window, app *App) {
		an.pending = false
		smoothScrollTick(w, app, an, offset, notify)
	})
}

// smoothScrollTick is one frame of the animator: sample the curve at
// the application's clock, write the shared offset cell and notify the
// view, then request the next frame only while the animation runs. A
// finished animation leaves the offset at its target and clears the
// animator; the reduced-motion or disabled gate adopts the target at
// once (D13, the ticket-74 rule the motion engine follows).
func smoothScrollTick(w *Window, app *App, an *smoothScrollAnimator, offset *Point, notify func(*Window, *App)) {
	if an == nil || an.anim == nil {
		return
	}
	anim := an.anim
	if app == nil || app.MotionReduced() || !app.smoothScrolling {
		*offset = anim.target
		an.anim = nil
		if notify != nil {
			notify(w, app)
		}
		return
	}
	elapsed := app.Now() - anim.start
	*offset = anim.valueAt(elapsed)
	if notify != nil {
		notify(w, app)
	}
	if elapsed >= anim.duration {
		an.anim = nil
		return
	}
	requestSmoothScrollTick(w, an, offset, notify)
}

// smoothScrollAnimated reports whether the application scrolls
// smoothly right now: the pref (on by default, D13) and the one
// reduced-motion source (App.MotionReduced, the ticket-74 gate).
func (a *App) smoothScrollAnimated() bool {
	return a != nil && a.smoothScrolling && !a.MotionReduced()
}

// animateSmoothScroll starts (or retargets) the scroller's animation
// toward target and requests the first frame: the raw form both the
// element's scroll slice (elementScroll.animateSmoothScroll) and the
// handle's programmatic ScrollTo use. The target must already be
// clamped into the scroller's range (the callers clamp, like
// Chromium's ClampScrollOffsetToLimits).
func animateSmoothScroll(offset *Point, maxOffset Point, smooth **smoothScrollAnimator, target Point,
	source smoothScrollSource, notify func(*Window, *App), w *Window, app *App) {
	if offset == nil || smooth == nil || w == nil || app == nil {
		return
	}
	an := *smooth
	if an == nil {
		an = &smoothScrollAnimator{}
		*smooth = an
	}
	if !an.start(offset, maxOffset, target, source, app.Now()) {
		// The zero-distance target: the write already happened, a plain
		// notify refreshes the view.
		if notify != nil {
			notify(w, app)
		}
		return
	}
	requestSmoothScrollTick(w, an, offset, notify)
}

// animateSmoothScroll starts (or retargets) s's animation toward target
// (see the raw form).
func (s *elementScroll) animateSmoothScroll(target Point, source smoothScrollSource, notify func(*Window, *App), w *Window, app *App) {
	if s == nil || s.offset == nil || s.smooth == nil {
		return
	}
	animateSmoothScroll(s.offset, s.maxOffset, s.smooth, target, source, notify, w, app)
}

// animator resolves the scroller's animator, nil when it scrolls
// without one (the handle's or the element state's cell).
func (s *elementScroll) animator() *smoothScrollAnimator {
	if s == nil || s.smooth == nil {
		return nil
	}
	return *s.smooth
}

// smoothScrollAdjust shifts a running animation by v (the anchoring and
// instant-write rule): a no-op without one.
func (s *elementScroll) smoothScrollAdjust(v Point) {
	if an := s.animator(); an != nil && an.anim != nil {
		an.anim.adjust(v)
	}
}

func smoothScrollVelocityBound(bound float64) time.Duration {
	if bound < 0 || bound*float64(time.Second) > float64(math.MaxInt64) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(bound * float64(time.Second))
}

// smoothScrollCancel drops a running animation without moving the
// offset (the precise-delta cancellation Chromium runs at ScrollBegin,
// input_handler.cc:149-161).
func (s *elementScroll) smoothScrollCancel() {
	if an := s.animator(); an != nil {
		an.anim = nil
	}
}

// smoothScrollMaxDimension is MaximumDimension
// (scroll_offset_animation_curve.cc:66-68): the larger dimension BY
// ABSOLUTE VALUE, keeping its sign.
func smoothScrollMaxDimension(v Point) float32 {
	if abs32(v.X) > abs32(v.Y) {
		return v.X
	}
	return v.Y
}

// smoothScrollMaxDimensionAbs is |MaximumDimension|.
func smoothScrollMaxDimensionAbs(v Point) float32 {
	return abs32(smoothScrollMaxDimension(v))
}

// clampF64, absF64, secsF64 and mathSqrt are the float64 helpers the
// curve's double math uses (Chromium computes durations and velocities
// in doubles).
func clampF64(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func absF64(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func secsF64(d time.Duration) float64 {
	return float64(d) / float64(time.Second)
}

func mathSqrt(v float64) float64 { return math.Sqrt(v) }
