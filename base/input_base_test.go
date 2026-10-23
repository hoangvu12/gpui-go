package base

import (
	"reflect"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ported tests of U:input/base/mod.rs (2/2).

// U:input/base/mod.rs:248-260
func TestInputBaseFrameAcceptsApplicationOwnedContentAndStyle(t *testing.T) {
	_ = NewInputBase("input").
		Focused(true).
		Disabled(false).
		Styles(func(s InputStyles) InputStyles {
			return s.
				Focused(func(style *StateStyle) *StateStyle { return style.Border1() }).
				Disabled(func(style *StateStyle) *StateStyle { return style.Opacity(0.5) })
		}).
		Child("value").
		Opacity(0.8)
}

// U:input/base/mod.rs:262-280
func TestInputBaseSemanticStateStylesOverrideTheNormalStyle(t *testing.T) {
	red, blue := gpui.Red(), inputTestBlue()
	focused := NewInputBase("focused").
		Focused(true).
		BorderColor(red).
		Styles(func(s InputStyles) InputStyles {
			return s.Focused(func(style *StateStyle) *StateStyle { return style.BorderColor(blue) })
		})
	if got, ok := focused.resolvedStyle().BorderColor.Get(); !ok || !reflect.DeepEqual(got, gpui.BackgroundOf(blue)) {
		t.Fatalf("border color = %v (set %v), want blue", got, ok)
	}

	disabled := NewInputBase("disabled").
		Focused(true).
		Disabled(true).
		Opacity(1).
		Styles(func(s InputStyles) InputStyles {
			return s.
				Focused(func(style *StateStyle) *StateStyle { return style.Opacity(0.8) }).
				Disabled(func(style *StateStyle) *StateStyle { return style.Opacity(0.5) })
		})
	if got := opacityOf(t, disabled.resolvedStyle()); got != 0.5 {
		t.Fatalf("opacity = %v, want 0.5", got)
	}
}
