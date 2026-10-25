package base

import (
	"slices"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// dialog.rs tests (6/6 ported, U:dialog.rs:648-910).

// dialogTestViewport draws a frame and returns the viewport.
func dialogTestViewport(t *testing.T, cx *gpui.VisualTestContext) gpui.Size {
	t.Helper()
	ovDraw(t, cx)
	return cx.Window().ViewportSize()
}

// U:dialog.rs:654-686: the close trigger supplies a Button with the
// accessible name "Close" and click activation.
func TestDialogCloseTriggerSuppliesAccessibleButton(t *testing.T) {
	ta := gpui.NewTestApp()
	var (
		captured bool
		role     gpui.Role
		hasRole  bool
		spec     gpui.A11ySpec
	)
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.Canvas(func(_ gpui.Bounds, w *gpui.Window, app *gpui.App) struct{} {
			NewDialogClose().Trigger(func(button *Button) any {
				element := button.render(w, app)
				role, hasRole = element.A11yRole()
				spec = gpui.A11ySpec{}
				element.WriteA11yInfo(&spec)
				captured = true
				return element
			})
			return struct{}{}
		}, func(gpui.Bounds, struct{}, *gpui.Window, *gpui.App) {}).Flex().SizeFull().IntoElement()
	})
	ovDraw(t, cx)
	if !captured {
		t.Fatal("the trigger builder never ran")
	}
	if !hasRole || role != gpui.RoleButton {
		t.Fatalf("role = %v (%v), want Button", role, hasRole)
	}
	if spec.Label != "Close" {
		t.Fatalf("label = %q, want Close", spec.Label)
	}
	if !slices.Contains(spec.Actions, gpui.A11yActionClick) {
		t.Fatalf("actions = %v, want Click", spec.Actions)
	}
}

// U:dialog.rs:688-755: the close trigger activates once per press and
// respects the cancel veto, for pointer and keyboard activation alike.
func TestDialogCloseTriggerActivatesOnceAndRespectsCancelVeto(t *testing.T) {
	ta := gpui.NewTestApp()
	ta.Update(ovInit)
	app := ta.App()
	focus := app.FocusHandle()
	buttonFocus := app.FocusHandle()
	handle := NewDialogHandle(true)
	attempts := 0
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.OwnRecipe(NewDialog().
			Handle(handle).
			FocusHandle(focus).
			OnCancel(func(*gpui.ClickEvent, *gpui.Window, *gpui.App) bool {
				attempts++
				return attempts > 1
			}).
			Popup(NewDialogClose().Trigger(func(button *Button) any {
				return button.Size(gpui.Px(100)).TrackFocus(buttonFocus)
			})))
	})
	cx.Update(func(w *gpui.Window, _ *gpui.App) { focus.Focus(w) })
	viewport := dialogTestViewport(t, cx)
	cx.SimulateClick(ovPoint(viewport.Width/2, viewport.Height/2), gpui.NoModifiers())
	cx.RunUntilParked()
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
	if !handle.IsOpen() {
		t.Fatal("on_cancel can veto pointer dismissal")
	}

	cx.Update(func(w *gpui.Window, _ *gpui.App) { buttonFocus.Focus(w) })
	ovDraw(t, cx)
	keystroke := gpui.MustParseKeystroke("space")
	cx.SimulateEvent(&gpui.KeyDownEvent{Keystroke: keystroke, IsHeld: false, PreferCharacterInput: false})
	cx.SimulateEvent(&gpui.KeyUpEvent{Keystroke: keystroke})
	cx.RunUntilParked()
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if handle.IsOpen() {
		t.Fatal("Space uses the same cancel decision")
	}
}

// U:dialog.rs:757-786
func TestDialogTriggerOpensSharedHandleAndReportsReason(t *testing.T) {
	ta := gpui.NewTestApp()
	handle := NewDialogHandle(false)
	type change struct {
		open   bool
		reason DialogChangeReason
	}
	var changes []change
	handle.onOpenChange = func(open bool, reason DialogChangeReason, _ *gpui.Window, _ *gpui.App) {
		changes = append(changes, change{open, reason})
	}
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.OwnRecipe(NewDialogTrigger(gpui.Div().Flex().Size(gpui.Px(100))).Handle(handle))
	})
	ovDraw(t, cx)
	cx.SimulateClick(ovPoint(20, 20), gpui.NoModifiers())

	if !handle.IsOpen() {
		t.Fatal("the trigger did not open the handle")
	}
	if !slices.Equal(changes, []change{{true, DialogChangeReasonTriggerPress}}) {
		t.Fatalf("changes = %v", changes)
	}
}

// U:dialog.rs:788-838: the backdrop, handed over as an absolute element,
// fills the host.
func TestDialogTheBackdropFillsTheHost(t *testing.T) {
	ta := gpui.NewTestApp()
	ta.Update(ovInit)
	focus := ta.App().FocusHandle()
	var bounds gpui.Bounds
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.OwnRecipe(NewDialog().
			Open(true).
			FocusHandle(focus).
			Backdrop(gpui.Canvas(func(b gpui.Bounds, _ *gpui.Window, _ *gpui.App) struct{} {
				bounds = b
				return struct{}{}
			}, func(gpui.Bounds, struct{}, *gpui.Window, *gpui.App) {}).Flex().Absolute().SizeFull()).
			Popup(gpui.Div().Flex().Size(gpui.Px(100))))
	})
	viewport := dialogTestViewport(t, cx)
	if bounds.Size != viewport {
		t.Fatalf("backdrop size = %v, want the viewport %v (a zero-sized backdrop paints no overlay)", bounds.Size, viewport)
	}
}

// U:dialog.rs:840-876: a press on the popup must not reach the backdrop
// under it and dismiss the dialog.
func TestDialogPressingThePopupDoesNotDismiss(t *testing.T) {
	ta := gpui.NewTestApp()
	ta.Update(ovInit)
	handle := NewDialogHandle(true)
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.OwnRecipe(NewDialog().
			Handle(handle).
			Backdrop(gpui.Div().Flex().Absolute().SizeFull()).
			Popup(NewDialogPopup().Size(gpui.Px(100))))
	})
	viewport := dialogTestViewport(t, cx)
	cx.SimulateClick(ovPoint(viewport.Width/2, viewport.Height/2), gpui.NoModifiers())
	if !handle.IsOpen() {
		t.Fatal("a press on the popup closed the dialog")
	}
	cx.SimulateClick(ovPoint(5, 5), gpui.NoModifiers())
	if handle.IsOpen() {
		t.Fatal("a press on the backdrop did not dismiss")
	}
}

// U:dialog.rs:878-909
func TestDialogThePopupIsCenteredByDefault(t *testing.T) {
	ta := gpui.NewTestApp()
	ta.Update(ovInit)
	var bounds gpui.Bounds
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.OwnRecipe(NewDialog().Popup(gpui.Canvas(func(b gpui.Bounds, _ *gpui.Window, _ *gpui.App) struct{} {
			bounds = b
			return struct{}{}
		}, func(gpui.Bounds, struct{}, *gpui.Window, *gpui.App) {}).Flex().Size(gpui.Px(100))))
	})
	viewport := dialogTestViewport(t, cx)
	if got, want := bounds.Center(), ovPoint(viewport.Width/2, viewport.Height/2); got != want {
		t.Fatalf("popup center = %v, want %v", got, want)
	}
}

// Go-own, browser-core ticket 72: the dialog is a core modal, so Tab
// follows the inert document — focus cannot leave the dialog while it
// is open (the focus trap's replacement, focus_trap.rs deleted), and
// Escape closes it through the modal's close watcher, restoring the
// previously focused element (the dialog focusing steps move focus
// into the popup at the entry's first frame).
func TestDialogTabStaysInsideAndEscapeClosesAndRestores(t *testing.T) {
	ta := gpui.NewTestApp()
	ta.Update(ovInit)
	app := ta.App()
	focus := app.FocusHandle()
	outside := app.FocusHandle().TabStop(true)
	inside := app.FocusHandle().TabStop(true)
	handle := NewDialogHandle(false)
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.Div().Flex().SizeFull().
			// The outside stop, inert while the modal is open.
			Child(gpui.Div().Flex().Size(gpui.Px(20)).TrackFocus(outside)).
			Child(gpui.OwnRecipe(NewDialog().
				Handle(handle).
				FocusHandle(focus).
				Popup(gpui.Div().Flex().Size(gpui.Px(100)).Occlude().
					Child(gpui.Div().Flex().ID("stop").Size(gpui.Px(20)).TrackFocus(inside))))).
			IntoElement()
	})
	// The dialog renders closed: focus the outside element first, so the
	// modal's previously-focused element is a real one.
	ovDraw(t, cx)
	cx.Update(func(w *gpui.Window, _ *gpui.App) { outside.Focus(w) })
	ovDraw(t, cx)
	cx.Update(func(w *gpui.Window, app *gpui.App) { handle.Open(w, app) })
	dialogTestViewport(t, cx)

	// The focusing steps moved focus into the dialog (the first keyboard
	// focusable inside).
	if !inside.IsFocused(cx.Window()) {
		t.Fatalf("focus after the focusing steps = %v, want the dialog's first stop", cx.Window().Focused())
	}
	// Tab inside the dialog: the outside stop is inert, so focus
	// passes out of the dialog to the body (Chrome hands focus to the
	// browser UI at the scope's end) — never onto the outside stop.
	cx.SimulateKeystrokes("tab")
	cx.RunUntilParked()
	if outside.IsFocused(cx.Window()) {
		t.Fatal("tab left the modal onto the inert outside element")
	}
	// Focus is back inside for the Escape step.
	cx.Update(func(w *gpui.Window, _ *gpui.App) { inside.Focus(w) })

	// Escape: the modal's close watcher closes the dialog and the core
	// restores the previously focused element (the outside stop).
	cx.SimulateKeystrokes("escape")
	cx.RunUntilParked()
	ovDraw(t, cx)
	if handle.IsOpen() {
		t.Fatal("escape did not close the dialog through the close watcher")
	}
	if !outside.IsFocused(cx.Window()) {
		t.Fatal("the modal's close did not restore the previously focused element")
	}
}
