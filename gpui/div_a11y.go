package gpui

// This file is Base ticket 11: accessibility authoring on divs — the
// StatefulInteractiveElement a11y builders, the element-side readers
// (a11y_role / is_a11y_hidden / write_a11y_info /
// a11y_synthetic_children) and the div's accessibility companion.
//
// The pinned reference is GPUI-CE 254b5dbd (R: =
// reference/ce-source/crates/gpui/src):
//
//   - R:elements/div.rs:1496-1738 — the builders: role (override_role,
//     with a debug_assert against GenericContainer), accessibility_id,
//     aria_label / description / keyshortcuts / hidden /
//     active_descendant, a11y_synthetic_children, aria_selected /
//     expanded / toggled / disabled / numeric_value / numeric_value_step
//     / value / placeholder / min / max / orientation / level /
//     position_in_set / size_of_set / row_index / column_index /
//     row_count / column_count / modal / live / live_atomic, and
//     on_a11y_action;
//   - R:elements/div.rs:2537-2564 AriaProperties and :2627-2632 the
//     Interactivity a11y fields;
//   - R:elements/div.rs:2261-2285 — the Div element hooks (a11y_role
//     filters GenericContainer; is_a11y_hidden; write_a11y_info
//     delegates to Interactivity; a11y_synthetic_children takes the
//     FnOnce);
//   - R:elements/div.rs:4065-4162 — Interactivity::write_a11y_info, the
//     write path (Option fields set only when present, flag fields set
//     AND cleared, Click for click listeners, Focus for a tracked focus
//     handle, every on_a11y_action action);
//   - R:element.rs:106-149 — the Element trait a11y contract (a node
//     needs an id and a role, or is_a11y_hidden; write_a11y_info runs
//     only then, R:element.rs:131) and R:element.rs:424-446 the generic
//     node construction (hidden without a role -> GenericContainer).
//
// Base usage (upstream-base/ce/crates/base/src, c0820693, grep
// `aria_|role(`): .role 57, .aria_label 19, .aria_toggled 5,
// .aria_expanded 5, .on_a11y_action 4, .aria_selected 4,
// .aria_orientation 4, .aria_size_of_set 3, .aria_position_in_set 3,
// .aria_numeric_value 3, .aria_value 2, .aria_min/max_numeric_value 2,
// .aria_description 2, .aria_row_index/.aria_row_count/.aria_level/
// .aria_column_index/.aria_column_count/.aria_numeric_value_step 1,
// .a11y_synthetic_children 1. The full pinned builder range is ported,
// including the builders Base does not call (aria_keyshortcuts,
// aria_hidden, aria_active_descendant, aria_disabled, aria_placeholder,
// aria_modal, aria_live, aria_live_atomic, accessibility_id).
//
// Bounded deviations, recorded honestly:
//
//   - Rust puts these builders on StatefulInteractiveElement, so a role
//     without an id does not compile. The Go DivElement has one type;
//     an id-less div with a role simply publishes no node (the pin's
//     runtime rule, element.rs:110: "inclusion in accessibility tree
//     requires non-None id").
//   - role(GenericContainer) is a debug_assert in the pin; the port
//     stores it and filters it at A11yRole exactly like release builds
//     (div.rs:2264-2266), never panicking.
//   - usize builder arguments (level, set position, table indices) are
//     Go ints; a negative value panics (the usize type invariant).
//   - STORAGE: the div's authoring state is the `a11y *divA11yState`
//     field on DivElement (gpui-perf ticket 06 retired the earlier
//     weak-keyed side table, whose per-div weak.Make lookup cost one
//     allocation per div per frame).
//   - PUBLICATION (ticket 02): the snapshot carries every role and
//     state exactly; the frozen rev-11 ABI publishes the nearest
//     supported role and drops the states (a11yprovider.go
//     a11yPublishedRole).

import (
	"fmt"
)

// divA11yState is the div's accessibility authoring state (the pin's
// Interactivity a11y fields, div.rs:2627-2632, plus AriaProperties,
// div.rs:2537-2564).
type divA11yState struct {
	// role is override_role.
	role Optional[Role]
	// The string properties (Option<SharedString> in AriaProperties).
	authorID     Optional[string]
	label        Optional[string]
	description  Optional[string]
	keyshortcuts Optional[string]
	value        Optional[string]
	placeholder  Optional[string]
	// hidden is aria.hidden (a plain bool in the pin).
	hidden bool
	// The Option-valued states.
	selected         Optional[bool]
	expanded         Optional[bool]
	toggled          Optional[Toggled]
	disabled         Optional[bool]
	numericValue     Optional[float64]
	minNumericValue  Optional[float64]
	maxNumericValue  Optional[float64]
	numericValueStep Optional[float64]
	orientation      Optional[A11yOrientation]
	level            Optional[int]
	positionInSet    Optional[int]
	sizeOfSet        Optional[int]
	rowIndex         Optional[int]
	columnIndex      Optional[int]
	rowCount         Optional[int]
	columnCount      Optional[int]
	modal            Optional[bool]
	live             Optional[A11yLive]
	liveAtomic       Optional[bool]
	// reportActiveDescendant is report_active_descendant_focus.
	reportActiveDescendant bool
	// synthetic is the a11y_synthetic_children FnOnce (taken on use).
	synthetic func(*A11ySubtree)
	// listeners are a11y_action_listeners, in registration order.
	listeners []A11yActionRegistration
}

// a11yStateIfAny returns the div's authoring state, or nil when no a11y
// builder was called.
func (d *DivElement) a11yStateIfAny() *divA11yState { return d.a11y }

// a11yState returns the div's authoring state, creating it on first use.
func (d *DivElement) a11yState() *divA11yState {
	if d.a11y == nil {
		d.a11y = &divA11yState{}
	}
	return d.a11y
}

// hasA11yAuthoring reports whether any accessibility builder was called
// on this div (the div then needs the accessibility companion wrapper:
// see divIntoA11yElement).
func (d *DivElement) hasA11yAuthoring() bool { return d.a11yStateIfAny() != nil }

// a11yUsize enforces the usize invariant of the pinned builders.
func a11yUsize(builder string, v int) int {
	if v < 0 {
		panic(fmt.Sprintf("gpui: DivElement.%s takes a non-negative value (the pinned usize), got %d", builder, v))
	}
	return v
}

// ---------------------------------------------------------------------------
// The builders (R:elements/div.rs:1496-1738)
// ---------------------------------------------------------------------------

// Role sets the accessible role (div.rs:1496-1503 role). GenericContainer
// has no effect: it is filtered out of the a11y tree (div.rs:1497-1500,
// 2264-2266).
func (d *DivElement) Role(role Role) *DivElement {
	d.a11yState().role = Some(role)
	return d
}

// AccessibilityID sets the author-provided identifier exposed to
// accessibility clients (div.rs:1512-1515; UIA AutomationId on Windows).
// Keep it stable and unique within its accessibility tree.
func (d *DivElement) AccessibilityID(id string) *DivElement {
	d.a11yState().authorID = Some(id)
	return d
}

// AriaLabel sets the accessible label (div.rs:1518-1521).
func (d *DivElement) AriaLabel(label string) *DivElement {
	d.a11yState().label = Some(label)
	return d
}

// AriaDescription sets the accessible description, announced after the
// name, role and value (div.rs:1527-1530).
func (d *DivElement) AriaDescription(description string) *DivElement {
	d.a11yState().description = Some(description)
	return d
}

// AriaKeyshortcuts sets the announced keyboard shortcut(s); it does not
// create a keymap (div.rs:1537-1540).
func (d *DivElement) AriaKeyshortcuts(keyshortcuts string) *DivElement {
	d.a11yState().keyshortcuts = Some(keyshortcuts)
	return d
}

// AriaHidden hides this element and its descendants from assistive
// technology without changing rendering, focus or input
// (div.rs:1546-1549).
func (d *DivElement) AriaHidden() *DivElement {
	d.a11yState().hidden = true
	return d
}

// AriaActiveDescendant reports this element as the focused node while
// one of its ancestors holds focus (the aria-activedescendant pattern,
// div.rs:1566-1569). The element needs an id and a role.
func (d *DivElement) AriaActiveDescendant() *DivElement {
	d.a11yState().reportActiveDescendant = true
	return d
}

// A11ySyntheticChildren contributes synthetic accessibility nodes as
// children of this element's node; f runs once, after prepaint
// (div.rs:1580-1586, 2277-2285). The element needs an id and a role or
// AriaHidden.
func (d *DivElement) A11ySyntheticChildren(f func(builder *A11ySubtree)) *DivElement {
	d.a11yState().synthetic = f
	return d
}

// AriaSelected sets the selected state (div.rs:1589-1592).
func (d *DivElement) AriaSelected(selected bool) *DivElement {
	d.a11yState().selected = Some(selected)
	return d
}

// AriaExpanded sets the expanded state (div.rs:1595-1598).
func (d *DivElement) AriaExpanded(expanded bool) *DivElement {
	d.a11yState().expanded = Some(expanded)
	return d
}

// AriaToggled sets the toggled state (div.rs:1601-1604).
func (d *DivElement) AriaToggled(toggled Toggled) *DivElement {
	d.a11yState().toggled = Some(toggled)
	return d
}

// AriaDisabled sets whether the element disallows input (AccessKit's
// Disabled flag; false explicitly clears it, div.rs:1608-1611,
// 4087-4093).
func (d *DivElement) AriaDisabled(disabled bool) *DivElement {
	d.a11yState().disabled = Some(disabled)
	return d
}

// AriaNumericValue sets the numeric value (div.rs:1614-1617).
func (d *DivElement) AriaNumericValue(value float64) *DivElement {
	d.a11yState().numericValue = Some(value)
	return d
}

// AriaNumericValueStep sets the expected numeric step (div.rs:1621-1624).
func (d *DivElement) AriaNumericValueStep(step float64) *DivElement {
	d.a11yState().numericValueStep = Some(step)
	return d
}

// AriaValue sets the string value (div.rs:1628-1631).
func (d *DivElement) AriaValue(value string) *DivElement {
	d.a11yState().value = Some(value)
	return d
}

// AriaPlaceholder sets the placeholder text reported for an empty input
// (div.rs:1635-1638).
func (d *DivElement) AriaPlaceholder(placeholder string) *DivElement {
	d.a11yState().placeholder = Some(placeholder)
	return d
}

// AriaMinNumericValue sets the minimum numeric value (div.rs:1641-1644).
func (d *DivElement) AriaMinNumericValue(value float64) *DivElement {
	d.a11yState().minNumericValue = Some(value)
	return d
}

// AriaMaxNumericValue sets the maximum numeric value (div.rs:1647-1650).
func (d *DivElement) AriaMaxNumericValue(value float64) *DivElement {
	d.a11yState().maxNumericValue = Some(value)
	return d
}

// AriaOrientation sets the orientation (div.rs:1653-1656).
func (d *DivElement) AriaOrientation(orientation A11yOrientation) *DivElement {
	d.a11yState().orientation = Some(orientation)
	return d
}

// AriaLevel sets the heading level (div.rs:1659-1662).
func (d *DivElement) AriaLevel(level int) *DivElement {
	d.a11yState().level = Some(a11yUsize("AriaLevel", level))
	return d
}

// AriaPositionInSet sets the position in set (div.rs:1665-1668).
func (d *DivElement) AriaPositionInSet(position int) *DivElement {
	d.a11yState().positionInSet = Some(a11yUsize("AriaPositionInSet", position))
	return d
}

// AriaSizeOfSet sets the size of set (div.rs:1671-1674).
func (d *DivElement) AriaSizeOfSet(size int) *DivElement {
	d.a11yState().sizeOfSet = Some(a11yUsize("AriaSizeOfSet", size))
	return d
}

// AriaRowIndex sets the row index (div.rs:1677-1680).
func (d *DivElement) AriaRowIndex(index int) *DivElement {
	d.a11yState().rowIndex = Some(a11yUsize("AriaRowIndex", index))
	return d
}

// AriaColumnIndex sets the column index (div.rs:1683-1686).
func (d *DivElement) AriaColumnIndex(index int) *DivElement {
	d.a11yState().columnIndex = Some(a11yUsize("AriaColumnIndex", index))
	return d
}

// AriaRowCount sets the row count (div.rs:1689-1692).
func (d *DivElement) AriaRowCount(count int) *DivElement {
	d.a11yState().rowCount = Some(a11yUsize("AriaRowCount", count))
	return d
}

// AriaColumnCount sets the column count (div.rs:1695-1698).
func (d *DivElement) AriaColumnCount(count int) *DivElement {
	d.a11yState().columnCount = Some(a11yUsize("AriaColumnCount", count))
	return d
}

// AriaModal marks the element (typically a dialog) as explicitly modal
// (AccessKit's Modal flag; false explicitly clears it,
// div.rs:1703-1706, 4136-4142).
func (d *DivElement) AriaModal(modal bool) *DivElement {
	d.a11yState().modal = Some(modal)
	return d
}

// AriaLive sets the live-region politeness (div.rs:1710-1713).
func (d *DivElement) AriaLive(live A11yLive) *DivElement {
	d.a11yState().live = Some(live)
	return d
}

// AriaLiveAtomic sets whether the live region is announced as a whole
// (AccessKit's LiveAtomic flag; false explicitly clears it,
// div.rs:1719-1722, 4146-4152).
func (d *DivElement) AriaLiveAtomic(atomic bool) *DivElement {
	d.a11yState().liveAtomic = Some(atomic)
	return d
}

// OnA11yAction registers a handler for an accessibility action on this
// element, called when assistive technology requests it
// (div.rs:1728-1738). The action is advertised on the node
// (div.rs:4159-4161) and the listener is registered at paint
// (div.rs:3086-3098).
func (d *DivElement) OnA11yAction(action A11yAction, listener A11yActionListener) *DivElement {
	if listener == nil {
		panic("gpui: DivElement.OnA11yAction requires a listener")
	}
	st := d.a11yState()
	st.listeners = append(st.listeners, A11yActionRegistration{Action: action, Listener: listener})
	return d
}

// ---------------------------------------------------------------------------
// The element-side readers (R:elements/div.rs:2261-2285, 4065-4162)
// ---------------------------------------------------------------------------

// A11yRole returns the div's accessible role, if any (div.rs:2261-2267:
// override_role with GenericContainer filtered out — "equivalent to an
// HTML div with no role").
func (d *DivElement) A11yRole() (Role, bool) {
	st := d.a11yStateIfAny()
	if st == nil {
		return RoleUnknown, false
	}
	role, ok := st.role.Get()
	if !ok || role == RoleGenericContainer {
		return RoleUnknown, false
	}
	return role, true
}

// IsA11yHidden reports aria_hidden (div.rs:2269-2271).
func (d *DivElement) IsA11yHidden() bool {
	st := d.a11yStateIfAny()
	return st != nil && st.hidden
}

// WriteA11yInfo writes the div's accessibility properties into a node
// record (Interactivity::write_a11y_info, div.rs:4065-4162): each
// Option property is written only when present; the flag properties
// (disabled, modal, live_atomic) are set on true and CLEARED on false;
// Click is advertised for click listeners, Focus for a tracked focus
// handle, and every on_a11y_action action. Role, hidden and the focus
// binding are not written here (the node constructor and
// element.rs:440-443 own them).
func (d *DivElement) WriteA11yInfo(spec *A11ySpec) {
	if spec == nil {
		return
	}
	if st := d.a11yStateIfAny(); st != nil {
		if v, ok := st.authorID.Get(); ok {
			spec.AuthorID = v
		}
		if v, ok := st.label.Get(); ok {
			spec.Label = v
		}
		if v, ok := st.description.Get(); ok {
			spec.Description = v
		}
		if v, ok := st.keyshortcuts.Get(); ok {
			spec.KeyboardShortcut = v
		}
		mergeOptional(&spec.Selected, st.selected)
		mergeOptional(&spec.Expanded, st.expanded)
		mergeOptional(&spec.Toggled, st.toggled)
		if v, ok := st.disabled.Get(); ok {
			spec.Disabled = v
		}
		mergeOptional(&spec.NumericValue, st.numericValue)
		mergeOptional(&spec.MinNumericValue, st.minNumericValue)
		mergeOptional(&spec.MaxNumericValue, st.maxNumericValue)
		mergeOptional(&spec.NumericValueStep, st.numericValueStep)
		if v, ok := st.value.Get(); ok {
			spec.Value = v
		}
		if v, ok := st.placeholder.Get(); ok {
			spec.Placeholder = v
		}
		mergeOptional(&spec.Orientation, st.orientation)
		mergeOptional(&spec.Level, st.level)
		mergeOptional(&spec.PositionInSet, st.positionInSet)
		mergeOptional(&spec.SizeOfSet, st.sizeOfSet)
		mergeOptional(&spec.RowIndex, st.rowIndex)
		mergeOptional(&spec.ColumnIndex, st.columnIndex)
		mergeOptional(&spec.RowCount, st.rowCount)
		mergeOptional(&spec.ColumnCount, st.columnCount)
		if v, ok := st.modal.Get(); ok {
			spec.Modal = v
		}
		mergeOptional(&spec.Live, st.live)
		if v, ok := st.liveAtomic.Get(); ok {
			spec.LiveAtomic = v
		}
	}
	if len(d.clickListeners) > 0 {
		addA11yAction(spec, A11yActionClick)
	}
	if d.focusHandle != nil {
		addA11yAction(spec, A11yActionFocus)
	}
	if st := d.a11yStateIfAny(); st != nil {
		for _, reg := range st.listeners {
			addA11yAction(spec, reg.Action)
		}
	}
}

// addA11yAction adds one advertised action (accesskit Node::add_action:
// set semantics, so a repeated action is not duplicated).
func addA11yAction(spec *A11ySpec, action A11yAction) {
	for _, existing := range spec.Actions {
		if existing == action {
			return
		}
	}
	spec.Actions = append(spec.Actions, action)
}

// a11yNodeSpec builds the div's node record the way the generic element
// path does (element.rs:424-446): no node unless the div has a role or
// is hidden; a hidden div without a role is a GenericContainer; the
// properties come from write_a11y_info; hidden is set afterwards; the
// tracked focus handle binds the node (div.rs:2800-2809) and the
// active-descendant claim rides along (div.rs:2821-2827).
func (d *DivElement) a11yNodeSpec() A11ySpec {
	role, hasRole := d.A11yRole()
	hidden := d.IsA11yHidden()
	if !hasRole && !hidden {
		return A11ySpec{}
	}
	if !hasRole {
		role = RoleGenericContainer
	}
	spec := A11ySpec{Role: role}
	d.WriteA11yInfo(&spec)
	if hidden {
		spec.Hidden = true
	}
	if d.focusHandle != nil {
		spec = spec.WithFocus(*d.focusHandle)
	}
	if st := d.a11yStateIfAny(); st != nil && st.reportActiveDescendant {
		spec.ActiveDescendant = true
	}
	return spec
}

// ---------------------------------------------------------------------------
// The div's accessibility companion (consumed by a11yElement)
// ---------------------------------------------------------------------------

// A11ySpec implements A11yCompanion: the div's node record. A div
// without a role or aria_hidden returns an empty spec and publishes no
// node (element.rs:106-115).
func (e *divElement) A11ySpec() A11ySpec { return e.builder.a11yNodeSpec() }

// A11yActionRegistrations implements A11yActions: the div's
// on_a11y_action listeners (registered at paint, div.rs:3086-3098).
func (e *divElement) A11yActionRegistrations(_ *divPrepaintState) []A11yActionRegistration {
	st := e.builder.a11yStateIfAny()
	if st == nil || len(st.listeners) == 0 {
		return nil
	}
	return append([]A11yActionRegistration(nil), st.listeners...)
}

// A11ySyntheticChildren implements A11ySynthetic: runs the div's
// a11y_synthetic_children closure once (div.rs:2277-2285 takes the
// FnOnce).
func (e *divElement) A11ySyntheticChildren(_ *divPrepaintState, builder *A11ySubtree) {
	st := e.builder.a11yStateIfAny()
	if st == nil || st.synthetic == nil {
		return
	}
	f := st.synthetic
	st.synthetic = nil
	f(builder)
}

// divIntoA11yElement converts the div into an AnyElement wrapped by the
// accessibility runtime (Accessibility), so its node joins the window's
// tree during the real element phases (the pin does this generically in
// Drawable::prepaint, element.rs:424-531). div.go's IntoElement is to
// route divs with a11y authoring here (`if d.hasA11yAuthoring() {
// return divIntoA11yElement(d) }`, the div.go edit ticket 11 hands to
// the div.go owner); until then callers convert explicitly.
func divIntoA11yElement(d *DivElement) AnyElement {
	return Accessibility[divFrameState, divPrepaintState](&divElement{builder: d})
}
