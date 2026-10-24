package gpui

import "testing"

// browser-core ticket 33 (spec D3): position: static, the opt-in.
// These tests pin the containing-block rule — an absolute child of a
// static parent resolves against the nearest positioned ancestor (its
// padding box), a static chain defers up to the root (the initial
// containing block), the default (relative) parent keeps the pin's
// direct-parent behaviour bit-for-bit, and a static element itself
// lays out in flow and ignores its insets. The end-to-end Chrome-gated
// behaviour lives in internal/parity/fixtures/position.

// cbWindow opens a harness window rendering build and returns its
// context.
func cbWindow(t *testing.T, build func(v *harnessView, w *Window, cx *Context[harnessView]) AnyElement) *VisualTestContext {
	t.Helper()
	_, _, cx := addHarnessWindow(t, build)
	return cx
}

// cbBounds returns one selector's bounds, failing when missing.
func cbBounds(t *testing.T, cx *VisualTestContext, selector string) Bounds {
	t.Helper()
	b, ok := cx.DebugBounds(selector)
	if !ok {
		t.Fatalf("selector %q has no bounds in the final frame", selector)
	}
	return b
}

// cbAncestor builds the ticket's core case: a positioned ancestor
// (border 4, padding 6) holding a static parent holding one absolute
// child with the given inset style. The ancestor sits at (10, 20) in a
// 200x160 root, so its padding box origin is (14, 24).
func cbAncestor(child func() *DivElement) *DivElement {
	return Div().
		Size(Px(180), Px(120)).
		Relative().
		Ml(Px(10)).
		Mt(Px(20)).
		Border(Px(4)).
		P(Px(6)).
		Child(Div().
			Size(Px(100), Px(60)).
			Static().
			Child(child()))
}

// cbAbsolute builds one absolute child with px insets and an explicit
// size, selector name.
func cbAbsolute(selector string, left, top float32) *DivElement {
	return Div().
		ID(selector).
		DebugSelector(selector).
		Absolute().
		Left(Px(left)).
		Top(Px(top)).
		Size(Px(40), Px(30))
}

// TestStaticParentDefersAbsoluteToPositionedAncestor pins the rule the
// ticket exists for: an absolute child of a static parent resolves its
// insets against the nearest positioned ancestor's PADDING box
// (css-position-3 §2.1; Chromium decides containership from position
// != static, LayoutObject::ComputeIsAbsoluteContainer,
// core/layout/layout_object.cc:2069-2081). The ancestor is at (10, 20)
// with border 4 and padding 6, so its padding box origin is (14, 24)
// and the child's left/top 12/8 lands at (26, 32). Under the pin (the
// direct parent, whose content origin is (20, 30)) the child would sit
// at (32, 38) — the parity fixture's wrong view demonstrates that
// failure against Chrome's record.
func TestStaticParentDefersAbsoluteToPositionedAncestor(t *testing.T) {
	cx := cbWindow(t, func(v *harnessView, w *Window, cx *Context[harnessView]) AnyElement {
		root := Div().Size(Px(200), Px(160)).DebugSelector("root")
		return root.Child(cbAncestor(func() *DivElement { return cbAbsolute("abs", 12, 8) })).IntoElement()
	})
	if b := cbBounds(t, cx, "abs"); b.Origin != (Point{X: 26, Y: 32}) || b.Size != (Size{Width: 40, Height: 30}) {
		t.Fatalf("absolute under a static parent = %+v, want origin (26, 32) size 40x30 (the ancestor's padding box (14, 24) + the 12/8 insets)", b)
	}
}

// TestStaticParentRightBottomInsetsResolveAgainstAncestor pins the far
// edges: right/bottom insets measure from the ancestor padding box's
// far edges (180-8 wide, 120-8 tall, origin (14, 24)), so right 16 /
// bottom 10 puts a 50x36 child at (14+172-16-50, 24+112-10-36) =
// (120, 90).
func TestStaticParentRightBottomInsetsResolveAgainstAncestor(t *testing.T) {
	cx := cbWindow(t, func(v *harnessView, w *Window, cx *Context[harnessView]) AnyElement {
		root := Div().Size(Px(200), Px(160)).DebugSelector("root")
		child := Div().
			ID("abs").
			DebugSelector("abs").
			Absolute().
			Right(Px(16)).
			Bottom(Px(10)).
			Size(Px(50), Px(36))
		return root.Child(cbAncestor(func() *DivElement { return child })).IntoElement()
	})
	if b := cbBounds(t, cx, "abs"); b.Origin != (Point{X: 120, Y: 90}) || b.Size != (Size{Width: 50, Height: 36}) {
		t.Fatalf("absolute right/bottom under a static parent = %+v, want origin (120, 90) size 50x36", b)
	}
}

// TestStaticParentPercentageInsetsResolveAgainstAncestorPaddingBox
// pins percentage insets: left 25% / top 50% resolve against the
// ancestor's padding box (172x112), so the child lands at
// (14+43, 24+56) = (57, 80).
func TestStaticParentPercentageInsetsResolveAgainstAncestorPaddingBox(t *testing.T) {
	cx := cbWindow(t, func(v *harnessView, w *Window, cx *Context[harnessView]) AnyElement {
		root := Div().Size(Px(200), Px(160)).DebugSelector("root")
		child := Div().
			ID("abs").
			DebugSelector("abs").
			Absolute().
			Left(Fraction(0.25)).
			Top(Fraction(0.5)).
			Size(Px(30), Px(20))
		return root.Child(cbAncestor(func() *DivElement { return child })).IntoElement()
	})
	if b := cbBounds(t, cx, "abs"); b.Origin != (Point{X: 57, Y: 80}) || b.Size != (Size{Width: 30, Height: 20}) {
		t.Fatalf("absolute percentage insets under a static parent = %+v, want origin (57, 80) size 30x20", b)
	}
}

// TestStaticChainDefersToTheRoot pins a nested static chain: static
// under static under the (default relative, thus positioned) root —
// the root is the port's stand-in for the CSS initial containing
// block, so an absolute at the end of the chain resolves against the
// root's box. right 20 / bottom 12 on a 44x28 child inside a 300x200
// root lands at (236, 160). Under the pin (the direct parent, a 30x20
// static div) the child would overflow to (-34, -20).
func TestStaticChainDefersToTheRoot(t *testing.T) {
	cx := cbWindow(t, func(v *harnessView, w *Window, cx *Context[harnessView]) AnyElement {
		abs := Div().
			ID("abs").
			DebugSelector("abs").
			Absolute().
			Right(Px(20)).
			Bottom(Px(12)).
			Size(Px(44), Px(28))
		return Div().
			Size(Px(300), Px(200)).
			DebugSelector("root").
			Child(Div().
				Size(Px(50), Px(40)).
				Static().
				DebugSelector("static1").
				Child(Div().
					Size(Px(30), Px(20)).
					Static().
					DebugSelector("static2").
					Child(abs))).
			IntoElement()
	})
	if b := cbBounds(t, cx, "abs"); b.Origin != (Point{X: 236, Y: 160}) || b.Size != (Size{Width: 44, Height: 28}) {
		t.Fatalf("absolute under a static chain to the root = %+v, want origin (236, 160) size 44x28", b)
	}
	if b := cbBounds(t, cx, "static1"); b.Origin != (Point{X: 0, Y: 0}) || b.Size != (Size{Width: 50, Height: 40}) {
		t.Fatalf("static1 = %+v, want (0, 0) 50x40 (static stays in flow)", b)
	}
}

// TestAbsoluteUnderDefaultParentKeepsPinBehaviour pins the
// bit-for-bit default: without the opt-in, an absolute child resolves
// against its DIRECT parent's padding box (the pin's behaviour — the
// research README L2, Taffy compute/flexbox.rs:2231-2239). A 100x80
// parent at (0, 0) with border 4 has its padding box origin at (4, 4),
// so left/top 10/6 puts the child at (14, 10).
func TestAbsoluteUnderDefaultParentKeepsPinBehaviour(t *testing.T) {
	cx := cbWindow(t, func(v *harnessView, w *Window, cx *Context[harnessView]) AnyElement {
		return Div().
			Size(Px(100), Px(80)).
			DebugSelector("root").
			Border(Px(4)).
			Child(Div().
				ID("abs").
				DebugSelector("abs").
				Absolute().
				Left(Px(10)).
				Top(Px(6)).
				Size(Px(50), Px(40))).
			IntoElement()
	})
	if b := cbBounds(t, cx, "abs"); b.Origin != (Point{X: 14, Y: 10}) || b.Size != (Size{Width: 50, Height: 40}) {
		t.Fatalf("absolute under a default (relative) parent = %+v, want origin (14, 10) size 50x40 (the pin's direct-parent rule)", b)
	}
}

// TestStaticBlockParentDefersAbsoluteToAncestor pins the Block-display
// path (the inline frame request): a static block div's absolute child
// still resolves against the nearest positioned ancestor, because the
// inline frame's node request runs through the same containing-block
// policy. The geometry is cbAncestor's, so the child lands at
// (26, 32); under the pin it would sit at the static parent's origin
// (20, 30).
func TestStaticBlockParentDefersAbsoluteToAncestor(t *testing.T) {
	cx := cbWindow(t, func(v *harnessView, w *Window, cx *Context[harnessView]) AnyElement {
		ancestor := Div().
			Size(Px(180), Px(120)).
			Relative().
			Ml(Px(10)).
			Mt(Px(20)).
			Border(Px(4)).
			P(Px(6)).
			Block()
		staticParent := Div().Size(Px(100), Px(60)).Static().Block()
		staticParent = staticParent.Child(cbAbsolute("abs", 12, 8))
		root := Div().Size(Px(200), Px(160)).DebugSelector("root")
		return root.Child(ancestor.Child(staticParent)).IntoElement()
	})
	if b := cbBounds(t, cx, "abs"); b.Origin != (Point{X: 26, Y: 32}) || b.Size != (Size{Width: 40, Height: 30}) {
		t.Fatalf("absolute under a static BLOCK parent = %+v, want origin (26, 32) size 40x30", b)
	}
}

// TestStaticElementStaysInFlowAndIgnoresInsets pins the static
// element's own layout: it participates in flow exactly like the
// default relative element (same bounds) and its insets have no effect
// (CSS static never offsets; the record translation zeroes them). Two
// static children with wildly different insets stack like two plain
// relative ones.
func TestStaticElementStaysInFlowAndIgnoresInsets(t *testing.T) {
	cx := cbWindow(t, func(v *harnessView, w *Window, cx *Context[harnessView]) AnyElement {
		return Div().
			Size(Px(120), Px(90)).
			DebugSelector("root").
			FlexCol().
			Child(Div().
				Size(Px(40), Px(20)).
				Static().
				Left(Px(30)).
				Top(Px(40)).
				DebugSelector("flow1")).
			Child(Div().
				Size(Px(40), Px(20)).
				Static().
				Right(Px(9)).
				Bottom(Px(9)).
				DebugSelector("flow2")).
			IntoElement()
	})
	if b := cbBounds(t, cx, "flow1"); b.Origin != (Point{X: 0, Y: 0}) || b.Size != (Size{Width: 40, Height: 20}) {
		t.Fatalf("static flow1 = %+v, want (0, 0) 40x20 (in flow, insets ignored)", b)
	}
	if b := cbBounds(t, cx, "flow2"); b.Origin != (Point{X: 0, Y: 20}) || b.Size != (Size{Width: 40, Height: 20}) {
		t.Fatalf("static flow2 = %+v, want (0, 20) 40x20 (stacked below flow1)", b)
	}
}

// TestStaticRootFallsBackToDirectParent pins the documented fallback:
// a static element with no positioned scope above it (here: the ROOT
// element itself opts into static, so nothing opens a scope) attaches
// its absolute children to itself — the pin's behaviour — rather than
// leaving them unattached. The root is the port's initial containing
// block stand-in only when it stays positioned; ticket 76 (the default
// flip) revisits the root's ICB role.
func TestStaticRootFallsBackToDirectParent(t *testing.T) {
	cx := cbWindow(t, func(v *harnessView, w *Window, cx *Context[harnessView]) AnyElement {
		return Div().
			Size(Px(100), Px(80)).
			Static().
			DebugSelector("root").
			Child(Div().
				ID("abs").
				DebugSelector("abs").
				Absolute().
				Left(Px(8)).
				Top(Px(8)).
				Size(Px(40), Px(30))).
			IntoElement()
	})
	if b := cbBounds(t, cx, "abs"); b.Origin != (Point{X: 8, Y: 8}) || b.Size != (Size{Width: 40, Height: 30}) {
		t.Fatalf("absolute under a static ROOT = %+v, want origin (8, 8) size 40x30 (the direct-parent fallback)", b)
	}
}

// TestPositionStaticValue pins the enum surface: the value's name,
// parsing (round trip) and the Tailwind class mapping (the generated
// class switch maps "static" to Static()).
func TestPositionStaticValue(t *testing.T) {
	if got := PositionStatic.String(); got != "Static" {
		t.Fatalf("PositionStatic.String() = %q, want %q", got, "Static")
	}
	for _, s := range []string{"Relative", "Absolute", "Static", "Sticky", "Fixed"} {
		p, err := ParsePosition(s)
		if err != nil || p.String() != s {
			t.Fatalf("ParsePosition(%q) = %v, %v", s, p, err)
		}
	}
	// The Tailwind class: "static" applies Static() (the generated
	// method-class switch; "relative" keeps applying Relative()).
	d := Div().Class("static")
	style := d.computeStyle()
	if style.Position != PositionStatic {
		t.Fatalf("class \"static\" leaves position %v, want Static", style.Position)
	}
	d = Div().Class("relative")
	style = d.computeStyle()
	if style.Position != PositionRelative {
		t.Fatalf("class \"relative\" leaves position %v, want Relative", style.Position)
	}
}
