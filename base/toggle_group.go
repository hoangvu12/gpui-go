package base

// An unstyled container for a set of toggles (U:toggle_group.rs).
//
// Display (map gap 2b): upstream's group is a plain div
// (U:toggle_group.rs:22), display Block under crates.io gpui 0.2.2 (the
// pin defaults to Flex, R:style.rs:1024-1027), so each such base is built
// with Block() before the caller's style refines over it: the upstream
// default layout holds and a caller's Flex() still wins.

import "github.com/hoangvu12/gpui-go/gpui"

// ToggleGroup is an unstyled container for a set of toggle elements
// (U:toggle_group.rs:10-17). Its default axis is horizontal.
type ToggleGroup struct {
	gpui.Component[*ToggleGroup]

	base *gpui.DivElement
	axis gpui.Axis
}

// NewToggleGroup creates a horizontal toggle group
// (U:toggle_group.rs:19-27).
func NewToggleGroup[I gpui.IntoElementID](id I) *ToggleGroup {
	base := gpui.Div().Block().ID(gpui.ElementIDOf(id))
	return gpui.BindComponent(&ToggleGroup{base: base, axis: gpui.AxisHorizontal}, base)
}

// Axis sets the semantic axis of the group (U:toggle_group.rs:29-33).
func (g *ToggleGroup) Axis(axis gpui.Axis) *ToggleGroup {
	g.axis = axis
	return g
}

// IntoElement renders the group (U:toggle_group.rs:56-66): a Toolbar
// node with the axis orientation.
func (g *ToggleGroup) IntoElement() gpui.AnyElement {
	return g.render().IntoElement()
}

// render builds the group's root div.
func (g *ToggleGroup) render() *gpui.DivElement {
	style := g.Style()
	d := g.base.
		Role(gpui.RoleToolbar).
		AriaOrientation(radioGroupOrientation(g.axis)).
		Extend(g.TakeChildren())
	return StyledExtOf(d).RefineStyle(&style)
}
