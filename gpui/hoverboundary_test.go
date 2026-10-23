package gpui

// Browser-core ticket 10's hover boundary gates (headless, simulated
// input only): the DOM pointerenter/pointerleave order across nested
// elements, the window-exit event that clears hover on every element,
// hover surviving key presses (spec D12), and the layout-change
// boundary sweep.

import (
	"testing"
)

// hoverFixture is one test window rendering a nested hover tree: the
// full-window root, a parent p at (100,100) whose width the view
// controls, and two 40x40 children c1 (at p's left) and c2 (40px right
// of c1), all recording pointer enter/leave into a transcript.
type hoverFixture struct {
	t        *testing.T
	app      *App
	window   *Window
	view     Entity[interactView]
	events   *transcript
	useStyle bool
}

// hoverTree builds the nested tree. p's width is the view's left field
// (so a layout change can move its hitbox under the stationary
// pointer); its children stack side by side from its top-left (the
// pin's flex default, no gap).
func hoverTree(f *hoverFixture, v *interactView, w *Window, cx *Context[interactView]) AnyElement {
	bind := func(target string) (enter func(*PointerEnterEvent, *Window, *App), leave func(*PointerLeaveEvent, *Window, *App)) {
		enter = func(e *PointerEnterEvent, w *Window, app *App) { f.events.add("enter:%s", target) }
		leave = func(e *PointerLeaveEvent, w *Window, app *App) { f.events.add("leave:%s", target) }
		return enter, leave
	}
	leaf := func(id string, x float32) *DivElement {
		enter, leave := bind(id)
		d := Div().ID(id).DebugSelector(id).
			// The offsets resolve against p (the nearest positioned
			// ancestor): Top0 is p's top edge.
			Absolute().Left(Px(x)).Top0().Size(Px(40)).
			OnPointerEnter(enter).OnPointerLeave(leave)
		if f.useStyle {
			d = d.Bg(interactRed).Hover(func(s StyleRefinement) StyleRefinement {
				return Div().Refine(s).Bg(interactBlue).Style()
			})
		}
		return d
	}
	rootEnter, rootLeave := bind("root")
	pEnter, pLeave := bind("p")
	return Div().ID("root").DebugSelector("root").SizeFull().Relative().
		OnPointerEnter(rootEnter).OnPointerLeave(rootLeave).
		Child(Div().ID("p").DebugSelector("p").
			Absolute().Left(Px(100)).Top(Px(100)).Size(Px(v.left), Px(100)).
			OnPointerEnter(pEnter).OnPointerLeave(pLeave).
			// The children resolve their absolute offsets against p, the
			// nearest positioned ancestor: c1 at p's origin, c2 40px right.
			Child(leaf("c1", 0).IntoElement()).
			Child(leaf("c2", 40).IntoElement())).
		IntoElement()
}

func newHoverFixture(t *testing.T, useStyle bool, pWidth float32) *hoverFixture {
	t.Helper()
	ta := NewTestApp()
	app := ta.App()
	window := NewTestWindow(app, Size{Width: 400, Height: 300})
	f := &hoverFixture{t: t, app: app, window: window, events: &transcript{}, useStyle: useStyle}
	view := NewEntity(app, window.Scope(), func(v *interactView, cx *Context[interactView]) {
		v.left = pWidth
		v.build = func(v *interactView, w *Window, cx *Context[interactView]) AnyElement {
			return hoverTree(f, v, w, cx)
		}
	})
	f.view = view
	window.SetRootView(ViewOf(view))
	f.draw()
	return f
}

func (f *hoverFixture) draw() {
	f.t.Helper()
	if _, err := DrawWindowFrame(f.window); err != nil {
		f.t.Fatalf("DrawWindowFrame: %v", err)
	}
}

func (f *hoverFixture) move(x, y float32) {
	f.window.DispatchInput(&MouseMoveEvent{Position: Point{X: x, Y: y}}, f.app)
}

func (f *hoverFixture) exitWindow() {
	f.window.DispatchInput(&MouseExitEvent{}, f.app)
}

func (f *hoverFixture) take() []string { return f.events.take() }

// TestHoverBoundaryOrderAcrossNestedElements is ticket 10's first
// acceptance criterion, at unit level (the Chrome-checked twin is the
// hover parity fixture): moving from outside into a child fires enter
// events outermost-first (root, p, c1), a sibling-to-sibling move
// fires the leave on the exited child and the enter on the entered
// child, and moving back out fires leave events innermost-first (c2,
// p). Chromium boundary_event_dispatcher.cc:190-193 (leave,
// child-to-parent) and :211-214 (enter, parent-to-child).
func TestHoverBoundaryOrderAcrossNestedElements(t *testing.T) {
	f := newHoverFixture(t, false, 100)

	// Outside every element (over the root's background).
	f.move(20, 20)
	wantTranscript(t, "enter the root", f.take(), []string{"enter:root"})

	// Into c1: enters fire outermost-first on the entered chain.
	f.move(115, 115)
	wantTranscript(t, "enter c1", f.take(), []string{"enter:p", "enter:c1"})

	// c1 to sibling c2: leave the exited child, enter the entered
	// child; p stays hovered (no events for it).
	f.move(155, 115)
	wantTranscript(t, "c1 to c2", f.take(), []string{"leave:c1", "enter:c2"})

	// Back out to the root's background: leaves fire innermost-first.
	f.move(20, 20)
	wantTranscript(t, "back out", f.take(), []string{"leave:c2", "leave:p"})

	// Into p directly (below the children): enter p only.
	f.move(105, 170)
	wantTranscript(t, "enter p below the children", f.take(), []string{"enter:p"})
}

// TestHoverBoundaryLeaveOnWindowExit is ticket 10's second acceptance
// criterion, at unit level: the pointer leaving the window (the
// MouseExitEvent WM_MOUSELEAVE produces) fires leave on every hovered
// element innermost-first and clears the hover styles. Chromium's
// kMouseLeave clears hover with a null target (event_handler.cc:
// 1158-1165).
func TestHoverBoundaryLeaveOnWindowExit(t *testing.T) {
	f := newHoverFixture(t, true, 100)

	colors := func() (red, blue int) {
		return quadCount(t, f.window, interactRed), quadCount(t, f.window, interactBlue)
	}
	if red, blue := colors(); red != 2 || blue != 0 {
		t.Fatalf("initial colors r%d b%d, want two un-hovered children", red, blue)
	}

	f.move(115, 115)
	f.draw()
	if red, blue := colors(); red != 1 || blue != 1 {
		t.Fatalf("hovered colors r%d b%d, want c1 hovered", red, blue)
	}
	wantTranscript(t, "enter c1", f.take(), []string{"enter:root", "enter:p", "enter:c1"})

	// The pointer leaves the window: hover clears everywhere, leaves
	// fire innermost-first down the whole hovered chain.
	f.exitWindow()
	f.draw()
	if red, blue := colors(); red != 2 || blue != 0 {
		t.Fatalf("after the exit r%d b%d, want hover cleared on every element", red, blue)
	}
	wantTranscript(t, "leave the window", f.take(), []string{"leave:c1", "leave:p", "leave:root"})

	// The tracked position moved off window: the hit test stays empty
	// until the pointer moves back in.
	if got := f.window.TrackedMousePosition(); got != offWindowMousePosition {
		t.Fatalf("tracked position after the exit = %+v, want the off-window sentinel", got)
	}

	// Moving back in re-enters from scratch.
	f.move(155, 115)
	f.draw()
	if red, blue := colors(); red != 1 || blue != 1 {
		t.Fatalf("re-entered colors r%d b%d, want c2 hovered", red, blue)
	}
	wantTranscript(t, "re-enter", f.take(), []string{"enter:root", "enter:p", "enter:c2"})
}

// TestHoverSurvivesKeystrokes is ticket 10's third acceptance
// criterion, at unit level (spec D12): typing while hovering keeps the
// hover style and fires no boundary events. The pointer stays still
// while the keystrokes arrive.
func TestHoverSurvivesKeystrokes(t *testing.T) {
	f := newHoverFixture(t, true, 100)

	f.move(115, 115)
	f.draw()
	if red, blue := quadCount(t, f.window, interactRed), quadCount(t, f.window, interactBlue); red != 1 || blue != 1 {
		t.Fatalf("hovered colors r%d b%d, want c1 hovered", red, blue)
	}
	wantTranscript(t, "enter c1", f.take(), []string{"enter:root", "enter:p", "enter:c1"})

	// Typing: a key down, its auto-repeat, another key, then release.
	for _, source := range []string{"a", "a", "b"} {
		stroke, err := ParseKeystroke(source)
		if err != nil {
			t.Fatalf("ParseKeystroke(%q): %v", source, err)
		}
		f.window.DispatchKeyEvent(&KeyDownEvent{Keystroke: stroke, IsHeld: source == "a"}, f.app)
	}
	stroke, err := ParseKeystroke("a")
	if err != nil {
		t.Fatal(err)
	}
	f.window.DispatchKeyEvent(&KeyUpEvent{Keystroke: stroke}, f.app)
	f.draw()
	if red, blue := quadCount(t, f.window, interactRed), quadCount(t, f.window, interactBlue); red != 1 || blue != 1 {
		t.Fatalf("after typing r%d b%d, want c1 still hovered (D12: hover survives typing)", red, blue)
	}
	// No pointer motion, no hover transition: no boundary events.
	if got := f.take(); len(got) != 0 {
		t.Fatalf("boundary events during typing = %v, want none", got)
	}
}

// TestHoverBoundaryLayoutChangeUnderStationaryMouse checks the
// deferred sweep: an element that moves under a stationary mouse fires
// enter (and, moving away, leave) in the boundary order without any
// pointer motion.
func TestHoverBoundaryLayoutChangeUnderStationaryMouse(t *testing.T) {
	f := newHoverFixture(t, false, 100)
	// The pointer sits over the root background, right of p.
	f.move(300, 115)
	wantTranscript(t, "enter the root", f.take(), []string{"enter:root"})

	// Widen p so its hitbox moves under the stationary pointer: the
	// deferred sweep reports the enter.
	f.view.Update(f.app, func(v *interactView, cx *Context[interactView]) {
		v.left = 220
		cx.Notify()
	})
	f.draw()
	wantTranscript(t, "layout moves under the mouse", f.take(), []string{"enter:p"})

	// Shrink p back: the leave fires the same way.
	f.view.Update(f.app, func(v *interactView, cx *Context[interactView]) {
		v.left = 100
		cx.Notify()
	})
	f.draw()
	wantTranscript(t, "layout moves away from the mouse", f.take(), []string{"leave:p"})
}
