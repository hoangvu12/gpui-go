package gpui

// The fixed-position draw (browser-core ticket 35, spec L10, the paint
// half of position: fixed; the layout half is the containing-block
// deferral in containingblock.go).
//
// Chromium's paint order (css2.1 Appendix E, and the PaintLayer tree
// Chromium parents from it): a fixed element whose containing block is
// the LayoutView — every fixed element, since only a transform, filter
// or containment makes an ancestor a fixed container
// (LayoutObject::ComputeIsFixedContainer, core/layout/
// layout_object.cc:2009-2067, and the port has none — paints as a
// POSITIONED element of the ROOT stacking context (Appendix E step 8):
// the paint-layer parent of a fixed element is found along its
// containing-block chain (PaintLayer::FindStackingContainer walks the
// containing block, not the DOM parent), which ends at the root. So a
// fixed box rides above ALL in-flow content of the whole tree (steps
// 3-7) no matter how deep its DOM position — over later siblings, over
// other subtrees, over the content scrolling under it — while an
// explicitly top-layer surface (a popover, a deferred popup) still
// paints above it, and equal-geometry fixed boxes keep tree order.
//
// The port's mechanism: the div's drawable defers its whole draw (the
// TopLayer pattern, deferred.go) at the fixed priority — every
// deferred draw paints after the whole main scene above a raised order
// floor (paintDeferredDraws), so the in-flow scene is exactly the
// layer the fixed box must ride over, and the explicit surfaces (Base
// popups at 100, tooltips at 200, the top layer at 1<<20) keep their
// place above it. The deferral runs the div's prepaint in the deferred
// round under a ZEROED element offset and outside every ancestor's
// content mask, which is also what keeps the box at its window
// position inside a scrolled scroller (the scroll offset lives in the
// element-offset context) and un-clipped by the scroller's overflow
// (Chromium: an overflow clip on an ancestor only clips descendants
// whose containing-block chain passes through it — a fixed element's
// does not; scroll offsets likewise accumulate along the containing-
// block chain, css-overflow-1 §2, and the fixed element's chain skips
// the scroller entirely).
//
// The decision point is DivElement.IntoElement on the AUTHORED
// position (the builder's refinement — Fixed() or the Tailwind
// `fixed` class), the position the request walk also computes the
// layout from; a state style (hover, focus) that set position would
// be invisible to the deferral — position is not a state style in
// practice, and the limitation is recorded in the ticket.

// fixedDrawPriority is the deferred-draw priority of position: fixed
// elements: every deferred draw paints above the whole main scene, and
// this priority keeps the fixed boxes below every explicitly deferred
// surface — Base's dialog sheets at 10+layer, its popups at 100, its
// tooltips at 200 and the top layer at overlayPriorityBase (1<<20) —
// the DOM's shape, where the top layer and overlay surfaces sit above
// the root stacking context's positioned layer. Equal priorities keep
// push order (the tree walk order), which is Appendix E's tree order
// among the root's positioned descendants.
const fixedDrawPriority = 0

// fixedDrawElement wraps a fixed div's element with the deferring
// drawable when the builder's authored position is fixed; every other
// div passes through unchanged.
func fixedDrawElement(d *DivElement, inner AnyElement) AnyElement {
	if st := d.Styled.storage(); st != nil && st.Position.present && st.Position.value == PositionFixed {
		return CustomElement[fixedDivRequest, fixedDivPrepaint](&fixedDivDrawable{inner: inner})
	}
	return inner
}

// fixedDivRequest is the wrapper's request-phase state (nothing: the
// node is the inner's).
type fixedDivRequest struct{}

// fixedDivPrepaint is the wrapper's prepaint-phase state (nothing: the
// draw was deferred).
type fixedDivPrepaint struct{}

// fixedDivDrawable is the fixed div's deferring wrapper: the request
// phase forwards to the inner (the div's own node — opened containing
// block, deferred into the root scope, everything the div does), the
// prepaint defers the INNER to the deferred round under a zeroed
// element offset, and the wrapper itself paints nothing (the inner
// paints in the deferred round). The wrapper carries no element id, so
// the inner pushes the div's own id exactly once — the subtree's
// element paths, hitboxes and dispatch nodes keep their DOM ancestry.
type fixedDivDrawable struct {
	inner AnyElement
}

// ID implements Element: none — the inner carries the div's identity
// (the deferred round restores the captured id stack and the inner
// pushes the div's own id, so the paths are the DOM ones).
func (d *fixedDivDrawable) ID() (ElementID, bool) { return ElementID{}, false }

// SourceLocation implements Element.
func (d *fixedDivDrawable) SourceLocation() *SourceLocation { return nil }

// RequestLayout implements Element: the inner's own request (the div's
// node, with its containing-block scope and children).
func (d *fixedDivDrawable) RequestLayout(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App) (LayoutID, fixedDivRequest) {
	return d.inner.RequestLayout(w, app), fixedDivRequest{}
}

// Prepaint implements Element: defer the inner's whole draw at the
// fixed priority, under a ZEROED absolute element offset — the
// deferred round prepaints and paints the div at its window position
// (the native bounds of a root-scope child are already window
// coordinates, and the offset is what would scroll or shift them) and
// outside every ancestor's content mask (the ambient mask at the
// deferred round is the frame default). The bounds this wrapper
// receives (native bounds plus the CURRENT offset) are ignored, as
// they should be: the containing-block chain of a fixed element skips
// every ancestor.
func (d *fixedDivDrawable) Prepaint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *fixedDivRequest, w *Window, app *App) fixedDivPrepaint {
	w.DeferDraw(d.inner, Point{}, fixedDrawPriority, nil)
	return fixedDivPrepaint{}
}

// Paint implements Element: nothing — the inner paints in the deferred
// round.
func (d *fixedDivDrawable) Paint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *fixedDivRequest, prepaint *fixedDivPrepaint, w *Window, app *App) {
}
