package base

import (
	"slices"
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ports of the U:text/node.rs tests (3437-4501) that exercise the data
// model and its pure methods: 30 of 37. The other 7 test the rendering
// half; ticket 28 ports them in text_node_render_test.go:
//   - custom_inline_inherits_marks_and_resolves_link_references (3518):
//     Paragraph::inline_flow_items, InlineFlowItem's TextStyle, TestApp.
//   - table_column_uses_prepared_inline_metrics_after_resource_update
//     (3558) and table_column_of_inline_code_cells_fits_the_mono_width
//     (3610): measure_table_columns, shaped text, InlineElement, img().
//   - code_block_highlights_are_cached_by_highlighter_identity (3658) and
//     a_new_highlighter_replaces_styles_instead_of_reusing_the_cache
//     (3688): CodeBlock's highlight cache, CodeBlockHighlighterFn and
//     HighlightStyle, read only by CodeBlock::render.
//   - cloned_table_keeps_cached_table_data (4187): TableDataCache /
//     cached_table_data, read only by the table actions render
//     (U:text/node.rs:3193,3288).
//   - test_image_node_source (4217): ImageNode::source, ImageSource and the
//     decoded data-URL image (`embedded`), read only by the image render.

func ntMarks(start, end int, mark TextMark) []textMarkRange {
	return []textMarkRange{{rng: gpui.TextRange{Start: start, End: end}, mark: mark}}
}

// reconstructMarkdown is the cfg(test) reconstruct_markdown
// (U:text/node.rs:886-903).
func reconstructMarkdown(text string, marks []textMarkRange, start, end int) string {
	var source markdownSource
	source.pushText(text, marks, gpui.TextRange{Start: start, End: end})
	return source.finish()
}

func ntCustom(text, markdown string) inlineNode {
	return inlineNodeCustom(NewMarkdownNode("math", nil).Text(text).Markdown(markdown))
}

func ntParagraph(children ...inlineNode) *paragraph {
	p := defaultParagraph()
	p.children = children
	return p
}

func TestSelectedInlineObjectsCoalesceSurroundingEmphasis(t *testing.T) {
	for _, c := range []struct {
		objectMark TextMark
		expected   string
	}{
		{TextMark{}.WithItalic(), "*fore $x$ aft*"},
		{TextMark{}.WithItalic().WithBold(), "*fore **$x$** aft*"},
	} {
		italic := TextMark{}.WithItalic()
		before := newInlineNode("before ").withMarks(ntMarks(0, 7, italic))
		formula := ntCustom("x", "$x$").withMarks(ntMarks(0, 1, c.objectMark))
		after := newInlineNode(" after").withMarks(ntMarks(0, 6, italic))
		formula.state.setText("before ")
		formula.state.setSelection(mdSel(2, 7))
		formula.customSelection.set(true)
		p := ntParagraph(before, formula, after)
		p.state.setText(" after")
		p.state.setSelection(mdSel(0, 4))
		if got := p.selectedSource(); got != c.expected {
			t.Errorf("selected source = %q, want %q", got, c.expected)
		}
	}
}

func TestConsecutiveInlineObjectsCopyAtomicallyWithoutNeighboringText(t *testing.T) {
	first := ntCustom("甲²", "$甲^2$")
	second := ntCustom("b", "$b$")
	first.customSelection.set(true)
	second.customSelection.set(true)
	p := ntParagraph(first, second)
	if p.selectedText() != "甲²b" || p.selectedSource() != "$甲^2$$b$" || !p.hasSelection() {
		t.Errorf("selected = %q / %q / %v", p.selectedText(), p.selectedSource(), p.hasSelection())
	}
	p.clearSelection()
	if p.selectedText() != "" || p.selectedSource() != "" || p.hasSelection() {
		t.Errorf("after clear = %q / %q / %v", p.selectedText(), p.selectedSource(), p.hasSelection())
	}
}

func TestSelectedInlineObjectInterleavesRunsAndPreservesEnclosingMark(t *testing.T) {
	before := newInlineNode("中文 ")
	formula := ntCustom("x²", "$x^2$").withMarks(ntMarks(0, 3, TextMark{}.WithBold()))
	after := newInlineNode(" English")
	formula.state.setText("中文 ")
	formula.state.setSelection(mdSel(3, 7))
	formula.customSelection.set(true)
	p := ntParagraph(before, formula, after)
	p.state.setText(" English")
	p.state.setSelection(mdSel(0, 4))
	if got := p.selectedText(); got != "文 x² Eng" {
		t.Errorf("selected text = %q", got)
	}
	if got := p.selectedSource(); got != "文 **$x^2$** Eng" {
		t.Errorf("selected source = %q", got)
	}
}

func TestReconstructMarkdownWrapsMarkedRuns(t *testing.T) {
	marks := ntMarks(0, 4, TextMark{}.WithBold())
	if got := reconstructMarkdown("bold", marks, 0, 4); got != "**bold**" {
		t.Errorf("got %q", got)
	}
	if got := reconstructMarkdown("bold", marks, 1, 3); got != "**ol**" {
		t.Errorf("got %q", got)
	}
}

func TestReconstructMarkdownEmitsUnmarkedTextVerbatim(t *testing.T) {
	marks := ntMarks(2, 3, TextMark{}.WithCode())
	if got := reconstructMarkdown("a b c", marks, 0, 5); got != "a `b` c" {
		t.Errorf("got %q", got)
	}
	if got := reconstructMarkdown("a b c", marks, 3, 5); got != " c" {
		t.Errorf("got %q", got)
	}
}

func TestReconstructMarkdownHandlesCodeItalicStrikeLink(t *testing.T) {
	for _, c := range []struct {
		mark TextMark
		want string
	}{
		{TextMark{}.WithCode(), "`x`"},
		{TextMark{}.WithItalic(), "*x*"},
		{TextMark{}.WithStrikethrough(), "~~x~~"},
		{TextMark{}.WithLink(LinkMark{URL: "https://example.com"}), "[x](https://example.com)"},
	} {
		if got := reconstructMarkdown("x", ntMarks(0, 1, c.mark), 0, 1); got != c.want {
			t.Errorf("got %q, want %q", got, c.want)
		}
	}
}

func TestReconstructMarkdownNestedBoldItalic(t *testing.T) {
	if got := reconstructMarkdown("x", ntMarks(0, 1, TextMark{}.WithBold().WithItalic()), 0, 1); got != "***x***" {
		t.Errorf("got %q", got)
	}
}

// ntParagraphWithChildren is paragraph_with_children: the state text is the
// children's concatenation, as Paragraph::render stores it.
func ntParagraphWithChildren(children ...inlineNode) *paragraph {
	p := ntParagraph(children...)
	p.state.setText(p.text())
	return p
}

func TestParagraphSelectedSourceMapsPartialSelectionAcrossRuns(t *testing.T) {
	p := ntParagraphWithChildren(
		newInlineNode("This has ").withMarks(ntMarks(0, 9, TextMark{})),
		newInlineNode("bold").withMarks(ntMarks(0, 4, TextMark{}.WithBold())),
		newInlineNode(" text.").withMarks(ntMarks(0, 6, TextMark{})),
	)
	for _, c := range []struct {
		start, end int
		want       string
	}{
		{0, 9 + 4 + 6, "This has **bold** text."},
		{5, 16, "has **bold** te"},
		{10, 12, "**ol**"},
	} {
		p.state.setSelection(mdSel(c.start, c.end))
		if got := p.selectedSource(); got != c.want {
			t.Errorf("%d..%d: got %q, want %q", c.start, c.end, got, c.want)
		}
	}
}

func TestParagraphSelectedSourceMatchesTextWhenNoMarks(t *testing.T) {
	p := ntParagraphWithChildren(newInlineNode("plain words").withMarks(ntMarks(0, 11, TextMark{})))
	p.state.setSelection(mdSel(0, 11))
	if p.selectedSource() != "plain words" || p.selectedText() != "plain words" {
		t.Errorf("got %q / %q", p.selectedSource(), p.selectedText())
	}
}

func ntSelectedParagraph(text string) *paragraph {
	p := ntParagraphWithChildren(newInlineNode(text).withMarks(ntMarks(0, len(text), TextMark{})))
	p.state.setSelection(mdSel(0, len(text)))
	return p
}

func ntItem(checked *bool, children ...blockNode) *blockListItem {
	return &blockListItem{children: children, checked: checked}
}

func ntStart(n uint32) *uint32 { return &n }

func ntBool(b bool) *bool { return &b }

func TestHeadingSelectedSourcePrefixesHashes(t *testing.T) {
	heading := &blockHeading{level: 2, children: ntSelectedParagraph("Title")}
	if got := blockNodeSelectedText(heading, SelectionFormatSource); got != "## Title\n" {
		t.Errorf("source = %q", got)
	}
	if got := blockNodeSelectedText(heading, SelectionFormatPlain); got != "Title\n" {
		t.Errorf("plain = %q", got)
	}
}

func TestUnorderedListSelectedSourcePrefixesDash(t *testing.T) {
	list := &blockList{children: []blockNode{
		ntItem(nil, ntSelectedParagraph("one")),
		ntItem(nil, ntSelectedParagraph("two")),
	}}
	if got := blockNodeSelectedText(list, SelectionFormatSource); got != "- one\n- two\n" {
		t.Errorf("got %q", got)
	}
}

func TestOrderedListSelectedSourcePreservesStart(t *testing.T) {
	list := &blockList{ordered: true, start: ntStart(3), children: []blockNode{
		ntItem(nil, ntSelectedParagraph("three")),
		ntItem(nil, ntSelectedParagraph("four")),
	}}
	if got := blockNodeToMarkdown(list); got != "3. three\n4. four" {
		t.Errorf("markdown = %q", got)
	}
	if got := blockNodeSelectedText(list, SelectionFormatSource); got != "3. three\n4. four\n" {
		t.Errorf("source = %q", got)
	}
}

func TestOrderedListSelectedSourcePreservesNestedStartsAndContinuations(t *testing.T) {
	nested := &blockList{ordered: true, start: ntStart(4), children: []blockNode{
		ntItem(nil, ntSelectedParagraph("nested")),
		ntItem(nil, ntSelectedParagraph("again")),
	}}
	nestedList := &blockList{ordered: true, start: ntStart(3), children: []blockNode{
		ntItem(nil, ntSelectedParagraph("three"), nested),
		ntItem(nil, ntSelectedParagraph("four")),
	}}
	if got := blockNodeSelectedText(nestedList, SelectionFormatSource); got != "3. three\n   4. nested\n   5. again\n4. four\n" {
		t.Errorf("nested = %q", got)
	}
	loose := &blockListItem{children: []blockNode{ntSelectedParagraph("loose"), ntSelectedParagraph("continuation")}, spread: true}
	looseList := &blockList{ordered: true, start: ntStart(3), children: []blockNode{
		loose,
		ntItem(nil, ntSelectedParagraph("four")),
	}}
	if got := blockNodeSelectedText(looseList, SelectionFormatSource); got != "3. loose\n   continuation\n4. four\n" {
		t.Errorf("loose = %q", got)
	}
}

func TestNestedListSelectedSourceIndentsSublists(t *testing.T) {
	nested := &blockList{children: []blockNode{ntItem(nil, ntSelectedParagraph("nested"))}}
	list := &blockList{children: []blockNode{
		ntItem(nil, ntSelectedParagraph("one"), nested),
		ntItem(nil, ntSelectedParagraph("two")),
	}}
	if got := blockNodeSelectedText(list, SelectionFormatSource); got != "- one\n  - nested\n- two\n" {
		t.Errorf("got %q", got)
	}
}

func TestTaskListSelectedSourceRestoresCheckboxes(t *testing.T) {
	list := &blockList{children: []blockNode{
		ntItem(ntBool(true), ntSelectedParagraph("done")),
		ntItem(ntBool(false), ntSelectedParagraph("todo")),
	}}
	if got := blockNodeSelectedText(list, SelectionFormatSource); got != "- [x] done\n- [ ] todo\n" {
		t.Errorf("got %q", got)
	}
}

func TestBlockquoteSelectedSourcePrefixesGt(t *testing.T) {
	quote := &blockBlockquote{children: []blockNode{ntSelectedParagraph("quoted text")}}
	if got := blockNodeSelectedText(quote, SelectionFormatSource); got != "> quoted text\n" {
		t.Errorf("got %q", got)
	}
}

func TestTableSelectedSourcePipesCellsWithAlignmentRow(t *testing.T) {
	cell := func(text string) textTableCell { return textTableCell{children: ntSelectedParagraph(text)} }
	table := &textTable{
		children: []textTableRow{
			{children: []textTableCell{cell("Name"), cell("Age")}},
			{children: []textTableCell{cell("Alice"), cell("30")}},
		},
		columnAligns: []columnumnAlign{columnAlignLeft, columnAlignRight},
	}
	if got := blockNodeSelectedText(table, SelectionFormatSource); got != "| Name | Age |\n| :-- | --: |\n| Alice | 30 |\n" {
		t.Errorf("got %q", got)
	}
}

func ntPlainCell(text string) textTableCell { return textTableCell{children: newParagraph(text)} }

func ntTableOf(rows [][]textTableCell, aligns ...columnumnAlign) *textTable {
	t := &textTable{columnAligns: aligns}
	for _, r := range rows {
		t.children = append(t.children, textTableRow{children: r})
	}
	return t
}

func TestTableToMarkdownPipesCellsWithAlignmentRow(t *testing.T) {
	table := ntTableOf([][]textTableCell{
		{ntPlainCell("Name"), ntPlainCell("Age"), ntPlainCell("Score")},
		{ntPlainCell("Alice"), ntPlainCell("30"), ntPlainCell("9.5")},
	}, columnAlignLeft, columnAlignCenter, columnAlignRight)
	want := "| Name | Age | Score |\n| :-- | :-: | --: |\n| Alice | 30 | 9.5 |"
	if got := table.toMarkdown(); got != want {
		t.Errorf("got %q", got)
	}
	if got := blockNodeToMarkdown(table); got != want {
		t.Errorf("block arm = %q", got)
	}
}

func TestTableToMarkdownKeepsOuterPipesForASingleColumn(t *testing.T) {
	table := ntTableOf([][]textTableCell{{ntPlainCell("Symbol")}, {ntPlainCell("TSLA.US")}}, columnAlignLeft)
	if got := table.toMarkdown(); got != "| Symbol |\n| :-- |\n| TSLA.US |" {
		t.Errorf("got %q", got)
	}
}

func TestTableToMarkdownEscapesPipesAndKeepsInlineMarks(t *testing.T) {
	bold := textTableCell{children: ntParagraphWithChildren(newInlineNode("bold").withMarks(ntMarks(0, 4, TextMark{}.WithBold())))}
	table := ntTableOf([][]textTableCell{
		{ntPlainCell("a | b"), ntPlainCell("plain")},
		{ntPlainCell("c"), bold},
	}, columnAlignLeft, columnAlignLeft)
	if got := table.toMarkdown(); got != "| a \\| b | plain |\n| :-- | :-- |\n| c | **bold** |" {
		t.Errorf("got %q", got)
	}
}

func TestTableDataSnapshotsPlainCellsAndMarkdown(t *testing.T) {
	table := ntTableOf([][]textTableCell{
		{ntPlainCell("  Name  "), ntPlainCell("Age")},
		{ntPlainCell("Alice"), ntPlainCell("30")},
	}, columnAlignLeft, columnAlignRight)
	table.span = &Span{Start: 4, End: 42}
	data := table.tableData()
	if !slices.Equal(data.Headers, []string{"Name", "Age"}) || len(data.Rows) != 1 || !slices.Equal(data.Rows[0], []string{"Alice", "30"}) {
		t.Errorf("data = %+v", data)
	}
	if data.Markdown != table.toMarkdown() || data.Span == nil || *data.Span != (gpui.TextRange{Start: 4, End: 42}) {
		t.Errorf("data = %+v", data)
	}
}

func TestTableDataHandlesTablesWithoutRows(t *testing.T) {
	headerOnly := ntTableOf([][]textTableCell{{ntPlainCell("Name"), ntPlainCell("Age")}}, columnAlignLeft, columnAlignLeft)
	data := headerOnly.tableData()
	if !slices.Equal(data.Headers, []string{"Name", "Age"}) || len(data.Rows) != 0 || data.Markdown != "| Name | Age |\n| :-- | :-- |" {
		t.Errorf("data = %+v", data)
	}
	empty := (&textTable{}).tableData()
	if len(empty.Headers) != 0 || len(empty.Rows) != 0 || empty.Markdown != "" || empty.Span != nil {
		t.Errorf("empty = %+v", empty)
	}
}

func TestMarksRoundTripThroughReconstruction(t *testing.T) {
	wrap := func(mark TextMark) string { return reconstructMarkdown("x", ntMarks(0, 1, mark), 0, 1) }
	title := "Tip"
	for _, c := range []struct {
		mark TextMark
		want string
	}{
		{TextMark{}.WithBold(), "**x**"},
		{TextMark{}.WithItalic(), "*x*"},
		{TextMark{}.WithCode(), "`x`"},
		{TextMark{}.WithStrikethrough(), "~~x~~"},
		{TextMark{}.WithHighlight(gpui.Hsla{H: 0.15, S: 0.9, L: 0.7, A: 1}), "==x=="},
		{TextMark{}.WithUnderline(), "<u>x</u>"},
		{TextMark{}.WithLink(LinkMark{URL: "https://example.com", Title: &title}), "[x](https://example.com \"Tip\")"},
	} {
		if got := wrap(c.mark); got != c.want {
			t.Errorf("got %q, want %q", got, c.want)
		}
	}
}

func TestDocumentSelectedSourceSlicesCoveredBlocksFromTheSource(t *testing.T) {
	source := "start\n\n3. _one_\n4. two\n\n---\n\nend"
	list := "3. _one_\n4. two"
	listStart := strings.Index(source, list)
	ruleStart := strings.Index(source, "---")
	doc := &parsedDocument{source: source, blocks: []blockNode{
		ntSelectedParagraph("start"),
		&blockList{ordered: true, start: ntStart(3), span: &Span{Start: listStart, End: listStart + len(list)}},
		&blockHorizontalRule{span: &Span{Start: ruleStart, End: ruleStart + 3}},
		ntSelectedParagraph("end"),
	}}
	if got := doc.selectedText(SelectionFormatSource, nil); got != source {
		t.Errorf("got %q", got)
	}
}

func ntImageParagraph(alt, url string) *paragraph {
	return ntParagraph(inlineNodeImage(ImageNode{URL: url, Alt: &alt}))
}

func TestDocumentSelectedSourceIncludesEnclosedImage(t *testing.T) {
	source := "before\n\n![alt](https://example.com/i.png)\n\nafter"
	imageMarkdown := "![alt](https://example.com/i.png)"
	start := strings.Index(source, imageMarkdown)
	image := ntImageParagraph("alt", "https://example.com/i.png")
	image.span = &Span{Start: start, End: start + len(imageMarkdown)}
	doc := &parsedDocument{source: source, blocks: []blockNode{
		ntSelectedParagraph("before"), image, ntSelectedParagraph("after"),
	}}
	if got := doc.selectedText(SelectionFormatSource, nil); got != source {
		t.Errorf("got %q", got)
	}
}

func TestDocumentSelectedSourceDropsUnenclosedImage(t *testing.T) {
	doc := &parsedDocument{blocks: []blockNode{
		ntSelectedParagraph("before"), ntImageParagraph("alt", "u"),
	}}
	if got := doc.selectedText(SelectionFormatSource, nil); got != "before" {
		t.Errorf("got %q", got)
	}
}

func ntSelectedCodeBlock(code string, lang *string) *CodeBlock {
	block := newCodeBlock(code, lang, nil)
	block.state.setSelection(mdSel(0, len(code)))
	return block
}

func TestCodeBlockSelectedSourceWrapsInFenceWithLang(t *testing.T) {
	lang := "rust"
	block := ntSelectedCodeBlock("let x = 1;\n", &lang)
	code := strings.TrimRight(blockNodeSelectedText(block, SelectionFormatPlain), "\n")
	source := blockNodeSelectedText(block, SelectionFormatSource)
	if source != "```rust\n"+code+"\n```\n" || !strings.HasPrefix(source, "```rust\n") || !strings.HasSuffix(strings.TrimSpace(source), "\n```") {
		t.Errorf("got %q", source)
	}
}

func TestCodeBlockSelectedSourceWithoutLang(t *testing.T) {
	block := ntSelectedCodeBlock("plain\n", nil)
	code := strings.TrimRight(blockNodeSelectedText(block, SelectionFormatPlain), "\n")
	if got := blockNodeSelectedText(block, SelectionFormatSource); got != "```\n"+code+"\n```\n" {
		t.Errorf("got %q", got)
	}
}

func TestDocumentSelectedSourceJoinsBlocksWithBlankLine(t *testing.T) {
	lang := "rust"
	doc := &parsedDocument{blocks: []blockNode{
		&blockHeading{level: 1, children: ntSelectedParagraph("Title")},
		ntSelectedParagraph("A paragraph."),
		ntSelectedCodeBlock("let x = 1;\n", &lang),
		&blockList{ordered: true, start: ntStart(1), children: []blockNode{
			ntItem(nil, ntSelectedParagraph("one")),
			ntItem(nil, ntSelectedParagraph("two")),
		}},
	}}
	want := "# Title\n\nA paragraph.\n\n```rust\nlet x = 1;\n```\n\n1. one\n2. two"
	if got := doc.selectedText(SelectionFormatSource, nil); got != want {
		t.Errorf("got %q", got)
	}
}

// Go-side: the PartialEq ports the TextViewState tests compare reparsed
// documents with (U:text/state.rs:1831,1850). Selection state is not part
// of equality.
func TestParsedDocumentEqualityComparesTreesNotSelection(t *testing.T) {
	source := "# T\n\n- [x] *a* `b` ![i](u \"t\")\n\n| x |\n| --: |\n| y |\n\n```go\nz\n```\n\n[r]: /u\n\n---"
	first := mdParse(t, source, &nodeContext{})
	second := mdParse(t, source, &nodeContext{})
	if !first.equal(second) {
		t.Fatal("two parses of one source differ")
	}
	mdFirstParagraphIn(first.blocks).state.setSelection(mdSel(0, 1))
	if !first.equal(second) {
		t.Error("selection state changed equality")
	}
	for _, other := range []string{strings.Replace(source, "*a*", "**a**", 1), strings.Replace(source, "--:", ":-:", 1), source + "\n"} {
		if first.equal(mdParse(t, other, &nodeContext{})) {
			t.Errorf("%q compares equal to %q", other, source)
		}
	}
}

func TestCodeBlockEqualityIncludesCodeContent(t *testing.T) {
	lang := "rust"
	first := newCodeBlock("let value = 1;", &lang, nil)
	second := newCodeBlock("let value = 2;", &lang, nil)
	if first.equal(second) {
		t.Error("code blocks with different code compare equal")
	}
}
