package base

// Vertical touch overscroll for an existing scroll viewport
// (U:scroll_bounce.rs). Boundary displacement only: the list keeps its
// clamped logical position.
//
// Go adaptations (recorded deviations):
//
//   - Clock: upstream reads Instant::now() (std or web_time,
//     U:scroll_bounce.rs:239, 397) and the background executor's clock
//     (U:scroll_bounce.rs:317); the port reads App.Now for both, so
//     TestApp.AdvanceClock drives the spring and the momentum gap.
//   - Paint offset: the port's drawables re-resolve their bounds at paint
//     (the core's documented hazard), so the child paints under the same
//     element offset it prepainted under; upstream reuses the prepainted
//     bounds and paints without re-applying it (U:scroll_bounce.rs:408-410).

import (
	"fmt"
	"math"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

// ScrollBounceMotion holds the motion tokens of ScrollBounce: how far a
// drag stretches the viewport past an edge, and how quickly a released
// edge returns (U:scroll_bounce.rs:15-24). Base plays the stretch and the
// return; the feel belongs to the caller.
type ScrollBounceMotion struct {
	tracking float32
	response time.Duration
}

// DefaultScrollBounceMotion returns the default, tuned to feel like a
// UIScrollView bounce (these are not UIKit constants)
// (U:scroll_bounce.rs:26-34).
func DefaultScrollBounceMotion() ScrollBounceMotion {
	return ScrollBounceMotion{tracking: 0.55, response: 524 * time.Millisecond}
}

// WithTracking sets the fraction of finger travel the stretched edge
// follows at first; the edge follows less and less as it approaches the
// viewport height, which it never reaches (U:scroll_bounce.rs:37-53). It
// panics when tracking is not finite or not positive.
func (m ScrollBounceMotion) WithTracking(tracking float32) ScrollBounceMotion {
	if math.IsInf(float64(tracking), 0) || math.IsNaN(float64(tracking)) || !(tracking > 0) {
		panic("scroll bounce tracking must be finite and positive")
	}
	m.tracking = tracking
	return m
}

// WithResponse sets the time scale of the return once the finger lifts:
// the period one undamped oscillation would take. The return is
// critically damped, so it never crosses the edge; a zero response snaps
// the edge back on the spot (U:scroll_bounce.rs:55-65).
func (m ScrollBounceMotion) WithResponse(response time.Duration) ScrollBounceMotion {
	m.response = response
	return m
}

// Tracking returns the fraction of finger travel the stretched edge
// follows at first (U:scroll_bounce.rs:67-70).
func (m ScrollBounceMotion) Tracking() float32 { return m.tracking }

// Response returns the time scale of the return (U:scroll_bounce.rs:72-75).
func (m ScrollBounceMotion) Response() time.Duration { return m.response }

// omega is the undamped angular frequency of the return, absent when it
// snaps (U:scroll_bounce.rs:77-81).
func (m ScrollBounceMotion) omega() (float32, bool) {
	seconds := secondsF32(m.response)
	if seconds > 0 {
		return 2 * math.Pi / seconds, true
	}
	return 0, false
}

// ScrollBounce adds vertical touch overscroll to an existing scroll
// viewport (U:scroll_bounce.rs:84-102).
//
// The child owns layout, content, and ordinary scrolling; the handle must
// be the child's scroll handle. Only unused vertical deltas stretch the
// viewport. The stable id owns gesture and spring state; change it when
// replacing the document. Put fixed chrome (scrollbars, toolbars) outside
// this wrapper.
//
// Enabled by default on iOS and Android; other platforms pass through
// unless explicitly enabled, and their input must emit Ended at finger
// release, before momentum. Reduced motion disables displacement.
// Keyboard, focus, and line-wheel input remain owned by the child.
type ScrollBounce struct {
	id       gpui.ElementID
	handle   ScrollbarHandle
	child    gpui.AnyElement
	enabled  bool
	motion   ScrollBounceMotion
	onScroll func(*gpui.Window, *gpui.App)
}

// NewScrollBounce wraps child, whose scroll handle is handle (any value
// ScrollbarHandleOf accepts), under the given id (U:scroll_bounce.rs:105-118).
func NewScrollBounce[I gpui.IntoElementID, H any](id I, handle H, child any) *ScrollBounce {
	element, err := gpui.TryIntoElement(child)
	if err != nil {
		panic(fmt.Sprintf("base: ScrollBounce child: %v", err))
	}
	return &ScrollBounce{
		id:      gpui.ElementIDOf(id),
		handle:  ScrollbarHandleOf(handle),
		child:   element,
		enabled: IsMobile(),
		motion:  DefaultScrollBounceMotion(),
	}
}

// Enabled opts in on a platform with compatible touch phase semantics
// (U:scroll_bounce.rs:120-124).
func (b *ScrollBounce) Enabled(enabled bool) *ScrollBounce {
	b.enabled = enabled
	return b
}

// Motion sets how far a drag stretches past an edge and how the edge
// returns (U:scroll_bounce.rs:126-130).
func (b *ScrollBounce) Motion(motion ScrollBounceMotion) *ScrollBounce {
	b.motion = motion
	return b
}

// OnScroll observes a logical scroll performed when a reverse drag leaves
// the stretched region. It runs after the handle update; ordinary child
// scrolling keeps the child's own notifications (U:scroll_bounce.rs:132-138).
func (b *ScrollBounce) OnScroll(handler func(*gpui.Window, *gpui.App)) *ScrollBounce {
	b.onScroll = handler
	return b
}

// bounceState is U:scroll_bounce.rs:141-151.
type bounceState struct {
	physics           bouncePhysics
	sampledAt         gpui.Optional[time.Duration]
	ongoingScroll     gpui.OngoingScroll
	shortDragDistance gpui.Optional[float32]
	lastWheelAt       gpui.Optional[time.Duration]
	// suppressedDirection is the sign of the stream being suppressed,
	// when it is known to be momentum that cannot reverse; absent
	// suppresses both directions.
	suppressedDirection gpui.Optional[float32]
}

// newBounceState is State::default() (U:scroll_bounce.rs:141, derive
// Default: the physics carry the default motion).
func newBounceState() *bounceState {
	return &bounceState{physics: defaultBouncePhysics()}
}

// release releases the edge (U:scroll_bounce.rs:153-163). A stretch made
// outside a gesture is momentum (or a phaseless wheel) hitting the edge;
// that stream only pushes outward, so an inward packet is a new scroll and
// ends the suppression. After a gesture's own release the suppressed
// momentum may point either way.
func (s *bounceState) release(fromRest bool) {
	s.physics.release()
	if fromRest && s.physics.suppressMomentum {
		s.suppressedDirection = gpui.Some(signum32(s.physics.offset()))
	} else {
		s.suppressedDirection = gpui.Optional[float32]{}
	}
}

// catchDragSlop: GPUI starts a normal touch pan only after its 8 px touch
// slop, but a touch catching a fling starts at zero displacement; a short
// catch should stop the old fling rather than turn a few fast pixels into
// a new one (U:scroll_bounce.rs:165-168).
const catchDragSlop float32 = 8

// momentumGap: momentum arrives once per frame until it stops, so a
// longer silence means the suppressed stream has ended
// (U:scroll_bounce.rs:170-174).
const momentumGap = 250 * time.Millisecond

// maxScrollExtent recovers the max offset from the definition
// content_size = viewport + max_offset; both dispatch phases clamp
// against it and must agree (U:scroll_bounce.rs:176-181).
func maxScrollExtent(handle ScrollbarHandle) float32 {
	return max(handle.ContentSize().Height-handle.ViewportBounds().Size.Height, 0)
}

// signum32 is f32::signum.
func signum32(v float32) float32 {
	if v != v {
		return v
	}
	if math.Signbit(float64(v)) {
		return -1
	}
	return 1
}

// ScrollBouncePrepaintState is the prepaint state (U:scroll_bounce.rs:
// 183-187, doc-hidden), plus the offset the child prepainted under.
type ScrollBouncePrepaintState struct {
	state  *bounceState
	hitbox gpui.Hitbox
	offset float32
}

// IntoElement converts the wrapper into its element
// (U:scroll_bounce.rs:189-194).
func (b *ScrollBounce) IntoElement() gpui.AnyElement {
	return gpui.CustomElement[struct{}, ScrollBouncePrepaintState](scrollBounceElement{b})
}

// scrollBounceElement is the Element impl of ScrollBounce
// (U:scroll_bounce.rs:196-412).
type scrollBounceElement struct{ b *ScrollBounce }

// ID implements gpui.Element (U:scroll_bounce.rs:200-202).
func (e scrollBounceElement) ID() (gpui.ElementID, bool) { return e.b.id, true }

// SourceLocation implements gpui.Element (U:scroll_bounce.rs:203-205).
func (e scrollBounceElement) SourceLocation() *gpui.SourceLocation { return nil }

// RequestLayout is the child's layout (U:scroll_bounce.rs:207-215).
func (e scrollBounceElement) RequestLayout(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, w *gpui.Window, app *gpui.App) (gpui.LayoutID, struct{}) {
	return e.b.child.RequestLayout(w, app), struct{}{}
}

// Prepaint steps the spring and prepaints the child displaced by it
// (U:scroll_bounce.rs:217-256).
func (e scrollBounceElement) Prepaint(global *gpui.GlobalElementID, _ *gpui.InspectorElementID, bounds gpui.Bounds, _ *struct{}, w *gpui.Window, app *gpui.App) ScrollBouncePrepaintState {
	b := e.b
	if global == nil {
		panic("ScrollBounce has an id")
	}
	state := gpui.WithElementState(w, global, func(slot **bounceState, _ *gpui.Window) (*bounceState, *bounceState) {
		var st *bounceState
		if slot != nil {
			st = *slot
		}
		if st == nil {
			st = newBounceState()
		}
		return st, st
	})
	if !b.enabled || app.ReduceMotion() {
		*state = *newBounceState()
	}
	state.physics.motion = b.motion
	now := app.Now()
	elapsed := float32(0)
	if at, ok := state.sampledAt.Get(); ok {
		elapsed = secondsF32(saturatingSince(now, at))
	}
	state.sampledAt = gpui.Some(now)
	if state.physics.step(elapsed) {
		gpui.RequestAnimationFrame(w)
	}
	offset := state.physics.offset()

	hitbox := w.InsertHitbox(bounds, gpui.HitboxNormal)
	gpui.WithContentMaskVoid(w, &bounds, func(w *gpui.Window) {
		gpui.WithElementOffset(w, gpui.Point{Y: offset}, func(w *gpui.Window) struct{} {
			b.child.Prepaint(w, app)
			return struct{}{}
		})
	})
	return ScrollBouncePrepaintState{state: state, hitbox: hitbox, offset: offset}
}

// Paint registers the two-phase wheel listener and paints the child
// (U:scroll_bounce.rs:258-411).
func (e scrollBounceElement) Paint(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, bounds gpui.Bounds, _ *struct{}, prepaint *ScrollBouncePrepaintState, w *gpui.Window, app *gpui.App) {
	b := e.b
	if b.enabled && !app.ReduceMotion() {
		state := prepaint.state
		hitbox := prepaint.hitbox
		handle := b.handle
		view := currentView(w)
		onScroll := b.onScroll
		var before float32
		allowEndBounce := false
		w.OnMouseEvent(func(event any, phase gpui.DispatchPhase, w *gpui.Window, app *gpui.App) {
			ev, ok := event.(*gpui.ScrollWheelEvent)
			if !ok || !ev.Delta.IsPixels {
				return
			}
			delta := ev.Delta.Pixels
			if !hitbox.ShouldHandleScroll(w) {
				return
			}
			// Lock the gesture to the axis it started on, so a diagonal
			// swipe cannot wobble out of the stretch from one packet to
			// the next. Both phases see the same packet, and the lock
			// gives both the same answer.
			OngoingScrollLockAxis(&state.ongoingScroll, &delta, ev.TouchPhase, app)
			if abs32(delta.X) > abs32(delta.Y) {
				return
			}
			ended := ev.TouchPhase == gpui.TouchPhaseEnded || ev.TouchPhase == gpui.TouchPhaseCancelled
			scrolled := false
			changed := false
			if phase == gpui.DispatchCapture {
				before = handle.Offset().Y
				if ev.TouchPhase == gpui.TouchPhaseStarted {
					if delta.Y == 0 {
						state.shortDragDistance = gpui.Some[float32](0)
					} else {
						state.shortDragDistance = gpui.Optional[float32]{}
					}
					state.physics.begin(bounds.Size.Height)
				} else if distance, ok := state.shortDragDistance.Get(); ok {
					distance += abs32(delta.Y)
					state.shortDragDistance = gpui.Some(distance)
					if distance > catchDragSlop {
						state.shortDragDistance = gpui.Optional[float32]{}
					}
				}
				suppressShortDragMomentum := false
				if ended {
					hadShortDrag := state.shortDragDistance.IsSome()
					state.shortDragDistance = gpui.Optional[float32]{}
					suppressShortDragMomentum = hadShortDrag && ev.TouchPhase == gpui.TouchPhaseEnded
				}
				// The current Ended packet may still cross an edge; only
				// momentum packets after it should be suppressed.
				allowEndBounce = suppressShortDragMomentum
				now := app.Now()
				paused := false
				if at, ok := state.lastWheelAt.Get(); ok {
					paused = saturatingSince(now, at) >= momentumGap
				}
				state.lastWheelAt = gpui.Some(now)
				reversed := false
				if direction, ok := state.suppressedDirection.Get(); ok {
					reversed = delta.Y != 0 && signum32(delta.Y) != direction
				}
				if paused || reversed {
					state.physics.suppressMomentum = false
				}
				if state.physics.suppressMomentum {
					app.StopPropagation()
					return
				}
				if state.physics.offset() != 0 {
					// Outside a gesture (a phaseless wheel once
					// suppression lifts) a packet grabs the returning edge
					// and lets it go again, as it would at rest.
					fromRest := !state.physics.dragging
					if fromRest {
						state.physics.begin(bounds.Size.Height)
					}
					remainder := state.physics.pull(delta.Y)
					if remainder != 0 {
						max := maxScrollExtent(handle)
						offset := handle.Offset()
						offset.Y = clamp32(before+remainder, -max, 0)
						handle.SetOffset(offset)
						scrolled = true
					}
					if ended || fromRest {
						state.release(fromRest)
					}
					changed = true
					app.StopPropagation()
				} else if ended {
					state.physics.release()
				}
				if suppressShortDragMomentum {
					state.physics.suppressMomentum = true
					state.suppressedDirection = gpui.Optional[float32]{}
				}
			} else {
				// The div applies deltas immediately but clamps at its
				// next prepaint: clamp here so boundary deltas are not
				// mistaken for consumed scrolling (ListState clamps
				// eagerly).
				offset := handle.Offset()
				max := maxScrollExtent(handle)
				clamped := clamp32(offset.Y, -max, 0)
				if clamped != offset.Y {
					offset.Y = clamped
					handle.SetOffset(offset)
				}
				after := offset.Y
				requested := delta.Y
				// A List can coalesce several packets against one painted
				// scroll position: their offset difference alone does not
				// prove overscroll, especially after direction changes or
				// a zero-delta Ended packet from a trackpad.
				atOutwardEdge := (requested > 0 && offset.Y == 0) || (requested < 0 && offset.Y == -max)
				residual := clamp32(requested-(after-before), min(requested, 0), max32(requested, 0))
				if atOutwardEdge && abs32(residual) > 0.01 && (!state.physics.suppressMomentum || allowEndBounce) {
					dragging := state.physics.dragging
					if !dragging {
						state.physics.begin(bounds.Size.Height)
					}
					state.physics.pull(residual)
					if !dragging || ended {
						state.release(!dragging)
					}
					changed = true
				}
			}
			if changed {
				state.sampledAt = gpui.Some(app.Now())
			}
			if changed {
				app.Notify(view)
			}
			if scrolled && onScroll != nil {
				onScroll(w, app)
			}
		})
	}
	gpui.WithContentMaskVoid(w, &bounds, func(w *gpui.Window) {
		gpui.WithElementOffset(w, gpui.Point{Y: prepaint.offset}, func(w *gpui.Window) struct{} {
			b.child.Paint(w, app)
			return struct{}{}
		})
	})
}

// max32 is f32::max for the residual bounds.
func max32(a, b float32) float32 { return max(a, b) }

// bouncePhysics is U:scroll_bounce.rs:414-422.
type bouncePhysics struct {
	position         float32
	velocity         float32
	dragging         bool
	suppressMomentum bool
	extent           float32
	motion           ScrollBounceMotion
}

// defaultBouncePhysics is Physics::default() (U:scroll_bounce.rs:414,
// derive Default: motion is ScrollBounceMotion::default()).
func defaultBouncePhysics() bouncePhysics {
	return bouncePhysics{motion: DefaultScrollBounceMotion()}
}

// offset is the rubber-banded displacement while dragging, else the raw
// position (U:scroll_bounce.rs:425-433).
func (p *bouncePhysics) offset() float32 {
	if p.dragging {
		d := max(p.extent, 1)
		tracking := p.motion.tracking
		return p.position * tracking / (1 + tracking*abs32(p.position)/d)
	}
	return p.position
}

// begin grabs the edge (U:scroll_bounce.rs:435-450). A displaced edge
// keeps the extent it was stretched under: the curve saturates at the
// extent, so re-reading a viewport that shrank mid-return could not place
// the finger where the edge is. The curve is inverted so grabbing a
// returning edge is continuous.
func (p *bouncePhysics) begin(extent float32) {
	offset := p.offset()
	if offset == 0 {
		p.extent = max(extent, 1)
	}
	tracking := p.motion.tracking
	p.position = offset / (tracking * max(1-abs32(offset)/p.extent, 0.01))
	p.velocity = 0
	p.dragging = true
	p.suppressMomentum = false
}

// pull applies finger displacement and returns the part that crosses
// back into the list (U:scroll_bounce.rs:452-463).
func (p *bouncePhysics) pull(delta float32) float32 {
	previous := p.position
	next := previous + delta
	if previous != 0 && signum32(previous) != signum32(next) {
		p.position = 0
		return next
	}
	p.position = next
	return 0
}

// release lets the edge go (U:scroll_bounce.rs:465-471).
func (p *bouncePhysics) release() {
	p.position = p.offset()
	p.dragging = false
	if p.position != 0 {
		p.suppressMomentum = true
	}
}

// step is the exact critically damped spring integration, independent of
// refresh rate (U:scroll_bounce.rs:473-494).
func (p *bouncePhysics) step(seconds float32) bool {
	if p.dragging || p.position == 0 {
		return false
	}
	omega, ok := p.motion.omega()
	if !ok {
		p.position = 0
		p.velocity = 0
		return false
	}
	decay := float32(math.Exp(float64(-omega * seconds)))
	c := p.velocity + omega*p.position
	p.position = (p.position + c*seconds) * decay
	p.velocity = (p.velocity - omega*c*seconds) * decay
	if abs32(p.position) < 0.1 && abs32(p.velocity) < 1 {
		p.position = 0
		p.velocity = 0
		return false
	}
	return true
}
