//go:build windows

// Package lineheight holds ticket 37's browser-parity fixtures
// (.scratch/browser-core/issues/37-line-height-normal.md, spec D1):
// the DEFAULT line height is CSS `normal` — the font's own metrics, not
// the reference's golden-ratio fraction.
//
// The rule this fixture records (the research checkout,
// %TEMP%\research-src\chromium):
//
//   - The used line height of `line-height: normal` is the primary
//     font's line spacing:
//     ComputedStyle::ComputedLineHeight's IsAuto branch returns
//     FontMetrics().LineSpacing()
//     (third_party/blink/renderer/core/style/computed_style.cc:
//     2425-2436, "For 'normal' line-height use the font's built-in
//     spacing").
//   - LineSpacing is lroundf(ascent) + lroundf(descent) +
//     lroundf(line_gap) (third_party/blink/renderer/platform/fonts/
//     simple_font_data.cc:158-165, PlatformInit), where the Windows
//     ascent/descent are themselves the rounded scaled metrics of
//     FontMetrics::AscentDescentWithHacks
//     (platform/fonts/font_metrics.cc:113-114). Segoe UI 16px:
//     round(2210/2048*16) + round(514/2048*16) + round(0) =
//     17 + 4 + 0 = 21.
//   - The half leading of a normal line splits with the ascent side
//     FLOORED and the remainder on the descent side
//     (CalculateLeadingSpace, core/layout/inline/line_utils.cc:34-43),
//     fed the rounded FixedAscent/FixedDescent
//     (inline_box_state.cc:113-158): a line-gap-less font has zero
//     leading, so the baseline is exactly the rounded ascent (17).
//
// The port's rule (gpui): TextStyle.LineHeight carries the CSS normal
// keyword (DefiniteLengthNormal) by default; the resolution
// (gpui.ResolveLineHeight, Window.LineHeight, the layout entry points
// of div.go / inline.go / styled_text.go) computes the same
// lroundf(ascent) + lroundf(descent) + lroundf(line gap) from the
// resolved font's FontMetrics (FontMetrics.LineSpacingPx), and the
// inline row bounds use the rounded metrics with Chrome's leading
// split (roundedInlineLineBounds, gpui/inline.go).
//
// The fixture shape follows the whitespace fixture (ticket 38): one
// 640-wide window, a flex column of 380-wide rows (8px padding, 1px
// border, AUTO heights) whose block spans pin the 362px content width —
// every span's height is its line count × the line height, so each Tier
// A box carries the rule under test. The rows cover several font sizes
// (the rounding moves at each), a two-line wrapped row, an inline span
// with a bigger font (the mixed-strut line box), and an explicit
// line-height: 1.5 row proving definite lengths still resolve.
//
// The deliberately-wrong view (WrongPhiView) puts the golden-ratio line
// height — the reference's default, the port's pre-D1 default — on the
// whole tree, which must fail the Tier A box comparison (the
// discrimination test in lineheight_test.go).
package lineheight

import (
	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The shared page numbers: one window, one column of rows. Every row is
// content-driven vertically: its span's height is the line count times
// the line height, plus the 18px of padding and border.
const (
	windowWidth  = 640.0
	windowHeight = 520.0

	pagePadding = 16.0
	rowGap      = 12.0

	rowWidth  = 380.0
	rowPad    = 8.0
	rowBorder = 1.0
	// spanWidth is the block span's pinned content width:
	// 380 - 2*8 - 2*1.
	spanWidth = rowWidth - 2*rowPad - 2*rowBorder

	pageBackground = 0xffffff
	rowBackground  = 0xe2e8f0
	spanBackground = 0xdbeafe
	borderColor    = 0x334155
	textColor      = 0x0f172a
)

// wrapText is the wrapped row's body: the mincontent fixture's six
// 13-14 char words, which wrap three per 362px row at 16px in both
// text systems — two lines, so the row's height is 2 × the line
// height.
const wrapText = "consequences determination possibilities extraordinary architecture mathematics"

// mixedTextPrefix, mixedTextBig and mixedTextSuffix spell the mixed row:
// a 16px paragraph carrying one 24px inline span, whose own normal
// strut grows the shared line box.
const (
	mixedPrefix = "one "
	mixedBig    = "BIG"
	mixedSuffix = " tail"
)

// Fixture is the Segoe UI fixture: the default line height under five
// font sizes, the wrapped row, the mixed-size inline span and the
// explicit 1.5 ratio row.
var Fixture = parity.Fixture{
	Name:   "lh-normal-segoe",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   page(segoePageFont, segoeRows),
	View:   View,
	BoxIDs: []string{
		"root",
		"row-12", "span-12",
		"row-13", "span-13",
		"row-16", "span-16",
		"row-24", "span-24",
		"row-48", "span-48",
		"row-wrap", "span-wrap",
		"row-mixed", "span-mixed",
		"row-ratio", "span-ratio",
	},
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run), set from
		// the measured correct-view statistics (whole 0.157 / worst ROI
		// span-ratio 1.317 — the explicit 1.5 ratio keeps the pin's
		// symmetric half-leading baseline, a subpixel divergence from
		// Chrome's floored ascent leading — / hot 0.0178) at ~2x each,
		// and verified by the wrong-view test (lineheight_test.go: the
		// golden-ratio view measures far above). Tier A is exact — the
		// row boxes carry the rule. See docs/browser-parity-harness.md.
		MaxMeanChannelDiff:    0.4,
		MaxROIMeanChannelDiff: 3.0,
		MaxHotPixelFraction:   0.04,
	},
}

func init() { parity.Register(Fixture) }

// segoePageFont is the shared body font: Segoe UI with NO line-height
// pin — the CSS initial value, `normal`, the rule under test.
const segoePageFont = `
	html, body {
		margin: 0;
		padding: 0;
		background: #ffffff;
	}
	body {
		width: 640px;
		height: 520px;
		display: flex;
		flex-direction: column;
		gap: 12px;
		padding: 16px;
		box-sizing: border-box;
		/* No line-height pin: the initial normal (browser-parity D1)
		   is the rule under test — the record carries Chrome's
		   font-metrics line boxes. */
		font: 16px "Segoe UI", system-ui, sans-serif;
		color: #0f172a;
	}
`

// segoeRows is the Segoe fixture's row bodies: one span per size.
const segoeRows = `
	<div class="row" data-id="row-12"><span class="fill" data-id="span-12" style="font-size: 12px">Twelve point text</span></div>
	<div class="row" data-id="row-13"><span class="fill" data-id="span-13" style="font-size: 13px">Thirteen point text</span></div>
	<div class="row" data-id="row-16"><span class="fill" data-id="span-16" style="font-size: 16px">Sixteen point text</span></div>
	<div class="row" data-id="row-24"><span class="fill" data-id="span-24" style="font-size: 24px">Twenty-four</span></div>
	<div class="row" data-id="row-48"><span class="fill" data-id="span-48" style="font-size: 48px">Forty-eight</span></div>
	<div class="row" data-id="row-wrap"><span class="fill" data-id="span-wrap" style="font-size: 16px">` + wrapText + `</span></div>
	<div class="row" data-id="row-mixed"><span class="fill" data-id="span-mixed" style="font-size: 16px">` + mixedPrefix + `<span style="font-size: 24px">` + mixedBig + `</span>` + mixedSuffix + `</span></div>
	<div class="row" data-id="row-ratio"><span class="fill" data-id="span-ratio" style="font-size: 16px; line-height: 1.5">Explicit ratio 1.5</span></div>
`

// rowStyles is the shared CSS of the fixture pages: the body is the
// flex column, every row a bordered 380px box, the block spans' extents
// painted so Tier C can see where each span's box ends.
const rowStyles = `
	.row {
		box-sizing: border-box;
		width: 380px;
		padding: 8px;
		border: 1px solid #334155;
		background: #e2e8f0;
		display: flex;
	}
	.fill {
		box-sizing: border-box;
		background: #dbeafe;
		display: block;
		width: 100%;
	}
`

// page wraps the font CSS, the row styles and the row bodies in the
// full HTML page.
func page(fontCSS, rowsHTML string) string {
	return `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<style>` + fontCSS + rowStyles + `
</style>
</head>
<body data-id="root">
` + rowsHTML + `
</body>
</html>
`
}

// View is the gpui-go twin of the Segoe page: the shared root carries
// the DEFAULT text style (CSS normal line height — nothing is pinned,
// that is the fixture's point).
func View(_ *parity.EventLog) gpui.AnyElement {
	return view(false)
}

// WrongPhiView is the deliberately-wrong view: the golden-ratio line
// height — the reference's default, the port's pre-D1 default — on the
// whole tree, so every row's height is the fraction's, not the font's
// line spacing. It must fail the Tier A box comparison.
func WrongPhiView(_ *parity.EventLog) gpui.AnyElement {
	return view(true)
}

// view builds the fixture's tree; phi selects the wrong view.
func view(phi bool) gpui.AnyElement {
	root := gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Flex().
		FlexCol().
		Gap(gpui.Px(rowGap)).
		Bg(gpui.Rgb(pageBackground)).
		P(gpui.Px(pagePadding)).
		Font(gpui.FontDescriptor{Family: "Segoe UI", Weight: 400}).
		TextColor(gpui.Rgb(textColor))
	if phi {
		root = root.LineHeight(gpui.PhiLineHeight())
	}
	root = root.
		Child(sizeRow("row-12", "span-12", 12.0, "Twelve point text")).
		Child(sizeRow("row-13", "span-13", 13.0, "Thirteen point text")).
		Child(sizeRow("row-16", "span-16", 16.0, "Sixteen point text")).
		Child(sizeRow("row-24", "span-24", 24.0, "Twenty-four")).
		Child(sizeRow("row-48", "span-48", 48.0, "Forty-eight")).
		Child(sizeRow("row-wrap", "span-wrap", 16.0, wrapText)).
		Child(mixedRow()).
		Child(ratioRow())
	return root.IntoElement()
}

// sizeRow builds one font-size row: the block span pins the content
// width, its text sets the size, and the line height stays the ambient
// DEFAULT (CSS normal) — the page twin is the span's font-size style.
func sizeRow(rowID, spanID string, size float32, text string) *gpui.DivElement {
	return rowBox(rowID).
		Child(gpui.Div().
			ID(spanID).
			DebugSelector(spanID).
			Block().
			W(gpui.Px(spanWidth)).
			Bg(gpui.Rgb(spanBackground)).
			TextSize(gpui.Px(size)).
			Child(gpui.Text(text)))
}

// mixedRow builds the mixed row: a 16px paragraph carrying one 24px
// inline span ("one BIG tail") — the 24px strut's own normal line box
// grows the shared row. The page twin is the inline font-size span.
func mixedRow() *gpui.DivElement {
	return rowBox("row-mixed").
		Child(gpui.Div().
			ID("span-mixed").
			DebugSelector("span-mixed").
			Block().
			W(gpui.Px(spanWidth)).
			Bg(gpui.Rgb(spanBackground)).
			TextSize(gpui.Px(16.0)).
			Child(gpui.Text(mixedPrefix)).
			Child(gpui.Div().Inline().TextSize(gpui.Px(24.0)).Child(gpui.Text(mixedBig))).
			Child(gpui.Text(mixedSuffix)))
}

// ratioRow builds the explicit-ratio row: line-height 1.5 at 16px — a
// definite relative length, resolved the pin's way (24px), proving the
// non-normal values still resolve beside the new default.
func ratioRow() *gpui.DivElement {
	return rowBox("row-ratio").
		Child(gpui.Div().
			ID("span-ratio").
			DebugSelector("span-ratio").
			Block().
			W(gpui.Px(spanWidth)).
			Bg(gpui.Rgb(spanBackground)).
			TextSize(gpui.Px(16.0)).
			LineHeight(gpui.Fraction(1.5)).
			Child(gpui.Text("Explicit ratio 1.5")))
}

// rowBox builds the shared row: 380 wide, border 1, padding 8 (the
// whitespace fixture's shape).
func rowBox(id string) *gpui.DivElement {
	return gpui.Div().
		ID(id).
		DebugSelector(id).
		W(gpui.Px(rowWidth)).
		Flex().
		Border(gpui.Px(rowBorder)).
		BorderColor(gpui.Rgb(borderColor)).
		Bg(gpui.Rgb(rowBackground)).
		P(gpui.Px(rowPad))
}
