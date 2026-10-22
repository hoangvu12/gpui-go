package base

import (
	"slices"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ported tests of U:dock/layout/tree.rs (2/2), U:dock/layout/edit.rs
// (13/13) and U:dock/layout/normalize.rs (11/11), with the #[cfg(test)]
// PaneTree helpers of U:dock/layout/tree.rs:167-275.

// pushTabsForTest is U:dock/layout/tree.rs:172-190.
func (t *PaneTree) pushTabsForTest(parent NodeID, panels []PanelID) NodeID {
	return t.pushSizedTabsForTest(parent, panels, nil)
}

// pushSizedTabsForTest is U:dock/layout/tree.rs:192-219.
func (t *PaneTree) pushSizedTabsForTest(parent NodeID, panels []PanelID, size *float32) NodeID {
	id := t.allocateNodeID()
	path, ok := t.pathOfNode(parent)
	if !ok {
		panic("parent must exist")
	}
	n := t.nodeAt(path)
	if !n.kind.split {
		panic("parent must be a split")
	}
	n.kind.children = append(n.kind.children, newPaneNode(id, tabsKind(panels, 0)))
	n.kind.sizes = append(n.kind.sizes, size)
	return id
}

// setRootSplitForTest is U:dock/layout/tree.rs:221-232.
func (t *PaneTree) setRootSplitForTest(axis gpui.Axis) NodeID {
	id := t.allocateNodeID()
	t.root = newPaneNode(id, splitKind(axis, nil, nil))
	return id
}

// setRootAxisForTest is U:dock/layout/tree.rs:234-238.
func (t *PaneTree) setRootAxisForTest(axis gpui.Axis) {
	if t.root.kind.split {
		t.root.kind.axis = axis
	}
}

// setRootTabsForTest is U:dock/layout/tree.rs:240-248.
func (t *PaneTree) setRootTabsForTest(panels []PanelID, activeIx int) NodeID {
	id := t.allocateNodeID()
	t.root = newPaneNode(id, tabsKind(panels, activeIx))
	return id
}

// pushSplitForTest is U:dock/layout/tree.rs:250-274.
func (t *PaneTree) pushSplitForTest(parent NodeID, axis gpui.Axis, size *float32) NodeID {
	id := t.allocateNodeID()
	path, ok := t.pathOfNode(parent)
	if !ok {
		panic("parent must exist")
	}
	n := t.nodeAt(path)
	if !n.kind.split {
		panic("parent must be a split")
	}
	n.kind.children = append(n.kind.children, newPaneNode(id, splitKind(axis, nil, nil)))
	n.kind.sizes = append(n.kind.sizes, size)
	return id
}

// normalizePassCountForTest is U:dock/layout/normalize.rs:87-92.
func (t *PaneTree) normalizePassCountForTest() int {
	passes, _ := t.runNormalizePasses()
	return passes
}

func dpid(n uint64) PanelID { return PanelIDFromU64(n) }

func dpx(v float32) *float32 { return &v }

func optSizes(sizes ...*float32) []*float32 { return sizes }

func mustNode(t *testing.T, tree *PaneTree, id NodeID) *PaneNode {
	t.Helper()
	node, ok := tree.FindNode(id)
	if !ok {
		t.Fatalf("node %d not found", id)
	}
	return node
}

func requireSizes(t *testing.T, got []*float32, want []*float32, msg string) {
	t.Helper()
	if !optPixelsSliceEqual(got, want) {
		t.Fatalf("%s: sizes = %v, want %v", msg, fmtSizes(got), fmtSizes(want))
	}
}

func fmtSizes(sizes []*float32) []any {
	out := make([]any, len(sizes))
	for i, s := range sizes {
		if s == nil {
			out[i] = "None"
		} else {
			out[i] = *s
		}
	}
	return out
}

// U:dock/layout/tree.rs:285-292
func TestDockTreeAFreshSplitRootTreeHasNoPanels(t *testing.T) {
	tree := NewPaneTree(RootKindSplit)
	if k := tree.Root().Kind(); !k.IsSplit || len(k.Children) != 0 {
		t.Fatal("a fresh split root holds no children")
	}
	if len(tree.Panels()) != 0 {
		t.Fatal("a fresh tree has panels")
	}
}

// U:dock/layout/tree.rs:294-302
func TestDockTreeNodeIDsAreUniqueAndResolvable(t *testing.T) {
	tree := NewPaneTree(RootKindSplit)
	root := tree.Root().ID()
	tabs := tree.pushTabsForTest(root, []PanelID{dpid(1)})
	if tabs == tree.Root().ID() {
		t.Fatal("the tab node reused the root id")
	}
	if _, ok := tree.FindNode(tabs); !ok {
		t.Fatal("the tab node does not resolve")
	}
	if got, ok := tree.FindPanelNode(dpid(1)); !ok || got != tabs {
		t.Fatalf("find_panel_node = %v %v, want %v", got, ok, tabs)
	}
}

// treeWithOneGroup is U:dock/layout/edit.rs:357-362.
func treeWithOneGroup() (PaneTree, NodeID) {
	tree := NewPaneTree(RootKindSplit)
	tabs := tree.pushTabsForTest(tree.Root().ID(), []PanelID{dpid(1)})
	tree.Normalize()
	return tree, tabs
}

// U:dock/layout/edit.rs:364-382
func TestDockEditInsertingIntoATabGroupAppendsAndCanActivate(t *testing.T) {
	tree, tabs := treeWithOneGroup()
	result := tree.InsertPanel(dpid(2), InsertTargetTabs{Node: tabs, Activate: true})
	if !result.Changed() {
		t.Fatal("no change reported")
	}
	k := mustNode(t, &tree, tabs).Kind()
	if !slices.Equal(k.Panels, []PanelID{dpid(1), dpid(2)}) {
		t.Fatalf("panels = %v", k.Panels)
	}
	if k.ActiveIx != 1 {
		t.Fatalf("active_ix = %d, want 1: the inserted panel becomes the displayed one", k.ActiveIx)
	}
}

// U:dock/layout/edit.rs:384-402
func TestDockEditABackgroundInsertLeavesTheActivePanelAlone(t *testing.T) {
	tree, tabs := treeWithOneGroup()
	result := tree.InsertPanel(dpid(2), InsertTargetTabs{Node: tabs})
	if !result.Changed() {
		t.Fatal("no change reported")
	}
	k := mustNode(t, &tree, tabs).Kind()
	if !slices.Equal(k.Panels, []PanelID{dpid(1), dpid(2)}) || k.ActiveIx != 0 {
		t.Fatalf("panels = %v active = %d, want the displayed panel left alone", k.Panels, k.ActiveIx)
	}
}

// U:dock/layout/edit.rs:404-415
func TestDockEditRemovingTheLastPanelCollapsesTheGroupAndReportsIt(t *testing.T) {
	tree, tabs := treeWithOneGroup()
	result := tree.RemovePanel(dpid(1))
	if !result.Changed() {
		t.Fatal("no change reported")
	}
	if tree.ContainsPanel(dpid(1)) {
		t.Fatal("the panel did not leave the tree")
	}
	if _, ok := tree.FindNode(tabs); ok {
		t.Fatal("its empty group did not collapse")
	}
	if k := tree.Root().Kind(); !k.IsSplit || len(k.Children) != 0 {
		t.Fatal("the root is not an empty split")
	}
}

// U:dock/layout/edit.rs:417-443
func TestDockEditSplittingCreatesASiblingGroupOnTheRequestedSide(t *testing.T) {
	tree, tabs := treeWithOneGroup()
	result := tree.Split(tabs, dpid(2), PlacementRight, dpx(240))
	if !result.Changed() {
		t.Fatal("no change reported")
	}
	k := tree.Root().Kind()
	if !k.IsSplit || k.Axis != gpui.AxisHorizontal || len(k.Children) != 2 {
		t.Fatalf("root = %+v", k)
	}
	if k.Sizes[1] == nil || *k.Sizes[1] != 240 {
		t.Fatalf("sizes[1] = %v", fmtSizes(k.Sizes))
	}
	if p := k.Children[0].Kind().Panels; !slices.Equal(p, []PanelID{dpid(1)}) {
		t.Fatalf("first group = %v", p)
	}
	if p := k.Children[1].Kind().Panels; !slices.Equal(p, []PanelID{dpid(2)}) {
		t.Fatalf("second group = %v", p)
	}
}

// U:dock/layout/edit.rs:445-457
func TestDockEditSplittingLeftPutsTheNewGroupFirst(t *testing.T) {
	tree, tabs := treeWithOneGroup()
	tree.Split(tabs, dpid(2), PlacementLeft, nil)
	k := tree.Root().Kind()
	if p := k.Children[0].Kind().Panels; !slices.Equal(p, []PanelID{dpid(2)}) {
		t.Fatalf("first group = %v", p)
	}
}

// U:dock/layout/edit.rs:459-492
func TestDockEditSplittingAcrossTheParentAxisNestsANewSplit(t *testing.T) {
	tree, tabs := treeWithOneGroup()
	tree.pushTabsForTest(tree.Root().ID(), []PanelID{dpid(9)})
	tree.Normalize()

	tree.Split(tabs, dpid(2), PlacementBottom, nil)

	k := tree.Root().Kind()
	if k.Axis != gpui.AxisHorizontal {
		t.Fatal("the root changed axis")
	}
	inner := k.Children[0].Kind()
	if !inner.IsSplit || inner.Axis != gpui.AxisVertical || len(inner.Children) != 2 {
		t.Fatalf("the split target is wrapped in a vertical split: %+v", inner)
	}
	// Bottom puts the new group after the original target.
	if p := inner.Children[0].Kind().Panels; !slices.Equal(p, []PanelID{dpid(1)}) {
		t.Fatalf("the original target stays first: %v", p)
	}
	if p := inner.Children[1].Kind().Panels; !slices.Equal(p, []PanelID{dpid(2)}) {
		t.Fatalf("the new group lands second, below: %v", p)
	}
}

// U:dock/layout/edit.rs:494-526
func TestDockEditSplittingTopAcrossTheParentAxisPutsTheNewGroupFirst(t *testing.T) {
	tree, tabs := treeWithOneGroup()
	tree.pushTabsForTest(tree.Root().ID(), []PanelID{dpid(9)})
	tree.Normalize()

	tree.Split(tabs, dpid(2), PlacementTop, nil)

	inner := tree.Root().Kind().Children[0].Kind()
	if !inner.IsSplit || inner.Axis != gpui.AxisVertical || len(inner.Children) != 2 {
		t.Fatalf("the split target is wrapped in a vertical split: %+v", inner)
	}
	if p := inner.Children[0].Kind().Panels; !slices.Equal(p, []PanelID{dpid(2)}) {
		t.Fatalf("the new group lands first, above: %v", p)
	}
	if p := inner.Children[1].Kind().Panels; !slices.Equal(p, []PanelID{dpid(1)}) {
		t.Fatalf("the original target moves second: %v", p)
	}
}

// U:dock/layout/edit.rs:528-537
func TestDockEditTheSplitTargetKeepsItsNodeIDSoItsEntitySurvives(t *testing.T) {
	tree, tabs := treeWithOneGroup()
	tree.Split(tabs, dpid(2), PlacementRight, nil)
	if _, ok := tree.FindNode(tabs); !ok {
		t.Fatal("the target group is reused, not rebuilt")
	}
}

// U:dock/layout/edit.rs:539-566
func TestDockEditMovingAPanelBetweenGroupsPreservesItsIdentity(t *testing.T) {
	tree, tabs := treeWithOneGroup()
	if !tree.Split(tabs, dpid(2), PlacementRight, nil).Changed() {
		t.Fatal("the split did not change the tree")
	}
	other, ok := tree.FindPanelNode(dpid(2))
	if !ok {
		t.Fatal("the split put panel 2 in a group of its own")
	}
	result := tree.MovePanel(dpid(1), InsertTargetTabs{Node: other, Activate: true})
	if !result.Changed() {
		t.Fatal("no change reported")
	}
	if got := tree.Panels(); !slices.Equal(got, []PanelID{dpid(2), dpid(1)}) {
		t.Fatalf("panels = %v: a move is not a removal", got)
	}
	if _, ok := tree.FindNode(tabs); ok {
		t.Fatal("the emptied group did not collapse")
	}
}

// U:dock/layout/edit.rs:568-573
func TestDockEditANoOpEditReportsNoChange(t *testing.T) {
	tree, tabs := treeWithOneGroup()
	if tree.SetActive(tabs, 0).Changed() {
		t.Fatal("a no-op edit reported a change")
	}
}

// U:dock/layout/edit.rs:575-588
func TestDockEditSetSizesReplacesAMatchingLengthVector(t *testing.T) {
	tree, tabs := treeWithOneGroup()
	tree.Split(tabs, dpid(2), PlacementRight, nil)
	root := tree.Root().ID()
	if !tree.SetSizes(root, optSizes(dpx(100), dpx(200))).Changed() {
		t.Fatal("no change reported")
	}
	requireSizes(t, tree.Root().Kind().Sizes, optSizes(dpx(100), dpx(200)), "after set_sizes")
}

// U:dock/layout/edit.rs:590-619
func TestDockEditSetSizesIgnoresAMismatchedLengthVector(t *testing.T) {
	tree, tabs := treeWithOneGroup()
	tree.Split(tabs, dpid(2), PlacementRight, nil)
	root := tree.Root().ID()
	before := slices.Clone(tree.Root().Kind().Sizes)

	// The split has 2 children; hand it 3 sizes.
	if tree.SetSizes(root, optSizes(dpx(10), dpx(20), dpx(30))).Changed() {
		t.Fatal("a mismatched vector must not report a change")
	}
	requireSizes(t, tree.Root().Kind().Sizes, before, "the mismatched vector is rejected")
}

// U:dock/layout/edit.rs:621-637
func TestDockEditEveryEditLeavesTheTreeNormalized(t *testing.T) {
	tree, tabs := treeWithOneGroup()
	tree.Split(tabs, dpid(2), PlacementBottom, nil)
	tree.InsertPanel(dpid(3), InsertTargetTabs{Node: tabs})
	tree.RemovePanel(dpid(2))
	tree.RemovePanel(dpid(3))
	if !tree.isNormalized() {
		t.Fatal("the tree is not normalized")
	}
}

// U:dock/layout/normalize.rs:273-288
func TestDockNormalizeEmptyTabGroupsAreDropped(t *testing.T) {
	tree := NewPaneTree(RootKindSplit)
	root := tree.Root().ID()
	tree.pushTabsForTest(root, nil)
	tree.pushTabsForTest(root, []PanelID{dpid(1)})
	tree.Normalize()
	if k := tree.Root().Kind(); !k.IsSplit || len(k.Children) != 1 {
		t.Fatal("the empty tab group was not dropped")
	}
	if got := tree.Panels(); !slices.Equal(got, []PanelID{dpid(1)}) {
		t.Fatalf("panels = %v", got)
	}
}

// U:dock/layout/normalize.rs:290-301
func TestDockNormalizeASingleChildSplitIsReplacedByItsChildKeepingTheChildID(t *testing.T) {
	tree := NewPaneTree(RootKindAny)
	outer := tree.setRootSplitForTest(gpui.AxisHorizontal)
	inner := tree.pushSplitForTest(outer, gpui.AxisVertical, dpx(120))
	tabs := tree.pushTabsForTest(inner, []PanelID{dpid(1)})
	tree.Normalize()
	if tree.Root().ID() != tabs {
		t.Fatal("the child did not keep its own NodeID")
	}
	if _, ok := tree.FindNode(inner); ok {
		t.Fatal("the inner split survived")
	}
}

// U:dock/layout/normalize.rs:303-321
func TestDockNormalizeACollapsingSplitHandsItsSlotSizeToTheChild(t *testing.T) {
	tree := NewPaneTree(RootKindSplit)
	root := tree.Root().ID()
	inner := tree.pushSplitForTest(root, gpui.AxisVertical, dpx(300))
	tree.pushTabsForTest(inner, []PanelID{dpid(1)})
	tree.pushTabsForTest(root, []PanelID{dpid(2)})
	tree.Normalize()
	if s := tree.Root().Kind().Sizes[0]; s == nil || *s != 300 {
		t.Fatalf("sizes[0] = %v, want the collapsed split's 300 slot", fmtSizes(tree.Root().Kind().Sizes))
	}
}

// U:dock/layout/normalize.rs:323-349
func TestDockNormalizeSameAxisNestingIsSplicedIntoTheParent(t *testing.T) {
	tree := NewPaneTree(RootKindSplit)
	tree.setRootAxisForTest(gpui.AxisHorizontal)
	root := tree.Root().ID()
	tree.pushTabsForTest(root, []PanelID{dpid(1)})
	inner := tree.pushSplitForTest(root, gpui.AxisHorizontal, nil)
	tree.pushTabsForTest(inner, []PanelID{dpid(2)})
	tree.pushTabsForTest(inner, []PanelID{dpid(3)})
	tree.Normalize()
	k := tree.Root().Kind()
	if k.Axis != gpui.AxisHorizontal || len(k.Children) != 3 {
		t.Fatalf("the inner split's children are not spliced in: %d children", len(k.Children))
	}
	if got := tree.Panels(); !slices.Equal(got, []PanelID{dpid(1), dpid(2), dpid(3)}) {
		t.Fatalf("order is not preserved: %v", got)
	}
}

// U:dock/layout/normalize.rs:351-362
func TestDockNormalizeActiveIndexIsClampedToThePanelCount(t *testing.T) {
	tree := NewPaneTree(RootKindAny)
	tabs := tree.setRootTabsForTest([]PanelID{dpid(1), dpid(2)}, 9)
	tree.Normalize()
	if ix := mustNode(t, &tree, tabs).Kind().ActiveIx; ix != 1 {
		t.Fatalf("active_ix = %d, want 1", ix)
	}
}

// U:dock/layout/normalize.rs:364-376
func TestDockNormalizeASplitRootSurvivesBeingEmptied(t *testing.T) {
	tree := NewPaneTree(RootKindSplit)
	tree.pushTabsForTest(tree.Root().ID(), nil)
	tree.Normalize()
	if k := tree.Root().Kind(); !k.IsSplit || len(k.Children) != 0 {
		t.Fatal("the center must still serialize as a StackPanel when empty")
	}
}

// U:dock/layout/normalize.rs:378-403
func TestDockNormalizeASameAxisSpliceWithOneUnknownInnerSizePassesThemThrough(t *testing.T) {
	tree := NewPaneTree(RootKindSplit)
	root := tree.Root().ID()
	inner := tree.pushSplitForTest(root, gpui.AxisHorizontal, dpx(400))
	tree.pushSizedTabsForTest(inner, []PanelID{dpid(1)}, dpx(100))
	tree.pushSizedTabsForTest(inner, []PanelID{dpid(2)}, nil)
	tree.Normalize()
	requireSizes(t, tree.Root().Kind().Sizes, optSizes(dpx(100), nil),
		"an unknown inner size leaves every sibling unscaled")
}

// U:dock/layout/normalize.rs:405-432
func TestDockNormalizeRemovingAMiddleContainerLeavesItsSiblingsUntouched(t *testing.T) {
	tree := NewPaneTree(RootKindSplit)
	root := tree.Root().ID()
	tree.pushSizedTabsForTest(root, []PanelID{dpid(1)}, dpx(400))
	tree.pushSizedTabsForTest(root, nil, dpx(800))
	tree.pushSizedTabsForTest(root, []PanelID{dpid(3)}, dpx(400))
	tree.Normalize()
	k := tree.Root().Kind()
	if len(k.Children) != 2 {
		t.Fatalf("children = %d", len(k.Children))
	}
	requireSizes(t, k.Sizes, optSizes(dpx(400), dpx(400)), "the survivors keep their own sizes")
}

// U:dock/layout/normalize.rs:434-448
func TestDockNormalizeIsIdempotent(t *testing.T) {
	tree := NewPaneTree(RootKindSplit)
	root := tree.Root().ID()
	inner := tree.pushSplitForTest(root, gpui.AxisHorizontal, nil)
	tree.pushTabsForTest(inner, []PanelID{dpid(1)})
	tree.pushTabsForTest(inner, nil)
	tree.pushTabsForTest(root, []PanelID{dpid(2)})
	tree.Normalize()
	once := tree.Clone()
	tree.Normalize()
	if !once.Equal(&tree) {
		t.Fatal("normalize(normalize(t)) != normalize(t)")
	}
}

// U:dock/layout/normalize.rs:450-478
func TestDockNormalizeSameAxisSpliceScalesInnerSizesToFillTheOuterSlot(t *testing.T) {
	tree := NewPaneTree(RootKindSplit)
	root := tree.Root().ID()
	inner := tree.pushSplitForTest(root, gpui.AxisHorizontal, dpx(400))
	tree.pushSizedTabsForTest(inner, []PanelID{dpid(1)}, dpx(50))
	tree.pushSizedTabsForTest(inner, []PanelID{dpid(2)}, dpx(150))
	tree.Normalize()
	sizes := tree.Root().Kind().Sizes
	requireSizes(t, sizes, optSizes(dpx(100), dpx(300)), "sizes scale by the outer/inner ratio (400/200 = 2x)")
	var total float32
	for _, s := range sizes {
		total += *s
	}
	if total != 400 {
		t.Fatalf("the scaled sizes sum to %v, want the outer slot 400", total)
	}
}

// U:dock/layout/normalize.rs:480-514
func TestDockNormalizeConvergesWithinTwoPassesOnAnAdversarialTree(t *testing.T) {
	tree := NewPaneTree(RootKindAny)
	root := tree.Root().ID()
	d := tree.pushSplitForTest(root, gpui.AxisVertical, nil)
	a := tree.pushSplitForTest(d, gpui.AxisHorizontal, nil)
	tree.pushTabsForTest(a, nil)
	b := tree.pushSplitForTest(a, gpui.AxisVertical, nil)
	c := tree.pushSplitForTest(b, gpui.AxisVertical, nil)
	tree.pushTabsForTest(c, []PanelID{dpid(1)})
	tree.pushTabsForTest(c, []PanelID{dpid(2)})

	if passes := tree.normalizePassCountForTest(); passes > 2 {
		t.Fatalf("expected the fixpoint within 2 passes, took %d", passes)
	}
	if !tree.isNormalized() {
		t.Fatal("the tree is not normalized")
	}
	if got := tree.Panels(); !slices.Equal(got, []PanelID{dpid(1), dpid(2)}) {
		t.Fatalf("every panel survives the collapse, in order: %v", got)
	}
}
