package base

import (
	"slices"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// sheet.rs tests (4/4 ported, U:sheet.rs:169-270).

// sheetHarness is U:sheet.rs:175-225 Harness plus harness().
func sheetHarness(t *testing.T, closable bool, cutoff *float32, focusFirst bool) (*gpui.VisualTestContext, *[]string) {
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
			Overlay(gpui.Div().Absolute().Inset0().Occlude()).
			Surface(gpui.Div().Absolute().Right0().Top0().HFull().W(gpui.Px(80)).Occlude())
		if cutoff != nil {
			sheet.DismissBeforeY(*cutoff)
		}
		return gpui.OwnRecipe(sheet)
	})
	if focusFirst {
		cx.Update(func(w *gpui.Window, _ *gpui.App) { focus.Focus(w) })
	}
	ovDraw(t, cx)
	return cx, events
}

// U:sheet.rs:227-232
func TestSheetOverlayCloseRequestsThenNotifies(t *testing.T) {
	cx, events := sheetHarness(t, true, nil, true)
	cx.SimulateClick(ovPoint(20, 20), gpui.NoModifiers())
	if !slices.Equal(*events, []string{"request", "closed"}) {
		t.Fatalf("events = %v", *events)
	}
}

// U:sheet.rs:234-239
func TestSheetNonClosableOverlayDoesNotRequestClose(t *testing.T) {
	cx, events := sheetHarness(t, false, nil, true)
	cx.SimulateClick(ovPoint(20, 20), gpui.NoModifiers())
	if len(*events) != 0 {
		t.Fatalf("events = %v", *events)
	}
}

// U:sheet.rs:241-247
func TestSheetEscapeUsesTheSameCloseOrderAndRegistersFocusTrap(t *testing.T) {
	cx, events := sheetHarness(t, true, nil, true)
	var ok bool
	cx.Update(func(w *gpui.Window, app *gpui.App) { _, ok = ActiveFocusTrap(w, app) })
	if !ok {
		t.Fatal("the sheet registered no active focus trap")
	}
	cx.DispatchAction(CancelAction.Box(Cancel{}))
	if !slices.Equal(*events, []string{"request", "closed"}) {
		t.Fatalf("events = %v", *events)
	}
}

// U:sheet.rs:249-269
func TestSheetPointerAboveTheDismissCutoffIsIgnored(t *testing.T) {
	cutoff := float32(50)
	cx, events := sheetHarness(t, true, &cutoff, false)
	cx.SimulateClick(ovPoint(20, 20), gpui.NoModifiers())
	if len(*events) != 0 {
		t.Fatalf("events above the cutoff = %v", *events)
	}
	cx.SimulateClick(ovPoint(20, 80), gpui.NoModifiers())
	if !slices.Equal(*events, []string{"request", "closed"}) {
		t.Fatalf("events = %v", *events)
	}
}
