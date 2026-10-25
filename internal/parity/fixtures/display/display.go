//go:build windows

// Package display is ticket 36's fixture for the display modes and the
// overflow: auto policy (spec stories 12 and 14): display: contents
// splices the element's children into the parent's layout (no box of
// its own, its inert box styles staying set), inline-block and
// inline-grid participate in a block parent's paragraph as atomic
// inline-level boxes, and overflow: auto scrollers show a scrollbar —
// reserving its classic gutter — only while their content overflows,
// with scrollbar-gutter: stable reserving the gutter even while
// nothing shows. The pages are text-free geometry (the boxpaint
// discipline), so Tier C carries no font rasterization noise; the
// inline boxes are all taller than the paragraph strut so their
// baseline positions do not depend on font metrics.
//
// Chromium computes the recorded behaviour:
//
//   - display: contents generates no LayoutObject
//     (LayoutObject::CreateObject's switch, core/layout/
//     layout_object.cc:430-432) and the tree builder splices the
//     children into the grandparent's child list
//     (NextLayoutSiblingInternal / NextLayoutSibling, core/dom/
//     layout_tree_builder_traversal.cc:604-634);
//   - inline-block and inline-grid are atomic inline-level boxes whose
//     contents use block flow / grid (EDisplay::kInlineBlock ->
//     LayoutBlockFlow, layout_object.cc:437-442; kInlineGrid ->
//     LayoutGrid, :470-472);
//   - an overflow: auto axis shows a scrollbar exactly while it
//     overflows (PaintLayerScrollableArea::ComputeScrollbarExistence,
//     core/paint/paint_layer_scrollable_area.cc:1852-1861), a change
//     relayouts (UpdateAfterOverflowRecalc -> SetNeedsLayout-
//     AndFullPaintInvalidation, :1480-1500), the scrollbar rect spans
//     the border box's inline-end column inside the border
//     (RectForVerticalScrollbar, :1625-1650), and scrollbar-gutter:
//     stable reserves the thickness even unshown (LayoutBox::
//     HasScrollbarGutters / ComputeScrollbarsInternal, core/layout/
//     layout_box.cc:1477-1560).
package display

import (
	"fmt"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The pages' numbers, shared by the HTML and the gpui views (the
// tracer's discipline: one set of constants builds both sides).
const (
	windowWidth  = 640.0
	windowHeight = 400.0

	// The display page: the contents row (a flex row with 10px gaps
	// holding four 40x30 children, the middle two spliced through a
	// contents element with inert box styles), the block section (one
	// row of three 30px-tall atomic boxes: inline-block 40, inline-block
	// 50, inline-grid 90 with two 45px columns), and the nested
	// contents column (contents(contents(B)) + an empty contents + D).
	boxW    = 40.0
	boxH    = 30.0
	row2W   = 50.0
	gridW   = 90.0
	gridCol = 45.0
	colGap  = 10.0

	// The overflow page: three 180x150 vertical scrollers over 40px
	// content rows (the fitting one, the gutter-stable one and the
	// overflowing one with 570px of filler), and a 560x100 horizontal
	// scroller over 1200px of content. The scrollers' children stretch
	// to the content width (the flex column's cross axis), so the
	// reserved gutter narrows them.
	scW    = 180.0
	scH    = 150.0
	rowH   = 40.0
	fillH  = 570.0
	hScW   = 560.0
	hScH   = 100.0
	hFillW = 1200.0
	hFillH = 60.0
)

// The palette, shared by the pages and the views.
const (
	pageBackground = 0xffffff
	sectionBg      = 0xf1f5f9
	contentsRed    = 0xdc2626
	ibBlue         = 0x2563eb
	ibGreen        = 0x16a34a
	gridPurple     = 0x7c3aed
	scrollerBg     = 0xf1f5f9
	fitsBg         = 0x93c5fd
	hScFillBg      = 0xe2e8f0
	hFillBg        = 0xcbd5e1
)

// tolerance is the flat-color pixel gate: text-free geometry, so the
// correct view is pixel-exact up to the scrollbar's rounded thumb and
// antialiased edges (measured; see display_test.go). Provisional
// (research 19's calibration has not run). See
// docs/browser-parity-harness.md.
var tolerance = parity.PixelTolerance{
	MaxMeanChannelDiff:    0.02,
	MaxROIMeanChannelDiff: 0.1,
	MaxHotPixelFraction:   0.002,
}

// ModesFixture is the display: contents / inline-block / inline-grid
// fixture: the spliced row, the atomic inline boxes in a block parent,
// and the nested contents column.
var ModesFixture = parity.Fixture{
	Name:   "display-modes",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   modesPage,
	View:   modesView,
	BoxIDs: []string{
		"root", "contents-row", "a", "b", "c", "d",
		"block", "ib1", "ib2", "ig", "g1", "g2",
		"nest", "nb", "nc", "nd",
	},
	Tolerance: tolerance,
}

// OverflowFixture is the overflow: auto fixture: a fitting scroller
// (no scrollbar, no reservation), a gutter-stable fitting scroller
// (reserved, nothing shown), an overflowing vertical scroller (the
// scrollbar shows and the children narrow by its width) and an
// overflowing horizontal scroller (the children shrink in height).
var OverflowFixture = parity.Fixture{
	Name:   "overflow-auto",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   overflowPage,
	View:   overflowView,
	BoxIDs: []string{
		"root",
		"sc-fits", "fits-row",
		"sc-stable", "stable-row",
		"sc-overflow", "over-row", "over-fill",
		"sc-h", "h-fill",
	},
	// The scrollbar pair's antialiased thumb ends and arrow glyph edges
	// leave a small residual against Chrome's rasterization (the
	// measured correct-view statistics: 0.0141 image mean, 0.0725 on
	// the overflowing scroller's ROI, 0.00092 hot); each gate sits at
	// least 2x those. Verified by display_test.go and
	// TestPixelTolerancesKeepHeadroom.
	Tolerance: parity.PixelTolerance{
		MaxMeanChannelDiff:    0.03,
		MaxROIMeanChannelDiff: 0.15,
		MaxHotPixelFraction:   0.002,
	},
}

func init() {
	parity.Register(ModesFixture)
	parity.Register(OverflowFixture)
}

// ---------------------------------------------------------------------------
// The display-modes page
// ---------------------------------------------------------------------------

// modesPage is the display modes' HTML twin.
var modesPage = `<!doctype html>
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
		width: ` + f2s(windowWidth) + `px;
		height: ` + f2s(windowHeight) + `px;
		/* The pin's default display, pinned on the page too. */
		display: flex;
		flex-direction: column;
		align-items: flex-start;
	}
	#contents-row {
		box-sizing: border-box;
		width: ` + f2s(4*boxW+3*colGap) + `px;
		height: ` + f2s(boxH) + `px;
		margin-left: 20px;
		margin-top: 20px;
		display: flex;
		gap: ` + f2s(colGap) + `px;
		background: #f1f5f9;
	}
	#contents-row > div {
		flex-shrink: 0;
		box-sizing: border-box;
	}
	.contents {
		/* The behaviour under test: no box, the children splice into
		   the row. Box styles stay set to pin that they are inert. */
		display: contents;
		background: #dc2626;
		width: 999px;
		height: 999px;
		padding: 8px;
	}
	#a, #b, #c, #d {
		width: ` + f2s(boxW) + `px;
		height: ` + f2s(boxH) + `px;
		background: #2563eb;
	}
	#block {
		box-sizing: border-box;
		width: ` + f2s(4*boxW+3*colGap) + `px;
		height: ` + f2s(boxH) + `px;
		/* The block parent whose paragraph collects the atomic boxes:
		   display: block, the browser's default since browser-core 71,
		   stated explicitly (the view pins it the same way). The fixed
		   height pins the flow below it: the line box's strut (invisible,
		   no glyphs) extends past it without moving any compared box or
		   painted pixel. */
		display: block;
		margin-left: 20px;
		margin-top: 20px;
		background: #f1f5f9;
	}
	#block > div {
		box-sizing: border-box;
	}
	#ib1 {
		/* The behaviour under test: an atomic inline-level box with
		   block flow inside. */
		display: inline-block;
		width: ` + f2s(boxW) + `px;
		height: ` + f2s(boxH) + `px;
		background: #2563eb;
	}
	#ib2 {
		display: inline-block;
		width: ` + f2s(row2W) + `px;
		height: ` + f2s(boxH) + `px;
		background: #16a34a;
	}
	#ig {
		/* The behaviour under test: an atomic inline-level box with
		   grid inside. */
		display: inline-grid;
		width: ` + f2s(gridW) + `px;
		height: ` + f2s(boxH) + `px;
		grid-template-columns: repeat(2, minmax(0px, 1fr));
		background: #7c3aed;
	}
	#ig > div {
		box-sizing: border-box;
	}
	#g1, #g2 {
		width: ` + f2s(gridCol) + `px;
		height: ` + f2s(boxH) + `px;
		background: #d97706;
	}
	#nest {
		box-sizing: border-box;
		width: ` + f2s(boxW) + `px;
		/* The nested case: contents(contents(#nb)), an empty contents
		   element (no box, no space) and #nd stack as the column's own
		   children. */
		display: flex;
		flex-direction: column;
		margin-left: 20px;
		margin-top: 20px;
	}
	#nest > div {
		box-sizing: border-box;
	}
	#nb, #nc, #nd {
		width: ` + f2s(boxW) + `px;
		height: ` + f2s(boxH) + `px;
		background: #16a34a;
	}
</style>
</head>
<body data-id="root">
	<div id="contents-row" data-id="contents-row">
		<div id="a" data-id="a"></div>
		<div class="contents" data-id="contents">
			<div id="b" data-id="b"></div>
			<div id="c" data-id="c"></div>
		</div>
		<div id="d" data-id="d"></div>
	</div>
	<div id="block" data-id="block">
		<div id="ib1" data-id="ib1"></div><div id="ib2" data-id="ib2"></div><div id="ig" data-id="ig">
			<div id="g1" data-id="g1"></div>
			<div id="g2" data-id="g2"></div>
		</div>
	</div>
	<div id="nest" data-id="nest">
		<div style="display: contents">
			<div style="display: contents">
				<div id="nb" data-id="nb"></div>
			</div>
			<div id="nc" data-id="nc"></div>
		</div>
		<div style="display: contents"></div>
		<div id="nd" data-id="nd"></div>
	</div>
</body>
</html>
`

// modesView is the display modes' gpui twin.
func modesView(_ *parity.EventLog) gpui.AnyElement {
	return modesRoot(true)
}

// WrongModesView is the deliberately-wrong display view: every contents
// element left as a plain flex box — the pre-ticket
// pin, whose boxes take space in the row.
func WrongModesView(_ *parity.EventLog) gpui.AnyElement {
	return modesRoot(false)
}

// modesRoot builds the display page's tree; contentsElements selects
// the correct view (display: contents) or the wrong one (a plain flex
// box, the pre-ticket pin's contents).
func modesRoot(contentsElements bool) gpui.AnyElement {
	root := gpui.Div().Flex().
		ID("root").
		DebugSelector("root").
		SizeFull().
		FlexCol().
		AlignItems(gpui.AlignItemsFlexStart).
		Bg(gpui.Rgb(pageBackground))
	row := gpui.Div().
		ID("contents-row").
		DebugSelector("contents-row").
		Size(gpui.Px(4*boxW+3*colGap), gpui.Px(boxH)).
		Ml(gpui.Px(20)).
		Mt(gpui.Px(20)).
		Flex().
		Gap(gpui.Px(colGap)).
		Bg(gpui.Rgb(sectionBg)).
		Child(solidBox("a", ibBlue)).
		Child(contentsBox(contentsElements, solidBox("b", ibBlue), solidBox("c", ibBlue))).
		Child(solidBox("d", ibBlue))
	block := gpui.Div().
		ID("block").
		DebugSelector("block").
		Size(gpui.Px(4*boxW+3*colGap), gpui.Px(boxH)).
		Ml(gpui.Px(20)).
		Mt(gpui.Px(20)).
		Display(gpui.DisplayBlock).
		Bg(gpui.Rgb(sectionBg)).
		Child(gpui.Div().
			ID("ib1").
			DebugSelector("ib1").
			InlineBlock().
			Size(gpui.Px(boxW), gpui.Px(boxH)).
			Bg(gpui.Rgb(ibBlue))).
		Child(gpui.Div().
			ID("ib2").
			DebugSelector("ib2").
			InlineBlock().
			Size(gpui.Px(row2W), gpui.Px(boxH)).
			Bg(gpui.Rgb(ibGreen))).
		Child(gpui.Div().
			ID("ig").
			DebugSelector("ig").
			InlineGrid().
			Size(gpui.Px(gridW), gpui.Px(boxH)).
			GridTemplateCols(gpui.GridTemplate{Repeat: 2, MinSize: gpui.GridTemplateMinZero}).
			Child(gpui.Div().
				ID("g1").
				DebugSelector("g1").
				Size(gpui.Px(gridCol), gpui.Px(boxH)).
				Bg(gpui.Rgb(0xd97706))).
			Child(gpui.Div().
				ID("g2").
				DebugSelector("g2").
				Size(gpui.Px(gridCol), gpui.Px(boxH)).
				Bg(gpui.Rgb(0xd97706))))
	nest := gpui.Div().Flex().
		ID("nest").
		DebugSelector("nest").
		W(gpui.Px(boxW)).
		Ml(gpui.Px(20)).
		Mt(gpui.Px(20)).
		FlexCol().
		Child(contentsBox(contentsElements,
			contentsBox(contentsElements, solidBox("nb", ibGreen)),
			solidBox("nc", ibGreen))).
		Child(contentsBox(contentsElements)).
		Child(solidBox("nd", ibGreen))
	return root.
		Child(row).
		Child(block).
		Child(nest).
		IntoElement()
}

// solidBox is one flat-coloured compared box (a plain div in the
// page).
func solidBox(id string, color uint32) *gpui.DivElement {
	return gpui.Div().
		ID(id).
		DebugSelector(id).
		Size(gpui.Px(boxW), gpui.Px(boxH)).
		FlexShrink0().
		Bg(gpui.Rgb(color))
}

// contentsBox is the contents element with inert box styles (the
// page's .contents twin): no box, its children spliced into the parent.
func contentsBox(contents bool, children ...*gpui.DivElement) *gpui.DivElement {
	box := gpui.Div().Flex().Bg(gpui.Rgb(contentsRed)).Size(gpui.Px(999), gpui.Px(999)).P(gpui.Px(8))
	if contents {
		box = box.Contents()
	}
	for _, child := range children {
		box = box.Child(child)
	}
	return box
}

// ---------------------------------------------------------------------------
// The overflow-auto page
// ---------------------------------------------------------------------------

// overflowPage is the overflow: auto fixture's HTML twin.
var overflowPage = `<!doctype html>
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
		width: ` + f2s(windowWidth) + `px;
		height: ` + f2s(windowHeight) + `px;
		display: flex;
		flex-direction: column;
		align-items: flex-start;
	}
	.row {
		display: flex;
		gap: 20px;
		margin-left: 20px;
		margin-top: 20px;
	}
	.scroller {
		box-sizing: border-box;
		display: flex;
		flex-direction: column;
		flex-shrink: 0;
		background: #f1f5f9;
	}
	.scroller > div {
		flex-shrink: 0;
		box-sizing: border-box;
	}
	#sc-fits, #sc-stable, #sc-overflow {
		width: ` + f2s(scW) + `px;
		height: ` + f2s(scH) + `px;
		overflow-x: hidden;
		/* The behaviour under test: the scrollbar (and its gutter)
		   appears only while the content overflows. */
		overflow-y: auto;
	}
	#sc-stable {
		/* The opt-in under test: the gutter is reserved even while no
		   scrollbar shows. */
		scrollbar-gutter: stable;
	}
	#sc-h {
		width: ` + f2s(hScW) + `px;
		height: ` + f2s(hScH) + `px;
		overflow-x: auto;
		overflow-y: hidden;
		display: flex;
		flex-direction: row;
		align-items: stretch;
	}
	#fits-row, #stable-row, #over-row {
		height: ` + f2s(rowH) + `px;
		background: #93c5fd;
	}
	#over-fill {
		height: ` + f2s(fillH) + `px;
		background: #cbd5e1;
	}
	#h-fill {
		width: ` + f2s(hFillW) + `px;
		height: ` + f2s(hFillH) + `px;
		background: #cbd5e1;
	}
</style>
</head>
<body data-id="root">
	<div class="row" data-id="row1">
		<div id="sc-fits" class="scroller" data-id="sc-fits">
			<div id="fits-row" data-id="fits-row"></div>
		</div>
		<div id="sc-stable" class="scroller" data-id="sc-stable">
			<div id="stable-row" data-id="stable-row"></div>
		</div>
		<div id="sc-overflow" class="scroller" data-id="sc-overflow">
			<div id="over-row" data-id="over-row"></div>
			<div id="over-fill" data-id="over-fill"></div>
		</div>
	</div>
	<div class="row" data-id="row2">
		<div id="sc-h" class="scroller" data-id="sc-h">
			<div id="h-fill" data-id="h-fill"></div>
		</div>
	</div>
</body>
</html>
`

// overflowView is the overflow: auto fixture's gpui twin.
func overflowView(_ *parity.EventLog) gpui.AnyElement {
	return overflowRoot()
}

// overflowRoot builds the overflow page's tree; auto selects the
// correct view (overflow: auto) or the wrong one (the pin's scroll
// approximation).
func overflowRoot() gpui.AnyElement {
	scroller := func(id, rowID, fillID string, overfill bool) *gpui.DivElement {
		s := gpui.Div().Flex().
			ID(id).
			DebugSelector(id).
			Size(gpui.Px(scW), gpui.Px(scH)).
			FlexCol().
			FlexShrink0().
			OverflowXHidden().
			Bg(gpui.Rgb(scrollerBg)).
			Child(gpui.Div().Flex().
				ID(rowID).
				DebugSelector(rowID).
				H(gpui.Px(rowH)).
				FlexShrink0().
				Bg(gpui.Rgb(fitsBg)))
		s = s.OverflowYAuto()
		if overfill {
			s = s.Child(gpui.Div().Flex().
				ID(fillID).
				DebugSelector(fillID).
				H(gpui.Px(fillH)).
				FlexShrink0().
				Bg(gpui.Rgb(hFillBg)))
		}
		return s
	}
	root := gpui.Div().Flex().
		ID("root").
		DebugSelector("root").
		SizeFull().
		FlexCol().
		AlignItems(gpui.AlignItemsFlexStart).
		Bg(gpui.Rgb(pageBackground))
	row1 := gpui.Div().Flex().Gap(gpui.Px(20)).Ml(gpui.Px(20)).Mt(gpui.Px(20)).
		Child(scroller("sc-fits", "fits-row", "", false)).
		Child(scroller("sc-stable", "stable-row", "", false).ScrollbarGutterStable()).
		Child(scroller("sc-overflow", "over-row", "over-fill", true))
	hSc := gpui.Div().
		ID("sc-h").
		DebugSelector("sc-h").
		Size(gpui.Px(hScW), gpui.Px(hScH)).
		Flex().
		FlexShrink0().
		OverflowYHidden().
		Bg(gpui.Rgb(scrollerBg)).
		Child(gpui.Div().Flex().
			ID("h-fill").
			DebugSelector("h-fill").
			W(gpui.Px(hFillW)).
			H(gpui.Px(hFillH)).
			FlexShrink0().
			Bg(gpui.Rgb(hFillBg)))
	hSc = hSc.OverflowXAuto()
	row2 := gpui.Div().Flex().Gap(gpui.Px(20)).Ml(gpui.Px(20)).Mt(gpui.Px(20)).Child(hSc)
	return root.
		Child(row1).
		Child(row2).
		IntoElement()
}

// f2s renders a number the pages' CSS uses.
func f2s(v float64) string {
	return fmt.Sprintf("%.0f", v)
}
