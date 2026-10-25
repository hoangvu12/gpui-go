package base

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// alert_dialog.rs tests (2/2 ported, U:alert_dialog.rs:292-352) plus one
// Go-own test of the action wrappers.

// U:alert_dialog.rs:311-324
func TestAlertDialogBackdropIsNotClosableByDefault(t *testing.T) {
	ta := gpui.NewTestApp()
	ta.Update(ovInit)
	closeRequested := false
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.OwnRecipe(NewAlertDialog().
			RequestClose(func(bool, *gpui.Window, *gpui.App) { closeRequested = true }).
			Backdrop(gpui.Div().Flex().Size(gpui.Px(200))))
	})
	ovDraw(t, cx)
	cx.SimulateClick(ovPoint(20, 20), gpui.NoModifiers())
	if closeRequested {
		t.Fatal("a backdrop press closed an alert dialog")
	}
}

// U:alert_dialog.rs:326-351: styling the alert dialog replaces the
// host's centering layout just as it does for a plain dialog.
func TestAlertDialogHostStylePositionsThePopup(t *testing.T) {
	ta := gpui.NewTestApp()
	ta.Update(ovInit)
	var bounds gpui.Bounds
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.OwnRecipe(NewAlertDialog().ItemsStart().JustifyStart().Popup(
			gpui.Canvas(func(b gpui.Bounds, _ *gpui.Window, _ *gpui.App) struct{} {
				bounds = b
				return struct{}{}
			}, func(gpui.Bounds, struct{}, *gpui.Window, *gpui.App) {}).Flex().Size(gpui.Px(100))))
	})
	ovDraw(t, cx)
	if bounds.Origin != (gpui.Point{}) {
		t.Fatalf("popup origin = %v, want (0, 0)", bounds.Origin)
	}
}

// Go-own: the action wrappers dispatch Confirm and Cancel to the dialog,
// whose decisions close it (U:alert_dialog.rs:130-139, 180-190;
// U:dialog.rs:571-600).
func TestAlertDialogActionAndCancelDispatchToTheDialog(t *testing.T) {
	for _, tc := range []struct {
		name    string
		confirm bool
	}{{"action", true}, {"cancel", false}} {
		t.Run(tc.name, func(t *testing.T) {
			ta := gpui.NewTestApp()
			ta.Update(ovInit)
			focus := ta.App().FocusHandle()
			handle := NewDialogHandle(true)
			var requested []bool
			_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
				var control any = NewAlertDialogCancel().Size(gpui.Px(50))
				if tc.confirm {
					control = NewAlertDialogAction().Size(gpui.Px(50))
				}
				return gpui.OwnRecipe(NewAlertDialog().
					Handle(handle).
					FocusHandle(focus).
					ItemsStart().
					JustifyStart().
					RequestClose(func(confirmed bool, _ *gpui.Window, _ *gpui.App) { requested = append(requested, confirmed) }).
					Popup(NewAlertDialogPopup().Child(control)))
			})
			cx.Update(func(w *gpui.Window, _ *gpui.App) { focus.Focus(w) })
			ovDraw(t, cx)
			cx.SimulateClick(ovPoint(10, 10), gpui.NoModifiers())
			cx.RunUntilParked()
			if handle.IsOpen() || len(requested) != 1 || requested[0] != tc.confirm {
				t.Fatalf("open=%v requested=%v", handle.IsOpen(), requested)
			}
		})
	}
}
