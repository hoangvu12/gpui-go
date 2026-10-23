package base

// The motion system (U:motion.rs): value transitions, keyframe playback
// and springs keyed by element state, sampled at the application clock and
// requesting animation frames while they move. Every function honors the
// reduced-motion flag (App.ReduceMotion, see reduce_motion.go) by adopting
// its target at once.
//
// Core seams (R: = reference/ce-source/crates/gpui/src):
//
//   - `cx.background_executor().now()` is App.Now, the scheduler clock: the
//     virtual clock under TestApp (TestApp.AdvanceClock), monotonic wall time
//     in a real application (gpui/clock.go). Instants are durations since
//     the application started.
//   - `window.use_keyed_state` is Window.UseKeyedState
//     (R:window.rs:4090-4114, gpui/keyedstate.go).
//   - `window.request_animation_frame()` is gpui.RequestAnimationFrame
//     (R:window.rs:2633-2636, gpui/imagecache.go): the rendering view is
//     notified on the window's next frame.
//
// Go adaptations:
//
//   - Names: the free functions `transition` and `spring` would collide
//     with the policy types Transition and Spring in one Go package; they
//     are MotionTransition and MotionSpring.
//   - Interpolate (U:motion.rs:32-59) is implemented upstream for every
//     Lerp type plus the foreign Size<Pixels> and Bounds<Pixels>. The Go
//     trait is the interface Interpolate[T] plus InterpolateValue, which
//     calls the trait method for any T (see InterpolateValue).
//   - `T: Interpolate + PartialEq` is `T comparable` with InterpolateValue.
//   - `impl Into<TransitionId>` is the IntoTransitionID type set; the
//     `(id, channel)` tuple conversion is TransitionIDWithChannel.
//   - SpringValue (U:motion.rs:557-585), implemented upstream for f32 and
//     Pixels (both float32 in the core), is the type set ~float32.
//   - The window and application are *gpui.Window and *gpui.App; a render
//     passes cx.App().

import (
	"fmt"
	"math"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

// defaultSpringEpsilon matches GPUI's own default spring settling
// tolerance (U:motion.rs:29-30).
const defaultSpringEpsilon float32 = 0.001

// Interpolate is a value that can be interpolated between two
// application-owned targets (U:motion.rs:32-35).
type Interpolate[T any] interface {
	Interpolate(target T, progress float32) T
}

// InterpolateValue interpolates from toward target by progress, the
// Interpolate trait method for any T (U:motion.rs:37-59): gpui.Size and
// gpui.Bounds interpolate channel by channel, a T implementing
// Interpolate[T] uses its method, and every Lerp type (see LerpValue)
// interpolates through Lerp. Any other T panics.
func InterpolateValue[T any](from, target T, progress float32) T {
	switch f := any(from).(type) {
	case gpui.Size:
		return any(interpolateSize(f, any(target).(gpui.Size), progress)).(T)
	case gpui.Bounds:
		to := any(target).(gpui.Bounds)
		return any(gpui.Bounds{
			Origin: LerpValue(f.Origin, to.Origin, progress),
			Size:   interpolateSize(f.Size, to.Size, progress),
		}).(T)
	case Interpolate[T]:
		return f.Interpolate(target, progress)
	}
	return LerpValue(from, target, progress)
}

// interpolateSize is `impl Interpolate for Size<Pixels>` (U:motion.rs:43-50).
func interpolateSize(from, to gpui.Size, progress float32) gpui.Size {
	return gpui.Size{
		Width:  lerp32(from.Width, to.Width, progress),
		Height: lerp32(from.Height, to.Height, progress),
	}
}

// MotionTransform is a presentation-neutral bundle for coordinated paint
// transforms (U:motion.rs:61-68).
type MotionTransform struct {
	Translation     gpui.Point
	Scale           gpui.Point
	RotationRadians float32
	Opacity         float32
}

// MotionTransformIdentity is no translation, unit scale, no rotation and
// full opacity (U:motion.rs:70-79).
func MotionTransformIdentity() MotionTransform {
	return MotionTransform{Scale: gpui.Point{X: 1, Y: 1}, Opacity: 1}
}

// DefaultMotionTransform is the identity (U:motion.rs:81-85).
func DefaultMotionTransform() MotionTransform { return MotionTransformIdentity() }

// Interpolate interpolates every field (U:motion.rs:87-101).
func (m MotionTransform) Interpolate(target MotionTransform, progress float32) MotionTransform {
	return MotionTransform{
		Translation: LerpValue(m.Translation, target.Translation, progress),
		Scale: gpui.Point{
			X: lerp32(m.Scale.X, target.Scale.X, progress),
			Y: lerp32(m.Scale.Y, target.Scale.Y, progress),
		},
		RotationRadians: lerp32(m.RotationRadians, target.RotationRadians, progress),
		Opacity:         lerp32(m.Opacity, target.Opacity, progress),
	}
}

// Transition is a CSS-like timing policy for a target-value transition
// (U:motion.rs:103-113). Unlike EffectTransition it never chooses a
// visual property for the caller.
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
	id gpui.ElementID
}

// IntoTransitionID is what converts into a TransitionID
// (U:motion.rs:182-216): an element id, a name, a usize (int) or i32
// integer, or a TransitionID.
type IntoTransitionID interface {
	string | gpui.ElementID | int | int32 | TransitionID
}

// TransitionIDOf converts id (U:motion.rs:182-216: ElementId, &str,
// String, SharedString, usize and i32 through their ElementId forms,
// R:window.rs:7716-7756).
func TransitionIDOf[I IntoTransitionID](id I) TransitionID {
	switch v := any(id).(type) {
	case TransitionID:
		return v
	case string:
		return TransitionID{gpui.NameElementID(v)}
	case gpui.ElementID:
		return TransitionID{v}
	case int:
		return TransitionID{gpui.IntegerElementID(uint64(int64(v)))}
	case int32:
		return TransitionID{gpui.IntegerElementID(uint64(int64(v)))}
	}
	panic("base: unreachable IntoTransitionID")
}

// TransitionIDWithChannel names one channel of an element
// (U:motion.rs:224-232, the `(id, channel)` conversion): the element id
// labelled by the channel. A TransitionID given as id converts through its
// element id (ElementID).
func TransitionIDWithChannel[I IntoTransitionID](id I, channel string) TransitionID {
	var parent gpui.ElementID
	if tid, ok := any(id).(TransitionID); ok {
		parent = tid.ElementID()
	} else {
		parent = TransitionIDOf(id).id
	}
	return TransitionID{gpui.NamedChildElementID(parent, channel)}
}

// ElementID is `From<TransitionId> for ElementId` (U:motion.rs:218-222):
// the transition's state key.
func (id TransitionID) ElementID() gpui.ElementID {
	return gpui.NamedChildElementID(id.id, "__base-transition-state")
}

// Equal is the derived PartialEq (U:motion.rs:179).
func (id TransitionID) Equal(other TransitionID) bool { return elementIDEqual(id.id, other.id) }

// String renders the identity.
func (id TransitionID) String() string { return id.id.String() }

func elementIDEqual(a, b gpui.ElementID) bool {
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
func MotionTransition[I IntoTransitionID, T comparable](id I, target T, policy Transition, w *gpui.Window, cx *gpui.App) T {
	return TransitionWithStatus(id, target, policy, w, cx).Value
}

// TransitionWithStatus is MotionTransition with the transition's status
// (U:motion.rs:278-358). A reversal toward the value the transition
// started from shortens the return by the progress made.
func TransitionWithStatus[I IntoTransitionID, T comparable](id I, target T, policy Transition, w *gpui.Window, cx *gpui.App) MotionValue[T] {
	key := TransitionIDOf(id).ElementID()
	now := cx.Now()
	state := w.UseKeyedState[valueTransition[T]](key, cx, func(*gpui.Window, *gpui.Context[valueTransition[T]]) valueTransition[T] {
		return valueTransition[T]{
			from:            target,
			target:          target,
			startedAt:       now,
			reversingFactor: 1,
			duration:        policy.duration,
		}
	})
	snapshot := state.Read(cx, func(s *valueTransition[T], _ *gpui.App) valueTransition[T] { return *s })

	if cx.ReduceMotion() || policy.duration == 0 {
		if snapshot.from != target || snapshot.target != target {
			state.Update(cx, func(s *valueTransition[T], _ *gpui.Context[valueTransition[T]]) {
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
			reversingFactor = clamp32(
				float32(policy.sample(progress)*snapshot.reversingFactor)+(1-snapshot.reversingFactor), 0, 1)
		}
		duration := mulF32(policy.duration, reversingFactor)
		state.Update(cx, func(s *valueTransition[T], _ *gpui.Context[valueTransition[T]]) {
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
		if snapshot.from == snapshot.target {
			status = MotionStatusIdle
		}
	}
	if status.active() {
		gpui.RequestAnimationFrame(w)
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
// TransitionIDWithChannel(gpui.NamedIntegerElementID("notification-enter", generation), ...).
func AnimateKeyframes[I IntoTransitionID, T any](id I, keyframes Keyframes[T], timing Timing, w *gpui.Window, cx *gpui.App) MotionValue[T] {
	key := gpui.NamedChildElementID(TransitionIDOf(id).ElementID(), "__keyframes")
	now := cx.Now()
	state := w.UseKeyedState[keyframePlayback](key, cx, func(*gpui.Window, *gpui.Context[keyframePlayback]) keyframePlayback {
		return keyframePlayback{startedAt: now}
	})
	startedAt := state.Read(cx, func(s *keyframePlayback, _ *gpui.App) time.Duration { return s.startedAt })

	if cx.ReduceMotion() {
		return MotionValue[T]{Value: keyframes.Sample(1), Status: MotionStatusFinished}
	}

	sample := timing.Sample(satSubDuration(now, startedAt))
	var status MotionStatus
	switch sample.Phase {
	case MotionPhaseBefore:
		status = MotionStatusDelayed
	case MotionPhaseActive:
		status = MotionStatusRunning
	default:
		status = MotionStatusFinished
	}
	if status.active() {
		gpui.RequestAnimationFrame(w)
	}
	return MotionValue[T]{Value: keyframes.Sample(sample.DirectedProgress), Status: status}
}

// Spring is a physical spring policy for MotionSpring (U:motion.rs:409-423).
//
// A spring is the counterpart to Transition for values that can be
// retargeted while they are still moving: it carries velocity across the
// retarget, so a value reversed mid-flight decelerates and turns around
// instead of snapping to a new curve's initial speed.
type Spring struct {
	response time.Duration
	damping  float32
	epsilon  float32
	travel   bool
}

// SpringError is an invalid physical or settling parameter
// (U:motion.rs:425-443).
type SpringError uint8

const (
	SpringErrorInvalidDamping SpringError = iota
	SpringErrorInvalidEpsilon
)

// Error is the Display rendering (U:motion.rs:432-441).
func (e SpringError) Error() string {
	switch e {
	case SpringErrorInvalidDamping:
		return "spring damping must be finite and non-negative"
	case SpringErrorInvalidEpsilon:
		return "spring epsilon must be finite and greater than zero"
	}
	return fmt.Sprintf("SpringError(%d)", uint8(e))
}

// NewSpring builds a critically damped spring that reaches its target in
// about response without overshooting it (U:motion.rs:446-467).
//
// response is the period one full undamped oscillation would take — the
// scale the motion is felt at, not the moment it stops; the remainder
// keeps settling until it is within the tolerance WithEpsilon sets. A
// zero response adopts the target on the spot.
func NewSpring(response time.Duration) Spring {
	return Spring{response: response, damping: 1, epsilon: defaultSpringEpsilon, travel: true}
}

// WithDamping sets the damping ratio ζ, 1 (no overshoot) by default
// (U:motion.rs:469-488). Below 1 the spring overshoots; above 1 it
// approaches slowly. It panics when ratio is negative or non-finite; use
// TryWithDamping for untrusted values.
func (s Spring) WithDamping(ratio float32) Spring {
	spring, err := s.TryWithDamping(ratio)
	if err != nil {
		panic("spring damping must be finite and non-negative")
	}
	return spring
}

// TryWithDamping is the checked WithDamping (U:motion.rs:490-497).
func (s Spring) TryWithDamping(ratio float32) (Spring, error) {
	if !finite32(ratio) || ratio < 0 {
		return s, SpringErrorInvalidDamping
	}
	s.damping = ratio
	return s, nil
}

// WithTravel sets whether the spring travels to its target or adopts it
// on the spot (U:motion.rs:499-514). A value the pointer is already moving
// stops travelling for as long as the drag lasts; retained state stays
// pinned to the target meanwhile, so travel resumes from the value the
// drag released.
func (s Spring) WithTravel(travel bool) Spring {
	s.travel = travel
	return s
}

// WithEpsilon sets the settling tolerance in the target's own units
// (U:motion.rs:516-532). It panics when epsilon is zero, negative or
// non-finite; use TryWithEpsilon for untrusted values.
func (s Spring) WithEpsilon(epsilon float32) Spring {
	spring, err := s.TryWithEpsilon(epsilon)
	if err != nil {
		panic("spring epsilon must be finite and greater than zero")
	}
	return spring
}

// TryWithEpsilon is the checked WithEpsilon (U:motion.rs:534-541).
func (s Spring) TryWithEpsilon(epsilon float32) (Spring, error) {
	if !finite32(epsilon) || epsilon <= 0 {
		return s, SpringErrorInvalidEpsilon
	}
	s.epsilon = epsilon
	return s, nil
}

// Epsilon returns the settling tolerance (U:motion.rs:543-546).
func (s Spring) Epsilon() float32 { return s.epsilon }

// config is the stiffness and damping coefficient of the integrator
// (U:motion.rs:548-554); the response must be non-zero.
func (s Spring) config() (float32, float32) {
	frequency := float32(2*math.Pi) / secsF32(s.response)
	return float32(frequency * frequency), float32(2 * s.damping * frequency)
}

// SpringValue is a scalar MotionSpring animates (U:motion.rs:557-585):
// f32 and Pixels upstream, any float32 type here.
type SpringValue interface {
	~float32
}

// springTransition is the keyed spring state (U:motion.rs:587-593).
type springTransition struct {
	position, velocity, target float32
	updatedAt                  time.Duration
}

// stepSpring integrates in fixed steps of at most 1/120 s so a resumed
// animation settles instead of exploding (U:motion.rs:595-612).
func stepSpring(state springTransition, target, elapsed, stiffness, damping float32) springTransition {
	steps := int(max(float32(math.Ceil(float64(elapsed/float32(1.0/120.0)))), 1))
	step := elapsed / float32(steps)
	for range steps {
		acceleration := float32(stiffness*(target-state.position)) - float32(damping*state.velocity)
		state.velocity += float32(acceleration * step)
		state.position += float32(state.velocity * step)
	}
	return state
}

// MotionSpring returns the current value for a spring travelling toward
// target (U:motion.rs:614-695 `spring`; renamed, see the file comment).
//
// State is keyed by id exactly as MotionTransition keys its own. The first
// value is adopted immediately; later target changes preserve both the
// current position and velocity, so an interrupted spring is redirected
// rather than restarted.
func MotionSpring[I IntoTransitionID, T SpringValue](id I, target T, policy Spring, w *gpui.Window, cx *gpui.App) T {
	key := TransitionIDOf(id).ElementID()
	now := cx.Now()
	targetPosition := float32(target)
	state := w.UseKeyedState[springTransition](key, cx, func(*gpui.Window, *gpui.Context[springTransition]) springTransition {
		return springTransition{position: targetPosition, target: targetPosition, updatedAt: now}
	})
	snapshot := state.Read(cx, func(s *springTransition, _ *gpui.App) springTransition { return *s })

	// A spring nothing is moving has no state to advance and no frame to
	// ask for (U:motion.rs:646-658).
	if snapshot.position == targetPosition && snapshot.velocity == 0 {
		return T(targetPosition)
	}

	settle := func(s *springTransition, _ *gpui.Context[springTransition]) {
		s.position = targetPosition
		s.velocity = 0
		s.target = targetPosition
		s.updatedAt = now
	}

	if cx.ReduceMotion() || !policy.travel || policy.response == 0 {
		state.Update(cx, settle)
		return T(targetPosition)
	}

	// Advance over the frame that just elapsed, which the previous target
	// governed, before adopting the new one (U:motion.rs:672-678).
	elapsed := secsF32(satSubDuration(now, snapshot.updatedAt))
	stiffness, damping := policy.config()
	stepped := stepSpring(snapshot, snapshot.target, elapsed, stiffness, damping)

	if abs32(stepped.position-targetPosition) <= policy.epsilon && abs32(stepped.velocity) <= policy.epsilon {
		state.Update(cx, settle)
		return T(targetPosition)
	}

	state.Update(cx, func(s *springTransition, _ *gpui.Context[springTransition]) {
		s.position = stepped.position
		s.velocity = stepped.velocity
		s.target = targetPosition
		s.updatedAt = now
	})
	gpui.RequestAnimationFrame(w)
	return T(stepped.position)
}
