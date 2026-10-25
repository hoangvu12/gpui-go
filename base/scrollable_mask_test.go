package base

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ported tests of scrollable_mask.rs (16/16, all on the test window).
//
// Deviations: a mask's parent is given Block display where upstream's
// plain div() relies on the gpui-ce 0.2.2 default (map gap 2b; see
// scrollable_mask.go), and so is the nested scroller whose children stack
// as blocks. Phaseless packets pass TouchPhaseMoved (Rust's Default; the
// Go zero value is Started). gpui.ListState has no alignment parameter.

// maskWheel is a pixel wheel packet with the pinned default phase.
func maskWheel(x, y, dx, dy float32) *gpui.ScrollWheelEvent {
	return &gpui.ScrollWheelEvent{
		Position:   gpui.Point{X: x, Y: y},
		Delta:      gpui.ScrollDeltaPixels(dx, dy),
		TouchPhase: gpui.TouchPhaseMoved,
	}
}

// maskSettle is `cx.run_until_parked(); cx.update(|w, cx| w.draw(cx))`.
func maskSettle(t *testing.T, cx *gpui.VisualTestContext) {
	t.Helper()
	cx.RunUntilParked()
	sbDraw(t, cx)
}

// maskAtTop reports `(item_ix, offset_in_item) == (0, 0)`.
func maskAtTop(state *gpui.ListState) bool {
	top := state.LogicalScrollTop()
	return top.ItemIx == 0 && top.OffsetInItem == 0
}

// horizontalAreaTest is U:scrollable_mask.rs:307-320.
type horizontalAreaTest struct{ scrollHandle gpui.ScrollHandle }

func (v *horizontalAreaTest) Render(*gpui.Window, *gpui.Context[horizontalAreaTest]) gpui.AnyElement {
	return gpui.Div().Flex().W(gpui.Px(100)).H(gpui.Px(40)).Child(horizontalScrollArea(
		"horizontal-scroll-area",
		v.scrollHandle,
		&gpui.StyleRefinement{},
		gpui.Div().Flex().W(gpui.Px(300)).H(gpui.Px(40)),
	)).IntoElement()
}

// maskOpenHorizontalArea is U:scrollable_mask.rs:631-647.
func maskOpenHorizontalArea(t *testing.T, scrollHandle gpui.ScrollHandle) *gpui.VisualTestContext {
	t.Helper()
	ta := gpui.NewTestApp()
	_, cx := ta.AddWindowView(func(v *horizontalAreaTest, _ *gpui.Window, _ *gpui.Context[horizontalAreaTest]) {
		*v = horizontalAreaTest{scrollHandle: scrollHandle}
	})
	maskSettle(t, cx)
	return cx
}

// U:scrollable_mask.rs:322-344
func TestScrollableMaskHorizontalScrollAreaIgnoresVerticalWheel(t *testing.T) {
	scrollHandle := gpui.NewScrollHandle()
	cx := maskOpenHorizontalArea(t, scrollHandle)
	cx.SimulateEvent(maskWheel(10, 10, 0, -40))
	if x := scrollHandle.Offset().X; x != 0 {
		t.Fatalf("offset x = %v", x)
	}
}

// listWithHorizontalAreaTest is U:scrollable_mask.rs:346-382.
type listWithHorizontalAreaTest struct {
	scrollHandle gpui.ScrollHandle
	listState    *gpui.ListState
	occluded     bool
}

func (v *listWithHorizontalAreaTest) Render(*gpui.Window, *gpui.Context[listWithHorizontalAreaTest]) gpui.AnyElement {
	scrollHandle := v.scrollHandle
	root := gpui.Div().Flex().W(gpui.Px(100)).H(gpui.Px(100)).Child(
		gpui.List(v.listState, func(ix int, _ *gpui.Window, _ *gpui.App) gpui.AnyElement {
			if ix == 0 {
				return horizontalScrollArea("horizontal-scroll-area", scrollHandle, &gpui.StyleRefinement{},
					gpui.Div().Flex().W(gpui.Px(300)).H(gpui.Px(40))).IntoElement()
			}
			return gpui.Div().Flex().W(gpui.Px(100)).H(gpui.Px(40)).IntoElement()
		}).SizeFull(),
	)
	if v.occluded {
		// An overlay above the list, like an open dialog or menu.
		root = root.Child(gpui.Div().Flex().Absolute().Top0().Left0().SizeFull().Occlude())
	}
	return root.IntoElement()
}

// maskSetupListTest is U:scrollable_mask.rs:384-404.
func maskSetupListTest(t *testing.T, scrollHandle gpui.ScrollHandle, listState *gpui.ListState, occluded bool) *gpui.VisualTestContext {
	t.Helper()
	ta := gpui.NewTestApp()
	_, cx := ta.AddWindowView(func(v *listWithHorizontalAreaTest, _ *gpui.Window, _ *gpui.Context[listWithHorizontalAreaTest]) {
		*v = listWithHorizontalAreaTest{scrollHandle: scrollHandle, listState: listState, occluded: occluded}
	})
	maskSettle(t, cx)
	return cx
}

// U:scrollable_mask.rs:406-425
func TestScrollableMaskHorizontalScrollAreaInListKeepsHorizontalDominantWheel(t *testing.T) {
	scrollHandle := gpui.NewScrollHandle()
	listState := gpui.NewListState(10, 0)
	cx := maskSetupListTest(t, scrollHandle, listState, false)

	// A trackpad swipe is rarely axis-pure.
	cx.SimulateEvent(maskWheel(10, 10, -40, -10))

	if x := scrollHandle.Offset().X; x != -40 {
		t.Fatalf("area offset x = %v, want -40", x)
	}
	if !maskAtTop(listState) {
		t.Fatalf("the outer list scrolled: %+v", listState.LogicalScrollTop())
	}
}

// U:scrollable_mask.rs:427-443
func TestScrollableMaskHorizontalScrollAreaInListBubblesVerticalDominantWheel(t *testing.T) {
	scrollHandle := gpui.NewScrollHandle()
	listState := gpui.NewListState(10, 0)
	cx := maskSetupListTest(t, scrollHandle, listState, false)

	cx.SimulateEvent(maskWheel(10, 10, -10, -40))

	if x := scrollHandle.Offset().X; x != 0 {
		t.Fatalf("area offset x = %v", x)
	}
	if maskAtTop(listState) {
		t.Fatal("the list did not scroll")
	}
}

// listWithVerticalMaskTest is U:scrollable_mask.rs:445-478.
type listWithVerticalMaskTest struct {
	outerHandle gpui.ScrollHandle
	listState   *gpui.ListState
}

func (v *listWithVerticalMaskTest) Render(*gpui.Window, *gpui.Context[listWithVerticalMaskTest]) gpui.AnyElement {
	return gpui.Div().Flex().
		ID("outer").
		W(gpui.Px(100)).
		H(gpui.Px(100)).
		OverflowYScroll().
		TrackScroll(v.outerHandle).
		Child(gpui.Div().Flex().WFull().H(gpui.Px(400)).Child(
			gpui.Div().
				Block(). // gap 2b: the mask's static position.
				Relative().
				WFull().
				H(gpui.Px(80)).
				OverflowHidden().
				Child(gpui.List(v.listState, func(int, *gpui.Window, *gpui.App) gpui.AnyElement {
					return gpui.Div().Flex().WFull().H(gpui.Px(40)).IntoElement()
				}).SizeFull()).
				Child(NewScrollableMask(gpui.AxisVertical, v.listState)))).
		IntoElement()
}

// maskSetupVerticalMaskTest is U:scrollable_mask.rs:480-498.
func maskSetupVerticalMaskTest(t *testing.T, outerHandle gpui.ScrollHandle, listState *gpui.ListState) *gpui.VisualTestContext {
	t.Helper()
	ta := gpui.NewTestApp()
	_, cx := ta.AddWindowView(func(v *listWithVerticalMaskTest, _ *gpui.Window, _ *gpui.Context[listWithVerticalMaskTest]) {
		*v = listWithVerticalMaskTest{outerHandle: outerHandle, listState: listState}
	})
	maskSettle(t, cx)
	return cx
}

// U:scrollable_mask.rs:500-517
func TestScrollableMaskVerticalMaskContainsWheelInsideTheList(t *testing.T) {
	outerHandle := gpui.NewScrollHandle()
	// A large overdraw measures every row up front, so the mask sees the
	// full content height.
	listState := gpui.NewListState(10, 1000)
	cx := maskSetupVerticalMaskTest(t, outerHandle, listState)

	cx.SimulateEvent(maskWheel(10, 10, 0, -40))

	if y := listState.ScrollPxOffsetForScrollbar().Y; y != -40 {
		t.Fatalf("list offset = %v, want -40", y)
	}
	if y := outerHandle.Offset().Y; y != 0 {
		t.Fatalf("outer offset = %v", y)
	}
}

// U:scrollable_mask.rs:519-539
func TestScrollableMaskVerticalMaskChainsToTheAncestorAtTheEdge(t *testing.T) {
	outerHandle := gpui.NewScrollHandle()
	listState := gpui.NewListState(10, 1000)
	// Start the inner list at its bottom edge (400 - 80 = 320).
	listState.ScrollTo(gpui.ListOffset{ItemIx: 8, OffsetInItem: 0})
	cx := maskSetupVerticalMaskTest(t, outerHandle, listState)

	cx.SimulateEvent(maskWheel(10, 10, 0, -40))

	if y := listState.ScrollPxOffsetForScrollbar().Y; y != -320 {
		t.Fatalf("list offset = %v, want -320", y)
	}
	if y := outerHandle.Offset().Y; y != -40 {
		t.Fatalf("outer offset = %v, want -40", y)
	}
}

// U:scrollable_mask.rs:541-564
func TestScrollableMaskHorizontalScrollAreaCoversViewportAfterScrolled(t *testing.T) {
	scrollHandle := gpui.NewScrollHandle()
	listState := gpui.NewListState(10, 0)
	cx := maskSetupListTest(t, scrollHandle, listState, false)

	scrollHandle.SetOffset(gpui.Point{X: -150})
	sbDraw(t, cx)

	// The mask must still cover the right side of the viewport.
	cx.SimulateEvent(maskWheel(90, 10, -40, -10))

	if x := scrollHandle.Offset().X; x != -190 {
		t.Fatalf("offset x = %v, want -190", x)
	}
	if !maskAtTop(listState) {
		t.Fatalf("the list scrolled: %+v", listState.LogicalScrollTop())
	}
}

// U:scrollable_mask.rs:566-588
func TestScrollableMaskHorizontalScrollAreaTrapsWheelAtEdge(t *testing.T) {
	scrollHandle := gpui.NewScrollHandle()
	listState := gpui.NewListState(10, 0)
	cx := maskSetupListTest(t, scrollHandle, listState, false)

	// Scroll the area to its right edge (300 - 100 = 200).
	scrollHandle.SetOffset(gpui.Point{X: -200})
	sbDraw(t, cx)

	cx.SimulateEvent(maskWheel(10, 10, -40, -10))

	// A horizontal mask consumes even at the edge.
	if !maskAtTop(listState) {
		t.Fatalf("the list scrolled: %+v", listState.LogicalScrollTop())
	}
}

// U:scrollable_mask.rs:590-605
func TestScrollableMaskHorizontalScrollAreaIgnoresWheelWhenOccluded(t *testing.T) {
	scrollHandle := gpui.NewScrollHandle()
	listState := gpui.NewListState(10, 0)
	cx := maskSetupListTest(t, scrollHandle, listState, true)

	cx.SimulateEvent(maskWheel(10, 10, -40, -10))

	if x := scrollHandle.Offset().X; x != 0 {
		t.Fatalf("the occluded area scrolled: %v", x)
	}
}

// U:scrollable_mask.rs:607-629
func TestScrollableMaskHorizontalScrollAreaUsesHorizontalWheel(t *testing.T) {
	scrollHandle := gpui.NewScrollHandle()
	cx := maskOpenHorizontalArea(t, scrollHandle)
	cx.SimulateEvent(maskWheel(10, 10, -40, 0))
	if x := scrollHandle.Offset().X; x != -40 {
		t.Fatalf("offset x = %v, want -40", x)
	}
}

// U:scrollable_mask.rs:649-670
func TestScrollableMaskHorizontalMaskKeepsAxisLockWithinAGesture(t *testing.T) {
	scrollHandle := gpui.NewScrollHandle()
	cx := maskOpenHorizontalArea(t, scrollHandle)

	cx.SimulateEvent(maskWheel(10, 10, -40, -10))
	if x := scrollHandle.Offset().X; x != -40 {
		t.Fatalf("offset x = %v, want -40", x)
	}

	// Same gesture, leaning vertical but under the unlock ratio: the lock
	// holds and the horizontal offset keeps moving.
	cx.SimulateEvent(maskWheel(10, 10, -10, -15))
	if x := scrollHandle.Offset().X; x != -50 {
		t.Fatalf("offset x = %v, want -50", x)
	}
}

// U:scrollable_mask.rs:672-692
func TestScrollableMaskHorizontalMaskReleasesAxisLockOnAStrongTurn(t *testing.T) {
	scrollHandle := gpui.NewScrollHandle()
	cx := maskOpenHorizontalArea(t, scrollHandle)

	cx.SimulateEvent(maskWheel(10, 10, -40, -10))
	if x := scrollHandle.Offset().X; x != -40 {
		t.Fatalf("offset x = %v, want -40", x)
	}

	// Past the unlock ratio the gesture is no longer horizontal.
	cx.SimulateEvent(maskWheel(10, 10, -10, -25))
	if x := scrollHandle.Offset().X; x != -40 {
		t.Fatalf("offset x = %v, want -40", x)
	}
}

// nestedVerticalScrollTest is U:scrollable_mask.rs:694-727.
type nestedVerticalScrollTest struct {
	outerHandle        gpui.ScrollHandle
	innerHandle        gpui.ScrollHandle
	innerContentHeight float32
}

func (v *nestedVerticalScrollTest) Render(*gpui.Window, *gpui.Context[nestedVerticalScrollTest]) gpui.AnyElement {
	return gpui.Div().
		ID("outer").
		Block(). // gap 2b: the two children stack as blocks.
		W(gpui.Px(100)).
		H(gpui.Px(100)).
		OverflowYScroll().
		TrackScroll(v.outerHandle).
		Child(gpui.Div().
			Block(). // gap 2b: the mask's static position.
			Relative().
			WFull().
			H(gpui.Px(60)).
			Child(gpui.Div().Flex().
				ID("inner").
				SizeFull().
				OverflowYScroll().
				TrackScroll(v.innerHandle).
				Child(gpui.Div().Flex().WFull().H(gpui.Px(v.innerContentHeight)))).
			Child(NewScrollableMask(gpui.AxisVertical, v.innerHandle))).
		Child(gpui.Div().Flex().WFull().H(gpui.Px(400))).
		IntoElement()
}

// maskSetupNestedVerticalTest is U:scrollable_mask.rs:729-749.
func maskSetupNestedVerticalTest(t *testing.T, outerHandle, innerHandle gpui.ScrollHandle, innerContentHeight float32) *gpui.VisualTestContext {
	t.Helper()
	ta := gpui.NewTestApp()
	_, cx := ta.AddWindowView(func(v *nestedVerticalScrollTest, _ *gpui.Window, _ *gpui.Context[nestedVerticalScrollTest]) {
		*v = nestedVerticalScrollTest{outerHandle: outerHandle, innerHandle: innerHandle, innerContentHeight: innerContentHeight}
	})
	maskSettle(t, cx)
	return cx
}

// U:scrollable_mask.rs:751-766
func TestScrollableMaskVerticalMaskConsumesWheelWhenScrollable(t *testing.T) {
	outerHandle, innerHandle := gpui.NewScrollHandle(), gpui.NewScrollHandle()
	cx := maskSetupNestedVerticalTest(t, outerHandle, innerHandle, 300)

	cx.SimulateEvent(maskWheel(10, 10, 0, -40))

	if y := innerHandle.Offset().Y; y != -40 {
		t.Fatalf("inner offset = %v, want -40", y)
	}
	if y := outerHandle.Offset().Y; y != 0 {
		t.Fatalf("outer offset = %v", y)
	}
}

// U:scrollable_mask.rs:768-793
func TestScrollableMaskVerticalMaskHandsOffToParentAtEdge(t *testing.T) {
	outerHandle, innerHandle := gpui.NewScrollHandle(), gpui.NewScrollHandle()
	cx := maskSetupNestedVerticalTest(t, outerHandle, innerHandle, 300)

	// Scroll the inner element to its bottom edge (300 - 60 = 240).
	innerHandle.SetOffset(gpui.Point{Y: -240})
	sbDraw(t, cx)

	cx.SimulateEvent(maskWheel(10, 10, 0, -40))

	if y := outerHandle.Offset().Y; y != -40 {
		t.Fatalf("outer offset = %v, want -40", y)
	}
	// The inner offset is clamped back to the edge on the next prepaint.
	sbDraw(t, cx)
	if y := innerHandle.Offset().Y; y != -240 {
		t.Fatalf("inner offset = %v, want -240", y)
	}
}

// U:scrollable_mask.rs:795-813
func TestScrollableMaskVerticalMaskBubblesWhenNoOverflow(t *testing.T) {
	outerHandle, innerHandle := gpui.NewScrollHandle(), gpui.NewScrollHandle()
	// The inner content (40) fits its 60px viewport.
	cx := maskSetupNestedVerticalTest(t, outerHandle, innerHandle, 40)

	cx.SimulateEvent(maskWheel(10, 10, 0, -40))

	if y := outerHandle.Offset().Y; y != -40 {
		t.Fatalf("outer offset = %v, want -40", y)
	}
	sbDraw(t, cx)
	if y := innerHandle.Offset().Y; y != 0 {
		t.Fatalf("inner offset = %v", y)
	}
}

// U:scrollable_mask.rs:815-842
func TestScrollableMaskVerticalMaskIgnoresTransientOverscroll(t *testing.T) {
	outerHandle, innerHandle := gpui.NewScrollHandle(), gpui.NewScrollHandle()
	cx := maskSetupNestedVerticalTest(t, outerHandle, innerHandle, 300)

	innerHandle.SetOffset(gpui.Point{Y: -240})
	sbDraw(t, cx)

	// Two wheel events at the edge with no redraw in between: the first
	// leaves the inner offset past the edge unclamped, which must not
	// read as room to scroll and swallow the second.
	for i := 0; i < 2; i++ {
		cx.SimulateEvent(maskWheel(10, 10, 0, -40))
	}

	if y := outerHandle.Offset().Y; y != -80 {
		t.Fatalf("outer offset = %v, want -80", y)
	}
	sbDraw(t, cx)
	if y := innerHandle.Offset().Y; y != -240 {
		t.Fatalf("inner offset = %v, want -240", y)
	}
}

// listWithVerticalAreaTest is U:scrollable_mask.rs:844-880.
type listWithVerticalAreaTest struct {
	scrollHandle gpui.ScrollHandle
	listState    *gpui.ListState
}

func (v *listWithVerticalAreaTest) Render(*gpui.Window, *gpui.Context[listWithVerticalAreaTest]) gpui.AnyElement {
	scrollHandle := v.scrollHandle
	return gpui.Div().Flex().W(gpui.Px(100)).H(gpui.Px(100)).Child(
		gpui.List(v.listState, func(ix int, _ *gpui.Window, _ *gpui.App) gpui.AnyElement {
			if ix == 0 {
				return gpui.Div().
					Block(). // gap 2b: the mask's static position.
					Relative().
					WFull().
					H(gpui.Px(60)).
					Child(gpui.Div().Flex().
						ID("inner").
						SizeFull().
						OverflowYScroll().
						TrackScroll(scrollHandle).
						Child(gpui.Div().Flex().WFull().H(gpui.Px(300)))).
					Child(NewScrollableMask(gpui.AxisVertical, scrollHandle)).
					IntoElement()
			}
			return gpui.Div().Flex().W(gpui.Px(100)).H(gpui.Px(40)).IntoElement()
		}).SizeFull(),
	).IntoElement()
}

// U:scrollable_mask.rs:882-909
func TestScrollableMaskVerticalMaskInListConsumesWheelWhenScrollable(t *testing.T) {
	scrollHandle := gpui.NewScrollHandle()
	listState := gpui.NewListState(10, 0)
	ta := gpui.NewTestApp()
	_, cx := ta.AddWindowView(func(v *listWithVerticalAreaTest, _ *gpui.Window, _ *gpui.Context[listWithVerticalAreaTest]) {
		*v = listWithVerticalAreaTest{scrollHandle: scrollHandle, listState: listState}
	})
	maskSettle(t, cx)

	cx.SimulateEvent(maskWheel(10, 10, 0, -40))

	if y := scrollHandle.Offset().Y; y != -40 {
		t.Fatalf("inner offset = %v, want -40", y)
	}
	if !maskAtTop(listState) {
		t.Fatalf("the list scrolled: %+v", listState.LogicalScrollTop())
	}
}
