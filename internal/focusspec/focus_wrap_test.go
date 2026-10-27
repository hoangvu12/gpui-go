//go:build windows

package focusspec

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Browser-core ticket 79 (the app-level Tab wrap switch): with
// App.SetTabWrap(true) the sequential cycle's boundary wraps — Tab past
// the last stop focuses the first, Shift+Tab before the first focuses
// the last — while the default (the flag off, the zero value) keeps
// ticket 14's Chrome boundary: the pass-out blur. These tests pin both
// halves on the same corpus: the wrap respects Inert subtrees (an
// inert element never registers a stop, so the wrap cannot land in
// one), the ordered slice's positive-index and TabGroup order, the
// excluded element's tree-order arm, and the nothing-focusable case.

// wrapView renders the wrap corpus, in tree order: an inert head
// (Inert, tabindex 1, containing a focusable child — it would sort
// FIRST of the cycle if inertness did not suppress the registration),
// z1 (0), q (1), p (2), a tab group with e and f, z2 (0), an inert
// tail (tabindex 5 — it would sort LAST if registered), and n (-1,
// focusable but excluded) at the tree's end. The cycle the walk must
// visit is q, p, z1, z2, e, f — positives ascending, then the zero
// bucket in tree order, the group after its zero-ranked siblings, the
// inert head/tail and n never reached.
type wrapView struct {
	ihead, ic, z1, q, p, e, f, z2, itail, n gpui.FocusHandle
}

func (v *wrapView) Render(w *gpui.Window, cx *gpui.Context[wrapView]) gpui.AnyElement {
	return gpui.Div().Flex().
		KeyContext("Root").
		Child(gpui.Div().Flex().KeyContext("InertHead").TrackFocus(v.ihead).Inert().
			Child(gpui.Div().Flex().KeyContext("InertChild").TrackFocus(v.ic))).
		Child(gpui.Div().Flex().KeyContext("Row").TrackFocus(v.z1)).
		Child(gpui.Div().Flex().KeyContext("Row").TrackFocus(v.q)).
		Child(gpui.Div().Flex().KeyContext("Row").TrackFocus(v.p)).
		Child(gpui.Div().Flex().KeyContext("Group").TabGroup().
			Child(gpui.Div().Flex().TrackFocus(v.e)).
			Child(gpui.Div().Flex().TrackFocus(v.f))).
		Child(gpui.Div().Flex().KeyContext("Row").TrackFocus(v.z2)).
		Child(gpui.Div().Flex().KeyContext("InertTail").TrackFocus(v.itail).Inert()).
		Child(gpui.Div().Flex().KeyContext("Row").TrackFocus(v.n)).
		IntoElement()
}

// wrapCorpus is one drawn wrap corpus: the app (wrap on or off), the
// test window and the view's handles.
type wrapCorpus struct {
	app    *gpui.App
	window *gpui.Window
	view   *wrapView
}

// newWrapCorpus builds the corpus with the app's Tab wrap flag set to
// wrap (browser-core ticket 79's opt-in) or left at the default.
func newWrapCorpus(t *testing.T, wrap bool) *wrapCorpus {
	t.Helper()
	ta := gpui.NewTestApp()
	if wrap {
		ta = ta.WithTabWrap(true)
	}
	app := ta.App()
	window := gpui.NewTestWindow(app, gpui.Size{Width: 320, Height: 200})
	v := &wrapView{
		ihead: gpui.NewFocusHandle(window).TabStop(true).TabIndex(1),
		ic:    gpui.NewFocusHandle(window).TabStop(true),
		z1:    gpui.NewFocusHandle(window).TabStop(true),
		q:     gpui.NewFocusHandle(window).TabStop(true).TabIndex(1),
		p:     gpui.NewFocusHandle(window).TabStop(true).TabIndex(2),
		e:     gpui.NewFocusHandle(window).TabStop(true),
		f:     gpui.NewFocusHandle(window).TabStop(true),
		z2:    gpui.NewFocusHandle(window).TabStop(true),
		itail: gpui.NewFocusHandle(window).TabStop(true).TabIndex(5),
		n:     gpui.NewFocusHandle(window).TabStop(true).TabIndex(-1),
	}
	window.SetRootView(gpui.ViewOf(gpui.NewEntity(app, window.Scope(), func(state *wrapView, cx *gpui.Context[wrapView]) {
		*state = *v
	})))
	if _, err := gpui.DrawWindowFrame(window); err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}
	return &wrapCorpus{app: app, window: window, view: v}
}

// wrapStep presses Tab (or Shift+Tab through FocusPrev) and asserts the
// focused handle: want nil pins the pass-out (no focus).
func (c *wrapCorpus) wrapStep(t *testing.T, next bool, want *gpui.FocusHandle, what string) {
	t.Helper()
	if next {
		c.window.FocusNext()
	} else {
		c.window.FocusPrev()
	}
	got := c.window.Focused()
	if want == nil {
		if got.Ok {
			t.Fatalf("%s = handle %d, want no focus (the pass-out)", what, got.Handle.ID())
		}
		return
	}
	if !got.Ok || got.Handle.ID() != want.ID() {
		t.Fatalf("%s = %+v, want handle %d", what, got, want.ID())
	}
}

// TestTabWrapOffByDefaultKeepsThePassOut pins the default (browser-core
// ticket 79): the flag's zero value is OFF, so Tab past the last stop
// still passes focus out — the blur, ticket 14's Chrome boundary
// (focus_controller.cc:2216-2223, CanTakeFocus/TakeFocus) — and the
// next Tab restarts from the first stop; backwards past the first stop
// passes out too, and Shift+Tab from the body lands on the last.
func TestTabWrapOffByDefaultKeepsThePassOut(t *testing.T) {
	if testing.Short() {
		t.Skip("tab traversal draws through the native layout services")
	}
	c := newWrapCorpus(t, false)
	v := c.view

	// The forward walk: q, p, z1, z2, e, f, then the pass-out, then a
	// restart from the first stop.
	c.wrapStep(t, true, &v.q, "first focus-next")
	c.wrapStep(t, true, &v.p, "second focus-next")
	c.wrapStep(t, true, &v.z1, "third focus-next")
	c.wrapStep(t, true, &v.z2, "fourth focus-next")
	c.wrapStep(t, true, &v.e, "fifth focus-next (into the group)")
	c.wrapStep(t, true, &v.f, "sixth focus-next")
	c.wrapStep(t, true, nil, "focus-next past the last stop (no wrap by default)")
	c.wrapStep(t, true, &v.q, "focus-next from no focus")

	// The backward walk: backwards past the first stop passes out, and
	// Shift+Tab from the body lands on the last stop.
	c.wrapStep(t, false, nil, "focus-prev before the first stop (no wrap by default)")
	c.wrapStep(t, false, &v.f, "focus-prev from no focus")
}

// TestTabWrapOnWrapsForwardToTheFirst pins the wrap's forward half: Tab
// past the last stop focuses the cycle's first stop, and the walk then
// continues in the cycle's order.
func TestTabWrapOnWrapsForwardToTheFirst(t *testing.T) {
	if testing.Short() {
		t.Skip("tab traversal draws through the native layout services")
	}
	c := newWrapCorpus(t, true)
	v := c.view

	// Walk to the last stop, then wrap.
	c.wrapStep(t, true, &v.q, "first focus-next")
	c.wrapStep(t, true, &v.p, "second focus-next")
	c.wrapStep(t, true, &v.z1, "third focus-next")
	c.wrapStep(t, true, &v.z2, "fourth focus-next")
	c.wrapStep(t, true, &v.e, "fifth focus-next")
	c.wrapStep(t, true, &v.f, "sixth focus-next")
	c.wrapStep(t, true, &v.q, "focus-next past the last stop wraps to the first")
	c.wrapStep(t, true, &v.p, "the cycle continues after the wrap")
}

// TestTabWrapOnWrapsBackwardToTheLast pins the wrap's backward half:
// Shift+Tab before the first stop focuses the cycle's last stop, and
// the backward walk then continues in the cycle's reverse order.
func TestTabWrapOnWrapsBackwardToTheLast(t *testing.T) {
	if testing.Short() {
		t.Skip("tab traversal draws through the native layout services")
	}
	c := newWrapCorpus(t, true)
	v := c.view

	c.wrapStep(t, true, &v.q, "focus onto the first stop")
	c.wrapStep(t, false, &v.f, "focus-prev before the first stop wraps to the last")
	c.wrapStep(t, false, &v.e, "the backward walk continues after the wrap")
	c.wrapStep(t, false, &v.z2, "the group's members walk in reverse")
}

// TestTabWrapSkipsInertSubtreesAtTheEdges pins the wrap against the
// corpus's inert bookends: the inert head carries tabindex 1 (it would
// sort first — the forward wrap's target — if inertness did not
// suppress its registration) and the inert tail tabindex 5 (it would
// sort last, the backward wrap's target). Neither wrap may land on an
// inert handle — nor on the head's focusable child, whose inherited
// inertness suppresses its registration too (browser-core 14's rule,
// unchanged by the switch).
func TestTabWrapSkipsInertSubtreesAtTheEdges(t *testing.T) {
	if testing.Short() {
		t.Skip("tab traversal draws through the native layout services")
	}
	c := newWrapCorpus(t, true)
	v := c.view

	// Walk to the last stop (f), then wrap: the target must be the
	// first stop q, not the inert head (tabindex 1) or its child.
	for i := 0; i < 6; i++ {
		c.window.FocusNext()
	}
	c.wrapStep(t, true, &v.q, "the forward wrap skips the inert bookends")
	// Wrap backwards from the first stop: the target must be the last
	// stop f, not the inert tail (tabindex 5).
	c.wrapStep(t, false, &v.f, "the backward wrap skips the inert bookends")
}

// TestTabWrapKeepsPositiveIndexAndTabGroupOrder pins that the wrap
// targets obey the same order the forward walk visits (browser-core
// ticket 14's D4): the forward wrap lands on the LOWEST positive tab
// index (q, before p and the whole zero bucket), the backward wrap on
// the cycle's end — the tab group's last member f, the group sorting
// after its zero-ranked siblings — and a full lap in both directions
// visits exactly the cycle, twice over.
func TestTabWrapKeepsPositiveIndexAndTabGroupOrder(t *testing.T) {
	if testing.Short() {
		t.Skip("tab traversal draws through the native layout services")
	}
	c := newWrapCorpus(t, true)
	v := c.view

	forward := []*gpui.FocusHandle{&v.q, &v.p, &v.z1, &v.z2, &v.e, &v.f}
	// Two full forward laps: the second starts with the wrap over the
	// boundary.
	for lap := 0; lap < 2; lap++ {
		for i, want := range forward {
			c.window.FocusNext()
			got := c.window.Focused()
			if !got.Ok || got.Handle.ID() != want.ID() {
				t.Fatalf("forward lap %d step %d = %+v, want handle %d", lap, i, got, want.ID())
			}
		}
	}
	// A full backward lap: it starts at the first stop (the last
	// forward step left focus on f, so the first Shift+Tab is the
	// in-cycle move to e) and ends back on f through the wrap.
	backward := []*gpui.FocusHandle{&v.e, &v.z2, &v.z1, &v.p, &v.q, &v.f}
	for i, want := range backward {
		c.window.FocusPrev()
		got := c.window.Focused()
		if !got.Ok || got.Handle.ID() != want.ID() {
			t.Fatalf("backward step %d = %+v, want handle %d", i, got, want.ID())
		}
	}
}

// TestTabWrapFromANegativeTabIndexAtTheEnd pins the boundary rule for
// an excluded element: a focused element with a negative tab index at
// the tree's end searches forward in TREE order (browser-core 14),
// finds nothing after itself, and — with the wrap on — the boundary
// wraps to the first stop instead of passing out.
func TestTabWrapFromANegativeTabIndexAtTheEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("tab traversal draws through the native layout services")
	}
	c := newWrapCorpus(t, true)
	v := c.view

	c.window.Focus(v.n)
	if focused := c.window.Focused(); !focused.Ok || focused.Handle.ID() != v.n.ID() {
		t.Fatalf("focus n = %+v, want the excluded handle %d", focused, v.n.ID())
	}
	c.wrapStep(t, true, &v.q, "focus-next from the excluded element at the end wraps to the first")
	// Backwards from the excluded element keeps ticket 14's tree-order
	// arm: the previous stop in tree order (z2), not the wrap.
	c.window.Focus(v.n)
	c.wrapStep(t, false, &v.z2, "focus-prev from the excluded element stays in tree order")
}

// TestTabWrapWithNothingFocusableDoesNothing pins the empty case: with
// no tab stop at all (only an inert focusable element, whose
// registration the inertness suppresses), the wrap has no target —
// FocusNext/FocusPrev leave focus where it was (nowhere), and the Tab
// default action does nothing either.
func TestTabWrapWithNothingFocusableDoesNothing(t *testing.T) {
	if testing.Short() {
		t.Skip("tab traversal draws through the native layout services")
	}
	ta := gpui.NewTestApp().WithTabWrap(true)
	app := ta.App()
	window := gpui.NewTestWindow(app, gpui.Size{Width: 320, Height: 200})
	inert := gpui.NewFocusHandle(window).TabStop(true)
	window.SetRootView(gpui.ViewOf(gpui.NewEntity(app, window.Scope(), func(v *emptyView, cx *gpui.Context[emptyView]) {
		v.inert = inert
	})))
	if _, err := gpui.DrawWindowFrame(window); err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}

	window.FocusNext()
	if focused := window.Focused(); focused.Ok {
		t.Fatalf("focus-next with nothing focusable = handle %d, want no focus", focused.Handle.ID())
	}
	window.FocusPrev()
	if focused := window.Focused(); focused.Ok {
		t.Fatalf("focus-prev with nothing focusable = handle %d, want no focus", focused.Handle.ID())
	}
	// The Tab default action path does nothing too.
	window.DispatchKeyEvent(&gpui.KeyDownEvent{Keystroke: gpui.MustParseKeystroke("tab")}, app)
	if focused := window.Focused(); focused.Ok {
		t.Fatalf("Tab with nothing focusable = handle %d, want no focus", focused.Handle.ID())
	}
}

// emptyView renders one inert focusable element and nothing else.
type emptyView struct {
	inert gpui.FocusHandle
}

func (v *emptyView) Render(w *gpui.Window, cx *gpui.Context[emptyView]) gpui.AnyElement {
	return gpui.Div().Flex().
		KeyContext("Root").
		Child(gpui.Div().Flex().KeyContext("Inert").TrackFocus(v.inert).Inert()).
		IntoElement()
}
