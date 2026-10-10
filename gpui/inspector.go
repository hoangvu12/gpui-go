package gpui

// This file is the port's inspector slice (ticket27): the capability
// surface that maps the reference's compile-time cfg gates onto an
// explicit, documented switch, the per-window Inspector state (the
// active element, picking mode and the active element's typed
// inspector states), the per-frame long-id bookkeeping the element
// phases feed (instance counters, inspector hitboxes and the
// inspector tree build), picking/selection over the real hit-test
// machinery, and the inspector element-state registry.
//
// The pinned reference is GPUI-CE 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a:
//
//   - crates/gpui/src/inspector.rs — InspectorElementId (the
//     debug-assertions/feature-gated long ids: a shared
//     InspectorElementPath { global_id, source_location } plus a
//     per-frame instance_id disambiguating same-path elements),
//     Inspector (active_element + pick_depth, select/hover/
//     set_active_element_id, with_active_element_state, the per-type
//     state map, render_inspector_states), InspectorElementRegistry
//     and the InspectorRenderer function type;
//   - crates/gpui/src/element.rs:338-420 — Drawable::request_layout
//     builds the inspector id from the element's source location and
//     the current element id stack, and threads
//     Option<&InspectorElementId> through every phase
//     (request_layout/prepaint/paint, element.rs:71-108);
//   - crates/gpui/src/window.rs — build_inspector_element_id (the
//     per-frame instance counters, 7245-7258), insert_inspector_hitbox
//     (7287-7304, only while picking), handle_inspector_mouse_event
//     (7322-7377: hover on move, select on down, pick-depth on scroll
//     with SCROLL_LINES 3.0 / SCROLL_PIXELS_PER_LAYER 36.0),
//     hovered_inspector_hitbox (7379-7393: the hit-test walk skipping
//     pick_depth entries), paint_inspector_hitbox (7306-7320, the
//     rgba(0x61afef4d) highlight), with_inspector_state (7222-7242),
//     toggle_inspector (7201-7209) and is_inspector_picking
//     (7211-7220, UNGATED in the pin), plus draw_roots' inspector
//     integration (the 30rem root shrink 3403-3415, prepaint_inspector
//     / paint_inspector 3429-3481);
//   - crates/gpui/src/app.rs:805-819, 2827-2840 — the app-level
//     inspector renderer and element-state registry
//     (set_inspector_renderer / register_inspector_element);
//   - crates/gpui/src/elements/div.rs:2672-2688, 2785-2794 — the
//     element-side inspector state pattern (DivInspectorState: the
//     base style recorded at request_layout, bounds and content size
//     at prepaint, written only when the element's inspector id is the
//     active one) and div.rs:3100-3111 — insert_inspector_hitbox at
//     the div's own hitbox.
//
// Go adaptations are documented at each site. The two big ones:
//
//   - CAPABILITY SURFACE: Rust separates the INSPECTOR FEATURE
//     (feature = "inspector", a release-build opt-in) from the debug
//     PROFILE (debug_assertions, on in every cargo test build) through
//     #[cfg(any(feature = "inspector", debug_assertions))]. Go has no
//     per-build cfg surface here, so the port maps every diagnostic
//     gate onto ONE explicit, documented switch (DiagnosticCapability
//     bits + SetDiagnosticCapability) with capability queries that are
//     ordinary function calls. The default is the pin's TEST/DEBUG
//     profile (inspector ids on, leak detection on, the profiler
//     overlay off — the profile `cargo test` runs under), and tests
//     flip the switch to the release profile to verify release
//     behavior. No build magic is invented: when a capability is off
//     the matching machinery is inert exactly like the cfg'd-out
//     reference branches (no inspector ids are built, no hitboxes are
//     registered, the overlay never paints or records, and
//     WithInspectorState passes a nil state).
//   - TREE SNAPSHOT: the pin's inspector has no tree data structure —
//     the inspector UI is app-provided (InspectorRenderer) and reads
//     state through with_inspector_state. The Go port's "applicable
//     inspector UI surface" (the ticket's wording) is therefore a
//     per-frame inspector tree build recorded during the REAL element
//     phases (every element with an inspector id, while the window's
//     inspector is toggled on), published as an immutable value
//     snapshot at successful frame completion — the same success-gated
//     publication discipline the port's accessibility slice uses. The
//     snapshot carries only values (ids, paths, source locations,
//     bounds), never element or entity references, so a stale
//     selection resolves to dead/absent and can never revive an
//     entity.

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// ---------------------------------------------------------------------------
// The explicit capability surface (the cfg gate mapping)
// ---------------------------------------------------------------------------

// DiagnosticCapability is one diagnostic family's capability bit. It is
// the Go mapping of one reference cfg expression; the documented
// correspondence (no invented build magic, capability queries stay
// explicit function calls):
//
//	CapInspectorIDs   cfg(any(feature = "inspector", debug_assertions))
//	CapFrameOverlay   cfg(feature = "profiler")
//	CapLeakDetection  cfg(any(test, feature = "leak-detection"))
//
// The reference default (a release build with no features) has all
// three OFF; `cargo test` (a debug build) has CapInspectorIDs and
// CapLeakDetection ON. The port defaults to the test/debug profile
// because a Go binary serves both; SetDiagnosticCapability switches
// profiles explicitly and tests verify the release behavior.
type DiagnosticCapability uint8

const (
	// CapInspectorIDs is the inspector long-id machinery
	// (cfg(any(feature = "inspector", debug_assertions)): the
	// InspectorElementPath ids, the per-frame instance counters, the
	// inspector hitboxes and the per-window Inspector state).
	CapInspectorIDs DiagnosticCapability = 1 << iota
	// CapFrameOverlay is the debug frame overlay
	// (cfg(feature = "profiler"): the window's frame-stat recording
	// and the overlay paint).
	CapFrameOverlay
	// CapLeakDetection is the leak detector
	// (cfg(any(test, feature = "leak-detection"))).
	CapLeakDetection
)

// CapabilitiesTestProfile is the pin's `cargo test` profile: debug
// assertions (inspector ids) plus the test cfg (leak detection), no
// profiler.
const CapabilitiesTestProfile = CapInspectorIDs | CapLeakDetection

// CapabilitiesReleaseProfile is the pin's release profile with no
// features: every diagnostic family off.
const CapabilitiesReleaseProfile DiagnosticCapability = 0

// diagnosticsCapabilities is the active capability set. Foreground
// thread only (tests and app setup flip it before drawing).
var diagnosticsCapabilities = CapabilitiesTestProfile

// DiagnosticCapabilities returns the active capability set.
func DiagnosticCapabilities() DiagnosticCapability { return diagnosticsCapabilities }

// DiagnosticCapabilityEnabled reports whether one capability family is
// enabled (the explicit capability query).
func DiagnosticCapabilityEnabled(c DiagnosticCapability) bool {
	return diagnosticsCapabilities&c == c
}

// SetDiagnosticCapability turns one capability family on or off. It is
// the port's explicit profile switch: the pin changes these through
// cargo features and build profiles, which a single Go binary cannot
// reproduce, so the switch is documented state instead of invented
// build magic. Flip to CapabilitiesReleaseProfile to observe the
// reference's release behavior.
func SetDiagnosticCapability(c DiagnosticCapability, enabled bool) {
	if enabled {
		diagnosticsCapabilities |= c
	} else {
		diagnosticsCapabilities &^= c
	}
}

// SetDiagnosticCapabilities replaces the whole active capability set —
// the profile switch (SetDiagnosticCapabilities(CapabilitiesRelease-
// Profile) selects the reference's release profile; the test/debug
// default is CapabilitiesTestProfile).
func SetDiagnosticCapabilities(capabilities DiagnosticCapability) {
	diagnosticsCapabilities = capabilities
}

// inspectorIDsActive reports whether the long-id machinery is on
// (cfg(any(feature = "inspector", debug_assertions))).
func inspectorIDsActive() bool { return DiagnosticCapabilityEnabled(CapInspectorIDs) }

// ---------------------------------------------------------------------------
// The per-window Inspector state (inspector.rs Inspector)
// ---------------------------------------------------------------------------

// windowInspectorState is one window's inspector runtime (inspector.rs
// Inspector, held by the pin as an Entity<Inspector> on the window;
// the port keeps plain window state so the inspector itself can never
// participate in an entity ownership cycle — the ticket's
// no-strong-cycles requirement). Foreground thread only.
type windowInspectorState struct {
	// windowID is the logical window identity.
	windowID WindowID
	// generation is the identity generation; bumped at teardown so
	// outstanding tokens resolve to a dead inspector, never a revived
	// one (the accessibility slice's generation-check pattern).
	generation uint64
	// active reports whether the inspector is toggled on
	// (window.rs toggle_inspector).
	active bool
	// pickDepth is the picking depth (inspector.rs pick_depth:
	// Some(depth) means picking, None means a selection was made).
	// Inspector::new starts at Some(0.0): toggling the inspector on
	// enters picking mode.
	pickDepth *float32
	// activeElement is the inspected element id (active_element.id).
	activeElement *InspectorElementID
	// activeStates is the active element's per-type state map
	// (inspector.rs InspectedElement.states, TypeIdHashMap<Box<dyn
	// Any>>).
	activeStates map[reflect.Type]any
	// stateFrames records the draw index each state was last written
	// at, so a report can distinguish a live write from a stale one
	// (the port's addition; the pin's UI re-renders every frame and
	// gets this implicitly).
	stateFrames map[reflect.Type]uint64
	// published is the last successfully published inspector tree.
	published *InspectorTreeSnapshot
}

// windowInspectorStates is the per-window inspector registry
// (foreground thread only, like windowDrawStates).
var windowInspectorStates = map[*Window]*windowInspectorState{}

// windowInspectorStateOf returns the window's inspector state when it
// exists and the inspector capability is on.
func windowInspectorStateOf(w *Window) *windowInspectorState {
	if w == nil || !inspectorIDsActive() {
		return nil
	}
	return windowInspectorStates[w]
}

// inspectorStateFor returns (creating when absent) the window's
// inspector state.
func inspectorStateFor(w *Window) *windowInspectorState {
	if w == nil {
		return nil
	}
	st, ok := windowInspectorStates[w]
	if !ok {
		st = &windowInspectorState{
			windowID:     w.id,
			generation:   1,
			activeStates: make(map[reflect.Type]any),
			stateFrames:  make(map[reflect.Type]uint64),
		}
		windowInspectorStates[w] = st
	}
	return st
}

// ToggleInspector toggles this window's inspector mode on or off
// (window.rs toggle_inspector, 7201-7209: None -> a fresh inspector
// (which starts in picking mode, Inspector::new's pick_depth
// Some(0.0)), Some -> None) and requests a redraw.
func (w *Window) ToggleInspector(app *App) {
	if w == nil {
		panic("gpui: ToggleInspector requires a live window")
	}
	if !inspectorIDsActive() {
		// cfg'd-out reference behavior: no inspector state exists and
		// toggling is inert (the method itself is gated in the pin).
		return
	}
	st, ok := windowInspectorStates[w]
	if ok && st.active {
		st.active = false
		// Keep the generation stable (no identity change), drop the
		// published tree and the states so nothing retains them.
		st.published = nil
		st.activeElement = nil
		st.activeStates = make(map[reflect.Type]any)
		st.stateFrames = make(map[reflect.Type]uint64)
		st.pickDepth = nil
	} else {
		if !ok {
			st = inspectorStateFor(w)
		}
		st.active = true
		depth := float32(0.0)
		st.pickDepth = &depth
	}
	w.requestRefresh()
}

// IsInspectorPicking returns whether the window's inspector is
// currently picking (window.rs is_inspector_picking, 7211-7220 — the
// pin compiles this method UNGATED and it answers false without an
// inspector; the port matches that).
func (w *Window) IsInspectorPicking() bool {
	if w == nil {
		return false
	}
	// The capability query mirrors the pin's release build: there the
	// toggle is cfg'd out so no inspector exists and this answers false
	// unconditionally; the port's release-profile simulation makes the
	// same state unreachable.
	if st := windowInspectorStateOf(w); st != nil && st.active {
		return st.pickDepth != nil
	}
	return false
}

// StartInspectorPicking starts element picking mode, allowing
// selection by clicking (inspector.rs Inspector::start_picking).
func (w *Window) StartInspectorPicking() {
	if st := windowInspectorStateOf(w); st != nil && st.active {
		depth := float32(0.0)
		st.pickDepth = &depth
	}
}

// InspectorActiveElement returns the id of the currently hovered or
// selected element (inspector.rs Inspector::active_element_id).
func InspectorActiveElement(w *Window) (InspectorElementID, bool) {
	st := windowInspectorStateOf(w)
	if st == nil || st.activeElement == nil {
		return InspectorElementID{}, false
	}
	return *st.activeElement, true
}

// inspectorSetActiveElement is set_active_element_id (inspector.rs
// 106-118): swap the active element (a fresh InspectedElement with no
// states) when the id changed, refresh, and report the change.
func inspectorSetActiveElement(w *Window, st *windowInspectorState, id InspectorElementID) bool {
	changed := true
	if st.activeElement != nil && st.activeElement.Equal(id) {
		changed = false
	} else {
		active := id
		st.activeElement = &active
		st.activeStates = make(map[reflect.Type]any)
		st.stateFrames = make(map[reflect.Type]uint64)
		w.requestRefresh()
	}
	return changed
}

// InspectorSelect selects the given element as the inspected one
// (inspector.rs Inspector::select: set the active id and LEAVE picking
// mode). It reports whether the selection changed.
func InspectorSelect(w *Window, id InspectorElementID) bool {
	st := windowInspectorStateOf(w)
	if st == nil || !st.active {
		return false
	}
	changed := inspectorSetActiveElement(w, st, id)
	st.pickDepth = nil
	return changed
}

// inspectorHover is Inspector::hover: while picking, update the active
// element to the hovered one and reset the pick depth when it changed.
func inspectorHover(w *Window, st *windowInspectorState, id InspectorElementID) {
	if st.pickDepth == nil {
		return
	}
	if inspectorSetActiveElement(w, st, id) {
		depth := float32(0.0)
		st.pickDepth = &depth
	}
}

// CloseWindowInspector tears down the window's inspector runtime: the
// generation is bumped so any outstanding selection resolves to a dead
// inspector, and the published tree, the active element and its typed
// states are dropped (the accessibility slice's CloseWindowA11y
// pattern). The inactive record stays registered as a generation
// tombstone — a later ToggleInspector on the same window reuses the
// BUMPED generation, so an identity from the torn-down runtime can
// never be addressed by the fresh one. Later queries report dead/
// absent without touching application state; nothing is revived.
func CloseWindowInspector(w *Window) {
	if w == nil {
		return
	}
	if st, ok := windowInspectorStates[w]; ok {
		st.generation++
		st.active = false
		st.pickDepth = nil
		st.activeElement = nil
		st.activeStates = nil
		st.stateFrames = nil
		st.published = nil
	}
}

// dropWindowDiagnostics releases every per-window diagnostic runtime
// when the window's element runtime is dropped (window closure): the
// inspector state (generation bumped) and the debug frame overlay. It
// is called from dropDrawState so CloseTestWindow / window destruction
// retire the diagnostics with the frame state.
func dropWindowDiagnostics(w *Window) {
	CloseWindowInspector(w)
	// The window is gone (unlike the CloseWindowInspector tombstone):
	// remove the record outright.
	delete(windowInspectorStates, w)
	dropWindowOverlay(w)
}

// ---------------------------------------------------------------------------
// The per-frame long-id bookkeeping (window.rs Frame's inspector
// members, element.rs Drawable::request_layout)
// ---------------------------------------------------------------------------

// buildInspectorElementID assigns the instance id of one inspector
// path for the frame under construction (window.rs
// build_inspector_element_id, 7245-7258): a per-frame counter keyed by
// the path VALUE (the pin keys FxHashMap<Rc<InspectorElementPath>, usize>
// by Rc value equality, so same-path elements get 0, 1, 2… within the
// frame). The counters live on the frame (Frame::next_inspector_instance_
// ids, cleared by Frame::clear every frame).
func buildInspectorElementID(w *Window, path *InspectorElementPath) *InspectorElementID {
	frame := currentFrame(w)
	if frame.inspectorInstanceIDs == nil {
		frame.inspectorInstanceIDs = make(map[string]uint64)
	}
	key := path.key()
	instance := frame.inspectorInstanceIDs[key]
	frame.inspectorInstanceIDs[key] = instance + 1
	return &InspectorElementID{path: path, instanceID: instance}
}

// InsertInspectorHitbox registers a hitbox that inspector picking
// can select (window.rs insert_inspector_hitbox, 7287-7304 — a
// PUBLIC window method in the pin, called by interactive elements
// with their inspector id; div.go calls it at the div's own hitbox and
// custom elements call it for theirs): only while the window is
// picking, and only with a non-nil inspector id. The map belongs to
// the frame under construction (Frame::inspector_hitboxes) and swaps
// into the window state at successful frame completion.
func (w *Window) InsertInspectorHitbox(hitbox Hitbox, inspector *InspectorElementID) {
	if w == nil {
		panic("gpui: InsertInspectorHitbox requires a live window")
	}
	if inspector == nil {
		return
	}
	st := windowInspectorStateOf(w)
	if st == nil || !st.active || st.pickDepth == nil {
		return
	}
	frame := currentFrame(w)
	if frame.inspectorHitboxes == nil {
		frame.inspectorHitboxes = make(map[uint64]InspectorElementID)
	}
	frame.inspectorHitboxes[hitbox.id] = *inspector
	// Attach the hitbox to the tree node being recorded for this
	// element (the pin's highlight lookup walks hitboxes; the tree
	// node's association lets the report name the hitbox).
	if build := frame.inspectorTree; build != nil && len(build.stack) > 0 {
		node := build.stack[len(build.stack)-1]
		if node.id.Equal(*inspector) {
			node.hitboxID = hitbox.id
		}
	}
}

// inspectorFinishFrame completes the inspector side of a SUCCESSFUL
// frame draw (window.rs draw's tail: next_frame.finish + the
// rendered/next swap carry Frame's inspector members over): publish
// the frame's tree build as the window's current tree and swap the
// frame's inspector hitbox map into the window state. An abandoned
// build never calls this, preserving the previously published tree
// and hitboxes (the success-gated publication discipline).
func inspectorFinishFrame(w *Window, frame *frameDrawState, drawIndex uint64) {
	st := windowInspectorStateOf(w)
	if st == nil {
		return
	}
	st.published = publishInspectorTree(w, frame, drawIndex)
	if ds, ok := windowDrawStates[w]; ok {
		ds.inspectorHitboxes = frame.inspectorHitboxes
	}
}

// windowInspectorHitboxes returns the last completed frame's
// inspector hitbox map (the pin reads the rendered frame's).
func windowInspectorHitboxes(w *Window) map[uint64]InspectorElementID {
	if ds, ok := windowDrawStates[w]; ok {
		return ds.inspectorHitboxes
	}
	return nil
}

// ---------------------------------------------------------------------------
// The inspector tree build (the port's UI surface; the pin's tree is
// app-rendered)
// ---------------------------------------------------------------------------

// inspectorTreeNode is one in-flight tree node recorded during a
// frame's prepaint phases.
type inspectorTreeNode struct {
	id       InspectorElementID
	global   string // the "."-joined global id path key
	location SourceLocation
	bounds   Bounds
	depth    int
	hitboxID uint64
	children []*inspectorTreeNode
}

// inspectorTreeBuild is a frame's tree build: a stack of open nodes
// (prepaint is parents-first, so children recorded during an element's
// own prepaint nest under it) and the completed roots.
type inspectorTreeBuild struct {
	stack []*inspectorTreeNode
	roots []*inspectorTreeNode
	count int
}

// inspectorTreeRecording reports whether the frame under construction
// should record the inspector tree: the capability is on and the
// window's inspector is toggled on.
func inspectorTreeRecording(w *Window) bool {
	st := windowInspectorStateOf(w)
	return st != nil && st.active
}

// beginInspectorNode pushes one element's tree node around its
// prepaint (the node's bounds are the committed layout bounds, the
// same values div.rs writes into DivInspectorState at prepaint,
// div.rs:2785-2794). It returns nil when the frame is not recording.
func beginInspectorNode(w *Window, inspector *InspectorElementID, bounds Bounds) *inspectorTreeNode {
	if inspector == nil || !inspectorTreeRecording(w) {
		return nil
	}
	frame := currentFrame(w)
	if frame.inspectorTree == nil {
		frame.inspectorTree = &inspectorTreeBuild{}
	}
	build := frame.inspectorTree
	node := &inspectorTreeNode{
		id:       *inspector,
		global:   inspector.path.globalKey,
		location: inspector.path.location,
		bounds:   bounds,
		depth:    len(build.stack),
	}
	if len(build.stack) > 0 {
		parent := build.stack[len(build.stack)-1]
		parent.children = append(parent.children, node)
	} else {
		build.roots = append(build.roots, node)
	}
	build.count++
	build.stack = append(build.stack, node)
	return node
}

// endInspectorNode pops one node off the tree build stack.
func endInspectorNode(w *Window) {
	frame := currentFrame(w)
	if build := frame.inspectorTree; build != nil && len(build.stack) > 0 {
		build.stack = build.stack[:len(build.stack)-1]
	}
}

// publishInspectorTree finalizes the frame's tree build into the
// immutable value snapshot (the accessibility slice's
// success-gated-publication discipline: called only when a frame
// completes, so an abandoned build preserves the previously published
// tree). Returns nil when the frame recorded nothing.
func publishInspectorTree(w *Window, frame *frameDrawState, drawIndex uint64) *InspectorTreeSnapshot {
	build := frame.inspectorTree
	if build == nil {
		return nil
	}
	st := windowInspectorStateOf(w)
	if st == nil {
		return nil
	}
	snapshot := &InspectorTreeSnapshot{
		windowID:   st.windowID,
		generation: st.generation,
		frame:      drawIndex,
		count:      build.count,
	}
	snapshot.roots = copyInspectorTreeNodes(build.roots)
	return snapshot
}

// copyInspectorTreeNodes deep-copies the built nodes into published
// value nodes (only values: ids, paths, locations, bounds — no element
// or entity references, so a published tree can never keep an entity
// alive or revive one).
func copyInspectorTreeNodes(nodes []*inspectorTreeNode) []InspectorTreeNode {
	if len(nodes) == 0 {
		return nil
	}
	out := make([]InspectorTreeNode, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, InspectorTreeNode{
			ID:       node.id,
			Global:   node.global,
			Location: node.location,
			Bounds:   node.bounds,
			Depth:    node.depth,
			HitboxID: node.hitboxID,
			Children: copyInspectorTreeNodes(node.children),
		})
	}
	return out
}

// InspectorTreeNode is one published inspector tree node: a value copy
// with the element's inspector identity, global id path, source
// location and the bounds committed at the frame's prepaint.
type InspectorTreeNode struct {
	// ID is the element's long inspector identity (path + instance).
	ID InspectorElementID
	// Global is the "."-joined global element id path.
	Global string
	// Location is the element's construction source location.
	Location SourceLocation
	// Bounds is the node's window-local logical bounds at the
	// published frame's prepaint.
	Bounds Bounds
	// Depth is the node's tree depth (roots at 0).
	Depth int
	// HitboxID is the node's hitbox identity when its element
	// registered one while picking (0: none).
	HitboxID uint64
	// Children are the child nodes in prepaint order.
	Children []InspectorTreeNode
}

// String renders the node for traces.
func (n InspectorTreeNode) String() string {
	return fmt.Sprintf("%s bounds=%s depth=%d children=%d",
		n.ID.String(), n.Bounds.String(), n.Depth, len(n.Children))
}

// GlobalID returns the node's global element identity as a value
// (the retained-element-state key of the element that recorded this
// node; the path is immutable after construction, so the copy is a
// safe value form).
func (n InspectorTreeNode) GlobalID() *GlobalElementID {
	if n.ID.path == nil || n.ID.path.global == nil {
		return nil
	}
	return &GlobalElementID{Path: append([]ElementID{}, n.ID.path.global.Path...)}
}

// InspectorTreeSnapshot is one immutable published inspector tree: the
// complete observable element identity/bounds state of a window's
// successful frame draw while its inspector was on. It holds only
// values, so it stays valid (and unchanged) across later app mutation
// and window teardown, and a stale selection can never resolve through
// it to a live entity.
type InspectorTreeSnapshot struct {
	windowID   WindowID
	generation uint64
	frame      uint64
	count      int
	roots      []InspectorTreeNode
}

// Roots returns a copy of the root nodes in prepaint order.
func (s *InspectorTreeSnapshot) Roots() []InspectorTreeNode {
	if s == nil {
		return nil
	}
	out := make([]InspectorTreeNode, len(s.roots))
	copyInspectorTreeValues(s.roots, out)
	return out
}

// copyInspectorTreeValues deep-copies published nodes into the flat
// destination (values only).
func copyInspectorTreeValues(src, dst []InspectorTreeNode) {
	for i := range src {
		dst[i] = src[i]
		dst[i].Children = copyInspectorTreeValueChildren(src[i].Children)
	}
}

// copyInspectorTreeValueChildren deep-copies one node's children.
func copyInspectorTreeValueChildren(src []InspectorTreeNode) []InspectorTreeNode {
	if src == nil {
		return nil
	}
	out := make([]InspectorTreeNode, len(src))
	copyInspectorTreeValues(src, out)
	return out
}

// Count returns the published node count.
func (s *InspectorTreeSnapshot) Count() int {
	if s == nil {
		return 0
	}
	return s.count
}

// Frame returns the draw index the tree was published at.
func (s *InspectorTreeSnapshot) Frame() uint64 {
	if s == nil {
		return 0
	}
	return s.frame
}

// Generation returns the window inspector generation the tree was
// published under.
func (s *InspectorTreeSnapshot) Generation() uint64 {
	if s == nil {
		return 0
	}
	return s.generation
}

// Node returns the published node with the given inspector identity.
func (s *InspectorTreeSnapshot) Node(id InspectorElementID) (InspectorTreeNode, bool) {
	if s == nil {
		return InspectorTreeNode{}, false
	}
	var find func(nodes []InspectorTreeNode) (InspectorTreeNode, bool)
	find = func(nodes []InspectorTreeNode) (InspectorTreeNode, bool) {
		for i := range nodes {
			if nodes[i].ID.Equal(id) {
				return nodes[i], true
			}
			if node, ok := find(nodes[i].Children); ok {
				return node, ok
			}
		}
		return InspectorTreeNode{}, false
	}
	return find(s.roots)
}

// Nodes returns every published node flattened in pre-order.
func (s *InspectorTreeSnapshot) Nodes() []InspectorTreeNode {
	if s == nil {
		return nil
	}
	out := make([]InspectorTreeNode, 0, s.count)
	var walk func(nodes []InspectorTreeNode)
	walk = func(nodes []InspectorTreeNode) {
		for i := range nodes {
			out = append(out, nodes[i])
			walk(nodes[i].Children)
		}
	}
	walk(s.roots)
	return out
}

// Trace renders the deterministic tree trace (pre-order from the
// roots), the fixture-comparison observable.
func (s *InspectorTreeSnapshot) Trace() string {
	if s == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "inspector gen=%d frame=%d nodes=%d\n", s.generation, s.frame, s.count)
	var walk func(nodes []InspectorTreeNode, depth int)
	walk = func(nodes []InspectorTreeNode, depth int) {
		for i := range nodes {
			fmt.Fprintf(&b, "%s%s\n", strings.Repeat("  ", depth), nodes[i])
			walk(nodes[i].Children, depth+1)
		}
	}
	walk(s.roots, 0)
	return b.String()
}

// InspectorTreeSnapshotOf returns the window's last published
// inspector tree (nil when the inspector is off or no successful frame
// drew while it was on).
func InspectorTreeSnapshotOf(w *Window) *InspectorTreeSnapshot {
	st := windowInspectorStateOf(w)
	if st == nil {
		return nil
	}
	return st.published
}

// ---------------------------------------------------------------------------
// Selection reports (the current bounds/state observable)
// ---------------------------------------------------------------------------

// InspectorSelection is the report of the window's active inspected
// element: its CURRENT bounds from the last published tree, plus the
// frame each inspector state was last written at. A selection whose id
// is absent from the current tree is DEAD: Present is false and no
// bounds are reported — a removed or reparented element never resolves
// to a revived node (the path/instance identity of the old element
// cannot match a replacement).
type InspectorSelection struct {
	// ID is the active element's inspector identity.
	ID InspectorElementID
	// Global is the active element's global id path.
	Global string
	// Location is the active element's source location.
	Location SourceLocation
	// Present reports whether the id exists in the CURRENT published
	// tree (a live element prepainted with that identity in the last
	// successful inspector frame).
	Present bool
	// Bounds is the current bounds (valid when Present).
	Bounds Bounds
	// Frame is the draw index of the tree the selection resolved
	// against.
	Frame uint64
	// StateFrame is the draw index the active element's inspector
	// states were last written at (0: never written — no live element
	// wrote since the selection).
	StateFrame uint64
	// StateTypes lists the active element's recorded state types in
	// deterministic (sorted) order.
	StateTypes []string
}

// InspectorSelectionOf reports the window's active inspected element
// against the CURRENT published tree. The second return is false when
// no inspector or no selection exists.
func InspectorSelectionOf(w *Window) (InspectorSelection, bool) {
	st := windowInspectorStateOf(w)
	if st == nil || st.activeElement == nil {
		return InspectorSelection{}, false
	}
	report := InspectorSelection{
		ID:       *st.activeElement,
		Global:   st.activeElement.path.globalKey,
		Location: st.activeElement.path.location,
	}
	if node, ok := st.published.Node(*st.activeElement); ok {
		report.Present = true
		report.Bounds = node.Bounds
		report.Frame = st.published.frame
	} else if st.published != nil {
		// A published tree exists and the id is absent: the selection
		// is stale/dead (dead is reported, never a revived node).
		report.Frame = st.published.frame
	}
	for typ := range st.activeStates {
		report.StateTypes = append(report.StateTypes, typ.String())
	}
	sort.Strings(report.StateTypes)
	// The last write frame across every recorded state type.
	var lastStateFrame uint64
	for _, frame := range st.stateFrames {
		if frame > lastStateFrame {
			lastStateFrame = frame
		}
	}
	report.StateFrame = lastStateFrame
	return report, true
}

// ---------------------------------------------------------------------------
// The element-side inspector state seam (window.rs with_inspector_state
// + inspector.rs with_active_element_state)
// ---------------------------------------------------------------------------

// WithInspectorState runs f with the inspector state of type S recorded
// for the window's ACTIVE inspected element (window.rs
// with_inspector_state, 7222-7242, over inspector.rs
// Inspector::with_active_element_state, 132-164: the typed state entry
// is removed from the active element's map, handed to f, and retained
// when f returns a non-nil state).
//
// The element passes its own inspector id; f runs only when that id IS
// the active element (the div.rs DivInspectorState pattern,
// div.rs:2672-2688/2785-2794: an element records its base style at
// request_layout and its bounds/content size at prepaint only while it
// is the inspected one). When the capability is off, or the id is nil
// or not the active element, f runs with a nil state (the reference's
// cfg'd-out branch: `f(&mut None, window)`).
func WithInspectorState[S any, R any](w *Window, inspector *InspectorElementID, f func(state *S, exists bool, w *Window) (R, *S)) R {
	if inspector == nil || !inspectorIDsActive() {
		result, _ := f(nil, false, w)
		return result
	}
	st := windowInspectorStateOf(w)
	if st == nil || !st.active || st.activeElement == nil || !st.activeElement.Equal(*inspector) {
		result, _ := f(nil, false, w)
		return result
	}
	stateType := reflect.TypeFor[S]()
	if st.activeStates == nil {
		st.activeStates = make(map[reflect.Type]any)
	}
	if st.stateFrames == nil {
		st.stateFrames = make(map[reflect.Type]uint64)
	}
	// with_active_element_state removes the entry, runs f, and
	// reinserts only a Some state.
	value, existed := st.activeStates[stateType]
	var previous *S
	if existed {
		if slot, ok := value.(*S); ok {
			previous = slot
		}
	}
	delete(st.activeStates, stateType)
	result, retained := f(previous, existed, w)
	if retained != nil {
		st.activeStates[stateType] = retained
		// The write frame: which draw the live element wrote under (0
		// when no draw has completed yet; outside a draw the stamp is
		// the last completed frame).
		if ds, ok := windowDrawStates[w]; ok {
			if ds.frame != nil {
				st.stateFrames[stateType] = ds.draws + 1
			} else {
				st.stateFrames[stateType] = ds.draws
			}
		}
	} else if existed && previous != nil {
		// f dropped the state (the Option<T> = None arm).
		delete(st.stateFrames, stateType)
	}
	return result
}

// InspectorActiveElementState reads the active element's recorded
// inspector state of type S (the read side of the
// InspectorElementRegistry surface). It returns the state, the draw
// index it was last written at (0: never) and whether it exists.
func InspectorActiveElementState[S any](w *Window) (S, uint64, bool) {
	var zero S
	st := windowInspectorStateOf(w)
	if st == nil || st.activeElement == nil {
		return zero, 0, false
	}
	value, ok := st.activeStates[reflect.TypeFor[S]()]
	if !ok {
		return zero, 0, false
	}
	slot, ok := value.(*S)
	if !ok || slot == nil {
		return zero, 0, false
	}
	frame := st.stateFrames[reflect.TypeFor[S]()]
	return *slot, frame, true
}

// ---------------------------------------------------------------------------
// The inspector element registry and renderer (app.rs 805-819,
// 2827-2840; inspector.rs render_inspector_states)
// ---------------------------------------------------------------------------

// InspectorRenderer is the function set on the App that renders the
// inspector UI (app.rs set_inspector_renderer; the pin's closure
// receives (&mut Inspector, &mut Window, &mut Context<Inspector>) —
// the port's Inspector is window state, not an entity, so the renderer
// receives the window and app and reads the state through the public
// queries; that also keeps the inspector out of entity ownership
// cycles).
type InspectorRenderer func(w *Window, app *App) AnyElement

// inspectorAppRuntime is the app-level inspector runtime: the
// inspector renderer and the per-type element-state renderers
// (app.rs inspector_renderer + inspector_element_registry; the pin
// stores both on App behind cfg gates — the port keeps a registry
// keyed by the app so no App struct edit is needed).
type inspectorAppRuntime struct {
	renderer       InspectorRenderer
	stateRenderers map[reflect.Type]func(id InspectorElementID, state any, w *Window, app *App) AnyElement
}

// inspectorAppRuntimes is the per-app registry.
var inspectorAppRuntimes = map[*App]*inspectorAppRuntime{}

// inspectorRuntimeFor returns (creating when absent) the app's runtime.
func inspectorRuntimeFor(app *App) *inspectorAppRuntime {
	if app == nil {
		return nil
	}
	rt, ok := inspectorAppRuntimes[app]
	if !ok {
		rt = &inspectorAppRuntime{
			stateRenderers: make(map[reflect.Type]func(id InspectorElementID, state any, w *Window, app *App) AnyElement),
		}
		inspectorAppRuntimes[app] = rt
	}
	return rt
}

// SetInspectorRenderer installs the app's inspector UI renderer
// (app.rs set_inspector_renderer, 2827-2831). The renderer runs when
// the window's inspector is toggled on and a frame draws: its element
// is prepainted/painted into the inspector panel (window.rs
// prepaint_inspector/paint_inspector). nil clears it.
func SetInspectorRenderer(app *App, render InspectorRenderer) {
	rt := inspectorRuntimeFor(app)
	if rt == nil {
		panic("gpui: SetInspectorRenderer requires an application")
	}
	rt.renderer = render
}

// RegisterInspectorElement registers a renderer for one inspector
// state type (app.rs register_inspector_element, 2833-2840, over
// inspector.rs InspectorElementRegistry::register): the renderer
// builds the inspector UI element for the active element's recorded
// state of type S.
func RegisterInspectorElement[S any](app *App, render func(id InspectorElementID, state *S, w *Window, app *App) AnyElement) {
	if render == nil {
		panic("gpui: RegisterInspectorElement requires a non-nil renderer")
	}
	rt := inspectorRuntimeFor(app)
	if rt == nil {
		panic("gpui: RegisterInspectorElement requires an application")
	}
	rt.stateRenderers[reflect.TypeFor[S]()] = func(id InspectorElementID, state any, w *Window, app *App) AnyElement {
		if slot, ok := state.(*S); ok {
			return render(id, slot, w, app)
		}
		return Empty()
	}
}

// RenderInspectorStates renders one element for every registered
// inspector state of the window's active inspected element
// (inspector.rs Inspector::render_inspector_states, 165-193: take the
// active element, render each recorded state whose type has a
// registered renderer, restore the active element). Deterministic
// order: state types sorted by name.
func RenderInspectorStates(w *Window, app *App) []AnyElement {
	st := windowInspectorStateOf(w)
	if st == nil || st.activeElement == nil {
		return nil
	}
	rt := inspectorRuntimeFor(app)
	if rt == nil || len(rt.stateRenderers) == 0 {
		return nil
	}
	types := make([]string, 0, len(st.activeStates))
	byName := make(map[string]reflect.Type)
	for typ := range st.activeStates {
		types = append(types, typ.String())
		byName[typ.String()] = typ
	}
	sort.Strings(types)
	var elements []AnyElement
	for _, name := range types {
		render, ok := rt.stateRenderers[byName[name]]
		if !ok {
			continue
		}
		if elem := render(*st.activeElement, st.activeStates[byName[name]], w, app); elem.obj != nil {
			elements = append(elements, elem)
		}
	}
	return elements
}

// ---------------------------------------------------------------------------
// Picking: the mouse-event path (window.rs handle_inspector_mouse_
// event 7322-7377 + hovered_inspector_hitbox 7379-7393)
// ---------------------------------------------------------------------------

// The pin's scroll constants (window.rs 7346-7348: kept in sync with
// SCROLL_LINES in the x11 platform).
const (
	inspectorScrollLines          = 3.0
	inspectorScrollPixelsPerLayer = 36.0
)

// InspectorDispatchMouseEvent dispatches one mouse-family event through
// the inspector picking path (window.rs handle_inspector_mouse_event,
// called from dispatch_mouse_event 6088-6092: while the window is
// picking, ALL other mouse handling is skipped). It reports whether
// the event was consumed (picking active); the caller then skips the
// normal dispatch path.
//
// Mouse moves hover (inspector.rs Inspector::hover), mouse downs
// select (Inspector::select) and scroll wheels adjust the pick depth
// (the SCROLL_LINES/SCROLL_PIXELS_PER_LAYER math, clamped to the hit
// test's depth). The tracked mouse position and modifiers update here
// because this path REPLACES the window's normal mouse dispatch while
// picking (window.rs dispatch_event's tracking arm).
//
// Integration seam, recorded honestly: the pin wires this into
// Window::dispatch_mouse_event's head; the port's dispatch path lives
// in mouseevent.go (a read-only file for this slice), so the inspector
// path is this exported entry the input layer/tests call. The four
// line wiring (return when InspectorDispatchMouseEvent answers true)
// remains for the owner of that file.
func InspectorDispatchMouseEvent(w *Window, app *App, event any) bool {
	if w == nil {
		return false
	}
	st := windowInspectorStateOf(w)
	if st == nil || !st.active || st.pickDepth == nil {
		return false
	}
	fs := focusState(w)
	is := inputState(w)
	// The dispatch-side hit test recompute (the pin's
	// dispatch_mouse_event head, 6083-6086: the hit test under the
	// tracked mouse position).
	switch e := event.(type) {
	case *MouseMoveEvent:
		is.mousePosition = e.Position
		is.modifiers = e.Modifiers
	case *MouseDownEvent:
		is.mousePosition = e.Position
		is.modifiers = e.Modifiers
	case *MouseUpEvent:
		is.mousePosition = e.Position
		is.modifiers = e.Modifiers
	case *ScrollWheelEvent:
		is.mousePosition = e.Position
		is.modifiers = e.Modifiers
	}
	if fs.rendered != nil {
		if hit := fs.rendered.hitTestAt(is.mousePosition); !hit.equal(&is.mouseHitTest) {
			is.mouseHitTest = hit
		}
	}
	hitboxes := windowInspectorHitboxes(w)
	if _, isMove := event.(*MouseMoveEvent); isMove {
		if _, id, ok := hoveredInspectorHitbox(w, hitboxes); ok {
			inspectorHover(w, st, id)
		}
		return true
	}
	if _, isDown := event.(*MouseDownEvent); isDown {
		if _, id, ok := hoveredInspectorHitbox(w, hitboxes); ok {
			InspectorSelect(w, id)
		}
		return true
	}
	if e, isScroll := event.(*ScrollWheelEvent); isScroll {
		lineHeight := float32(inspectorScrollPixelsPerLayer / inspectorScrollLines)
		delta := e.Delta
		var deltaY float32
		if delta.IsPixels {
			deltaY = delta.Pixels.Y
		} else {
			deltaY = lineHeight * delta.Lines.Y
		}
		if depth := st.pickDepth; depth != nil {
			*depth += deltaY / float32(inspectorScrollPixelsPerLayer)
			maxDepth := float32(len(is.mouseHitTest.orderedIDs)) - 0.5
			if *depth < 0 {
				*depth = 0
			} else if *depth > maxDepth {
				*depth = maxDepth
			}
			if _, id, ok := hoveredInspectorHitbox(w, hitboxes); ok {
				inspectorSetActiveElement(w, st, id)
			}
		}
		return true
	}
	// Every other mouse-family event is consumed while picking ("all
	// other mouse handling is skipped").
	return true
}

// hoveredInspectorHitbox resolves the inspector element under the
// tracked mouse position at the pick depth (window.rs
// hovered_inspector_hitbox, 7379-7393): walk the mouse hit test's
// ordered ids (front-most first), skipping pick_depth entries, and
// return the first that has an inspector hitbox in the given map. The
// map is the RENDERED frame's for dispatch (windowInspectorHitboxes)
// or the frame under construction's for the draw-time highlight
// (currentFrame).inspectorHitboxes, mirroring the pin's two call
// sites (rendered_frame at 7352/7363, next_frame at 7314).
func hoveredInspectorHitbox(w *Window, hitboxes map[uint64]InspectorElementID) (uint64, InspectorElementID, bool) {
	st := windowInspectorStateOf(w)
	if st == nil || st.pickDepth == nil {
		return 0, InspectorElementID{}, false
	}
	if len(hitboxes) == 0 {
		return 0, InspectorElementID{}, false
	}
	is := inputState(w)
	depth := int64(*st.pickDepth)
	maxSkipped := len(is.mouseHitTest.orderedIDs)
	if maxSkipped > 0 {
		maxSkipped--
	}
	skipCount := int(depth)
	if skipCount > maxSkipped {
		skipCount = maxSkipped
	}
	if skipCount < 0 {
		skipCount = 0
	}
	ids := is.mouseHitTest.orderedIDs
	for i := skipCount; i < len(ids); i++ {
		if id, ok := hitboxes[ids[i]]; ok {
			return ids[i], id, true
		}
	}
	return 0, InspectorElementID{}, false
}

// ---------------------------------------------------------------------------
// The draw_roots integration (window.rs 3403-3481)
// ---------------------------------------------------------------------------

// inspectorRootShrink reports the inspector panel's width in logical
// pixels when the window's inspector is on (window.rs 3403-3415:
// rems(30.0).to_pixels(rem_size) — 30 rems; draw_roots shrinks the
// root's width by it so the inspected content and the inspector panel
// share the viewport).
func inspectorRootShrink(w *Window, rem float32) float32 {
	st := windowInspectorStateOf(w)
	if st == nil || !st.active {
		return 0
	}
	return 30.0 * rem
}

// inspectorPanelRegion returns the inspector panel's origin and size
// in logical pixels (window.rs prepaint_inspector, 7266-7281: the
// panel occupies the right side of the viewport).
func inspectorPanelRegion(w *Window, viewport Size) (Point, Size) {
	width := inspectorRootShrink(w, DefaultRemSize)
	if width <= 0 {
		return Point{}, Size{}
	}
	return Point{X: viewport.Width - width, Y: 0}, Size{Width: width, Height: viewport.Height}
}

// prepaintInspectorElement prepaints the inspector UI element after the
// root element's prepaint (window.rs prepaint_inspector, 7262-7281):
// when the window's inspector is on and the app has an inspector
// renderer, the renderer's element is laid out and prepainted as a
// root in the panel region. The element is returned for painting.
func prepaintInspectorElement(w *Window, app *App, viewport Size) AnyElement {
	st := windowInspectorStateOf(w)
	if st == nil || !st.active {
		return AnyElement{}
	}
	rt := inspectorRuntimeFor(app)
	if rt == nil || rt.renderer == nil {
		// The pin renders Empty when no renderer is installed
		// (inspector.rs Render's fallback arm).
		return AnyElement{}
	}
	origin, size := inspectorPanelRegion(w, viewport)
	element := rt.renderer(w, app)
	if element.obj == nil {
		return AnyElement{}
	}
	available := AvailableSize{
		Width:  DefiniteAvailableSpace(size.Width),
		Height: DefiniteAvailableSpace(size.Height),
	}
	element.PrepaintAsRoot(origin, available, w, app)
	return element
}

// paintInspectorElement paints the prepainted inspector element
// (window.rs paint_inspector, 3467-3473).
func paintInspectorElement(w *Window, app *App, element AnyElement) {
	if element.obj == nil {
		return
	}
	element.Paint(w, app)
}

// paintInspectorHighlight paints the hovered inspector hitbox's
// highlight quad (window.rs paint_inspector_hitbox, 7306-7320: while
// an inspector exists and a hitbox is hovered under the pick depth,
// fill the hitbox's bounds with rgba(0x61afef4d)) into the frame under
// construction.
func paintInspectorHighlight(w *Window, app *App) {
	st := windowInspectorStateOf(w)
	if st == nil || !st.active {
		return
	}
	fs := focusState(w)
	if fs.next == nil {
		return
	}
	// The pin passes &self.next_frame (the frame under construction,
	// whose hitboxes and inspector map are being filled this draw).
	hitboxID, _, ok := hoveredInspectorHitbox(w, currentFrame(w).inspectorHitboxes)
	if !ok {
		return
	}
	for _, record := range fs.next.hitboxes {
		if record.box.id == hitboxID {
			frame := currentFrame(w)
			pq := PaintQuad{
				Bounds:             record.box.Bounds,
				Background:         SolidBackground(RgbaToHsla(0x61afef4d)),
				BorderColor:        SolidBackground(TransparentBlack()),
				BorderDashedLength: DefaultBorderDashedLength,
				BorderDashedGap:    DefaultBorderDashedGap,
			}
			if err := frame.scene.PaintQuad(pq, 0, frame.paint); err != nil {
				panic(fmt.Sprintf("gpui: inspector highlight: %v", err))
			}
			return
		}
	}
}
