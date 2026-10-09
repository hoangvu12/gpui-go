// Package inlinespec holds ticket15's inline layout tests: deterministic
// paragraph/fragment/box geometry through the real element phase
// pipeline (test windows drawn with the real layout/scene services and
// the deterministic test text system, whose layout_inline is the exact
// port of the reference TestTextSystem::layout_inline), plus real-window
// inline evidence through the real text stack (inline_real_test.go).
//
// The exact expectations derive from the pinned CE semantics:
// crates/gpui/src/platform.rs (TestTextSystem::layout_inline,
// layout_text, TestPlatformTextLayout::selection_bounds),
// crates/gpui/src/text_system/line_layout.rs
// (base_inline_line_bounds, expand_inline_line_for_box,
// aligned_inline_box_y, align_inline_boxes),
// crates/gpui/src/elements/div/inline.rs (InlineParagraphCollector,
// prepare_layout, merge_fragments), crates/gpui/src/util.rs
// (round_half_toward_zero) and window.rs pixel_snap_point, evaluated at
// the test window's fixed profile (scale factor 2.0, rem 16).
package inlinespec

import (
	"testing"

	"gpui-go/gpui"
)

// ---------------------------------------------------------------------------
// The inline corpus (the mirror of the reference
// div.rs::inline_div_places_element_children_in_text_flow corpus)
// ---------------------------------------------------------------------------

// inlineView is the corpus root view: a Block div of the given width
// with a "prefix " text, two 18x18 inline-flex boxes (baseline- and
// middle-aligned, with observable backgrounds) and a " suffix" text.
type inlineView struct {
	width float32
}

// Render implements Render[inlineView].
func (v *inlineView) Render(w *gpui.Window, cx *gpui.Context[inlineView]) gpui.AnyElement {
	return gpui.Div().
		Block().
		W(gpui.PxLength(v.width)).
		DebugSelector("flow").
		Child("prefix ").
		Child(gpui.Div().
			InlineFlex().
			Size(gpui.PxLength(18), gpui.PxLength(18)).
			AlignBaseline().
			ID("box-baseline").
			DebugSelector("box-baseline").
			Bg(gpui.Hsla{H: 0.1, S: 0.5, L: 0.5, A: 1})).
		Child(gpui.Div().
			InlineFlex().
			Size(gpui.PxLength(18), gpui.PxLength(18)).
			AlignMiddle().
			ID("box-middle").
			DebugSelector("box-middle").
			Bg(gpui.Hsla{H: 0.5, S: 0.5, L: 0.5, A: 1})).
		Child(" suffix").
		IntoElement()
}

// drawCorpus builds one test window and draws the inline corpus once,
// returning the window.
func drawCorpus(t *testing.T, width float32) *gpui.Window {
	t.Helper()
	ta := gpui.NewTestApp()
	app := ta.App()
	window := gpui.NewTestWindow(app, gpui.Size{Width: 200, Height: 120})
	view := gpui.NewEntity(app, window.Scope(), func(v *inlineView, cx *gpui.Context[inlineView]) {
		v.width = width
	})
	window.SetRootView(gpui.ViewOf(view))
	if _, err := gpui.DrawWindowFrame(window); err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}
	return window
}

// f32eq compares a derived decimal expectation within float32 ulp
// slack: the expectations are decimal derivations of the pinned
// arithmetic, and a float32 constant can sit one ulp off the same
// arithmetic performed through intermediate values (the bit-exact
// gates live in the recorded fixture suites fx-0001..fx-0007).
func f32eq(got, want float32) bool {
	diff := got - want
	if diff < 0 {
		diff = -diff
	}
	return diff <= 1e-4
}

// TestInlineDivPlacesElementChildrenInTextFlow is the port of the
// reference div.rs test of the same name, with the exact derived
// expectations (scale 2.0, font 16px, line height 26):
//
//   - em width 9.6; "prefix " (7 chars) paints to 67.2;
//   - the baseline box sits after the prefix text, the middle box after
//     the baseline box;
//   - the middle-aligned box's y is below the baseline box's and its
//     bottom is below the baseline box's;
//   - the paragraph row height is the middle box's expansion
//     (28.272) with baseline 23.4.
func TestInlineDivPlacesElementChildrenInTextFlow(t *testing.T) {
	window := drawCorpus(t, 160)

	facts := gpui.WindowInlineFacts(window)
	if len(facts) != 1 {
		t.Fatalf("inline paragraphs = %d, want 1 (facts: %+v)", len(facts), facts)
	}
	paragraph := facts[0]
	if paragraph.Text != "prefix  suffix" {
		t.Fatalf("paragraph text = %q, want %q", paragraph.Text, "prefix  suffix")
	}
	if len(paragraph.Rows) != 1 {
		t.Fatalf("paragraph rows = %d, want 1", len(paragraph.Rows))
	}
	row := paragraph.Rows[0]
	if row.Baseline != 23.4 {
		t.Fatalf("row baseline = %v, want 23.4", row.Baseline)
	}
	if !f32eq(row.Size.Height, 28.272) {
		t.Fatalf("row height = %v, want 28.272 (the middle box grows the row)", row.Size.Height)
	}
	// The document advance: 14 chars * 9.6 + 36 of boxes = 170.4; the
	// row width is clamped to the wrap width 160.
	if !f32eq(paragraph.Size.Width, 170.4) {
		t.Fatalf("paragraph width = %v, want 170.4", paragraph.Size.Width)
	}
	if row.Size.Width != 160 {
		t.Fatalf("row width = %v, want 160 (the wrap constraint)", row.Size.Width)
	}
	if paragraph.WrapWidth == nil || *paragraph.WrapWidth != 160 {
		t.Fatalf("paragraph wrap width = %v, want 160", paragraph.WrapWidth)
	}

	if len(paragraph.Boxes) != 2 {
		t.Fatalf("paragraph boxes = %d, want 2", len(paragraph.Boxes))
	}
	baselineBox, middleBox := paragraph.Boxes[0], paragraph.Boxes[1]
	if baselineBox.LineIndex != 0 || middleBox.LineIndex != 0 {
		t.Fatalf("box line indices = %d/%d, want 0/0", baselineBox.LineIndex, middleBox.LineIndex)
	}
	// box x: pen(7) = 67.2, then +18 for the second box.
	if !f32eq(baselineBox.Bounds.Origin.X, 67.2) {
		t.Fatalf("baseline box x = %v, want 67.2", baselineBox.Bounds.Origin.X)
	}
	if !f32eq(middleBox.Bounds.Origin.X, 85.2) {
		t.Fatalf("middle box x = %v, want 85.2", middleBox.Bounds.Origin.X)
	}
	if baselineBox.Bounds.Size != (gpui.Size{Width: 18, Height: 18}) {
		t.Fatalf("baseline box size = %+v, want 18x18", baselineBox.Bounds.Size)
	}
	// box y: baseline-aligned box bottom sits on the baseline (23.4-18);
	// middle-aligned box centers on the x-height midpoint
	// (23.4 - 8.256/2 - 9).
	if !f32eq(baselineBox.Bounds.Origin.Y, 5.4) {
		t.Fatalf("baseline box y = %v, want 5.4", baselineBox.Bounds.Origin.Y)
	}
	if !f32eq(middleBox.Bounds.Origin.Y, 10.272) {
		t.Fatalf("middle box y = %v, want 10.272", middleBox.Bounds.Origin.Y)
	}
	if middleBox.Bounds.Origin.Y <= baselineBox.Bounds.Origin.Y {
		t.Fatal("the middle-aligned box must sit below the baseline-aligned box")
	}
	if middleBox.Bounds.Bottom() <= baselineBox.Bounds.Bottom() {
		t.Fatal("the middle-aligned box's bottom must be below the baseline box's")
	}

	// The spans: the two text children, each with its fragment region —
	// the byte-range geometry (pen positions) minus the row's boxes.
	if len(paragraph.Spans) != 2 {
		t.Fatalf("paragraph spans = %d, want 2", len(paragraph.Spans))
	}
	prefix, suffix := paragraph.Spans[0], paragraph.Spans[1]
	if prefix.Text != "prefix " || prefix.Range.Start != 0 || prefix.Range.End != 7 {
		t.Fatalf("prefix span = %+v, want text %q range 0..7", prefix, "prefix ")
	}
	if suffix.Text != " suffix" || suffix.Range.Start != 7 || suffix.Range.End != 14 {
		t.Fatalf("suffix span = %+v, want text %q range 7..14", suffix, " suffix")
	}
	// The prefix fragment: corners (0,0) and (67.2, 28.272) snap (scale
	// 2, midpoint toward zero) to (0,0) and (67, 28.5).
	if len(prefix.Fragments) != 1 {
		t.Fatalf("prefix fragments = %d, want 1 (%+v)", len(prefix.Fragments), prefix.Fragments)
	} else if f := prefix.Fragments[0]; f.Origin != (gpui.Point{X: 0, Y: 0}) || f.Size != (gpui.Size{Width: 67, Height: 28.5}) {
		t.Fatalf("prefix fragment = %+v, want origin (0,0) size 67x28.5", f)
	}
	// The suffix geometry [67.2..134.4] minus the two boxes (67.2..103.2)
	// leaves [103.2..134.4]; the corners snap to (103,0)..(134.5,28.5).
	if len(suffix.Fragments) != 1 {
		t.Fatalf("suffix fragments = %d, want 1 (%+v)", len(suffix.Fragments), suffix.Fragments)
	} else if f := suffix.Fragments[0]; f.Origin.X != 103 || f.Size.Width != 31.5 {
		t.Fatalf("suffix fragment = %+v, want origin x 103 width 31.5", f)
	}

	// The placed box bounds (the debug-selector observable): the box
	// origins snap to the device grid at the paragraph origin (0,0):
	// (67.2, 5.4) -> (67, 5.5); (85.2, 10.272) -> (85, 10.5).
	placed, ok := gpui.WindowDebugBound(window, "box-baseline")
	if !ok {
		t.Fatal("the baseline box's placed bounds were not recorded")
	}
	if placed.Origin != (gpui.Point{X: 67, Y: 5.5}) || placed.Size != (gpui.Size{Width: 18, Height: 18}) {
		t.Fatalf("baseline box placed bounds = %+v, want origin (67,5.5) size 18x18", placed)
	}
	placedMiddle, ok := gpui.WindowDebugBound(window, "box-middle")
	if !ok {
		t.Fatal("the middle box's placed bounds were not recorded")
	}
	if placedMiddle.Origin != (gpui.Point{X: 85, Y: 10.5}) {
		t.Fatalf("middle box placed bounds = %+v, want origin (85,10.5)", placedMiddle)
	}

	// Painting uses the same placement: the boxes' background quads sit
	// exactly at the placed bounds in device pixels (the scene stores
	// device geometry; the test window's scale is 2: (67,5.5) ->
	// (134,11), (85,10.5) -> (170,21), 18x18 -> 36x36).
	scene := gpui.LastDrawnScene(window)
	quads, err := scene.Quads()
	if err != nil {
		t.Fatalf("scene quads: %v", err)
	}
	wantQuads := []gpui.Bounds{
		{Origin: gpui.Point{X: 134, Y: 11}, Size: gpui.Size{Width: 36, Height: 36}},
		{Origin: gpui.Point{X: 170, Y: 21}, Size: gpui.Size{Width: 36, Height: 36}},
	}
	var boxQuads int
	for _, quad := range quads {
		for _, want := range wantQuads {
			if quad.Bounds == want {
				boxQuads++
			}
		}
	}
	if boxQuads != 2 {
		t.Fatalf("box background quads at the placed bounds = %d, want 2 (paint must use the placement; quads: %+v)", boxQuads, quads)
	}

	// Hit regions read the same fragments: a point inside the prefix
	// fragment resolves to the prefix span, a point inside the suffix
	// fragment to the suffix span, and a point below the row to none.
	if span, ok := gpui.WindowInlineSpanAt(window, 10, 10); !ok || span != "prefix " {
		t.Fatalf("hit (10,10) = span %q ok=%v, want %q", span, ok, "prefix ")
	}
	if span, ok := gpui.WindowInlineSpanAt(window, 120, 10); !ok || span != " suffix" {
		t.Fatalf("hit (120,10) = span %q ok=%v, want %q", span, ok, " suffix")
	}
	if _, ok := gpui.WindowInlineSpanAt(window, 10, 60); ok {
		t.Fatal("hit (10,60) must resolve to no span (below the row)")
	}
}

// TestInlineTopAndBottomAlignmentGrowsBothExtents exercises the
// top/bottom box alignment settling (expand_inline_line_for_box's
// top_box_height/bottom_box_height and align_inline_boxes's final
// settling): a 40px top-aligned box and a 36px bottom-aligned box with
// the same 16px font stack grow the row to cover both, with the
// baseline keeping the text's placement.
func TestInlineTopAndBottomAlignmentGrowsBothExtents(t *testing.T) {
	ta := gpui.NewTestApp()
	app := ta.App()
	window := gpui.NewTestWindow(app, gpui.Size{Width: 200, Height: 120})
	view := gpui.NewEntity(app, window.Scope(), func(v *inlineView, cx *gpui.Context[inlineView]) {
		v.width = 200
	})
	window.SetRootView(gpui.ViewOf(view))
	if _, err := gpui.DrawWindowFrame(window); err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}

	facts := gpui.WindowInlineFacts(window)
	if len(facts) != 1 {
		t.Fatalf("inline paragraphs = %d, want 1", len(facts))
	}
	// The middle-aligned 18px box grows the row to 28.272 with baseline
	// 23.4 (as the flow test derives); the top box keeps the row's top,
	// the bottom box the row's bottom.
	row := facts[0].Rows[0]
	if row.Baseline != 23.4 || row.Size.Height != 28.272 {
		t.Fatalf("row = %+v, want baseline 23.4 height 28.272", row)
	}

	// Now the aligned corpus: top and bottom boxes against the same
	// text. The row must cover the taller extents.
	window2 := gpui.NewTestWindow(ta.App(), gpui.Size{Width: 200, Height: 120})
	view2 := gpui.NewEntity(app, window2.Scope(), func(v *alignedView, cx *gpui.Context[alignedView]) {})
	window2.SetRootView(gpui.ViewOf(view2))
	if _, err := gpui.DrawWindowFrame(window2); err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}
	facts2 := gpui.WindowInlineFacts(window2)
	if len(facts2) != 1 {
		t.Fatalf("inline paragraphs = %d, want 1", len(facts2))
	}
	p := facts2[0]
	// Derived: ascent 16.4, descent -4.4, line height 26 → base top
	// -23.4, bottom 2.6. Top box (40px): top_box_height 40 → bottom
	// settles to max(2.6, -23.4+40)=16.6. Bottom box (36px):
	// bottom_box_height 36 → top settles to min(-23.4, 16.6-36)=-23.4.
	// Height 40; baseline 23.4.
	if p.Rows[0].Size.Height != 40 {
		t.Fatalf("row height = %v, want 40 (the settled extents)", p.Rows[0].Size.Height)
	}
	if p.Rows[0].Baseline != 23.4 {
		t.Fatalf("row baseline = %v, want 23.4", p.Rows[0].Baseline)
	}
	// The top box pins the row top (y 0, full height 40).
	if box := p.Boxes[0]; box.Bounds.Origin.Y != 0 || box.Bounds.Size.Height != 40 {
		t.Fatalf("top box = %+v, want y 0 height 40", box)
	}
	// The bottom box pins the row bottom (y 40-36 = 4).
	if box := p.Boxes[1]; box.Bounds.Origin.Y != 4 || box.Bounds.Size.Height != 36 {
		t.Fatalf("bottom box = %+v, want y 4 height 36", box)
	}
}

// alignedView is the top/bottom alignment corpus.
type alignedView struct{}

// Render implements Render[alignedView].
func (v *alignedView) Render(w *gpui.Window, cx *gpui.Context[alignedView]) gpui.AnyElement {
	return gpui.Div().
		Block().
		W(gpui.PxLength(200)).
		Child("x").
		Child(gpui.Div().InlineFlex().Size(gpui.PxLength(10), gpui.PxLength(40)).AlignTop().Bg(gpui.Hsla{H: 0.2, S: 0.5, L: 0.5, A: 1})).
		Child(gpui.Div().InlineFlex().Size(gpui.PxLength(10), gpui.PxLength(36)).AlignBottom().Bg(gpui.Hsla{H: 0.3, S: 0.5, L: 0.5, A: 1})).
		IntoElement()
}

// TestInlineContainerSpanCollectsChildrenIntoParentParagraph checks the
// Container walk: an Inline div between two texts joins the same
// paragraph, its children's text joins the document at its position
// (the reference Text publishes InlineContent::Text unconditionally,
// text.rs:910), and the container's own span covers its children's
// ranges.
func TestInlineContainerSpanCollectsChildrenIntoParentParagraph(t *testing.T) {
	ta := gpui.NewTestApp()
	app := ta.App()
	window := gpui.NewTestWindow(app, gpui.Size{Width: 200, Height: 120})
	view := gpui.NewEntity(app, window.Scope(), func(v *containerView, cx *gpui.Context[containerView]) {})
	window.SetRootView(gpui.ViewOf(view))
	if _, err := gpui.DrawWindowFrame(window); err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}

	facts := gpui.WindowInlineFacts(window)
	if len(facts) != 1 {
		t.Fatalf("inline paragraphs = %d, want 1 (the inline container joins the outer paragraph)", len(facts))
	}
	p := facts[0]
	// The reference collector walk joins the container's Text child
	// between the outer texts: the document is "axb".
	if p.Text != "axb" {
		t.Fatalf("paragraph text = %q, want %q (the container's child joins at its position)", p.Text, "axb")
	}
	// The spans in creation order (record_span_ranges /
	// record_open_span_ranges): text "a" [0..1), the inner text "x"
	// [1..2), the container's own span — created by
	// record_open_span_ranges at its first inner content — covering
	// [1..2), and text "b" [2..3).
	if len(p.Spans) != 4 {
		t.Fatalf("paragraph spans = %d, want 4 (%+v)", len(p.Spans), p.Spans)
	}
	if p.Spans[0].Text != "a" || p.Spans[0].Range != (gpui.TextRange{Start: 0, End: 1}) {
		t.Fatalf("span 0 = %+v, want text \"a\" range 0..1", p.Spans[0])
	}
	if p.Spans[1].Text != "x" || p.Spans[1].Range != (gpui.TextRange{Start: 1, End: 2}) {
		t.Fatalf("span 1 = %+v, want text \"x\" range 1..2", p.Spans[1])
	}
	if p.Spans[2].Range != (gpui.TextRange{Start: 1, End: 2}) {
		t.Fatalf("container span = %+v, want range 1..2 (covering its children)", p.Spans[2])
	}
	if p.Spans[3].Text != "b" || p.Spans[3].Range != (gpui.TextRange{Start: 2, End: 3}) {
		t.Fatalf("span 3 = %+v, want text \"b\" range 2..3", p.Spans[3])
	}
	// The container's fragment is its children's byte-range geometry:
	// "x" occupies [9.6..19.2], which snaps at its corners (scale 2,
	// midpoint toward zero: 19.2 device -> 19 = 9.5 logical, 38.4 -> 38
	// = 19) to one 9.5-wide fragment at x 9.5.
	if len(p.Spans[2].Fragments) != 1 || !f32eq(p.Spans[2].Fragments[0].Size.Width, 9.5) || !f32eq(p.Spans[2].Fragments[0].Origin.X, 9.5) {
		t.Fatalf("container fragment = %+v, want one fragment at x 9.5 width 9.5", p.Spans[2].Fragments)
	}
}

// containerView is the nested-container corpus.
type containerView struct{}

// Render implements Render[containerView].
func (v *containerView) Render(w *gpui.Window, cx *gpui.Context[containerView]) gpui.AnyElement {
	return gpui.Div().
		Block().
		W(gpui.PxLength(200)).
		Child("a").
		Child(gpui.Div().Inline().ID("inner").Child(gpui.Text("x"))).
		Child("b").
		IntoElement()
}

// TestBlockChildSplitsParagraphs checks the collector's paragraph
// breaking: a non-inline child between two texts finishes the first
// paragraph and the trailing text starts a second one.
func TestBlockChildSplitsParagraphs(t *testing.T) {
	ta := gpui.NewTestApp()
	app := ta.App()
	window := gpui.NewTestWindow(app, gpui.Size{Width: 200, Height: 160})
	view := gpui.NewEntity(app, window.Scope(), func(v *splitView, cx *gpui.Context[splitView]) {})
	window.SetRootView(gpui.ViewOf(view))
	if _, err := gpui.DrawWindowFrame(window); err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}

	facts := gpui.WindowInlineFacts(window)
	if len(facts) != 2 {
		t.Fatalf("inline paragraphs = %d, want 2 (the flex child breaks the paragraph)", len(facts))
	}
	if facts[0].Text != "one" || facts[1].Text != "two" {
		t.Fatalf("paragraph texts = %q/%q, want one/two", facts[0].Text, facts[1].Text)
	}
	// The two paragraphs stack in block flow with the flex child
	// between them: the second paragraph's paint origin is at or below
	// the first paragraph's bottom (row origins are paragraph-relative,
	// so the stacking is read from the paragraph origins).
	if facts[1].Origin.Y < facts[0].Origin.Y+facts[0].Rows[0].Size.Height {
		t.Fatalf("the paragraphs must stack: origin0 %+v height %v origin1 %+v", facts[0].Origin, facts[0].Rows[0].Size.Height, facts[1].Origin)
	}
}

// splitView is the paragraph-breaking corpus.
type splitView struct{}

// Render implements Render[splitView].
func (v *splitView) Render(w *gpui.Window, cx *gpui.Context[splitView]) gpui.AnyElement {
	return gpui.Div().
		Block().
		W(gpui.PxLength(200)).
		Child("one").
		Child(gpui.Div().Flex().Size(gpui.PxLength(40), gpui.PxLength(10)).Bg(gpui.Hsla{H: 0.4, S: 0.5, L: 0.5, A: 1})).
		Child("two").
		IntoElement()
}

// TestEmptyRunsPlaceNoGeometry checks the empty-run path: an empty text
// child between two boxes contributes no geometry (its span is placed
// with no fragments) and does not disturb the box positions.
func TestEmptyRunsPlaceNoGeometry(t *testing.T) {
	ta := gpui.NewTestApp()
	app := ta.App()
	window := gpui.NewTestWindow(app, gpui.Size{Width: 200, Height: 120})
	view := gpui.NewEntity(app, window.Scope(), func(v *emptyView, cx *gpui.Context[emptyView]) {})
	window.SetRootView(gpui.ViewOf(view))
	if _, err := gpui.DrawWindowFrame(window); err != nil {
		t.Fatalf("DrawWindowFrame: %v — empty runs must not panic", err)
	}

	facts := gpui.WindowInlineFacts(window)
	if len(facts) != 1 {
		t.Fatalf("inline paragraphs = %d, want 1", len(facts))
	}
	p := facts[0]
	if p.Text != "ab" {
		t.Fatalf("paragraph text = %q, want %q", p.Text, "ab")
	}
	// The empty spans carry no fragments but stay placed (their children
	// know they live inside the paragraph). Both empty text children
	// join the document (the reference Text publishes its content
	// unconditionally), so each contributes one empty span.
	var emptySpans int
	for _, span := range p.Spans {
		if span.Text == "" {
			emptySpans++
			if len(span.Fragments) != 0 {
				t.Fatalf("empty span fragments = %+v, want none", span.Fragments)
			}
		}
	}
	if emptySpans != 2 {
		t.Fatalf("empty spans = %d, want 2 (one per empty text child)", emptySpans)
	}
	// Both boxes sit on the row in order: after "a" (9.6) and after the
	// first box.
	if len(p.Boxes) != 2 {
		t.Fatalf("boxes = %d, want 2", len(p.Boxes))
	}
	if p.Boxes[0].Bounds.Origin.X != 9.6 {
		t.Fatalf("box 0 x = %v, want 9.6 (after %q)", p.Boxes[0].Bounds.Origin.X, "a")
	}
	if p.Boxes[1].Bounds.Origin.X != 9.6+12 {
		t.Fatalf("box 1 x = %v, want 21.6 (after the first box)", p.Boxes[1].Bounds.Origin.X)
	}
}

// emptyView is the empty-run corpus.
type emptyView struct{}

// Render implements Render[emptyView].
func (v *emptyView) Render(w *gpui.Window, cx *gpui.Context[emptyView]) gpui.AnyElement {
	return gpui.Div().
		Block().
		W(gpui.PxLength(200)).
		Child("a").
		Child("").
		Child(gpui.Div().InlineFlex().Size(gpui.PxLength(12), gpui.PxLength(12)).AlignBaseline().Bg(gpui.Hsla{H: 0.6, S: 0.5, L: 0.5, A: 1})).
		Child("").
		Child(gpui.Div().InlineFlex().Size(gpui.PxLength(12), gpui.PxLength(12)).AlignBaseline().Bg(gpui.Hsla{H: 0.7, S: 0.5, L: 0.5, A: 1})).
		Child("b").
		IntoElement()
}
