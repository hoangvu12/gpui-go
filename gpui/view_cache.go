package gpui

// This file is the port's view caching (gpui-perf ticket 04): the cached
// view element's window half — the paint index, the prepaint/paint range
// reuse that grafts a cached view's previous-frame output into the frame
// under construction, the dirty-view invalidation and the public
// CachedView constructor.
//
// The pinned reference is GPUI-CE 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a
// (R: = reference/ce-source/crates/gpui/src):
//
//   - AnyView::cached / Entity::cached / ViewElement::cached
//     (R:view.rs:34-41, 277-289, 316-328) and the cached ViewElement
//     phases (R:view.rs:339-537; view.go viewElement);
//   - PrepaintStateIndex / PaintIndex (R:window.rs:1123-1142),
//     prepaint_index / reuse_prepaint (R:window.rs:3712-3772) and
//     paint_index / reuse_paint (R:window.rs:3774-3822);
//   - the dirty views: WindowInvalidator::invalidate_view inserts the
//     notified entity (R:window.rs:189-214), Window::draw's
//     invalidate_entities marks it and its ancestor views dirty through
//     the rendered dispatch tree (R:window.rs:3316-3322, 2210-2222,
//     R:key_dispatch.rs:588-596 view_path_reversed) and clears the set
//     after draw_roots (R:window.rs:3197); Window::refresh and
//     App::apply_refresh_effect set `refreshing`, which bypasses every
//     cache for one frame (R:window.rs:2287-2292, R:app.rs:1896-1903,
//     reset at R:window.rs:3278);
//   - DispatchTree::reuse_subtree / ReusedSubtree::refresh_node_id
//     (R:key_dispatch.rs:93-113, 246-308);
//   - App::detect_accessed_entities (R:app.rs:1173-1187);
//   - LineLayoutCache::layout_index / reuse_layouts / truncate_layouts
//     (R:text_system/line_layout.rs:940-1006; text_cache.go).
//
// Mapping onto the port, recorded:
//
//   - The port's frame is split across several holders (the catalog in
//     element.go/focus.go/interact.go): hitboxes, mouse listeners, the
//     dispatch tree and the tab stops live on focusState's next/rendered
//     frames; cursor requests on interactWindowState keyed by frame;
//     input handlers on focusState; deferred draws, accessed element
//     states and inline facts on frameDrawState; the published copies of
//     the last completed frame on windowDrawState. Each index below is the
//     length of the matching ordered list, exactly like the pin's.
//   - The port registers tab stops (and the dispatch listeners) during
//     prepaint (div.go registerDivElementDispatch), so the tab-stop
//     history is part of the PREPAINT index and replays in reusePrepaint;
//     the pin's tab_handle_index is a paint index (R:window.rs:3781).
//     Mouse listeners are registered at paint, as in the pin, and are
//     reused by reusePaint only (window.go's prepaintStateIndex also marks
//     them, for transact's cut-back alone).
//   - The port has no tooltip requests (interact.go: tooltips are not
//     ported), so the pin's tooltips_index has nothing to carry.
//   - Accessed element states are a set in the port; the frame now also
//     keeps the pin's ordered log (frameDrawState.accessedElementStateLog)
//     so a reused range re-marks exactly the keys it accessed.
//   - The port keeps per-frame observables the pin has no counterpart for:
//     the inline paragraph facts (paint) are carried with the paint range
//     so a reused frame reports the same facts. The debug bounds and the
//     accessibility nodes are NOT carried, like the pin's reuse_prepaint
//     (R:window.rs:3723-3772 copies neither next_frame.debug_bounds nor
//     any a11y state): a reused view contributes neither that frame.
//   - An abandoned frame build (a returned error or panic; the pin's draw
//     cannot fail) may already have rewritten cached views' ranges against
//     the discarded frame, so it leaves the window refreshing: the next
//     frame bypasses every cache, like a Window::refresh.

import (
	"fmt"
	"reflect"
)

// paintIndex marks how far the frame under construction's paint output
// reaches (R:window.rs:1133-1142 PaintIndex, taken by paint_index,
// R:window.rs:3774-3784).
type paintIndex struct {
	scene                 uint32
	mouseListeners        int
	inputHandlers         int
	cursorStyles          int
	accessedElementStates int
	lineLayout            int
	inlineFacts           int
}

// paintIndexOf is window.rs paint_index (R:window.rs:3774-3784).
func (w *Window) paintIndex() paintIndex {
	fs := focusState(w)
	next := fs.next
	if next == nil {
		panic("gpui: no frame is being built (paint runs inside a window draw)")
	}
	frame := currentFrame(w)
	sceneLen, err := frame.scene.Len()
	if err != nil {
		panic(fmt.Sprintf("gpui: paint_index: scene len: %v", err))
	}
	return paintIndex{
		scene:                 sceneLen,
		mouseListeners:        len(next.mouseListeners),
		inputHandlers:         len(fs.frameInputHandlers),
		cursorStyles:          len(cursorRequestsOf(w, next)),
		accessedElementStates: len(frame.accessedElementStateLog),
		lineLayout:            frame.text.layoutIndex(),
		inlineFacts:           len(frame.inlineFacts),
	}
}

// cursorRequestsOf returns one frame's cursor requests (interact.go
// frameCursorRequests).
func cursorRequestsOf(w *Window, frame *renderedFrame) []cursorStyleRequest {
	st, ok := interactWindowStates[w]
	if !ok || frame == nil {
		return nil
	}
	for _, f := range st.cursorFrames {
		if f.frame == frame {
			return f.requests
		}
	}
	return nil
}

// reusePrepaint is window.rs reuse_prepaint (R:window.rs:3723-3772): the
// previous frame's prepaint registrations in the range are carried into
// the frame under construction — hitboxes, accessed element states, the
// line layouts, the dispatch subtree (with its view, focus and key
// context marks and listeners), the tab stops (prepaint-registered in the
// port) and the deferred draws, whose dispatch parent is re-pointed into
// the reused subtree and whose element is left empty so the deferred
// draw passes replay their own ranges.
func (w *Window) reusePrepaint(start, end prepaintStateIndex) {
	fs := focusState(w)
	ds := drawState(w)
	frame := currentFrame(w)
	rendered, next := fs.rendered, fs.next
	if rendered == nil || next == nil {
		panic("gpui: reuse_prepaint requires a rendered frame and a frame under construction")
	}

	next.hitboxes = append(next.hitboxes, rendered.hitboxes[start.hitboxes:end.hitboxes]...)
	reuseElementStateAccesses(frame, ds.lastAccessedElementStateLog[start.accessedElementStates:end.accessedElementStates])
	frame.text.reuseLayouts(start.lineLayout, end.lineLayout)

	reused := reuseDispatchSubtree(next.dispatchTree, rendered.dispatchTree, start.dispatchTree, end.dispatchTree)

	// The port's prepaint-time tab stops (see the file comment): the pin
	// replays tab_stops.insertion_history in reuse_paint.
	next.tabStops.Replay(rendered.tabStops.insertionHistory[start.tabStops:end.tabStops], nil)

	for _, draw := range ds.lastDeferredDraws[start.deferredDraws:end.deferredDraws] {
		copied := *draw
		copied.element = AnyElement{}
		copied.elementIDs = append([]ElementID(nil), draw.elementIDs...)
		copied.textStyles = append([]TextStyleRefinement(nil), draw.textStyles...)
		if draw.hasParentNode {
			copied.parentNode = reused.refreshNodeID(draw.parentNode)
		}
		frame.deferredDraws = append(frame.deferredDraws, &copied)
	}
}

// reusePaint is window.rs reuse_paint (R:window.rs:3786-3822): the
// previous frame's paint output in the range is carried into the frame
// under construction — cursor requests, input handlers, mouse listeners,
// accessed element states, line layouts, the inline paragraph facts (the
// port's observable) and the scene's paint operations, replayed from the
// previous scene (Scene::replay: orders are recomputed against the new
// scene).
func (w *Window) reusePaint(start, end paintIndex) {
	fs := focusState(w)
	ds := drawState(w)
	frame := currentFrame(w)
	rendered, next := fs.rendered, fs.next
	if rendered == nil || next == nil {
		panic("gpui: reuse_paint requires a rendered frame and a frame under construction")
	}

	for _, request := range cursorRequestsOf(w, rendered)[start.cursorStyles:end.cursorStyles] {
		appendCursorRequest(w, next, request)
	}
	fs.frameInputHandlers = append(fs.frameInputHandlers, fs.renderedInputHandlers[start.inputHandlers:end.inputHandlers]...)
	next.mouseListeners = append(next.mouseListeners, rendered.mouseListeners[start.mouseListeners:end.mouseListeners]...)
	reuseElementStateAccesses(frame, ds.lastAccessedElementStateLog[start.accessedElementStates:end.accessedElementStates])
	frame.text.reuseLayouts(start.lineLayout, end.lineLayout)
	frame.inlineFacts = append(frame.inlineFacts, ds.lastInlineFacts[start.inlineFacts:end.inlineFacts]...)

	if start.scene < end.scene {
		if ds.lastScene == nil {
			panic("gpui: reuse_paint: no previous scene to replay")
		}
		if err := frame.scene.Replay(start.scene, end.scene, ds.lastScene); err != nil {
			panic(fmt.Sprintf("gpui: reuse_paint: %v", err))
		}
	}
}

// reuseElementStateAccesses re-marks the element-state keys a reused range
// accessed (the accessed_element_states extend of reuse_prepaint /
// reuse_paint): they enter both the frame's log and its retention set.
func reuseElementStateAccesses(frame *frameDrawState, keys []string) {
	if len(keys) == 0 {
		return
	}
	if frame.accessedElementStates == nil {
		frame.accessedElementStates = make(map[string]struct{})
	}
	for _, key := range keys {
		frame.accessedElementStates[key] = struct{}{}
	}
	frame.accessedElementStateLog = append(frame.accessedElementStateLog, keys...)
}

// reusedSubtree is the pin's ReusedSubtree (R:key_dispatch.rs:93-113).
type reusedSubtree struct {
	oldStart, oldEnd int
	newStart         int
}

// refreshNodeID is ReusedSubtree::refresh_node_id: a node of the reused
// old range, renumbered into the new tree.
func (r reusedSubtree) refreshNodeID(id DispatchNodeID) DispatchNodeID {
	if id.Index < r.oldStart || id.Index >= r.oldEnd {
		panic(fmt.Sprintf("gpui: dispatch node %d was not part of the reused subtree [%d, %d)", id.Index, r.oldStart, r.oldEnd))
	}
	return DispatchNodeID{Index: id.Index - r.oldStart + r.newStart}
}

// reuseDispatchSubtree is DispatchTree::reuse_subtree
// (R:key_dispatch.rs:264-308): the source's nodes in [start, end) are
// re-pushed under the target's active node, keeping their parent shape,
// key contexts, focus and view marks and listeners (move_node,
// R:key_dispatch.rs:246-262). The pin's contains_focus feeds
// next_frame.focus; the port records the frame's focus at finish for
// every frame (helpers_frame.go), so it is not needed here.
func reuseDispatchSubtree(target, source *DispatchTree, start, end int) reusedSubtree {
	reused := reusedSubtree{oldStart: start, oldEnd: end, newStart: len(target.nodes)}
	var sourceStack []int
	for ix := start; ix < end; ix++ {
		node := &source.nodes[ix]
		for len(sourceStack) > 0 {
			ancestor := sourceStack[len(sourceStack)-1]
			if node.hasParent && node.parent.Index == ancestor {
				break
			}
			sourceStack = sourceStack[:len(sourceStack)-1]
			target.PopNode()
		}
		sourceStack = append(sourceStack, ix)
		moveDispatchNode(target, node)
	}
	for range sourceStack {
		target.PopNode()
	}
	return reused
}

// moveDispatchNode is DispatchTree::move_node (R:key_dispatch.rs:246-262).
// The pin takes the listener vectors out of the source; the source frame
// is discarded when the new one is published, so the port shares them.
func moveDispatchNode(target *DispatchTree, source *dispatchNode) {
	target.PushNode()
	if source.context != nil {
		target.SetKeyContext(*source.context)
	}
	if source.hasFocusID {
		target.SetFocusID(source.focusID)
	}
	if source.hasViewID {
		target.SetViewID(source.viewID)
	}
	id := target.activeNodeIDValue()
	node := &target.nodes[id.Index]
	node.keyListeners = source.keyListeners
	node.actionListeners = source.actionListeners
	node.modifiersChangedListeners = source.modifiersChangedListeners
	// Browser-core ticket 14: a reused suppressed node keeps its
	// focus-suppression mark (the end-of-frame focus fixup reads it;
	// focus.go fixupFocusedElement).
	node.focusSuppressed = source.focusSuppressed
}

// viewPathReversed is DispatchTree::view_path_reversed
// (R:key_dispatch.rs:588-596): the view marks from the view's node up to
// the root.
func (t *DispatchTree) viewPathReversed(view EntityID) []EntityID {
	id, ok := t.viewNodeIDs[view]
	if !ok {
		return nil
	}
	var out []EntityID
	for {
		node := &t.nodes[id.Index]
		if node.hasViewID {
			out = append(out, node.viewID)
		}
		if !node.hasParent {
			return out
		}
		id = node.parent
	}
}

// invalidateEntities is Window::invalidate_entities
// (R:window.rs:3316-3322): every view notified since the last draw is
// marked dirty with its ancestor views through the rendered dispatch tree
// (mark_view_dirty, R:window.rs:2210-2222).
func invalidateEntities(w *Window, ds *windowDrawState) {
	if len(ds.invalidatedViews) == 0 {
		return
	}
	var tree *DispatchTree
	if rendered := focusState(w).rendered; rendered != nil {
		tree = rendered.dispatchTree
	}
	for view := range ds.invalidatedViews {
		if tree == nil {
			continue
		}
		for _, id := range tree.viewPathReversed(view) {
			if _, dirty := ds.dirtyViews[id]; dirty {
				break
			}
			if ds.dirtyViews == nil {
				ds.dirtyViews = make(map[EntityID]struct{})
			}
			ds.dirtyViews[id] = struct{}{}
		}
	}
	clear(ds.invalidatedViews)
}

// markRefreshing is the `refreshing = true` half of Window::refresh and
// App::apply_refresh_effect (R:window.rs:2287-2292, R:app.rs:1896-1903):
// the next frame bypasses every view cache.
func (w *Window) markRefreshing() {
	if ds, ok := windowDrawStates[w]; ok {
		ds.refreshing = true
	}
}

// detectAccessedEntities is App::detect_accessed_entities
// (R:app.rs:1173-1187): f runs and the entities it newly tracked for the
// window (those not already tracked by the frame before it) are returned.
func detectAccessedEntities(w *Window, f func()) map[EntityID]struct{} {
	fs := frameStateOf(w)
	before := make(map[EntityID]struct{}, len(fs.nextTrackedEntities))
	for id := range fs.nextTrackedEntities {
		before[id] = struct{}{}
	}
	f()
	accessed := make(map[EntityID]struct{})
	for id := range fs.nextTrackedEntities {
		if _, ok := before[id]; !ok {
			accessed[id] = struct{}{}
		}
	}
	return accessed
}

// extendAccessedEntities is EntityMap::extend_accessed: a reused view's
// recorded entities stay tracked by the window.
func extendAccessedEntities(w *Window, accessed map[EntityID]struct{}) {
	for id := range accessed {
		trackEntity(w, id)
	}
}

// ---------------------------------------------------------------------------
// The cached view element's retained state (R:view.rs:339-350)
// ---------------------------------------------------------------------------

// viewElementState is the pin's ViewElementState: the cached view's
// prepaint and paint ranges in the frame that drew it, the cache key and
// the entities its render accessed.
type viewElementState struct {
	prepaintStart, prepaintEnd prepaintStateIndex
	paintStart, paintEnd       paintIndex
	cacheKey                   viewElementCacheKey
	accessedEntities           map[EntityID]struct{}
}

// viewElementCacheKey is the pin's ViewElementCacheKey: bounds, content
// mask and text style.
type viewElementCacheKey struct {
	bounds      Bounds
	contentMask Bounds
	textStyle   TextStyle
}

// matches is the cache-key comparison of R:view.rs:430-433 (TextStyle's
// derived PartialEq compares its optional members by value).
func (k viewElementCacheKey) matches(bounds, contentMask Bounds, textStyle TextStyle) bool {
	return k.bounds == bounds && k.contentMask == contentMask && reflect.DeepEqual(k.textStyle, textStyle)
}

// ---------------------------------------------------------------------------
// The public constructor
// ---------------------------------------------------------------------------

// CachedView embeds an entity-backed view as a cached view element laid
// out at style (AnyView::cached / Entity::cached, R:view.rs:34-41,
// 277-289). The rendered subtree is recycled from the previous frame —
// its layout, prepaint registrations and painted scene operations — until
// the view's entity (or a view it renders) is notified, its bounds,
// content mask or text style change, or the window is refreshed
// (Window.Refresh ignores caching for one frame).
//
// Caching requires a definite size: the cached view is laid out from
// style, not measured from its contents (the caller supplies the layout
// style because caching skips rendering the contents). style is a
// *StyleBuilder (gpui.Styles().SizeFull()) or the closure form. Caching
// is only sound for entity-backed views, where Notify is the contract
// that busts the cache, so a view without an entity (a RenderOnce
// recipe) panics, mirroring the pin's crate-private ViewElement::cached.
func CachedView[F StyleArg](view View, style F) AnyElement {
	if view == nil || isNilReflect(view) {
		panic("gpui: CachedView requires a non-nil view")
	}
	if _, ok := view.EntityID(); !ok {
		panic("gpui: CachedView requires an entity-backed view (a recipe has no notify contract to bust the cache)")
	}
	refinement := stateRefinement(style, "CachedView")
	elem := newViewElement(view)
	elem.cachedStyle = refinement
	return CustomElement[AnyElement, AnyElement](elem)
}
