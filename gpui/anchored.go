package gpui

// This file is Base ticket 08's anchored element: the pinned
// crates/gpui/src/elements/anchored.rs (GPUI-CE 254b5dbd, R: below) — an
// absolutely positioned flex box whose children are moved so they avoid
// overflowing the window: either by switching the anchor corner or by
// snapping to the window edges (with an optional margin plus the
// window's client inset).
//
// Ticket 25 (browser-core) extends it with the element-anchored mode
// of CSS anchor positioning
// (drafts.csswg.org/css-anchor-position-1):
//
//   - position-anchor — the anchor is another element, either named by
//     its element id (AnchorTo, recorded per frame by the anchor's own
//     prepaint) or a tracked bounds cell (AnchorBounds, Base's popup
//     measure pattern);
//   - the side/alignment/gap placement of position-area / anchor()
//     insets (Side, Align, Gap): the popup is placed on one side of
//     the anchor, aligned start/center/end along the side, a gap away;
//   - position-try-fallbacks (TryFallbacks): an ordered list of
//     fallbacks tried when the placement overflows the viewport —
//     flip-block and flip-inline mirror the placement across the
//     anchor's block/inline midline (Chromium's TryTacticTransform,
//     try_tactic_transform.h: on a vertical side flip-block flips the
//     side and flip-inline flips the alignment, and the other way
//     round), and shift clamps the placement into the viewport (Base's
//     positioner clamp; CSS expresses it with min()/max() over the
//     anchor() inset). Like Chromium (out_of_flow_layout_part.cc's
//     OOFCandidateStyleIterator, 2098-2260), the base style is tried
//     first, then each fallback in order, the first that fits wins, and
//     when none fits the base placement stands — CSS never snaps an
//     anchored popup implicitly, so the corner modes' snap does not
//     apply in side mode unless AnchorShift is listed;
//   - position-visibility: anchors-visible (HideWhenAnchorClipped):
//     the popup paints nothing while its anchor does not intersect the
//     clipping scroller it lives in (Chromium's
//     anchor_position_visibility_observer.cc hides the paint layer when
//     the anchor's intersection with its nearest clipping ancestor is
//     empty); the layout bounds still exist, like the browser's.
//
// Go adaptation: the port's drawables re-resolve their layout bounds at
// paint (element.go), so the anchored element re-applies the offset it
// prepainted its children under around their paint (the pin reuses the
// prepainted bounds).

import (
	"fmt"
	"math"
)

// AnchoredFitMode selects the algorithm fitting the anchored element
// inside the window (R:anchored.rs:241-250 AnchoredFitMode).
type AnchoredFitMode uint8

const (
	// AnchoredFitSwitchAnchor switches which corner the anchored element
	// is attached to (the default).
	AnchoredFitSwitchAnchor AnchoredFitMode = iota
	// AnchoredFitSnapToWindow snaps the anchored element to the window
	// edge.
	AnchoredFitSnapToWindow
	// AnchoredFitSnapToWindowWithMargin snaps to the window edge and
	// leaves the element's margin edges.
	AnchoredFitSnapToWindowWithMargin
)

// AnchoredPositionMode selects how the anchored position is interpreted
// (R:anchored.rs:252-259 AnchoredPositionMode).
type AnchoredPositionMode uint8

const (
	// AnchoredPositionWindow interprets the position relative to the
	// window (the default).
	AnchoredPositionWindow AnchoredPositionMode = iota
	// AnchoredPositionLocal interprets the position relative to the
	// anchored element's own laid-out origin (its parent's coordinate
	// space).
	AnchoredPositionLocal
)

// positionAndBounds is R:anchored.rs:261-288 get_position_and_bounds.
func (m AnchoredPositionMode) positionAndBounds(anchorPosition *Point, anchor Anchor, size Size, bounds Bounds, offset *Point) (Point, Bounds) {
	var off Point
	if offset != nil {
		off = *offset
	}
	switch m {
	case AnchoredPositionLocal:
		var position Point
		if anchorPosition != nil {
			position = *anchorPosition
		}
		at := Point{X: bounds.Origin.X + position.X + off.X, Y: bounds.Origin.Y + position.Y + off.Y}
		return position, BoundsFromAnchorAndSize(anchor, at, size)
	default:
		position := bounds.Origin
		if anchorPosition != nil {
			position = *anchorPosition
		}
		at := Point{X: position.X + off.X, Y: position.Y + off.Y}
		return position, BoundsFromAnchorAndSize(anchor, at, size)
	}
}

// AnchoredElement is an anchored element that displays UI avoiding
// overflow of the window bounds (R:anchored.rs:14-23 Anchored). Its
// children should have no margin to avoid measurement issues. Build one
// with Anchored.
//
// Ticket 25 adds the element-anchored mode: with an anchor source set
// (AnchorTo or AnchorBounds), the children are placed on a side of that
// anchor with alignment and gap, under the position-try-fallbacks list,
// optionally hiding while the anchor is clipped — see the file comment.
type AnchoredElement struct {
	children       []AnyElement
	anchor         Anchor
	fitMode        AnchoredFitMode
	fitMargin      Edges
	anchorPosition *Point
	positionMode   AnchoredPositionMode
	offset         *Point

	// The element-anchored mode (ticket 25). anchorName and anchorCell
	// are the two anchor sources; setting either activates the mode.
	anchorName            string
	anchorCell            *Bounds
	side                  AnchorSide
	align                 AnchorAlign
	gap                   float32
	tryFallbacks          []AnchorFallback
	margin                Edges
	hideWhenAnchorClipped bool
	// occlude blocks the mouse over the placed bounds (browser-core
	// ticket 72: the DOM popover's hit-test coverage — the top layer
	// hit-tests above the page beneath, which Base's popup surfaces
	// express as an occluding hitbox over the placed box).
	occlude bool
	// onResolved observes the resolved bounds at each prepaint, before
	// the children prepaint (browser-core ticket 72: Base's positioner
	// OnPosition seam — presentation such as a pointer arrow reads the
	// placement).
	onResolved func(bounds Bounds)
}

// Anchored returns an element that avoids overflowing the window bounds
// (R:anchored.rs:27-36 anchored): top-left anchor, the switch-anchor fit,
// window position mode. Ticket 25: the element-anchored mode's defaults
// are the bottom side, start alignment, no gap and no fallbacks.
func Anchored() *AnchoredElement {
	return &AnchoredElement{anchor: AnchorTopLeft, fitMode: AnchoredFitSwitchAnchor, positionMode: AnchoredPositionWindow, side: AnchorSideBottom}
}

// Anchor sets which corner of the anchored element is anchored to the
// current position (R:anchored.rs:40).
func (a *AnchoredElement) Anchor(anchor Anchor) *AnchoredElement {
	a.anchor = anchor
	return a
}

// Position sets the position in window coordinates (otherwise the
// location the anchored element is laid out at is used;
// R:anchored.rs:47).
func (a *AnchoredElement) Position(position Point) *AnchoredElement {
	p := position
	a.anchorPosition = &p
	return a
}

// Offset offsets the final position by this amount (R:anchored.rs:54).
func (a *AnchoredElement) Offset(offset Point) *AnchoredElement {
	o := offset
	a.offset = &o
	return a
}

// PositionMode sets the position mode (R:anchored.rs:62).
func (a *AnchoredElement) PositionMode(mode AnchoredPositionMode) *AnchoredElement {
	a.positionMode = mode
	return a
}

// SnapToWindow snaps to the window edge instead of switching the anchor
// corner when an overflow would occur (R:anchored.rs:68).
func (a *AnchoredElement) SnapToWindow() *AnchoredElement {
	a.fitMode = AnchoredFitSnapToWindow
	return a
}

// SnapToWindowWithMargin snaps to the window edge leaving the margin
// edges (R:anchored.rs:74).
func (a *AnchoredElement) SnapToWindowWithMargin(edges Edges) *AnchoredElement {
	a.fitMode = AnchoredFitSnapToWindowWithMargin
	a.fitMargin = edges
	return a
}

// ---------------------------------------------------------------------------
// The element-anchored mode (ticket 25, CSS anchor positioning)
// ---------------------------------------------------------------------------

// AnchorSide is the side of the anchor the anchored element is placed
// on (the side half of position-area, or the anchor() side pair of the
// placement insets: bottom is top: anchor(bottom)+gap).
type AnchorSide uint8

const (
	// AnchorSideTop places the element above the anchor.
	AnchorSideTop AnchorSide = iota
	// AnchorSideBottom places the element below the anchor (the
	// default).
	AnchorSideBottom
	// AnchorSideLeft places the element to the left of the anchor.
	AnchorSideLeft
	// AnchorSideRight places the element to the right of the anchor.
	AnchorSideRight
)

// String renders the side like the CSS keyword.
func (s AnchorSide) String() string {
	switch s {
	case AnchorSideTop:
		return "top"
	case AnchorSideBottom:
		return "bottom"
	case AnchorSideLeft:
		return "left"
	case AnchorSideRight:
		return "right"
	}
	return fmt.Sprintf("AnchorSide(%d)", uint8(s))
}

// isVertical reports whether the side runs along the vertical axis
// (top/bottom), i.e. the popup sits above or below the anchor.
func (s AnchorSide) isVertical() bool { return s == AnchorSideTop || s == AnchorSideBottom }

// flipped returns the opposite side.
func (s AnchorSide) flipped() AnchorSide {
	switch s {
	case AnchorSideTop:
		return AnchorSideBottom
	case AnchorSideBottom:
		return AnchorSideTop
	case AnchorSideLeft:
		return AnchorSideRight
	}
	return AnchorSideLeft
}

// AnchorAlign aligns the anchored element along the side it is placed
// on (position-area's second keyword: start/center/end).
type AnchorAlign uint8

const (
	// AnchorAlignStart aligns the element's leading edge with the
	// anchor's (the default).
	AnchorAlignStart AnchorAlign = iota
	// AnchorAlignCenter centers the element on the anchor along the
	// side.
	AnchorAlignCenter
	// AnchorAlignEnd aligns the element's trailing edge with the
	// anchor's.
	AnchorAlignEnd
)

// String renders the alignment like the CSS keyword.
func (a AnchorAlign) String() string {
	switch a {
	case AnchorAlignStart:
		return "start"
	case AnchorAlignCenter:
		return "center"
	case AnchorAlignEnd:
		return "end"
	}
	return fmt.Sprintf("AnchorAlign(%d)", uint8(a))
}

// flipped returns the opposite alignment (start <-> end, center fixed).
func (a AnchorAlign) flipped() AnchorAlign {
	switch a {
	case AnchorAlignStart:
		return AnchorAlignEnd
	case AnchorAlignEnd:
		return AnchorAlignStart
	}
	return AnchorAlignCenter
}

// AnchorFallback is one position-try-fallback (position-try-fallbacks),
// tried in the order TryFallbacks lists them once the placement
// overflows the viewport.
type AnchorFallback uint8

const (
	// AnchorFlipBlock mirrors the placement across the anchor's
	// horizontal midline (CSS flip-block): on a vertical side the side
	// flips (bottom <-> top), on a horizontal side the alignment along
	// the side flips (start <-> end).
	AnchorFlipBlock AnchorFallback = iota
	// AnchorFlipInline mirrors the placement across the anchor's
	// vertical midline (CSS flip-inline): on a horizontal side the side
	// flips (left <-> right), on a vertical side the alignment flips.
	AnchorFlipInline
	// AnchorShift clamps the placement into the viewport (Base's
	// positioner clamp; CSS expresses it with min()/max() over the
	// anchor() inset). The clamped placement fits by construction, so
	// listing it last means "flip if possible, else stay inside the
	// window".
	AnchorShift
)

// String renders the fallback like the CSS keyword.
func (f AnchorFallback) String() string {
	switch f {
	case AnchorFlipBlock:
		return "flip-block"
	case AnchorFlipInline:
		return "flip-inline"
	}
	return "shift"
}

// AnchorTo anchors the element to the element carrying the given id
// (CSS position-anchor): the anchor element — usually an interactive
// div — names itself with .ID(name), and its prepaint records its
// window-space bounds (and its clipping scroller's visible rectangle)
// for this frame. The anchor resolves at the anchored element's
// prepaint from the current frame first (the anchor prepaints before
// the usual deferred anchored element), then from the last completed
// frame (the first frame, or an anchor later in the tree). A later
// same-named element wins.
func (a *AnchoredElement) AnchorTo(id string) *AnchoredElement {
	a.anchorName = id
	return a
}

// AnchorBounds anchors the element to a tracked bounds cell that the
// anchor side updates each frame (Base's popup measure pattern: a
// *Bounds an app-owned canvas or keyed state refreshes). The cell is
// read at every prepaint, so a moving anchor moves the anchored
// element with it. HideWhenAnchorClipped does not apply to a cell
// anchor — a cell carries no clipping rectangle — so a cell-anchored
// element never hides; use AnchorTo when the clipped case matters.
func (a *AnchoredElement) AnchorBounds(cell *Bounds) *AnchoredElement {
	a.anchorCell = cell
	return a
}

// Side sets the side of the anchor the element is placed on (the side
// half of position-area). The default is AnchorSideBottom.
func (a *AnchoredElement) Side(side AnchorSide) *AnchoredElement {
	a.side = side
	return a
}

// Align sets the alignment along the side (position-area's second
// keyword). The default is AnchorAlignStart.
func (a *AnchoredElement) Align(align AnchorAlign) *AnchoredElement {
	a.align = align
	return a
}

// Gap sets the distance kept between the anchor and the anchored
// element along the side (the anchor() inset's own offset, as in
// top: calc(anchor(bottom) + gap)). The default is 0.
func (a *AnchoredElement) Gap(gap float32) *AnchoredElement {
	a.gap = gap
	return a
}

// TryFallbacks sets the ordered position-try-fallbacks list, tried
// once the placement overflows the viewport: the base placement first,
// then each fallback in this order, first that fits wins, and the base
// placement stands when none fits (Chromium's
// OOFCandidateStyleIterator). The default is none, as in CSS.
func (a *AnchoredElement) TryFallbacks(fallbacks ...AnchorFallback) *AnchoredElement {
	a.tryFallbacks = append(a.tryFallbacks[:0], fallbacks...)
	return a
}

// Margin sets the minimum distance kept from the viewport edge when
// trying fallbacks and shifting (Base's Positioner::margin; CSS checks
// the raw viewport, so leave it at zero for browser-exact placement).
func (a *AnchoredElement) Margin(edges Edges) *AnchoredElement {
	a.margin = edges
	return a
}

// HideWhenAnchorClipped hides the element (it paints nothing, but
// keeps its layout bounds, like the browser) while its anchor does
// not intersect the clipping scroller the anchor lives in (CSS
// position-visibility: anchors-visible; Chromium hides the paint layer
// when the anchor's intersection with its nearest clipping ancestor is
// empty). Only an AnchorTo anchor tracks a clipping rectangle; see
// AnchorBounds.
func (a *AnchoredElement) HideWhenAnchorClipped() *AnchoredElement {
	a.hideWhenAnchorClipped = true
	return a
}

// Occlude blocks the mouse over the placed bounds, ahead of the
// children's own hitboxes (browser-core ticket 72: the DOM popover
// hit-tests above the page beneath it — the top layer's coverage, which
// a deferred anchored surface expresses as an occluding hitbox over
// the placed box; Base's positioner Occlude pattern). The children's
// own hitboxes sit above it, so the surface's content still receives
// the pointer.
func (a *AnchoredElement) Occlude() *AnchoredElement {
	a.occlude = true
	return a
}

// OnResolved sets a listener notified with the element's resolved
// window-space bounds at each prepaint, before the children prepaint
// (browser-core ticket 72: Base's positioner OnPosition seam —
// presentation such as a pointer arrow reads the placement while the
// content still lays out under it). Only the element-anchored mode
// reports (the classic modes keep their position math to themselves).
func (a *AnchoredElement) OnResolved(f func(bounds Bounds)) *AnchoredElement {
	a.onResolved = f
	return a
}

// Child adds one child (R:anchored.rs:80-84 ParentElement::extend).
func (a *AnchoredElement) Child(value any) *AnchoredElement {
	a.children = append(a.children, intoChild("Anchored", len(a.children), value))
	return a
}

// Children adds the children in order (ParentElement::children).
func (a *AnchoredElement) Children(values ...any) *AnchoredElement {
	for _, value := range values {
		a.Child(value)
	}
	return a
}

// IntoElement implements IntoElement (R:anchored.rs:233-239).
func (a *AnchoredElement) IntoElement() AnyElement {
	return CustomElement[anchoredState, anchoredPrepaint](&anchoredDrawable{anchored: a})
}

// anchoredState is R:anchored.rs:10-12 AnchoredState.
type anchoredState struct {
	childLayoutIDs []LayoutID
}

// anchoredPrepaint records the offset the children prepainted under (the
// port's paint re-applies it; see the file comment) and whether
// position-visibility hid the element this frame (ticket 25: the
// children paint nothing, but their layout bounds stay, like the
// browser's).
type anchoredPrepaint struct {
	offset Point
	hidden bool
}

// anchoredDrawable adapts the builder into the Element phases.
type anchoredDrawable struct {
	anchored *AnchoredElement
}

// anchoredNodeStyle is the anchored node's style (R:anchored.rs:111-115):
// absolute, flex.
func anchoredNodeStyle() Style {
	style := DefaultStyle()
	style.Position = PositionAbsolute
	style.Display = DisplayFlex
	return style
}

// ID implements Element (R:anchored.rs:90).
func (d *anchoredDrawable) ID() (ElementID, bool) { return ElementID{}, false }

// SourceLocation implements Element (R:anchored.rs:94).
func (d *anchoredDrawable) SourceLocation() *SourceLocation { return nil }

// RequestLayout implements Element (R:anchored.rs:98-120).
func (d *anchoredDrawable) RequestLayout(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App) (LayoutID, anchoredState) {
	// Ticket 25: a name-anchored element turns the window's anchor
	// recording on now, during the tree's request pass — the whole tree
	// requests layout before the first prepaint, so the named elements'
	// prepaints then record into this frame's registry.
	if d.anchored.anchorName != "" {
		requestAnchorTracking(w)
	}
	// Ticket 21 (the ticket-33 note): the anchored node is an
	// absolutely positioned container, so it requests through
	// RequestContainerLayout — a participating containing block for
	// absolutely positioned descendants of Static() divs inside it
	// (browser-core 33, spec D3), like any positioned element.
	ids := make([]LayoutID, 0, len(d.anchored.children))
	id, err := RequestContainerLayout(w, anchoredNodeStyle(), func(w *Window) []LayoutID {
		for i := range d.anchored.children {
			ids = append(ids, d.anchored.children[i].RequestLayout(w, app))
		}
		return ids
	})
	if err != nil {
		panic(fmt.Sprintf("gpui: Anchored request_layout: %v", err))
	}
	return id, anchoredState{childLayoutIDs: ids}
}

// boundsUnion is the pinned Bounds::union (geometry.rs:1182-1186): the
// min of the origins to the max of the bottom-right corners.
func boundsUnion(a, b Bounds) Bounds {
	topLeft := Point{X: min32(a.Origin.X, b.Origin.X), Y: min32(a.Origin.Y, b.Origin.Y)}
	bottomRight := Point{X: max32(a.Right(), b.Right()), Y: max32(a.Bottom(), b.Bottom())}
	return fromCorners(topLeft, bottomRight)
}

// Prepaint implements Element (R:anchored.rs:122-215): the children's
// union size is positioned by the position mode; the switch-anchor fit
// flips the anchor along each axis whose overflow the flip fixes; every
// mode then snaps overflowing edges to the window (aligning to the
// left/top when larger than the window) with the margin edges plus the
// client inset; the children prepaint under the rounded offset.
//
// Ticket 25: in the element-anchored mode the union size is placed on a
// side of the resolved anchor instead, under the position-try-fallbacks
// list (the base placement first, then each fallback, first that fits,
// the base placement when none does — no implicit snapping), and a
// clipped anchor hides the element's paint (the children still
// prepaint, so hover, dispatch and the debug bounds behave like the
// browser's laid-out-but-invisible element).
func (d *anchoredDrawable) Prepaint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *anchoredState, w *Window, app *App) anchoredPrepaint {
	a := d.anchored
	if len(layout.childLayoutIDs) == 0 {
		return anchoredPrepaint{}
	}
	childrenBounds := w.LayoutBounds(layout.childLayoutIDs[0])
	for _, id := range layout.childLayoutIDs[1:] {
		childrenBounds = boundsUnion(childrenBounds, w.LayoutBounds(id))
	}

	if a.anchorName != "" || a.anchorCell != nil {
		return d.prepaintAnchoredToElement(a, bounds, childrenBounds.Size, w, app)
	}

	origin, desired := a.positionMode.positionAndBounds(a.anchorPosition, a.anchor, childrenBounds.Size, bounds, a.offset)

	limits := Bounds{Size: w.ViewportSize()}

	if a.fitMode == AnchoredFitSwitchAnchor {
		anchor := a.anchor
		if desired.Left() < limits.Left() || desired.Right() > limits.Right() {
			switched := BoundsFromAnchorAndSize(anchor.OtherSideAlong(AxisHorizontal), origin, childrenBounds.Size)
			if !(switched.Left() < limits.Left() || switched.Right() > limits.Right()) {
				anchor = anchor.OtherSideAlong(AxisHorizontal)
				desired = switched
			}
		}
		if desired.Top() < limits.Top() || desired.Bottom() > limits.Bottom() {
			switched := BoundsFromAnchorAndSize(anchor.OtherSideAlong(AxisVertical), origin, childrenBounds.Size)
			if !(switched.Top() < limits.Top() || switched.Bottom() > limits.Bottom()) {
				desired = switched
			}
		}
	}

	clientInset, _ := w.ClientInset()
	var edges Edges
	if a.fitMode == AnchoredFitSnapToWindowWithMargin {
		edges = a.fitMargin
	}
	edges = Edges{Top: edges.Top + clientInset, Right: edges.Right + clientInset, Bottom: edges.Bottom + clientInset, Left: edges.Left + clientInset}

	// Snap the horizontal edges to the window's if they overflow,
	// aligning to the left when wider than the limits.
	if desired.Right() > limits.Right() {
		desired.Origin.X -= desired.Right() - limits.Right() + edges.Right
	}
	if desired.Left() < limits.Left() {
		desired.Origin.X = limits.Origin.X + edges.Left
	}
	// Snap the vertical edges likewise, aligning to the top when taller.
	if desired.Bottom() > limits.Bottom() {
		desired.Origin.Y -= desired.Bottom() - limits.Bottom() + edges.Bottom
	}
	if desired.Top() < limits.Top() {
		desired.Origin.Y = limits.Origin.Y + edges.Top
	}

	offset := Point{
		X: float32(math.Round(float64(desired.Origin.X - bounds.Origin.X))),
		Y: float32(math.Round(float64(desired.Origin.Y - bounds.Origin.Y))),
	}
	WithElementOffset(w, offset, func(w *Window) struct{} {
		for i := range a.children {
			a.children[i].Prepaint(w, app)
		}
		return struct{}{}
	})
	return anchoredPrepaint{offset: offset}
}

// anchorSource is one resolved element anchor (ticket 25): the
// anchor's window-space rectangle and, when tracked, the visible
// (clipped) rectangle it lives in.
type anchorSource struct {
	bounds Bounds
	clip   Bounds
	// clipped is known (true or false) when the anchor's clip was
	// tracked — an AnchorTo anchor tracks it, a cell anchor does not.
	clippedKnown bool
	clipped      bool
}

// resolveAnchor resolves the element anchor at prepaint: by name from
// the frame's registry, or from the tracked bounds cell.
func (d *anchoredDrawable) resolveAnchor(w *Window) (anchorSource, bool) {
	a := d.anchored
	if a.anchorCell != nil {
		return anchorSource{bounds: *a.anchorCell}, true
	}
	if record, ok := lookupAnchorBounds(w, a.anchorName); ok {
		source := anchorSource{bounds: record.bounds, clip: record.clip, clippedKnown: true}
		source.clipped = isEmptyBounds(intersectBounds(record.bounds, record.clip))
		return source, true
	}
	return anchorSource{}, false
}

// prepaintAnchoredToElement is the element-anchored mode's prepaint
// (ticket 25): resolve the anchor, place the children's union size on
// the chosen side with alignment and gap, try the fallbacks, hide on a
// clipped anchor, and prepaint the children under the rounded offset.
func (d *anchoredDrawable) prepaintAnchoredToElement(a *AnchoredElement, bounds Bounds, size Size, w *Window, app *App) anchoredPrepaint {
	anchor, ok := d.resolveAnchor(w)
	if !ok {
		// No anchor yet (the first frame, or a misspelled id): the
		// children keep their laid-out position, like Base's popup
		// first frame.
		WithElementOffset(w, Point{}, func(w *Window) struct{} {
			for i := range a.children {
				a.children[i].Prepaint(w, app)
			}
			return struct{}{}
		})
		return anchoredPrepaint{}
	}

	desired := anchoredSidePlacement(anchor.bounds, size, a.side, a.align, a.gap)
	if a.offset != nil {
		desired.Origin = Point{X: desired.Origin.X + a.offset.X, Y: desired.Origin.Y + a.offset.Y}
	}

	// The viewport the placement must fit in, inset by the client inset
	// (zero in undecorated test windows) and the margin. Chromium tries
	// the base style, then each fallback in order, and keeps the base
	// style when none fits (out_of_flow_layout_part.cc:2139-2260).
	clientInset, _ := w.ClientInset()
	inset := Edges{
		Top:    a.margin.Top + clientInset,
		Right:  a.margin.Right + clientInset,
		Bottom: a.margin.Bottom + clientInset,
		Left:   a.margin.Left + clientInset,
	}
	fits := func(b Bounds) bool {
		return b.Left() >= inset.Left && b.Top() >= inset.Top &&
			b.Right() <= w.ViewportSize().Width-inset.Right &&
			b.Bottom() <= w.ViewportSize().Height-inset.Bottom
	}

	// The position-try-fallbacks iteration (Chromium's
	// OOFCandidateStyleIterator, out_of_flow_layout_part.cc:2098-2260):
	// the base style is candidate 0 — it wins outright when it fits,
	// and the fallbacks are only tried when it overflows; each fallback
	// derives from the base style, the first that fits wins, and when
	// none fits the base placement stands (CSS never implicitly snaps).
	if !fits(desired) {
		for _, fallback := range a.tryFallbacks {
			candidate := desired
			switch fallback {
			case AnchorFlipBlock:
				candidate = anchoredSidePlacement(anchor.bounds, size, a.flippedSide(AnchorFlipBlock), a.flippedAlign(AnchorFlipBlock), a.gap)
			case AnchorFlipInline:
				candidate = anchoredSidePlacement(anchor.bounds, size, a.flippedSide(AnchorFlipInline), a.flippedAlign(AnchorFlipInline), a.gap)
			case AnchorShift:
				candidate = anchoredShift(desired, w.ViewportSize(), inset)
			}
			if a.offset != nil {
				candidate.Origin = Point{X: candidate.Origin.X + a.offset.X, Y: candidate.Origin.Y + a.offset.Y}
			}
			if fits(candidate) {
				desired = candidate
				break
			}
		}
	}

	hidden := a.hideWhenAnchorClipped && anchor.clippedKnown && anchor.clipped

	// browser-core ticket 72: the placement observation and the
	// occluding hitbox, both ahead of the children's own hitboxes
	// (Base's positioner order: OnPosition, then the occlude hitbox,
	// then the children prepaint).
	if a.onResolved != nil {
		a.onResolved(desired)
	}
	if a.occlude {
		w.InsertHitbox(desired, HitboxBlockMouse)
	}

	offset := Point{
		X: float32(math.Round(float64(desired.Origin.X - bounds.Origin.X))),
		Y: float32(math.Round(float64(desired.Origin.Y - bounds.Origin.Y))),
	}
	WithElementOffset(w, offset, func(w *Window) struct{} {
		for i := range a.children {
			a.children[i].Prepaint(w, app)
		}
		return struct{}{}
	})
	return anchoredPrepaint{offset: offset, hidden: hidden}
}

// flippedSide returns the side the given flip tactic would place the
// element on: a flip along the placement axis swaps the side, a flip
// along the alignment axis keeps it (try_tactic_transform.h's mirrors).
func (a *AnchoredElement) flippedSide(fallback AnchorFallback) AnchorSide {
	vertical := a.side.isVertical()
	flipAlongSideAxis := (fallback == AnchorFlipBlock) == vertical
	if flipAlongSideAxis {
		return a.side.flipped()
	}
	return a.side
}

// flippedAlign returns the alignment the given flip tactic would use:
// a flip along the alignment axis swaps start and end, a flip along the
// side axis keeps it (center is its own mirror).
func (a *AnchoredElement) flippedAlign(fallback AnchorFallback) AnchorAlign {
	vertical := a.side.isVertical()
	flipAlongAlignAxis := (fallback == AnchorFlipBlock) != vertical
	if flipAlongAlignAxis {
		return a.align.flipped()
	}
	return a.align
}

// anchoredSidePlacement places a size-sized element on side of the
// anchor, align pixels aligned along it, gap away — the geometry of
// the anchor() insets (bottom is top: anchor(bottom)+gap, start is
// left: anchor(left), center is anchor(center)-size/2, end is
// left: anchor(right)-width, and the mirrored forms for the other
// sides).
func anchoredSidePlacement(anchor Bounds, size Size, side AnchorSide, align AnchorAlign, gap float32) Bounds {
	var x, y float32
	switch align {
	case AnchorAlignStart:
		x, y = anchor.Origin.X, anchor.Origin.Y
	case AnchorAlignEnd:
		x, y = anchor.Right()-size.Width, anchor.Bottom()-size.Height
	default:
		x = anchor.Center().X - size.Width/2
		y = anchor.Center().Y - size.Height/2
	}
	switch side {
	case AnchorSideTop:
		y = anchor.Origin.Y - gap - size.Height
	case AnchorSideBottom:
		y = anchor.Bottom() + gap
	case AnchorSideLeft:
		x = anchor.Origin.X - gap - size.Width
	case AnchorSideRight:
		x = anchor.Right() + gap
	}
	return Bounds{Origin: Point{X: x, Y: y}, Size: size}
}

// anchoredShift clamps the placement into the viewport keeping inset
// from each edge (Base's positioner clamp: aligning to the leading edge
// when larger than the viewport).
func anchoredShift(desired Bounds, viewport Size, inset Edges) Bounds {
	rightLimit := max(viewport.Width-inset.Right, inset.Left)
	bottomLimit := max(viewport.Height-inset.Bottom, inset.Top)
	if desired.Right() > rightLimit {
		desired.Origin.X -= desired.Right() - rightLimit
	}
	if desired.Left() < inset.Left {
		desired.Origin.X = inset.Left
	}
	if desired.Bottom() > bottomLimit {
		desired.Origin.Y -= desired.Bottom() - bottomLimit
	}
	if desired.Top() < inset.Top {
		desired.Origin.Y = inset.Top
	}
	return desired
}

// Paint implements Element (R:anchored.rs:217-231): the children paint
// (under the prepainted offset; see the file comment). Ticket 25: a
// clipped anchor hides the element — nothing paints, like Chromium's
// invisible layer for position-visibility: anchors-visible (the layout
// bounds still exist; the children still prepaint, so the next visible
// frame has its hover, dispatch and debug state in place).
func (d *anchoredDrawable) Paint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *anchoredState, prepaint *anchoredPrepaint, w *Window, app *App) {
	if prepaint.hidden {
		return
	}
	WithElementOffset(w, prepaint.offset, func(w *Window) struct{} {
		for i := range d.anchored.children {
			d.anchored.children[i].Paint(w, app)
		}
		return struct{}{}
	})
}

// LayoutNodeStyle implements the root-stretch report.
func (d *anchoredDrawable) LayoutNodeStyle() (Style, bool) { return anchoredNodeStyle(), true }
