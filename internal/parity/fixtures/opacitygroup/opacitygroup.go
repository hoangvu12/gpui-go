//go:build windows

// Package opacitygroup is ticket 55's D9 fixture: a translucent
// element with OVERLAPPING children, recorded in headless Chrome and
// replayed against the gpui-go view through the browser-parity
// harness. The overlap is the point: CSS composites the element's
// whole rendering through ONE opacity group (Chromium forces a render
// surface for opacity when at least two layers in the subtree draw
// content, cc/trees/property_tree_builder.cc:395-403), so the
// overlapping children blend together INSIDE the group before the
// group blends with the page - the overlap reads the group's single
// 0.5 alpha. The pin multiplied the opacity into every primitive's
// colour, so the overlap read 0.75 (two 0.5 blends stacked) - the
// classic overlap artifact of research report 01 §2.
//
// The page is text-free, so Tier C carries no font rasterization
// noise. The wrong view is the pre-D9 shape (the children each
// carrying the opacity, the parent fully opaque) whose overlap
// measures 0.75 - the per-primitive multiply the group replaces.
package opacitygroup

import (
	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The numbers shared by the page and the view (the tracer's
// discipline: both sides are built from one set of constants).
const (
	windowWidth  = 640.0
	windowHeight = 400.0

	// The translucent parent: 120x120 with two 60x120 children
	// overlapping by 30px (the artifact's visible band).
	parentWidth  = 120.0
	parentHeight = 120.0
	childWidth   = 60.0
	overlap      = 30.0
	groupOpacity = 0.5
)

// The palette, shared by the page and the view.
const (
	pageBackground = 0xffffff
	parentFill     = 0x94a3b8
	childRed       = 0xdc2626
	childBlue      = 0x2563eb
)

// Fixture is the D9 group-opacity fixture.
var Fixture = parity.Fixture{
	Name:   "opacitygroup",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   page,
	View:   View,
	BoxIDs: []string{"parent", "child-a", "child-b"},
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run), set from
		// the measured correct-view statistics: 0.0234 mean image-wide,
		// 0.5 in child-a's ROI and 0.417 in the parent's (the group
		// composite's 8-bit rounding of the 0.5 blends - Chrome rounds
		// the red blend to 237, the group target to 238), no hot pixels.
		// The deliberately-wrong view (the per-primitive multiply) must
		// fail it. Verified by opacitygroup_test.go.
		MaxMeanChannelDiff:    0.06,
		MaxROIMeanChannelDiff: 1.2,
		MaxHotPixelFraction:   0.002,
	},
}

func init() {
	parity.Register(Fixture)
}

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
		align-items: center;
		justify-content: center;
	}
	.parent {
		position: relative;
		box-sizing: border-box;
		width: 120px;
		height: 120px;
		background: #94a3b8;
		/* The element opacity: ONE group composites the whole subtree
		   (D9) - the overlapping children blend inside it. */
		opacity: 0.5;
	}
	.child {
		position: absolute;
		top: 0;
		width: 60px;
		height: 120px;
	}
	.child-a {
		left: 0;
		background: #dc2626;
	}
	.child-b {
		left: 30px;
		background: #2563eb;
	}
</style>
</head>
<body>
	<div class="parent" data-id="parent">
		<div class="child child-a" data-id="child-a"></div>
		<div class="child child-b" data-id="child-b"></div>
	</div>
</body>
</html>
`

// View is the gpui-go twin of page: a 0.5-opacity parent with two
// overlapping absolute children (the group carries the opacity; the
// parent's background colour stays at full alpha).
func View(_ *parity.EventLog) gpui.AnyElement {
	return root(0.5, 1, 1, 1)
}

// WrongPerPrimitiveView is the deliberately-wrong view for the Tier C
// comparison: the pre-D9 per-primitive multiply - the parent fully
// opaque with its background colour pre-multiplied to 0.5 and each
// child carrying its own 0.5 element opacity, so the overlap band
// blends twice (0.75) where the record's group blends once (0.5). The
// boxes are unchanged, so only Tier C fails.
func WrongPerPrimitiveView(_ *parity.EventLog) gpui.AnyElement {
	return root(1, 0.5, 0.5, 0.5)
}

// root builds the fixture's root: the parent (bgAlpha on its background
// colour, elemOpacity as the element opacity) with two absolute
// children (aOpacity / bOpacity), centered.
func root(elemOpacity, bgAlpha, aOpacity, bOpacity float32) gpui.AnyElement {
	parentBg := gpui.Rgb(parentFill).Opacity(bgAlpha)
	return gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Flex().
		Justify(gpui.AlignContentCenter).
		AlignItems(gpui.AlignItemsCenter).
		Bg(gpui.Rgb(pageBackground)).
		Child(gpui.Div().
			ID("parent").
			DebugSelector("parent").
			W(gpui.Px(parentWidth)).
			H(gpui.Px(parentHeight)).
			Relative().
			Bg(parentBg).
			Opacity(elemOpacity).
			Child(gpui.Div().
				ID("child-a").
				DebugSelector("child-a").
				Absolute().
				Left(gpui.Px(0)).
				Top(gpui.Px(0)).
				W(gpui.Px(childWidth)).
				H(gpui.Px(parentHeight)).
				Bg(gpui.Rgb(childRed)).
				Opacity(aOpacity)).
			Child(gpui.Div().
				ID("child-b").
				DebugSelector("child-b").
				Absolute().
				Left(gpui.Px(overlap)).
				Top(gpui.Px(0)).
				W(gpui.Px(childWidth)).
				H(gpui.Px(parentHeight)).
				Bg(gpui.Rgb(childBlue)).
				Opacity(bOpacity))).
		IntoElement()
}
