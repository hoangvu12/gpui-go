//go:build windows

// Package position is ticket 33's fixture for position: static (spec
// D3): absolutely positioned children of a static parent resolve
// against the nearest positioned ancestor, a static chain defers up to
// the initial containing block (the window root), and the untouched
// default — an absolute child of a positioned (relative) parent —
// keeps the pin's direct-parent behaviour. Insets on all sides and
// percentage insets resolve against the containing block's padding
// box, so the positioned ancestors carry border and padding (the basis
// is observable). The page is text-free geometry (the boxpaint
// discipline), so Tier C carries no font rasterization noise.
//
// Chromium decides the containing block the way this fixture records:
// an object can contain absolute children when its position is not
// static (LayoutObject::ComputeIsAbsoluteContainer,
// core/layout/layout_object.cc:2069-2081 in the research checkout; the
// bit set from style at layout_box_model_object.cc:503-504 and read by
// LayoutObject::CanContainAbsolutePositionObjects,
// layout_object.h:2356-2360; the containing block is the first
// ancestor with the bit, LayoutObject::ContainerForAbsolutePosition,
// layout_object.cc:1877-1883). The inset basis is the containing
// block's padding box ("|inset| is relative to the container's
// padding-box", out_of_flow_layout_part.cc:2574-2576).
package position

import (
	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The page's numbers, shared by the HTML and the gpui view (the
// tracer's discipline: one set of constants builds both sides).
const (
	windowWidth  = 640.0
	windowHeight = 400.0

	// The escape case: a positioned ancestor (border 6, padding 14)
	// holding a static parent (120x90) holding three absolute children.
	// The ancestor's border box is 200x160 at the flow origin, so its
	// padding box origin is (6, 6) and its size is 188x148.
	ancestorWidth   = 200.0
	ancestorHeight  = 160.0
	ancestorBorder  = 6.0
	ancestorPadding = 14.0
	staticW         = 120.0
	staticH         = 90.0
	absTLLeft       = 12.0
	absTLTop        = 8.0
	absTLW          = 40.0
	absTLH          = 30.0
	absBRRight      = 16.0
	absBRBottom     = 10.0
	absBRW          = 50.0
	absBRH          = 36.0
	absPctLeft      = 0.25 // 25% of the ancestor's padding box width
	absPctTop       = 0.5  // 50% of the ancestor's padding box height
	absPctW         = 30.0
	absPctH         = 20.0

	// The control case: a relative parent (border 5, padding 10) with
	// an absolute child — the pin's untouched default. Its border box is
	// 150x110, so its padding box origin is (5, 5) relative to it.
	controlGap     = 40.0
	controlWidth   = 150.0
	controlHeight  = 110.0
	controlBorder  = 5.0
	controlPadding = 10.0
	ctlLeft        = 10.0
	ctlTop         = 6.0
	ctlW           = 60.0
	ctlH           = 40.0

	// The root case: a nested static chain (60x50 under 40x30) whose
	// absolute children escape to the initial containing block — the
	// 640x400 viewport in the page, the root div (which fills the
	// window) in the view.
	chainS1W = 60.0
	chainS1H = 50.0
	chainS2W = 40.0
	chainS2H = 30.0
	// rootBR: right 20 / bottom 16 of the viewport, 44x28.
	rootBRRight  = 20.0
	rootBRBottom = 16.0
	rootBRW      = 44.0
	rootBRH      = 28.0
	// rootBL: left 30 / bottom 24, 36x22 (below the escape case).
	rootBLLeft   = 30.0
	rootBLBottom = 24.0
	rootBLW      = 36.0
	rootBLH      = 22.0
)

// The palette, shared by the page and the view.
const (
	pageBackground      = 0xffffff
	ancestorBorderColor = 0x334155
	staticBackground    = 0xe2e8f0
	absRed              = 0xdc2626
	absBlue             = 0x2563eb
	absGreen            = 0x16a34a
	controlBorderColor  = 0x334155
	ctlAmber            = 0xd97706
	chainBackground     = 0xdbeafe
	rootAbsPurple       = 0x7c3aed
)

// Fixture is the position: static fixture.
var Fixture = parity.Fixture{
	Name:   "position-static",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   page,
	View:   View,
	BoxIDs: []string{
		"root",
		"ancestor",
		"static-parent",
		"abs-tl",
		"abs-br",
		"abs-pct",
		"control",
		"abs-ctl",
		"chain-s1",
		"chain-s2",
		"root-abs-br",
		"root-abs-bl",
	},
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run), set from
		// the measured correct-view statistics: text-free geometry, so
		// the replay is pixel-exact (0.0000 mean image-wide and per
		// box); the deliberately-wrong view (the pin's direct-parent
		// resolution) moves five absolute boxes and measures far above.
		// Verified by position_test.go. See docs/browser-parity-harness.md.
		MaxMeanChannelDiff:    0.02,
		MaxROIMeanChannelDiff: 0.1,
		MaxHotPixelFraction:   0.002,
	},
}

func init() { parity.Register(Fixture) }

// page is the HTML twin of View.
const page = `<!doctype html>
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
	.ancestor {
		box-sizing: border-box;
		width: 200px;
		height: 160px;
		position: relative;
		border: 6px solid #334155;
		padding: 14px;
		background: #ffffff;
	}
	.static-parent {
		box-sizing: border-box;
		width: 120px;
		height: 90px;
		/* The opt-in under test: static, so this element is not the
		   containing block of its absolute children. */
		position: static;
		/* The pin's default display, pinned on the page too. */
		display: flex;
		background: #e2e8f0;
	}
	.abs-tl {
		position: absolute;
		left: 12px;
		top: 8px;
		width: 40px;
		height: 30px;
		background: #dc2626;
	}
	.abs-br {
		position: absolute;
		right: 16px;
		bottom: 10px;
		width: 50px;
		height: 36px;
		background: #2563eb;
	}
	.abs-pct {
		position: absolute;
		left: 25%;
		top: 50%;
		width: 30px;
		height: 20px;
		background: #16a34a;
	}
	.control {
		box-sizing: border-box;
		width: 150px;
		height: 110px;
		position: relative;
		border: 5px solid #334155;
		padding: 10px;
		background: #ffffff;
	}
	.abs-ctl {
		position: absolute;
		left: 10px;
		top: 6px;
		width: 60px;
		height: 40px;
		background: #d97706;
	}
	.chain-s1 {
		box-sizing: border-box;
		width: 60px;
		height: 50px;
		position: static;
		display: flex;
		background: #dbeafe;
	}
	.chain-s2 {
		box-sizing: border-box;
		width: 40px;
		height: 30px;
		position: static;
		display: flex;
		background: #dbeafe;
	}
	.root-abs-br {
		position: absolute;
		right: 20px;
		bottom: 16px;
		width: 44px;
		height: 28px;
		background: #7c3aed;
	}
	.root-abs-bl {
		position: absolute;
		left: 30px;
		bottom: 24px;
		width: 36px;
		height: 22px;
		background: #7c3aed;
	}
</style>
</head>
<body data-id="root">
	<div class="ancestor" data-id="ancestor">
		<div class="static-parent" data-id="static-parent">
			<div class="abs-tl" data-id="abs-tl"></div>
			<div class="abs-br" data-id="abs-br"></div>
			<div class="abs-pct" data-id="abs-pct"></div>
		</div>
	</div>
	<div class="control" data-id="control">
		<div class="abs-ctl" data-id="abs-ctl"></div>
	</div>
	<div class="chain-s1" data-id="chain-s1">
		<div class="chain-s2" data-id="chain-s2">
			<div class="root-abs-br" data-id="root-abs-br"></div>
			<div class="root-abs-bl" data-id="root-abs-bl"></div>
		</div>
	</div>
</body>
</html>
`

// View is the gpui-go twin of the page: the positioned ancestor with
// its static parent and three absolute children (left/top, right/
// bottom and percentage insets), the relative control parent with its
// absolute child (the untouched default), and the nested static chain
// whose absolute children resolve against the window root.
func View(_ *parity.EventLog) gpui.AnyElement {
	return view(true)
}

// WrongDirectParent is the deliberately-wrong view for the Tier A and
// Tier C comparisons: every static parent is pinned at the port's OLD
// default (relative — stated explicitly, because ticket 76 flipped the
// default to static and the unpositioned form would now be the
// correct view), so the absolute children resolve against their
// DIRECT parent — the pin's behaviour, the exact state before ticket
// 33. Five absolute boxes move (abs-tl, abs-br, abs-pct land on the
// static parent's box instead of the ancestor's padding box;
// root-abs-br and root-abs-bl land on the inner static div instead of
// the window root), while the control case and every in-flow box are
// unchanged.
func WrongDirectParent(_ *parity.EventLog) gpui.AnyElement {
	return view(false)
}

// view builds the fixture's tree; staticParents selects the correct
// view (position: static) or the wrong one (the old default, relative,
// stated explicitly — the unpositioned form is the static default since
// ticket 76 and would be the correct view).
func view(staticParents bool) gpui.AnyElement {
	root := gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Flex().
		AlignItems(gpui.AlignItemsFlexStart).
		Gap(gpui.Px(controlGap)).
		Bg(gpui.Rgb(pageBackground)).
		Child(ancestorBox(staticParents)).
		Child(controlBox()).
		Child(chainS1(staticParents))
	return root.IntoElement()
}

// ancestorBox builds the escape case: the positioned ancestor (border
// 6, padding 14) holding the static parent holding three absolute
// children with left/top, right/bottom and percentage insets.
func ancestorBox(staticParents bool) *gpui.DivElement {
	parent := gpui.Div().Flex().
		ID("static-parent").
		DebugSelector("static-parent").
		W(gpui.Px(staticW)).
		H(gpui.Px(staticH)).
		Bg(gpui.Rgb(staticBackground))
	if staticParents {
		parent = parent.Static()
	} else {
		parent = parent.Relative()
	}
	parent = parent.
		Child(absoluteBox("abs-tl", absRed).
			Left(gpui.Px(absTLLeft)).
			Top(gpui.Px(absTLTop)).
			W(gpui.Px(absTLW)).
			H(gpui.Px(absTLH))).
		Child(absoluteBox("abs-br", absBlue).
			Right(gpui.Px(absBRRight)).
			Bottom(gpui.Px(absBRBottom)).
			W(gpui.Px(absBRW)).
			H(gpui.Px(absBRH))).
		Child(absoluteBox("abs-pct", absGreen).
			Left(gpui.Fraction(absPctLeft)).
			Top(gpui.Fraction(absPctTop)).
			W(gpui.Px(absPctW)).
			H(gpui.Px(absPctH)))
	return gpui.Div().
		ID("ancestor").
		DebugSelector("ancestor").
		W(gpui.Px(ancestorWidth)).
		H(gpui.Px(ancestorHeight)).
		Relative().
		Border(gpui.Px(ancestorBorder)).
		BorderColor(gpui.Rgb(ancestorBorderColor)).
		P(gpui.Px(ancestorPadding)).
		Bg(gpui.Rgb(pageBackground)).
		Child(parent)
}

// controlBox builds the control case: a relative parent (border 5,
// padding 10) whose absolute child resolves against it directly — the
// pin's behaviour, which the opt-in must not disturb.
func controlBox() *gpui.DivElement {
	return gpui.Div().
		ID("control").
		DebugSelector("control").
		W(gpui.Px(controlWidth)).
		H(gpui.Px(controlHeight)).
		Relative().
		Border(gpui.Px(controlBorder)).
		BorderColor(gpui.Rgb(controlBorderColor)).
		P(gpui.Px(controlPadding)).
		Bg(gpui.Rgb(pageBackground)).
		Child(absoluteBox("abs-ctl", ctlAmber).
			Left(gpui.Px(ctlLeft)).
			Top(gpui.Px(ctlTop)).
			W(gpui.Px(ctlW)).
			H(gpui.Px(ctlH)))
}

// chainS1 builds the nested static chain whose absolute children
// escape to the window root (the port's initial containing block).
func chainS1(staticParents bool) *gpui.DivElement {
	s2 := gpui.Div().Flex().
		ID("chain-s2").
		DebugSelector("chain-s2").
		W(gpui.Px(chainS2W)).
		H(gpui.Px(chainS2H)).
		Bg(gpui.Rgb(chainBackground))
	if staticParents {
		s2 = s2.Static()
	} else {
		s2 = s2.Relative()
	}
	s2 = s2.
		Child(absoluteBox("root-abs-br", rootAbsPurple).
			Right(gpui.Px(rootBRRight)).
			Bottom(gpui.Px(rootBRBottom)).
			W(gpui.Px(rootBRW)).
			H(gpui.Px(rootBRH))).
		Child(absoluteBox("root-abs-bl", rootAbsPurple).
			Left(gpui.Px(rootBLLeft)).
			Bottom(gpui.Px(rootBLBottom)).
			W(gpui.Px(rootBLW)).
			H(gpui.Px(rootBLH)))
	s1 := gpui.Div().Flex().
		ID("chain-s1").
		DebugSelector("chain-s1").
		W(gpui.Px(chainS1W)).
		H(gpui.Px(chainS1H)).
		Bg(gpui.Rgb(chainBackground))
	if staticParents {
		s1 = s1.Static()
	} else {
		s1 = s1.Relative()
	}
	return s1.Child(s2)
}

// absoluteBox builds one absolutely positioned child with the given
// id and color; the caller adds the insets and size.
func absoluteBox(id string, color uint32) *gpui.DivElement {
	return gpui.Div().
		ID(id).
		DebugSelector(id).
		Absolute().
		Bg(gpui.Rgb(color))
}
