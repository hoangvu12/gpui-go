package base

import (
	"slices"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ported tests of U:select.rs (6/6).

// selectHarness is U:select.rs:316-371 SelectHarness.
type selectHarness struct {
	open               bool
	disabled           bool
	focusHandle        gpui.FocusHandle
	contentFocusHandle gpui.FocusHandle
	changes            []bool
	// closing is every step of a close, in the order it ran.
	closing []string
}

func (h *selectHarness) Render(_ *gpui.Window, cx *gpui.Context[selectHarness]) gpui.AnyElement {
	state := cx.Entity().Downgrade()
	return gpui.OwnRecipe(NewSelect("select").
		Open(h.open).
		Disabled(h.disabled).
		FocusHandle(h.focusHandle).
		ContentFocusHandle(h.contentFocusHandle).
		OnOpenChange(func(open bool, _ *gpui.Window, app *gpui.App) {
			_ = state.TryUpdate(app, func(h *selectHarness, cx *gpui.Context[selectHarness]) {
				h.changes = append(h.changes, open)
				if open {
					h.closing = append(h.closing, "open")
				} else {
					h.closing = append(h.closing, "close")
				}
				h.open = open
				cx.Notify()
			})
		}).
		OnDismiss(func(_ *gpui.Window, app *gpui.App) {
			_ = state.TryUpdate(app, func(h *selectHarness, _ *gpui.Context[selectHarness]) {
				h.closing = append(h.closing, "dismiss")
			})
		}).
		Child(gpui.Div().Flex().TrackFocus(h.contentFocusHandle).Size(gpui.Px(20))))
}

// selectTestHarness is U:select.rs:373-384 harness: the trigger starts
// focused and the window is drawn.
func selectTestHarness(t *testing.T, disabled bool) (*gpui.VisualTestContext, gpui.Entity[selectHarness]) {
	t.Helper()
	ta := gpui.NewTestApp()
	ta.Update(Init)
	state, cx := ta.AddWindowView(func(h *selectHarness, _ *gpui.Window, cx *gpui.Context[selectHarness]) {
		h.disabled = disabled
		h.focusHandle = cx.App().FocusHandle()
		h.contentFocusHandle = cx.App().FocusHandle()
	})
	cx.Update(func(w *gpui.Window, app *gpui.App) {
		state.Read(app, func(h *selectHarness, _ *gpui.App) struct{} { h.focusHandle.Focus(w); return struct{}{} })
		if _, err := gpui.DrawWindowFrame(w); err != nil {
			t.Fatal(err)
		}
	})
	return cx, state
}

func readSelect(cx *gpui.VisualTestContext, state gpui.Entity[selectHarness]) selectHarness {
	var h selectHarness
	cx.Update(func(_ *gpui.Window, app *gpui.App) {
		h = state.Read(app, func(h *selectHarness, _ *gpui.App) selectHarness { return *h })
	})
	return h
}

func isFocused(cx *gpui.VisualTestContext, handle gpui.FocusHandle) bool {
	var ok bool
	cx.Update(func(w *gpui.Window, _ *gpui.App) { ok = handle.IsFocused(w) })
	return ok
}

// U:select.rs:386-402
func TestSelectArrowsOpenAndTransferFocusToContent(t *testing.T) {
	cx, state := selectTestHarness(t, false)
	cx.SimulateKeystrokes("down")
	h := readSelect(cx, state)
	if !h.open || !isFocused(cx, h.contentFocusHandle) {
		t.Fatalf("open = %v, content focused = %v", h.open, isFocused(cx, h.contentFocusHandle))
	}
	if !slices.Equal(h.changes, []bool{true}) {
		t.Fatalf("changes = %v, want [true]", h.changes)
	}
}

// U:select.rs:404-413
func TestSelectConfirmOpensAClosedSelect(t *testing.T) {
	cx, state := selectTestHarness(t, false)
	cx.SimulateKeystrokes("enter")
	h := readSelect(cx, state)
	if !h.open || !isFocused(cx, h.contentFocusHandle) {
		t.Fatalf("open = %v, content focused = %v", h.open, isFocused(cx, h.contentFocusHandle))
	}
}

// U:select.rs:415-431
func TestSelectEscapeClosesAndRestoresTriggerFocus(t *testing.T) {
	cx, state := selectTestHarness(t, false)
	cx.SimulateKeystrokes("down escape")
	h := readSelect(cx, state)
	if h.open || !isFocused(cx, h.focusHandle) {
		t.Fatalf("open = %v, trigger focused = %v", h.open, isFocused(cx, h.focusHandle))
	}
	if !slices.Equal(h.changes, []bool{true, false}) {
		t.Fatalf("changes = %v, want [true false]", h.changes)
	}
}

// U:select.rs:433-456: closing runs on_dismiss before the open state is
// asked to close.
func TestSelectEveryCloseDismissesBeforeItCloses(t *testing.T) {
	cx, state := selectTestHarness(t, false)
	cx.SimulateKeystrokes("down escape")
	if got := readSelect(cx, state).closing; !slices.Equal(got, []string{"open", "dismiss", "close"}) {
		t.Fatalf("closing = %v, want [open dismiss close]", got)
	}
}

// U:select.rs:458-471
func TestSelectDisabledSelectIsNotKeyboardInteractive(t *testing.T) {
	cx, state := selectTestHarness(t, true)
	cx.SimulateKeystrokes("down enter")
	h := readSelect(cx, state)
	if h.open || len(h.changes) != 0 {
		t.Fatalf("open = %v, changes = %v", h.open, h.changes)
	}
}

// U:select.rs:473-506
func TestSelectProjectsApplicationOwnedAccessibleState(t *testing.T) {
	got := a11yOf(t,
		func(w *gpui.Window, app *gpui.App) *gpui.DivElement {
			return NewSelect("enabled").
				Open(true).
				AccessibilityLabel("Programming language").
				AccessibilityValue("Rust").
				render(w, app)
		},
		// Open, so the expanded assertion says something about disabled
		// rather than about the default open state.
		func(w *gpui.Window, app *gpui.App) *gpui.DivElement {
			return NewSelect("disabled").Open(true).Disabled(true).render(w, app)
		},
	)
	enabled, disabled := got[0].spec, got[1].spec
	if enabled.Label != "Programming language" || enabled.Value != "Rust" {
		t.Fatalf("label = %q, value = %q", enabled.Label, enabled.Value)
	}
	if v, ok := enabled.Expanded.Get(); !ok || !v {
		t.Fatal("enabled select is not expanded")
	}
	if v, ok := disabled.Expanded.Get(); !ok || !v {
		t.Fatal("a disabled control still reports the state it is in")
	}
	if !hasAction(enabled, gpui.A11yActionClick) {
		t.Fatal("enabled select does not advertise Click")
	}
	if hasAction(disabled, gpui.A11yActionClick) {
		t.Fatal("disabled select advertises Click")
	}
	if got[0].role != gpui.RoleComboBox {
		t.Fatalf("role = %v, want ComboBox", got[0].role)
	}
}
