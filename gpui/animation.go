package gpui

// This file is Base ticket 08's animation element: the pinned
// crates/gpui/src/elements/animation.rs (Animation, AnimationExt's
// with_animation / with_animations, AnimationElement and the easing
// functions) over the duration half of crates/gpui/src/motion.rs (the
// Motion<DurationDescription> sampler Animation is built on). GPUI-CE
// 254b5dbd; A: = elements/animation.rs, M: = motion.rs.
//
// Bounded scope, recorded:
//
//   - The spring half (AnimationExt::with_spring, SpringAnimationElement,
//     Motion<SpringDescription>, spring.rs) is not ported.
//   - Go cannot add methods to arbitrary element types, so the pinned
//     extension-trait methods are the free functions WithAnimation /
//     WithAnimations over any IntoElement; the pinned ParentElement
//     forwarding is MapElement (add children before wrapping, or map).
//   - The pinned Direction / Iterations / Playback names are prefixed
//     (MotionDirection, MotionIterations, MotionPlayback): the bare names
//     are too generic for the package namespace.
//   - The synced-animation epoch (the pinned App::synced_animation_epoch,
//     app.rs:851, captured from the executor clock at App creation) is the
//     application clock's origin: App.Now() counts from the application's
//     creation, so the epoch is zero.
//   - Durations are int64 nanoseconds (time.Duration) where the pin uses
//     u128 nanosecond arithmetic: ~292 years of elapsed time fit.

import (
	"math"
	"time"
)

// ---------------------------------------------------------------------------
// Motion (motion.rs, the duration description)
// ---------------------------------------------------------------------------

// Progress is normalized animation progress: normalized before easing,
// possibly overshooting afterwards (M:21-57 Progress).
type Progress float32

const (
	// ProgressStart is the beginning of an animation (M:27).
	ProgressStart Progress = 0
	// ProgressEnd is the end of an animation (M:30).
	ProgressEnd Progress = 1
)

// ClampedProgress returns progress clamped to the normalized range
// (M:33-36 Progress::clamped; NaN panics like the pinned assert).
func ClampedProgress(value float32) Progress {
	if math.IsNaN(float64(value)) {
		panic("gpui: progress must not be NaN")
	}
	return Progress(min32(max32(value, 0), 1))
}

// Get returns the underlying progress value (M:39).
func (p Progress) Get() float32 { return float32(p) }

// IsAtEnd reports whether presentation is exactly at one (M:45).
func (p Progress) IsAtEnd() bool { return p == ProgressEnd }

// easedProgress is M:53-56 Progress::eased (finite assert).
func easedProgress(value float32) Progress {
	if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
		panic("gpui: easing must return a finite value")
	}
	return Progress(value)
}

// reversed is M:49-51.
func (p Progress) reversed() Progress { return easedProgress(1 - float32(p)) }

// MotionPass is the duration and easing curve of one directional motion
// pass (M:103-151 MotionPass). The zero easing is linear.
type MotionPass struct {
	duration time.Duration
	easing   func(float32) float32
}

// NewMotionPass creates a linear pass with the duration (M:116).
func NewMotionPass(duration time.Duration) MotionPass {
	return MotionPass{duration: duration}
}

// WithEasing configures the pass's easing function (M:124).
func (p MotionPass) WithEasing(easing func(float32) float32) MotionPass {
	p.easing = easing
	return p
}

// Duration returns the pass's duration (M:130).
func (p MotionPass) Duration() time.Duration { return p.duration }

// ease is Easing::sample (M:92-94; the default easing is linear, M:97-101).
func (p MotionPass) ease(progress Progress) Progress {
	if p.easing == nil {
		return easedProgress(float32(progress))
	}
	return easedProgress(p.easing(float32(progress)))
}

// localTime is M:143-150 MotionPass::local_time.
func (p MotionPass) localTime(elapsedNanos int64) Progress {
	linear := float64(elapsedNanos) / float64(p.duration.Nanoseconds())
	return ClampedProgress(float32(linear))
}

// MotionIterations is the number of passes in a playback run
// (M:159-173 Iterations): Forever, or a nonzero Count (the default 1).
type MotionIterations struct {
	// Forever continues until the owner stops playback.
	Forever bool
	// Count is the finite number of passes, including the first.
	Count uint32
}

// MotionDirection is the configured direction of successive iterations
// (M:175-187 Direction).
type MotionDirection uint8

const (
	// MotionForward travels every iteration from origin to target.
	MotionForward MotionDirection = iota
	// MotionReverse travels every iteration from target to origin.
	MotionReverse
	// MotionAlternate begins forward, then alternates.
	MotionAlternate
	// MotionAlternateReverse begins in reverse, then alternates.
	MotionAlternateReverse
)

// PassDirection is the direction of the pass selected for a sample
// (M:189-196).
type PassDirection uint8

const (
	// PassForward is origin to target.
	PassForward PassDirection = iota
	// PassReverse is target to origin.
	PassReverse
)

// pass is M:248-268 Direction::pass.
func (d MotionDirection) pass(iteration int64) PassDirection {
	switch d {
	case MotionReverse:
		return PassReverse
	case MotionAlternate:
		if iteration%2 == 0 {
			return PassForward
		}
		return PassReverse
	case MotionAlternateReverse:
		if iteration%2 == 0 {
			return PassReverse
		}
		return PassForward
	}
	return PassForward
}

// MotionPhase is the lifecycle state of a sampled run (M:234-246).
type MotionPhase uint8

const (
	// MotionDelayed: the initial delay has not elapsed.
	MotionDelayed MotionPhase = iota
	// MotionPlaying: playback is in progress.
	MotionPlaying
	// MotionCompleted: every finite iteration has elapsed.
	MotionCompleted
	// MotionInactive: infinite playback whose passes are all zero length.
	MotionInactive
)

// MotionSample is the result of evaluating motion at a point in time
// (M:271-308).
type MotionSample struct {
	// Progress is the eased presentation progress.
	Progress Progress
	// LocalTime is the uneased normalized time within the pass.
	LocalTime Progress
	// Iteration is the zero-based iteration index.
	Iteration int64
	// Direction is the resolved pass direction.
	Direction PassDirection
	// Phase is the whole-run lifecycle state.
	Phase MotionPhase
	// IsActive reports whether another sample may produce a different
	// value (Delayed or Playing).
	IsActive bool
}

// IsComplete reports whether the entire finite run has completed (M:300).
func (s MotionSample) IsComplete() bool { return s.Phase == MotionCompleted }

// Motion is a deterministic, time-sampled description of duration-based
// motion (M:310-345 DurationDescription + M:359-363 Motion): one forward
// pass, an optional reverse pass, an initial delay and the playback
// policy. Build one with NewMotion.
type Motion struct {
	forward    MotionPass
	reverse    *MotionPass
	delay      time.Duration
	iterations MotionIterations
	direction  MotionDirection
}

// NewMotion creates one linear motion pass with the duration (M:381-390;
// also the pinned From<Duration>).
func NewMotion(duration time.Duration) Motion {
	return Motion{forward: NewMotionPass(duration), iterations: MotionIterations{Count: 1}}
}

// WithEasing configures the forward pass's easing (M:396).
func (m Motion) WithEasing(easing func(float32) float32) Motion {
	m.forward.easing = easing
	return m
}

// WithForwardPass replaces the forward pass (M:424).
func (m Motion) WithForwardPass(pass MotionPass) Motion {
	m.forward = pass
	return m
}

// WithReversePass configures the pass used for backward legs (M:439).
func (m Motion) WithReversePass(pass MotionPass) Motion {
	p := pass
	m.reverse = &p
	return m
}

// WithDelay delays the start of playback once per run (M:445).
func (m Motion) WithDelay(delay time.Duration) Motion {
	m.delay = delay
	return m
}

// Iterations sets the total number of iterations, including the first
// (M:461; zero panics like the pinned expect).
func (m Motion) Iterations(iterations uint32) Motion {
	if iterations == 0 {
		panic("gpui: motion iterations must be at least 1")
	}
	m.iterations = MotionIterations{Count: iterations}
	return m
}

// RepeatForever repeats the motion indefinitely (M:472).
func (m Motion) RepeatForever() Motion {
	m.iterations = MotionIterations{Forever: true}
	return m
}

// Alternate alternates forward and backward iterations (M:482).
func (m Motion) Alternate() Motion {
	m.direction = MotionAlternate
	return m
}

// Direction configures the direction of playback iterations (M:488).
func (m Motion) Direction(direction MotionDirection) Motion {
	m.direction = direction
	return m
}

// Delay returns the initial delay (M:510).
func (m Motion) Delay() time.Duration { return m.delay }

// reversePassOrForward is M:519-521.
func (m Motion) reversePassOrForward() MotionPass {
	if m.reverse != nil {
		return *m.reverse
	}
	return m.forward
}

// pass is M:554-559.
func (m Motion) pass(direction PassDirection) MotionPass {
	if direction == PassReverse {
		return m.reversePassOrForward()
	}
	return m.forward
}

// finiteDurationNanos is M:523-537 (false on overflow).
func (m Motion) finiteDurationNanos(iterations uint32) (int64, bool) {
	n := int64(iterations)
	forward := m.forward.duration.Nanoseconds()
	reverse := m.reversePassOrForward().duration.Nanoseconds()
	mul := func(a, b int64) (int64, bool) {
		if a == 0 || b == 0 {
			return 0, true
		}
		if a > math.MaxInt64/b {
			return 0, false
		}
		return a * b, true
	}
	add := func(a, b int64) (int64, bool) {
		if a > math.MaxInt64-b {
			return 0, false
		}
		return a + b, true
	}
	switch m.direction {
	case MotionReverse:
		return mul(reverse, n)
	case MotionAlternate, MotionAlternateReverse:
		cycle, ok := add(forward, reverse)
		if !ok {
			return 0, false
		}
		pairs, ok := mul(cycle, n/2)
		if !ok {
			return 0, false
		}
		last := forward
		if m.direction == MotionAlternateReverse {
			last = reverse
		}
		tail, ok := mul(last, n%2)
		if !ok {
			return 0, false
		}
		return add(pairs, tail)
	}
	return mul(forward, n)
}

// activePass is M:561-599.
func (m Motion) activePass(elapsedNanos int64) (MotionPass, int64, PassDirection, int64, bool) {
	switch m.direction {
	case MotionForward, MotionReverse:
		direction := m.direction.pass(0)
		pass := m.pass(direction)
		duration := pass.duration.Nanoseconds()
		if duration == 0 {
			return MotionPass{}, 0, 0, 0, false
		}
		return pass, elapsedNanos % duration, direction, elapsedNanos / duration, true
	}
	cycle := m.forward.duration.Nanoseconds() + m.reversePassOrForward().duration.Nanoseconds()
	if cycle == 0 {
		return MotionPass{}, 0, 0, 0, false
	}
	first := m.direction.pass(0)
	firstDuration := m.pass(first).duration.Nanoseconds()
	cycleIndex := elapsedNanos / cycle
	inCycle := elapsedNanos % cycle
	iteration := cycleIndex * 2
	if inCycle < firstDuration {
		return m.pass(first), inCycle, first, iteration, true
	}
	second := m.direction.pass(1)
	return m.pass(second), inCycle - firstDuration, second, iteration + 1, true
}

// startProgress is M:601-606.
func (m Motion) startProgress() Progress {
	if m.direction.pass(0) == PassReverse {
		return ProgressEnd
	}
	return ProgressStart
}

// presentation is M:633-645.
func (m Motion) presentation(pass MotionPass, localTime Progress, direction PassDirection) Progress {
	eased := pass.ease(localTime)
	if direction == PassReverse {
		return eased.reversed()
	}
	return eased
}

// terminalSample is M:608-631.
func (m Motion) terminalSample(direction PassDirection, iteration int64, phase MotionPhase, zeroDuration bool) MotionSample {
	var progress Progress
	if zeroDuration {
		progress = ProgressStart
		if direction == PassForward {
			progress = ProgressEnd
		}
	} else {
		progress = m.presentation(m.pass(direction), ProgressEnd, direction)
	}
	return MotionSample{Progress: progress, LocalTime: ProgressEnd, Iteration: iteration, Direction: direction, Phase: phase}
}

// Sample evaluates the motion after the elapsed time (M:652-721): pass
// selection, iteration parity and completion use integer nanoseconds;
// only the fraction within the selected pass is eased.
func (m Motion) Sample(elapsed time.Duration) MotionSample {
	if elapsed < m.delay {
		return MotionSample{Progress: m.startProgress(), LocalTime: ProgressStart, Direction: m.direction.pass(0), Phase: MotionDelayed, IsActive: true}
	}
	elapsedNanos := (elapsed - m.delay).Nanoseconds()
	if !m.iterations.Forever {
		total, ok := m.finiteDurationNanos(m.iterations.Count)
		if !ok {
			panic("gpui: finite Motion extent overflows")
		}
		if elapsedNanos >= total {
			last := int64(m.iterations.Count - 1)
			return m.terminalSample(m.direction.pass(last), last, MotionCompleted, total == 0)
		}
	}
	pass, inPass, direction, iteration, ok := m.activePass(elapsedNanos)
	if !ok {
		return MotionSample{Progress: m.startProgress(), LocalTime: ProgressStart, Direction: m.direction.pass(0), Phase: MotionInactive}
	}
	// A restarting pass presents its endpoint at the exact boundary
	// while the run itself remains active (M:693-709).
	if (m.direction == MotionForward || m.direction == MotionReverse) && elapsedNanos > 0 && inPass == 0 {
		return MotionSample{Progress: m.presentation(pass, ProgressEnd, direction), LocalTime: ProgressEnd, Iteration: iteration - 1, Direction: direction, Phase: MotionPlaying, IsActive: true}
	}
	local := pass.localTime(inPass)
	return MotionSample{Progress: m.presentation(pass, local, direction), LocalTime: local, Iteration: iteration, Direction: direction, Phase: MotionPlaying, IsActive: true}
}

// restingProgress is M:731-742: the final iteration's endpoint for a
// finite run, the start for an infinite one.
func (m Motion) restingProgress() Progress {
	if m.iterations.Forever {
		return m.startProgress()
	}
	if m.direction.pass(int64(m.iterations.Count-1)) == PassReverse {
		return ProgressStart
	}
	return ProgressEnd
}

// ---------------------------------------------------------------------------
// Animation (elements/animation.rs)
// ---------------------------------------------------------------------------

// Animation is an animation that can be applied to an element
// (A:13-23).
type Animation struct {
	// Motion is the timing, repetition and easing of the animation.
	Motion Motion
	// Synced derives the phase from a clock shared by the whole App (see
	// RepeatSynced).
	Synced bool
	// MaxFPS limits re-renders per second when set (see WithMaxFPS).
	MaxFPS *float32
}

// NewAnimation creates an animation with the given motion (A:29-35); a
// duration creates one linear motion pass (the pinned
// `impl Into<Motion>` over Duration).
func NewAnimation[M time.Duration | Motion](motion M) Animation {
	switch v := any(motion).(type) {
	case time.Duration:
		return Animation{Motion: NewMotion(v)}
	case Motion:
		return Animation{Motion: v}
	}
	panic("gpui: unreachable NewAnimation motion")
}

// Repeat sets the animation to loop when it finishes (A:38).
func (a Animation) Repeat() Animation {
	a.Motion = a.Motion.RepeatForever()
	return a
}

// RepeatSynced loops the animation phase-locked to the clock shared by
// the whole App (A:44).
func (a Animation) RepeatSynced() Animation {
	a.Motion = a.Motion.RepeatForever()
	a.Synced = true
	return a
}

// WithEasing sets the easing without clamping the output (A:51).
func (a Animation) WithEasing(easing func(float32) float32) Animation {
	a.Motion = a.Motion.WithEasing(easing)
	return a
}

// WithMaxFPS limits re-renders to maxFPS by scheduling a timer between
// frames (A:58); non-finite and non-positive values are ignored.
func (a Animation) WithMaxFPS(maxFPS float32) Animation {
	v := maxFPS
	a.MaxFPS = &v
	return a
}

// AnimationElement applies an animation to another element (A:149-155).
// Build one with WithAnimation or WithAnimations.
type AnimationElement[E IntoElement] struct {
	id         ElementID
	element    E
	hasElement bool
	animations []Animation
	animator   func(E, int, float32) E
}

// WithAnimation renders the element with an animation (A:73-88
// AnimationExt::with_animation): every frame the animator receives a
// fresh element and the eased progress.
func WithAnimation[E IntoElement, I IntoElementID](element E, id I, animation Animation, animator func(E, float32) E) *AnimationElement[E] {
	if animator == nil {
		panic("gpui: WithAnimation requires an animator")
	}
	return &AnimationElement[E]{
		id:         ElementIDOf(id),
		element:    element,
		hasElement: true,
		animations: []Animation{animation},
		animator:   func(e E, _ int, delta float32) E { return animator(e, delta) },
	}
}

// WithAnimations renders the element with a chain of animations
// (A:91-108 with_animations): the animator receives the active
// animation's index and its progress.
func WithAnimations[E IntoElement, I IntoElementID](element E, id I, animations []Animation, animator func(E, int, float32) E) *AnimationElement[E] {
	if len(animations) == 0 {
		panic("gpui: animations must not be empty")
	}
	if animator == nil {
		panic("gpui: WithAnimations requires an animator")
	}
	return &AnimationElement[E]{
		id:         ElementIDOf(id),
		element:    element,
		hasElement: true,
		animations: append([]Animation(nil), animations...),
		animator:   animator,
	}
}

// MapElement applies f to the element being animated (A:205-211
// map_element).
func (a *AnimationElement[E]) MapElement(f func(E) E) *AnimationElement[E] {
	if a.hasElement {
		a.element = f(a.element)
	}
	return a
}

// IntoElement implements IntoElement (A:214-220).
func (a *AnimationElement[E]) IntoElement() AnyElement {
	return CustomElement[AnyElement, struct{}](a)
}

// animationState is A:222-228 AnimationState.
type animationState struct {
	start               time.Duration
	animationIx         int
	delayedFramePending *bool
}

// ID implements Element: the animation element is identified (A:377).
func (a *AnimationElement[E]) ID() (ElementID, bool) { return a.id, true }

// SourceLocation implements Element (A:381).
func (a *AnimationElement[E]) SourceLocation() *SourceLocation { return nil }

// RequestLayout implements Element (A:385-452): inside the element's
// retained state, the active animation is sampled at the application
// clock (the shared epoch for synced animations); a finished animation
// advances the chain; reduce motion renders the last animation's resting
// progress and schedules nothing. The animator builds this frame's
// element, and an unfinished animation requests the next animation frame
// — or, under a max FPS, one timer-driven re-render of the current view.
func (a *AnimationElement[E]) RequestLayout(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App) (LayoutID, AnyElement) {
	type result struct {
		id      LayoutID
		element AnyElement
	}
	r := WithElementState(w, global, func(slot *animationState, w *Window) (result, animationState) {
		now := app.Now()
		var state animationState
		if slot != nil {
			state = *slot
		} else {
			state = animationState{start: now, delayedFramePending: new(bool)}
		}
		var animationIx int
		var delta float32
		var done bool
		if app.ReduceMotion() {
			animationIx = len(a.animations) - 1
			delta = a.animations[animationIx].Motion.restingProgress().Get()
			done = true
		} else {
			animationIx = state.animationIx
			animation := a.animations[animationIx]
			var elapsed time.Duration
			if animation.Synced {
				// now - cx.synced_animation_epoch (the epoch is the clock
				// origin; see the file comment).
				elapsed = now
			} else {
				elapsed = now - state.start
			}
			sample := animation.Motion.Sample(elapsed)
			done = !sample.IsActive
			if done && animationIx < len(a.animations)-1 {
				state.start = now
				state.animationIx++
				done = false
			}
			delta = sample.Progress.Get()
		}

		if !a.hasElement {
			panic("gpui: AnimationElement request_layout should only be called once")
		}
		element := a.animator(a.element, animationIx, delta).IntoElement()
		var zero E
		a.element, a.hasElement = zero, false

		if !done {
			maxFPS := a.animations[animationIx].MaxFPS
			if maxFPS != nil && !math.IsNaN(float64(*maxFPS)) && !math.IsInf(float64(*maxFPS), 0) && *maxFPS > 0 {
				if !*state.delayedFramePending {
					*state.delayedFramePending = true
					pending := state.delayedFramePending
					view, hasView := currentViewOf(w)
					interval := time.Duration(float64(time.Second) / float64(*maxFPS))
					app.SpawnDelivering(w.Scope(), func(run *TaskRun) struct{} {
						run.Sleep(interval)
						return struct{}{}
					}, func(_ struct{}, acx *App) {
						*pending = false
						if hasView {
							acx.Notify(view)
						} else {
							w.requestRefresh()
						}
					})
				}
			} else {
				RequestAnimationFrame(w)
			}
		}
		return result{id: element.RequestLayout(w, app), element: element}, state
	})
	return r.id, r.element
}

// Prepaint implements Element (A:454-464).
func (a *AnimationElement[E]) Prepaint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, element *AnyElement, w *Window, app *App) struct{} {
	element.Prepaint(w, app)
	return struct{}{}
}

// Paint implements Element (A:466-477).
func (a *AnimationElement[E]) Paint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, element *AnyElement, prepaint *struct{}, w *Window, app *App) {
	element.Paint(w, app)
}

// ---------------------------------------------------------------------------
// Easing functions (elements/animation.rs mod easing, A:480-534)
// ---------------------------------------------------------------------------

// Linear is the linear easing function, or delta itself (A:484).
func Linear(delta float32) float32 { return delta }

// Quadratic is the quadratic easing function, delta * delta (A:489).
func Quadratic(delta float32) float32 { return delta * delta }

// EaseInOut is the quadratic ease-in-out function (A:494-501).
func EaseInOut(delta float32) float32 {
	if delta < 0.5 {
		return 2 * delta * delta
	}
	x := -2*delta + 2
	return 1 - x*x/2
}

// EaseOutQuint is the quint ease-out function (A:504-506).
func EaseOutQuint() func(float32) float32 {
	return func(delta float32) float32 {
		x := 1 - delta
		return 1 - x*x*x*x*x
	}
}

// Bounce applies the easing forward then in reverse (A:509-517).
func Bounce(easing func(float32) float32) func(float32) float32 {
	return func(delta float32) float32 {
		if delta < 0.5 {
			return easing(delta * 2)
		}
		return easing((1 - delta) * 2)
	}
}

// PulsatingBetween is the breathing alpha easing between min and max
// (A:520-533).
func PulsatingBetween(min, max float32) func(float32) float32 {
	span := max - min
	return func(delta float32) float32 {
		t := float32(math.Sin(float64(delta * 2 * math.Pi)))
		breath := (t*t*t + t) / 2
		normalized := (breath + 1) / 2
		return min + normalized*span
	}
}
