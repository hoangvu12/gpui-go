package base

import (
	"fmt"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ported tests of virtual_list.rs (7/7: 4 pure, 3 on the test window).

// vlRange is a recorded Range<usize>.
type vlRange struct{ start, end int }

func (r vlRange) contains(ix int) bool { return r.start <= ix && ix < r.end }

// vlHarness is U:virtual_list.rs:773-800.
type vlHarness struct {
	axis          gpui.Axis
	itemSizes     []gpui.Size
	scrollHandle  VirtualListScrollHandle
	visibleRanges *[]vlRange
}

func (v *vlHarness) Render(_ *gpui.Window, cx *gpui.Context[vlHarness]) gpui.AnyElement {
	itemSizes := v.itemSizes
	visibleRanges := v.visibleRanges
	return NewVirtualList(cx.Entity(), "virtual-list-test", v.axis, v.itemSizes,
		func(_ *vlHarness, start, end int, _ *gpui.Window, _ *gpui.Context[vlHarness]) []*gpui.DivElement {
			*visibleRanges = append(*visibleRanges, vlRange{start, end})
			items := make([]*gpui.DivElement, 0, end-start)
			for ix := start; ix < end; ix++ {
				items = append(items, gpui.Div().Flex().W(gpui.Px(itemSizes[ix].Width)).H(gpui.Px(itemSizes[ix].Height)).DebugSelector(fmt.Sprintf("vl-item-%d", ix)))
			}
			return items
		}).
		TrackScroll(v.scrollHandle).
		W(gpui.Px(60)).
		H(gpui.Px(60)).
		IntoElement()
}

// vlExerciseAxis is U:virtual_list.rs:802-835.
func vlExerciseAxis(t *testing.T, axis gpui.Axis) {
	t.Helper()
	itemSize := gpui.Size{Width: 10, Height: 20}
	if axis == gpui.AxisHorizontal {
		itemSize = gpui.Size{Width: 20, Height: 10}
	}
	itemSizes := make([]gpui.Size, 20)
	for i := range itemSizes {
		itemSizes[i] = itemSize
	}
	scrollHandle := NewVirtualListScrollHandle()
	var visibleRanges []vlRange
	ta := gpui.NewTestApp()
	_, cx := ta.AddWindowView(func(v *vlHarness, _ *gpui.Window, _ *gpui.Context[vlHarness]) {
		*v = vlHarness{axis: axis, itemSizes: itemSizes, scrollHandle: scrollHandle, visibleRanges: &visibleRanges}
	})

	sbDraw(t, cx)
	initial := visibleRanges[len(visibleRanges)-1]
	if initial.start != 0 || !(initial.end < len(itemSizes)) {
		t.Fatalf("initial range = %v", initial)
	}

	scrollHandle.ScrollToItem(12, gpui.ScrollTop)
	sbDraw(t, cx)
	scrolled := visibleRanges[len(visibleRanges)-1]
	if !scrolled.contains(12) {
		t.Fatalf("scrolled range = %v", scrolled)
	}
	off := scrollHandle.Offset()
	if axis == gpui.AxisHorizontal && !(off.X < 0) || axis == gpui.AxisVertical && !(off.Y < 0) {
		t.Fatalf("offset = %v", off)
	}
}

// vlLayout is U:virtual_list.rs:837-848.
func vlLayout(sizes ...float32) ([]float32, []float32) {
	origins := make([]float32, len(sizes))
	cumulative := float32(0)
	for i, size := range sizes {
		origins[i] = cumulative
		cumulative += size
	}
	return origins, sizes
}

func vlRepeat(size float32, n int) []float32 {
	out := make([]float32, n)
	for i := range out {
		out[i] = size
	}
	return out
}

func vlCheckRange(t *testing.T, origins, sizes []float32, start, end float32, want vlRange) {
	t.Helper()
	first, last := visibleRange(origins, sizes, start, end)
	if (vlRange{first, last}) != want {
		t.Fatalf("visible_range(%v..%v) = %d..%d, want %d..%d", start, end, first, last, want.start, want.end)
	}
}

// U:virtual_list.rs:850-855
func TestVirtualListVisibleRangeStartsAtTheFirstItemCrossingTheViewport(t *testing.T) {
	origins, sizes := vlLayout(vlRepeat(20, 10)...)
	vlCheckRange(t, origins, sizes, 0, 60, vlRange{0, 5})
	vlCheckRange(t, origins, sizes, 50, 110, vlRange{2, 7})
}

// U:virtual_list.rs:857-862
func TestVirtualListVisibleRangeOverdrawsOneItemPastTheViewportButNotPastTheEnd(t *testing.T) {
	origins, sizes := vlLayout(vlRepeat(20, 10)...)
	vlCheckRange(t, origins, sizes, 150, 210, vlRange{7, 10})
	vlCheckRange(t, origins, sizes, 0, 500, vlRange{0, 10})
}

// U:virtual_list.rs:864-869
func TestVirtualListVisibleRangeHandlesUnevenSizes(t *testing.T) {
	origins, sizes := vlLayout(10, 30, 5, 50)
	vlCheckRange(t, origins, sizes, 12, 44, vlRange{1, 4})
	vlCheckRange(t, origins, sizes, 45, 50, vlRange{3, 4})
}

// U:virtual_list.rs:871-877
func TestVirtualListVisibleRangeIsEmptyWithoutItemsOrPastTheContent(t *testing.T) {
	origins, sizes := vlLayout()
	vlCheckRange(t, origins, sizes, 0, 60, vlRange{0, 0})
	origins, sizes = vlLayout(vlRepeat(20, 10)...)
	if first, last := visibleRange(origins, sizes, 300, 360); first < last {
		t.Fatalf("range past the content = %d..%d", first, last)
	}
}

// U:virtual_list.rs:879-882
func TestVirtualListVerticalVisibleRangeAndDeferredScrollArePreserved(t *testing.T) {
	vlExerciseAxis(t, gpui.AxisVertical)
}

// U:virtual_list.rs:884-887
func TestVirtualListHorizontalVisibleRangeAndDeferredScrollArePreserved(t *testing.T) {
	vlExerciseAxis(t, gpui.AxisHorizontal)
}

// U:virtual_list.rs:889-904
func TestVirtualListEmptyListDrawsWithoutRequestingItems(t *testing.T) {
	var visibleRanges []vlRange
	ta := gpui.NewTestApp()
	_, cx := ta.AddWindowView(func(v *vlHarness, _ *gpui.Window, _ *gpui.Context[vlHarness]) {
		*v = vlHarness{axis: gpui.AxisVertical, scrollHandle: NewVirtualListScrollHandle(), visibleRanges: &visibleRanges}
	})
	sbDraw(t, cx)
	if len(visibleRanges) != 0 {
		t.Fatalf("ranges = %v", visibleRanges)
	}
}

// Go-own: the inner div tracks the handle (bounds, max offset) and the
// wheel scrolls the list, with the items repainted at the scrolled
// origins.
func TestVirtualListWheelScrollsAndReportsHandleMetrics(t *testing.T) {
	itemSizes := make([]gpui.Size, 20)
	for i := range itemSizes {
		itemSizes[i] = gpui.Size{Width: 10, Height: 20}
	}
	scrollHandle := NewVirtualListScrollHandle()
	var visibleRanges []vlRange
	ta := gpui.NewTestApp()
	_, cx := ta.AddWindowView(func(v *vlHarness, _ *gpui.Window, _ *gpui.Context[vlHarness]) {
		*v = vlHarness{axis: gpui.AxisVertical, itemSizes: itemSizes, scrollHandle: scrollHandle, visibleRanges: &visibleRanges}
	})
	sbDraw(t, cx)
	if b := scrollHandle.ViewportBounds(); b.Size != (gpui.Size{Width: 60, Height: 60}) {
		t.Fatalf("viewport = %v", b)
	}
	if c := scrollHandle.ContentSize(); c.Height != 400 {
		t.Fatalf("content size = %v", c)
	}
	cx.SimulateEvent(&gpui.ScrollWheelEvent{
		Position:   gpui.Point{X: 30, Y: 30},
		Delta:      gpui.ScrollDeltaPixels(0, -100),
		TouchPhase: gpui.TouchPhaseMoved,
	})
	sbDraw(t, cx)
	if off := scrollHandle.Offset(); off.Y != -100 {
		t.Fatalf("offset = %v", off)
	}
	if r := visibleRanges[len(visibleRanges)-1]; r.start != 5 {
		t.Fatalf("range after the wheel = %v", r)
	}
	// Item 5 sits at the viewport top: origin + 5*20 - 100.
	if b, ok := cx.DebugBounds("vl-item-5"); !ok || b.Origin.Y != 0 || b.Size.Height != 20 {
		t.Fatalf("item 5 bounds = %v %v", b, ok)
	}
}
