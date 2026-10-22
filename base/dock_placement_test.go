package base

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ported tests of U:dock/dock_placement.rs (7/7) and U:dock/drag.rs (4/4).

func dockArea800x600() gpui.Bounds {
	return gpui.Bounds{Size: gpui.Size{Width: 800, Height: 600}}
}

// U:dock/dock_placement.rs:187-197
func TestDockPlacementALeftDockCannotSqueezePastTheRightDock(t *testing.T) {
	sizing := NewDockSizing(DockPlacementLeft).WithAreaWidth(1000).WithOppositeDockSize(300)
	if got, want := sizing.Clamp(900), float32(1000)-PanelMinSize-300; got != want {
		t.Fatalf("clamp = %v, want %v", got, want)
	}
}

// U:dock/dock_placement.rs:199-203
func TestDockPlacementADockNeverClampsBelowTheMinimum(t *testing.T) {
	sizing := NewDockSizing(DockPlacementBottom).WithAreaHeight(120)
	if got := sizing.Clamp(1); got != PanelMinSize {
		t.Fatalf("clamp = %v, want %v", got, PanelMinSize)
	}
}

// U:dock/dock_placement.rs:205-216
func TestDockPlacementABottomDockMeasuresFromTheAreaBottom(t *testing.T) {
	sizing := NewDockSizing(DockPlacementBottom).WithAreaBounds(dockArea800x600())
	if got := sizing.SizeFromPointer(gpui.Point{X: 400, Y: 400}); got != 200 {
		t.Fatalf("size = %v, want 200", got)
	}
}

// U:dock/dock_placement.rs:218-226
func TestDockPlacementARightDockMeasuresFromTheAreaRightEdge(t *testing.T) {
	sizing := NewDockSizing(DockPlacementRight).WithAreaBounds(dockArea800x600())
	if got := sizing.SizeFromPointer(gpui.Point{X: 500}); got != 300 {
		t.Fatalf("size = %v, want 300", got)
	}
}

// U:dock/dock_placement.rs:228-239
func TestDockPlacementADockNeverShrinksBelowTheMinimum(t *testing.T) {
	dock := NewDock(240)
	dock.SetSize(1)
	if dock.Size() != PanelMinSize {
		t.Fatalf("size = %v, want %v", dock.Size(), PanelMinSize)
	}
	dock.SetSize(400)
	if dock.Size() != 400 {
		t.Fatalf("a size above the floor is not kept: %v", dock.Size())
	}
}

// U:dock/dock_placement.rs:241-253
func TestDockPlacementTheCenterPlacementIsAnsweredRatherThanPanicking(t *testing.T) {
	sizing := NewDockSizing(DockPlacementCenter).WithAreaBounds(dockArea800x600())
	if got := sizing.Clamp(7); got != 7 {
		t.Fatalf("the center clamps nothing: %v", got)
	}
	if got := sizing.SizeFromPointer(gpui.Point{X: 400, Y: 300}); got != 0 {
		t.Fatalf("size = %v, want 0", got)
	}
}

// U:dock/dock_placement.rs:255-262
func TestDockPlacementDockStateDefaultsToOpenAndCollapsible(t *testing.T) {
	dock := NewDock(240)
	if !dock.IsOpen() || !dock.IsCollapsible() || dock.Size() != 240 || dock.IsResizing() {
		t.Fatalf("dock = %+v", dock)
	}
}

func dockDragBounds() gpui.Bounds {
	return gpui.Bounds{Origin: gpui.Point{X: 120, Y: 80}, Size: gpui.Size{Width: 400, Height: 300}}
}

// U:dock/drag.rs:289-331
func TestDockDragDropPlaceholderBoundsCoverEachTargetPlacement(t *testing.T) {
	bounds := dockDragBounds()
	half := func(x, y, w, h float32) DropPlaceholderBounds {
		return NewDropPlaceholderBounds(gpui.Point{X: x, Y: y}, gpui.Size{Width: w, Height: h})
	}
	for _, tc := range []struct {
		placement Placement
		has       bool
		want      DropPlaceholderBounds
	}{
		{PlacementLeft, true, half(0, 0, 200, 300)},
		{PlacementRight, true, half(200, 0, 200, 300)},
		{PlacementTop, true, half(0, 0, 400, 150)},
		{PlacementBottom, true, half(0, 150, 400, 150)},
		{0, false, half(0, 0, 400, 300)},
	} {
		if got := DropPlaceholderBoundsForPlacement(bounds, tc.placement, tc.has); got != tc.want {
			t.Fatalf("%v (%v): %+v, want %+v", tc.placement, tc.has, got, tc.want)
		}
	}
}

func requirePlacementAt(t *testing.T, bounds gpui.Bounds, x, y float32, want Placement, wantOK bool) {
	t.Helper()
	got, ok := splitPlacementAt(bounds, gpui.Point{X: x, Y: y})
	if ok != wantOK || (ok && got != want) {
		t.Fatalf("at (%v, %v) = %v %v, want %v %v", x, y, got, ok, want, wantOK)
	}
}

// U:dock/drag.rs:333-347
func TestDockDragSplitPlacementFollowsTheCursorZone(t *testing.T) {
	bounds := dockDragBounds()
	requirePlacementAt(t, bounds, 130, 230, PlacementLeft, true)
	requirePlacementAt(t, bounds, 510, 230, PlacementRight, true)
	requirePlacementAt(t, bounds, 320, 90, PlacementTop, true)
	requirePlacementAt(t, bounds, 320, 370, PlacementBottom, true)
	// The centre merges into the tab group.
	requirePlacementAt(t, bounds, 320, 230, 0, false)
}

// U:dock/drag.rs:349-365
func TestDockDragSplitPlacementPrefersHorizontalInTheCorners(t *testing.T) {
	bounds := gpui.Bounds{Size: gpui.Size{Width: 400, Height: 300}}
	// Top-left satisfies both Left and Top; x is tested first.
	requirePlacementAt(t, bounds, 10, 10, PlacementLeft, true)
	requirePlacementAt(t, bounds, 390, 290, PlacementRight, true)
}

// U:dock/drag.rs:367-379
func TestDockDragSplitPlacementBoundariesFallIntoTheCentre(t *testing.T) {
	bounds := gpui.Bounds{Size: gpui.Size{Width: 400, Height: 300}}
	// Comparisons are strict, so the threshold itself is the centre zone.
	requirePlacementAt(t, bounds, 140, 150, 0, false)
	requirePlacementAt(t, bounds, 260, 150, 0, false)
	requirePlacementAt(t, bounds, 200, 105, 0, false)
	requirePlacementAt(t, bounds, 200, 195, 0, false)
}
