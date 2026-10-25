//go:build windows

// Package hover is browser-core ticket 10's fixture: the pointer
// enter/leave boundary order across nested elements, recorded in
// headless Chrome and replayed against the gpui view. The pointer
// moves from outside into a child (enter fires outermost-first: p,
// then c1), from one sibling to the other (leave on the exited child,
// enter on the entered one), back out (leave fires innermost-first:
// c2, then p) and finally out of the window (leave on the root), the
// exact order Chromium's boundary event dispatcher produces
// (third_party/blink/renderer/core/input/boundary_event_dispatcher.cc:
// 190-193 leave child-to-parent, :211-214 enter parent-to-child).
package hover

import (
	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The page's numbers, shared by the HTML and the gpui view (the tracer
// pattern): body 640x400 flex-centering p (260x120, 20px padding,
// border-box), whose row of children c1 and c2 (100x80 each, 220px of
// content) places c1 at (210,160) and c2 at (310,160).
const (
	windowWidth  = 640.0
	windowHeight = 400.0

	pWidth  = 260.0
	pHeight = 120.0
	pPad    = 20.0

	childWidth  = 100.0
	childHeight = 80.0

	pageBackground = 0xffffff
	pBackground    = 0xe2e8f0
	c1Background   = 0xfde68a
	c2Background   = 0xbbf7d0

	// The scripted pointer positions: the root's background, the
	// centers of c1 and c2, back to the background, and finally beyond
	// the viewport's bottom-right corner (the pointer leaving the
	// window, which fires the leave on the root like WM_MOUSELEAVE
	// does — the DOM-visible half of ticket 10's window-leave rule).
	outsideX   = 20.0
	outsideY   = 20.0
	c1X        = 260.0
	c1Y        = 200.0
	c2X        = 360.0
	c2Y        = 200.0
	windowOutX = 700.0
	windowOutY = 450.0
)

// Fixture is the hover boundary fixture.
var Fixture = parity.Fixture{
	Name:   "hover-enter-leave",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   page,
	Steps: []parity.Step{
		parity.Move(outsideX, outsideY),
		parity.Move(c1X, c1Y),
		parity.Move(c2X, c2Y),
		parity.Move(outsideX, outsideY),
		parity.Move(windowOutX, windowOutY),
	},
	BoxIDs:     []string{"root", "p", "c1", "c2"},
	EventKinds: []string{"pointerenter", "pointerleave"},
	View:       View,
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run): the page
		// is four solid rectangles with no text, so the measured
		// correct-view statistics are 0.0000 across the board (verified
		// by TestPixelTolerancesKeepHeadroom); the gates keep the tracer's
		// house values with full headroom. See
		// docs/browser-parity-harness.md.
		MaxMeanChannelDiff:    0.1,
		MaxROIMeanChannelDiff: 0.5,
		MaxHotPixelFraction:   0.006,
	},
}

func init() { parity.Register(Fixture) }

// page is the HTML/CSS twin of the gpui view below.
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
	.p {
		box-sizing: border-box;
		width: 260px;
		height: 120px;
		padding: 20px;
		display: flex;
		justify-content: flex-start;
		background: #e2e8f0;
	}
	.c1 {
		box-sizing: border-box;
		width: 100px;
		height: 80px;
		background: #fde68a;
	}
	.c2 {
		box-sizing: border-box;
		width: 100px;
		height: 80px;
		background: #bbf7d0;
	}
</style>
</head>
<body data-id="root">
	<div class="p" data-id="p">
		<div class="c1" data-id="c1"></div>
		<div class="c2" data-id="c2"></div>
	</div>
</body>
</html>
`

// View is the gpui-go twin of the page: a full-window root that
// flex-centers p, whose row of children holds c1 and c2. All four
// elements log pointerenter and pointerleave; the boundary sweeps
// deliver them in Chromium's order (gpui/hoverboundary.go).
func View(log *parity.EventLog) gpui.AnyElement {
	child := func(id string, background uint32) *gpui.DivElement {
		return gpui.Div().
			ID(id).
			DebugSelector(id).
			Size(gpui.Px(childWidth), gpui.Px(childHeight)).
			Bg(gpui.Rgb(background)).
			OnPointerEnter(log.PointerEnter(id)).
			OnPointerLeave(log.PointerLeave(id))
	}
	p := gpui.Div().
		ID("p").
		DebugSelector("p").
		Size(gpui.Px(pWidth), gpui.Px(pHeight)).
		P(gpui.Px(pPad)).
		Flex().
		Bg(gpui.Rgb(pBackground)).
		OnPointerEnter(log.PointerEnter("p")).
		OnPointerLeave(log.PointerLeave("p")).
		Child(child("c1", c1Background).IntoElement()).
		Child(child("c2", c2Background).IntoElement())
	return gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Flex().
		Justify(gpui.AlignContentCenter).
		AlignItems(gpui.AlignItemsCenter).
		Bg(gpui.Rgb(pageBackground)).
		OnPointerEnter(log.PointerEnter("root")).
		OnPointerLeave(log.PointerLeave("root")).
		Child(p).
		IntoElement()
}

// WithoutEnterListeners is the deliberately-wrong view for the Tier B
// comparison: only the leaves are instrumented, so the transcript
// loses every pointerenter. The view renders identically, so only
// Tier B fails.
func WithoutEnterListeners(log *parity.EventLog) gpui.AnyElement {
	child := func(id string, background uint32) *gpui.DivElement {
		return gpui.Div().
			ID(id).
			DebugSelector(id).
			Size(gpui.Px(childWidth), gpui.Px(childHeight)).
			Bg(gpui.Rgb(background)).
			OnPointerLeave(log.PointerLeave(id))
	}
	p := gpui.Div().
		ID("p").
		DebugSelector("p").
		Size(gpui.Px(pWidth), gpui.Px(pHeight)).
		P(gpui.Px(pPad)).
		Flex().
		Bg(gpui.Rgb(pBackground)).
		OnPointerLeave(log.PointerLeave("p")).
		Child(child("c1", c1Background).IntoElement()).
		Child(child("c2", c2Background).IntoElement())
	return gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Flex().
		Justify(gpui.AlignContentCenter).
		AlignItems(gpui.AlignItemsCenter).
		Bg(gpui.Rgb(pageBackground)).
		OnPointerLeave(log.PointerLeave("root")).
		Child(p).
		IntoElement()
}
