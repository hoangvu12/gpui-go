package gpui

import (
	"fmt"
	"math"
)

// This file is the paint-side of the scene slice (ticket08): the color
// types and conversions, the paint parameter types (PaintQuad, BoxShadow,
// UnderlineStyle) and the ports of the pinned Window paint methods'
// record construction (crates/gpui/src/window.rs at
// 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a):
//
//   - paint_quad_with_corner_smoothing: snapped bounds, scaled corner
//     radii, stroke-snapped border widths, element opacity folded into
//     background and border colors, corner smoothing clamped, and the
//     border-only split into strips (largest_border_interior);
//   - paint_drop_shadows_with_corner_smoothing /
//     paint_inset_shadows_with_corner_smoothing;
//   - paint_underline;
//   - with_filter_layer_with_corner_smoothing / paint_backdrop_filter_
//     with_corner_smoothing.
//
// The pinned window helpers (snap_bounds, cover_bounds, snap_stroke,
// snapped_content_mask) are the pub(crate) ports already present in
// layout.go's rounding helpers; they are restated here as scene-specific
// wrappers over those helpers.
//
// Colors: the pinned reference stores HSL-with-alpha (SceneHsla) and
// converts sRGB hex inputs with `rgba(hex)` + `rgb_to_hsla` (palette's
// Srgb->Hsl). The conversion below is a bit-exact port of palette 0.7.7's
// f32 path (hsl.rs FromColorUnclamped<Rgb> for Hsl, Mask = bool branch,
// plus hues.rs's RgbHue::new/into_positive_degrees and angle.rs's
// normalize_unsigned_angle).

// ---------------------------------------------------------------------------
// Colors
// ---------------------------------------------------------------------------

// Hsla is the scene's HSL-with-alpha color (the pinned SceneHsla): hue in
// [0, 1] (degrees / 360), saturation, lightness and alpha in [0, 1].
// Every field is an f32 exactly as stored in the scene records.
type Hsla struct {
	H float32
	S float32
	L float32
	A float32
}

// TransparentBlack is the pinned transparent black.
func TransparentBlack() Hsla { return Hsla{H: 0, S: 0, L: 0, A: 0} }

// White is the pinned opaque white.
func White() Hsla { return Hsla{H: 0, S: 0, L: 1, A: 1} }

// clamp01 clamps an f32 to [0, 1] (Rust's f32::clamp(0., 1.)).
func clamp01(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// isSubnormal32 reports whether v is an IEEE-754 subnormal (Rust's
// f32::is_subnormal; zero is NOT subnormal).
func isSubnormal32(v float32) bool {
	b := math.Float32bits(v)
	return b&0x7f800000 == 0 && b&0x007fffff != 0
}

// normHueDeg is palette's normalize_unsigned_angle: x - floor(x/360)*360,
// mapping subnormals and values >= 360 to 0.
func normHueDeg(x float32) float32 {
	normalized := x - float32(math.Floor(float64(x/360.0)))*360.0
	if isSubnormal32(normalized) || normalized >= 360.0 {
		return 0
	}
	return normalized
}

// RgbaFromHex decodes an 0xRRGGBBAA value into its f32 components
// (the pinned rgba(): each byte / 255.0).
func RgbaFromHex(hex uint32) (r, g, b, a float32) {
	r = float32((hex>>24)&0xFF) / 255.0
	g = float32((hex>>16)&0xFF) / 255.0
	b = float32((hex>>8)&0xFF) / 255.0
	a = float32(hex&0xFF) / 255.0
	return
}

// RgbaToHsla converts an 0xRRGGBBAA color to the scene's HSL-with-alpha,
// a bit-exact port of the pinned `rgba(hex)` + `rgb_to_hsla` (palette
// 0.7.7's Srgb->Hsl f32 path):
//
//	red, green, blue = max(c, 0)
//	(max, min, sep, coeff):
//	  red > green   -> (red, green, green-blue, 0)
//	  otherwise     -> (green, red, blue-red, 2)
//	  blue > max    -> (blue, min, red-green, 4)
//	sum = max + min; l = sum / 2
//	if max != min:
//	  d = max - min
//	  s = d / (2 - sum) if sum > 1 else d / sum
//	  h = (sep/d + coeff) * 60
//	hue = normalize_unsigned_angle(h); H = hue / 360
func RgbaToHsla(hex uint32) Hsla {
	r0, g0, b0, a := RgbaFromHex(hex)
	// Avoid negative numbers (palette's rgb.max(T::zero())).
	red, green, blue := max32(r0, 0), max32(g0, 0), max32(b0, 0)

	var mx, mn, sep, coeff float32
	if red > green {
		mx, mn, sep, coeff = red, green, green-blue, 0
	} else {
		mx, mn, sep, coeff = green, red, blue-red, 2
	}
	if blue > mx {
		// max becomes blue; min is unchanged; sep = red - green; coeff = 4.
		mx, sep, coeff = blue, red-green, 4
	} else if blue < mn {
		mn = blue
	}

	sum := mx + mn
	l := sum / 2
	h, s := float32(0), float32(0)
	if mx != mn {
		d := mx - mn
		if sum > 1 {
			s = d / (2 - sum)
		} else {
			s = d / sum
		}
		h = (sep/d + coeff) * 60
	}
	return Hsla{H: normHueDeg(h) / 360, S: s, L: l, A: a}
}

func max32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

// OpacityBackground applies an element opacity to a stored background
// color exactly like the pinned Background::opacity: the round trip
// SceneHsla -> Hsla(h*360) -> alpha *= clamp(factor) -> SceneHsla, i.e.
// H = norm(H*360)/360 and A = A * clamp01(factor). Quads (background and
// border color) and box shadows (color) take this path.
func OpacityBackground(c Hsla, factor float32) Hsla {
	hue := normHueDeg(c.H * 360)
	return Hsla{H: hue / 360, S: c.S, L: c.L, A: c.A * clamp01(factor)}
}

// OpacityHsla applies an element opacity to a palette-style Hsla exactly
// like the pinned ColorExt::opacity for Hsla (used by the underline paint
// path BEFORE the SceneHsla conversion): only the alpha changes, A = A *
// clamp01(factor); the hue is normalized once at the SceneHsla
// conversion.
func OpacityHsla(c Hsla, factor float32) Hsla {
	return Hsla{H: c.H, S: c.S, L: c.L, A: c.A * clamp01(factor)}
}

// Background is the solid form of the pinned Background (the packed
// reference's BackgroundTag::Solid shape; gradients and patterns are
// later work).
type Background struct {
	// Solid is the stored HSL-with-alpha record.
	Solid Hsla
}

// SolidBackground builds a solid background from a color.
func SolidBackground(c Hsla) Background { return Background{Solid: c} }

// Opacity applies an element opacity (the pinned Background::opacity:
// alpha multiplied by the clamped factor, hue round-tripped).
func (b Background) Opacity(factor float32) Background {
	return Background{Solid: OpacityBackground(b.Solid, factor)}
}

// AsSolid returns the stored solid color.
func (b Background) AsSolid() Hsla { return b.Solid }

// IsTransparent reports whether the background is fully transparent (the
// pinned Background::is_transparent for the solid tag: a == 0).
func (b Background) IsTransparent() bool { return b.Solid.A == 0 }

// ---------------------------------------------------------------------------
// Paint parameter types (logical pixels, the paint API surface)
// ---------------------------------------------------------------------------

// Corners is a set of corner radii in logical pixels (the pinned
// Corners<Pixels>).
type Corners struct {
	TopLeft     float32
	TopRight    float32
	BottomRight float32
	BottomLeft  float32
}

// Edges is a set of edge widths in logical pixels (the pinned
// Edges<Pixels>).
type Edges struct {
	Top    float32
	Right  float32
	Bottom float32
	Left   float32
}

// BorderStyle is the style of a border (the pinned BorderStyle).
type BorderStyle uint8

const (
	// BorderStyleSolid is a solid border.
	BorderStyleSolid BorderStyle = 0
	// BorderStyleDashed is a dashed border.
	BorderStyleDashed BorderStyle = 1
)

// The pinned Quad default dashed-border parameters (scene.rs
// DEFAULT_BORDER_DASHED_LENGTH / DEFAULT_BORDER_DASHED_GAP).
const (
	DefaultBorderDashedLength float32 = 2.0
	DefaultBorderDashedGap    float32 = 1.0
)

// PaintQuad is a rectangle to be rendered in the window at the given
// position and size (the pinned PaintQuad): logical pixels, the paint
// API's input shape.
type PaintQuad struct {
	// Bounds of the quad within the window, in logical pixels.
	Bounds Bounds
	// CornerRadii of the quad, in logical pixels.
	CornerRadii Corners
	// Background color of the quad.
	Background Background
	// BorderWidths of the quad's borders, in logical pixels.
	BorderWidths Edges
	// BorderColor is the background painted into the quad's borders.
	BorderColor Background
	// Style of the quad's borders.
	BorderStyle BorderStyle
	// BorderDashedLength is the length of each border dash, as a multiple
	// of the border width.
	BorderDashedLength float32
	// BorderDashedGap is the gap between border dashes, as a multiple of
	// the border width.
	BorderDashedGap float32
}

// Fill builds a filled quad (the pinned fill()).
func Fill(bounds Bounds, background Background) PaintQuad {
	return PaintQuad{
		Bounds:             bounds,
		Background:         background,
		BorderColor:        SolidBackground(TransparentBlack()),
		BorderDashedLength: DefaultBorderDashedLength,
		BorderDashedGap:    DefaultBorderDashedGap,
	}
}

// BoxShadow is one box shadow of an element (the pinned BoxShadow):
// logical pixels.
type BoxShadow struct {
	// Color of the shadow.
	Color Background
	// Offset from the element.
	OffsetX float32
	OffsetY float32
	// BlurRadius of the shadow.
	BlurRadius float32
	// SpreadRadius of the shadow.
	SpreadRadius float32
	// Inset draws the shadow inside the element's bounds.
	Inset bool
}

// UnderlineStyle is the style of an underline (the pinned
// UnderlineStyle): logical pixels.
type UnderlineStyle struct {
	// Thickness of the underline.
	Thickness float32
	// Color of the underline; nil is the reference default
	// (transparent black).
	Color *Hsla
	// Wavy draws a spell-checker-style wavy underline (height = 3x
	// thickness).
	Wavy bool
}

// ---------------------------------------------------------------------------
// The pinned window.rs snap helpers over the layout.go rounding ports
// ---------------------------------------------------------------------------

// snapBounds is the pinned window.rs snap_bounds: each edge rounded to
// the device grid, far edges clamped to at least the near edges. Input
// is logical; output is device pixels.
func snapBounds(b Bounds, scale float32) Bounds {
	left := roundToDevicePixel(b.Left(), scale)
	top := roundToDevicePixel(b.Top(), scale)
	right := max32(roundToDevicePixel(b.Right(), scale), left)
	bottom := max32(roundToDevicePixel(b.Bottom(), scale), top)
	return Bounds{Origin: Point{X: left, Y: top}, Size: Size{Width: right - left, Height: bottom - top}}
}

// coverBounds is the pinned window.rs cover_bounds: floor the near edges,
// ceil the far edges (a strict superset of the raw region).
func coverBounds(b Bounds, scale float32) Bounds {
	left := floorToDevicePixel(b.Left(), scale)
	top := floorToDevicePixel(b.Top(), scale)
	right := max32(ceilToDevicePixel(b.Right(), scale), left)
	bottom := max32(ceilToDevicePixel(b.Bottom(), scale), top)
	return Bounds{Origin: Point{X: left, Y: top}, Size: Size{Width: right - left, Height: bottom - top}}
}

// snapStroke is the pinned window.rs snap_stroke: the stroke rule (exact
// zero stays zero; anything else clamps up to one device pixel).
func snapStroke(value float32, scale float32) float32 {
	return roundStrokeToDevicePixel(value, scale)
}

// snapBorderWidths snaps each border edge with the stroke rule.
func snapBorderWidths(edges Edges, scale float32) Edges {
	return Edges{
		Top:    snapStroke(edges.Top, scale),
		Right:  snapStroke(edges.Right, scale),
		Bottom: snapStroke(edges.Bottom, scale),
		Left:   snapStroke(edges.Left, scale),
	}
}

// scaleCorners multiplies each corner radius by the scale factor (the
// pinned Corners::scale).
func scaleCorners(c Corners, scale float32) Corners {
	return Corners{
		TopLeft:     c.TopLeft * scale,
		TopRight:    c.TopRight * scale,
		BottomRight: c.BottomRight * scale,
		BottomLeft:  c.BottomLeft * scale,
	}
}

// snappedContentMask is the pinned window.rs snapped_content_mask: the
// cover bounds of the logical mask (the fade component is always absent
// in this slice).
func snappedContentMask(mask Bounds, scale float32) Bounds {
	return coverBounds(mask, scale)
}

// dilate is the pinned Bounds::dilate: origin -= (a, a); size += (2a, 2a).
func dilate(b Bounds, amount float32) Bounds {
	return Bounds{
		Origin: Point{X: b.Origin.X - amount, Y: b.Origin.Y - amount},
		Size:   Size{Width: b.Size.Width + 2*amount, Height: b.Size.Height + 2*amount},
	}
}

// offsetBounds is the pinned Bounds + Point (origin shift).
func offsetBounds(b Bounds, dx, dy float32) Bounds {
	return Bounds{Origin: Point{X: b.Origin.X + dx, Y: b.Origin.Y + dy}, Size: b.Size}
}

// ---------------------------------------------------------------------------
// Record construction (the pinned paint methods' scene records)
// ---------------------------------------------------------------------------

// paintQuadFor builds the scene Quad record for a paint-quad under an
// element opacity, mirroring the pinned paint_quad_with_corner_
// smoothing: snapped bounds, snapped content mask, scaled radii,
// stroke-snapped border widths, opacity folded into the colors, and the
// border-only split deferred to PaintQuadStrips.
func paintQuadFor(pq PaintQuad, mask Bounds, scale, elementOpacity float32, cornerSmoothing float32) Quad {
	return Quad{
		Bounds:          snapBounds(pq.Bounds, scale),
		ContentMask:     snappedContentMask(mask, scale),
		Background:      pq.Background.Opacity(elementOpacity),
		BorderColor:     pq.BorderColor.Opacity(elementOpacity),
		CornerRadii:     scaleCorners(pq.CornerRadii, scale),
		BorderWidths:    snapBorderWidths(pq.BorderWidths, scale),
		BorderStyle:     pq.BorderStyle,
		DashedLength:    pq.BorderDashedLength,
		DashedGap:       pq.BorderDashedGap,
		CornerSmoothing: clamp01(cornerSmoothing),
	}
}

// largestBorderInterior is the pinned window.rs largest_border_interior
// (device pixels): the largest inner rectangle free of border pixels,
// used to split a border-only quad into strips.
func largestBorderInterior(q Quad) Bounds {
	radii := q.CornerRadii
	widths := q.BorderWidths
	reachFactor := 1.0 + q.CornerSmoothing
	edgeReaches := Edges{
		Top:    max32(radii.TopLeft, radii.TopRight) * reachFactor,
		Right:  max32(radii.TopRight, radii.BottomRight) * reachFactor,
		Bottom: max32(radii.BottomLeft, radii.BottomRight) * reachFactor,
		Left:   max32(radii.TopLeft, radii.BottomLeft) * reachFactor,
	}

	const antialiasInset = float32(1.0)
	insetBounds := func(topLeftInset, bottomRightInset Edges) Bounds {
		// Bounds::from_corners(origin + tl + aa, bottomRight - br - aa).
		originX := q.Bounds.Origin.X + topLeftInset.Left + antialiasInset
		originY := q.Bounds.Origin.Y + topLeftInset.Top + antialiasInset
		brX := q.Bounds.Origin.X + q.Bounds.Size.Width - bottomRightInset.Right - antialiasInset
		brY := q.Bounds.Origin.Y + q.Bounds.Size.Height - bottomRightInset.Bottom - antialiasInset
		return Bounds{Origin: Point{X: originX, Y: originY}, Size: Size{Width: brX - originX, Height: brY - originY}}
	}

	// Rounded corners need only be excluded on one axis. Either candidate
	// is empty of border pixels, so use the larger interior.
	horizontalBand := insetBounds(
		Edges{Left: widths.Left, Top: max32(widths.Top, edgeReaches.Top)},
		Edges{Right: widths.Right, Bottom: max32(widths.Bottom, edgeReaches.Bottom)},
	)
	verticalBand := insetBounds(
		Edges{Left: max32(widths.Left, edgeReaches.Left), Top: widths.Top},
		Edges{Right: max32(widths.Right, edgeReaches.Right), Bottom: widths.Bottom},
	)

	area := func(b Bounds) float32 {
		return max32(b.Size.Width, 0) * max32(b.Size.Height, 0)
	}
	if area(horizontalBand) >= area(verticalBand) {
		return horizontalBand
	}
	return verticalBand
}

// paintQuadStrips splits a border-only quad around its empty interior
// (the pinned paint_quad split): returns the strip quads, each with its
// content mask pre-intersected with the strip bounds (empty strips are
// dropped). Call only when the background is transparent and the
// interior is non-empty.
func paintQuadStrips(q Quad) []Quad {
	outer := q.Bounds
	inner := largestBorderInterior(q)
	outerRight := Point{X: outer.Right(), Y: 0}
	outerBottomRight := Point{X: outer.Right(), Y: outer.Bottom()}
	strips := []Bounds{
		// Top: from outer.origin to (outer.right, inner.top).
		fromCorners(outer.Origin, Point{X: outerRight.X, Y: inner.Origin.Y}),
		// Bottom: from (outer.left, inner.bottom) to outer.bottom_right.
		fromCorners(Point{X: outer.Origin.X, Y: inner.Bottom()}, outerBottomRight),
		// Left: from (outer.left, inner.top) to inner.bottom_left.
		fromCorners(Point{X: outer.Origin.X, Y: inner.Origin.Y}, Point{X: inner.Origin.X, Y: inner.Bottom()}),
		// Right: from inner.top_right to (outer.right, inner.bottom).
		fromCorners(Point{X: inner.Right(), Y: inner.Origin.Y}, Point{X: outerRight.X, Y: inner.Bottom()}),
	}
	var out []Quad
	for _, strip := range strips {
		masked := intersectBounds(q.ContentMask, strip)
		if !isEmptyBounds(masked) {
			stripQuad := q
			stripQuad.ContentMask = masked
			out = append(out, stripQuad)
		}
	}
	return out
}

// fromCorners is the pinned Bounds::from_corners (size = br - tl).
func fromCorners(tl, br Point) Bounds {
	return Bounds{Origin: tl, Size: Size{Width: br.X - tl.X, Height: br.Y - tl.Y}}
}

// intersectBounds is the pinned Bounds::intersect (component-wise max
// of origins, min of bottom-rights, max'd with the upper-left).
func intersectBounds(a, b Bounds) Bounds {
	upperLeftX := max32(a.Origin.X, b.Origin.X)
	upperLeftY := max32(a.Origin.Y, b.Origin.Y)
	brX := max32(min32(a.Right(), b.Right()), upperLeftX)
	brY := max32(min32(a.Bottom(), b.Bottom()), upperLeftY)
	return Bounds{Origin: Point{X: upperLeftX, Y: upperLeftY}, Size: Size{Width: brX - upperLeftX, Height: brY - upperLeftY}}
}

func min32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

// isEmptyBounds is the pinned Bounds::is_empty (width or height <= 0).
func isEmptyBounds(b Bounds) bool {
	return b.Size.Width <= 0 || b.Size.Height <= 0
}

// intersectLogical is intersectBounds for logical-space masks/bounds.
func intersectLogical(a, b Bounds) Bounds { return intersectBounds(a, b) }

// dropShadowFor builds the scene Shadow record for a drop shadow,
// mirroring the pinned paint_drop_shadows_with_corner_smoothing: the
// dilated (bounds + offset) region with cover bounds, blur scaled, the
// element's corner radii, and the opacity folded into the color.
func dropShadowFor(elementBounds Bounds, shadow BoxShadow, mask Bounds, scale, elementOpacity float32, elementRadii Corners, cornerSmoothing float32) Shadow {
	shadowBounds := dilate(offsetBounds(elementBounds, shadow.OffsetX, shadow.OffsetY), shadow.SpreadRadius)
	return Shadow{
		BlurRadius:         shadow.BlurRadius * scale,
		Bounds:             coverBounds(shadowBounds, scale),
		ContentMask:        snappedContentMask(mask, scale),
		CornerRadii:        scaleCorners(elementRadii, scale),
		Color:              shadow.Color.Opacity(elementOpacity),
		ElementBounds:      coverBounds(elementBounds, scale),
		ElementCornerRadii: scaleCorners(elementRadii, scale),
		Inset:              false,
		CornerSmoothing:    clamp01(cornerSmoothing),
	}
}

// insetShadowFor builds the scene Shadow record for an inset shadow,
// mirroring the pinned paint_inset_shadows_with_corner_smoothing: the
// hole ((bounds + offset) dilated by -spread) with cover bounds, hole
// corner radii clamped at zero per corner, blur scaled, inset true.
func insetShadowFor(elementBounds Bounds, shadow BoxShadow, mask Bounds, scale, elementOpacity float32, elementRadii Corners, cornerSmoothing float32) Shadow {
	hole := dilate(offsetBounds(elementBounds, shadow.OffsetX, shadow.OffsetY), -shadow.SpreadRadius)
	holeRadii := Corners{
		TopLeft:     max32(elementRadii.TopLeft-shadow.SpreadRadius, 0),
		TopRight:    max32(elementRadii.TopRight-shadow.SpreadRadius, 0),
		BottomRight: max32(elementRadii.BottomRight-shadow.SpreadRadius, 0),
		BottomLeft:  max32(elementRadii.BottomLeft-shadow.SpreadRadius, 0),
	}
	return Shadow{
		BlurRadius:         shadow.BlurRadius * scale,
		Bounds:             coverBounds(hole, scale),
		ContentMask:        snappedContentMask(mask, scale),
		CornerRadii:        scaleCorners(holeRadii, scale),
		Color:              shadow.Color.Opacity(elementOpacity),
		ElementBounds:      coverBounds(elementBounds, scale),
		ElementCornerRadii: scaleCorners(elementRadii, scale),
		Inset:              true,
		CornerSmoothing:    clamp01(cornerSmoothing),
	}
}

// underlineFor builds the scene Underline record, mirroring the pinned
// paint_underline: stroke-snapped thickness (wavy height = 3x thickness),
// origin rounded per component, width stroke-snapped, and the color
// taking the palette-Hsla opacity path (alpha only).
func underlineFor(origin Point, width float32, style UnderlineStyle, mask Bounds, scale, elementOpacity float32) Underline {
	thickness := snapStroke(style.Thickness, scale)
	height := thickness
	if style.Wavy {
		height = thickness * 3
	}
	color := Hsla{}
	if style.Color != nil {
		color = *style.Color
	}
	return Underline{
		Bounds: Bounds{
			Origin: Point{
				X: roundToDevicePixel(origin.X, scale),
				Y: roundToDevicePixel(origin.Y, scale),
			},
			Size: Size{Width: snapStroke(width, scale), Height: height},
		},
		ContentMask: snappedContentMask(mask, scale),
		Color:       OpacityHsla(color, elementOpacity),
		Thickness:   thickness,
		Wavy:        style.Wavy,
	}
}

// blurRadiusIsIdentity reports whether a logical blur radius is an
// identity filter (the pinned Filter::is_identity for Blur: radius <= 0).
func blurRadiusIsIdentity(radius float32) bool {
	return radius <= 0
}

// filterBoundaryFor builds the matched content-filter boundary pair
// snapshot, mirroring the pinned with_filter_layer_with_corner_
// smoothing: snapped bounds, snapped mask, scaled radii, the blur scaled
// into device pixels, and opacity 1.0 (NOT element opacity — the pinned
// comment: the group's children already carry it).
func filterBoundaryFor(bounds Bounds, blurRadius float32, radii Corners, mask Bounds, scale float32, cornerSmoothing float32) (FilterBoundary, bool) {
	if blurRadiusIsIdentity(blurRadius) {
		return FilterBoundary{}, false
	}
	return FilterBoundary{
		Bounds:          snapBounds(bounds, scale),
		ContentMask:     snappedContentMask(mask, scale),
		CornerRadii:     scaleCorners(radii, scale),
		CornerSmoothing: clamp01(cornerSmoothing),
		BlurRadius:      blurRadius * scale,
		Opacity:         1.0,
		IsStart:         true,
	}, true
}

// backdropFilterFor builds the scene BackdropFilter record, mirroring the
// pinned paint_backdrop_filter_with_corner_smoothing: snapped bounds,
// scaled radii, the blur scaled, and the ELEMENT OPACITY captured at
// paint time. The second return is false for identity filters (nothing
// is painted).
func backdropFilterFor(bounds Bounds, blurRadius float32, radii Corners, mask Bounds, scale, elementOpacity float32, cornerSmoothing float32) (BackdropFilter, bool) {
	if blurRadiusIsIdentity(blurRadius) {
		return BackdropFilter{}, false
	}
	return BackdropFilter{
		Bounds:          snapBounds(bounds, scale),
		ContentMask:     snappedContentMask(mask, scale),
		CornerRadii:     scaleCorners(radii, scale),
		CornerSmoothing: clamp01(cornerSmoothing),
		BlurRadius:      blurRadius * scale,
		Opacity:         elementOpacity,
	}, true
}

// Left/Top/Right/Bottom edge helpers (the pinned Bounds::left etc.).
func (b Bounds) Left() float32   { return b.Origin.X }
func (b Bounds) Top() float32    { return b.Origin.Y }
func (b Bounds) Right() float32  { return b.Origin.X + b.Size.Width }
func (b Bounds) Bottom() float32 { return b.Origin.Y + b.Size.Height }

// String renders a Bounds for diagnostics.
func (b Bounds) String() string {
	return fmt.Sprintf("(%g, %g, %g x %g)", b.Origin.X, b.Origin.Y, b.Size.Width, b.Size.Height)
}
