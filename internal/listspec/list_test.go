package listspec

import (
	"testing"

	"gpui-go/gpui"
)

// This file exercises the variable-height List (ticket14's remaining
// core item, the pinned list.rs): the visible range from per-item
// heights, the layout-time offset clamp, the per-item height remeasure
// feedback, the insertion/removal scroll anchor, the per-index keyed
// element state under items, the scroll-to-reveal placement over
// variable heights, the scroll-delta clamp with the scroll-handler
// hook and the reset guard, and the Infer sizing's request-time
// measurement batch — through the real element pipeline on
// deterministic test windows.

// itemWidth is the width every corpus item requests.
const itemWidth float32 = 200

// listItemSpec is one corpus item: its content identity (the debug
// selector key, which travels with the entry across splices) and its
// requested height.
type listItemSpec struct {
	id     string
	height float32
}

// variableItemSpecs builds the default corpus: ten items with the
// repeating heights 20, 30, 40 (cumulative tops 0, 20, 50, 90, 110,
// 140, 180, 200, 230, 270 — total 290).
func variableItemSpecs(count int) []listItemSpec {
	items := make([]listItemSpec, count)
	for ix := 0; ix < count; ix++ {
		items[ix] = listItemSpec{id: itemSelector(ix), height: variableItemHeight(ix)}
	}
	return items
}

// variableItemHeight is the repeating 20/30/40 pattern.
func variableItemHeight(ix int) float32 {
	return 20 + 10*float32(ix%3)
}

// listView is the variable-list corpus: the items rendered through
// the per-item callback (recording every rendered index), optionally
// as stateful elements whose keyed element state is probed at each
// request.
type listView struct {
	state    *gpui.ListState
	items    []listItemSpec
	stateful bool
	probes   *[]itemProbe
	sizing   gpui.ListSizingBehavior
	// parentHeight wraps the list in a parent div of this height (the
	// Infer sizing's definite available height); zero stretches the
	// list to the window.
	parentHeight float32
	rendered     *[]int
}

// Render implements Render[listView].
func (v *listView) Render(w *gpui.Window, cx *gpui.Context[listView]) gpui.AnyElement {
	list := gpui.List(v.state, func(ix int, w *gpui.Window, app *gpui.App) gpui.AnyElement {
		*v.rendered = append(*v.rendered, ix)
		spec := v.items[ix]
		if v.stateful {
			return newStatefulItem(ix, spec.height, v.probes)
		}
		return gpui.Div().
			W(gpui.PxLength(itemWidth)).
			H(gpui.PxLength(spec.height)).
			DebugSelector(spec.id).
			Child("row " + spec.id).
			IntoElement()
	})
	if v.sizing != gpui.ListAuto {
		list = list.WithSizingBehavior(v.sizing)
	}
	if v.parentHeight > 0 {
		return gpui.Div().
			H(gpui.PxLength(v.parentHeight)).
			Child(list.W(gpui.DefiniteLengthOf(gpui.Fraction(1.0)))).
			IntoElement()
	}
	return list.SizeFull().IntoElement()
}

// drawList draws one frame of the corpus and reports the item indices
// rendered during it and, for the stateful corpus, the keyed-state
// probes recorded during it.
func drawList(t *testing.T, window *gpui.Window, app *gpui.App, view gpui.Entity[listView]) ([]int, []itemProbe) {
	t.Helper()
	rendered := []int{}
	probes := []itemProbe{}
	view.Update(app, func(v *listView, cx *gpui.Context[listView]) {
		v.rendered = &rendered
		v.probes = &probes
	})
	if _, err := gpui.DrawWindowFrame(window); err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}
	return rendered, probes
}

// newListCorpus builds the corpus over the given state and items in a
// 200-wide window of the given height.
func newListCorpus(t *testing.T, state *gpui.ListState, items []listItemSpec, windowHeight float32) (*gpui.App, *gpui.Window, gpui.Entity[listView]) {
	t.Helper()
	ta := gpui.NewTestApp()
	app := ta.App()
	window := gpui.NewTestWindow(app, gpui.Size{Width: itemWidth, Height: windowHeight})
	var sink []int
	view := gpui.NewEntity(app, window.Scope(), func(v *listView, cx *gpui.Context[listView]) {
		v.state = state
		v.items = items
		// The reference List's default sizing (the parent's layout
		// sizes the list node); the Go zero value of the behavior is
		// ListInfer, so the corpus pins the default explicitly.
		v.sizing = gpui.ListAuto
		v.rendered = &sink
	})
	window.SetRootView(gpui.ViewOf(view))
	return app, window, view
}

// newVariableCorpus builds the default corpus: a 10-item list of the
// repeating 20/30/40 heights in a 200x100 window with the given
// overdraw.
func newVariableCorpus(t *testing.T, overdraw float32) (*gpui.App, *gpui.Window, gpui.Entity[listView], *gpui.ListState) {
	t.Helper()
	state := gpui.NewListState(10, overdraw)
	app, window, view := newListCorpus(t, state, variableItemSpecs(10), 100)
	return app, window, view, state
}

// assertItemBounds asserts one item's recorded debug bounds.
func assertItemBounds(t *testing.T, window *gpui.Window, selector string, wantX, wantY, wantWidth, wantHeight float32) {
	t.Helper()
	bounds, ok := gpui.WindowDebugBound(window, selector)
	if !ok {
		t.Fatalf("item %q was not placed", selector)
	}
	if bounds.Origin != (gpui.Point{X: wantX, Y: wantY}) || bounds.Size != (gpui.Size{Width: wantWidth, Height: wantHeight}) {
		t.Fatalf("item %q = %+v, want origin (%v,%v) size %vx%v", selector, bounds, wantX, wantY, wantWidth, wantHeight)
	}
}

// assertRendered asserts the exact rendered index sequence.
func assertRendered(t *testing.T, rendered []int, want []int) {
	t.Helper()
	if len(rendered) != len(want) {
		t.Fatalf("rendered items = %v, want %v", rendered, want)
	}
	for i := range want {
		if rendered[i] != want[i] {
			t.Fatalf("rendered items = %v, want %v", rendered, want)
		}
	}
}

// TestListVisibleRangeFromVariableHeights checks the virtualization
// over variable heights: a 10-item list of 20/30/40 heights in a
// 100px viewport renders exactly the items whose cumulative ranges
// intersect the viewport (the four items covering content 0..110),
// placed at their scrolled origins.
func TestListVisibleRangeFromVariableHeights(t *testing.T) {
	app, window, view, state := newVariableCorpus(t, 0)

	rendered, _ := drawList(t, window, app, view)
	assertRendered(t, rendered, []int{0, 1, 2, 3})
	if top := state.LogicalScrollTop(); top != (gpui.ListOffset{ItemIx: 0, OffsetInItem: 0}) {
		t.Fatalf("logical scroll top = %+v, want {0, 0}", top)
	}
	// The placement: item 2 spans content [50, 90), item 3 [90, 110).
	assertItemBounds(t, window, itemSelector(2), 0, 50, itemWidth, 40)
	assertItemBounds(t, window, itemSelector(3), 0, 90, itemWidth, 20)

	// The item-bounds observable: the full list width at the item's
	// scrolled position; an unrendered item reports no bounds.
	bounds, ok := state.BoundsForItem(2)
	if !ok || bounds.Origin != (gpui.Point{X: 0, Y: 50}) || bounds.Size != (gpui.Size{Width: itemWidth, Height: 40}) {
		t.Fatalf("bounds for item 2 = %+v ok=%v, want origin (0,50) size 200x40", bounds, ok)
	}
	if _, ok := state.BoundsForItem(4); ok {
		t.Fatal("bounds for the unrendered item 4 should be absent")
	}
}

// TestListScrolledRangeAndOffsetClamp checks the scrolled visible
// range over variable heights, the placement under a partial-item
// offset, and the layout-time clamp when the scroll position rests
// past the end of the content: the upward fill renders the trailing
// items and the content bottom lands on the viewport bottom.
func TestListScrolledRangeAndOffsetClamp(t *testing.T) {
	app, window, view, state := newVariableCorpus(t, 0)

	// Scroll 5px into item 3 (content 90): items 3..6 render, item 3's
	// top sits 5px above the viewport.
	state.ScrollTo(gpui.ListOffset{ItemIx: 3, OffsetInItem: 5})
	rendered, _ := drawList(t, window, app, view)
	assertRendered(t, rendered, []int{3, 4, 5, 6})
	assertItemBounds(t, window, itemSelector(3), 0, -5, itemWidth, 20)
	assertItemBounds(t, window, itemSelector(4), 0, 15, itemWidth, 30)
	assertItemBounds(t, window, itemSelector(6), 0, 85, itemWidth, 20)

	// Scroll far past the end: the logical scroll top clamps to the
	// item count, and the layout walks upward from the end until the
	// viewport fills — items 9, 8, 7, 6 render in walk order, the
	// scroll top lands 10px into item 6, and item 9's bottom aligns
	// with the viewport bottom.
	state.ScrollTo(gpui.ListOffset{ItemIx: 50, OffsetInItem: 0})
	rendered, _ = drawList(t, window, app, view)
	assertRendered(t, rendered, []int{9, 8, 7, 6})
	if top := state.LogicalScrollTop(); top != (gpui.ListOffset{ItemIx: 6, OffsetInItem: 10}) {
		t.Fatalf("clamped logical scroll top = %+v, want {6, 10}", top)
	}
	assertItemBounds(t, window, itemSelector(6), 0, -10, itemWidth, 20)
	assertItemBounds(t, window, itemSelector(8), 0, 40, itemWidth, 40)
	assertItemBounds(t, window, itemSelector(9), 0, 80, itemWidth, 20)
	if bounds, ok := gpui.WindowDebugBound(window, itemSelector(9)); !ok || bounds.Bottom() != 100 {
		t.Fatalf("item 9 bottom = %v, want 100 (content clamped to the viewport bottom)", bounds.Bottom())
	}
}

// TestListItemHeightRemeasureFeedback checks the remeasure feedback:
// a visible item whose content grows re-measures on the next frame,
// shifting the subsequent items and growing the measured content
// height.
func TestListItemHeightRemeasureFeedback(t *testing.T) {
	// The overdraw covers the whole content, so every item is measured
	// and the content height is fully observable.
	app, window, view, state := newVariableCorpus(t, 400)

	rendered, _ := drawList(t, window, app, view)
	assertRendered(t, rendered, []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9})
	assertItemBounds(t, window, itemSelector(3), 0, 90, itemWidth, 20)
	if max := state.MaxOffsetForScrollbar(); max.Y != 190 {
		t.Fatalf("max scroll offset = %v, want 190 (content 290 - viewport 100)", max.Y)
	}

	// Item 2 grows from 40 to 70: the next frame re-measures it, item 3
	// shifts down by 30 (its top moves from content 90 to 120, out of
	// the visible range — the walk stops rendering at it), and the
	// content height grows by 30.
	view.Update(app, func(v *listView, cx *gpui.Context[listView]) {
		v.items[2].height = 70
	})
	rendered, _ = drawList(t, window, app, view)
	// Only the items that are still visible re-render; the others keep
	// their cached heights.
	assertRendered(t, rendered, []int{0, 1, 2})
	assertItemBounds(t, window, itemSelector(2), 0, 50, itemWidth, 70)
	bounds, ok := state.BoundsForItem(3)
	if !ok || bounds.Origin.Y != 120 || bounds.Size != (gpui.Size{Width: itemWidth, Height: 20}) {
		t.Fatalf("bounds for item 3 after the growth = %+v ok=%v, want y 120 size 200x20 (shifted down by 30)", bounds, ok)
	}
	if max := state.MaxOffsetForScrollbar(); max.Y != 220 {
		t.Fatalf("max scroll offset after the growth = %v, want 220 (content 320 - viewport 100)", max.Y)
	}
}

// TestListInsertionAnchorPreservesScrollPosition checks the scroll
// anchor on insertion: content inserted above the scroll position
// shifts the anchor's index while preserving the same pixel offset
// into the anchor item, so the view does not jump; content inserted
// below the scroll position changes nothing.
func TestListInsertionAnchorPreservesScrollPosition(t *testing.T) {
	state := gpui.NewListState(10, 0)
	entries := variableItemSpecs(10)
	app, window, view := newListCorpus(t, state, entries, 100)

	// Scroll 5px into item 3.
	state.ScrollTo(gpui.ListOffset{ItemIx: 3, OffsetInItem: 5})
	drawList(t, window, app, view)
	assertItemBounds(t, window, itemSelector(3), 0, -5, itemWidth, 20)
	assertItemBounds(t, window, itemSelector(4), 0, 15, itemWidth, 30)

	// Insert two items above the scroll position: the anchor item
	// shifts from index 3 to index 5 with the same offset into it, so
	// the view does not move.
	view.Update(app, func(v *listView, cx *gpui.Context[listView]) {
		inserted := []listItemSpec{{id: "entry-a", height: 20}, {id: "entry-b", height: 30}}
		v.items = append(append(inserted, v.items...), listItemSpec{})[:len(inserted)+len(v.items)]
	})
	state.Splice(0, 0, 2)
	rendered, _ := drawList(t, window, app, view)
	assertRendered(t, rendered, []int{5, 6, 7, 8})
	if top := state.LogicalScrollTop(); top != (gpui.ListOffset{ItemIx: 5, OffsetInItem: 5}) {
		t.Fatalf("logical scroll top after inserting above = %+v, want {5, 5}", top)
	}
	assertItemBounds(t, window, itemSelector(3), 0, -5, itemWidth, 20)
	assertItemBounds(t, window, itemSelector(4), 0, 15, itemWidth, 30)

	// Insert an item below the scroll position: no change at all.
	view.Update(app, func(v *listView, cx *gpui.Context[listView]) {
		rest := append([]listItemSpec{{id: "entry-c", height: 20}}, v.items[9:]...)
		v.items = append(v.items[:9:9], rest...)
	})
	state.Splice(9, 9, 1)
	rendered, _ = drawList(t, window, app, view)
	assertRendered(t, rendered, []int{5, 6, 7, 8})
	if top := state.LogicalScrollTop(); top != (gpui.ListOffset{ItemIx: 5, OffsetInItem: 5}) {
		t.Fatalf("logical scroll top after inserting below = %+v, want {5, 5}", top)
	}
	assertItemBounds(t, window, itemSelector(3), 0, -5, itemWidth, 20)
}

// TestListRemovalOfAnchorItemClampsAndContinues checks the anchor on
// removal: removing the range containing the anchor item clamps the
// scroll top to the range start and the list continues rendering from
// there; removing items above the anchor preserves the view.
func TestListRemovalOfAnchorItemClampsAndContinues(t *testing.T) {
	state := gpui.NewListState(10, 0)
	entries := variableItemSpecs(10)
	app, window, view := newListCorpus(t, state, entries, 100)

	// Scroll 5px into item 3, then remove item 3 itself: the anchor is
	// gone, so the scroll top clamps to the removed range's start and
	// the next item renders from the viewport top.
	state.ScrollTo(gpui.ListOffset{ItemIx: 3, OffsetInItem: 5})
	drawList(t, window, app, view)
	view.Update(app, func(v *listView, cx *gpui.Context[listView]) {
		v.items = append(v.items[:3:3], v.items[4:]...)
	})
	state.Splice(3, 4, 0)
	rendered, _ := drawList(t, window, app, view)
	assertRendered(t, rendered, []int{3, 4, 5, 6})
	if top := state.LogicalScrollTop(); top != (gpui.ListOffset{ItemIx: 3, OffsetInItem: 0}) {
		t.Fatalf("logical scroll top after removing the anchor = %+v, want {3, 0}", top)
	}
	assertItemBounds(t, window, itemSelector(4), 0, 0, itemWidth, 30)
	assertItemBounds(t, window, itemSelector(5), 0, 30, itemWidth, 40)

	// Remove two items above the anchor: the anchor's index shifts up
	// by two with its offset preserved, so the view does not move.
	view.Update(app, func(v *listView, cx *gpui.Context[listView]) {
		v.items = v.items[2:]
	})
	state.Splice(0, 2, 0)
	rendered, _ = drawList(t, window, app, view)
	assertRendered(t, rendered, []int{1, 2, 3, 4})
	if top := state.LogicalScrollTop(); top != (gpui.ListOffset{ItemIx: 1, OffsetInItem: 0}) {
		t.Fatalf("logical scroll top after removing above = %+v, want {1, 0}", top)
	}
	assertItemBounds(t, window, itemSelector(4), 0, 0, itemWidth, 30)
}

// ---------------------------------------------------------------------------
// The keyed element state corpus
// ---------------------------------------------------------------------------

// itemProbe records one keyed-state observation: the item index and
// the retained state's request count before this request incremented
// it (0 means fresh state).
type itemProbe struct {
	ix      int
	renders int
}

// itemCounterState is the retained per-item element state.
type itemCounterState struct {
	renders int
}

// statefulItemElement is one item element whose retained keyed state
// counts its layout requests; each request records a probe.
type statefulItemElement struct {
	ix     int
	height float32
	probes *[]itemProbe
}

// ID implements Element: the keyed-state identity under the list's
// per-index wrapper.
func (e *statefulItemElement) ID() (gpui.ElementID, bool) {
	return gpui.NameElementID("stateful"), true
}

// SourceLocation implements Element.
func (e *statefulItemElement) SourceLocation() *gpui.SourceLocation { return nil }

// RequestLayout implements Element: read/increment the retained state
// keyed by this element's global id (view id, item index, "stateful").
func (e *statefulItemElement) RequestLayout(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, w *gpui.Window, app *gpui.App) (gpui.LayoutID, struct{}) {
	prior := 0
	gpui.WithElementState(w, global, func(state *itemCounterState, w *gpui.Window) (struct{}, itemCounterState) {
		if state != nil {
			prior = state.renders
		}
		return struct{}{}, itemCounterState{renders: prior + 1}
	})
	*e.probes = append(*e.probes, itemProbe{ix: e.ix, renders: prior})
	style := gpui.DefaultStyle()
	style.Size = gpui.LengthSize{Width: gpui.PxLength(itemWidth), Height: gpui.PxLength(e.height)}
	layoutID, err := gpui.RequestElementLayout(w, style)
	if err != nil {
		t := "gpui: stateful item request_layout: " + err.Error()
		panic(t)
	}
	return layoutID, struct{}{}
}

// Prepaint implements Element.
func (e *statefulItemElement) Prepaint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *struct{}, w *gpui.Window, app *gpui.App) struct{} {
	return struct{}{}
}

// Paint implements Element.
func (e *statefulItemElement) Paint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *struct{}, prepaint *struct{}, w *gpui.Window, app *gpui.App) {
}

// newStatefulItem builds one stateful item element.
func newStatefulItem(ix int, height float32, probes *[]itemProbe) gpui.AnyElement {
	return gpui.CustomElement[struct{}, struct{}](&statefulItemElement{ix: ix, height: height, probes: probes})
}

// assertProbes asserts the exact probe sequence.
func assertProbes(t *testing.T, probes []itemProbe, want []itemProbe) {
	t.Helper()
	if len(probes) != len(want) {
		t.Fatalf("probes = %+v, want %+v", probes, want)
	}
	for i := range want {
		if probes[i] != want[i] {
			t.Fatalf("probes = %+v, want %+v", probes, want)
		}
	}
}

// TestListItemKeyedElementStateRetention checks the stable item
// identity: keyed element state under an item survives redraws while
// the item keeps rendering (the per-index element id keeps the state
// key stable), and drops when the item leaves the virtualized range
// (the frame-end retention), so a returning item starts fresh.
func TestListItemKeyedElementStateRetention(t *testing.T) {
	state := gpui.NewListState(10, 0)
	app, window, view := newListCorpus(t, state, variableItemSpecs(10), 100)
	view.Update(app, func(v *listView, cx *gpui.Context[listView]) { v.stateful = true })

	// First draw: the visible items' states are fresh (renders 0
	// before the increment).
	_, probes := drawList(t, window, app, view)
	assertProbes(t, probes, []itemProbe{{0, 0}, {1, 0}, {2, 0}, {3, 0}})

	// A redraw of the same range: the states survive (renders 1).
	_, probes = drawList(t, window, app, view)
	assertProbes(t, probes, []itemProbe{{0, 1}, {1, 1}, {2, 1}, {3, 1}})

	// Scroll items 0..3 out of the range: their states are not
	// accessed and the frame-end retention drops them.
	state.ScrollTo(gpui.ListOffset{ItemIx: 5, OffsetInItem: 0})
	_, probes = drawList(t, window, app, view)
	assertProbes(t, probes, []itemProbe{{5, 0}, {6, 0}, {7, 0}, {8, 0}})

	// Scroll back: the returning items start fresh (renders 0).
	state.ScrollTo(gpui.ListOffset{ItemIx: 0, OffsetInItem: 0})
	_, probes = drawList(t, window, app, view)
	assertProbes(t, probes, []itemProbe{{0, 0}, {1, 0}, {2, 0}, {3, 0}})
}

// TestListRemeasureItemsPreservesScrollOffset checks the remeasure
// anchor (the pinned test_remeasure_item_preserves_scroll_offset): a
// remeasured scroll-top item restores the same pixel offset into
// itself when it grows, and clamps the offset to its new height when
// it shrinks.
func TestListRemeasureItemsPreservesScrollOffset(t *testing.T) {
	state := gpui.NewListState(20, 0)
	items := make([]listItemSpec, 20)
	for ix := range items {
		items[ix] = listItemSpec{id: itemSelector(ix), height: 100}
	}
	app, window, view := newListCorpus(t, state, items, 200)

	// Scroll 40px into item 5.
	state.ScrollTo(gpui.ListOffset{ItemIx: 5, OffsetInItem: 40})
	rendered, _ := drawList(t, window, app, view)
	assertRendered(t, rendered, []int{5, 6, 7})
	assertItemBounds(t, window, itemSelector(5), 0, -40, itemWidth, 100)
	assertItemBounds(t, window, itemSelector(6), 0, 60, itemWidth, 100)
	assertItemBounds(t, window, itemSelector(7), 0, 160, itemWidth, 100)

	// Item 5 grows from 100 to 200 and is invalidated: the absolute
	// anchor restores the same 40px offset into it, so the view does
	// not move.
	view.Update(app, func(v *listView, cx *gpui.Context[listView]) {
		v.items[5].height = 200
	})
	state.RemeasureItems(5, 6)
	rendered, _ = drawList(t, window, app, view)
	assertRendered(t, rendered, []int{5, 6})
	if top := state.LogicalScrollTop(); top != (gpui.ListOffset{ItemIx: 5, OffsetInItem: 40}) {
		t.Fatalf("logical scroll top after the growth = %+v, want {5, 40}", top)
	}
	assertItemBounds(t, window, itemSelector(5), 0, -40, itemWidth, 200)
	assertItemBounds(t, window, itemSelector(6), 0, 160, itemWidth, 100)

	// Item 5 shrinks from 200 to 30: the anchor clamps the 40px offset
	// to the new 30px height, keeping item 5's tail at the viewport top
	// with item 6 directly below it.
	view.Update(app, func(v *listView, cx *gpui.Context[listView]) {
		v.items[5].height = 30
	})
	state.RemeasureItems(5, 6)
	rendered, _ = drawList(t, window, app, view)
	assertRendered(t, rendered, []int{5, 6, 7})
	if top := state.LogicalScrollTop(); top != (gpui.ListOffset{ItemIx: 5, OffsetInItem: 30}) {
		t.Fatalf("logical scroll top after the shrink = %+v, want {5, 30} (clamped to the new height)", top)
	}
	assertItemBounds(t, window, itemSelector(5), 0, -30, itemWidth, 30)
	assertItemBounds(t, window, itemSelector(6), 0, 0, itemWidth, 100)
}

// TestListScrollToRevealItemOverVariableHeights checks the
// scroll-to-item placement over variable heights: an item below the
// scroll top lands with its bottom on the viewport bottom, an item at
// or above the scroll top moves to the viewport top.
func TestListScrollToRevealItemOverVariableHeights(t *testing.T) {
	// The overdraw covers the whole content, so the reveal math sees
	// every item's height.
	app, window, view, state := newVariableCorpus(t, 400)
	drawList(t, window, app, view)

	// Reveal item 8: its bottom (content 270) aligns with the viewport
	// bottom, so the scroll top lands 30px into item 5 (goal top 170).
	state.ScrollToRevealItem(8)
	rendered, _ := drawList(t, window, app, view)
	assertRendered(t, rendered, []int{5, 6, 7, 8})
	if top := state.LogicalScrollTop(); top != (gpui.ListOffset{ItemIx: 5, OffsetInItem: 30}) {
		t.Fatalf("reveal-8 logical scroll top = %+v, want {5, 30}", top)
	}
	assertItemBounds(t, window, itemSelector(8), 0, 60, itemWidth, 40)
	if bounds, ok := gpui.WindowDebugBound(window, itemSelector(8)); !ok || bounds.Bottom() != 100 {
		t.Fatalf("item 8 bottom = %v, want 100 (fully visible at the viewport bottom)", bounds.Bottom())
	}

	// Reveal item 2, at or above the scroll top: it moves to the
	// viewport top.
	state.ScrollToRevealItem(2)
	rendered, _ = drawList(t, window, app, view)
	assertRendered(t, rendered, []int{2, 3, 4, 5})
	if top := state.LogicalScrollTop(); top != (gpui.ListOffset{ItemIx: 2, OffsetInItem: 0}) {
		t.Fatalf("reveal-2 logical scroll top = %+v, want {2, 0}", top)
	}
	assertItemBounds(t, window, itemSelector(2), 0, 0, itemWidth, 40)
}

// TestListScrollByClampsAndFiresScrollHandler checks the scroll-delta
// path: the pixel offset clamps to [0, content - viewport] over
// variable heights, the registered scroll handler observes the
// resulting visible range, scroll events are dropped after a reset
// until the next paint, and the reset also re-seeds the item count.
func TestListScrollByClampsAndFiresScrollHandler(t *testing.T) {
	app, window, view, state := newVariableCorpus(t, 400)

	var events []*gpui.ListScrollEvent
	state.SetScrollHandler(func(e *gpui.ListScrollEvent, w *gpui.Window, app *gpui.App) {
		events = append(events, e)
	})
	drawList(t, window, app, view)

	// Scroll down 35px: 20px into item 1 (seek over the measured
	// heights), visible range [1, 5) (content 35..135 covers items
	// 1..4).
	state.ScrollBy(35, window, app)
	if top := state.LogicalScrollTop(); top != (gpui.ListOffset{ItemIx: 1, OffsetInItem: 15}) {
		t.Fatalf("scroll-by logical top = %+v, want {1, 15}", top)
	}
	if len(events) != 1 {
		t.Fatalf("scroll events = %d, want 1", len(events))
	}
	if e := events[0]; e.VisibleRangeStart != 1 || e.VisibleRangeEnd != 5 || e.Count != 10 || !e.IsScrolled {
		t.Fatalf("scroll event = %+v, want range [1,5) count 10 scrolled", e)
	}

	// Scrolling up past the top clamps at zero.
	state.ScrollBy(-1000, window, app)
	if top := state.LogicalScrollTop(); top != (gpui.ListOffset{ItemIx: 0, OffsetInItem: 0}) {
		t.Fatalf("top-clamped logical top = %+v, want {0, 0}", top)
	}

	// Scrolling down past the end clamps at content - viewport (190),
	// landing 10px into item 6.
	state.ScrollBy(10000, window, app)
	if top := state.LogicalScrollTop(); top != (gpui.ListOffset{ItemIx: 6, OffsetInItem: 10}) {
		t.Fatalf("bottom-clamped logical top = %+v, want {6, 10}", top)
	}

	// A reset drops scroll events until the next paint and reseeds the
	// item count.
	state.Reset(5)
	view.Update(app, func(v *listView, cx *gpui.Context[listView]) { v.items = v.items[:5] })
	if count := state.ItemCount(); count != 5 {
		t.Fatalf("item count after reset = %d, want 5", count)
	}
	if top := state.LogicalScrollTop(); top != (gpui.ListOffset{ItemIx: 0, OffsetInItem: 0}) {
		t.Fatalf("logical top after reset = %+v, want {0, 0}", top)
	}
	before := len(events)
	state.ScrollBy(30, window, app)
	if top := state.LogicalScrollTop(); top != (gpui.ListOffset{ItemIx: 0, OffsetInItem: 0}) {
		t.Fatalf("logical top after the dropped scroll = %+v, want {0, 0} (dropped after reset)", top)
	}
	if len(events) != before {
		t.Fatalf("a dropped scroll fired %d events, want 0", len(events)-before)
	}

	// After the next paint consumes the reset, scrolling works again
	// (5 items: 20+30+40+20+30 = 140px of content, max scroll 40).
	drawList(t, window, app, view)
	state.ScrollBy(30, window, app)
	if top := state.LogicalScrollTop(); top != (gpui.ListOffset{ItemIx: 1, OffsetInItem: 10}) {
		t.Fatalf("logical top after the post-reset scroll = %+v, want {1, 10}", top)
	}
}

// TestListInferSizingMeasuresAtRequestLayout checks the Infer sizing:
// the items are measured at request time under max-content width (the
// pinned request-layout layout_items call with the overdraw as the
// first frame's available height), the list node sizes to
// min(content, available) inside its parent, and the prepaint renders
// the visible range of the sized viewport.
func TestListInferSizingMeasuresAtRequestLayout(t *testing.T) {
	state := gpui.NewListState(10, 400)
	items := variableItemSpecs(10)
	ta := gpui.NewTestApp()
	app := ta.App()
	window := gpui.NewTestWindow(app, gpui.Size{Width: itemWidth, Height: 200})
	var sink []int
	view := gpui.NewEntity(app, window.Scope(), func(v *listView, cx *gpui.Context[listView]) {
		v.state = state
		v.items = items
		v.sizing = gpui.ListInfer
		v.parentHeight = 150
		v.rendered = &sink
	})
	window.SetRootView(gpui.ViewOf(view))

	rendered, _ := drawList(t, window, app, view)
	// The request-time batch measures all ten items (the overdraw 400
	// covers the 290px content), and the first prepaint re-measures
	// them all again: the pinned width-change invalidation clears the
	// cached heights when the last layout bounds are absent.
	assertRendered(t, rendered, []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9})

	// The list sized itself to the parent's 150px (the content 290
	// clamped by the available height).
	if bounds := state.ViewportBounds(); bounds.Origin != (gpui.Point{}) || bounds.Size != (gpui.Size{Width: itemWidth, Height: 150}) {
		t.Fatalf("viewport bounds = %+v, want origin (0,0) size 200x150", bounds)
	}
	// The visible range covers content 0..180 (items 0..5); item 5
	// spans [140, 180).
	assertItemBounds(t, window, itemSelector(5), 0, 140, itemWidth, 40)
	if max := state.MaxOffsetForScrollbar(); max.Y != 140 {
		t.Fatalf("max scroll offset = %v, want 140 (content 290 - viewport 150)", max.Y)
	}
}
