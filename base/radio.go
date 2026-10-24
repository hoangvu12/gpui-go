package base

// An unstyled radio control (U:radio.rs). It follows the Button pattern
// (base/button.go).
//
// Display (map gap 2b): the Radio root (U:radio.rs:63)
// is a plain div upstream, display Block under
// crates.io gpui 0.2.2 (the pin defaults to Flex, R:style.rs:1024-1027),
// so each such base is built with Block() before the caller's style
// refines over it: the upstream default layout holds and a caller's
// Flex() still wins.

import "github.com/hoangvu12/gpui-go/gpui"

// RadioChangeHandler handles a requested selection change; it always
// receives true (U:radio.rs:13 ChangeHandler, 124-134).
type RadioChangeHandler = func(checked bool, event *gpui.ClickEvent, w *gpui.Window, app *gpui.App)

// Radio is an unstyled radio control that owns activation, focus and
// accessibility (U:radio.rs:15-36). Selection is controlled through
// Checked; activating an unchecked radio requests true through OnChange.
type Radio struct {
	gpui.Component[*Radio]

	id                  gpui.ElementID
	base                *gpui.DivElement
	semanticStyles      RadioStyles
	checked             bool
	disabled            bool
	onChange            RadioChangeHandler
	accessibilityLabel  *string
	tabIndex            int64
	tabStop             bool
	providedFocusHandle *gpui.FocusHandle
	positionInSet       *int
	sizeOfSet           *int
}

// RadioStyles are the semantic root styles supported by Radio
// (U:radio.rs:38-57).
type RadioStyles struct {
	checked  gpui.StyleRefinement
	disabled gpui.StyleRefinement
}

// Checked refines the root style while checked (U:radio.rs:46-50).
func (s RadioStyles) Checked(build func(*StateStyle) *StateStyle) RadioStyles {
	s.checked = refineState(s.checked, build)
	return s
}

// Disabled refines the root style while disabled (U:radio.rs:52-56).
func (s RadioStyles) Disabled(build func(*StateStyle) *StateStyle) RadioStyles {
	s.disabled = refineState(s.disabled, build)
	return s
}

// NewRadio creates an unchecked radio (U:radio.rs:60-78 Radio::new).
func NewRadio[I gpui.IntoElementID](id I) *Radio {
	eid := gpui.ElementIDOf(id)
	base := gpui.Div().Block().ID(eid)
	return gpui.BindComponent(&Radio{id: eid, base: base, tabStop: true}, base)
}

// ID updates the element identity used when the radio is rendered, so a
// group assigning positional ids after construction keeps them distinct
// (U:radio.rs:80-89). It shadows the promoted Interactive.ID so the
// keyed focus state follows the new id too.
func (r *Radio) ID[I gpui.IntoElementID](id I) *Radio {
	eid := gpui.ElementIDOf(id)
	r.base.ID(eid)
	r.id = eid
	return r
}

// Checked sets the controlled checked state (U:radio.rs:91-94).
func (r *Radio) Checked(checked bool) *Radio {
	r.checked = checked
	return r
}

// Disabled sets whether activation is ignored (U:radio.rs:96-99).
func (r *Radio) Disabled(disabled bool) *Radio {
	r.disabled = disabled
	return r
}

// Styles configures the semantic state styles (U:radio.rs:101-105).
func (r *Radio) Styles(build func(RadioStyles) RadioStyles) *Radio {
	r.semanticStyles = build(r.semanticStyles)
	return r
}

// resolvedStyle layers instance, checked and disabled (U:radio.rs:107-117).
func (r *Radio) resolvedStyle() gpui.StyleRefinement {
	instance := r.Style()
	var states []*gpui.StyleRefinement
	if r.checked {
		states = append(states, &r.semanticStyles.checked)
	}
	if r.disabled {
		states = append(states, &r.semanticStyles.disabled)
	}
	return resolveStyle(&instance, states...)
}

// AccessibilityLabel sets the accessible label (U:radio.rs:119-122).
func (r *Radio) AccessibilityLabel(label string) *Radio {
	r.accessibilityLabel = &label
	return r
}

// OnChange handles a requested selection change; activating an already
// checked radio is a no-op (U:radio.rs:124-134).
func (r *Radio) OnChange(handler RadioChangeHandler) *Radio {
	r.onChange = handler
	return r
}

// TrackFocus uses a caller-owned focus handle (U:radio.rs:136-143).
func (r *Radio) TrackFocus(focusHandle gpui.FocusHandle) *Radio {
	r.providedFocusHandle = &focusHandle
	return r
}

// SetPosition sets the one-based position and the group's size
// (U:radio.rs:145-151).
func (r *Radio) SetPosition(position, size int) *Radio {
	r.positionInSet = &position
	r.sizeOfSet = &size
	return r
}

// TabIndex sets the focus traversal index (U:radio.rs:153-156).
func (r *Radio) TabIndex(tabIndex int64) *Radio {
	r.tabIndex = tabIndex
	return r
}

// TabStop sets keyboard traversal participation (U:radio.rs:158-161).
func (r *Radio) TabStop(tabStop bool) *Radio {
	r.tabStop = tabStop
	return r
}

// RenderOnce implements gpui.RenderOnce (U:radio.rs:193-238).
func (r *Radio) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return r.render(w, app).IntoElement()
}

// render builds the radio's root div (U:radio.rs:194-237).
func (r *Radio) render(w *gpui.Window, app *gpui.App) *gpui.DivElement {
	focusHandle := keyedFocusHandle(r.providedFocusHandle, r.id, w, app)
	disabled := r.disabled
	checked := r.checked
	style := r.resolvedStyle()
	onChange := r.onChange

	d := TestSupport(r.base).
		Role(gpui.RoleRadioButton).
		// A form control, like the UA stylesheet's opt-outs
		// (user-select: none, html.css:850/:904; browser-core 20's
		// default-on selection opts it back out).
		SelectNone().
		AriaToggled(toggledOf(checked)).
		// Both toggled and selected: assistive technologies read one or
		// the other (U:radio.rs:209-212).
		AriaSelected(checked)
	if r.accessibilityLabel != nil {
		d.AriaLabel(*r.accessibilityLabel)
	}
	if r.positionInSet != nil {
		d.AriaPositionInSet(*r.positionInSet)
	}
	if r.sizeOfSet != nil {
		d.AriaSizeOfSet(*r.sizeOfSet)
	}
	if !disabled {
		d.TrackFocus(focusHandle.TabIndex(r.tabIndex).TabStop(r.tabStop))
	}
	if !disabled && !checked && onChange != nil {
		d.OnClick(func(event *gpui.ClickEvent, w *gpui.Window, app *gpui.App) {
			onChange(!checked, event, w, app)
		})
	}
	d.Extend(r.TakeChildren())
	return StyledExtOf(d).RefineStyle(&style)
}
