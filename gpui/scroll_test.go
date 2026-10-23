package gpui

// Base ticket 07's deterministic wheel transcripts and the ports of the
// pinned scroll tests (R: = reference/ce-source/crates/gpui/src, pin
// 254b5dbd). Every test drives a test window (scale 2, rem 16) with
// synthetic dispatched ScrollWheelEvents; nothing touches the real mouse.
// The window line height is round(16 * golden ratio) = 26px, so one wheel
// line is 26px for the div and the uniform list (R:div.rs:3828,
// R:window.rs:2983) and the pinned 20px for the list (R:list.rs:1629).
//
// Each transcript step records the offset the observer saw INSIDE the
// dispatch (an ancestor's bubble listener runs after the scroller's —
// R:window.rs:6094-6113 reverse registration order) and the offset after
// the dispatch's flush redrew the notified view (R:app.rs:1800-1812): the
// div and the uniform list apply deltas unclamped and clamp at prepaint
// (R:div.rs:3871-3872, 2913-2976); the list clamps eagerly in the
// listener (R:list.rs:1626-1639, 898-960).

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// scrollView renders whatever its build function returns each frame.
type scrollView struct {
	build func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement
	// width is a mutable layout input (the pinned width-change draws).
	width float32
	// count is a mutable item count.
	count int
}

func (v *scrollView) Render(w *Window, cx *Context[scrollView]) AnyElement {
	return v.build(v, w, cx)
}

// scrollFixture is one test app, window and root view.
type scrollFixture struct {
	t      *testing.T
	ta     *TestApp
	app    *App
	window *Window
	view   Entity[scrollView]
}

func newScrollFixture(t *testing.T, size Size, build func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement) *scrollFixture {
	t.Helper()
	ta := NewTestApp()
	app := ta.App()
	window := NewTestWindow(app, size)
	view := NewEntity(app, window.Scope(), func(v *scrollView, cx *Context[scrollView]) { v.build = build })
	window.SetRootView(ViewOf(view))
	f := &scrollFixture{t: t, ta: ta, app: app, window: window, view: view}
	f.draw()
	return f
}

func (f *scrollFixture) draw() {
	f.t.Helper()
	if _, err := DrawWindowFrame(f.window); err != nil {
		f.t.Fatalf("DrawWindowFrame: %v", err)
	}
}

func (f *scrollFixture) wheel(x, y float32, delta ScrollDelta, phase TouchPhase) {
	f.window.DispatchInput(&ScrollWheelEvent{Position: Point{X: x, Y: y}, Delta: delta, TouchPhase: phase}, f.app)
}

func (f *scrollFixture) update(fn func(v *scrollView)) {
	f.view.Update(f.app, func(v *scrollView, cx *Context[scrollView]) {
		fn(v)
		cx.Notify()
	})
}

// scrollSteps records ordered observations.
type scrollSteps struct{ entries []string }

func (r *scrollSteps) add(format string, args ...any) {
	r.entries = append(r.entries, fmt.Sprintf(format, args...))
}

func (r *scrollSteps) take() []string {
	out := r.entries
	r.entries = nil
	return out
}

func wantScrollSteps(t *testing.T, step string, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s transcript:\n got  %s\n want %s", step, strings.Join(got, " | "), strings.Join(want, " | "))
	}
}

func pt(p Point) string { return fmt.Sprintf("(%g,%g)", p.X, p.Y) }

// observeWheel wraps child in a full-size identified div whose bubble
// wheel listener records observe() inside the dispatch.
func observeWheel(rec *scrollSteps, observe func() string, child any) AnyElement {
	return Div().ID("observer").SizeFull().
		OnScrollWheel(func(_ *ScrollWheelEvent, phase DispatchPhase, _ *Hitbox, _ *Window, _ *App) {
			if phase == DispatchBubble {
				rec.add("dispatch=%s", observe())
			}
		}).
		Child(child).
		IntoElement()
}

var scrollContentColor = Hsla{H: 0.6, S: 1, L: 0.5, A: 1}

// contentQuadY returns the device-pixel top of the last frame's quad
// with the content color.
func contentQuadY(t *testing.T, w *Window) float32 {
	t.Helper()
	quads, err := LastDrawnScene(w).Quads()
	if err != nil {
		t.Fatal(err)
	}
	want := SolidBackground(scrollContentColor).Opacity(1)
	for _, q := range quads {
		if q.Background == want {
			return q.Bounds.Origin.Y
		}
	}
	t.Fatal("no content quad painted")
	return 0
}

// ---------------------------------------------------------------------------
// OngoingScroll (R:gestures.rs:1124-1234, ported 6/6)
// ---------------------------------------------------------------------------

func TestScrollOngoingScrollLocksToDominantAxis(t *testing.T) {
	var s OngoingScroll
	d := Point{X: 10, Y: 2}
	s.filterAt(&d, TouchPhaseStarted, 0)
	if !s.hasAxis || s.axis != AxisHorizontal || d != (Point{X: 10, Y: 0}) {
		t.Fatalf("started: axis=%v/%v delta=%v", s.hasAxis, s.axis, d)
	}
	d = Point{X: 3, Y: 2}
	s.filterAt(&d, TouchPhaseMoved, 1_000_000)
	if !s.hasAxis || s.axis != AxisHorizontal || d != (Point{X: 3, Y: 0}) {
		t.Fatalf("continued: axis=%v/%v delta=%v", s.hasAxis, s.axis, d)
	}
}

func TestScrollOngoingScrollUnlocksWhenDirectionChanges(t *testing.T) {
	var s OngoingScroll
	d := Point{X: 10, Y: 2}
	s.filterAt(&d, TouchPhaseStarted, 0)
	d = Point{X: 2, Y: 10}
	s.filterAt(&d, TouchPhaseMoved, 1_000_000)
	if s.hasAxis || d != (Point{X: 2, Y: 10}) {
		t.Fatalf("axis=%v/%v delta=%v, want unlocked and unfiltered", s.hasAxis, s.axis, d)
	}
}

func TestScrollOngoingScrollStartsNewGestureAtTimeoutBoundary(t *testing.T) {
	var s OngoingScroll
	d := Point{X: 10, Y: 2}
	s.filterAt(&d, TouchPhaseMoved, 0)
	d = Point{X: 2, Y: 10}
	s.filterAt(&d, TouchPhaseMoved, scrollEventSeparation)
	if !s.hasAxis || s.axis != AxisVertical || d != (Point{X: 0, Y: 10}) {
		t.Fatalf("axis=%v/%v delta=%v", s.hasAxis, s.axis, d)
	}
}

func TestScrollOngoingScrollIgnoresZeroDeltaAndResetsWhenEnded(t *testing.T) {
	var s OngoingScroll
	d := Point{X: 10, Y: 2}
	s.filterAt(&d, TouchPhaseStarted, 0)
	zero := Point{}
	s.filterAt(&zero, TouchPhaseEnded, 1_000_000)
	if s.hasAxis {
		t.Fatalf("ended: axis still locked")
	}
	d = Point{X: 2, Y: 3}
	s.filterAt(&d, TouchPhaseMoved, 2_000_000)
	if !s.hasAxis || s.axis != AxisVertical || d != (Point{X: 0, Y: 3}) {
		t.Fatalf("axis=%v/%v delta=%v", s.hasAxis, s.axis, d)
	}
}

func TestScrollOngoingScrollIgnoresZeroDeltaMovement(t *testing.T) {
	var s OngoingScroll
	d := Point{X: 10, Y: 2}
	s.filterAt(&d, TouchPhaseStarted, 0)
	zero := Point{}
	s.filterAt(&zero, TouchPhaseMoved, scrollEventSeparation)
	d = Point{X: 2, Y: 10}
	s.filterAt(&d, TouchPhaseMoved, scrollEventSeparation)
	if !s.hasAxis || s.axis != AxisVertical || d != (Point{X: 0, Y: 10}) {
		t.Fatalf("axis=%v/%v delta=%v", s.hasAxis, s.axis, d)
	}
}

func TestScrollOngoingScrollSupportsMovedOnlyPlatforms(t *testing.T) {
	var s OngoingScroll
	d := Point{X: 10, Y: 2}
	s.filterAt(&d, TouchPhaseMoved, 0)
	if !s.hasAxis || s.axis != AxisHorizontal || d != (Point{X: 10, Y: 0}) {
		t.Fatalf("axis=%v/%v delta=%v", s.hasAxis, s.axis, d)
	}
}

// ---------------------------------------------------------------------------
// ScrollHandle (R:div.rs:5411-5447, ported 2/2)
// ---------------------------------------------------------------------------

func TestScrollHandleAlignsWideChildrenToLeftEdge(t *testing.T) {
	h := NewScrollHandle()
	h.state.bounds = Bounds{Size: Size{Width: 80, Height: 20}}
	h.state.childBounds = []Bounds{{Origin: Point{X: 25}, Size: Size{Width: 200, Height: 20}}}
	h.state.overflowX = OverflowScroll
	h.ScrollToItem(0)
	h.scrollToActiveItem()
	if got := h.Offset().X; got != -25 {
		t.Fatalf("offset.x = %v, want -25", got)
	}
}

func TestScrollHandleAlignsTallChildrenToTopEdge(t *testing.T) {
	h := NewScrollHandle()
	h.state.bounds = Bounds{Size: Size{Width: 20, Height: 80}}
	h.state.childBounds = []Bounds{{Origin: Point{Y: 25}, Size: Size{Width: 20, Height: 200}}}
	h.state.overflowY = OverflowScroll
	h.ScrollToItem(0)
	h.scrollToActiveItem()
	if got := h.Offset().Y; got != -25 {
		t.Fatalf("offset.y = %v, want -25", got)
	}
}

// TestScrollCoalesceScrollDelta pins ScrollDelta::coalesce
// (R:interactive.rs:621-661).
func TestScrollCoalesceScrollDelta(t *testing.T) {
	cases := []struct {
		a, b, want ScrollDelta
	}{
		{ScrollDeltaPixels(0, -30), ScrollDeltaPixels(0, -30), ScrollDeltaPixels(0, -60)},
		{ScrollDeltaPixels(5, -30), ScrollDeltaPixels(-2, 10), ScrollDeltaPixels(-2, 10)},
		{ScrollDeltaLines(0, 1), ScrollDeltaLines(0, 2), ScrollDeltaLines(0, 3)},
		{ScrollDeltaPixels(0, -30), ScrollDeltaLines(0, -1), ScrollDeltaLines(0, -1)},
		{ScrollDelta{}, ScrollDeltaPixels(0, -4), ScrollDeltaPixels(0, -4)},
		// +0 has signum 1: a positive delta accumulates onto zero.
		{ScrollDeltaLines(0, 0), ScrollDeltaLines(3, 1), ScrollDeltaLines(3, 1)},
		// Pages accumulate like the other inexact variant (a page-scroll
		// wheel's notches coalesce), and a variant mix yields the newer delta.
		{ScrollDeltaPages(0, -1), ScrollDeltaPages(0, -1), ScrollDeltaPages(0, -2)},
		{ScrollDeltaPages(0, -1), ScrollDeltaPages(0, 1), ScrollDeltaPages(0, 1)},
		{ScrollDeltaLines(0, -1), ScrollDeltaPages(0, -1), ScrollDeltaPages(0, -1)},
		{ScrollDeltaPages(0, -1), ScrollDeltaLines(0, -1), ScrollDeltaLines(0, -1)},
	}
	for i, c := range cases {
		if got := coalesceScrollDelta(c.a, c.b); got != c.want {
			t.Fatalf("case %d: coalesce(%+v, %+v) = %+v, want %+v", i, c.a, c.b, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// The scrolling div (R:div.rs:1746-1777, 2735-2755, 2913-2976, 3816-3879)
// ---------------------------------------------------------------------------

// divScroller is a 100x100 tracked scroller over a content child.
func divScroller(handle ScrollHandle, contentW, contentH float32, configure func(*DivElement) *DivElement) *DivElement {
	d := Div().ID("scroller").W(Px(100)).H(Px(100)).TrackScroll(handle)
	d = configure(d)
	return d.Child(Div().W(Px(contentW)).H(Px(contentH)).FlexShrink0().Bg(scrollContentColor).DebugSelector("content"))
}

// TestScrollDivWheelTranscript is the div wheel transcript: line and
// pixel deltas apply UNCLAMPED inside the dispatch and the redraw's
// prepaint clamps them; a y-only scroller takes a horizontal delta on y;
// a wheel outside the scroller's hitbox does nothing; the handle reports
// the bounds and max offset, and the content prepaints and paints under
// the offset.
func TestScrollDivWheelTranscript(t *testing.T) {
	handle := NewScrollHandle()
	rec := &scrollSteps{}
	f := newScrollFixture(t, Size{Width: 400, Height: 300}, func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
		return observeWheel(rec, func() string { return pt(handle.Offset()) },
			divScroller(handle, 100, 300, func(d *DivElement) *DivElement { return d.OverflowYScroll() }))
	})
	if handle.MaxOffset() != (Point{Y: 200}) || handle.Bounds() != (Bounds{Size: Size{Width: 100, Height: 100}}) {
		t.Fatalf("first frame metrics: max=%v bounds=%+v", handle.MaxOffset(), handle.Bounds())
	}
	if handle.ChildrenCount() != 1 {
		t.Fatalf("children count = %d, want 1", handle.ChildrenCount())
	}

	steps := []struct {
		name  string
		x, y  float32
		delta ScrollDelta
	}{
		{"lines(0,-1)", 50, 50, ScrollDeltaLines(0, -1)},
		{"pixels(0,-500)", 50, 50, ScrollDeltaPixels(0, -500)},
		{"pixels(0,50)", 50, 50, ScrollDeltaPixels(0, 50)},
		{"pixels(-30,0)", 50, 50, ScrollDeltaPixels(-30, 0)},
		{"lines(0,100)", 50, 50, ScrollDeltaLines(0, 100)},
		{"outside pixels(0,-40)", 300, 50, ScrollDeltaPixels(0, -40)},
	}
	for _, s := range steps {
		f.wheel(s.x, s.y, s.delta, TouchPhaseMoved)
		bounds, _ := WindowDebugBound(f.window, "content")
		rec.add("%s frame=%s content.y=%g", s.name, pt(handle.Offset()), bounds.Origin.Y)
	}
	wantScrollSteps(t, "div wheel", rec.take(), []string{
		"dispatch=(0,-26)", "lines(0,-1) frame=(0,-26) content.y=-26",
		"dispatch=(0,-526)", "pixels(0,-500) frame=(0,-200) content.y=-200",
		"dispatch=(0,-150)", "pixels(0,50) frame=(0,-150) content.y=-150",
		"dispatch=(0,-180)", "pixels(-30,0) frame=(0,-180) content.y=-180",
		"dispatch=(0,2420)", "lines(0,100) frame=(0,0) content.y=0",
		"dispatch=(0,0)", "outside pixels(0,-40) frame=(0,0) content.y=0",
	})

	// The content paints under the offset (device pixels, scale 2) and
	// the handle's child bounds stay the unscrolled layout bounds.
	f.wheel(50, 50, ScrollDeltaPixels(0, -40), TouchPhaseMoved)
	rec.take()
	if y := contentQuadY(t, f.window); y != -80 {
		t.Fatalf("content quad y = %v device px, want -80", y)
	}
	if child, ok := handle.BoundsForItem(0); !ok || child != (Bounds{Size: Size{Width: 100, Height: 300}}) {
		t.Fatalf("child bounds = %+v ok=%v, want the unscrolled 100x300 at the origin", child, ok)
	}
	if ix, off := handle.LogicalScrollTop(); ix != 0 || off != -40 {
		t.Fatalf("logical scroll top = (%d, %v), want (0, -40)", ix, off)
	}
}

// TestScrollDivWheelPageScroll is the page-granularity wheel transcript
// (browser-core ticket 02): the WHEEL_PAGESCROLL sentinel's one page per
// notch, as a Pages delta. Chromium resolves a page delta against the
// scroller's own visible length with the page step
// max(0.875×length, length−40) (blink ScrollableArea::PageStep →
// cc/input/scroll_utils.cc:51-56 CalculatePageStep): a 100px-tall
// scroller steps 87.5px per notch, applies UNCLAMPED like the other
// inexact deltas, clamps at the next prepaint, and a y-only scroller
// takes a horizontal page delta on y.
func TestScrollDivWheelPageScroll(t *testing.T) {
	handle := NewScrollHandle()
	rec := &scrollSteps{}
	f := newScrollFixture(t, Size{Width: 400, Height: 300}, func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
		return observeWheel(rec, func() string { return pt(handle.Offset()) },
			divScroller(handle, 100, 300, func(d *DivElement) *DivElement { return d.OverflowYScroll() }))
	})
	if handle.MaxOffset() != (Point{Y: 200}) {
		t.Fatalf("max offset = %v, want (0,200)", handle.MaxOffset())
	}
	steps := []struct {
		name  string
		x, y  float32
		delta ScrollDelta
	}{
		{"pages(0,-1)", 50, 50, ScrollDeltaPages(0, -1)},
		{"pages(0,-1)", 50, 50, ScrollDeltaPages(0, -1)},
		{"pages(0,-2)", 50, 50, ScrollDeltaPages(0, -2)},
		{"pages(-1,0)", 50, 50, ScrollDeltaPages(-1, 0)},
	}
	for _, s := range steps {
		f.wheel(s.x, s.y, s.delta, TouchPhaseMoved)
		bounds, _ := WindowDebugBound(f.window, "content")
		rec.add("%s frame=%s content.y=%g", s.name, pt(handle.Offset()), bounds.Origin.Y)
	}
	wantScrollSteps(t, "div page wheel", rec.take(), []string{
		"dispatch=(0,-87.5)", "pages(0,-1) frame=(0,-87.5) content.y=-87.5",
		"dispatch=(0,-175)", "pages(0,-1) frame=(0,-175) content.y=-175",
		"dispatch=(0,-350)", "pages(0,-2) frame=(0,-200) content.y=-200",
		"dispatch=(0,-287.5)", "pages(-1,0) frame=(0,-200) content.y=-200",
	})
}

// TestScrollDivRestrictScrollToAxis checks restrict_scroll_to_axis on a
// y-only scroller: a horizontal delta is no longer redirected to y, so
// the offset does not move and the view is not notified (no redraw).
func TestScrollDivRestrictScrollToAxis(t *testing.T) {
	handle := NewScrollHandle()
	f := newScrollFixture(t, Size{Width: 400, Height: 300}, func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
		return divScroller(handle, 100, 300, func(d *DivElement) *DivElement { return d.OverflowYScroll().RestrictScrollToAxis() }).IntoElement()
	})
	draws := WindowDrawCount(f.window)
	f.wheel(50, 50, ScrollDeltaPixels(-30, 0), TouchPhaseMoved)
	f.wheel(50, 50, ScrollDeltaLines(-1, 0), TouchPhaseMoved)
	if handle.Offset() != (Point{}) || WindowDrawCount(f.window) != draws {
		t.Fatalf("restricted horizontal deltas moved the offset to %v (draws %d -> %d)", handle.Offset(), draws, WindowDrawCount(f.window))
	}
	f.wheel(50, 50, ScrollDeltaPixels(0, -30), TouchPhaseMoved)
	if handle.Offset() != (Point{Y: -30}) || WindowDrawCount(f.window) != draws+1 {
		t.Fatalf("vertical delta: offset %v draws %d, want (0,-30) and one redraw", handle.Offset(), WindowDrawCount(f.window)-draws)
	}
}

// TestScrollDivTouchPhaseAxisLockTranscript drives a both-axes scroller
// under restrict_scroll_to_axis with touchpad phases: the gesture locks
// to its dominant axis, unlocks on a decisive direction change, resets
// at Ended, keeps the lock for small cross movement and starts a new
// gesture after the 28ms separation (R:gestures.rs:63-107 through
// R:div.rs:3837-3844).
func TestScrollDivTouchPhaseAxisLockTranscript(t *testing.T) {
	handle := NewScrollHandle()
	rec := &scrollSteps{}
	f := newScrollFixture(t, Size{Width: 400, Height: 300}, func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
		return divScroller(handle, 300, 300, func(d *DivElement) *DivElement { return d.OverflowScroll().RestrictScrollToAxis() }).IntoElement()
	})
	steps := []struct {
		name    string
		delta   ScrollDelta
		phase   TouchPhase
		advance int64
	}{
		{"started(-10,-2)", ScrollDeltaPixels(-10, -2), TouchPhaseStarted, 0},
		{"moved(-3,-2)", ScrollDeltaPixels(-3, -2), TouchPhaseMoved, 0},
		{"moved(-2,-10)", ScrollDeltaPixels(-2, -10), TouchPhaseMoved, 0},
		{"ended(0,0)", ScrollDeltaPixels(0, 0), TouchPhaseEnded, 0},
		{"moved(-2,-3)", ScrollDeltaPixels(-2, -3), TouchPhaseMoved, 0},
		{"moved(-4,-3)", ScrollDeltaPixels(-4, -3), TouchPhaseMoved, 0},
		{"after 28ms moved(-4,-3)", ScrollDeltaPixels(-4, -3), TouchPhaseMoved, 28},
		{"lines(-1,0) unfiltered", ScrollDeltaLines(-1, 0), TouchPhaseMoved, 0},
	}
	for _, s := range steps {
		if s.advance > 0 {
			f.ta.AdvanceClock(s.advance)
		}
		f.wheel(50, 50, s.delta, s.phase)
		rec.add("%s=%s", s.name, pt(handle.Offset()))
	}
	wantScrollSteps(t, "axis lock", rec.take(), []string{
		"started(-10,-2)=(-10,0)",
		"moved(-3,-2)=(-13,0)",
		"moved(-2,-10)=(-13,-10)",
		"ended(0,0)=(-13,-10)",
		"moved(-2,-3)=(-13,-13)",
		"moved(-4,-3)=(-13,-16)",
		"after 28ms moved(-4,-3)=(-17,-16)",
		"lines(-1,0) unfiltered=(-43,-16)",
	})
}

// TestScrollDivConcurrentScrollArbitration checks the both-axes
// arbitration: without allow_concurrent_scroll only the larger axis
// moves; with it both do; a scrolling axis with no delta of its own takes
// nothing when the other axis scrolls too.
func TestScrollDivConcurrentScrollArbitration(t *testing.T) {
	for _, concurrent := range []bool{false, true} {
		handle := NewScrollHandle()
		f := newScrollFixture(t, Size{Width: 400, Height: 300}, func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
			return divScroller(handle, 300, 300, func(d *DivElement) *DivElement {
				d = d.OverflowScroll()
				if concurrent {
					d.StyleMut().AllowConcurrentScroll = Some(true)
				}
				return d
			}).IntoElement()
		})
		rec := &scrollSteps{}
		for _, delta := range []ScrollDelta{ScrollDeltaPixels(-10, -20), ScrollDeltaPixels(-20, -10), ScrollDeltaLines(-1, 0)} {
			f.wheel(50, 50, delta, TouchPhaseMoved)
			rec.add("%s", pt(handle.Offset()))
		}
		want := []string{"(0,-20)", "(-20,-20)", "(-46,-20)"}
		if concurrent {
			want = []string{"(-10,-20)", "(-30,-30)", "(-56,-30)"}
		}
		wantScrollSteps(t, fmt.Sprintf("concurrent=%v", concurrent), rec.take(), want)
	}
}

// TestScrollDivElementStateOffset checks the untracked scroller: an
// identified overflow-y-scroll div keeps its offset in its element state
// across frames, and its children prepaint under it.
func TestScrollDivElementStateOffset(t *testing.T) {
	f := newScrollFixture(t, Size{Width: 400, Height: 300}, func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
		return Div().ID("scroller").W(Px(100)).H(Px(100)).OverflowYScroll().
			Child(Div().W(Px(100)).H(Px(300)).FlexShrink0().DebugSelector("content")).
			IntoElement()
	})
	f.wheel(50, 50, ScrollDeltaPixels(0, -40), TouchPhaseMoved)
	if b, _ := WindowDebugBound(f.window, "content"); b.Origin.Y != -40 {
		t.Fatalf("content y after the wheel = %v, want -40", b.Origin.Y)
	}
	f.update(func(*scrollView) {})
	f.draw()
	if b, _ := WindowDebugBound(f.window, "content"); b.Origin.Y != -40 {
		t.Fatalf("content y after a redraw = %v, want -40 (retained element state)", b.Origin.Y)
	}
	f.wheel(50, 50, ScrollDeltaPixels(0, -1000), TouchPhaseMoved)
	if b, _ := WindowDebugBound(f.window, "content"); b.Origin.Y != -200 {
		t.Fatalf("content y after the far wheel = %v, want -200 (clamped)", b.Origin.Y)
	}
}

// TestScrollBuildersRequireElementID pins the StatefulInteractiveElement
// restriction (R:div.rs:1746-1777 live on Stateful<Div>): an id-less
// scroller panics at prepaint, like ticket 06's stateful listeners.
func TestScrollBuildersRequireElementID(t *testing.T) {
	builders := map[string]func(*DivElement) *DivElement{
		"OverflowScroll":       (*DivElement).OverflowScroll,
		"OverflowXScroll":      (*DivElement).OverflowXScroll,
		"OverflowYScroll":      (*DivElement).OverflowYScroll,
		"RestrictScrollToAxis": (*DivElement).RestrictScrollToAxis,
		"TrackScroll":          func(d *DivElement) *DivElement { return d.TrackScroll(NewScrollHandle()) },
	}
	for name, build := range builders {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), "require an element ID") {
					t.Fatalf("recover = %v, want the element ID panic", r)
				}
			}()
			newScrollFixture(t, Size{Width: 100, Height: 100}, func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
				return build(Div().Size(Px(50))).IntoElement()
			})
		})
	}
}

// TestScrollHandleScrollToItemAndBottom checks the handle's deferred
// requests through real frames: scroll_to_item reveals with the minimal
// movement, scroll_to_top_of_item aligns the top, scroll_to_bottom pins
// the max, and top_item / bottom_item / logical_scroll_bottom read the
// pinned binary search (ten 30px children in a 100px viewport).
func TestScrollHandleScrollToItemAndBottom(t *testing.T) {
	handle := NewScrollHandle()
	f := newScrollFixture(t, Size{Width: 400, Height: 300}, func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
		d := Div().ID("scroller").W(Px(100)).H(Px(100)).FlexCol().OverflowYScroll().TrackScroll(handle)
		for i := 0; i < 10; i++ {
			d = d.Child(Div().WFull().H(Px(30)).FlexShrink0())
		}
		return d.IntoElement()
	})
	redraw := func() {
		f.update(func(*scrollView) {})
	}
	if handle.ChildrenCount() != 10 || handle.MaxOffset() != (Point{Y: 200}) {
		t.Fatalf("children=%d max=%v", handle.ChildrenCount(), handle.MaxOffset())
	}
	handle.ScrollToItem(5)
	redraw()
	if handle.Offset() != (Point{Y: -80}) {
		t.Fatalf("scroll_to_item(5) offset = %v, want (0,-80): child 5's bottom (180) at the viewport bottom", handle.Offset())
	}
	if top, bottom := handle.TopItem(), handle.BottomItem(); top != 2 || bottom != 6 {
		t.Fatalf("top/bottom item = %d/%d, want 2/6 (the bottom edge 180 ties children 5 and 6; the pinned search returns the later)", top, bottom)
	}
	handle.ScrollToItem(4) // already fully visible: no movement
	redraw()
	if handle.Offset() != (Point{Y: -80}) {
		t.Fatalf("scroll_to_item(4) moved the offset to %v", handle.Offset())
	}
	handle.ScrollToTopOfItem(1)
	redraw()
	if handle.Offset() != (Point{Y: -30}) {
		t.Fatalf("scroll_to_top_of_item(1) offset = %v, want (0,-30)", handle.Offset())
	}
	handle.ScrollToBottom()
	redraw()
	if handle.Offset() != (Point{Y: -200}) {
		t.Fatalf("scroll_to_bottom offset = %v, want (0,-200)", handle.Offset())
	}
	if ix, off := handle.LogicalScrollBottom(); ix != 9 || off != -200+300-100 {
		t.Fatalf("logical scroll bottom = (%d, %v), want (9, 0)", ix, off)
	}
}

// TestScrollChildPrepaintPreservesScrollOffset ports
// R:div.rs:5048-5082 child_prepaint_preserves_order_and_scroll_offset
// (deviated): the children prepaint in order under the element offset
// plus the scroll offset. The custom-order half is n/a
// (with_dynamic_prepaint_order is not ported), and the port's draw has no
// root origin offset (the root lays out at the window origin), so the
// expected offset is the scroll offset itself.
func TestScrollChildPrepaintPreservesScrollOffset(t *testing.T) {
	handle := NewScrollHandle()
	handle.SetOffset(Point{Y: -15})
	var visits []string
	newScrollFixture(t, Size{Width: 50, Height: 50}, func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
		d := Div().ID("prepaint-order").SizeFull().Flex().FlexCol().OverflowYScroll().TrackScroll(handle)
		for i := 0; i < 3; i++ {
			d = d.Child(CustomElement[struct{}, struct{}](&offsetProbe{index: i, visits: &visits}))
		}
		return d.IntoElement()
	})
	want := []string{"0@(0,-15)", "1@(0,-15)", "2@(0,-15)"}
	if !reflect.DeepEqual(visits, want) {
		t.Fatalf("visits = %v, want %v", visits, want)
	}
}

// offsetProbe is the pinned canvas child: a 40px-tall full-width leaf
// recording the element offset at prepaint.
type offsetProbe struct {
	index  int
	visits *[]string
}

func (p *offsetProbe) ID() (ElementID, bool)           { return ElementID{}, false }
func (p *offsetProbe) SourceLocation() *SourceLocation { return nil }
func (p *offsetProbe) RequestLayout(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App) (LayoutID, struct{}) {
	style := DefaultStyle()
	style.Size = LengthSize{Width: DefiniteLengthOf(Fraction(1)), Height: PxLength(40)}
	style.FlexShrink = 0
	id, err := requestLayoutOf(w, style)
	if err != nil {
		panic(err)
	}
	return id, struct{}{}
}
func (p *offsetProbe) Prepaint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *struct{}, w *Window, app *App) struct{} {
	*p.visits = append(*p.visits, fmt.Sprintf("%d@%s", p.index, pt(ElementOffset(w))))
	return struct{}{}
}
func (p *offsetProbe) Paint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *struct{}, prepaint *struct{}, w *Window, app *App) {
}

// TestScrollFractionalPaddingDoesNotMakeFittingContainerScrollable ports
// R:div.rs:6037-6077: 4.25px vertical padding snaps to 4px (scale 2,
// round half toward zero), so a 42px child fits the 50px box exactly.
func TestScrollFractionalPaddingDoesNotMakeFittingContainerScrollable(t *testing.T) {
	handle := NewScrollHandle()
	newScrollFixture(t, Size{Width: 400, Height: 300}, func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
		return Div().SizeFull().Child(
			Div().ID("container").H(Px(50)).W(Px(100)).Py(Px(4.25)).OverflowYScroll().TrackScroll(handle).
				Child(Div().WFull().H(Px(42))),
		).IntoElement()
	})
	if got := handle.MaxOffset().Y; got != 0 {
		t.Fatalf("max offset y = %v, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// The uniform list (R:uniform_list.rs through the interactivity)
// ---------------------------------------------------------------------------

func uniformItems(start, end int) []AnyElement {
	items := make([]AnyElement, 0, end-start)
	for ix := start; ix < end; ix++ {
		items = append(items, Div().W(Px(200)).H(Px(20)).DebugSelector(fmt.Sprintf("item-%d", ix)).IntoElement())
	}
	return items
}

// TestScrollUniformListWheelTranscript is the uniform-list wheel
// transcript: the list's base style scrolls on y only, so a horizontal
// delta lands on y (R:div.rs:3855-3863); deltas apply unclamped and the
// list's prepaint clamps them, writing the base handle's max offset and
// bounds (R:div.rs:2967-2970).
func TestScrollUniformListWheelTranscript(t *testing.T) {
	handle := NewUniformListScrollHandle()
	rec := &scrollSteps{}
	f := newScrollFixture(t, Size{Width: 200, Height: 100}, func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
		return observeWheel(rec, func() string { return pt(handle.Offset()) },
			UniformList(handle, 100, func(start, end int, w *Window, app *App) []AnyElement { return uniformItems(start, end) }).SizeFull())
	})
	base := handle.BaseHandle()
	if base.MaxOffset() != (Point{Y: 1900}) || base.Bounds() != (Bounds{Size: Size{Width: 200, Height: 100}}) {
		t.Fatalf("first frame metrics: max=%v bounds=%+v", base.MaxOffset(), base.Bounds())
	}
	if atEnd, ok := handle.IsScrolledToEnd(); !ok || atEnd {
		t.Fatalf("is_scrolled_to_end = %v/%v, want false/scrollable", atEnd, ok)
	}
	steps := []struct {
		name  string
		delta ScrollDelta
	}{
		{"pixels(0,-30)", ScrollDeltaPixels(0, -30)},
		{"lines(0,-2)", ScrollDeltaLines(0, -2)},
		{"pixels(-40,0)", ScrollDeltaPixels(-40, 0)},
		{"pixels(0,-5000)", ScrollDeltaPixels(0, -5000)},
		{"lines(0,1)", ScrollDeltaLines(0, 1)},
	}
	for _, s := range steps {
		f.wheel(100, 50, s.delta, TouchPhaseMoved)
		first := -1
		for ix := 0; ix < 100; ix++ {
			if b, ok := WindowDebugBound(f.window, fmt.Sprintf("item-%d", ix)); ok && b.Bottom() > 0 {
				first = ix
				break
			}
		}
		rec.add("%s frame=%s first=%d", s.name, pt(handle.Offset()), first)
	}
	wantScrollSteps(t, "uniform wheel", rec.take(), []string{
		"dispatch=(0,-30)", "pixels(0,-30) frame=(0,-30) first=1",
		"dispatch=(0,-82)", "lines(0,-2) frame=(0,-82) first=4",
		"dispatch=(0,-122)", "pixels(-40,0) frame=(0,-122) first=6",
		"dispatch=(0,-5122)", "pixels(0,-5000) frame=(0,-1900) first=95",
		"dispatch=(0,-1874)", "lines(0,1) frame=(0,-1874) first=93",
	})
	f.wheel(100, 50, ScrollDeltaPixels(0, -100), TouchPhaseMoved)
	if atEnd, ok := handle.IsScrolledToEnd(); !ok || !atEnd {
		t.Fatalf("is_scrolled_to_end at the end = %v/%v", atEnd, ok)
	}
}

// TestScrollUniformListScrollStrategyNearest ports
// R:uniform_list.rs:728-868 test_scroll_strategy_nearest (deviated: the
// SelectNext / SelectPrev actions are direct handle calls plus a redraw).
func TestScrollUniformListScrollStrategyNearest(t *testing.T) {
	handle := NewUniformListScrollHandle()
	var visible [2]int
	f := newScrollFixture(t, Size{Width: 400, Height: 300}, func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
		return Div().ID("list-example").SizeFull().Child(
			UniformList(handle, 47, func(start, end int, w *Window, app *App) []AnyElement {
				visible = [2]int{start, end}
				return uniformItems(start, end)
			}).H(PxLength(200)),
		).IntoElement()
	})
	index := 0
	step := func(next bool) {
		if next {
			index = (index + 1) % 47
		} else {
			index = (index + 46) % 47
		}
		handle.ScrollToItem(index, ScrollNearest)
		f.update(func(*scrollView) {})
	}
	for ix := 1; ix < 10; ix++ {
		step(true)
		if index != ix || visible != [2]int{0, 10} {
			t.Fatalf("next to %d: visible %v, want [0 10]", index, visible)
		}
	}
	for ix := 10; ix < 47; ix++ {
		step(true)
		if visible != [2]int{ix - 9, ix + 1} {
			t.Fatalf("next to %d: visible %v, want [%d %d]", ix, visible, ix-9, ix+1)
		}
	}
	step(true)
	if index != 0 || visible != [2]int{0, 10} {
		t.Fatalf("wrap to 0: visible %v", visible)
	}
	step(false)
	if index != 46 || visible != [2]int{37, 47} {
		t.Fatalf("back to 46: visible %v", visible)
	}
	for ix := 45; ix >= 37; ix-- {
		step(false)
		if visible != [2]int{37, 47} {
			t.Fatalf("prev to %d: visible %v, want [37 47]", ix, visible)
		}
	}
	for ix := 36; ix >= 0; ix-- {
		step(false)
		if visible != [2]int{ix, ix + 10} {
			t.Fatalf("prev to %d: visible %v, want [%d %d]", ix, visible, ix, ix+10)
		}
	}
}

// ---------------------------------------------------------------------------
// The list (R:list.rs:1551, 1604-1650, 898-960)
// ---------------------------------------------------------------------------

func listRows(height float32) func(ix int, w *Window, app *App) AnyElement {
	return func(ix int, w *Window, app *App) AnyElement {
		return Div().H(Px(height)).WFull().DebugSelector(fmt.Sprintf("row-%d", ix)).IntoElement()
	}
}

// TestScrollListWheelTranscript is the list wheel transcript: the
// listener clamps EAGERLY (the observer sees the clamped logical top
// inside the dispatch), coalesces the deltas of one frame onto the
// frame's scroll top (two events before a redraw add; an opposite sign
// restarts), converts lines at 20px, and fires the scroll handler with
// the frame's visible range. The list measures all items so the scroll
// range covers the 200px content (an unmeasured item contributes nothing,
// R:list.rs:1672-1686).
func TestScrollListWheelTranscript(t *testing.T) {
	state := NewListState(10, 0).MeasureAll()
	rec := &scrollSteps{}
	state.SetScrollHandler(func(e *ListScrollEvent, w *Window, app *App) {
		rec.add("handler range=%d..%d", e.VisibleRangeStart, e.VisibleRangeEnd)
	})
	top := func() string {
		o := state.LogicalScrollTop()
		return fmt.Sprintf("(%d,%g)", o.ItemIx, o.OffsetInItem)
	}
	f := newScrollFixture(t, Size{Width: 100, Height: 100}, func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
		return observeWheel(rec, top, List(state, listRows(20)).SizeFull())
	})
	at := func(e ...ScrollDelta) {
		f.app.Update(func(a *App) {
			for _, d := range e {
				f.window.dispatchInput(&ScrollWheelEvent{Position: Point{X: 50, Y: 50}, Delta: d, TouchPhase: TouchPhaseMoved}, a)
			}
		})
		rec.add("frame=%s", top())
	}
	at(ScrollDeltaPixels(0, -30))
	at(ScrollDeltaPixels(0, -30), ScrollDeltaPixels(0, -30))
	at(ScrollDeltaPixels(0, -20), ScrollDeltaPixels(0, 10))
	at(ScrollDeltaLines(0, -1))
	at(ScrollDeltaPixels(0, -5000))
	at(ScrollDeltaLines(0, 2), ScrollDeltaPixels(0, 15))
	wantScrollSteps(t, "list wheel", rec.take(), []string{
		"handler range=0..5", "dispatch=(1,10)", "frame=(1,10)",
		"handler range=1..7", "dispatch=(3,0)", "handler range=1..7", "dispatch=(4,10)", "frame=(4,10)",
		"handler range=4..10", "dispatch=(5,0)", "handler range=4..10", "dispatch=(4,0)", "frame=(4,0)",
		"handler range=4..9", "dispatch=(5,0)", "frame=(5,0)",
		"handler range=5..10", "dispatch=(5,0)", "frame=(5,0)",
		"handler range=5..10", "dispatch=(3,0)", "handler range=5..10", "dispatch=(4,5)", "frame=(4,5)",
	})
	if max := state.MaxOffsetForScrollbar(); max != (Point{Y: 100}) {
		t.Fatalf("max offset for scrollbar = %v, want (0,100)", max)
	}
	if px := state.ScrollPxOffsetForScrollbar(); px != (Point{Y: -85}) {
		t.Fatalf("scroll px offset = %v, want (0,-85)", px)
	}
}

// TestScrollListWheelPageScroll is the list's page-granularity wheel
// transcript (browser-core ticket 02): a Pages delta resolves against
// the list's own visible height (100px → the 87.5px Chromium page step,
// cc/input/scroll_utils.cc:51-56) and clamps eagerly like the list's
// other wheel deltas.
func TestScrollListWheelPageScroll(t *testing.T) {
	state := NewListState(10, 0).MeasureAll()
	rec := &scrollSteps{}
	top := func() string {
		o := state.LogicalScrollTop()
		return fmt.Sprintf("(%d,%g)", o.ItemIx, o.OffsetInItem)
	}
	f := newScrollFixture(t, Size{Width: 100, Height: 100}, func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
		return observeWheel(rec, top, List(state, listRows(20)).SizeFull())
	})
	at := func(e ...ScrollDelta) {
		f.app.Update(func(a *App) {
			for _, d := range e {
				f.window.dispatchInput(&ScrollWheelEvent{Position: Point{X: 50, Y: 50}, Delta: d, TouchPhase: TouchPhaseMoved}, a)
			}
		})
		rec.add("frame=%s", top())
	}
	at(ScrollDeltaPages(0, -1))
	at(ScrollDeltaPages(0, -1))
	wantScrollSteps(t, "list page wheel", rec.take(), []string{
		"dispatch=(4,7.5)", "frame=(4,7.5)",
		"dispatch=(5,0)", "frame=(5,0)",
	})
}

// TestScrollListResetAfterPaintBeforeScroll ports R:list.rs:1814-1855.
func TestScrollListResetAfterPaintBeforeScroll(t *testing.T) {
	state := NewListState(5, 10)
	state.ScrollTo(ListOffset{})
	f := newScrollFixture(t, Size{Width: 100, Height: 20}, func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
		return List(state, listRows(10)).SizeFull().IntoElement()
	})
	state.Reset(5)
	f.wheel(1, 1, ScrollDeltaPixels(0, -500), TouchPhaseMoved)
	if top := state.LogicalScrollTop(); top != (ListOffset{}) {
		t.Fatalf("logical scroll top = %+v, want {0 0}", top)
	}
}

// TestScrollListScrollByPositiveAndNegativeDistance ports
// R:list.rs:1857-1900 (deviated): ListState.ScrollBy keeps the port's
// clamped body, so the viewport is 60px with a 100px overdraw (all five
// rows measured, max 40) instead of 100px (max 0, where only the pin's
// unclamped scroll_by reaches item 1).
func TestScrollListScrollByPositiveAndNegativeDistance(t *testing.T) {
	state := NewListState(5, 100)
	f := newScrollFixture(t, Size{Width: 100, Height: 60}, func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
		return List(state, listRows(20)).SizeFull().IntoElement()
	})
	state.ScrollBy(30, f.window, f.app)
	if top := state.LogicalScrollTop(); top != (ListOffset{ItemIx: 1, OffsetInItem: 10}) {
		t.Fatalf("after +30 = %+v, want {1 10}", top)
	}
	state.ScrollBy(-30, f.window, f.app)
	if top := state.LogicalScrollTop(); top != (ListOffset{}) {
		t.Fatalf("after -30 = %+v, want {0 0}", top)
	}
	state.ScrollBy(0, f.window, f.app)
	if top := state.LogicalScrollTop(); top != (ListOffset{}) {
		t.Fatalf("after 0 = %+v, want {0 0}", top)
	}
}

// TestScrollListChildScrollHandlerCanStopListScroll ports
// R:list.rs:1902-1954: the list registers its listener before its items,
// so an item's bubble listener runs first and stops the list's.
func TestScrollListChildScrollHandlerCanStopListScroll(t *testing.T) {
	state := NewListState(5, 10)
	childSaw := false
	f := newScrollFixture(t, Size{Width: 100, Height: 100}, func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
		return List(state, func(ix int, w *Window, app *App) AnyElement {
			return Div().H(Px(20)).WFull().
				OnScrollWheel(func(_ *ScrollWheelEvent, phase DispatchPhase, hitbox *Hitbox, w *Window, app *App) {
					if phase == DispatchBubble && hitbox.ShouldHandleScroll(w) {
						childSaw = true
						app.StopPropagation()
					}
				}).IntoElement()
		}).SizeFull().IntoElement()
	})
	f.wheel(50, 10, ScrollDeltaPixels(0, -30), TouchPhaseMoved)
	if !childSaw {
		t.Fatal("the child's scroll-wheel handler did not run")
	}
	if top := state.LogicalScrollTop(); top != (ListOffset{}) {
		t.Fatalf("the list scrolled to %+v despite the stopped propagation", top)
	}
}

// listHeights renders 100px rows except item special at *special px.
func listHeights(special int, height *float32) func(ix int, w *Window, app *App) AnyElement {
	return func(ix int, w *Window, app *App) AnyElement {
		h := float32(100)
		if ix == special {
			h = *height
		}
		return Div().H(Px(h)).WFull().IntoElement()
	}
}

// TestScrollListRemeasureThenScrollDoesNotRevertScrollPosition ports
// R:list.rs:2250-2304: the remeasured item keeps its size hint, so the
// wheel lands at (5, 70) and the rebased pending scroll keeps it.
func TestScrollListRemeasureThenScrollDoesNotRevertScrollPosition(t *testing.T) {
	state := NewListState(20, 10)
	height := float32(100)
	f := newScrollFixture(t, Size{Width: 100, Height: 200}, func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
		return List(state, listHeights(5, &height)).SizeFull().IntoElement()
	})
	state.ScrollTo(ListOffset{ItemIx: 5, OffsetInItem: 40})
	f.update(func(*scrollView) {})
	state.RemeasureItems(5, 6)
	f.wheel(50, 100, ScrollDeltaPixels(0, -30), TouchPhaseMoved)
	if top := state.LogicalScrollTop(); top != (ListOffset{ItemIx: 5, OffsetInItem: 70}) {
		t.Fatalf("after the wheel = %+v, want {5 70}", top)
	}
	f.update(func(*scrollView) {})
	if top := state.LogicalScrollTop(); top != (ListOffset{ItemIx: 5, OffsetInItem: 70}) {
		t.Fatalf("after the redraw = %+v, want {5 70} (not reverted by the stale pending scroll)", top)
	}
}

// TestScrollListScrollAfterRemeasureClampsToShrunkItemHeight ports
// R:list.rs:2306-2366.
func TestScrollListScrollAfterRemeasureClampsToShrunkItemHeight(t *testing.T) {
	state := NewListState(20, 10)
	height := float32(100)
	f := newScrollFixture(t, Size{Width: 100, Height: 200}, func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
		return List(state, listHeights(5, &height)).SizeFull().IntoElement()
	})
	state.ScrollTo(ListOffset{ItemIx: 5, OffsetInItem: 40})
	f.update(func(*scrollView) {})
	height = 50
	state.RemeasureItems(5, 6)
	f.wheel(50, 100, ScrollDeltaPixels(0, -30), TouchPhaseMoved)
	f.update(func(*scrollView) {})
	if top := state.LogicalScrollTop(); top != (ListOffset{ItemIx: 5, OffsetInItem: 50}) {
		t.Fatalf("logical scroll top = %+v, want {5 50}", top)
	}
}

// TestScrollListMeasureAllAfterWidthChange ports R:list.rs:2095-2128 (the
// width change is the list's own width).
func TestScrollListMeasureAllAfterWidthChange(t *testing.T) {
	state := NewListState(10, 0).MeasureAll()
	f := newScrollFixture(t, Size{Width: 400, Height: 200}, func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
		return List(state, listRows(50)).W(PxLength(v.width)).H(PxLength(200)).IntoElement()
	})
	f.update(func(v *scrollView) { v.width = 100 })
	if max := state.MaxOffsetForScrollbar().Y; max != 300 {
		t.Fatalf("max offset at width 100 = %v, want 300", max)
	}
	f.update(func(v *scrollView) { v.width = 200 })
	if max := state.MaxOffsetForScrollbar().Y; max != 300 {
		t.Fatalf("max offset at width 200 = %v, want 300", max)
	}
}

// TestScrollListScrollbarDragWithGrowingContent ports
// R:list.rs:2637-2700: the drag freezes the content height reported to
// the scrollbar while a measure-all list grows.
func TestScrollListScrollbarDragWithGrowingContent(t *testing.T) {
	state := NewListState(10, 0).MeasureAll()
	last := float32(50)
	f := newScrollFixture(t, Size{Width: 100, Height: 200}, func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
		return List(state, func(ix int, w *Window, app *App) AnyElement {
			h := float32(50)
			if ix == 9 {
				h = last
			}
			return Div().H(Px(h)).WFull().IntoElement()
		}).SizeFull().IntoElement()
	})
	state.ScrollbarDragStarted()
	if !state.IsScrollbarDragging() {
		t.Fatal("drag not started")
	}
	state.SetOffsetFromScrollbar(Point{Y: -150})
	before := state.ScrollPxOffsetForScrollbar()
	if top := state.LogicalScrollTop(); top != (ListOffset{ItemIx: 3}) {
		t.Fatalf("after the drag = %+v, want {3 0}", top)
	}
	last = 550
	state.RemeasureItems(9, 10)
	f.update(func(*scrollView) {})
	if max := state.MaxOffsetForScrollbar().Y; max != 300 {
		t.Fatalf("max offset during the drag = %v, want 300 (frozen)", max)
	}
	if got := state.ScrollPxOffsetForScrollbar(); got != before {
		t.Fatalf("scrollbar offset = %v, want %v", got, before)
	}
	state.SetOffsetFromScrollbar(Point{Y: -150})
	if top := state.LogicalScrollTop(); top != (ListOffset{ItemIx: 3}) {
		t.Fatalf("after the second drag = %+v, want {3 0}", top)
	}
	state.ScrollbarDragEnded()
	if state.IsScrollbarDragging() || state.MaxOffsetForScrollbar().Y != 800 {
		t.Fatalf("after the drag: dragging=%v max=%v, want false/800", state.IsScrollbarDragging(), state.MaxOffsetForScrollbar().Y)
	}
}
