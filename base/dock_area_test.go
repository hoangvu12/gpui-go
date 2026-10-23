package base

import (
	"cmp"
	"encoding/json"
	"math"
	"slices"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ported tests of U:dock/dock_area.rs (57/57).

// containerEntityIDs is every cached container as (node, entity), sorted
// (U:dock/dock_area.rs:1937-1959).
func (a *DockArea) containerEntityIDs() [][2]uint64 {
	var ids [][2]uint64
	for node, cached := range a.groups {
		ids = append(ids, [2]uint64{node.AsU64(), uint64(cached.entity.EntityID())})
	}
	for node, cached := range a.splits {
		ids = append(ids, [2]uint64{node.AsU64(), uint64(cached.entity.EntityID())})
	}
	slices.SortFunc(ids, func(a, b [2]uint64) int {
		if c := cmp.Compare(a[0], b[0]); c != 0 {
			return c
		}
		return cmp.Compare(a[1], b[1])
	})
	return ids
}

func sizesOf(values ...float32) []*float32 {
	out := make([]*float32, len(values))
	for i := range values {
		out[i] = &values[i]
	}
	return out
}

// U:dock/dock_area.rs:1974-1981
func TestDockAreaSlotSizesAreReExpressedAsSharesOfTheContainer(t *testing.T) {
	requireSizes(t, scaleSizesTo(800, sizesOf(300, 100)), sizesOf(600, 200), "scaled")
}

// U:dock/dock_area.rs:1983-1990
func TestDockAreaAnUnconstrainedSlotLeavesEverySizeAlone(t *testing.T) {
	sizes := optSizes(dpx(300), nil)
	requireSizes(t, scaleSizesTo(800, sizes), sizes, "unconstrained")
}

// U:dock/dock_area.rs:1992-2001
func TestDockAreaAnUnusableContainerOrTotalLeavesEverySizeAlone(t *testing.T) {
	sizes := sizesOf(300, 100)
	requireSizes(t, scaleSizesTo(0, sizes), sizes, "no container")
	zeroed := sizesOf(0, 0)
	requireSizes(t, scaleSizesTo(800, zeroed), zeroed, "no total")
}

// openDockArea opens a DockArea window in ta (U:dock/dock_area.rs:2003-2008
// setup, with an optional renderer).
func openDockArea(ta *gpui.TestApp, renderer DockAreaRenderer) (gpui.Entity[DockArea], *gpui.VisualTestContext) {
	ta.Update(func(app *gpui.App) { ThemeGlobalMut(app) })
	return ta.AddWindowView(func(a *DockArea, w *gpui.Window, cx *gpui.Context[DockArea]) {
		*a = NewDockArea("test-dock", nil, w, cx)
		if renderer != nil {
			a.WithRenderer(renderer)
		}
	})
}

// setupDockArea is U:dock/dock_area.rs:2003-2008.
func setupDockArea(t *testing.T) (gpui.Entity[DockArea], *gpui.VisualTestContext) {
	t.Helper()
	ta := gpui.NewTestApp()
	shutdownAtCleanup(t, ta)
	return openDockArea(ta, nil)
}

func updateArea(cx *gpui.VisualTestContext, area gpui.Entity[DockArea], f func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea])) {
	cx.Update(func(w *gpui.Window, app *gpui.App) {
		area.Update(app, func(a *DockArea, acx *gpui.Context[DockArea]) { f(a, w, acx) })
	})
}

func readArea[R any](cx *gpui.VisualTestContext, area gpui.Entity[DockArea], f func(a *DockArea, app *gpui.App) R) R {
	return area.Read(cx, f)
}

func dumpArea(cx *gpui.VisualTestContext, area gpui.Entity[DockArea]) DockAreaState {
	return readArea(cx, area, func(a *DockArea, app *gpui.App) DockAreaState { return a.Dump(app) })
}

// countLayoutEvents counts DockEventLayoutChanged from outside the area.
func countLayoutEvents(cx *gpui.VisualTestContext, area gpui.Entity[DockArea]) *int {
	count := new(int)
	cx.Update(func(_ *gpui.Window, app *gpui.App) {
		observer := gpui.NewEntity(app, app.RootScope(), func(*eventObserver, *gpui.Context[eventObserver]) {})
		observer.Update(app, func(_ *eventObserver, ocx *gpui.Context[eventObserver]) {
			DockEvents.Subscribe(ocx, area, func(_ *eventObserver, _ gpui.Entity[DockArea], e *DockEvent, _ *gpui.Context[eventObserver]) {
				if _, ok := (*e).(DockEventLayoutChanged); ok {
					*count++
				}
			})
		})
	})
	return count
}

func centerRoot(cx *gpui.VisualTestContext, area gpui.Entity[DockArea]) NodeID {
	return readArea(cx, area, func(a *DockArea, _ *gpui.App) NodeID {
		tree, _ := a.Layout(DockPlacementCenter)
		return tree.Root().ID()
	})
}

func splitSizesOf(cx *gpui.VisualTestContext, area gpui.Entity[DockArea], node NodeID) []float32 {
	return readArea(cx, area, func(a *DockArea, app *gpui.App) []float32 {
		cached, ok := a.splits[node]
		if !ok {
			panic("no cached split for the node")
		}
		return cached.entity.Read(app, func(s *ResizableState, _ *gpui.App) []float32 { return slices.Clone(s.Sizes()) })
	})
}

// U:dock/dock_area.rs:2010-2045
func TestDockAreaDockSizeChangeEmitsOneLayoutEvent(t *testing.T) {
	area, cx := setupDockArea(t)
	updateArea(cx, area, func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) {
		a.SetDock(DockPlacementLeft, DockLayoutTabs().Panel(newTestPanel("Left", acx.App())), w, acx)
	})
	events := countLayoutEvents(cx, area)
	updateArea(cx, area, func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) {
		a.SetDockSize(DockPlacementLeft, 320, w, acx)
		a.SetDockSize(DockPlacementLeft, 320, w, acx)
	})
	if *events != 1 {
		t.Fatalf("events = %d: only an effective size change is persisted", *events)
	}
}

// twoGroups is two tab groups side by side, holding one logging panel each
// (U:dock/dock_area.rs:2047-2073).
func twoGroups(t *testing.T, log dockLog) (gpui.Entity[DockArea], gpui.Entity[testPanel], *gpui.VisualTestContext) {
	t.Helper()
	area, cx := setupDockArea(t)
	var alpha gpui.Entity[testPanel]
	updateArea(cx, area, func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) {
		alpha = loggingTestPanel("Alpha", log, acx.App())
		beta := loggingTestPanel("Beta", log, acx.App())
		a.SetCenter(DockLayoutHSplit().
			Child(DockLayoutTabs().Panel(alpha), nil).
			Child(DockLayoutTabs().Panel(beta), nil), w, acx)
	})
	return area, alpha, cx
}

// childNode is the id of the center split's ix-th child container
// (U:dock/dock_area.rs:2075-2089).
func childNode(cx *gpui.VisualTestContext, area gpui.Entity[DockArea], ix int) NodeID {
	return readArea(cx, area, func(a *DockArea, _ *gpui.App) NodeID {
		tree, _ := a.Layout(DockPlacementCenter)
		k := tree.Root().Kind()
		if !k.IsSplit {
			panic("the center root is a split")
		}
		return k.Children[ix].ID()
	})
}

// moveAlphaIntoTheOtherGroup is U:dock/dock_area.rs:2095-2117.
func moveAlphaIntoTheOtherGroup(cx *gpui.VisualTestContext, area gpui.Entity[DockArea], alpha gpui.Entity[testPanel]) {
	movePanelInto(cx, area, testPanelID(alpha), childNode(cx, area, 1), -1, true)
}

// collectSizes is U:dock/dock_area.rs:2119-2126.
func collectSizes(state *PanelState, out *[]float32) {
	if sizes, ok := state.Info.Sizes(); ok {
		*out = append(*out, sizes...)
	}
	for i := range state.Children {
		collectSizes(&state.Children[i], out)
	}
}

// registerTestPanels is U:dock/dock_area.rs:2128-2134.
func registerTestPanels(app *gpui.App) {
	for _, name := range []string{"Alpha", "Beta", "Gamma"} {
		RegisterPanel(app, name, func(_ PanelBuildContext, _ *gpui.Window, app *gpui.App) PanelView {
			return testPanelView(newTestPanel(name, app))
		})
	}
}

// oneGroup installs one tab group holding names as the whole center
// (U:dock/dock_area.rs:2136-2169). activeIx < 0 is None.
func oneGroup(t *testing.T, log dockLog, activeIx int, names ...string) (gpui.Entity[DockArea], []gpui.Entity[testPanel], *gpui.VisualTestContext) {
	t.Helper()
	area, cx := setupDockArea(t)
	var panels []gpui.Entity[testPanel]
	updateArea(cx, area, func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) {
		layout := DockLayoutTabs()
		for _, name := range names {
			panel := loggingTestPanel(name, log, acx.App())
			panels = append(panels, panel)
			layout.Panel(panel)
		}
		layout.ActiveIndex(max(activeIx, 0))
		a.SetCenter(layout, w, acx)
	})
	return area, panels, cx
}

// groupOf is the live group behind the center split's ix-th child
// (U:dock/dock_area.rs:2171-2179).
func groupOf(cx *gpui.VisualTestContext, area gpui.Entity[DockArea], ix int) gpui.Entity[TabGroup] {
	node := childNode(cx, area, ix)
	return groupAt(cx, area, node)
}

func groupAt(cx *gpui.VisualTestContext, area gpui.Entity[DockArea], node NodeID) gpui.Entity[TabGroup] {
	return readArea(cx, area, func(a *DockArea, _ *gpui.App) gpui.Entity[TabGroup] { return a.groups[node].entity })
}

// movePanelInto is U:dock/dock_area.rs:2181-2195 (ix < 0 is None).
func movePanelInto(cx *gpui.VisualTestContext, area gpui.Entity[DockArea], panel PanelID, node NodeID, ix int, activate bool) {
	updateArea(cx, area, func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) {
		a.MovePanel(panel, InsertTargetTabs{Node: node, Ix: max(ix, 0), HasIx: ix >= 0, Activate: activate}, w, acx)
	})
	cx.RunUntilParked()
}

// isCenterEmpty is U:dock/dock_area.rs:2197-2199.
func isCenterEmpty(cx *gpui.VisualTestContext, area gpui.Entity[DockArea]) bool {
	return readArea(cx, area, func(a *DockArea, app *gpui.App) bool { return a.IsEmpty(DockPlacementCenter, app) })
}

func panelNodeOf(cx *gpui.VisualTestContext, area gpui.Entity[DockArea], placement DockPlacement, panel PanelID) (NodeID, bool) {
	type found struct {
		node NodeID
		ok   bool
	}
	f := readArea(cx, area, func(a *DockArea, _ *gpui.App) found {
		tree, ok := a.Layout(placement)
		if !ok {
			return found{}
		}
		node, ok := tree.FindPanelNode(panel)
		return found{node, ok}
	})
	return f.node, f.ok
}

func movePanel(cx *gpui.VisualTestContext, area gpui.Entity[DockArea], panel PanelID, target InsertTarget) {
	updateArea(cx, area, func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) { a.MovePanel(panel, target, w, acx) })
	cx.RunUntilParked()
}

func requireHalves(t *testing.T, sizes []float32, label string) {
	t.Helper()
	if len(sizes) != 2 {
		t.Fatalf("%s: the drop splits in two, got %v", label, sizes)
	}
	a, b := sizes[0], sizes[1]
	if float32(math.Abs(float64(a-b))) > (a+b)*0.02 {
		t.Fatalf("%s: the two halves must be within 2%% of each other, got %v and %v", label, a, b)
	}
}

// U:dock/dock_area.rs:2201-2223
func TestDockAreaALayoutInstallsAndDumpsBackToTheSameState(t *testing.T) {
	area, cx := setupDockArea(t)
	updateArea(cx, area, func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) {
		a.SetCenter(DockLayoutHSplit().
			Child(DockLayoutTabs().Panel(newTestPanel("Alpha", acx.App())), dpx(300)).
			Child(DockLayoutTabs().Panel(newTestPanel("Beta", acx.App())), nil), w, acx)
	})
	state := dumpArea(cx, area)
	if state.Center.PanelName != "StackPanel" || len(state.Center.Children) != 2 ||
		state.Center.Children[0].Children[0].PanelName != "Alpha" || state.Center.Children[1].Children[0].PanelName != "Beta" {
		t.Fatalf("center = %+v", state.Center)
	}
}

// U:dock/dock_area.rs:2225-2285
func TestDockAreaMovingAPanelReusesItsEntity(t *testing.T) {
	log := newDockLog()
	area, alpha, cx := twoGroups(t, log)
	cx.RunUntilParked()
	drainLog(log)

	destination := childNode(cx, area, 1)
	before := groupAt(cx, area, destination).EntityID()
	moveAlphaIntoTheOtherGroup(cx, area, alpha)
	if after := groupAt(cx, area, destination).EntityID(); after != before {
		t.Fatal("the group the panel arrived in was rebuilt, not reused")
	}
	if _, ok := panelNodeOf(cx, area, DockPlacementCenter, testPanelID(alpha)); !ok {
		t.Fatal("the moved panel is not in the tree")
	}
	state := dumpArea(cx, area)
	if len(state.Center.Children) != 1 {
		t.Fatalf("center children = %d: the emptied group collapsed out of the split", len(state.Center.Children))
	}
	if len(state.Center.Children[0].Children) != 2 {
		t.Fatal("both panels now share the surviving group")
	}
}

// U:dock/dock_area.rs:2287-2300
func TestDockAreaAMovedPanelIsNotToldItWasRemoved(t *testing.T) {
	log := newDockLog()
	area, alpha, cx := twoGroups(t, log)
	cx.RunUntilParked()
	drainLog(log)
	moveAlphaIntoTheOtherGroup(cx, area, alpha)
	if logContains(drainLog(log), "Alpha", sigRemoved) {
		t.Fatal("moving a panel between groups must never deliver on_removed")
	}
}

// U:dock/dock_area.rs:2302-2321
func TestDockAreaRemovingAPanelDoesTellItItWasRemoved(t *testing.T) {
	log := newDockLog()
	area, alpha, cx := twoGroups(t, log)
	cx.RunUntilParked()
	drainLog(log)
	updateArea(cx, area, func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) { a.RemovePanel(alpha, w, acx) })
	cx.RunUntilParked()
	if !logContains(drainLog(log), "Alpha", sigRemoved) {
		t.Fatal("a genuine removal must deliver on_removed")
	}
}

// U:dock/dock_area.rs:2323-2345
func TestDockAreaReconcilingAnUnchangedTreeCreatesNoEntities(t *testing.T) {
	log := newDockLog()
	area, _, cx := twoGroups(t, log)
	cx.RunUntilParked()
	drainLog(log)

	ids := func() [][2]uint64 {
		return readArea(cx, area, func(a *DockArea, _ *gpui.App) [][2]uint64 { return a.containerEntityIDs() })
	}
	before := ids()
	updateArea(cx, area, func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) { a.reconcile(w, acx) })
	after := ids()
	if len(before) == 0 {
		t.Fatal("there were no containers to preserve")
	}
	if !slices.Equal(before, after) {
		t.Fatalf("a steady-state pass created or dropped containers: %v -> %v", before, after)
	}
	cx.RunUntilParked()
	if got := drainLog(log); len(got) != 0 {
		t.Fatalf("a panel was re-added or re-activated: %v", got)
	}
}

// U:dock/dock_area.rs:2347-2374
func TestDockAreaALoadedLayoutRoundTripsThroughDump(t *testing.T) {
	area, cx := setupDockArea(t)
	cx.Update(func(_ *gpui.Window, app *gpui.App) { registerTestPanels(app) })
	state := decodeDockAreaState(t, dockFixture(t, "nested_splits.json"))

	load := func(s DockAreaState) {
		updateArea(cx, area, func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) {
			if err := a.Load(s, w, acx); err != nil {
				t.Fatal(err)
			}
		})
	}
	load(state)
	dumped := dumpArea(cx, area)
	load(dumped)
	again := dumpArea(cx, area)
	if !dumped.Equal(&again) {
		t.Fatal("load/dump must reach a fixpoint")
	}
	if len(dumped.Center.Children) != 3 || dumped.Center.Children[0].Children[0].PanelName != "Alpha" {
		t.Fatalf("center = %+v", dumped.Center)
	}
}

// U:dock/dock_area.rs:2376-2408
func TestDockAreaADumpedLiveLayoutHasNoZeroSizes(t *testing.T) {
	area, cx := setupDockArea(t)
	updateArea(cx, area, func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) {
		app := acx.App()
		a.SetCenter(DockLayoutVSplit().
			// Unconstrained, which the writer would otherwise emit as 0.0.
			Child(DockLayoutTabs().Panel(newTestPanel("Alpha", app)), nil).
			// Already zero.
			Child(DockLayoutTabs().Panel(newTestPanel("Beta", app)), dpx(0)).
			Child(DockLayoutTabs().Panel(newTestPanel("Gamma", app)), dpx(240)), w, acx)
	})
	state := dumpArea(cx, area)
	var sizes []float32
	collectSizes(&state.Center, &sizes)
	if len(sizes) == 0 {
		t.Fatal("the layout has no slots to check")
	}
	for _, size := range sizes {
		if size <= 0 {
			t.Fatalf("an older build reads a persisted 0.0 back as a real zero-pixel panel: %v", sizes)
		}
	}
}

// installTwoFixedSlots is the 300/300 center of U:dock/dock_area.rs:2416-2431.
func installTwoFixedSlots(cx *gpui.VisualTestContext, area gpui.Entity[DockArea]) {
	updateArea(cx, area, func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) {
		a.SetCenter(DockLayoutHSplit().
			Child(DockLayoutTabs().Panel(newTestPanel("Alpha", acx.App())), dpx(300)).
			Child(DockLayoutTabs().Panel(newTestPanel("Beta", acx.App())), dpx(300)), w, acx)
	})
	cx.RunUntilParked()
}

// U:dock/dock_area.rs:2410-2458
func TestDockAreaADumpedSplitWritesTheSizesItIsActuallyDrawnAt(t *testing.T) {
	area, cx := setupDockArea(t)
	installTwoFixedSlots(cx, area)
	root := centerRoot(cx, area)
	measured := splitSizesOf(cx, area, root)
	if slices.Equal(measured, []float32{300, 300}) {
		t.Fatal("the split has to have been rescaled by a layout pass")
	}
	state := dumpArea(cx, area)
	requireStackSizes(t, state.Center.Info, measured, "the written sizes are the ones on screen")
}

// U:dock/dock_area.rs:2460-2515
func TestDockAreaSetSplitSizesRestoresAShareAndReportsIt(t *testing.T) {
	area, cx := setupDockArea(t)
	installTwoFixedSlots(cx, area)
	root := centerRoot(cx, area)
	events := countLayoutEvents(cx, area)

	updateArea(cx, area, func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) {
		a.SetSplitSizes(root, []float32{100, 300}, w, acx)
	})
	cx.RunUntilParked()
	if *events != 1 {
		t.Fatalf("events = %d", *events)
	}
	measured := splitSizesOf(cx, area, root)
	if share := measured[0] / (measured[0] + measured[1]); math.Abs(float64(share-0.25)) >= 0.01 {
		t.Fatalf("measured %v", measured)
	}

	updateArea(cx, area, func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) {
		a.SetSplitSizes(root, []float32{10}, w, acx)
	})
	cx.RunUntilParked()
	if *events != 1 {
		t.Fatalf("events = %d after a mismatched vector", *events)
	}
	if after := splitSizesOf(cx, area, root); !slices.Equal(after, measured) {
		t.Fatalf("sizes moved: %v -> %v", measured, after)
	}
}

// U:dock/dock_area.rs:2517-2560
func TestDockAreaAPanelDroppedBesideAnotherTakesHalfTheSplit(t *testing.T) {
	area, panels, cx := oneGroup(t, newDockLog(), -1, "Alpha", "Beta")
	group := childNode(cx, area, 0)
	movePanel(cx, area, testPanelID(panels[1]), InsertTargetSplit{Node: group, Placement: PlacementRight})
	requireHalves(t, splitSizesOf(cx, area, centerRoot(cx, area)), "right of the group")
}

// U:dock/dock_area.rs:2562-2578
func TestDockAreaAMoveOfAnUnownedPanelIsIgnored(t *testing.T) {
	area, _, cx := oneGroup(t, newDockLog(), -1, "Alpha", "Beta")
	group := childNode(cx, area, 0)
	before := dumpArea(cx, area)
	// A PanelID from nowhere, as if dropped from another DockArea.
	movePanelInto(cx, area, PanelIDFromU64(9_999_999), group, -1, true)
	after := dumpArea(cx, area)
	if !before.Equal(&after) {
		t.Fatal("an unowned panel move must not touch the tree")
	}
}

// U:dock/dock_area.rs:2580-2623
func TestDockAreaAPanelDroppedAcrossTheAxisStillTakesHalf(t *testing.T) {
	area, panels, cx := oneGroup(t, newDockLog(), -1, "Alpha", "Beta")
	group := childNode(cx, area, 0)
	movePanel(cx, area, testPanelID(panels[1]), InsertTargetSplit{Node: group, Placement: PlacementBottom})
	// The wrapper becomes the center root's only child.
	wrapper := childNode(cx, area, 0)
	requireHalves(t, splitSizesOf(cx, area, wrapper), "below the group")
}

// U:dock/dock_area.rs:2625-2683
func TestDockAreaAPanelDroppedIntoAPopulatedSplitTakesAnEvenShare(t *testing.T) {
	area, cx := setupDockArea(t)
	var gamma gpui.Entity[testPanel]
	updateArea(cx, area, func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) {
		app := acx.App()
		alpha, beta := newTestPanel("Alpha", app), newTestPanel("Beta", app)
		gamma = newTestPanel("Gamma", app)
		a.SetCenter(DockLayoutHSplit().
			Child(DockLayoutTabs().Panel(alpha), dpx(240)).
			Child(DockLayoutTabs().Panel(beta).Panel(gamma), nil), w, acx)
	})
	cx.RunUntilParked()
	right := childNode(cx, area, 1)
	movePanel(cx, area, testPanelID(gamma), InsertTargetSplit{Node: right, Placement: PlacementRight})
	sizes := splitSizesOf(cx, area, centerRoot(cx, area))
	if len(sizes) != 3 {
		t.Fatalf("three slots side by side, got %v", sizes)
	}
	requireHalves(t, sizes[1:], "the dropped panel splits its neighbour evenly")
}

// U:dock/dock_area.rs:2685-2754
func TestDockAreaAPanelDroppedBesideADockTakesHalf(t *testing.T) {
	for _, placement := range []DockPlacement{DockPlacementBottom, DockPlacementLeft} {
		area, cx := setupDockArea(t)
		var dropped gpui.Entity[testPanel]
		updateArea(cx, area, func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) {
			app := acx.App()
			resident := newTestPanel("Resident", app)
			dropped = newTestPanel("Dropped", app)
			a.SetCenter(DockLayoutTabs().Panel(newTestPanel("Center", app)), w, acx)
			a.SetDock(placement, DockLayoutTabs().Panel(resident).Panel(dropped), w, acx)
			a.SetDockSize(placement, 400, w, acx)
		})
		cx.RunUntilParked()
		group, ok := panelNodeOf(cx, area, placement, testPanelID(dropped))
		if !ok {
			t.Fatalf("%v: both panels start in the dock's only group", placement)
		}
		movePanel(cx, area, testPanelID(dropped), InsertTargetSplit{Node: group, Placement: PlacementBottom})
		split := readArea(cx, area, func(a *DockArea, _ *gpui.App) NodeID {
			tree, _ := a.Layout(placement)
			if !tree.Root().Kind().IsSplit {
				panic("the drop must have produced a split")
			}
			return tree.Root().ID()
		})
		requireHalves(t, splitSizesOf(cx, area, split), placement.String())
	}
}

// U:dock/dock_area.rs:2756-2802
func TestDockAreaAnExplicitSlotSizeSurvivesTheFirstLayoutPass(t *testing.T) {
	area, cx := setupDockArea(t)
	updateArea(cx, area, func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) {
		a.SetCenter(DockLayoutHSplit().
			Child(DockLayoutTabs().Panel(newTestPanel("Sidebar", acx.App())), dpx(200)).
			Child(DockLayoutTabs().Panel(newTestPanel("Content", acx.App())), nil), w, acx)
	})
	cx.RunUntilParked()
	sizes := splitSizesOf(cx, area, centerRoot(cx, area))
	if len(sizes) == 0 {
		t.Fatal("the split has no slots")
	}
	if fixed := sizes[0]; math.Abs(float64(fixed-200)) > 4 {
		t.Fatalf("the fixed slot keeps its 200px, got %v", fixed)
	}
}

// measuredPanel draws a measurable box (U:dock/dock_area.rs:2804-2839).
type measuredPanel struct {
	name        string
	focusHandle gpui.FocusHandle
}

func newMeasuredPanel(name string, app *gpui.App) gpui.Entity[measuredPanel] {
	return gpui.NewEntity(app, app.RootScope(), func(p *measuredPanel, cx *gpui.Context[measuredPanel]) {
		*p = measuredPanel{name: name, focusHandle: cx.App().FocusHandle()}
	})
}

func (p *measuredPanel) PanelName() string                      { return p.name }
func (p *measuredPanel) FocusHandle(*gpui.App) gpui.FocusHandle { return p.focusHandle }
func (p *measuredPanel) Render(*gpui.Window, *gpui.Context[measuredPanel]) gpui.AnyElement {
	return gpui.Div().Block().SizeFull().DebugSelector(p.name).IntoElement()
}

// drawFrames is U:dock/dock_area.rs:2841-2845.
func drawFrames(t *testing.T, cx *gpui.VisualTestContext, frames int) {
	t.Helper()
	for range frames {
		drawFrame(t, cx)
	}
}

// U:dock/dock_area.rs:2847-2918
func TestDockAreaSwitchingATabLeavesAnUntouchedSplitWhereItWasDrawn(t *testing.T) {
	area, cx := setupDockArea(t)
	updateArea(cx, area, func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) {
		app := acx.App()
		a.SetCenter(DockLayoutTabs().Panel(newTestPanel("Center", app)), w, acx)
		a.SetDock(DockPlacementLeft, DockLayoutVSplit().
			Child(DockLayoutTabs().Panel(newMeasuredPanel("upper-left", app)), nil).
			Child(DockLayoutTabs().Panel(newMeasuredPanel("lower-left", app)), dpx(360)), w, acx)
		a.SetDockSize(DockPlacementLeft, 350, w, acx)
		a.SetDock(DockPlacementBottom, DockLayoutTabs().
			Panel(newTestPanel("Tooltip", app)).
			Panel(newTestPanel("Icon", app)), w, acx)
		a.SetDockSize(DockPlacementBottom, 200, w, acx)
	})
	cx.RunUntilParked()
	drawFrames(t, cx, 3)
	before := [2]gpui.Bounds{debugBounds(t, cx, "upper-left"), debugBounds(t, cx, "lower-left")}

	bottom := readArea(cx, area, func(a *DockArea, _ *gpui.App) NodeID {
		tree, _ := a.Layout(DockPlacementBottom)
		return tree.Root().ID()
	})
	group := groupAt(cx, area, bottom)
	updateGroup(cx, group, func(g *TabGroup, w *gpui.Window, gcx *gpui.Context[TabGroup]) { g.SelectTab(1, w, gcx) })
	cx.RunUntilParked()
	drawFrames(t, cx, 3)
	after := [2]gpui.Bounds{debugBounds(t, cx, "upper-left"), debugBounds(t, cx, "lower-left")}
	if before != after {
		t.Fatalf("a tab change in the bottom dock moved the left split: %v -> %v", before, after)
	}
}

// U:dock/dock_area.rs:2920-2970
func TestDockAreaSwitchingATabKeepsARestoredSplitAtItsRescaledShare(t *testing.T) {
	area, cx := setupDockArea(t)
	updateArea(cx, area, func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) {
		app := acx.App()
		a.SetCenter(DockLayoutHSplit().
			Child(DockLayoutTabs().Panel(newTestPanel("Alpha", app)).Panel(newTestPanel("Beta", app)), dpx(620)).
			Child(DockLayoutTabs().Panel(newMeasuredPanel("second", app)), dpx(350)), w, acx)
	})
	cx.RunUntilParked()
	drawFrames(t, cx, 3)
	before := debugBounds(t, cx, "second")
	if before.Right() == 970 {
		t.Fatal("the window must not match the recorded total")
	}
	group := groupOf(cx, area, 0)
	updateGroup(cx, group, func(g *TabGroup, w *gpui.Window, gcx *gpui.Context[TabGroup]) { g.SelectTab(1, w, gcx) })
	cx.RunUntilParked()
	drawFrames(t, cx, 3)
	if after := debugBounds(t, cx, "second"); after != before {
		t.Fatalf("a tab change handed the file's pixels back to the split: %v -> %v", before, after)
	}
}

func loadArea(t *testing.T, cx *gpui.VisualTestContext, area gpui.Entity[DockArea], state DockAreaState) {
	t.Helper()
	updateArea(cx, area, func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) {
		if err := a.Load(state, w, acx); err != nil {
			t.Fatal(err)
		}
	})
}

// U:dock/dock_area.rs:2972-3014
func TestDockAreaTheShippedFixtureSurvivesALoadDumpLoadRoundTrip(t *testing.T) {
	area, cx := setupDockArea(t)
	loadArea(t, cx, area, decodeDockAreaState(t, dockFixture(t, "layout.json")))
	cx.RunUntilParked()
	first := dumpArea(cx, area)

	if len(first.Center.Children) != 2 || len(first.Center.Children[0].Children) != 15 || len(first.Center.Children[1].Children) != 1 {
		t.Fatalf("center = %+v", first.Center)
	}
	for _, dock := range []*DockState{first.LeftDock, first.BottomDock, first.RightDock} {
		if dock == nil {
			t.Fatal("all three docks are attached")
		}
		if !dock.Open() || len(dock.Panel().Children) == 0 {
			t.Fatalf("dock %v loaded closed or empty", dock.Placement())
		}
	}
	if first.LeftDock.Size() != 350 || first.BottomDock.Size() != 200 || first.RightDock.Size() != 320 {
		t.Fatalf("dock sizes = %v %v %v", first.LeftDock.Size(), first.BottomDock.Size(), first.RightDock.Size())
	}

	loadArea(t, cx, area, first)
	cx.RunUntilParked()
	second := dumpArea(cx, area)
	if !second.Equal(&first) {
		a, _ := json.Marshal(first)
		b, _ := json.Marshal(second)
		t.Fatalf("dump != dump(load(dump)):\n%s\n%s", a, b)
	}
}

// U:dock/dock_area.rs:3016-3034
func TestDockAreaAnUnregisteredPanelSurvivesALoadAndSaveRoundTrip(t *testing.T) {
	area, cx := setupDockArea(t)
	cx.Update(func(_ *gpui.Window, app *gpui.App) { registerTestPanels(app) })
	loadArea(t, cx, area, decodeDockAreaState(t, dockFixture(t, "unregistered_panel.json")))
	dumped := dumpArea(cx, area)
	leaf := dumped.Center.Children[0].Children[0]
	if leaf.PanelName != "PanelFromTheFuture" || !leaf.Info.Equal(PanelInfoPanel(json.RawMessage(`{"keep": "me"}`))) {
		t.Fatalf("leaf = %+v: a panel this build cannot construct keeps its payload", leaf)
	}
}

// U:dock/dock_area.rs:3036-3086
func TestDockAreaADockCarriesItsOwnTreeAndSurvivesARoundTrip(t *testing.T) {
	area, cx := setupDockArea(t)
	updateArea(cx, area, func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) {
		a.SetCenter(DockLayoutTabs().Panel(newTestPanel("Alpha", acx.App())), w, acx)
		a.SetDock(DockPlacementLeft, DockLayoutTabs().Panel(newTestPanel("Beta", acx.App())), w, acx)
	})
	ids := func(placement DockPlacement) []NodeID {
		return readArea(cx, area, func(a *DockArea, _ *gpui.App) []NodeID {
			tree, _ := a.Layout(placement)
			return tree.NodeIDs()
		})
	}
	centerIDs, dockIDs := ids(DockPlacementCenter), ids(DockPlacementLeft)
	if len(centerIDs) == 0 || len(dockIDs) == 0 {
		t.Fatal("a region has no nodes")
	}
	for _, id := range centerIDs {
		if slices.Contains(dockIDs, id) {
			t.Fatalf("every tree in one area must draw from one id space: center %v vs left %v", centerIDs, dockIDs)
		}
	}
	state := dumpArea(cx, area)
	left := state.LeftDock
	if left == nil || left.Placement() != DockPlacementLeft || !left.Open() || left.Panel().Children[0].PanelName != "Beta" {
		t.Fatalf("left dock = %+v", left)
	}
	if state.RightDock != nil {
		t.Fatal("a right dock was written")
	}
}

// U:dock/dock_area.rs:3088-3103
func TestDockAreaAPanelMovedBetweenRegionsKeepsItsActiveState(t *testing.T) {
	log := newDockLog()
	area, alpha, cx := twoGroups(t, log)
	cx.RunUntilParked()
	if !logContains(drainLog(log), "Alpha", sigActive(true)) {
		t.Fatal("Alpha was never told true")
	}
	moveAlphaIntoTheOtherGroup(cx, area, alpha)
	if logContains(drainLog(log), "Alpha", sigActive(true)) {
		t.Fatal("a displayed panel dragged to another group must not be told true twice")
	}
}

// U:dock/dock_area.rs:3105-3133
func TestDockAreaAGroupsCloseIntentReachesTheTree(t *testing.T) {
	log := newDockLog()
	area, alpha, cx := twoGroups(t, log)
	cx.RunUntilParked()
	drainLog(log)
	alphaID := testPanelID(alpha)
	group := groupOf(cx, area, 0)
	updateGroup(cx, group, func(g *TabGroup, _ *gpui.Window, gcx *gpui.Context[TabGroup]) { g.ClosePanel(alphaID, gcx) })
	cx.RunUntilParked()
	if _, ok := panelNodeOf(cx, area, DockPlacementCenter, alphaID); ok {
		t.Fatal("the close intent was not applied to the tree")
	}
	if !logContains(drainLog(log), "Alpha", sigRemoved) {
		t.Fatal("the closed panel was not told it was removed")
	}
}

// U:dock/dock_area.rs:3135-3153
func TestDockAreaReplacingTheCenterTellsThePanelsThatLeft(t *testing.T) {
	log := newDockLog()
	area, _, cx := twoGroups(t, log)
	cx.RunUntilParked()
	drainLog(log)
	updateArea(cx, area, func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) {
		a.SetCenter(DockLayoutTabs().Panel(newTestPanel("Gamma", acx.App())), w, acx)
	})
	cx.RunUntilParked()
	seen := drainLog(log)
	if !logContains(seen, "Alpha", sigRemoved) || !logContains(seen, "Beta", sigRemoved) {
		t.Fatalf("seen = %v", seen)
	}
}

// U:dock/dock_area.rs:3155-3173
func TestDockAreaClosingAZoomedPanelClearsTheZoom(t *testing.T) {
	log := newDockLog()
	area, alpha, cx := twoGroups(t, log)
	cx.RunUntilParked()
	node := childNode(cx, area, 0)
	updateArea(cx, area, func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) { a.SetZoomedIn(node, w, acx) })
	if !readArea(cx, area, func(a *DockArea, _ *gpui.App) bool { return a.IsZoomed() }) {
		t.Fatal("the area did not zoom")
	}
	updateArea(cx, area, func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) { a.RemovePanel(alpha, w, acx) })
	if readArea(cx, area, func(a *DockArea, _ *gpui.App) bool { return a.IsZoomed() }) {
		t.Fatal("a zoomed panel that left the dock must not keep filling it")
	}
}

// skinPlaceholder is the skin's stand-in for a panel this build cannot
// construct (U:dock/dock_area.rs:3175-3205).
type skinPlaceholder struct {
	state       PanelState
	focusHandle gpui.FocusHandle
}

func (*skinPlaceholder) PanelName() string                        { return "SkinPlaceholder" }
func (p *skinPlaceholder) Dump(*gpui.App) PanelState              { return p.state.Clone() }
func (p *skinPlaceholder) FocusHandle(*gpui.App) gpui.FocusHandle { return p.focusHandle }
func (*skinPlaceholder) Render(*gpui.Window, *gpui.Context[skinPlaceholder]) gpui.AnyElement {
	return gpui.Empty()
}

// placeholderSkin is U:dock/dock_area.rs:3207-3229.
type placeholderSkin struct {
	asked *[]string
}

func (s placeholderSkin) BuildPlaceholder(state *PanelState, _ *gpui.Window, app *gpui.App) (PanelView, bool) {
	*s.asked = append(*s.asked, state.PanelName)
	saved := state.Clone()
	return PanelViewOf(gpui.NewEntity(app, app.RootScope(), func(p *skinPlaceholder, cx *gpui.Context[skinPlaceholder]) {
		*p = skinPlaceholder{state: saved, focusHandle: cx.App().FocusHandle()}
	})), true
}

func (placeholderSkin) TabGroupRenderer() TabGroupRenderer { return bareTabGroup{} }

// U:dock/dock_area.rs:3231-3274
func TestDockAreaAnUnbuildablePanelBecomesTheSkinsPlaceholder(t *testing.T) {
	asked := new([]string)
	ta := gpui.NewTestApp()
	shutdownAtCleanup(t, ta)
	area, cx := openDockArea(ta, placeholderSkin{asked: asked})
	// Nothing is registered, so the round trip cannot rebuild this panel.
	updateArea(cx, area, func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) {
		a.SetCenter(DockLayoutTabs().Panel(newTestPanel("Ghost", acx.App())), w, acx)
	})
	loadArea(t, cx, area, dumpArea(cx, area))
	cx.RunUntilParked()

	if !slices.Equal(*asked, []string{"Ghost"}) {
		t.Fatalf("asked = %v", *asked)
	}
	names := readArea(cx, area, func(a *DockArea, app *gpui.App) []string {
		var names []string
		for _, panel := range a.panels {
			names = append(names, panel.PanelName(app))
		}
		return names
	})
	if !slices.Equal(names, []string{"SkinPlaceholder"}) {
		t.Fatalf("panels = %v: base installed its own placeholder, not the skin's", names)
	}
	if name := dumpArea(cx, area).Center.Children[0].Children[0].PanelName; name != "Ghost" {
		t.Fatalf("the unknown panel did not survive the next save: %q", name)
	}
}

// U:dock/dock_area.rs:3276-3331
func TestDockAreaRemovingANonTailChildShiftsTheSplitSizesWithIt(t *testing.T) {
	log := newDockLog()
	area, cx := setupDockArea(t)
	var alpha gpui.Entity[testPanel]
	updateArea(cx, area, func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) {
		app := acx.App()
		alpha = loggingTestPanel("Alpha", log, app)
		a.SetCenter(DockLayoutHSplit().
			Child(DockLayoutTabs().Panel(alpha), dpx(100)).
			Child(DockLayoutTabs().Panel(loggingTestPanel("Beta", log, app)), dpx(200)).
			Child(DockLayoutTabs().Panel(loggingTestPanel("Gamma", log, app)), dpx(300)), w, acx)
	})
	root := centerRoot(cx, area)
	split := readArea(cx, area, func(a *DockArea, _ *gpui.App) gpui.Entity[ResizableState] { return a.splits[root].entity })
	sizes := func() []float32 {
		return split.Read(cx, func(s *ResizableState, _ *gpui.App) []float32 { return slices.Clone(s.Sizes()) })
	}
	before := sizes()
	if len(before) != 3 {
		t.Fatalf("before = %v", before)
	}
	keptIfCorrect := before[1] / before[2]
	keptIfTruncated := before[0] / before[1]
	if math.Abs(float64(keptIfCorrect-keptIfTruncated)) <= 0.1 {
		t.Fatal("the fixture must be able to tell the two outcomes apart")
	}
	updateArea(cx, area, func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) { a.RemovePanel(alpha, w, acx) })
	after := sizes()
	if len(after) != 2 || math.Abs(float64(after[0]/after[1]-keptIfCorrect)) >= 0.01 {
		t.Fatalf("the survivors did not keep their own proportions: got %v from %v", after, before)
	}
}

// U:dock/dock_area.rs:3333-3404
func TestDockAreaAPanelMovesBetweenTheCenterAndADock(t *testing.T) {
	log := newDockLog()
	area, alpha, cx := twoGroups(t, log)
	updateArea(cx, area, func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) {
		a.SetDock(DockPlacementLeft, DockLayoutTabs().Panel(loggingTestPanel("Gamma", log, acx.App())), w, acx)
	})
	cx.RunUntilParked()
	drainLog(log)
	alphaID := testPanelID(alpha)
	dockGroup := readArea(cx, area, func(a *DockArea, _ *gpui.App) NodeID {
		tree, _ := a.Layout(DockPlacementLeft)
		return tree.Root().ID()
	})
	movePanelInto(cx, area, alphaID, dockGroup, -1, true)
	if _, ok := panelNodeOf(cx, area, DockPlacementCenter, alphaID); ok {
		t.Fatal("the panel did not leave the center")
	}
	if node, ok := panelNodeOf(cx, area, DockPlacementLeft, alphaID); !ok || node != dockGroup {
		t.Fatal("the panel did not arrive in the dock's group")
	}
	seen := drainLog(log)
	if logContains(seen, "Alpha", sigRemoved) {
		t.Fatal("crossing regions is still a move, not a removal")
	}
	if logContains(seen, "Alpha", sigActive(true)) {
		t.Fatal("it was displayed in both, so it is not told true twice")
	}
}

// U:dock/dock_area.rs:3406-3465
func TestDockAreaAMoveOntoAnUnusableTargetLeavesNoStrandedPanel(t *testing.T) {
	log := newDockLog()
	area, _, cx := twoGroups(t, log)
	var gamma gpui.Entity[testPanel]
	updateArea(cx, area, func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) {
		gamma = loggingTestPanel("Gamma", log, acx.App())
		a.SetDock(DockPlacementLeft, DockLayoutTabs().Panel(gamma), w, acx)
	})
	cx.RunUntilParked()
	drainLog(log)
	gammaID := testPanelID(gamma)
	// The center root is a split, so a Tabs insert naming it does nothing.
	movePanelInto(cx, area, gammaID, centerRoot(cx, area), -1, true)
	if readArea(cx, area, func(a *DockArea, _ *gpui.App) bool { _, ok := a.Panel(gammaID); return ok }) {
		t.Fatal("the view map carries a panel that belongs to no tree")
	}
	if !logContains(drainLog(log), "Gamma", sigRemoved) {
		t.Fatal("the panel was not told it was removed")
	}
}

// U:dock/dock_area.rs:3467-3505
func TestDockAreaAnAllHiddenContainerReportsItselfInvisible(t *testing.T) {
	area, cx := setupDockArea(t)
	var beta gpui.Entity[testPanel]
	updateArea(cx, area, func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) {
		beta = newTestPanel("Beta", acx.App())
		a.SetCenter(DockLayoutHSplit().
			Child(DockLayoutTabs().Panel(newTestPanel("Alpha", acx.App())), nil).
			Child(DockLayoutTabs().Panel(beta), nil), w, acx)
	})
	visible := func(ix int) bool {
		node := childNode(cx, area, ix)
		return readArea(cx, area, func(a *DockArea, app *gpui.App) bool {
			tree, _ := a.Layout(DockPlacementCenter)
			found, _ := tree.FindNode(node)
			return a.isNodeVisible(found, app)
		})
	}
	if !visible(0) || !visible(1) {
		t.Fatal("both groups start visible")
	}
	cx.Update(func(_ *gpui.Window, app *gpui.App) {
		beta.Update(app, func(p *testPanel, pcx *gpui.Context[testPanel]) { p.setVisible(false, pcx) })
	})
	if !visible(0) {
		t.Fatal("the visible group still holds its slot")
	}
	if visible(1) {
		t.Fatal("a group whose every panel is hidden must give its slot up")
	}
}

// U:dock/dock_area.rs:3507-3526
func TestDockAreaALockedAreaSealsItsGroups(t *testing.T) {
	log := newDockLog()
	area, _, cx := twoGroups(t, log)
	cx.RunUntilParked()
	group := groupOf(cx, area, 0)
	closable := func() bool {
		return readGroup(cx, group, func(g *TabGroup, app *gpui.App) bool { return g.IsClosable(app) })
	}
	if !closable() {
		t.Fatal("an unlocked group's panel can be closed")
	}
	updateArea(cx, area, func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) { a.SetLocked(true, w, acx) })
	if closable() {
		t.Fatal("the lock did not reach the group through the constraints push")
	}
}

// U:dock/dock_area.rs:3535-3546
func TestDockAreaEmptyCenterRoundTripsAsAStack(t *testing.T) {
	area, cx := setupDockArea(t)
	center := dumpArea(cx, area).Center
	if center.PanelName != "StackPanel" || !center.Info.IsStack() {
		t.Fatalf("center = %+v", center)
	}
}

// U:dock/dock_area.rs:3548-3556
func TestDockAreaFreshCenterIsEmpty(t *testing.T) {
	area, cx := setupDockArea(t)
	if !isCenterEmpty(cx, area) {
		t.Fatal("NewDockArea starts with an empty split centre")
	}
}

// U:dock/dock_area.rs:3558-3565
func TestDockAreaCenterHoldingATabGroupIsNotEmpty(t *testing.T) {
	area, _, cx := oneGroup(t, newDockLog(), -1, "A", "B")
	cx.RunUntilParked()
	if isCenterEmpty(cx, area) {
		t.Fatal("the center is empty")
	}
}

// U:dock/dock_area.rs:3567-3584
func TestDockAreaCenterIsEmptyAgainOnceEveryPanelIsRemoved(t *testing.T) {
	area, panels, cx := oneGroup(t, newDockLog(), -1, "A", "B")
	cx.RunUntilParked()
	for _, panel := range panels {
		updateArea(cx, area, func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) { a.RemovePanel(panel, w, acx) })
	}
	cx.RunUntilParked()
	if !isCenterEmpty(cx, area) {
		t.Fatal("the center is not empty")
	}
}

// U:dock/dock_area.rs:3586-3600
func TestDockAreaCenterIsNotEmptyAfterAddingToATabGroup(t *testing.T) {
	area, cx := setupDockArea(t)
	if !isCenterEmpty(cx, area) {
		t.Fatal("the center starts non-empty")
	}
	updateArea(cx, area, func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) {
		a.AddPanel(newTestPanel("Alpha", acx.App()), DockPlacementCenter, nil, w, acx)
	})
	cx.RunUntilParked()
	if isCenterEmpty(cx, area) {
		t.Fatal("the center is empty")
	}
}

// U:dock/dock_area.rs:3602-3627
func TestDockAreaAddPanelViewRegistersTheHandleItWasGiven(t *testing.T) {
	area, cx := setupDockArea(t)
	var view PanelView
	updateArea(cx, area, func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) {
		view = testPanelView(newTestPanel("Alpha", acx.App()))
		a.AddPanelView(view, DockPlacementCenter, nil, w, acx)
	})
	cx.RunUntilParked()
	same := readArea(cx, area, func(a *DockArea, app *gpui.App) bool {
		stored, ok := a.Panel(view.PanelID(app))
		return ok && stored == view
	})
	if !same {
		t.Fatal("the stored handle is not the one that was handed over, under its own id")
	}
	if isCenterEmpty(cx, area) {
		t.Fatal("the center is empty")
	}
}

// U:dock/dock_area.rs:3629-3656
func TestDockAreaCenterHoldingOnlyHiddenPanelsIsEmpty(t *testing.T) {
	area, panels, cx := oneGroup(t, newDockLog(), -1, "A", "B")
	cx.RunUntilParked()
	if isCenterEmpty(cx, area) {
		t.Fatal("the center starts empty")
	}
	cx.Update(func(_ *gpui.Window, app *gpui.App) {
		for _, panel := range panels {
			panel.Update(app, func(p *testPanel, pcx *gpui.Context[testPanel]) { p.setVisible(false, pcx) })
		}
	})
	cx.RunUntilParked()
	count := readArea(cx, area, func(a *DockArea, _ *gpui.App) int {
		tree, _ := a.Layout(DockPlacementCenter)
		return len(tree.Panels())
	})
	if count != 2 {
		t.Fatalf("hiding a panel removed it from the tab group: %d panels", count)
	}
	if !isCenterEmpty(cx, area) {
		t.Fatal("a centre holding only hidden panels is not empty")
	}
}

// U:dock/dock_area.rs:3658-3665
func TestDockAreaSinglePanelGroupReceivesInitialActive(t *testing.T) {
	log := newDockLog()
	_, _, cx := oneGroup(t, log, -1, "A")
	cx.RunUntilParked()
	requireActive(t, drainActive(log), activeEntry{"A", true})
}

// U:dock/dock_area.rs:3667-3675
func TestDockAreaMultiTabConstructionNotifiesOnlyDisplayedPanel(t *testing.T) {
	log := newDockLog()
	_, _, cx := oneGroup(t, log, -1, "A", "B", "C")
	cx.RunUntilParked()
	requireActive(t, drainActive(log), activeEntry{"A", true})
}

// U:dock/dock_area.rs:3677-3684
func TestDockAreaActiveIndexRestoreNotifiesThatPanelOnly(t *testing.T) {
	log := newDockLog()
	_, _, cx := oneGroup(t, log, 2, "A", "B", "C")
	cx.RunUntilParked()
	requireActive(t, drainActive(log), activeEntry{"C", true})
}

// U:dock/dock_area.rs:3686-3698
func TestDockAreaSwitchingTabsSendsFalseThenTrue(t *testing.T) {
	log := newDockLog()
	area, _, cx := oneGroup(t, log, -1, "A", "B")
	cx.RunUntilParked()
	drainLog(log)
	group := groupOf(cx, area, 0)
	updateGroup(cx, group, func(g *TabGroup, w *gpui.Window, gcx *gpui.Context[TabGroup]) { g.SelectTab(1, w, gcx) })
	cx.RunUntilParked()
	requireActive(t, drainActive(log), activeEntry{"A", false}, activeEntry{"B", true})
}

// U:dock/dock_area.rs:3700-3726
func TestDockAreaSelectPanelDisplaysThatTabWhereItSits(t *testing.T) {
	log := newDockLog()
	area, panels, cx := oneGroup(t, log, -1, "A", "B", "C")
	cx.RunUntilParked()
	drainLog(log)
	b := testPanelID(panels[1])
	updateArea(cx, area, func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) { a.SelectPanel(b, w, acx) })
	cx.RunUntilParked()
	requireActive(t, drainActive(log), activeEntry{"A", false}, activeEntry{"B", true})
	order, activeIx := readArea(cx, area, func(a *DockArea, _ *gpui.App) PaneRef {
		tree, _ := a.Layout(DockPlacementCenter)
		node, _ := tree.FindPanelNode(b)
		found, _ := tree.FindNode(node)
		return found.Kind()
	}).Panels, readArea(cx, area, func(a *DockArea, _ *gpui.App) int {
		tree, _ := a.Layout(DockPlacementCenter)
		node, _ := tree.FindPanelNode(b)
		found, _ := tree.FindNode(node)
		return found.Kind().ActiveIx
	})
	if activeIx != 1 {
		t.Fatalf("active_ix = %d, want the selected tab displayed", activeIx)
	}
	want := []PanelID{testPanelID(panels[0]), testPanelID(panels[1]), testPanelID(panels[2])}
	if !slices.Equal(order, want) {
		t.Fatalf("selecting a tab moved it: %v", order)
	}
}

// U:dock/dock_area.rs:3728-3746
func TestDockAreaSelectPanelIsSilentForTheDisplayedOrAnUnknownPanel(t *testing.T) {
	log := newDockLog()
	area, panels, cx := oneGroup(t, log, -1, "A", "B")
	cx.RunUntilParked()
	drainLog(log)
	a0 := testPanelID(panels[0])
	var stranger PanelID
	cx.Update(func(_ *gpui.Window, app *gpui.App) { stranger = testPanelID(loggingTestPanel("Z", log, app)) })
	updateArea(cx, area, func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) {
		a.SelectPanel(a0, w, acx)
		a.SelectPanel(stranger, w, acx)
	})
	cx.RunUntilParked()
	requireActive(t, drainActive(log))
}

// U:dock/dock_area.rs:3748-3760
func TestDockAreaReselectingActiveTabStaysSilent(t *testing.T) {
	log := newDockLog()
	area, _, cx := oneGroup(t, log, -1, "A", "B")
	cx.RunUntilParked()
	drainLog(log)
	group := groupOf(cx, area, 0)
	updateGroup(cx, group, func(g *TabGroup, w *gpui.Window, gcx *gpui.Context[TabGroup]) { g.SelectTab(0, w, gcx) })
	cx.RunUntilParked()
	requireActive(t, drainActive(log))
}

// twoPairGroups installs two groups of two logging panels side by side.
func twoPairGroups(t *testing.T, log dockLog, names [4]string) (gpui.Entity[DockArea], []gpui.Entity[testPanel], *gpui.VisualTestContext) {
	t.Helper()
	area, cx := setupDockArea(t)
	var panels []gpui.Entity[testPanel]
	updateArea(cx, area, func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) {
		for _, name := range names {
			if name != "" {
				panels = append(panels, loggingTestPanel(name, log, acx.App()))
			}
		}
		left := DockLayoutTabs()
		right := DockLayoutTabs()
		split := 2
		if names[1] == "" {
			split = 1
		}
		for i, panel := range panels {
			if i < split {
				left.Panel(panel)
			} else {
				right.Panel(panel)
			}
		}
		a.SetCenter(DockLayoutHSplit().Child(left, nil).Child(right, nil), w, acx)
	})
	cx.RunUntilParked()
	drainLog(log)
	return area, panels, cx
}

// U:dock/dock_area.rs:3762-3802
func TestDockAreaInsertingAtActiveIxSwapsNotifications(t *testing.T) {
	log := newDockLog()
	area, panels, cx := twoPairGroups(t, log, [4]string{"A", "B", "X", "C"})
	c := testPanelID(panels[3])
	movePanelInto(cx, area, c, childNode(cx, area, 0), 0, true)
	requireActive(t, drainActive(log), activeEntry{"A", false}, activeEntry{"C", true})
	group := groupOf(cx, area, 0)
	if ix := readGroup(cx, group, func(g *TabGroup, _ *gpui.App) int { return g.ActiveIx() }); ix != 0 {
		t.Fatalf("active_ix = %d", ix)
	}
	if first := readGroup(cx, group, func(g *TabGroup, app *gpui.App) PanelID { return g.Panels()[0].PanelID(app) }); first != c {
		t.Fatal("the arriving panel did not take the slot it named")
	}
}

// U:dock/dock_area.rs:3804-3827
func TestDockAreaRemovingBeforeActiveKeepsDisplayedPanel(t *testing.T) {
	log := newDockLog()
	area, panels, cx := oneGroup(t, log, -1, "A", "B", "C")
	group := groupOf(cx, area, 0)
	updateGroup(cx, group, func(g *TabGroup, w *gpui.Window, gcx *gpui.Context[TabGroup]) { g.SelectTab(1, w, gcx) })
	cx.RunUntilParked()
	drainLog(log)
	updateArea(cx, area, func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) { a.RemovePanel(panels[0], w, acx) })
	cx.RunUntilParked()
	requireActive(t, drainActive(log))
	if ix := readGroup(cx, group, func(g *TabGroup, _ *gpui.App) int { return g.ActiveIx() }); ix != 0 {
		t.Fatalf("active_ix = %d", ix)
	}
	if first := readGroup(cx, group, func(g *TabGroup, app *gpui.App) PanelID { return g.Panels()[0].PanelID(app) }); first != testPanelID(panels[1]) {
		t.Fatal("the same panel is not still displayed at its new index")
	}
}

// U:dock/dock_area.rs:3829-3865
func TestDockAreaCollapseAndExpandNotifyActivePanel(t *testing.T) {
	log := newDockLog()
	area, cx := setupDockArea(t)
	updateArea(cx, area, func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) {
		a.SetDock(DockPlacementLeft, DockLayoutTabs().
			Panel(loggingTestPanel("A", log, acx.App())).
			Panel(loggingTestPanel("B", log, acx.App())), w, acx)
	})
	cx.RunUntilParked()
	drainLog(log)
	toggle := func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) {
		a.ToggleDock(DockPlacementLeft, w, acx)
	}
	updateArea(cx, area, toggle)
	cx.RunUntilParked()
	requireActive(t, drainActive(log), activeEntry{"A", false})
	updateArea(cx, area, toggle)
	cx.RunUntilParked()
	requireActive(t, drainActive(log), activeEntry{"A", true})
}

// U:dock/dock_area.rs:3867-3906
func TestDockAreaBackgroundAddIsSilentButFirstPanelIsNot(t *testing.T) {
	log := newDockLog()
	area, panels, cx := twoPairGroups(t, log, [4]string{"A", "B", "C", "D"})
	// D is a background tab in its own group and arrives as a background
	// tab in the other one, so nothing changes for anybody.
	movePanelInto(cx, area, testPanelID(panels[3]), childNode(cx, area, 0), -1, false)
	requireActive(t, drainActive(log))
	// The first panel of a region that had none is displayed regardless.
	updateArea(cx, area, func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) {
		a.AddPanel(loggingTestPanel("E", log, acx.App()), DockPlacementLeft, nil, w, acx)
	})
	cx.RunUntilParked()
	requireActive(t, drainActive(log), activeEntry{"E", true})
}

// U:dock/dock_area.rs:3908-3944
func TestDockAreaDragActivePanelToOtherGroupStaysSilentForIt(t *testing.T) {
	log := newDockLog()
	area, panels, cx := twoPairGroups(t, log, [4]string{"A", "B", "C", ""})
	movePanelInto(cx, area, testPanelID(panels[0]), childNode(cx, area, 1), -1, true)
	// Two groups reconcile independently, so their deliveries interleave
	// in no guaranteed order.
	seen := drainActive(log)
	if !slices.Contains(seen, activeEntry{"B", true}) || !slices.Contains(seen, activeEntry{"C", false}) {
		t.Fatalf("got %v", seen)
	}
	for _, e := range seen {
		if e.name == "A" {
			t.Fatalf("the moved panel was displayed before and after: %v", seen)
		}
	}
}

// U:dock/dock_area.rs:3946-3974
func TestDockAreaDragActivePanelToBackgroundSlotDeactivatesIt(t *testing.T) {
	log := newDockLog()
	area, cx := setupDockArea(t)
	var a gpui.Entity[testPanel]
	updateArea(cx, area, func(da *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) {
		app := acx.App()
		a = loggingTestPanel("A", log, app)
		da.SetCenter(DockLayoutHSplit().
			Child(DockLayoutTabs().Panel(a), nil).
			Child(DockLayoutTabs().Panel(loggingTestPanel("C", log, app)).Panel(loggingTestPanel("D", log, app)), nil), w, acx)
	})
	cx.RunUntilParked()
	drainLog(log)
	movePanelInto(cx, area, testPanelID(a), childNode(cx, area, 1), -1, false)
	requireActive(t, drainActive(log), activeEntry{"A", false})
}

// recordingSkin records the tab bars drawn (U:dock/dock_area.rs:3976-4008).
type recordingSkin struct {
	tabBars *[]NodeID
}

type recordingTabGroup struct {
	drawn *[]NodeID
}

func (s recordingSkin) TabGroupRenderer() TabGroupRenderer {
	return recordingTabGroup{drawn: s.tabBars}
}

func (r recordingTabGroup) RenderTabBar(group *TabGroupContext, _ *gpui.Window, _ *gpui.App) gpui.AnyElement {
	*r.drawn = append(*r.drawn, group.Node())
	return gpui.Empty()
}

// U:dock/dock_area.rs:4036-4084
func TestDockAreaAZoomedGroupIsDrawnWholeRatherThanAsItsBarePanel(t *testing.T) {
	log := newDockLog()
	tabBars := new([]NodeID)
	ta := gpui.NewTestApp()
	shutdownAtCleanup(t, ta)
	area, cx := openDockArea(ta, recordingSkin{tabBars: tabBars})
	updateArea(cx, area, func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) {
		a.SetCenter(DockLayoutHSplit().
			Child(DockLayoutTabs().Panel(loggingTestPanel("Alpha", log, acx.App())), nil).
			Child(DockLayoutTabs().Panel(loggingTestPanel("Beta", log, acx.App())), nil), w, acx)
	})
	cx.RunUntilParked()
	zoomed, other := childNode(cx, area, 0), childNode(cx, area, 1)
	if !slices.Contains(*tabBars, zoomed) || !slices.Contains(*tabBars, other) {
		t.Fatalf("both groups draw their own tab bar while nothing is zoomed: %v", *tabBars)
	}
	*tabBars = nil
	group := groupOf(cx, area, 0)
	updateGroup(cx, group, func(g *TabGroup, w *gpui.Window, gcx *gpui.Context[TabGroup]) { g.ToggleZoom(w, gcx) })
	cx.RunUntilParked()
	if !slices.Contains(*tabBars, zoomed) {
		t.Fatalf("a zoomed group is rendered whole, its own tab bar included: %v", *tabBars)
	}
	if slices.Contains(*tabBars, other) {
		t.Fatalf("the zoomed group is not the only thing on screen: %v", *tabBars)
	}
}

// U:dock/dock_area.rs:4086-4126
func TestDockAreaClearingTheZoomFromOutsidePutsTheGroupsOwnFlagBack(t *testing.T) {
	log := newDockLog()
	area, _, cx := twoGroups(t, log)
	cx.RunUntilParked()
	drainLog(log)
	node := childNode(cx, area, 0)
	group := groupOf(cx, area, 0)
	updateGroup(cx, group, func(g *TabGroup, w *gpui.Window, gcx *gpui.Context[TabGroup]) { g.ToggleZoom(w, gcx) })
	cx.RunUntilParked()
	zoomedNode := readArea(cx, area, func(a *DockArea, _ *gpui.App) [2]uint64 {
		n, ok := a.ZoomedGroup()
		if !ok {
			return [2]uint64{}
		}
		return [2]uint64{n.AsU64(), 1}
	})
	if zoomedNode != [2]uint64{node.AsU64(), 1} {
		t.Fatalf("zoomed group = %v, want %v", zoomedNode, node)
	}
	if !readGroup(cx, group, func(g *TabGroup, _ *gpui.App) bool { return g.IsZoomed() }) {
		t.Fatal("the group is not zoomed")
	}
	if got := zoomSignals(log); !slices.Equal(got, []dockLogEntry{{"Alpha", sigZoomed(true)}}) {
		t.Fatalf("zoom signals = %v", got)
	}

	updateArea(cx, area, func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) { a.SetZoomedOut(w, acx) })
	cx.RunUntilParked()
	if readArea(cx, area, func(a *DockArea, _ *gpui.App) bool { return a.IsZoomed() }) {
		t.Fatal("the area is still zoomed")
	}
	if readGroup(cx, group, func(g *TabGroup, _ *gpui.App) bool { return g.IsZoomed() }) {
		t.Fatal("a group left flagged zoomed would stay locked and refuse every drop")
	}
	if !groupContext(cx, group).IsDroppable() {
		t.Fatal("the lock the zoom imposed was not lifted")
	}
	if got := zoomSignals(log); !slices.Equal(got, []dockLogEntry{{"Alpha", sigZoomed(false)}}) {
		t.Fatalf("the panel did not hear the zoom end: %v", got)
	}
}

// U:dock/dock_area.rs:4128-4147
func TestDockAreaAGroupThatRefusesToZoomLeavesTheAreaUnzoomed(t *testing.T) {
	log := newDockLog()
	area, alpha, cx := twoGroups(t, log)
	cx.RunUntilParked()
	cx.Update(func(_ *gpui.Window, app *gpui.App) {
		alpha.Update(app, func(p *testPanel, pcx *gpui.Context[testPanel]) { p.setZoomable(false, pcx) })
	})
	node := childNode(cx, area, 0)
	group := groupOf(cx, area, 0)
	updateArea(cx, area, func(a *DockArea, w *gpui.Window, acx *gpui.Context[DockArea]) { a.SetZoomedIn(node, w, acx) })
	cx.RunUntilParked()
	if readGroup(cx, group, func(g *TabGroup, _ *gpui.App) bool { return g.IsZoomed() }) {
		t.Fatal("the group zoomed")
	}
	if readArea(cx, area, func(a *DockArea, _ *gpui.App) bool { return a.IsZoomed() }) {
		t.Fatal("the area must not fill itself with a group that never zoomed")
	}
}
