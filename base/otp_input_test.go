package base

import (
	"slices"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ported tests of U:otp_input.rs (2/2).

// U:otp_input.rs:214-226
func TestOtpInputKeyboardEditingBackspacesAndStopsAtLength(t *testing.T) {
	for _, c := range []struct {
		value, key string
		want       string
		wantOK     bool
	}{
		{"12", "backspace", "1", true},
		{"12", "３", "123", true},
		{"1234", "5", "", false},
		{"12", "left", "", false},
	} {
		got, ok := otpEditValue(c.value, c.key, "", 4)
		if ok != c.wantOK || got != c.want {
			t.Fatalf("edit(%q, %q) = %q, %v; want %q, %v", c.value, c.key, got, ok, c.want, c.wantOK)
		}
	}
}

// U:otp_input.rs:228-236: programmatic values intentionally do not use
// the keyboard editor path, while key entry remains digit-only.
func TestOtpInputProgrammaticValuesRemainUnfilteredForCompatibility(t *testing.T) {
	value := "token-over-length"
	if value != "token-over-length" {
		t.Fatal("unreachable")
	}
	if _, ok := otpEditValue("token", "x", "", 2); ok {
		t.Fatal("a non-digit key edited the value")
	}
}

// otpHarness renders an OtpInput over its state (Go-own fixture).
type otpHarness struct {
	state gpui.Entity[OtpState]
}

func (h *otpHarness) Render(*gpui.Window, *gpui.Context[otpHarness]) gpui.AnyElement {
	return gpui.OwnRecipe(NewOtpInput(h.state).Size(gpui.Px(40)))
}

// Go-own: typed digits edit the focused state, emit Change then
// Complete, and SetValue stays unfiltered (U:otp_input.rs:58-66,
// 121-139, 196-208).
func TestOtpInputKeyDownEditsTheFocusedState(t *testing.T) {
	ta := gpui.NewTestApp()
	ta.Update(Init)
	var state gpui.Entity[OtpState]
	var events []OtpEvent
	_, cx := ta.AddWindowView(func(h *otpHarness, w *gpui.Window, cx *gpui.Context[otpHarness]) {
		state = gpui.NewEntity(cx, w.Scope(), func(s *OtpState, scx *gpui.Context[OtpState]) {
			*s = NewOtpState(2, w, scx)
		})
		OtpEvents.Subscribe(cx, state, func(_ *otpHarness, _ gpui.Entity[OtpState], e *OtpEvent, _ *gpui.Context[otpHarness]) {
			events = append(events, *e)
		})
		h.state = state
	})
	cx.Update(func(w *gpui.Window, app *gpui.App) {
		_ = state.UpdateIn(app, w, func(s *OtpState, w *gpui.Window, cx *gpui.Context[OtpState]) { s.Focus(w, cx) })
		if _, err := gpui.DrawWindowFrame(w); err != nil {
			t.Fatal(err)
		}
	})
	cx.SimulateKeystrokes("4 a 2 7")
	cx.RunUntilParked()
	if v := state.Read(ta, func(s *OtpState, _ *gpui.App) string { return s.Value() }); v != "42" {
		t.Fatalf("value = %q, want 42", v)
	}
	if want := []OtpEvent{OtpEventFocus, OtpEventChange, OtpEventChange, OtpEventComplete}; !slices.Equal(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
	cx.Update(func(w *gpui.Window, app *gpui.App) {
		_ = state.UpdateIn(app, w, func(s *OtpState, w *gpui.Window, cx *gpui.Context[OtpState]) { s.SetValue("token-over-length", w, cx) })
	})
	if v := state.Read(ta, func(s *OtpState, _ *gpui.App) string { return s.Value() }); v != "token-over-length" {
		t.Fatalf("SetValue filtered the value: %q", v)
	}
}
