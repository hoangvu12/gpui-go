package base

// Unstyled toasts (U:toast.rs): the ToastManager lifecycle, the
// ToastStack element owning measurement, overlap and expansion motion,
// and the semantic Toast root.
//
// Go adaptations, recorded:
//
//   - Instant is time.Time; Option<Duration> is gpui.Optional.
//   - ToastStackState's shared Rc cells are one shared pointer: copies
//     of a ToastStackState share the layout state like Rust clones. Its
//     measured heights are keyed by the item ElementID's rendered form
//     (ElementID holds a pointer for named children, so it is not a map
//     key).
//   - ToastManager's iterators are slices of ToastEntry.
//   - ObservedElement (test_support) is the identity in this port
//     (observe.go), so the bases are plain stateful divs.
//   - Display (map.md gap 2b): the stack root, each item layer and the
//     Toast root are plain divs upstream (U:toast.rs:298, 526, 611),
//     display Block under crates.io gpui 0.2.2 (the pin defaults to Flex,
//     R:style.rs:1024-1027), so each is built with Block() before the
//     caller's style refines over it; a caller's Flex() still wins. The
//     item layers depend on it: a toast without its own width fills its
//     layer between the sprung insets, which is how collapsed layers
//     narrow (the reflow test fails under Flex).

import (
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

// ToastMotion are the motion tokens of an unstyled toast stack
// (U:toast.rs:20-42).
type ToastMotion struct {
	// Duration is the time scale of the stack's motion: ToastManager
	// treats it as a deadline (a toast is present once it elapsed); the
	// layout treats it as a spring response (U:toast.rs:23-31).
	Duration time.Duration
	// ExitDuration is the duration before an ending toast is unmounted.
	ExitDuration time.Duration
	// CollapsedPeek is the visible distance between collapsed layers.
	CollapsedPeek float32
	// ExpandedGap is the distance between expanded items.
	ExpandedGap float32
	// CollapsedScaleStep is the fractional width reduction of each
	// collapsed layer.
	CollapsedScaleStep float32
	// CollapsedVisible is the number of layers visible while collapsed.
	CollapsedVisible int
}

// ToastMotionSonner is the motion of the shadcn/Sonner toaster
// (U:toast.rs:44-56).
func ToastMotionSonner() ToastMotion {
	return ToastMotion{
		Duration:           400 * time.Millisecond,
		ExitDuration:       200 * time.Millisecond,
		CollapsedPeek:      14,
		ExpandedGap:        14,
		CollapsedScaleStep: 0.05,
		CollapsedVisible:   3,
	}
}

// DefaultToastMotion is ToastMotionSonner (U:toast.rs:58-62).
func DefaultToastMotion() ToastMotion { return ToastMotionSonner() }

// toastStackShared is the shared layout state behind a ToastStackState
// (U:toast.rs:64-71).
type toastStackShared struct {
	heights map[string]float32
	width   float32
	hovered bool
	focused bool
}

// ToastStackState is the persistent private layout state of a
// ToastStack (U:toast.rs:64-71). Copies share the state.
type ToastStackState struct {
	shared *toastStackShared
}

// DefaultToastStackState creates an empty state (U:toast.rs:65
// derive(Default)).
func DefaultToastStackState() ToastStackState {
	return ToastStackState{shared: &toastStackShared{heights: make(map[string]float32)}}
}

// IsExpanded reports whether interaction has expanded the stack
// (U:toast.rs:73-78).
func (s ToastStackState) IsExpanded() bool { return s.shared.hovered || s.shared.focused }

// ToastOptions are applied when a toast enters a ToastManager
// (U:toast.rs:80-85).
type ToastOptions struct {
	// Timeout is the active time before automatic dismissal; absent
	// disables auto-hide.
	Timeout gpui.Optional[time.Duration]
}

// ToastTransitionStatus is the lifecycle phase exposed by a toast root
// to application-owned presentation (U:toast.rs:586-596).
type ToastTransitionStatus uint8

const (
	// ToastStarting is a toast just added, which may run its enter
	// transition (the default).
	ToastStarting ToastTransitionStatus = iota
	// ToastPresent is a fully present toast.
	ToastPresent
	// ToastEnding is a closing toast, mounted until its exit transition
	// completes.
	ToastEnding
)

// String renders the phase like the derived Debug.
func (s ToastTransitionStatus) String() string {
	switch s {
	case ToastPresent:
		return "Present"
	case ToastEnding:
		return "Ending"
	}
	return "Starting"
}

// managedToast is one stored toast (U:toast.rs:87-95).
type managedToast[I comparable, T any] struct {
	id                I
	value             T
	status            ToastTransitionStatus
	timeoutRemaining  gpui.Optional[time.Duration]
	transitionElapsed time.Duration
	lastAdvance       time.Time
}

// ToastEntry is one mounted toast in display order (the iterator item
// (&I, &T, ToastTransitionStatus), U:toast.rs:138-164).
type ToastEntry[I comparable, T any] struct {
	ID     I
	Value  T
	Status ToastTransitionStatus
}

// ToastRemoved is a toast removed after its exit (U:toast.rs:107).
type ToastRemoved[I comparable, T any] struct {
	ID    I
	Value T
}

// ToastAdvance are the changes produced when a manager advances its
// lifecycle clock (U:toast.rs:97-108).
type ToastAdvance[I comparable, T any] struct {
	// Changed reports whether a mounted toast changed phase or
	// membership.
	Changed bool
	// Presented are the toasts whose entry transition completed.
	Presented []I
	// Ending are the toasts that entered their ending transition.
	Ending []I
	// Removed are the toasts removed after their ending transition.
	Removed []ToastRemoved[I, T]
}

// ToastManager is ordered toast storage, lifecycle, auto-hide, limits
// and exit coordination (U:toast.rs:110-116).
type ToastManager[I comparable, T any] struct {
	entries            []managedToast[I, T]
	transitionDuration time.Duration
	exitDuration       time.Duration
}

// NewToastManager creates a manager using the motion's durations for
// enter and exit (U:toast.rs:119-126).
func NewToastManager[I comparable, T any](motion ToastMotion) *ToastManager[I, T] {
	return &ToastManager[I, T]{transitionDuration: motion.Duration, exitDuration: motion.ExitDuration}
}

// Len returns the number of mounted toasts, ending ones included
// (U:toast.rs:128-131).
func (m *ToastManager[I, T]) Len() int { return len(m.entries) }

// IsEmpty reports whether no toast is mounted (U:toast.rs:133-136).
func (m *ToastManager[I, T]) IsEmpty() bool { return len(m.entries) == 0 }

// Iter returns the mounted toasts in display order (U:toast.rs:138-143).
func (m *ToastManager[I, T]) Iter() []ToastEntry[I, T] {
	out := make([]ToastEntry[I, T], 0, len(m.entries))
	for _, e := range m.entries {
		out = append(out, ToastEntry[I, T]{ID: e.id, Value: e.value, Status: e.status})
	}
	return out
}

// Visible returns the newest limit active toasts plus every ending
// toast, in display order (U:toast.rs:145-164).
func (m *ToastManager[I, T]) Visible(limit int) []ToastEntry[I, T] {
	active := 0
	for _, e := range m.entries {
		if e.status != ToastEnding {
			active++
		}
	}
	first := max(active-limit, 0)
	var out []ToastEntry[I, T]
	activeIndex := 0
	for _, e := range m.entries {
		visible := true
		if e.status != ToastEnding {
			visible = activeIndex >= first
			activeIndex++
		}
		if visible {
			out = append(out, ToastEntry[I, T]{ID: e.id, Value: e.value, Status: e.status})
		}
	}
	return out
}

// Get returns a mounted toast's value (U:toast.rs:166-174).
func (m *ToastManager[I, T]) Get(id I) (T, bool) {
	for _, e := range m.entries {
		if e.id == id {
			return e.value, true
		}
	}
	var zero T
	return zero, false
}

// Push adds a newest toast, replacing (and returning) an existing toast
// with the same id (U:toast.rs:178-195).
func (m *ToastManager[I, T]) Push(id I, value T, options ToastOptions, now time.Time) (T, bool) {
	var replaced T
	found := false
	for i, e := range m.entries {
		if e.id == id {
			replaced, found = e.value, true
			m.entries = append(m.entries[:i], m.entries[i+1:]...)
			break
		}
	}
	m.entries = append(m.entries, managedToast[I, T]{
		id:               id,
		value:            value,
		status:           ToastStarting,
		timeoutRemaining: options.Timeout,
		lastAdvance:      now,
	})
	return replaced, found
}

// Dismiss begins a toast's exit transition, reporting whether its state
// changed (U:toast.rs:197-209).
func (m *ToastManager[I, T]) Dismiss(id I, now time.Time) bool {
	for i := range m.entries {
		entry := &m.entries[i]
		if entry.id != id {
			continue
		}
		if entry.status == ToastEnding {
			return false
		}
		entry.status = ToastEnding
		entry.transitionElapsed = 0
		entry.lastAdvance = now
		return true
	}
	return false
}

// DismissAll begins the exit transition of every active toast
// (U:toast.rs:211-223).
func (m *ToastManager[I, T]) DismissAll(now time.Time) []I {
	var changed []I
	for i := range m.entries {
		entry := &m.entries[i]
		if entry.status != ToastEnding {
			entry.status = ToastEnding
			entry.transitionElapsed = 0
			entry.lastAdvance = now
			changed = append(changed, entry.id)
		}
	}
	return changed
}

// Advance advances lifecycle time; active timers pause while paused
// (U:toast.rs:225-277).
func (m *ToastManager[I, T]) Advance(now time.Time, paused bool) ToastAdvance[I, T] {
	var result ToastAdvance[I, T]
	for i := range m.entries {
		entry := &m.entries[i]
		var delta time.Duration
		if now.After(entry.lastAdvance) {
			delta = now.Sub(entry.lastAdvance)
		}
		entry.lastAdvance = now
		switch entry.status {
		case ToastStarting:
			entry.transitionElapsed += delta
			if entry.transitionElapsed >= m.transitionDuration {
				entry.status = ToastPresent
				entry.transitionElapsed = 0
				result.Presented = append(result.Presented, entry.id)
				result.Changed = true
			}
		case ToastPresent:
			if paused {
				break
			}
			if remaining, ok := entry.timeoutRemaining.Get(); ok {
				remaining = max(remaining-delta, 0)
				entry.timeoutRemaining = gpui.Some(remaining)
				if remaining == 0 {
					entry.status = ToastEnding
					entry.transitionElapsed = 0
					result.Ending = append(result.Ending, entry.id)
					result.Changed = true
				}
			}
		case ToastEnding:
			entry.transitionElapsed += delta
		}
	}
	kept := m.entries[:0]
	for _, entry := range m.entries {
		if entry.status == ToastEnding && entry.transitionElapsed >= m.exitDuration {
			result.Removed = append(result.Removed, ToastRemoved[I, T]{ID: entry.id, Value: entry.value})
			result.Changed = true
			continue
		}
		kept = append(kept, entry)
	}
	m.entries = kept
	return result
}

// toastStackItem is one keyed stack child (U:toast.rs:290).
type toastStackItem struct {
	id    gpui.ElementID
	child gpui.AnyElement
}

// ToastStack is a deep toast-stack element owning measurement, overlap
// and expansion motion (U:toast.rs:280-291). Its Styled methods have
// their own storage; its Interactive methods reach the root div.
type ToastStack struct {
	gpui.Styled[*ToastStack]
	gpui.Interactive[*ToastStack]

	id          gpui.ElementID
	base        *gpui.DivElement
	state       ToastStackState
	motion      ToastMotion
	placement   gpui.Anchor
	focusHandle *gpui.FocusHandle
	children    []toastStackItem
}

// NewToastStack creates a stack with Base UI-compatible motion
// (U:toast.rs:293-307 ToastStack::new).
func NewToastStack[I gpui.IntoElementID](id I, state ToastStackState) *ToastStack {
	eid := gpui.ElementIDOf(id)
	base := TestSupport(gpui.Div().Block().ID(eid))
	s := &ToastStack{
		id:        eid,
		base:      base,
		state:     state,
		motion:    ToastMotionSonner(),
		placement: gpui.AnchorTopRight,
	}
	gpui.BindStyled(s)
	gpui.BindInteractive(s, base)
	return s
}

// Item adds a stably keyed toast item (U:toast.rs:309-313): any child
// value.
func (s *ToastStack) Item[I gpui.IntoElementID](id I, child any) *ToastStack {
	s.children = append(s.children, toastStackItem{id: gpui.ElementIDOf(id), child: overlayIntoElement("ToastStack item", child)})
	return s
}

// Motion sets the motion tokens (U:toast.rs:315-319).
func (s *ToastStack) Motion(motion ToastMotion) *ToastStack {
	s.motion = motion
	return s
}

// Placement sets the viewport edge the stack geometry anchors to
// (U:toast.rs:321-325).
func (s *ToastStack) Placement(placement gpui.Anchor) *ToastStack {
	s.placement = placement
	return s
}

// FocusHandle sets the focus scope that expands the stack and pauses
// auto-hide timers (U:toast.rs:327-331).
func (s *ToastStack) FocusHandle(focusHandle gpui.FocusHandle) *ToastStack {
	s.focusHandle = &focusHandle
	return s
}

// Extend appends unkeyed children, keyed by their position
// (U:toast.rs:340-350).
func (s *ToastStack) Extend(elements []gpui.AnyElement) {
	for index, child := range elements {
		s.children = append(s.children, toastStackItem{
			id:    gpui.NamedIntegerElementID("toast-stack-child", uint64(index)),
			child: child,
		})
	}
}

// Child adds one unkeyed child (ParentElement::child).
func (s *ToastStack) Child(value any) *ToastStack {
	s.Extend([]gpui.AnyElement{overlayIntoElement("ToastStack", value)})
	return s
}

// toastStackOffset is one item's (collapsed, expanded) offset.
type toastStackOffset struct {
	collapsed, expanded float32
}

// toastStackGeometry computes the collapsed and expanded stack heights
// and each item's offsets, the newest item last (U:toast.rs:365-412
// stack_geometry).
func toastStackGeometry(heights []float32, gap, peek float32, anchoredBottom bool) (float32, float32, []toastStackOffset) {
	count := len(heights)
	var expandedHeight float32
	for _, h := range heights {
		expandedHeight += h
	}
	expandedHeight += gap * float32(max(count-1, 0))
	var frontHeight float32
	if count > 0 {
		frontHeight = heights[count-1]
	}
	collapsedHeight := frontHeight + peek*float32(max(count-1, 0))
	for index, h := range heights {
		rank := count - 1 - index
		collapsedHeight = max(collapsedHeight, h+peek*float32(rank))
	}
	offsets := make([]toastStackOffset, 0, count)
	for index, h := range heights {
		rank := count - 1 - index
		var newerHeight float32
		for _, newer := range heights[index+1:] {
			newerHeight += newer
		}
		var o toastStackOffset
		if anchoredBottom {
			o.expanded = expandedHeight - newerHeight - gap*float32(rank) - h
			o.collapsed = collapsedHeight - h - peek*float32(rank)
		} else {
			o.expanded = newerHeight + gap*float32(rank)
			o.collapsed = peek * float32(rank)
		}
		offsets = append(offsets, o)
	}
	return collapsedHeight, expandedHeight, offsets
}

// RenderOnce implements gpui.RenderOnce (U:toast.rs:414-584): the items
// are absolutely positioned at sprung offsets and insets, measured in
// prepaint; the stack height is sprung between the collapsed and
// expanded heights; hover over the collapsed stack (or focus inside the
// focus scope) expands it.
func (s *ToastStack) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	shared := s.state.shared
	shared.focused = s.focusHandle != nil && s.focusHandle.ContainsFocused(w)
	expanded := s.state.IsExpanded()
	keys := make(map[string]bool, len(s.children))
	for _, item := range s.children {
		keys[item.id.String()] = true
	}
	for key := range shared.heights {
		if !keys[key] {
			delete(shared.heights, key)
		}
	}
	heights := make([]float32, 0, len(s.children))
	for _, item := range s.children {
		heights = append(heights, shared.heights[item.id.String()])
	}
	peek := s.motion.CollapsedPeek
	gap := s.motion.ExpandedGap
	scaleStep := s.motion.CollapsedScaleStep
	collapsedVisible := max(s.motion.CollapsedVisible, 1)
	stackWidth := shared.width
	count := len(s.children)
	anchoredBottom := s.placement == gpui.AnchorBottomLeft || s.placement == gpui.AnchorBottomCenter || s.placement == gpui.AnchorBottomRight
	collapsedHeight, expandedHeight, offsets := toastStackGeometry(heights, gap, peek, anchoredBottom)

	// The whole stack reflows every time a toast arrives or leaves, so
	// each layer is sprung: one retargeted mid-move carries its velocity
	// into the new layout instead of restarting. Critically damped,
	// because a height or an opacity that overshoots reads as a glitch;
	// geometry settles in pixels, so its tolerance is coarser than the
	// fade's (U:toast.rs:453-466).
	geometry := NewSpring(s.motion.Duration).WithEpsilon(0.1)
	fade := NewSpring(s.motion.Duration)
	heightTarget := collapsedHeight
	if expanded {
		heightTarget = expandedHeight
	}
	stackHeight := MotionSpring(TransitionIDWithChannel(s.id, "height"), heightTarget, geometry, w, app)

	items := make([]any, 0, count)
	for index, item := range s.children {
		var offset toastStackOffset
		if index < len(offsets) {
			offset = offsets[index]
		}
		rank := max(count-(1+index), 0)
		targetOffset := offset.collapsed
		if expanded {
			targetOffset = offset.expanded
		}
		top := MotionSpring(TransitionIDWithChannel(item.id, "offset"), targetOffset, geometry, w, app)
		var targetInset float32
		if !expanded {
			targetInset = stackWidth * (scaleStep * float32(min(rank, collapsedVisible-1)) / 2)
		}
		inset := MotionSpring(TransitionIDWithChannel(item.id, "inset"), targetInset, geometry, w, app)
		var targetOpacity float32
		if expanded || rank < collapsedVisible {
			targetOpacity = 1
		}
		opacity := MotionSpring(TransitionIDWithChannel(item.id, "visibility"), targetOpacity, fade, w, app)
		layer := gpui.Div().Block().
			ID(item.id).
			Absolute().
			Top0().
			Left0().
			Right0().
			Top(gpui.Px(top)).
			Left(gpui.Px(inset)).
			Right(gpui.Px(inset)).
			Opacity(opacity)
		if !expanded && rank >= collapsedVisible {
			layer.Invisible()
		}
		key := item.id.String()
		layer = ElementExtOf(layer).OnPrepaint(func(bounds gpui.Bounds, _ *gpui.Window, app *gpui.App) {
			if h, ok := shared.heights[key]; !ok || h != bounds.Size.Height {
				shared.heights[key] = bounds.Size.Height
				app.RefreshWindows()
			}
		})
		items = append(items, layer.Child(item.child))
	}

	root := s.base.Relative().H(gpui.Px(stackHeight))
	if s.focusHandle != nil {
		root.TrackFocus(*s.focusHandle)
	}
	root.Child(gpui.Canvas(
		func(gpui.Bounds, *gpui.Window, *gpui.App) struct{} { return struct{}{} },
		func(bounds gpui.Bounds, _ struct{}, w *gpui.Window, app *gpui.App) {
			if shared.width != bounds.Size.Width {
				shared.width = bounds.Size.Width
				app.RefreshWindows()
			}
			if !expanded {
				if anchoredBottom {
					bounds.Origin.Y += stackHeight - collapsedHeight
				}
				bounds.Size.Height = collapsedHeight
			}
			w.OnMouseEvent(func(event any, _ gpui.DispatchPhase, _ *gpui.Window, app *gpui.App) {
				move, ok := event.(*gpui.MouseMoveEvent)
				if !ok {
					return
				}
				hovered := bounds.Contains(move.Position)
				if shared.hovered != hovered {
					shared.hovered = hovered
					app.RefreshWindows()
				}
			})
		},
	).Flex().Absolute().SizeFull())
	root.Children(items...)
	style := s.Style()
	return StyledExtOf(root).RefineStyle(&style).IntoElement()
}

// Toast is an unstyled semantic toast root with the Alert role;
// applications own all presentation and motion (U:toast.rs:598-605). Its
// Styled methods have their own storage, its Parent methods their own
// children, and its Interactive methods reach the root div.
type Toast struct {
	gpui.Component[*Toast]

	base             *gpui.DivElement
	transitionStatus ToastTransitionStatus
}

// NewToast creates a toast in the starting phase (U:toast.rs:607-616
// Toast::new).
func NewToast[I gpui.IntoElementID](id I) *Toast {
	base := TestSupport(gpui.Div().Block().ID(gpui.ElementIDOf(id)))
	return gpui.BindComponent(&Toast{base: base, transitionStatus: ToastStarting}, base)
}

// TransitionStatus sets the lifecycle phase used by application-owned
// presentation (U:toast.rs:618-622).
func (t *Toast) TransitionStatus(status ToastTransitionStatus) *Toast {
	t.transitionStatus = status
	return t
}

// Status returns the lifecycle phase (U:toast.rs:624-627).
func (t *Toast) Status() ToastTransitionStatus { return t.transitionStatus }

// RenderOnce implements gpui.RenderOnce (U:toast.rs:655-662).
func (t *Toast) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return t.render().IntoElement()
}

// render builds the toast's root div.
func (t *Toast) render() *gpui.DivElement {
	d := t.base.Role(gpui.RoleAlert)
	d.Extend(t.TakeChildren())
	style := t.Style()
	return StyledExtOf(d).RefineStyle(&style)
}
