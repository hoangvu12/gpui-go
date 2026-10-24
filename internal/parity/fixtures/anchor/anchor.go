//go:build windows

// Package anchor is ticket 25's fixture set for element-anchored
// positioning (CSS anchor positioning: position-anchor, the
// side/align/gap placement of position-area / anchor(), the
// position-try-fallbacks flip tactics, the shift clamp, and
// position-visibility: anchors-visible), each recorded in headless
// Chrome and replayed against the gpui-go Anchored view through the
// browser-parity harness:
//
//   - anchor-sides: one anchor with a popup on each side, start/center/
//     end aligned, 8px away. The Chrome twin places the popups with the
//     anchor() insets (bottom is top: calc(anchor(bottom) + 8px), end is
//     left: calc(anchor(right) - width), center is anchor(center) -
//     size/2); the gpui twin is Anchored().AnchorTo(id).Side(...).
//     Align(...).Gap(8).
//   - anchor-flip: a popup that would overflow the window's bottom edge
//     flips above the anchor (position-try-fallbacks: flip-block;
//     Chromium's OOFCandidateStyleIterator,
//     out_of_flow_layout_part.cc:2098-2260, tries the base style, then
//     each fallback, and the first that fits wins), beside a control
//     popup with room to spare that keeps its side.
//   - anchor-shift: popups near the right and bottom window edges clamp
//     into the viewport (the shift fallback; CSS expresses the clamp
//     with min() over the anchor() inset — anchor() is a math node,
//     css_math_expression_node.cc:3926-3941).
//   - anchor-hidden: a popup anchored to an element inside a scroller
//     follows it while it scrolls (anchor_position_scroll_data.cc) and
//     disappears once the anchor no longer intersects the scroller
//     (position-visibility: anchors-visible,
//     anchor_position_visibility_observer.cc:33-71 — the anchor's
//     intersection with its nearest clipping ancestor is empty). The
//     input sequence clicks the anchor (focus) and scrolls with seven
//     ArrowDowns.
//
// The pages are text-free, so Tier C carries no font rasterization
// noise. The fixture geometry is integral (integer boxes at the 2.0
// device scale), so Tier A compares exactly.
package anchor

import (
	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The numbers shared by every page and its gpui view (the tracer's
// discipline: both sides are built from one set of constants).
const (
	windowWidth  = 640.0
	windowHeight = 400.0

	// The sides fixture: a 160x40 anchor at (240,180) with 60x40 popups
	// on its four sides, 8px away, top aligned end, bottom aligned
	// start, left and right aligned center.
	sidesAnchorX = 240.0
	sidesAnchorY = 180.0
	sidesAnchorW = 160.0
	sidesAnchorH = 40.0
	popupW       = 60.0
	popupH       = 40.0
	sidesGap     = 8.0

	// The flip fixture: a 120x40 anchor at (40,360) — its bottom edge is
	// the window's bottom edge — with a 100x60 popup below it that
	// overflows and flips above; a 120x40 control anchor at (200,100)
	// whose popup keeps its side.
	flipAnchorX = 40.0
	flipAnchorY = 360.0
	flipAnchorW = 120.0
	flipAnchorH = 40.0
	flipPopupW  = 100.0
	flipPopupH  = 60.0
	flipCtlX    = 200.0
	flipCtlY    = 100.0

	// The shift fixture: a 60x40 anchor at (560,200) — 20px from the
	// right window edge — with a 100x60 popup that shifts left; and a
	// 60x40 anchor at (100,360) — its bottom edge is the window's —
	// with an 80x50 popup that shifts up.
	shiftXAnchorX = 560.0
	shiftXAnchorY = 200.0
	shiftXPopupW  = 100.0
	shiftXPopupH  = 60.0
	shiftYAnchorX = 100.0
	shiftYAnchorY = 360.0
	shiftYPopupW  = 80.0
	shiftYPopupH  = 50.0

	// The hidden fixture: a 200x300 scroller at (20,40) over 200x800
	// content; the 120x40 anchor sits 200px into the content, so it
	// starts at window y=240 and leaves the scroller's 40..340 clip
	// after 280px of scroll (seven 40px ArrowDown lines). The 100x50
	// popup follows it 8px below, center-aligned.
	hiddenScrollerX = 20.0
	hiddenScrollerY = 40.0
	hiddenScrollerW = 200.0
	hiddenScrollerH = 300.0
	hiddenContentH  = 800.0
	hiddenSpacerH   = 200.0
	hiddenAnchorW   = 120.0
	hiddenAnchorH   = 40.0
	hiddenPopupW    = 100.0
	hiddenPopupH    = 50.0
)

// The palette, shared by the pages and the views.
const (
	pageBackground = 0xffffff
	anchorBlue     = 0x2563eb
	scrollerGray   = 0xe2e8f0
	topOrange      = 0xea580c
	bottomGreen    = 0x16a34a
	leftPurple     = 0x9333ea
	rightTeal      = 0x0d9488
)

// SidesFixture is the four-side placement fixture: side, alignment and
// gap against one anchor (the anchor() inset geometry).
var SidesFixture = parity.Fixture{
	Name:   "anchor-sides",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   sidesPage,
	View:   SidesView,
	BoxIDs: []string{"anchor", "p-top", "p-bottom", "p-left", "p-right"},
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run). The
		// fixture is text-free solid boxes at integral logical
		// coordinates, so the correct view is pixel-exact: 0.0 mean and
		// 0.0 hot everywhere (measured). The wrong-side view moves three
		// popups, failing Tier A first.
		MaxMeanChannelDiff:    0.02,
		MaxROIMeanChannelDiff: 0.1,
		MaxHotPixelFraction:   0.002,
	},
}

// FlipFixture is the flip fixture: a popup that would overflow the
// window's bottom flips above the anchor (flip-block); a control popup
// with room keeps its side.
var FlipFixture = parity.Fixture{
	Name:   "anchor-flip",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   flipPage,
	View:   FlipView,
	BoxIDs: []string{"anchor-flip", "p-flip", "anchor-ctl", "p-ctl"},
	Tolerance: parity.PixelTolerance{
		// Provisional. Text-free solid boxes, pixel-exact when correct;
		// the no-fallback wrong view moves the popup to (40,408), out of
		// the window, failing Tier A first.
		MaxMeanChannelDiff:    0.02,
		MaxROIMeanChannelDiff: 0.1,
		MaxHotPixelFraction:   0.002,
	},
}

// ShiftFixture is the shift fixture: popups near the right and bottom
// window edges clamp into the viewport.
var ShiftFixture = parity.Fixture{
	Name:   "anchor-shift",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   shiftPage,
	View:   ShiftView,
	BoxIDs: []string{"anchor-sx", "p-sx", "anchor-sy", "p-sy"},
	Tolerance: parity.PixelTolerance{
		// Provisional. Text-free solid boxes, pixel-exact when correct;
		// an unshifted wrong view fails Tier A first.
		MaxMeanChannelDiff:    0.02,
		MaxROIMeanChannelDiff: 0.1,
		MaxHotPixelFraction:   0.002,
	},
}

// HiddenFixture is the clipped-anchor fixture: the popup follows the
// anchor inside the scroller and hides when the anchor leaves the
// scroller's clip (position-visibility: anchors-visible).
var HiddenFixture = parity.Fixture{
	Name:   "anchor-hidden",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   hiddenPage,
	Steps: []parity.Step{
		// Click the anchor (focus) and scroll it out of the scroller:
		// seven ArrowDown lines at 40px each move the anchor's top to
		// window y=-40, clear of the scroller's 40..340 clip.
		parity.Click(80, 260),
		parity.Key("arrowdown"),
		parity.Key("arrowdown"),
		parity.Key("arrowdown"),
		parity.Key("arrowdown"),
		parity.Key("arrowdown"),
		parity.Key("arrowdown"),
		parity.Key("arrowdown"),
	},
	ViewWithWindow: HiddenView,
	// Only the scroller's box compares: the scrolled boxes (the content
	// and the anchor) and the popup's box are Chrome-side stale by
	// design — a scroller containing an anchor defers its main-thread
	// scroll commit (anchor_position_scroll_data.cc tracks the offset so
	// the popup follows without relayout), so getBoundingClientRect
	// lags the compositor's scroll while the port follows it exactly
	// every frame. The popup's hiding — the behaviour under test — is
	// proven by Tier C: the record's pixels show nothing where a
	// still-visible popup would paint (the discrimination test).
	BoxIDs:     []string{"scroller"},
	EventKinds: []string{"click", "keydown", "keyup"},
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run). The final
		// frame's popup paints nothing (the anchor is clipped), and the
		// visible scroller region is text-free solid boxes: the correct
		// view is pixel-exact (measured 0.0 mean, 0.0 hot everywhere). A
		// still-visible wrong view leaves a 100x50 green box at (30,8) —
		// measured 1.471 mean image-wide, 1.830 in the scroller's ROI,
		// 0.0195 hot (anchor_test.go's discrimination test).
		MaxMeanChannelDiff:    0.02,
		MaxROIMeanChannelDiff: 0.1,
		MaxHotPixelFraction:   0.002,
	},
}

func init() {
	parity.Register(SidesFixture)
	parity.Register(FlipFixture)
	parity.Register(ShiftFixture)
	parity.Register(HiddenFixture)
}

// ---------------------------------------------------------------------------
// The sides fixture
// ---------------------------------------------------------------------------

// sidesPage is the HTML twin of SidesView: one anchor named --a with
// four position:fixed popups placed by the anchor() insets.
const sidesPage = `<!doctype html>
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
		position: relative;
	}
	#anchor {
		position: absolute;
		left: 240px;
		top: 180px;
		width: 160px;
		height: 40px;
		background: #2563eb;
		anchor-name: --a;
	}
	.p {
		position: fixed;
		position-anchor: --a;
		width: 60px;
		height: 40px;
	}
	/* top, aligned end: the popup's bottom sits 8px above the anchor's
	   top (an end-side inset measures from the viewport's bottom edge,
	   so the gap adds), its right edge at the anchor's right. */
	#p-top {
		bottom: calc(anchor(top) + 8px);
		left: calc(anchor(right) - 60px);
		background: #ea580c;
	}
	/* bottom, aligned start: the popup's top sits 8px below the anchor's
	   bottom, its left edge at the anchor's left. */
	#p-bottom {
		top: calc(anchor(bottom) + 8px);
		left: anchor(left);
		background: #16a34a;
	}
	/* left, aligned center: the popup's right edge sits 8px left of the
	   anchor's left edge, vertically centered on the anchor. */
	#p-left {
		right: calc(anchor(left) + 8px);
		top: calc(anchor(center) - 20px);
		background: #9333ea;
	}
	/* right, aligned center: the popup's left edge sits 8px right of the
	   anchor's right edge, vertically centered on the anchor. */
	#p-right {
		left: calc(anchor(right) + 8px);
		top: calc(anchor(center) - 20px);
		background: #0d9488;
	}
</style>
</head>
<body>
	<div id="anchor" data-id="anchor"></div>
	<div class="p" id="p-top" data-id="p-top"></div>
	<div class="p" id="p-bottom" data-id="p-bottom"></div>
	<div class="p" id="p-left" data-id="p-left"></div>
	<div class="p" id="p-right" data-id="p-right"></div>
</body>
</html>
`

// SidesView is the gpui-go twin of sidesPage: the anchor element plus
// four deferred, element-anchored popups, one per side.
func SidesView(_ *parity.EventLog) gpui.AnyElement {
	return anchorRoot(
		anchorBox("anchor", sidesAnchorX, sidesAnchorY, sidesAnchorW, sidesAnchorH, anchorBlue),
		popup("p-top", "anchor", gpui.AnchorSideTop, gpui.AnchorAlignEnd, sidesGap, topOrange),
		popup("p-bottom", "anchor", gpui.AnchorSideBottom, gpui.AnchorAlignStart, sidesGap, bottomGreen),
		popup("p-left", "anchor", gpui.AnchorSideLeft, gpui.AnchorAlignCenter, sidesGap, leftPurple),
		popup("p-right", "anchor", gpui.AnchorSideRight, gpui.AnchorAlignCenter, sidesGap, rightTeal),
	)
}

// SidesWrongSide is the deliberately-wrong view for the discrimination
// test: every popup is placed on the bottom side regardless of its
// recorded side, so the top/left/right popups sit where the bottom
// geometry puts them instead of around the anchor. The anchor and the
// bottom popup are unchanged.
func SidesWrongSide(_ *parity.EventLog) gpui.AnyElement {
	return anchorRoot(
		anchorBox("anchor", sidesAnchorX, sidesAnchorY, sidesAnchorW, sidesAnchorH, anchorBlue),
		popup("p-top", "anchor", gpui.AnchorSideBottom, gpui.AnchorAlignEnd, sidesGap, topOrange),
		popup("p-bottom", "anchor", gpui.AnchorSideBottom, gpui.AnchorAlignStart, sidesGap, bottomGreen),
		popup("p-left", "anchor", gpui.AnchorSideBottom, gpui.AnchorAlignCenter, sidesGap, leftPurple),
		popup("p-right", "anchor", gpui.AnchorSideBottom, gpui.AnchorAlignCenter, sidesGap, rightTeal),
	)
}

// ---------------------------------------------------------------------------
// The flip fixture
// ---------------------------------------------------------------------------

// flipPage is the HTML twin of FlipView: two anchors, one whose popup
// overflows the window and flips (flip-block), one with room that
// keeps its side.
const flipPage = `<!doctype html>
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
		position: relative;
	}
	.anchor {
		position: absolute;
		width: 120px;
		height: 40px;
		background: #2563eb;
	}
	#anchor-flip {
		left: 40px;
		top: 360px;
		anchor-name: --flip;
	}
	#anchor-ctl {
		left: 200px;
		top: 100px;
		anchor-name: --ctl;
	}
	.p {
		position: fixed;
		width: 100px;
		height: 60px;
	}
	/* 8px below the anchor, start-aligned; the anchor's bottom edge is
	   the window's, so the base placement overflows and flip-block is
	   tried: the popup lands 8px above the anchor's top. */
	#p-flip {
		position-anchor: --flip;
		top: calc(anchor(bottom) + 8px);
		left: anchor(left);
		position-try-fallbacks: flip-block;
		background: #16a34a;
	}
	/* The control: room to spare below, the base placement stands. */
	#p-ctl {
		position-anchor: --ctl;
		top: calc(anchor(bottom) + 8px);
		left: anchor(left);
		background: #0d9488;
	}
</style>
</head>
<body>
	<div class="anchor" id="anchor-flip" data-id="anchor-flip"></div>
	<div class="p" id="p-flip" data-id="p-flip"></div>
	<div class="anchor" id="anchor-ctl" data-id="anchor-ctl"></div>
	<div class="p" id="p-ctl" data-id="p-ctl"></div>
</body>
</html>
`

// FlipView is the gpui-go twin of flipPage.
func FlipView(_ *parity.EventLog) gpui.AnyElement {
	return anchorRoot(
		anchorBox("anchor-flip", flipAnchorX, flipAnchorY, flipAnchorW, flipAnchorH, anchorBlue),
		anchorBox("anchor-ctl", flipCtlX, flipCtlY, flipAnchorW, flipAnchorH, anchorBlue),
		flipPopup("p-flip", "anchor-flip", true, bottomGreen),
		flipPopup("p-ctl", "anchor-ctl", false, rightTeal),
	)
}

// FlipNoFallback is the deliberately-wrong view for the discrimination
// test: the flipping popup carries no fallbacks, so it keeps its base
// placement below the window's bottom edge.
func FlipNoFallback(_ *parity.EventLog) gpui.AnyElement {
	return anchorRoot(
		anchorBox("anchor-flip", flipAnchorX, flipAnchorY, flipAnchorW, flipAnchorH, anchorBlue),
		anchorBox("anchor-ctl", flipCtlX, flipCtlY, flipAnchorW, flipAnchorH, anchorBlue),
		flipPopup("p-flip", "anchor-flip", false, bottomGreen),
		flipPopup("p-ctl", "anchor-ctl", false, rightTeal),
	)
}

// flipPopup builds one 100x60 popup 8px below its anchor, start
// aligned, flipping when flip is set.
func flipPopup(id, anchorID string, flip bool, background uint32) gpui.AnyElement {
	anchored := gpui.Anchored().
		AnchorTo(anchorID).
		Side(gpui.AnchorSideBottom).
		Align(gpui.AnchorAlignStart).
		Gap(sidesGap)
	if flip {
		anchored = anchored.TryFallbacks(gpui.AnchorFlipBlock)
	}
	return gpui.Deferred(anchored.Child(
		gpui.Div().ID(id).DebugSelector(id).
			W(gpui.Px(flipPopupW)).H(gpui.Px(flipPopupH)).
			Bg(gpui.Rgb(background)),
	)).IntoElement()
}

// ---------------------------------------------------------------------------
// The shift fixture
// ---------------------------------------------------------------------------

// shiftPage is the HTML twin of ShiftView: two anchors near the right
// and bottom window edges whose popups clamp into the viewport with
// min() over the anchor() inset.
const shiftPage = `<!doctype html>
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
		position: relative;
	}
	.anchor {
		position: absolute;
		width: 60px;
		height: 40px;
		background: #2563eb;
	}
	#anchor-sx {
		left: 560px;
		top: 200px;
		anchor-name: --sx;
	}
	#anchor-sy {
		left: 100px;
		top: 360px;
		anchor-name: --sy;
	}
	/* 8px below the anchor, start-aligned; the anchor is 20px from the
	   right window edge, so a 100px popup would overflow by 20px and
	   the clamp holds its right edge at the window's right. */
	#p-sx {
		position: fixed;
		position-anchor: --sx;
		top: calc(anchor(bottom) + 8px);
		left: min(anchor(left), calc(100vw - 100px));
		width: 100px;
		height: 60px;
		background: #16a34a;
	}
	/* 8px left of the anchor, start-aligned; the anchor's bottom edge
	   is the window's, so a 50px popup would overflow by 10px and the
	   clamp holds its bottom edge at the window's bottom. */
	#p-sy {
		position: fixed;
		position-anchor: --sy;
		right: calc(anchor(left) + 8px);
		top: min(anchor(top), calc(100vh - 50px));
		width: 80px;
		height: 50px;
		background: #9333ea;
	}
</style>
</head>
<body>
	<div class="anchor" id="anchor-sx" data-id="anchor-sx"></div>
	<div id="p-sx" data-id="p-sx"></div>
	<div class="anchor" id="anchor-sy" data-id="anchor-sy"></div>
	<div id="p-sy" data-id="p-sy"></div>
</body>
</html>
`

// ShiftView is the gpui-go twin of shiftPage.
func ShiftView(_ *parity.EventLog) gpui.AnyElement {
	return anchorRoot(
		anchorBox("anchor-sx", shiftXAnchorX, shiftXAnchorY, 60, 40, anchorBlue),
		anchorBox("anchor-sy", shiftYAnchorX, shiftYAnchorY, 60, 40, anchorBlue),
		gpui.Deferred(gpui.Anchored().
			AnchorTo("anchor-sx").
			Side(gpui.AnchorSideBottom).
			Align(gpui.AnchorAlignStart).
			Gap(sidesGap).
			TryFallbacks(gpui.AnchorShift).
			Child(gpui.Div().ID("p-sx").DebugSelector("p-sx").
				W(gpui.Px(shiftXPopupW)).H(gpui.Px(shiftXPopupH)).
				Bg(gpui.Rgb(bottomGreen)))),
		gpui.Deferred(gpui.Anchored().
			AnchorTo("anchor-sy").
			Side(gpui.AnchorSideLeft).
			Align(gpui.AnchorAlignStart).
			Gap(sidesGap).
			TryFallbacks(gpui.AnchorShift).
			Child(gpui.Div().ID("p-sy").DebugSelector("p-sy").
				W(gpui.Px(shiftYPopupW)).H(gpui.Px(shiftYPopupH)).
				Bg(gpui.Rgb(leftPurple)))),
	)
}

// ---------------------------------------------------------------------------
// The hidden fixture
// ---------------------------------------------------------------------------

// hiddenPage is the HTML twin of HiddenView: a scroller over tall
// content with the anchor 200px in, a fixed popup 8px below it
// (center-aligned) that follows the scroll and hides while the anchor
// is clipped.
const hiddenPage = `<!doctype html>
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
		position: relative;
	}
	#scroller {
		position: absolute;
		left: 20px;
		top: 40px;
		width: 200px;
		height: 300px;
		overflow-y: scroll;
		/* The port draws no scrollbars and reserves no gutter
		   (browser-core ticket 36 owns scrollbar gutter). */
		scrollbar-width: none;
		background: #e2e8f0;
	}
	#content {
		width: 200px;
		height: 800px;
		display: flex;
		flex-direction: column;
	}
	#spacer {
		height: 200px;
	}
	#anchor {
		width: 120px;
		height: 40px;
		background: #2563eb;
		anchor-name: --a;
		outline: none;
	}
	#popup {
		position: fixed;
		position-anchor: --a;
		/* 8px below the anchor, center-aligned; the fixed element
		   follows the scroller (anchor_position_scroll_data) and hides
		   while the anchor is clipped. */
		top: calc(anchor(bottom) + 8px);
		left: calc(anchor(center) - 50px);
		width: 100px;
		height: 50px;
		background: #16a34a;
		position-visibility: anchors-visible;
	}
</style>
</head>
<body>
	<div id="scroller" data-id="scroller">
		<div id="content" data-id="content">
			<div id="spacer"></div>
			<!-- tabindex on the element (not CSS): the click focuses it, so
			     ArrowDown scrolls its scroller on both sides. -->
			<div id="anchor" data-id="anchor" tabindex="0"></div>
		</div>
	</div>
	<div id="popup" data-id="popup"></div>
</body>
</html>
`

// HiddenView is the gpui-go twin of hiddenPage: the scroller with the
// anchor, and a deferred element-anchored popup that hides when the
// anchor is clipped.
func HiddenView(log *parity.EventLog, w *gpui.Window, _ *gpui.App) gpui.AnyElement {
	return hiddenRoot(log, w, true)
}

// HiddenAlwaysVisible is the deliberately-wrong view for the
// discrimination test: the popup never hides, so the final frame (the
// anchor scrolled out of the scroller's clip) still paints it at
// (30,8) where the record has nothing. The boxes are unchanged, so
// only Tier C fails.
func HiddenAlwaysVisible(log *parity.EventLog, w *gpui.Window, _ *gpui.App) gpui.AnyElement {
	return hiddenRoot(log, w, false)
}

// hiddenRoot builds the hidden fixture's tree; the popup hides on a
// clipped anchor only when hide is set.
func hiddenRoot(log *parity.EventLog, w *gpui.Window, hide bool) gpui.AnyElement {
	// The focus handle lives outside the returned closure, keyed on the
	// window (the harness's discipline: the view function re-runs every
	// frame).
	focus := hiddenFocus(w, log)
	anchored := gpui.Anchored().
		AnchorTo("anchor").
		Side(gpui.AnchorSideBottom).
		Align(gpui.AnchorAlignCenter).
		Gap(sidesGap)
	if hide {
		anchored = anchored.HideWhenAnchorClipped()
	}
	return anchorRoot(
		gpui.Div().ID("scroller").DebugSelector("scroller").
			Absolute().Top(gpui.Px(hiddenScrollerY)).Left(gpui.Px(hiddenScrollerX)).
			W(gpui.Px(hiddenScrollerW)).H(gpui.Px(hiddenScrollerH)).
			OverflowYScroll().
			OnClick(log.Click("scroller")).
			OnKeyDown(log.KeyDown("scroller")).
			OnKeyUp(log.KeyUp("scroller")).
			Bg(gpui.Rgb(scrollerGray)).
			Child(
				gpui.Div().ID("content").DebugSelector("content").
					W(gpui.Px(hiddenScrollerW)).H(gpui.Px(hiddenContentH)).
					FlexCol().
					OnClick(log.Click("content")).
					OnKeyDown(log.KeyDown("content")).
					OnKeyUp(log.KeyUp("content")).
					Child(gpui.Div().H(gpui.Px(hiddenSpacerH))).
					Child(
						gpui.Div().ID("anchor").DebugSelector("anchor").
							W(gpui.Px(hiddenAnchorW)).H(gpui.Px(hiddenAnchorH)).
							TrackFocus(focus).
							OnClick(log.Click("anchor")).
							OnKeyDown(log.KeyDown("anchor")).
							OnKeyUp(log.KeyUp("anchor")).
							Bg(gpui.Rgb(anchorBlue)),
					),
			),
		gpui.Deferred(anchored.Child(
			gpui.Div().ID("popup").DebugSelector("popup").
				W(gpui.Px(hiddenPopupW)).H(gpui.Px(hiddenPopupH)).
				Bg(gpui.Rgb(bottomGreen)))),
	)
}

// hiddenFocus keeps one focus handle per window across the re-rendered
// views (the harness's mutable-state discipline).
var hiddenFocuses = map[*gpui.Window]gpui.FocusHandle{}

func hiddenFocus(w *gpui.Window, log *parity.EventLog) gpui.FocusHandle {
	if focus, ok := hiddenFocuses[w]; ok {
		return focus
	}
	focus := gpui.NewFocusHandle(w).TabStop(true)
	hiddenFocuses[w] = focus
	log.TrackFocus(focus, "anchor")
	return focus
}

// ---------------------------------------------------------------------------
// Shared view builders
// ---------------------------------------------------------------------------

// anchorRoot builds the fixture root: a full-size flex body carrying
// the absolutely positioned anchors and the deferred popups.
func anchorRoot(children ...any) gpui.AnyElement {
	root := gpui.Div().
		ID("root").
		SizeFull().
		Flex().
		Bg(gpui.Rgb(pageBackground))
	for _, child := range children {
		root = root.Child(child)
	}
	return root.IntoElement()
}

// anchorBox builds one absolutely positioned anchor element at the
// given window coordinates; its id names it as the popups' anchor.
func anchorBox(id string, x, y, w, h float32, background uint32) *gpui.DivElement {
	return gpui.Div().
		ID(id).
		DebugSelector(id).
		Absolute().
		Top(gpui.Px(y)).
		Left(gpui.Px(x)).
		W(gpui.Px(w)).
		H(gpui.Px(h)).
		Bg(gpui.Rgb(background))
}

// popup builds one deferred, element-anchored 60x40 popup.
func popup(id, anchorID string, side gpui.AnchorSide, align gpui.AnchorAlign, gap float32, background uint32) gpui.AnyElement {
	return gpui.Deferred(gpui.Anchored().
		AnchorTo(anchorID).
		Side(side).
		Align(align).
		Gap(gap).
		Child(gpui.Div().ID(id).DebugSelector(id).
			W(gpui.Px(popupW)).H(gpui.Px(popupH)).
			Bg(gpui.Rgb(background)))).IntoElement()
}
