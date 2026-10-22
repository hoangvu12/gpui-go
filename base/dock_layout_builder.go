package base

// Port of U:dock/layout/builder.rs: describing a layout without building it.

import (
	"log/slog"

	"github.com/hoangvu12/gpui-go/gpui"
)

// DockLayout describes a layout without constructing any entity
// (U:dock/layout/builder.rs:11-37). DockArea reconciles it into entities
// when it is installed.
//
// Misuse — a panel added to a split, a child container added to a tab
// group, an active index on anything but a tab group — is ignored (the
// upstream debug_assert! is a logged warning here).
type DockLayout struct {
	split bool

	axis     gpui.Axis
	children []dockLayoutChild

	panels   []dockLayoutPanel
	activeIx int
}

type dockLayoutChild struct {
	layout *DockLayout
	size   *float32
}

type dockLayoutPanel struct {
	id   PanelID
	view PanelView
}

// DockLayoutHSplit is a split whose children sit side by side
// (U:dock/layout/builder.rs:40-43).
func DockLayoutHSplit() *DockLayout { return dockLayoutSplit(gpui.AxisHorizontal) }

// DockLayoutVSplit is a split whose children stack
// (U:dock/layout/builder.rs:45-48).
func DockLayoutVSplit() *DockLayout { return dockLayoutSplit(gpui.AxisVertical) }

// DockLayoutTabs is a tab group: panels stacked one at a time, selected
// from a tab bar (U:dock/layout/builder.rs:50-58).
func DockLayoutTabs() *DockLayout { return &DockLayout{} }

func dockLayoutSplit(axis gpui.Axis) *DockLayout { return &DockLayout{split: true, axis: axis} }

func dockLayoutMisuse(method, valid string) {
	slog.Warn("DockLayout misuse: " + method + " is only valid on " + valid)
}

// Child adds a child container to a split (U:dock/layout/builder.rs:69-80).
// size is the child's slot along the split's axis; nil leaves it
// unconstrained.
func (l *DockLayout) Child(child *DockLayout, size *float32) *DockLayout {
	if !l.split {
		dockLayoutMisuse("Child", "DockLayoutHSplit or DockLayoutVSplit")
		return l
	}
	l.children = append(l.children, dockLayoutChild{layout: child, size: size})
	return l
}

// Panel adds a panel entity to a tab group (U:dock/layout/builder.rs:82-92).
func (l *DockLayout) Panel[S any, PS interface {
	*S
	Panel[S]
}](panel gpui.Entity[S]) *DockLayout {
	if l.split {
		dockLayoutMisuse("Panel", "DockLayoutTabs")
		return l
	}
	l.panels = append(l.panels, dockLayoutPanel{id: PanelIDOf(panel.EntityID()), view: PanelViewOf[S, PS](panel)})
	return l
}

// PanelView adds an already-wrapped panel handle to a tab group
// (U:dock/layout/builder.rs:94-110). The id comes from PanelView.PanelID.
func (l *DockLayout) PanelView(panel PanelView, app *gpui.App) *DockLayout {
	if l.split {
		dockLayoutMisuse("PanelView", "DockLayoutTabs")
		return l
	}
	l.panels = append(l.panels, dockLayoutPanel{id: panel.PanelID(app), view: panel})
	return l
}

// ActiveIndex says which tab is displayed (U:dock/layout/builder.rs:112-123).
// Out-of-range values are clamped by normalization once installed.
func (l *DockLayout) ActiveIndex(ix int) *DockLayout {
	if l.split {
		dockLayoutMisuse("ActiveIndex", "DockLayoutTabs")
		return l
	}
	l.activeIx = ix
	return l
}

// build lowers the layout into a node plus the panel views the area must
// register, paired with the ids the tree was built from
// (U:dock/layout/builder.rs:125-176).
func (l *DockLayout) build(tree *PaneTree) (PaneNode, []dockLayoutPanel) {
	var panels []dockLayoutPanel
	node := l.buildNode(tree, &panels)
	return node, panels
}

func (l *DockLayout) buildNode(tree *PaneTree, collected *[]dockLayoutPanel) PaneNode {
	id := tree.allocateNodeID()
	if l.split {
		nodes := make([]PaneNode, 0, len(l.children))
		sizes := make([]*float32, 0, len(l.children))
		for _, child := range l.children {
			nodes = append(nodes, child.layout.buildNode(tree, collected))
			sizes = append(sizes, child.size)
		}
		return newPaneNode(id, splitKind(l.axis, nodes, sizes))
	}
	ids := make([]PanelID, len(l.panels))
	for i, p := range l.panels {
		ids[i] = p.id
	}
	*collected = append(*collected, l.panels...)
	return newPaneNode(id, tabsKind(ids, l.activeIx))
}

// paneTreeFromLayout builds a whole tree from a described layout
// (U:dock/layout/builder.rs:178-211). A RootKindSplit tree whose described
// root is a tab group is wrapped in a split.
func paneTreeFromLayout(layout *DockLayout, rootKind RootKind) (PaneTree, []dockLayoutPanel) {
	tree := NewPaneTree(rootKind)
	root, panels := layout.build(&tree)
	if rootKind == RootKindSplit && !root.kind.split {
		root = newPaneNode(tree.allocateNodeID(), splitKind(gpui.AxisHorizontal, []PaneNode{root}, []*float32{nil}))
	}
	tree.replaceRoot(root)
	tree.Normalize()
	return tree, panels
}
