package gpui

// The containing-block rule for absolutely positioned elements
// (browser-core ticket 33, spec decision D3).
//
// CSS: an absolutely positioned box's containing block is the nearest
// ancestor whose position is anything but static (css-position-3 §2.1
// and §3.1, def-cb / abs-cb). Chromium decides containership exactly
// that way: LayoutObject::ComputeIsAbsoluteContainer
// (third_party/blink/renderer/core/layout/layout_object.cc:2072-2082 in
// the research checkout) returns true when the style's position is not
// kStatic; the bit is set from the style
// (layout_box_model_object.cc:503-504), read by
// LayoutObject::CanContainAbsolutePositionObjects
// (layout_object.h:2356-2360), and the containing block of an absolute
// is the first ancestor with the bit
// (LayoutObject::ContainerForAbsolutePosition, layout_object.cc:1877
// -1883, through FindAncestorByPredicate's parent-chain walk at
// :192-207). When no ancestor qualifies, the LayoutView contains it
// (ComputeIsFixedContainer, layout_object.cc:2035-2041: the LayoutView
// is always a fixed — and therefore absolute — container): the initial
// containing block. The inset basis is the containing block's PADDING
// box ("|inset| is relative to the container's padding-box",
// out_of_flow_layout_part.cc:2574-2576). Taffy 0.13's absolute pass
// agrees on the basis (compute/flexbox.rs:2231-2339,
// inset_relative_size = container size minus border) but has no static
// value, so it places every absolute child against its DIRECT parent —
// which is also this port's pinned behaviour (the research README's
// L2: "reparent in Go").
//
// This file implements that rule for the opt-in (D3's first step,
// browser-core 33) and for the DEFAULT (D3's second step,
// browser-core 76): the default position is Static, so every plain
// div is a static parent — exactly the browser's rule — and
// positioned parents state .Relative() (or another positioned value)
// explicitly; cmd/positioncodemod migrated the sites that relied on
// the pin's Relative default and guards the tree.
//
// Mechanism: the native ABI attaches children when the parent node is
// created (CreateNode; there is no reparent call), so the attachment
// decision is made at the parent's node request. A positioned element
// (relative or absolute — everything but static) OPENS a
// containing-block scope around its child-request walk; a static
// parent's node request DEFERS its absolute children into the
// innermost open scope instead of attaching them to itself; the scope
// owner's node request ATTACHES the deferred children to itself, so
// they become Taffy children of the containing block and Taffy's
// absolute pass places them against its padding box, as CSS says. A
// deferred child with no open scope (no participating positioned
// ancestor) falls back to attaching to its direct parent — the pin's
// behaviour — which keeps non-participating containers (a
// non-div root that walks children outside RequestContainerLayout,
// custom elements on the plain RequestElementLayout seam) at today's
// semantics for absolute children under a static wrapper.
//
// The root's ICB role (browser-core 76): the window ROOT element
// always opens a scope — even when its own style is static —
// because the root node stands in for Chromium's LayoutView, which is
// always a fixed — and therefore absolute — container
// (ComputeIsFixedContainer's IsA<LayoutView> arm,
// layout_object.cc:2035-2041, and ComputeIsAbsoluteContainer's
// is_fixed_container || position != kStatic, layout_object.cc:
// 2072-2082): the initial containing block exists no matter what the
// html element's own position says. A static chain with no positioned
// ancestor therefore defers into the root's scope and resolves
// against the root node's padding box (the root fills the window; its
// own padding shifts the basis — the ticket-33 documented
// approximation). The flag that selects the root's request is set by
// drawRoots around the root walk and consumed by the first scope
// open (or discarded by the plain requestLayoutOf seam, the
// non-participating-root approximation).
//
// Standalone roots (browser-core 76): a subtree laid out with
// LayoutAsRoot — List/UniformList/virtual-list items, the
// measured trees, the cached-view refresh, the deferred surfaces —
// IS its own native root, so its request walk runs with the enclosing
// scopes detached (withIsolatedContainingBlocks in
// drawable.layoutAsRoot): an absolute child of a row stays inside the
// row (the pin's direct-parent rule — "elements whose absolute
// children must stay inside them keep behaving the same"), while the
// CSS rule keeps holding WITHIN the row's own subtree. The detach
// also stops a measured tree's deferred children from leaking into
// the live tree's scopes (a cross-tree attach the pin never produced).
//
// The scope stack lives on the frame under construction and is
// balanced per element (open before the child walk, close after the
// node request), so a panicking build leaves no residue: the frame
// state dies with the abandoned frame.

// containingBlockScope is one open containing-block scope: the
// absolute children deferred by static descendants of the scope owner,
// waiting to attach to the owner's layout node when it is requested.
type containingBlockScope struct {
	// deferred holds the deferred absolute children in walk order.
	deferred []LayoutID
}

// openContainingBlock pushes one containing-block scope for an element
// whose position makes it a containing block for absolute descendants
// (relative, absolute, sticky and fixed — everything but static, which
// is Chromium's rule; a fixed element IS an absolute container too:
// ComputeIsAbsoluteContainer runs is_fixed_container || position !=
// kStatic, layout_object.cc:2072-2082). A static element returns nil:
// it opens no scope, and its absolute children defer to the enclosing
// one — EXCEPT the window root: when the frame's icbPending flag is
// set (drawRoots marked this request walk as the root's), the root
// opens its scope even when static, because the root node stands in
// for the LayoutView, which is always an absolute container
// (ComputeIsFixedContainer's IsA<LayoutView> arm, layout_object.cc:
// 2035-2041). The flag is consumed here (the root's own scope open is
// the first open of the walk) and by requestLayoutOf (the
// non-participating-root discard).
func openContainingBlock(w *Window, style *Style) *containingBlockScope {
	f := currentFrame(w)
	rootICB := f.icbPending
	if rootICB {
		f.icbPending = false
	}
	if style.Position == PositionStatic && !rootICB {
		return nil
	}
	scope := &containingBlockScope{}
	f.containingBlockScopes = append(f.containingBlockScopes, scope)
	return scope
}

// closeContainingBlock pops one scope opened by openContainingBlock
// (the nil form of a static element closes nothing). It panics on
// out-of-order closes because the scope stack mirrors the element walk
// nesting: a disorder would mis-attribute deferred children.
func closeContainingBlock(w *Window, scope *containingBlockScope) {
	if scope == nil {
		return
	}
	f := currentFrame(w)
	n := len(f.containingBlockScopes)
	if n == 0 || f.containingBlockScopes[n-1] != scope {
		panic("gpui: containing-block scope closed out of order")
	}
	f.containingBlockScopes[n-1] = nil
	f.containingBlockScopes = f.containingBlockScopes[:n-1]
}

// withIsolatedContainingBlocks runs f with the frame's containing-
// block scopes detached: the enclosing walk's scopes (and the pending
// root ICB flag) are stashed, f runs with an empty scope stack, and
// both are restored after. Standalone roots use this (browser-core
// 76; see the file header): a subtree laid out with LayoutAsRoot is
// its own native root, so its static elements keep the pin's
// direct-parent rule at the root boundary instead of deferring into
// an enclosing (unrelated) scope, and its own positioned elements
// still open scopes for their descendants — the CSS rule holds within
// the subtree.
func withIsolatedContainingBlocks[R any](w *Window, f func(*Window) R) R {
	frame := currentFrame(w)
	savedScopes := frame.containingBlockScopes
	savedPending := frame.icbPending
	frame.containingBlockScopes = nil
	frame.icbPending = false
	defer func() {
		frame.containingBlockScopes = savedScopes
		frame.icbPending = savedPending
	}()
	return f(w)
}

// deferAbsoluteChild hands one absolute child to the innermost open
// containing-block scope — its nearest positioned ancestor in the
// element walk. It reports false when no scope is open; the caller then
// keeps the child attached to its direct parent (the pin's behaviour).
func deferAbsoluteChild(w *Window, child LayoutID) bool {
	f := currentFrame(w)
	n := len(f.containingBlockScopes)
	if n == 0 {
		return false
	}
	scope := f.containingBlockScopes[n-1]
	scope.deferred = append(scope.deferred, child)
	return true
}

// deferFixedChild hands one fixed child to the OUTERMOST open
// containing-block scope — the window root's, when the root element
// participates (the first scope opened in the frame's request walk is
// the root's, the whole tree's outermost container, and it stays at
// the bottom of the stack while the subtree requests). CSS: a fixed
// element's containing block is the viewport —
// LayoutObject::ContainerForFixedPosition walks PAST every positioned
// ancestor to the LayoutView (layout_object.cc:1885-1892), because
// only a transform, filter or containment makes an ancestor a fixed
// container (ComputeIsFixedContainer, layout_object.cc:2009-2067) —
// and the port has no transforms, so the root is always the one. It
// reports false when no scope is open (a non-participating root
// element); the caller then keeps the child attached to its direct
// parent (the pin's behaviour, the ticket-33 documented approximation).
func deferFixedChild(w *Window, child LayoutID) bool {
	f := currentFrame(w)
	if len(f.containingBlockScopes) == 0 {
		return false
	}
	scope := f.containingBlockScopes[0]
	scope.deferred = append(scope.deferred, child)
	return true
}

// requestLayoutNode requests one layout node under the containing-block
// policy (browser-core 33, spec D3):
//
//   - a STATIC style does not attach its absolute children: each
//     defers into the innermost open scope (the nearest positioned
//     ancestor) and only the in-flow children are attached here — the
//     deferred child attaches to the scope owner's node instead, so
//     Taffy resolves its insets against the containing block's padding
//     box. With no open scope the child stays attached here (the pin's
//     behaviour);
//   - a POSITIONED style with its own scope (the participating form,
//     opened around the child walk) absorbs the absolutes deferred by
//     its static descendants and attaches them to this node, after its
//     element children;
//   - a POSITIONED style without a scope (the plain requestLayoutOf
//     seam) attaches every child directly, exactly like the pin — it
//     never steals another element's deferred children.
//
// The absolute children of a node keep their element-tree parent for
// every other phase (prepaint, paint, hitboxes, a11y): only the native
// layout attachment moves, and LayoutBounds walks the native parent
// chain, so their bounds resolve against the containing block.
//
// browser-core 36: this is the ATTACH bottom of every container (the
// div, RequestContainerLayout, RequestElementLayout and requestLayoutOf
// all land here), so the display:contents splice lands here too — the
// child list is expanded before the static/deferred split or the
// attach (contents.go). The div expands earlier as well, because its
// Block/Inline display routes the list through the inline collector
// and the published inline content before any attach.
func requestLayoutNode(w *Window, own *containingBlockScope, style Style, children []LayoutID) (LayoutID, error) {
	ds := drawState(w)
	ctx := layoutContextOf(w)
	children = expandContentsChildren(w, children)
	if style.Position == PositionFixed {
		// browser-core 35: this node IS fixed — flag the frame so every
		// container's request scan (below) looks for fixed children. The
		// flag is set at the node's own request, which runs before every
		// enclosing container's request (children are requested first),
		// and views without fixed elements pay one boolean check.
		currentFrame(w).fixedRequested = true
	}
	attached := children
	// browser-core 35 (spec L10): the fixed split. A fixed child escapes
	// EVERY ancestor — its containing block is the window root, not the
	// nearest positioned one (ContainerForFixedPosition walks past the
	// positioned ancestors, layout_object.cc:1885-1892) — so every
	// container (static, relative, absolute, sticky, a custom element, a
	// TopLayer entry) defers it into the ROOT scope instead of attaching
	// it to itself. The scan runs only in frames whose request walk saw
	// a fixed element; the static split below keeps ticket 33's rule
	// unchanged for absolute children.
	deferredFixed := false
	if currentFrame(w).fixedRequested && len(children) > 0 {
		flow := make([]LayoutID, 0, len(children))
		for _, child := range children {
			if _, position := layoutDisplayAndPositionOf(w, child); position == PositionFixed {
				if deferFixedChild(w, child) {
					deferredFixed = true
					continue
				}
			}
			flow = append(flow, child)
		}
		if deferredFixed {
			attached = flow
		}
	}
	if style.Position == PositionStatic {
		// The static opt-in: absolute children skip this element and
		// resolve against the nearest positioned ancestor (the innermost
		// open scope). The split reads each child's recorded position
		// (the engine records the display/position pair of every node at
		// request time, the pinned TaffyLayoutEngine bookkeeping).
		flow := make([]LayoutID, 0, len(attached))
		deferredAny := false
		for _, child := range attached {
			if _, position := layoutDisplayAndPositionOf(w, child); position == PositionAbsolute {
				if deferAbsoluteChild(w, child) {
					deferredAny = true
					continue
				}
			}
			flow = append(flow, child)
		}
		if deferredAny {
			attached = flow
		}
	}
	if own != nil && len(own.deferred) > 0 {
		// The scope owner: attach the absolutes deferred by static
		// descendants AND the fixed children deferred by every descendant
		// as children of this (positioned) node, after the element
		// children — out of flow, so their order in the child list does
		// not affect the in-flow layout. For the ROOT scope this is the
		// initial-containing-block stand-in (ticket 33's static-chain
		// case; browser-core 76: the root owns its scope even when its
		// own style is static, so this attach runs for a static root too —
		// the root defers its OWN absolute children into its own scope
		// above and immediately re-absorbs them, resolving against
		// itself, exactly as the ICB rule says): the fixed children and
		// the escaped absolutes become Taffy children of the root node
		// and their insets resolve against its padding box — the window.
		// (The element children here are the POST-split list: a fixed or
		// absolute child deferred above is already in own.deferred or in
		// an inner scope's deferred list, not in the element list.)
		element := attached
		attached = make([]LayoutID, 0, len(element)+len(own.deferred))
		attached = append(attached, element...)
		attached = append(attached, own.deferred...)
		own.deferred = nil
	}
	id, err := ds.engine.RequestLayout(&ctx, style, attached...)
	if err != nil {
		return id, err
	}
	// The anchoring bookkeeping (browser-core 46): every node requested
	// through the container seam records its element id path while the
	// window's engine has the walk on.
	if id.engine.anchorBookkeepingRequested {
		id.engine.nodePaths[id] = (&GlobalElementID{Path: currentElementIDs(w)}).key()
	}
	return id, nil
}

// recordAnchorPath records a node requested through a non-container
// seam (the measured leaves): the anchoring walk needs those paths too
// (browser-core 46).
func recordAnchorPath(w *Window, id LayoutID, err error) (LayoutID, error) {
	if err != nil || !id.engine.anchorBookkeepingRequested {
		return id, err
	}
	id.engine.nodePaths[id] = (&GlobalElementID{Path: currentElementIDs(w)}).key()
	return id, nil
}
