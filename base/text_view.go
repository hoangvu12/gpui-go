package base

// Port of U:text/text_view.rs (ticket 28) and the TextView constructors of
// U:text/mod.rs: the TextView element, its builders and hooks, the
// application defaults, and the max_lines clip.
//
// Go adaptations (recorded deviations):
//   - CodeBlockHighlighterFn identity: the code block highlight cache is
//     keyed on the highlighter's Arc (U:text/node.rs:1843-1846). Go funcs
//     do not compare, so a highlighter is a *CodeBlockHighlighter; the
//     builder wraps a fresh one per call, as Rust wraps a fresh Arc.
//   - Copy reads the window TextSelection (ticket 29) through
//     textSelectionSelectedText; without it the state's own selection
//     (select-all, multi-click) is copied.
//   - The selection adapter's per-frame registration and touch handles
//     are ticket 29's (text_selection_adapter.go); the reveal handler is
//     ticket 30's (text_range_highlight_state.go).
//   - TextViewPlugin: a MarkdownPlugin is applied with Plugin; other
//     plugins implement TextViewPlugin.

import (
	"math"
	"strconv"
	"strings"

	"github.com/hoangvu12/gpui-go/gpui"
)

// CodeBlockActionsFn renders the actions of a code block
// (U:text/text_view.rs:20-22).
type CodeBlockActionsFn func(block *CodeBlock, w *gpui.Window, app *gpui.App) gpui.AnyElement

// CodeBlockHighlighter highlights a code block; its identity keys the
// block's highlight cache (U:text/text_view.rs:24-25).
type CodeBlockHighlighter struct {
	highlight func(block *CodeBlock) []gpui.HighlightRange
}

// NewCodeBlockHighlighter wraps a highlighter function.
func NewCodeBlockHighlighter(f func(block *CodeBlock) []gpui.HighlightRange) *CodeBlockHighlighter {
	return &CodeBlockHighlighter{highlight: f}
}

// TableActionsFn renders the actions row of a table
// (U:text/text_view.rs:74-76).
type TableActionsFn func(table *TableData, w *gpui.Window, app *gpui.App) gpui.AnyElement

// ImageSourceFn resolves a document image URL (U:text/text_view.rs:78).
type ImageSourceFn func(url string) gpui.ImageSource

// LinkClickHandlerFn handles a link click (U:text/text_view.rs:80-81).
type LinkClickHandlerFn func(url string, event *gpui.ClickEvent, w *gpui.Window, app *gpui.App)

// handleLinkClick calls the handler, or opens the URL for a left or middle
// mouse click, a keyboard click or a tap (U:text/text_view.rs:87-105).
func handleLinkClick(handler LinkClickHandlerFn, url string, event gpui.ClickEvent, w *gpui.Window, app *gpui.App) {
	if handler != nil {
		handler(url, &event, w, app)
		return
	}
	open := true
	switch event.Kind {
	case gpui.ClickEventMouse:
		open = event.Mouse.Up.Button == gpui.MouseButtonLeft || event.Mouse.Up.Button == gpui.MouseButtonMiddle
	case gpui.ClickEventTouch:
		open = !event.Touch.LongPress
	}
	if open {
		_ = app.OpenURL(url)
	}
}

// TextViewDefaults are application-wide defaults for TextViews without
// explicit presentation or highlighting overrides
// (U:text/text_view.rs:27-72).
type TextViewDefaults struct {
	style                *TextViewStyle
	codeBlockHighlighter *CodeBlockHighlighter
}

// NewTextViewDefaults leaves every view as Base renders it.
func NewTextViewDefaults() TextViewDefaults { return TextViewDefaults{} }

// WithStyle sets the style every view starts from.
func (d TextViewDefaults) WithStyle(style TextViewStyle) TextViewDefaults { d.style = &style; return d }

// WithCodeBlockHighlighter sets the highlighter for fenced code blocks.
func (d TextViewDefaults) WithCodeBlockHighlighter(f func(block *CodeBlock) []gpui.HighlightRange) TextViewDefaults {
	d.codeBlockHighlighter = NewCodeBlockHighlighter(f)
	return d
}

// Install installs the defaults for the whole application.
func (d TextViewDefaults) Install(app *gpui.App) { app.SetGlobal(&d) }

// TextViewDefaultsGlobal returns the installed defaults, or Base's.
func TextViewDefaultsGlobal(app *gpui.App) TextViewDefaults {
	if d, ok := gpui.TryGlobal[*TextViewDefaults](app); ok {
		return *d
	}
	return TextViewDefaults{}
}

// HasCodeBlockHighlighter reports whether a highlighter was installed.
func (d TextViewDefaults) HasCodeBlockHighlighter() bool { return d.codeBlockHighlighter != nil }

// TextViewPlugin configures a TextView (U:text/text_view.rs:145-160).
type TextViewPlugin interface {
	Setup(view *TextView) *TextView
}

// TextView renders Markdown (or HTML) (U:text/text_view.rs:107-143).
type TextView struct {
	gpui.Styled[*TextView]
	id                   gpui.ElementID
	format               *textViewFormat
	text                 *string
	state                *gpui.Entity[TextViewState]
	textViewStyle        *TextViewStyle
	selectable           bool
	selectionFormat      SelectionFormat
	scrollable           bool
	maxLines             *int
	codeBlockActions     CodeBlockActionsFn
	codeBlockHighlighter *CodeBlockHighlighter
	tableActions         TableActionsFn
	linkClickHandler     LinkClickHandlerFn
	imageSource          ImageSourceFn
	markdownExtensions   *MarkdownExtensions
	revealHandler        RevealHandlerFn
	motion               *TextViewMotion
}

// NewTextView creates a TextView over a managed state
// (U:text/text_view.rs:169-191).
func NewTextView(state gpui.Entity[TextViewState]) *TextView {
	return gpui.BindStyled(&TextView{
		id:                 gpui.NameElementID(strconv.FormatUint(uint64(state.EntityID()), 10)),
		state:              &state,
		selectable:         true,
		markdownExtensions: &MarkdownExtensions{},
	})
}

// TextViewMarkdown creates a Markdown text view (U:text/text_view.rs:193-215).
func TextViewMarkdown[I gpui.IntoElementID](id I, markdown string) *TextView {
	f := textViewFormatMarkdown
	return gpui.BindStyled(&TextView{id: gpui.ElementIDOf(id), format: &f, text: &markdown, selectable: true, markdownExtensions: &MarkdownExtensions{}})
}

// TextViewHTML creates an HTML text view (U:text/text_view.rs:217-239).
func TextViewHTML[I gpui.IntoElementID](id I, html string) *TextView {
	f := textViewFormatHTML
	return gpui.BindStyled(&TextView{id: gpui.ElementIDOf(id), format: &f, text: &html, selectable: true, markdownExtensions: &MarkdownExtensions{}})
}

// ImageSource overrides the source of every document image
// (U:text/text_view.rs:241-252).
func (v *TextView) ImageSource(resolver ImageSourceFn) *TextView { v.imageSource = resolver; return v }

// Style sets the TextViewStyle (U:text/text_view.rs:254-258).
func (v *TextView) Style(style TextViewStyle) *TextView { v.textViewStyle = &style; return v }

// Selectable sets whether the view is selectable, true by default
// (U:text/text_view.rs:260-264).
func (v *TextView) Selectable(selectable bool) *TextView { v.selectable = selectable; return v }

// SelectionFormat sets the copy format (U:text/text_view.rs:266-273).
func (v *TextView) SelectionFormat(format SelectionFormat) *TextView {
	v.selectionFormat = format
	return v
}

// Scrollable makes the view scroll its content in a virtualized list
// (U:text/text_view.rs:275-290).
func (v *TextView) Scrollable(scrollable bool) *TextView { v.scrollable = scrollable; return v }

// MaxLines clamps the content to at most n lines of body text
// (U:text/text_view.rs:292-313).
func (v *TextView) MaxLines(n int) *TextView { v.maxLines = &n; return v }

// CodeBlockActions sets the actions of code blocks
// (U:text/text_view.rs:315-328).
func (v *TextView) CodeBlockActions(f CodeBlockActionsFn) *TextView { v.codeBlockActions = f; return v }

// CodeBlockHighlighter adds syntax highlighting for fenced code blocks
// (U:text/text_view.rs:330-340).
func (v *TextView) CodeBlockHighlighter(f func(block *CodeBlock) []gpui.HighlightRange) *TextView {
	v.codeBlockHighlighter = NewCodeBlockHighlighter(f)
	return v
}

// SharedCodeBlockHighlighter sets a highlighter that keeps its identity
// across frames (U:text/text_view.rs:342-357).
func (v *TextView) SharedCodeBlockHighlighter(h *CodeBlockHighlighter) *TextView {
	v.codeBlockHighlighter = h
	return v
}

// TableActions renders actions below each table (U:text/text_view.rs:359-372).
func (v *TextView) TableActions(f TableActionsFn) *TextView { v.tableActions = f; return v }

// OnLinkClick handles link clicks (U:text/text_view.rs:374-384).
func (v *TextView) OnLinkClick(handler LinkClickHandlerFn) *TextView {
	v.linkClickHandler = handler
	return v
}

// MarkdownExtensions replaces the extension registry
// (U:text/text_view.rs:402-406).
func (v *TextView) MarkdownExtensions(extensions MarkdownExtensions) *TextView {
	v.markdownExtensions = &extensions
	return v
}

// MarkdownMDX enables MDX parsing (U:text/text_view.rs:415-423).
func (v *TextView) MarkdownMDX() *TextView {
	e := v.markdownExtensions.MDX()
	v.markdownExtensions = &e
	return v
}

// MarkdownBlockParser registers a block parser (U:text/text_view.rs:425-442).
func (v *TextView) MarkdownBlockParser(parser MarkdownBlockParserFn) *TextView {
	e := v.markdownExtensions.BlockParser(parser)
	v.markdownExtensions = &e
	return v
}

// MarkdownBlockRenderer registers a block renderer
// (U:text/text_view.rs:444-456).
func (v *TextView) MarkdownBlockRenderer(name string, renderer MarkdownBlockRenderFn) *TextView {
	e := v.markdownExtensions.BlockRenderer(name, renderer)
	v.markdownExtensions = &e
	return v
}

// Plugin applies a Markdown plugin (U:text/text_view.rs:150-160,458-464).
func (v *TextView) Plugin(plugin MarkdownPlugin) *TextView {
	e := v.markdownExtensions.Plugin(plugin)
	v.markdownExtensions = &e
	return v
}

// WithPlugin applies a TextViewPlugin (U:text/text_view.rs:458-464).
func (v *TextView) WithPlugin(plugin TextViewPlugin) *TextView { return plugin.Setup(v) }

// textClipEpsilon absorbs sub-pixel layout jitter (U:text/text_view.rs:490-492).
const textClipEpsilon float32 = 1

// lastLineBottomAbove is the bottom of the last whole line at or above y,
// with its line height (U:text/text_view.rs:494-518).
func lastLineBottomAbove(spans []textLineSpan, y float32) (float32, float32, bool) {
	var last, lastHeight float32
	found := false
	keep := func(bottom, lineHeight float32) {
		if bottom <= y+textClipEpsilon && (!found || bottom > last) {
			last, lastHeight, found = bottom, lineHeight, true
		}
	}
	for _, span := range spans {
		if span.lineHeight <= 0 {
			continue
		}
		for bottom := span.top + span.lineHeight; bottom <= span.bottom+textClipEpsilon; bottom += span.lineHeight {
			keep(bottom, span.lineHeight)
		}
		keep(span.bottom, span.lineHeight)
	}
	return last, lastHeight, found
}

// lineSafeClipBottom is where to clip given the reported lines; false
// leaves the clip on the box edge (U:text/text_view.rs:520-570).
func lineSafeClipBottom(spans []textLineSpan, boxBottom, contentBottom float32) (float32, bool) {
	clip := boxBottom
	for _, span := range spans {
		if span.lineHeight <= 0 || span.top >= boxBottom || span.bottom <= boxBottom+textClipEpsilon {
			continue
		}
		wholeLines := float32(math.Floor(float64((boxBottom - span.top) / span.lineHeight)))
		lineTop := span.top + span.lineHeight*wholeLines
		if lineTop < boxBottom-textClipEpsilon {
			clip = min(clip, lineTop)
		}
	}
	lastBottom, lineHeight, ok := lastLineBottomAbove(spans, clip)
	if !ok {
		return 0, false
	}
	if contentBottom > boxBottom+textClipEpsilon {
		if strip := clip - lastBottom; strip > textClipEpsilon && strip < lineHeight {
			clip = lastBottom
		}
	}
	return clip, clip < boxBottom-textClipEpsilon
}

// textViewLayoutState is U:text/text_view.rs:475-478.
type textViewLayoutState struct {
	state   gpui.Entity[TextViewState]
	element gpui.AnyElement
}

// textViewPrepaintState is U:text/text_view.rs:480-488.
type textViewPrepaintState struct {
	hitbox     gpui.Hitbox
	clipBottom *float32
	// touchHandles is the touch handles this view owns, with their
	// hitboxes (ticket 29).
	touchHandles TouchHandleLayout
}

// IntoElement implements gpui.IntoElement.
func (v *TextView) IntoElement() gpui.AnyElement {
	return gpui.CustomElement[textViewLayoutState, textViewPrepaintState](v)
}

// ID implements gpui.Element (U:text/text_view.rs:576-578).
func (v *TextView) ID() (gpui.ElementID, bool) { return v.id, true }

// SourceLocation implements gpui.Element.
func (v *TextView) SourceLocation() *gpui.SourceLocation { return nil }

// RequestLayout resolves the state and style, hands the builder's options
// to the state and lays the view's box out (U:text/text_view.rs:584-706).
func (v *TextView) RequestLayout(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, w *gpui.Window, app *gpui.App) (gpui.LayoutID, textViewLayoutState) {
	var state gpui.Entity[TextViewState]
	if v.state != nil {
		state = *v.state
	} else {
		format := textViewFormatMarkdown
		if v.format != nil {
			format = *v.format
		}
		text := ""
		if v.text != nil {
			text = *v.text
		}
		state = w.UseKeyedState(gpui.NameElementID(v.id.String()+"/state"), app, func(_ *gpui.Window, cx *gpui.Context[TextViewState]) TextViewState {
			return newTextViewState(format, text, cx)
		})
		v.state = &state
	}

	var maxLines *int
	if !v.scrollable {
		maxLines = v.maxLines
	}
	defaults := TextViewDefaultsGlobal(app)
	style := v.textViewStyle
	if style == nil {
		style = defaults.style
	}
	if style == nil {
		s := TextViewStyleFromTheme(ThemeGlobal(app))
		style = &s
	}
	foreground := style.Foreground()
	highlighter := v.codeBlockHighlighter
	if highlighter == nil {
		highlighter = defaults.codeBlockHighlighter
	}
	state.Update(app, func(s *TextViewState, cx *gpui.Context[TextViewState]) {
		s.codeBlockActions = v.codeBlockActions
		s.codeBlockHighlighter = highlighter
		s.tableActions = v.tableActions
		s.linkClickHandler = v.linkClickHandler
		s.imageSource = v.imageSource
		s.setMarkdownExtensions(v.markdownExtensions, cx)
		if v.motion != nil {
			s.SetMotion(*v.motion)
		}
		s.selectable = v.selectable
		s.selectionFormat = v.selectionFormat
		s.scrollable = v.scrollable
		s.maxLines = maxLines
		if !s.textViewStyle.Equal(*style) {
			s.selectionRevision++
			copied := *style
			s.textViewStyle = &copied
		}
		if v.text != nil {
			s.setElementText(*v.text, cx)
		}
	})

	focus := state.Read(app, func(s *TextViewState, _ *gpui.App) gpui.FocusHandle { return s.focusHandle })
	list := state.Read(app, func(s *TextViewState, _ *gpui.App) *gpui.ListState { return s.listState })
	// Gap 2b: upstream's div() is Block; the box holds the state's view
	// (no text of its own), and a flex row would stretch a clamped
	// document to the capped height and hide the overflow max_lines measures.
	el := gpui.Div().Block().
		ID(gpui.NamedIntegerElementID("text-view-scroll", uint64(state.EntityID()))).
		KeyContext(textViewContext).
		TrackFocus(focus).
		Relative().
		TextColor(foreground)
	if v.scrollable {
		el = el.SizeFull()
	}
	if maxLines != nil {
		textStyle := gpui.WindowTextStyle(w)
		refinement := v.StyleMut().Text
		refinement.Refine(&textStyle)
		// The line-height cap for the clamp (browser-parity D1: the
		// default style's CSS normal resolves through the window's text
		// system, ResolveLineHeight).
		cap := gpui.ResolveLineHeight(w, textStyle) * float32(*maxLines)
		el = el.MaxH(gpui.Px(cap)).OverflowHidden()
	}
	// browser-core 73: the copy keystroke no longer dispatches this
	// listener off macOS (textInit's !mac copy row is deleted — the
	// core's copy command owns the key, untrimmed). The listener stays
	// for the action dispatches that remain: an app-bound key or a
	// programmatic DispatchAction of the aliased edit::Copy, and macOS's
	// cmd-c binding.
	el = el.OnAction(CopyAction, func(_ *Copy, w *gpui.Window, app *gpui.App) {
		var text string
		if textSelectionSelectedText != nil {
			text = textSelectionSelectedText(w, app)
		} else {
			text = state.Read(app, func(s *TextViewState, _ *gpui.App) string { return s.SelectedText() })
		}
		text = strings.TrimSpace(text)
		if text == "" {
			app.Propagate()
			return
		}
		_ = app.WriteClipboard(gpui.NewClipboardStringItem(text))
	}).
		OnAction(SelectAllAction, w.ListenerFor(state, (*TextViewState).onActionSelectAll)).
		Child(gpui.ViewOf(state))
	if v.scrollable {
		el = el.Child(gpui.Div().Flex().Absolute().Inset0().Child(
			ScrollbarVertical(list).ID(gpui.NamedIntegerElementID("text-view-scrollbar", uint64(state.EntityID()))).ViewportFromLayout(),
		))
	}
	element := StyledExtOf(el).RefineStyle(v.StyleMut()).IntoElement()
	id := element.RequestLayout(w, app)
	return id, textViewLayoutState{state: state, element: element}
}

// Prepaint collects the line spans of a clamped view and decides the clip
// (U:text/text_view.rs:708-779).
func (v *TextView) Prepaint(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, bounds gpui.Bounds, layout *textViewLayoutState, w *gpui.Window, app *gpui.App) textViewPrepaintState {
	state := layout.state
	maxLinesActive := state.Read(app, func(s *TextViewState, _ *gpui.App) bool { return s.maxLines != nil })
	if maxLinesActive {
		state.Read(app, func(s *TextViewState, _ *gpui.App) struct{} { s.lineSpans.clear(); return struct{}{} })
		textViewStatePush(app, state)
	}
	layout.element.Prepaint(w, app)
	if maxLinesActive {
		textViewStatePop(app)
	}
	var clipBottom *float32
	if maxLinesActive {
		spans, contentBottom, clamped := func() ([]textLineSpan, float32, bool) {
			var spans []textLineSpan
			var bottom float32
			var clamped bool
			state.Read(app, func(s *TextViewState, _ *gpui.App) struct{} {
				spans, bottom, clamped = s.lineSpans.snapshot(), s.bounds.Bottom(), s.clamped
				return struct{}{}
			})
			return spans, bottom, clamped
		}()
		clipped := contentBottom > bounds.Bottom()+1
		if clamped != clipped {
			state.Update(app, func(s *TextViewState, cx *gpui.Context[TextViewState]) {
				s.clamped = clipped
				cx.Notify()
			})
		}
		if clipped {
			if clip, ok := lineSafeClipBottom(spans, bounds.Bottom(), contentBottom); ok {
				clipBottom = &clip
			}
		}
	}
	hitbox := w.InsertHitbox(bounds, gpui.HitboxNormal)
	// Over the text, so after its hitbox (ticket 29).
	var touchHandles TouchHandleLayout
	if v.selectable {
		touchHandles = textViewPrepaintTouchHandles(state, w, app)
	}
	return textViewPrepaintState{hitbox: hitbox, clipBottom: clipBottom, touchHandles: touchHandles}
}

// Paint paints the document with the view's state on the stack, clipped to
// the last whole line under max_lines (U:text/text_view.rs:781-857).
func (v *TextView) Paint(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, bounds gpui.Bounds, layout *textViewLayoutState, prepaint *textViewPrepaintState, w *gpui.Window, app *gpui.App) {
	if v.selectable {
		textViewSelectionBeginFrame(layout.state, app)
	}
	textViewStatePush(app, layout.state)
	if prepaint.clipBottom != nil {
		mask := textBoundsFromCorners(bounds.Origin, gpui.Point{X: bounds.Right(), Y: *prepaint.clipBottom})
		gpui.WithContentMaskVoid(w, &mask, func(w *gpui.Window) { layout.element.Paint(w, app) })
	} else {
		layout.element.Paint(w, app)
	}
	textViewStatePop(app)
	textViewRevealProgress(layout.state, v.revealHandler, w, app)
	if v.selectable {
		// Registration and touch handles (ticket 29,
		// text_selection_adapter.go).
		textViewSelectionPaint(layout.state, prepaint.hitbox, &prepaint.touchHandles, w, app)
	}
}

// Text is a plain string or a TextView (U:text/mod.rs:48-103).
type Text struct {
	str  string
	view *TextView
}

// TextOfString is From<&str> for Text.
func TextOfString(s string) Text { return Text{str: s} }

// TextOfView is From<TextView> for Text.
func TextOfView(v *TextView) Text { return Text{view: v} }

// Style sets the TextViewStyle; a string ignores it (U:text/mod.rs:79-88).
func (t Text) Style(style TextViewStyle) Text {
	if t.view != nil {
		t.view = t.view.Style(style)
	}
	return t
}

// GetText is the text content (U:text/mod.rs:89-102).
func (t Text) GetText(app *gpui.App) string {
	if t.view == nil {
		return t.str
	}
	if t.view.state == nil {
		return ""
	}
	return t.view.state.Read(app, func(s *TextViewState, _ *gpui.App) string { return s.source() })
}

// IntoElement renders the string or the view (U:text/mod.rs:105-112).
func (t Text) IntoElement() gpui.AnyElement {
	if t.view != nil {
		return t.view.IntoElement()
	}
	return gpui.Text(t.str)
}

// Markdown creates a Markdown text view whose id is its call site
// (U:text/mod.rs:34-39).
func Markdown(source string) *TextView { return TextViewMarkdown(callerElementID(1), source) }

// HTML creates an HTML text view whose id is its call site
// (U:text/mod.rs:41-46).
func HTML(source string) *TextView { return TextViewHTML(callerElementID(1), source) }
