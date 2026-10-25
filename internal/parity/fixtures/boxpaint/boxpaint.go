//go:build windows

// Package boxpaint is ticket 05's fixture set for the three box
// painting rules (clip inset, radius scaling, object-fit), each
// recorded in headless Chrome and replayed against the gpui-go view
// through the browser-parity harness:
//
//   - boxpaint-clip: a bordered overflow-hidden box with a child at
//     the edge. CSS clips the element's content at the padding box
//     (the border box inset by the border widths, whatever the border
//     color): Chromium's LayoutBox::OverflowClipRect contracts
//     PhysicalBorderBoxRect by BorderOutsets with no color condition
//     (third_party/blink/renderer/core/layout/layout_box.cc:2288-2302
//     in the research checkout). The fixture pairs a transparent
//     5px-border box (the rule's observable case: a transparent border
//     still reserves and insets the clip) with a visible-border
//     control that already clipped at the padding edge under the pin.
//   - boxpaint-radii: a box whose corner radii overflow. CSS
//     Backgrounds 3 §corner-overlap scales ALL radii by one factor
//     f = min(Li/Si) over the four sides (Chromium
//     FloatRoundedRect::ConstrainRadii, float_rounded_rect.cc:289-305),
//     where the pin clamps each corner independently to half the
//     shortest side. The uneven box keeps its 30px top-left radius in
//     CSS where the pin clamps to 20px; the factor box (60/20 top
//     radii on a 40px-tall box) scales both together to 40/13.33.
//   - boxpaint-objectfit: an image with object-fit none/contain/cover
//     inside padding. CSS fits replaced content in the content box
//     (border box minus border widths minus padding:
//     LayoutReplaced::ReplacedContentRect from PhysicalContentBoxRect,
//     layout_replaced.cc:474-477, layout_box.cc:3848-3856) and centres
//     object-fit: none at the default object-position 50% 50%
//     (layout_replaced.cc:453-471).
//
// The three pages are text-free, so Tier C carries no font
// rasterization noise. The image fixture embeds its PNG (image.png,
// checked in beside this file) and injects it into the HTML page as a
// data URI, so the recorded page.html is self-contained; the gpui side
// builds the same image from the embedded bytes with gpui.NewImage.
package boxpaint

import (
	_ "embed"
	"encoding/base64"
	"fmt"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The numbers shared by every page and its gpui view (the tracer's
// discipline: both sides are built from one set of constants).
const (
	windowWidth  = 640.0
	windowHeight = 400.0

	// The clip fixture: two 160x120 boxes, a 5px transparent border on
	// one and a 5px visible border on the other, each with a red child
	// as large as the whole box at the content origin, so the child
	// runs into the clip on every side.
	clipBoxWidth  = 160.0
	clipBoxHeight = 120.0
	clipBorder    = 5.0
	clipGap       = 48.0

	// The radii fixture: a 100x40 box with a single 30px top-left
	// radius (no CSS overflow; the pin clamps it to 20px), and an
	// 80x40 box whose left side (42 + 21) overflows the 40px height (CSS
	// scales every radius by 40/63 to 26.67/13.33/0/13.33, where the
	// pair meets exactly on the left side; the pin clamps to
	// 20/20/0/20).
	unevenWidth  = 100.0
	unevenHeight = 40.0
	unevenRadius = 30.0
	factorWidth  = 80.0
	factorHeight = 40.0
	factorTL     = 42.0
	factorTR     = 21.0
	factorBL     = 21.0
	radiiGap     = 32.0

	// The object-fit fixture: three 128x108 images with a 4px border
	// and 12px padding (a 96x76 content box) around a 60x40 image.
	frameWidth   = 128.0
	frameHeight  = 108.0
	frameBorder  = 4.0
	framePadding = 12.0
	frameGap     = 32.0
)

// The palette, shared by the pages and the views.
const (
	pageBackground = 0xffffff
	childRed       = 0xdc2626
	borderDark     = 0x334155
	unevenBlue     = 0x2563eb
	factorGreen    = 0x16a34a
	frameGray      = 0xe2e8f0
)

// imagePNG is the object-fit fixture's checked-in source image: a
// 60x40 quadrant PNG (red, blue, yellow, green), small enough to fit
// every object-fit mode inside the content box (no ink overflow) and
// asymmetric enough that a wrong position, scale or crop moves clear
// pixels.
//
//go:embed image.png
var imagePNG []byte

// ClipFixture is the overflow-clip fixture: the border widths always
// inset the overflow clip (CSS clips at the padding box).
var ClipFixture = parity.Fixture{
	Name:   "boxpaint-clip",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   clipPage,
	View:   ClipView,
	BoxIDs: []string{"clip", "clip-visible", "child", "child-visible"},
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run), set from
		// the measured correct-view statistics: 0.0000 mean everywhere
		// image-wide and per box (the fixture is text-free geometry, so
		// the replay is pixel-exact), and the deliberately-wrong views
		// measure 0.7488 image-wide / 4.8997 in the clip ROI / 0.0106 hot
		// (the unclipped view). Verified by boxpaint_test.go. See
		// docs/browser-parity-harness.md.
		MaxMeanChannelDiff:    0.02,
		MaxROIMeanChannelDiff: 0.1,
		MaxHotPixelFraction:   0.002,
	},
}

// RadiiFixture is the corner-radius fixture: overflowing radii scale by
// one common factor.
var RadiiFixture = parity.Fixture{
	Name:   "boxpaint-radii",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   radiiPage,
	View:   RadiiView,
	BoxIDs: []string{"radii-uneven", "radii-factor"},
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run), set from
		// the measured correct-view statistics: 0.0047 mean image-wide,
		// 0.2076 in the factor box's ROI and 0.1303 in the uneven box's
		// (the pinned shader confines each corner's arc to its quadrant,
		// so a radius past half the side renders a small band — see the
		// ticket), and the per-corner-clamp wrong view measures 0.0736 /
		// 4.1553 / 1.3830. Verified by boxpaint_test.go.
		MaxMeanChannelDiff:    0.02,
		MaxROIMeanChannelDiff: 0.5,
		MaxHotPixelFraction:   0.004,
	},
}

// ObjectFitFixture is the object-fit fixture: none is centred, and
// every mode fits in the content box inside the border and padding.
var ObjectFitFixture = parity.Fixture{
	Name:   "boxpaint-objectfit",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	View:   ObjectFitView,
	BoxIDs: []string{"img-none", "img-contain", "img-cover"},
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run), set from
		// the measured correct-view statistics: 0.1084 mean image-wide /
		// 0.0019 hot, and per image 0.2659 (none) / 0.6213 (contain) /
		// 1.1199 (cover) with hot fractions up to 0.0178 — the pinned
		// renderer's image resampling (atlas edge-texel blends and the
		// linear sampler vs Chrome's upsampling), not geometry: the
		// image rectangles match to a pixel. The fill wrong view measures
		// 0.8972 image-wide / 12.4812 in the none ROI / 0.1763 hot.
		// Verified by boxpaint_test.go.
		MaxMeanChannelDiff:    0.25,
		MaxROIMeanChannelDiff: 2.5,
		MaxHotPixelFraction:   0.04,
	},
}

func init() {
	// The object-fit page carries the embedded PNG as a data URI so the
	// recorded page.html is self-contained; the recorder writes the
	// page from this string (the harness's image support: no files
	// beside the page are needed).
	ObjectFitFixture.HTML = fmt.Sprintf(objectFitPage, imageDataURL(), imageDataURL(), imageDataURL())
	parity.Register(ClipFixture)
	parity.Register(RadiiFixture)
	parity.Register(ObjectFitFixture)
}

// imageDataURL returns the embedded PNG as a data: URI.
func imageDataURL() string {
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(imagePNG)
}

// objectImage builds the gpui image from the embedded bytes: the same
// PNG the page's <img> loads through its data URI.
func objectImage() gpui.Image {
	return gpui.NewImage(gpui.ImageFormatPNG, imagePNG)
}

// ---------------------------------------------------------------------------
// The clip fixture
// ---------------------------------------------------------------------------

// clipPage is the HTML twin of ClipView.
const clipPage = `<!doctype html>
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
	.clip {
		box-sizing: border-box;
		width: 160px;
		height: 120px;
		/* The wrapper is a flex row (gpui-go's pin default display), and
		   the child does not shrink, so it overflows the clip on the
		   right and bottom exactly like the gpui view. */
		display: flex;
		/* A transparent border still reserves its space and still insets
		   the overflow clip (CSS clips at the padding box). */
		border: 5px solid transparent;
		overflow: hidden;
		background: #ffffff;
	}
	.clip-visible {
		box-sizing: border-box;
		width: 160px;
		height: 120px;
		display: flex;
		border: 5px solid #334155;
		overflow: hidden;
		background: #ffffff;
	}
	.child {
		width: 160px;
		height: 120px;
		flex-shrink: 0;
		background: #dc2626;
	}
</style>
</head>
<body>
	<div class="clip" data-id="clip"><div class="child" data-id="child"></div></div>
	<div class="clip-visible" data-id="clip-visible"><div class="child" data-id="child-visible"></div></div>
</body>
</html>
`

// ClipView is the gpui-go twin of clipPage: two overflow-hidden boxes
// whose clip insets by the 5px border widths, one with a transparent
// border (the observable case) and one with a visible border (the
// control), each with a red child as large as the whole box at the
// content origin.
func ClipView(_ *parity.EventLog) gpui.AnyElement {
	return clipRoot(true)
}

// ClipUnclipped is the deliberately-wrong view for the Tier C
// comparison: the transparent-border box no longer hides its overflow,
// so its child paints past the padding box (and past the box) where
// Chrome's record is clipped. The boxes are unchanged, so only Tier C
// fails.
func ClipUnclipped(_ *parity.EventLog) gpui.AnyElement {
	return clipRoot(false)
}

// clipRoot builds the clip fixture's root: the transparent-border box
// (clipped in the correct view, unclipped in the wrong view) beside
// the visible-border control.
func clipRoot(clipped bool) gpui.AnyElement {
	return gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Flex().
		Justify(gpui.AlignContentCenter).
		AlignItems(gpui.AlignItemsCenter).
		Gap(gpui.Px(clipGap)).
		Bg(gpui.Rgb(pageBackground)).
		Child(clipBox("clip", "child", clipped, false)).
		Child(clipBox("clip-visible", "child-visible", true, true)).
		IntoElement()
}

// clipBox builds one clip fixture box: a 160x120 flex box (the pin's
// default display, pinned on the page too) with a 5px border
// (transparent unless visibleBorder) hiding its overflow when clipped,
// and a red child as large as the whole box at the content origin, not
// shrinking, so it overflows the clip on the right and bottom.
func clipBox(boxID, childID string, clipped, visibleBorder bool) *gpui.DivElement {
	box := gpui.Div().Flex().
		ID(boxID).
		DebugSelector(boxID).
		W(gpui.Px(clipBoxWidth)).
		H(gpui.Px(clipBoxHeight)).
		Border(gpui.Px(clipBorder)).
		Bg(gpui.Rgb(pageBackground))
	if clipped {
		box = box.OverflowHidden()
	}
	if visibleBorder {
		box = box.BorderColor(gpui.Rgb(borderDark))
	}
	child := gpui.Div().
		ID(childID).
		DebugSelector(childID).
		W(gpui.Px(clipBoxWidth)).
		H(gpui.Px(clipBoxHeight)).
		FlexShrink(0).
		Bg(gpui.Rgb(childRed))
	return box.Child(child)
}

// ---------------------------------------------------------------------------
// The radii fixture
// ---------------------------------------------------------------------------

// radiiPage is the HTML twin of RadiiView.
const radiiPage = `<!doctype html>
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
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 32px;
	}
	.radii-uneven {
		width: 100px;
		height: 40px;
		/* 30px on the top left only: the sides do not overlap, so CSS
		   keeps 30px where the per-corner clamp gives 20px. */
		border-radius: 30px 0 0 0;
		background: #2563eb;
	}
	.radii-factor {
		width: 80px;
		height: 40px;
		/* 42px + 21px on the left side (top-left + bottom-left) overflows
		   the 40px height, so CSS scales every radius by 40/63: the
		   top-left becomes 26.67px, the top-right and bottom-left 13.33px
		   — and the left-side pair then meets exactly, with no straight
		   segment left on that side. The per-corner clamp gives
		   20/20/0/20 instead. */
		border-radius: 42px 21px 0 21px;
		background: #16a34a;
	}
</style>
</head>
<body>
	<div class="radii-uneven" data-id="radii-uneven"></div>
	<div class="radii-factor" data-id="radii-factor"></div>
</body>
</html>
`

// RadiiView is the gpui-go twin of radiiPage.
func RadiiView(_ *parity.EventLog) gpui.AnyElement {
	return radiiRoot(
		radiiBox("radii-uneven", unevenWidth, unevenHeight, gpui.Rgb(unevenBlue), gpui.Corners{
			TopLeft: unevenRadius, TopRight: 0, BottomRight: 0, BottomLeft: 0,
		}),
		radiiBox("radii-factor", factorWidth, factorHeight, gpui.Rgb(factorGreen), gpui.Corners{
			TopLeft: factorTL, TopRight: factorTR, BottomRight: 0, BottomLeft: factorBL,
		}),
	)
}

// RadiiPerCornerClamp is the deliberately-wrong view for the Tier C
// comparison: every radius clamped per corner to half the shortest
// side — the pin's rule (geometry.rs:2511-2523), which the CSS factor
// rule replaces. The uneven box's 30px radius becomes 20px, and the
// factor box's 42/21/0/21 radii become 20/20/0/20 (CSS scales them
// together to 26.67/13.33/0/13.33). The boxes are unchanged, so only
// Tier C fails.
func RadiiPerCornerClamp(_ *parity.EventLog) gpui.AnyElement {
	return radiiRoot(
		radiiBox("radii-uneven", unevenWidth, unevenHeight, gpui.Rgb(unevenBlue), gpui.Corners{
			TopLeft: unevenHeight / 2, TopRight: 0, BottomRight: 0, BottomLeft: 0,
		}),
		radiiBox("radii-factor", factorWidth, factorHeight, gpui.Rgb(factorGreen), gpui.Corners{
			TopLeft: factorHeight / 2, TopRight: factorHeight / 2, BottomRight: 0, BottomLeft: factorHeight / 2,
		}),
	)
}

// radiiRoot builds the radii fixture's root: the uneven box over the
// factor box, centered.
func radiiRoot(uneven, factor *gpui.DivElement) gpui.AnyElement {
	return gpui.Div().Flex().
		ID("root").
		DebugSelector("root").
		SizeFull().
		FlexCol().
		Justify(gpui.AlignContentCenter).
		AlignItems(gpui.AlignItemsCenter).
		Gap(gpui.Px(radiiGap)).
		Bg(gpui.Rgb(pageBackground)).
		Child(uneven).
		Child(factor).
		IntoElement()
}

// radiiBox builds one plain rounded box of the radii fixture: the
// width, height, color and radii come from the view that builds it.
func radiiBox(id string, width, height float32, background gpui.Hsla, radii gpui.Corners) *gpui.DivElement {
	box := gpui.Div().
		ID(id).
		DebugSelector(id).
		W(gpui.Px(width)).
		H(gpui.Px(height)).
		Bg(background)
	if radii.TopLeft != 0 {
		box = box.RoundedTl(gpui.Px(radii.TopLeft))
	}
	if radii.TopRight != 0 {
		box = box.RoundedTr(gpui.Px(radii.TopRight))
	}
	if radii.BottomRight != 0 {
		box = box.RoundedBr(gpui.Px(radii.BottomRight))
	}
	if radii.BottomLeft != 0 {
		box = box.RoundedBl(gpui.Px(radii.BottomLeft))
	}
	return box
}

// ---------------------------------------------------------------------------
// The object-fit fixture
// ---------------------------------------------------------------------------

// objectFitPage is the HTML twin of ObjectFitView; the three %s
// receive the image's data URI.
const objectFitPage = `<!doctype html>
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
		gap: 32px;
	}
	.frame {
		width: 128px;
		height: 108px;
	}
	.frame img {
		display: block;
		box-sizing: border-box;
		width: 128px;
		height: 108px;
		border: 4px solid #334155;
		padding: 12px;
		background: #e2e8f0;
	}
</style>
</head>
<body>
	<div class="frame" data-id="img-none">
		<img src="%s" style="object-fit: none">
	</div>
	<div class="frame" data-id="img-contain">
		<img src="%s" style="object-fit: contain">
	</div>
	<div class="frame" data-id="img-cover">
		<img src="%s" style="object-fit: cover">
	</div>
</body>
</html>
`

// ObjectFitView is the gpui-go twin of objectFitPage: three images
// with a 4px border and 12px padding (a 96x76 content box) fitted by
// object-fit none, contain and cover.
func ObjectFitView(_ *parity.EventLog) gpui.AnyElement {
	return objectFitRoot(func(image gpui.Image) []gpui.ObjectFit {
		return []gpui.ObjectFit{gpui.ObjectFitNone, gpui.ObjectFitContain, gpui.ObjectFitCover}
	})
}

// ObjectFitFill is the deliberately-wrong view for the Tier C
// comparison: every image stretched to fill its box, where none is
// centred at its natural size and contain/cover letterbox and crop.
// The frames are unchanged, so only Tier C fails.
func ObjectFitFill(_ *parity.EventLog) gpui.AnyElement {
	return objectFitRoot(func(image gpui.Image) []gpui.ObjectFit {
		return []gpui.ObjectFit{gpui.ObjectFitFill, gpui.ObjectFitFill, gpui.ObjectFitFill}
	})
}

// objectFitRoot builds the object-fit fixture's root: the three
// framed images with the fits chosen by fits, centered.
func objectFitRoot(fits func(image gpui.Image) []gpui.ObjectFit) gpui.AnyElement {
	ids := []string{"img-none", "img-contain", "img-cover"}
	image := objectImage()
	root := gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Flex().
		Justify(gpui.AlignContentCenter).
		AlignItems(gpui.AlignItemsCenter).
		Gap(gpui.Px(frameGap)).
		Bg(gpui.Rgb(pageBackground))
	for i, fit := range fits(image) {
		root = root.Child(gpui.Div().
			ID(ids[i]).
			DebugSelector(ids[i]).
			W(gpui.Px(frameWidth)).
			H(gpui.Px(frameHeight)).
			Child(fittedImage(image, fit)))
	}
	return root.IntoElement()
}

// fittedImage builds one object-fit image: the intrinsic 60x40 source
// inside a 128x108 box with a 4px border and 12px padding.
func fittedImage(image gpui.Image, fit gpui.ObjectFit) *gpui.ImgElement {
	return gpui.Img(image).
		ObjectFit(fit).
		W(gpui.Px(frameWidth)).
		H(gpui.Px(frameHeight)).
		Border(gpui.Px(frameBorder)).
		BorderColor(gpui.Rgb(borderDark)).
		P(gpui.Px(framePadding)).
		Bg(gpui.Rgb(frameGray))
}
