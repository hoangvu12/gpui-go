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
			Child(gpui.Div().
				ID("background").
				Absolute().
				SizeFull().
				OnMouseMove(func(*gpui.MouseMoveEvent, *gpui.Window, *gpui.App) { backgroundHovered = true })).
			Child(NewPopup("popup", gpui.Div().Size(gpui.Px(100))).Content(
				gpui.Div().
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
		return gpui.OwnRecipe(NewPopup("popup", gpui.Div().DebugSelector("popup-trigger").Size(gpui.Px(100))).
			Content(gpui.Div().DebugSelector("popup-content").Size(gpui.Px(20))))
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
