//go:build windows

// Package whitespace holds ticket 38's browser-parity fixtures
// (.scratch/browser-core/issues/38-white-space-and-tracking.md, spec
// story 22 "white-space collapsing (normal, nowrap, pre-line)" and
// story 28 "tab stops of 8 spaces under pre"): runs of spaces,
// newlines and tabs under every white-space value, collapsing across
// inline element boundaries, the empty paragraph, and em letter
// spacing.
//
// The rules this fixture records (the research checkout,
// %TEMP%\research-src\chromium — the full citation list lives in
// gpui/white_space.go):
//
//   - The mode matrix of core/css/white_space.h:47-108: normal/nowrap
//     collapse; pre/pre-wrap preserve; pre-line collapses spaces but
//     preserves newlines (AppendCollapseWhitespace /
//     AppendPreserveWhitespace / AppendPreserveNewline,
//     core/layout/inline/inline_items_builder.cc:798-965, 1052-1175).
//   - A run of collapsible spaces (space, tab, newline, carriage
//     return) folds to ONE space, except a leading run at the
//     paragraph start or after a preserved newline, and the trailing
//     run at a paragraph's end (ExitBlock,
//     inline_items_builder.cc:1624-1638) — so "Alpha   beta\n\ngamma"
//     under normal renders "Alpha beta gamma" on one line, and
//     everything-whitespace renders nothing at all (the empty line's
//     self-collapsing block, inline_layout_algorithm.cc:1488-1492).
//   - The collapse crosses inline element boundaries
//     (LastItemToCollapseWith over the collected items,
//     inline_items_builder.cc:218-228): "Alpha   " + " beta" is one
//     kept space, not four.
//   - pre keeps tabs, and a tab advances to the next multiple of 8 ×
//     (the space advance + letter spacing) from the row start
//     (platform/text/tab_size.h:34-38 with the CSS default TabSize(8),
//     core/css/css_properties.json5's tab-size default, and
//     platform/fonts/font.cc:319-337 Font::TabWidth).
//   - letter-spacing in ems resolves against the element's own font
//     size (core/css/resolver/style_builder_converter.cc:2821-2830
//     ConvertSpacing → ConvertLength).
//
// The fixture shape follows the mincontent fixture (ticket 32): one
// 640-wide window, a flex column of 380-wide rows with 1px borders and
// 8px padding, Segoe UI at a pinned 25px line height, and AUTO row
// heights — the row and span boxes are content-driven, so every
// line-count error (the collapse collapsing too much or too little)
// moves a Tier A box. The texts are chosen so every line is far from
// the wrap width: the collapse rows render one ~120px line in a 362px
// span, and the pre-wrap row wraps only at the unambiguous 3-word
// boundary of the mincontent fixture's 13-14 char words.
//
// The deliberately-wrong view (WrongPreserveView) puts white-space: pre
// on the whole tree — the port's exact pre-ticket behaviour, where
// nothing collapses and every newline renders its own row — which must
// fail the Tier A box comparison (the wrong-view test in
// whitespace_test.go).
package whitespace

import (
	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The shared page numbers: one window, one column of rows. Every value
// is whole-pixel; every box the comparison reads is derived from these
// and the LINE COUNT the white-space mode produces.
const (
	windowWidth  = 640.0
	windowHeight = 350.0

	pagePadding = 16.0
	rowGap      = 12.0

	rowWidth  = 380.0
	rowPad    = 8.0
	rowBorder = 1.0
	// rowContent is the row's content width: 380 - 2*8 - 2*1.
	rowContent = rowWidth - 2*rowPad - 2*rowBorder

	textSize      = 16.0
	rowLineHeight = 25.0

	// trackingEm is the letter spacing of the tracking row (Tailwind's
	// tracking-widest, 0.1em).
	trackingEm = 0.1
)

// collapseText is the normal/nowrap/pre-line body: a run of three
// spaces, two newlines and a tab that normal folds to "Alpha beta
// gamma" (one line), pre-line to "Alpha beta\n\ngamma" (three lines)
// and pre to the verbatim five lines.
const collapseText = "Alpha   beta\n\ngamma"

// preLineText is the pre-line body: the spaces before the newline are
// removed and the line after the break starts at "gamma" (two lines).
const preLineText = "Alpha   beta\ngamma"

// preText is the pre body: two lines whose tabs advance to the 8-space
// stops (the tab widths are the Tier C signal; the row's height is the
// line count).
const preText = "Alpha\tbeta\ngamma\t\tdelta"

// preWrapText is the pre-wrap body: the six 13-14 char words of the
// mincontent fixture (three per 362px row — every break point at least
// 40px from the boundary) wrap to two rows, then the preserved newline
// breaks to the zeta row: three lines.
const preWrapText = "consequences determination possibilities extraordinary architecture mathematics\nzeta"

// emptyText is the all-whitespace body: normal removes every line box
// (the empty paragraph, inline_layout_algorithm.cc:1488-1492); pre
// keeps its rows.
const emptyText = "   \n\t "

// trackingText is the em-tracked body (one line; the tracking is the
// Tier C signal).
const trackingText = "Alpha beta gamma"

// The palette, shared by the page and the view.
const (
	pageBackground = 0xffffff
	rowBackground  = 0xe2e8f0
	spanBackground = 0xdbeafe
	borderColor    = 0x334155
	textColor      = 0x0f172a
)

// CollapseFixture is the white-space collapse fixture: the paragraph,
// text-leaf and StyledText paths under normal, the cross-boundary
// collapse of two inline spans, pre-line's preserved breaks, and the
// emptied paragraph.
var CollapseFixture = parity.Fixture{
	Name:   "whitespace-collapse",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   collapsePage,
	View:   View,
	BoxIDs: []string{
		"root",
		"row-normal",
		"span-normal",
		"row-leaf",
		"span-leaf",
		"row-styled",
		"span-styled",
		"row-spans",
		"span-spans",
		"row-preline",
		"span-preline",
		"row-empty",
		"span-empty",
	},
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run), set from
		// the measured correct-view statistics (whole 0.0478 / worst ROI
		// span-normal 0.2068 / hot 0.00135) at ~2x each, and verified by
		// the wrong-view test (whitespace_test.go: the pre view measures
		// ~15x above). See docs/browser-parity-harness.md.
		MaxMeanChannelDiff:    0.1,
		MaxROIMeanChannelDiff: 0.42,
		MaxHotPixelFraction:   0.003,
	},
}

func init() { parity.Register(CollapseFixture) }

// rowStyles is the shared CSS of the fixture pages: the body is the
// flex column, every row a bordered 380px box, the text spans'
// extents painted so Tier C can see where each span's box ends.
const rowStyles = `
	html, body {
		margin: 0;
		padding: 0;
		background: #ffffff;
	}
	body {
		width: 640px;
		height: 350px;
		display: flex;
		flex-direction: column;
		gap: 12px;
		padding: 16px;
		box-sizing: border-box;
		/* The pinned 25px line height (the textstyle fixtures' rule:
		   the two sides' baseline placement agree at 25px). */
		font: 16px/25px "Segoe UI", system-ui, sans-serif;
		color: #0f172a;
	}
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
	}
`

// collapsePage is the HTML twin of View.
const collapsePage = `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<style>` + rowStyles + `
</style>
</head>
<body data-id="root">
	<div class="row" data-id="row-normal"><span class="fill" data-id="span-normal" style="display: block; width: 100%; white-space: normal">` + collapseText + `</span></div>
	<div class="row" data-id="row-leaf"><span class="fill" data-id="span-leaf" style="display: flex; width: 100%; white-space: normal">` + collapseText + `</span></div>
	<div class="row" data-id="row-styled"><span class="fill" data-id="span-styled" style="display: flex; width: 100%; white-space: normal">` + collapseText + `</span></div>
	<div class="row" data-id="row-spans"><span class="fill" data-id="span-spans" style="display: block; width: 100%; white-space: normal"><span>Alpha   </span><span> beta</span></span></div>
	<div class="row" data-id="row-preline"><span class="fill" data-id="span-preline" style="display: block; width: 100%; white-space: pre-line">` + preLineText + `</span></div>
	<div class="row" data-id="row-empty"><span class="fill" data-id="span-empty" style="display: block; width: 100%; white-space: normal">` + emptyText + `</span></div>
</body>
</html>
`

// View is the gpui twin of the collapse page.
func View(_ *parity.EventLog) gpui.AnyElement {
	return view(false)
}

// WrongPreserveView is the deliberately-wrong view: white-space: pre on
// the whole tree, so nothing collapses and every newline renders its
// own row — the port's exact pre-ticket behaviour, which must fail the
// Tier A box comparison (the line counts move every row).
func WrongPreserveView(_ *parity.EventLog) gpui.AnyElement {
	return view(true)
}

// view builds the fixture's tree; preserve selects the wrong view.
func view(preserve bool) gpui.AnyElement {
	root := gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Flex().
		FlexCol().
		Gap(gpui.Px(rowGap)).
		Bg(gpui.Rgb(pageBackground)).
		P(gpui.Px(pagePadding)).
		TextSize(gpui.Px(textSize)).
		LineHeight(gpui.Px(rowLineHeight)).
		TextColor(gpui.Rgb(textColor))
	if preserve {
		root = root.WhitespacePre()
	}
	root = root.
		Child(paragraphSpan("row-normal", "span-normal", collapseText)).
		Child(textLeafSpan("row-leaf", "span-leaf", collapseText)).
		Child(styledTextSpan("row-styled", "span-styled", collapseText)).
		Child(spansRow()).
		Child(paragraphSpan("row-preline", "span-preline", preLineText).WhitespacePreLine()).
		Child(paragraphSpan("row-empty", "span-empty", emptyText))
	return root.IntoElement()
}

// rowBox builds the shared row: 380 wide, border 1, padding 8, the
// pinned text style (the spans and text inherit it).
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

// paragraphSpan is the paragraph measure path: a Block div whose text
// child joins the inline paragraph (gpui/inline.go
// collapseParagraphDocument). The page twin is the display: block span.
func paragraphSpan(rowID, spanID, text string) *gpui.DivElement {
	return rowBox(rowID).
		Child(gpui.Div().
			ID(spanID).
			DebugSelector(spanID).
			Block().
			W(gpui.Px(rowContent)).
			Bg(gpui.Rgb(spanBackground)).
			Child(gpui.Text(text)))
}

// textLeafSpan is the measured-leaf path: the Text element directly as
// a flex child (gpui/div.go textElement). The page twin is the
// display: flex span, whose anonymous flex item carries the same text.
func textLeafSpan(rowID, spanID, text string) *gpui.DivElement {
	return rowBox(rowID).
		Child(gpui.Div().
			ID(spanID).
			DebugSelector(spanID).
			Flex().
			W(gpui.Px(rowContent)).
			Bg(gpui.Rgb(spanBackground)).
			Child(gpui.Text(text)))
}

// styledTextSpan is the rich-text measure path: a StyledText element
// directly as a flex child (gpui/styled_text.go).
func styledTextSpan(rowID, spanID, text string) *gpui.DivElement {
	return rowBox(rowID).
		Child(gpui.Div().
			ID(spanID).
			DebugSelector(spanID).
			Flex().
			W(gpui.Px(rowContent)).
			Bg(gpui.Rgb(spanBackground)).
			Child(gpui.NewStyledText(text)))
}

// spansRow is the cross-boundary collapse: a Block paragraph whose two
// INLINE spans hold "Alpha   " and " beta" — the space run crosses the
// element boundary and folds to one kept space (the collector's
// cross-record state, like Chromium's per-item collapsing). The
// inline spans carry no data-id: their auto widths are the shaped
// advance of "Alpha " and " beta", which is not layout-integral; the
// block span's box and the pixels pin the collapse.
func spansRow() *gpui.DivElement {
	return rowBox("row-spans").
		Child(gpui.Div().
			ID("span-spans").
			DebugSelector("span-spans").
			Block().
			W(gpui.Px(rowContent)).
			Bg(gpui.Rgb(spanBackground)).
			Child(gpui.Div().Inline().Child(gpui.Text("Alpha   "))).
			Child(gpui.Div().Inline().Child(gpui.Text(" beta"))))
}
