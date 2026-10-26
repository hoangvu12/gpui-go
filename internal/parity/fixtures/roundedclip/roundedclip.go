//go:build windows

// Package roundedclip is ticket 55's rounded overflow-clip fixture: a
// rounded card with an image child, recorded in headless Chrome and
// replayed against the gpui-go view through the browser-parity
// harness. CSS clips an overflowing box's descendants to the INNER
// border rounded rect (the padding box with the corner radii):
// Chromium's UpdateInnerBorderRadiusClip reads
// ContouredBorderGeometry::ContouredInnerBorder - the border rrect
// inset by the border widths (core/paint/paint_property_tree_builder.
// cc:3257-3293, core/paint/contoured_border_geometry.cc:209-219). The
// card here is borderless, so the clip is the border box with the
// authored 24px radii, and the IMAGE (a replaced element filling the
// card) is cut at the corners - the rounded clip the R16 compositor
// provides through the rounded_clip_only effect group (the pinned
// sprite pipeline clips rect-only, the recorded native divergence 4,
// so the group composite is the clip).
//
// The image is the checked-in quadrant PNG beside this file, embedded
// into the page as a data URI (the recorded page.html is
// self-contained) and built on the gpui side with gpui.NewImage - the
// boxpaint object-fit fixture's pattern. The page is text-free.
package roundedclip

import (
	_ "embed"
	"encoding/base64"
	"fmt"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The numbers shared by the page and the view.
const (
	windowWidth  = 640.0
	windowHeight = 400.0

	// The card: 120x90 with a 36px radius, hidden overflow, filled by
	// the image (the source is 256x160; cover scales it 0.5625 and
	// crops the sides). The large radius against the small card makes
	// the rounded clip's corner arcs a large fraction of the card, so
	// the deliberately-wrong rectangular clip fails the tolerance
	// loudly.
	cardWidth  = 120.0
	cardHeight = 90.0
	cardRadius = 36.0
)

// The palette, shared by the page and the view.
const (
	pageBackground = 0xffffff
)

// imagePNG is the fixture's checked-in source image: a 256x160
// quadrant PNG (red, blue, green, yellow) with white diagonals, so
// every corner region reads a distinct colour - a corner that escapes
// the rounded clip cannot hide.
//
//go:embed image.png
var imagePNG []byte

// Fixture is the rounded-overflow-clip fixture.
var Fixture = parity.Fixture{
	Name:   "roundedclip",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	View:   View,
	BoxIDs: []string{"card"},
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run), set from
		// the measured correct-view statistics: 0.0415 mean image-wide
		// and 0.9843 in the card's ROI with a 0.0121 hot fraction - the
		// image resampling noise the boxpaint object-fit fixture
		// measures (the pinned renderer's atlas sampling vs Chrome's),
		// plus the rounded corner's anti-aliasing band. The
		// deliberately-wrong view (the rectangular clip, whose corner
		// arcs read the image instead of the page) measures far past the
		// ROI gate. Verified by roundedclip_test.go.
		MaxMeanChannelDiff:    0.3,
		MaxROIMeanChannelDiff: 2.5,
		MaxHotPixelFraction:   0.04,
	},
}

func init() {
	// The page carries the embedded PNG as a data URI so the recorded
	// page.html is self-contained (the harness's image support).
	Fixture.HTML = fmt.Sprintf(page, imageDataURL())
	parity.Register(Fixture)
}

// imageDataURL returns the embedded PNG as a data: URI.
func imageDataURL() string {
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(imagePNG)
}

// image builds the gpui image from the embedded bytes: the same PNG
// the page's <img> loads through its data URI.
func image() gpui.Image {
	return gpui.NewImage(gpui.ImageFormatPNG, imagePNG)
}

// page is the HTML twin of View; the %s receives the image's data URI.
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
	.card {
		position: relative;
		box-sizing: border-box;
		width: 120px;
		height: 90px;
		/* The rounded overflow clip: the descendants (the image) clip
		   to the inner border rounded rect - here the border box with
		   the 36px radii. */
		border-radius: 36px;
		overflow: hidden;
	}
	.card img {
		display: block;
		width: 120px;
		height: 90px;
		object-fit: cover;
	}
</style>
</head>
<body>
	<div class="card" data-id="card">
		<img src="%s" data-id="img">
	</div>
</body>
</html>
`

// View is the gpui-go twin of page: a rounded overflow-hidden card
// with a covering image child.
func View(_ *parity.EventLog) gpui.AnyElement {
	return card(true)
}

// WrongRectClipView is the deliberately-wrong view for the Tier C
// comparison: the card clips its overflow at the RECTANGLE (no corner
// radii on the clip) - exactly the pre-R16-styling rendering (the
// rect-only content mask; the image sprite's corners escape the
// rounded arc). The boxes are unchanged, so only Tier C fails.
func WrongRectClipView(_ *parity.EventLog) gpui.AnyElement {
	return card(false)
}

// card builds the fixture's root: the centered card with the image,
// rounded or rect clipped.
func card(rounded bool) gpui.AnyElement {
	card := gpui.Div().
		ID("card").
		DebugSelector("card").
		W(gpui.Px(cardWidth)).
		H(gpui.Px(cardHeight)).
		Relative().
		OverflowHidden()
	if rounded {
		card = card.Rounded(gpui.Px(cardRadius))
	}
	return gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Flex().
		Justify(gpui.AlignContentCenter).
		AlignItems(gpui.AlignItemsCenter).
		Bg(gpui.Rgb(pageBackground)).
		Child(card.
			Child(gpui.Img(image()).
				ObjectFit(gpui.ObjectFitCover).
				W(gpui.Px(cardWidth)).
				H(gpui.Px(cardHeight)))).
		IntoElement()
}
