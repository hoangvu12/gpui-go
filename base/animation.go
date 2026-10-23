package base

// Animation helpers (U:animation.rs): the CSS cubic-bezier solver, the
// cubic easing presets, the Lerp trait and the EffectTransition
// combinator over the core animation element (gpui/animation.go, ticket
// 08; R:elements/animation.rs).
//
// Go adaptations:
//
//   - Lerp is implemented upstream for the foreign types f32, Pixels,
//     Point<Pixels> and Hsla (U:animation.rs:96-132). Go cannot add
//     methods to them, so the trait is the interface Lerp[T] for
//     application types plus LerpValue, which calls the trait method for
//     any T: the core's float32 (f32 and Pixels, which the core represents
//     as float32), gpui.Point and gpui.Hsla are interpolated in place,
//     and any other T must implement Lerp[T].
//   - The deprecated alias `Transition = EffectTransition`
//     (U:animation.rs:252-257) is not ported: in this package Transition
//     is the motion timing policy (U:motion.rs:108), the name the alias was
//     retired to free.
//   - `impl FluentBuilder for EffectTransition` (U:animation.rs:250) is
//     not ported: the core has no FluentBuilder trait.

import (
	"fmt"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

// clamp32 is Rust's f32::clamp: NaN passes through.
func clamp32(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// CubicBezier returns a cubic bezier easing like CSS `cubic-bezier`
// (U:animation.rs:9-63). The input is elapsed progress along x: the
// curve parameter solving x(s) = t is found by Newton iteration with a
// bisection fallback, then y(s) is sampled. Input and output are clamped
// to [0, 1] (the core asserts easing deltas stay in range).
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
			if abs32(err) < 1e-6 {
				return s
			}
			slope := slopeX(s)
			if abs32(slope) < 1e-6 {
				break
			}
			s = clamp32(s-err/slope, 0, 1)
		}

		low, high := float32(0), float32(1)
		s = t
		for range 32 {
			x := sampleX(s)
			if abs32(x-t) < 1e-6 {
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
		t = clamp32(t, 0, 1)
		return clamp32(sampleY(solveS(t)), 0, 1)
	}
}

// EaseOutCubic is the cubic ease-out — fast start, slow end; good for
// enter animations (U:animation.rs:67-71).
func EaseOutCubic(t float32) float32 {
	t = clamp32(t, 0, 1)
	u := 1 - t
	return 1 - u*u*u
}

// EaseInCubic is the cubic ease-in — slow start, fast end; good for exit
// animations (U:animation.rs:73-77).
func EaseInCubic(t float32) float32 {
	t = clamp32(t, 0, 1)
	return t * t * t
}

// EaseInOutCubic is the cubic ease-in-out — slow start and end; good for
// position transitions (U:animation.rs:79-87).
func EaseInOutCubic(t float32) float32 {
	t = clamp32(t, 0, 1)
	if t < 0.5 {
		return 4 * t * t * t
	}
	u := -2*t + 2
	return 1 - u*u*u/2
}

// Lerp is implemented by types that support linear interpolation
// (U:animation.rs:91-94). See LerpValue for the core's types.
type Lerp[T any] interface {
	Lerp(target T, t float32) T
}

// lerp32 is `impl Lerp for f32` (U:animation.rs:96-100). The explicit
// conversion keeps the product rounded on its own (no fused multiply-add),
// as Rust computes it.
func lerp32(from, target, t float32) float32 { return from + float32((target-from)*t) }

// LerpValue interpolates from toward target by t, the Lerp trait method
// for any T: float32 (f32 and Pixels, U:animation.rs:96-108), gpui.Point
// (Point<Pixels>, :110-117) and gpui.Hsla (:119-132) are built in; any
// other T must implement Lerp[T] (it panics otherwise).
//
// Hsla: every channel is interpolated linearly and clamped like the
// pinned hsla() constructor (R:color.rs:49-60). Deviation: the core
// stores the hue as the 0..1 fraction, so the upstream round trip through
// palette's into_degrees (which wraps hues past 0.5 to negative degrees)
// does not apply; intended, as upstream says, for near-grayscale colors.
func LerpValue[T any](from, target T, t float32) T {
	switch f := any(from).(type) {
	case float32:
		return any(lerp32(f, any(target).(float32), t)).(T)
	case gpui.Point:
		to := any(target).(gpui.Point)
		return any(gpui.Point{X: lerp32(f.X, to.X, t), Y: lerp32(f.Y, to.Y, t)}).(T)
	case gpui.Hsla:
		to := any(target).(gpui.Hsla)
		return any(gpui.Hsla{
			H: clamp32(lerp32(f.H, to.H, t), 0, 1),
			S: clamp32(lerp32(f.S, to.S, t), 0, 1),
			L: clamp32(lerp32(f.L, to.L, t), 0, 1),
			A: clamp32(lerp32(f.A, to.A, t), 0, 1),
		}).(T)
	case Lerp[T]:
		return f.Lerp(target, t)
	}
	panic(fmt.Sprintf("base: %T implements neither Lerp nor Interpolate", from))
}

// transitionEffectKind is the TransitionEffect variant
// (U:animation.rs:159-166).
type transitionEffectKind uint8

const (
	effectSlideY transitionEffectKind = iota
	effectSlideX
	effectFade
	effectWidth
	effectHeight
)

type transitionEffect struct {
	kind     transitionEffectKind
	from, to float32
}

// EffectTransition is a composable transition that applies concrete
// fade, slide and size effects to an element (U:animation.rs:136-157).
//
// It is distinct from Transition, a timing policy for a caller-chosen
// value that never picks a visual property. Prefer the motion API for new
// code.
//
//	NewEffectTransition(150 * time.Millisecond).
//		Ease(EaseOutCubic).
//		SlideY(-4, 0).
//		Fade(0, 1).
//		Apply(element, "enter-anim")
type EffectTransition struct {
	Duration time.Duration
	easing   func(float32) float32
	effects  []transitionEffect
}

// NewEffectTransition creates a transition easing out (cubic) over the
// duration (U:animation.rs:169-175).
func NewEffectTransition(duration time.Duration) EffectTransition {
	return EffectTransition{Duration: duration, easing: EaseOutCubic}
}

// Ease sets the easing function (U:animation.rs:177-181).
func (t EffectTransition) Ease(easing func(float32) float32) EffectTransition {
	t.easing = easing
	return t
}

func (t EffectTransition) push(effect transitionEffect) EffectTransition {
	t.effects = append(append([]transitionEffect(nil), t.effects...), effect)
	return t
}

// SlideY animates the vertical offset from from to to (U:animation.rs:183-187).
func (t EffectTransition) SlideY(from, to float32) EffectTransition {
	return t.push(transitionEffect{effectSlideY, from, to})
}

// SlideX animates the horizontal offset from from to to (U:animation.rs:189-193).
func (t EffectTransition) SlideX(from, to float32) EffectTransition {
	return t.push(transitionEffect{effectSlideX, from, to})
}

// Fade animates the opacity from from to to (U:animation.rs:195-199).
func (t EffectTransition) Fade(from, to float32) EffectTransition {
	return t.push(transitionEffect{effectFade, from, to})
}

// Width animates the width from from to to (U:animation.rs:201-205).
func (t EffectTransition) Width(from, to float32) EffectTransition {
	return t.push(transitionEffect{effectWidth, from, to})
}

// Height animates the height from from to to (U:animation.rs:207-211).
func (t EffectTransition) Height(from, to float32) EffectTransition {
	return t.push(transitionEffect{effectHeight, from, to})
}

// EffectTarget is an element Apply can animate: a styled element (it
// embeds gpui.Styled) that converts into an element (U:animation.rs:214
// `E: IntoElement + Styled`).
type EffectTarget[S any] interface {
	StyledElement[S]
	gpui.IntoElement
}

// Apply applies the transition to a styled element, returning the core
// animation element (U:animation.rs:213-247; gpui.WithAnimation is the
// pinned AnimationExt::with_animation, R:elements/animation.rs:73-88).
func (t EffectTransition) Apply[S EffectTarget[S], I gpui.IntoElementID](element S, id I) *gpui.AnimationElement[S] {
	easing := t.easing
	animation := gpui.NewAnimation(t.Duration).WithEasing(func(v float32) float32 { return easing(v) })
	effects := t.effects
	return gpui.WithAnimation(element, id, animation, func(el S, delta float32) S {
		for _, effect := range effects {
			v := lerp32(effect.from, effect.to, delta)
			switch effect.kind {
			case effectSlideY:
				el = el.StyleBase().Top(gpui.Px(v))
			case effectSlideX:
				el = el.StyleBase().Left(gpui.Px(v))
			case effectFade:
				el = el.StyleBase().Opacity(v)
			case effectWidth:
				el = el.StyleBase().W(gpui.Px(v))
			case effectHeight:
				el = el.StyleBase().H(gpui.Px(v))
			}
		}
		return el
	})
}
