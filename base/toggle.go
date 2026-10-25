package base

// An unstyled, controlled toggle button (U:toggle.rs). It follows the
// Button pattern (base/button.go): gpui.Component for the instance
// style, children and delegated interactivity; a RenderOnce recipe whose
// render returns the root div.

import "github.com/hoangvu12/gpui-go/gpui"

// ToggleChangeHandler handles a request to change a controlled boolean
// state: the requested next value and the activating click
// (U:toggle.rs:13 ChangeHandler).
type ToggleChangeHandler = func(next bool, event *gpui.ClickEvent, w *gpui.Window, app *gpui.App)

// Toggle is an unstyled, controlled toggle button (U:toggle.rs:15-33).
// It owns activation, focus and accessibility; the application owns the
// layout, visual states, sizes and variants.
type Toggle struct {
	gpui.Component[*Toggle]

	id                 gpui.ElementID
	base               *gpui.DivElement
	semanticStyles     ToggleStyles
	pressed            bool
	disabled           bool
	onChange           ToggleChangeHandler
	accessibilityLabel *string
	tabIndex           int64
	tabStop            bool
	trackedFocus       *gpui.FocusHandle
}

// ToggleStyles are the semantic root styles supported by Toggle
// (U:toggle.rs:35-54). The zero value is Rust's Default.
type ToggleStyles struct {
	pressed  gpui.StyleRefinement
	disabled gpui.StyleRefinement
}

// Pressed refines the root style while pressed (U:toggle.rs:43-47).
func (s ToggleStyles) Pressed(build func(*StateStyle) *StateStyle) ToggleStyles {
	s.pressed = refineState(s.pressed, build)
	return s
}

// Disabled refines the root style while disabled (U:toggle.rs:49-53).
func (s ToggleStyles) Disabled(build func(*StateStyle) *StateStyle) ToggleStyles {
	s.disabled = refineState(s.disabled, build)
	return s
}

// NewToggle creates a toggle with the given element id
// (U:toggle.rs:57-73 Toggle::new).
func NewToggle[I gpui.IntoElementID](id I) *Toggle {
	eid := gpui.ElementIDOf(id)
	base := gpui.Div().Flex().ID(eid)
	return gpui.BindComponent(&Toggle{id: eid, base: base, tabStop: true}, base)
}

// Pressed sets the controlled pressed state (U:toggle.rs:75-78).
func (t *Toggle) Pressed(pressed bool) *Toggle {
	t.pressed = pressed
	return t
}

// Disabled sets whether the toggle ignores activation (U:toggle.rs:80-83).
func (t *Toggle) Disabled(disabled bool) *Toggle {
	t.disabled = disabled
	return t
}

// Styles configures application-owned styles for the toggle's semantic
// states (U:toggle.rs:85-89).
func (t *Toggle) Styles(build func(ToggleStyles) ToggleStyles) *Toggle {
	t.semanticStyles = build(t.semanticStyles)
	return t
}

// resolvedStyle layers the instance style, pressed and disabled
// (U:toggle.rs:91-101).
func (t *Toggle) resolvedStyle() gpui.StyleRefinement {
	instance := t.Style()
	var states []*gpui.StyleRefinement
	if t.pressed {
		states = append(states, &t.semanticStyles.pressed)
	}
	if t.disabled {
		states = append(states, &t.semanticStyles.disabled)
	}
	return resolveStyle(&instance, states...)
}

// AccessibilityLabel sets the accessible label (U:toggle.rs:103-106).
func (t *Toggle) AccessibilityLabel(label string) *Toggle {
	t.accessibilityLabel = &label
	return t
}

// OnChange handles a request to change the controlled pressed state
// (U:toggle.rs:108-115).
func (t *Toggle) OnChange(handler ToggleChangeHandler) *Toggle {
	t.onChange = handler
	return t
}

// TabIndex sets the focus traversal index (U:toggle.rs:117-120).
func (t *Toggle) TabIndex(tabIndex int64) *Toggle {
	t.tabIndex = tabIndex
	return t
}

// TabStop sets keyboard traversal participation (U:toggle.rs:122-125).
func (t *Toggle) TabStop(tabStop bool) *Toggle {
	t.tabStop = tabStop
	return t
}

// TrackFocus uses a caller-owned focus handle (U:toggle.rs:127-130).
func (t *Toggle) TrackFocus(focusHandle gpui.FocusHandle) *Toggle {
	t.trackedFocus = &focusHandle
	return t
}

// RenderOnce implements gpui.RenderOnce (U:toggle.rs:160-212).
func (t *Toggle) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return t.render(w, app).IntoElement()
}

// toggledOf maps a bool onto the accesskit tri-state.
func toggledOf(on bool) gpui.Toggled {
	if on {
		return gpui.ToggledTrue
	}
	return gpui.ToggledFalse
}

// render builds the toggle's root div (U:toggle.rs:161-211).
func (t *Toggle) render(w *gpui.Window, app *gpui.App) *gpui.DivElement {
	focusHandle := keyedFocusHandle(t.trackedFocus, t.id, w, app)
	pressed := t.pressed
	disabled := t.disabled
	style := t.resolvedStyle()
	onChange := t.onChange

	d := TestSupport(t.base).
		Role(gpui.RoleButton).
		// Button's neutral control geometry (U:toggle.rs:174-180).
		Flex().
		ItemsCenter().
		JustifyCenter(). // A control, like the UA stylesheet's button rule
		// (user-select: none, html.css:850/:904; browser-core 20's
		// default-on selection opts it back out).
		SelectNone().
		LineHeight(gpui.Fraction(1)).
		AriaToggled(toggledOf(pressed))
	if t.accessibilityLabel != nil {
		d.AriaLabel(*t.accessibilityLabel)
	}
	if !disabled {
		d.TrackFocus(focusHandle.TabIndex(t.tabIndex).TabStop(t.tabStop))
	} else {
		d.OnMouseDown(gpui.MouseButtonLeft, stopPropagation)
	}
	if !disabled && onChange != nil {
		d.OnClick(func(event *gpui.ClickEvent, w *gpui.Window, app *gpui.App) {
			onChange(!pressed, event, w, app)
		})
	}
	d.Extend(t.TakeChildren())
	return StyledExtOf(d).RefineStyle(&style)
}
