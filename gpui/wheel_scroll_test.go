package gpui

// browser-core ticket 15 (.scratch/browser-core/issues/
// 15-wheel-latching-and-keyboard-scroll.md): the wheel's scroll
// latching, overscroll-behavior containment and the keyboard scroll's
// start node, as Chromium runs them:
//
//   - A wheel gesture latches to ONE scroller: at the gesture's begin
//     the walk starts at the innermost scroller under the wheel and
//     latches the first that can consume the delta in its direction
//     (cc/input/input_handler.cc FindNodeToLatch, :2341-2405); the
//     gesture's updates then scroll the LATCHED scroller only — no
//     chaining mid-gesture (ScrollLatchedScroller scrolls
//     CurrentlyScrollingNode alone, input_handler.cc:2195-2330).
//   - Classic (phaseless) wheel events form one transaction for 500ms
//     after the last event, broken earlier by changed modifiers or a
//     moved position (content/browser/renderer_host/input/
//     mouse_wheel_phase_handler.cc AddPhaseIfNeededAndScheduleEndEvent,
//     kDefaultMouseWheelLatchingTransaction mouse_wheel_phase_handler.
//     h:20); the transaction's end dispatches the wheel end that ends
//     the gesture. The port checks the deadline lazily at the next
//     event, on the application's injectable clock (App.Now), so the
//     tests advance the virtual clock.
//   - overscroll-behavior: contain/none stops the chaining walk at the
//     scroller that cannot consume: the walk latches to it and cuts the
//     chain (FindNodeToLatch's CanPropagate arm, input_handler.cc:
//     2362-2378, cc/input/overscroll_behavior.h PropagatesX/YScroll);
//     the keyboard's chain is cut the same way (core/input/
//     scroll_manager.cc RecomputeScrollChain :166-213, behind the
//     stable RespectOverscrollBehaviorForScrollBubbling feature).
//   - The keyboard scroll starts at the focused element's chain, else
//     the mouse-press node's, else the root scroller (LogicalScroll,
//     scroll_manager.cc:216-232: start_node -> FocusedElement ->
//     mouse_press_node -> the LayoutView node).

import "testing"

// nestedWheelScrollers builds the nesting every wheel-latching test
// uses: an outer 200x200 scroller (range 300) whose content is an inner
// 100x100 scroller at (50,50) (range 200) plus a tall spacer. configure
// styles the inner scroller (overscroll containment).
func nestedWheelScrollers(outer, inner ScrollHandle, configure func(*DivElement) *DivElement) *DivElement {
	innerDiv := Div().Flex().ID("inner").W(Px(100)).H(Px(100)).TrackScroll(inner).OverflowYScroll().FlexShrink0()
	if configure != nil {
		innerDiv = configure(innerDiv)
	}
	innerDiv = innerDiv.Child(Div().Flex().ID("inner-content").W(Px(100)).H(Px(300)).FlexShrink0().Bg(scrollContentColor))
	return Div().Flex().ID("outer").W(Px(200)).H(Px(200)).TrackScroll(outer).OverflowYScroll().FlexCol().
		Child(innerDiv.Ml(Px(50)).Mt(Px(50))).
		Child(Div().Flex().ID("outer-spacer").W(Px(200)).H(Px(350)).FlexShrink0().Bg(scrollContentColor))
}

// TestWheelScrollLatching is the ticket's first acceptance check: a
// wheel gesture latches to the innermost scroller that can scroll, and
// the nested scrollers no longer move together. Three deltas of 80px
// over the inner scroller (range 200) scroll it alone; the third runs
// into the end and does NOT chain to the outer mid-gesture; after the
// 500ms transaction ends, a new gesture latches past the exhausted
// inner scroller to the outer one.
func TestWheelScrollLatching(t *testing.T) {
	inner := NewScrollHandle()
	outer := NewScrollHandle()
	f := newScrollFixture(t, Size{Width: 300, Height: 300}, func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
		return Div().Flex().ID("root").SizeFull().Child(nestedWheelScrollers(outer, inner, nil)).IntoElement()
	})

	// One gesture: three notches at the same position. The inner
	// scroller is the innermost one that can consume the downward
	// delta, so it latches the gesture and takes every notch; when it
	// reaches its end the remaining delta does not chain to the outer
	// (Chromium scrolls only the latched node for the gesture's
	// updates, input_handler.cc ScrollLatchedScroller).
	for i := 0; i < 3; i++ {
		f.wheel(100, 100, ScrollDeltaPixels(0, -80), TouchPhaseMoved)
		if inner.Offset() != (Point{Y: -float32(min((i+1)*80, 200))}) {
			t.Fatalf("after wheel %d: inner offset %v", i+1, inner.Offset())
		}
		if outer.Offset() != (Point{}) {
			t.Fatalf("after wheel %d: outer offset %v, want 0 (the gesture is latched to the inner scroller)", i+1, outer.Offset())
		}
	}

	// The transaction ends 500ms after the last wheel event; the clock
	// is the application's, so the test advances it deterministically.
	f.ta.AdvanceClock(600)
	f.draw()

	// A new gesture: the inner scroller cannot consume a downward delta
	// (it sits at its end), so the walk continues and the outer
	// latches (FindNodeToLatch walks up the scroll tree for the first
	// node that can consume, input_handler.cc:2341-2405).
	f.wheel(100, 100, ScrollDeltaPixels(0, -80), TouchPhaseMoved)
	if inner.Offset() != (Point{Y: -200}) {
		t.Fatalf("inner offset after the new gesture: %v, want -200 (unchanged)", inner.Offset())
	}
	if outer.Offset() != (Point{Y: -80}) {
		t.Fatalf("outer offset after the new gesture: %v, want -80 (the new gesture chains to the outer scroller)", outer.Offset())
	}
}

// TestWheelOverscrollContainStopsChaining pins
// overscroll-behavior: contain (browser-core 15): the inner scroller
// with contain reaches its end within one gesture; a NEW gesture after
// the latching transaction ends cannot chain — the walk latches to the
// contain scroller without consuming, and nothing scrolls.
func TestWheelOverscrollContainStopsChaining(t *testing.T) {
	inner := NewScrollHandle()
	outer := NewScrollHandle()
	f := newScrollFixture(t, Size{Width: 300, Height: 300}, func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
		return Div().Flex().ID("root").SizeFull().Child(
			nestedWheelScrollers(outer, inner, func(d *DivElement) *DivElement { return d.OverscrollContain() }),
		).IntoElement()
	})

	// One gesture scrolls the inner scroller to its end (contain does
	// not stop the scroller's OWN scrolling, only the chaining).
	for i := 0; i < 3; i++ {
		f.wheel(100, 100, ScrollDeltaPixels(0, -80), TouchPhaseMoved)
	}
	if inner.Offset() != (Point{Y: -200}) || outer.Offset() != (Point{}) {
		t.Fatalf("after the first gesture: inner %v outer %v, want -200 and 0", inner.Offset(), outer.Offset())
	}

	// New gestures after the transaction: the inner cannot consume, and
	// its overscroll-behavior: contain cuts the chain — the outer never
	// scrolls.
	f.ta.AdvanceClock(600)
	f.draw()
	f.wheel(100, 100, ScrollDeltaPixels(0, -80), TouchPhaseMoved)
	if inner.Offset() != (Point{Y: -200}) || outer.Offset() != (Point{}) {
		t.Fatalf("after a contained gesture: inner %v outer %v, want both unmoved (contain stops the chaining)", inner.Offset(), outer.Offset())
	}

	// A per-axis contain on the horizontal axis only does not cut a
	// vertical scroll: rebuilding the same nesting with contain on x
	// alone, the vertical chain still runs. (The per-axis rule,
	// CanPropagate's x/y arms, input_handler.cc:2336-2338.)
	innerX := NewScrollHandle()
	outerX := NewScrollHandle()
	f2 := newScrollFixture(t, Size{Width: 300, Height: 300}, func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
		return Div().Flex().ID("root").SizeFull().Child(
			nestedWheelScrollers(outerX, innerX, func(d *DivElement) *DivElement { return d.OverscrollXContain() }),
		).IntoElement()
	})
	for i := 0; i < 3; i++ {
		f2.wheel(100, 100, ScrollDeltaPixels(0, -80), TouchPhaseMoved)
	}
	f2.ta.AdvanceClock(600)
	f2.draw()
	f2.wheel(100, 100, ScrollDeltaPixels(0, -80), TouchPhaseMoved)
	if innerX.Offset() != (Point{Y: -200}) || outerX.Offset() != (Point{Y: -80}) {
		t.Fatalf("x-only contain: inner %v outer %v, want -200 and -80 (a vertical delta still chains)", innerX.Offset(), outerX.Offset())
	}
}

// TestWheelLatchingModifiersBreak pins the transaction's modifier
// break (mouse_wheel_phase_handler.cc's HasDifferentModifiers,
// :203-208): a wheel event with different modifiers starts a new
// transaction, so the latch re-resolves — observable at the state
// level (the latch's snapshot carries the new modifiers).
func TestWheelLatchingModifiersBreak(t *testing.T) {
	inner := NewScrollHandle()
	outer := NewScrollHandle()
	f := newScrollFixture(t, Size{Width: 300, Height: 300}, func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
		return Div().Flex().ID("root").SizeFull().Child(nestedWheelScrollers(outer, inner, nil)).IntoElement()
	})
	f.wheel(100, 100, ScrollDeltaPixels(0, -80), TouchPhaseMoved)
	latch := inputState(f.window).wheelLatch
	if latch == nil || latch.target != (any)(inner.state.offset) {
		t.Fatalf("after the first wheel: latch %v, want the inner scroller's offset cell", latch)
	}
	if latch.modifiers != (Modifiers{}) {
		t.Fatalf("latch modifiers %v, want none", latch.modifiers)
	}
	shift := Modifiers{Shift: true}
	f.wheel(100, 100, ScrollDeltaPixels(0, -80), TouchPhaseMoved)
	// The event dispatch itself goes through the public API; setting
	// the modifiers means dispatching an event that carries them.
	f.window.DispatchInput(&ScrollWheelEvent{
		Position:   Point{X: 100, Y: 100},
		Delta:      ScrollDeltaPixels(0, -80),
		Modifiers:  shift,
		TouchPhase: TouchPhaseMoved,
	}, f.app)
	latch = inputState(f.window).wheelLatch
	if latch == nil || latch.modifiers != shift {
		t.Fatalf("after a shift-modified wheel: latch %v, want a fresh latch with the shift modifiers", latch)
	}
	// The re-claimed gesture is still the inner scroller (it can
	// consume), and its offset advanced to its end.
	if inner.Offset() != (Point{Y: -200}) {
		t.Fatalf("inner offset %v, want -200 (the new gesture scrolls the inner scroller to its end)", inner.Offset())
	}
	if outer.Offset() != (Point{}) {
		t.Fatalf("outer offset %v, want 0", outer.Offset())
	}
}

// TestKeyboardScrollMousePressTarget pins the keyboard scroll's start
// node (browser-core 15, LogicalScroll's fallback order,
// scroll_manager.cc:216-232): with NOTHING focused, the arrows scroll
// the chain of the MOUSE-PRESS node — the element under the last
// mousedown — not only the root scrollers. With a focused element, the
// focused chain wins (ticket 09's behaviour).
func TestKeyboardScrollMousePressTarget(t *testing.T) {
	left := NewScrollHandle()
	right := NewScrollHandle()
	ta := NewTestApp()
	app := ta.App()
	focus := app.FocusHandle().TabStop(true)
	window := NewTestWindow(app, Size{Width: 300, Height: 300})
	view := NewEntity(app, window.Scope(), func(v *scrollView, cx *Context[scrollView]) {
		v.build = func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
			return Div().ID("root").SizeFull().Flex().
				Child(Div().Flex().ID("left").W(Px(140)).H(Px(300)).TrackScroll(left).OverflowYScroll().
					Child(Div().Flex().ID("left-content").W(Px(140)).H(Px(600)).FlexShrink0().
						Child(Div().Flex().ID("left-target").W(Px(140)).H(Px(40)).FlexShrink0().TrackFocus(focus)))).
				Child(Div().Flex().ID("right").W(Px(140)).H(Px(300)).TrackScroll(right).OverflowYScroll().
					Child(Div().Flex().ID("right-content").W(Px(140)).H(Px(600)).FlexShrink0().
						Child(Div().Flex().ID("right-press").W(Px(140)).H(Px(40)).FlexShrink0()))).
				IntoElement()
		}
	})
	window.SetRootView(ViewOf(view))
	f := &scrollFixture{t: t, ta: ta, app: app, window: window, view: view}
	f.draw()

	// Focused: click the focusable target inside the left scroller,
	// then arrows — the focused node's chain scrolls the left scroller.
	f.window.DispatchInput(&MouseDownEvent{Position: Point{X: 70, Y: 20}, Button: MouseButtonLeft, ClickCount: 1}, f.app)
	f.window.DispatchInput(&MouseUpEvent{Position: Point{X: 70, Y: 20}, Button: MouseButtonLeft, ClickCount: 1}, f.app)
	down := MustParseKeystroke("down")
	f.window.DispatchKeyEvent(&KeyDownEvent{Keystroke: down}, f.app)
	f.window.DispatchKeyEvent(&KeyUpEvent{Keystroke: down}, f.app)
	if left.Offset() != (Point{Y: -40}) || right.Offset() != (Point{}) {
		t.Fatalf("focused arrows: left %v right %v, want -40 and 0", left.Offset(), right.Offset())
	}

	// Nothing focused: click (press) the plain content inside the RIGHT
	// scroller, then arrows — the mouse-press node's chain scrolls the
	// right scroller. The click blurs the focused target (the
	// mouse-focus default, browser-core 14).
	f.window.DispatchInput(&MouseDownEvent{Position: Point{X: 220, Y: 20}, Button: MouseButtonLeft, ClickCount: 1}, f.app)
	f.window.DispatchInput(&MouseUpEvent{Position: Point{X: 220, Y: 20}, Button: MouseButtonLeft, ClickCount: 1}, f.app)
	if focused := window.Focused(); focused.Ok {
		t.Fatalf("focus after the plain click: %v, want none (blurred)", focused)
	}
	f.window.DispatchKeyEvent(&KeyDownEvent{Keystroke: down}, f.app)
	f.window.DispatchKeyEvent(&KeyUpEvent{Keystroke: down}, f.app)
	if right.Offset() != (Point{Y: -40}) {
		t.Fatalf("press-node arrows: right offset %v, want -40 (the press node's scroller scrolls)", right.Offset())
	}
	if left.Offset() != (Point{Y: -40}) {
		t.Fatalf("left offset %v, want -40 (unchanged by the right press)", left.Offset())
	}
}

// TestKeyboardScrollContainCutsChaining pins the keyboard half of
// overscroll-behavior: contain (browser-core 15): a focused element
// inside a contain scroller that sits at its end — the arrows cannot
// scroll it, and the contain cut stops the walk before the outer
// scroller. Without contain the keyboard chain DOES continue to the
// outer scroller (BubblingScroll's chaining).
func TestKeyboardScrollContainCutsChaining(t *testing.T) {
	inner := NewScrollHandle()
	outer := NewScrollHandle()
	ta := NewTestApp()
	app := ta.App()
	focus := app.FocusHandle().TabStop(true)
	window := NewTestWindow(app, Size{Width: 300, Height: 300})
	contain := true
	view := NewEntity(app, window.Scope(), func(v *scrollView, cx *Context[scrollView]) {
		v.build = func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
			innerDiv := Div().Flex().ID("inner").W(Px(100)).H(Px(100)).TrackScroll(inner).OverflowYScroll().FlexShrink0()
			if contain {
				innerDiv = innerDiv.OverscrollContain()
			}
			innerDiv = innerDiv.Child(Div().Flex().ID("inner-content").W(Px(100)).H(Px(300)).FlexShrink0().Bg(scrollContentColor).
				Child(Div().Flex().ID("focus-row").W(Px(100)).H(Px(40)).FlexShrink0().TrackFocus(focus)))
			outerDiv := Div().Flex().ID("outer").W(Px(200)).H(Px(200)).TrackScroll(outer).OverflowYScroll().FlexCol().
				Child(innerDiv.Ml(Px(50)).Mt(Px(50))).
				Child(Div().Flex().ID("outer-spacer").W(Px(200)).H(Px(350)).FlexShrink0().Bg(scrollContentColor))
			return Div().Flex().ID("root").SizeFull().Child(outerDiv).IntoElement()
		}
	})
	window.SetRootView(ViewOf(view))
	f := &scrollFixture{t: t, ta: ta, app: app, window: window, view: view}
	f.draw()

	// Scroll the inner scroller to its end with the wheel (its own
	// gesture), end the transaction, then focus the row inside it.
	f.wheel(100, 100, ScrollDeltaPixels(0, -200), TouchPhaseMoved)
	if inner.Offset() != (Point{Y: -200}) || outer.Offset() != (Point{}) {
		t.Fatalf("setup: inner %v outer %v, want -200 and 0", inner.Offset(), outer.Offset())
	}
	f.ta.AdvanceClock(600)
	f.draw()
	app.Update(func(*App) { window.Focus(focus) })
	f.draw()
	down := MustParseKeystroke("down")

	// With contain: the inner cannot take the arrow, the walk stops —
	// nothing scrolls.
	f.window.DispatchKeyEvent(&KeyDownEvent{Keystroke: down}, f.app)
	f.window.DispatchKeyEvent(&KeyUpEvent{Keystroke: down}, f.app)
	if inner.Offset() != (Point{Y: -200}) || outer.Offset() != (Point{}) {
		t.Fatalf("contain arrows: inner %v outer %v, want both unmoved", inner.Offset(), outer.Offset())
	}

	// Without contain: the keyboard chain continues to the outer
	// scroller (the arrows scroll the outer 40px).
	contain = false
	f.update(func(v *scrollView) {})
	f.window.DispatchKeyEvent(&KeyDownEvent{Keystroke: down}, f.app)
	f.window.DispatchKeyEvent(&KeyUpEvent{Keystroke: down}, f.app)
	if inner.Offset() != (Point{Y: -200}) {
		t.Fatalf("chaining arrows: inner %v, want -200 (still at its end)", inner.Offset())
	}
	if outer.Offset() != (Point{Y: -40}) {
		t.Fatalf("chaining arrows: outer %v, want -40 (the keyboard chain continues without contain)", outer.Offset())
	}
}

// TestWheelScrollLatchingExhaustedInnerStartsOuter pins the latch's
// begin rule on its own: when the innermost scroller already sits at
// the end (nothing to consume in the delta's direction), the FIRST
// event of the gesture latches the outer scroller directly — the
// inner scroller never takes part.
func TestWheelScrollLatchingExhaustedInnerStartsOuter(t *testing.T) {
	inner := NewScrollHandle()
	outer := NewScrollHandle()
	f := newScrollFixture(t, Size{Width: 300, Height: 300}, func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
		return Div().Flex().ID("root").SizeFull().Child(nestedWheelScrollers(outer, inner, nil)).IntoElement()
	})
	// Scroll the inner to its end through its own gestures.
	f.wheel(100, 100, ScrollDeltaPixels(0, -200), TouchPhaseMoved)
	if inner.Offset() != (Point{Y: -200}) || outer.Offset() != (Point{}) {
		t.Fatalf("setup: inner %v outer %v, want -200 and 0", inner.Offset(), outer.Offset())
	}
	f.ta.AdvanceClock(600)
	f.draw()

	// A new gesture over the exhausted inner scroller: the outer takes
	// it from the first notch.
	f.wheel(100, 100, ScrollDeltaPixels(0, -80), TouchPhaseMoved)
	if inner.Offset() != (Point{Y: -200}) {
		t.Fatalf("inner offset: %v, want -200", inner.Offset())
	}
	if outer.Offset() != (Point{Y: -80}) {
		t.Fatalf("outer offset: %v, want -80 (the outer latches when the inner cannot consume)", outer.Offset())
	}
}

// TestWheelLineStepIsChromesPixelsPerLine pins the D10 wheel step: one
// wheel line is Chromium's kScrollbarPixelsPerLine = 100/3 CSS px
// (ui/events/blink/web_input_event_builders_win.cc:331, applied at :335
// by the Windows wheel builder: the notch's ticks x lines are multiplied
// by it before becoming the event's pixel delta) — so the default one
// notch of three lines (SPI_GETWHEELSCROLLLINES) scrolls 100 px, where
// the pin used the window's 26px text line height for the div
// (R:div.rs:3828) and a fixed 20px for the list (R:list.rs:1629). The
// 3-line notch lands at float32(100/3)*3 = 99.999996 — the same
// rounding Chrome's own float32 builder produces.
func TestWheelLineStepIsChromesPixelsPerLine(t *testing.T) {
	notch := -(wheelPixelsPerLine * 3) // one notch of 3 lines, downward

	// The div scroller: a 3-line notch moves the offset by the notch.
	handle := NewScrollHandle()
	f := newScrollFixture(t, Size{Width: 400, Height: 300}, func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
		return divScroller(handle, 100, 1200, func(d *DivElement) *DivElement { return d.OverflowYScroll() }).IntoElement()
	})
	if handle.MaxOffset() != (Point{Y: 1100}) {
		t.Fatalf("max offset = %v, want (0,1100)", handle.MaxOffset())
	}
	f.wheel(50, 50, ScrollDeltaLines(0, -3), TouchPhaseMoved)
	if handle.Offset() != (Point{Y: notch}) {
		t.Fatalf("div offset after one 3-line notch = %v, want %v (100/3 px per line, D10)", handle.Offset(), notch)
	}

	// The uniform list: the same notch advances the scroll top.
	listHandle := NewUniformListScrollHandle()
	g := newScrollFixture(t, Size{Width: 200, Height: 100}, func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
		return Div().Flex().ID("list-example").SizeFull().Child(
			UniformList(listHandle, 100, func(start, end int, w *Window, app *App) []AnyElement {
				return uniformItems(start, end)
			}).SizeFull()).IntoElement()
	})
	g.wheel(100, 50, ScrollDeltaLines(0, -3), TouchPhaseMoved)
	if offset := listHandle.Offset(); offset != (Point{Y: notch}) {
		t.Fatalf("list offset after one 3-line notch = %v, want %v (100/3 px per line, D10)", offset, notch)
	}
}
