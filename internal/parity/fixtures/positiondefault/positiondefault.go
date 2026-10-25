//go:build windows

// Package positiondefault is ticket 76's fixture for the flipped
// default position (spec D3, the second step): with no Static()
// anywhere, an unpositioned parent is static by default, so an
// absolute child resolves against the nearest positioned ancestor —
// the grandparent in the escape case, and the window root (the
// initial containing block) when no ancestor is positioned at all.
// The parents are as plain as the page's divs: no display, no
// position, just size and margins (the block default of browser-core
// 71 and the static default of this ticket — the browser's plain
// div). The page is text-free geometry (the boxpaint discipline), so
// Tier C carries no font rasterization noise.
//
// Chromium decides the containing block the way this fixture records:
// an object can contain absolute children when its position is not
// static (LayoutObject::ComputeIsAbsoluteContainer,
// core/layout/layout_object.cc:2072-2082 in the research checkout),
// and the containing block of an absolute is the first ancestor with
// that bit (LayoutObject::ContainerForAbsolutePosition,
// layout_object.cc:1877-1883); with no positioned ancestor the
// LayoutView contains it (ComputeIsFixedContainer's IsA<LayoutView>
// arm, layout_object.cc:2035-2041) — the initial containing block the
// port's window root stands in for. The inset basis is the containing
// block's padding box ("|inset| is relative to the container's
// padding-box", out_of_flow_layout_part.cc:2574-2576), which is why
// the grandparent carries border and padding.
package positiondefault

import (
	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The page's numbers, shared by the HTML and the gpui view (the
// tracer's discipline: one set of constants builds both sides).
const (
	windowWidth  = 640.0
	windowHeight = 400.0
	columnGap    = 40.0

	// The escape case: a positioned grandparent (border 6, padding 14)
	// holding an UNPOSITIONED parent (120x90, margin 10/6 so the wrong
	// view's direct-parent resolution lands elsewhere) holding an
	// absolute child directly, and a nested UNPOSITIONED parent
	// (60x50, margins 8/40) holding a second absolute child. The
	// grandparent's border box is 200x160 at the flow origin, so its
	// padding box origin is (6, 6) and its size is 188x148.
	grandWidth   = 200.0
	grandHeight  = 160.0
	grandBorder  = 6.0
	grandPadding = 14.0

	parentW   = 120.0
	parentH   = 90.0
	parentML  = 10.0
	parentMT  = 6.0
	parent2W  = 60.0
	parent2H  = 50.0
	parent2ML = 8.0
	parent2MT = 40.0

	// childA: the absolute child of the unpositioned parent.
	childALeft = 12.0
	childATop  = 8.0
	childAW    = 40.0
	childAH    = 30.0

	// childB: the absolute child of the nested unpositioned parent.
	childBRight  = 16.0
	childBBottom = 10.0
	childBW      = 50.0
	childBH      = 36.0

	// The root case: an UNPOSITIONED chain (outer 60x50, inner 40x30)
	// whose absolute children escape past every ancestor to the
	// initial containing block — the 640x400 viewport in the page, the
	// root div (which fills the window) in the view.
	chainW  = 60.0
	chainH  = 50.0
	innerW  = 40.0
	innerH  = 30.0
	innerML = 6.0
	innerMT = 40.0

	// rootBR: right 24 / bottom 18 of the window, 40x26.
	rootBRRRight  = 24.0
	rootBRBottom  = 18.0
	rootBRW       = 40.0
	rootBRH       = 26.0
	rootBLLeft    = 30.0
	rootBLBottom2 = 24.0
	rootBLW       = 36.0
	rootBLH       = 22.0
)

// The palette, shared by the page and the view.
const (
	pageBackground   = 0xffffff
	grandBorderColor = 0x334155
	parentBg         = 0xe2e8f0
	nestedBg         = 0xdbeafe
	childARed        = 0xdc2626
	childBBlue       = 0x2563eb
	rootAbsPurple    = 0x7c3aed
)

// Fixture is the position-default fixture.
var Fixture = parity.Fixture{
	Name:   "position-default",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   page,
	View:   View,
	BoxIDs: []string{
		"root",
		"grandparent",
		"parent",
		"parent2",
		"child-a",
		"child-b",
		"chain-outer",
		"chain-inner",
		"root-abs-br",
		"root-abs-bl",
	},
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run), set from
		// the measured correct-view statistics: text-free geometry, so
		// the replay is pixel-exact (0.0000 mean image-wide and per
		// box); the deliberately-wrong view (the old relative default,
		// every parent pinned Relative) moves the four absolute boxes
		// and measures far above. Verified by positiondefault_test.go.
		// See docs/browser-parity-harness.md.
		MaxMeanChannelDiff:    0.02,
		MaxROIMeanChannelDiff: 0.1,
		MaxHotPixelFraction:   0.002,
	},
}

func init() { parity.Register(Fixture) }

// page is the HTML twin of View: the positioned grandparent with its
// plain (unpositioned) parent and nested parent, and the plain chain
// whose absolute children resolve against the initial containing
// block. No display and no position appear on the parents — the
// browser's plain div, exactly the shape ticket 76's default mirrors.
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
	#grandparent {
		box-sizing: border-box;
		width: 200px;
		height: 160px;
		position: relative;
		border: 6px solid #334155;
		padding: 14px;
		background: #ffffff;
	}
	/* The parents under test: no position (the browser default is
	   static, so they are NOT containing blocks), no display (the
	   browser default is block). */
	#parent {
		box-sizing: border-box;
		width: 120px;
		height: 90px;
		margin-left: 10px;
		margin-top: 6px;
		background: #e2e8f0;
	}
	#parent2 {
		box-sizing: border-box;
		width: 60px;
		height: 50px;
		margin-left: 8px;
		margin-top: 40px;
		background: #dbeafe;
	}
	#child-a {
		position: absolute;
		left: 12px;
		top: 8px;
		width: 40px;
		height: 30px;
		background: #dc2626;
	}
	#child-b {
		position: absolute;
		right: 16px;
		bottom: 10px;
		width: 50px;
		height: 36px;
		background: #2563eb;
	}
	#chain-outer {
		box-sizing: border-box;
		width: 60px;
		height: 50px;
		background: #dbeafe;
	}
	#chain-inner {
		box-sizing: border-box;
		width: 40px;
		height: 30px;
		margin-left: 6px;
		margin-top: 40px;
		background: #dbeafe;
	}
	#root-abs-br {
		position: absolute;
		right: 24px;
		bottom: 18px;
		width: 40px;
		height: 26px;
		background: #7c3aed;
	}
	#root-abs-bl {
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
	<div id="grandparent" data-id="grandparent">
		<div id="parent" data-id="parent">
			<div id="child-a" data-id="child-a"></div>
			<div id="parent2" data-id="parent2">
				<div id="child-b" data-id="child-b"></div>
			</div>
		</div>
	</div>
	<div id="chain-outer" data-id="chain-outer">
		<div id="chain-inner" data-id="chain-inner">
			<div id="root-abs-br" data-id="root-abs-br"></div>
			<div id="root-abs-bl" data-id="root-abs-bl"></div>
		</div>
	</div>
</body>
</html>
`

// View is the gpui-go twin of the page: the positioned grandparent
// holding two nested UNPOSITIONED parents whose absolute children
// resolve against the grandparent's padding box, and the unpositioned
// chain whose absolute children resolve against the window root (the
// initial containing block). No parent states a display or a
// position — the flipped defaults (block, static) mirror the page's
// plain divs.
func View(_ *parity.EventLog) gpui.AnyElement {
	return view(false)
}

// WrongRelative is the deliberately-wrong view for the Tier A and
// Tier C comparisons: every unpositioned parent is pinned at the
// port's OLD default (relative), so the four absolute children
// resolve against their DIRECT parents — the pre-76 pin. child-a and
// child-b land on the parent's and parent2's boxes instead of the
// grandparent's padding box; root-abs-br and root-abs-bl land on the
// chain's inner div instead of the window root, while every in-flow
// box is unchanged.
func WrongRelative(_ *parity.EventLog) gpui.AnyElement {
	return view(true)
}

// view builds the fixture's tree; oldRelative pins every parent at
// the pre-76 default (relative) — the deliberately-wrong view — while
// the correct view leaves them unpositioned (the static default).
func view(oldRelative bool) gpui.AnyElement {
	parent := gpui.Div().
		ID("parent").
		DebugSelector("parent").
		W(gpui.Px(parentW)).
		H(gpui.Px(parentH)).
		Ml(gpui.Px(parentML)).
		Mt(gpui.Px(parentMT)).
		Bg(gpui.Rgb(parentBg))
	if oldRelative {
		parent = parent.Relative()
	}
	parent2 := gpui.Div().
		ID("parent2").
		DebugSelector("parent2").
		W(gpui.Px(parent2W)).
		H(gpui.Px(parent2H)).
		Ml(gpui.Px(parent2ML)).
		Mt(gpui.Px(parent2MT)).
		Bg(gpui.Rgb(nestedBg))
	if oldRelative {
		parent2 = parent2.Relative()
	}
	parent = parent.
		Child(absoluteBox("child-a", childARed).
			Left(gpui.Px(childALeft)).
			Top(gpui.Px(childATop)).
			W(gpui.Px(childAW)).
			H(gpui.Px(childAH))).
		Child(parent2.
			Child(absoluteBox("child-b", childBBlue).
				Right(gpui.Px(childBRight)).
				Bottom(gpui.Px(childBBottom)).
				W(gpui.Px(childBW)).
				H(gpui.Px(childBH))))
	grandparent := gpui.Div().
		ID("grandparent").
		DebugSelector("grandparent").
		W(gpui.Px(grandWidth)).
		H(gpui.Px(grandHeight)).
		Relative().
		Border(gpui.Px(grandBorder)).
		BorderColor(gpui.Rgb(grandBorderColor)).
		P(gpui.Px(grandPadding)).
		Bg(gpui.Rgb(pageBackground)).
		Child(parent)

	inner := gpui.Div().
		ID("chain-inner").
		DebugSelector("chain-inner").
		W(gpui.Px(innerW)).
		H(gpui.Px(innerH)).
		Ml(gpui.Px(innerML)).
		Mt(gpui.Px(innerMT)).
		Bg(gpui.Rgb(nestedBg))
	if oldRelative {
		inner = inner.Relative()
	}
	inner = inner.
		Child(absoluteBox("root-abs-br", rootAbsPurple).
			Right(gpui.Px(rootBRRRight)).
			Bottom(gpui.Px(rootBRBottom)).
			W(gpui.Px(rootBRW)).
			H(gpui.Px(rootBRH))).
		Child(absoluteBox("root-abs-bl", rootAbsPurple).
			Left(gpui.Px(rootBLLeft)).
			Bottom(gpui.Px(rootBLBottom2)).
			W(gpui.Px(rootBLW)).
			H(gpui.Px(rootBLH)))
	outer := gpui.Div().
		ID("chain-outer").
		DebugSelector("chain-outer").
		W(gpui.Px(chainW)).
		H(gpui.Px(chainH)).
		Bg(gpui.Rgb(nestedBg))
	if oldRelative {
		outer = outer.Relative()
	}
	outer = outer.Child(inner)

	return gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Flex().
		AlignItems(gpui.AlignItemsFlexStart).
		Gap(gpui.Px(columnGap)).
		Bg(gpui.Rgb(pageBackground)).
		Child(grandparent).
		Child(outer).
		IntoElement()
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
