package gpui

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// The CSS transform model (browser-core ticket 56, spec story 34): the
// Style carries a transform-function list and a transform-origin, both
// resolved at PREPAINT and PAINT against the element's laid-out border
// box (transforms never affect layout — the layout engine never sees
// them), composed into one window-to-window 2x3 TransformationMatrix
// that drives three consumers:
//
//   - PAINT: the matrix brackets the element's whole box (background,
//     borders, shadows, outline and children) in a scene effect group
//     with HasTransform (ticket 51's ABI record; the local frame is the
//     untransformed window frame, matching the record's contract "the
//     matrix maps LOCAL (untransformed window-space) points to device
//     points"). A translation-only transform skips the group and shifts
//     the element offset instead — the design §3.4 fast path, the pin's
//     existing offset economy.
//   - HIT TESTING: the hitbox carries the matrix; the hit test maps the
//     pointer through the inverse before the bounds test, so hover,
//     click, capture and drag targets all follow the transform
//     (Chromium maps each hit-test location through the accumulated
//     inverse of the transform chain, HitTestingTransformState; the
//     port composes the matrix down the element tree the same way).
//   - TRANSITIONS: the transform list is an interpolable style
//     property (transition.go's slot; matching function lists
//     interpolate per CSS Transforms 1, the list-interpolation rule).
//
// transform-origin resolves against the BORDER box (the CSS initial
// 50% 50%, css_properties.json5's initial `TransformOrigin(50%, 50%, 0)`
// — the 2D port ignores the z offset). Percentages of the border box
// resolve at matrix time, when the box is known.

// TransformKind selects one CSS transform function.
type TransformKind uint8

const (
	// TransformTranslate is translate(<x>, <y>): the two lengths are
	// fractions of the element's own border box when given as
	// percentages (css-transforms-1 §translate-property: "percentage of
	// the width/height of the reference box").
	TransformTranslate TransformKind = iota + 1
	// TransformScale is scale(<x>, <y>): unitless factors.
	TransformScale
	// TransformRotate is rotate(<angle>): degrees, clockwise on screen
	// (the y-down matrix convention the pin's sprite path uses).
	TransformRotate
	// TransformSkew is skew(<x-angle>, <y-angle>): degrees.
	TransformSkew
)

// String renders the CSS spelling of the function.
func (k TransformKind) String() string {
	switch k {
	case TransformTranslate:
		return "translate"
	case TransformScale:
		return "scale"
	case TransformRotate:
		return "rotate"
	case TransformSkew:
		return "skew"
	}
	return "none"
}

// TransformFunction is one function of the CSS transform property. The
// zero value is not a valid function; build one with the package
// constructors (Translate, TranslateX, Scale, Rotate, Skew ...).
type TransformFunction struct {
	// Kind selects the function.
	Kind TransformKind
	// X and Y carry the numeric arguments of Scale (the unitless
	// factors), Rotate (the degrees, X only) and Skew (the per-axis
	// degrees, X = the x-axis shear angle, Y = the y-axis shear angle).
	X, Y float32
	// TranslateX and TranslateY carry Translate's arguments: lengths in
	// logical px, rems, or fractions of the element's own border box
	// (CSS translate percentages), resolved when the matrix is composed
	// (the border box and the window's rem are known there).
	TranslateX, TranslateY DefiniteLength
}

// Translate builds translate(<x>, <y>) — the two lengths may be px,
// rems or fractions of the element's border box.
func Translate(x, y DefiniteLength) TransformFunction {
	return TransformFunction{Kind: TransformTranslate, TranslateX: x, TranslateY: y}
}

// TranslateX builds translate(<x>, 0).
func TranslateX(x DefiniteLength) TransformFunction {
	return Translate(x, DefinitePx(0))
}

// TranslateY builds translate(0, <y>).
func TranslateY(y DefiniteLength) TransformFunction {
	return Translate(DefinitePx(0), y)
}

// Scale builds scale(<x>, <y>) — the unitless factors.
func Scale(x, y float32) TransformFunction {
	return TransformFunction{Kind: TransformScale, X: x, Y: y}
}

// ScaleUniform builds scale(<f>, <f>).
func ScaleUniform(factor float32) TransformFunction {
	return Scale(factor, factor)
}

// ScaleX builds scale(<x>, 1).
func ScaleX(x float32) TransformFunction {
	return Scale(x, 1)
}

// ScaleY builds scale(1, <y>).
func ScaleY(y float32) TransformFunction {
	return Scale(1, y)
}

// Rotate builds rotate(<degrees>) — clockwise on screen.
func Rotate(degrees float32) TransformFunction {
	return TransformFunction{Kind: TransformRotate, X: degrees}
}

// Skew builds skew(<x-degrees>, <y-degrees>).
func Skew(xDegrees, yDegrees float32) TransformFunction {
	return TransformFunction{Kind: TransformSkew, X: xDegrees, Y: yDegrees}
}

// SkewX builds skew(<x-degrees>, 0).
func SkewX(xDegrees float32) TransformFunction {
	return Skew(xDegrees, 0)
}

// SkewY builds skew(0, <y-degrees>).
func SkewY(yDegrees float32) TransformFunction {
	return Skew(0, yDegrees)
}

// String renders the CSS spelling of the function (the arguments a
// stylesheet would carry).
func (f TransformFunction) String() string {
	switch f.Kind {
	case TransformTranslate:
		return fmt.Sprintf("%s(%s, %s)", f.Kind, cssLengthSpelling(f.TranslateX), cssLengthSpelling(f.TranslateY))
	case TransformScale:
		return fmt.Sprintf("%s(%s, %s)", f.Kind, cssNumberSpelling(f.X), cssNumberSpelling(f.Y))
	case TransformRotate:
		return fmt.Sprintf("%s(%sdeg)", f.Kind, cssNumberSpelling(f.X))
	case TransformSkew:
		return fmt.Sprintf("%s(%sdeg, %sdeg)", f.Kind, cssNumberSpelling(f.X), cssNumberSpelling(f.Y))
	}
	return "none"
}

// cssLengthSpelling renders a DefiniteLength the way CSS spells it.
func cssLengthSpelling(l DefiniteLength) string {
	switch l.Kind {
	case DefiniteLengthPx:
		return cssNumberSpelling(l.Value) + "px"
	case DefiniteLengthRem:
		return cssNumberSpelling(l.Value) + "rem"
	case DefiniteLengthFraction:
		return cssNumberSpelling(l.Value*100) + "%"
	}
	return "0px"
}

// cssNumberSpelling renders a float without trailing zeros.
func cssNumberSpelling(v float32) string {
	s := strconv.FormatFloat(float64(v), 'g', -1, 32)
	return s
}

// matrix is the function's own 2x3 (the transform-origin and the
// element position are NOT resolved into it — the caller composes
// those, css-transforms-1 §transform-origin).
func (f TransformFunction) matrix() TransformationMatrix {
	switch f.Kind {
	case TransformTranslate:
		// The lengths resolve at composition time; this arm is only hit
		// through transformFunctionsMatrix, which resolves them first.
		return TransformationMatrix{
			RotationScale: [2][2]float32{{1, 0}, {0, 1}},
			Translation:   [2]float32{0, 0},
		}
	case TransformScale:
		return TransformationMatrix{RotationScale: [2][2]float32{{f.X, 0}, {0, f.Y}}}
	case TransformRotate:
		radians := float64(f.X) * math.Pi / 180
		c, s := float32(math.Cos(radians)), float32(math.Sin(radians))
		return TransformationMatrix{RotationScale: [2][2]float32{{c, -s}, {s, c}}}
	case TransformSkew:
		xTan := float32(math.Tan(float64(f.X) * math.Pi / 180))
		yTan := float32(math.Tan(float64(f.Y) * math.Pi / 180))
		return TransformationMatrix{RotationScale: [2][2]float32{{1, xTan}, {yTan, 1}}}
	}
	return UnitTransformation()
}

// translatePixels resolves the translate arguments against the border
// box and the rem size (CSS percentages of the element's own size).
func (f TransformFunction) translatePixels(borderBox Size, rem float32) (float32, float32) {
	x := f.TranslateX.Value
	if f.TranslateX.Kind == DefiniteLengthRem {
		x *= rem
	} else if f.TranslateX.Kind == DefiniteLengthFraction {
		x *= borderBox.Width
	}
	y := f.TranslateY.Value
	if f.TranslateY.Kind == DefiniteLengthRem {
		y *= rem
	} else if f.TranslateY.Kind == DefiniteLengthFraction {
		y *= borderBox.Height
	}
	return x, y
}

// ---------------------------------------------------------------------------
// The origin
// ---------------------------------------------------------------------------

// TransformOrigin is the CSS transform-origin property: two lengths
// resolved against the element's border box. The zero value means the
// CSS initial 50% 50% (the centre), so an unset origin needs no
// Option: OriginFrom resolves a nil pointer to the default.
type TransformOrigin struct {
	// X is the horizontal origin: a px or rem length, or a fraction of
	// the border box's width (0.5 = 50%).
	X DefiniteLength
	// Y is the vertical origin: a px or rem length, or a fraction of
	// the border box's height.
	Y DefiniteLength
}

// DefaultTransformOrigin is the CSS initial transform-origin: 50% 50%
// of the border box.
func DefaultTransformOrigin() TransformOrigin {
	return TransformOrigin{X: Fraction(0.5), Y: Fraction(0.5)}
}

// point resolves the origin against the border box (logical pixels).
func (o TransformOrigin) point(borderBox Bounds, rem float32) Point {
	resolve := func(l DefiniteLength, total float32) float32 {
		switch l.Kind {
		case DefiniteLengthRem:
			return l.Value * rem
		case DefiniteLengthFraction:
			return l.Value * total
		default:
			return l.Value
		}
	}
	return Point{
		X: borderBox.Origin.X + resolve(o.X, borderBox.Size.Width),
		Y: borderBox.Origin.Y + resolve(o.Y, borderBox.Size.Height),
	}
}

// OriginFrom resolves the style's origin pointer, defaulting to the
// CSS initial 50% 50%.
func OriginFrom(o *TransformOrigin) TransformOrigin {
	if o == nil {
		return DefaultTransformOrigin()
	}
	return *o
}

// ---------------------------------------------------------------------------
// Matrix composition
// ---------------------------------------------------------------------------

// mulTransformation composes a then b: the result maps p to a·(b·p),
// the matrix-product a×b (CSS's left-to-right function order: the
// rightmost function applies to the point first).
func mulTransformation(a, b TransformationMatrix) TransformationMatrix {
	ra, rb := a.RotationScale, b.RotationScale
	return TransformationMatrix{
		RotationScale: [2][2]float32{
			{ra[0][0]*rb[0][0] + ra[0][1]*rb[1][0], ra[0][0]*rb[0][1] + ra[0][1]*rb[1][1]},
			{ra[1][0]*rb[0][0] + ra[1][1]*rb[1][0], ra[1][0]*rb[0][1] + ra[1][1]*rb[1][1]},
		},
		Translation: [2]float32{
			a.Translation[0] + ra[0][0]*b.Translation[0] + ra[0][1]*b.Translation[1],
			a.Translation[1] + ra[1][0]*b.Translation[0] + ra[1][1]*b.Translation[1],
		},
	}
}

// applyTransformation maps a point through the matrix (p' = M·p + t).
func applyTransformation(m TransformationMatrix, p Point) Point {
	return Point{
		X: m.RotationScale[0][0]*p.X + m.RotationScale[0][1]*p.Y + m.Translation[0],
		Y: m.RotationScale[1][0]*p.X + m.RotationScale[1][1]*p.Y + m.Translation[1],
	}
}

// invertTransformation inverts the matrix; ok is false when the 2x2 is
// singular (a scale-0 collapse, which the scene record also rejects).
func invertTransformation(m TransformationMatrix) (TransformationMatrix, bool) {
	r := m.RotationScale
	det := r[0][0]*r[1][1] - r[0][1]*r[1][0]
	if det == 0 || math.IsNaN(float64(det)) || math.IsInf(float64(det), 0) {
		return TransformationMatrix{}, false
	}
	inv := 1 / det
	ri := [2][2]float32{
		{r[1][1] * inv, -r[0][1] * inv},
		{-r[1][0] * inv, r[0][0] * inv},
	}
	// t' = -Ri·t.
	return TransformationMatrix{
		RotationScale: ri,
		Translation: [2]float32{
			-(ri[0][0]*m.Translation[0] + ri[0][1]*m.Translation[1]),
			-(ri[1][0]*m.Translation[0] + ri[1][1]*m.Translation[1]),
		},
	}, true
}

// transformationDeterminant is the 2x2 determinant.
func transformationDeterminant(m TransformationMatrix) float32 {
	return m.RotationScale[0][0]*m.RotationScale[1][1] - m.RotationScale[0][1]*m.RotationScale[1][0]
}

// transformationIsTranslation reports whether the 2x2 is the identity
// (the design §3.4 fast-path economy case) and returns the translation.
func transformationIsTranslation(m TransformationMatrix) (float32, float32, bool) {
	r := m.RotationScale
	if r[0][0] == 1 && r[1][1] == 1 && r[0][1] == 0 && r[1][0] == 0 {
		return m.Translation[0], m.Translation[1], true
	}
	return 0, 0, false
}

// mapBoundsThrough maps the bounds' four corners through the matrix and
// returns the axis-aligned bounding box of the images (getBoundingClientRect
// geometry: a rotated box reports its mapped AABB).
func mapBoundsThrough(m TransformationMatrix, b Bounds) Bounds {
	corners := [4]Point{
		{X: b.Origin.X, Y: b.Origin.Y},
		{X: b.Origin.X + b.Size.Width, Y: b.Origin.Y},
		{X: b.Origin.X + b.Size.Width, Y: b.Origin.Y + b.Size.Height},
		{X: b.Origin.X, Y: b.Origin.Y + b.Size.Height},
	}
	first := applyTransformation(m, corners[0])
	minX, minY := first.X, first.Y
	maxX, maxY := first.X, first.Y
	for i := 1; i < len(corners); i++ {
		p := applyTransformation(m, corners[i])
		minX = min32(minX, p.X)
		minY = min32(minY, p.Y)
		maxX = max32(maxX, p.X)
		maxY = max32(maxY, p.Y)
	}
	return Bounds{
		Origin: Point{X: minX, Y: minY},
		Size:   Size{Width: maxX - minX, Height: maxY - minY},
	}
}

// transformMatrixOf composes the style's transform functions about the
// transform-origin against the border box, in logical window
// coordinates: the returned matrix maps a point of the element's
// UNTRANSFORMED layout position to its transformed window position.
// ok is false when there are no functions or the composed 2x2 is
// singular (scale 0 — the element paints nothing and hit tests false,
// like CSS's collapsed transform).
func transformMatrixOf(functions []TransformFunction, origin *TransformOrigin, borderBox Bounds, rem float32) (TransformationMatrix, bool) {
	if len(functions) == 0 {
		return TransformationMatrix{}, false
	}
	// Resolve the translate lengths, then compose left to right (the
	// rightmost function applies first).
	m := UnitTransformation()
	for _, f := range functions {
		fm := f.matrix()
		if f.Kind == TransformTranslate {
			tx, ty := f.translatePixels(borderBox.Size, rem)
			fm.Translation = [2]float32{tx, ty}
		}
		m = mulTransformation(m, fm)
	}
	det := transformationDeterminant(m)
	if det == 0 || math.IsNaN(float64(det)) || math.IsInf(float64(det), 0) {
		return TransformationMatrix{}, false
	}
	// Resolve the origin: the effective transform is
	// translate(o) · M · translate(−o), so p' = R·(p − o) + t + o and the
	// composed translation is t + (o − R·o) — the LINEAR part maps the
	// origin, not the affine image (the affine translation t survives
	// once).
	o := OriginFrom(origin).point(borderBox, rem)
	m.Translation[0] += o.X - (m.RotationScale[0][0]*o.X + m.RotationScale[0][1]*o.Y)
	m.Translation[1] += o.Y - (m.RotationScale[1][0]*o.X + m.RotationScale[1][1]*o.Y)
	return m, true
}

// deviceTransform scales the logical window-to-window matrix into the
// device space the scene records carry: the uniform scale cancels out
// of the 2x2 (S·M·S⁻¹ = M) and multiplies the translation.
func deviceTransform(m TransformationMatrix, scale float32) TransformationMatrix {
	return TransformationMatrix{
		RotationScale: m.RotationScale,
		Translation:   [2]float32{m.Translation[0] * scale, m.Translation[1] * scale},
	}
}

// ---------------------------------------------------------------------------
// TSX parsing (gpui/script's style object values)
// ---------------------------------------------------------------------------

// splitTransformTokens splits a transform value on whitespace OUTSIDE
// parentheses, so "translate(8px, 4px) rotate(45deg)" is two tokens.
func splitTransformTokens(value string) []string {
	var out []string
	depth, start := 0, -1
	flush := func(end int) {
		if end > start+1 {
			out = append(out, value[start+1:end])
		}
		start = end
	}
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '(', '[':
			depth++
		case ')', ']':
			if depth > 0 {
				depth--
			}
		case ' ', '\t', '\n', '\r':
			if depth == 0 {
				flush(i)
			}
		}
	}
	flush(len(value))
	return out
}

// ParseTransformList parses a CSS transform property value —
// "translate(8px, 4px) rotate(45deg) scale(1.5)" — into the function
// list. Each function takes the forms the TSX surface accepts: lengths
// (px, rem or %) for translate, unitless numbers for scale, degrees for
// rotate and skew. Anything else is an error naming the function.
func ParseTransformList(value string) ([]TransformFunction, error) {
	fields := splitTransformTokens(value)
	var out []TransformFunction
	for _, field := range fields {
		f, err := parseTransformFunction(field)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no transform functions in %q", value)
	}
	return out, nil
}

// parseTransformFunction parses one function token: name(args).
func parseTransformFunction(field string) (TransformFunction, error) {
	open := strings.IndexByte(field, '(')
	if open < 0 || !strings.HasSuffix(field, ")") {
		return TransformFunction{}, fmt.Errorf("transform function %q: expected name(args)", field)
	}
	name := field[:open]
	args := field[open+1 : len(field)-1]
	parts := strings.Split(args, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	need := func(n int) error {
		if len(parts) != n {
			return fmt.Errorf("transform function %q: expected %d argument(s), got %d", name, n, len(parts))
		}
		return nil
	}
	switch name {
	case "translate":
		if err := need(2); err != nil {
			return TransformFunction{}, err
		}
		x, err := parseTransformLength(parts[0])
		if err != nil {
			return TransformFunction{}, err
		}
		y, err := parseTransformLength(parts[1])
		if err != nil {
			return TransformFunction{}, err
		}
		return Translate(x, y), nil
	case "translateX", "translate-x":
		if err := need(1); err != nil {
			return TransformFunction{}, err
		}
		x, err := parseTransformLength(parts[0])
		if err != nil {
			return TransformFunction{}, err
		}
		return TranslateX(x), nil
	case "translateY", "translate-y":
		if err := need(1); err != nil {
			return TransformFunction{}, err
		}
		y, err := parseTransformLength(parts[0])
		if err != nil {
			return TransformFunction{}, err
		}
		return TranslateY(y), nil
	case "scale":
		if len(parts) == 1 {
			f, err := parseTransformNumber(parts[0])
			if err != nil {
				return TransformFunction{}, err
			}
			return ScaleUniform(f), nil
		}
		if err := need(2); err != nil {
			return TransformFunction{}, err
		}
		x, err := parseTransformNumber(parts[0])
		if err != nil {
			return TransformFunction{}, err
		}
		y, err := parseTransformNumber(parts[1])
		if err != nil {
			return TransformFunction{}, err
		}
		return Scale(x, y), nil
	case "rotate":
		if err := need(1); err != nil {
			return TransformFunction{}, err
		}
		deg, err := parseTransformAngle(parts[0])
		if err != nil {
			return TransformFunction{}, err
		}
		return Rotate(deg), nil
	case "skew":
		if err := need(2); err != nil {
			return TransformFunction{}, err
		}
		x, err := parseTransformAngle(parts[0])
		if err != nil {
			return TransformFunction{}, err
		}
		y, err := parseTransformAngle(parts[1])
		if err != nil {
			return TransformFunction{}, err
		}
		return Skew(x, y), nil
	case "skewX", "skew-x":
		if err := need(1); err != nil {
			return TransformFunction{}, err
		}
		x, err := parseTransformAngle(parts[0])
		if err != nil {
			return TransformFunction{}, err
		}
		return SkewX(x), nil
	case "skewY", "skew-y":
		if err := need(1); err != nil {
			return TransformFunction{}, err
		}
		y, err := parseTransformAngle(parts[0])
		if err != nil {
			return TransformFunction{}, err
		}
		return SkewY(y), nil
	}
	return TransformFunction{}, fmt.Errorf("transform function %q is not supported (use translate, translateX, translateY, scale, rotate, skew, skewX or skewY)", name)
}

// parseTransformLength parses a length argument: 8, 8px, 0.5rem or 50%.
func parseTransformLength(s string) (DefiniteLength, error) {
	switch {
	case strings.HasSuffix(s, "px"):
		v, err := parseFloat32(strings.TrimSuffix(s, "px"))
		if err != nil {
			return DefiniteLength{}, fmt.Errorf("transform length %q: %v", s, err)
		}
		return DefinitePx(v), nil
	case strings.HasSuffix(s, "rem"):
		v, err := parseFloat32(strings.TrimSuffix(s, "rem"))
		if err != nil {
			return DefiniteLength{}, fmt.Errorf("transform length %q: %v", s, err)
		}
		return DefiniteRem(v), nil
	case strings.HasSuffix(s, "%"):
		v, err := parseFloat32(strings.TrimSuffix(s, "%"))
		if err != nil {
			return DefiniteLength{}, fmt.Errorf("transform length %q: %v", s, err)
		}
		return Fraction(v / 100), nil
	default:
		v, err := parseFloat32(s)
		if err != nil {
			return DefiniteLength{}, fmt.Errorf("transform length %q: expected px, rem or %%", s)
		}
		return DefinitePx(v), nil
	}
}

// parseTransformNumber parses a unitless factor.
func parseTransformNumber(s string) (float32, error) {
	v, err := parseFloat32(s)
	if err != nil {
		return 0, fmt.Errorf("transform scale %q: expected a number", s)
	}
	return v, nil
}

// parseTransformAngle parses an angle: a bare number or "45deg".
func parseTransformAngle(s string) (float32, error) {
	if strings.HasSuffix(s, "deg") {
		v, err := parseFloat32(strings.TrimSuffix(s, "deg"))
		if err != nil {
			return 0, fmt.Errorf("transform angle %q: %v", s, err)
		}
		return v, nil
	}
	v, err := parseFloat32(s)
	if err != nil {
		return 0, fmt.Errorf("transform angle %q: expected degrees", s)
	}
	return v, nil
}

// ParseTransformOrigin parses a CSS transform-origin value — "50% 50%",
// "top left", "10px 20px" — into the origin. The keywords resolve to
// their percentage equivalents (top/left 0%, centre 50%, bottom/right
// 100%); one argument applies to both axes.
func ParseTransformOrigin(value string) (TransformOrigin, error) {
	fields := strings.Fields(value)
	if len(fields) == 0 || len(fields) > 2 {
		return TransformOrigin{}, fmt.Errorf("transform-origin %q: expected one or two arguments", value)
	}
	parse := func(s string) (DefiniteLength, error) {
		switch s {
		case "left", "top":
			return Fraction(0), nil
		case "center", "centre":
			return Fraction(0.5), nil
		case "right", "bottom":
			return Fraction(1), nil
		}
		return parseTransformLength(s)
	}
	x, err := parse(fields[0])
	if err != nil {
		return TransformOrigin{}, fmt.Errorf("transform-origin %q: %v", value, err)
	}
	y := x
	if len(fields) == 2 {
		y, err = parse(fields[1])
		if err != nil {
			return TransformOrigin{}, fmt.Errorf("transform-origin %q: %v", value, err)
		}
	}
	return TransformOrigin{X: x, Y: y}, nil
}
