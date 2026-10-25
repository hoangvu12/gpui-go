package base

// Port of U:nav_stack.rs: a stack of views, one visible at a time, with
// a transition between them.

import (
	"github.com/hoangvu12/gpui-go/gpui"
)

// NavOperation is what a running transition is doing, in Qt's terms
// (U:nav_stack.rs:14-26). The operation decides paint order and lets a
// renderer move a pushed view differently from a popped one.
type NavOperation uint8

const (
	// NavOperationPush: a view was pushed over the previous top.
	NavOperationPush NavOperation = iota
	// NavOperationPop: the top was popped, revealing the view below.
	NavOperationPop
	// NavOperationReplace: the top was swapped for another view.
	NavOperationReplace
)

// NavMotion is whether one change runs the NavStack's transition, as
// UIKit's `animated:` and Qt's `StackView.Immediate` decide per call
// (U:nav_stack.rs:28-39). A NavStack without a transition is always
// immediate, whatever is passed here.
type NavMotion uint8

const (
	// NavMotionAnimated runs the transition.
	NavMotionAnimated NavMotion = iota
	// NavMotionImmediate switches views on the spot.
	NavMotionImmediate
)

// NavStackEvent is emitted by NavStackState after the stack changed
// (U:nav_stack.rs:41-49).
type NavStackEvent uint8

const (
	NavStackPushed NavStackEvent = iota
	NavStackPopped
	NavStackForwarded
	NavStackReplaced
	NavStackCleared
)

// NavStackEvents is the NavStackState event descriptor
// (U:nav_stack.rs:98).
var NavStackEvents = gpui.DefineEvent[NavStackState, NavStackEvent]()

// navTransit is the view leaving the stack, kept mounted until its exit
// transition finishes (U:nav_stack.rs:51-61). The element samples that
// transition as a Presence keyed by the view, so an interrupted change
// reverses from where it is.
type navTransit struct {
	outgoing gpui.View
	// index is the position the outgoing view had on the stack.
	index     int
	operation NavOperation
	motion    NavMotion
}

// navEntry is a view on the stack; a page pushed twice is two separate
// entries (U:nav_stack.rs:86-96).
type navEntry struct {
	view gpui.View
}

// NavStackState is a stack of views, one visible at a time, with the
// pages popped off it kept for Forward (U:nav_stack.rs:63-84).
//
// Underneath it is a History of views from the root through the current
// page. A popped page becomes a forward entry until a push discards that
// forward branch. After a push, pop or replace, the outgoing view stays
// mounted until the NavStack's transition finishes. Pop keeps the root;
// Clear empties the stack.
type NavStackState struct {
	history *History[navEntry]
	transit *navTransit
}

// NewNavStackState is NavStackState::new (U:nav_stack.rs:100-112).
func NewNavStackState() NavStackState {
	return NavStackState{history: NewHistory[navEntry]()}
}

// Depth returns the number of views on the stack (U:nav_stack.rs:114-117).
func (s *NavStackState) Depth() int { return len(s.history.Entries()) }

// IsEmpty reports whether the stack is empty (U:nav_stack.rs:119-121).
func (s *NavStackState) IsEmpty() bool { return s.Depth() == 0 }

// Current returns the view on top of the stack, which is the one shown
// once any transition has finished (U:nav_stack.rs:123-127).
func (s *NavStackState) Current() (gpui.View, bool) {
	entry, ok := s.history.Current()
	return entry.view, ok
}

// Views returns every view on the stack, root first
// (U:nav_stack.rs:129-132).
func (s *NavStackState) Views() []gpui.View { return navViews(s.history.Entries()) }

// ForwardViews returns the views popped since the last push, nearest
// first: the one Forward would bring back is the first
// (U:nav_stack.rs:134-138).
func (s *NavStackState) ForwardViews() []gpui.View { return navViews(s.history.ForwardEntries()) }

func navViews(entries []navEntry) []gpui.View {
	views := make([]gpui.View, len(entries))
	for i, e := range entries {
		views[i] = e.view
	}
	return views
}

// Push pushes view on top of the stack and discards the forward views
// (U:nav_stack.rs:140-155). Into an empty stack this is immediate; over an
// existing top it starts a Push transition, unless motion is Immediate.
func (s *NavStackState) Push(view gpui.View, motion NavMotion, cx *gpui.Context[NavStackState]) {
	outgoing := s.top()
	s.history.Push(navEntry{view: view})
	s.finish(outgoing, NavOperationPush, motion, NavStackPushed, cx)
}

// Pop pops the top view and returns it, starting a Pop transition to the
// view below; the view waits in ForwardViews. The root is never popped:
// this returns false at a depth of one or less (U:nav_stack.rs:157-176).
func (s *NavStackState) Pop(motion NavMotion, cx *gpui.Context[NavStackState]) (gpui.View, bool) {
	if s.Depth() <= 1 {
		return nil, false
	}
	popped := s.top()
	if popped == nil {
		return nil, false
	}
	if _, ok := s.history.Back(); !ok {
		return nil, false
	}
	s.finish(popped, NavOperationPop, motion, NavStackPopped, cx)
	return popped.view, true
}

// PopToRoot pops every view above the root in one Pop transition from the
// previous top, and returns them root-side first (U:nav_stack.rs:178-204).
func (s *NavStackState) PopToRoot(motion NavMotion, cx *gpui.Context[NavStackState]) []gpui.View {
	outgoing := s.top()
	var popped []gpui.View
	for s.Depth() > 1 {
		view, ok := s.Current()
		if !ok {
			break
		}
		if _, ok := s.history.Back(); !ok {
			break
		}
		popped = append(popped, view)
	}
	if len(popped) == 0 {
		return popped
	}
	s.finish(outgoing, NavOperationPop, motion, NavStackPopped, cx)
	for i, j := 0, len(popped)-1; i < j; i, j = i+1, j-1 {
		popped[i], popped[j] = popped[j], popped[i]
	}
	return popped
}

// Forward brings back the most recently popped view, starting a Push
// transition over the current top, and returns it; false when nothing has
// been popped since the last push (U:nav_stack.rs:206-220).
func (s *NavStackState) Forward(motion NavMotion, cx *gpui.Context[NavStackState]) (gpui.View, bool) {
	outgoing := s.top()
	entry, ok := s.history.Forward()
	if !ok {
		return nil, false
	}
	s.finish(outgoing, NavOperationPush, motion, NavStackForwarded, cx)
	return entry.view, true
}

// Replace swaps the top view for view and returns the one replaced,
// starting a Replace transition; the forward views are kept. On an empty
// stack this is a push (U:nav_stack.rs:222-244).
func (s *NavStackState) Replace(view gpui.View, motion NavMotion, cx *gpui.Context[NavStackState]) (gpui.View, bool) {
	replaced := s.top()
	if replaced == nil {
		s.Push(view, motion, cx)
		return nil, false
	}
	s.history.ReplaceCurrent(navEntry{view: view})
	s.finish(replaced, NavOperationReplace, motion, NavStackReplaced, cx)
	return replaced.view, true
}

// Clear empties the stack and the forward views immediately, abandoning
// any running transition (U:nav_stack.rs:246-253).
func (s *NavStackState) Clear(cx *gpui.Context[NavStackState]) {
	s.history.Clear()
	s.transit = nil
	NavStackEvents.Emit(cx, NavStackCleared)
	cx.Notify()
}

// navTop is the top view with its position (U:nav_stack.rs:255-259).
type navTop struct {
	view  gpui.View
	index int
}

// top returns the top view with its position, before an operation moves
// it (U:nav_stack.rs:255-259).
func (s *NavStackState) top() *navTop {
	depth := s.Depth()
	if depth == 0 {
		return nil
	}
	view, ok := s.Current()
	if !ok {
		return nil
	}
	return &navTop{view: view, index: depth - 1}
}

// finish records the change: outgoing stays mounted until its exit
// transition finishes. A change already in transit is superseded, so at
// most one outgoing view is ever mounted (U:nav_stack.rs:261-281).
func (s *NavStackState) finish(outgoing *navTop, operation NavOperation, motion NavMotion, event NavStackEvent, cx *gpui.Context[NavStackState]) {
	s.transit = nil
	if outgoing != nil {
		s.transit = &navTransit{outgoing: outgoing.view, index: outgoing.index, operation: operation, motion: motion}
	}
	NavStackEvents.Emit(cx, event)
	cx.Notify()
}

// NavItemRenderer renders one mounted view (U:nav_stack.rs:284).
type NavItemRenderer func(page *NavPage, w *gpui.Window, app *gpui.App) gpui.AnyElement

// NavStack is an unstyled host for a NavStackState
// (U:nav_stack.rs:286-302).
//
// The container is positioned so that the two views of a transition can
// overlap; each mounted view is handed to the Item renderer as a NavPage
// that already fills the container. Without a Transition the stack
// switches views immediately, as it also does under reduced motion.
type NavStack struct {
	gpui.Styled[*NavStack]

	base       *gpui.DivElement
	state      gpui.Entity[NavStackState]
	transition *Transition
	renderItem NavItemRenderer
}

// NewNavStack is NavStack::new (U:nav_stack.rs:305-313).
func NewNavStack(state gpui.Entity[NavStackState]) *NavStack {
	// Gap 2b: the plain upstream div() is a block.
	return gpui.BindStyled(&NavStack{base: gpui.Div().Block(), state: state})
}

// Transition sets the timing every push, pop and replace runs under
// (U:nav_stack.rs:315-319).
func (n *NavStack) Transition(transition Transition) *NavStack {
	n.transition = &transition
	return n
}

// Item renders each mounted view. The page is already positioned to fill
// the container; refine it to move or fade the view by its phase and
// progress, then return it (U:nav_stack.rs:321-330).
func (n *NavStack) Item(render NavItemRenderer) *NavStack {
	n.renderItem = render
	return n
}

// RenderOnce implements gpui.RenderOnce (U:nav_stack.rs:339-450).
func (n *NavStack) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	type snapshot struct {
		current    gpui.View
		hasCurrent bool
		depth      int
		transit    *navTransit
	}
	snap := n.state.Read(app, func(s *NavStackState, _ *gpui.App) snapshot {
		current, ok := s.Current()
		var transit *navTransit
		if s.transit != nil {
			t := *s.transit
			transit = &t
		}
		return snapshot{current, ok, s.Depth(), transit}
	})
	immediate := app.ReduceMotion() || n.transition == nil ||
		(snap.transit != nil && snap.transit.motion == NavMotionImmediate)
	transition := NewTransition(0)
	if !immediate {
		transition = *n.transition
	}

	// The outgoing view's presence is the change's clock: its exit runs
	// 1 → 0, reversing if the change is interrupted, and both pages of the
	// change read their progress from it (U:nav_stack.rs:362-375).
	var change *navTransit
	var changeProgress float32
	if snap.transit != nil {
		sample := NewPresence(navPageID(snap.transit.outgoing), false).Transition(transition).Sample(w, app)
		if sample.ShouldRender() {
			change = snap.transit
			changeProgress = 1 - sample.Progress
		} else {
			n.state.Update(app, func(s *NavStackState, _ *gpui.Context[NavStackState]) { s.transit = nil })
		}
	}

	// The current view's presence is sampled too, so a view brought back
	// by forward or revealed by pop starts its next exit from present. With
	// nothing changing it settles on the spot; the root does not animate in
	// (U:nav_stack.rs:377-390).
	if snap.hasCurrent {
		currentTransition := NewTransition(0)
		if change != nil {
			currentTransition = transition
		}
		NewPresence(navPageID(snap.current), true).Transition(currentTransition).Sample(w, app)
	}

	items := make([]*NavPage, 0, 3)
	if snap.hasCurrent {
		index := snap.depth - 1
		if change != nil {
			operation := change.operation
			current := newNavPage(snap.current, index, PresencePhaseEntering, &operation, changeProgress)
			outgoing := newNavPage(change.outgoing, change.index, PresencePhaseExiting, &operation, changeProgress)
			// A pushed or replacing view paints over what it covers; a
			// popped view paints over what it reveals.
			switch operation {
			case NavOperationPush, NavOperationReplace:
				items = append(items, outgoing, current)
			case NavOperationPop:
				items = append(items, current, outgoing)
			}
		} else {
			items = append(items, newNavPage(snap.current, index, PresencePhasePresent, nil, 1))
		}
	}
	changing := len(items) > 1

	style := n.Style()
	d := StyledExtOf(n.base.Relative()).RefineStyle(&style)
	for _, item := range items {
		if n.renderItem != nil {
			d.Child(n.renderItem(item, w, app))
		} else {
			d.Child(gpui.OwnRecipe(item))
		}
	}
	// Neither page takes pointer input while the change runs: the
	// outgoing one is on its way out, and the incoming one is not yet
	// where it will be.
	if changing {
		d.Child(gpui.Div().Flex().Absolute().Inset0().Occlude())
	}
	return d.IntoElement()
}

// navPageID is a page's presence key (U:nav_stack.rs:452-454).
func navPageID(view gpui.View) gpui.ElementID {
	id, _ := view.EntityID()
	return gpui.NamedIntegerElementID("nav-stack", uint64(id))
}

// NavPage is one mounted view of a NavStack, handed to the item renderer
// (U:nav_stack.rs:456-472).
//
// The page fills its container. Phase says whether the view is arriving,
// settled, or leaving; Operation says which change; Progress runs from 0
// to 1 over the transition, already eased, and is shared by both views of
// one change.
type NavPage struct {
	gpui.Styled[*NavPage]

	base      *gpui.DivElement
	view      gpui.View
	index     int
	phase     PresencePhase
	operation *NavOperation
	progress  float32
}

// newNavPage is NavPage::new (U:nav_stack.rs:474-491).
func newNavPage(view gpui.View, index int, phase PresencePhase, operation *NavOperation, progress float32) *NavPage {
	// Gap 2b: the plain upstream div() is a block.
	return gpui.BindStyled(&NavPage{
		base:      gpui.Div().Block(),
		view:      view,
		index:     index,
		phase:     phase,
		operation: operation,
		progress:  progress,
	})
}

// View returns the page's view (U:nav_stack.rs:493-495).
func (p *NavPage) View() gpui.View { return p.view }

// Index returns the view's position on the stack, root first; a view on
// its way out keeps the position it had (U:nav_stack.rs:497-501).
func (p *NavPage) Index() int { return p.index }

// Phase returns the page's presence phase (U:nav_stack.rs:503-505).
func (p *NavPage) Phase() PresencePhase { return p.phase }

// Operation returns the change in progress, false once the stack has
// settled (U:nav_stack.rs:507-510).
func (p *NavPage) Operation() (NavOperation, bool) {
	if p.operation == nil {
		return 0, false
	}
	return *p.operation, true
}

// Progress returns the shared, eased change progress
// (U:nav_stack.rs:512-514).
func (p *NavPage) Progress() float32 { return p.progress }

// RenderOnce implements gpui.RenderOnce (U:nav_stack.rs:523-531).
func (p *NavPage) RenderOnce(*gpui.Window, *gpui.App) gpui.AnyElement {
	style := p.Style()
	d := p.base.Absolute().Inset0().Child(p.view)
	return StyledExtOf(d).RefineStyle(&style).IntoElement()
}
