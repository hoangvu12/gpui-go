package gpui

import (
	"fmt"
	"testing"
)

// The List's autoscroll path (Base ticket 37): R:list.rs:1250-1361,
// 1571-1597 with R:window.rs request_autoscroll / take_autoscroll /
// transact.

// autoscrollRows renders 20px rows; the row at requester asks, during its
// prepaint, to scroll the bounds request(rowBounds) into view.
func autoscrollRows(requester int, request func(Bounds) Bounds) func(ix int, w *Window, app *App) AnyElement {
	return func(ix int, w *Window, app *App) AnyElement {
		if ix == requester {
			return Canvas(func(bounds Bounds, w *Window, _ *App) struct{} {
				w.RequestAutoscroll(request(bounds))
				return struct{}{}
			}, func(Bounds, struct{}, *Window, *App) {}).H(Px(20)).WFull().IntoElement()
		}
		return Div().H(Px(20)).WFull().DebugSelector(fmt.Sprintf("row-%d", ix)).
			OnMouseDown(MouseButtonLeft, func(*MouseDownEvent, *Window, *App) {}).IntoElement()
	}
}

func newAutoscrollFixture(t *testing.T, height float32, build func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement) *scrollFixture {
	t.Helper()
	f := newScrollFixture(t, Size{Width: 100, Height: height}, build)
	t.Cleanup(func() { f.app.Shutdown() })
	return f
}

// TestListAutoscrollAboveItemTopRendersItemsAbove ports R:list.rs:
// 1761-1811.
func TestListAutoscrollAboveItemTopRendersItemsAbove(t *testing.T) {
	state := NewListState(5, 10)
	state.ScrollTo(ListOffset{ItemIx: 2})
	newAutoscrollFixture(t, 60, func(*scrollView, *Window, *Context[scrollView]) AnyElement {
		// Request an autoscroll whose top sits 30px above item 2's own
		// top, mimicking a scroll-margin overshoot.
		return List(state, autoscrollRows(2, func(b Bounds) Bounds {
			return Bounds{Origin: Point{X: b.Left(), Y: b.Top() - 30}, Size: Size{Width: b.Size.Width, Height: 35}}
		})).SizeFull().IntoElement()
	})
	// 30px above item 2's top, with 20px items, lands 10px into item 0.
	top := state.LogicalScrollTop()
	if top.OffsetInItem < 0 {
		t.Fatalf("offset_in_item must never be negative (would leave blank space above), got %v", top.OffsetInItem)
	}
	if top != (ListOffset{ItemIx: 0, OffsetInItem: 10}) {
		t.Fatalf("logical scroll top = %+v, want {0 10}", top)
	}
}

// TestListAutoscrollBelowRevealsTheBottom: a request below the list puts
// its bottom on the list's bottom (R:list.rs:1327-1355), and the
// discarded first prepaint leaves no registrations behind
// (R:window.rs:3970-3987): the frame has exactly the hitboxes and
// dispatch nodes of a frame drawn at the new scroll top directly.
func TestListAutoscrollBelowRevealsTheBottom(t *testing.T) {
	state := NewListState(8, 0)
	state.ScrollTo(ListOffset{})
	f := newAutoscrollFixture(t, 60, func(*scrollView, *Window, *Context[scrollView]) AnyElement {
		// Item 2 (40..60 at scroll 0) asks for 50px from its top, down to
		// 90.
		return List(state, autoscrollRows(2, func(b Bounds) Bounds {
			return Bounds{Origin: b.Origin, Size: Size{Width: b.Size.Width, Height: 50}}
		})).SizeFull().IntoElement()
	})
	// The requested bottom on the list's bottom: 10px above item 2's top
	// stay in view, so the scroll top is 10px into item 1.
	if top := state.LogicalScrollTop(); top != (ListOffset{ItemIx: 1, OffsetInItem: 10}) {
		t.Fatalf("logical scroll top = %+v, want {1 10}", top)
	}
	b, ok := WindowDebugBounds(f.window)["row-3"]
	if !ok || b.Top() != 30 {
		t.Fatalf("row-3 bounds %v (%v), want top 30 (the request then ends at 60)", b, ok)
	}
	rendered := focusState(f.window).rendered
	hitboxes, nodes := len(rendered.hitboxes), rendered.dispatchTree.Len()

	// The same rows at the same scroll top, drawn without a request.
	direct := NewListState(8, 0)
	direct.ScrollTo(ListOffset{ItemIx: 1, OffsetInItem: 10})
	g := newAutoscrollFixture(t, 60, func(*scrollView, *Window, *Context[scrollView]) AnyElement {
		return List(direct, autoscrollRows(2, func(b Bounds) Bounds { return b })).SizeFull().IntoElement()
	})
	if top := direct.LogicalScrollTop(); top != (ListOffset{ItemIx: 1, OffsetInItem: 10}) {
		t.Fatalf("direct scroll top = %+v", top)
	}
	want := focusState(g.window).rendered
	if hitboxes != len(want.hitboxes) || nodes != want.dispatchTree.Len() {
		t.Fatalf("hitboxes %d, dispatch nodes %d; a direct frame has %d, %d", hitboxes, nodes, len(want.hitboxes), want.dispatchTree.Len())
	}
}

// TestListAutoscrollInViewDoesNotScroll: a request inside the list's
// bounds leaves the scroll top alone (neither arm of R:list.rs:1293-1356
// applies).
func TestListAutoscrollInViewDoesNotScroll(t *testing.T) {
	state := NewListState(8, 0)
	state.ScrollTo(ListOffset{ItemIx: 1, OffsetInItem: 5})
	newAutoscrollFixture(t, 60, func(*scrollView, *Window, *Context[scrollView]) AnyElement {
		return List(state, autoscrollRows(2, func(b Bounds) Bounds { return b })).SizeFull().IntoElement()
	})
	if top := state.LogicalScrollTop(); top != (ListOffset{ItemIx: 1, OffsetInItem: 5}) {
		t.Fatalf("logical scroll top = %+v, want {1 5}", top)
	}
}

// TestListAutoscrollIgnoresRequestsFromOutside: a request made before the
// list prepaints its items is not its children's and is dropped
// (R:list.rs:1278-1279).
func TestListAutoscrollIgnoresRequestsFromOutside(t *testing.T) {
	state := NewListState(8, 0)
	state.ScrollTo(ListOffset{})
	newAutoscrollFixture(t, 60, func(*scrollView, *Window, *Context[scrollView]) AnyElement {
		before := Canvas(func(_ Bounds, w *Window, _ *App) struct{} {
			w.RequestAutoscroll(Bounds{Origin: Point{Y: 500}, Size: Size{Width: 10, Height: 10}})
			return struct{}{}
		}, func(Bounds, struct{}, *Window, *App) {}).H(Px(0)).WFull().IntoElement()
		return Div().SizeFull().Child(before).Child(List(state, listRows(20)).SizeFull()).IntoElement()
	})
	if top := state.LogicalScrollTop(); top != (ListOffset{}) {
		t.Fatalf("logical scroll top = %+v, want {0 0}", top)
	}
}
