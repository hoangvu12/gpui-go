package base

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// popup.rs tests (3/3 ported, U:popup.rs:203-325). The overlay tests
// share the helpers below; they
// run on the deterministic test window (gpui.TestApp), where the pinned
// `window.draw(cx).clear(cx)` is ovDraw.

// ovDraw draws one frame inside a window update, like the pinned
// `cx.update(|window, cx| window.draw(cx).clear(cx))`.
func ovDraw(t *testing.T, cx *gpui.VisualTestContext) {
	t.Helper()
	cx.Update(func(w *gpui.Window, _ *gpui.App) {
		if _, err := gpui.DrawWindowFrame(w); err != nil {
			t.Fatal(err)
		}
	})
}

// ovHasBounds reports whether a debug selector was drawn in the last
// frame (the pinned `cx.debug_bounds(selector).is_some()`).
func ovHasBounds(cx *gpui.VisualTestContext, selector string) bool {
	_, ok := cx.DebugBounds(selector)
	return ok
}

// ovPoint builds a point.
func ovPoint(x, y float32) gpui.Point { return gpui.Point{X: x, Y: y} }

// U:popup.rs:208-222
func TestPopupResolvedCornerPreservesExistingAnchorMath(t *testing.T) {
	bounds := gpui.Bounds{Origin: ovPoint(100, 100), Size: gpui.Size{Width: 200, Height: 50}}
	if got := PopupResolvedCorner(gpui.AnchorTopCenter, bounds); got != ovPoint(200, 100) {
		t.Fatalf("top center = %v", got)
	}
	if got := PopupResolvedCorner(gpui.AnchorBottomRight, bounds); got != ovPoint(300, 50) {
		t.Fatalf("bottom right = %v", got)
	}
}

// U:popup.rs:242-308: the host blocks the mouse over the popup surface,
// so the panel it covers stops reacting to a pointer over the popup,
// while the popup's own content still does.
//
// Display (map.md gap 2b): the harness's outer div is a plain div
// upstream (U:popup.rs:254), Block under gpui-ce 0.2.2, so it is built
// with Block() like every plain upstream div. The popup root's own Block
// does not cover it: under a flex parent the root stretches to the
// container's 200px height and the content lands below the box.
func TestPopupTheSurfaceBlocksThePanelItCovers(t *testing.T) {
	ta := gpui.NewTestApp()
	var backgroundHovered, contentHovered bool
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.Div().
			Block().
			Relative().
			Size(gpui.Px(200)).
			Child(gpui.Div().Flex().
				ID("background").
				Absolute().
				SizeFull().
				OnMouseMove(func(*gpui.MouseMoveEvent, *gpui.Window, *gpui.App) { backgroundHovered = true })).
			Child(NewPopup("popup", gpui.Div().Flex().Size(gpui.Px(100))).Content(
				gpui.Div().Flex().
					ID("content").
					Size(gpui.Px(40)).
					OnMouseMove(func(*gpui.MouseMoveEvent, *gpui.Window, *gpui.App) { contentHovered = true }))).
			IntoElement()
	})
	ovDraw(t, cx)
	ovDraw(t, cx)

	cx.SimulateMouseMove(ovPoint(20, 110), nil, gpui.NoModifiers())
	if backgroundHovered {
		t.Fatal("the panel behind the popup received the move")
	}
	// The surface blocks what is behind it, not its own content: the
	// hitbox goes in ahead of the children, never over them.
	if !contentHovered {
		t.Fatal("the popup content did not receive the move")
	}

	// The same pointer outside the surface still reaches the panel, so
	// the assertion above is about occlusion and not a dead listener.
	cx.SimulateMouseMove(ovPoint(150, 180), nil, gpui.NoModifiers())
	if !backgroundHovered {
		t.Fatal("the panel outside the popup did not receive the move")
	}
}

// U:popup.rs:310-324: the trigger is measured on the first frame and the
// deferred content appears on the next.
func TestPopupTriggerCaptureEnablesDeferredContentOnTheNextFrame(t *testing.T) {
	ta := gpui.NewTestApp()
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.OwnRecipe(NewPopup("popup", gpui.Div().Flex().DebugSelector("popup-trigger").Size(gpui.Px(100))).
			Content(gpui.Div().Flex().DebugSelector("popup-content").Size(gpui.Px(20))))
	})
	ovDraw(t, cx)
	ovDraw(t, cx)

	if b := debugBounds(t, cx, "popup-trigger"); b.Size != (gpui.Size{Width: 100, Height: 100}) {
		t.Fatalf("trigger size = %v", b.Size)
	}
	if b := debugBounds(t, cx, "popup-content"); b.Size != (gpui.Size{Width: 20, Height: 20}) {
		t.Fatalf("content size = %v", b.Size)
	}
}

// popupPlacementView renders a Popup around a 100x100 trigger inside a
// relative container of the same size, so the host's measure canvas
// captures the trigger's box; the content is a 100x60 debug box.
func popupPlacementView(anchor gpui.Anchor, offset float32) func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
	return func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.Div().Flex().SizeFull().Child(gpui.Div().Flex().Relative().Mt(gpui.Px(100)).W(gpui.Px(100)).H(gpui.Px(100)).Child(gpui.OwnRecipe(
			NewPopup("placement", gpui.Div().Flex().ID("placement-trigger").Size(gpui.Px(100))).
				Anchor(anchor).
				Offset(offset).
				Content(gpui.Div().Flex().DebugSelector("placement-content").W(gpui.Px(100)).H(gpui.Px(60))),
		))).IntoElement()
	}
}

// browser-core ticket 72: the popup host places its content through the
// core's element-anchored Anchored — each corner anchor maps onto a
// side and alignment of the measured trigger (the old positioner's
// corner math, U:popup.rs:102-114), and the placement clamps into the
// viewport without flipping (the corner rule, U:positioner.rs:95-109).
// The popup is as wide as the trigger here, so every alignment lands
// the same left edge; the top-family anchors place below the trigger
// and the bottom-family above. The placements clamp into the window
// margin (8px), so a trigger at the container's left edge places its
// popup 8px in (the corner rule's clamp, U:positioner.rs:303-325).
func TestPopupPlacesEachCornerAnchorThroughTheCoreAnchored(t *testing.T) {
	cases := []struct {
		anchor gpui.Anchor
		want   gpui.Bounds
	}{
		{gpui.AnchorTopLeft, gpui.Bounds{Origin: ovPoint(8, 200), Size: gpui.Size{Width: 100, Height: 60}}},
		{gpui.AnchorTopCenter, gpui.Bounds{Origin: ovPoint(8, 200), Size: gpui.Size{Width: 100, Height: 60}}},
		{gpui.AnchorTopRight, gpui.Bounds{Origin: ovPoint(8, 200), Size: gpui.Size{Width: 100, Height: 60}}},
		{gpui.AnchorBottomLeft, gpui.Bounds{Origin: ovPoint(8, 40), Size: gpui.Size{Width: 100, Height: 60}}},
		{gpui.AnchorBottomCenter, gpui.Bounds{Origin: ovPoint(8, 40), Size: gpui.Size{Width: 100, Height: 60}}},
		{gpui.AnchorBottomRight, gpui.Bounds{Origin: ovPoint(8, 40), Size: gpui.Size{Width: 100, Height: 60}}},
	}
	for _, tc := range cases {
		anchor := tc.anchor
		ta := gpui.NewTestApp()
		_, cx := openTestView(t, ta, popupPlacementView(anchor, 0))
		ovDraw(t, cx)
		ovDraw(t, cx)
		if got := debugBounds(t, cx, "placement-content"); got != tc.want {
			t.Fatalf("anchor %v: content = %v, want %v", anchor, got, tc.want)
		}
	}
}

// The offset is the gap along the side (U:popup.rs:60-64), and the
// placement clamps into the viewport keeping the window margin,
// without flipping (U:positioner.rs:303-325 — the corner rule).
func TestPopupOffsetAndClampThroughTheCoreAnchored(t *testing.T) {
	ta := gpui.NewTestApp()
	_, cx := openTestView(t, ta, popupPlacementView(gpui.AnchorTopLeft, 8))
	ovDraw(t, cx)
	ovDraw(t, cx)
	// Below the trigger with the 8px gap (and the window margin on the
	// left).
	if got := debugBounds(t, cx, "placement-content"); got.Origin != ovPoint(8, 208) {
		t.Fatalf("offset content origin = %v, want (8,208)", got.Origin)
	}

	// A trigger near the window's bottom edge: the below placement
	// overflows, and the shift clamps the popup's bottom edge to the
	// window's (1920x1080 test window, 8px margin).
	_, cx = openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.Div().Flex().SizeFull().Child(gpui.Div().Flex().Relative().Mt(gpui.Px(1000)).W(gpui.Px(100)).H(gpui.Px(100)).Child(gpui.OwnRecipe(
			NewPopup("clamped", gpui.Div().Flex().ID("clamped-trigger").Size(gpui.Px(100))).
				Content(gpui.Div().Flex().DebugSelector("clamped-content").W(gpui.Px(100)).H(gpui.Px(60))),
		))).IntoElement()
	})
	ovDraw(t, cx)
	ovDraw(t, cx)
	if got := debugBounds(t, cx, "clamped-content"); got.Bottom() != 1080-popupWindowMargin {
		t.Fatalf("clamped bottom = %v, want %v", got.Bottom(), 1080-popupWindowMargin)
	}
}
