package base

import (
	"slices"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ports of U:accordion.rs tests (3/3), plus one Go-own layout test for
// the gap-2b Block roots.

// U:accordion.rs:372-417
func TestAccordionTriggerProjectsExpandedAccessibilityState(t *testing.T) {
	var requested []bool
	got := a11yOf(t, func(w *gpui.Window, app *gpui.App) *gpui.DivElement {
		return NewAccordionTrigger("trigger").Open(true).
			OnChange(func(open bool, _ *gpui.ClickEvent, _ *gpui.Window, _ *gpui.App) { requested = append(requested, open) }).
			render(w, app)
	})
	if got[0].role != gpui.RoleButton {
		t.Fatalf("role = %v, want Button", got[0].role)
	}
	if v, ok := got[0].spec.Expanded.Get(); !ok || !v {
		t.Fatalf("expanded = %v (set %v), want Some(true)", v, ok)
	}
	if len(requested) != 0 {
		t.Fatalf("requested = %v, want none", requested)
	}
}

func accordionHarness(t *testing.T, open, disabled bool) (*gpui.VisualTestContext, *[]bool) {
	t.Helper()
	requested := new([]bool)
	cx := drawn(t, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return elementOf(t, NewAccordionTrigger("trigger").
			Open(open).
			Disabled(disabled).
			Size(gpui.Px(100)).
			OnChange(func(next bool, _ *gpui.ClickEvent, _ *gpui.Window, _ *gpui.App) {
				*requested = append(*requested, next)
			}))
	})
	return cx, requested
}

// U:accordion.rs:454-463
func TestAccordionPointerRequestsNextControlledStateAndRespectsDisabled(t *testing.T) {
	cx, requested := accordionHarness(t, false, false)
	clickAt(cx, 10, 10)
	if !slices.Equal(*requested, []bool{true}) {
		t.Fatalf("requested = %v, want [true]", *requested)
	}
	cx, requested = accordionHarness(t, true, true)
	clickAt(cx, 10, 10)
	if len(*requested) != 0 {
		t.Fatalf("disabled requested = %v, want none", *requested)
	}
}

// U:accordion.rs:465-506. Upstream builds each accesskit::Node with the
// expected role, so its role assertions only check write_a11y_info ran;
// the port reads the rendered div's own role (stronger) and the level.
func TestAccordionHeaderAndPanelProjectStructuralRoles(t *testing.T) {
	got := a11yOf(t,
		func(w *gpui.Window, app *gpui.App) *gpui.DivElement {
			return NewAccordionHeader(NewAccordionTrigger("trigger")).ID("header").Level(2).render(w, app)
		},
		func(w *gpui.Window, app *gpui.App) *gpui.DivElement {
			return NewAccordionPanel().ID("panel").Open(true).render(w, app)
		},
	)
	if got[0].role != gpui.RoleHeading {
		t.Fatalf("header role = %v, want Heading", got[0].role)
	}
	if level, ok := got[0].spec.Level.Get(); !ok || level != 2 {
		t.Fatalf("header level = %v (set %v), want 2", level, ok)
	}
	if got[1].role != gpui.RoleRegion {
		t.Fatalf("panel role = %v, want Region", got[1].role)
	}
}

// Go-own (gap 2b): an item stacks its header above its panel like the
// 0.2.2 Block default, and a closed panel is not mounted.
func TestAccordionItemStacksHeaderAbovePanel(t *testing.T) {
	for _, open := range []bool{true, false} {
		cx := drawn(t, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
			return elementOf(t, NewAccordion("accordion").W(gpui.Px(200)).Child(
				NewAccordionItem().Open(open).
					Header(NewAccordionHeader(NewAccordionTrigger("trigger").H(gpui.Px(20)).DebugSelector("trigger"))).
					Panel(NewAccordionPanel().Child(gpui.Div().Flex().H(gpui.Px(30)).DebugSelector("panel")))))
		})
		trigger := debugBounds(t, cx, "trigger")
		if trigger.Size.Width != 200 {
			t.Fatalf("trigger width = %v, want 200 (block fills)", trigger.Size.Width)
		}
		panel, ok := cx.DebugBounds("panel")
		if ok != open {
			t.Fatalf("open=%v: panel mounted = %v", open, ok)
		}
		if open && panel.Origin.Y != trigger.Origin.Y+20 {
			t.Fatalf("panel top = %v, want %v", panel.Origin.Y, trigger.Origin.Y+20)
		}
	}
}
