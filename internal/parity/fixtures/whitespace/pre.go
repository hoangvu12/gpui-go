//go:build windows

// The pre-mode fixture (ticket 38): tabs under pre advance to the
// browser's 8-space stops, pre-wrap keeps the newlines while wrapping
// at the span width, and em letter spacing widens the text (the
// tracking row's Tier C signal; the package comment in collapse.go
// carries the citation list).
package whitespace

import (
	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// PreFixture is the pre/pre-wrap/tracking fixture.
var PreFixture = parity.Fixture{
	Name:   "whitespace-pre",
	Window: parity.Size{Width: windowWidth, Height: 260.0},
	HTML:   prePage,
	View:   PreView,
	BoxIDs: []string{
		"root",
		"row-pre",
		"span-pre",
		"row-prewrap",
		"span-prewrap",
		"row-tracking",
		"span-tracking",
	},
	Tolerance: parity.PixelTolerance{
		// Provisional, set from the measured correct-view statistics
		// (whole 0.0935 / worst ROI span-prewrap 0.3382 / hot 0.00142)
		// at ~2x each, and verified by the wrong-view test
		// (whitespace_test.go). The pre rows carry the tab-stop and
		// preserved-space glyphs, so their ROIs run richer than the
		// collapse fixture's.
		MaxMeanChannelDiff:    0.19,
		MaxROIMeanChannelDiff: 0.68,
		MaxHotPixelFraction:   0.003,
	},
}

func init() { parity.Register(PreFixture) }

// prePage is the HTML twin of PreView: the pre row with two tabbed
// lines, the pre-wrap row whose six long words wrap to two rows before
// the preserved newline's third, and the 0.1em-tracked row.
const prePage = `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<style>` + rowStyles + `
	body {
		height: 260px;
	}
</style>
</head>
<body data-id="root">
	<div class="row" data-id="row-pre"><span class="fill" data-id="span-pre" style="display: block; width: 100%; white-space: pre">` + preText + `</span></div>
	<div class="row" data-id="row-prewrap"><span class="fill" data-id="span-prewrap" style="display: block; width: 100%; white-space: pre-wrap">` + preWrapText + `</span></div>
	<div class="row" data-id="row-tracking"><span class="fill" data-id="span-tracking" style="display: flex; width: 100%; white-space: normal; letter-spacing: 0.1em">` + trackingText + `</span></div>
</body>
</html>
`

// PreView is the gpui twin of the pre page.
func PreView(_ *parity.EventLog) gpui.AnyElement {
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
		// The pre row: preserved text, no soft wrap, tabs at the
		// 8-space stops (gpui/text_tabs.go's browser mode).
		Child(paragraphSpan("row-pre", "span-pre", preText).WhitespacePre()).
		// The pre-wrap row: preserved text, wrapping at the span width.
		Child(paragraphSpan("row-prewrap", "span-prewrap", preWrapText).WhitespacePreWrap()).
		// The tracking row: 0.1em letter spacing resolves against the
		// row's own 16px font size (gpui/div.go
		// resolveLetterSpacing).
		Child(textLeafSpan("row-tracking", "span-tracking", trackingText).LetterSpacingEm(trackingEm))
	return root.IntoElement()
}
