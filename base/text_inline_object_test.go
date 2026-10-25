package base

import (
	"math"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ports of the U:text/inline_object.rs tests (410-485): 3 of 3; 2 ported,
// 1 deviated (the accessible name is written as the node Value: gpui's
// Node has no label).

// U:text/inline_object.rs:415-438
func TestNativePaddingAndFixedSizeAreMeasuredWithoutFakeScaling(t *testing.T) {
	inPrepaint(t, func(w *gpui.Window, app *gpui.App) struct{} {
		native := NewInlineElement(gpui.Div().Flex().Px(gpui.Px(4)).Py(gpui.Px(2)).Child(gpui.Div().Flex().W(gpui.Px(40)).H(gpui.Px(10)))).WithBaseline(10)
		measured := measureInlineObject("badge", &native, gpui.DefaultTextStyle(), w, app)
		if measured.metrics.size != (gpui.Size{Width: 48, Height: 14}) || measured.metrics.baseline != 10 {
			t.Fatalf("metrics %+v", measured.metrics)
		}
		width := float32(20)
		if narrow := measured.fitText(&width); narrow.metrics.size != (gpui.Size{Width: 48, Height: 14}) {
			t.Fatalf("a native element was scaled: %+v", narrow.metrics)
		}
		return struct{}{}
	})
}

// U:text/inline_object.rs:440-461
func TestInvalidBaselineUsesTextFallbackAndZeroWidthRemainsFinite(t *testing.T) {
	inPrepaint(t, func(w *gpui.Window, app *gpui.App) struct{} {
		for _, baseline := range []float32{float32(math.NaN()), -1, 30} {
			native := NewInlineElement(gpui.Div().Flex().W(gpui.Px(20)).H(gpui.Px(20))).WithBaseline(baseline)
			measured := measureInlineObject("fallback", &native, gpui.DefaultTextStyle(), w, app)
			if measured.content != nil || !measured.metrics.isValid() {
				t.Fatalf("baseline %v: content %v metrics %+v", baseline, measured.content != nil, measured.metrics)
			}
			zeroWidth := float32(0)
			zero := measured.fitText(&zeroWidth)
			if zero.metrics.size != (gpui.Size{}) || zero.metrics.baseline != 0 {
				t.Fatalf("baseline %v: zero fit %+v", baseline, zero.metrics)
			}
		}
		return struct{}{}
	})
}

// U:text/inline_object.rs:463-484 (deviated: the name is the Value).
func TestAtomicWrapperDoesNotMislabelNativeControlsAsImages(t *testing.T) {
	inPrepaint(t, func(w *gpui.Window, app *gpui.App) struct{} {
		measured := measureInlineObject("member", nil, gpui.DefaultTextStyle(), w, app)
		object := newInlineObject(gpui.NameElementID("member"), "member", "Member profile", measured, &sharedFlag{}, gpui.Bounds{}, gpui.Bounds{})
		var node gpui.Node
		object.WriteA11yInfo(&node)
		if role, ok := object.A11yRole(); !ok || role != gpui.RoleGenericContainer || node.Role != gpui.RoleGenericContainer || node.Value != "Member profile" {
			t.Fatalf("node %+v", node)
		}
		return struct{}{}
	})
}
