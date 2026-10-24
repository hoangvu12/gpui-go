//go:build windows

// Package tracer is ticket 01's tracer fixture: a padded, bordered box
// with text and a click handler, centered in an 800x600 page. It is
// the first end-to-end proof of the browser-parity harness (boxes
// exactly, the event transcript exactly, pixels approximately) and the
// template for every later fixture.
package tracer

import (
	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The page's numbers, shared by the HTML and the gpui view. Where a
// gpui-go default differs from a browser today, the page pins the
// gpui-go value explicitly so both sides draw the same thing: the line
// height is 1.618 (the pin's default, before D1 lands).
const (
	windowWidth  = 640.0
	windowHeight = 400.0

	boxWidth     = 200.0
	boxHeight    = 80.0
	boxPadding   = 8.0
	boxBorder    = 1.0
	wrongPadding = 24.0

	pageBackground = 0xffffff
	boxBackground  = 0xe2e8f0
	borderColor    = 0x334155
	textColor      = 0x0f172a

	textSize = 16.0

	// clickX/clickY is where the click lands: inside the box, clear of
	// the text run so the fixture's own listener is unambiguously the
	// target on both sides.
	clickX = 400.0
	clickY = 230.0
)

// Fixture is the tracer fixture.
var Fixture = parity.Fixture{
	Name:   "tracer-box-click",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   page,
	Steps: []parity.Step{
		parity.Click(clickX, clickY),
	},
	BoxIDs:     []string{"box", "root"},
	EventKinds: []string{"mousedown", "click"},
	View:       View,
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run): the
		// measured correct-vs-correct statistics are 0.014 mean
		// image-wide / 0.231 in the box ROI / 0.0029 hot, and the
		// deliberately-wrong views measure 0.538-3.496 (wrong box) and
		// 0.817 ROI (wrong padding); the gates sit between with roughly
		// 2x headroom over the correct values. Verified by
		// tracer_test.go. See docs/browser-parity-harness.md.
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
		font: 16px/1.618 "Segoe UI", system-ui, sans-serif;
	}
	.box {
		box-sizing: border-box;
		width: 200px;
		height: 80px;
		padding: 8px;
		border: 1px solid #334155;
		background: #e2e8f0;
		color: #0f172a;
	}
</style>
</head>
<body data-id="root">
	<div class="box" data-id="box">Hello!</div>
</body>
</html>
`

// View is the gpui-go twin of the page: a full-window white root that
// centers the padded, bordered box with text. Both elements log
// mousedown and click — the box is the click's target and the root
// receives it by bubbling, which both sides deliver in the same order
// (target first, then ancestors). The mouseup listener is not
// instrumented — a shape the fixture chose while the pin still fired
// OnClick before the OnMouseUp listeners during the up dispatch
// (browser-core ticket 12 closed that divergence; mouseup-then-click
// is now pinned by ticket 12's activation fixtures, and this record
// stays as recorded).
func View(log *parity.EventLog) gpui.AnyElement {
	return boxElement(log, boxWidth, boxHeight, boxPadding, true)
}

// WrongBoxSize is the deliberately-wrong view for the Tier A
// comparison: the box is 220x90, so its box differs from the record.
func WrongBoxSize(log *parity.EventLog) gpui.AnyElement {
	return boxElement(log, boxWidth+20, boxHeight+10, boxPadding, true)
}

// WrongPadding is the deliberately-wrong view for the Tier C
// comparison: 24px padding moves the text inside the same-sized box.
func WrongPadding(log *parity.EventLog) gpui.AnyElement {
	return boxElement(log, boxWidth, boxHeight, wrongPadding, true)
}

// WithoutClick is the deliberately-wrong view for the Tier B
// comparison: no click handler, so the transcript loses "click:box".
// The view itself renders identically, so only Tier B fails.
func WithoutClick(log *parity.EventLog) gpui.AnyElement {
	return boxElement(log, boxWidth, boxHeight, boxPadding, false)
}

// boxElement builds the tracer's view with the given shape. Lengths
// are explicit px (the shared numbers above); the ids are the page's
// data-ids, which the replay's box comparison and event transcript
// key on.
func boxElement(log *parity.EventLog, width, height, pad float32, withClick bool) gpui.AnyElement {
	box := gpui.Div().
		ID("box").
		DebugSelector("box").
		Size(gpui.Px(width), gpui.Px(height)).
		Bg(gpui.Rgb(boxBackground)).
		P(gpui.Px(pad)).
		Border(gpui.Px(boxBorder)).
		BorderColor(gpui.Rgb(borderColor)).
		TextColor(gpui.Rgb(textColor)).
		TextSize(gpui.Px(textSize)).
		// The page pins the reference's 1.618 line height (the body's
		// `font: 16px/1.618`); the view mirrors it with the golden-ratio
		// opt-in — the port's default is CSS `normal` since browser-parity
		// D1, so the pin must be explicit to draw the page's text
		// placement.
		LineHeight(gpui.PhiLineHeight()).
		OnMouseDown(gpui.MouseButtonLeft, log.MouseDown("box"))
	if withClick {
		box = box.OnClick(log.Click("box"))
	}
	root := gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Flex().
		Justify(gpui.AlignContentCenter).
		AlignItems(gpui.AlignItemsCenter).
		Bg(gpui.Rgb(pageBackground)).
		OnMouseDown(gpui.MouseButtonLeft, log.MouseDown("root"))
	if withClick {
		root = root.OnClick(log.Click("root"))
	}
	return root.
		Child(box.Child("Hello!")).
		IntoElement()
}
