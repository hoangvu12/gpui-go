package base

// Port of U:dock/layout/tree.rs: one region's layout as pure data.

import (
	"sync/atomic"

	"github.com/hoangvu12/gpui-go/gpui"
)

// RootKind says whether the root of a tree is pinned to a split
// (U:dock/layout/tree.rs:7-15). The center of a DockArea must serialize as
// a StackPanel even when empty, which RootKindSplit guarantees; a dock's
// root is unconstrained.
type RootKind uint8

const (
	// RootKindSplit pins the root to a split.
	RootKindSplit RootKind = iota
	// RootKindAny leaves the root unconstrained.
	RootKindAny
)

// nodePath is the path from the root as a sequence of child indices
// (U:dock/layout/tree.rs:17-18; the SmallVec is a plain slice).
type nodePath []int

// nextNodeIDCounter is the source of every NodeID, shared by every tree in
// the process (U:dock/layout/tree.rs:20-32). A per-tree counter would
// restart at the same value in each tree, and a DockArea owns several of
// them while its entity cache is keyed by NodeID alone.
var nextNodeIDCounter atomic.Uint64

func nextNodeID() NodeID { return NodeID(nextNodeIDCounter.Add(1) - 1) }

// PaneTree is one region's layout, as pure data
// (U:dock/layout/tree.rs:34-49). Containers are addressed by NodeID and
// panels by PanelID; no entity handle lives in here. Every edit method
// normalizes before returning and reports what it did as an EditResult.
type PaneTree struct {
	root     PaneNode
	rootKind RootKind
}

// NewPaneTree is PaneTree::new (U:dock/layout/tree.rs:51-62): an empty
// horizontal split root.
func NewPaneTree(rootKind RootKind) PaneTree {
	return PaneTree{
		root:     newPaneNode(nextNodeID(), splitKind(gpui.AxisHorizontal, nil, nil)),
		rootKind: rootKind,
	}
}

// Root returns the root node (U:dock/layout/tree.rs:64-66).
func (t *PaneTree) Root() *PaneNode { return &t.root }

// RootKind returns the root kind (U:dock/layout/tree.rs:73-75).
func (t *PaneTree) RootKind() RootKind { return t.rootKind }

func (t *PaneTree) allocateNodeID() NodeID { return nextNodeID() }

// Clone deep-copies the tree (the derived Clone).
func (t *PaneTree) Clone() PaneTree { return PaneTree{root: t.root.clone(), rootKind: t.rootKind} }

// Equal is the derived PartialEq.
func (t *PaneTree) Equal(other *PaneTree) bool {
	return t.rootKind == other.rootKind && t.root.Equal(&other.root)
}

// NodeIDs returns every container id in the tree, in pre-order
// (U:dock/layout/tree.rs:81-86).
func (t *PaneTree) NodeIDs() []NodeID {
	var ids []NodeID
	t.root.Walk(func(n *PaneNode) { ids = append(ids, n.id) })
	return ids
}

// Panels returns every panel in the tree, in pre-order
// (U:dock/layout/tree.rs:88-96).
func (t *PaneTree) Panels() []PanelID {
	var found []PanelID
	t.root.Walk(func(n *PaneNode) {
		if !n.kind.split {
			found = append(found, n.kind.panels...)
		}
	})
	return found
}

// FindNode returns the node with id (U:dock/layout/tree.rs:98-100).
func (t *PaneTree) FindNode(id NodeID) (*PaneNode, bool) {
	path, ok := t.pathOfNode(id)
	if !ok {
		return nil, false
	}
	return t.nodeAt(path), true
}

// FindPanelNode returns the tab node holding panel
// (U:dock/layout/tree.rs:102-115). Like upstream it reports the last
// holder in pre-order.
func (t *PaneTree) FindPanelNode(panel PanelID) (NodeID, bool) {
	var found NodeID
	ok := false
	t.root.Walk(func(n *PaneNode) {
		if n.kind.split {
			return
		}
		for _, p := range n.kind.panels {
			if p == panel {
				found, ok = n.id, true
				return
			}
		}
	})
	return found, ok
}

// pathOfNode is U:dock/layout/tree.rs:117-136.
func (t *PaneTree) pathOfNode(id NodeID) (nodePath, bool) {
	var search func(node *PaneNode, path *nodePath) bool
	search = func(node *PaneNode, path *nodePath) bool {
		if node.id == id {
			return true
		}
		if node.kind.split {
			for ix := range node.kind.children {
				*path = append(*path, ix)
				if search(&node.kind.children[ix], path) {
					return true
				}
				*path = (*path)[:len(*path)-1]
			}
		}
		return false
	}
	path := nodePath{}
	if search(&t.root, &path) {
		return path, true
	}
	return nil, false
}

// nodeAt is U:dock/layout/tree.rs:138-158 (node_at and node_at_mut: Go
// pointers serve both).
func (t *PaneTree) nodeAt(path nodePath) *PaneNode {
	node := &t.root
	for _, ix := range path {
		if !node.kind.split {
			panic("path traverses a non-split node")
		}
		node = &node.kind.children[ix]
	}
	return node
}

// replaceRoot replaces the whole tree with node, keeping node's own id and
// the root kind (U:dock/layout/tree.rs:160-164).
func (t *PaneTree) replaceRoot(node PaneNode) { t.root = node }
