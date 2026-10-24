package base

// The motion system's Base half (U:motion.rs). Ticket 44 moved the
// timing, easing and keyframe-interpolation engine — the Transition
// policy with CSS's reversing rule, the keyed value transitions and
// keyframe playback — into the core (gpui/motion.go, gpui/
// motion_easing.go, gpui/motion_timing.go, gpui/motion_keyframes.go and
// the bezier solver from base/animation.go); this file keeps Base's
// public surface as aliases over the moved engine. Ticket 74 then
// removed the rest of Base's own motion code: the presets call the core
// engine directly (Presence runs on gpui.TransitionFromWithStatus, the
// sequences and springs on the core's Transition policies and clock)
// and every reduced-motion gate is the core's single source,
// App.MotionReduced (the App flag plus the live SystemPreferences
// record; Base no longer reads the platform preference itself).
//
// What remains Base's is the research split's component half (research/
// browser-parity README, "Presence, stagger, sequence, reveal, springs
// as tokens → Components"): the springs and the MotionTransform
// presentation bundle, plus the presence/sequence/stagger/reveal
// orchestration in their own files.
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
// tolerance (U:motion.rs:29-30; the engine's copy is private to the
// core).
const defaultSpringEpsilon float32 = 0.001

// ---------------------------------------------------------------------------
// The moved engine, aliased (no behaviour change)
// ---------------------------------------------------------------------------

// The engine's public types are the core's types now; the aliases keep
// Base's names, methods and constructors resolving to the identical
// implementations.
type (
	// Easing is gpui.Easing (U:motion/easing.rs).
	Easing = gpui.Easing
	// StepPosition is gpui.StepPosition.
	StepPosition = gpui.StepPosition
	// LinearStop is gpui.LinearStop.
	LinearStop = gpui.LinearStop
	// EasingError is gpui.EasingError.
	EasingError = gpui.EasingError
	// SignedDuration is gpui.SignedDuration (U:motion/timing.rs).
	SignedDuration = gpui.SignedDuration
	// IterationCount is gpui.IterationCount.
	IterationCount = gpui.IterationCount
	// PlaybackDirection is gpui.PlaybackDirection.
	PlaybackDirection = gpui.PlaybackDirection
	// TimingSample is gpui.TimingSample.
	TimingSample = gpui.TimingSample
	// Timing is gpui.Timing (U:motion/timing.rs).
	Timing = gpui.Timing
	// TransitionID is gpui.TransitionID (U:motion.rs:178-180).
	TransitionID = gpui.TransitionID
	// MotionStatus is gpui.MotionStatus (U:motion.rs:243-249).
	MotionStatus = gpui.MotionStatus
	// Transition is gpui.Transition, the CSS-like timing policy
	// (U:motion.rs:103-113).
	Transition = gpui.Transition
	// Interpolate is gpui.Interpolate (U:motion.rs:32-35).
	Interpolate[T any] = gpui.Interpolate[T]
	// Lerp is gpui.Lerp (U:animation.rs:91-94).
	Lerp[T any] = gpui.Lerp[T]
	// Keyframe is gpui.Keyframe (U:motion/keyframes.rs:5-25).
	Keyframe[T any] = gpui.Keyframe[T]
	// Keyframes is gpui.Keyframes (U:motion/keyframes.rs:36-39).
	Keyframes[T any] = gpui.Keyframes[T]
	// Discrete is gpui.Discrete (U:motion/keyframes.rs:106-111).
	Discrete[T any] = gpui.Discrete[T]
	// KeyframeError is gpui.KeyframeError.
	KeyframeError = gpui.KeyframeError
	// DiscreteError is gpui.DiscreteError.
	DiscreteError = gpui.DiscreteError
	// MotionValue is gpui.MotionValue (U:motion.rs:251-255).
	MotionValue[T any] = gpui.MotionValue[T]
)

// MotionPhase is gpui.TimingPhase (U:motion/timing.rs:57-62; the timing
// half of the moved engine). The core's own MotionPhase is the pinned
// duration-motion's phase (gpui/animation.go), so the timing phase was
// renamed when it moved into the core; the alias keeps Base's name.
type MotionPhase = gpui.TimingPhase

// The timing phase constants (Base's MotionPhase* names).
const (
	MotionPhaseBefore = gpui.TimingPhaseBefore
	MotionPhaseActive = gpui.TimingPhaseActive
	MotionPhaseAfter  = gpui.TimingPhaseAfter
)

// The easing constructors and constants.
var (
	EasingLinear      = gpui.EasingLinear
	EasingEase        = gpui.EasingEase
	EasingEaseIn      = gpui.EasingEaseIn
	EasingEaseOut     = gpui.EasingEaseOut
	EasingEaseInOut   = gpui.EasingEaseInOut
	EasingCustom      = gpui.EasingCustom
	DefaultEasing     = gpui.DefaultEasing
	EasingCubicBezier = gpui.EasingCubicBezier
	EasingSteps       = gpui.EasingSteps
	EasingLinearStops = gpui.EasingLinearStops

	// The easing error constants.
	EasingErrorInvalidBezierX            = gpui.EasingErrorInvalidBezierX
	EasingErrorInvalidBezierControlPoint = gpui.EasingErrorInvalidBezierControlPoint
	EasingErrorInvalidStepCount          = gpui.EasingErrorInvalidStepCount
	EasingErrorInvalidLinearStops        = gpui.EasingErrorInvalidLinearStops

	// The step-position constants.
	StepPositionJumpStart = gpui.StepPositionJumpStart
	StepPositionJumpEnd   = gpui.StepPositionJumpEnd
	StepPositionJumpNone  = gpui.StepPositionJumpNone
	StepPositionJumpBoth  = gpui.StepPositionJumpBoth

	// NewLinearStop and LinearStopAt are the stop constructors.
	NewLinearStop = gpui.NewLinearStop
	LinearStopAt  = gpui.LinearStopAt

	// CubicBezier is the CSS cubic-bezier solver (moved to the core with
	// the easing engine; U:animation.rs:9-63).
	CubicBezier = gpui.CubicBezier
	// EaseOutCubic is the cubic ease-out (U:animation.rs:67-71), the
	// moved engine's default transition ease.
	EaseOutCubic = gpui.EaseOutCubic

	// SignedDuration constructors and constants.
	SignedDurationZero     = gpui.SignedDurationZero
	SignedDurationPositive = gpui.SignedDurationPositive
	SignedDurationNegative = gpui.SignedDurationNegative
	DefaultSignedDuration  = gpui.DefaultSignedDuration

	// IterationCount constructors.
	IterationCountFinite   = gpui.IterationCountFinite
	IterationCountInfinite = gpui.IterationCountInfinite
	DefaultIterationCount  = gpui.DefaultIterationCount

	// PlaybackDirection constants.
	PlaybackDirectionNormal           = gpui.PlaybackDirectionNormal
	PlaybackDirectionReverse          = gpui.PlaybackDirectionReverse
	PlaybackDirectionAlternate        = gpui.PlaybackDirectionAlternate
	PlaybackDirectionAlternateReverse = gpui.PlaybackDirectionAlternateReverse

	// NewTiming is the one-iteration timing constructor.
	NewTiming = gpui.NewTiming

	// NewTransition eases out (cubic) over duration without delay
	// (U:motion.rs:116-122).
	NewTransition = gpui.NewTransition

	// MotionStatus constants.
	MotionStatusIdle     = gpui.MotionStatusIdle
	MotionStatusDelayed  = gpui.MotionStatusDelayed
	MotionStatusRunning  = gpui.MotionStatusRunning
	MotionStatusFinished = gpui.MotionStatusFinished

	// KeyframeError and DiscreteError constants.
	KeyframeErrorTooFewFrames        = gpui.KeyframeErrorTooFewFrames
	KeyframeErrorOffsetNotFinite     = gpui.KeyframeErrorOffsetNotFinite
	KeyframeErrorOffsetOutOfRange    = gpui.KeyframeErrorOffsetOutOfRange
	KeyframeErrorOffsetsNotMonotonic = gpui.KeyframeErrorOffsetsNotMonotonic
	KeyframeErrorMissingEndpoint     = gpui.KeyframeErrorMissingEndpoint
	DiscreteErrorInvalidSwitchPoint  = gpui.DiscreteErrorInvalidSwitchPoint
)

// TransitionIDOf converts id into a TransitionID (U:motion.rs:182-216).
func TransitionIDOf[I gpui.IntoTransitionID](id I) TransitionID { return gpui.TransitionIDOf(id) }

// TransitionIDWithChannel names one channel of an element
// (U:motion.rs:224-232).
func TransitionIDWithChannel[I gpui.IntoTransitionID](id I, channel string) TransitionID {
	return gpui.TransitionIDWithChannel(id, channel)
}

// IntoTransitionID is what converts into a TransitionID: Base's spelling
// over the core's set (which includes Base's TransitionID alias).
type IntoTransitionID = gpui.IntoTransitionID

// LerpValue interpolates from toward target by t, the Lerp trait method
// for any T (U:animation.rs:96+); the core implementation, moved by
// ticket 44.
func LerpValue[T any](from, target T, t float32) T { return gpui.LerpValue(from, target, t) }

// InterpolateValue interpolates from toward target by progress, the
// Interpolate trait method for any T (U:motion.rs:37-59); the core
// implementation, moved by ticket 44.
func InterpolateValue[T any](from, target T, progress float32) T {
	return gpui.InterpolateValue(from, target, progress)
}

// KeyframesTryNew validates a keyframe track (U:motion/keyframes.rs:42-68).
func KeyframesTryNew[T any](frames ...Keyframe[T]) (Keyframes[T], error) {
	return gpui.KeyframesTryNew(frames...)
}

// NewKeyframe is a linearly eased frame (U:motion/keyframes.rs:13-19).
func NewKeyframe[T any](offset float32, value T) Keyframe[T] {
	return gpui.NewKeyframe(offset, value)
}

// NewDiscrete is the halfway discrete switch (U:motion/keyframes.rs:114-120).
func NewDiscrete[T any](from, to T) Discrete[T] { return gpui.NewDiscrete(from, to) }

// MotionTransition returns the current value for a CSS-like transition
// toward target (U:motion.rs:257-276 `transition`; renamed, see the file
// comment). State is keyed by id; the first value is adopted immediately,
// later target changes transition from the value sampled at that instant.
func MotionTransition[I IntoTransitionID, T comparable](id I, target T, policy Transition, w *gpui.Window, cx *gpui.App) T {
	return gpui.MotionTransition[I, T](id, target, policy, w, cx)
}

// TransitionWithStatus is MotionTransition with the transition's status
// (U:motion.rs:278-358). A reversal toward the value the transition
// started from shortens the return by the progress made (CSS's
// reversing rule).
func TransitionWithStatus[I IntoTransitionID, T comparable](id I, target T, policy Transition, w *gpui.Window, cx *gpui.App) MotionValue[T] {
	return gpui.TransitionWithStatus[I, T](id, target, policy, w, cx)
}

// AnimateKeyframes samples a keyed keyframe playback and requests frames
// while it is active (U:motion.rs:365-407).
func AnimateKeyframes[I IntoTransitionID, T any](id I, keyframes Keyframes[T], timing Timing, w *gpui.Window, cx *gpui.App) MotionValue[T] {
	return gpui.AnimateKeyframes[I, T](id, keyframes, timing, w, cx)
}

// ---------------------------------------------------------------------------
// MotionTransform (stays in Base: a presentation value bundle)
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Springs (stay in Base: the research split's component half —
// research/browser-parity/02-motion-and-compositing.md §5, "Keyframes
// and springs can stay in components", and the README's table row
// "Presence, stagger, sequence, reveal, springs as tokens →
// Components"; the timing, easing and keyed-transition engine they run
// against is the core's)
// ---------------------------------------------------------------------------

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
	if !springFinite32(ratio) || ratio < 0 {
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
	if !springFinite32(epsilon) || epsilon <= 0 {
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
	frequency := float32(2*math.Pi) / springSecsF32(s.response)
	return float32(frequency * frequency), float32(2 * s.damping * frequency)
}

// springFinite32 is Rust's f32::is_finite: the spring parameter
// validation's own check (the engine's easing validation carries the
// core's copy).
func springFinite32(v float32) bool {
	return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0)
}

// springSecsF32 is Rust's Duration::as_secs_f32: the spring
// integrator's own conversion of a response and an elapsed span.
func springSecsF32(d time.Duration) float32 {
	return float32(d/time.Second) + float32(d%time.Second)/float32(1e9)
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

	if cx.MotionReduced() || !policy.travel || policy.response == 0 {
		state.Update(cx, settle)
		return T(targetPosition)
	}

	// Advance over the frame that just elapsed, which the previous target
	// governed, before adopting the new one (U:motion.rs:672-678). The
	// clock is monotonic and updatedAt is a previous sample's now, so the
	// subtraction cannot go negative.
	elapsed := springSecsF32(now - snapshot.updatedAt)
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
