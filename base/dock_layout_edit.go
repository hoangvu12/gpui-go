package base

// Port of U:dock/layout/edit.rs: the tree edits.

import (
	"slices"
)

// InsertTarget says where a panel should land (U:dock/layout/edit.rs:10-25).
// It is InsertTargetTabs or InsertTargetSplit.
type InsertTarget interface {
	// targetNode is the node the target names (U:dock/dock_area.rs:1657-1661
	// target_node).
	targetNode() NodeID
}

// InsertTargetTabs lands the panel in an existing tab group, at Ix when
// HasIx (else appended), becoming the displayed tab when Activate
// (U:dock/layout/edit.rs:13-18).
type InsertTargetTabs struct {
	Node     NodeID
	Ix       int
	HasIx    bool
	Activate bool
}

// InsertTargetSplit lands the panel beside an existing node, in a new tab
// group (U:dock/layout/edit.rs:19-24). A nil Size leaves the slot to be
// decided by the split.
type InsertTargetSplit struct {
	Node      NodeID
	Placement Placement
	Size      *float32
}

func (t InsertTargetTabs) targetNode() NodeID  { return t.Node }
func (t InsertTargetSplit) targetNode() NodeID { return t.Node }

// EditResult reports what one edit changed (U:dock/layout/edit.rs:27-43):
// only whether anything changed.
type EditResult struct {
	changed bool
}

// Changed reports whether the edit changed the tree.
func (r EditResult) Changed() bool { return r.changed }

// InsertPanel inserts panel at target (U:dock/layout/edit.rs:46-48).
func (t *PaneTree) InsertPanel(panel PanelID, target InsertTarget) EditResult {
	return t.edit(func() bool { return t.applyInsert(panel, target) })
}

// RemovePanel removes panel wherever it lives (U:dock/layout/edit.rs:50-52).
func (t *PaneTree) RemovePanel(panel PanelID) EditResult {
	return t.edit(func() bool { return t.detachPanel(panel) })
}

// MovePanel moves a panel to a new home without ever removing it from the
// tree's perspective, so the caller never fires OnRemoved for a drag
// (U:dock/layout/edit.rs:54-62).
func (t *PaneTree) MovePanel(panel PanelID, target InsertTarget) EditResult {
	return t.edit(func() bool {
		detached := t.detachPanel(panel)
		inserted := t.applyInsert(panel, target)
		return detached || inserted
	})
}

// Split puts panel in a new tab group beside at (U:dock/layout/edit.rs:64-79).
func (t *PaneTree) Split(at NodeID, panel PanelID, placement Placement, size *float32) EditResult {
	return t.InsertPanel(panel, InsertTargetSplit{Node: at, Placement: placement, Size: size})
}

// SetActive displays tab ix of the tab group node
// (U:dock/layout/edit.rs:81-95).
func (t *PaneTree) SetActive(node NodeID, ix int) EditResult {
	return t.edit(func() bool {
		path, ok := t.pathOfNode(node)
		if !ok {
			return false
		}
		n := t.nodeAt(path)
		if n.kind.split || n.kind.activeIx == ix {
			return false
		}
		n.kind.activeIx = ix
		return true
	})
}

// SetSizes replaces a split's slot sizes wholesale
// (U:dock/layout/edit.rs:97-121). A length mismatch is a no-op.
func (t *PaneTree) SetSizes(node NodeID, newSizes []*float32) EditResult {
	return t.edit(func() bool {
		path, ok := t.pathOfNode(node)
		if !ok {
			return false
		}
		n := t.nodeAt(path)
		if !n.kind.split {
			return false
		}
		if len(newSizes) != len(n.kind.children) || optPixelsSliceEqual(n.kind.sizes, newSizes) {
			return false
		}
		n.kind.sizes = slices.Clone(newSizes)
		return true
	})
}

// adoptMeasuredSizes replaces every split's slot sizes with what that
// split is actually drawn at (U:dock/layout/edit.rs:124-164). A measured
// zero means the split has not been laid out yet, and keeps the slot.
func (t *PaneTree) adoptMeasuredSizes(measured map[NodeID][]float32) {
	var walk func(node *PaneNode)
	walk = func(node *PaneNode) {
		if !node.kind.split {
			return
		}
		if actual, ok := measured[node.id]; ok && len(actual) == len(node.kind.sizes) {
			for i, size := range actual {
				if size > 0 {
					node.kind.sizes[i] = someF32(size)
				}
			}
		}
		for i := range node.kind.children {
			walk(&node.kind.children[i])
		}
	}
	walk(&t.root)
}

// edit applies one mutation, then collapses (U:dock/layout/edit.rs:166-180).
func (t *PaneTree) edit(apply func() bool) EditResult {
	mutated := apply()
	collapsed := t.normalizeReporting()
	return EditResult{changed: mutated || collapsed}
}

// ContainsPanel reports whether panel is anywhere in this tree
// (U:dock/layout/edit.rs:182-185).
func (t *PaneTree) ContainsPanel(panel PanelID) bool {
	return slices.Contains(t.Panels(), panel)
}

// applyInsert reports whether the panel actually landed
// (U:dock/layout/edit.rs:188-218). An unresolvable target is a no-op.
func (t *PaneTree) applyInsert(panel PanelID, target InsertTarget) bool {
	switch target := target.(type) {
	case InsertTargetTabs:
		path, ok := t.pathOfNode(target.Node)
		if !ok {
			return false
		}
		n := t.nodeAt(path)
		if n.kind.split {
			return false
		}
		ix := len(n.kind.panels)
		if target.HasIx {
			ix = min(target.Ix, len(n.kind.panels))
		}
		n.kind.panels = slices.Insert(n.kind.panels, ix, panel)
		if target.Activate {
			n.kind.activeIx = ix
		} else if ix <= n.kind.activeIx && len(n.kind.panels) > 1 {
			// Keep the displayed panel displayed.
			n.kind.activeIx++
		}
		return true
	case InsertTargetSplit:
		return t.insertBeside(target.Node, panel, target.Placement, target.Size)
	}
	return false
}

// insertBeside places panel in a new tab group beside node
// (U:dock/layout/edit.rs:220-314).
func (t *PaneTree) insertBeside(node NodeID, panel PanelID, placement Placement, size *float32) bool {
	path, ok := t.pathOfNode(node)
	if !ok {
		return false
	}
	group := newPaneNode(t.allocateNodeID(), tabsKind([]PanelID{panel}, 0))
	before := placement == PlacementLeft || placement == PlacementTop

	if len(path) > 0 {
		parentPath, ix := path[:len(path)-1], path[len(path)-1]
		parent := t.nodeAt(parentPath)
		if parent.kind.split && parent.kind.axis == placement.Axis() {
			// The new group splits the slot it landed beside: a named size
			// is taken as is, and an unknown neighbour halves to another
			// unknown.
			share := size
			if size == nil {
				var half *float32
				if slot := parent.kind.sizes[ix]; slot != nil {
					half = someF32(*slot / 2)
				}
				parent.kind.sizes[ix] = half
				share = half
			}
			at := ix + 1
			if before {
				at = ix
			}
			parent.kind.children = slices.Insert(parent.kind.children, at, group)
			parent.kind.sizes = slices.Insert(parent.kind.sizes, at, share)
			return true
		}
	}

	// Wrap the target in a new split of the placement's axis; the target
	// keeps its own id.
	wrapperID := t.allocateNodeID()
	target := t.nodeAt(path)
	moved := *target
	var children []PaneNode
	var sizes []*float32
	if before {
		children, sizes = []PaneNode{group, moved}, []*float32{size, nil}
	} else {
		children, sizes = []PaneNode{moved, group}, []*float32{nil, size}
	}
	*target = newPaneNode(wrapperID, splitKind(placement.Axis(), children, sizes))
	return true
}

// detachPanel removes panel wherever it lives and reports whether it was
// found (U:dock/layout/edit.rs:316-338).
func (t *PaneTree) detachPanel(panel PanelID) bool {
	node, ok := t.FindPanelNode(panel)
	if !ok {
		return false
	}
	path, ok := t.pathOfNode(node)
	if !ok {
		return false
	}
	n := t.nodeAt(path)
	if n.kind.split {
		return false
	}
	ix := slices.Index(n.kind.panels, panel)
	if ix < 0 {
		return false
	}
	n.kind.panels = slices.Delete(n.kind.panels, ix, ix+1)
	if ix < n.kind.activeIx {
		n.kind.activeIx--
	}
	return true
}
