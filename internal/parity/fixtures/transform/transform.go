//go:build windows

// Package transform is browser-core ticket 56's fixture set for the
// CSS transform property and its hit testing: pages with translated,
// scaled, rotated and skewed elements, recorded in headless Chrome and
// replayed against the gpui-go view whose scene brackets each
// transformed element in a rev-16 effect group (or shifts the element
// offset, for a translation) and whose hitboxes map the pointer
// through the inverse matrix.
//
//   - transform-hit: THE spec risk, written first. A rotated square, a
//     scaled square and a translated parent with a rotated child, each
//     instrumented for mousedown, click, pointerenter and pointerleave.
//     The clicks land at points that are inside the TRANSFORMED shapes
//     but outside the layout boxes (a hit test that ignores the matrix
//     misses them) and at points inside the layout boxes but outside
//     the transformed shapes (a hit test that ignores the matrix hits
//     them): both directions of the inverse mapping, plus the nested
//     composition (the child resolves through the parent's translate
//     AND its own rotation). Tier B is the gate; Tier A pins the
//     translated parent's box (integer geometry) and the pixels pin
//     the painted composites.
//   - transform-translate, transform-scale, transform-rotate,
//     transform-skew: one box+pixel fixture per transform kind, each
//     with Tier-A-exact geometry (the mapped AABB is representable in
//     float32: integer translations, a dyadic scale factor, a
//     quarter-turn rotation and a 45-degree skew whose tangent is 1).
//   - transform-origin: two rotations of one rectangle, one about the
//     default centre origin and one about the top-left corner
//     (transform-origin: 0 0), pinning the origin resolution.
//   - transform-transition: a transform transition sampled mid-flight
//     through the recorder's paused-animation seek (the transition
//     fixture's pattern): a scale 1 -> 1.75 rule whose 200ms sample is
//     1.375 (Tier A exact) and a rotate 0 -> 90deg rule whose
//     mid-flight rotation the pixels pin.
package transform

import (
	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The shared window and palette.
const (
	windowWidth  = 320.0
	windowHeight = 240.0

	pageBackground = 0xffffff
)

// ---------------------------------------------------------------------------
// transform-hit: the event-log fixture (the spec's risk, first)
// ---------------------------------------------------------------------------

// The hit fixture's geometry: a rotated square, a scaled square, and a
// translated parent with a rotated child.
const (
	rotSize = 80.0
	rotX    = 40.0
	rotY    = 30.0
	rotDeg  = 30.0

	scaledSize = 40.0
	scaledX    = 200.0
	scaledY    = 40.0
	scaledF    = 1.8

	parentW = 120.0
	parentH = 60.0
	parentX = 40.0
	parentY = 140.0

	childSize = 50.0
	childDX   = 10.0
	childDY   = 5.0
	childDeg  = -20.0
)

// The palette.
const (
	rotBg    = 0x00aa00
	scaledBg = 0x0066ff
	parentBg = 0xcccccc
	childBg  = 0xff4444
)

// HitFixture is the event-log fixture: clicks that hit through the
// transforms and clicks that miss through them.
var HitFixture = parity.Fixture{
	Name:   "transform-hit",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   hitPage,
	Steps: []parity.Step{
		// Into the rotated square's upper-right region: inside the
		// rotated shape, outside the layout box (x > 120).
		parity.Move(126, 56),
		parity.Click(126, 56),
		// Inside the layout box's top-right corner but outside the
		// rotated shape: the click must fall through to the root.
		parity.Click(115, 35),
		// Into the scaled square's lower-right region: outside the
		// 40x40 layout box (240, 80), inside the 72x72 scaled shape.
		parity.Click(250, 90),
		// The nested case: through the parent's translate(30px, 10px)
		// and the child's rotate(-20deg) onto the child.
		parity.Click(120, 152),
		// Inside the parent's LAYOUT box but outside the translated
		// box (x < 70): the parent must miss; only the root sees it.
		parity.Click(52, 192),
	},
	EventKinds: []string{"mousedown", "click", "pointerenter", "pointerleave"},
	BoxIDs:     []string{"root", "parent"},
	View:       HitView,
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run), measured
		// from the correct view: whole mean 0.1158, hot 0.00089; the
		// parent ROI (its translated box over the rotated child) mean
		// 0.1845, hot 0.00306 — the residue is the composites'
		// antialiased edges against Chrome's rasterization (three
		// rotated or scaled shapes carry ~400 device px of edge; the
		// interiors are solid and exact). The gates sit at 2x those.
		// Verified by transform_test.go's wrong view.
		MaxMeanChannelDiff:    0.25,
		MaxROIMeanChannelDiff: 0.4,
		MaxHotPixelFraction:   0.007,
	},
}

// hitPage is the HTML twin of HitView.
const hitPage = `<!doctype html>
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
	}
	#rot {
		box-sizing: border-box;
		position: absolute;
		left: 40px;
		top: 30px;
		width: 80px;
		height: 80px;
		background: #00aa00;
		transform: rotate(30deg);
	}
	#scaled {
		box-sizing: border-box;
		position: absolute;
		left: 200px;
		top: 40px;
		width: 40px;
		height: 40px;
		background: #0066ff;
		transform: scale(1.8);
	}
	#parent {
		box-sizing: border-box;
		position: absolute;
		left: 40px;
		top: 140px;
		width: 120px;
		height: 60px;
		background: #cccccc;
		transform: translate(30px, 10px);
	}
	#child {
		box-sizing: border-box;
		position: absolute;
		left: 10px;
		top: 5px;
		width: 50px;
		height: 50px;
		background: #ff4444;
		transform: rotate(-20deg);
	}
</style>
</head>
<body data-id="root">
	<div id="rot" data-id="rot"></div>
	<div id="scaled" data-id="scaled"></div>
	<div id="parent" data-id="parent">
		<div id="child" data-id="child"></div>
	</div>
</body>
</html>
`

// HitView is the gpui-go twin of hitPage: the same absolute geometry,
// the same transforms, and the four event listeners on every element.
func HitView(log *parity.EventLog) gpui.AnyElement {
	instrument := func(d *gpui.DivElement, id string) *gpui.DivElement {
		return d.
			ID(id).
			DebugSelector(id).
			OnMouseDown(gpui.MouseButtonLeft, log.MouseDown(id)).
			OnClick(log.Click(id)).
			OnPointerEnter(log.PointerEnter(id)).
			OnPointerLeave(log.PointerLeave(id))
	}
	child := instrument(
		gpui.Div().
			Absolute().Left(gpui.Px(childDX)).Top(gpui.Px(childDY)).
			Size(gpui.Px(childSize), gpui.Px(childSize)).
			Bg(gpui.Rgb(childBg)).
			Transform(gpui.Rotate(childDeg)),
		"child")
	parent := instrument(
		gpui.Div().
			Absolute().Left(gpui.Px(parentX)).Top(gpui.Px(parentY)).
			Size(gpui.Px(parentW), gpui.Px(parentH)).
			Bg(gpui.Rgb(parentBg)).
			Transform(gpui.Translate(gpui.DefinitePx(30), gpui.DefinitePx(10))).
			Child(child.IntoElement()),
		"parent")
	rot := instrument(
		gpui.Div().
			Absolute().Left(gpui.Px(rotX)).Top(gpui.Px(rotY)).
			Size(gpui.Px(rotSize), gpui.Px(rotSize)).
			Bg(gpui.Rgb(rotBg)).
			Transform(gpui.Rotate(rotDeg)),
		"rot")
	scaled := instrument(
		gpui.Div().
			Absolute().Left(gpui.Px(scaledX)).Top(gpui.Px(scaledY)).
			Size(gpui.Px(scaledSize), gpui.Px(scaledSize)).
			Bg(gpui.Rgb(scaledBg)).
			Transform(gpui.Scale(scaledF, scaledF)),
		"scaled")
	root := instrument(
		gpui.Div().
			SizeFull().
			Bg(gpui.Rgb(pageBackground)).
			Child(rot.IntoElement()).
			Child(scaled.IntoElement()).
			Child(parent.IntoElement()),
		"root")
	return root.IntoElement()
}

// HitNoTransformView is the deliberately-wrong view: the same
// elements and listeners without any transforms, so the hit test sees
// the layout boxes. The clicks that must hit through the transforms
// miss their targets and the clicks that must miss hit them — Tier B
// fails, with the painted boxes (Tier A and C) failing too.
func HitNoTransformView(log *parity.EventLog) gpui.AnyElement {
	instrument := func(d *gpui.DivElement, id string) *gpui.DivElement {
		return d.
			ID(id).
			DebugSelector(id).
			OnMouseDown(gpui.MouseButtonLeft, log.MouseDown(id)).
			OnClick(log.Click(id)).
			OnPointerEnter(log.PointerEnter(id)).
			OnPointerLeave(log.PointerLeave(id))
	}
	child := instrument(
		gpui.Div().
			Absolute().Left(gpui.Px(childDX)).Top(gpui.Px(childDY)).
			Size(gpui.Px(childSize), gpui.Px(childSize)).
			Bg(gpui.Rgb(childBg)),
		"child")
	parent := instrument(
		gpui.Div().
			Absolute().Left(gpui.Px(parentX)).Top(gpui.Px(parentY)).
			Size(gpui.Px(parentW), gpui.Px(parentH)).
			Bg(gpui.Rgb(parentBg)).
			Child(child.IntoElement()),
		"parent")
	rot := instrument(
		gpui.Div().
			Absolute().Left(gpui.Px(rotX)).Top(gpui.Px(rotY)).
			Size(gpui.Px(rotSize), gpui.Px(rotSize)).
			Bg(gpui.Rgb(rotBg)),
		"rot")
	scaled := instrument(
		gpui.Div().
			Absolute().Left(gpui.Px(scaledX)).Top(gpui.Px(scaledY)).
			Size(gpui.Px(scaledSize), gpui.Px(scaledSize)).
			Bg(gpui.Rgb(scaledBg)),
		"scaled")
	root := instrument(
		gpui.Div().
			SizeFull().
			Bg(gpui.Rgb(pageBackground)).
			Child(rot.IntoElement()).
			Child(scaled.IntoElement()).
			Child(parent.IntoElement()),
		"root")
	return root.IntoElement()
}

func init() {
	parity.Register(HitFixture)
}
