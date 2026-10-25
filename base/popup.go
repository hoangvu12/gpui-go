package base

// An unstyled trigger and anchored popup host (U:popup.rs).
//
// Go adaptations, recorded:
//
//   - Popup is a gpui.RenderOnce recipe (U:popup.rs:130): render needs
//     the window for its keyed anchor state. Convert it as a child, or
//     with gpui.OwnRecipe where an AnyElement is required.
//   - The pinned Rc<Cell<_>> cells shared between the render and the
//     trigger canvas are plain pointers.
//   - Display (map.md gap 2b): the root `div().id(id)` holding the
//     trigger (U:popup.rs:44) is a plain div upstream, display Block
//     under crates.io gpui 0.2.2 (the pin defaults to Flex,
//     R:style.rs:1024-1027), so it is built with Block() before the
//     caller's style refines over it; a caller's Flex() still wins.
//   - Positioning (browser-core ticket 72): the placement is the core's
//     element-anchored Anchored (gpui.Anchored().AnchorBounds over the
//     measured trigger cell, the corner anchors mapped onto side and
//     alignment, the offset onto the gap, AnchorShift as the only
//     fallback — the corner rule clamps without flipping — and the
//     window margin), replacing Base's own positioner element
//     (positioner.go, deleted): every anchored surface in Base now
//     resolves through the same core rule as CSS anchor positioning.

import (
	"fmt"

	"github.com/hoangvu12/gpui-go/gpui"
)

// popupWindowMargin is the distance kept between a popup and the window
// edge (U:popup.rs:11-12).
const popupWindowMargin float32 = 8

// PopupPriority is the deferred paint priority of interactive surfaces
// that must appear above dialogs (U:popup.rs:14-15 POPUP_PRIORITY).
const PopupPriority = 100

// ResolvedPosition is the bounds a popup resolved to, and the side it
// ended up on (U:positioner.rs:48-56; the positioner element is gone,
// the type is the popup host's placement report).
type ResolvedPosition struct {
	// Bounds are the final popup bounds in window coordinates.
	Bounds gpui.Bounds
	// Placement is the side the popup was placed on (the side the
	// corner anchor maps onto).
	Placement gpui.Optional[Placement]
}

// edgesAll is Edges::all over logical pixels.
func edgesAll(value float32) gpui.Edges {
	return gpui.Edges{Top: value, Right: value, Bottom: value, Left: value}
}

// popupAnchorState is the keyed trigger measurement (U:popup.rs:17-21).
type popupAnchorState struct {
	bounds   gpui.Bounds
	captured bool
}

// Popup is an unstyled trigger and anchored popup host
// (U:popup.rs:23-38). It owns trigger measurement, anchor-point
// calculation, first-frame synchronization, deferred rendering and
// window-edge snapping. Callers own open state, interaction, popup
// content, appearance and motion. Its Styled methods style the root (the
// trigger container); its Interactive methods reach the root div.
type Popup struct {
	gpui.Styled[*Popup]
	gpui.Interactive[*Popup]

	id         gpui.ElementID
	base       *gpui.DivElement
	anchor     gpui.Anchor
	offset     float32
	onPosition func(ResolvedPosition, gpui.Bounds)
	trigger    gpui.AnyElement
	content    *gpui.AnyElement
}

// NewPopup creates a popup host around trigger (U:popup.rs:41-53
// Popup::new): any child value (an element, IntoElement, string, view or
// recipe).
func NewPopup[I gpui.IntoElementID](id I, trigger any) *Popup {
	eid := gpui.ElementIDOf(id)
	base := gpui.Div().Block().ID(eid)
	p := &Popup{
		id:      eid,
		base:    base,
		anchor:  gpui.AnchorTopLeft,
		trigger: overlayIntoElement("Popup trigger", trigger),
	}
	gpui.BindStyled(p)
	gpui.BindInteractive(p, base)
	return p
}

// Anchor sets the popup corner attached to the trigger
// (U:popup.rs:55-58).
func (p *Popup) Anchor(anchor gpui.Anchor) *Popup {
	p.anchor = anchor
	return p
}

// Offset sets the gap from the trigger along the anchor's outward
// direction, zero by default (U:popup.rs:60-64).
func (p *Popup) Offset(offset float32) *Popup {
	p.offset = offset
	return p
}

// OnPosition observes the resolved popup and trigger bounds before the
// content prepaints (U:popup.rs:66-73).
func (p *Popup) OnPosition(callback func(position ResolvedPosition, triggerBounds gpui.Bounds)) *Popup {
	p.onPosition = callback
	return p
}

// Content sets the popup content (U:popup.rs:75-78): any child value.
func (p *Popup) Content(content any) *Popup {
	element := overlayIntoElement("Popup content", content)
	p.content = &element
	return p
}

// PopupResolvedCorner is the legacy anchor-point math of a trigger's
// bounds (U:popup.rs:80-99 Popup::resolved_corner).
func PopupResolvedCorner(anchor gpui.Anchor, triggerBounds gpui.Bounds) gpui.Point {
	above := triggerBounds.Origin.Y - triggerBounds.Size.Height
	switch anchor {
	case gpui.AnchorTopLeft:
		return triggerBounds.Origin
	case gpui.AnchorTopCenter:
		return triggerBounds.TopCenter()
	case gpui.AnchorTopRight:
		return triggerBounds.TopRight()
	case gpui.AnchorBottomLeft:
		return gpui.Point{X: triggerBounds.Origin.X, Y: above}
	case gpui.AnchorBottomCenter:
		return gpui.Point{X: triggerBounds.TopCenter().X, Y: above}
	case gpui.AnchorBottomRight:
		return gpui.Point{X: triggerBounds.TopRight().X, Y: above}
	}
	// LeftCenter | RightCenter.
	return triggerBounds.Origin
}

// RenderOnce implements gpui.RenderOnce (U:popup.rs:130-201): the root
// measures the trigger with a full-size canvas; once the trigger has
// been captured, the content renders deferred at PopupPriority, at the
// anchor position tracked from the trigger's prepaint, blocking the
// mouse over the surface.
func (p *Popup) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	state := w.UseKeyedState(gpui.NamedChildElementID(p.id, "anchor"), app, func(*gpui.Window, *gpui.Context[popupAnchorState]) popupAnchorState {
		return popupAnchorState{}
	})
	snapshot := state.Read(app, func(s *popupAnchorState, _ *gpui.App) popupAnchorState { return *s })
	anchor := p.anchor
	offset := p.offset
	triggerBounds := new(gpui.Bounds)
	*triggerBounds = snapshot.bounds

	measure := gpui.Canvas(func(bounds gpui.Bounds, w *gpui.Window, app *gpui.App) struct{} {
		*triggerBounds = bounds
		first := state.UpdateWith(app, func(s *popupAnchorState, _ *gpui.Context[popupAnchorState]) bool {
			first := !s.captured
			s.bounds = bounds
			s.captured = true
			return first
		})
		if first {
			gpui.RequestAnimationFrame(w)
		}
		return struct{}{}
	}, func(gpui.Bounds, struct{}, *gpui.Window, *gpui.App) {}).Flex().Absolute().SizeFull().Top0().Left0()

	style := p.Style()
	root := StyledExtOf(p.base.Child(p.trigger).Child(measure)).RefineStyle(&style)

	if p.content == nil || !snapshot.captured {
		return root.IntoElement()
	}
	// The core's element-anchored placement (browser-core ticket 72): the
	// measured trigger cell is the anchor, the corner anchor maps onto a
	// side and alignment, the offset onto the gap, and AnchorShift — the
	// positioner's clamp — is the only fallback, so a corner-anchored
	// popup never flips (U:popup.rs:95-109). The host blocks the mouse
	// over the placed box, so no caller has to remember: what a popup
	// covers belongs to the popup.
	anchored := gpui.Anchored().
		AnchorBounds(triggerBounds).
		Side(popupAnchorSide(anchor)).
		Align(popupAnchorAlign(anchor)).
		Gap(offset).
		TryFallbacks(gpui.AnchorShift).
		Margin(edgesAll(popupWindowMargin)).
		Occlude()
	if callback := p.onPosition; callback != nil {
		placement := popupAnchorPlacement(anchor)
		anchored.OnResolved(func(bounds gpui.Bounds) {
			callback(ResolvedPosition{Bounds: bounds, Placement: gpui.Some(placement)}, *triggerBounds)
		})
	}
	return root.Child(gpui.Deferred(
		anchored.Child(*p.content),
	).Priority(PopupPriority)).IntoElement()
}

// popupAnchorSide maps a corner anchor onto the side the popup is
// placed on: the popup's anchored corner sits against the opposite
// edge of the trigger (U:popup.rs:102-114 anchor_position —
// AnchorTopLeft places the popup's top-left at the trigger's
// bottom-left, so the popup sits BELOW the trigger, start-aligned).
func popupAnchorSide(anchor gpui.Anchor) gpui.AnchorSide {
	switch anchor {
	case gpui.AnchorTopLeft, gpui.AnchorTopCenter, gpui.AnchorTopRight:
		return gpui.AnchorSideBottom
	case gpui.AnchorBottomLeft, gpui.AnchorBottomCenter, gpui.AnchorBottomRight:
		return gpui.AnchorSideTop
	case gpui.AnchorLeftCenter:
		return gpui.AnchorSideRight
	}
	// RightCenter.
	return gpui.AnchorSideLeft
}

// popupAnchorAlign maps a corner anchor onto the alignment along the
// side (the corner's cross-axis position: Left/Start, Center, Right/End).
func popupAnchorAlign(anchor gpui.Anchor) gpui.AnchorAlign {
	switch anchor {
	case gpui.AnchorTopLeft, gpui.AnchorBottomLeft:
		return gpui.AnchorAlignStart
	case gpui.AnchorTopCenter, gpui.AnchorBottomCenter, gpui.AnchorLeftCenter, gpui.AnchorRightCenter:
		return gpui.AnchorAlignCenter
	}
	// TopRight | BottomRight.
	return gpui.AnchorAlignEnd
}

// popupAnchorPlacement maps a corner anchor onto the reported
// placement (the side the popup sits on).
func popupAnchorPlacement(anchor gpui.Anchor) Placement {
	switch anchor {
	case gpui.AnchorTopLeft, gpui.AnchorTopCenter, gpui.AnchorTopRight:
		return PlacementBottom
	case gpui.AnchorBottomLeft, gpui.AnchorBottomCenter, gpui.AnchorBottomRight:
		return PlacementTop
	case gpui.AnchorLeftCenter:
		return PlacementRight
	}
	// RightCenter.
	return PlacementLeft
}

// overlayIntoElement converts one child-position value (an element,
// IntoElement, string, view or recipe) into an AnyElement, panicking
// with the owner's name on invalid input (the `impl IntoElement`
// parameters of the overlay builders).
func overlayIntoElement(owner string, value any) gpui.AnyElement {
	element, err := gpui.TryIntoElement(value)
	if err != nil {
		panic(fmt.Sprintf("base: %s: %v", owner, err))
	}
	return element
}
