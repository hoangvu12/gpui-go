package gpui

import (
	"errors"
	"math"
)

// This file is the browser-core ticket 40's public gradient surface:
// a two-stop linear gradient Background (CSS linear-gradient,
// css-images-3 §linear-gradients) carried through the pinned
// Background's shape to the rev-15 scene record
// (reference/native/SCENE_ABI.md, GpuiGoSceneBackground). The
// interpolation is CSS's legacy default: premultiplied, in the sRGB
// (encoded) color space (css-images-4 §coloring-gradient-line;
// Chromium maps legacy colors to Skia kSRGB and premultiplies,
// platform/graphics/gradient.cc:247 and :287-291 @ 0d4dcd4f) — exactly
// what the rev-15 shader draws. The record's Oklab arm is the PINNED
// Oklab path (a 2.2-curve, non-premultiplied mix; shaders/common.rs),
// not CSS `in oklab`, so this API does not expose it: see
// ErrGradientInterpolationUnsupported.

// GradientStop is one color stop of a linear gradient (the CSS
// <linear-color-stop>: a color and its position on the gradient line).
type GradientStop struct {
	// Color of the stop.
	Color Hsla
	// Position along the gradient line: 0 is the line's start, 1 its
	// end (a CSS percentage divided by 100). CSS stop fixup is not
	// applied: the two positions must differ — a hard stop (equal
	// positions) and a descending pair are the n-stop form's rules
	// (ticket 52, the R16 gradient redesign) — and the scene kernel
	// rejects the record otherwise.
	Position float32
}

// GradientDirection is the direction a linear gradient's line points
// (css-images-3 §linear-gradient): an explicit angle, or one of the
// CSS keywords. The angle is CSS degrees — 0 points up ("to top") and
// grows clockwise — so 90 is "to right". The corner keywords follow
// CSS's "magic corner" rule: the 50% line passes through the two
// corners of the diagonal quadrant, so the angle depends on the box's
// aspect ratio and is resolved at paint time against the painted
// bounds (Chromium CSSLinearGradientValue::CreateGradient,
// core/css/css_gradient_value.cc:1425-1431 @ 0d4dcd4f: bearing angle
// 90 − atan2(width, height) for `to top right`, mirrored per quadrant).
type GradientDirection uint8

const (
	// GradientDirectionAngle uses the gradient's Angle field.
	GradientDirectionAngle GradientDirection = iota
	// GradientDirectionToTop is `to top` (0deg).
	GradientDirectionToTop
	// GradientDirectionToTopRight is `to top right`.
	GradientDirectionToTopRight
	// GradientDirectionToRight is `to right` (90deg).
	GradientDirectionToRight
	// GradientDirectionToBottomRight is `to bottom right`.
	GradientDirectionToBottomRight
	// GradientDirectionToBottom is `to bottom` (180deg).
	GradientDirectionToBottom
	// GradientDirectionToBottomLeft is `to bottom left`.
	GradientDirectionToBottomLeft
	// GradientDirectionToLeft is `to left` (270deg).
	GradientDirectionToLeft
	// GradientDirectionToTopLeft is `to top left`.
	GradientDirectionToTopLeft
)

// LinearGradient is a two-stop linear gradient (the CSS
// linear-gradient() the rev-15 record carries). The stops interpolate
// premultiplied in the sRGB color space, CSS's legacy default for
// <color> stops.
type LinearGradient struct {
	// Direction of the gradient line.
	Direction GradientDirection
	// Angle in CSS degrees (0 up, clockwise), read when Direction is
	// GradientDirectionAngle.
	Angle float32
	// From and To are the gradient's two stops, in gradient-line
	// order. A stop's color at alpha 0 keeps its hue on the way
	// (premultiplied interpolation), like CSS `transparent` fading a
	// color without going grey.
	From, To GradientStop
}

// BackgroundKind tags which arm a Background carries.
type BackgroundKind uint8

const (
	// BackgroundKindSolid is the flat color arm.
	BackgroundKindSolid BackgroundKind = iota
	// BackgroundKindLinearGradient is the two-stop linear gradient arm.
	BackgroundKindLinearGradient
	// BackgroundKindGradientTable is the scene-level gradient table arm
	// (rev 16, ticket 52: n-stop, radial, conic and repeating
	// gradients - the R15 two-stop record keeps its own arm,
	// bit-exact).
	BackgroundKindGradientTable
	// BackgroundKindCssGradient is the unresolved CSS gradient arm (rev
	// 16, browser-core ticket 57): the n-stop / radial / conic /
	// repeating description resolved at PAINT time into one table
	// entry (Scene.PaintQuad; the kernel insert paths reject it).
	BackgroundKindCssGradient
)

// LinearGradientBackground builds a linear gradient Background with an
// explicit angle (CSS degrees; 0 points up, growing clockwise, 90 is
// to the right). The two stops' positions must differ (CSS hard stops
// need the n-stop form, ticket 52).
func LinearGradientBackground(angle float32, from, to GradientStop) Background {
	return Background{
		Kind: BackgroundKindLinearGradient,
		Gradient: LinearGradient{
			Direction: GradientDirectionAngle,
			Angle:     angle,
			From:      from,
			To:        to,
		},
	}
}

// LinearGradientToBackground builds a linear gradient Background from a
// CSS direction keyword. The axis keywords (to top, to right, …) paint
// at their fixed angles; the corner keywords follow the magic-corner
// rule and resolve against the element's bounds at paint time.
func LinearGradientToBackground(direction GradientDirection, from, to GradientStop) Background {
	return Background{
		Kind: BackgroundKindLinearGradient,
		Gradient: LinearGradient{
			Direction: direction,
			From:      from,
			To:        to,
		},
	}
}

// Gradient errors, distinguishable with errors.Is. Both name the
// recorded limit this revision of the scene record has: exactly two
// stops, interpolated in sRGB. The R16 gradient redesign (browser-core
// ticket 52) owns lifting them.
var (
	// ErrGradientInterpolationUnsupported reports a non-sRGB
	// interpolation request (Tailwind's oklab default and friends):
	// the record's Oklab arm is the pinned 2.2-curve non-premultiplied
	// mix, not CSS `in oklab`, so it is not exposed. Gradients
	// interpolate in sRGB, CSS's legacy default.
	ErrGradientInterpolationUnsupported = errors.New("non-sRGB gradient interpolation is not supported: the record's Oklab arm is the pinned (2.2-curve, non-premultiplied) path, not CSS's (ticket 52, the R16 gradient redesign)")
)

// resolveGradientAngle returns the gradient's CSS angle in degrees
// against a painted box of w x h: the explicit angle as-is, the axis
// keywords at their fixed angles, and the corner keywords at the
// magic-corner angle atan2(h, w) mirrored per quadrant (the 50% line
// passes through the two corners of the diagonal quadrant). The angle
// convention matches the ABI record and the shader: 0 up, clockwise.
func resolveGradientAngle(g *LinearGradient, w, h float32) float32 {
	switch g.Direction {
	case GradientDirectionAngle:
		return g.Angle
	case GradientDirectionToTop:
		return 0
	case GradientDirectionToRight:
		return 90
	case GradientDirectionToBottom:
		return 180
	case GradientDirectionToLeft:
		return 270
	}
	// The magic-corner angle: the gradient line is perpendicular to
	// the box diagonal of the OPPOSITE quadrant, so the 50% line
	// touches the two corners of the target quadrant's diagonal
	// (Chromium computes atan2(rise = width, run = height) and flips
	// it to a bearing angle; css_gradient_value.cc:1425-1431).
	theta := float32(math.Atan2(float64(h), float64(w))) * 180 / math.Pi
	switch g.Direction {
	case GradientDirectionToTopRight:
		return theta
	case GradientDirectionToBottomRight:
		return 180 - theta
	case GradientDirectionToBottomLeft:
		return 180 + theta
	case GradientDirectionToTopLeft:
		return -theta
	}
	return g.Angle
}

// gradientDefaultStops are Tailwind's default gradient stops when only
// a direction class is given: both stops transparent (v4 registers
// --tw-gradient-from and --tw-gradient-to with the initial value
// #0000), at the default positions 0% and 100%.
func gradientDefaultStops() (from, to GradientStop) {
	return GradientStop{Color: Hsla{}, Position: 0}, GradientStop{Color: Hsla{}, Position: 1}
}
