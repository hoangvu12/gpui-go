package base

import (
	"math"
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ports of U:slider.rs tests (4/4) plus one Go-own interaction test
// (marked): upstream has no window test for the slider parts.

// U:slider.rs:797-806
func TestSliderLegacyValueConversionsAndClampingArePreserved(t *testing.T) {
	if SliderValueSingle(5) != (SliderValue{start: 5, end: 5}) {
		t.Fatal("From<f32>")
	}
	if got := SliderValueRange(2, 8); !got.IsRange() || got.Start() != 2 || got.End() != 8 {
		t.Fatalf("From<(f32, f32)> = %v", got)
	}
	if got := SliderValueRange(-1, 12).Clamp(0, 10); got != SliderValueRange(0, 10) {
		t.Fatalf("clamp = %v, want 0..10", got)
	}
}

// U:slider.rs:808-816
func TestSliderLegacyLinearStateKeepsPercentageAndRangeOrdering(t *testing.T) {
	state := NewSliderState().Min(0).Max(200).DefaultValue(SliderValueRange(50, 150))
	if state.Value() != SliderValueRange(50, 150) {
		t.Fatalf("value = %v", state.Value())
	}
	if start, end := state.Percentage(); start != 0.25 || end != 0.75 {
		t.Fatalf("percentage = %v..%v, want 0.25..0.75", start, end)
	}
}

// U:slider.rs:818-827
func TestSliderLegacyLogarithmicStateKeepsMapping(t *testing.T) {
	state := NewSliderState().Min(1).Max(1000).Scale(SliderScaleLogarithmic).DefaultValue(SliderValueSingle(10))
	if _, end := state.Percentage(); math.Abs(float64(end)-1.0/3.0) >= 0.0001 {
		t.Fatalf("percentage end = %v, want ~1/3", end)
	}
}

// U:slider.rs:829-833
func TestSliderLegacyLogarithmicValidationIsPreserved(t *testing.T) {
	defer func() {
		r := recover()
		if msg, _ := r.(string); !strings.Contains(msg, "`min` must be greater than 0") {
			t.Fatalf("panic = %v", r)
		}
	}()
	_ = NewSliderState().Scale(SliderScaleLogarithmic)
}

// Go-own: a press on the track maps through the indicator's prepainted
// bounds (U:slider.rs:577-610, 674-681), the release ends the drag
// (U:slider.rs:507-516, 383-390), and the root projects the slider's
// numeric a11y and increment/decrement actions (U:slider.rs:477-506).
func TestSliderTrackPressMapsThroughIndicatorBounds(t *testing.T) {
	ta := gpui.NewTestApp()
	var state gpui.Entity[SliderState]
	ta.Update(func(app *gpui.App) {
		state = gpui.NewEntity(app, ta.RootScope(), func(s *SliderState, _ *gpui.Context[SliderState]) {
			*s = NewSliderState()
		})
	})
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return elementOf(t, NewSlider(state).Child(
			NewSliderTrack(state).W(gpui.Px(100)).H(gpui.Px(10)).Child(
				NewSliderIndicator(state).SizeFull())))
	})
	redraw(t, cx)

	clickAt(cx, 50, 5)
	var value SliderValue
	var dragging bool
	cx.Update(func(_ *gpui.Window, app *gpui.App) {
		state.Read(app, func(s *SliderState, _ *gpui.App) struct{} {
			value, dragging = s.Value(), s.dragging
			return struct{}{}
		})
	})
	if value != SliderValueSingle(50) {
		t.Fatalf("value = %v, want 50", value)
	}
	if dragging {
		t.Fatal("release did not end the drag")
	}

	var p a11yProbe
	cx.Update(func(w *gpui.Window, app *gpui.App) {
		d := NewSlider(state).render(w, app)
		p.role, p.hasRole = d.A11yRole()
		d.WriteA11yInfo(&p.spec)
	})
	if p.role != gpui.RoleSlider {
		t.Fatalf("role = %v", p.role)
	}
	progressWantFloat(t, "numeric", p.spec.NumericValue, 50)
	progressWantFloat(t, "step", p.spec.NumericValueStep, 1)
	if !hasAction(p.spec, gpui.A11yActionIncrement) || !hasAction(p.spec, gpui.A11yActionDecrement) {
		t.Fatalf("actions = %v", p.spec.Actions)
	}
}
