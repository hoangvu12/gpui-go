package base

import (
	"math"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/hoangvu12/gpui-go/base/rope"
	"github.com/hoangvu12/gpui-go/gpui"
)

// Ported tests of U:input/base/element.rs (24 of 33; the editor gutter,
// geometric decoration, decoration-collection and editor-scrollbar tests
// join with their tickets).

// U:input/base/element.rs:3448-3458
func TestInputElementLineNumberColumnStaysAtThreeDigitsThenGrowsUpToSeven(t *testing.T) {
	for _, c := range []struct{ lines, want int }{
		{1, 3}, {9, 3}, {10, 3}, {999, 3}, {1_000, 4}, {999_999, 6}, {9_999_999, 7}, {10_000_000, 7},
	} {
		if got := lineNumberLen(c.lines); got != c.want {
			t.Fatalf("lineNumberLen(%d) = %d, want %d", c.lines, got, c.want)
		}
	}
}

// U:input/base/element.rs:3460-3465
func TestInputElementDisplayedLineNumberStaysWithinSevenDigits(t *testing.T) {
	for _, c := range [][2]int{{42, 42}, {9_999_999, 9_999_999}, {10_000_000, 9_999_999}} {
		if got := displayedLineNumber(c[0]); got != c[1] {
			t.Fatalf("displayedLineNumber(%d) = %d, want %d", c[0], got, c[1])
		}
	}
}

func ipt(x, y float32) gpui.Point { return gpui.Point{X: x, Y: y} }

func rectCorners(l, top, r, b float32) pointCorners {
	return pointCorners{topLeft: ipt(l, top), topRight: ipt(r, top), bottomLeft: ipt(l, b), bottomRight: ipt(r, b)}
}

// U:input/base/element.rs:3791-3822
func TestInputElementFrameOutlineIsContinuousAcrossDifferentLineWidths(t *testing.T) {
	got := frameOutlinePoints([]pointCorners{rectCorners(2, 0, 20, 10), rectCorners(0, 10, 12, 20)})
	want := []gpui.Point{ipt(2, 0), ipt(20, 0), ipt(20, 10), ipt(12, 10), ipt(12, 20), ipt(0, 20), ipt(0, 10), ipt(2, 10), ipt(2, 0)}
	if !slices.Equal(got, want) {
		t.Fatalf("outline = %v, want %v", got, want)
	}
}

// U:input/base/element.rs:3824-3845
func TestInputElementFrameOutlineKeepsHorizontalSpaceBetweenTheStrokeAndText(t *testing.T) {
	corners := []pointCorners{rectCorners(2, 0, 20, 10)}
	if got := padFrameCorners(corners, 1); !slices.Equal(got, []pointCorners{rectCorners(1, 0, 21, 10)}) {
		t.Fatalf("padded = %v", got)
	}
	points := frameOutlinePoints(corners)
	if points[0] != points[len(points)-1] {
		t.Fatalf("outline not closed: %v", points)
	}
}

func near(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-5 }

// U:input/base/element.rs:3847-3875
func TestInputElementFrameOutlineStrokeIsAlignedToPhysicalPixelsAtEachScaleFactor(t *testing.T) {
	points := []gpui.Point{ipt(0.2, 1.8), ipt(10.7, 20.3)}
	for _, c := range []struct {
		scale  float32
		width  float32
		points []gpui.Point
	}{
		{1, 1, []gpui.Point{ipt(0.5, 1.5), ipt(10.5, 20.5)}},
		{1.5, 2. / 3, []gpui.Point{ipt(1./3, 5./3), ipt(11, 61./3)}},
		{2, 1, []gpui.Point{ipt(0, 2), ipt(10.5, 20.5)}},
	} {
		width, snapped := snapFrameOutline(points, 1, c.scale)
		if !near(width, c.width) {
			t.Fatalf("scale %v: width = %v, want %v", c.scale, width, c.width)
		}
		for i, p := range snapped {
			if !near(p.X, c.points[i].X) || !near(p.Y, c.points[i].Y) {
				t.Fatalf("scale %v: points = %v, want %v", c.scale, snapped, c.points)
			}
		}
	}
}

// U:input/base/element.rs:3877-3898
func TestInputElementFrameOutlineKeepsItsVerticalEdgesInsideTheContentMask(t *testing.T) {
	points := []gpui.Point{ipt(9.5, 2.5), ipt(20.5, 2.5), ipt(20.5, 10.5), ipt(9.5, 10.5), ipt(9.5, 2.5)}
	mask := gpui.Bounds{Origin: ipt(10, 0), Size: gpui.Size{Width: 10, Height: 20}}
	want := []gpui.Point{ipt(10.5, 2.5), ipt(19.5, 2.5), ipt(19.5, 10.5), ipt(10.5, 10.5), ipt(10.5, 2.5)}
	if got := clampFrameToContentMask(points, 1, mask); !slices.Equal(got, want) {
		t.Fatalf("clamped = %v, want %v", got, want)
	}
}

func hr(start, end int, style gpui.HighlightStyle) gpui.HighlightRange {
	return gpui.HighlightRange{Range: gpui.TextRange{Start: start, End: end}, Style: style}
}

// U:input/base/element.rs:3900-3918
func TestInputElementPlainTextDecorationsIncludeUnstyledGaps(t *testing.T) {
	red := gpui.Red()
	styles := composeDecorations(nil, []gpui.HighlightRange{hr(2, 5, gpui.HighlightStyle{BackgroundColor: &red})}, gpui.TextRange{Start: 0, End: 10})
	var ranges []gpui.TextRange
	for _, s := range styles {
		ranges = append(ranges, s.Range)
	}
	if !slices.Equal(ranges, []gpui.TextRange{{Start: 0, End: 2}, {Start: 2, End: 5}, {Start: 5, End: 10}}) {
		t.Fatalf("ranges = %v", ranges)
	}
	if !styles[0].Style.IsEmpty() || !styles[2].Style.IsEmpty() {
		t.Fatalf("gaps are styled: %+v", styles)
	}
	if bg := styles[1].Style.BackgroundColor; bg == nil || *bg != red {
		t.Fatalf("decoration background = %v", bg)
	}
}

// U:input/base/element.rs:3944-3964
func TestInputElementDecorationsOverrideSyntaxHighlights(t *testing.T) {
	blue, red := inputTestBlue(), gpui.Red()
	italic := gpui.FontStyleItalic
	syntax := []gpui.HighlightRange{hr(0, 8, gpui.HighlightStyle{Color: &blue})}
	styles := composeDecorations(syntax, []gpui.HighlightRange{hr(2, 6, gpui.HighlightStyle{Color: &red, FontStyle: &italic})}, gpui.TextRange{Start: 0, End: 8})
	if styles[1].Range != (gpui.TextRange{Start: 2, End: 6}) {
		t.Fatalf("range = %v", styles[1].Range)
	}
	if c := styles[1].Style.Color; c == nil || *c != red {
		t.Fatalf("color = %v, want red", c)
	}
	if fs := styles[1].Style.FontStyle; fs == nil || *fs != italic {
		t.Fatalf("font style = %v", fs)
	}
}

// U:input/base/element.rs:4015-4037
func TestInputElementAutoGrowScrollOffsetIsClampedToCurrentViewport(t *testing.T) {
	mode := layoutModeAutoGrow(3, 8)
	for _, c := range [][2]float32{{-260, -180}, {-40, -40}, {20, 0}} {
		if got := clampAutoGrowVerticalScrollOffset(&mode, c[0], 340, 160); got != c[1] {
			t.Fatalf("clamp(%v) = %v, want %v", c[0], got, c[1])
		}
	}
	plain := layoutModePlainText()
	if got := clampAutoGrowVerticalScrollOffset(&plain, -260, 340, 160); got != -260 {
		t.Fatalf("plain text clamp = %v", got)
	}
}

func runLens(runs []gpui.TextRun) []int {
	out := []int{}
	for _, r := range runs {
		out = append(out, r.Len)
	}
	return out
}

func testRun(n int) gpui.TextRun {
	return gpui.TextRun{Len: n, Font: gpui.FontDescriptor{Family: ".SystemUIFont"}, Color: gpui.Black()}
}

// U:input/base/element.rs:4039-4096
func TestInputElementRunsForRange(t *testing.T) {
	// "use hello this-is-test"
	runs := []gpui.TextRun{testRun(3), testRun(1), testRun(5), testRun(1), testRun(12)}
	for _, c := range []struct {
		offset, start, end int
		want               []int
	}{
		{0, 0, 0, []int{}},
		{0, 0, 100, []int{3, 1, 5, 1, 12}},
		{0, 0, 6, []int{3, 1, 2}},
		{0, 1, 6, []int{2, 1, 2}},
		{0, 3, 10, []int{1, 5, 1}},
		{0, 5, 8, []int{3}},
		{3, 0, 3, []int{1, 2}},
		{3, 2, 10, []int{4, 1, 3}},
		{9, 0, 8, []int{1, 7}},
	} {
		if got := runLens(runsForRange(runs, c.offset, NewInputSelection(c.start, c.end))); !slices.Equal(got, c.want) {
			t.Fatalf("runsForRange(%d, %d..%d) = %v, want %v", c.offset, c.start, c.end, got, c.want)
		}
	}
}

type runShape struct {
	len       int
	underline bool
}

func runShapes(runs []gpui.TextRun) []runShape {
	var out []runShape
	for _, r := range runs {
		out = append(out, runShape{r.Len, r.Underline != nil})
	}
	return out
}

func testUnderline() *gpui.UnderlineStyle {
	black := gpui.Black()
	return &gpui.UnderlineStyle{Thickness: 1, Color: &black}
}

// U:input/base/element.rs:4098-4124
func TestInputElementSplitRunsPreserveImeUnderlineAcrossHighlightBoundaries(t *testing.T) {
	marked := &gpui.TextRange{Start: 2, End: 7}
	var runs []gpui.TextRun
	for _, r := range []gpui.TextRange{{Start: 0, End: 4}, {Start: 4, End: 10}} {
		runs = append(runs, splitRunForImeUnderline(gpui.DefaultTextStyle().ToRun(r.End-r.Start), r, marked, testUnderline())...)
	}
	want := []runShape{{2, false}, {2, true}, {3, true}, {3, false}}
	if got := runShapes(runs); !slices.Equal(got, want) {
		t.Fatalf("runs = %v, want %v", got, want)
	}
}

// U:input/base/element.rs:4126-4151
func TestInputElementSplitRunAppliesImeUnderlineWithoutHighlighting(t *testing.T) {
	runs := splitRunForImeUnderline(gpui.DefaultTextStyle().ToRun(10), gpui.TextRange{Start: 0, End: 10}, &gpui.TextRange{Start: 2, End: 7}, testUnderline())
	want := []runShape{{2, false}, {5, true}, {3, false}}
	if got := runShapes(runs); !slices.Equal(got, want) {
		t.Fatalf("runs = %v, want %v", got, want)
	}
	if got := splitRunForImeUnderline(gpui.DefaultTextStyle().ToRun(0), gpui.TextRange{}, nil, nil); len(got) != 0 {
		t.Fatalf("empty run split = %v", got)
	}
}

// U:input/base/element.rs:4153-4187
func TestInputElementMaskedImeUnderlineSplitsOnMaskCharBoundaries(t *testing.T) {
	text := rope.FromStr("abcdef")
	maskLen := utf8.RuneLen(inputMaskChar)

	if got := imeMarkedDisplayRange(&text, &gpui.TextRange{Start: 4, End: 6}, false); got == nil || *got != (gpui.TextRange{Start: 4, End: 6}) {
		t.Fatalf("unmasked = %v", got)
	}
	if got := imeMarkedDisplayRange(&text, nil, true); got != nil {
		t.Fatalf("no mark = %v", got)
	}
	masked := imeMarkedDisplayRange(&text, &gpui.TextRange{Start: 4, End: 6}, true)
	if masked == nil || *masked != (gpui.TextRange{Start: 4 * maskLen, End: 6 * maskLen}) {
		t.Fatalf("masked = %v", masked)
	}

	displayText := strings.Repeat(string(inputMaskChar), text.LenChars())
	runs := splitRunForImeUnderline(gpui.DefaultTextStyle().ToRun(len(displayText)), gpui.TextRange{Start: 0, End: len(displayText)}, masked, testUnderline())
	offset := 0
	for _, r := range runs {
		if !utf8.RuneStart(displayText[offset]) {
			t.Fatalf("run boundary %d splits a mask char", offset)
		}
		offset += r.Len
	}
	if offset != len(displayText) {
		t.Fatalf("runs cover %d of %d bytes", offset, len(displayText))
	}
}

// U:input/base/element.rs:4189-4226
func TestInputElementPlaceholderLineRuns(t *testing.T) {
	lines := placeholderLineRuns("ab\n\nc", []gpui.TextRun{testRun(2), testRun(2), testRun(1)})
	var texts []string
	var lens [][]int
	for _, l := range lines {
		texts = append(texts, l.line)
		lens = append(lens, runLens(l.runs))
	}
	if !slices.Equal(texts, []string{"ab", "", "c"}) {
		t.Fatalf("lines = %q", texts)
	}
	want := [][]int{{2}, {}, {1}}
	for i := range want {
		if !slices.Equal(lens[i], want[i]) {
			t.Fatalf("run lengths = %v, want %v", lens, want)
		}
	}
}

// U:input/base/element.rs:4228-4267
func TestInputElementAlignRunsToCharBoundaries(t *testing.T) {
	runs := func(lens ...int) []gpui.TextRun {
		var out []gpui.TextRun
		for _, n := range lens {
			out = append(out, testRun(n))
		}
		return out
	}
	// 15 bytes; boundaries at 0, 3, 6, 9, 12, 15.
	text := "你好，世界"

	if _, changed := alignRunsToCharBoundaries(text, runs(3, 12)); changed {
		t.Fatal("aligned runs changed")
	}
	for _, c := range []struct{ in, want []int }{
		// A mid-char boundary snaps up.
		{[]int{4, 11}, []int{6, 9}},
		// Boundaries inside one char collapse into one run.
		{[]int{1, 1, 13}, []int{3, 12}},
		// Overshooting runs are capped at the text length.
		{[]int{3, 20}, []int{3, 12}},
	} {
		aligned, changed := alignRunsToCharBoundaries(text, runs(c.in...))
		if !changed || !slices.Equal(runLens(aligned), c.want) {
			t.Fatalf("align(%v) = %v (changed %v), want %v", c.in, runLens(aligned), changed, c.want)
		}
	}
	// Undershooting runs keep the shortfall.
	if _, changed := alignRunsToCharBoundaries(text, runs(3, 3)); changed {
		t.Fatal("undershooting runs changed")
	}
}

// U:input/base/element.rs:4269-4306
func TestInputElementClipStylesToRanges(t *testing.T) {
	style := gpui.HighlightStyle{}
	styles := []gpui.HighlightRange{hr(0, 4, style), hr(4, 10, style), hr(12, 20, style)}
	rangesOf := func(s []gpui.HighlightRange) []gpui.TextRange {
		var out []gpui.TextRange
		for _, h := range s {
			out = append(out, h.Range)
		}
		return out
	}
	tr := func(a, b int) gpui.TextRange { return gpui.TextRange{Start: a, End: b} }

	if got := rangesOf(clipStylesToRanges(styles, []gpui.TextRange{tr(2, 6), tr(14, 18)})); !slices.Equal(got, []gpui.TextRange{tr(2, 4), tr(4, 6), tr(14, 18)}) {
		t.Fatalf("gap clip = %v", got)
	}
	if got := rangesOf(clipStylesToRanges(styles, []gpui.TextRange{tr(0, 20)})); !slices.Equal(got, []gpui.TextRange{tr(0, 4), tr(4, 10), tr(12, 20)}) {
		t.Fatalf("inside clip = %v", got)
	}
	if got := rangesOf(clipStylesToRanges([]gpui.HighlightRange{hr(0, 30, style)}, []gpui.TextRange{tr(5, 10), tr(20, 25)})); !slices.Equal(got, []gpui.TextRange{tr(5, 10), tr(20, 25)}) {
		t.Fatalf("split clip = %v", got)
	}
	if got := clipStylesToRanges(styles, nil); len(got) != 0 {
		t.Fatalf("no ranges = %v", got)
	}
}

// U:input/base/element.rs:4308-4347
func TestInputElementSplitRunsByBgSegments(t *testing.T) {
	blue := inputTestBlue()
	run := func(n int) gpui.TextRun { r := testRun(n); r.Color = blue; return r }
	segments := []inputColoredRange{
		{rng: NewInputSelection(8, 12), color: gpui.Red()},
		{rng: NewInputSelection(12, 18), color: inputTestBlue()},
	}
	result := splitRunsByBgSegments(5, []gpui.TextRun{run(5), run(7), run(24)}, segments)
	if got := runLens(result); !slices.Equal(got, []int{3, 2, 2, 5, 1, 23}) {
		t.Fatalf("lengths = %v", got)
	}
	wantColors := []gpui.Hsla{blue, gpui.Black(), gpui.Black(), gpui.Black(), gpui.Black(), blue}
	for i, r := range result {
		if r.Color != wantColors[i] {
			t.Fatalf("run %d color = %v, want %v", i, r.Color, wantColors[i])
		}
	}
}

// U:input/base/element.rs:4349-4359
func TestInputElementEmptyBottomHeightOutsideCodeEditor(t *testing.T) {
	for _, rows := range []*int{nil, intPtr(0), intPtr(3), intPtr(99)} {
		if got := emptyBottomHeight(false, rows, 800, 20); got != 0 {
			t.Fatalf("empty bottom = %v", got)
		}
	}
}

func intPtr(v int) *int { return &v }

// U:input/base/element.rs:4361-4377
func TestInputElementEmptyBottomHeightCodeEditorDefault(t *testing.T) {
	if got := emptyBottomHeight(true, nil, 800, 20); got != 400 {
		t.Fatalf("half viewport = %v", got)
	}
	if got := emptyBottomHeight(true, nil, 40, 20); got != bottomMarginRows*20 {
		t.Fatalf("floor = %v", got)
	}
}

// U:input/base/element.rs:4379-4398
func TestInputElementEmptyBottomHeightExplicitRowCount(t *testing.T) {
	for _, rows := range []int{0, 1, 3, 8, 64} {
		want := float32(rows) * 20
		if got := emptyBottomHeight(true, &rows, 800, 20); got != want {
			t.Fatalf("rows %d: %v", rows, got)
		}
		if got := emptyBottomHeight(true, &rows, 20, 20); got != want {
			t.Fatalf("rows %d tiny viewport: %v", rows, got)
		}
	}
}

// U:input/base/element.rs:4400-4413
func TestInputElementCursorSurroundingPaddingAutoGrow(t *testing.T) {
	for _, lines := range []*int{nil, intPtr(0), intPtr(3), intPtr(99)} {
		for _, visible := range []int{0, 1, 8, 64} {
			if got := cursorSurroundingPadding(true, lines, visible, 20); got != 20 {
				t.Fatalf("auto grow padding = %v", got)
			}
		}
	}
}

// U:input/base/element.rs:4415-4441
func TestInputElementCursorSurroundingPaddingDefault(t *testing.T) {
	if got := cursorSurroundingPadding(false, nil, bottomMarginRows*8-1, 20); got != 20 {
		t.Fatalf("small viewport = %v", got)
	}
	if got := cursorSurroundingPadding(false, nil, bottomMarginRows*8, 20); got != bottomMarginRows*20 {
		t.Fatalf("boundary = %v", got)
	}
	if got := cursorSurroundingPadding(false, nil, 100, 20); got != bottomMarginRows*20 {
		t.Fatalf("large viewport = %v", got)
	}
}

// U:input/base/element.rs:4443-4460
func TestInputElementCursorSurroundingPaddingExplicit(t *testing.T) {
	for _, lines := range []int{0, 1, 2, 5, 50} {
		raw := float32(lines) * 20
		for _, visible := range []int{0, 1, 8, 100} {
			want := min(raw, float32(visible)*20/2)
			if got := cursorSurroundingPadding(false, &lines, visible, 20); got != want {
				t.Fatalf("lines %d visible %d: %v, want %v", lines, visible, got, want)
			}
		}
	}
}

// U:input/base/element.rs:4462-4496
func TestInputElementCursorSurroundingPaddingSaturatesAgainstViewport(t *testing.T) {
	if got := cursorSurroundingPadding(false, intPtr(50), 10, 20); got != 100 {
		t.Fatalf("oversized override = %v", got)
	}
	if got := cursorSurroundingPadding(false, intPtr(3), 40, 20); got != 60 {
		t.Fatalf("fitting override = %v", got)
	}
	visible := bottomMarginRows * 8
	if got := cursorSurroundingPadding(false, nil, visible, 20); got != min(bottomMarginRows*20, float32(visible)*20/2) {
		t.Fatalf("default saturation = %v", got)
	}
}

// inputTestBlue is the pinned gpui::blue() (R:color.rs: h 0.6, s 1, l 0.5);
// the Go core has no Blue helper.
func inputTestBlue() gpui.Hsla { return gpui.Hsla{H: 0.6, S: 1, L: 0.5, A: 1} }
