package base

// An unstyled tooltip popup (U:tooltip.rs). The tooltip's lifecycle —
// the hover and keyboard-focus triggers, Chrome's interest delays
// (250 ms in, 150 ms out), the grace period, the hint layer of the top
// layer, Escape and the light dismiss — is the core's tooltip hook
// (browser-core ticket 24, gpui/tooltip.go): an element carries the
// tooltip (DivElement.Tooltip for the plain default look, TooltipView
// for custom content such as this Tooltip surface), and the window
// renders it in the hint layer. Base's own per-window provider
// (U:tooltip.rs:93-248 TooltipOverlay) is deleted with it
// (browser-core ticket 72): an application-owned TooltipOverlay with
// its own delays and deferred rendering duplicated the hook.
//
// Go adaptations, recorded:
//
//   - TooltipTransition and the provider's renderer are gone with the
//     provider: the core hook has only the toggle (OnTooltipToggle).
//   - Display (map.md gap 2b): the Tooltip base (U:tooltip.rs:31) is a
//     plain div upstream, display Block under crates.io gpui 0.2.2
//     (the pin defaults to Flex, R:style.rs:1024-1027), so it is built
//     with Block(); a caller's Flex() still wins.

import "github.com/hoangvu12/gpui-go/gpui"

// tooltipWindowMargin is the distance kept from the window edge
// (U:tooltip.rs:12).
const tooltipWindowMargin float32 = 4

// Tooltip is an unstyled tooltip popup (U:tooltip.rs:19-26), Base UI's
// Tooltip.Popup: it owns the accessible tooltip role and accepts
// application-owned content and presentation. Attach it to an element
// with the core hook — Div().ID("anchor").TooltipView(func(w, app) {
// return NewTooltip("tip").Child(...).IntoElement() }) — and place it
// beside the anchor with Anchored().AnchorTo(<the anchor's id>)
// (TooltipPositioner wraps that placement for the trigger-bounds form).
// Its Styled and Parent methods reach the root div (U:tooltip.rs:36-46).
type Tooltip struct {
	gpui.Styled[*Tooltip]

	base *gpui.DivElement
}

// NewTooltip creates a tooltip popup (U:tooltip.rs:28-34 Tooltip::new).
func NewTooltip[I gpui.IntoElementID](id I) *Tooltip {
	base := gpui.Div().Block().ID(gpui.ElementIDOf(id)).Role(gpui.RoleTooltip)
	return gpui.BindStyledTo(&Tooltip{base: base}, base.StyleMut())
}

// Extend appends children (U:tooltip.rs:42-46).
func (t *Tooltip) Extend(elements []gpui.AnyElement) { t.base.Extend(elements) }

// Child adds one child (ParentElement::child).
func (t *Tooltip) Child(value any) *Tooltip {
	t.base.Child(value)
	return t
}

// Children adds children in order (ParentElement::children).
func (t *Tooltip) Children(values ...any) *Tooltip {
	t.base.Children(values...)
	return t
}

// RenderOnce implements gpui.RenderOnce (U:tooltip.rs:48-52).
func (t *Tooltip) RenderOnce(*gpui.Window, *gpui.App) gpui.AnyElement {
	return t.base.IntoElement()
}

// TooltipPositioner is an unstyled tooltip positioner with
// viewport-aware flipping and clamping (U:tooltip.rs:250-279): a
// tooltip-named view of the core's element-anchored Anchored side
// placement (browser-core ticket 25 moved the positioner's side
// strategy into gpui.Anchored — position-anchor, the side/align
// placement, the flip fallback and the shift clamp — so every anchored
// surface in Base resolves its position through the same core rule as
// the browser's CSS anchor positioning). It adds no element of its
// own; the core Anchored is what gets rendered.
type TooltipPositioner struct {
	anchored *gpui.AnchoredElement
}

// NewTooltipPositioner positions beside the tracked trigger bounds
// with the tooltip window margin (U:tooltip.rs:257-259): top side by
// default, center-aligned, flipping to the opposite side when the
// preferred one does not fit and shifting into the viewport otherwise
// (the positioner's flip-then-clamp). The side-axis flip tactic is
// picked per side — flip-block flips a vertical side and flip-inline a
// horizontal one (CSS's tactics mirror across the anchor's midlines,
// so the tactic that flips the side depends on the side's axis).
func NewTooltipPositioner(triggerBounds *gpui.Bounds) *TooltipPositioner {
	return &TooltipPositioner{anchored: gpui.Anchored().
		AnchorBounds(triggerBounds).
		Side(gpui.AnchorSideTop).
		Align(gpui.AnchorAlignCenter).
		TryFallbacks(gpui.AnchorFlipBlock, gpui.AnchorShift).
		Margin(edgesAll(tooltipWindowMargin))}
}

// Placement sets the preferred side (U:tooltip.rs:261-264).
func (t *TooltipPositioner) Placement(placement Placement) *TooltipPositioner {
	t.anchored = t.anchored.Side(anchorSideOf(placement)).TryFallbacks(anchorSideFlip(placement), gpui.AnchorShift)
	return t
}

// Extend appends children (U:tooltip.rs:267-271).
func (t *TooltipPositioner) Extend(elements []gpui.AnyElement) {
	for _, element := range elements {
		t.anchored.Child(element)
	}
}

// Child adds one child (ParentElement::child).
func (t *TooltipPositioner) Child(value any) *TooltipPositioner {
	t.anchored.Child(value)
	return t
}

// IntoElement renders the core Anchored (U:tooltip.rs:273-279).
func (t *TooltipPositioner) IntoElement() gpui.AnyElement { return t.anchored.IntoElement() }

// anchorSideOf maps the placement onto the anchored side.
func anchorSideOf(placement Placement) gpui.AnchorSide {
	switch placement {
	case PlacementBottom:
		return gpui.AnchorSideBottom
	case PlacementLeft:
		return gpui.AnchorSideLeft
	case PlacementRight:
		return gpui.AnchorSideRight
	}
	return gpui.AnchorSideTop
}

// anchorSideFlip returns the flip tactic that flips the side the
// placement sits on: flip-block on a vertical side, flip-inline on a
// horizontal one (the CSS tactics mirror across the anchor's
// midlines).
func anchorSideFlip(placement Placement) gpui.AnchorFallback {
	switch placement {
	case PlacementLeft, PlacementRight:
		return gpui.AnchorFlipInline
	}
	return gpui.AnchorFlipBlock
}
