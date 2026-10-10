package inspectorspec

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"gpui-go/gpui"
)

// This file is the inspector corpus (ticket27): the element-id paths
// with the capability separation, the tree/selection correspondence
// against the virtualized custom-component tree (removal, reparenting,
// scrolling, reinsertion), picking through the real hit-test
// machinery, the abandoned-frame and window-closure diagnostics, the
// leak/cycle detector over the real entity map, and the debug frame
// overlay's modes, stats and scene paint. All tests draw through the
// real element phases (the native layout service), so they skip under
// -short like the accessibility corpus.

// ---------------------------------------------------------------------------
// The capability surface (the cfg mapping)
// ---------------------------------------------------------------------------

// TestCapabilityProfileDefaultsAndReleaseBehavior pins the explicit
// capability surface: the default is the pin's cargo-test profile
// (inspector ids + leak detection on, the profiler overlay off), and
// flipping to the release profile reproduces the reference's
// cfg'd-out branches — no inspector ids are built, no tree records,
// toggling is inert, picking is off and WithInspectorState passes a
// nil state — while IsInspectorPicking stays queryable (the pin
// compiles that method ungated).
func TestCapabilityProfileDefaultsAndReleaseBehavior(t *testing.T) {
	if testing.Short() {
		t.Skip("draws through the native layout service")
	}
	defer gpui.SetDiagnosticCapability(gpui.CapabilitiesTestProfile, true)

	if got := gpui.DiagnosticCapabilities(); got != gpui.CapabilitiesTestProfile {
		t.Errorf("default capabilities = %v, want the test profile (inspector ids + leak detection, no overlay)", got)
	}
	if !gpui.DiagnosticCapabilityEnabled(gpui.CapInspectorIDs) || !gpui.DiagnosticCapabilityEnabled(gpui.CapLeakDetection) {
		t.Error("the test profile must enable the inspector ids and the leak detection")
	}
	if gpui.DiagnosticCapabilityEnabled(gpui.CapFrameOverlay) {
		t.Error("the test profile must keep the profiler-gated overlay off (cargo test runs without the feature)")
	}

	f := newInspectorFixture(t, 20)
	f.toggleInspector(t)
	f.draw(t)
	if snapshot := gpui.InspectorTreeSnapshotOf(f.window); snapshot == nil || snapshot.Count() == 0 {
		t.Fatal("the inspector tree must record while the capability is on")
	}

	// The release profile: every gated branch is inert.
	gpui.SetDiagnosticCapabilities(gpui.CapabilitiesReleaseProfile)
	defer gpui.SetDiagnosticCapabilities(gpui.CapabilitiesTestProfile)
	f.window.ToggleInspector(f.app) // inert: no state exists in release
	if f.window.IsInspectorPicking() {
		t.Error("release profile must not pick")
	}
	f.draw(t)
	if snapshot := gpui.InspectorTreeSnapshotOf(f.window); snapshot != nil {
		t.Errorf("release profile published a tree: %s", snapshot.Trace())
	}
	// The element phases receive a nil inspector id in release (the
	// cfg'd-out branch leaves inspector_id = None).
	seen := 0
	probe := &phaseProbe{onInspect: func(id *gpui.InspectorElementID) {
		if id != nil {
			seen++
		}
	}}
	probeWindow := gpui.NewTestWindow(f.app, gpui.Size{Width: 200, Height: 100})
	probeWindow.SetRootView(&probeView{build: func() gpui.AnyElement {
		return gpui.CustomElement[struct{}, struct{}](probe)
	}})
	if _, err := gpui.DrawWindowFrame(probeWindow); err != nil {
		t.Fatalf("release-profile probe draw: %v", err)
	}
	if seen != 0 {
		t.Errorf("release profile built %d inspector ids, want 0", seen)
	}
	gpui.CloseTestWindow(probeWindow)
	// WithInspectorState passes a nil state (f(&mut None, window)).
	gpui.WithInspectorState(f.window, nil, func(state *ItemInspectorState, exists bool, w *gpui.Window) (struct{}, *ItemInspectorState) {
		if state != nil || exists {
			t.Error("release profile passed a live inspector state")
		}
		return struct{}{}, nil
	})
}

// phaseProbe records whether its phases received a non-nil inspector
// id (the long-id path check).
type phaseProbe struct {
	onInspect func(id *gpui.InspectorElementID)
	gotLayout bool
	gotPaint  bool
	idLayout  *gpui.InspectorElementID
	idPrepain *gpui.InspectorElementID
	idPaint   *gpui.InspectorElementID
}

// ID implements Element.
func (e *phaseProbe) ID() (gpui.ElementID, bool) { return gpui.NameElementID("probe"), true }

// SourceLocation implements Element: the probe participates in the
// long-id paths.
func (e *phaseProbe) SourceLocation() *gpui.SourceLocation {
	location := gpui.SourceLocation{File: "inspectorspec/inspector_test.go", Line: 100}
	return &location
}

// RequestLayout implements Element.
func (e *phaseProbe) RequestLayout(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, w *gpui.Window, app *gpui.App) (gpui.LayoutID, struct{}) {
	e.idLayout = inspector
	e.gotLayout = inspector != nil
	e.onInspect(inspector)
	id, err := gpui.RequestElementLayout(w, gpui.DefaultStyle())
	if err != nil {
		panic(err)
	}
	return id, struct{}{}
}

// Prepaint implements Element.
func (e *phaseProbe) Prepaint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *struct{}, w *gpui.Window, app *gpui.App) struct{} {
	e.idPrepain = inspector
	return struct{}{}
}

// Paint implements Element.
func (e *phaseProbe) Paint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *struct{}, prepaint *struct{}, w *gpui.Window, app *gpui.App) {
	e.idPaint = inspector
	e.gotPaint = inspector != nil
	e.onInspect(inspector)
}

// LayoutNodeStyle implements the root-stretch report.
func (e *phaseProbe) LayoutNodeStyle() (gpui.Style, bool) { return gpui.DefaultStyle(), true }

// probeView builds one fresh element per render (the drawable phase
// machine is single-use, so roots must rebuild every frame).
type probeView struct {
	build func() gpui.AnyElement
}

// EntityID implements gpui.View.
func (v *probeView) EntityID() (gpui.EntityID, bool) { return 0, false }

// RenderOnce implements gpui.View.
func (v *probeView) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement { return v.build() }

// TestInspectorIDFlowsThroughEveryPhase pins the long id's phase path
// (element.rs:71-108): request_layout, prepaint and paint receive the
// SAME InspectorElementID (path + instance), and the id's global path
// carries the element's own identity.
func TestInspectorIDFlowsThroughEveryPhase(t *testing.T) {
	if testing.Short() {
		t.Skip("draws through the native layout service")
	}
	ta := gpui.NewTestApp()
	app := ta.App()
	window := gpui.NewTestWindow(app, gpui.Size{Width: 200, Height: 100})
	probe := &phaseProbe{onInspect: func(*gpui.InspectorElementID) {}}
	window.SetRootView(&probeView{build: func() gpui.AnyElement {
		return gpui.CustomElement[struct{}, struct{}](probe)
	}})
	if _, err := gpui.DrawWindowFrame(window); err != nil {
		t.Fatalf("draw: %v", err)
	}
	if !probe.gotLayout || !probe.gotPaint {
		t.Fatalf("phases received ids: layout=%v paint=%v (want both, the default capability is on)", probe.gotLayout, probe.gotPaint)
	}
	if !probe.idLayout.Equal(*probe.idPrepain) || !probe.idLayout.Equal(*probe.idPaint) {
		t.Errorf("phase ids differ: layout=%s prepaint=%s paint=%s", probe.idLayout, probe.idPrepain, probe.idPaint)
	}
	if probe.idLayout.InstanceID() != 0 {
		t.Errorf("first same-path element instance id = %d, want 0", probe.idLayout.InstanceID())
	}
	if got := probe.idLayout.GlobalPath(); !strings.Contains(got, "probe") {
		t.Errorf("global path %q must carry the element's own id", got)
	}
}

// TestLongIDInstanceDisambiguation pins the instance-id semantics
// (inspector.rs InspectorElementId + window.rs
// build_inspector_element_id): the uniform list renders anonymous
// items from ONE source location, so every item shares the path and
// the per-frame instance ids disambiguate them (the measurement pass
// consumes instance 0; the visible items get 1..N in prepaint order).
func TestLongIDInstanceDisambiguation(t *testing.T) {
	if testing.Short() {
		t.Skip("draws through the native layout service")
	}
	f := newInspectorFixture(t, 20)
	f.toggleInspector(t)
	f.draw(t)
	snapshot := gpui.InspectorTreeSnapshotOf(f.window)
	var items []gpui.InspectorTreeNode
	for _, node := range snapshot.Nodes() {
		if node.Location == itemSource {
			items = append(items, node)
		}
	}
	if len(items) != 14 {
		t.Fatalf("visible items = %d, want 14 (the 270px list at a 20px pitch)", len(items))
	}
	for i, node := range items {
		if node.ID.InstanceID() != uint64(i+1) {
			t.Errorf("item %d instance id = %d, want %d (0 is the measurement pass)", i, node.ID.InstanceID(), i+1)
		}
		if i > 0 && items[i].ID.Equal(items[i-1].ID) {
			t.Errorf("items %d and %d share one identity", i-1, i)
		}
		if items[i].Global != items[0].Global {
			t.Errorf("item %d global path %q differs from item 0's %q (anonymous items share the path)", i, items[i].Global, items[0].Global)
		}
	}
}

// TestTreeSnapshotPublicationAndImmutability pins the success-gated
// tree publication: the snapshot is immutable against later app
// mutation, redraws republish, and the trace is the deterministic
// observable.
func TestTreeSnapshotPublicationAndImmutability(t *testing.T) {
	if testing.Short() {
		t.Skip("draws through the native layout service")
	}
	f := newInspectorFixture(t, 20)
	if got := gpui.InspectorTreeSnapshotOf(f.window); got != nil {
		t.Fatal("a tree published before the inspector was toggled on")
	}
	f.toggleInspector(t)
	f.draw(t)
	first := gpui.InspectorTreeSnapshotOf(f.window)
	if first == nil || first.Count() != 16 {
		t.Fatalf("first tree count = %v, want 16 (container + header + 14 items)", first)
	}
	if first.Frame() != 1 {
		t.Errorf("first tree frame = %d, want 1", first.Frame())
	}
	trace := first.Trace()

	// Mutate the app WITHOUT redrawing: the published snapshot keeps
	// its values (value copies; nothing aliases the live tree).
	f.mutate(t, func(m *PanelModel, cx *gpui.Context[PanelModel]) {
		m.itemHeight = 40
	})
	if got := gpui.InspectorTreeSnapshotOf(f.window).Trace(); got != trace {
		t.Errorf("published snapshot mutated without a draw:\n%s", got)
	}
	nodes := gpui.InspectorTreeSnapshotOf(f.window).Nodes()
	nodes[0].Bounds = gpui.Bounds{}
	if !strings.Contains(gpui.InspectorTreeSnapshotOf(f.window).Trace(), "bounds=(0, 0, 320 x 300)") {
		t.Error("mutating the returned nodes reached the snapshot")
	}

	// Redraw: a fresh publication at the next frame.
	f.draw(t)
	second := gpui.InspectorTreeSnapshotOf(f.window)
	if second == first {
		t.Fatal("redraw did not publish a new snapshot")
	}
	if second.Frame() != 2 {
		t.Errorf("second tree frame = %d, want 2", second.Frame())
	}
	// The taller pitch virtualizes harder: fewer visible items.
	if got := countNodes(second, itemSource); got != 7 {
		t.Errorf("visible items after the pitch change = %d, want 7 (270/40)", got)
	}
}

// countNodes counts the snapshot's nodes at one source location.
func countNodes(snapshot *gpui.InspectorTreeSnapshot, location gpui.SourceLocation) int {
	count := 0
	for _, node := range snapshot.Nodes() {
		if node.Location == location {
			count++
		}
	}
	return count
}

// ---------------------------------------------------------------------------
// Selection: current bounds/state (the acceptance's core)
// ---------------------------------------------------------------------------

// TestSelectionReportsCurrentBoundsAndState pins the selection report:
// selecting a node reports the CURRENT bounds and state from the live
// frame; a model change that moves the element updates the report
// after the redraw; the typed state is written only by the live
// element whose inspector id is the active one (the div.rs
// DivInspectorState pattern).
func TestSelectionReportsCurrentBoundsAndState(t *testing.T) {
	if testing.Short() {
		t.Skip("draws through the native layout service")
	}
	f := newInspectorFixture(t, 20)
	f.toggleInspector(t)
	f.draw(t)
	snapshot := gpui.InspectorTreeSnapshotOf(f.window)
	node := itemNodeAt(t, snapshot, 50) // the item at y=50 (index 1)
	if node.Bounds.Origin.Y != 50 || node.Bounds.Size.Height != 20 {
		t.Fatalf("item node bounds = %s, want (0, 50, 320 x 20)", node.Bounds)
	}

	if changed := gpui.InspectorSelect(f.window, node.ID); !changed {
		t.Fatal("selecting an unselected node must report a change")
	}
	// The selection leaves picking mode (Inspector::select) and requests
	// a refresh: the typed state is written by the element during the
	// NEXT frame's phases (div.rs writes DivInspectorState at
	// request_layout/prepaint while it is the active element).
	if f.window.IsInspectorPicking() {
		t.Error("selection must leave picking mode")
	}
	if !f.window.RefreshRequested() {
		t.Error("selecting must request a redraw (Inspector::select refreshes)")
	}
	f.draw(t)
	live := gpui.InspectorTreeSnapshotOf(f.window)
	report, ok := gpui.InspectorSelectionOf(f.window)
	if !ok || !report.Present {
		t.Fatalf("selection report = %+v, want a live selection", report)
	}
	if report.Bounds != node.Bounds {
		t.Errorf("selection bounds = %s, want the tree node's %s", report.Bounds, node.Bounds)
	}
	if report.Frame != live.Frame() {
		t.Errorf("selection frame = %d, want the live tree's %d", report.Frame, live.Frame())
	}
	// The item wrote its typed state during the live draw.
	state, frame, ok := gpui.InspectorActiveElementState[ItemInspectorState](f.window)
	if !ok {
		t.Fatal("the selected item did not write its inspector state")
	}
	if state.Kind != "item-1" {
		t.Errorf("state kind = %q, want item-1", state.Kind)
	}
	if state.Bounds != node.Bounds {
		t.Errorf("state bounds = %s, want the item's %s", state.Bounds, node.Bounds)
	}
	if frame != live.Frame() {
		t.Errorf("state write frame = %d, want %d (written during the live draw)", frame, live.Frame())
	}
	if state.Writes != 1 {
		t.Errorf("state writes = %d, want 1 (the selection reset the states)", state.Writes)
	}
	// Selecting the same id again is not a change.
	if changed := gpui.InspectorSelect(f.window, node.ID); changed {
		t.Error("re-selecting the same id must not report a change")
	}

	// Mutate the pitch and redraw: the SAME identity keeps rendering
	// (same path, same instance) and the report shows the CURRENT
	// bounds — the live element re-writes its state.
	f.mutate(t, func(m *PanelModel, cx *gpui.Context[PanelModel]) {
		m.itemHeight = 30
	})
	f.draw(t)
	report, _ = gpui.InspectorSelectionOf(f.window)
	if !report.Present {
		t.Fatal("the selection died across a pitch change (the same identity keeps rendering)")
	}
	if report.Bounds.Size.Height != 30 {
		t.Errorf("current bounds height = %v, want 30 (the mutated pitch)", report.Bounds.Size.Height)
	}
	if report.Bounds.Origin.Y != 60 {
		t.Errorf("current bounds y = %v, want 60 (the header plus one 30px row)", report.Bounds.Origin.Y)
	}
	state, frame, _ = gpui.InspectorActiveElementState[ItemInspectorState](f.window)
	if state.Bounds.Size.Height != 30 || state.Writes < 2 {
		t.Errorf("state = %+v, want the live re-write at the new pitch (Writes >= 2)", state)
	}
	if frame != report.Frame {
		t.Errorf("state write frame = %d, want the current frame %d", frame, report.Frame)
	}

	// Selecting a DIFFERENT element resets the typed states (a fresh
	// InspectedElement).
	header := headerNode(t, gpui.InspectorTreeSnapshotOf(f.window))
	gpui.InspectorSelect(f.window, header.ID)
	state, _, ok = gpui.InspectorActiveElementState[ItemInspectorState](f.window)
	if ok {
		t.Errorf("the item state survived a re-selection: %+v", state)
	}
	headerState, _, ok := gpui.InspectorActiveElementState[ItemInspectorState](f.window)
	if ok && headerState.Kind != "header" {
		t.Errorf("header state = %+v", headerState)
	}
}

// TestSelectionStateStalenessWhenElementStopsWriting pins the state
// frame discipline: when the selected element stops rendering (its
// subtree is removed), the retained state's write frame stays at the
// last live write — no live element wrote since — while the bounds
// report goes dead. The retained state is the pin's behavior (states
// persist until re-selection); the stale write frame is the port's
// observable that distinguishes it from a live write.
func TestSelectionStateStalenessWhenElementStopsWriting(t *testing.T) {
	if testing.Short() {
		t.Skip("draws through the native layout service")
	}
	f := newInspectorFixture(t, 20)
	f.toggleInspector(t)
	f.draw(t)
	node := itemNodeAt(t, gpui.InspectorTreeSnapshotOf(f.window), 50)
	gpui.InspectorSelect(f.window, node.ID)
	f.draw(t)
	_, liveFrame, ok := gpui.InspectorActiveElementState[ItemInspectorState](f.window)
	if !ok || liveFrame != 2 {
		t.Fatalf("the live write frame = %d ok=%v, want frame 2 (the post-selection draw)", liveFrame, ok)
	}

	// Remove the list: the selected item stops rendering.
	f.mutate(t, func(m *PanelModel, cx *gpui.Context[PanelModel]) {
		m.showList = false
	})
	f.draw(t)
	if report, _ := gpui.InspectorSelectionOf(f.window); report.Present {
		t.Fatalf("the removed subtree's selection is live: %+v", report)
	}
	_, staleFrame, ok := gpui.InspectorActiveElementState[ItemInspectorState](f.window)
	if !ok {
		t.Fatal("the retained state must persist (the pin keeps it until re-selection)")
	}
	if staleFrame != liveFrame {
		t.Errorf("the stale write frame = %d, want the last live write %d (no live element wrote)", staleFrame, liveFrame)
	}
}

// ---------------------------------------------------------------------------
// Picking (window.rs handle_inspector_mouse_event)
// ---------------------------------------------------------------------------

// TestPickingHoverSelectAndPickDepth drives the inspector's mouse path
// over the REAL hit-test machinery: toggling the inspector on enters
// picking (Inspector::new starts at pick depth 0), a mouse move hovers
// the front-most element, a mouse down selects it, and the scroll
// wheel walks the pick depth through the hit-test ordering.
func TestPickingHoverSelectAndPickDepth(t *testing.T) {
	if testing.Short() {
		t.Skip("draws through the native layout service")
	}
	f := newInspectorFixture(t, 20)
	f.toggleInspector(t)
	if !f.window.IsInspectorPicking() {
		t.Fatal("toggling the inspector on must enter picking mode (Inspector::new)")
	}
	// The frame must draw WHILE picking so the inspector hitboxes are
	// registered (insert_inspector_hitbox only records while picking).
	f.draw(t)

	// A move over item 1 (y=50) hovers it: the active element updates
	// without leaving picking mode.
	move := &gpui.MouseMoveEvent{Position: gpui.Point{X: 100, Y: 55}}
	if !gpui.InspectorDispatchMouseEvent(f.window, f.app, move) {
		t.Fatal("a mouse move while picking must be consumed by the inspector path")
	}
	active, ok := gpui.InspectorActiveElement(f.window)
	if !ok {
		t.Fatal("the move did not hover an element")
	}
	if active.InstanceID() != 2 {
		t.Errorf("hovered instance id = %d, want 2 (item 1)", active.InstanceID())
	}
	if !f.window.IsInspectorPicking() {
		t.Error("hovering must keep picking mode")
	}

	// A down selects: picking ends and the selection is live.
	down := &gpui.MouseDownEvent{Button: gpui.MouseButtonLeft, Position: gpui.Point{X: 100, Y: 55}}
	if !gpui.InspectorDispatchMouseEvent(f.window, f.app, down) {
		t.Fatal("a mouse down while picking must be consumed")
	}
	if f.window.IsInspectorPicking() {
		t.Error("selecting must leave picking mode")
	}
	report, ok := gpui.InspectorSelectionOf(f.window)
	if !ok || !report.Present {
		t.Fatalf("selection after pick = %+v", report)
	}
	if report.Bounds.Origin.Y != 50 {
		t.Errorf("picked bounds = %s, want the item-1 row at y=50", report.Bounds)
	}

	// Re-enter picking and walk the depth with the scroll wheel: each
	// layer skips one hit-test entry, so the hovered element moves up
	// the stacking (toward the parents).
	f.window.StartInspectorPicking()
	// Draw again while picking so the hitboxes re-register (the
	// previous frame was drawn while NOT picking after the select).
	f.draw(t)
	gpui.InspectorDispatchMouseEvent(f.window, f.app, &gpui.MouseMoveEvent{Position: gpui.Point{X: 100, Y: 55}})
	scroll := &gpui.ScrollWheelEvent{
		Position: gpui.Point{X: 100, Y: 55},
		Delta:    gpui.ScrollDeltaLines(0, 3), // one layer: 3 lines * 12 px/line
	}
	if !gpui.InspectorDispatchMouseEvent(f.window, f.app, scroll) {
		t.Fatal("a scroll while picking must be consumed")
	}
	active, _ = gpui.InspectorActiveElement(f.window)
	if active.InstanceID() != 0 {
		// Skipping one hit-test entry moves from item 1 (instance 2) up
		// the stacking to the CONTAINER (instance 0), whose hitbox sits
		// behind every item's.
		t.Errorf("after one pick-depth layer the hovered instance = %d, want 0 (the container, the next hit-test entry)", active.InstanceID())
	}

	// Non-picking windows do not consume: the same event flows to the
	// normal dispatch path.
	gpui.InspectorSelect(f.window, active)
	if gpui.InspectorDispatchMouseEvent(f.window, f.app, move) {
		t.Error("a mouse move while NOT picking must not be consumed by the inspector")
	}
}

// ---------------------------------------------------------------------------
// The cached/virtualized correspondence: removal, reparenting,
// scrolling, reinsertion
// ---------------------------------------------------------------------------

// TestVirtualizedRemovalStaleSelectionIsDead pins the removal case:
// the selected item is removed from the virtualized window; after the
// redraw the selection resolves to DEAD (absent from the current
// tree), never to a revived node.
func TestVirtualizedRemovalStaleSelectionIsDead(t *testing.T) {
	if testing.Short() {
		t.Skip("draws through the native layout service")
	}
	f := newInspectorFixture(t, 20)
	f.toggleInspector(t)
	f.draw(t)
	snapshot := gpui.InspectorTreeSnapshotOf(f.window)
	// The last visible item (index 13, instance 14).
	node := itemNodeAt(t, snapshot, 290)
	if node.ID.InstanceID() != 14 {
		t.Fatalf("the y=290 item instance id = %d, want 14 (the last visible)", node.ID.InstanceID())
	}
	gpui.InspectorSelect(f.window, node.ID)
	f.draw(t) // the selected item writes its typed state while live

	// Remove items 10..19: only ten items remain, so the visible window
	// renders instances 1..10 and no element with instance id 14
	// prepaints anymore.
	f.mutate(t, func(m *PanelModel, cx *gpui.Context[PanelModel]) {
		m.items = m.items[:10]
	})
	f.draw(t)
	report, ok := gpui.InspectorSelectionOf(f.window)
	if !ok {
		t.Fatal("the selection itself must persist (the pin keeps the active id)")
	}
	if report.Present {
		t.Fatalf("the removed item's selection resolved to a live node: %+v (stale selections must be dead)", report)
	}
	// The retained state is stale: no live element wrote since the
	// removal (the write frame stays at the pre-removal draw).
	state, frame, hasState := gpui.InspectorActiveElementState[ItemInspectorState](f.window)
	if !hasState {
		t.Fatal("the pin retains the state until re-selection; the report must show it stale")
	}
	if state.Kind != "item-13" {
		t.Errorf("retained state kind = %q, want the removed item's last write", state.Kind)
	}
	if frame >= gpui.InspectorTreeSnapshotOf(f.window).Frame() {
		t.Errorf("stale state write frame = %d, want < the current frame %d", frame, gpui.InspectorTreeSnapshotOf(f.window).Frame())
	}
	// The reinserted corpus (a different item count) does not revive
	// the OLD identity: instance 14 does not exist (13 items).
	if _, found := gpui.InspectorTreeSnapshotOf(f.window).Node(node.ID); found {
		t.Error("the removed identity resolved in the current tree")
	}
}

// TestReparentedSelectionResolvesToNewPath pins the reparenting case:
// moving the list under a different container id changes every item's
// identity path; the stale selection dies and the items live at their
// new paths.
func TestReparentedSelectionResolvesToNewPath(t *testing.T) {
	if testing.Short() {
		t.Skip("draws through the native layout service")
	}
	f := newInspectorFixture(t, 20)
	f.toggleInspector(t)
	f.draw(t)
	snapshot := gpui.InspectorTreeSnapshotOf(f.window)
	node := itemNodeAt(t, snapshot, 50)
	if !strings.Contains(node.Global, ".main") {
		t.Fatalf("item path %q must run through the main container", node.Global)
	}
	gpui.InspectorSelect(f.window, node.ID)

	// Reparent: the container's id flips to "alt".
	f.mutate(t, func(m *PanelModel, cx *gpui.Context[PanelModel]) {
		m.containerName = "alt"
	})
	f.draw(t)
	report, _ := gpui.InspectorSelectionOf(f.window)
	if report.Present {
		t.Fatalf("the reparented selection stayed live: %+v (the old path is gone)", report)
	}
	// The items render under the new path with the same geometry.
	after := gpui.InspectorTreeSnapshotOf(f.window)
	moved := itemNodeAt(t, after, 50)
	if moved.Bounds != node.Bounds {
		t.Errorf("the reparented item moved: %s -> %s", node.Bounds, moved.Bounds)
	}
	if !strings.Contains(moved.Global, ".alt") {
		t.Errorf("reparented item path = %q, want the alt container", moved.Global)
	}
	if moved.ID.Equal(node.ID) {
		t.Errorf("the reparented item kept the old identity %s", moved.ID)
	}
	// Selecting the moved node reports its current bounds.
	gpui.InspectorSelect(f.window, moved.ID)
	report, _ = gpui.InspectorSelectionOf(f.window)
	if !report.Present || report.Bounds != moved.Bounds {
		t.Fatalf("selection after reparenting = %+v, want the moved node's bounds", report)
	}
}

// TestScrollVirtualizedWindowCorrespondence pins the virtualized
// correspondence: after scrolling, the tree contains exactly the
// visible items and each node's bounds are the SCROLLED geometry (the
// live window state), not the pre-scroll positions.
func TestScrollVirtualizedWindowCorrespondence(t *testing.T) {
	if testing.Short() {
		t.Skip("draws through the native layout service")
	}
	f := newInspectorFixture(t, 20)
	f.toggleInspector(t)
	f.draw(t)
	before := itemNodeAt(t, gpui.InspectorTreeSnapshotOf(f.window), 50)

	// Scroll down two items (40px).
	f.mutate(t, func(m *PanelModel, cx *gpui.Context[PanelModel]) {
		m.handle.SetOffset(gpui.Point{X: 0, Y: -40})
	})
	f.draw(t)
	after := gpui.InspectorTreeSnapshotOf(f.window)
	if got := countNodes(after, itemSource); got != 14 {
		t.Fatalf("visible items after scrolling = %d, want 14 (the window size is unchanged)", got)
	}
	// Item 2 is now the top visible row: its bounds moved up by the
	// scroll offset (y = 30 + 40 - 40).
	top := itemNodeAt(t, after, 30)
	if top.Bounds.Origin.Y != 30 || top.Bounds.Size.Height != 20 {
		t.Errorf("the scrolled top item bounds = %s, want (0, 30, 320 x 20)", top.Bounds)
	}
	// The identities are POSITIONAL per frame (path + prepaint-order
	// instance): the scrolled frame's instance 2 is the item that now
	// renders second (index 3), at its scrolled position — the
	// correspondence that matters is that every node reports its
	// CURRENT (scrolled) geometry.
	shifted, found := after.Node(before.ID)
	if !found || shifted.Bounds.Origin.Y != 50 {
		t.Errorf("the scrolled frame's slot-1 identity reports %+v, want the scrolled row at y=50", shifted)
	}
	// The item that scrolled to the top of the window resolves at its
	// CURRENT (scrolled) position.
	if node, found := after.Node(top.ID); !found || node.Bounds.Origin.Y != 30 {
		t.Errorf("the top item does not resolve at its current bounds: %+v", node)
	}
}

// TestReinsertedTreeIdentityIsPositional pins the pin's identity model
// honestly: after the subtree is removed (the selection goes dead) and
// reinserted with the SAME structure, the path+instance identity
// resolves again — the pin's selection persistence across identical
// rebuilds — and the state is re-written by the element that NOW owns
// the identity (a live write, not a revival of the old element).
func TestReinsertedTreeIdentityIsPositional(t *testing.T) {
	if testing.Short() {
		t.Skip("draws through the native layout service")
	}
	f := newInspectorFixture(t, 20)
	f.toggleInspector(t)
	f.draw(t)
	snapshot := gpui.InspectorTreeSnapshotOf(f.window)
	node := itemNodeAt(t, snapshot, 50)
	gpui.InspectorSelect(f.window, node.ID)

	// Remove the whole list: dead.
	f.mutate(t, func(m *PanelModel, cx *gpui.Context[PanelModel]) {
		m.showList = false
	})
	f.draw(t)
	if report, _ := gpui.InspectorSelectionOf(f.window); report.Present {
		t.Fatalf("the removed subtree's selection is live: %+v", report)
	}

	// Reinsert the identical structure: the same path and instance
	// numbering resolve again (the pin's identity model).
	f.mutate(t, func(m *PanelModel, cx *gpui.Context[PanelModel]) {
		m.showList = true
	})
	f.draw(t)
	report, _ := gpui.InspectorSelectionOf(f.window)
	if !report.Present {
		t.Fatal("the reinserted identical structure must resolve the selection (the pin's path+instance identity)")
	}
	// The report shows a LIVE write by the element that now owns the
	// identity: the state frame is the current frame.
	state, frame, ok := gpui.InspectorActiveElementState[ItemInspectorState](f.window)
	if !ok || frame != report.Frame {
		t.Fatalf("state after reinsertion = %+v frame=%d, want a live write at frame %d", state, frame, report.Frame)
	}
	if state.Kind != "item-1" {
		t.Errorf("state kind = %q, want item-1 (the slot's current content)", state.Kind)
	}
}

// TestRetainedStateDropsWhenItemLeavesTheVirtualWindow rides ticket14's
// keyed-state retention through the VARIABLE-height list (whose items
// carry per-index element identities): the retained element state of
// an item that leaves the virtualized window is dropped at frame end
// and starts fresh when the item returns. This is the cached/
// virtualized retained-state correspondence the inspector tree rides.
func TestRetainedStateDropsWhenItemLeavesTheVirtualWindow(t *testing.T) {
	if testing.Short() {
		t.Skip("draws through the native layout service")
	}
	ta := gpui.NewTestApp()
	app := ta.App()
	window := gpui.NewTestWindow(app, gpui.Size{Width: 200, Height: 300})
	state := gpui.NewListState(20, 0)
	window.SetRootView(&keyedListView{state: state})
	recordedKeyedGlobals = map[int]*gpui.GlobalElementID{}
	if _, err := gpui.DrawWindowFrame(window); err != nil {
		t.Fatalf("draw: %v", err)
	}
	global, ok := recordedKeyedGlobals[5]
	if !ok {
		t.Fatal("item 5 did not render (or did not record its global id)")
	}
	if count, ok := gpui.ElementStateOf[int](window, global); !ok || count != 1 {
		t.Fatalf("retained state = %d ok=%v, want 1 write (one frame)", count, ok)
	}
	// A second frame accumulates.
	if _, err := gpui.DrawWindowFrame(window); err != nil {
		t.Fatalf("draw: %v", err)
	}
	if count, _ := gpui.ElementStateOf[int](window, global); count != 2 {
		t.Fatalf("retained state after two frames = %d, want 2", count)
	}
	// Splice items 10..19 away: item 5 keeps rendering (its state
	// persists); item 15's state drops at frame end.
	globalFifteen := recordedKeyedGlobals[14]
	if globalFifteen == nil {
		t.Fatal("item 14 (the last visible row) did not render in the full list")
	}
	state.Splice(10, 20, 0)
	if _, err := gpui.DrawWindowFrame(window); err != nil {
		t.Fatalf("draw: %v", err)
	}
	if count, ok := gpui.ElementStateOf[int](window, global); !ok || count != 3 {
		t.Fatalf("the still-rendering item's state = %d ok=%v, want 3", count, ok)
	}
	if _, ok := gpui.ElementStateOf[int](window, globalFifteen); ok {
		t.Error("the spliced-out item's retained state survived the frame-end retention")
	}
	// Reinsert: the keyed state starts fresh.
	state.Splice(10, 10, 10)
	if _, err := gpui.DrawWindowFrame(window); err != nil {
		t.Fatalf("draw: %v", err)
	}
	if count, ok := gpui.ElementStateOf[int](window, globalFifteen); !ok || count != 1 {
		t.Fatalf("the reinserted item's retained state = %d ok=%v, want a fresh 1", count, ok)
	}
}

// keyedListView renders the variable-height list of keyed items (the
// ticket14 List with its per-index item identities).
type keyedListView struct {
	state *gpui.ListState
}

// EntityID implements gpui.View.
func (v *keyedListView) EntityID() (gpui.EntityID, bool) { return 0, false }

// RenderOnce implements gpui.View.
func (v *keyedListView) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return gpui.List(v.state, func(ix int, w *gpui.Window, app *gpui.App) gpui.AnyElement {
		return gpui.CustomElement[struct{}, struct{}](&keyedItem{ix: ix})
	}).SizeFull().IntoElement()
}

// recordedKeyedGlobals records each keyed item's global id path during
// prepaint (the test's ElementStateOf lookup key).
var recordedKeyedGlobals map[int]*gpui.GlobalElementID

// keyedItem is one keyed list item: a fixed 200x20 leaf whose retained
// element state counts its frames.
type keyedItem struct {
	ix int
}

// ID implements Element: the item's own name identity (the list's
// per-index wrapper id prefixes it), so the keyed retained state
// resolves by the item's own global id path.
func (e *keyedItem) ID() (gpui.ElementID, bool) {
	return gpui.NameElementID("keyed-" + strconv.Itoa(e.ix)), true
}

// SourceLocation implements Element.
func (e *keyedItem) SourceLocation() *gpui.SourceLocation { return nil }

// RequestLayout implements Element.
func (e *keyedItem) RequestLayout(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, w *gpui.Window, app *gpui.App) (gpui.LayoutID, struct{}) {
	style := gpui.DefaultStyle()
	style.Size.Width = gpui.PxLength(200)
	style.Size.Height = gpui.PxLength(20)
	id, err := gpui.RequestElementLayout(w, style)
	if err != nil {
		panic(err)
	}
	return id, struct{}{}
}

// Prepaint implements Element: record the global id and bump the
// retained keyed state.
func (e *keyedItem) Prepaint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *struct{}, w *gpui.Window, app *gpui.App) struct{} {
	if global != nil && recordedKeyedGlobals != nil {
		recordedKeyedGlobals[e.ix] = global
	}
	gpui.WithOptionalElementState[int](w, global, func(state *int, w *gpui.Window) (struct{}, *int) {
		count := 1
		if state != nil {
			count = *state + 1
		}
		return struct{}{}, &count
	})
	return struct{}{}
}

// Paint implements Element.
func (e *keyedItem) Paint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *struct{}, prepaint *struct{}, w *gpui.Window, app *gpui.App) {
}

// LayoutNodeStyle implements the root-stretch report.
func (e *keyedItem) LayoutNodeStyle() (gpui.Style, bool) { return gpui.DefaultStyle(), true }

// ---------------------------------------------------------------------------
// Abandoned frames and window closure
// ---------------------------------------------------------------------------

// TestAbandonedFramePreservesPriorPublishedTree pins the
// success-gated publication during diagnostics: a build that panics
// mid-frame leaves the previously published tree, the draw count and
// the selection intact (an abandoned frame never publishes).
func TestAbandonedFramePreservesPriorPublishedTree(t *testing.T) {
	if testing.Short() {
		t.Skip("draws through the native layout service")
	}
	f := newInspectorFixture(t, 20)
	f.toggleInspector(t)
	f.draw(t)
	published := gpui.InspectorTreeSnapshotOf(f.window)
	draws := gpui.WindowDrawCount(f.window)
	node := itemNodeAt(t, published, 50)
	gpui.InspectorSelect(f.window, node.ID)
	report, _ := gpui.InspectorSelectionOf(f.window)
	if !report.Present {
		t.Fatal("the selection must be live before the abandonment")
	}

	// The abandoned build: the view flips to the panicking child. The
	// panic propagates out of DrawWindowFrame (the build fails after
	// mutating app state).
	f.mutate(t, func(m *PanelModel, cx *gpui.Context[PanelModel]) {
		m.panicNextRender = true
	})
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("the panicking build did not propagate")
			}
		}()
		_, _ = gpui.DrawWindowFrame(f.window)
	}()
	if got := gpui.WindowDrawCount(f.window); got != draws {
		t.Errorf("draw count after the abandoned build = %d, want %d", got, draws)
	}
	if got := gpui.InspectorTreeSnapshotOf(f.window); got != published {
		t.Errorf("the abandoned build changed the published tree:\n%s", got.Trace())
	}
	// The selection still resolves against the preserved tree.
	report, _ = gpui.InspectorSelectionOf(f.window)
	if !report.Present || report.Frame != published.Frame() {
		t.Fatalf("the selection after the abandoned build = %+v, want the preserved frame's report", report)
	}
	// Diagnostics over the preserved tree stay answerable.
	if _, found := published.Node(node.ID); !found {
		t.Error("the preserved tree lost the selected node")
	}

	// Recovery: the next successful draw republishes.
	f.mutate(t, func(m *PanelModel, cx *gpui.Context[PanelModel]) {
		m.panicNextRender = false
	})
	f.draw(t)
	if got := gpui.InspectorTreeSnapshotOf(f.window); got == published || got.Frame() != draws+1 {
		t.Errorf("the recovery draw did not republish: frame=%d", got.Frame())
	}
}

// TestWindowClosureDuringInspectionDropsSelectionAndEntities pins the
// closure discipline: closing the window mid-inspection makes every
// inspector query answer dead/absent without panicking, and the
// inspector state's strong entity handle is dropped with the window —
// the diagnostics keep no strong cycle and revive nothing.
func TestWindowClosureDuringInspectionDropsSelectionAndEntities(t *testing.T) {
	if testing.Short() {
		t.Skip("draws through the native layout service")
	}
	f := newInspectorFixture(t, 20)
	f.toggleInspector(t)
	f.draw(t)
	// Select the header (its typed state holds the model handle).
	header := headerNode(t, gpui.InspectorTreeSnapshotOf(f.window))
	gpui.InspectorSelect(f.window, header.ID)
	f.draw(t)
	if _, _, ok := gpui.InspectorActiveElementState[HeldEntityState](f.window); !ok {
		t.Fatal("the header did not write the held-entity state")
	}
	// An extra window-owned entity created AFTER the snapshot: its ONLY
	// lease is the window's scope, so closing the window must release
	// it — if any diagnostic runtime retained it, the assert would list
	// it as a new leak.
	snapshot := f.app.LeakDetectorSnapshot()
	extra := gpui.NewEntity(f.app, f.window.Scope(), func(s *PanelModel, cx *gpui.Context[PanelModel]) {})

	// Close the window mid-inspection.
	gpui.CloseTestWindow(f.window)
	if _, ok := gpui.InspectorSelectionOf(f.window); ok {
		t.Error("the closed window's selection query must report no selection")
	}
	if got := gpui.InspectorTreeSnapshotOf(f.window); got != nil {
		t.Errorf("the closed window still publishes a tree: %s", got.Trace())
	}
	if f.window.IsInspectorPicking() {
		t.Error("the closed window must not pick")
	}
	// The strong-handle state is gone: the inspector dropped it with
	// the window (no strong cycle survives the teardown).
	f.testApp.Update(func(app *gpui.App) {})
	if lines := f.app.AssertNoNewLeaks(snapshot); len(lines) > 0 {
		t.Errorf("window closure leaked entities:\n%s", strings.Join(lines, "\n"))
	}
	if lines := f.app.AssertEntityReleased(extra.EntityID()); len(lines) > 0 {
		t.Errorf("the window-owned entity did not release with the window:\n%s", strings.Join(lines, "\n"))
	}
	// The model entity survives (its lease is the app root scope's, not
	// the window's) — the diagnostics revived nothing and killed
	// nothing it did not own.
	if !f.model.Downgrade().IsUpgradable() {
		t.Error("the root-owned model released with the window (the inspector must not own it)")
	}
}

// TestCloseWindowInspectorGenerationTeardown pins the explicit
// teardown: the generation bumps so an outstanding held snapshot's
// tokens cannot address a later inspector, and re-toggling creates a
// fresh runtime.
func TestCloseWindowInspectorGenerationTeardown(t *testing.T) {
	if testing.Short() {
		t.Skip("draws through the native layout service")
	}
	f := newInspectorFixture(t, 20)
	f.toggleInspector(t)
	f.draw(t)
	published := gpui.InspectorTreeSnapshotOf(f.window)
	node := itemNodeAt(t, published, 50)
	gpui.InspectorSelect(f.window, node.ID)

	gpui.CloseWindowInspector(f.window)
	if report, ok := gpui.InspectorSelectionOf(f.window); ok {
		t.Errorf("the closed inspector answered a selection: %+v", report)
	}
	// The held snapshot stays readable (a value snapshot), but the
	// window has no inspector: no revival through the old tree.
	if _, found := published.Node(node.ID); !found {
		t.Error("the held value snapshot lost its nodes (it must outlive the inspector)")
	}
	// Re-toggle: a fresh runtime with a fresh tree.
	f.window.ToggleInspector(f.app)
	f.draw(t)
	fresh := gpui.InspectorTreeSnapshotOf(f.window)
	if fresh == nil || fresh == published {
		t.Fatalf("the re-toggled inspector did not publish a fresh tree: %v", fresh)
	}
	if fresh.Generation() <= published.Generation() {
		t.Errorf("the fresh tree generation = %d, want > the torn-down %d", fresh.Generation(), published.Generation())
	}
}

// ---------------------------------------------------------------------------
// The leak / cycle detector over the real entity map
// ---------------------------------------------------------------------------

// TestLeakDetectorSnapshotAndAssert pins the leak detector over the
// real entity map: a snapshot ignores its own entities, new entities
// that release before the assert are clean, retained new entities are
// listed with their TYPE NAME and the port's allocation-context labels
// (the lease-owner scopes), and the report observes only.
func TestLeakDetectorSnapshotAndAssert(t *testing.T) {
	ta := gpui.NewTestApp()
	app := ta.App()
	window := gpui.NewTestWindow(app, gpui.Size{Width: 100, Height: 100})
	snapshot := app.LeakDetectorSnapshot()
	if snapshot.EntityCount() != 0 {
		t.Fatalf("a fresh app snapshot has %d entities, want 0", snapshot.EntityCount())
	}

	// A temporary entity that releases before the assert: clean (the
	// pin's test_leak_detector_snapshot_no_leaks).
	scope := app.RootScope().Child()
	gpui.NewEntity(app, scope, func(s *PanelModel, cx *gpui.Context[PanelModel]) {
		s.items = []string{"temporary"}
	})
	// Release the temporary entity with its owning scope (the
	// test_leak_detector_snapshot_no_leaks shape) and flush.
	scope.Close()
	ta.Update(func(app *gpui.App) {})
	if lines := app.AssertNoNewLeaks(snapshot); len(lines) > 0 {
		t.Errorf("the released temporary entity leaked:\n%s", strings.Join(lines, "\n"))
	}

	// A retained new entity: listed with its type name and labels.
	retained := gpui.NewEntity(app, window.Scope(), func(s *PanelModel, cx *gpui.Context[PanelModel]) {})
	lines := app.AssertNoNewLeaks(snapshot)
	if len(lines) == 0 {
		t.Fatal("the retained new entity was not reported")
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "new entity leaks detected since snapshot") {
		t.Errorf("the report header is missing:\n%s", joined)
	}
	if !strings.Contains(joined, "inspectorspec.PanelModel") {
		t.Errorf("the leak listing has no type name:\n%s", joined)
	}
	if !strings.Contains(joined, "lease(s) held by") || !strings.Contains(joined, "scope") {
		t.Errorf("the leak listing has no lease-owner labels:\n%s", joined)
	}
	// The assert observes only: the entity is untouched.
	if !retained.Downgrade().IsUpgradable() {
		t.Error("AssertNoNewLeaks mutated ownership")
	}
	// Releasing it clears the report (and the released-entity assert).
	retained.Release()
	ta.Update(func(app *gpui.App) {})
	if lines := app.AssertNoNewLeaks(snapshot); len(lines) > 0 {
		t.Errorf("the released entity still leaks:\n%s", strings.Join(lines, "\n"))
	}
	if lines := app.AssertEntityReleased(retained.EntityID()); len(lines) > 0 {
		t.Errorf("AssertEntityReleased on a released entity reported:\n%s", strings.Join(lines, "\n"))
	}
	// A live entity reports its leases.
	live := gpui.NewEntity(app, app.RootScope(), func(s *PanelModel, cx *gpui.Context[PanelModel]) {})
	if lines := app.AssertEntityReleased(live.EntityID()); len(lines) == 0 {
		t.Error("AssertEntityReleased on a live entity reported nothing")
	}
	if !strings.Contains(strings.Join(lines, "\n"), "inspectorspec.PanelModel") {
		t.Errorf("AssertEntityReleased listing has no type name:\n%s", strings.Join(lines, "\n"))
	}
	// A nil snapshot reports the misuse instead of passing silently.
	if lines := app.AssertNoNewLeaks(nil); len(lines) == 0 {
		t.Error("a nil snapshot must not assert clean")
	}
}

// TestLeakCycleLinesReportStrongCycle pins the cycle counterpart: a
// strong cycle among entities (each holding a lease in the other's
// dependent scope) is REPORTED, never collected, and the lines carry
// the semantic labels.
func TestLeakCycleLinesReportStrongCycle(t *testing.T) {
	ta := gpui.NewTestApp()
	app := ta.App()
	first := gpui.NewEntity(app, app.RootScope(), func(s *PanelModel, cx *gpui.Context[PanelModel]) {
		s.items = []string{"first"}
	})
	second := gpui.NewEntity(app, app.RootScope(), func(s *PanelModel, cx *gpui.Context[PanelModel]) {
		s.items = []string{"second"}
	})
	var depFirst, depSecond *gpui.Scope
	first.Update(app, func(s *PanelModel, cx *gpui.Context[PanelModel]) { depFirst = cx.Scope() })
	second.Update(app, func(s *PanelModel, cx *gpui.Context[PanelModel]) { depSecond = cx.Scope() })
	firstCycle := first.RetainInto(depSecond)
	secondCycle := second.RetainInto(depFirst)

	lines := app.LeakCycleLines()
	if len(lines) == 0 {
		t.Fatal("the strong cycle was not reported")
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "strong cycle") || !strings.Contains(joined, "break an edge explicitly") {
		t.Errorf("the cycle report lacks the pin's guidance:\n%s", joined)
	}
	// The cycle is not collected: both stay live.
	if !first.Downgrade().IsUpgradable() || !second.Downgrade().IsUpgradable() {
		t.Error("the cycle report collected the cycle")
	}
	// Breaking it explicitly clears the report.
	firstCycle.Release()
	secondCycle.Release()
	first.Release()
	second.Release()
	ta.Update(func(app *gpui.App) {})
	if lines := app.LeakCycleLines(); len(lines) > 0 {
		t.Errorf("the broken cycle still reports:\n%s", strings.Join(lines, "\n"))
	}
}

// ---------------------------------------------------------------------------
// The debug frame overlay (debug_overlay.rs)
// ---------------------------------------------------------------------------

// TestDebugOverlayModeCycleAndStats ports the pin's debug_overlay.rs
// unit tests: the mode cycle, the percentile readout lines, the stat
// reset semantics, the frame-count accumulation across mode changes
// and the five-digit saturation.
func TestDebugOverlayModeCycleAndStats(t *testing.T) {
	ta := gpui.NewTestApp()
	app := ta.App()
	window := gpui.NewTestWindow(app, gpui.Size{Width: 200, Height: 100})
	defer gpui.SetDiagnosticCapability(gpui.CapFrameOverlay, false)
	gpui.SetDiagnosticCapability(gpui.CapFrameOverlay, true)

	// The mode cycle: Hidden -> Minimal -> Full -> Hidden
	// (DebugFrameOverlayMode::next).
	if got := gpui.DebugOverlayHidden.Next(); got != gpui.DebugOverlayMinimal {
		t.Errorf("Hidden.Next() = %s, want Minimal", got)
	}
	if got := gpui.DebugOverlayMinimal.Next(); got != gpui.DebugOverlayFull {
		t.Errorf("Minimal.Next() = %s, want Full", got)
	}
	if got := gpui.DebugOverlayFull.Next(); got != gpui.DebugOverlayHidden {
		t.Errorf("Full.Next() = %s, want Hidden", got)
	}
	if got := gpui.DebugFrameOverlayModeOf(window); got != gpui.DebugOverlayHidden {
		t.Errorf("the default mode = %s, want Hidden", got)
	}
	gpui.CycleDebugFrameOverlayMode(window)
	if got := gpui.DebugFrameOverlayModeOf(window); got != gpui.DebugOverlayMinimal {
		t.Errorf("after one cycle the mode = %s, want Minimal", got)
	}

	// percentile_lows_are_reported_as_times: 1..=100ms samples.
	gpui.SetDebugFrameOverlayMode(window, gpui.DebugOverlayFull)
	for ms := 1; ms <= 100; ms++ {
		gpui.RecordDebugFrame(window, time.Duration(ms)*time.Millisecond)
	}
	_, _, lines := gpui.DebugFrameOverlayStats(window)
	want := []string{
		"CUR 100.0 MS",
		"1%   99.0 MS",
		"10%  90.0 MS",
		"MAX 100.0 MS",
		"FRAMES   100",
	}
	if len(lines) != len(want) {
		t.Fatalf("full-mode lines = %v, want %v", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}

	// reset_clears_durations_but_keeps_frame_count.
	gpui.ResetDebugFrameOverlayStats(window)
	samples, frames, lines := gpui.DebugFrameOverlayStats(window)
	if samples != 0 || frames != 100 {
		t.Fatalf("after reset: samples=%d frames=%d, want 0/100", samples, frames)
	}
	if lines[0] != "CUR    -- MS" || lines[3] != "MAX    -- MS" || lines[4] != "FRAMES   100" {
		t.Errorf("after reset the lines = %v", lines)
	}
	gpui.RecordDebugFrame(window, 20*time.Millisecond)
	_, frames, lines = gpui.DebugFrameOverlayStats(window)
	if lines[0] != "CUR  20.0 MS" || lines[4] != "FRAMES   101" {
		t.Errorf("after the post-reset record the lines = %v (frames=%d)", lines, frames)
	}

	// frame_count_accumulates_across_mode_changes (and Minimal shows
	// only the current duration).
	gpui.SetDebugFrameOverlayMode(window, gpui.DebugOverlayMinimal)
	gpui.RecordDebugFrame(window, 10*time.Millisecond)
	if _, _, lines := gpui.DebugFrameOverlayStats(window); len(lines) != 1 || lines[0] != " 10.0 MS" {
		t.Errorf("minimal lines = %v, want [\" 10.0 MS\"]", lines)
	}
	gpui.SetDebugFrameOverlayMode(window, gpui.DebugOverlayFull)
	gpui.RecordDebugFrame(window, 10*time.Millisecond)
	if _, _, lines := gpui.DebugFrameOverlayStats(window); lines[4] != "FRAMES   103" {
		t.Errorf("after mode changes the frame line = %q, want FRAMES   103", lines[4])
	}
	gpui.SetDebugFrameOverlayMode(window, gpui.DebugOverlayHidden)
	gpui.RecordDebugFrame(window, 10*time.Millisecond)
	gpui.SetDebugFrameOverlayMode(window, gpui.DebugOverlayFull)
	if _, _, lines := gpui.DebugFrameOverlayStats(window); lines[4] != "FRAMES   104" {
		t.Errorf("hidden-mode records must still count: %q", lines[4])
	}

	// toggling_on_shows_previous_frame_immediately.
	other := gpui.NewTestWindow(app, gpui.Size{Width: 200, Height: 100})
	gpui.RecordDebugFrame(other, 10*time.Millisecond)
	gpui.SetDebugFrameOverlayMode(other, gpui.DebugOverlayMinimal)
	if _, _, lines := gpui.DebugFrameOverlayStats(other); len(lines) != 1 || lines[0] != " 10.0 MS" {
		t.Errorf("toggling on must show the previous frame: %v", lines)
	}
}

// TestDebugOverlayFrameCountSaturatesAtFiveDigits pins the frame
// count's column alignment and its LOTS saturation.
func TestDebugOverlayFrameCountSaturatesAtFiveDigits(t *testing.T) {
	ta := gpui.NewTestApp()
	app := ta.App()
	window := gpui.NewTestWindow(app, gpui.Size{Width: 200, Height: 100})
	defer gpui.SetDiagnosticCapability(gpui.CapFrameOverlay, false)
	gpui.SetDiagnosticCapability(gpui.CapFrameOverlay, true)
	gpui.SetDebugFrameOverlayMode(window, gpui.DebugOverlayFull)

	gpui.RecordDebugFrame(window, 10*time.Millisecond)
	if _, _, lines := gpui.DebugFrameOverlayStats(window); lines[4] != "FRAMES     1" {
		t.Errorf("frame line = %q, want FRAMES     1", lines[4])
	}
	for i := 0; i < 99_998; i++ {
		gpui.RecordDebugFrame(window, time.Millisecond)
	}
	if _, _, lines := gpui.DebugFrameOverlayStats(window); lines[4] != "FRAMES 99999" {
		t.Errorf("frame line at 99,999 = %q", lines[4])
	}
	gpui.RecordDebugFrame(window, time.Millisecond)
	if _, _, lines := gpui.DebugFrameOverlayStats(window); lines[4] != "FRAMES  LOTS" {
		t.Errorf("frame line at 100,000 = %q, want the LOTS saturation", lines[4])
	}
}

// TestDebugOverlayEveryRenderedCharacterHasAGlyph ports the pin's
// coverage test: every character the readouts can produce must have a
// glyph, or it would silently render as blank space.
func TestDebugOverlayEveryRenderedCharacterHasAGlyph(t *testing.T) {
	ta := gpui.NewTestApp()
	app := ta.App()
	window := gpui.NewTestWindow(app, gpui.Size{Width: 200, Height: 100})
	defer gpui.SetDiagnosticCapability(gpui.CapFrameOverlay, false)
	gpui.SetDiagnosticCapability(gpui.CapFrameOverlay, true)
	gpui.SetDebugFrameOverlayMode(window, gpui.DebugOverlayFull)

	var lines []string
	for _, sample := range []time.Duration{
		0,
		time.Microsecond,
		8333 * time.Microsecond,
		123 * time.Millisecond,
		2 * time.Second,
	} {
		gpui.RecordDebugFrame(window, sample)
		_, _, readout := gpui.DebugFrameOverlayStats(window)
		lines = append(lines, readout...)
	}
	for i := 0; i < 99_999; i++ {
		gpui.RecordDebugFrame(window, time.Millisecond)
	}
	_, _, readout := gpui.DebugFrameOverlayStats(window)
	lines = append(lines, readout...)
	// An enabled overlay with no samples renders placeholders.
	empty := gpui.NewTestWindow(app, gpui.Size{Width: 200, Height: 100})
	gpui.SetDebugFrameOverlayMode(empty, gpui.DebugOverlayFull)
	_, _, readout = gpui.DebugFrameOverlayStats(empty)
	lines = append(lines, readout...)

	for _, line := range lines {
		for _, character := range line {
			if character == ' ' {
				continue
			}
			if !overlayGlyphDefined(byte(character)) {
				t.Errorf("no glyph for %q in line %q", character, line)
			}
		}
	}
}

// TestOverlayPaintsIntoSceneAndReleaseIsInert pins the overlay's scene
// integration: with the capability and a mode on, a drawn frame gains
// the panel and glyph quads; the release profile (capability off)
// paints nothing and records nothing.
func TestOverlayPaintsIntoSceneAndReleaseIsInert(t *testing.T) {
	if testing.Short() {
		t.Skip("draws through the native layout service")
	}
	ta := gpui.NewTestApp()
	app := ta.App()
	window := gpui.NewTestWindow(app, gpui.Size{Width: 200, Height: 100})
	window.SetRootView(&probeView{build: func() gpui.AnyElement { return gpui.Div().Child("stable").IntoElement() }})

	baseline, err := gpui.DrawWindowFrame(window)
	if err != nil {
		t.Fatalf("baseline draw: %v", err)
	}
	baselineQuads, err := baseline.Quads()
	if err != nil {
		t.Fatalf("baseline quads: %v", err)
	}

	// The overlay capability + Minimal mode: the next frame paints the
	// panel and the current-time glyph run.
	gpui.SetDiagnosticCapability(gpui.CapFrameOverlay, true)
	defer gpui.SetDiagnosticCapability(gpui.CapFrameOverlay, false)
	gpui.RecordDebugFrame(window, 12345*time.Microsecond)
	gpui.SetDebugFrameOverlayMode(window, gpui.DebugOverlayMinimal)
	overlay, err := gpui.DrawWindowFrame(window)
	if err != nil {
		t.Fatalf("overlay draw: %v", err)
	}
	quads, err := overlay.Quads()
	if err != nil {
		t.Fatalf("overlay quads: %v", err)
	}
	if len(quads) <= len(baselineQuads) {
		t.Fatalf("the overlay frame added no quads: %d vs the baseline %d", len(quads), len(baselineQuads))
	}
	// The stats recorded the draw (the profiler call site: one sample
	// per completed draw).
	samples, frames, _ := gpui.DebugFrameOverlayStats(window)
	if samples == 0 || frames == 0 {
		t.Fatalf("the draw recorded no overlay stats: samples=%d frames=%d", samples, frames)
	}

	// The release profile: the overlay never paints or records.
	gpui.SetDiagnosticCapability(gpui.CapFrameOverlay, false)
	samplesBefore, _, _ := gpui.DebugFrameOverlayStats(window)
	release, err := gpui.DrawWindowFrame(window)
	if err != nil {
		t.Fatalf("release draw: %v", err)
	}
	releaseQuads, err := release.Quads()
	if err != nil {
		t.Fatalf("release quads: %v", err)
	}
	if len(releaseQuads) != len(baselineQuads) {
		t.Errorf("the release-profile frame painted overlay quads: %d vs the baseline %d", len(releaseQuads), len(baselineQuads))
	}
	samplesAfter, _, _ := gpui.DebugFrameOverlayStats(window)
	if samplesAfter != samplesBefore {
		t.Errorf("the release draw recorded a sample: samples %d -> %d", samplesBefore, samplesAfter)
	}
}

// overlayGlyphDefined reports whether the overlay's glyph table covers
// a character (the test-side mirror of gpui's table).
func overlayGlyphDefined(character byte) bool {
	switch character {
	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9',
		'.', '-', '%',
		'A', 'C', 'E', 'F', 'L', 'M', 'N', 'O', 'R', 'S', 'T', 'U', 'X':
		return true
	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// The inspector panel (draw_roots integration)
// ---------------------------------------------------------------------------

// TestInspectorPanelShrinksRootAndRendersElement pins the draw_roots
// integration: while the inspector is on, the root's width shrinks by
// the 30rem panel (the container's bounds prove it), and an installed
// renderer's element renders into the panel region.
func TestInspectorPanelShrinksRootAndRendersElement(t *testing.T) {
	if testing.Short() {
		t.Skip("draws through the native layout service")
	}
	f := newInspectorFixture(t, 20)

	// Install an inspector renderer (set_inspector_renderer) whose
	// element records that it rendered.
	rendered := 0
	gpui.SetInspectorRenderer(f.app, func(w *gpui.Window, app *gpui.App) gpui.AnyElement {
		rendered++
		return gpui.Div().Child("inspector").IntoElement()
	})
	defer gpui.SetInspectorRenderer(f.app, nil)

	f.toggleInspector(t)
	f.draw(t)
	onWidth := itemNodeAt(t, gpui.InspectorTreeSnapshotOf(f.window), 50).Bounds.Size.Width
	// The corpus window is 800 logical px wide; the 30rem panel (480
	// logical px at rem 16) shrinks the root to 320 (window.rs
	// 3403-3415: (size.width - inspector_width).max(px(0))).
	if onWidth != 800-480 {
		t.Errorf("the inspector-on item width = %v, want 320 (the 800px window minus the 30rem panel)", onWidth)
	}
	if rendered == 0 {
		t.Error("the inspector renderer never ran")
	}
	// The root's available width never goes negative: a narrow window
	// clamps at zero (window.rs's (size.width - width).max(px(0.))).
	narrow := gpui.NewTestWindow(f.app, gpui.Size{Width: 200, Height: 300})
	narrow.SetRootView(&panelView{model: f.model})
	narrow.ToggleInspector(f.app)
	if _, err := gpui.DrawWindowFrame(narrow); err != nil {
		t.Fatalf("narrow draw: %v", err)
	}
	if got := gpui.InspectorTreeSnapshotOf(narrow).Nodes()[0].Bounds.Size.Width; got != 0 {
		t.Errorf("the narrow window's root width = %v, want 0 (the panel exceeds the viewport)", got)
	}
	gpui.CloseTestWindow(narrow)
}

// TestRenderInspectorStatesAndRegistry pins the element-state registry
// (app.rs register_inspector_element + inspector.rs
// render_inspector_states): the registered renderer for the active
// element's state type builds its element, in deterministic type
// order, only while a selection with that state exists.
func TestRenderInspectorStatesAndRegistry(t *testing.T) {
	if testing.Short() {
		t.Skip("draws through the native layout service")
	}
	f := newInspectorFixture(t, 20)
	f.toggleInspector(t)
	f.draw(t)
	node := itemNodeAt(t, gpui.InspectorTreeSnapshotOf(f.window), 50)

	renderedStates := map[string]int{}
	gpui.RegisterInspectorElement(f.app, func(id gpui.InspectorElementID, state *ItemInspectorState, w *gpui.Window, app *gpui.App) gpui.AnyElement {
		renderedStates[state.Kind]++
		return gpui.Text("state: " + state.Kind)
	})
	// Without a selection there is nothing to render.
	if elements := gpui.RenderInspectorStates(f.window, f.app); len(elements) != 0 {
		t.Fatalf("RenderInspectorStates without a selection returned %d elements", len(elements))
	}
	gpui.InspectorSelect(f.window, node.ID)
	f.draw(t) // the selected item writes its typed state while live
	elements := gpui.RenderInspectorStates(f.window, f.app)
	if len(elements) != 1 {
		t.Fatalf("RenderInspectorStates returned %d elements, want 1 (the item's state)", len(elements))
	}
	if renderedStates["item-1"] != 1 {
		t.Errorf("the state renderer ran %d times for item-1, want 1", renderedStates["item-1"])
	}
}

// TestToggleInspectorLifecycle pins the toggle lifecycle: the second
// toggle drops the state (the published tree, the selection and the
// states go), and the picking query stays answerable.
func TestToggleInspectorLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("draws through the native layout service")
	}
	f := newInspectorFixture(t, 20)
	f.toggleInspector(t)
	f.draw(t)
	node := itemNodeAt(t, gpui.InspectorTreeSnapshotOf(f.window), 50)
	gpui.InspectorSelect(f.window, node.ID)

	f.toggleInspector(t) // off
	if f.window.IsInspectorPicking() {
		t.Error("the toggled-off inspector must not pick")
	}
	if got := gpui.InspectorTreeSnapshotOf(f.window); got != nil {
		t.Errorf("the toggled-off inspector kept a tree: %s", got.Trace())
	}
	if _, ok := gpui.InspectorSelectionOf(f.window); ok {
		t.Error("the toggled-off inspector kept a selection")
	}
	if _, _, ok := gpui.InspectorActiveElementState[ItemInspectorState](f.window); ok {
		t.Error("the toggled-off inspector kept the typed states")
	}
	f.draw(t)
	if got := gpui.InspectorTreeSnapshotOf(f.window); got != nil {
		t.Errorf("a frame drawn with the inspector off published a tree: %s", got.Trace())
	}
	// Toggling back on re-enters picking with a fresh runtime.
	f.toggleInspector(t)
	if !f.window.IsInspectorPicking() {
		t.Error("re-toggling on must enter picking (Inspector::new)")
	}
	f.draw(t)
	if got := gpui.InspectorTreeSnapshotOf(f.window); got == nil || got.Count() == 0 {
		t.Error("re-toggling on did not record a tree")
	}
}

// ---------------------------------------------------------------------------
// The dispatch wiring (window.rs dispatch_mouse_event 6088-6092)
// ---------------------------------------------------------------------------

// listenerElement is a minimal element that registers one frame-scoped
// mouse listener and a hitbox during prepaint: the integration probe
// for the inspector's dispatch seam.
type listenerElement struct {
	runs *int
}

var listenerSource = gpui.SourceLocation{File: "inspectorspec/inspector_test.go", Line: 620}

// listenerView is the minimal stateless root view carrying the
// listener element.
type listenerView struct {
	element *listenerElement
}

func (v *listenerView) EntityID() (gpui.EntityID, bool) { return 0, false }

func (v *listenerView) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return gpui.CustomElement(v.element)
}

func (e *listenerElement) ID() (gpui.ElementID, bool) {
	return gpui.NameElementID("listener"), true
}

func (e *listenerElement) SourceLocation() *gpui.SourceLocation {
	location := listenerSource
	return &location
}

func (e *listenerElement) RequestLayout(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, w *gpui.Window, app *gpui.App) (gpui.LayoutID, struct{}) {
	style := gpui.DefaultStyle()
	style.Size.Width = gpui.Length{Definite: gpui.Fraction(1.0)}
	style.Size.Height = gpui.Length{Definite: gpui.Fraction(1.0)}
	id, err := gpui.RequestElementLayout(w, style)
	if err != nil {
		panic(fmt.Sprintf("inspectorspec: listener request_layout: %v", err))
	}
	return id, struct{}{}
}

func (e *listenerElement) Prepaint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *struct{}, w *gpui.Window, app *gpui.App) struct{} {
	box := w.InsertHitbox(bounds, gpui.HitboxNormal)
	w.InsertInspectorHitbox(box, inspector)
	w.OnMouseEvent(func(event any, phase gpui.DispatchPhase, w *gpui.Window, app *gpui.App) {
		if phase == gpui.DispatchBubble {
			*e.runs++
		}
	})
	return struct{}{}
}

func (e *listenerElement) Paint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *struct{}, prepaint *struct{}, w *gpui.Window, app *gpui.App) {
}

func (e *listenerElement) LayoutNodeStyle() (gpui.Style, bool) {
	style := gpui.DefaultStyle()
	style.Size.Width = gpui.Length{Definite: gpui.Fraction(1.0)}
	style.Size.Height = gpui.Length{Definite: gpui.Fraction(1.0)}
	return style, true
}

// TestDispatchInputRoutesThroughInspectorWhilePicking verifies the
// wiring window.rs pins at dispatch_mouse_event's head
// (handle_inspector_mouse_event first, all other mouse handling
// skipped while it reports picking): through the PUBLIC
// Window.DispatchInput entry, a mouse move with the inspector off
// reaches the frame's normal mouse listeners, and with picking active
// the same move is consumed by the inspector path (the hover updates)
// without ever running the normal listeners.
func TestDispatchInputRoutesThroughInspectorWhilePicking(t *testing.T) {
	if testing.Short() {
		t.Skip("draws through the native layout service")
	}
	testApp := gpui.NewTestApp()
	app := testApp.App()
	window := gpui.NewTestWindow(app, gpui.Size{Width: 800, Height: 300})
	defer gpui.CloseTestWindow(window)

	runs := 0
	window.SetRootView(&listenerView{element: &listenerElement{runs: &runs}})
	if _, err := gpui.DrawWindowFrame(window); err != nil {
		t.Fatalf("DrawWindowFrame (inspector off): %v", err)
	}
	window.DispatchInput(&gpui.MouseMoveEvent{Position: gpui.Point{X: 100, Y: 100}}, app)
	if runs != 1 {
		t.Fatalf("normal dispatch runs = %d, want 1 (the listener fired once on bubble)", runs)
	}

	// Picking on: redraw so the inspector hitboxes register, then the
	// same dispatch must be consumed by the inspector path.
	window.ToggleInspector(app)
	if !window.IsInspectorPicking() {
		t.Fatal("toggling the inspector on must enter picking mode")
	}
	if _, err := gpui.DrawWindowFrame(window); err != nil {
		t.Fatalf("DrawWindowFrame (picking): %v", err)
	}
	window.DispatchInput(&gpui.MouseMoveEvent{Position: gpui.Point{X: 100, Y: 100}}, app)
	if runs != 1 {
		t.Fatalf("listener runs while picking = %d, want 1 (the normal dispatch path was skipped)", runs)
	}
	if !window.IsInspectorPicking() {
		t.Fatal("the move must not leave picking mode")
	}
	if _, ok := gpui.InspectorActiveElement(window); !ok {
		t.Fatal("the move through DispatchInput did not hover anything (the inspector path did not run)")
	}

	// Picking off: the normal path serves the listener again.
	window.ToggleInspector(app)
	if _, err := gpui.DrawWindowFrame(window); err != nil {
		t.Fatalf("DrawWindowFrame (inspector off again): %v", err)
	}
	window.DispatchInput(&gpui.MouseMoveEvent{Position: gpui.Point{X: 100, Y: 100}}, app)
	if runs != 2 {
		t.Fatalf("normal dispatch runs after toggling off = %d, want 2", runs)
	}
}
