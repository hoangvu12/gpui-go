package base

// Unstyled tabs (U:tabs.rs): Tab and the Tabs collection root, built on
// the Button pattern (base/button.go).
//
// Naming: the crate-root Tab component keeps its name; root.rs's Tab
// action is the unexported rootTab (base/root.go, doc.go naming rules).
//
// Display: Tabs' base is a plain `div().id(id)` (U:tabs.rs:200), display
// Block under Base's gpui-ce 0.2.2 but Flex under the core pin
// (R:style.rs:1024-1027, map.md gap 2b); NewTabs sets Block before the
// caller's style refines over it. Tab sets flex itself (U:tabs.rs:161),
// so it is unaffected.
//
// Upstream TODO kept (U:tabs.rs:19-21): compound keyboard navigation
// (roving focus, arrows, Home/End, Enter/Space) is not implemented
// upstream either; tabs do not take keyboard focus.

import "github.com/hoangvu12/gpui-go/gpui"

// Tab is an unstyled tab that owns pointer activation and accessibility
// behavior (U:tabs.rs:15-35).
type Tab struct {
	gpui.Component[*Tab]

	id                 gpui.ElementID
	base               *gpui.DivElement
	semanticStyles     TabStyles
	selected           bool
	disabled           bool
	onClick            ClickHandler
	accessibilityLabel *string
	positionInSet      *int
	sizeOfSet          *int
}

// NewTab creates a tab with the given element id (U:tabs.rs:38-52
// Tab::new).
func NewTab[I gpui.IntoElementID](id I) *Tab {
	base := gpui.Div().Flex()
	return gpui.BindComponent(&Tab{id: gpui.ElementIDOf(id), base: base}, base)
}

// ID updates the element identity used when the tab is rendered
// (U:tabs.rs:54-58). It shadows the promoted Interactive.ID, which would
// only re-id the inner div.
func (t *Tab) ID[I gpui.IntoElementID](id I) *Tab {
	t.id = gpui.ElementIDOf(id)
	return t
}

// Selected sets the selected state (U:tabs.rs:60-63).
func (t *Tab) Selected(selected bool) *Tab {
	t.selected = selected
	return t
}

// IsSelected reports the selected state (the Selectable shape).
func (t *Tab) IsSelected() bool { return t.selected }

// Disabled sets whether activation is ignored (U:tabs.rs:65-68).
func (t *Tab) Disabled(disabled bool) *Tab {
	t.disabled = disabled
	return t
}

// AccessibilityLabel sets the accessible label (U:tabs.rs:70-73).
func (t *Tab) AccessibilityLabel(label string) *Tab {
	t.accessibilityLabel = &label
	return t
}

// SetPosition sets this tab's one-based position and the tab list's
// total size, so assistive technology can announce "tab 2 of 5"
// (U:tabs.rs:75-81).
func (t *Tab) SetPosition(position, size int) *Tab {
	t.positionInSet = &position
	t.sizeOfSet = &size
	return t
}

// OnClick sets the activation handler (U:tabs.rs:83-89).
func (t *Tab) OnClick(handler ClickHandler) *Tab {
	t.onClick = handler
	return t
}

// Styles configures the semantic state styles (U:tabs.rs:91-94).
func (t *Tab) Styles(build func(TabStyles) TabStyles) *Tab {
	t.semanticStyles = build(t.semanticStyles)
	return t
}

// resolvedStyle layers the instance style, selected and disabled
// (U:tabs.rs:96-106).
func (t *Tab) resolvedStyle() gpui.StyleRefinement {
	instance := t.Style()
	var states []*gpui.StyleRefinement
	if t.selected {
		states = append(states, &t.semanticStyles.selected)
	}
	if t.disabled {
		states = append(states, &t.semanticStyles.disabled)
	}
	return resolveStyle(&instance, states...)
}

// TabStyles are the semantic styles supported by Tab
// (U:tabs.rs:109-127). The zero value is Rust's Default.
type TabStyles struct {
	selected gpui.StyleRefinement
	disabled gpui.StyleRefinement
}

// Selected refines the root style when the tab is selected
// (U:tabs.rs:116-120).
func (s TabStyles) Selected(build func(*StateStyle) *StateStyle) TabStyles {
	s.selected = refineState(s.selected, build)
	return s
}

// Disabled refines the root style when the tab is disabled
// (U:tabs.rs:122-126).
func (s TabStyles) Disabled(build func(*StateStyle) *StateStyle) TabStyles {
	s.disabled = refineState(s.disabled, build)
	return s
}

// RenderOnce implements gpui.RenderOnce (U:tabs.rs:149-183).
func (t *Tab) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return t.render(w, app).IntoElement()
}

// render builds the tab's root div (U:tabs.rs:150-182).
func (t *Tab) render(*gpui.Window, *gpui.App) *gpui.DivElement {
	style := t.resolvedStyle()
	d := TestSupport(t.base.ID(t.id)).
		Role(gpui.RoleTab).
		// Button's neutral control geometry (U:tabs.rs:158-164).
		Flex().
		ItemsCenter().
		JustifyCenter().
		// A tab is a control, like the UA stylesheet's button rule
		// (user-select: none, html.css:850/:904; browser-core 20's
		// default-on selection opts it back out).
		SelectNone().
		LineHeight(gpui.Fraction(1))
	if t.accessibilityLabel != nil {
		d.AriaLabel(*t.accessibilityLabel)
	}
	d.AriaSelected(t.selected)
	if t.positionInSet != nil {
		d.AriaPositionInSet(*t.positionInSet)
	}
	if t.sizeOfSet != nil {
		d.AriaSizeOfSet(*t.sizeOfSet)
	}
	d.OnMouseDown(gpui.MouseButtonLeft, stopPropagation)
	if !t.disabled && t.onClick != nil {
		d.OnClick(t.onClick)
	}
	d.Extend(t.TakeChildren())
	return StyledExtOf(d).RefineStyle(&style)
}

// Tabs is an unstyled collection root for Tab elements
// (U:tabs.rs:185-195). Selection and activation are expressed by its
// child tabs; the root supplies collection accessibility.
type Tabs struct {
	gpui.Component[*Tabs]

	base *gpui.DivElement
}

// NewTabs creates a tab list with the given element id
// (U:tabs.rs:197-205).
func NewTabs[I gpui.IntoElementID](id I) *Tabs {
	// Block: the 0.2.2 plain-div default (see the file comment).
	base := gpui.Div().ID(id).Block()
	return gpui.BindComponent(&Tabs{base: base}, base)
}

// RenderOnce implements gpui.RenderOnce (U:tabs.rs:227-234).
func (t *Tabs) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return t.render(w, app).IntoElement()
}

// render builds the tab list's root div (U:tabs.rs:228-233).
func (t *Tabs) render(*gpui.Window, *gpui.App) *gpui.DivElement {
	style := t.Style()
	d := t.base.Role(gpui.RoleTabList)
	d.Extend(t.TakeChildren())
	return StyledExtOf(d).RefineStyle(&style)
}
