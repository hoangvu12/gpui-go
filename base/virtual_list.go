package base

// A virtual list of differently sized rows or columns (U:virtual_list.rs).
// Only the visible range is rendered; each item's size along the axis must
// be known up front.
//
// Go adaptation (recorded deviation): upstream drives the interactivity of
// its inner Stateful<Div> directly — interactivity().request_layout /
// prepaint / paint with its own measured layout node and its own content
// size (U:virtual_list.rs:460, 660, 751). The core exports no
// Interactivity seam and no ScrollHandle write-back, so the port keeps
// the same inner div (id, size_full, overflow_scroll, lock_scroll_axis,
// track_scroll; U:virtual_list.rs:199-204) as a real child element and
// gives it one child, a content slot sized to the items' content size:
//
//   - the div's scroll clamp measures the slot, which is exactly the
//     content size upstream hands to interactivity().prepaint, and writes
//     the handle's bounds and max offset as upstream's does;
//   - the slot prepaints and paints the visible items, so they run inside
//     the div's hitbox, dispatch node, text style and overflow mask, after
//     its hitbox — where upstream's interactivity closure runs them.
//
// Display (map gap 2b): the inner div is upstream's plain div(), Block
// under the pinned gpui-ce 0.2.2 default; the core defaults to Flex, so
// the port sets Block first (the Base port's convention for constructors
// over a plain upstream div()). The slot also sets flex_shrink 0, so a
// caller restyling the list as a flex container cannot shrink the content
// it measures.
//
// Sizing: under ListSizingBehavior Auto the slot is absolutely positioned,
// so the node sizes from its style alone (upstream's request_layout with
// no children). Under Infer the slot is in flow, so the node's
// content-based size is the items' content size; upstream instead
// measures with a function that takes a definite available size whole
// (U:virtual_list.rs:526-576). The two agree whenever the size is
// definite, including the default size_full.

import (
	"fmt"
	"slices"

	"github.com/hoangvu12/gpui-go/gpui"
)

// deferredScrollToItem is R:elements/uniform_list.rs:101-111
// DeferredScrollToItem, which the core does not export.
type deferredScrollToItem struct {
	// itemIndex is the item index to scroll to.
	itemIndex int
	// strategy is the scroll strategy to use.
	strategy gpui.ScrollStrategy
	// offset is the offset in number of items.
	offset       int
	scrollStrict bool
}

// virtualListScrollHandleState is U:virtual_list.rs:30-36.
type virtualListScrollHandleState struct {
	axis       gpui.Axis
	itemsCount int
	// lastContentSize is the content bounds size (excluding padding and
	// border) from the last frame.
	lastContentSize      gpui.Optional[gpui.Size]
	deferredScrollToItem *deferredScrollToItem
}

// VirtualListScrollHandle is the scroll handle of a VirtualList
// (U:virtual_list.rs:38-45). It embeds its base gpui.ScrollHandle (the
// pinned Deref / AsRef<ScrollHandle>), so the base handle's methods apply
// directly; copies share the state (the pinned Clone of Rc state). The
// zero value is unusable; construct with NewVirtualListScrollHandle.
type VirtualListScrollHandle struct {
	state *virtualListScrollHandleState
	gpui.ScrollHandle
}

// NewVirtualListScrollHandle creates a new handle (U:virtual_list.rs:88-99).
func NewVirtualListScrollHandle() VirtualListScrollHandle {
	return VirtualListScrollHandle{
		state:        &virtualListScrollHandleState{axis: gpui.AxisVertical},
		ScrollHandle: gpui.NewScrollHandle(),
	}
}

// VirtualListScrollHandleFrom wraps an existing scroll handle
// (U:virtual_list.rs:47-53 From<ScrollHandle>).
func VirtualListScrollHandleFrom(handle gpui.ScrollHandle) VirtualListScrollHandle {
	h := NewVirtualListScrollHandle()
	h.ScrollHandle = handle
	return h
}

// BaseHandle returns the base scroll handle (U:virtual_list.rs:101-104).
func (h VirtualListScrollHandle) BaseHandle() gpui.ScrollHandle { return h.ScrollHandle }

// ViewportBounds implements ScrollbarHandle (U:virtual_list.rs:61-64).
func (h VirtualListScrollHandle) ViewportBounds() gpui.Bounds { return h.ScrollHandle.Bounds() }

// ContentSize implements ScrollbarHandle through the base handle's
// (U:virtual_list.rs:74-76).
func (h VirtualListScrollHandle) ContentSize() gpui.Size {
	return scrollHandleContentSize(h.ScrollHandle)
}

// ScrollToItem scrolls to the item at index ix at the list's next
// prepaint (U:virtual_list.rs:106-109).
func (h VirtualListScrollHandle) ScrollToItem(ix int, strategy gpui.ScrollStrategy) {
	h.scrollToItemWithOffset(ix, strategy, 0)
}

// scrollToItemWithOffset adds an offset in items (U:virtual_list.rs:111-120).
func (h VirtualListScrollHandle) scrollToItemWithOffset(ix int, strategy gpui.ScrollStrategy, offset int) {
	h.state.deferredScrollToItem = &deferredScrollToItem{itemIndex: ix, strategy: strategy, offset: offset}
}

// ScrollToBottom scrolls to the bottom of the list (U:virtual_list.rs:122-126).
func (h VirtualListScrollHandle) ScrollToBottom() {
	h.ScrollToItem(max(h.state.itemsCount-1, 0), gpui.ScrollTop)
}

// VVirtualList creates a vertical VirtualList (U:virtual_list.rs:129-150).
// Only the height of each item size is used; the width is measured from
// the item chosen by WithItemToMeasureIndex (the first by default).
// render renders the items [start, end) of view.
func VVirtualList[V, R any, I gpui.IntoElementID](view gpui.Entity[V], id I, itemSizes []gpui.Size, render func(state *V, start, end int, w *gpui.Window, cx *gpui.Context[V]) []R) *VirtualList {
	return NewVirtualList(view, id, gpui.AxisVertical, itemSizes, render)
}

// HVirtualList creates a horizontal VirtualList (U:virtual_list.rs:152-171).
// Only the width of each item size is used; the height is measured.
func HVirtualList[V, R any, I gpui.IntoElementID](view gpui.Entity[V], id I, itemSizes []gpui.Size, render func(state *V, start, end int, w *gpui.Window, cx *gpui.Context[V]) []R) *VirtualList {
	return NewVirtualList(view, id, gpui.AxisHorizontal, itemSizes, render)
}

// NewVirtualList creates a VirtualList along axis (U:virtual_list.rs:
// 173-212, the doc-hidden virtual_list). The Rust Range<usize> is the
// half-open [start, end); each R converts like a child (gpui.TryIntoElement).
func NewVirtualList[V, R any, I gpui.IntoElementID](view gpui.Entity[V], id I, axis gpui.Axis, itemSizes []gpui.Size, render func(state *V, start, end int, w *gpui.Window, cx *gpui.Context[V]) []R) *VirtualList {
	elementID := gpui.ElementIDOf(id)
	scrollHandle := NewVirtualListScrollHandle()
	// The pinned closure owns a strong Entity<V>; a Go handle borrowed in
	// a render callback expires with it, so the closure keeps the weak
	// identity, live for as long as the view renders.
	weak := view.Downgrade()
	renderRange := func(start, end int, w *gpui.Window, app *gpui.App) []gpui.AnyElement {
		elements, err := weak.TryUpdateInWith(app, w, func(state *V, w *gpui.Window, cx *gpui.Context[V]) []gpui.AnyElement {
			items := render(state, start, end, w, cx)
			out := make([]gpui.AnyElement, 0, len(items))
			for _, item := range items {
				element, err := gpui.TryIntoElement(item)
				if err != nil {
					panic(fmt.Sprintf("base: VirtualList item: %v", err))
				}
				out = append(out, element)
			}
			return out
		})
		if err != nil {
			panic(fmt.Sprintf("base: VirtualList view update: %v", err))
		}
		return elements
	}
	list := &VirtualList{
		id:           elementID,
		axis:         axis,
		scrollHandle: scrollHandle,
		itemsCount:   len(itemSizes),
		itemSizes:    itemSizes,
		renderItems:  renderRange,
	}
	list.base = newVirtualListBase(elementID).
		OverflowScroll().
		TrackScroll(scrollHandle.ScrollHandle)
	list.base = InteractiveElementExtOf(list.base).LockScrollAxis()
	return gpui.BindStyled(list)
}

// newVirtualListBase is `div().id(id).size_full()` (Block first: map gap
// 2b).
func newVirtualListBase(id gpui.ElementID) *gpui.DivElement {
	return gpui.Div().Block().ID(id).SizeFull()
}

// VirtualList renders a large number of differently sized items
// (U:virtual_list.rs:214-227). Its style methods style the inner
// scrolling div (the pinned `impl Styled` delegating to base.style()).
type VirtualList struct {
	gpui.Styled[*VirtualList]
	id                 gpui.ElementID
	axis               gpui.Axis
	base               *gpui.DivElement
	scrollHandle       VirtualListScrollHandle
	itemsCount         int
	itemSizes          []gpui.Size
	renderItems        func(start, end int, w *gpui.Window, app *gpui.App) []gpui.AnyElement
	sizingBehavior     gpui.ListSizingBehavior
	itemToMeasureIndex int

	// Per-frame plumbing between the element phases and the slot.
	baseElement   gpui.AnyElement
	frame         *VirtualListFrameState
	scrollOffset  gpui.Point
	contentBounds gpui.Bounds
	paddings      gpui.Edges
	bounds        gpui.Bounds
}

// TrackScroll tracks the scroll state with handle (U:virtual_list.rs:236-240).
func (l *VirtualList) TrackScroll(handle VirtualListScrollHandle) *VirtualList {
	l.base.TrackScroll(handle.ScrollHandle)
	l.scrollHandle = handle
	return l
}

// WithSizingBehavior sets the sizing behavior (U:virtual_list.rs:242-246).
func (l *VirtualList) WithSizingBehavior(behavior gpui.ListSizingBehavior) *VirtualList {
	l.sizingBehavior = behavior
	return l
}

// WithItemToMeasureIndex sets the item measured for the cross-axis size
// (U:virtual_list.rs:248-252).
func (l *VirtualList) WithItemToMeasureIndex(index int) *VirtualList {
	l.itemToMeasureIndex = index
	return l
}

// WithScrollHandle is for the table, whose scroll handle belongs to its
// header rather than to a virtual list: the inner div is replaced by a
// plain `div().id(id).size_full()` (styles set before are dropped with
// it, like upstream's replaced base) and handle is adopted
// (U:virtual_list.rs:254-262, doc-hidden).
func (l *VirtualList) WithScrollHandle(handle VirtualListScrollHandle) *VirtualList {
	l.base = newVirtualListBase(l.id)
	*l.StyleMut() = gpui.StyleRefinement{}
	l.scrollHandle = handle
	return l
}

// scrollToDeferredItem is U:virtual_list.rs:264-311.
func (l *VirtualList) scrollToDeferredItem(scrollOffset gpui.Point, sizeLayout *ItemSizeLayout, contentBounds gpui.Bounds, scrollTo deferredScrollToItem) gpui.Point {
	bounds, ok := sizeLayout.itemBounds(scrollTo.itemIndex+scrollTo.offset, l.axis, contentBounds)
	if !ok {
		return scrollOffset
	}
	vertical := l.axis == gpui.AxisVertical
	switch scrollTo.strategy {
	case gpui.ScrollCenter:
		if vertical {
			scrollOffset.Y = contentBounds.Top() + contentBounds.Size.Height/2 - bounds.Top() - bounds.Size.Height/2
		} else {
			scrollOffset.X = contentBounds.Left() + contentBounds.Size.Width/2 - bounds.Left() - bounds.Size.Width/2
		}
	default:
		// R:elements/div.rs scroll_to_active_item's minimal reveal.
		if vertical {
			if bounds.Top()+scrollOffset.Y < contentBounds.Top() {
				scrollOffset.Y = contentBounds.Top() - bounds.Top()
			} else if bounds.Bottom()+scrollOffset.Y > contentBounds.Bottom() {
				scrollOffset.Y = contentBounds.Bottom() - bounds.Bottom()
			}
		} else {
			if bounds.Left()+scrollOffset.X < contentBounds.Left() {
				scrollOffset.X = contentBounds.Left() - bounds.Left()
			} else if bounds.Right()+scrollOffset.X > contentBounds.Right() {
				scrollOffset.X = contentBounds.Right() - bounds.Right()
			}
		}
	}
	l.scrollHandle.SetOffset(scrollOffset)
	return scrollOffset
}

// measureItem lays the measured item out as a root
// (U:virtual_list.rs:313-336).
func (l *VirtualList) measureItem(listWidth gpui.Optional[float32], w *gpui.Window, app *gpui.App) gpui.Size {
	if l.itemsCount == 0 {
		return gpui.Size{}
	}
	ix := min(l.itemToMeasureIndex, l.itemsCount-1)
	items := l.renderItems(ix, ix+1, w, app)
	if len(items) == 0 {
		return gpui.Size{}
	}
	item := items[len(items)-1]
	available := gpui.AvailableSize{Width: gpui.MinContentAvailableSpace(), Height: gpui.MinContentAvailableSpace()}
	if width, ok := listWidth.Get(); ok {
		available.Width = gpui.DefiniteAvailableSpace(width)
	}
	return item.LayoutAsRoot(available, w, app)
}

// VirtualListFrameState is the frame state (U:virtual_list.rs:339-344):
// the visible items to paint, with the origins they prepainted at (the
// port's drawables re-resolve bounds at paint).
type VirtualListFrameState struct {
	items      []gpui.AnyElement
	origins    []gpui.Point
	sizeLayout ItemSizeLayout
}

// ItemSizeLayout holds the per-item sizes along the list axis, gap
// included, and their prefix sums (U:virtual_list.rs:346-357).
type ItemSizeLayout struct {
	itemsSizes       []gpui.Size
	contentSize      gpui.Size
	sizes            []float32
	origins          []float32
	lastLayoutBounds gpui.Bounds
}

// itemBounds is the bounds of item ix in the list's content space: offset
// from the content origin along the axis, filling it on the other
// (U:virtual_list.rs:359-381).
func (s *ItemSizeLayout) itemBounds(ix int, axis gpui.Axis, contentBounds gpui.Bounds) (gpui.Bounds, bool) {
	if ix < 0 || ix >= len(s.origins) {
		return gpui.Bounds{}, false
	}
	origin := s.origins[ix]
	itemSize := s.sizes[ix]
	if axis == gpui.AxisHorizontal {
		return gpui.Bounds{
			Origin: gpui.Point{X: contentBounds.Left() + origin},
			Size:   gpui.Size{Width: itemSize, Height: contentBounds.Size.Height},
		}, true
	}
	return gpui.Bounds{
		Origin: gpui.Point{Y: contentBounds.Top() + origin},
		Size:   gpui.Size{Width: contentBounds.Size.Width, Height: itemSize},
	}, true
}

// visibleRange returns the items intersecting the viewport [start, end)
// along the axis plus one item of overdraw past its end; both edges are
// binary searches (U:virtual_list.rs:383-412).
func visibleRange(origins, sizes []float32, start, end float32) (int, int) {
	count := len(origins)
	// Item ends grow with the index, so the first item ending past edge is
	// a partition point.
	endsBefore := func(edge float32) int {
		low, high := 0, count
		for low < high {
			mid := low + (high-low)/2
			if origins[mid]+sizes[mid] <= edge {
				low = mid + 1
			} else {
				high = mid
			}
		}
		return low
	}
	first := endsBefore(start)
	pastEnd := endsBefore(end)
	last := count
	if pastEnd != count {
		last = min(pastEnd+2, count)
	}
	return first, max(last, first)
}

// definiteToPixels is R:geometry.rs:3616-3621 DefiniteLength::to_pixels
// with a pixel base size.
func definiteToPixels(length gpui.DefiniteLength, base, rem float32) float32 {
	switch length.Kind {
	case gpui.DefiniteLengthPx:
		return length.Value
	case gpui.DefiniteLengthRem:
		return length.Value * rem
	}
	return length.Value * base
}

// IntoElement converts the list into its element (U:virtual_list.rs:414-420).
func (l *VirtualList) IntoElement() gpui.AnyElement {
	return gpui.CustomElement[VirtualListFrameState, struct{}](virtualListElement{l})
}

// virtualListElement is the Element impl of VirtualList
// (U:virtual_list.rs:422-765).
type virtualListElement struct{ l *VirtualList }

// ID implements gpui.Element (U:virtual_list.rs:426-428).
func (e virtualListElement) ID() (gpui.ElementID, bool) { return e.l.id, true }

// SourceLocation implements gpui.Element (U:virtual_list.rs:430-432).
func (e virtualListElement) SourceLocation() *gpui.SourceLocation { return nil }

// LayoutNodeStyle reports the inner div's base style for the window
// root's auto-size stretch (the core's root-stretch report).
func (e virtualListElement) LayoutNodeStyle() (gpui.Style, bool) {
	style := gpui.DefaultStyle()
	refinement := e.l.base.Style()
	style.Refine(&refinement)
	return style, true
}

// divGlobal is the inner div's global id: the list's path plus the div's
// own id (the same id, U:virtual_list.rs:199-200).
func (l *VirtualList) divGlobal(global *gpui.GlobalElementID) *gpui.GlobalElementID {
	if global == nil {
		return nil
	}
	return &gpui.GlobalElementID{Path: append(slices.Clone(global.Path), l.id)}
}

// RequestLayout measures the cross axis, prepares the item sizes and
// origins, and requests the inner div with its content slot
// (U:virtual_list.rs:434-595).
func (e virtualListElement) RequestLayout(global *gpui.GlobalElementID, _ *gpui.InspectorElementID, w *gpui.Window, app *gpui.App) (gpui.LayoutID, VirtualListFrameState) {
	l := e.l
	if global == nil {
		panic("base: VirtualList requires an element id")
	}
	rem := gpui.RemSize(w)
	fontSize := gpui.WindowTextStyle(w).FontSizePixels(rem)
	// Measure vertical lists at the last known content width, so relative
	// widths and truncation resolve like the visible rows; an unconstrained
	// measure would turn one long non-wrapping text into a phantom
	// horizontal scroll range.
	var listWidth gpui.Optional[float32]
	if l.axis == gpui.AxisVertical {
		if size, ok := l.scrollHandle.state.lastContentSize.Get(); ok && size.Width != 0 {
			listWidth = gpui.Some(size.Width)
		}
	}
	longestItemSize := l.measureItem(listWidth, w, app)

	// The inner div carries the list's style (the pinned delegation).
	refinement := l.Style()
	l.base.StyleMut().Refine(&refinement)
	style := l.base.ComputeStyle(l.divGlobal(global), nil, w, app)

	sizeLayout := gpui.WithElementState(w, global, func(slot *ItemSizeLayout, _ *gpui.Window) (ItemSizeLayout, ItemSizeLayout) {
		state := ItemSizeLayout{}
		if slot != nil {
			state = *slot
		}

		// The gap between items is part of each item's size.
		gapLength := style.Gap.Height
		if l.axis == gpui.AxisHorizontal {
			gapLength = style.Gap.Width
		}
		gap := definiteToPixels(gapLength, fontSize, rem)

		if !slices.Equal(state.itemsSizes, l.itemSizes) {
			state.itemsSizes = l.itemSizes
			state.sizes = make([]float32, len(l.itemSizes))
			for i, size := range l.itemSizes {
				along := size.Along(l.axis)
				if i+1 == l.itemsCount {
					state.sizes[i] = along
				} else {
					state.sizes[i] = along + gap
				}
			}
			cumulative := float32(0)
			state.origins = make([]float32, len(state.sizes))
			for i, size := range state.sizes {
				state.origins[i] = cumulative
				cumulative += size
			}
			if l.axis == gpui.AxisHorizontal {
				state.contentSize.Width = cumulative
			} else {
				state.contentSize.Height = cumulative
			}
		}

		if l.axis == gpui.AxisHorizontal {
			state.contentSize.Height = longestItemSize.Height
		} else {
			state.contentSize.Width = longestItemSize.Width
		}
		return state, state
	})

	l.base.Child(gpui.CustomElement[struct{}, struct{}](virtualListSlot{l: l, contentSize: sizeLayout.contentSize, inFlow: l.sizingBehavior == gpui.ListInfer}))
	l.baseElement = l.base.IntoElement()
	layoutID := l.baseElement.RequestLayout(w, app)
	return layoutID, VirtualListFrameState{sizeLayout: sizeLayout}
}

// Prepaint resolves the content bounds and the scroll offset (the
// deferred scroll-to-item, then the clamp), then prepaints the inner div,
// whose slot prepaints the visible items (U:virtual_list.rs:597-739).
func (e virtualListElement) Prepaint(global *gpui.GlobalElementID, _ *gpui.InspectorElementID, bounds gpui.Bounds, layout *VirtualListFrameState, w *gpui.Window, app *gpui.App) struct{} {
	l := e.l
	layout.sizeLayout.lastLayoutBounds = bounds

	style := l.base.ComputeStyle(l.divGlobal(global), nil, w, app)
	rem := gpui.RemSize(w)
	borderWidths := style.BorderWidths.ToPixels(rem)
	padding := style.Padding
	paddings := gpui.Edges{
		Top:    definiteToPixels(padding.Top, bounds.Size.Height, rem),
		Right:  definiteToPixels(padding.Right, bounds.Size.Width, rem),
		Bottom: definiteToPixels(padding.Bottom, bounds.Size.Height, rem),
		Left:   definiteToPixels(padding.Left, bounds.Size.Width, rem),
	}
	bottomRight := bounds.BottomRight()
	topLeft := gpui.Point{
		X: bounds.Origin.X + borderWidths.Left + paddings.Left,
		Y: bounds.Origin.Y + borderWidths.Top + paddings.Top,
	}
	contentBounds := gpui.Bounds{
		Origin: topLeft,
		Size: gpui.Size{
			Width:  bottomRight.X - (borderWidths.Right + paddings.Right) - topLeft.X,
			Height: bottomRight.Y - (borderWidths.Bottom + paddings.Bottom) - topLeft.Y,
		},
	}

	scrollState := l.scrollHandle.state
	scrollState.axis = l.axis
	scrollState.itemsCount = l.itemsCount
	scrollState.lastContentSize = gpui.Some(contentBounds.Size)

	scrollOffset := l.scrollHandle.Offset()
	if deferred := scrollState.deferredScrollToItem; deferred != nil {
		scrollState.deferredScrollToItem = nil
		scrollOffset = l.scrollToDeferredItem(scrollOffset, &layout.sizeLayout, contentBounds, *deferred)
	}

	content := layout.sizeLayout.contentSize
	scrollOffset.X = min(max(scrollOffset.X, contentBounds.Size.Width-content.Width), 0)
	scrollOffset.Y = min(max(scrollOffset.Y, contentBounds.Size.Height-content.Height), 0)
	if scrollOffset != l.scrollHandle.Offset() {
		l.scrollHandle.SetOffset(scrollOffset)
	}

	l.frame = layout
	l.scrollOffset = scrollOffset
	l.contentBounds = contentBounds
	l.paddings = paddings
	l.bounds = bounds
	l.baseElement.Prepaint(w, app)
	return struct{}{}
}

// Paint paints the inner div, whose slot paints the items
// (U:virtual_list.rs:741-764).
func (e virtualListElement) Paint(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, _ gpui.Bounds, layout *VirtualListFrameState, _ *struct{}, w *gpui.Window, app *gpui.App) {
	e.l.frame = layout
	e.l.baseElement.Paint(w, app)
}

// virtualListSlot is the inner div's content slot: its node carries the
// content size the div's scroll clamp measures, and its phases run the
// item body of upstream's interactivity closures.
type virtualListSlot struct {
	l           *VirtualList
	contentSize gpui.Size
	inFlow      bool
}

func (s virtualListSlot) ID() (gpui.ElementID, bool)           { return gpui.ElementID{}, false }
func (s virtualListSlot) SourceLocation() *gpui.SourceLocation { return nil }

// RequestLayout sizes the slot to the content size.
func (s virtualListSlot) RequestLayout(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, w *gpui.Window, _ *gpui.App) (gpui.LayoutID, struct{}) {
	style := gpui.DefaultStyle()
	style.Size.Width = gpui.PxLength(s.contentSize.Width)
	style.Size.Height = gpui.PxLength(s.contentSize.Height)
	style.FlexShrink = 0
	if !s.inFlow {
		style.Position = gpui.PositionAbsolute
	}
	id, err := gpui.RequestElementLayout(w, style)
	if err != nil {
		panic(fmt.Sprintf("base: VirtualList content request_layout: %v", err))
	}
	return id, struct{}{}
}

// Prepaint renders, lays out and prepaints the visible items
// (U:virtual_list.rs:667-737).
func (s virtualListSlot) Prepaint(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, _ gpui.Bounds, _ *struct{}, w *gpui.Window, app *gpui.App) struct{} {
	l := s.l
	layout := l.frame
	if l.itemsCount == 0 {
		return struct{}{}
	}
	scrollOffset := l.scrollOffset
	contentBounds := l.contentBounds
	itemSizes := layout.sizeLayout.sizes
	itemOrigins := layout.sizeLayout.origins
	minScrollOffset := contentBounds.Size.Along(l.axis) - layout.sizeLayout.contentSize.Along(l.axis)

	if scrollOffset.Along(l.axis) != 0 {
		switch {
		case l.axis == gpui.AxisHorizontal && scrollOffset.X < minScrollOffset:
			scrollOffset.X = minScrollOffset
			l.scrollHandle.SetOffset(scrollOffset)
		case l.axis == gpui.AxisVertical && scrollOffset.Y < minScrollOffset:
			scrollOffset.Y = minScrollOffset
			l.scrollHandle.SetOffset(scrollOffset)
		}
	}

	// The viewport in content space: the scroll offset is negative, and
	// the leading padding is admitted at the start so an item under it
	// is still drawn.
	var viewStart, viewEnd float32
	if l.axis == gpui.AxisHorizontal {
		viewStart, viewEnd = -(scrollOffset.X + l.paddings.Left), -scrollOffset.X+contentBounds.Size.Width
	} else {
		viewStart, viewEnd = -(scrollOffset.Y + l.paddings.Top), -scrollOffset.Y+contentBounds.Size.Height
	}
	first, last := visibleRange(itemOrigins, itemSizes, viewStart, viewEnd)

	items := l.renderItems(first, last, w, app)

	bounds := l.bounds
	gpui.WithContentMaskVoid(w, &bounds, func(w *gpui.Window) {
		for i, item := range items {
			ix := first + i
			if ix >= last {
				break
			}
			var origin gpui.Point
			var available gpui.AvailableSize
			if l.axis == gpui.AxisHorizontal {
				origin = gpui.Point{X: contentBounds.Origin.X + itemOrigins[ix] + scrollOffset.X, Y: contentBounds.Origin.Y + scrollOffset.Y}
				available = gpui.AvailableSize{Width: gpui.DefiniteAvailableSpace(itemSizes[ix]), Height: gpui.DefiniteAvailableSpace(contentBounds.Size.Height)}
			} else {
				origin = gpui.Point{X: contentBounds.Origin.X + scrollOffset.X, Y: contentBounds.Origin.Y + itemOrigins[ix] + scrollOffset.Y}
				available = gpui.AvailableSize{Width: gpui.DefiniteAvailableSpace(contentBounds.Size.Width), Height: gpui.DefiniteAvailableSpace(itemSizes[ix])}
			}
			item.LayoutAsRoot(available, w, app)
			item.PrepaintAt(origin, w, app)
			layout.items = append(layout.items, item)
			layout.origins = append(layout.origins, origin)
		}
	})
	return struct{}{}
}

// Paint paints the items at the origins they prepainted at
// (U:virtual_list.rs:758-762).
func (s virtualListSlot) Paint(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, _ gpui.Bounds, _ *struct{}, _ *struct{}, w *gpui.Window, app *gpui.App) {
	layout := s.l.frame
	for i, item := range layout.items {
		gpui.WithAbsoluteElementOffsetVoid(w, layout.origins[i], func(w *gpui.Window) {
			item.Paint(w, app)
		})
	}
}
