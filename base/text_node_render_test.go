package base

import (
	"math"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// The 7 U:text/node.rs tests ticket 27 deferred (text_node_test.go's
// header): 5 ported, 2 deviated:
//   - table_column_of_inline_code_cells_fits_the_mono_width runs under the
//     deterministic test text system, where the mono family shapes as wide
//     as the body (see text_inline_flow_test.go); it pins the code-size
//     measurement and padding.
//   - cloned_table_keeps_cached_table_data: Go shares a parsed table by
//     pointer instead of deep-cloning it, so the reuse it asserts is the
//     same table's second read.

// textRenderPlugin is U:text/markdown_ext.rs:462-524 TestInlinePlugin with
// its render_with renderer (the parse half is text_markdown_test.go's
// testInlinePlugin).
type textRenderPlugin struct {
	testInlinePlugin
	render func(node *MarkdownNode, context *InlineRenderContext, w *gpui.Window, app *gpui.App) (InlineElement, bool)
}

func (p textRenderPlugin) RenderInline(node *MarkdownNode, context *InlineRenderContext, w *gpui.Window, app *gpui.App) (InlineElement, bool) {
	if p.render == nil {
		return InlineElement{}, false
	}
	return p.render(node, context, w, app)
}

func tableOfCells(rows [][]textTableCell) *textTable {
	t := &textTable{}
	for _, r := range rows {
		t.children = append(t.children, textTableRow{children: r})
	}
	return t
}

// U:text/node.rs:3518-3556
func TestCustomInlineInheritsMarksAndResolvesLinkReferences(t *testing.T) {
	ta := textTestApp(t)
	ta.Update(func(app *gpui.App) {
		cx := defaultNodeRenderContext()
		ref := "ref"
		mark := TextMark{}.WithBold()
		mark.Italic, mark.Strikethrough = true, true
		mark.Link = &LinkMark{Identifier: &ref}
		cx.linkRefs = map[string]LinkMark{"ref": {URL: "https://example.com"}}
		p := ntParagraph(inlineNodeCustom(NewMarkdownNode("test", nil).Text("x")).withMarks(ntMarks(0, 1, mark)))
		items := p.inlineFlowItems(nil, nil, nil, cx, app)
		if items[0].kind != inlineFlowObject {
			t.Fatalf("item %v", items[0].kind)
		}
		s := items[0].style
		if s.FontWeight == nil || *s.FontWeight != textFontWeightBold || s.FontStyle == nil || *s.FontStyle != gpui.FontStyleItalic || s.Strikethrough == nil {
			t.Fatalf("style %+v", s)
		}
		if items[0].link == nil || items[0].link.URL != "https://example.com" {
			t.Fatalf("link %+v", items[0].link)
		}
	})
}

// U:text/node.rs:3558-3605
func TestTableColumnUsesPreparedInlineMetricsAfterResourceUpdate(t *testing.T) {
	var width atomic.Int64
	width.Store(400)
	cx := defaultNodeRenderContext()
	e := MarkdownExtensions{}.Plugin(textRenderPlugin{
		testInlinePlugin: newTestInlinePlugin("test"),
		render: func(*MarkdownNode, *InlineRenderContext, *gpui.Window, *gpui.App) (InlineElement, bool) {
			img := gpui.Img(gpui.NewImage(gpui.ImageFormatSVG, []byte("<svg/>"))).Flex().W(gpui.Px(float32(width.Load()))).H(gpui.Px(20))
			return NewInlineElement(img).WithBaseline(15), true
		},
	})
	cx.markdownExtensions = &e
	table := tableOfCells([][]textTableCell{{{children: ntParagraph(inlineNodeCustom(NewMarkdownNode("test", nil).Text("x")))}}})
	inPrepaint(t, func(w *gpui.Window, app *gpui.App) struct{} {
		for _, expected := range []int64{400, 600} {
			width.Store(expected)
			if got := measureTableColumns(table, 1, cx, w, app)[0]; got != float32(expected)+cellPadPx {
				t.Errorf("column %v, want %v", got, float32(expected)+cellPadPx)
			}
		}
		return struct{}{}
	})
}

// U:text/node.rs:3607-3656 (deviated: test text system).
func TestTableColumnOfInlineCodeCellsFitsTheMonoWidth(t *testing.T) {
	const code = "method_name()"
	p := defaultParagraph()
	p.push(newInlineNode(code).withMarks(ntMarks(0, len(code), TextMark{}.WithCode())))
	table := tableOfCells([][]textTableCell{{{children: p}}})
	inPrepaint(t, func(w *gpui.Window, app *gpui.App) struct{} {
		theme := ThemeGlobalMut(app)
		theme.Tokens.Typography.Mono = "Mono"
		fontSize := gpui.WindowTextStyle(w).FontSizePixels(gpui.RemSize(w))
		colW := measureTableColumns(table, 1, defaultNodeRenderContext(), w, app)
		want := testWidthOf(code, fontSize*0.875) + cellPadPx + inlineCodePadding*2
		if math.Abs(float64(colW[0]-want)) >= 0.01 {
			t.Fatalf("col_w %v, want %v", colW[0], want)
		}
		return struct{}{}
	})
}

// U:text/node.rs:3658-3686
func TestCodeBlockHighlightsAreCachedByHighlighterIdentity(t *testing.T) {
	calls := 0
	highlighter := NewCodeBlockHighlighter(func(*CodeBlock) []gpui.HighlightRange { calls++; return nil })
	lang := "rust"
	block := newCodeBlock("fn main() {}", &lang, nil)
	block.highlightedStyles(highlighter)
	block.highlightedStyles(highlighter)
	if calls != 1 {
		t.Fatalf("%d calls", calls)
	}
	replacement := NewCodeBlockHighlighter(func(*CodeBlock) []gpui.HighlightRange { return nil })
	block.highlightedStyles(replacement)
	if cached, _ := codeBlockHighlightCache.get(block); cached.highlighter != replacement {
		t.Fatal("the cache kept the old highlighter")
	}
}

// U:text/node.rs:3688-3725
func TestANewHighlighterReplacesStylesInsteadOfReusingTheCache(t *testing.T) {
	colored := func(hex uint32) *CodeBlockHighlighter {
		return NewCodeBlockHighlighter(func(*CodeBlock) []gpui.HighlightRange {
			return []gpui.HighlightRange{{Range: mdRange(0, 2), Style: gpui.HighlightStyleFromColor(gpui.Rgb(hex))}}
		})
	}
	lang := "json"
	block := CodeBlockFromCode("42", &lang)
	light := block.highlightedStyles(colored(0x0000ff))
	dark := block.highlightedStyles(colored(0xffff00))
	if *light[0].Style.Color != gpui.Rgb(0x0000ff) || *dark[0].Style.Color != gpui.Rgb(0xffff00) || block.Code() != "42" {
		t.Fatal("styles reused across highlighters")
	}
}

// U:text/node.rs:4187-4199 (deviated: same-pointer reuse).
func TestClonedTableKeepsCachedTableData(t *testing.T) {
	table := tableOfCells([][]textTableCell{{{children: newParagraph("Name")}}, {{children: newParagraph("Alice")}}})
	table.columnAligns = []columnumnAlign{columnAlignLeft}
	if data := table.cachedTableData(); data != table.cachedTableData() || data.Headers[0] != "Name" {
		t.Fatal("the cached table data was rebuilt")
	}
}

// U:text/node.rs:4217-4260
func TestImageNodeSource(t *testing.T) {
	for _, url := range []string{
		"https://example.com/logo.png", "http://example.com/logo.png", "website/public/logo.svg",
		"./images/a.png", "../images/a.png", "/absolute/path/logo.svg", "file:///absolute/path/logo.svg",
		`C:\images\logo.png`, "docs/a:b.png", "data:text/plain;base64,aGVsbG8=",
	} {
		s := imageNodeSource(&ImageNode{URL: url})
		if s.Kind != gpui.ImageSourceResource || s.Resource.Kind != gpui.ResourceURI || s.Resource.Value != url {
			t.Errorf("%q: %+v", url, s)
		}
	}
	node := &ImageNode{URL: "data:image/png;base64,iVBORw0KGgo="}
	first, second := imageNodeSource(node), imageNodeSource(node)
	if first.Kind != gpui.ImageSourceImage || first.Image.Format != gpui.ImageFormatPNG || string(first.Image.Bytes) != "\x89PNG\r\n\x1a\n" {
		t.Fatalf("first %+v", first)
	}
	if second.Kind != gpui.ImageSourceImage || &second.Image.Bytes[0] != &first.Image.Bytes[0] || !strings.HasPrefix(string(second.Image.Bytes), "\x89PNG") {
		t.Fatal("the data URL was decoded twice")
	}
}
