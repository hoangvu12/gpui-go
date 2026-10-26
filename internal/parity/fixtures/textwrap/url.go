//go:build windows

// Package textwrap holds ticket 43's browser-parity fixtures
// (.scratch/browser-core/issues/43-text-wrapping-and-alignment.md):
// word-break, overflow-wrap, text-wrap: balance and text-align's
// justify driving real line breaking and alignment, recorded against
// headless Chrome.
//
// The fixture shape follows the whitespace fixture (ticket 38): one
// 640-wide window, a flex column of 380-wide rows (8px padding, 1px
// border — rowContent 362px) with AUTO heights at a pinned 25px line
// height, Segoe UI 16px (the CJK fixture pins Microsoft YaHei). Every
// row's height is its line count, so the line-breaking fixtures
// discriminate at Tier A (the boxes); the balance and justify fixtures
// keep their line counts and discriminate at Tier C (the pixels),
// gated by their wrong-view tests.
//
// The Chromium rules these fixtures record (the research checkout,
// %TEMP%\research-src\chromium at 0d4dcd4f):
//
//   - overflow-wrap: anywhere / break-word are the emergency break
//     classes — a word that cannot fit breaks at an arbitrary point
//     (break-word differs from anywhere only in min-content sizing,
//     which a definite-width row cannot observe). word-break:
//     break-all makes every inter-character position a break
//     opportunity: the line breaker fills each row completely
//     (text_break_iterator.cc's break-all class table,
//     ShouldBreakAfterBreakAll:125-149). word-break: keep-all forbids
//     the CJK ideograph breaks normal allows.
//   - text-wrap: balance: ParagraphLineBreaker::
//     AttemptParagraphBalancing (paragraph_line_breaker.cc) — "find the
//     desired available width by bisecting the maximum available width
//     that produces num_lines" (BisectAvailableWidth:119-145), the
//     single-line guard (:188-190), kMaxLinesForBalance = 6
//     (score_line_break_context.h:17), the 1px epsilon (:199-202) and
//     the 0.8 × average lower bound (:226-228). The port's Go-side
//     bisection (gpui/text_balance.go) is the recorded contract.
//   - text-align: justify stretches the line's word spacing to the
//     line box edges, except the last line of the paragraph
//     (css-text §text-align: "in the case of justify, the lines'
//     contents are aligned to both edges, except the last line").
package textwrap

import (
	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The shared page numbers: one window, one column of rows. Every value
// is whole-pixel; the row boxes are derived from these and the LINE
// COUNT each wrapping mode produces.
const (
	windowWidth  = 640.0
	windowHeight = 400.0

	pagePadding = 16.0
	rowGap      = 12.0

	rowWidth  = 380.0
	rowPad    = 8.0
	rowBorder = 1.0
	// rowContent is the row's content width: 380 - 2*8 - 2*1.
	rowContent = rowWidth - 2*rowPad - 2*rowBorder

	textSize      = 16.0
	rowLineHeight = 25.0
)

// urlText is the long URL: no break opportunities at overflow-wrap
// normal (it overflows the 362px content box on one line) and plenty
// of emergency positions under the breaking classes.
const urlText = "https://example.com/browser-parity/fixtures/very/unbreakable/url/path/segments"

// cjkText is the CJK body (25 full-width ideographs ≈ 400px at 16px):
// word-break normal wraps it at the ideograph boundaries, keep-all
// keeps it one overflowing line. Plain ideographs only — parley's
// keep-all diverges from Chrome after U+3005 (the ideographic
// iteration mark), recorded in the ticket's answer.
const cjkText = "東京都渋谷区神南一丁目公園神宮外苑恵比寿広尾橋方向"

// balanceText is the balanced heading: three wrapped rows greedy, the
// same three balanced (every break far from a boundary).
const balanceText = "Chromium balances the narrowest width with the same line count"

// justifyText is the justified paragraph: four wrapped rows, the last
// one short (start-aligned).
const justifyText = "The quick brown fox jumps over the lazy dog and then the dog jumps back over the fox again"

// The palette, shared by the pages and the views.
const (
	pageBackground = 0xffffff
	rowBackground  = 0xe2e8f0
	spanBackground = 0xdbeafe
	borderColor    = 0x334155
	textColor      = 0x0f172a
)

// rowStyles is the shared CSS of the fixture pages.
const rowStyles = `
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
		gap: 12px;
		padding: 16px;
		box-sizing: border-box;
		/* The pinned 25px line height (the textstyle fixtures' rule). */
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

// ---------------------------------------------------------------------------
// textwrap-url: the long URL under the breaking classes
// ---------------------------------------------------------------------------

// URLFixture is the long-URL fixture: the URL at the breaking classes'
// defaults (one overflow line — no break opportunities), under
// overflow-wrap: anywhere and break-word (the emergency class), and
// under word-break: break-all (every position an opportunity).
var URLFixture = parity.Fixture{
	Name:   "textwrap-url",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   urlPage,
	View:   URLView,
	BoxIDs: []string{
		"root",
		"row-url",
		"span-url",
		"row-url-anywhere",
		"span-url-anywhere",
		"row-url-break-word",
		"span-url-break-word",
		"row-url-break-all",
		"span-url-break-all",
	},
	Tolerance: parity.PixelTolerance{
		// Provisional, set from the measured correct-view statistics and
		// verified by the wrong-view test (textwrap_test.go: the no-class
		// view fails Tier A). See docs/browser-parity-harness.md.
		MaxMeanChannelDiff:    0.4,
		MaxROIMeanChannelDiff: 1.5,
		MaxHotPixelFraction:   0.008,
	},
}

// urlPage is the HTML twin of URLView.
const urlPage = `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<style>` + rowStyles + `
</style>
</head>
<body data-id="root">
	<div class="row" data-id="row-url"><span class="fill" data-id="span-url" style="display: block; width: 100%">` + urlText + `</span></div>
	<div class="row" data-id="row-url-anywhere"><span class="fill" data-id="span-url-anywhere" style="display: block; width: 100%; overflow-wrap: anywhere">` + urlText + `</span></div>
	<div class="row" data-id="row-url-break-word"><span class="fill" data-id="span-url-break-word" style="display: block; width: 100%; overflow-wrap: break-word">` + urlText + `</span></div>
	<div class="row" data-id="row-url-break-all"><span class="fill" data-id="span-url-break-all" style="display: block; width: 100%; word-break: break-all">` + urlText + `</span></div>
</body>
</html>
`

// URLView is the gpui twin of urlPage.
func URLView(_ *parity.EventLog) gpui.AnyElement {
	return urlView(false)
}

// WrongURLView is the deliberately-wrong view: the breaking classes
// dropped — the port's exact pre-ticket behaviour, where every row
// keeps the one overflow line — which must fail the Tier A box
// comparison (the emergency/break-all rows' line counts).
func WrongURLView(_ *parity.EventLog) gpui.AnyElement {
	return urlView(true)
}

func urlView(wrong bool) gpui.AnyElement {
	// wrong drops the breaking classes — the port's pre-ticket rule.
	anywhere := func(e *gpui.DivElement) *gpui.DivElement { return e }
	breakWord := func(e *gpui.DivElement) *gpui.DivElement { return e }
	breakAll := func(e *gpui.DivElement) *gpui.DivElement { return e }
	if !wrong {
		anywhere = (*gpui.DivElement).WrapAnywhere
		breakWord = (*gpui.DivElement).WrapBreakWord
		breakAll = (*gpui.DivElement).BreakAll
	}
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
		TextColor(gpui.Rgb(textColor)).
		Child(urlParagraph("row-url", "span-url", urlText)).
		Child(anywhere(urlParagraph("row-url-anywhere", "span-url-anywhere", urlText))).
		Child(breakWord(urlParagraph("row-url-break-word", "span-url-break-word", urlText))).
		Child(breakAll(urlParagraph("row-url-break-all", "span-url-break-all", urlText)))
	return root.IntoElement()
}

// urlParagraph is the paragraph measure path (a Block div whose text
// child joins the inline paragraph; the page twin is the display:block
// span) carrying one breaking class.
func urlParagraph(rowID, spanID, text string) *gpui.DivElement {
	return gpui.Div().
		ID(rowID).
		DebugSelector(rowID).
		W(gpui.Px(rowWidth)).
		Flex().
		Border(gpui.Px(rowBorder)).
		BorderColor(gpui.Rgb(borderColor)).
		Bg(gpui.Rgb(rowBackground)).
		P(gpui.Px(rowPad)).
		Child(gpui.Div().
			ID(spanID).
			DebugSelector(spanID).
			Block().
			W(gpui.Px(rowContent)).
			Bg(gpui.Rgb(spanBackground)).
			Child(gpui.Text(text)))
}

func init() { parity.Register(URLFixture) }
