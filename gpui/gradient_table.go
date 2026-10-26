package gpui



// This file is the browser-core ticket 52 (rev 16) public surface: the
// scene-level gradient table (n-stop, radial, conic and repeating
// gradients with the baked 1024x1 premultiplied-sRGB ramp rows), the
// clip-path coverage entries, the drop-shadow filter-op chains and the
// effect groups' blend and mask payload. The STYLING that emits them
// (the class layer's bg-radial/conic, mask-image, clip-path,
// mix-blend-mode and the filter builder) is tickets 55/57; this file is
// the scene encoding the styling and the tests drive.
//
// The 2-stop linear gradient keeps the R15 record (BackgroundKind
// LinearGradient) bit-exact; the table path is for more stops, radial,
// conic, repeating, and the gradient masks.

// BlendMode is a CSS compositing blend mode (the W3C compositing-1
// §10 tags; the effect groups' composite applies the formula against a
// backdrop copy, per the design's §3.7).
type BlendMode uint8

const (
	// BlendNormal is source-over (no blending).
	BlendNormal BlendMode = 0
	// BlendMultiply is B(Cb, Cs) = Cb × Cs.
	BlendMultiply BlendMode = 1
	// BlendScreen is B(Cb, Cs) = Cb + Cs − (Cb × Cs).
	BlendScreen BlendMode = 2
	// BlendOverlay is HardLight(Cs, Cb) (hard-light with swapped
	// arguments).
	BlendOverlay BlendMode = 3
	// BlendDarken is B(Cb, Cs) = min(Cb, Cs).
	BlendDarken BlendMode = 4
	// BlendLighten is B(Cb, Cs) = max(Cb, Cs).
	BlendLighten BlendMode = 5
	// BlendColorDodge brightens the backdrop toward the source.
	BlendColorDodge BlendMode = 6
	// BlendColorBurn darkens the backdrop toward the source.
	BlendColorBurn BlendMode = 7
	// BlendHardLight multiplies or screens depending on the source.
	BlendHardLight BlendMode = 8
	// BlendSoftLight darkens or lightens with a diffused spotlight.
	BlendSoftLight BlendMode = 9
	// BlendDifference is B(Cb, Cs) = |Cb − Cs|.
	BlendDifference BlendMode = 10
	// BlendExclusion is B(Cb, Cs) = Cb + Cs − 2·Cb·Cs.
	BlendExclusion BlendMode = 11
	// BlendHue takes the source's hue and the backdrop's saturation
	// and luminosity.
	BlendHue BlendMode = 12
	// BlendSaturation takes the source's saturation and the backdrop's
	// hue and luminosity.
	BlendSaturation BlendMode = 13
	// BlendColor takes the source's hue and saturation and the
	// backdrop's luminosity.
	BlendColor BlendMode = 14
	// BlendLuminosity takes the source's luminosity and the backdrop's
	// hue and saturation.
	BlendLuminosity BlendMode = 15
	// BlendPlusLighter adds the premultiplied source and backdrop with
	// a per-channel clamp (Skia kPlus, "clamp(s+d)").
	BlendPlusLighter BlendMode = 16
)

// MaskKind is an effect group's mask source kind (the design's
// appendix A.1).
type MaskKind uint8

const (
	// MaskKindNone carries no mask.
	MaskKindNone MaskKind = 0
	// MaskKindGradient composites dst_in against a gradient table
	// entry (mask_reference is the table index): an edge fade is a
	// linear alpha ramp, an image mask is a many-stop gradient.
	MaskKindGradient MaskKind = 1
	// MaskKindAtlas composites dst_in against an atlas texture
	// (mask_reference is the POLYCHROME texture index; the whole
	// texture stretches over the group's bounds - the mask-image
	// contract).
	MaskKindAtlas MaskKind = 2
	// MaskKindCoverage composites dst_in against a clip-path coverage
	// entry (mask_reference is the coverage index).
	MaskKindCoverage MaskKind = 3
)

// MaskMode selects what the mask source's pixels contribute (the
// design's §3.5).
type MaskMode uint8

const (
	// MaskModeAlpha uses the sample's alpha (the default).
	MaskModeAlpha MaskMode = 0
	// MaskModeLuminance uses the sample's REC-601 luminance (Chromium's
	// luminance-to-alpha mask filter).
	MaskModeLuminance MaskMode = 1
)

// GradientKind is a gradient table entry's geometry kind (the design's
// appendix A.3).
type GradientKind uint8

const (
	// GradientLinear runs along the record's [x0, y0, x1, y1] line (the
	// CSS gradient line's endpoints resolved by the caller).
	GradientLinear GradientKind = 0
	// GradientRadial runs over the ellipse-normalized radius around
	// [cx, cy]: the radii in ry units and the rx/ry aspect.
	GradientRadial GradientKind = 1
	// GradientConic sweeps clockwise from up around [cx, cy], starting
	// at start_angle, one period per rotation radians.
	GradientConic GradientKind = 2
)

// GradientTileMode wraps the gradient's parameter before the ramp is
// sampled (CSS repeating-* sets the repeat tile).
type GradientTileMode uint8

const (
	// GradientTilePad clamps at the ramp's own end colors.
	GradientTilePad GradientTileMode = 0
	// GradientTileRepeat wraps with fract(t) (CSS repeating-*).
	GradientTileRepeat GradientTileMode = 1
	// GradientTileReflect mirrors around the ends (a triangle wave).
	GradientTileReflect GradientTileMode = 2
)

// TableGradientStop is one color stop of a table gradient: a color and
// its position in [0, 1] along the gradient's parameter (a CSS
// percentage divided by 100). CSS's stop fixup (ordering, the 0/1 end
// rules) is this API's caller's responsibility - the scene kernel
// interpolates the fixed stops.
type TableGradientStop struct {
	// Color of the stop (straight sRGBA).
	Color Rgba
	// Position along the gradient's parameter in [0, 1].
	Position float32
}

// Gradient is one scene-level gradient table entry (rev 16, ticket 52;
// the design's §3.8): the kind, tile mode, geometry (device pixels -
// the caller resolves the CSS endpoint/shape math) and up to 64 stops.
// The scene bakes the stops into one 1024x1 premultiplied-sRGB ramp
// row and the quads/shadows/gradient masks sample it with linear
// filtering (hardware interpolation of premultiplied sRGB bytes IS
// CSS's interpolation rule).
type Gradient struct {
	// Kind of the gradient's geometry.
	Kind GradientKind
	// Tile wraps the gradient's parameter (CSS repeating-*).
	Tile GradientTileMode
	// Geometry, device pixels: linear [x0, y0, x1, y1]; radial [cx, cy,
	// r0, r1, aspect] (radii in ry units, aspect = rx/ry - 1 for a
	// circle); conic [cx, cy, start_angle, rotation] (radians,
	// clockwise from up; rotation = 2π for a full turn).
	Geometry [8]float32
	// Stops in position order (1..=64; above 64 the insert resamples
	// down and warns in debug builds).
	Stops []TableGradientStop
}

// MaxTableGradientStops is the gradient table's stop cap (rev 16,
// ticket 52; the user's 2026-10-24 decision - the design text's 32 was
// raised to 64).
const MaxTableGradientStops = 64

// GradientDebugWarnf is the resampling warning hook (the >64-stop
// resample fires it once per gradient; the message carries the stop
// count and the cap). The default is silent (production builds); tests
// swap it to observe the warning, and debug tooling can point it at a
// logger.
var GradientDebugWarnf = func(format string, args ...any) {}

// InsertGradient inserts one gradient table entry and returns its
// table index (rev 16, ticket 52): the stops are baked into a 1024x1
// premultiplied-sRGB ramp row by the native service; quads reference
// the entry through TableGradientBackground and effect groups through
// the gradient mask kind. Above MaxTableGradientStops stops the
// gradient is RESAMPLED down to 64 evenly-spaced stops (the ramp is a
// 64-stop sampling of the original's interpolation - the visual
// approximation the design's §8.1 answer 4 chose) with a debug-build
// warning.
func (s *Scene) InsertGradient(g Gradient) (uint32, error) {
	return s.kernel.insertGradient(g)
}

// ResampleTableGradientStops resamples a stop list above the cap down
// to exactly 64 evenly-spaced stops (rev 16, ticket 52): each output
// stop evaluates the input gradient's interpolation at t = i/63, the
// piecewise-linear premultiplied mix between the surrounding stops.
// The output reproduces the ramp's own resolution (the GPU samples the
// baked 1024-texel row with linear filtering, so a 64-stop resample of
// an n-stop gradient lands within the same quantization). Pure
// function; exported for the tests and the styling layer.
func ResampleTableGradientStops(stops []TableGradientStop) []TableGradientStop {
	if len(stops) <= MaxTableGradientStops {
		return stops
	}
	out := make([]TableGradientStop, MaxTableGradientStops)
	for i := 0; i < MaxTableGradientStops; i++ {
		t := float32(i) / float32(MaxTableGradientStops-1)
		color, position := tableGradientAt(stops, t)
		out[i] = TableGradientStop{Color: color, Position: position}
	}
	// The resampled positions are the even grid (t = i/63); the colors
	// carry the original gradient's shape.
	return out
}

// tableGradientAt evaluates one fixed stop list's interpolated color
// at t (premultiplied linear interpolation, the ramp bake's own rule)
// and returns it with the position t itself.
func tableGradientAt(stops []TableGradientStop, t float32) (Rgba, float32) {
	if len(stops) == 0 {
		return Rgba{}, t
	}
	if t <= stops[0].Position {
		return stops[0].Color, t
	}
	last := stops[len(stops)-1]
	if t >= last.Position {
		return last.Color, t
	}
	index := 0
	for candidate := 1; candidate < len(stops); candidate++ {
		if stops[candidate].Position <= t {
			index = candidate
		}
	}
	left, right := stops[index], stops[index+1]
	span := right.Position - left.Position
	factor := float32(0)
	if span > 0 {
		factor = (t - left.Position) / span
	}
	l := left.Color
	r := right.Color
	return Rgba{
		R: l.R + factor*(r.R-l.R),
		G: l.G + factor*(r.G-l.G),
		B: l.B + factor*(r.B-l.B),
		A: l.A + factor*(r.A-l.A),
	}, t
}

// InsertCoverage inserts one clip-path coverage entry (rev 16, ticket
// 52; the design's §3.6): the entry references the scene's PATH record
// (the tessellated path, inserted before) plus its device bounds; the
// renderer rasterizes the path into the per-surface A8 coverage target
// and the effect groups' coverage masks sample it. Returns the coverage
// index the groups reference.
func (s *Scene) InsertCoverage(pathIndex uint32, bounds Bounds) (uint32, error) {
	return s.kernel.insertCoverage(pathIndex, bounds)
}

// FilterOp is one drop-shadow op of a filter-op chain (rev 16, ticket
// 52; the design's §3.9): the offset, the CSS blur radius (σ =
// radius/2 at render, the CSS shadow rule) and the shadow color.
type FilterOp struct {
	// OffsetX and OffsetY shift the shadow, device pixels.
	OffsetX, OffsetY float32
	// BlurRadius is the CSS blur radius, device pixels (σ = radius/2).
	BlurRadius float32
	// Color of the shadow (straight sRGBA).
	Color Rgba
}

// InsertFilterOpChain inserts one drop-shadow op chain (rev 16, ticket
// 52) and returns the chain index the filter records reference through
// their appended op-chain fields (OpChainIndex/OpChainCount; 0/0 = the
// R15 legacy chain, bit-exact).
func (s *Scene) InsertFilterOpChain(ops []FilterOp) (uint32, error) {
	return s.kernel.insertFilterOpChain(ops)
}

// TableGradientBackground builds a Background that references the
// gradient table entry `index` (rev 16, ticket 52): the quad/shadow
// fills with the table gradient (the R15 2-stop record keeps its own
// arm, bit-exact). The referenced gradient must be inserted BEFORE the
// primitive that carries this background.
func TableGradientBackground(index uint32) Background {
	return Background{
		Kind:          BackgroundKindGradientTable,
		GradientTable: index,
	}
}

