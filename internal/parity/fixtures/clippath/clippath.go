//go:build windows

// Package clippath is ticket 57's clip-path fixture: two boxes clipped
// by basic shapes — circle(40px) and the bottom triangle polygon —
// recorded in headless Chrome and replayed against the gpui-go view
// through the browser-parity harness. The clip composites the
// element's rendering against the rasterized shape (rev 16's coverage
// entry, ticket 52) and SHAPES POINTER EVENTS (css-masking-1: a click
// inside the element's bounds but outside the shape misses the element
// and falls through to what is behind) — the event-log half of the
// record: the corner click on the circle box logs only the root.
//
// The page is text-free. The wrong view drops the clip-paths — the
// full squares — and must fail Tier C (and Tier B, whose transcript
// gains the corner clicks the record does not have).
package clippath

import (
	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The numbers shared by the page and the view.
const (
	windowWidth  = 640.0
	windowHeight = 400.0

	boxSize = 120.0

	// The two boxes, laid out absolutely inside the full-window
	// relative root.
	circleX = 60.0
	circleY = 120.0
	circleR = 40.0

	polyX = 300.0
	polyY = 120.0
)

// The click targets (window coordinates): inside the circle, the
// circle box's corner (inside its bounds, OUTSIDE the shape — the
// miss), inside the triangle, and the triangle box's top-left (outside
// the shape — another miss).
const (
	clickInsideCircleX = circleX + boxSize/2
	clickInsideCircleY = circleY + boxSize/2

	clickOutsideCircleX = circleX + 8
	clickOutsideCircleY = circleY + 8

	clickInsidePolyX = polyX + boxSize/2
	clickInsidePolyY = polyY + boxSize*0.8

	clickOutsidePolyX = polyX + 10
	clickOutsidePolyY = polyY + 10
)

// The palette, shared by the page and the view.
const (
	pageBackground = 0xffffff
	circleFill     = 0xdc2626
	polyFill       = 0x2563eb
)

// Fixture is the clip-path fixture.
var Fixture = parity.Fixture{
	Name:   "clippath",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   page,
	Steps: []parity.Step{
		parity.Click(clickInsideCircleX, clickInsideCircleY),
		parity.Click(clickOutsideCircleX, clickOutsideCircleY),
		parity.Click(clickInsidePolyX, clickInsidePolyY),
		parity.Click(clickOutsidePolyX, clickOutsidePolyY),
	},
	BoxIDs:     []string{"clip-circle", "clip-poly", "root"},
	EventKinds: []string{"click"},
	View:       View,
	Tolerance: parity.PixelTolerance{
		// Provisional: the correct view's statistics are the shape
		// edges' anti-aliasing (Chrome's analytic circle against the
		// port's MSAA-resolved tessellation) over a thin band of each
		// box's ROI. Verified by clippath_test.go.
		MaxMeanChannelDiff:    0.1,
		MaxROIMeanChannelDiff: 2.0,
		MaxHotPixelFraction:   0.01,
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
	}
	.root {
		position: relative;
		box-sizing: border-box;
		width: 640px;
		height: 400px;
		background: #ffffff;
	}
	.box {
		position: absolute;
		box-sizing: border-box;
		width: 120px;
		height: 120px;
	}
	.circle {
		left: 60px;
		top: 120px;
		background: #dc2626;
		clip-path: circle(40px);
	}
	.poly {
		left: 300px;
		top: 120px;
		background: #2563eb;
		clip-path: polygon(50% 0, 100% 100%, 0 100%);
	}
</style>
</head>
<body>
	<div class="root" data-id="root" data-events="click">
		<div class="box circle" data-id="clip-circle" data-events="click"></div>
		<div class="box poly" data-id="clip-poly" data-events="click"></div>
	</div>
</body>
</html>
`

// View is the gpui-go twin of page: the relative root with the two
// absolutely-placed clipped boxes; all three log clicks (the clipped
// boxes' hit tests answer point-in-shape, so the corner clicks miss
// them and reach the root alone).
func View(log *parity.EventLog) gpui.AnyElement {
	return root(log, true)
}

// WrongUnclippedView is the deliberately-wrong view for the
// comparisons: the clip-paths dropped — the full squares. Tier C fails
// (the corners the shapes cut away), and Tier B fails too (the corner
// clicks now hit the boxes, transcripts the record does not have).
func WrongUnclippedView(log *parity.EventLog) gpui.AnyElement {
	return root(log, false)
}

// root builds the fixture's root: the two clipped (or not) boxes at
// their absolute positions, every element logging clicks.
func root(log *parity.EventLog, clipped bool) gpui.AnyElement {
	circle := gpui.Div().
		ID("clip-circle").
		DebugSelector("clip-circle").
		Absolute().
		Left(gpui.Px(circleX)).
		Top(gpui.Px(circleY)).
		W(gpui.Px(boxSize)).
		H(gpui.Px(boxSize)).
		Bg(gpui.Rgb(circleFill)).
		OnClick(log.Click("clip-circle"))
	if clipped {
		circle = circle.ClipPath(gpui.ClipPathCircle(gpui.ClipPx(circleR)))
	}
	poly := gpui.Div().
		ID("clip-poly").
		DebugSelector("clip-poly").
		Absolute().
		Left(gpui.Px(polyX)).
		Top(gpui.Px(polyY)).
		W(gpui.Px(boxSize)).
		H(gpui.Px(boxSize)).
		Bg(gpui.Rgb(polyFill)).
		OnClick(log.Click("clip-poly"))
	if clipped {
		poly = poly.ClipPath(gpui.ClipPathPolygon(
			gpui.ClipPointFraction(0.5, 0),
			gpui.ClipPointFraction(1, 1),
			gpui.ClipPointFraction(0, 1),
		))
	}
	return gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Relative().
		Bg(gpui.Rgb(pageBackground)).
		OnClick(log.Click("root")).
		Child(circle.IntoElement()).
		Child(poly.IntoElement()).
		IntoElement()
}
