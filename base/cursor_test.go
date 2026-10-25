package base

// Base ticket 39 (Go-own): hovering a Base component requests the cursor
// upstream requests. Each test hovers an element on a deterministic test
// window and reads the cursor the window resolved for the platform
// (gpui.WindowCursorStyle; test windows are never hovered, so this is the
// style a hovered real window hands to Host.SetCursorStyle).

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// hoverCursor moves the mouse to (x, y) and returns the resolved cursor.
func hoverCursor(t *testing.T, cx *gpui.VisualTestContext, x, y float32) gpui.CursorStyle {
	t.Helper()
	cx.SimulateMouseMove(gpui.Point{X: x, Y: y}, nil, gpui.NoModifiers())
	style, ok := gpui.WindowCursorStyle(cx.Window())
	if !ok {
		t.Fatalf("no cursor resolved after hovering (%v,%v)", x, y)
	}
	return style
}

func wantCursor(t *testing.T, what string, got, want gpui.CursorStyle) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: cursor = %v, want %v", what, got, want)
	}
}

// Base's Button is unstyled (U:button.rs:207-257 sets no cursor; the
// pointer is the styled layer's, U:component/button/button.rs:655), so the
// application's Styled cursor must survive the instance-style refinement,
// and a disabled state style can replace it.
func TestCursorAButtonShowsTheCursorItIsStyledWith(t *testing.T) {
	cx := drawn(t, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.Div().Flex().Relative().Size(gpui.Px(300)).
			Child(gpui.Div().Flex().Absolute().Left(gpui.Px(0)).Top(gpui.Px(0)).Size(gpui.Px(50)).
				Child(NewButton("pointer").SizeFull().CursorPointer())).
			Child(gpui.Div().Flex().Absolute().Left(gpui.Px(100)).Top(gpui.Px(0)).Size(gpui.Px(50)).
				Child(NewButton("plain").SizeFull())).
			Child(gpui.Div().Flex().Absolute().Left(gpui.Px(200)).Top(gpui.Px(0)).Size(gpui.Px(50)).
				Child(NewButton("disabled").SizeFull().CursorPointer().Disabled(true).
					Styles(func(s ButtonStyles) ButtonStyles {
						return s.Disabled(func(st *StateStyle) *StateStyle { return st.CursorNotAllowed() })
					}))).
			IntoElement()
	})
	wantCursor(t, "styled button", hoverCursor(t, cx, 10, 10), gpui.CursorPointingHand)
	wantCursor(t, "unstyled button", hoverCursor(t, cx, 110, 10), gpui.CursorArrow)
	wantCursor(t, "disabled button", hoverCursor(t, cx, 210, 10), gpui.CursorOperationNotAllowed)
	wantCursor(t, "outside", hoverCursor(t, cx, 150, 150), gpui.CursorArrow)
}

// U:input/base/state.rs:4483-4489: an enabled input shows the I-beam.
func TestCursorAnInputShowsTheIBeam(t *testing.T) {
	v := buildInput(t, plainInput)
	v.cx.RunUntilParked()
	drawFrame(t, v.cx)
	var bounds gpui.Bounds
	v.read(func(s *InputState) { bounds = s.inputBounds })
	if bounds.Size.Width <= 0 || bounds.Size.Height <= 0 {
		t.Fatalf("input bounds = %v, want a laid-out input", bounds)
	}
	c := bounds.Center()
	wantCursor(t, "input", hoverCursor(t, v.cx, c.X, c.Y), gpui.CursorIBeam)
}

// U:input/base/state.rs:4483: a disabled input requests no cursor.
func TestCursorADisabledInputKeepsTheArrow(t *testing.T) {
	v := buildInput(t, func(s InputState) InputState { return s.Disabled(true) })
	v.cx.RunUntilParked()
	drawFrame(t, v.cx)
	var bounds gpui.Bounds
	v.read(func(s *InputState) { bounds = s.inputBounds })
	c := bounds.Center()
	wantCursor(t, "disabled input", hoverCursor(t, v.cx, c.X, c.Y), gpui.CursorArrow)
}

// U:input/base/state.rs:4484-4485: a multi-line input shows the I-beam,
// and the crosshair while Alt (the column selection) is held.
func TestCursorAMultiLineInputShowsTheCrosshairWithAlt(t *testing.T) {
	v := buildEditor(t)
	v.cx.RunUntilParked()
	drawFrame(t, v.cx)
	var bounds gpui.Bounds
	v.read(func(s *EditorState) { bounds = s.inputBounds })
	c := bounds.Center()
	wantCursor(t, "editor", hoverCursor(t, v.cx, c.X, c.Y), gpui.CursorIBeam)
	v.cx.SimulateModifiersChange(gpui.Modifiers{Alt: true})
	v.cx.RunUntilParked()
	drawFrame(t, v.cx)
	style, _ := gpui.WindowCursorStyle(v.cx.Window())
	wantCursor(t, "editor with Alt", style, gpui.CursorCrosshair)
}

type resizableCursorHarness struct{ vertical bool }

func (h *resizableCursorHarness) Render(*gpui.Window, *gpui.Context[resizableCursorHarness]) gpui.AnyElement {
	group := HResizable("cursor-h")
	if h.vertical {
		group = VResizable("cursor-v")
	}
	return gpui.Div().Flex().W(gpui.Px(400)).H(gpui.Px(400)).Child(
		group.
			Child(NewResizablePanel().Size(150).Child(gpui.Div().Flex().SizeFull())).
			Child(NewResizablePanel().Size(250).Child(gpui.Div().Flex().SizeFull().DebugSelector("cursor-second"))),
	).IntoElement()
}

// U:resizable/resize_handle.rs:290-301: a straddling handle shows the
// column resize cursor on a horizontal group and the row resize cursor
// on a vertical one; the panels keep the arrow.
func TestCursorAResizableHandleShowsTheResizeCursor(t *testing.T) {
	for _, tc := range []struct {
		name     string
		vertical bool
		want     gpui.CursorStyle
	}{
		{"horizontal", false, gpui.CursorResizeColumn},
		{"vertical", true, gpui.CursorResizeRow},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ta := gpui.NewTestApp()
			shutdownAtCleanup(t, ta)
			_, cx := ta.AddWindowView(func(h *resizableCursorHarness, _ *gpui.Window, _ *gpui.Context[resizableCursorHarness]) {
				h.vertical = tc.vertical
			})
			drawFrame(t, cx)
			drawFrame(t, cx)
			second := debugBounds(t, cx, "cursor-second")
			x, y := second.Left()-1, float32(50)
			if tc.vertical {
				x, y = 50, second.Top()-1
			}
			wantCursor(t, "handle", hoverCursor(t, cx, x, y), tc.want)
			wantCursor(t, "panel", hoverCursor(t, cx, second.Left()+60, second.Top()+60), gpui.CursorArrow)
		})
	}
}

type huggingCursorHarness struct {
	axis gpui.Axis
	edge HandleEdge
}

func (h *huggingCursorHarness) Render(*gpui.Window, *gpui.Context[huggingCursorHarness]) gpui.AnyElement {
	return gpui.Div().Flex().Relative().OverflowHidden().W(gpui.Px(200)).H(gpui.Px(200)).
		Child(NewResizeHandle[noDrag]("hug", h.axis).Inside(h.edge)).
		IntoElement()
}

// U:resizable/resize_handle.rs:262-287: a handle hugging either edge of
// its container (a dock's splitter) shows the column resize cursor on
// the horizontal axis and the row resize cursor on the vertical one.
func TestCursorAHuggingHandleShowsTheResizeCursor(t *testing.T) {
	forEachHugging(t, func(name string, ta *gpui.TestApp, axis gpui.Axis, edge HandleEdge) {
		_, cx := ta.AddWindowView(func(h *huggingCursorHarness, _ *gpui.Window, _ *gpui.Context[huggingCursorHarness]) {
			h.axis, h.edge = axis, edge
		})
		drawFrame(t, cx)
		along := float32(1)
		if edge == HandleEdgeTrailing {
			along = 199
		}
		x, y, want := along, float32(100), gpui.CursorResizeColumn
		if axis == gpui.AxisVertical {
			x, y, want = 100, along, gpui.CursorResizeRow
		}
		wantCursor(t, name+" handle", hoverCursor(t, cx, x, y), want)
		wantCursor(t, name+" container", hoverCursor(t, cx, 100, 100), gpui.CursorArrow)
	})
}
