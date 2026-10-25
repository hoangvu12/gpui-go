package base

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// tooltip.rs tests: the provider's four tests (the grace switch, the
// priority, the disabled gate, the virtual-clock delays) are gone with
// the provider (browser-core ticket 72: the core tooltip hook owns the
// lifecycle — gpui/tooltip.go's unit tests and the tooltip parity
// fixtures pin it); the positioner tests stay (ticket 25), and one
// Go-own test pins the Base Tooltip surface on the hook.

func tooltipTestBounds(x, y, w, h float32) gpui.Bounds {
	return gpui.Bounds{Origin: ovPoint(x, y), Size: gpui.Size{Width: w, Height: h}}
}

// tooltipPositionerView renders one TooltipPositioner around an 80x30
// tip anchored to a tracked trigger cell, on the 1920x1080 test window.
func tooltipPositionerView(trigger *gpui.Bounds, placement *Placement) func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
	return func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		positioner := NewTooltipPositioner(trigger)
		if placement != nil {
			positioner.Placement(*placement)
		}
		return gpui.Div().Flex().SizeFull().Child(
			gpui.Deferred(positioner.Child(
				gpui.Div().Flex().DebugSelector("TIP").W(gpui.Px(80)).H(gpui.Px(30)).Bg(gpui.Rgb(0x16a34a)),
			)),
		).IntoElement()
	}
}

// TestTooltipPositionerPlacesBesideTheTrigger checks the migrated
// positioner's side placement through the core's Anchored (browser-core
// ticket 25): the tip sits centered above the trigger when it fits,
// flips below when the top does not fit, and shifts away from the
// window's right edge when the aligned placement would overflow. The
// trigger cell is tracked, so the tip follows a moving trigger.
func TestTooltipPositionerPlacesBesideTheTrigger(t *testing.T) {
	ta := gpui.NewTestApp()

	// Room to spare: centered above the trigger.
	trigger := &gpui.Bounds{Origin: gpui.Point{X: 100, Y: 500}, Size: gpui.Size{Width: 120, Height: 40}}
	_, cx := openTestView(t, ta, tooltipPositionerView(trigger, nil))
	ovDraw(t, cx)
	if got := debugBounds(t, cx, "TIP"); got != tooltipTestBounds(120, 470, 80, 30) {
		t.Fatalf("TIP = %v, want (120,470 80x30): centered above the trigger", got)
	}

	// Near the top edge: the top side does not fit, the flip moves the
	// tip below the trigger.
	trigger.Origin = gpui.Point{X: 100, Y: 10}
	cx.Update(func(*gpui.Window, *gpui.App) {})
	ovDraw(t, cx)
	if got := debugBounds(t, cx, "TIP"); got != tooltipTestBounds(120, 50, 80, 30) {
		t.Fatalf("TIP = %v, want (120,50 80x30): flipped below the trigger", got)
	}

	// Near the right edge (vert centered): neither the top nor the
	// flipped bottom fits horizontally, so the shift clamps the top
	// placement's right edge 4px from the window's right (1920).
	trigger.Origin = gpui.Point{X: 1820, Y: 500}
	cx.Update(func(*gpui.Window, *gpui.App) {})
	ovDraw(t, cx)
	if got := debugBounds(t, cx, "TIP"); got != tooltipTestBounds(1836, 470, 80, 30) {
		t.Fatalf("TIP = %v, want (1836,470 80x30): shifted 4px from the right window edge", got)
	}
}

// TestTooltipPositionerPlacesEachRequestedSide checks that the
// preferred placement selects the side, with the side-axis flip tactic
// (flip-block on a vertical side, flip-inline on a horizontal one).
func TestTooltipPositionerPlacesEachRequestedSide(t *testing.T) {
	ta := gpui.NewTestApp()
	trigger := &gpui.Bounds{Origin: gpui.Point{X: 100, Y: 500}, Size: gpui.Size{Width: 120, Height: 40}}
	cases := []struct {
		placement Placement
		want      gpui.Bounds
	}{
		{PlacementTop, tooltipTestBounds(120, 470, 80, 30)},
		{PlacementBottom, tooltipTestBounds(120, 540, 80, 30)},
		{PlacementLeft, tooltipTestBounds(20, 505, 80, 30)},
		{PlacementRight, tooltipTestBounds(220, 505, 80, 30)},
	}
	for _, tc := range cases {
		placement := tc.placement
		_, cx := openTestView(t, ta, tooltipPositionerView(trigger, &placement))
		ovDraw(t, cx)
		if got := debugBounds(t, cx, "TIP"); got != tc.want {
			t.Fatalf("placement %v: TIP = %v, want %v", tc.placement, got, tc.want)
		}
	}
}

// tooltipHookView renders an element carrying a Base Tooltip surface
// through the core hook's custom content (browser-core ticket 72: the
// app attaches .TooltipView, the window renders it in the hint layer).
func tooltipHookView(trigger gpui.FocusHandle) func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
	return func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.Div().Flex().SizeFull().Child(
			gpui.Div().Flex().ID("tooltip-anchor").
				W(gpui.Px(120)).H(gpui.Px(40)).
				TrackFocus(trigger).
				TooltipView(func(w *gpui.Window, app *gpui.App) gpui.AnyElement {
					// The custom content places itself: Anchored against
					// the invoker's recorded bounds (the hook records the
					// invoker's anchor registry entry).
					return gpui.Anchored().
						AnchorTo("tooltip-anchor").
						Side(gpui.AnchorSideBottom).
						Align(gpui.AnchorAlignStart).
						Gap(0).
						Child(NewTooltip("hook-tip").
							Child(gpui.Div().Flex().DebugSelector("hook-tip").
								P2().TextXs().Bg(gpui.Rgb(0x16a34a)).
								Child("Tip"))).
						IntoElement()
				}),
		).IntoElement()
	}
}

// Go-own, browser-core ticket 72: a Base Tooltip surface rides the core
// tooltip hook — the hover interest shows it after the show delay (the
// core's 250 ms, the hook's interest-delay-start), and Escape closes it
// through the hint entry's close path.
func TestTooltipSurfaceRidesTheCoreHook(t *testing.T) {
	ta := gpui.NewTestApp()
	ta.Update(ovInit)
	trigger := ta.App().FocusHandle()
	_, cx := openTestView(t, ta, tooltipHookView(trigger))
	ovDraw(t, cx)

	// Hover the anchor: nothing before the interest delay.
	cx.SimulateMouseMove(ovPoint(60, 20), nil, gpui.NoModifiers())
	cx.RunUntilParked()
	if ovHasBounds(cx, "hook-tip") {
		t.Fatal("the tooltip showed before the interest delay")
	}
	cx.AdvanceClock(249)
	if ovHasBounds(cx, "hook-tip") {
		t.Fatal("the tooltip showed before the interest delay elapsed")
	}
	cx.AdvanceClock(1)
	cx.RunUntilParked()
	if !ovHasBounds(cx, "hook-tip") {
		t.Fatal("the tooltip did not show after the interest delay")
	}

	// Escape closes it (the interest loss, then the close watcher).
	cx.SimulateKeystrokes("escape")
	cx.RunUntilParked()
	if ovHasBounds(cx, "hook-tip") {
		t.Fatal("escape did not close the tooltip")
	}
}
