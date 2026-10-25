package gpui

import (
	"fmt"
	"testing"
)

// browser-core ticket 34 (spec L3): position: sticky. These tests pin
// the constraint math against Chromium's rules (verified in the
// research checkout; see sticky.go's header for the citations): a
// sticky top header pinned at several scroll offsets and pushed out
// past its containing block, a bottom-sticky item pinned then
// released, a left-sticky column in a horizontal scroller, the classic
// containing-block-is-the-scroller case, nested sticky (the inner box
// and containing block both carry the outer's offset), the margins
// contracting the containing block, the record translation (in flow,
// insets not relative offsets), the nearest-scroller rule, and the
// enum/class surface. The list-header hook has its own test below; the
// end-to-end Chrome-gated behaviour lives in
// internal/parity/fixtures/sticky.

// stickyFixture is one sticky test app: a window rendering build, with
// a scroll handle whose offset the test moves between draws.
type stickyFixture struct {
	t      *testing.T
	ta     *TestApp
	window *Window
	cx     *VisualTestContext
	handle *ScrollHandle
}

// newStickyFixture renders build once and returns the fixture.
func newStickyFixture(t *testing.T, build func(f *stickyFixture, w *Window, cx *Context[harnessView]) AnyElement) *stickyFixture {
	t.Helper()
	handle := NewScrollHandle()
	f := &stickyFixture{t: t, handle: &handle}
	ta, _, cx := addHarnessWindow(t, func(v *harnessView, w *Window, cx *Context[harnessView]) AnyElement {
		return build(f, w, cx)
	})
	f.ta = ta
	f.window = cx.Window()
	f.cx = cx
	return f
}

// draw redraws the frame (SetOffset changed only the offset cell: the
// next prepaint re-clamps and recomputes the sticky offsets without
// touching the layout tree — the ticket's prepaint-only rule).
func (f *stickyFixture) draw() {
	f.t.Helper()
	if _, err := DrawWindowFrame(f.window); err != nil {
		f.t.Fatalf("DrawWindowFrame: %v", err)
	}
}

// scrollTo sets the scroller's offset (negative y scrolls down) and
// redraws.
func (f *stickyFixture) scrollTo(y float32) {
	f.handle.SetOffset(Point{X: 0, Y: -y})
	f.draw()
}

// at returns one selector's debug bounds, failing when missing.
func (f *stickyFixture) at(selector string) Bounds {
	f.t.Helper()
	b, ok := f.cx.DebugBounds(selector)
	if !ok {
		f.t.Fatalf("selector %q has no bounds in the final frame", selector)
	}
	return b
}

// wantBox asserts one selector's bounds.
func (f *stickyFixture) wantBox(selector string, x, y, w, h float32) {
	f.t.Helper()
	b := f.at(selector)
	if b.Origin != (Point{X: x, Y: y}) || b.Size != (Size{Width: w, Height: h}) {
		f.t.Fatalf("%s bounds = (%g,%g) %gx%g, want (%g,%g) %gx%g", selector,
			b.Origin.X, b.Origin.Y, b.Size.Width, b.Size.Height, x, y, w, h)
	}
}

// The vertical fixture's numbers (the parity page's twin): a 240x300
// scroller at (100, 50) — port y [50, 350] — over 816px of content
// (scroll max 516): a 40px focus row, a 200px section whose 30px
// sticky header carries top: 12 (natural content [40, 70]), a 500px
// spacer, a 36px sticky footer with bottom: 8 (natural content
// [740, 776]) and a 40px tail.
const (
	stkScrollerX = 100.0
	stkScrollerY = 50.0
)

// verticalView builds the vertical scroller twin: focus row, section
// with the sticky header (and a 10px bar child, to pin that the
// children shift too), spacer, sticky footer, tail. The scroller's
// children pin flex-shrink: 0 (the harness discipline: the content
// must overflow, not shrink).
func verticalView(f *stickyFixture) AnyElement {
	root := Div().Flex().SizeFull().DebugSelector("root")
	scroller := Div().Flex().
		ID("scroller").
		DebugSelector("scroller").
		Size(Px(240), Px(300)).
		Ml(Px(stkScrollerX)).
		Mt(Px(stkScrollerY)).
		FlexCol().
		OverflowYScroll().
		TrackScroll(*f.handle).
		Child(Div().Flex().ID("focus").DebugSelector("focus").Size(Px(240), Px(40)).FlexShrink0().Bg(Rgb(0xfca5a5))).
		Child(Div().Flex().
			ID("section-a").
			DebugSelector("section-a").
			Size(Px(240), Px(200)).
			FlexCol().
			FlexShrink0().
			Child(Div().Flex().
				ID("header-a").
				DebugSelector("header-a").
				Sticky().
				Top(Px(12)).
				Size(Px(240), Px(30)).
				Bg(Rgb(0x2563eb)).
				Child(Div().Flex().ID("bar").DebugSelector("bar").Size(Px(240), Px(10)).Bg(Rgb(0x93c5fd)))).
			Child(Div().Flex().Size(Px(240), Px(170)).Bg(Rgb(0xe2e8f0)))).
		Child(Div().Flex().Size(Px(240), Px(500)).FlexShrink0().Bg(Rgb(0xcbd5e1))).
		Child(Div().Flex().
			ID("footer").
			DebugSelector("footer").
			Sticky().
			Bottom(Px(8)).
			Size(Px(240), Px(36)).
			FlexShrink0().
			Bg(Rgb(0x16a34a))).
		Child(Div().Flex().Size(Px(240), Px(40)).FlexShrink0().Bg(Rgb(0x94a3b8)))
	return root.Child(scroller).IntoElement()
}

// TestStickyTopHeaderAcrossScrollOffsets pins the top-sticky rule at
// the scroll offsets 0, 120, 240 and 516 (the max): at 0 the header is
// at its natural position (the inset region has not reached it); at
// 120 it is pinned 12px below the scroller's top edge (50+12=62); at
// 240 its containing block clamps it — the pushed-out state, the
// section's window rect [90-240, 290-240] = [-150, 50] leaves only
// 170px of room, so the 30px header sits at [20, 50] with its bottom
// on the section's end; at the max it has scrolled away above the
// scroller with its section ([-256, -226]). The header's child bar
// rides along, and the footer (bottom: 8) pins to [306, 342] — 8px
// above the port bottom 350 — until its natural end (826-516=310)
// rises above the inset line 342 at the max, where it settles into its
// natural place.
func TestStickyTopHeaderAcrossScrollOffsets(t *testing.T) {
	f := newStickyFixture(t, func(f *stickyFixture, w *Window, cx *Context[harnessView]) AnyElement {
		return verticalView(f)
	})
	f.draw()
	f.wantBox("scroller", stkScrollerX, stkScrollerY, 240, 300)
	f.wantBox("header-a", stkScrollerX, 90, 240, 30)
	f.wantBox("bar", stkScrollerX, 90, 240, 10)
	f.wantBox("footer", stkScrollerX, 306, 240, 36)

	f.scrollTo(120)
	f.wantBox("header-a", stkScrollerX, 62, 240, 30)
	f.wantBox("bar", stkScrollerX, 62, 240, 10)
	f.wantBox("footer", stkScrollerX, 306, 240, 36)

	f.scrollTo(240)
	f.wantBox("header-a", stkScrollerX, 20, 240, 30)
	f.wantBox("bar", stkScrollerX, 20, 240, 10)
	f.wantBox("footer", stkScrollerX, 306, 240, 36)

	f.scrollTo(516)
	f.wantBox("header-a", stkScrollerX, -256, 240, 30)
	f.wantBox("bar", stkScrollerX, -256, 240, 10)
	f.wantBox("footer", stkScrollerX, 274, 240, 36)
}

// TestStickyInsetsAreConstraintsNotRelativeOffsets pins the record
// translation: a sticky element lays out at its FLOW position — the
// insets never offset it (a relative element with the same insets
// would sit 12px lower). The 40px focus row above the header makes the
// natural position observable.
func TestStickyInsetsAreConstraintsNotRelativeOffsets(t *testing.T) {
	f := newStickyFixture(t, func(f *stickyFixture, w *Window, cx *Context[harnessView]) AnyElement {
		return verticalView(f)
	})
	f.draw()
	// The header's flow position: scroller top 50 + focus 40 = 90 (not
	// 90+12: the top inset is a scroll constraint, zeroed in the
	// record — see layout.go styleToRecord).
	f.wantBox("header-a", stkScrollerX, 90, 240, 30)
}

// TestStickySticksWithinScrollerNotAncestor pins the nearest-scroller
// rule: the sticky resolves against the INNERMOST scroll container
// (PaintLayer::ContainingScrollContainerLayer's walk) — a sticky
// inside an unscrolled inner scroller does not react to the OUTER
// scroller's offset, and pins against the inner port once the inner
// scrolls.
func TestStickySticksWithinScrollerNotAncestor(t *testing.T) {
	inner := NewScrollHandle()
	outer := NewScrollHandle()
	f := newStickyFixture(t, func(f *stickyFixture, w *Window, cx *Context[harnessView]) AnyElement {
		root := Div().Flex().SizeFull()
		// The inner scroller: 120x120 over 400px of content, whose
		// first child is a 20px sticky header (top: 0).
		innerScroller := Div().Flex().
			ID("inner").
			DebugSelector("inner").
			Size(Px(120), Px(120)).
			Ml(Px(30)).
			Mt(Px(30)).
			FlexCol().
			FlexShrink0().
			OverflowYScroll().
			TrackScroll(inner).
			Child(Div().Flex().ID("ih").DebugSelector("ih").Sticky().Top(Px(0)).Size(Px(120), Px(20)).FlexShrink0()).
			Child(Div().Flex().Size(Px(120), Px(380)).FlexShrink0())
		// The outer scroller wraps it with plenty of content.
		outerScroller := Div().Flex().
			ID("outer").
			DebugSelector("outer").
			Size(Px(400), Px(400)).
			FlexCol().
			OverflowYScroll().
			TrackScroll(outer).
			Child(Div().Flex().Size(Px(400), Px(500)).FlexShrink0()).
			Child(innerScroller).
			Child(Div().Flex().Size(Px(400), Px(500)).FlexShrink0())
		return root.Child(outerScroller).IntoElement()
	})
	// Scroll the OUTER 120 only: the inner scroller's window moves (to
	// y 380) but the inner port did not scroll, so the header stays at
	// the inner's content top.
	outer.SetOffset(Point{X: 0, Y: -120})
	f.draw()
	innerBox := f.at("inner")
	f.wantBox("ih", innerBox.Origin.X, innerBox.Origin.Y, 120, 20)
	// Scroll the INNER 100 too: the header pins at the inner's port
	// top (the inner's own window origin), whatever the outer does.
	inner.SetOffset(Point{X: 0, Y: -100})
	f.draw()
	f.wantBox("ih", innerBox.Origin.X, innerBox.Origin.Y, 120, 20)
}

// TestStickyLeftColumnInHorizontalScroller pins the horizontal axis: a
// left-sticky column (left: 14) in a 320x160 horizontal scroller at
// (20, 40) over 800px of content. At scroll 0 the column (natural x
// [60, 120]) is past the inset line 34, so it does not move; at 120 it
// pins at 20+14=34; at the max 480 it stays pinned (its containing
// block — the scroller's content extent — still has room).
func TestStickyLeftColumnInHorizontalScroller(t *testing.T) {
	f := newStickyFixture(t, func(f *stickyFixture, w *Window, cx *Context[harnessView]) AnyElement {
		root := Div().Flex().SizeFull()
		scroller := Div().
			ID("hscroller").
			DebugSelector("hscroller").
			Size(Px(320), Px(160)).
			Ml(Px(20)).
			Mt(Px(40)).
			Flex().
			OverflowXScroll().
			TrackScroll(*f.handle).
			Child(Div().Flex().Size(Px(40), Px(160)).FlexShrink0().Bg(Rgb(0xfca5a5))).
			Child(Div().Flex().
				ID("col").
				DebugSelector("col").
				Sticky().
				Left(Px(14)).
				Size(Px(60), Px(160)).
				FlexShrink0().
				Bg(Rgb(0x2563eb))).
			Child(Div().Flex().Size(Px(700), Px(160)).FlexShrink0().Bg(Rgb(0xcbd5e1)))
		return root.Child(scroller).IntoElement()
	})
	f.draw()
	f.wantBox("hscroller", 20, 40, 320, 160)
	f.wantBox("col", 60, 40, 60, 160)
	f.handle.SetOffset(Point{X: -120, Y: 0})
	f.draw()
	f.wantBox("col", 34, 40, 60, 160)
	f.handle.SetOffset(Point{X: -480, Y: 0})
	f.draw()
	f.wantBox("col", 34, 40, 60, 160)
}

// TestStickyDirectChildOfScroller pins the classic case: the sticky's
// flow containing block IS the scroller (its direct child), so the
// containing-block rect is the scroller's SCROLLABLE OVERFLOW extent
// (layout_box_model_object.cc:583-587), not the visible port: a
// 240x300 scroller over 700px of content whose 30px header (top: 0)
// starts at the content top pins for the whole range — the content
// below it (670px) never runs out within the 400px scroll range.
func TestStickyDirectChildOfScroller(t *testing.T) {
	f := newStickyFixture(t, func(f *stickyFixture, w *Window, cx *Context[harnessView]) AnyElement {
		root := Div().Flex().SizeFull()
		scroller := Div().Flex().
			ID("scroller").
			DebugSelector("scroller").
			Size(Px(240), Px(300)).
			Ml(Px(100)).
			Mt(Px(50)).
			FlexCol().
			OverflowYScroll().
			TrackScroll(*f.handle).
			Child(Div().Flex().ID("hdr").DebugSelector("hdr").Sticky().Top(Px(0)).Size(Px(240), Px(30)).FlexShrink0()).
			Child(Div().Flex().Size(Px(240), Px(670)).FlexShrink0())
		return root.Child(scroller).IntoElement()
	})
	f.draw()
	f.wantBox("hdr", 100, 50, 240, 30)
	f.scrollTo(200)
	f.wantBox("hdr", 100, 50, 240, 30)
	// The max scroll is 700-300=400: still pinned (the shift 400 never
	// reaches the available room 670).
	f.scrollTo(400)
	f.wantBox("hdr", 100, 50, 240, 30)
}

// TestNestedStickyInnerRidesOuter pins Chromium's nested rule
// (sticky_position_scrolling_constraints.cc:75-89): an outer sticky's
// offset shifts an inner sticky's box AND its containing block — the
// inner's flow containing block is the outer sticky element, so its
// recorded flow box already carries the outer's shift — while the
// scrollport does not move. A 200x300 scroller at (60, 30) whose
// section (400 tall, after a 20px block) holds a 100px outer sticky
// (top: 0) with a 50px inner sticky (top: 10): at scroll 150 the outer
// pins at [30, 130], and the inner pins at the inset line 30+10=40
// inside it.
func TestNestedStickyInnerRidesOuter(t *testing.T) {
	f := newStickyFixture(t, func(f *stickyFixture, w *Window, cx *Context[harnessView]) AnyElement {
		root := Div().Flex().SizeFull()
		scroller := Div().Flex().
			ID("scroller").
			Size(Px(200), Px(300)).
			Ml(Px(60)).
			Mt(Px(30)).
			FlexCol().
			OverflowYScroll().
			TrackScroll(*f.handle).
			Child(Div().Flex().Size(Px(200), Px(20)).FlexShrink0()).
			Child(Div().Flex().
				ID("section").
				DebugSelector("section").
				Size(Px(200), Px(400)).
				FlexCol().
				FlexShrink0().
				Child(Div().Flex().
					ID("outer").
					DebugSelector("outer").
					Sticky().
					Top(Px(0)).
					Size(Px(200), Px(100)).
					FlexCol().
					Child(Div().Flex().
						ID("inner").
						DebugSelector("inner").
						Sticky().
						Top(Px(10)).
						Size(Px(200), Px(50)))).
				Child(Div().Flex().Size(Px(200), Px(300)))).
			Child(Div().Flex().Size(Px(200), Px(500)).FlexShrink0())
		return root.Child(scroller).IntoElement()
	})
	// Scroll 0: nothing is stuck (the outer's natural top is the port
	// top 50, flush; the inner sits at 50).
	f.draw()
	f.wantBox("outer", 60, 50, 200, 100)
	f.wantBox("inner", 60, 50, 200, 50)
	// Scroll 150: the outer pins at the port top [30, 130]; the inner,
	// riding it, would sit at [30, 80] but its own top: 10 constraint
	// pins it at the inset line 40 — inside the outer's pinned rect,
	// which is also its clamped containing block.
	f.scrollTo(150)
	f.wantBox("outer", 60, 30, 200, 100)
	f.wantBox("inner", 60, 40, 200, 50)
}

// TestStickyMarginsContractContainingBlock pins
// layout_box_model_object.cc:610-615: the sticky element's own margins
// contract its containing-block rect, keeping the box a margin inside
// its flow rectangle. A 200x150 scroller at (60, 50) whose section
// (120 tall, after a 20px block) holds a 30px header (top: 0,
// margin-bottom 6): at scroll 400 the section's window rect is
// [70-400, 190-400] = [-330, -210] and the header's natural window
// rect [-324, -294] pins against the port top 50, but the
// margin-contracted containing block ends at -216, capping the shift
// so the header's bottom sits 6px above the section's end.
func TestStickyMarginsContractContainingBlock(t *testing.T) {
	f := newStickyFixture(t, func(f *stickyFixture, w *Window, cx *Context[harnessView]) AnyElement {
		root := Div().Flex().SizeFull()
		scroller := Div().Flex().
			ID("scroller").
			Size(Px(200), Px(150)).
			Ml(Px(60)).
			Mt(Px(50)).
			FlexCol().
			OverflowYScroll().
			TrackScroll(*f.handle).
			Child(Div().Flex().Size(Px(200), Px(20)).FlexShrink0()).
			Child(Div().Flex().
				ID("section").
				Size(Px(200), Px(120)).
				FlexCol().
				FlexShrink0().
				Child(Div().Flex().
					ID("hdr").
					DebugSelector("hdr").
					Sticky().
					Top(Px(0)).
					Mb(Px(6)).
					Size(Px(200), Px(30)))).
			Child(Div().Flex().Size(Px(200), Px(500)).FlexShrink0())
		return root.Child(scroller).IntoElement()
	})
	f.scrollTo(400)
	b := f.at("hdr")
	if want := float32(-216); b.Bottom() != want {
		t.Fatalf("header bottom = %g, want %g (the margin-contracted containing block's end: the section's end -210 minus the 6px margin)", b.Bottom(), want)
	}
	if b.Size != (Size{Width: 200, Height: 30}) {
		t.Fatalf("header size = %+v, want 200x30", b.Size)
	}
}

// TestStickyValue pins the enum surface: the value's name, parsing
// (round trip) and the Tailwind class mapping (the generated class
// switch maps "sticky" to Sticky()).
func TestStickyValue(t *testing.T) {
	if got := PositionSticky.String(); got != "Sticky" {
		t.Fatalf("PositionSticky.String() = %q, want %q", got, "Sticky")
	}
	for _, s := range []string{"Relative", "Absolute", "Static", "Sticky"} {
		p, err := ParsePosition(s)
		if err != nil || p.String() != s {
			t.Fatalf("ParsePosition(%q) = %v, %v", s, p, err)
		}
	}
	// The Tailwind class: "sticky" applies Sticky() — it left the
	// unsupported list (class.go) with this ticket, as "static" did
	// with 33.
	d := Div().Flex().Class("sticky")
	style := d.computeStyle()
	if style.Position != PositionSticky {
		t.Fatalf("class \"sticky\" leaves position %v, want Sticky", style.Position)
	}
}

// ---------------------------------------------------------------------------
// The list-header hook (StickyOffset + WithStickyScrollContainer)
// ---------------------------------------------------------------------------

// stickyHookList is the list-header hook's test double: a minimal
// uniform-list-shaped custom element (listelement.go's shape — items
// laid out as roots and prepainted at computed origins through
// PrepaintAt) that uses the ticket's public hook: it opens its own
// scroller scope with WithStickyScrollContainer and shifts each
// section header's origin by StickyOffset against the section's
// content rect. A header element is a plain Sticky() div laid out as a
// root, so the div path adds nothing (a root-laid node has no layout
// parent) — the hook's offset is the only one.
type stickyHookList struct {
	handle      ScrollHandle
	sections    int
	pitch       float32
	headerH     float32
	inset       float32
	listStyle   Style
	headerStyle Style
}

// hookFrame and hookPrepaint are the custom element's phase states.
type hookFrame struct {
	scroll *elementScroll
	items  []hookItem
}

type hookItem struct {
	element AnyElement
	origin  Point
}

type hookPrepaint struct {
	hitbox       *Hitbox
	scrollOffset Point
}

// ID implements Element.
func (l *stickyHookList) ID() (ElementID, bool) { return ElementID{}, false }

// SourceLocation implements Element.
func (l *stickyHookList) SourceLocation() *SourceLocation { return nil }

// RequestLayout implements Element: one fixed-size leaf node with the
// list's overflow style, and the scroll slice resolved from the
// handle.
func (l *stickyHookList) RequestLayout(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App) (LayoutID, hookFrame) {
	id, err := requestLayoutOf(w, l.listStyle)
	if err != nil {
		panic("gpui: stickyHookList request_layout: " + err.Error())
	}
	return id, hookFrame{scroll: resolveElementScroll(&l.handle, nil, global, w)}
}

// Prepaint implements Element: the scroller's clamp, then — inside
// WithStickyScrollContainer, the hook's scope — every section's header
// laid out as a root and prepainted at its shifted origin.
func (l *stickyHookList) Prepaint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *hookFrame, w *Window, app *App) hookPrepaint {
	state := hookPrepaint{}
	contentSize := Size{Width: bounds.Size.Width, Height: l.pitch * float32(l.sections)}
	WithContentMaskVoid(w, l.listStyle.OverflowMask(bounds, RemSize(w)), func(w *Window) {
		box := w.InsertHitbox(bounds, HitboxNormal)
		state.hitbox = &box
		// The hook list's own virtualized extent as a scroller with no
		// padding: the pin's range arithmetic (the test's list has no
		// layout node to read record facts from).
		scrollMax := Point{
			X: max32(roundToTwoDecimals(contentSize.Width-bounds.Size.Width), 0),
			Y: max32(roundToTwoDecimals(contentSize.Height-bounds.Size.Height), 0),
		}
		scrollOffset := layout.scroll.clamp(bounds, &l.listStyle, scrollMax, w)
		state.scrollOffset = scrollOffset
		WithStickyScrollContainer(w, &l.listStyle, bounds, layout.scroll.maxOffset, scrollOffset, func(w *Window) struct{} {
			for i := 0; i < l.sections; i++ {
				sectionTop := bounds.Origin.Y + l.pitch*float32(i) + scrollOffset.Y
				headerBox := Bounds{
					Origin: Point{X: bounds.Origin.X, Y: sectionTop},
					Size:   Size{Width: bounds.Size.Width, Height: l.headerH},
				}
				sectionCB := Bounds{
					Origin: Point{X: bounds.Origin.X, Y: sectionTop},
					Size:   Size{Width: bounds.Size.Width, Height: l.pitch},
				}
				sticky := StickyOffset(w, &l.headerStyle, headerBox, sectionCB)
				header := Div().Flex().
					ID(fmt.Sprintf("hook-h%d", i)).
					DebugSelector(fmt.Sprintf("hook-h%d", i)).
					Sticky().
					Top(Px(l.inset)).
					Size(Px(bounds.Size.Width), Px(l.headerH)).
					Bg(Rgb(0x2563eb)).IntoElement()
				header.LayoutAsRoot(AvailableSize{
					Width:  DefiniteAvailableSpace(bounds.Size.Width),
					Height: DefiniteAvailableSpace(l.headerH),
				}, w, app)
				origin := Point{X: headerBox.Origin.X + sticky.X, Y: headerBox.Origin.Y + sticky.Y}
				header.PrepaintAt(origin, w, app)
				layout.items = append(layout.items, hookItem{element: header, origin: origin})
			}
			return struct{}{}
		})
	})
	return state
}

// Paint implements Element: the items repaint at their recorded
// origins (the uniform list's WithAbsoluteElementOffsetVoid shape).
func (l *stickyHookList) Paint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *hookFrame, prepaint *hookPrepaint, w *Window, app *App) {
	WithContentMaskVoid(w, l.listStyle.OverflowMask(bounds, RemSize(w)), func(w *Window) {
		if prepaint.hitbox != nil {
			layout.scroll.paintListener(*prepaint.hitbox, &l.listStyle, w)
		}
		for _, item := range layout.items {
			WithAbsoluteElementOffsetVoid(w, item.origin, func(w *Window) {
				item.element.Paint(w, app)
			})
		}
	})
}

// newStickyHookList builds the hook list: sections of pitch 160 with
// 24px headers carrying top: 10, a 200x300 scroller at (40, 60).
func newStickyHookList(handle ScrollHandle) *stickyHookList {
	listStyle := DefaultStyle()
	listStyle.Size = LengthSize{Width: PxLength(200), Height: PxLength(300)}
	listStyle.OverflowY = OverflowScroll
	listStyle.Margin = LengthEdges{Left: PxLength(40), Top: PxLength(60)}
	headerStyle := DefaultStyle()
	headerStyle.Position = PositionSticky
	headerStyle.Inset = LengthEdges{Top: PxLength(10)}
	return &stickyHookList{
		handle:      handle,
		sections:    4,
		pitch:       160,
		headerH:     24,
		inset:       10,
		listStyle:   listStyle,
		headerStyle: headerStyle,
	}
}

// TestStickyListHeaderHook pins the ticket's hook on the uniform-list
// shape: WithStickyScrollContainer opens the custom scroller's scope,
// and StickyOffset's result shifts the header origins — at scroll 0
// section 0's header starts inside the inset region (the port top 60,
// inset 10 → line 70) and is shifted down immediately (Chromium's
// position-sticky-get-bounding-client-rect.html behaviour); at scroll
// 200 of the 640px content (max 340): section 0's header is pushed out
// (its bottom at the section's end 20), section 1's is pinned at the
// inset line 70, sections 2 and 3 stay natural (180 and 340).
func TestStickyListHeaderHook(t *testing.T) {
	handle := NewScrollHandle()
	f := newStickyFixture(t, func(f *stickyFixture, w *Window, cx *Context[harnessView]) AnyElement {
		root := Div().Flex().SizeFull()
		return root.Child(CustomElement[hookFrame, hookPrepaint](newStickyHookList(handle))).IntoElement()
	})
	f.draw()
	f.wantBox("hook-h0", 40, 70, 200, 24)
	f.wantBox("hook-h1", 40, 220, 200, 24)
	handle.SetOffset(Point{X: 0, Y: -200})
	f.draw()
	f.wantBox("hook-h0", 40, -4, 200, 24)
	f.wantBox("hook-h1", 40, 70, 200, 24)
	f.wantBox("hook-h2", 40, 180, 200, 24)
	f.wantBox("hook-h3", 40, 340, 200, 24)
}
