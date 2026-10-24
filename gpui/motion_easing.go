package gpui

// CSS-compatible easing policies (browser-core ticket 44; moved from
// base/motion_easing.go, which ports U:motion/easing.rs).
//
// The Chromium rules this implements:
//   - Cubic Bézier: gfx::CubicBezier::Solve solves x(s) = t by Newton's
//     method with a bisection fallback (ui/gfx/geometry/cubic_bezier.cc:
//     181-236; kMaxNewtonIterations = 4 at :18, the bisection loop at
//     :225-234); blink wraps it as CubicBezierTimingFunction::Evaluate
//     (third_party/blink/renderer/platform/animation/timing_function.cc:
//     132-137). The CSS keyword curves are the four cubic-bézier presets
//     (css-timing-1: ease = (0.25, 0.1, 0.25, 1), ease-in = (0.42, 0, 1,
//     1), ease-out = (0, 0, 0.58, 1), ease-in-out = (0.42, 0, 0.58, 1)).
//   - steps(): gfx::StepsTimingFunction::GetValue is
//     floor(steps * t + offset) clamped to [0, jumps] and divided by
//     jumps (ui/gfx/animation/keyframe/timing_function.cc:98-116), with
//     jumps = steps, steps + 1 (jump-both) or steps - 1 (jump-none)
//     (NumberOfJumps, :118-133) and offset = 1 for start/jump-start/
//     jump-both, 0 otherwise (GetStepsStartOffset, :136-150).
//
// Go adaptation: the Easing enum (U:motion/easing.rs:64-85) is a value
// type whose variants are built by functions: the unit variants
// EasingLinear, EasingEase, EasingEaseIn, EasingEaseOut and
// EasingEaseInOut; the data variants CubicBezier, Steps and LinearStops
// only through their checked constructors EasingCubicBezier,
// EasingSteps and EasingLinearStops (Rust also lets a caller build those
// variants unchecked; Go has no public variant syntax to port). The zero
// Easing is Linear; DefaultEasing is the Rust default, EaseOut.

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// StepPosition is the point at which a stepped easing jumps
// (U:motion/easing.rs:5-12; CSS step-position).
type StepPosition uint8

const (
	StepPositionJumpStart StepPosition = iota
	StepPositionJumpEnd
	StepPositionJumpNone
	StepPositionJumpBoth
)

// String is the Debug rendering.
func (p StepPosition) String() string {
	switch p {
	case StepPositionJumpStart:
		return "JumpStart"
	case StepPositionJumpEnd:
		return "JumpEnd"
	case StepPositionJumpNone:
		return "JumpNone"
	case StepPositionJumpBoth:
		return "JumpBoth"
	}
	return fmt.Sprintf("StepPosition(%d)", p)
}

// LinearStop is one output and its optional input position in a CSS-like
// `linear()` curve (U:motion/easing.rs:14-35).
type LinearStop struct {
	Output float32
	// Input is the stop's position; HasInput false is Rust's None.
	Input    float32
	HasInput bool
}

// NewLinearStop is a stop whose position is filled in (U:motion/easing.rs:22-27).
func NewLinearStop(output float32) LinearStop { return LinearStop{Output: output} }

// LinearStopAt is a stop at an explicit input position (U:motion/easing.rs:29-34).
func LinearStopAt(output, input float32) LinearStop {
	return LinearStop{Output: output, Input: input, HasInput: true}
}

// EasingError is an invalid easing configuration (U:motion/easing.rs:37-62).
type EasingError uint8

const (
	// EasingErrorInvalidBezierX is retained for source compatibility.
	//
	// Deprecated: validation reports EasingErrorInvalidBezierControlPoint.
	EasingErrorInvalidBezierX EasingError = iota
	EasingErrorInvalidBezierControlPoint
	EasingErrorInvalidStepCount
	EasingErrorInvalidLinearStops
)

// Error is the Display rendering (U:motion/easing.rs:49-60).
func (e EasingError) Error() string {
	switch e {
	case EasingErrorInvalidBezierX, EasingErrorInvalidBezierControlPoint:
		return "cubic Bézier control points must be finite and x must be within 0..=1"
	case EasingErrorInvalidStepCount:
		return "step easing requires a valid step count"
	case EasingErrorInvalidLinearStops:
		return "linear easing stops are invalid"
	}
	return fmt.Sprintf("EasingError(%d)", e)
}

type easingKind uint8

const (
	easingLinear easingKind = iota
	easingEase
	easingEaseIn
	easingEaseOut
	easingEaseInOut
	easingCubicBezier
	easingSteps
	easingLinearStops
	easingCustom
)

// linearPoint is one validated (input, output) stop.
type linearPoint struct{ input, output float32 }

// Easing is a cheap, copyable CSS-compatible easing policy
// (U:motion/easing.rs:64-85).
type Easing struct {
	kind           easingKind
	x1, y1, x2, y2 float32
	count          uint32
	position       StepPosition
	stops          []linearPoint
	custom         func(float32) float32
}

// EasingLinear is the identity easing.
func EasingLinear() Easing { return Easing{kind: easingLinear} }

// EasingEase is the CSS `ease` keyword.
func EasingEase() Easing { return Easing{kind: easingEase} }

// EasingEaseIn is the CSS `ease-in` keyword.
func EasingEaseIn() Easing { return Easing{kind: easingEaseIn} }

// EasingEaseOut is the CSS `ease-out` keyword.
func EasingEaseOut() Easing { return Easing{kind: easingEaseOut} }

// EasingEaseInOut is the CSS `ease-in-out` keyword.
func EasingEaseInOut() Easing { return Easing{kind: easingEaseInOut} }

// EasingCustom is an application-defined easing function.
func EasingCustom(easing func(float32) float32) Easing {
	return Easing{kind: easingCustom, custom: easing}
}

// DefaultEasing is `Default for Easing`: EaseOut (U:motion/easing.rs:65-71).
func DefaultEasing() Easing { return EasingEaseOut() }

func finite32(v float32) bool {
	return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0)
}

func in01(v float32) bool { return v >= 0 && v <= 1 }

// clampMotion32 is Rust's f32::clamp: NaN passes through.
func clampMotion32(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// lerpMotion32 is `impl Lerp for f32` (U:animation.rs:96-100). The
// explicit conversion keeps the product rounded on its own (no fused
// multiply-add), as Rust computes it.
func lerpMotion32(from, target, t float32) float32 { return from + float32((target-from)*t) }

// EasingCubicBezier is a validated CSS cubic-bezier curve
// (U:motion/easing.rs:114-125): every control point finite and both x
// within 0..=1.
func EasingCubicBezier(x1, y1, x2, y2 float32) (Easing, error) {
	if !finite32(x1) || !finite32(x2) || !in01(x1) || !in01(x2) || !finite32(y1) || !finite32(y2) {
		return Easing{}, EasingErrorInvalidBezierControlPoint
	}
	return Easing{kind: easingCubicBezier, x1: x1, y1: y1, x2: x2, y2: y2}, nil
}

// EasingSteps is a validated stepped easing (U:motion/easing.rs:127-133):
// a nonzero count, and at least two steps for JumpNone.
func EasingSteps(count uint32, position StepPosition) (Easing, error) {
	if count == 0 {
		return Easing{}, EasingErrorInvalidStepCount
	}
	if position == StepPositionJumpNone && count == 1 {
		return Easing{}, EasingErrorInvalidStepCount
	}
	return Easing{kind: easingSteps, count: count, position: position}, nil
}

// EasingLinearStops is a validated CSS `linear()` curve
// (U:motion/easing.rs:135-176): at least two finite stops; the first and
// last default to inputs 0 and 1, runs of omitted inputs are spread evenly
// between their neighbours, and inputs lie in 0..=1 without decreasing.
func EasingLinearStops(stops ...LinearStop) (Easing, error) {
	stops = append([]LinearStop(nil), stops...)
	if len(stops) < 2 {
		return Easing{}, EasingErrorInvalidLinearStops
	}
	for _, stop := range stops {
		if !finite32(stop.Output) || (stop.HasInput && !finite32(stop.Input)) {
			return Easing{}, EasingErrorInvalidLinearStops
		}
	}
	if !stops[0].HasInput {
		stops[0].Input, stops[0].HasInput = 0, true
	}
	last := len(stops) - 1
	if !stops[last].HasInput {
		stops[last].Input, stops[last].HasInput = 1, true
	}

	anchor := 0
	for anchor < last {
		next := -1
		for ix := anchor + 1; ix <= last; ix++ {
			if stops[ix].HasInput {
				next = ix
				break
			}
		}
		if next < 0 {
			return Easing{}, EasingErrorInvalidLinearStops
		}
		from, to := stops[anchor].Input, stops[next].Input
		if !in01(from) || !in01(to) || to < from {
			return Easing{}, EasingErrorInvalidLinearStops
		}
		span := float32(next - anchor)
		for offset := range next - anchor - 1 {
			stop := &stops[anchor+1+offset]
			stop.Input = from + (to-from)*float32(offset+1)/span
			stop.HasInput = true
		}
		anchor = next
	}

	points := make([]linearPoint, len(stops))
	for i, stop := range stops {
		points[i] = linearPoint{input: stop.Input, output: stop.Output}
	}
	return Easing{kind: easingLinearStops, stops: points}, nil
}

// Sample eases progress, clamped to [0, 1] first (U:motion/easing.rs:178-216).
func (e Easing) Sample(progress float32) float32 {
	progress = clampMotion32(progress, 0, 1)
	switch e.kind {
	case easingEase:
		return CubicBezier(0.25, 0.1, 0.25, 1.0)(progress)
	case easingEaseIn:
		return CubicBezier(0.42, 0.0, 1.0, 1.0)(progress)
	case easingEaseOut:
		return CubicBezier(0.0, 0.0, 0.58, 1.0)(progress)
	case easingEaseInOut:
		return CubicBezier(0.42, 0.0, 0.58, 1.0)(progress)
	case easingCubicBezier:
		return CubicBezier(e.x1, e.y1, e.x2, e.y2)(progress)
	case easingSteps:
		count := float32(e.count)
		var jumps, offset float32
		switch e.position {
		case StepPositionJumpStart:
			jumps, offset = count, 1
		case StepPositionJumpEnd:
			jumps, offset = count, 0
		case StepPositionJumpNone:
			jumps, offset = count-1, 0
		default: // JumpBoth
			jumps, offset = count+1, 1
		}
		floor := float32(math.Floor(float64(float32(progress * count))))
		return clampMotion32(floor+offset, 0, jumps) / jumps
	case easingLinearStops:
		stops := e.stops
		// partition_point(|(input, _)| *input <= progress)
		upper := sort.Search(len(stops), func(i int) bool { return stops[i].input > progress })
		if upper == 0 {
			return stops[0].output
		}
		if upper == len(stops) {
			return stops[len(stops)-1].output
		}
		a, b := stops[upper-1], stops[upper]
		if a.input == b.input {
			return b.output
		}
		return a.output + (b.output-a.output)*((progress-a.input)/(b.input-a.input))
	case easingCustom:
		return e.custom(progress)
	}
	return progress // Linear
}

// String is the Debug rendering (U:motion/easing.rs:87-111).
func (e Easing) String() string {
	switch e.kind {
	case easingEase:
		return "Ease"
	case easingEaseIn:
		return "EaseIn"
	case easingEaseOut:
		return "EaseOut"
	case easingEaseInOut:
		return "EaseInOut"
	case easingCubicBezier:
		return fmt.Sprintf("CubicBezier { x1: %v, y1: %v, x2: %v, y2: %v }", e.x1, e.y1, e.x2, e.y2)
	case easingSteps:
		return fmt.Sprintf("Steps { count: %d, position: %v }", e.count, e.position)
	case easingLinearStops:
		parts := make([]string, len(e.stops))
		for i, s := range e.stops {
			parts[i] = fmt.Sprintf("(%v, %v)", s.input, s.output)
		}
		return "LinearStops([" + strings.Join(parts, ", ") + "])"
	case easingCustom:
		return "Custom(..)"
	}
	return "Linear"
}

// ---------------------------------------------------------------------------
// The bezier solver (moved from base/animation.go)
// ---------------------------------------------------------------------------

// absMotion32 is Rust's f32::abs.
func absMotion32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// CubicBezier returns a cubic bezier easing like CSS `cubic-bezier`
// (U:animation.rs:9-63; moved from base). The input is elapsed progress
// along x: the curve parameter solving x(s) = t is found by Newton
// iteration with a bisection fallback, then y(s) is sampled. Input and
// output are clamped to [0, 1] (the core asserts easing deltas stay in
// range).
func CubicBezier(x1, y1, x2, y2 float32) func(float32) float32 {
	// Polynomial form of the unit bezier, where p0 = (0, 0) and p3 = (1, 1).
	cx, cy := 3*x1, 3*y1
	bx, by := float32(3*(x2-x1))-cx, float32(3*(y2-y1))-cy
	ax, ay := 1-cx-bx, 1-cy-by
	sampleX := func(t float32) float32 { return float32(float32(float32(ax*t)+bx)*t+cx) * t }
	sampleY := func(t float32) float32 { return float32(float32(float32(ay*t)+by)*t+cy) * t }
	slopeX := func(t float32) float32 { return float32(float32(float32(3*ax)*t)+float32(2*bx))*t + cx }

	// Solve `x(s) = t` for the curve parameter `s` (U:animation.rs:24-53).
	solveS := func(t float32) float32 {
		s := t
		for range 8 {
			err := sampleX(s) - t
			if absMotion32(err) < 1e-6 {
				return s
			}
			slope := slopeX(s)
			if absMotion32(slope) < 1e-6 {
				break
			}
			s = clampMotion32(s-err/slope, 0, 1)
		}

		low, high := float32(0), float32(1)
		s = t
		for range 32 {
			x := sampleX(s)
			if absMotion32(x-t) < 1e-6 {
				break
			}
			if x < t {
				low = s
			} else {
				high = s
			}
			s = (low + high) / 2
		}
		return s
	}

	return func(t float32) float32 {
		t = clampMotion32(t, 0, 1)
		return clampMotion32(sampleY(solveS(t)), 0, 1)
	}
}

// EaseOutCubic is the cubic ease-out — fast start, slow end; good for
// enter animations (U:animation.rs:67-71; moved from base).
func EaseOutCubic(t float32) float32 {
	t = clampMotion32(t, 0, 1)
	u := 1 - t
	return 1 - u*u*u
}
