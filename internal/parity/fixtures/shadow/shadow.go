//go:build windows

// Package shadow is ticket 04's fixture set for CSS shadow and blur
// geometry (spec decision D8), each recorded in headless Chrome and
// replayed against the gpui-go view through the browser-parity
// harness:
//
//   - shadow-blur: a box with a blurred drop shadow. CSS
//     css-backgrounds-3 §shadow-blur: the blur is a Gaussian whose σ is
//     half the blur radius (Chromium ShadowData::BlurRadiusToStdDev,
//     core/style/shadow_data.h:76-88 in the research checkout — "Per
//     spec, sigma is exactly half the blur radius", fed to Skia's blur
//     mask filter whose sigma IS the standard deviation,
//     box_painter_base.cc:339 + cc/paint/draw_looper.cc:28-30). The pin
//     passed the blur radius through as σ, twice as blurry.
//   - shadow-spread: hard spread shadows on rounded boxes. CSS
//     §corner-shaping grows the shadow shape's radii with the spread —
//     r + spread when the radius exceeds the spread, the cubic ratio
//     rule when it does not (Chromium OutsetWithCornerCorrection /
//     AdjustedRadiusDimension, platform/geometry/float_rounded_rect.cc:
//     131-186, applied by ApplySpreadToShadowShape,
//     core/paint/box_painter_base.cc:90-95). The pin kept the element's
//     radii: the 8px halo on a 12px-radius box kept 12px corners where
//     CSS gives 20px, and the 10px halo on the 4px-radius box keeps 4px
//     where CSS gives ≈11.84px (the ratio rule, not the naive 14).
//   - shadow-order: a two-shadow list. CSS paints the FIRST shadow on
//     top (Chromium "the first-specified shadow is painted on top",
//     core/style/shadow_list.h:46-47 — box_painter_base.cc:295 iterates
//     the list backwards). The pin painted them in list order, so its
//     last shadow was on top.
//   - shadow-filter: an element with `filter: blur` AND a box shadow.
//     filter-effects-1 §FilterProperty: the filter applies to the whole
//     rendered element, including its box-shadow (Chromium paints the
//     decorations inside the filter effect node; the pin painted the
//     shadows outside with_filter_layer, so its blur never touched
//     them). The blur's σ is the CSS length (filter-effects-1
//     §funcdef-filter-blur; Chromium FEGaussianBlur(std_deviation = the
//     raw length), core/paint/filter_effect_builder.cc:284-291, passed
//     to Skia as the sigma, cc/paint/render_surface_filters.cc:
//     193-212) — and the pin's record already lands there: its kernel
//     computes σ = 0.5 × the record radius in half-resolution TEXELS
//     (blur.rs:17), so the effective device-pixel σ is the record
//     radius = the CSS radius × scale. The fixture measured the pin
//     value at 0.42 mean channel diff against Chrome (9.73 doubled),
//     correcting the spec's D8 note that read the texel conversion
//     without the half-resolution factor.
//   - shadow-backdrop: a translucent panel with `backdrop-filter:
//     blur` over colored bars. filter-effects-2: the backdrop's blur σ
//     is the CSS length, and the input is the backdrop behind the
//     element (Chromium builds the same chain, paint_layer.cc:2629-
//     2638); the pin's record value is the CSS σ (the same half-res
//     kernel). The fixture's pixel comparison is blocked on a native
//     rev-15 backdrop blur bug (see BackdropFixture).
//
// The pages are text-free, so Tier C carries no font rasterization
// noise. The ROIs are wrapper elements sized to cover each shadow's
// ink extent (shadows paint outside the element box, so the element's
// own box would not cover the interesting pixels).
package shadow

import (
	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The numbers shared by every page and its gpui view (the tracer's
// discipline: both sides are built from one set of constants).
const (
	windowWidth  = 640.0
	windowHeight = 400.0
	rootGap      = 48.0

	// The blur fixture: a 200x120 card with a 24px blur shadow offset
	// (14, 18). The ink reaches ceil(3σ)=36 ± offset past the card
	// (right 50, bottom 54), so the ROI wrapper pads 56 on every side.
	blurBoxW    = 200.0
	blurBoxH    = 120.0
	blurOffsetX = 14.0
	blurOffsetY = 18.0
	blurBlur    = 24.0
	blurPad     = 56.0

	// The spread fixture: a 160x100 box with 12px radii and an 8px hard
	// halo (CSS grows the radii to 20px), beside a 120x70 box with 4px
	// radii and a 10px halo (the ratio rule: ≈11.84px, where the naive
	// r + spread gives 14 and the pin keeps 4). The wrappers pad the
	// halo plus 2px.
	spreadWideW      = 160.0
	spreadWideH      = 100.0
	spreadWideRadii  = 12.0
	spreadWideS      = 8.0
	spreadSmallW     = 120.0
	spreadSmallH     = 70.0
	spreadSmallRadii = 4.0
	spreadSmallS     = 10.0
	spreadMargin     = 2.0

	// The order fixture: a 160x100 box with two hard shadows — a 10px
	// red halo first (on top), then a blue shadow offset (28, 28). The
	// ink reaches 10 left/top and 28 right/bottom, so the wrapper is the
	// box plus 38 per axis plus 2px margin.
	orderBoxW   = 160.0
	orderBoxH   = 100.0
	orderHaloS  = 10.0
	orderOffset = 28.0
	orderMargin = 2.0

	// The filter fixture: a bordered 180x110 card with a swatch child,
	// a shadow offset (10, 12) with 18px blur, and `filter: blur(3px)`.
	// The shadow ink reaches ceil(3σ)=27 ± offset past the card and the
	// filter adds its own reach, so the wrapper pads 48 on every side.
	filterBoxW    = 180.0
	filterBoxH    = 110.0
	filterBorder  = 2.0
	filterOffsetX = 10.0
	filterOffsetY = 12.0
	filterBlur    = 18.0
	filterSigma   = 3.0
	filterPad     = 48.0
	swatchW       = 120.0
	swatchH       = 24.0

	// The backdrop fixture: eight 80x400 colored bars under a
	// full-viewport translucent panel with `backdrop-filter: blur(6px)`.
	// The panel covers the whole window because the rev-15 backdrop
	// crop's composite samples the window-sized blur texture with
	// crop-relative coordinates (a native bug this ticket reports: a
	// partial-region backdrop renders its blurred input zoomed — see
	// the ticket's Answer); with a full-viewport panel the crop region
	// IS the window and the composite's mapping is exact, so the
	// fixture pins the CSS σ rule (σ = the blur length).
	backdropBarW = 80.0
	backdropBlur = 6.0
)

// The palette, shared by the pages and the views.
const (
	pageBackground  = 0xffffff
	blurShadow      = 0x0f172e66 // rgba(15,23,42,0.4): alpha 0.4 = 102 = 0x66
	orderCardBg     = 0x0f172e
	orderRed        = 0xdc262666 // rgba(220,38,38,0.4): alpha 0.4 = 0x66... see below
	orderBlue       = 0x2563eb99 // rgba(37,99,235,0.6): alpha 0.6 = 153 = 0x99
	spreadWideBg    = 0x2563eb
	spreadWideHalo  = 0xdc262666 // rgba(220,38,38,0.4)
	spreadSmallBg   = 0x16a34a
	spreadSmallHalo = 0x2563eb99 // rgba(37,99,235,0.6)
	filterBorderC   = 0x334155
	swatchGreen     = 0x16a34a
	panelTint       = 0xffffff33 // rgba(255,255,255,0.2): alpha 0.2 = 51 = 0x33
)

// The backdrop bars' colors, left to right.
var backdropBars = []uint32{0xef4444, 0xf59e0b, 0x22c55e, 0x3b82f6, 0xa855f7, 0xec4899, 0xef4444, 0xf59e0b}

// BlurFixture is the blurred-shadow fixture: the shadow's σ is half
// the CSS blur radius.
var BlurFixture = parity.Fixture{
	Name:   "shadow-blur",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   blurPage,
	View:   BlurView,
	BoxIDs: []string{"blur-card", "blur-box"},
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run), set at
		// ~2x the measured correct-view statistics: whole 0.029, ROI
		// (blur-card, the ink-covering wrapper) 0.104, hot 0.0000 -- the
		// analytic shadow sigma at CSS blur/2 matches Chrome's Skia box
		// blur that closely. The deliberately-wrong view (sigma = the
		// blur radius, the pin's scale) measures whole 1.073 / ROI
		// 3.616. Verified by shadow_test.go.
		MaxMeanChannelDiff:    0.06,
		MaxROIMeanChannelDiff: 0.25,
		MaxHotPixelFraction:   0.002,
	},
}

// SpreadFixture is the spread fixture: a spread grows the shadow
// shape's corner radii (the simple rule and the ratio rule).
var SpreadFixture = parity.Fixture{
	Name:   "shadow-spread",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   spreadPage,
	View:   SpreadView,
	BoxIDs: []string{"spread-wide", "spread-small"},
	Tolerance: parity.PixelTolerance{
		// Provisional, ~2x the measured correct-view statistics: whole
		// 0.002, ROIs 0.011 (small) / 0.017 (wide), hot 0.0000 -- the
		// spread-grown radii land on Chrome's rasterized corners almost
		// exactly. The pin-radii wrong view (the spread does not grow
		// the radii) measures whole 0.089 / ROIs 0.632-0.655 / hot
		// 0.0101. Verified by shadow_test.go.
		MaxMeanChannelDiff:    0.01,
		MaxROIMeanChannelDiff: 0.05,
		MaxHotPixelFraction:   0.002,
	},
}

// OrderFixture is the shadow-list order fixture: the first shadow
// paints on top.
var OrderFixture = parity.Fixture{
	Name:   "shadow-order",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   orderPage,
	View:   OrderView,
	BoxIDs: []string{"order-card", "order-box"},
	Tolerance: parity.PixelTolerance{
		// Provisional, ~2x the measured correct-view statistics: 0.0000
		// everywhere (the two hard shadows' overlap region compares
		// pixel-exactly: the same rrect edges, the same 40/60 blend).
		// The reversed-order wrong view measures ROI 2.611 / hot 0.0746.
		// Verified by shadow_test.go.
		MaxMeanChannelDiff:    0.02,
		MaxROIMeanChannelDiff: 0.05,
		MaxHotPixelFraction:   0.002,
	},
}

// FilterFixture is the filter fixture: `filter: blur` blurs the
// element's own shadow too, at the CSS σ.
var FilterFixture = parity.Fixture{
	Name:   "shadow-filter",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   filterPage,
	View:   FilterView,
	BoxIDs: []string{"filter-card", "filter-box"},
	Tolerance: parity.PixelTolerance{
		// Provisional, ~2x the measured correct-view statistics: whole
		// 0.063, ROIs 0.422 (box) / 0.285 (card), hot 0.0000 -- the pin's
		// filter blur radius (whose half-resolution kernel sigma IS the
		// CSS length in device pixels; see the package comment) and the
		// group's ink-dilated bounds cover the blurred shadow almost
		// exactly. The wrong views measure: sigma = radius/2 whole 0.698
		// / box ROI 6.930 / hot 0.0502; the shadow outside the group
		// whole 0.165 / box ROI 1.384 / hot 0.0064. Verified by
		// shadow_test.go.
		MaxMeanChannelDiff:    0.15,
		MaxROIMeanChannelDiff: 0.9,
		MaxHotPixelFraction:   0.004,
	},
}

// BackdropFixture is the backdrop fixture: `backdrop-filter: blur`
// over a pattern, at the CSS σ.
var BackdropFixture = parity.Fixture{
	Name:   "shadow-backdrop",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   backdropPage,
	View:   BackdropView,
	BoxIDs: []string{"backdrop-stage", "backdrop-panel"},
	Tolerance: parity.PixelTolerance{
		// Provisional; the pixel comparison is blocked on the native
		// rev-15 backdrop blur bug (see the fixture's comment and
		// shadow_test.go's blocked test) -- the values are placeholders
		// until the native fix lands, then recalibrate from the measured
		// correct-view statistics.
		MaxMeanChannelDiff:    0.5,
		MaxROIMeanChannelDiff: 4.0,
		MaxHotPixelFraction:   0.05,
	},
}

func init() {
	parity.Register(BlurFixture)
	parity.Register(SpreadFixture)
	parity.Register(OrderFixture)
	parity.Register(FilterFixture)
	// BackdropFixture is NOT registered: its record and view are
	// checked in and ready, but the rev-15 native backdrop blur is
	// broken (the crop path's composite samples the window-sized blur
	// texture with crop-relative coordinates, so a partial-region
	// backdrop renders its blurred input zoomed, and even a
	// full-viewport backdrop shows an un-blurred, neighbor-averaged
	// input — reproducible with cmd/parityrecord's record and the
	// shadow package's blocked test). Native code is out of this
	// ticket's scope; re-register the fixture when the native fix
	// lands (the record lives in testdata/shadow-backdrop).
}

// ---------------------------------------------------------------------------
// Shared view helpers
// ---------------------------------------------------------------------------

// centeredRoot builds the fixtures' shared root: a full-size flex box
// centering its children on the white page.
func centeredRoot(children ...*gpui.DivElement) gpui.AnyElement {
	root := gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Flex().
		Justify(gpui.AlignContentCenter).
		AlignItems(gpui.AlignItemsCenter).
		Gap(gpui.Px(rootGap)).
		Bg(gpui.Rgb(pageBackground))
	for _, child := range children {
		root = root.Child(child)
	}
	return root.IntoElement()
}

// roiWrapper builds an invisible wrapper that centers one box and
// whose rect (the Tier A box / Tier C ROI) covers the box plus its
// shadow ink.
func roiWrapper(id string, width, height float32, child *gpui.DivElement) *gpui.DivElement {
	return gpui.Div().
		ID(id).
		DebugSelector(id).
		W(gpui.Px(width)).
		H(gpui.Px(height)).
		Flex().
		Justify(gpui.AlignContentCenter).
		AlignItems(gpui.AlignItemsCenter).
		Child(child)
}

// dropShadow builds one box shadow in the CSS box-shadow order:
// offsetX, offsetY, blur, spread, color.
func dropShadow(offsetX, offsetY, blur, spread float32, color uint32) gpui.BoxShadow {
	return gpui.NewBoxShadow(offsetX, offsetY, gpui.SolidBackground(gpui.RgbaToHsla(color))).
		WithBlurRadius(blur).
		WithSpreadRadius(spread)
}

// ---------------------------------------------------------------------------
// The blur fixture
// ---------------------------------------------------------------------------

// blurPage is the HTML twin of BlurView.
const blurPage = `<!doctype html>
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
		width: 312px;
		height: 232px;
		display: flex;
		align-items: center;
		justify-content: center;
	}
	.box {
		width: 200px;
		height: 120px;
		background: #ffffff;
		/* CSS: sigma = blur / 2, so 24px of blur is a 12px-sigma Gaussian. */
		box-shadow: 14px 18px 24px rgba(15, 23, 42, 0.4);
	}
</style>
</head>
<body>
	<div class="card" data-id="blur-card"><div class="box" data-id="blur-box"></div></div>
</body>
</html>
`

// BlurView is the gpui-go twin of blurPage: a 200x120 white card with
// one blurred drop shadow.
func BlurView(_ *parity.EventLog) gpui.AnyElement {
	return blurRoot(blurBlur)
}

// BlurSigmaIsBlur is the deliberately-wrong view for the Tier C
// comparison: the shadow's sigma equals the CSS blur radius (the pin's
// scale), so the shadow renders twice as blurry as Chrome's record.
func BlurSigmaIsBlur(_ *parity.EventLog) gpui.AnyElement {
	return blurRoot(blurBlur * 2)
}

// blurRoot builds the blur fixture's view with the given blur value.
func blurRoot(blur float32) gpui.AnyElement {
	return centeredRoot(roiWrapper("blur-card", blurBoxW+2*blurPad, blurBoxH+2*blurPad, gpui.Div().
		ID("blur-box").
		DebugSelector("blur-box").
		W(gpui.Px(blurBoxW)).
		H(gpui.Px(blurBoxH)).
		Bg(gpui.Rgb(pageBackground)).
		Shadow([]gpui.BoxShadow{dropShadow(blurOffsetX, blurOffsetY, blur, 0, blurShadow)})))
}

// ---------------------------------------------------------------------------
// The spread fixture
// ---------------------------------------------------------------------------

// spreadPage is the HTML twin of SpreadView.
const spreadPage = `<!doctype html>
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
	.halo {
		display: flex;
		align-items: center;
		justify-content: center;
	}
	.wide {
		width: 180px;
		height: 120px;
	}
	.small {
		width: 144px;
		height: 94px;
	}
	.wide .box {
		width: 160px;
		height: 100px;
		border-radius: 12px;
		background: #2563eb;
		/* CSS grows the shadow's corner radii by the spread: 12 + 8 = 20. */
		box-shadow: 0 0 0 8px rgba(220, 38, 38, 0.4);
	}
	.small .box {
		width: 120px;
		height: 70px;
		border-radius: 4px;
		background: #16a34a;
		/* The 4px radius is smaller than the 10px spread, so the ratio
		   rule grows it to ~11.84px, not the naive 14 (and not the pin's
		   unchanged 4). */
		box-shadow: 0 0 0 10px rgba(37, 99, 235, 0.6);
	}
</style>
</head>
<body>
	<div class="halo wide" data-id="spread-wide"><div class="box" data-id="spread-wide-box"></div></div>
	<div class="halo small" data-id="spread-small"><div class="box" data-id="spread-small-box"></div></div>
</body>
</html>
`

// SpreadView is the gpui-go twin of spreadPage.
func SpreadView(_ *parity.EventLog) gpui.AnyElement {
	return centeredRoot(
		spreadHalo("spread-wide", spreadWideW, spreadWideH, spreadWideRadii, spreadWideS,
			spreadWideBg, spreadWideHalo),
		spreadHalo("spread-small", spreadSmallW, spreadSmallH, spreadSmallRadii, spreadSmallS,
			spreadSmallBg, spreadSmallHalo),
	)
}

// spreadHalo builds one spread fixture box wrapped in its ROI wrapper:
// the rounded box with its hard spread halo, through the element path
// (the CSS rules).
func spreadHalo(id string, width, height, radii, spread float32, background, halo uint32) *gpui.DivElement {
	box := gpui.Div().
		ID(id + "-box").
		DebugSelector(id + "-box").
		W(gpui.Px(width)).
		H(gpui.Px(height)).
		Rounded(gpui.Px(radii)).
		Bg(gpui.Rgb(background)).
		Shadow([]gpui.BoxShadow{dropShadow(0, 0, 0, spread, halo)})
	return roiWrapper(id, width+2*(spread+spreadMargin), height+2*(spread+spreadMargin), box)
}

// SpreadPinRadii is the deliberately-wrong view for the Tier C
// comparison: the shadows are painted through the pin's
// Window.PaintDropShadows path, whose spread does NOT grow the corner
// radii — the wide halo keeps 12px corners where CSS gives 20px, and
// the small halo keeps 4px where CSS gives ~11.84px. The wrappers and
// boxes are unchanged, so only Tier C fails.
func SpreadPinRadii(_ *parity.EventLog) gpui.AnyElement {
	return centeredRoot(
		pinShadowBox("spread-wide", spreadWideW, spreadWideH, spreadWideRadii, spreadWideS,
			spreadWideBg, spreadWideHalo),
		pinShadowBox("spread-small", spreadSmallW, spreadSmallH, spreadSmallRadii, spreadSmallS,
			spreadSmallBg, spreadSmallHalo),
	)
}

// spreadBox builds one rounded box with a hard spread halo, through
// the element path (the CSS rules).
func spreadBox(id string, width, height, radii, spread float32, background, halo uint32) *gpui.DivElement {
	return gpui.Div().
		ID(id + "-box").
		DebugSelector(id + "-box").
		W(gpui.Px(width)).
		H(gpui.Px(height)).
		Rounded(gpui.Px(radii)).
		Bg(gpui.Rgb(background)).
		Shadow([]gpui.BoxShadow{dropShadow(0, 0, 0, spread, halo)})
}

// pinShadowBox renders one spread fixture box through the PIN's
// shadow path: a custom element that paints its shadow with
// Window.PaintDropShadowsWithCornerSmoothing (the record the
// pin-conformance fixtures pin: the spread does not grow the radii)
// and its background quad on top, wrapped in the same-sized ROI
// wrapper so the boxes are unchanged.
func pinShadowBox(id string, width, height, radii, spread float32, background, halo uint32) *gpui.DivElement {
	box := gpui.Div().
		ID(id + "-box").
		DebugSelector(id + "-box").
		W(gpui.Px(width)).
		H(gpui.Px(height)).
		Child(gpui.CustomElement[gpui.LayoutID, struct{}](&pinnedShadowPainter{
			width:      width,
			height:     height,
			radius:     radii,
			background: gpui.Rgb(background),
			shadow:     dropShadow(0, 0, 0, spread, halo),
		}))
	return roiWrapper(id, width+2*(spread+spreadMargin), height+2*(spread+spreadMargin), box)
}

// pinnedShadowPainter is the pin's shadow path as a custom element:
// it paints the drop shadow through Window.PaintDropShadows (the
// pinned record construction — the spread leaves the corner radii
// alone) and then the background quad, like the pin's paint_box.
type pinnedShadowPainter struct {
	width      float32
	height     float32
	radius     float32
	background gpui.Hsla
	shadow     gpui.BoxShadow
}

func (p *pinnedShadowPainter) ID() (gpui.ElementID, bool)           { return gpui.ElementID{}, false }
func (p *pinnedShadowPainter) SourceLocation() *gpui.SourceLocation { return nil }

func (p *pinnedShadowPainter) RequestLayout(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, w *gpui.Window, _ *gpui.App) (gpui.LayoutID, gpui.LayoutID) {
	style := gpui.DefaultStyle()
	style.Size = gpui.LengthSize{Width: gpui.PxLength(p.width), Height: gpui.PxLength(p.height)}
	id, err := gpui.RequestElementLayout(w, style)
	if err != nil {
		panic(err)
	}
	return id, id
}

func (p *pinnedShadowPainter) Prepaint(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, _ gpui.Bounds, _ *gpui.LayoutID, _ *gpui.Window, _ *gpui.App) struct{} {
	return struct{}{}
}

func (p *pinnedShadowPainter) Paint(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, bounds gpui.Bounds, _ *gpui.LayoutID, _ *struct{}, w *gpui.Window, _ *gpui.App) {
	radii := gpui.Corners{TopLeft: p.radius, TopRight: p.radius, BottomRight: p.radius, BottomLeft: p.radius}
	// The pin's window.rs paint_drop_shadows: the record keeps the
	// element's radii under a spread (Scene.PaintShadow semantics).
	w.PaintDropShadowsWithCornerSmoothing(bounds, radii, 0, []gpui.BoxShadow{p.shadow})
	w.PaintQuadWithCornerSmoothing(gpui.PaintQuad{
		Bounds:             bounds,
		CornerRadii:        radii,
		Background:         gpui.SolidBackground(p.background),
		BorderColor:        gpui.SolidBackground(gpui.TransparentBlack()),
		BorderStyle:        gpui.BorderStyleSolid,
		BorderDashedLength: gpui.DefaultBorderDashedLength,
		BorderDashedGap:    gpui.DefaultBorderDashedGap,
	}, 0)
}

// ---------------------------------------------------------------------------
// The order fixture
// ---------------------------------------------------------------------------

// orderPage is the HTML twin of OrderView.
const orderPage = `<!doctype html>
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
		width: 202px;
		height: 142px;
		display: flex;
		align-items: center;
		justify-content: center;
	}
	.box {
		width: 160px;
		height: 100px;
		background: #0f172e;
		/* The FIRST shadow (the red halo) paints on top where the two
		   shadows overlap, at the box's bottom-right corner. */
		box-shadow: 0 0 0 10px rgba(220, 38, 38, 0.4), 28px 28px 0 0 rgba(37, 99, 235, 0.6);
	}
</style>
</head>
<body>
	<div class="card" data-id="order-card"><div class="box" data-id="order-box"></div></div>
</body>
</html>
`

// OrderView is the gpui-go twin of orderPage.
func OrderView(_ *parity.EventLog) gpui.AnyElement {
	return orderRoot(false)
}

// OrderReversed is the deliberately-wrong view for the Tier C
// comparison: the shadow list is reversed, so the blue offset shadow
// paints on top of the red halo where they overlap (the pin's list
// order). The boxes are unchanged, so only Tier C fails.
func OrderReversed(_ *parity.EventLog) gpui.AnyElement {
	return orderRoot(true)
}

// orderRoot builds the order fixture's view, with the shadow list
// reversed when reversed is set.
func orderRoot(reversed bool) gpui.AnyElement {
	shadows := []gpui.BoxShadow{
		dropShadow(0, 0, 0, orderHaloS, orderRed),
		dropShadow(orderOffset, orderOffset, 0, 0, orderBlue),
	}
	if reversed {
		shadows[0], shadows[1] = shadows[1], shadows[0]
	}
	// Per-side ink: the halo reaches 10 on every side, the offset
	// shadow reaches 28 right and bottom; the near sides only see the
	// halo. The wrapper is the box plus each side's ink plus a 2px
	// margin (202x144, like the page).
	inkNear := float32(orderHaloS + orderMargin)
	inkFar := float32(orderHaloS + orderMargin)
	if orderOffset > orderHaloS {
		inkFar = float32(orderOffset + orderMargin)
	}
	return centeredRoot(roiWrapper("order-card",
		orderBoxW+inkNear+inkFar, orderBoxH+inkNear+inkFar,
		gpui.Div().
			ID("order-box").
			DebugSelector("order-box").
			W(gpui.Px(orderBoxW)).
			H(gpui.Px(orderBoxH)).
			Bg(gpui.Rgb(orderCardBg)).
			Shadow(shadows)))
}

// ---------------------------------------------------------------------------
// The filter fixture
// ---------------------------------------------------------------------------

// filterPage is the HTML twin of FilterView.
const filterPage = `<!doctype html>
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
		width: 276px;
		height: 206px;
		display: flex;
		align-items: center;
		justify-content: center;
	}
	.box {
		box-sizing: border-box;
		width: 180px;
		height: 110px;
		background: #ffffff;
		border: 2px solid #334155;
		/* The filter applies to the whole element, box-shadow included. */
		box-shadow: 10px 12px 18px rgba(15, 23, 42, 0.4);
		filter: blur(3px);
		display: flex;
		align-items: center;
		justify-content: center;
	}
	.swatch {
		width: 120px;
		height: 24px;
		background: #16a34a;
	}
</style>
</head>
<body>
	<div class="card" data-id="filter-card">
		<div class="box" data-id="filter-box"><div class="swatch"></div></div>
	</div>
</body>
</html>
`

// FilterView is the gpui-go twin of filterPage: a bordered card with a
// swatch, a blurred shadow and `filter: blur(3px)` on the card.
func FilterView(_ *parity.EventLog) gpui.AnyElement {
	return filterRoot(true, filterSigma)
}

// FilterShadowOutsideGroup is the deliberately-wrong view for the Tier
// C comparison: the shadow sits on an unfiltered wrapper around the
// blurred card, so the shadow paints OUTSIDE the filter group and
// stays sharp (the pin's structure: paint the shadows above
// with_filter_layer). The boxes are unchanged, so only Tier C fails.
func FilterShadowOutsideGroup(_ *parity.EventLog) gpui.AnyElement {
	return filterRoot(false, filterSigma)
}

// FilterSigmaIsHalfRadius is the deliberately-wrong view for the Tier
// C comparison: the filter blur's sigma is half the CSS length — the
// literal reading of the pin's blur.rs texel conversion without the
// half-resolution factor (the spec's D8 note) — so the card and its
// shadow blur half as much as Chrome's record. The boxes are
// unchanged, so only Tier C fails.
func FilterSigmaIsHalfRadius(_ *parity.EventLog) gpui.AnyElement {
	return filterRoot(true, filterSigma/2)
}

// filterRoot builds the filter fixture's view. In the correct view the
// card carries the shadow AND the filter, so the group blurs the
// shadow. In the wrong view an unfiltered wrapper of the same size
// carries the shadow and the card (the inner div) carries only the
// filter: the shadow paints outside the group and stays sharp.
func filterRoot(shadowInsideGroup bool, sigma float32) gpui.AnyElement {
	card := gpui.Div().
		ID("filter-box").
		DebugSelector("filter-box").
		W(gpui.Px(filterBoxW)).
		H(gpui.Px(filterBoxH)).
		Border(gpui.Px(filterBorder)).
		BorderColor(gpui.Rgb(filterBorderC)).
		Bg(gpui.Rgb(pageBackground)).
		Blur(sigma).
		Flex().
		Justify(gpui.AlignContentCenter).
		AlignItems(gpui.AlignItemsCenter).
		Child(gpui.Div().W(gpui.Px(swatchW)).H(gpui.Px(swatchH)).Bg(gpui.Rgb(swatchGreen)))
	if shadowInsideGroup {
		card = card.Shadow([]gpui.BoxShadow{dropShadow(filterOffsetX, filterOffsetY, filterBlur, 0, blurShadow)})
		return centeredRoot(roiWrapper("filter-card", filterBoxW+2*filterPad, filterBoxH+2*filterPad, card))
	}
	wrapper := gpui.Div().
		W(gpui.Px(filterBoxW)).
		H(gpui.Px(filterBoxH)).
		Shadow([]gpui.BoxShadow{dropShadow(filterOffsetX, filterOffsetY, filterBlur, 0, blurShadow)}).
		Child(card)
	return centeredRoot(roiWrapper("filter-card", filterBoxW+2*filterPad, filterBoxH+2*filterPad, wrapper))
}

// ---------------------------------------------------------------------------
// The backdrop fixture
// ---------------------------------------------------------------------------

// backdropPage is the HTML twin of BackdropView.
const backdropPage = `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<style>
	html, body {
		margin: 0;
		padding: 0;
		background: #ffffff;
	}
	.stage {
		position: relative;
		width: 640px;
		height: 400px;
	}
	.bars {
		position: absolute;
		inset: 0;
		display: flex;
	}
	.bar {
		width: 80px;
		height: 100%;
		flex-shrink: 0;
	}
	.b1 { background: #ef4444; }
	.b2 { background: #f59e0b; }
	.b3 { background: #22c55e; }
	.b4 { background: #3b82f6; }
	.b5 { background: #a855f7; }
	.b6 { background: #ec4899; }
	.b7 { background: #ef4444; }
	.b8 { background: #f59e0b; }
	.panel {
		position: absolute;
		inset: 0;
		/* The blur length is the sigma: 6px. */
		backdrop-filter: blur(6px);
		background: rgba(255, 255, 255, 0.2);
	}
</style>
</head>
<body>
	<div class="stage" data-id="backdrop-stage">
		<div class="bars">
			<div class="bar b1"></div>
			<div class="bar b2"></div>
			<div class="bar b3"></div>
			<div class="bar b4"></div>
			<div class="bar b5"></div>
			<div class="bar b6"></div>
			<div class="bar b7"></div>
			<div class="bar b8"></div>
		</div>
		<div class="panel" data-id="backdrop-panel"></div>
	</div>
</body>
</html>
`

// BackdropView is the gpui-go twin of backdropPage: a rounded
// translucent panel with a backdrop blur over six colored bars.
func BackdropView(_ *parity.EventLog) gpui.AnyElement {
	return backdropRoot(backdropBlur)
}

// BackdropSigmaIsHalfRadius is the deliberately-wrong view for the
// Tier C comparison: the backdrop blur's sigma is half the CSS length
// — the literal reading of the pin's blur.rs texel conversion without
// the half-resolution factor (the spec's D8 note) — so the panel
// blurs half as much as Chrome's record. The boxes are unchanged, so
// only Tier C fails.
func BackdropSigmaIsHalfRadius(_ *parity.EventLog) gpui.AnyElement {
	return backdropRoot(backdropBlur / 2)
}

// backdropRoot builds the backdrop fixture's view with the given
// backdrop blur: the bars fill the window, the translucent backdrop
// panel covers it.
func backdropRoot(blur float32) gpui.AnyElement {
	bars := gpui.Div().Absolute().Inset0().Flex()
	for _, color := range backdropBars {
		bars = bars.Child(gpui.Div().
			W(gpui.Px(backdropBarW)).
			HFull().
			Bg(gpui.Rgb(color)))
	}
	return gpui.Div().
		ID("backdrop-stage").
		DebugSelector("backdrop-stage").
		SizeFull().
		Relative().
		Bg(gpui.Rgb(pageBackground)).
		Child(bars).
		Child(gpui.Div().
			ID("backdrop-panel").
			DebugSelector("backdrop-panel").
			Absolute().
			Inset0().
			BackdropBlur(blur).
			Bg(gpui.RgbaToHsla(panelTint))).
		IntoElement()
}
