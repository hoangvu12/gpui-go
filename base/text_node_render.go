package base

// The rendering half of U:text/node.rs (ticket 28): NodeContext's render
// fields, the Paragraph / CodeBlock / Table / list / block render impls,
// their caches, and Document::render_root (U:text/document.rs:189-245).
//
// Go adaptations (recorded deviations):
//   - Render caches (Paragraph's ParagraphRenderCache, CodeBlock's
//     highlight cache, Table's TableDataCache, ImageNode's decoded
//     data-URL image) live in side tables keyed by a weak pointer to the
//     node, because text_node.go's structs are ticket 27's data model.
//     A paragraph's entry also records the children it was built from and
//     is dropped when they change, as Rust's invalidate_render_cache does.
//   - Block display (gap 2b): upstream's plain div() is Block under the
//     0.2.2 core Base targets, which has no inline paragraphs. Under the
//     pin a Block div gathers text children into an inline paragraph
//     (R:elements/div/inline.rs) and would take over the painting of
//     Base's own Inline elements. Boxes that hold rendered text are
//     therefore flex columns (textBlockDiv), which stack and stretch their
//     children as Block layout does.
//   - Stream fades, range highlights and reveal (ticket 30) enter through
//     the nodeRenderContext hooks; ticket 30 owns TextLeafKey's semantics,
//     textLeafKey carries its identity.
//   - Table scroll mode is opted in by an overflow-x scroll on the table
//     refinement, as upstream.

import (
	"strconv"
	"strings"
	"sync"
	"weak"

	"github.com/hoangvu12/gpui-go/gpui"
)

// textCheckSVGLight and textCheckSVGDark are the task-list checks
// (U:text/node.rs:40-41).
var (
	textCheckSVGLight = []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="none"><path d="m3.25 8.25 3 3 6.5-7" stroke="white" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"/></svg>`)
	textCheckSVGDark  = []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="none"><path d="m3.25 8.25 3 3 6.5-7" stroke="black" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"/></svg>`)
)

// textLeafKey names a text leaf for the streamed fade-in and range
// highlights (U:text/stream_fade.rs:103-135, ticket 30's module).
type textLeafKey struct {
	blockStart int
	ordinal    int
}

func textLeafBlock(start int) *textLeafKey { return &textLeafKey{blockStart: start} }

func textLeafTableCell(tableStart, ordinal int) *textLeafKey {
	return &textLeafKey{blockStart: tableStart, ordinal: ordinal + 1}
}

func spanLeafKey(span *Span) *textLeafKey {
	if span == nil {
		return nil
	}
	return textLeafBlock(span.Start)
}

// textBlockDiv is the Block box of a text block (see the file comment).
// The white-space mode pins the TextView's verbatim rendering (browser-
// core 38, gpui/white_space.go): the pin's TextView lays text out
// byte-for-byte with soft wrap — exactly CSS pre-wrap, the mode the
// browser's UA sheet gives input-like verbatim content — where the
// core's default normal would collapse runs of spaces and newlines in
// Base's code blocks and paragraphs. Recorded divergence: tabs in
// pre-wrapped text follow the browser's 8-space stops (Chromium
// tab_size.h's TabSize(8)), where the pin's platform line used
// DirectWrite's 4em stops.
func textBlockDiv() *gpui.DivElement { return gpui.Div().Flex().FlexCol().WhitespacePreWrap() }

// nodeRenderContext is the render half of NodeContext
// (U:text/node.rs:1984-2048).
type nodeRenderContext struct {
	offset               int
	linkRefs             map[string]LinkMark
	style                *TextViewStyle
	codeBlockActions     CodeBlockActionsFn
	codeBlockHighlighter *CodeBlockHighlighter
	tableActions         TableActionsFn
	imageSource          ImageSourceFn
	linkClickHandler     LinkClickHandlerFn
	markdownExtensions   *MarkdownExtensions
	// streamFades and rangeBackgrounds are ticket 30's frames; reveal is
	// its pending reveal (text_range_highlight_state.go).
	streamFades      func(key textLeafKey) []textFadeRange
	rangeBackgrounds func(key textLeafKey) []textColorRange
	reveal           *revealRequest

	// anonBlock and anonLeaf number the text leaves without a key (see
	// leafElementID): the top-level block being rendered and the next
	// leaf in it.
	anonBlock int
	anonLeaf  int
}

// leafElementID is leaf_element_id (U:text/node.rs:2505-2507).
//
// Deviation: upstream names every leaf without a key "p". HTML documents
// carry no spans (U:text/format/html.rs sets span: None throughout), so
// every flow paragraph of an HTML document shares that id under the same
// parent and therefore the flow's element state (U:text/inline_flow.rs:
// 80-98: one layout key, its layouts, the fragment states). The last flow
// requested sets the key, the others measure and paint its layout, and the
// shared fragment states keep only its text: a paragraph with inline code
// followed by a <pre> drew the <pre>'s code background and no text. A leaf
// without a key is numbered here instead, in render order within its
// top-level block, which is stable while the document is unchanged.
func (cx *nodeRenderContext) leafElementID(key *textLeafKey) gpui.ElementID {
	if key != nil {
		return textLeafElementID(key)
	}
	n := cx.anonLeaf
	cx.anonLeaf++
	return gpui.NamedIntegerElementID("p-"+strconv.Itoa(cx.anonBlock), uint64(n))
}

// startBlock restarts the keyless leaf numbering for top-level block ix.
func (cx *nodeRenderContext) startBlock(ix int) {
	cx.anonBlock, cx.anonLeaf = ix, 0
}

func defaultNodeRenderContext() *nodeRenderContext {
	style := DefaultTextViewStyle()
	return &nodeRenderContext{style: &style, markdownExtensions: &MarkdownExtensions{}}
}

// imageSourceOf is NodeContext::image_source (U:text/node.rs:2007-2012).
func (cx *nodeRenderContext) imageSourceOf(image *ImageNode) gpui.ImageSource {
	if cx.imageSource != nil {
		return cx.imageSource(image.URL)
	}
	return imageNodeSource(image)
}

func (cx *nodeRenderContext) fades(key *textLeafKey) []textFadeRange {
	if cx.streamFades == nil || key == nil {
		return nil
	}
	return cx.streamFades(*key)
}

func (cx *nodeRenderContext) backgrounds(key *textLeafKey) []textColorRange {
	if cx.rangeBackgrounds == nil || key == nil {
		return nil
	}
	return cx.rangeBackgrounds(*key)
}

// ---------------------------------------------------------------------------
// Side-table caches
// ---------------------------------------------------------------------------

// textSideCache is a weak-keyed cache of render state derived from a node.
type textSideCache[K any, V any] struct {
	mu sync.Mutex
	m  map[weak.Pointer[K]]V
}

func (c *textSideCache[K, V]) get(k *K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[weak.Make(k)]
	return v, ok
}

func (c *textSideCache[K, V]) put(k *K, v V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[weak.Pointer[K]]V{}
	}
	if len(c.m) >= retainedSweepAt {
		for key := range c.m {
			if key.Value() == nil {
				delete(c.m, key)
			}
		}
	}
	c.m[weak.Make(k)] = v
}

// imageNodeEmbedded caches ImageNode::embedded (U:text/node.rs:509-514).
var imageNodeEmbedded textSideCache[ImageNode, *gpui.Image]

// imageNodeSource is ImageNode::source: a data: image is decoded once,
// every other value stays URI-backed (U:text/node.rs:502-514).
func imageNodeSource(image *ImageNode) gpui.ImageSource {
	embedded, ok := imageNodeEmbedded.get(image)
	if !ok {
		embedded = dataURLImage(image.URL)
		imageNodeEmbedded.put(image, embedded)
	}
	if embedded != nil {
		return gpui.ImageSourceFromImage(*embedded)
	}
	return gpui.ImageSourceOfResource(gpui.URIResource(image.URL))
}

// tableDataCache is Table::table_data_cache (U:text/node.rs:1512-1539).
var tableDataCache textSideCache[textTable, *TableData]

// cachedTableData is Table::cached_table_data (U:text/node.rs:1627-1643).
func (t *textTable) cachedTableData() *TableData {
	if d, ok := tableDataCache.get(t); ok {
		return d
	}
	d := t.tableData()
	tableDataCache.put(t, &d)
	return &d
}

// cachedCodeBlockHighlights is U:text/node.rs:1751-1754.
type cachedCodeBlockHighlights struct {
	highlighter *CodeBlockHighlighter
	styles      []gpui.HighlightRange
}

var codeBlockHighlightCache textSideCache[CodeBlock, cachedCodeBlockHighlights]

// highlightedStyles runs the highlighter, cached by its identity; invalid
// ranges are discarded (U:text/node.rs:1839-1862).
func (c *CodeBlock) highlightedStyles(h *CodeBlockHighlighter) []gpui.HighlightRange {
	if cached, ok := codeBlockHighlightCache.get(c); ok && cached.highlighter == h {
		return cached.styles
	}
	codeLen := len(c.Code())
	var styles []gpui.HighlightRange
	for _, r := range h.highlight(c) {
		if r.Range.Start <= r.Range.End && r.Range.End <= codeLen {
			styles = append(styles, r)
		}
	}
	codeBlockHighlightCache.put(c, cachedCodeBlockHighlights{highlighter: h, styles: styles})
	return styles
}

// paragraphRender is U:text/node.rs:1133-1149.
type paragraphRender struct {
	style      *TextViewStyle
	monoFont   string
	children   []inlineNode
	text       string
	highlights []inlineHighlightRange
	links      []textLinkRange
}

var paragraphRenderCache textSideCache[paragraph, *paragraphRender]

// sameChildren reports the children a render was built from.
func sameChildren(a, b []inlineNode) bool {
	return len(a) == len(b) && (len(a) == 0 || &a[0] == &b[0])
}

// markHighlight is the highlight a text mark renders with; links are the
// caller's (U:text/node.rs:2050-2085).
func markHighlight(mark TextMark, cx *nodeRenderContext, app *gpui.App) inlineHighlight {
	var h gpui.HighlightStyle
	if mark.Bold {
		w := textFontWeightBold
		h.FontWeight = &w
	}
	if mark.Italic {
		s := gpui.FontStyleItalic
		h.FontStyle = &s
	}
	if mark.Strikethrough {
		h.Strikethrough = &gpui.StrikethroughStyle{Thickness: 1}
	}
	if mark.Underline {
		h.Underline = &gpui.UnderlineStyle{Thickness: 1}
	}
	var family *string
	if mark.Code {
		h = h.Highlight(cx.style.inlineCodeHighlight())
		mono := ThemeGlobal(app).Tokens.Typography.Mono
		family = &mono
	}
	if mark.Highlight != nil {
		c := *mark.Highlight
		h.BackgroundColor = &c
	}
	out := inlineHighlight{style: h, fontFamily: family}
	if mark.Code {
		scale := float32(0.875)
		out.fontSizeScale = &scale
	}
	return out
}

// linkHighlight decorates a link (U:text/node.rs:1205-1210).
func linkHighlight(h inlineHighlight, cx *nodeRenderContext) inlineHighlight {
	link := cx.style.Link()
	h.style.Color = &link
	h.style.Underline = &gpui.UnderlineStyle{Thickness: 1}
	return h
}

// resolveLink replaces a reference link by its definition.
func resolveLink(link LinkMark, cx *nodeRenderContext) LinkMark {
	if link.Identifier != nil {
		if mark, ok := cx.linkRefs[*link.Identifier]; ok {
			return mark
		}
	}
	return link
}

// plainRender is the text, highlights and unresolved links of a text-only
// paragraph, cached while the style is unchanged (U:text/node.rs:1170-1229).
func (p *paragraph) plainRender(cx *nodeRenderContext, app *gpui.App) (string, []inlineHighlightRange, []textLinkRange) {
	mono := ThemeGlobal(app).Tokens.Typography.Mono
	if cached, ok := paragraphRenderCache.get(p); ok && sameChildren(cached.children, p.children) &&
		(cached.style == cx.style || cached.style.Equal(*cx.style)) && cached.monoFont == mono {
		return cached.text, cached.highlights, cached.links
	}
	var text strings.Builder
	var highlights []inlineHighlightRange
	var links []textLinkRange
	offset := 0
	for i := range p.children {
		node := &p.children[i]
		text.WriteString(node.text)
		var nodeHighlights []inlineHighlightRange
		for _, m := range node.marks {
			r := gpui.TextRange{Start: offset + m.rng.Start, End: offset + m.rng.End}
			h := markHighlight(m.mark, cx, app)
			if m.mark.Link != nil {
				h = linkHighlight(h, cx)
				links = append(links, textLinkRange{r, *m.mark.Link})
			}
			nodeHighlights = append(nodeHighlights, inlineHighlightRange{r, h})
		}
		highlights = combineInlineHighlights(highlights, nodeHighlights)
		offset += len(node.text)
	}
	render := &paragraphRender{style: cx.style, monoFont: mono, children: p.children, text: text.String(), highlights: highlights, links: links}
	paragraphRenderCache.put(p, render)
	return render.text, render.highlights, render.links
}

// inlineHighlights are the highlights over the paragraph's text, link
// colors left out (U:text/node.rs:2087-2113).
func (p *paragraph) inlineHighlights(cx *nodeRenderContext, app *gpui.App) []inlineHighlightRange {
	var highlights []inlineHighlightRange
	offset := 0
	for i := range p.children {
		node := &p.children[i]
		var nodeHighlights []inlineHighlightRange
		for _, m := range node.marks {
			nodeHighlights = append(nodeHighlights, inlineHighlightRange{gpui.TextRange{Start: offset + m.rng.Start, End: offset + m.rng.End}, markHighlight(m.mark, cx, app)})
		}
		highlights = combineInlineHighlights(highlights, nodeHighlights)
		offset += len(node.text)
	}
	return highlights
}

// render renders the paragraph; leafKey names its text for fades and
// range highlights (U:text/node.rs:2115-2315).
func (p *paragraph) render(leafKey *textLeafKey, cx *nodeRenderContext, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	fades := cx.fades(leafKey)
	backgrounds := cx.backgrounds(leafKey)
	if p.shouldRenderInlineFlow() {
		return newInlineFlow(cx.leafElementID(leafKey), p.inlineFlowItems(leafKey, fades, backgrounds, cx, app), cx.linkClickHandler).IntoElement()
	}
	hasImage := false
	for i := range p.children {
		if p.children[i].image != nil {
			hasImage = true
		}
	}
	if !hasImage {
		text, highlights, cached := p.plainRender(cx, app)
		if text == "" {
			return gpui.Div().Flex().IntoElement()
		}
		links := make([]textLinkRange, len(cached))
		for i, l := range cached {
			links[i] = textLinkRange{l.rng, resolveLink(l.link, cx)}
		}
		highlights = fadeHighlights(highlights, sliceFadeRanges(fades, 0, len(text)))
		p.state.setText(text)
		return newInlineText(p.state, links, highlights, cx.linkClickHandler).
			withRangeBackgrounds(sliceColorRanges(backgrounds, 0, len(text))).
			withReveal(cx.revealAt(leafKey, 0, len(text))).IntoElement()
	}

	var children []any
	var text strings.Builder
	var highlights []inlineHighlightRange
	var links []textLinkRange
	offset, consumed := 0, 0
	for ix := range p.children {
		node := &p.children[ix]
		text.WriteString(node.text)
		if node.image != nil {
			if text.Len() > 0 {
				t := text.String()
				node.state.setText(t)
				children = append(children, newInlineText(node.state, links,
					fadeHighlights(highlights, sliceFadeRanges(fades, consumed, consumed+len(t))), cx.linkClickHandler).
					withRangeBackgrounds(sliceColorRanges(backgrounds, consumed, consumed+len(t))).
					withReveal(cx.revealAt(leafKey, consumed, consumed+len(t))).IntoElement())
			}
			image := gpui.Img(cx.imageSourceOf(node.image)).Flex().ID(gpui.IntegerElementID(uint64(ix))).
				ObjectFit(gpui.ObjectFitContain).MaxW(gpui.Fraction(1))
			if node.image.Width != nil {
				image = image.W(*node.image.Width)
			}
			if node.image.Link != nil {
				children = append(children, textLinkedImage(gpui.NamedIntegerElementID("image-link", uint64(ix)), image, node.image.Link.URL, cx.linkClickHandler))
			} else {
				children = append(children, image)
			}
			consumed += text.Len()
			text.Reset()
			links, highlights = nil, nil
			offset = 0
			continue
		}
		var nodeHighlights []inlineHighlightRange
		for _, m := range node.marks {
			r := gpui.TextRange{Start: offset + m.rng.Start, End: offset + m.rng.End}
			h := markHighlight(m.mark, cx, app)
			if m.mark.Link != nil {
				h = linkHighlight(h, cx)
				links = append(links, textLinkRange{r, resolveLink(*m.mark.Link, cx)})
			}
			nodeHighlights = append(nodeHighlights, inlineHighlightRange{r, h})
		}
		highlights = combineInlineHighlights(highlights, nodeHighlights)
		offset += len(node.text)
	}
	if text.Len() > 0 {
		t := text.String()
		end := consumed + len(t)
		p.state.setText(t)
		children = append(children, newInlineText(p.state, links, fadeHighlights(highlights, sliceFadeRanges(fades, consumed, end)), cx.linkClickHandler).
			withRangeBackgrounds(sliceColorRanges(backgrounds, consumed, end)).
			withReveal(cx.revealAt(leafKey, consumed, end)).IntoElement())
	}
	// Images keep an identified box: an image's element state is its
	// animation, and the box scopes that state per paragraph.
	return textBlockDiv().ID(cx.leafElementID(leafKey)).Children(children...).IntoElement()
}

// shouldRenderInlineFlow is U:text/node.rs:2317-2326.
func (p *paragraph) shouldRenderInlineFlow() bool {
	hasImage, hasText := false, false
	for i := range p.children {
		c := &p.children[i]
		if c.custom != nil {
			return true
		}
		if c.image != nil {
			hasImage = true
		}
		if c.text != "" {
			hasText = true
		}
		for _, m := range c.marks {
			if m.mark.Code {
				return true
			}
		}
	}
	return hasImage && hasText
}

// inlineFlowItems builds the flow of a paragraph with images, inline code
// or custom inline nodes (U:text/node.rs:2328-2490).
func (p *paragraph) inlineFlowItems(leafKey *textLeafKey, fades []textFadeRange, backgrounds []textColorRange, cx *nodeRenderContext, app *gpui.App) []inlineFlowItem {
	var items []inlineFlowItem
	var text strings.Builder
	var highlights []inlineHighlightRange
	var links []textLinkRange
	offset, consumed := 0, 0
	pushText := func(state *inlineState) {
		t := text.String()
		state.setText(t)
		items = append(items, inlineFlowItem{
			kind: inlineFlowText, state: state, text: t, links: links,
			highlights:  fadeHighlights(highlights, sliceFadeRanges(fades, consumed, consumed+len(t))),
			backgrounds: sliceColorRanges(backgrounds, consumed, consumed+len(t)),
			reveal:      cx.revealAt(leafKey, consumed, consumed+len(t)),
		})
	}
	for i := range p.children {
		node := &p.children[i]
		if node.custom != nil {
			node.state.setText(text.String())
			if text.Len() > 0 {
				pushText(node.state)
				consumed += text.Len()
				text.Reset()
				links, highlights = nil, nil
			}
			var style gpui.HighlightStyle
			var link *LinkMark
			for _, m := range node.marks {
				style = style.Highlight(markHighlight(m.mark, cx, app).style)
				if m.mark.Link != nil {
					resolved := resolveLink(*m.mark.Link, cx)
					link = &resolved
					c := cx.style.Link()
					style.Color = &c
					style.Underline = &gpui.UnderlineStyle{Thickness: 1}
				}
			}
			custom := *node.custom
			extensions := cx.markdownExtensions
			id := len(items)
			if r, ok := custom.SourceRange(); ok {
				id = r.Start
			}
			items = append(items, inlineFlowItem{
				kind: inlineFlowObject, text: custom.AsText(), accessibilityLabel: custom.AccessibilityName(),
				id: id, selected: node.customSelection, style: style, link: link,
				renderer: func(context *InlineRenderContext, w *gpui.Window, app *gpui.App) (InlineElement, bool) {
					return extensions.renderInline(&custom, context, w, app)
				},
			})
			consumed += len(node.text)
			offset = 0
			continue
		}
		text.WriteString(node.text)
		if node.image != nil {
			if text.Len() > 0 {
				pushText(node.state)
			}
			items = append(items, inlineFlowItem{
				kind: inlineFlowImage, source: cx.imageSourceOf(node.image), link: node.image.Link,
				title: node.image.TitleText(), width: node.image.Width, height: node.image.Height,
			})
			consumed += text.Len()
			text.Reset()
			links, highlights = nil, nil
			offset = 0
			continue
		}
		var nodeHighlights []inlineHighlightRange
		for _, m := range node.marks {
			r := gpui.TextRange{Start: offset + m.rng.Start, End: offset + m.rng.End}
			h := markHighlight(m.mark, cx, app)
			if m.mark.Link != nil {
				h = linkHighlight(h, cx)
				links = append(links, textLinkRange{r, resolveLink(*m.mark.Link, cx)})
			}
			nodeHighlights = append(nodeHighlights, inlineHighlightRange{r, h})
		}
		highlights = combineInlineHighlights(highlights, nodeHighlights)
		offset += len(node.text)
	}
	if text.Len() > 0 {
		pushText(p.state)
	}
	return items
}

// blockElementID is the id of a block that needs one: its kind and source
// start, or its index without a span (U:text/node.rs:2493-2499).
func blockElementID(kind string, span *Span, ix int) gpui.ElementID {
	v := ix
	if span != nil {
		v = span.Start
	}
	return gpui.NamedIntegerElementID(kind, uint64(v))
}

// gapped puts gap below block; a zero gap returns it as is
// (U:text/node.rs:2509-2517).
func gapped(block gpui.AnyElement, gap gpui.AbsoluteLength) gpui.AnyElement {
	if gap.Value == 0 {
		return block
	}
	return textBlockDiv().Pb(gap).Child(block).IntoElement()
}

// render renders a code block (U:text/node.rs:1914-1981).
func (c *CodeBlock) render(options nodeRenderOptions, cx *nodeRenderContext, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	style := cx.style
	theme := ThemeGlobal(app)
	leafKey := spanLeafKey(c.Span)
	var highlights []inlineHighlightRange
	if cx.codeBlockHighlighter != nil {
		for _, r := range c.highlightedStyles(cx.codeBlockHighlighter) {
			highlights = append(highlights, inlineHighlightRange{r.Range, inlineHighlightOf(r.Style)})
		}
	}
	codeBlock := style.CodeBlock()
	block := StyledExtOf(textBlockDiv().WFull().MinW0().P3().Bg(style.CodeBackground()).
		FontFamily(theme.Tokens.Typography.Mono).TextSize(gpui.Px(theme.Tokens.Typography.MonoMd.Size)).Relative()).
		RefineStyle(&codeBlock).
		Child(newInlineText(c.state, nil, fadeHighlights(highlights, cx.fades(leafKey)), cx.linkClickHandler).
			withRangeBackgrounds(cx.backgrounds(leafKey)).
			withReveal(cx.revealAt(leafKey, 0, len(c.Code()))))
	var el gpui.AnyElement
	if cx.codeBlockActions != nil {
		el = block.ID(blockElementID("codeblock", c.Span, options.ix)).
			Child(gpui.Div().Flex().ID("actions").Absolute().Top2().Right2().Bg(style.CodeBackground()).
				Rounded(gpui.Px(theme.Tokens.Radius.Md)).Child(cx.codeBlockActions(c, w, app))).
			IntoElement()
	} else {
		el = block.IntoElement()
	}
	gap := style.ParagraphGap()
	if options.isLast {
		gap = gpui.RemsOf(0)
	}
	return gapped(el, gap)
}

const (
	cellPadPx    float32 = 16 // px_2 horizontal padding
	cellMinPx    float32 = 48
	cellBorderPx float32 = 1 // border_r_1 on every column but the last
)

// measureTableColumns is the max-content width of every column: the widest
// cell line, shaped with the runs the cell renders with, plus padding and
// border (U:text/node.rs:2542-2632).
func measureTableColumns(table *textTable, colCount int, cx *nodeRenderContext, w *gpui.Window, app *gpui.App) []float32 {
	textStyle := gpui.WindowTextStyle(w)
	fontSize := textStyle.FontSizePixels(gpui.RemSize(w))
	colW := make([]float32, colCount)
	for i := range colW {
		colW[i] = cellMinPx
	}
	for _, row := range table.children {
		for ix, cell := range row.children {
			if ix >= colCount {
				continue
			}
			border := float32(0)
			if ix+1 < colCount {
				border = cellBorderPx
			}
			hasCustom := false
			for i := range cell.children.children {
				if cell.children.children[i].custom != nil {
					hasCustom = true
				}
			}
			if hasCustom {
				items := cell.children.inlineFlowItems(nil, nil, nil, cx, app)
				colW[ix] = max(colW[ix], inlineFlowIntrinsicWidth(items, w, app)+cellPadPx+border)
				continue
			}
			text := cell.children.text()
			highlights := cell.children.inlineHighlights(cx, app)
			width := float32(0)
			lineStart := 0
			for _, line := range strings.Split(text, "\n") {
				start := lineStart + (len(line) - len(strings.TrimLeftFunc(line, isRustWhitespace)))
				lineStart += len(line) + 1
				line = strings.TrimFunc(line, isRustWhitespace)
				if line == "" {
					continue
				}
				lineHighlights := sliceHighlightRanges(highlights, start, start+len(line))
				lineW := float32(0)
				for _, r := range textSizeRanges(len(line), lineHighlights) {
					hl := sliceHighlightRanges(lineHighlights, r.rng.Start, r.rng.End)
					if highlightsHaveScale(hl) {
						lineW += inlineCodePadding * 2
					}
					runs := textRuns(r.rng.End-r.rng.Start, textStyle, hl)
					lw, _, _ := shapedLineMetrics(line[r.rng.Start:r.rng.End], fontSize*r.scale, runs, w)
					lineW += lw
				}
				width = max(width, lineW)
			}
			colW[ix] = max(colW[ix], width+cellPadPx+border)
		}
	}
	return colW
}

// renderListItemRow is U:text/node.rs:2802-2860.
func renderListItemRow(content gpui.AnyElement, ix int, options nodeRenderOptions, checked *bool, style *TextViewStyle, lineHeight float32) *gpui.DivElement {
	row := HFlex().WFull().MinW0().Relative().ItemsStart().ContentStart()
	if !options.todo && checked == nil {
		row = row.Child(listItemPrefix(ix, options.listStart, options.ordered, options.depth))
	}
	if checked != nil {
		svg := textCheckSVGLight
		if style.IsDark() {
			svg = textCheckSVGDark
		}
		box := gpui.Div().Flex().Size(gpui.RemsOf(0.875)).ItemsCenter().JustifyCenter().Border1().BorderColor(style.Foreground())
		if *checked {
			box = box.Bg(style.Foreground()).Child(gpui.Img(gpui.NewImage(gpui.ImageFormatSVG, svg)).Flex().Size(gpui.RemsOf(0.625)))
		}
		row = row.Child(gpui.Div().Flex().Mr1p5().H(gpui.Px(lineHeight)).FlexNone().ItemsCenter().JustifyCenter().Child(box))
	}
	return row.Child(textBlockDiv().Flex1().MinW0().OverflowHidden().Child(content))
}

// renderListItem is U:text/node.rs:2862-2994.
func renderListItem(item blockNode, ix int, options nodeRenderOptions, cx *nodeRenderContext, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	li, ok := item.(*blockListItem)
	if !ok {
		return gpui.Div().Flex().IntoElement()
	}
	child := func() nodeRenderOptions {
		o := options
		o.depth++
		o.todo = li.checked != nil
		o.isLast = true
		return o
	}
	el := textBlockDiv().WFull().MinW0()
	if li.spread {
		el = el.Child(gpui.Div().Flex())
	}
	var rows []*gpui.DivElement
	for childIx, c := range li.children {
		switch c.(type) {
		case *paragraph:
			lastNotList := false
			if childIx > 0 {
				_, isList := li.children[childIx-1].(*blockList)
				lastNotList = !isList
			}
			text := renderBlock(c, child(), cx, w, app)
			if lastNotList && len(rows) > 0 {
				prev := rows[len(rows)-1]
				rows[len(rows)-1] = textBlockDiv().Child(prev).Child(textBlockDiv().WFull().Pl(gpui.RemsOf(1)).OverflowHidden().Child(text))
				continue
			}
			rows = append(rows, renderListItemRow(text, ix, options, li.checked, cx.style, w.LineHeight()))
		case *blockList:
			rows = append(rows, textBlockDiv().Ml(gpui.RemsOf(1)).Child(renderBlock(c, child(), cx, w, app)))
		case *blockRoot, *blockHeading, *blockBlockquote, *CodeBlock, *blockCustom, *textTable, *blockHorizontalRule:
			block := renderBlock(c, child(), cx, w, app)
			if childIx == 0 {
				rows = append(rows, renderListItemRow(block, ix, options, li.checked, cx.style, w.LineHeight()))
			} else {
				rows = append(rows, textBlockDiv().WFull().MinW0().Pl(gpui.RemsOf(1)).OverflowHidden().Child(block))
			}
		}
	}
	for _, r := range rows {
		el = el.Child(r)
	}
	return el.IntoElement()
}

// renderTable dispatches to the scroll layout when the table refinement
// sets overflow-x scroll (U:text/node.rs:2996-3031).
func renderTable(table *textTable, options nodeRenderOptions, cx *nodeRenderContext, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	const defaultLength = 5
	var colLens []int
	for _, row := range table.children {
		for ix, cell := range row.children {
			if len(colLens) <= ix {
				colLens = append(colLens, defaultLength)
			}
			colLens[ix] = max(colLens[ix], cell.children.textLen())
		}
	}
	tableStyle := cx.style.Table()
	if overflow, ok := tableStyle.OverflowX.Get(); ok && overflow == gpui.OverflowScroll {
		return renderScrollTable(table, len(colLens), options, cx, w, app)
	}
	return renderWrapTable(table, colLens, options, cx, w, app)
}

// tableCell is one cell box of either layout.
func tableCell(table *textTable, cell textTableCell, ix, rowLen int, cx *nodeRenderContext, leafKey *textLeafKey, base *gpui.DivElement, w *gpui.Window, app *gpui.App) *gpui.DivElement {
	style := cx.style
	align := table.columnAlign(ix)
	el := base.OverflowHidden()
	if align == columnAlignCenter {
		el = el.TextCenter()
	}
	if align == columnAlignRight {
		el = el.TextRight()
	}
	el = el.Px2().Py1()
	if ix != rowLen-1 {
		el = el.BorderR1().BorderColor(style.Border())
	}
	cellStyle := style.TableCell()
	return StyledExtOf(el).RefineStyle(&cellStyle).Child(cell.children.render(leafKey, cx, w, app))
}

// tableRows builds the rows of either layout (U:text/node.rs:3097-3152,
// 3211-3263).
func tableRows(table *textTable, cx *nodeRenderContext, cellBase func(ix int) *gpui.DivElement, w *gpui.Window, app *gpui.App) []any {
	style := cx.style
	rowCount := len(table.children)
	var rows []any
	ordinal := 0
	for rowIx, row := range table.children {
		r := gpui.Div().Flex().WFull()
		if rowIx < rowCount-1 {
			r = r.BorderB1()
		}
		r = r.BorderColor(style.Border()).Flex().FlexRow()
		if rowIx == 0 {
			head := style.TableHead()
			r = StyledExtOf(r.Bg(style.CodeBackground()).TextColor(style.Foreground())).RefineStyle(&head)
		}
		for ix, cell := range row.children {
			var leafKey *textLeafKey
			if table.span != nil {
				leafKey = textLeafTableCell(table.span.Start, ordinal)
			}
			ordinal++
			r = r.Child(tableCell(table, cell, ix, len(row.children), cx, leafKey, cellBase(ix), w, app))
		}
		rows = append(rows, r)
	}
	return rows
}

// tableActionsRow is the actions row below a table
// (U:text/node.rs:3183-3194); upstream's plain div is a Block box whose
// child spans the full width (gap 2b, see the file comment).
func tableActionsRow(el *gpui.DivElement, table *textTable, options nodeRenderOptions, cx *nodeRenderContext, w *gpui.Window, app *gpui.App) *gpui.DivElement {
	if cx.tableActions == nil {
		return el
	}
	return el.Child(textBlockDiv().ID(blockElementID("table-actions", table.span, options.ix)).Mt1().
		Child(cx.tableActions(table.cachedTableData(), w, app)))
}

// renderScrollTable is the horizontally scrollable layout: measured column
// widths as flex bases, shrinking to per-column floors, then scrolling
// (U:text/node.rs:3033-3196).
func renderScrollTable(table *textTable, colCount int, options nodeRenderOptions, cx *nodeRenderContext, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	const (
		cellWrapMaxLines float32 = 2
		cellWrapMinPx    float32 = 160
		cellWrapMaxPx    float32 = 480
		tableBorderPx    float32 = 2
	)
	colW := measureTableColumns(table, colCount, cx, w, app)
	style := cx.style
	cellStyle := style.TableCell()
	ws, hasWS := cellStyle.Text.WhiteSpace.Get()
	nowrap := hasWS && ws == gpui.WhiteSpaceNowrap
	colMinW := make([]float32, len(colW))
	minTotal := tableBorderPx
	for i, cw := range colW {
		if nowrap {
			colMinW[i] = cw
		} else {
			colMinW[i] = min(min(max(cw/cellWrapMaxLines, cellWrapMinPx), cellWrapMaxPx), cw)
		}
		minTotal += colMinW[i]
	}
	handle := w.UseKeyedState(blockElementID("table-scroll", table.span, options.ix), app, func(*gpui.Window, *gpui.Context[gpui.ScrollHandle]) gpui.ScrollHandle {
		return gpui.NewScrollHandle()
	}).Read(app, func(h *gpui.ScrollHandle, _ *gpui.App) gpui.ScrollHandle { return *h })
	rows := tableRows(table, cx, func(ix int) *gpui.DivElement {
		width, minWidth := cellMinPx, cellMinPx
		if ix < len(colW) {
			width, minWidth = colW[ix], colMinW[ix]
		}
		return textBlockDiv().FlexBasis(gpui.Px(width)).FlexGrow(width).FlexShrink(1).MinW(gpui.Px(minWidth))
	}, w, app)
	frame := NewStateStyle().Bg(ThemeGlobal(app).Tokens.Colors.Surface).Border1().BorderColor(style.Border()).intoRefinement()
	tableStyle := style.Table()
	frame.Refine(&tableStyle)
	el := textBlockDiv().Pb(gpui.RemsOf(1)).WFull().Child(
		// The row track is upstream's plain div (U:text/node.rs:3180), a
		// Block box under 0.2.2 that stacks the rows; under the pin's Flex
		// default it laid every row out on one line (gap 2b).
		horizontalScrollArea(blockElementID("table", table.span, options.ix), handle, &frame,
			textBlockDiv().MinWFull().W(gpui.Px(minTotal)).Children(rows...)))
	return tableActionsRow(el, table, options, cx, w, app).IntoElement()
}

// renderWrapTable is the default layout: columns proportioned by content
// length that shrink to fit (U:text/node.rs:3198-3291).
func renderWrapTable(table *textTable, colLens []int, options nodeRenderOptions, cx *nodeRenderContext, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	const maxLength = 150
	style := cx.style
	rows := tableRows(table, cx, func(ix int) *gpui.DivElement {
		n := maxLength
		if ix < len(colLens) {
			n = min(colLens[ix], maxLength)
		}
		return textBlockDiv().MinW16().W(gpui.Fraction(float32(n)))
	}, w, app)
	tableStyle := style.Table()
	// The frame is upstream's plain div (U:text/node.rs:3269-3276), a Block
	// box under 0.2.2 that stacks the rows; under the pin's Flex default it
	// laid every row out on one line (gap 2b).
	inner := StyledExtOf(textBlockDiv().WFull().Bg(ThemeGlobal(app).Tokens.Colors.Surface).Border1().
		BorderColor(style.Border()).OverflowHidden().Children(rows...)).RefineStyle(&tableStyle)
	el := textBlockDiv().Pb(gpui.RemsOf(1)).WFull().Child(inner)
	return tableActionsRow(el, table, options, cx, w, app).IntoElement()
}

// renderBlock renders one block (U:text/node.rs:3293-3434).
func renderBlock(b blockNode, options nodeRenderOptions, cx *nodeRenderContext, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	mb := cx.style.ParagraphGap()
	if options.inList || options.isLast {
		mb = gpui.RemsOf(0)
	}
	switch v := b.(type) {
	case *blockRoot:
		el := textBlockDiv()
		for ix, c := range v.children {
			o := options
			o.ix = ix
			el = el.Child(renderBlock(c, o, cx, w, app))
		}
		return el.IntoElement()
	case *paragraph:
		return gapped(v.render(spanLeafKey(v.span), cx, w, app), mb)
	case *blockHeading:
		var size float32
		weight := textFontWeightSemibold
		switch v.level {
		case 1:
			size, weight = 2, textFontWeightBold
		case 2:
			size = 1.5
		case 3:
			size = 1.25
		case 4:
			size = 1.125
		case 5:
			size = 1
		case 6:
			size, weight = 1, textFontWeightMedium
		default:
			size, weight = 1, textFontWeightNormal
		}
		// rems(..).to_pixels(px(14.)).
		heading := cx.style.Heading(v.level)
		return StyledExtOf(textBlockDiv().Pb(gpui.RemsOf(0.3)).WhitespacePreWrap().TextSize(gpui.Px(size * 14)).FontWeight(weight)).
			RefineStyle(&heading).Child(v.children.render(spanLeafKey(v.span), cx, w, app)).IntoElement()
	case *blockBlockquote:
		el := textBlockDiv().WFull().TextColor(cx.style.MutedForeground()).BorderL3().BorderColor(cx.style.Border()).Px4()
		for i, c := range v.children {
			el = el.Child(renderBlock(c, options.withIsLast(i == len(v.children)-1), cx, w, app))
		}
		return gapped(el.IntoElement(), mb)
	case *blockList:
		el := textBlockDiv().WFull().MinW0().Pb(mb)
		itemIndex := 0
		for ix, item := range v.children {
			o := options
			o.ix, o.ordered, o.listStart = ix, v.ordered, v.start
			el = el.Child(renderListItem(item, itemIndex, o, cx, w, app))
			if blockNodeIsListItem(item) {
				itemIndex++
			}
		}
		return el.IntoElement()
	case *CodeBlock:
		return v.render(options, cx, w, app)
	case *blockCustom:
		inner, ok := cx.markdownExtensions.renderBlock(&v.node, w, app)
		if !ok {
			inner = gpui.Div().Flex().Child(v.node.AsText()).IntoElement()
		}
		return textBlockDiv().Pb(mb).Child(inner).IntoElement()
	case *textTable:
		return renderTable(v, options, cx, w, app)
	case *blockHorizontalRule:
		return gapped(gpui.Div().Flex().Bg(cx.style.Border()).H(gpui.Px(2)).IntoElement(), mb)
	}
	return gpui.Div().Flex().IntoElement()
}

// renderRoot renders the document: every block in a box, or a virtualized
// list when the view scrolls (U:text/document.rs:189-245).
func (d *parsedDocument) renderRoot(list *gpui.ListState, cx *nodeRenderContext, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	if list == nil {
		el := textBlockDiv()
		for ix, b := range d.blocks {
			cx.startBlock(ix)
			el = el.Child(renderBlock(b, nodeRenderOptions{ix: ix, isLast: ix+1 == len(d.blocks)}, cx, w, app))
		}
		return el.IntoElement()
	}
	blocks := d.blocks
	if list.ItemCount() != len(blocks) {
		list.Reset(len(blocks))
	}
	return gpui.Div().Flex().SizeFull().Child(gpui.List(list, func(ix int, w *gpui.Window, app *gpui.App) gpui.AnyElement {
		cx.startBlock(ix)
		// An item is laid out as a root at the list's width. Upstream's
		// item roots are plain divs (gapped, Root), Block boxes that fill
		// that width under 0.2.2; a flex root takes its content's width
		// instead, which shrank a nested table or a code block's
		// background to its text (gap 2b). The full-width box restores it.
		return textBlockDiv().WFull().Child(
			renderBlock(blocks[ix], nodeRenderOptions{ix: ix, isLast: ix+1 == len(blocks)}, cx, w, app)).IntoElement()
	}).SizeFull()).IntoElement()
}
