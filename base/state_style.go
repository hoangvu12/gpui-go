package base

import "github.com/hoangvu12/gpui-go/gpui"

// StateStyle is a semantic-state style builder (U:state_style.rs:3-25).
// Visual modifiers come from the embedded gpui.Styled vocabulary; unlike
// a bare StyleRefinement it also has the FluentBuilder helpers (When,
// WhenSome, WhenNone), which gpui.Styled carries.
//
// Construct it with NewStateStyle (Rust's Default): the embedded helper
// must be bound, and the value used through its pointer.
type StateStyle struct {
	gpui.Styled[*StateStyle]
}

// NewStateStyle returns an empty state style (U:state_style.rs:8
// derive(Default)).
func NewStateStyle() *StateStyle { return gpui.BindStyled(&StateStyle{}) }

// intoRefinement returns the built refinement (U:state_style.rs:13-17).
func (s *StateStyle) intoRefinement() gpui.StyleRefinement { return s.StyleMut().Clone() }

// resolveStyle resolves the final style of a control from its instance
// style and the semantic-state styles currently active
// (U:state_style.rs:27-53). The layering order is fixed for every Base
// control:
//
//  1. the instance style supplied through the Styled builder chain,
//  2. value states such as checked, pressed, selected or focused,
//  3. disabled, which is always resolved last.
//
// Semantic states override the builder chain, matching how GPUI layers
// hover, active and focus_visible on top of an element's base style.
// Callers pass their active states in that order and omit inactive ones.
func resolveStyle(instance *gpui.StyleRefinement, activeStates ...*gpui.StyleRefinement) gpui.StyleRefinement {
	var style gpui.StyleRefinement
	style.Refine(instance)
	for _, state := range activeStates {
		style.Refine(state)
	}
	return style
}
