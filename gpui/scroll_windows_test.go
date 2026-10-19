//go:build windows

package gpui

// Base ticket 07's real-window gate: on a REAL window enrolled in the
// frame loop, a touchpad pan delivered through the host window's gesture
// callback — the exact delivery the Direct Manipulation drain uses
// (directmanipulation_windows.go; OpenWindow's OnGesture routing into
// dispatch_event) — reaches the scrolling div's default wheel listener
// (R:elements/div.rs:3816-3879), notifies the view, and the WM_PAINT frame
// request's prepaint clamps the offset and writes the handle's metrics
// (R:div.rs:2913-2976). No real input is injected and the cursor is never
// moved; a window-creation failure FAILS the test.
//
// The mouse wheel itself (WM_MOUSEWHEEL / WM_MOUSEHWHEEL,
// R:gpui_windows/src/events.rs:150-151, 562-651) is not routed by the
// port's host yet (win32host_windows.go, not this ticket's file), so it
// has no real-window gate here.

import "testing"

type scrollRealView struct {
	handle ScrollHandle
}

func (v *scrollRealView) Render(w *Window, cx *Context[scrollRealView]) AnyElement {
	return Div().ID("scroller").W(Px(100)).H(Px(100)).OverflowYScroll().TrackScroll(v.handle).
		Child(Div().W(Px(100)).H(Px(300)).FlexShrink0()).
		IntoElement()
}

func TestScrollRealWindowGestureScrollsTrackedDiv(t *testing.T) {
	h := desktopNewHost(t)
	app := NewApp()
	if err := app.Attach(h); err != nil {
		t.Fatalf("app attach: %v", err)
	}
	handle := NewScrollHandle()
	window, err := app.OpenWindowView(WindowOptions{
		Title:  "gpui scroll",
		Bounds: Bounds{Origin: Point{X: 60, Y: 60}, Size: Size{Width: 240, Height: 160}},
		Show:   true,
	}, func(w *Window, app *App) View {
		return ViewOf(NewEntity(app, w.Scope(), func(v *scrollRealView, _ *Context[scrollRealView]) { v.handle = handle }))
	})
	if err != nil {
		t.Fatalf("OpenWindowView failed: %v — this session is expected to create real windows", err)
	}
	t.Cleanup(func() {
		if window.Alive() {
			window.Close()
			desktopWaitFor(t, "the window to be destroyed", func() bool { return !window.Alive() })
		}
	})

	type facts struct {
		draws  uint64
		offset Point
		max    Point
		bounds Bounds
	}
	read := func() facts {
		var f facts
		if !h.runForegroundSync(func() {
			f.draws = WindowDrawCount(window)
			f.offset = handle.Offset()
			f.max = handle.MaxOffset()
			f.bounds = handle.Bounds()
		}) {
			t.Fatal("the host stopped")
		}
		return f
	}
	desktopWaitFor(t, "the opening frame to write the scroll metrics", func() bool {
		f := read()
		return f.draws >= 1 && f.max == (Point{Y: 200})
	})
	base := read()
	if base.bounds.Size != (Size{Width: 100, Height: 100}) || base.offset != (Point{}) {
		t.Fatalf("opening metrics: bounds %+v offset %v", base.bounds, base.offset)
	}

	deliver := func(event *ScrollWheelEvent) {
		t.Helper()
		delivered := false
		if !h.runForegroundSync(func() {
			record := window.handle.record()
			if record == nil || record.onGesture == nil {
				return
			}
			record.onGesture(event)
			delivered = true
		}) || !delivered {
			t.Fatal("the window's gesture callback is not installed")
		}
	}
	at := Point{X: 50, Y: 50}
	deliver(&ScrollWheelEvent{Position: at, Delta: ScrollDeltaPixels(0, -60), TouchPhase: TouchPhaseStarted})
	deliver(&ScrollWheelEvent{Position: at, Delta: ScrollDeltaPixels(0, -500), TouchPhase: TouchPhaseMoved})
	deliver(&ScrollWheelEvent{Position: at, Delta: ScrollDeltaPixels(0, 0), TouchPhase: TouchPhaseEnded})
	desktopWaitFor(t, "the pan to be clamped by a frame-loop redraw", func() bool {
		f := read()
		return f.draws > base.draws && f.offset == (Point{Y: -200})
	})
}
