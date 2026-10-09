package listspec

import (
	"testing"

	"gpui-go/gpui"
)

// This file exercises the uniform list (ticket14's virtualized-list
// slice): the visible-range rendering, the scrolled placement, the
// offset clamp against the content size, the deferred scroll-to-item
// strategies and the scroll stability across content changes, through
// the real element pipeline on deterministic test windows.

// uniformView is the uniform-list corpus: a full-size uniform list of
// count fixed-size (200x20) items whose render callback records the
// rendered item batches and labels each item with a debug selector.
type uniformView struct {
	handle *gpui.UniformListScrollHandle
	count  int
	// rendered receives every rendered item index, measurement
	// included (the pinned request-layout measurement renders item 0).
	rendered *[]int
}

// Render implements Render[uniformView].
func (v *uniformView) Render(w *gpui.Window, cx *gpui.Context[uniformView]) gpui.AnyElement {
	return gpui.UniformList(v.handle, v.count, func(start, end int, w *gpui.Window, app *gpui.App) []gpui.AnyElement {
		items := make([]gpui.AnyElement, 0, end-start)
		for ix := start; ix < end; ix++ {
			*v.rendered = append(*v.rendered, ix)
			items = append(items, gpui.Div().
				W(gpui.PxLength(200)).
				H(gpui.PxLength(20)).
				DebugSelector(itemSelector(ix)).
				Child(itemLabel(ix)).
				IntoElement())
		}
		return items
	}).
		SizeFull().
		IntoElement()
}

// itemSelector is the debug selector of item ix.
func itemSelector(ix int) string { return "item-" + itoa(ix) }

// itemLabel is the text of item ix.
func itemLabel(ix int) string { return "row " + itoa(ix) }

// itoa is the integer-to-string helper (strconv kept out for the
// corpus's self-containment).
func itoa(ix int) string {
	if ix == 0 {
		return "0"
	}
	negative := ix < 0
	if negative {
		ix = -ix
	}
	var digits []byte
	for ix > 0 {
		digits = append([]byte{byte('0' + ix%10)}, digits...)
		ix /= 10
	}
	if negative {
		return "-" + string(digits)
	}
	return string(digits)
}

// drawUniform draws one frame of the uniform corpus and reports the
// item indices rendered during it.
func drawUniform(t *testing.T, window *gpui.Window, app *gpui.App, view gpui.Entity[uniformView]) []int {
	t.Helper()
	rendered := []int{}
	view.Update(app, func(v *uniformView, cx *gpui.Context[uniformView]) {
		v.rendered = &rendered
	})
	if _, err := gpui.DrawWindowFrame(window); err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}
	return rendered
}

// newUniformCorpus builds the corpus with the given item count.
func newUniformCorpus(t *testing.T, count int) (*gpui.App, *gpui.Window, gpui.Entity[uniformView], *gpui.UniformListScrollHandle) {
	t.Helper()
	ta := gpui.NewTestApp()
	app := ta.App()
	window := gpui.NewTestWindow(app, gpui.Size{Width: 200, Height: 100})
	var sink []int
	handle := gpui.NewUniformListScrollHandle()
	view := gpui.NewEntity(app, window.Scope(), func(v *uniformView, cx *gpui.Context[uniformView]) {
		v.handle = handle
		v.count = count
		v.rendered = &sink
	})
	window.SetRootView(gpui.ViewOf(view))
	return app, window, view, handle
}

// TestUniformListRendersOnlyVisibleItems checks the virtualization: a
// 100-item list in a 100px viewport with 20px items renders exactly
// the visible five, placed at their scrolled origins.
func TestUniformListRendersOnlyVisibleItems(t *testing.T) {
	app, window, view, _ := newUniformCorpus(t, 100)

	rendered := drawUniform(t, window, app, view)
	// The measurement render (item 0) precedes the visible batch.
	if len(rendered) != 6 || rendered[0] != 0 || rendered[1] != 0 {
		t.Fatalf("rendered items = %v, want the measurement [0] plus the visible [0..5)", rendered)
	}
	for i := 2; i < len(rendered); i++ {
		if rendered[i] != i-1 {
			t.Fatalf("rendered item %d = index %d, want the contiguous visible range", i, rendered[i])
		}
	}
	// The placement: item 3 at y 60 with the full list width.
	bounds, ok := gpui.WindowDebugBound(window, itemSelector(3))
	if !ok {
		t.Fatal("item 3 was not placed")
	}
	if bounds.Origin != (gpui.Point{X: 0, Y: 60}) || bounds.Size != (gpui.Size{Width: 200, Height: 20}) {
		t.Fatalf("item 3 = %+v, want origin (0,60) size 200x20", bounds)
	}
}

// TestUniformListScrolledRangeAndClamp checks the scrolled visible
// range, the placement under a scroll offset and the clamp at the
// content end.
func TestUniformListScrolledRangeAndClamp(t *testing.T) {
	app, window, view, handle := newUniformCorpus(t, 100)

	// Scroll 40px down: items 2..7 render, item 3 sits at y 20.
	handle.SetOffset(gpui.Point{X: 0, Y: -40})
	rendered := drawUniform(t, window, app, view)
	// The measurement (item 0) precedes the visible batch [2..7).
	if len(rendered) != 6 || rendered[1] != 2 {
		t.Fatalf("scrolled rendered items = %v, want the measurement [0] plus [2..7)", rendered)
	}
	bounds, ok := gpui.WindowDebugBound(window, itemSelector(3))
	if !ok {
		t.Fatal("item 3 was not placed")
	}
	if bounds.Origin.Y != 20 {
		t.Fatalf("scrolled item 3 y = %v, want 20 (60 - 40)", bounds.Origin.Y)
	}

	// Scroll far past the end: the offset clamps to content - viewport
	// (2000 - 100 = 1900), so the last visible item is 95.
	handle.SetOffset(gpui.Point{X: 0, Y: -5000})
	rendered = drawUniform(t, window, app, view)
	// The request-layout measurement always renders item 0 (the pinned
	// measure_item(None)); the clamped visible batch is [95..100).
	if len(rendered) != 6 || rendered[0] != 0 || rendered[1] != 95 || rendered[len(rendered)-1] != 99 {
		t.Fatalf("clamped rendered items = %v, want the measurement [0] plus [95..100)", rendered)
	}
	if offset := handle.Offset(); offset.Y != -1900 {
		t.Fatalf("clamped offset = %v, want -1900", offset)
	}
}

// TestUniformListScrollToItemStrategies checks the deferred
// scroll-to-item placement: top, center and nearest.
func TestUniformListScrollToItemStrategies(t *testing.T) {
	app, window, view, handle := newUniformCorpus(t, 100)

	// Top: item 50 lands at the viewport top.
	handle.SetOffset(gpui.Point{})
	handle.ScrollToItem(50, gpui.ScrollTop)
	drawUniform(t, window, app, view)
	if offset := handle.Offset(); offset.Y != -1000 {
		t.Fatalf("scroll-to-top offset = %v, want -1000", offset)
	}

	// Center: item 50's center aligns with the viewport center
	// (itemCenter 1010 - viewportCenter 50 = 960).
	handle.SetOffset(gpui.Point{})
	handle.ScrollToItem(50, gpui.ScrollCenter)
	drawUniform(t, window, app, view)
	if offset := handle.Offset(); offset.Y != -960 {
		t.Fatalf("scroll-to-center offset = %v, want -960", offset)
	}

	// Nearest with a visible item does not scroll.
	handle.SetOffset(gpui.Point{X: 0, Y: -40})
	handle.ScrollToItem(3, gpui.ScrollNearest)
	drawUniform(t, window, app, view)
	if offset := handle.Offset(); offset.Y != -40 {
		t.Fatalf("nearest-visible offset = %v, want -40 (no scroll)", offset)
	}

	// Nearest with an item below the viewport scrolls it to the bottom.
	handle.SetOffset(gpui.Point{X: 0, Y: -40})
	handle.ScrollToItem(90, gpui.ScrollNearest)
	drawUniform(t, window, app, view)
	// Bottom placement: itemBottom 1820 - viewport 100 = 1720, clamped.
	if offset := handle.Offset(); offset.Y != -1720 {
		t.Fatalf("nearest-below offset = %v, want -1720", offset)
	}
}

// TestUniformListScrollStableAcrossContentChange checks the ticket's
// insertion/removal criterion for the uniform list: shrinking or
// growing the content preserves the scroll position while it stays
// within the new bounds, and clamps when it does not.
func TestUniformListScrollStableAcrossContentChange(t *testing.T) {
	app, window, view, handle := newUniformCorpus(t, 100)

	handle.SetOffset(gpui.Point{X: 0, Y: -60})
	drawUniform(t, window, app, view)
	bounds, ok := gpui.WindowDebugBound(window, itemSelector(3))
	if !ok || bounds.Origin.Y != 0 {
		t.Fatalf("item 3 before the content change = %+v, want y 0", bounds)
	}

	// Shrink the content to 50 items (1000px): the -60 offset is well
	// within the new clamp (900), so the scroll is preserved.
	view.Update(app, func(v *uniformView, cx *gpui.Context[uniformView]) { v.count = 50 })
	drawUniform(t, window, app, view)
	if offset := handle.Offset(); offset.Y != -60 {
		t.Fatalf("offset after shrinking = %v, want -60 (preserved)", offset)
	}
	bounds, ok = gpui.WindowDebugBound(window, itemSelector(3))
	if !ok || bounds.Origin.Y != 0 {
		t.Fatalf("item 3 after shrinking = %+v, want y 0 (stable scroll)", bounds)
	}

	// Shrink to 10 items (200px): -60 stays within the 100px clamp
	// range? content 200 - viewport 100 = 100 max: -60 is valid, so
	// still preserved.
	view.Update(app, func(v *uniformView, cx *gpui.Context[uniformView]) { v.count = 10 })
	drawUniform(t, window, app, view)
	if offset := handle.Offset(); offset.Y != -60 {
		t.Fatalf("offset after shrinking to 10 = %v, want -60 (preserved)", offset)
	}

	// Shrink to 4 items (80px): the content no longer fills the
	// viewport — the offset clamps to zero.
	view.Update(app, func(v *uniformView, cx *gpui.Context[uniformView]) { v.count = 4 })
	drawUniform(t, window, app, view)
	if offset := handle.Offset(); offset.Y != 0 {
		t.Fatalf("offset after shrinking below the viewport = %v, want 0 (clamped)", offset)
	}
}
