package base

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ports of U:toolbar.rs tests (7/7).

// U:toolbar.rs:271-277
func TestToolbarBuilder(t *testing.T) {
	toolbar := NewToolbar("toolbar").Disabled(true).Child(gpui.Div().Flex())
	if !toolbar.disabled || toolbar.ChildCount() != 1 {
		t.Fatalf("disabled = %v, children = %d", toolbar.disabled, toolbar.ChildCount())
	}
}

// U:toolbar.rs:279-285
func TestToolbarDefaults(t *testing.T) {
	toolbar := NewToolbar("toolbar")
	if toolbar.disabled || toolbar.ChildCount() != 0 {
		t.Fatalf("disabled = %v, children = %d", toolbar.disabled, toolbar.ChildCount())
	}
}

// U:toolbar.rs:287-296
func TestToolbarGroupBuilder(t *testing.T) {
	group := NewToolbarGroup("history-group").Label("History").Child(gpui.Div().Flex()).Child(gpui.Div().Flex())
	if group.label == nil || *group.label != "History" || group.ChildCount() != 2 {
		t.Fatalf("label = %v, children = %d", group.label, group.ChildCount())
	}
}

// toolbarHarness opens a toolbar of n focusable 20px items with the first
// focused (U:toolbar.rs:307-336 NavHarness / harness).
func toolbarHarness(t *testing.T, id string, disabled bool, n int) (*gpui.VisualTestContext, []gpui.FocusHandle) {
	t.Helper()
	var items []gpui.FocusHandle
	ta := gpui.NewTestApp()
	_, cx := ta.AddWindowView(func(v *testView, w *gpui.Window, cx *gpui.Context[testView]) {
		for range n {
			items = append(items, cx.App().FocusHandle().TabStop(true))
		}
		items[0].Focus(w)
		v.build = func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
			tb := NewToolbar(id).Disabled(disabled)
			names := []string{"first", "second", "third"}
			if n == 1 {
				names = []string{"only"}
			}
			for i, h := range items {
				tb.Child(gpui.Div().Flex().ID(names[i]).Size(gpui.Px(20)).TrackFocus(h))
			}
			return elementOf(t, tb)
		}
	})
	redraw(t, cx)
	return cx, items
}

func toolbarAssertFocused(t *testing.T, cx *gpui.VisualTestContext, expected gpui.FocusHandle, label string) {
	t.Helper()
	if !expected.IsFocused(cx.Window()) {
		t.Fatalf("expected %s to be focused", label)
	}
}

// U:toolbar.rs:347-362
func TestToolbarArrowKeysRoveFocusAcrossItems(t *testing.T) {
	cx, items := toolbarHarness(t, "nav-toolbar", false, 3)
	cx.SimulateKeystrokes("right")
	toolbarAssertFocused(t, cx, items[1], "second")
	cx.SimulateKeystrokes("right")
	toolbarAssertFocused(t, cx, items[2], "third")
	// Wrapping: past the last item, focus returns to the first.
	cx.SimulateKeystrokes("right")
	toolbarAssertFocused(t, cx, items[0], "first (wrapped)")
	cx.SimulateKeystrokes("left")
	toolbarAssertFocused(t, cx, items[2], "third (wrapped back)")
}

// U:toolbar.rs:364-390
func TestToolbarWithASingleItemKeepsFocusOnIt(t *testing.T) {
	cx, items := toolbarHarness(t, "single-toolbar", false, 1)
	cx.SimulateKeystrokes("right left right")
	toolbarAssertFocused(t, cx, items[0], "the only item")
}

// U:toolbar.rs:392-425
func TestToolbarGroupExposesGroupRoleAndAccessibleName(t *testing.T) {
	got := a11yOf(t, func(w *gpui.Window, app *gpui.App) *gpui.DivElement {
		return NewToolbarGroup("history").Label("History").Child(gpui.Div().Flex().Size(gpui.Px(20))).render(w, app)
	})
	if got[0].role != gpui.RoleGroup || got[0].spec.Label != "History" {
		t.Fatalf("group = %+v", got[0])
	}
}

// U:toolbar.rs:427-458
func TestToolbarDisabledToolbarIgnoresArrowKeys(t *testing.T) {
	cx, items := toolbarHarness(t, "disabled-toolbar", true, 2)
	cx.SimulateKeystrokes("right")
	toolbarAssertFocused(t, cx, items[0], "first (disabled toolbar)")
}
