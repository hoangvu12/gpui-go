package gpui

// This file is the port's element accessibility layer (ticket21): the
// semantic node model an element publishes, the type-asserted
// accessibility companions elements opt into, the per-window tree
// builder driven from the real element phases, the immutable published
// snapshots, and the generation-checked foreground action routing.
//
// The pinned reference is GPUI-CE 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a:
//
//   - crates/gpui/src/_accessibility.rs — the guide: nodes are reported
//     when they have a GlobalElementId AND a role; node identity is the
//     global id path across frames; aria_hidden hides a subtree while
//     keeping focus and input handlers; accessibility actions are
//     AccessKit actions, unrelated to GPUI's Action trait; synthetic
//     children are contributed after prepaint through A11ySubtreeBuilder;
//   - crates/gpui/src/window/a11y.rs — the A11y/A11yNodeBuilder model:
//     ROOT_NODE_ID = NodeId(0), a per-frame push/pop node stack, seen-id
//     dedup (duplicates are discarded, debug builds assert),
//     synthetic_node_id = hash(parent id, key), set_focusable/set_focus
//     (focus must be registered focusable, one set_focus per frame),
//     action listeners keyed by node id (cleared at frame start,
//     repopulated during paint), finalize -> TreeUpdate with
//     repair_tree_update (drop dangling child refs, focus falls back to
//     root), and the active/forced/force_disabled activation flags;
//   - crates/gpui/src/window.rs — Window::perform_a11y_action /
//     handle_a11y_action: listener dispatch first, then built-in
//     fallbacks (Focus -> window.focus(handle), Blur -> window.blur;
//     the Click fallback synthesizes mouse events at the node bounds
//     center);
//   - crates/gpui/src/elements/div.rs — the element-side surface:
//     a11y_role / is_a11y_hidden / write_a11y_info /
//     a11y_synthetic_children (lines 2261-2282), the a11y action
//     listeners on Interactivity (2627-2629) registered during paint
//     (3086-3093).
//
// Bounded deviations, recorded honestly:
//
//   - This slice publishes Go-side semantic trees only. There is no
//     native AccessKit adapter and no UIA interop (ticket22); the
//     published snapshot is the observable seam the native provider
//     will consume.
//   - The Click fallback that synthesizes MouseDown/MouseUp at the node
//     bounds center is NOT implemented: the port's input dispatch for
//     synthetic mouse events belongs to the input/UIA tickets. Click
//     works through registered listeners, which is how CE's
//     .on_click() wires AccessibleAction::Click anyway.
//   - Node ids are FNV-1a-64 over a canonical encoding of the global
//     element id path (the reference hashes with Rust's DefaultHasher;
//     the exact integers differ by design, the derivation policy —
//     hash of the semantic path, stable across frames, per window —
//     is the ported contract).
//   - set_focus/set_focusable misuse does not panic (the reference
//     panics only in debug builds); it is recorded as a diagnostic on
//     the published snapshot.
//
// FILE OWNERSHIP: this file owns the accessibility runtime. It does not
// modify the element/div/window files; it calls their existing seams
// (RequestElementLayout, the phase machinery, the focus and window
// registries) and type-asserts elements against the companion
// interfaces below, including the placeholder interfaces element.go
// already declared (A11yRole/A11yHidden/A11yProperties).

import (
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// Roles and semantic actions (accesskit::Role / accesskit::Action)
// ---------------------------------------------------------------------------

// Role constants for the semantic node model (the accesskit Role
// discriminants the port publishes; Role itself is element.go's type).
const (
	// RoleUnknown is the zero role: the node is not semantically typed.
	RoleUnknown Role = iota
	// RoleWindow is the window root role (accesskit Role::Window).
	RoleWindow
	// RoleGenericContainer is a plain grouping node (the reference
	// filters GenericContainer out of the a11y tree; the port keeps it
	// visible so container identity is observable).
	RoleGenericContainer
	// RoleButton is an interactive button.
	RoleButton
	// RoleTextInput is an editable text field.
	RoleTextInput
	// RoleTextRun is one run of text inside a text input (the
	// synthetic-child example role of the pinned guide).
	RoleTextRun
	// RoleStaticText is static, non-editable text.
	RoleStaticText
	// RoleCheckBox is a toggleable check box.
	RoleCheckBox
	// RoleSlider is an adjustable slider.
	RoleSlider
)

// String renders the role like the reference Debug impl.
func (r Role) String() string {
	switch r {
	case RoleUnknown:
		return "Unknown"
	case RoleWindow:
		return "Window"
	case RoleGenericContainer:
		return "GenericContainer"
	case RoleButton:
		return "Button"
	case RoleTextInput:
		return "TextInput"
	case RoleTextRun:
		return "TextRun"
	case RoleStaticText:
		return "StaticText"
	case RoleCheckBox:
		return "CheckBox"
	case RoleSlider:
		return "Slider"
	default:
		return fmt.Sprintf("Role(%d)", uint16(r))
	}
}

// A11yAction is an accessibility action requested by assistive
// technology (accesskit::Action; explicitly NOT the GPUI Action trait —
// _accessibility.rs "Handling actions").
type A11yAction uint16

// The semantic actions of the published slice.
const (
	// A11yActionClick activates the node (buttons).
	A11yActionClick A11yAction = iota + 1
	// A11yActionFocus moves keyboard focus to the node.
	A11yActionFocus
	// A11yActionBlur clears window focus.
	A11yActionBlur
	// A11yActionIncrement increases the node's value.
	A11yActionIncrement
	// A11yActionDecrement decreases the node's value.
	A11yActionDecrement
	// A11yActionSetValue assigns the node's value (data.Value).
	A11yActionSetValue
	// A11yActionReplaceSelectedText replaces the current selection.
	A11yActionReplaceSelectedText
	// A11yActionSetTextSelection changes the text selection.
	A11yActionSetTextSelection
	// A11yActionShowMenu opens the node's context menu.
	A11yActionShowMenu
)

// String renders the action like the reference Debug impl.
func (a A11yAction) String() string {
	switch a {
	case A11yActionClick:
		return "Click"
	case A11yActionFocus:
		return "Focus"
	case A11yActionBlur:
		return "Blur"
	case A11yActionIncrement:
		return "Increment"
	case A11yActionDecrement:
		return "Decrement"
	case A11yActionSetValue:
		return "SetValue"
	case A11yActionReplaceSelectedText:
		return "ReplaceSelectedText"
	case A11yActionSetTextSelection:
		return "SetTextSelection"
	case A11yActionShowMenu:
		return "ShowMenu"
	default:
		return fmt.Sprintf("Action(%d)", uint16(a))
	}
}

// A11yActionData is the data payload of an action request (the
// accesskit ActionData::Value string slice of the pinned surface).
type A11yActionData struct {
	// Value is the requested value (SetValue/ReplaceSelectedText).
	Value string
}

// A11yActionListener responds to one semantic action on one node (the
// reference A11yActionListener: FnMut(Option<&ActionData>, &mut Window,
// &mut App)). Listeners run inside a foreground application update.
type A11yActionListener func(data *A11yActionData, w *Window, app *App)

// A11yActionRegistration binds one action to one listener for the
// element's node (the div.rs a11y_action_listeners entries,
// div.rs:2627-2629).
type A11yActionRegistration struct {
	// Action is the semantic action.
	Action A11yAction
	// Listener handles the request.
	Listener A11yActionListener
}

// ---------------------------------------------------------------------------
// Node identity (a11y.rs ROOT_NODE_ID / GlobalElementId node ids)
// ---------------------------------------------------------------------------

// A11yNodeID is the stable identity of one semantic node (accesskit
// NodeId). Zero is the fixed root of every window's tree (a11y.rs
// ROOT_NODE_ID = NodeId(0)).
//
// ID POLICY (determinism and collisions):
//
//   - The root is always 0.
//   - Every other node id is FNV-1a-64 over the CANONICAL encoding of
//     the element's GlobalElementID path: each component is encoded
//     with a kind tag and a separator byte so different id kinds and
//     different path lengths never encode alike (the reference joins
//     ids with "." and hashes with DefaultHasher; the exact integers
//     differ, the derivation is the contract).
//   - Synthetic child ids are FNV-1a-64 over (parent node id, key),
//     exactly like a11y.rs synthetic_node_id, so keys must be unique
//     within one synthetic-children call and may repeat across calls.
//   - Ids are derived from the semantic path only, never from
//     addresses, map iteration or run order: the same path in the same
//     frame shape yields the same id across processes.
//   - Ids are TREE-LOCAL (per window), like the reference where each
//     window owns its adapter tree; action tokens carry the window
//     identity and generation so identical paths in sibling windows
//     never cross-route (see A11yToken).
//   - COLLISIONS: a duplicate id in one frame is discarded from the
//     tree and recorded on the published snapshot (a11y.rs can_push:
//     debug builds assert, release builds silently discard — the port
//     always records, never panics). Duplicate paths are an authoring
//     error per the guide ("GlobalElementIds should be unique
//     per-frame").
type A11yNodeID uint64

// RootA11yNodeID is the fixed root node id.
const RootA11yNodeID A11yNodeID = 0

// String renders the id for traces.
func (id A11yNodeID) String() string { return fmt.Sprintf("#%016x", uint64(id)) }

// canonicalElementID encodes one path component unambiguously.
func canonicalElementID(id ElementID) string {
	switch id.Kind {
	case ElementIDName:
		return "\x01" + id.Name
	case ElementIDView:
		return "\x02" + strconv.FormatUint(uint64(id.Entity), 10)
	default:
		return "\x03" + strconv.FormatUint(id.Integer, 10)
	}
}

// canonicalA11yPath encodes a whole global element id path.
func canonicalA11yPath(global *GlobalElementID) string {
	if global == nil {
		return ""
	}
	parts := make([]string, 0, len(global.Path))
	for _, id := range global.Path {
		parts = append(parts, canonicalElementID(id))
	}
	return strings.Join(parts, "\x00")
}

// A11yNodeIDOf derives the node id of an element path (the port of
// GlobalElementId::accesskit_node_id). A nil path reports false: nodes
// without a GlobalElementId cannot join the tree (a11y.rs docs).
func A11yNodeIDOf(global *GlobalElementID) (A11yNodeID, bool) {
	if global == nil || global.Len() == 0 {
		return 0, false
	}
	h := fnv.New64a()
	h.Write([]byte(canonicalA11yPath(global)))
	return A11yNodeID(h.Sum64()), true
}

// syntheticA11yNodeID derives a synthetic child id from its parent and
// key (a11y.rs A11ySubtreeBuilder::synthetic_node_id).
func syntheticA11yNodeID(parent A11yNodeID, key any) A11yNodeID {
	h := fnv.New64a()
	h.Write([]byte{0xa1, 0x11}) // marker byte pair
	var buf [8]byte
	v := uint64(parent)
	for i := 7; i >= 0; i-- {
		buf[i] = byte(v)
		v >>= 8
	}
	h.Write(buf[:])
	fmt.Fprintf(h, "%T\x00%v", key, key)
	return A11yNodeID(h.Sum64())
}

// ---------------------------------------------------------------------------
// The node spec and the type-asserted element companions
// ---------------------------------------------------------------------------

// A11ySpec is the semantic record one element contributes for its node
// (accesskit::Node's ported subset: role, label, description, value,
// hidden, the author id, and the focus binding).
type A11ySpec struct {
	// Role is the node's semantic role. Zero means untyped.
	Role Role
	// Label is the accessible label (div.rs accessibility_label).
	Label string
	// Description is the accessible description (div.rs
	// accessibility_description).
	Description string
	// Value is the node's value (text inputs and text runs set it).
	Value string
	// Hidden hides this node AND its descendants from assistive
	// technology (aria_hidden; the subtree keeps focus and input
	// handlers, and the node keeps a stable id).
	Hidden bool
	// AuthorID is the author-provided identifier exposed to
	// accessibility clients (div.rs accessibility_id: "keep it stable
	// and unique within its accessibility tree").
	AuthorID string
	// Focus binds the node to a focus handle: the node becomes
	// focusable, and when the handle holds window focus the node is
	// reported as the tree's focus (a11y.rs set_focusable / set_focus,
	// driven from Interactivity::paint, div.rs:2803-2825).
	Focus FocusHandle
	// hasFocus marks a non-nil Focus binding.
	hasFocus bool
}

// WithFocus binds a focus handle to the spec.
func (s A11ySpec) WithFocus(handle FocusHandle) A11ySpec {
	s.Focus = handle
	s.hasFocus = true
	return s
}

// focusBinding returns the bound handle when present.
func (s A11ySpec) focusBinding() (FocusHandle, bool) {
	if !s.hasFocus || s.Focus.id == 0 {
		return FocusHandle{}, false
	}
	return s.Focus, true
}

// A11yCompanion is the primary element opt-in: elements implement it to
// publish a semantic node (the Element::a11y_role / is_a11y_hidden /
// write_a11y_info group of the reference trait, element.rs). The
// companion is TYPE-ASSERTED by the accessibility runtime (see
// Accessibility below); elements that do not implement it are not
// published.
//
// A node joins the tree when the element has a global id path AND the
// spec carries a role, a hidden flag, or a focus binding (the guide:
// nodes are reported when they have an id and a role; hidden subtrees
// need an id but no role).
type A11yCompanion interface {
	// A11ySpec returns the element's semantic node record.
	A11ySpec() A11ySpec
}

// A11ySynthetic is the synthetic-children companion
// (Element::a11y_synthetic_children): it contributes nodes that do not
// correspond to any element, AFTER prepaint so prepaint state is
// available (a11y.rs "Synthetic children").
type A11ySynthetic[P any] interface {
	// A11ySyntheticChildren builds the synthetic children of this
	// element's node using its prepaint state.
	A11ySyntheticChildren(prepaint *P, builder *A11ySubtree)
}

// A11yActions is the action companion (div.rs on_a11y_action): the
// element's action listeners for its node, rebuilt each frame during
// paint with the prepaint state available (a11y.rs: listeners are
// cleared at the start of a frame and re-populated during painting).
type A11yActions[P any] interface {
	// A11yActionRegistrations returns this frame's action listeners.
	A11yActionRegistrations(prepaint *P) []A11yActionRegistration
}

// A11ySubtree is the synthetic-children builder (a11y.rs
// A11ySubtreeBuilder): synthetic ids derive from the parent id plus a
// key, pushed children become leaves of the current node, and the
// parent's own record stays mutable.
type A11ySubtree struct {
	state  *windowA11yState
	parent *a11yPendingNode
}

// SyntheticNodeID derives a synthetic child id (a11y.rs
// synthetic_node_id: hash of the parent id and the key; keys must be
// unique within one call, may repeat across calls).
func (b *A11ySubtree) SyntheticNodeID(key any) A11yNodeID {
	if b == nil {
		return 0
	}
	return syntheticA11yNodeID(b.parent.id, key)
}

// PushChild appends one synthetic child node under this element's node.
// It returns false when a node with this id was already pushed this
// frame; the node is then discarded (a11y.rs push_leaf).
func (b *A11ySubtree) PushChild(id A11yNodeID, spec A11ySpec) bool {
	if b == nil || b.state == nil {
		return false
	}
	// beginNode records the child link on the current stack parent.
	return b.state.beginNode(id, spec, b.parent.bounds, true)
}

// Parent returns the parent node's mutable record (a11y.rs
// parent_node(): the synthetic builder may also mutate the element's
// own node, e.g. to set a text selection).
func (b *A11ySubtree) Parent() *A11ySpec {
	if b == nil {
		return nil
	}
	return &b.parent.spec
}

// ---------------------------------------------------------------------------
// The accessibility wrapper element (drives the tree from real phases)
// ---------------------------------------------------------------------------

// a11yElement wraps one typed element and drives the accessibility tree
// from its real phases: the node is pushed at prepaint (bounds recorded
// from the committed layout), synthetic children run after the child's
// prepaint, and focus/action registrations run at paint (the reference
// builds the tree in Drawable::prepaint and registers listeners in
// Interactivity::paint).
type a11yElement[L, P any] struct {
	child Element[L, P]
}

// Accessibility adapts one typed custom element into an AnyElement that
// publishes its accessibility companion during the element phases. The
// element opts in by implementing A11yCompanion (plus optionally
// A11ySynthetic[P] and A11yActions[P]); without the window's
// accessibility runtime being active the wrapper is a pass-through.
func Accessibility[L, P any](child Element[L, P]) AnyElement {
	return CustomElement[L, P](&a11yElement[L, P]{child: child})
}

// ID implements Element: the wrapper shares the child's identity so the
// global id path (and thus the node id) is the child's own.
func (e *a11yElement[L, P]) ID() (ElementID, bool) { return e.child.ID() }

// SourceLocation implements Element.
func (e *a11yElement[L, P]) SourceLocation() *SourceLocation {
	return e.child.SourceLocation()
}

// RequestLayout implements Element: the child's own request.
func (e *a11yElement[L, P]) RequestLayout(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App) (LayoutID, L) {
	return e.child.RequestLayout(global, inspector, w, app)
}

// Prepaint implements Element: push the node (when the element
// publishes one), run the child's prepaint, contribute synthetic
// children with the prepaint state, then pop.
func (e *a11yElement[L, P]) Prepaint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *L, w *Window, app *App) P {
	state, node := beginElementA11yNode(w, global, e)
	prepaint := e.child.Prepaint(global, inspector, bounds, layout, w, app)
	if node != nil {
		node.bounds = bounds
		if syn, ok := any(e.child).(A11ySynthetic[P]); ok {
			syn.A11ySyntheticChildren(&prepaint, &A11ySubtree{state: state, parent: node})
		}
		state.endNode(node)
	}
	return prepaint
}

// Paint implements Element: run the child's paint, then register this
// frame's focus binding and action listeners on the node (div.rs
// Interactivity::paint, lines 2803-2825 and 3086-3093).
func (e *a11yElement[L, P]) Paint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *L, prepaint *P, w *Window, app *App) {
	e.child.Paint(global, inspector, bounds, layout, prepaint, w, app)
	registerElementA11yActions(w, global, e, prepaint)
}

// elementA11ySpec resolves the child's companion record, honoring both
// the port's companion interface and the placeholder interfaces
// element.go declared (A11yRole / A11yHidden / A11yProperties).
func elementA11ySpec(child any) (A11ySpec, bool) {
	spec := A11ySpec{}
	found := false
	if c, ok := child.(A11yCompanion); ok {
		spec = c.A11ySpec()
		found = true
	}
	if r, ok := child.(A11yRole); ok && !found {
		if role, has := r.A11yRole(); has {
			spec.Role = role
			found = true
		}
	}
	if h, ok := child.(A11yHidden); ok && h.IsA11yHidden() {
		spec.Hidden = true
		found = true
	}
	if p, ok := child.(A11yProperties); ok {
		node := Node{Role: spec.Role, Value: spec.Value, Hidden: spec.Hidden}
		p.WriteA11yInfo(&node)
		if node.Role != spec.Role || node.Value != spec.Value || node.Hidden != spec.Hidden {
			spec.Role, spec.Value, spec.Hidden = node.Role, node.Value, node.Hidden
			found = true
		}
	}
	return spec, found
}

// specPublishes reports whether a spec makes a node join the tree.
func specPublishes(spec A11ySpec, hasCompanion bool) bool {
	if !hasCompanion {
		return false
	}
	return spec.Role != RoleUnknown || spec.Hidden || spec.hasFocus
}

// beginElementA11yNode pushes the element's node for the prepaint phase
// when the window's accessibility build is active. It returns the build
// state and the pushed node (nil when the element publishes no node).
func beginElementA11yNode[L, P any](w *Window, global *GlobalElementID, e *a11yElement[L, P]) (*windowA11yState, *a11yPendingNode) {
	state := activeA11yBuild(w)
	if state == nil || global == nil {
		return nil, nil
	}
	id, ok := A11yNodeIDOf(global)
	if !ok {
		return state, nil
	}
	spec, hasCompanion := elementA11ySpec(e.child)
	if !specPublishes(spec, hasCompanion) {
		return state, nil
	}
	if !state.beginNode(id, spec, Bounds{}, false) {
		// Duplicate id this frame: discarded and recorded; the element
		// still renders (a11y.rs can_push returns false).
		return state, nil
	}
	return state, state.stack[len(state.stack)-1]
}

// registerElementA11yActions records the focus binding and action
// listeners of one publishing element at paint time.
func registerElementA11yActions[L, P any](w *Window, global *GlobalElementID, e *a11yElement[L, P], prepaint *P) {
	state := activeA11yBuild(w)
	if state == nil || global == nil {
		return
	}
	id, ok := A11yNodeIDOf(global)
	if !ok || !state.hasNode(id) {
		return
	}
	spec, hasCompanion := elementA11ySpec(e.child)
	if !specPublishes(spec, hasCompanion) {
		return
	}
	if handle, ok := spec.focusBinding(); ok {
		state.setFocusable(id, handle)
		if handle.IsFocused(w) {
			state.setFocus(id)
		}
	}
	if actions, ok := any(e.child).(A11yActions[P]); ok {
		for _, reg := range actions.A11yActionRegistrations(prepaint) {
			if reg.Listener == nil {
				continue
			}
			state.addListener(id, reg.Action, reg.Listener)
		}
	}
}

// ---------------------------------------------------------------------------
// Per-window accessibility state (a11y.rs A11y, per window)
// ---------------------------------------------------------------------------

// a11yPendingNode is one node under construction (the a11y.rs node
// stack entries).
type a11yPendingNode struct {
	id        A11yNodeID
	spec      A11ySpec
	children  []A11yNodeID
	bounds    Bounds
	synthetic bool
}

// a11yDiagnostic records one soft misuse of the builder API (the
// reference panics on these in debug builds; the port records).
type a11yDiagnostic struct {
	// Node is the node the diagnostic concerns.
	Node A11yNodeID
	// Kind names the misuse.
	Kind string
}

// windowA11yState is one window's accessibility runtime (a11y.rs A11y):
// the per-frame builder plus the published identity model. It is
// touched only on the foreground thread, like the draw state.
type windowA11yState struct {
	app        *App
	windowID   WindowID
	title      string
	active     bool
	generation uint64
	// building marks an in-flight frame build; framePtr identifies the
	// draw the pending build belongs to (an unpublished build from a
	// completed frame is discarded when a new frame starts pushing).
	building  bool
	framePtr  *frameDrawState
	sequence  uint64
	stack     []*a11yPendingNode
	nodes     []*a11yPendingNode
	seen      map[A11yNodeID]bool
	focusable map[A11yNodeID]uint64
	focus     A11yNodeID
	focusSet  bool
	listeners map[A11yNodeID]map[A11yAction][]A11yActionListener
	// nodeGenerations is the live identity model: each published node id
	// carries a generation assigned at insertion and retired at removal;
	// a reinserted id gets a FRESH generation so a stale token can never
	// target the replacement (windows platform contract: "prevent a
	// removed node/window generation from targeting a replacement").
	nodeGenerations    map[A11yNodeID]uint64
	nodeGenerationSeq  uint64
	published          *A11ySnapshot
	pendingCollisions  []A11yNodeID
	pendingDiagnostics []a11yDiagnostic
}

// windowA11yStates is the per-window accessibility registry (foreground
// thread only, like windowDrawStates).
var windowA11yStates = map[*Window]*windowA11yState{}

// windowA11yStateOf returns the window's state when it exists.
func windowA11yStateOf(w *Window) *windowA11yState {
	if w == nil {
		return nil
	}
	return windowA11yStates[w]
}

// ActivateWindowA11y turns the window's accessibility runtime on (the
// forced/automation path of window.rs set_a11y_forced + the pinned
// activation pattern: a minimal valid root is returned immediately, a
// refresh is requested, and the next draw publishes the complete
// update).
func ActivateWindowA11y(w *Window) *A11ySnapshot {
	if w == nil {
		panic("gpui: ActivateWindowA11y requires a live window")
	}
	st := stateFor(w)
	st.active = true
	if st.title == "" {
		st.title = w.title
	}
	// The minimal valid root, immediately (activation pattern).
	minimal := &A11ySnapshot{
		windowID:   st.windowID,
		generation: st.generation,
		sequence:   st.sequence + 1,
		title:      st.title,
		nodes:      []A11ySnapshotNode{rootSnapshotNode(st.title)},
		root:       RootA11yNodeID,
		focus:      RootA11yNodeID,
	}
	st.sequence = minimal.sequence
	st.published = minimal
	// Post a refresh so the complete update follows (the port's draw
	// seam: the corpus redraws and publishes).
	w.requestRefresh()
	return minimal
}

// SetA11yWindowTitle labels the window's root node (a11y.rs
// set_window_title). It may be called before activation: the runtime
// state is created inert and the title survives activation.
func SetA11yWindowTitle(w *Window, title string) {
	if w == nil {
		return
	}
	stateFor(w).title = title
}

// stateFor returns (creating when absent) the window's accessibility
// state. A created state is inert: nothing builds until activation.
func stateFor(w *Window) *windowA11yState {
	st, ok := windowA11yStates[w]
	if !ok {
		st = &windowA11yState{
			app:             w.app,
			windowID:        w.id,
			generation:      1,
			focus:           RootA11yNodeID,
			focusable:       make(map[A11yNodeID]uint64),
			listeners:       make(map[A11yNodeID]map[A11yAction][]A11yActionListener),
			nodeGenerations: make(map[A11yNodeID]uint64),
		}
		windowA11yStates[w] = st
	}
	return st
}

// CloseWindowA11y retires the window's accessibility runtime: the
// generation is bumped so every outstanding token goes stale, listeners
// and the published snapshot are dropped, and later dispatch attempts
// are rejected without touching application state. The native teardown
// (adapter release, outstanding COM references) is ticket22.
func CloseWindowA11y(w *Window) {
	if w == nil {
		return
	}
	if st, ok := windowA11yStates[w]; ok {
		st.generation++
		st.active = false
		st.listeners = nil
		st.published = nil
		st.nodeGenerations = nil
		delete(windowA11yStates, w)
	}
}

// activeA11yBuild returns the window's state when a build is active for
// the current in-flight frame (the a11y.is_active() gate).
func activeA11yBuild(w *Window) *windowA11yState {
	st := windowA11yStateOf(w)
	if st == nil || !st.active {
		return nil
	}
	ds, ok := windowDrawStates[w]
	if !ok || ds.frame == nil {
		return nil
	}
	if st.building && st.framePtr != ds.frame {
		// A new frame started pushing: the previous frame's unpublished
		// build is discarded (the reference clears per-frame state at
		// begin_frame).
		st.resetBuild()
	}
	if !st.building {
		st.beginBuild(ds.frame)
	}
	return st
}

// beginBuild starts one frame's build (a11y.rs begin_frame): clear
// per-frame state and push the root node.
func (st *windowA11yState) beginBuild(frame *frameDrawState) {
	st.building = true
	st.framePtr = frame
	st.stack = nil
	st.nodes = nil
	st.seen = make(map[A11yNodeID]bool)
	st.focusable = make(map[A11yNodeID]uint64)
	st.focus = RootA11yNodeID
	st.focusSet = false
	st.listeners = make(map[A11yNodeID]map[A11yAction][]A11yActionListener)
	st.pendingCollisions = nil
	st.pendingDiagnostics = nil
	root := &a11yPendingNode{
		id:   RootA11yNodeID,
		spec: A11ySpec{Role: RoleWindow, Label: st.title},
	}
	st.seen[RootA11yNodeID] = true
	st.stack = append(st.stack, root)
}

// resetBuild drops an abandoned build.
func (st *windowA11yState) resetBuild() {
	st.building = false
	st.framePtr = nil
	st.stack = nil
	st.nodes = nil
	st.seen = nil
	st.pendingCollisions = nil
	st.pendingDiagnostics = nil
}

// beginNode pushes one node onto the build (a11y.rs push/push_leaf).
func (st *windowA11yState) beginNode(id A11yNodeID, spec A11ySpec, bounds Bounds, synthetic bool) bool {
	if !st.building || len(st.stack) == 0 {
		return false
	}
	if st.seen[id] {
		// Duplicate id this frame: discarded and recorded (a11y.rs
		// can_push: debug asserts, release drops silently).
		st.pendingCollisions = append(st.pendingCollisions, id)
		return false
	}
	st.seen[id] = true
	node := &a11yPendingNode{id: id, spec: spec, bounds: bounds, synthetic: synthetic}
	parent := st.stack[len(st.stack)-1]
	parent.children = append(parent.children, id)
	if synthetic {
		// Synthetic children are leaves (a11y.rs push_leaf).
		st.nodes = append(st.nodes, node)
		return true
	}
	st.stack = append(st.stack, node)
	return true
}

// endNode pops one element node off the build stack (a11y.rs pop).
func (st *windowA11yState) endNode(node *a11yPendingNode) {
	if len(st.stack) < 2 {
		// The root is never popped by an element.
		st.pendingDiagnostics = append(st.pendingDiagnostics, a11yDiagnostic{Node: node.id, Kind: "stack-underflow"})
		return
	}
	top := st.stack[len(st.stack)-1]
	if top != node {
		st.pendingDiagnostics = append(st.pendingDiagnostics, a11yDiagnostic{Node: node.id, Kind: "stack-imbalance"})
		return
	}
	st.stack = st.stack[:len(st.stack)-1]
	st.nodes = append(st.nodes, node)
}

// hasNode reports whether a node was pushed this frame (a11y.rs
// has_node; the root is always present).
func (st *windowA11yState) hasNode(id A11yNodeID) bool {
	return id == RootA11yNodeID || (st.seen != nil && st.seen[id])
}

// setFocusable binds a node to a focus handle (a11y.rs set_focusable).
func (st *windowA11yState) setFocusable(id A11yNodeID, handle FocusHandle) {
	st.focusable[id] = handle.id
}

// setFocus reports the focused node (a11y.rs set_focus: must be
// registered focusable, once per frame; the port records the misuse
// instead of panicking and keeps last-wins, the reference release
// behavior).
func (st *windowA11yState) setFocus(id A11yNodeID) {
	if _, ok := st.focusable[id]; !ok {
		st.pendingDiagnostics = append(st.pendingDiagnostics, a11yDiagnostic{Node: id, Kind: "focus-not-focusable"})
	}
	if st.focusSet {
		st.pendingDiagnostics = append(st.pendingDiagnostics, a11yDiagnostic{Node: id, Kind: "focus-set-twice"})
	}
	st.focus = id
	st.focusSet = true
}

// addListener registers one action listener for a node (a11y.rs
// action_listeners, populated during paint).
func (st *windowA11yState) addListener(id A11yNodeID, action A11yAction, listener A11yActionListener) {
	byAction, ok := st.listeners[id]
	if !ok {
		byAction = make(map[A11yAction][]A11yActionListener)
		st.listeners[id] = byAction
	}
	byAction[action] = append(byAction[action], listener)
}

// ---------------------------------------------------------------------------
// Published snapshots (immutable, independent of mutable app state)
// ---------------------------------------------------------------------------

// A11ySnapshotNode is one published semantic node. It is a value copy:
// after publication it never aliases element or entity state.
type A11ySnapshotNode struct {
	// ID is the node's stable identity.
	ID A11yNodeID
	// Role is the semantic role.
	Role Role
	// Label, Description, Value and AuthorID are the semantic
	// properties.
	Label       string
	Description string
	Value       string
	AuthorID    string
	// Hidden hides this node and its subtree from assistive technology.
	Hidden bool
	// Children are the child node ids in tree order.
	Children []A11yNodeID
	// Bounds is the node's window-local logical bounds (a11y.rs
	// node_bounds).
	Bounds Bounds
	// FocusHandleID is the bound focus handle identity (0: none).
	FocusHandleID uint64
	// Synthetic marks nodes that do not correspond to any element.
	Synthetic bool
	// Actions are the semantic actions listeners answered this frame.
	Actions []A11yAction
}

// String renders the node for traces.
func (n A11ySnapshotNode) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s", n.ID, n.Role)
	if n.AuthorID != "" {
		fmt.Fprintf(&b, " id=%q", n.AuthorID)
	}
	if n.Label != "" {
		fmt.Fprintf(&b, " label=%q", n.Label)
	}
	if n.Description != "" {
		fmt.Fprintf(&b, " description=%q", n.Description)
	}
	if n.Value != "" {
		fmt.Fprintf(&b, " value=%q", n.Value)
	}
	if n.Hidden {
		b.WriteString(" hidden")
	}
	if n.Synthetic {
		b.WriteString(" synthetic")
	}
	if n.FocusHandleID != 0 {
		fmt.Fprintf(&b, " focus-handle=%d", n.FocusHandleID)
	}
	if len(n.Actions) > 0 {
		names := make([]string, 0, len(n.Actions))
		for _, a := range n.Actions {
			names = append(names, a.String())
		}
		fmt.Fprintf(&b, " actions=[%s]", strings.Join(names, ","))
	}
	fmt.Fprintf(&b, " bounds=%s", n.Bounds.String())
	return b.String()
}

// A11ySnapshot is one immutable published accessibility tree: the
// complete observable state of a window's accessibility at one commit.
// It is built by copying every value out of the frame build; it never
// references elements, entities or the per-frame builder, so it stays
// valid and unchanged while the app mutates (the "immutable published
// snapshot independent of mutable app state" of the ticket).
type A11ySnapshot struct {
	windowID   WindowID
	generation uint64
	sequence   uint64
	title      string
	nodes      []A11ySnapshotNode
	root       A11yNodeID
	focus      A11yNodeID
	collisions []A11yNodeID
	// diagnostics records builder misuses of the frame.
	diagnostics []string
}

// rootSnapshotNode builds the window root record.
func rootSnapshotNode(title string) A11ySnapshotNode {
	return A11ySnapshotNode{ID: RootA11yNodeID, Role: RoleWindow, Label: title}
}

// Node returns the published node with the given id.
func (s *A11ySnapshot) Node(id A11yNodeID) (A11ySnapshotNode, bool) {
	if s == nil {
		return A11ySnapshotNode{}, false
	}
	for _, n := range s.nodes {
		if n.ID == id {
			return n, true
		}
	}
	return A11ySnapshotNode{}, false
}

// Nodes returns a copy of the published nodes in finalize order.
func (s *A11ySnapshot) Nodes() []A11ySnapshotNode {
	if s == nil {
		return nil
	}
	out := make([]A11ySnapshotNode, len(s.nodes))
	copy(out, s.nodes)
	return out
}

// NodeCount returns the published node count.
func (s *A11ySnapshot) NodeCount() int {
	if s == nil {
		return 0
	}
	return len(s.nodes)
}

// Root returns the root node id (always 0).
func (s *A11ySnapshot) Root() A11yNodeID {
	if s == nil {
		return RootA11yNodeID
	}
	return s.root
}

// Focus returns the reported focused node (the root when nothing
// focused).
func (s *A11ySnapshot) Focus() A11yNodeID {
	if s == nil {
		return RootA11yNodeID
	}
	return s.focus
}

// WindowID returns the window the snapshot belongs to.
func (s *A11ySnapshot) WindowID() WindowID {
	if s == nil {
		return 0
	}
	return s.windowID
}

// Generation returns the window accessibility generation the snapshot
// was published under.
func (s *A11ySnapshot) Generation() uint64 {
	if s == nil {
		return 0
	}
	return s.generation
}

// Sequence returns the publication sequence number (activation is 1;
// every publish increments).
func (s *A11ySnapshot) Sequence() uint64 {
	if s == nil {
		return 0
	}
	return s.sequence
}

// Collisions returns the duplicate node ids discarded this frame (the
// collision policy's observable).
func (s *A11ySnapshot) Collisions() []A11yNodeID {
	if s == nil {
		return nil
	}
	out := make([]A11yNodeID, len(s.collisions))
	copy(out, s.collisions)
	return out
}

// Diagnostics returns the builder misuses recorded this frame.
func (s *A11ySnapshot) Diagnostics() []string {
	if s == nil {
		return nil
	}
	out := make([]string, len(s.diagnostics))
	copy(out, s.diagnostics)
	return out
}

// Token mints a generation-checked action token for one node of this
// snapshot. The minted token carries the window identity/generation and
// the node; it is UNRESOLVED (its node generation is pinned by
// ResolveA11yToken against the live identity model, typically right
// after minting): dispatch rejects unresolved and re-targeted tokens
// (a removed-then-reinserted node is a replacement the old token may
// not address). A node without listeners still mints: the built-in
// Focus/Blur fallbacks answer for it.
func (s *A11ySnapshot) Token(node A11yNodeID) (A11yToken, error) {
	if s == nil {
		return A11yToken{}, fmt.Errorf("gpui: no published snapshot")
	}
	if _, ok := s.Node(node); !ok {
		return A11yToken{}, fmt.Errorf("gpui: node %s is not in snapshot %d", node, s.sequence)
	}
	return A11yToken{
		windowID:  s.windowID,
		windowGen: s.generation,
		node:      node,
		snapshot:  s.sequence,
	}, nil
}

// Trace renders the deterministic tree trace used by fixture
// comparison: pre-order from the root, focus, collisions and
// diagnostics. Node ids are stable across runs (path-derived hashing).
func (s *A11ySnapshot) Trace() string {
	if s == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "window %q gen=%d seq=%d nodes=%d focus=%s\n", s.title, s.generation, s.sequence, len(s.nodes), s.focus)
	byID := make(map[A11yNodeID]*A11ySnapshotNode, len(s.nodes))
	for i := range s.nodes {
		byID[s.nodes[i].ID] = &s.nodes[i]
	}
	var walk func(id A11yNodeID, depth int)
	walk = func(id A11yNodeID, depth int) {
		node, ok := byID[id]
		if !ok {
			fmt.Fprintf(&b, "%s%s <missing>\n", strings.Repeat("  ", depth), id)
			return
		}
		fmt.Fprintf(&b, "%s%s\n", strings.Repeat("  ", depth), node)
		for _, child := range node.Children {
			walk(child, depth+1)
		}
	}
	walk(s.root, 0)
	if len(s.collisions) > 0 {
		ids := make([]string, 0, len(s.collisions))
		for _, c := range s.collisions {
			ids = append(ids, c.String())
		}
		fmt.Fprintf(&b, "collisions: %s\n", strings.Join(ids, ","))
	}
	for _, d := range s.diagnostics {
		fmt.Fprintf(&b, "diagnostic: %s\n", d)
	}
	return b.String()
}

// PublishWindowA11y finalizes the window's in-flight accessibility
// build into an immutable published snapshot (a11y.rs end_frame +
// finalize + repair_tree_update) and returns it. It must run outside a
// frame draw; publishing with no pending build returns the last
// published snapshot.
func PublishWindowA11y(w *Window) (*A11ySnapshot, error) {
	st := windowA11yStateOf(w)
	if st == nil {
		return nil, fmt.Errorf("gpui: PublishWindowA11y: the window's accessibility runtime is not active")
	}
	if ds, ok := windowDrawStates[w]; ok && ds.frame != nil {
		return nil, fmt.Errorf("gpui: PublishWindowA11y: a frame is being drawn; publish after the draw completes")
	}
	if !st.building {
		if st.published == nil {
			return nil, fmt.Errorf("gpui: PublishWindowA11y: no accessibility build to publish")
		}
		return st.published, nil
	}
	// Stack balance: only the root remains (a11y.rs finalize).
	if len(st.stack) != 1 {
		st.pendingDiagnostics = append(st.pendingDiagnostics, a11yDiagnostic{
			Node: RootA11yNodeID,
			Kind: fmt.Sprintf("stack-imbalance-at-finalize depth=%d", len(st.stack)),
		})
		for len(st.stack) > 1 {
			top := st.stack[len(st.stack)-1]
			st.stack = st.stack[:len(st.stack)-1]
			st.nodes = append(st.nodes, top)
		}
	}
	root := st.stack[0]
	st.nodes = append(st.nodes, root)
	st.stack = nil
	st.building = false
	st.framePtr = nil

	// Copy into the immutable snapshot, repairing as we go (a11y.rs
	// repair_tree_update): focus must exist (else root), child refs must
	// exist (else stripped).
	publishedIDs := make(map[A11yNodeID]A11ySnapshotNode, len(st.nodes))
	for _, n := range st.nodes {
		spec := n.spec
		snapshotNode := A11ySnapshotNode{
			ID:            n.id,
			Role:          spec.Role,
			Label:         spec.Label,
			Description:   spec.Description,
			Value:         spec.Value,
			AuthorID:      spec.AuthorID,
			Hidden:        spec.Hidden,
			Children:      append([]A11yNodeID{}, n.children...),
			Bounds:        n.bounds,
			Synthetic:     n.synthetic,
			FocusHandleID: st.focusable[n.id],
		}
		if byAction := st.listeners[n.id]; len(byAction) > 0 {
			actions := make([]A11yAction, 0, len(byAction))
			for action := range byAction {
				actions = append(actions, action)
			}
			sortA11yActions(actions)
			snapshotNode.Actions = actions
		}
		publishedIDs[n.id] = snapshotNode
	}
	focus := st.focus
	if _, ok := publishedIDs[focus]; !ok {
		focus = RootA11yNodeID
	}
	for id, node := range publishedIDs {
		valid := node.Children[:0:0]
		for _, child := range node.Children {
			if _, ok := publishedIDs[child]; ok {
				valid = append(valid, child)
			}
		}
		node.Children = valid
		publishedIDs[id] = node
	}
	// Finalize order: the build's pop order (deterministic; children
	// lists define the tree).
	nodes := make([]A11ySnapshotNode, 0, len(publishedIDs))
	for _, n := range st.nodes {
		nodes = append(nodes, publishedIDs[n.id])
	}

	// Node identity model: retire absent ids, assign fresh generations
	// to inserted ids (a removed node's tokens can never target its
	// replacement; a reinserted id is a NEW identity for tokens).
	for id := range st.nodeGenerations {
		if _, ok := publishedIDs[id]; !ok {
			delete(st.nodeGenerations, id)
		}
	}
	for id := range publishedIDs {
		if _, ok := st.nodeGenerations[id]; !ok {
			st.nodeGenerationSeq++
			st.nodeGenerations[id] = st.nodeGenerationSeq
		}
	}

	diagnostics := make([]string, 0, len(st.pendingDiagnostics))
	for _, d := range st.pendingDiagnostics {
		diagnostics = append(diagnostics, fmt.Sprintf("%s %s", d.Node, d.Kind))
	}
	snapshot := &A11ySnapshot{
		windowID:    st.windowID,
		generation:  st.generation,
		sequence:    st.sequence + 1,
		title:       st.title,
		nodes:       nodes,
		root:        RootA11yNodeID,
		focus:       focus,
		collisions:  append([]A11yNodeID{}, st.pendingCollisions...),
		diagnostics: diagnostics,
	}
	st.sequence = snapshot.sequence
	st.published = snapshot
	st.pendingCollisions = nil
	st.pendingDiagnostics = nil
	return snapshot, nil
}

// LastA11ySnapshot returns the window's last published snapshot.
func LastA11ySnapshot(w *Window) (*A11ySnapshot, bool) {
	st := windowA11yStateOf(w)
	if st == nil || st.published == nil {
		return nil, false
	}
	return st.published, true
}

// sortA11yActions sorts action enums ascending.
func sortA11yActions(actions []A11yAction) {
	for i := 1; i < len(actions); i++ {
		for j := i; j > 0 && actions[j-1] > actions[j]; j-- {
			actions[j-1], actions[j] = actions[j], actions[j-1]
		}
	}
}

// ---------------------------------------------------------------------------
// Generation-checked foreground action routing
// ---------------------------------------------------------------------------

// A11yToken is a generation-checked action token (the windows platform
// contract: "Accessibility actions enqueue copied requests with
// window/node generations for foreground validation"). A token is a
// plain copied value: it keeps no pointers into window, element or
// entity state, so dispatching a stale token can never revive or
// dereference released app state.
//
// Validation at dispatch: the window must still be open; the window
// accessibility generation must match (CloseWindowA11y / teardown bumps
// it); the node must still exist in the CURRENT published snapshot; and
// the node's live identity generation must match the one recorded when
// the token was resolved — a node that was removed and reinserted is a
// replacement the old token may not target.
type A11yToken struct {
	windowID  WindowID
	windowGen uint64
	node      A11yNodeID
	nodeGen   uint64
	snapshot  uint64
}

// Node returns the token's target node.
func (t A11yToken) Node() A11yNodeID { return t.node }

// ResolveA11yToken binds a minted token to the live node generation of
// the window's CURRENT published snapshot (the token records the
// generation it was resolved under; minting via A11ySnapshot.Token
// returns an unresolved token that dispatch resolves against the live
// identity model — call this to pin it explicitly, typically right
// after minting).
func ResolveA11yToken(app *App, token A11yToken) (A11yToken, error) {
	if app == nil {
		return token, fmt.Errorf("gpui: ResolveA11yToken requires an application")
	}
	w, ok := app.Window(token.windowID)
	if !ok {
		return token, ErrA11yWindowGone
	}
	st := windowA11yStateOf(w)
	if st == nil || st.published == nil {
		return token, ErrA11yWindowGone
	}
	gen, ok := st.nodeGenerations[token.node]
	if !ok {
		return token, ErrA11yNodeRemoved
	}
	if _, ok := st.published.Node(token.node); !ok {
		return token, ErrA11yNodeRemoved
	}
	token.nodeGen = gen
	return token, nil
}

// Typed accessibility action errors.
var (
	// ErrA11yWindowGone reports a token whose window is closed or whose
	// accessibility runtime was retired.
	ErrA11yWindowGone = fmt.Errorf("gpui: the accessibility window is gone")
	// ErrA11yStaleGeneration reports a token whose window or node
	// generation no longer matches the live state.
	ErrA11yStaleGeneration = fmt.Errorf("gpui: the accessibility token's generation is stale")
	// ErrA11yNodeRemoved reports a token whose node no longer exists.
	ErrA11yNodeRemoved = fmt.Errorf("gpui: the accessibility node was removed")
	// ErrA11yNoHandler reports an action with no listener and no
	// built-in fallback.
	ErrA11yNoHandler = fmt.Errorf("gpui: no handler for the accessibility action")
)

// DispatchA11yAction validates one action request against the token and
// delivers it on the FOREGROUND thread inside one application update
// (window.rs handle_a11y_action): listener dispatch first, then the
// built-in fallbacks (Focus -> window.Focus(handle), Blur ->
// window.Blur). Stale requests are rejected before any application
// state is touched: no handler runs, no entity is revived.
func DispatchA11yAction(app *App, token A11yToken, action A11yAction, data *A11yActionData) error {
	if app == nil {
		return fmt.Errorf("gpui: DispatchA11yAction requires an application")
	}
	var result error
	app.Update(func(app *App) {
		result = dispatchA11yActionForeground(app, token, action, data)
	})
	return result
}

// dispatchA11yActionForeground runs inside the update: validation plus
// delivery.
func dispatchA11yActionForeground(app *App, token A11yToken, action A11yAction, data *A11yActionData) error {
	w, ok := app.Window(token.windowID)
	if !ok {
		return ErrA11yWindowGone
	}
	st := windowA11yStateOf(w)
	if st == nil || !st.active || st.published == nil {
		return ErrA11yWindowGone
	}
	if st.generation != token.windowGen {
		return ErrA11yStaleGeneration
	}
	if _, ok := st.published.Node(token.node); !ok {
		return ErrA11yNodeRemoved
	}
	gen, ok := st.nodeGenerations[token.node]
	if !ok {
		return ErrA11yNodeRemoved
	}
	if token.nodeGen == 0 || token.nodeGen != gen {
		// Unresolved tokens are rejected, and a node that was removed and
		// reinserted after the token was resolved is a replacement the
		// old token may not target.
		return ErrA11yStaleGeneration
	}
	if token.snapshot > st.published.sequence {
		return ErrA11yStaleGeneration
	}

	// Listener dispatch first (window.rs handle_a11y_action): the
	// listener slice is COPIED so a handler may re-dispatch or replace
	// registrations while running.
	if byAction := st.listeners[token.node]; len(byAction) > 0 {
		listeners := append([]A11yActionListener(nil), byAction[action]...)
		for _, listener := range listeners {
			listener(data, w, app)
		}
		if len(listeners) > 0 {
			return nil
		}
	}

	// Built-in fallbacks (window.rs handle_a11y_action).
	switch action {
	case A11yActionFocus:
		focusID, ok := st.focusable[token.node]
		if !ok {
			return ErrA11yNoHandler
		}
		handle, ok := w.FocusHandleByID(focusID)
		if !ok {
			return ErrA11yNodeRemoved
		}
		w.Focus(handle)
		return nil
	case A11yActionBlur:
		w.Blur()
		return nil
	default:
		return ErrA11yNoHandler
	}
}
