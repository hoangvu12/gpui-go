package gpui

// This file ports the geometry helpers Base uses from the pinned CE
// reference 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a
// (reference/ce-source/crates/gpui/src/geometry.rs) onto the port's
// logical-pixel Point/Size/Bounds (window.go):
//
//   - 26-41 Axis + invert; 44-53 Along (along/apply_along) with the Point
//     (163-185) and Size (535-561) impls.
//   - 2207-2289 Anchor + opposite / other_side_along / is_center.
//   - 494-505 Size::at_anchor; 866-871 Bounds::from_anchor_and_size;
//     893-922 top_center / bottom_center / left_center / right_center;
//     998-1003 Bounds::center; 1339-1395 top_right / bottom_right /
//     bottom_left; 1418-1430 Bounds::corner; 1465-1470 Bounds::contains.
//
// Half for f32 Pixels is division by two (geometry.rs Half for Pixels).

// Axis is an axis in a 2D cartesian space (geometry.rs:26-31).
type Axis uint8

const (
	// AxisVertical is the y axis, up and down.
	AxisVertical Axis = iota
	// AxisHorizontal is the x axis, left and right.
	AxisHorizontal
)

// Invert swaps this axis to the opposite axis (geometry.rs:33-41).
func (a Axis) Invert() Axis {
	if a == AxisVertical {
		return AxisHorizontal
	}
	return AxisVertical
}

// String renders the axis like the reference Debug derive.
func (a Axis) String() string {
	if a == AxisVertical {
		return "Vertical"
	}
	return "Horizontal"
}

// Along returns the coordinate along the axis (geometry.rs:166-171).
func (p Point) Along(axis Axis) float32 {
	if axis == AxisHorizontal {
		return p.X
	}
	return p.Y
}

// ApplyAlong applies f to the coordinate along the axis
// (geometry.rs:173-184).
func (p Point) ApplyAlong(axis Axis, f func(float32) float32) Point {
	if axis == AxisHorizontal {
		return Point{X: f(p.X), Y: p.Y}
	}
	return Point{X: p.X, Y: f(p.Y)}
}

// Along returns the extent along the axis (geometry.rs:541-546).
func (s Size) Along(axis Axis) float32 {
	if axis == AxisHorizontal {
		return s.Width
	}
	return s.Height
}

// ApplyAlong applies f to the extent along the axis
// (geometry.rs:549-560).
func (s Size) ApplyAlong(axis Axis, f func(float32) float32) Size {
	if axis == AxisHorizontal {
		return Size{Width: f(s.Width), Height: s.Height}
	}
	return Size{Width: s.Width, Height: f(s.Height)}
}

// Anchor identifies a reference point on a 2D box, used to anchor
// positioned elements (geometry.rs:2207-2226).
type Anchor uint8

const (
	// AnchorTopLeft is the top left corner.
	AnchorTopLeft Anchor = iota
	// AnchorTopRight is the top right corner.
	AnchorTopRight
	// AnchorBottomLeft is the bottom left corner.
	AnchorBottomLeft
	// AnchorBottomRight is the bottom right corner.
	AnchorBottomRight
	// AnchorTopCenter is the top center position.
	AnchorTopCenter
	// AnchorBottomCenter is the bottom center position.
	AnchorBottomCenter
	// AnchorLeftCenter is the left center position.
	AnchorLeftCenter
	// AnchorRightCenter is the right center position.
	AnchorRightCenter
)

// String renders the anchor like the reference Debug derive.
func (a Anchor) String() string {
	switch a {
	case AnchorTopLeft:
		return "TopLeft"
	case AnchorTopRight:
		return "TopRight"
	case AnchorBottomLeft:
		return "BottomLeft"
	case AnchorBottomRight:
		return "BottomRight"
	case AnchorTopCenter:
		return "TopCenter"
	case AnchorBottomCenter:
		return "BottomCenter"
	case AnchorLeftCenter:
		return "LeftCenter"
	case AnchorRightCenter:
		return "RightCenter"
	}
	return "Anchor(?)"
}

// Opposite returns the directly opposite anchor (geometry.rs:2238-2250).
func (a Anchor) Opposite() Anchor {
	switch a {
	case AnchorTopLeft:
		return AnchorBottomRight
	case AnchorTopRight:
		return AnchorBottomLeft
	case AnchorBottomLeft:
		return AnchorTopRight
	case AnchorBottomRight:
		return AnchorTopLeft
	case AnchorTopCenter:
		return AnchorBottomCenter
	case AnchorBottomCenter:
		return AnchorTopCenter
	case AnchorLeftCenter:
		return AnchorRightCenter
	case AnchorRightCenter:
		return AnchorLeftCenter
	}
	return a
}

// OtherSideAlong returns the anchor across from this one, moving along
// the axis (geometry.rs:2261-2285).
func (a Anchor) OtherSideAlong(axis Axis) Anchor {
	if axis == AxisVertical {
		switch a {
		case AnchorTopLeft:
			return AnchorBottomLeft
		case AnchorTopRight:
			return AnchorBottomRight
		case AnchorBottomLeft:
			return AnchorTopLeft
		case AnchorBottomRight:
			return AnchorTopRight
		case AnchorTopCenter:
			return AnchorBottomCenter
		case AnchorBottomCenter:
			return AnchorTopCenter
		}
		return a // LeftCenter / RightCenter stay
	}
	switch a {
	case AnchorTopLeft:
		return AnchorTopRight
	case AnchorTopRight:
		return AnchorTopLeft
	case AnchorBottomLeft:
		return AnchorBottomRight
	case AnchorBottomRight:
		return AnchorBottomLeft
	case AnchorLeftCenter:
		return AnchorRightCenter
	case AnchorRightCenter:
		return AnchorLeftCenter
	}
	return a // TopCenter / BottomCenter stay
}

// IsCenter reports whether the anchor is an edge center
// (geometry.rs:2287-2293).
func (a Anchor) IsCenter() bool {
	return a == AnchorTopCenter || a == AnchorBottomCenter || a == AnchorLeftCenter || a == AnchorRightCenter
}

// AtAnchor returns the point at the anchor along the edges of a box of
// this size (geometry.rs:494-505).
func (s Size) AtAnchor(anchor Anchor) Point {
	switch anchor {
	case AnchorTopRight:
		return Point{X: s.Width}
	case AnchorBottomLeft:
		return Point{Y: s.Height}
	case AnchorBottomRight:
		return Point{X: s.Width, Y: s.Height}
	case AnchorTopCenter:
		return Point{X: s.Width / 2}
	case AnchorBottomCenter:
		return Point{X: s.Width / 2, Y: s.Height}
	case AnchorLeftCenter:
		return Point{Y: s.Height / 2}
	case AnchorRightCenter:
		return Point{X: s.Width, Y: s.Height / 2}
	}
	return Point{} // TopLeft
}

// BoundsFromAnchorAndSize builds bounds whose anchor point lands on
// origin: the new origin is origin minus the size's anchor point
// (geometry.rs:866-871 Bounds::from_anchor_and_size).
func BoundsFromAnchorAndSize(anchor Anchor, origin Point, size Size) Bounds {
	at := size.AtAnchor(anchor)
	return Bounds{Origin: Point{X: origin.X - at.X, Y: origin.Y - at.Y}, Size: size}
}

// Contains reports whether point lies in the bounds: origin-inclusive,
// far-edge-exclusive (geometry.rs:1465-1470 Bounds::contains).
func (b Bounds) Contains(point Point) bool {
	return boundsContain(b, point)
}

// Center returns the center point (geometry.rs:998-1003).
func (b Bounds) Center() Point {
	return Point{X: b.Origin.X + b.Size.Width/2, Y: b.Origin.Y + b.Size.Height/2}
}

// TopRight returns the top right corner (geometry.rs:1341-1346).
func (b Bounds) TopRight() Point {
	return Point{X: b.Origin.X + b.Size.Width, Y: b.Origin.Y}
}

// BottomRight returns the bottom right corner (geometry.rs:1365-1370).
func (b Bounds) BottomRight() Point {
	return Point{X: b.Origin.X + b.Size.Width, Y: b.Origin.Y + b.Size.Height}
}

// BottomLeft returns the bottom left corner (geometry.rs:1389-1394).
func (b Bounds) BottomLeft() Point {
	return Point{X: b.Origin.X, Y: b.Origin.Y + b.Size.Height}
}

// TopCenter returns the top center point (geometry.rs:893-898).
func (b Bounds) TopCenter() Point {
	return Point{X: b.Origin.X + b.Size.Width/2, Y: b.Origin.Y}
}

// BottomCenter returns the bottom center point (geometry.rs:901-906).
func (b Bounds) BottomCenter() Point {
	return Point{X: b.Origin.X + b.Size.Width/2, Y: b.Origin.Y + b.Size.Height}
}

// LeftCenter returns the left center point (geometry.rs:909-914).
func (b Bounds) LeftCenter() Point {
	return Point{X: b.Origin.X, Y: b.Origin.Y + b.Size.Height/2}
}

// RightCenter returns the right center point (geometry.rs:917-922).
func (b Bounds) RightCenter() Point {
	return Point{X: b.Origin.X + b.Size.Width, Y: b.Origin.Y + b.Size.Height/2}
}

// Corner returns the point at the anchor (geometry.rs:1418-1429
// Bounds::corner).
func (b Bounds) Corner(anchor Anchor) Point {
	switch anchor {
	case AnchorTopRight:
		return b.TopRight()
	case AnchorBottomLeft:
		return b.BottomLeft()
	case AnchorBottomRight:
		return b.BottomRight()
	case AnchorTopCenter:
		return b.TopCenter()
	case AnchorBottomCenter:
		return b.BottomCenter()
	case AnchorLeftCenter:
		return b.LeftCenter()
	case AnchorRightCenter:
		return b.RightCenter()
	}
	return b.Origin
}
