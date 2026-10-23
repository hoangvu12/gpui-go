package base

// The band scale (U:plot/scale/band.rs, after d3-scale's band scale).
//
// Go adaptation: `T: Eq + Hash` is `T comparable`; the Rust HashMap of
// distinct domain values is a Go map.

// ScaleBand lays the distinct values of a domain out as bands across a
// range (U:plot/scale/band.rs:9-25).
//
// D3 keys its band domain through an InternMap, so a repeated value keeps
// the index of its first occurrence and the band count follows the
// distinct values, not the entry count.
type ScaleBand[T comparable] struct {
	indices      map[T]int
	bandCount    int
	maxBandWidth float32
	hasMaxWidth  bool
	rangeStart   float32
	rangeDiff    float32
	paddingInner float32
	paddingOuter float32
}

// NewScaleBand lays the distinct values of domain out as bands across
// rng, from its lower end in domain order (U:plot/scale/band.rs:28-49).
func NewScaleBand[T comparable](domain []T, rng [2]float32) ScaleBand[T] {
	indices := make(map[T]int, len(domain))
	for _, value := range domain {
		if _, ok := indices[value]; !ok {
			indices[value] = len(indices)
		}
	}
	return ScaleBand[T]{
		indices:    indices,
		rangeStart: rustMinF32(rng[0], rng[1]),
		rangeDiff:  abs32(rng[1] - rng[0]),
	}
}

// BandWidth is the width of a band: the range divided among the bands,
// less the inner padding, and no wider than MaxBandWidth when set
// (U:plot/scale/band.rs:51-57).
func (s ScaleBand[T]) BandWidth() float32 {
	width := s.avgWidth() * (1 - s.paddingInner)
	if s.hasMaxWidth {
		return rustMinF32(width, s.maxBandWidth)
	}
	return width
}

// MaxBandWidth caps the band width at width, so a few bands in a wide
// range stay narrow; a band still starts where it would uncapped. Unset by
// default (U:plot/scale/band.rs:59-64).
func (s ScaleBand[T]) MaxBandWidth(width float32) ScaleBand[T] {
	s.maxBandWidth = width
	s.hasMaxWidth = true
	return s
}

// Step is the distance between the starts of two adjacent bands: the band
// width plus the inner padding. The whole range for a single band
// (U:plot/scale/band.rs:66-74).
func (s ScaleBand[T]) Step() float32 {
	if s.len() <= 1 {
		return s.rangeDiff
	}
	return s.displayAvgWidth() * s.ratio()
}

// BandCount lays the range out for count bands, the domain taking the
// leading ones in order and the rest staying empty. A count below the
// domain's length has no effect (U:plot/scale/band.rs:76-82).
func (s ScaleBand[T]) BandCount(count int) ScaleBand[T] {
	s.bandCount = count
	return s
}

// PaddingInner sets the inner padding of the band
// (U:plot/scale/band.rs:84-88).
func (s ScaleBand[T]) PaddingInner(paddingInner float32) ScaleBand[T] {
	s.paddingInner = paddingInner
	return s
}

// PaddingOuter sets the outer padding of the band
// (U:plot/scale/band.rs:90-94).
func (s ScaleBand[T]) PaddingOuter(paddingOuter float32) ScaleBand[T] {
	s.paddingOuter = paddingOuter
	return s
}

// len is the number of bands: one per distinct domain value, or the band
// count when larger (U:plot/scale/band.rs:96-100).
func (s ScaleBand[T]) len() int { return max(len(s.indices), s.bandCount) }

// avgWidth is the range divided evenly among the bands
// (U:plot/scale/band.rs:102-110).
func (s ScaleBand[T]) avgWidth() float32 {
	n := float32(s.len())
	if n == 0 {
		return 0
	}
	return s.rangeDiff / n
}

// ratio is the band ratio (U:plot/scale/band.rs:112-115).
func (s ScaleBand[T]) ratio() float32 {
	return 1 + s.paddingInner/float32(s.len()-1)
}

// displayAvgWidth is the average band width for display
// (U:plot/scale/band.rs:117-121).
func (s ScaleBand[T]) displayAvgWidth() float32 {
	paddingOuterWidth := s.avgWidth() * s.paddingOuter
	return (s.rangeDiff - paddingOuterWidth*2) / float32(s.len())
}

// Tick implements Scale (U:plot/scale/band.rs:128-140).
func (s ScaleBand[T]) Tick(value T) (float32, bool) {
	index, ok := s.indices[value]
	if !ok {
		return 0, false
	}
	// When there's only one element, place it in the center.
	if s.len() == 1 {
		return s.rangeStart + (s.rangeDiff-s.BandWidth())/2, true
	}
	avgWidth := s.displayAvgWidth()
	paddingOuterWidth := s.avgWidth() * s.paddingOuter
	return s.rangeStart + float32(index)*avgWidth*s.ratio() + paddingOuterWidth, true
}

// NearestIndex implements Scale (U:plot/scale/band.rs:142-159).
func (s ScaleBand[T]) NearestIndex(tick float32) int {
	n := s.len()
	if n <= 1 {
		return 0
	}
	avgWidth := s.displayAvgWidth()
	paddingOuterWidth := s.avgWidth() * s.paddingOuter
	adjusted := tick - s.rangeStart - paddingOuterWidth
	index := plotRoundInt(adjusted / (avgWidth * s.ratio()))
	return min(max(index, 0), n-1)
}
