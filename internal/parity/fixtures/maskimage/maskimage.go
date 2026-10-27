//go:build windows

// Package maskimage is ticket 57's mask-image fixture: a solid box
// masked by a linear gradient (the alpha ramp), recorded in headless
// Chrome and replayed against the gpui-go view through the
// browser-parity harness. The mask composites the element's whole
// rendered image dst_in against the gradient's alpha — CSS
// mask-image: linear-gradient(to bottom, black, transparent) (the
// default mask-mode for a gradient mask is alpha,
// css-masking-1 §the-mask-image); the port's R16 effect group carries
// the same shape (rev 16, ticket 52's mask composite).
//
// The page is text-free, so Tier C carries no font rasterization
// noise. The wrong view drops the mask — the fully opaque box — and
// must fail Tier C.
package maskimage

import (
	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The numbers shared by the page and the view.
const (
	windowWidth  = 640.0
	windowHeight = 400.0

	boxWidth  = 160.0
	boxHeight = 120.0
)

// The palette, shared by the page and the view.
const (
	pageBackground = 0xffffff
	boxFill        = 0x2563eb
)

// Fixture is the mask-image (gradient) fixture.
var Fixture = parity.Fixture{
	Name:   "maskimage",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   page,
	View:   View,
	BoxIDs: []string{"masked"},
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run): the
		// correct view's statistics are the 8-bit ramp quantization of
		// the two mask composites, no hot pixels. Verified by
		// maskimage_test.go.
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
	.masked {
		box-sizing: border-box;
		width: 160px;
		height: 120px;
		background: #2563eb;
		/* The gradient mask: the alpha ramp composites the whole box
		   (mask-mode is alpha for a gradient, the CSS default). */
		mask-image: linear-gradient(to bottom, black, transparent);
	}
</style>
</head>
<body>
	<div class="masked" data-id="masked"></div>
</body>
</html>
`

// View is the gpui-go twin of page: a solid blue box masked by the
// same gradient (the alpha 1 → 0 ramp from the top edge to the
// bottom).
func View(_ *parity.EventLog) gpui.AnyElement {
	return root(true)
}

// WrongUnmaskedView is the deliberately-wrong view for the Tier C
// comparison: the mask dropped — the fully opaque box. The boxes are
// unchanged, so only Tier C fails.
func WrongUnmaskedView(_ *parity.EventLog) gpui.AnyElement {
	return root(false)
}

// root builds the fixture's root: the masked box, centered.
func root(masked bool) gpui.AnyElement {
	box := gpui.Div().
		ID("masked").
		DebugSelector("masked").
		W(gpui.Px(boxWidth)).
		H(gpui.Px(boxHeight)).
		Bg(gpui.Rgb(boxFill))
	if masked {
		box = box.MaskImage(gpui.GradientMaskImage(gpui.CssGradient{
			Kind:      gpui.CssGradientLinear,
			Direction: gpui.GradientDirectionToBottom,
			Stops: []gpui.GradientStop{
				{Color: gpui.Rgb(0x000000)},
				{Color: gpui.Hsla{A: 0}, Position: 1},
			},
		}))
	}
	return gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Flex().
		Justify(gpui.AlignContentCenter).
		AlignItems(gpui.AlignItemsCenter).
		Bg(gpui.Rgb(pageBackground)).
		Child(box.IntoElement()).IntoElement()
}
