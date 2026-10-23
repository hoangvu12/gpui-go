package base

// Port of U:dock/state_convert.rs: converting between a PaneTree and the
// persisted PanelState.

import (
	"github.com/hoangvu12/gpui-go/gpui"
)

// The names written to persisted layouts (U:dock/state_convert.rs:6-9).
// These are contract, not type names.
const (
	stackPanelName = "StackPanel"
	tabPanelName   = "TabPanel"
)

// PanelSource is how the layout tree reads properties of panels it only
// knows by id (U:dock/state_convert.rs:11-19).
type PanelSource interface {
	PanelName(id PanelID) string
	IsVisible(id PanelID) bool
	Dump(id PanelID) PanelState
}

// ToState writes the tree out (U:dock/state_convert.rs:21-25).
func (t *PaneTree) ToState(source PanelSource) PanelState {
	return nodeToState(&t.root, source)
}

// nodeToState is U:dock/state_convert.rs:27-63. An unconstrained (nil)
// slot is written as the 0.0 sentinel, which the reader maps back to nil.
func nodeToState(node *PaneNode, source PanelSource) PanelState {
	if node.kind.split {
		children := make([]PanelState, len(node.kind.children))
		for i := range node.kind.children {
			children[i] = nodeToState(&node.kind.children[i], source)
		}
		sizes := make([]float32, len(node.kind.sizes))
		for i, size := range node.kind.sizes {
			if size != nil {
				sizes[i] = *size
			}
		}
		return PanelState{PanelName: stackPanelName, Children: children, Info: PanelInfoStack(sizes, node.kind.axis)}
	}
	children := make([]PanelState, len(node.kind.panels))
	for i, panel := range node.kind.panels {
		children[i] = source.Dump(panel)
	}
	// Unconditional, unlike the old writer which left an empty group
	// looking like a bare panel.
	return PanelState{PanelName: tabPanelName, Children: children, Info: PanelInfoTabs(node.kind.activeIx)}
}

// PanelBuilder turns a persisted leaf into a live panel id
// (U:dock/state_convert.rs:65-74).
type PanelBuilder interface {
	Build(state *PanelState, info *PanelInfo) PanelID
}

// PaneTreeFromState reads a persisted layout (U:dock/state_convert.rs:76-119).
//
// Compatibility rules: a Tabs whose children are themselves Tabs is
// flattened; a bare Panel leaf where a container belongs is wrapped in a
// Tabs; a node named TabPanel carrying PanelInfo::Panel is read as an empty
// tab group (data written by the old empty-group dump defect).
func PaneTreeFromState(state *PanelState, rootKind RootKind, builder PanelBuilder) PaneTree {
	tree := NewPaneTree(rootKind)
	root := buildStateNode(&tree, state, builder)
	if rootKind == RootKindSplit && !root.kind.split {
		root = newPaneNode(tree.allocateNodeID(), splitKind(gpui.AxisHorizontal, []PaneNode{root}, []*float32{nil}))
	}
	tree.replaceRoot(root)
	tree.Normalize()
	return tree
}

// buildStateNode is U:dock/state_convert.rs:121-175.
func buildStateNode(tree *PaneTree, state *PanelState, builder PanelBuilder) PaneNode {
	id := tree.allocateNodeID()
	switch {
	case state.Info.IsStack():
		axis, _ := state.Info.Axis()
		stored, _ := state.Info.Sizes()
		children := make([]PaneNode, len(state.Children))
		for i := range state.Children {
			children[i] = buildStateNode(tree, &state.Children[i], builder)
		}
		sizes := make([]*float32, len(children))
		for ix := range children {
			if ix < len(stored) && stored[ix] > 0 {
				sizes[ix] = someF32(stored[ix])
			}
		}
		return newPaneNode(id, splitKind(axis, children, sizes))
	case state.Info.IsTabs():
		activeIndex, _ := state.Info.ActiveIndex()
		return newPaneNode(id, tabsKind(collectTabPanels(state.Children, builder), activeIndex))
	}
	// A container name carrying a leaf info means the writer that produced
	// this file had the empty-group defect.
	var panels []PanelID
	if state.PanelName != tabPanelName {
		panels = []PanelID{builder.Build(state, &state.Info)}
	}
	return newPaneNode(id, tabsKind(panels, 0))
}

// collectTabPanels flattens one level of tab nesting, which the old writer
// could produce (U:dock/state_convert.rs:177-187).
func collectTabPanels(children []PanelState, builder PanelBuilder) []PanelID {
	var out []PanelID
	for i := range children {
		child := &children[i]
		switch {
		case child.Info.IsTabs():
			out = append(out, collectTabPanels(child.Children, builder)...)
		case child.Info.IsPanel() && child.PanelName == tabPanelName:
		default:
			out = append(out, builder.Build(child, &child.Info))
		}
	}
	return out
}
