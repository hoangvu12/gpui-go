package listspec

import (
	"testing"
	"time"

	"gpui-go/gpui"
)

// This file closes ticket14's retained-callback and large-list
// criteria against the uniform list: the focused item's key dispatch
// flows through the retained dispatch tree while the item renders,
// stops reaching it when it scrolls out of the virtualized range, and
// returns when it scrolls back; a large corpus renders only its
// visible range in bounded time (a smoke check, not a benchmark gate).
//
// The pointer hit-testing half of the criterion rides the hitbox
// surface, which div.go's header records as the input ticket's scope;
// the item placement geometry (the debug-selector bounds these tests
// read) is the substrate that surface will consume.

// focusView is the focus corpus: a uniform list whose item at
// focusIndex tracks a stable (view-held) focus handle with a key-down
// listener that records deliveries.
type focusView struct {
	handle     *gpui.UniformListScrollHandle
	count      int
	focusIndex int
	focus      gpui.FocusHandle
	// deliveries counts key-down deliveries to the focused item.
	deliveries *int
	// rendered receives every rendered item index.
	rendered *[]int
}

// Render implements Render[focusView].
func (v *focusView) Render(w *gpui.Window, cx *gpui.Context[focusView]) gpui.AnyElement {
	return gpui.UniformList(v.handle, v.count, func(start, end int, w *gpui.Window, app *gpui.App) []gpui.AnyElement {
		items := make([]gpui.AnyElement, 0, end-start)
		for ix := start; ix < end; ix++ {
			*v.rendered = append(*v.rendered, ix)
			item := gpui.Div().
				W(gpui.PxLength(200)).
				H(gpui.PxLength(20)).
				DebugSelector(itemSelector(ix)).
				Child(itemLabel(ix))
			if ix == v.focusIndex {
				item = item.TrackFocus(v.focus).
					OnKeyDown(func(e *gpui.KeyDownEvent, w *gpui.Window, app *gpui.App) {
						*v.deliveries++
					})
			}
			items = append(items, item.IntoElement())
		}
		return items
	}).
		SizeFull().
		IntoElement()
}

// TestUniformListRetainsFocusedItemDispatch checks the retained
// dispatch callbacks: the focused item's key listener fires through
// the retained dispatch tree across redraws, stops firing when the
// item scrolls out of the virtualized range, and fires again when it
// returns.
func TestUniformListRetainsFocusedItemDispatch(t *testing.T) {
	ta := gpui.NewTestApp()
	app := ta.App()
	window := gpui.NewTestWindow(app, gpui.Size{Width: 200, Height: 100})
	deliveries := 0
	var sink []int
	focus := gpui.NewFocusHandle(window)
	view := gpui.NewEntity(app, window.Scope(), func(v *focusView, cx *gpui.Context[focusView]) {
		v.handle = gpui.NewUniformListScrollHandle()
		v.count = 100
		v.focusIndex = 3
		v.focus = focus
		v.deliveries = &deliveries
		v.rendered = &sink
	})
	window.SetRootView(gpui.ViewOf(view))
	if _, err := gpui.DrawWindowFrame(window); err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}
	window.Focus(focus)
	if _, err := gpui.DrawWindowFrame(window); err != nil {
		t.Fatalf("focus draw: %v", err)
	}

	// The focused item is visible: the key dispatch reaches its
	// listener.
	window.DispatchKeyEvent(&gpui.KeyDownEvent{Keystroke: gpui.MustParseKeystroke("a")}, app)
	if deliveries != 1 {
		t.Fatalf("key deliveries to the focused item = %d, want 1", deliveries)
	}

	// A redraw (the same visible range) keeps the callback retained.
	sink = nil
	view.Update(app, func(v *focusView, cx *gpui.Context[focusView]) {})
	if _, err := gpui.DrawWindowFrame(window); err != nil {
		t.Fatalf("redraw: %v", err)
	}
	window.DispatchKeyEvent(&gpui.KeyDownEvent{Keystroke: gpui.MustParseKeystroke("a")}, app)
	if deliveries != 2 {
		t.Fatalf("key deliveries after the redraw = %d, want 2 (retained callback)", deliveries)
	}

	// Scroll the focused item out of the virtualized range: the item
	// no longer renders, its dispatch node is gone, and the key falls
	// through without delivery.
	view.Update(app, func(v *focusView, cx *gpui.Context[focusView]) {
		v.handle.SetOffset(gpui.Point{X: 0, Y: -1200}) // items 60.. render
	})
	sink = nil
	if _, err := gpui.DrawWindowFrame(window); err != nil {
		t.Fatalf("scrolled draw: %v", err)
	}
	window.DispatchKeyEvent(&gpui.KeyDownEvent{Keystroke: gpui.MustParseKeystroke("a")}, app)
	if deliveries != 2 {
		t.Fatalf("key deliveries after scrolling the item out = %d, want 2 (no delivery: the node is gone)", deliveries)
	}

	// Scroll the item back into the range: the callback returns with
	// the item.
	view.Update(app, func(v *focusView, cx *gpui.Context[focusView]) {
		v.handle.SetOffset(gpui.Point{})
	})
	sink = nil
	if _, err := gpui.DrawWindowFrame(window); err != nil {
		t.Fatalf("return draw: %v", err)
	}
	window.DispatchKeyEvent(&gpui.KeyDownEvent{Keystroke: gpui.MustParseKeystroke("a")}, app)
	if deliveries != 3 {
		t.Fatalf("key deliveries after the item returns = %d, want 3 (the callback returns)", deliveries)
	}
}

// TestUniformListLargeCorpusRendersVisibleRangeOnly is the large-list
// smoke check: a 10,000-item list renders exactly its visible range in
// a bounded draw time (no benchmark gate; stutter tracing remains the
// renderer ticket's observability).
func TestUniformListLargeCorpusRendersVisibleRangeOnly(t *testing.T) {
	const count = 10000
	app, window, view, handle := newUniformCorpus(t, count)

	start := time.Now()
	rendered := drawUniform(t, window, app, view)
	elapsed := time.Since(start)
	if len(rendered) != 6 {
		t.Fatalf("a 10k-item list rendered %d items, want the measurement plus the 5 visible", len(rendered))
	}
	// The smoke bound: a virtualized draw of 10k items must stay well
	// under a generous 2s ceiling (the point is O(visible) behavior,
	// not a number).
	if elapsed > 2*time.Second {
		t.Fatalf("the 10k-item draw took %v, want under 2s (virtualization must be O(visible))", elapsed)
	}

	// Scrolling deep into the corpus still renders only the visible
	// range.
	handle.SetOffset(gpui.Point{X: 0, Y: -199800}) // item 9990 (2000px content - 100 viewport)
	rendered = drawUniform(t, window, app, view)
	if len(rendered) != 6 || rendered[1] < 9990 {
		t.Fatalf("deep scroll rendered %v, want the measurement plus the last visible range", rendered)
	}
}
