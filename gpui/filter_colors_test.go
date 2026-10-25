package gpui

import (
	"math"
	"testing"
)

// Ticket 41's CSS colour filter functions: the matrices against
// Chromium's source values, the chain composition, and the identity and
// clamping rules.
//
// Chromium citations (the sparse checkout, 0d4dcd4f):
//   - GrayscaleMatrix / SepiaMatrix: core/paint/filter_effect_builder.cc
//     66-127.
//   - SaturateMatrix / HueRotateMatrix: platform/graphics/filters/
//     fe_color_matrix.cc 58-96.
//   - invert / opacity / brightness / contrast: the component transfers
//     of core/paint/filter_effect_builder.cc 233-305 (table [x, 1-x],
//     table [0, x], linear slope x intercept 0, linear slope x
//     intercept -0.5x + 0.5).
//   - sRGB operating space: filter_effect_builder.cc 361-366.
//   - Amount clamping: core/css/resolver/filter_operation_resolver.cc
//     133-170 (grayscale/sepia/invert/opacity clamp to [0,1];
//     brightness/saturate/contrast do not; hue-rotate is degrees).

func TestFilterColorMatrixValues(t *testing.T) {
	const epsilon = 1e-6
	cases := []struct {
		name   string
		filter Filter
		want   [20]float32
	}{
		{
			"grayscale(1)",
			Grayscale(1),
			[20]float32{
				0.2126, 0.7152, 0.0722, 0, 0,
				0.2126, 0.7152, 0.0722, 0, 0,
				0.2126, 0.7152, 0.0722, 0, 0,
				0, 0, 0, 1, 0,
			},
		},
		{
			"grayscale(0.5)",
			Grayscale(0.5),
			[20]float32{
				0.6063, 0.3576, 0.0361, 0, 0,
				0.1063, 0.8576, 0.0361, 0, 0,
				0.1063, 0.3576, 0.5361, 0, 0,
				0, 0, 0, 1, 0,
			},
		},
		{
			"sepia(1)",
			Sepia(1),
			[20]float32{
				0.393, 0.769, 0.189, 0, 0,
				0.349, 0.686, 0.168, 0, 0,
				0.272, 0.534, 0.131, 0, 0,
				0, 0, 0, 1, 0,
			},
		},
		{
			"saturate(0.5)",
			Saturate(0.5),
			[20]float32{
				0.6065, 0.3575, 0.036, 0, 0,
				0.1065, 0.8575, 0.036, 0, 0,
				0.1065, 0.3575, 0.536, 0, 0,
				0, 0, 0, 1, 0,
			},
		},
		{
			"hue-rotate(90deg)",
			HueRotate(90),
			[20]float32{
				0, 0, 1, 0, 0,
				0.356, 0.855, -0.211, 0, 0,
				-0.574, 1.43, 0.144, 0, 0,
				0, 0, 0, 1, 0,
			},
		},
		{
			"invert(1)",
			Invert(1),
			[20]float32{
				-1, 0, 0, 0, 1,
				0, -1, 0, 0, 1,
				0, 0, -1, 0, 1,
				0, 0, 0, 1, 0,
			},
		},
		{
			"invert(0.25)",
			Invert(0.25),
			[20]float32{
				0.5, 0, 0, 0, 0.25,
				0, 0.5, 0, 0, 0.25,
				0, 0, 0.5, 0, 0.25,
				0, 0, 0, 1, 0,
			},
		},
		{
			"opacity(0.25)",
			OpacityFilter(0.25),
			[20]float32{
				1, 0, 0, 0, 0,
				0, 1, 0, 0, 0,
				0, 0, 1, 0, 0,
				0, 0, 0, 0.25, 0,
			},
		},
		{
			"brightness(1.75)",
			Brightness(1.75),
			[20]float32{
				1.75, 0, 0, 0, 0,
				0, 1.75, 0, 0, 0,
				0, 0, 1.75, 0, 0,
				0, 0, 0, 1, 0,
			},
		},
		{
			"contrast(1.5)",
			Contrast(1.5),
			[20]float32{
				1.5, 0, 0, 0, -0.25,
				0, 1.5, 0, 0, -0.25,
				0, 0, 1.5, 0, -0.25,
				0, 0, 0, 1, 0,
			},
		},
	}
	for _, tc := range cases {
		got, ok := tc.filter.colorMatrix()
		if !ok {
			t.Fatalf("%s: no colour matrix", tc.name)
		}
		for i := 0; i < 20; i++ {
			if math.Abs(float64(got[i]-tc.want[i])) > epsilon {
				t.Errorf("%s: matrix[%d] = %v, want %v (full: %v)", tc.name, i, got[i], tc.want[i], got)
			}
		}
	}
}

func TestFilterBlurHasNoColorMatrix(t *testing.T) {
	if _, ok := Blur(4).colorMatrix(); ok {
		t.Fatal("Blur reported a colour matrix")
	}
	if _, ok := Grayscale(0).colorMatrix(); !ok {
		// Identity functions still carry a matrix; IsIdentity drops them
		// at compose time.
		t.Log("grayscale(0) has a matrix (identity), dropped at compose")
	}
}

func TestFilterIsIdentityRules(t *testing.T) {
	cases := []struct {
		filter Filter
		want   bool
	}{
		{Blur(0), true},
		{Blur(4), false},
		{Grayscale(0), true},
		{Grayscale(0.25), false},
		{Grayscale(1), false},
		{Sepia(0), true},
		{Invert(0), true},
		{HueRotate(0), true},
		{HueRotate(360), false}, // not an exact multiple in float cos/sin
		{Saturate(1), true},
		{Saturate(0), false},
		{OpacityFilter(1), true},
		{OpacityFilter(0), false},
		{Brightness(1), true},
		{Brightness(0), false},
		{Contrast(1), true},
		{Contrast(2), false},
	}
	for _, tc := range cases {
		if got := tc.filter.IsIdentity(); got != tc.want {
			t.Errorf("%+v.IsIdentity() = %v, want %v", tc.filter, got, tc.want)
		}
	}
}

func TestFilterConstructorsClampAmounts(t *testing.T) {
	// grayscale/sepia/invert/opacity clamp to [0,1] (Chromium
	// filter_operation_resolver.cc:151-153); saturate/brightness/
	// contrast stay above 1; NaN collapses to 0.
	if got := Grayscale(2).Amount; got != 1 {
		t.Errorf("Grayscale(2).Amount = %v, want 1", got)
	}
	if got := Sepia(-1).Amount; got != 0 {
		t.Errorf("Sepia(-1).Amount = %v, want 0", got)
	}
	if got := Invert(1.5).Amount; got != 1 {
		t.Errorf("Invert(1.5).Amount = %v, want 1", got)
	}
	if got := OpacityFilter(-0.5).Amount; got != 0 {
		t.Errorf("OpacityFilter(-0.5).Amount = %v, want 0", got)
	}
	if got := Saturate(3).Amount; got != 3 {
		t.Errorf("Saturate(3).Amount = %v, want 3 (unclamped)", got)
	}
	if got := Brightness(2.5).Amount; got != 2.5 {
		t.Errorf("Brightness(2.5).Amount = %v, want 2.5 (unclamped)", got)
	}
	if got := Contrast(-1).Amount; got != 0 {
		t.Errorf("Contrast(-1).Amount = %v, want 0", got)
	}
	if got := Grayscale(float32(math.NaN())).Amount; got != 0 {
		t.Errorf("Grayscale(NaN).Amount = %v, want 0", got)
	}
	if got := HueRotate(float32(math.Inf(1))).Amount; got != 0 {
		t.Errorf("HueRotate(+Inf).Amount = %v, want 0", got)
	}
}

func TestFilterValueZeroIsBlur(t *testing.T) {
	// Filter{BlurRadius: r} literals keep meaning blur (the pinned
	// struct's zero function).
	if got := (Filter{BlurRadius: 4}).Function; got != FilterFunctionBlur {
		t.Errorf("Filter{BlurRadius: 4}.Function = %v, want FilterFunctionBlur", got)
	}
	if got := Blur(4); got != (Filter{BlurRadius: 4, Function: FilterFunctionBlur, Amount: 0}) {
		t.Errorf("Blur(4) = %+v", got)
	}
}

func TestComposeColorMatrixAppliesFirstFirst(t *testing.T) {
	// brightness(2) then invert(1): the invert's offset survives the
	// composition (out = A2·A1, offset = A2·b1 + b2), so a 50% gray
	// input (0.5) becomes 2·0.5=1 then 1-1=0.
	brightness, _ := Brightness(2).colorMatrix()
	invert, _ := Invert(1).colorMatrix()
	composed := composeColorMatrix(brightness, invert)
	apply := func(m [20]float32, r, g, b, a float32) [4]float32 {
		x := [4]float32{r, g, b, a}
		var out [4]float32
		for row := 0; row < 4; row++ {
			out[row] = m[5*row+4]
			for col := 0; col < 4; col++ {
				out[row] += m[5*row+col] * x[col]
			}
		}
		return out
	}
	if got := apply(composed, 0.5, 0.5, 0.5, 1); got != [4]float32{0, 0, 0, 1} {
		t.Errorf("invert(brightness(0.5)) = %v, want [0 0 0 1]", got)
	}
	// The reverse order differs: invert first maps 0.5 to 0.5, then
	// brightness doubles it.
	reversed := composeColorMatrix(invert, brightness)
	if got := apply(reversed, 0.5, 0.5, 0.5, 1); got != [4]float32{1, 1, 1, 1} {
		t.Errorf("brightness(invert(0.5)) = %v, want [1 1 1 1]", got)
	}
}

func TestComposeFilterChain(t *testing.T) {
	// A matrix-only chain carries one slot per function in order;
	// identity filters drop; the blur composes by quadrature.
	sepia, _ := Sepia(1).colorMatrix()
	hue, _ := HueRotate(90).colorMatrix()
	chain := composeFilterChain([]Filter{Sepia(1), HueRotate(90), Grayscale(0), Invert(0)})
	if chain.BlurRadius != 0 || chain.ColorMatrixCount != 2 {
		t.Fatalf("chain = %+v, want two matrix slots", chain)
	}
	var slots [2][20]float32
	copy(slots[0][:], chain.ColorMatrix[0:20])
	copy(slots[1][:], chain.ColorMatrix[20:40])
	if slots[0] != sepia || slots[1] != hue {
		t.Errorf("chain slots = %v, want sepia then hue-rotate UNcomposed (rev 15.1: the per-function clamp lives between them)", slots)
	}
	// The COMPOSED matrix (what rev 15 carried) would differ — the
	// order-sensitivity the per-function slots preserve.
	composed := composeColorMatrix(sepia, hue)
	if slots[1] == composed && composed != hue {
		t.Errorf("the chain was composed into one matrix; rev 15.1 carries them per function")
	}

	// blur(3) blur(4) = sqrt(3² + 4²) = 5.
	blurChain := composeFilterChain([]Filter{Blur(3), Blur(4)})
	if math.Abs(float64(blurChain.BlurRadius-5)) > 1e-5 || blurChain.ColorMatrixCount != 0 {
		t.Errorf("blur chain = %+v, want radius 5, no matrix", blurChain)
	}

	// blur + matrix: both payload halves.
	mixed := composeFilterChain([]Filter{Blur(3), Invert(1)})
	if mixed.BlurRadius != 3 || mixed.ColorMatrixCount != 1 {
		t.Errorf("mixed chain = %+v, want blur 3 and a matrix", mixed)
	}
	invert, _ := Invert(1).colorMatrix()
	var mixedSlot [20]float32
	copy(mixedSlot[:], mixed.ColorMatrix[0:20])
	if mixedSlot != invert {
		t.Errorf("mixed chain matrix = %v, want invert's", mixedSlot)
	}

	// An all-identity chain is empty (paints inline).
	if !(composeFilterChain([]Filter{Blur(0), Grayscale(0), Saturate(1)}).IsEmpty()) {
		t.Error("an identity chain is not empty")
	}
	if !(composeFilterChain(nil).IsEmpty()) {
		t.Error("the nil chain is not empty")
	}
}

func TestFilterChainIsEmptyRules(t *testing.T) {
	if !(FilterChain{}.IsEmpty()) {
		t.Error("the zero chain is not empty")
	}
	if (FilterChain{BlurRadius: 2}).IsEmpty() {
		t.Error("a blur-only chain is empty")
	}
	if (FilterChain{ColorMatrixCount: 1}).IsEmpty() {
		t.Error("a matrix-only chain is empty")
	}
}

func TestStyledFilterMethodsAppendInOrder(t *testing.T) {
	// .Grayscale(1).Blur(4) is the CSS filter: grayscale(1) blur(4) —
	// the chain in call order, appended like Blur.
	d := Div().Flex().Grayscale(1).Blur(4).Invert(0.5)
	filters, ok := d.StyleMut().Filter.Get()
	if !ok {
		t.Fatal("no filter chain")
	}
	want := []Filter{Grayscale(1), Blur(4), Invert(0.5)}
	if len(filters) != len(want) {
		t.Fatalf("chain = %+v, want %+v", filters, want)
	}
	for i := range want {
		if filters[i] != want[i] {
			t.Errorf("chain[%d] = %+v, want %+v", i, filters[i], want[i])
		}
	}
}
