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

import "github.com/hoangvu12/gpui-go/gpui"

// popupWindowMargin is the distance kept between a popup and the window
// edge (U:popup.rs:11-12).
const popupWindowMargin float32 = 8

// PopupPriority is the deferred paint priority of interactive surfaces
// that must appear above dialogs (U:popup.rs:14-15 POPUP_PRIORITY).
const PopupPriority = 100

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

// popupAnchorPosition matches the popup's anchor to the opposite edge of
// the measured trigger (U:popup.rs:102-114 anchor_position).
func popupAnchorPosition(anchor gpui.Anchor, trigger gpui.Bounds, offset float32) gpui.Point {
	add := func(p gpui.Point, dx, dy float32) gpui.Point { return gpui.Point{X: p.X + dx, Y: p.Y + dy} }
	switch anchor {
	case gpui.AnchorTopLeft:
		return add(trigger.BottomLeft(), 0, offset)
	case gpui.AnchorTopCenter:
		return add(trigger.BottomCenter(), 0, offset)
	case gpui.AnchorTopRight:
		return add(trigger.BottomRight(), 0, offset)
	case gpui.AnchorBottomLeft:
		return add(trigger.Origin, 0, -offset)
	case gpui.AnchorBottomCenter:
		return add(trigger.TopCenter(), 0, -offset)
	case gpui.AnchorBottomRight:
		return add(trigger.TopRight(), 0, -offset)
	case gpui.AnchorLeftCenter:
		return add(trigger.RightCenter(), offset, 0)
	}
	// RightCenter.
	return add(trigger.LeftCenter(), -offset, 0)
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
	position := new(gpui.Point)
	*position = popupAnchorPosition(anchor, snapshot.bounds, offset)

	measure := gpui.Canvas(func(bounds gpui.Bounds, w *gpui.Window, app *gpui.App) struct{} {
		*triggerBounds = bounds
		*position = popupAnchorPosition(anchor, bounds, offset)
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
	}, func(gpui.Bounds, struct{}, *gpui.Window, *gpui.App) {}).Absolute().SizeFull().Top0().Left0()

	style := p.Style()
	root := StyledExtOf(p.base.Child(p.trigger).Child(measure)).RefineStyle(&style)

	if p.content == nil || !snapshot.captured {
		return root.IntoElement()
	}
	positioner := PositionerCorner(anchor, *position).trackedCornerPosition(position)
	if callback := p.onPosition; callback != nil {
		positioner.OnPosition(func(resolved ResolvedPosition) { callback(resolved, *triggerBounds) })
	}
	return root.Child(gpui.Deferred(
		positioner.
			Margin(popupWindowMargin).
			// The host blocks the mouse, so no caller has to remember:
			// what a popup covers belongs to the popup.
			Occlude().
			Child(*p.content),
	).Priority(PopupPriority)).IntoElement()
}
