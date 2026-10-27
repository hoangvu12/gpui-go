package gpui

// This file is browser-core ticket 59: the AUTOMATIC reuse of views.
// The port's opt-in view caching (view_cache.go, the pin's
// AnyView::cached) grows into the default for every entity-backed
// ViewOf/ManagedViewOf child: a view whose tracked inputs did not
// change — its entity not notified, the entities its render accessed
// not notified, no refresh, no drag, no inspector — is laid out from
// its RECORDED node style and its whole subtree's prepaint and paint
// output is replayed from the previous frame, like the pin's cached
// view. Reused views keep their accessibility nodes: the view's a11y
// node range is recorded on the fresh frame and replayed on the reused
// one, so the published tree is identical frame over frame (the
// gpui-fast model: reused subtrees replay their a11y log,
// GF:fast/a11y.rs:1-30; Chromium's accessibility updates are atomic
// diffs that leave untouched subtrees standing,
// ui/accessibility/ax_tree_update.h:19-36).
//
// The design, pinned to its sources:
//
//   - The reuse decision is gpui-fast's reusable_record
//     (GF:fast/retained.rs:587-626: dependencies, hovers, offsets,
//     refreshing, drag, a11y staleness) translated to the port's
//     invalidation seams: dependencies are the entity notifications
//     (the window's dirty-view marking plus this file's per-view
//     dependency index — a notified entity a view's render ACCESSED
//     dirties that view, and the ancestors through the dispatch tree,
//     the port's mark_view_dirty), hovers and scroll offsets arrive as
//     notifications of the view that painted the listener (interact.go
//     paintHoverGroupHandler, scroll_element.go paintListener — both
//     notify the current view on the flip), refreshing is the window
//     refresh, and the drag and the inspector disable the candidate
//     outright. Chromium's incremental layout is the same shape from
//     the other side: a clean subtree (no self, child or simplified
//     needs — layout_object.h:1477-1481 NeedsLayout) is skipped, and a
//     dirty one marks its container chain
//     (layout_object.h:4203-4211 SetChildNeedsLayout,
//     layout_object.cc:1690-1715 MarkContainerChainForLayout's core:
//     stop at an already-marked container).
//   - The layout ask: gpui-fast retains the Taffy nodes themselves
//     (GF:fast/layout.rs request_retained_layout) — the port resets
//     the engine every frame, so reuse requests ONE node at the style
//     the view's content root was last requested with (the drawable's
//     LayoutNodeStyle report, div.go lastStyle) and only when that
//     style's own size is provable without measuring the content: both
//     size axes definite (px/rem) or exactly 100%, a plain container
//     display (block, flex, grid), no overflow: auto root (the
//     scrollbar reservation rewrites the node style, div.go
//     autoOverflowRecordStyle). Everything else — content-sized views,
//     fraction sizes other than full, inline roots, display: contents,
//     fixed-position roots (the deferring wrapper) — falls back to a
//     fresh render every frame, "when in doubt, re-render". A miss
//     (the parent moved the slot: the key's bounds, content mask or
//     text style changed) re-renders and lays the content out as a
//     root at the computed bounds exactly like the pin's cached miss
//     (R:view.rs:406-485), which is pixel-identical for the provable
//     styles — the view-cache differential test's guarantee.
//   - The dependency index: the pin's cached views are busted only by
//     VIEW notifications (dirty_views), leaving model notifications to
//     the app's observe-and-notify discipline; the automatic path is
//     on for every app, so entities read during a view's fresh render
//     (captured around the request, prepaint and paint phases through
//     the frame's tracked-entity LOG — an O(1) mark, unlike the pin's
//     set diff) are recorded per view, and invalidateEntities marks
//     every view that accessed a notified entity — with its
//     ancestors — dirty. Globals read during the frame are recorded
//     per window; a global notify refreshes every window that read the
//     type (the full bust, correctness first).
//   - The frame records a reuse must re-contribute (beyond the pin's
//     prepaint/paint ranges, which reusePrepaint/reusePaint already
//     carry): the accessibility node range (this file's replay), the
//     debug-selector bounds, the anchor and invoker registries, the
//     auto-scrollbar facts, the user-select registrations and the a11y
//     action listeners. The pin carries none of these (R:window.rs:
//     3723-3772 copies neither debug bounds nor a11y state) — honest
//     divergence: the automatic path turns every one of them into a
//     test-visible observable, so they ride the record.
//
// The opt-outs: GPUI_VIEW_REUSE=0 disables it process-wide
// (gpui-fast's GPUI_VIEW_RETENTION=0, GF:fast/retained.rs:292),
// WindowOptions.DisableViewReuse / SetWindowViewReuse per window, and
// WithoutViewReuse per view.

import (
	"os"
	"reflect"
	"strings"
	"sync"
)

// ---------------------------------------------------------------------------
// The opt-outs
// ---------------------------------------------------------------------------

// viewReuseEnabled resolves the process-wide switch: GPUI_VIEW_REUSE=0
// (or "false") disables the automatic reuse everywhere, like
// gpui-fast's GPUI_VIEW_RETENTION=0 (GF:fast/retained.rs:292). The
// default is on.
var viewReuseEnabled = sync.OnceValue(func() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GPUI_VIEW_REUSE"))) {
	case "0", "false", "off", "no":
		return false
	default:
		return true
	}
})

// SetWindowViewReuse turns this window's automatic view reuse on or
// off (the per-window opt-out, browser-core 59). It can be called any
// time outside a frame; the next draw either reuses the window's views
// or renders them all fresh. Real windows can also disable it at open
// through WindowOptions.DisableViewReuse. GPUI_VIEW_REUSE=0 overrides
// this to off process-wide.
func SetWindowViewReuse(w *Window, enabled bool) {
	if w == nil {
		return
	}
	if ds, ok := windowDrawStates[w]; ok {
		ds.viewReuseDisabled = !enabled
	}
}

// ViewReuseEnabled reports whether this window's views are reused
// automatically (after every opt-out).
func ViewReuseEnabled(w *Window) bool {
	return windowViewReuseEnabled(w)
}

// windowViewReuseEnabled is the whole gate (view_reuse.go's file
// comment: gpui-fast's reusable_record conditions).
func windowViewReuseEnabled(w *Window) bool {
	if !viewReuseEnabled() {
		return false
	}
	if ds, ok := windowDrawStates[w]; ok && ds.viewReuseDisabled {
		return false
	}
	return true
}

// uncachedView wraps one view so the automatic reuse never applies to
// it (the per-view opt-out): it renders fresh on every frame while
// keeping its identity (notifications still invalidate it).
type uncachedView struct {
	view View
}

// EntityID implements View: the wrapped view's identity.
func (v uncachedView) EntityID() (EntityID, bool) { return v.view.EntityID() }

// RenderOnce implements View: the wrapped view's render.
func (v uncachedView) RenderOnce(w *Window, app *App) AnyElement { return v.view.RenderOnce(w, app) }

// noReuse is the automatic reuse's marker (viewElement checks it).
func (v uncachedView) noReuse() {}

// WithoutViewReuse returns view with the automatic view reuse turned
// off for that placement (the per-view opt-out, browser-core 59): the
// view renders fresh on every frame. Notifications keep invalidating
// it normally; only the reuse is suppressed.
func WithoutViewReuse(view View) View {
	if view == nil || isNilReflect(view) {
		panic("gpui: WithoutViewReuse requires a non-nil view")
	}
	return uncachedView{view: view}
}

// windowInspectorActive reports whether the window's inspector is on
// (the tree build records a node per element during prepaint — a
// reused view would vanish from it; reuse is disabled while it runs,
// and toggling the inspector refreshes anyway).
func windowInspectorActive(w *Window) bool {
	if st := windowInspectorStateOf(w); st != nil {
		return st.active
	}
	return false
}

// ---------------------------------------------------------------------------
// The provable node style
// ---------------------------------------------------------------------------

// viewNodeStyleReproducible reports whether one layout node requested
// at style reproduces the view's committed bounds without measuring
// the content (the automatic reuse's definite-size rule, the ticket's
// "fall back to a fresh render when the key can't be proven"):
//
//   - a plain container display (block, flex, grid): the request
//     attaches one node the parent lays out exactly like the content's
//     root node; contents/none change the node's meaning and the
//     inline displays ride the paragraph machinery;
//   - no overflow: auto root: the scrollbar reservation rewrites the
//     style the node was requested with (div.go
//     autoOverflowRecordStyle), so the recorded style is not the node
//     style;
//   - both size axes definite (px or rem) or exactly 100%: the node's
//     size resolves without its children. A fraction other than full
//     resolves against the parent's inner size while a miss's
//     LayoutAsRoot resolves it against the computed bounds — the two
//     disagree, so only the full fraction is kept.
func viewNodeStyleReproducible(style Style) bool {
	switch style.Display {
	case DisplayBlock, DisplayFlex, DisplayGrid:
	default:
		return false
	}
	if style.OverflowX == OverflowAuto || style.OverflowY == OverflowAuto {
		return false
	}
	return viewLengthProvable(style.Size.Width) && viewLengthProvable(style.Size.Height)
}

// viewLengthProvable is one size axis of the definite-size rule.
func viewLengthProvable(l Length) bool {
	if l.Auto {
		return false
	}
	switch l.Definite.Kind {
	case DefiniteLengthPx, DefiniteLengthRem:
		return true
	case DefiniteLengthFraction:
		return l.Definite.Value == 1
	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// The retained record (viewElementState.reuse)
// ---------------------------------------------------------------------------

// viewReuseRecord is the automatic path's retained record: everything
// a reused view must re-contribute to the frame beyond the prepaint
// and paint ranges the pin's viewElementState already carries, plus
// the node style the synthesized request replays. It is captured
// around a fresh render's phases and completes at the paint record —
// an abandoned build never completes it, and the pin's
// refreshing-after-abandon rule (view_cache.go) forces a fresh render
// anyway.
type viewReuseRecord struct {
	// nodeStyle is the style the content's root node was last requested
	// with (the drawable's LayoutNodeStyle report; for overflow: auto
	// roots it is the style BEFORE the reservation, so those views are
	// not reproducible and carry no record at all).
	nodeStyle Style
	// complete marks the record after the paint-phase capture.
	complete bool
	// deps is the deferred round's entity reads (deferred.go): the
	// entities attributed to the owning view's dependency index when the
	// draw ran fresh and re-tracked when it replays. The view-level
	// record leaves it nil (the view's state holds the deps).
	deps map[EntityID]struct{}
	// a11y is the subtree's accessibility node range with its focus
	// bindings, focus and active-descendant state, and (after paint)
	// its action listeners.
	a11y *viewA11yRecord
	// debugBounds, anchors and invokers are the frame registries the
	// subtree touched (the recorded values; the map-diff capture keeps
	// the later-same-name-wins order stable across replays).
	debugBounds map[string]Bounds
	anchors     map[string]anchorRecord
	invokers    map[string]string
	// scrollbarFacts are the subtree's overflow: auto scrollbar facts.
	scrollbarFacts []AutoScrollbarWindowFacts
	// userSelect and userSelectSlotTo are the subtree's paint-time
	// user-select registrations and the slot counter after them.
	userSelect       []userSelectRegistration
	userSelectSlotTo uint64
	// scrolls is the scroll-handle state the subtree's phases read with
	// the versions they saw (browser-core 59, view_reuse.go): the reuse
	// check refuses the reuse when an unnotified programmatic write
	// (SetOffset, ScrollToBottom) moved one — the pin's "the scrollable
	// element clamps it at its next prepaint" keeps holding.
	scrolls map[*scrollHandleState]uint64
}

// viewA11yRecord is one view's accessibility contribution: the pending
// nodes its subtree pushed, in the build's pop order (children before
// parents, roots in prepaint order), the ids that are CHILDREN of
// nodes in the range (the rest are the range's roots, linked under the
// current build parent at replay), the focusable bindings, the focus
// and active-descendant state the range set, and the action listeners
// the paint phase registered. Replaying it re-creates the identical
// tree: the same ids (so the UIA node identity and generations stand,
// the windows platform contract), the same specs, bounds, children and
// actions.
type viewA11yRecord struct {
	nodes    []a11yPendingNode
	childIDs map[A11yNodeID]struct{}
	// focusable holds the focus-handle bindings of the range's nodes.
	focusable map[A11yNodeID]uint64
	// focus and focusSet record that a node of the range was reported
	// as the frame's focus.
	focus    A11yNodeID
	focusSet bool
	// activeDescendant and activeDescendantSet record the range's
	// aria_active_descendant claim.
	activeDescendant    A11yNodeID
	activeDescendantSet bool
	// listeners holds the paint-time action listeners by node.
	listeners map[A11yNodeID]map[A11yAction][]A11yActionListener
}

// viewPrepaintCapture is the scratch of one fresh prepaint's capture.
type viewPrepaintCapture struct {
	// a11yStart is the build's pending-node count at the capture start
	// (the a11y inactive leaves ok false and no record).
	ok        bool
	a11yStart int
	// debugBefore, anchorsBefore and invokersBefore snapshot the frame
	// registries before the subtree ran (nil when the registry did not
	// exist yet — the whole map then belongs to this capture).
	debugBefore    map[string]Bounds
	anchorsBefore  map[string]anchorRecord
	invokersBefore map[string]string
	// factsStart is the scrollbar-facts slice length before.
	factsStart int
	// scrollStart is the scroll-read log length before.
	scrollStart int
}

// beginViewPrepaintCapture snapshots the frame records around a fresh
// subtree prepaint (or render+prepaint — the miss closure). The
// captured maps are cloned only when they already exist; a nil
// registry means every entry written after belongs to the subtree.
func beginViewPrepaintCapture(w *Window) viewPrepaintCapture {
	frame := currentFrame(w)
	c := viewPrepaintCapture{ok: true, factsStart: len(frame.autoScrollbarFacts), scrollStart: len(frame.scrollReads)}
	if st := activeA11yBuild(w); st != nil {
		c.a11yStart = len(st.nodes)
	}
	if frame.debugBounds != nil {
		c.debugBefore = cloneMap(frame.debugBounds)
	}
	if windowTracksAnchors(w) && frame.anchorBounds != nil {
		c.anchorsBefore = cloneMap(frame.anchorBounds)
	}
	if windowTracksInvokers(w) && frame.invokerPaths != nil {
		c.invokersBefore = cloneMap(frame.invokerPaths)
	}
	return c
}

// cloneMap is the generic map clone (the capture snapshots).
func cloneMap[K comparable, V any](m map[K]V) map[K]V {
	out := make(map[K]V, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// finishViewPrepaintCapture closes the capture and returns the
// prepaint-phase half of the record: the a11y node range with its
// focus state, the registry entries the subtree added or overrode,
// and the scrollbar facts it recorded. The node copies carry their own
// children lists; the child-id set marks which nodes are NOT the
// range's roots.
func finishViewPrepaintCapture(w *Window, c viewPrepaintCapture) *viewReuseRecord {
	if !c.ok {
		return nil
	}
	frame := currentFrame(w)
	rec := &viewReuseRecord{}
	if st := windowA11yStateOf(w); st != nil && st.building && st.framePtr == frame {
		end := len(st.nodes)
		if end > c.a11yStart {
			a11y := &viewA11yRecord{
				childIDs: map[A11yNodeID]struct{}{},
			}
			for i := c.a11yStart; i < end; i++ {
				n := st.nodes[i]
				node := *n
				node.children = append([]A11yNodeID(nil), n.children...)
				a11y.nodes = append(a11y.nodes, node)
				for _, child := range n.children {
					a11y.childIDs[child] = struct{}{}
				}
				if handle, ok := st.focusable[n.id]; ok {
					if a11y.focusable == nil {
						a11y.focusable = map[A11yNodeID]uint64{}
					}
					a11y.focusable[n.id] = handle
				}
			}
			// The focus and active-descendant state the range set: a node
			// of this range must be the claimer (the pushing element's own
			// node).
			if st.focusSet && nodeInA11yRange(a11y, st.focus) {
				a11y.focus, a11y.focusSet = st.focus, true
			}
			if st.activeDescendantSet && nodeInA11yRange(a11y, st.activeDescendant) {
				a11y.activeDescendant, a11y.activeDescendantSet = st.activeDescendant, true
			}
			rec.a11y = a11y
		}
	}
	if frame.debugBounds != nil {
		for k, v := range frame.debugBounds {
			if before, ok := c.debugBefore[k]; !ok || before != v {
				if rec.debugBounds == nil {
					rec.debugBounds = map[string]Bounds{}
				}
				rec.debugBounds[k] = v
			}
		}
	}
	if windowTracksAnchors(w) && frame.anchorBounds != nil {
		for k, v := range frame.anchorBounds {
			if before, ok := c.anchorsBefore[k]; !ok || before != v {
				if rec.anchors == nil {
					rec.anchors = map[string]anchorRecord{}
				}
				rec.anchors[k] = v
			}
		}
	}
	if windowTracksInvokers(w) && frame.invokerPaths != nil {
		for k, v := range frame.invokerPaths {
			if before, ok := c.invokersBefore[k]; !ok || before != v {
				if rec.invokers == nil {
					rec.invokers = map[string]string{}
				}
				rec.invokers[k] = v
			}
		}
	}
	if end := len(frame.autoScrollbarFacts); end > c.factsStart {
		rec.scrollbarFacts = append([]AutoScrollbarWindowFacts(nil), frame.autoScrollbarFacts[c.factsStart:end]...)
	}
	// The scroll-handle reads of the subtree's phases, with the versions
	// as they stand AFTER the prepaint (the clamp's own writes are
	// deterministic per layout and need no bump).
	if end := len(frame.scrollReads); end > c.scrollStart {
		rec.scrolls = map[*scrollHandleState]uint64{}
		for _, state := range frame.scrollReads[c.scrollStart:end] {
			rec.scrolls[state] = state.version
		}
	}
	return rec
}

// nodeInA11yRange reports whether id is one of the range's RECORDED
// nodes (a node the subtree pushed, not a child reference).
func nodeInA11yRange(a11y *viewA11yRecord, id A11yNodeID) bool {
	for i := range a11y.nodes {
		if a11y.nodes[i].id == id {
			return true
		}
	}
	return false
}

// captureViewA11yListeners closes the paint-phase half of the a11y
// record: the action listeners registered for the range's nodes during
// the subtree's paint.
func captureViewA11yListeners(w *Window, rec *viewReuseRecord) {
	if rec == nil || rec.a11y == nil {
		return
	}
	st := windowA11yStateOf(w)
	if st == nil || !st.building {
		return
	}
	for i := range rec.a11y.nodes {
		id := rec.a11y.nodes[i].id
		actions, ok := st.listeners[id]
		if !ok || len(actions) == 0 {
			continue
		}
		if rec.a11y.listeners == nil {
			rec.a11y.listeners = map[A11yNodeID]map[A11yAction][]A11yActionListener{}
		}
		own := map[A11yAction][]A11yActionListener{}
		for action, listeners := range actions {
			own[action] = append([]A11yActionListener(nil), listeners...)
		}
		rec.a11y.listeners[id] = own
	}
}

// replayViewPrepaintRecords re-contributes a reused view's
// prepaint-phase frame records (view_reuse.go's file comment): the a11y
// node range, the registry entries and the scrollbar facts, at the
// view's prepaint position so the later-same-name-wins order holds.
func replayViewPrepaintRecords(w *Window, rec *viewReuseRecord) {
	if rec == nil {
		return
	}
	frame := currentFrame(w)
	replayViewA11yNodes(w, rec.a11y)
	for k, v := range rec.debugBounds {
		if frame.debugBounds == nil {
			frame.debugBounds = map[string]Bounds{}
		}
		frame.debugBounds[k] = v
	}
	for k, v := range rec.anchors {
		if frame.anchorBounds == nil {
			frame.anchorBounds = map[string]anchorRecord{}
		}
		frame.anchorBounds[k] = v
	}
	for k, v := range rec.invokers {
		if frame.invokerPaths == nil {
			frame.invokerPaths = map[string]string{}
		}
		frame.invokerPaths[k] = v
	}
	if len(rec.scrollbarFacts) > 0 {
		frame.autoScrollbarFacts = append(frame.autoScrollbarFacts, rec.scrollbarFacts...)
	}
}

// replayViewA11yNodes re-pushes a reused view's a11y node range into
// the current build: the node copies (same ids, specs, bounds and
// children) are appended in the recorded order, the range's roots —
// the nodes that are not children of nodes in the range — link under
// the build's current parent, and the focus bindings, the focus and
// active-descendant claims and the node ids' seen marks are restored.
// The published tree is identical to the frame that recorded it.
func replayViewA11yNodes(w *Window, a11y *viewA11yRecord) {
	if a11y == nil {
		return
	}
	st := activeA11yBuild(w)
	if st == nil || len(st.stack) == 0 {
		return
	}
	parent := st.stack[len(st.stack)-1]
	for i := range a11y.nodes {
		n := &a11y.nodes[i]
		if st.seen[n.id] {
			// A duplicate id this frame: discarded and recorded
			// (a11y.rs can_push).
			st.pendingCollisions = append(st.pendingCollisions, n.id)
			continue
		}
		st.seen[n.id] = true
		node := *n
		node.children = append([]A11yNodeID(nil), n.children...)
		st.nodes = append(st.nodes, &node)
		if _, isChild := a11y.childIDs[n.id]; !isChild {
			parent.children = append(parent.children, n.id)
		}
	}
	for id, handle := range a11y.focusable {
		st.focusable[id] = handle
	}
	if a11y.focusSet {
		st.setFocus(a11y.focus)
	}
	if a11y.activeDescendantSet {
		st.setActiveDescendant(a11y.activeDescendant)
	}
}

// replayViewA11yListeners re-registers a reused view's a11y action
// listeners at the paint phase (div.rs registers them in paint; the
// closures are shared from the recorded frame like the dispatch
// listeners moveDispatchNode shares).
func replayViewA11yListeners(w *Window, a11y *viewA11yRecord) {
	if a11y == nil || len(a11y.listeners) == 0 {
		return
	}
	st := activeA11yBuild(w)
	if st == nil {
		return
	}
	for id, actions := range a11y.listeners {
		own, ok := st.listeners[id]
		if !ok {
			own = map[A11yAction][]A11yActionListener{}
			st.listeners[id] = own
		}
		for action, listeners := range actions {
			own[action] = append(own[action], listeners...)
		}
	}
}

// replayViewPaintRecords re-contributes a reused view's paint-phase
// frame records: the user-select registrations (so the selection
// participants of a reused view are not swept as stopped drawing) and
// the a11y action listeners.
func replayViewPaintRecords(w *Window, rec *viewReuseRecord) {
	if rec == nil {
		return
	}
	frame := currentFrame(w)
	if len(rec.userSelect) > 0 {
		frame.userSelectPending = append(frame.userSelectPending, rec.userSelect...)
	}
	if frame.userSelectSlot < rec.userSelectSlotTo {
		frame.userSelectSlot = rec.userSelectSlotTo
	}
	replayViewA11yListeners(w, rec.a11y)
}

// ---------------------------------------------------------------------------
// The dependency capture and index
// ---------------------------------------------------------------------------

// trackedLogMark is the O(1) mark into the frame's tracked-entity log
// (the diff base for a view's dependency capture).
func trackedLogMark(w *Window) int {
	return len(frameStateOf(w).nextTrackedLog)
}

// accessedEntitiesSinceMark diffs the frame's tracked-entity log since
// mark into a set: the entities the phases that ran in between
// accessed (the log spans only the capture's own closure in each
// phase, so the diff is exactly the view's subtree's accesses).
func accessedEntitiesSinceMark(w *Window, mark int) map[EntityID]struct{} {
	fs := frameStateOf(w)
	if len(fs.nextTrackedLog) <= mark {
		return nil
	}
	out := make(map[EntityID]struct{}, len(fs.nextTrackedLog)-mark)
	for _, id := range fs.nextTrackedLog[mark:] {
		out[id] = struct{}{}
	}
	return out
}

// mergeEntityDeps unions b into a (the request, prepaint and paint
// captures of one fresh render).
func mergeEntityDeps(a, b map[EntityID]struct{}) map[EntityID]struct{} {
	if len(b) == 0 {
		return a
	}
	if a == nil {
		a = map[EntityID]struct{}{}
	}
	for id := range b {
		a[id] = struct{}{}
	}
	return a
}

// setViewDependencies replaces the recorded dependencies of one view
// entity in the window's reverse index (entity -> the views whose last
// fresh render accessed it). invalidateEntities marks every view in an
// entity's entry — with its ancestors — dirty when that entity is
// notified. Stale entries are safe: a view no longer rendering marks
// nothing (viewPathReversed finds no node), and a re-render replaces
// its entry.
func setViewDependencies(w *Window, view EntityID, old, newDeps map[EntityID]struct{}) {
	ds := drawState(w)
	if len(old) > 0 {
		for id := range old {
			if _, still := newDeps[id]; still {
				continue
			}
			if views := ds.depViews[id]; views != nil {
				delete(views, view)
				if len(views) == 0 {
					delete(ds.depViews, id)
				}
			}
		}
	}
	for id := range newDeps {
		if ds.depViews == nil {
			ds.depViews = map[EntityID]map[EntityID]struct{}{}
		}
		views := ds.depViews[id]
		if views == nil {
			views = map[EntityID]struct{}{}
			ds.depViews[id] = views
		}
		views[view] = struct{}{}
	}
}

// recordViewGlobalRead tracks one global type read while a window's
// frame runs (the Global/TryGlobal read hook): the frame's accessed
// set publishes to the window at completion, and a notify of the type
// refreshes every window that read it.
func recordViewGlobalRead(app *App, t reflect.Type) {
	w := activeFrame
	if w == nil || w.app != app {
		return
	}
	ds, ok := windowDrawStates[w]
	if !ok || ds.frame == nil || !ds.frame.trackingAccess {
		return
	}
	if ds.frame.accessedGlobals == nil {
		ds.frame.accessedGlobals = map[reflect.Type]struct{}{}
	}
	ds.frame.accessedGlobals[t] = struct{}{}
}

// ---------------------------------------------------------------------------
// The viewElement's automatic-reuse phases
// ---------------------------------------------------------------------------

// retainedViewElementState reads the retained viewElementState of a
// path without recording an access (the request-phase candidate check;
// the prepaint's WithElementState records the access and follows this
// read on every path).
func retainedViewElementState(w *Window, global *GlobalElementID) *viewElementState {
	if global == nil {
		return nil
	}
	ds := drawState(w)
	byType, ok := ds.elementStates[global.key()]
	if !ok {
		return nil
	}
	state, ok := byType[reflect.TypeFor[viewElementState]()]
	if !ok {
		return nil
	}
	typed, ok := state.(*viewElementState)
	if !ok {
		return nil
	}
	return typed
}

// autoReuseRecord is the request-phase candidate check: the retained
// state of a view whose automatic reuse can be attempted this frame —
// the reuse enabled, the view clean (not refreshing, not dirty, no
// drag, no inspector), and the record complete and reproducible. The
// returned state's reuse.nodeStyle is the style the synthesized node
// is requested with; the reuse itself is decided at prepaint against
// the computed bounds (the pin's cache key).
func (v *viewElement) autoReuseRecord(global *GlobalElementID, w *Window) *viewElementState {
	if v.cachedStyle != nil {
		// The explicit CachedView path owns the element.
		return nil
	}
	if _, uncached := v.view.(interface{ noReuse() }); uncached {
		return nil
	}
	if !windowViewReuseEnabled(w) {
		return nil
	}
	ds := drawState(w)
	if ds.refreshing || ds.isViewDirty(*v.entityID) {
		return nil
	}
	if w.IsInspectorPicking() || windowInspectorActive(w) {
		return nil
	}
	if app := w.app; app != nil && app.HasActiveDrag() {
		// gpui-fast's reusable_record checks the drag (GF:fast/
		// retained.rs:587-626): drop targets must re-evaluate.
		return nil
	}
	state := retainedViewElementState(w, global)
	if state == nil || state.reuse == nil || !state.reuse.complete {
		return nil
	}
	if !viewScrollVersionsMatch(state.reuse) {
		// An unnotified programmatic scroll write moved a handle this view
		// read — the clamp and the sticky offsets must re-run (the pin's
		// "the scrollable element clamps it at its next prepaint").
		return nil
	}
	if !viewReuseTableSafe(w, state) {
		// The recorded paint output inserts or references the scene's
		// index-addressed tables (gradients, coverage, filter-op chains):
		// the replay's baked indices are unprovable — render fresh
		// (view_reuse.go viewReuseTableSafe).
		return nil
	}
	return state
}

// viewScrollVersionsMatch reports whether every scroll handle the
// record read still carries its recorded version (browser-core 59,
// view_reuse.go: the programmatic writes' dependency).
func viewScrollVersionsMatch(rec *viewReuseRecord) bool {
	for state, version := range rec.scrolls {
		if state.version != version {
			return false
		}
	}
	return true
}

// viewReuseTableSafe reports whether a view's recorded paint output can
// replay without the scene's index-addressed tables: the native replay
// re-inserts the range's gradient, coverage and filter-op-chain rows at
// NEW table indices (reference/native/src/scene.rs replay → insert_
// gradient / insert_coverage / insert_filter_op_chain) while the copied
// quads, shadows, boundaries and coverage rows keep the table indices
// baked in the previous scene, so a range that inserts or references
// table rows is only correct when both scenes' tables align exactly at
// the range boundary — unprovable at the reuse decision (the new
// scene's counts at the view's paint insertion are not yet known at
// prepaint). The conservative rule: table-carrying ranges render fresh
// (the ticket's "when in doubt, re-render"). Paths are data-carried
// (their ops re-insert with their own data) and Replay advances the
// kernel's counters with the copied rows, so a path insert alone keeps
// the reuse — a coverage row referencing it does not.
//
// The check covers the view's own paint range AND the paint ranges of
// the deferred draws its recorded prepaint range carries: those draws
// replay their ranges at the frame's deferred round, after the reuse
// decision — the refusal must happen here, where the fresh-render
// fallback exists.
func viewReuseTableSafe(w *Window, state *viewElementState) bool {
	if state == nil {
		return false
	}
	ds := drawState(w)
	if ds.lastScene == nil || ds.lastScene.kernel == nil {
		return false
	}
	prev := ds.lastScene.kernel
	if sceneRangeTableRefs(prev, state.paintStart.scene, state.paintEnd.scene) {
		return false
	}
	start, end := state.prepaintStart.deferredDraws, state.prepaintEnd.deferredDraws
	if end > start {
		if end > len(ds.lastDeferredDraws) {
			// The draws list is shorter than the recorded range: drift —
			// unprovable is unsafe.
			return false
		}
		for _, draw := range ds.lastDeferredDraws[start:end] {
			if sceneRangeTableRefs(prev, draw.paintStart.scene, draw.paintEnd.scene) {
				return false
			}
		}
	}
	return true
}

// autoPrepaintStart is the legacy fresh render's prepaint range start:
// the prepaint-phase axes at the prepaint, but the element-state and
// line-layout axes at the REQUEST — the request walk's accesses (the
// div's interactive and transition states, the text shaping) precede
// the prepaint range and must be replayed with it, so the indices span
// both phases (the siblings interleaved between them are over-marked,
// which keeps their states and lines alive — harmless).
func (v *viewElement) autoPrepaintStart(w *Window) prepaintStateIndex {
	start := w.prepaintIndex()
	start.accessedElementStates = v.requestLogStart
	start.lineLayout = v.requestLineLayout
	return start
}

// autoPrepaintFresh is the automatic path's legacy fresh request: the
// element renders in the parent's tree and prepaints plain. The view
// records its reuse state around the prepaint when the content's root
// node style is reproducible (the synthesized request of a later frame
// re-creates the node); otherwise the record is cleared — a view whose
// style stopped being provable must never reuse a stale style.
func (v *viewElement) autoPrepaintFresh(global *GlobalElementID, element *AnyElement, bounds Bounds, w *Window, app *App) AnyElement {
	var nodeStyle Style
	provable := false
	if style, ok := element.obj.rootStretchStyle(); ok {
		nodeStyle, provable = style, viewNodeStyleReproducible(style)
	}
	if !provable {
		// Never reusable in this shape: a plain prepaint, and the record
		// (with its ranges) is dropped.
		element.Prepaint(w, app)
		v.autoRecording = false
		WithElementState(w, global, func(state *viewElementState, w *Window) (struct{}, viewElementState) {
			next := viewElementState{}
			if state != nil {
				setViewDependencies(w, *v.entityID, state.accessedEntities, nil)
			}
			return struct{}{}, next
		})
		return *element
	}
	entityID := *v.entityID
	contentMask := ContentMask(w)
	textStyle := WindowTextStyle(w)
	capture := beginViewPrepaintCapture(w)
	prepaintStart := v.autoPrepaintStart(w)
	mark := trackedLogMark(w)
	element.Prepaint(w, app)
	deps := mergeEntityDeps(v.requestDeps, accessedEntitiesSinceMark(w, mark))
	prepaintEnd := w.prepaintIndex()
	record := finishViewPrepaintCapture(w, capture)
	record.nodeStyle = nodeStyle
	v.autoRecording = true
	WithElementState(w, global, func(state *viewElementState, w *Window) (struct{}, viewElementState) {
		var previous map[EntityID]struct{}
		if state != nil {
			previous = state.accessedEntities
		}
		setViewDependencies(w, entityID, previous, deps)
		return struct{}{}, viewElementState{
			prepaintStart:    prepaintStart,
			prepaintEnd:      prepaintEnd,
			cacheKey:         viewElementCacheKey{bounds: bounds, contentMask: contentMask, textStyle: textStyle},
			accessedEntities: deps,
			reuse:            record,
		}
	})
	return *element
}

// autoPrepaintSynthesized is the automatic path's synthesized request:
// one node at the recorded style is in the parent's tree, and the
// computed bounds arrive here. The key holds (bounds, content mask and
// text style unchanged, the view still clean) — replay the previous
// frame's prepaint range and re-contribute the frame records; the key
// moved — render, lay out as a root at the computed bounds and record,
// exactly the pin's cached miss (R:view.rs:406-485).
func (v *viewElement) autoPrepaintSynthesized(global *GlobalElementID, bounds Bounds, w *Window, app *App) AnyElement {
	if global == nil {
		// No state can be retained: render as a root without recording.
		var rendered AnyElement
		rendered = v.view.RenderOnce(w, app)
		rendered.LayoutAsRoot(AvailableSize{
			Width:  DefiniteAvailableSpace(bounds.Size.Width),
			Height: DefiniteAvailableSpace(bounds.Size.Height),
		}, w, app)
		rendered.PrepaintAt(bounds.Origin, w, app)
		return rendered
	}
	entityID := *v.entityID
	return WithElementState(w, global, func(state *viewElementState, w *Window) (AnyElement, viewElementState) {
		ds := drawState(w)
		contentMask := ContentMask(w)
		textStyle := WindowTextStyle(w)
		if state != nil && state.reuse != nil && state.reuse.complete &&
			state.cacheKey.matches(bounds, contentMask, textStyle) &&
			!ds.isViewDirty(entityID) && !ds.refreshing && windowViewReuseEnabled(w) &&
			viewScrollVersionsMatch(state.reuse) && viewReuseTableSafe(w, state) {
			ds.viewReuseHits++
			reused := *state
			prepaintStart := w.prepaintIndex()
			w.reusePrepaint(state.prepaintStart, state.prepaintEnd)
			extendAccessedEntities(w, state.accessedEntities)
			replayViewPrepaintRecords(w, state.reuse)
			reused.prepaintStart, reused.prepaintEnd = prepaintStart, w.prepaintIndex()
			return AnyElement{}, reused
		}

		ds.viewReuseMisses++
		refreshing := ds.refreshing
		ds.refreshing = true
		prepaintStart := w.prepaintIndex()
		capture := beginViewPrepaintCapture(w)
		mark := trackedLogMark(w)
		var rendered AnyElement
		rendered = v.view.RenderOnce(w, app)
		rendered.LayoutAsRoot(AvailableSize{
			Width:  DefiniteAvailableSpace(bounds.Size.Width),
			Height: DefiniteAvailableSpace(bounds.Size.Height),
		}, w, app)
		rendered.PrepaintAt(bounds.Origin, w, app)
		deps := accessedEntitiesSinceMark(w, mark)
		prepaintEnd := w.prepaintIndex()
		record := finishViewPrepaintCapture(w, capture)
		var previous map[EntityID]struct{}
		if state != nil {
			previous = state.accessedEntities
		}
		// The re-rendered content's root style: normally the synthesized
		// style again (the view's inputs did not change); a style that
		// stopped being reproducible drops the record — this view stops
		// being reusable until a provable render records one again.
		if style, ok := rendered.obj.rootStretchStyle(); ok && viewNodeStyleReproducible(style) {
			record.nodeStyle = style
		} else {
			record = nil
		}
		ds.refreshing = refreshing
		v.autoRecording = record != nil
		v.autoMiss = true
		setViewDependencies(w, entityID, previous, deps)
		return rendered, viewElementState{
			prepaintStart:    prepaintStart,
			prepaintEnd:      prepaintEnd,
			cacheKey:         viewElementCacheKey{bounds: bounds, contentMask: contentMask, textStyle: textStyle},
			accessedEntities: deps,
			reuse:            record,
		}
	})
}

// autoPaintFresh is the automatic path's fresh paint: the miss paints
// with refreshing (mirroring the pin's cached miss, R:view.rs:487-537),
// the legacy paints plain so nested views reuse inside the recorded
// range. The recorded state gains its paint range and the paint-phase
// records — the user-select registrations and the a11y action
// listeners — completing the record for the next frame.
func (v *viewElement) autoPaintFresh(global *GlobalElementID, prepaint *AnyElement, w *Window, app *App) {
	if !v.autoRecording || global == nil {
		if v.autoMiss {
			ds := drawState(w)
			refreshing := ds.refreshing
			ds.refreshing = true
			prepaint.Paint(w, app)
			ds.refreshing = refreshing
			return
		}
		prepaint.Paint(w, app)
		return
	}
	entityID := *v.entityID
	frame := currentFrame(w)
	paintStart := w.paintIndex()
	pendingStart := len(frame.userSelectPending)
	mark := trackedLogMark(w)
	if v.autoMiss {
		ds := drawState(w)
		refreshing := ds.refreshing
		ds.refreshing = true
		prepaint.Paint(w, app)
		ds.refreshing = refreshing
	} else {
		prepaint.Paint(w, app)
	}
	paintDeps := accessedEntitiesSinceMark(w, mark)
	paintEnd := w.paintIndex()
	WithElementState(w, global, func(state *viewElementState, w *Window) (struct{}, viewElementState) {
		if state == nil {
			panic("gpui: automatic view reuse paint without its prepaint state")
		}
		next := *state
		next.paintStart, next.paintEnd = paintStart, paintEnd
		// The paint-phase dependencies join the record's set (the paint
		// reads — the selection model, the image playback state).
		next.accessedEntities = mergeEntityDeps(state.accessedEntities, paintDeps)
		setViewDependencies(w, entityID, state.accessedEntities, next.accessedEntities)
		if next.reuse != nil {
			if end := len(frame.userSelectPending); end > pendingStart {
				next.reuse.userSelect = append([]userSelectRegistration(nil), frame.userSelectPending[pendingStart:end]...)
			}
			next.reuse.userSelectSlotTo = frame.userSelectSlot
			captureViewA11yListeners(w, next.reuse)
			next.reuse.complete = true
		}
		return struct{}{}, next
	})
}

// autoPaintReused is the automatic path's reuse hit: replay the
// previous frame's paint range and re-contribute the paint-phase
// records (the user-select registrations and the a11y action
// listeners), recording the new range.
func (v *viewElement) autoPaintReused(global *GlobalElementID, w *Window) {
	WithElementState(w, global, func(state *viewElementState, w *Window) (struct{}, viewElementState) {
		if state == nil {
			panic("gpui: automatic view reuse paint without its prepaint state")
		}
		next := *state
		paintStart := w.paintIndex()
		w.reusePaint(state.paintStart, state.paintEnd)
		replayViewPaintRecords(w, state.reuse)
		next.paintStart, next.paintEnd = paintStart, w.paintIndex()
		return struct{}{}, next
	})
}
