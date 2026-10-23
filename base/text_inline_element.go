package base

// Port of U:text/inline_element.rs (InlineElement, InlineRenderContext)
// and the inline renderer half of U:text/markdown_ext.rs that needs them
// (MarkdownInlineRenderFn, MarkdownPlugin::render_inline,
// MarkdownExtensions::render_inline; U:text/markdown_ext.rs:38-42,64-74,
// 434-444). text_markdown_ext.go keeps each inline plugin under its node
// name; renderInline calls it here (ticket 28).

import "github.com/hoangvu12/gpui-go/gpui"

// InlineElement is a GPUI element participating as one atomic object in a
// text line (U:text/inline_element.rs:3-11). TextView measures its
// intrinsic size and wraps around the whole object.
type InlineElement struct {
	element  gpui.AnyElement
	baseline *float32
}

// NewInlineElement wraps an element (U:text/inline_element.rs:14-19);
// element is anything gpui.TryIntoElement accepts.
func NewInlineElement(element any) InlineElement {
	el, err := gpui.TryIntoElement(element)
	if err != nil {
		panic("base: NewInlineElement: " + err.Error())
	}
	return InlineElement{element: el}
}

// WithBaseline sets the distance from the top edge to the alphabetic
// baseline, in logical pixels; by default the box aligns its bottom with
// the surrounding text's descent (U:text/inline_element.rs:21-26).
func (e InlineElement) WithBaseline(baseline float32) InlineElement {
	e.baseline = &baseline
	return e
}

// InlineRenderContext is the inherited typography for an inline renderer
// (U:text/inline_element.rs:29-38). TextView builds it while laying a line
// out; renderers only read it.
type InlineRenderContext struct {
	textStyle  gpui.TextStyle
	fontSize   float32
	lineHeight float32
	remSize    float32
}

// newInlineRenderContext is InlineRenderContext::new
// (U:text/inline_element.rs:41-53).
func newInlineRenderContext(textStyle gpui.TextStyle, fontSize, lineHeight, remSize float32) *InlineRenderContext {
	return &InlineRenderContext{textStyle: textStyle, fontSize: fontSize, lineHeight: lineHeight, remSize: remSize}
}

// TextStyle is the effective text style at the object's position, marks
// already applied.
func (c *InlineRenderContext) TextStyle() gpui.TextStyle { return c.textStyle }

// FontSize is the font size of the surrounding text, in logical pixels.
func (c *InlineRenderContext) FontSize() float32 { return c.fontSize }

// LineHeight is the line height of the surrounding text, in logical pixels.
func (c *InlineRenderContext) LineHeight() float32 { return c.lineHeight }

// RemSize is the root font size, for resolving rem-relative lengths.
func (c *InlineRenderContext) RemSize() float32 { return c.remSize }

// MarkdownInlineRenderFn produces a native inline element, or false for the
// atomic text fallback (U:text/markdown_ext.rs:38-42).
type MarkdownInlineRenderFn func(node *MarkdownNode, context *InlineRenderContext, window *gpui.Window, cx *gpui.App) (InlineElement, bool)

// MarkdownPluginInlineRenderer is the optional render_inline override
// (U:text/markdown_ext.rs:64-74): override it only when inherited layout
// context or an explicit baseline is needed.
type MarkdownPluginInlineRenderer interface {
	RenderInline(node *MarkdownNode, context *InlineRenderContext, window *gpui.Window, cx *gpui.App) (InlineElement, bool)
}

// MarkdownPluginRenderInline renders an inline node: the override when
// implemented, otherwise the plugin's render wrapped in an InlineElement
// (U:text/markdown_ext.rs:64-74).
func MarkdownPluginRenderInline(p MarkdownPlugin, node *MarkdownNode, context *InlineRenderContext, window *gpui.Window, cx *gpui.App) (InlineElement, bool) {
	if r, ok := p.(MarkdownPluginInlineRenderer); ok {
		return r.RenderInline(node, context, window, cx)
	}
	return InlineElement{element: MarkdownPluginRender(p, node, window, cx)}, true
}

// renderInline renders a custom inline node with the plugin registered for
// its name (U:text/markdown_ext.rs:434-444).
func (e *MarkdownExtensions) renderInline(node *MarkdownNode, context *InlineRenderContext, window *gpui.Window, cx *gpui.App) (InlineElement, bool) {
	if e == nil {
		return InlineElement{}, false
	}
	plugin, ok := e.inlineRenderers[node.Name()]
	if !ok {
		return InlineElement{}, false
	}
	return MarkdownPluginRenderInline(plugin, node, context, window, cx)
}
