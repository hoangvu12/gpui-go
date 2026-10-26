//go:build windows

// The CJK fixture: word-break keep-all versus normal over full-width
// ideographs. The text and both sides pin Microsoft YaHei (the CJK
// face both Chrome on Windows and the port's chain resolve for it —
// the fixture must not depend on fallback selection).
package textwrap

import (
	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// CJKFixture is the CJK fixture: 28 ideographs (≈448px) under
// word-break normal (the ideograph boundaries wrap it) and keep-all
// (one overflow line).
var CJKFixture = parity.Fixture{
	Name:   "textwrap-cjk",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   cjkPage,
	View:   CJKView,
	BoxIDs: []string{
		"root",
		"row-cjk",
		"span-cjk",
		"row-cjk-keepall",
		"span-cjk-keepall",
	},
	Tolerance: parity.PixelTolerance{
		// Provisional, set from the measured correct-view statistics and
		// verified by the wrong-view test (textwrap_test.go: the
		// keep-all-less view fails Tier A).
		MaxMeanChannelDiff:    0.2,
		MaxROIMeanChannelDiff: 1.8,
		MaxHotPixelFraction:   0.006,
	},
}

// cjkStyles extends the shared rows with the pinned CJK family.
const cjkStyles = rowStyles + `
	body, .row .fill {
		font-family: "Microsoft YaHei", "Segoe UI", system-ui, sans-serif;
	}
`

// cjkPage is the HTML twin of CJKView.
const cjkPage = `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<style>` + cjkStyles + `
</style>
</head>
<body data-id="root">
	<div class="row" data-id="row-cjk"><span class="fill" data-id="span-cjk" style="display: block; width: 100%">` + cjkText + `</span></div>
	<div class="row" data-id="row-cjk-keepall"><span class="fill" data-id="span-cjk-keepall" style="display: block; width: 100%; word-break: keep-all">` + cjkText + `</span></div>
</body>
</html>
`

// CJKView is the gpui twin of cjkPage.
func CJKView(_ *parity.EventLog) gpui.AnyElement {
	return cjkView(false)
}

// WrongCJKView is the deliberately-wrong view: keep-all dropped — the
// port's pre-ticket rule, where the CJK text wraps at the ideograph
// boundaries either way — which must fail the Tier A box comparison
// (the keep-all row's line count).
func WrongCJKView(_ *parity.EventLog) gpui.AnyElement {
	return cjkView(true)
}

func cjkView(wrong bool) gpui.AnyElement {
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
		FontFamily("Microsoft YaHei").
		TextColor(gpui.Rgb(textColor)).
		Child(cjkParagraph("row-cjk", "span-cjk", cjkText))
	if !wrong {
		root = root.Child(cjkParagraph("row-cjk-keepall", "span-cjk-keepall", cjkText).BreakKeep())
	} else {
		root = root.Child(cjkParagraph("row-cjk-keepall", "span-cjk-keepall", cjkText))
	}
	return root.IntoElement()
}

func cjkParagraph(rowID, spanID, text string) *gpui.DivElement {
	return gpui.Div().
		ID(rowID).
		DebugSelector(rowID).
		W(gpui.Px(rowWidth)).
		Flex().
		Border(gpui.Px(rowBorder)).
		BorderColor(gpui.Rgb(borderColor)).
		Bg(gpui.Rgb(rowBackground)).
		P(gpui.Px(rowPad)).
		FontFamily("Microsoft YaHei").
		Child(gpui.Div().
			ID(spanID).
			DebugSelector(spanID).
			Block().
			W(gpui.Px(rowContent)).
			Bg(gpui.Rgb(spanBackground)).
			Child(gpui.Text(text)))
}

func init() { parity.Register(CJKFixture) }
