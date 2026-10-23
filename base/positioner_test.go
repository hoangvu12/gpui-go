package base

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// positioner.rs tests (15/15 ported, U:positioner.rs:451-792).

const positionerTestMargin float32 = 4

func positionerTrigger(x, y, w, h float32) gpui.Bounds {
	return gpui.Bounds{Origin: gpui.Point{X: x, Y: y}, Size: gpui.Size{Width: w, Height: h}}
}

func positionerViewport() gpui.Size { return gpui.Size{Width: 500, Height: 400} }

func positionerSideAt(trigger gpui.Bounds, placement gpui.Optional[Placement], align Align, popup gpui.Size) ResolvedPosition {
	return positionerResolve(positionerStrategy{triggerBounds: trigger, placement: placement, align: align}, popup, positionerViewport(), edgesAll(positionerTestMargin))
}

func posRequirePlacement(t *testing.T, got ResolvedPosition, want Placement) {
	t.Helper()
	if p, ok := got.Placement.Get(); !ok || p != want {
		t.Fatalf("placement = %v (%v), want %v", p, ok, want)
	}
}

func posRequireF32(t *testing.T, what string, got, want float32) {
	t.Helper()
	if got != want {
		t.Fatalf("%s = %v, want %v", what, got, want)
	}
}

// U:positioner.rs:485-496
func TestPositionerPrefersTheRequestedSideWhenItFits(t *testing.T) {
	p := positionerSideAt(positionerTrigger(200, 200, 40, 20), gpui.Some(PlacementTop), AlignCenter, gpui.Size{Width: 80, Height: 30})
	posRequirePlacement(t, p, PlacementTop)
	posRequireF32(t, "bottom", p.Bounds.Bottom(), 200)
}

// U:positioner.rs:498-509
func TestPositionerFlipsToTheOppositeSideWhenThePreferredSideDoesNotFit(t *testing.T) {
	p := positionerSideAt(positionerTrigger(200, 10, 40, 20), gpui.Some(PlacementTop), AlignCenter, gpui.Size{Width: 80, Height: 60})
	posRequirePlacement(t, p, PlacementBottom)
	posRequireF32(t, "top", p.Bounds.Top(), 30)
}

// U:positioner.rs:511-522
func TestPositionerClampsIntoTheViewportWhileKeepingTheFlippedSide(t *testing.T) {
	p := positionerSideAt(positionerTrigger(480, 200, 40, 20), gpui.Some(PlacementBottom), AlignCenter, gpui.Size{Width: 120, Height: 30})
	posRequirePlacement(t, p, PlacementBottom)
	posRequireF32(t, "right", p.Bounds.Right(), positionerViewport().Width-positionerTestMargin)
}

// U:positioner.rs:524-541
func TestPositionerAlignmentSelectsTheLeadingCenterOrTrailingEdge(t *testing.T) {
	trigger := positionerTrigger(200, 200, 100, 20)
	popup := gpui.Size{Width: 40, Height: 30}
	bottom := gpui.Some(PlacementBottom)
	posRequireF32(t, "start left", positionerSideAt(trigger, bottom, AlignStart, popup).Bounds.Left(), 200)
	posRequireF32(t, "center left", positionerSideAt(trigger, bottom, AlignCenter, popup).Bounds.Left(), 230)
	posRequireF32(t, "end left", positionerSideAt(trigger, bottom, AlignEnd, popup).Bounds.Left(), 260)
}

// U:positioner.rs:543-558
func TestPositionerSideOffsetAddsAGapBetweenTriggerAndPopup(t *testing.T) {
	p := positionerResolve(positionerStrategy{
		triggerBounds: positionerTrigger(200, 200, 40, 20),
		placement:     gpui.Some(PlacementBottom),
		align:         AlignCenter,
		offset:        8,
	}, gpui.Size{Width: 40, Height: 30}, positionerViewport(), edgesAll(positionerTestMargin))
	posRequireF32(t, "top", p.Bounds.Top(), 228)
}

// U:positioner.rs:560-574
func TestPositionerCornerPositioningPlacesTheNamedCornerAndNeverReportsASide(t *testing.T) {
	p := positionerResolve(positionerStrategy{corner: true, anchor: gpui.AnchorTopLeft, position: gpui.Point{X: 100, Y: 100}},
		gpui.Size{Width: 40, Height: 30}, positionerViewport(), edgesAll(positionerTestMargin))
	if p.Placement.IsSome() {
		t.Fatal("corner positioning reported a side")
	}
	if p.Bounds.Origin != (gpui.Point{X: 100, Y: 100}) {
		t.Fatalf("origin = %v", p.Bounds.Origin)
	}
}

// U:positioner.rs:576-591
func TestPositionerCornerPositioningClampsButDoesNotFlip(t *testing.T) {
	p := positionerResolve(positionerStrategy{corner: true, anchor: gpui.AnchorTopLeft, position: gpui.Point{X: 480, Y: 390}},
		gpui.Size{Width: 40, Height: 30}, positionerViewport(), edgesAll(positionerTestMargin))
	if p.Placement.IsSome() {
		t.Fatal("corner positioning reported a side")
	}
	posRequireF32(t, "right", p.Bounds.Right(), positionerViewport().Width-positionerTestMargin)
	posRequireF32(t, "bottom", p.Bounds.Bottom(), positionerViewport().Height-positionerTestMargin)
}

// U:positioner.rs:593-605
func TestPositionerCornerPositionCanBeUpdatedWithoutRebuildingThePositioner(t *testing.T) {
	p := PositionerCorner(gpui.AnchorTopLeft, gpui.Point{X: 10, Y: 20}).Position(gpui.Point{X: 30, Y: 40})
	if !p.strategy.corner || p.strategy.position != (gpui.Point{X: 30, Y: 40}) {
		t.Fatalf("strategy = %+v", p.strategy)
	}
}

// U:positioner.rs:607-647: a tiled edge draws no shadow, so a popup may
// run right up to the margin there; an untiled edge keeps the whole
// client inset as well.
func TestPositionerFrameInsetsApplyTheClientInsetOnlyOnUntiledEdges(t *testing.T) {
	const inset float32 = 20
	if got := frameInsets(decorations{}, inset); got != (gpui.Edges{}) {
		t.Fatalf("a server-decorated window has no frame to keep clear of: %v", got)
	}
	if got := frameInsets(decorations{client: true, tiling: tilingTiled()}, inset); got != (gpui.Edges{}) {
		t.Fatalf("a window tiled on every side draws no shadow padding: %v", got)
	}
	got := frameInsets(decorations{client: true, tiling: tiling{top: false, left: true, right: false, bottom: true}}, inset)
	if want := (gpui.Edges{Top: inset, Right: inset, Bottom: 0, Left: 0}); got != want {
		t.Fatalf("frame insets = %v, want %v", got, want)
	}
}

// U:positioner.rs:649-673: a menu opened from a trigger near the right
// edge of a tiled window must not be pushed inward by the client inset.
func TestPositionerClampingKeepsOnlyTheMarginOnATiledEdge(t *testing.T) {
	popup := gpui.Size{Width: 120, Height: 30}
	corner := positionerStrategy{corner: true, anchor: gpui.AnchorTopRight, position: gpui.Point{X: positionerViewport().Width - 10, Y: 100}}
	margin := edgesAll(positionerTestMargin)
	const inset float32 = 20

	tiled := positionerResolve(corner, popup, positionerViewport(), margin)
	posRequireF32(t, "tiled right", tiled.Bounds.Right(), positionerViewport().Width-10)

	untiled := positionerResolve(corner, popup, positionerViewport(), edgesAll(positionerTestMargin+inset))
	posRequireF32(t, "untiled right", untiled.Bounds.Right(), positionerViewport().Width-positionerTestMargin-inset)
}

// The migrated tooltip placement tests (U:positioner.rs:675-791).

const positionerTooltipMargin float32 = 4

func tooltipPlacementAt(trigger gpui.Bounds, popup, viewport gpui.Size, margin float32, placement gpui.Optional[Placement]) (gpui.Bounds, Placement) {
	resolved := positionerResolve(positionerStrategy{triggerBounds: trigger, placement: placement, align: AlignCenter}, popup, viewport, edgesAll(margin))
	side, ok := resolved.Placement.Get()
	if !ok {
		panic("side positioning reported no side")
	}
	return resolved.Bounds, side
}

func posSize(w, h float32) gpui.Size { return gpui.Size{Width: w, Height: h} }

// U:positioner.rs:709-721
func TestPositionerPrefersAboveWhenSpaceAllows(t *testing.T) {
	bounds, side := tooltipPlacementAt(positionerTrigger(100, 80, 80, 24), posSize(120, 30), posSize(300, 200), positionerTooltipMargin, gpui.Optional[Placement]{})
	if side != PlacementTop || bounds.Origin != (gpui.Point{X: 80, Y: 50}) {
		t.Fatalf("got %v %v", side, bounds.Origin)
	}
}

// U:positioner.rs:722-750
func TestPositionerFlipsAndClampsOnEachAxis(t *testing.T) {
	if _, side := tooltipPlacementAt(positionerTrigger(24, 4, 120, 32), posSize(240, 32), posSize(520, 260), positionerTooltipMargin, gpui.Optional[Placement]{}); side != PlacementBottom {
		t.Fatalf("top flip = %v", side)
	}
	if _, side := tooltipPlacementAt(positionerTrigger(260, 60, 32, 32), posSize(120, 30), posSize(300, 200), positionerTooltipMargin, gpui.Some(PlacementRight)); side != PlacementLeft {
		t.Fatalf("right flip = %v", side)
	}
	bounds, _ := tooltipPlacementAt(positionerTrigger(4, 80, 24, 24), posSize(120, 30), posSize(300, 200), positionerTooltipMargin, gpui.Optional[Placement]{})
	posRequireF32(t, "left edge", bounds.Left(), positionerTooltipMargin)
}

// U:positioner.rs:751-764
func TestPositionerPlacesTooltipToTheRight(t *testing.T) {
	trigger := positionerTrigger(20, 60, 32, 32)
	bounds, side := tooltipPlacementAt(trigger, posSize(120, 30), posSize(300, 200), positionerTooltipMargin, gpui.Some(PlacementRight))
	if side != PlacementRight {
		t.Fatalf("side = %v", side)
	}
	posRequireF32(t, "left", bounds.Left(), trigger.Right())
	posRequireF32(t, "center y", bounds.Center().Y, trigger.Center().Y)
}

// U:positioner.rs:765-778
func TestPositionerRightPlacementClampsVerticalEdges(t *testing.T) {
	trigger := positionerTrigger(20, 2, 32, 20)
	bounds, side := tooltipPlacementAt(trigger, posSize(120, 40), posSize(300, 200), positionerTooltipMargin, gpui.Some(PlacementRight))
	if side != PlacementRight {
		t.Fatalf("side = %v", side)
	}
	posRequireF32(t, "top", bounds.Top(), positionerTooltipMargin)
	posRequireF32(t, "left", bounds.Left(), trigger.Right())
}

// U:positioner.rs:779-791
func TestPositionerUsesLargerSideWhenNeitherVerticalSideFits(t *testing.T) {
	bounds, side := tooltipPlacementAt(positionerTrigger(120, 20, 40, 20), posSize(160, 120), posSize(300, 100), positionerTooltipMargin, gpui.Optional[Placement]{})
	if side != PlacementBottom {
		t.Fatalf("side = %v", side)
	}
	posRequireF32(t, "top", bounds.Top(), positionerTooltipMargin)
	posRequireF32(t, "left", bounds.Left(), 60)
}
