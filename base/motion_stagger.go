package base

// Stagger delays computed per index without allocating a schedule
// (U:motion/stagger.rs).

import (
	"math"
	"time"
)

// StaggerOrigin is the index a stagger radiates from
// (U:motion/stagger.rs:3-9): First, Last, Center or Index(n).
type StaggerOrigin struct {
	kind  staggerOriginKind
	index int
}

type staggerOriginKind uint8

const (
	staggerFirst staggerOriginKind = iota
	staggerLast
	staggerCenter
	staggerIndex
)

// StaggerOriginFirst radiates from the first index.
func StaggerOriginFirst() StaggerOrigin { return StaggerOrigin{kind: staggerFirst} }

// StaggerOriginLast radiates from the last index.
func StaggerOriginLast() StaggerOrigin { return StaggerOrigin{kind: staggerLast} }

// StaggerOriginCenter radiates from the middle index (rounded down).
func StaggerOriginCenter() StaggerOrigin { return StaggerOrigin{kind: staggerCenter} }

// StaggerOriginIndex radiates from index (clamped to the last index).
func StaggerOriginIndex(index int) StaggerOrigin {
	return StaggerOrigin{kind: staggerIndex, index: index}
}

// Stagger is an interval applied per index of distance from an origin
// (U:motion/stagger.rs:11-15).
type Stagger struct {
	interval time.Duration
	origin   StaggerOrigin
}

// NewStagger is U:motion/stagger.rs:18-20.
func NewStagger(interval time.Duration, origin StaggerOrigin) Stagger {
	return Stagger{interval: interval, origin: origin}
}

// Delay is the delay of item index among count items: the interval times
// the distance from the origin, saturating (U:motion/stagger.rs:22-36).
// Indices are usize upstream; negative ones are treated as 0.
func (s Stagger) Delay(index, count int) time.Duration {
	if count <= 0 {
		return 0
	}
	index = min(max(index, 0), count-1)
	var origin int
	switch s.origin.kind {
	case staggerFirst:
		origin = 0
	case staggerLast:
		origin = count - 1
	case staggerCenter:
		origin = (count - 1) / 2
	default:
		origin = min(max(s.origin.index, 0), count-1)
	}
	distance := index - origin
	if distance < 0 {
		distance = -distance
	}
	steps := int64(min(uint64(distance), math.MaxUint32))
	if steps != 0 && s.interval > time.Duration(math.MaxInt64/steps) {
		return time.Duration(math.MaxInt64)
	}
	return s.interval * time.Duration(steps)
}
