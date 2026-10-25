package base

import (
	"encoding/json"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ported tests of scrollbar.rs (35/35: 22 pure, 13 on the test window).
//
// Clock: upstream samples Instant::now(); the pure tests use a fixed
// application-clock reading (sbStart) — the transitions only read
// differences — and the window tests use the TestApp virtual clock.

// sbStart stands in for the pinned `Instant::now()` of the pure tests.
const sbStart = 10 * time.Second

// Timing standing in for what a styled layer projects; Base itself ships
// none of these (U:scrollbar.rs:1819-1823).
const (
	sbEnter  = 300 * time.Millisecond
	sbExit   = 500 * time.Millisecond
	sbExpand = 300 * time.Millisecond
)

// U:scrollbar.rs:1806-1817
func TestScrollbarThumbRadiusIsLimitedByItsActualBounds(t *testing.T) {
	vertical := gpui.Bounds{Size: gpui.Size{Width: 8, Height: 80}}
	horizontal := gpui.Bounds{Size: gpui.Size{Width: 80, Height: 6}}

	if r := clampThumbRadius(6, vertical); r != 4 {
		t.Fatalf("vertical = %v", r)
	}
	if r := clampThumbRadius(6, horizontal); r != 3 {
		t.Fatalf("horizontal = %v", r)
	}
	if r := clampThumbRadius(0, vertical); r != 0 {
		t.Fatalf("zero = %v", r)
	}
}

// U:scrollbar.rs:1825-1848
func TestScrollbarVisibilityAnimationUsesDirectionSpecificCurvesAndDurations(t *testing.T) {
	start := sbStart
	animation := hiddenVisibilityAnimation(start)

	animation.setVisible(true, ScrollbarEntranceSlideAndFade, sbEnter, sbExit, start)
	if o := animation.sample(start).opacity; o != 0 {
		t.Fatalf("opacity at start = %v", o)
	}
	if entering := animation.sample(start + sbEnter/2).position; !(entering > 0.5) {
		t.Fatalf("ease-out must advance quickly: %v", entering)
	}
	entered := animation.sample(start + sbEnter)
	if entered.opacity != 1 || entered.position != 1 {
		t.Fatalf("entered = %+v", entered)
	}

	animation.setVisible(false, ScrollbarEntranceSlideAndFade, sbEnter, sbExit, start+sbEnter)
	if exiting := animation.sample(start + sbEnter + sbExit/2).opacity; !(exiting > 0.5) {
		t.Fatalf("ease-in must remain visible early in exit: %v", exiting)
	}
	if o := animation.sample(start + sbEnter + sbExit).opacity; o != 0 {
		t.Fatalf("exited opacity = %v", o)
	}
}

// U:scrollbar.rs:1850-1859
func TestScrollbarEntranceFadesLinearlyWhilePositionEasesOut(t *testing.T) {
	animation := hiddenVisibilityAnimation(sbStart)
	animation.setVisible(true, ScrollbarEntranceSlideAndFade, sbEnter, sbExit, sbStart)

	halfway := animation.sample(sbStart + sbEnter/2)
	if math.Abs(float64(halfway.opacity-0.5)) >= 0.001 {
		t.Fatalf("halfway opacity = %v", halfway.opacity)
	}
	if !(halfway.position > halfway.opacity) {
		t.Fatalf("position %v not ahead of opacity %v", halfway.position, halfway.opacity)
	}
}

// U:scrollbar.rs:1861-1873
func TestScrollbarFadeEntranceSnapsPositionAndAnimatesOpacity(t *testing.T) {
	animation := hiddenVisibilityAnimation(sbStart)
	animation.setVisible(true, ScrollbarEntranceFade, sbEnter, sbExit, sbStart)

	initial := animation.sample(sbStart)
	if initial.opacity != 0 || initial.position != 1 {
		t.Fatalf("initial = %+v", initial)
	}
	halfway := animation.sample(sbStart + sbEnter/2)
	if math.Abs(float64(halfway.opacity-0.5)) >= 0.001 || halfway.position != 1 {
		t.Fatalf("halfway = %+v", halfway)
	}
}

// U:scrollbar.rs:1875-1891
func TestScrollbarActiveVisibilityAdoptsAChangedEntrancePolicy(t *testing.T) {
	halfway := sbStart + sbEnter/2
	animation := hiddenVisibilityAnimation(sbStart)
	animation.setVisible(true, ScrollbarEntranceSlideAndFade, sbEnter, sbExit, sbStart)
	before := animation.sample(halfway)

	animation.setVisible(true, ScrollbarEntranceFade, sbEnter, sbExit, halfway)
	after := animation.sample(halfway)

	if after.opacity != before.opacity {
		t.Fatalf("policy changes must not flash: %v -> %v", before.opacity, after.opacity)
	}
	if after.position != 1 {
		t.Fatalf("fade entrance must stop stale sliding: %v", after.position)
	}
}

// U:scrollbar.rs:1893-1906
func TestScrollbarBaseShipsNoMotionOfItsOwn(t *testing.T) {
	motion := DefaultScrollbarMotion()
	if motion.Enter() != 0 || motion.Exit() != 0 || motion.Expand() != 0 {
		t.Fatalf("motion = %+v", motion)
	}
	if motion.Entrance() != ScrollbarEntranceFade || motion.ThumbHoverEntrance() != ScrollbarEntranceFade {
		t.Fatalf("entrances = %v, %v", motion.Entrance(), motion.ThumbHoverEntrance())
	}
	if motion.Idle() != scrollbarDefaultIdle {
		t.Fatalf("the visibility hold is behavior, not motion, and must stay usable: %v", motion.Idle())
	}
}

// U:scrollbar.rs:1908-1926
func TestScrollbarHoverModeSlidesOnlyWhenTheThumbIsHovered(t *testing.T) {
	motion := DefaultScrollbarMotion().
		WithEntrance(ScrollbarEntranceFade).
		WithThumbHoverEntrance(ScrollbarEntranceSlideAndFade)

	if e := motion.entranceFor(ScrollbarModeHover, false); e != ScrollbarEntranceFade {
		t.Fatalf("hover, not on thumb = %v", e)
	}
	if e := motion.entranceFor(ScrollbarModeHover, true); e != ScrollbarEntranceSlideAndFade {
		t.Fatalf("hover, on thumb = %v", e)
	}
	if e := motion.entranceFor(ScrollbarModeScrolling, true); e != ScrollbarEntranceFade {
		t.Fatalf("scrolling, on thumb = %v", e)
	}
}

// U:scrollbar.rs:1928-1933
func TestScrollbarHiddenScrollingModeDoesNotTrackThumbHover(t *testing.T) {
	if tracksThumbHover(ScrollbarModeScrolling, false) {
		t.Fatal("hidden scrolling mode tracks thumb hover")
	}
	if !tracksThumbHover(ScrollbarModeScrolling, true) || !tracksThumbHover(ScrollbarModeHover, false) {
		t.Fatal("visible or hover mode does not track thumb hover")
	}
}

// U:scrollbar.rs:1935-1940
func TestScrollbarVisibleScrollingModeStaysVisibleWhileHovered(t *testing.T) {
	if hoverKeepsVisible(ScrollbarModeScrolling, true, false) {
		t.Fatal("hover kept a hidden scrolling scrollbar")
	}
	if !hoverKeepsVisible(ScrollbarModeScrolling, true, true) || !hoverKeepsVisible(ScrollbarModeHover, true, false) {
		t.Fatal("hover did not keep visibility")
	}
}

// U:scrollbar.rs:1942-1969
func TestScrollbarMotionlessBaseSnapsEveryTransition(t *testing.T) {
	now := sbStart
	motion := DefaultScrollbarMotion()
	visibility := hiddenVisibilityAnimation(now)

	visibility.setVisible(true, motion.Entrance(), motion.Enter(), motion.Exit(), now)
	shown := visibility.sample(now)
	if shown.opacity != 1 || shown.position != 1 || shown.running {
		t.Fatalf("shown = %+v (a motionless theme must request no frames)", shown)
	}
	if p := visibilityTranslation(gpui.AxisVertical, 16, shown.position); p != (gpui.Point{}) {
		t.Fatalf("translation = %v", p)
	}

	visibility.setVisible(false, motion.Entrance(), motion.Enter(), motion.Exit(), now)
	hidden := visibility.sample(now)
	if hidden.opacity != 0 || hidden.position != 0 || hidden.running {
		t.Fatalf("hidden = %+v", hidden)
	}

	width := newWidthAnimation(now)
	if v, running := width.setTarget(8, motion.Expand(), now); v != 8 || running {
		t.Fatalf("width = %v, %v", v, running)
	}
}

// U:scrollbar.rs:1971-1990
func TestScrollbarAZeroDurationSettlesATransitionAlreadyInFlight(t *testing.T) {
	animation := hiddenVisibilityAnimation(sbStart)
	animation.setVisible(true, ScrollbarEntranceSlideAndFade, sbEnter, sbExit, sbStart)

	// Reduced motion turns on midway through the entrance.
	midway := sbStart + sbEnter/2
	animation.setVisible(true, ScrollbarEntranceSlideAndFade, 0, 0, midway)
	settled := animation.sample(midway)
	if settled.opacity != 1 || settled.position != 1 || settled.running {
		t.Fatalf("settled = %+v", settled)
	}
}

// U:scrollbar.rs:1992-2010
func TestScrollbarThumbExpansionAnimatesInBothDirections(t *testing.T) {
	start := sbStart
	animation := newWidthAnimation(start)
	if v, running := animation.setTarget(6, sbExpand, start); v != 6 || running {
		t.Fatalf("first target = %v, %v", v, running)
	}

	initial, running := animation.setTarget(8, sbExpand, start)
	if initial != 6 || !running {
		t.Fatalf("expanding = %v, %v", initial, running)
	}
	if halfway, _ := animation.sample(start + sbExpand/2); !(halfway > 7) {
		t.Fatalf("expanded halfway = %v", halfway)
	}

	reversal := start + sbExpand/2
	before, _ := animation.sample(reversal)
	afterReversal, running := animation.setTarget(6, sbExpand, reversal)
	if afterReversal != before || !running {
		t.Fatalf("reversal = %v (before %v), %v", afterReversal, before, running)
	}
	if v, _ := animation.sample(reversal + sbExpand); v != 6 {
		t.Fatalf("reversed = %v", v)
	}
}

// U:scrollbar.rs:2012-2020
func TestScrollbarReducedMotionSnapsThumbExpansion(t *testing.T) {
	animation := newWidthAnimation(sbStart)
	if v, running := animation.setTarget(8, 0, sbStart); v != 8 || running {
		t.Fatalf("width = %v, %v", v, running)
	}
}

// U:scrollbar.rs:2022-2036
func TestScrollbarVisibilityTranslationMovesTowardTheNearestEdge(t *testing.T) {
	if p := visibilityTranslation(gpui.AxisVertical, 16, 0); p != (gpui.Point{X: 16}) {
		t.Fatalf("vertical hidden = %v", p)
	}
	if p := visibilityTranslation(gpui.AxisHorizontal, 16, 0); p != (gpui.Point{Y: 16}) {
		t.Fatalf("horizontal hidden = %v", p)
	}
	if p := visibilityTranslation(gpui.AxisVertical, 16, 1); p != (gpui.Point{}) {
		t.Fatalf("vertical shown = %v", p)
	}
}

// U:scrollbar.rs:2038-2059
func TestScrollbarVisibilityAnimationReversesFromCurrentProgress(t *testing.T) {
	animation := hiddenVisibilityAnimation(sbStart)
	animation.setVisible(true, ScrollbarEntranceSlideAndFade, sbEnter, sbExit, sbStart)
	reversalTime := sbStart + 60*time.Millisecond
	before := animation.sample(reversalTime)

	animation.setVisible(false, ScrollbarEntranceSlideAndFade, sbEnter, sbExit, reversalTime)
	reversed := animation.sample(reversalTime)
	if reversed.opacity != before.opacity || reversed.position != before.position {
		t.Fatalf("reversed = %+v, before = %+v", reversed, before)
	}
	after := animation.sample(reversalTime + 10*time.Millisecond)
	if !(after.opacity < before.opacity) || !(after.position < before.position) {
		t.Fatalf("after = %+v, before = %+v", after, before)
	}
}

// U:scrollbar.rs:2061-2115
func TestScrollbarAlwaysHoverDragAndRecentScrollRequestVisibility(t *testing.T) {
	now := sbStart
	idle := scrollbarDefaultIdle
	none := gpui.Optional[time.Duration]{}
	if !wantsVisible(ScrollbarModeAlways, false, false, none, idle, now) {
		t.Fatal("always")
	}
	if !wantsVisible(ScrollbarModeHover, true, false, none, idle, now) {
		t.Fatal("hover")
	}
	if !wantsVisible(ScrollbarModeScrolling, false, true, none, idle, now) {
		t.Fatal("drag")
	}
	if !wantsVisible(ScrollbarModeScrolling, false, false, gpui.Some(now-idle+time.Millisecond), idle, now) {
		t.Fatal("recent scroll")
	}
	if wantsVisible(ScrollbarModeScrolling, false, false, gpui.Some(now-idle), idle, now) {
		t.Fatal("expired scroll")
	}

	settled := hiddenVisibilityAnimation(now - sbEnter)
	settled.setVisible(true, ScrollbarEntranceSlideAndFade, sbEnter, sbExit, now-sbEnter)
	if settled.sample(now).running {
		t.Fatal("idle hold must not animate")
	}
}

// U:scrollbar.rs:2117-2135
func TestScrollbarLeavingHoverStartsAFreshIdleHold(t *testing.T) {
	enteredAt := sbStart
	leftAt := enteredAt + 5*time.Second
	state := newScrollbarState(enteredAt).get()

	hovered := state.withHovered(gpui.Some(gpui.AxisVertical), enteredAt)
	if v, ok := hovered.lastScrollTime.Get(); !ok || v != enteredAt {
		t.Fatalf("hovered last scroll = %v %v", v, ok)
	}
	left := hovered.withHovered(optAxis{}, leftAt)
	if v, ok := left.lastScrollTime.Get(); !ok || v != leftAt {
		t.Fatalf("left last scroll = %v %v", v, ok)
	}
	if !wantsVisible(ScrollbarModeHover, false, false, left.lastScrollTime, scrollbarDefaultIdle, leftAt+scrollbarDefaultIdle-time.Millisecond) {
		t.Fatal("leaving hover did not start a fresh hold")
	}
}

// U:scrollbar.rs:2137-2157
func TestScrollbarIdleBoundaryStartsTheExitWithoutAJump(t *testing.T) {
	activity := sbStart
	exitStart := activity + scrollbarDefaultIdle
	animation := hiddenVisibilityAnimation(activity - sbEnter)
	animation.setVisible(true, ScrollbarEntranceFade, sbEnter, sbExit, activity-sbEnter)
	if wantsVisible(ScrollbarModeScrolling, false, false, gpui.Some(activity), scrollbarDefaultIdle, exitStart) {
		t.Fatal("visible past the idle boundary")
	}

	animation.setVisible(false, ScrollbarEntranceFade, sbEnter, sbExit, exitStart)
	start := animation.sample(exitStart)
	if start.opacity != 1 || start.position != 1 {
		t.Fatalf("exit start = %+v", start)
	}
	if o := animation.sample(exitStart + sbExit).opacity; o != 0 {
		t.Fatalf("exit end opacity = %v", o)
	}
}

// sbTestHandle is the tests' ScrollbarHandle (U:scrollbar.rs:2159-2202).
type sbTestHandle struct {
	offset      *gpui.Point
	contentSize gpui.Size
	dragStarts  *int
	dragEnds    *int
}

func newSbTestHandle(contentSize gpui.Size) sbTestHandle {
	return sbTestHandle{offset: &gpui.Point{}, contentSize: contentSize, dragStarts: new(int), dragEnds: new(int)}
}

func (h sbTestHandle) ViewportBounds() gpui.Bounds {
	return gpui.Bounds{Size: gpui.Size{Width: 100, Height: 100}}
}
func (h sbTestHandle) Offset() gpui.Point          { return *h.offset }
func (h sbTestHandle) SetOffset(offset gpui.Point) { *h.offset = offset }
func (h sbTestHandle) ContentSize() gpui.Size      { return h.contentSize }
func (h sbTestHandle) StartDrag()                  { *h.dragStarts++ }
func (h sbTestHandle) EndDrag()                    { *h.dragEnds++ }

// sbHarness is U:scrollbar.rs:2204-2217.
type sbHarness struct {
	handle sbTestHandle
	axis   ScrollbarAxis
	mode   ScrollbarMode
}

func (v *sbHarness) Render(*gpui.Window, *gpui.Context[sbHarness]) gpui.AnyElement {
	return gpui.Div().Flex().
		Relative().
		W(gpui.Px(100)).
		H(gpui.Px(100)).
		Child(NewScrollbar(v.handle).Axis(v.axis).Mode(v.mode)).
		IntoElement()
}

// sbDraw is `cx.update(|window, cx| window.draw(cx).clear(cx))`.
func sbDraw(t *testing.T, cx *gpui.VisualTestContext) {
	t.Helper()
	var err error
	cx.Update(func(w *gpui.Window, _ *gpui.App) { _, err = gpui.DrawWindowFrame(w) })
	if err != nil {
		t.Fatal(err)
	}
}

// sbOpen is U:scrollbar.rs:2219-2232 harness.
func sbOpen(t *testing.T, axis ScrollbarAxis, mode ScrollbarMode, contentSize gpui.Size) (gpui.Entity[sbHarness], *gpui.VisualTestContext, sbTestHandle) {
	t.Helper()
	handle := newSbTestHandle(contentSize)
	ta := gpui.NewTestApp()
	view, cx := ta.AddWindowView(func(v *sbHarness, _ *gpui.Window, _ *gpui.Context[sbHarness]) {
		*v = sbHarness{handle: handle, axis: axis, mode: mode}
	})
	sbDraw(t, cx)
	return view, cx, handle
}

// U:scrollbar.rs:2234-2244
func TestScrollbarExplicitViewportBoundsOverrideHandleBounds(t *testing.T) {
	expected := gpui.Bounds{Origin: gpui.Point{X: 12, Y: 24}, Size: gpui.Size{Width: 240, Height: 96}}
	scrollbar := ScrollbarVertical(newSbTestHandle(gpui.Size{Width: 240, Height: 480})).ViewportBounds(expected)
	if b := scrollbar.resolvedViewportBounds(gpui.Bounds{}); b != expected {
		t.Fatalf("bounds = %v", b)
	}
}

// U:scrollbar.rs:2246-2253
func TestScrollbarLayoutViewportUsesCurrentElementBounds(t *testing.T) {
	expected := gpui.Bounds{Origin: gpui.Point{X: 20, Y: 30}, Size: gpui.Size{Width: 180, Height: 12}}
	scrollbar := ScrollbarHorizontal(newSbTestHandle(gpui.Size{Width: 600, Height: 12})).ViewportFromLayout()
	if b := scrollbar.resolvedViewportBounds(expected); b != expected {
		t.Fatalf("bounds = %v", b)
	}
}

// U:scrollbar.rs:2255-2295
func TestScrollbarTypedStylesAreFluentAndIncludeGeometry(t *testing.T) {
	track := gpui.Hsla{H: 0.1, S: 0.2, L: 0.3, A: 1}
	border := gpui.Hsla{H: 0.2, S: 0.3, L: 0.4, A: 1}
	thumb := gpui.Hsla{H: 0.3, S: 0.4, L: 0.5, A: 1}
	hover := gpui.Hsla{H: 0.4, S: 0.5, L: 0.6, A: 1}
	active := gpui.Hsla{H: 0.5, S: 0.6, L: 0.7, A: 1}
	scrollbar := NewScrollbar(newSbTestHandle(gpui.Size{})).Styles(func(styles ScrollbarStyles) ScrollbarStyles {
		return styles.
			Track(func(style ScrollbarTrackStyle) ScrollbarTrackStyle {
				return style.Width(14).Bg(track).BorderColor(border).
					When(false, func(style ScrollbarTrackStyle) ScrollbarTrackStyle { return style.Width(99) })
			}).
			Thumb(func(style ScrollbarThumbStyle) ScrollbarThumbStyle {
				return style.Width(7).Inset(3).Radius(3.5).MinLength(40).Bg(thumb)
			}).
			ThumbHover(func(style ScrollbarThumbStyle) ScrollbarThumbStyle { return style.Width(9).Bg(hover) }).
			ThumbActive(func(style ScrollbarThumbStyle) ScrollbarThumbStyle { return style.Radius(4.5).Bg(active) })
	})

	st := scrollbar.styles
	if v, _ := st.track.background.Get(); v != track {
		t.Fatalf("track bg = %v", v)
	}
	if v, _ := st.track.border.Get(); v != border {
		t.Fatalf("track border = %v", v)
	}
	if v, _ := st.track.width.Get(); v != 14 {
		t.Fatalf("track width = %v", v)
	}
	if !st.thumb.background.IsSome() {
		t.Fatal("thumb bg missing")
	}
	for name, got := range map[string]gpui.Optional[float32]{
		"thumb width": st.thumb.width, "thumb inset": st.thumb.inset, "thumb radius": st.thumb.radius,
		"thumb min": st.thumb.minLength, "hover width": st.thumbHover.width, "active radius": st.thumbActive.radius,
	} {
		want := map[string]float32{"thumb width": 7, "thumb inset": 3, "thumb radius": 3.5, "thumb min": 40, "hover width": 9, "active radius": 4.5}[name]
		if v, ok := got.Get(); !ok || v != want {
			t.Fatalf("%s = %v %v, want %v", name, v, ok, want)
		}
	}
	if !st.thumbHover.background.IsSome() || !st.thumbActive.background.IsSome() {
		t.Fatal("state backgrounds missing")
	}
}

// U:scrollbar.rs:2297-2316
func TestScrollbarUnstyledThumbFollowsTheThemeRatherThanAFixedColour(t *testing.T) {
	ta := gpui.NewTestApp()
	ta.Update(func(app *gpui.App) {
		scrollbar := NewScrollbar(newSbTestHandle(gpui.Size{}))

		light := gpui.Hsla{H: 0, S: 0, L: 0.04, A: 1}
		ThemeGlobalMut(app).Tokens.Colors.Foreground = light
		onLight := scrollbar.styleForNormal(app).thumb

		dark := gpui.Hsla{H: 0, S: 0, L: 0.98, A: 1}
		ThemeGlobalMut(app).Tokens.Colors.Foreground = dark
		onDark := scrollbar.styleForNormal(app).thumb

		light.A, dark.A = 0.35, 0.35
		if onLight != gpui.SolidBackground(light) || onDark != gpui.SolidBackground(dark) {
			t.Fatalf("thumbs = %v / %v", onLight, onDark)
		}
		// A thumb that never moved with the palette was invisible on one
		// of the two surfaces.
		if onLight == onDark {
			t.Fatal("thumb did not follow the palette")
		}
	})
}

// U:scrollbar.rs:2318-2330
func TestScrollbarAStyledThumbStillBeatsTheThemeDerivedDefault(t *testing.T) {
	ta := gpui.NewTestApp()
	ta.Update(func(app *gpui.App) {
		chosen := gpui.Hsla{H: 0.6, S: 0.5, L: 0.5, A: 1}
		ThemeGlobalMut(app).Tokens.Colors.Foreground = gpui.Hsla{H: 0, S: 0, L: 0.98, A: 1}

		scrollbar := NewScrollbar(newSbTestHandle(gpui.Size{})).Styles(func(styles ScrollbarStyles) ScrollbarStyles {
			return styles.Thumb(func(style ScrollbarThumbStyle) ScrollbarThumbStyle { return style.Bg(chosen) })
		})
		if thumb := scrollbar.styleForNormal(app).thumb; thumb != gpui.SolidBackground(chosen) {
			t.Fatalf("thumb = %v", thumb)
		}
	})
}

// U:scrollbar.rs:2332-2357
func TestScrollbarInstanceStylesOverrideThemeScrollbarDefaults(t *testing.T) {
	ta := gpui.NewTestApp()
	ta.Update(func(app *gpui.App) {
		themeTrack := gpui.Hsla{H: 0.1, S: 0.2, L: 0.3, A: 1}
		themeThumb := gpui.Hsla{H: 0.2, S: 0.3, L: 0.4, A: 1}
		instanceThumb := gpui.Hsla{H: 0.3, S: 0.4, L: 0.5, A: 1}

		ThemeGlobalMut(app).Scrollbar = NewScrollbarTheme().
			WithMode(ScrollbarModeAlways).
			WithMotion(DefaultScrollbarMotion()).
			WithStyles(ScrollbarStyles{}.
				Track(func(style ScrollbarTrackStyle) ScrollbarTrackStyle { return style.Width(13).Bg(themeTrack) }).
				Thumb(func(style ScrollbarThumbStyle) ScrollbarThumbStyle { return style.Width(7).Bg(themeThumb) }))

		scrollbar := NewScrollbar(newSbTestHandle(gpui.Size{})).Styles(func(styles ScrollbarStyles) ScrollbarStyles {
			return styles.Thumb(func(style ScrollbarThumbStyle) ScrollbarThumbStyle { return style.Bg(instanceThumb) })
		})
		style := scrollbar.styleForNormal(app)

		if style.thumb != gpui.SolidBackground(instanceThumb) {
			t.Fatalf("thumb = %v", style.thumb)
		}
		if style.track != themeTrack {
			t.Fatalf("track = %v", style.track)
		}
		if style.width != 7 {
			t.Fatalf("width = %v", style.width)
		}
		theme := activeTheme(app)
		if v, ok := theme.Scrollbar.Styles().track.width.Get(); !ok || v != 13 {
			t.Fatalf("theme track width = %v %v", v, ok)
		}
	})
}

// U:scrollbar.rs:2359-2369
func TestScrollbarAutoHideModesUseASixPixelRestingThumb(t *testing.T) {
	ta := gpui.NewTestApp()
	ta.Update(func(app *gpui.App) {
		handle := newSbTestHandle(gpui.Size{})
		scrolling := NewScrollbar(handle).Mode(ScrollbarModeScrolling)
		always := NewScrollbar(handle).Mode(ScrollbarModeAlways)
		if w := scrolling.styleForNormal(app).width; w != 6 {
			t.Fatalf("scrolling = %v", w)
		}
		if w := always.styleForNormal(app).width; w != 6 {
			t.Fatalf("always = %v", w)
		}
	})
}

// U:scrollbar.rs:2371-2386
func TestScrollbarEveryModeExpandsOnlyForThumbHover(t *testing.T) {
	ta := gpui.NewTestApp()
	ta.Update(func(app *gpui.App) {
		handle := newSbTestHandle(gpui.Size{})
		for _, mode := range []ScrollbarMode{ScrollbarModeScrolling, ScrollbarModeHover, ScrollbarModeAlways} {
			scrollbar := NewScrollbar(handle).Mode(mode)
			if w := scrollbar.styleForNormal(app).width; w != 6 {
				t.Fatalf("%v normal = %v", mode, w)
			}
			if w := scrollbar.styleForHoveredBar(app).width; w != 6 {
				t.Fatalf("%v hovered bar = %v", mode, w)
			}
			if w := scrollbar.styleForHoveredThumb(app).width; w != 8 {
				t.Fatalf("%v hovered thumb = %v", mode, w)
			}
		}
	})
}

// U:scrollbar.rs:2388-2399
func TestScrollbarVerticalTrackClickUpdatesVerticalOffset(t *testing.T) {
	_, cx, vertical := sbOpen(t, ScrollbarAxisVertical, ScrollbarModeAlways, gpui.Size{Width: 100, Height: 500})
	cx.SimulateClick(gpui.Point{X: 95, Y: 80}, gpui.Modifiers{})
	if off := vertical.Offset(); !(off.Y < 0) || off.X != 0 {
		t.Fatalf("offset = %v", off)
	}
}

// U:scrollbar.rs:2401-2412
func TestScrollbarHorizontalTrackClickUpdatesHorizontalOffset(t *testing.T) {
	_, cx, horizontal := sbOpen(t, ScrollbarAxisHorizontal, ScrollbarModeAlways, gpui.Size{Width: 500, Height: 100})
	cx.SimulateClick(gpui.Point{X: 80, Y: 95}, gpui.Modifiers{})
	if off := horizontal.Offset(); !(off.X < 0) || off.Y != 0 {
		t.Fatalf("offset = %v", off)
	}
}

// U:scrollbar.rs:2414-2424
func TestScrollbarNoOverflowHasNoInteractiveTrack(t *testing.T) {
	_, cx, handle := sbOpen(t, ScrollbarAxisBoth, ScrollbarModeAlways, gpui.Size{Width: 100, Height: 100})
	cx.SimulateClick(gpui.Point{X: 95, Y: 80}, gpui.Modifiers{})
	if off := handle.Offset(); off != (gpui.Point{}) {
		t.Fatalf("offset = %v", off)
	}
}

// U:scrollbar.rs:2426-2436
func TestScrollbarHiddenHoverScrollbarIgnoresTrackClick(t *testing.T) {
	_, cx, handle := sbOpen(t, ScrollbarAxisVertical, ScrollbarModeHover, gpui.Size{Width: 100, Height: 500})
	cx.SimulateClick(gpui.Point{X: 95, Y: 80}, gpui.Modifiers{})
	if off := handle.Offset(); off != (gpui.Point{}) {
		t.Fatalf("offset = %v", off)
	}
}

// U:scrollbar.rs:2438-2465
func TestScrollbarHiddenHoverScrollbarIgnoresThumbDrag(t *testing.T) {
	_, cx, handle := sbOpen(t, ScrollbarAxisVertical, ScrollbarModeHover, gpui.Size{Width: 100, Height: 500})
	left := gpui.MouseButtonLeft
	cx.SimulateMouseDown(gpui.Point{X: 95, Y: 20}, gpui.MouseButtonLeft, gpui.Modifiers{})
	cx.SimulateMouseMove(gpui.Point{X: 95, Y: 70}, &left, gpui.Modifiers{})
	cx.SimulateMouseUp(gpui.Point{X: 95, Y: 70}, gpui.MouseButtonLeft, gpui.Modifiers{})

	if *handle.dragStarts != 0 || *handle.dragEnds != 0 {
		t.Fatalf("drag starts/ends = %d/%d", *handle.dragStarts, *handle.dragEnds)
	}
	if off := handle.Offset(); off != (gpui.Point{}) {
		t.Fatalf("offset = %v", off)
	}
}

// U:scrollbar.rs:2467-2481
func TestScrollbarHoveringRevealsScrollbarForTrackInteraction(t *testing.T) {
	_, cx, handle := sbOpen(t, ScrollbarAxisVertical, ScrollbarModeHover, gpui.Size{Width: 100, Height: 500})
	// Base ships no motion, so the reveal is immediate.
	cx.SimulateMouseMove(gpui.Point{X: 95, Y: 50}, nil, gpui.Modifiers{})
	cx.RunUntilParked()

	cx.SimulateClick(gpui.Point{X: 95, Y: 80}, gpui.Modifiers{})
	if off := handle.Offset(); !(off.Y < 0) {
		t.Fatalf("offset = %v", off)
	}
}

// sbObserver records a view's notifications (the pinned cx.observe).
type sbObserver struct{}

// U:scrollbar.rs:2483-2553
func TestScrollbarDragNotificationsAreThrottledAndDeliverTheLatestOffset(t *testing.T) {
	view, cx, handle := sbOpen(t, ScrollbarAxisVertical, ScrollbarModeAlways, gpui.Size{Width: 100, Height: 1000})
	var observed []float32
	cx.Update(func(_ *gpui.Window, app *gpui.App) {
		observer := gpui.NewEntity(app, cx.RootScope(), func(*sbObserver, *gpui.Context[sbObserver]) {})
		observer.Update(app, func(_ *sbObserver, ocx *gpui.Context[sbObserver]) {
			ocx.Observe(view, func(*sbObserver, gpui.Entity[sbHarness], *gpui.Context[sbObserver]) {
				observed = append(observed, handle.Offset().Y)
			})
		})
	})
	state := newScrollbarState(cx.Now())
	now := cx.Now()
	interval := time.Duration(math.Round(1e9 / 30.0))
	inner := state.get().withDragPos(gpui.AxisVertical, gpui.Point{}, now)
	inner.lastUpdate = now
	state.set(inner)
	update := func(offset float32, elapsed time.Duration) {
		handle.SetOffset(gpui.Point{Y: offset})
		cx.Update(func(w *gpui.Window, app *gpui.App) {
			state.notifyDrag(now+elapsed, interval, view.EntityID(), w, app)
		})
		cx.RunUntilParked()
	}
	update(-10, 0)
	cx.AdvanceClock(16)
	update(-20, 16*time.Millisecond)
	update(-30, 16*time.Millisecond)
	// Rendering at 120 Hz must not bypass the configured 30 Hz timer.
	cx.Update(func(w *gpui.Window, app *gpui.App) { w.SimulateNextFrame(app) })
	cx.RunUntilParked()
	if len(observed) != 0 {
		t.Fatalf("observed early: %v", observed)
	}
	cx.AdvanceClock(16)
	cx.RunUntilParked()
	if len(observed) != 0 {
		t.Fatalf("observed before the interval: %v", observed)
	}
	cx.AdvanceClock(2)
	cx.RunUntilParked()
	if !slices.Equal(observed, []float32{-30}) {
		t.Fatalf("one trailing notification must see the newest offset even when movement stops: %v", observed)
	}

	// A second burst gets its own interval, and release invalidates its
	// timer.
	next := state.get().lastUpdate
	handle.SetOffset(gpui.Point{Y: -40})
	cx.Update(func(w *gpui.Window, app *gpui.App) { state.notifyDrag(next, interval, view.EntityID(), w, app) })
	cx.RunUntilParked()
	state.set(state.get().withUnsetDragPos(cx.Now()))
	handle.SetOffset(gpui.Point{Y: -50})
	cx.Update(func(_ *gpui.Window, app *gpui.App) { app.Notify(view.EntityID()) })
	if !slices.Equal(observed, []float32{-30, -50}) {
		t.Fatalf("observed = %v", observed)
	}
	// The interval rounded up to the virtual clock's millisecond.
	cx.AdvanceClock(34)
	cx.RunUntilParked()
	if !slices.Equal(observed, []float32{-30, -50}) {
		t.Fatalf("release must not leave a stale trailing notification: %v", observed)
	}
}

// U:scrollbar.rs:2555-2568
func TestScrollbarFullTrackThumbDoesNotResetTheScrollOffset(t *testing.T) {
	geometry := newThumbGeometry(20, 40, 500, 0, 4, 64)
	offset := gpui.Point{Y: -100}
	if got := geometry.dragOffset(gpui.AxisVertical, gpui.Point{Y: 50}, gpui.Point{Y: 8}, offset); got != offset {
		t.Fatalf("offset = %v", got)
	}
}

// U:scrollbar.rs:2570-2596
func TestScrollbarThumbDragNotifiesHandleStartAndEnd(t *testing.T) {
	_, cx, handle := sbOpen(t, ScrollbarAxisVertical, ScrollbarModeAlways, gpui.Size{Width: 100, Height: 500})
	left := gpui.MouseButtonLeft
	cx.SimulateMouseDown(gpui.Point{X: 95, Y: 20}, gpui.MouseButtonLeft, gpui.Modifiers{})
	cx.SimulateMouseMove(gpui.Point{X: 95, Y: 70}, &left, gpui.Modifiers{})
	cx.SimulateMouseUp(gpui.Point{X: 95, Y: 70}, gpui.MouseButtonLeft, gpui.Modifiers{})

	if *handle.dragStarts != 1 || *handle.dragEnds != 1 {
		t.Fatalf("drag starts/ends = %d/%d", *handle.dragStarts, *handle.dragEnds)
	}
}

// Go-own: ScrollbarMode keeps its serde names.
func TestScrollbarModeSerdeNames(t *testing.T) {
	data, err := json.Marshal([]ScrollbarMode{ScrollbarModeScrolling, ScrollbarModeHover, ScrollbarModeAlways})
	if err != nil || string(data) != `["Scrolling","Hover","Always"]` {
		t.Fatalf("marshal = %s, %v", data, err)
	}
	var mode ScrollbarMode
	if err := json.Unmarshal([]byte(`"Hover"`), &mode); err != nil || mode != ScrollbarModeHover {
		t.Fatalf("unmarshal = %v, %v", mode, err)
	}
	if err := json.Unmarshal([]byte(`"hover"`), &mode); err == nil {
		t.Fatal("unknown variant accepted")
	}
}

// Go-own: ScrollbarHandleOf adapts the core handles like the pinned impls.
func TestScrollbarHandleOfAdaptsCoreHandles(t *testing.T) {
	scroll := gpui.NewScrollHandle()
	h := ScrollbarHandleOf(scroll)
	h.SetOffset(gpui.Point{Y: -7})
	if scroll.Offset() != (gpui.Point{Y: -7}) {
		t.Fatal("ScrollHandle adapter does not share the offset")
	}
	list := gpui.NewListState(3, 0)
	lh := ScrollbarHandleOf(list)
	ScrollbarHandleStartDrag(lh)
	if !list.IsScrollbarDragging() {
		t.Fatal("ListState adapter did not start the drag")
	}
	ScrollbarHandleEndDrag(lh)
	if list.IsScrollbarDragging() {
		t.Fatal("ListState adapter did not end the drag")
	}
	custom := newSbTestHandle(gpui.Size{})
	if _, ok := ScrollbarHandleOf(custom).(sbTestHandle); !ok {
		t.Fatal("a ScrollbarHandle was rewrapped")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("a non-handle was accepted")
		}
	}()
	ScrollbarHandleOf(42)
}
