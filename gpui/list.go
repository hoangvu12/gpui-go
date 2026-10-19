package gpui

// This file is the list slice of ticket14: the UniformList
// (R:elements/uniform_list.rs, bounded) — the fixed-pitch virtualized
// list. The scroll state lives in a view-held UniformListScrollHandle
// whose base ScrollHandle carries the shared offset cell, the bounds and
// the max offset (R:uniform_list.rs:77-122); the element requests a
// measured layout node, and during PREPAINT inserts its hitbox, clamps
// the offset through the shared clamp_scroll_position
// (R:elements/div.rs:2913-2976) plus the list's own content-end check
// (R:uniform_list.rs:396-413), applies the deferred scroll-to-item
// (R:uniform_list.rs:415-471), renders only the visible items, lays each
// one out as a standalone root under definite space and prepaints/paints
// it at its scrolled origin under the scroll mask. Paint registers the
// interactivity's default wheel listener (R:div.rs:3816-3879 through
// R:uniform_list.rs:555) before the items — the list's base style
// scrolls on y (R:uniform_list.rs:31-32), so a wheel delta applies
// unclamped to the offset cell and the next prepaint clamps it (Base
// ticket 07).
//
// Bounded deviations from the pin (recorded): the y-flipped mode, list
// decorations, with_width_from_item and the strict / offset scroll-to
// variants are not ported; horizontal sizing is always FitList (the x
// offset clamps to zero); the item is measured once per frame at request
// layout and reused at prepaint (the pin measures again,
// R:uniform_list.rs:359); a zero item pitch renders no items (the pin's
// division would yield an empty or saturated range); the list stays
// anonymous (the pin's uniform_list takes an id, used only for the
// element-state fallback the always-present handle makes unnecessary).
//
// R: = reference/ce-source/crates/gpui/src (pin 254b5dbd).

import (
	"fmt"
	"math"
)

// ListSizingBehavior selects how the list sizes itself (the reference
// ListSizingBehavior).
type ListSizingBehavior uint8

const (
	// ListInfer sizes the list to its content height, clamped by the
	// available height (the default).
	ListInfer ListSizingBehavior = iota
	// ListAuto lets the parent's layout size the list node.
	ListAuto
)

// uniformListScrollState is the uniform list's shared scroll state
// (R:uniform_list.rs:113-122 UniformListScrollState, bounded).
type uniformListScrollState struct {
	// baseHandle carries the offset cell (y <= 0; scrolling down
	// decreases y), the bounds and the max offset (R:uniform_list.rs:116).
	baseHandle ScrollHandle
	// pendingScrollTo is the deferred scroll target consumed by the
	// next prepaint (R:uniform_list.rs:117 deferred_scroll_to_item).
	pendingScrollTo *uniformListPendingScroll
	// lastItemSize and lastContentSize observe the last computed
	// geometry (the port's ItemSize record).
	lastItemSize    Size
	lastContentSize Size
}

// uniformListPendingScroll is one deferred scroll-to-item request
// (R:uniform_list.rs:101-111 DeferredScrollToItem with offset 0, not
// strict).
type uniformListPendingScroll struct {
	index    int
	strategy ScrollStrategy
}

// UniformListScrollHandle is the scroll state a view holds for a
// uniform list (R:uniform_list.rs:77-80 UniformListScrollHandle).
type UniformListScrollHandle struct {
	state *uniformListScrollState
}

// NewUniformListScrollHandle constructs a new handle
// (R:uniform_list.rs:135-143).
func NewUniformListScrollHandle() *UniformListScrollHandle {
	return &UniformListScrollHandle{state: &uniformListScrollState{baseHandle: NewScrollHandle()}}
}

// BaseHandle returns the list's base scroll handle (the pinned
// UniformListScrollState::base_handle field, R:uniform_list.rs:116): its
// Offset, MaxOffset and Bounds are the scrollbar metrics, written by the
// list's prepaint.
func (h *UniformListScrollHandle) BaseHandle() ScrollHandle { return h.state.baseHandle }

// Offset returns the current scroll offset (the base handle's).
func (h *UniformListScrollHandle) Offset() Point { return h.state.baseHandle.Offset() }

// SetOffset sets the scroll offset (clamped by the list's next
// prepaint against the content size).
func (h *UniformListScrollHandle) SetOffset(offset Point) { h.state.baseHandle.SetOffset(offset) }

// ScrollToItem defers a scroll bringing item ix into view under the
// strategy, applied at the list's next prepaint (R:uniform_list.rs:
// 145-157 scroll_to_item: non-strict — a fully visible item does not
// move).
func (h *UniformListScrollHandle) ScrollToItem(ix int, strategy ScrollStrategy) {
	h.state.pendingScrollTo = &uniformListPendingScroll{index: ix, strategy: strategy}
}

// IsScrolledToEnd reports whether the list is scrolled to the end; ok is
// false when the list is not scrollable (R:uniform_list.rs:239-249).
func (h *UniformListScrollHandle) IsScrolledToEnd() (atEnd bool, ok bool) {
	base := h.state.baseHandle
	maxOffset := base.MaxOffset()
	if maxOffset.Y <= 0 {
		return false, false
	}
	return -base.Offset().Y >= maxOffset.Y, true
}

// LastItemSize returns the last measured item size (the handle's
// observable of the list geometry).
func (h *UniformListScrollHandle) LastItemSize() Size { return h.state.lastItemSize }

// LastContentSize returns the last computed content size.
func (h *UniformListScrollHandle) LastContentSize() Size { return h.state.lastContentSize }

// uniformListFrameState is the uniform list's request-layout state.
type uniformListFrameState struct {
	// items are this frame's rendered visible items (element + origin).
	items []uniformListItem
	// itemHeight is the measured item pitch.
	itemHeight float32
}

// uniformListPrepaintState is the list's prepaint state (the pinned
// Option<Hitbox>, R:uniform_list.rs:265).
type uniformListPrepaintState struct {
	hitbox *Hitbox
}

// uniformListItem is one visible item with its prepaint origin.
type uniformListItem struct {
	element AnyElement
	origin  Point
}

// UniformListElement is the uniform list element (the reference
// UniformList): count items of one fixed pitch, rendered through the
// callback for the visible range only.
type UniformListElement struct {
	// handle is the view-held scroll state.
	handle *UniformListScrollHandle
	// count is the item count.
	count int
	// renderItems builds the elements for the half-open item range.
	renderItems func(start, end int, w *Window, app *App) []AnyElement
	// style is the element's layout style (overflow-y scroll by default,
	// R:uniform_list.rs:31-32).
	style Style
	// sizing selects the sizing behavior.
	sizing ListSizingBehavior
	// measuredItem is the item-0 measurement from request layout.
	measuredItem Size
	// scroll is this frame's scroll slice over the handle's base handle.
	scroll *elementScroll
}

// UniformList constructs a uniform list over the handle's scroll state
// with the given item count and item-range render callback (the
// reference uniform_list + track_scroll, R:uniform_list.rs:22-55,
// 686-691).
func UniformList(handle *UniformListScrollHandle, count int, renderItems func(start, end int, w *Window, app *App) []AnyElement) *UniformListElement {
	style := DefaultStyle()
	style.OverflowY = OverflowScroll
	return &UniformListElement{
		handle:      handle,
		count:       count,
		renderItems: renderItems,
		style:       style,
	}
}

// WithSizingBehavior sets the sizing behavior (with_sizing_behavior).
func (u *UniformListElement) WithSizingBehavior(behavior ListSizingBehavior) *UniformListElement {
	u.sizing = behavior
	return u
}

// W sets the width style.
func (u *UniformListElement) W(width Length) *UniformListElement {
	u.style.Size.Width = width
	return u
}

// H sets the height style.
func (u *UniformListElement) H(height Length) *UniformListElement {
	u.style.Size.Height = height
	return u
}

// SizeFull stretches the list to its parent's size.
func (u *UniformListElement) SizeFull() *UniformListElement {
	u.style.Size = LengthSize{Width: DefiniteLengthOf(Fraction(1.0)), Height: DefiniteLengthOf(Fraction(1.0))}
	return u
}

// ID implements Element: uniform lists are anonymous (the scroll state
// is view-held, not element-state-keyed).
func (u *UniformListElement) ID() (ElementID, bool) { return ElementID{}, false }

// SourceLocation implements Element.
func (u *UniformListElement) SourceLocation() *SourceLocation { return nil }

// RequestLayout implements Element: measure item 0 under max-content
// width and min-content height (R:uniform_list.rs:662-684 measure_item
// (None)), resolve the scroll slice from the handle's base handle
// (R:div.rs:2735-2738), then request the list's node — Infer sizes the
// height to min(content, available), Auto requests a plain node
// (R:uniform_list.rs:275-329).
func (u *UniformListElement) RequestLayout(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App) (LayoutID, uniformListFrameState) {
	// The item-0 measurement: render the first item and lay it out as
	// a standalone root.
	u.measuredItem = Size{}
	if u.renderItems != nil && u.count > 0 {
		items := u.renderItems(0, 1, w, app)
		if len(items) > 0 {
			u.measuredItem = items[len(items)-1].LayoutAsRoot(AvailableSize{
				Width:  MaxContentAvailableSpace(),
				Height: MinContentAvailableSpace(),
			}, w, app)
		}
	}
	base := u.handle.state.baseHandle
	u.scroll = resolveElementScroll(&base, nil, global, w)
	state := uniformListFrameState{itemHeight: u.measuredItem.Height}

	var layoutID LayoutID
	var err error
	if u.sizing == ListAuto {
		layoutID, err = requestLayoutOf(w, u.style)
	} else {
		itemHeight := u.measuredItem.Height
		count := u.count
		itemWidth := u.measuredItem.Width
		layoutID, err = requestMeasuredLayoutOf(w, u.style, func(req MeasureRequest) Size {
			desiredHeight := itemHeight * float32(count)
			width := itemWidth
			if req.KnownWidthPresent {
				width = req.KnownWidth
			} else if req.AvailWidth.Kind == AvailDefinite {
				width = req.AvailWidth.Definite
			}
			height := desiredHeight
			if req.AvailHeight.Kind == AvailDefinite && req.AvailHeight.Definite < height {
				height = req.AvailHeight.Definite
			}
			return Size{Width: width, Height: height}
		})
	}
	if err != nil {
		panic(fmt.Sprintf("gpui: uniform list request_layout: %v", err))
	}
	return layoutID, state
}

// Prepaint implements Element (R:uniform_list.rs:331-543): the padded
// bounds and content size, then — inside the style's overflow mask — the
// hitbox and the shared scroll clamp (which writes the base handle's max
// offset and bounds), the list's content-end check, the deferred
// scroll-to-item, the visible range, and each visible item laid out and
// prepainted at its scrolled origin under the scroll mask.
func (u *UniformListElement) Prepaint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *uniformListFrameState, w *Window, app *App) uniformListPrepaintState {
	style := u.style
	rem := RemSize(w)
	border := style.BorderWidths.ToPixels(rem)
	padding := Edges{
		Top:    resolveDefiniteListLength(style.Padding.Top, bounds.Size.Height, rem),
		Right:  resolveDefiniteListLength(style.Padding.Right, bounds.Size.Width, rem),
		Bottom: resolveDefiniteListLength(style.Padding.Bottom, bounds.Size.Height, rem),
		Left:   resolveDefiniteListLength(style.Padding.Left, bounds.Size.Width, rem),
	}
	paddedBounds := fromCorners(
		Point{X: bounds.Origin.X + border.Left + padding.Left, Y: bounds.Origin.Y + border.Top + padding.Top},
		Point{X: bounds.Right() - (border.Right + padding.Right), Y: bounds.Bottom() - (border.Bottom + padding.Bottom)},
	)
	pitch := layout.itemHeight
	contentSize := Size{Width: paddedBounds.Size.Width, Height: pitch * float32(u.count)}

	shared := u.scroll.offset
	deferred := u.handle.state.pendingScrollTo
	u.handle.state.pendingScrollTo = nil
	u.handle.state.lastItemSize = Size{Width: paddedBounds.Size.Width, Height: pitch}
	u.handle.state.lastContentSize = contentSize

	state := uniformListPrepaintState{}
	layout.items = nil
	WithContentMaskVoid(w, style.OverflowMask(bounds, rem), func(w *Window) {
		box := w.InsertHitbox(bounds, HitboxNormal)
		state.hitbox = &box
		scrollOffset := u.scroll.clamp(bounds, &style, contentSize, w)

		if u.count == 0 {
			return
		}
		contentHeight := pitch * float32(u.count)
		// The list's own content-end check (R:uniform_list.rs:399-405).
		maxScrollOffset := paddedBounds.Size.Height - contentHeight
		if scrollOffset.Y != 0 && scrollOffset.Y < maxScrollOffset {
			shared.Y = maxScrollOffset
			scrollOffset.Y = maxScrollOffset
		}

		// The deferred scroll-to-item (R:uniform_list.rs:415-471, offset
		// 0, not strict).
		if deferred != nil {
			listHeight := paddedBounds.Size.Height
			itemTop := pitch * float32(deferred.index)
			itemBottom := itemTop + pitch
			scrollTop := -shared.Y
			isAbove := itemTop < scrollTop
			isBelow := itemBottom > scrollTop+listHeight
			if isAbove || isBelow {
				strategy := deferred.strategy
				if strategy == ScrollNearest {
					if isAbove {
						strategy = ScrollTop
					} else {
						strategy = ScrollBottom
					}
				}
				maxScroll := max32(contentHeight-listHeight, 0)
				switch strategy {
				case ScrollTop:
					shared.Y = -clampF32(itemTop, 0, maxScroll)
				case ScrollCenter:
					itemCenter := itemTop + pitch/2
					target := itemCenter - listHeight/2
					shared.Y = -clampF32(target, 0, maxScroll)
				case ScrollBottom:
					shared.Y = -clampF32(itemBottom-listHeight, 0, maxScroll)
				}
			}
			scrollOffset = *shared
		}

		if pitch <= 0 {
			return
		}
		// The visible range (R:uniform_list.rs:473-480; `as usize`
		// saturates negatives to zero).
		first := int(math.Floor(float64(-(scrollOffset.Y + padding.Top) / pitch)))
		last := int(math.Ceil(float64((-scrollOffset.Y + paddedBounds.Size.Height) / pitch)))
		if first < 0 {
			first = 0
		}
		if last > u.count {
			last = u.count
		}
		if u.renderItems == nil || first >= last {
			return
		}
		items := u.renderItems(first, last, w, app)
		WithContentMaskVoid(w, scrollMaskOf(&style, bounds, rem), func(w *Window) {
			for i, element := range items {
				ix := first + i
				if ix >= last {
					break
				}
				origin := Point{
					X: paddedBounds.Origin.X + scrollOffset.X,
					Y: paddedBounds.Origin.Y + scrollOffset.Y + pitch*float32(ix),
				}
				element.LayoutAsRoot(AvailableSize{
					Width:  DefiniteAvailableSpace(paddedBounds.Size.Width),
					Height: DefiniteAvailableSpace(pitch),
				}, w, app)
				element.PrepainAt(origin, w, app)
				layout.items = append(layout.items, uniformListItem{element: element, origin: origin})
			}
		})
	})
	return state
}

// Paint implements Element (R:uniform_list.rs:545-571 through
// Interactivity::paint): register the default wheel listener with the
// hitbox (R:div.rs:3081), then paint each visible item at its origin
// under the overflow and scroll masks.
func (u *UniformListElement) Paint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *uniformListFrameState, prepaint *uniformListPrepaintState, w *Window, app *App) {
	style := u.style
	rem := RemSize(w)
	WithContentMaskVoid(w, style.OverflowMask(bounds, rem), func(w *Window) {
		if prepaint.hitbox != nil {
			u.scroll.paintListener(*prepaint.hitbox, &style, w)
		}
		WithContentMaskVoid(w, scrollMaskOf(&style, bounds, rem), func(w *Window) {
			for _, item := range layout.items {
				WithAbsoluteElementOffsetVoid(w, item.origin, func(w *Window) {
					item.element.Paint(w, app)
				})
			}
		})
	})
}

// LayoutNodeStyle implements the root-stretch report.
func (u *UniformListElement) LayoutNodeStyle() (Style, bool) { return u.style, true }

// IntoElement converts the builder into a child element
// (the fixture's IntoElement convention).
func (u *UniformListElement) IntoElement() AnyElement {
	return CustomElement[uniformListFrameState, uniformListPrepaintState](u)
}

// intersectMask intersects two content masks (the pin's scroll mask
// intersects the current mask with the element's bounds; a disjoint
// pair clamps to a zero-size mask at the corner).
func intersectMask(a, b Bounds) *Bounds {
	left := max32(a.Left(), b.Left())
	top := max32(a.Top(), b.Top())
	right := min32(a.Right(), b.Right())
	bottom := min32(a.Bottom(), b.Bottom())
	if right < left {
		right = left
	}
	if bottom < top {
		bottom = top
	}
	mask := Bounds{
		Origin: Point{X: left, Y: top},
		Size:   Size{Width: right - left, Height: bottom - top},
	}
	return &mask
}

// clampF32 clamps v into [lo, hi].
func clampF32(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
