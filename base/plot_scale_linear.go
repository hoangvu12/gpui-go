package base

// The linear scale (U:plot/scale/linear.rs, after d3-scale's linear
// scale).
//
// Go adaptation: itertools' minmax_by over partial_cmp
// (U:plot/scale/linear.rs:3, 20-24) is a hand-written loop with the same
// tie rule (the first minimum, the last maximum; an incomparable pair
// counts as equal).

// ScaleLinear maps the extent of a numeric domain onto a range
// (U:plot/scale/linear.rs:7-13).
type ScaleLinear[T PlotValue] struct {
	domainStart T
	domainDiff  T
	rangeStart  float32
	rangeDiff   float32
}

// NewScaleLinear maps the extent of domain onto rng: the smallest domain
// value to rng[0] and the largest to rng[1], so a reversed range such as
// [height, 0] puts larger values higher (U:plot/scale/linear.rs:15-33).
func NewScaleLinear[T PlotValue](domain []T, rng [2]float32) ScaleLinear[T] {
	var lo, hi T
	for i, v := range domain {
		if i == 0 {
			lo, hi = v, v
			continue
		}
		if v < lo {
			lo = v
		}
		if !(v < hi) {
			hi = v
		}
	}
	return ScaleLinear[T]{
		domainStart: lo,
		domainDiff:  hi - lo,
		rangeStart:  rng[0],
		rangeDiff:   rng[1] - rng[0],
	}
}

// Tick implements Scale (U:plot/scale/linear.rs:35-45): false for an
// empty or single-valued domain.
func (s ScaleLinear[T]) Tick(value T) (float32, bool) {
	if s.domainDiff == 0 {
		return 0, false
	}
	ratio := float32((value - s.domainStart) / s.domainDiff)
	return ratio*s.rangeDiff + s.rangeStart, true
}

// NearestIndex implements Scale with the trait default: a linear scale
// has no discrete positions (U:plot/scale.rs:30-34).
func (s ScaleLinear[T]) NearestIndex(float32) int { return 0 }
