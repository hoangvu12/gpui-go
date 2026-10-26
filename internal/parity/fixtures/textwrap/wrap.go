//go:build windows

// The balance and justify fixtures. The balanced heading uses the
// measured-leaf path (a flex child Text element — the port's inline
// paragraph assembler does not balance; the recorded gap in the
// ticket's answer), the justified paragraph uses both the paragraph
// path (a Block div's single text child — the native justify gate) and
// the leaf path. Both fixtures keep their line counts under the
// classes, so Tier C (the pixels) is the signal and the wrong-view
// tests are the gates.
package textwrap

import (
	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// BalanceFixture is the balanced heading: the heading under text-wrap:
// balance (the bisection — the rows are as even as the break
// opportunities allow) and, as the control, the greedy wrap.
var BalanceFixture = parity.Fixture{
	Name:   "textwrap-balance",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   balancePage,
	View:   BalanceView,
	BoxIDs: []string{
		"root",
		"row-heading",
		"span-heading",
		"row-balanced",
		"span-balanced",
	},
	Tolerance: parity.PixelTolerance{
		// Provisional, set from the measured correct-view statistics and
		// verified by the wrong-view test (textwrap_test.go: the
		// unbalanced view fails Tier C). See docs/browser-parity-harness.md.
		MaxMeanChannelDiff:    0.2,
		MaxROIMeanChannelDiff: 0.9,
		MaxHotPixelFraction:   0.006,
	},
}

// balancePage is the HTML twin of BalanceView.
const balancePage = `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<style>` + rowStyles + `
</style>
</head>
<body data-id="root">
	<div class="row" data-id="row-heading"><span class="fill" data-id="span-heading" style="display: flex; width: 100%">` + balanceText + `</span></div>
	<div class="row" data-id="row-balanced"><span class="fill" data-id="span-balanced" style="display: flex; width: 100%; text-wrap: balance">` + balanceText + `</span></div>
</body>
</html>
`

// BalanceView is the gpui twin of balancePage (the text-leaf measure
// path — the flex child carries the text).
func BalanceView(_ *parity.EventLog) gpui.AnyElement {
	return balanceView(false)
}

// WrongBalanceView is the deliberately-wrong view: text-wrap: balance
// dropped — the port's pre-ticket rule, the greedy wrap — which must
// fail the Tier C pixel comparison (the rows keep the same line count
// under both wraps, so only the pixels move).
func WrongBalanceView(_ *parity.EventLog) gpui.AnyElement {
	return balanceView(true)
}

func balanceView(wrong bool) gpui.AnyElement {
	balanced := headingSpan("row-balanced", "span-balanced", balanceText)
	if !wrong {
		balanced = balanced.TextBalance()
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
		Child(headingSpan("row-heading", "span-heading", balanceText)).
		Child(balanced)
	return root.IntoElement()
}

// headingSpan is the measured-leaf path: the Text element directly as
// a flex child (gpui/div.go textElement). The page twin is the
// display:flex span whose anonymous flex item carries the text.
func headingSpan(rowID, spanID, text string) *gpui.DivElement {
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
			Flex().
			W(gpui.Px(rowContent)).
			Bg(gpui.Rgb(spanBackground)).
			Child(gpui.Text(text)))
}

// ---------------------------------------------------------------------------

// JustifyFixture is the justified paragraph: a left-aligned control,
// the justified paragraph through the inline-paragraph path (a Block
// div's single text child) and through the measured-leaf path.
var JustifyFixture = parity.Fixture{
	Name:   "textwrap-justify",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   justifyPage,
	View:   JustifyView,
	BoxIDs: []string{
		"root",
		"row-left",
		"span-left",
		"row-justify",
		"span-justify",
		"row-justify-leaf",
		"span-justify-leaf",
	},
	Tolerance: parity.PixelTolerance{
		// Provisional, set from the measured correct-view statistics at
		// ~2x each (the justified rows' stretched word spacing is the
		// signal; the tolerance stays generous for the sub-pixel
		// rounding of the distributed space) and verified by the
		// wrong-view test (textwrap_test.go: the unjustified view fails
		// Tier C). See docs/browser-parity-harness.md.
		MaxMeanChannelDiff:    0.25,
		MaxROIMeanChannelDiff: 1.3,
		MaxHotPixelFraction:   0.006,
	},
}

// justifyPage is the HTML twin of JustifyView.
const justifyPage = `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<style>` + rowStyles + `
</style>
</head>
<body data-id="root">
	<div class="row" data-id="row-left"><span class="fill" data-id="span-left" style="display: block; width: 100%">` + justifyText + `</span></div>
	<div class="row" data-id="row-justify"><span class="fill" data-id="span-justify" style="display: block; width: 100%; text-align: justify">` + justifyText + `</span></div>
	<div class="row" data-id="row-justify-leaf"><span class="fill" data-id="span-justify-leaf" style="display: flex; width: 100%; text-align: justify">` + justifyText + `</span></div>
</body>
</html>
`

// JustifyView is the gpui twin of justifyPage.
func JustifyView(_ *parity.EventLog) gpui.AnyElement {
	return justifyView(false)
}

// WrongJustifyView is the deliberately-wrong view: justify dropped —
// the port's pre-ticket rule (paint-time left alignment only) — which
// must fail the Tier C pixel comparison (the rows keep the same line
// counts, so only the stretched word spacing moves).
func WrongJustifyView(_ *parity.EventLog) gpui.AnyElement {
	return justifyView(true)
}

func justifyView(wrong bool) gpui.AnyElement {
	justify := func(e *gpui.DivElement) *gpui.DivElement { return e }
	if !wrong {
		justify = (*gpui.DivElement).TextJustify
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
		Child(justifyParagraph("row-left", "span-left", justifyText)).
		Child(justify(urlParagraph("row-justify", "span-justify", justifyText))).
		Child(justify(headingSpan("row-justify-leaf", "span-justify-leaf", justifyText)))
	return root.IntoElement()
}

// justifyParagraph is the left-aligned control's paragraph path.
func justifyParagraph(rowID, spanID, text string) *gpui.DivElement {
	return urlParagraph(rowID, spanID, text)
}

func init() {
	parity.Register(BalanceFixture)
	parity.Register(JustifyFixture)
}
