package base

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ported tests of U:dock/tab_group.rs (27/27).

// groupNode is the node buildGroup gives its group
// (U:dock/tab_group.rs:988-991).
func groupNode() NodeID { return NodeIDFromU64(1) }

// elsewhere is U:dock/tab_group.rs:993-995.
func elsewhere() NodeID { return NodeIDFromU64(7) }

// contentBounds is U:dock/tab_group.rs:997-1002.
func contentBounds() gpui.Bounds { return gpui.Bounds{Size: gpui.Size{Width: 400, Height: 300}} }

// eventObserver subscribes to a group from outside it (the upstream
// App-level cx.subscribe).
type eventObserver struct{}

// recordEvents collects the group's outgoing intents as readable strings
// (U:dock/tab_group.rs:1004-1019).
func recordEvents(group gpui.Entity[TabGroup], cx *gpui.VisualTestContext) *[]string {
	events := new([]string)
	cx.Update(func(_ *gpui.Window, app *gpui.App) {
		observer := gpui.NewEntity(app, app.RootScope(), func(*eventObserver, *gpui.Context[eventObserver]) {})
		observer.Update(app, func(_ *eventObserver, ocx *gpui.Context[eventObserver]) {
			TabGroupEvents.Subscribe(ocx, group, func(_ *eventObserver, _ gpui.Entity[TabGroup], e *TabGroupEvent, _ *gpui.Context[eventObserver]) {
				*events = append(*events, describeTabGroupEvent(*e))
			})
		})
	})
	return events
}

func debugOptionUsize(ix int, has bool) string {
	if !has {
		return "None"
	}
	return fmt.Sprintf("Some(%d)", ix)
}

func debugOptionPlacement(p Placement, has bool) string {
	if !has {
		return "None"
	}
	return fmt.Sprintf("Some(%v)", p)
}

// describeTabGroupEvent is U:dock/tab_group.rs:1021-1056.
func describeTabGroupEvent(event TabGroupEvent) string {
	switch e := event.(type) {
	case TabGroupDrop:
		switch target := e.Target.(type) {
		case InsertTargetTabs:
			return fmt.Sprintf("drop panel %d from %d into tabs %d at %s activate=%v",
				e.Panel.AsU64(), e.Source.AsU64(), target.Node.AsU64(), debugOptionUsize(target.Ix, target.HasIx), target.Activate)
		case InsertTargetSplit:
			return fmt.Sprintf("drop panel %d from %d split %d %v", e.Panel.AsU64(), e.Source.AsU64(), target.Node.AsU64(), target.Placement)
		}
	case TabGroupDragDrop:
		p, ok := e.Target.Placement()
		return fmt.Sprintf("item onto %d at %s", e.Target.Node().AsU64(), debugOptionPlacement(p, ok))
	case TabGroupClosePanel:
		return fmt.Sprintf("close %d", e.Panel.AsU64())
	case TabGroupActiveChanged:
		return fmt.Sprintf("active %d", e.Ix)
	case TabGroupZoomIn:
		return "zoom in"
	case TabGroupZoomOut:
		return "zoom out"
	}
	return fmt.Sprintf("%#v", event)
}

func updateGroup(cx *gpui.VisualTestContext, group gpui.Entity[TabGroup], f func(g *TabGroup, w *gpui.Window, gcx *gpui.Context[TabGroup])) {
	cx.Update(func(w *gpui.Window, app *gpui.App) {
		group.Update(app, func(g *TabGroup, gcx *gpui.Context[TabGroup]) { f(g, w, gcx) })
	})
}

func readGroup[R any](cx *gpui.VisualTestContext, group gpui.Entity[TabGroup], f func(g *TabGroup, app *gpui.App) R) R {
	return group.Read(cx, f)
}

func groupContext(cx *gpui.VisualTestContext, group gpui.Entity[TabGroup]) *TabGroupContext {
	return readGroup(cx, group, func(g *TabGroup, app *gpui.App) *TabGroupContext { return g.Context(app) })
}

func setConstraints(cx *gpui.VisualTestContext, group gpui.Entity[TabGroup], c TabGroupConstraints) {
	updateGroup(cx, group, func(g *TabGroup, w *gpui.Window, gcx *gpui.Context[TabGroup]) { g.SetConstraints(c, w, gcx) })
}

func requireEvents(t *testing.T, events *[]string, want ...string) {
	t.Helper()
	if !slices.Equal(*events, want) {
		t.Fatalf("events = %q, want %q", *events, want)
	}
}

// U:dock/tab_group.rs:1058-1066
func TestDockTabGroupAGroupAnnouncesItsFirstPanelActive(t *testing.T) {
	log := newDockLog()
	_, _, cx := buildGroup(t, log, "a")
	cx.RunUntilParked()
	requireActive(t, drainActive(log), activeEntry{"a", true})
}

// U:dock/tab_group.rs:1068-1081
func TestDockTabGroupSelectingAnotherTabDeactivatesThenActivates(t *testing.T) {
	log := newDockLog()
	group, _, cx := buildGroup(t, log, "a", "b")
	cx.RunUntilParked()
	drainActive(log)
	updateGroup(cx, group, func(g *TabGroup, w *gpui.Window, gcx *gpui.Context[TabGroup]) { g.SelectTab(1, w, gcx) })
	cx.RunUntilParked()
	requireActive(t, drainActive(log), activeEntry{"a", false}, activeEntry{"b", true})
}

// U:dock/tab_group.rs:1083-1100
func TestDockTabGroupTheContextSnapshotsTheGroupState(t *testing.T) {
	log := newDockLog()
	group, _, cx := buildGroup(t, log, "a", "b")
	ctx := groupContext(cx, group)
	if len(ctx.Panels()) != 2 || ctx.ActiveIx() != 0 || ctx.IsZoomed() {
		t.Fatalf("context = %d panels, active %d, zoomed %v", len(ctx.Panels()), ctx.ActiveIx(), ctx.IsZoomed())
	}
}

// U:dock/tab_group.rs:1102-1119
func TestDockTabGroupAHiddenDisplayedPanelIsStillTheActiveOne(t *testing.T) {
	log := newDockLog()
	group, panels, cx := buildGroup(t, log, "a", "b")
	cx.Update(func(_ *gpui.Window, app *gpui.App) {
		panels[0].Update(app, func(p *testPanel, pcx *gpui.Context[testPanel]) { p.setVisible(false, pcx) })
	})
	cx.RunUntilParked()
	requireActive(t, drainActive(log), activeEntry{"a", true})
	name := readGroup(cx, group, func(g *TabGroup, app *gpui.App) string {
		panel, _ := g.ActivePanel(app)
		return panel.PanelName(app)
	})
	if name != "b" {
		t.Fatalf("rendering falls back to the first visible panel: got %q", name)
	}
}

// U:dock/tab_group.rs:1121-1139
func TestDockTabGroupAPanelDroppedFromTheGroupIsNeverToldItWentInactive(t *testing.T) {
	log := newDockLog()
	group, panels, cx := buildGroup(t, log, "a", "b")
	cx.RunUntilParked()
	drainLog(log)
	updateGroup(cx, group, func(g *TabGroup, w *gpui.Window, gcx *gpui.Context[TabGroup]) {
		g.SyncFromTree([]PanelView{testPanelView(panels[1])}, 0, w, gcx)
	})
	cx.RunUntilParked()
	requireActive(t, drainActive(log), activeEntry{"b", true})
}

// U:dock/tab_group.rs:1141-1157
func TestDockTabGroupSelectingATabThatIsNotThereChangesNothing(t *testing.T) {
	log := newDockLog()
	group, _, cx := buildGroup(t, log, "a", "b")
	cx.RunUntilParked()
	drainActive(log)
	events := recordEvents(group, cx)
	updateGroup(cx, group, func(g *TabGroup, w *gpui.Window, gcx *gpui.Context[TabGroup]) { g.SelectTab(5, w, gcx) })
	cx.RunUntilParked()
	if ix := readGroup(cx, group, func(g *TabGroup, _ *gpui.App) int { return g.ActiveIx() }); ix != 0 {
		t.Fatalf("active_ix = %d", ix)
	}
	requireEvents(t, events)
	requireActive(t, drainActive(log))
}

// U:dock/tab_group.rs:1159-1189
func TestDockTabGroupALockedGroupCanBeNeitherDraggedNorDroppedInto(t *testing.T) {
	log := newDockLog()
	group, _, cx := buildGroup(t, log, "a", "b")
	setConstraints(cx, group, TabGroupConstraintsInSplit(false))
	unlocked := groupContext(cx, group)
	setConstraints(cx, group, TabGroupConstraintsInSplit(false).DockLocked(true))
	locked := groupContext(cx, group)
	if !unlocked.IsDraggable() || !unlocked.IsDroppable() {
		t.Fatal("an unlocked group is draggable and droppable")
	}
	if locked.IsDraggable() || locked.IsDroppable() {
		t.Fatal("a locked group is neither draggable nor droppable")
	}
}

// U:dock/tab_group.rs:1191-1206
func TestDockTabGroupAZoomedGroupTakesNoDrops(t *testing.T) {
	log := newDockLog()
	group, _, cx := buildGroup(t, log, "a", "b")
	updateGroup(cx, group, func(g *TabGroup, w *gpui.Window, gcx *gpui.Context[TabGroup]) {
		g.SetConstraints(TabGroupConstraintsInSplit(false), w, gcx)
		g.setZoomed(true, w, gcx)
	})
	if groupContext(cx, group).IsDroppable() {
		t.Fatal("a zoomed group took drops")
	}
}

// U:dock/tab_group.rs:1208-1230
func TestDockTabGroupTheOnlyGroupsLastVisiblePanelCannotBeDraggedOut(t *testing.T) {
	log := newDockLog()
	group, _, cx := buildGroup(t, log, "a")
	setConstraints(cx, group, TabGroupConstraintsInSplit(true))
	alone := groupContext(cx, group).IsDraggable()
	setConstraints(cx, group, TabGroupConstraintsInSplit(false))
	besideOthers := groupContext(cx, group).IsDraggable()
	if alone || !besideOthers {
		t.Fatalf("draggable alone = %v, beside others = %v", alone, besideOthers)
	}
}

// U:dock/tab_group.rs:1232-1248
func TestDockTabGroupAPanelFromAnotherGroupLandsAsOneMove(t *testing.T) {
	log := newDockLog()
	group, _, cx := buildGroup(t, log, "a")
	events := recordEvents(group, cx)
	drag := NewDragPanel(PanelIDFromU64(99), elsewhere())
	updateGroup(cx, group, func(g *TabGroup, _ *gpui.Window, gcx *gpui.Context[TabGroup]) { g.onDrop(drag, -1, true, gcx) })
	cx.RunUntilParked()
	requireEvents(t, events, "drop panel 99 from 7 into tabs 1 at None activate=true")
}

// U:dock/tab_group.rs:1250-1264
func TestDockTabGroupDroppingAPanelBackOntoItsOwnGroupIsIgnored(t *testing.T) {
	log := newDockLog()
	group, panels, cx := buildGroup(t, log, "a", "b")
	events := recordEvents(group, cx)
	drag := NewDragPanel(testPanelID(panels[0]), groupNode())
	updateGroup(cx, group, func(g *TabGroup, _ *gpui.Window, gcx *gpui.Context[TabGroup]) { g.onDrop(drag, -1, true, gcx) })
	cx.RunUntilParked()
	requireEvents(t, events)
}

func hoverAndDrop(cx *gpui.VisualTestContext, group gpui.Entity[TabGroup], drag DragPanel, placement Placement, ix int) {
	updateGroup(cx, group, func(g *TabGroup, _ *gpui.Window, gcx *gpui.Context[TabGroup]) {
		g.syncDropPlaceholder(contentBounds(), placement, true, drag.DragSessionID(),
			DropPlaceholderBoundsForPlacement(contentBounds(), 0, false), gcx)
		g.onDrop(drag, ix, true, gcx)
	})
	cx.RunUntilParked()
}

// U:dock/tab_group.rs:1266-1288
func TestDockTabGroupAHoveringSplitTurnsADropIntoASplit(t *testing.T) {
	log := newDockLog()
	group, _, cx := buildGroup(t, log, "a")
	events := recordEvents(group, cx)
	hoverAndDrop(cx, group, NewDragPanel(PanelIDFromU64(99), elsewhere()), PlacementRight, -1)
	requireEvents(t, events, "drop panel 99 from 7 split 1 Right")
}

// U:dock/tab_group.rs:1290-1317
func TestDockTabGroupATabSlotOverridesTheHoveringSplit(t *testing.T) {
	log := newDockLog()
	group, _, cx := buildGroup(t, log, "a")
	events := recordEvents(group, cx)
	hoverAndDrop(cx, group, NewDragPanel(PanelIDFromU64(99), elsewhere()), PlacementRight, 0)
	requireEvents(t, events, "drop panel 99 from 7 into tabs 1 at Some(0) activate=true")
}

// U:dock/tab_group.rs:1319-1343
func TestDockTabGroupALonePanelCannotSplitOutOfItsOwnGroup(t *testing.T) {
	log := newDockLog()
	group, panels, cx := buildGroup(t, log, "a")
	events := recordEvents(group, cx)
	hoverAndDrop(cx, group, NewDragPanel(testPanelID(panels[0]), groupNode()), PlacementRight, -1)
	requireEvents(t, events)
}

// U:dock/tab_group.rs:1345-1375
func TestDockTabGroupThePlaceholderReplaysOnlyWhenTheTargetMoves(t *testing.T) {
	log := newDockLog()
	group, _, cx := buildGroup(t, log, "a")
	bounds := contentBounds()
	source := NewDropPlaceholderBounds(gpui.Point{X: 10, Y: 10}, gpui.Size{Width: 96, Height: 30})
	sync := func(placement Placement) DropIndicator {
		var out DropIndicator
		updateGroup(cx, group, func(g *TabGroup, _ *gpui.Window, gcx *gpui.Context[TabGroup]) {
			g.syncDropPlaceholder(bounds, placement, true, 42, source, gcx)
			out = *g.dropIndicator
		})
		return out
	}
	first := sync(PlacementLeft)
	again := sync(PlacementLeft)
	moved := sync(PlacementRight)
	if first.Epoch() != 0 || first.From() != source {
		t.Fatalf("the first run flies in from the drag: %+v", first)
	}
	if again.Epoch() != 0 || again.From() != source {
		t.Fatalf("an unchanged target keeps animating: %+v", again)
	}
	if moved.Epoch() != 1 || moved.From() != first.To() {
		t.Fatalf("a moved target replays from where it was: %+v", moved)
	}
	if moved.To() != DropPlaceholderBoundsForPlacement(bounds, PlacementRight, true) {
		t.Fatalf("moved.to = %+v", moved.To())
	}
}

// U:dock/tab_group.rs:1377-1402
func TestDockTabGroupTheIndicatorIsWithheldWhenNoDragIsInFlight(t *testing.T) {
	log := newDockLog()
	group, _, cx := buildGroup(t, log, "a")
	var stored, published bool
	updateGroup(cx, group, func(g *TabGroup, _ *gpui.Window, gcx *gpui.Context[TabGroup]) {
		g.syncDropPlaceholder(contentBounds(), PlacementTop, true, 42, DropPlaceholderBoundsForPlacement(contentBounds(), 0, false), gcx)
		stored = g.dropIndicator != nil
		_, published = g.Context(gcx.App()).DropIndicator()
	})
	if !stored || published {
		t.Fatalf("stored = %v, published = %v", stored, published)
	}
}

// U:dock/tab_group.rs:1404-1418
func TestDockTabGroupAHostDragReportsTheGroupAndTheEdgeItResolved(t *testing.T) {
	log := newDockLog()
	group, _, cx := buildGroup(t, log, "a")
	events := recordEvents(group, cx)
	updateGroup(cx, group, func(g *TabGroup, _ *gpui.Window, gcx *gpui.Context[TabGroup]) {
		g.emitDragDrop(NewAnyDrag(uint32(7)), PlacementBottom, true, gcx)
	})
	cx.RunUntilParked()
	requireEvents(t, events, "item onto 1 at Some(Bottom)")
}

// U:dock/tab_group.rs:1420-1442
func TestDockTabGroupClosingAsksTheContainerRatherThanEditingTheGroup(t *testing.T) {
	log := newDockLog()
	group, panels, cx := buildGroup(t, log, "a", "b")
	events := recordEvents(group, cx)
	panel := testPanelID(panels[1])
	updateGroup(cx, group, func(g *TabGroup, _ *gpui.Window, gcx *gpui.Context[TabGroup]) {
		g.ClosePanel(panel, gcx)
		// A panel that is not a member is not this group's to close.
		g.ClosePanel(PanelIDFromU64(4242), gcx)
	})
	cx.RunUntilParked()
	requireEvents(t, events, fmt.Sprintf("close %d", panel.AsU64()))
	if n := readGroup(cx, group, func(g *TabGroup, _ *gpui.App) int { return len(g.Panels()) }); n != 2 {
		t.Fatalf("the group holds %d panels; it keeps both until the container edits the tree", n)
	}
}

func zoomSignals(log dockLog) []dockLogEntry {
	var out []dockLogEntry
	for _, e := range drainLog(log) {
		if e.signal.kind == signalZoomed {
			out = append(out, e)
		}
	}
	return out
}

// U:dock/tab_group.rs:1444-1468
func TestDockTabGroupZoomingTogglesAndTellsTheDisplayedPanel(t *testing.T) {
	log := newDockLog()
	group, _, cx := buildGroup(t, log, "a")
	events := recordEvents(group, cx)
	toggle := func(g *TabGroup, w *gpui.Window, gcx *gpui.Context[TabGroup]) { g.ToggleZoom(w, gcx) }
	updateGroup(cx, group, toggle)
	cx.RunUntilParked()
	updateGroup(cx, group, toggle)
	cx.RunUntilParked()
	requireEvents(t, events, "zoom in", "zoom out")
	if readGroup(cx, group, func(g *TabGroup, _ *gpui.App) bool { return g.IsZoomed() }) {
		t.Fatal("the group is still zoomed")
	}
	if got, want := zoomSignals(log), []dockLogEntry{{"a", sigZoomed(true)}, {"a", sigZoomed(false)}}; !slices.Equal(got, want) {
		t.Fatalf("zoom signals = %v, want %v", got, want)
	}
}

// U:dock/tab_group.rs:1470-1493
func TestDockTabGroupADragOverALockedGroupResolvesNothing(t *testing.T) {
	group, _, cx := buildSkinnedGroup(t, "a", "b")
	setConstraints(cx, group, TabGroupConstraintsInSplit(false).DockLocked(true))
	drawFrame(t, cx)
	dragFromTheTabIntoTheContent(cx)
	if readGroup(cx, group, func(g *TabGroup, _ *gpui.App) bool { return g.dropIndicator != nil }) {
		t.Fatal("a locked group installs no drag-move listener, so nothing resolved")
	}
}

// U:dock/tab_group.rs:1495-1518
func TestDockTabGroupAGroupTheContainerSealedShutRefusesToClose(t *testing.T) {
	log := newDockLog()
	group, panels, cx := buildGroup(t, log, "a", "b")
	events := recordEvents(group, cx)
	panel := testPanelID(panels[0])
	updateGroup(cx, group, func(g *TabGroup, w *gpui.Window, gcx *gpui.Context[TabGroup]) {
		g.SetConstraints(TabGroupConstraintsInSplit(false).Closable(false), w, gcx)
		g.ClosePanel(panel, gcx)
	})
	cx.RunUntilParked()
	if groupContext(cx, group).IsClosable() {
		t.Fatal("a sealed group reports itself closable")
	}
	requireEvents(t, events)
}

// U:dock/tab_group.rs:1520-1546
func TestDockTabGroupTheOnlyGroupsLastPanelIsNotClosable(t *testing.T) {
	log := newDockLog()
	group, _, cx := buildGroup(t, log, "a")
	setConstraints(cx, group, TabGroupConstraintsInSplit(true))
	alone := groupContext(cx, group).IsClosable()
	setConstraints(cx, group, TabGroupConstraintsInSplit(false))
	besideASibling := groupContext(cx, group).IsClosable()
	if alone || !besideASibling {
		t.Fatalf("closable alone = %v, beside a sibling = %v", alone, besideASibling)
	}
}

// U:dock/tab_group.rs:1548-1578
func TestDockTabGroupCollapsingDeactivatesTheDisplayedPanelAndExpandingRestoresIt(t *testing.T) {
	log := newDockLog()
	group, _, cx := buildGroup(t, log, "a", "b")
	cx.RunUntilParked()
	requireActive(t, drainActive(log), activeEntry{"a", true})
	setConstraints(cx, group, TabGroupConstraintsInSplit(false).Collapsed(true))
	cx.RunUntilParked()
	requireActive(t, drainActive(log), activeEntry{"a", false})
	setConstraints(cx, group, TabGroupConstraintsInSplit(false))
	cx.RunUntilParked()
	requireActive(t, drainActive(log), activeEntry{"a", true})
}

// skinContent is where recordingRenderer puts its content frame
// (U:dock/tab_group.rs:1582-1590).
func skinContent() gpui.Bounds {
	return gpui.Bounds{Origin: gpui.Point{X: 100, Y: 50}, Size: gpui.Size{Width: 400, Height: 300}}
}

// recordingRenderer records which hooks base calls, and lays its two
// frames out at known coordinates (U:dock/tab_group.rs:1592-1674).
type recordingRenderer struct {
	calls *[]string
}

func (r recordingRenderer) saw(call string) { *r.calls = append(*r.calls, call) }

func (r recordingRenderer) Frame(*TabGroupContext, *gpui.Window, *gpui.App) *gpui.DivElement {
	r.saw("frame")
	return gpui.Div().ID("skin-frame").Block().Relative().SizeFull()
}

func (r recordingRenderer) ContentFrame(*TabGroupContext, *gpui.Window, *gpui.App) *gpui.DivElement {
	r.saw("content_frame")
	b := skinContent()
	return gpui.Div().ID("skin-content").Block().Absolute().
		Left(gpui.Px(b.Origin.X)).Top(gpui.Px(b.Origin.Y)).W(gpui.Px(b.Size.Width)).H(gpui.Px(b.Size.Height))
}

func (r recordingRenderer) RenderTabBar(group *TabGroupContext, _ *gpui.Window, app *gpui.App) gpui.AnyElement {
	r.saw("tab_bar")
	tab := gpui.Div().ID("skin-tab").Block().Absolute().Left(gpui.Px(0)).Top(gpui.Px(0)).W(gpui.Px(80)).H(gpui.Px(24))
	if drag, ok := group.DragPanel(0, app); ok {
		tab.OnDrag(drag, func(drag *DragPanel, offset gpui.Point, w *gpui.Window, app *gpui.App) gpui.View {
			drag.SetDragOffset(offset)
			value := *drag
			return gpui.ViewOf(gpui.NewEntity(app, w.Scope(), func(v *DragPanel, _ *gpui.Context[DragPanel]) { *v = value }))
		})
	}
	return tab.IntoElement()
}

func (r recordingRenderer) RenderActivePanel(panel gpui.View, _ *TabGroupContext, _ *gpui.Window, _ *gpui.App) gpui.AnyElement {
	r.saw("active_panel")
	element, err := gpui.TryIntoElement(panel)
	if err != nil {
		panic(err)
	}
	return element
}

func (r recordingRenderer) RenderEmpty(*TabGroupContext, *gpui.Window, *gpui.App) (gpui.AnyElement, bool) {
	r.saw("empty")
	return gpui.AnyElement{}, false
}

func (r recordingRenderer) RenderDropIndicator(DropIndicator, *gpui.Window, *gpui.App) (gpui.AnyElement, bool) {
	r.saw("drop_indicator")
	return gpui.AnyElement{}, false
}

// buildSkinnedGroup is U:dock/tab_group.rs:1676-1712.
func buildSkinnedGroup(t *testing.T, names ...string) (gpui.Entity[TabGroup], *[]string, *gpui.VisualTestContext) {
	t.Helper()
	calls := new([]string)
	renderer := recordingRenderer{calls: calls}
	ta := gpui.NewTestApp()
	shutdownAtCleanup(t, ta)
	group, cx := ta.AddWindowView(func(g *TabGroup, w *gpui.Window, gcx *gpui.Context[TabGroup]) {
		*g = NewTabGroup(NodeIDFromU64(1), w, gcx)
		g.WithRenderer(renderer)
	})
	log := newDockLog()
	cx.Update(func(w *gpui.Window, app *gpui.App) {
		views := make([]PanelView, 0, len(names))
		for _, name := range names {
			views = append(views, testPanelView(loggingTestPanel(name, log, app)))
		}
		group.Update(app, func(g *TabGroup, gcx *gpui.Context[TabGroup]) {
			g.SetConstraints(TabGroupConstraintsInSplit(false), w, gcx)
			g.SyncFromTree(views, 0, w, gcx)
		})
	})
	cx.RunUntilParked()
	*calls = nil
	return group, calls, cx
}

// dragFromTheTabIntoTheContent presses on the skin's tab, starts the drag
// and moves into the right-hand third of skinContent
// (U:dock/tab_group.rs:1714-1732).
func dragFromTheTabIntoTheContent(cx *gpui.VisualTestContext) {
	left := gpui.MouseButtonLeft
	cx.SimulateMouseDown(gpui.Point{X: 20, Y: 10}, gpui.MouseButtonLeft, gpui.NoModifiers())
	cx.SimulateMouseMove(gpui.Point{X: 30, Y: 14}, &left, gpui.NoModifiers())
	cx.SimulateMouseMove(gpui.Point{X: 450, Y: 200}, &left, gpui.NoModifiers())
}

func requireCalls(t *testing.T, calls *[]string, want ...string) {
	t.Helper()
	if !slices.Equal(*calls, want) {
		t.Fatalf("calls = %q, want %q", *calls, want)
	}
}

// U:dock/tab_group.rs:1734-1747
func TestDockTabGroupTheRendererComposesFrameThenTabBarThenContent(t *testing.T) {
	_, calls, cx := buildSkinnedGroup(t, "a")
	drawFrame(t, cx)
	requireCalls(t, calls, "frame", "tab_bar", "content_frame", "active_panel")
}

// U:dock/tab_group.rs:1749-1761
func TestDockTabGroupAnEmptyGroupAsksTheRendererForItsEmptyState(t *testing.T) {
	_, calls, cx := buildSkinnedGroup(t)
	drawFrame(t, cx)
	requireCalls(t, calls, "frame", "tab_bar", "content_frame", "empty")
}

// U:dock/tab_group.rs:1763-1789
func TestDockTabGroupTheContentFrameIsWhatADragIsMeasuredAgainst(t *testing.T) {
	group, calls, cx := buildSkinnedGroup(t, "a", "b")
	drawFrame(t, cx)
	dragFromTheTabIntoTheContent(cx)
	indicator := readGroup(cx, group, func(g *TabGroup, _ *gpui.App) *DropIndicator { return g.dropIndicator })
	if indicator == nil {
		t.Fatal("the content frame's drag-move listener did not run")
	}
	if indicator.Bounds() != skinContent() {
		t.Fatalf("bounds = %v, want the content frame's %v", indicator.Bounds(), skinContent())
	}
	if p, ok := indicator.Placement(); !ok || p != PlacementRight {
		t.Fatalf("placement = %v %v, want Right", p, ok)
	}
	drawFrame(t, cx)
	if !slices.Contains(*calls, "drop_indicator") {
		t.Fatalf("a published indicator is handed to the renderer to draw: calls = %q", *calls)
	}
}

// U:dock/tab_group.rs:1791-1814
func TestDockTabGroupDroppingOnTheContentFrameReportsTheMove(t *testing.T) {
	group, _, cx := buildSkinnedGroup(t, "a", "b")
	events := recordEvents(group, cx)
	drawFrame(t, cx)
	dragFromTheTabIntoTheContent(cx)
	cx.SimulateMouseUp(gpui.Point{X: 450, Y: 200}, gpui.MouseButtonLeft, gpui.NoModifiers())
	cx.RunUntilParked()
	if len(*events) != 1 || !strings.HasSuffix((*events)[0], "split 1 Right") {
		t.Fatalf("events = %q", *events)
	}
	if readGroup(cx, group, func(g *TabGroup, _ *gpui.App) bool { return g.dropIndicator != nil }) {
		t.Fatal("the drop did not clear the hover state")
	}
}
