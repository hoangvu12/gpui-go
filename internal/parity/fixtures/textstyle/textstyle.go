//go:build windows

// Package textstyle holds ticket 06's browser-parity fixtures
// (.scratch/browser-core/issues/06-text-style-quick-fixes.md): one
// fixture per ticket item — text-transform, letter-spacing ligatures,
// Chrome's underline geometry, line-clamp ellipsis, tabular-nums and
// the generic families. The grayscale-antialiasing item is a
// scene/glyph spec test instead (internal/glyphspec/
// text_aa_spec_test.go and gpui's hidden-window test): the effect is a
// paint-mode selection, not layout or pixels.
//
// Every fixture shares the same shape (the tracer's conventions,
// docs/browser-parity-harness.md): a 640x400 white page, a flex column
// of fixed-size rows, Segoe UI text at a pinned line-height of 1.618
// (the pin's default until D1 lands — both sides pin it explicitly),
// and no input sequence (the items under test are static). The row
// boxes are compared exactly (Tier A); the pixels carry each item's
// visual difference (Tier C).
package textstyle

import (
	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The shared page numbers: one window, one column of rows.
const (
	windowWidth  = 640.0
	windowHeight = 400.0

	pagePadding = 16.0
	rowGap      = 12.0

	rowWidth  = 380.0
	rowHeight = 44.0
	rowPad    = 8.0
	rowBorder = 1.0

	pageBackground = 0xffffff
	rowBackground  = 0xe2e8f0
	borderColor    = 0x334155
	textColor      = 0x0f172a

	textSize        = 16.0
	rowLineHeight   = 25.0
	lineHeightRatio = 1.618
)

// rowStyles is the shared CSS of every fixture page: the body is the
// flex column, every row a fixed border-box with the same look.
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
		/* The line height is 25px, not the 1.618 default (25.888 -> 26):
		   the two sides' baseline placement rules differ subpixel-wise, and
		   25px is the value where they agree. Chrome snaps each line box's
		   baseline DOWN to a whole CSS pixel (measured: floor of
		   9 + (L - 21)/2 + 17 with the rounded Win32 metrics of
		   platform/fonts/font_metrics.cc:113-114 — Segoe UI 16px: 28),
		   while the pin computes the fractional half-leading baseline
		   (9 + (25 - 21.28)/2 + 17.27 = 28.125) and quantizes each glyph
		   origin to whole device pixels (round(2 x 28.125) = 56 = 2 x 28:
		   the same row). At L=26 the pin quantizes one device row below
		   Chrome's floored baseline and every glyph edge shifts a pixel.
		   Fonts whose own metric rounding breaks the agreement carry a
		   per-row line-height override (families.go). */
		font: 16px/25px "Segoe UI", system-ui, sans-serif;
		color: #0f172a;
	}
	.row {
		box-sizing: border-box;
		width: 380px;
		height: 44px;
		padding: 8px;
		border: 1px solid #334155;
		background: #e2e8f0;
		color: #0f172a;
	}
`

// page wraps the row styles and the row bodies in the full HTML page.
func page(extraStyles, rowsHTML string) string {
	return `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<style>` + rowStyles + extraStyles + `
</style>
</head>
<body data-id="root">
` + rowsHTML + `
</body>
</html>
`
}

// fixture builds the shared Fixture shape: no steps, the root and the
// rows compared exactly, and a provisional Tier C gate the fixture
// file tunes from the measured numbers.
func fixture(name, html string, view func(*parity.EventLog) gpui.AnyElement, boxIDs []string, tolerance parity.PixelTolerance) parity.Fixture {
	return parity.Fixture{
		Name:      name,
		Window:    parity.Size{Width: windowWidth, Height: windowHeight},
		HTML:      html,
		View:      view,
		BoxIDs:    append([]string{"root"}, boxIDs...),
		Tolerance: tolerance,
	}
}

// rowElement builds one gpui row div: the shared box look, the extra
// style application, and the row's text.
func rowElement(id string, text string, style func(*gpui.DivElement) *gpui.DivElement) *gpui.DivElement {
	div := gpui.Div().
		ID(id).
		DebugSelector(id).
		// A flex COLUMN, like Chrome's block rows: the text child
		// stretches to the row's content width (align-items stretch is
		// the default) and so wraps at the row width. (Divergence note:
		// a flex row would also wrap since browser-core ticket 32 — D2
		// made text min-content the longest word, so a flex row now
		// shrinks and wraps it — but these fixtures predate it and keep
		// the column shape they were recorded with; the mincontent
		// fixture is the flex-row case.)
		FlexCol().
		Size(gpui.Px(rowWidth), gpui.Px(rowHeight)).
		Bg(gpui.Rgb(rowBackground)).
		P(gpui.Px(rowPad)).
		Border(gpui.Px(rowBorder)).
		BorderColor(gpui.Rgb(borderColor)).
		TextColor(gpui.Rgb(textColor)).
		TextSize(gpui.Px(textSize)).
		// The pinned 25px line height matches the page's (rowStyles'
		// comment).
		LineHeight(gpui.Px(rowLineHeight))
	if style != nil {
		div = style(div)
	}
	return div.Child(text)
}

// rootElement builds the shared root: the white flex column with the
// page padding and the row gap.
func rootElement(rows ...*gpui.DivElement) gpui.AnyElement {
	root := gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Flex().
		FlexCol().
		Gap(gpui.Px(rowGap)).
		Bg(gpui.Rgb(pageBackground)).
		P(gpui.Px(pagePadding))
	for _, row := range rows {
		root = root.Child(row)
	}
	return root.IntoElement()
}
