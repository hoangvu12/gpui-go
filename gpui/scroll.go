package gpui

// This file is the scroll-state slice of ticket14: the ScrollHandle
// (elements/div.rs 4725-4840, bounded) — the shared scroll state a view
// holds on behalf of a scrollable element, exposing the offset, the
// max offset, the top/bottom visible children and child bounds, with
// the pinned binary-search item lookup. The mouse-wheel/scrollbar
// input plumbing arrives with the hitbox/input tickets; the list
// elements (list.go) drive this handle from their prepaint.

import "sort"

// ScrollStrategy places a scrolled-to child in the viewport (the
// reference ScrollStrategy).
type ScrollStrategy uint8

const (
	// ScrollTop places the child at the top of the viewport.
	ScrollTop ScrollStrategy = iota
	// ScrollCenter places the child in the middle of the viewport when
	// the content allows (else the closest position).
	ScrollCenter
	// ScrollBottom places the child at the bottom of the viewport when
	// the content allows (else the closest position).
	ScrollBottom
	// ScrollNearest scrolls the minimum distance that reveals the
	// child (top when it is above the viewport, bottom when below).
	ScrollNearest
)

// scrollHandleState is the handle's shared state (the reference
// ScrollHandleState, bounded to the offset/bounds/children surface).
type scrollHandleState struct {
	// offset is the current scroll offset (negative y scrolls down).
	offset Point
	// maxOffset is the maximum scroll offset (content - viewport,
	// clamped at zero).
	maxOffset Point
	// bounds is the scrollable element's painted bounds.
	bounds Bounds
	// childBounds are the children's layout bounds in window-local
	// pixels (the reference stores them scrolled-in-view).
	childBounds []Bounds
	// pendingScrollToItem is the deferred scroll-to target consumed by
	// the next prepaint (the reference ScrollActiveItem).
	pendingScrollToItem *scrollActiveItem
}

// scrollActiveItem is one deferred scroll-to request.
type scrollActiveItem struct {
	index    int
	strategy ScrollStrategy
}

// ScrollHandle is a handle to the scrollable aspects of an element
// (div.rs ScrollHandle): views hold it and pass it to the scrollable
// element; it exposes the scroll state and mutates the offset.
type ScrollHandle struct {
	state *scrollHandleState
}

// NewScrollHandle constructs a new scroll handle (ScrollHandle::new).
func NewScrollHandle() ScrollHandle {
	return ScrollHandle{state: &scrollHandleState{}}
}

// Offset returns the current scroll offset (ScrollHandle::offset).
func (h ScrollHandle) Offset() Point {
	return h.state.offset
}

// MaxOffset returns the maximum scroll offset (content minus viewport,
// zero when the content fits; ScrollHandle::max_offset).
func (h ScrollHandle) MaxOffset() Point {
	return h.state.maxOffset
}

// SetOffset sets the scroll offset (ScrollHandle::set_offset; the
// scrollable element clamps it against the content size at the next
// prepaint).
func (h ScrollHandle) SetOffset(offset Point) {
	h.state.offset = offset
}

// Bounds returns the scrollable element's painted bounds
// (ScrollHandle::bounds).
func (h ScrollHandle) Bounds() Bounds {
	return h.state.bounds
}

// TopItem returns the topmost child scrolled into view (the pinned
// top_item binary search: the child whose y range contains the
// viewport's top edge, else the nearest index).
func (h ScrollHandle) TopItem() int {
	state := h.state
	top := state.bounds.Top() - state.offset.Y
	ix := sort.Search(len(state.childBounds), func(i int) bool {
		bounds := state.childBounds[i]
		return top <= bounds.Bottom()
	})
	if ix < len(state.childBounds) && top < state.childBounds[ix].Top() {
		// The viewport top is before this child's top: the search's
		// insertion point IS the answer (the reference's Err arm).
		return ix
	}
	if ix >= len(state.childBounds) {
		if len(state.childBounds) == 0 {
			return 0
		}
		return len(state.childBounds) - 1
	}
	return ix
}

// BottomItem returns the bottom-most child scrolled into view (the
// pinned bottom_item binary search).
func (h ScrollHandle) BottomItem() int {
	state := h.state
	bottom := state.bounds.Bottom() - state.offset.Y
	ix := sort.Search(len(state.childBounds), func(i int) bool {
		bounds := state.childBounds[i]
		return bottom <= bounds.Bottom()
	})
	if ix >= len(state.childBounds) {
		if len(state.childBounds) == 0 {
			return 0
		}
		return len(state.childBounds) - 1
	}
	return ix
}

// BoundsForItem returns the painted bounds of child ix
// (ScrollHandle::bounds_for_item).
func (h ScrollHandle) BoundsForItem(ix int) (Bounds, bool) {
	if ix < 0 || ix >= len(h.state.childBounds) {
		return Bounds{}, false
	}
	return h.state.childBounds[ix], true
}

// ScrollToItem defers a scroll that brings item ix into view under
// the strategy (ScrollHandle::scroll_to_item), applied by the
// scrollable element's next prepaint.
func (h ScrollHandle) ScrollToItem(ix int, strategy ScrollStrategy) {
	h.state.pendingScrollToItem = &scrollActiveItem{index: ix, strategy: strategy}
}

// scrollTo computes the offset that places childBounds under the
// strategy within the scroll bounds (the pinned scroll_to placement
// math: top/center/bottom/nearest offsets clamped to [max, 0]).
func (h ScrollHandle) scrollTo(bounds, child Bounds, strategy ScrollStrategy) Point {
	state := h.state
	maxY := minZero32(state.maxOffset.Y)
	var target float32
	switch strategy {
	case ScrollTop:
		target = minZero32(maxY + (child.Top() - bounds.Top()))
	case ScrollCenter:
		viewport := bounds.Size.Height
		target = minZero32(maxY + (child.Top() - bounds.Top()) - (viewport-child.Size.Height)/2)
	case ScrollBottom:
		viewport := bounds.Size.Height
		target = minZero32(maxY + (child.Bottom() - bounds.Bottom()) + (viewport - child.Size.Height))
	case ScrollNearest:
		if child.Top() < bounds.Top() {
			target = minZero32(maxY + (child.Top() - bounds.Top()))
		} else if child.Bottom() > bounds.Bottom() {
			viewport := bounds.Size.Height
			target = minZero32(maxY + (child.Bottom() - bounds.Bottom()) + (viewport - child.Size.Height))
		} else {
			target = minZero32(state.offset.Y)
		}
	}
	target = max32(target, minZero32(maxY))
	return Point{X: state.offset.X, Y: target}
}

// consumePendingScrollTo applies and clears the deferred scroll-to
// request against the current state (used by the scrollable elements'
// prepaint).
func (h ScrollHandle) consumePendingScrollTo(bounds Bounds, childBoundsAt func(ix int) (Bounds, bool)) (Point, bool) {
	pending := h.state.pendingScrollToItem
	if pending == nil {
		return Point{}, false
	}
	h.state.pendingScrollToItem = nil
	child, ok := childBoundsAt(pending.index)
	if !ok {
		return Point{}, false
	}
	h.state.offset = h.scrollTo(bounds, child, pending.strategy)
	return h.state.offset, true
}

// minZero32 clamps a negative value to zero.
func minZero32(v float32) float32 {
	if v < 0 {
		return 0
	}
	return v
}
