package gpui

// This file ports the scroll-gesture axis lock of R:gestures.rs:26-108
// (OngoingScroll: dominant_axis, lock_delta_to_axis, filter / filter_at,
// SCROLL_EVENT_SEPARATION). The scrollable div applies it under
// restrict_scroll_to_axis to precise (pixel) deltas (R:elements/
// div.rs:3837-3844). The pinned filter reads Instant::now(); the port
// reads the application clock (App.Now), so TestApp.AdvanceClock drives
// the gesture timeout deterministically.
//
// R: = reference/ce-source/crates/gpui/src (pin 254b5dbd).

import "time"

// scrollEventSeparation is the gap after which a Moved-only scroll
// starts a new gesture (R:gestures.rs:26 SCROLL_EVENT_SEPARATION).
const scrollEventSeparation = 28 * time.Millisecond

// dominantAxis is R:gestures.rs:28-34: vertical unless |x| > |y|.
func dominantAxis(delta Point) Axis {
	if abs32(delta.X) <= abs32(delta.Y) {
		return AxisVertical
	}
	return AxisHorizontal
}

// lockDeltaToAxis zeroes the off-axis component (R:gestures.rs:36-41).
func lockDeltaToAxis(delta *Point, axis Axis) {
	if axis == AxisVertical {
		delta.X = 0
	} else {
		delta.Y = 0
	}
}

// OngoingScroll tracks the dominant axis across the events of one scroll
// gesture (R:gestures.rs:47-52). The zero value is the pinned Default.
type OngoingScroll struct {
	// lastEvent is the clock reading of the last filtered event; valid
	// when hasLastEvent (the pinned Option<Instant>).
	lastEvent    time.Duration
	hasLastEvent bool
	// axis is the locked axis; valid when hasAxis (Option<Axis>).
	axis    Axis
	hasAxis bool
}

// Filter filters delta to the dominant axis of the current scroll
// gesture (R:gestures.rs:55-61 filter). Gestures are delimited by their
// touch phase when available, with the 28ms timeout fallback for
// platforms that only emit Moved. The clock is the application's
// (App.Now); a nil app reads zero.
func (s *OngoingScroll) Filter(delta *Point, touchPhase TouchPhase, app *App) {
	var now time.Duration
	if app != nil {
		now = app.Now()
	}
	s.filterAt(delta, touchPhase, now)
}

// filterAt is R:gestures.rs:63-107 filter_at.
func (s *OngoingScroll) filterAt(delta *Point, touchPhase TouchPhase, now time.Duration) {
	const unlockPercent = 1.9
	const unlockLowerBound = 6

	if touchPhase == TouchPhaseEnded || touchPhase == TouchPhaseCancelled {
		s.hasLastEvent = false
		s.hasAxis = false
		return
	}

	x := abs32(delta.X)
	y := abs32(delta.Y)
	if x == 0 && y == 0 {
		if touchPhase == TouchPhaseStarted {
			s.hasLastEvent = false
			s.hasAxis = false
		}
		return
	}

	startsNewGesture := touchPhase == TouchPhaseStarted ||
		!s.hasLastEvent || now-s.lastEvent >= scrollEventSeparation
	axis, hasAxis := s.axis, s.hasAxis
	if startsNewGesture {
		axis, hasAxis = dominantAxis(*delta), true
	} else if max32(x, y) >= unlockLowerBound && hasAxis {
		switch {
		case axis == AxisVertical && x > y && x >= y*unlockPercent:
			hasAxis = false
		case axis == AxisHorizontal && y > x && y >= x*unlockPercent:
			hasAxis = false
		}
	}

	s.lastEvent, s.hasLastEvent = now, true
	s.axis, s.hasAxis = axis, hasAxis
	if hasAxis {
		lockDeltaToAxis(delta, axis)
	}
}
