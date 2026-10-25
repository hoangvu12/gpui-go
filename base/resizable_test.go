package base

import (
	"slices"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ported tests of U:resizable/mod.rs (8/8).

func drawFrame(t *testing.T, cx *gpui.VisualTestContext) {
	t.Helper()
	if _, err := gpui.DrawWindowFrame(cx.Window()); err != nil {
		t.Fatal(err)
	}
}

func newResizableState(t *testing.T, ta *gpui.TestApp) gpui.Entity[ResizableState] {
	t.Helper()
	var state gpui.Entity[ResizableState]
	ta.Update(func(app *gpui.App) {
		state = gpui.NewEntity(app, ta.RootScope(), func(s *ResizableState, _ *gpui.Context[ResizableState]) {
			*s = NewResizableState()
		})
	})
	return state
}

func resizableSizes(ta *gpui.TestApp, state gpui.Entity[ResizableState]) []float32 {
	var sizes []float32
	ta.Update(func(app *gpui.App) {
		sizes = state.Read(app, func(s *ResizableState, _ *gpui.App) []float32 { return slices.Clone(s.Sizes()) })
	})
	return sizes
}

// mixedSizingHarness is U:resizable/mod.rs:414-436.
type mixedSizingHarness struct {
	width float32
}

func (h *mixedSizingHarness) Render(*gpui.Window, *gpui.Context[mixedSizingHarness]) gpui.AnyElement {
	return gpui.Div().Flex().W(gpui.Px(h.width)).H(gpui.Px(100)).Child(
		HResizable("mixed-sizing").
			Child(NewResizablePanel().Size(240).Child(gpui.Div().Flex().SizeFull().DebugSelector("fixed-sidebar"))).
			Child(NewResizablePanel().Child(gpui.Div().Flex().SizeFull().DebugSelector("flexible-content"))),
	).IntoElement()
}

// U:resizable/mod.rs:438-461
func TestResizableMixedSizingIsStableBetweenResizeAndFollowupFrame(t *testing.T) {
	ta := gpui.NewTestApp()
	shutdownAtCleanup(t, ta)
	view, cx := ta.AddWindowView(func(h *mixedSizingHarness, _ *gpui.Window, _ *gpui.Context[mixedSizingHarness]) { h.width = 800 })
	drawFrame(t, cx)
	drawFrame(t, cx)
	before := debugBounds(t, cx, "fixed-sidebar").Size.Width

	cx.Update(func(_ *gpui.Window, app *gpui.App) {
		view.Update(app, func(h *mixedSizingHarness, vcx *gpui.Context[mixedSizingHarness]) {
			h.width = 1200
			vcx.Notify()
		})
	})
	cx.RunUntilParked()
	settled := debugBounds(t, cx, "fixed-sidebar").Size.Width
	drawFrame(t, cx)
	followup := debugBounds(t, cx, "fixed-sidebar").Size.Width

	// Resizable panels preserve their proportional sizing across a
	// container resize; applying the state on the follow-up frame does not
	// move the divider again.
	if settled == before {
		t.Fatalf("the sidebar kept %v across the container resize", before)
	}
	if followup != settled {
		t.Fatalf("follow-up frame moved the divider: %v != %v", followup, settled)
	}
}

// callerStateHarness is U:resizable/mod.rs:463-481.
type callerStateHarness struct {
	width float32
	state gpui.Entity[ResizableState]
}

func (h *callerStateHarness) Render(*gpui.Window, *gpui.Context[callerStateHarness]) gpui.AnyElement {
	return gpui.Div().Flex().W(gpui.Px(h.width)).H(gpui.Px(100)).Child(
		HResizable("caller-state").WithState(h.state).
			Child(NewResizablePanel().Size(240).Child(gpui.Div().Flex().SizeFull().DebugSelector("cs-sidebar"))).
			Child(NewResizablePanel().Child(gpui.Div().Flex().SizeFull())),
	).IntoElement()
}

// U:resizable/mod.rs:483-511: a caller-owned state has no keyed-state
// observer behind it, so the settling frame has to be scheduled by the
// deferred notify.
func TestResizableCallerOwnedStateSettlesOnTheSameFrame(t *testing.T) {
	ta := gpui.NewTestApp()
	shutdownAtCleanup(t, ta)
	state := newResizableState(t, ta)
	view, cx := ta.AddWindowView(func(h *callerStateHarness, _ *gpui.Window, _ *gpui.Context[callerStateHarness]) {
		h.width, h.state = 800, state
	})
	drawFrame(t, cx)
	drawFrame(t, cx)

	cx.Update(func(_ *gpui.Window, app *gpui.App) {
		view.Update(app, func(h *callerStateHarness, vcx *gpui.Context[callerStateHarness]) {
			h.width = 1200
			vcx.Notify()
		})
	})
	cx.RunUntilParked()
	settled := debugBounds(t, cx, "cs-sidebar").Size.Width
	drawFrame(t, cx)
	followup := debugBounds(t, cx, "cs-sidebar").Size.Width
	if followup != settled {
		t.Fatalf("settling frame must not be pending: %v != %v", followup, settled)
	}
}

// resizableHarness is U:resizable/mod.rs:513-539.
type resizableHarness struct {
	state   gpui.Entity[ResizableState]
	resizes *int
}

func (h *resizableHarness) Render(*gpui.Window, *gpui.Context[resizableHarness]) gpui.AnyElement {
	resizes := h.resizes
	return gpui.Div().Flex().W(gpui.Px(400)).H(gpui.Px(100)).Child(
		HResizable("resizable").WithState(h.state).
			OnResize(func(gpui.Entity[ResizableState], *gpui.Window, *gpui.App) { *resizes++ }).
			Child(NewResizablePanel().Size(150).Child(gpui.Div().Flex().SizeFull().DebugSelector("first-panel"))).
			Child(NewResizablePanel().Size(250).Child(gpui.Div().Flex().SizeFull().DebugSelector("second-panel"))),
	).IntoElement()
}

// resizableTestHarness is U:resizable/mod.rs:541-558 harness.
func resizableTestHarness(t *testing.T) (*gpui.TestApp, *gpui.VisualTestContext, gpui.Entity[ResizableState], *int) {
	t.Helper()
	ta := gpui.NewTestApp()
	shutdownAtCleanup(t, ta)
	state := newResizableState(t, ta)
	resizes := new(int)
	_, cx := ta.AddWindowView(func(h *resizableHarness, _ *gpui.Window, _ *gpui.Context[resizableHarness]) {
		h.state, h.resizes = state, resizes
	})
	drawFrame(t, cx)
	drawFrame(t, cx)
	return ta, cx, state, resizes
}

// U:resizable/mod.rs:560-582
func TestResizableDynamicPanelLifecycleIsOwnedByResizableState(t *testing.T) {
	ta := gpui.NewTestApp()
	shutdownAtCleanup(t, ta)
	state := newResizableState(t, ta)
	ta.Update(func(app *gpui.App) {
		state.Update(app, func(s *ResizableState, cx *gpui.Context[ResizableState]) {
			s.bounds.Size = gpui.Size{Width: 400, Height: 100}
			s.panels = append(s.panels, resizablePanelState{})
			s.sizes = append(s.sizes, 400)
			size := float32(200)
			s.InsertPanel(&size, -1, cx)
			if !slices.Equal(s.Sizes(), []float32{200, 200}) {
				t.Fatalf("after insert: %v", s.Sizes())
			}
			s.ResetPanel(0, cx)
			if !slices.Equal(s.Sizes(), []float32{200, 200}) {
				t.Fatalf("after reset: %v", s.Sizes())
			}
			s.RemovePanel(0, cx)
			if !slices.Equal(s.Sizes(), []float32{400}) {
				t.Fatalf("after remove: %v", s.Sizes())
			}
			s.Clear()
			if len(s.Sizes()) != 0 {
				t.Fatalf("after clear: %v", s.Sizes())
			}
		})
	})
}

// U:resizable/mod.rs:584-601
func TestResizableGroupMeasuresPanelsAndProgrammaticResizeUsesDragRules(t *testing.T) {
	ta, cx, state, _ := resizableTestHarness(t)
	first := debugBounds(t, cx, "first-panel")
	second := debugBounds(t, cx, "second-panel")
	if first.Size.Width+second.Size.Width != 400 {
		t.Fatalf("widths %v + %v != 400", first.Size.Width, second.Size.Width)
	}

	cx.Update(func(w *gpui.Window, app *gpui.App) {
		state.Update(app, func(s *ResizableState, scx *gpui.Context[ResizableState]) {
			s.ResizePanel(0, 220, w, scx)
		})
		if _, err := gpui.DrawWindowFrame(w); err != nil {
			t.Fatal(err)
		}
	})
	if got := resizableSizes(ta, state); !slices.Equal(got, []float32{220, 180}) {
		t.Fatalf("sizes = %v, want [220 180]", got)
	}
}

// U:resizable/mod.rs:603-633
func TestResizableDraggingTheHandleResizesAndEmitsOnce(t *testing.T) {
	ta, cx, state, resizes := resizableTestHarness(t)
	boundary := debugBounds(t, cx, "second-panel").Left()
	left := gpui.MouseButtonLeft

	cx.SimulateMouseDown(gpui.Point{X: boundary - 2, Y: 50}, gpui.MouseButtonLeft, gpui.NoModifiers())
	cx.SimulateMouseMove(gpui.Point{X: boundary + 10, Y: 50}, &left, gpui.NoModifiers())
	cx.SimulateMouseMove(gpui.Point{X: 220, Y: 50}, &left, gpui.NoModifiers())
	cx.SimulateMouseUp(gpui.Point{X: 220, Y: 50}, gpui.MouseButtonLeft, gpui.NoModifiers())

	if got := resizableSizes(ta, state); !slices.Equal(got, []float32{220, 180}) {
		t.Fatalf("sizes = %v, want [220 180]", got)
	}
	if *resizes != 1 {
		t.Fatalf("on_resize ran %d times, want 1", *resizes)
	}
}

// handleStateHarness is U:resizable/mod.rs:635-675: it reports every
// state its divider is rendered in; covered lays an occluding overlay
// over the whole group.
type handleStateHarness struct {
	seen    *[]ResizeHandleState
	covered bool
}

func (h *handleStateHarness) Render(*gpui.Window, *gpui.Context[handleStateHarness]) gpui.AnyElement {
	seen := h.seen
	d := gpui.Div().Flex().Relative().W(gpui.Px(400)).H(gpui.Px(100)).Child(
		HResizable("handle-state").
			WithHandleAppearance(func(handle *ResizeHandleContext, _ *gpui.Window, _ *gpui.App) (gpui.AnyElement, bool) {
				if n := len(*seen); n == 0 || (*seen)[n-1] != handle.State() {
					*seen = append(*seen, handle.State())
				}
				// Nothing painted: this renderer is here to watch.
				return gpui.AnyElement{}, false
			}).
			Child(NewResizablePanel().Size(150).Child(gpui.Div().Flex().SizeFull())).
			Child(NewResizablePanel().Size(250).Child(gpui.Div().Flex().SizeFull().DebugSelector("hs-second"))),
	)
	if h.covered {
		d.Child(gpui.Div().Flex().Absolute().Inset0().Occlude())
	}
	return d.IntoElement()
}

func openHandleStateHarness(t *testing.T, covered bool) (*gpui.VisualTestContext, *[]ResizeHandleState) {
	t.Helper()
	ta := gpui.NewTestApp()
	shutdownAtCleanup(t, ta)
	seen := new([]ResizeHandleState)
	_, cx := ta.AddWindowView(func(h *handleStateHarness, _ *gpui.Window, _ *gpui.Context[handleStateHarness]) {
		h.seen, h.covered = seen, covered
	})
	drawFrame(t, cx)
	drawFrame(t, cx)
	return cx, seen
}

// U:resizable/mod.rs:677-709: a handle under something else does not
// answer the pointer.
func TestResizableACoveredHandleStaysIdle(t *testing.T) {
	cx, seen := openHandleStateHarness(t, true)
	onHandle := gpui.Point{X: debugBounds(t, cx, "hs-second").Left() - 2, Y: 50}

	cx.SimulateMouseMove(onHandle, nil, gpui.NoModifiers())
	drawFrame(t, cx)
	cx.SimulateMouseDown(onHandle, gpui.MouseButtonLeft, gpui.NoModifiers())
	drawFrame(t, cx)
	cx.SimulateMouseUp(onHandle, gpui.MouseButtonLeft, gpui.NoModifiers())
	drawFrame(t, cx)

	if want := []ResizeHandleState{ResizeHandleIdle}; !slices.Equal(*seen, want) {
		t.Fatalf("seen = %v, want %v", *seen, want)
	}
}

// U:resizable/mod.rs:711-758
func TestResizableAHandleReportsThePressAndTheDragToItsRenderer(t *testing.T) {
	cx, seen := openHandleStateHarness(t, false)
	onHandle := gpui.Point{X: debugBounds(t, cx, "hs-second").Left() - 2, Y: 50}
	left := gpui.MouseButtonLeft

	cx.SimulateMouseMove(onHandle, nil, gpui.NoModifiers())
	drawFrame(t, cx)
	cx.SimulateMouseDown(onHandle, gpui.MouseButtonLeft, gpui.NoModifiers())
	drawFrame(t, cx)
	cx.SimulateMouseMove(gpui.Point{X: 260, Y: 50}, &left, gpui.NoModifiers())
	drawFrame(t, cx)
	cx.SimulateMouseUp(gpui.Point{X: 390, Y: 90}, gpui.MouseButtonLeft, gpui.NoModifiers())
	drawFrame(t, cx)

	want := []ResizeHandleState{ResizeHandleIdle, ResizeHandleHovered, ResizeHandlePressed, ResizeHandleDragging, ResizeHandleIdle}
	if !slices.Equal(*seen, want) {
		t.Fatalf("seen = %v, want %v", *seen, want)
	}
}

// sizedGroupHarness is U:resizable/mod.rs:760-774.
type sizedGroupHarness struct{}

func (*sizedGroupHarness) Render(*gpui.Window, *gpui.Context[sizedGroupHarness]) gpui.AnyElement {
	return gpui.Div().Flex().W(gpui.Px(400)).H(gpui.Px(100)).Child(
		HResizable("sized-resizable").Size(40).Child(
			NewResizablePanel().Child(gpui.Div().Flex().SizeFull().DebugSelector("sized-panel"))),
	).IntoElement()
}

// U:resizable/mod.rs:776-785
func TestResizableAGroupSizeBindsTheCrossAxis(t *testing.T) {
	ta := gpui.NewTestApp()
	shutdownAtCleanup(t, ta)
	_, cx := ta.AddWindowView(func(*sizedGroupHarness, *gpui.Window, *gpui.Context[sizedGroupHarness]) {})
	drawFrame(t, cx)
	drawFrame(t, cx)
	panel := debugBounds(t, cx, "sized-panel")
	if panel.Size.Width != 400 || panel.Size.Height != 40 {
		t.Fatalf("panel = %vx%v, want 400x40", panel.Size.Width, panel.Size.Height)
	}
}
