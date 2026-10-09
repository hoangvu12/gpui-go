// Package listspec holds ticket14's retained-state and scroll/list
// tests: the keyed element-state retention lifecycle (window.rs
// with_element_state / accessed_element_states / Frame::finish), the
// abandoned-build publication contract, the scroll state and the
// list/uniform-list visible-range behavior.
//
// The exact expectations derive from the pinned CE semantics:
// crates/gpui/src/window.rs (with_element_state's take/put, the
// accessed-keys carry-over in Frame::finish, the state type check),
// crates/gpui/src/elements/list.rs and uniform_list.rs (visible
// ranges, scroll clamping, stable identity paths).
package listspec

import (
	"testing"

	"gpui-go/gpui"
)

// ---------------------------------------------------------------------------
// The retained element-state lifecycle
// ---------------------------------------------------------------------------

// stateView is a corpus root whose render optionally includes a keyed
// probe element that retains an int state across frames.
type stateView struct {
	// includeProbe controls whether the probe draws this frame.
	includeProbe bool
	// probeView observes the state passed to the probe's closure.
	probeView *int
}

// Render implements Render[stateView].
func (v *stateView) Render(w *gpui.Window, cx *gpui.Context[stateView]) gpui.AnyElement {
	if !v.includeProbe {
		return gpui.Empty()
	}
	return CustomProbe(func(state *int, w *gpui.Window) (struct{}, int) {
		if state == nil {
			*v.probeView = 0
		} else {
			*v.probeView = *state
		}
		next := *v.probeView + 1
		return struct{}{}, next
	})
}

// probeElement is a custom element that reads and writes a keyed
// element state during request layout (the reference elements' state
// access phase).
type probeElement struct {
	access func(state *int, w *gpui.Window) (struct{}, int)
}

// probeState carries the probe's last layout id (unused geometry).
type probeState struct {
	layoutID gpui.LayoutID
}

// ID implements Element: the probe is named, so its state is keyed by
// the identity path.
func (e *probeElement) ID() (gpui.ElementID, bool) {
	return gpui.NameElementID("probe"), true
}

// SourceLocation implements Element.
func (e *probeElement) SourceLocation() *gpui.SourceLocation { return nil }

// RequestLayout implements Element: the state access happens here.
func (e *probeElement) RequestLayout(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, w *gpui.Window, app *gpui.App) (gpui.LayoutID, probeState) {
	gpui.WithElementState(w, global, e.access)
	id, err := gpui.RequestElementLayout(w, gpui.DefaultStyle())
	if err != nil {
		panic(err)
	}
	return id, probeState{layoutID: id}
}

// Prepaint implements Element.
func (e *probeElement) Prepaint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *probeState, w *gpui.Window, app *gpui.App) struct{} {
	return struct{}{}
}

// Paint implements Element.
func (e *probeElement) Paint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *probeState, prepaint *struct{}, w *gpui.Window, app *gpui.App) {
}

// LayoutNodeStyle implements Element.
func (e *probeElement) LayoutNodeStyle() (gpui.Style, bool) { return gpui.DefaultStyle(), true }

// CustomProbe wraps the probe's access closure as an AnyElement.
func CustomProbe(access func(state *int, w *gpui.Window) (struct{}, int)) gpui.AnyElement {
	return gpui.CustomElement[probeState, struct{}](&probeElement{access: access})
}

// drawStateView draws one frame of the state corpus and returns the
// observed state.
func drawStateView(t *testing.T, window *gpui.Window, app *gpui.App, view gpui.Entity[stateView]) int {
	t.Helper()
	view.Update(app, func(v *stateView, cx *gpui.Context[stateView]) {})
	if _, err := gpui.DrawWindowFrame(window); err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}
	return view.Read(app, func(v *stateView, _ *gpui.App) int { return *v.probeView })
}

// TestElementStateDroppedWhenElementStopsDrawing checks the retention
// lifecycle: state survives while the element draws, is dropped when
// it stops drawing for a full frame, and reinsertion starts fresh.
func TestElementStateDroppedWhenElementStopsDrawing(t *testing.T) {
	ta := gpui.NewTestApp()
	app := ta.App()
	window := gpui.NewTestWindow(app, gpui.Size{Width: 200, Height: 200})
	observed := 0
	view := gpui.NewEntity(app, window.Scope(), func(v *stateView, cx *gpui.Context[stateView]) {
		v.includeProbe = true
		v.probeView = &observed
	})
	window.SetRootView(gpui.ViewOf(view))

	// Frame 1: fresh state (nil) -> observed 0, retained 1.
	if got := drawStateView(t, window, app, view); got != 0 {
		t.Fatalf("first frame observed %d, want 0 (fresh state)", got)
	}
	// Frame 2: retained state 1 -> observed 1, retained 2.
	if got := drawStateView(t, window, app, view); got != 1 {
		t.Fatalf("second frame observed %d, want 1 (retained state)", got)
	}
	// Frame 3: the probe stops drawing — its state is dropped after
	// the frame completes.
	view.Update(app, func(v *stateView, cx *gpui.Context[stateView]) { v.includeProbe = false })
	if _, err := gpui.DrawWindowFrame(window); err != nil {
		t.Fatalf("frame without the probe: %v", err)
	}
	global := &gpui.GlobalElementID{Path: []gpui.ElementID{
		gpui.ViewElementID(view.EntityID()),
		gpui.NameElementID("probe"),
	}}
	if _, ok := gpui.ElementStateOf[int](window, global); ok {
		t.Fatal("the stopped element's state must be dropped after the frame")
	}
	// Frame 4: the probe returns — the state starts fresh.
	view.Update(app, func(v *stateView, cx *gpui.Context[stateView]) { v.includeProbe = true })
	if got := drawStateView(t, window, app, view); got != 0 {
		t.Fatalf("reinserted probe observed %d, want 0 (fresh state after reinsertion)", got)
	}
}

// abandonView draws the good corpus until it is flipped to fail,
// after which it mutates app state during render and fails the build
// (a panicking child element).
type abandonView struct {
	// fail flips the corpus to the panicking child.
	fail bool
	// mutations counts the renders (app mutations that persist).
	mutations *int
}

// Render implements Render[abandonView].
func (v *abandonView) Render(w *gpui.Window, cx *gpui.Context[abandonView]) gpui.AnyElement {
	*v.mutations++
	if v.fail {
		return gpui.Div().Child(gpui.Empty()).Child(panickingElement()).IntoElement()
	}
	return gpui.Div().Child("stable").IntoElement()
}

// panickingElement returns an element whose paint panics (the
// abandoned build).
func panickingElement() gpui.AnyElement {
	return gpui.CustomElement[struct{}, struct{}](&panicElement{})
}

type panicElement struct{}

// ID implements Element.
func (e *panicElement) ID() (gpui.ElementID, bool) { return gpui.ElementID{}, false }

// SourceLocation implements Element.
func (e *panicElement) SourceLocation() *gpui.SourceLocation { return nil }

// RequestLayout implements Element.
func (e *panicElement) RequestLayout(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, w *gpui.Window, app *gpui.App) (gpui.LayoutID, struct{}) {
	id, err := gpui.RequestElementLayout(w, gpui.DefaultStyle())
	if err != nil {
		panic(err)
	}
	return id, struct{}{}
}

// Prepaint implements Element.
func (e *panicElement) Prepaint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *struct{}, w *gpui.Window, app *gpui.App) struct{} {
	return struct{}{}
}

// Paint implements Element: the build fails here.
func (e *panicElement) Paint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *struct{}, prepaint *struct{}, w *gpui.Window, app *gpui.App) {
	panic("gpui: abandoned build (listspec)")
}

// LayoutNodeStyle implements Element.
func (e *panicElement) LayoutNodeStyle() (gpui.Style, bool) { return gpui.DefaultStyle(), true }

// TestAbandonedBuildPreservesPriorPublishedFrame checks the failure
// path: a build that fails after mutating app state preserves the
// previous published frame (draw count, scene) without rolling back
// the application mutation.
func TestAbandonedBuildPreservesPriorPublishedFrame(t *testing.T) {
	ta := gpui.NewTestApp()
	app := ta.App()
	window := gpui.NewTestWindow(app, gpui.Size{Width: 200, Height: 200})
	mutations := 0
	view := gpui.NewEntity(app, window.Scope(), func(v *abandonView, cx *gpui.Context[abandonView]) {
		v.mutations = &mutations
	})
	window.SetRootView(gpui.ViewOf(view))
	// Establish a published frame (the good corpus).
	scene, err := gpui.DrawWindowFrame(window)
	if err != nil {
		t.Fatalf("good frame: %v", err)
	}
	draws := gpui.WindowDrawCount(window)
	if mutations != 1 {
		t.Fatalf("the good frame's render mutations = %d, want 1", mutations)
	}

	// The abandoned build: the view mutates app state during render,
	// then a child panics in paint. The panic propagates; the test
	// recovers it.
	view.Update(app, func(v *abandonView, cx *gpui.Context[abandonView]) { v.fail = true })
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("the panicking element must panic the draw")
			}
		}()
		_, _ = gpui.DrawWindowFrame(window)
	}()

	// The prior published frame is preserved: the draw count and the
	// last scene are unchanged.
	if got := gpui.WindowDrawCount(window); got != draws {
		t.Fatalf("draw count after the abandoned build = %d, want %d (the failed frame is not published)", got, draws)
	}
	if last := gpui.LastDrawnScene(window); last != scene {
		t.Fatal("the last drawn scene must still be the previously published frame")
	}
	// The application mutation is NOT rolled back (the render during
	// the failed build counted).
	if mutations != 2 {
		t.Fatalf("app mutations after the failed build = %d, want 2 (mutations persist)", mutations)
	}
}

// staticTextView is a trivial good corpus.
type staticTextView struct{}

// Render implements Render[staticTextView].
func (v *staticTextView) Render(w *gpui.Window, cx *gpui.Context[staticTextView]) gpui.AnyElement {
	return gpui.Div().Child("stable").IntoElement()
}
