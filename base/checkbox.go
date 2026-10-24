package base

// An unstyled checkbox and its indicator part (U:checkbox.rs). Checkbox
// follows the Button pattern (base/button.go). CheckboxIndicator renders
// a plain div (U:checkbox.rs:222-342): it embeds only gpui.Styled and
// gpui.Parent, like upstream's Styled + ParentElement impls, and no
// interactivity.
//
// Display (map gap 2b): Checkbox and CheckboxIndicator
// (U:checkbox.rs:67, 267) are plain divs upstream, display Block under
// crates.io gpui 0.2.2 (the pin defaults to Flex, R:style.rs:1024-1027),
// so each such base is built with Block() before the caller's style
// refines over it: the upstream default layout holds and a caller's
// Flex() still wins.

import (
	"fmt"

	"github.com/hoangvu12/gpui-go/gpui"
)

// CheckboxState is the semantic value exposed by a Checkbox
// (U:checkbox.rs:15-22). The zero value is Unchecked (#[default]).
type CheckboxState uint8

const (
	// CheckboxStateUnchecked is unchecked.
	CheckboxStateUnchecked CheckboxState = iota
	// CheckboxStateChecked is checked.
	CheckboxStateChecked
	// CheckboxStateIndeterminate is indeterminate (mixed).
	CheckboxStateIndeterminate
)

// String renders the state like the derived Debug.
func (s CheckboxState) String() string {
	switch s {
	case CheckboxStateUnchecked:
		return "Unchecked"
	case CheckboxStateChecked:
		return "Checked"
	case CheckboxStateIndeterminate:
		return "Indeterminate"
	}
	return fmt.Sprintf("CheckboxState(%d)", uint8(s))
}

// toggled maps the state onto accesskit's tri-state (U:checkbox.rs:25-31).
func (s CheckboxState) toggled() gpui.Toggled {
	switch s {
	case CheckboxStateChecked:
		return gpui.ToggledTrue
	case CheckboxStateIndeterminate:
		return gpui.ToggledMixed
	}
	return gpui.ToggledFalse
}

// activated returns the state an activation requests: unchecked and
// indeterminate become checked, checked becomes unchecked
// (U:checkbox.rs:33-38).
func (s CheckboxState) activated() CheckboxState {
	if s == CheckboxStateChecked {
		return CheckboxStateUnchecked
	}
	return CheckboxStateChecked
}

// CheckboxChangeHandler handles an activation with the next controlled
// state (U:checkbox.rs:13).
type CheckboxChangeHandler = func(next CheckboxState, event *gpui.ClickEvent, w *gpui.Window, app *gpui.App)

// Checkbox is an unstyled checkbox that owns toggle, focus, keyboard and
// accessibility behavior (U:checkbox.rs:41-60). The application owns all
// layout and visual state rendering.
type Checkbox struct {
	gpui.Component[*Checkbox]

	id                  gpui.ElementID
	base                *gpui.DivElement
	semanticStyles      CheckboxStyles
	state               CheckboxState
	disabled            bool
	onChange            CheckboxChangeHandler
	accessibilityLabel  *string
	tabIndex            int64
	tabStop             bool
	providedFocusHandle *gpui.FocusHandle
	role                RoleOverride
}

// NewCheckbox creates an unchecked checkbox with a stable element id
// (U:checkbox.rs:63-81 Checkbox::new).
func NewCheckbox[I gpui.IntoElementID](id I) *Checkbox {
	eid := gpui.ElementIDOf(id)
	base := gpui.Div().Block().ID(eid)
	return gpui.BindComponent(&Checkbox{id: eid, base: base, tabStop: true, role: RoleOverrideImplicit()}, base)
}

// Role overrides the accessibility role; the default is RoleCheckBox
// (U:checkbox.rs:82-85).
func (c *Checkbox) Role(role RoleOverride) *Checkbox {
	c.role = role
	return c
}

// State sets the controlled semantic state (U:checkbox.rs:87-91).
func (c *Checkbox) State(state CheckboxState) *Checkbox {
	c.state = state
	return c
}

// Checked sets the checked state, clearing any indeterminate state
// (U:checkbox.rs:93-100).
func (c *Checkbox) Checked(checked bool) *Checkbox {
	if checked {
		return c.State(CheckboxStateChecked)
	}
	return c.State(CheckboxStateUnchecked)
}

// Indeterminate sets or clears the indeterminate state; clearing it
// leaves the checkbox unchecked (U:checkbox.rs:102-114).
func (c *Checkbox) Indeterminate(indeterminate bool) *Checkbox {
	if indeterminate {
		return c.State(CheckboxStateIndeterminate)
	}
	if c.state == CheckboxStateIndeterminate {
		return c.State(CheckboxStateUnchecked)
	}
	return c
}

// Disabled sets whether activation is ignored (U:checkbox.rs:116-120).
func (c *Checkbox) Disabled(disabled bool) *Checkbox {
	c.disabled = disabled
	return c
}

// Styles defines application-owned styles for the semantic states
// (U:checkbox.rs:122-126).
func (c *Checkbox) Styles(build func(CheckboxStyles) CheckboxStyles) *Checkbox {
	c.semanticStyles = build(c.semanticStyles)
	return c
}

// AccessibilityLabel sets the accessible label (U:checkbox.rs:128-132).
func (c *Checkbox) AccessibilityLabel(label string) *Checkbox {
	c.accessibilityLabel = &label
	return c
}

// OnChange handles an activation with the next controlled state; an
// indeterminate checkbox becomes checked (U:checkbox.rs:134-145).
func (c *Checkbox) OnChange(handler CheckboxChangeHandler) *Checkbox {
	c.onChange = handler
	return c
}

// TabIndex sets the focus traversal index (U:checkbox.rs:147-151).
func (c *Checkbox) TabIndex(tabIndex int64) *Checkbox {
	c.tabIndex = tabIndex
	return c
}

// TabStop sets keyboard traversal participation (U:checkbox.rs:153-157).
func (c *Checkbox) TabStop(tabStop bool) *Checkbox {
	c.tabStop = tabStop
	return c
}

// TrackFocus uses a caller-owned focus handle (U:checkbox.rs:159-163).
func (c *Checkbox) TrackFocus(focusHandle gpui.FocusHandle) *Checkbox {
	c.providedFocusHandle = &focusHandle
	return c
}

// checkboxStateLayers returns the active value-state and disabled layers
// (the shared resolved_style body, U:checkbox.rs:174-188, 302-316).
func checkboxStateLayers(state CheckboxState, disabled bool, checked, indeterminate, disabledStyle *gpui.StyleRefinement) []*gpui.StyleRefinement {
	var states []*gpui.StyleRefinement
	switch state {
	case CheckboxStateChecked:
		states = append(states, checked)
	case CheckboxStateIndeterminate:
		states = append(states, indeterminate)
	}
	if disabled {
		states = append(states, disabledStyle)
	}
	return states
}

// resolvedStyle layers instance, state and disabled (U:checkbox.rs:174-188).
func (c *Checkbox) resolvedStyle() gpui.StyleRefinement {
	instance := c.Style()
	s := &c.semanticStyles
	return resolveStyle(&instance, checkboxStateLayers(c.state, c.disabled, &s.checked, &s.indeterminate, &s.disabled)...)
}

// CheckboxStyles are the semantic styles supported by Checkbox
// (U:checkbox.rs:191-220).
type CheckboxStyles struct {
	checked       gpui.StyleRefinement
	indeterminate gpui.StyleRefinement
	disabled      gpui.StyleRefinement
}

// Checked refines the root style when checked (U:checkbox.rs:200-205).
func (s CheckboxStyles) Checked(build func(*StateStyle) *StateStyle) CheckboxStyles {
	s.checked = refineState(s.checked, build)
	return s
}

// Indeterminate refines the root style when indeterminate
// (U:checkbox.rs:207-212).
func (s CheckboxStyles) Indeterminate(build func(*StateStyle) *StateStyle) CheckboxStyles {
	s.indeterminate = refineState(s.indeterminate, build)
	return s
}

// Disabled refines the root style when disabled (U:checkbox.rs:214-219).
func (s CheckboxStyles) Disabled(build func(*StateStyle) *StateStyle) CheckboxStyles {
	s.disabled = refineState(s.disabled, build)
	return s
}

// RenderOnce implements gpui.RenderOnce (U:checkbox.rs:364-399).
func (c *Checkbox) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return c.render(w, app).IntoElement()
}

// render builds the checkbox's root div (U:checkbox.rs:365-398). Unlike
// Button and Toggle, a disabled checkbox does not swallow mouse-down, so
// pointer events bubble to its parent (U:checkbox.rs:550-558).
func (c *Checkbox) render(w *gpui.Window, app *gpui.App) *gpui.DivElement {
	focusHandle := keyedFocusHandle(c.providedFocusHandle, c.id, w, app)
	disabled := c.disabled
	nextState := c.state.activated()
	style := c.resolvedStyle()
	onChange := c.onChange

	d := TestSupport(c.base).
		// A form control, like the UA stylesheet's opt-outs
		// (user-select: none, html.css:850/:904; browser-core 20's
		// default-on selection opts it back out).
		SelectNone()
	if role, ok := c.role.Resolve(func() gpui.Role { return gpui.RoleCheckBox }); ok {
		d.Role(role)
	}
	d.AriaToggled(c.state.toggled())
	if c.accessibilityLabel != nil {
		d.AriaLabel(*c.accessibilityLabel)
	}
	if !disabled {
		d.TrackFocus(focusHandle.TabIndex(c.tabIndex).TabStop(c.tabStop))
	}
	if !disabled && onChange != nil {
		d.OnClick(func(event *gpui.ClickEvent, w *gpui.Window, app *gpui.App) {
			onChange(nextState, event, w, app)
		})
	}
	d.Extend(c.TakeChildren())
	return StyledExtOf(d).RefineStyle(&style)
}

// CheckboxIndicator is an unstyled checkbox indicator part with typed
// checkbox-state projection (U:checkbox.rs:222-234). It renders its div
// directly; applications own its geometry, presentation and children.
type CheckboxIndicator struct {
	gpui.Styled[*CheckboxIndicator]
	gpui.Parent[*CheckboxIndicator]

	semanticStyles CheckboxIndicatorStyles
	state          CheckboxState
	disabled       bool
}

// CheckboxIndicatorStyles are the semantic styles supported by
// CheckboxIndicator (U:checkbox.rs:236-262).
type CheckboxIndicatorStyles struct {
	checked       gpui.StyleRefinement
	indeterminate gpui.StyleRefinement
	disabled      gpui.StyleRefinement
}

// Checked refines the indicator when checked (U:checkbox.rs:245-249).
func (s CheckboxIndicatorStyles) Checked(build func(*StateStyle) *StateStyle) CheckboxIndicatorStyles {
	s.checked = refineState(s.checked, build)
	return s
}

// Indeterminate refines the indicator when indeterminate
// (U:checkbox.rs:251-255).
func (s CheckboxIndicatorStyles) Indeterminate(build func(*StateStyle) *StateStyle) CheckboxIndicatorStyles {
	s.indeterminate = refineState(s.indeterminate, build)
	return s
}

// Disabled refines the indicator when disabled (U:checkbox.rs:257-261).
func (s CheckboxIndicatorStyles) Disabled(build func(*StateStyle) *StateStyle) CheckboxIndicatorStyles {
	s.disabled = refineState(s.disabled, build)
	return s
}

// NewCheckboxIndicator creates an unchecked indicator
// (U:checkbox.rs:264-274; Default at 319-323).
func NewCheckboxIndicator() *CheckboxIndicator {
	i := gpui.BindStyled(&CheckboxIndicator{})
	return gpui.BindParent(i)
}

// DefaultCheckboxIndicator is Default for CheckboxIndicator
// (U:checkbox.rs:319-323).
func DefaultCheckboxIndicator() *CheckboxIndicator { return NewCheckboxIndicator() }

// State sets the projected state (U:checkbox.rs:276-279).
func (i *CheckboxIndicator) State(state CheckboxState) *CheckboxIndicator {
	i.state = state
	return i
}

// Checked sets checked or unchecked (U:checkbox.rs:281-287).
func (i *CheckboxIndicator) Checked(checked bool) *CheckboxIndicator {
	if checked {
		return i.State(CheckboxStateChecked)
	}
	return i.State(CheckboxStateUnchecked)
}

// Disabled sets the projected disabled state (U:checkbox.rs:289-292).
func (i *CheckboxIndicator) Disabled(disabled bool) *CheckboxIndicator {
	i.disabled = disabled
	return i
}

// Styles defines the indicator's semantic styles (U:checkbox.rs:294-300).
func (i *CheckboxIndicator) Styles(build func(CheckboxIndicatorStyles) CheckboxIndicatorStyles) *CheckboxIndicator {
	i.semanticStyles = build(i.semanticStyles)
	return i
}

// resolvedStyle layers instance, state and disabled (U:checkbox.rs:302-316).
func (i *CheckboxIndicator) resolvedStyle() gpui.StyleRefinement {
	instance := i.Style()
	s := &i.semanticStyles
	return resolveStyle(&instance, checkboxStateLayers(i.state, i.disabled, &s.checked, &s.indeterminate, &s.disabled)...)
}

// IntoElement renders the indicator's div (U:checkbox.rs:337-342; it
// needs no window, so it is an IntoElement rather than a recipe).
func (i *CheckboxIndicator) IntoElement() gpui.AnyElement {
	style := i.resolvedStyle()
	d := gpui.Div().Block().Extend(i.TakeChildren())
	return StyledExtOf(d).RefineStyle(&style).IntoElement()
}
