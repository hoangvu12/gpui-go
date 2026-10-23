package base

// Port of U:text/format/html.rs (ticket 30): HTML source -> Base's
// block/inline node tree. The source is minified first
// (text_html5minify.go), then parsed with golang.org/x/net/html in place of
// html5ever + RcDom (ticket 01).
//
// Naming: text_markdown.go owns the unprefixed parse helpers, so this
// module's take the html prefix (htmlParse, htmlParseParagraph, ...).
//
// Go adaptations (recorded deviations):
//   - The two debug-build tracing::warn! calls for an <img> without src
//     (U:text/format/html.rs:389-391, 484-486) are dropped; the node is
//     skipped as upstream.
//   - Rust's f32 parse for lengths is strconv.ParseFloat restricted to the
//     decimal syntax Rust accepts (no hex floats or digit separators).

import (
	"slices"
	"strconv"
	"strings"

	"github.com/hoangvu12/gpui-go/gpui"
	xhtml "golang.org/x/net/html"
)

func init() {
	// Install the parser ticket 27/28 left the hook for
	// (text_markdown.go textHTMLParse).
	textHTMLParse = htmlParse
}

// htmlBlockElements is BLOCK_ELEMENTS (U:text/format/html.rs:18-54).
var htmlBlockElements = []string{
	"html", "body", "head", "address", "article", "aside", "blockquote", "details", "summary",
	"dialog", "div", "dl", "fieldset", "figcaption", "figure", "footer", "form", "h1", "h2",
	"h3", "h4", "h5", "h6", "header", "hr", "main", "nav", "ol", "p", "pre", "section",
	"table", "ul", "style", "script",
}

// htmlParse parses HTML into a document (U:text/format/html.rs:56-81).
func htmlParse(source string, cx *nodeContext) (*parsedDocument, error) {
	cleaned := htmlCleanup(source)
	dom, err := xhtml.Parse(strings.NewReader(string(cleaned)))
	if err != nil {
		return nil, err
	}
	// NOTE (upstream): the outer paragraph is not used.
	p := defaultParagraph()
	node := htmlParseNode(dom, p, cx)
	if node == nil {
		node = blockUnknown{}
	}
	return &parsedDocument{source: source, blocks: []blockNode{blockNodeCompact(node)}}, nil
}

// htmlCleanup minifies the source, or keeps it when minifying fails
// (U:text/format/html.rs:83-93).
func htmlCleanup(source string) []byte {
	out, err := minifyHTML(source, func(m *html5Minifier) { m.setOmitDoctype(true) })
	if err != nil {
		return []byte(source)
	}
	return out
}

// htmlAttrValue is attr_value (U:text/format/html.rs:95-103).
func htmlAttrValue(attrs []xhtml.Attribute, name string) (string, bool) {
	for _, a := range attrs {
		if a.Key == name {
			return a.Val, true
		}
	}
	return "", false
}

// htmlMarkColor is the highlight background of a <mark>: the color
// attribute, then the style's background-color
// (U:text/format/html.rs:105-127).
func htmlMarkColor(attrs []xhtml.Attribute) (gpui.Hsla, bool) {
	if value, ok := htmlAttrValue(attrs, "color"); ok {
		if color, ok := htmlParseMarkColor(strings.TrimSpace(value)); ok {
			return color, true
		}
	}
	if value, ok := htmlStyleAttrs(attrs)["background-color"]; ok {
		return htmlParseMarkColor(strings.TrimSpace(value))
	}
	return gpui.Hsla{}, false
}

// htmlParseMarkColor accepts CSS hex plus common named colors
// (U:text/format/html.rs:129-140).
func htmlParseMarkColor(value string) (gpui.Hsla, bool) {
	if strings.HasPrefix(value, "#") {
		return parseHex(value)
	}
	switch htmlASCIILower(value) {
	case "black":
		return gpui.Rgb(0x000000), true
	case "white":
		return gpui.Rgb(0xffffff), true
	case "blue":
		return gpui.Rgb(0x3b82f6), true
	case "yellow":
		return gpui.Rgb(0xfacc15), true
	}
	return gpui.Hsla{}, false
}

// htmlStyleAttrs is the style attribute's declarations
// (U:text/format/html.rs:142-161).
func htmlStyleAttrs(attrs []xhtml.Attribute) map[string]string {
	styles := map[string]string{}
	css, ok := htmlAttrValue(attrs, "style")
	if !ok {
		return styles
	}
	for _, decl := range strings.Split(css, ";") {
		key, value, ok := strings.Cut(decl, ":")
		if !ok {
			continue
		}
		styles[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
	}
	return styles
}

// rustParseF32 is str::parse::<f32> for the decimal syntax.
func rustParseF32(s string) (float32, bool) {
	if strings.ContainsAny(s, "_xXpP") {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 32)
	if err != nil {
		return 0, false
	}
	return float32(v), true
}

// htmlValueToLength parses a length: a percentage is relative, anything
// else pixels (U:text/format/html.rs:163-181).
func htmlValueToLength(value string) (*gpui.DefiniteLength, bool) {
	if strings.HasSuffix(value, "%") {
		v, ok := rustParseF32(trimSuffixAll(value, "%"))
		if !ok {
			return nil, false
		}
		l := gpui.Fraction(v / 100)
		return &l, true
	}
	v, ok := rustParseF32(trimSuffixAll(value, "px"))
	if !ok {
		return nil, false
	}
	l := gpui.DefinitePx(v)
	return &l, true
}

// trimSuffixAll is str::trim_end_matches for a string pattern.
func trimSuffixAll(s, suffix string) string {
	for strings.HasSuffix(s, suffix) {
		s = strings.TrimSuffix(s, suffix)
	}
	return s
}

// htmlAttrWidthHeight is the width and height from attributes, else from
// the style attribute (U:text/format/html.rs:183-209).
func htmlAttrWidthHeight(attrs []xhtml.Attribute) (*gpui.DefiniteLength, *gpui.DefiniteLength) {
	var width, height *gpui.DefiniteLength
	if v, ok := htmlAttrValue(attrs, "width"); ok {
		width, _ = htmlValueToLength(v)
	}
	if v, ok := htmlAttrValue(attrs, "height"); ok {
		height, _ = htmlValueToLength(v)
	}
	if width == nil || height == nil {
		styles := htmlStyleAttrs(attrs)
		if width == nil {
			if v, ok := styles["width"]; ok {
				width, _ = htmlValueToLength(v)
			}
		}
		if height == nil {
			if v, ok := styles["height"]; ok {
				height, _ = htmlValueToLength(v)
			}
		}
	}
	return width, height
}

// htmlParseTableRow is U:text/format/html.rs:211-235.
func htmlParseTableRow(table *textTable, node *xhtml.Node) {
	var row textTableRow
	count := 0
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == xhtml.ElementNode && (child.Data == "td" || child.Data == "th") {
			if child.FirstChild == nil {
				continue
			}
			count++
			htmlParseTableCell(&row, child)
		}
	}
	if count > 0 {
		table.children = append(table.children, row)
	}
}

// htmlParseTableCell is U:text/format/html.rs:237-252.
func htmlParseTableCell(row *textTableRow, node *xhtml.Node) {
	p := defaultParagraph()
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		htmlParseParagraph(p, child)
	}
	width, _ := htmlAttrWidthHeight(node.Attr)
	row.children = append(row.children, textTableCell{children: p, width: width})
}

// htmlTrimText trims text but leaves at least one space
// (U:text/format/html.rs:254-272; dead code upstream, kept for its test).
func htmlTrimText(text string) string {
	var out strings.Builder
	i := 0
	for _, c := range text {
		if !(isRustWhitespace(c) && i > 0 && strings.HasSuffix(out.String(), " ")) {
			out.WriteRune(c)
		}
		i++
	}
	return out.String()
}

// htmlPushMerged is push_merged (U:text/format/html.rs:275-297).
func htmlPushMerged(p *paragraph, text string, marks []textMarkRange, newMark *TextMark) {
	if text == "" {
		return
	}
	node := newInlineNode(text).withMarks(marks)
	if newMark != nil {
		n := len(node.text)
		if last := len(node.marks) - 1; last >= 0 && node.marks[last].rng.Start == 0 && node.marks[last].rng.End == n {
			node.marks[last].mark.Merge(*newMark)
		} else {
			node.marks = append(node.marks, textMarkRange{rng: gpui.TextRange{Start: 0, End: n}, mark: *newMark})
		}
	}
	p.push(node)
}

// htmlMergeChildrenWithMark is merge_children_with_mark
// (U:text/format/html.rs:299-336).
func htmlMergeChildrenWithMark(node *xhtml.Node, p *paragraph, newMark *TextMark) {
	var mergedText strings.Builder
	var mergedMarks []textMarkRange
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		childParagraph := defaultParagraph()
		htmlParseParagraph(childParagraph, child)
		for _, n := range childParagraph.children {
			offset := mergedText.Len()
			mergedText.WriteString(n.text)
			for _, m := range n.marks {
				mergedMarks = append(mergedMarks, textMarkRange{rng: gpui.TextRange{Start: m.rng.Start + offset, End: m.rng.End + offset}, mark: m.mark})
			}
			if n.image != nil {
				image := *n.image
				if newMark != nil && newMark.Link != nil {
					link := *newMark.Link
					image.Link = &link
				}
				htmlPushMerged(p, mergedText.String(), mergedMarks, htmlCloneMark(newMark))
				mergedText.Reset()
				mergedMarks = nil
				p.push(inlineNodeImage(image))
			}
		}
	}
	htmlPushMerged(p, mergedText.String(), mergedMarks, htmlCloneMark(newMark))
}

func htmlCloneMark(m *TextMark) *TextMark {
	if m == nil {
		return nil
	}
	c := *m
	return &c
}

// htmlParseParagraph is parse_paragraph (U:text/format/html.rs:274-416).
func htmlParseParagraph(p *paragraph, node *xhtml.Node) {
	switch node.Type {
	case xhtml.TextNode:
		p.pushStr(node.Data)
		return
	case xhtml.ElementNode:
	default:
		htmlMergeChildrenWithMark(node, p, nil)
		return
	}
	attrs := node.Attr
	switch node.Data {
	case "em", "i":
		htmlMergeChildrenWithMark(node, p, &TextMark{Italic: true})
	case "strong", "b":
		htmlMergeChildrenWithMark(node, p, &TextMark{Bold: true})
	case "del", "s":
		htmlMergeChildrenWithMark(node, p, &TextMark{Strikethrough: true})
	case "u":
		htmlMergeChildrenWithMark(node, p, &TextMark{Underline: true})
	case "code":
		htmlMergeChildrenWithMark(node, p, &TextMark{Code: true})
	case "mark":
		color, ok := htmlMarkColor(attrs)
		if !ok {
			color = gpui.Rgb(0xfef08a)
		}
		htmlMergeChildrenWithMark(node, p, &TextMark{Highlight: &color})
	case "a":
		href, _ := htmlAttrValue(attrs, "href")
		link := LinkMark{URL: href}
		if title, ok := htmlAttrValue(attrs, "title"); ok {
			link.Title = &title
		}
		htmlMergeChildrenWithMark(node, p, &TextMark{Link: &link})
	case "img":
		src, ok := htmlAttrValue(attrs, "src")
		if !ok {
			return
		}
		p.pushImage(htmlImageNode(src, attrs))
	default:
		htmlMergeChildrenWithMark(node, p, nil)
	}
}

// htmlImageNode builds the ImageNode of an <img>.
func htmlImageNode(src string, attrs []xhtml.Attribute) ImageNode {
	image := ImageNode{URL: src}
	if alt, ok := htmlAttrValue(attrs, "alt"); ok {
		image.Alt = &alt
	}
	if title, ok := htmlAttrValue(attrs, "title"); ok {
		image.Title = &title
	}
	image.Width, image.Height = htmlAttrWidthHeight(attrs)
	return image
}

// htmlParseNode is parse_node (U:text/format/html.rs:418-646).
func htmlParseNode(node *xhtml.Node, p *paragraph, cx *nodeContext) blockNode {
	switch node.Type {
	case xhtml.TextNode:
		if node.Data != "" {
			p.pushStr(node.Data)
		}
		return nil
	case xhtml.DocumentNode:
		return &blockRoot{children: htmlConsumeChildrenNodes(node, p, cx)}
	case xhtml.ElementNode:
	default:
		// Doctype, comment, processing instruction.
		return nil
	}
	attrs := node.Attr
	name := node.Data
	switch name {
	case "br":
		return &blockBreak{html: true}
	case "h1", "h2", "h3", "h4", "h5", "h6":
		var children []blockNode
		htmlConsumeParagraph(&children, p)
		level := uint8(name[1] - '0')
		heading := defaultParagraph()
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			htmlParseParagraph(heading, child)
		}
		block := &blockHeading{level: level, children: heading}
		if len(children) > 0 {
			return &blockRoot{children: append(children, block)}
		}
		return block
	case "img":
		var children []blockNode
		htmlConsumeParagraph(&children, p)
		src, ok := htmlAttrValue(attrs, "src")
		if !ok {
			return nil
		}
		image := defaultParagraph()
		image.pushImage(htmlImageNode(src, attrs))
		if len(children) > 0 {
			return &blockRoot{children: append(children, image)}
		}
		return image
	case "ul", "ol":
		return &blockList{children: htmlConsumeChildrenNodes(node, p, cx), ordered: name == "ol"}
	case "li":
		var children []blockNode
		htmlConsumeParagraph(&children, p)
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			childParagraph := defaultParagraph()
			if childNode := htmlParseNode(child, childParagraph, cx); childNode != nil {
				children = append(children, childNode)
			}
			if childParagraph.textLen() > 0 {
				// If the last child is a paragraph, merge into it.
				if n := len(children); n > 0 {
					if last, ok := children[n-1].(*paragraph); ok {
						last.merge(childParagraph)
						continue
					}
				}
				children = append(children, childParagraph)
			}
		}
		htmlConsumeParagraph(&children, p)
		return &blockListItem{children: children}
	case "table":
		var children []blockNode
		htmlConsumeParagraph(&children, p)
		table := &textTable{}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if child.Type == xhtml.ElementNode && (child.Data == "tbody" || child.Data == "thead") {
				for sub := child.FirstChild; sub != nil; sub = sub.NextSibling {
					htmlParseTableRow(table, sub)
				}
				continue
			}
			htmlParseTableRow(table, child)
		}
		htmlConsumeParagraph(&children, p)
		if len(children) > 0 {
			return &blockRoot{children: append(children, table)}
		}
		return table
	case "blockquote":
		return &blockBlockquote{children: htmlConsumeChildrenNodes(node, p, cx)}
	case "style", "script":
		return nil
	}
	if slices.Contains(htmlBlockElements, strings.TrimSpace(name)) {
		var children []blockNode
		// Case: Hello <p>Inner text of block element</p> World
		// The text before, as a node -- the "Hello".
		htmlConsumeParagraph(&children, p)
		// The inner of the block element.
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if childNode := htmlParseNode(child, p, cx); childNode != nil {
				children = append(children, childNode)
			}
		}
		htmlConsumeParagraph(&children, p)
		if len(children) == 0 {
			return nil
		}
		return &blockRoot{children: children}
	}
	// Others as inline.
	htmlParseParagraph(p, node)
	if p.isImage() {
		return p.take()
	}
	return nil
}

// htmlConsumeChildrenNodes is U:text/format/html.rs:648-663.
func htmlConsumeChildrenNodes(node *xhtml.Node, p *paragraph, cx *nodeContext) []blockNode {
	var children []blockNode
	htmlConsumeParagraph(&children, p)
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if childNode := htmlParseNode(child, p, cx); childNode != nil {
			children = append(children, childNode)
		}
		htmlConsumeParagraph(&children, p)
	}
	return children
}

// htmlConsumeParagraph is U:text/format/html.rs:665-671.
func htmlConsumeParagraph(children *[]blockNode, p *paragraph) {
	if p.isEmpty() {
		return
	}
	*children = append(*children, p.take())
}
