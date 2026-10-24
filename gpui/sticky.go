package gpui

// The sticky positioning slice (browser-core ticket 34, spec L3):
// position: sticky lays out in flow (the record translation maps it
// onto Taffy's Relative with the insets NOT applied as relative
// offsets — they are scroll constraints, not offsets) and is shifted,
// at prepaint, so the box stays inside its scroll container's
// inset-adjusted scrollport while never leaving its containing block.
//
// Chromium's model, verified in %TEMP%\research-src\chromium
// (0d4dcd4ffc1d, third_party/blink/renderer/core):
//
//   - The constraints are computed AFTER layout, per axis, against the
//     NEAREST SCROLL CONTAINER (the containing-block chain's first
//     IsScrollContainer ancestor — PaintLayer::ContainingScrollContainer-
//     Layer, paint_layer.cc:275-292): the sticky box's border-box rect,
//     its containing block's rect, and the constraining rect
//     (LayoutBoxModelObject::ComputeStickyPositionConstraints,
//     layout_box_model_object.cc:548-717). The containing block of a
//     sticky element is its ordinary FLOW containing block (StickyContainer
//     = ContainingBlock, layout_box_model_object.cc:544-546) — not the
//     nearest positioned ancestor — and when that block is the scroller
//     itself the rect is the scroller's scrollable overflow; otherwise
//     it is the block's padding box contracted by the block's padding
//     (the flow rectangle) and by the STICKY element's own margins
//     (layout_box_model_object.cc:583-615). The constraining rect is the
//     scroller's overflow clip rect contracted by border, padding and
//     scrollbars — the CONTENT box in scrolling coordinates
//     (LayoutBox::ComputeStickyConstrainingRect, layout_box.cc:4312-4322:
//     OverflowClipRect, border-offset-subtracted, padding-contracted; a
//     scroller's padding therefore pushes the sticky inset basis inward,
//     position-sticky-overflow-padding.html).
//   - The offset is one BoxEdge shift per axis with 'top'/'left'
//     overriding 'bottom'/'right' (StickyPositionScrollingConstraints::
//     PerAxisData::ComputeOffset, core/page/scrolling/
//     sticky_position_scrolling_constraints.cc:65-144): shift the box to
//     the inset-adjusted scrollport edge, clamped to the containing
//     block. Both insets set with insufficient room reduce the END
//     inset (layout_box_model_object.cc:668-680).
//   - The offset is applied at PAINT time as a translation
//     (FragmentPaintPropertyTreeBuilder::UpdateStickyTranslation,
//     paint_property_tree_builder.cc:922-935 — rounded to whole device
//     pixels there), recomputed from the current scroll offset by the
//     scroller (PaintLayerScrollableArea::UpdateAllStickyConstraints,
//     paint_layer_scrollable_area.cc:2397-2413) without re-layout.
//     getBoundingClientRect and offsetTop carry it
//     (position-sticky-get-bounding-client-rect.html), so the port's
//     debug bounds must too.
//
// The port keeps the same split: the native tree never sees the sticky
// offset (Taffy has no sticky value), and the div's prepaint shifts the
// element's bounds and opens the shift as the children's element
// offset, so hitboxes, debug bounds, anchors and the paint all move
// together — a scroll only changes the prepaint inputs (the offset
// cell), never the layout.
//
// This file works in WINDOW coordinates. Chromium computes in
// scrolling ("content") coordinates where the scrollport moves by the
// scroll offset and the box does not; mapping both through the scroller's
// padding box shows the two formulations agree exactly (the scroll
// offset cancels), because the port's element-offset mechanism already
// carries the scroll — and any enclosing sticky — into the box's and
// the containing block's resolved bounds:
//
//	sticky shift = clamp to [CB.start, CB.end] of
//	    (scrollport content-box edge ∓ inset) − box edge
//
// Nested sticky follows for free: an ancestor sticky's offset sits in
// frame.elementOffset while its descendants resolve bounds, so an inner
// sticky's box AND containing block both carry it (Chromium's
// FindNearestStickyLayerShiftingContainingBlock case,
// sticky_position_scrolling_constraints.cc:75-89) while the scrollport
// does not. The remaining Chromium case — a sticky ancestor strictly
// between the sticky and its containing block — needs inline splits
// the port does not support (see the limitations in the ticket answer).

import "github.com/hoangvu12/gpui-go/internal/native"

// stickyScrollContext is one open scroll-container scope for sticky
// descendants, pushed by the scroller's prepaint (the div, or a custom
// scroller through WithStickyScrollContainer) around its child walk.
// Everything is precomputed at push time (the scroll offset is constant
// for the whole frame walk) in window coordinates.
type stickyScrollContext struct {
	// constraining is the scroller's content box in window coordinates
	// (Chromium's ComputeStickyConstrainingRect): the sticky inset
	// basis. It is fixed — the scrollport does not move in window
	// space.
	constraining Bounds
	// selfCB is the sticky containing-block rect in window coordinates
	// for stickies whose flow containing block is this scroller itself:
	// the scroller's scrollable overflow extent contracted by its
	// padding (layout_box_model_object.cc:583-615), at the current
	// scroll position. It moves with the content.
	selfCB Bounds
	// scrollerNode is the scroller's native layout node, zero for a
	// custom scroller (whose headers resolve their containing blocks
	// through the StickyOffset hook, not the flow-box registry).
	scrollerNode native.NodeHandle
	// scrollsX and scrollsY report whether the axis is a CSS scroll-
	// container axis — every overflow value but visible and clip, per
	// ComputedStyle::IsScrollContainer (browser-core 36; Chromium's
	// ScrollableAxes, paint_layer_scrollable_area.cc:2520-2542). A
	// hidden axis is one: its sticky descendants resolve constraints
	// against a scrollport that never moves, so they stay put — the CSS
	// behaviour (ticket 34's recorded OverflowHidden limitation, closed
	// by this ticket).
	scrollsX, scrollsY bool
}

// stickyScrollContextOf builds one scroll container's context from its
// prepaint facts: the style (borders, overflow axes), the border-box
// bounds, the resolved padding, the clamped scroll maximum and the
// clamped offset (negative when scrolled down/right), and the
// scroller's native node (zero for custom scrollers). padding is the
// scroller's record-resolved padding for divs (browser-core 07: the
// percentages resolved against the containing block's width, exactly
// what Taffy laid out), and the style-resolved values for custom
// scrollers (WithStickyScrollContainer), which have no node of their
// own to read.
func stickyScrollContextOf(w *Window, style *Style, bounds Bounds, padding Edges, maxOffset, offset Point, node native.NodeHandle) stickyScrollContext {
	rem := RemSize(w)
	// The clamp-lined border widths (browser-core 07): the scrollport
	// insets by exactly the borders the layout used.
	borders := style.borderEdges(rem)
	padTop, padBottom, padLeft, padRight := padding.Top, padding.Bottom, padding.Left, padding.Right
	// The content box: the border box contracted by border and padding
	// (ComputeStickyConstrainingRect). Nothing is clamped at zero — a
	// scroll container always has non-negative inner space by layout.
	constraining := Bounds{
		Origin: Point{
			X: bounds.Origin.X + borders.Left + padLeft,
			Y: bounds.Origin.Y + borders.Top + padTop,
		},
		Size: Size{
			Width:  bounds.Size.Width - borders.Left - borders.Right - padLeft - padRight,
			Height: bounds.Size.Height - borders.Top - borders.Bottom - padTop - padBottom,
		},
	}
	// The scrollable overflow extent (the content box plus the padding
	// plus the clamped scroll maximum) contracted by the padding, at
	// the current offset: the flow rectangle a sticky child of THIS
	// scroller may roam.
	scrollableWidth := constraining.Size.Width + padLeft + padRight + maxOffset.X
	scrollableHeight := constraining.Size.Height + padTop + padBottom + maxOffset.Y
	selfCB := Bounds{
		Origin: Point{
			X: constraining.Origin.X + offset.X,
			Y: constraining.Origin.Y + offset.Y,
		},
		Size: Size{
			Width:  scrollableWidth - padLeft - padRight,
			Height: scrollableHeight - padTop - padBottom,
		},
	}
	return stickyScrollContext{
		constraining: constraining,
		selfCB:       selfCB,
		scrollerNode: node,
		scrollsX:     overflowAxisScrollable(style.OverflowX),
		scrollsY:     overflowAxisScrollable(style.OverflowY),
	}
}

// WithStickyScrollContainer opens one scroll container's sticky scope
// around f (browser-core ticket 34): sticky descendants — divs with
// Sticky(), and custom elements calling StickyOffset — resolve their
// constraints against THIS scroller (the nearest one wins, as
// PaintLayer::ContainingScrollContainerLayer walks the ancestor chain)
// while the scope is open. style is the scroller's computed style
// (borders, padding and the overflow axes), bounds its border-box
// bounds in window coordinates, maxOffset and offset the clamped
// scroll maximum and the clamped offset of this frame
// (elementScroll.clamp's results — negative offsets scroll down/right).
// Custom
// scrollers that render items themselves (the uniform list shape) pass
// their containing-block rects directly to StickyOffset. The padding
// resolves from the style here (custom scrollers have no layout node
// to read the record-resolved values from — browser-core 07's
// percentage-basis fix covers the div scrollers).
func WithStickyScrollContainer[R any](w *Window, style *Style, bounds Bounds, maxOffset, offset Point, f func(*Window) R) R {
	rem := RemSize(w)
	snap := func(v float32) float32 { return PixelSnap(w, v) }
	padding := Edges{
		Top:    snap(resolveDefiniteListLength(style.Padding.Top, bounds.Size.Height, rem)),
		Bottom: snap(resolveDefiniteListLength(style.Padding.Bottom, bounds.Size.Height, rem)),
		Left:   snap(resolveDefiniteListLength(style.Padding.Left, bounds.Size.Width, rem)),
		Right:  snap(resolveDefiniteListLength(style.Padding.Right, bounds.Size.Width, rem)),
	}
	ctx := stickyScrollContextOf(w, style, bounds, padding, maxOffset, offset, 0)
	frame := currentFrame(w)
	frame.stickyScrollers = append(frame.stickyScrollers, ctx)
	defer func() { frame.stickyScrollers = frame.stickyScrollers[:len(frame.stickyScrollers)-1] }()
	return f(w)
}

// StickyOffset computes the sticky positioning offset of one element
// (browser-core ticket 34) against the innermost open
// WithStickyScrollContainer scope: the shift that keeps the element's
// box inside the scroller's inset-adjusted scrollport while never
// leaving its containing block, per axis, with top/left overriding
// bottom/right (css-position-3 §3.4;
// sticky_position_scrolling_constraints.cc:65-144). style is the
// element's computed style (Position must be PositionSticky; the inset
// edges are the constraints and resolve against the scroller's content
// box, the element's own margins contract the containing block); box is
// the element's border-box bounds in window coordinates (the bounds it
// would paint at without sticky); containingBlock is its flow
// containing block's CONTENT box in window coordinates (border and
// padding already contracted). The caller applies the result to the
// element's bounds and to its children's element offset — the div does
// both in its prepaint; a virtualized list adds it to the origin it
// prepaints its header at.
//
// The offset is zero when no scroller scope is open (the port has no
// root/document scroller), when the element is not sticky, or when no
// inset constrains the axis — an all-auto sticky behaves like a plain
// in-flow box, as Chromium's HasScrollDependentOffset note records
// (sticky_position_scrolling_constraints.cc:158-163).
func StickyOffset(w *Window, style *Style, box, containingBlock Bounds) Point {
	return stickyOffsetOf(w, style, box, containingBlock)
}

// stickyOffsetOf is StickyOffset's engine: the frame's innermost
// scroller context plus Chromium's per-axis clamp math.
func stickyOffsetOf(w *Window, style *Style, box, cbContent Bounds) Point {
	if style == nil || style.Position != PositionSticky {
		return Point{}
	}
	frame := currentFrame(w)
	n := len(frame.stickyScrollers)
	if n == 0 {
		return Point{}
	}
	sc := frame.stickyScrollers[n-1]
	rem := RemSize(w)
	cb := contractStickyContainingBlock(cbContent, style, rem)
	var offset Point
	if sc.scrollsY {
		offset.Y = stickyAxisOffset(true, sc.constraining, box, cb, &style.Inset, rem)
	}
	if sc.scrollsX {
		offset.X = stickyAxisOffset(false, sc.constraining, box, cb, &style.Inset, rem)
	}
	return offset
}

// stickyAxisOffset is one axis's shift (PerAxisData::ComputeOffset,
// sticky_position_scrolling_constraints.cc:65-144) in window
// coordinates. vertical selects the y axis (min = top, max = bottom);
// the horizontal axis maps min = left, max = right.
func stickyAxisOffset(vertical bool, constraining, box, cb Bounds, insets *LengthEdges, rem float32) float32 {
	var minLen, maxLen Length
	var availSize, boxSize float32
	var cStart, cEnd, bStart, bEnd, cbStart, cbEnd float32
	if vertical {
		minLen, maxLen = insets.Top, insets.Bottom
		availSize, boxSize = constraining.Size.Height, box.Size.Height
		cStart, cEnd = constraining.Origin.Y, constraining.Bottom()
		bStart, bEnd = box.Origin.Y, box.Bottom()
		cbStart, cbEnd = cb.Origin.Y, cb.Bottom()
	} else {
		minLen, maxLen = insets.Left, insets.Right
		availSize, boxSize = constraining.Size.Width, box.Size.Width
		cStart, cEnd = constraining.Origin.X, constraining.Right()
		bStart, bEnd = box.Origin.X, box.Right()
		cbStart, cbEnd = cb.Origin.X, cb.Right()
	}
	minInset, minSet := stickyInsetPixels(minLen, availSize, rem)
	maxInset, maxSet := stickyInsetPixels(maxLen, availSize, rem)

	// Both insets set with insufficient room reduce the END inset
	// (layout_box_model_object.cc:668-680; 'left' overrides 'right' and
	// 'top' overrides 'bottom', css-position-3 §3.4).
	if minSet && maxSet {
		if free := availSize - boxSize - minInset - maxInset; free < 0 {
			maxInset += free
		}
	}

	// The end side (bottom/right): shift the box's far edge to the
	// scrollport's far edge minus the inset, clamped so the box's near
	// edge never passes the containing block's near edge
	// (:104-118 — ClampPositiveToZero keeps non-positive shifts).
	maxShift := float32(0)
	if maxSet {
		delta := (cEnd - maxInset) - bEnd
		available := min32(cbStart-bStart, 0)
		if delta > 0 {
			delta = 0
		}
		if delta < available {
			delta = available
		}
		maxShift = delta
	}
	// The start side (top/left): shift the box's near edge to the
	// scrollport's near edge plus the inset, clamped so the box's far
	// edge never passes the containing block's far edge
	// (:120-134 — ClampNegativeToZero keeps non-negative shifts).
	minShift := float32(0)
	if minSet {
		delta := (cStart + minInset) - bStart
		available := max32(cbEnd-bEnd, 0)
		if delta < 0 {
			delta = 0
		}
		if delta > available {
			delta = available
		}
		minShift = delta
	}
	return minShift + maxShift
}

// stickyInsetPixels resolves one inset edge against the constraining
// size (MinimumValueForLength's basis is the scrollport, not the
// containing block — layout_box_model_object.cc:655-661): auto insets
// are unset (they do not constrain), rems resolve against the window
// rem, percentages against the constraining size.
func stickyInsetPixels(length Length, availSize, rem float32) (float32, bool) {
	if length.Auto {
		return 0, false
	}
	switch length.Definite.Kind {
	case DefiniteLengthPx:
		return length.Definite.Value, true
	case DefiniteLengthRem:
		return length.Definite.Value * rem, true
	default:
		return length.Definite.Value * availSize, true
	}
}

// contractStickyContainingBlock contracts the containing block's
// content box by the STICKY element's own margins
// (layout_box_model_object.cc:610-615): the space the element's
// margins reserve keeps the box a margin's width inside its flow
// rectangle. Auto margins contract nothing; percentages resolve
// against the containing block's content width (the
// MinimumValueForLength(max_width) basis).
func contractStickyContainingBlock(cb Bounds, style *Style, rem float32) Bounds {
	margin := func(length Length) float32 {
		if length.Auto {
			return 0
		}
		switch length.Definite.Kind {
		case DefiniteLengthPx:
			return length.Definite.Value
		case DefiniteLengthRem:
			return length.Definite.Value * rem
		default:
			return length.Definite.Value * cb.Size.Width
		}
	}
	edges := style.Margin
	top := margin(edges.Top)
	right := margin(edges.Right)
	bottom := margin(edges.Bottom)
	left := margin(edges.Left)
	width := cb.Size.Width - left - right
	height := cb.Size.Height - top - bottom
	if width < 0 {
		width = 0
	}
	if height < 0 {
		height = 0
	}
	return Bounds{
		Origin: Point{X: cb.Origin.X + left, Y: cb.Origin.Y + top},
		Size:   Size{Width: width, Height: height},
	}
}

// stickyFlowBoxOf resolves a sticky div's flow containing-block rect:
// the scroller's own scrollable extent when the sticky's layout parent
// IS the innermost scroller (layout_box_model_object.cc:583-587's
// arm), else the parent's entry in the frame's flow-box registry (its
// content box recorded at its prepaint — a scroller records its
// scrollable extent instead, :589-615). The parent prepaints before
// its children, so its entry exists when this runs. A parent with no
// entry (a custom container that is not a div) falls back to its
// border-box layout bounds, uncontracted — a documented approximation
// (custom containers pass their containing blocks explicitly through
// StickyOffset instead). The root (no parent) reports no containing
// block: the port's window root is not a scroll container, so there is
// nothing to clamp against.
func stickyFlowBoxOf(w *Window, own LayoutID, sc stickyScrollContext) (Bounds, bool) {
	engine := own.engine
	parent, err := engine.native.NodeParent(own.node)
	if err != nil || parent == 0 {
		return Bounds{}, false
	}
	if sc.scrollerNode != 0 && parent == sc.scrollerNode {
		// The classic case: the sticky's flow containing block IS the
		// scroll container (its direct child) — the scrollable extent.
		return sc.selfCB, true
	}
	frame := currentFrame(w)
	if frame.stickyFlowBoxes != nil {
		if cb, ok := frame.stickyFlowBoxes[parent]; ok {
			return cb, true
		}
	}
	parentID := LayoutID{engine: engine, node: parent, generation: own.generation}
	cb, err := layoutBoundsOf(w, parentID)
	if err != nil {
		return Bounds{}, false
	}
	return cb, true
}

// stickyDivOffset computes one sticky div's offset at its prepaint:
// the innermost scroller context, the div's current bounds, and its
// flow containing block from the registry (or the scroller's own
// extent). It reports the zero offset for anything that is not a
// sticky div under an open scroller scope.
func stickyDivOffset(w *Window, style *Style, bounds Bounds, own LayoutID) Point {
	if style.Position != PositionSticky {
		return Point{}
	}
	frame := currentFrame(w)
	n := len(frame.stickyScrollers)
	if n == 0 {
		return Point{}
	}
	sc := frame.stickyScrollers[n-1]
	cb, ok := stickyFlowBoxOf(w, own, sc)
	if !ok {
		return Point{}
	}
	return stickyOffsetOf(w, style, bounds, cb)
}

// paintsAfterInFlow reports whether one child element paints (and
// hit-tests) after its in-flow siblings: a sticky div does
// (browser-core ticket 34; css2.1 Appendix E step 8 — a positioned
// element paints above in-flow non-positioned content, so a pinned
// header rides over the content scrolling under it, and Chromium
// hit-tests in paint order). Everything else keeps the port's tree
// order — the port has no z-index, and this rule is scoped to sticky
// children of a div: a sticky above content in OTHER subtrees, and
// the ordering between a sticky and positioned (relative/absolute)
// siblings, remain the port's general paint-order limitation.
func paintsAfterInFlow(el AnyElement) bool {
	p, ok := el.obj.(interface{ paintsAfterInFlow() bool })
	return ok && p.paintsAfterInFlow()
}

// stickyChildrenLast reorders one div's children for the child walk
// (prepaint and paint, the same order both phases): the in-flow
// children in tree order first, the sticky children after them in
// tree order. Children without a sticky element return the slice
// unchanged, so non-sticky views pay one cheap interface check per
// child and no allocation.
func stickyChildrenLast(children []AnyElement) []AnyElement {
	var sticky []AnyElement
	for _, child := range children {
		if paintsAfterInFlow(child) {
			sticky = append(sticky, child)
		}
	}
	if sticky == nil {
		return children
	}
	ordered := make([]AnyElement, 0, len(children))
	for _, child := range children {
		if !paintsAfterInFlow(child) {
			ordered = append(ordered, child)
		}
	}
	return append(ordered, sticky...)
}

// recordStickyFlowBox records one div's flow rectangle for its sticky
// descendants in the frame's registry, filled only while the frame
// requested a sticky element (the request walk's flag — non-sticky
// views pay nothing): its content box, or its scrollable extent when
// the div is the scroll container they stick inside. The div records
// AFTER applying its own sticky offset, so a sticky ancestor's shift
// is part of its descendants' containing block (Chromium's
// ancestor-shift rule).
func recordStickyFlowBox(w *Window, node native.NodeHandle, box Bounds) {
	frame := currentFrame(w)
	if !frame.stickyRequested {
		return
	}
	if frame.stickyFlowBoxes == nil {
		frame.stickyFlowBoxes = make(map[native.NodeHandle]Bounds)
	}
	frame.stickyFlowBoxes[node] = box
}
