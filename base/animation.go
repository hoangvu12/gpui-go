package base

// Animation helpers (U:animation.rs): the cubic easing presets, the
// Lerp trait surface and the EffectTransition combinator over the core
// animation element (gpui/animation.go, ticket 08; R:elements/
// animation.rs).
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
//   - browser-core ticket 44 moved the CSS cubic-bezier solver
//     (CubicBezier), the default ease (EaseOutCubic) and the Lerp value
//     machinery into the core with the motion engine (gpui/
//     motion_easing.go, gpui/motion.go); Base aliases them in motion.go.
//     What stays here is the rest of the U:animation.rs surface plus the
//     private arithmetic helpers Base's own files share.
//   - The deprecated alias `Transition = EffectTransition`
//     (U:animation.rs:252-257) is not ported: in this package Transition
//     is the motion timing policy (U:motion.rs:108), the name the alias was
//     retired to free.
//   - `impl FluentBuilder for EffectTransition` (U:animation.rs:250) is
//     not ported: the core has no FluentBuilder trait.

import (
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

// lerp32 is `impl Lerp for f32` (U:animation.rs:96-100). The explicit
// conversion keeps the product rounded on its own (no fused multiply-add),
// as Rust computes it.
func lerp32(from, target, t float32) float32 { return from + float32((target-from)*t) }

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
