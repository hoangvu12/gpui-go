package base

import (
	"slices"
	"testing"
	"weak"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ports of the U:text/inline.rs tests (1364-2418). Of 21: 15 ported here,
// 5 ported by ticket 30, 1 deferred:
//   - line_bounds_tests::row_walk_matches_the_character_walk and the
//     range_highlight_tests module (text_rows_extent_matches_the_character
//     _walk, a_highlight_starting_a_wrapped_row_paints_only_that_row,
//     highlights_after_a_hard_line_break_start_on_its_row,
//     highlights_follow_centered_and_right_aligned_rows) lay a StyledText
//     out under the WideMonoTextSystem fake and wrap it. gpui exposes no
//     platform text-system seam to a test, and the deterministic test text
//     system lays every document out on one visual line (gpui/testwindow.go),
//     so ticket 30 runs them on real fonts in a real window:
//     text_range_highlight_real_windows_test.go.
//   - retained_layout_tests::shortening_an_inline_flow_releases_obsolete_
//     fragment_layouts: the Go table is weak-keyed, so a dropped fragment
//     state leaves an entry only until the next sweep; deferred with 30's
//     scroll/perf transcripts (the live-entry count it asserts is ported in
//     inline_flow_fragments_keep_their_layouts_across_frames).

func monoHighlight(style gpui.HighlightStyle) inlineHighlight {
	mono := "Mono"
	return inlineHighlight{style: style, fontFamily: &mono}
}

func ptrOf[T any](v T) *T { return &v }

// U:text/inline.rs:1368-1390
func TestFadesTextAndBackgroundOnlyInsideTheRange(t *testing.T) {
	red := gpui.Hsla{H: 0, S: 1, L: 0.5, A: 1}
	code := inlineHighlightOf(gpui.HighlightStyle{BackgroundColor: &red})
	combined := fadeHighlights([]inlineHighlightRange{{mdRange(0, 4), code}}, []textFadeRange{{mdRange(2, 6), 0.5}})
	var ranges []gpui.TextRange
	for _, c := range combined {
		ranges = append(ranges, c.rng)
	}
	if !slices.Equal(ranges, []gpui.TextRange{mdRange(0, 2), mdRange(2, 4), mdRange(4, 6)}) {
		t.Fatalf("ranges %v", ranges)
	}
	if combined[0].h.style.FadeOut != nil || combined[0].h.style.BackgroundColor.A != 1 {
		t.Fatal("untouched range faded")
	}
	if f := combined[1].h.style.FadeOut; f == nil || *f != 0.5 || combined[1].h.style.BackgroundColor.A != 0.5 {
		t.Fatal("code range not faded")
	}
	if f := combined[2].h.style.FadeOut; f == nil || *f != 0.5 || combined[2].h.style.BackgroundColor != nil {
		t.Fatal("text range not faded")
	}
}

// U:text/inline.rs:1392-1400
func TestNoFadesLeaveHighlightsUntouched(t *testing.T) {
	bold := inlineHighlightOf(gpui.HighlightStyle{FontWeight: ptrOf(textFontWeightBold)})
	highlights := []inlineHighlightRange{{mdRange(1, 3), bold}}
	if !inlineHighlightsEqual(fadeHighlights(highlights, nil), highlights) {
		t.Fatal("highlights changed")
	}
}

// U:text/inline.rs:1946-1961
func TestTextRunsShapeACodeHighlightInItsFontFamily(t *testing.T) {
	style := gpui.DefaultTextStyle()
	style.FontFamily = "Body"
	runs := textRuns(12, style, []inlineHighlightRange{{mdRange(4, 8), monoHighlight(gpui.HighlightStyle{})}})
	type fam struct {
		n int
		f string
	}
	var got []fam
	for _, r := range runs {
		got = append(got, fam{r.Len, r.Font.Family})
	}
	if !slices.Equal(got, []fam{{4, "Body"}, {4, "Mono"}, {4, "Body"}}) {
		t.Fatalf("runs %v", got)
	}
}

// U:text/inline.rs:1963-1969
func TestRowsAlignTheWayGPUIPaintsThem(t *testing.T) {
	if alignedRowLeft(gpui.TextAlignLeft, 100, 40) != 0 || alignedRowLeft(gpui.TextAlignCenter, 100, 40) != 30 ||
		alignedRowLeft(gpui.TextAlignRight, 100, 40) != 60 {
		t.Fatal("alignment")
	}
}

func glyph(start, end, row int, left, right float32) glyphBox {
	return glyphBox{text: mdRange(start, end), row: row, left: left, right: right}
}

// U:text/inline.rs:1980-1996
func TestRangeBoxesJoinTheGlyphsOfARangeOnEachRow(t *testing.T) {
	glyphs := []glyphBox{glyph(0, 1, 0, 0, 8), glyph(1, 2, 0, 8, 16), glyph(2, 3, 0, 16, 24), glyph(3, 4, 1, 0, 8), glyph(4, 5, 1, 8, 16)}
	check := func(r gpui.TextRange, want []rowBox) {
		t.Helper()
		if got := rangeBoxes(glyphs, r); !slices.Equal(got, want) {
			t.Errorf("%v: %v, want %v", r, got, want)
		}
	}
	check(mdRange(1, 2), []rowBox{{0, 8, 16}})
	check(mdRange(0, 5), []rowBox{{0, 0, 24}, {1, 0, 16}})
	check(mdRange(3, 5), []rowBox{{1, 0, 16}})
	check(mdRange(2, 2), nil)
}

// U:text/inline.rs:1998-2009
func TestRangeBoxesFollowRightToLeftGlyphs(t *testing.T) {
	glyphs := []glyphBox{glyph(0, 2, 0, 16, 24), glyph(2, 4, 0, 8, 16), glyph(4, 6, 0, 0, 8)}
	if got := rangeBoxes(glyphs, mdRange(0, 2)); !slices.Equal(got, []rowBox{{0, 16, 24}}) {
		t.Fatal(got)
	}
	if got := rangeBoxes(glyphs, mdRange(2, 6)); !slices.Equal(got, []rowBox{{0, 0, 16}}) {
		t.Fatal(got)
	}
}

// U:text/inline.rs:2011-2016
func TestRangeBoxesCoverAGlyphDrawingPartOfTheRange(t *testing.T) {
	glyphs := []glyphBox{glyph(0, 2, 0, 0, 10), glyph(2, 3, 0, 10, 15)}
	if got := rangeBoxes(glyphs, mdRange(1, 2)); !slices.Equal(got, []rowBox{{0, 0, 10}}) {
		t.Fatal(got)
	}
}

// U:text/inline.rs:2018-2039
func TestCombineHighlightsCutsABoldSpanAtTheCodeBoundary(t *testing.T) {
	bold := inlineHighlightOf(gpui.HighlightStyle{FontWeight: ptrOf(textFontWeightBold)})
	combined := combineInlineHighlights([]inlineHighlightRange{{mdRange(0, 10), bold}}, []inlineHighlightRange{{mdRange(6, 10), monoHighlight(gpui.HighlightStyle{})}})
	if len(combined) != 2 || combined[0].rng != mdRange(0, 6) || combined[1].rng != mdRange(6, 10) {
		t.Fatalf("%v", combined)
	}
	for _, c := range combined {
		if c.h.style.FontWeight == nil || *c.h.style.FontWeight != textFontWeightBold {
			t.Fatal("bold lost")
		}
	}
	if combined[0].h.fontFamily != nil || combined[1].h.fontFamily == nil || *combined[1].h.fontFamily != "Mono" {
		t.Fatal("families")
	}
}

func tpt(x, y float32) gpui.Point { return gpui.Point{X: x, Y: y} }

// U:text/inline.rs:2041-2187
func TestPointInTextSelection(t *testing.T) {
	start, end := tpt(50, 50), tpt(150, 150)
	for _, c := range []struct {
		p    gpui.Point
		want bool
	}{
		{tpt(50, 40), true}, {tpt(50, 50), true}, {tpt(40, 50), false}, {tpt(160, 50), true},
		{tpt(100, 70), true}, {tpt(40, 70), true}, {tpt(160, 70), true},
		{tpt(100, 140), true}, {tpt(40, 140), true}, {tpt(160, 140), false},
		{tpt(100, 20), false}, {tpt(100, 160), false},
	} {
		if got := pointInTextSelection(c.p, 10, start, end, 20); got != c.want {
			t.Errorf("%v = %v, want %v", c.p, got, c.want)
		}
	}
}

// U:text/inline.rs:2189-2230
func TestPointInTextSelectionReversedDragDirection(t *testing.T) {
	start, end := tpt(80, 150), tpt(150, 50)
	for _, c := range []struct {
		p    gpui.Point
		want bool
	}{{tpt(140, 50), false}, {tpt(150, 50), true}, {tpt(75, 140), true}, {tpt(80, 140), false}} {
		if got := pointInTextSelection(c.p, 10, start, end, 20); got != c.want {
			t.Errorf("%v = %v, want %v", c.p, got, c.want)
		}
	}
}

// U:text/inline.rs:2232-2260 and 2262-2290
func TestPointInTextSelectionSameVisualLine(t *testing.T) {
	for _, ends := range [][2]gpui.Point{{tpt(100, 55), tpt(60, 58)}, {tpt(60, 58), tpt(100, 55)}} {
		for _, c := range []struct {
			p    gpui.Point
			want bool
		}{{tpt(40, 50), false}, {tpt(70, 50), true}, {tpt(110, 50), false}} {
			if got := pointInTextSelection(c.p, 10, ends[0], ends[1], 20); got != c.want {
				t.Errorf("%v %v = %v, want %v", ends, c.p, got, c.want)
			}
		}
	}
}

func retainedCounts() (int, int) {
	retainedLayouts.Lock()
	defer retainedLayouts.Unlock()
	alive := 0
	for _, r := range retainedLayouts.m {
		if r.state.Value() != nil {
			alive++
		}
	}
	return len(retainedLayouts.m), alive
}

func clearRetained() {
	retainedLayouts.Lock()
	retainedLayouts.m = map[weak.Pointer[inlineState]]*retainedLayout{}
	retainedLayouts.Unlock()
}

// U:text/inline.rs:2318-2341: the same document twice in a frame keeps one
// layout per paragraph state and paints both.
func TestTheSameParagraphRenderedTwiceInAFrameKeepsOneLayoutAndPaintsBoth(t *testing.T) {
	clearRetained()
	ta := textTestApp(t)
	state := newTextViewStateEntity(ta, "First paragraph.\n\nSecond **paragraph**.")
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.Div().Block().W(gpui.Px(300)).Child(NewTextView(state)).Child(NewTextView(state)).IntoElement()
	})
	for range 3 {
		drawText(t, cx)
	}
	if n, _ := retainedCounts(); n != 2 {
		t.Fatalf("retained %d layouts, want one per paragraph state", n)
	}
}

// U:text/inline.rs:2353-2385
func TestInlineFlowFragmentsKeepTheirLayoutsAcrossFrames(t *testing.T) {
	clearRetained()
	ta := textTestApp(t)
	state := newTextViewStateEntity(ta, "Call `foo` now.")
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.Div().Block().W(gpui.Px(300)).Child(NewTextView(state)).IntoElement()
	})
	for range 3 {
		drawText(t, cx)
	}
	if n, alive := retainedCounts(); n != 3 || alive != 3 {
		t.Fatalf("retained %d (%d alive), want one per fragment", n, alive)
	}
}
