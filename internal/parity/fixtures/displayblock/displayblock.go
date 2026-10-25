//go:build windows

// Package displayblock is ticket 71's fixture for the block default
// display (spec D6): a plain div — no display anywhere in its chain —
// stacks its block-level children vertically, an auto inline size
// fills the containing block, a nested plain block nests the same way,
// a text child forms the block's inline paragraph, and an explicit
// display: flex sibling contrasts with the block flow (its children
// lay out in a row while the block children stack). Every compared box
// in the view relies on the DEFAULT display; the page states none,
// exactly like the HTML the default mirrors.
//
// The page avoids the block features gpui has not verified, so the
// fixture gates the implemented subset: no vertical margins (Taffy
// 0.13's block algorithm carries CSS margin collapsing, but it is
// unverified against Chrome at the gpui level — a gap, and the
// fixtures stay away from collapsing shapes), no floats (no float
// property exists and the native build enables no float_layout
// feature — a structural gap), and no mixed inline/block children in
// one box (the inline collector's paragraphs are the anonymous-block
// equivalent for the tested shapes — one run of inline content
// between block children — but the full CSS anonymous-box model is a
// gap). The only margins used are inline (horizontal), which the
// block algorithm subtracts from the stretched auto width.
//
// Chromium computes the recorded behaviour:
//
//   - the block container places its in-flow children at the running
//     block offset, one below the other (BlockLayoutAlgorithm::Layout's
//     BlockChildIterator loop over each child, placing them at
//     previous_inflow_position's block edge, core/layout/
//     block_layout_algorithm.cc:974-1075);
//   - a block-level child with an auto inline size and justify-self:
//     normal stretches to the available inline size (minus inline
//     margins) — CreateConstraintSpaceForChild's
//     SetInlineAutoBehavior(AutoSizeBehavior::kStretchImplicit) for a
//     parallel-flow, non-replaced child, core/layout/
//     block_layout_algorithm.cc:3490-3500, gated by
//     ShouldBlockContainerChildStretchAutoInlineSize (core/layout/
//     space_utils.cc:82-102) — CSS2.1 §10.3.3's rule 3: the margin box
//     of a block-level, non-replaced, in-flow box fills the containing
//     block.
package displayblock

import (
	"fmt"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The page's numbers, shared by the HTML and the gpui view (the
// tracer's discipline: one set of constants builds both sides).
const (
	windowWidth  = 640.0
	windowHeight = 400.0

	// The body is a plain block with 20px padding; its content width
	// is 600.
	pagePad = 20.0

	// The stack: a plain 400x200 block whose plain children stack. b1
	// states its own width (200) and an inline margin (20 left); b2
	// has an auto width (stretched to the stack's content width); the
	// nested block is 60 tall with 8px padding holding the inner plain
	// block, whose auto width stretches to the nested block's content
	// width (384).
	stackW  = 400.0
	stackH  = 200.0
	b1W     = 200.0
	rowH    = 40.0
	b1Ml    = 20.0
	nestedH = 60.0
	nestedP = 8.0
	innerH  = 44.0
	innerMl = 12.0

	// The paragraph: a plain block with a text child (the anonymous
	// inline content), 30 tall, auto width (stretched to 600).
	paraH      = 30.0
	paraText   = "text in a plain block"
	textSize   = 16.0
	textLine   = 25.0
	textIndent = 8.0

	// The explicit-flex sibling: display: flex, 40 tall, an 8px gap,
	// two 60x24 children laid out in a row.
	flexH    = 40.0
	flexGap  = 8.0
	flexBoxW = 60.0
	flexBoxH = 24.0
)

// The palette, shared by the page and the view.
const (
	pageBackground = 0xffffff
	stackBg        = 0xf1f5f9
	b1Bg           = 0x2563eb
	b2Bg           = 0x16a34a
	nestedBg       = 0xe2e8f0
	innerBg        = 0xdc2626
	paraBg         = 0xfde68a
	flexBg         = 0xbbf7d0
	flexBoxBg      = 0x7c3aed
	textColor      = 0x0f172a
)

// f2s renders one shared float constant for the page's CSS.
func f2s(v float64) string { return fmt.Sprintf("%.0f", v) }

// BlockFixture is the block default fixture.
var BlockFixture = parity.Fixture{
	Name:   "display-block",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   page,
	View:   View,
	BoxIDs: []string{
		"root", "stack", "b1", "b2", "nested", "inner",
		"para", "flex-sib", "fs1", "fs2",
	},
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run), set from
		// the measured correct-view statistics with the whitespace
		// fixtures' text discipline (one line of Segoe UI at the 25px
		// agreement line height), verified by the discrimination test
		// in displayblock_test.go (the wrong view — every plain div left
		// at the pre-71 flex default — measures far above). See
		// docs/browser-parity-harness.md.
		MaxMeanChannelDiff:    0.1,
		MaxROIMeanChannelDiff: 0.42,
		MaxHotPixelFraction:   0.003,
	},
}

// page is the HTML twin: plain divs with no display anywhere except
// the contrast sibling.
var page = `<!doctype html>
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
		/* No display: the browser's block default, the rule under
		   test. */
		padding: ` + f2s(pagePad) + `px;
		box-sizing: border-box;
		font: ` + f2s(textSize) + `px/` + f2s(textLine) + `px "Segoe UI", system-ui, sans-serif;
		color: #0f172a;
	}
	#stack {
		box-sizing: border-box;
		width: ` + f2s(stackW) + `px;
		height: ` + f2s(stackH) + `px;
		background: #f1f5f9;
	}
	#b1 {
		box-sizing: border-box;
		width: ` + f2s(b1W) + `px;
		height: ` + f2s(rowH) + `px;
		margin-left: ` + f2s(b1Ml) + `px;
		background: #2563eb;
	}
	#b2 {
		box-sizing: border-box;
		height: ` + f2s(rowH) + `px;
		background: #16a34a;
	}
	#nested {
		box-sizing: border-box;
		height: ` + f2s(nestedH) + `px;
		padding: ` + f2s(nestedP) + `px;
		background: #e2e8f0;
	}
	#inner {
		box-sizing: border-box;
		height: ` + f2s(innerH) + `px;
		margin-left: ` + f2s(innerMl) + `px;
		background: #dc2626;
	}
	#para {
		box-sizing: border-box;
		height: ` + f2s(paraH) + `px;
		background: #fde68a;
		padding-left: ` + f2s(textIndent) + `px;
	}
	#flex-sib {
		box-sizing: border-box;
		height: ` + f2s(flexH) + `px;
		/* The contrast: display stated, like the view's .Flex(). */
		display: flex;
		gap: ` + f2s(flexGap) + `px;
		background: #bbf7d0;
	}
	#fs1, #fs2 {
		box-sizing: border-box;
		width: ` + f2s(flexBoxW) + `px;
		height: ` + f2s(flexBoxH) + `px;
		background: #7c3aed;
	}
</style>
</head>
<body data-id="root">
	<div id="stack" data-id="stack">
		<div id="b1" data-id="b1"></div>
		<div id="b2" data-id="b2"></div>
		<div id="nested" data-id="nested">
			<div id="inner" data-id="inner"></div>
		</div>
	</div>
	<div id="para" data-id="para">` + paraText + `</div>
	<div id="flex-sib" data-id="flex-sib">
		<div id="fs1" data-id="fs1"></div>
		<div id="fs2" data-id="fs2"></div>
	</div>
</body>
</html>
`

// View is the fixture's gpui twin: plain divs relying on the block
// default (no display in any chain but the contrast sibling's), the
// text child joining the block's paragraph.
func View(_ *parity.EventLog) gpui.AnyElement {
	return view(false)
}

// WrongFlexView is the deliberately-wrong view: every plain div left
// at the pre-71 default display (Flex) — the exact state before
// browser-core 71 flipped the default to Block. The stack's children
// sit in a row instead of stacking, the auto-width children stop
// filling their containing blocks, the root's blocks sit in a row, and
// the paragraph's text becomes a flex item instead of inline content.
func WrongFlexView(_ *parity.EventLog) gpui.AnyElement {
	return view(true)
}

// view builds the tree; flexDefaults selects the wrong view (every
// plain div pinned Flex, the pre-71 default).
func view(flexDefaults bool) gpui.AnyElement {
	plain := func(d *gpui.DivElement) *gpui.DivElement {
		if flexDefaults {
			return d.Flex()
		}
		return d
	}
	root := plain(gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		P(gpui.Px(pagePad)).
		TextSize(gpui.Px(textSize)).
		LineHeight(gpui.Px(textLine)).
		TextColor(gpui.Rgb(textColor)))
	stack := plain(gpui.Div().
		ID("stack").
		DebugSelector("stack").
		Size(gpui.Px(stackW), gpui.Px(stackH)).
		Bg(gpui.Rgb(stackBg))).
		Child(plain(gpui.Div().
			ID("b1").
			DebugSelector("b1").
			Size(gpui.Px(b1W), gpui.Px(rowH)).
			Ml(gpui.Px(b1Ml)).
			Bg(gpui.Rgb(b1Bg)))).
		Child(plain(gpui.Div().
			ID("b2").
			DebugSelector("b2").
			H(gpui.Px(rowH)).
			Bg(gpui.Rgb(b2Bg)))).
		Child(plain(gpui.Div().
			ID("nested").
			DebugSelector("nested").
			H(gpui.Px(nestedH)).
			P(gpui.Px(nestedP)).
			Bg(gpui.Rgb(nestedBg))).
			Child(plain(gpui.Div().
				ID("inner").
				DebugSelector("inner").
				H(gpui.Px(innerH)).
				Ml(gpui.Px(innerMl)).
				Bg(gpui.Rgb(innerBg)))))
	para := plain(gpui.Div().
		ID("para").
		DebugSelector("para").
		H(gpui.Px(paraH)).
		Pl(gpui.Px(textIndent)).
		Bg(gpui.Rgb(paraBg))).
		Child(paraText)
	flexSib := gpui.Div().
		ID("flex-sib").
		DebugSelector("flex-sib").
		H(gpui.Px(flexH)).
		Flex().
		Gap(gpui.Px(flexGap)).
		Bg(gpui.Rgb(flexBg)).
		Child(gpui.Div().
			ID("fs1").
			DebugSelector("fs1").
			Size(gpui.Px(flexBoxW), gpui.Px(flexBoxH)).
			Bg(gpui.Rgb(flexBoxBg))).
		Child(gpui.Div().
			ID("fs2").
			DebugSelector("fs2").
			Size(gpui.Px(flexBoxW), gpui.Px(flexBoxH)).
			Bg(gpui.Rgb(flexBoxBg)))
	return root.
		Child(stack).
		Child(para).
		Child(flexSib).
		IntoElement()
}

func init() { parity.Register(BlockFixture) }
