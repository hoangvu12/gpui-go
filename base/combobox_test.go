package base

import (
	"slices"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ported tests of U:combobox.rs (2/2).

// comboboxHarness is U:combobox.rs:152-200 Harness.
type comboboxHarness struct {
	open         bool
	triggerFocus gpui.FocusHandle
	contentFocus gpui.FocusHandle
	events       []string
}

func (h *comboboxHarness) Render(_ *gpui.Window, cx *gpui.Context[comboboxHarness]) gpui.AnyElement {
	state := cx.Entity().Downgrade()
	record := func(app *gpui.App, event string, open *bool) {
		_ = state.TryUpdate(app, func(h *comboboxHarness, cx *gpui.Context[comboboxHarness]) {
			h.events = append(h.events, event)
			if open != nil {
				h.open = *open
				cx.Notify()
			}
		})
	}
	return gpui.OwnRecipe(NewCombobox("combobox").
		Open(h.open).
		FocusHandle(h.triggerFocus).
		ContentFocusHandle(h.contentFocus).
		OnConfirm(func(_ *gpui.Window, app *gpui.App) { record(app, "confirm", nil) }).
		OnDismiss(func(_ *gpui.Window, app *gpui.App) { record(app, "dismiss", nil) }).
		OnOpenChange(func(open bool, _ *gpui.Window, app *gpui.App) { record(app, "close", &open) }).
		Child(gpui.Div().Flex().TrackFocus(h.contentFocus).Size(gpui.Px(20))))
}

// comboboxTestHarness is U:combobox.rs:202-211: open, with the content
// focused.
func comboboxTestHarness(t *testing.T) (*gpui.VisualTestContext, gpui.Entity[comboboxHarness]) {
	t.Helper()
	ta := gpui.NewTestApp()
	ta.Update(Init)
	state, cx := ta.AddWindowView(func(h *comboboxHarness, _ *gpui.Window, cx *gpui.Context[comboboxHarness]) {
		h.open = true
		h.triggerFocus = cx.App().FocusHandle()
		h.contentFocus = cx.App().FocusHandle()
	})
	cx.Update(func(w *gpui.Window, app *gpui.App) {
		state.Read(app, func(h *comboboxHarness, _ *gpui.App) struct{} { h.contentFocus.Focus(w); return struct{}{} })
		if _, err := gpui.DrawWindowFrame(w); err != nil {
			t.Fatal(err)
		}
	})
	return cx, state
}

func readCombobox(cx *gpui.VisualTestContext, state gpui.Entity[comboboxHarness]) comboboxHarness {
	var h comboboxHarness
	cx.Update(func(_ *gpui.Window, app *gpui.App) {
		h = state.Read(app, func(h *comboboxHarness, _ *gpui.App) comboboxHarness { return *h })
	})
	return h
}

// U:combobox.rs:213-229
func TestComboboxEscapeDismissesThenClosesAndRestoresTriggerFocus(t *testing.T) {
	cx, state := comboboxTestHarness(t)
	cx.SimulateKeystrokes("escape")
	h := readCombobox(cx, state)
	if h.open || !isFocused(cx, h.triggerFocus) {
		t.Fatalf("open = %v, trigger focused = %v", h.open, isFocused(cx, h.triggerFocus))
	}
	// Dismissal runs before the close request so a caller can still
	// commit its pending value.
	if !slices.Equal(h.events, []string{"dismiss", "close"}) {
		t.Fatalf("events = %v, want [dismiss close]", h.events)
	}
}

// U:combobox.rs:231-244
func TestComboboxEnterConfirmsWithoutDismissingWhileOpen(t *testing.T) {
	cx, state := comboboxTestHarness(t)
	cx.SimulateKeystrokes("enter")
	h := readCombobox(cx, state)
	if !h.open || !slices.Equal(h.events, []string{"confirm"}) {
		t.Fatalf("open = %v, events = %v", h.open, h.events)
	}
}
