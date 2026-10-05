package authorspec

import (
	"strings"
	"testing"

	"gpui-go/gpui"
)

// This file ports the executed-behavior controls of the authoring round
// to the real runtime: the child-conversion precedence and nil
// diagnostics, the checked child insertion, the OwnRecipe single-use
// token, the reentrancy gate on destination mutation, and the retained
// element-state model across frames.
//
// The signature-level negative controls of the compile-only fixture
// (the 29 cases under .scratch/signature-validation/testdata/controls)
// stay compile-time by construction: the real package's signatures
// match the validated fixture, so those programs still fail to compile
// against gpui — that is signature conformance, which this file does
// not re-verify. What only the real runtime can show — the panic
// diagnostics, the error results, the preserved child lists, the
// retained state — is exercised here.

// mustPanic runs f, failing when it does not panic with a message
// containing want.
func mustPanic(t *testing.T, want, context string, f func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("%s: expected a panic containing %q", context, want)
		}
		message, ok := r.(string)
		if !ok {
			if err, isErr := r.(error); isErr {
				message = err.Error()
			} else {
				t.Fatalf("%s: panic value %v is neither string nor error", context, r)
			}
		}
		if !strings.Contains(message, want) {
			t.Fatalf("%s: panic %q, want it to contain %q", context, message, want)
		}
	}()
	f()
}

// TestChildConversionNilDiagnostics checks the nil diagnostics of the
// unchecked child path: an accidental nil (including a typed nil) at a
// child position panics with the parent/child context, never silently
// omitting the child.
func TestChildConversionNilDiagnostics(t *testing.T) {
	if testing.Short() {
		t.Skip("element runtime draws through the native layout services")
	}
	mustPanic(t, "nil child value", "Child(nil)", func() {
		gpui.Div().Child(nil)
	})
	mustPanic(t, "nil RenderOnce child value", "Child(typed nil recipe)", func() {
		var label *CountLabel
		gpui.Div().Child(label)
	})
	mustPanic(t, "nil child value", "Child(nil view)", func() {
		var view gpui.View
		gpui.Div().Child(view)
	})
	mustPanic(t, "zero AnyElement", "Child(zero AnyElement)", func() {
		gpui.Div().Child(gpui.AnyElement{})
	})
	mustPanic(t, "unsupported child value of type int", "Child(42)", func() {
		gpui.Div().Child(42)
	})
}

// TestTryIntoElementPrecedence checks the selected conversion
// precedence: AnyElement accepted, explicit IntoElement honored,
// strings converted, identity-bearing Views adapted, stateless
// RenderOnce recipes adapted, raw entities rejected.
func TestTryIntoElementPrecedence(t *testing.T) {
	if testing.Short() {
		t.Skip("element runtime draws through the native layout services")
	}
	app := gpui.NewTestApp().App()
	window := gpui.NewTestWindow(app, gpui.Size{Width: 100, Height: 60})
	counter := NewCounter(app, window.Scope(), window, 3)

	// AnyElement passes through.
	_, err := gpui.TryIntoElement(gpui.Text("hi"))
	if err != nil {
		t.Fatalf("AnyElement conversion: %v", err)
	}

	// IntoElement is honored (the div builder).
	elem, err := gpui.TryIntoElement(gpui.Div().Flex())
	if err != nil {
		t.Fatalf("IntoElement conversion: %v", err)
	}
	if kind, hasID := elem.ID(); hasID && kind.Kind == gpui.ElementIDView {
		t.Errorf("div conversion produced a view identity")
	}

	// Strings convert to text elements.
	elem, err = gpui.TryIntoElement("Count: 3")
	if err != nil {
		t.Fatalf("string conversion: %v", err)
	}

	// Identity-bearing Views adapt (the ViewOf seam).
	view := gpui.ViewOf(counter)
	elem, err = gpui.TryIntoElement(view)
	if err != nil {
		t.Fatalf("View conversion: %v", err)
	}
	if _, ok := elem.ID(); !ok {
		t.Errorf("view conversion lost the entity identity element id")
	}

	// Stateless recipes adapt.
	elem, err = gpui.TryIntoElement(NewCountLabel(3))
	if err != nil {
		t.Fatalf("RenderOnce conversion: %v", err)
	}
	if _, hasID := elem.ID(); hasID {
		t.Errorf("recipe conversion produced an identity element id")
	}

	// Raw entities are rejected at the child seam (use ViewOf).
	_, err = gpui.TryIntoElement(counter)
	if err == nil {
		t.Fatal("raw entity child conversion unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "unsupported child value") {
		t.Errorf("raw entity conversion error = %q, want the unsupported-child diagnostic", err)
	}
}

// TestTryChildrenPreservesDestinationOnFailure checks the checked child
// insertion: a failing batch preserves the destination's child list and
// releases its reservations (the selected contract).
func TestTryChildrenPreservesDestinationOnFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("element runtime draws through the native layout services")
	}
	div := gpui.Div().Flex().Child("first")
	before := divChildCount(t, div)

	// The batch fails on the invalid candidate.
	_, err := div.TryChildren("second", 42, "third")
	if err == nil {
		t.Fatal("TryChildren with an invalid candidate unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "child 1") {
		t.Errorf("TryChildren error = %q, want the child index in the diagnostic", err)
	}
	// The destination's child list is unchanged.
	if got := divChildCount(t, div); got != before {
		t.Errorf("child count after the failed batch = %d, want %d (the destination is preserved)", got, before)
	}

	// The typed slice path: valid slices attach, invalid ones fail
	// atomically.
	if _, err := div.TryChildrenSlice([]string{"a", "b"}); err != nil {
		t.Fatalf("TryChildrenSlice(valid) = %v", err)
	}
	after := divChildCount(t, div)
	if _, err := div.TryChildrenSlice([]any{"c", 7}); err == nil {
		t.Fatal("TryChildrenSlice with an invalid candidate unexpectedly succeeded")
	}
	if got := divChildCount(t, div); got != after {
		t.Errorf("child count after the failed slice batch = %d, want %d", got, after)
	}
}

// divChildCount renders one child through the children reflection
// helper (the destination's committed child count).
func divChildCount(t *testing.T, div *gpui.DivElement) int {
	t.Helper()
	return div.ChildCount()
}

// TestOwnRecipeSingleUseToken checks the OwnRecipe attachment token:
// the first child commit attaches the handle and a second attempt fails
// immediately, before expansion (the single-use misuse diagnostic), as
// does a duplicate of the same handle inside one batch.
func TestOwnRecipeSingleUseToken(t *testing.T) {
	if testing.Short() {
		t.Skip("element runtime draws through the native layout services")
	}
	owned := gpui.OwnRecipe(NewCountLabel(3))

	// The first commit attaches the handle.
	div := gpui.Div().Flex().Child(owned)
	if got := div.ChildCount(); got != 1 {
		t.Fatalf("child count after the first commit = %d, want 1", got)
	}

	// The attached handle fails on the second conversion (the fluent
	// path panics with the parent/child context).
	mustPanic(t, "already attached", "reusing an OwnRecipe handle", func() {
		gpui.Div().Flex().Child(owned)
	})

	// The checked path reports the same misuse as an error.
	_, err := gpui.TryIntoElement(owned)
	if err == nil || !strings.Contains(err.Error(), "already attached") {
		t.Fatalf("TryIntoElement(attached handle) = %v, want the already-attached error", err)
	}

	// A duplicate of one handle inside one batch is rejected before the
	// commit.
	fresh := gpui.OwnRecipe(NewCountLabel(4))
	mustPanic(t, "attached twice in one child batch", "duplicate OwnRecipe in one batch", func() {
		gpui.Div().Flex().Children(fresh, fresh)
	})
}

// TestConversionReentrancyGate checks the destination's child-mutation
// gate: a converter that reenters child insertion on the same
// destination during its conversion panics (the atomic
// reservation/commit contract), and the destination's child list is
// unchanged by the failed operation.
func TestConversionReentrancyGate(t *testing.T) {
	if testing.Short() {
		t.Skip("element runtime draws through the native layout services")
	}
	reentered := false
	div := gpui.Div().Flex().Child("first")
	before := div.ChildCount()
	// The converter's IntoElement mutates the SAME div during its
	// conversion (converter reentry on the destination).
	converter := reenteringConverter{target: div, armed: &reentered}
	mustPanic(t, "reentrant child mutation", "converter reentry on the destination", func() {
		div.Child(converter)
	})
	if !reentered {
		t.Fatal("the converter did not run (the gate rejected before the conversion started)")
	}
	if got := div.ChildCount(); got != before {
		t.Errorf("child count after the reentrant failure = %d, want %d (the destination is preserved)", got, before)
	}
}

// reenteringConverter is a child value whose conversion reenters the
// destination's child list.
type reenteringConverter struct {
	target *gpui.DivElement
	armed  *bool
}

// IntoElement implements gpui.IntoElement.
func (r reenteringConverter) IntoElement() gpui.AnyElement {
	*r.armed = true
	r.target.Child("reentered")
	return gpui.Empty()
}

// TestElementStateRetainedAcrossFrames checks the retained element
// state model: an identified element's state (keyed by its global id
// path) survives across frame draws of the same window, and a
// reentrant with-element-state access for the same key and type
// panics.
func TestElementStateRetainedAcrossFrames(t *testing.T) {
	if testing.Short() {
		t.Skip("element runtime draws through the native layout services")
	}
	app := gpui.NewTestApp().App()
	window := gpui.NewTestWindow(app, gpui.Size{Width: 100, Height: 60})
	var view gpui.Entity[textView]
	window.SetRootView(rootViewOfText(window, app, &view))

	// Draw twice through the retained-state element: the frame counter
	// keyed by the element's global id path accumulates across frames
	// (the state survives the frame boundary).
	for i := 0; i < 2; i++ {
		if _, err := gpui.DrawWindowFrame(window); err != nil {
			t.Fatalf("draw %d: %v", i+1, err)
		}
	}
	if got := gpui.ElementStateCount(window); got == 0 {
		t.Fatal("no retained element state after two frames (the state arena is empty)")
	}
	if got := gpui.WindowDrawCount(window); got != 2 {
		t.Errorf("draw count = %d, want 2", got)
	}
	// The third draw observes the accumulated retained state (3
	// request_layout phases on one retained slot).
	if _, err := gpui.DrawWindowFrame(window); err != nil {
		t.Fatalf("third draw: %v", err)
	}
	if got := probeFramesOf(app, view); got != 3 {
		t.Errorf("retained state after three draws = %d, want 3 (the state accumulates across frames)", got)
	}

	// The retained slot is keyed by the element's global id path: the
	// root view's entity id followed by the probe's own name id (the
	// reference identity-path keying).
	global := &gpui.GlobalElementID{Path: []gpui.ElementID{
		gpui.ViewElementID(view.EntityID()),
		gpui.NameElementID("probe"),
	}}
	if value, ok := gpui.ElementStateOf[int](window, global); !ok || value != 3 {
		t.Errorf("ElementStateOf through the identity path = (%d, %v), want (3, true)", value, ok)
	}

	// Reentrant access for the same key and state type panics.
	mustPanic(t, "reentrant call to with_element_state", "reentrant element state access", func() {
		gpui.WithElementState[int](window, global, func(state *int, w *gpui.Window) (struct{}, int) {
			gpui.WithElementState[int](window, global, func(state *int, w *gpui.Window) (struct{}, int) {
				return struct{}{}, 0
			})
			return struct{}{}, 0
		})
	})
}

// rootViewOfText builds a trivial root view for the runtime control
// tests (the draw path needs a root view), reporting the view entity
// through out.
func rootViewOfText(window *gpui.Window, app *gpui.App, out *gpui.Entity[textView]) gpui.View {
	view := gpui.NewEntity(app, window.Scope(), func(v *textView, cx *gpui.Context[textView]) {
		v.frames = &probeFrames{}
	})
	*out = view
	return gpui.ViewOf(view)
}

// probeFramesOf reads the probe's last observed retained frame count
// through the root view entity.
func probeFramesOf(app *gpui.App, view gpui.Entity[textView]) int {
	return view.Read(app, func(v *textView, _ *gpui.App) int {
		if v.frames == nil {
			return 0
		}
		return v.frames.count
	})
}

// textView is the trivial root view of the control tests, rendering the
// stateful probe element (an element that retains state across
// frames).
type textView struct {
	// frames receives the probe's retained frame count at each request.
	frames *probeFrames
}

// probeFrames carries the probe's retained frame count out of the draw.
type probeFrames struct {
	count int
}

// Render implements gpui.Render[textView].
func (v *textView) Render(w *gpui.Window, cx *gpui.Context[textView]) gpui.AnyElement {
	return gpui.CustomElement[probeLayout, struct{}](&statefulProbe{frames: v.frames})
}

// statefulProbe is a custom element retaining per-element state across
// frames through the window's element state arena, keyed by its global
// id path (the reference with_element_state model).
type statefulProbe struct {
	// frames receives the retained count at each request_layout.
	frames *probeFrames
}

// probeLayout is the probe's request-layout state.
type probeLayout struct {
	// retained is the retained state value observed at request time.
	retained int
}

// ID implements gpui.Element (an identity keys the retained state).
func (p *statefulProbe) ID() (gpui.ElementID, bool) {
	return gpui.NameElementID("probe"), true
}

// SourceLocation implements gpui.Element.
func (p *statefulProbe) SourceLocation() *gpui.SourceLocation { return nil }

// RequestLayout implements gpui.Element: request a layout node and
// retain a per-element frame counter keyed by the global id path.
func (p *statefulProbe) RequestLayout(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, w *gpui.Window, app *gpui.App) (gpui.LayoutID, probeLayout) {
	retained := gpui.WithElementState(w, global, func(state *int, w *gpui.Window) (probeLayout, int) {
		if state == nil {
			value := 1
			return probeLayout{retained: 1}, value
		}
		*state++
		return probeLayout{retained: *state}, *state
	})
	id, err := gpui.RequestElementLayout(w, gpui.DefaultStyle())
	if err != nil {
		panic(err)
	}
	if p.frames != nil {
		p.frames.count = retained.retained
	}
	return id, retained
}

// Prepaint implements gpui.Element.
func (p *statefulProbe) Prepaint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *probeLayout, w *gpui.Window, app *gpui.App) struct{} {
	return struct{}{}
}

// Paint implements gpui.Element.
func (p *statefulProbe) Paint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *probeLayout, prepaint *struct{}, w *gpui.Window, app *gpui.App) {
}

// LayoutNodeStyle implements the root-stretch report.
func (p *statefulProbe) LayoutNodeStyle() (gpui.Style, bool) { return gpui.DefaultStyle(), true }
