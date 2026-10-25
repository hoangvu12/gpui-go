//go:build windows

// Package filters is ticket 41's fixture set: the CSS colour filters,
// their chains, object-position and image-rendering: pixelated, each
// recorded in headless Chrome and replayed against the gpui-go view
// through the browser-parity harness. The pages are text-free, so Tier
// C carries no font rasterization noise.
//
//   - filters-colors: one swatch per CSS colour filter function
//     (grayscale, sepia, invert, brightness, contrast, saturate,
//     hue-rotate) around an unfiltered control. Each swatch is five
//     saturated stripes (red, green, blue, white, gray) so every colour
//     channel is exercised and each function's matrix moves clear
//     pixels. The filter values are the full amounts (grayscale(1),
//     sepia(1), invert(1)) and typical multipliers
//     (brightness(1.5), contrast(1.5), saturate(2), hue-rotate(90deg)).
//   - filters-chain: a two-function chain whose order matters —
//     sepia(1) hue-rotate(90deg) beside hue-rotate(90deg) sepia(1)
//     (plus a control): matrix composition is order-sensitive, so the
//     pair proves the chain composes in CSS order (Chromium's
//     FilterEffectBuilder chains each effect's input to the previous
//     one, filter_effect_builder.cc:326-373).
//   - filters-image: the same filters on img elements (grayscale(1),
//     hue-rotate(90deg) around a control) — the sprite path through the
//     filter group, not the quad path.
//   - filters-objectposition: object-position with each object-fit
//     mode: keywords (left top, right bottom), percentages (25% 75%)
//     and lengths (12px 8px) under contain and none, plus cover (which
//     pans the crop through the negative free space), scale-down and
//     fill (whose percentage positions are no-ops and whose lengths
//     still shift — Chromium's ComputeObjectFitAndPositionRect,
//     layout_replaced.cc:410-477). The cover row sets its fit through
//     the STYLE (the object-cover class's path); the other rows use the
//     pinned img element option, so both storages are verified.
//   - filters-pixelated: the same 8x8 pixel-art image upscaled 12.5x
//     (100x100, a non-integer scale so bilinear sampling blends) with
//     image-rendering: pixelated beside the default smooth rendering:
//     hard edges against blends.
package filters

import (
	_ "embed"
	"encoding/base64"
	"fmt"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// rev 15.1 (browser-core ticket 77) note: the three colour-filter
// fixtures' Tier C comparisons are ACTIVE. The rev-15 blocker —
// dx_blur_pass wrote the composite-extensions cbuffer through
// `&[*uniforms]` whose element type was the reference (8 bytes, not
// the 112-byte struct), so COLOR_MATRIX_LOCALS read zeros and every
// colour function was a GPU no-op — was fixed in the rev-15.1 native
// rebuild (`&[**uniforms]`, reference/native/src/renderer/draw.rs, the
// ticket-41 diagnosis); the same fix un-breaks the backdrop-crop
// uniform (ticket 04's shadow-backdrop). The tolerances were measured
// against the rev-15.1 DLL (13cfb285…).

// The numbers shared by every page and its gpui view (the harness's
// discipline: both sides are built from one set of constants).
const (
	windowWidth  = 640.0
	windowHeight = 400.0

	// The swatch cells: 120x48 divs of five 24x48 stripes.
	swatchWidth  = 120.0
	swatchHeight = 48.0
	stripeWidth  = 24.0
	swatchGap    = 16.0
	rowGap       = 16.0

	// The image cells: 60x40 wrappers around the 60x40 quadrant image.
	imageCellGap = 24.0

	// The object-position cells: the boxpaint-objectfit frame shape —
	// 128x108 img elements with a 4px border and 12px padding (a 96x76
	// content box) around the 60x40 quadrant image.
	frameWidth   = 128.0
	frameHeight  = 108.0
	frameBorder  = 4.0
	framePadding = 12.0
	frameGap     = 24.0

	// The pixelated cells: the 8x8 pixel art upscaled to 100x100 (12.5x,
	// non-integer, so bilinear blends and point sampling does not).
	pixelCell = 100.0
	pixelGap  = 48.0
)

// The palette, shared by the pages and the views.
const (
	pageBackground = 0xffffff
	stripeRed      = 0xff0000
	stripeGreen    = 0x00ff00
	stripeBlue     = 0x0000ff
	stripeWhite    = 0xffffff
	stripeGray     = 0x808080
	frameBorderCol = 0x334155
	frameGray      = 0xe2e8f0
)

// The filter values the colour fixture applies, shared by the pages
// and the views (fractions and degrees, the CSS amounts).
const (
	amountFull       = 1.0
	amountBrightness = 1.5
	amountContrast   = 1.5
	amountSaturate   = 2.0
	hueDegrees       = 90.0
)

// quadrantPNG is the 60x40 quadrant image (red, green, blue, yellow),
// the same shape as the boxpaint-objectfit fixture's: small enough to
// fit every object-fit mode inside the content box and asymmetric
// enough that a wrong position, scale or crop moves clear pixels.
//
//go:embed quadrant.png
var quadrantPNG []byte

// pixelArtPNG is the 8x8 pixel-art image: four saturated colors in a
// diagonal pattern, upscaled non-integrally (12.5x) by the pixelated
// fixture so bilinear sampling blends between texels where point
// sampling keeps hard edges.
//
//go:embed pixelart.png
var pixelArtPNG []byte

// quadrantImage builds the gpui image from the embedded bytes: the
// same PNG the pages' <img> loads through its data URI.
func quadrantImage() gpui.Image {
	return gpui.NewImage(gpui.ImageFormatPNG, quadrantPNG)
}

// pixelArtImage builds the gpui pixel-art image from the embedded
// bytes.
func pixelArtImage() gpui.Image {
	return gpui.NewImage(gpui.ImageFormatPNG, pixelArtPNG)
}

// imageDataURL returns the embedded quadrant PNG as a data: URI.
func quadrantDataURL() string {
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(quadrantPNG)
}

// ---------------------------------------------------------------------------
// filters-colors
// ---------------------------------------------------------------------------

// ColorsFixture is the colour filter fixture: one swatch per CSS filter
// function around an unfiltered control.
var ColorsFixture = parity.Fixture{
	Name:   "filters-colors",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   colorsPage,
	View:   ColorsView,
	BoxIDs: []string{
		"control", "grayscale", "sepia", "invert",
		"brightness", "contrast", "saturate", "hue-rotate",
	},
	Tolerance: parity.PixelTolerance{
		// Measured against the rev-15.1 DLL (13cfb285… then
		// ef337ad3…, the per-function matrix payload): the whole
		// image and every swatch ROI measure 0.0000 / 0.00000 —
		// pixel-exact vs Chrome (the swatches are flat colored boxes:
		// the filters are pure per-pixel colour maps over exact
		// colors). The gates stay near zero so any matrix or upload
		// regression fails immediately; the wrong views measure whole
		// means in the tens (filters_test.go).
		MaxMeanChannelDiff:    0.02,
		MaxROIMeanChannelDiff: 0.1,
		MaxHotPixelFraction:   0.002,
	},
}

// colorsPage is the HTML twin of ColorsView.
const colorsPage = `<!doctype html>
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
		gap: 16px;
	}
	.row {
		display: flex;
		gap: 16px;
	}
	.swatch {
		width: 120px;
		height: 48px;
		display: flex;
	}
	.swatch > div {
		width: 24px;
		height: 48px;
		flex-shrink: 0;
	}
	.red { background: #ff0000; }
	.green { background: #00ff00; }
	.blue { background: #0000ff; }
	.white { background: #ffffff; }
	.gray { background: #808080; }
	.f-grayscale { filter: grayscale(1); }
	.f-sepia { filter: sepia(1); }
	.f-invert { filter: invert(1); }
	.f-brightness { filter: brightness(1.5); }
	.f-contrast { filter: contrast(1.5); }
	.f-saturate { filter: saturate(2); }
	.f-hue-rotate { filter: hue-rotate(90deg); }
</style>
</head>
<body>
	<div class="row">
		<div class="swatch" data-id="control"><div class="red"></div><div class="green"></div><div class="blue"></div><div class="white"></div><div class="gray"></div></div>
		<div class="swatch f-grayscale" data-id="grayscale"><div class="red"></div><div class="green"></div><div class="blue"></div><div class="white"></div><div class="gray"></div></div>
		<div class="swatch f-sepia" data-id="sepia"><div class="red"></div><div class="green"></div><div class="blue"></div><div class="white"></div><div class="gray"></div></div>
		<div class="swatch f-invert" data-id="invert"><div class="red"></div><div class="green"></div><div class="blue"></div><div class="white"></div><div class="gray"></div></div>
	</div>
	<div class="row">
		<div class="swatch f-brightness" data-id="brightness"><div class="red"></div><div class="green"></div><div class="blue"></div><div class="white"></div><div class="gray"></div></div>
		<div class="swatch f-contrast" data-id="contrast"><div class="red"></div><div class="green"></div><div class="blue"></div><div class="white"></div><div class="gray"></div></div>
		<div class="swatch f-saturate" data-id="saturate"><div class="red"></div><div class="green"></div><div class="blue"></div><div class="white"></div><div class="gray"></div></div>
		<div class="swatch f-hue-rotate" data-id="hue-rotate"><div class="red"></div><div class="green"></div><div class="blue"></div><div class="white"></div><div class="gray"></div></div>
	</div>
</body>
</html>
`

// ColorsView is the gpui-go twin of colorsPage: eight swatches, each
// five saturated stripes, one per CSS filter function around a control.
func ColorsView(_ *parity.EventLog) gpui.AnyElement {
	return colorsRoot(func(id string) []gpui.Filter {
		switch id {
		case "grayscale":
			return []gpui.Filter{gpui.Grayscale(amountFull)}
		case "sepia":
			return []gpui.Filter{gpui.Sepia(amountFull)}
		case "invert":
			return []gpui.Filter{gpui.Invert(amountFull)}
		case "brightness":
			return []gpui.Filter{gpui.Brightness(amountBrightness)}
		case "contrast":
			return []gpui.Filter{gpui.Contrast(amountContrast)}
		case "saturate":
			return []gpui.Filter{gpui.Saturate(amountSaturate)}
		case "hue-rotate":
			return []gpui.Filter{gpui.HueRotate(hueDegrees)}
		default:
			return nil
		}
	})
}

// ColorsUnfiltered is the deliberately-wrong view for the Tier C
// comparison: no swatch carries its filter (every function a no-op),
// where the record shows each function's colours. The swatches are
// unchanged, so only Tier C fails.
func ColorsUnfiltered(_ *parity.EventLog) gpui.AnyElement {
	return colorsRoot(func(string) []gpui.Filter { return nil })
}

// colorsRoot builds the colour fixture's root: two rows of four
// swatches, each filtered by the filters the chooser returns.
func colorsRoot(chooser func(id string) []gpui.Filter) gpui.AnyElement {
	root := gpui.Div().Flex().
		ID("root").
		DebugSelector("root").
		SizeFull().
		FlexCol().
		Justify(gpui.AlignContentCenter).
		AlignItems(gpui.AlignItemsCenter).
		Gap(gpui.Px(rowGap)).
		Bg(gpui.Rgb(pageBackground))
	for _, row := range [][]string{
		{"control", "grayscale", "sepia", "invert"},
		{"brightness", "contrast", "saturate", "hue-rotate"},
	} {
		rowDiv := gpui.Div().Flex().Gap(gpui.Px(swatchGap))
		for _, id := range row {
			rowDiv = rowDiv.Child(filteredSwatch(id, chooser(id)))
		}
		root = root.Child(rowDiv)
	}
	return root.IntoElement()
}

// filteredSwatch builds one 120x48 swatch of five 24x48 stripes under a
// filter chain.
func filteredSwatch(id string, filters []gpui.Filter) *gpui.DivElement {
	sw := gpui.Div().
		ID(id).
		DebugSelector(id).
		W(gpui.Px(swatchWidth)).
		H(gpui.Px(swatchHeight)).
		Flex()
	if filters != nil {
		sw = sw.Filter(filters)
	}
	colors := []uint32{stripeRed, stripeGreen, stripeBlue, stripeWhite, stripeGray}
	for _, color := range colors {
		sw = sw.Child(gpui.Div().Flex().
			W(gpui.Px(stripeWidth)).
			H(gpui.Px(swatchHeight)).
			FlexShrink(0).
			Bg(gpui.Rgb(color)))
	}
	return sw
}

// ---------------------------------------------------------------------------
// filters-chain
// ---------------------------------------------------------------------------

// ChainFixture is the two-function chain fixture: sepia(1)
// hue-rotate(90deg) beside hue-rotate(90deg) sepia(1) (order matters
// for matrix composition), around a control.
var ChainFixture = parity.Fixture{
	Name:   "filters-chain",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   chainPage,
	View:   ChainView,
	BoxIDs: []string{"chain-control", "sepia-hue", "hue-sepia"},
	Tolerance: parity.PixelTolerance{
		// Measured against the rev-15.1 DLL: whole 0.0000 / 0.00000,
		// every ROI 0.0000 / 0.00000 — pixel-exact vs Chrome. This is
		// the fixture that pins the per-function clamp (rev 15's one
		// composed matrix measured a 15.4667 mean / 0.2 hot on the
		// hue-sepia cell — Chrome clamps each function's output to
		// [0,1] before the next applies); the near-zero gates make any
		// return to composition, or a wrong order, fail loudly.
		MaxMeanChannelDiff:    0.02,
		MaxROIMeanChannelDiff: 0.1,
		MaxHotPixelFraction:   0.002,
	},
}

// chainPage is the HTML twin of ChainView.
const chainPage = `<!doctype html>
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
		gap: 24px;
	}
	.swatch {
		width: 120px;
		height: 48px;
		display: flex;
	}
	.swatch > div {
		width: 24px;
		height: 48px;
		flex-shrink: 0;
	}
	.red { background: #ff0000; }
	.green { background: #00ff00; }
	.blue { background: #0000ff; }
	.white { background: #ffffff; }
	.gray { background: #808080; }
	.f-sepia-hue { filter: sepia(1) hue-rotate(90deg); }
	.f-hue-sepia { filter: hue-rotate(90deg) sepia(1); }
</style>
</head>
<body>
	<div class="swatch" data-id="chain-control"><div class="red"></div><div class="green"></div><div class="blue"></div><div class="white"></div><div class="gray"></div></div>
	<div class="swatch f-sepia-hue" data-id="sepia-hue"><div class="red"></div><div class="green"></div><div class="blue"></div><div class="white"></div><div class="gray"></div></div>
	<div class="swatch f-hue-sepia" data-id="hue-sepia"><div class="red"></div><div class="green"></div><div class="blue"></div><div class="white"></div><div class="gray"></div></div>
</body>
</html>
`

// ChainView is the gpui-go twin of chainPage: the control, sepia then
// hue-rotate, and hue-rotate then sepia — the chain composes in CSS
// order (first function applied first).
func ChainView(_ *parity.EventLog) gpui.AnyElement {
	return chainRoot(
		nil,
		[]gpui.Filter{gpui.Sepia(amountFull), gpui.HueRotate(hueDegrees)},
		[]gpui.Filter{gpui.HueRotate(hueDegrees), gpui.Sepia(amountFull)},
	)
}

// ChainSwapped is the deliberately-wrong view for the Tier C
// comparison: each chain's functions swapped — the composition in the
// wrong order. The swatches are unchanged, so only Tier C fails.
func ChainSwapped(_ *parity.EventLog) gpui.AnyElement {
	return chainRoot(
		nil,
		[]gpui.Filter{gpui.HueRotate(hueDegrees), gpui.Sepia(amountFull)},
		[]gpui.Filter{gpui.Sepia(amountFull), gpui.HueRotate(hueDegrees)},
	)
}

// chainRoot builds the chain fixture's root: the control beside the two
// order-swapped chains, centered.
func chainRoot(control, sepiaHue, hueSepia []gpui.Filter) gpui.AnyElement {
	root := gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Flex().
		Justify(gpui.AlignContentCenter).
		AlignItems(gpui.AlignItemsCenter).
		Gap(gpui.Px(imageCellGap)).
		Bg(gpui.Rgb(pageBackground))
	for _, cell := range []struct {
		id      string
		filters []gpui.Filter
	}{
		{"chain-control", control},
		{"sepia-hue", sepiaHue},
		{"hue-sepia", hueSepia},
	} {
		root = root.Child(filteredSwatch(cell.id, cell.filters))
	}
	return root.IntoElement()
}

// ---------------------------------------------------------------------------
// filters-image
// ---------------------------------------------------------------------------

// ImageFixture is the filter-on-an-image fixture: grayscale(1) and
// hue-rotate(90deg) on img elements around a control — the sprite path
// through the filter group.
var ImageFixture = parity.Fixture{
	Name:   "filters-image",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	View:   ImageView,
	BoxIDs: []string{"img-control", "img-grayscale", "img-hue-rotate"},
	Tolerance: parity.PixelTolerance{
		// Measured against the rev-15.1 DLL: whole 0.0493 / 0.00096;
		// ROIs img-control 2.1740 / 0.04094, img-grayscale 1.2747 /
		// 0.02042, img-hue-rotate 1.8084 / 0.04083 (the same image
		// resampling class the object-position fixture measured — the
		// pinned bilinear sampler vs Chrome's upsampling; the colour
		// map itself is exact, as filters-colors proves). The gates sit
		// at 2x the measured stats.
		MaxMeanChannelDiff:    0.1,
		MaxROIMeanChannelDiff: 4.5,
		MaxHotPixelFraction:   0.09,
	},
}

// imagePage is the HTML twin of ImageView; the three %s receive the
// quadrant image's data URI.
const imagePage = `<!doctype html>
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
		gap: 24px;
	}
	.cell {
		width: 60px;
		height: 40px;
	}
	.cell img {
		display: block;
		width: 60px;
		height: 40px;
	}
	.f-grayscale { filter: grayscale(1); }
	.f-hue-rotate { filter: hue-rotate(90deg); }
</style>
</head>
<body>
	<div class="cell" data-id="img-control"><img src="%s"></div>
	<div class="cell" data-id="img-grayscale"><img src="%s" class="f-grayscale"></div>
	<div class="cell" data-id="img-hue-rotate"><img src="%s" class="f-hue-rotate"></div>
</body>
</html>
`

// ImageView is the gpui-go twin of imagePage: three 60x40 images, the
// control, grayscale(1) and hue-rotate(90deg).
func ImageView(_ *parity.EventLog) gpui.AnyElement {
	return imageRoot(func(id string) []gpui.Filter {
		switch id {
		case "img-grayscale":
			return []gpui.Filter{gpui.Grayscale(amountFull)}
		case "img-hue-rotate":
			return []gpui.Filter{gpui.HueRotate(hueDegrees)}
		default:
			return nil
		}
	})
}

// ImageUnfiltered is the deliberately-wrong view for the Tier C
// comparison: no image carries its filter. The cells are unchanged, so
// only Tier C fails.
func ImageUnfiltered(_ *parity.EventLog) gpui.AnyElement {
	return imageRoot(func(string) []gpui.Filter { return nil })
}

// imageRoot builds the image fixture's root: three 60x40 framed images
// with the filters the chooser returns, centered.
func imageRoot(chooser func(id string) []gpui.Filter) gpui.AnyElement {
	image := quadrantImage()
	root := gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Flex().
		Justify(gpui.AlignContentCenter).
		AlignItems(gpui.AlignItemsCenter).
		Gap(gpui.Px(imageCellGap)).
		Bg(gpui.Rgb(pageBackground))
	for _, id := range []string{"img-control", "img-grayscale", "img-hue-rotate"} {
		img := gpui.Img(image).Flex().
			W(gpui.Px(60)).
			H(gpui.Px(40))
		if filters := chooser(id); filters != nil {
			img = img.Filter(filters)
		}
		root = root.Child(gpui.Div().Flex().
			ID(id).
			DebugSelector(id).
			W(gpui.Px(60)).
			H(gpui.Px(40)).
			Child(img))
	}
	return root.IntoElement()
}

// ---------------------------------------------------------------------------
// filters-objectposition
// ---------------------------------------------------------------------------

// ObjectPositionFixture is the object-position fixture: keywords,
// percentages and lengths under each object-fit mode (contain, none,
// cover, scale-down, fill).
var ObjectPositionFixture = parity.Fixture{
	Name:   "filters-objectposition",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	View:   ObjectPositionView,
	BoxIDs: []string{
		"contain-left-top", "contain-right-bottom", "contain-percent", "contain-px",
		"none-left-top", "none-right-bottom", "none-percent", "none-px",
		"cover-left-top", "cover-percent", "scaledown-right-bottom", "fill-px",
	},
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run), set at
		// ~2x the measured correct-view statistics: whole mean 0.4882 /
		// hot 0.0068, and per frame up to 1.4933 mean (cover-left-top) /
		// 0.01769 hot — the pinned renderer's image resampling (the
		// linear sampler vs Chrome's upsampling on the hard-edged
		// quadrant image), the same class boxpaint-objectfit measured;
		// the image rectangles themselves match to a pixel (verified:
		// the pixelated fixture's point-sampled cell measures 0.0000).
		// The centered wrong view measures whole 6.6664, ROIs 2.80–17.29
		// (none-left-top 17.2879), hot up to 0.2167. Verified by
		// filters_test.go.
		MaxMeanChannelDiff:    1.0,
		MaxROIMeanChannelDiff: 3.0,
		MaxHotPixelFraction:   0.04,
	},
}

// objectPositionPage is the HTML twin of ObjectPositionView; the
// twelve %s receive the quadrant image's data URI.
const objectPositionPage = `<!doctype html>
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
		gap: 24px;
	}
	.row {
		display: flex;
		gap: 24px;
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
	.contain img { object-fit: contain; }
	.none img { object-fit: none; }
	.cover img { object-fit: cover; }
	.scale-down img { object-fit: scale-down; }
	.fill img { object-fit: fill; }
	.pos-left-top img { object-position: left top; }
	.pos-right-bottom img { object-position: right bottom; }
	.pos-percent img { object-position: 25%% 75%%; }
	.pos-px img { object-position: 12px 8px; }
</style>
</head>
<body>
	<div class="row">
		<div class="frame contain pos-left-top" data-id="contain-left-top"><img src="%s"></div>
		<div class="frame contain pos-right-bottom" data-id="contain-right-bottom"><img src="%s"></div>
		<div class="frame contain pos-percent" data-id="contain-percent"><img src="%s"></div>
		<div class="frame contain pos-px" data-id="contain-px"><img src="%s"></div>
	</div>
	<div class="row">
		<div class="frame none pos-left-top" data-id="none-left-top"><img src="%s"></div>
		<div class="frame none pos-right-bottom" data-id="none-right-bottom"><img src="%s"></div>
		<div class="frame none pos-percent" data-id="none-percent"><img src="%s"></div>
		<div class="frame none pos-px" data-id="none-px"><img src="%s"></div>
	</div>
	<div class="row">
		<div class="frame cover pos-left-top" data-id="cover-left-top"><img src="%s"></div>
		<div class="frame cover pos-percent" data-id="cover-percent"><img src="%s"></div>
		<div class="frame scale-down pos-right-bottom" data-id="scaledown-right-bottom"><img src="%s"></div>
		<div class="frame fill pos-px" data-id="fill-px"><img src="%s"></div>
	</div>
</body>
</html>
`

// ObjectPositionView is the gpui-go twin of objectPositionPage: twelve
// framed images (the boxpaint-objectfit frame shape) with each
// object-fit mode positioned by keywords, percentages and lengths.
func ObjectPositionView(_ *parity.EventLog) gpui.AnyElement {
	return objectPositionRoot(positionedFrames())
}

// ObjectPositionCentered is the deliberately-wrong view for the Tier C
// comparison: every image centered (the CSS default position), where
// the record places each by its object-position — the position is the
// only thing that moved. The frames are unchanged, so only Tier C
// fails.
func ObjectPositionCentered(_ *parity.EventLog) gpui.AnyElement {
	cells := positionedFrames()
	for i := range cells {
		cells[i].position = gpui.ObjectPositionCenter()
	}
	return objectPositionRoot(cells)
}

// positionedFrame is one object-position cell: the frame's id, fit and
// position. styleFit marks the cover cells, which set the fit through
// the style (object-cover's path) instead of the img element's
// option, so both storages are exercised end-to-end.
type positionedFrame struct {
	id       string
	fit      gpui.ObjectFit
	position gpui.ObjectPosition
	styleFit bool
}

// positionedFrames lists the fixture's twelve cells.
func positionedFrames() []positionedFrame {
	leftTop := gpui.ObjectPosition{X: gpui.ObjectLeft(), Y: gpui.ObjectTop()}
	rightBottom := gpui.ObjectPosition{X: gpui.ObjectRight(), Y: gpui.ObjectBottom()}
	percent := gpui.ObjectPosition{X: gpui.ObjectPercent(25), Y: gpui.ObjectPercent(75)}
	px := gpui.ObjectPosition{X: gpui.ObjectPx(12), Y: gpui.ObjectPx(8)}
	return []positionedFrame{
		{"contain-left-top", gpui.ObjectFitContain, leftTop, false},
		{"contain-right-bottom", gpui.ObjectFitContain, rightBottom, false},
		{"contain-percent", gpui.ObjectFitContain, percent, false},
		{"contain-px", gpui.ObjectFitContain, px, false},
		{"none-left-top", gpui.ObjectFitNone, leftTop, false},
		{"none-right-bottom", gpui.ObjectFitNone, rightBottom, false},
		{"none-percent", gpui.ObjectFitNone, percent, false},
		{"none-px", gpui.ObjectFitNone, px, false},
		{"cover-left-top", gpui.ObjectFitCover, leftTop, true},
		{"cover-percent", gpui.ObjectFitCover, percent, true},
		{"scaledown-right-bottom", gpui.ObjectFitScaleDown, rightBottom, false},
		{"fill-px", gpui.ObjectFitFill, px, false},
	}
}

// objectPositionRoot builds the object-position fixture's root: three
// rows of four framed images, centered.
func objectPositionRoot(cells []positionedFrame) gpui.AnyElement {
	image := quadrantImage()
	root := gpui.Div().Flex().
		ID("root").
		DebugSelector("root").
		SizeFull().
		FlexCol().
		Justify(gpui.AlignContentCenter).
		AlignItems(gpui.AlignItemsCenter).
		Gap(gpui.Px(frameGap)).
		Bg(gpui.Rgb(pageBackground))
	for row := 0; row < 3; row++ {
		rowDiv := gpui.Div().Flex().Gap(gpui.Px(frameGap))
		for col := 0; col < 4; col++ {
			cell := cells[row*4+col]
			img := gpui.Img(image).Flex().
				ObjectPosition(cell.position.X, cell.position.Y).
				W(gpui.Px(frameWidth)).
				H(gpui.Px(frameHeight)).
				Border(gpui.Px(frameBorder)).
				BorderColor(gpui.Rgb(frameBorderCol)).
				P(gpui.Px(framePadding)).
				Bg(gpui.Rgb(frameGray))
			if cell.styleFit {
				// The cover row exercises the STYLE's object-fit (CSS
				// object-fit from styles — the object-cover class's path);
				// the other rows use the pinned img element option.
				img = img.ObjectCover()
			} else {
				img = img.ObjectFit(cell.fit)
			}
			rowDiv = rowDiv.Child(gpui.Div().Flex().
				ID(cell.id).
				DebugSelector(cell.id).
				W(gpui.Px(frameWidth)).
				H(gpui.Px(frameHeight)).
				Child(img))
		}
		root = root.Child(rowDiv)
	}
	return root.IntoElement()
}

// ---------------------------------------------------------------------------
// filters-pixelated
// ---------------------------------------------------------------------------

// PixelatedFixture is the image-rendering fixture: the 8x8 pixel art
// upscaled to 100x100 (12.5x, non-integer) with image-rendering:
// pixelated beside the default smooth rendering.
var PixelatedFixture = parity.Fixture{
	Name:   "filters-pixelated",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	View:   PixelatedView,
	BoxIDs: []string{"smooth", "pixelated"},
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run), set at
		// ~2x the measured correct-view statistics: the point-sampled
		// cell measures 0.0000 mean and 0.0000 hot — pixel-exact against
		// Chrome — and the smooth control cell 6.4025 / 0.08687 hot (the
		// pinned bilinear sampler vs Chrome's upsampling on the 1px
		// diagonal pattern), whole 0.2501 / 0.0034. The smooth-instead-
		// of-pixelated wrong view measures 41.7909 mean / 0.67450 hot in
		// the pixelated cell. Verified by filters_test.go.
		MaxMeanChannelDiff:    0.6,
		MaxROIMeanChannelDiff: 15.0,
		MaxHotPixelFraction:   0.2,
	},
}

// pixelatedPage is the HTML twin of PixelatedView; the two %s receive
// the pixel art's data URI.
const pixelatedPage = `<!doctype html>
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
	.cell img {
		display: block;
		box-sizing: border-box;
		width: 100px;
		height: 100px;
		object-fit: fill;
	}
	.pixelated img { image-rendering: pixelated; }
</style>
</head>
<body>
	<div class="cell" data-id="smooth"><img src="%s"></div>
	<div class="cell pixelated" data-id="pixelated"><img src="%s"></div>
</body>
</html>
`

// PixelatedView is the gpui-go twin of pixelatedPage: the same 8x8
// pixel art upscaled 12.5x, once smooth (the default bilinear sampler)
// and once through the point sampler (image-rendering: pixelated).
func PixelatedView(_ *parity.EventLog) gpui.AnyElement {
	return pixelatedRoot(true)
}

// PixelatedSmooth is the deliberately-wrong view for the Tier C
// comparison: both images smooth (no point sampling), where the record
// shows one with hard edges. The cells are unchanged, so only Tier C
// fails.
func PixelatedSmooth(_ *parity.EventLog) gpui.AnyElement {
	return pixelatedRoot(false)
}

// pixelatedRoot builds the pixelated fixture's root: the pixel art
// upscaled to 100x100 twice — smooth always, and pixelated when
// pointSampled.
func pixelatedRoot(pointSampled bool) gpui.AnyElement {
	image := pixelArtImage()
	root := gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Flex().
		Justify(gpui.AlignContentCenter).
		AlignItems(gpui.AlignItemsCenter).
		Gap(gpui.Px(pixelGap)).
		Bg(gpui.Rgb(pageBackground))
	for _, cell := range []struct {
		id        string
		pointSamp bool
	}{
		{"smooth", false},
		{"pixelated", pointSampled},
	} {
		img := gpui.Img(image).Flex().
			ObjectFit(gpui.ObjectFitFill).
			W(gpui.Px(pixelCell)).
			H(gpui.Px(pixelCell))
		if cell.pointSamp {
			img = img.RenderingPixelated()
		}
		root = root.Child(gpui.Div().Flex().
			ID(cell.id).
			DebugSelector(cell.id).
			W(gpui.Px(pixelCell)).
			H(gpui.Px(pixelCell)).
			Child(img))
	}
	return root.IntoElement()
}

// ---------------------------------------------------------------------------
// Registration
// ---------------------------------------------------------------------------

func init() {
	// The image pages carry their embedded PNGs as data URIs so the
	// recorded page.html files are self-contained.
	ImageFixture.HTML = fmt.Sprintf(imagePage,
		quadrantDataURL(), quadrantDataURL(), quadrantDataURL())
	ObjectPositionFixture.HTML = fmt.Sprintf(objectPositionPage,
		quadrantDataURL(), quadrantDataURL(), quadrantDataURL(), quadrantDataURL(),
		quadrantDataURL(), quadrantDataURL(), quadrantDataURL(), quadrantDataURL(),
		quadrantDataURL(), quadrantDataURL(), quadrantDataURL(), quadrantDataURL())
	PixelatedFixture.HTML = fmt.Sprintf(pixelatedPage,
		pixelArtDataURL(), pixelArtDataURL())
	parity.Register(ColorsFixture)
	parity.Register(ChainFixture)
	parity.Register(ImageFixture)
	parity.Register(ObjectPositionFixture)
	parity.Register(PixelatedFixture)
}

// pixelArtDataURL returns the embedded pixel-art PNG as a data: URI.
func pixelArtDataURL() string {
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(pixelArtPNG)
}
