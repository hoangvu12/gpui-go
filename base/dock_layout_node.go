package base

// Port of U:dock/layout/node.rs: container and panel identities and the
// node shape of a PaneTree.
//
// Go adaptations: Option<Pixels> slot sizes are *float32 (the resizable
// port's spelling, U:resizable/mod.rs AdoptSizes); a slot pointer is
// never written through, only replaced, so cloning a tree may share them.
// The private NodeKind enum is a struct with a split flag.

import (
	"github.com/hoangvu12/gpui-go/gpui"
)

// NodeID is a stable container identity (U:dock/layout/node.rs:3-16). It
// survives structural edits and normalization so the DockArea view cache
// does not tear down and rebuild entities.
type NodeID uint64

// NodeIDFromU64 is NodeId::from_u64 (pub(crate) upstream; the Go tests
// and the tab group tests build ids directly).
func NodeIDFromU64(raw uint64) NodeID { return NodeID(raw) }

// AsU64 returns the raw id (U:dock/layout/node.rs:13-15).
func (id NodeID) AsU64() uint64 { return uint64(id) }

// PanelID is a stable panel identity (U:dock/layout/node.rs:18-37). It
// wraps the EntityID of the panel entity so the layout algebra can be
// exercised without an App.
type PanelID uint64

// PanelIDFromU64 is PanelId::from_u64 (U:dock/layout/node.rs:24-26).
func PanelIDFromU64(raw uint64) PanelID { return PanelID(raw) }

// PanelIDOf is From<EntityId> for PanelId (U:dock/layout/node.rs:33-37).
func PanelIDOf(id gpui.EntityID) PanelID { return PanelID(uint64(id)) }

// AsU64 returns the raw id (U:dock/layout/node.rs:28-30).
func (id PanelID) AsU64() uint64 { return uint64(id) }

// nodeKind is the shape of one container (U:dock/layout/node.rs:39-54
// NodeKind). Private: every mutation goes through PaneTree so
// normalization always runs. split selects the variant: a Split carries
// axis, children and sizes; a Tabs carries panels and activeIx.
type nodeKind struct {
	split bool

	axis     gpui.Axis
	children []PaneNode
	sizes    []*float32

	panels   []PanelID
	activeIx int
}

func splitKind(axis gpui.Axis, children []PaneNode, sizes []*float32) nodeKind {
	return nodeKind{split: true, axis: axis, children: children, sizes: sizes}
}

func tabsKind(panels []PanelID, activeIx int) nodeKind {
	return nodeKind{panels: panels, activeIx: activeIx}
}

// PaneRef is the borrowed read-only projection of a node
// (U:dock/layout/node.rs:56-67). IsSplit selects the variant: a split
// reads Axis, Children and Sizes; a tab group reads Panels and ActiveIx.
// The slices alias the tree and must not be modified.
type PaneRef struct {
	IsSplit bool

	Axis     gpui.Axis
	Children []PaneNode
	Sizes    []*float32

	Panels   []PanelID
	ActiveIx int
}

// PaneNode is one container (U:dock/layout/node.rs:69-73).
type PaneNode struct {
	id   NodeID
	kind nodeKind
}

func newPaneNode(id NodeID, kind nodeKind) PaneNode { return PaneNode{id: id, kind: kind} }

// ID returns the node id (U:dock/layout/node.rs:80-82).
func (n *PaneNode) ID() NodeID { return n.id }

// Kind returns the read-only projection (U:dock/layout/node.rs:84-100).
func (n *PaneNode) Kind() PaneRef {
	if n.kind.split {
		return PaneRef{IsSplit: true, Axis: n.kind.axis, Children: n.kind.children, Sizes: n.kind.sizes}
	}
	return PaneRef{Panels: n.kind.panels, ActiveIx: n.kind.activeIx}
}

// Walk is a depth-first pre-order walk over this node and its descendants
// (U:dock/layout/node.rs:110-118).
func (n *PaneNode) Walk(f func(*PaneNode)) {
	f(n)
	if n.kind.split {
		for i := range n.kind.children {
			n.kind.children[i].Walk(f)
		}
	}
}

// clone deep-copies the node (the derived Clone).
func (n *PaneNode) clone() PaneNode {
	out := PaneNode{id: n.id, kind: nodeKind{split: n.kind.split, axis: n.kind.axis, activeIx: n.kind.activeIx}}
	if n.kind.split {
		out.kind.children = make([]PaneNode, len(n.kind.children))
		for i := range n.kind.children {
			out.kind.children[i] = n.kind.children[i].clone()
		}
		out.kind.sizes = append([]*float32(nil), n.kind.sizes...)
	} else {
		out.kind.panels = append([]PanelID(nil), n.kind.panels...)
	}
	return out
}

// Equal is the derived PartialEq: ids, shapes and slot sizes by value.
func (n *PaneNode) Equal(other *PaneNode) bool {
	if n.id != other.id || n.kind.split != other.kind.split {
		return false
	}
	if !n.kind.split {
		if n.kind.activeIx != other.kind.activeIx || len(n.kind.panels) != len(other.kind.panels) {
			return false
		}
		for i, p := range n.kind.panels {
			if other.kind.panels[i] != p {
				return false
			}
		}
		return true
	}
	if n.kind.axis != other.kind.axis || len(n.kind.children) != len(other.kind.children) ||
		!optPixelsSliceEqual(n.kind.sizes, other.kind.sizes) {
		return false
	}
	for i := range n.kind.children {
		if !n.kind.children[i].Equal(&other.kind.children[i]) {
			return false
		}
	}
	return true
}

// optPixelsSliceEqual compares Vec<Option<Pixels>> by value.
func optPixelsSliceEqual(a, b []*float32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !optF32Equal(a[i], b[i]) {
			return false
		}
	}
	return true
}
