package base

import (
	"math"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ported tests of scroll_bounce.rs (23/23: 10 pure, 13 on the test
// window). Go adaptations: gpui.ListState has no alignment parameter (Top
// only); ScrollWheelEvent's zero TouchPhase is Started where Rust's
// Default is Moved, so phaseless packets pass Moved explicitly.

// bounceScrollTest is U:scroll_bounce.rs:505-527.
type bounceScrollTest struct {
	handle  gpui.ScrollHandle
	enabled bool
}

func (v *bounceScrollTest) Render(*gpui.Window, *gpui.Context[bounceScrollTest]) gpui.AnyElement {
	return gpui.Div().Flex().P(gpui.Px(20)).Child(
		NewScrollBounce("bounce", v.handle,
			gpui.Div().Flex().
				ID("viewport").
				W(gpui.Px(200)).
				H(gpui.Px(200)).
				OverflowYScroll().
				TrackScroll(v.handle).
				Child(gpui.Div().Flex().H(gpui.Px(600)).WFull())).
			Enabled(v.enabled),
	).IntoElement()
}

// bounceListTest is U:scroll_bounce.rs:542-557.
type bounceListTest struct{ state *gpui.ListState }

func (v *bounceListTest) Render(*gpui.Window, *gpui.Context[bounceListTest]) gpui.AnyElement {
	return NewScrollBounce("bounce-list", v.state,
		gpui.List(v.state, func(int, *gpui.Window, *gpui.App) gpui.AnyElement {
			return gpui.Div().Flex().H(gpui.Px(40)).IntoElement()
		}).W(gpui.PxLength(200)).H(gpui.PxLength(200))).
		Enabled(true).
		IntoElement()
}

// bounceWheel is the test's ScrollWheelEvent at (100, 100).
func bounceWheel(dx, dy float32, phase gpui.TouchPhase) *gpui.ScrollWheelEvent {
	return &gpui.ScrollWheelEvent{
		Position:   gpui.Point{X: 100, Y: 100},
		Delta:      gpui.ScrollDeltaPixels(dx, dy),
		TouchPhase: phase,
	}
}

// bounceScroll is U:scroll_bounce.rs:533-540.
func bounceScroll(cx *gpui.VisualTestContext, delta float32, phase gpui.TouchPhase) {
	cx.SimulateEvent(bounceWheel(0, delta, phase))
}

// bounceDispatchInOneUpdate is the pinned `cx.update(|window, cx| for ..
// window.dispatch_event(..))`: the packets are dispatched within one
// update, with no draw in between.
func bounceDispatchInOneUpdate(cx *gpui.VisualTestContext, events ...*gpui.ScrollWheelEvent) {
	cx.Update(func(w *gpui.Window, app *gpui.App) {
		for _, e := range events {
			w.DispatchInput(e, app)
		}
	})
}

func bounceOpenScrollTest(t *testing.T, ta *gpui.TestApp, enabled bool) (gpui.ScrollHandle, *gpui.VisualTestContext) {
	t.Helper()
	handle := gpui.NewScrollHandle()
	_, cx := ta.AddWindowView(func(v *bounceScrollTest, _ *gpui.Window, _ *gpui.Context[bounceScrollTest]) {
		*v = bounceScrollTest{handle: handle, enabled: enabled}
	})
	sbDraw(t, cx)
	return handle, cx
}

func bounceOpenListTest(t *testing.T) (*gpui.ListState, *gpui.VisualTestContext) {
	t.Helper()
	state := gpui.NewListState(30, 0).MeasureAll()
	ta := gpui.NewTestApp()
	_, cx := ta.AddWindowView(func(v *bounceListTest, _ *gpui.Window, _ *gpui.Context[bounceListTest]) {
		*v = bounceListTest{state: state}
	})
	sbDraw(t, cx)
	return state, cx
}

func listOffsetY(state *gpui.ListState) float32 { return ScrollbarHandleOf(state).Offset().Y }

func listSetOffsetY(state *gpui.ListState, y float32) {
	ScrollbarHandleOf(state).SetOffset(gpui.Point{Y: y})
}

// U:scroll_bounce.rs:559-573
func TestScrollBounceListReverseDragPreservesEventsBeforeTheNextFrame(t *testing.T) {
	state, cx := bounceOpenListTest(t)
	bounceScroll(cx, 100, gpui.TouchPhaseStarted)
	bounceScroll(cx, -140, gpui.TouchPhaseMoved)
	if y := listOffsetY(state); y != -40 {
		t.Fatalf("offset = %v, want -40", y)
	}
	bounceScroll(cx, -10, gpui.TouchPhaseMoved)
	sbDraw(t, cx)
	if y := listOffsetY(state); y != -50 {
		t.Fatalf("offset = %v, want -50", y)
	}
}

// U:scroll_bounce.rs:575-604
func TestScrollBounceTrackpadReleaseBetweenFramesDoesNotBounceInTheMiddle(t *testing.T) {
	state, cx := bounceOpenListTest(t)
	listSetOffsetY(state, -400)
	sbDraw(t, cx)
	origin := state.ViewportBounds().Origin.Y
	bounceDispatchInOneUpdate(cx, bounceWheel(0, -30, gpui.TouchPhaseStarted), bounceWheel(0, 0, gpui.TouchPhaseEnded))
	sbDraw(t, cx)
	if y := state.ViewportBounds().Origin.Y; y != origin {
		t.Fatalf("viewport moved to %v from %v", y, origin)
	}
	if y := listOffsetY(state); !(y < -300 && y > -500) {
		t.Fatalf("offset = %v", y)
	}
}

// U:scroll_bounce.rs:606-635
func TestScrollBounceTrackpadDirectionChangeBetweenFramesDoesNotBounceInTheMiddle(t *testing.T) {
	state, cx := bounceOpenListTest(t)
	listSetOffsetY(state, -400)
	sbDraw(t, cx)
	origin := state.ViewportBounds().Origin.Y
	bounceDispatchInOneUpdate(cx, bounceWheel(0, -30, gpui.TouchPhaseStarted), bounceWheel(0, 5, gpui.TouchPhaseMoved))
	sbDraw(t, cx)
	if y := listOffsetY(state); !(y < -300 && y > -500) {
		t.Fatalf("offset = %v", y)
	}
	if y := state.ViewportBounds().Origin.Y; y != origin {
		t.Fatalf("viewport moved to %v from %v", y, origin)
	}
}

// U:scroll_bounce.rs:637-659
func TestScrollBounceListStretchesOnlyTheDistancePastEitherEdge(t *testing.T) {
	for _, c := range []struct{ start, delta, end float32 }{{-30, 50, 0}, {-970, -50, -1000}} {
		state, cx := bounceOpenListTest(t)
		listSetOffsetY(state, c.start)
		sbDraw(t, cx)
		origin := state.ViewportBounds().Origin.Y
		bounceScroll(cx, c.delta, gpui.TouchPhaseStarted)
		sbDraw(t, cx)
		if y := listOffsetY(state); y != c.end {
			t.Fatalf("start %v: offset = %v, want %v", c.start, y, c.end)
		}
		stretch := state.ViewportBounds().Origin.Y - origin
		if signum32(stretch) != signum32(c.delta) {
			t.Fatalf("start %v: stretch %v has the wrong sign", c.start, stretch)
		}
		// Of the 50 px input, 30 px is ordinary scrolling; only the
		// remaining 20 px may be rubber-banded (resistance reduces it).
		if !(abs32(stretch) > 0 && abs32(stretch) < 20) {
			t.Fatalf("start %v: stretch = %v", c.start, stretch)
		}
	}
}

// U:scroll_bounce.rs:661-682
func TestScrollBounceReverseDragConsumesStretchBeforeScrollingContent(t *testing.T) {
	handle, cx := bounceOpenScrollTest(t, gpui.NewTestApp(), true)
	origin := handle.Bounds().Origin.Y
	if origin != 20 {
		t.Fatalf("origin = %v, want 20", origin)
	}
	bounceScroll(cx, 100, gpui.TouchPhaseStarted)
	sbDraw(t, cx)
	if y := handle.Offset().Y; y != 0 {
		t.Fatalf("offset = %v", y)
	}
	if !(handle.Bounds().Origin.Y > origin) {
		t.Fatalf("not stretched: %v", handle.Bounds().Origin.Y)
	}
	bounceScroll(cx, -140, gpui.TouchPhaseMoved)
	sbDraw(t, cx)
	if y := handle.Offset().Y; y != -40 {
		t.Fatalf("offset = %v, want -40", y)
	}
	if y := handle.Bounds().Origin.Y; y != origin {
		t.Fatalf("origin = %v, want %v", y, origin)
	}
}

// U:scroll_bounce.rs:684-715
func TestScrollBounceTouchReleaseIgnoresMomentumUntilANewTouchTakesOver(t *testing.T) {
	handle, cx := bounceOpenScrollTest(t, gpui.NewTestApp(), true)
	origin := handle.Bounds().Origin.Y
	bounceScroll(cx, 100, gpui.TouchPhaseStarted)
	sbDraw(t, cx)
	if !(handle.Bounds().Origin.Y > origin) {
		t.Fatal("not stretched")
	}
	bounceScroll(cx, 0, gpui.TouchPhaseEnded)
	sbDraw(t, cx)
	released := handle.Bounds().Origin.Y
	// Momentum Moved packets after finger-up must not move the logical
	// list while the returning edge owns the gesture.
	bounceScroll(cx, -400, gpui.TouchPhaseMoved)
	sbDraw(t, cx)
	if y := handle.Offset().Y; y != 0 {
		t.Fatalf("momentum moved the list: %v", y)
	}
	if !(handle.Bounds().Origin.Y <= released) {
		t.Fatalf("edge grew: %v > %v", handle.Bounds().Origin.Y, released)
	}
	// A new finger-down ends suppression and takes over immediately.
	bounceScroll(cx, 0, gpui.TouchPhaseStarted)
	bounceScroll(cx, -250, gpui.TouchPhaseMoved)
	sbDraw(t, cx)
	if !(handle.Offset().Y < 0) {
		t.Fatalf("offset = %v", handle.Offset().Y)
	}
	if y := handle.Bounds().Origin.Y; y != origin {
		t.Fatalf("origin = %v, want %v", y, origin)
	}
}

// U:scroll_bounce.rs:717-736
func TestScrollBouncePhaselessWheelScrollsBackRightAfterBouncing(t *testing.T) {
	state, cx := bounceOpenListTest(t)
	listSetOffsetY(state, -1000)
	sbDraw(t, cx)
	origin := state.ViewportBounds().Origin.Y
	// Smooth-scrolling mouse drivers on macOS: precise deltas, no phase.
	bounceScroll(cx, -50, gpui.TouchPhaseMoved)
	sbDraw(t, cx)
	if !(state.ViewportBounds().Origin.Y < origin) {
		t.Fatal("did not bounce")
	}
	bounceScroll(cx, 200, gpui.TouchPhaseMoved)
	sbDraw(t, cx)
	if !(listOffsetY(state) > -1000) {
		t.Fatalf("offset = %v", listOffsetY(state))
	}
	if y := state.ViewportBounds().Origin.Y; y != origin {
		t.Fatalf("origin = %v, want %v", y, origin)
	}
}

// U:scroll_bounce.rs:738-762
func TestScrollBouncePhaselessWheelBouncesAgainOnlyAfterAPause(t *testing.T) {
	handle, cx := bounceOpenScrollTest(t, gpui.NewTestApp(), true)
	bounceScroll(cx, 50, gpui.TouchPhaseMoved)
	sbDraw(t, cx)
	bounced := handle.Bounds().Origin.Y
	// Momentum after an edge hit keeps pushing outward; it must not
	// stretch further.
	bounceScroll(cx, 50, gpui.TouchPhaseMoved)
	sbDraw(t, cx)
	if !(handle.Bounds().Origin.Y <= bounced) {
		t.Fatalf("stretched further: %v > %v", handle.Bounds().Origin.Y, bounced)
	}
	cx.AdvanceClock(momentumGap.Milliseconds())
	bounceScroll(cx, 50, gpui.TouchPhaseMoved)
	sbDraw(t, cx)
	if !(handle.Bounds().Origin.Y > bounced) {
		t.Fatalf("did not bounce again: %v <= %v", handle.Bounds().Origin.Y, bounced)
	}
	if y := handle.Offset().Y; y != 0 {
		t.Fatalf("offset = %v", y)
	}
}

// U:scroll_bounce.rs:764-808
func TestScrollBounceTinyDragCatchingMomentumDoesNotStartAReverseFling(t *testing.T) {
	handle, cx := bounceOpenScrollTest(t, gpui.NewTestApp(), true)
	handle.SetOffset(gpui.Point{Y: -200})
	sbDraw(t, cx)

	// The old momentum stream ends, then a drag starts at zero
	// displacement when a finger catches the moving content.
	bounceScroll(cx, -60, gpui.TouchPhaseStarted)
	bounceScroll(cx, 0, gpui.TouchPhaseEnded)
	sbDraw(t, cx)
	bounceScroll(cx, -100, gpui.TouchPhaseMoved)
	sbDraw(t, cx)
	bounceScroll(cx, 0, gpui.TouchPhaseEnded)
	sbDraw(t, cx)
	beforeCatch := handle.Offset().Y
	if !(beforeCatch < 0) {
		t.Fatalf("before catch = %v", beforeCatch)
	}
	bounceScroll(cx, 0, gpui.TouchPhaseStarted)
	bounceScroll(cx, 8, gpui.TouchPhaseMoved)
	bounceScroll(cx, 0, gpui.TouchPhaseEnded)
	sbDraw(t, cx)
	stopped := handle.Offset().Y
	if stopped != beforeCatch+8 {
		t.Fatalf("stopped = %v, want %v", stopped, beforeCatch+8)
	}

	// A large reverse momentum packet synthesized from that 8 px movement
	// must not move the logical viewport.
	bounceScroll(cx, 100, gpui.TouchPhaseMoved)
	sbDraw(t, cx)
	if y := handle.Offset().Y; y != stopped {
		t.Fatalf("offset = %v, want %v", y, stopped)
	}

	// A fresh gesture restores ordinary scrolling and momentum.
	bounceScroll(cx, -20, gpui.TouchPhaseStarted)
	bounceScroll(cx, 0, gpui.TouchPhaseEnded)
	bounceScroll(cx, -10, gpui.TouchPhaseMoved)
	sbDraw(t, cx)
	if y := handle.Offset().Y; y != stopped-30 {
		t.Fatalf("offset = %v, want %v", y, stopped-30)
	}
}

// U:scroll_bounce.rs:810-832
func TestScrollBounceDeliberateDragAfterCatchingMomentumCanFling(t *testing.T) {
	handle, cx := bounceOpenScrollTest(t, gpui.NewTestApp(), true)
	handle.SetOffset(gpui.Point{Y: -200})
	sbDraw(t, cx)

	bounceScroll(cx, 0, gpui.TouchPhaseStarted)
	bounceScroll(cx, 24, gpui.TouchPhaseMoved)
	bounceScroll(cx, 0, gpui.TouchPhaseEnded)
	sbDraw(t, cx)
	if y := handle.Offset().Y; y != -176 {
		t.Fatalf("offset = %v, want -176", y)
	}
	bounceScroll(cx, 40, gpui.TouchPhaseMoved)
	sbDraw(t, cx)
	if y := handle.Offset().Y; y != -136 {
		t.Fatalf("offset = %v, want -136", y)
	}
}

// U:scroll_bounce.rs:834-854
func TestScrollBounceShortCatchReleaseStillStretchesPastTheEdge(t *testing.T) {
	handle, cx := bounceOpenScrollTest(t, gpui.NewTestApp(), true)
	handle.SetOffset(gpui.Point{Y: -4})
	sbDraw(t, cx)
	origin := handle.Bounds().Origin.Y

	bounceScroll(cx, 0, gpui.TouchPhaseStarted)
	bounceScroll(cx, 8, gpui.TouchPhaseEnded)
	sbDraw(t, cx)
	if y := handle.Offset().Y; y != 0 {
		t.Fatalf("offset = %v", y)
	}
	if !(handle.Bounds().Origin.Y > origin) {
		t.Fatalf("not stretched: %v <= %v", handle.Bounds().Origin.Y, origin)
	}
}

// U:scroll_bounce.rs:856-877
func TestScrollBounceDisabledAndReducedMotionLeaveTheViewportFixed(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		ta := gpui.NewTestApp()
		if enabled {
			ta.Update(func(app *gpui.App) { app.SetReduceMotion(true) })
		}
		handle, cx := bounceOpenScrollTest(t, ta, enabled)
		origin := handle.Bounds().Origin.Y
		bounceScroll(cx, 100, gpui.TouchPhaseStarted)
		sbDraw(t, cx)
		if y := handle.Bounds().Origin.Y; y != origin {
			t.Fatalf("enabled=%v: origin = %v, want %v", enabled, y, origin)
		}
		bounceScroll(cx, -40, gpui.TouchPhaseMoved)
		sbDraw(t, cx)
		if y := handle.Offset().Y; y != -40 {
			t.Fatalf("enabled=%v: offset = %v, want -40", enabled, y)
		}
	}
}

// U:scroll_bounce.rs:879-887
func TestScrollBounceResistanceAndReversePreserveUnconsumedDistance(t *testing.T) {
	scroll := defaultBouncePhysics()
	scroll.begin(600)
	if r := scroll.pull(100); r != 0 {
		t.Fatalf("pull = %v", r)
	}
	if o := scroll.offset(); !(o > 0 && o < 55) {
		t.Fatalf("offset = %v", o)
	}
	if r := scroll.pull(-130); r != -30 {
		t.Fatalf("reverse pull = %v", r)
	}
	if o := scroll.offset(); o != 0 {
		t.Fatalf("offset = %v", o)
	}
}

// U:scroll_bounce.rs:889-926
func TestScrollBounceDiagonalWobbleStaysWithTheStretch(t *testing.T) {
	handle, cx := bounceOpenScrollTest(t, gpui.NewTestApp(), true)
	origin := handle.Bounds().Origin.Y
	bounceScroll(cx, 100, gpui.TouchPhaseStarted)
	sbDraw(t, cx)
	stretched := handle.Bounds().Origin.Y
	if !(stretched > origin) {
		t.Fatal("not stretched")
	}
	// A swipe that started vertical wobbles horizontal-dominant for a
	// packet; both stay within the axis lock's gesture separation.
	bounceDispatchInOneUpdate(cx, bounceWheel(30, -20, gpui.TouchPhaseMoved), bounceWheel(0, -20, gpui.TouchPhaseMoved))
	sbDraw(t, cx)
	// Both packets shrink the stretch; neither scrolls the list.
	if y := handle.Bounds().Origin.Y; !(y < stretched && y > origin) {
		t.Fatalf("origin = %v (stretched %v, rest %v)", y, stretched, origin)
	}
	if off := handle.Offset(); off != (gpui.Point{}) {
		t.Fatalf("offset = %v", off)
	}
}

// U:scroll_bounce.rs:928-935
func TestScrollBounceMotionBuilderConfiguresTrackingAndResponse(t *testing.T) {
	motion := DefaultScrollBounceMotion().WithTracking(0.4).WithResponse(300 * time.Millisecond)
	if motion.Tracking() != 0.4 || motion.Response() != 300*time.Millisecond {
		t.Fatalf("motion = %+v", motion)
	}
}

// U:scroll_bounce.rs:937-950
func TestScrollBounceTrackingScalesTheFirstStretch(t *testing.T) {
	stretch := func(tracking float32) float32 {
		scroll := defaultBouncePhysics()
		scroll.motion = DefaultScrollBounceMotion().WithTracking(tracking)
		scroll.begin(600)
		scroll.pull(100)
		return scroll.offset()
	}
	if !(stretch(0.3) < stretch(0.55)) || !(stretch(0.55) < stretch(0.8)) {
		t.Fatalf("stretches = %v, %v, %v", stretch(0.3), stretch(0.55), stretch(0.8))
	}
}

// U:scroll_bounce.rs:952-968
func TestScrollBounceResponseScalesTheReturnAndZeroSnaps(t *testing.T) {
	remaining := func(response time.Duration) float32 {
		scroll := defaultBouncePhysics()
		scroll.motion = DefaultScrollBounceMotion().WithResponse(response)
		scroll.begin(600)
		scroll.pull(180)
		scroll.release()
		scroll.step(0.25)
		return scroll.offset()
	}
	if !(remaining(time.Second) > remaining(524*time.Millisecond)) {
		t.Fatal("a slower response did not leave more")
	}
	if !(remaining(524*time.Millisecond) > remaining(200*time.Millisecond)) {
		t.Fatal("a faster response did not leave less")
	}
	if r := remaining(0); r != 0 {
		t.Fatalf("zero response left %v", r)
	}
}

// U:scroll_bounce.rs:970-986
func TestScrollBounceRegrabbingADisplacedEdgeKeepsItsExtent(t *testing.T) {
	scroll := defaultBouncePhysics()
	scroll.begin(600)
	scroll.pull(-150)
	scroll.release()
	scroll.step(0.08)
	before := scroll.offset()
	// The viewport shrank below the displacement while the edge returned;
	// the finger still lands on the edge where it is.
	scroll.begin(40)
	if math.Abs(float64(scroll.offset()-before)) >= 0.001 {
		t.Fatalf("regrab jumped: %v -> %v", before, scroll.offset())
	}
	// A short pull inward moves the edge right away.
	scroll.pull(10)
	if o := scroll.offset(); !(o > before && o < before+10) {
		t.Fatalf("offset = %v (before %v)", o, before)
	}
}

// U:scroll_bounce.rs:988-996
func TestScrollBounceAGestureFromRestAdoptsTheCurrentExtent(t *testing.T) {
	scroll := defaultBouncePhysics()
	scroll.begin(600)
	scroll.begin(40)
	scroll.pull(-100)
	// The stretch saturates below the 40 px viewport, not the 600 px one.
	if o := scroll.offset(); !(o > -40) {
		t.Fatalf("offset = %v", o)
	}
}

// U:scroll_bounce.rs:998-1011
func TestScrollBounceGrabbingTheSpringDoesNotJump(t *testing.T) {
	scroll := defaultBouncePhysics()
	scroll.begin(600)
	scroll.pull(-150)
	scroll.release()
	scroll.step(0.08)
	before := scroll.offset()
	scroll.begin(600)
	if math.Abs(float64(scroll.offset()-before)) >= 0.001 {
		t.Fatalf("grab jumped: %v -> %v", before, scroll.offset())
	}
	held := scroll.offset()
	if scroll.step(0.1) {
		t.Fatal("a held spring stepped")
	}
	if scroll.offset() != held {
		t.Fatalf("held offset moved: %v -> %v", held, scroll.offset())
	}
}

// U:scroll_bounce.rs:1013-1026
func TestScrollBounceSpringHasTheSameTrajectoryAt60And120Hz(t *testing.T) {
	at := func(hz int) float32 {
		scroll := defaultBouncePhysics()
		scroll.begin(600)
		scroll.pull(180)
		scroll.release()
		for i := 0; i < hz/4; i++ {
			scroll.step(1 / float32(hz))
		}
		return scroll.offset()
	}
	if d := math.Abs(float64(at(60) - at(120))); d >= 0.001 {
		t.Fatalf("trajectories differ by %v", d)
	}
}

// U:scroll_bounce.rs:1028-1039
func TestScrollBounceReturnKeepsAVisibleTailAfterAQuarterSecond(t *testing.T) {
	scroll := defaultBouncePhysics()
	scroll.position = 100
	if !scroll.step(0.25) {
		t.Fatal("settled too early")
	}
	// A 100 px release keeps a visible, decelerating tail after 250 ms.
	if o := scroll.offset(); !(o > 10 && o < 30) {
		t.Fatalf("offset = %v", o)
	}
	if !(scroll.velocity < 0) {
		t.Fatalf("velocity = %v", scroll.velocity)
	}
}

// U:scroll_bounce.rs:1041-1057
func TestScrollBounceSpringSettlesWithoutCrossingTheBoundary(t *testing.T) {
	scroll := defaultBouncePhysics()
	scroll.begin(600)
	scroll.pull(-200)
	scroll.release()
	previous := scroll.offset()
	for i := 0; i < 120; i++ {
		scroll.step(1.0 / 120)
		if !(scroll.offset() >= previous && scroll.offset() <= 0) {
			t.Fatalf("step %d: %v after %v", i, scroll.offset(), previous)
		}
		previous = scroll.offset()
	}
	if scroll.offset() != 0 {
		t.Fatalf("offset = %v", scroll.offset())
	}
	if !scroll.suppressMomentum {
		t.Fatal("momentum not suppressed")
	}
	scroll.begin(600)
	if scroll.suppressMomentum {
		t.Fatal("begin kept the suppression")
	}
}
