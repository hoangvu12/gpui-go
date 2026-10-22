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
type FocusableExt[Self any] interface {
	FocusRing(enabled bool) Self
	IsFocusRingEnabled() bool
}

// CollapsibleTrait is component_traits::Collapsible, an element or
// component exposing collapsed state (U:component_traits.rs:52-56). It is
// renamed because the crate root's Collapsible is the collapsible.rs
// component (U:lib.rs:99).
type CollapsibleTrait[Self any] interface {
	Collapsed(collapsed bool) Self
	IsCollapsed() bool
}
