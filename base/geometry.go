package base

// Geometry helpers (U:geometry.rs): Placement, Side, the Axis and Length
// extensions, and the serializable Edges. The local Anchor enum was
// removed upstream in favor of gpui::Anchor (U:geometry.rs:60), which the
// core ports as gpui.Anchor.

import (
	"fmt"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Placement is the placement of an element on one of four edges
// (U:geometry.rs:8-21). See also Side for left/right only.
type Placement uint8

const (
	// PlacementTop places on the top edge.
	PlacementTop Placement = iota
	// PlacementBottom places on the bottom edge.
	PlacementBottom
	// PlacementLeft places on the left edge.
	PlacementLeft
	// PlacementRight places on the right edge.
	PlacementRight
)

// String renders the placement (U:geometry.rs:23-32 Display).
func (p Placement) String() string {
	switch p {
	case PlacementTop:
		return "Top"
	case PlacementBottom:
		return "Bottom"
	case PlacementLeft:
		return "Left"
	case PlacementRight:
		return "Right"
	}
	return fmt.Sprintf("Placement(%d)", uint8(p))
}

// IsHorizontal reports Left or Right (U:geometry.rs:35-41).
func (p Placement) IsHorizontal() bool { return p == PlacementLeft || p == PlacementRight }

// IsVertical reports Top or Bottom (U:geometry.rs:43-49).
func (p Placement) IsVertical() bool { return p == PlacementTop || p == PlacementBottom }

// Axis returns the axis the placement runs along (U:geometry.rs:51-57).
func (p Placement) Axis() gpui.Axis {
	if p.IsVertical() {
		return gpui.AxisVertical
	}
	return gpui.AxisHorizontal
}

// MarshalText encodes the serde name (U:geometry.rs:13-20 rename).
func (p Placement) MarshalText() ([]byte, error) {
	switch p {
	case PlacementTop:
		return []byte("top"), nil
	case PlacementBottom:
		return []byte("bottom"), nil
	case PlacementLeft:
		return []byte("left"), nil
	case PlacementRight:
		return []byte("right"), nil
	}
	return nil, fmt.Errorf("base: invalid Placement %d", uint8(p))
}

// UnmarshalText decodes the serde name.
func (p *Placement) UnmarshalText(text []byte) error {
	switch string(text) {
	case "top":
		*p = PlacementTop
	case "bottom":
		*p = PlacementBottom
	case "left":
		*p = PlacementLeft
	case "right":
		*p = PlacementRight
	default:
		return fmt.Errorf("base: unknown variant %q, expected one of `top`, `bottom`, `left`, `right`", string(text))
	}
	return nil
}

// Side is the left or right side of an element (U:geometry.rs:62-71). See
// also Placement for the four edges.
type Side uint8

const (
	// SideLeft is the left side.
	SideLeft Side = iota
	// SideRight is the right side.
	SideRight
)

// IsLeft reports the left side (U:geometry.rs:74-78).
func (s Side) IsLeft() bool { return s == SideLeft }

// IsRight reports the right side (U:geometry.rs:80-84).
func (s Side) IsRight() bool { return s == SideRight }

// String renders the side like the derived Debug.
func (s Side) String() string {
	if s == SideRight {
		return "Right"
	}
	return "Left"
}

// MarshalText encodes the serde name (U:geometry.rs:67-70 rename).
func (s Side) MarshalText() ([]byte, error) {
	switch s {
	case SideLeft:
		return []byte("left"), nil
	case SideRight:
		return []byte("right"), nil
	}
	return nil, fmt.Errorf("base: invalid Side %d", uint8(s))
}

// UnmarshalText decodes the serde name.
func (s *Side) UnmarshalText(text []byte) error {
	switch string(text) {
	case "left":
		*s = SideLeft
	case "right":
		*s = SideRight
	default:
		return fmt.Errorf("base: unknown variant %q, expected `left` or `right`", string(text))
	}
	return nil
}

// AxisIsHorizontal is AxisExt::is_horizontal (U:geometry.rs:93-97).
func AxisIsHorizontal(axis gpui.Axis) bool { return axis == gpui.AxisHorizontal }

// AxisIsVertical is AxisExt::is_vertical (U:geometry.rs:99-102).
func AxisIsVertical(axis gpui.Axis) bool { return axis == gpui.AxisVertical }

// LengthToPixels is LengthExt::to_pixels (U:geometry.rs:105-120): an auto
// length has no pixels; a definite length resolves against baseSize and
// remSize like DefiniteLength::to_pixels (R:geometry.rs:3616-3621,
// Relative::to_pixels 3367-3372, AbsoluteLength::to_pixels 3469-3474).
func LengthToPixels(length gpui.Length, baseSize gpui.AbsoluteLength, remSize float32) (float32, bool) {
	if length.Auto {
		return 0, false
	}
	d := length.Definite
	if d.Kind != gpui.DefiniteLengthFraction {
		return d.Absolute().ToPixels(remSize), true
	}
	if baseSize.Kind == gpui.AbsoluteLengthRem {
		return baseSize.Value * remSize * d.Value, true
	}
	return baseSize.Value * d.Value, true
}

// Edges are the four edges of an element, the serializable extension of
// gpui::Edges (U:geometry.rs:122-136).
type Edges[T any] struct {
	// Top is the size of the top edge.
	Top T `json:"top"`
	// Right is the size of the right edge.
	Right T `json:"right"`
	// Bottom is the size of the bottom edge.
	Bottom T `json:"bottom"`
	// Left is the size of the left edge.
	Left T `json:"left"`
}

// EdgesAll returns edges with every side set to value
// (U:geometry.rs:142-150 Edges::all).
func EdgesAll[T any](value T) Edges[T] {
	return Edges[T]{Top: value, Right: value, Bottom: value, Left: value}
}
