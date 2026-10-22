package base

// The point scale (U:plot/scale/point.rs, after d3-scale's point scale).
//
// Go adaptation: `T: PartialEq` is `T comparable`; tick_at returns
// Option<f32> as (value, ok).

// ScalePoint maps discrete domain values to continuous range positions
// (U:plot/scale/point.rs:5-14). Points are evenly distributed across the
// range, with the first and last points aligned to the range boundaries.
type ScalePoint[T comparable] struct {
	domain     []T
	rangeStart float32
	rangeTick  float32
}

// NewScalePoint places domain evenly from rng[0] to rng[1]; a single value
// sits in the middle of the range (U:plot/scale/point.rs:20-46).
func NewScalePoint[T comparable](domain []T, rng [2]float32) ScalePoint[T] {
	d := append([]T(nil), domain...)
	rangeDiff := rng[1] - rng[0]
	var start, tick float32
	switch len(d) {
	case 0:
	case 1:
		start, tick = rng[0], rangeDiff
	default:
		start, tick = rng[0], rangeDiff/float32(len(d)-1)
	}
	return ScalePoint[T]{domain: d, rangeStart: start, rangeTick: tick}
}

// TickAt returns the position of the domain value at index
// (U:plot/scale/point.rs:48-64): Tick of domain[index] for a domain of
// unique values, without searching the domain.
func (s ScalePoint[T]) TickAt(index int) (float32, bool) {
	n := len(s.domain)
	if index < 0 || index >= n {
		return 0, false
	}
	if n == 1 {
		return s.rangeStart + s.rangeTick/2, true
	}
	return s.rangeStart + float32(index)*s.rangeTick, true
}

// Tick implements Scale (U:plot/scale/point.rs:71-74).
func (s ScalePoint[T]) Tick(value T) (float32, bool) {
	for i, v := range s.domain {
		if v == value {
			return s.TickAt(i)
		}
	}
	return 0, false
}

// NearestIndex implements Scale (U:plot/scale/point.rs:76-88).
func (s ScalePoint[T]) NearestIndex(tick float32) int {
	if len(s.domain) == 0 || s.rangeTick == 0 {
		return 0
	}
	index := plotRoundUsize((tick - s.rangeStart) / s.rangeTick)
	return min(index, len(s.domain)-1)
}
