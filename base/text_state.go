package base

// Port of U:text/state.rs (ticket 28): TextViewState — the parsed
// document of a TextView, the streaming parser, select-all and
// multi-click selection state, max_lines bookkeeping and the render of
// the document — plus the text init (U:text/mod.rs:30-32).
//
// Go adaptations (recorded deviations):
//
//   - The background parser (U:text/state.rs:1117-1281 UpdateFuture over
//     async channels) is a mutex-guarded queue drained by a delivering
//     task (App.SpawnDeliveringInto): updates are taken in send order,
//     coalesced 64 at a time (MAX_COALESCED_UPDATES_PER_PARSE), and each
//     coalesced batch delivers one parsedUpdate, applied on the entity
//     like the Rust receive loop.
//   - Ticket 30's state (range highlights, reveal, stream fade, rendered
//     text, U:text/state.rs:586-700,959-1001) is not carried; only the
//     committed revision the range-highlight reconcile records is.
//   - Ticket 29's selection adapter (U:text/selection_adapter.rs) and the
//     window TextSelection layer join through the hooks below; without
//     them a TextView has no window drag selection, as before 29.
//   - The text-view state stack (GlobalState::text_view_state_stack,
//     U:global_state.rs:23,96-98) is this file's textViewStateStack global.

import (
	"strings"
	"sync"

	"github.com/hoangvu12/gpui-go/base/segment"
	"github.com/hoangvu12/gpui-go/gpui"
)

// textViewContext is the key context of a TextView (U:text/state.rs:39).
const textViewContext = "TextView"

// maxCoalescedUpdatesPerParse keeps coalescing bounded so sustained
// streams still render intermediate updates (U:text/state.rs:40-41).
const maxCoalescedUpdatesPerParse = 64

// maxSyncFullReplaceBytes bounds the source parsed synchronously on the UI
// thread (U:text/state.rs:42-44).
const maxSyncFullReplaceBytes = 4 * 1024

// textInit binds the TextView copy and select-all keys
// (U:text/state.rs:48-59, U:text/mod.rs:30-32). Init (lib.go) joins it
// with this ticket.
func textInit(app *gpui.App) {
	ctx := textViewContext
	if textIsMacOS() {
		app.BindKeys(
			gpui.NewKeyBinding("cmd-c", CopyAction.Box(Copy{}), &ctx),
			gpui.NewKeyBinding("cmd-a", SelectAllAction.Box(SelectAll{}), &ctx),
		)
		return
	}
	app.BindKeys(
		gpui.NewKeyBinding("ctrl-c", CopyAction.Box(Copy{}), &ctx),
		gpui.NewKeyBinding("ctrl-a", SelectAllAction.Box(SelectAll{}), &ctx),
	)
}

// textViewFormat is the content format of the text view
// (U:text/state.rs:61-68).
type textViewFormat int

const (
	textViewFormatMarkdown textViewFormat = iota
	textViewFormatHTML
)

// textLineSpan is one text element's laid-out vertical extent, reported
// during prepaint so a max_lines TextView can snap its clip to a whole
// line (U:text/state.rs:86-94).
type textLineSpan struct {
	top, bottom, lineHeight float32
}

// textLineSpans is the shared Arc<Mutex<Vec<LineSpan>>>.
type textLineSpans struct {
	mu    sync.Mutex
	spans []textLineSpan
}

func (l *textLineSpans) push(span textLineSpan) {
	l.mu.Lock()
	l.spans = append(l.spans, span)
	l.mu.Unlock()
}

func (l *textLineSpans) clear() {
	l.mu.Lock()
	l.spans = l.spans[:0]
	l.mu.Unlock()
}

func (l *textLineSpans) snapshot() []textLineSpan {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]textLineSpan(nil), l.spans...)
}

// textViewMultiClickKind is U:text/state.rs:952-957.
type textViewMultiClickKind int

const (
	textViewMultiClickWord textViewMultiClickKind = iota
	textViewMultiClickParagraph
	textViewMultiClickLine
)

// textViewMultiClickSelection is U:text/state.rs:945-950.
type textViewMultiClickSelection struct {
	pos        gpui.Point
	kind       textViewMultiClickKind
	lineBounds *gpui.Bounds
}

// ---------------------------------------------------------------------------
// The text-view state stack (U:global_state.rs:23,96-98)
// ---------------------------------------------------------------------------

// textViewStateStack is GlobalState::text_view_state_stack: the TextView
// states being painted (or prepainted with max_lines), innermost last.
type textViewStateStack struct {
	states []gpui.Entity[TextViewState]
}

func textViewStack(app *gpui.App) *textViewStateStack {
	if s, ok := gpui.TryGlobal[*textViewStateStack](app); ok {
		return s
	}
	s := &textViewStateStack{}
	app.SetGlobal(s)
	return s
}

func textViewStatePush(app *gpui.App, state gpui.Entity[TextViewState]) {
	s := textViewStack(app)
	s.states = append(s.states, state)
}

func textViewStatePop(app *gpui.App) {
	s := textViewStack(app)
	if n := len(s.states); n > 0 {
		s.states = s.states[:n-1]
	}
}

// textViewStateCurrent is GlobalState::text_view_state
// (U:global_state.rs:96-98): the innermost state.
func textViewStateCurrent(app *gpui.App) (gpui.Entity[TextViewState], bool) {
	s, ok := gpui.TryGlobal[*textViewStateStack](app)
	if !ok || len(s.states) == 0 {
		return gpui.Entity[TextViewState]{}, false
	}
	return s.states[len(s.states)-1], true
}

// textViewReportLineSpan pushes a span to the current view when it clamps
// (U:text/inline.rs:908-923, U:text/inline_object.rs:274-285).
func textViewReportLineSpan(app *gpui.App, span textLineSpan) {
	view, ok := textViewStateCurrent(app)
	if !ok {
		return
	}
	view.Read(app, func(s *TextViewState, _ *gpui.App) struct{} {
		if s.maxLines != nil {
			s.lineSpans.push(span)
		}
		return struct{}{}
	})
}

// ---------------------------------------------------------------------------
// Ticket 29 hooks
// ---------------------------------------------------------------------------

// textViewSelectionAdapter is the part of U:text/selection_adapter.rs
// TextViewSelectionAdapter this ticket's code calls.
type textViewSelectionAdapter interface {
	// setLocalSelection is set_local_selection.
	setLocalSelection(selected bool, app *gpui.App)
	// selectionPoints is selection_points: the window selection's anchor
	// and cursor, when this view takes part in it.
	selectionPoints(app *gpui.App) (gpui.Point, gpui.Point, bool)
}

var (
	// newTextViewSelectionAdapter is TextViewSelectionAdapter::new
	// (ticket 29); nil leaves views without window selection. owner (the
	// state's dependent scope) owns the adapter's selection participant.
	newTextViewSelectionAdapter func(state gpui.WeakEntity[TextViewState], owner *gpui.Scope, app *gpui.App) textViewSelectionAdapter
	// textSelectionRegisterInline is the adapter's register_inline +
	// register_text_run for a painted inline (U:text/inline.rs:1005-1022).
	textSelectionRegisterInline func(view gpui.Entity[TextViewState], lineBounds []gpui.Bounds, text string, layout *gpui.TextLayout, bounds gpui.Bounds, app *gpui.App)
	// textSelectionEnd is TextSelection::end (U:text_selection.rs).
	textSelectionEnd func(w *gpui.Window, app *gpui.App)
	// textSelectionSelectedText is TextSelection::selected_text.
	textSelectionSelectedText func(w *gpui.Window, app *gpui.App) string
	// textWordRangeAt is U:text/selection.rs::word_range_at (ticket 29),
	// which clips the offset and delegates to text_boundary.rs; until 29
	// replaces it, it is base/segment.WordRangeAt (that text_boundary port).
	textWordRangeAt = func(text string, offset int) (gpui.TextRange, bool) {
		if text == "" {
			return gpui.TextRange{}, false
		}
		start, end, ok := segment.WordRangeAt(text, min(offset, len(text)))
		return gpui.TextRange{Start: start, End: end}, ok
	}
)

func textSelectionEndHook(w *gpui.Window, app *gpui.App) {
	if textSelectionEnd != nil {
		textSelectionEnd(w, app)
	}
}

// ---------------------------------------------------------------------------
// TextViewState (U:text/state.rs:96-943)
// ---------------------------------------------------------------------------

// TextViewState is the state of a TextView (U:text/state.rs:96-158).
type TextViewState struct {
	entityID    gpui.EntityID
	focusHandle gpui.FocusHandle
	listState   *gpui.ListState

	bounds gpui.Bounds

	selectable      bool
	selectionFormat SelectionFormat
	scrollable      bool
	maxLines        *int
	lineSpans       *textLineSpans
	clamped         bool
	textViewStyle   *TextViewStyle

	codeBlockActions     CodeBlockActionsFn
	codeBlockHighlighter *CodeBlockHighlighter
	tableActions         TableActionsFn
	imageSource          ImageSourceFn
	linkClickHandler     LinkClickHandlerFn
	markdownExtensions   *MarkdownExtensions

	isSelecting             bool
	preserveInlineSelection bool
	multiClick              *textViewMultiClickSelection
	selectedTextOverride    *string
	selectAll               bool
	autoScroll              AutoScroll
	selectionAdapter        textViewSelectionAdapter

	parsedContent parsedContent
	format        textViewFormat
	text          string
	// elementText is the text a TextView element last handed over.
	elementText *string

	revision               int
	committedRevision      int
	fullUpdateRevision     int
	selectionRevision      int
	compatibleLayoutUpdate bool
	layoutTypography       *textLayoutTypography
	parsedError            *string
	parser                 *textViewParser
}

// textLayoutTypography is the (TextStyle, rem) pair a layout was measured
// with (U:text/state.rs:153,1005).
type textLayoutTypography struct {
	style gpui.TextStyle
	rem   float32
}

// parsedContent is U:text/state.rs:1111-1115.
type parsedContent struct {
	document *parsedDocument
	nodeCx   nodeContext
}

func emptyParsedContent() parsedContent { return parsedContent{document: &parsedDocument{}} }

// TextViewStateMarkdown creates a Markdown TextViewState
// (U:text/state.rs:161-164).
func TextViewStateMarkdown(text string, cx *gpui.Context[TextViewState]) TextViewState {
	return newTextViewState(textViewFormatMarkdown, text, cx)
}

// TextViewStateHTML creates an HTML TextViewState (U:text/state.rs:166-169).
// HTML parsing is ticket 30's (U:text/format/html.rs); until it lands the
// state reports a parse error.
func TextViewStateHTML(text string, cx *gpui.Context[TextViewState]) TextViewState {
	return newTextViewState(textViewFormatHTML, text, cx)
}

// newTextViewState is TextViewState::new (U:text/state.rs:171-285).
func newTextViewState(format textViewFormat, text string, cx *gpui.Context[TextViewState]) TextViewState {
	style := DefaultTextViewStyle()
	s := TextViewState{
		entityID:    cx.EntityID(),
		focusHandle: cx.App().FocusHandle(),
		// Measure all blocks so the scrollbar thumb stays stable.
		listState:          gpui.NewListState(0, 1000).MeasureAll(),
		lineSpans:          &textLineSpans{},
		textViewStyle:      &style,
		markdownExtensions: &MarkdownExtensions{},
		parsedContent:      emptyParsedContent(),
		format:             format,
		text:               text,
		parser:             &textViewParser{format: format, content: emptyParsedContent()},
	}
	if newTextViewSelectionAdapter != nil {
		s.selectionAdapter = newTextViewSelectionAdapter(cx.Entity().Downgrade(), cx.Scope(), cx.App())
	}
	s.incrementUpdate(text, false, cx)
	return s
}

// source is the text content (U:text/state.rs:287-290).
func (s *TextViewState) source() string { return s.parsedContent.document.source }

// SetSelectable sets whether the text is selectable
// (U:text/state.rs:298-302); the builder form is Selectable.
func (s *TextViewState) SetSelectable(selectable bool, cx *gpui.Context[TextViewState]) {
	s.selectable = selectable
	cx.Notify()
}

// Selectable is the builder TextViewState::selectable
// (U:text/state.rs:292-296).
func (s TextViewState) Selectable(selectable bool) TextViewState { s.selectable = selectable; return s }

// SelectionFormat is the builder TextViewState::selection_format
// (U:text/state.rs:304-308).
func (s TextViewState) SelectionFormat(format SelectionFormat) TextViewState {
	s.selectionFormat = format
	return s
}

// SetSelectionFormat sets the SelectionFormat (U:text/state.rs:310-318).
func (s *TextViewState) SetSelectionFormat(format SelectionFormat, cx *gpui.Context[TextViewState]) {
	s.selectionFormat = format
	cx.Notify()
}

// Scrollable is the builder TextViewState::scrollable
// (U:text/state.rs:320-324).
func (s TextViewState) Scrollable(scrollable bool) TextViewState { s.scrollable = scrollable; return s }

// SetScrollable sets whether the view scrolls internally
// (U:text/state.rs:326-333).
func (s *TextViewState) SetScrollable(scrollable bool, cx *gpui.Context[TextViewState]) {
	if !scrollable {
		s.resetSelectionAndAdapter(cx.App())
	}
	s.scrollable = scrollable
	cx.Notify()
}

// IsClamped reports whether the last painted frame clipped content because
// of TextView.MaxLines (U:text/state.rs:335-339).
func (s *TextViewState) IsClamped() bool { return s.clamped }

// SetText sets the text content: Markdown that extends the current
// non-empty text is appended like PushStr, keeping the selection; any other
// text replaces it (U:text/state.rs:341-368).
func (s *TextViewState) SetText(text string, cx *gpui.Context[TextViewState]) {
	if s.text == text {
		return
	}
	if s.format == textViewFormatMarkdown && s.parsedError == nil && s.text != "" && strings.HasPrefix(text, s.text) {
		s.PushStr(text[len(s.text):], cx)
		return
	}
	s.text = text
	s.parsedError = nil
	s.incrementUpdate(text, false, cx)
}

// setElementText is SetText for the text a TextView element hands over
// every frame: the same string as last time is recognized without a
// comparison (U:text/state.rs:370-383). Go strings carry no allocation
// identity the way SharedString's Arc does, so "the same string" is the
// same data pointer and length.
func (s *TextViewState) setElementText(text string, cx *gpui.Context[TextViewState]) {
	if s.elementText != nil && sameStringData(*s.elementText, text) && len(s.text) == len(text) {
		return
	}
	s.elementText = &text
	s.SetText(text, cx)
}

// PushStr appends partial text content (U:text/state.rs:385-393).
func (s *TextViewState) PushStr(newText string, cx *gpui.Context[TextViewState]) {
	if newText == "" {
		return
	}
	s.text += newText
	s.incrementUpdate(newText, true, cx)
}

// setMarkdownExtensions installs a registry, reparsing when the parser
// configuration changed (U:text/state.rs:406-423).
func (s *TextViewState) setMarkdownExtensions(extensions *MarkdownExtensions, cx *gpui.Context[TextViewState]) {
	if s.markdownExtensions.revisionValue() == extensions.revisionValue() {
		return
	}
	changed := !s.markdownExtensions.hasSameParserConfiguration(extensions)
	s.markdownExtensions = extensions
	if changed && s.format == textViewFormatMarkdown {
		s.incrementUpdate(s.text, false, cx)
	}
}

// SelectedText is the selected text, in the view's SelectionFormat
// (U:text/state.rs:425-428).
func (s *TextViewState) SelectedText() string { return s.selectedTextIn(nil) }

// SelectedSourceRange is the Markdown source byte range of the rendered
// selection; false for HTML views and unmapped selections
// (U:text/state.rs:430-448).
func (s *TextViewState) SelectedSourceRange() (gpui.TextRange, bool) {
	if s.format != textViewFormatMarkdown {
		return gpui.TextRange{}, false
	}
	if s.selectAll {
		return gpui.TextRange{Start: 0, End: len(s.source())}, true
	}
	return s.parsedContent.document.selectedSourceRange()
}

// effectiveFormat is the format to copy in: Plain whenever the requested
// one cannot be produced (U:text/state.rs:450-464).
func (s *TextViewState) effectiveFormat() SelectionFormat {
	if s.format == textViewFormatMarkdown {
		return s.selectionFormat
	}
	return SelectionFormatPlain
}

// selectedTextIn is the selected text, blocks bounding the top-level
// blocks the selection covers (U:text/state.rs:466-494).
func (s *TextViewState) selectedTextIn(blocks *[2]int) string {
	format := s.effectiveFormat()
	if s.selectAll {
		if format == SelectionFormatSource {
			return s.source()
		}
		return s.parsedContent.document.text()
	}
	if format != SelectionFormatSource && s.selectedTextOverride != nil {
		return *s.selectedTextOverride
	}
	return s.parsedContent.document.selectedText(format, blocks)
}

// invalidateMeasuredHeights forces a full re-measure of the block list
// (U:text/state.rs:496-516).
func (s *TextViewState) invalidateMeasuredHeights() {
	if count := s.listState.ItemCount(); count > 0 {
		s.listState.RemeasureItems(0, count)
	}
}

// InvalidateInlineLayout remeasures inline resources after their prepared
// content or metrics change, without reparsing; the logical selection is
// retained until the next gesture (U:text/state.rs:518-528).
func (s *TextViewState) InvalidateInlineLayout(cx *gpui.Context[TextViewState]) {
	s.preserveInlineSelection = true
	s.compatibleLayoutUpdate = true
	s.invalidateMeasuredHeights()
	cx.Notify()
}

// incrementUpdate queues a parse of text, parsing small full replacements
// synchronously (U:text/state.rs:530-584).
func (s *TextViewState) incrementUpdate(text string, appendText bool, cx *gpui.Context[TextViewState]) {
	s.revision++
	if !appendText {
		s.fullUpdateRevision = s.revision
		s.selectionRevision++
	}
	sync := !appendText && len(text) <= maxSyncFullReplaceBytes
	mode := textParseReplace
	switch {
	case appendText:
		mode = textParseCompatible
	case sync:
		mode = textParseBaselineAck
	}
	options := textUpdateOptions{
		revision:           s.revision,
		appendText:         appendText,
		mode:               mode,
		pendingText:        text,
		markdownExtensions: s.markdownExtensions,
	}
	if sync {
		content, err := parseTextContent(s.format, emptyParsedContent(), options)
		if err == nil {
			s.committedRevision = s.revision
			s.parsedContent = content
			s.parsedError = nil
			s.invalidateMeasuredHeights()
			if !s.isSelecting {
				s.resetSelectionAndAdapter(cx.App())
			}
		} else {
			msg := err.Error()
			s.parsedError = &msg
		}
		// Keep the background parser's document in sync so a later append
		// extends this baseline.
		s.parser.send(options, cx)
		cx.Notify()
		return
	}
	s.parser.send(options, cx)
}

// applyParsedUpdate is the receive loop body (U:text/state.rs:180-227).
func (s *TextViewState) applyParsedUpdate(update textParsedUpdate, cx *gpui.Context[TextViewState]) {
	if update.revision != s.revision || update.baselineAck {
		return
	}
	if update.err == nil {
		appendText := update.selectionCompatible && !update.fullParse
		if appendText && s.fullUpdateRevision <= s.committedRevision {
			s.spliceAppendedBlocks(update.content.document)
		}
		s.committedRevision = update.revision
		s.parsedContent = update.content
		s.parsedError = nil
		s.compatibleLayoutUpdate = update.selectionCompatible
		if update.fullParse {
			s.invalidateMeasuredHeights()
		}
	} else {
		msg := update.err.Error()
		s.parsedError = &msg
	}
	if !update.selectionCompatible && !s.isSelecting {
		s.resetSelectionAndAdapter(cx.App())
	}
	cx.Notify()
}

// spliceAppendedBlocks tells the scrollable list which blocks an append
// replaced (U:text/state.rs:702-736).
func (s *TextViewState) spliceAppendedBlocks(next *parsedDocument) {
	old := s.parsedContent.document.blocks
	oldCount, newCount := len(old), len(next.blocks)
	if oldCount == 0 || newCount <= oldCount || s.listState.ItemCount() != oldCount {
		return
	}
	unchanged := 0
	for i := 0; i < oldCount-1; i++ {
		span := blockNodeSpan(old[i])
		if span == nil {
			break
		}
		ns := blockNodeSpan(next.blocks[i])
		if ns == nil || *ns != *span {
			break
		}
		unchanged++
	}
	s.listState.RemeasureItems(unchanged, oldCount)
	s.listState.Splice(oldCount, oldCount, newCount-oldCount)
}

// updateBounds saves the bounds (U:text/state.rs:738-741).
func (s *TextViewState) updateBounds(bounds gpui.Bounds) { s.bounds = bounds }

// Bounds is the view's bounds (U:text/state.rs:769-772).
func (s *TextViewState) Bounds() gpui.Bounds { return s.bounds }

// ListState is the virtualized block list (U:text/state.rs:774-777).
func (s *TextViewState) ListState() *gpui.ListState { return s.listState }

// IsSelecting reports a drag selection in progress (U:text/state.rs:779-782).
func (s *TextViewState) IsSelecting() bool { return s.isSelecting }

// FocusHandle is the view's focus handle (U:text/state.rs:784-787).
func (s *TextViewState) FocusHandle() gpui.FocusHandle { return s.focusHandle }

// hasViewSelection reports a view-local selection
// (U:text/state.rs:789-795).
func (s *TextViewState) hasViewSelection() bool {
	return s.selectAll || s.multiClick != nil || s.selectedTextOverride != nil
}

// resetSelection is U:text/state.rs:801-812.
func (s *TextViewState) resetSelection() {
	s.preserveInlineSelection = false
	s.multiClick = nil
	s.selectedTextOverride = nil
	s.selectAll = false
	s.isSelecting = false
	s.autoScroll.Stop()
	s.parsedContent.document.clearSelection()
}

func (s *TextViewState) resetSelectionAndAdapter(app *gpui.App) {
	s.resetSelection()
	if s.selectionAdapter != nil {
		s.selectionAdapter.setLocalSelection(false, app)
	}
}

// ClearSelection clears the current text selection
// (U:text/state.rs:819-823).
func (s *TextViewState) ClearSelection(cx *gpui.Context[TextViewState]) {
	s.resetSelectionAndAdapter(cx.App())
	cx.Notify()
}

// scrollOffset is U:text/state.rs:825-831.
func (s *TextViewState) scrollOffset() gpui.Point {
	if s.scrollable {
		return s.listState.ScrollPxOffsetForScrollbar()
	}
	return gpui.Point{}
}

// SelectAll selects all rendered text in this view
// (U:text/state.rs:833-842).
func (s *TextViewState) SelectAll(cx *gpui.Context[TextViewState]) {
	s.multiClick = nil
	s.selectedTextOverride = nil
	s.selectAll = true
	s.isSelecting = false
	s.autoScroll.Stop()
	if s.selectionAdapter != nil {
		s.selectionAdapter.setLocalSelection(true, cx.App())
	}
	cx.Notify()
}

// setMultiClickSelection records a double/triple click
// (U:text/state.rs:844-864).
func (s *TextViewState) setMultiClickSelection(pos gpui.Point, kind textViewMultiClickKind, selectedText string, app *gpui.App) {
	s.preserveInlineSelection = false
	local := textPointSub(textPointSub(pos, s.bounds.Origin), s.scrollOffset())
	s.multiClick = &textViewMultiClickSelection{pos: local, kind: kind}
	s.selectedTextOverride = &selectedText
	s.selectAll = false
	s.isSelecting = false
	s.autoScroll.Stop()
	if s.selectionAdapter != nil {
		s.selectionAdapter.setLocalSelection(true, app)
	}
}

// setMultiClickLine records a triple click on a mixed line
// (U:text/state.rs:866-878).
func (s *TextViewState) setMultiClickLine(bounds gpui.Bounds, app *gpui.App) {
	s.setMultiClickSelection(bounds.Center(), textViewMultiClickLine, "", app)
	s.selectedTextOverride = nil
	offset := textPointAdd(s.bounds.Origin, s.scrollOffset())
	if s.multiClick != nil {
		lb := gpui.Bounds{Origin: textPointSub(bounds.Origin, offset), Size: bounds.Size}
		s.multiClick.lineBounds = &lb
	}
}

// selectionPoints is the window selection in window coordinates when this
// view takes part in it (U:text/state.rs:887-898).
func (s *TextViewState) selectionPoints(app *gpui.App) (gpui.Point, gpui.Point, bool) {
	if !s.selectable || s.selectionAdapter == nil {
		return gpui.Point{}, gpui.Point{}, false
	}
	return s.selectionAdapter.selectionPoints(app)
}

// hasSelection is U:text/state.rs:900-902.
func (s *TextViewState) hasSelection(app *gpui.App) bool {
	if s.hasViewSelection() {
		return true
	}
	_, _, ok := s.selectionPoints(app)
	return ok
}

// onActionSelectAll is U:text/state.rs:904-916.
func (s *TextViewState) onActionSelectAll(_ *SelectAll, _ *gpui.Window, cx *gpui.Context[TextViewState]) {
	if !s.selectable {
		cx.Propagate()
		return
	}
	s.SelectAll(cx)
}

func (s *TextViewState) isSelectable() bool  { return s.selectable }
func (s *TextViewState) isAllSelected() bool { return s.selectAll }

// multiClickSelection is the multi-click selection in window coordinates
// (U:text/state.rs:926-942).
func (s *TextViewState) multiClickSelection() *textViewMultiClickSelection {
	if s.multiClick == nil {
		return nil
	}
	offset := textPointAdd(s.scrollOffset(), s.bounds.Origin)
	out := *s.multiClick
	out.pos = textPointAdd(out.pos, offset)
	if out.lineBounds != nil {
		lb := gpui.Bounds{Origin: textPointAdd(out.lineBounds.Origin, offset), Size: out.lineBounds.Size}
		out.lineBounds = &lb
	}
	return &out
}

// Render renders the parsed document (U:text/state.rs:1003-1109).
func (s *TextViewState) Render(w *gpui.Window, cx *gpui.Context[TextViewState]) gpui.AnyElement {
	typography := textLayoutTypography{style: gpui.WindowTextStyle(w), rem: gpui.RemSize(w)}
	if s.layoutTypography != nil && !textLayoutTypographyEqual(*s.layoutTypography, typography) {
		// An inherited font or rem change affects offscreen inline metrics.
		s.preserveInlineSelection = true
		s.compatibleLayoutUpdate = true
		s.invalidateMeasuredHeights()
	}
	s.layoutTypography = &typography
	state := cx.Entity().Downgrade()
	nodeCx := &nodeRenderContext{
		offset:               s.parsedContent.nodeCx.offset,
		linkRefs:             s.parsedContent.nodeCx.linkRefs,
		style:                s.textViewStyle,
		codeBlockActions:     s.codeBlockActions,
		codeBlockHighlighter: s.codeBlockHighlighter,
		tableActions:         s.tableActions,
		linkClickHandler:     s.linkClickHandler,
		imageSource:          s.imageSource,
		markdownExtensions:   s.markdownExtensions,
	}

	content := VFlex().WFull()
	if s.maxLines == nil {
		content = content.HFull()
	}
	if s.parsedError == nil {
		var list *gpui.ListState
		if s.scrollable {
			list = s.listState
		}
		content = content.Child(s.parsedContent.document.renderRoot(list, nodeCx, w, cx.App()))
	} else {
		content = content.Child(VFlex().Gap1().Child("Failed to parse content").Child(*s.parsedError))
	}
	// The bounds, and the window selection clear when the layout changed
	// under it (U:text/state.rs:1066-1101; ticket 29, text_selection_adapter.go).
	return ElementExtOf(content).OnPrepaint(func(bounds gpui.Bounds, w *gpui.Window, app *gpui.App) {
		textViewSelectionPrepaint(state, bounds, w, app)
	}).IntoElement()
}

func textLayoutTypographyEqual(a, b textLayoutTypography) bool {
	return a.rem == b.rem && textStyleEqual(a.style, b.style)
}

// ---------------------------------------------------------------------------
// The parser (U:text/state.rs:1117-1281)
// ---------------------------------------------------------------------------

// textParseMode is U:text/state.rs:1204-1209.
type textParseMode int

const (
	textParseBaselineAck textParseMode = iota
	textParseReplace
	textParseCompatible
)

// textUpdateOptions is U:text/state.rs:1173-1180.
type textUpdateOptions struct {
	revision           int
	pendingText        string
	appendText         bool
	mode               textParseMode
	markdownExtensions *MarkdownExtensions
}

// merge folds next into o (U:text/state.rs:1182-1194).
func (o *textUpdateOptions) merge(next textUpdateOptions) {
	if next.appendText {
		o.pendingText += next.pendingText
		o.revision = next.revision
		if o.mode != textParseReplace {
			o.mode = textParseCompatible
		}
		return
	}
	*o = next
}

// textParsedUpdate is U:text/state.rs:1196-1202.
type textParsedUpdate struct {
	revision            int
	fullParse           bool
	selectionCompatible bool
	baselineAck         bool
	content             parsedContent
	err                 error
}

// textViewParser is the background parser: the accumulated document and
// the queued updates.
type textViewParser struct {
	mu      sync.Mutex
	format  textViewFormat
	content parsedContent
	pending []textUpdateOptions
}

// send queues an update and spawns the drain that parses it.
func (p *textViewParser) send(options textUpdateOptions, cx *gpui.Context[TextViewState]) {
	p.mu.Lock()
	p.pending = append(p.pending, options)
	p.mu.Unlock()
	app := cx.App()
	app.SpawnDeliveringInto(cx.Scope(), cx.Entity(), func(*gpui.TaskRun) []textParsedUpdate {
		return p.drain()
	}, func(s *TextViewState, updates []textParsedUpdate, cx *gpui.Context[TextViewState]) {
		for _, u := range updates {
			s.applyParsedUpdate(u, cx)
		}
	})
}

// drain parses every queued update, coalescing at most
// maxCoalescedUpdatesPerParse per parse (U:text/state.rs:1139-1171,
// 1211-1225).
func (p *textViewParser) drain() []textParsedUpdate {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []textParsedUpdate
	for len(p.pending) > 0 {
		options := p.pending[0]
		n := 1
		for n < len(p.pending) && n < maxCoalescedUpdatesPerParse {
			options.merge(p.pending[n])
			n++
		}
		p.pending = p.pending[n:]
		content, err := parseTextContent(p.format, p.content, options)
		if err == nil {
			p.content = content
		}
		out = append(out, textParsedUpdate{
			revision:            options.revision,
			fullParse:           !options.appendText,
			selectionCompatible: options.mode == textParseCompatible,
			baselineAck:         options.mode == textParseBaselineAck,
			content:             content,
			err:                 err,
		})
	}
	return out
}

// parseTextContent parses an update into content: an append reparses the
// last block together with the appended text (U:text/state.rs:1227-1281).
func parseTextContent(format textViewFormat, content parsedContent, options textUpdateOptions) (parsedContent, error) {
	nodeCx := nodeContext{markdownExtensions: options.markdownExtensions}
	var lastSpan *Span
	if options.appendText && len(content.document.blocks) > 0 {
		lastSpan = blockNodeSpan(content.document.blocks[len(content.document.blocks)-1])
	}
	blocks := content.document.blocks
	var source string
	if lastSpan != nil {
		blocks = blocks[:len(blocks)-1]
		nodeCx.offset = lastSpan.Start
		source = content.document.source[lastSpan.Start:] + options.pendingText
	} else {
		if options.appendText {
			nodeCx.offset = len(content.document.source)
		}
		source = options.pendingText
	}
	var next *parsedDocument
	var err error
	if format == textViewFormatMarkdown {
		next, err = markdownParse(source, &nodeCx)
	} else {
		next, err = markdownHTMLParse(source, &nodeCx)
	}
	if err != nil {
		return content, err
	}
	if options.appendText {
		merged := make([]blockNode, 0, len(blocks)+len(next.blocks))
		merged = append(append(merged, blocks...), next.blocks...)
		content.document = &parsedDocument{source: content.document.source + options.pendingText, blocks: merged}
	} else {
		content.document = next
	}
	return content, nil
}
