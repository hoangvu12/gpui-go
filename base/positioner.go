package base

// Shared popup positioning (U:positioner.rs). Every anchored surface in
// Base resolves its position here so that flipping, alignment and
// viewport clamping cannot drift apart between popups, tooltips and
// menus.
//
// Go adaptations, recorded:
//
//   - The core has no Window::window_decorations (R:window.rs:2833-2835)
//     nor the Decorations/Tiling types (R:platform.rs:581-590, 758-778).
//     The pinned PlatformWindow default is Decorations::Server
//     (R:platform.rs:996-998), which is what the Windows platform window
//     reports, so the port resolves every window as server-decorated
//     (windowDecorations). The frame_insets logic and its test keep the
//     two cases with base-local unexported mirrors of the types.
//   - The port's drawables re-resolve their layout bounds at paint
//     (gpui/element.go), so the positioner re-applies the offset it
//     prepainted its children under around their paint, like the core's
//     anchored element (gpui/anchored.go); the pin reuses the prepainted
//     bounds (U:positioner.rs:427-440).

import (
	"fmt"
	"math"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Align is the alignment of a popup along the side it is placed on
// (U:positioner.rs:16-26).
type Align uint8

const (
	// AlignStart aligns with the trigger's leading edge.
	AlignStart Align = iota
	// AlignCenter centers on the trigger (the default).
	AlignCenter
	// AlignEnd aligns with the trigger's trailing edge.
	AlignEnd
)

// DefaultAlign returns the default alignment, Center
// (U:positioner.rs:21-23 #[default]).
func DefaultAlign() Align { return AlignCenter }

// String renders the alignment like the derived Debug.
func (a Align) String() string {
	switch a {
	case AlignStart:
		return "Start"
	case AlignCenter:
		return "Center"
	case AlignEnd:
		return "End"
	}
	return fmt.Sprintf("Align(%d)", uint8(a))
}

// positionerStrategy is how a popup derives its position
// (U:positioner.rs:28-46 Strategy): Corner places anchor's corner of the
// popup at position, then clamps into the viewport without flipping;
// Side places the popup on placement's side of the trigger, flipping when
// it does not fit, then clamps.
type positionerStrategy struct {
	corner bool
	// Corner.
	anchor   gpui.Anchor
	position gpui.Point
	// Side.
	triggerBounds gpui.Bounds
	placement     gpui.Optional[Placement]
	align         Align
	offset        float32
}

// ResolvedPosition is the bounds a popup resolved to, and the side it
// ended up on (U:positioner.rs:48-56).
type ResolvedPosition struct {
	// Bounds are the final popup bounds in window coordinates.
	Bounds gpui.Bounds
	// Placement is the side the popup was placed on; absent for corner
	// positioning, which has no notion of a side.
	Placement gpui.Optional[Placement]
}

// Positioner is an unstyled element that positions its children as an
// anchored popup (U:positioner.rs:58-70). It owns measurement, side
// selection, alignment and viewport clamping; it installs no
// presentation of its own and adds no wrapper element around its
// children.
type Positioner struct {
	strategy       positionerStrategy
	cornerPosition *gpui.Point
	onPosition     func(ResolvedPosition)
	margin         float32
	occlude        bool
	children       []gpui.AnyElement
}

// PositionerSide places the popup on a side of triggerBounds, flipping
// when needed (U:positioner.rs:78-93 Positioner::side).
func PositionerSide(triggerBounds gpui.Bounds) *Positioner {
	return &Positioner{
		strategy: positionerStrategy{triggerBounds: triggerBounds, align: AlignCenter},
		margin:   4,
	}
}

// PositionerCorner places anchor's corner of the popup at position
// (U:positioner.rs:95-109 Positioner::corner): the corner-anchoring path
// of triggers written against the anchored element. It clamps into the
// viewport without changing the requested anchor.
func PositionerCorner(anchor gpui.Anchor, position gpui.Point) *Positioner {
	return &Positioner{
		strategy: positionerStrategy{corner: true, anchor: anchor, position: position},
		margin:   4,
	}
}

// Position updates the requested corner position, e.g. when an animation
// moves an already-composed popup; it has no effect on a side-positioned
// popup (U:positioner.rs:111-123).
func (p *Positioner) Position(position gpui.Point) *Positioner {
	if p.strategy.corner {
		p.strategy.position = position
	}
	return p
}

// Placement sets the preferred side; only meaningful for PositionerSide
// (U:positioner.rs:125-134).
func (p *Positioner) Placement(placement Placement) *Positioner {
	if !p.strategy.corner {
		p.strategy.placement = gpui.Some(placement)
	}
	return p
}

// Align sets the alignment along the chosen side; only meaningful for
// PositionerSide (U:positioner.rs:136-143).
func (p *Positioner) Align(align Align) *Positioner {
	if !p.strategy.corner {
		p.strategy.align = align
	}
	return p
}

// Offset sets the gap between the trigger and the popup; only meaningful
// for PositionerSide (U:positioner.rs:145-152).
func (p *Positioner) Offset(offset float32) *Positioner {
	if !p.strategy.corner {
		p.strategy.offset = offset
	}
	return p
}

// trackedCornerPosition reads the corner position from cell after the
// trigger's prepaint, so an open popup follows a moving trigger
// (U:positioner.rs:154-158).
func (p *Positioner) trackedCornerPosition(cell *gpui.Point) *Positioner {
	p.cornerPosition = cell
	return p
}

// OnPosition observes the resolved geometry before the children prepaint
// (U:positioner.rs:160-164).
func (p *Positioner) OnPosition(callback func(ResolvedPosition)) *Positioner {
	p.onPosition = callback
	return p
}

// Occlude blocks the mouse over the positioned popup
// (U:positioner.rs:166-175). It is off by default, because a tooltip
// that swallowed the pointer would un-hover the very trigger keeping it
// open; an interactive surface (a popover, a menu, a dropdown) wants it
// on.
func (p *Positioner) Occlude() *Positioner {
	p.occlude = true
	return p
}

// Margin sets the minimum distance kept between the popup and the
// viewport edge (U:positioner.rs:177-181).
func (p *Positioner) Margin(margin float32) *Positioner {
	p.margin = margin
	return p
}

// Extend appends children (U:positioner.rs:327-331).
func (p *Positioner) Extend(elements []gpui.AnyElement) {
	p.children = append(p.children, elements...)
}

// Child adds one child (ParentElement::child).
func (p *Positioner) Child(value any) *Positioner {
	p.children = append(p.children, overlayIntoElement("Positioner", value))
	return p
}

// Children adds the children in order (ParentElement::children).
func (p *Positioner) Children(values ...any) *Positioner {
	for _, value := range values {
		p.Child(value)
	}
	return p
}

// IntoElement implements gpui.IntoElement (U:positioner.rs:443-449).
func (p *Positioner) IntoElement() gpui.AnyElement {
	return gpui.CustomElement[positionerState, positionerPrepaint](&positionerDrawable{p: p})
}

// tiling mirrors the pinned gpui::Tiling (R:platform.rs:758-767): which
// window edges are tiled against the screen.
type tiling struct {
	top, left, right, bottom bool
}

// tilingTiled is Tiling::tiled (R:platform.rs:769-778).
func tilingTiled() tiling { return tiling{top: true, left: true, right: true, bottom: true} }

// decorations mirrors the pinned gpui::Decorations
// (R:platform.rs:581-590): Server, or Client with its tiling.
type decorations struct {
	client bool
	tiling tiling
}

// windowDecorations is Window::window_decorations (R:window.rs:2833)
// for the port: always Server, the PlatformWindow default
// (R:platform.rs:996-998) the Windows platform window reports. See the
// file comment.
func windowDecorations(*gpui.Window) decorations { return decorations{} }

// frameInsets is the part of the window's viewport that is frame rather
// than content, per side (U:positioner.rs:184-204). A window drawn with
// client-side decorations pads its content by the client inset to make
// room for a shadow, except along an edge tiled against the screen; a
// server-decorated window has no frame of its own.
func frameInsets(d decorations, clientInset float32) gpui.Edges {
	if !d.client {
		return gpui.Edges{}
	}
	inset := func(tiled bool) float32 {
		if tiled {
			return 0
		}
		return clientInset
	}
	return gpui.Edges{
		Top:    inset(d.tiling.top),
		Right:  inset(d.tiling.right),
		Bottom: inset(d.tiling.bottom),
		Left:   inset(d.tiling.left),
	}
}

// edgesAll is Edges::all over logical pixels.
func edgesAll(value float32) gpui.Edges {
	return gpui.Edges{Top: value, Right: value, Bottom: value, Left: value}
}

// positionerResolve resolves the bounds of a popup of popupSize
// (U:positioner.rs:206-241 resolve). Side placement picks the preferred
// side when the popup fits, otherwise the opposite side, otherwise
// whichever side has more room. The result is always clamped into the
// viewport, keeping margin from each edge.
func positionerResolve(strategy positionerStrategy, popupSize, viewportSize gpui.Size, margin gpui.Edges) ResolvedPosition {
	if strategy.corner {
		return ResolvedPosition{
			Bounds: positionerClamp(gpui.BoundsFromAnchorAndSize(strategy.anchor, strategy.position, popupSize), viewportSize, margin),
		}
	}
	placement := positionerResolvePlacement(strategy.triggerBounds, popupSize, viewportSize, margin, strategy.placement)
	origin := positionerSideOrigin(strategy.triggerBounds, popupSize, placement, strategy.align, strategy.offset)
	return ResolvedPosition{
		Bounds:    positionerClamp(gpui.Bounds{Origin: origin, Size: popupSize}, viewportSize, margin),
		Placement: gpui.Some(placement),
	}
}

// positionerResolvePlacement picks the side (U:positioner.rs:243-275).
func positionerResolvePlacement(trigger gpui.Bounds, popupSize, viewportSize gpui.Size, margin gpui.Edges, preferred gpui.Optional[Placement]) Placement {
	rightLimit := max(viewportSize.Width-margin.Right, margin.Left)
	bottomLimit := max(viewportSize.Height-margin.Bottom, margin.Top)
	availableLeft := max(trigger.Left()-margin.Left, 0)
	availableRight := max(rightLimit-trigger.Right(), 0)
	availableAbove := max(trigger.Top()-margin.Top, 0)
	availableBelow := max(bottomLimit-trigger.Bottom(), 0)

	side, ok := preferred.Get()
	if !ok {
		side = PlacementTop
	}
	switch side {
	case PlacementRight:
		switch {
		case popupSize.Width <= availableRight:
			return PlacementRight
		case popupSize.Width <= availableLeft:
			return PlacementLeft
		case availableRight >= availableLeft:
			return PlacementRight
		}
		return PlacementLeft
	case PlacementLeft:
		switch {
		case popupSize.Width <= availableLeft:
			return PlacementLeft
		case popupSize.Width <= availableRight:
			return PlacementRight
		case availableLeft >= availableRight:
			return PlacementLeft
		}
		return PlacementRight
	case PlacementBottom:
		switch {
		case popupSize.Height <= availableBelow:
			return PlacementBottom
		case popupSize.Height <= availableAbove:
			return PlacementTop
		case availableBelow >= availableAbove:
			return PlacementBottom
		}
		return PlacementTop
	}
	// Some(Top) | None.
	switch {
	case popupSize.Height <= availableAbove:
		return PlacementTop
	case popupSize.Height <= availableBelow:
		return PlacementBottom
	case availableBelow >= availableAbove:
		return PlacementBottom
	}
	return PlacementTop
}

// positionerSideOrigin is the popup origin on placement's side
// (U:positioner.rs:277-301).
func positionerSideOrigin(trigger gpui.Bounds, popupSize gpui.Size, placement Placement, align Align, offset float32) gpui.Point {
	var alignedX, alignedY float32
	switch align {
	case AlignStart:
		alignedX, alignedY = trigger.Left(), trigger.Top()
	case AlignEnd:
		alignedX, alignedY = trigger.Right()-popupSize.Width, trigger.Bottom()-popupSize.Height
	default:
		center := trigger.Center()
		alignedX, alignedY = center.X-popupSize.Width/2, center.Y-popupSize.Height/2
	}
	switch placement {
	case PlacementTop:
		return gpui.Point{X: alignedX, Y: trigger.Top() - popupSize.Height - offset}
	case PlacementBottom:
		return gpui.Point{X: alignedX, Y: trigger.Bottom() + offset}
	case PlacementLeft:
		return gpui.Point{X: trigger.Left() - popupSize.Width - offset, Y: alignedY}
	}
	return gpui.Point{X: trigger.Right() + offset, Y: alignedY}
}

// positionerClamp clamps bounds into the viewport keeping margin from
// each edge, aligning to the leading edge when larger
// (U:positioner.rs:303-325).
func positionerClamp(bounds gpui.Bounds, viewportSize gpui.Size, margin gpui.Edges) gpui.Bounds {
	rightLimit := max(viewportSize.Width-margin.Right, margin.Left)
	bottomLimit := max(viewportSize.Height-margin.Bottom, margin.Top)
	if bounds.Right() > rightLimit {
		bounds.Origin.X -= bounds.Right() - rightLimit
	}
	if bounds.Left() < margin.Left {
		bounds.Origin.X = margin.Left
	}
	if bounds.Bottom() > bottomLimit {
		bounds.Origin.Y -= bounds.Bottom() - bottomLimit
	}
	if bounds.Top() < margin.Top {
		bounds.Origin.Y = margin.Top
	}
	return bounds
}

// positionerState is U:positioner.rs:72-75 PositionerState.
type positionerState struct {
	childLayoutIDs []gpui.LayoutID
}

// positionerPrepaint records the offset the children prepainted under
// (re-applied at paint; see the file comment).
type positionerPrepaint struct {
	offset gpui.Point
}

// positionerDrawable adapts the builder into the Element phases.
type positionerDrawable struct {
	p *Positioner
}

// positionerNodeStyle is the positioner node's style
// (U:positioner.rs:357-362): absolute, flex.
func positionerNodeStyle() gpui.Style {
	style := gpui.DefaultStyle()
	style.Position = gpui.PositionAbsolute
	style.Display = gpui.DisplayFlex
	return style
}

// ID implements gpui.Element (U:positioner.rs:337-339).
func (d *positionerDrawable) ID() (gpui.ElementID, bool) { return gpui.ElementID{}, false }

// SourceLocation implements gpui.Element (U:positioner.rs:341-343).
func (d *positionerDrawable) SourceLocation() *gpui.SourceLocation { return nil }

// RequestLayout lays out the children under an absolute flex node
// (U:positioner.rs:345-368).
func (d *positionerDrawable) RequestLayout(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, w *gpui.Window, app *gpui.App) (gpui.LayoutID, positionerState) {
	ids := make([]gpui.LayoutID, 0, len(d.p.children))
	for i := range d.p.children {
		ids = append(ids, d.p.children[i].RequestLayout(w, app))
	}
	id, err := gpui.RequestElementLayout(w, positionerNodeStyle(), ids...)
	if err != nil {
		panic(fmt.Sprintf("base: Positioner request_layout: %v", err))
	}
	return id, positionerState{childLayoutIDs: ids}
}

// Prepaint measures the children, resolves the position, reports it,
// blocks the mouse behind the popup when occluding, and prepaints the
// children at the resolved origin (U:positioner.rs:370-425).
func (d *positionerDrawable) Prepaint(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, bounds gpui.Bounds, layout *positionerState, w *gpui.Window, app *gpui.App) positionerPrepaint {
	p := d.p
	if len(layout.childLayoutIDs) == 0 {
		return positionerPrepaint{}
	}
	childMin := gpui.Point{X: math.MaxFloat32, Y: math.MaxFloat32}
	var childMax gpui.Point
	for _, id := range layout.childLayoutIDs {
		child := w.LayoutBounds(id)
		childMin = gpui.Point{X: min(childMin.X, child.Origin.X), Y: min(childMin.Y, child.Origin.Y)}
		bottomRight := child.BottomRight()
		childMax = gpui.Point{X: max(childMax.X, bottomRight.X), Y: max(childMax.Y, bottomRight.Y)}
	}
	popupSize := gpui.Size{Width: childMax.X - childMin.X, Height: childMax.Y - childMin.Y}
	clientInset, _ := w.ClientInset()
	frame := frameInsets(windowDecorations(w), clientInset)
	strategy := p.strategy
	if strategy.corner && p.cornerPosition != nil {
		strategy.position = *p.cornerPosition
	}
	margin := gpui.Edges{
		Top:    frame.Top + p.margin,
		Right:  frame.Right + p.margin,
		Bottom: frame.Bottom + p.margin,
		Left:   frame.Left + p.margin,
	}
	position := positionerResolve(strategy, popupSize, w.ViewportSize(), margin)
	if p.onPosition != nil {
		p.onPosition(position)
	}
	// Ahead of the children so it blocks what is behind the popup
	// without blocking the popup's own content.
	if p.occlude {
		w.InsertHitbox(position.Bounds, gpui.HitboxBlockMouse)
	}

	offset := gpui.Point{
		X: float32(math.Round(float64(position.Bounds.Origin.X - bounds.Origin.X))),
		Y: float32(math.Round(float64(position.Bounds.Origin.Y - bounds.Origin.Y))),
	}
	gpui.WithElementOffset(w, offset, func(w *gpui.Window) struct{} {
		for i := range p.children {
			p.children[i].Prepaint(w, app)
		}
		return struct{}{}
	})
	return positionerPrepaint{offset: offset}
}

// Paint paints the children (U:positioner.rs:427-440) under the
// prepainted offset (see the file comment).
func (d *positionerDrawable) Paint(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, _ gpui.Bounds, _ *positionerState, prepaint *positionerPrepaint, w *gpui.Window, app *gpui.App) {
	gpui.WithElementOffset(w, prepaint.offset, func(w *gpui.Window) struct{} {
		for i := range d.p.children {
			d.p.children[i].Paint(w, app)
		}
		return struct{}{}
	})
}

// LayoutNodeStyle implements the core's root-stretch report.
func (d *positionerDrawable) LayoutNodeStyle() (gpui.Style, bool) { return positionerNodeStyle(), true }
