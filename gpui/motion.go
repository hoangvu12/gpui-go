package gpui

// The motion engine (U:motion.rs): value transitions, keyframe playback
// and the timing/easing/keyframe-interpolation machinery, sampled at the
// application clock and requesting animation frames while they move.
// Moved from base/motion.go into the core by browser-core ticket 44
// (research/browser-parity README, motion split: "Timing, easing and
// keyframe interpolation ... and the reversing rule → Core"); Base keeps
// aliases over this file plus its springs and orchestration
// (base/motion.go), with no behaviour change. Ticket 74 then moved Base's
// presets onto the engine directly: Presence runs on
// TransitionFromWithStatus (the presence seeding), and every gate reads
// App.MotionReduced, the one reduced-motion source (the App flag plus
// the live SystemPreferences record).
//
// Every function honours that gate by adopting its target at once.
//
// Core seams (R: = reference/ce-source/crates/gpui/src):
//
//   - `cx.background_executor().now()` is App.Now, the scheduler clock: the
//     virtual clock under TestApp (TestApp.AdvanceClock), monotonic wall time
//     in a real application (clock.go). Instants are durations since the
//     application started.
//   - `window.use_keyed_state` is Window.UseKeyedState
//     (R:window.rs:4090-4114, keyedstate.go).
//   - `window.request_animation_frame()` is RequestAnimationFrame
//     (R:window.rs:2633-2636, imagecache.go): the rendering view is
//     notified on the window's next frame.
//
// Go adaptations:
//
//   - Names: the free functions `transition` and `spring` would collide
//     with the policy types Transition and Spring in one Go package; they
//     are MotionTransition and MotionSpring. The transition half lives
//     here; the spring half stays in Base (the research split's
//     component half), so TransitionFromWithStatus is the seam Base's
//     presence seeds its enter animation through.
//   - Interpolate (U:motion.rs:32-59) is implemented upstream for every
//     Lerp type plus the foreign Size<Pixels> and Bounds<Pixels>. The Go
//     trait is the interface Interpolate[T] plus InterpolateValue, which
//     calls the trait method for any T (see InterpolateValue).
//   - `T: Interpolate + PartialEq` is `T comparable` with InterpolateValue.
//   - `impl Into<TransitionId>` is the IntoTransitionID type set; the
//     `(id, channel)` tuple conversion is TransitionIDWithChannel.

import (
	"fmt"
	"math"
	"time"
)

// defaultSpringEpsilon matches GPUI's own default spring settling
// tolerance (U:motion.rs:29-30); Base's Spring shares it.
const defaultSpringEpsilon float32 = 0.001

// Interpolate is a value that can be interpolated between two
// application-owned targets (U:motion.rs:32-35).
type Interpolate[T any] interface {
	Interpolate(target T, progress float32) T
}

// Lerp is implemented by types that support linear interpolation
// (U:animation.rs:91-94). See LerpValue for the core's types.
type Lerp[T any] interface {
	Lerp(target T, t float32) T
}

// LerpValue interpolates from toward target by t, the Lerp trait method
// for any T: float32 (f32 and Pixels, U:animation.rs:96-108), Point
// (Point<Pixels>, :110-117) and Hsla (:119-132) are built in; any other T
// must implement Lerp[T] (it panics otherwise).
//
// Hsla: every channel is interpolated linearly and clamped like the
// pinned hsla() constructor (R:color.rs:49-60). Deviation: the core
// stores the hue as the 0..1 fraction, so the upstream round trip through
// palette's into_degrees (which wraps hues past 0.5 to negative degrees)
// does not apply; intended, as upstream says, for near-grayscale colors.
func LerpValue[T any](from, target T, t float32) T {
	switch f := any(from).(type) {
	case float32:
		return any(lerpMotion32(f, any(target).(float32), t)).(T)
	case Point:
		to := any(target).(Point)
		return any(Point{X: lerpMotion32(f.X, to.X, t), Y: lerpMotion32(f.Y, to.Y, t)}).(T)
	case Hsla:
		to := any(target).(Hsla)
		return any(Hsla{
			H: clampMotion32(lerpMotion32(f.H, to.H, t), 0, 1),
			S: clampMotion32(lerpMotion32(f.S, to.S, t), 0, 1),
			L: clampMotion32(lerpMotion32(f.L, to.L, t), 0, 1),
			A: clampMotion32(lerpMotion32(f.A, to.A, t), 0, 1),
		}).(T)
	case Lerp[T]:
		return f.Lerp(target, t)
	}
	panic(fmt.Sprintf("gpui: %T implements neither Lerp nor Interpolate", from))
}

// InterpolateValue interpolates from toward target by progress, the
// Interpolate trait method for any T (U:motion.rs:37-59): Size and
// Bounds interpolate channel by channel, a T implementing
// Interpolate[T] uses its method, and every Lerp type (see LerpValue)
// interpolates through Lerp. Any other T panics.
func InterpolateValue[T any](from, target T, progress float32) T {
	switch f := any(from).(type) {
	case Size:
		return any(interpolateSize(f, any(target).(Size), progress)).(T)
	case Bounds:
		to := any(target).(Bounds)
		return any(Bounds{
			Origin: LerpValue(f.Origin, to.Origin, progress),
			Size:   interpolateSize(f.Size, to.Size, progress),
		}).(T)
	case Interpolate[T]:
		return f.Interpolate(target, progress)
	}
	return LerpValue(from, target, progress)
}

// interpolateSize is `impl Interpolate for Size<Pixels>` (U:motion.rs:43-50).
func interpolateSize(from, to Size, progress float32) Size {
	return Size{
		Width:  lerpMotion32(from.Width, to.Width, progress),
		Height: lerpMotion32(from.Height, to.Height, progress),
	}
}

// Transition is a CSS-like timing policy for a target-value transition
// (U:motion.rs:103-113). Unlike the style property's TransitionRule
// (transition.go) it never chooses a visual property for the caller.
type Transition struct {
	duration time.Duration
	delay    SignedDuration
	easing   Easing
}

// NewTransition eases out (cubic) over duration without delay
// (U:motion.rs:116-122).
func NewTransition(duration time.Duration) Transition {
	return Transition{duration: duration, easing: EasingCustom(EaseOutCubic)}
}

// Delay sets the delay: a time.Duration is a positive delay
// (U:motion.rs:124-127 with `From<Duration> for SignedDuration`, :172-176).
func (t Transition) Delay[D time.Duration | SignedDuration](delay D) Transition {
	switch v := any(delay).(type) {
	case time.Duration:
		t.delay = SignedDurationPositive(v)
	case SignedDuration:
		t.delay = v
	}
	return t
}

// Ease sets a custom easing function (U:motion.rs:129-132).
func (t Transition) Ease(easing func(float32) float32) Transition {
	t.easing = EasingCustom(easing)
	return t
}

// Easing sets the easing policy (U:motion.rs:134-137).
func (t Transition) Easing(easing Easing) Transition {
	t.easing = easing
	return t
}

// sample eases progress (U:motion.rs:139-141).
func (t Transition) sample(progress float32) float32 { return t.easing.Sample(progress) }

// curve is the easing curve, for a caller that samples one timeline at
// several offsets, such as a staggered plot appear (U:motion.rs:143-147).
func (t Transition) curve() Easing { return t.easing }

// progress is U:motion.rs:149-160.
func (t Transition) progress(elapsed, duration time.Duration) (float32, MotionStatus) {
	active, ok := t.delay.activeElapsed(elapsed)
	if !ok {
		return 0, MotionStatusDelayed
	}
	if duration == 0 || active >= duration {
		return 1, MotionStatusFinished
	}
	return secsF32(active) / secsF32(duration), MotionStatusRunning
}

// Duration returns the transition's duration.
func (t Transition) Duration() time.Duration { return t.duration }

// Sample eases progress (the public form of the engine's sample; Base's
// orchestration layers call it).
func (t Transition) Sample(progress float32) float32 { return t.sample(progress) }

// Progress evaluates the transition's timing at elapsed (the public form
// of the engine's progress).
func (t Transition) Progress(elapsed, duration time.Duration) (float32, MotionStatus) {
	return t.progress(elapsed, duration)
}

// Curve returns the easing curve (the public form of the engine's curve).
func (t Transition) Curve() Easing { return t.curve() }

// FinishesAfter is the elapsed time at which progress over the
// transition's own duration first reports Finished (the public form of
// the engine's finishesAfter; U:motion.rs:162-169).
func (t Transition) FinishesAfter() time.Duration { return t.finishesAfter() }

// finishesAfter is the elapsed time at which progress over the
// transition's own duration first reports Finished (U:motion.rs:162-169).
func (t Transition) finishesAfter() time.Duration {
	if t.delay.negative {
		return satSubDuration(t.duration, t.delay.d)
	}
	return satAddDuration(t.delay.d, t.duration)
}

// secsF32 is Rust's Duration::as_secs_f32: whole seconds plus the
// nanoseconds over 1e9, in f32.
func secsF32(d time.Duration) float32 {
	return float32(d/time.Second) + float32(d%time.Second)/float32(1e9)
}

// mulF32 scales a duration by a factor (the upstream Duration::mul_f32,
// U:motion.rs:331, U:motion/presence.rs:91).
//
// Deviation: Rust's mul_f32 round-trips through f32 seconds
// (from_secs_f32(rhs * as_secs_f32())), which turns 100ms * 1.0 into
// 100.000001ms; on the millisecond scheduler clock that keeps a
// transition Running at exactly delay + duration, against the upstream
// tests that expect Finished there (U:motion.rs:964-976, 1637-1649,
// 1126-1149). The port scales in f64 and rounds to the nanosecond, which
// is exact for the common factors (1, 1/2, 1/4) and otherwise within a
// nanosecond of Rust.
func mulF32(d time.Duration, rhs float32) time.Duration {
	return time.Duration(math.Round(float64(d) * float64(rhs)))
}

// TransitionID identifies one independently transitioning value
// (U:motion.rs:178-180).
type TransitionID struct {
	id ElementID
}

// IntoTransitionID is what converts into a TransitionID
// (U:motion.rs:182-216): an element id, a name, a usize (int) or i32
// integer, or a TransitionID.
type IntoTransitionID interface {
	string | ElementID | int | int32 | TransitionID
}

// TransitionIDOf converts id (U:motion.rs:182-216: ElementId, &str,
// String, SharedString, usize and i32 through their ElementId forms,
// R:window.rs:7716-7756).
func TransitionIDOf[I IntoTransitionID](id I) TransitionID {
	switch v := any(id).(type) {
	case TransitionID:
		return v
	case string:
		return TransitionID{NameElementID(v)}
	case ElementID:
		return TransitionID{v}
	case int:
		return TransitionID{IntegerElementID(uint64(int64(v)))}
	case int32:
		return TransitionID{IntegerElementID(uint64(int64(v)))}
	}
	panic("gpui: unreachable IntoTransitionID")
}

// TransitionIDWithChannel names one channel of an element
// (U:motion.rs:224-232, the `(id, channel)` conversion): the element id
// labelled by the channel. A TransitionID given as id converts through its
// element id (ElementID).
func TransitionIDWithChannel[I IntoTransitionID](id I, channel string) TransitionID {
	var parent ElementID
	if tid, ok := any(id).(TransitionID); ok {
		parent = tid.ElementID()
	} else {
		parent = TransitionIDOf(id).id
	}
	return TransitionID{NamedChildElementID(parent, channel)}
}

// ElementID is `From<TransitionId> for ElementId` (U:motion.rs:218-222):
// the transition's state key.
func (id TransitionID) ElementID() ElementID {
	return NamedChildElementID(id.id, "__base-transition-state")
}

// Key returns the transition's own element id, the raw identity the
// ElementID state key derives from (Base's keyed presence/sequence
// orchestration builds its own child ids on it).
func (id TransitionID) Key() ElementID { return id.id }

// Equal is the derived PartialEq (U:motion.rs:179).
func (id TransitionID) Equal(other TransitionID) bool { return elementIDEqual(id.id, other.id) }

// String renders the identity.
func (id TransitionID) String() string { return id.id.String() }

func elementIDEqual(a, b ElementID) bool {
	if a.Kind != b.Kind || a.Name != b.Name || a.Entity != b.Entity || a.Integer != b.Integer {
		return false
	}
	if a.Parent == nil || b.Parent == nil {
		return a.Parent == nil && b.Parent == nil
	}
	return elementIDEqual(*a.Parent, *b.Parent)
}

// valueTransition is the keyed state of one transition (U:motion.rs:234-241).
type valueTransition[T any] struct {
	from, target    T
	startedAt       time.Duration
	reversingFactor float32
	duration        time.Duration
}

// MotionStatus is the status of a motion sample (U:motion.rs:243-249).
type MotionStatus uint8

const (
	MotionStatusIdle MotionStatus = iota
	MotionStatusDelayed
	MotionStatusRunning
	MotionStatusFinished
)

// String is the Debug rendering.
func (s MotionStatus) String() string {
	switch s {
	case MotionStatusIdle:
		return "Idle"
	case MotionStatusDelayed:
		return "Delayed"
	case MotionStatusRunning:
		return "Running"
	case MotionStatusFinished:
		return "Finished"
	}
	return fmt.Sprintf("MotionStatus(%d)", uint8(s))
}

// active reports Delayed or Running, the statuses that request a frame.
func (s MotionStatus) active() bool {
	return s == MotionStatusDelayed || s == MotionStatusRunning
}

// IsActive reports Delayed or Running, the statuses that request a frame
// (the public form of the engine's active; Base's orchestration layers
// call it).
func (s MotionStatus) IsActive() bool { return s.active() }

// MotionValue is a sampled value and its status (U:motion.rs:251-255).
type MotionValue[T any] struct {
	Value  T
	Status MotionStatus
}

// MotionTransition returns the current value for a CSS-like transition
// toward target (U:motion.rs:257-276 `transition`; renamed, see the file
// comment).
//
// State is keyed by id. The first value is adopted immediately; later
// target changes transition from the value sampled at that instant.
// Components opt into this function explicitly — base components do not
// install default motion. Call it while rendering an element, where keyed
// element state is available. A channel id must identify one value type
// within that element.
func MotionTransition[I IntoTransitionID, T comparable](id I, target T, policy Transition, w *Window, cx *App) T {
	return TransitionWithStatus(id, target, policy, w, cx).Value
}

// TransitionWithStatus is MotionTransition with the transition's status
// (U:motion.rs:278-358). A reversal toward the value the transition
// started from shortens the return by the progress made.
//
// This is the CSS reversing-shortening rule (css-transitions-1 §3;
// Chromium core/animation/css/css_animations.cc:2890-2912): the
// shortening factor is the interrupted transition's eased progress times
// its old factor plus (1 - old factor), and the new duration is the rule
// duration times that factor.
//
// The keyed state adopts the first target immediately, and a state at
// rest (from == target) reports Idle; use TransitionFromWithStatus when
// the first sample must begin from a caller-owned starting value instead
// (the mount-presence seeding).
func TransitionWithStatus[I IntoTransitionID, T comparable](id I, target T, policy Transition, w *Window, cx *App) MotionValue[T] {
	return transitionWithStatus(TransitionIDOf(id), target, target, policy, w, cx, true)
}

// TransitionFromWithStatus is TransitionWithStatus with a caller-owned
// starting value: the keyed state seeds from `from` and target, so the
// first sample begins a transition from from toward target instead of
// adopting it (the mount-presence seeding, U:motion/presence.rs:128-135,
// which seeds its keyed state the same way). Every later target change
// retargets and reverses exactly as TransitionWithStatus does, and the
// reduced-motion gate resolves at once. Presence runs on this seam.
//
// Unlike the adopting form, a seeded state whose from equals its target
// does not report Idle: the transition still runs out its policy
// duration (the mount clock a presence owes an exit even from nothing —
// U:motion/presence.rs:48-125, which nav_stack's transit measures).
func TransitionFromWithStatus[I IntoTransitionID, T comparable](id I, from, target T, policy Transition, w *Window, cx *App) MotionValue[T] {
	return transitionWithStatus(TransitionIDOf(id), from, target, policy, w, cx, false)
}

// transitionWithStatus is the shared keyed-transition body; adopting
// marks the entry point that adopts its first target, whose at-rest
// state (from == target) reports Idle instead of running out the policy.
func transitionWithStatus[T comparable](id TransitionID, from, target T, policy Transition, w *Window, cx *App, adopting bool) MotionValue[T] {
	key := id.ElementID()
	now := cx.Now()
	state := w.UseKeyedState[valueTransition[T]](key, cx, func(*Window, *Context[valueTransition[T]]) valueTransition[T] {
		return valueTransition[T]{
			from:            from,
			target:          target,
			startedAt:       now,
			reversingFactor: 1,
			duration:        policy.duration,
		}
	})
	snapshot := state.Read(cx, func(s *valueTransition[T], _ *App) valueTransition[T] { return *s })

	if cx.MotionReduced() || policy.duration == 0 {
		if snapshot.from != target || snapshot.target != target {
			state.Update(cx, func(s *valueTransition[T], _ *Context[valueTransition[T]]) {
				s.from = target
				s.target = target
				s.startedAt = now
				s.reversingFactor = 1
				s.duration = policy.duration
			})
		}
		return MotionValue[T]{Value: target, Status: MotionStatusFinished}
	}

	elapsed := satSubDuration(now, snapshot.startedAt)
	progress, status := policy.progress(elapsed, snapshot.duration)
	sampled := InterpolateValue(snapshot.from, snapshot.target, policy.sample(progress))

	var value T
	if snapshot.target != target {
		reversing := target == snapshot.from
		reversingFactor := float32(1)
		if reversing {
			reversingFactor = clampMotion32(
				float32(policy.sample(progress)*snapshot.reversingFactor)+(1-snapshot.reversingFactor), 0, 1)
		}
		duration := mulF32(policy.duration, reversingFactor)
		state.Update(cx, func(s *valueTransition[T], _ *Context[valueTransition[T]]) {
			s.from = sampled
			s.target = target
			s.startedAt = now
			s.reversingFactor = reversingFactor
			s.duration = duration
		})
		initialProgress, initialStatus := policy.progress(0, duration)
		value = InterpolateValue(sampled, target, policy.sample(initialProgress))
		status = initialStatus
	} else {
		value = sampled
		if adopting && snapshot.from == snapshot.target {
			status = MotionStatusIdle
		}
	}
	if status.active() {
		RequestAnimationFrame(w)
	}
	return MotionValue[T]{Value: value, Status: status}
}

// keyframePlayback is the keyed start of a keyframe playback
// (U:motion.rs:360-363).
type keyframePlayback struct {
	startedAt time.Duration
}

// AnimateKeyframes samples a keyed keyframe playback and requests frames
// while it is active (U:motion.rs:365-407).
//
// The stable id owns the playback's start time. Re-rendering with the
// same id continues that playback; it does not restart when keyframes or
// timing is reconstructed. To replay, include an application-owned
// generation in the id, for example
// TransitionIDWithChannel(NamedIntegerElementID("notification-enter", generation), ...).
func AnimateKeyframes[I IntoTransitionID, T any](id I, keyframes Keyframes[T], timing Timing, w *Window, cx *App) MotionValue[T] {
	key := NamedChildElementID(TransitionIDOf(id).ElementID(), "__keyframes")
	now := cx.Now()
	state := w.UseKeyedState[keyframePlayback](key, cx, func(*Window, *Context[keyframePlayback]) keyframePlayback {
		return keyframePlayback{startedAt: now}
	})
	startedAt := state.Read(cx, func(s *keyframePlayback, _ *App) time.Duration { return s.startedAt })

	if cx.MotionReduced() {
		return MotionValue[T]{Value: keyframes.Sample(1), Status: MotionStatusFinished}
	}

	sample := timing.Sample(satSubDuration(now, startedAt))
	var status MotionStatus
	switch sample.Phase {
	case TimingPhaseBefore:
		status = MotionStatusDelayed
	case TimingPhaseActive:
		status = MotionStatusRunning
	default:
		status = MotionStatusFinished
	}
	if status.active() {
		RequestAnimationFrame(w)
	}
	return MotionValue[T]{Value: keyframes.Sample(sample.DirectedProgress), Status: status}
}

// MotionReduced reports whether motion is reduced for this application:
// the App's own policy flag (ReduceMotion / SetReduceMotion) or the OS
// preference through SystemPreferences — the live record the host
// refreshes on the Windows settings messages (ticket 27), or the default
// record when there is no host. This is the single reduced-motion
// source: the keyed transitions and keyframe playback in this package,
// the style-diff transition engine (transition.go), and Base's motion
// presets (browser-core ticket 74) all gate on it, so a component never
// reads the platform preference itself.
func (a *App) MotionReduced() bool {
	return a.reduceMotion || a.SystemPreferences().ReducedMotion
}
