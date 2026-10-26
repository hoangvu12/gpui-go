//go:build windows

// Package borderorder is ticket 55's border-order fixture: a bordered
// box with an overflowing child, recorded in headless Chrome and
// replayed against the gpui-go view through the browser-parity
// harness. CSS paints the element's borders in the BACKGROUND phase -
// backgrounds AND borders before the foreground (Chromium's paint
// phase list: "Background phase: backgrounds and borders of all blocks
// are painted", core/paint/paint_phase.h:33-35, the phase before the
// children; box_fragment_painter.cc's
// PaintBoxDecorationBackgroundWithRectImpl paints the border as the
// decoration phase's last step) - so a child that pokes over the
// border box paints ABOVE the border. The pin painted the border after
// the children, so the border covered the overflowing child.
//
// The child here is a 200x200 red block in a 200x120 12px-bordered box
// (the port's default display is block, no flex pin): it covers the
// box's right and bottom border bands entirely, so the record shows
// the border interrupted by the child where the pin showed it
// continuous. The page is text-free.
package borderorder

import (
	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The numbers shared by the page and the view.
const (
	windowWidth  = 640.0
	windowHeight = 400.0

	// The box: 200x120 with a 12px border; the child (200x200 at the
	// content origin) covers the right and bottom border bands and
	// overflows past the border box's bottom edge.
	boxWidth    = 200.0
	boxHeight   = 120.0
	borderSize  = 12.0
	childWidth  = 200.0
	childHeight = 200.0
)

// The palette, shared by the page and the view.
const (
	pageBackground = 0xffffff
	borderDark     = 0x334155
	childRed       = 0xdc2626
)

// Fixture is the border-order fixture.
var Fixture = parity.Fixture{
	Name:   "borderorder",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   page,
	View:   View,
	BoxIDs: []string{"box", "poker"},
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run), set from
		// the measured correct-view statistics (the fixture is text-free
		// geometry, so the replay is near pixel-exact); the
		// deliberately-wrong view (the border painted over the child)
		// must fail it. Verified by borderorder_test.go.
		MaxMeanChannelDiff:    0.02,
		MaxROIMeanChannelDiff: 0.1,
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
	.box {
		box-sizing: border-box;
		width: 200px;
		height: 120px;
		border: 12px solid #334155;
		/* overflow: visible (the default): the child paints over the
		   border, past the padding box and past the border box. */
		overflow: visible;
	}
	.poker {
		width: 200px;
		height: 200px;
		background: #dc2626;
	}
</style>
</head>
<body>
	<div class="box" data-id="box">
		<div class="poker" data-id="poker"></div>
	</div>
</body>
</html>
`

// View is the gpui-go twin of page: the bordered box whose overflowing
// child paints over the border.
func View(_ *parity.EventLog) gpui.AnyElement {
	return root(false)
}

// WrongBorderOverChildView is the deliberately-wrong view for the Tier
// C comparison: the pin's order reproduced - a border-only overlay
// absolutely positioned over the box (the last child paints on top),
// so the border covers the overflowing child exactly as the
// paint-after-children border did. The boxes are unchanged, so only
// Tier C fails.
func WrongBorderOverChildView(_ *parity.EventLog) gpui.AnyElement {
	return root(true)
}

// root builds the fixture's root: the centered bordered box with the
// overflowing poker, plus the border-only overlay when wrong is set.
func root(wrong bool) gpui.AnyElement {
	box := gpui.Div().
		ID("box").
		DebugSelector("box").
		W(gpui.Px(boxWidth)).
		H(gpui.Px(boxHeight)).
		Border(gpui.Px(borderSize)).
		BorderColor(gpui.Rgb(borderDark)).
		Relative().
		Child(gpui.Div().
			ID("poker").
			DebugSelector("poker").
			W(gpui.Px(childWidth)).
			H(gpui.Px(childHeight)).
			Bg(gpui.Rgb(childRed)))
	if wrong {
		// The overlay repaints the border on top of the poker: the
		// absolute offsets resolve against the box's PADDING box (CSS's
		// containing-block rule), so -12/-12 lands on the border box.
		box = box.Child(gpui.Div().
			Absolute().
			Left(gpui.Px(-borderSize)).
			Top(gpui.Px(-borderSize)).
			W(gpui.Px(boxWidth)).
			H(gpui.Px(boxHeight)).
			Border(gpui.Px(borderSize)).
			BorderColor(gpui.Rgb(borderDark)))
	}
	return gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Flex().
		Justify(gpui.AlignContentCenter).
		AlignItems(gpui.AlignItemsCenter).
		Bg(gpui.Rgb(pageBackground)).
		Child(box).
		IntoElement()
}
