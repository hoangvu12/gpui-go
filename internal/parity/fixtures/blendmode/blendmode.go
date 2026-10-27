//go:build windows

// Package blendmode is ticket 57's mix-blend-mode fixture: two pairs of
// overlapping boxes, the top one compositing with multiply and with
// plus-lighter, recorded in headless Chrome and replayed against the
// gpui-go view through the browser-parity harness. The blend modes are
// the W3C compositing-1 formulas the R16 blend group applies against a
// backdrop copy (rev 16, ticket 52): multiply is B(Cb, Cs) = Cb·Cs,
// plus-lighter the premultiplied clamp(source + backdrop).
//
// The page is text-free. The wrong view drops the blend modes — the
// top boxes composite normally — and must fail Tier C.
package blendmode

import (
	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The numbers shared by the page and the view.
const (
	windowWidth  = 640.0
	windowHeight = 400.0

	backdropWidth  = 120.0
	backdropHeight = 90.0
	blendWidth     = 80.0
	blendHeight    = 90.0
	// The blend box overlaps the backdrop by 40px (the visible blend
	// band); its other half sits on the page background.
	overlap = 40.0
)

// The palette, shared by the page and the view.
const (
	pageBackground = 0xffffff

	// multiply: a gray blend box over a red backdrop.
	multiplyBackdrop = 0xdc2626
	multiplyFill     = 0x808080

	// plus-lighter: a blue blend box over a dark slate backdrop (a
	// light page background would clamp to itself and hide the band).
	plusBackdrop = 0x334155
	plusFill     = 0x2563eb
)

// Fixture is the mix-blend-mode fixture.
var Fixture = parity.Fixture{
	Name:   "blendmode",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   page,
	View:   View,
	BoxIDs: []string{"multiply-backdrop", "multiply-blend", "plus-backdrop", "plus-blend"},
	Tolerance: parity.PixelTolerance{
		// Provisional: the correct view's statistics are the 8-bit
		// rounding of the two blend composites. Verified by
		// blendmode_test.go.
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
		gap: 48px;
	}
	.pair {
		position: relative;
		box-sizing: border-box;
		width: 120px;
		height: 90px;
	}
	.backdrop {
		width: 120px;
		height: 90px;
	}
	.blend {
		position: absolute;
		top: 0;
		left: 40px;
		width: 80px;
		height: 90px;
	}
	.multiply-backdrop {
		background: #dc2626;
	}
	.multiply-blend {
		background: #808080;
		mix-blend-mode: multiply;
	}
	.plus-backdrop {
		background: #334155;
	}
	.plus-blend {
		background: #2563eb;
		mix-blend-mode: plus-lighter;
	}
</style>
</head>
<body>
	<div class="pair">
		<div class="backdrop multiply-backdrop" data-id="multiply-backdrop"></div>
		<div class="blend multiply-blend" data-id="multiply-blend"></div>
	</div>
	<div class="pair">
		<div class="backdrop plus-backdrop" data-id="plus-backdrop"></div>
		<div class="blend plus-blend" data-id="plus-blend"></div>
	</div>
</body>
</html>
`

// View is the gpui-go twin of page: the two blend pairs.
func View(_ *parity.EventLog) gpui.AnyElement {
	return root(true)
}

// WrongNormalView is the deliberately-wrong view for the Tier C
// comparison: the blend modes dropped — the boxes composite normally.
func WrongNormalView(_ *parity.EventLog) gpui.AnyElement {
	return root(false)
}

// root builds the fixture's root: the two pairs (with or without the
// blend modes), centered with the page's gap.
func root(blend bool) gpui.AnyElement {
	pair := func(backdropID, blendID string, backdrop, fill uint32, mode gpui.BlendMode) gpui.AnyElement {
		blendBox := gpui.Div().
			ID(blendID).
			DebugSelector(blendID).
			Absolute().
			Left(gpui.Px(overlap)).
			Top(gpui.Px(0)).
			W(gpui.Px(blendWidth)).
			H(gpui.Px(blendHeight)).
			Bg(gpui.Rgb(fill))
		if blend {
			blendBox = blendBox.MixBlendMode(mode)
		}
		return gpui.Div().
			Relative().
			W(gpui.Px(backdropWidth)).
			H(gpui.Px(backdropHeight)).
			Child(gpui.Div().
				ID(backdropID).
				DebugSelector(backdropID).
				W(gpui.Px(backdropWidth)).
				H(gpui.Px(backdropHeight)).
				Bg(gpui.Rgb(backdrop))).
			Child(blendBox.IntoElement()).
			IntoElement()
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
		Child(pair("multiply-backdrop", "multiply-blend", multiplyBackdrop, multiplyFill, gpui.BlendMultiply)).
		Child(pair("plus-backdrop", "plus-blend", plusBackdrop, plusFill, gpui.BlendPlusLighter)).
		IntoElement()
}
