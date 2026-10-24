package gpui

// This file is the scroll-state slice: the ScrollHandle (R:elements/
// div.rs:4724-4943) — the shared scroll state a view holds on behalf of
// a scrollable div, exposing the offset, the max offset, the painted
// bounds, the top/bottom visible children and the child bounds, with the
// pinned binary-search item lookup and the deferred scroll-to-item
// (FirstVisible / Top strategies). The scrollable div (div.go,
// scroll_element.go; Base ticket 07) writes the state back at prepaint
// (R:div.rs:2392-2400, 2931-2970) and applies wheel deltas to the shared
// offset cell at paint-registered listeners (R:div.rs:3816-3879). The
// uniform list keeps its own ScrollStrategy (R:elements/uniform_list.rs:
// 82-99), declared here with the shared scroll vocabulary.
//
// R: = reference/ce-source/crates/gpui/src (pin 254b5dbd).

// ScrollStrategy places a scrolled-to uniform-list item in the viewport
// (R:uniform_list.rs:82-99 ScrollStrategy).
type ScrollStrategy uint8

const (
	// ScrollTop places the item at the top of the viewport.
	ScrollTop ScrollStrategy = iota
	// ScrollCenter places the item in the middle of the viewport when the
	// content allows (else the closest position).
	ScrollCenter
	// ScrollBottom places the item at the bottom of the viewport when the
	// content allows (else the closest position).
	ScrollBottom
	// ScrollNearest scrolls the minimum distance that reveals the item
	// (top when it is above the viewport, bottom when below).
	ScrollNearest
)

// divScrollStrategy is the div scroll handle's private strategy
// (R:div.rs:4742-4747 ScrollStrategy: FirstVisible is the default).
type divScrollStrategy uint8

const (
	// divScrollFirstVisible scrolls the minimal amount that fully reveals
	// the child (R:div.rs:4745).
	divScrollFirstVisible divScrollStrategy = iota
	// divScrollTop places the child's top at the viewport top
	// (R:div.rs:4746).
	divScrollTop
)

// scrollActiveItem is one deferred scroll-to request (R:div.rs:4736-4740
// ScrollActiveItem).
type scrollActiveItem struct {
	index    int
	strategy divScrollStrategy
}

// scrollHandleState is the handle's shared state (R:div.rs:4724-4734
// ScrollHandleState). offset and ongoingScroll are cells shared with the
// tracking element (the pinned Rc<RefCell<..>> fields): the element's
// wheel listener writes the offset cell directly.
type scrollHandleState struct {
	// offset is the current scroll offset cell (negative y scrolls down).
	offset *Point
	// ongoingScroll is the dominant-axis tracker the element's
	// restrict_scroll_to_axis filter uses (R:div.rs:4727).
	ongoingScroll *OngoingScroll
	// bounds is the scrollable element's prepainted bounds.
	bounds Bounds
	// maxOffset is the maximum scroll distance per axis (content plus
	// padding minus the bounds, rounded to two decimals and clamped at
	// zero; R:div.rs:2953-2955). Positive: the offset ranges over
	// [-maxOffset, 0].
	maxOffset Point
	// childBounds are the children's layout bounds at the element's
	// prepaint, before the scroll offset is applied (R:div.rs:2394-2399).
	childBounds []Bounds
	// scrollToBottom is the pending scroll-to-bottom request
	// (R:div.rs:4731).
	scrollToBottom bool
	// overflowX and overflowY are the element's last computed overflow
	// (R:div.rs:4732, written at R:div.rs:2932).
	overflowX, overflowY Overflow
	// activeItem is the deferred scroll-to target (R:div.rs:4733).
	activeItem *scrollActiveItem
	// animator is the scroller's smooth-scroll state (browser-core 45,
	// scroll_smooth.go): the running animation lives next to the offset
	// cell it writes, like Chromium's per-ScrollableArea ScrollAnimator.
	animator *smoothScrollAnimator
	// anchor is the scroll-anchoring bookkeeping (browser-core 46,
	// scroll_anchor.go): the handle form of the element-state cell.
	anchor scrollAnchorState
}

// ScrollHandle is a handle to the scrollable aspects of an element
// (R:div.rs:4749-4753 ScrollHandle): views hold it and pass it to the
// scrollable element with DivElement.TrackScroll; it exposes the scroll
// state and mutates the offset. Copies share the state (the pinned
// Rc clone). The zero value is unusable; construct with NewScrollHandle.
type ScrollHandle struct {
	state *scrollHandleState
}

// NewScrollHandle constructs a new scroll handle (R:div.rs:4762-4765
// ScrollHandle::new).
func NewScrollHandle() ScrollHandle {
	return ScrollHandle{state: &scrollHandleState{
		offset:        &Point{},
		ongoingScroll: &OngoingScroll{},
		animator:      &smoothScrollAnimator{},
	}}
}

// Offset returns the current scroll offset (R:div.rs:4767-4770).
func (h ScrollHandle) Offset() Point {
	return *h.state.offset
}

// MaxOffset returns the maximum scroll offset (R:div.rs:4772-4775).
func (h ScrollHandle) MaxOffset() Point {
	return h.state.maxOffset
}

// pinnedBinarySearch is Rust's slice::binary_search_by over n elements
// with the comparator cmp(i) (<0 Less, 0 Equal, >0 Greater): it returns
// (index, true) for a match and (insertion point, false) otherwise. The
// probe sequence is the standard library's (base advances while the
// probe is not Greater), so ties between adjacent Equal children resolve
// identically.
func pinnedBinarySearch(n int, cmp func(i int) int) (int, bool) {
	size := n
	if size == 0 {
		return 0, false
	}
	base := 0
	for size > 1 {
		half := size / 2
		mid := base + half
		if cmp(mid) <= 0 {
			base = mid
		}
		size -= half
	}
	c := cmp(base)
	if c == 0 {
		return base, true
	}
	if c < 0 {
		return base + 1, false
	}
	return base, false
}

// itemAt is the shared body of top_item / bottom_item: the child whose
// vertical range contains y, else the insertion point clamped to the last
// child (R:div.rs:4782-4793).
func (h ScrollHandle) itemAt(y float32) int {
	children := h.state.childBounds
	ix, found := pinnedBinarySearch(len(children), func(i int) int {
		bounds := children[i]
		if y < bounds.Top() {
			return 1
		}
		if y > bounds.Bottom() {
			return -1
		}
		return 0
	})
	if found {
		return ix
	}
	last := len(children) - 1
	if last < 0 {
		last = 0
	}
	if ix > last {
		return last
	}
	return ix
}

// TopItem returns the topmost child scrolled into view
// (R:div.rs:4777-4794 top_item).
func (h ScrollHandle) TopItem() int {
	return h.itemAt(h.state.bounds.Top() - h.state.offset.Y)
}

// BottomItem returns the bottom-most child scrolled into view
// (R:div.rs:4796-4813 bottom_item).
func (h ScrollHandle) BottomItem() int {
	return h.itemAt(h.state.bounds.Bottom() - h.state.offset.Y)
}

// Bounds returns the bounds into which the scrollable element is painted
// (R:div.rs:4815-4818).
func (h ScrollHandle) Bounds() Bounds {
	return h.state.bounds
}

// BoundsForItem returns the layout bounds of child ix
// (R:div.rs:4820-4823 bounds_for_item).
func (h ScrollHandle) BoundsForItem(ix int) (Bounds, bool) {
	if ix < 0 || ix >= len(h.state.childBounds) {
		return Bounds{}, false
	}
	return h.state.childBounds[ix], true
}

// ScrollToItem defers a scroll that fully reveals child ix with the
// minimal movement, applied at the element's next prepaint
// (R:div.rs:4825-4832 scroll_to_item, FirstVisible).
func (h ScrollHandle) ScrollToItem(ix int) {
	h.state.activeItem = &scrollActiveItem{index: ix, strategy: divScrollFirstVisible}
}

// ScrollToTopOfItem defers a scroll that places child ix at the top of
// the viewport (R:div.rs:4834-4842 scroll_to_top_of_item).
func (h ScrollHandle) ScrollToTopOfItem(ix int) {
	h.state.activeItem = &scrollActiveItem{index: ix, strategy: divScrollTop}
}

// scrollToActiveItem applies the deferred scroll-to request against the
// last prepaint's bounds, children and overflow (R:div.rs:4844-4893). A
// request for a child that does not exist yet stays pending. An
// instant write under a running smooth animation adjusts (not cancels)
// the animation (browser-core 45, Chromium's crbug.com/1264266 rule).
func (h ScrollHandle) scrollToActiveItem() {
	state := h.state
	active := state.activeItem
	if active == nil {
		return
	}
	if active.index < 0 || active.index >= len(state.childBounds) {
		return
	}
	bounds := state.childBounds[active.index]
	offset := state.offset
	old := *offset
	switch active.strategy {
	case divScrollFirstVisible:
		if state.overflowY == OverflowScroll {
			childHeight := bounds.Size.Height
			viewportHeight := state.bounds.Size.Height
			if childHeight > viewportHeight {
				offset.Y = state.bounds.Top() - bounds.Top()
			} else if bounds.Top()+offset.Y < state.bounds.Top() {
				offset.Y = state.bounds.Top() - bounds.Top()
			} else if bounds.Bottom()+offset.Y > state.bounds.Bottom() {
				offset.Y = state.bounds.Bottom() - bounds.Bottom()
			}
		}
	case divScrollTop:
		offset.Y = state.bounds.Top() - bounds.Top()
	}
	if state.overflowX == OverflowScroll {
		childWidth := bounds.Size.Width
		viewportWidth := state.bounds.Size.Width
		if childWidth > viewportWidth {
			offset.X = state.bounds.Left() - bounds.Left()
		} else if bounds.Left()+offset.X < state.bounds.Left() {
			offset.X = state.bounds.Left() - bounds.Left()
		} else if bounds.Right()+offset.X > state.bounds.Right() {
			offset.X = state.bounds.Right() - bounds.Right()
		}
	}
	state.activeItem = nil
	if *offset != old && state.animator != nil && state.animator.anim != nil {
		state.animator.anim.adjust(pointSub(*offset, old))
	}
}

// ScrollToBottom scrolls to the bottom at the element's next prepaint
// (R:div.rs:4895-4899 scroll_to_bottom).
func (h ScrollHandle) ScrollToBottom() {
	h.state.scrollToBottom = true
}

// SetOffset sets the offset explicitly: the distance from the top left of
// the parent container to the top left of the first child, more negative
// as the content scrolls further down (R:div.rs:4901-4907). The
// scrollable element clamps it at its next prepaint. A running smooth
// scroll animation is adjusted (not cancelled) by the write (browser-
// core 45, Chromium's crbug.com/1264266 rule); use ScrollTo for a smooth
// programmatic scroll.
func (h ScrollHandle) SetOffset(offset Point) {
	if h.state == nil {
		return
	}
	if h.state.animator != nil {
		h.state.animator.writeInstant(h.state.offset, offset)
		return
	}
	*h.state.offset = offset
}

// ScrollBehavior is the timing of a programmatic scroll (CSSOM's
// ScrollBehavior, dom.d.ts scrollIntoView/scrollTo): auto follows the
// scroller's own policy (the port has no scroll-behavior style, so it
// is the instant default Chrome computes), instant jumps, smooth
// animates with the programmatic curve — while the application scrolls
// smoothly (the D13 pref and the reduced-motion gate; otherwise it
// jumps like instant).
type ScrollBehavior uint8

const (
	// ScrollBehaviorAuto follows the scroller's policy (instant in the
	// port — Chrome's computed scroll-behavior default).
	ScrollBehaviorAuto ScrollBehavior = iota
	// ScrollBehaviorInstant jumps to the target.
	ScrollBehaviorInstant
	// ScrollBehaviorSmooth animates to the target with the programmatic
	// curve (Chromium's kProgrammatic scroll type: the tweaked bezier and
	// the sqrt-of-delta duration).
	ScrollBehaviorSmooth
)

// ScrollTo sets the offset with an explicit behavior (CSSOM's
// Element.scrollTo): smooth starts a programmatic animation toward the
// clamped target — a running user animation is cancelled first, as
// Chromium cancels the user scroll animation before a programmatic one
// (scrollable_area.cc:519-525), leaving the offset at its current
// animated position — and auto or instant write it directly, adjusting
// (not cancelling) a running animation (crbug.com/1264266). The
// scrollable element clamps the result at its next prepaint; w and app
// drive the smooth animation's frames (nil falls back to the instant
// write, as do the reduced-motion and smooth-scrolling gates).
func (h ScrollHandle) ScrollTo(offset Point, behavior ScrollBehavior, w *Window, app *App) {
	if h.state == nil {
		return
	}
	if behavior == ScrollBehaviorSmooth && w != nil && app != nil && app.smoothScrollAnimated() {
		if an := h.state.animator; an != nil {
			if anim := an.anim; anim != nil {
				// Cancel at the current animated position (the offset cell
				// holds the last tick's sample; re-sample for exactness).
				*h.state.offset = anim.valueAt(app.Now() - anim.start)
				an.anim = nil
			}
			target := offset
			target.X = clampF32(target.X, -h.state.maxOffset.X, 0)
			target.Y = clampF32(target.Y, -h.state.maxOffset.Y, 0)
			animateSmoothScroll(h.state.offset, h.state.maxOffset, &h.state.animator, target,
				smoothScrollProgrammatic, func(w *Window, app *App) { w.requestRefresh() }, w, app)
			return
		}
	}
	h.SetOffset(offset)
}

// LogicalScrollTop returns the top child index and the pixel offset of
// its top from the viewport top (R:div.rs:4909-4922).
func (h ScrollHandle) LogicalScrollTop() (int, float32) {
	ix := h.TopItem()
	state := h.state
	if ix >= 0 && ix < len(state.childBounds) {
		return ix, state.childBounds[ix].Top() + state.offset.Y - state.bounds.Top()
	}
	return ix, 0
}

// LogicalScrollBottom returns the bottom child index and the pixel offset
// of its bottom from the viewport bottom (R:div.rs:4924-4937).
func (h ScrollHandle) LogicalScrollBottom() (int, float32) {
	ix := h.BottomItem()
	state := h.state
	if ix >= 0 && ix < len(state.childBounds) {
		return ix, state.childBounds[ix].Bottom() + state.offset.Y - state.bounds.Bottom()
	}
	return ix, 0
}

// ChildrenCount returns the number of tracked children
// (R:div.rs:4939-4942 children_count).
func (h ScrollHandle) ChildrenCount() int {
	return len(h.state.childBounds)
}
