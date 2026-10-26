//go:build windows

// Package gridlayout holds ticket 42's browser-parity fixtures
// (.scratch/browser-core/issues/42-grid-baselines-flow-root.md): the
// full grid track lists (grid-template-columns: 240px 1fr), the named
// grid areas (grid-template-areas + grid-area) and baseline alignment
// of mixed font sizes in flex and grid rows, recorded against headless
// Chrome.
//
// The fixture shape follows the textwrap fixture (ticket 43): one
// 640x400 window, a flex column of grids under 16px page padding, Segoe
// UI 16px/40px, plain backgrounds (the boxes carry the discrimination).
// The baseline fixture's numbers are the Chrome-measured strut math:
// line-height 40 with Segoe UI's FIXED (rounded) metrics places the
// first baselines at 25 (12px), 26.5 (16px) and 30 (24px), so the
// baseline-aligned rows put the 12px and 16px items 5px and 3.5px below
// the 24px one, and both rows grow to 45 (max baseline 30 + the 12px
// item's 15 below it).
//
// The Chromium rules these fixtures record (the research checkout,
// %TEMP%\research-src\chromium at 0d4dcd4f):
//
//   - The grid track list `240px 1fr` sizes the fixed track and gives
//     the rest to the fr (css-grid-1 §7.2.3: a bare size is the minmax
//     pair with itself on both sides; a bare fr is minmax(auto, fr)).
//   - grid-template-areas names rectangles of the explicit grid and
//     grid-area: <name> resolves through the implicit <name>-start /
//     <name>-end lines (§8.3-8.4; Chromium's GridTrackSizingAlgorithm
//     placement).
//   - Baseline alignment uses each item's first line baseline
//     (css-flexbox-1 §8.3, css-grid-1 §11.5.1; the strut is the font's
//     FixedAscent/FixedDescent with the even half-leading split,
//     inline_box_state.cc:182/202 and line_utils.cc
//     CalculateLeadingSpace).
package gridlayout

import (
	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The shared page numbers: one window, one column of grids.
const (
	windowWidth  = 640.0
	windowHeight = 400.0

	pagePadding = 16.0
	pageGap     = 12.0

	textSize      = 16.0
	rowLineHeight = 40.0
)

// The palette, shared by the pages and the views.
const (
	pageBackground = 0xffffff
	gridBackground = 0xe2e8f0
	cellBackground = 0xdbeafe
	textColor      = 0x0f172a
)

// gridStyles is the shared CSS of the fixture pages.
const gridStyles = `
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
		font: 16px/40px "Segoe UI", system-ui, sans-serif;
		color: #0f172a;
	}
	.grid {
		box-sizing: border-box;
		display: grid;
		grid-template-columns: 240px 1fr;
		background: #e2e8f0;
	}
	.cell {
		box-sizing: border-box;
		background: #dbeafe;
	}
`

// ---------------------------------------------------------------------------
// gridtracks: grid-template-columns: 240px 1fr with auto rows
// ---------------------------------------------------------------------------

// TracksFixture is the track-list fixture: a 460px grid with `240px
// 1fr` columns and auto rows (the row is the tallest item in it), plus
// a 360px grid whose fr resolves to 120.
var TracksFixture = parity.Fixture{
	Name:   "gridtracks",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   tracksPage,
	View:   TracksView,
	BoxIDs: []string{
		"root",
		"grid-a",
		"a1", "a2", "a3", "a4",
		"grid-b",
		"b1", "b2",
	},
	Tolerance: parity.PixelTolerance{
		// Provisional, set from the measured correct-view statistics and
		// verified by the wrong-view test (gridlayout_test.go: the
		// pinned repeat form fails Tier A). See
		// docs/browser-parity-harness.md.
		MaxMeanChannelDiff:    0.4,
		MaxROIMeanChannelDiff: 1.5,
		MaxHotPixelFraction:   0.008,
	},
}

// tracksPage is the HTML twin of TracksView.
const tracksPage = `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<style>` + gridStyles + `
</style>
</head>
<body data-id="root">
	<div class="grid" data-id="grid-a" style="width: 460px">
		<div class="cell" data-id="a1" style="height: 40px"></div>
		<div class="cell" data-id="a2" style="height: 60px"></div>
		<div class="cell" data-id="a3" style="height: 40px"></div>
		<div class="cell" data-id="a4" style="height: 40px"></div>
	</div>
	<div class="grid" data-id="grid-b" style="width: 360px">
		<div class="cell" data-id="b1" style="height: 40px"></div>
		<div class="cell" data-id="b2" style="height: 40px"></div>
	</div>
</body>
</html>
`

// TracksView is the gpui twin of tracksPage.
func TracksView(_ *parity.EventLog) gpui.AnyElement {
	return tracksView(false)
}

// WrongTracksView is the deliberately-wrong view: the pinned repeat
// form (repeat(2, minmax(0, 1fr))) — the port's exact pre-ticket
// surface, where every column is an equal fr — which must fail the
// Tier A box comparison (the 240px fixed track becomes 230px).
func WrongTracksView(_ *parity.EventLog) gpui.AnyElement {
	return tracksView(true)
}

func tracksView(wrong bool) gpui.AnyElement {
	applyCols := func(g *gpui.DivElement) *gpui.DivElement {
		if wrong {
			// The pre-ticket pin: repeat(2, minmax(0, 1fr)) — two equal
			// fr columns.
			return g.GridCols(2)
		}
		return g.GridColsList(gpui.PxTrack(240), gpui.FrTrack(1))
	}
	root := gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Flex().
		FlexCol().
		Gap(gpui.Px(pageGap)).
		Bg(gpui.Rgb(pageBackground)).
		P(gpui.Px(pagePadding)).
		TextSize(gpui.Px(textSize)).
		LineHeight(gpui.Px(rowLineHeight)).
		TextColor(gpui.Rgb(textColor)).
		Child(applyCols(grid("grid-a", 460,
			cell("a1", 40), cell("a2", 60), cell("a3", 40), cell("a4", 40)))).
		Child(applyCols(grid("grid-b", 360, cell("b1", 40), cell("b2", 40))))
	return root.IntoElement()
}

// grid builds one track-list grid with its cells.
func grid(id string, width float32, cells ...*gpui.DivElement) *gpui.DivElement {
	g := gpui.Div().
		ID(id).
		DebugSelector(id).
		Grid().
		W(gpui.Px(width)).
		Bg(gpui.Rgb(gridBackground))
	for _, c := range cells {
		g = g.Child(c)
	}
	return g
}

// cell builds one grid cell of the given height.
func cell(id string, height float32) *gpui.DivElement {
	return gpui.Div().
		ID(id).
		DebugSelector(id).
		H(gpui.Px(height)).
		Bg(gpui.Rgb(cellBackground))
}

func init() { parity.Register(TracksFixture) }

// ---------------------------------------------------------------------------
// gridareas: grid-template-areas + grid-area
// ---------------------------------------------------------------------------

// AreasFixture is the named-area fixture: a 300px grid with
// grid-template-areas: "head head" "side main" over `120px 1fr`
// columns — head spans both columns of row 1, side and main split row
// 2.
var AreasFixture = parity.Fixture{
	Name:   "gridareas",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   areasPage,
	View:   AreasView,
	BoxIDs: []string{
		"root",
		"areas",
		"head", "side", "main",
	},
	Tolerance: parity.PixelTolerance{
		MaxMeanChannelDiff:    0.4,
		MaxROIMeanChannelDiff: 1.5,
		MaxHotPixelFraction:   0.008,
	},
}

// areasPage is the HTML twin of AreasView.
const areasPage = `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<style>` + gridStyles + `
	.areas {
		box-sizing: border-box;
		display: grid;
		grid-template-columns: 120px 1fr;
		grid-template-areas: "head head" "side main";
		width: 300px;
		background: #e2e8f0;
	}
	.areas .cell { box-sizing: border-box; background: #dbeafe; }
</style>
</head>
<body data-id="root">
	<div class="areas" data-id="areas">
		<div class="cell" data-id="head" style="grid-area: head; height: 30px"></div>
		<div class="cell" data-id="side" style="grid-area: side; height: 50px"></div>
		<div class="cell" data-id="main" style="grid-area: main; height: 50px"></div>
	</div>
</body>
</html>
`

// AreasView is the gpui twin of areasPage.
func AreasView(_ *parity.EventLog) gpui.AnyElement {
	return areasView(false)
}

// WrongAreasView is the deliberately-wrong view: the named placements
// dropped (auto placement) — the port's pre-ticket behaviour, where
// grid-area had no surface — which must fail Tier A (side and main
// move to row 1 and row 2 column 1).
func WrongAreasView(_ *parity.EventLog) gpui.AnyElement {
	return areasView(true)
}

func areasView(wrong bool) gpui.AnyElement {
	area := func(name string) func(*gpui.DivElement) *gpui.DivElement {
		return func(e *gpui.DivElement) *gpui.DivElement {
			if wrong {
				return e
			}
			return e.GridArea(name)
		}
	}
	root := gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Flex().
		FlexCol().
		Gap(gpui.Px(pageGap)).
		Bg(gpui.Rgb(pageBackground)).
		P(gpui.Px(pagePadding)).
		TextSize(gpui.Px(textSize)).
		LineHeight(gpui.Px(rowLineHeight)).
		TextColor(gpui.Rgb(textColor)).
		Child(
			gpui.Div().
				ID("areas").
				DebugSelector("areas").
				Grid().
				W(gpui.Px(300)).
				Bg(gpui.Rgb(gridBackground)).
				GridColsList(gpui.PxTrack(120), gpui.FrTrack(1)).
				GridTemplateAreas("head head", "side main").
				Child(area("head")(areaCell("head", 30))).
				Child(area("side")(areaCell("side", 50))).
				Child(area("main")(areaCell("main", 50))))
	return root.IntoElement()
}

// areaCell builds one named-area cell of the given height.
func areaCell(id string, height float32) *gpui.DivElement {
	return gpui.Div().
		ID(id).
		DebugSelector(id).
		H(gpui.Px(height)).
		Bg(gpui.Rgb(cellBackground))
}

func init() { parity.Register(AreasFixture) }

// ---------------------------------------------------------------------------
// gridbaseline: baseline-aligned mixed font sizes in flex and grid rows
// ---------------------------------------------------------------------------

// BaselineFixture is the baseline fixture: a flex row and a grid row of
// 12/16/24px text spans (line-height 40) under align-items: baseline —
// the rows' boxes and the items' offsets are the strut math above.
var BaselineFixture = parity.Fixture{
	Name:   "gridbaseline",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   baselinePage,
	View:   BaselineView,
	BoxIDs: []string{
		"root",
		"flexrow",
		"flex-small", "flex-medium", "flex-large",
		"gridrow",
		"grid-small", "grid-medium", "grid-large",
	},
	Tolerance: parity.PixelTolerance{
		// Calibrated from the measured correct-view statistics at 2× (the
		// harness rule): the medium items' ink carries the paint path's
		// known sub-pixel drift — the glyphs paint at the pin's
		// raw-metric line baseline while Chrome paints at the strut's
		// fixed-metric one, up to ~0.6px at the leading-split floor —
		// which Tier A's byte-exact boxes pin down; the tolerance covers
		// that ink drift, and the wrong-view test still fails Tier A on
		// the vanished 5px/4px offsets.
		MaxMeanChannelDiff:    0.2,
		MaxROIMeanChannelDiff: 5.0,
		MaxHotPixelFraction:   0.07,
	},
}

// baselinePage is the HTML twin of BaselineView.
const baselinePage = `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<style>` + gridStyles + `
	.baserow {
		box-sizing: border-box;
		align-items: baseline;
		background: #e2e8f0;
	}
	#flexrow {
		display: flex;
		width: 300px;
	}
	#gridrow {
		display: grid;
		grid-template-columns: 120px 120px 120px;
		width: 360px;
	}
	.baserow span {
		box-sizing: border-box;
		width: 60px;
		background: #dbeafe;
	}
	.s12 { font-size: 12px; }
	.s16 { font-size: 16px; }
	.s24 { font-size: 24px; }
</style>
</head>
<body data-id="root">
	<div class="baserow" data-id="flexrow" id="flexrow">
		<span class="s12" data-id="flex-small">small</span>
		<span class="s16" data-id="flex-medium">medium</span>
		<span class="s24" data-id="flex-large">large</span>
	</div>
	<div class="baserow" data-id="gridrow" id="gridrow">
		<span class="s12" data-id="grid-small">small</span>
		<span class="s16" data-id="grid-medium">medium</span>
		<span class="s24" data-id="grid-large">large</span>
	</div>
</body>
</html>
`

// BaselineView is the gpui twin of baselinePage.
func BaselineView(_ *parity.EventLog) gpui.AnyElement {
	return baselineView(false)
}

// WrongBaselineView is the deliberately-wrong view: the baseline
// alignment dropped (flex-start) — the pre-ticket pin's effective
// behaviour, whose height-fallback baseline equals the bottom edge, so
// equal-height items do not move — which must fail Tier A (the 5px and
// 3.5px offsets vanish).
func WrongBaselineView(_ *parity.EventLog) gpui.AnyElement {
	return baselineView(true)
}

func baselineView(wrong bool) gpui.AnyElement {
	align := func(e *gpui.DivElement) *gpui.DivElement {
		if wrong {
			return e.ItemsStart()
		}
		return e.ItemsBaseline()
	}
	flexRow := align(gpui.Div().
		ID("flexrow").
		DebugSelector("flexrow").
		Flex().
		W(gpui.Px(300)).
		Bg(gpui.Rgb(gridBackground)).
		Child(baselineSpan("flex-small", "small", 12)).
		Child(baselineSpan("flex-medium", "medium", 16)).
		Child(baselineSpan("flex-large", "large", 24)))
	gridRow := align(gpui.Div().
		ID("gridrow").
		DebugSelector("gridrow").
		Grid().
		W(gpui.Px(360)).
		Bg(gpui.Rgb(gridBackground)).
		GridColsList(gpui.PxTrack(120), gpui.PxTrack(120), gpui.PxTrack(120)).
		Child(baselineSpan("grid-small", "small", 12)).
		Child(baselineSpan("grid-medium", "medium", 16)).
		Child(baselineSpan("grid-large", "large", 24)))
	root := gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Flex().
		FlexCol().
		Gap(gpui.Px(pageGap)).
		Bg(gpui.Rgb(pageBackground)).
		P(gpui.Px(pagePadding)).
		TextSize(gpui.Px(textSize)).
		LineHeight(gpui.Px(rowLineHeight)).
		TextColor(gpui.Rgb(textColor)).
		Child(flexRow).
		Child(gridRow)
	return root.IntoElement()
}

// baselineSpan builds one baseline-row item: a 60px-wide block whose
// text is one line at the given font size under the row's 40px line
// height (the page twin's span).
func baselineSpan(id, text string, fontSize float32) *gpui.DivElement {
	return gpui.Div().
		ID(id).
		DebugSelector(id).
		W(gpui.Px(60)).
		Bg(gpui.Rgb(cellBackground)).
		TextSize(gpui.Px(fontSize)).
		LineHeight(gpui.Px(rowLineHeight)).
		Child(gpui.Text(text))
}

func init() { parity.Register(BaselineFixture) }
