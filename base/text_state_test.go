package base

import (
	"slices"
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ports of the U:text/state.rs tests (1283-3187), 83 in all. This file
// holds the 23 outside the stream_fade, range_highlights and reveal_range
// modules: 22 ported, 1 deviated. The modules are ticket 30's, in
// text_stream_fade_state_test.go (12), text_range_highlight_state_test.go
// (26) and text_range_highlight_reveal_test.go (22).
//   - deviated: update_future_yields_before_coalescing_all_queued_updates
//     (the Go drain delivers one update per 64-update batch instead of
//     yielding a poll).
//   - large_markdown_and_html_full_replacements_wait_for_background_
//     executor, html_push_str_keeps_earlier_blocks and set_text_extending_
//     html_parses_it_again run on U:text/format/html.rs (ticket 30).

func newTextViewStateEntityHTML(ta *gpui.TestApp, text string) gpui.Entity[TextViewState] {
	var state gpui.Entity[TextViewState]
	ta.Update(func(app *gpui.App) {
		state = gpui.NewEntity(app, ta.RootScope(), func(s *TextViewState, cx *gpui.Context[TextViewState]) {
			*s = TextViewStateHTML(text, cx)
		})
	})
	return state
}

func readState[R any](ta *gpui.TestApp, state gpui.Entity[TextViewState], f func(*TextViewState) R) R {
	return state.Read(ta, func(s *TextViewState, _ *gpui.App) R { return f(s) })
}

func updateState(ta *gpui.TestApp, state gpui.Entity[TextViewState], f func(*TextViewState, *gpui.Context[TextViewState])) {
	state.Update(ta, f)
}

// U:text/state.rs:1536-1546
func TestSmallFullReplaceParsesBeforeBackgroundExecutorRuns(t *testing.T) {
	ta := textTestApp(t)
	state := newTextViewStateEntity(ta, "# ready")
	readState(ta, state, func(s *TextViewState) struct{} {
		if s.source() != "# ready" || len(s.parsedContent.document.blocks) != 1 {
			t.Fatalf("source %q, %d blocks", s.source(), len(s.parsedContent.document.blocks))
		}
		return struct{}{}
	})
}

// U:text/state.rs:1548-1586
func TestLargeMarkdownAndHTMLFullReplacementsWaitForBackgroundExecutor(t *testing.T) {
	ta := textTestApp(t)
	markdown := strings.Repeat("# x\n\n", maxSyncFullReplaceBytes/5+1)
	html := "<p>" + strings.Repeat("x", maxSyncFullReplaceBytes+1) + "</p>"
	states := map[string]gpui.Entity[TextViewState]{
		markdown: newTextViewStateEntity(ta, markdown),
		html:     newTextViewStateEntityHTML(ta, html),
	}
	for text, state := range states {
		readState(ta, state, func(s *TextViewState) struct{} {
			if s.text != text || s.source() != "" || len(s.parsedContent.document.blocks) != 0 {
				t.Fatal("a large replacement parsed synchronously")
			}
			return struct{}{}
		})
	}
	ta.RunUntilParked()
	for text, state := range states {
		readState(ta, state, func(s *TextViewState) struct{} {
			if s.source() != text || len(s.parsedContent.document.blocks) == 0 {
				t.Fatalf("the background parse did not land: %d blocks", len(s.parsedContent.document.blocks))
			}
			return struct{}{}
		})
	}
}

// U:text/state.rs:1588-1631
func TestInlineSourceRangesFollowStreamedTailReparsing(t *testing.T) {
	ta := textTestApp(t)
	state := newTextViewStateEntity(ta, "中文 $a$\n\n尾 $x")
	updateState(ta, state, func(s *TextViewState, cx *gpui.Context[TextViewState]) {
		e := MarkdownExtensions{}.Plugin(newTestInlinePlugin("test").parseWith(func(node *MdastNode, _ *MarkdownParseContext) *MarkdownNode {
			if node.Kind != MdastInlineMath {
				return nil
			}
			n := NewMarkdownNode("formula", nil).Text(node.Value)
			return &n
		}))
		s.setMarkdownExtensions(&e, cx)
	})
	ta.RunUntilParked()
	updateState(ta, state, func(s *TextViewState, cx *gpui.Context[TextViewState]) { s.PushStr("^2$ 后", cx) })
	ta.RunUntilParked()
	readState(ta, state, func(s *TextViewState) struct{} {
		source := "中文 $a$\n\n尾 $x^2$ 后"
		if s.source() != source {
			t.Fatalf("source = %q", s.source())
		}
		var objects []*MarkdownNode
		for _, b := range s.parsedContent.document.blocks {
			p, ok := b.(*paragraph)
			if !ok {
				t.Fatalf("block %T", b)
			}
			for i := range p.children {
				if p.children[i].custom != nil {
					objects = append(objects, p.children[i].custom)
				}
			}
		}
		if len(objects) != 2 {
			t.Fatalf("%d objects", len(objects))
		}
		for i, md := range []string{"$a$", "$x^2$"} {
			start := strings.Index(source, md)
			r, ok := objects[i].SourceRange()
			if !ok || r != (gpui.TextRange{Start: start, End: start + len(md)}) || objects[i].AsMarkdown() != md {
				t.Errorf("object %d: range %v %v, markdown %q", i, r, ok, objects[i].AsMarkdown())
			}
		}
		return struct{}{}
	})
}

// U:text/state.rs:1633-1651
func TestAsyncFullReplaceThenPushStrPreservesCompleteSource(t *testing.T) {
	ta := textTestApp(t)
	state := newTextViewStateEntity(ta, "old")
	ta.RunUntilParked()
	replacement := strings.Repeat("x", maxSyncFullReplaceBytes+1)
	updateState(ta, state, func(s *TextViewState, cx *gpui.Context[TextViewState]) {
		s.SetText(replacement, cx)
		s.PushStr(" tail", cx)
	})
	ta.RunUntilParked()
	readState(ta, state, func(s *TextViewState) struct{} {
		if want := replacement + " tail"; s.text != want || s.source() != want {
			t.Fatalf("text/source mismatch: %d/%d", len(s.text), len(s.source()))
		}
		return struct{}{}
	})
}

// U:text/state.rs:1653-1676
func TestHTMLPushStrKeepsEarlierBlocks(t *testing.T) {
	ta := textTestApp(t)
	state := newTextViewStateEntityHTML(ta, "<p>first</p>")
	ta.RunUntilParked()
	updateState(ta, state, func(s *TextViewState, cx *gpui.Context[TextViewState]) { s.PushStr("<p>second</p>", cx) })
	ta.RunUntilParked()
	readState(ta, state, func(s *TextViewState) struct{} {
		if s.source() != "<p>first</p><p>second</p>" {
			t.Fatalf("source = %q", s.source())
		}
		var text strings.Builder
		for _, b := range s.parsedContent.document.blocks {
			text.WriteString(blockNodeText(b))
		}
		if !strings.Contains(text.String(), "first") || !strings.Contains(text.String(), "second") {
			t.Fatalf("lost a block: %q", text.String())
		}
		return struct{}{}
	})
}

// U:text/state.rs:1678-1703 (Go strings share bytes instead of an Arc).
func TestElementTextOfTheSameStringIsNotComparedAgain(t *testing.T) {
	ta := textTestApp(t)
	state := newTextViewStateEntity(ta, "")
	revision := func() int { return readState(ta, state, func(s *TextViewState) int { return s.revision }) }
	text := strings.Clone("hello")
	updateState(ta, state, func(s *TextViewState, cx *gpui.Context[TextViewState]) { s.setElementText(text, cx) })
	parsed := revision()
	if parsed == 0 {
		t.Fatal("not parsed")
	}
	updateState(ta, state, func(s *TextViewState, cx *gpui.Context[TextViewState]) { s.setElementText(text, cx) })
	equal := strings.Clone("hello")
	updateState(ta, state, func(s *TextViewState, cx *gpui.Context[TextViewState]) { s.setElementText(equal, cx) })
	if revision() != parsed {
		t.Fatal("the same text parsed again")
	}
	updateState(ta, state, func(s *TextViewState, cx *gpui.Context[TextViewState]) {
		s.PushStr(" world", cx)
		s.setElementText(equal, cx)
	})
	if got := readState(ta, state, func(s *TextViewState) string { return s.text }); got != "hello" {
		t.Fatalf("text = %q", got)
	}
}

// U:text/state.rs:1705-1732
func TestSetTextThenPushStrAppendsToReplacedContent(t *testing.T) {
	ta := textTestApp(t)
	state := newTextViewStateEntity(ta, "old")
	ta.RunUntilParked()
	updateState(ta, state, func(s *TextViewState, cx *gpui.Context[TextViewState]) {
		s.SetText("", cx)
		s.PushStr("new", cx)
		s.PushStr(" text", cx)
	})
	ta.RunUntilParked()
	for _, want := range []string{"new text", ""} {
		got := readState(ta, state, func(s *TextViewState) [2]string { return [2]string{s.text, s.source()} })
		if got != [2]string{want, want} {
			t.Fatalf("text/source = %q, want %q", got, want)
		}
		updateState(ta, state, func(s *TextViewState, cx *gpui.Context[TextViewState]) { s.SetText("", cx) })
		ta.RunUntilParked()
	}
}

// U:text/state.rs:1734-1751
func TestFullParseCoalescedWithAppendPreservesNewSelectAll(t *testing.T) {
	ta := textTestApp(t)
	state := newTextViewStateEntity(ta, "old")
	ta.RunUntilParked()
	updateState(ta, state, func(s *TextViewState, cx *gpui.Context[TextViewState]) {
		s.SetText("new", cx)
		s.PushStr(" text", cx)
		s.SelectAll(cx)
	})
	ta.RunUntilParked()
	readState(ta, state, func(s *TextViewState) struct{} {
		if !s.selectAll || strings.TrimSpace(s.SelectedText()) != "new text" {
			t.Fatalf("select all %v, text %q", s.selectAll, s.SelectedText())
		}
		return struct{}{}
	})
}

type blockSnapshot struct {
	text string
	span Span
}

func parsedBlocks(ta *gpui.TestApp, state gpui.Entity[TextViewState]) []blockSnapshot {
	return readState(ta, state, func(s *TextViewState) []blockSnapshot {
		var out []blockSnapshot
		for _, b := range s.parsedContent.document.blocks {
			snap := blockSnapshot{text: blockNodeText(b)}
			if span := blockNodeSpan(b); span != nil {
				snap.span = *span
			}
			out = append(out, snap)
		}
		return out
	})
}

// U:text/state.rs:1768-1791
func TestSetTextExtendingMarkdownAppendsAndKeepsSelection(t *testing.T) {
	ta := textTestApp(t)
	state := newTextViewStateEntity(ta, "hello")
	ta.RunUntilParked()
	revisions := func() [2]int {
		return readState(ta, state, func(s *TextViewState) [2]int { return [2]int{s.selectionRevision, s.fullUpdateRevision} })
	}
	initial := revisions()
	updateState(ta, state, func(s *TextViewState, cx *gpui.Context[TextViewState]) { s.SetText("hello world", cx) })
	ta.RunUntilParked()
	if src := readState(ta, state, func(s *TextViewState) string { return s.source() }); src != "hello world" || revisions() != initial {
		t.Fatalf("source %q revisions %v want %v", src, revisions(), initial)
	}
	updateState(ta, state, func(s *TextViewState, cx *gpui.Context[TextViewState]) { s.SetText("hello", cx) })
	ta.RunUntilParked()
	after := revisions()
	if src := readState(ta, state, func(s *TextViewState) string { return s.source() }); src != "hello" || after[0] == initial[0] || after[1] == initial[1] {
		t.Fatalf("source %q revisions %v", src, after)
	}
}

// U:text/state.rs:1793-1832
func TestSetTextStreamingMarkdownMatchesAFullParse(t *testing.T) {
	ta := textTestApp(t)
	streamed := newTextViewStateEntity(ta, "# Title")
	ta.RunUntilParked()
	filler := strings.Repeat("word ", maxSyncFullReplaceBytes/5+1)
	chunks := []string{"\n\nfirst para", "graph with `co", "de`\n\n- one\n", "- two\n\n```rust\nfn main() {", "}\n```\n\n", filler, "\n\nlast"}
	full := readState(ta, streamed, func(s *TextViewState) int { return s.fullUpdateRevision })
	text := "# Title"
	for _, c := range chunks {
		text += c
		updateState(ta, streamed, func(s *TextViewState, cx *gpui.Context[TextViewState]) { s.SetText(text, cx) })
		ta.RunUntilParked()
	}
	if got := readState(ta, streamed, func(s *TextViewState) int { return s.fullUpdateRevision }); got != full {
		t.Fatal("streaming replaced the text")
	}
	parsed := newTextViewStateEntity(ta, text)
	ta.RunUntilParked()
	if readState(ta, streamed, func(s *TextViewState) string { return s.source() }) != readState(ta, parsed, func(s *TextViewState) string { return s.source() }) {
		t.Fatal("sources differ")
	}
	if a, b := parsedBlocks(ta, streamed), parsedBlocks(ta, parsed); !slices.Equal(a, b) {
		t.Fatalf("blocks differ:\n%v\n%v", a, b)
	}
}

// U:text/state.rs:1834-1851
func TestSetTextExtendingHTMLParsesItAgain(t *testing.T) {
	ta := textTestApp(t)
	streamed := newTextViewStateEntityHTML(ta, "<ul><li>a</li>")
	ta.RunUntilParked()
	const text = "<ul><li>a</li><li>b</li></ul>"
	updateState(ta, streamed, func(s *TextViewState, cx *gpui.Context[TextViewState]) { s.SetText(text, cx) })
	ta.RunUntilParked()
	if readState(ta, streamed, func(s *TextViewState) int { return s.fullUpdateRevision }) == 0 {
		t.Fatal("extending HTML appended instead of parsing again")
	}
	parsed := newTextViewStateEntityHTML(ta, text)
	ta.RunUntilParked()
	if a, b := parsedBlocks(ta, streamed), parsedBlocks(ta, parsed); !slices.Equal(a, b) {
		t.Fatalf("blocks differ:\n%v\n%v", a, b)
	}
}

// U:text/state.rs:1853-1872
func TestSetTextExtendingAfterAParseErrorParsesItAgain(t *testing.T) {
	ta := textTestApp(t)
	state := newTextViewStateEntity(ta, "hello")
	ta.RunUntilParked()
	full := readState(ta, state, func(s *TextViewState) int { return s.fullUpdateRevision })
	updateState(ta, state, func(s *TextViewState, cx *gpui.Context[TextViewState]) {
		failed := "failed"
		s.parsedError = &failed
		s.SetText("hello world", cx)
	})
	ta.RunUntilParked()
	readState(ta, state, func(s *TextViewState) struct{} {
		if s.parsedError != nil || s.fullUpdateRevision == full || s.source() != "hello world" {
			t.Fatalf("error %v, revision %d, source %q", s.parsedError, s.fullUpdateRevision, s.source())
		}
		return struct{}{}
	})
}

// U:text/state.rs:1874-1902
func TestUpdateOptionsMergeKeepsLatestFullText(t *testing.T) {
	o := textUpdateOptions{revision: 1, pendingText: "old", appendText: true, mode: textParseCompatible}
	o.merge(textUpdateOptions{revision: 2, pendingText: "new", mode: textParseBaselineAck})
	o.merge(textUpdateOptions{revision: 3, pendingText: " text", appendText: true, mode: textParseCompatible})
	if o.revision != 3 || o.pendingText != "new text" || o.appendText {
		t.Fatalf("%+v", o)
	}
}

// U:text/state.rs:1904-1926
func TestAppendMergedIntoAsyncReplaceRemainsAReplacement(t *testing.T) {
	o := textUpdateOptions{revision: 1, pendingText: "new", mode: textParseReplace}
	o.merge(textUpdateOptions{revision: 2, pendingText: " text", appendText: true, mode: textParseCompatible})
	if o.revision != 2 || o.pendingText != "new text" || o.appendText || o.mode != textParseReplace {
		t.Fatalf("%+v", o)
	}
}

// U:text/state.rs:1928-1971 (deviated: one delivered update per coalesced
// batch instead of a yielding poll).
func TestUpdateFutureYieldsBeforeCoalescingAllQueuedUpdates(t *testing.T) {
	p := &textViewParser{format: textViewFormatMarkdown, content: emptyParsedContent()}
	const total = 128
	for revision := 1; revision <= total; revision++ {
		mode := textParseCompatible
		if revision == 1 {
			mode = textParseBaselineAck
		}
		p.pending = append(p.pending, textUpdateOptions{revision: revision, pendingText: strings.Repeat("x", 1) + "\n", appendText: revision != 1, mode: mode, markdownExtensions: &MarkdownExtensions{}})
	}
	updates := p.drain()
	if len(updates) < 2 || updates[0].revision >= total {
		t.Fatalf("one batch coalesced every update: %d batches", len(updates))
	}
	if last := updates[len(updates)-1].revision; last != total {
		t.Fatalf("last revision %d", last)
	}
}

// U:text/state.rs:1973-1996
func TestSelectAllReturnsRenderedText(t *testing.T) {
	ta := textTestApp(t)
	state := newTextViewStateEntity(ta, "**quick** value")
	ta.RunUntilParked()
	updateState(ta, state, func(s *TextViewState, cx *gpui.Context[TextViewState]) { s.SelectAll(cx) })
	readState(ta, state, func(s *TextViewState) struct{} {
		if !s.hasViewSelection() || strings.TrimSpace(s.SelectedText()) != "quick value" {
			t.Fatalf("selected %q", s.SelectedText())
		}
		return struct{}{}
	})
	updateState(ta, state, func(s *TextViewState, cx *gpui.Context[TextViewState]) { s.ClearSelection(cx) })
	readState(ta, state, func(s *TextViewState) struct{} {
		if s.hasViewSelection() || s.SelectedText() != "" {
			t.Fatalf("selection survived clear: %q", s.SelectedText())
		}
		return struct{}{}
	})
}

// U:text/state.rs:1998-2020
func TestSelectAllInSourceFormatReturnsSource(t *testing.T) {
	ta := textTestApp(t)
	const markdown = "**quick** value"
	state := newTextViewStateEntity(ta, markdown)
	ta.RunUntilParked()
	updateState(ta, state, func(s *TextViewState, cx *gpui.Context[TextViewState]) { s.SelectAll(cx) })
	if got := readState(ta, state, func(s *TextViewState) string { return strings.TrimSpace(s.SelectedText()) }); got != "quick value" {
		t.Fatalf("plain = %q", got)
	}
	updateState(ta, state, func(s *TextViewState, cx *gpui.Context[TextViewState]) {
		s.SetSelectionFormat(SelectionFormatSource, cx)
	})
	if got := readState(ta, state, func(s *TextViewState) string { return strings.TrimSpace(s.SelectedText()) }); got != markdown {
		t.Fatalf("source = %q", got)
	}
}

// U:text/state.rs:2022-2034
func TestSelectedSourceRangeReturnsFullMarkdownSourceForSelectAll(t *testing.T) {
	ta := textTestApp(t)
	const markdown = "**quick** value"
	state := newTextViewStateEntity(ta, markdown)
	ta.RunUntilParked()
	updateState(ta, state, func(s *TextViewState, cx *gpui.Context[TextViewState]) { s.SelectAll(cx) })
	readState(ta, state, func(s *TextViewState) struct{} {
		if r, ok := s.SelectedSourceRange(); !ok || r != (gpui.TextRange{Start: 0, End: len(markdown)}) {
			t.Fatalf("range %v %v", r, ok)
		}
		return struct{}{}
	})
}

// U:text/state.rs:2036-2047
func TestSelectedSourceRangeReturnsNoneForHTML(t *testing.T) {
	ta := textTestApp(t)
	state := newTextViewStateEntityHTML(ta, "<b>quick</b>")
	ta.RunUntilParked()
	updateState(ta, state, func(s *TextViewState, cx *gpui.Context[TextViewState]) { s.SelectAll(cx) })
	readState(ta, state, func(s *TextViewState) struct{} {
		if _, ok := s.SelectedSourceRange(); ok {
			t.Fatal("an HTML view mapped its selection to source")
		}
		return struct{}{}
	})
}

// U:text/state.rs:2049-2081
func TestSelectedSourceRangeKeepsGlobalOffsetsAfterIncrementalTailParse(t *testing.T) {
	ext := &MarkdownExtensions{}
	content, err := parseTextContent(textViewFormatMarkdown, emptyParsedContent(), textUpdateOptions{revision: 1, pendingText: "first\n\nsecond", mode: textParseReplace, markdownExtensions: ext})
	if err != nil {
		t.Fatal(err)
	}
	content, err = parseTextContent(textViewFormatMarkdown, content, textUpdateOptions{revision: 2, pendingText: "\n\n**écho**", appendText: true, mode: textParseCompatible, markdownExtensions: ext})
	if err != nil {
		t.Fatal(err)
	}
	p, ok := content.document.blocks[2].(*paragraph)
	if !ok {
		t.Fatalf("block 2 is %T", content.document.blocks[2])
	}
	p.state.setText(p.text())
	p.state.setSelection(&InputSelection{Start: 0, End: len("écho")})
	if r, ok := content.document.selectedSourceRange(); !ok || r != (gpui.TextRange{Start: 17, End: 22}) {
		t.Fatalf("range %v %v", r, ok)
	}
}

// U:text/state.rs:2083-2118
func TestStreamedOrderedListContinuationPreservesStart(t *testing.T) {
	ext := &MarkdownExtensions{}
	initial, err := parseTextContent(textViewFormatMarkdown, emptyParsedContent(), textUpdateOptions{revision: 1, pendingText: "3. three", mode: textParseReplace, markdownExtensions: ext})
	if err != nil {
		t.Fatal(err)
	}
	continued, err := parseTextContent(textViewFormatMarkdown, initial, textUpdateOptions{revision: 2, pendingText: "\n4. four", appendText: true, mode: textParseCompatible, markdownExtensions: ext})
	if err != nil {
		t.Fatal(err)
	}
	list, ok := continued.document.blocks[0].(*blockList)
	if !ok || list.start == nil || *list.start != 3 || len(list.children) != 2 {
		t.Fatalf("list %+v", continued.document.blocks[0])
	}
}

// U:text/state.rs:2120-2151
func TestParserRevisionReparsesSameNameInlineConfiguration(t *testing.T) {
	ta := textTestApp(t)
	state := newTextViewStateEntity(ta, "@member")
	for i, label := range []string{"Alice", "Bob"} {
		e := MarkdownExtensions{}.ParserRevision(uint64(i + 1)).Plugin(newTestInlinePlugin("test").parseWith(func(node *MdastNode, _ *MarkdownParseContext) *MarkdownNode {
			if node.Kind != MdastText {
				return nil
			}
			n := NewMarkdownNode("mention", label).Text(label)
			return &n
		}))
		updateState(ta, state, func(s *TextViewState, cx *gpui.Context[TextViewState]) { s.setMarkdownExtensions(&e, cx) })
		ta.RunUntilParked()
		got := readState(ta, state, func(s *TextViewState) string {
			return s.parsedContent.document.blocks[0].(*paragraph).children[0].custom.AsText()
		})
		if got != label {
			t.Fatalf("custom text = %q, want %q", got, label)
		}
	}
}

// U:text/state.rs:2153-2188
func TestSetMarkdownExtensionsReparsesExistingText(t *testing.T) {
	ta := textTestApp(t)
	state := newTextViewStateEntity(ta, "$TSLA.US")
	ta.RunUntilParked()
	e := MarkdownExtensions{}.BlockParser(func(node *MdastNode, cx *MarkdownParseContext) *MarkdownNode {
		if node.Kind != MdastParagraph || len(node.Children) != 1 || node.Children[0].Kind != MdastText {
			return nil
		}
		symbol, ok := strings.CutPrefix(node.Children[0].Value, "$")
		if !ok {
			return nil
		}
		source, _ := cx.NodeSource(node)
		n := NewMarkdownNode("ticker", symbol).Text("$" + symbol).Markdown(source)
		return &n
	})
	updateState(ta, state, func(s *TextViewState, cx *gpui.Context[TextViewState]) { s.setMarkdownExtensions(&e, cx) })
	ta.RunUntilParked()
	readState(ta, state, func(s *TextViewState) struct{} {
		custom, ok := s.parsedContent.document.blocks[0].(*blockCustom)
		if !ok {
			t.Fatalf("block %T", s.parsedContent.document.blocks[0])
		}
		if data, _ := MarkdownNodeData[string](custom.node); custom.node.Name() != "ticker" || data != "TSLA.US" {
			t.Fatalf("node %q data %q", custom.node.Name(), data)
		}
		return struct{}{}
	})
}
