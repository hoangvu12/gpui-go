package base

// Port of U:dock/layout/normalize.rs: collapsing a tree to canonical shape.

import (
	"log/slog"
	"slices"
)

// maxNormalizePasses bounds Normalize (U:dock/layout/normalize.rs:6-11).
const maxNormalizePasses = 64

// Normalize collapses the tree to canonical shape
// (U:dock/layout/normalize.rs:13-35): one post-order pass repeated to a
// fixpoint. Rules, applied bottom up:
//
//  1. An empty Tabs or Split is removed from its parent.
//  2. A Split with one child is replaced by that child, which keeps its own
//     NodeID and inherits the split's slot size.
//  3. A Split whose child is a Split of the same axis splices that child's
//     children into itself.
//  4. activeIx is clamped.
//  5. The root is preserved according to RootKind.
//
// Idempotent: normalize(normalize(t)) == normalize(t).
func (t *PaneTree) Normalize() { t.normalizeReporting() }

// normalizeReporting is Normalize, reporting whether it changed anything
// (U:dock/layout/normalize.rs:37-47). The upstream debug_assert that the
// fixpoint was reached is the warning in runNormalizePasses.
func (t *PaneTree) normalizeReporting() bool {
	_, changed := t.runNormalizePasses()
	return changed
}

// runNormalizePasses runs passes until nothing changes or the bound is
// exhausted, returning the pass count and whether anything changed
// (U:dock/layout/normalize.rs:49-85).
func (t *PaneTree) runNormalizePasses() (int, bool) {
	passes := 0
	changed := true
	anyChange := false
	for changed && passes < maxNormalizePasses {
		changed = false
		normalizeNode(&t.root, &changed)
		collapseRoot(t, &changed)
		anyChange = anyChange || changed
		passes++
	}
	if changed {
		// The upstream tracing::warn!.
		slog.Warn("PaneTree.Normalize exhausted its passes without reaching a fixpoint; the tree may "+
			"still contain an empty container, a single-child split, same-axis split nesting, or an "+
			"unclamped Tabs active_ix", "passes", passes)
	}
	return passes, anyChange
}

// isNormalized reports whether the tree satisfies every structural
// invariant (U:dock/layout/normalize.rs:94-121).
func (t *PaneTree) isNormalized() bool {
	ok := true
	rootID := t.root.id
	t.root.Walk(func(node *PaneNode) {
		if node.kind.split {
			ok = ok && len(node.kind.children) == len(node.kind.sizes)
			// The root may legitimately be an empty or single-child split.
			if node.id != rootID {
				ok = ok && len(node.kind.children) > 1
			}
			for i := range node.kind.children {
				child := &node.kind.children[i]
				if child.kind.split && child.kind.axis == node.kind.axis {
					ok = false
				}
			}
			return
		}
		ok = ok && (len(node.kind.panels) == 0 || node.kind.activeIx < len(node.kind.panels))
		if node.id != rootID {
			ok = ok && len(node.kind.panels) > 0
		}
	})
	return ok
}

// normalizeNode is one post-order pass (U:dock/layout/normalize.rs:124-215).
func normalizeNode(node *PaneNode, changed *bool) {
	k := &node.kind
	if !k.split {
		clamped := min(k.activeIx, max(len(k.panels)-1, 0))
		if k.activeIx != clamped {
			k.activeIx = clamped
			*changed = true
		}
		return
	}
	axis := k.axis
	for i := range k.children {
		normalizeNode(&k.children[i], changed)
	}

	// Rule 1: drop empty children.
	for ix := 0; ix < len(k.children); {
		if isEmptyContainer(&k.children[ix]) {
			k.children = slices.Delete(k.children, ix, ix+1)
			k.sizes = slices.Delete(k.sizes, ix, ix+1)
			*changed = true
		} else {
			ix++
		}
	}

	// Rule 2: a single-child split child is replaced by its child, which
	// inherits the slot the split occupied.
	for ix := range k.children {
		child := &k.children[ix]
		if child.kind.split && len(child.kind.children) == 1 {
			k.children[ix] = child.kind.children[0]
			*changed = true
		}
	}

	// Rule 3: splice same-axis nesting.
	for ix := 0; ix < len(k.children); {
		child := &k.children[ix]
		if !child.kind.split || child.kind.axis != axis {
			ix++
			continue
		}
		inner := child.kind.children
		innerSizes := distributeSlot(k.sizes[ix], child.kind.sizes)
		count := len(inner)
		k.children = slices.Replace(k.children, ix, ix+1, inner...)
		k.sizes = slices.Replace(k.sizes, ix, ix+1, innerSizes...)
		ix += count
		*changed = true
	}
}

// distributeSlot spreads an outer slot size across the inner sizes that
// replace it (U:dock/layout/normalize.rs:217-236): unchanged when the slot
// is unconstrained or any inner size is unknown; otherwise scaled to fill
// the slot.
func distributeSlot(slot *float32, inner []*float32) []*float32 {
	if slot == nil {
		return inner
	}
	var total float32
	for _, size := range inner {
		if size == nil {
			return inner
		}
		total += *size
	}
	if total <= 0 {
		return inner
	}
	out := make([]*float32, len(inner))
	for i, size := range inner {
		out[i] = someF32(*size * (*slot / total))
	}
	return out
}

// isEmptyContainer is U:dock/layout/normalize.rs:238-243.
func isEmptyContainer(node *PaneNode) bool {
	if node.kind.split {
		return len(node.kind.children) == 0
	}
	return len(node.kind.panels) == 0
}

// collapseRoot is rule 5 (U:dock/layout/normalize.rs:245-262): a
// RootKindSplit tree keeps a split root no matter what; a RootKindAny tree
// lets a single-child root collapse like any other node.
func collapseRoot(t *PaneTree, changed *bool) {
	if t.rootKind == RootKindSplit {
		return
	}
	if t.root.kind.split && len(t.root.kind.children) == 1 {
		t.replaceRoot(t.root.kind.children[0].clone())
		*changed = true
	}
}
