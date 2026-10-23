package base

// An unstyled container for a set of radios (U:radio_group.rs).
//
// Display (map gap 2b): upstream's group is a plain div
// (U:radio_group.rs:22), display Block under crates.io gpui 0.2.2 (the
// pin defaults to Flex, R:style.rs:1024-1027), so each such base is built
// with Block() before the caller's style refines over it: the upstream
// default layout holds and a caller's Flex() still wins.

import "github.com/hoangvu12/gpui-go/gpui"

// RadioGroup is an unstyled container for a set of radio elements
// (U:radio_group.rs:10-17). Its default axis is vertical.
type RadioGroup struct {
	gpui.Component[*RadioGroup]

	base *gpui.DivElement
	axis gpui.Axis
}

// NewRadioGroup creates a vertical radio group (U:radio_group.rs:19-27).
func NewRadioGroup[I gpui.IntoElementID](id I) *RadioGroup {
	base := gpui.Div().Block().ID(gpui.ElementIDOf(id))
	return gpui.BindComponent(&RadioGroup{base: base, axis: gpui.AxisVertical}, base)
}

// Axis sets the semantic axis of the group (U:radio_group.rs:29-33).
func (g *RadioGroup) Axis(axis gpui.Axis) *RadioGroup {
	g.axis = axis
	return g
}

// radioGroupOrientation maps an axis onto the accesskit orientation
// (U:radio_group.rs:60-63, U:toggle_group.rs:60-63).
func radioGroupOrientation(axis gpui.Axis) gpui.A11yOrientation {
	if axis == gpui.AxisHorizontal {
		return gpui.A11yOrientationHorizontal
	}
	return gpui.A11yOrientationVertical
}

// IntoElement renders the group (U:radio_group.rs:56-66; the render
// needs no window, so the component converts directly).
func (g *RadioGroup) IntoElement() gpui.AnyElement {
	return g.render().IntoElement()
}

// render builds the group's root div.
func (g *RadioGroup) render() *gpui.DivElement {
	style := g.Style()
	d := g.base.
		Role(gpui.RoleRadioGroup).
		AriaOrientation(radioGroupOrientation(g.axis)).
		Extend(g.TakeChildren())
	return StyledExtOf(d).RefineStyle(&style)
}
