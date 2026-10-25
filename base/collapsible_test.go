package base

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ports of U:collapsible.rs tests (1/1), plus one Go-own layout test for
// the gap-2b Block base.

func collapsibleHarness(t *testing.T, open bool) *gpui.VisualTestContext {
	t.Helper()
	return drawn(t, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return elementOf(t, NewCollapsible().
			Open(open).
			Child(gpui.Div().Flex().DebugSelector("trigger").Size(gpui.Px(10))).
			Content(gpui.Div().Flex().DebugSelector("content").Size(gpui.Px(10))))
	})
}

// U:collapsible.rs:100-111
func TestCollapsibleContentIsOnlyRenderedWhileOpen(t *testing.T) {
	cx := collapsibleHarness(t, false)
	if _, ok := cx.DebugBounds("trigger"); !ok {
		t.Fatal("closed: trigger missing")
	}
	if _, ok := cx.DebugBounds("content"); ok {
		t.Fatal("closed: content rendered")
	}
	cx = collapsibleHarness(t, true)
	if _, ok := cx.DebugBounds("trigger"); !ok {
		t.Fatal("open: trigger missing")
	}
	if _, ok := cx.DebugBounds("content"); !ok {
		t.Fatal("open: content missing")
	}
}

// Go-own (gap 2b): with the 0.2.2 Block default the content stacks below
// the trigger.
func TestCollapsibleContentStacksBelowTrigger(t *testing.T) {
	cx := collapsibleHarness(t, true)
	trigger := debugBounds(t, cx, "trigger")
	content := debugBounds(t, cx, "content")
	if content.Origin.X != trigger.Origin.X || content.Origin.Y != trigger.Origin.Y+10 {
		t.Fatalf("content origin %v, trigger origin %v", content.Origin, trigger.Origin)
	}
}
