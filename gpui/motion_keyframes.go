package gpui

// Keyframe tracks and discrete values (U:motion/keyframes.rs; moved from
// base/motion_keyframes.go by browser-core ticket 44).
//
// Go adaptation: Keyframes interpolates with InterpolateValue, the Go
// form of the `T: Interpolate` bound (see motion.go).

import (
	"fmt"
	"sort"
)

// Keyframe is one value at an offset of a keyframe track, eased toward
// the next frame (U:motion/keyframes.rs:5-25).
type Keyframe[T any] struct {
	Offset float32
	Value  T
	Easing Easing
}

// NewKeyframe is a linearly eased frame (U:motion/keyframes.rs:13-19).
func NewKeyframe[T any](offset float32, value T) Keyframe[T] {
	return Keyframe[T]{Offset: offset, Value: value, Easing: EasingLinear()}
}

// Ease sets the easing toward the next frame (U:motion/keyframes.rs:21-24).
func (k Keyframe[T]) Ease(easing Easing) Keyframe[T] {
	k.Easing = easing
	return k
}

// KeyframeError is an invalid keyframe track (U:motion/keyframes.rs:27-34).
type KeyframeError uint8

const (
	KeyframeErrorTooFewFrames KeyframeError = iota
	KeyframeErrorOffsetNotFinite
	KeyframeErrorOffsetOutOfRange
	KeyframeErrorOffsetsNotMonotonic
	KeyframeErrorMissingEndpoint
)

// Error renders the variant name (the Rust Debug derive).
func (e KeyframeError) Error() string {
	switch e {
	case KeyframeErrorTooFewFrames:
		return "TooFewFrames"
	case KeyframeErrorOffsetNotFinite:
		return "OffsetNotFinite"
	case KeyframeErrorOffsetOutOfRange:
		return "OffsetOutOfRange"
	case KeyframeErrorOffsetsNotMonotonic:
		return "OffsetsNotMonotonic"
	case KeyframeErrorMissingEndpoint:
		return "MissingEndpoint"
	}
	return fmt.Sprintf("KeyframeError(%d)", e)
}

// Keyframes is a validated keyframe track (U:motion/keyframes.rs:36-39).
type Keyframes[T any] struct {
	frames []Keyframe[T]
}

// KeyframesTryNew validates a track (U:motion/keyframes.rs:42-68): at
// least two frames, finite offsets in 0..=1 that never decrease, starting
// at 0 and ending at 1.
func KeyframesTryNew[T any](frames ...Keyframe[T]) (Keyframes[T], error) {
	if len(frames) < 2 {
		return Keyframes[T]{}, KeyframeErrorTooFewFrames
	}
	for _, frame := range frames {
		if !finite32(frame.Offset) {
			return Keyframes[T]{}, KeyframeErrorOffsetNotFinite
		}
	}
	for _, frame := range frames {
		if !in01(frame.Offset) {
			return Keyframes[T]{}, KeyframeErrorOffsetOutOfRange
		}
	}
	for i := 1; i < len(frames); i++ {
		if frames[i-1].Offset > frames[i].Offset {
			return Keyframes[T]{}, KeyframeErrorOffsetsNotMonotonic
		}
	}
	if frames[0].Offset != 0 || frames[len(frames)-1].Offset != 1 {
		return Keyframes[T]{}, KeyframeErrorMissingEndpoint
	}
	return Keyframes[T]{frames: append([]Keyframe[T](nil), frames...)}, nil
}

// Sample samples the track at progress, clamped to [0, 1]; the segment's
// own easing shapes the interpolation (U:motion/keyframes.rs:70-90).
func (k Keyframes[T]) Sample(progress float32) T {
	progress = clampMotion32(progress, 0, 1)
	frames := k.frames
	upper := sort.Search(len(frames), func(i int) bool { return frames[i].Offset > progress })
	if upper == 0 {
		return frames[0].Value
	}
	if upper == len(frames) {
		return frames[len(frames)-1].Value
	}
	from, to := frames[upper-1], frames[upper]
	if from.Offset == to.Offset {
		return to.Value
	}
	segment := (progress - from.Offset) / (to.Offset - from.Offset)
	return InterpolateValue(from.Value, to.Value, from.Easing.Sample(segment))
}

// Len reports the number of frames (U:motion/keyframes.rs:92-94).
func (k Keyframes[T]) Len() int { return len(k.frames) }

// IsEmpty reports an empty track (U:motion/keyframes.rs:96-98).
func (k Keyframes[T]) IsEmpty() bool { return len(k.frames) == 0 }

// DiscreteError is an invalid discrete switch point
// (U:motion/keyframes.rs:101-104).
type DiscreteError uint8

const (
	DiscreteErrorInvalidSwitchPoint DiscreteError = iota
)

// Error renders the variant name (the Rust Debug derive).
func (e DiscreteError) Error() string {
	switch e {
	case DiscreteErrorInvalidSwitchPoint:
		return "InvalidSwitchPoint"
	}
	return fmt.Sprintf("DiscreteError(%d)", e)
}

// Discrete is a value that switches from one value to another at a
// progress (U:motion/keyframes.rs:106-111).
type Discrete[T any] struct {
	from, to T
	switchAt float32
}

// NewDiscrete switches halfway (U:motion/keyframes.rs:114-120).
func NewDiscrete[T any](from, to T) Discrete[T] {
	return Discrete[T]{from: from, to: to, switchAt: 0.5}
}

// SwitchAt sets the switch point, finite and within 0..=1
// (U:motion/keyframes.rs:122-128).
func (d Discrete[T]) SwitchAt(progress float32) (Discrete[T], error) {
	if !finite32(progress) || !in01(progress) {
		return d, DiscreteErrorInvalidSwitchPoint
	}
	d.switchAt = progress
	return d, nil
}

// Sample is from before the switch point and to from it on
// (U:motion/keyframes.rs:131-140).
func (d Discrete[T]) Sample(progress float32) T {
	if progress < d.switchAt {
		return d.from
	}
	return d.to
}
