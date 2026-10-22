package base

// Port of U:text/markdown_ext.rs: the public Markdown extension surface
// (MarkdownPlugin, MarkdownParseContext, MarkdownNode, MarkdownExtensions).
// Plugins receive the mdast tree of text_mdast.go.
//
// Not ported here: the inline renderer half (MarkdownInlineRenderFn,
// MarkdownPlugin::render_inline, MarkdownExtensions::render_inline,
// U:text/markdown_ext.rs:38-42,64-74,434-444) needs InlineElement and
// InlineRenderContext (U:text/inline_element.rs), which are ticket 28's.
// Until then inlineRenderers keeps each inline plugin under its name (the
// value Rust's renderer closure captures), so the registry's shape and
// hasSameParserConfiguration match upstream; ticket 28 calls the plugin's
// render_inline through it. MDX (mdx()) is
// accepted, but parsing with it fails with errMdastMDXUnsupported
// (ticket 01 deferral). The test fixture TestInlinePlugin
// (U:text/markdown_ext.rs:462-524) lives in text_markdown_test.go.

import (
	"sync/atomic"

	"github.com/hoangvu12/gpui-go/gpui"
)

// markdownExtensionsRevision hands out revisions 1, 2, 3, ... (Rust's
// fetch_add on a counter starting at 1; U:text/markdown_ext.rs:18,457-459).
var markdownExtensionsRevision atomic.Uint64

// MarkdownBlockParserFn is a custom Markdown block parser
// (U:text/markdown_ext.rs:23-29). Parsers run during AST conversion, often
// off the main thread; they must not depend on a Window or App. A nil
// result declines the node.
type MarkdownBlockParserFn func(node *MdastNode, cx *MarkdownParseContext) *MarkdownNode

// MarkdownBlockRenderFn renders a custom block node
// (U:text/markdown_ext.rs:31-33).
type MarkdownBlockRenderFn func(node *MarkdownNode, window *gpui.Window, cx *gpui.App) gpui.AnyElement

// markdownInlineParserFn is the parser for one inline AST node; it follows
// the block parser contract (U:text/markdown_ext.rs:35-36).
type markdownInlineParserFn = MarkdownBlockParserFn

// MarkdownPlugin is a reusable Markdown extension that parses and renders
// one custom node (U:text/markdown_ext.rs:44-75): the required methods.
// MarkdownPluginBlock and MarkdownPluginRenderer are the optional
// overrides; MarkdownPluginIsBlock and MarkdownPluginRender supply the
// defaults (doc.go).
type MarkdownPlugin interface {
	// Name is the stable name of the nodes this plugin produces.
	Name() string
	// Parse converts an mdast node into a custom node, or returns nil.
	Parse(node *MdastNode, cx *MarkdownParseContext) *MarkdownNode
}

// MarkdownPluginBlock is the optional is_block override
// (U:text/markdown_ext.rs:46-51).
type MarkdownPluginBlock interface {
	IsBlock() bool
}

// MarkdownPluginRenderer is the optional render override
// (U:text/markdown_ext.rs:59-62).
type MarkdownPluginRenderer interface {
	Render(node *MarkdownNode, window *gpui.Window, cx *gpui.App) gpui.AnyElement
}

// MarkdownPluginIsBlock reports whether p produces block-level nodes:
// the override when implemented, otherwise false (plugins are inline by
// default; U:text/markdown_ext.rs:46-51).
func MarkdownPluginIsBlock(p MarkdownPlugin) bool {
	if b, ok := p.(MarkdownPluginBlock); ok {
		return b.IsBlock()
	}
	return false
}

// MarkdownPluginRender renders node: the override when implemented,
// otherwise the node's plain text (U:text/markdown_ext.rs:59-62).
func MarkdownPluginRender(p MarkdownPlugin, node *MarkdownNode, window *gpui.Window, cx *gpui.App) gpui.AnyElement {
	if r, ok := p.(MarkdownPluginRenderer); ok {
		return r.Render(node, window, cx)
	}
	return gpui.Text(node.AsText())
}

// MarkdownParseContext is the context passed to custom Markdown parsers
// (U:text/markdown_ext.rs:77-104).
type MarkdownParseContext struct {
	source string
	offset int
}

// newMarkdownParseContext is MarkdownParseContext::new
// (U:text/markdown_ext.rs:84-86).
func newMarkdownParseContext(source string, offset int) *MarkdownParseContext {
	return &MarkdownParseContext{source: source, offset: offset}
}

// Source is the text of the Markdown fragment being parsed.
func (c *MarkdownParseContext) Source() string { return c.source }

// Offset is the byte offset of Source in the full document when an
// appended fragment is parsed.
func (c *MarkdownParseContext) Offset() int { return c.offset }

// NodeSource is the source slice of node, if it has a position inside
// Source (U:text/markdown_ext.rs:99-103).
func (c *MarkdownParseContext) NodeSource(node *MdastNode) (string, bool) {
	if node == nil || node.Position == nil {
		return "", false
	}
	s, e := node.Position.Start.Offset, node.Position.End.Offset
	if s < 0 || e > len(c.source) || s > e {
		return "", false
	}
	return c.source[s:e], true
}

// MarkdownNode is a custom node produced by MarkdownExtensions
// (U:text/markdown_ext.rs:106-115). It is a value: builders return a
// modified copy, as Rust's `self`-consuming builders do.
type MarkdownNode struct {
	name               string
	text               string
	markdown           string
	accessibilityLabel *string
	data               any
	span               *Span
}

// NewMarkdownNode creates a custom node with a stable name and typed data
// (U:text/markdown_ext.rs:118-131).
func NewMarkdownNode(name string, data any) MarkdownNode {
	return MarkdownNode{name: name, data: data}
}

// Name is the node's stable name.
func (n MarkdownNode) Name() string { return n.name }

// AsText is the node's text representation.
func (n MarkdownNode) AsText() string { return n.text }

// AsMarkdown is the node's Markdown representation.
func (n MarkdownNode) AsMarkdown() string { return n.markdown }

// SourceRange is the node's full-document UTF-8 source range, including
// syntax delimiters (U:text/markdown_ext.rs:148-151).
func (n MarkdownNode) SourceRange() (gpui.TextRange, bool) {
	if n.span == nil {
		return gpui.TextRange{}, false
	}
	return gpui.TextRange{Start: n.span.Start, End: n.span.End}, true
}

// AccessibilityName is the name exposed to accessibility clients,
// defaulting to the plain text (U:text/markdown_ext.rs:153-156).
func (n MarkdownNode) AccessibilityName() string {
	if n.accessibilityLabel != nil {
		return *n.accessibilityLabel
	}
	return n.text
}

// AccessibilityLabel sets the accessible name; an empty label clears it
// (U:text/markdown_ext.rs:158-162).
func (n MarkdownNode) AccessibilityLabel(label string) MarkdownNode {
	if label == "" {
		n.accessibilityLabel = nil
	} else {
		n.accessibilityLabel = &label
	}
	return n
}

// withInlineSource fills an empty text or Markdown representation with
// the node's source (U:text/markdown_ext.rs:180-188).
func (n MarkdownNode) withInlineSource(source string) MarkdownNode {
	if n.text == "" {
		n.text = source
	}
	if n.markdown == "" {
		n.markdown = source
	}
	return n
}

// Text sets the node's text representation (U:text/markdown_ext.rs:190-194).
func (n MarkdownNode) Text(text string) MarkdownNode {
	n.text = text
	return n
}

// Markdown sets the node's Markdown representation
// (U:text/markdown_ext.rs:196-200).
func (n MarkdownNode) Markdown(markdown string) MarkdownNode {
	n.markdown = markdown
	return n
}

// Data is the node's data, untyped; MarkdownNodeData reads it typed.
func (n MarkdownNode) Data() any { return n.data }

// MarkdownNodeData is MarkdownNode::data::<T> (U:text/markdown_ext.rs:
// 202-208): the data when it holds a T.
func MarkdownNodeData[T any](n MarkdownNode) (T, bool) {
	v, ok := n.data.(T)
	return v, ok
}

// setSpan is MarkdownNode::set_span (U:text/markdown_ext.rs:210-212).
func (n *MarkdownNode) setSpan(span *Span) { n.span = span }

// toMarkdown is the Markdown, falling back to the text
// (U:text/markdown_ext.rs:214-220).
func (n MarkdownNode) toMarkdown() string {
	if n.markdown == "" {
		return n.text
	}
	return n.markdown
}

// equal is MarkdownNode's PartialEq (U:text/markdown_ext.rs:234-242).
func (n MarkdownNode) equal(o MarkdownNode) bool {
	spanEq := (n.span == nil) == (o.span == nil) && (n.span == nil || *n.span == *o.span)
	return n.name == o.name && n.text == o.text && n.markdown == o.markdown &&
		optStringEqual(n.accessibilityLabel, o.accessibilityLabel) && spanEq
}

// MarkdownExtensions is the registry for custom Markdown parsing and
// rendering (U:text/markdown_ext.rs:244-255). The zero value is
// Default::default(); builders return a modified copy.
type MarkdownExtensions struct {
	enableMDX         bool
	enableFrontmatter bool
	blockParsers      []MarkdownBlockParserFn
	blockRenderers    map[string]MarkdownBlockRenderFn
	inlineParsers     []markdownInlineParserFn
	// inlineRenderers maps a node name to the inline plugin that renders
	// it (U:text/markdown_ext.rs:252,331-336).
	inlineRenderers map[string]MarkdownPlugin
	revision        uint64
	parserRevision  uint64
}

// clone copies the registry so a builder never mutates its receiver's
// slices or maps (Rust's builders consume `self`).
func (e MarkdownExtensions) clone() MarkdownExtensions {
	e.blockParsers = append([]MarkdownBlockParserFn(nil), e.blockParsers...)
	e.inlineParsers = append([]markdownInlineParserFn(nil), e.inlineParsers...)
	renderers := make(map[string]MarkdownBlockRenderFn, len(e.blockRenderers))
	for k, v := range e.blockRenderers {
		renderers[k] = v
	}
	e.blockRenderers = renderers
	inline := make(map[string]MarkdownPlugin, len(e.inlineRenderers))
	for k, v := range e.inlineRenderers {
		inline[k] = v
	}
	e.inlineRenderers = inline
	return e
}

// ParserRevision sets the parser revision; change it when parser captures
// or plugin configuration change, so equivalent registrations rebuilt
// while rendering keep the parsed document (U:text/markdown_ext.rs:258-265).
func (e MarkdownExtensions) ParserRevision(revision uint64) MarkdownExtensions {
	e = e.clone()
	e.parserRevision = revision
	e.bumpRevision()
	return e
}

// Frontmatter enables YAML/TOML frontmatter parsing, off by default
// (U:text/markdown_ext.rs:267-276).
func (e MarkdownExtensions) Frontmatter() MarkdownExtensions {
	e = e.clone()
	e.enableFrontmatter = true
	e.bumpRevision()
	return e
}

// MDX enables MDX JSX/expression constructs, which disables raw HTML
// (U:text/markdown_ext.rs:278-286). MDX parsing is deferred (ticket 01):
// a document parsed with it fails with errMdastMDXUnsupported.
func (e MarkdownExtensions) MDX() MarkdownExtensions {
	e = e.clone()
	e.enableMDX = true
	e.bumpRevision()
	return e
}

// BlockParser registers a parser for block-level mdast nodes
// (U:text/markdown_ext.rs:288-298).
func (e MarkdownExtensions) BlockParser(parser MarkdownBlockParserFn) MarkdownExtensions {
	e = e.clone()
	e.pushBlockParser(parser)
	return e
}

// BlockRenderer registers a renderer for a custom block node name
// (U:text/markdown_ext.rs:300-308).
func (e MarkdownExtensions) BlockRenderer(name string, renderer MarkdownBlockRenderFn) MarkdownExtensions {
	e = e.clone()
	e.pushBlockRenderer(name, renderer)
	return e
}

// Plugin applies a reusable Markdown plugin (U:text/markdown_ext.rs:
// 310-340). A block plugin registers its parser and block renderer; an
// inline plugin registers its parser and itself as the renderer of its
// name (rendering it is ticket 28's).
func (e MarkdownExtensions) Plugin(plugin MarkdownPlugin) MarkdownExtensions {
	name := plugin.Name()
	parse := func(node *MdastNode, cx *MarkdownParseContext) *MarkdownNode { return plugin.Parse(node, cx) }
	if MarkdownPluginIsBlock(plugin) {
		e = e.BlockParser(parse)
		e.pushBlockRenderer(name, func(node *MarkdownNode, window *gpui.Window, cx *gpui.App) gpui.AnyElement {
			return MarkdownPluginRender(plugin, node, window, cx)
		})
		return e
	}
	e = e.clone()
	e.inlineParsers = append(e.inlineParsers, parse)
	e.inlineRenderers[name] = plugin
	e.bumpRevision()
	return e
}

// revisionValue is MarkdownExtensions::revision
// (U:text/markdown_ext.rs:342-344).
func (e *MarkdownExtensions) revisionValue() uint64 { return e.revision }

// hasSameParserConfiguration reports whether replacing e with other can
// change the parsed tree (U:text/markdown_ext.rs:346-367).
func (e *MarkdownExtensions) hasSameParserConfiguration(other *MarkdownExtensions) bool {
	if e.parserRevision != other.parserRevision || e.enableMDX != other.enableMDX ||
		e.enableFrontmatter != other.enableFrontmatter ||
		len(e.blockParsers) != len(other.blockParsers) ||
		len(e.blockRenderers) != len(other.blockRenderers) ||
		len(e.inlineParsers) != len(other.inlineParsers) ||
		len(e.inlineRenderers) != len(other.inlineRenderers) {
		return false
	}
	for name := range e.inlineRenderers {
		if _, ok := other.inlineRenderers[name]; !ok {
			return false
		}
	}
	for name := range e.blockRenderers {
		if _, ok := other.blockRenderers[name]; !ok {
			return false
		}
	}
	return true
}

// pushBlockParser is MarkdownExtensions::push_block_parser
// (U:text/markdown_ext.rs:369-378).
func (e *MarkdownExtensions) pushBlockParser(parser MarkdownBlockParserFn) {
	e.blockParsers = append(e.blockParsers, parser)
	e.bumpRevision()
}

// pushBlockRenderer is MarkdownExtensions::push_block_renderer
// (U:text/markdown_ext.rs:380-390).
func (e *MarkdownExtensions) pushBlockRenderer(name string, renderer MarkdownBlockRenderFn) {
	if e.blockRenderers == nil {
		e.blockRenderers = map[string]MarkdownBlockRenderFn{}
	}
	e.blockRenderers[name] = renderer
	e.bumpRevision()
}

// parseOptions is MarkdownExtensions::parse_options
// (U:text/markdown_ext.rs:392-409): GFM, math (text and flow, both or
// neither so a `$$` block is never swallowed inline), optional
// frontmatter, optional MDX.
func (e *MarkdownExtensions) parseOptions() mdastParseOptions {
	return mdastParseOptions{
		frontmatter: e.enableFrontmatter,
		mathText:    true,
		mathFlow:    true,
		mdx:         e.enableMDX,
	}
}

// parseBlock runs the block parsers in order; the first match wins
// (U:text/markdown_ext.rs:411-422).
func (e *MarkdownExtensions) parseBlock(node *MdastNode, cx *MarkdownParseContext) *MarkdownNode {
	for _, parser := range e.blockParsers {
		if n := parser(node, cx); n != nil {
			return n
		}
	}
	return nil
}

// parseInline runs the inline parsers in order; the first match wins
// (U:text/markdown_ext.rs:424-432).
func (e *MarkdownExtensions) parseInline(node *MdastNode, cx *MarkdownParseContext) *MarkdownNode {
	for _, parser := range e.inlineParsers {
		if n := parser(node, cx); n != nil {
			return n
		}
	}
	return nil
}

// renderBlock renders a custom block node with its registered renderer
// (U:text/markdown_ext.rs:446-455).
func (e *MarkdownExtensions) renderBlock(node *MarkdownNode, window *gpui.Window, cx *gpui.App) (gpui.AnyElement, bool) {
	render, ok := e.blockRenderers[node.Name()]
	if !ok {
		return gpui.AnyElement{}, false
	}
	return render(node, window, cx), true
}

func (e *MarkdownExtensions) bumpRevision() {
	e.revision = markdownExtensionsRevision.Add(1)
}
