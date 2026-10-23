package base

// Plot scales (U:plot/scale.rs, U:plot/scale/sealed.rs): the PlotValue
// number set and the Scale trait every scale implements.
//
// Go adaptations (recorded deviations):
//
//   - PlotValue is sealed upstream over f32, f64 and, with the `decimal`
//     feature, rust_decimal::Decimal (U:plot/scale.rs:14-24,
//     U:plot/scale/sealed.rs:1-7). The Go constraint is ~float32 |
//     ~float64; the optional Decimal impl is not ported (no third-party
//     decimal module, dependency decision 01).
//   - Scale::tick returns Option<f32>; the Go method returns (value, ok).
//   - Scale::nearest_index has a default body returning 0
//     (U:plot/scale.rs:30-34); Go interfaces carry no defaults, so every
//     scale implements NearestIndex (ScaleLinear returns 0).

import "math"

// PlotValue is a number a ScaleLinear and the charts built on it can
// place: float32 and float64 (U:plot/scale.rs:14-19). Integer types are
// left out on purpose, because a linear scale divides values and integer
// division would truncate every position.
type PlotValue interface {
	~float32 | ~float64
}

// Scale maps domain values to positions (U:plot/scale.rs:26-35).
type Scale[T any] interface {
	// Tick returns the position of value, or false when the scale cannot
	// place it.
	Tick(value T) (float32, bool)
	// NearestIndex returns the index of the domain value whose position
	// is nearest tick; 0 for an empty domain or a scale without discrete
	// positions.
	NearestIndex(tick float32) int
}

// plotRoundInt is Rust's `f32::round` (half away from zero) followed by a
// saturating `as` cast to a signed integer (NaN becomes 0).
func plotRoundInt(v float32) int {
	if v != v {
		return 0
	}
	r := float32(math.Round(float64(v)))
	const maxInt = int(^uint(0) >> 1)
	if r >= float32(maxInt) {
		return maxInt
	}
	if r <= float32(-maxInt-1) {
		return -maxInt - 1
	}
	return int(r)
}

// plotRoundUsize is `f32::round` then a saturating `as usize` cast: negatives
// and NaN become 0.
func plotRoundUsize(v float32) int {
	n := plotRoundInt(v)
	if n < 0 {
		return 0
	}
	return n
}
