package base

// Port of U:dock/tab_group.rs: a tab group's behavior, with no appearance
// of its own.
//
// Go adaptations: TabGroupEvent is a sealed interface over one struct per
// variant; Option<usize> parameters are an int with negative meaning None
// (the ResizableState.InsertPanel convention) and Option<Placement> is a
// (Placement, bool) pair; the renderer trait's default methods are
// optional interfaces.

import (
	"github.com/hoangvu12/gpui-go/gpui"
)

// TabGroupEvent is behavior a tab group cannot carry out on its own
// (U:dock/tab_group.rs:24-51): every variant ends in an edit to the layout
// tree, which belongs to the container that owns the group.
type TabGroupEvent interface{ isTabGroupEvent() }

// TabGroupDrop is TabGroupEvent::Drop: a panel was dropped on this group,
// and Target says where in the tree it lands.
type TabGroupDrop struct {
	Panel  PanelID
	Source NodeID
	Target InsertTarget
}

// TabGroupDragDrop is TabGroupEvent::DragDrop: a host-owned drag landed.
type TabGroupDragDrop struct {
	Item   AnyDrag
	Target DropTarget
}

// TabGroupClosePanel is TabGroupEvent::ClosePanel.
type TabGroupClosePanel struct {
	Panel PanelID
}

// TabGroupActiveChanged is TabGroupEvent::ActiveChanged: the displayed
// tab changed, so the tree's stored active index is stale.
type TabGroupActiveChanged struct {
	Ix int
}

// TabGroupZoomIn is TabGroupEvent::ZoomIn.
type TabGroupZoomIn struct{}

// TabGroupZoomOut is TabGroupEvent::ZoomOut.
type TabGroupZoomOut struct{}

func (TabGroupDrop) isTabGroupEvent()          {}
func (TabGroupDragDrop) isTabGroupEvent()      {}
func (TabGroupClosePanel) isTabGroupEvent()    {}
func (TabGroupActiveChanged) isTabGroupEvent() {}
func (TabGroupZoomIn) isTabGroupEvent()        {}
func (TabGroupZoomOut) isTabGroupEvent()       {}

// TabGroupEvents is the TabGroup event descriptor
// (U:dock/tab_group.rs:672 impl EventEmitter<TabGroupEvent>).
var TabGroupEvents = gpui.DefineEvent[TabGroup, TabGroupEvent]()

// TabGroupConstraints is everything the container knows about a group's
// place in the dock, pushed as one value (U:dock/tab_group.rs:53-134).
type TabGroupConstraints struct {
	alone      bool
	dockLocked bool
	collapsed  bool
	closable   bool
}

// TabGroupConstraintsSealed is a group with nowhere to go: locked, alone,
// unclosable (U:dock/tab_group.rs:70-79).
func TabGroupConstraintsSealed() TabGroupConstraints {
	return TabGroupConstraints{alone: true, dockLocked: true}
}

// TabGroupConstraintsInSplit is a group in a split layout; alone when
// nothing sits beside it (U:dock/tab_group.rs:81-91).
func TabGroupConstraintsInSplit(alone bool) TabGroupConstraints {
	return TabGroupConstraints{alone: alone, closable: true}
}

// DockLocked sets whether the dock as a whole forbids rearranging
// (U:dock/tab_group.rs:93-97).
func (c TabGroupConstraints) DockLocked(dockLocked bool) TabGroupConstraints {
	c.dockLocked = dockLocked
	return c
}

// Collapsed sets whether the group is folded to a strip of tabs
// (U:dock/tab_group.rs:99-103).
func (c TabGroupConstraints) Collapsed(collapsed bool) TabGroupConstraints {
	c.collapsed = collapsed
	return c
}

// Closable sets whether the container allows closing at all
// (U:dock/tab_group.rs:105-110).
func (c TabGroupConstraints) Closable(closable bool) TabGroupConstraints {
	c.closable = closable
	return c
}

// IsAlone is U:dock/tab_group.rs:112-115.
func (c TabGroupConstraints) IsAlone() bool { return c.alone }

// IsLocked is the dock-wide lock (U:dock/tab_group.rs:117-125).
func (c TabGroupConstraints) IsLocked() bool { return c.dockLocked }

// IsCollapsed is U:dock/tab_group.rs:127-129.
func (c TabGroupConstraints) IsCollapsed() bool { return c.collapsed }

// IsClosable is U:dock/tab_group.rs:131-133.
func (c TabGroupConstraints) IsClosable() bool { return c.closable }

// TabGroup is a tab group's behavior (U:dock/tab_group.rs:136-159): the
// panel list mirrored from the layout tree, the displayed index, the focus
// handle, drag and drop hit state and the zoom flag. Everything visible is
// produced by the TabGroupRenderer the host installs.
type TabGroup struct {
	node          NodeID
	this          gpui.WeakEntity[TabGroup]
	panels        []PanelView
	activeIx      int
	zoomed        bool
	constraints   TabGroupConstraints
	focusHandle   gpui.FocusHandle
	active        activeTracker
	dropIndicator *DropIndicator
	renderer      TabGroupRenderer
}

// NewTabGroup is TabGroup::new (U:dock/tab_group.rs:161-179; pub(crate)
// upstream, exported for a container built outside the package). Build it
// in place: *g = NewTabGroup(node, w, cx).
func NewTabGroup(node NodeID, _ *gpui.Window, cx *gpui.Context[TabGroup]) TabGroup {
	return TabGroup{
		node: node,
		this: cx.Entity().Downgrade(),
		// A group that no container has placed yet can do nothing.
		constraints: TabGroupConstraintsSealed(),
		focusHandle: cx.App().FocusHandle(),
		renderer:    bareTabGroup{},
	}
}

// WithRenderer installs the appearance (U:dock/tab_group.rs:181-184).
func (g *TabGroup) WithRenderer(renderer TabGroupRenderer) *TabGroup {
	g.renderer = renderer
	return g
}

// Node is the Tabs node this group mirrors (U:dock/tab_group.rs:186-189).
func (g *TabGroup) Node() NodeID { return g.node }

// Panels is U:dock/tab_group.rs:191-193.
func (g *TabGroup) Panels() []PanelView { return g.panels }

// ActiveIx is U:dock/tab_group.rs:195-197.
func (g *TabGroup) ActiveIx() int { return g.activeIx }

// ActivePanel is the panel on screen: the displayed tab unless it has gone
// invisible, then the first visible panel (U:dock/tab_group.rs:199-208).
func (g *TabGroup) ActivePanel(app *gpui.App) (PanelView, bool) {
	if g.activeIx >= len(g.panels) {
		return nil, false
	}
	if panel := g.panels[g.activeIx]; panel.Visible(app) {
		return panel, true
	}
	for _, panel := range g.panels {
		if panel.Visible(app) {
			return panel, true
		}
	}
	return nil, false
}

// IsZoomed is U:dock/tab_group.rs:210-212.
func (g *TabGroup) IsZoomed() bool { return g.zoomed }

// IsCollapsed is U:dock/tab_group.rs:214-216.
func (g *TabGroup) IsCollapsed() bool { return g.constraints.IsCollapsed() }

// IsClosable reports whether closing the displayed panel is allowed at all
// (U:dock/tab_group.rs:218-229).
func (g *TabGroup) IsClosable(app *gpui.App) bool {
	if !g.constraints.IsClosable() || !g.draggable(app) {
		return false
	}
	panel, ok := g.ActivePanel(app)
	return ok && panel.Closable(app)
}

// SelectTab displays ix, if it names a tab that is not already displayed
// (U:dock/tab_group.rs:231-242).
func (g *TabGroup) SelectTab(ix int, w *gpui.Window, cx *gpui.Context[TabGroup]) {
	if ix < 0 || ix >= len(g.panels) || ix == g.activeIx {
		return
	}
	g.activeIx = ix
	g.focusActivePanel(w, cx)
	g.scheduleActiveSync(w, cx)
	TabGroupEvents.Emit(cx, TabGroupActiveChanged{Ix: ix})
	cx.Notify()
}

// ClosePanel asks the container to close panel (U:dock/tab_group.rs:244-265).
// Nothing happens for a non-member, or when the group or panel refuses.
func (g *TabGroup) ClosePanel(panel PanelID, cx *gpui.Context[TabGroup]) {
	if !g.constraints.IsClosable() {
		return
	}
	// A dock's last group has nowhere to go and must stay.
	if !g.draggable(cx.App()) {
		return
	}
	if !g.panelClosable(panel, cx.App()) {
		return
	}
	TabGroupEvents.Emit(cx, TabGroupClosePanel{Panel: panel})
	cx.Notify()
}

func (g *TabGroup) panelClosable(panel PanelID, app *gpui.App) bool {
	for _, candidate := range g.panels {
		if candidate.PanelID(app) == panel && candidate.Closable(app) {
			return true
		}
	}
	return false
}

// ToggleZoom is U:dock/tab_group.rs:267-269.
func (g *TabGroup) ToggleZoom(w *gpui.Window, cx *gpui.Context[TabGroup]) {
	g.setZoomed(!g.zoomed, w, cx)
}

// Context is a snapshot of everything a skin needs to draw this group
// (U:dock/tab_group.rs:271-319).
func (g *TabGroup) Context(app *gpui.App) *TabGroupContext {
	group := g.this
	activePanel, _ := g.ActivePanel(app)
	ctx := &TabGroupContext{
		node:                g.node,
		panels:              append([]PanelView(nil), g.panels...),
		activePanel:         activePanel,
		activeIx:            g.activeIx,
		zoomed:              g.zoomed,
		constraints:         g.constraints,
		activePanelClosable: g.IsClosable(app),
		draggable:           g.draggable(app),
		droppable:           g.droppable(),
	}
	// A stale indicator would otherwise outlive a drag that was cancelled
	// while hovering this group.
	if app.HasActiveDrag() && g.dropIndicator != nil {
		indicator := *g.dropIndicator
		ctx.dropIndicator = &indicator
	}
	ctx.onSelectTab = func(ix int, w *gpui.Window, app *gpui.App) {
		_ = group.TryUpdateIn(app, w, func(g *TabGroup, w *gpui.Window, cx *gpui.Context[TabGroup]) { g.SelectTab(ix, w, cx) })
	}
	ctx.onClose = func(panel PanelID, _ *gpui.Window, app *gpui.App) {
		_ = group.TryUpdate(app, func(g *TabGroup, cx *gpui.Context[TabGroup]) { g.ClosePanel(panel, cx) })
	}
	ctx.onToggleZoom = func(w *gpui.Window, app *gpui.App) {
		_ = group.TryUpdateIn(app, w, func(g *TabGroup, w *gpui.Window, cx *gpui.Context[TabGroup]) { g.ToggleZoom(w, cx) })
	}
	ctx.onDropPanel = func(drag DragPanel, ix int, activate bool, _ *gpui.Window, app *gpui.App) {
		_ = group.TryUpdate(app, func(g *TabGroup, cx *gpui.Context[TabGroup]) { g.onDrop(drag, ix, activate, cx) })
	}
	ctx.onDropItem = func(item AnyDrag, placement Placement, hasPlacement bool, _ *gpui.Window, app *gpui.App) {
		_ = group.TryUpdate(app, func(g *TabGroup, cx *gpui.Context[TabGroup]) { g.emitDragDrop(item, placement, hasPlacement, cx) })
	}
	return ctx
}

// SyncFromTree mirrors one Tabs node's membership and displayed index into
// this group (U:dock/tab_group.rs:322-348; pub(crate) upstream, exported
// for a container built outside the package). OnAddedTo fires for
// arrivals; departures are silent here.
func (g *TabGroup) SyncFromTree(panels []PanelView, activeIx int, w *gpui.Window, cx *gpui.Context[TabGroup]) {
	group := cx.Entity().Downgrade()
	app := cx.App()
	existing := make(map[PanelID]bool, len(g.panels))
	for _, panel := range g.panels {
		existing[panel.PanelID(app)] = true
	}
	for _, panel := range panels {
		if !existing[panel.PanelID(app)] {
			panel.OnAddedTo(group, w, app)
		}
	}
	g.panels = panels
	g.activeIx = min(activeIx, max(len(g.panels)-1, 0))
	g.scheduleActiveSync(w, cx)
	cx.Notify()
}

// SetConstraints replaces everything the container knows about this
// group's place in the dock, in one call (U:dock/tab_group.rs:350-375;
// pub(crate) upstream).
func (g *TabGroup) SetConstraints(constraints TabGroupConstraints, w *gpui.Window, cx *gpui.Context[TabGroup]) {
	if g.constraints == constraints {
		return
	}
	// Collapsing takes the displayed panel off screen, which the
	// active-state contract counts as no panel being displayed.
	collapseChanged := g.constraints.IsCollapsed() != constraints.IsCollapsed()
	g.constraints = constraints
	if collapseChanged {
		g.scheduleActiveSync(w, cx)
	}
	cx.Notify()
}

// setZoomed zooms this group in or out, in full: the flag flips, the
// displayed panel is told, and the container is asked to install or clear
// the zoomed view (U:dock/tab_group.rs:377-417). Zooming in is refused
// when there is no displayed panel or it is not zoomable; zooming out
// never is.
func (g *TabGroup) setZoomed(zoomed bool, w *gpui.Window, cx *gpui.Context[TabGroup]) {
	if g.zoomed == zoomed {
		return
	}
	panel, ok := g.ActivePanel(cx.App())
	if zoomed && !(ok && panel.Zoomable(cx.App())) {
		return
	}
	g.zoomed = zoomed
	if zoomed {
		TabGroupEvents.Emit(cx, TabGroupZoomIn{})
	} else {
		TabGroupEvents.Emit(cx, TabGroupZoomOut{})
	}
	// Delivered outside this update so a SetZoomed handler may call back
	// into the group.
	if ok {
		cx.SpawnIn(w, func(_ gpui.WeakEntity[TabGroup], acx *gpui.AsyncWindowContext) struct{} {
			_ = acx.Update(func(w *gpui.Window, app *gpui.App) { panel.SetZoomed(zoomed, w, app) })
			return struct{}{}
		}).Detach()
	}
	cx.Notify()
}

// lastNotifiedActive is what this group last told panel about being active
// (U:dock/tab_group.rs:419-423).
func (g *TabGroup) lastNotifiedActive(panel PanelID) (bool, bool) {
	return g.active.lastNotified(panel)
}

// seedActive records what an arriving panel already believes
// (U:dock/tab_group.rs:425-429).
func (g *TabGroup) seedActive(panel PanelID, active bool) { g.active.seed(panel, active) }

// visiblePanels is every visible panel, in tab order
// (U:dock/tab_group.rs:433-439).
func (g *TabGroup) visiblePanels(app *gpui.App) []PanelView {
	var out []PanelView
	for _, panel := range g.panels {
		if panel.Visible(app) {
			out = append(out, panel)
		}
	}
	return out
}

// isLocked: a zoomed group is locked too (U:dock/tab_group.rs:441-445).
func (g *TabGroup) isLocked() bool { return g.constraints.IsLocked() || g.zoomed }

// isLastPanel: this group holds the last visible panel anything could be
// rearranged around (U:dock/tab_group.rs:447-452).
func (g *TabGroup) isLastPanel(app *gpui.App) bool {
	return g.constraints.IsAlone() && len(g.visiblePanels(app)) <= 1
}

func (g *TabGroup) draggable(app *gpui.App) bool { return !g.isLocked() && !g.isLastPanel(app) }

func (g *TabGroup) droppable() bool { return !g.isLocked() }

// focusActivePanel is U:dock/tab_group.rs:462-466.
func (g *TabGroup) focusActivePanel(w *gpui.Window, cx *gpui.Context[TabGroup]) {
	if panel, ok := g.ActivePanel(cx.App()); ok {
		panel.FocusHandle(cx.App()).Focus(w)
	}
}

// scheduleActiveSync queues one reconcile per frame that notifies panels
// of their frame-end net active state (U:dock/tab_group.rs:468-489). A
// spawned task, not a defer, so it runs after every same-frame mutation
// including a deferred collapse.
func (g *TabGroup) scheduleActiveSync(w *gpui.Window, cx *gpui.Context[TabGroup]) {
	if !g.active.scheduleSync() {
		return
	}
	cx.SpawnIn(w, func(group gpui.WeakEntity[TabGroup], acx *gpui.AsyncWindowContext) struct{} {
		_ = acx.Update(func(w *gpui.Window, app *gpui.App) {
			changes, err := group.TryUpdateWith(app, func(g *TabGroup, cx *gpui.Context[TabGroup]) []panelActiveChange {
				return g.reconcileActive(cx.App())
			})
			if err != nil {
				return
			}
			// Dispatched outside the group's update so a SetActive handler
			// may call back into it.
			for _, change := range changes {
				change.panel.SetActive(change.active, w, app)
			}
		})
		return struct{}{}
	}).Detach()
}

type panelActiveChange struct {
	panel  PanelView
	active bool
}

// reconcileActive returns the deliveries this frame owes, deactivations
// first (U:dock/tab_group.rs:491-512). The displayed slot is activeIx even
// when the panel there is invisible.
func (g *TabGroup) reconcileActive(app *gpui.App) []panelActiveChange {
	g.active.syncFinished()
	ids := make([]PanelID, len(g.panels))
	for i, panel := range g.panels {
		ids[i] = panel.PanelID(app)
	}
	var displayed PanelID
	hasDisplayed := false
	if !g.constraints.IsCollapsed() && g.activeIx < len(ids) {
		displayed, hasDisplayed = ids[g.activeIx], true
	}
	var out []panelActiveChange
	for _, change := range g.active.reconcile(ids, displayed, hasDisplayed) {
		for ix, id := range ids {
			if id == change.panel {
				out = append(out, panelActiveChange{g.panels[ix], change.active})
				break
			}
		}
	}
	return out
}

// onPanelDragMove tracks where a dragged panel would land
// (U:dock/tab_group.rs:514-537).
func (g *TabGroup) onPanelDragMove(drag *gpui.DragMoveEvent[DragPanel], _ *gpui.Window, cx *gpui.Context[TabGroup]) {
	bounds := drag.Bounds
	position := drag.Event.Position
	if !bounds.Contains(position) {
		g.clearDropIndicator(cx)
		return
	}
	placement, hasPlacement := splitPlacementAt(bounds, position)
	dragged := drag.Drag(cx.App())
	// The placeholder flies in from wherever the preview currently is.
	offset := dragged.DragOffset()
	source := NewDropPlaceholderBounds(
		gpui.Point{X: position.X - offset.X - bounds.Origin.X, Y: position.Y - offset.Y - bounds.Origin.Y},
		dragged.PreviewSize(),
	)
	g.syncDropPlaceholder(bounds, placement, hasPlacement, dragged.DragSessionID(), source, cx)
}

// onItemDragMove is onPanelDragMove for a host-owned drag item
// (U:dock/tab_group.rs:539-559).
func (g *TabGroup) onItemDragMove(drag *gpui.DragMoveEvent[AnyDrag], _ *gpui.Window, cx *gpui.Context[TabGroup]) {
	bounds := drag.Bounds
	if !bounds.Contains(drag.Event.Position) {
		g.clearDropIndicator(cx)
		return
	}
	placement, hasPlacement := splitPlacementAt(bounds, drag.Event.Position)
	source := DropPlaceholderBoundsForPlacement(bounds, placement, hasPlacement)
	g.syncDropPlaceholder(bounds, placement, hasPlacement, itemDragSessionID, source, cx)
}

// syncDropPlaceholder is U:dock/tab_group.rs:561-596.
func (g *TabGroup) syncDropPlaceholder(bounds gpui.Bounds, placement Placement, hasPlacement bool, dragSessionID uint64, source DropPlaceholderBounds, cx *gpui.Context[TabGroup]) {
	if !hasPlacement {
		placement = 0
	}
	to := DropPlaceholderBoundsForPlacement(bounds, placement, hasPlacement)
	previous := g.dropIndicator
	restart := previous == nil || previous.dragSessionID != dragSessionID ||
		previous.hasPlacement != hasPlacement || previous.placement != placement

	var from DropPlaceholderBounds
	var epoch uint64
	switch {
	case !restart:
		from, epoch = previous.from, previous.epoch
	case previous != nil:
		from = source
		if previous.dragSessionID == dragSessionID {
			from = previous.to
		}
		epoch = previous.epoch + 1
	default:
		from, epoch = source, 0
	}
	g.dropIndicator = &DropIndicator{
		bounds:        bounds,
		placement:     placement,
		hasPlacement:  hasPlacement,
		from:          from,
		to:            to,
		dragSessionID: dragSessionID,
		epoch:         epoch,
	}
	cx.Notify()
}

// clearDropIndicator is U:dock/tab_group.rs:598-602.
func (g *TabGroup) clearDropIndicator(cx *gpui.Context[TabGroup]) {
	if g.dropIndicator != nil {
		g.dropIndicator = nil
		cx.Notify()
	}
}

// emitDragDrop reports a host-owned drag landing on this group
// (U:dock/tab_group.rs:604-618).
func (g *TabGroup) emitDragDrop(item AnyDrag, placement Placement, hasPlacement bool, cx *gpui.Context[TabGroup]) {
	if !hasPlacement {
		placement = 0
	}
	g.dropIndicator = nil
	TabGroupEvents.Emit(cx, TabGroupDragDrop{Item: item, Target: newDropTarget(g.node, placement, hasPlacement)})
	cx.Notify()
}

// onDrop resolves where a dropped panel lands and reports it as one move
// (U:dock/tab_group.rs:620-669). ix names a tab slot (negative: none),
// which always merges.
func (g *TabGroup) onDrop(drag DragPanel, ix int, activate bool, cx *gpui.Context[TabGroup]) {
	indicator := g.dropIndicator
	g.dropIndicator = nil
	var placement Placement
	hasPlacement := false
	if ix < 0 && indicator != nil {
		placement, hasPlacement = indicator.Placement()
	}

	// Dropping a panel back onto its own group is a move only when it
	// splits out of a group holding more than itself, or when it lands on
	// a specific tab slot.
	if drag.Source() == g.node && ix < 0 && (!hasPlacement || len(g.panels) == 1) {
		cx.Notify()
		return
	}

	var target InsertTarget
	if hasPlacement {
		target = InsertTargetSplit{Node: g.node, Placement: placement}
	} else {
		target = InsertTargetTabs{Node: g.node, Ix: max(ix, 0), HasIx: ix >= 0, Activate: activate}
	}
	TabGroupEvents.Emit(cx, TabGroupDrop{Panel: drag.Panel(), Source: drag.Source(), Target: target})
	cx.Notify()
}

// FocusHandle is Focusable for TabGroup (U:dock/tab_group.rs:674-681): the
// displayed panel's handle, else the group's own.
func (g *TabGroup) FocusHandle(app *gpui.App) gpui.FocusHandle {
	if panel, ok := g.ActivePanel(app); ok {
		return panel.FocusHandle(app)
	}
	return g.focusHandle
}

// Render applies structure around whatever the renderer returns
// (U:dock/tab_group.rs:683-757).
func (g *TabGroup) Render(w *gpui.Window, cx *gpui.Context[TabGroup]) gpui.AnyElement {
	app := cx.App()
	context := g.Context(app)
	renderer := g.renderer
	focusHandle := g.FocusHandle(app)
	droppable := context.droppable
	indicator := context.dropIndicator

	// A column that fills its slot: in block layout the content region
	// below the tab bar would resolve to zero height.
	frame := TestSupport(tabGroupFrame(renderer, context, w, app)).
		Flex().
		FlexCol().
		SizeFull().
		OverflowHidden().
		TrackFocus(focusHandle).
		TabGroup().
		Child(renderer.RenderTabBar(context, w, app))

	content := TestSupport(tabGroupContentFrame(renderer, context, w, app)).Flex().FlexCol()
	// The region below the tab bar takes the rest of the group, except in
	// a collapsed one, which claims no space at all.
	if !context.IsCollapsed() {
		content.Flex1()
	}
	// A flex item's min-height is auto; flooring it at zero lets the
	// region win over its content.
	content.MinH(gpui.Px(0)).OverflowHidden()
	// Both drag kinds hang off droppable alone.
	if droppable {
		this := g.this
		content.OnDragMove(cx.Listener(func(g *TabGroup, e *gpui.DragMoveEvent[DragPanel], w *gpui.Window, cx *gpui.Context[TabGroup]) {
			g.onPanelDragMove(e, w, cx)
		}))
		gpui.OnDrop(content, func(drag *DragPanel, _ *gpui.MouseUpEvent, _ *gpui.Window, app *gpui.App) {
			_ = this.TryUpdate(app, func(g *TabGroup, cx *gpui.Context[TabGroup]) { g.onDrop(*drag, -1, true, cx) })
		})
		content.OnDragMove(cx.Listener(func(g *TabGroup, e *gpui.DragMoveEvent[AnyDrag], w *gpui.Window, cx *gpui.Context[TabGroup]) {
			g.onItemDragMove(e, w, cx)
		}))
		gpui.OnDrop(content, func(item *AnyDrag, _ *gpui.MouseUpEvent, _ *gpui.Window, app *gpui.App) {
			_ = this.TryUpdate(app, func(g *TabGroup, cx *gpui.Context[TabGroup]) {
				var placement Placement
				hasPlacement := false
				if g.dropIndicator != nil {
					placement, hasPlacement = g.dropIndicator.Placement()
				}
				g.emitDragDrop(*item, placement, hasPlacement, cx)
			})
		})
	}
	if context.activePanel != nil {
		content.Child(tabGroupRenderActivePanel(renderer, context.activePanel.View(), context, w, app))
	} else if empty, ok := tabGroupRenderEmpty(renderer, context, w, app); ok {
		content.Child(empty)
	}
	if indicator != nil {
		if drawn, ok := tabGroupRenderDropIndicator(renderer, *indicator, w, app); ok {
			content.Child(drawn)
		}
	}
	return frame.Child(content).IntoElement()
}

// TabGroupContext is what a skin needs to draw a tab group, and the
// callbacks it invokes rather than reimplementing behavior
// (U:dock/tab_group.rs:759-893).
type TabGroupContext struct {
	node                NodeID
	panels              []PanelView
	activePanel         PanelView
	activeIx            int
	zoomed              bool
	constraints         TabGroupConstraints
	draggable           bool
	droppable           bool
	activePanelClosable bool
	dropIndicator       *DropIndicator
	onSelectTab         func(ix int, w *gpui.Window, app *gpui.App)
	onClose             func(panel PanelID, w *gpui.Window, app *gpui.App)
	onToggleZoom        func(w *gpui.Window, app *gpui.App)
	onDropPanel         func(drag DragPanel, ix int, activate bool, w *gpui.Window, app *gpui.App)
	onDropItem          func(item AnyDrag, placement Placement, hasPlacement bool, w *gpui.Window, app *gpui.App)
}

// Node is the Tabs node this group mirrors (U:dock/tab_group.rs:787-791).
func (c *TabGroupContext) Node() NodeID { return c.node }

// Panels is every panel in the group, visible or not
// (U:dock/tab_group.rs:793-797).
func (c *TabGroupContext) Panels() []PanelView { return c.panels }

// ActiveIx is U:dock/tab_group.rs:799-801.
func (c *TabGroupContext) ActiveIx() int { return c.activeIx }

// ActivePanel is the panel on screen (U:dock/tab_group.rs:803-807).
func (c *TabGroupContext) ActivePanel() (PanelView, bool) {
	return c.activePanel, c.activePanel != nil
}

// DropIndicator is U:dock/tab_group.rs:809-811.
func (c *TabGroupContext) DropIndicator() (DropIndicator, bool) {
	if c.dropIndicator == nil {
		return DropIndicator{}, false
	}
	return *c.dropIndicator, true
}

// IsZoomed is U:dock/tab_group.rs:813-815.
func (c *TabGroupContext) IsZoomed() bool { return c.zoomed }

// IsCollapsed is U:dock/tab_group.rs:817-819.
func (c *TabGroupContext) IsCollapsed() bool { return c.constraints.IsCollapsed() }

// IsClosable reports whether the active panel can be closed
// (U:dock/tab_group.rs:821-824).
func (c *TabGroupContext) IsClosable() bool { return c.activePanelClosable }

// IsPanelClosable reports whether panel can be closed from this group
// (U:dock/tab_group.rs:826-835).
func (c *TabGroupContext) IsPanelClosable(panel PanelID, app *gpui.App) bool {
	if !c.constraints.IsClosable() || !c.draggable {
		return false
	}
	for _, candidate := range c.panels {
		if candidate.PanelID(app) == panel && candidate.Closable(app) {
			return true
		}
	}
	return false
}

// IsLocked is U:dock/tab_group.rs:837-839.
func (c *TabGroupContext) IsLocked() bool { return c.constraints.IsLocked() || c.zoomed }

// IsDraggable is U:dock/tab_group.rs:841-843.
func (c *TabGroupContext) IsDraggable() bool { return c.draggable }

// IsDroppable is U:dock/tab_group.rs:845-847.
func (c *TabGroupContext) IsDroppable() bool { return c.droppable }

// SelectTab is U:dock/tab_group.rs:849-851.
func (c *TabGroupContext) SelectTab(ix int, w *gpui.Window, app *gpui.App) { c.onSelectTab(ix, w, app) }

// Close is U:dock/tab_group.rs:853-855.
func (c *TabGroupContext) Close(panel PanelID, w *gpui.Window, app *gpui.App) {
	c.onClose(panel, w, app)
}

// ToggleZoom is U:dock/tab_group.rs:857-859.
func (c *TabGroupContext) ToggleZoom(w *gpui.Window, app *gpui.App) { c.onToggleZoom(w, app) }

// DragPanel is the drag payload for the tab at ix, for a skin wiring OnDrag
// onto its tabs (U:dock/tab_group.rs:861-867).
func (c *TabGroupContext) DragPanel(ix int, app *gpui.App) (DragPanel, bool) {
	if ix < 0 || ix >= len(c.panels) {
		return DragPanel{}, false
	}
	return NewDragPanel(c.panels[ix].PanelID(app), c.node), true
}

// DropPanel is a panel dropped on the tab bar: ix names the slot it lands
// in, negative to append (U:dock/tab_group.rs:869-880).
func (c *TabGroupContext) DropPanel(drag DragPanel, ix int, activate bool, w *gpui.Window, app *gpui.App) {
	c.onDropPanel(drag, ix, activate, w, app)
}

// DropItem is a host-owned drag dropped on the tab bar; no placement
// merges into this group (U:dock/tab_group.rs:882-892).
func (c *TabGroupContext) DropItem(item AnyDrag, placement Placement, hasPlacement bool, w *gpui.Window, app *gpui.App) {
	c.onDropItem(item, placement, hasPlacement, w, app)
}

// TabGroupRenderer is the appearance for a tab group; base draws none of
// it (U:dock/tab_group.rs:895-963). Only the tab bar is required; the other
// hooks are the optional TabGroupFrameRenderer,
// TabGroupContentFrameRenderer, TabGroupActivePanelRenderer,
// TabGroupDropIndicatorRenderer and TabGroupEmptyRenderer.
type TabGroupRenderer interface {
	RenderTabBar(group *TabGroupContext, w *gpui.Window, app *gpui.App) gpui.AnyElement
}

// TabGroupFrameRenderer supplies the group's outer frame, which base
// tracks focus on (U:dock/tab_group.rs:903-914). It must carry an id.
type TabGroupFrameRenderer interface {
	Frame(group *TabGroupContext, w *gpui.Window, app *gpui.App) *gpui.DivElement
}

// TabGroupContentFrameRenderer supplies the region below the tab bar,
// which base installs drop handling on (U:dock/tab_group.rs:916-924).
type TabGroupContentFrameRenderer interface {
	ContentFrame(group *TabGroupContext, w *gpui.Window, app *gpui.App) *gpui.DivElement
}

// TabGroupActivePanelRenderer places the displayed panel's view in the
// content region (U:dock/tab_group.rs:933-944).
type TabGroupActivePanelRenderer interface {
	RenderActivePanel(panel gpui.View, group *TabGroupContext, w *gpui.Window, app *gpui.App) gpui.AnyElement
}

// TabGroupDropIndicatorRenderer draws the drop indicator
// (U:dock/tab_group.rs:946-953); false draws nothing.
type TabGroupDropIndicatorRenderer interface {
	RenderDropIndicator(indicator DropIndicator, w *gpui.Window, app *gpui.App) (gpui.AnyElement, bool)
}

// TabGroupEmptyRenderer draws a group with nothing to display
// (U:dock/tab_group.rs:955-962); false draws nothing.
type TabGroupEmptyRenderer interface {
	RenderEmpty(group *TabGroupContext, w *gpui.Window, app *gpui.App) (gpui.AnyElement, bool)
}

func tabGroupFrame(r TabGroupRenderer, group *TabGroupContext, w *gpui.Window, app *gpui.App) *gpui.DivElement {
	if f, ok := r.(TabGroupFrameRenderer); ok {
		return f.Frame(group, w, app)
	}
	// Gap 2b: the plain upstream div() is a block.
	return gpui.Div().ID("tab-group").Block()
}

func tabGroupContentFrame(r TabGroupRenderer, group *TabGroupContext, w *gpui.Window, app *gpui.App) *gpui.DivElement {
	if f, ok := r.(TabGroupContentFrameRenderer); ok {
		return f.ContentFrame(group, w, app)
	}
	// Gap 2b: the plain upstream div() is a block.
	return gpui.Div().ID("tab-group-content").Block()
}

func tabGroupRenderActivePanel(r TabGroupRenderer, panel gpui.View, group *TabGroupContext, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	if f, ok := r.(TabGroupActivePanelRenderer); ok {
		return f.RenderActivePanel(panel, group, w, app)
	}
	element, err := gpui.TryIntoElement(panel)
	if err != nil {
		panic(err)
	}
	return element
}

func tabGroupRenderDropIndicator(r TabGroupRenderer, indicator DropIndicator, w *gpui.Window, app *gpui.App) (gpui.AnyElement, bool) {
	if f, ok := r.(TabGroupDropIndicatorRenderer); ok {
		return f.RenderDropIndicator(indicator, w, app)
	}
	return gpui.AnyElement{}, false
}

func tabGroupRenderEmpty(r TabGroupRenderer, group *TabGroupContext, w *gpui.Window, app *gpui.App) (gpui.AnyElement, bool) {
	if f, ok := r.(TabGroupEmptyRenderer); ok {
		return f.RenderEmpty(group, w, app)
	}
	return gpui.AnyElement{}, false
}

// bareTabGroup is the renderer a group starts with: the displayed panel
// and nothing else (U:dock/tab_group.rs:965-972).
type bareTabGroup struct{}

func (bareTabGroup) RenderTabBar(*TabGroupContext, *gpui.Window, *gpui.App) gpui.AnyElement {
	return gpui.Empty()
}
