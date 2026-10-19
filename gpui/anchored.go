package gpui

// This file is Base ticket 08's anchored element: the pinned
// crates/gpui/src/elements/anchored.rs (GPUI-CE 254b5dbd, R: below) — an
// absolutely positioned flex box whose children are moved so they avoid
// overflowing the window: either by switching the anchor corner or by
// snapping to the window edges (with an optional margin plus the
// window's client inset).
//
// Go adaptation: the port's drawables re-resolve their layout bounds at
// paint (element.go), so the anchored element re-applies the offset it
// prepainted its children under around their paint (the pin reuses the
// prepainted bounds).

import (
	"fmt"
	"math"
)

// AnchoredFitMode selects the algorithm fitting the anchored element
// inside the window (R:anchored.rs:241-250 AnchoredFitMode).
type AnchoredFitMode uint8

const (
	// AnchoredFitSwitchAnchor switches which corner the anchored element
	// is attached to (the default).
	AnchoredFitSwitchAnchor AnchoredFitMode = iota
	// AnchoredFitSnapToWindow snaps the anchored element to the window
	// edge.
	AnchoredFitSnapToWindow
	// AnchoredFitSnapToWindowWithMargin snaps to the window edge and
	// leaves the element's margin edges.
	AnchoredFitSnapToWindowWithMargin
)

// AnchoredPositionMode selects how the anchored position is interpreted
// (R:anchored.rs:252-259 AnchoredPositionMode).
type AnchoredPositionMode uint8

const (
	// AnchoredPositionWindow interprets the position relative to the
	// window (the default).
	AnchoredPositionWindow AnchoredPositionMode = iota
	// AnchoredPositionLocal interprets the position relative to the
	// anchored element's own laid-out origin (its parent's coordinate
	// space).
	AnchoredPositionLocal
)

// positionAndBounds is R:anchored.rs:261-288 get_position_and_bounds.
func (m AnchoredPositionMode) positionAndBounds(anchorPosition *Point, anchor Anchor, size Size, bounds Bounds, offset *Point) (Point, Bounds) {
	var off Point
	if offset != nil {
		off = *offset
	}
	switch m {
	case AnchoredPositionLocal:
		var position Point
		if anchorPosition != nil {
			position = *anchorPosition
		}
		at := Point{X: bounds.Origin.X + position.X + off.X, Y: bounds.Origin.Y + position.Y + off.Y}
		return position, BoundsFromAnchorAndSize(anchor, at, size)
	default:
		position := bounds.Origin
		if anchorPosition != nil {
			position = *anchorPosition
		}
		at := Point{X: position.X + off.X, Y: position.Y + off.Y}
		return position, BoundsFromAnchorAndSize(anchor, at, size)
	}
}

// AnchoredElement is an anchored element that displays UI avoiding
// overflow of the window bounds (R:anchored.rs:14-23 Anchored). Its
// children should have no margin to avoid measurement issues. Build one
// with Anchored.
type AnchoredElement struct {
	children       []AnyElement
	anchor         Anchor
	fitMode        AnchoredFitMode
	fitMargin      Edges
	anchorPosition *Point
	positionMode   AnchoredPositionMode
	offset         *Point
}

// Anchored returns an element that avoids overflowing the window bounds
// (R:anchored.rs:27-36 anchored): top-left anchor, the switch-anchor fit,
// window position mode.
func Anchored() *AnchoredElement {
	return &AnchoredElement{anchor: AnchorTopLeft, fitMode: AnchoredFitSwitchAnchor, positionMode: AnchoredPositionWindow}
}

// Anchor sets which corner of the anchored element is anchored to the
// current position (R:anchored.rs:40).
func (a *AnchoredElement) Anchor(anchor Anchor) *AnchoredElement {
	a.anchor = anchor
	return a
}

// Position sets the position in window coordinates (otherwise the
// location the anchored element is laid out at is used;
// R:anchored.rs:47).
func (a *AnchoredElement) Position(position Point) *AnchoredElement {
	p := position
	a.anchorPosition = &p
	return a
}

// Offset offsets the final position by this amount (R:anchored.rs:54).
func (a *AnchoredElement) Offset(offset Point) *AnchoredElement {
	o := offset
	a.offset = &o
	return a
}

// PositionMode sets the position mode (R:anchored.rs:62).
func (a *AnchoredElement) PositionMode(mode AnchoredPositionMode) *AnchoredElement {
	a.positionMode = mode
	return a
}

// SnapToWindow snaps to the window edge instead of switching the anchor
// corner when an overflow would occur (R:anchored.rs:68).
func (a *AnchoredElement) SnapToWindow() *AnchoredElement {
	a.fitMode = AnchoredFitSnapToWindow
	return a
}

// SnapToWindowWithMargin snaps to the window edge leaving the margin
// edges (R:anchored.rs:74).
func (a *AnchoredElement) SnapToWindowWithMargin(edges Edges) *AnchoredElement {
	a.fitMode = AnchoredFitSnapToWindowWithMargin
	a.fitMargin = edges
	return a
}

// Child adds one child (R:anchored.rs:80-84 ParentElement::extend).
func (a *AnchoredElement) Child(value any) *AnchoredElement {
	a.children = append(a.children, intoChild("Anchored", len(a.children), value))
	return a
}

// Children adds the children in order (ParentElement::children).
func (a *AnchoredElement) Children(values ...any) *AnchoredElement {
	for _, value := range values {
		a.Child(value)
	}
	return a
}

// IntoElement implements IntoElement (R:anchored.rs:233-239).
func (a *AnchoredElement) IntoElement() AnyElement {
	return CustomElement[anchoredState, anchoredPrepaint](&anchoredDrawable{anchored: a})
}

// anchoredState is R:anchored.rs:10-12 AnchoredState.
type anchoredState struct {
	childLayoutIDs []LayoutID
}

// anchoredPrepaint records the offset the children prepainted under (the
// port's paint re-applies it; see the file comment).
type anchoredPrepaint struct {
	offset Point
}

// anchoredDrawable adapts the builder into the Element phases.
type anchoredDrawable struct {
	anchored *AnchoredElement
}

// anchoredNodeStyle is the anchored node's style (R:anchored.rs:111-115):
// absolute, flex.
func anchoredNodeStyle() Style {
	style := DefaultStyle()
	style.Position = PositionAbsolute
	style.Display = DisplayFlex
	return style
}

// ID implements Element (R:anchored.rs:90).
func (d *anchoredDrawable) ID() (ElementID, bool) { return ElementID{}, false }

// SourceLocation implements Element (R:anchored.rs:94).
func (d *anchoredDrawable) SourceLocation() *SourceLocation { return nil }

// RequestLayout implements Element (R:anchored.rs:98-120).
func (d *anchoredDrawable) RequestLayout(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App) (LayoutID, anchoredState) {
	ids := make([]LayoutID, 0, len(d.anchored.children))
	for i := range d.anchored.children {
		ids = append(ids, d.anchored.children[i].RequestLayout(w, app))
	}
	id, err := requestLayoutOf(w, anchoredNodeStyle(), ids...)
	if err != nil {
		panic(fmt.Sprintf("gpui: Anchored request_layout: %v", err))
	}
	return id, anchoredState{childLayoutIDs: ids}
}

// boundsUnion is the pinned Bounds::union (geometry.rs:1182-1186): the
// min of the origins to the max of the bottom-right corners.
func boundsUnion(a, b Bounds) Bounds {
	topLeft := Point{X: min32(a.Origin.X, b.Origin.X), Y: min32(a.Origin.Y, b.Origin.Y)}
	bottomRight := Point{X: max32(a.Right(), b.Right()), Y: max32(a.Bottom(), b.Bottom())}
	return fromCorners(topLeft, bottomRight)
}

// Prepaint implements Element (R:anchored.rs:122-215): the children's
// union size is positioned by the position mode; the switch-anchor fit
// flips the anchor along each axis whose overflow the flip fixes; every
// mode then snaps overflowing edges to the window (aligning to the
// left/top when larger than the window) with the margin edges plus the
// client inset; the children prepaint under the rounded offset.
func (d *anchoredDrawable) Prepaint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *anchoredState, w *Window, app *App) anchoredPrepaint {
	a := d.anchored
	if len(layout.childLayoutIDs) == 0 {
		return anchoredPrepaint{}
	}
	childrenBounds := w.LayoutBounds(layout.childLayoutIDs[0])
	for _, id := range layout.childLayoutIDs[1:] {
		childrenBounds = boundsUnion(childrenBounds, w.LayoutBounds(id))
	}

	origin, desired := a.positionMode.positionAndBounds(a.anchorPosition, a.anchor, childrenBounds.Size, bounds, a.offset)

	limits := Bounds{Size: w.ViewportSize()}

	if a.fitMode == AnchoredFitSwitchAnchor {
		anchor := a.anchor
		if desired.Left() < limits.Left() || desired.Right() > limits.Right() {
			switched := BoundsFromAnchorAndSize(anchor.OtherSideAlong(AxisHorizontal), origin, childrenBounds.Size)
			if !(switched.Left() < limits.Left() || switched.Right() > limits.Right()) {
				anchor = anchor.OtherSideAlong(AxisHorizontal)
				desired = switched
			}
		}
		if desired.Top() < limits.Top() || desired.Bottom() > limits.Bottom() {
			switched := BoundsFromAnchorAndSize(anchor.OtherSideAlong(AxisVertical), origin, childrenBounds.Size)
			if !(switched.Top() < limits.Top() || switched.Bottom() > limits.Bottom()) {
				desired = switched
			}
		}
	}

	clientInset, _ := w.ClientInset()
	var edges Edges
	if a.fitMode == AnchoredFitSnapToWindowWithMargin {
		edges = a.fitMargin
	}
	edges = Edges{Top: edges.Top + clientInset, Right: edges.Right + clientInset, Bottom: edges.Bottom + clientInset, Left: edges.Left + clientInset}

	// Snap the horizontal edges to the window's if they overflow,
	// aligning to the left when wider than the limits.
	if desired.Right() > limits.Right() {
		desired.Origin.X -= desired.Right() - limits.Right() + edges.Right
	}
	if desired.Left() < limits.Left() {
		desired.Origin.X = limits.Origin.X + edges.Left
	}
	// Snap the vertical edges likewise, aligning to the top when taller.
	if desired.Bottom() > limits.Bottom() {
		desired.Origin.Y -= desired.Bottom() - limits.Bottom() + edges.Bottom
	}
	if desired.Top() < limits.Top() {
		desired.Origin.Y = limits.Origin.Y + edges.Top
	}

	offset := Point{
		X: float32(math.Round(float64(desired.Origin.X - bounds.Origin.X))),
		Y: float32(math.Round(float64(desired.Origin.Y - bounds.Origin.Y))),
	}
	WithElementOffset(w, offset, func(w *Window) struct{} {
		for i := range a.children {
			a.children[i].Prepaint(w, app)
		}
		return struct{}{}
	})
	return anchoredPrepaint{offset: offset}
}

// Paint implements Element (R:anchored.rs:217-231): the children paint
// (under the prepainted offset; see the file comment).
func (d *anchoredDrawable) Paint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *anchoredState, prepaint *anchoredPrepaint, w *Window, app *App) {
	WithElementOffset(w, prepaint.offset, func(w *Window) struct{} {
		for i := range d.anchored.children {
			d.anchored.children[i].Paint(w, app)
		}
		return struct{}{}
	})
}

// LayoutNodeStyle implements the root-stretch report.
func (d *anchoredDrawable) LayoutNodeStyle() (Style, bool) { return anchoredNodeStyle(), true }
