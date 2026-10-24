package gpui

// The containing-block rule for absolutely positioned elements
// (browser-core ticket 33, spec decision D3).
//
// CSS: an absolutely positioned box's containing block is the nearest
// ancestor whose position is anything but static (css-position-3 §2.1
// and §3.1, def-cb / abs-cb). Chromium decides containership exactly
// that way: LayoutObject::ComputeIsAbsoluteContainer
// (third_party/blink/renderer/core/layout/layout_object.cc:2069-2081 in
// the research checkout) returns true when the style's position is not
// kStatic; the bit is set from the style
// (layout_box_model_object.cc:503-504), read by
// LayoutObject::CanContainAbsolutePositionObjects
// (layout_object.h:2356-2360), and the containing block of an absolute
// is the first ancestor with the bit
// (LayoutObject::ContainerForAbsolutePosition, layout_object.cc:1877
// -1883, through FindAncestorByPredicate's parent-chain walk at
// :192-207). When no ancestor qualifies, the LayoutView contains it
// (ComputeIsFixedContainer, layout_object.cc:2035-2040: the LayoutView
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
// This file implements that reparenting for the opt-in (D3's first
// step): parents that set PositionStatic stop being containing blocks
// for their absolute children, which then resolve against the nearest
// positioned ancestor. The DEFAULT stays PositionRelative until ticket
// 76 flips it, so with no Static() anywhere the native tree is built
// exactly as before (bit-for-bit).
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
// behaviour — which keeps non-participating containers (Anchored,
// Deferred, custom elements that walk children outside
// RequestContainerLayout) at today's semantics for absolute children
// under a static wrapper: their absolutes escape to the nearest
// PARTICIPATING positioned ancestor, or stay on the direct parent when
// there is none. Ticket 25 (anchored) and ticket 76 (the default flip)
// can adopt RequestContainerLayout to close that gap.
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
// (relative, absolute and sticky today — everything but static, which
// is Chromium's rule; fixed joins with ticket 35). A static element
// returns nil: it opens no scope, and its absolute children defer to
// the enclosing one.
func openContainingBlock(w *Window, style *Style) *containingBlockScope {
	if style.Position == PositionStatic {
		return nil
	}
	scope := &containingBlockScope{}
	f := currentFrame(w)
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
func requestLayoutNode(w *Window, own *containingBlockScope, style Style, children []LayoutID) (LayoutID, error) {
	ds := drawState(w)
	ctx := layoutContextOf(w)
	attached := children
	if style.Position == PositionStatic {
		// The static opt-in: absolute children skip this element and
		// resolve against the nearest positioned ancestor (the innermost
		// open scope). The split reads each child's recorded position
		// (the engine records the display/position pair of every node at
		// request time, the pinned TaffyLayoutEngine bookkeeping).
		flow := make([]LayoutID, 0, len(children))
		deferredAny := false
		for _, child := range children {
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
	} else if own != nil && len(own.deferred) > 0 {
		// The scope owner: attach the absolutes deferred by static
		// descendants as children of this (positioned) node, after the
		// element children — out of flow, so their order in the child
		// list does not affect the in-flow layout.
		attached = make([]LayoutID, 0, len(children)+len(own.deferred))
		attached = append(attached, children...)
		attached = append(attached, own.deferred...)
		own.deferred = nil
	}
	return ds.engine.RequestLayout(&ctx, style, attached...)
}
