package base

// The appear motion shared by every Plot (U:plot/appear.rs): how far its
// data marks have drawn in since the plot was first painted. Behavior
// only: a plot decides what appearing looks like and a styled layer
// projects the timing through PlotMotion.
//
// The appear lives in the plot's element state, so a plot that stops
// being painted forgets it and draws in again when it comes back. A
// PlotAppearScope around the region remembers which plots have finished,
// for as long as the scope itself is painted.
//
// Core seams: the scope's memory is element state
// (gpui.WithElementState, R:window.rs:4140); the appear itself is a
// Presence sample (motion_presence.go); the theme is read with
// gpui.TryGlobal (R:app.rs:2120 try_global).
//
// Go adaptations (recorded deviations):
//
//   - The thread-local SCOPES stack (U:plot/appear.rs:86-90) is a stack
//     per window behind a mutex, so concurrently driven test apps do not
//     share it.
//   - The Appeared map is keyed by GlobalElementID.String(): the Go
//     GlobalElementID holds a slice and is not comparable.
//   - `PlotAppearScope::new(id: impl Into<ElementId>, child: impl
//     IntoElement)` is NewPlotAppearScope over gpui.IntoElementID and
//     gpui.IntoElement.

import (
	"sync"

	"github.com/hoangvu12/gpui-go/gpui"
)

// PlotAppear is how far a plot's data marks have appeared this frame,
// handed to Plot.Appear (U:plot/appear.rs:24-36). Base's default duration
// is zero, and reduced motion skips it, so a plot is then complete from
// its first frame.
type PlotAppear struct {
	// time is the linear time through the appear, from 0 to 1.
	time   float32
	easing Easing
}

// PlotAppearComplete is a finished appear: every mark is complete
// (U:plot/appear.rs:39-45).
func PlotAppearComplete() PlotAppear {
	return PlotAppear{time: 1, easing: EasingLinear()}
}

// Progress is how far the whole plot has appeared, from 0 to 1, eased
// (U:plot/appear.rs:47-55).
func (a PlotAppear) Progress() float32 {
	// Charts read this per mark on every frame, long after the appear is
	// done, so a finished appear skips sampling the curve.
	if a.time >= 1 {
		return 1
	}
	return a.easing.Sample(a.time)
}

// IsAppearing reports whether the appear is still running
// (U:plot/appear.rs:57-60).
func (a PlotAppear) IsAppearing() bool { return a.time < 1 }

// Staggered is how far mark index of count has appeared, from 0 to 1,
// eased (U:plot/appear.rs:62-76). The marks start one after another
// across the first spread of the appear and each runs for the rest of it;
// a spread of 0 moves every mark together.
func (a PlotAppear) Staggered(index, count int, spread float32) float32 {
	if count <= 1 || a.time >= 1 {
		return a.Progress()
	}
	spread = clamp32(spread, 0, 0.95)
	start := spread * float32(min(index, count-1)) / float32(count-1)
	time := clamp32((a.time-start)/(1-spread), 0, 1)
	return a.easing.Sample(time)
}

// plotAppearKey is the element-state key of a plot's appear, within the
// plot's scope (U:plot/appear.rs:79-80).
const plotAppearKey = "__plot-appear"

// plotAppeared is the plots a PlotAppearScope has seen finish appearing,
// by global id, with the generation that finished
// (U:plot/appear.rs:82-84).
type plotAppeared struct {
	byID map[string]uint64
}

func newPlotAppeared() *plotAppeared { return &plotAppeared{byID: map[string]uint64{}} }

// plotAppearScopes are the scopes being laid out or painted, innermost
// last, per window (U:plot/appear.rs:86-90).
var plotAppearScopes = struct {
	sync.Mutex
	stacks map[*gpui.Window][]*plotAppeared
}{stacks: map[*gpui.Window][]*plotAppeared{}}

// plotWithinScope runs f with appeared as the innermost scope
// (U:plot/appear.rs:128-134).
func plotWithinScope[R any](w *gpui.Window, appeared *plotAppeared, f func() R) R {
	plotAppearScopes.Lock()
	plotAppearScopes.stacks[w] = append(plotAppearScopes.stacks[w], appeared)
	plotAppearScopes.Unlock()
	defer func() {
		plotAppearScopes.Lock()
		stack := plotAppearScopes.stacks[w]
		if len(stack) <= 1 {
			delete(plotAppearScopes.stacks, w)
		} else {
			plotAppearScopes.stacks[w] = stack[:len(stack)-1]
		}
		plotAppearScopes.Unlock()
	}()
	return f()
}

// plotCurrentScope is the innermost scope, if any
// (U:plot/appear.rs:212-215).
func plotCurrentScope(w *gpui.Window) *plotAppeared {
	plotAppearScopes.Lock()
	defer plotAppearScopes.Unlock()
	stack := plotAppearScopes.stacks[w]
	if len(stack) == 0 {
		return nil
	}
	return stack[len(stack)-1]
}

// PlotAppearScope remembers which plots inside it have finished
// appearing, so a plot painted again after a gap — a row a virtual list
// scrolled out of view and back — shows its data whole instead of drawing
// in again (U:plot/appear.rs:92-116). A plot is remembered by its global
// element id and its appear generation once its appear has finished; the
// memory is the scope's own element state and goes with it. The innermost
// scope wins. The scope takes no part in layout: it hands on its child's
// layout.
type PlotAppearScope struct {
	id       gpui.ElementID
	child    gpui.AnyElement
	hasChild bool
}

// NewPlotAppearScope remembers the appears of the plots in child under
// id, unique among its siblings (U:plot/appear.rs:119-126).
func NewPlotAppearScope[I gpui.IntoElementID](id I, child gpui.IntoElement) *PlotAppearScope {
	return &PlotAppearScope{id: gpui.ElementIDOf(id), child: child.IntoElement(), hasChild: true}
}

// IntoElement implements gpui.IntoElement (U:plot/appear.rs:137-143).
func (s *PlotAppearScope) IntoElement() gpui.AnyElement {
	return gpui.CustomElement[plotAppearScopeLayout, struct{}](s)
}

// plotAppearScopeLayout is the request-layout state: the child and the
// scope's memory (U:plot/appear.rs:146).
type plotAppearScopeLayout struct {
	child    gpui.AnyElement
	hasChild bool
	appeared *plotAppeared
}

// ID implements gpui.Element (U:plot/appear.rs:149-151).
func (s *PlotAppearScope) ID() (gpui.ElementID, bool) { return s.id, true }

// SourceLocation implements gpui.Element (U:plot/appear.rs:153-155).
func (s *PlotAppearScope) SourceLocation() *gpui.SourceLocation { return nil }

// RequestLayout implements gpui.Element (U:plot/appear.rs:157-177).
func (s *PlotAppearScope) RequestLayout(global *gpui.GlobalElementID, _ *gpui.InspectorElementID, w *gpui.Window, app *gpui.App) (gpui.LayoutID, plotAppearScopeLayout) {
	var appeared *plotAppeared
	if global != nil {
		appeared = gpui.WithElementState(w, global, func(state **plotAppeared, _ *gpui.Window) (*plotAppeared, *plotAppeared) {
			a := newPlotAppeared()
			if state != nil && *state != nil {
				a = *state
			}
			return a, a
		})
	} else {
		appeared = newPlotAppeared()
	}
	layout := plotAppearScopeLayout{child: s.child, hasChild: s.hasChild, appeared: appeared}
	s.hasChild = false
	id := plotWithinScope(w, appeared, func() gpui.LayoutID {
		if layout.hasChild {
			return layout.child.RequestLayout(w, app)
		}
		id, err := gpui.RequestElementLayout(w, gpui.DefaultStyle())
		if err != nil {
			panic("base: PlotAppearScope request_layout: " + err.Error())
		}
		return id
	})
	return id, layout
}

// Prepaint implements gpui.Element (U:plot/appear.rs:179-194): a virtual
// list lays its rows out here, so their plots appear here.
func (s *PlotAppearScope) Prepaint(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, _ gpui.Bounds, layout *plotAppearScopeLayout, w *gpui.Window, app *gpui.App) struct{} {
	if layout.hasChild {
		plotWithinScope(w, layout.appeared, func() struct{} {
			layout.child.Prepaint(w, app)
			return struct{}{}
		})
	}
	return struct{}{}
}

// Paint implements gpui.Element (U:plot/appear.rs:196-209).
func (s *PlotAppearScope) Paint(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, _ gpui.Bounds, layout *plotAppearScopeLayout, _ *struct{}, w *gpui.Window, app *gpui.App) {
	if layout.hasChild {
		plotWithinScope(w, layout.appeared, func() struct{} {
			layout.child.Paint(w, app)
			return struct{}{}
		})
	}
}

// plotTrackAppear samples the appear of the plot painting under the
// window's current element id (U:plot/appear.rs:217-257). A new
// generation starts it over, and one the innermost PlotAppearScope saw
// finish is complete at once.
func plotTrackAppear(global *gpui.GlobalElementID, generation uint64, w *gpui.Window, app *gpui.App) PlotAppear {
	scope := plotCurrentScope(w)
	key := global.String()
	if scope != nil {
		if g, ok := scope.byID[key]; ok && g == generation {
			return PlotAppearComplete()
		}
	}
	theme, ok := gpui.TryGlobal[*Theme](app)
	if !ok || theme == nil {
		return PlotAppearComplete()
	}
	policy := theme.Plot.Motion().Appear()
	easing := policy.Curve()
	// Presence keeps the linear time so the marks can each ease over
	// their own slice of it; see PlotAppear.Staggered.
	sample := NewPresence(TransitionIDWithChannel(gpui.IntegerElementID(generation), plotAppearKey), true).
		Transition(policy.Easing(EasingLinear())).
		Sample(w, app)
	if sample.Progress >= 1 && scope != nil {
		scope.byID[key] = generation
	}
	return PlotAppear{time: sample.Progress, easing: easing}
}
