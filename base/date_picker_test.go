package base

import (
	"slices"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// U:date_picker.rs has no tests. Go-own coverage of its render.

type datePickerHarness struct {
	open    bool
	focus   gpui.FocusHandle
	changes *[]bool
}

func (h *datePickerHarness) Render(_ *gpui.Window, cx *gpui.Context[datePickerHarness]) gpui.AnyElement {
	state := cx.Entity().Downgrade()
	changes := h.changes
	return gpui.OwnRecipe(NewDatePicker("date", h.focus).
		Open(h.open).
		Size(gpui.Px(20)).
		OnOpenChange(func(open bool, _ *gpui.Window, app *gpui.App) {
			*changes = append(*changes, open)
			_ = state.TryUpdate(app, func(h *datePickerHarness, cx *gpui.Context[datePickerHarness]) {
				h.open = open
				cx.Notify()
			})
		}))
}

// Go-own: Confirm toggles the picker, Cancel closes an open one and
// propagates from a closed one (U:date_picker.rs:78-111).
func TestDatePickerConfirmTogglesAndCancelCloses(t *testing.T) {
	ta := gpui.NewTestApp()
	ta.Update(Init)
	changes := new([]bool)
	state, cx := ta.AddWindowView(func(h *datePickerHarness, _ *gpui.Window, cx *gpui.Context[datePickerHarness]) {
		h.focus = cx.App().FocusHandle()
		h.changes = changes
	})
	cx.Update(func(w *gpui.Window, app *gpui.App) {
		state.Read(app, func(h *datePickerHarness, _ *gpui.App) struct{} { h.focus.Focus(w); return struct{}{} })
		if _, err := gpui.DrawWindowFrame(w); err != nil {
			t.Fatal(err)
		}
	})
	cx.DispatchAction(ConfirmAction.Box(Confirm{}))
	cx.DispatchAction(ConfirmAction.Box(Confirm{}))
	cx.DispatchAction(CancelAction.Box(Cancel{}))
	cx.DispatchAction(ConfirmAction.Box(Confirm{}))
	cx.DispatchAction(CancelAction.Box(Cancel{}))
	if !slices.Equal(*changes, []bool{true, false, true, false}) {
		t.Fatalf("changes = %v, want [true false true false]", *changes)
	}
	got := a11yOf(t, func(w *gpui.Window, app *gpui.App) *gpui.DivElement {
		return NewDatePicker("probe", app.FocusHandle()).Open(true).render(w, app)
	})
	if got[0].role != gpui.RoleComboBox {
		t.Fatalf("role = %v, want ComboBox", got[0].role)
	}
	if v, ok := got[0].spec.Expanded.Get(); !ok || !v {
		t.Fatal("open picker is not expanded")
	}
}
