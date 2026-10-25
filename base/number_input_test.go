package base

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ported tests of U:number_input.rs (2/2) and U:input/base/state.rs
// test_number_step (1/1, left to this ticket by ticket 20).

// initWithTheme runs the base Init with the default theme global (the
// upstream `cx.update(crate::init)` plus `cx.set_global(Theme::default())`).
func initWithTheme(app *gpui.App) {
	Init(app)
	theme := DefaultTheme()
	app.SetGlobal(&theme)
}

// stepperHarness is U:number_input.rs:384-397 StepperHarness.
type stepperHarness struct {
	state gpui.Entity[InputState]
}

func (h *stepperHarness) Render(*gpui.Window, *gpui.Context[stepperHarness]) gpui.AnyElement {
	return gpui.OwnRecipe(NewNumberInput(h.state).
		W(gpui.Px(120)).
		H(gpui.Px(20)).
		DecrementButton(func(b *Button) *Button { return b.W(gpui.Px(20)).HFull() }).
		Input(gpui.Div().Flex().SizeFull()).
		IncrementButton(func(b *Button) *Button { return b.W(gpui.Px(20)).HFull() }))
}

// U:number_input.rs:399-464: pressing a step button must not move focus
// off the editor, not even for the press itself.
func TestNumberInputPressingAStepButtonNeverTakesFocusOffTheEditor(t *testing.T) {
	ta := gpui.NewTestApp()
	ta.Update(initWithTheme)
	var state gpui.Entity[InputState]
	_, cx := ta.AddWindowView(func(h *stepperHarness, w *gpui.Window, cx *gpui.Context[stepperHarness]) {
		state = gpui.NewEntity(cx, w.Scope(), func(s *InputState, scx *gpui.Context[InputState]) {
			*s = NewInputState(w, scx)
		})
		h.state = state
	})
	focused := func() bool {
		var ok bool
		cx.Update(func(w *gpui.Window, app *gpui.App) {
			handle := state.Read(app, func(s *InputState, app *gpui.App) gpui.FocusHandle { return s.FocusHandle(app) })
			ok = handle.IsFocused(w)
		})
		return ok
	}

	cx.Update(func(w *gpui.Window, app *gpui.App) {
		_ = state.UpdateIn(app, w, func(s *InputState, w *gpui.Window, cx *gpui.Context[InputState]) { s.Focus(w, cx) })
		if _, err := gpui.DrawWindowFrame(w); err != nil {
			t.Fatal(err)
		}
	})
	if !focused() {
		t.Fatal("the editor should start focused")
	}

	// The decrement button sits at the left edge of the control.
	at := gpui.Point{X: 10, Y: 10}
	left := gpui.MouseButtonLeft
	cx.SimulateMouseMove(at, &left, gpui.Modifiers{})
	cx.SimulateEvent(&gpui.MouseDownEvent{Button: gpui.MouseButtonLeft, Position: at, ClickCount: 1})
	if !focused() {
		t.Fatal("pressing the step button pulled focus off the editor")
	}

	cx.SimulateEvent(&gpui.MouseUpEvent{Button: gpui.MouseButtonLeft, Position: at, ClickCount: 1})
	if !focused() {
		t.Fatal("releasing the step button left focus off the editor")
	}
	// Go-own: the click stepped the empty value down by the default step.
	if v := state.Read(ta, func(s *InputState, _ *gpui.App) string { return s.Value() }); v != "-1" {
		t.Fatalf("value after a decrement click = %q, want -1", v)
	}
}

// U:number_input.rs:466-488
func TestNumberInputSteppingPreservesPrecisionAndDirectionalBounds(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	for _, c := range []struct {
		value    string
		action   StepAction
		step     float64
		min, max *float64
		want     string
		wantOK   bool
	}{
		{"0.1", StepActionIncrement, 0.2, nil, nil, "0.3", true},
		{"10", StepActionDecrement, 1, f(10), nil, "", false},
		{"99.5", StepActionIncrement, 1, nil, f(100), "100.0", true},
		{"10", StepActionIncrement, 1, nil, f(10), "", false},
		{"5", StepActionDecrement, 10, f(0), nil, "0", true},
	} {
		got, ok := StepValue(c.value, c.action, c.step, c.min, c.max)
		if ok != c.wantOK || got != c.want {
			t.Fatalf("StepValue(%q, %v, %v) = %q, %v; want %q, %v", c.value, c.action, c.step, got, ok, c.want, c.wantOK)
		}
	}
}

// U:input/base/state.rs:5717-5743
func TestInputStateNumberStep(t *testing.T) {
	ta := gpui.NewTestApp()
	ta.Update(func(app *gpui.App) {
		if got := NumberStepFixed(5).Value(123, StepActionIncrement, app); got != 5 {
			t.Fatalf("fixed step = %v, want 5", got)
		}

		// The step can differ by direction at a boundary: at 1.0 it is 0.1
		// going down and 0.5 going up.
		step := NumberStepByValue(func(value float64, action StepAction, _ *gpui.App) float64 {
			below := value <= 1.0
			if action == StepActionIncrement {
				below = value < 1.0
			}
			if below {
				return 0.1
			}
			return 0.5
		})
		for _, c := range []struct {
			value  float64
			action StepAction
			want   float64
		}{
			{0.5, StepActionIncrement, 0.1},
			{1.0, StepActionIncrement, 0.5},
			{1.0, StepActionDecrement, 0.1},
			{2.0, StepActionDecrement, 0.5},
		} {
			if got := step.Value(c.value, c.action, app); got != c.want {
				t.Fatalf("step.Value(%v, %v) = %v, want %v", c.value, c.action, got, c.want)
			}
		}
	})
}

// Go-own: Rust's f64 parser rejects the hexadecimal and underscore forms
// Go's ParseFloat accepts.
func TestNumberInputParseRustF64(t *testing.T) {
	for _, s := range []string{"0x10", "1_0", "0x1p-2", "", "abc", "++1"} {
		if _, ok := parseRustF64(s); ok {
			t.Fatalf("%q parsed", s)
		}
	}
	for s, want := range map[string]float64{"1.5": 1.5, "-2": -2, ".5": 0.5, "5.": 5, "1e3": 1000, "+7": 7} {
		if got, ok := parseRustF64(s); !ok || got != want {
			t.Fatalf("%q = %v, %v; want %v", s, got, ok, want)
		}
	}
}
