package gpui

// Ticket 21 (browser-core): the top layer and click-based light dismiss
// — the ordered layer, the kinds, the invoker link, the nested-popover
// and outside-click dismissal rules — tested through the public
// TopLayer/OverlayHandle API on the deterministic test window. The
// Chrome-parity twins of these tests are
// internal/parity/fixtures/popover.

import (
	"fmt"
	"testing"
)

// overlayColors is the test palette.
var overlayColors = struct {
	page, invoker, popover, child Hsla
}{
	page:    Hsla{H: 0, S: 0, L: 1, A: 1},
	invoker: Hsla{H: 0.6, S: 1, L: 0.5, A: 1},
	popover: Hsla{H: 0.12, S: 1, L: 0.75, A: 1},
	child:   Hsla{H: 0.3, S: 1, L: 0.6, A: 1},
}

// overlayView renders the popover fixture tree: one invoker at the
// root (b1), one parent popover (p1, in the top layer) whose content
// carries the child's invoker (b2), and the child popover (p2, in the
// top layer beside p1) — the DOM shape, where both popovers are body
// children and the nesting is the invoker link (Chromium's popover
// ancestor rule 3: an invoker inside a popover makes that popover the
// ancestor). The handles and their toggle log live on the view entity
// (mutable state outside the per-frame build).
type overlayView struct {
	parent, child OverlayHandle
	handles       bool
	// manual renders the parent's entry as popover=manual.
	manual bool
	// sibling renders b2 at the root instead of inside p1: the child's
	// invoker is then NOT inside the parent, so showing the child is a
	// sibling show (ShowPopoverInternal closes non-ancestors).
	sibling bool
	// backdrop gives the parent's entry a backdrop slot.
	backdrop bool
	// log records clicks ("click:p1") and layer toggles ("toggle:p1
	// open") in delivery order.
	log []string
}

func (v *overlayView) ensureHandles(w *Window) {
	if v.handles {
		return
	}
	v.parent = NewOverlayHandle(w)
	v.parent.OnToggle(func(open bool, _ *Window, _ *App) {
		v.log = append(v.log, fmt.Sprintf("toggle:p1 %v", open))
	})
	v.child = NewOverlayHandle(w)
	v.child.OnToggle(func(open bool, _ *Window, _ *App) {
		v.log = append(v.log, fmt.Sprintf("toggle:p2 %v", open))
	})
	v.handles = true
}

func (v *overlayView) Render(w *Window, cx *Context[overlayView]) AnyElement {
	v.ensureHandles(w)
	root := Div().ID("root").SizeFull().Flex().Bg(overlayColors.page).
		// The parent's invoker: clicking it toggles the parent (the
		// popovertarget activation).
		Child(Div().ID("b1").DebugSelector("b1").
			W(Px(40)).H(Px(20)).Bg(overlayColors.invoker).
			OnClick(func(*ClickEvent, *Window, *App) {
				v.log = append(v.log, "click:b1")
				v.parent.Toggle()
			}))
	// b2: inside p1's content (the invoker link that makes p1 an
	// ancestor of p2), or at the root in sibling mode.
	b2 := Div().ID("b2").DebugSelector("b2").
		W(Px(40)).H(Px(20)).Bg(overlayColors.invoker).
		OnClick(func(*ClickEvent, *Window, *App) {
			v.log = append(v.log, "click:b2")
			v.child.Toggle()
		})
	if v.sibling {
		root.Child(b2)
	}
	if v.parent.IsOpen() {
		parentLayer := TopLayer(v.parent).Invoker("b1")
		if v.manual {
			parentLayer = parentLayer.Kind(OverlayPopoverManual)
		}
		if v.backdrop {
			parentLayer = parentLayer.Backdrop(Div().DebugSelector("backdrop").
				SizeFull().Bg(Hsla{H: 0, S: 0, L: 0, A: 0.4}))
		}
		p1 := Div().ID("p1").DebugSelector("p1").
			Absolute().Top(Px(60)).Left(Px(20)).W(Px(150)).H(Px(120)).
			Bg(overlayColors.popover).Occlude().
			OnClick(func(*ClickEvent, *Window, *App) {
				v.log = append(v.log, "click:p1")
			})
		if !v.sibling {
			// The child's invoker, inside the parent popover: the
			// invoker link that makes p1 an ancestor of p2.
			p1.Child(b2.Absolute().Top(Px(10)).Left(Px(10)))
		}
		root.Child(parentLayer.Child(p1))
	}
	if v.child.IsOpen() {
		root.Child(TopLayer(v.child).Invoker("b2").
			Child(Div().ID("p2").DebugSelector("p2").
				Absolute().Top(Px(60)).Left(Px(200)).W(Px(100)).H(Px(80)).
				Bg(overlayColors.child).Occlude().
				OnClick(func(*ClickEvent, *Window, *App) {
					v.log = append(v.log, "click:p2")
				})))
	}
	return root.IntoElement()
}

// overlayFixture is one test app, window and overlayView.
type overlayFixture struct {
	t      *testing.T
	ta     *TestApp
	app    *App
	window *Window
	view   Entity[overlayView]
}

func newOverlayFixture(t *testing.T) *overlayFixture {
	t.Helper()
	ta := NewTestApp()
	app := ta.App()
	window := NewTestWindow(app, Size{Width: 400, Height: 300})
	view := NewEntity(app, window.Scope(), func(v *overlayView, cx *Context[overlayView]) {})
	f := &overlayFixture{t: t, ta: ta, app: app, window: window, view: view}
	window.SetRootView(ViewOf(view))
	t.Cleanup(func() {
		CloseTestWindow(window)
		releaseAppImageCodec(app)
	})
	f.draw()
	return f
}

// draw draws one frame.
func (f *overlayFixture) draw() {
	f.t.Helper()
	if _, err := DrawWindowFrame(f.window); err != nil {
		f.t.Fatalf("DrawWindowFrame: %v", err)
	}
}

// settle parks the scheduler (a click's state changes redraw through
// the flush) and draws a frame.
func (f *overlayFixture) settle() {
	f.t.Helper()
	f.ta.RunUntilParked()
	f.draw()
}

// click simulates a left click at (x, y).
func (f *overlayFixture) click(x, y float32) {
	f.t.Helper()
	f.window.DispatchInput(&MouseDownEvent{Button: MouseButtonLeft, Position: Point{X: x, Y: y}, ClickCount: 1}, f.app)
	f.window.DispatchInput(&MouseUpEvent{Button: MouseButtonLeft, Position: Point{X: x, Y: y}, ClickCount: 1}, f.app)
}

// log returns the recorded click and toggle entries.
func (f *overlayFixture) log() []string {
	return f.view.Read(f.app, func(v *overlayView, _ *App) []string { return v.log })
}

// takeLog returns the log and clears it.
func (f *overlayFixture) takeLog() []string {
	entries := f.log()
	f.view.Update(f.app, func(v *overlayView, _ *Context[overlayView]) { v.log = nil })
	return entries
}

// openBoth opens the parent and then the child (through their
// invokers, like the fixture's input sequence).
func (f *overlayFixture) openBoth() {
	f.click(20, 10) // b1
	f.settle()
	f.click(30, 75) // b2, inside p1 at (20,60)+(10,10)+(20,10)
	f.settle()
}

// hasBounds reports whether the selector drew in the last frame.
func (f *overlayFixture) hasBounds(selector string) bool {
	_, ok := WindowDebugBound(f.window, selector)
	return ok
}

// wantLog asserts the exact log.
func wantLog(t *testing.T, what string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: log = %q, want %q", what, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s: log = %q, want %q", what, got, want)
		}
	}
}

// TestTopLayerClickInsideNestedPopoverKeepsBoth is the ticket's core
// acceptance at unit level: a click inside the nested child popover
// keeps both popovers open — the child is "inside" the parent through
// the invoker link, so nothing above the child hides (Chromium's
// FindTopmostRelatedPopover resolves the click to the child; the
// parent sits below it and stays).
func TestTopLayerClickInsideNestedPopoverKeepsBoth(t *testing.T) {
	f := newOverlayFixture(t)
	f.openBoth()
	if !f.hasBounds("p1") || !f.hasBounds("p2") {
		t.Fatal("the popovers did not open")
	}

	f.click(250, 100) // inside p2, outside p1
	f.settle()

	if !f.hasBounds("p2") {
		t.Fatal("a click inside the child popover closed the child")
	}
	if !f.hasBounds("p1") {
		t.Fatal("a click inside the nested child popover dismissed the parent (HTML popovers keep it open)")
	}
}

// TestTopLayerClickInsideTheParentClosesTheChild: a click inside the
// parent's content (not on the child's invoker) relates to the parent,
// so everything above it — the child — hides and the parent stays
// (HideAllPopoversUntil).
func TestTopLayerClickInsideTheParentClosesTheChild(t *testing.T) {
	f := newOverlayFixture(t)
	f.openBoth()

	f.click(120, 150) // inside p1, away from b2
	f.settle()

	if !f.hasBounds("p1") {
		t.Fatal("a click inside the parent closed the parent")
	}
	if f.hasBounds("p2") {
		t.Fatal("a click inside the parent left the child open")
	}
}

// TestTopLayerClickOutsideClosesEverything: a click outside every
// popover relates to no popover, so the whole light-dismissable layer
// hides, from the top down (the child's toggle precedes the parent's).
func TestTopLayerClickOutsideClosesEverything(t *testing.T) {
	f := newOverlayFixture(t)
	f.openBoth()
	f.takeLog()

	f.click(350, 20) // the empty page
	f.settle()

	if f.hasBounds("p1") || f.hasBounds("p2") {
		t.Fatal("an outside click left a popover open")
	}
	wantLog(t, "the outside-click toggles", f.takeLog(), []string{
		"toggle:p2 false",
		"toggle:p1 false",
	})
}

// TestTopLayerInvokerClickKeepsThePopoverAndTheActivationToggles: a
// click on the open parent's invoker does not light-dismiss it (the
// invoker link resolves the click to the parent, so nothing above it
// hides); the click's own activation then toggles the parent closed.
// If light dismiss closed it first, the activation's toggle would
// reopen it and the popover would still draw.
func TestTopLayerInvokerClickKeepsThePopoverAndTheActivationToggles(t *testing.T) {
	f := newOverlayFixture(t)
	f.openBoth()
	f.takeLog()

	f.click(20, 10) // b1, the parent's invoker
	f.settle()

	if f.hasBounds("p1") {
		t.Fatal("the invoker click did not toggle the parent closed")
	}
	if f.hasBounds("p2") {
		t.Fatal("the invoker click left the child open (light dismiss hides everything above the parent)")
	}
	wantLog(t, "the invoker-click log", f.takeLog(), []string{
		"click:b1",
		"toggle:p2 false", // the light dismiss hides the child above the parent
		"toggle:p1 false", // the activation's toggle closes the parent
	})
}

// TestTopLayerManualKindNeverLightDismisses: a popover=manual entry
// stays open through an outside click (Chromium keeps manual popovers
// out of the light-dismiss stacks).
func TestTopLayerManualKindNeverLightDismisses(t *testing.T) {
	f := newOverlayFixture(t)
	f.view.Update(f.app, func(v *overlayView, _ *Context[overlayView]) {
		v.ensureHandles(f.window)
		v.parent.Show()
	})
	f.settle()
	if !f.hasBounds("p1") {
		t.Fatal("the popover did not open")
	}

	// Turn the entry manual by re-registering with the manual kind.
	f.view.Update(f.app, func(v *overlayView, _ *Context[overlayView]) {
		v.manual = true
	})
	f.settle()
	f.click(350, 20)
	f.settle()

	if !f.hasBounds("p1") {
		t.Fatal("an outside click closed a manual popover")
	}
}

// TestTopLayerShowClosesSiblingPopovers: showing an auto popover with
// no popover ancestor closes the open popovers above it
// (ShowPopoverInternal's HideAllPopoversUntil(nullptr)); showing one
// whose invoker sits inside another popover keeps that ancestor
// (the openBoth flow of every other test).
func TestTopLayerShowClosesSiblingPopovers(t *testing.T) {
	f := newOverlayFixture(t)
	// Open the parent through its invoker; then show the child WITHOUT
	// its invoker being inside the parent: hide p1's content from the
	// tree so b2 is not under it (a sibling invoker).
	f.click(20, 10)
	f.settle()
	f.view.Update(f.app, func(v *overlayView, _ *Context[overlayView]) {
		v.sibling = true
	})
	f.settle()
	f.click(60, 10) // b2, at the root beside b1 in sibling mode
	f.settle()

	if f.hasBounds("p1") {
		t.Fatal("showing a sibling popover left the parent open (ShowPopoverInternal closes non-ancestors)")
	}
	if !f.hasBounds("p2") {
		t.Fatal("the sibling popover did not open")
	}
}

// TestTopLayerToggleNotificationsLandAfterTheClick: the toggle
// notification is queued and delivered at the input task's end, after
// the whole click dispatch's listeners — the DOM's toggle task order.
func TestTopLayerToggleNotificationsLandAfterTheClick(t *testing.T) {
	f := newOverlayFixture(t)
	f.openBoth()
	f.takeLog()

	f.click(250, 100) // inside p2: no dismissal, but the click logs
	f.settle()

	entries := f.takeLog()
	if len(entries) == 0 || entries[0] != "click:p2" {
		t.Fatalf("the click entry must precede any toggle, got %q", entries)
	}
	wantLog(t, "a click inside the child logs only the click", entries, []string{"click:p2"})
}

// TestTopLayerBackdropCoversTheWindowBelowTheContent: the backdrop
// slot paints over everything below the entry and under the entry's
// content — its box is the window, the popover's box sits above it in
// the same deferred draw.
func TestTopLayerBackdropCoversTheWindowBelowTheContent(t *testing.T) {
	f := newOverlayFixture(t)
	f.view.Update(f.app, func(v *overlayView, _ *Context[overlayView]) {
		v.ensureHandles(f.window)
		v.parent.Show()
		v.backdrop = true
	})
	f.settle()

	if !f.hasBounds("p1") {
		t.Fatal("the popover did not open")
	}
	bounds, ok := WindowDebugBound(f.window, "backdrop")
	if !ok {
		t.Fatal("the backdrop did not render")
	}
	if bounds != logical(0, 0, 400, 300) {
		t.Fatalf("the backdrop bounds = %v, want the window", bounds)
	}
	// The popover content still places at its fixed box.
	if p1, ok := WindowDebugBound(f.window, "p1"); !ok || p1 != logical(20, 60, 150, 120) {
		t.Fatalf("the popover bounds = %v, want (20,60,150,120)", p1)
	}
}

// TestTopLayerDragFromInsideKeepsThePopover: the press inside the
// popover and the release outside relate to different popovers, so the
// pair rule never dismisses (a drag from inside to outside — WCAG's
// pointer-cancellation rationale, html_element.cc:3183-3198).
func TestTopLayerDragFromInsideKeepsThePopover(t *testing.T) {
	f := newOverlayFixture(t)
	f.click(20, 10) // open the parent
	f.settle()

	// Press inside p1, release outside.
	f.window.DispatchInput(&MouseDownEvent{Button: MouseButtonLeft, Position: Point{X: 60, Y: 80}, ClickCount: 1}, f.app)
	f.window.DispatchInput(&MouseUpEvent{Button: MouseButtonLeft, Position: Point{X: 350, Y: 20}, ClickCount: 1}, f.app)
	f.settle()

	if !f.hasBounds("p1") {
		t.Fatal("a drag from inside the popover to outside dismissed it (the pair rule must not)")
	}
}

// TestAnchoredIsAContainingBlockForAbsoluteChildrenOfStaticDivs is the
// ticket-33 note adopted here (ticket 25 deferred it): the Anchored
// element's absolutely positioned node requests through
// RequestContainerLayout, so an absolute child of a Static() div inside
// it resolves against the ANCHORED's padding box (CSS's nearest
// positioned ancestor), not the static div's.
func TestAnchoredIsAContainingBlockForAbsoluteChildrenOfStaticDivs(t *testing.T) {
	f := newPaintFixture(t, func(*paintView, *Window, *Context[paintView]) AnyElement {
		return Div().SizeFull().Flex().Child(
			Anchored().Child(
				Div().DebugSelector("STATIC").Static().
					W(Px(100)).H(Px(100)).
					Mt(Px(30)).
					Child(Div().DebugSelector("ABS").Absolute().
						Top(Px(10)).Left(Px(15)).W(Px(20)).H(Px(20)).Bg(paintRed)),
			)).IntoElement()
	})
	f.draw()
	f.ta.RunUntilParked()

	// The anchored node sits at the root's origin (its absolute node's
	// static position, the root's only child); the static div is 30px
	// down. CSS: the absolute child's containing block is the anchored
	// (positioned), so its box is (15,10)+anchored-origin — not the
	// static div's (15,40).
	got, ok := WindowDebugBound(f.window, "ABS")
	if !ok {
		t.Fatal("the absolute child did not render")
	}
	if got != logical(15, 10, 20, 20) && got != logical(15, 40, 20, 20) {
		t.Fatalf("the absolute child's bounds = %v, want (15,10,20,20) against the anchored node (or the pre-fix (15,40,20,20) against the static div)", got)
	}
	if got == logical(15, 40, 20, 20) {
		t.Fatal("the absolute child resolved against the static div: the Anchored element must be the containing block (RequestContainerLayout)")
	}
}
