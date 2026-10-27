package gpui

// The FLIP layout-animation helper (browser-core ticket 61): First,
// Last, Invert, Play — the technique FLIP libraries (flip.js, React
// flip-toolkit, Vue's <transition-group>) use to animate layout
// changes on top of a layout engine that never animates them itself.
//
//   - First: every element placed in a Flip records the border box it
//     last showed — the mapped bounds through its own transform
//     (mapBoundsThrough), the geometry getBoundingClientRect reports.
//   - Last: at the next prepaint the element's newly laid-out border
//     box is the target.
//   - Invert: when an ARMED generation changed the box, the element
//     carries the inverse of the change as its CSS transform —
//     Style.Transform, browser-core ticket 56 — so the frame the
//     layout changed in still SHOWS the element at the First box: a
//     translate rides the translation-only fast path (no scene group,
//     transform_paint.go's offset economy), a scale rides the
//     transform effect group.
//   - Play: the motion engine's keyed value transition (ticket 44;
//     TransitionFromWithStatus — the seeding seam Base's presence
//     enters through) eases the inverse to the identity over the
//     group's policy, requesting animation frames while it runs and
//     completing at once under App.MotionReduced. Each frame's
//     prepaint writes the sampled inverse back onto the style, so the
//     element slides from the First box to the Last box and settles
//     with no transform at all.
//
// Arming: a Flip animates only the changes the application arms. Call
// Arm in the update that changes the arrangement (a reorder, an
// insert, a drag drop, or the render that detects one, like zterm's
// shape strings). Every element placed after the arming eases from
// where it last showed; elements placed while unarmed follow their
// layout at once, so a scroll, a window resize or another view's
// change never slides. An element first placed (or placed after its
// state was evicted — it stopped drawing for a frame) adopts its
// position at once, like CSS, which never transitions a
// newly-inserted element's initial style.
//
// Interruption: a second Arm while a move runs starts a fresh
// transition from the element's CURRENT visual position (the First
// box of the new move is the mid-flight sample), which is what FLIP
// libraries do and what Chrome's transition retargeting does
// (css_animations.cc's retarget arm: sample the running transition's
// value, start a new full-duration transition from it). The CSS
// reversing-shortening rule is deliberately not applied — a move back
// toward the previous resting box plays at full duration, like the
// retarget, not shortened like a reversal.
//
// Why the keyed value transition and not the TransitionTransform style
// slot (transition.go): the style diff's before-change value is the
// first style resolution of a frame, at request_layout, which runs
// BEFORE the new layout exists — the inverse is only computable at
// prepaint, against the laid-out border box. FLIP's diff is geometric,
// not stylistic, and needs a transition whose start value the caller
// owns; that is exactly TransitionFromWithStatus's seeding seam. The
// transform list itself is ticket 56's, and the fast path and the
// scene group carry the paint; ticket 60's property-only frames will
// not accelerate FLIP as built (the write happens at prepaint, not
// through the style diff), though the transform group and matrix
// plumbing it exercises is the same seam a property-node update would
// drive.
//
// Identity: the bookkeeping is retained under the element's global id
// path (WithElementState, like the transition style diff's state), so
// a Flip element requires an ID and id paths must be unique — the same
// contract the pinned stateful styles carry. The per-move transitions
// key under the element path too (UseKeyedState inside the placement),
// namespaced by the group's identity and the armed generation, so two
// groups never share a move's state and an interrupted move's state is
// released by the frame-end element-state sweep.

import (
	"strconv"
	"sync/atomic"
	"time"
)

// flipGroupIDs hands each Flip a unique identity for its move keys
// (two groups must never share a keyed transition, even on the same
// element path).
var flipGroupIDs atomic.Uint64

// Flip animates layout changes of the elements placed in it (browser-
// core ticket 61; the file comment is the design). Build one per
// animated arrangement, keep it in the view's state, place elements
// with Div().Flip(f) and call Arm in the update that changes the
// arrangement.
//
// A Flip is window-draw state like a ScrollHandle: use it from the
// render and draw phases of one window's elements; it carries no lock.
type Flip struct {
	// id identifies the group among all Flips (the move keys).
	id uint64
	// policy is a move's timing: the motion engine's Transition (the
	// duration and easing the play step runs on).
	policy Transition
	// scale animates size changes as a scale about the transform-origin
	// (WithScale); off by default, so a move is a pure translate.
	scale bool
	// gen is the armed generation; Arm bumps it, and a placement eases
	// when the generation it last answered is older.
	gen int
}

// NewFlip builds a Flip whose moves take duration, eased by easing
// (the motion engine's transition policy — ticket 44's timing and
// easing machinery). A zero duration completes every move at once.
func NewFlip(duration time.Duration, easing Easing) *Flip {
	return &Flip{id: flipGroupIDs.Add(1), policy: NewTransition(duration).Easing(easing)}
}

// WithScale returns the Flip with size changes animated: a move whose
// box changed size reproduces the First box with a scale about the
// element's transform-origin (the FLIP libraries' optional second
// half) before the translate, and eases both to identity. Off by
// default, so a move is a pure translate — the translation-only fast
// path, no scene group. With scale, a size-changing move rides the
// ticket-56 transform effect group (and an axis whose old or new size
// is zero does not scale: it snaps in size and still translates).
func (f *Flip) WithScale() *Flip {
	f.scale = true
	return f
}

// Arm marks the group's next placements as moves: every element placed
// in this Flip after the call eases from the border box it last showed
// to wherever the next layout puts it. Call it in the update that
// changes the arrangement (before the notify), or in the render that
// detects the change. Elements placed while unarmed follow their
// layout at once.
func (f *Flip) Arm() {
	f.gen++
}

// ---------------------------------------------------------------------------
// The per-element placement (the div prepaint hook)
// ---------------------------------------------------------------------------

// flipElementState is the per-element FLIP bookkeeping, retained under
// the element's global id path: the border box the element last showed
// (the mapped bounds through its transform), the border box it was last
// laid out at, the armed generation it last answered, and the live
// move's generation — the keyed transition it samples, 0 when it is at
// rest.
type flipElementState struct {
	owner   *Flip
	gen     int
	layout  Bounds
	visual  Bounds
	moveGen int
}

// identityFlipOffset is the identity transform as a flipOffset (a zero
// translate and a unit scale); the struct's zero value is NOT it.
var identityFlipOffset = flipOffset{SX: 1, SY: 1}

// flipOffset is the FLIP inverse as one interpolable value: the
// translate (X, Y, logical px) and the scale (SX, SY, factors), eased
// together to the identity by the motion engine's transition.
type flipOffset struct {
	X, Y, SX, SY float32
}

// Lerp implements the engine's interpolation contract (LerpValue's
// Lerp[T] arm): every channel interpolates linearly.
func (o flipOffset) Lerp(target flipOffset, t float32) flipOffset {
	return flipOffset{
		X:  lerpMotion32(o.X, target.X, t),
		Y:  lerpMotion32(o.Y, target.Y, t),
		SX: lerpMotion32(o.SX, target.SX, t),
		SY: lerpMotion32(o.SY, target.SY, t),
	}
}

// place is the div prepaint's FLIP hook (div.go): records the
// element's laid-out border box, starts a move when the armed
// generation changed it, and writes the sampled inverse transform into
// the style — before the transform scope resolves it, so the element's
// own paint, hitbox, children and debug bounds all carry the scope. A
// nil f is the no-Flip path: one pointer check.
func (f *Flip) place(global *GlobalElementID, style *Style, bounds Bounds, rem float32, w *Window, app *App) {
	if f == nil {
		return
	}
	if global == nil {
		panic("gpui: Flip requires an element ID (the FLIP bookkeeping keys its state on element identity, like the Transition style)")
	}
	WithElementState(w, global, func(slot *flipElementState, w *Window) (struct{}, flipElementState) {
		// A fresh element — or one that switched groups — adopts its
		// placement at once (armed is false for a fresh state, whose
		// generation is the current one).
		state := flipElementState{owner: f, gen: f.gen, layout: bounds, visual: bounds}
		if slot != nil && slot.owner == f {
			state = *slot
		}
		armed := state.gen < f.gen
		state.gen = f.gen

		// First → Last → Invert: an armed placement whose LAYOUT box moved
		// starts a move from the inverse of the change — measured against
		// the box the element last SHOWS, which mid-flight is the running
		// move's current visual box (the interruption rule). An armed
		// placement whose layout box did not move leaves a running move
		// alone; one whose box moved ONTO the box it already shows is at
		// rest and drops its move (an ongoing offset would jump it off
		// the box it shows).
		moveGen, from := state.moveGen, identityFlipOffset
		if armed && state.layout != bounds {
			if inverse := flipInverseOf(state.visual, bounds, style, rem, f.scale); inverse != identityFlipOffset {
				moveGen, from = f.gen, inverse
			} else {
				moveGen = 0
			}
		}

		// Play: the motion engine's keyed transition eases the inverse
		// to the identity. A new move seeds a fresh keyed state from
		// its inverse (moveID carries the group and the generation), so
		// an interrupted move's successor starts from the current
		// visual position; later placements sample the same state. The
		// engine requests animation frames while the transition runs
		// and completes it at once under reduced motion. A finished
		// move writes nothing — the element rests on its layout with no
		// transform (a non-linear easing's final sample is within an
		// epsilon of identity, not exactly it).
		if moveGen > 0 {
			move := TransitionFromWithStatus(f.moveID(moveGen), from, identityFlipOffset, f.policy, w, app)
			if move.Status.IsActive() && move.Value != identityFlipOffset {
				writeFlipTransform(style, move.Value)
			}
			if !move.Status.IsActive() {
				moveGen = 0
			}
		}
		state.moveGen = moveGen
		state.layout = bounds

		// The box this element shows this frame — the First box of the
		// next move: the border box mapped through the whole transform
		// list (the FLIP functions this placement wrote plus the
		// element's own).
		state.visual = flipVisualBounds(style, bounds, rem)
		return struct{}{}, state
	})
}

// moveID names the keyed transition of one move of this group: the
// group's identity and the armed generation that started the move. A
// fresh move gets a fresh keyed state; the superseded move's state
// stops being accessed and the frame-end element-state sweep releases
// it.
func (f *Flip) moveID(gen int) TransitionID {
	return TransitionIDWithChannel(NameElementID("flip"), strconv.FormatUint(f.id, 10)+"-"+strconv.Itoa(gen))
}

// flipInverseOf computes the FLIP inverse that maps the laid-out border
// box (last) onto the box the element last showed (first): the inverse
// translate, and with scale the inverse size ratio about the element's
// transform-origin. The composite translate(t) scale(s) about the
// origin p maps the last box's corner c to p + s·(c − p) + t, so with
// a scale the translate carries the origin: t = firstMin − (p +
// s·(lastMin − p)); a pure translate is origin-independent (s = 1
// returns p + (lastMin − p) = lastMin).
func flipInverseOf(first, last Bounds, style *Style, rem float32, scale bool) flipOffset {
	o := flipOffset{
		X:  first.Origin.X - last.Origin.X,
		Y:  first.Origin.Y - last.Origin.Y,
		SX: 1,
		SY: 1,
	}
	if !scale {
		return o
	}
	if last.Size.Width > 0 && first.Size.Width > 0 {
		o.SX = first.Size.Width / last.Size.Width
	}
	if last.Size.Height > 0 && first.Size.Height > 0 {
		o.SY = first.Size.Height / last.Size.Height
	}
	if o.SX == 1 && o.SY == 1 {
		return o
	}
	p := OriginFrom(style.TransformOrigin).point(last, rem)
	o.X = first.Origin.X - (p.X + o.SX*(last.Origin.X-p.X))
	o.Y = first.Origin.Y - (p.Y + o.SY*(last.Origin.Y-p.Y))
	return o
}

// writeFlipTransform prepends the sampled inverse to the element's
// transform list: the FLIP functions compose BEFORE the element's own
// (leftmost), so the inverse translate moves whatever the element's
// own transform produces as a unit. A translate-only inverse rides the
// translation-only fast path; a scaling one rides the effect group.
// The style's own transform list is left in place (the combination is
// rarely intended and only approximate: FLIP measures the un-
// transformed border box; the visual-bounds record self-corrects the
// next move).
func writeFlipTransform(style *Style, o flipOffset) {
	fns := make([]TransformFunction, 0, 2+len(style.Transform))
	fns = append(fns, Translate(DefinitePx(o.X), DefinitePx(o.Y)))
	if o.SX != 1 || o.SY != 1 {
		fns = append(fns, Scale(o.SX, o.SY))
	}
	style.Transform = append(fns, style.Transform...)
}

// flipVisualBounds is the border box the element shows this frame: the
// bounds mapped through the whole transform list (the FLIP functions
// plus the element's own) — the mapped-bounds helper, the AABB
// getBoundingClientRect reports. A singular matrix (a collapsed
// element) falls back to the laid-out box.
func flipVisualBounds(style *Style, bounds Bounds, rem float32) Bounds {
	if len(style.Transform) == 0 {
		return bounds
	}
	if m, ok := transformMatrixOf(style.Transform, style.TransformOrigin, bounds, rem); ok {
		return mapBoundsThrough(m, bounds)
	}
	return bounds
}
