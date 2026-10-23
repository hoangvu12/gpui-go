package base

// The mdast surface of the Markdown parser: Base parses with markdown-rs
// 1.0.0 `to_mdast` and hands plugins `&mdast::Node`
// (U:text/markdown_ext.rs:20-21,28-29,57; U:text/format/markdown.rs:20-29).
// The Go port parses with github.com/yuin/goldmark v1.8.6 and adapts its
// tree to mdast with markdown-rs's start AND end byte offsets (ticket 01's
// dependency policy; .scratch/gpui-base/research/deps-01-markdown.md, whose
// adapter prototype this file productionizes). goldmark stores no end
// offsets on containers and drops closing syntax, so each reconstruction
// below derives the end from children or rescans the source; the recorded
// markdown-rs output in .scratch/gpui-base/research/oracles/markdown is the
// golden test (text_mdast_test.go).
//
// Math (`$`/`$$`, markdown-rs math_text/math_flow) and frontmatter
// (`---`/`+++`) are hand-written goldmark extensions (ticket 01). MDX is
// deferred: goldmark has no MDX constructs, so a parse with MDX enabled
// fails with errMdastMDXUnsupported instead of silently parsing raw HTML.
//
// Naming (doc.go): mdast types take the Mdast prefix (the Rust module
// name), since Node, Position and Point are too generic for the crate root.

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	gast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// MdastKind is the variant of an mdast::Node (markdown-rs 1.0.0 mdast.rs).
// The MDX kinds exist for parity with Base's match arms but are never
// produced: MDX parsing is deferred.
type MdastKind int

const (
	MdastRoot MdastKind = iota
	MdastBlockquote
	MdastFootnoteDefinition
	MdastMdxJsxFlowElement
	MdastList
	MdastToml
	MdastYaml
	MdastBreak
	MdastInlineCode
	MdastInlineMath
	MdastDelete
	MdastEmphasis
	MdastMdxTextExpression
	MdastFootnoteReference
	MdastHTML
	MdastImage
	MdastImageReference
	MdastMdxJsxTextElement
	MdastLink
	MdastLinkReference
	MdastStrong
	MdastText
	MdastCode
	MdastMath
	MdastMdxFlowExpression
	MdastHeading
	MdastTable
	MdastThematicBreak
	MdastTableRow
	MdastTableCell
	MdastListItem
	MdastDefinition
	MdastParagraph
)

var mdastKindNames = [...]string{
	"Root", "Blockquote", "FootnoteDefinition", "MdxJsxFlowElement", "List",
	"Toml", "Yaml", "Break", "InlineCode", "InlineMath", "Delete", "Emphasis",
	"MdxTextExpression", "FootnoteReference", "Html", "Image", "ImageReference",
	"MdxJsxTextElement", "Link", "LinkReference", "Strong", "Text", "Code",
	"Math", "MdxFlowExpression", "Heading", "Table", "ThematicBreak",
	"TableRow", "TableCell", "ListItem", "Definition", "Paragraph",
}

// String is the mdast variant name, as markdown-rs's Debug prints it.
func (k MdastKind) String() string {
	if int(k) >= 0 && int(k) < len(mdastKindNames) {
		return mdastKindNames[k]
	}
	return "MdastKind(" + strconv.Itoa(int(k)) + ")"
}

// MdastAlignKind is mdast::AlignKind, a table column's alignment.
type MdastAlignKind int

const (
	MdastAlignNone MdastAlignKind = iota
	MdastAlignLeft
	MdastAlignRight
	MdastAlignCenter
)

// MdastReferenceKind is mdast::ReferenceKind: how a link or image
// reference names its definition.
type MdastReferenceKind int

const (
	MdastReferenceShortcut MdastReferenceKind = iota
	MdastReferenceCollapsed
	MdastReferenceFull
)

// MdastPoint is unist::Point: 1-based line and column, 0-based byte
// offset. Columns count bytes from the line start.
type MdastPoint struct {
	Line   int
	Column int
	Offset int
}

// MdastPosition is unist::Position, the node's source range.
type MdastPosition struct {
	Start MdastPoint
	End   MdastPoint
}

// MdastNode is mdast::Node. Rust's enum becomes one struct: Kind selects
// the variant and the fields a variant does not carry stay zero.
type MdastNode struct {
	Kind MdastKind
	// Position is the node's source range; nil only for synthesized nodes.
	Position *MdastPosition
	// Children of a parent node; leaves have none.
	Children []*MdastNode

	// Value of Text, InlineCode, InlineMath, Math, Code, Html, Yaml and Toml.
	Value string
	// Depth of a Heading (1-6).
	Depth uint8
	// Ordered, Start and Spread of a List; Spread and Checked of a ListItem.
	Ordered bool
	Start   *uint32
	Spread  bool
	Checked *bool
	// URL and Title of a Link, Image or Definition; Alt of an Image.
	URL   string
	Title *string
	Alt   string
	// Lang and Meta of a Code block; Meta of a Math block.
	Lang *string
	Meta *string
	// Identifier (normalized) and Label (as written) of references,
	// footnotes and definitions.
	Identifier    string
	Label         *string
	ReferenceKind MdastReferenceKind
	// Align holds a Table's column alignments.
	Align []MdastAlignKind
}

// mdastParseOptions are the markdown-rs ParseOptions constructs Base
// toggles (U:text/markdown_ext.rs:392-409): GFM is always on.
type mdastParseOptions struct {
	frontmatter bool
	mathText    bool
	mathFlow    bool
	mdx         bool
}

var errMdastMDXUnsupported = errors.New("markdown: MDX constructs are not supported (goldmark has no MDX; deferred by ticket 01)")

var mdastParsers sync.Map // mdastParseOptions -> goldmark.Markdown

func mdastParserFor(opts mdastParseOptions) goldmark.Markdown {
	key := opts
	key.frontmatter = false // handled before goldmark runs
	if md, ok := mdastParsers.Load(key); ok {
		return md.(goldmark.Markdown)
	}
	var popts []parser.Option
	if opts.mathText {
		popts = append(popts, parser.WithInlineParsers(util.Prioritized(mathTextParser{}, 501)))
	}
	if opts.mathFlow {
		popts = append(popts, parser.WithBlockParsers(util.Prioritized(mathFlowParser{}, 701)))
	}
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM, extension.Footnote),
		goldmark.WithParserOptions(popts...),
	)
	actual, _ := mdastParsers.LoadOrStore(key, md)
	return actual.(goldmark.Markdown)
}

// mdastParse is markdown::to_mdast(source, options): the Root node of
// source, with every node positioned in source's bytes.
func mdastParse(source string, opts mdastParseOptions) (*MdastNode, error) {
	if opts.mdx {
		return nil, errMdastMDXUnsupported
	}
	src := []byte(source)
	var front *MdastNode
	if opts.frontmatter {
		if fm, end := mdastFrontmatter(src); fm != nil {
			front = fm
			// goldmark parses the rest; blank the frontmatter so it reads as
			// blank lines and every offset stays a document offset.
			for i := range end {
				if src[i] != '\n' && src[i] != '\r' {
					src[i] = ' '
				}
			}
		}
	}
	pc := parser.NewContext()
	doc := mdastParserFor(opts).Parser().Parse(text.NewReader(src), parser.WithContext(pc))
	a := &mdastAdapter{src: src}
	root := a.root(doc)
	if front != nil {
		root.Children = append([]*MdastNode{front}, root.Children...)
	}
	mdastPositionTree(root, []byte(source))
	return root, nil
}

// mdastAdapter converts a goldmark tree over src to mdast. Nodes get their
// byte offsets here; mdastPositionTree adds lines and columns afterwards.
type mdastAdapter struct {
	src []byte
}

func (a *mdastAdapter) node(kind MdastKind, start, end int) *MdastNode {
	n := &MdastNode{Kind: kind}
	a.setSpan(n, start, end)
	return n
}

func (a *mdastAdapter) setSpan(n *MdastNode, start, end int) {
	n.Position = &MdastPosition{Start: MdastPoint{Offset: start}, End: MdastPoint{Offset: end}}
}

func mdastStart(n *MdastNode) int { return n.Position.Start.Offset }
func mdastEnd(n *MdastNode) int   { return n.Position.End.Offset }

// mdastPositionTree fills line and column from each offset.
func mdastPositionTree(root *MdastNode, src []byte) {
	lineStarts := []int{0}
	for i := 0; i < len(src); i++ {
		switch src[i] {
		case '\n':
			lineStarts = append(lineStarts, i+1)
		case '\r':
			if i+1 < len(src) && src[i+1] == '\n' {
				i++
			}
			lineStarts = append(lineStarts, i+1)
		}
	}
	point := func(p *MdastPoint) {
		line := sort.Search(len(lineStarts), func(i int) bool { return lineStarts[i] > p.Offset }) - 1
		line = max(line, 0)
		p.Line = line + 1
		p.Column = p.Offset - lineStarts[line] + 1
	}
	var walk func(n *MdastNode)
	walk = func(n *MdastNode) {
		if n.Position != nil {
			point(&n.Position.Start)
			point(&n.Position.End)
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
}

// ---- frontmatter (markdown-rs `frontmatter` construct) ----

// mdastFrontmatter recognizes a `---` (YAML) or `+++` (TOML) fence on the
// first line closed by the same fence on a later line. It returns the node
// and the offset just past the closing fence.
func mdastFrontmatter(src []byte) (*MdastNode, int) {
	if len(src) < 3 {
		return nil, 0
	}
	var kind MdastKind
	switch string(src[:3]) {
	case "---":
		kind = MdastYaml
	case "+++":
		kind = MdastToml
	default:
		return nil, 0
	}
	fence := src[:3]
	isFence := func(line []byte) bool {
		return bytes.Equal(bytes.TrimRight(line, " \t"), fence)
	}
	if !isFence(src[:mdastLineEnd(src, 0)]) {
		return nil, 0
	}
	bodyStart := mdastNextLineStart(src, 0)
	if bodyStart == mdastLineEnd(src, 0) {
		return nil, 0 // no line ending after the opening fence
	}
	for p := bodyStart; p < len(src); p = mdastNextLineStart(src, p) {
		le := mdastLineEnd(src, p)
		if isFence(src[p:le]) {
			body := strings.TrimRight(string(src[bodyStart:p]), "\r\n")
			n := &MdastNode{Kind: kind, Value: body}
			n.Position = &MdastPosition{Start: MdastPoint{Offset: 0}, End: MdastPoint{Offset: le}}
			return n, le
		}
		if le == len(src) {
			break
		}
	}
	return nil, 0
}

func mdastLineEnd(src []byte, p int) int {
	for p < len(src) && src[p] != '\n' && src[p] != '\r' {
		p++
	}
	return p
}

func mdastNextLineStart(src []byte, p int) int {
	p = mdastLineEnd(src, p)
	if p < len(src) && src[p] == '\r' {
		p++
	}
	if p < len(src) && src[p] == '\n' {
		p++
	}
	return p
}

// ---- math (markdown-rs `math_text` and `math_flow` constructs) ----

var (
	kindMathText = gast.NewNodeKind("MdastInlineMath")
	kindMathFlow = gast.NewNodeKind("MdastMath")
)

// mathTextNode is an inline math span: `$x$`, `$$x$$`. It records its own
// source range and value, which goldmark would otherwise drop.
type mathTextNode struct {
	gast.BaseInline
	start, end int
	value      string
}

func (n *mathTextNode) Kind() gast.NodeKind { return kindMathText }
func (n *mathTextNode) Dump(source []byte, level int) {
	gast.DumpHelper(n, source, level, map[string]string{"Value": n.value}, nil)
}

// mathTextParser parses math (text) like a code span with `$` as the
// marker: an opening run closes at the next run of exactly the same length
// (single dollars allowed, markdown-rs's default), escapes do not apply
// inside, and one padding space is stripped from each side when both are
// present. Line endings inside stay in the value. Structured after
// goldmark's codeSpanParser (parser/code_span.go, MIT).
type mathTextParser struct{}

func (mathTextParser) Trigger() []byte { return []byte{'$'} }

func (mathTextParser) Parse(parent gast.Node, block text.Reader, pc parser.Context) gast.Node {
	line, startSegment := block.PeekLine()
	opener := 0
	for opener < len(line) && line[opener] == '$' {
		opener++
	}
	start := startSegment.Start
	block.Advance(opener)
	l, pos := block.Position()
	var value bytes.Buffer
	for {
		line, segment := block.PeekLine()
		if line == nil {
			block.SetPosition(l, pos)
			return gast.NewTextSegment(startSegment.WithStop(startSegment.Start + opener))
		}
		for i := 0; i < len(line); i++ {
			if line[i] != '$' {
				continue
			}
			from := i
			for i < len(line) && line[i] == '$' {
				i++
			}
			if i-from == opener {
				value.Write(line[:from])
				block.Advance(i)
				node := &mathTextNode{start: start, end: segment.Start + i, value: mathPadding(value.String())}
				return node
			}
			i--
		}
		value.Write(line)
		block.AdvanceLine()
	}
}

// mathPadding strips one space or line ending from both ends when both are
// present and the content is not all spaces (code (text) padding, which
// math (text) shares).
func mathPadding(v string) string {
	if len(v) < 2 || strings.TrimSpace(v) == "" {
		return v
	}
	pad := func(c byte) bool { return c == ' ' || c == '\n' || c == '\r' }
	if !pad(v[0]) || !pad(v[len(v)-1]) {
		return v
	}
	head := 1
	if strings.HasPrefix(v, "\r\n") {
		head = 2
	}
	tail := 1
	if strings.HasSuffix(v, "\r\n") {
		tail = 2
	}
	if head+tail > len(v) {
		return v
	}
	return v[head : len(v)-tail]
}

// mathFlowNode is a `$$` math block; start/end are its source range,
// including both fences when closed.
type mathFlowNode struct {
	gast.BaseBlock
	start, end int
	meta       []byte
	hasMeta    bool
}

func (n *mathFlowNode) Kind() gast.NodeKind { return kindMathFlow }
func (n *mathFlowNode) IsRaw() bool         { return true }
func (n *mathFlowNode) Dump(source []byte, level int) {
	gast.DumpHelper(n, source, level, nil, nil)
}

type mathFenceData struct {
	indent int
	length int
	node   *mathFlowNode
}

var mathFlowInfoKey = parser.NewContextKey()

// mathFlowParser parses math (flow) like a fenced code block with `$` as
// the marker and a minimum fence of two: the closing fence is at least as
// long as the opening one, the content is dedented by the opening fence's
// indent, and an unclosed block runs to the end of its container.
// Structured after goldmark's fencedCodeBlockParser
// (parser/fcode_block.go, MIT).
type mathFlowParser struct{}

func (mathFlowParser) Trigger() []byte { return []byte{'$'} }

func (mathFlowParser) Open(parent gast.Node, reader text.Reader, pc parser.Context) (gast.Node, parser.State) {
	line, segment := reader.PeekLine()
	pos := pc.BlockOffset()
	if pos < 0 || line[pos] != '$' {
		return nil, parser.NoChildren
	}
	i := pos
	for i < len(line) && line[i] == '$' {
		i++
	}
	length := i - pos
	if length < 2 {
		return nil, parser.NoChildren
	}
	rest := util.TrimRightSpace(util.TrimLeftSpace(line[i:]))
	if bytes.IndexByte(rest, '$') >= 0 {
		return nil, parser.NoChildren
	}
	base := segment.Start - segment.Padding
	node := &mathFlowNode{start: base + pos}
	node.end = base + i + len(util.TrimRightSpace(line[i:]))
	if len(rest) > 0 {
		node.meta, node.hasMeta = rest, true
	}
	pc.Set(mathFlowInfoKey, &mathFenceData{indent: pos, length: length, node: node})
	return node, parser.NoChildren
}

func (mathFlowParser) Continue(node gast.Node, reader text.Reader, pc parser.Context) parser.State {
	line, segment := reader.PeekLine()
	fdata := pc.Get(mathFlowInfoKey).(*mathFenceData)
	w, pos := util.IndentWidth(line, reader.LineOffset())
	if w < 4 {
		i := pos
		for i < len(line) && line[i] == '$' {
			i++
		}
		if i-pos >= fdata.length && util.IsBlank(line[i:]) {
			base := segment.Start - segment.Padding
			fdata.node.end = base + i
			reader.AdvanceToEOL()
			return parser.Close
		}
	}
	pos, padding := util.IndentPositionPadding(line, reader.LineOffset(), segment.Padding, fdata.indent)
	if pos < 0 {
		pos = max(0, util.FirstNonSpacePosition(line)) - segment.Padding
		padding = 0
	}
	seg := text.NewSegmentPadding(segment.Start+pos, segment.Stop, padding)
	seg.ForceNewline = true
	node.Lines().Append(seg)
	if trimmed := len(util.TrimRightSpace(line)); trimmed > 0 {
		fdata.node.end = segment.Start - segment.Padding + trimmed
	}
	reader.AdvanceToEOL()
	return parser.Continue | parser.NoChildren
}

func (mathFlowParser) Close(node gast.Node, reader text.Reader, pc parser.Context) {
	if fdata, ok := pc.Get(mathFlowInfoKey).(*mathFenceData); ok && fdata.node == node {
		pc.Set(mathFlowInfoKey, nil)
	}
}

func (mathFlowParser) CanInterruptParagraph() bool { return true }
func (mathFlowParser) CanAcceptIndentedLine() bool { return false }

// ---- goldmark -> mdast ----

func mdastIsWS(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }

func (a *mdastAdapter) trimEnd(e int) int {
	for e > 0 && mdastIsWS(a.src[e-1]) {
		e--
	}
	return e
}

func (a *mdastAdapter) lineEnd(p int) int       { return mdastLineEnd(a.src, p) }
func (a *mdastAdapter) nextLineStart(p int) int { return mdastNextLineStart(a.src, p) }

// mdastNormalizeIdentifier collapses whitespace and lowercases, the
// identifier markdown-rs stores for labels.
func mdastNormalizeIdentifier(b []byte) string {
	return strings.ToLower(strings.Join(strings.Fields(string(b)), " "))
}

// mdastDecode resolves backslash escapes and character references in one
// pass, as markdown-rs decodes text values.
func mdastDecode(seg []byte) string {
	var b strings.Builder
	for i := 0; i < len(seg); {
		c := seg[i]
		if c == '\\' && i+1 < len(seg) && util.IsPunct(seg[i+1]) {
			b.WriteByte(seg[i+1])
			i += 2
			continue
		}
		if c == '&' {
			if decoded, n := mdastCharacterReference(seg[i:]); n > 0 {
				b.WriteString(decoded)
				i += n
				continue
			}
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}

// mdastCharacterReference decodes a CommonMark character reference at the
// start of s, returning the text and the bytes consumed (0 when none).
func mdastCharacterReference(s []byte) (string, int) {
	if len(s) < 3 || s[0] != '&' {
		return "", 0
	}
	if s[1] == '#' {
		i, hex := 2, false
		if i < len(s) && (s[i] == 'x' || s[i] == 'X') {
			hex = true
			i++
		}
		start := i
		limit := 7
		if hex {
			limit = 6
		}
		for i < len(s) && i-start < limit && ((hex && util.IsHexDecimal(s[i])) || (!hex && util.IsNumeric(s[i]))) {
			i++
		}
		if i == start || i >= len(s) || s[i] != ';' {
			return "", 0
		}
		base := 10
		if hex {
			base = 16
		}
		v, err := strconv.ParseUint(string(s[start:i]), base, 32)
		r := rune(v)
		if err != nil || v == 0 || !utf8.ValidRune(r) {
			r = utf8.RuneError
		}
		return string(r), i + 1
	}
	i := 1
	for i < len(s) && util.IsAlphaNumeric(s[i]) {
		i++
	}
	if i == 1 || i >= len(s) || s[i] != ';' {
		return "", 0
	}
	entity, ok := util.LookUpHTML5EntityByName(string(s[1:i]))
	if !ok {
		return "", 0
	}
	return string(entity.Characters), i + 1
}

func (a *mdastAdapter) root(doc gast.Node) *MdastNode {
	root := a.node(MdastRoot, 0, len(a.src))
	var footnotes []*MdastNode
	for ch := doc.FirstChild(); ch != nil; ch = ch.NextSibling() {
		if fl, ok := ch.(*east.FootnoteList); ok {
			for f := fl.FirstChild(); f != nil; f = f.NextSibling() {
				if m := a.block(f); m != nil {
					footnotes = append(footnotes, m)
				}
			}
			continue
		}
		if m := a.block(ch); m != nil {
			root.Children = append(root.Children, m)
		}
	}
	if len(footnotes) > 0 {
		// goldmark moves definitions into a trailing FootnoteList; mdast
		// keeps them where they were written.
		root.Children = append(root.Children, footnotes...)
		slices.SortStableFunc(root.Children, func(x, y *MdastNode) int { return mdastStart(x) - mdastStart(y) })
	}
	return root
}

func (a *mdastAdapter) linesValue(n gast.Node) string {
	var b strings.Builder
	l := n.Lines()
	for i := 0; i < l.Len(); i++ {
		s := l.At(i)
		b.WriteString(strings.Repeat(" ", s.Padding))
		b.Write(a.src[s.Start:s.Stop])
	}
	return strings.TrimRight(b.String(), "\r\n")
}

// closingFence finds a closing fence on the line at `from`, skipping
// container prefixes, and returns its trimmed end or -1.
func (a *mdastAdapter) closingFence(from int, ch byte, min int) int {
	if from >= len(a.src) {
		return -1
	}
	p := from
	le := a.lineEnd(p)
	for p < le && (a.src[p] == ' ' || a.src[p] == '>' || a.src[p] == '\t') {
		p++
	}
	n := 0
	for p+n < le && a.src[p+n] == ch {
		n++
	}
	if n < min || strings.TrimSpace(string(a.src[p+n:le])) != "" {
		return -1
	}
	return a.trimEnd(le)
}

// blockStart: mdast block positions include up-to-3-space indentation
// after the container prefix (`> ` or the line start).
func (a *mdastAdapter) blockStart(p int) int {
	s := p
	for s > 0 && (a.src[s-1] == ' ' || a.src[s-1] == '\t') {
		s--
	}
	if s == 0 || a.src[s-1] == '\n' || a.src[s-1] == '\r' {
		return s
	}
	if a.src[s-1] == '>' {
		if s < p && a.src[s] == ' ' {
			return s + 1
		}
		return s
	}
	return p
}

func (a *mdastAdapter) blocks(n gast.Node) []*MdastNode {
	var out []*MdastNode
	for ch := n.FirstChild(); ch != nil; ch = ch.NextSibling() {
		if m := a.block(ch); m != nil {
			out = append(out, m)
		}
	}
	return out
}

func (a *mdastAdapter) containerEnd(children []*MdastNode, pos int) int {
	if len(children) > 0 {
		return mdastEnd(children[len(children)-1])
	}
	return a.trimEnd(a.lineEnd(pos))
}

// gapSpread reports a blank line between consecutive children: mdast's
// list spread is a gap between items, an item's spread a gap between its
// children (goldmark keeps only tightness).
func (a *mdastAdapter) gapSpread(c []*MdastNode) bool {
	for i := 1; i < len(c); i++ {
		if bytes.Count(a.src[mdastEnd(c[i-1]):mdastStart(c[i])], []byte{'\n'}) >= 2 {
			return true
		}
	}
	return false
}

var mdastAligns = map[east.Alignment]MdastAlignKind{
	east.AlignLeft: MdastAlignLeft, east.AlignRight: MdastAlignRight,
	east.AlignCenter: MdastAlignCenter, east.AlignNone: MdastAlignNone,
}

func (a *mdastAdapter) block(n gast.Node) *MdastNode {
	src := a.src
	switch v := n.(type) {
	case *gast.Paragraph, *gast.TextBlock:
		l := n.Lines()
		if l.Len() == 0 {
			return nil
		}
		m := a.node(MdastParagraph, l.At(0).Start, a.trimEnd(l.At(l.Len()-1).Stop))
		m.Children = a.inlines(n)
		if _, ok := n.FirstChild().(*east.TaskCheckBox); ok && len(m.Children) > 0 {
			// The checkbox is ListItem.checked; the paragraph starts at its text.
			a.setSpan(m, mdastStart(m.Children[0]), mdastEnd(m))
		}
		return m
	case *gast.Heading:
		var s, e int
		if src[v.Pos()] == '#' {
			s, e = a.blockStart(v.Pos()), a.lineEnd(v.Pos())
		} else {
			l := v.Lines()
			s = l.At(0).Start
			e = a.trimEnd(a.lineEnd(a.nextLineStart(l.At(l.Len() - 1).Start)))
		}
		m := a.node(MdastHeading, s, e)
		m.Depth = uint8(v.Level)
		m.Children = a.inlines(n)
		return m
	case *gast.ThematicBreak:
		return a.node(MdastThematicBreak, a.blockStart(v.Pos()), a.trimEnd(a.lineEnd(v.Pos())))
	case *gast.FencedCodeBlock:
		fch := src[v.Pos()]
		flen := 0
		for v.Pos()+flen < len(src) && src[v.Pos()+flen] == fch {
			flen++
		}
		m := &MdastNode{Kind: MdastCode, Value: a.linesValue(v)}
		if v.Info != nil {
			if lang := v.Language(src); len(lang) > 0 {
				s := mdastDecode(lang)
				m.Lang = &s
				info := v.Info.Segment.Value(src)
				if rest := bytes.TrimSpace(info[len(lang):]); len(rest) > 0 {
					meta := mdastDecode(rest)
					m.Meta = &meta
				}
			}
		}
		after := a.nextLineStart(v.Pos())
		lastEnd := a.trimEnd(a.lineEnd(v.Pos()))
		if l := v.Lines(); l.Len() > 0 {
			after = l.At(l.Len() - 1).Stop
			lastEnd = a.trimEnd(l.At(l.Len() - 1).Stop)
		}
		e := a.closingFence(after, fch, flen)
		if e < 0 {
			e = lastEnd
		}
		a.setSpan(m, a.blockStart(v.Pos()), e)
		return m
	case *mathFlowNode:
		m := a.node(MdastMath, v.start, v.end)
		m.Value = a.linesValue(v)
		if v.hasMeta {
			meta := string(v.meta)
			m.Meta = &meta
		}
		return m
	case *gast.CodeBlock:
		l := v.Lines()
		s := v.Pos()
		for s > 0 && (src[s-1] == ' ' || src[s-1] == '\t') {
			s--
		}
		m := a.node(MdastCode, s, a.trimEnd(l.At(l.Len()-1).Stop))
		m.Value = a.linesValue(v)
		return m
	case *gast.Blockquote:
		children := a.blocks(n)
		m := a.node(MdastBlockquote, v.Pos(), a.containerEnd(children, v.Pos()))
		m.Children = children
		return m
	case *gast.List:
		children := a.blocks(n)
		m := a.node(MdastList, v.Pos(), a.containerEnd(children, v.Pos()))
		m.Children = children
		m.Ordered = v.IsOrdered()
		if m.Ordered {
			start := uint32(v.Start)
			m.Start = &start
		}
		m.Spread = a.gapSpread(children)
		return m
	case *gast.ListItem:
		children := a.blocks(n)
		m := a.node(MdastListItem, v.Pos(), a.containerEnd(children, v.Pos()))
		m.Children = children
		if fc := n.FirstChild(); fc != nil {
			if cb, ok := fc.FirstChild().(*east.TaskCheckBox); ok {
				checked := cb.IsChecked
				m.Checked = &checked
			}
		}
		m.Spread = a.gapSpread(children)
		return m
	case *gast.HTMLBlock:
		l := v.Lines()
		if l.Len() == 0 {
			return nil
		}
		s := a.blockStart(l.At(0).Start)
		e := l.At(l.Len() - 1).Stop
		if v.HasClosure() {
			e = v.ClosureLine.Stop
		}
		e = a.trimEnd(e)
		m := a.node(MdastHTML, s, e)
		m.Value = string(src[s:e])
		return m
	case *gast.LinkReferenceDefinition:
		l := v.Lines()
		m := a.node(MdastDefinition, v.Pos(), a.trimEnd(l.At(l.Len()-1).Stop))
		m.Identifier = mdastNormalizeIdentifier(v.Label)
		label := string(v.Label)
		m.Label = &label
		m.URL = mdastDecode(v.Destination)
		if v.Title != nil {
			title := mdastDecode(v.Title)
			m.Title = &title
		}
		return m
	case *east.Table:
		m := &MdastNode{Kind: MdastTable}
		for _, x := range v.Alignments {
			m.Align = append(m.Align, mdastAligns[x])
		}
		for r := n.FirstChild(); r != nil; r = r.NextSibling() {
			m.Children = append(m.Children, a.tableRow(r))
		}
		e := v.Pos()
		if len(m.Children) > 0 {
			e = mdastEnd(m.Children[len(m.Children)-1])
		}
		a.setSpan(m, v.Pos(), e)
		return m
	case *east.Footnote:
		children := a.blocks(n)
		e := a.containerEnd(children, v.Pos())
		// markdown-rs quirk: a definition followed by more input keeps its
		// line ending.
		if e < len(src)-1 && (src[e] == '\n' || src[e] == '\r') {
			e = a.nextLineStart(e)
		}
		m := a.node(MdastFootnoteDefinition, v.Pos(), e)
		m.Children = children
		m.Identifier = mdastNormalizeIdentifier(v.Ref)
		label := string(v.Ref)
		m.Label = &label
		return m
	}
	return nil
}

func (a *mdastAdapter) tableRow(r gast.Node) *MdastNode {
	src := a.src
	row := a.node(MdastTableRow, r.Pos(), a.trimEnd(a.lineEnd(r.Pos())))
	prev := mdastStart(row)
	rowEnd := mdastEnd(row)
	for c := r.FirstChild(); c != nil; c = c.NextSibling() {
		if c.Pos() < 0 {
			continue // goldmark pads short rows; mdast does not
		}
		children := a.inlines(c)
		var e int
		if c.NextSibling() == nil || c.NextSibling().Pos() < 0 {
			e = rowEnd
		} else {
			p := c.Pos()
			if len(children) > 0 {
				p = mdastEnd(children[len(children)-1])
			}
			for p < rowEnd && src[p] != '|' {
				p++
			}
			e = p
		}
		cell := a.node(MdastTableCell, prev, e)
		cell.Children = children
		prev = e
		row.Children = append(row.Children, cell)
	}
	return row
}

// ---- inlines ----

// inlines converts parent's inline children, merging the adjacent Text
// nodes goldmark splits at escapes, soft breaks and unresolved brackets
// into mdast's single Text.
func (a *mdastAdapter) inlines(parent gast.Node) []*MdastNode {
	src := a.src
	var out []*MdastNode
	var (
		on    bool
		s, e  int
		value strings.Builder
	)
	flush := func() {
		if on {
			t := a.node(MdastText, s, e)
			t.Value = value.String()
			out = append(out, t)
		}
		on = false
		s, e = 0, 0
		value.Reset()
	}
	for ch := parent.FirstChild(); ch != nil; ch = ch.NextSibling() {
		switch v := ch.(type) {
		case *gast.Text:
			seg := v.Segment
			if !on {
				on, s = true, seg.Start
				// markdown-rs starts a text that opens with a character
				// escape after the backslash.
				if seg.Stop-seg.Start >= 2 && src[seg.Start] == '\\' && util.IsPunct(src[seg.Start+1]) {
					s++
				}
			}
			e = max(e, seg.Stop)
			if v.IsRaw() {
				value.Write(seg.Value(src))
			} else {
				value.WriteString(mdastDecode(seg.Value(src)))
			}
			if v.SoftLineBreak() {
				p := seg.Stop
				for p < len(src) && (src[p] == ' ' || src[p] == '\t') {
					p++
				}
				value.Write(src[p:a.nextLineStart(p)])
			}
			if v.HardLineBreak() {
				flush()
				out = append(out, a.node(MdastBreak, seg.Stop, a.nextLineStart(seg.Stop)))
			}
		case *gast.String:
			if !on {
				on = true
			}
			value.Write(v.Value)
		case *east.FootnoteBacklink, *east.TaskCheckBox:
			// mdast drops the checkbox (ListItem.checked carries it).
		default:
			flush()
			if m := a.inline(ch); m != nil {
				out = append(out, m)
			}
		}
	}
	flush()
	return out
}

func mdastLastEnd(c []*MdastNode, dflt int) int {
	if len(c) > 0 {
		return mdastEnd(c[len(c)-1])
	}
	return dflt
}

// linkTail scans an inline link's `(destination "title")` from the `(`
// and returns the offset past the closing `)`.
func (a *mdastAdapter) linkTail(p int) int {
	src := a.src
	p++ // (
	skip := func() {
		for p < len(src) && mdastIsWS(src[p]) {
			p++
		}
	}
	skip()
	if p < len(src) && src[p] == '<' {
		for p < len(src) && src[p] != '>' {
			if src[p] == '\\' {
				p++
			}
			p++
		}
		p++
	} else {
		depth := 0
		for p < len(src) && !mdastIsWS(src[p]) {
			c := src[p]
			if c == '\\' {
				p += 2
				continue
			}
			if c == '(' {
				depth++
			} else if c == ')' {
				if depth == 0 {
					break
				}
				depth--
			}
			p++
		}
	}
	skip()
	if p < len(src) && (src[p] == '"' || src[p] == '\'' || src[p] == '(') {
		cl := src[p]
		if cl == '(' {
			cl = ')'
		}
		p++
		for p < len(src) && src[p] != cl {
			if src[p] == '\\' {
				p++
			}
			p++
		}
		p++
		skip()
	}
	return min(p+1, len(src))
}

// mdastPlain is mdast's to_string over an image's description: the values
// of its descendants.
func mdastPlain(c []*MdastNode) string {
	var b strings.Builder
	for _, m := range c {
		switch m.Kind {
		case MdastText, MdastInlineCode, MdastInlineMath:
			b.WriteString(m.Value)
		}
		b.WriteString(mdastPlain(m.Children))
	}
	return b.String()
}

func (a *mdastAdapter) inline(n gast.Node) *MdastNode {
	src := a.src
	switch v := n.(type) {
	case *gast.CodeSpan:
		s := v.Pos()
		k := 0
		for s+k < len(src) && src[s+k] == '`' {
			k++
		}
		p := s + k
		for p < len(src) {
			if src[p] == '`' {
				j := p
				for j < len(src) && src[j] == '`' {
					j++
				}
				if j-p == k {
					break
				}
				p = j
				continue
			}
			p++
		}
		// goldmark's raw segments exclude container prefixes and already
		// strip the padding; line endings become spaces (CommonMark).
		var body strings.Builder
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			if t, ok := c.(*gast.Text); ok {
				body.Write(src[t.Segment.Start:t.Segment.Stop])
			}
		}
		value := strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(body.String())
		m := a.node(MdastInlineCode, s, min(p+k, len(src)))
		m.Value = value
		return m
	case *gast.Emphasis:
		kind := MdastEmphasis
		if v.Level == 2 {
			kind = MdastStrong
		}
		children := a.inlines(n)
		s := v.Pos()
		e := mdastLastEnd(children, v.Pos()+v.Level) + v.Level
		// goldmark's Pos is the start of the whole delimiter run, not the
		// part this node consumed.
		if len(children) > 0 && mdastStart(children[0])-v.Level > s {
			s = mdastStart(children[0]) - v.Level
		}
		m := a.node(kind, s, e)
		m.Children = children
		return m
	case *east.Strikethrough:
		children := a.inlines(n)
		p := mdastLastEnd(children, v.Pos())
		for p < len(src) && src[p] == '~' {
			p++
		}
		m := a.node(MdastDelete, v.Pos(), p)
		m.Children = children
		return m
	case *gast.Link, *gast.Image:
		var dest, title []byte
		var ref *gast.ReferenceLink
		isImg := false
		if l, ok := n.(*gast.Link); ok {
			dest, title, ref = l.Destination, l.Title, l.Reference
		} else {
			im := n.(*gast.Image)
			dest, title, ref = im.Destination, im.Title, im.Reference
			isImg = true
		}
		s := n.Pos()
		open := 1
		if isImg {
			open = 2
		}
		children := a.inlines(n)
		b := mdastLastEnd(children, s+open) // ']'
		if b < len(src) && src[b] != ']' {
			if i := bytes.IndexByte(src[b:], ']'); i >= 0 {
				b += i
			}
		}
		m := &MdastNode{Kind: MdastLink}
		var e int
		if ref != nil {
			m.Kind = MdastLinkReference
			if isImg {
				m.Kind = MdastImageReference
			}
			label := src[s+open : b]
			switch ref.Type {
			case gast.ReferenceLinkFull:
				p := bytes.IndexByte(src[b+2:], ']') + b + 2
				label = src[b+2 : p]
				e = p + 1
				m.ReferenceKind = MdastReferenceFull
			case gast.ReferenceLinkCollapsed:
				e = b + 3
				m.ReferenceKind = MdastReferenceCollapsed
			default:
				e = b + 1
				m.ReferenceKind = MdastReferenceShortcut
			}
			m.Identifier = mdastNormalizeIdentifier(label)
			l := string(label)
			m.Label = &l
		} else {
			e = a.linkTail(b + 1)
			m.URL = mdastDecode(dest)
			if title != nil {
				t := mdastDecode(title)
				m.Title = &t
			}
		}
		a.setSpan(m, s, e)
		if isImg {
			if m.Kind == MdastLink {
				m.Kind = MdastImage
			}
			m.Alt = mdastPlain(children)
			return m
		}
		m.Children = children
		return m
	case *gast.AutoLink:
		label := v.Label(src)
		url := string(v.URL(src))
		if v.AutoLinkType == gast.AutoLinkEmail && !strings.HasPrefix(url, "mailto:") {
			url = "mailto:" + url
		}
		var s, ts, te int
		if src[v.Pos()] == '<' {
			s = v.Pos()
			ts, te = s+1, s+1+len(label)
			m := a.node(MdastLink, s, te+1)
			m.URL = url
			t := a.node(MdastText, ts, te)
			t.Value = string(label)
			m.Children = []*MdastNode{t}
			return m
		}
		// goldmark's linkify Pos is not the literal's start.
		s = v.Pos() + max(bytes.Index(src[v.Pos():], label), 0)
		if v.Protocol == nil && !strings.Contains(url, "://") && v.AutoLinkType == gast.AutoLinkURL {
			url = "http://" + url
		}
		m := a.node(MdastLink, s, s+len(label))
		m.URL = url
		t := a.node(MdastText, s, s+len(label))
		t.Value = string(label)
		m.Children = []*MdastNode{t}
		return m
	case *gast.RawHTML:
		if v.Segments.Len() == 0 {
			return nil
		}
		s := v.Segments.At(0).Start
		e := v.Segments.At(v.Segments.Len() - 1).Stop
		m := a.node(MdastHTML, s, e)
		m.Value = string(src[s:e])
		return m
	case *east.FootnoteLink:
		s := v.Pos()
		e := bytes.IndexByte(src[s:], ']') + s + 1
		m := a.node(MdastFootnoteReference, s, e)
		m.Identifier = mdastNormalizeIdentifier(src[s+2 : e-1])
		label := string(src[s+2 : e-1])
		m.Label = &label
		return m
	case *mathTextNode:
		m := a.node(MdastInlineMath, v.start, v.end)
		m.Value = v.value
		return m
	}
	return nil
}

// mdastDebugLines renders the tree one node per line as
// `kind start..end detail`, the format of the recorded markdown-rs oracle
// (.scratch/gpui-base/research/oracles/markdown/src/main.rs).
func mdastDebugLines(root *MdastNode) []string {
	var out []string
	optq := func(s *string) string {
		if s == nil {
			return "None"
		}
		return "Some(" + strconv.Quote(*s) + ")"
	}
	var walk func(n *MdastNode, depth int)
	walk = func(n *MdastNode, depth int) {
		detail := ""
		switch n.Kind {
		case MdastText, MdastInlineCode, MdastInlineMath, MdastMath, MdastHTML, MdastYaml, MdastToml:
			detail = strconv.Quote(n.Value)
		case MdastCode:
			detail = "lang=" + optq(n.Lang) + " " + strconv.Quote(n.Value)
		case MdastHeading:
			detail = fmt.Sprintf("depth=%d", n.Depth)
		case MdastList:
			start := "None"
			if n.Start != nil {
				start = fmt.Sprintf("Some(%d)", *n.Start)
			}
			detail = fmt.Sprintf("ordered=%v start=%s spread=%v", n.Ordered, start, n.Spread)
		case MdastListItem:
			checked := "None"
			if n.Checked != nil {
				checked = fmt.Sprintf("Some(%v)", *n.Checked)
			}
			detail = fmt.Sprintf("checked=%s spread=%v", checked, n.Spread)
		case MdastLink:
			detail = "url=" + strconv.Quote(n.URL) + " title=" + optq(n.Title)
		case MdastImage:
			detail = "url=" + strconv.Quote(n.URL) + " alt=" + strconv.Quote(n.Alt) + " title=" + optq(n.Title)
		case MdastFootnoteReference, MdastFootnoteDefinition, MdastLinkReference, MdastImageReference:
			detail = "id=" + strconv.Quote(n.Identifier)
		case MdastDefinition:
			detail = "id=" + strconv.Quote(n.Identifier) + " url=" + strconv.Quote(n.URL) + " title=" + optq(n.Title)
		case MdastTable:
			names := map[MdastAlignKind]string{MdastAlignNone: "None", MdastAlignLeft: "Left", MdastAlignRight: "Right", MdastAlignCenter: "Center"}
			var al []string
			for _, x := range n.Align {
				al = append(al, names[x])
			}
			detail = "align=[" + strings.Join(al, ", ") + "]"
		}
		s, e := -1, -1
		if n.Position != nil {
			s, e = n.Position.Start.Offset, n.Position.End.Offset
		}
		out = append(out, fmt.Sprintf("%s%s %d..%d %s", strings.Repeat("  ", depth), n.Kind, s, e, detail))
		for _, c := range n.Children {
			walk(c, depth+1)
		}
	}
	walk(root, 0)
	return out
}
