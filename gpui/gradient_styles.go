package gpui

import (
	"errors"
	"math"
)

// This file is browser-core ticket 57's CSS-gradient styling: the
// n-stop / radial / conic / repeating gradient description the style
// layer carries (the CSS <gradient> of css-images-3/-4), resolved at
// PAINT time against the element's device bounds into one scene
// gradient table entry (rev 16, ticket 52's InsertGradient +
// TableGradientBackground). The two-stop linear gradient keeps the R15
// record arm bit-exact (gpui/gradient.go); this arm is for everything
// the two-stop record cannot express: a third stop (Tailwind's via-*),
// hard stops (equal positions), radial and conic shapes, and the
// repeating tile.
//
// The CSS rules implemented here are Chromium's (0d4dcd4f):
//   - the linear gradient line: the endpoints where the line through
//     the box center at the CSS angle (0 up, clockwise) exits the box —
//     |W·sinθ|+|H·cosθ| long, EndPointsFromAngle,
//     core/css/css_gradient_value.cc:1284-1350;
//   - the radial end shape: RadiusToSide / RadiusToCorner with the
//     farthest-corner ellipse default (css_gradient_value.cc:1848-1990
//     — the ellipse keeps the closest-/farthest-SIDE aspect ratio and
//     is scaled through the corner, EllipseRadius);
//   - the conic sweep: clockwise from up, the from-angle measured from
//     up (css_gradient_value.cc:2246-2279);
//   - the color-stop fixup: unspecified end stops 0/1, a specified
//     position never descending below the previous specified one,
//     unspecified middle stops evenly spaced, then the whole list
//     normalized to [0,1] over its span with the geometry's domain
//     contracted to match (AddStops + NormalizeAndAddStops,
//     css_gradient_value.cc:658-830) — the stops a repeating gradient
//     tiles over.
//
// Interpolation is CSS's legacy default — premultiplied sRGB, exactly
// what the native ramp bake does (bake_gradient_ramp, scene.rs:4270) —
// so no interpolation method is exposed (see
// ErrGradientInterpolationUnsupported).

// CssGradientKind is a CSS gradient's shape (css-images-3).
type CssGradientKind uint8

const (
	// CssGradientLinear is linear-gradient (the default).
	CssGradientLinear CssGradientKind = iota
	// CssGradientRadial is radial-gradient.
	CssGradientRadial
	// CssGradientConic is conic-gradient.
	CssGradientConic
)

// RadialShape is a radial gradient's end shape (css-images-3
// §radial-gradient: circle or ellipse, the default).
type RadialShape uint8

const (
	// RadialShapeEllipse is the ellipse end shape (the CSS default).
	RadialShapeEllipse RadialShape = iota
	// RadialShapeCircle is the circle end shape.
	RadialShapeCircle
)

// RadialExtent is a radial gradient's extent keyword (css-images-3
// §radial-gradient; farthest-corner is the default, CSS `cover`, and
// `contain` is closest-side).
type RadialExtent uint8

const (
	// RadialExtentFarthestCorner sizes the end shape to the farthest
	// corner (the CSS default).
	RadialExtentFarthestCorner RadialExtent = iota
	// RadialExtentClosestSide sizes the end shape to the closest side
	// (CSS contain).
	RadialExtentClosestSide
	// RadialExtentFarthestSide sizes the end shape to the farthest side.
	RadialExtentFarthestSide
	// RadialExtentClosestCorner sizes the end shape to the closest
	// corner.
	RadialExtentClosestCorner
)

// CssGradient is the CSS <gradient> the style layer carries
// (browser-core ticket 57): the shape, the stops and the repeating
// tile, with every length relative to the gradient box (the element's
// border box) resolved at paint time. A GradientStop's NaN Position is
// CSS's unspecified position — the first and last unspecified stops
// resolve to 0 and 1 and middle ones distribute evenly between the
// surrounding specified stops (Chromium's AddStops rule); positions
// are otherwise CSS numbers (a percentage divided by 100, negatives
// legal).
type CssGradient struct {
	// Kind of the gradient's shape.
	Kind CssGradientKind
	// Repeating tiles the gradient over the stop span (CSS
	// repeating-*).
	Repeating bool
	// Direction of the linear gradient line (GradientDirectionAngle
	// with Angle for an explicit CSS angle; the corner keywords apply
	// the magic-corner rule at paint time). Linear only.
	Direction GradientDirection
	// Angle in CSS degrees (0 up, clockwise), read when Direction is
	// GradientDirectionAngle. Linear only.
	Angle float32
	// Shape of the radial end shape. Radial only.
	Shape RadialShape
	// Extent of the radial end shape. Radial only.
	Extent RadialExtent
	// CenterX/CenterY locate the radial and conic center as fractions
	// of the box. The constructors default to (0.5, 0.5) — CSS's `at
	// center`; a hand-built zero value is the top-left corner (`at 0%
	// 0%`). Radial and conic only.
	CenterX, CenterY float32
	// FromAngle is the conic start angle in CSS degrees, clockwise
	// from up (0 — CSS's `from 0deg`). Conic only.
	FromAngle float32
	// Stops in CSS order (1..=64; above 64 the insert resamples down
	// with a debug warning).
	Stops []GradientStop
}

// Gradient errors of the n-stop arm, distinguishable with errors.Is.
var (
	// ErrGradientStops reports a gradient without the stop count the
	// shape needs.
	ErrGradientStops = errors.New("a gradient needs at least one color stop")
	// ErrGradientDegenerate reports a gradient whose resolved geometry
	// is degenerate (a zero-size box, or a stop span of zero length
	// the repeating tile cannot wrap).
	ErrGradientDegenerate = errors.New("the gradient's geometry is degenerate at this size (a zero-size gradient box or a zero-length stop span)")
)

// LinearGradientStopsBackground builds an n-stop linear gradient with
// an explicit CSS angle (0 up, clockwise, 90 to the right): the
// CSS linear-gradient(<angle>, <stops>) of ticket 57's arm — resolved
// through the scene gradient table, unlike the two-stop
// LinearGradientBackground whose R15 record stays bit-exact.
func LinearGradientStopsBackground(angle float32, stops ...GradientStop) Background {
	return Background{Kind: BackgroundKindCssGradient, CssGradient: &CssGradient{
		Kind:      CssGradientLinear,
		Direction: GradientDirectionAngle,
		Angle:     angle,
		Stops:     cloneGradientStops(stops),
	}}
}

// LinearGradientToStopsBackground builds an n-stop linear gradient
// from a CSS direction keyword (the axis keywords at their fixed
// angles, the corner keywords at the magic-corner angle resolved at
// paint time against the element's bounds).
func LinearGradientToStopsBackground(direction GradientDirection, stops ...GradientStop) Background {
	return Background{Kind: BackgroundKindCssGradient, CssGradient: &CssGradient{
		Kind:      CssGradientLinear,
		Direction: direction,
		Stops:     cloneGradientStops(stops),
	}}
}

// RadialGradientBackground builds an n-stop radial gradient
// (CSS radial-gradient(<shape> <extent>, <stops>); shape/extent are
// the CSS defaults when passed as their zero values — ellipse
// farthest-corner). The center is the box center; see CssGradient for
// the other center spellings.
func RadialGradientBackground(shape RadialShape, extent RadialExtent, stops ...GradientStop) Background {
	return Background{Kind: BackgroundKindCssGradient, CssGradient: &CssGradient{
		Kind:    CssGradientRadial,
		Shape:   shape,
		Extent:  extent,
		CenterX: 0.5,
		CenterY: 0.5,
		Stops:   cloneGradientStops(stops),
	}}
}

// ConicGradientBackground builds an n-stop conic gradient (CSS
// conic-gradient(from <angle>, <stops>), the sweep clockwise from up,
// the angle in degrees).
func ConicGradientBackground(fromDegrees float32, stops ...GradientStop) Background {
	return Background{Kind: BackgroundKindCssGradient, CssGradient: &CssGradient{
		Kind:      CssGradientConic,
		FromAngle: fromDegrees,
		CenterX:   0.5,
		CenterY:   0.5,
		Stops:     cloneGradientStops(stops),
	}}
}

// Repeating is the CSS repeating-* form: the returned background tiles
// the gradient over its stop span instead of padding the end colors.
func (b Background) Repeating() Background {
	if b.Kind != BackgroundKindCssGradient || b.CssGradient == nil {
		return b
	}
	g := *b.CssGradient
	g.Repeating = true
	return Background{Kind: BackgroundKindCssGradient, CssGradient: &g}
}

// cloneGradientStops copies a variadic stop list into the value the
// background owns (the class layer's stop mutations never alias a
// caller's slice).
func cloneGradientStops(stops []GradientStop) []GradientStop {
	if len(stops) == 0 {
		return nil
	}
	out := make([]GradientStop, len(stops))
	copy(out, stops)
	return out
}

// ---------------------------------------------------------------------------
// The CSS color-stop fixup (Chromium css_gradient_value.cc:658-830)
// ---------------------------------------------------------------------------

// fixedGradientStops is a fixed stop list: CSS stop fixup applied
// (unspecified ends at 0/1, no descending positions, unspecified
// middles evenly spaced), plus the span the stops cover.
type fixedGradientStops struct {
	// stops in position order, all positions finite.
	stops []GradientStop
	// first and last are the first/last stop positions (the span the
	// geometry's domain contracts to when they are not 0 and 1).
	first, last float32
}

// fixGradientStops applies CSS's color-stop fixup to a stop list
// (Chromium CSSGradientValue::AddStops, css_gradient_value.cc:658-788):
// a stop's negative position is unspecified — the first and last
// unspecified stops sit at 0 and 1; a specified position never comes
// before the largest specified position before it; and each run of
// unspecified middle stops distributes evenly between the surrounding
// specified positions.
func fixGradientStops(stops []GradientStop) fixedGradientStops {
	n := len(stops)
	if n == 0 {
		return fixedGradientStops{}
	}
	specified := make([]bool, n)
	positions := make([]float32, n)
	for i, stop := range stops {
		if math.IsNaN(float64(stop.Position)) {
			// NaN is CSS's unspecified position; negative positions are
			// legal CSS (a stop before the gradient line's start).
			continue
		}
		specified[i] = true
		positions[i] = stop.Position
	}
	// The first and last stops default to 0% and 100% (Chromium sets
	// them unconditionally — an explicitly positioned first stop keeps
	// its own position).
	if !specified[0] {
		positions[0] = 0
		specified[0] = true
	}
	if n > 1 && !specified[n-1] {
		positions[n-1] = 1
		specified[n-1] = true
	}
	// A specified position never descends below the largest specified
	// position before it (css_gradient_value.cc:731-746).
	prev := 0
	for i := 1; i < n; i++ {
		if specified[i] {
			if positions[i] < positions[prev] {
				positions[i] = positions[prev]
			}
			prev = i
		}
	}
	// Unspecified middle stops distribute evenly between the
	// surrounding specified positions (css_gradient_value.cc:759-786).
	runStart := -1
	for i := 0; i < n; i++ {
		if !specified[i] {
			if runStart < 0 {
				runStart = i
			}
			continue
		}
		if runStart < 0 {
			continue
		}
		runEnd := i
		if runStart < runEnd {
			lastSpecified := positions[runStart-1]
			nextSpecified := positions[runEnd]
			delta := (nextSpecified - lastSpecified) / float32(runEnd-runStart+1)
			for j := runStart; j < runEnd; j++ {
				positions[j] = lastSpecified + float32(j-runStart+1)*delta
			}
		}
		runStart = -1
	}
	out := make([]GradientStop, n)
	for i, stop := range stops {
		out[i] = GradientStop{Color: stop.Color, Position: positions[i]}
	}
	return fixedGradientStops{stops: out, first: positions[0], last: positions[n-1]}
}

// normalize maps the fixed stops onto [0, 1] over their span (Chromium
// NormalizeAndAddStops, css_gradient_value.cc:508-558): each position
// becomes (p - first)/(last - first). A zero span (all stops
// coincident) does not normalize: the hard edge sits at the stops' own
// position for a padded gradient, and a repeating gradient is solid
// (the last stop's color) — the geometry keeps its full domain
// (degenerate reports it).
func (f fixedGradientStops) normalize() (stops []TableGradientStop, degenerate bool) {
	span := f.last - f.first
	if span <= 0 || math.IsNaN(float64(span)) {
		last := f.stops[len(f.stops)-1].Color
		if len(f.stops) == 1 {
			// A single stop is the solid color it names.
			color := hslaToRgba(last)
			return []TableGradientStop{{Color: color, Position: 0}, {Color: color, Position: 1}}, true
		}
		p := clamp01(f.first)
		return []TableGradientStop{
			{Color: hslaToRgba(f.stops[0].Color), Position: p},
			{Color: hslaToRgba(last), Position: p},
		}, true
	}
	out := make([]TableGradientStop, len(f.stops))
	for i, stop := range f.stops {
		out[i] = TableGradientStop{
			Color:    hslaToRgba(stop.Color),
			Position: (stop.Position - f.first) / span,
		}
	}
	return out, false
}

// ---------------------------------------------------------------------------
// The paint-time resolution (the geometry words the table entry needs)
// ---------------------------------------------------------------------------

// resolveCssGradient resolves a CSS gradient over a logical gradient
// box into its scene gradient table entry (browser-core ticket 57):
// the geometry in DEVICE pixels (the box scaled by the paint context's
// factor), the stops fixed up and normalized to [0, 1] over their span
// (Chromium's AddStops + NormalizeAndAddStops), and the geometry's
// domain contracted to the same span so the pad/repeat wrap lands on
// the CSS stop range.
func resolveCssGradient(g CssGradient, box Bounds, scale float32) (Gradient, error) {
	if len(g.Stops) == 0 {
		return Gradient{}, ErrGradientStops
	}
	fixed := fixGradientStops(g.Stops)
	stops, degenerateSpan := fixed.normalize()
	if degenerateSpan && g.Repeating {
		// A coincident-stop repeating gradient is solid — the LAST
		// stop's color (Chromium's zero-span rule).
		color := stops[len(stops)-1].Color
		for i := range stops {
			stops[i].Color = color
		}
	}
	w := box.Size.Width * scale
	h := box.Size.Height * scale
	cx := box.Origin.X * scale
	cy := box.Origin.Y * scale
	tile := GradientTilePad
	if g.Repeating {
		tile = GradientTileRepeat
	}
	out := Gradient{Tile: tile, Stops: stops}
	span := fixed.last - fixed.first
	if span <= 0 {
		span = 1
	}
	// contract reports whether the stops cover less than the full
	// [0, 1] domain (the geometry shrinks to the span, Chromium's
	// AdjustedGradientDomainForOffsetRange). A coincident-stop set never
	// contracts (the hard edge rides inside the un-normalized ramp).
	contract := !degenerateSpan && (fixed.first != 0 || fixed.last != 1)
	switch g.Kind {
	case CssGradientRadial:
		centerX := cx + w*g.CenterX
		centerY := cy + h*g.CenterY
		rx, ry := radialEndRadii(g.Shape, g.Extent, centerX, centerY, cx, cy, w, h)
		if rx <= 0 || ry <= 0 {
			return Gradient{}, ErrGradientDegenerate
		}
		// The geometry's radii are in ry units with the rx/ry aspect
		// (Gradient.Geometry's radial arm): r1 = ry, r0 = 0, then the
		// domain contracts to the stop span (Chromium's
		// AdjustGradientRadiiForOffsetRange).
		r0 := float32(0)
		r1 := ry
		if contract {
			r0 = ry * fixed.first
			r1 = ry * fixed.last
			if r1 <= r0 {
				// A span past the full radius: the repeating wrap needs a
				// positive period; clamp the start below the end.
				r0 = r1 - ry
				if r0 < 0 {
					r0 = 0
				}
			}
		}
		out.Kind = GradientRadial
		out.Geometry = [8]float32{centerX, centerY, r0, r1, rx / ry}
	case CssGradientConic:
		centerX := cx + w*g.CenterX
		centerY := cy + h*g.CenterY
		// The sweep starts at the from-angle shifted to the first stop
		// (Chromium's AdjustedGradientDomainForOffsetRange over the
		// angles) and one period spans the stop span's share of the
		// turn.
		start := float32(g.FromAngle * math.Pi / 180)
		rotation := float32(2 * math.Pi)
		if contract {
			start += fixed.first * 2 * float32(math.Pi)
			rotation = span * 2 * float32(math.Pi)
		}
		out.Kind = GradientConic
		out.Geometry = [8]float32{centerX, centerY, start, rotation}
	default:
		// The gradient line through the box center at the CSS angle,
		// |W·sinθ|+|H·cosθ| long (EndPointsFromAngle,
		// css_gradient_value.cc:1284-1350): the endpoints are the line's
		// exits from the box, then the domain contracts to the stop
		// span (AdjustedGradientDomainForOffsetRange).
		angle := resolveGradientAngle(&LinearGradient{Direction: g.Direction, Angle: g.Angle}, w, h)
		radians := angle * math.Pi / 180
		sin, cos := float32(math.Sin(float64(radians))), float32(math.Cos(float64(radians)))
		length := abs32(w*sin) + abs32(h*cos)
		if length <= 0 {
			return Gradient{}, ErrGradientDegenerate
		}
		centerX := cx + w*0.5
		centerY := cy + h*0.5
		dx := (length * 0.5) * sin
		dy := -(length * 0.5) * cos
		x0, y0 := centerX-dx, centerY-dy
		x1, y1 := centerX+dx, centerY+dy
		if contract {
			spanX, spanY := x1-x0, y1-y0
			x0 += spanX * fixed.first
			y0 += spanY * fixed.first
			x1 = x0 + spanX*span
			y1 = y0 + spanY*span
		}
		if x1 == x0 && y1 == y0 {
			return Gradient{}, ErrGradientDegenerate
		}
		out.Kind = GradientLinear
		out.Geometry = [8]float32{x0, y0, x1, y1}
	}
	return out, nil
}

// radialEndRadii resolves a radial gradient's end shape over the box
// (Chromium RadiusToSide / RadiusToCorner / EllipseRadius,
// css_gradient_value.cc:1848-1917): the circle takes the side or
// corner distance directly; the ellipse takes the closest-/farthest-
// SIDE aspect ratio scaled to pass through the chosen corner.
func radialEndRadii(shape RadialShape, extent RadialExtent, centerX, centerY, boxX, boxY, w, h float32) (rx, ry float32) {
	compare := func(a, b float32) bool { return a < b }
	switch extent {
	case RadialExtentFarthestSide, RadialExtentFarthestCorner:
		compare = func(a, b float32) bool { return a > b }
	}
	dx1 := abs32(centerX - boxX)
	dx2 := abs32(centerX - (boxX + w))
	dy1 := abs32(centerY - boxY)
	dy2 := abs32(centerY - (boxY + h))
	sideX := dx1
	if compare(dx2, dx1) {
		sideX = dx2
	}
	sideY := dy1
	if compare(dy2, dy1) {
		sideY = dy2
	}
	if shape == RadialShapeCircle {
		r := sideX
		if compare(sideY, sideX) {
			r = sideY
		}
		return r, r
	}
	if extent == RadialExtentClosestSide || extent == RadialExtentFarthestSide {
		return sideX, sideY
	}
	// The corner: the ellipse keeps the side radii's aspect ratio and
	// passes through the closest (or farthest) corner —
	// EllipseRadius(corner - center, sideAspect): a = sqrt(x² + y²·aspect²),
	// b = a/aspect with aspect = sideX/sideY (the first corner wins a
	// tie, like Chromium's strict-compare loop).
	aspect := sideX / sideY
	cornerX, cornerY := boxX-centerX, boxY-centerY
	best := float32(math.Hypot(float64(cornerX), float64(cornerY)))
	for _, corner := range [4][2]float32{{boxX, boxY}, {boxX + w, boxY}, {boxX + w, boxY + h}, {boxX, boxY + h}} {
		d := float32(math.Hypot(float64(corner[0]-centerX), float64(corner[1]-centerY)))
		if compare(d, best) {
			best = d
			cornerX, cornerY = corner[0]-centerX, corner[1]-centerY
		}
	}
	if aspect <= 0 || !isFinite32(aspect) {
		return 0, 0
	}
	a := float32(math.Sqrt(float64(cornerX*cornerX + cornerY*cornerY*aspect*aspect)))
	return a, a / aspect
}

// resolveCssGradientBackground resolves a CSS-gradient Background
// against a logical box and inserts it into the scene's gradient
// table, returning the table-referencing Background the records carry.
// The scene-level seam (PaintQuad) calls this; the index must be
// inserted before the primitive that paints it.
func (s *Scene) resolveCssGradientBackground(b Background, box Bounds, scale float32) (Background, error) {
	if s.kernel == nil {
		return Background{}, ErrSceneClosed
	}
	if b.CssGradient == nil {
		return Background{}, ErrGradientStops
	}
	gradient, err := resolveCssGradient(*b.CssGradient, box, scale)
	if err != nil {
		return Background{}, err
	}
	index, err := s.InsertGradient(gradient)
	if err != nil {
		return Background{}, err
	}
	return TableGradientBackground(index), nil
}

func isFinite32(v float32) bool {
	return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0)
}
