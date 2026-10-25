package base

import (
	"fmt"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ported tests of U:resizable/resize_handle.rs (8/8).

// handleProbe is U:resizable/resize_handle.rs:485-506 Probe: what a
// hugging handle's renderer was told and drew, and where the drawing
// landed in the frame.
type handleProbe struct {
	edgeSet           bool
	edge              *HandleEdge
	line              *gpui.Bounds
	mask              *gpui.Bounds
	prepaints         int
	linePrepainted    *int
	popoverPrepainted *int
}

func (p *handleProbe) tick() int {
	n := p.prepaints
	p.prepaints++
	return n
}

// hairline is U:resizable/resize_handle.rs:508-523.
func hairline(axis gpui.Axis, probe *handleProbe) gpui.AnyElement {
	line := gpui.Div().Flex().FlexNone()
	if axis == gpui.AxisHorizontal {
		line.W(gpui.Px(1)).HFull()
	} else {
		line.H(gpui.Px(1)).WFull()
	}
	return ElementExtOf(line).OnPrepaint(func(bounds gpui.Bounds, w *gpui.Window, _ *gpui.App) {
		probe.line = &bounds
		mask := gpui.ContentMask(w)
		probe.mask = &mask
		n := probe.tick()
		probe.linePrepainted = &n
	}).IntoElement()
}

// noDrag is U:resizable/resize_handle.rs:525-532 NoDrag.
type noDrag struct{}

// huggingHarness is U:resizable/resize_handle.rs:534-595: a dock-shaped
// box 200px along the axis, clipped to itself, between two 100px
// neighbours; the first neighbour defers a popover across both seams.
type huggingHarness struct {
	axis  gpui.Axis
	edge  HandleEdge
	probe *handleProbe
}

func (h *huggingHarness) Render(*gpui.Window, *gpui.Context[huggingHarness]) gpui.AnyElement {
	axis, probe := h.axis, h.probe
	neighbour := func() *gpui.DivElement {
		if axis == gpui.AxisHorizontal {
			return gpui.Div().Flex().W(gpui.Px(100)).HFull()
		}
		return gpui.Div().Flex().H(gpui.Px(100)).WFull()
	}
	popover := gpui.Div().Flex().Absolute()
	if axis == gpui.AxisHorizontal {
		popover.Top0().Left(gpui.Px(50)).W(gpui.Px(300)).HFull()
	} else {
		popover.Left0().Top(gpui.Px(50)).H(gpui.Px(300)).WFull()
	}
	popover = ElementExtOf(popover).OnPrepaint(func(gpui.Bounds, *gpui.Window, *gpui.App) {
		n := probe.tick()
		probe.popoverPrepainted = &n
	})
	row := gpui.Div().Flex()
	if axis == gpui.AxisHorizontal {
		row.FlexRow().W(gpui.Px(400)).H(gpui.Px(100))
	} else {
		row.FlexCol().H(gpui.Px(400)).W(gpui.Px(100))
	}
	dock := gpui.Div().Flex().Relative().OverflowHidden()
	if axis == gpui.AxisHorizontal {
		dock.W(gpui.Px(200)).HFull()
	} else {
		dock.H(gpui.Px(200)).WFull()
	}
	dock.Child(NewResizeHandle[noDrag]("hugging", axis).Inside(h.edge).WithAppearance(
		func(handle *ResizeHandleContext, _ *gpui.Window, _ *gpui.App) (gpui.AnyElement, bool) {
			probe.edgeSet = true
			probe.edge = nil
			if e, ok := handle.Edge(); ok {
				probe.edge = &e
			}
			return hairline(handle.Axis(), probe), true
		}))
	return row.
		Child(neighbour().Child(gpui.Deferred(popover))).
		Child(dock).
		Child(neighbour()).
		IntoElement()
}

// drawHugging is U:resizable/resize_handle.rs:597-605.
func drawHugging(t *testing.T, ta *gpui.TestApp, axis gpui.Axis, edge HandleEdge) *handleProbe {
	t.Helper()
	probe := &handleProbe{}
	_, cx := ta.AddWindowView(func(h *huggingHarness, _ *gpui.Window, _ *gpui.Context[huggingHarness]) {
		h.axis, h.edge, h.probe = axis, edge, probe
	})
	drawFrame(t, cx)
	return probe
}

// seam is U:resizable/resize_handle.rs:607-616: the dock spans 100..300.
func seam(edge HandleEdge) float32 {
	if edge == HandleEdgeLeading {
		return 100
	}
	return 300
}

// alongBounds is U:resizable/resize_handle.rs:618-623.
func alongBounds(axis gpui.Axis, b gpui.Bounds) [2]float32 {
	if axis == gpui.AxisHorizontal {
		return [2]float32{b.Left(), b.Right()}
	}
	return [2]float32{b.Top(), b.Bottom()}
}

func forEachHugging(t *testing.T, f func(name string, ta *gpui.TestApp, axis gpui.Axis, edge HandleEdge)) {
	ta := gpui.NewTestApp()
	shutdownAtCleanup(t, ta)
	for _, axis := range []gpui.Axis{gpui.AxisHorizontal, gpui.AxisVertical} {
		for _, edge := range []HandleEdge{HandleEdgeLeading, HandleEdgeTrailing} {
			f(fmt.Sprintf("axis %d %v", axis, edge), ta, axis, edge)
		}
	}
}

// U:resizable/resize_handle.rs:625-649: a hugging handle's hairline is
// the container's outermost pixel.
func TestResizeHandleAHuggingHandleDrawsItsLineOnTheSeam(t *testing.T) {
	forEachHugging(t, func(name string, ta *gpui.TestApp, axis gpui.Axis, edge HandleEdge) {
		probe := drawHugging(t, ta, axis, edge)
		if probe.line == nil {
			t.Fatalf("%s: the renderer was not asked to draw", name)
		}
		got := alongBounds(axis, *probe.line)
		want := [2]float32{seam(edge), seam(edge) + 1}
		if edge == HandleEdgeTrailing {
			want = [2]float32{seam(edge) - 1, seam(edge)}
		}
		if got != want {
			t.Fatalf("%s: the hairline has to be the pixel against the seam: %v, want %v", name, got, want)
		}
	})
}

// U:resizable/resize_handle.rs:651-691: a hugging handle's appearance is
// painted in tree order, under its container's mask, beneath a popover
// deferred before it.
func TestResizeHandleAHuggingHandlePaintsBeneathAPopoverDeferredBeforeIt(t *testing.T) {
	forEachHugging(t, func(name string, ta *gpui.TestApp, axis gpui.Axis, edge HandleEdge) {
		probe := drawHugging(t, ta, axis, edge)
		if probe.linePrepainted == nil || probe.popoverPrepainted == nil {
			t.Fatalf("%s: line %v popover %v were not both prepainted", name, probe.linePrepainted, probe.popoverPrepainted)
		}
		if line, popover := *probe.linePrepainted, *probe.popoverPrepainted; line >= popover {
			t.Fatalf("%s: the line (prepaint #%d) has to go down before the popover (#%d)", name, line, popover)
		}
		if got := alongBounds(axis, *probe.mask); got != [2]float32{100, 300} {
			t.Fatalf("%s: the line is painted under its container's clip: mask %v", name, got)
		}
	})
}

// U:resizable/resize_handle.rs:693-703: a renderer is told which edge the
// handle hugs.
func TestResizeHandleARendererIsToldTheEdgeAHandleHugs(t *testing.T) {
	forEachHugging(t, func(name string, ta *gpui.TestApp, axis gpui.Axis, edge HandleEdge) {
		probe := drawHugging(t, ta, axis, edge)
		if !probe.edgeSet || probe.edge == nil || *probe.edge != edge {
			t.Fatalf("%s: edge = %v (set %v)", name, probe.edge, probe.edgeSet)
		}
	})
}

// U:resizable/resize_handle.rs:705-718
func TestResizeHandleAListenerWritesItsProgressBackIntoTheStoredState(t *testing.T) {
	stored := newSharedHandleState()
	// What paint hands each mouse listener.
	listener := stored
	if !listener.set(ResizeHandlePressed) {
		t.Fatal("set reported no change")
	}
	if stored.get() != ResizeHandlePressed || !stored.get().IsActive() {
		t.Fatalf("stored = %v", stored.get())
	}
}

// U:resizable/resize_handle.rs:720-726
func TestResizeHandleSettingTheStateItAlreadyHasAsksForNoRepaint(t *testing.T) {
	state := newSharedHandleState()
	if !state.set(ResizeHandleHovered) {
		t.Fatal("the first set reported no change")
	}
	if state.set(ResizeHandleHovered) {
		t.Fatal("setting the same state asked for a repaint")
	}
}

// U:resizable/resize_handle.rs:728-734
func TestResizeHandleOnlyAHeldHandleIsActive(t *testing.T) {
	if ResizeHandleIdle.IsActive() || ResizeHandleHovered.IsActive() {
		t.Fatal("an unheld handle is active")
	}
	if !ResizeHandlePressed.IsActive() || !ResizeHandleDragging.IsActive() {
		t.Fatal("a held handle is not active")
	}
}

// U:resizable/resize_handle.rs:736-753
func TestResizeHandleAnUnprojectedHandleResolvesFromTheThemeTokens(t *testing.T) {
	ta := gpui.NewTestApp()
	shutdownAtCleanup(t, ta)
	ta.Update(func(app *gpui.App) {
		border := gpui.Hsla{H: 0, S: 0, L: 0.5, A: 1}
		ring := gpui.Hsla{H: 0.6, S: 0.5, L: 0.5, A: 1}
		theme := ThemeGlobalMut(app)
		theme.Tokens.Colors.Border = border
		theme.Tokens.Colors.Ring = ring
		theme.Resizable = ResizableTheme{}

		current := ThemeGlobal(app)
		if got := handleColor(current, false); got != border {
			t.Fatalf("rest = %v, want %v", got, border)
		}
		if got := handleColor(current, true); got != ring {
			t.Fatalf("active = %v, want %v", got, ring)
		}
		if handleColor(current, false) == (gpui.Hsla{}) {
			t.Fatal("the unprojected handle is transparent")
		}
	})
}

// U:resizable/resize_handle.rs:755-771
func TestResizeHandleAProjectedHandleStillWins(t *testing.T) {
	ta := gpui.NewTestApp()
	shutdownAtCleanup(t, ta)
	ta.Update(func(app *gpui.App) {
		projected := gpui.Hsla{H: 0.3, S: 0.4, L: 0.5, A: 1}
		active := gpui.Hsla{H: 0.9, S: 0.4, L: 0.5, A: 1}
		theme := ThemeGlobalMut(app)
		theme.Tokens.Colors.Border = gpui.Hsla{H: 0, S: 0, L: 0.5, A: 1}
		theme.Resizable = ResizableTheme{Handle: gpui.Some(projected), ActiveHandle: gpui.Some(active)}

		current := ThemeGlobal(app)
		if got := handleColor(current, false); got != projected {
			t.Fatalf("rest = %v, want %v", got, projected)
		}
		if got := handleColor(current, true); got != active {
			t.Fatalf("active = %v, want %v", got, active)
		}
	})
}
