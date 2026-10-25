package base

import (
	"slices"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

// hover_card.rs tests (3/3 ported, U:hover_card.rs:269-406).

const hoverCardTestDelay = 100 * time.Millisecond

// hoverCardHarness is U:hover_card.rs:277-307 Harness.
func hoverCardHarness(changes *[]bool, tapToOpen bool) func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
	return func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		card := NewHoverCard("hover-card")
		card.tapToOpen = tapToOpen
		return gpui.OwnRecipe(card.
			OpenDelay(hoverCardTestDelay).
			CloseDelay(hoverCardTestDelay).
			OnOpenChange(func(open bool, _ *gpui.Window, _ *gpui.App) { *changes = append(*changes, open) }).
			Trigger(gpui.Div().Flex().DebugSelector("hover-card-trigger").Size(gpui.Px(20))).
			Content(func(*HoverCardState, *gpui.Window, *gpui.Context[HoverCardState]) *gpui.DivElement {
				return gpui.Div().Flex().ID("hover-card-content").DebugSelector("hover-card-content").Size(gpui.Px(10))
			}))
	}
}

// U:hover_card.rs:309-329
func TestHoverCardOwnsDelayedOpenAndClose(t *testing.T) {
	ta := gpui.NewTestApp()
	var changes []bool
	_, cx := openTestView(t, ta, hoverCardHarness(&changes, false))
	ovDraw(t, cx)

	cx.SimulateMouseMove(ovPoint(10, 10), nil, gpui.NoModifiers())
	cx.AdvanceClock(hoverCardTestDelay.Milliseconds())
	cx.RunUntilParked()
	ovDraw(t, cx)
	ovDraw(t, cx)
	if !ovHasBounds(cx, "hover-card-content") {
		t.Fatal("hovering the trigger past the open delay did not open the card")
	}

	cx.SimulateMouseMove(ovPoint(100, 100), nil, gpui.NoModifiers())
	cx.AdvanceClock(hoverCardTestDelay.Milliseconds())
	cx.RunUntilParked()
	ovDraw(t, cx)
	if ovHasBounds(cx, "hover-card-content") {
		t.Fatal("leaving past the close delay did not close the card")
	}
}

// U:hover_card.rs:331-360
func TestHoverCardReportsEachOpenChange(t *testing.T) {
	ta := gpui.NewTestApp()
	var changes []bool
	_, cx := openTestView(t, ta, hoverCardHarness(&changes, false))
	ovDraw(t, cx)

	cx.SimulateMouseMove(ovPoint(10, 10), nil, gpui.NoModifiers())
	if len(changes) != 0 {
		t.Fatalf("changes before the delay = %v", changes)
	}
	cx.AdvanceClock(hoverCardTestDelay.Milliseconds())
	cx.RunUntilParked()
	ovDraw(t, cx)
	ovDraw(t, cx)
	if !slices.Equal(changes, []bool{true}) {
		t.Fatalf("changes = %v, want [true]", changes)
	}

	cx.SimulateMouseMove(ovPoint(100, 100), nil, gpui.NoModifiers())
	cx.AdvanceClock(hoverCardTestDelay.Milliseconds())
	cx.RunUntilParked()
	ovDraw(t, cx)
	if !slices.Equal(changes, []bool{true, false}) {
		t.Fatalf("changes = %v, want [true false]", changes)
	}
}

// U:hover_card.rs:361-405
func TestHoverCardTapCardIgnoresHoverAndTogglesAndDismisses(t *testing.T) {
	ta := gpui.NewTestApp()
	var changes []bool
	_, cx := openTestView(t, ta, hoverCardHarness(&changes, true))
	ovDraw(t, cx)
	cx.SimulateMouseMove(ovPoint(10, 10), nil, gpui.NoModifiers())
	cx.AdvanceClock(time.Second.Milliseconds())
	cx.RunUntilParked()
	if len(changes) != 0 {
		t.Fatalf("hover opened a tap card: %v", changes)
	}

	cx.SimulateClick(ovPoint(10, 10), gpui.NoModifiers())
	ovDraw(t, cx)
	ovDraw(t, cx)
	if !ovHasBounds(cx, "hover-card-content") {
		t.Fatal("a tap did not open the card")
	}
	cx.SimulateMouseMove(ovPoint(100, 100), nil, gpui.NoModifiers())
	cx.AdvanceClock(time.Second.Milliseconds())
	cx.RunUntilParked()
	if !slices.Equal(changes, []bool{true}) {
		t.Fatalf("changes = %v, want [true]", changes)
	}

	cx.SimulateClick(ovPoint(10, 10), gpui.NoModifiers())
	ovDraw(t, cx)
	if ovHasBounds(cx, "hover-card-content") {
		t.Fatal("a second tap did not close the card")
	}
	if !slices.Equal(changes, []bool{true, false}) {
		t.Fatalf("changes = %v, want [true false]", changes)
	}

	cx.SimulateClick(ovPoint(10, 10), gpui.NoModifiers())
	ovDraw(t, cx)
	ovDraw(t, cx)
	bounds := debugBounds(t, cx, "hover-card-content")
	cx.SimulateClick(bounds.Center(), gpui.NoModifiers())
	if !slices.Equal(changes, []bool{true, false, true}) {
		t.Fatalf("changes = %v, want [true false true]", changes)
	}
	cx.SimulateClick(ovPoint(100, 100), gpui.NoModifiers())
	ovDraw(t, cx)
	if ovHasBounds(cx, "hover-card-content") {
		t.Fatal("a tap outside did not dismiss the card")
	}
	if !slices.Equal(changes, []bool{true, false, true, false}) {
		t.Fatalf("changes = %v, want [true false true false]", changes)
	}
}
