package base

// The ordinal scale (U:plot/scale/ordinal.rs, after d3-scale's ordinal
// scale).
//
// Go adaptation: `D: PartialEq` is `D comparable`; Map returns
// Option<R> as (value, ok).

// ScaleOrdinal maps a discrete domain onto a discrete range, cycling the
// range (U:plot/scale/ordinal.rs:3-8).
type ScaleOrdinal[D comparable, R any] struct {
	domain     []D
	rng        []R
	unknown    R
	hasUnknown bool
}

// NewScaleOrdinal returns an ordinal scale over domain and rng
// (U:plot/scale/ordinal.rs:21-27). The zero value is the Default scale
// (U:plot/scale/ordinal.rs:10-18).
func NewScaleOrdinal[D comparable, R any](domain []D, rng []R) ScaleOrdinal[D, R] {
	return ScaleOrdinal[D, R]{domain: domain, rng: rng}
}

// Domain sets the domain to the specified values
// (U:plot/scale/ordinal.rs:29-33).
func (s ScaleOrdinal[D, R]) Domain(domain []D) ScaleOrdinal[D, R] {
	s.domain = domain
	return s
}

// Range sets the range of the scale to the specified values
// (U:plot/scale/ordinal.rs:35-39).
func (s ScaleOrdinal[D, R]) Range(rng []R) ScaleOrdinal[D, R] {
	s.rng = rng
	return s
}

// Unknown sets the output for unknown input values
// (U:plot/scale/ordinal.rs:41-45).
func (s ScaleOrdinal[D, R]) Unknown(unknown R) ScaleOrdinal[D, R] {
	s.unknown = unknown
	s.hasUnknown = true
	return s
}

// Map returns the range value of a domain value, cycling the range; an
// unknown value maps to Unknown when set (U:plot/scale/ordinal.rs:53-64).
func (s ScaleOrdinal[D, R]) Map(value D) (R, bool) {
	for index, v := range s.domain {
		if v == value {
			if len(s.rng) == 0 {
				var zero R
				return zero, false
			}
			return s.rng[index%len(s.rng)], true
		}
	}
	return s.unknown, s.hasUnknown
}
