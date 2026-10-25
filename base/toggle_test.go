package base

import (
	"slices"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ports of U:toggle.rs tests (7/7).

type toggleRecord struct {
	changes  []bool
	keyboard int
}

func toggleHarness(t *testing.T, pressed, disabled bool) (*gpui.VisualTestContext, *toggleRecord) {
	t.Helper()
	rec := &toggleRecord{}
	cx := drawn(t, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return elementOf(t, NewToggle("toggle").
			Pressed(pressed).
			Disabled(disabled).
			Size(gpui.Px(100)).
			OnChange(func(next bool, event *gpui.ClickEvent, _ *gpui.Window, _ *gpui.App) {
				rec.changes = append(rec.changes, next)
				if event.IsKeyboard() {
					rec.keyboard++
				}
			}))
	})
	return cx, rec
}

// U:toggle.rs:278-285
func TestTogglePointerRequestsInverseControlledStateOnce(t *testing.T) {
	for _, c := range []struct{ pressed, want bool }{{false, true}, {true, false}} {
		cx, rec := toggleHarness(t, c.pressed, false)
		clickAt(cx, 10, 10)
		if !slices.Equal(rec.changes, []bool{c.want}) {
			t.Fatalf("pressed=%v: changes = %v, want [%v]", c.pressed, rec.changes, c.want)
		}
	}
}

// U:toggle.rs:287-305
func TestToggleEnterAndSpaceUseOneNativeKeyboardClickEach(t *testing.T) {
	cx, rec := toggleHarness(t, false, false)
	clickAt(cx, 10, 10)
	rec.changes = nil
	redraw(t, cx)
	pressKeys(cx, "enter", "space")
	if !slices.Equal(rec.changes, []bool{true, true}) || rec.keyboard != 2 {
		t.Fatalf("changes = %v keyboard = %d; want [true true] 2", rec.changes, rec.keyboard)
	}
}

// U:toggle.rs:307-313
func TestToggleDisabledIsInert(t *testing.T) {
	cx, rec := toggleHarness(t, false, true)
	clickAt(cx, 10, 10)
	cx.SimulateKeystrokes("enter space")
	if len(rec.changes) != 0 {
		t.Fatalf("changes = %v, want none", rec.changes)
	}
}

// U:toggle.rs:315-361
func TestToggleFixedHeightCentersOrdinaryChildGeometry(t *testing.T) {
	var root, child gpui.Bounds
	var haveRoot, haveChild bool
	drawn(t, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return elementOf(t, ElementExtOf(NewToggle("alignment-toggle").
			W(gpui.Px(120)).
			H(gpui.Px(40)).
			Child(ElementExtOf(gpui.Div().Flex().W(gpui.Px(48)).H(gpui.Px(12))).
				OnPrepaint(func(b gpui.Bounds, _ *gpui.Window, _ *gpui.App) { child, haveChild = b, true }))).
			OnPrepaint(func(b gpui.Bounds, _ *gpui.Window, _ *gpui.App) { root, haveRoot = b, true }))
	})
	if !haveRoot || !haveChild {
		t.Fatalf("bounds captured: root %v child %v", haveRoot, haveChild)
	}
	if child.Center() != root.Center() {
		t.Fatalf("child center %v != toggle center %v", child.Center(), root.Center())
	}
}

// U:toggle.rs:363-375
func TestToggleStateStylingAndChildrenAreApplicationOwned(t *testing.T) {
	_ = NewToggle("styled").
		Child("Label").
		Styles(func(s ToggleStyles) ToggleStyles {
			return s.
				Pressed(func(style *StateStyle) *StateStyle { return style.Opacity(0.8) }).
				Disabled(func(style *StateStyle) *StateStyle { return style.Opacity(0.5) })
		}).
		Hover(gpui.Styles().Opacity(0.9)).
		Active(gpui.Styles().Opacity(0.8)).
		FocusVisible(gpui.Styles().Opacity(0.7))
}

// U:toggle.rs:377-411
func TestToggleSemanticRootStylesFollowTogglePriority(t *testing.T) {
	styled := func(toggle *Toggle) *Toggle {
		return toggle.Styles(func(s ToggleStyles) ToggleStyles {
			return s.
				Pressed(func(style *StateStyle) *StateStyle { return style.Opacity(0.8) }).
				Disabled(func(style *StateStyle) *StateStyle { return style.Opacity(0.5) })
		})
	}
	if styled(NewToggle("normal")).resolvedStyle().Opacity.IsSome() {
		t.Fatal("normal toggle has an opacity")
	}
	if got := opacityOf(t, styled(NewToggle("pressed").Pressed(true)).resolvedStyle()); got != 0.8 {
		t.Fatalf("pressed opacity = %v, want 0.8", got)
	}
	if got := opacityOf(t, styled(NewToggle("pressed-disabled").Pressed(true).Disabled(true)).resolvedStyle()); got != 0.5 {
		t.Fatalf("pressed-disabled opacity = %v, want 0.5", got)
	}
	if got := opacityOf(t, styled(NewToggle("state-over-instance").Pressed(true).Disabled(true).Opacity(0.9)).resolvedStyle()); got != 0.5 {
		t.Fatalf("state-over-instance opacity = %v, want 0.5", got)
	}
}

// U:toggle.rs:413-458
func TestToggleAccessibilityExposesButtonRoleToggledStateAndAction(t *testing.T) {
	noop := func(bool, *gpui.ClickEvent, *gpui.Window, *gpui.App) {}
	got := a11yOf(t,
		func(w *gpui.Window, app *gpui.App) *gpui.DivElement {
			return NewToggle("enabled").Pressed(true).AccessibilityLabel("Bold").OnChange(noop).render(w, app)
		},
		func(w *gpui.Window, app *gpui.App) *gpui.DivElement {
			return NewToggle("disabled").Disabled(true).OnChange(noop).render(w, app)
		},
	)
	enabled, disabled := got[0], got[1]
	if enabled.role != gpui.RoleButton || enabled.spec.Label != "Bold" || !hasAction(enabled.spec, gpui.A11yActionClick) {
		t.Fatalf("enabled = %+v", enabled)
	}
	if v, ok := enabled.spec.Toggled.Get(); !ok || v != gpui.ToggledTrue {
		t.Fatalf("enabled toggled = %v (%v)", v, ok)
	}
	if v, ok := disabled.spec.Toggled.Get(); !ok || v != gpui.ToggledFalse {
		t.Fatalf("disabled toggled = %v (%v)", v, ok)
	}
	if hasAction(disabled.spec, gpui.A11yActionClick) {
		t.Fatal("disabled toggle advertises Click")
	}
}
