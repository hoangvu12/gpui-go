package gpui

import (
	"reflect"
	"testing"
)

// browser-core ticket 36 (spec stories 12 and 14): display: contents,
// inline-block, inline-grid (and flow-root), overflow: auto with its
// scrollbar-only-when-needed reservation, and scrollbar-gutter: stable.
// These tests pin the record translation, the splice, the atomic inline
// boxes, the auto reservation/detection cycle, the gutter policy, the
// sticky scroll-container rule for hidden overflow (ticket 34's
// recorded limitation), and the enum/class surface. The end-to-end
// Chrome-gated behaviour lives in internal/parity/fixtures/display.

// TestDisplayAndOverflowRecordTranslation pins styleToRecord's mapping
// of the new values: the atomic inline boxes fold like the pin's
// InlineFlex (InlineBlock -> Block, InlineGrid -> Grid), contents takes
// the Block fallback (a contents element normally requests no node),
// flow-root uses the rev-15 tag, and OverflowAuto folds onto Taffy's
// Hidden (the reservation is the div's request-time copy, folded onto
// Scroll WITH a scrollbar width when it applies).
func TestDisplayAndOverflowRecordTranslation(t *testing.T) {
	ctx := LayoutContext{remSize: 16, scaleFactor: 2}
	for _, tc := range []struct {
		display  Display
		overflow Overflow
		wantD    uint32
		wantO    uint32
	}{
		{DisplayContents, OverflowVisible, 1, 0},
		{DisplayInlineBlock, OverflowVisible, 1, 0},
		{DisplayInlineGrid, OverflowVisible, 2, 0},
		{DisplayFlowRoot, OverflowVisible, 4, 0},
		{DisplayFlex, OverflowAuto, 0, 2},
	} {
		style := DefaultStyle()
		style.Display = tc.display
		style.OverflowX = tc.overflow
		rec := styleToRecord(&style, &ctx)
		if rec.Display != tc.wantD {
			t.Errorf("display %v record = %d, want %d", tc.display, rec.Display, tc.wantD)
		}
		if rec.OverflowX != tc.wantO {
			t.Errorf("overflow %v record = %d, want %d", tc.overflow, rec.OverflowX, tc.wantO)
		}
	}
}

// TestParseDisplayAndOverflowRoundTrip pins the new enum surface.
func TestParseDisplayAndOverflowRoundTrip(t *testing.T) {
	for _, d := range []Display{DisplayContents, DisplayInlineBlock, DisplayInlineGrid, DisplayFlowRoot} {
		parsed, err := ParseDisplay(d.String())
		if err != nil || parsed != d {
			t.Errorf("Display round trip %q = %v, %v", d.String(), parsed, err)
		}
	}
	if _, err := ParseOverflow(OverflowAuto.String()); err != nil {
		t.Errorf("ParseOverflow(Auto) = %v", err)
	}
	if _, err := ParseScrollbarGutter(ScrollbarGutterStable.String()); err != nil {
		t.Errorf("ParseScrollbarGutter(Stable) = %v", err)
	}
	if got := DefaultStyle().ScrollbarGutter; got != ScrollbarGutterAuto {
		t.Errorf("default ScrollbarGutter = %v, want Auto", got)
	}
}

// displayFixture is one harness window with draw control: the auto
// scrollbar's reservation settles one frame after the content changes,
// so tests draw (and park) until the layout is stable.
type displayFixture struct {
	t      *testing.T
	ta     *TestApp
	window *Window
	cx     *VisualTestContext
}

func newDisplayFixture(t *testing.T, build func(w *Window) AnyElement) *displayFixture {
	t.Helper()
	f := &displayFixture{t: t}
	ta, _, cx := addHarnessWindow(t, func(v *harnessView, w *Window, cx *Context[harnessView]) AnyElement {
		return build(w)
	})
	f.ta = ta
	f.window = cx.Window()
	f.cx = cx
	return f
}

func (f *displayFixture) park() {
	f.t.Helper()
	f.ta.RunUntilParked()
}

// draw redraws the window once (SetOffset changed only the offset cell:
// the window is not dirty, the next prepaint re-clamps — the sticky
// fixture's draw pattern).
func (f *displayFixture) draw() {
	f.t.Helper()
	if _, err := DrawWindowFrame(f.window); err != nil {
		f.t.Fatalf("DrawWindowFrame: %v", err)
	}
}

func (f *displayFixture) at(selector string) Bounds {
	f.t.Helper()
	b, ok := f.cx.DebugBounds(selector)
	if !ok {
		f.t.Fatalf("selector %q has no bounds in the final frame", selector)
	}
	return b
}

func (f *displayFixture) wantBox(selector string, x, y, w, h float32) {
	f.t.Helper()
	b := f.at(selector)
	if b.Origin != (Point{X: x, Y: y}) || b.Size != (Size{Width: w, Height: h}) {
		f.t.Fatalf("%s bounds = (%g,%g) %gx%g, want (%g,%g) %gx%g", selector,
			b.Origin.X, b.Origin.Y, b.Size.Width, b.Size.Height, x, y, w, h)
	}
}

// TestDisplayContentsSplicesChildrenIntoParent pins the splice: a
// contents div between flex siblings contributes its children at its
// own position — [A, contents(B, C), D] lays out exactly like [A, B, C,
// D] (Chromium: no LayoutObject for the contents element,
// layout_object.cc:430-432; the tree builder splices the children,
// layout_tree_builder_traversal.cc:604-634). A contents element with
// box styles set (background, size, padding — they must stay inert)
// and a nested contents child splice the same.
func TestDisplayContentsSplicesChildrenIntoParent(t *testing.T) {
	child := func(id string, color uint32) *DivElement {
		return Div().ID(id).DebugSelector(id).Size(Px(40), Px(30)).FlexShrink0().Bg(Rgb(color))
	}
	contents := func(children ...*DivElement) *DivElement {
		// Box styles on a contents element are inert (no box to apply
		// them to, css-display-3 §3.1) — they stay set to pin that.
		box := Div().Contents().Bg(Rgb(0xff0000)).Size(Px(999), Px(999)).P(Px(8))
		for _, c := range children {
			box = box.Child(c)
		}
		return box
	}
	f := newDisplayFixture(t, func(w *Window) AnyElement {
		root := Div().ID("root").DebugSelector("root").SizeFull().Flex().Gap(Px(10)).
			Child(child("a", 0x111111)).
			Child(contents(child("b", 0x222222), child("c", 0x333333))).
			Child(child("d", 0x444444))
		return root.IntoElement()
	})
	f.park()
	// [A, B, C, D] in one 40-wide row with 10px gaps: x = 0, 50, 100,
	// 150; the contents element takes no space, gap, or box.
	for i, id := range []string{"a", "b", "c", "d"} {
		f.wantBox(id, float32(50*i), 0, 40, 30)
	}

	// The control: four direct children lay out identically — the
	// splice is a no-op against the plain list.
	f2 := newDisplayFixture(t, func(w *Window) AnyElement {
		root := Div().ID("root").DebugSelector("root").SizeFull().Flex().Gap(Px(10)).
			Child(child("a", 0x111111)).
			Child(child("b", 0x222222)).
			Child(child("c", 0x333333)).
			Child(child("d", 0x444444))
		return root.IntoElement()
	})
	f2.park()
	for _, id := range []string{"a", "b", "c", "d"} {
		b, cb := f.at(id), f2.at(id)
		if b != cb {
			t.Errorf("%s bounds differ from the spliced control: %v vs %v", id, b, cb)
		}
	}
}

// TestDisplayContentsNestedSplicesUnderBlock pins a contents chain
// under a block parent: contents(contents(B)) splices B directly into
// the block's flow, and a contents element with no children contributes
// nothing (its inert none node takes no space).
func TestDisplayContentsNestedSplicesUnderBlock(t *testing.T) {
	f := newDisplayFixture(t, func(w *Window) AnyElement {
		root := Div().ID("root").DebugSelector("root").SizeFull().FlexCol().
			Child(Div().Contents().
				Child(Div().Contents().
					Child(Div().ID("b").DebugSelector("b").Size(Px(40), Px(30)).FlexShrink0().Bg(Rgb(0x222222)))).
				Child(Div().ID("c").DebugSelector("c").Size(Px(40), Px(30)).FlexShrink0().Bg(Rgb(0x333333)))).
			Child(Div().Contents()).
			Child(Div().ID("d").DebugSelector("d").Size(Px(40), Px(30)).FlexShrink0().Bg(Rgb(0x444444)))
		return root.IntoElement()
	})
	f.park()
	f.wantBox("b", 0, 0, 40, 30)
	f.wantBox("c", 0, 30, 40, 30)
	f.wantBox("d", 0, 60, 40, 30)
}

// TestInlineBlockAndInlineGridAreAtomicInlineBoxes pins the atomic
// inline-level boxes in a block parent's paragraph: two inline-blocks
// sit side by side on one row at the text position (the paragraph
// collection treats them as atomic boxes, css-display-3), and an
// inline-grid lays its children out on its grid INSIDE the atomic box
// (Chromium: kInlineBlock creates a LayoutBlockFlow,
// layout_object.cc:437-442; kInlineGrid a LayoutGrid, :470-472).
func TestInlineBlockAndInlineGridAreAtomicInlineBoxes(t *testing.T) {
	f := newDisplayFixture(t, func(w *Window) AnyElement {
		block := Div().ID("root").DebugSelector("root").SizeFull().Display(DisplayBlock).
			Child(Div().ID("ib1").DebugSelector("ib1").InlineBlock().Size(Px(40), Px(30)).Bg(Rgb(0x222222))).
			Child(Div().ID("ib2").DebugSelector("ib2").InlineBlock().Size(Px(50), Px(20)).Bg(Rgb(0x333333))).
			Child(Div().ID("ig").DebugSelector("ig").InlineGrid().Size(Px(90), Px(30)).
				GridTemplateCols(GridTemplate{Repeat: 2, MinSize: GridTemplateMinZero}).
				Child(Div().ID("g1").DebugSelector("g1").Size(Px(45), Px(30)).Bg(Rgb(0x444444))).
				Child(Div().ID("g2").DebugSelector("g2").Size(Px(45), Px(30)).Bg(Rgb(0x555555))))
		return block.IntoElement()
	})
	f.park()
	// Two atomic boxes at the text positions on one row: ib1 at x=0,
	// ib2 right after it (x=40). A row of only boxes is as tall as the
	// tallest baseline-aligned box (both sit with their bottoms on the
	// row's baseline — the 30px box sets the row), so ib2 (20px) hangs
	// 10px below the baseline: y=10.
	f.wantBox("ib1", 0, 0, 40, 30)
	f.wantBox("ib2", 40, 10, 50, 20)
	// The inline-grid follows at x=90 as one atomic box...
	f.wantBox("ig", 90, 0, 90, 30)
	// ...with its children on the 45px grid columns inside it.
	f.wantBox("g1", 90, 0, 45, 30)
	f.wantBox("g2", 135, 0, 45, 30)
}

// TestOverflowAutoReservesOnlyWhileOverflowing pins the overflow: auto
// policy (spec story 14): a scroller whose content overflows shows a
// scrollbar (the layout reserves the classic 15px gutter on the
// inline-end side, so a full-width child narrows), one whose content
// fits shows none (no reservation), and scrollbar-gutter: stable
// reserves even while nothing shows. The reservation settles one frame
// after the content: frame one lays out unreserved and the prepaint's
// detection notifies the redraw (Chromium's post-layout relayout,
// UpdateAfterOverflowRecalc -> SetNeedsLayoutAndFullPaintInvalidation,
// paint_layer_scrollable_area.cc:1480-1500).
func TestOverflowAutoReservesOnlyWhileOverflowing(t *testing.T) {
	scroller := func(children ...*DivElement) *DivElement {
		s := Div().ID("scroller").DebugSelector("scroller").
			Size(Px(240), Px(300)).FlexCol().OverflowYAuto().
			Child(Div().ID("filler").DebugSelector("filler").H(Px(30)).FlexShrink0().Bg(Rgb(0xe2e8f0)))
		for _, c := range children {
			s = s.Child(c)
		}
		return s
	}
	tall := Div().ID("tall").DebugSelector("tall").H(Px(570)).FlexShrink0().Bg(Rgb(0xcbd5e1))

	// Fits: no scrollbar, no reservation — the child keeps the full 240.
	f := newDisplayFixture(t, func(w *Window) AnyElement {
		return scroller().IntoElement()
	})
	f.park()
	f.wantBox("filler", 0, 0, 240, 30)

	// Overflows: the vertical scrollbar shows and reserves its gutter —
	// the 240-wide child narrows to 225.
	f2 := newDisplayFixture(t, func(w *Window) AnyElement {
		return scroller(tall).IntoElement()
	})
	f2.park()
	f2.wantBox("filler", 0, 0, 240-ClassicScrollbarWidth, 30)
	f2.wantBox("tall", 0, 30, 240-ClassicScrollbarWidth, 570)

	// Gutter stable, content fits: the gutter is reserved anyway, and
	// no scrollbar paints (Chromium: HasScrollbarGutters +
	// ComputeScrollbarsInternal, layout_box.cc:1477-1560).
	f3 := newDisplayFixture(t, func(w *Window) AnyElement {
		s := scroller()
		s = s.ScrollbarGutterStable()
		return s.IntoElement()
	})
	f3.park()
	f3.wantBox("filler", 0, 0, 240-ClassicScrollbarWidth, 30)

	// ScrollbarWidth overrides the classic width (an 8px scrollbar
	// reserves 8).
	f4 := newDisplayFixture(t, func(w *Window) AnyElement {
		s := scroller(tall)
		s = s.ScrollbarWidth(Px(8))
		return s.IntoElement()
	})
	f4.park()
	f4.wantBox("filler", 0, 0, 232, 30)
}

// TestOverflowAutoScrollbarOnlyWhileOverflowingPixels pins the painted
// scrollbar pair's geometry: the overflowing scroller paints a track
// in the border box's right column inside the border (the padding
// box's inline-end, 15 wide, the scroller's full height) and a thumb
// whose length is the visible fraction of the content (300 of 600 =>
// half the track) at the current scroll position.
func TestOverflowAutoScrollbarOnlyWhileOverflowingPixels(t *testing.T) {
	handle := NewScrollHandle()
	f := newDisplayFixture(t, func(w *Window) AnyElement {
		root := Div().ID("root").SizeFull().
			Child(Div().ID("scroller").DebugSelector("scroller").
				Size(Px(240), Px(300)).FlexCol().OverflowYAuto().TrackScroll(handle).
				Child(Div().ID("filler").DebugSelector("filler").H(Px(30)).FlexShrink0().Bg(Rgb(0xe2e8f0))).
				Child(Div().ID("tall").DebugSelector("tall").H(Px(570)).FlexShrink0().Bg(Rgb(0xcbd5e1))))
		return root.IntoElement()
	})
	f.park()
	// The auto-scrollbar observable: the child narrowing (the layout
	// half of the pair — the gutter reservation) —
	f.wantBox("filler", 0, 0, 240-ClassicScrollbarWidth, 30)

	// — and the paint facts: WindowAutoScrollbarFacts reports the
	// scroller's pair for the last frame.
	allFacts := WindowAutoScrollbarFacts(f.window)
	if len(allFacts) != 1 {
		t.Fatalf("auto scrollbar facts = %+v, want one scroller's pair", allFacts)
	}
	facts := allFacts[0]
	if !facts.VerticalShown {
		t.Fatalf("auto scrollbar facts = %+v, want a shown vertical scrollbar", facts)
	}
	if got := facts.VerticalTrack; got != (Bounds{Origin: Point{X: 240 - ClassicScrollbarWidth, Y: 0}, Size: Size{Width: ClassicScrollbarWidth, Height: 300}}) {
		t.Errorf("vertical track = %v, want (225,0) 15x300", got)
	}
	// Thumb: the slot between the buttons (300 - 2x15 - 2x3 = 264) —
	// the visible fraction 300/600 makes half of it — at the slot's
	// start (scroll 0, inset 3 past the first button).
	if got := facts.VerticalThumb; got != (Bounds{Origin: Point{X: 240 - ClassicScrollbarWidth + 3, Y: 15 + 3}, Size: Size{Width: 9, Height: 132}}) {
		t.Errorf("vertical thumb = %v, want (228,18) 9x132", got)
	}
	if facts.HorizontalShown {
		t.Errorf("horizontal scrollbar shows for a vertical-only overflow: %+v", facts)
	}
}

// TestOverflowAutoScrollsWhenOverflowing pins that an overflowing auto
// scroller actually scrolls (the wheel listener and the clamp keep the
// auto axis scrollable; a fitting one has range zero and never moves).
func TestOverflowAutoScrollsWhenOverflowing(t *testing.T) {
	handle := NewScrollHandle()
	f := newDisplayFixture(t, func(w *Window) AnyElement {
		root := Div().ID("root").SizeFull().
			Child(Div().ID("scroller").DebugSelector("scroller").
				Size(Px(240), Px(300)).FlexCol().OverflowYAuto().TrackScroll(handle).
				Child(Div().ID("filler").DebugSelector("filler").H(Px(30)).FlexShrink0().Bg(Rgb(0xe2e8f0))).
				Child(Div().ID("tall").DebugSelector("tall").H(Px(570)).FlexShrink0().Bg(Rgb(0xcbd5e1))))
		return root.IntoElement()
	})
	f.park()
	f.wantBox("filler", 0, 0, 240-ClassicScrollbarWidth, 30)
	// The scroll max: content 600 (narrowed) vs the 300 port => 300.
	// SetOffset clamps at the next prepaint (the port's clamp is the
	// scroll's single source of range truth).
	handle.SetOffset(Point{X: 0, Y: -999})
	f.draw()
	// At the end: the filler (the first 30px) has scrolled away above
	// the port, the tall box's top at 30-300 = -270.
	if b := f.at("tall"); b.Origin.Y != -270 {
		t.Errorf("tall at the end = %v, want y -270", b)
	}
	if max := handle.MaxOffset(); max.Y != 300 {
		t.Errorf("max offset = %v, want 300", max)
	}
}

// TestOverflowAutoFitsNeverScrolls pins the fitting half of the policy:
// the clamp's range is zero, so a keyboard scroll does nothing.
func TestOverflowAutoFitsNeverScrolls(t *testing.T) {
	handle := NewScrollHandle()
	f := newDisplayFixture(t, func(w *Window) AnyElement {
		root := Div().ID("root").SizeFull().
			Child(Div().ID("scroller").DebugSelector("scroller").
				Size(Px(240), Px(300)).FlexCol().OverflowYAuto().TrackScroll(handle).
				Child(Div().ID("filler").DebugSelector("filler").Size(Px(240), Px(30)).FlexShrink0().Bg(Rgb(0xe2e8f0))))
		return root.IntoElement()
	})
	f.park()
	// A fitting auto scroller has range zero: the clamp pins the offset
	// and nothing moves.
	handle.SetOffset(Point{X: 0, Y: -999})
	f.draw()
	f.wantBox("filler", 0, 0, 240, 30)
	if max := handle.MaxOffset(); max.Y != 0 {
		t.Errorf("max offset = %v, want 0", max)
	}
}

// TestStickyInsideHiddenScrollerStaysNatural pins ticket 34's closed
// limitation: an overflow: hidden box IS a scroll container per CSS
// (ComputedStyle::IsScrollContainer, computed_style.h:1988-1993), so a
// sticky inside it resolves against IT — the nearest one — and never
// sticks (its scrollport never moves), even while an outer scroller
// scrolls. Before ticket 36, the hidden box pushed no scope, so the
// sticky bound to the OUTER scroller and pinned when it scrolled.
func TestStickyInsideHiddenScrollerStaysNatural(t *testing.T) {
	handle := NewScrollHandle()
	build := func(w *Window) AnyElement {
		root := Div().ID("root").SizeFull()
		outer := Div().ID("outer").DebugSelector("outer").
			Size(Px(300), Px(400)).FlexCol().OverflowYScroll().TrackScroll(handle).
			Child(Div().Size(Px(300), Px(40)).FlexShrink0().Bg(Rgb(0xfca5a5))).
			Child(Div().ID("hidden").DebugSelector("hidden").Size(Px(300), Px(300)).FlexCol().FlexShrink0().OverflowHidden().
				Child(Div().ID("hdr").DebugSelector("hdr").Sticky().Top(Px(12)).
					Size(Px(300), Px(30)).FlexShrink0().Bg(Rgb(0x2563eb))).
				Child(Div().Size(Px(300), Px(500)).FlexShrink0().Bg(Rgb(0xe2e8f0)))).
			Child(Div().Size(Px(300), Px(500)).FlexShrink0().Bg(Rgb(0xcbd5e1)))
		return root.Child(outer).IntoElement()
	}
	f := newDisplayFixture(t, build)
	f.park()
	// Natural position: the sticky header sits at the hidden scroller's
	// flow origin (0 inside it, 40 into the outer scroller), and the
	// top: 12 constraint applies even without scrolling — the box pins
	// 12 below the (static) scrollport top: 40 + 12 = 52.
	f.wantBox("hdr", 0, 52, 300, 30)
	// Scroll the outer scroller 200: the sticky header must NOT pin to
	// the outer scroller's top (its nearest scroll container is the
	// hidden one, whose scrollport never moves) — it scrolls away with
	// its box, exactly like Chrome: 40 - 200 + 12 = -148.
	handle.SetOffset(Point{X: 0, Y: -200})
	f.draw()
	f.wantBox("hdr", 0, -148, 300, 30)
}

// TestClassDisplayAndGutterClasses pins the new Tailwind classes: the
// display classes apply through the generated method switch, and
// scrollbar-gutter-stable sets the stable policy.
func TestClassDisplayAndGutterClasses(t *testing.T) {
	if err := CheckClasses("contents inline-block inline-grid flow-root scrollbar-gutter-stable overflow-auto"); err != nil {
		t.Fatalf("CheckClasses: %v", err)
	}
	d := Div().ID("d").Class("contents")
	if v, ok := d.StyleMut().Display.Get(); !ok || v != DisplayContents {
		t.Errorf("class contents display = %v, %v", v, ok)
	}
	d = Div().ID("d2").Class("inline-block")
	if v, ok := d.StyleMut().Display.Get(); !ok || v != DisplayInlineBlock {
		t.Errorf("class inline-block display = %v, %v", v, ok)
	}
	d = Div().ID("d3").Class("inline-grid")
	if v, ok := d.StyleMut().Display.Get(); !ok || v != DisplayInlineGrid {
		t.Errorf("class inline-grid display = %v, %v", v, ok)
	}
	d = Div().ID("d4").Class("flow-root")
	if v, ok := d.StyleMut().Display.Get(); !ok || v != DisplayFlowRoot {
		t.Errorf("class flow-root display = %v, %v", v, ok)
	}
	d = Div().ID("d5").Class("scrollbar-gutter-stable")
	if v, ok := d.StyleMut().ScrollbarGutter.Get(); !ok || v != ScrollbarGutterStable {
		t.Errorf("class scrollbar-gutter-stable = %v, %v", v, ok)
	}
}

// TestOverflowStyleRefinement pins the refinement surface: the
// ScrollbarGutter refinement composes with the class layer exactly like
// the builder, and a div carries the auto overflow refinement.
func TestOverflowStyleRefinement(t *testing.T) {
	viaClass := Div().ID("d").Class("scrollbar-gutter-stable overflow-y-auto")
	viaBuilder := Div().ID("d").ScrollbarGutterStable().OverflowYAuto()
	if !reflect.DeepEqual(*viaClass.StyleMut(), *viaBuilder.StyleMut()) {
		t.Errorf("class refinement = %+v; want the builder's %+v", *viaClass.StyleMut(), *viaBuilder.StyleMut())
	}
	style := DefaultStyle()
	style.refineBoxed(viaBuilder.Styled.storage(), &viaBuilder.styleBoxes)
	if style.ScrollbarGutter != ScrollbarGutterStable {
		t.Errorf("ScrollbarGutter = %v, want Stable", style.ScrollbarGutter)
	}
	if style.OverflowY != OverflowAuto {
		t.Errorf("OverflowY = %v, want Auto", style.OverflowY)
	}
	// Without the refinement: the pin's defaults, nothing changed.
	if plain := DefaultStyle(); plain.OverflowY != OverflowVisible || plain.ScrollbarGutter != ScrollbarGutterAuto {
		t.Errorf("plain defaults = %v, %v", plain.OverflowY, plain.ScrollbarGutter)
	}
}
