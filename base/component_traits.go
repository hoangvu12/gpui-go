package base

// Component traits (U:component_traits.rs).
//
// Go adaptation: a trait becomes an interface of its required methods;
// its default methods become package functions that call an override
// when the component implements one (see doc.go naming rules).

// Selectable is an element or component exposing controlled selected
// state (U:component_traits.rs:1-35).
//
// selected is the application's own meaning of selection: the current
// view, the active item, the chosen option. open is a separate state a
// popover, menu or dropdown puts on its trigger while it is open. They
// coincide on a plain button, so open falls back to selected by default
// (SelectableOpen); a trigger whose selection means something else
// implements SelectableOpener to keep the two apart.
type Selectable[Self any] interface {
	Selected(selected bool) Self
	IsSelected() bool
}

// SelectableSecondary is the optional secondary_selected override
// (U:component_traits.rs:16-18).
type SelectableSecondary[Self any] interface {
	SecondarySelected(secondary bool) Self
}

// SelectableOpener is the optional open / is_open override
// (U:component_traits.rs:20-34).
type SelectableOpener[Self any] interface {
	Open(open bool) Self
	IsOpen() bool
}

// SelectableSecondarySelected is secondary_selected: the override when
// implemented, otherwise the component unchanged
// (U:component_traits.rs:16-18).
func SelectableSecondarySelected[S Selectable[S]](s S, secondary bool) S {
	if o, ok := any(s).(SelectableSecondary[S]); ok {
		return o.SecondarySelected(secondary)
	}
	return s
}

// SelectableOpen sets the open state a popover, menu or dropdown holds on
// its trigger: the override when implemented, otherwise selected
// (U:component_traits.rs:20-27).
func SelectableOpen[S Selectable[S]](s S, open bool) S {
	if o, ok := any(s).(SelectableOpener[S]); ok {
		return o.Open(open)
	}
	return s.Selected(open)
}

// SelectableIsOpen reports whether the trigger is marked open: the
// override when implemented, otherwise is_selected
// (U:component_traits.rs:29-34).
func SelectableIsOpen[S Selectable[S]](s S) bool {
	if o, ok := any(s).(SelectableOpener[S]); ok {
		return o.IsOpen()
	}
	return s.IsSelected()
}

// Disableable is an element or component that can be disabled
// (U:component_traits.rs:37-41).
type Disableable[Self any] interface {
	Disabled(disabled bool) Self
}

// FocusableExt is a component exposing whether its UI layer should draw a
// focus ring (U:component_traits.rs:43-50). It carries state only:
// focus-ring geometry and presentation belong to the visual layer.
//
// browser-core 48/73: the core now paints the default :focus-visible
// ring on every focused element (Chrome's UA rule, gpui/interact.go's
// auto→focus-ring resolution), and Base paints no ring of its own, so
// an enabled FocusRing means exactly "let the core's ring draw". A
// visual layer that paints its own ring must opt the focusable element
// out with FocusRingOutline (below) — the core's OutlineNone — or the
// two rings double-draw.
type FocusableExt[Self any] interface {
	FocusRing(enabled bool) Self
	IsFocusRingEnabled() bool
}

// FocusRingOutline maps the FocusableExt focus-ring state onto the
// core's outline rule (browser-core 48, per 48's verification note;
// ticket 73): FocusRing(false) pins outline-style none on the styled
// focusable element, so the core's default :focus-visible ring never
// draws on a component that opted its ring off or paints its own; an
// enabled ring leaves the outline at the default (auto — the core
// resolves it to the focus ring on a focused focus-visible element,
// nothing on a click-focused one, the focus origin rule of
// browser-core 12). Call it from the component's visual layer with the
// component's IsFocusRingEnabled():
//
//	FocusRingOutline(gpui.Div().TrackFocus(focus), control.IsFocusRingEnabled())
//
// No Base component implements the trait today (the upstream port's
// FocusRing was never painted), so every Base control shows the core's
// ring; the helper is the seam a future component with its own ring
// uses to keep the two from double-drawing.
func FocusRingOutline[S StyledElement[S]](element S, enabled bool) S {
	if !enabled {
		element.StyleBase().OutlineNone()
	}
	return element
}

// CollapsibleTrait is component_traits::Collapsible, an element or
// component exposing collapsed state (U:component_traits.rs:52-56). It is
// renamed because the crate root's Collapsible is the collapsible.rs
// component (U:lib.rs:99).
type CollapsibleTrait[Self any] interface {
	Collapsed(collapsed bool) Self
	IsCollapsed() bool
}
