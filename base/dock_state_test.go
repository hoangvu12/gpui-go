package base

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ported tests of U:dock/state.rs (3/3) and U:dock/state_convert.rs
// (15/15). The frozen fixtures are copied under testdata/dock.

func dockFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "dock", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func decodeDockAreaState(t *testing.T, data []byte) DockAreaState {
	t.Helper()
	var state DockAreaState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	return state
}

func requireJSONValue(t *testing.T, value any, want string) {
	t.Helper()
	got, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	// Numbers compare as float64: Go writes the f32 1.0 as 1, serde as 1.0.
	var a, b any
	if err := json.Unmarshal(got, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(want), &b); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("json = %s, want %s", got, want)
	}
}

// U:dock/state.rs:198-242
func TestDockStateTheShippedFixtureStillDeserializes(t *testing.T) {
	state := decodeDockAreaState(t, dockFixture(t, "layout.json"))

	if state.Version != nil {
		t.Fatalf("version = %v", *state.Version)
	}
	c := state.Center
	if c.PanelName != "StackPanel" || len(c.Children) != 2 || c.Children[0].PanelName != "TabPanel" ||
		c.Children[1].PanelName != "TabPanel" || len(c.Children[1].Children) != 1 ||
		c.Children[1].Children[0].PanelName != "StoryContainer" {
		t.Fatalf("center = %+v", c)
	}
	for _, tc := range []struct {
		dock      *DockState
		size      float32
		placement DockPlacement
		children  int
	}{
		{state.LeftDock, 350, DockPlacementLeft, 1},
		{state.BottomDock, 200, DockPlacementBottom, 2},
		{state.RightDock, 320, DockPlacementRight, 1},
	} {
		if tc.dock == nil {
			t.Fatalf("%v dock missing", tc.placement)
		}
		if !tc.dock.Open() || tc.dock.Size() != tc.size || tc.dock.Placement() != tc.placement {
			t.Fatalf("%v dock = open %v size %v placement %v", tc.placement, tc.dock.Open(), tc.dock.Size(), tc.dock.Placement())
		}
		panel := tc.dock.Panel()
		if panel.PanelName != "TabPanel" || len(panel.Children) != tc.children || panel.Children[0].PanelName != "StoryContainer" {
			t.Fatalf("%v dock panel = %+v", tc.placement, panel)
		}
	}
}

// U:dock/state.rs:244-257
func TestDockStateTheSerdeTagsAreFrozen(t *testing.T) {
	requireJSONValue(t, PanelInfoStack([]float32{1}, gpui.AxisVertical), `{"stack": {"sizes": [1.0], "axis": 1}}`)
	requireJSONValue(t, PanelInfoTabs(2), `{"tabs": {"active_index": 2}}`)
	requireJSONValue(t, DockPlacementBottom, `"bottom"`)
}

// U:dock/state.rs:259-266
func TestDockStateOptionalDocksAreOmittedNotNulled(t *testing.T) {
	data, err := json.Marshal(DockAreaState{})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"left_dock", "right_dock", "bottom_dock"} {
		if _, ok := fields[key]; ok {
			t.Fatalf("%s is present in %s", key, data)
		}
	}
}

// fakePanels is a PanelSource backed by a fixed map
// (U:dock/state_convert.rs:197-219).
type fakePanels []struct {
	id   PanelID
	name string
}

func (f fakePanels) PanelName(id PanelID) string {
	for _, p := range f {
		if p.id == id {
			return p.name
		}
	}
	return "Unknown"
}

func (fakePanels) IsVisible(PanelID) bool { return true }

func (f fakePanels) Dump(id PanelID) PanelState {
	return PanelState{PanelName: f.PanelName(id), Info: PanelInfoPanel(json.RawMessage("null"))}
}

func requireStackSizes(t *testing.T, info PanelInfo, want []float32, msg string) {
	t.Helper()
	sizes, ok := info.Sizes()
	if !ok {
		t.Fatalf("%s: expected Stack info, got %v", msg, info)
	}
	if !slices.Equal(sizes, want) {
		t.Fatalf("%s: sizes = %v, want %v", msg, sizes, want)
	}
}

// U:dock/state_convert.rs:221-243
func TestDockStateConvertASplitSerializesAsAStackPanel(t *testing.T) {
	tree := NewPaneTree(RootKindSplit)
	tree.pushTabsForTest(tree.Root().ID(), []PanelID{dpid(1)})
	tree.pushTabsForTest(tree.Root().ID(), []PanelID{dpid(2)})
	tree.Normalize()

	state := tree.ToState(fakePanels{{dpid(1), "Alpha"}, {dpid(2), "Beta"}})
	if state.PanelName != "StackPanel" {
		t.Fatalf("name = %q", state.PanelName)
	}
	// Both slots were pushed with no explicit size, so both serialize as
	// the zero sentinel.
	requireStackSizes(t, state.Info, []float32{0, 0}, "stack")
	if state.Children[0].PanelName != "TabPanel" || state.Children[0].Children[0].PanelName != "Alpha" {
		t.Fatalf("children = %+v", state.Children)
	}
}

// U:dock/state_convert.rs:245-265
func TestDockStateConvertAnUnresolvedSlotSizeSerializesAsTheZeroSentinel(t *testing.T) {
	tree := NewPaneTree(RootKindSplit)
	root := tree.Root().ID()
	tree.pushSizedTabsForTest(root, []PanelID{dpid(1)}, dpx(120))
	tree.pushSizedTabsForTest(root, []PanelID{dpid(2)}, nil)
	tree.Normalize()

	state := tree.ToState(fakePanels{{dpid(1), "Alpha"}, {dpid(2), "Beta"}})
	requireStackSizes(t, state.Info, []float32{120, 0}, "only the unresolved slot is written as 0.0")
}

// U:dock/state_convert.rs:267-279
func TestDockStateConvertAnEmptyTabGroupSerializesAsTabsNotAsAPanel(t *testing.T) {
	tree := NewPaneTree(RootKindAny)
	tree.setRootTabsForTest(nil, 0)
	state := tree.ToState(fakePanels{})
	if state.PanelName != "TabPanel" {
		t.Fatalf("name = %q", state.PanelName)
	}
	if ix, ok := state.Info.ActiveIndex(); !ok || ix != 0 {
		t.Fatalf("info = %v: the old writer emitted PanelInfo::Panel here", state.Info)
	}
}

// U:dock/state_convert.rs:281-287
func TestDockStateConvertAnEmptyCenterStillSerializesAsAStackPanel(t *testing.T) {
	tree := NewPaneTree(RootKindSplit)
	state := tree.ToState(fakePanels{})
	if state.PanelName != "StackPanel" || !state.Info.IsStack() {
		t.Fatalf("state = %+v", state)
	}
}

// recordingBuilder assigns each leaf an id in encounter order
// (U:dock/state_convert.rs:289-301).
type recordingBuilder struct {
	built []string
}

func (b *recordingBuilder) Build(state *PanelState, _ *PanelInfo) PanelID {
	b.built = append(b.built, state.PanelName)
	return PanelIDFromU64(uint64(len(b.built)))
}

// tabsState is U:dock/state_convert.rs:303-309.
func tabsState(children []PanelState, activeIndex int) PanelState {
	return PanelState{PanelName: tabPanelName, Children: children, Info: PanelInfoTabs(activeIndex)}
}

// panelState is U:dock/state_convert.rs:311-317.
func panelState(name string) PanelState {
	return PanelState{PanelName: name, Info: PanelInfoPanel(json.RawMessage("null"))}
}

// U:dock/state_convert.rs:319-338
func TestDockStateConvertNestedTabGroupsAreFlattened(t *testing.T) {
	state := tabsState([]PanelState{
		tabsState([]PanelState{panelState("Alpha")}, 0),
		tabsState([]PanelState{panelState("Beta")}, 0),
	}, 1)
	var builder recordingBuilder
	tree := PaneTreeFromState(&state, RootKindAny, &builder)
	if !slices.Equal(builder.built, []string{"Alpha", "Beta"}) {
		t.Fatalf("built = %v", builder.built)
	}
	k := tree.Root().Kind()
	if k.IsSplit || len(k.Panels) != 2 || k.ActiveIx != 1 {
		t.Fatalf("root = %+v", k)
	}
}

// U:dock/state_convert.rs:340-346
func TestDockStateConvertABarePanelLeafIsWrappedInATabGroup(t *testing.T) {
	var builder recordingBuilder
	state := panelState("Alpha")
	tree := PaneTreeFromState(&state, RootKindAny, &builder)
	if k := tree.Root().Kind(); k.IsSplit || len(k.Panels) != 1 {
		t.Fatalf("root = %+v", k)
	}
}

// U:dock/state_convert.rs:348-365
func TestDockStateConvertATabPanelCarryingPanelInfoIsReadAsAnEmptyGroup(t *testing.T) {
	// What the old TabPanel::dump wrote for an empty tab group.
	state := PanelState{PanelName: tabPanelName, Info: PanelInfoPanel(json.RawMessage("null"))}
	var builder recordingBuilder
	tree := PaneTreeFromState(&state, RootKindAny, &builder)
	if len(builder.built) != 0 {
		t.Fatal("a panel was built for the phantom leaf")
	}
	if k := tree.Root().Kind(); k.IsSplit || len(k.Panels) != 0 {
		t.Fatalf("root = %+v", k)
	}
}

// U:dock/state_convert.rs:367-374
func TestDockStateConvertASplitRootIsForcedEvenWhenTheStateIsATabGroup(t *testing.T) {
	state := tabsState([]PanelState{panelState("Alpha")}, 0)
	var builder recordingBuilder
	tree := PaneTreeFromState(&state, RootKindSplit, &builder)
	if !tree.Root().Kind().IsSplit {
		t.Fatal("the root is not a split")
	}
}

// preservingPanels round-trips leaves by remembering the exact PanelState
// each id came from (U:dock/state_convert.rs:376-410).
type preservingPanels struct {
	states []PanelState
}

func (p *preservingPanels) Build(state *PanelState, _ *PanelInfo) PanelID {
	p.states = append(p.states, state.Clone())
	return PanelIDFromU64(uint64(len(p.states)))
}

func (p *preservingPanels) PanelName(id PanelID) string { return p.states[id.AsU64()-1].PanelName }

func (*preservingPanels) IsVisible(PanelID) bool { return true }

func (p *preservingPanels) Dump(id PanelID) PanelState { return p.states[id.AsU64()-1].Clone() }

// canonicalize is U:dock/state_convert.rs:412-417.
func canonicalize(t *testing.T, data []byte) PanelState {
	t.Helper()
	state := decodeDockAreaState(t, data)
	panels := &preservingPanels{}
	tree := PaneTreeFromState(&state.Center, RootKindSplit, panels)
	return tree.ToState(panels)
}

// U:dock/state_convert.rs:419-438
func TestDockStateConvertCanonicalizationReachesAFixpointInOnePass(t *testing.T) {
	for _, name := range []string{"layout.json", "nested_splits.json", "legacy_empty_tab_group.json", "unregistered_panel.json", "zero_size_sentinel.json"} {
		once := canonicalize(t, dockFixture(t, name))
		wrapped, err := json.Marshal(DockAreaState{Center: once.Clone()})
		if err != nil {
			t.Fatal(err)
		}
		twice := canonicalize(t, wrapped)
		if !once.Equal(&twice) {
			t.Fatalf("%s: r(r(x)) != r(x)", name)
		}
	}
}

// U:dock/state_convert.rs:440-450
func TestDockStateConvertAnUnregisteredPanelKeepsItsPayloadThroughARoundTrip(t *testing.T) {
	state := canonicalize(t, dockFixture(t, "unregistered_panel.json"))
	leaf := state.Children[0].Children[0]
	if leaf.PanelName != "PanelFromTheFuture" {
		t.Fatalf("name = %q", leaf.PanelName)
	}
	if !leaf.Info.Equal(PanelInfoPanel(json.RawMessage(`{"keep": "me"}`))) {
		t.Fatalf("info = %v", leaf.Info)
	}
}

// U:dock/state_convert.rs:452-459
func TestDockStateConvertTheLegacyEmptyTabGroupIsRewrittenIntoTheTabsForm(t *testing.T) {
	state := canonicalize(t, dockFixture(t, "legacy_empty_tab_group.json"))
	// The empty group collapses, leaving the mandatory split root.
	if state.PanelName != "StackPanel" || len(state.Children) != 0 {
		t.Fatalf("state = %+v", state)
	}
}

// U:dock/state_convert.rs:461-499
func TestDockStateConvertNestedSameAxisSplitsAreFlattenedAndSingleChildSplitsCollapse(t *testing.T) {
	state := canonicalize(t, dockFixture(t, "nested_splits.json"))
	if state.PanelName != "StackPanel" || len(state.Children) != 3 {
		t.Fatalf("state = %s with %d children", state.PanelName, len(state.Children))
	}
	for i, want := range []string{"Alpha", "Beta", "Gamma"} {
		child := state.Children[i]
		if child.PanelName != "TabPanel" || child.Children[0].PanelName != want {
			t.Fatalf("child %d = %+v, want a TabPanel holding %s", i, child, want)
		}
	}
	requireStackSizes(t, state.Info, []float32{100, 300, 300},
		"the spliced-in sizes are scaled by outer/inner (400/200 = 2x), and the collapsed split hands its own outer slot (300.0) to its surviving child")
}

// U:dock/state_convert.rs:501-517
func TestDockStateConvertATreeSurvivesARoundTripExactly(t *testing.T) {
	state := decodeDockAreaState(t, dockFixture(t, "nested_splits.json"))
	panels := &preservingPanels{}
	tree := PaneTreeFromState(&state.Center, RootKindSplit, panels)
	dumped := tree.ToState(panels)
	rebuiltPanels := &preservingPanels{}
	rebuilt := PaneTreeFromState(&dumped, RootKindSplit, rebuiltPanels)
	a, b := tree.ToState(panels), rebuilt.ToState(rebuiltPanels)
	if !a.Equal(&b) {
		t.Fatal("load(dump(t)) must describe the same layout as t")
	}
}

// U:dock/state_convert.rs:519-551
func TestDockStateConvertAZeroSizeSlotRoundTripsThroughTheNoneSentinel(t *testing.T) {
	state := decodeDockAreaState(t, dockFixture(t, "zero_size_sentinel.json"))
	panels := &preservingPanels{}
	tree := PaneTreeFromState(&state.Center, RootKindSplit, panels)
	k := tree.Root().Kind()
	if !k.IsSplit {
		t.Fatal("expected the root to stay a split")
	}
	requireSizes(t, k.Sizes, optSizes(nil, dpx(200)), "the 0.0 sentinel reads back as None")
	requireStackSizes(t, tree.ToState(panels).Info, []float32{0, 200}, "the unconstrained slot writes back out as the 0.0 sentinel")
}

// U:dock/state_convert.rs:553-586
func TestDockStateConvertABareTabPanelRootIsNotWrappedUnderRootKindAny(t *testing.T) {
	var state PanelState
	if err := json.Unmarshal(dockFixture(t, "bare_tab_panel_root.json"), &state); err != nil {
		t.Fatal(err)
	}
	panels := &preservingPanels{}
	tree := PaneTreeFromState(&state, RootKindAny, panels)
	if tree.Root().Kind().IsSplit {
		t.Fatal("a bare TabPanel root must stay a Tabs node under RootKindAny")
	}
	if dumped := tree.ToState(panels); dumped.PanelName != "TabPanel" {
		t.Fatalf("dumped name = %q", dumped.PanelName)
	}
}
