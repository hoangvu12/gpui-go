package gpui

// This file is the list slice of ticket14: the UniformList
// (elements/uniform_list.rs, bounded) — the fixed-pitch virtualized
// list. The scroll state lives in a view-held
// UniformListScrollHandle (the reference keeps it in the app's view
// state); the element requests a measured layout node, and during
// PREPAINT renders only the visible items (render_items over the
// range), lays each one out as a standalone root under definite space
// and prepaints/paints it at its scrolled origin under the content
// mask — the pinned virtualization shape.
//
// Bounded deviations from the pin (recorded): the y-flipped mode and
// list decorations are not ported (no consumers in this slice);
// horizontal scrolling is always clamped to the bounds width
// (ListHorizontalSizingBehavior::Unconstrained omitted); the mouse
// wheel and scrollbar input plumbing arrives with the hitbox/input
// tickets — this slice owns the scroll state and geometry.

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
// (the reference UniformListScrollState, bounded).
type uniformListScrollState struct {
	// offset is the scroll offset (y <= 0; scrolling down decreases y).
	offset Point
	// pendingScrollTo is the deferred scroll target consumed by the
	// next prepaint (the reference deferred_scroll_to_item).
	pendingScrollTo *uniformListPendingScroll
	// lastItemSize and lastContentSize observe the last computed
	// geometry (the reference ItemSize record).
	lastItemSize    Size
	lastContentSize Size
}

// uniformListPendingScroll is one deferred scroll-to-item request.
type uniformListPendingScroll struct {
	index    int
	strategy ScrollStrategy
}

// UniformListScrollHandle is the scroll state a view holds for a
// uniform list (the reference UniformListScrollHandle).
type UniformListScrollHandle struct {
	state *uniformListScrollState
}

// NewUniformListScrollHandle constructs a new handle.
func NewUniformListScrollHandle() *UniformListScrollHandle {
	return &UniformListScrollHandle{state: &uniformListScrollState{}}
}

// Offset returns the current scroll offset.
func (h *UniformListScrollHandle) Offset() Point { return h.state.offset }

// SetOffset sets the scroll offset (clamped by the list's next
// prepaint against the content size).
func (h *UniformListScrollHandle) SetOffset(offset Point) { h.state.offset = offset }

// ScrollToItem defers a scroll bringing item ix into view under the
// strategy, applied at the list's next prepaint (the pinned
// scroll_to_item).
func (h *UniformListScrollHandle) ScrollToItem(ix int, strategy ScrollStrategy) {
	h.state.pendingScrollTo = &uniformListPendingScroll{index: ix, strategy: strategy}
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
	// style is the element's layout style.
	style Style
	// sizing selects the sizing behavior.
	sizing ListSizingBehavior
	// measuredItem is the item-0 measurement from request layout.
	measuredItem Size
}

// UniformList constructs a uniform list over the handle's scroll state
// with the given item count and item-range render callback (the
// reference UniformList::new).
func UniformList(handle *UniformListScrollHandle, count int, renderItems func(start, end int, w *Window, app *App) []AnyElement) *UniformListElement {
	return &UniformListElement{
		handle:      handle,
		count:       count,
		renderItems: renderItems,
		style:       DefaultStyle(),
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

// RequestLayout implements Element: measure item 0 under
// max-content (the pinned measure_item(None)), then request the list's
// node — Infer sizes the height to min(content, available), Auto
// requests a plain node.
func (u *UniformListElement) RequestLayout(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App) (LayoutID, uniformListFrameState) {
	// The item-0 measurement: render the first item and lay it out as
	// a standalone root under max-content space.
	if u.renderItems != nil && u.count > 0 {
		items := u.renderItems(0, 1, w, app)
		if len(items) > 0 {
			u.measuredItem = items[0].LayoutAsRoot(AvailableSize{
				Width:  MaxContentAvailableSpace(),
				Height: MaxContentAvailableSpace(),
			}, w, app)
		}
	}
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

// Prepaint implements Element: clamp the scroll offset against the
// content size, apply the deferred scroll target, compute the visible
// range, render and lay out only those items, and prepaint each at its
// scrolled origin under the content mask (the pinned prepaint).
func (u *UniformListElement) Prepaint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *uniformListFrameState, w *Window, app *App) struct{} {
	frame := currentFrame(w)
	state := layout
	pitch := state.itemHeight
	viewport := bounds.Size

	// The scroll clamp: the content height is count * pitch; the
	// offset's negative extent is bounded by content - viewport.
	contentHeight := pitch * float32(u.count)
	maxScroll := contentHeight - viewport.Height
	if maxScroll < 0 {
		maxScroll = 0
	}
	offset := u.handle.state.offset
	if offset.Y > 0 {
		offset.Y = 0
	}
	if offset.Y < -maxScroll {
		offset.Y = -maxScroll
	}
	if offset.X != 0 && contentHeight <= viewport.Height {
		offset.X = 0
	}

	// The deferred scroll-to-item (the pinned placement math).
	if pending := u.handle.state.pendingScrollTo; pending != nil {
		u.handle.state.pendingScrollTo = nil
		if pending.index >= 0 && pending.index < u.count && u.count > 0 {
			itemTop := pitch * float32(pending.index)
			itemBottom := itemTop + pitch
			scrollTop := -offset.Y
			isAbove := itemTop < scrollTop
			isBelow := itemBottom > scrollTop+viewport.Height
			strategy := pending.strategy
			if strategy == ScrollNearest {
				if isAbove {
					strategy = ScrollTop
				} else if isBelow {
					strategy = ScrollBottom
				}
			}
			if isAbove || isBelow || pending.strategy == ScrollNearest {
				switch strategy {
				case ScrollTop:
					offset.Y = -clampF32(itemTop, 0, maxScroll)
				case ScrollCenter:
					itemCenter := itemTop + pitch/2
					target := itemCenter - viewport.Height/2
					offset.Y = -clampF32(target, 0, maxScroll)
				case ScrollBottom:
					offset.Y = -clampF32(itemBottom-viewport.Height, 0, maxScroll)
				case ScrollNearest:
					// Visible under nearest: no scroll.
				}
			}
		}
	}
	u.handle.state.offset = offset
	u.handle.state.lastItemSize = Size{Width: viewport.Width, Height: pitch}
	u.handle.state.lastContentSize = Size{Width: viewport.Width, Height: contentHeight}

	// The visible range (the pinned first/last computation).
	if u.count == 0 || pitch <= 0 {
		layout.items = nil
		return struct{}{}
	}
	first := int(math.Floor(float64((-offset.Y) / pitch)))
	last := int(math.Ceil(float64((-offset.Y + viewport.Height) / pitch)))
	if first < 0 {
		first = 0
	}
	if last > u.count {
		last = u.count
	}
	if first > last {
		first = last
	}

	// Render, lay out and prepaint the visible items.
	layout.items = nil
	if u.renderItems != nil && first < last {
		mask := frame.contentMask
		items := u.renderItems(first, last, w, app)
		for i, element := range items {
			ix := first + i
			if ix >= last {
				break
			}
			origin := Point{X: bounds.Origin.X + offset.X, Y: bounds.Origin.Y + offset.Y + pitch*float32(ix)}
			available := AvailableSize{
				Width:  DefiniteAvailableSpace(bounds.Size.Width),
				Height: DefiniteAvailableSpace(pitch),
			}
			element.LayoutAsRoot(available, w, app)
			WithContentMaskVoid(w, intersectMask(mask, bounds), func(w *Window) {
				WithAbsoluteElementOffsetVoid(w, origin, func(w *Window) {
					element.Prepaint(w, app)
				})
			})
			layout.items = append(layout.items, uniformListItem{element: element, origin: origin})
		}
	}
	return struct{}{}
}

// Paint implements Element: paint each visible item at its origin
// under the content mask.
func (u *UniformListElement) Paint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *uniformListFrameState, prepaint *struct{}, w *Window, app *App) {
	frame := currentFrame(w)
	mask := frame.contentMask
	for _, item := range layout.items {
		WithContentMaskVoid(w, intersectMask(mask, bounds), func(w *Window) {
			WithAbsoluteElementOffsetVoid(w, item.origin, func(w *Window) {
				item.element.Paint(w, app)
			})
		})
	}
}

// LayoutNodeStyle implements the root-stretch report.
func (u *UniformListElement) LayoutNodeStyle() (Style, bool) { return u.style, true }

// IntoElement converts the builder into a child element
// (the fixture's IntoElement convention).
func (u *UniformListElement) IntoElement() AnyElement {
	return CustomElement[uniformListFrameState, struct{}](u)
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
