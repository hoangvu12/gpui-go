package gpui

// This file is the variable-height list slice of ticket14: the List
// (elements/list.rs) — the virtualized list over per-item heights. A
// view-held ListState keeps the measured item sizes, the logical scroll
// top (item index + pixel offset into the item), the last layout
// bounds/padding, the reset flag and the scroll-handler hook; the List
// element renders only the visible range through the per-item render
// callback, lays each item out as a standalone root (definite width,
// min-content height), prepaints and paints it at its scrolled origin
// under the intersected content mask, and writes the measured sizes
// back into the state — the remeasure feedback. Splice adjusts the
// logical scroll top so content inserted or removed above the scroll
// position preserves the same pixel offset into the anchor item, and
// clamps the anchor to the range start when the anchor item itself is
// removed. Items are stamped with per-index element ids so keyed
// element state under items is stable per item while it keeps
// rendering.
//
// Base ticket 07 adds the wheel: the list inserts a hitbox at prepaint
// and, at paint and before its items, registers the bubble wheel
// listener that coalesces the frame's deltas, converts lines at 20px and
// scrolls EAGERLY through StateInner::scroll — clamped, handler-firing,
// notifying the view (R:elements/list.rs:1551, 1614-1639, 898-960) —
// plus the scrollbar plumbing (R:list.rs:336-339 measure_all,
// 743-796, 878-884, 985-1025, 1372-1419) and the remeasure size hints
// (R:list.rs:457-474, 1667-1698).
//
// Bounded deviations from the pin (recorded): the SumTree of ListItem
// is a Go slice with O(n) cumulative-height walks (identical
// observables at these sizes); ListAlignment is Top only (the
// Bottom/chat alignment, follow-tail mode, with_uniform_item_height,
// the proportional remeasure anchor and the per-item focus-handle
// retention for off-screen focused items are omitted — no consumers in
// this slice); ListState.ScrollBy keeps the
// port's clamped, handler-firing scroll body for its programmatic
// callers (the pinned scroll_by, R:list.rs:565-595, is unclamped above
// and handler-less; the wheel path uses the pinned scroll body); the
// scroll mask is the bounds intersection (the uniform-list precedent;
// the reference routes it through the style's overflow).
//
// Base ticket 37 adds the autoscroll request path (R:list.rs:1250-1361,
// 1571-1597): an item that calls Window.RequestAutoscroll during its
// prepaint makes the list discard the frame's item prepaint (the pinned
// Window::transact, gpui/window.go transact), move its scroll top so the
// requested bounds show, and prepaint the items again there. Base's
// TextView uses it to reveal a line inside a long block.
//
// R: = reference/ce-source/crates/gpui/src (pin 254b5dbd).

import (
	"fmt"
)

// ---------------------------------------------------------------------------
// The scroll position and the scroll event (list.rs ListOffset /
// ListScrollEvent)
// ---------------------------------------------------------------------------

// ListOffset is an offset into the list's items: the item index and the
// pixels to offset from the top of that item (the reference ListOffset).
type ListOffset struct {
	// ItemIx is the index of the item the scroll top sits in.
	ItemIx int
	// OffsetInItem is the pixel distance from that item's top.
	OffsetInItem float32
}

// ListScrollEvent is a scroll event in terms of the list's items (the
// reference ListScrollEvent; the item Range is the start/end pair).
type ListScrollEvent struct {
	// VisibleRangeStart and VisibleRangeEnd are the half-open visible
	// item range after applying the scroll event.
	VisibleRangeStart int
	VisibleRangeEnd   int
	// Count is the number of items in the list.
	Count int
	// IsScrolled reports whether an explicit scroll position is set.
	IsScrolled bool
}

// ---------------------------------------------------------------------------
// The view-held list state (list.rs ListState / StateInner)
// ---------------------------------------------------------------------------

// listItemState is one item's retained measurement (the reference
// ListItem; bounded: no focus handles).
type listItemState struct {
	// measured reports whether size holds a real measurement (the
	// pinned ListItem::Measured).
	measured bool
	// size is the item's measured size (valid when measured).
	size Size
	// hint is an unmeasured item's size hint (ListItem::Unmeasured's
	// size_hint): the size it had when RemeasureItems invalidated it. It
	// counts toward the summary height until the item is re-rendered
	// (R:list.rs:1672-1686).
	hint *Size
}

// sizeHint is ListItem::size_hint: the measured size, else the hint.
func (s listItemState) sizeHint() *Size {
	if s.measured {
		size := s.size
		return &size
	}
	return s.hint
}

// listPadding is the resolved vertical padding the list math uses (the
// reference Edges<Pixels>: only the vertical edges enter the layout
// walk, the scroll clamp and the reveal placement).
type listPadding struct {
	// Top offsets the first item below the list's top edge.
	Top float32
	// Bottom extends the rendered content height.
	Bottom float32
}

// listPendingScroll is the reference PendingScroll::Absolute: preserve
// the same pixel offset into the scroll-top item across a remeasure.
// (The Proportional variant rides the omitted remeasure-all path.)
type listPendingScroll struct {
	itemIx int
	offset float32
}

// listStateInner is the ListState's shared state (the reference
// StateInner, bounded).
type listStateInner struct {
	// lastLayoutBounds is the list element's last prepainted bounds.
	lastLayoutBounds *Bounds
	// lastPadding is the last resolved padding.
	lastPadding *listPadding
	// items holds the per-item measurements (the SumTree replacement).
	items []listItemState
	// logicalScrollTop is the scroll position in item terms; nil means
	// no explicit position — the Top-alignment default {0, 0}.
	logicalScrollTop *ListOffset
	// overdraw is the extra space rendered above and below the visible
	// area.
	overdraw float32
	// reset drops scroll events until the next prepaint.
	reset bool
	// scrollHandler observes scrolls (set_scroll_handler).
	scrollHandler func(*ListScrollEvent, *Window, *App)
	// pendingScroll is the deferred remeasure anchor.
	pendingScroll *listPendingScroll
	// scrollbarDragStartHeight freezes the content height a scrollbar
	// drag maps against (R:list.rs:72).
	scrollbarDragStartHeight *float32
	// measureAll and hasMeasured are ListMeasuringBehavior::Measure(bool)
	// (R:list.rs:196-214); measureAll false is the Visible default.
	measureAll  bool
	hasMeasured bool
}

// resetMeasuring is ListMeasuringBehavior::reset (R:list.rs:207-213).
func (st *listStateInner) resetMeasuring() { st.hasMeasured = false }

// ListState is the list state a view holds on behalf of the List
// element (the reference ListState: "this element's state is stored
// intrusively on your own views"). Construct it once, keep it in the
// view, and pass it to every List element the view renders.
type ListState struct {
	inner *listStateInner
}

// NewListState constructs the list state for itemCount items with the
// given overdraw in logical pixels (ListState::new; bounded: the
// alignment is Top).
func NewListState(itemCount int, overdraw float32) *ListState {
	if itemCount < 0 {
		itemCount = 0
	}
	return &ListState{inner: &listStateInner{
		items:    make([]listItemState, itemCount),
		overdraw: overdraw,
	}}
}

// ItemCount returns the number of items in the list (ListState::
// item_count).
func (s *ListState) ItemCount() int { return len(s.inner.items) }

// LogicalScrollTop returns the current scroll offset in terms of the
// list's items (ListState::logical_scroll_top; the Top-alignment
// default when no position was set).
func (s *ListState) LogicalScrollTop() ListOffset {
	return s.inner.logicalScrollTopValue()
}

// SetScrollHandler sets the handler called when the list is scrolled
// (ListState::set_scroll_handler). The window and application are
// passed through to the handler; both may be nil when the handler does
// not use them.
func (s *ListState) SetScrollHandler(handler func(*ListScrollEvent, *Window, *App)) {
	s.inner.scrollHandler = handler
}

// Reset resets the list state to elementCount fresh items, dropping the
// scroll position and any pending scroll adjustment (ListState::reset;
// scroll events are dropped until the next paint).
func (s *ListState) Reset(elementCount int) {
	st := s.inner
	st.reset = true
	st.resetMeasuring()
	st.logicalScrollTop = nil
	st.pendingScroll = nil
	st.scrollbarDragStartHeight = nil
	if elementCount < 0 {
		elementCount = 0
	}
	st.items = make([]listItemState, elementCount)
}

// MeasureAll makes the list measure every item in its first layout
// phase, so scrollbar metrics cover the whole content rather than the
// rendered items (R:list.rs:333-339 measure_all). It returns the state
// for chaining.
func (s *ListState) MeasureAll() *ListState {
	s.inner.measureAll = true
	s.inner.hasMeasured = false
	return s
}

// Splice informs the list state that the items in [oldStart, oldEnd)
// have been replaced by count new unmeasured items (ListState::splice).
// The scroll anchor is preserved: content inserted or removed entirely
// above the scroll position shifts the anchor's index while keeping
// its pixel offset into the anchor item, so the view does not jump; a
// splice that removes the anchor item itself clamps the anchor to the
// range start.
func (s *ListState) Splice(oldStart, oldEnd, count int) {
	st := s.inner
	if oldStart < 0 {
		oldStart = 0
	}
	if oldEnd < oldStart {
		oldEnd = oldStart
	}
	if oldStart > len(st.items) {
		oldStart = len(st.items)
	}
	if oldEnd > len(st.items) {
		oldEnd = len(st.items)
	}
	if count < 0 {
		count = 0
	}
	items := make([]listItemState, 0, len(st.items)-(oldEnd-oldStart)+count)
	items = append(items, st.items[:oldStart]...)
	items = append(items, make([]listItemState, count)...)
	items = append(items, st.items[oldEnd:]...)
	st.items = items
	if top := st.logicalScrollTop; top != nil {
		if oldStart <= top.ItemIx && top.ItemIx < oldEnd {
			top.ItemIx = oldStart
			top.OffsetInItem = 0
		} else if oldEnd <= top.ItemIx {
			top.ItemIx = top.ItemIx - (oldEnd - oldStart) + count
		}
	}
}

// RemeasureItems marks the items in [start, end) as needing remeasure-
// ment while preserving the current scroll position through the
// absolute anchor: the same pixel offset into the scroll-top item is
// restored after the item re-renders at its new height (ListState::
// remeasure_items, R:list.rs:412-475): the invalidated items keep their
// last size as a hint, so the content height and the pixel scroll math
// stay stable until they re-render, and a measure-all list measures
// again.
func (s *ListState) RemeasureItems(start, end int) {
	st := s.inner
	if start < 0 {
		start = 0
	}
	if end > len(st.items) {
		end = len(st.items)
	}
	if top := st.logicalScrollTop; top != nil && start <= top.ItemIx && top.ItemIx < end {
		st.pendingScroll = &listPendingScroll{itemIx: top.ItemIx, offset: top.OffsetInItem}
	}
	for ix := start; ix < end; ix++ {
		st.items[ix] = listItemState{hint: st.items[ix].sizeHint()}
	}
	st.resetMeasuring()
}

// ScrollTo scrolls the list to the given offset in item terms
// (ListState::scroll_to; the item index clamps to the item count).
func (s *ListState) ScrollTo(top ListOffset) {
	st := s.inner
	if top.ItemIx >= len(st.items) {
		top.ItemIx = len(st.items)
		top.OffsetInItem = 0
	}
	if top.ItemIx < 0 {
		top.ItemIx = 0
	}
	st.rebasePendingScroll(top)
	st.logicalScrollTop = &top
}

// ScrollToRevealItem scrolls the list so the given item is fully
// visible (ListState::scroll_to_reveal_item): items at or above the
// scroll top move to the viewport top; items below are placed with
// their bottom at the viewport bottom.
func (s *ListState) ScrollToRevealItem(ix int) {
	st := s.inner
	scrollTop := s.LogicalScrollTop()
	height := float32(0)
	if bounds := st.lastLayoutBounds; bounds != nil {
		height = bounds.Size.Height
	}
	padding := listPadding{}
	if last := st.lastPadding; last != nil {
		padding = *last
	}
	if ix <= scrollTop.ItemIx {
		scrollTop.ItemIx = ix
		scrollTop.OffsetInItem = 0
	} else {
		bottom := st.heightBefore(ix+1) + padding.Top
		goalTop := bottom - height + padding.Bottom
		if goalTop < 0 {
			goalTop = 0
		}
		startIx, startItemTop := st.seekHeightLeft(goalTop)
		if startIx >= scrollTop.ItemIx {
			scrollTop.ItemIx = startIx
			scrollTop.OffsetInItem = goalTop - startItemTop
		}
	}
	st.rebasePendingScroll(scrollTop)
	st.logicalScrollTop = &scrollTop
}

// ScrollBy scrolls the list by the given pixel distance (positive
// scrolls down) through the reference's scroll-delta path: the new
// pixel offset clamps to [0, content - viewport], the logical scroll
// top is rebased onto the item containing it, and the registered
// scroll handler observes the resulting visible range. The window and
// application are only forwarded to the handler; both may be nil when
// no handler is registered or the handler does not use them. Scroll
// events are dropped after a Reset until the next paint (the pinned
// reset guard).
func (s *ListState) ScrollBy(distance float32, w *Window, app *App) {
	st := s.inner
	if st.reset {
		return
	}
	height := float32(0)
	if bounds := st.lastLayoutBounds; bounds != nil {
		height = bounds.Size.Height
	}
	padding := listPadding{}
	if last := st.lastPadding; last != nil {
		padding = *last
	}
	scrollMax := st.totalHeight() + padding.Top + padding.Bottom - height
	if scrollMax < 0 {
		scrollMax = 0
	}
	newScrollTop := st.pixelScrollTop(st.logicalScrollTopValue()) + distance
	if newScrollTop < 0 {
		newScrollTop = 0
	}
	if newScrollTop > scrollMax {
		newScrollTop = scrollMax
	}
	ix, heightAtIx := st.seekHeightRight(newScrollTop)
	top := ListOffset{ItemIx: ix, OffsetInItem: newScrollTop - heightAtIx}
	if top.OffsetInItem < 0 {
		top.OffsetInItem = 0
	}
	st.rebasePendingScroll(top)
	st.logicalScrollTop = &top
	if handler := st.scrollHandler; handler != nil {
		start, end := st.visibleRange(height, top)
		handler(&ListScrollEvent{
			VisibleRangeStart: start,
			VisibleRangeEnd:   end,
			Count:             len(st.items),
			IsScrolled:        st.logicalScrollTop != nil,
		}, w, app)
	}
}

// ViewportBounds returns the bounds of the list's viewport in pixels
// (ListState::viewport_bounds; zero before the first layout).
func (s *ListState) ViewportBounds() Bounds {
	if bounds := s.inner.lastLayoutBounds; bounds != nil {
		return *bounds
	}
	return Bounds{}
}

// MaxOffsetForScrollbar returns the maximum scroll offset according to
// the measured items; it stays constant during a scrollbar drag
// (R:list.rs:770-775 max_offset_for_scrollbar over 878-884
// max_scroll_offset).
func (s *ListState) MaxOffsetForScrollbar() Point {
	return Point{X: 0, Y: s.inner.maxScrollOffset()}
}

// maxScrollOffset is R:list.rs:878-884: the drag-frozen (else live)
// content height minus the viewport height, at least zero.
func (st *listStateInner) maxScrollOffset() float32 {
	height := float32(0)
	if bounds := st.lastLayoutBounds; bounds != nil {
		height = bounds.Size.Height
	}
	content := st.totalHeight()
	if st.scrollbarDragStartHeight != nil {
		content = *st.scrollbarDragStartHeight
	}
	return max32(content-height, 0)
}

// ScrollbarDragStarted freezes the content height reported to the
// scrollbar while the user drags it, so measuring overdraw items does not
// move the thumb (R:list.rs:739-746 scrollbar_drag_started).
func (s *ListState) ScrollbarDragStarted() {
	height := s.inner.totalHeight()
	s.inner.scrollbarDragStartHeight = &height
}

// ScrollbarDragEnded ends a scrollbar drag (R:list.rs:748-753).
func (s *ListState) ScrollbarDragEnded() { s.inner.scrollbarDragStartHeight = nil }

// IsScrollbarDragging reports whether a scrollbar drag is in progress
// (R:list.rs:755-763).
func (s *ListState) IsScrollbarDragging() bool { return s.inner.scrollbarDragStartHeight != nil }

// SetOffsetFromScrollbar scrolls to a scrollbar position: point.Y is the
// negated pixel scroll top, mapped against the drag-frozen content
// height and clamped into the scroll range (R:list.rs:765-768, 1372-1419;
// the follow-tail arm is not ported). A list that has not laid out yet
// ignores it.
func (s *ListState) SetOffsetFromScrollbar(point Point) {
	st := s.inner
	bounds := st.lastLayoutBounds
	if bounds == nil {
		return
	}
	padding := listPadding{}
	if last := st.lastPadding; last != nil {
		padding = *last
	}
	content := st.totalHeight()
	if st.scrollbarDragStartHeight != nil {
		content = *st.scrollbarDragStartHeight
	}
	scrollMax := max32(content+padding.Top+padding.Bottom-bounds.Size.Height, 0)
	newScrollTop := min32(max32(-point.Y, 0), scrollMax)
	ix, start := st.seekHeightRight(newScrollTop)
	top := ListOffset{ItemIx: ix, OffsetInItem: newScrollTop - start}
	st.rebasePendingScroll(top)
	st.logicalScrollTop = &top
}

// ScrollPxOffsetForScrollbar returns the current scroll offset for the
// scrollbar: y is the negated pixel scroll top (R:list.rs:777-796; Top
// alignment).
func (s *ListState) ScrollPxOffsetForScrollbar() Point {
	st := s.inner
	top := st.logicalScrollTopValue()
	return Point{X: 0, Y: -(st.heightBefore(top.ItemIx) + top.OffsetInItem)}
}

// BoundsForItem returns the bounds of item ix in window coordinates
// when it has been measured (ListState::bounds_for_item): the full
// list width at the item's scrolled position. Items above the logical
// scroll top report no bounds.
func (s *ListState) BoundsForItem(ix int) (Bounds, bool) {
	st := s.inner
	bounds := Bounds{}
	if last := st.lastLayoutBounds; last != nil {
		bounds = *last
	}
	scrollTop := st.logicalScrollTopValue()
	if ix < 0 || ix >= len(st.items) || !st.items[ix].measured {
		return Bounds{}, false
	}
	if ix < scrollTop.ItemIx {
		return Bounds{}, false
	}
	pixelTop := st.pixelScrollTop(scrollTop)
	top := bounds.Top() + st.heightBefore(ix) - pixelTop
	return Bounds{
		Origin: Point{X: bounds.Left(), Y: top},
		Size:   Size{Width: bounds.Size.Width, Height: st.items[ix].size.Height},
	}, true
}

// logicalScrollTopValue returns the effective logical scroll top (the
// reference StateInner::logical_scroll_top's Top-alignment default).
func (st *listStateInner) logicalScrollTopValue() ListOffset {
	if st.logicalScrollTop != nil {
		return *st.logicalScrollTop
	}
	return ListOffset{}
}

// heightBefore returns the cumulative measured height above item ix
// (the SumTree's Height dimension seek start; O(ix) — the bounded
// replacement).
func (st *listStateInner) heightBefore(ix int) float32 {
	if ix > len(st.items) {
		ix = len(st.items)
	}
	sum := float32(0)
	for i := 0; i < ix; i++ {
		sum += st.itemHeight(i)
	}
	return sum
}

// totalHeight returns the summary height (R:list.rs:1667-1698: measured
// heights plus the unmeasured items' hints; unhinted unmeasured items
// contribute zero).
func (st *listStateInner) totalHeight() float32 { return st.heightBefore(len(st.items)) }

// itemHeight returns one item's height contribution to the summary
// (the measured height, else the size hint, else zero).
func (st *listStateInner) itemHeight(ix int) float32 {
	if st.items[ix].measured {
		return st.items[ix].size.Height
	}
	if hint := st.items[ix].hint; hint != nil {
		return hint.Height
	}
	return 0
}

// scroll is R:list.rs:898-960 StateInner::scroll, the wheel body: events
// after a reset are dropped; the new pixel scroll top is the frame's
// scroll top minus the delta, clamped into [0, content + padding -
// height]; the logical scroll top is rebased onto the item containing it
// (re-anchoring a pending remeasure); the scroll handler observes the
// visible range of the frame's scroll top (the pinned argument); the
// view is notified.
func (st *listStateInner) scroll(scrollTop ListOffset, height float32, delta Point, notify func(*Window, *App), w *Window, app *App) {
	if st.reset {
		return
	}
	padding := listPadding{}
	if last := st.lastPadding; last != nil {
		padding = *last
	}
	scrollMax := max32(st.totalHeight()+padding.Top+padding.Bottom-height, 0)
	newScrollTop := min32(max32(st.pixelScrollTop(scrollTop)-delta.Y, 0), scrollMax)
	ix, start := st.seekHeightRight(newScrollTop)
	top := ListOffset{ItemIx: ix, OffsetInItem: newScrollTop - start}
	st.rebasePendingScroll(top)
	st.logicalScrollTop = &top
	if handler := st.scrollHandler; handler != nil {
		rangeStart, rangeEnd := st.visibleRange(height, scrollTop)
		handler(&ListScrollEvent{
			VisibleRangeStart: rangeStart,
			VisibleRangeEnd:   rangeEnd,
			Count:             len(st.items),
			IsScrolled:        st.logicalScrollTop != nil,
		}, w, app)
	}
	if notify != nil {
		notify(w, app)
	}
}

// layoutAllItems is R:list.rs:985-1025: under the measure-all behavior,
// once per measuring cycle, every unmeasured item renders and is laid out
// at the list width; measured items keep their size.
func (st *listStateInner) layoutAllItems(availableWidth float32, renderItem func(int, *Window, *App) AnyElement, w *Window, app *App) {
	if !st.measureAll || st.hasMeasured {
		return
	}
	st.hasMeasured = true
	avail := AvailableSize{Width: DefiniteAvailableSpace(availableWidth), Height: MinContentAvailableSpace()}
	for ix := range st.items {
		if st.items[ix].measured {
			continue
		}
		element := wrapListItem(ix, renderItem(ix, w, app))
		size := element.LayoutAsRoot(avail, w, app)
		st.items[ix] = listItemState{measured: true, size: size}
	}
}

// seekHeightRight seeks to the item containing the pixel offset
// target (the reference find(Height(target), Bias::Right): boundary
// ties go right). It returns the item index and the cumulative height
// above it.
func (st *listStateInner) seekHeightRight(target float32) (int, float32) {
	sum := float32(0)
	for ix := 0; ix < len(st.items); ix++ {
		if sum+st.itemHeight(ix) > target {
			return ix, sum
		}
		sum += st.itemHeight(ix)
	}
	return len(st.items), sum
}

// seekHeightLeft seeks to the item containing the pixel offset target
// with boundary ties going left (the reference Bias::Left): the item
// whose end reaches the target. It returns the item index and the
// cumulative height above it.
func (st *listStateInner) seekHeightLeft(target float32) (int, float32) {
	sum := float32(0)
	for ix := 0; ix < len(st.items); ix++ {
		if sum+st.itemHeight(ix) >= target {
			return ix, sum
		}
		sum += st.itemHeight(ix)
	}
	return len(st.items), sum
}

// pixelScrollTop returns the pixel offset of the logical scroll top
// (StateInner::scroll_top).
func (st *listStateInner) pixelScrollTop(top ListOffset) float32 {
	return st.heightBefore(top.ItemIx) + top.OffsetInItem
}

// visibleRange returns the visible item range for the given viewport
// height and scroll top (StateInner::visible_range: the range extends
// through the item the viewport bottom edge lands on).
func (st *listStateInner) visibleRange(height float32, scrollTop ListOffset) (int, int) {
	startY := st.pixelScrollTop(scrollTop)
	ix, _ := st.seekHeightLeft(startY + height)
	return scrollTop.ItemIx, ix + 1
}

// rebasePendingScroll re-anchors a pending remeasure adjustment onto a
// newly set scroll position so it clamps to the remeasured item's new
// height at the next layout instead of reverting the scroll
// (StateInner::rebase_pending_scroll).
func (st *listStateInner) rebasePendingScroll(scrollTop ListOffset) {
	pending := st.pendingScroll
	if pending == nil {
		return
	}
	st.pendingScroll = nil
	if scrollTop.ItemIx >= len(st.items) {
		return
	}
	st.pendingScroll = &listPendingScroll{itemIx: scrollTop.ItemIx, offset: scrollTop.OffsetInItem}
}

// ---------------------------------------------------------------------------
// The layout walk (list.rs StateInner::layout_items / prepaint_items)
// ---------------------------------------------------------------------------

// listItemLayout is one rendered visible item (the reference
// ItemLayout).
type listItemLayout struct {
	index   int
	element AnyElement
	size    Size
}

// listLayoutItemsResponse is the reference LayoutItemsResponse.
type listLayoutItemsResponse struct {
	// maxItemWidth is the widest measured item (the Infer sizing's
	// max-content width).
	maxItemWidth float32
	// scrollTop is the effective scroll top after the walk.
	scrollTop ListOffset
	// itemLayouts are the visible items in document order.
	itemLayouts []listItemLayout
}

// layoutItems renders and measures the items around the scroll
// position (StateInner::layout_items): the downward walk from the
// scroll top (visible items plus the trailing overdraw), the upward
// fill when the content below the scroll position does not fill the
// viewport (the layout-time offset clamp: the content bottom aligns
// with the viewport bottom, resting at the list top when everything
// fits), and the leading-overdraw measurement above the scroll top.
// Measured sizes are written back into the item states — the
// per-item-height remeasure feedback.
func (st *listStateInner) layoutItems(availableWidth *float32, availableHeight float32, padding listPadding, renderItem func(int, *Window, *App) AnyElement, w *Window, app *App) listLayoutItemsResponse {
	avail := AvailableSize{
		Width:  MaxContentAvailableSpace(),
		Height: MinContentAvailableSpace(),
	}
	if availableWidth != nil {
		avail.Width = DefiniteAvailableSpace(*availableWidth)
	}

	var itemLayouts []listItemLayout
	var upwardLayouts []listItemLayout
	renderedHeight := padding.Top
	maxItemWidth := float32(0)
	scrollTop := st.logicalScrollTopValue()
	first := scrollTop.ItemIx
	if first > len(st.items) {
		first = len(st.items)
	}

	// Render items after the scroll top, including the trailing
	// overdraw; use the cached size of measured items outside the
	// visible region.
	for ix := first; ix < len(st.items); ix++ {
		visibleHeight := renderedHeight - scrollTop.OffsetInItem
		if visibleHeight >= availableHeight+st.overdraw {
			break
		}
		item := st.items[ix]
		size := item.size
		if !item.measured || visibleHeight < availableHeight {
			element := wrapListItem(ix, renderItem(ix, w, app))
			elementSize := element.LayoutAsRoot(avail, w, app)
			size = elementSize
			// A pending remeasure anchor on the scroll-top item:
			// restore the stashed pixel offset into it, clamped to its
			// new height.
			if ix == first {
				if pending := st.pendingScroll; pending != nil {
					st.pendingScroll = nil
					if pending.itemIx == scrollTop.ItemIx {
						scrollTop.OffsetInItem = min32(pending.offset, elementSize.Height)
						st.logicalScrollTop = &scrollTop
					}
				}
			}
			if visibleHeight < availableHeight {
				itemLayouts = append(itemLayouts, listItemLayout{index: ix, element: element, size: elementSize})
			}
		}
		renderedHeight += size.Height
		maxItemWidth = max32(maxItemWidth, size.Width)
		st.items[ix] = listItemState{measured: true, size: size}
	}
	renderedHeight += padding.Bottom

	// If the rendered items do not fill the visible region, walk upward
	// from the scroll top prepending items until they do, then rest the
	// scroll top at the topmost item with the offset that puts the
	// content bottom on the viewport bottom.
	if renderedHeight-scrollTop.OffsetInItem < availableHeight {
		cursorIx := first
		for renderedHeight < availableHeight {
			if cursorIx <= 0 {
				break
			}
			cursorIx--
			element := wrapListItem(cursorIx, renderItem(cursorIx, w, app))
			elementSize := element.LayoutAsRoot(avail, w, app)
			st.items[cursorIx] = listItemState{measured: true, size: elementSize}
			renderedHeight += elementSize.Height
			upwardLayouts = append(upwardLayouts, listItemLayout{index: cursorIx, element: element, size: elementSize})
		}
		scrollTop = ListOffset{ItemIx: cursorIx, OffsetInItem: renderedHeight - availableHeight}
		if scrollTop.OffsetInItem < 0 {
			scrollTop.OffsetInItem = 0
		}
		st.logicalScrollTop = &scrollTop
		for i, j := 0, len(upwardLayouts)-1; i < j; i, j = i+1, j-1 {
			upwardLayouts[i], upwardLayouts[j] = upwardLayouts[j], upwardLayouts[i]
		}
		itemLayouts = append(upwardLayouts, itemLayouts...)
	}

	// Measure items in the leading overdraw above the scroll top
	// (heights only; the elements are discarded after measurement).
	leadingOverdraw := scrollTop.OffsetInItem
	cursorIx := scrollTop.ItemIx
	for leadingOverdraw < st.overdraw {
		if cursorIx <= 0 {
			break
		}
		cursorIx--
		if item := st.items[cursorIx]; !item.measured {
			element := wrapListItem(cursorIx, renderItem(cursorIx, w, app))
			size := element.LayoutAsRoot(avail, w, app)
			st.items[cursorIx] = listItemState{measured: true, size: size}
			leadingOverdraw += size.Height
		} else {
			leadingOverdraw += item.size.Height
		}
	}

	return listLayoutItemsResponse{maxItemWidth: maxItemWidth, scrollTop: scrollTop, itemLayouts: itemLayouts}
}

// resolveListPadding resolves the style's vertical padding against the
// reference size and rem size (the reference Edges::to_pixels).
func resolveListPadding(style Style, base Size, rem float32) listPadding {
	return listPadding{
		Top:    resolveDefiniteListLength(style.Padding.Top, base.Height, rem),
		Bottom: resolveDefiniteListLength(style.Padding.Bottom, base.Height, rem),
	}
}

// resolveDefiniteListLength resolves one definite padding edge to
// logical pixels: pixels pass through, rems multiply the rem size, and
// fractions resolve against the given base extent.
func resolveDefiniteListLength(length DefiniteLength, base, rem float32) float32 {
	switch length.Kind {
	case DefiniteLengthPx:
		return length.Value
	case DefiniteLengthRem:
		return length.Value * rem
	default:
		return length.Value * base
	}
}

// ---------------------------------------------------------------------------
// The per-index item identity (the port's stable item keying)
// ---------------------------------------------------------------------------

// listItemIdElement wraps one item element with its per-index element
// id (the port's stable item identity: keyed element state under list
// items is scoped by the item's index while the item keeps rendering,
// and the frame-end retention drops it when the item leaves the range;
// the reference instead relies on the callback's elements carrying
// their own ids).
type listItemIdElement struct {
	ix    int
	inner AnyElement
}

// ID implements Element: the per-index identity.
func (e *listItemIdElement) ID() (ElementID, bool) {
	return IntegerElementID(uint64(e.ix)), true
}

// SourceLocation implements Element.
func (e *listItemIdElement) SourceLocation() *SourceLocation { return nil }

// RequestLayout implements Element: the wrapper takes the inner item's
// layout node; the id stamped by this element's own identity flows
// into the inner subtree's global ids.
func (e *listItemIdElement) RequestLayout(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App) (LayoutID, struct{}) {
	return e.inner.RequestLayout(w, app), struct{}{}
}

// Prepaint implements Element: delegate to the inner item at the
// committed bounds.
func (e *listItemIdElement) Prepaint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *struct{}, w *Window, app *App) struct{} {
	e.inner.Prepaint(w, app)
	return struct{}{}
}

// Paint implements Element: delegate to the inner item.
func (e *listItemIdElement) Paint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *struct{}, prepaint *struct{}, w *Window, app *App) {
	e.inner.Paint(w, app)
}

// wrapListItem stamps the per-index element id onto one rendered item.
func wrapListItem(ix int, element AnyElement) AnyElement {
	return CustomElement[struct{}, struct{}](&listItemIdElement{ix: ix, inner: element})
}

// ---------------------------------------------------------------------------
// The List element (list.rs List)
// ---------------------------------------------------------------------------

// ListElement is the variable-height list element (the reference
// List): the items of the view-held ListState rendered through the
// per-item callback for the visible range only.
type ListElement struct {
	// state is the view-held list state.
	state *ListState
	// renderItem builds one item's element.
	renderItem func(ix int, w *Window, app *App) AnyElement
	// style is the element's layout style.
	style Style
	// sizing selects the sizing behavior.
	sizing ListSizingBehavior
}

// List constructs a variable-height list over the view-held state with
// the per-item render callback (the reference list()). The sizing
// behavior defaults to ListAuto, the reference List's default: the
// parent's layout sizes the list node.
func List(state *ListState, renderItem func(ix int, w *Window, app *App) AnyElement) *ListElement {
	if state == nil {
		panic("gpui: List requires a list state")
	}
	if renderItem == nil {
		panic("gpui: List requires a non-nil item render callback")
	}
	return &ListElement{
		state:      state,
		renderItem: renderItem,
		style:      DefaultStyle(),
		sizing:     ListAuto,
	}
}

// WithSizingBehavior sets the sizing behavior (List::with_sizing_
// behavior); ListInfer sizes the list to min(content, available).
func (l *ListElement) WithSizingBehavior(behavior ListSizingBehavior) *ListElement {
	l.sizing = behavior
	return l
}

// W sets the width style.
func (l *ListElement) W(width Length) *ListElement {
	l.style.Size.Width = width
	return l
}

// H sets the height style.
func (l *ListElement) H(height Length) *ListElement {
	l.style.Size.Height = height
	return l
}

// P sets the padding on all four edges (Styled::p; the vertical edges
// enter the list math).
func (l *ListElement) P(padding DefiniteLength) *ListElement {
	l.style.Padding = DefiniteLengthEdges{Top: padding, Right: padding, Bottom: padding, Left: padding}
	return l
}

// SizeFull stretches the list to its parent's size.
func (l *ListElement) SizeFull() *ListElement {
	l.style.Size = LengthSize{Width: DefiniteLengthOf(Fraction(1.0)), Height: DefiniteLengthOf(Fraction(1.0))}
	return l
}

// ID implements Element: lists are anonymous — the state is view-held,
// not element-state-keyed (the pinned id() -> None).
func (l *ListElement) ID() (ElementID, bool) { return ElementID{}, false }

// SourceLocation implements Element.
func (l *ListElement) SourceLocation() *SourceLocation { return nil }

// listItemPlacement is one prepainted item with its origin and size.
type listItemPlacement struct {
	element AnyElement
	origin  Point
	size    Size
}

// listPrepaintState is the frame state built by the list's prepaint
// (the reference ListPrepaintState, bounded to the item layouts).
type listPrepaintState struct {
	// items are this frame's visible items in document order.
	items []listItemPlacement
	// hitbox is the list's hitbox (R:list.rs:1551).
	hitbox *Hitbox
	// scrollTop is the layout walk's effective scroll top, the base the
	// frame's wheel deltas apply to (R:list.rs:1623).
	scrollTop ListOffset
}

// RequestLayout implements Element: the Infer sizing measures its
// items at request time (the pinned layout_items call under max-
// content width, with the last bounds height — or the overdraw on the
// first render — as the available height) and requests a measured node
// of min(content, available) height; the Auto sizing requests a plain
// node.
func (l *ListElement) RequestLayout(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App) (LayoutID, struct{}) {
	st := l.state.inner
	var layoutID LayoutID
	var err error
	if l.sizing == ListInfer {
		availableHeight := st.overdraw
		base := Size{}
		if bounds := st.lastLayoutBounds; bounds != nil {
			availableHeight = bounds.Size.Height
			base = bounds.Size
		}
		padding := resolveListPadding(l.style, base, RemSize(w))
		response := st.layoutItems(nil, availableHeight, padding, l.renderItem, w, app)
		maxItemWidth := response.maxItemWidth
		totalHeight := st.totalHeight()
		layoutID, err = requestMeasuredLayoutOf(w, l.style, func(req MeasureRequest) Size {
			width := maxItemWidth
			if req.KnownWidthPresent {
				width = req.KnownWidth
			} else if req.AvailWidth.Kind == AvailDefinite {
				width = req.AvailWidth.Definite
			}
			height := totalHeight
			if req.AvailHeight.Kind == AvailDefinite && req.AvailHeight.Definite < height {
				height = req.AvailHeight.Definite
			}
			return Size{Width: width, Height: height}
		})
	} else {
		layoutID, err = requestLayoutOf(w, l.style)
	}
	if err != nil {
		panic(fmt.Sprintf("gpui: list request_layout: %v", err))
	}
	return layoutID, struct{}{}
}

// Prepaint implements Element: consume the reset flag, invalidate the
// cached heights on a width change, lay out the visible items around
// the scroll position (the offset clamp and the remeasure feedback),
// then prepaint each visible item at its scrolled origin under the
// intersected content mask (the pinned prepaint_items placement).
func (l *ListElement) Prepaint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *struct{}, w *Window, app *App) listPrepaintState {
	st := l.state.inner
	st.reset = false

	// The list's hitbox (R:list.rs:1551). Browser-core ticket 11: the
	// items prepaint inside the hitbox's subtree so their hitboxes
	// record it as their parent (the capture chain's ancestry,
	// pointercapture.go).
	listRecord := w.insertHitboxRecord(bounds, HitboxNormal)
	box := listRecord.box

	// A width change invalidates every cached item height and hint
	// (R:list.rs:1553-1568).
	if st.lastLayoutBounds == nil || st.lastLayoutBounds.Size.Width != bounds.Size.Width {
		for i := range st.items {
			st.items[i] = listItemState{}
		}
		st.resetMeasuring()
	}

	padding := resolveListPadding(l.style, bounds.Size, RemSize(w))
	mask := intersectMask(currentFrame(w).contentMask, bounds)
	// An item asking to be scrolled into view moves the scroll top, and
	// the items are prepainted again there without honoring a second
	// request (R:list.rs:1571-1597) — both passes inside the hitbox's
	// subtree (browser-core ticket 11).
	var state listPrepaintState
	var request *ListOffset
	w.withHitboxSubtree(listRecord, func() {
		state, request = st.prepaintItems(bounds, padding, mask, true, l.renderItem, w, app)
		if request != nil {
			st.logicalScrollTop = request
			state, _ = st.prepaintItems(bounds, padding, mask, false, l.renderItem, w, app)
		}
	})
	state.hitbox = &box
	return state
}

// prepaintItems is StateInner::prepaint_items (R:list.rs:1250-1361): lay
// out the visible items and prepaint each at its scrolled origin, inside
// a transaction. When autoscroll is on and an item's prepaint requested
// an autoscroll (Window.RequestAutoscroll) outside the list's bounds, the
// transaction is discarded and the scroll top that shows the requested
// bounds is returned instead: its top at the list's top when it sits
// above, its bottom at the list's bottom when it sits below.
func (st *listStateInner) prepaintItems(bounds Bounds, padding listPadding, mask *Bounds, autoscroll bool, renderItem func(int, *Window, *App) AnyElement, w *Window, app *App) (listPrepaintState, *ListOffset) {
	type result struct {
		state   listPrepaintState
		request *ListOffset
	}
	r, _ := transact(w, func(w *Window) (result, bool) {
		// The measure-all pass precedes the visible walk
		// (R:list.rs:1262-1267).
		st.layoutAllItems(bounds.Size.Width, renderItem, w, app)
		response := st.layoutItems(&bounds.Size.Width, bounds.Size.Height, padding, renderItem, w, app)

		// The port records the bounds before the items prepaint (the pin
		// does so after prepaint_items, R:list.rs:1599-1600), so an item's
		// prepaint reads this frame's viewport.
		st.lastLayoutBounds = &bounds
		st.lastPadding = &padding

		// Avoid honoring autoscroll requests from elements other than our
		// children (R:list.rs:1278-1279).
		w.TakeAutoscroll()

		state := listPrepaintState{scrollTop: response.scrollTop}
		// Only paint the visible items when there is space for them
		// (taking the padding into account).
		if bounds.Size.Height > padding.Top+padding.Bottom {
			origin := Point{X: bounds.Origin.X, Y: bounds.Origin.Y + padding.Top - response.scrollTop.OffsetInItem}
			for _, item := range response.itemLayouts {
				element := item.element
				WithContentMaskVoid(w, mask, func(w *Window) {
					WithAbsoluteElementOffsetVoid(w, origin, func(w *Window) {
						element.Prepaint(w, app)
					})
				})
				if requested, ok := w.TakeAutoscroll(); ok && autoscroll {
					if request := st.autoscrollOffset(requested, item.index, origin, bounds, padding, renderItem, w, app); request != nil {
						return result{request: request}, false
					}
				}
				state.items = append(state.items, listItemPlacement{element: element, origin: origin, size: item.size})
				origin.Y += item.size.Height
			}
		}
		return result{state: state}, true
	})
	return r.state, r.request
}

// autoscrollOffset is the scroll top that brings the requested bounds into
// the list, requested by the item at itemIx prepainted at itemOrigin, or
// nil when they need no scroll (R:list.rs:1290-1356).
func (st *listStateInner) autoscrollOffset(requested Bounds, itemIx int, itemOrigin Point, bounds Bounds, padding listPadding, renderItem func(int, *Window, *App) AnyElement, w *Window, app *App) *ListOffset {
	// An item's height for the walks: its measurement, else it is
	// rendered and laid out at the list width (R:list.rs:1310-1317,
	// 1341-1346).
	heightOf := func(ix int) float32 {
		if item := st.items[ix]; item.measured {
			return item.size.Height
		}
		element := wrapListItem(ix, renderItem(ix, w, app))
		return element.LayoutAsRoot(AvailableSize{Width: DefiniteAvailableSpace(bounds.Size.Width), Height: MinContentAvailableSpace()}, w, app).Height
	}
	if requested.Top() < bounds.Top() {
		offsetInItem := requested.Top() - itemOrigin.Y
		// The requested top can sit above this item's own top. Walk into
		// earlier items so the offset stays non-negative and no blank
		// space appears above the list (R:list.rs:1297-1320).
		for offsetInItem < 0 {
			if itemIx == 0 {
				offsetInItem = 0
				break
			}
			itemIx--
			offsetInItem += heightOf(itemIx)
		}
		return &ListOffset{ItemIx: itemIx, OffsetInItem: offsetInItem}
	}
	if requested.Bottom() > bounds.Bottom() {
		// Account for the height of the element down until the autoscroll
		// bottom, then keep decreasing the scroll top until the available
		// space is filled (R:list.rs:1327-1355).
		height := bounds.Size.Height - padding.Top - padding.Bottom
		height -= requested.Bottom() - itemOrigin.Y
		ix := itemIx
		for height > 0 {
			if ix == 0 {
				break
			}
			ix--
			height -= heightOf(ix)
		}
		offsetInItem := float32(0)
		if height < 0 {
			offsetInItem = -height
		}
		return &ListOffset{ItemIx: ix, OffsetInItem: offsetInItem}
	}
	return nil
}

// Paint implements Element (R:list.rs:1604-1650): register the wheel
// listener BEFORE painting the items, so in the bubble phase (reverse
// registration order) the items' wheel listeners run first and can stop
// the list from scrolling; then paint each visible item at its origin
// under the content mask. The listener coalesces the frame's deltas
// (ScrollDelta::coalesce), converts lines at the pinned 20px — pages
// resolve against the list's own visible size (browser-core ticket 02) —
// and applies the accumulated delta to the frame's scroll top through
// the eagerly clamping scroll body.
func (l *ListElement) Paint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *struct{}, prepaint *listPrepaintState, w *Window, app *App) {
	if prepaint.hitbox != nil {
		notify := currentViewNotifier(w)
		inner := l.state.inner
		height := bounds.Size.Height
		scrollTop := prepaint.scrollTop
		box := *prepaint.hitbox
		accumulated := ScrollDelta{}
		w.OnMouseEvent(func(event any, phase DispatchPhase, w *Window, app *App) {
			e, ok := event.(*ScrollWheelEvent)
			if !ok || phase != DispatchBubble || !box.ShouldHandleScroll(w) {
				return
			}
			accumulated = coalesceScrollDelta(accumulated, e.Delta)
			inner.scroll(scrollTop, height, accumulated.PixelDeltaFor(20, bounds.Size), notify, w, app)
		})
	}
	mask := intersectMask(currentFrame(w).contentMask, bounds)
	for _, item := range prepaint.items {
		element := item.element
		WithContentMaskVoid(w, mask, func(w *Window) {
			WithAbsoluteElementOffsetVoid(w, item.origin, func(w *Window) {
				element.Paint(w, app)
			})
		})
	}
}

// LayoutNodeStyle implements the root-stretch report.
func (l *ListElement) LayoutNodeStyle() (Style, bool) { return l.style, true }

// IntoElement converts the builder into a child element.
func (l *ListElement) IntoElement() AnyElement {
	return CustomElement[struct{}, listPrepaintState](l)
}
