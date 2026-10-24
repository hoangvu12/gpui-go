//go:build windows

// Package scrollrange is browser-core ticket 07's fixture set for the
// scroll range and the device-pixel rounding rules: a scroller whose
// range comes from the layout engine's content size — the descendants'
// overflow, the in-flow children's trailing margins and the end padding
// — with vertical padding percentages resolved against the containing
// block's width, plus the border-width fixtures (Chromium's
// computed-style floor: a 1px border at a fractional device scale, a
// 0.5px hairline at 100% and a fractional CSS width at the default
// scale).
//
// The pages are text-free geometry (the boxpaint discipline), so Tier C
// carries no font rasterization noise; the scroll step is the
// keyboard-scroll default action (the End key over a focusable row
// clicked first, like the sticky fixtures).
//
// Chromium's rules these fixtures pin:
//
//   - the scrollable overflow includes the end padding and the in-flow
//     children's end margins (ScrollableOverflowCalculator::Result,
//     core/layout/scrollable_overflow_calculator.cc:100-118, and the
//     inflow bounds' margin unification,
//     core/layout/box_fragment_builder.cc:321-357);
//   - a descendant's overflow extends the range (the calculator folds
//     each child's own scrollable overflow in, :262-311);
//   - percentage padding resolves against the containing block's
//     inline size on every edge
//     (LayoutBoxModelObject::ComputedPaddingOutsets,
//     core/layout/layout_box_model_object.cc:838-859);
//   - the computed border width is an integer of CSS px, with
//     sub-pixel hairlines kept at one (StyleBuilderConverter::
//     ConvertBorderWidth -> ClampLineWidth, core/css/resolver/
//     style_builder_converter.cc:1971-1987).
package scrollrange

import (
	"fmt"
	"sync"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The page's numbers, shared by the HTML and the gpui views (the
// tracer's discipline: one set of constants builds both sides).
const (
	windowWidth  = 640.0
	windowHeight = 400.0

	scrollerW = 240.0
	scrollerH = 300.0
	scrollerX = 20.0
	scrollerY = 20.0

	// The scroller's padding: 10% top (of the containing block's 640px
	// content width = 64px — the percentage basis the ticket pins), no
	// inline padding, and a 24px END padding (part of the scrollable
	// overflow).
	padTopPercent = 0.10
	padBottom     = 24.0

	focusH  = 40.0
	nestedH = 60.0
	tallW   = 120.0
	tallH   = 200.0
	spacerH = 100.0
	lastH   = 50.0
	lastMb  = 40.0

	// The border fixtures' numbers. The shared geometry picks CSS
	// lengths whose device values are integral at every fixture's scale
	// (1.0, 1.75 and 2.0: multiples of 4), so the port's device-edge
	// bounds snapping lands on the same pixels Chrome lays out and Tier A
	// compares exactly at any of them.
	borderBoxW = 200.0
	borderBoxH = 152.0
	borderBoxX = 12.0
	borderBoxY = 12.0
)

// The palette, shared by the pages and the views.
const (
	pageBackground = 0xffffff
	scrollerBg     = 0xf1f5f9
	focusBg        = 0xfca5a5
	nestedBg       = 0x93c5fd
	tallBg         = 0x2563eb
	spacerBg       = 0xe2e8f0
	lastBg         = 0x16a34a
	innerBg        = 0xcccccc
	borderColor    = 0x000000
)

// rangeBoxIDs are the range fixture's compared boxes.
var rangeBoxIDs = []string{
	"root", "scroller", "focus", "nested", "tall", "spacer", "last",
}

// Fixture is the scroll-range fixture: nested overflowing content, a
// trailing bottom margin, a percentage top padding and an end padding,
// scrolled to the end with the End key. The visible boxes after the
// scroll pin the whole range: the descendant overflow (the tall
// grandchild), the last row's bottom margin and the scroller's end
// padding must all be reachable, and the content's start offset pins
// the padding's percentage basis.
var Fixture = parity.Fixture{
	Name:   "scrollrange",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   rangePage,
	Steps: []parity.Step{
		parity.Click(scrollerX+scrollerW/2, scrollerY+focusH/2+padTopPercent*windowWidth),
		parity.Key("end"),
	},
	BoxIDs:         rangeBoxIDs,
	ViewWithWindow: RangeView,
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run), measured
		// from the correct view: flat-colour geometry, pixel-exact at
		// the 2.0 device scale. Verified by scrollrange_test.go.
		MaxMeanChannelDiff:    0.02,
		MaxROIMeanChannelDiff: 0.1,
		MaxHotPixelFraction:   0.002,
	},
}

// BorderFloorFixture is the fractional-width border fixture at the
// harness's default scale: a 1.3px border is 2.6 device px at 2.0, and
// Chromium floors the CSS width to 1 — so the content insets by exactly
// 1 CSS px (2 device px) where the pin's round-to-nearest rule would
// inset 1.5 CSS px. Tier A compares the inset child box exactly; Tier C
// carries the painted 2-device-px strip.
var BorderFloorFixture = parity.Fixture{
	Name:   "border-floor",
	Window: parity.Size{Width: 320, Height: 240},
	HTML:   borderPage("1.3px", 1.3),
	View:   borderView(1.3),
	BoxIDs: []string{"boxed", "inner"},
	Tolerance: parity.PixelTolerance{
		// Flat-colour geometry; the correct view is pixel-exact.
		MaxMeanChannelDiff:    0.02,
		MaxROIMeanChannelDiff: 0.1,
		MaxHotPixelFraction:   0.002,
	},
}

// Border175Fixture is the 1px border at a 175% device scale (the
// fixture's own Scale): Chromium's computed width is still exactly 1
// CSS px, and the painted strip is an antialiased 1.75 device px — the
// measured record (Chrome 155) confirms both. The port's layout inset
// carries the unrounded 1.75 device px (the child's snapped bounds land
// on the same device edge), and the painted strip is the rounded 2
// device px a hard-edged renderer can draw.
var Border175Fixture = parity.Fixture{
	Name:   "border-175",
	Window: parity.Size{Width: 320, Height: 240},
	Scale:  1.75,
	HTML:   borderPage("1px", 1.0),
	View:   borderView(1.0),
	BoxIDs: []string{"boxed"},
	Tolerance: parity.PixelTolerance{
		// Measured from the correct view: the hard 2-device-px strip
		// against Chrome's antialiased 1.75 leaves a thin partial band on
		// every border edge — whole-image mean 0.0709, boxed-ROI mean
		// 0.1791, no hot pixels (verified by scrollrange_test.go; the
		//	gates sit at ~2.8x the measured values).
		MaxMeanChannelDiff:    0.2,
		MaxROIMeanChannelDiff: 0.5,
		MaxHotPixelFraction:   0.002,
	},
}

// BorderHairlineFixture is the 0.5px hairline at a 100% device scale
// (the fixture's own Scale): the sub-pixel width keeps one CSS pixel —
// the inset and the painted strip are both exactly 1 device px, and the
// child box insets 1 CSS px. This is the "sub-device-pixel lengths keep
// one device pixel" rule at the scale where it is observable.
var BorderHairlineFixture = parity.Fixture{
	Name:   "border-hairline",
	Window: parity.Size{Width: 320, Height: 240},
	Scale:  1.0,
	HTML:   borderPage("0.5px", 0.5),
	View:   borderView(0.5),
	BoxIDs: []string{"boxed", "inner"},
	Tolerance: parity.PixelTolerance{
		// Flat-colour geometry; the correct view is pixel-exact.
		MaxMeanChannelDiff:    0.02,
		MaxROIMeanChannelDiff: 0.1,
		MaxHotPixelFraction:   0.002,
	},
}

func init() {
	parity.Register(Fixture)
	parity.Register(BorderFloorFixture)
	parity.Register(Border175Fixture)
	parity.Register(BorderHairlineFixture)
}

// The per-window focus handle of the range fixture's focus row (the
// sticky fixtures' scrollFocus pattern: the handle lives outside the
// view function so its identity survives the per-frame rebuilds, and a
// fresh window gets a fresh handle).
var (
	focusMu    sync.Mutex
	focusByWin = map[*gpui.Window]gpui.FocusHandle{}
)

func focusOf(log *parity.EventLog, w *gpui.Window) gpui.FocusHandle {
	focusMu.Lock()
	defer focusMu.Unlock()
	if focus, ok := focusByWin[w]; ok {
		return focus
	}
	focus := gpui.NewFocusHandle(w).TabStop(true)
	focusByWin[w] = focus
	log.TrackFocus(focus, "focus")
	return focus
}

// f2s renders one shared float constant for the pages' CSS.
func f2s(v float64) string {
	return fmt.Sprintf("%.0f", v)
}

// rangePage is the range fixture's page.
var rangePage = `<!doctype html>
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
		width: ` + f2s(scrollerW) + `px;
		height: ` + f2s(scrollerH) + `px;
		margin-left: ` + f2s(scrollerX) + `px;
		margin-top: ` + f2s(scrollerY) + `px;
		display: flex;
		flex-direction: column;
		overflow-y: scroll;
		/* The port draws no scrollbars and reserves no gutter. */
		scrollbar-width: none;
		padding-top: 10%;
		padding-bottom: ` + f2s(padBottom) + `px;
		background: #f1f5f9;
	}
	#scroller > div {
		flex-shrink: 0;
		box-sizing: border-box;
		width: ` + f2s(scrollerW) + `px;
	}
	#focus {
		height: ` + f2s(focusH) + `px;
		background: #fca5a5;
		outline: none;
	}
	#nested {
		height: ` + f2s(nestedH) + `px;
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		background: #93c5fd;
	}
	#tall {
		/* Overflows the nested box: the descendant's overflow must
		   extend the scroller's range. */
		width: ` + f2s(tallW) + `px;
		height: ` + f2s(tallH) + `px;
		flex-shrink: 0;
		background: #2563eb;
	}
	#spacer {
		height: ` + f2s(spacerH) + `px;
		background: #e2e8f0;
	}
	#last {
		height: ` + f2s(lastH) + `px;
		margin-bottom: ` + f2s(lastMb) + `px;
		background: #16a34a;
	}
</style>
</head>
<body data-id="root">
	<div id="scroller" data-id="scroller">
		<div id="focus" data-id="focus" tabindex="0"></div>
		<div id="nested" data-id="nested">
			<div id="tall" data-id="tall"></div>
		</div>
		<div id="spacer" data-id="spacer"></div>
		<div id="last" data-id="last"></div>
	</div>
</body>
</html>`

// borderPage is one border fixture's page: a bordered box with an
// inner child, at the given CSS border width.
func borderPage(cssWidth string, width float64) string {
	return `<!doctype html>
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
		width: 320px;
		height: 240px;
		display: flex;
	}
	#boxed {
		box-sizing: border-box;
		width: ` + f2s(borderBoxW) + `px;
		height: ` + f2s(borderBoxH) + `px;
		margin-left: ` + f2s(borderBoxX) + `px;
		margin-top: ` + f2s(borderBoxY) + `px;
		border: ` + cssWidth + ` solid #000000;
		background: #ffffff;
	}
	#inner {
		box-sizing: border-box;
		width: 100%;
		height: 100%;
		background: #cccccc;
	}
</style>
</head>
<body>
	<div id="boxed" data-id="boxed"><div id="inner" data-id="inner"></div></div>
</body>
</html>`
}

// RangeView is the range fixture's gpui twin (the window-aware form:
// the focus row needs a stable focus handle for the keyboard scroll).
func RangeView(log *parity.EventLog, w *gpui.Window, _ *gpui.App) gpui.AnyElement {
	focus := focusOf(log, w)
	root := gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Flex().
		AlignItems(gpui.AlignItemsFlexStart).
		Bg(gpui.Rgb(pageBackground))
	scroller := gpui.Div().
		ID("scroller").
		DebugSelector("scroller").
		Size(gpui.Px(scrollerW), gpui.Px(scrollerH)).
		Ml(gpui.Px(scrollerX)).
		Mt(gpui.Px(scrollerY)).
		FlexCol().
		OverflowYScroll().
		// The percentage padding rides the record (browser-core 07):
		// the fraction resolves against the containing block's width.
		Pt(gpui.Fraction(padTopPercent)).
		Pb(gpui.Px(padBottom)).
		Bg(gpui.Rgb(scrollerBg)).
		Child(gpui.Div().
			ID("focus").
			DebugSelector("focus").
			Size(gpui.Px(scrollerW), gpui.Px(focusH)).
			FlexShrink0().
			TrackFocus(focus).
			Bg(gpui.Rgb(focusBg))).
		Child(gpui.Div().
			ID("nested").
			DebugSelector("nested").
			Size(gpui.Px(scrollerW), gpui.Px(nestedH)).
			FlexCol().
			AlignItems(gpui.AlignItemsFlexStart).
			FlexShrink0().
			Bg(gpui.Rgb(nestedBg)).
			Child(gpui.Div().
				ID("tall").
				DebugSelector("tall").
				Size(gpui.Px(tallW), gpui.Px(tallH)).
				FlexShrink0().
				Bg(gpui.Rgb(tallBg)))).
		Child(gpui.Div().
			ID("spacer").
			DebugSelector("spacer").
			Size(gpui.Px(scrollerW), gpui.Px(spacerH)).
			FlexShrink0().
			Bg(gpui.Rgb(spacerBg))).
		Child(gpui.Div().
			ID("last").
			DebugSelector("last").
			Size(gpui.Px(scrollerW), gpui.Px(lastH)).
			FlexShrink0().
			Mb(gpui.Px(lastMb)).
			Bg(gpui.Rgb(lastBg)))
	return root.Child(scroller).IntoElement()
}

// borderView is one border fixture's gpui twin.
func borderView(width float32) func(log *parity.EventLog) gpui.AnyElement {
	return func(log *parity.EventLog) gpui.AnyElement {
		return gpui.Div().
			ID("root").
			SizeFull().
			Flex().
			Bg(gpui.Rgb(pageBackground)).
			Child(gpui.Div().
				ID("boxed").
				DebugSelector("boxed").
				Size(gpui.Px(borderBoxW), gpui.Px(borderBoxH)).
				Ml(gpui.Px(borderBoxX)).
				Mt(gpui.Px(borderBoxY)).
				Border(gpui.Px(width)).
				BorderColor(gpui.Rgb(borderColor)).
				Bg(gpui.Rgb(pageBackground)).
				Child(gpui.Div().
					ID("inner").
					DebugSelector("inner").
					SizeFull().
					Bg(gpui.Rgb(innerBg)))).
			IntoElement()
	}
}

// WrongPaddingBasisView is the deliberately-wrong view of the range
// fixture for the discrimination test: the scroller's top padding
// resolved as a fraction of the scroller's OWN height (the pre-ticket
// pin's basis — 10% of 300 is 30px) instead of the containing block's
// width (64px), AND the end padding dropped (the pin's element-tree
// range stopped at the children's border boxes and never included
// it). The end padding half is what the boxes see: browser-core ticket
// 15's press-node keyboard scroll scrolls the scroller to its end even
// when the fixture's click misses the focus row (the wrong basis moves
// the row), and at the end scroll the top-padding basis cancels — the
// end padding does not: every content box lands 24px off, so the Tier
// A comparison fails with a readable diff while the root and scroller
// boxes still match.
func WrongPaddingBasisView(log *parity.EventLog, w *gpui.Window, _ *gpui.App) gpui.AnyElement {
	// Swap the scroller's top padding for the resolved-against-height
	// value the pin would have computed, and drop the end padding the
	// pin's range never included.
	root := gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Flex().
		AlignItems(gpui.AlignItemsFlexStart).
		Bg(gpui.Rgb(pageBackground))
	scroller := gpui.Div().
		ID("scroller").
		DebugSelector("scroller").
		Size(gpui.Px(scrollerW), gpui.Px(scrollerH)).
		Ml(gpui.Px(scrollerX)).
		Mt(gpui.Px(scrollerY)).
		FlexCol().
		OverflowYScroll().
		Pt(gpui.Px(padTopPercent * scrollerH)).
		Pb(gpui.Px(0)).
		Bg(gpui.Rgb(scrollerBg)).
		Child(gpui.Div().
			ID("focus").
			DebugSelector("focus").
			Size(gpui.Px(scrollerW), gpui.Px(focusH)).
			FlexShrink0().
			TrackFocus(focusOf(log, w)).
			Bg(gpui.Rgb(focusBg))).
		Child(gpui.Div().
			ID("nested").
			DebugSelector("nested").
			Size(gpui.Px(scrollerW), gpui.Px(nestedH)).
			FlexCol().
			AlignItems(gpui.AlignItemsFlexStart).
			FlexShrink0().
			Bg(gpui.Rgb(nestedBg)).
			Child(gpui.Div().
				ID("tall").
				DebugSelector("tall").
				Size(gpui.Px(tallW), gpui.Px(tallH)).
				FlexShrink0().
				Bg(gpui.Rgb(tallBg)))).
		Child(gpui.Div().
			ID("spacer").
			DebugSelector("spacer").
			Size(gpui.Px(scrollerW), gpui.Px(spacerH)).
			FlexShrink0().
			Bg(gpui.Rgb(spacerBg))).
		Child(gpui.Div().
			ID("last").
			DebugSelector("last").
			Size(gpui.Px(scrollerW), gpui.Px(lastH)).
			FlexShrink0().
			Mb(gpui.Px(lastMb)).
			Bg(gpui.Rgb(lastBg)))
	return root.Child(scroller).IntoElement()
}
