package gpui

import "math"

// The CSS colour filter functions of browser-core ticket 41: the seven
// shorthand functions the Filter Effects spec defines as colour
// matrices and component transfers (grayscale, sepia, saturate,
// hue-rotate, invert, opacity, brightness, contrast), composed into the
// one 4x5 matrix the native rev-15 filter record carries
// (reference/native/SCENE_ABI.md, GpuiGoSceneFilterRecord).
//
// The matrices are Chromium's, which follow the Filter Effects spec:
//
//   - grayscale and sepia: filter_effect_builder.cc's GrayscaleMatrix /
//     SepiaMatrix (third_party/blink/renderer/core/paint/
//     filter_effect_builder.cc:66-127), the luminance weights
//     0.2126/0.7152/0.0722 the spec's grayscaleEquivalent /
//     sepiaEquivalent sections give.
//   - saturate and hue-rotate: fe_color_matrix.cc's SaturateMatrix /
//     HueRotateMatrix (third_party/blink/renderer/platform/graphics/
//     filters/fe_color_matrix.cc:58-96), the spec's rounded
//     0.213/0.715/0.072 coefficients.
//   - invert, opacity, brightness, contrast: component transfers
//     (filter_effect_builder.cc:233-305). invert is a table transfer
//     [x, 1-x] — linear interpolation gives C' = (1-2x)·C + x
//     (fe_component_transfer.cc:49-62); opacity is the alpha table
//     [0, x] — A' = x·A; brightness is a linear transfer with slope x
//     and intercept 0; contrast a linear transfer with slope x and
//     intercept -0.5·x + 0.5 (fe_component_transfer.cc:64-73).
//
// Color space: Chromium forces sRGB for the CSS shorthand functions
// (filter_effect_builder.cc:361-366, SetOperatingInterpolationSpace
// (kInterpolationSpaceSRGB); SVG filters default to linearRGB, the CSS
// shorthands do not), so the matrices multiply sRGB channel values
// directly — the same space the native composite applies the matrix in.
//
// Chains compose in order: Chromium's FilterEffectBuilder walks the
// operation list chaining each effect's input to the previous one
// (filter_effect_builder.cc:326-373), so the first listed function is
// applied first; the composed matrix is the product applied in that
// order. A per-pixel linear map commutes with the uniform-per-channel
// gaussian blur (the native record's recorded reasoning), so
// blur-then-matrix — the native draw order — is the CSS result for any
// interleaving of blurs and colour functions.

// FilterFunction selects a filter's CSS function (the pinned Filter
// enum's single variant plus ticket 41's colour functions).
type FilterFunction uint8

const (
	// FilterFunctionBlur is the gaussian blur, blur(radius)
	// (Filter::Blur, the pinned variant; the zero value so a plain
	// Filter{BlurRadius: r} keeps meaning blur).
	FilterFunctionBlur FilterFunction = iota
	// FilterFunctionGrayscale is grayscale(amount).
	FilterFunctionGrayscale
	// FilterFunctionSepia is sepia(amount).
	FilterFunctionSepia
	// FilterFunctionSaturate is saturate(amount).
	FilterFunctionSaturate
	// FilterFunctionHueRotate is hue-rotate(degrees).
	FilterFunctionHueRotate
	// FilterFunctionInvert is invert(amount).
	FilterFunctionInvert
	// FilterFunctionOpacity is opacity(amount).
	FilterFunctionOpacity
	// FilterFunctionBrightness is brightness(amount).
	FilterFunctionBrightness
	// FilterFunctionContrast is contrast(amount).
	FilterFunctionContrast
)

// Grayscale returns the grayscale filter: amount 0 leaves the element
// unchanged and 1 makes it entirely gray (CSS filter: grayscale, a
// number or percentage clamped to [0,1] — Chromium clamps at resolve,
// filter_operation_resolver.cc:151-153). Amounts are fractions: 0.5 is
// CSS grayscale(50%).
func Grayscale(amount float32) Filter {
	return Filter{Function: FilterFunctionGrayscale, Amount: filterClampedAmount(amount)}
}

// Sepia returns the sepia filter: amount 0 leaves the element unchanged
// and 1 makes it entirely sepia (CSS filter: sepia, clamped to [0,1],
// filter_operation_resolver.cc:151-153).
func Sepia(amount float32) Filter {
	return Filter{Function: FilterFunctionSepia, Amount: filterClampedAmount(amount)}
}

// Saturate returns the saturate filter: amount 0 is fully desaturated,
// 1 unchanged and values over 1 oversaturate (CSS filter: saturate; not
// clamped at the top, filter_operation_resolver.cc:154-156).
func Saturate(amount float32) Filter {
	return Filter{Function: FilterFunctionSaturate, Amount: filterNonNegative(amount)}
}

// HueRotate returns the hue-rotate filter: degrees of hue rotation
// around the color wheel, 0 (or any multiple of 360) unchanged (CSS
// filter: hue-rotate; degrees, unclamped,
// filter_operation_resolver.cc:167-174).
func HueRotate(degrees float32) Filter {
	return Filter{Function: FilterFunctionHueRotate, Amount: filterFinite(degrees)}
}

// Invert returns the invert filter: amount 0 leaves the element
// unchanged and 1 fully inverts it (CSS filter: invert, clamped to
// [0,1], filter_operation_resolver.cc:151-153).
func Invert(amount float32) Filter {
	return Filter{Function: FilterFunctionInvert, Amount: filterClampedAmount(amount)}
}

// OpacityFilter returns the filter opacity function: amount 0 makes the
// element fully transparent and 1 unchanged (CSS filter: opacity — the
// filter function, not the opacity property, clamped to [0,1],
// filter_operation_resolver.cc:151-153).
func OpacityFilter(amount float32) Filter {
	return Filter{Function: FilterFunctionOpacity, Amount: filterClampedAmount(amount)}
}

// Brightness returns the brightness filter: amount 0 makes the element
// black, 1 unchanged and values over 1 brighten it (CSS filter:
// brightness; not clamped at the top,
// filter_operation_resolver.cc:154-156).
func Brightness(amount float32) Filter {
	return Filter{Function: FilterFunctionBrightness, Amount: filterNonNegative(amount)}
}

// Contrast returns the contrast filter: amount 0 makes the element
// gray, 1 unchanged and values over 1 increase the contrast (CSS
// filter: contrast; not clamped at the top,
// filter_operation_resolver.cc:154-156).
func Contrast(amount float32) Filter {
	return Filter{Function: FilterFunctionContrast, Amount: filterNonNegative(amount)}
}

// filterClampedAmount clamps a [0,1] amount (the grayscale/sepia/
// invert/opacity rule; NaN collapses to 0).
func filterClampedAmount(amount float32) float32 {
	switch {
	case math.IsNaN(float64(amount)):
		return 0
	case amount < 0:
		return 0
	case amount > 1:
		return 1
	}
	return amount
}

// filterNonNegative clamps a non-negative amount (saturate, brightness
// and contrast: CSS rejects negative values at parse time; NaN
// collapses to 0).
func filterNonNegative(amount float32) float32 {
	switch {
	case math.IsNaN(float64(amount)):
		return 0
	case amount < 0:
		return 0
	}
	return amount
}

// filterFinite zeroes non-finite values (hue-rotate's degrees).
func filterFinite(degrees float32) float32 {
	if math.IsNaN(float64(degrees)) || math.IsInf(float64(degrees), 0) {
		return 0
	}
	return degrees
}

// colorMatrixOf returns the filter's 4x5 row-major colour matrix
// operating on non-premultiplied RGBA (row i: five floats, the fifth
// the additive offset). The second return is false for blur and
// identity functions (no matrix payload).
func (f Filter) colorMatrix() ([20]float32, bool) {
	x := f.Amount
	var m [20]float32
	switch f.Function {
	case FilterFunctionGrayscale:
		// Chromium GrayscaleMatrix (filter_effect_builder.cc:66-99).
		n := 1 - x
		m = [20]float32{
			0.2126 + 0.7874*n, 0.7152 - 0.7152*n, 0.0722 - 0.0722*n, 0, 0,
			0.2126 - 0.2126*n, 0.7152 + 0.2848*n, 0.0722 - 0.0722*n, 0, 0,
			0.2126 - 0.2126*n, 0.7152 - 0.7152*n, 0.0722 + 0.9278*n, 0, 0,
			0, 0, 0, 1, 0,
		}
	case FilterFunctionSepia:
		// Chromium SepiaMatrix (filter_effect_builder.cc:101-127).
		n := 1 - x
		m = [20]float32{
			0.393 + 0.607*n, 0.769 - 0.769*n, 0.189 - 0.189*n, 0, 0,
			0.349 - 0.349*n, 0.686 + 0.314*n, 0.168 - 0.168*n, 0, 0,
			0.272 - 0.272*n, 0.534 - 0.534*n, 0.131 + 0.869*n, 0, 0,
			0, 0, 0, 1, 0,
		}
	case FilterFunctionSaturate:
		// Chromium SaturateMatrix (fe_color_matrix.cc:58-73).
		m = [20]float32{
			0.213 + 0.787*x, 0.715 - 0.715*x, 0.072 - 0.072*x, 0, 0,
			0.213 - 0.213*x, 0.715 + 0.285*x, 0.072 - 0.072*x, 0, 0,
			0.213 - 0.213*x, 0.715 - 0.715*x, 0.072 + 0.928*x, 0, 0,
			0, 0, 0, 1, 0,
		}
	case FilterFunctionHueRotate:
		// Chromium HueRotateMatrix (fe_color_matrix.cc:75-96).
		radians := float64(x) * (math.Pi / 180)
		c, s := float32(math.Cos(radians)), float32(math.Sin(radians))
		m = [20]float32{
			0.213 + c*0.787 - s*0.213, 0.715 - c*0.715 - s*0.715, 0.072 - c*0.072 + s*0.928, 0, 0,
			0.213 - c*0.213 + s*0.143, 0.715 + c*0.285 + s*0.140, 0.072 - c*0.072 - s*0.283, 0, 0,
			0.213 - c*0.213 - s*0.787, 0.715 - c*0.715 + s*0.715, 0.072 + c*0.928 + s*0.072, 0, 0,
			0, 0, 0, 1, 0,
		}
	case FilterFunctionInvert:
		// The table transfer [x, 1-x]: C' = (1-2x)·C + x
		// (filter_effect_builder.cc:233-251, fe_component_transfer.cc:49-62).
		m = [20]float32{
			1 - 2*x, 0, 0, 0, x,
			0, 1 - 2*x, 0, 0, x,
			0, 0, 1 - 2*x, 0, x,
			0, 0, 0, 1, 0,
		}
	case FilterFunctionOpacity:
		// The alpha table [0, x]: A' = x·A
		// (filter_effect_builder.cc:252-269).
		m = [20]float32{
			1, 0, 0, 0, 0,
			0, 1, 0, 0, 0,
			0, 0, 1, 0, 0,
			0, 0, 0, x, 0,
		}
	case FilterFunctionBrightness:
		// The linear transfer slope x, intercept 0
		// (filter_effect_builder.cc:270-287).
		m = [20]float32{
			x, 0, 0, 0, 0,
			0, x, 0, 0, 0,
			0, 0, x, 0, 0,
			0, 0, 0, 1, 0,
		}
	case FilterFunctionContrast:
		// The linear transfer slope x, intercept -0.5·x + 0.5
		// (filter_effect_builder.cc:288-305).
		o := 0.5 - 0.5*x
		m = [20]float32{
			x, 0, 0, 0, o,
			0, x, 0, 0, o,
			0, 0, x, 0, o,
			0, 0, 0, 1, 0,
		}
	default:
		return m, false
	}
	return m, true
}

// composeColorMatrix composes two colour matrices: first applied first,
// then second — the CSS chain order (out = A2·A1, offset =
// A2·b1 + b2). Both operate on non-premultiplied RGBA.
func composeColorMatrix(first, second [20]float32) [20]float32 {
	var out [20]float32
	for row := 0; row < 4; row++ {
		for col := 0; col < 4; col++ {
			sum := float32(0)
			for k := 0; k < 4; k++ {
				sum += second[5*row+k] * first[5*k+col]
			}
			out[5*row+col] = sum
		}
		offset := second[5*row+4]
		for k := 0; k < 4; k++ {
			offset += second[5*row+k] * first[5*k+4]
		}
		out[5*row+4] = offset
	}
	return out
}

// FilterChain is a filter chain's composed scene payload: the chain's
// gaussian blurs composed into one radius and its colour-matrix filters
// composed into one 4x5 matrix — the native rev-15 filter record's
// payload (SCENE_ABI.md, GpuiGoSceneFilterRecord: one composed matrix,
// blur entries read their radius in chain order).
//
// The blur composition is exact: sequential gaussian blurs equal one
// gaussian whose standard deviation is the quadrature sum of theirs
// (a convolution of gaussians), so the radii compose the same way —
// sqrt(r1² + r2²) — under any linear radius/sigma convention. The
// matrix composition is exact in chain order (composeColorMatrix); a
// per-pixel linear map commutes with the uniform-per-channel gaussian,
// so the native draw order (blur, then the matrix in the composite) is
// the CSS result for any interleaving.
type FilterChain struct {
	// BlurRadius is the composed blur radius, logical pixels at paint
	// time (scaled to device pixels on the way into the record, like
	// FilterBoundary.BlurRadius).
	BlurRadius float32
	// ColorMatrix is the composed row-major 4x5 colour matrix, valid
	// when ColorMatrixCount > 0.
	ColorMatrix [20]float32
	// ColorMatrixCount is 0 (no colour-matrix filters in the chain) or
	// 1 (the composed payload of any number of them).
	ColorMatrixCount int
}

// IsEmpty reports a chain with no visible filter (every entry
// identity), which paints inline with no isolation group.
func (c FilterChain) IsEmpty() bool {
	return c.BlurRadius <= 0 && c.ColorMatrixCount == 0
}

// composeFilterChain composes a style filter chain into its scene
// payload: identity filters drop out (CSS no-ops), blurs compose by
// quadrature and the colour-matrix filters compose in chain order
// (FilterChain).
func composeFilterChain(filters []Filter) FilterChain {
	var chain FilterChain
	for _, f := range filters {
		if f.IsIdentity() {
			continue
		}
		if matrix, ok := f.colorMatrix(); ok {
			if chain.ColorMatrixCount == 0 {
				chain.ColorMatrix = matrix
			} else {
				chain.ColorMatrix = composeColorMatrix(chain.ColorMatrix, matrix)
			}
			chain.ColorMatrixCount = 1
			continue
		}
		chain.BlurRadius = filterComposeRadius(chain.BlurRadius, f.BlurRadius)
	}
	return chain
}

// filterComposeRadius composes two gaussian blur radii by quadrature
// (sequential gaussians: σ² adds, so the radii compose the same way).
func filterComposeRadius(first, second float32) float32 {
	if second <= 0 {
		return first
	}
	if first <= 0 {
		return second
	}
	return float32(math.Sqrt(float64(first)*float64(first) + float64(second)*float64(second)))
}
