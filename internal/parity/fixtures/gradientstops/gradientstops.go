//go:build windows

// Package gradientstops is ticket 57's n-stop and radial gradient
// fixture: a three-stop linear gradient (Tailwind's from/via/to shape,
// the stops carrying NO positions — CSS's color-stop fixup
// distributes the middle stop to the halfway point) and the default
// radial gradient (the ellipse to the farthest corner), recorded in
// headless Chrome and replayed against the gpui-go view through the
// browser-parity harness. Both ride the scene gradient table (rev 16,
// ticket 52) resolved at paint time (browser-core ticket 57's CSS
// gradient arm).
//
// The page is text-free. The wrong view collapses both to two-stop
// linear gradients — the shape the R15 record carried — and must fail
// Tier C.
package gradientstops

import (
	"math"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The numbers shared by the page and the view.
const (
	windowWidth  = 640.0
	windowHeight = 400.0

	boxWidth  = 180.0
	boxHeight = 120.0
)

// The palette, shared by the page and the view.
const (
	pageBackground = 0xffffff

	linearFrom = 0xef4444
	linearVia  = 0xf97316
	linearTo   = 0x3b82f6

	radialFrom = 0xdc2626
	radialTo   = 0x2563eb
)

// unspecified is CSS's omitted stop position: the fixup assigns the
// ends 0 and 1 and distributes the middles evenly.
var unspecified = float32(math.NaN())

// Fixture is the n-stop + radial gradient fixture.
var Fixture = parity.Fixture{
	Name:   "gradientstops",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   page,
	View:   View,
	BoxIDs: []string{"linear", "radial"},
	Tolerance: parity.PixelTolerance{
		// Provisional: the correct view's statistics are the ramp
		// quantization (the 1024-texel row's 8-bit steps against
		// Chrome's own interpolation), no hot pixels. Verified by
		// gradientstops_test.go.
		MaxMeanChannelDiff:    0.2,
		MaxROIMeanChannelDiff: 3.0,
		MaxHotPixelFraction:   0.02,
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
		gap: 48px;
	}
	.linear {
		box-sizing: border-box;
		width: 180px;
		height: 120px;
		/* The three stops carry no positions: the ends sit at 0% and
		   100% and the middle distributes to the halfway point. */
		background: linear-gradient(to right, #ef4444, #f97316, #3b82f6);
	}
	.radial {
		box-sizing: border-box;
		width: 180px;
		height: 120px;
		/* The CSS default: the ellipse to the farthest corner,
		   centred. */
		background: radial-gradient(#dc2626, #2563eb);
	}
</style>
</head>
<body>
	<div class="linear" data-id="linear"></div>
	<div class="radial" data-id="radial"></div>
</body>
</html>
`

// View is the gpui-go twin of page: the three-stop linear gradient and
// the default radial.
func View(_ *parity.EventLog) gpui.AnyElement {
	return root(true)
}

// WrongTwoStopView is the deliberately-wrong view for the Tier C
// comparison: both gradients collapsed to two stops — the R15 record's
// shape (the via dropped, the radial drawn as a linear).
func WrongTwoStopView(_ *parity.EventLog) gpui.AnyElement {
	return root(false)
}

// root builds the fixture's root: the two boxes, centered.
func root(nStop bool) gpui.AnyElement {
	linear := gpui.LinearGradientToBackground(gpui.GradientDirectionToRight,
		gpui.GradientStop{Color: gpui.Rgb(linearFrom), Position: 0},
		gpui.GradientStop{Color: gpui.Rgb(linearTo), Position: 1})
	radial := gpui.LinearGradientToBackground(gpui.GradientDirectionToBottom,
		gpui.GradientStop{Color: gpui.Rgb(radialFrom), Position: 0},
		gpui.GradientStop{Color: gpui.Rgb(radialTo), Position: 1})
	if nStop {
		linear = gpui.LinearGradientToStopsBackground(gpui.GradientDirectionToRight,
			gpui.GradientStop{Color: gpui.Rgb(linearFrom), Position: unspecified},
			gpui.GradientStop{Color: gpui.Rgb(linearVia), Position: unspecified},
			gpui.GradientStop{Color: gpui.Rgb(linearTo), Position: unspecified})
		radial = gpui.RadialGradientBackground(gpui.RadialShapeEllipse, gpui.RadialExtentFarthestCorner,
			gpui.GradientStop{Color: gpui.Rgb(radialFrom), Position: unspecified},
			gpui.GradientStop{Color: gpui.Rgb(radialTo), Position: unspecified})
	}
	return gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Flex().
		Justify(gpui.AlignContentCenter).
		AlignItems(gpui.AlignItemsCenter).
		Gap(gpui.Px(48)).
		Bg(gpui.Rgb(pageBackground)).
		Child(gpui.Div().
			ID("linear").
			DebugSelector("linear").
			W(gpui.Px(boxWidth)).
			H(gpui.Px(boxHeight)).
			Bg(linear).IntoElement()).
		Child(gpui.Div().
			ID("radial").
			DebugSelector("radial").
			W(gpui.Px(boxWidth)).
			H(gpui.Px(boxHeight)).
			Bg(radial).IntoElement()).
		IntoElement()
}
