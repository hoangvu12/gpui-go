package base

import (
	"slices"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// sheet.rs tests (4/4 ported, U:sheet.rs:169-270), moved onto the core
// modal (browser-core ticket 72): Escape is the modal's close watcher
// and an overlay press is the closedby=any light dismiss — both run the
// sheet's close steps through the entry's toggle.

// sheetHarness is U:sheet.rs:175-225 Harness plus harness().
func sheetHarness(t *testing.T, closable bool, focusFirst bool) (*gpui.VisualTestContext, *[]string) {
	t.Helper()
	ta := gpui.NewTestApp()
	ta.Update(ovInit)
	focus := ta.App().FocusHandle()
	events := &[]string{}
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		sheet := NewSheet().
			FocusHandle(focus).
			OverlayClosable(closable).
			RequestClose(func(*gpui.Window, *gpui.App) { *events = append(*events, "request") }).
			OnClose(func(*gpui.ClickEvent, *gpui.Window, *gpui.App) { *events = append(*events, "closed") }).
			Overlay(gpui.Div().Flex().Absolute().Inset0()).
			Surface(gpui.Div().Flex().Absolute().Right0().Top0().HFull().W(gpui.Px(80)).Occlude())
		return gpui.OwnRecipe(sheet)
	})
	if focusFirst {
		cx.Update(func(w *gpui.Window, _ *gpui.App) { focus.Focus(w) })
	}
	ovDraw(t, cx)
	return cx, events
}

// U:sheet.rs:227-232: an overlay press light-dismisses the modal
// (closedby=any) and the entry's toggle runs the sheet's close steps.
func TestSheetOverlayCloseRequestsThenNotifies(t *testing.T) {
	cx, events := sheetHarness(t, true, true)
	cx.SimulateClick(ovPoint(20, 20), gpui.NoModifiers())
	cx.RunUntilParked()
	if !slices.Equal(*events, []string{"request", "closed"}) {
		t.Fatalf("events = %v", *events)
	}
}

// U:sheet.rs:234-239: with OverlayClosable(false) the closedby policy
// is closerequest — an outside press is inert and requests nothing.
func TestSheetNonClosableOverlayDoesNotRequestClose(t *testing.T) {
	cx, events := sheetHarness(t, false, true)
	cx.SimulateClick(ovPoint(20, 20), gpui.NoModifiers())
	cx.RunUntilParked()
	if len(*events) != 0 {
		t.Fatalf("events = %v", *events)
	}
}

// U:sheet.rs:241-247: Escape uses the same close order. browser-core
// ticket 72: the Escape key itself (no action dispatch — the binding is
// gone) signals the modal's close watcher, whose toggle runs the close
// steps; the focus trap is gone with the modal's inert outside.
func TestSheetEscapeUsesTheSameCloseOrder(t *testing.T) {
	cx, events := sheetHarness(t, true, true)
	cx.SimulateKeystrokes("escape")
	cx.RunUntilParked()
	if !slices.Equal(*events, []string{"request", "closed"}) {
		t.Fatalf("events = %v", *events)
	}
}

// sheetInScrollArea draws a sheet inside a 100x100 scroll area at
// (20,20) and reports the surface's bounds and the content mask it
// painted under. deferred wraps the sheet in gpui.Deferred, as a caller
// does to lift it out of the scroll area's clip.
func sheetInScrollArea(t *testing.T, deferred bool) (surface, mask, viewport gpui.Bounds) {
	t.Helper()
	ta := gpui.NewTestApp()
	ta.Update(ovInit)
	_, cx := openTestView(t, ta, func(w *gpui.Window, _ *gpui.Context[testView]) gpui.AnyElement {
		size := w.ViewportSize()
		viewport = gpui.Bounds{Size: size}
		probe := gpui.Canvas(
			func(gpui.Bounds, *gpui.Window, *gpui.App) struct{} { return struct{}{} },
			func(b gpui.Bounds, _ struct{}, w *gpui.Window, _ *gpui.App) {
				surface, mask = b, gpui.ContentMask(w)
			},
		).Flex().Absolute().Top0().Left0().SizeFull()
		var sheet any = NewSheet().Surface(probe)
		if deferred {
			sheet = gpui.Deferred(sheet)
		}
		scroll := gpui.Div().Flex().ID("scroll").Absolute().Left(gpui.Px(20)).Top(gpui.Px(20)).
			W(gpui.Px(100)).H(gpui.Px(100)).OverflowYScroll().
			Child(gpui.Div().Flex().H(gpui.Px(300))).
			Child(sheet)
		return gpui.Div().Flex().SizeFull().Child(scroll).IntoElement()
	})
	ovDraw(t, cx)
	return surface, mask, viewport
}

// Ticket 42: upstream's base Sheet anchors at the window origin but is
// not deferred (U:sheet.rs:123, unlike Dialog's deferred host at
// U:dialog.rs:548-643), so drawn in place it lays out viewport-sized yet
// paints under a scrolling ancestor's clip. Upstream's styled sheet never
// meets one: component Root mounts it in the window's root layer
// (component/src/root.rs:156-179, 490). A caller that renders the base
// Sheet inside a scroll area wraps it in gpui.Deferred, which paints it
// under the window's mask (R:window.rs:3661-3703).
func TestSheetInsideAScrollAreaIsClippedUnlessDeferred(t *testing.T) {
	surface, mask, viewport := sheetInScrollArea(t, false)
	if surface != viewport {
		t.Fatalf("in-place surface bounds = %v, want the viewport %v", surface, viewport)
	}
	scrollArea := gpui.Bounds{Origin: ovPoint(20, 20), Size: gpui.Size{Width: 100, Height: 100}}
	if mask != scrollArea {
		t.Fatalf("in-place surface mask = %v, want the scroll area %v (upstream parity: not deferred)", mask, scrollArea)
	}

	surface, mask, viewport = sheetInScrollArea(t, true)
	if surface != viewport {
		t.Fatalf("deferred surface bounds = %v, want the viewport %v", surface, viewport)
	}
	if mask != viewport {
		t.Fatalf("deferred surface mask = %v, want the window %v (not clipped by the scroll area)", mask, viewport)
	}
}

// Go-own, browser-core ticket 72: the DismissBeforeY cutoff is gone
// with the overlay's own press listener — Chrome's closedby=any light
// dismiss has no positional rule, so every press outside the surface
// closes the sheet wherever it lands.
func TestSheetEveryOverlayPressClosesWhereverItLands(t *testing.T) {
	cx, events := sheetHarness(t, true, false)
	cx.SimulateClick(ovPoint(20, 20), gpui.NoModifiers())
	cx.RunUntilParked()
	if !slices.Equal(*events, []string{"request", "closed"}) {
		t.Fatalf("events = %v", *events)
	}
}
