package gpui

// Base ticket 08's anchored-element tests (anchored.go): the ports of
// R:elements/anchored.rs:291-398 plus the switch-anchor flip, the
// margin snap and the local position mode.

import "testing"

// anchoredTestView is the pinned AnchoredTestView (R:anchored.rs:298-327):
// a scroll container holding a tall child and a deferred, snapping
// anchored menu.
func anchoredTestView(position Point) func(*paintView, *Window, *Context[paintView]) AnyElement {
	return func(*paintView, *Window, *Context[paintView]) AnyElement {
		return Div().SizeFull().Child(
			Div().ID("scroll-container").OverflowYScroll().SizeFull().
				Child(Div().DebugSelector("CONTENT").H(Px(2000)).WFull()).
				Child(Deferred(
					Anchored().SnapToWindow().Position(position).Child(
						Div().ID("menu").DebugSelector("MENU").W(Px(200)).H(Px(300)),
					),
				).Priority(1)),
		).IntoElement()
	}
}

func menuBounds(t *testing.T, f *paintFixture, selector string) Bounds {
	t.Helper()
	bounds, ok := WindowDebugBound(f.window, selector)
	if !ok {
		t.Fatalf("%s debug bounds not found", selector)
	}
	return bounds
}

// TestAnchoredPositionWithoutScroll ports R:anchored.rs:329-346.
func TestAnchoredPositionWithoutScroll(t *testing.T) {
	f := newPaintFixture(t, anchoredTestView(Point{X: 100, Y: 100}))
	f.draw()
	f.ta.RunUntilParked()
	if got := menuBounds(t, f, "MENU"); got != logical(100, 100, 200, 300) {
		t.Fatalf("MENU = %v, want (100,100 200x300)", got)
	}
}

// TestAnchoredPositionWhenScrolled ports R:anchored.rs:348-378: a window
// position is independent of the scroll offset of the container the
// anchored element is laid out in.
func TestAnchoredPositionWhenScrolled(t *testing.T) {
	f := newPaintFixture(t, anchoredTestView(Point{X: 100, Y: 100}))
	f.draw()
	f.ta.RunUntilParked()
	f.window.DispatchInput(&ScrollWheelEvent{
		Position:   Point{X: 400, Y: 300},
		Delta:      ScrollDeltaPixels(0, -1000),
		TouchPhase: TouchPhaseMoved,
	}, f.app)
	f.ta.RunUntilParked()
	f.draw()
	if content := menuBounds(t, f, "CONTENT"); content.Origin.Y != -1000 {
		t.Fatalf("the container did not scroll: CONTENT at %v", content)
	}
	if got := menuBounds(t, f, "MENU"); got != logical(100, 100, 200, 300) {
		t.Fatalf("MENU after scrolling = %v, want (100,100 200x300)", got)
	}
}

// TestAnchoredSnapsToWindow ports R:anchored.rs:380-397: a menu
// overflowing the bottom snaps up to the window edge.
func TestAnchoredSnapsToWindow(t *testing.T) {
	f := newPaintFixture(t, anchoredTestView(Point{X: 100, Y: 500}))
	f.draw()
	if got := menuBounds(t, f, "MENU"); got != logical(100, 300, 200, 300) {
		t.Fatalf("MENU = %v, want (100,300 200x300)", got)
	}
}

// anchoredAt renders one anchored 200x300 menu built by configure.
func anchoredAt(configure func(*AnchoredElement) *AnchoredElement) func(*paintView, *Window, *Context[paintView]) AnyElement {
	return func(*paintView, *Window, *Context[paintView]) AnyElement {
		return Div().SizeFull().Child(Deferred(
			configure(Anchored()).Child(Div().DebugSelector("MENU").W(Px(200)).H(Px(300)).Bg(paintBlue)),
		)).IntoElement()
	}
}

// TestAnchoredSwitchAnchorFlips pins the default switch-anchor fit
// (R:anchored.rs:155-180): a top-left anchored menu overflowing right and
// bottom flips to the opposite corner along each axis whose flip fits.
func TestAnchoredSwitchAnchorFlips(t *testing.T) {
	f := newPaintFixture(t, anchoredAt(func(a *AnchoredElement) *AnchoredElement {
		return a.Position(Point{X: 700, Y: 500})
	}))
	scene := f.draw()
	if got := menuBounds(t, f, "MENU"); got != logical(500, 200, 200, 300) {
		t.Fatalf("MENU = %v, want (500,200 200x300): flipped to the bottom-right corner", got)
	}
	// The menu's quad paints where its layout went (the port re-applies
	// the prepainted offset at paint).
	if quad := oneQuadOf(t, scene, paintBlue, "menu"); quad.Bounds != dev(1000, 400, 400, 600) {
		t.Fatalf("menu quad %v, want (1000,400 400x600)", quad.Bounds)
	}
}

// TestAnchoredSwitchAnchorFallsBackToSnap pins the snap after a flip that
// would not fit either (R:anchored.rs:158-168 then 189-196): a 600px menu
// at x=500 overflows the right edge anchored top-left and the left edge
// anchored top-right, so it keeps its anchor and snaps left.
func TestAnchoredSwitchAnchorFallsBackToSnap(t *testing.T) {
	f := newPaintFixture(t, func(*paintView, *Window, *Context[paintView]) AnyElement {
		return Div().SizeFull().Child(Deferred(
			Anchored().Position(Point{X: 500, Y: 10}).Child(Div().DebugSelector("MENU").W(Px(600)).H(Px(300))),
		)).IntoElement()
	})
	f.draw()
	if got := menuBounds(t, f, "MENU"); got != logical(200, 10, 600, 300) {
		t.Fatalf("MENU = %v, want (200,10 600x300)", got)
	}
}

// TestAnchoredSnapToWindowWithMarginAndClientInset pins the margin snap
// (R:anchored.rs:73-77, 182-205): the snapped edge keeps the margin plus
// the window's client inset.
func TestAnchoredSnapToWindowWithMarginAndClientInset(t *testing.T) {
	f := newPaintFixture(t, anchoredAt(func(a *AnchoredElement) *AnchoredElement {
		return a.SnapToWindowWithMargin(Edges{Top: 4, Right: 8, Bottom: 12, Left: 16}).Position(Point{X: 700, Y: 400})
	}))
	f.window.SetClientInset(2)
	f.draw()
	// Right overflow: 900-800 = 100, plus margin 8 + inset 2 → x = 590.
	// Bottom overflow: 700-600 = 100, plus 12 + 2 → y = 286.
	if got := menuBounds(t, f, "MENU"); got != logical(590, 286, 200, 300) {
		t.Fatalf("MENU = %v, want (590,286 200x300)", got)
	}
}

// TestAnchoredLocalPositionAndOffset pins the local position mode and the
// offset (R:anchored.rs:54-65, 261-288): the position is relative to the
// anchored element's laid-out origin (an absolute flex item with auto
// insets sits at its container's content origin), then offset.
func TestAnchoredLocalPositionAndOffset(t *testing.T) {
	f := newPaintFixture(t, func(*paintView, *Window, *Context[paintView]) AnyElement {
		return Div().SizeFull().Child(Div().W(Px(40)).H(Px(10))).Child(
			Anchored().PositionMode(AnchoredPositionLocal).Position(Point{X: 5, Y: 6}).Offset(Point{X: 1, Y: 2}).
				Child(Div().DebugSelector("MENU").W(Px(20)).H(Px(30))),
		).IntoElement()
	})
	f.draw()
	if got := menuBounds(t, f, "MENU"); got != logical(6, 8, 20, 30) {
		t.Fatalf("MENU = %v, want (6,8 20x30)", got)
	}
}
