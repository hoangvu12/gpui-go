package gpui

// Browser-core ticket 61's unit gates: the FLIP helper's reorder
// animation on the test clock (the change frame shows the First box,
// the mid-flight sample is between, the end state is the new layout
// with no transform), reduced motion completing at once, the
// interruption starting from the current visual position (the mid-
// flight box, like FLIP libraries and Chrome's transition
// retargeting), the optional scale about the transform-origin (the
// scene's transform group mid-flight, none after), the arming gate and
// the element-ID requirement.

import (
	"testing"
	"time"
)

// flipRowsView is the root view of the FLIP reorder tests: a column of
// keyed rows whose order the test drives through the view state,
// re-rendered on refresh like any view. The rows carry a click listener
// so the hit tests can ask which row a mid-flight point lands on.
type flipRowsView struct {
	order  []string
	flip   *Flip
	clicks []string
}

// Render implements Render: the rows in the current order, each placed
// in the view's Flip group.
func (v *flipRowsView) Render(w *Window, cx *Context[flipRowsView]) AnyElement {
	list := Div().Flex().FlexCol().ID("list")
	for _, id := range v.order {
		row := id
		list = list.Child(Div().
			ID(id).
			DebugSelector(id).
			Flip(v.flip).
			W(Px(120)).
			H(Px(40)).
			Bg(interactRed).
			OnClick(cx.Listener(func(v *flipRowsView, _ *ClickEvent, _ *Window, _ *Context[flipRowsView]) {
				v.clicks = append(v.clicks, row)
			})))
	}
	return list.IntoElement()
}

// openFlipRowsWindow opens a 400x300 test window rendering two 40px
// rows (ids "a" and "b", debug selectors of the same names) whose
// order the returned view state drives, placed in the given Flip.
func openFlipRowsWindow(t *testing.T, f *Flip) (*TestApp, *Window, *flipRowsView) {
	t.Helper()
	ta := NewTestApp()
	v := &flipRowsView{order: []string{"a", "b"}, flip: f}
	win := ta.OpenWindowWithOptions[flipRowsView, *flipRowsView](WindowOptions{Bounds: Bounds{Size: Size{Width: 400, Height: 300}}},
		func(state *flipRowsView, _ *Window, _ *Context[flipRowsView]) {
			*state = *v
			v = state
		})
	return ta, win.Window(), v
}

// rowY reads a row's debug-selector origin y from the last completed
// frame (the visual position: the translate fast path re-records the
// bounds at the shifted position).
func rowY(t *testing.T, ta *TestApp, w *Window, id string) float32 {
	t.Helper()
	cx := NewVisualTestContext(w, ta)
	b, ok := cx.DebugBounds(id)
	if !ok {
		t.Fatalf("row %q has no debug bounds", id)
	}
	return b.Origin.Y
}

// flipAt asserts a row's visual y within a hundredth of a pixel.
func flipAt(t *testing.T, ta *TestApp, w *Window, id string, want float32) {
	t.Helper()
	if got := rowY(t, ta, w, id); abs32(got-want) > 0.01 {
		t.Fatalf("row %q at y = %v, want %v", id, got, want)
	}
}

// reorder mutates the view's order, arms the group and redraws.
func reorder(t *testing.T, ta *TestApp, w *Window, v *flipRowsView, order ...string) {
	t.Helper()
	v.order = order
	v.flip.Arm()
	refreshWindow(ta, w)
}

// TestFlipReorderSlidesRowsFromOldToNewPositions is the acceptance
// reorder test: rows a and b swap, and the test clock shows a holding
// its old position in the change frame (the inverse translate), the
// exact midpoint at half the duration, the new layout at the end with
// the transform gone (the row paints at its layout position and no
// further frames are requested).
func TestFlipReorderSlidesRowsFromOldToNewPositions(t *testing.T) {
	ta, w, v := openFlipRowsWindow(t, NewFlip(280*time.Millisecond, EasingLinear()))

	// The initial layout: a at 0, b at 40 (the first placement adopts;
	// FLIP never animates an element's first appearance).
	flipAt(t, ta, w, "a", 0)
	flipAt(t, ta, w, "b", 40)

	// The reorder: the change frame still shows a at 0 and b at 40 —
	// the inverse translate on the new layout — and each moving row
	// requests one animation frame (the per-element request, like the
	// style-diff transition engine's).
	reorder(t, ta, w, v, "b", "a")
	flipAt(t, ta, w, "a", 0)
	flipAt(t, ta, w, "b", 40)
	if n := w.SimulateNextFrame(ta.App()); n != 2 {
		t.Fatalf("the change frame requested %v frames, want 2 (one per moving row)", n)
	}

	// Half way (140 of 280ms, linear): both rows are exactly between
	// their old and new positions.
	ta.AdvanceClock(140)
	w.SimulateNextFrame(ta.App())
	flipAt(t, ta, w, "a", 20)
	flipAt(t, ta, w, "b", 20)

	// The end (280ms): the new layout, a at 40 and b at 0, with no
	// transform left — the rows paint at their layout positions and the
	// finished move requests no further frames.
	ta.AdvanceClock(140)
	w.SimulateNextFrame(ta.App())
	flipAt(t, ta, w, "a", 40)
	flipAt(t, ta, w, "b", 0)
	if n := w.SimulateNextFrame(ta.App()); n != 0 {
		t.Fatalf("a finished move requested %v frames, want 0", n)
	}
}

// TestFlipReducedMotionCompletesImmediately: under App.MotionReduced
// the armed change lands at the new layout in the change frame itself
// and requests no animation frames (the motion engine's reduced-motion
// gate resolves the transition at once).
func TestFlipReducedMotionCompletesImmediately(t *testing.T) {
	ta, w, v := openFlipRowsWindow(t, NewFlip(280*time.Millisecond, EasingLinear()))
	ta.Update(func(app *App) { app.SetReduceMotion(true) })

	reorder(t, ta, w, v, "b", "a")
	flipAt(t, ta, w, "a", 40)
	flipAt(t, ta, w, "b", 0)
	if n := w.SimulateNextFrame(ta.App()); n != 0 {
		t.Fatalf("reduced motion requested %v frames, want 0", n)
	}
}

// TestFlipInterruptionStartsFromTheCurrentVisualPosition: a second
// reorder while the first move is mid-flight starts from where the row
// currently shows (the mid-flight sample), not from either resting
// position — FLIP libraries' and Chrome's retargeting behavior — and
// eases to the newest layout over a fresh full duration.
func TestFlipInterruptionStartsFromTheCurrentVisualPosition(t *testing.T) {
	ta, w, v := openFlipRowsWindow(t, NewFlip(280*time.Millisecond, EasingLinear()))

	// The first reorder: half way, a is at 20.
	reorder(t, ta, w, v, "b", "a")
	ta.AdvanceClock(140)
	w.SimulateNextFrame(ta.App())
	flipAt(t, ta, w, "a", 20)

	// The second reorder (back to the original order) interrupts the
	// move: the interruption frame shows a still at 20 — the current
	// visual position — with the new move's inverse measured from
	// there, not a jump to 0 or 40.
	reorder(t, ta, w, v, "a", "b")
	flipAt(t, ta, w, "a", 20)

	// The second move is a fresh full duration from 20 toward 0: half
	// way (140 of 280ms) a is at 10, and at the end it rests at 0.
	ta.AdvanceClock(140)
	w.SimulateNextFrame(ta.App())
	flipAt(t, ta, w, "a", 10)
	ta.AdvanceClock(140)
	w.SimulateNextFrame(ta.App())
	flipAt(t, ta, w, "a", 0)
	flipAt(t, ta, w, "b", 40)
	if n := w.SimulateNextFrame(ta.App()); n != 0 {
		t.Fatalf("a finished move requested %v frames, want 0", n)
	}
}

// TestFlipMidFlightClicksHitTheVisualPosition: the inverse translate
// rides the translation-only fast path — the element's own bounds and
// hitbox shift — so a click mid-flight lands on the row at its VISUAL
// position (b), not the row the post-swap layout puts there (a); and a
// translate-only move emits no scene transform group.
func TestFlipMidFlightClicksHitTheVisualPosition(t *testing.T) {
	ta, w, v := openFlipRowsWindow(t, NewFlip(280*time.Millisecond, EasingLinear()))
	reorder(t, ta, w, v, "b", "a")
	// 70 of 280ms (linear, progress 0.25): a shows at 10 (span 10-50),
	// b at 30 (span 30-70); the swapped layout alone would put a at
	// 40 (span 40-80) and b at 0 (span 0-40).
	ta.AdvanceClock(70)
	w.SimulateNextFrame(ta.App())
	flipAt(t, ta, w, "a", 10)
	flipAt(t, ta, w, "b", 30)
	// The point (60, 55) is in b's visual span (30-70) and in a's layout
	// span (40-80), not in a's visual span (10-50).
	cx := NewVisualTestContext(w, ta)
	cx.SimulateClick(Point{X: 60, Y: 55}, Modifiers{})
	if len(v.clicks) != 1 || v.clicks[0] != "b" {
		t.Fatalf("mid-flight clicks hit %v, want [b] (the visual position)", v.clicks)
	}
	// A translate-only move rides the fast path: no transform group in
	// the scene while it runs.
	scene := LastDrawnScene(w)
	if scene == nil {
		t.Fatal("no drawn scene")
	}
	groups, err := scene.EffectGroups()
	if err != nil {
		t.Fatalf("EffectGroups: %v", err)
	}
	for _, g := range groups {
		if g.HasTransform {
			t.Fatalf("a translate-only FLIP move emitted a transform group (%+v), want the offset fast path", g)
		}
	}
}

// TestFlipUnarmedChangeFollowsAtOnce: a layout change without an Arm
// follows the layout immediately (the arming gate: scrolls, resizes
// and other views' changes never slide).
func TestFlipUnarmedChangeFollowsAtOnce(t *testing.T) {
	ta, w, v := openFlipRowsWindow(t, NewFlip(280*time.Millisecond, EasingLinear()))
	v.order = []string{"b", "a"}
	refreshWindow(ta, w)
	flipAt(t, ta, w, "a", 40)
	flipAt(t, ta, w, "b", 0)
	if n := w.SimulateNextFrame(ta.App()); n != 0 {
		t.Fatalf("an unarmed change requested %v frames, want 0", n)
	}
}

// flipBoxView is the root view of the scale test: one absolutely
// positioned box whose size the test changes.
type flipBoxView struct {
	width, height float32
	flip          *Flip
}

// Render implements Render: the box at (40, 30) with the current size,
// placed in the view's Flip group.
func (v *flipBoxView) Render(w *Window, cx *Context[flipBoxView]) AnyElement {
	box := Div().
		ID("box").
		DebugSelector("box").
		Absolute().
		Left(Px(40)).
		Top(Px(30)).
		W(Px(v.width)).
		H(Px(v.height)).
		Bg(interactRed).
		Flip(v.flip)
	return Div().Flex().ID("root").SizeFull().Relative().Child(box).IntoElement()
}

// openFlipBoxWindow opens a 300x200 test window rendering one box at
// (40, 30) of the given initial size, placed in the given Flip.
func openFlipBoxWindow(t *testing.T, f *Flip, width, height float32) (*TestApp, *Window, *flipBoxView) {
	t.Helper()
	ta := NewTestApp()
	v := &flipBoxView{width: width, height: height, flip: f}
	win := ta.OpenWindowWithOptions[flipBoxView, *flipBoxView](WindowOptions{Bounds: Bounds{Size: Size{Width: 300, Height: 200}}},
		func(state *flipBoxView, _ *Window, _ *Context[flipBoxView]) {
			*state = *v
			v = state
		})
	return ta, win.Window(), v
}

// TestFlipScaleAnimatesTheBoxAboutTheOrigin: with WithScale, a size
// change reproduces the First box with a scale about the
// transform-origin (the default centre) and eases the whole box to the
// new layout — the visual box interpolates linearly — riding the
// transform effect group while it moves and leaving no group (no
// transform) once it settles.
func TestFlipScaleAnimatesTheBoxAboutTheOrigin(t *testing.T) {
	ta, w, v := openFlipBoxWindow(t, NewFlip(400*time.Millisecond, EasingLinear()).WithScale(), 80, 40)
	boundsOf := func() Bounds {
		t.Helper()
		cx := NewVisualTestContext(w, ta)
		b, ok := cx.DebugBounds("box")
		if !ok {
			t.Fatal("the box has no debug bounds")
		}
		return b
	}
	groups := func() int {
		t.Helper()
		scene := LastDrawnScene(w)
		if scene == nil {
			t.Fatal("no drawn scene")
		}
		list, err := scene.EffectGroups()
		if err != nil {
			t.Fatalf("EffectGroups: %v", err)
		}
		n := 0
		for _, g := range list {
			if g.HasTransform {
				n++
			}
		}
		return n
	}

	// The initial layout: 80x40 at (40, 30).
	if b := boundsOf(); b.Origin.X != 40 || b.Origin.Y != 30 || b.Size.Width != 80 || b.Size.Height != 40 {
		t.Fatalf("initial box = %+v, want (40, 30, 80, 40)", b)
	}

	// The armed change to 40x20: the change frame still shows the First
	// box (the inverse scale 2x2 with its origin-adjusted translate),
	// through the transform effect group.
	v.width, v.height = 40, 20
	v.flip.Arm()
	refreshWindow(ta, w)
	if b := boundsOf(); b.Origin.X != 40 || b.Origin.Y != 30 || b.Size.Width != 80 || b.Size.Height != 40 {
		t.Fatalf("box in the change frame = %+v, want the First box (40, 30, 80, 40)", b)
	}
	if n := groups(); n != 2 {
		t.Fatalf("the scaling move carries %d transform group records, want a start/end pair", n)
	}
	w.SimulateNextFrame(ta.App())

	// Half way (200 of 400ms, linear): the visual box interpolates
	// linearly between the boxes — 60x30 at (40, 30) — still grouped.
	ta.AdvanceClock(200)
	w.SimulateNextFrame(ta.App())
	b := boundsOf()
	if abs32(b.Origin.X-40) > 0.01 || abs32(b.Origin.Y-30) > 0.01 ||
		abs32(b.Size.Width-60) > 0.01 || abs32(b.Size.Height-30) > 0.01 {
		t.Fatalf("box at half = %+v, want (40, 30, 60, 30)", b)
	}
	if n := groups(); n != 2 {
		t.Fatalf("the scaling move carries %d transform group records at half, want a start/end pair", n)
	}

	// The end (400ms): the new layout 40x20 with the transform gone —
	// no transform group in the settled scene and no frames requested.
	ta.AdvanceClock(200)
	w.SimulateNextFrame(ta.App())
	if b := boundsOf(); b.Origin.X != 40 || b.Origin.Y != 30 || b.Size.Width != 40 || b.Size.Height != 20 {
		t.Fatalf("box at the end = %+v, want the new layout (40, 30, 40, 20)", b)
	}
	if n := groups(); n != 0 {
		t.Fatalf("the settled scene carries %d transform group records, want none (a zero transform)", n)
	}
	if n := w.SimulateNextFrame(ta.App()); n != 0 {
		t.Fatalf("a finished move requested %v frames, want 0", n)
	}
}

// TestFlipRequiresAnElementID: the FLIP bookkeeping keys its state on
// element identity; an id-less element with a Flip panics with the
// documented message.
func TestFlipRequiresAnElementID(t *testing.T) {
	defer func() {
		r := recover()
		msg, _ := r.(string)
		if msg == "" || !containsStr(msg, "Flip requires an element ID") {
			t.Fatalf("panic = %v, want the missing-element-ID message", r)
		}
	}()
	ta := NewTestApp()
	f := NewFlip(280*time.Millisecond, EasingLinear())
	ta.OpenWindowWithOptions[flipIdlessView, *flipIdlessView](WindowOptions{Bounds: Bounds{Size: Size{Width: 400, Height: 300}}},
		func(state *flipIdlessView, _ *Window, _ *Context[flipIdlessView]) {
			state.flip = f
		})
	ta.RunUntilParked()
}

// flipIdlessView is the id-less Flip element of the panic test.
type flipIdlessView struct {
	flip *Flip
}

// Render implements Render: a div with a Flip but no ID.
func (v *flipIdlessView) Render(w *Window, cx *Context[flipIdlessView]) AnyElement {
	return Div().Flex().Flip(v.flip).Child(Div().W(Px(40)).H(Px(40))).IntoElement()
}
