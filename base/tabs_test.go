package base

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ports of U:tabs.rs tests (5/5).

func tabsHarness(t *testing.T, disabled bool) (*gpui.VisualTestContext, *int) {
	t.Helper()
	clicks := new(int)
	cx := drawn(t, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return elementOf(t, NewTab("tab").
			Disabled(disabled).
			Size(gpui.Px(100)).
			OnClick(func(*gpui.ClickEvent, *gpui.Window, *gpui.App) { *clicks++ }))
	})
	return cx, clicks
}

// U:tabs.rs:279-288
func TestTabsPointerActivationAndDisabledGatingMatchTabs(t *testing.T) {
	cx, clicks := tabsHarness(t, false)
	clickAt(cx, 10, 10)
	if *clicks != 1 {
		t.Fatalf("enabled clicks = %d, want 1", *clicks)
	}
	cx, clicks = tabsHarness(t, true)
	clickAt(cx, 10, 10)
	if *clicks != 0 {
		t.Fatalf("disabled clicks = %d, want 0", *clicks)
	}
}

// U:tabs.rs:290-333
func TestTabsFixedHeightTabCentersOrdinaryChildGeometry(t *testing.T) {
	var root, child gpui.Bounds
	drawn(t, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		tab := ElementExtOf(NewTab("alignment-tab").W(gpui.Px(120)).H(gpui.Px(40)).
			Child(ElementExtOf(gpui.Div().Flex().W(gpui.Px(48)).H(gpui.Px(12))).
				OnPrepaint(func(b gpui.Bounds, _ *gpui.Window, _ *gpui.App) { child = b }))).
			OnPrepaint(func(b gpui.Bounds, _ *gpui.Window, _ *gpui.App) { root = b })
		return elementOf(t, tab)
	})
	if root.Size.Width == 0 || child.Size.Width == 0 {
		t.Fatalf("bounds not captured: root %v child %v", root, child)
	}
	if child.Center() != root.Center() {
		t.Fatalf("child center %v != tab center %v", child.Center(), root.Center())
	}
}

// U:tabs.rs:335-379
func TestTabsExposesTabAccessibilityState(t *testing.T) {
	got := a11yOf(t,
		func(w *gpui.Window, app *gpui.App) *gpui.DivElement {
			return NewTab("selected").Selected(true).AccessibilityLabel("Account").
				OnClick(func(*gpui.ClickEvent, *gpui.Window, *gpui.App) {}).render(w, app)
		},
		func(w *gpui.Window, app *gpui.App) *gpui.DivElement {
			return NewTab("disabled").Disabled(true).
				OnClick(func(*gpui.ClickEvent, *gpui.Window, *gpui.App) {}).render(w, app)
		},
	)
	selected, disabled := got[0], got[1]
	if selected.role != gpui.RoleTab || selected.spec.Label != "Account" {
		t.Fatalf("selected = %+v", selected)
	}
	if v, ok := selected.spec.Selected.Get(); !ok || !v {
		t.Fatalf("selected state = %v (set %v), want Some(true)", v, ok)
	}
	if !hasAction(selected.spec, gpui.A11yActionClick) || hasAction(disabled.spec, gpui.A11yActionClick) {
		t.Fatalf("click actions: selected %v disabled %v", selected.spec.Actions, disabled.spec.Actions)
	}
}

// U:tabs.rs:381-397
func TestTabsSemanticStylesPreserveTheLegacyStatePriority(t *testing.T) {
	expected := gpui.Hsla{H: 0.3, S: 0.4, L: 0.5, A: 1}
	tab := NewTab("tab").
		Selected(true).
		Disabled(true).
		Styles(func(s TabStyles) TabStyles {
			return s.
				Selected(func(style *StateStyle) *StateStyle { return style.Opacity(0.8) }).
				Disabled(func(style *StateStyle) *StateStyle { return style.Opacity(0.5) })
		}).
		Opacity(0.9)
	if got := opacityOf(t, tab.resolvedStyle()); got != 0.5 {
		t.Fatalf("opacity = %v, want 0.5", got)
	}
	tab.StyleMut().Background = gpui.Some(gpui.BackgroundOf(expected))
	wantBackground(t, "instance bg", tab.resolvedStyle(), &expected)
}

// U:tabs.rs:399-426
func TestTabsExposesTabListRole(t *testing.T) {
	got := a11yOf(t, func(w *gpui.Window, app *gpui.App) *gpui.DivElement {
		return NewTabs("tabs").render(w, app)
	})
	if got[0].role != gpui.RoleTabList {
		t.Fatalf("role = %v, want TabList", got[0].role)
	}
}
