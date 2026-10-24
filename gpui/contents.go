package gpui

import "fmt"

// The display: contents splice (browser-core ticket 36, spec story 12).
//
// CSS: a display: contents element generates no box; its children are
// laid out as if they were children of the element's parent
// (css-display-3 §3.1). Chromium implements exactly that in the layout
// tree: no LayoutObject is created for the element
// (LayoutObject::CreateObject's switch returns nullptr for
// EDisplay::kContents, third_party/blink/renderer/core/layout/
// layout_object.cc:430-432 in the research checkout @ 0d4dcd4f), and
// the tree builder's sibling walk splices the children into the
// grandparent's child list (NextLayoutSiblingInternal /
// NextLayoutSibling, core/dom/layout_tree_builder_traversal.cc:
// 604-634: a contents sibling is skipped and its first inner child
// takes its place, climbing through contents parents the same way).
//
// The port splices at REQUEST LAYOUT. The native ABI attaches children
// when the parent node is created (CreateNode; there is no reparent
// call — the same constraint ticket 33's containing-block walk works
// under), so the splice decision is made where the attachment is: a
// contents div requests no node of its own, hands its children to the
// enclosing node request, and keeps walking them for every other phase
// (prepaint, paint, hitboxes) through the ordinary element tree — the
// element itself paints and hit-tests nothing, like a contents box in
// CSS.
//
// Mechanism: a contents element returns its FIRST child's layout id to
// the parent's child walk and records the remaining children as that
// id's continuation in the frame (contentsContinuations). Every node
// request splices continuations before attaching
// (expandContentsChildren at the top of requestLayoutNode — the shared
// bottom of the div, RequestContainerLayout and RequestElementLayout,
// so every attaching container participates), and the div also expands
// before the two non-attaching consumers of a child list (the inline
// paragraph collector and the published inline content). Nested
// contents works because a contents element expands its own collected
// list the same way before publishing it, and an empty contents
// element contributes nothing (it requests an inert DisplayNone node
// so its drawable still has bounds to resolve).
//
// Limitations (recorded): a contents element at a window ROOT — the
// root path (AnyElement.LayoutAsRoot) computes one node — lays out only
// its first child; keep contents below the root. A contents element's
// own box styles (background, borders, padding, size, position, sticky
// and overflow) are ignored, as CSS says (no box to apply them to);
// inherited text styling still flows through the text-style stack of
// the walk. Mouse listeners on the contents element never fire (it has
// no box to hit); key listeners and actions still register on its
// dispatch node, which sits between its children and the parent in the
// tree, like a DOM element with no box.

// recordContentsContinuation notes that the children after first
// belong at first's position in whatever node request receives it (the
// frame-scoped splice register; entries are consumed exactly once by
// expandContentsChildren, and the map dies with the frame).
func recordContentsContinuation(w *Window, first LayoutID, rest []LayoutID) {
	frame := currentFrame(w)
	if frame.contentsContinuations == nil {
		frame.contentsContinuations = make(map[LayoutID][]LayoutID)
	}
	frame.contentsContinuations[first] = rest
}

// expandContentsChildren splices every recorded contents continuation
// into a collected child list, recursively (a continuation's members
// can themselves be contents first-children): each child id stays, and
// the children a contents element parked behind it join it in order.
// The returned slice shares storage with children when nothing splices
// (the common case: one nil-map check per element).
func expandContentsChildren(w *Window, children []LayoutID) []LayoutID {
	frame := currentFrame(w)
	if len(frame.contentsContinuations) == 0 {
		return children
	}
	out := children
	changed := false
	for i, id := range children {
		rest, ok := frame.contentsContinuations[id]
		if ok {
			delete(frame.contentsContinuations, id)
			rest = expandContentsChildren(w, rest)
		}
		if !changed {
			if !ok {
				// Nothing spliced yet: children[:i+1] still aliases out.
				continue
			}
			out = make([]LayoutID, 0, len(children)+len(rest))
			out = append(out, children[:i+1]...)
			changed = true
		} else {
			out = append(out, id)
		}
		if ok {
			out = append(out, rest...)
		}
	}
	return out
}

// contentsChildrenOf is the div's contents arm: after the child walk,
// expand the list (nested contents), then either publish the splice
// (first child up, rest recorded behind it) or — for an element with no
// children — request an inert none node so the drawable has bounds to
// resolve. The caller passes the frame state to fill.
func requestContentsLayout(w *Window, state *divFrameState, app *App) LayoutID {
	children := expandContentsChildren(w, state.childLayoutIDs)
	state.childLayoutIDs = children
	if len(children) == 0 {
		// No box and no children: an inert none node takes no space in
		// the parent (Taffy skips none nodes), keeps the drawable's
		// bounds resolution valid, and prepaints/paints nothing. A
		// contents element has no box, so it opens no containing-block
		// scope either (its position is inert, css-display-3: box-
		// generating properties do not apply).
		style := DefaultStyle()
		style.Display = DisplayNone
		id, err := requestLayoutNode(w, nil, style, nil)
		if err != nil {
			panic(fmt.Sprintf("gpui: DivElement contents request_layout: %v", err))
		}
		return id
	}
	first := children[0]
	if len(children) > 1 {
		recordContentsContinuation(w, first, children[1:])
	}
	return first
}
