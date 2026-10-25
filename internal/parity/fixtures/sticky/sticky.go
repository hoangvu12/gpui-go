//go:build windows

// Package sticky is ticket 34's fixture for position: sticky (spec
// L3): sticky elements stick within their scroll container — the
// offset applied at prepaint, updating on scroll without re-layout —
// with a top header pinned at several scroll offsets (including the
// pushed-out state past its containing block), a bottom-sticky item
// pinned then released, a left-sticky column in a horizontal
// scroller, and several sibling section headers replacing each other.
// The pages are text-free geometry (the boxpaint discipline), so Tier
// C carries no font rasterization noise; every scroll step is the
// keyboard-scroll default action (40 CSS px per arrow,
// cc/input/scroll_utils.h:20) over a focusable target clicked first,
// like the keyorder scroll fixtures.
//
// Chromium computes the sticky offset the way these fixtures record:
// the constraints against the nearest scroll container
// (LayoutBoxModelObject::ComputeStickyPositionConstraints,
// core/layout/layout_box_model_object.cc:548-717; the scrollport is
// the scroller's content box, ComputeStickyConstrainingRect,
// core/layout/layout_box.cc:4312-4322; the containing block is the
// flow block's content rect contracted by the sticky's margins, or
// the scroller's scrollable overflow when the sticky is its direct
// child), the per-axis clamp math in
// core/page/scrolling/sticky_position_scrolling_constraints.cc:65-144
// (top/left overriding bottom/right, both insets over-constrained
// reducing the end inset), applied as a paint-time translation
// (paint_property_tree_builder.cc:922-935) that getBoundingClientRect
// and offsetTop carry — so the recorded boxes include the offset, and
// the port's debug bounds must too.
package sticky

import (
	"fmt"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The pages' numbers, shared by the HTML and the gpui views (the
// tracer's discipline: one set of constants builds both sides).
const (
	windowWidth  = 640.0
	windowHeight = 400.0

	// The vertical page: a 240x300 scroller at (20, 20) — port y
	// [20, 320] — over 816px of content (scroll max 516): a 40px focus
	// row, a 200px section whose 30px sticky header carries top: 12
	// (natural content [40, 70]), a 500px spacer, a 36px sticky footer
	// with bottom: 8 (natural content [740, 776]) and a 40px tail.
	vScrollerW = 240.0
	vScrollerH = 300.0
	vScrollerX = 20.0
	vScrollerY = 20.0
	focusH     = 40.0
	sectionH   = 200.0
	headerH    = 30.0
	barH       = 10.0
	fillerH    = 170.0
	spacerH    = 500.0
	footerH    = 36.0
	tailH      = 40.0
	topInset   = 12.0
	botInset   = 8.0

	// The horizontal page: a 320x160 scroller at (20, 60) over 800px
	// of content (scroll max 480): a 40px focus column, a 60px sticky
	// column with left: 14 and a 700px filler.
	hScrollerW = 320.0
	hScrollerH = 160.0
	hScrollerX = 20.0
	hScrollerY = 60.0
	hFocusW    = 40.0
	hColW      = 60.0
	hFillerW   = 700.0
	leftInset  = 14.0

	// The sections page: the same 240x300 scroller over 680px of
	// content (scroll max 380): a 40px focus row then four 160px
	// sections, each with a 24px sticky header (top: 0) over a 136px
	// filler. At scroll 200 (five arrows) the first header's bottom
	// meets the scroller's top edge exactly as the second header's top
	// reaches it — the replacement handoff.
	sSectionH = 160.0
	sHeaderH  = 24.0
	sFillerH  = 136.0
)

// The palette, shared by the pages and the views.
const (
	pageBackground = 0xffffff
	scrollerBg     = 0xf1f5f9
	focusBg        = 0xfca5a5
	headerBg       = 0x2563eb
	barBg          = 0x93c5fd
	fillerBg       = 0xe2e8f0
	spacerBg       = 0xcbd5e1
	footerBg       = 0x16a34a
	tailBg         = 0x94a3b8
)

// tolerance is the flat-color pixel gate: text-free geometry, so the
// correct view is pixel-exact (0.0 everywhere); the deliberately-wrong
// view (sticky treated as relative) moves the stuck boxes and measures
// far above. Verified by sticky_test.go. Provisional (research 19's
// calibration has not run). See docs/browser-parity-harness.md.
var tolerance = parity.PixelTolerance{
	MaxMeanChannelDiff:    0.02,
	MaxROIMeanChannelDiff: 0.1,
	MaxHotPixelFraction:   0.002,
}

// vBoxIDs are the vertical page's compared boxes.
var vBoxIDs = []string{
	"root", "scroller", "focus", "section-a", "header-a", "bar",
	"filler-a", "spacer", "footer", "tail",
}

// TopFixture is the vertical page at scroll 0: the header at its
// natural position (the inset region has not reached it) and the
// bottom-sticky footer already pinned 8px above the scroller's bottom
// edge (its natural position is below the inset-adjusted port).
var TopFixture = parity.Fixture{
	Name:   "sticky-top",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   verticalPage,
	View:   verticalView,
	BoxIDs: vBoxIDs,
	Tolerance: parity.PixelTolerance{
		MaxMeanChannelDiff:    0.02,
		MaxROIMeanChannelDiff: 0.1,
		MaxHotPixelFraction:   0.002,
	},
}

// TopScrolledFixture is the vertical page scrolled 120 (three arrows):
// the header pinned 12px below the scroller's top edge, the bar child
// riding along, the footer still pinned.
var TopScrolledFixture = parity.Fixture{
	Name:   "sticky-top-scrolled",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   verticalPage,
	Steps: []parity.Step{
		parity.Click(vScrollerX+vScrollerW/2, vScrollerY+focusH/2),
		parity.Key("arrowdown"),
		parity.Key("arrowdown"),
		parity.Key("arrowdown"),
	},
	BoxIDs:         vBoxIDs,
	ViewWithWindow: VerticalScrolledView,
	Tolerance:      tolerance,
}

// TopPastFixture is the vertical page scrolled to the end (516, the
// End key): the header pushed past its containing block and scrolled
// away with its section, and the footer released to its natural
// position (its end has risen above the inset line).
var TopPastFixture = parity.Fixture{
	Name:   "sticky-top-past",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   verticalPage,
	Steps: []parity.Step{
		parity.Click(vScrollerX+vScrollerW/2, vScrollerY+focusH/2),
		parity.Key("end"),
	},
	BoxIDs:         vBoxIDs,
	ViewWithWindow: VerticalScrolledView,
	Tolerance:      tolerance,
}

// LeftFixture is the horizontal page scrolled 120 (three rights): the
// left-sticky column pinned 14px inside the scroller's left edge.
var LeftFixture = parity.Fixture{
	Name:   "sticky-left",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   horizontalPage,
	Steps: []parity.Step{
		parity.Click(hScrollerX+hFocusW/2, hScrollerY+hScrollerH/2),
		parity.Key("arrowright"),
		parity.Key("arrowright"),
		parity.Key("arrowright"),
	},
	BoxIDs:         []string{"root", "hscroller", "hfocus", "col", "hfiller"},
	ViewWithWindow: LeftView,
	Tolerance:      tolerance,
}

// SectionsFixture is the sections page scrolled 200 (five arrows):
// the first header pushed out (its bottom flush with the scroller's
// top edge), the second at its natural position about to pin, the
// third and fourth untouched — the replacement handoff.
var SectionsFixture = parity.Fixture{
	Name:   "sticky-sections",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   sectionsPage,
	Steps: []parity.Step{
		parity.Click(vScrollerX+vScrollerW/2, vScrollerY+focusH/2),
		parity.Key("arrowdown"),
		parity.Key("arrowdown"),
		parity.Key("arrowdown"),
		parity.Key("arrowdown"),
		parity.Key("arrowdown"),
	},
	BoxIDs: []string{
		"root", "scroller", "focus",
		"s1", "h1", "f1", "s2", "h2", "f2", "s3", "h3", "f3", "s4", "h4", "f4",
	},
	ViewWithWindow: SectionsView,
	Tolerance:      tolerance,
}

func init() {
	parity.Register(TopFixture)
	parity.Register(TopScrolledFixture)
	parity.Register(TopPastFixture)
	parity.Register(LeftFixture)
	parity.Register(SectionsFixture)
}

// verticalPage is the vertical fixtures' page.
var verticalPage = `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<style>
	html, body {
		margin: 0;
		padding: 0;
		background: #ffffff;
	}
	body {
		width: ` + f2s(windowWidth) + `px;
		height: ` + f2s(windowHeight) + `px;
		/* The pin's default display, pinned on the page too. */
		display: flex;
		align-items: flex-start;
	}
	#scroller {
		box-sizing: border-box;
		width: ` + f2s(vScrollerW) + `px;
		height: ` + f2s(vScrollerH) + `px;
		margin-left: ` + f2s(vScrollerX) + `px;
		margin-top: ` + f2s(vScrollerY) + `px;
		display: flex;
		flex-direction: column;
		overflow-y: scroll;
		/* The port draws no scrollbars and reserves no gutter
		   (browser-core ticket 36 owns the gutter). */
		scrollbar-width: none;
		background: #f1f5f9;
	}
	#scroller > div {
		flex-shrink: 0;
		box-sizing: border-box;
	}
	#focus {
		width: ` + f2s(vScrollerW) + `px;
		height: ` + f2s(focusH) + `px;
		background: #fca5a5;
		outline: none;
	}
	#section-a {
		width: ` + f2s(vScrollerW) + `px;
		height: ` + f2s(sectionH) + `px;
		display: flex;
		flex-direction: column;
		background: #ffffff;
	}
	#header-a {
		/* The behaviour under test. */
		position: sticky;
		top: ` + f2s(topInset) + `px;
		width: ` + f2s(vScrollerW) + `px;
		height: ` + f2s(headerH) + `px;
		background: #2563eb;
	}
	#bar {
		width: ` + f2s(vScrollerW) + `px;
		height: ` + f2s(barH) + `px;
		background: #93c5fd;
	}
	#filler-a {
		width: ` + f2s(vScrollerW) + `px;
		height: ` + f2s(fillerH) + `px;
		background: #e2e8f0;
	}
	#spacer {
		width: ` + f2s(vScrollerW) + `px;
		height: ` + f2s(spacerH) + `px;
		background: #cbd5e1;
	}
	#footer {
		position: sticky;
		bottom: ` + f2s(botInset) + `px;
		width: ` + f2s(vScrollerW) + `px;
		height: ` + f2s(footerH) + `px;
		background: #16a34a;
	}
	#tail {
		width: ` + f2s(vScrollerW) + `px;
		height: ` + f2s(tailH) + `px;
		background: #94a3b8;
	}
</style>
</head>
<body data-id="root">
	<div id="scroller" data-id="scroller">
		<div id="focus" data-id="focus" tabindex="0"></div>
		<div id="section-a" data-id="section-a">
			<div id="header-a" data-id="header-a">
				<div id="bar" data-id="bar"></div>
			</div>
			<div id="filler-a" data-id="filler-a"></div>
		</div>
		<div id="spacer" data-id="spacer"></div>
		<div id="footer" data-id="footer"></div>
		<div id="tail" data-id="tail"></div>
	</div>
</body>
</html>
`

// horizontalPage is the left fixture's page.
var horizontalPage = `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<style>
	html, body {
		margin: 0;
		padding: 0;
		background: #ffffff;
	}
	body {
		width: ` + f2s(windowWidth) + `px;
		height: ` + f2s(windowHeight) + `px;
		display: flex;
		align-items: flex-start;
	}
	#hscroller {
		box-sizing: border-box;
		width: ` + f2s(hScrollerW) + `px;
		height: ` + f2s(hScrollerH) + `px;
		margin-left: ` + f2s(hScrollerX) + `px;
		margin-top: ` + f2s(hScrollerY) + `px;
		display: flex;
		overflow-x: scroll;
		scrollbar-width: none;
		background: #f1f5f9;
	}
	#hscroller > div {
		flex-shrink: 0;
		box-sizing: border-box;
	}
	#hfocus {
		width: ` + f2s(hFocusW) + `px;
		height: ` + f2s(hScrollerH) + `px;
		background: #fca5a5;
		outline: none;
	}
	#col {
		position: sticky;
		left: ` + f2s(leftInset) + `px;
		width: ` + f2s(hColW) + `px;
		height: ` + f2s(hScrollerH) + `px;
		background: #2563eb;
	}
	#hfiller {
		width: ` + f2s(hFillerW) + `px;
		height: ` + f2s(hScrollerH) + `px;
		background: #cbd5e1;
	}
</style>
</head>
<body data-id="root">
	<div id="hscroller" data-id="hscroller">
		<div id="hfocus" data-id="hfocus" tabindex="0"></div>
		<div id="col" data-id="col"></div>
		<div id="hfiller" data-id="hfiller"></div>
	</div>
</body>
</html>
`

// sectionsPage is the sections fixture's page.
var sectionsPage = `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<style>
	html, body {
		margin: 0;
		padding: 0;
		background: #ffffff;
	}
	body {
		width: ` + f2s(windowWidth) + `px;
		height: ` + f2s(windowHeight) + `px;
		display: flex;
		align-items: flex-start;
	}
	#scroller {
		box-sizing: border-box;
		width: ` + f2s(vScrollerW) + `px;
		height: ` + f2s(vScrollerH) + `px;
		margin-left: ` + f2s(vScrollerX) + `px;
		margin-top: ` + f2s(vScrollerY) + `px;
		display: flex;
		flex-direction: column;
		overflow-y: scroll;
		scrollbar-width: none;
		background: #f1f5f9;
	}
	#scroller > div {
		flex-shrink: 0;
		box-sizing: border-box;
	}
	#focus {
		width: ` + f2s(vScrollerW) + `px;
		height: ` + f2s(focusH) + `px;
		background: #fca5a5;
		outline: none;
	}
	.section {
		width: ` + f2s(vScrollerW) + `px;
		height: ` + f2s(sSectionH) + `px;
		display: flex;
		flex-direction: column;
		background: #ffffff;
	}
	.hdr {
		position: sticky;
		top: 0;
		width: ` + f2s(vScrollerW) + `px;
		height: ` + f2s(sHeaderH) + `px;
	}
	.filler {
		width: ` + f2s(vScrollerW) + `px;
		height: ` + f2s(sFillerH) + `px;
		background: #e2e8f0;
	}
	#h1 { background: #2563eb; }
	#h2 { background: #16a34a; }
	#h3 { background: #d97706; }
	#h4 { background: #7c3aed; }
</style>
</head>
<body data-id="root">
	<div id="scroller" data-id="scroller">
		<div id="focus" data-id="focus" tabindex="0"></div>
		<div class="section" id="s1" data-id="s1">
			<div class="hdr" id="h1" data-id="h1"></div>
			<div class="filler" id="f1" data-id="f1"></div>
		</div>
		<div class="section" id="s2" data-id="s2">
			<div class="hdr" id="h2" data-id="h2"></div>
			<div class="filler" id="f2" data-id="f2"></div>
		</div>
		<div class="section" id="s3" data-id="s3">
			<div class="hdr" id="h3" data-id="h3"></div>
			<div class="filler" id="f3" data-id="f3"></div>
		</div>
		<div class="section" id="s4" data-id="s4">
			<div class="hdr" id="h4" data-id="h4"></div>
			<div class="filler" id="f4" data-id="f4"></div>
		</div>
	</div>
</body>
</html>
`

// ---------------------------------------------------------------------------
// The views
// ---------------------------------------------------------------------------

// verticalView is the vertical page's gpui twin for the scroll-0
// fixture (no steps, no focus).
func verticalView(_ *parity.EventLog) gpui.AnyElement {
	return verticalRoot(nil)
}

// VerticalScrolledView is the vertical page's twin for the scrolled
// fixtures: the focus target is clickable (the keyboard scroll
// follows focus), so the view carries a focus handle.
func VerticalScrolledView(log *parity.EventLog, w *gpui.Window, _ *gpui.App) gpui.AnyElement {
	return verticalRoot(scrollFocus(log, w, "focus"))
}

// LeftView is the horizontal page's twin.
func LeftView(log *parity.EventLog, w *gpui.Window, _ *gpui.App) gpui.AnyElement {
	focus := scrollFocus(log, w, "hfocus")
	root := gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Flex().
		AlignItems(gpui.AlignItemsFlexStart).
		Bg(gpui.Rgb(pageBackground))
	scroller := gpui.Div().
		ID("hscroller").
		DebugSelector("hscroller").
		Size(gpui.Px(hScrollerW), gpui.Px(hScrollerH)).
		Ml(gpui.Px(hScrollerX)).
		Mt(gpui.Px(hScrollerY)).
		Flex().
		OverflowXScroll().
		Bg(gpui.Rgb(scrollerBg)).
		Child(gpui.Div().Flex().
			ID("hfocus").
			DebugSelector("hfocus").
			Size(gpui.Px(hFocusW), gpui.Px(hScrollerH)).
			FlexShrink0().
			Bg(gpui.Rgb(focusBg)).
			TrackFocus(*focus)).
		Child(stickyBox("col", headerBg).
			Left(gpui.Px(leftInset)).
			Size(gpui.Px(hColW), gpui.Px(hScrollerH)).
			FlexShrink0()).
		Child(gpui.Div().Flex().
			ID("hfiller").
			DebugSelector("hfiller").
			Size(gpui.Px(hFillerW), gpui.Px(hScrollerH)).
			FlexShrink0().
			Bg(gpui.Rgb(spacerBg)))
	return root.Child(scroller).IntoElement()
}

// SectionsView is the sections page's twin.
func SectionsView(log *parity.EventLog, w *gpui.Window, _ *gpui.App) gpui.AnyElement {
	focus := scrollFocus(log, w, "focus")
	root := gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Flex().
		AlignItems(gpui.AlignItemsFlexStart).
		Bg(gpui.Rgb(pageBackground))
	scroller := gpui.Div().Flex().
		ID("scroller").
		DebugSelector("scroller").
		Size(gpui.Px(vScrollerW), gpui.Px(vScrollerH)).
		Ml(gpui.Px(vScrollerX)).
		Mt(gpui.Px(vScrollerY)).
		FlexCol().
		OverflowYScroll().
		Bg(gpui.Rgb(scrollerBg)).
		Child(gpui.Div().Flex().
			ID("focus").
			DebugSelector("focus").
			Size(gpui.Px(vScrollerW), gpui.Px(focusH)).
			FlexShrink0().
			Bg(gpui.Rgb(focusBg)).
			TrackFocus(*focus))
	sectionColors := []uint32{headerBg, footerBg, 0xd97706, 0x7c3aed}
	for i, color := range sectionColors {
		id := string(rune('1' + i))
		section := gpui.Div().Flex().
			ID("s"+id).
			DebugSelector("s"+id).
			Size(gpui.Px(vScrollerW), gpui.Px(sSectionH)).
			FlexCol().
			FlexShrink0().
			Bg(gpui.Rgb(pageBackground)).
			Child(stickyBox("h"+id, color).
				Top(gpui.Px(0)).
				Size(gpui.Px(vScrollerW), gpui.Px(sHeaderH))).
			Child(gpui.Div().Flex().
				ID("f"+id).
				DebugSelector("f"+id).
				Size(gpui.Px(vScrollerW), gpui.Px(sFillerH)).
				FlexShrink0().
				Bg(gpui.Rgb(fillerBg)))
		scroller = scroller.Child(section)
	}
	return root.Child(scroller).IntoElement()
}

// verticalRoot builds the vertical page's tree; focus is nil for the
// scroll-0 fixture (nothing clicks).
func verticalRoot(focus *gpui.FocusHandle) gpui.AnyElement {
	root := gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Flex().
		AlignItems(gpui.AlignItemsFlexStart).
		Bg(gpui.Rgb(pageBackground))
	focusRow := gpui.Div().Flex().
		ID("focus").
		DebugSelector("focus").
		Size(gpui.Px(vScrollerW), gpui.Px(focusH)).
		FlexShrink0().
		Bg(gpui.Rgb(focusBg))
	if focus != nil {
		focusRow = focusRow.TrackFocus(*focus)
	}
	section := gpui.Div().Flex().
		ID("section-a").
		DebugSelector("section-a").
		Size(gpui.Px(vScrollerW), gpui.Px(sectionH)).
		FlexCol().
		FlexShrink0().
		Bg(gpui.Rgb(pageBackground)).
		Child(stickyBox("header-a", headerBg).
			Top(gpui.Px(topInset)).
			Size(gpui.Px(vScrollerW), gpui.Px(headerH)).
			Child(gpui.Div().Flex().
				ID("bar").
				DebugSelector("bar").
				Size(gpui.Px(vScrollerW), gpui.Px(barH)).
				Bg(gpui.Rgb(barBg)))).
		Child(gpui.Div().Flex().
			ID("filler-a").
			DebugSelector("filler-a").
			Size(gpui.Px(vScrollerW), gpui.Px(fillerH)).
			FlexShrink0().
			Bg(gpui.Rgb(fillerBg)))
	scroller := gpui.Div().Flex().
		ID("scroller").
		DebugSelector("scroller").
		Size(gpui.Px(vScrollerW), gpui.Px(vScrollerH)).
		Ml(gpui.Px(vScrollerX)).
		Mt(gpui.Px(vScrollerY)).
		FlexCol().
		OverflowYScroll().
		Bg(gpui.Rgb(scrollerBg)).
		Child(focusRow).
		Child(section).
		Child(gpui.Div().Flex().
			ID("spacer").
			DebugSelector("spacer").
			Size(gpui.Px(vScrollerW), gpui.Px(spacerH)).
			FlexShrink0().
			Bg(gpui.Rgb(spacerBg))).
		Child(stickyBox("footer", footerBg).
			Bottom(gpui.Px(botInset)).
			Size(gpui.Px(vScrollerW), gpui.Px(footerH)).
			FlexShrink0()).
		Child(gpui.Div().Flex().
			ID("tail").
			DebugSelector("tail").
			Size(gpui.Px(vScrollerW), gpui.Px(tailH)).
			FlexShrink0().
			Bg(gpui.Rgb(tailBg)))
	return root.Child(scroller).IntoElement()
}

// WrongRelativeView is the deliberately-wrong view for the
// discrimination test (sticky_test.go): every sticky element is left
// at the port's default relative — the pre-ticket state, the elements
// at their natural flow positions, never sticking. The pinned and
// pushed-out boxes move back to their natural positions, so Tier A
// fails on exactly those boxes while the scroller, the focus row and
// the natural elements still match.
func WrongRelativeScrolledView(log *parity.EventLog, w *gpui.Window, _ *gpui.App) gpui.AnyElement {
	return verticalWrongRoot(scrollFocus(log, w, "focus"))
}

// verticalWrongRoot builds the vertical page's tree with the sticky
// elements at plain relative; focus carries the click target (the
// keyboard scroll must still run, so only the sticking is wrong).
func verticalWrongRoot(focus *gpui.FocusHandle) gpui.AnyElement {
	root := gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Flex().
		AlignItems(gpui.AlignItemsFlexStart).
		Bg(gpui.Rgb(pageBackground))
	section := gpui.Div().Flex().
		ID("section-a").
		DebugSelector("section-a").
		Size(gpui.Px(vScrollerW), gpui.Px(sectionH)).
		FlexCol().
		FlexShrink0().
		Bg(gpui.Rgb(pageBackground)).
		Child(gpui.Div().Flex().
			ID("header-a").
			DebugSelector("header-a").
			Relative().
			Size(gpui.Px(vScrollerW), gpui.Px(headerH)).
			Bg(gpui.Rgb(headerBg)).
			Child(gpui.Div().Flex().
				ID("bar").
				DebugSelector("bar").
				Size(gpui.Px(vScrollerW), gpui.Px(barH)).
				Bg(gpui.Rgb(barBg)))).
		Child(gpui.Div().Flex().
			ID("filler-a").
			DebugSelector("filler-a").
			Size(gpui.Px(vScrollerW), gpui.Px(fillerH)).
			FlexShrink0().
			Bg(gpui.Rgb(fillerBg)))
	scroller := gpui.Div().Flex().
		ID("scroller").
		DebugSelector("scroller").
		Size(gpui.Px(vScrollerW), gpui.Px(vScrollerH)).
		Ml(gpui.Px(vScrollerX)).
		Mt(gpui.Px(vScrollerY)).
		FlexCol().
		OverflowYScroll().
		Bg(gpui.Rgb(scrollerBg)).
		Child(gpui.Div().Flex().
			ID("focus").
			DebugSelector("focus").
			Size(gpui.Px(vScrollerW), gpui.Px(focusH)).
			FlexShrink0().
			Bg(gpui.Rgb(focusBg)).
			TrackFocus(*focus)).
		Child(section).
		Child(gpui.Div().Flex().
			ID("spacer").
			DebugSelector("spacer").
			Size(gpui.Px(vScrollerW), gpui.Px(spacerH)).
			FlexShrink0().
			Bg(gpui.Rgb(spacerBg))).
		Child(gpui.Div().Flex().
			ID("footer").
			DebugSelector("footer").
			Relative().
			Size(gpui.Px(vScrollerW), gpui.Px(footerH)).
			FlexShrink0().
			Bg(gpui.Rgb(footerBg))).
		Child(gpui.Div().Flex().
			ID("tail").
			DebugSelector("tail").
			Size(gpui.Px(vScrollerW), gpui.Px(tailH)).
			FlexShrink0().
			Bg(gpui.Rgb(tailBg)))
	return root.Child(scroller).IntoElement()
}

// stickyBox builds one sticky div with the given id and color; the
// caller adds the insets and size.
func stickyBox(id string, color uint32) *gpui.DivElement {
	return gpui.Div().Flex().
		ID(id).
		DebugSelector(id).
		Sticky().
		Bg(gpui.Rgb(color))
}

// scrollFocuses holds the scroll fixtures' focus handles, keyed on
// the window (the harness's mutable-state discipline: the view
// function re-runs every frame, so the handle must not).
var scrollFocuses = map[*gpui.Window]gpui.FocusHandle{}

// scrollFocus returns the window's focus handle for the click target,
// creating and tracking it on first use.
func scrollFocus(log *parity.EventLog, w *gpui.Window, id string) *gpui.FocusHandle {
	if focus, ok := scrollFocuses[w]; ok {
		return &focus
	}
	focus := gpui.NewFocusHandle(w).TabStop(true)
	scrollFocuses[w] = focus
	log.TrackFocus(focus, id)
	return &focus
}

// f2s renders a shared float constant for the pages' CSS.
func f2s(v float64) string {
	return fmt.Sprintf("%.0f", v)
}
