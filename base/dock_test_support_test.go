package base

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Port of U:dock/test_support.rs (the shared panel double and its ordered
// delivery log), plus the ported tests of U:dock/panel.rs (2/2) and
// U:dock/layout/builder.rs (4/4).

// panelSignalKind is the PanelSignal variant (U:dock/test_support.rs:19-39).
type panelSignalKind uint8

const (
	signalActive panelSignalKind = iota
	signalZoomed
	signalAdded
	signalRemoved
)

// panelSignal is one thing a panel was told.
type panelSignal struct {
	kind  panelSignalKind
	value bool
}

func sigActive(v bool) panelSignal { return panelSignal{signalActive, v} }
func sigZoomed(v bool) panelSignal { return panelSignal{signalZoomed, v} }

var (
	sigAdded   = panelSignal{kind: signalAdded}
	sigRemoved = panelSignal{kind: signalRemoved}
)

// dockLogEntry is one (panel name, signal) delivery.
type dockLogEntry struct {
	name   string
	signal panelSignal
}

// activeEntry is one (panel name, active) edge.
type activeEntry struct {
	name   string
	active bool
}

// dockLog is the shared, cross-panel ordered log (U:dock/test_support.rs:41-46).
type dockLog = *[]dockLogEntry

func newDockLog() dockLog { return new([]dockLogEntry) }

// drainLog takes everything logged so far (U:dock/test_support.rs:48-51).
func drainLog(log dockLog) []dockLogEntry {
	out := *log
	*log = nil
	return out
}

// drainActive keeps only the SetActive edges (U:dock/test_support.rs:53-64).
func drainActive(log dockLog) []activeEntry {
	var out []activeEntry
	for _, e := range drainLog(log) {
		if e.signal.kind == signalActive {
			out = append(out, activeEntry{e.name, e.signal.value})
		}
	}
	return out
}

func logContains(entries []dockLogEntry, name string, signal panelSignal) bool {
	return slices.Contains(entries, dockLogEntry{name, signal})
}

func requireActive(t *testing.T, got []activeEntry, want ...activeEntry) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("active edges = %v, want %v", got, want)
	}
}

// testPanel is the shared panel double (U:dock/test_support.rs:66-155).
type testPanel struct {
	name        string
	focusHandle gpui.FocusHandle
	log         dockLog
	visible     bool
	zoomable    bool
	group       *gpui.WeakEntity[TabGroup]
}

// newTestPanel is a panel whose deliveries go nowhere
// (U:dock/test_support.rs:76-80).
func newTestPanel(name string, app *gpui.App) gpui.Entity[testPanel] {
	return loggingTestPanel(name, newDockLog(), app)
}

// loggingTestPanel is U:dock/test_support.rs:82-92.
func loggingTestPanel(name string, log dockLog, app *gpui.App) gpui.Entity[testPanel] {
	return gpui.NewEntity(app, app.RootScope(), func(p *testPanel, cx *gpui.Context[testPanel]) {
		*p = testPanel{name: name, focusHandle: cx.App().FocusHandle(), log: log, visible: true, zoomable: true}
	})
}

func (p *testPanel) setVisible(visible bool, cx *gpui.Context[testPanel]) {
	p.visible = visible
	cx.Notify()
}

func (p *testPanel) setZoomable(zoomable bool, cx *gpui.Context[testPanel]) {
	p.zoomable = zoomable
	cx.Notify()
}

func (p *testPanel) record(signal panelSignal) {
	*p.log = append(*p.log, dockLogEntry{p.name, signal})
}

func (p *testPanel) PanelName() string                                           { return p.name }
func (p *testPanel) Visible(*gpui.App) bool                                      { return p.visible }
func (p *testPanel) Zoomable(*gpui.App) bool                                     { return p.zoomable }
func (p *testPanel) FocusHandle(*gpui.App) gpui.FocusHandle                      { return p.focusHandle }
func (*testPanel) Render(*gpui.Window, *gpui.Context[testPanel]) gpui.AnyElement { return gpui.Empty() }

func (p *testPanel) SetActive(active bool, _ *gpui.Window, _ *gpui.Context[testPanel]) {
	p.record(sigActive(active))
}

func (p *testPanel) SetZoomed(zoomed bool, _ *gpui.Window, _ *gpui.Context[testPanel]) {
	p.record(sigZoomed(zoomed))
}

func (p *testPanel) OnAddedTo(group gpui.WeakEntity[TabGroup], _ *gpui.Window, _ *gpui.Context[testPanel]) {
	p.group = &group
	p.record(sigAdded)
}

func (p *testPanel) OnRemoved(_ *gpui.Window, _ *gpui.Context[testPanel]) {
	p.group = nil
	p.record(sigRemoved)
}

// testPanelID is U:dock/test_support.rs:157-159.
func testPanelID(panel gpui.Entity[testPanel]) PanelID { return PanelIDOf(panel.EntityID()) }

func testPanelView(panel gpui.Entity[testPanel]) PanelView { return PanelViewOf(panel) }

// buildGroup opens a window holding one tab group over freshly built
// panels named names, with the first tab displayed
// (U:dock/test_support.rs:161-196).
func buildGroup(t *testing.T, log dockLog, names ...string) (gpui.Entity[TabGroup], []gpui.Entity[testPanel], *gpui.VisualTestContext) {
	t.Helper()
	ta := gpui.NewTestApp()
	shutdownAtCleanup(t, ta)
	group, cx := ta.AddWindowView(func(g *TabGroup, w *gpui.Window, gcx *gpui.Context[TabGroup]) {
		*g = NewTabGroup(NodeIDFromU64(1), w, gcx)
	})
	var panels []gpui.Entity[testPanel]
	cx.Update(func(w *gpui.Window, app *gpui.App) {
		views := make([]PanelView, 0, len(names))
		for _, name := range names {
			panel := loggingTestPanel(name, log, app)
			panels = append(panels, panel)
			views = append(views, testPanelView(panel))
		}
		group.Update(app, func(g *TabGroup, gcx *gpui.Context[TabGroup]) {
			// A freshly built group is sealed; place it beside siblings.
			g.SetConstraints(TabGroupConstraintsInSplit(false), w, gcx)
			g.SyncFromTree(views, 0, w, gcx)
		})
	})
	return group, panels, cx
}

// probePanel is U:dock/panel.rs:256-281.
type probePanel struct {
	focusHandle gpui.FocusHandle
	visible     bool
}

func (*probePanel) PanelName() string                        { return "Probe" }
func (p *probePanel) Visible(*gpui.App) bool                 { return p.visible }
func (p *probePanel) FocusHandle(*gpui.App) gpui.FocusHandle { return p.focusHandle }
func (*probePanel) Render(*gpui.Window, *gpui.Context[probePanel]) gpui.AnyElement {
	return gpui.Empty()
}

func newProbe(ta *gpui.TestApp, visible bool) gpui.Entity[probePanel] {
	var panel gpui.Entity[probePanel]
	ta.Update(func(app *gpui.App) {
		panel = gpui.NewEntity(app, app.RootScope(), func(p *probePanel, cx *gpui.Context[probePanel]) {
			*p = probePanel{focusHandle: cx.App().FocusHandle(), visible: visible}
		})
	})
	return panel
}

// U:dock/panel.rs:283-296
func TestDockPanelAPanelEntityAnswersThroughTheObjectSafeView(t *testing.T) {
	ta := gpui.NewTestApp()
	shutdownAtCleanup(t, ta)
	panel := newProbe(ta, false)
	view := PanelViewOf(panel)
	ta.Update(func(app *gpui.App) {
		if view.PanelName(app) != "Probe" || view.Visible(app) || view.PanelID(app) != PanelIDOf(panel.EntityID()) {
			t.Fatalf("view answered %q %v %v", view.PanelName(app), view.Visible(app), view.PanelID(app))
		}
	})
}

// U:dock/panel.rs:298-309
func TestDockPanelTheDefaultDumpRecordsOnlyThePanelName(t *testing.T) {
	ta := gpui.NewTestApp()
	shutdownAtCleanup(t, ta)
	panel := newProbe(ta, true)
	var state PanelState
	ta.Update(func(app *gpui.App) { state = PanelViewOf(panel).Dump(app) })
	if state.PanelName != "Probe" || len(state.Children) != 0 || !state.Info.Equal(PanelInfoPanel(json.RawMessage("null"))) {
		t.Fatalf("state = %+v", state)
	}
}

// fromLayout runs PaneTree::from_layout over panels built in a fresh app.
func fromLayout(t *testing.T, build func(app *gpui.App) *DockLayout, rootKind RootKind) (PaneTree, []dockLayoutPanel) {
	t.Helper()
	ta := gpui.NewTestApp()
	shutdownAtCleanup(t, ta)
	var tree PaneTree
	var panels []dockLayoutPanel
	ta.Update(func(app *gpui.App) { tree, panels = paneTreeFromLayout(build(app), rootKind) })
	return tree, panels
}

// U:dock/layout/builder.rs:221-247
func TestDockBuilderADescribedSplitLowersToASplitOfTabGroups(t *testing.T) {
	tree, panels := fromLayout(t, func(app *gpui.App) *DockLayout {
		return DockLayoutHSplit().
			Child(DockLayoutTabs().Panel(newTestPanel("Alpha", app)), dpx(300)).
			Child(DockLayoutTabs().Panel(newTestPanel("Beta", app)), nil)
	}, RootKindSplit)
	if len(panels) != 2 {
		t.Fatalf("panels = %d", len(panels))
	}
	k := tree.Root().Kind()
	if !k.IsSplit || k.Axis != gpui.AxisHorizontal || len(k.Children) != 2 {
		t.Fatalf("root = %+v", k)
	}
	requireSizes(t, k.Sizes, optSizes(dpx(300), nil), "described sizes")
	if c := k.Children[0].Kind(); c.IsSplit || len(c.Panels) != 1 {
		t.Fatalf("first child = %+v", c)
	}
}

// U:dock/layout/builder.rs:249-277
func TestDockBuilderSplitWithSizesAddsEachChildOnce(t *testing.T) {
	tree, _ := fromLayout(t, func(app *gpui.App) *DockLayout {
		return DockLayoutHSplit().
			Child(DockLayoutTabs().Panel(newTestPanel("Alpha", app)), nil).
			Child(DockLayoutTabs().Panel(newTestPanel("Beta", app)), nil)
	}, RootKindSplit)
	k := tree.Root().Kind()
	if len(k.Children) != 2 || len(k.Sizes) != 2 {
		t.Fatalf("children = %d sizes = %d: each described child appears exactly once", len(k.Children), len(k.Sizes))
	}
	if k.Children[0].ID() == k.Children[1].ID() {
		t.Fatal("the children share an id")
	}
}

// U:dock/layout/builder.rs:279-287
func TestDockBuilderABareTabGroupIsWrappedForASplitRoot(t *testing.T) {
	tree, _ := fromLayout(t, func(app *gpui.App) *DockLayout {
		return DockLayoutTabs().Panel(newTestPanel("Alpha", app))
	}, RootKindSplit)
	if !tree.Root().Kind().IsSplit {
		t.Fatal("the root is not a split")
	}
}

// U:dock/layout/builder.rs:289-304
func TestDockBuilderAnActiveIndexSelectsTheDisplayedTab(t *testing.T) {
	tree, _ := fromLayout(t, func(app *gpui.App) *DockLayout {
		return DockLayoutTabs().Panel(newTestPanel("Alpha", app)).Panel(newTestPanel("Beta", app)).ActiveIndex(1)
	}, RootKindAny)
	if k := tree.Root().Kind(); k.IsSplit || k.ActiveIx != 1 {
		t.Fatalf("root = %+v", k)
	}
}
