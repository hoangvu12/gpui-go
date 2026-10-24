package gpui

// The Transition style property and its per-element style diff
// (browser-core ticket 44; spec stories 79-81, research/browser-parity
// 02 §5).
//
// CSS starts transitions from style changes: Chromium's
// CSSAnimations::CalculateTransitionUpdate (third_party/blink/renderer/
// core/animation/css/css_animations.cc:3105) diffs the before-change and
// after-change styles (:3062-3075) and starts a transition per changed
// property covered by a rule; a rule whose combined delay and duration
// is not positive starts nothing and unstarts a prior update (:2870-
// 2885, "If we have multiple transitions on the same property, we will
// use the last one"); the reversal rule is the reversing-adjusted start
// value and shortening factor (:2890-2912); the properties that can
// transition are the interpolable ones (core/css/properties/
// css_property.h:87 IsInterpolable, flag kInterpolable at :217), with
// the two-keyframe effect model built from the interpolable values
// (css_animations.cc:2918-2940).
//
// This port's model: every element resolves its style up to three times
// per frame (request_layout, prepaint, paint); the first resolution of
// a frame runs the diff once (keyed on the frame's draw count), and the
// later phases sample the same runs. The animated values override the
// resolved style — layout properties (width) feed the layout node, paint
// properties (opacity, backgrounds, borders) the paint. The runs live in
// the element's retained state (element.go WithElementState) keyed by
// the global element id, so a Transition requires an element ID, like
// the pinned stateful interactive styles. Elements without a Transition
// pay nothing: the resolution is one length check.
//
// Reduced motion completes transitions immediately: the App's flag plus
// the live OS preference through ticket 27's SystemPreferences (the same
// gate the moved engine applies; Chromium turns compositor animations
// off under forced reduced motion, compositor_animations.cc:606-607).
//
// Frames: a run requests animation frames while it is delayed or
// running (RequestAnimationFrame, the notified view re-renders), and
// requests nothing once finished. Known boundary: an element inside a
// CachedView does not re-run the diff while its view is cached, so a
// transition freezes mid-flight there; that is the property-only-frame
// fast path's problem (research 02 §2), not this engine's.

import (
	"time"
)

// TransitionProperty is the style property a TransitionRule animates
// (the port's interpolable set, see the file comment). CSS's
// transition-property names the property; the pin has no element
// transforms yet (research 02 gap 4), so the size pair stands in for
// them.
type TransitionProperty uint8

const (
	// TransitionAll covers every interpolable property (CSS
	// `transition-property: all`, the shorthand default).
	TransitionAll TransitionProperty = iota
	// TransitionOpacity animates Opacity (CSS opacity).
	TransitionOpacity
	// TransitionBackground animates a solid Background (CSS
	// background-color).
	TransitionBackground
	// TransitionWidth animates the resolved width (CSS width, definite
	// lengths only: auto and percentages do not interpolate here).
	TransitionWidth
	// TransitionHeight animates the resolved height (CSS height).
	TransitionHeight
	// TransitionBorderColor animates a solid BorderColor (CSS
	// border-color).
	TransitionBorderColor
)

// transitionSlotCount is the number of concrete animated properties
// (TransitionAll is not a slot; it covers them all).
const transitionSlotCount = 5

// String renders the CSS spelling of the property.
func (p TransitionProperty) String() string {
	switch p {
	case TransitionAll:
		return "all"
	case TransitionOpacity:
		return "opacity"
	case TransitionBackground:
		return "background-color"
	case TransitionWidth:
		return "width"
	case TransitionHeight:
		return "height"
	case TransitionBorderColor:
		return "border-color"
	}
	return "all"
}

// slot maps a concrete property to its run slot; TransitionAll and the
// unknown values answer false (callers fan All out over every slot).
func (p TransitionProperty) slot() (int, bool) {
	switch p {
	case TransitionOpacity:
		return 0, true
	case TransitionBackground:
		return 1, true
	case TransitionWidth:
		return 2, true
	case TransitionHeight:
		return 3, true
	case TransitionBorderColor:
		return 4, true
	}
	return 0, false
}

// slotProperty is the concrete property of a slot.
func slotProperty(slot int) TransitionProperty {
	return []TransitionProperty{TransitionOpacity, TransitionBackground, TransitionWidth, TransitionHeight, TransitionBorderColor}[slot]
}

// TransitionRule is one entry of the transition style property — CSS
// `transition: <property> <duration> <timing-function> <delay>`. The
// zero rule is TransitionAll over no time (inert). Build one with
// NewTransitionRule; the last matching rule for a property wins
// (css_animations.cc:2870-2873).
type TransitionRule struct {
	// Property is the animated property.
	Property TransitionProperty
	// Duration is the transition's duration (CSS transition-duration;
	// non-negative).
	Duration time.Duration
	// Delay is the transition's delay (CSS transition-delay); a negative
	// delay starts the transition inside its active interval.
	Delay SignedDuration
	// Easing is the timing function (CSS transition-timing-function).
	// The zero Easing is Linear.
	Easing Easing
}

// NewTransitionRule builds a rule over duration for property with the
// CSS shorthand defaults: the `ease` timing function and no delay.
func NewTransitionRule(property TransitionProperty, duration time.Duration) TransitionRule {
	return TransitionRule{Property: property, Duration: duration, Easing: EasingEase()}
}

// WithDelay returns the rule with the delay.
func (r TransitionRule) WithDelay(delay SignedDuration) TransitionRule {
	r.Delay = delay
	return r
}

// WithEasing returns the rule with the easing.
func (r TransitionRule) WithEasing(easing Easing) TransitionRule {
	r.Easing = easing
	return r
}

// combined is the delay plus duration (css_animations.cc:2878-2885: a
// transition whose start delay plus duration is not positive starts
// nothing).
func (r TransitionRule) combined() time.Duration {
	d, negative := r.Delay.Duration()
	if negative {
		d = -d
	}
	return d + r.Duration
}

// matchTransitionRule returns the rule covering property: the LAST rule
// whose Property is the property or All, matching Chromium's iteration
// order (css_animations.cc:2870-2873).
func matchTransitionRule(rules []TransitionRule, property TransitionProperty) (TransitionRule, bool) {
	var found TransitionRule
	ok := false
	for _, rule := range rules {
		if rule.Property == property || rule.Property == TransitionAll {
			found, ok = rule, true
		}
	}
	return found, ok
}

// ---------------------------------------------------------------------------
// The animated values
// ---------------------------------------------------------------------------

// animatedValue is one animatable style value in its resolved form: a
// number (opacity, a length in logical pixels) or a colour. A value that
// does not interpolate (an absent property, a gradient, a percentage
// length) reports interpolable false, and the property applies discretely.
type animatedValue struct {
	num   float32
	color Hsla
}

// readAnimated resolves property out of style (rem resolves rem lengths).
func readAnimated(property TransitionProperty, style *Style, rem float32) (animatedValue, bool) {
	switch property {
	case TransitionOpacity:
		// CSS's initial opacity is 1.
		v := float32(1)
		if style.Opacity != nil {
			v = *style.Opacity
		}
		return animatedValue{num: v}, true
	case TransitionBackground:
		// Only solid backgrounds interpolate (a gradient or an absent
		// background is discrete); an absent background transitions from
		// transparent black, the CSS initial color.
		if style.Background != nil && style.Background.Kind == BackgroundKindSolid {
			return animatedValue{color: style.Background.Solid}, true
		}
		if style.Background == nil {
			return animatedValue{color: TransparentBlack()}, true
		}
		return animatedValue{}, false
	case TransitionBorderColor:
		if style.BorderColor != nil && style.BorderColor.Kind == BackgroundKindSolid {
			return animatedValue{color: style.BorderColor.Solid}, true
		}
		if style.BorderColor == nil {
			return animatedValue{color: TransparentBlack()}, true
		}
		return animatedValue{}, false
	case TransitionWidth, TransitionHeight:
		length := style.Size.Width
		if property == TransitionHeight {
			length = style.Size.Height
		}
		if px, ok := definiteLengthPixels(length.Definite, rem); ok {
			return animatedValue{num: px}, true
		}
		return animatedValue{}, false
	}
	return animatedValue{}, false
}

// definiteLengthPixels resolves a definite length to logical pixels; a
// percentage length does not interpolate here (CSS interpolates it as a
// calc() mix, which the port has no scene representation for yet).
func definiteLengthPixels(length DefiniteLength, rem float32) (float32, bool) {
	switch length.Kind {
	case DefiniteLengthPx:
		return length.Value, true
	case DefiniteLengthRem:
		return length.Value * rem, true
	default:
		return 0, false
	}
}

// equal compares two animated values of the same property.
func (v animatedValue) equal(o animatedValue) bool { return v == o }

// lerp interpolates: numbers linearly; colours channel-wise through
// gamma-encoded sRGB (CSS Color 4's sRGB interpolation of legacy
// colors — the classic rgb() midpoint; Blink's AnimatableColor lerps
// the SkColor4f channels), NOT through HSL, whose midpoints differ
// visibly (red→blue at 0.5 is dark purple in sRGB, bright magenta in
// HSL).
func (v animatedValue) lerp(property TransitionProperty, o animatedValue, t float32) animatedValue {
	switch property {
	case TransitionBackground, TransitionBorderColor:
		from, to := hslaToRgba(v.color), hslaToRgba(o.color)
		return animatedValue{color: rgbaToHsla(Rgba{
			R: lerpMotion32(from.R, to.R, t),
			G: lerpMotion32(from.G, to.G, t),
			B: lerpMotion32(from.B, to.B, t),
			A: lerpMotion32(from.A, to.A, t),
		})}
	default:
		return animatedValue{num: lerpMotion32(v.num, o.num, t)}
	}
}

// writeAnimated applies v to style's property. A written length is a
// resolved px length; a written color is a solid background.
func writeAnimated(property TransitionProperty, style *Style, v animatedValue) {
	switch property {
	case TransitionOpacity:
		op := v.num
		style.Opacity = &op
	case TransitionBackground:
		if style.Background == nil {
			style.Background = &Background{}
		}
		style.Background.Kind = BackgroundKindSolid
		style.Background.Solid = v.color
	case TransitionBorderColor:
		if style.BorderColor == nil {
			style.BorderColor = &Background{}
		}
		style.BorderColor.Kind = BackgroundKindSolid
		style.BorderColor.Solid = v.color
	case TransitionWidth:
		style.Size.Width = Length{Definite: DefiniteLength{Kind: DefiniteLengthPx, Value: v.num}}
	case TransitionHeight:
		style.Size.Height = Length{Definite: DefiniteLength{Kind: DefiniteLengthPx, Value: v.num}}
	}
}

// ---------------------------------------------------------------------------
// The per-element run state
// ---------------------------------------------------------------------------

// styleTransitionRun is one active transition on one property of one
// element: the two endpoints, the start instant and the CSS reversing
// bookkeeping (css-transitions-1 §starting).
type styleTransitionRun struct {
	from, to animatedValue
	// slotProperty is the concrete animated property (a TransitionAll
	// rule creates one run per slot, so the run remembers its slot).
	slotProperty TransitionProperty
	// reversingAdjustedStart is the value a reversal back toward
	// compares against (Chromium's reversing_adjusted_start_value,
	// css_animations.cc:2890-2892, 2903).
	reversingAdjustedStart animatedValue
	// shortening is the run's reversing shortening factor (1 for a plain
	// transition).
	shortening float32
	startedAt  time.Duration
	duration   time.Duration
	rule       TransitionRule
}

// sample evaluates the run at elapsed since its start: the delay holds
// the from value, the easing shapes the active interval, and a finished
// run presents its target. The returned flag reports an active (delayed
// or running) transition that still needs frames.
func (r *styleTransitionRun) sample(elapsed time.Duration) (animatedValue, bool) {
	active, ok := r.rule.Delay.activeElapsed(elapsed)
	if !ok {
		return r.from, true
	}
	if r.duration == 0 || active >= r.duration {
		return r.to, false
	}
	progress := secsF32(active) / secsF32(r.duration)
	eased := r.rule.Easing.Sample(progress)
	return r.from.lerp(r.slotProperty, r.to, eased), true
}

// slotPropertyOfRun is the run's animated property. The run stores its
// rule's property; TransitionAll runs are created per concrete slot, so
// the slot is recoverable from the value kind via the creation-time
// field below.
func slotPropertyOfRun(r *styleTransitionRun) TransitionProperty { return r.slotProperty }

// easedProgress is the run's eased progress at elapsed — the value
// Chromium's reversal rule consumes as effect->Progress() (the
// TRANSFORMED progress through the timing function, timing.cc:226-235,
// read at css_animations.cc:2895-2899).
func (r *styleTransitionRun) easedProgress(elapsed time.Duration) float32 {
	active, ok := r.rule.Delay.activeElapsed(elapsed)
	if !ok || r.duration == 0 {
		return 0
	}
	if active >= r.duration {
		return 1
	}
	return r.rule.Easing.Sample(secsF32(active) / secsF32(r.duration))
}

// transitionElementState is the retained per-element state of the style
// diff: the last diff's frame, the after-change values it saw, the
// active runs, and the frame the element last requested animation
// frames for (one request per frame, not one per style resolution).
// It lives in the window's element state arena keyed by the element's
// global id, one per element (never per property map — the slots are a
// fixed array).
type transitionElementState struct {
	frame          uint64
	requestedFrame uint64
	targets        [transitionSlotCount]animatedValue
	has            [transitionSlotCount]bool
	runs           [transitionSlotCount]*styleTransitionRun
}

// ---------------------------------------------------------------------------
// The resolution
// ---------------------------------------------------------------------------

// resolveStyleTransitions applies the style's Transition rules to the
// resolved style: the first resolution of a frame diffs the animated
// properties against the element's last after-change values, starting,
// retargeting or reversing runs (with the CSS reversing rule), and the
// later resolutions sample the same runs. Animated values are written
// back into the returned style — width and height as resolved px
// lengths (feeding the layout node), opacity, backgrounds and border
// colors for paint.
//
// Elements without a Transition pay one length check here. A Transition
// on an element without an ID panics: the style diff keys its state on
// element identity, exactly like the pinned stateful interactive
// styles. Reduced motion (App.ReduceMotion or the OS preference through
// SystemPreferences) completes every transition immediately.
func resolveStyleTransitions(global *GlobalElementID, style Style, w *Window, app *App) Style {
	if len(style.Transition) == 0 {
		return style
	}
	if global == nil {
		panic("gpui: Transition style requires an element ID (the per-element style diff keys its state on element identity, like the pinned stateful interactive styles)")
	}
	if w == nil || app == nil {
		return style
	}
	now := app.Now()
	rem := RemSize(w)
	draws := drawState(w).draws

	return WithElementState(w, global, func(slot *transitionElementState, w *Window) (Style, transitionElementState) {
		next := transitionElementState{}
		if slot == nil {
			// The element's first frame: adopt the current values as the
			// before-change style with no runs — CSS does not transition
			// a newly-inserted element's initial style.
			for i := range transitionSlotCount {
				v, ok := readAnimated(slotProperty(i), &style, rem)
				next.targets[i], next.has[i] = v, ok
			}
			next.frame = draws
			return style, next
		}
		next = *slot
		if next.frame != draws {
			diffTransitions(&next, &style, now, rem, app)
			next.frame = draws
		}

		// Reduced motion: no runs, the target applies at once (Chromium's
		// forced reduce-motion turns animations off; the port's gate is
		// the App flag plus the OS preference).
		if reduceMotionActive(app) {
			return style, next
		}

		// Sample the runs onto the style. A finished run's target equals
		// the style's own value, so writing it is the identity.
		animated := false
		for i := range transitionSlotCount {
			run := next.runs[i]
			if run == nil {
				continue
			}
			value, active := run.sample(satSubDuration(now, run.startedAt))
			writeAnimated(slotProperty(i), &style, value)
			if active {
				animated = true
			}
		}
		// One frame request per frame: the three style resolutions of a
		// frame (request_layout, prepaint, paint) share it.
		if animated && next.requestedFrame != draws {
			next.requestedFrame = draws
			RequestAnimationFrame(w)
		}
		return style, next
	})
}

// diffTransitions is the once-per-frame diff: for every concrete
// property, compare the resolved after-change value with the last
// frame's; a change covered by a rule starts, retargets or reverses a
// run (css_animations.cc:2890-2912).
func diffTransitions(state *transitionElementState, style *Style, now time.Duration, rem float32, app *App) {
	for i := range transitionSlotCount {
		property := slotProperty(i)
		target, interpolable := readAnimated(property, style, rem)
		rule, hasRule := matchTransitionRule(style.Transition, property)

		// No rule, a non-interpolable value, or a combined duration that
		// is not positive: the value applies without a run
		// (css_animations.cc:2878-2885, and an unstarted update unstarts
		// a prior one).
		if !interpolable || !hasRule || rule.combined() <= 0 {
			state.runs[i] = nil
			state.targets[i], state.has[i] = target, interpolable
			continue
		}

		// An unchanged value keeps a running run; a finished run is
		// dropped (Chromium cancels finished transitions) and is NOT
		// re-started while the value stays at its target.
		unchanged := state.has[i] && target.equal(state.targets[i])
		run := state.runs[i]
		if run != nil {
			if _, active := run.sample(satSubDuration(now, run.startedAt)); !active {
				state.runs[i] = nil
				run = nil
			}
		}
		if unchanged {
			state.targets[i], state.has[i] = target, interpolable
			continue
		}
		if run == nil {
			// A plain start: from the before-change value, a factor of 1
			// and the reversing-adjusted start value = the before-change
			// value (css_animations.cc:2890-2892).
			state.runs[i] = &styleTransitionRun{
				from:                   state.targets[i],
				to:                     target,
				reversingAdjustedStart: state.targets[i],
				shortening:             1,
				startedAt:              now,
				duration:               rule.Duration,
				rule:                   rule,
				slotProperty:           property,
			}
			state.targets[i], state.has[i] = target, true
			continue
		}

		// A change while a run is active: sample the before-change value
		// (the running transition's current value), then retarget. A
		// reversal — the new target equals the run's
		// reversing-adjusted start value — shortens by the eased progress
		// (css_animations.cc:2893-2912): factor = easedProgress × old
		// factor + (1 − old factor), the new duration is the rule's
		// duration × factor, a negative delay scales by the factor, and
		// the new reversing-adjusted start is the interrupted run's
		// target.
		current, _ := run.sample(satSubDuration(now, run.startedAt))
		adjustedRule := rule
		adjustedStart := current
		shortening := float32(1)
		if target.equal(run.reversingAdjustedStart) {
			adjustedStart = run.to
			shortening = clampMotion32(
				run.easedProgress(satSubDuration(now, run.startedAt))*run.shortening+(1-run.shortening), 0, 1)
			adjustedRule.Duration = mulF32(rule.Duration, shortening)
			if d, negative := rule.Delay.Duration(); negative {
				adjustedRule.Delay = SignedDurationNegative(mulF32(d, shortening))
			}
		}
		state.runs[i] = &styleTransitionRun{
			from:                   current,
			to:                     target,
			reversingAdjustedStart: adjustedStart,
			shortening:             shortening,
			startedAt:              now,
			duration:               adjustedRule.Duration,
			rule:                   adjustedRule,
			slotProperty:           property,
		}
		state.targets[i], state.has[i] = target, true
	}
}
