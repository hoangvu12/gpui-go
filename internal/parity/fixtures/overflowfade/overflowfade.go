//go:build windows

// Package overflowfade is ticket 57's edge-fade fixture: an
// overflow-clipped box whose content fades out over the last pixels of
// the bottom edge — the OverflowFade setter, which finally paints (the
// pin's ContentMask fade_out ramps riding the clipped records). The
// Chrome twin spells the same ramp as a mask-image gradient
// (linear-gradient(to bottom, black calc(100% - 12px), transparent)),
// which matches the pin's fade shape exactly: alpha 1 inward, the
// linear ramp over the final 12px, alpha 0 AT the edge.
//
// The page is text-free. The wrong view drops the fade — the child
// clipped hard — and must fail Tier C.
package overflowfade

import (
	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The numbers shared by the page and the view.
const (
	windowWidth  = 640.0
	windowHeight = 400.0

	boxWidth   = 140.0
	boxHeight  = 100.0
	childWidth = 140.0
	// The child is taller than the clip: the clip cuts it, the fade
	// softens the cut.
	childHeight = 180.0
	// The fade distance on the bottom edge (logical px).
	fadeBottom = 12.0
)

// The palette, shared by the page and the view.
const (
	pageBackground = 0xffffff
	childFill      = 0x16a34a
)

// Fixture is the edge-fade fixture.
var Fixture = parity.Fixture{
	Name:   "overflowfade",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   page,
	View:   View,
	BoxIDs: []string{"scroller", "child"},
	Tolerance: parity.PixelTolerance{
		// Provisional: the correct view's statistics are the ramp's
		// 8-bit quantization, no hot pixels. Verified by
		// overflowfade_test.go.
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
	.scroller {
		position: relative;
		box-sizing: border-box;
		width: 140px;
		height: 100px;
		overflow: hidden;
		/* The edge fade, spelled as the equivalent mask ramp: opaque
		   until 12px above the bottom, then linear to transparent AT
		   the edge — the pin's ContentMask fade_out shape. */
		mask-image: linear-gradient(to bottom, black calc(100% - 12px), transparent);
	}
	.child {
		width: 140px;
		height: 180px;
		background: #16a34a;
	}
</style>
</head>
<body>
	<div class="scroller" data-id="scroller">
		<div class="child" data-id="child"></div>
	</div>
</body>
</html>
`

// View is the gpui-go twin of page: the overflow-hidden scroller with
// a 12px bottom fade, the taller child clipped through it.
func View(_ *parity.EventLog) gpui.AnyElement {
	return root(true)
}

// WrongHardClipView is the deliberately-wrong view for the Tier C
// comparison: the fade dropped — the child clipped hard at the bottom
// edge (the pre-57 behaviour, the setter that painted nothing).
func WrongHardClipView(_ *parity.EventLog) gpui.AnyElement {
	return root(false)
}

// root builds the fixture's root: the clipped scroller with (or
// without) the bottom fade, centered.
func root(fade bool) gpui.AnyElement {
	scroller := gpui.Div().
		ID("scroller").
		DebugSelector("scroller").
		W(gpui.Px(boxWidth)).
		H(gpui.Px(boxHeight)).
		Relative().
		OverflowHidden()
	if fade {
		// Bottom-only (the page's mask): OverflowFade sets exactly the
		// edges given — the Y sugar would fade the top edge too.
		scroller = scroller.OverflowFade(gpui.AbsoluteLengthEdges{Bottom: gpui.Px(fadeBottom)})
	}
	scroller = scroller.Child(gpui.Div().
		ID("child").
		DebugSelector("child").
		W(gpui.Px(childWidth)).
		H(gpui.Px(childHeight)).
		Bg(gpui.Rgb(childFill)))
	return gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Flex().
		Justify(gpui.AlignContentCenter).
		AlignItems(gpui.AlignItemsCenter).
		Bg(gpui.Rgb(pageBackground)).
		Child(scroller.IntoElement()).IntoElement()
}
