package base

// Port of U:dock/dock_area.rs: the dock area — the trees, the entity cache
// that mirrors them, and the reconciliation that keeps the two in step.
//
// Go adaptations:
//   - DockEvent is a sealed interface over one struct per variant.
//   - Cached container entities are owned by the area's dependent scope and
//     released, with their subscriptions cancelled, when the cache drops
//     them (the Rust drop of Cached / CachedSplit). Placeholder panels the
//     area builds are owned the same way; every other panel handle stays
//     owned by whoever created it.
//   - The docks are walked in the fixed order left, bottom, right where
//     upstream iterates a HashMap.
//   - The renderer trait's default methods are optional interfaces.

import (
	"slices"

	"github.com/hoangvu12/gpui-go/gpui"
)

// DockEvent is what the dock area reports outward (U:dock/dock_area.rs:38-45).
type DockEvent interface{ isDockEvent() }

// DockEventLayoutChanged is DockEvent::LayoutChanged: subscribe to persist
// the layout; it fires on every edit, so a writer should debounce.
type DockEventLayoutChanged struct{}

// DockEventDragDrop is DockEvent::DragDrop: a host-owned drag item was
// dropped inside the dock.
type DockEventDragDrop struct {
	Item   AnyDrag
	Target DropTarget
}

func (DockEventLayoutChanged) isDockEvent() {}
func (DockEventDragDrop) isDockEvent()      {}

// DockEvents is the DockArea event descriptor
// (U:dock/dock_area.rs:1456 impl EventEmitter<DockEvent>).
var DockEvents = gpui.DefineEvent[DockArea, DockEvent]()

// dockRegion is one dock: its own tree plus its runtime state
// (U:dock/dock_area.rs:62-66).
type dockRegion struct {
	tree PaneTree
	dock Dock
}

// cachedGroup is a cached group entity with the subscription that carries
// its intents back (U:dock/dock_area.rs:68-74).
type cachedGroup struct {
	entity       gpui.Entity[TabGroup]
	subscription gpui.Subscription
}

// cachedSplit is a split's cached ResizableState, plus the child order and
// tree sizes that state last mirrored (U:dock/dock_area.rs:76-93).
type cachedSplit struct {
	entity       gpui.Entity[ResizableState]
	children     []NodeID
	sizes        []*float32
	subscription gpui.Subscription
}

// dockOrder is the fixed walk over the docks.
var dockOrder = [...]DockPlacement{DockPlacementLeft, DockPlacementBottom, DockPlacementRight}

// DockArea is the main area of the dock (U:dock/dock_area.rs:95-116). It
// owns one PaneTree per region and the entity cache that mirrors them.
type DockArea struct {
	id      string
	version *uint64
	bounds  gpui.Bounds
	this    gpui.WeakEntity[DockArea]

	center PaneTree
	docks  map[DockPlacement]*dockRegion

	groups map[NodeID]*cachedGroup
	splits map[NodeID]*cachedSplit
	panels map[PanelID]PanelView
	// owned releases the panels this area built itself (placeholders).
	owned map[PanelID]func()

	locked bool
	// zoomed is the tab group filling the whole area (the upstream
	// Option<Zoomed>, whose only variant is Zoomed::Group).
	zoomed      *NodeID
	focusHandle gpui.FocusHandle
	renderer    DockAreaRenderer
}

// NewDockArea is DockArea::new (U:dock/dock_area.rs:118-149): an empty area
// that draws nothing but its panels. version is written into Dump and read
// back by Load. Build it in place: *a = NewDockArea(id, nil, w, cx).
func NewDockArea(id string, version *uint64, _ *gpui.Window, cx *gpui.Context[DockArea]) DockArea {
	panelRegistryInit(cx.App())
	return DockArea{
		id:          id,
		version:     version,
		this:        cx.Entity().Downgrade(),
		center:      NewPaneTree(RootKindSplit),
		docks:       make(map[DockPlacement]*dockRegion),
		groups:      make(map[NodeID]*cachedGroup),
		splits:      make(map[NodeID]*cachedSplit),
		panels:      make(map[PanelID]PanelView),
		owned:       make(map[PanelID]func()),
		focusHandle: cx.App().FocusHandle(),
		renderer:    bareDockArea{},
	}
}

// WithRenderer installs the appearance for this area and everything under
// it (U:dock/dock_area.rs:151-157).
func (a *DockArea) WithRenderer(renderer DockAreaRenderer) *DockArea {
	a.renderer = renderer
	return a
}

// ID is U:dock/dock_area.rs:159-161.
func (a *DockArea) ID() string { return a.id }

// Version is U:dock/dock_area.rs:163-165.
func (a *DockArea) Version() *uint64 { return a.version }

// SetVersion changes the schema version a later Dump writes
// (U:dock/dock_area.rs:167-175).
func (a *DockArea) SetVersion(version *uint64, cx *gpui.Context[DockArea]) {
	a.version = version
	cx.Notify()
}

// Bounds is the area's own bounds, recorded each frame
// (U:dock/dock_area.rs:177-181).
func (a *DockArea) Bounds() gpui.Bounds { return a.bounds }

// Layout returns the tree for one region, or false for a dock that does
// not exist (U:dock/dock_area.rs:183-194).
func (a *DockArea) Layout(placement DockPlacement) (*PaneTree, bool) {
	if placement == DockPlacementCenter {
		return &a.center, true
	}
	if region, ok := a.docks[placement]; ok {
		return &region.tree, true
	}
	return nil, false
}

// Panel returns the live view for a panel, if the dock still holds it
// (U:dock/dock_area.rs:196-199).
func (a *DockArea) Panel(panel PanelID) (PanelView, bool) {
	view, ok := a.panels[panel]
	return view, ok
}

// IsLocked is U:dock/dock_area.rs:201-203.
func (a *DockArea) IsLocked() bool { return a.locked }

// IsEmpty reports whether a region holds no visible panel; a region that
// does not exist is empty too (U:dock/dock_area.rs:205-214).
func (a *DockArea) IsEmpty(placement DockPlacement, app *gpui.App) bool {
	tree, ok := a.Layout(placement)
	return !ok || !a.isNodeVisible(tree.Root(), app)
}

// SetLocked locks the layout against rearranging; resizing stays available
// (U:dock/dock_area.rs:216-225).
func (a *DockArea) SetLocked(locked bool, w *gpui.Window, cx *gpui.Context[DockArea]) {
	if a.locked == locked {
		return
	}
	a.locked = locked
	a.reconcile(w, cx)
}

// SetCenter replaces the center region with a described layout
// (U:dock/dock_area.rs:229-238). Whatever was there leaves the dock.
func (a *DockArea) SetCenter(layout *DockLayout, w *gpui.Window, cx *gpui.Context[DockArea]) {
	tree, panels := paneTreeFromLayout(layout, RootKindSplit)
	a.center = tree
	a.registerPanels(panels)
	a.reconcile(w, cx)
	DockEvents.Emit(cx, DockEventLayoutChanged{})
}

func (a *DockArea) registerPanels(panels []dockLayoutPanel) {
	for _, p := range panels {
		a.panels[p.id] = p.view
	}
}

// SetDock replaces one dock with a described layout, creating it if needed
// and keeping the size and open state of the one it replaces
// (U:dock/dock_area.rs:240-267). The center defers to SetCenter.
func (a *DockArea) SetDock(placement DockPlacement, layout *DockLayout, w *gpui.Window, cx *gpui.Context[DockArea]) {
	if placement == DockPlacementCenter {
		a.SetCenter(layout, w, cx)
		return
	}
	tree, panels := paneTreeFromLayout(layout, RootKindAny)
	dock := NewDock(PanelMinSize * 2)
	if region, ok := a.docks[placement]; ok {
		dock = region.dock
	}
	a.docks[placement] = &dockRegion{tree: tree, dock: dock}
	a.registerPanels(panels)
	a.reconcile(w, cx)
	DockEvents.Emit(cx, DockEventLayoutChanged{})
}

// RemoveDock takes a dock away entirely, panels and all
// (U:dock/dock_area.rs:269-284).
func (a *DockArea) RemoveDock(placement DockPlacement, w *gpui.Window, cx *gpui.Context[DockArea]) {
	if _, ok := a.docks[placement]; !ok {
		return
	}
	delete(a.docks, placement)
	a.reconcile(w, cx)
	DockEvents.Emit(cx, DockEventLayoutChanged{})
}

// HasDock is U:dock/dock_area.rs:286-288.
func (a *DockArea) HasDock(placement DockPlacement) bool {
	_, ok := a.docks[placement]
	return ok
}

// IsDockOpen reports whether a dock is on screen; a missing dock never is
// (U:dock/dock_area.rs:290-297).
func (a *DockArea) IsDockOpen(placement DockPlacement) bool {
	region, ok := a.docks[placement]
	return ok && region.dock.IsOpen()
}

// ToggleDock opens a closed dock or closes an open one; a dock that is not
// collapsible refuses to close (U:dock/dock_area.rs:299-320).
func (a *DockArea) ToggleDock(placement DockPlacement, w *gpui.Window, cx *gpui.Context[DockArea]) {
	region, ok := a.docks[placement]
	if !ok {
		return
	}
	if !region.dock.IsCollapsible() && region.dock.IsOpen() {
		return
	}
	region.dock.SetOpen(!region.dock.IsOpen())
	// A closed dock takes its displayed panel off screen, which
	// TabGroupConstraints.Collapsed carries.
	a.reconcile(w, cx)
	DockEvents.Emit(cx, DockEventLayoutChanged{})
}

// IsDockCollapsible is U:dock/dock_area.rs:322-328.
func (a *DockArea) IsDockCollapsible(placement DockPlacement) bool {
	region, ok := a.docks[placement]
	return ok && region.dock.IsCollapsible()
}

// SetDockCollapsible is U:dock/dock_area.rs:330-341.
func (a *DockArea) SetDockCollapsible(placement DockPlacement, collapsible bool, _ *gpui.Window, cx *gpui.Context[DockArea]) {
	if region, ok := a.docks[placement]; ok {
		region.dock.SetCollapsible(collapsible)
		cx.Notify()
	}
}

// DockSize is the dock's size along its axis (U:dock/dock_area.rs:343-346).
func (a *DockArea) DockSize(placement DockPlacement) (float32, bool) {
	region, ok := a.docks[placement]
	if !ok {
		return 0, false
	}
	return region.dock.Size(), true
}

// SetDockSize is U:dock/dock_area.rs:348-364: only an effective change
// notifies and reports LayoutChanged.
func (a *DockArea) SetDockSize(placement DockPlacement, size float32, _ *gpui.Window, cx *gpui.Context[DockArea]) {
	region, ok := a.docks[placement]
	if !ok {
		return
	}
	previous := region.dock.Size()
	region.dock.SetSize(size)
	if region.dock.Size() == previous {
		return
	}
	cx.Notify()
	DockEvents.Emit(cx, DockEventLayoutChanged{})
}

// AddPanel adds a panel to a region, merging it into the first tab group
// there or starting a group when the region is empty
// (U:dock/dock_area.rs:369-381).
func (a *DockArea) AddPanel[S any, PS interface {
	*S
	Panel[S]
}](panel gpui.Entity[S], placement DockPlacement, size *float32, w *gpui.Window, cx *gpui.Context[DockArea]) {
	a.addPanelInner(PanelIDOf(panel.EntityID()), PanelViewOf[S, PS](panel), placement, size, w, cx)
}

// AddPanelView adds an already-wrapped panel handle to a region
// (U:dock/dock_area.rs:383-399).
func (a *DockArea) AddPanelView(panel PanelView, placement DockPlacement, size *float32, w *gpui.Window, cx *gpui.Context[DockArea]) {
	a.addPanelInner(panel.PanelID(cx.App()), panel, placement, size, w, cx)
}

// addPanelInner is U:dock/dock_area.rs:401-457.
func (a *DockArea) addPanelInner(id PanelID, panel PanelView, placement DockPlacement, size *float32, w *gpui.Window, cx *gpui.Context[DockArea]) {
	previous, hadPrevious := a.panels[id]
	a.panels[id] = panel

	// A dock is created to hold the panel; size seeds it.
	if placement != DockPlacementCenter && !a.HasDock(placement) {
		initial := PanelMinSize * 2
		if size != nil {
			initial = *size
		}
		a.docks[placement] = &dockRegion{tree: NewPaneTree(RootKindAny), dock: NewDock(initial)}
	}
	tree, ok := a.Layout(placement)
	if !ok {
		a.restoreRegistration(id, previous, hadPrevious)
		return
	}
	var target InsertTarget
	if node, ok := firstTabGroup(tree.Root()); ok {
		target = InsertTargetTabs{Node: node, Activate: true}
	} else {
		// An empty region has no container to merge into, so the panel
		// makes one beside the root.
		target = InsertTargetSplit{Node: tree.Root().ID(), Placement: PlacementRight, Size: size}
	}
	result := tree.InsertPanel(id, target)
	if !result.Changed() {
		a.restoreRegistration(id, previous, hadPrevious)
		return
	}
	a.commit(result, w, cx)
}

// restoreRegistration puts the view map back the way an add found it
// (U:dock/dock_area.rs:459-466).
func (a *DockArea) restoreRegistration(id PanelID, previous PanelView, hadPrevious bool) {
	if hadPrevious {
		a.panels[id] = previous
	} else {
		delete(a.panels, id)
	}
}

// RemovePanel removes a panel from wherever it lives, telling it that it
// was removed (U:dock/dock_area.rs:468-476).
func (a *DockArea) RemovePanel[S any](panel gpui.Entity[S], w *gpui.Window, cx *gpui.Context[DockArea]) {
	a.removePanelID(PanelIDOf(panel.EntityID()), w, cx)
}

// MovePanel moves a panel to a new home; it never leaves the dock, so it is
// never told it was removed (U:dock/dock_area.rs:478-553).
func (a *DockArea) MovePanel(panel PanelID, target InsertTarget, w *gpui.Window, cx *gpui.Context[DockArea]) {
	// A panel this area does not own has no backing entity here.
	if _, ok := a.Panel(panel); !ok {
		return
	}
	destination, ok := a.placementOfNode(target.targetNode())
	if !ok {
		return
	}
	source, hasSource := a.placementOfPanel(panel)

	// A split target divides an existing slot, so the tree needs real
	// pixels to divide.
	if _, ok := target.(InsertTargetSplit); ok {
		a.adoptMeasuredSizes(destination, cx.App())
	}

	// Read what the source group last told the panel before the edit, so
	// the destination can be seeded with it.
	wasActive, hadActive := false, false
	lookup := destination
	if hasSource {
		lookup = source
	}
	if tree, ok := a.Layout(lookup); ok {
		if node, ok := tree.FindPanelNode(panel); ok {
			if cached, ok := a.groups[node]; ok {
				last := cached.entity.Read(cx, func(g *TabGroup, _ *gpui.App) [2]bool {
					active, known := g.lastNotifiedActive(panel)
					return [2]bool{active, known}
				})
				wasActive, hadActive = last[0], last[1]
			}
		}
	}

	var changed bool
	if hasSource && source == destination {
		tree, ok := a.Layout(destination)
		if !ok {
			return
		}
		changed = tree.MovePanel(panel, target).Changed()
	} else {
		// Across trees a move is a detach plus an insert; both halves are
		// committed on.
		detached := false
		if hasSource {
			if tree, ok := a.Layout(source); ok {
				detached = tree.RemovePanel(panel).Changed()
			}
		}
		tree, ok := a.Layout(destination)
		if !ok {
			return
		}
		inserted := tree.InsertPanel(panel, target).Changed()
		changed = detached || inserted
	}

	a.commitChanged(changed, w, cx)

	if hadActive {
		if tree, ok := a.Layout(destination); ok {
			if node, ok := tree.FindPanelNode(panel); ok {
				if cached, ok := a.groups[node]; ok {
					cached.entity.Update(cx, func(g *TabGroup, _ *gpui.Context[TabGroup]) { g.seedActive(panel, wasActive) })
				}
			}
		}
	}
}

// SelectPanel displays panel in the tab group that holds it, wherever that
// is (U:dock/dock_area.rs:555-582). Nothing happens for a panel the dock
// does not hold, or one already displayed.
func (a *DockArea) SelectPanel(panel PanelID, w *gpui.Window, cx *gpui.Context[DockArea]) {
	placement, ok := a.placementOfPanel(panel)
	if !ok {
		return
	}
	tree, ok := a.Layout(placement)
	if !ok {
		return
	}
	node, ok := tree.FindPanelNode(panel)
	if !ok {
		return
	}
	found, ok := tree.FindNode(node)
	if !ok || found.kind.split {
		return
	}
	ix := slices.Index(found.kind.panels, panel)
	if ix < 0 {
		return
	}
	a.commit(tree.SetActive(node, ix), w, cx)
}

// SetSplitSizes replaces the slot sizes of the split at node in place
// (U:dock/dock_area.rs:584-600).
func (a *DockArea) SetSplitSizes(node NodeID, sizes []float32, w *gpui.Window, cx *gpui.Context[DockArea]) {
	placement, ok := a.placementOfNode(node)
	if !ok {
		return
	}
	tree, ok := a.Layout(placement)
	if !ok {
		return
	}
	slots := make([]*float32, len(sizes))
	for i, size := range sizes {
		slots[i] = someF32(size)
	}
	a.commit(tree.SetSizes(node, slots), w, cx)
}

// SplitAt puts panel in a new tab group beside node
// (U:dock/dock_area.rs:602-620).
func (a *DockArea) SplitAt(node NodeID, panel PanelID, placement Placement, w *gpui.Window, cx *gpui.Context[DockArea]) {
	region, ok := a.placementOfNode(node)
	if !ok {
		return
	}
	a.adoptMeasuredSizes(region, cx.App())
	tree, ok := a.Layout(region)
	if !ok {
		return
	}
	a.commit(tree.Split(node, panel, placement, nil), w, cx)
}

// removePanelID is U:dock/dock_area.rs:622-631.
func (a *DockArea) removePanelID(panel PanelID, w *gpui.Window, cx *gpui.Context[DockArea]) {
	region, ok := a.placementOfPanel(panel)
	if !ok {
		return
	}
	tree, ok := a.Layout(region)
	if !ok {
		return
	}
	a.commit(tree.RemovePanel(panel), w, cx)
}

// SetZoomedIn zooms the tab group at node in, as if its own zoom control
// had been used (U:dock/dock_area.rs:644-651). Nothing happens for a node
// that is not a live tab group, or when the group refuses.
func (a *DockArea) SetZoomedIn(node NodeID, w *gpui.Window, cx *gpui.Context[DockArea]) {
	a.setZoom(&node, w, cx)
}

// SetZoomedOut clears the zoom, putting the zoomed container's own flag
// back with it (U:dock/dock_area.rs:653-660).
func (a *DockArea) SetZoomedOut(w *gpui.Window, cx *gpui.Context[DockArea]) { a.setZoom(nil, w, cx) }

// IsZoomed is U:dock/dock_area.rs:662-664.
func (a *DockArea) IsZoomed() bool { return a.zoomed != nil }

// ZoomedGroup is the tab group filling the area, if any
// (U:dock/dock_area.rs:666-672).
func (a *DockArea) ZoomedGroup() (NodeID, bool) {
	if a.zoomed == nil {
		return 0, false
	}
	return *a.zoomed, true
}

// setZoom is the one place zoomed is written (U:dock/dock_area.rs:674-695).
func (a *DockArea) setZoom(zoomed *NodeID, w *gpui.Window, cx *gpui.Context[DockArea]) {
	if (a.zoomed == nil && zoomed == nil) || (a.zoomed != nil && zoomed != nil && *a.zoomed == *zoomed) {
		return
	}
	if a.zoomed != nil {
		a.driveZoom(*a.zoomed, false, w, cx)
	}
	var accepted *NodeID
	if zoomed != nil && a.driveZoom(*zoomed, true, w, cx) {
		node := *zoomed
		accepted = &node
	}
	a.zoomed = accepted
	cx.Notify()
}

// driveZoom asks one container to zoom in or out and reports whether it now
// agrees (U:dock/dock_area.rs:697-720).
func (a *DockArea) driveZoom(node NodeID, zoomIn bool, w *gpui.Window, cx *gpui.Context[DockArea]) bool {
	cached, ok := a.groups[node]
	if !ok {
		return false
	}
	return cached.entity.UpdateWith(cx, func(g *TabGroup, gcx *gpui.Context[TabGroup]) bool {
		g.setZoomed(zoomIn, w, gcx)
		return g.IsZoomed() == zoomIn
	})
}

// zoomedView is the container that fills the area when something is
// zoomed (U:dock/dock_area.rs:722-730).
func (a *DockArea) zoomedView() (gpui.View, bool) {
	if a.zoomed == nil {
		return nil, false
	}
	cached, ok := a.groups[*a.zoomed]
	if !ok {
		return nil, false
	}
	return gpui.ViewOf[TabGroup](cached.entity), true
}

// Load reads a persisted layout, rebuilding every panel through the
// PanelRegistry (U:dock/dock_area.rs:735-795). A panel this build does not
// know becomes a placeholder that carries the original PanelState forward.
func (a *DockArea) Load(state DockAreaState, w *gpui.Window, cx *gpui.Context[DockArea]) error {
	a.version = state.Version
	a.zoomed = nil
	// Nothing in the old layout survives a load, so the caches are emptied
	// rather than reconciled. The outgoing panels stay in the view map so
	// reconcile prunes them and tells them they were removed.
	for node, cached := range a.groups {
		cached.subscription.Cancel()
		cached.entity.Release()
		delete(a.groups, node)
	}
	for node, cached := range a.splits {
		cached.subscription.Cancel()
		cached.entity.Release()
		delete(a.splits, node)
	}
	clear(a.docks)

	builder := &registryPanelBuilder{area: a, w: w, cx: cx}
	a.center = PaneTreeFromState(&state.Center, RootKindSplit, builder)
	for _, dockState := range []*DockState{state.LeftDock, state.RightDock, state.BottomDock} {
		if dockState == nil {
			continue
		}
		tree := PaneTreeFromState(dockState.Panel(), RootKindAny, builder)
		dock := NewDock(dockState.Size())
		dock.SetOpen(dockState.Open())
		a.docks[dockState.Placement()] = &dockRegion{tree: tree, dock: dock}
	}
	a.registerPanels(builder.built)
	a.reconcile(w, cx)
	DockEvents.Emit(cx, DockEventLayoutChanged{})
	return nil
}

// Dump writes the layout out (U:dock/dock_area.rs:797-827). Slot sizes are
// resolved to concrete pixels first: the split's measured size, then the
// tree's own size, then PanelMinSize.
func (a *DockArea) Dump(app *gpui.App) DockAreaState {
	source := livePanels{panels: a.panels, app: app}
	center := a.resolvedTree(&a.center, app)
	return DockAreaState{
		Version:    a.version,
		Center:     center.ToState(source),
		LeftDock:   a.dumpDock(DockPlacementLeft, source, app),
		RightDock:  a.dumpDock(DockPlacementRight, source, app),
		BottomDock: a.dumpDock(DockPlacementBottom, source, app),
	}
}

// dumpDock is U:dock/dock_area.rs:829-842.
func (a *DockArea) dumpDock(placement DockPlacement, source livePanels, app *gpui.App) *DockState {
	region, ok := a.docks[placement]
	if !ok {
		return nil
	}
	tree := a.resolvedTree(&region.tree, app)
	state := NewDockState(tree.ToState(source), placement, region.dock.Size(), region.dock.IsOpen())
	return &state
}

// resolvedTree is U:dock/dock_area.rs:844-848.
func (a *DockArea) resolvedTree(tree *PaneTree, app *gpui.App) PaneTree {
	out := tree.Clone()
	a.resolveSizes(&out.root, app)
	return out
}

// resolveSizes is U:dock/dock_area.rs:850-877: a zero measurement or stored
// value is as unsafe as an absent one.
func (a *DockArea) resolveSizes(node *PaneNode, app *gpui.App) {
	if !node.kind.split {
		return
	}
	var measured []float32
	if cached, ok := a.splits[node.id]; ok {
		measured = cached.entity.Read(app, func(s *ResizableState, _ *gpui.App) []float32 { return slices.Clone(s.Sizes()) })
	}
	for ix, size := range node.kind.sizes {
		resolved := PanelMinSize
		if size != nil && *size > 0 {
			resolved = *size
		}
		if ix < len(measured) && measured[ix] > 0 {
			resolved = measured[ix]
		}
		node.kind.sizes[ix] = someF32(resolved)
	}
	for i := range node.kind.children {
		a.resolveSizes(&node.kind.children[i], app)
	}
}

// commit applies one edit: brings the entity cache back in line and says
// so (U:dock/dock_area.rs:882-892). OnRemoved is fired by reconcile.
func (a *DockArea) commit(result EditResult, w *gpui.Window, cx *gpui.Context[DockArea]) {
	a.commitChanged(result.Changed(), w, cx)
}

// commitChanged is U:dock/dock_area.rs:894-902.
func (a *DockArea) commitChanged(changed bool, w *gpui.Window, cx *gpui.Context[DockArea]) {
	if !changed {
		return
	}
	a.reconcile(w, cx)
	DockEvents.Emit(cx, DockEventLayoutChanged{})
}

// containerPlan is one container's worth of reconciliation work,
// snapshotted out of the tree (U:dock/dock_area.rs:1519-1541).
type containerPlan struct {
	split    bool
	node     NodeID
	axis     gpui.Axis
	children []NodeID
	sizes    []*float32

	panels      []PanelID
	activeIx    int
	constraints TabGroupConstraints
}

// reconcile brings the entity cache in line with the trees
// (U:dock/dock_area.rs:904-1028). A steady-state pass creates and drops
// nothing.
func (a *DockArea) reconcile(w *gpui.Window, cx *gpui.Context[DockArea]) {
	var plans []containerPlan
	planTree(&a.center, false, a.locked, &plans)
	for _, placement := range dockOrder {
		if region, ok := a.docks[placement]; ok {
			planTree(&region.tree, !region.dock.IsOpen(), a.locked, &plans)
		}
	}

	liveNodes := make(map[NodeID]bool, len(plans))
	livePanels := make(map[PanelID]bool)
	for _, plan := range plans {
		liveNodes[plan.node] = true
		if plan.split {
			state := a.splitEntity(plan.node, cx)
			cached := a.splits[plan.node]
			previous, adopted := slices.Clone(cached.children), slices.Clone(cached.sizes)
			// Only a split the edit changed is handed anything: re-adopting
			// an unchanged tree would move slots the edit never named.
			if !slices.Equal(previous, plan.children) || !optPixelsSliceEqual(adopted, plan.sizes) {
				state.Update(cx, func(s *ResizableState, scx *gpui.Context[ResizableState]) {
					syncSplitPanels(s, previous, plan.children, plan.sizes, scx)
					s.syncPanelsCount(plan.axis, len(plan.children), scx)
					// The tree decides the share, not the pixel count.
					s.AdoptSizes(scaleSizesTo(s.ContainerSize(), plan.sizes), scx)
				})
			}
			cached.children = plan.children
			cached.sizes = plan.sizes
			continue
		}
		for _, panel := range plan.panels {
			livePanels[panel] = true
		}
		views := a.viewsOf(plan.panels)
		group := a.groupEntity(plan.node, w, cx)
		constraints, activeIx := plan.constraints, plan.activeIx
		group.Update(cx, func(g *TabGroup, gcx *gpui.Context[TabGroup]) {
			// Every group must be told this; an unconstrained group stays
			// sealed and silently declines drags, drops and closes.
			g.SetConstraints(constraints, w, gcx)
			g.SyncFromTree(views, activeIx, w, gcx)
		})
	}

	for node, cached := range a.groups {
		if !liveNodes[node] {
			cached.subscription.Cancel()
			cached.entity.Release()
			delete(a.groups, node)
		}
	}
	for node, cached := range a.splits {
		if !liveNodes[node] {
			cached.subscription.Cancel()
			cached.entity.Release()
			delete(a.splits, node)
		}
	}

	var departed []PanelView
	for panel, view := range a.panels {
		if !livePanels[panel] {
			departed = append(departed, view)
			delete(a.panels, panel)
		}
	}

	// A zoomed container that left the dock must not keep filling it.
	if a.zoomed != nil {
		if _, ok := a.groups[*a.zoomed]; !ok {
			a.setZoom(nil, w, cx)
		}
	}

	cx.Notify()
	// Last, so a panel reacting sees a dock that already agrees with its
	// trees.
	app := cx.App()
	for _, view := range departed {
		id := view.PanelID(app)
		view.OnRemoved(w, app)
		if release, ok := a.owned[id]; ok {
			delete(a.owned, id)
			release()
		}
	}
}

// viewsOf returns the views for a group's panel ids, in tab order
// (U:dock/dock_area.rs:1030-1041).
func (a *DockArea) viewsOf(panels []PanelID) []PanelView {
	views := make([]PanelView, 0, len(panels))
	for _, panel := range panels {
		if view, ok := a.panels[panel]; ok {
			views = append(views, view)
		}
	}
	return views
}

// groupEntity returns the cached group for node, building it on first use
// (U:dock/dock_area.rs:1043-1064).
func (a *DockArea) groupEntity(node NodeID, w *gpui.Window, cx *gpui.Context[DockArea]) gpui.Entity[TabGroup] {
	if cached, ok := a.groups[node]; ok {
		return cached.entity
	}
	renderer := a.renderer.TabGroupRenderer()
	entity := gpui.NewEntity(cx, cx.Scope(), func(g *TabGroup, gcx *gpui.Context[TabGroup]) {
		*g = NewTabGroup(node, w, gcx)
		g.WithRenderer(renderer)
	})
	subscription := TabGroupEvents.SubscribeIn(cx, entity, w, func(a *DockArea, group gpui.Entity[TabGroup], event *TabGroupEvent, w *gpui.Window, cx *gpui.Context[DockArea]) {
		a.onTabGroupEvent(group, *event, w, cx)
	})
	a.groups[node] = &cachedGroup{entity: entity, subscription: subscription}
	return entity
}

// splitEntity returns the cached ResizableState for node, building it on
// first use (U:dock/dock_area.rs:1066-1114). A finished drag writes the
// measured sizes straight back into the tree.
func (a *DockArea) splitEntity(node NodeID, cx *gpui.Context[DockArea]) gpui.Entity[ResizableState] {
	if cached, ok := a.splits[node]; ok {
		return cached.entity
	}
	entity := gpui.NewEntity(cx, cx.Scope(), func(s *ResizableState, _ *gpui.Context[ResizableState]) {
		*s = NewResizableState()
	})
	subscription := ResizableEvents.Subscribe(cx, entity, func(a *DockArea, state gpui.Entity[ResizableState], _ *ResizablePanelEvent, cx *gpui.Context[DockArea]) {
		measured := state.Read(cx, func(s *ResizableState, _ *gpui.App) []float32 { return slices.Clone(s.Sizes()) })
		sizes := make([]*float32, len(measured))
		for i, size := range measured {
			sizes[i] = someF32(size)
		}
		region, ok := a.placementOfNode(node)
		if !ok {
			return
		}
		tree, ok := a.Layout(region)
		if !ok {
			return
		}
		if tree.SetSizes(node, sizes).Changed() {
			DockEvents.Emit(cx, DockEventLayoutChanged{})
		}
		// The state is where these came from, so the next reconcile has
		// nothing to hand it.
		if cached, ok := a.splits[node]; ok {
			cached.sizes = sizes
		}
	})
	a.splits[node] = &cachedSplit{entity: entity, subscription: subscription}
	return entity
}

// onTabGroupEvent applies the intents arriving from the containers
// (U:dock/dock_area.rs:1117-1162).
func (a *DockArea) onTabGroupEvent(group gpui.Entity[TabGroup], event TabGroupEvent, w *gpui.Window, cx *gpui.Context[DockArea]) {
	node := group.Read(cx, func(g *TabGroup, _ *gpui.App) NodeID { return g.Node() })
	switch event := event.(type) {
	case TabGroupDrop:
		a.MovePanel(event.Panel, event.Target, w, cx)
	case TabGroupDragDrop:
		DockEvents.Emit(cx, DockEventDragDrop{Item: event.Item, Target: event.Target})
	case TabGroupClosePanel:
		a.removePanelID(event.Panel, w, cx)
	case TabGroupActiveChanged:
		region, ok := a.placementOfNode(node)
		if !ok {
			return
		}
		tree, ok := a.Layout(region)
		if !ok {
			return
		}
		a.commit(tree.SetActive(node, event.Ix), w, cx)
	case TabGroupZoomIn:
		a.setZoom(&node, w, cx)
	case TabGroupZoomOut:
		// Only the group actually on screen can give the dock back.
		if a.zoomed != nil && *a.zoomed == node {
			a.setZoom(nil, w, cx)
		}
	}
}

// adoptMeasuredSizes feeds every laid-out split's measured size back into
// the tree before an edit that has to divide space
// (U:dock/dock_area.rs:1166-1188).
func (a *DockArea) adoptMeasuredSizes(placement DockPlacement, app *gpui.App) {
	measured := make(map[NodeID][]float32)
	for node, cached := range a.splits {
		// Only splits that have actually been laid out: a split created
		// earlier in this same edit has a zero container and placeholder
		// sizes.
		laidOut := cached.entity.Read(app, func(s *ResizableState, _ *gpui.App) bool { return s.ContainerSize() > 0 })
		if laidOut {
			measured[node] = cached.entity.Read(app, func(s *ResizableState, _ *gpui.App) []float32 { return slices.Clone(s.Sizes()) })
		}
	}
	if tree, ok := a.Layout(placement); ok {
		tree.adoptMeasuredSizes(measured)
	}
}

// placementOfNode is the region a container belongs to
// (U:dock/dock_area.rs:1197-1207).
func (a *DockArea) placementOfNode(node NodeID) (DockPlacement, bool) {
	if _, ok := a.center.FindNode(node); ok {
		return DockPlacementCenter, true
	}
	for _, placement := range dockOrder {
		if region, ok := a.docks[placement]; ok {
			if _, ok := region.tree.FindNode(node); ok {
				return placement, true
			}
		}
	}
	return 0, false
}

// placementOfPanel is U:dock/dock_area.rs:1209-1217.
func (a *DockArea) placementOfPanel(panel PanelID) (DockPlacement, bool) {
	if _, ok := a.center.FindPanelNode(panel); ok {
		return DockPlacementCenter, true
	}
	for _, placement := range dockOrder {
		if region, ok := a.docks[placement]; ok {
			if _, ok := region.tree.FindPanelNode(panel); ok {
				return placement, true
			}
		}
	}
	return 0, false
}

// resizeDock resizes one dock from a pointer position, clamped so neither
// it nor the opposite dock is squeezed below the minimum
// (U:dock/dock_area.rs:1219-1262). A collapsible bottom dock follows the
// pointer below the minimum down to its closed strip.
func (a *DockArea) resizeDock(placement DockPlacement, pointer gpui.Point, w *gpui.Window, cx *gpui.Context[DockArea]) {
	var opposite float32
	switch placement {
	case DockPlacementLeft:
		opposite, _ = a.DockSize(DockPlacementRight)
	case DockPlacementRight:
		opposite, _ = a.DockSize(DockPlacementLeft)
	}
	sizing := NewDockSizing(placement).WithAreaBounds(a.bounds).WithOppositeDockSize(opposite)
	size := min(sizing.SizeFromPointer(pointer), sizing.Clamp(pixelsMax))

	region, ok := a.docks[placement]
	if !ok {
		return
	}
	wasOpen := region.dock.IsOpen()
	if placement == DockPlacementBottom && region.dock.IsCollapsible() && size < PanelMinSize {
		region.dock.SetOpen(size > ClosedBottomStrip)
		region.dock.setLiveSize(someF32(max(size, ClosedBottomStrip)))
	} else {
		region.dock.SetOpen(true)
		region.dock.setLiveSize(nil)
		region.dock.SetSize(size)
	}
	if region.dock.IsOpen() != wasOpen {
		a.reconcile(w, cx)
	}
	cx.Notify()
}

// endDockResize settles a drag that ended below the minimum: nearer the
// closed strip it closes, nearer the minimum it opens at the minimum
// (U:dock/dock_area.rs:1264-1289).
func (a *DockArea) endDockResize(placement DockPlacement, w *gpui.Window, cx *gpui.Context[DockArea]) {
	region, ok := a.docks[placement]
	if !ok {
		return
	}
	live := region.dock.liveSizeValue()
	if live == nil {
		return
	}
	size := *live
	region.dock.setLiveSize(nil)
	open := size >= (ClosedBottomStrip+PanelMinSize)/2
	if open {
		region.dock.SetSize(PanelMinSize)
	}
	if region.dock.IsOpen() != open {
		region.dock.SetOpen(open)
		a.reconcile(w, cx)
	}
	cx.Notify()
}

// renderNode lowers one container to an element
// (U:dock/dock_area.rs:1294-1379).
func (a *DockArea) renderNode(node *PaneNode, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	if !node.kind.split {
		if cached, ok := a.groups[node.id]; ok {
			element, err := gpui.TryIntoElement(gpui.ViewOf[TabGroup](cached.entity))
			if err != nil {
				panic(err)
			}
			return element
		}
		return gpui.Empty()
	}
	axis := node.kind.axis
	id := gpui.NamedIntegerElementID("dock-split", node.id.AsU64())
	var group *ResizablePanelGroup
	if axis == gpui.AxisHorizontal {
		group = HResizable(id)
	} else {
		group = VResizable(id)
	}
	// A container whose every panel is hidden must not keep occupying its
	// slot, and the slot absorbing the leftover has to be one that is drawn.
	shown := make([]bool, len(node.kind.children))
	grows := -1
	for ix := range node.kind.children {
		shown[ix] = a.isNodeVisible(&node.kind.children[ix], app)
		if shown[ix] {
			grows = ix
		}
	}
	panels := make([]any, len(node.kind.children))
	for ix := range node.kind.children {
		panel := NewResizablePanel().Visible(shown[ix]).Child(a.renderNode(&node.kind.children[ix], w, app))
		// flex_none is what makes a size stick; the growth slot absorbs
		// container growth.
		if size := node.kind.sizes[ix]; size != nil {
			panel.Size(*size)
			if ix != grows {
				panel.FlexNone()
			}
		}
		panels[ix] = panel
	}
	if cached, ok := a.splits[node.id]; ok {
		group.WithState(cached.entity)
	}
	renderer := a.renderer
	group.WithHandleAppearance(func(handle *ResizeHandleContext, w *gpui.Window, app *gpui.App) (gpui.AnyElement, bool) {
		return dockAreaRenderSplitHandle(renderer, handle, w, app)
	})
	group.Children(panels...)

	// A split frame with no size collapses; size_full and flex_1 are belt
	// and braces.
	return dockAreaSplitFrame(a.renderer, node.id, axis, w, app).
		SizeFull().
		Flex1().
		MinH(gpui.Px(0)).
		OverflowHidden().
		Child(group).
		IntoElement()
}

// isNodeVisible reports whether anything in this container is on screen
// (U:dock/dock_area.rs:1381-1394).
func (a *DockArea) isNodeVisible(node *PaneNode, app *gpui.App) bool {
	if node.kind.split {
		for i := range node.kind.children {
			if a.isNodeVisible(&node.kind.children[i], app) {
				return true
			}
		}
		return false
	}
	source := livePanels{panels: a.panels, app: app}
	for _, panel := range node.kind.panels {
		if source.IsVisible(panel) {
			return true
		}
	}
	return false
}

// renderDock is U:dock/dock_area.rs:1396-1425: a closed side dock takes no
// space, a closed bottom dock keeps a strip; the dock's box is applied here
// around the renderer's chrome.
func (a *DockArea) renderDock(placement DockPlacement, w *gpui.Window, app *gpui.App) (gpui.AnyElement, bool) {
	region, ok := a.docks[placement]
	if !ok {
		return gpui.AnyElement{}, false
	}
	dock := a.dockContext(placement, &region.dock)
	size := DockExtent(dock)
	if size <= 0 {
		// Gap 2b: the plain upstream div() is a block.
		return gpui.Div().Block().IntoElement(), true
	}
	content := a.renderNode(region.tree.Root(), w, app)
	chrome := dockAreaRenderDock(a.renderer, dock, content, w, app)
	return DockFrame(dock, size).Child(chrome).IntoElement(), true
}

// dockContext is U:dock/dock_area.rs:1427-1453.
func (a *DockArea) dockContext(placement DockPlacement, dock *Dock) *DockContext {
	area := a.this
	size := dock.Size()
	if live := dock.liveSizeValue(); live != nil {
		size = *live
	}
	return &DockContext{
		placement:   placement,
		size:        size,
		open:        dock.IsOpen(),
		collapsible: dock.IsCollapsible(),
		onToggle: func(w *gpui.Window, app *gpui.App) {
			_ = area.TryUpdateIn(app, w, func(a *DockArea, w *gpui.Window, cx *gpui.Context[DockArea]) { a.ToggleDock(placement, w, cx) })
		},
		onResize: func(pointer gpui.Point, w *gpui.Window, app *gpui.App) {
			_ = area.TryUpdateIn(app, w, func(a *DockArea, w *gpui.Window, cx *gpui.Context[DockArea]) { a.resizeDock(placement, pointer, w, cx) })
		},
		onResizeEnd: func(w *gpui.Window, app *gpui.App) {
			_ = area.TryUpdateIn(app, w, func(a *DockArea, w *gpui.Window, cx *gpui.Context[DockArea]) { a.endDockResize(placement, w, cx) })
		},
	}
}

// FocusHandle is Focusable for DockArea (U:dock/dock_area.rs:1458-1462).
func (a *DockArea) FocusHandle(*gpui.App) gpui.FocusHandle { return a.focusHandle }

// Render lays the left dock, the centre column and the right dock out in a
// row around the renderer's frame (U:dock/dock_area.rs:1464-1517).
func (a *DockArea) Render(w *gpui.Window, cx *gpui.Context[DockArea]) gpui.AnyElement {
	app := cx.App()
	renderer := a.renderer
	area := a.this
	frame := TestSupport(dockAreaFrame(renderer, w, app)).
		Relative().
		SizeFull().
		OverflowHidden().
		Flex().
		FlexRow()
	frame = ElementExtOf(frame).OnPrepaint(func(bounds gpui.Bounds, _ *gpui.Window, app *gpui.App) {
		_ = area.TryUpdate(app, func(a *DockArea, _ *gpui.Context[DockArea]) { a.bounds = bounds })
	})
	frame.TrackFocus(a.focusHandle)
	if view, ok := a.zoomedView(); ok {
		return frame.Child(view).IntoElement()
	}
	if left, ok := a.renderDock(DockPlacementLeft, w, app); ok {
		frame.Child(left)
	}
	// The centre is whatever the side docks leave, in a column with the
	// bottom dock.
	center := dockAreaCenterFrame(renderer, w, app).
		Flex().
		Flex1().
		FlexCol().
		OverflowHidden().
		Child(a.renderNode(a.center.Root(), w, app))
	if bottom, ok := a.renderDock(DockPlacementBottom, w, app); ok {
		center.Child(bottom)
	}
	frame.Child(center)
	if right, ok := a.renderDock(DockPlacementRight, w, app); ok {
		frame.Child(right)
	}
	return frame.IntoElement()
}

// scaleSizesTo re-expresses slot sizes as shares of container
// (U:dock/dock_area.rs:1543-1560): unchanged unless every slot is
// constrained and both the container and the total are usable.
func scaleSizesTo(container float32, sizes []*float32) []*float32 {
	var total float32
	for _, size := range sizes {
		if size == nil {
			return slices.Clone(sizes)
		}
		total += *size
	}
	if container <= 0 || total <= 0 {
		return slices.Clone(sizes)
	}
	scale := container / total
	out := make([]*float32, len(sizes))
	for i, size := range sizes {
		out[i] = someF32(*size * scale)
	}
	return out
}

// syncSplitPanels brings one split's ResizableState panel list from
// previous to next, inserting and removing at the exact index
// (U:dock/dock_area.rs:1562-1608).
func syncSplitPanels(state *ResizableState, previous, next []NodeID, sizes []*float32, cx *gpui.Context[ResizableState]) {
	current := slices.Clone(previous)
	// Removals from the tail, so the indices ahead of each stay valid.
	for ix := len(current) - 1; ix >= 0; ix-- {
		if !slices.Contains(next, current[ix]) {
			if ix < len(state.Sizes()) {
				state.RemovePanel(ix, cx)
			}
			current = slices.Delete(current, ix, ix+1)
		}
	}
	for ix, node := range next {
		if ix < len(current) && current[ix] == node {
			continue
		}
		at := min(ix, len(state.Sizes()))
		var size *float32
		if ix < len(sizes) {
			size = sizes[ix]
		}
		state.InsertPanel(size, at, cx)
		current = slices.Insert(current, min(at, len(current)), node)
	}
}

// planTree is U:dock/dock_area.rs:1610-1613: the root has nothing beside it.
func planTree(tree *PaneTree, collapsed, locked bool, out *[]containerPlan) {
	planNode(tree.Root(), true, collapsed, locked, out)
}

// planNode is U:dock/dock_area.rs:1615-1648.
func planNode(node *PaneNode, alone, collapsed, locked bool, out *[]containerPlan) {
	if node.kind.split {
		children := make([]NodeID, len(node.kind.children))
		for i := range node.kind.children {
			children[i] = node.kind.children[i].id
		}
		*out = append(*out, containerPlan{
			split:    true,
			node:     node.id,
			axis:     node.kind.axis,
			children: children,
			sizes:    slices.Clone(node.kind.sizes),
		})
		childrenAlone := len(node.kind.children) <= 1
		for i := range node.kind.children {
			planNode(&node.kind.children[i], childrenAlone, collapsed, locked, out)
		}
		return
	}
	*out = append(*out, containerPlan{
		node:        node.id,
		panels:      slices.Clone(node.kind.panels),
		activeIx:    node.kind.activeIx,
		constraints: TabGroupConstraintsInSplit(alone).DockLocked(locked).Collapsed(collapsed),
	})
}

// firstTabGroup is U:dock/dock_area.rs:1650-1655.
func firstTabGroup(node *PaneNode) (NodeID, bool) {
	if !node.kind.split {
		return node.id, true
	}
	for i := range node.kind.children {
		if id, ok := firstTabGroup(&node.kind.children[i]); ok {
			return id, true
		}
	}
	return 0, false
}

// registryPanelBuilder rebuilds panels out of persisted state through the
// PanelRegistry (U:dock/dock_area.rs:1663-1691).
type registryPanelBuilder struct {
	area  *DockArea
	w     *gpui.Window
	cx    *gpui.Context[DockArea]
	built []dockLayoutPanel
}

func (b *registryPanelBuilder) Build(state *PanelState, info *PanelInfo) PanelID {
	app := b.cx.App()
	context := NewPanelBuildContext(b.area.this, state, info)
	view, ok := PanelRegistryBuildPanel(state.PanelName, context, b.w, app)
	if !ok {
		view, ok = dockAreaBuildPlaceholder(b.area.renderer, state, b.w, app)
	}
	if !ok {
		saved := state.Clone()
		entity := gpui.NewEntity(b.cx, b.cx.Scope(), func(p *placeholderPanel, pcx *gpui.Context[placeholderPanel]) {
			*p = placeholderPanel{state: saved, focusHandle: pcx.App().FocusHandle()}
		})
		view = PanelViewOf[placeholderPanel](entity)
		b.area.owned[PanelIDOf(entity.EntityID())] = entity.Release
	}
	id := view.PanelID(app)
	b.built = append(b.built, dockLayoutPanel{id: id, view: view})
	return id
}

// placeholderPanel stands in for a panel this build cannot construct
// (U:dock/dock_area.rs:1693-1735): it draws nothing but hands its original
// PanelState back from Dump, so the panel survives a load and save.
type placeholderPanel struct {
	state       PanelState
	focusHandle gpui.FocusHandle
}

// PanelName is U:dock/dock_area.rs:1714-1716.
func (*placeholderPanel) PanelName() string { return "InvalidPanel" }

// Dump is U:dock/dock_area.rs:1718-1720.
func (p *placeholderPanel) Dump(*gpui.App) PanelState { return p.state.Clone() }

// FocusHandle is U:dock/dock_area.rs:1725-1729.
func (p *placeholderPanel) FocusHandle(*gpui.App) gpui.FocusHandle { return p.focusHandle }

// Render draws nothing (U:dock/dock_area.rs:1731-1735).
func (*placeholderPanel) Render(*gpui.Window, *gpui.Context[placeholderPanel]) gpui.AnyElement {
	return gpui.Empty()
}

// DockContext is what a skin needs to draw one dock, and the callbacks it
// invokes rather than reimplementing open/close and clamping
// (U:dock/dock_area.rs:1737-1787).
type DockContext struct {
	placement   DockPlacement
	size        float32
	open        bool
	collapsible bool
	onToggle    func(w *gpui.Window, app *gpui.App)
	onResize    func(pointer gpui.Point, w *gpui.Window, app *gpui.App)
	onResizeEnd func(w *gpui.Window, app *gpui.App)
}

// Placement is U:dock/dock_area.rs:1754-1756.
func (c *DockContext) Placement() DockPlacement { return c.placement }

// Size is the dock's extent along its own axis
// (U:dock/dock_area.rs:1758-1762).
func (c *DockContext) Size() float32 { return c.size }

// IsOpen is U:dock/dock_area.rs:1764-1766.
func (c *DockContext) IsOpen() bool { return c.open }

// IsCollapsible is U:dock/dock_area.rs:1768-1770.
func (c *DockContext) IsCollapsible() bool { return c.collapsible }

// Toggle is U:dock/dock_area.rs:1772-1774.
func (c *DockContext) Toggle(w *gpui.Window, app *gpui.App) { c.onToggle(w, app) }

// ResizeTo resizes from a pointer position in window coordinates
// (U:dock/dock_area.rs:1776-1780).
func (c *DockContext) ResizeTo(pointer gpui.Point, w *gpui.Window, app *gpui.App) {
	c.onResize(pointer, w, app)
}

// EndResize ends a resize started with ResizeTo
// (U:dock/dock_area.rs:1782-1786).
func (c *DockContext) EndResize(w *gpui.Window, app *gpui.App) { c.onResizeEnd(w, app) }

// ClosedBottomStrip is what a closed bottom dock keeps, so its tab bar
// stays clickable (U:dock/dock_area.rs:1789-1792).
const ClosedBottomStrip float32 = 29

// DockExtent is how much room a dock asks for along its own axis
// (U:dock/dock_area.rs:1794-1801).
func DockExtent(dock *DockContext) float32 {
	switch {
	case dock.IsOpen():
		return dock.Size()
	case dock.Placement() == DockPlacementBottom:
		return ClosedBottomStrip
	}
	return 0
}

// DockFrame is the box a dock occupies: its extent along its own axis, full
// across, held at that size (U:dock/dock_area.rs:1803-1820).
func DockFrame(dock *DockContext, size float32) *gpui.DivElement {
	// Gap 2b: the plain upstream div() is a block.
	d := gpui.Div().Block().Flex().FlexNone().Relative().OverflowHidden()
	switch dock.Placement() {
	case DockPlacementLeft, DockPlacementRight:
		d.FlexRow().HFull().W(gpui.Px(size))
	case DockPlacementBottom:
		d.WFull().H(gpui.Px(size))
	}
	return d
}

// DockAreaRenderer is the appearance for the dock area; base draws none of
// it (U:dock/dock_area.rs:1822-1926). Only TabGroupRenderer is required;
// the other hooks are the optional DockAreaFrameRenderer,
// DockAreaSplitFrameRenderer, DockAreaCenterFrameRenderer,
// DockAreaSplitHandleRenderer, DockAreaDockRenderer and
// DockAreaPlaceholderBuilder.
type DockAreaRenderer interface {
	// TabGroupRenderer supplies the renderer every group the area builds
	// uses (U:dock/dock_area.rs:1925).
	TabGroupRenderer() TabGroupRenderer
}

// DockAreaFrameRenderer supplies the area's outer frame, which base
// records its bounds on (U:dock/dock_area.rs:1834-1841). It must carry an
// id.
type DockAreaFrameRenderer interface {
	Frame(w *gpui.Window, app *gpui.App) *gpui.DivElement
}

// DockAreaSplitFrameRenderer supplies one split container's frame, around
// base's resizable group (U:dock/dock_area.rs:1843-1860).
type DockAreaSplitFrameRenderer interface {
	SplitFrame(node NodeID, axis gpui.Axis, w *gpui.Window, app *gpui.App) *gpui.DivElement
}

// DockAreaCenterFrameRenderer supplies the column holding the center
// region and the bottom dock (U:dock/dock_area.rs:1862-1867).
type DockAreaCenterFrameRenderer interface {
	CenterFrame(w *gpui.Window, app *gpui.App) *gpui.DivElement
}

// DockAreaSplitHandleRenderer paints the divider between two slots of a
// split (U:dock/dock_area.rs:1869-1882); false keeps base's own line. The
// hit area, cursor and drag stay with base.
type DockAreaSplitHandleRenderer interface {
	RenderSplitHandle(handle *ResizeHandleContext, w *gpui.Window, app *gpui.App) (gpui.AnyElement, bool)
}

// DockAreaDockRenderer draws one dock's chrome around its content
// (U:dock/dock_area.rs:1884-1900). The dock's box is applied by base.
type DockAreaDockRenderer interface {
	RenderDock(dock *DockContext, content gpui.AnyElement, w *gpui.Window, app *gpui.App) gpui.AnyElement
}

// DockAreaPlaceholderBuilder builds the stand-in for a panel no registry
// builder answers to (U:dock/dock_area.rs:1902-1923); false takes base's
// own placeholder. It should answer Dump with state unchanged.
type DockAreaPlaceholderBuilder interface {
	BuildPlaceholder(state *PanelState, w *gpui.Window, app *gpui.App) (PanelView, bool)
}

func dockAreaFrame(r DockAreaRenderer, w *gpui.Window, app *gpui.App) *gpui.DivElement {
	if f, ok := r.(DockAreaFrameRenderer); ok {
		return f.Frame(w, app)
	}
	// Gap 2b: the plain upstream div() is a block.
	return gpui.Div().ID("dock-area").Block()
}

func dockAreaSplitFrame(r DockAreaRenderer, node NodeID, axis gpui.Axis, w *gpui.Window, app *gpui.App) *gpui.DivElement {
	if f, ok := r.(DockAreaSplitFrameRenderer); ok {
		return f.SplitFrame(node, axis, w, app)
	}
	// Gap 2b: the plain upstream div() is a block.
	return gpui.Div().ID(gpui.NamedIntegerElementID("dock-split-frame", node.AsU64())).Block()
}

func dockAreaCenterFrame(r DockAreaRenderer, w *gpui.Window, app *gpui.App) *gpui.DivElement {
	if f, ok := r.(DockAreaCenterFrameRenderer); ok {
		return f.CenterFrame(w, app)
	}
	// Gap 2b: the plain upstream div() is a block.
	return gpui.Div().ID("dock-area-center").Block()
}

func dockAreaRenderSplitHandle(r DockAreaRenderer, handle *ResizeHandleContext, w *gpui.Window, app *gpui.App) (gpui.AnyElement, bool) {
	if f, ok := r.(DockAreaSplitHandleRenderer); ok {
		return f.RenderSplitHandle(handle, w, app)
	}
	return gpui.AnyElement{}, false
}

func dockAreaRenderDock(r DockAreaRenderer, dock *DockContext, content gpui.AnyElement, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	if f, ok := r.(DockAreaDockRenderer); ok {
		return f.RenderDock(dock, content, w, app)
	}
	return content
}

func dockAreaBuildPlaceholder(r DockAreaRenderer, state *PanelState, w *gpui.Window, app *gpui.App) (PanelView, bool) {
	if f, ok := r.(DockAreaPlaceholderBuilder); ok {
		return f.BuildPlaceholder(state, w, app)
	}
	return nil, false
}

// bareDockArea is the renderer an area starts with: the layout and nothing
// else (U:dock/dock_area.rs:1928-1935).
type bareDockArea struct{}

func (bareDockArea) TabGroupRenderer() TabGroupRenderer { return bareTabGroup{} }
