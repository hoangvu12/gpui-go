package base

import (
	"slices"
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ports of the U:text/state.rs range_highlights module tests (2190-2643):
// 26 of 26, ported.

func rhState(t *testing.T, ta *gpui.TestApp, markdown string) gpui.Entity[TextViewState] {
	t.Helper()
	state := newTextViewStateEntity(ta, markdown)
	ta.RunUntilParked()
	return state
}

func rhHighlights(ranges []gpui.TextRange) []RangeHighlight {
	out := make([]RangeHighlight, len(ranges))
	for i, r := range ranges {
		out[i] = NewRangeHighlight(r, rhColor())
	}
	return out
}

// rhSet highlights ranges of the current rendered text.
func rhSet(ta *gpui.TestApp, state gpui.Entity[TextViewState], ranges ...gpui.TextRange) error {
	var err error
	updateState(ta, state, func(s *TextViewState, cx *gpui.Context[TextViewState]) {
		err = s.SetRangeHighlights(rhHighlights(ranges), cx)
	})
	return err
}

func rhMustSet(t *testing.T, ta *gpui.TestApp, state gpui.Entity[TextViewState], ranges ...gpui.TextRange) {
	t.Helper()
	if err := rhSet(ta, state, ranges...); err != nil {
		t.Fatal(err)
	}
}

// rhPainted is the ranges leaf key paints, in its own byte space.
func rhPainted(ta *gpui.TestApp, state gpui.Entity[TextViewState], key textLeafKey) []gpui.TextRange {
	return readState(ta, state, func(s *TextViewState) []gpui.TextRange {
		var out []gpui.TextRange
		for _, bg := range s.rangeHighlights.backgrounds(key) {
			out = append(out, bg.rng)
		}
		return out
	})
}

func rhWant(t *testing.T, ta *gpui.TestApp, state gpui.Entity[TextViewState], key textLeafKey, want ...gpui.TextRange) {
	t.Helper()
	if got := rhPainted(ta, state, key); !slices.Equal(got, want) {
		t.Errorf("%+v paints %v, want %v", key, got, want)
	}
}

func rhHas(ta *gpui.TestApp, state gpui.Entity[TextViewState]) bool {
	return readState(ta, state, func(s *TextViewState) bool { return s.rangeHighlights != nil })
}

func rhText(ta *gpui.TestApp, state gpui.Entity[TextViewState]) string {
	return readState(ta, state, func(s *TextViewState) string { return s.RenderedText().String() })
}

// U:text/state.rs:2241-2253
func TestRenderedTextIsThePlainCopyText(t *testing.T) {
	ta := textTestApp(t)
	state := rhState(t, ta, "# Title\n\nhello **world** \\*\n\n- item\n\n| a | b |\n|---|---|\n| c | d |\n\n```\nlet x\n```")
	if got := rhText(ta, state); got != "Title\nhello world *\nitem\na b\nc d\n\nlet x\n" {
		t.Fatalf("rendered text = %q", got)
	}
}

// U:text/state.rs:2255-2260
func TestAMatchAcrossMarksPaintsInItsParagraph(t *testing.T) {
	ta := textTestApp(t)
	state := rhState(t, ta, "hello **world**")
	rhMustSet(t, ta, state, mdRange(0, len("hello world")))
	rhWant(t, ta, state, *textLeafBlock(0), mdRange(0, 11))
}

// U:text/state.rs:2262-2271
func TestRepeatedTextMapsToTheOccurrenceAddressed(t *testing.T) {
	ta := textTestApp(t)
	state := rhState(t, ta, "foo\n\nfoo")
	second := strings.LastIndex(rhText(ta, state), "foo")
	rhMustSet(t, ta, state, mdRange(second, second+3))
	rhWant(t, ta, state, *textLeafBlock(0))
	rhWant(t, ta, state, *textLeafBlock(5), mdRange(0, 3))
}

// U:text/state.rs:2273-2280
func TestARangeAcrossBlocksSkipsTheSeparator(t *testing.T) {
	ta := textTestApp(t)
	state := rhState(t, ta, "ab\n\ncd")
	// "ab\ncd\n": 1..4 is "b\nc".
	rhMustSet(t, ta, state, mdRange(1, 4))
	rhWant(t, ta, state, *textLeafBlock(0), mdRange(1, 2))
	rhWant(t, ta, state, *textLeafBlock(4), mdRange(0, 1))
}

// U:text/state.rs:2282-2292
func TestTableCellsAndCodeBlocksAreLeaves(t *testing.T) {
	ta := textTestApp(t)
	state := rhState(t, ta, "| a | b |\n|---|---|\n| c | d |\n\n```\nlet x\n```")
	// "a b\nc d\n\nlet x\n"
	rhMustSet(t, ta, state, mdRange(0, 3), mdRange(6, 7), mdRange(9, 12))
	rhWant(t, ta, state, *textLeafTableCell(0, 0), mdRange(0, 1))
	rhWant(t, ta, state, *textLeafTableCell(0, 1), mdRange(0, 1))
	rhWant(t, ta, state, *textLeafTableCell(0, 3), mdRange(0, 1))
	codeStart := len("| a | b |\n|---|---|\n| c | d |\n\n")
	rhWant(t, ta, state, *textLeafBlock(codeStart), mdRange(0, 3))
}

// U:text/state.rs:2294-2309
func TestInvalidRangesRejectTheWholeSet(t *testing.T) {
	ta := textTestApp(t)
	state := rhState(t, ta, "中文\n\nab")
	// "中文\nab\n"
	rhMustSet(t, ta, state, mdRange(0, len("中")))
	for _, invalid := range []gpui.TextRange{{Start: 6, End: 3}, mdRange(0, 1), mdRange(0, 100)} {
		err := rhSet(ta, state, mdRange(0, 3), invalid)
		if err != (RangeHighlightError{Kind: RangeHighlightInvalidRange, Index: 1}) {
			t.Errorf("%v: %v", invalid, err)
		}
	}
	// The rejected sets left the earlier highlight in place.
	rhWant(t, ta, state, *textLeafBlock(0), mdRange(0, 3))
}

// U:text/state.rs:2311-2334
func TestTextOutsideEveryBlockIsLeftUnpainted(t *testing.T) {
	ta := textTestApp(t)
	source := "foo one\n\n<div>foo two</div>\n\n| foo | x |\n|---|---|\n\nfoo three"
	state := rhState(t, ta, source)
	text := rhText(ta, state)
	// Every "foo" and " ", an empty range, and the separator after the
	// first block: the HTML block's text and the separators are not any
	// block's text, so they paint nothing, but nothing fails.
	var ranges []gpui.TextRange
	for _, needle := range []string{"foo", " "} {
		for i := 0; ; {
			ix := strings.Index(text[i:], needle)
			if ix < 0 {
				break
			}
			ranges = append(ranges, mdRange(i+ix, i+ix+len(needle)))
			i += ix + len(needle)
		}
	}
	ranges = append(ranges, mdRange(3, 3), mdRange(7, 8))
	rhMustSet(t, ta, state, ranges...)
	rhWant(t, ta, state, *textLeafBlock(0), mdRange(0, 3), mdRange(3, 4))
	rhWant(t, ta, state, *textLeafTableCell(strings.Index(source, "| foo"), 0), mdRange(0, 3))
	rhWant(t, ta, state, *textLeafBlock(strings.Index(source, "foo three")), mdRange(0, 3), mdRange(3, 4))
}

// U:text/state.rs:2336-2341
func TestALaterHighlightPaintsOverAnEarlierOne(t *testing.T) {
	ta := textTestApp(t)
	state := rhState(t, ta, "abcdef")
	rhMustSet(t, ta, state, mdRange(0, 6), mdRange(2, 3))
	rhWant(t, ta, state, *textLeafBlock(0), mdRange(0, 6), mdRange(2, 3))
}

// U:text/state.rs:2343-2353
func TestHTMLTextIsUnsupported(t *testing.T) {
	ta := textTestApp(t)
	html := newTextViewStateEntityHTML(ta, "<p>one</p>")
	ta.RunUntilParked()
	if err := rhSet(ta, html, mdRange(0, 3)); err != (RangeHighlightError{Kind: RangeHighlightUnsupported}) {
		t.Fatalf("err = %v", err)
	}
}

func rhFormulaExtensions() *MarkdownExtensions {
	e := MarkdownExtensions{}.Plugin(newTestInlinePlugin("test").parseWith(func(node *MdastNode, _ *MarkdownParseContext) *MarkdownNode {
		if node.Kind != MdastInlineMath {
			return nil
		}
		n := NewMarkdownNode("formula", nil).Text(node.Value)
		return &n
	}))
	return &e
}

// U:text/state.rs:2355-2380
func TestInlineObjectsAreSkipped(t *testing.T) {
	ta := textTestApp(t)
	state := rhState(t, ta, "x $a$ y")
	updateState(ta, state, func(s *TextViewState, cx *gpui.Context[TextViewState]) {
		s.setMarkdownExtensions(rhFormulaExtensions(), cx)
	})
	ta.RunUntilParked()
	// "x a y\n", where "a" is the formula.
	rhMustSet(t, ta, state, mdRange(2, 3))
	rhWant(t, ta, state, *textLeafBlock(0))
	rhMustSet(t, ta, state, mdRange(0, 5))
	rhWant(t, ta, state, *textLeafBlock(0), mdRange(0, 2), mdRange(3, 5))
}

// U:text/state.rs:2382-2394
func TestPushStrKeepsEarlierBlocksAndClipsTheChangedTail(t *testing.T) {
	ta := textTestApp(t)
	state := rhState(t, ta, "first\n\na **b")
	// "first\na **b\n"
	rhMustSet(t, ta, state, mdRange(0, 5), mdRange(6, 11))
	// Closing the emphasis renders the tail as "a bc".
	sfPushStr(ta, state, "c**")
	rhWant(t, ta, state, *textLeafBlock(0), mdRange(0, 5))
	rhWant(t, ta, state, *textLeafBlock(7), mdRange(0, 2))
}

// U:text/state.rs:2396-2405
func TestPushStrKeepsABlockWhoseTextIsUnchanged(t *testing.T) {
	ta := textTestApp(t)
	state := rhState(t, ta, "first\n\nsecond")
	rhMustSet(t, ta, state, mdRange(6, 12))
	// A setext underline turns the paragraph into a heading at the same
	// source start, rendering the same text.
	sfPushStr(ta, state, "\n===")
	rhWant(t, ta, state, *textLeafBlock(7), mdRange(0, 6))
}

// U:text/state.rs:2407-2418
func TestPushStrDropsHighlightsOfALeafThatIsGone(t *testing.T) {
	ta := textTestApp(t)
	state := rhState(t, ta, "first\n\n| a |")
	rhMustSet(t, ta, state, mdRange(0, 5), mdRange(6, 11))
	// A delimiter row turns the paragraph into a table, whose text lives in
	// cells instead.
	sfPushStr(ta, state, "\n|---|")
	rhWant(t, ta, state, *textLeafBlock(0), mdRange(0, 5))
	rhWant(t, ta, state, *textLeafBlock(7))
	rhWant(t, ta, state, *textLeafTableCell(7, 0))
}

// U:text/state.rs:2420-2433
func TestReplacingTextDropsHighlightsOnlyWhereItChanged(t *testing.T) {
	ta := textTestApp(t)
	state := rhState(t, ta, "first\n\nsecond")
	rhMustSet(t, ta, state, mdRange(0, 5), mdRange(6, 12))
	sfSetText(ta, state, "first\n\nchanged")
	rhWant(t, ta, state, *textLeafBlock(0), mdRange(0, 5))
	rhWant(t, ta, state, *textLeafBlock(7))

	sfSetText(ta, state, strings.Repeat("x", maxSyncFullReplaceBytes+1))
	if rhHas(ta, state) {
		t.Fatal("a large replacement kept highlights")
	}
}

// U:text/state.rs:2435-2452
func TestAHighlightFollowsItsBlockPastAnEarlierEdit(t *testing.T) {
	ta := textTestApp(t)
	state := rhState(t, ta, "foo\n\nbar\n\nbar")
	// "foo\nbar\nbar\n": the first "bar", in the block at source 5.
	rhMustSet(t, ta, state, mdRange(4, 7))
	// Deleting "foo" moves that "bar" to 0, and the second one to 5.
	sfSetText(ta, state, "bar\n\nbar")
	rhWant(t, ta, state, *textLeafBlock(0), mdRange(0, 3))
	rhWant(t, ta, state, *textLeafBlock(5))

	state = rhState(t, ta, "ERROR a\n\nERROR b\n\nERROR c")
	rhMustSet(t, ta, state, mdRange(8, 15))
	sfSetText(ta, state, "ERROR b\n\nERROR c")
	rhWant(t, ta, state, *textLeafBlock(0), mdRange(0, 7))
	rhWant(t, ta, state, *textLeafBlock(9))
}

// U:text/state.rs:2454-2463
func TestAppendingACopyOfTheLastBlockKeepsTheHighlightOnIt(t *testing.T) {
	ta := textTestApp(t)
	state := rhState(t, ta, "a\n\nfoo")
	// "a\nfoo\n"
	rhMustSet(t, ta, state, mdRange(2, 5))
	sfSetText(ta, state, "a\n\nfoo\n\nfoo")
	rhWant(t, ta, state, *textLeafBlock(3), mdRange(0, 3))
	rhWant(t, ta, state, *textLeafBlock(8))
}

// U:text/state.rs:2465-2484
func TestDeletingATableRowDropsHighlightsInTheRowsItMoves(t *testing.T) {
	table := "| a | b |\n|---|---|\n| x | 1 |\n| x | 2 |"
	withoutMiddleRow := "| a | b |\n|---|---|\n| x | 2 |"
	// "a b\nx 1\nx 2\n\n": the last row's cells, then the middle row's "x".
	for _, ranges := range [][]gpui.TextRange{{mdRange(8, 9), mdRange(10, 11)}, {mdRange(4, 5)}} {
		ta := textTestApp(t)
		state := rhState(t, ta, table)
		rhMustSet(t, ta, state, append([]gpui.TextRange{mdRange(0, 1)}, ranges...)...)
		sfSetText(ta, state, withoutMiddleRow)
		rhWant(t, ta, state, *textLeafTableCell(0, 0), mdRange(0, 1))
		for cell := 1; cell < 4; cell++ {
			rhWant(t, ta, state, *textLeafTableCell(0, cell))
		}
	}
}

// U:text/state.rs:2486-2497
func TestStreamingTableRowsKeepsTheHighlightsOfEarlierRows(t *testing.T) {
	ta := textTestApp(t)
	state := rhState(t, ta, "| a | b |\n|---|---|\n| x | 1 |")
	// "a b\nx 1\n\n"
	rhMustSet(t, ta, state, mdRange(0, 1), mdRange(4, 5))
	sfSetText(ta, state, "| a | b |\n|---|---|\n| x | 1 |\n| y | 2 |")
	rhWant(t, ta, state, *textLeafTableCell(0, 0), mdRange(0, 1))
	rhWant(t, ta, state, *textLeafTableCell(0, 2), mdRange(0, 1))
}

// U:text/state.rs:2499-2507
func TestAnEditInAnEarlierBlockKeepsLaterHighlights(t *testing.T) {
	ta := textTestApp(t)
	state := rhState(t, ta, "one\n\ntwo")
	rhMustSet(t, ta, state, mdRange(0, 3), mdRange(4, 7))
	sfSetText(ta, state, "one!\n\ntwo")
	rhWant(t, ta, state, *textLeafBlock(0), mdRange(0, 3))
	rhWant(t, ta, state, *textLeafBlock(6), mdRange(0, 3))
}

// U:text/state.rs:2509-2531
func TestAnAppendAfterAFullUpdateComparesEveryBlock(t *testing.T) {
	ta := textTestApp(t)
	state := rhState(t, ta, "first\n\nsecond")
	rhMustSet(t, ta, state, mdRange(0, 5))
	// An append result whose baseline was a full update that has not been
	// committed: its earlier blocks are not the current ones.
	content, err := parseTextContent(textViewFormatMarkdown, emptyParsedContent(), textUpdateOptions{pendingText: "FIRST\n\nsecond", mode: textParseReplace, markdownExtensions: &MarkdownExtensions{}})
	if err != nil {
		t.Fatal(err)
	}
	updateState(ta, state, func(s *TextViewState, _ *gpui.Context[TextViewState]) {
		s.fullUpdateRevision = s.committedRevision + 1
		s.reconcileRangeHighlights(content.document, s.committedRevision+2, true)
	})
	if rhHas(ta, state) {
		t.Fatal("highlights survived a changed block")
	}
}

// U:text/state.rs:2533-2542
func TestStreamingThroughSetTextKeepsHighlights(t *testing.T) {
	ta := textTestApp(t)
	state := rhState(t, ta, "first\n\nsec")
	// "first\nsec\n"
	rhMustSet(t, ta, state, mdRange(0, 5), mdRange(6, 9))
	sfSetText(ta, state, "first\n\nsecond")
	rhWant(t, ta, state, *textLeafBlock(0), mdRange(0, 5))
	rhWant(t, ta, state, *textLeafBlock(7), mdRange(0, 3))
}

// U:text/state.rs:2544-2564
func TestAFullParseMergedWithAnAppendComparesEveryBlock(t *testing.T) {
	ta := textTestApp(t)
	state := rhState(t, ta, "")
	// The small text is committed at once, and its background parse merges
	// with the append into one full parse, in which the definition turns
	// the earlier `[foo]` into the link text `foo`.
	updateState(ta, state, func(s *TextViewState, cx *gpui.Context[TextViewState]) {
		s.SetText("[foo] and some text\n\nmore", cx)
		some := strings.Index(s.RenderedText().String(), "some")
		if err := s.SetRangeHighlights(rhHighlights([]gpui.TextRange{mdRange(some, some+4)}), cx); err != nil {
			t.Fatal(err)
		}
		s.PushStr("\n\n[foo]: https://example.com", cx)
	})
	ta.RunUntilParked()
	if text := rhText(ta, state); !strings.HasPrefix(text, "foo and some") {
		t.Fatalf("rendered text = %q", text)
	}
	rhWant(t, ta, state, *textLeafBlock(0))
}

// U:text/state.rs:2566-2577
func TestAFullUpdateBeforeAnAppendDropsReplacedHighlights(t *testing.T) {
	ta := textTestApp(t)
	state := rhState(t, ta, "first")
	rhMustSet(t, ta, state, mdRange(0, 5))
	updateState(ta, state, func(s *TextViewState, cx *gpui.Context[TextViewState]) {
		s.SetText(strings.Repeat("x", maxSyncFullReplaceBytes+1), cx)
		s.PushStr(" tail", cx)
	})
	ta.RunUntilParked()
	if rhHas(ta, state) {
		t.Fatal("highlights survived the replacement")
	}
}

// U:text/state.rs:2579-2591
func TestReparsingUnchangedTextKeepsHighlights(t *testing.T) {
	ta := textTestApp(t)
	state := rhState(t, ta, "first")
	rhMustSet(t, ta, state, mdRange(0, 5))
	updateState(ta, state, func(s *TextViewState, cx *gpui.Context[TextViewState]) {
		e := MarkdownExtensions{}.ParserRevision(1)
		s.setMarkdownExtensions(&e, cx)
	})
	ta.RunUntilParked()
	rhWant(t, ta, state, *textLeafBlock(0), mdRange(0, 5))
}

// U:text/state.rs:2605-2634
func TestHighlightsPaintAcrossInlineCodeTablesAndCodeBlocks(t *testing.T) {
	ta := textTestApp(t)
	markdown := "wrapping text with `inline code` and a [link](https://x.y) that wraps\n\n| a | b |\n|---|---|\n| c | d |\n\n```\nlet x = 1;\n```"
	state := newTextViewStateEntity(ta, markdown)
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.Div().Block().W(gpui.Px(120)).Child(NewTextView(state).Selectable(true)).IntoElement()
	})
	drawText(t, cx)
	text := rhText(ta, state)
	code := strings.Index(text, "code")
	// Part of the inline code, every character, and the whole text.
	ranges := []gpui.TextRange{mdRange(code, code+2)}
	for ix, c := range text {
		ranges = append(ranges, mdRange(ix, ix+len(string(c))))
	}
	ranges = append(ranges, mdRange(0, len(text)))
	kept := ranges[:0]
	for _, r := range ranges {
		if strings.TrimSpace(text[r.Start:r.End]) != "" {
			kept = append(kept, r)
		}
	}
	rhMustSet(t, ta, state, kept...)
	drawText(t, cx)
	if got := rhPainted(ta, state, *textLeafBlock(0)); len(got) == 0 || got[0] != mdRange(code, code+2) {
		t.Fatalf("first background %v, want %v", got, mdRange(code, code+2))
	}
}

// U:text/state.rs:2636-2642
func TestClearRangeHighlightsRemovesThem(t *testing.T) {
	ta := textTestApp(t)
	state := rhState(t, ta, "first")
	rhMustSet(t, ta, state, mdRange(0, 5))
	updateState(ta, state, func(s *TextViewState, cx *gpui.Context[TextViewState]) { s.ClearRangeHighlights(cx) })
	if rhHas(ta, state) {
		t.Fatal("highlights survived the clear")
	}
}
