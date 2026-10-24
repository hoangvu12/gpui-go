package base

// An unstyled switch and its track and thumb parts (U:switch.rs). Switch
// follows the Button pattern (base/button.go). SwitchTrack is a stateful
// div with interactivity (U:switch.rs:58-155); SwitchThumb a plain div
// with only Styled and Parent (U:switch.rs:157-241). Neither part needs
// the window, so both convert directly (IntoElement) instead of being
// recipes.
//
// Display (map gap 2b): Switch, SwitchTrack and SwitchThumb
// (U:switch.rs:93, 192, 247) are plain divs upstream, display Block under
// crates.io gpui 0.2.2 (the pin defaults to Flex, R:style.rs:1024-1027),
// so each such base is built with Block() before the caller's style
// refines over it: the upstream default layout holds and a caller's
// Flex() still wins.

import "github.com/hoangvu12/gpui-go/gpui"

// SwitchChangeHandler handles activation with the next checked value
// and its input event (U:switch.rs:13).
type SwitchChangeHandler = func(next bool, event *gpui.ClickEvent, w *gpui.Window, app *gpui.App)

// Switch is an unstyled binary control that owns switch interaction and
// semantics (U:switch.rs:15-35). The checked value is controlled by the
// application: activation reports the next value through OnChange and
// the application renders it back through Checked.
type Switch struct {
	gpui.Component[*Switch]

	id                  gpui.ElementID
	base                *gpui.DivElement
	semanticStyles      SwitchStyles
	checked             bool
	disabled            bool
	onChange            SwitchChangeHandler
	accessibilityLabel  *string
	tabIndex            int64
	tabStop             bool
	providedFocusHandle *gpui.FocusHandle
}

// SwitchStyles are the semantic root styles supported by Switch
// (U:switch.rs:37-56).
type SwitchStyles struct {
	checked  gpui.StyleRefinement
	disabled gpui.StyleRefinement
}

// Checked refines the root style while checked (U:switch.rs:45-49).
func (s SwitchStyles) Checked(build func(*StateStyle) *StateStyle) SwitchStyles {
	s.checked = refineState(s.checked, build)
	return s
}

// Disabled refines the root style while disabled (U:switch.rs:51-55).
func (s SwitchStyles) Disabled(build func(*StateStyle) *StateStyle) SwitchStyles {
	s.disabled = refineState(s.disabled, build)
	return s
}

// switchLayers returns the active checked and disabled layers (the
// shared resolved_style body, U:switch.rs:117-127, 211-221, 280-290).
func switchLayers(checked, disabled bool, checkedStyle, disabledStyle *gpui.StyleRefinement) []*gpui.StyleRefinement {
	var states []*gpui.StyleRefinement
	if checked {
		states = append(states, checkedStyle)
	}
	if disabled {
		states = append(states, disabledStyle)
	}
	return states
}

// SwitchTrack is an unstyled switch track with typed checked and
// disabled state projection (U:switch.rs:58-67).
type SwitchTrack struct {
	gpui.Component[*SwitchTrack]

	base           *gpui.DivElement
	semanticStyles SwitchTrackStyles
	checked        bool
	disabled       bool
}

// SwitchTrackStyles are the semantic styles supported by SwitchTrack
// (U:switch.rs:69-88).
type SwitchTrackStyles struct {
	checked  gpui.StyleRefinement
	disabled gpui.StyleRefinement
}

// Checked refines the track while checked (U:switch.rs:77-81).
func (s SwitchTrackStyles) Checked(build func(*StateStyle) *StateStyle) SwitchTrackStyles {
	s.checked = refineState(s.checked, build)
	return s
}

// Disabled refines the track while disabled (U:switch.rs:83-87).
func (s SwitchTrackStyles) Disabled(build func(*StateStyle) *StateStyle) SwitchTrackStyles {
	s.disabled = refineState(s.disabled, build)
	return s
}

// NewSwitchTrack creates a track with the given element id
// (U:switch.rs:90-100). Scope it to its switch with
// gpui.NamedChildElementID(switchID, "track") (U:switch.rs:456-477).
func NewSwitchTrack[I gpui.IntoElementID](id I) *SwitchTrack {
	base := gpui.Div().Block().ID(gpui.ElementIDOf(id))
	return gpui.BindComponent(&SwitchTrack{base: base}, base)
}

// Checked sets the projected checked state (U:switch.rs:102-105).
func (t *SwitchTrack) Checked(checked bool) *SwitchTrack {
	t.checked = checked
	return t
}

// Disabled sets the projected disabled state (U:switch.rs:107-110).
func (t *SwitchTrack) Disabled(disabled bool) *SwitchTrack {
	t.disabled = disabled
	return t
}

// Styles defines the track's semantic styles (U:switch.rs:112-115).
func (t *SwitchTrack) Styles(build func(SwitchTrackStyles) SwitchTrackStyles) *SwitchTrack {
	t.semanticStyles = build(t.semanticStyles)
	return t
}

// resolvedStyle layers instance, checked and disabled
// (U:switch.rs:117-127).
func (t *SwitchTrack) resolvedStyle() gpui.StyleRefinement {
	instance := t.Style()
	s := &t.semanticStyles
	return resolveStyle(&instance, switchLayers(t.checked, t.disabled, &s.checked, &s.disabled)...)
}

// IntoElement renders the track's div (U:switch.rs:150-155).
func (t *SwitchTrack) IntoElement() gpui.AnyElement {
	style := t.resolvedStyle()
	d := t.base.Extend(t.TakeChildren())
	return StyledExtOf(d).RefineStyle(&style).IntoElement()
}

// SwitchThumb is an unstyled switch thumb with typed checked and
// disabled state projection (U:switch.rs:157-166).
type SwitchThumb struct {
	gpui.Styled[*SwitchThumb]
	gpui.Parent[*SwitchThumb]

	semanticStyles SwitchThumbStyles
	checked        bool
	disabled       bool
}

// SwitchThumbStyles are the semantic styles supported by SwitchThumb
// (U:switch.rs:168-187).
type SwitchThumbStyles struct {
	checked  gpui.StyleRefinement
	disabled gpui.StyleRefinement
}

// Checked refines the thumb while checked (U:switch.rs:176-180).
func (s SwitchThumbStyles) Checked(build func(*StateStyle) *StateStyle) SwitchThumbStyles {
	s.checked = refineState(s.checked, build)
	return s
}

// Disabled refines the thumb while disabled (U:switch.rs:182-186).
func (s SwitchThumbStyles) Disabled(build func(*StateStyle) *StateStyle) SwitchThumbStyles {
	s.disabled = refineState(s.disabled, build)
	return s
}

// NewSwitchThumb creates a thumb projecting checked (U:switch.rs:189-199).
func NewSwitchThumb(checked bool) *SwitchThumb {
	t := gpui.BindStyled(&SwitchThumb{checked: checked})
	return gpui.BindParent(t)
}

// Disabled sets the projected disabled state (U:switch.rs:201-204).
func (t *SwitchThumb) Disabled(disabled bool) *SwitchThumb {
	t.disabled = disabled
	return t
}

// Styles defines the thumb's semantic styles (U:switch.rs:206-209).
func (t *SwitchThumb) Styles(build func(SwitchThumbStyles) SwitchThumbStyles) *SwitchThumb {
	t.semanticStyles = build(t.semanticStyles)
	return t
}

// resolvedStyle layers instance, checked and disabled
// (U:switch.rs:211-221).
func (t *SwitchThumb) resolvedStyle() gpui.StyleRefinement {
	instance := t.Style()
	s := &t.semanticStyles
	return resolveStyle(&instance, switchLayers(t.checked, t.disabled, &s.checked, &s.disabled)...)
}

// IntoElement renders the thumb's div (U:switch.rs:236-241).
func (t *SwitchThumb) IntoElement() gpui.AnyElement {
	style := t.resolvedStyle()
	d := gpui.Div().Block().Extend(t.TakeChildren())
	return StyledExtOf(d).RefineStyle(&style).IntoElement()
}

// NewSwitch creates an unchecked switch (U:switch.rs:243-260).
func NewSwitch[I gpui.IntoElementID](id I) *Switch {
	eid := gpui.ElementIDOf(id)
	base := gpui.Div().Block().ID(eid)
	return gpui.BindComponent(&Switch{id: eid, base: base, tabStop: true}, base)
}

// Checked sets the application-controlled checked value
// (U:switch.rs:262-266).
func (s *Switch) Checked(checked bool) *Switch {
	s.checked = checked
	return s
}

// Disabled sets whether activation is ignored (U:switch.rs:268-272).
func (s *Switch) Disabled(disabled bool) *Switch {
	s.disabled = disabled
	return s
}

// Styles configures the semantic state styles (U:switch.rs:274-278).
func (s *Switch) Styles(build func(SwitchStyles) SwitchStyles) *Switch {
	s.semanticStyles = build(s.semanticStyles)
	return s
}

// resolvedStyle layers instance, checked and disabled
// (U:switch.rs:280-290).
func (s *Switch) resolvedStyle() gpui.StyleRefinement {
	instance := s.Style()
	st := &s.semanticStyles
	return resolveStyle(&instance, switchLayers(s.checked, s.disabled, &st.checked, &st.disabled)...)
}

// OnChange handles activation with the next checked value
// (U:switch.rs:292-299).
func (s *Switch) OnChange(handler SwitchChangeHandler) *Switch {
	s.onChange = handler
	return s
}

// AccessibilityLabel sets the accessible name (U:switch.rs:301-305).
func (s *Switch) AccessibilityLabel(label string) *Switch {
	s.accessibilityLabel = &label
	return s
}

// TabIndex sets the focus traversal index (U:switch.rs:307-311).
func (s *Switch) TabIndex(tabIndex int64) *Switch {
	s.tabIndex = tabIndex
	return s
}

// TabStop sets keyboard traversal participation (U:switch.rs:313-317).
func (s *Switch) TabStop(tabStop bool) *Switch {
	s.tabStop = tabStop
	return s
}

// TrackFocus uses a caller-owned focus handle (U:switch.rs:319-323).
func (s *Switch) TrackFocus(focusHandle gpui.FocusHandle) *Switch {
	s.providedFocusHandle = &focusHandle
	return s
}

// RenderOnce implements gpui.RenderOnce (U:switch.rs:355-396).
func (s *Switch) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return s.render(w, app).IntoElement()
}

// render builds the switch's root div (U:switch.rs:356-395).
func (s *Switch) render(w *gpui.Window, app *gpui.App) *gpui.DivElement {
	focusHandle := keyedFocusHandle(s.providedFocusHandle, s.id, w, app)
	checked := s.checked
	disabled := s.disabled
	style := s.resolvedStyle()
	onChange := s.onChange

	d := TestSupport(s.base).
		Role(gpui.RoleSwitch).
		// A form control, like the UA stylesheet's opt-outs
		// (user-select: none, html.css:850/:904; browser-core 20's
		// default-on selection opts it back out).
		SelectNone().
		AriaToggled(toggledOf(checked))
	if s.accessibilityLabel != nil {
		d.AriaLabel(*s.accessibilityLabel)
	}
	if !disabled {
		d.TrackFocus(focusHandle.TabIndex(s.tabIndex).TabStop(s.tabStop))
	} else {
		d.OnMouseDown(gpui.MouseButtonLeft, stopPropagation)
	}
	if !disabled && onChange != nil {
		d.OnClick(func(event *gpui.ClickEvent, w *gpui.Window, app *gpui.App) {
			onChange(!checked, event, w, app)
		})
	}
	d.Extend(s.TakeChildren())
	return StyledExtOf(d).RefineStyle(&style)
}
