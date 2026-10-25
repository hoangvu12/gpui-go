package base

// An unstyled button (U:button.rs). It is the pattern the other simple
// controls copy:
//
//   - The component embeds gpui.Component[*Button]: its own instance
//     style (Styled, own storage — U:button.rs:187-191), its own child
//     list (Parent — U:button.rs:193-197) and interactivity delegated to
//     the inner div (Interactive — U:button.rs:199-205).
//   - It is a gpui.RenderOnce recipe (U:button.rs:207): render needs the
//     window for the keyed focus handle, so the component does not
//     implement IntoElement; a parent converts it as a recipe child.
//   - render builds the inner div and returns it (*gpui.DivElement), so
//     the ported a11y tests read the rendered div's WriteA11yInfo like
//     upstream's `button.render(window, cx).into_element()
//     .write_a11y_info(&mut node)` (U:button.rs:515-518).
//   - Builders that share a name with a promoted Interactive builder
//     (OnClick, TrackFocus, TabIndex) are the component's own, like the
//     upstream inherent methods that shadow the trait methods.
//
// Rust `Option<T>` builder state is a pointer; `impl Into<ElementId>` is
// gpui.IntoElementID; `Rc<dyn Fn(...)>` handlers are func values.
//
// test_support: upstream wraps the base in ObservedElement
// (U:button.rs:22, 41); the port's TestSupport is the identity
// (base/observe.go), so the base is the plain stateful div.

import "github.com/hoangvu12/gpui-go/gpui"

// ClickHandler is a click activation handler (U:button.rs:13).
type ClickHandler = func(event *gpui.ClickEvent, w *gpui.Window, app *gpui.App)

// Button is an unstyled button that owns interaction, focus, keyboard
// and accessibility behavior (U:button.rs:15-35). Layout and visual
// states are supplied by the application through the Styled API.
type Button struct {
	gpui.Component[*Button]

	id                  gpui.ElementID
	base                *gpui.DivElement
	semanticStyles      ButtonStyles
	selected            bool
	disabled            bool
	onClick             ClickHandler
	accessibilityLabel  *string
	role                RoleOverride
	providedFocusHandle *gpui.FocusHandle
	tabIndex            int64
	tabStop             bool
	focusable           bool
}

// NewButton creates a button with the given element id
// (U:button.rs:38-56 Button::new).
func NewButton[I gpui.IntoElementID](id I) *Button {
	eid := gpui.ElementIDOf(id)
	base := TestSupport(gpui.Div().Flex().ID(eid))
	return gpui.BindComponent(&Button{
		id:        eid,
		base:      base,
		role:      RoleOverrideImplicit(),
		tabIndex:  0,
		tabStop:   true,
		focusable: true,
	}, base)
}

// Disabled sets whether the button ignores pointer and keyboard
// activation (U:button.rs:58-62).
func (b *Button) Disabled(disabled bool) *Button {
	b.disabled = disabled
	return b
}

// Selected sets the application-controlled selected presentation state
// (U:button.rs:64-73). It is distinct from momentary active, Toggle's
// pressed state and accessibility toggle metadata; selecting a Button
// does not set aria_toggled.
func (b *Button) Selected(selected bool) *Button {
	b.selected = selected
	return b
}

// IsSelected implements Selectable (U:button.rs:154-162).
func (b *Button) IsSelected() bool { return b.selected }

// Styles defines application-owned styles for the button's semantic
// states (U:button.rs:75-79).
func (b *Button) Styles(build func(ButtonStyles) ButtonStyles) *Button {
	b.semanticStyles = build(b.semanticStyles)
	return b
}

// AccessibilityLabel sets the label exposed to accessibility clients
// (U:button.rs:81-85).
func (b *Button) AccessibilityLabel(label string) *Button {
	b.accessibilityLabel = &label
	return b
}

// Role overrides the accessibility role; the default is RoleButton
// (U:button.rs:87-91).
func (b *Button) Role(role RoleOverride) *Button {
	b.role = role
	return b
}

// TrackFocus uses a caller-owned focus handle instead of creating keyed
// state (U:button.rs:93-97).
func (b *Button) TrackFocus(focusHandle gpui.FocusHandle) *Button {
	b.providedFocusHandle = &focusHandle
	return b
}

// OnClick sets the activation handler for pointer, Enter and Space input
// (U:button.rs:99-106).
func (b *Button) OnClick(handler ClickHandler) *Button {
	b.onClick = handler
	return b
}

// TabIndex sets the focus traversal index; the default is 0
// (U:button.rs:108-112).
func (b *Button) TabIndex(tabIndex int64) *Button {
	b.tabIndex = tabIndex
	return b
}

// TabStop sets whether the button participates in keyboard focus
// traversal (U:button.rs:114-118).
func (b *Button) TabStop(tabStop bool) *Button {
	b.tabStop = tabStop
	return b
}

// Focusable sets whether pressing the button moves focus onto it; the
// default is true (U:button.rs:120-130). A non-focusable button also
// gives up Enter and Space activation.
func (b *Button) Focusable(focusable bool) *Button {
	b.focusable = focusable
	return b
}

// keyedFocusHandle returns the caller's handle, else the focus handle
// kept in keyed state under id (U:button.rs:132-139; the same body
// recurs in every focusable control).
func keyedFocusHandle(provided *gpui.FocusHandle, id gpui.ElementID, w *gpui.Window, app *gpui.App) gpui.FocusHandle {
	if provided != nil {
		return *provided
	}
	state := w.UseKeyedState(id, app, func(_ *gpui.Window, cx *gpui.Context[gpui.FocusHandle]) gpui.FocusHandle {
		return cx.App().FocusHandle()
	})
	return state.Read(app, func(h *gpui.FocusHandle, _ *gpui.App) gpui.FocusHandle { return *h })
}

// resolvedStyle layers the instance style, selected and disabled
// (U:button.rs:141-151).
func (b *Button) resolvedStyle() gpui.StyleRefinement {
	instance := b.Style()
	var states []*gpui.StyleRefinement
	if b.selected {
		states = append(states, &b.semanticStyles.selected)
	}
	if b.disabled {
		states = append(states, &b.semanticStyles.disabled)
	}
	return resolveStyle(&instance, states...)
}

// ButtonStyles are the semantic styles supported by Button
// (U:button.rs:164-185). The zero value is Rust's Default.
type ButtonStyles struct {
	selected gpui.StyleRefinement
	disabled gpui.StyleRefinement
}

// Selected refines the root style when the button is selected
// (U:button.rs:172-177).
func (s ButtonStyles) Selected(build func(*StateStyle) *StateStyle) ButtonStyles {
	s.selected = refineState(s.selected, build)
	return s
}

// Disabled refines the root style when the button is disabled
// (U:button.rs:179-184).
func (s ButtonStyles) Disabled(build func(*StateStyle) *StateStyle) ButtonStyles {
	s.disabled = refineState(s.disabled, build)
	return s
}

// refineState refines a copy of current with the built state style
// (the shared `self.x.refine(&build(StateStyle::default())
// .into_refinement())` body of every *Styles builder).
func refineState(current gpui.StyleRefinement, build func(*StateStyle) *StateStyle) gpui.StyleRefinement {
	next := current.Clone()
	built := build(NewStateStyle()).intoRefinement()
	next.Refine(&built)
	return next
}

// RenderOnce implements gpui.RenderOnce (U:button.rs:207-258).
func (b *Button) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return b.render(w, app).IntoElement()
}

// render builds the button's root div (U:button.rs:208-257).
func (b *Button) render(w *gpui.Window, app *gpui.App) *gpui.DivElement {
	focusHandle := keyedFocusHandle(b.providedFocusHandle, b.id, w, app)
	disabled := b.disabled
	style := b.resolvedStyle()
	onClick := b.onClick

	d := b.base.
		// Centering is part of Button's control geometry and the neutral
		// line height makes the text box exactly the font size
		// (U:button.rs:215-228).
		Flex().
		ItemsCenter().
		JustifyCenter().
		// Chrome's UA stylesheet opts buttons out of text selection
		// (user-select: none, html.css:850/:904); browser-core 20 makes
		// text selectable by default, so the control opts back out the
		// same way.
		SelectNone().
		LineHeight(gpui.Fraction(1))
	if role, ok := b.role.Resolve(func() gpui.Role { return gpui.RoleButton }); ok {
		d.Role(role)
	}
	if b.accessibilityLabel != nil {
		d.AriaLabel(*b.accessibilityLabel)
	}
	if !disabled && b.focusable {
		d.TrackFocus(focusHandle.TabIndex(b.tabIndex).TabStop(b.tabStop))
	}
	if disabled {
		d.OnMouseDown(gpui.MouseButtonLeft, stopPropagation)
	}
	if !disabled && onClick != nil {
		d.OnClick(onClick)
	}
	d.Extend(b.TakeChildren())
	return StyledExtOf(d).RefineStyle(&style)
}

// stopPropagation is the disabled controls' mouse-down swallow
// (U:button.rs:242-246 `cx.stop_propagation()`).
func stopPropagation(_ *gpui.MouseDownEvent, _ *gpui.Window, app *gpui.App) {
	app.StopPropagation()
}
