package base

// Scrollbar control for a scroll area, a uniform list or a list
// (U:scrollbar.rs).
//
// Go adaptations (recorded deviations):
//
//   - Clock: upstream reads the wall clock (web_time::Instant::now(),
//     U:scrollbar.rs:171, 548, 566, 593, 623, 1301, 1651, 1714, 1783); the
//     port reads the application clock (App.Now), so TestApp.AdvanceClock
//     drives every animation, hold and throttle deterministically. Inner
//     helpers that read Instant::now() themselves take `now`.
//   - ScrollbarHandle is implemented by upstream for the foreign gpui
//     types ScrollHandle, UniformListScrollHandle and ListState
//     (U:scrollbar.rs:83-144); Go cannot add methods to them, so
//     ScrollbarHandleOf wraps them (doc.go's extension-trait rule), and
//     the constructors accept any of them. The trait's default no-op
//     start_drag / end_drag (U:scrollbar.rs:77-80) are an optional
//     interface (ScrollbarDragHandle) called through
//     ScrollbarHandleStartDrag / ScrollbarHandleEndDrag.
//   - ScrollbarMode keeps its serde names (MarshalText / UnmarshalText);
//     the schemars JsonSchema derive (U:scrollbar.rs:46) has no Go
//     counterpart and is dropped.
//   - Element::id is ElementId::CodeLocation(Location::caller())
//     (U:scrollbar.rs:836-838); Go has no column, so the location is
//     "file:line" (the core's own UseState convention).
//   - window.use_state (U:scrollbar.rs:1296-1299) is Window.UseState: the
//     state entity holds the shared cell (the pinned Rc<Cell<..>>).

import (
	"fmt"
	"math"
	"runtime"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

// scrollbarWidthPx is the width of the scrollbar
// (THUMB_ACTIVE_INSET * 2 + THUMB_ACTIVE_WIDTH, U:scrollbar.rs:20-21).
const scrollbarWidthPx float32 = 4*2 + 8

// scrollbarMinThumbSize is U:scrollbar.rs:22 MIN_THUMB_SIZE.
const scrollbarMinThumbSize float32 = 48

// The resting and active thumb geometry (U:scrollbar.rs:24-30).
const (
	scrollbarThumbWidth        float32 = 6
	scrollbarThumbRadius       float32 = 0
	scrollbarThumbInset        float32 = 4
	scrollbarThumbActiveWidth  float32 = 8
	scrollbarThumbActiveRadius float32 = 0
	scrollbarThumbActiveInset  float32 = 4
)

// scrollbarDefaultIdle is how long visibility is held after the last
// activity, when the styled layer projects no ScrollbarMotion of its own
// (U:scrollbar.rs:32-37 DEFAULT_IDLE): a visibility hold rather than
// motion — without it ScrollbarModeScrolling could never reveal the
// scrollbar.
const scrollbarDefaultIdle = 2 * time.Second

// clampThumbRadius limits a radius by the thumb's actual bounds
// (U:scrollbar.rs:39-43).
func clampThumbRadius(radius float32, bounds gpui.Bounds) float32 {
	return min(radius, bounds.Size.Width/2, bounds.Size.Height/2)
}

// secondsF32 is Duration::as_secs_f32.
func secondsF32(d time.Duration) float32 { return float32(d.Seconds()) }

// saturatingSince is Instant::saturating_duration_since over the
// application clock.
func saturatingSince(now, earlier time.Duration) time.Duration {
	if now < earlier {
		return 0
	}
	return now - earlier
}

// ---------------------------------------------------------------------------
// ScrollbarMode (U:scrollbar.rs:45-65)
// ---------------------------------------------------------------------------

// ScrollbarMode is the scrollbar show mode (U:scrollbar.rs:45-55).
type ScrollbarMode uint8

const (
	// ScrollbarModeScrolling shows the scrollbar while scrolling and fades
	// it out after idle (the default).
	ScrollbarModeScrolling ScrollbarMode = iota
	// ScrollbarModeHover shows the scrollbar on hover.
	ScrollbarModeHover
	// ScrollbarModeAlways always shows the scrollbar.
	ScrollbarModeAlways
)

// isHover reports the Hover mode (U:scrollbar.rs:58-60).
func (m ScrollbarMode) isHover() bool { return m == ScrollbarModeHover }

// isAlways reports the Always mode (U:scrollbar.rs:62-64).
func (m ScrollbarMode) isAlways() bool { return m == ScrollbarModeAlways }

// String renders the mode like the derived Debug.
func (m ScrollbarMode) String() string {
	switch m {
	case ScrollbarModeHover:
		return "Hover"
	case ScrollbarModeAlways:
		return "Always"
	default:
		return "Scrolling"
	}
}

// MarshalText encodes the serde variant name (U:scrollbar.rs:46 derive
// Serialize: unit variants as their names).
func (m ScrollbarMode) MarshalText() ([]byte, error) {
	if m > ScrollbarModeAlways {
		return nil, fmt.Errorf("base: invalid ScrollbarMode %d", uint8(m))
	}
	return []byte(m.String()), nil
}

// UnmarshalText decodes the serde variant name (U:scrollbar.rs:46 derive
// Deserialize).
func (m *ScrollbarMode) UnmarshalText(text []byte) error {
	switch string(text) {
	case "Scrolling":
		*m = ScrollbarModeScrolling
	case "Hover":
		*m = ScrollbarModeHover
	case "Always":
		*m = ScrollbarModeAlways
	default:
		return fmt.Errorf("base: unknown variant %q, expected one of `Scrolling`, `Hover`, `Always`", string(text))
	}
	return nil
}

// ---------------------------------------------------------------------------
// ScrollbarHandle (U:scrollbar.rs:67-144)
// ---------------------------------------------------------------------------

// ScrollbarHandle is a scroll handle the scrollbar can read and move
// (U:scrollbar.rs:67-81): the required methods of the pinned trait.
type ScrollbarHandle interface {
	// ViewportBounds returns the bounds of the viewport the scrollbar
	// overlays.
	ViewportBounds() gpui.Bounds
	// Offset returns the current offset of the scroll handle.
	Offset() gpui.Point
	// SetOffset sets the offset of the scroll handle.
	SetOffset(offset gpui.Point)
	// ContentSize returns the full size of the content, including
	// padding.
	ContentSize() gpui.Size
}

// ScrollbarDragHandle is the optional half of the pinned trait: the
// start_drag / end_drag hooks, whose defaults do nothing
// (U:scrollbar.rs:77-80).
type ScrollbarDragHandle interface {
	// StartDrag is called when dragging the thumb starts.
	StartDrag()
	// EndDrag is called when dragging the thumb ends.
	EndDrag()
}

// ScrollbarHandleStartDrag calls the handle's StartDrag when it has one
// (the pinned default is a no-op, U:scrollbar.rs:77-78).
func ScrollbarHandleStartDrag(h ScrollbarHandle) {
	if d, ok := h.(ScrollbarDragHandle); ok {
		d.StartDrag()
	}
}

// ScrollbarHandleEndDrag calls the handle's EndDrag when it has one
// (U:scrollbar.rs:79-80).
func ScrollbarHandleEndDrag(h ScrollbarHandle) {
	if d, ok := h.(ScrollbarDragHandle); ok {
		d.EndDrag()
	}
}

// ScrollbarHandleOf returns the ScrollbarHandle of a scroll handle: a
// gpui.ScrollHandle (U:scrollbar.rs:83-99), a *gpui.UniformListScrollHandle
// (U:scrollbar.rs:101-118), a *gpui.ListState (U:scrollbar.rs:120-144) or
// any ScrollbarHandle (returned as is). Any other value panics.
func ScrollbarHandleOf[H any](handle H) ScrollbarHandle {
	switch h := any(handle).(type) {
	case gpui.ScrollHandle:
		return scrollHandleScrollbar{h}
	case *gpui.ScrollHandle:
		return scrollHandleScrollbar{*h}
	case *gpui.UniformListScrollHandle:
		return uniformListScrollbar{h}
	case *gpui.ListState:
		return listStateScrollbar{h}
	case ScrollbarHandle:
		return h
	}
	panic(fmt.Sprintf("base: %T is not a scrollbar handle (want gpui.ScrollHandle, *gpui.UniformListScrollHandle, *gpui.ListState or a ScrollbarHandle)", handle))
}

// scrollHandleScrollbar is `impl ScrollbarHandle for ScrollHandle`
// (U:scrollbar.rs:83-99).
type scrollHandleScrollbar struct{ h gpui.ScrollHandle }

func (s scrollHandleScrollbar) ViewportBounds() gpui.Bounds { return s.h.Bounds() }
func (s scrollHandleScrollbar) Offset() gpui.Point          { return s.h.Offset() }
func (s scrollHandleScrollbar) SetOffset(offset gpui.Point) { s.h.SetOffset(offset) }
func (s scrollHandleScrollbar) ContentSize() gpui.Size      { return scrollHandleContentSize(s.h) }
func scrollHandleContentSize(h gpui.ScrollHandle) gpui.Size { // (max_offset + bounds.size)
	max, size := h.MaxOffset(), h.Bounds().Size
	return gpui.Size{Width: max.X + size.Width, Height: max.Y + size.Height}
}

// uniformListScrollbar is `impl ScrollbarHandle for
// UniformListScrollHandle` over its base handle (U:scrollbar.rs:101-118).
type uniformListScrollbar struct{ h *gpui.UniformListScrollHandle }

func (s uniformListScrollbar) ViewportBounds() gpui.Bounds { return s.h.BaseHandle().Bounds() }
func (s uniformListScrollbar) Offset() gpui.Point          { return s.h.BaseHandle().Offset() }
func (s uniformListScrollbar) SetOffset(offset gpui.Point) { s.h.BaseHandle().SetOffset(offset) }
func (s uniformListScrollbar) ContentSize() gpui.Size {
	return scrollHandleContentSize(s.h.BaseHandle())
}

// listStateScrollbar is `impl ScrollbarHandle for ListState`
// (U:scrollbar.rs:120-144).
type listStateScrollbar struct{ s *gpui.ListState }

func (l listStateScrollbar) ViewportBounds() gpui.Bounds { return l.s.ViewportBounds() }
func (l listStateScrollbar) Offset() gpui.Point          { return l.s.ScrollPxOffsetForScrollbar() }
func (l listStateScrollbar) SetOffset(offset gpui.Point) { l.s.SetOffsetFromScrollbar(offset) }
func (l listStateScrollbar) ContentSize() gpui.Size {
	size, max := l.s.ViewportBounds().Size, l.s.MaxOffsetForScrollbar()
	return gpui.Size{Width: size.Width + max.X, Height: size.Height + max.Y}
}
func (l listStateScrollbar) StartDrag() { l.s.ScrollbarDragStarted() }
func (l listStateScrollbar) EndDrag()   { l.s.ScrollbarDragEnded() }

// ---------------------------------------------------------------------------
// Scrollbar state (U:scrollbar.rs:146-188, 507-625)
// ---------------------------------------------------------------------------

// optAxis is the pinned Option<Axis>.
type optAxis = gpui.Optional[gpui.Axis]

// axisIs is `opt == Some(axis)`.
func axisIs(opt optAxis, axis gpui.Axis) bool {
	v, ok := opt.Get()
	return ok && v == axis
}

// scrollbarState is the shared state cell (U:scrollbar.rs:146-148
// ScrollbarState(Rc<Cell<ScrollbarStateInner>>)): copies of the pointer
// share it.
type scrollbarState struct {
	inner scrollbarStateInner
}

func (s *scrollbarState) get() scrollbarStateInner  { return s.inner }
func (s *scrollbarState) set(v scrollbarStateInner) { s.inner = v }

// scrollbarStateInner is U:scrollbar.rs:150-167.
type scrollbarStateInner struct {
	hoveredAxis      optAxis
	hoveredOnThumb   optAxis
	draggedAxis      optAxis
	dragPos          gpui.Point
	lastScrollOffset gpui.Point
	lastScrollTime   gpui.Optional[time.Duration]
	// lastUpdate is the last drag update.
	lastUpdate           time.Duration
	dragUpdateScheduled  bool
	dragUpdateGeneration uint64
	idleTimerScheduled   bool
	visibility           visibilityAnimation
	verticalWidth        widthAnimation
	horizontalWidth      widthAnimation
}

// newScrollbarState is `ScrollbarState::default()` read at now
// (U:scrollbar.rs:169-188).
func newScrollbarState(now time.Duration) *scrollbarState {
	return &scrollbarState{inner: scrollbarStateInner{
		lastUpdate:      now,
		visibility:      hiddenVisibilityAnimation(now),
		verticalWidth:   newWidthAnimation(now),
		horizontalWidth: newWidthAnimation(now),
	}}
}

// scalarTransition is U:scrollbar.rs:190-234 ScalarTransition<T> (both
// pinned instantiations, Pixels and f32, are float32 here).
type scalarTransition struct {
	from      float32
	target    float32
	startedAt time.Duration
	duration  time.Duration
}

func settledTransition(value float32, now time.Duration) scalarTransition {
	return scalarTransition{from: value, target: value, startedAt: now}
}

func (t scalarTransition) sample(now time.Duration, interpolate func(from, target, linear float32) float32) (float32, bool) {
	if t.from == t.target || t.duration == 0 {
		return t.target, false
	}
	linear := secondsF32(saturatingSince(now, t.startedAt)) / secondsF32(t.duration)
	if linear >= 1 {
		return t.target, false
	}
	return interpolate(t.from, t.target, clamp32(linear, 0, 1)), true
}

func (t *scalarTransition) start(from, target float32, duration, now time.Duration) {
	t.from, t.target, t.startedAt, t.duration = from, target, now, duration
}

func (t *scalarTransition) settle(target float32, now time.Duration) {
	t.start(target, target, 0, now)
}

// widthAnimation is U:scrollbar.rs:236-268.
type widthAnimation struct {
	transition  scalarTransition
	initialized bool
}

func newWidthAnimation(now time.Duration) widthAnimation {
	return widthAnimation{transition: settledTransition(0, now)}
}

func (a widthAnimation) sample(now time.Duration) (float32, bool) {
	return a.transition.sample(now, func(from, target, linear float32) float32 {
		return from + (target-from)*EaseOutCubic(linear)
	})
}

// setTarget moves toward target over duration. A zero duration adopts the
// target immediately, which is how reduced motion and a motionless theme
// arrive here (U:scrollbar.rs:256-267).
func (a *widthAnimation) setTarget(target float32, duration, now time.Duration) (float32, bool) {
	if duration == 0 || !a.initialized {
		a.transition.settle(target, now)
		a.initialized = true
	} else if a.transition.target != target {
		from, _ := a.sample(now)
		a.transition.start(from, target, duration, now)
	}
	return a.sample(now)
}

// ScrollbarEntrance is how a scrollbar becomes visible; the styled layer
// chooses the choreography and Base only plays it (U:scrollbar.rs:270-280).
type ScrollbarEntrance uint8

const (
	// ScrollbarEntranceFade fades in without moving (the default).
	ScrollbarEntranceFade ScrollbarEntrance = iota
	// ScrollbarEntranceSlideAndFade slides in from the nearest edge while
	// fading.
	ScrollbarEntranceSlideAndFade
)

// ScrollbarMotion holds the motion tokens used by the scrollbar
// (U:scrollbar.rs:282-295). Base installs no motion of its own: every
// transition duration defaults to zero, so visibility and thumb width
// snap. Product timing belongs to the styled layer, which projects it
// through ScrollbarTheme.
type ScrollbarMotion struct {
	idle               time.Duration
	enter              time.Duration
	exit               time.Duration
	expand             time.Duration
	entrance           ScrollbarEntrance
	thumbHoverEntrance ScrollbarEntrance
}

// DefaultScrollbarMotion returns the motionless defaults
// (U:scrollbar.rs:297-308).
func DefaultScrollbarMotion() ScrollbarMotion {
	return ScrollbarMotion{idle: scrollbarDefaultIdle}
}

// WithIdle sets how long visibility is held after the last scroll, drag
// or hover (U:scrollbar.rs:311-315).
func (m ScrollbarMotion) WithIdle(idle time.Duration) ScrollbarMotion { m.idle = idle; return m }

// WithEnter sets how long the scrollbar takes to become fully visible
// (U:scrollbar.rs:317-321).
func (m ScrollbarMotion) WithEnter(enter time.Duration) ScrollbarMotion { m.enter = enter; return m }

// WithExit sets how long the scrollbar takes to fade away once the idle
// hold expires (U:scrollbar.rs:323-327).
func (m ScrollbarMotion) WithExit(exit time.Duration) ScrollbarMotion { m.exit = exit; return m }

// WithExpand sets how long the thumb takes to reach a new width
// (U:scrollbar.rs:329-333).
func (m ScrollbarMotion) WithExpand(expand time.Duration) ScrollbarMotion {
	m.expand = expand
	return m
}

// WithEntrance sets the entrance choreography (U:scrollbar.rs:335-339).
func (m ScrollbarMotion) WithEntrance(entrance ScrollbarEntrance) ScrollbarMotion {
	m.entrance = entrance
	return m
}

// WithThumbHoverEntrance sets the entrance played when hover reveals the
// thumb (U:scrollbar.rs:341-345).
func (m ScrollbarMotion) WithThumbHoverEntrance(entrance ScrollbarEntrance) ScrollbarMotion {
	m.thumbHoverEntrance = entrance
	return m
}

// Idle returns the visibility hold (U:scrollbar.rs:347).
func (m ScrollbarMotion) Idle() time.Duration { return m.idle }

// Enter returns the enter duration (U:scrollbar.rs:351).
func (m ScrollbarMotion) Enter() time.Duration { return m.enter }

// Exit returns the exit duration (U:scrollbar.rs:355).
func (m ScrollbarMotion) Exit() time.Duration { return m.exit }

// Expand returns the thumb expand duration (U:scrollbar.rs:359).
func (m ScrollbarMotion) Expand() time.Duration { return m.expand }

// Entrance returns the entrance choreography (U:scrollbar.rs:363).
func (m ScrollbarMotion) Entrance() ScrollbarEntrance { return m.entrance }

// ThumbHoverEntrance returns the thumb-hover entrance (U:scrollbar.rs:367).
func (m ScrollbarMotion) ThumbHoverEntrance() ScrollbarEntrance { return m.thumbHoverEntrance }

// entranceFor picks the entrance for the mode (U:scrollbar.rs:371-377).
func (m ScrollbarMotion) entranceFor(mode ScrollbarMode, thumbHovered bool) ScrollbarEntrance {
	if mode.isHover() && thumbHovered {
		return m.thumbHoverEntrance
	}
	return m.entrance
}

// visibilityAnimation is U:scrollbar.rs:380-385.
type visibilityAnimation struct {
	opacity  scalarTransition
	position scalarTransition
	entrance ScrollbarEntrance
}

// visibilitySample is U:scrollbar.rs:387-392.
type visibilitySample struct {
	opacity  float32
	position float32
	running  bool
}

func hiddenVisibilityAnimation(now time.Duration) visibilityAnimation {
	return visibilityAnimation{
		opacity:  settledTransition(0, now),
		position: settledTransition(0, now),
		entrance: ScrollbarEntranceFade,
	}
}

// sample is U:scrollbar.rs:403-428: entering fades linearly while the
// position eases out; leaving eases both in.
func (a visibilityAnimation) sample(now time.Duration) visibilitySample {
	entering := a.opacity.target > a.opacity.from || a.position.target > a.position.from
	opacity, opacityRunning := a.opacity.sample(now, func(from, target, linear float32) float32 {
		factor := EaseInCubic(linear)
		if entering {
			factor = linear
		}
		return from + (target-from)*factor
	})
	position, positionRunning := a.position.sample(now, func(from, target, linear float32) float32 {
		factor := EaseInCubic(linear)
		if entering {
			factor = EaseOutCubic(linear)
		}
		return from + (target-from)*factor
	})
	return visibilitySample{opacity: opacity, position: position, running: opacityRunning || positionRunning}
}

// setVisible reverses or starts a transition toward visible
// (U:scrollbar.rs:430-473). The leg runs for enter or exit scaled by the
// distance still to cover, so an interrupted transition keeps its speed
// instead of restarting.
func (a *visibilityAnimation) setVisible(visible bool, entrance ScrollbarEntrance, enter, exit, now time.Duration) {
	target, fullDuration := float32(0), exit
	if visible {
		target, fullDuration = 1, enter
	}
	if fullDuration == 0 {
		// A motionless policy — reduced motion, an always-visible
		// scrollbar, or a theme that projects no motion — adopts the
		// target outright, even mid-transition.
		a.opacity.settle(target, now)
		a.position.settle(target, now)
		a.entrance = entrance
		return
	}
	if a.opacity.target == target && a.position.target == target && a.entrance == entrance {
		return
	}

	sample := a.sample(now)
	fromPosition := sample.position
	if visible && entrance == ScrollbarEntranceFade {
		fromPosition = 1
	}
	distance := max(abs32(target-sample.opacity), abs32(target-fromPosition))
	// Duration::mul_f32 (secs_f32 * rhs, rounded to nanoseconds).
	duration := time.Duration(math.Round(float64(secondsF32(fullDuration)*distance) * 1e9))
	a.opacity.start(sample.opacity, target, duration, now)
	a.position.start(fromPosition, target, duration, now)
	a.entrance = entrance
}

// visibilityTranslation slides the bar toward its nearest edge
// (U:scrollbar.rs:476-483).
func visibilityTranslation(axis gpui.Axis, trackWidth, progress float32) gpui.Point {
	offset := trackWidth * (1 - clamp32(progress, 0, 1))
	if axis == gpui.AxisVertical {
		return gpui.Point{X: offset}
	}
	return gpui.Point{Y: offset}
}

// wantsVisible is U:scrollbar.rs:485-497.
func wantsVisible(mode ScrollbarMode, isHovered, isDragging bool, lastScrollTime gpui.Optional[time.Duration], idle, now time.Duration) bool {
	last, ok := lastScrollTime.Get()
	return mode.isAlways() || isDragging || (mode.isHover() && isHovered) ||
		(ok && saturatingSince(now, last) < idle)
}

// tracksThumbHover is U:scrollbar.rs:499-501.
func tracksThumbHover(mode ScrollbarMode, isVisible bool) bool {
	return mode.isHover() || isVisible
}

// hoverKeepsVisible is U:scrollbar.rs:503-505.
func hoverKeepsVisible(mode ScrollbarMode, isHovered, isCurrentlyVisible bool) bool {
	return isHovered && (mode.isHover() || (mode == ScrollbarModeScrolling && isCurrentlyVisible))
}

// notifyDrag coalesces drag re-renders to the max-fps interval with one
// trailing notification that sees the newest offset
// (U:scrollbar.rs:515-554). The timer is a window-scoped task (the pinned
// window.spawn): closing the window cancels it.
func (s *scrollbarState) notifyDrag(now, interval time.Duration, view gpui.EntityID, w *gpui.Window, app *gpui.App) {
	inner := s.get()
	if inner.dragUpdateScheduled {
		return
	}
	elapsed := saturatingSince(now, inner.lastUpdate)
	if elapsed >= interval {
		s.set(inner.withLastUpdate(now))
		app.Notify(view)
		return
	}
	inner.dragUpdateScheduled = true
	s.set(inner)
	generation := inner.dragUpdateGeneration
	delay := interval - elapsed
	app.SpawnDelivering(w.Scope(), func(run *gpui.TaskRun) struct{} {
		run.Sleep(delay)
		return struct{}{}
	}, func(_ struct{}, app *gpui.App) {
		inner := s.get()
		// A release or new drag supersedes the pending trailing update.
		if !inner.dragUpdateScheduled || inner.dragUpdateGeneration != generation {
			return
		}
		inner.dragUpdateScheduled = false
		inner.lastUpdate = app.Now()
		s.set(inner)
		app.Notify(view)
	})
}

// withDragPos is U:scrollbar.rs:557-570.
func (st scrollbarStateInner) withDragPos(axis gpui.Axis, pos gpui.Point, now time.Duration) scrollbarStateInner {
	if axis == gpui.AxisVertical {
		st.dragPos.Y = pos.Y
	} else {
		st.dragPos.X = pos.X
	}
	st.draggedAxis = gpui.Some(axis)
	st.lastUpdate = now
	st.dragUpdateScheduled = false
	st.dragUpdateGeneration++
	return st
}

// withUnsetDragPos is U:scrollbar.rs:572-579.
func (st scrollbarStateInner) withUnsetDragPos(now time.Duration) scrollbarStateInner {
	st.draggedAxis = optAxis{}
	st.dragUpdateScheduled = false
	st.dragUpdateGeneration++
	st.lastScrollTime = gpui.Some(now)
	return st
}

// withHovered is U:scrollbar.rs:581-586.
func (st scrollbarStateInner) withHovered(axis optAxis, now time.Duration) scrollbarStateInner {
	st.hoveredAxis = axis
	st.lastScrollTime = gpui.Some(now)
	return st
}

// withHoveredOnThumb is U:scrollbar.rs:588-597.
func (st scrollbarStateInner) withHoveredOnThumb(axis optAxis, now time.Duration) scrollbarStateInner {
	visible := st.isScrollbarVisible(now)
	st.hoveredOnThumb = axis
	if visible && axis.IsSome() {
		st.lastScrollTime = gpui.Some(now)
	}
	return st
}

// withLastScroll is U:scrollbar.rs:599-608.
func (st scrollbarStateInner) withLastScroll(offset gpui.Point, at gpui.Optional[time.Duration]) scrollbarStateInner {
	st.lastScrollOffset = offset
	st.lastScrollTime = at
	return st
}

// withLastUpdate is U:scrollbar.rs:610-614.
func (st scrollbarStateInner) withLastUpdate(t time.Duration) scrollbarStateInner {
	st.lastUpdate = t
	return st
}

// withIdleTimerScheduled is U:scrollbar.rs:616-620.
func (st scrollbarStateInner) withIdleTimerScheduled(scheduled bool) scrollbarStateInner {
	st.idleTimerScheduled = scheduled
	return st
}

// isScrollbarVisible is U:scrollbar.rs:622-624.
func (st scrollbarStateInner) isScrollbarVisible(now time.Duration) bool {
	return st.draggedAxis.IsSome() || st.visibility.sample(now).opacity > 0
}

// ---------------------------------------------------------------------------
// ScrollbarAxis (U:scrollbar.rs:627-636, 761-811)
// ---------------------------------------------------------------------------

// ScrollbarAxis is the scrollbar axis (U:scrollbar.rs:627-636).
type ScrollbarAxis uint8

const (
	// ScrollbarAxisVertical is a vertical scrollbar.
	ScrollbarAxisVertical ScrollbarAxis = iota
	// ScrollbarAxisHorizontal is a horizontal scrollbar.
	ScrollbarAxisHorizontal
	// ScrollbarAxisBoth shows both vertical and horizontal scrollbars.
	ScrollbarAxisBoth
)

// ScrollbarAxisFrom converts a layout axis (U:scrollbar.rs:761-768
// From<Axis>).
func ScrollbarAxisFrom(axis gpui.Axis) ScrollbarAxis {
	if axis == gpui.AxisHorizontal {
		return ScrollbarAxisHorizontal
	}
	return ScrollbarAxisVertical
}

// IsVertical reports the vertical axis (U:scrollbar.rs:771-775).
func (a ScrollbarAxis) IsVertical() bool { return a == ScrollbarAxisVertical }

// IsHorizontal reports the horizontal axis (U:scrollbar.rs:777-781).
func (a ScrollbarAxis) IsHorizontal() bool { return a == ScrollbarAxisHorizontal }

// IsBoth reports both axes (U:scrollbar.rs:783-787).
func (a ScrollbarAxis) IsBoth() bool { return a == ScrollbarAxisBoth }

// HasVertical reports a vertical bar (U:scrollbar.rs:789-793).
func (a ScrollbarAxis) HasVertical() bool {
	return a == ScrollbarAxisVertical || a == ScrollbarAxisBoth
}

// HasHorizontal reports a horizontal bar (U:scrollbar.rs:795-799).
func (a ScrollbarAxis) HasHorizontal() bool {
	return a == ScrollbarAxisHorizontal || a == ScrollbarAxisBoth
}

// all lists the axes, Horizontal first for Both: Vertical is the primary
// axis, and when it needs no bar the horizontal one keeps no right margin
// (U:scrollbar.rs:801-810).
func (a ScrollbarAxis) all() []gpui.Axis {
	switch a {
	case ScrollbarAxisVertical:
		return []gpui.Axis{gpui.AxisVertical}
	case ScrollbarAxisHorizontal:
		return []gpui.Axis{gpui.AxisHorizontal}
	}
	return []gpui.Axis{gpui.AxisHorizontal, gpui.AxisVertical}
}

// ---------------------------------------------------------------------------
// Paint-only styles (U:scrollbar.rs:638-759)
// ---------------------------------------------------------------------------

// ScrollbarTrackStyle holds the paint-only styles of a scrollbar track
// (U:scrollbar.rs:638-661).
type ScrollbarTrackStyle struct {
	background gpui.Optional[gpui.Hsla]
	border     gpui.Optional[gpui.Hsla]
	width      gpui.Optional[float32]
}

// Bg sets the track background (U:scrollbar.rs:647).
func (s ScrollbarTrackStyle) Bg(background gpui.Hsla) ScrollbarTrackStyle {
	s.background = gpui.Some(background)
	return s
}

// BorderColor sets the track border color (U:scrollbar.rs:652).
func (s ScrollbarTrackStyle) BorderColor(border gpui.Hsla) ScrollbarTrackStyle {
	s.border = gpui.Some(border)
	return s
}

// Width sets the track width in logical pixels (U:scrollbar.rs:657).
func (s ScrollbarTrackStyle) Width(width float32) ScrollbarTrackStyle {
	s.width = gpui.Some(width)
	return s
}

// When applies then when condition holds (U:scrollbar.rs:663 impl
// FluentBuilder; R:util.rs:21).
func (s ScrollbarTrackStyle) When(condition bool, then func(ScrollbarTrackStyle) ScrollbarTrackStyle) ScrollbarTrackStyle {
	if condition {
		return then(s)
	}
	return s
}

// ScrollbarThumbStyle holds the paint-only styles of a scrollbar thumb
// (U:scrollbar.rs:665-700).
type ScrollbarThumbStyle struct {
	background gpui.Optional[gpui.Background]
	width      gpui.Optional[float32]
	inset      gpui.Optional[float32]
	radius     gpui.Optional[float32]
	minLength  gpui.Optional[float32]
}

// Bg sets the thumb background (U:scrollbar.rs:676).
func (s ScrollbarThumbStyle) Bg[B gpui.IntoBackground](background B) ScrollbarThumbStyle {
	s.background = gpui.Some(gpui.BackgroundOf(background))
	return s
}

// Width sets the thumb width (U:scrollbar.rs:681).
func (s ScrollbarThumbStyle) Width(width float32) ScrollbarThumbStyle {
	s.width = gpui.Some(width)
	return s
}

// Inset sets the thumb inset (U:scrollbar.rs:686).
func (s ScrollbarThumbStyle) Inset(inset float32) ScrollbarThumbStyle {
	s.inset = gpui.Some(inset)
	return s
}

// Radius sets the thumb corner radius (U:scrollbar.rs:691).
func (s ScrollbarThumbStyle) Radius(radius float32) ScrollbarThumbStyle {
	s.radius = gpui.Some(radius)
	return s
}

// MinLength sets the thumb's minimum length (U:scrollbar.rs:696).
func (s ScrollbarThumbStyle) MinLength(minLength float32) ScrollbarThumbStyle {
	s.minLength = gpui.Some(minLength)
	return s
}

// When applies then when condition holds (U:scrollbar.rs:702 impl
// FluentBuilder; R:util.rs:21).
func (s ScrollbarThumbStyle) When(condition bool, then func(ScrollbarThumbStyle) ScrollbarThumbStyle) ScrollbarThumbStyle {
	if condition {
		return then(s)
	}
	return s
}

// ScrollbarStyles are the typed paint styles a scrollbar supports
// (U:scrollbar.rs:704-757).
type ScrollbarStyles struct {
	track       ScrollbarTrackStyle
	trackHover  ScrollbarTrackStyle
	trackActive ScrollbarTrackStyle
	thumb       ScrollbarThumbStyle
	thumbHover  ScrollbarThumbStyle
	thumbActive ScrollbarThumbStyle
}

// Track edits the resting track style (U:scrollbar.rs:716).
func (s ScrollbarStyles) Track(build func(ScrollbarTrackStyle) ScrollbarTrackStyle) ScrollbarStyles {
	s.track = build(s.track)
	return s
}

// TrackHover edits the hovered track style (U:scrollbar.rs:721).
func (s ScrollbarStyles) TrackHover(build func(ScrollbarTrackStyle) ScrollbarTrackStyle) ScrollbarStyles {
	s.trackHover = build(s.trackHover)
	return s
}

// TrackActive edits the active track style (U:scrollbar.rs:729).
func (s ScrollbarStyles) TrackActive(build func(ScrollbarTrackStyle) ScrollbarTrackStyle) ScrollbarStyles {
	s.trackActive = build(s.trackActive)
	return s
}

// Thumb edits the resting thumb style (U:scrollbar.rs:737).
func (s ScrollbarStyles) Thumb(build func(ScrollbarThumbStyle) ScrollbarThumbStyle) ScrollbarStyles {
	s.thumb = build(s.thumb)
	return s
}

// ThumbHover edits the hovered thumb style (U:scrollbar.rs:742).
func (s ScrollbarStyles) ThumbHover(build func(ScrollbarThumbStyle) ScrollbarThumbStyle) ScrollbarStyles {
	s.thumbHover = build(s.thumbHover)
	return s
}

// ThumbActive edits the active thumb style (U:scrollbar.rs:750).
func (s ScrollbarStyles) ThumbActive(build func(ScrollbarThumbStyle) ScrollbarThumbStyle) ScrollbarStyles {
	s.thumbActive = build(s.thumbActive)
	return s
}

// When applies then when condition holds (U:scrollbar.rs:759 impl
// FluentBuilder; R:util.rs:21).
func (s ScrollbarStyles) When(condition bool, then func(ScrollbarStyles) ScrollbarStyles) ScrollbarStyles {
	if condition {
		return then(s)
	}
	return s
}

// ---------------------------------------------------------------------------
// Scrollbar (U:scrollbar.rs:813-1154)
// ---------------------------------------------------------------------------

// Scrollbar is the scrollbar control for a scroll area or a list
// (U:scrollbar.rs:813-828).
type Scrollbar struct {
	id              gpui.ElementID
	axis            ScrollbarAxis
	mode            gpui.Optional[ScrollbarMode]
	scrollHandle    ScrollbarHandle
	scrollSize      gpui.Optional[gpui.Size]
	viewportBounds  gpui.Optional[gpui.Bounds]
	useLayoutBounds bool
	// maxFps is the maximum frames per second for scrolling by drag
	// (default 120), limiting the update rate of complex views.
	maxFps int
	styles ScrollbarStyles
}

// callerElementID is ElementId::CodeLocation(*Location::caller())
// (#[track_caller], U:scrollbar.rs:834-838), skip frames above the
// caller of this helper.
func callerElementID(skip int) gpui.ElementID {
	_, file, line, ok := runtime.Caller(skip + 1)
	location := "<unknown>"
	if ok {
		location = fmt.Sprintf("%s:%d", file, line)
	}
	return gpui.ElementID{Kind: gpui.ElementIDCodeLocation, Name: location}
}

// NewScrollbar creates a scrollbar with both vertical and horizontal bars
// over handle (any value ScrollbarHandleOf accepts); its id is the call
// site (U:scrollbar.rs:830-848 Scrollbar::new).
func NewScrollbar[H any](handle H) *Scrollbar {
	return newScrollbarAt(callerElementID(2), handle)
}

func newScrollbarAt[H any](id gpui.ElementID, handle H) *Scrollbar {
	return &Scrollbar{
		id:           id,
		axis:         ScrollbarAxisBoth,
		scrollHandle: ScrollbarHandleOf(handle),
		maxFps:       120,
	}
}

// ScrollbarHorizontal creates a horizontal scrollbar
// (U:scrollbar.rs:850-854).
func ScrollbarHorizontal[H any](handle H) *Scrollbar {
	return newScrollbarAt(callerElementID(2), handle).Axis(ScrollbarAxisHorizontal)
}

// ScrollbarVertical creates a vertical scrollbar (U:scrollbar.rs:856-860).
func ScrollbarVertical[H any](handle H) *Scrollbar {
	return newScrollbarAt(callerElementID(2), handle).Axis(ScrollbarAxisVertical)
}

// ID sets a specific element id instead of the call site
// (U:scrollbar.rs:862-868). Rarely needed.
func (s *Scrollbar) ID[I gpui.IntoElementID](id I) *Scrollbar {
	s.id = gpui.ElementIDOf(id)
	return s
}

// Mode sets the show mode; unset, the active theme's projection is used
// (U:scrollbar.rs:870-876).
func (s *Scrollbar) Mode(mode ScrollbarMode) *Scrollbar {
	s.mode = gpui.Some(mode)
	return s
}

// ScrollSize sets a special scroll size of the content area; by default
// the handle's content size is used (U:scrollbar.rs:878-884).
func (s *Scrollbar) ScrollSize(scrollSize gpui.Size) *Scrollbar {
	s.scrollSize = gpui.Some(scrollSize)
	return s
}

// ViewportBounds overrides the viewport bounds this scrollbar overlays,
// for custom-painted viewports whose visible bounds differ from the
// handle's layout bounds (U:scrollbar.rs:886-895).
func (s *Scrollbar) ViewportBounds(bounds gpui.Bounds) *Scrollbar {
	s.viewportBounds = gpui.Some(bounds)
	return s
}

// ViewportFromLayout uses the scrollbar element's own layout bounds as
// its viewport (U:scrollbar.rs:897-905).
func (s *Scrollbar) ViewportFromLayout() *Scrollbar {
	s.useLayoutBounds = true
	return s
}

// resolvedViewportBounds is U:scrollbar.rs:907-915.
func (s *Scrollbar) resolvedViewportBounds(layoutBounds gpui.Bounds) gpui.Bounds {
	if b, ok := s.viewportBounds.Get(); ok {
		return b
	}
	if s.useLayoutBounds {
		return layoutBounds
	}
	return s.scrollHandle.ViewportBounds()
}

// Axis sets the scrollbar axis from a ScrollbarAxis or a gpui.Axis
// (U:scrollbar.rs:917-921, `impl Into<ScrollbarAxis>`).
func (s *Scrollbar) Axis[A ScrollbarAxis | gpui.Axis](axis A) *Scrollbar {
	switch a := any(axis).(type) {
	case ScrollbarAxis:
		s.axis = a
	case gpui.Axis:
		s.axis = ScrollbarAxisFrom(a)
	}
	return s
}

// Styles edits the instance styles (U:scrollbar.rs:923-926).
func (s *Scrollbar) Styles(build func(ScrollbarStyles) ScrollbarStyles) *Scrollbar {
	s.styles = build(s.styles)
	return s
}

// MaxFps sets the maximum frames per second for scrolling by drag,
// clamped to 30..120 (U:scrollbar.rs:928-937).
func (s *Scrollbar) MaxFps(maxFps int) *Scrollbar {
	s.maxFps = min(max(maxFps, 30), 120)
	return s
}

// ScrollbarWidth returns the width of the scrollbar
// (U:scrollbar.rs:939-943 Scrollbar::width).
func ScrollbarWidth() float32 { return scrollbarWidthPx }

// scrollbarResolvedStyle is the pinned style tuple (Background, Hsla,
// Hsla, Pixels, Pixels, Pixels, Pixels): thumb, track, border, thumb
// width, inset, radius and minimum length.
type scrollbarResolvedStyle struct {
	thumb     gpui.Background
	track     gpui.Hsla
	border    gpui.Hsla
	width     float32
	inset     float32
	radius    float32
	minLength float32
}

// firstSome is the pinned `.or(..).or(..)` chain.
func firstSome[T any](options ...gpui.Optional[T]) gpui.Optional[T] {
	for _, o := range options {
		if o.IsSome() {
			return o
		}
	}
	return gpui.Optional[T]{}
}

// resolveTrack is U:scrollbar.rs:945-968: instance state > instance
// resting > theme state > theme resting > default.
func (s *Scrollbar) resolveTrack(global *ScrollbarStyles, state, globalState *ScrollbarTrackStyle, defaultBorder gpui.Hsla) (gpui.Hsla, gpui.Hsla) {
	return firstSome(state.background, s.styles.track.background, globalState.background, global.track.background).UnwrapOr(gpui.TransparentBlack()),
		firstSome(state.border, s.styles.track.border, globalState.border, global.track.border).UnwrapOr(defaultBorder)
}

// resolveThumb is U:scrollbar.rs:970-1012.
func (s *Scrollbar) resolveThumb(global *ScrollbarStyles, state, globalState *ScrollbarThumbStyle, defaults ScrollbarThumbStyle) scrollbarResolvedStyle {
	defaultBg, _ := defaults.background.Get()
	defaultWidth, _ := defaults.width.Get()
	defaultInset, _ := defaults.inset.Get()
	defaultRadius, _ := defaults.radius.Get()
	return scrollbarResolvedStyle{
		thumb:     firstSome(state.background, s.styles.thumb.background, globalState.background, global.thumb.background).UnwrapOr(defaultBg),
		width:     firstSome(state.width, s.styles.thumb.width, globalState.width, global.thumb.width).UnwrapOr(defaultWidth),
		inset:     firstSome(state.inset, s.styles.thumb.inset, globalState.inset, global.thumb.inset).UnwrapOr(defaultInset),
		radius:    firstSome(state.radius, s.styles.thumb.radius, globalState.radius, global.thumb.radius).UnwrapOr(defaultRadius),
		minLength: firstSome(state.minLength, s.styles.thumb.minLength, globalState.minLength, global.thumb.minLength, defaults.minLength).UnwrapOr(scrollbarMinThumbSize),
	}
}

// thumbDefaultBackground is the theme-derived thumb colour at the bottom
// of the cascade: the foreground token ("ink on this surface") at the
// given alpha, so it flips with the appearance (U:scrollbar.rs:1014-1033).
func thumbDefaultBackground(theme *Theme, alpha float32) gpui.Background {
	color := theme.Tokens.Colors.Foreground
	color.A = clamp32(alpha, 0, 1)
	return gpui.SolidBackground(color)
}

// thumbDefaults is U:scrollbar.rs:1035-1048.
func thumbDefaults(background gpui.Background, width, inset, radius float32) ScrollbarThumbStyle {
	return ScrollbarThumbStyle{
		background: gpui.Some(background),
		width:      gpui.Some(width),
		inset:      gpui.Some(inset),
		radius:     gpui.Some(radius),
		minLength:  gpui.Some(scrollbarMinThumbSize),
	}
}

// resolveStyle is the shared body of the four style_for_* methods: the
// track from the given track states, the thumb from the given thumb
// states over the given defaults.
func (s *Scrollbar) resolveStyle(app *gpui.App, track func(*ScrollbarStyles) *ScrollbarTrackStyle, thumb func(*ScrollbarStyles) *ScrollbarThumbStyle, alpha, width, inset, radius float32) scrollbarResolvedStyle {
	theme := activeTheme(app)
	global := theme.Scrollbar.Styles()
	trackBg, border := s.resolveTrack(global, track(&s.styles), track(global), gpui.TransparentBlack())
	style := s.resolveThumb(global, thumb(&s.styles), thumb(global), thumbDefaults(thumbDefaultBackground(&theme, alpha), width, inset, radius))
	style.track, style.border = trackBg, border
	return style
}

// styleForActive is U:scrollbar.rs:1050-1074.
func (s *Scrollbar) styleForActive(app *gpui.App) scrollbarResolvedStyle {
	return s.resolveStyle(app,
		func(st *ScrollbarStyles) *ScrollbarTrackStyle { return &st.trackActive },
		func(st *ScrollbarStyles) *ScrollbarThumbStyle { return &st.thumbActive },
		0.55, scrollbarThumbActiveWidth, scrollbarThumbActiveInset, scrollbarThumbActiveRadius)
}

// styleForHoveredThumb is U:scrollbar.rs:1076-1100.
func (s *Scrollbar) styleForHoveredThumb(app *gpui.App) scrollbarResolvedStyle {
	return s.resolveStyle(app,
		func(st *ScrollbarStyles) *ScrollbarTrackStyle { return &st.trackActive },
		func(st *ScrollbarStyles) *ScrollbarThumbStyle { return &st.thumbHover },
		0.55, scrollbarThumbActiveWidth, scrollbarThumbActiveInset, scrollbarThumbActiveRadius)
}

// styleForHoveredBar is U:scrollbar.rs:1102-1126.
func (s *Scrollbar) styleForHoveredBar(app *gpui.App) scrollbarResolvedStyle {
	return s.resolveStyle(app,
		func(st *ScrollbarStyles) *ScrollbarTrackStyle { return &st.trackHover },
		func(st *ScrollbarStyles) *ScrollbarThumbStyle { return &st.thumb },
		0.35, scrollbarThumbWidth, scrollbarThumbInset, scrollbarThumbRadius)
}

// styleForNormal is U:scrollbar.rs:1128-1153.
func (s *Scrollbar) styleForNormal(app *gpui.App) scrollbarResolvedStyle {
	return s.resolveStyle(app,
		func(st *ScrollbarStyles) *ScrollbarTrackStyle { return &st.track },
		func(st *ScrollbarStyles) *ScrollbarThumbStyle { return &st.thumb },
		0.35, scrollbarThumbWidth, scrollbarThumbInset, scrollbarThumbRadius)
}

// IntoElement converts the scrollbar into its element
// (U:scrollbar.rs:1156-1162).
func (s *Scrollbar) IntoElement() gpui.AnyElement {
	return gpui.CustomElement[struct{}, ScrollbarPrepaintState](scrollbarElement{s})
}

// scrollbarElement is the Element impl of Scrollbar (U:scrollbar.rs:
// 1253-1792); the builder keeps the ID name for its id setter.
type scrollbarElement struct{ s *Scrollbar }

// ScrollbarPrepaintState is the scrollbar's prepaint state
// (U:scrollbar.rs:1164-1169, doc-hidden).
type ScrollbarPrepaintState struct {
	hitbox         gpui.Hitbox
	scrollbarState *scrollbarState
	states         []ScrollbarAxisPrepaintState
}

// ScrollbarAxisPrepaintState is one bar's prepaint state
// (U:scrollbar.rs:1171-1189, doc-hidden).
type ScrollbarAxisPrepaintState struct {
	axis      gpui.Axis
	barHitbox gpui.Hitbox
	bounds    gpui.Bounds
	radius    float32
	bg        gpui.Hsla
	border    gpui.Hsla
	// thumbBounds is the clickable area of the thumb.
	thumbBounds gpui.Bounds
	// thumbFillBounds are the bounds of the thumb to be rendered.
	thumbFillBounds     gpui.Bounds
	thumbBg             gpui.Background
	geometry            thumbGeometry
	activeGeometry      thumbGeometry
	trackWidth          float32
	visibilityOpacity   float32
	visibilityPosition  float32
	visibilityRequested bool
}

// thumbGeometry is the longitudinal geometry that drives painting, track
// clicks and dragging: length excludes the inset, travel excludes the
// full logical thumb (U:scrollbar.rs:1191-1200).
type thumbGeometry struct {
	origin float32
	inset  float32
	length float32
	travel float32
	extent float32
}

// newThumbGeometry is U:scrollbar.rs:1203-1221.
func newThumbGeometry(origin, container, content, marginEnd, inset, minLength float32) thumbGeometry {
	track := max(container-marginEnd, 0)
	logicalLength := min(max(container/content*container, minLength), track)
	inset = clamp32(inset, 0, logicalLength/2)
	return thumbGeometry{
		origin: origin,
		inset:  inset,
		length: logicalLength - inset*2,
		travel: track - logicalLength,
		extent: content - container,
	}
}

// start is U:scrollbar.rs:1223-1225.
func (g thumbGeometry) start(offset float32) float32 {
	return g.origin + g.inset + clamp32(-offset/g.extent, 0, 1)*g.travel
}

// offset is U:scrollbar.rs:1227-1232.
func (g thumbGeometry) offset(position, grab float32) float32 {
	if g.travel <= 0 {
		return 0
	}
	return -g.extent * clamp32((position-g.origin-g.inset-grab)/g.travel, 0, 1)
}

// dragOffset is U:scrollbar.rs:1234-1250.
func (g thumbGeometry) dragOffset(axis gpui.Axis, position, grab, offset gpui.Point) gpui.Point {
	if g.travel <= 0 {
		return offset
	}
	if axis == gpui.AxisVertical {
		offset.Y = g.offset(position.Y, grab.Y)
	} else {
		offset.X = g.offset(position.X, grab.X)
	}
	return offset
}

// ID implements gpui.Element (U:scrollbar.rs:1257-1259).
func (e scrollbarElement) ID() (gpui.ElementID, bool) { return e.s.id, true }

// SourceLocation implements gpui.Element (U:scrollbar.rs:1261-1263).
func (e scrollbarElement) SourceLocation() *gpui.SourceLocation { return nil }

// RequestLayout fills the parent as an absolutely positioned layer
// (U:scrollbar.rs:1265-1280).
func (e scrollbarElement) RequestLayout(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, w *gpui.Window, _ *gpui.App) (gpui.LayoutID, struct{}) {
	style := gpui.DefaultStyle()
	style.Position = gpui.PositionAbsolute
	style.FlexGrow = 1
	style.FlexShrink = 1
	style.Size.Width = gpui.DefiniteLengthOf(gpui.Fraction(1))
	style.Size.Height = gpui.DefiniteLengthOf(gpui.Fraction(1))
	id, err := gpui.RequestElementLayout(w, style)
	if err != nil {
		panic(fmt.Sprintf("base: Scrollbar request_layout: %v", err))
	}
	return id, struct{}{}
}

// currentView is Window::current_view (R:window.rs:5497-5500), which
// unwraps the rendering view.
func currentView(w *gpui.Window) gpui.EntityID {
	view, ok := gpui.CurrentView(w)
	if !ok {
		panic("base: no view is rendering (Window::current_view)")
	}
	return view
}

// scrollbarStateBox is the use_state entity holding the shared cell.
type scrollbarStateBox struct{ cell *scrollbarState }

// Prepaint inserts the hitboxes and resolves visibility, styles and thumb
// geometry per axis (U:scrollbar.rs:1282-1563).
func (e scrollbarElement) Prepaint(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, bounds gpui.Bounds, _ *struct{}, w *gpui.Window, app *gpui.App) ScrollbarPrepaintState {
	s := e.s
	bounds = s.resolvedViewportBounds(bounds)
	hitbox := gpui.WithContentMask(w, &bounds, func(w *gpui.Window) gpui.Hitbox {
		return w.InsertHitbox(bounds, gpui.HitboxNormal)
	})

	state := w.UseState(app, func(*gpui.Window, *gpui.Context[scrollbarStateBox]) scrollbarStateBox {
		return scrollbarStateBox{cell: newScrollbarState(app.Now())}
	}).Read(app, func(b *scrollbarStateBox, _ *gpui.App) *scrollbarState { return b.cell })

	now := app.Now()
	baseTheme := activeTheme(app)
	mode := s.mode.UnwrapOr(baseTheme.Scrollbar.Mode())
	motion := baseTheme.Scrollbar.Motion()
	// Always-visible scrollbars skip visibility motion but still animate
	// their activity width. Reduced motion snaps every channel.
	reduceMotion := app.ReduceMotion()
	var enter, exit time.Duration
	if !mode.isAlways() && !reduceMotion {
		enter, exit = motion.Enter(), motion.Exit()
	}
	var expand time.Duration
	if !reduceMotion {
		expand = motion.Expand()
	}

	inner := state.get()
	currentOffset := s.scrollHandle.Offset()
	if currentOffset != inner.lastScrollOffset {
		inner = inner.withLastScroll(currentOffset, gpui.Some(now))
	}

	isHovered := inner.hoveredAxis.IsSome() || inner.hoveredOnThumb.IsSome()
	isDragging := inner.draggedAxis.IsSome()
	isCurrentlyVisible := inner.visibility.sample(now).opacity > 0
	visible := hoverKeepsVisible(mode, isHovered, isCurrentlyVisible) ||
		wantsVisible(mode, isHovered, isDragging, inner.lastScrollTime, motion.Idle(), now)
	inner.visibility.setVisible(visible, motion.entranceFor(mode, inner.hoveredOnThumb.IsSome()), enter, exit, now)
	visibility := inner.visibility.sample(now)
	if visibility.running {
		gpui.RequestAnimationFrame(w)
	}

	if !isHovered && !isDragging {
		if lastTime, ok := inner.lastScrollTime.Get(); ok {
			elapsed := saturatingSince(now, lastTime)
			if elapsed < motion.Idle() && !inner.idleTimerScheduled {
				inner.idleTimerScheduled = true
				view := currentView(w)
				nextDelay := motion.Idle() - elapsed
				app.SpawnDelivering(w.Scope(), func(run *gpui.TaskRun) struct{} {
					run.Sleep(nextDelay)
					return struct{}{}
				}, func(_ struct{}, app *gpui.App) {
					state.set(state.get().withIdleTimerScheduled(false))
					app.Notify(view)
				})
			}
		}
	}
	state.set(inner)

	var states []ScrollbarAxisPrepaintState
	hasBoth := s.axis.IsBoth()
	scrollSize := s.scrollSize.UnwrapOr(s.scrollHandle.ContentSize())

	for _, axis := range s.axis.all() {
		isVertical := axis == gpui.AxisVertical
		trackWidth := firstSome(s.styles.track.width, baseTheme.Scrollbar.Styles().track.width).UnwrapOr(scrollbarWidthPx)
		hb := hitbox.Bounds
		scrollAreaSize, containerSize, scrollPosition := scrollSize.Width, hb.Size.Width, s.scrollHandle.Offset().X
		if isVertical {
			scrollAreaSize, containerSize, scrollPosition = scrollSize.Height, hb.Size.Height, s.scrollHandle.Offset().Y
		}

		// The horizontal bar avoids overlapping the vertical one when
		// both are shown.
		var marginEnd float32
		if hasBoth && !isVertical {
			marginEnd = trackWidth
		}

		// Hide the scrollbar when the scroll area fits the container.
		if scrollAreaSize <= containerSize {
			hasBoth = false
			continue
		}

		var barBounds gpui.Bounds
		if isVertical {
			barBounds = gpui.Bounds{
				Origin: gpui.Point{X: hb.Origin.X + hb.Size.Width - trackWidth, Y: hb.Origin.Y},
				Size:   gpui.Size{Width: trackWidth, Height: hb.Size.Height},
			}
		} else {
			barBounds = gpui.Bounds{
				Origin: gpui.Point{X: hb.Origin.X, Y: hb.Origin.Y + hb.Size.Height - trackWidth},
				Size:   gpui.Size{Width: hb.Size.Width, Height: trackWidth},
			}
		}

		isAlwaysToShow := mode.isAlways()
		isHoverToShow := mode.isHover()
		isHoveredOnBar := axisIs(state.get().hoveredAxis, axis)
		isHoveredOnThumb := axisIs(state.get().hoveredOnThumb, axis)

		var style scrollbarResolvedStyle
		switch {
		case axisIs(state.get().draggedAxis, axis):
			style = s.styleForActive(app)
		case (isHoverToShow || mode == ScrollbarModeScrolling) && (isHoveredOnBar || isHoveredOnThumb),
			isAlwaysToShow && (isHoveredOnBar || isHoveredOnThumb):
			if isHoveredOnThumb {
				style = s.styleForHoveredThumb(app)
			} else {
				style = s.styleForHoveredBar(app)
			}
		default:
			style = s.styleForNormal(app)
		}

		updated := state.get()
		widthAnim := &updated.horizontalWidth
		if isVertical {
			widthAnim = &updated.verticalWidth
		}
		thumbWidth, running := widthAnim.setTarget(style.width, expand, now)
		state.set(updated)
		if running {
			gpui.RequestAnimationFrame(w)
		}

		origin := barBounds.Origin.X
		if isVertical {
			origin = barBounds.Origin.Y
		}
		geometry := newThumbGeometry(origin, containerSize, scrollAreaSize, marginEnd, style.inset, style.minLength)
		active := s.styleForActive(app)
		activeGeometry := newThumbGeometry(origin, containerSize, scrollAreaSize, marginEnd, active.inset, active.minLength)
		thumbStart := geometry.start(scrollPosition) - origin

		// The clickable area of the thumb, and the area actually painted.
		thumbLength := geometry.length
		var thumbBounds, thumbFillBounds gpui.Bounds
		if isVertical {
			corner := barBounds.TopRight()
			at := gpui.Point{X: corner.X - style.inset, Y: corner.Y + thumbStart}
			thumbBounds = gpui.BoundsFromAnchorAndSize(gpui.AnchorTopRight, at, gpui.Size{Width: trackWidth, Height: thumbLength})
			thumbFillBounds = gpui.BoundsFromAnchorAndSize(gpui.AnchorTopRight, at, gpui.Size{Width: thumbWidth, Height: thumbLength})
		} else {
			corner := barBounds.BottomLeft()
			at := gpui.Point{X: corner.X + thumbStart, Y: corner.Y - style.inset}
			thumbBounds = gpui.BoundsFromAnchorAndSize(gpui.AnchorBottomLeft, at, gpui.Size{Width: thumbLength, Height: trackWidth})
			thumbFillBounds = gpui.BoundsFromAnchorAndSize(gpui.AnchorBottomLeft, at, gpui.Size{Width: thumbLength, Height: thumbWidth})
		}

		barHitbox := gpui.WithContentMask(w, &barBounds, func(w *gpui.Window) gpui.Hitbox {
			return w.InsertHitbox(barBounds, gpui.HitboxNormal)
		})

		states = append(states, ScrollbarAxisPrepaintState{
			axis:                axis,
			barHitbox:           barHitbox,
			bounds:              barBounds,
			radius:              style.radius,
			bg:                  style.track,
			border:              style.border,
			thumbBounds:         thumbBounds,
			thumbFillBounds:     thumbFillBounds,
			thumbBg:             style.thumb,
			geometry:            geometry,
			activeGeometry:      activeGeometry,
			trackWidth:          trackWidth,
			visibilityOpacity:   visibility.opacity,
			visibilityPosition:  visibility.position,
			visibilityRequested: visible,
		})
	}

	return ScrollbarPrepaintState{hitbox: hitbox, states: states, scrollbarState: state}
}

// translateBounds is `Bounds + Point`.
func translateBounds(b gpui.Bounds, p gpui.Point) gpui.Bounds {
	b.Origin.X += p.X
	b.Origin.Y += p.Y
	return b
}

// Paint paints each bar and registers its wheel, press, move and release
// listeners (U:scrollbar.rs:1565-1791).
func (e scrollbarElement) Paint(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, _ gpui.Bounds, _ *struct{}, prepaint *ScrollbarPrepaintState, w *gpui.Window, app *gpui.App) {
	s := e.s
	scrollbarState := prepaint.scrollbarState
	theme := activeTheme(app)
	mode := s.mode.UnwrapOr(theme.Scrollbar.Mode())
	viewID := currentView(w)
	hitboxBounds := prepaint.hitbox.Bounds
	isHoverToShow := mode.isHover()
	scrollHandle := s.scrollHandle
	maxFpsDuration := time.Duration(math.Round(1e9 / float64(s.maxFps)))

	gpui.WithContentMaskVoid(w, &hitboxBounds, func(w *gpui.Window) {
		for i := range prepaint.states {
			state := prepaint.states[i]
			axis := state.axis
			radius := state.radius
			if theme.Tokens.Radius.Md == 0 {
				radius = 0
			}
			radius = clampThumbRadius(radius, state.thumbFillBounds)
			bounds := state.bounds
			thumbBounds := state.thumbBounds
			geometry := state.geometry
			activeGeometry := state.activeGeometry
			visibilityOpacity := state.visibilityOpacity
			isVisible := state.visibilityRequested || visibilityOpacity > 0
			translation := visibilityTranslation(axis, state.trackWidth, state.visibilityPosition)
			paintedBounds := translateBounds(state.bounds, translation)
			paintedThumbBounds := translateBounds(state.thumbFillBounds, translation)
			paintedTrackBg := state.bg.Opacity(visibilityOpacity)
			paintedBorder := state.border.Opacity(visibilityOpacity)
			paintedThumbBg := state.thumbBg.Opacity(visibilityOpacity)

			barHitbox := state.barHitbox
			w.SetCursorStyle(gpui.CursorArrow, &barHitbox)

			w.PaintLayer(hitboxBounds, func(w *gpui.Window) {
				w.PaintQuad(gpui.Fill(paintedBounds, gpui.SolidBackground(paintedTrackBg)))

				// The pinned border quad: zero border widths on either
				// axis (U:scrollbar.rs:1614-1635).
				border := gpui.Fill(paintedBounds, gpui.SolidBackground(gpui.TransparentBlack()))
				border.BorderWidths = gpui.Edges{}
				border.BorderColor = gpui.SolidBackground(paintedBorder)
				w.PaintQuad(border)

				thumb := gpui.Fill(paintedThumbBounds, paintedThumbBg)
				thumb.CornerRadii = gpui.Corners{TopLeft: radius, TopRight: radius, BottomRight: radius, BottomLeft: radius}
				w.PaintQuad(thumb)
			})

			w.OnMouseEvent(func(event any, phase gpui.DispatchPhase, _ *gpui.Window, app *gpui.App) {
				e, ok := event.(*gpui.ScrollWheelEvent)
				if !ok {
					return
				}
				if phase == gpui.DispatchBubble && hitboxBounds.Contains(e.Position) {
					if scrollHandle.Offset() != scrollbarState.get().lastScrollOffset {
						scrollbarState.set(scrollbarState.get().withLastScroll(scrollHandle.Offset(), gpui.Some(app.Now())))
						app.Notify(viewID)
					}
				}
			})

			if isVisible {
				w.OnMouseEvent(func(event any, phase gpui.DispatchPhase, _ *gpui.Window, app *gpui.App) {
					e, ok := event.(*gpui.MouseDownEvent)
					if !ok || phase != gpui.DispatchBubble || !bounds.Contains(e.Position) {
						return
					}
					app.StopPropagation()

					if thumbBounds.Contains(e.Position) {
						// A press on the thumb records the grab point.
						pos := gpui.Point{X: e.Position.X - thumbBounds.Origin.X, Y: e.Position.Y - thumbBounds.Origin.Y}
						ScrollbarHandleStartDrag(scrollHandle)
						scrollbarState.set(scrollbarState.get().withDragPos(axis, pos, app.Now()))
						// Active styling can change the inset or minimum
						// length; keep the grab point under the pointer.
						scrollHandle.SetOffset(activeGeometry.dragOffset(axis, e.Position, pos, scrollHandle.Offset()))
					} else {
						// A press on the track centers the thumb on it.
						center := gpui.Point{X: geometry.length / 2, Y: geometry.length / 2}
						scrollHandle.SetOffset(geometry.dragOffset(axis, e.Position, center, scrollHandle.Offset()))
					}

					app.Notify(viewID)
				})
			}

			w.OnMouseEvent(func(event any, _ gpui.DispatchPhase, w *gpui.Window, app *gpui.App) {
				e, ok := event.(*gpui.MouseMoveEvent)
				if !ok {
					return
				}
				notify := false
				// In hover mode, or once visible, hovering updates the
				// hovered state and extends the idle hold.
				needHoverToUpdate := isHoverToShow || isVisible
				if bounds.Contains(e.Position) && needHoverToUpdate {
					hoverChanged := !axisIs(scrollbarState.get().hoveredAxis, axis)
					scrollbarState.set(scrollbarState.get().withHovered(gpui.Some(axis), app.Now()))
					notify = notify || hoverChanged
				} else if axisIs(scrollbarState.get().hoveredAxis, axis) {
					scrollbarState.set(scrollbarState.get().withHovered(optAxis{}, app.Now()))
					notify = true
				}

				if tracksThumbHover(mode, isVisible) && thumbBounds.Contains(e.Position) {
					if !axisIs(scrollbarState.get().hoveredOnThumb, axis) {
						scrollbarState.set(scrollbarState.get().withHoveredOnThumb(gpui.Some(axis), app.Now()))
						notify = true
					}
				} else if axisIs(scrollbarState.get().hoveredOnThumb, axis) {
					scrollbarState.set(scrollbarState.get().withHoveredOnThumb(optAxis{}, app.Now()))
					notify = true
				}

				// Move the thumb while dragging.
				if axisIs(scrollbarState.get().draggedAxis, axis) && e.Dragging() {
					// Stop propagation to avoid selecting text or other
					// side effects.
					app.StopPropagation()

					offset := activeGeometry.dragOffset(axis, e.Position, scrollbarState.get().dragPos, scrollHandle.Offset())
					if scrollHandle.Offset() != offset {
						// Coalesce rendering, never discard the newest
						// pointer position.
						scrollHandle.SetOffset(offset)
						notify = true
					}
					if notify {
						scrollbarState.notifyDrag(app.Now(), maxFpsDuration, viewID, w, app)
					}
					return
				}

				if notify {
					app.Notify(viewID)
				}
			})

			w.OnMouseEvent(func(event any, phase gpui.DispatchPhase, _ *gpui.Window, app *gpui.App) {
				e, ok := event.(*gpui.MouseUpEvent)
				if !ok || phase != gpui.DispatchBubble || !axisIs(scrollbarState.get().draggedAxis, axis) {
					return
				}
				scrollHandle.SetOffset(activeGeometry.dragOffset(axis, e.Position, scrollbarState.get().dragPos, scrollHandle.Offset()))
				ScrollbarHandleEndDrag(scrollHandle)
				scrollbarState.set(scrollbarState.get().withUnsetDragPos(app.Now()))
				app.Notify(viewID)
			})
		}
	})
}
