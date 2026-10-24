package gpui

// Ticket 35 (browser-core): position: fixed — a fixed element's
// containing block is the window root (the port's
// initial-containing-block stand-in), no matter its ancestors; it is
// placed relative to the window (its insets resolve against the root's
// padding box), it is not scrolled by any ancestor scroller (the
// deferred prepaint runs under a zeroed element offset, outside every
// scroller's content mask), and it paints above the whole in-flow
// scene (the deferred draw), so it rides over later siblings and over
// content scrolling under it. Tested through the public Div API on the
// deterministic test window; the Chrome-parity twin is
// internal/parity/fixtures/fixed.

import (
	"testing"
)

// fixedBox builds one fixed div; the caller adds the insets and size.
func fixedBox(id string, color uint32) *DivElement {
	return Div().ID(id).DebugSelector(id).Fixed().Bg(Rgb(color))
}

// newFixedWindow opens one test app and window whose root view is set
// by the caller.
func newFixedWindow(t *testing.T) (*TestApp, *App, *Window) {
	t.Helper()
	ta := NewTestApp()
	app := ta.App()
	window := NewTestWindow(app, Size{Width: 400, Height: 300})
	t.Cleanup(func() {
		CloseTestWindow(window)
		releaseAppImageCodec(app)
	})
	return ta, app, window
}

// drawBoundsOf draws one frame and returns the selector's bounds.
func drawBoundsOf(t *testing.T, window *Window, selector string) Bounds {
	t.Helper()
	if _, err := DrawWindowFrame(window); err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}
	bounds, ok := WindowDebugBound(window, selector)
	if !ok {
		t.Fatalf("the selector %q has no bounds", selector)
	}
	return bounds
}

// fixedPlainView: a root with an in-flow child holding the fixed box.
type fixedPlainView struct{}

func (v *fixedPlainView) Render(w *Window, cx *Context[fixedPlainView]) AnyElement {
	return Div().ID("root").SizeFull().Flex().Bg(Rgb(0xffffff)).
		Child(Div().ID("flow").W(Px(100)).H(Px(100)).Bg(Rgb(0x94a3b8)).
			Child(fixedBox("fixed", 0xdc2626).
				Left(Px(40)).Top(Px(20)).W(Px(50)).H(Px(30)))).
		IntoElement()
}

// TestFixedResolvesAgainstTheWindowRoot: a fixed box nested inside an
// in-flow parent places at its window coordinates — the deferral
// carries it to the root's scope and the insets resolve against the
// root's padding box (the viewport; ContainerForFixedPosition walks
// PAST every positioned ancestor, layout_object.cc:1885-1892).
func TestFixedResolvesAgainstTheWindowRoot(t *testing.T) {
	_, app, window := newFixedWindow(t)
	window.SetRootView(ViewOf(NewEntity(app, window.Scope(), func(v *fixedPlainView, cx *Context[fixedPlainView]) {})))
	bounds := drawBoundsOf(t, window, "fixed")
	if bounds.Origin.X != 40 || bounds.Origin.Y != 20 {
		t.Fatalf("the fixed box placed at (%v, %v), want the window coordinates (40, 20)", bounds.Origin.X, bounds.Origin.Y)
	}
	if bounds.Size.Width != 50 || bounds.Size.Height != 30 {
		t.Fatalf("the fixed box sized %vx%v, want 50x30", bounds.Size.Width, bounds.Size.Height)
	}
}

// fixedChainedView: a relative ancestor holding a static parent
// holding the fixed box.
type fixedChainedView struct{}

func (v *fixedChainedView) Render(w *Window, cx *Context[fixedChainedView]) AnyElement {
	return Div().ID("root").SizeFull().Flex().Bg(Rgb(0xffffff)).
		Child(Div().ID("ancestor").W(Px(200)).H(Px(200)).Relative().Bg(Rgb(0xf8fafc)).
			Child(Div().ID("static-parent").W(Px(100)).H(Px(100)).Static().Bg(Rgb(0xe2e8f0)).
				Child(fixedBox("fixed", 0xdc2626).
					Left(Px(300)).Top(Px(200)).W(Px(60)).H(Px(40))))).
		IntoElement()
}

// TestFixedEscapesRelativeAndStaticAncestors: the fixed box under a
// relative ancestor holding a static parent still places at its window
// coordinates — the static/positioned chain cannot contain it (only
// transforms, filters and containment do, ComputeIsFixedContainer
// layout_object.cc:2009-2067; the port has no transforms, so the
// window root is always the containing block).
func TestFixedEscapesRelativeAndStaticAncestors(t *testing.T) {
	_, app, window := newFixedWindow(t)
	window.SetRootView(ViewOf(NewEntity(app, window.Scope(), func(v *fixedChainedView, cx *Context[fixedChainedView]) {})))
	bounds := drawBoundsOf(t, window, "fixed")
	if bounds.Origin.X != 300 || bounds.Origin.Y != 200 {
		t.Fatalf("the fixed box under a relative+static chain placed at (%v, %v), want the window coordinates (300, 200)", bounds.Origin.X, bounds.Origin.Y)
	}
}

// fixedScrollerView: a 200x200 scroller over 600px of content with a
// focusable first row and a fixed box inside the content (at window
// coordinates that reach past the scroller's right edge, so the clip
// escape is observable too). The scroll handle and the focus handle
// live on the view (the harness's discipline: the view re-runs every
// frame and the handles must not).
type fixedScrollerView struct {
	target FocusHandle
	scroll ScrollHandle
}

func (v *fixedScrollerView) Render(w *Window, cx *Context[fixedScrollerView]) AnyElement {
	return Div().ID("root").SizeFull().Flex().Bg(Rgb(0xffffff)).
		Child(Div().ID("scroller").DebugSelector("scroller").W(Px(200)).H(Px(200)).OverflowYScroll().
			TrackScroll(v.scroll).
			Child(Div().ID("content").DebugSelector("content").W(Px(200)).H(Px(600)).FlexCol().
				Child(Div().ID("row1").DebugSelector("row1").W(Px(200)).H(Px(40)).Bg(Rgb(0xfca5a5)).TrackFocus(v.target)).
				Child(Div().ID("row2").DebugSelector("row2").W(Px(200)).H(Px(40)).Bg(Rgb(0xe2e8f0))).
				Child(fixedBox("fixed", 0xdc2626).
					Left(Px(140)).Top(Px(60)).W(Px(90)).H(Px(50))))).
		IntoElement()
}

// TestFixedStaysPutInsideAScrolledScroller: a fixed box inside a
// scroller does not scroll with it (its containing block is outside
// the scroller, so the scroll offset and the scroller's overflow clip
// never reach it) — after two keyboard line steps the box sits at its
// window position while the content has moved.
func TestFixedStaysPutInsideAScrolledScroller(t *testing.T) {
	ta, app, window := newFixedWindow(t)
	var target FocusHandle
	view := NewEntity(app, window.Scope(), func(v *fixedScrollerView, cx *Context[fixedScrollerView]) {
		if target == (FocusHandle{}) {
			target = NewFocusHandle(window).TabStop(true)
			v.target = target
			v.scroll = NewScrollHandle()
		}
	})
	window.SetRootView(ViewOf(view))
	if _, err := DrawWindowFrame(window); err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}
	// Focus the first row through a click at its centre (the keyboard
	// scroll follows focus).
	window.DispatchInput(&MouseDownEvent{Button: MouseButtonLeft, Position: Point{X: 100, Y: 10}, ClickCount: 1}, app)
	window.DispatchInput(&MouseUpEvent{Button: MouseButtonLeft, Position: Point{X: 100, Y: 10}, ClickCount: 1}, app)
	ta.RunUntilParked()
	for i := 0; i < 2; i++ {
		stroke := MustParseKeystroke("arrowdown")
		window.DispatchKeyEvent(&KeyDownEvent{Keystroke: stroke}, app)
		window.DispatchKeyEvent(&KeyUpEvent{Keystroke: stroke}, app)
	}
	ta.RunUntilParked()
	if _, err := DrawWindowFrame(window); err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}
	bounds, ok := WindowDebugBound(window, "fixed")
	if !ok {
		t.Fatal("the fixed box has no bounds")
	}
	if bounds.Origin.X != 140 || bounds.Origin.Y != 60 {
		t.Fatalf("the fixed box inside the scrolled scroller placed at (%v, %v), want its window position (140, 60)", bounds.Origin.X, bounds.Origin.Y)
	}
	row, ok := WindowDebugBound(window, "row1")
	if !ok {
		t.Fatal("the first row has no bounds")
	}
	if row.Origin.Y > -79 {
		t.Fatalf("the scroller did not scroll (row1 at y=%v, want -80)", row.Origin.Y)
	}
}

// TestFixedDefersItsDrawAboveTheInFlowScene: the fixed box's draw is
// deferred (the deferred draws paint after the whole main scene,
// deferred.go paintDeferredDraws — css2.1 Appendix E step 8: a fixed
// box paints as a positioned element of the root stacking context,
// above all in-flow content of the tree), at the fixed priority
// (below every explicitly deferred surface). The pixel-level order is
// the parity fixture's Tier C.
func TestFixedDefersItsDrawAboveTheInFlowScene(t *testing.T) {
	_, app, window := newFixedWindow(t)
	window.SetRootView(ViewOf(NewEntity(app, window.Scope(), func(v *fixedPlainView, cx *Context[fixedPlainView]) {})))
	if _, err := DrawWindowFrame(window); err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}
	ds, ok := windowDrawStates[window]
	if !ok {
		t.Fatal("the window has no draw state")
	}
	if len(ds.lastDeferredDraws) != 1 {
		t.Fatalf("the frame deferred %d draws, want exactly the fixed box's one", len(ds.lastDeferredDraws))
	}
	if got := ds.lastDeferredDraws[0].priority; got != fixedDrawPriority {
		t.Fatalf("the fixed draw deferred at priority %d, want %d", got, fixedDrawPriority)
	}
}

// TestFixedClassAppliesTheTailwindFixedClass: the class layer's
// `fixed` class writes PositionFixed (the method-class switch; the
// class left class.go's unsupported list).
func TestFixedClassAppliesTheTailwindFixedClass(t *testing.T) {
	st := &StyleRefinement{}
	applyClasses(nil, st, "fixed")
	if !st.Position.present || st.Position.value != PositionFixed {
		t.Fatalf("the fixed class did not set PositionFixed: %+v", st.Position)
	}
}

// TestFixedIsAContainingBlockForAbsoluteChildren: a fixed element is
// positioned, so it IS a containing block for its absolute descendants
// (ComputeIsAbsoluteContainer: is_fixed_container || position !=
// static, layout_object.cc:2069-2081) — an absolute child of the fixed
// box resolves against the fixed box, not the window.
func TestFixedIsAContainingBlockForAbsoluteChildren(t *testing.T) {
	_, app, window := newFixedWindow(t)
	window.SetRootView(ViewOf(NewEntity(app, window.Scope(), func(v *fixedAbsChildView, cx *Context[fixedAbsChildView]) {})))
	bounds := drawBoundsOf(t, window, "abs")
	if bounds.Origin.X != 340 || bounds.Origin.Y != 220 {
		t.Fatalf("the absolute child of the fixed box placed at (%v, %v), want the fixed box's padding-box coordinates (340, 220)", bounds.Origin.X, bounds.Origin.Y)
	}
}

// fixedAbsChildView: a fixed box at (300, 200) with an absolute child
// inset (40, 20) that must resolve against the fixed box.
type fixedAbsChildView struct{}

func (v *fixedAbsChildView) Render(w *Window, cx *Context[fixedAbsChildView]) AnyElement {
	return Div().ID("root").SizeFull().Flex().Bg(Rgb(0xffffff)).
		Child(fixedBox("fixed", 0xdc2626).
			Left(Px(300)).Top(Px(200)).W(Px(60)).H(Px(40)).
			Child(Div().ID("abs").DebugSelector("abs").Absolute().
				Left(Px(40)).Top(Px(20)).W(Px(20)).H(Px(10)).Bg(Rgb(0x2563eb)))).
		IntoElement()
}
