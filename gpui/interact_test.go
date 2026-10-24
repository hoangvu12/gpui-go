package gpui

// Base ticket 06's deterministic dispatch transcripts and the ports of
// the pinned div interactivity tests (R: = reference/ce-source/crates/
// gpui/src). Every test drives a test window with synthetic dispatched
// events; nothing touches the real mouse.

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// interactView renders whatever its build function returns each frame
// (the pinned tests' Render impls).
type interactView struct {
	build func(v *interactView, w *Window, cx *Context[interactView]) AnyElement
	// left is the movable target's left offset (the pinned
	// HoverListenerLayoutTestView::target_left).
	left float32
}

func (v *interactView) Render(w *Window, cx *Context[interactView]) AnyElement {
	return v.build(v, w, cx)
}

// interactFixture is one test app, window and root view.
type interactFixture struct {
	t      *testing.T
	app    *App
	window *Window
	view   Entity[interactView]
}

func newInteractFixture(t *testing.T, build func(v *interactView, w *Window, cx *Context[interactView]) AnyElement) *interactFixture {
	t.Helper()
	ta := NewTestApp()
	app := ta.App()
	window := NewTestWindow(app, Size{Width: 400, Height: 300})
	view := NewEntity(app, window.Scope(), func(v *interactView, cx *Context[interactView]) { v.build = build })
	window.SetRootView(ViewOf(view))
	f := &interactFixture{t: t, app: app, window: window, view: view}
	f.draw()
	return f
}

func (f *interactFixture) draw() {
	f.t.Helper()
	if _, err := DrawWindowFrame(f.window); err != nil {
		f.t.Fatalf("DrawWindowFrame: %v", err)
	}
}

func (f *interactFixture) move(x, y float32) {
	f.window.DispatchInput(&MouseMoveEvent{Position: Point{X: x, Y: y}}, f.app)
}

func (f *interactFixture) dragMove(x, y float32) {
	left := MouseButtonLeft
	f.window.DispatchInput(&MouseMoveEvent{Position: Point{X: x, Y: y}, PressedButton: &left}, f.app)
}

func (f *interactFixture) down(button MouseButton, x, y float32) {
	f.window.DispatchInput(&MouseDownEvent{Button: button, Position: Point{X: x, Y: y}, ClickCount: 1}, f.app)
}

func (f *interactFixture) up(button MouseButton, x, y float32) {
	f.window.DispatchInput(&MouseUpEvent{Button: button, Position: Point{X: x, Y: y}, ClickCount: 1}, f.app)
}

func (f *interactFixture) key(down bool, source string) {
	f.t.Helper()
	stroke, err := ParseKeystroke(source)
	if err != nil {
		f.t.Fatalf("ParseKeystroke(%q): %v", source, err)
	}
	if down {
		f.window.DispatchKeyEvent(&KeyDownEvent{Keystroke: stroke}, f.app)
	} else {
		f.window.DispatchKeyEvent(&KeyUpEvent{Keystroke: stroke}, f.app)
	}
}

// focusAndDraw is the pinned focus_and_draw helper (div.rs:5825-5833).
func (f *interactFixture) focusAndDraw(handle FocusHandle) {
	f.app.Update(func(*App) { f.window.Focus(handle) })
	f.draw()
}

// transcript records ordered dispatch observations.
type transcript struct{ entries []string }

func (r *transcript) add(format string, args ...any) {
	r.entries = append(r.entries, fmt.Sprintf(format, args...))
}

func (r *transcript) take() []string {
	out := r.entries
	r.entries = nil
	return out
}

func wantTranscript(t *testing.T, step string, got, want []string) {
	t.Helper()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s transcript:\n got  %s\n want %s", step, strings.Join(got, " | "), strings.Join(want, " | "))
	}
}

// quadCount counts the last frame's quads with the given solid
// background.
func quadCount(t *testing.T, w *Window, color Hsla) int {
	t.Helper()
	scene := LastDrawnScene(w)
	if scene == nil {
		t.Fatal("no drawn scene")
	}
	quads, err := scene.Quads()
	if err != nil {
		t.Fatal(err)
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

var (
	interactRed   = Hsla{H: 0, S: 1, L: 0.5, A: 1}
	interactBlue  = Hsla{H: 0.5, S: 1, L: 0.5, A: 1}
	interactGreen = Hsla{H: 0.25, S: 1, L: 0.5, A: 1}
)

// ---------------------------------------------------------------------------
// Click synthesis (div.rs:3447-3626, 3512-3577)
// ---------------------------------------------------------------------------

// TestInteractMouseClickDispatchTranscript pins the capture / bubble
// order of the mouse-down family and the click pairing: capture runs
// parent-first, bubble child-first in reverse registration order, and the
// click fires in the mouse-up bubble before the element's own up
// listeners, which registered earlier (div.rs:3291-3296 then 3579-3626;
// window.rs:6094-6113).
func TestInteractMouseClickDispatchTranscript(t *testing.T) {
	rec := &transcript{}
	f := newInteractFixture(t, func(v *interactView, w *Window, cx *Context[interactView]) AnyElement {
		child := Div().ID("a").Size(Px(50)).
			CaptureAnyMouseDown(func(e *MouseDownEvent, w *Window, app *App) { rec.add("a.capture-down") }).
			OnMouseDown(MouseButtonLeft, func(e *MouseDownEvent, w *Window, app *App) { rec.add("a.down-left") }).
			OnMouseDown(MouseButtonRight, func(e *MouseDownEvent, w *Window, app *App) { rec.add("a.down-right") }).
			CaptureAnyMouseUp(func(e *MouseUpEvent, w *Window, app *App) { rec.add("a.capture-up") }).
			OnAnyMouseUp(func(e *MouseUpEvent, w *Window, app *App) { rec.add("a.up") }).
			OnClick(func(e *ClickEvent, w *Window, app *App) {
				rec.add("a.click(%s,%d,%v)", e.Kind, e.ClickCount(), e.Position())
			}).
			OnAuxClick(func(e *ClickEvent, w *Window, app *App) {
				rec.add("a.aux(%v)", e.IsRightClick())
			})
		return Div().ID("p").SizeFull().
			CaptureAnyMouseDown(func(e *MouseDownEvent, w *Window, app *App) { rec.add("p.capture-down") }).
			OnAnyMouseDown(func(e *MouseDownEvent, w *Window, app *App) { rec.add("p.down") }).
			Child(child).IntoElement()
	})

	f.down(MouseButtonLeft, 10, 10)
	wantTranscript(t, "left down", rec.take(), []string{"p.capture-down", "a.capture-down", "a.down-left", "p.down"})
	f.draw()
	f.up(MouseButtonLeft, 12, 12)
	// Browser-core ticket 12 (divergence toward Chrome, spec D12): the
	// click fires after every mouseup listener — the DOM's order
	// (Chromium pointer_event_manager.cc:1283-1296 dispatches the click
	// after the whole pointerup/mouseup chain) — where the pin fired it
	// inside the up's listener traversal (click before mouseup). The
	// expectation changed with the ticket; never silently.
	wantTranscript(t, "left up", rec.take(), []string{"a.capture-up", "a.up", "a.click(Mouse,1,{12 12})"})

	// A right press and release is an aux click, not a click.
	f.draw()
	f.down(MouseButtonRight, 10, 10)
	wantTranscript(t, "right down", rec.take(), []string{"p.capture-down", "a.capture-down", "a.down-right", "p.down"})
	f.draw()
	f.up(MouseButtonRight, 10, 10)
	// The aux click also fires after the up listeners (ticket 12).
	wantTranscript(t, "right up", rec.take(), []string{"a.capture-up", "a.up", "a.aux(true)"})

	// A press inside released outside the element clicks nothing; the
	// pending press is dropped (div.rs:3591-3599).
	f.draw()
	f.down(MouseButtonLeft, 10, 10)
	rec.take()
	f.draw()
	f.up(MouseButtonLeft, 200, 200)
	wantTranscript(t, "release outside", rec.take(), nil)
	// ...and a later release inside does not resurrect it.
	f.draw()
	f.up(MouseButtonLeft, 10, 10)
	wantTranscript(t, "stale release", rec.take(), []string{"a.capture-up", "a.up"})

	// A press outside released inside clicks nothing either.
	f.draw()
	f.down(MouseButtonLeft, 200, 200)
	wantTranscript(t, "press outside", rec.take(), []string{"p.capture-down", "p.down"})
	f.draw()
	f.up(MouseButtonLeft, 10, 10)
	wantTranscript(t, "release inside", rec.take(), []string{"a.capture-up", "a.up"})
}

// TestInteractStopPropagationEndsDispatch checks that a child's
// StopPropagation in the bubble phase keeps the event from the parent
// (window.rs:6106-6111).
func TestInteractStopPropagationEndsDispatch(t *testing.T) {
	rec := &transcript{}
	f := newInteractFixture(t, func(v *interactView, w *Window, cx *Context[interactView]) AnyElement {
		return Div().SizeFull().
			OnAnyMouseDown(func(e *MouseDownEvent, w *Window, app *App) { rec.add("parent") }).
			Child(Div().Size(Px(50)).OnAnyMouseDown(func(e *MouseDownEvent, w *Window, app *App) {
				rec.add("child")
				app.StopPropagation()
			})).IntoElement()
	})
	f.down(MouseButtonLeft, 5, 5)
	wantTranscript(t, "down", rec.take(), []string{"child"})
}

// keyboardActivation is the pinned KeyboardActivationTest view
// (div.rs:5767-5821): two focusable, clickable elements.
func keyboardActivation(t *testing.T) (*interactFixture, *[]string, FocusHandle, FocusHandle) {
	t.Helper()
	var clicks []string
	var focusA, focusB FocusHandle
	ta := NewTestApp()
	app := ta.App()
	focusA, focusB = app.FocusHandle(), app.FocusHandle()
	window := NewTestWindow(app, Size{Width: 400, Height: 300})
	view := NewEntity(app, window.Scope(), func(v *interactView, cx *Context[interactView]) {
		v.build = func(v *interactView, w *Window, cx *Context[interactView]) AnyElement {
			return Div().SizeFull().
				Child(Div().ID("a").W(Px(50)).H(Px(50)).TrackFocus(focusA).
					OnClick(func(e *ClickEvent, w *Window, app *App) {
						clicks = append(clicks, fmt.Sprintf("a:%s", e.Kind))
					})).
				Child(Div().ID("b").W(Px(50)).H(Px(50)).TrackFocus(focusB).
					OnClick(func(e *ClickEvent, w *Window, app *App) {
						clicks = append(clicks, fmt.Sprintf("b:%s", e.Kind))
					})).
				IntoElement()
		}
	})
	window.SetRootView(ViewOf(view))
	f := &interactFixture{t: t, app: app, window: window, view: view}
	f.draw()
	return f, &clicks, focusA, focusB
}

// TestInteractKeyboardActivation ports the seven pinned keyboard
// activation tests (div.rs:5859-5970), updated where browser-core
// ticket 12 (spec D12) moved the port toward Chrome: Enter activates
// at the KEYDOWN (the DOM keypress's timing, html_element.cc:4318-
// 4324 — the click lands between the halves of the press, before any
// listener or focus change in between) where the pin paired the
// keydown with the keyup; Space still arms at the keydown and clicks
// at the keyup, so the focus-move and intervening-key guards keep
// their pin meaning for Space. The Enter subtests that asserted the
// pairing (the focus-move and intervening-key cancels) now hold for
// Space, with the Enter timing covered by the parity fixture
// activation-enter.
func TestInteractKeyboardActivation(t *testing.T) {
	t.Run("fires_click_on_same_element", func(t *testing.T) {
		f, clicks, a, _ := keyboardActivation(t)
		f.focusAndDraw(a)
		f.key(true, "enter")
		f.key(false, "enter")
		if !reflect.DeepEqual(*clicks, []string{"a:Keyboard"}) {
			t.Fatalf("clicks = %v, want [a:Keyboard]", *clicks)
		}
	})
	// Enter now fires at the keydown (ticket 12: Chrome's keypress), so
	// a focus move between the halves cannot retro-cancel it — the
	// pinned expectation ([], the keyup pairing's guard) became the
	// Space test below; the Enter timing is the divergence note above.
	t.Run("enter_fires_before_a_focus_change", func(t *testing.T) {
		f, clicks, a, b := keyboardActivation(t)
		f.focusAndDraw(a)
		f.key(true, "enter")
		f.focusAndDraw(b)
		f.key(false, "enter")
		if !reflect.DeepEqual(*clicks, []string{"a:Keyboard"}) {
			t.Fatalf("clicks = %v, want [a:Keyboard] (Enter clicks at the keydown, before the focus move)", *clicks)
		}
	})
	t.Run("space_does_not_leak_across_focus_change", func(t *testing.T) {
		f, clicks, a, b := keyboardActivation(t)
		f.focusAndDraw(a)
		f.key(true, "space")
		f.focusAndDraw(b)
		f.key(false, "space")
		if len(*clicks) != 0 {
			t.Fatalf("clicks = %v, want none", *clicks)
		}
	})
	t.Run("space_does_not_leak_when_focus_returns", func(t *testing.T) {
		f, clicks, a, b := keyboardActivation(t)
		f.focusAndDraw(a)
		f.key(true, "space")
		f.focusAndDraw(b)
		f.focusAndDraw(a)
		f.key(false, "space")
		if len(*clicks) != 0 {
			t.Fatalf("clicks = %v, want none", *clicks)
		}
	})
	t.Run("space_cleared_by_intervening_key_release", func(t *testing.T) {
		f, clicks, a, _ := keyboardActivation(t)
		f.focusAndDraw(a)
		f.key(true, "escape")
		f.key(true, "space")
		f.key(false, "escape")
		f.key(false, "space")
		if len(*clicks) != 0 {
			t.Fatalf("clicks = %v, want none", *clicks)
		}
	})
	// Ticket 12 (divergence toward Chrome): Chrome distinguishes the
	// activation keys — Space arms at the keydown and clicks at its own
	// keyup, Enter clicks at its keydown — so a mixed press (Space down,
	// Enter up) clicks nothing (html_element.cc:4314-4338 has an arm
	// per key). The pin paired any activation keydown with any other's
	// keyup; the expectation changed with the ticket.
	t.Run("distinguishes_space_and_enter", func(t *testing.T) {
		f, clicks, a, _ := keyboardActivation(t)
		f.focusAndDraw(a)
		f.key(true, "space")
		f.key(false, "enter")
		if len(*clicks) != 0 {
			t.Fatalf("clicks = %v, want none (the mixed press clicks nothing, Chrome's per-key arms)", *clicks)
		}
	})
	t.Run("space_cleared_by_intervening_keydown", func(t *testing.T) {
		f, clicks, a, _ := keyboardActivation(t)
		f.focusAndDraw(a)
		f.key(true, "space")
		f.key(true, "a")
		f.key(false, "space")
		if len(*clicks) != 0 {
			t.Fatalf("clicks = %v, want none", *clicks)
		}
	})
	// Enter fires at the keydown (ticket 12: Chrome's keypress follows
	// the keydown immediately — keyboard_event_manager.cc:330-389 — so
	// no later key can cancel it); the pin's intervening-keydown cancel
	// is the Space test above.
	t.Run("enter_fires_before_an_intervening_keydown", func(t *testing.T) {
		f, clicks, a, _ := keyboardActivation(t)
		f.focusAndDraw(a)
		f.key(true, "enter")
		f.key(true, "a")
		f.key(false, "enter")
		if !reflect.DeepEqual(*clicks, []string{"a:Keyboard"}) {
			t.Fatalf("clicks = %v, want [a:Keyboard] (Enter clicks at the keydown, before the 'a')", *clicks)
		}
	})
	t.Run("ignores_modified_keys", func(t *testing.T) {
		f, clicks, a, _ := keyboardActivation(t)
		f.focusAndDraw(a)
		f.key(true, "cmd-enter")
		f.key(false, "cmd-enter")
		if len(*clicks) != 0 {
			t.Fatalf("clicks = %v, want none", *clicks)
		}
	})
	// Port extension: a mouse press that focuses the element (its focus
	// transfer claims the innermost-wins flag, div.rs:3279 — ticket 12
	// made the claim internal, the event stays unprevented) must not
	// suppress the next keyboard activation — the pin resets
	// default_prevented per dispatched event (window.rs:5760).
	t.Run("after_mouse_focus", func(t *testing.T) {
		f, clicks, a, _ := keyboardActivation(t)
		f.down(MouseButtonLeft, 10, 10)
		f.draw()
		f.up(MouseButtonLeft, 10, 10)
		f.draw()
		if !a.IsFocused(f.window) {
			t.Fatal("the press did not focus the element")
		}
		f.key(true, "enter")
		f.key(false, "enter")
		if !reflect.DeepEqual(*clicks, []string{"a:Mouse", "a:Keyboard"}) {
			t.Fatalf("clicks = %v, want [a:Mouse a:Keyboard]", *clicks)
		}
	})
}

// TestInteractClickRequiresElementID checks the Go transcription of the
// pinned type restriction: on_click is a StatefulInteractiveElement
// method (div.rs:1814), so an id-less clickable div panics at prepaint.
func TestInteractClickRequiresElementID(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil || !strings.Contains(fmt.Sprint(r), "require an element ID") {
			t.Fatalf("recover = %v, want the element-id panic", r)
		}
	}()
	ta := NewTestApp()
	app := ta.App()
	window := NewTestWindow(app, Size{Width: 100, Height: 100})
	view := NewEntity(app, window.Scope(), func(v *interactView, cx *Context[interactView]) {
		v.build = func(v *interactView, w *Window, cx *Context[interactView]) AnyElement {
			return Div().SizeFull().Child(Div().Size(Px(10)).OnClick(func(*ClickEvent, *Window, *App) {})).IntoElement()
		}
	})
	window.SetRootView(ViewOf(view))
	_, _ = DrawWindowFrame(window)
}

// TestInteractClickEventAccessors pins the ClickEvent accessors
// (interactive.rs:305-438).
func TestInteractClickEventAccessors(t *testing.T) {
	var zero ClickEvent
	if !zero.IsKeyboard() || zero.Kind != ClickEventKeyboard {
		t.Fatal("the default ClickEvent is Keyboard (interactive.rs:299-303)")
	}
	keyboard := ClickEvent{Kind: ClickEventKeyboard, Keyboard: KeyboardClickEvent{Button: KeyboardButtonSpace, Bounds: Bounds{Origin: Point{X: 10, Y: 20}, Size: Size{Width: 30, Height: 40}}}}
	if keyboard.Position() != (Point{X: 10, Y: 60}) || keyboard.ClickCount() != 1 || !keyboard.StandardClick() ||
		keyboard.IsRightClick() || keyboard.FirstFocus() || keyboard.Modifiers() != (Modifiers{}) {
		t.Fatalf("keyboard accessors wrong: %+v", keyboard)
	}
	if _, ok := keyboard.MousePosition(); ok {
		t.Fatal("a keyboard click has no mouse position")
	}
	right := ClickEvent{Kind: ClickEventMouse, Mouse: MouseClickEvent{
		Down: MouseDownEvent{Button: MouseButtonRight, FirstMouse: true},
		Up:   MouseUpEvent{Button: MouseButtonRight, Position: Point{X: 3, Y: 4}, ClickCount: 2, Modifiers: Modifiers{Shift: true}},
	}}
	if !right.IsRightClick() || !right.IsSecondary() || right.StandardClick() || right.IsMiddleClick() ||
		right.ClickCount() != 2 || !right.FirstFocus() || !right.Modifiers().Shift || right.Position() != (Point{X: 3, Y: 4}) {
		t.Fatalf("right-click accessors wrong: %+v", right)
	}
	if p, ok := right.MousePosition(); !ok || p != (Point{X: 3, Y: 4}) {
		t.Fatal("a mouse click reports the mouse-up position")
	}
	middle := ClickEvent{Kind: ClickEventMouse, Mouse: MouseClickEvent{Down: MouseDownEvent{Button: MouseButtonMiddle}, Up: MouseUpEvent{Button: MouseButtonMiddle}}}
	if !middle.IsMiddleClick() || middle.IsSecondary() {
		t.Fatal("middle-click accessors wrong")
	}
	touch := ClickEvent{Kind: ClickEventTouch, Touch: TouchClickEvent{Position: Point{X: 1, Y: 2}, TapCount: 3, LongPress: true}}
	if touch.StandardClick() || !touch.IsSecondary() || touch.ClickCount() != 3 || touch.Position() != (Point{X: 1, Y: 2}) {
		t.Fatal("touch accessors wrong")
	}
}

// ---------------------------------------------------------------------------
// Mouse-down-out / up-out (div.rs:276-323)
// ---------------------------------------------------------------------------

// TestInteractMouseDownOutAndUpOut ports the first half of
// mouse_down_out_is_suppressed_while_window_prompt_is_active
// (div.rs:5609-5688; the port has no in-window prompt, so the
// suppression half is not applicable) and adds the up-out arm.
func TestInteractMouseDownOutAndUpOut(t *testing.T) {
	rec := &transcript{}
	f := newInteractFixture(t, func(v *interactView, w *Window, cx *Context[interactView]) AnyElement {
		return Div().SizeFull().Child(Div().ID("target").W(Px(50)).H(Px(50)).
			OnMouseDownOut(func(e *MouseDownEvent, w *Window, app *App) { rec.add("down-out(%v)", e.Position) }).
			OnMouseUpOut(MouseButtonLeft, func(e *MouseUpEvent, w *Window, app *App) { rec.add("up-out-left") }).
			OnMouseUpOut(MouseButtonRight, func(e *MouseUpEvent, w *Window, app *App) { rec.add("up-out-right") })).
			IntoElement()
	})
	f.down(MouseButtonLeft, 75, 75)
	wantTranscript(t, "down outside", rec.take(), []string{"down-out({75 75})"})
	f.down(MouseButtonLeft, 10, 10)
	wantTranscript(t, "down inside", rec.take(), nil)
	f.up(MouseButtonLeft, 75, 75)
	wantTranscript(t, "up outside", rec.take(), []string{"up-out-left"})
	f.up(MouseButtonLeft, 10, 10)
	wantTranscript(t, "up inside", rec.take(), nil)
}

// ---------------------------------------------------------------------------
// Hover listeners (div.rs:3629-3681)
// ---------------------------------------------------------------------------

func hoverListenerFixture(t *testing.T, left float32, transitions *[]bool) *interactFixture {
	t.Helper()
	ta := NewTestApp()
	app := ta.App()
	window := NewTestWindow(app, Size{Width: 400, Height: 300})
	view := NewEntity(app, window.Scope(), func(v *interactView, cx *Context[interactView]) {
		v.left = left
		v.build = func(v *interactView, w *Window, cx *Context[interactView]) AnyElement {
			return Div().Relative().SizeFull().Child(Div().ID("hover-target").Absolute().
				Left(Px(v.left)).Top0().Size(Px(20)).
				OnClick(func(*ClickEvent, *Window, *App) {}).
				OnHover(func(hovered *bool, w *Window, app *App) { *transitions = append(*transitions, *hovered) })).
				IntoElement()
		}
	})
	window.SetRootView(ViewOf(view))
	f := &interactFixture{t: t, app: app, window: window, view: view}
	f.draw()
	return f
}

// TestInteractHoverListenersFollowLayoutUnderStationaryMouse ports
// hover_listeners_update_when_layout_changes_under_stationary_mouse
// (div.rs:5209-5247).
func TestInteractHoverListenersFollowLayoutUnderStationaryMouse(t *testing.T) {
	var transitions []bool
	f := hoverListenerFixture(t, 40, &transitions)
	f.move(10, 10)
	if len(transitions) != 0 {
		t.Fatalf("transitions = %v, want none", transitions)
	}
	f.view.Update(f.app, func(v *interactView, cx *Context[interactView]) { v.left = 0; cx.Notify() })
	f.draw()
	if !reflect.DeepEqual(transitions, []bool{true}) {
		t.Fatalf("transitions = %v, want [true]", transitions)
	}
	f.view.Update(f.app, func(v *interactView, cx *Context[interactView]) { v.left = 40; cx.Notify() })
	f.draw()
	if !reflect.DeepEqual(transitions, []bool{true, false}) {
		t.Fatalf("transitions = %v, want [true false]", transitions)
	}
}

// TestInteractHoverListenersRemainHoveredDuringPress ports
// hover_listeners_remain_hovered_during_stationary_mouse_press
// (div.rs:5249-5301).
func TestInteractHoverListenersRemainHoveredDuringPress(t *testing.T) {
	var transitions []bool
	f := hoverListenerFixture(t, 0, &transitions)
	f.move(10, 10)
	if !reflect.DeepEqual(transitions, []bool{true}) {
		t.Fatalf("transitions = %v, want [true]", transitions)
	}
	f.down(MouseButtonLeft, 10, 10)
	f.draw()
	if !reflect.DeepEqual(transitions, []bool{true}) {
		t.Fatalf("after press: transitions = %v, want [true]", transitions)
	}
	f.up(MouseButtonLeft, 10, 10)
	f.draw()
	if !reflect.DeepEqual(transitions, []bool{true}) {
		t.Fatalf("after release: transitions = %v, want [true]", transitions)
	}
	// Leaving reports the end of the hover.
	f.move(200, 200)
	if !reflect.DeepEqual(transitions, []bool{true, false}) {
		t.Fatalf("after leave: transitions = %v, want [true false]", transitions)
	}
}

// ---------------------------------------------------------------------------
// State styles (div.rs:3904-4063)
// ---------------------------------------------------------------------------

// notifyCounter counts the root view's notifications.
type notifyCounter struct{ count int }

func countNotifications(t *testing.T, f *interactFixture) *Entity[notifyCounter] {
	t.Helper()
	counter := NewEntity(f.app, f.window.Scope(), func(c *notifyCounter, cx *Context[notifyCounter]) {
		cx.Observe(f.view, func(c *notifyCounter, _ Entity[interactView], cx *Context[notifyCounter]) { c.count++ })
	})
	return &counter
}

func notifications(f *interactFixture, counter *Entity[notifyCounter]) int {
	n := 0
	counter.Update(f.app, func(c *notifyCounter, cx *Context[notifyCounter]) { n = c.count })
	return n
}

// TestInteractGroupHoverUpdatesOnlyOnTransitions ports
// group_hover_styles_update_only_on_transitions (div.rs:5084-5184): the
// anonymous invisible member becomes visible while the group is hovered
// (its paint_hover_group_handler notifies on the transition), the
// stateful member's layout size follows the retained group-hover state,
// and moves that do not flip the group hover notify nothing. The pinned
// render count is observed as the root view's notification count, and
// the canvas paint count as the member's painted background quad.
func TestInteractGroupHoverUpdatesOnlyOnTransitions(t *testing.T) {
	f := newInteractFixture(t, func(v *interactView, w *Window, cx *Context[interactView]) AnyElement {
		return Div().SizeFull().Child(
			Div().Ml(Px(20)).Mt(Px(20)).Size(Px(50)).Relative().Group("hover-group").
				Child(Div().Absolute().SizeFull().Invisible().Bg(interactGreen).
					GroupHover("hover-group", func(s StyleRefinement) StyleRefinement {
						s.Visibility = Some(VisibilityVisible)
						return s
					})).
				Child(Div().ID("stateful-group-hover-target").Absolute().Top0().Left0().Size(Px(10)).
					DebugSelector("stateful").
					GroupHover("hover-group", func(s StyleRefinement) StyleRefinement {
						s.SizeWidth, s.SizeHeight = Some(PxLength(20)), Some(PxLength(20))
						return s
					})),
		).IntoElement()
	})
	counter := countNotifications(t, f)
	width := func() float32 {
		b, ok := WindowDebugBound(f.window, "stateful")
		if !ok {
			t.Fatal("stateful target not drawn")
		}
		return b.Size.Width
	}
	if quadCount(t, f.window, interactGreen) != 0 || width() != 10 {
		t.Fatalf("initial: green quads %d, width %v", quadCount(t, f.window, interactGreen), width())
	}

	step := func(x, y float32, wantNotes, wantGreen int, wantWidth float32) {
		t.Helper()
		f.move(x, y)
		f.draw()
		if n := notifications(f, counter); n != wantNotes {
			t.Fatalf("move (%v,%v): notifications = %d, want %d", x, y, n, wantNotes)
		}
		if g := quadCount(t, f.window, interactGreen); g != wantGreen {
			t.Fatalf("move (%v,%v): visible member quads = %d, want %d", x, y, g, wantGreen)
		}
		if got := width(); got != wantWidth {
			t.Fatalf("move (%v,%v): stateful width = %v, want %v", x, y, got, wantWidth)
		}
	}
	// Entering the group: the anonymous handler and the stateful tracker
	// both see the transition; the pin re-renders once (the coalesced
	// notify).
	step(25, 25, 1, 1, 20)
	step(30, 30, 1, 1, 20)
	step(5, 5, 2, 0, 10)
	step(10, 10, 2, 0, 10)
}

// TestInteractHoverActiveFocusStyles checks the state style refinements
// and their order (div.rs:3912-4041): focus styles, then hover, then
// active (each refines over the previous).
func TestInteractHoverActiveFocusStyles(t *testing.T) {
	var focus FocusHandle
	ta := NewTestApp()
	app := ta.App()
	focus = app.FocusHandle()
	window := NewTestWindow(app, Size{Width: 400, Height: 300})
	view := NewEntity(app, window.Scope(), func(v *interactView, cx *Context[interactView]) {
		v.build = func(v *interactView, w *Window, cx *Context[interactView]) AnyElement {
			return Div().SizeFull().Child(Div().ID("t").Size(Px(50)).Bg(interactRed).TrackFocus(focus).
				Hover(func(s StyleRefinement) StyleRefinement { return Div().Refine(s).Bg(interactBlue).Style() }).
				Active(func(s StyleRefinement) StyleRefinement { return Div().Refine(s).Bg(interactGreen).Style() })).
				IntoElement()
		}
	})
	window.SetRootView(ViewOf(view))
	f := &interactFixture{t: t, app: app, window: window, view: view}
	f.draw()
	colors := func() (red, blue, green int) {
		return quadCount(t, window, interactRed), quadCount(t, window, interactBlue), quadCount(t, window, interactGreen)
	}
	if r, b, g := colors(); r != 1 || b != 0 || g != 0 {
		t.Fatalf("initial colors r%d b%d g%d", r, b, g)
	}
	f.move(10, 10)
	f.draw()
	if r, b, g := colors(); r != 0 || b != 1 || g != 0 {
		t.Fatalf("hovered colors r%d b%d g%d", r, b, g)
	}
	f.down(MouseButtonLeft, 10, 10)
	f.draw()
	if r, b, g := colors(); r != 0 || b != 0 || g != 1 {
		t.Fatalf("pressed colors r%d b%d g%d", r, b, g)
	}
	f.up(MouseButtonLeft, 10, 10)
	f.draw()
	if r, b, g := colors(); r != 0 || b != 1 || g != 0 {
		t.Fatalf("released colors r%d b%d g%d", r, b, g)
	}
	if !focus.IsFocused(window) {
		t.Fatal("the press focused the element")
	}
	// Mouse-focused: focus-visible does not apply. A key down switches
	// the modality to the keyboard (window.rs:5748), which applies
	// focus-visible styles — but no longer clears hover (browser-core
	// ticket 10, spec D12: hover survives typing; Chromium's :hover has
	// no keyboard link, a deliberate divergence from window.rs:887-897
	// under D0). The hover style therefore stays applied.
	style := func() Style {
		var s Style
		app.Update(func(app *App) {
			s = Div().TrackFocus(focus).FocusVisible(func(s StyleRefinement) StyleRefinement { s.Opacity = Some[float32](0.5); return s }).
				computeStyleInternal(nil, nil, window, app)
		})
		return s
	}
	if s := style(); s.Opacity != nil {
		t.Fatalf("focus-visible applied after a mouse focus: %v", *s.Opacity)
	}
	app.Update(func(app *App) { window.beginPlatformInputDispatch(&KeyDownEvent{}, app) })
	if s := style(); s.Opacity == nil || *s.Opacity != 0.5 {
		t.Fatalf("focus-visible did not apply after keyboard input: %+v", s.Opacity)
	}
	f.draw()
	if r, b, g := colors(); r != 0 || b != 1 || g != 0 {
		t.Fatalf("hover must survive typing (browser-core ticket 10, D12): r%d b%d g%d", r, b, g)
	}
}

// ---------------------------------------------------------------------------
// Cursor styles (window.rs:1200-1213, 3844-3862, 5684-5693)
// ---------------------------------------------------------------------------

// TestInteractCursorStylePerHitbox checks the frame cursor resolution:
// the front-most hovered hitbox's request wins, the arrow applies
// elsewhere, and the reset runs when the hit test changes
// (window.rs:6083-6086).
func TestInteractCursorStylePerHitbox(t *testing.T) {
	f := newInteractFixture(t, func(v *interactView, w *Window, cx *Context[interactView]) AnyElement {
		return Div().SizeFull().Child(
			Div().Size(Px(100)).CursorPointer().Child(Div().Size(Px(40)).CursorText()),
		).IntoElement()
	})
	expect := func(x, y float32, want CursorStyle) {
		t.Helper()
		f.move(x, y)
		got, ok := resolvedCursorStyle(f.window)
		if !ok || got != want {
			t.Fatalf("cursor at (%v,%v) = %v (%v), want %v", x, y, got, ok, want)
		}
	}
	expect(10, 10, CursorIBeam)
	expect(60, 60, CursorPointingHand)
	expect(300, 250, CursorArrow)
	expect(20, 20, CursorIBeam)
}

// TestInteractWindowCursorTakesPrecedence checks set_window_cursor_style's
// precedence over every hitbox request (window.rs:1200-1213, 3852-3862).
func TestInteractWindowCursorTakesPrecedence(t *testing.T) {
	var windowCursor bool
	f := newInteractFixture(t, func(v *interactView, w *Window, cx *Context[interactView]) AnyElement {
		return Div().SizeFull().Child(Div().Size(Px(100)).CursorPointer()).Child(&cursorPainter{enabled: &windowCursor}).IntoElement()
	})
	f.move(10, 10)
	if got, _ := resolvedCursorStyle(f.window); got != CursorPointingHand {
		t.Fatalf("cursor = %v, want PointingHand", got)
	}
	windowCursor = true
	f.draw()
	f.move(300, 250) // a hit-test change resets the cursor
	if got, _ := resolvedCursorStyle(f.window); got != CursorResizeColumn {
		t.Fatalf("cursor = %v, want the window request ResizeColumn", got)
	}
}

// cursorPainter requests a window cursor at paint when enabled.
type cursorPainter struct{ enabled *bool }

func (c *cursorPainter) IntoElement() AnyElement {
	return CustomElement[struct{}, struct{}](&cursorPainterElement{enabled: c.enabled})
}

type cursorPainterElement struct{ enabled *bool }

func (c *cursorPainterElement) ID() (ElementID, bool)           { return ElementID{}, false }
func (c *cursorPainterElement) SourceLocation() *SourceLocation { return nil }
func (c *cursorPainterElement) RequestLayout(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App) (LayoutID, struct{}) {
	id, err := requestLayoutOf(w, DefaultStyle())
	if err != nil {
		panic(err)
	}
	return id, struct{}{}
}
func (c *cursorPainterElement) Prepaint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *struct{}, w *Window, app *App) struct{} {
	return struct{}{}
}
func (c *cursorPainterElement) Paint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *struct{}, prepaint *struct{}, w *Window, app *App) {
	if *c.enabled {
		w.SetWindowCursorStyle(CursorResizeColumn)
	}
}

// ---------------------------------------------------------------------------
// Drag sources (div.rs:703-743, 388-412, 3482-3510; window.rs:3439-3475)
// ---------------------------------------------------------------------------

type dragPayload struct{ name string }

type dragPreview struct{}

func (p *dragPreview) Render(w *Window, cx *Context[dragPreview]) AnyElement {
	return Div().Size(Px(30)).DebugSelector("preview").IntoElement()
}

// previewHost runs the active-drag preview steps the draw path owns
// (window.rs:3451-3456 then 3474-3475) from inside the frame.
type previewHost struct{ element *AnyElement }

func (p *previewHost) ID() (ElementID, bool)           { return ElementID{}, false }
func (p *previewHost) SourceLocation() *SourceLocation { return nil }
func (p *previewHost) RequestLayout(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App) (LayoutID, struct{}) {
	id, err := requestLayoutOf(w, DefaultStyle())
	if err != nil {
		panic(err)
	}
	return id, struct{}{}
}
func (p *previewHost) Prepaint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *struct{}, w *Window, app *App) struct{} {
	p.element = nil
	if element, ok := prepaintActiveDragPreview(w, app); ok {
		p.element = &element
	}
	return struct{}{}
}
func (p *previewHost) Paint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *struct{}, prepaint *struct{}, w *Window, app *App) {
	if p.element != nil {
		paintActiveDragPreview(*p.element, w, app)
	}
}

// TestInteractDragSourceTranscript drives a full element drag: the press,
// a move inside the threshold, the move that starts the drag (bubble,
// once), drag moves delivered to every OnDragMove listener in the
// capture phase, the window drag cursor, the preview at mouse - offset,
// and the drop that ends the drag without a click.
func TestInteractDragSourceTranscript(t *testing.T) {
	rec := &transcript{}
	f := newInteractFixture(t, func(v *interactView, w *Window, cx *Context[interactView]) AnyElement {
		source := Div().ID("source").Absolute().Left(Px(0)).Top(Px(0)).Size(Px(50)).CursorGrab().
			OnClick(func(*ClickEvent, *Window, *App) { rec.add("source.click") }).
			OnDrag(dragPayload{name: "tab"}, func(value *dragPayload, offset Point, w *Window, app *App) View {
				rec.add("constructor(%s,%v)", value.name, offset)
				return ViewOf(NewEntity(app, w.Scope(), func(*dragPreview, *Context[dragPreview]) {}))
			})
		target := Div().ID("target").Absolute().Left(Px(200)).Top(Px(100)).Size(Px(100)).
			OnDragMove(func(e *DragMoveEvent[dragPayload], w *Window, app *App) {
				rec.add("target.drag-move(%v,%s)", e.Event.Position, e.Drag(app).name)
			})
		target = OnDrop(target, func(value *dragPayload, e *MouseUpEvent, w *Window, app *App) {
			rec.add("target.drop(%s)", value.name)
		})
		return Div().Relative().SizeFull().Child(source).Child(target).Child(&previewHostElement{}).IntoElement()
	})
	f.down(MouseButtonLeft, 10, 10)
	f.draw()
	f.dragMove(11, 11) // |d| = 1.41 <= DRAG_THRESHOLD
	if f.app.HasActiveDrag() {
		t.Fatal("a move inside the drag threshold started the drag")
	}
	f.draw()
	f.dragMove(20, 20)
	wantTranscript(t, "drag start", rec.take(), []string{"constructor(tab,{20 20})"})
	drag := f.app.ActiveDrag()
	if drag == nil || drag.CursorStyle == nil || *drag.CursorStyle != CursorOpenHand || drag.CursorOffset != (Point{X: 20, Y: 20}) {
		t.Fatalf("active drag = %+v", drag)
	}
	f.draw()
	if b, ok := WindowDebugBound(f.window, "preview"); !ok || b.Origin != (Point{X: 0, Y: 0}) {
		t.Fatalf("preview bounds = %+v, %v; want origin mouse - offset = (0,0)", b, ok)
	}
	f.dragMove(250, 150)
	wantTranscript(t, "drag move", rec.take(), []string{"target.drag-move({250 150},tab)"})
	if got, _ := resolvedCursorStyle(f.window); got != CursorOpenHand {
		t.Fatalf("drag cursor = %v, want OpenHand (the window request of every hitbox div)", got)
	}
	f.draw()
	if b, ok := WindowDebugBound(f.window, "preview"); !ok || b.Origin != (Point{X: 230, Y: 130}) {
		t.Fatalf("preview bounds = %+v, %v; want origin (230,130)", b, ok)
	}
	f.up(MouseButtonLeft, 250, 150)
	wantTranscript(t, "drop", rec.take(), []string{"target.drop(tab)"})
	if f.app.HasActiveDrag() {
		t.Fatal("the drop ended the drag")
	}
	// The press that became a drag never clicks.
	f.draw()
	f.down(MouseButtonLeft, 10, 10)
	f.draw()
	f.up(MouseButtonLeft, 10, 10)
	wantTranscript(t, "plain click after the drag", rec.take(), []string{"source.click"})
}

// previewHostElement adapts previewHost as a child value.
type previewHostElement struct{}

func (p *previewHostElement) IntoElement() AnyElement {
	return CustomElement[struct{}, struct{}](&previewHost{})
}

// ---------------------------------------------------------------------------
// Pointer capture (window.rs:3098-3110, 6134-6136)
// ---------------------------------------------------------------------------

// TestInteractPointerCapture checks that a captured hitbox reports
// hovered outside its bounds until the mouse up releases the capture.
// Browser-core ticket 11 changed the timing to the browser's pending
// model: CapturePointer during the down only requests the capture —
// it becomes active (and gotpointercapture fires) at the next pointer
// event, like Chromium's ProcessPendingPointerCapture — so the active
// capture is asserted after the move, not after the press. The capture
// target here is an id-less element, so the capture is id-only (pin
// compat) and lives within this frame.
func TestInteractPointerCapture(t *testing.T) {
	rec := &transcript{}
	f := newInteractFixture(t, func(v *interactView, w *Window, cx *Context[interactView]) AnyElement {
		return Div().SizeFull().Child(Div().Size(Px(50)).
			OnMouseDownAll(func(e *MouseDownEvent, phase DispatchPhase, hitbox *Hitbox, w *Window, app *App) {
				if phase == DispatchBubble && hitbox.IsHovered(w) {
					w.CapturePointer(hitbox.ID())
				}
			}).
			OnMouseMoveAll(func(e *MouseMoveEvent, phase DispatchPhase, hitbox *Hitbox, w *Window, app *App) {
				if phase == DispatchBubble {
					rec.add("move(%v,%v)", e.Position, hitbox.IsHovered(w))
				}
			})).IntoElement()
	})
	f.move(200, 200)
	wantTranscript(t, "uncaptured", rec.take(), []string{"move({200 200},false)"})
	f.down(MouseButtonLeft, 10, 10)
	// The capture request is pending until the next pointer event —
	// gotpointercapture timing (pointer_event_manager.cc:1353-1365).
	f.dragMove(200, 200)
	if _, ok := f.window.CapturedHitbox(); !ok {
		t.Fatal("the capture did not become active at the next pointer event")
	}
	wantTranscript(t, "captured", rec.take(), []string{"move({200 200},true)"})
	f.up(MouseButtonLeft, 200, 200)
	if _, ok := f.window.CapturedHitbox(); ok {
		t.Fatal("the mouse up must release the capture")
	}
	f.move(201, 201)
	wantTranscript(t, "released", rec.take(), []string{"move({201 201},false)"})
}

// ---------------------------------------------------------------------------
// Hidden divs (div.rs:3021-3023) and tab groups (div.rs:5972-6035)
// ---------------------------------------------------------------------------

// TestInteractHiddenDivRegistersNothing checks the ticket-05 handoff: a
// hidden div — and its children — register no mouse or key listeners
// and no tab stop, while its hitbox is still inserted.
func TestInteractHiddenDivRegistersNothing(t *testing.T) {
	rec := &transcript{}
	ta := NewTestApp()
	app := ta.App()
	hiddenFocus := app.FocusHandle().TabStop(true)
	visibleFocus := app.FocusHandle().TabStop(true)
	window := NewTestWindow(app, Size{Width: 400, Height: 300})
	view := NewEntity(app, window.Scope(), func(v *interactView, cx *Context[interactView]) {
		v.build = func(v *interactView, w *Window, cx *Context[interactView]) AnyElement {
			hidden := Div().Size(Px(100)).Invisible().TrackFocus(hiddenFocus).
				OnAnyMouseDown(func(*MouseDownEvent, *Window, *App) { rec.add("hidden.down") }).
				OnKeyDown(func(*KeyDownEvent, *Window, *App) { rec.add("hidden.key") }).
				Child(Div().Size(Px(50)).OnAnyMouseDown(func(*MouseDownEvent, *Window, *App) { rec.add("hidden-child.down") }))
			return Div().SizeFull().
				OnAnyMouseDown(func(*MouseDownEvent, *Window, *App) { rec.add("root.down") }).
				Child(hidden).
				Child(Div().Size(Px(10)).TrackFocus(visibleFocus)).
				IntoElement()
		}
	})
	window.SetRootView(ViewOf(view))
	f := &interactFixture{t: t, app: app, window: window, view: view}
	f.draw()
	f.down(MouseButtonLeft, 10, 10)
	wantTranscript(t, "down over the hidden div", rec.take(), []string{"root.down"})
	if hiddenFocus.IsFocused(window) {
		t.Fatal("a hidden div must not take focus on press")
	}
	app.Update(func(*App) { window.FocusNext() })
	if !visibleFocus.IsFocused(window) {
		t.Fatal("FocusNext must skip the hidden div (no tab stop)")
	}
	// The hidden div still inserts its hitbox (prepaint, div.rs:2865),
	// as does its child: root, hidden, hidden child.
	if got := len(window.hitTestForDiagnostics()); got != 3 {
		t.Fatalf("hit test ids = %d, want 3 (root, hidden, hidden child)", got)
	}
}

// tabGroupFocus is the pinned TabGroupFocus view (div.rs:5972-5994).
func tabGroupGroup(container, item FocusHandle) *DivElement {
	return Div().TrackFocus(container).TabGroup().Child(Div().TrackFocus(item))
}

// TestInteractFocusNextFromTabGroupContainer ports
// focus_next_from_tab_group_container_enters_that_group
// (div.rs:5996-6035): it exercises the tab group the div opens and
// closes around its children.
func TestInteractFocusNextFromTabGroupContainer(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	groupA, itemA := app.FocusHandle(), app.FocusHandle().TabStop(true)
	groupB, itemB := app.FocusHandle(), app.FocusHandle().TabStop(true)
	window := NewTestWindow(app, Size{Width: 400, Height: 300})
	view := NewEntity(app, window.Scope(), func(v *interactView, cx *Context[interactView]) {
		v.build = func(v *interactView, w *Window, cx *Context[interactView]) AnyElement {
			return Div().Child(tabGroupGroup(groupA, itemA)).Child(tabGroupGroup(groupB, itemB)).IntoElement()
		}
	})
	window.SetRootView(ViewOf(view))
	if _, err := DrawWindowFrame(window); err != nil {
		t.Fatal(err)
	}
	app.Update(func(*App) {
		window.Focus(groupB)
		window.FocusNext()
	})
	if got := window.Focused(); !got.Ok || got.Handle.ID() != itemB.ID() {
		t.Fatalf("focused = %+v, want item_b %d", got, itemB.ID())
	}
}

// TestInteractSiblingKeyListenersStaySeparate checks that the div pops
// its dispatch node after its children (element.rs:505): a sibling's key
// listener is not on the focused element's dispatch path.
func TestInteractSiblingKeyListenersStaySeparate(t *testing.T) {
	rec := &transcript{}
	ta := NewTestApp()
	app := ta.App()
	focus := app.FocusHandle()
	window := NewTestWindow(app, Size{Width: 400, Height: 300})
	view := NewEntity(app, window.Scope(), func(v *interactView, cx *Context[interactView]) {
		v.build = func(v *interactView, w *Window, cx *Context[interactView]) AnyElement {
			return Div().OnKeyDown(func(*KeyDownEvent, *Window, *App) { rec.add("parent") }).
				Child(Div().OnKeyDown(func(*KeyDownEvent, *Window, *App) { rec.add("sibling") })).
				Child(Div().TrackFocus(focus).OnKeyDown(func(*KeyDownEvent, *Window, *App) { rec.add("focused") })).
				IntoElement()
		}
	})
	window.SetRootView(ViewOf(view))
	if _, err := DrawWindowFrame(window); err != nil {
		t.Fatal(err)
	}
	app.Update(func(*App) { window.Focus(focus) })
	stroke, _ := ParseKeystroke("a")
	window.DispatchKeyEvent(&KeyDownEvent{Keystroke: stroke}, app)
	wantTranscript(t, "key down", rec.take(), []string{"focused", "parent"})
}

// ---------------------------------------------------------------------------
// The Interactive mixin forwarding (fluent.go Interactive; button.rs:199)
// ---------------------------------------------------------------------------

type interactButton struct {
	Component[*interactButton]
}

func newInteractButton(id string) *interactButton {
	return BindComponent(&interactButton{}, Div().ID(id))
}

func (b *interactButton) IntoElement() AnyElement {
	return b.Interactivity().Refine(b.Style()).Extend(b.TakeChildren()).IntoElement()
}

// TestInteractMixinForwardsTicket06Builders checks that a component's
// Interactive mixin forwards the new builders onto its inner div.
func TestInteractMixinForwardsTicket06Builders(t *testing.T) {
	rec := &transcript{}
	f := newInteractFixture(t, func(v *interactView, w *Window, cx *Context[interactView]) AnyElement {
		b := newInteractButton("button").Size(Px(40)).
			Group("g").
			Hover(func(s StyleRefinement) StyleRefinement { return s }).
			Active(func(s StyleRefinement) StyleRefinement { return s }).
			OnMouseDown(MouseButtonLeft, func(*MouseDownEvent, *Window, *App) { rec.add("down") }).
			OnHover(func(h *bool, w *Window, app *App) { rec.add("hover(%v)", *h) }).
			OnClick(func(*ClickEvent, *Window, *App) { rec.add("click") })
		return Div().SizeFull().Child(b).IntoElement()
	})
	f.move(10, 10)
	f.down(MouseButtonLeft, 10, 10)
	f.draw()
	f.up(MouseButtonLeft, 10, 10)
	wantTranscript(t, "button", rec.take(), []string{"hover(true)", "down", "click"})
}
