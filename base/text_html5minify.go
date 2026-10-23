package base

// Port of U:text/format/html5minify/mod.rs (ticket 30): the HTML5 markup
// minifier Base forked from the html5minify crate. html.rs runs it before
// parsing to collapse insignificant whitespace.
//
// Go adaptations (recorded deviations):
//   - html5ever + markup5ever_rcdom become golang.org/x/net/html (ticket 01:
//     36/36 Base inputs give trees identical to html5ever 0.27; the only
//     html5lib differences are the 2025 `<select>` parsing rules).
//   - A qualified name's prefix is x/net's attribute Namespace ("xlink",
//     "xml", "xmlns" for foreign attributes); elements carry none, as in
//     html5ever's HTML tree.
//   - The Minify trait / minify(r, w) convenience wrappers
//     (U:text/format/html5minify/mod.rs:14-51, all #[allow(dead_code)]) are
//     not ported; Base calls Minifier directly.

import (
	"bytes"
	"io"
	"strings"

	xhtml "golang.org/x/net/html"
)

// html5Minifier is the minifier over an io.Writer
// (U:text/format/html5minify/mod.rs:53-61).
type html5Minifier struct {
	w                   io.Writer
	omitDoctype         bool
	collapseWhitespace  bool
	preserveComments    bool
	precedingWhitespace bool
}

// minifyContext holds node positional context
// (U:text/format/html5minify/mod.rs:63-69).
type minifyContext struct {
	parent        *xhtml.Node
	parentContext *minifyContext
	left          []*xhtml.Node
	right         []*xhtml.Node
	hasLeft       bool
	hasRight      bool
}

// trim decides whether to trim whitespace, by naive HTML5 whitespace
// collapsing rules (U:text/format/html5minify/mod.rs:72-76).
func (c *minifyContext) trim(precedingWhitespace bool) (bool, bool) {
	return precedingWhitespace || c.trimLeft(), c.trimRight()
}

// trimLeft is U:text/format/html5minify/mod.rs:78-89.
func (c *minifyContext) trimLeft() bool {
	if !c.hasLeft {
		return minifyIsBlockElement(c.parent) || c.parentTrimLeft()
	}
	for i := len(c.left) - 1; i >= 0; i-- {
		if block, ok := minifyContextIsBlockElement(c.left[i]); ok {
			return block
		}
	}
	return c.parentTrimLeft()
}

// parentTrimLeft is U:text/format/html5minify/mod.rs:91-93.
func (c *minifyContext) parentTrimLeft() bool {
	if c.parentContext == nil {
		return true
	}
	return c.parentContext.trimLeft()
}

// trimRight is U:text/format/html5minify/mod.rs:95-102.
func (c *minifyContext) trimRight() bool {
	if !c.hasRight {
		return true
	}
	for _, n := range c.right {
		if block, ok := minifyContextIsBlockElement(n); ok {
			return block
		}
	}
	return true
}

// nextElement is U:text/format/html5minify/mod.rs:104-110.
func (c *minifyContext) nextElement() *xhtml.Node {
	for _, n := range c.right {
		if n.Type == xhtml.ElementNode {
			return n
		}
	}
	return nil
}

// minifyContextIsBlockElement is Context::is_block_element
// (U:text/format/html5minify/mod.rs:112-118): false for a non-element.
func minifyContextIsBlockElement(n *xhtml.Node) (bool, bool) {
	if n.Type != xhtml.ElementNode {
		return false, false
	}
	return isBlockElementName(n.Data), true
}

// newHTML5Minifier is Minifier::new (U:text/format/html5minify/mod.rs:125-135).
func newHTML5Minifier(w io.Writer) *html5Minifier {
	return &html5Minifier{w: w, collapseWhitespace: true}
}

// setCollapseWhitespace is U:text/format/html5minify/mod.rs:137-144.
func (m *html5Minifier) setCollapseWhitespace(collapse bool) *html5Minifier {
	m.collapseWhitespace = collapse
	return m
}

// setOmitDoctype is U:text/format/html5minify/mod.rs:146-153.
func (m *html5Minifier) setOmitDoctype(omit bool) *html5Minifier {
	m.omitDoctype = omit
	return m
}

// setPreserveComments is U:text/format/html5minify/mod.rs:155-162.
func (m *html5Minifier) setPreserveComments(preserve bool) *html5Minifier {
	m.preserveComments = preserve
	return m
}

// minify minifies the reader's input (U:text/format/html5minify/mod.rs:164-181).
func (m *html5Minifier) minify(r io.Reader) error {
	dom, err := xhtml.Parse(r)
	if err != nil {
		return err
	}
	if !m.omitDoctype {
		if _, err := io.WriteString(m.w, "<!doctype html>"); err != nil {
			return err
		}
	}
	return m.minifyNode(nil, dom)
}

func minifyChildren(n *xhtml.Node) []*xhtml.Node {
	var out []*xhtml.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		out = append(out, c)
	}
	return out
}

func (m *html5Minifier) writeAll(b []byte) error {
	_, err := m.w.Write(b)
	return err
}

// minifyNode is U:text/format/html5minify/mod.rs:183-285.
func (m *html5Minifier) minifyNode(ctx *minifyContext, node *xhtml.Node) error {
	switch node.Type {
	case xhtml.TextNode:
		contents := node.Data
		if !m.collapseWhitespace {
			return m.writeAll([]byte(contents))
		}
		// Whether the parent preserves whitespace or holds code
		// (<script>, <style>).
		skipCollapse, holdsCode := false, false
		if ctx != nil && ctx.parent.Type == xhtml.ElementNode {
			skipCollapse, holdsCode = preserveWhitespace(ctx.parent.Data), containsCode(ctx.parent.Data)
		}
		if skipCollapse {
			return m.writeAll([]byte(contents))
		}
		if holdsCode {
			return m.writeAll([]byte(strings.TrimFunc(contents, isASCIIWhitespaceRune)))
		}
		// Early exit if empty to forego expensive trim logic.
		if contents == "" {
			return nil
		}
		trimLeft, trimRight := true, true
		if ctx != nil {
			trimLeft, trimRight = ctx.trim(m.precedingWhitespace)
		}
		switch {
		case trimLeft && trimRight:
			contents = strings.TrimFunc(contents, isASCIIWhitespaceRune)
		case trimLeft:
			contents = strings.TrimLeftFunc(contents, isASCIIWhitespaceRune)
		case trimRight:
			contents = strings.TrimRightFunc(contents, isASCIIWhitespaceRune)
		}
		// Second empty check after trimming whitespace.
		if contents != "" {
			// Replace \n, \r with ' '.
			b := []byte(contents)
			for i, c := range b {
				if c == '\n' || c == '\r' {
					b[i] = ' '
				}
			}
			if err := m.writeCollapseWhitespace(b, reservedEntity, nil); err != nil {
				return err
			}
			m.precedingWhitespace = !trimRight && isASCIIWhitespaceByte(b[len(b)-1])
		}
		return nil

	case xhtml.CommentNode:
		if !m.preserveComments {
			return nil
		}
		if err := m.writeAll([]byte("<!--")); err != nil {
			return err
		}
		if err := m.writeAll([]byte(node.Data)); err != nil {
			return err
		}
		return m.writeAll([]byte("-->"))

	case xhtml.DocumentNode:
		return m.minifyChildren(ctx, node)

	case xhtml.ElementNode:
		tag := node.Data
		if isSelfClosing(tag) {
			return m.writeStartTag(node)
		}
		omitStart, omitEnd := m.omitTags(ctx, node, tag, len(node.Attr) == 0)
		if !omitStart {
			if err := m.writeStartTag(node); err != nil {
				return err
			}
		}
		if err := m.minifyChildren(ctx, node); err != nil {
			return err
		}
		if !omitEnd {
			return m.writeEndTag(node)
		}
		return nil
	}
	return nil
}

// nextIsComment is U:text/format/html5minify/mod.rs:287-305.
func (m *html5Minifier) nextIsComment(nodes []*xhtml.Node) bool {
	for _, n := range nodes {
		switch n.Type {
		case xhtml.TextNode:
			if m.collapseWhitespace && isWhitespaceText(n.Data) {
				// Blocks of whitespace are skipped.
				continue
			}
			return false
		case xhtml.CommentNode:
			return m.preserveComments
		default:
			return false
		}
	}
	return false
}

// isWhitespace is Minifier::is_whitespace: skip (false ok) a whitespace
// block, else whether the text starts with a non-space
// (U:text/format/html5minify/mod.rs:307-319).
func (m *html5Minifier) isWhitespace(s string) (bool, bool) {
	if m.collapseWhitespace && isWhitespaceText(s) {
		return false, false
	}
	return !(s != "" && isASCIIWhitespaceByte(s[0])), true
}

// omitTags decides whether start and end tags can be omitted
// (U:text/format/html5minify/mod.rs:321-450).
func (m *html5Minifier) omitTags(ctx *minifyContext, node *xhtml.Node, name string, emptyAttributes bool) (bool, bool) {
	if ctx == nil {
		return false, false
	}
	commentResult := func() (bool, bool) {
		if m.preserveComments {
			return false, true
		}
		return false, false
	}
	children := minifyChildren(node)
	switch name {
	case "html":
		// The end tag may be omitted if <html> is not immediately followed
		// by a comment; the start tag if the first thing inside it is not
		// a comment.
		omitEnd := !ctx.hasRight || !m.nextIsComment(ctx.right)
		omitStart := emptyAttributes && omitEnd && !m.nextIsComment(children)
		return omitStart, omitEnd
	case "head":
		// The end tag may be omitted if the first thing following <head>
		// is not a space character or a comment.
		omitEnd := true
		if ctx.hasRight {
			omitEnd = findMapOr(ctx.right, true, func(n *xhtml.Node) (bool, bool) {
				switch n.Type {
				case xhtml.TextNode:
					return m.isWhitespace(n.Data)
				case xhtml.CommentNode:
					return commentResult()
				}
				return true, true
			})
		}
		// The start tag may be omitted if the first thing inside <head> is
		// an element.
		omitStart := emptyAttributes && omitEnd && findMapOr(children, true, func(n *xhtml.Node) (bool, bool) {
			switch n.Type {
			case xhtml.TextNode:
				return m.isWhitespace(n.Data)
			case xhtml.ElementNode:
				return true, true
			case xhtml.CommentNode:
				return commentResult()
			}
			return false, true
		})
		return omitStart, omitEnd
	case "body":
		// The start tag may be omitted if the first thing inside it is not
		// a space character, comment, <script> element or <style> element.
		omitStart := emptyAttributes && findMapOr(children, true, func(n *xhtml.Node) (bool, bool) {
			switch n.Type {
			case xhtml.TextNode:
				return m.isWhitespace(n.Data)
			case xhtml.ElementNode:
				return n.Data != "script" && n.Data != "style", true
			case xhtml.CommentNode:
				return commentResult()
			}
			return true, true
		})
		// The end tag may be omitted if <body> is not immediately followed
		// by a comment.
		omitEnd := !ctx.hasRight || !m.nextIsComment(ctx.right)
		return omitStart && omitEnd, omitEnd
	case "p":
		omitEnd := true
		if next := ctx.nextElement(); next != nil {
			switch strings.ToLower(next.Data) {
			case "address", "article", "aside", "blockquote", "div", "dl", "fieldset", "footer",
				"form", "h1", "h2", "h3", "h4", "h5", "h6", "header", "hr", "menu", "nav", "ol",
				"p", "pre", "section", "table", "ul":
				omitEnd = true
			default:
				omitEnd = false
			}
		}
		return false, omitEnd
	}
	// TODO (upstream): comprehensive handling of optional end element rules.
	return false, optionalEndTag(name)
}

// findMapOr is Iterator::find_map(..).unwrap_or(fallback): f returns
// (value, found).
func findMapOr(nodes []*xhtml.Node, fallback bool, f func(*xhtml.Node) (bool, bool)) bool {
	for _, n := range nodes {
		if v, ok := f(n); ok {
			return v
		}
	}
	return fallback
}

// minifyChildren is U:text/format/html5minify/mod.rs:452-476.
func (m *html5Minifier) minifyChildren(ctx *minifyContext, node *xhtml.Node) error {
	children := minifyChildren(node)
	l := len(children)
	for i, child := range children {
		if m.precedingWhitespace && minifyIsBlockElement(child) {
			m.precedingWhitespace = false
		}
		c := &minifyContext{parent: node, parentContext: ctx}
		if i > 0 {
			c.left, c.hasLeft = children[:i], true
		}
		if i+1 < l {
			c.right, c.hasRight = children[i+1:], true
		}
		if err := m.minifyNode(c, child); err != nil {
			return err
		}
	}
	return nil
}

// writeQualifiedName is U:text/format/html5minify/mod.rs:478-487.
func (m *html5Minifier) writeQualifiedName(prefix, local string) error {
	if prefix != "" {
		if err := m.writeAll([]byte(strings.ToLower(prefix) + ":")); err != nil {
			return err
		}
	}
	return m.writeAll([]byte(htmlASCIILower(local)))
}

// htmlASCIILower is str::to_ascii_lowercase.
func htmlASCIILower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

// writeStartTag is U:text/format/html5minify/mod.rs:489-498.
func (m *html5Minifier) writeStartTag(node *xhtml.Node) error {
	if err := m.writeAll([]byte("<")); err != nil {
		return err
	}
	if err := m.writeQualifiedName("", node.Data); err != nil {
		return err
	}
	for _, attr := range node.Attr {
		if err := m.writeAttribute(attr); err != nil {
			return err
		}
	}
	return m.writeAll([]byte(">"))
}

// writeEndTag is U:text/format/html5minify/mod.rs:500-504.
func (m *html5Minifier) writeEndTag(node *xhtml.Node) error {
	if err := m.writeAll([]byte("</")); err != nil {
		return err
	}
	if err := m.writeQualifiedName("", node.Data); err != nil {
		return err
	}
	return m.writeAll([]byte(">"))
}

// writeAttribute is U:text/format/html5minify/mod.rs:506-541.
func (m *html5Minifier) writeAttribute(attr xhtml.Attribute) error {
	if err := m.writeAll([]byte(" ")); err != nil {
		return err
	}
	if err := m.writeQualifiedName(attr.Namespace, attr.Key); err != nil {
		return err
	}
	value := attr.Val
	if m.collapseWhitespace {
		value = strings.TrimFunc(value, isASCIIWhitespaceRune)
	}
	if value == "" {
		return nil
	}
	if err := m.writeAll([]byte("=")); err != nil {
		return err
	}
	b := []byte(value)
	unquoted, double, single := true, false, false
	for _, c := range b {
		double = double || c == '"'
		single = single || c == '\''
		unquoted = unquoted && !double && !single && c != '=' && !isASCIIWhitespaceByte(c)
	}
	switch {
	case unquoted:
		return m.writeAll(b)
	case double:
		return m.writeAttributeValue(b, []byte("'"), reservedEntityWithApos)
	}
	return m.writeAttributeValue(b, []byte(`"`), reservedEntity)
}

// writeAttributeValue is U:text/format/html5minify/mod.rs:543-560.
func (m *html5Minifier) writeAttributeValue(v, quote []byte, f entityFn) error {
	if err := m.writeAll(quote); err != nil {
		return err
	}
	if m.collapseWhitespace {
		no := false
		if err := m.writeCollapseWhitespace(v, f, &no); err != nil {
			return err
		}
	} else if err := m.writeAll(v); err != nil {
		return err
	}
	return m.writeAll(quote)
}

// writeCollapseWhitespace writes b, collapsing whitespace runs
// (U:text/format/html5minify/mod.rs:562-596).
func (m *html5Minifier) writeCollapseWhitespace(b []byte, f entityFn, precedingWhitespace *bool) error {
	preceding := m.precedingWhitespace
	if precedingWhitespace != nil {
		preceding = *precedingWhitespace
	}
	pos := 0
	for i, c := range b {
		ws := isASCIIWhitespaceByte(c)
		if ws && preceding {
			if i != pos {
				if err := m.write(b[pos:i], f); err != nil {
					return err
				}
			}
			// ASCII whitespace = 1 byte.
			pos = i + 1
			preceding = true
		} else {
			preceding = ws
		}
	}
	if pos < len(b) {
		return m.write(b[pos:], f)
	}
	return nil
}

// write writes b with reserved characters replaced by entities
// (U:text/format/html5minify/mod.rs:598-619).
func (m *html5Minifier) write(b []byte, f entityFn) error {
	pos := 0
	for i, c := range b {
		if entity := f(c); entity != nil {
			if err := m.writeAll(b[pos:i]); err != nil {
				return err
			}
			if err := m.writeAll(entity); err != nil {
				return err
			}
			// Reserved characters are 1 byte.
			pos = i + 1
		}
	}
	if pos < len(b) {
		return m.writeAll(b[pos:])
	}
	return nil
}

// entityFn is U:text/format/html5minify/mod.rs:622.
type entityFn func(byte) []byte

// reservedEntity is U:text/format/html5minify/mod.rs:624-631.
func reservedEntity(v byte) []byte {
	switch v {
	case '<':
		return []byte("&lt;")
	case '>':
		return []byte("&gt;")
	case '&':
		return []byte("&#38;")
	}
	return nil
}

// reservedEntityWithApos is U:text/format/html5minify/mod.rs:633-639.
func reservedEntityWithApos(v byte) []byte {
	if v == '\'' {
		return []byte("&#39;")
	}
	return reservedEntity(v)
}

// isWhitespaceText is U:text/format/html5minify/mod.rs:641-643.
func isWhitespaceText(s string) bool {
	for i := 0; i < len(s); i++ {
		if !isASCIIWhitespaceByte(s[i]) {
			return false
		}
	}
	return true
}

// isBlockElementName is U:text/format/html5minify/mod.rs:645-695.
func isBlockElementName(name string) bool {
	switch name {
	case "address", "article", "aside", "blockquote", "body", "br", "details", "dialog", "dd",
		"div", "dl", "dt", "fieldset", "figcaption", "figure", "footer", "form", "h1", "h2",
		"h3", "h4", "h5", "h6", "head", "header", "hgroup", "hr", "html", "li", "link", "main",
		"meta", "nav", "ol", "option", "p", "pre", "script", "section", "source", "table",
		"td", "th", "title", "tr", "ul":
		return true
	}
	return false
}

// minifyIsBlockElement is is_block_element (U:text/format/html5minify/mod.rs:697-703).
func minifyIsBlockElement(n *xhtml.Node) bool {
	switch n.Type {
	case xhtml.ElementNode:
		return isBlockElementName(n.Data)
	case xhtml.DocumentNode:
		return true
	}
	return false
}

// isASCIIWhitespaceByte is u8::is_ascii_whitespace: space, \t, \n, \x0C, \r.
func isASCIIWhitespaceByte(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\x0C' || c == '\r'
}

// isASCIIWhitespaceRune is is_ascii_whitespace (U:text/format/html5minify/mod.rs:705-708).
func isASCIIWhitespaceRune(c rune) bool { return c < 0x80 && isASCIIWhitespaceByte(byte(c)) }

// preserveWhitespace is U:text/format/html5minify/mod.rs:710-712.
func preserveWhitespace(name string) bool { return name == "pre" || name == "textarea" }

// containsCode is U:text/format/html5minify/mod.rs:714-716.
func containsCode(name string) bool { return name == "script" || name == "style" }

// isSelfClosing is U:text/format/html5minify/mod.rs:718-739.
func isSelfClosing(name string) bool {
	switch name {
	case "area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "param",
		"source", "track", "wbr", "command", "keygen", "menuitem":
		return true
	}
	return false
}

// optionalEndTag is U:text/format/html5minify/mod.rs:741-760.
func optionalEndTag(name string) bool {
	switch name {
	case "basefont", "colgroup", "dd", "dt", "frame", "isindex", "li", "option", "p", "tbody",
		"td", "tfoot", "th", "thead", "tr":
		return true
	}
	return false
}

// minifyHTML runs the minifier over source into a buffer.
func minifyHTML(source string, configure func(*html5Minifier)) ([]byte, error) {
	var w bytes.Buffer
	m := newHTML5Minifier(&w)
	if configure != nil {
		configure(m)
	}
	if err := m.minify(strings.NewReader(source)); err != nil {
		return nil, err
	}
	return w.Bytes(), nil
}
