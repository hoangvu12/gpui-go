//go:build windows

// Package fixed is ticket 35's fixture for position: fixed (spec
// L10): a fixed box is placed relative to the window and painted above
// normal flow, regardless of its ancestors. One page carries the three
// cases:
//
//   - a fixed box inside a SCROLLED SCROLLER (a 200x200 overflow-y
//     scroller over 600px of content, scrolled 120 by three keyboard
//     line steps): the box stays at its window coordinates while the
//     content moves under it, and it is not clipped by the scroller —
//     it straddles the scroller's right edge (140..230 over the
//     scroller's 20..220);
//   - a fixed box inside a RELATIVE ANCESTOR holding a STATIC PARENT:
//     the box sits at its window coordinates, far from the chain that
//     contains its DOM position;
//   - a fixed box among a plain flow column's children, overlapping a
//     LATER IN-FLOW SIBLING: the fixed box paints above it (css2.1
//     Appendix E step 8 — a fixed box paints as a positioned element
//     of the root stacking context, above every in-flow box of the
//     whole tree).
//
// Chromium decides all three the way this fixture records: the
// containing block of a fixed element is the viewport —
// LayoutObject::ContainerForFixedPosition walks PAST every positioned
// ancestor to the LayoutView (core/layout/layout_object.cc:1885-1892),
// because nothing but a transform, filter or containment makes an
// ancestor a fixed container (ComputeIsFixedContainer,
// layout_object.cc:2009-2067) — so no positioned ancestor contains it
// and no ancestor scroll offset or overflow clip reaches it; and its
// paint parent is the root stacking context (Appendix E step 8), so it
// rides above all in-flow content. The page is text-free geometry (the
// boxpaint discipline), so Tier C carries no font rasterization noise;
// the fixture binds no listeners, so Tier B is empty. The scroll steps
// are the keyboard-scroll default action (40 CSS px per arrow,
// cc/input/scroll_utils.h:20) over a focusable row clicked first, like
// the keyorder scroll fixtures.
package fixed

import (
	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The page's numbers, shared by the HTML and the gpui view (the
// tracer's discipline: one set of constants builds both sides).
const (
	windowWidth  = 640.0
	windowHeight = 400.0

	// The scroller: 200x200 at (20, 20) over 600px of content (scroll
	// max 400). The content is a flex column of 40px rows; row1 is the
	// focusable click target.
	scrollerW    = 200.0
	scrollerH    = 200.0
	scrollerGapL = 20.0
	scrollerGapT = 20.0
	contentH     = 600.0
	rowH         = 40.0

	// fixed-a: the box inside the scroller's content, at window
	// (140, 60) 90x50 — straddling the scroller's right edge (its
	// viewport ends at x=220), so the clip escape is observable.
	fixedAX = 140.0
	fixedAY = 60.0
	fixedAW = 90.0
	fixedAH = 50.0

	// The escape chain: a relative ancestor 150x110 holding a static
	// parent 100x80, whose fixed box sits at window (300, 300) — far
	// below and right of the chain that contains it.
	ancestorW = 150.0
	ancestorH = 110.0
	parentW   = 100.0
	parentH   = 80.0
	fixedBX   = 300.0
	fixedBY   = 300.0
	fixedBW   = 60.0
	fixedBH   = 40.0

	// The paint-order case: a flow column 180x300 whose children are
	// row-a (50), row-b (100), the fixed box, row-c (50) and row-d
	// (100). The fixed box at window (470, 150) 60x40 overlaps row-c's
	// region — a LATER in-flow sibling it must paint above.
	stackW  = 180.0
	stackH  = 300.0
	rowAW   = 180.0
	rowAH   = 50.0
	rowBW   = 180.0
	rowBH   = 100.0
	rowCW   = 180.0
	rowCH   = 50.0
	rowDW   = 180.0
	rowDH   = 100.0
	fixedCX = 470.0
	fixedCY = 150.0
	fixedCW = 60.0
	fixedCH = 40.0

	// The flex row's gap (the port's default display is flex, pinned
	// on the page too) and the number of filler rows after row1.
	columnGap  = 40.0
	fillerRows = 14
)

// The palette, shared by the page and the view.
const (
	pageBackground = 0xffffff
	scrollerBg     = 0xf1f5f9
	row1Bg         = 0xfca5a5
	fillerBg       = 0xe2e8f0
	fixedABg       = 0xdc2626
	ancestorBg     = 0xf8fafc
	parentBg       = 0xe2e8f0
	fixedBBg       = 0x2563eb
	rowABg         = 0x94a3b8
	rowBBg         = 0x86efac
	rowCBg         = 0xfca5a5
	rowDBg         = 0xcbd5e1
	fixedCBg       = 0x7c3aed
)

// tolerance is the flat-color pixel gate: text-free geometry, so the
// correct view is pixel-exact (0.0 everywhere; the position and sticky
// fixtures' measured numbers). The deliberately-wrong view (the fixed
// boxes as absolute, the pre-ticket pin) moves three boxes and measures
// far above. Verified by fixed_test.go. Provisional (research 19's
// calibration has not run). See docs/browser-parity-harness.md.
var tolerance = parity.PixelTolerance{
	MaxMeanChannelDiff:    0.02,
	MaxROIMeanChannelDiff: 0.1,
	MaxHotPixelFraction:   0.002,
}

// Fixture is the position: fixed fixture: the scroller scrolled 120
// (three arrows) with the three fixed boxes at their window positions.
var Fixture = parity.Fixture{
	Name:   "fixed-window",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   page,
	Steps: []parity.Step{
		// Click the first row (20..220 x 20..60): the keyboard scroll
		// follows focus.
		parity.Click(120, 40),
		// Three arrows = 120 (the 40px line step): the content moves
		// under the fixed box, which stays.
		parity.Key("arrowdown"),
		parity.Key("arrowdown"),
		parity.Key("arrowdown"),
	},
	BoxIDs: []string{
		"root", "scroller", "content", "row1", "fixed-a",
		"ancestor", "static-parent", "fixed-b",
		"stack", "row-a", "row-b", "fixed-c", "row-c", "row-d",
	},
	ViewWithWindow: View,
	Tolerance:      tolerance,
}

func init() { parity.Register(Fixture) }

// page is the HTML twin of View.
var page = `<!doctype html>
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
		width: 640px;
		height: 400px;
		display: flex;
		align-items: flex-start;
		gap: 40px;
	}
	#scroller {
		box-sizing: border-box;
		width: 200px;
		height: 200px;
		margin: 20px 0 0 20px;
		overflow-y: scroll;
		scrollbar-width: none;
		background: #f1f5f9;
	}
	#content {
		box-sizing: border-box;
		width: 200px;
		height: 600px;
		display: flex;
		flex-direction: column;
	}
	#row1 {
		box-sizing: border-box;
		width: 200px;
		height: 40px;
		background: #fca5a5;
		outline: none;
	}
	.row {
		box-sizing: border-box;
		width: 200px;
		height: 40px;
		background: #e2e8f0;
	}
	/* The fixed box inside the scroller's content: placed at the
	   viewport's coordinates, un-clipped by the scroller and
	   un-scrolled by its offset (no transformed ancestor exists to
	   contain it, layout_object.cc:2009-2067). */
	#fixed-a {
		position: fixed;
		left: 140px;
		top: 60px;
		width: 90px;
		height: 50px;
		background: #dc2626;
	}
	#ancestor {
		box-sizing: border-box;
		width: 150px;
		height: 110px;
		position: relative;
		background: #f8fafc;
	}
	#static-parent {
		box-sizing: border-box;
		width: 100px;
		height: 80px;
		position: static;
		display: flex;
		background: #e2e8f0;
	}
	#fixed-b {
		position: fixed;
		left: 300px;
		top: 300px;
		width: 60px;
		height: 40px;
		background: #2563eb;
	}
	#stack {
		box-sizing: border-box;
		width: 180px;
		height: 300px;
		display: flex;
		flex-direction: column;
	}
	#row-a {
		box-sizing: border-box;
		width: 180px;
		height: 50px;
		background: #94a3b8;
	}
	#row-b {
		box-sizing: border-box;
		width: 180px;
		height: 100px;
		background: #86efac;
	}
	/* The fixed box among the flow column's children, BEFORE the later
	   row-c it must paint above (Appendix E step 8: the root stacking
	   context's positioned layer rides over all in-flow boxes). */
	#fixed-c {
		position: fixed;
		left: 470px;
		top: 150px;
		width: 60px;
		height: 40px;
		background: #7c3aed;
	}
	#row-c {
		box-sizing: border-box;
		width: 180px;
		height: 50px;
		background: #fca5a5;
	}
	#row-d {
		box-sizing: border-box;
		width: 180px;
		height: 100px;
		background: #cbd5e1;
	}
</style>
</head>
<body data-id="root">
	<div id="scroller" data-id="scroller">
		<div id="content" data-id="content">
			<div id="row1" data-id="row1" tabindex="0"></div>
			<div class="row"></div>
			<div class="row"></div>
			<div id="fixed-a" data-id="fixed-a"></div>
			<div class="row"></div>
			<div class="row"></div>
			<div class="row"></div>
			<div class="row"></div>
			<div class="row"></div>
			<div class="row"></div>
			<div class="row"></div>
			<div class="row"></div>
			<div class="row"></div>
			<div class="row"></div>
			<div class="row"></div>
			<div class="row"></div>
		</div>
	</div>
	<div id="ancestor" data-id="ancestor">
		<div id="static-parent" data-id="static-parent">
			<div id="fixed-b" data-id="fixed-b"></div>
		</div>
	</div>
	<div id="stack" data-id="stack">
		<div id="row-a" data-id="row-a"></div>
		<div id="row-b" data-id="row-b"></div>
		<div id="fixed-c" data-id="fixed-c"></div>
		<div id="row-c" data-id="row-c"></div>
		<div id="row-d" data-id="row-d"></div>
	</div>
</body>
</html>
`

// View is the gpui-go twin of the page: the scroller with its focusable
// first row and the fixed box inside its content, the escape chain,
// and the flow column with the fixed box above the later sibling.
func View(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return buildTree(log, w, app, false)
}

// WrongAbsolute is the deliberately-wrong view for the discrimination
// test: the three fixed boxes are left at the port's pre-ticket shape
// — absolute, resolving against their DOM ancestors (fixed-a against
// the scrolled content, fixed-b against the static chain's positioned
// ancestor, fixed-c against the flow column) — the exact state before
// ticket 35 (class.go's old advice: "use absolute inside a relative
// ancestor that fills the window"). Since ticket 76 flipped the default
// position to static, a plain absolute box under unpositioned ancestors
// escapes to the window root exactly like a fixed one, so the wrong
// view also positions the boxes' DOM ancestors (the content and the
// flow column are .Relative() there): the pre-35 pin IS "absolute
// inside positioned ancestors". All three boxes move: fixed-a scrolls
// with the content (and is clipped by the scroller), fixed-b lands on
// the ancestor's padding box, fixed-c falls off the window.
func WrongAbsolute(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return buildTree(log, w, app, true)
}

// buildTree renders the fixture's tree; absolute selects the
// deliberately-wrong view's plain-absolute boxes (with the boxes' DOM
// ancestors positioned, the pre-35 pin's shape — see WrongAbsolute).
func buildTree(log *parity.EventLog, w *gpui.Window, _ *gpui.App, absolute bool) gpui.AnyElement {
	focus := scrollFocusOf(log, w)
	fixedOrAbsolute := func(id string, color uint32) *gpui.DivElement {
		box := gpui.Div().ID(id).DebugSelector(id).Bg(gpui.Rgb(color))
		if absolute {
			return box.Absolute()
		}
		return box.Fixed()
	}
	root := gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Flex().
		AlignItems(gpui.AlignItemsFlexStart).
		Gap(gpui.Px(columnGap)).
		Bg(gpui.Rgb(pageBackground))
	content := gpui.Div().Flex().
		ID("content").
		DebugSelector("content").
		W(gpui.Px(scrollerW)).
		H(gpui.Px(contentH)).
		FlexCol()
	if absolute {
		// The pre-35 pin's shape: the wrong view's absolute boxes resolve
		// against their DOM ancestors, so those ancestors are positioned.
		content = content.Relative()
	}
	content = content.
		Child(gpui.Div().ID("row1").DebugSelector("row1").
			W(gpui.Px(scrollerW)).H(gpui.Px(rowH)).
			Bg(gpui.Rgb(row1Bg)).
			TrackFocus(*focus)).
		Child(fixedOrAbsolute("fixed-a", fixedABg).
			Left(gpui.Px(fixedAX)).
			Top(gpui.Px(fixedAY)).
			W(gpui.Px(fixedAW)).
			H(gpui.Px(fixedAH)))
	for i := 0; i < fillerRows; i++ {
		content = content.Child(gpui.Div().
			W(gpui.Px(scrollerW)).H(gpui.Px(rowH)).
			Bg(gpui.Rgb(fillerBg)))
	}
	scroller := gpui.Div().
		ID("scroller").
		DebugSelector("scroller").
		W(gpui.Px(scrollerW)).
		H(gpui.Px(scrollerH)).
		Ml(gpui.Px(scrollerGapL)).
		Mt(gpui.Px(scrollerGapT)).
		OverflowYScroll().
		TrackScroll(scrollHandleOf(w)).
		Bg(gpui.Rgb(scrollerBg)).
		Child(content)
	root.Child(scroller)
	root.Child(gpui.Div().
		ID("ancestor").
		DebugSelector("ancestor").
		W(gpui.Px(ancestorW)).
		H(gpui.Px(ancestorH)).
		Relative().
		Bg(gpui.Rgb(ancestorBg)).
		Child(gpui.Div().
			ID("static-parent").
			DebugSelector("static-parent").
			W(gpui.Px(parentW)).
			H(gpui.Px(parentH)).
			Static().
			Flex().
			Bg(gpui.Rgb(parentBg)).
			Child(fixedOrAbsolute("fixed-b", fixedBBg).
				Left(gpui.Px(fixedBX)).
				Top(gpui.Px(fixedBY)).
				W(gpui.Px(fixedBW)).
				H(gpui.Px(fixedBH)))))
	stack := gpui.Div().Flex().
		ID("stack").
		DebugSelector("stack").
		W(gpui.Px(stackW)).
		H(gpui.Px(stackH)).
		FlexCol()
	if absolute {
		stack = stack.Relative()
	}
	root.Child(stack.
		Child(gpui.Div().ID("row-a").DebugSelector("row-a").
			W(gpui.Px(rowAW)).H(gpui.Px(rowAH)).
			Bg(gpui.Rgb(rowABg))).
		Child(gpui.Div().ID("row-b").DebugSelector("row-b").
			W(gpui.Px(rowBW)).H(gpui.Px(rowBH)).
			Bg(gpui.Rgb(rowBBg))).
		Child(fixedOrAbsolute("fixed-c", fixedCBg).
			Left(gpui.Px(fixedCX)).
			Top(gpui.Px(fixedCY)).
			W(gpui.Px(fixedCW)).
			H(gpui.Px(fixedCH))).
		Child(gpui.Div().ID("row-c").DebugSelector("row-c").
			W(gpui.Px(rowCW)).H(gpui.Px(rowCH)).
			Bg(gpui.Rgb(rowCBg))).
		Child(gpui.Div().ID("row-d").DebugSelector("row-d").
			W(gpui.Px(rowDW)).H(gpui.Px(rowDH)).
			Bg(gpui.Rgb(rowDBg))))
	return root.IntoElement()
}

// fixtureStates holds the per-window focus and scroll handles (the
// harness's mutable-state discipline: the view function re-runs every
// frame, the handles must not).
var fixtureStates = map[*gpui.Window]fixtureState{}

// fixtureState is one replayed window's handles.
type fixtureState struct {
	focus  gpui.FocusHandle
	scroll gpui.ScrollHandle
}

// scrollFocusOf returns the window's focus handle for the click target,
// creating and tracking it on first use.
func scrollFocusOf(log *parity.EventLog, w *gpui.Window) *gpui.FocusHandle {
	state, ok := fixtureStates[w]
	if !ok {
		state = fixtureState{
			focus:  gpui.NewFocusHandle(w).TabStop(true),
			scroll: gpui.NewScrollHandle(),
		}
		fixtureStates[w] = state
		log.TrackFocus(state.focus, "row1")
	}
	return &state.focus
}

// scrollHandleOf returns the window's scroll handle.
func scrollHandleOf(w *gpui.Window) gpui.ScrollHandle {
	if state, ok := fixtureStates[w]; ok {
		return state.scroll
	}
	// The view always resolves the focus handle first (the same first
	// build), so this only runs for a window that never drew.
	state := fixtureState{scroll: gpui.NewScrollHandle()}
	fixtureStates[w] = state
	return state.scroll
}
