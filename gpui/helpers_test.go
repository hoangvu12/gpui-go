package gpui

import (
	"testing"
)

// Ticket 09 unit tests for the window-free helpers: globals, geometry,
// element ids, the mouse-event helpers, App.Notify and app-level focus
// handles. Each test cites the pinned behavior it derives from
// (reference/ce-source/crates/gpui/src, CE 254b5dbd). The window-bound
// helpers are exercised through real test windows in
// helpers_draw_windows_test.go.

type helperTheme struct{ name string }

// TestHelperGlobals pins app.rs:2106-2146: has_global/try_global report
// assignment; default_global assigns the default once, returns the same
// mutable value afterwards and queues a global notification each call.
func TestHelperGlobals(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()

	if HasGlobal[helperTheme](app) {
		t.Fatal("HasGlobal before SetGlobal = true")
	}
	if _, ok := TryGlobal[helperTheme](app); ok {
		t.Fatal("TryGlobal before SetGlobal reported a value")
	}
	app.Update(func(a *App) { a.SetGlobal(helperTheme{name: "dark"}) })
	if !HasGlobal[helperTheme](app) {
		t.Fatal("HasGlobal after SetGlobal = false")
	}
	if got, ok := TryGlobal[helperTheme](app); !ok || got.name != "dark" {
		t.Fatalf("TryGlobal = %+v, %v", got, ok)
	}

	notified := 0
	app.Update(func(a *App) {
		a.ObserveGlobal[*helperTheme](func(*App) { notified++ })
	})
	if HasGlobal[*helperTheme](app) {
		t.Fatal("pointer global present before DefaultGlobal")
	}
	var first *helperTheme
	app.Update(func(a *App) {
		first = DefaultGlobal[helperTheme](a)
		first.name = "light"
	})
	if notified != 1 {
		t.Fatalf("DefaultGlobal notifications = %d, want 1", notified)
	}
	app.Update(func(a *App) {
		second := DefaultGlobal[helperTheme](a)
		if second != first || second.name != "light" {
			t.Fatalf("DefaultGlobal returned %p (%q), want the assigned %p", second, second.name, first)
		}
	})
	if notified != 2 {
		t.Fatalf("DefaultGlobal notifications = %d, want 2", notified)
	}
	if got := Global[*helperTheme](app); got != first {
		t.Fatal("Global[*G] does not read the DefaultGlobal value")
	}
}

// TestHelperAxisAndAlong pins geometry.rs:33-41 (invert) and the Point
// and Size Along impls (166-184, 541-560).
func TestHelperAxisAndAlong(t *testing.T) {
	if AxisVertical.Invert() != AxisHorizontal || AxisHorizontal.Invert() != AxisVertical {
		t.Fatal("Axis.Invert does not swap")
	}
	p := Point{X: 3, Y: 4}
	if p.Along(AxisHorizontal) != 3 || p.Along(AxisVertical) != 4 {
		t.Fatalf("Point.Along = %v/%v", p.Along(AxisHorizontal), p.Along(AxisVertical))
	}
	double := func(v float32) float32 { return v * 2 }
	if got := p.ApplyAlong(AxisHorizontal, double); got != (Point{X: 6, Y: 4}) {
		t.Fatalf("Point.ApplyAlong(H) = %+v", got)
	}
	if got := p.ApplyAlong(AxisVertical, double); got != (Point{X: 3, Y: 8}) {
		t.Fatalf("Point.ApplyAlong(V) = %+v", got)
	}
	s := Size{Width: 10, Height: 20}
	if s.Along(AxisHorizontal) != 10 || s.Along(AxisVertical) != 20 {
		t.Fatal("Size.Along mismatch")
	}
	if got := s.ApplyAlong(AxisVertical, double); got != (Size{Width: 10, Height: 40}) {
		t.Fatalf("Size.ApplyAlong(V) = %+v", got)
	}
}

// TestHelperAnchor pins the Anchor doc examples and tables
// (geometry.rs:2228-2293: opposite, other_side_along, is_center).
func TestHelperAnchor(t *testing.T) {
	// Doc examples (geometry.rs:2234, 2257-2258).
	if AnchorTopLeft.Opposite() != AnchorBottomRight {
		t.Fatal("TopLeft.Opposite != BottomRight")
	}
	if AnchorTopLeft.OtherSideAlong(AxisHorizontal) != AnchorTopRight {
		t.Fatal("TopLeft.OtherSideAlong(Horizontal) != TopRight")
	}
	opposite := map[Anchor]Anchor{
		AnchorTopLeft: AnchorBottomRight, AnchorTopRight: AnchorBottomLeft,
		AnchorBottomLeft: AnchorTopRight, AnchorBottomRight: AnchorTopLeft,
		AnchorTopCenter: AnchorBottomCenter, AnchorBottomCenter: AnchorTopCenter,
		AnchorLeftCenter: AnchorRightCenter, AnchorRightCenter: AnchorLeftCenter,
	}
	vertical := map[Anchor]Anchor{
		AnchorTopLeft: AnchorBottomLeft, AnchorTopRight: AnchorBottomRight,
		AnchorBottomLeft: AnchorTopLeft, AnchorBottomRight: AnchorTopRight,
		AnchorTopCenter: AnchorBottomCenter, AnchorBottomCenter: AnchorTopCenter,
		AnchorLeftCenter: AnchorLeftCenter, AnchorRightCenter: AnchorRightCenter,
	}
	horizontal := map[Anchor]Anchor{
		AnchorTopLeft: AnchorTopRight, AnchorTopRight: AnchorTopLeft,
		AnchorBottomLeft: AnchorBottomRight, AnchorBottomRight: AnchorBottomLeft,
		AnchorTopCenter: AnchorTopCenter, AnchorBottomCenter: AnchorBottomCenter,
		AnchorLeftCenter: AnchorRightCenter, AnchorRightCenter: AnchorLeftCenter,
	}
	for a, want := range opposite {
		if got := a.Opposite(); got != want {
			t.Errorf("%v.Opposite = %v, want %v", a, got, want)
		}
		if got := a.OtherSideAlong(AxisVertical); got != vertical[a] {
			t.Errorf("%v.OtherSideAlong(V) = %v, want %v", a, got, vertical[a])
		}
		if got := a.OtherSideAlong(AxisHorizontal); got != horizontal[a] {
			t.Errorf("%v.OtherSideAlong(H) = %v, want %v", a, got, horizontal[a])
		}
		wantCenter := a == AnchorTopCenter || a == AnchorBottomCenter || a == AnchorLeftCenter || a == AnchorRightCenter
		if a.IsCenter() != wantCenter {
			t.Errorf("%v.IsCenter = %v", a, a.IsCenter())
		}
	}
}

// TestHelperBoundsGeometry pins Bounds::contains (geometry.rs:1465-1470
// and its doc example), the corner/center doc examples (998, 1341,
// 1365, 1389, 1418) and from_anchor_and_size over Size::at_anchor
// (866-871, 494-505).
func TestHelperBoundsGeometry(t *testing.T) {
	// Doc example: (0,0,10,10) contains (5,5), not (15,15).
	square := Bounds{Size: Size{Width: 10, Height: 10}}
	if !square.Contains(Point{X: 5, Y: 5}) || square.Contains(Point{X: 15, Y: 15}) {
		t.Fatal("Contains doc example mismatch")
	}
	// Origin inclusive, far edge exclusive.
	if !square.Contains(Point{}) || square.Contains(Point{X: 10, Y: 5}) || square.Contains(Point{X: 5, Y: 10}) {
		t.Fatal("Contains edge semantics mismatch")
	}

	b := Bounds{Size: Size{Width: 10, Height: 20}}
	cases := []struct {
		anchor Anchor
		want   Point
	}{
		{AnchorTopLeft, Point{}},
		{AnchorTopRight, Point{X: 10}},
		{AnchorBottomLeft, Point{Y: 20}},
		{AnchorBottomRight, Point{X: 10, Y: 20}},
		{AnchorTopCenter, Point{X: 5}},
		{AnchorBottomCenter, Point{X: 5, Y: 20}},
		{AnchorLeftCenter, Point{Y: 10}},
		{AnchorRightCenter, Point{X: 10, Y: 10}},
	}
	for _, c := range cases {
		if got := b.Corner(c.anchor); got != c.want {
			t.Errorf("Corner(%v) = %+v, want %+v", c.anchor, got, c.want)
		}
		if got := b.Size.AtAnchor(c.anchor); got != c.want {
			t.Errorf("AtAnchor(%v) = %+v, want %+v", c.anchor, got, c.want)
		}
		// The anchor point of the built bounds lands on the origin.
		origin := Point{X: 100, Y: 50}
		built := BoundsFromAnchorAndSize(c.anchor, origin, b.Size)
		if built.Size != b.Size || built.Corner(c.anchor) != origin {
			t.Errorf("BoundsFromAnchorAndSize(%v) = %+v (corner %+v)", c.anchor, built, built.Corner(c.anchor))
		}
	}
	if got := b.Center(); got != (Point{X: 5, Y: 10}) {
		t.Fatalf("Center = %+v, want (5,10)", got)
	}
}

// TestHelperElementIDs pins the NamedInteger / NamedChild / CodeLocation
// Display forms (window.rs:7692-7696) and the tuple conversions they
// replace (7770-7803).
func TestHelperElementIDs(t *testing.T) {
	if got := NamedIntegerElementID("item", 3).String(); got != "item-3" {
		t.Fatalf("NamedInteger = %q", got)
	}
	child := NamedChildElementID(NamedIntegerElementID("row", 2), "cell")
	if got := child.String(); got != "row-2-cell" {
		t.Fatalf("NamedChild = %q", got)
	}
	nested := NamedChildElementID(child, "label")
	if got := nested.String(); got != "row-2-cell-label" {
		t.Fatalf("nested NamedChild = %q", got)
	}
	if got := codeLocationElementID("file.go:12").String(); got != "file.go:12" {
		t.Fatalf("CodeLocation = %q", got)
	}
	g := &GlobalElementID{Path: []ElementID{NameElementID("root"), NamedIntegerElementID("item", 1)}}
	if got := g.String(); got != "root.item-1" {
		t.Fatalf("global path = %q", got)
	}
}

// TestHelperMouseEventHelpers pins MouseMoveEvent::dragging
// (interactive.rs:515-517) and ScrollDelta::precise / pixel_delta
// (606-619).
func TestHelperMouseEventHelpers(t *testing.T) {
	left, right := MouseButtonLeft, MouseButtonRight
	if (MouseMoveEvent{}).Dragging() {
		t.Fatal("a move without a pressed button is dragging")
	}
	if !(MouseMoveEvent{PressedButton: &left}).Dragging() {
		t.Fatal("a left-button move is not dragging")
	}
	if (MouseMoveEvent{PressedButton: &right}).Dragging() {
		t.Fatal("a right-button move is dragging")
	}

	pixels := ScrollDeltaPixels(3, -4)
	if !pixels.Precise() || pixels.PixelDelta(20) != (Point{X: 3, Y: -4}) {
		t.Fatalf("pixel delta = %v %+v", pixels.Precise(), pixels.PixelDelta(20))
	}
	lines := ScrollDeltaLines(1, -2)
	if lines.Precise() || lines.PixelDelta(20) != (Point{X: 20, Y: -40}) {
		t.Fatalf("line delta = %v %+v", lines.Precise(), lines.PixelDelta(20))
	}
}

type helperCounter struct{ n int }

// TestHelperAppNotify pins App::notify(entity_id) (app.rs:2920): the
// entity's observers run at the next flush, coalesced while pending.
func TestHelperAppNotify(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	source := NewEntity(app, app.RootScope(), func(*helperCounter, *Context[helperCounter]) {})
	observed := 0
	NewEntity(app, app.RootScope(), func(_ *helperCounter, cx *Context[helperCounter]) {
		cx.Observe(source, func(*helperCounter, Entity[helperCounter], *Context[helperCounter]) { observed++ })
	})
	app.Update(func(a *App) {
		a.Notify(source.EntityID())
		a.Notify(source.EntityID()) // coalesced while pending
	})
	if observed != 1 {
		t.Fatalf("observer runs = %d, want 1", observed)
	}
}

// TestHelperAppFocusHandle pins App::focus_handle (app.rs:2794-2798): a
// window-free handle with default tab properties whose release is
// idempotent and whose weak form stops upgrading after release.
func TestHelperAppFocusHandle(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	a := app.FocusHandle()
	b := app.FocusHandle()
	if a.ID() == b.ID() || a.ID() == 0 {
		t.Fatalf("app focus ids not distinct: %d %d", a.ID(), b.ID())
	}
	if !isAppFocusID(a.ID()) {
		t.Fatal("app-level handle allocated from the window id space")
	}
	ref := a.windowRef()
	if ref == nil || ref.tabIndex != 0 || ref.tabStop {
		t.Fatalf("default tab properties = %+v", ref)
	}
	a = a.TabIndex(2).TabStop(true)
	if ref := a.windowRef(); ref.tabIndex != 2 || !ref.tabStop {
		t.Fatalf("tab properties not recorded: %+v", ref)
	}
	weak := a.Downgrade()
	if _, ok := weak.Upgrade(); !ok {
		t.Fatal("weak app handle does not upgrade while live")
	}
	a.Release()
	a.Release() // idempotent
	if _, ok := weak.Upgrade(); ok {
		t.Fatal("weak app handle upgrades after release")
	}
	if b.windowRef() == nil {
		t.Fatal("releasing one app handle released another")
	}
}
