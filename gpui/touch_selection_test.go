package gpui

import "testing"

// Ports of the U:touch_selection.rs tests (239-297): 3 tests, 3 ported —
// moved from base with the model (browser-core 19).

// U:touch_selection.rs:243-266
func TestTouchSnapshotReportsBoundsAndEdges(t *testing.T) {
	start := caretLineBox(Point{X: 10, Y: 20}, 16)
	end := caretLineBox(Point{X: 80, Y: 52}, 16)
	snapshot := newTouchSelectionSnapshot(start, end).withMenuOpen(true).withDragging(Some(SelectionEdgeEnd))
	if snapshot.Edge(SelectionEdgeStart) != start || snapshot.Edge(SelectionEdgeEnd) != end {
		t.Fatal("edges")
	}
	if snapshot.IsEmpty() || !snapshot.IsMenuOpen() || snapshot.Dragging() != Some(SelectionEdgeEnd) {
		t.Fatalf("snapshot %+v", snapshot)
	}
	bounds, ok := snapshot.Bounds()
	if want := boundsFromCorners(Point{X: 10, Y: 20}, Point{X: 80, Y: 68}); !ok || bounds != want {
		t.Fatalf("bounds %v, want %v", bounds, want)
	}
	if !newTouchSelectionSnapshot(start, start).IsEmpty() {
		t.Fatal("a caret is not empty")
	}
}

// U:touch_selection.rs:268-284
func TestAScrolledAwayEndKeepsItsGeometryButNoHandle(t *testing.T) {
	start := caretLineBox(Point{X: 10, Y: -30}, 16)
	end := caretLineBox(Point{X: 80, Y: 52}, 16)
	viewport := Bounds{Size: Size{Width: 200, Height: 100}}
	if caretInView(start, viewport) || !caretInView(end, viewport) {
		t.Fatal("caret visibility")
	}
	snapshot := newTouchSelectionSnapshot(start, end).withEdgeVisible(SelectionEdgeStart, false)
	if snapshot.IsEdgeVisible(SelectionEdgeStart) || snapshot.Edge(SelectionEdgeStart) != start {
		t.Fatal("hidden start")
	}
	if bounds, ok := snapshot.Bounds(); !ok || bounds != end {
		t.Fatalf("bounds %v", bounds)
	}
	if _, ok := snapshot.withEdgeVisible(SelectionEdgeEnd, false).Bounds(); ok {
		t.Fatal("a hidden selection has bounds")
	}
}

// U:touch_selection.rs:286-296
func TestEdgeDragKeepsTheFingerOffset(t *testing.T) {
	caret := caretLineBox(Point{X: 100, Y: 40}, 20)
	drag := newEdgeDrag(SelectionEdgeEnd, caret, Point{X: 102, Y: 72})
	if drag.edge != SelectionEdgeEnd {
		t.Fatal("edge")
	}
	// The finger started 22px below the caret's center; it stays there.
	if got := drag.textPosition(Point{X: 150, Y: 90}); got != (Point{X: 148, Y: 68}) {
		t.Fatalf("text position %v", got)
	}
}
