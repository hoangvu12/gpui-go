//go:build windows

// Package textpaint is ticket 08's text rasterization and italic
// fixtures for the browser-parity harness: the measured A/B of
// gpui-go's text rasterization against Chrome that decides whether
// gamma/contrast shader work (the spec's R17) is needed, plus the
// synthetic-italic slant probe.
//
//   - textpaint-raster: "Hamburg" in dark-on-light, light-on-dark and
//     grey at 12/16/24/32 px, recorded with the recorder's default
//     Chrome flags (whatever headless Chrome's text AA is on this
//     machine — the probe report measures and documents it).
//   - textpaint-raster-gray: the same page recorded with
//     --disable-lcd-text, Chrome's grayscale text mode, so the probe
//     can compare like with like against the gpui side's mode.
//   - textpaint-italic: Tahoma (a family with no italic face) upright
//     and italic, plus a Segoe UI italic control (a real italic face).
//     Chrome synthesizes the slant as a -0.25 x-shear
//     (font_platform_data_win.cc); gpui-go synthesizes it through
//     DirectWrite's oblique simulation (glyph.rs). The probe measures
//     the ink slant of both.
//
// Both raster fixtures replay the same view: the gpui side's mode is
// the platform recommendation (ClearType on/off by the OS setting),
// so the pair of records gives Chrome's two AA modes against gpui's
// one. The probe report (research/browser-parity/08-raster-probes)
// carries the numbers; these fixtures keep them as regression gates.
package textpaint

import (
	"strconv"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The numbers shared by the pages and the gpui views (the tracer's
// discipline: both sides are built from one set of constants).
const (
	windowWidth  = 640.0
	windowHeight = 400.0

	// The raster grid: three color columns x four size rows of fixed
	// 200x56 cells, 12px gaps. The line height is pinned to 1.618 (the
	// pin's default, before D1 lands) and the font to Segoe UI on both
	// sides (the page names it; the gpui view uses the default system
	// UI font, which is Segoe UI on this PC).
	cellWidth  = 200.0
	cellHeight = 56.0
	cellGap    = 12.0

	// The italic probe: fixed 480x64 rows, Tahoma at 32px (Tahoma has
	// no italic face, so both engines synthesize), a Segoe UI italic
	// control with a real italic face, and ten lowercase l's — plain
	// vertical stems whose slanted ink measures the shear.
	italicRowWidth  = 480.0
	italicRowHeight = 64.0
	italicGap       = 16.0
	italicText      = "llllllllll"
	italicSize      = 32.0

	sampleText = "Hamburg"
)

// The palette, shared by the pages and the views.
const (
	pageBackground = 0xffffff
	textDark       = 0x000000
	textLight      = 0xffffff
	textGrey       = 0x808080

	lightBackground = 0x000000
)

// sampleSizes are the probed font sizes, in logical px (2x device).
var sampleSizes = []float32{12, 16, 24, 32}

// sampleColumns are the probed color combinations, in column order:
// dark text on light, light text on dark, grey text on light.
type sampleColumn struct {
	id         string
	textHex    uint32
	background uint32
}

var sampleColumns = []sampleColumn{
	{id: "dark", textHex: textDark, background: pageBackground},
	{id: "light", textHex: textLight, background: lightBackground},
	{id: "grey", textHex: textGrey, background: pageBackground},
}

// RasterFixture is the rasterization A/B fixture under the recorder's
// default Chrome flags.
var RasterFixture = parity.Fixture{
	Name:   "textpaint-raster",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   rasterPage,
	View:   RasterView,
	BoxIDs: sampleBoxIDs(),
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run), set from
		// the measured correct-view statistics by the probe (see
		// research/browser-parity/08-raster-probes/README.md): whole mean
		// 0.4470 / hot 0.00554, worst ROI light-32 mean 1.5235 / hot
		// 0.02205 — the gates sit at 2x those. The deliberately-wrong
		// 1.5x-size view measures whole 4.0068 / worst ROI 16.1440 (7-9x
		// the gates), verified by textpaint_test.go.
		MaxMeanChannelDiff:    1.0,
		MaxROIMeanChannelDiff: 3.2,
		MaxHotPixelFraction:   0.05,
	},
}

// RasterGrayFixture is the same rasterization A/B recorded with
// Chrome's grayscale text mode (--disable-lcd-text), so the probe
// compares gpui's text against Chrome's grayscale rasterization.
var RasterGrayFixture = parity.Fixture{
	Name:        "textpaint-raster-gray",
	Window:      parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:        rasterPage,
	View:        RasterView,
	BoxIDs:      sampleBoxIDs(),
	ChromeFlags: []string{"--disable-lcd-text"},
	Tolerance: parity.PixelTolerance{
		// Set from the measured statistics by the probe: whole mean
		// 0.5858 / hot 0.00758, worst ROI light-24 mean 1.7428 / hot
		// 0.02502 — the gates sit at 2x those. The deliberately-wrong
		// 1.5x-size view measures whole 4.0197 / worst ROI 16.1440,
		// verified by textpaint_test.go.
		MaxMeanChannelDiff:    1.2,
		MaxROIMeanChannelDiff: 3.6,
		MaxHotPixelFraction:   0.055,
	},
}

// ItalicFixture is the synthetic-italic slant probe.
var ItalicFixture = parity.Fixture{
	Name:   "textpaint-italic",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   italicPage,
	View:   ItalicView,
	BoxIDs: []string{"tahoma-upright", "tahoma-italic", "segoe-italic"},
	Tolerance: parity.PixelTolerance{
		// Set from the measured statistics by the probe: whole mean
		// 0.3460 / hot 0.00494, worst ROI tahoma-italic mean 1.7671 /
		// hot 0.02340 — the gates sit at 2x those. The deliberately-wrong
		// M-stems view measures whole 1.5690 / worst ROI 6.5492 / hot
		// 0.08277, past every gate, verified by textpaint_test.go.
		MaxMeanChannelDiff:    0.75,
		MaxROIMeanChannelDiff: 3.7,
		MaxHotPixelFraction:   0.05,
	},
}

func init() {
	parity.Register(RasterFixture)
	parity.Register(RasterGrayFixture)
	parity.Register(ItalicFixture)
}

// sampleBoxIDs lists every sample cell's id, row-major (size rows,
// color columns), for the box comparisons and the probe's ROIs.
func sampleBoxIDs() []string {
	ids := make([]string, 0, len(sampleSizes)*len(sampleColumns))
	for _, size := range sampleSizes {
		for _, col := range sampleColumns {
			ids = append(ids, cellID(col, size))
		}
	}
	return ids
}

// sizeClass formats a size as its CSS class suffix (12 -> "12").
func sizeClass(size float32) string {
	return strconv.Itoa(int(size))
}

// cellID builds one sample cell's data-id: "dark-12" and so on.
func cellID(col sampleColumn, size float32) string {
	return col.id + "-" + sizeClass(size)
}

// rasterPage is the HTML twin of RasterView, built from the rows at
// package initialization (the object-fit fixture's pattern for a page
// assembled from data).
var rasterPage = `<!doctype html>
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
	.grid {
		display: flex;
		flex-direction: column;
		gap: 12px;
	}
	.row {
		display: flex;
		gap: 12px;
	}
	.cell {
		box-sizing: border-box;
		width: 200px;
		height: 56px;
		overflow: hidden;
		line-height: 1.618;
		font-family: "Segoe UI", system-ui, sans-serif;
	}
	.dark {
		background: #ffffff;
		color: #000000;
	}
	.light {
		background: #000000;
		color: #ffffff;
	}
	.grey {
		background: #ffffff;
		color: #808080;
	}
	.s12 { font-size: 12px; }
	.s16 { font-size: 16px; }
	.s24 { font-size: 24px; }
	.s32 { font-size: 32px; }
</style>
</head>
<body>
	<div class="grid">
` + rasterRowsHTML() + `	</div>
</body>
</html>
`

// rasterRowsHTML builds the grid's rows (the size rows, each with the
// three color cells).
func rasterRowsHTML() string {
	out := ""
	for _, size := range sampleSizes {
		out += "\t\t<div class=\"row\">\n"
		for _, col := range sampleColumns {
			out += "\t\t\t<div class=\"cell " + col.id + " s" + sizeClass(size) +
				"\" data-id=\"" + cellID(col, size) + "\">" + sampleText + "</div>\n"
		}
		out += "\t\t</div>\n"
	}
	return out
}

// RasterView is the gpui-go twin of rasterPage: the sample grid, every
// cell the same fixed box with the same background, text color, size
// and text as the page.
func RasterView(_ *parity.EventLog) gpui.AnyElement {
	return rasterRoot(1)
}

// RasterWrongSize is the deliberately-wrong view for the Tier C
// comparison: every cell draws its sample at 1.5x the size, so the
// ink mass and glyph shapes differ where the cell boxes stay
// identical (the fixed 200x56 cells keep Tier A exact).
func RasterWrongSize(_ *parity.EventLog) gpui.AnyElement {
	return rasterRoot(1.5)
}

// rasterRoot builds the raster fixture's root with a size multiplier
// applied to every sample (1 for the correct view).
func rasterRoot(sizeScale float32) gpui.AnyElement {
	root := gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Flex().
		Justify(gpui.AlignContentCenter).
		AlignItems(gpui.AlignItemsCenter).
		Bg(gpui.Rgb(pageBackground))
	grid := gpui.Div().
		ID("grid").
		DebugSelector("grid").
		FlexCol().
		Gap(gpui.Px(cellGap))
	for _, size := range sampleSizes {
		row := gpui.Div().
			ID("row-" + sizeClass(size)).
			DebugSelector("row-" + sizeClass(size)).
			Flex().
			Gap(gpui.Px(cellGap))
		for _, col := range sampleColumns {
			// The cell keeps its size-named id (the boxes must stay
			// comparable); only the rendered font size scales.
			row = row.Child(sampleCellAt(col, size, size*sizeScale))
		}
		grid = grid.Child(row)
	}
	return root.Child(grid).IntoElement()
}

// sampleCell builds one sample cell: a fixed 200x56 box with the
// column's background and text color at the size, holding the sample
// text.
func sampleCell(col sampleColumn, size float32) *gpui.DivElement {
	return sampleCellAt(col, size, size)
}

// sampleCellAt builds one sample cell whose id and geometry come from
// the base size while the rendered font size is explicit (the wrong
// view's lever).
func sampleCellAt(col sampleColumn, baseSize, renderSize float32) *gpui.DivElement {
	return gpui.Div().
		ID(cellID(col, baseSize)).
		DebugSelector(cellID(col, baseSize)).
		W(gpui.Px(cellWidth)).
		H(gpui.Px(cellHeight)).
		Bg(gpui.Rgb(col.background)).
		TextColor(gpui.Rgb(col.textHex)).
		TextSize(gpui.Px(renderSize)).
		Child(sampleText)
}

// ---------------------------------------------------------------------------
// The italic fixture
// ---------------------------------------------------------------------------

// italicPage is the HTML twin of ItalicView.
const italicPage = `<!doctype html>
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
	.rows {
		display: flex;
		flex-direction: column;
		gap: 16px;
	}
	.it {
		box-sizing: border-box;
		width: 480px;
		height: 64px;
		overflow: hidden;
		line-height: 1.618;
		font-size: 32px;
	}
	.tahoma {
		font-family: "Tahoma", sans-serif;
	}
	.segoe {
		font-family: "Segoe UI", system-ui, sans-serif;
	}
	.upright { font-style: normal; }
	.italic { font-style: italic; }
</style>
</head>
<body>
	<div class="rows">
		<div class="it tahoma upright" data-id="tahoma-upright">` + italicText + `</div>
		<div class="it tahoma italic" data-id="tahoma-italic">` + italicText + `</div>
		<div class="it segoe italic" data-id="segoe-italic">` + italicText + `</div>
	</div>
</body>
</html>
`

// ItalicView is the gpui-go twin of italicPage.
func ItalicView(_ *parity.EventLog) gpui.AnyElement {
	root := gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Flex().
		Justify(gpui.AlignContentCenter).
		AlignItems(gpui.AlignItemsCenter).
		Bg(gpui.Rgb(pageBackground))
	rows := gpui.Div().
		ID("rows").
		DebugSelector("rows").
		FlexCol().
		Gap(gpui.Px(italicGap))
	rows = rows.Child(italicRow("tahoma-upright", "Tahoma", gpui.FontStyleNormal))
	rows = rows.Child(italicRow("tahoma-italic", "Tahoma", gpui.FontStyleItalic))
	rows = rows.Child(italicRow("segoe-italic", ".SystemUIFont", gpui.FontStyleItalic))
	return root.Child(rows).IntoElement()
}

// ItalicWrongText is the deliberately-wrong view for the Tier C
// comparison: the italic rows draw dense M stems instead of the
// fixture's l stems (same families, styles and fixed rows), so the
// ink differs where the row boxes stay identical.
func ItalicWrongText(_ *parity.EventLog) gpui.AnyElement {
	root := gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Flex().
		Justify(gpui.AlignContentCenter).
		AlignItems(gpui.AlignItemsCenter).
		Bg(gpui.Rgb(pageBackground))
	rows := gpui.Div().
		ID("rows").
		DebugSelector("rows").
		FlexCol().
		Gap(gpui.Px(italicGap))
	rows = rows.Child(italicRowText("tahoma-upright", "Tahoma", gpui.FontStyleNormal, italicText))
	rows = rows.Child(italicRowText("tahoma-italic", "Tahoma", gpui.FontStyleItalic, "MMMMMMMMMM"))
	rows = rows.Child(italicRowText("segoe-italic", ".SystemUIFont", gpui.FontStyleItalic, "MMMMMMMMMM"))
	return root.Child(rows).IntoElement()
}

// italicRow builds one italic fixture row: a fixed 480x64 box holding
// the stems in the given family and style.
func italicRow(id, family string, style gpui.FontStyle) *gpui.DivElement {
	return italicRowText(id, family, style, italicText)
}

// italicRowText builds one italic fixture row with an explicit text
// (the wrong view's lever).
func italicRowText(id, family string, style gpui.FontStyle, text string) *gpui.DivElement {
	return gpui.Div().
		ID(id).
		DebugSelector(id).
		W(gpui.Px(italicRowWidth)).
		H(gpui.Px(italicRowHeight)).
		Font(gpui.FontDescriptor{Family: family, Weight: 400, Style: style}).
		TextSize(gpui.Px(italicSize)).
		Child(text)
}
