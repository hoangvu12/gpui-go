package base

// The scrollbar value types the Theme global carries (U:theme.rs:5 imports
// ScrollbarMode, ScrollbarMotion and ScrollbarStyles from scrollbar.rs).
// They are ported here ahead of the Scrollbar element because Theme needs
// them; the scrollbar ticket (18) owns scrollbar.rs's element and moves
// these declarations into scrollbar.go when it lands. Only plain values
// are here: no element, no animation sampling.

import (
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

// scrollbarDefaultIdle is how long visibility is held after the last
// activity (U:scrollbar.rs:37 DEFAULT_IDLE).
const scrollbarDefaultIdle = 2 * time.Second

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
// (U:scrollbar.rs:282-378). Base installs no motion of its own: every
// transition duration defaults to zero, so visibility and thumb width
// snap.
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
