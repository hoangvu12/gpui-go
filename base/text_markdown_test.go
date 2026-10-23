package base

import (
	"slices"
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ports of the U:text/format/markdown.rs tests (1084-2284), one Go test
// per Rust test, named Test + the Rust name in CamelCase.

// testInlinePlugin is U:text/markdown_ext.rs:462-524 TestInlinePlugin: an
// inline plugin that exercises the application registration path.
type testInlinePlugin struct {
	name   string
	parser MarkdownBlockParserFn
}

func newTestInlinePlugin(name string) testInlinePlugin { return testInlinePlugin{name: name} }

func (p testInlinePlugin) parseWith(parser MarkdownBlockParserFn) testInlinePlugin {
	p.parser = parser
	return p
}

func (p testInlinePlugin) Name() string { return p.name }

func (p testInlinePlugin) Parse(node *MdastNode, cx *MarkdownParseContext) *MarkdownNode {
	if p.parser == nil {
		return nil
	}
	return p.parser(node, cx)
}

func mdFirstParagraph(block blockNode) *paragraph {
	switch v := block.(type) {
	case *paragraph:
		return v
	case *blockHeading:
		return v.children
	case *blockRoot:
		return mdFirstParagraphIn(v.children)
	case *blockBlockquote:
		return mdFirstParagraphIn(v.children)
	case *blockList:
		return mdFirstParagraphIn(v.children)
	case *blockListItem:
		return mdFirstParagraphIn(v.children)
	}
	return nil
}

func mdFirstParagraphIn(blocks []blockNode) *paragraph {
	for _, b := range blocks {
		if p := mdFirstParagraph(b); p != nil {
			return p
		}
	}
	return nil
}

func mdFirstCodeBlock(block blockNode) *CodeBlock {
	var children []blockNode
	switch v := block.(type) {
	case *CodeBlock:
		return v
	case *blockRoot:
		children = v.children
	case *blockBlockquote:
		children = v.children
	case *blockList:
		children = v.children
	case *blockListItem:
		children = v.children
	}
	for _, c := range children {
		if code := mdFirstCodeBlock(c); code != nil {
			return code
		}
	}
	return nil
}

func mdParse(t *testing.T, source string, cx *nodeContext) *parsedDocument {
	t.Helper()
	doc, err := markdownParse(source, cx)
	if err != nil {
		t.Fatalf("parse %q: %v", source, err)
	}
	return doc
}

func mdSel(start, end int) *InputSelection {
	s := NewInputSelection(start, end)
	return &s
}

func mdRange(start, end int) gpui.TextRange { return gpui.TextRange{Start: start, End: end} }

// mdSelectedRenderedRange is selected_rendered_range
// (U:text/format/markdown.rs:1117-1131).
func mdSelectedRenderedRange(t *testing.T, source string, start, end int) (gpui.TextRange, bool) {
	t.Helper()
	doc := mdParse(t, source, &nodeContext{})
	p := mdFirstParagraphIn(doc.blocks)
	if p == nil {
		t.Fatalf("expected paragraph in %q", source)
	}
	p.state.setText(p.text())
	p.state.setSelection(mdSel(start, end))
	return doc.selectedSourceRange()
}

func mdExpectRange(t *testing.T, got gpui.TextRange, ok bool, want gpui.TextRange) {
	t.Helper()
	if !ok || got != want {
		t.Errorf("selected source range = %v (ok=%v), want %v", got, ok, want)
	}
}

func mdExpectSelected(t *testing.T, source string, start, end int, want gpui.TextRange) {
	t.Helper()
	got, ok := mdSelectedRenderedRange(t, source, start, end)
	mdExpectRange(t, got, ok, want)
}

func mdSetCodeSelection(code *CodeBlock, start, end int) {
	code.state.setSelection(mdSel(start, end))
}

// mdSelectedCodeRange is selected_code_range
// (U:text/format/markdown.rs:1137-1147).
func mdSelectedCodeRange(t *testing.T, source string, start, end int) (gpui.TextRange, bool) {
	t.Helper()
	doc := mdParse(t, source, &nodeContext{})
	var code *CodeBlock
	for _, b := range doc.blocks {
		if code = mdFirstCodeBlock(b); code != nil {
			break
		}
	}
	if code == nil {
		t.Fatalf("expected code block in %q", source)
	}
	mdSetCodeSelection(code, start, end)
	return doc.selectedSourceRange()
}

func mdExpectCode(t *testing.T, source string, start, end int, want gpui.TextRange) {
	t.Helper()
	got, ok := mdSelectedCodeRange(t, source, start, end)
	mdExpectRange(t, got, ok, want)
}

func mdSegs(pairs ...[4]int) []sourceSegment {
	out := make([]sourceSegment, len(pairs))
	for i, p := range pairs {
		out[i] = sourceSegment{rendered: mdRange(p[0], p[1]), source: mdRange(p[2], p[3])}
	}
	return out
}

func mdInlineExtensions(name string, parser MarkdownBlockParserFn) *MarkdownExtensions {
	e := MarkdownExtensions{}.Plugin(newTestInlinePlugin(name).parseWith(parser))
	return &e
}

func TestSelectedSourceRangeUsesTheSelectedIdenticalStyledOccurrence(t *testing.T) {
	mdExpectSelected(t, "**same** then **same**", 10, 14, mdRange(16, 20))
}

func TestSelectedSourceRangeMapsPartialStyledText(t *testing.T) {
	mdExpectSelected(t, "**same** then **same**", 11, 13, mdRange(17, 19))
}

func TestSourceSegmentsCompactContiguousOneToOneMappings(t *testing.T) {
	source := "plain text"
	doc := mdParse(t, source, &nodeContext{})
	p := mdFirstParagraph(doc.blocks[0])
	if want := mdSegs([4]int{0, len(source), 0, len(source)}); !slices.Equal(p.children[0].sourceSegments, want) {
		t.Errorf("segments = %v, want %v", p.children[0].sourceSegments, want)
	}
	mdExpectSelected(t, source, 2, 7, mdRange(2, 7))
}

func TestSourceAlignmentCompactsLongWhitespaceRuns(t *testing.T) {
	for _, raw := range []string{
		strings.Repeat("a ", 16_384),
		"a" + strings.Repeat(" ", 32_768) + "b",
		"a" + strings.Repeat(" \t", 16_384) + "b\n",
	} {
		segments := markdownAlignedSourceSegments(raw, raw, 7, true)
		if want := mdSegs([4]int{0, len(raw), 7, 7 + len(raw)}); !slices.Equal(segments, want) {
			t.Errorf("segments = %v, want %v", segments, want)
		}
		if cap(segments) >= 64 {
			t.Errorf("compaction must not retain a per-character allocation (cap %d)", cap(segments))
		}
	}
}

func TestSourceAlignmentPreservesMultilineCodeAndFinalNewline(t *testing.T) {
	raw := strings.Repeat("x\n", 16_384)
	rendered := raw[:len(raw)-1]
	if got, want := markdownAlignedSourceSegments(raw, rendered, 4, false), mdSegs([4]int{0, len(rendered), 4, 4 + len(rendered)}); !slices.Equal(got, want) {
		t.Errorf("segments = %v, want %v", got, want)
	}
	if got, want := markdownAlignedSourceSegments("a\n> \n", "a\n\n", 0, false), mdSegs([4]int{0, 2, 0, 2}, [4]int{2, 3, 2, 5}); !slices.Equal(got, want) {
		t.Errorf("segments = %v, want %v", got, want)
	}
}

func TestSourceAlignmentKeepsSoftBreaksAndEntitiesAtomic(t *testing.T) {
	if got, want := markdownAlignedSourceSegments("a \r\nb", "a b", 9, true), mdSegs([4]int{0, 1, 9, 10}, [4]int{1, 2, 11, 13}, [4]int{2, 3, 13, 14}); !slices.Equal(got, want) {
		t.Errorf("segments = %v, want %v", got, want)
	}
	entity := "&NotEqualTilde;"
	decoded := "≂̸"
	if got, want := markdownAlignedSourceSegments(entity, decoded, 3, true), mdSegs([4]int{0, len(decoded), 3, 3 + len(entity)}); !slices.Equal(got, want) {
		t.Errorf("segments = %v, want %v", got, want)
	}
}

func TestSourceAlignmentResumesAfterUnmappedCharacters(t *testing.T) {
	raw := strings.Repeat("abc", 4_096)
	var positions markdownCharPositions
	if got, ok := markdownSourceCharOffset(raw, 0, 'b', &positions); !ok || got != 1 {
		t.Errorf("offset of b = %d, %v; want 1", got, ok)
	}
	if positions.built {
		t.Error("successful scans need no index")
	}
	if _, ok := markdownSourceCharOffset(raw, 0, '�', &positions); ok {
		t.Error("missing character found")
	}
	if !positions.built {
		t.Error("failed scans must not be repeated")
	}
	if got, ok := markdownSourceCharOffset(raw, 2, 'b', &positions); !ok || got != 4 {
		t.Errorf("offset of b after 2 = %d, %v; want 4", got, ok)
	}
	if _, ok := markdownSourceCharOffset(raw, len(raw), 'a', &positions); ok {
		t.Error("found a past the end")
	}

	missing := strings.Repeat("�", 4_096)
	if got, want := markdownAlignedSourceSegments(raw, missing+"abc", 5, true), mdSegs([4]int{len(missing), len(missing) + 3, 5, 8}); !slices.Equal(got, want) {
		t.Errorf("segments = %v, want %v", got, want)
	}
	if got, want := markdownAlignedSourceSegments("&amp;z", missing+"&z", 0, true), mdSegs([4]int{len(missing), len(missing) + 1, 0, 5}, [4]int{len(missing) + 1, len(missing) + 2, 5, 6}); !slices.Equal(got, want) {
		t.Errorf("segments = %v, want %v", got, want)
	}
}

func TestSourceAlignmentParsesLongTextAndCodeWithoutSelection(t *testing.T) {
	source := strings.Repeat("a ", 4_096) + "end"
	cx := &nodeContext{}
	doc := mdParse(t, source, cx)
	p := mdFirstParagraph(doc.blocks[0])
	if p.text() != source {
		t.Fatalf("text differs from source")
	}
	segments := p.children[0].sourceSegments
	if len(segments) != 1 || cap(segments) >= 64 || segments[0].source != mdRange(0, len(source)) {
		t.Errorf("segments = %v (cap %d)", segments, cap(segments))
	}
	mdExpectSelected(t, source, len(source)-3, len(source), mdRange(len(source)-3, len(source)))

	body := strings.Repeat("x\n", 4_096)
	code := "```\n" + body + "```"
	doc = mdParse(t, code, cx)
	if mdFirstCodeBlock(doc.blocks[0]) == nil {
		t.Fatal("expected code block")
	}
	mdExpectCode(t, code, len(body)-2, len(body)-1, mdRange(len(body)+2, len(body)+3))
}

func TestSourceSegmentsKeepNonLinearMappingsAtomic(t *testing.T) {
	source := `a\* &amp; b`
	doc := mdParse(t, source, &nodeContext{})
	p := mdFirstParagraph(doc.blocks[0])
	segments := p.children[0].sourceSegments
	if len(segments) >= len([]rune(p.children[0].text)) {
		t.Errorf("ordinary characters should be compacted into runs: %v", segments)
	}
	for _, want := range mdSegs([4]int{1, 2, 1, 3}, [4]int{3, 4, 4, 9}) {
		if !slices.Contains(segments, want) {
			t.Errorf("segments %v lack %v", segments, want)
		}
	}
	mdExpectSelected(t, source, 1, 2, mdRange(1, 3))
	mdExpectSelected(t, source, 3, 4, mdRange(4, 9))
}

func TestSelectedSourceRangeCrossesStyleBoundaries(t *testing.T) {
	mdExpectSelected(t, "left **bold** right", 2, 12, mdRange(2, 16))
}

func TestSelectedSourceRangeMapsInlineCodeInMergedStyledNode(t *testing.T) {
	mdExpectSelected(t, "**left `code` right**", 5, 9, mdRange(8, 12))
}

func TestSelectedSourceRangeMapsInlineCodeDelimitersAndBoundaries(t *testing.T) {
	mdExpectSelected(t, "`code` x", 0, 4, mdRange(1, 5))
	mdExpectSelected(t, "`code` x", 5, 6, mdRange(7, 8))
	mdExpectSelected(t, "`code` x", 3, 6, mdRange(4, 8))

	padded := "`` code ` value ``"
	mdExpectSelected(t, padded, 0, 4, mdRange(3, 7))
	mdExpectSelected(t, padded, 5, 6, mdRange(8, 9))

	mdExpectSelected(t, "`&amp;`", 0, 5, mdRange(1, 6))
}

func TestSelectedSourceRangeMapsFootnoteReferenceSyntax(t *testing.T) {
	source := "before[^note] after\n\n[^note]: body"
	mdExpectSelected(t, source, 0, 6, mdRange(0, 6))
	mdExpectSelected(t, source, 6, 12, mdRange(6, 13))
	mdExpectSelected(t, source, 13, 18, mdRange(14, 19))
	mdExpectSelected(t, source, 4, 15, mdRange(4, 16))
}

// Deviated (MDX deferred by ticket 01): upstream maps an MDX text
// expression's body (U:text/format/markdown.rs:1419-1424); the port must
// refuse an MDX parse instead of silently parsing raw HTML.
func TestSelectedSourceRangeMapsMdxTextExpressionBody(t *testing.T) {
	ext := MarkdownExtensions{}.MDX()
	if _, err := markdownParse("before {value + 1} after", &nodeContext{markdownExtensions: &ext}); err != errMdastMDXUnsupported {
		t.Errorf("MDX parse error = %v, want errMdastMDXUnsupported", err)
	}
}

// Deviated (MDX deferred by ticket 01): U:text/format/markdown.rs:1426-1431.
func TestSelectedSourceRangeMapsMdxFlowExpressionBody(t *testing.T) {
	ext := MarkdownExtensions{}.MDX()
	if _, err := markdownParse("{\n  value + 1\n}", &nodeContext{markdownExtensions: &ext}); err != errMdastMDXUnsupported {
		t.Errorf("MDX parse error = %v, want errMdastMDXUnsupported", err)
	}
}

func TestSelectedSourceRangeMapsMathBlockBody(t *testing.T) {
	source := "$$\nx + y\n$$"
	mdExpectCode(t, source, 0, 5, mdRange(3, 8))
	mdExpectCode(t, source, 2, 3, mdRange(5, 6))
}

func TestSelectedSourceRangeRejectsMappedBlockPlusUnmappableEntity(t *testing.T) {
	doc := mdParse(t, "mapped\n\nA &amp; B", &nodeContext{})
	if len(doc.blocks) != 2 {
		t.Fatalf("expected two paragraphs, got %d blocks", len(doc.blocks))
	}
	mapped, ok1 := doc.blocks[0].(*paragraph)
	entity, ok2 := doc.blocks[1].(*paragraph)
	if !ok1 || !ok2 {
		t.Fatal("expected two paragraphs")
	}
	mapped.state.setSelection(mdSel(0, 6))

	entity.state.setSelection(mdSel(2, 2))
	got, ok := doc.selectedSourceRange()
	mdExpectRange(t, got, ok, mdRange(0, 6))

	entity.state.setSelection(mdSel(2, 3))
	got, ok = doc.selectedSourceRange()
	mdExpectRange(t, got, ok, mdRange(0, 15))
}

func mdOnlyCodeBlock(t *testing.T, doc *parsedDocument) *CodeBlock {
	t.Helper()
	code, ok := doc.blocks[0].(*CodeBlock)
	if !ok {
		t.Fatalf("expected code block, got %T", doc.blocks[0])
	}
	return code
}

func TestSelectedSourceRangeMapsFencedCodeBodyAfterMatchingInfoString(t *testing.T) {
	doc := mdParse(t, "```rust\nrust\n```", &nodeContext{})
	mdSetCodeSelection(mdOnlyCodeBlock(t, doc), 0, 4)
	got, ok := doc.selectedSourceRange()
	mdExpectRange(t, got, ok, mdRange(8, 12))
}

func TestSelectedSourceRangeExcludesClosingFenceCandidate(t *testing.T) {
	doc := mdParse(t, "````text\n```\n````", &nodeContext{})
	mdSetCodeSelection(mdOnlyCodeBlock(t, doc), 0, 3)
	got, ok := doc.selectedSourceRange()
	mdExpectRange(t, got, ok, mdRange(9, 12))
}

func TestSelectedSourceRangeMapsIndentedCodeContent(t *testing.T) {
	doc := mdParse(t, "    rust", &nodeContext{})
	mdSetCodeSelection(mdOnlyCodeBlock(t, doc), 0, 4)
	got, ok := doc.selectedSourceRange()
	mdExpectRange(t, got, ok, mdRange(4, 8))
}

func TestSelectedSourceRangeMapsMultilineIndentedCode(t *testing.T) {
	source := "    one\n    two\n    three"
	mdExpectCode(t, source, 0, 3, mdRange(4, 7))
	mdExpectCode(t, source, 4, 7, mdRange(12, 15))
	mdExpectCode(t, source, 0, 13, mdRange(4, 25))
}

func TestSelectedSourceRangeMapsFencedCodeNestedInAList(t *testing.T) {
	source := "- ```rust\n  one\n  two\n  ```"
	mdExpectCode(t, source, 0, 3, mdRange(12, 15))
	mdExpectCode(t, source, 4, 7, mdRange(18, 21))
	mdExpectCode(t, source, 0, 7, mdRange(12, 21))
}

func TestSelectedSourceRangeMapsFencedCodeNestedInABlockquote(t *testing.T) {
	source := "> ```\n> one\n> two\n> ```"
	mdExpectCode(t, source, 0, 3, mdRange(8, 11))
	mdExpectCode(t, source, 4, 7, mdRange(14, 17))
	mdExpectCode(t, source, 0, 7, mdRange(8, 17))
}

func TestSelectedSourceRangeMapsFencedCodeWithBlankLinesAndRepeatedText(t *testing.T) {
	source := "```\nsame\n\nsame\n```"
	mdExpectCode(t, source, 0, 4, mdRange(4, 8))
	mdExpectCode(t, source, 6, 10, mdRange(10, 14))
	mdExpectCode(t, source, 0, 10, mdRange(4, 14))
}

func TestSelectedSourceRangeMapsTheWholeMarkdownEscape(t *testing.T) {
	mdExpectSelected(t, `\*`, 0, 1, mdRange(0, 2))
}

func TestSelectedSourceRangeMapsAfterAnEscapedBackslash(t *testing.T) {
	source := `a\\b`
	mdExpectSelected(t, source, 1, 2, mdRange(1, 3))
	mdExpectSelected(t, source, 2, 3, mdRange(3, 4))
	mdExpectSelected(t, source, 1, 3, mdRange(1, 4))
	mdExpectSelected(t, `\\\\b`, 2, 3, mdRange(4, 5))
}

func TestSelectedSourceRangeDoesNotBorrowAnEscapeFromThePreviousNode(t *testing.T) {
	source := `a\\$x$`
	mdExpectSelected(t, source, 2, 3, mdRange(3, 4))
	mdExpectSelected(t, source, 1, 3, mdRange(1, 4))
}

func TestSelectedSourceRangeDoesNotShiftAHardBreakAfterAnEscape(t *testing.T) {
	source := "a\\\\  \nb"
	mdExpectSelected(t, source, 2, 3, mdRange(3, 6))
	mdExpectSelected(t, source, 1, 3, mdRange(1, 6))
}

func mdOnlyParagraph(t *testing.T, doc *parsedDocument) *paragraph {
	t.Helper()
	p, ok := doc.blocks[0].(*paragraph)
	if !ok {
		t.Fatalf("expected paragraph, got %T", doc.blocks[0])
	}
	return p
}

func mdFirstImage(t *testing.T, p *paragraph) *inlineNode {
	t.Helper()
	for i := range p.children {
		if p.children[i].image != nil {
			return &p.children[i]
		}
	}
	t.Fatal("expected image")
	return nil
}

func TestSelectedSourceRangeIncludesATrailingInlineImage(t *testing.T) {
	source := "before ![alt](image.png)"
	doc := mdParse(t, source, &nodeContext{})
	image := mdFirstImage(t, mdOnlyParagraph(t, doc))
	image.state.setText("before ")
	image.state.setSelection(mdSel(0, 7))
	got, ok := doc.selectedSourceRange()
	mdExpectRange(t, got, ok, mdRange(0, len(source)))
}

func TestSelectedSourceRangeIncludesALeadingInlineImage(t *testing.T) {
	source := "![alt](image.png) after"
	doc := mdParse(t, source, &nodeContext{})
	p := mdOnlyParagraph(t, doc)
	p.state.setText(" after")
	p.state.setSelection(mdSel(0, 6))
	got, ok := doc.selectedSourceRange()
	mdExpectRange(t, got, ok, mdRange(0, len(source)))
}

func TestSelectedSourceRangeIncludesAnEnclosedInlineImage(t *testing.T) {
	source := "before ![alt](image.png) after"
	doc := mdParse(t, source, &nodeContext{})
	p := mdOnlyParagraph(t, doc)
	image := mdFirstImage(t, p)
	image.state.setText("before ")
	image.state.setSelection(mdSel(0, 7))
	p.state.setText(" after")
	p.state.setSelection(mdSel(0, 6))
	got, ok := doc.selectedSourceRange()
	mdExpectRange(t, got, ok, mdRange(0, len(source)))
}

func TestSelectedSourceRangeExcludesAnUnreachedInlineImage(t *testing.T) {
	source := "before ![alt](image.png) after"
	doc := mdParse(t, source, &nodeContext{})
	p := mdOnlyParagraph(t, doc)
	image := mdFirstImage(t, p)
	image.state.setText("before ")
	image.state.setSelection(mdSel(0, 3))
	got, ok := doc.selectedSourceRange()
	mdExpectRange(t, got, ok, mdRange(0, 3))

	image.state.setSelection(nil)
	afterStart := strings.Index(source, "after")
	p.state.setText(" after")
	p.state.setSelection(mdSel(2, 6))
	got, ok = doc.selectedSourceRange()
	mdExpectRange(t, got, ok, mdRange(afterStart+1, len(source)))
}

func TestSelectedSourceRangeIncludesConsecutiveInlineImages(t *testing.T) {
	source := "![first](one.png)![second](two.png) after"
	doc := mdParse(t, source, &nodeContext{})
	p := mdOnlyParagraph(t, doc)
	p.state.setText(" after")
	p.state.setSelection(mdSel(0, 6))
	got, ok := doc.selectedSourceRange()
	mdExpectRange(t, got, ok, mdRange(0, len(source)))
}

func TestSelectedSourceRangeMapsDecodedEntityToItsSourceSyntax(t *testing.T) {
	source := "A &amp; B"
	doc := mdParse(t, source, &nodeContext{})
	if got := mdOnlyParagraph(t, doc).text(); got != "A & B" {
		t.Errorf("text = %q", got)
	}
	mdExpectSelected(t, source, 2, 3, mdRange(2, 7))
}

func TestSelectedSourceRangeMapsAroundNamedAndNumericEntities(t *testing.T) {
	source := "Copyright &copy; &#x1F600; &#169; 2024"
	mdExpectSelected(t, source, 0, 9, mdRange(0, 9))
	mdExpectSelected(t, source, 10, 12, mdRange(10, 16))
	mdExpectSelected(t, source, 13, 17, mdRange(17, 26))
	mdExpectSelected(t, source, 18, 20, mdRange(27, 33))
	mdExpectSelected(t, source, 21, 25, mdRange(34, 38))
	mdExpectSelected(t, source, 8, 22, mdRange(8, 35))
}

func TestSelectedSourceRangeMapsSoftBreaksWithSourcePrefixes(t *testing.T) {
	mdExpectSelected(t, "a\n   b", 0, 1, mdRange(0, 1))
	mdExpectSelected(t, "a\n   b", 2, 3, mdRange(5, 6))
	mdExpectSelected(t, "a\n   b", 0, 3, mdRange(0, 6))

	mdExpectSelected(t, "> a\n> b", 0, 1, mdRange(2, 3))
	mdExpectSelected(t, "> a\n> b", 2, 3, mdRange(6, 7))
	mdExpectSelected(t, "> a\n> b", 0, 3, mdRange(2, 7))

	mdExpectSelected(t, "- a\n  b", 0, 1, mdRange(2, 3))
	mdExpectSelected(t, "- a\n  b", 2, 3, mdRange(6, 7))
	mdExpectSelected(t, "- a\n  b", 0, 3, mdRange(2, 7))
}

func TestSelectedSourceRangeMapsSoftBreaksWithTrailingSpacesAndCrlf(t *testing.T) {
	mdExpectSelected(t, "a \nb", 0, 1, mdRange(0, 1))
	mdExpectSelected(t, "a \nb", 1, 2, mdRange(2, 3))
	mdExpectSelected(t, "a \nb", 2, 3, mdRange(3, 4))
	mdExpectSelected(t, "a \nb", 0, 3, mdRange(0, 4))

	mdExpectSelected(t, "a \r\nb", 1, 2, mdRange(2, 4))
	mdExpectSelected(t, "a \r\nb", 2, 3, mdRange(4, 5))

	mdExpectSelected(t, "a\r\nb", 0, 1, mdRange(0, 1))
	mdExpectSelected(t, "a\r\nb", 2, 3, mdRange(3, 4))
	mdExpectSelected(t, "a\r\nb", 0, 3, mdRange(0, 4))
}

func TestNestedEmphasisMergesTextMarks(t *testing.T) {
	doc := mdParse(t, "This has **_bold and italic_** text.", &nodeContext{})
	p := mdOnlyParagraph(t, doc)
	for _, child := range p.children {
		if child.text != "bold and italic" {
			continue
		}
		for _, m := range child.marks {
			if m.mark.Bold && m.mark.Italic {
				return
			}
		}
		t.Fatal("nested emphasis should produce a bold and italic mark")
	}
	t.Fatal("expected emphasized text")
}

// The inline raw HTML tests below go through U:text/format/html.rs
// (text_html.go, ticket 30).

func TestInlineHTMLImageStaysInMarkdownParagraph(t *testing.T) {
	doc := mdParse(t, `Before <img src="https://example.com/avatar.png" alt="Avatar" width="32" height="32" /> after.`, &nodeContext{})
	p := mdOnlyParagraph(t, doc)
	if len(p.children) != 3 || p.children[0].text != "Before " || p.children[2].text != " after." {
		t.Fatalf("children = %+v", p.children)
	}
	image := p.children[1].image
	if image == nil || image.URL != "https://example.com/avatar.png" {
		t.Fatalf("image = %+v", image)
	}
	want := gpui.DefinitePx(32)
	if image.Width == nil || *image.Width != want || image.Height == nil || *image.Height != want {
		t.Errorf("size = %v x %v, want 32px", image.Width, image.Height)
	}
}

func TestInlineHTMLImageWithoutSizeStaysInMarkdownParagraph(t *testing.T) {
	doc := mdParse(t, `Before <img src="https://avatars.githubusercontent.com/u/5518"> after.`, &nodeContext{})
	p := mdOnlyParagraph(t, doc)
	if len(p.children) != 3 || p.children[0].text != "Before " || p.children[2].text != " after." {
		t.Fatalf("children = %+v", p.children)
	}
	image := p.children[1].image
	if image == nil || image.URL != "https://avatars.githubusercontent.com/u/5518" {
		t.Fatalf("image = %+v", image)
	}
	if image.Width != nil || image.Height != nil {
		t.Errorf("size = %v x %v, want none", image.Width, image.Height)
	}
}

func TestSoftBreakReflowsToSpace(t *testing.T) {
	for _, source := range []string{
		"this sentence\ncontinues as a soft wrap",
		"this sentence\r\ncontinues as a soft wrap",
		"this sentence\rcontinues as a soft wrap",
	} {
		p := mdOnlyParagraph(t, mdParse(t, source, &nodeContext{}))
		if len(p.children) != 1 || p.children[0].text != "this sentence continues as a soft wrap" {
			t.Errorf("source %q: children = %+v", source, p.children)
		}
	}
}

func TestSoftBreakReflowsWhileHardBreakSurvives(t *testing.T) {
	p := mdOnlyParagraph(t, mdParse(t, "a\nb  \nc", &nodeContext{}))
	var texts []string
	for _, c := range p.children {
		texts = append(texts, c.text)
	}
	if !slices.Equal(texts, []string{"a b", "\n", "c"}) {
		t.Errorf("texts = %q", texts)
	}
}

func TestHardBreakRendersNewline(t *testing.T) {
	for _, source := range []string{
		"Owner: Jane  \nPersona: assistant",
		"Owner: Jane\\\nPersona: assistant",
	} {
		p := mdOnlyParagraph(t, mdParse(t, source, &nodeContext{}))
		var texts []string
		for _, c := range p.children {
			texts = append(texts, c.text)
		}
		if !slices.Equal(texts, []string{"Owner: Jane", "\n", "Persona: assistant"}) {
			t.Errorf("source %q: texts = %q", source, texts)
		}
	}
}

func TestYamlFrontmatterIsDisabledByDefault(t *testing.T) {
	doc := mdParse(t, "---\nSome text\n---", &nodeContext{})
	if _, ok := doc.blocks[0].(*blockHorizontalRule); !ok {
		t.Errorf("blocks[0] = %T, want horizontal rule", doc.blocks[0])
	}
	h, ok := doc.blocks[1].(*blockHeading)
	if !ok {
		t.Fatalf("blocks[1] = %T, want setext heading", doc.blocks[1])
	}
	if h.level != 2 || h.children.text() != "Some text" {
		t.Errorf("heading = level %d %q", h.level, h.children.text())
	}
}

func TestNonMappingYamlFrontmatterFallsBackToCodeBlock(t *testing.T) {
	ext := MarkdownExtensions{}.Frontmatter()
	doc := mdParse(t, "---\n- name: example\n---", &nodeContext{markdownExtensions: &ext})
	code := mdOnlyCodeBlock(t, doc)
	if lang, ok := code.Lang(); !ok || lang != "yml" {
		t.Errorf("lang = %q, %v", lang, ok)
	}
	if code.Code() != "- name: example" {
		t.Errorf("code = %q", code.Code())
	}
}

func TestFencedYamlRemainsACodeBlock(t *testing.T) {
	code := mdOnlyCodeBlock(t, mdParse(t, "```yaml\nname: example\n```", &nodeContext{}))
	if lang, ok := code.Lang(); !ok || lang != "yaml" {
		t.Errorf("lang = %q, %v", lang, ok)
	}
	if code.Code() != "name: example" {
		t.Errorf("code = %q", code.Code())
	}
}

type mdTicker struct{ symbol string }

func mdParseTickerBlock(node *MdastNode, cx *MarkdownParseContext) *MarkdownNode {
	if node.Kind != MdastParagraph || len(node.Children) != 1 || node.Children[0].Kind != MdastText {
		return nil
	}
	symbol, ok := strings.CutPrefix(node.Children[0].Value, "$")
	if !ok {
		return nil
	}
	source, _ := cx.NodeSource(node)
	n := NewMarkdownNode("ticker", mdTicker{symbol: symbol}).Text("$" + symbol).Markdown(source)
	return &n
}

func TestCustomBlockParserConvertsTickerSyntaxToCustomNode(t *testing.T) {
	ext := MarkdownExtensions{}.BlockParser(mdParseTickerBlock)
	doc := mdParse(t, "$TSLA.US", &nodeContext{markdownExtensions: &ext})
	custom, ok := doc.blocks[0].(*blockCustom)
	if !ok {
		t.Fatalf("blocks[0] = %T, want custom node", doc.blocks[0])
	}
	node := custom.node
	if node.Name() != "ticker" || node.AsText() != "$TSLA.US" || node.AsMarkdown() != "$TSLA.US" {
		t.Errorf("node = %+v", node)
	}
	if data, ok := MarkdownNodeData[mdTicker](node); !ok || data.symbol != "TSLA.US" {
		t.Errorf("data = %+v, %v", data, ok)
	}
	if doc.text() != "$TSLA.US\n" {
		t.Errorf("text = %q", doc.text())
	}
	if doc.toMarkdown() != "$TSLA.US" {
		t.Errorf("markdown = %q", doc.toMarkdown())
	}
}

func mdCustomCount(p *paragraph) int {
	n := 0
	for _, c := range p.children {
		if c.custom != nil {
			n++
		}
	}
	return n
}

func TestInlineMathIsEnabledByDefaultAndRespectsCodeAndEscapes(t *testing.T) {
	ext := mdInlineExtensions("formula", func(node *MdastNode, _ *MarkdownParseContext) *MarkdownNode {
		if node.Kind != MdastInlineMath {
			return nil
		}
		if node.Value != "x" {
			t.Errorf("math value = %q, want x", node.Value)
		}
		n := NewMarkdownNode("formula", nil).Text("formula")
		return &n
	})
	doc := mdParse(t, `$x$ `+"`$code$`"+` \$escaped\$`, &nodeContext{markdownExtensions: ext})
	if doc.text() != "formula $code$ $escaped$\n" {
		t.Errorf("text = %q", doc.text())
	}
	if n := mdCustomCount(mdOnlyParagraph(t, doc)); n != 1 {
		t.Errorf("custom nodes = %d, want 1", n)
	}
}

func TestInlineExtensionsPreserveNodesInsideMarksAndGlobalSourceRanges(t *testing.T) {
	source := "中文 **before $x^2$ after** and `$ignored$`"
	ext := mdInlineExtensions("test", func(node *MdastNode, cx *MarkdownParseContext) *MarkdownNode {
		if node.Kind != MdastInlineMath {
			return nil
		}
		if s, ok := cx.NodeSource(node); !ok || s != "$x^2$" {
			t.Errorf("node source = %q, %v", s, ok)
		}
		n := NewMarkdownNode("formula", node.Value).Text("x²")
		return &n
	})
	doc := mdParse(t, source, &nodeContext{offset: 50, markdownExtensions: ext})
	p := mdOnlyParagraph(t, doc)
	var objects []*MarkdownNode
	for _, c := range p.children {
		if c.custom != nil {
			objects = append(objects, c.custom)
		}
	}
	if len(objects) != 1 {
		t.Fatalf("objects = %d, want 1", len(objects))
	}
	object := objects[0]
	start := strings.Index(source, "$x^2$") + 50
	if r, ok := object.SourceRange(); !ok || r != mdRange(start, start+5) {
		t.Errorf("source range = %v, %v", r, ok)
	}
	if object.AsText() != "x²" || object.AsMarkdown() != "$x^2$" || object.AccessibilityName() != "x²" {
		t.Errorf("object = %+v", object)
	}
	if doc.text() != "中文 before x² after and $ignored$\n" {
		t.Errorf("text = %q", doc.text())
	}
	if !strings.Contains(doc.toMarkdown(), "**before $x^2$ after**") {
		t.Errorf("markdown = %q", doc.toMarkdown())
	}
}

func TestBlockMathParsesAsABlockAndFallsBackToACodeBlock(t *testing.T) {
	doc := mdParse(t, "$$\n\\sum_{i=1}^{n} i\n$$", &nodeContext{})
	code := mdOnlyCodeBlock(t, doc)
	if code.Code() != "\\sum_{i=1}^{n} i" {
		t.Errorf("code = %q", code.Code())
	}
	if !strings.Contains(doc.toMarkdown(), "\\sum_{i=1}^{n} i") {
		t.Errorf("markdown = %q", doc.toMarkdown())
	}
}

func mdMarkedRuns(p *paragraph, pred func(TextMark) bool) []string {
	var out []string
	for _, c := range p.children {
		for _, m := range c.marks {
			if pred(m.mark) {
				out = append(out, c.text[m.rng.Start:m.rng.End])
			}
		}
	}
	return out
}

func TestUnclaimedInlineMathParsesItsSpanAsProse(t *testing.T) {
	source := "EPS of <strong>$1.56</strong> beat the <strong>$1.50</strong> consensus, *up $2*"
	doc := mdParse(t, source, &nodeContext{})
	if doc.text() != "EPS of $1.56 beat the $1.50 consensus, up $2\n" {
		t.Errorf("text = %q", doc.text())
	}
	p := mdOnlyParagraph(t, doc)
	if got := mdMarkedRuns(p, func(m TextMark) bool { return m.Bold }); !slices.Equal(got, []string{"$1.56", "$1.50"}) {
		t.Errorf("bold runs = %q", got)
	}
	if got := mdMarkedRuns(p, func(m TextMark) bool { return m.Italic }); !slices.Contains(got, "up $2") {
		t.Errorf("italic runs = %q", got)
	}
	if !strings.Contains(doc.toMarkdown(), "$1.56") {
		t.Errorf("markdown = %q", doc.toMarkdown())
	}
}

func TestClaimedInlineMathSurvivesProseFlattening(t *testing.T) {
	ext := mdInlineExtensions("formula", func(node *MdastNode, _ *MarkdownParseContext) *MarkdownNode {
		if node.Kind != MdastInlineMath {
			return nil
		}
		n := NewMarkdownNode("formula", nil).Text("[" + node.Value + "]")
		return &n
	})
	doc := mdParse(t, "area $x^2$ costs $5 and $10", &nodeContext{markdownExtensions: ext})
	if doc.text() != "area [x^2] costs [5 and ]10\n" {
		t.Errorf("text = %q", doc.text())
	}
}

func TestInlineHTMLFormattingTagsPairAcrossSiblings(t *testing.T) {
	source := "a <strong>b *c* <em>d</em></strong> e <b>f</b> <i>g</i> <del>h</del> <br> <strong>unclosed"
	doc := mdParse(t, source, &nodeContext{})
	if doc.text() != "a b c d e f g h \n unclosed\n" {
		t.Errorf("text = %q", doc.text())
	}
	p := mdOnlyParagraph(t, doc)
	if got := mdMarkedRuns(p, func(m TextMark) bool { return m.Bold }); !slices.Equal(got, []string{"b c d", "f"}) {
		t.Errorf("bold runs = %q", got)
	}
	if got := mdMarkedRuns(p, func(m TextMark) bool { return m.Italic }); !slices.Equal(got, []string{"c", "d", "g"}) {
		t.Errorf("italic runs = %q", got)
	}
	if got := mdMarkedRuns(p, func(m TextMark) bool { return m.Strikethrough }); !slices.Equal(got, []string{"h"}) {
		t.Errorf("strikethrough runs = %q", got)
	}
}

func TestUnclaimedInlineMathKeepsItsLiteralSource(t *testing.T) {
	source := "spent $5 and $10 today"
	doc := mdParse(t, source, &nodeContext{})
	if doc.text() != "spent $5 and $10 today\n" {
		t.Errorf("text = %q", doc.text())
	}
	if !strings.Contains(doc.toMarkdown(), "spent $5 and $10 today") {
		t.Errorf("markdown = %q", doc.toMarkdown())
	}
	if n := mdCustomCount(mdOnlyParagraph(t, doc)); n != 0 {
		t.Errorf("custom nodes = %d, want 0", n)
	}
}

func TestInlineObjectMarkdownRoundtripPreservesNestedEmphasis(t *testing.T) {
	for _, source := range []string{
		"*before $x$ after*",
		"*before **$x$** after*",
		"**before *$x$* after**",
	} {
		ext := mdInlineExtensions("test", func(node *MdastNode, _ *MarkdownParseContext) *MarkdownNode {
			if node.Kind != MdastInlineMath {
				return nil
			}
			n := NewMarkdownNode("math", nil).Text(node.Value)
			return &n
		})
		cx := &nodeContext{markdownExtensions: ext}
		original := mdParse(t, source, cx)
		markdown := original.toMarkdown()
		reparsed := mdParse(t, markdown, cx)
		before, after := mdOnlyParagraph(t, original), mdOnlyParagraph(t, reparsed)
		object := func(p *paragraph) *inlineNode {
			for i := range p.children {
				if p.children[i].custom != nil {
					return &p.children[i]
				}
			}
			t.Fatalf("%s -> %s: no inline object", source, markdown)
			return nil
		}
		if before.text() != after.text() {
			t.Errorf("%s -> %s: text %q != %q", source, markdown, before.text(), after.text())
		}
		bm, am := object(before).marks, object(after).marks
		if !slices.EqualFunc(bm, am, func(x, y textMarkRange) bool { return x.rng == y.rng && x.mark.equal(y.mark) }) {
			t.Errorf("%s -> %s: marks %+v != %+v", source, markdown, bm, am)
		}
	}
}

func TestInlineParserWithoutMetadataFallsBackToOriginalSource(t *testing.T) {
	ext := mdInlineExtensions("test", func(node *MdastNode, _ *MarkdownParseContext) *MarkdownNode {
		if node.Kind != MdastInlineCode {
			return nil
		}
		n := NewMarkdownNode("opaque", nil)
		return &n
	})
	doc := mdParse(t, "`原子`", &nodeContext{markdownExtensions: ext})
	node := mdOnlyParagraph(t, doc).children[0].custom
	if node == nil {
		t.Fatal("expected custom node")
	}
	if node.AsText() != "`原子`" || node.AsMarkdown() != "`原子`" || node.AccessibilityName() != "`原子`" {
		t.Errorf("node = %+v", node)
	}
}

// mdTickerPlugin is the block MarkdownPlugin of
// U:text/format/markdown.rs:1974-1976,2234-2261.
type mdTickerPlugin struct{ name string }

func (p mdTickerPlugin) IsBlock() bool { return true }
func (p mdTickerPlugin) Name() string  { return p.name }
func (p mdTickerPlugin) Parse(node *MdastNode, cx *MarkdownParseContext) *MarkdownNode {
	return mdParseTickerBlock(node, cx)
}
func (p mdTickerPlugin) Render(node *MarkdownNode, _ *gpui.Window, _ *gpui.App) gpui.AnyElement {
	return gpui.Text(node.AsText())
}

// Go-side: inline plugins register under their name and the parser
// configuration compares those names (U:text/markdown_ext.rs:331-336,
// 351-367).
func TestInlinePluginNamesTakePartInParserConfiguration(t *testing.T) {
	a := MarkdownExtensions{}.Plugin(newTestInlinePlugin("math"))
	same := MarkdownExtensions{}.Plugin(newTestInlinePlugin("math"))
	other := MarkdownExtensions{}.Plugin(newTestInlinePlugin("emoji"))
	if _, ok := a.inlineRenderers["math"]; !ok {
		t.Error("inline plugin not registered under its name")
	}
	if !a.hasSameParserConfiguration(&same) {
		t.Error("equivalent registrations differ")
	}
	if a.hasSameParserConfiguration(&other) {
		t.Error("differently named inline plugins compare the same")
	}
	if len((MarkdownExtensions{}).inlineRenderers) != 0 || len(a.Plugin(newTestInlinePlugin("x")).inlineRenderers) != 2 || len(a.inlineRenderers) != 1 {
		t.Error("Plugin mutated its receiver's registry")
	}
}

func TestCustomBlockPluginRegistersParserAndRenderer(t *testing.T) {
	ext := MarkdownExtensions{}.Plugin(mdTickerPlugin{name: "ticker"})
	if _, ok := ext.blockRenderers["ticker"]; !ok {
		t.Error("plugin renderer not registered")
	}
	doc := mdParse(t, "$TSLA.US", &nodeContext{markdownExtensions: &ext})
	custom, ok := doc.blocks[0].(*blockCustom)
	if !ok {
		t.Fatalf("blocks[0] = %T, want custom node", doc.blocks[0])
	}
	if custom.node.Name() != "ticker" {
		t.Errorf("name = %q", custom.node.Name())
	}
	if data, ok := MarkdownNodeData[mdTicker](custom.node); !ok || data.symbol != "TSLA.US" {
		t.Errorf("data = %+v, %v", data, ok)
	}
}
