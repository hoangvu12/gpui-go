package base

// Ported tests of U:input/editor/display_map/text_wrapper.rs:989-1673.

import (
	"reflect"
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/base/rope"
	"github.com/hoangvu12/gpui-go/base/sumtree"
	"github.com/hoangvu12/gpui-go/gpui"
)

func noWrap(string, float32, int) []gpui.Boundary { return nil }

// wrapperLines is assert_wrapper_lines' actual side
// (U:input/editor/display_map/text_wrapper.rs:1015-1030).
func wrapperLines(text rope.Rope, w *textWrapper) [][]string {
	var out [][]string
	offset := 0
	for _, line := range w.iterLines() {
		var parts []string
		for _, r := range line.wrappedLines {
			parts = append(parts, text.Slice(offset+r.Start, offset+r.End).String())
		}
		out = append(out, parts)
		offset += line.len() + 1
	}
	return out
}

func wantWrapperLines(t *testing.T, text rope.Rope, w *textWrapper, want [][]string) {
	t.Helper()
	if got := wrapperLines(text, w); !reflect.DeepEqual(got, want) {
		t.Fatalf("wrapper lines = %q, want %q", got, want)
	}
}

func replaceRope(text *rope.Rope, start, end int, newText string) {
	RopeExtOf(text).Replace(start, end, newText)
}

func sel(start, end int) InputSelection { return NewInputSelection(start, end) }

// U:input/editor/display_map/text_wrapper.rs:996-1191
func TestTextWrapperUpdate(t *testing.T) {
	wrapper := newTextWrapper(gpui.FontDescriptor{Family: "Arial"}, 14, nil)
	text := rope.FromStr("Hello, 世界!\r\nThis is second line.\nThis is third line.\n这里是第 4 行。")
	ext := RopeExtOf(&text)
	edit := func(start, end int, newText string) {
		t.Helper()
		replaceRope(&text, start, end, newText)
		wrapper.updateWith(text, sel(start, end), rope.FromStr(newText), noWrap)
	}

	wrapper.updateWith(text, sel(0, text.Len()), text, noWrap)
	if n := wrapper.linesCount(); n != 4 {
		t.Fatalf("lines count = %d, want 4", n)
	}
	wantWrapperLines(t, text, &wrapper, [][]string{{"Hello, 世界!\r"}, {"This is second line."}, {"This is third line."}, {"这里是第 4 行。"}})

	// Add a new text to the end.
	edit(text.Len(), text.Len(), "New text")
	if got := text.String(); got != "Hello, 世界!\r\nThis is second line.\nThis is third line.\n这里是第 4 行。New text" {
		t.Fatalf("text = %q", got)
	}
	wantWrapperLines(t, text, &wrapper, [][]string{{"Hello, 世界!\r"}, {"This is second line."}, {"This is third line."}, {"这里是第 4 行。New text"}})

	// Replace the first line's `Hello` with `AAA`.
	edit(0, 5, "AAA")
	wantWrapperLines(t, text, &wrapper, [][]string{{"AAA, 世界!\r"}, {"This is second line."}, {"This is third line."}, {"这里是第 4 行。New text"}})

	// Remove the second line.
	edit(ext.LineStartOffset(1), ext.LineEndOffset(1)+1, "")
	if got := text.String(); got != "AAA, 世界!\r\nThis is third line.\n这里是第 4 行。New text" {
		t.Fatalf("text = %q", got)
	}
	if n := wrapper.linesCount(); n != 3 {
		t.Fatalf("lines count = %d, want 3", n)
	}
	wantWrapperLines(t, text, &wrapper, [][]string{{"AAA, 世界!\r"}, {"This is third line."}, {"这里是第 4 行。New text"}})

	// Replace the first 2 lines.
	edit(ext.LineStartOffset(0), ext.LineEndOffset(1)+1, "This is a new line.\nThis is new line 2.\n")
	if n := wrapper.linesCount(); n != 3 {
		t.Fatalf("lines count = %d, want 3", n)
	}
	wantWrapperLines(t, text, &wrapper, [][]string{{"This is a new line."}, {"This is new line 2."}, {"这里是第 4 行。New text"}})

	// Add a new line at the end.
	edit(text.Len(), text.Len(), "\nThis is a new line at the end.")
	if n := wrapper.linesCount(); n != 4 {
		t.Fatalf("lines count = %d, want 4", n)
	}
	wantWrapperLines(t, text, &wrapper, [][]string{{"This is a new line."}, {"This is new line 2."}, {"这里是第 4 行。New text"}, {"This is a new line at the end."}})

	// Add a new line at the beginning.
	edit(0, 0, "This is a new line at the beginning.\n")
	if n := wrapper.linesCount(); n != 5 {
		t.Fatalf("lines count = %d, want 5", n)
	}
	wantWrapperLines(t, text, &wrapper, [][]string{{"This is a new line at the beginning."}, {"This is a new line."}, {"This is new line 2."}, {"这里是第 4 行。New text"}, {"This is a new line at the end."}})

	// Remove all: at least one line stays.
	edit(0, text.Len(), "")
	if text.String() != "" || wrapper.linesCount() != 1 {
		t.Fatalf("text = %q, lines = %d", text.String(), wrapper.linesCount())
	}
	if line, _ := wrapper.line(0); !reflect.DeepEqual(line.wrappedLines, []InputSelection{sel(0, 0)}) {
		t.Fatalf("line 0 = %v", line.wrappedLines)
	}

	// update_all.
	rng := sel(0, text.Len())
	replaceRope(&text, rng.Start, rng.End, "This is a full text.\nThis is a second line.")
	wrapper.updateWith(text, rng, text, noWrap)
	if n := wrapper.linesCount(); n != 2 {
		t.Fatalf("lines count = %d, want 2", n)
	}
}

// U:input/editor/display_map/text_wrapper.rs:1203-1220
func TestTextWrapperLongestRowAfterShrink(t *testing.T) {
	wrapper := newTextWrapper(gpui.FontDescriptor{Family: "Arial"}, 14, nil)
	text := rope.FromStr("aa\nthis is the longest line\nbb")
	wrapper.updateWith(text, sel(0, text.Len()), text, noWrap)
	if r := wrapper.longestRow(); r != 1 {
		t.Fatalf("longest row = %d, want 1", r)
	}
	ext := RopeExtOf(&text)
	rng := sel(ext.LineStartOffset(0), ext.LineEndOffset(0))
	newText := "a very very long first line now"
	replaceRope(&text, rng.Start, rng.End, newText)
	wrapper.updateWith(text, rng, rope.FromStr(newText), noWrap)
	if r := wrapper.longestRow(); r != 0 {
		t.Fatalf("longest row = %d, want 0", r)
	}
}

// U:input/editor/display_map/text_wrapper.rs:1222-1246
func TestTextWrapperEditLastLineAndFullDelete(t *testing.T) {
	wrapper := newTextWrapper(gpui.FontDescriptor{Family: "Arial"}, 14, nil)
	text := rope.FromStr("one\ntwo\nthree")
	wrapper.updateWith(text, sel(0, text.Len()), text, noWrap)
	if n := wrapper.linesCount(); n != 3 {
		t.Fatalf("lines = %d", n)
	}
	rng := sel(RopeExtOf(&text).LineStartOffset(2), text.Len())
	replaceRope(&text, rng.Start, rng.End, "THREE EDITED")
	wrapper.updateWith(text, rng, rope.FromStr("THREE EDITED"), noWrap)
	if line, _ := wrapper.line(2); wrapper.linesCount() != 3 || line.len() != len("THREE EDITED") {
		t.Fatalf("lines = %d, line 2 len = %d", wrapper.linesCount(), line.len())
	}

	rng = sel(0, text.Len())
	replaceRope(&text, rng.Start, rng.End, "")
	wrapper.updateWith(text, rng, rope.FromStr(""), noWrap)
	if wrapper.linesCount() != 1 || wrapper.len() != 1 {
		t.Fatalf("lines = %d, wrap rows = %d", wrapper.linesCount(), wrapper.len())
	}
	if line, _ := wrapper.line(0); !reflect.DeepEqual(line.wrappedLines, []InputSelection{sel(0, 0)}) {
		t.Fatalf("line 0 = %v", line.wrappedLines)
	}
}

func items(lines ...lineItem) wrapLines { return sumtree.FromIter[lineItem](lines) }

// U:input/editor/display_map/text_wrapper.rs:1248-1291
func TestTextWrapperWrapRowBufferLineBoundaries(t *testing.T) {
	wrapper := newTextWrapper(gpui.FontDescriptor{Family: "Arial"}, 14, nil)
	wrapper.text = rope.FromStr("aa\nbbbb\nc")
	wrapper.lines = items(
		lineItem{length: 2, wrappedLines: []InputSelection{sel(0, 2)}},
		lineItem{length: 4, wrappedLines: []InputSelection{sel(0, 2), sel(2, 4)}},
		lineItem{length: 1, wrappedLines: []InputSelection{sel(0, 1)}},
	)
	if wrapper.linesCount() != 3 || wrapper.len() != 4 {
		t.Fatalf("lines = %d, wrap rows = %d", wrapper.linesCount(), wrapper.len())
	}
	for row, want := range []int{0, 1, 3, 4} {
		if got := wrapper.bufferLineToFirstWrapRow(row); got != want {
			t.Errorf("first wrap row of %d = %d, want %d", row, got, want)
		}
	}
	for row, want := range []InputSelection{sel(0, 1), sel(1, 3), sel(3, 4), sel(4, 4)} {
		if got := wrapper.bufferLineToWrapRowRange(row); got != want {
			t.Errorf("wrap row range of %d = %v, want %v", row, got, want)
		}
	}
	for wrapRow, want := range []int{0, 1, 1, 2, 2} {
		if got := wrapper.wrapRowToBufferLine(wrapRow); got != want {
			t.Errorf("buffer line of wrap row %d = %d, want %d", wrapRow, got, want)
		}
	}
}

// U:input/editor/display_map/text_wrapper.rs:1293-1329
func TestTextWrapperWrapRowQueriesAfterIncrementalSplice(t *testing.T) {
	width := float32(10)
	wrapper := newTextWrapper(gpui.FontDescriptor{Family: "Arial"}, 14, &width)
	text := rope.FromStr("aa\nbbbb\nc")
	fakeWrap := func(line string, _ float32, _ int) []gpui.Boundary {
		if len(line) > 2 {
			return []gpui.Boundary{{Ix: 2}}
		}
		return nil
	}
	wrapper.updateWith(text, sel(0, text.Len()), text, fakeWrap)
	for row, want := range []InputSelection{sel(0, 1), sel(1, 3), sel(3, 4)} {
		if got := wrapper.bufferLineToWrapRowRange(row); got != want {
			t.Fatalf("wrap row range of %d = %v, want %v", row, got, want)
		}
	}

	ext := RopeExtOf(&text)
	rng := sel(ext.LineStartOffset(1), ext.LineEndOffset(1))
	replaceRope(&text, rng.Start, rng.End, "dd\neeee")
	wrapper.updateWith(text, rng, rope.FromStr("dd\neeee"), fakeWrap)

	if wrapper.linesCount() != 4 || wrapper.len() != 5 {
		t.Fatalf("lines = %d, wrap rows = %d", wrapper.linesCount(), wrapper.len())
	}
	for row, want := range []InputSelection{sel(0, 1), sel(1, 2), sel(2, 4), sel(4, 5)} {
		if got := wrapper.bufferLineToWrapRowRange(row); got != want {
			t.Errorf("wrap row range of %d = %v, want %v", row, got, want)
		}
	}
	for wrapRow, want := range []int{0, 1, 2, 2, 3} {
		if got := wrapper.wrapRowToBufferLine(wrapRow); got != want {
			t.Errorf("buffer line of wrap row %d = %d, want %d", wrapRow, got, want)
		}
	}
}

// emptyTestRoot is an empty window root.
type emptyTestRoot struct{}

func (*emptyTestRoot) Render(*gpui.Window, *gpui.Context[emptyTestRoot]) gpui.AnyElement {
	return gpui.Div().IntoElement()
}

// emptyShapedLines are ShapedLine::default().with_len(n): shaped empty
// text (zero width) reporting each length.
func emptyShapedLines(t *testing.T, lens ...int) []*gpui.ShapedLine {
	t.Helper()
	ta := gpui.NewTestApp()
	_, cx := ta.AddWindowView(func(*emptyTestRoot, *gpui.Window, *gpui.Context[emptyTestRoot]) {})
	var out []*gpui.ShapedLine
	cx.Update(func(w *gpui.Window, _ *gpui.App) {
		empty := mustShapeLine(w, "", 14, []gpui.TextRun{{Font: gpui.FontDescriptor{Family: "Arial"}}})
		for _, n := range lens {
			out = append(out, empty.WithLen(n))
		}
	})
	return out
}

// U:input/editor/display_map/text_wrapper.rs:1331-1341
func TestTextWrapperLineLayout(t *testing.T) {
	l := &inputLineLayout{}
	l.setWrappedLines(emptyShapedLines(t, 100, 50))
	if l.len() != 150 || len(l.wrappedLines) != 2 {
		t.Fatalf("len = %d, lines = %d", l.len(), len(l.wrappedLines))
	}
}

// testLastLayout is a layout context whose only load-bearing field is the
// line height (U:input/editor/display_map/text_wrapper.rs:1343-1361).
func testLastLayout(lineHeight float32) *lastLayout {
	return &lastLayout{
		visibleRange:           sel(0, 1),
		visibleBufferLines:     []int{0},
		visibleLineByteOffsets: []int{0},
		lineHeight:             lineHeight,
		textAlign:              gpui.TextAlignLeft,
	}
}

// U:input/editor/display_map/text_wrapper.rs:1363-1376
func TestTextWrapperPositionForIndexPrefersFirstLeadingEmptyVisualLine(t *testing.T) {
	l := newInputLineLayout(emptyShapedLines(t, 0, 0, 3))
	if pos, ok := l.positionForIndex(0, testLastLayout(20), false); !ok || pos != (gpui.Point{}) {
		t.Fatalf("position = %v, %v; want (0, 0)", pos, ok)
	}
}

// U:input/editor/display_map/text_wrapper.rs:1378-1417
func TestTextWrapperClickingPastAWrappedRowKeepsTheCaretOnThatRow(t *testing.T) {
	// One buffer line wrapped into two visual rows, splitting at byte 10.
	l := newInputLineLayout(emptyShapedLines(t, 10, 5))
	layout := testLastLayout(20)

	ix, affinity, ok := l.closestIndexForPosition(gpui.Point{X: 999, Y: 5}, layout)
	if !ok || ix != 10 || !affinity {
		t.Fatalf("closest = %d, %v, %v; want 10 with line-end affinity", ix, affinity, ok)
	}
	if pos, _ := l.positionForIndex(ix, layout, affinity); pos.Y != 0 {
		t.Fatalf("y with affinity = %v, want 0", pos.Y)
	}
	if pos, _ := l.positionForIndex(ix, layout, false); pos.Y != 20 {
		t.Fatalf("y without affinity = %v, want 20", pos.Y)
	}

	// The final row owns the end of the line outright.
	ix, affinity, ok = l.closestIndexForPosition(gpui.Point{X: 999, Y: 25}, layout)
	if !ok || ix != 15 || affinity {
		t.Fatalf("closest = %d, %v, %v; want 15 without affinity", ix, affinity, ok)
	}
}

// U:input/editor/display_map/text_wrapper.rs:1419-1462
func TestTextWrapperAWrapBoundaryOffsetResolvesToTheRowTheCaretIsDrawnOn(t *testing.T) {
	wrapper := newTextWrapper(gpui.FontDescriptor{Family: "Arial"}, 14, nil)
	wrapper.text = rope.FromStr("first line\nthis one wraps")
	wrapper.lines = items(
		lineItem{length: len("first line"), wrappedLines: []InputSelection{sel(0, 10)}},
		lineItem{length: len("this one wraps"), wrappedLines: []InputSelection{sel(0, 9), sel(9, 14)}},
	)
	// Offset 20 is the wrap boundary of the second buffer line: 11 + 9.
	if got := wrapper.offsetToDisplayPointWithAffinity(20, true); got != newWrapDisplayPoint(1, 0, 9) {
		t.Fatalf("with affinity = %v", got)
	}
	if got := wrapper.offsetToDisplayPointWithAffinity(20, false); got != newWrapDisplayPoint(2, 1, 0) {
		t.Fatalf("without affinity = %v", got)
	}
	if wrapper.offsetToDisplayPoint(20) != wrapper.offsetToDisplayPointWithAffinity(20, false) {
		t.Fatal("offsetToDisplayPoint must default to no affinity")
	}
	for _, affinity := range []bool{false, true} {
		if got := wrapper.offsetToDisplayPointWithAffinity(23, affinity); got != newWrapDisplayPoint(2, 1, 3) {
			t.Fatalf("offset 23 (affinity %v) = %v", affinity, got)
		}
	}
}

// U:input/editor/display_map/text_wrapper.rs:1464-1570
func TestTextWrapperOffsetToDisplayPoint(t *testing.T) {
	wrapper := newTextWrapper(gpui.FontDescriptor{Family: "Arial"}, 14, nil)
	wrapper.text = rope.FromStr("Hello, 世界!\r\nThis is second line.\nThis is third line.\n这里是第 4 行。")
	wrapper.lines = items(
		lineItem{length: len("Hello, 世界!\r"), wrappedLines: []InputSelection{sel(0, 15)}},
		lineItem{length: len("This is second line.\n"), wrappedLines: []InputSelection{sel(0, 10), sel(10, 20)}},
		lineItem{length: len("This is third line.\n"), wrappedLines: []InputSelection{sel(0, 9), sel(9, 15), sel(15, 20)}},
		lineItem{length: len("这里是第 4 行。"), wrappedLines: []InputSelection{sel(0, 22)}},
	)
	cases := []struct {
		offset int
		point  wrapDisplayPoint
	}{
		{12, newWrapDisplayPoint(0, 0, 12)},
		{15, newWrapDisplayPoint(0, 0, 15)},
		{16, newWrapDisplayPoint(1, 0, 0)},
		{21, newWrapDisplayPoint(1, 0, 5)},
		{27, newWrapDisplayPoint(2, 1, 1)},
		{37, newWrapDisplayPoint(3, 0, 0)},
		{54, newWrapDisplayPoint(5, 2, 2)},
		{59, newWrapDisplayPoint(6, 0, 2)},
	}
	for _, c := range cases {
		if got := wrapper.offsetToDisplayPoint(c.offset); got != c.point {
			t.Errorf("offsetToDisplayPoint(%d) = %v, want %v", c.offset, got, c.point)
		}
	}
	for _, c := range cases[1:] {
		if got := wrapper.displayPointToOffset(c.point); got != c.offset {
			t.Errorf("displayPointToOffset(%v) = %d, want %d", c.point, got, c.offset)
		}
	}
}

// fakeIndentWrap is the fake wrap_line of the two wrapping-indent tests:
// an indented line breaks at 5 with an indent of 2, any other line every
// 8 bytes (U:input/editor/display_map/text_wrapper.rs:1577-1595).
func fakeIndentWrap(line string, _ float32, _ int) []gpui.Boundary {
	if strings.HasPrefix(line, " ") {
		return []gpui.Boundary{{Ix: 5, NextIndent: 2}}
	}
	var out []gpui.Boundary
	for i := 8; i < len(line); i += 8 {
		out = append(out, gpui.Boundary{Ix: i})
	}
	return out
}

// U:input/editor/display_map/text_wrapper.rs:1572-1602
func TestTextWrapperWrappingIndentSameKeepsIndentReserved(t *testing.T) {
	width := float32(10)
	wrapper := newTextWrapper(gpui.FontDescriptor{Family: "Arial"}, 14, &width)
	wrapper.wrappingIndent = WrappingIndentSame
	text := rope.FromStr("  abcdefghijklmnopqrstuv")
	wrapper.updateWith(text, sel(0, text.Len()), text, fakeIndentWrap)
	line, _ := wrapper.line(0)
	if line.indent != 2 || !reflect.DeepEqual(line.wrappedLines, []InputSelection{sel(0, 5), sel(5, 24)}) {
		t.Fatalf("indent = %d, wrapped = %v", line.indent, line.wrappedLines)
	}
}

// U:input/editor/display_map/text_wrapper.rs:1604-1634
func TestTextWrapperWrappingIndentNoneContinuationLinesWrappedAtFullWidth(t *testing.T) {
	width := float32(10)
	wrapper := newTextWrapper(gpui.FontDescriptor{Family: "Arial"}, 14, &width)
	wrapper.wrappingIndent = WrappingIndentNone
	text := rope.FromStr("  abcdefghijklmnopqrstuv")
	wrapper.updateWith(text, sel(0, text.Len()), text, fakeIndentWrap)
	line, _ := wrapper.line(0)
	want := []InputSelection{sel(0, 5), sel(5, 13), sel(13, 21), sel(21, 24)}
	if line.indent != 0 || !reflect.DeepEqual(line.wrappedLines, want) {
		t.Fatalf("indent = %d, wrapped = %v", line.indent, line.wrappedLines)
	}
}

// U:input/editor/display_map/text_wrapper.rs:1636-1672
func TestTextWrapperWrapIndentOffsetsContinuationLines(t *testing.T) {
	l := newInputLineLayout(emptyShapedLines(t, 5, 10)).withWrapIndent(20)
	width := float32(10)
	layout := testLastLayout(20)
	layout.wrapWidth = &width
	layout.wrappingIndent = WrappingIndentSame
	if pos, ok := l.positionForIndex(0, layout, false); !ok || pos != (gpui.Point{}) {
		t.Fatalf("position 0 = %v", pos)
	}
	if pos, ok := l.positionForIndex(6, layout, false); !ok || pos != (gpui.Point{X: 20, Y: 20}) {
		t.Fatalf("position 6 = %v, want (20, 20)", pos)
	}
}
