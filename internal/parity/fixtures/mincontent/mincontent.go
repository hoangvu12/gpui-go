//go:build windows

// Package mincontent holds ticket 32's browser-parity fixtures
// (.scratch/browser-core/issues/32-text-min-content-wraps.md, spec D2):
// text min-content wraps at every break opportunity, so text in a flex
// row shrinks and wraps instead of overflowing, and the text measure
// cache is keyed by sizing mode.
//
// Chromium's rule this fixture records (the research checkout,
// %TEMP%\research-src\chromium): InlineNode::ComputeMinMaxSizes
// computes the min-content by breaking lines with ZERO available inline
// size — "the max of inline sizes of all line boxes with 0 available
// inline size. This gives the min-content, the width where lines wrap at
// every break opportunity" (core/layout/inline/inline_node.cc:2314-2323,
// ComputeContentSize's kMinContent arm returning LayoutUnit() at
// 1966-1967, the per-line max at 2265; LineBreakerMode at
// core/layout/inline/line_breaker.h:37). Min-content is therefore the
// widest unbreakable segment (the longest word), never the full
// unwrapped advance.
//
// The fixture is one 640x480 page of five fixed-width flex rows (the
// tracer's discipline: integral boxes only — every compared box is
// whole-CSS-pixel, set by the row's fixed width and the wrapped line
// count, never by a shaped text advance):
//
//   - row-block: a block span of text in a flex row — the paragraph
//     measure path (gpui Div().Block() + Text, the inline paragraph).
//   - row-leaf: the text directly as a flex child — the measured text
//     leaf path (gpui Div().Flex() + Text; the page twin is a
//     display:flex span, whose anonymous flex item carries the same
//     min-content).
//   - row-styled: a StyledText as a flex child — the rich-text measure
//     path (gpui Div().Flex() + NewStyledText).
//   - row-shrink: a fixed flex-shrink: 0 box plus a text span: the span
//     shrinks to the leftover content width and wraps there (the
//     flex-shrink case; the port's pre-D2 behaviour leaves the span at
//     its full max-content width and overflows).
//   - row-longword: one unbreakable word wider than the row: the
//     automatic minimum size (flexbox §4.5) clamps the span at the
//     word's own width, which is exactly the min-content floor. The
//     span carries NO data-id: its box width is the word's shaped
//     advance, which is not layout-integral; the row box (one line
//     tall) is the Tier A pin and the span's painted extent is Tier C.
//
// Every row's text is chosen so each wrap point sits at least 40px from
// the boundary (words of 11-13 chars, three per line at 362px and two
// per line at 262px), so Chrome's and the port's shaped advances cannot
// disagree about a line break.
//
// The deliberately-wrong view (WrongNowwrapView) puts white-space: nowrap
// on the whole tree: min-content equals max-content, nothing shrinks and
// nothing wraps — the port's exact pre-ticket behaviour, which must fail
// the Tier A box comparison (the wrong-view test in mincontent_test.go).
package mincontent

import (
	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The shared page numbers: one window, one column of rows. Every value
// is whole-pixel and every box the comparison reads is derived from
// these, never from a shaped advance.
const (
	windowWidth  = 640.0
	windowHeight = 480.0

	pagePadding = 16.0
	rowGap      = 12.0

	rowWidth  = 380.0
	rowPad    = 8.0
	rowBorder = 1.0
	// rowContent is the row's content width: 380 - 2*8 - 2*1. The wrap
	// rows' text shrinks to exactly this; the longword row overflows it.
	rowContent = rowWidth - 2*rowPad - 2*rowBorder

	shrinkFixedWidth  = 100.0
	shrinkFixedHeight = 25.0
	// The shrink row's text span shrinks to the leftover content
	// width: rowContent - shrinkFixedWidth = 262.

	textSize      = 16.0
	rowLineHeight = 25.0
)

// The wrap and shrink texts: 11-13 char words, three per line at 362px
// and two per line at 262px (every break point at least 40px from the
// boundary, so both shapers agree on the line count).
const (
	wrapText   = "consequences determination possibilities extraordinary architecture mathematics"
	shrinkText = "consequences determination possibilities extraordinary architecture mathematics"
	longWord   = "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
)

// The palette, shared by the page and the view.
const (
	pageBackground = 0xffffff
	rowBackground  = 0xe2e8f0
	spanBackground = 0xdbeafe
	fixedRed       = 0xfca5a5
	borderColor    = 0x334155
	textColor      = 0x0f172a
)

// Fixture is the min-content wrap fixture.
var Fixture = parity.Fixture{
	Name:   "mincontent-wrap",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   page,
	View:   View,
	BoxIDs: []string{
		"root",
		"row-block",
		"wrap-block",
		"row-leaf",
		"wrap-leaf",
		"row-styled",
		"wrap-styled",
		"row-shrink",
		"shrink-fixed",
		"shrink-text",
		"row-longword",
	},
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run), set from
		// the measured correct-view statistics (TestPixelTolerancesKeep
		// Headroom reports whole mean 0.179 / hot 0.0008, the worst ROI
		// row-longword 0.94 / 0.008 — the 60-glyph unbreakable word's
		// cumulative glyph-phase drift, the text-heavy fixtures' known
		// raster noise) at ~2x each, and verified by mincontent_test.go's
		// wrong-view check (the nowrap view measures ~10x above). See
		// docs/browser-parity-harness.md.
		MaxMeanChannelDiff:    0.36,
		MaxROIMeanChannelDiff: 1.9,
		MaxHotPixelFraction:   0.016,
	},
}

func init() { parity.Register(Fixture) }

// rowStyles is the shared CSS of the page: the body is the flex column,
// every row a fixed-width bordered flex box, the text spans' extents
// painted so Tier C can see where each span's box ends.
const rowStyles = `
	html, body {
		margin: 0;
		padding: 0;
		background: #ffffff;
	}
	body {
		width: 640px;
		height: 480px;
		display: flex;
		flex-direction: column;
		gap: 12px;
		padding: 16px;
		box-sizing: border-box;
		/* The pinned 25px line height (the textstyle fixtures' rule:
		   the two sides' baseline placement agrees at 25px). */
		font: 16px/25px "Segoe UI", system-ui, sans-serif;
		color: #0f172a;
	}
	.row {
		box-sizing: border-box;
		width: 380px;
		padding: 8px;
		border: 1px solid #334155;
		background: #e2e8f0;
		/* The pin's default display, pinned on the page too. */
		display: flex;
	}
	.fill {
		box-sizing: border-box;
		background: #dbeafe;
	}
	.fixed {
		box-sizing: border-box;
		width: 100px;
		height: 25px;
		flex-shrink: 0;
		background: #fca5a5;
	}
`

// page is the HTML twin of View.
const page = `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<style>` + rowStyles + `
</style>
</head>
<body data-id="root">
	<div class="row" data-id="row-block"><span class="fill" data-id="wrap-block">` + wrapText + `</span></div>
	<div class="row" data-id="row-leaf"><span class="fill" style="display: flex" data-id="wrap-leaf">` + wrapText + `</span></div>
	<div class="row" data-id="row-styled"><span class="fill" style="display: flex" data-id="wrap-styled">` + wrapText + `</span></div>
	<div class="row" data-id="row-shrink"><div class="fixed" data-id="shrink-fixed"></div><span class="fill" data-id="shrink-text">` + shrinkText + `</span></div>
	<div class="row" data-id="row-longword"><span class="fill">` + longWord + `</span></div>
</body>
</html>
`

// View is the gpui twin of the page.
func View(_ *parity.EventLog) gpui.AnyElement {
	return view(false)
}

// WrongNowwrapView is the deliberately-wrong view: white-space: nowrap
// on the whole tree, so min-content equals max-content, nothing shrinks
// and nothing wraps — the port's pre-ticket behaviour (the wrong-view
// test must fail Tier A with it).
func WrongNowwrapView(_ *parity.EventLog) gpui.AnyElement {
	return view(true)
}

// view builds the fixture's tree; nowrap selects the wrong view.
func view(nowrap bool) gpui.AnyElement {
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
	if nowrap {
		root = root.WhitespaceNowrap()
	}
	root = root.
		Child(paragraphSpan("row-block", "wrap-block")).
		Child(textLeafSpan("row-leaf", "wrap-leaf")).
		Child(styledTextSpan("row-styled", "wrap-styled")).
		Child(shrinkRow()).
		Child(longwordRow())
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

// paragraphSpan is the block-span case: a Block div whose text child
// joins the inline paragraph (the paragraph measure path). The page
// twin is the plain span, blockified by the flex row.
func paragraphSpan(rowID, spanID string) *gpui.DivElement {
	return rowBox(rowID).
		Child(gpui.Div().
			ID(spanID).
			DebugSelector(spanID).
			Block().
			Bg(gpui.Rgb(spanBackground)).
			Child(gpui.Text(wrapText)))
}

// textLeafSpan is the measured-leaf case: the Text element directly as
// a flex child (the textElement measure path). The page twin is the
// display:flex span, whose anonymous flex item carries the same
// min-content.
func textLeafSpan(rowID, spanID string) *gpui.DivElement {
	return rowBox(rowID).
		Child(gpui.Div().
			ID(spanID).
			DebugSelector(spanID).
			Flex().
			Bg(gpui.Rgb(spanBackground)).
			Child(gpui.Text(wrapText)))
}

// styledTextSpan is the rich-text measure path: a StyledText element
// directly as a flex child (Base's text-block shape).
func styledTextSpan(rowID, spanID string) *gpui.DivElement {
	return rowBox(rowID).
		Child(gpui.Div().
			ID(spanID).
			DebugSelector(spanID).
			Flex().
			Bg(gpui.Rgb(spanBackground)).
			Child(gpui.NewStyledText(wrapText)))
}

// shrinkRow is the flex-shrink case: a fixed flex-shrink: 0 box, then a
// block span of text that shrinks to the leftover content width and
// wraps there.
func shrinkRow() *gpui.DivElement {
	return rowBox("row-shrink").
		Child(gpui.Div().Flex().
			ID("shrink-fixed").
			DebugSelector("shrink-fixed").
			W(gpui.Px(shrinkFixedWidth)).
			H(gpui.Px(shrinkFixedHeight)).
			FlexShrink0().
			Bg(gpui.Rgb(fixedRed))).
		Child(gpui.Div().
			ID("shrink-text").
			DebugSelector("shrink-text").
			Block().
			Bg(gpui.Rgb(spanBackground)).
			Child(gpui.Text(shrinkText)))
}

// longwordRow is the min-content floor: one unbreakable word wider than
// the row, so the span's automatic minimum size (its min-content) keeps
// it at the word's own width, overflowing the row. The span carries no
// selector: its width is the word's shaped advance (not integral); the
// row box (one line tall) is the Tier A pin, the painted extent Tier C.
func longwordRow() *gpui.DivElement {
	return rowBox("row-longword").
		Child(gpui.Div().
			Block().
			Bg(gpui.Rgb(spanBackground)).
			Child(gpui.Text(longWord)))
}
