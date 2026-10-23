//go:build windows

package gpui

// Base ticket 34's gates: on REAL Win32 windows, synthetic client-area
// mouse messages delivered with SendMessageW on the host thread (never
// the real mouse: no SendInput, no SetCursorPos) travel the whole path —
// the window procedure's translation (gpui_windows events.rs:114,
// 138-151, 371-398, 489-651), the mouse input callback App.OpenWindow
// installs (window.dispatchInput inside an update, window.rs:5739), the
// element listeners, and the frame loop's WM_PAINT redraw. A window
// creation failure FAILS the tests.

import (
	"sync"
	"testing"
	"time"
	"unsafe"
)

// TestWin32InputTranslation pins the pure translation halves: the
// pressed-button match order (events.rs:378-388), the move position
// (util.rs:147-152 logical_point) and the wheel distances
// (events.rs:574-604, 626-643).
func TestWin32InputTranslation(t *testing.T) {
	cases := []struct {
		wparam uintptr
		want   *MouseButton
	}{
		{0, nil},
		{mkLButton | mkRButton, ptrTo(MouseButtonLeft)},
		{mkRButton | mkMButton, ptrTo(MouseButtonRight)},
		{mkMButton | mkXButton1, ptrTo(MouseButtonMiddle)},
		{mkXButton1 | mkXButton2, ptrTo(MouseButtonButton4)},
		{mkXButton2 | 0x0004 /* MK_SHIFT */, ptrTo(MouseButtonButton5)},
		{0x0004 | 0x0008 /* MK_SHIFT|MK_CONTROL */, nil},
	}
	for _, c := range cases {
		got := win32PressedButton(c.wparam)
		if (got == nil) != (c.want == nil) || (got != nil && *got != *c.want) {
			t.Errorf("pressed button for wparam %#x = %v, want %v", c.wparam, got, c.want)
		}
	}
	move := win32MouseMoveEvent(mkLButton, buttonLParam(-30, 45), 1.5, Modifiers{Control: true})
	if move.Position != (Point{X: -20, Y: 30}) || move.PressedButton == nil || *move.PressedButton != MouseButtonLeft || !move.Modifiers.Control {
		t.Errorf("move = %+v", move)
	}

	settings := MouseWheelSettings{WheelScrollLines: 3, WheelScrollChars: 2}
	notches := func(n int16) uintptr { return uintptr(uint16(n*wheelDelta)) << 16 }
	down := win32WheelEvent(false, notches(-1), 30, 60, 2, Modifiers{}, settings)
	if down.Delta != ScrollDeltaLines(0, -3) || down.Position != (Point{X: 15, Y: 30}) || down.TouchPhase != TouchPhaseMoved {
		t.Errorf("vertical wheel = %+v, want 3 lines down at (15,30), Moved", down)
	}
	shifted := win32WheelEvent(false, notches(2), 0, 0, 1, Modifiers{Shift: true}, settings)
	if shifted.Delta != ScrollDeltaLines(4, 0) || !shifted.Modifiers.Shift {
		t.Errorf("shift wheel = %+v, want 2 notches x 2 chars on x", shifted)
	}
	horizontal := win32WheelEvent(true, notches(1), 0, 0, 1, Modifiers{}, settings)
	if horizontal.Delta != ScrollDeltaLines(-2, 0) {
		t.Errorf("horizontal wheel = %+v, want the negated chars distance on x", horizontal)
	}
	if win32InputResult(true) != 0 || win32InputResult(false) != 1 {
		t.Error("handled must map to 0 and unhandled to 1")
	}
}

func ptrTo[T any](v T) *T { return &v }

func inputAbs(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// sceneQuadColorCount counts the last drawn scene's quads with the solid
// background color (-1 when nothing was drawn or the scene failed).
func sceneQuadColorCount(w *Window, color Hsla) int {
	scene := LastDrawnScene(w)
	if scene == nil {
		return -1
	}
	quads, err := scene.Quads()
	if err != nil {
		return -1
	}
	want := SolidBackground(color).Opacity(1)
	n := 0
	for _, q := range quads {
		if q.Background == want {
			n++
		}
	}
	return n
}

// win32InputWindow is a real window opened for the input gates, with a
// recorder of the events the mouse input callback delivered.
type win32InputWindow struct {
	t      *testing.T
	h      *Host
	app    *App
	window *Window
	scale  float32

	// sending is true while the test's own SendMessageW runs (host
	// thread only): the OnMouseInput interceptor consumes every other
	// mouse event — real mouse moves over the window and the moves the
	// system synthesizes at the real cursor position (capture changes,
	// window show) — so only the synthetic messages reach element
	// dispatch.
	sending bool
	foreign int

	mu     sync.Mutex
	events []any
	log    []string
}

func (f *win32InputWindow) add(entry string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log = append(f.log, entry)
}

func (f *win32InputWindow) takeLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.log
	f.log = nil
	return out
}

// openWin32InputWindow opens a shown real window whose root view renders
// build; OnMouseInput records the test's own events without consuming
// them and consumes every foreign one (see sending).
func openWin32InputWindow(t *testing.T, title string, build func(f *win32InputWindow) func(v *interactView, w *Window, cx *Context[interactView]) AnyElement) *win32InputWindow {
	t.Helper()
	h := desktopNewHost(t)
	app := NewApp()
	if err := app.Attach(h); err != nil {
		t.Fatalf("app attach: %v", err)
	}
	f := &win32InputWindow{t: t, h: h, app: app}
	render := build(f)
	window, err := app.OpenWindowView(WindowOptions{
		Title:  title,
		Bounds: Bounds{Origin: Point{X: 40, Y: 40}, Size: Size{Width: 460, Height: 360}},
		Show:   true,
		OnMouseInput: func(event any) bool {
			if !f.sending {
				f.foreign++
				return true
			}
			f.mu.Lock()
			f.events = append(f.events, event)
			f.mu.Unlock()
			return false
		},
	}, func(w *Window, app *App) View {
		return ViewOf(NewEntity(app, w.Scope(), func(v *interactView, _ *Context[interactView]) { v.build = render }))
	})
	if err != nil {
		t.Fatalf("OpenWindowView failed: %v — this session is expected to create real windows", err)
	}
	f.window = window
	t.Cleanup(func() {
		if window.Alive() {
			window.Close()
			desktopWaitFor(t, "the window to be destroyed", func() bool { return !window.Alive() })
		}
	})
	scale, err := window.ScaleFactor()
	if err != nil || scale <= 0 {
		t.Fatalf("ScaleFactor = %v, %v", scale, err)
	}
	f.scale = scale
	f.waitIdle()
	return f
}

// onHost runs fn on the host thread.
func (f *win32InputWindow) onHost(fn func()) {
	f.t.Helper()
	if !f.h.runForegroundSync(fn) {
		f.t.Fatal("the host stopped")
	}
}

// waitIdle waits until the window has no frame demand.
func (f *win32InputWindow) waitIdle() {
	f.t.Helper()
	desktopWaitFor(f.t, "the window to go idle", func() bool {
		var demand bool
		f.onHost(func() { demand = f.window.hasFrameDemand() })
		return !demand
	})
}

// send delivers one synthetic message to the window with SendMessageW on
// the host thread and returns its result.
func (f *win32InputWindow) send(message uint32, wparam, lparam uintptr) uintptr {
	f.t.Helper()
	var result uintptr
	f.onHost(func() {
		f.sending = true
		defer func() { f.sending = false }()
		result, _, _ = procSendMessageW.Call(f.window.Handle().Hwnd(), uintptr(message), wparam, lparam)
	})
	return result
}

// at packs a window-local logical point as the client device-pixel
// lparam of the button and move messages.
func (f *win32InputWindow) at(x, y float32) uintptr {
	return buttonLParam(int16(x*f.scale+0.5), int16(y*f.scale+0.5))
}

// draws reads the window's draw count.
func (f *win32InputWindow) draws() uint64 {
	var n uint64
	f.onHost(func() { n = WindowDrawCount(f.window) })
	return n
}

// redrawn waits for a frame after before, then for the window to idle.
func (f *win32InputWindow) redrawn(what string, before uint64, cond func() bool) {
	f.t.Helper()
	desktopWaitFor(f.t, what, func() bool {
		if f.draws() <= before {
			return false
		}
		ok := false
		f.onHost(func() { ok = cond() })
		return ok
	})
}

// TestWin32InputRealWindowClickHoverCursor proves click, hover-style and
// cursor changes end to end on a real window.
func TestWin32InputRealWindowClickHoverCursor(t *testing.T) {
	var clicks []ClickEvent
	f := openWin32InputWindow(t, "gpui input click", func(f *win32InputWindow) func(v *interactView, w *Window, cx *Context[interactView]) AnyElement {
		return func(v *interactView, w *Window, cx *Context[interactView]) AnyElement {
			return Div().Relative().SizeFull().Child(
				Div().ID("button").Absolute().Left(Px(20)).Top(Px(20)).Size(Px(80)).Bg(interactRed).CursorPointer().
					Hover(func(s StyleRefinement) StyleRefinement { return Div().Refine(s).Bg(interactBlue).Style() }).
					OnClick(func(e *ClickEvent, w *Window, app *App) {
						clicks = append(clicks, *e)
						// The handled answer travels back as the message
						// result (events.rs:394, 536).
						app.StopPropagation()
					}),
			).IntoElement()
		}
	})
	// colors counts the button quads of the last drawn scene on the host
	// thread (no t.Fatal off the test goroutine).
	colors := func() (red, blue int) {
		return sceneQuadColorCount(f.window, interactRed), sceneQuadColorCount(f.window, interactBlue)
	}
	var red, blue int
	f.onHost(func() { red, blue = colors() })
	if red != 1 || blue != 0 {
		t.Fatalf("opening frame colors red %d blue %d, want the un-hovered button", red, blue)
	}

	// Hover: WM_MOUSEMOVE over the button repaints it with the hover
	// style and requests its pointing-hand cursor.
	before := f.draws()
	if r := f.send(wmMouseMove, 0, f.at(50, 50)); r != 1 {
		t.Errorf("WM_MOUSEMOVE result = %d, want 1 (no listener stopped the move)", r)
	}
	f.redrawn("the hovered button to be repainted", before, func() bool {
		r, b := colors()
		return r == 0 && b == 1
	})
	var resolved CursorStyle
	var resolvedOK bool
	f.onHost(func() { resolved, resolvedOK = resolvedCursorStyle(f.window) })
	if !resolvedOK || resolved != CursorPointingHand {
		t.Fatalf("resolved cursor over the button = %v (%v), want PointingHand", resolved, resolvedOK)
	}
	// The window was hovered while the move was handled (the move starts
	// the leave tracking, events.rs:372), so the reset reached the
	// platform cursor (window.rs:5684-5693) and every window adopts it.
	if style, err := f.h.CursorStyle(); err != nil || style != CursorPointingHand {
		t.Fatalf("platform cursor style = %v, %v; want PointingHand", style, err)
	}
	want := loadCursorHandle(CursorPointingHand)
	desktopWaitFor(t, "the window to adopt the pointing-hand cursor", func() bool {
		var cursor uintptr
		f.onHost(func() {
			if record := f.window.Handle().record(); record != nil {
				cursor = record.cursor
			}
		})
		return cursor == want
	})

	// Click: the press and release over the button synthesize one click.
	f.waitIdle()
	down := f.send(wmLButtonDown, mkLButton, f.at(50, 50))
	var captured uintptr
	f.onHost(func() { captured, _, _ = procGetCapture.Call() })
	if captured != f.window.Handle().Hwnd() {
		t.Errorf("GetCapture after the press = %#x, want the window", captured)
	}
	up := f.send(wmLButtonUp, 0, f.at(51, 50))
	f.onHost(func() { captured, _, _ = procGetCapture.Call() })
	if captured != 0 {
		t.Errorf("GetCapture after the release = %#x, want none", captured)
	}
	if up != 0 {
		t.Errorf("WM_LBUTTONUP result = %d, want 0 (the click listener stopped propagation)", up)
	}
	_ = down
	var got []ClickEvent
	f.onHost(func() { got = append(got, clicks...) })
	if len(got) != 1 {
		t.Fatalf("clicks = %d, want 1", len(got))
	}
	if got[0].Kind != ClickEventMouse || got[0].ClickCount() != 1 || got[0].IsRightClick() {
		t.Errorf("click = %+v, want one left mouse click", got[0])
	}
	if p := got[0].Position(); inputAbs(p.X-51) > 1 || inputAbs(p.Y-50) > 1 {
		t.Errorf("click position = %v, want about (51,50)", p)
	}

	// Leaving the button: the hover style and the cursor revert.
	f.waitIdle()
	before = f.draws()
	f.send(wmMouseMove, 0, f.at(300, 250))
	f.redrawn("the un-hovered button to be repainted", before, func() bool {
		r, b := colors()
		return r == 1 && b == 0
	})
	f.onHost(func() { resolved, _ = resolvedCursorStyle(f.window) })
	if resolved != CursorArrow {
		t.Fatalf("resolved cursor outside the button = %v, want Arrow", resolved)
	}

	// The mouse input callback saw the translated events in order.
	f.mu.Lock()
	events := append([]any(nil), f.events...)
	f.mu.Unlock()
	var kinds []string
	for _, e := range events {
		switch e := e.(type) {
		case *MouseMoveEvent:
			if e.PressedButton == nil {
				kinds = append(kinds, "move")
			} else {
				kinds = append(kinds, "drag")
			}
		case *MouseDownEvent:
			kinds = append(kinds, "down")
		case *MouseUpEvent:
			kinds = append(kinds, "up")
		}
	}
	// A real mouse over the window may add moves; the synthetic sequence
	// must appear in order.
	if !containsInOrder(kinds, []string{"move", "down", "up", "move"}) {
		t.Fatalf("delivered event kinds = %v, want move, down, up, move in order", kinds)
	}
}

// containsInOrder reports whether want is a subsequence of got.
func containsInOrder(got, want []string) bool {
	i := 0
	for _, g := range got {
		if i < len(want) && g == want[i] {
			i++
		}
	}
	return i == len(want)
}

// TestWin32InputRealWindowDragWithPreview proves an element drag on a
// real window: the press, the threshold-crossing move (MK_LBUTTON) that
// starts the drag, the preview at mouse - offset (window.rs:3451-3456,
// 3474-3475, through the previewHostElement child while the draw path's
// own preview wiring is ticket 08's), drag moves over the target, the
// drag cursor, and the drop on release.
func TestWin32InputRealWindowDragWithPreview(t *testing.T) {
	f := openWin32InputWindow(t, "gpui input drag", func(f *win32InputWindow) func(v *interactView, w *Window, cx *Context[interactView]) AnyElement {
		return func(v *interactView, w *Window, cx *Context[interactView]) AnyElement {
			source := Div().ID("source").Absolute().Left(Px(0)).Top(Px(0)).Size(Px(50)).CursorGrab().
				OnClick(func(*ClickEvent, *Window, *App) { f.add("source.click") }).
				OnDrag(dragPayload{name: "tab"}, func(value *dragPayload, offset Point, w *Window, app *App) View {
					f.add("constructor(" + value.name + ")")
					return ViewOf(NewEntity(app, w.Scope(), func(*dragPreview, *Context[dragPreview]) {}))
				})
			target := Div().ID("target").Absolute().Left(Px(200)).Top(Px(100)).Size(Px(100)).
				OnDragMove(func(e *DragMoveEvent[dragPayload], w *Window, app *App) {
					f.add("target.drag-move(" + e.Drag(app).name + ")")
				})
			target = OnDrop(target, func(value *dragPayload, e *MouseUpEvent, w *Window, app *App) {
				f.add("target.drop(" + value.name + ")")
			})
			return Div().Relative().SizeFull().Child(source).Child(target).Child(&previewHostElement{}).IntoElement()
		}
	})

	f.send(wmMouseMove, 0, f.at(10, 10))
	f.send(wmLButtonDown, mkLButton, f.at(10, 10))
	f.waitIdle()
	f.send(wmMouseMove, mkLButton, f.at(11, 10)) // inside DRAG_THRESHOLD
	var active bool
	f.onHost(func() { active = f.app.HasActiveDrag() })
	if active {
		t.Fatal("a move inside the drag threshold started the drag")
	}
	f.waitIdle()
	before := f.draws()
	f.send(wmMouseMove, mkLButton, f.at(20, 20))
	var drag *AnyDrag
	f.onHost(func() { drag = f.app.ActiveDrag() })
	if drag == nil {
		t.Fatalf("the threshold-crossing move did not start the drag (log %v)", f.takeLog())
	}
	if offset := drag.CursorOffset; inputAbs(offset.X-20) > 1 || inputAbs(offset.Y-20) > 1 {
		t.Errorf("drag cursor offset = %v, want about (20,20)", offset)
	}
	if log := f.takeLog(); !containsInOrder(log, []string{"constructor(tab)"}) {
		t.Fatalf("drag start log = %v", log)
	}
	previewAt := func(x, y float32) func() bool {
		return func() bool {
			b, ok := WindowDebugBound(f.window, "preview")
			return ok && inputAbs(b.Origin.X-x) <= 1 && inputAbs(b.Origin.Y-y) <= 1
		}
	}
	f.redrawn("the drag preview at mouse - offset", before, previewAt(0, 0))

	f.waitIdle()
	before = f.draws()
	f.send(wmMouseMove, mkLButton, f.at(250, 150))
	if log := f.takeLog(); !containsInOrder(log, []string{"target.drag-move(tab)"}) {
		t.Fatalf("drag move log = %v", log)
	}
	f.redrawn("the drag preview to follow the mouse", before, previewAt(230, 130))
	var resolved CursorStyle
	f.onHost(func() { resolved, _ = resolvedCursorStyle(f.window) })
	if resolved != CursorOpenHand {
		t.Errorf("drag cursor = %v, want OpenHand", resolved)
	}

	f.send(wmLButtonUp, 0, f.at(250, 150))
	if log := f.takeLog(); len(log) != 1 || log[0] != "target.drop(tab)" {
		t.Fatalf("drop log = %v, want exactly the drop (a dragging press never clicks)", log)
	}
	f.onHost(func() { active = f.app.HasActiveDrag() })
	if active {
		t.Fatal("the drop did not end the drag")
	}
	f.waitIdle()
	desktopWaitFor(t, "the preview to disappear", func() bool {
		ok := true
		f.onHost(func() { _, ok = WindowDebugBound(f.window, "preview") })
		return !ok
	})
}

// TestWin32InputRealWindowWheelScrolls proves WM_MOUSEWHEEL on a real
// window scrolls an OverflowYScroll().TrackScroll div (events.rs:562-613
// into div.rs:3816-3879): the screen position goes through
// ScreenToClient, the line delta is the system's wheel lines, and a
// flood of notches clamps at the max offset.
func TestWin32InputRealWindowWheelScrolls(t *testing.T) {
	handle := NewScrollHandle()
	f := openWin32InputWindow(t, "gpui input wheel", func(f *win32InputWindow) func(v *interactView, w *Window, cx *Context[interactView]) AnyElement {
		return func(v *interactView, w *Window, cx *Context[interactView]) AnyElement {
			return Div().Relative().SizeFull().Child(
				Div().ID("scroller").Absolute().Left(Px(40)).Top(Px(40)).W(Px(100)).H(Px(100)).OverflowYScroll().TrackScroll(handle).
					Child(Div().W(Px(100)).H(Px(300)).FlexShrink0()),
			).IntoElement()
		}
	})
	settings := f.h.MouseWheelSettings()
	if settings.WheelScrollLines == 0 || settings.WheelScrollLines > 100 {
		t.Fatalf("system wheel scroll lines = %d: the gate needs a line-scrolling wheel setting", settings.WheelScrollLines)
	}
	var max Point
	f.onHost(func() { max = handle.MaxOffset() })
	if max != (Point{Y: 200}) {
		t.Fatalf("opening max offset = %v, want (0,200)", max)
	}
	// screen converts a window-local logical point to the screen device
	// pixels WM_MOUSEWHEEL carries.
	screen := func(x, y float32) uintptr {
		pt := point{x: int32(x*f.scale + 0.5), y: int32(y*f.scale + 0.5)}
		f.onHost(func() { procClientToScreen.Call(f.window.Handle().Hwnd(), uintptr(unsafe.Pointer(&pt))) })
		return buttonLParam(int16(pt.x), int16(pt.y))
	}
	wheel := func(notches int16) uintptr { return uintptr(uint16(notches*wheelDelta)) << 16 }
	var offset Point
	readOffset := func() Point {
		f.onHost(func() { offset = handle.Offset() })
		return offset
	}

	f.mu.Lock()
	f.events = nil
	f.mu.Unlock()
	before := f.draws()
	f.send(wmMouseWheel, wheel(-1), screen(90, 90))
	var event *ScrollWheelEvent
	f.mu.Lock()
	for _, e := range f.events {
		if e, ok := e.(*ScrollWheelEvent); ok {
			event = e
		}
	}
	f.mu.Unlock()
	if event == nil {
		t.Fatal("WM_MOUSEWHEEL delivered no ScrollWheelEvent")
	}
	if inputAbs(event.Position.X-90) > 1 || inputAbs(event.Position.Y-90) > 1 {
		t.Errorf("wheel position = %v, want about (90,90) (ScreenToClient then logical)", event.Position)
	}
	if !event.Modifiers.Shift {
		if event.Delta != ScrollDeltaLines(0, -float32(settings.WheelScrollLines)) {
			t.Errorf("wheel delta = %+v, want %d lines down", event.Delta, settings.WheelScrollLines)
		}
	}
	desktopWaitFor(t, "one notch to scroll the tracked div", func() bool {
		return f.draws() > before && readOffset().Y < 0
	})
	f.waitIdle()
	// The line delta became whole line heights (ScrollDelta::pixel_delta
	// with the window's line height, div.rs:3827).
	// A Shift held anywhere on the machine (GetAsyncKeyState) turns the
	// notch into horizontal lines, which the vertical-only scroller still
	// applies to Y; the per-line check divides by whichever axis carries
	// the lines.
	wantY := readOffset().Y
	lines := event.Delta.Lines.Y
	if lines == 0 {
		lines = event.Delta.Lines.X
	}
	if wantY > -200 && lines != 0 {
		perLine := -wantY / -lines
		if perLine < 8 || perLine > 64 || perLine != float32(int(perLine)) {
			t.Errorf("one notch scrolled %v for %v lines: %v px per line is not a whole line height", -wantY, -lines, perLine)
		}
	}

	// Outside the scroller the wheel scrolls nothing.
	f.send(wmMouseWheel, wheel(-1), screen(300, 250))
	time.Sleep(50 * time.Millisecond)
	if got := readOffset(); got.Y != wantY {
		t.Fatalf("a wheel outside the scroller moved it: %v", got)
	}

	// A flood of notches clamps at the max offset on the next frame.
	f.send(wmMouseWheel, wheel(-100), screen(90, 90))
	desktopWaitFor(t, "the flood to clamp at the max offset", func() bool { return readOffset().Y == -200 })
}

// TestWin32InputRealWindowClickStateLifecycle checks the ClickState wiring
// of a real window: WM_SETTINGCHANGE with a double-click action refreshes
// the tolerances (events.rs:1220, window.rs:1392-1408), and WM_NCDESTROY
// forgets the window's state.
func TestWin32InputRealWindowClickStateLifecycle(t *testing.T) {
	h := desktopNewHost(t)
	handle, err := h.OpenWindow(WindowOptions{Title: "gpui input click state", Show: true})
	if err != nil {
		t.Fatalf("OpenWindow failed (real window expected): %v", err)
	}
	desktopWaitFor(t, "the window to be visible", handle.Visible)
	var record *hostWindow
	var interval time.Duration
	h.runForegroundSync(func() {
		record = handle.record()
		if record == nil {
			return
		}
		c := record.clickState()
		c.interval = -1
		procSendMessageW.Call(handle.Hwnd(), uintptr(wmSettingChange), spiSetDoubleClickTime, 0)
		interval = c.interval
	})
	if record == nil {
		t.Fatal("no live window record")
	}
	if interval <= 0 {
		t.Fatalf("WM_SETTINGCHANGE(SPI_SETDOUBLECLICKTIME) left the interval at %v", interval)
	}
	handle.Close()
	desktopWaitFor(t, "the window to be destroyed", func() bool { return !handle.Alive() })
	var kept bool
	h.runForegroundSync(func() { _, kept = win32ClickStates[record] })
	if kept {
		t.Fatal("WM_NCDESTROY did not forget the window's click state")
	}
}
