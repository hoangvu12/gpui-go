package gpui

// The editor-side UTF-16 document/selection/marked-range model
// (ticket20), ported from the pinned CE reference
// 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a:
//
//   - crates/gpui_elements/src/editable_text/state.rs — the
//     EditableTextState members the IME handler surface consumes:
//     ime_resolve_range (the fallback chain IME range -> marked range
//     -> selection, clamped to storage), ime_mark_text_in_range,
//     ime_mark_selected_range (composition selections are relative to
//     the inserted text, in UTF-16 units), replace_text (selection
//     affinity from the inserted text, marked range cleared),
//     validate_incoming_text (single-line fields strip CR/LF), and the
//     EntityInputHandler impl (text_for_range with the adjusted range,
//     selected_text_range with the reversed flag, marked_text_range,
//     unmark_text, replace_text_in_range,
//     replace_and_mark_text_in_range, bounds_for_range,
//     character_index_for_point).
//   - crates/gpui_elements/src/editable_text/storage.rs —
//     StringStorage's version counter and the UTF-8/UTF-16 conversion
//     plumbing (utf_range_16to8 / utf_range_8to16).
//   - crates/gpui/src/text_system/line_layout.rs — CaretPosition
//     (byte index + affinity), CaretSelection (anchor/caret, either
//     endpoint may be the logical start), CaretAffinity::
//     for_inserted_text (hard line breaks place the new caret
//     downstream), and visual_position_for_caret (caret_bounds'
//     origin).
//
// Text geometry comes from the EXISTING text system (text.go's
// WrappedLine; read-only for this ticket): the editor re-shapes its
// document through a layout provider when the storage version changes,
// exactly like the reference's current_document() version check. The
// history/undo subsystem (editable_text/history.rs), movements,
// multi-selection drag state and the element rendering are other
// tickets; this model is the bounded IME slice.
//
// Units: document storage is a UTF-8 Go string; all IME-facing ranges
// are UTF-16 code units converted through the input.rs helpers in
// ime.go. A surrogate pair or grapheme is never cut: mid-character
// UTF-16 offsets snap to character ends and the clamped ranges stay on
// byte boundaries.

import "strings"

// ---------------------------------------------------------------------------
// Caret positions and selections (line_layout.rs CaretPosition /
// CaretSelection)
// ---------------------------------------------------------------------------

// EditorCaretPosition is a caret's logical location: a UTF-8 byte
// index plus the affinity that owns it at a shared boundary
// (CaretPosition; downstream attaches to the following cluster,
// upstream to the preceding one).
type EditorCaretPosition struct {
	// Index is the caret's UTF-8 byte index.
	Index int
	// Affinity selects the logical neighbor that owns this position.
	Affinity CaretAffinity
}

// EditorCaretAttachedToNext builds a caret attached to the next
// cluster (CaretPosition::attached_to_next_cluster).
func EditorCaretAttachedToNext(index int) EditorCaretPosition {
	return EditorCaretPosition{Index: index, Affinity: CaretAffinityDownstream}
}

// EditorCaretAttachedToPrevious builds a caret attached to the
// previous cluster (CaretPosition::attached_to_previous_cluster).
func EditorCaretAttachedToPrevious(index int) EditorCaretPosition {
	return EditorCaretPosition{Index: index, Affinity: CaretAffinityUpstream}
}

// EditorSelection is an affinity-aware selection (CaretSelection): a
// fixed anchor and an active caret; either endpoint may be the logical
// start or end of the selected byte range.
type EditorSelection struct {
	// Anchor is the fixed endpoint.
	Anchor EditorCaretPosition
	// Caret is the active endpoint; it equals Anchor for an empty
	// selection.
	Caret EditorCaretPosition
}

// EditorSelectionAt builds an empty downstream selection at index
// (From<usize> for CaretSelection).
func EditorSelectionAt(index int) EditorSelection {
	return EditorSelection{
		Anchor: EditorCaretAttachedToNext(index),
		Caret:  EditorCaretAttachedToNext(index),
	}
}

// EditorSelectionFromRange builds a downstream selection whose caret
// is at start and anchor at end (From<Range<usize>> for
// CaretSelection).
func EditorSelectionFromRange(start, end int) EditorSelection {
	return EditorSelection{
		Anchor: EditorCaretAttachedToNext(end),
		Caret:  EditorCaretAttachedToNext(start),
	}
}

// IsEmpty reports an empty selection (anchor and caret share the byte
// index).
func (s EditorSelection) IsEmpty() bool { return s.Anchor.Index == s.Caret.Index }

// Reversed reports whether the active caret is the logical start of
// the selection (endpoint_ordering == Greater: caret before anchor).
func (s EditorSelection) Reversed() bool { return s.Caret.Index < s.Anchor.Index }

// ByteRange returns the selected UTF-8 byte range in logical order.
func (s EditorSelection) ByteRange() TextRange {
	return TextRange{
		Start: min(s.Anchor.Index, s.Caret.Index),
		End:   max(s.Anchor.Index, s.Caret.Index),
	}
}

// hardLineBreakCharacters is HARD_LINE_BREAK_CHARACTERS
// (line_layout.rs): the characters after which an inserted-text caret
// is placed downstream. Line separator U+2028 is in this set but is
// not a paragraph separator (is_paragraph_separator).
var hardLineBreakCharacters = map[rune]struct{}{
	'\n':     {},
	'\r':     {},
	'\u001c': {},
	'\u001d': {},
	'\u001e': {},
	'\u0085': {},
	'\u2028': {},
	'\u2029': {},
}

// affinityForInsertedText ports CaretAffinity::for_inserted_text: the
// caret after inserted text attaches upstream unless the text is empty
// or ends with a hard line break.
func affinityForInsertedText(text string) CaretAffinity {
	last := rune(-1)
	for _, r := range text {
		last = r
	}
	if last < 0 {
		return CaretAffinityDownstream
	}
	if _, hard := hardLineBreakCharacters[last]; hard {
		return CaretAffinityDownstream
	}
	return CaretAffinityUpstream
}

// ---------------------------------------------------------------------------
// The editor (EditableTextState's IME slice)
// ---------------------------------------------------------------------------

// EditorLayoutProvider shapes the document text into a WrappedLine
// (the native text stack through DefaultTextSystem, or a test seam).
// Returning an error leaves the editor without geometry: text and
// range operations still work, bounds/point queries report not-ok
// (current_document's Err path in the reference).
type EditorLayoutProvider func(text string) (*WrappedLine, error)

// EditorOptions configures a NewEditor.
type EditorOptions struct {
	// Font is the document's font descriptor (the default is the
	// pinned Font::default() system UI font).
	Font FontDescriptor
	// FontSize is the document's font size in pixels (16 by default).
	FontSize float32
	// LineHeight parameterizes caret/selection geometry (the reference
	// reads it from the element's layout data; DefaultLineHeight(
	// FontSize) by default).
	LineHeight float32
	// WrapWidth enables soft wrapping when non-nil.
	WrapWidth *float32
	// Multiline keeps CR/LF in incoming text; a single-line field
	// strips them (validate_incoming_text's supports_multiline branch).
	Multiline bool
	// Shape overrides the layout provider (tests inject deterministic
	// geometry); nil shapes through DefaultTextSystem.
	Shape EditorLayoutProvider
	// ElementBounds is the element's window-local bounds, used by
	// BoundsForRange (ElementInputHandler::new's element_bounds from
	// paint; the origin is the scroll/element offset).
	ElementBounds Bounds
}

// Editor is the editable-text document model the IME drives
// (EditableTextState bounded to the IME slice): a UTF-8 string with a
// version counter, an affinity-aware selection, the marked
// (composing) byte range and a shaped document layout.
//
// An Editor is safe for use on the foreground thread (like the
// reference's entity state); it implements IMEInputHandler directly
// so a window can install it with Window.SetInputHandler.
type Editor struct {
	text    string
	version uint16

	selection   EditorSelection
	markedRange *TextRange

	layout        *WrappedLine
	layoutAt      uint16
	layoutErr     error
	lineHeight    float32
	font          FontDescriptor
	fontSize      float32
	wrapWidth     *float32
	multiline     bool
	shape         EditorLayoutProvider
	elementBounds Bounds

	textChanged []func()
}

// NewEditor creates an editor holding text with the caret at caretByte
// (create_test_input's text + caret shape; the caret is a UTF-8 byte
// index). Zero options take the pinned defaults.
func NewEditor(text string, caretByte int, opts EditorOptions) *Editor {
	e := &Editor{
		text:      text,
		selection: EditorSelectionAt(min(caretByte, len(text))),
	}
	e.font = opts.Font
	if e.font.Family == "" {
		e.font = DefaultFontDescriptor()
	}
	e.fontSize = opts.FontSize
	if e.fontSize == 0 {
		e.fontSize = 16
	}
	e.lineHeight = opts.LineHeight
	if e.lineHeight == 0 {
		e.lineHeight = DefaultLineHeight(e.fontSize)
	}
	if opts.WrapWidth != nil {
		wrap := *opts.WrapWidth
		e.wrapWidth = &wrap
	}
	e.multiline = opts.Multiline
	e.shape = opts.Shape
	if e.shape == nil {
		e.shape = e.shapeWithTextSystem
	}
	e.elementBounds = opts.ElementBounds
	return e
}

// OnTextChanged registers a callback fired after every text mutation
// (state.rs emit_text_changed's TextChanged event, bounded to a
// direct callback: entity events belong to the element ticket).
func (e *Editor) OnTextChanged(f func()) {
	if f != nil {
		e.textChanged = append(e.textChanged, f)
	}
}

func (e *Editor) emitTextChanged() {
	for _, f := range e.textChanged {
		f()
	}
}

// Text returns the document contents (as_str).
func (e *Editor) Text() string { return e.text }

// Version returns the storage version, incremented on every content
// change (UnicodeTextStorage::version).
func (e *Editor) Version() uint16 { return e.version }

// LineHeight returns the editor's line height.
func (e *Editor) LineHeight() float32 { return e.lineHeight }

// ElementBounds returns the element's window-local bounds used by
// bounds queries.
func (e *Editor) ElementBounds() Bounds { return e.elementBounds }

// SetElementBounds updates the element's window-local bounds (the
// paint-time element_bounds; scrolling moves the origin).
func (e *Editor) SetElementBounds(bounds Bounds) { e.elementBounds = bounds }

// Selection returns the editor's selection (caret_selection).
func (e *Editor) Selection() EditorSelection { return e.selection }

// setSelection replaces the selection (set_selection).
func (e *Editor) setSelection(s EditorSelection) { e.selection = s }

// Caret returns the active caret endpoint (visible_caret without the
// drag case).
func (e *Editor) Caret() EditorCaretPosition { return e.selection.Caret }

// replaceStorageRange mutates the text and refreshes the version
// (StringStorage::replace_range).
func (e *Editor) replaceStorageRange(rng TextRange, text string) {
	var b strings.Builder
	b.WriteString(e.text[:rng.Start])
	b.WriteString(text)
	b.WriteString(e.text[rng.End:])
	e.text = b.String()
	e.version++
}

// currentDocument returns the shaped document for the current
// storage version, re-shaping when stale (current_document's version
// check; the reference stores the layout from the element's layout
// pass instead of shaping lazily — an equivalent version-gated
// refresh through the layout provider).
func (e *Editor) currentDocument() (*WrappedLine, error) {
	if e.layout != nil && e.layoutAt == e.version {
		return e.layout, nil
	}
	if e.layoutErr != nil && e.layoutAt == e.version {
		return nil, e.layoutErr
	}
	line, err := e.shape(e.text)
	e.layout = line
	e.layoutErr = err
	e.layoutAt = e.version
	if err != nil {
		return nil, err
	}
	return line, nil
}

// shapeWithTextSystem is the default layout provider: shape the
// document through the process-global text system (the native
// Parley/Fontique stack; one style run over the whole text).
func (e *Editor) shapeWithTextSystem(text string) (*WrappedLine, error) {
	ts, err := DefaultTextSystem()
	if err != nil {
		return nil, err
	}
	return ts.ShapeText(TextLayoutRequest{
		Text:     text,
		FontSize: e.fontSize,
		Runs: []TextRun{{
			Len:  len(text),
			Font: e.font,
		}},
		WrapWidth: e.wrapWidth,
	})
}

// validateIncomingText applies the field's input rules
// (validate_incoming_text: single-line fields strip CR/LF; the
// reference's other validations are TODO there too).
func (e *Editor) validateIncomingText(text string) string {
	if e.multiline {
		return text
	}
	return strings.NewReplacer("\n", "", "\r", "").Replace(text)
}

// replaceText is the internal replacement (replace_text): replace the
// storage range, move the selection to the end of the inserted text
// with the inserted-text affinity, and clear the marked range. The
// history subsystem is out of this slice (record_history's skip
// during composition is therefore vacuous here).
func (e *Editor) replaceText(rng TextRange, insert string) {
	endPos := rng.Start + len(insert)
	e.replaceStorageRange(rng, insert)
	pos := EditorCaretPosition{Index: endPos, Affinity: affinityForInsertedText(insert)}
	e.setSelection(EditorSelection{Anchor: pos, Caret: pos})
	e.markedRange = nil
}

// imeResolveRange is the replacement-range fallback chain
// (ime_resolve_range): the IME-provided UTF-16 range, else the marked
// range, else the selection; each end clamped to the storage length.
func (e *Editor) imeResolveRange(rng *UTF16Range) TextRange {
	var r TextRange
	switch {
	case rng != nil:
		r = e.utf16RangeTo8(*rng)
	case e.markedRange != nil:
		r = *e.markedRange
	default:
		r = e.selection.ByteRange()
	}
	n := len(e.text)
	return TextRange{Start: min(r.Start, n), End: min(r.End, n)}
}

// imeMarkTextInRange sets the marked range over the inserted text
// (ime_mark_text_in_range: None for empty inserts).
func (e *Editor) imeMarkTextInRange(rng TextRange, textLen int) {
	if textLen == 0 {
		e.markedRange = nil
		return
	}
	r := TextRange{Start: rng.Start, End: rng.Start + textLen}
	e.markedRange = &r
}

// imeMarkSelectedRange moves the selection for a marked replacement
// (ime_mark_selected_range): the new selection is given in UTF-16
// units RELATIVE TO THE INSERTED TEXT and lands in document
// coordinates; without one the caret sits at the end of the insert.
func (e *Editor) imeMarkSelectedRange(rangeOverwritten TextRange, newSelected *UTF16Range, inserted string) {
	if newSelected != nil {
		start := UTF16ToUTF8Offset(inserted, newSelected.Start) + rangeOverwritten.Start
		end := UTF16ToUTF8Offset(inserted, newSelected.End) + rangeOverwritten.Start
		e.setSelection(EditorSelectionFromRange(start, end))
		return
	}
	pos := rangeOverwritten.Start + len(inserted)
	e.setSelection(EditorSelectionAt(pos))
}

// utf16RangeTo8 converts a UTF-16 range to UTF-8 bytes
// (utf_range_16to8).
func (e *Editor) utf16RangeTo8(rng UTF16Range) TextRange {
	return TextRange{
		Start: UTF16ToUTF8Offset(e.text, rng.Start),
		End:   UTF16ToUTF8Offset(e.text, rng.End),
	}
}

// utf8RangeTo16 converts a UTF-8 range to UTF-16 units
// (utf_range_8to16).
func (e *Editor) utf8RangeTo16(rng TextRange) UTF16Range {
	return UTF16Range{
		Start: UTF8ToUTF16Offset(e.text, rng.Start),
		End:   UTF8ToUTF16Offset(e.text, rng.End),
	}
}

// markedRangeUTF16 returns the marked byte range converted to UTF-16
// (marked_text_range's conversion).
func (e *Editor) markedRangeUTF16() (UTF16Range, bool) {
	if e.markedRange == nil {
		return UTF16Range{}, false
	}
	return e.utf8RangeTo16(*e.markedRange), true
}

// ---------------------------------------------------------------------------
// IMEInputHandler (state.rs EntityInputHandler impl)
// ---------------------------------------------------------------------------

// AcceptsTextInput implements InputHandler (accepts_text_input; the
// reference's EditableTextState does not override the trait's default
// and always accepts).
func (e *Editor) AcceptsTextInput(w *Window, app *App) bool { return true }

// DispatchInput implements InputHandler (dispatch_input: a replayed
// keystroke's text inserts at the selection).
func (e *Editor) DispatchInput(input string, w *Window, app *App) {
	e.ReplaceTextInRange(nil, input, w, app)
}

// ReplaceTextInRange implements InputHandler (replace_text_in_range):
// resolve the range with the IME fallback chain, validate, replace.
func (e *Editor) ReplaceTextInRange(rng *UTF16Range, text string, w *Window, app *App) {
	rng8 := e.imeResolveRange(rng)
	insert := e.validateIncomingText(text)
	e.replaceText(rng8, insert)
	e.emitTextChanged()
}

// ReplaceAndMarkTextInRange implements InputHandler::
// replace_and_mark_text_in_range: resolve and replace, then mark the
// inserted text as composing and move the selection to the new
// selection (relative to the inserted text).
func (e *Editor) ReplaceAndMarkTextInRange(rng *UTF16Range, text string, newSelectedRange *UTF16Range, w *Window, app *App) {
	rng8 := e.imeResolveRange(rng)
	insert := e.validateIncomingText(text)
	e.replaceText(rng8, insert)
	e.imeMarkTextInRange(rng8, len(insert))
	e.imeMarkSelectedRange(rng8, newSelectedRange, insert)
	e.emitTextChanged()
}

// MarkedTextRange implements InputHandler::marked_text_range.
func (e *Editor) MarkedTextRange(w *Window, app *App) (UTF16Range, bool) {
	return e.markedRangeUTF16()
}

// UnmarkText implements InputHandler::unmark_text.
func (e *Editor) UnmarkText(w *Window, app *App) { e.markedRange = nil }

// SelectedTextRange implements InputHandler::selected_text_range: the
// selection in UTF-16 units with the reversed flag.
func (e *Editor) SelectedTextRange(ignoreDisabledInput bool, w *Window, app *App) (UTF16Selection, bool) {
	return UTF16Selection{
		Range:    e.utf8RangeTo16(e.selection.ByteRange()),
		Reversed: e.selection.Reversed(),
	}, true
}

// SetSelectedTextRange implements InputHandler::set_selected_text_range.
// The reference's EditableTextState does not override the trait's
// default (empty), matching system-driven selection moves this port
// does not model.
func (e *Editor) SetSelectedTextRange(rng UTF16Range, w *Window, app *App) {}

// TextForRange implements InputHandler::text_for_range: the clamped
// range's text plus the clamped range reported back (the
// surrounding-text contract; adjusted_range is the reference's
// out-parameter).
func (e *Editor) TextForRange(rng UTF16Range, w *Window, app *App) (string, *UTF16Range, bool) {
	rng8 := e.utf16RangeTo8(rng)
	n := len(e.text)
	start := min(rng8.Start, n)
	end := min(rng8.End, n)
	if start > end {
		// A caller range whose clamped start exceeds its clamped end
		// would panic the reference's slice; the port reports the
		// empty range at the clamped start (defensive; no recorded
		// fixture exercises it).
		end = start
	}
	clamped := TextRange{Start: start, End: end}
	adjusted := e.utf8RangeTo16(clamped)
	return e.text[clamped.Start:clamped.End], &adjusted, true
}

// TextLengthUTF16 implements InputHandler::text_length_utf16. The
// reference's EditableTextState keeps the trait's default (None);
// this port reports the length instead of modeling the entity-error
// case that default covers (documented deviation, harmless for the
// IMM32 path which never queries it).
func (e *Editor) TextLengthUTF16(w *Window, app *App) (int, bool) {
	return UTF8ToUTF16Offset(e.text, len(e.text)), true
}

// BoundsForRange implements InputHandler::bounds_for_range (the
// coordinate convention is documented in ime.go's header):
// window-local logical bounds = element origin + document-local
// geometry.
//
//   - An empty range yields the caret rect: CARET_PIXELS_EPSILON (4px)
//     wide, one line height tall, at the caret position of a caret
//     attached to the NEXT cluster (state.rs bounds_for_range's
//     attached_to_next_cluster + CARET_PIXELS_EPSILON).
//   - A nonempty range yields the first selection rectangle
//     (selection_bounds().next()).
func (e *Editor) BoundsForRange(rng UTF16Range, w *Window, app *App) (Bounds, bool) {
	doc, err := e.currentDocument()
	if err != nil {
		return Bounds{}, false
	}
	rng8 := e.utf16RangeTo8(rng)
	n := len(e.text)
	start := min(rng8.Start, n)
	end := min(rng8.End, n)
	if start == end {
		caret, err := doc.Caret(start, CaretAffinityDownstream, e.lineHeight)
		if err != nil || !caret.Present {
			return Bounds{}, false
		}
		origin := Point{
			X: e.elementBounds.Origin.X + caret.Bounds.X,
			Y: e.elementBounds.Origin.Y + caret.Bounds.Y,
		}
		return Bounds{
			Origin: origin,
			Size:   Size{Width: caretPixelsEpsilon, Height: e.lineHeight},
		}, true
	}
	rects, err := doc.SelectionRects(start, end, e.lineHeight)
	if err != nil || len(rects) == 0 {
		return Bounds{}, false
	}
	first := rects[0]
	return Bounds{
		Origin: Point{
			X: e.elementBounds.Origin.X + first.X,
			Y: e.elementBounds.Origin.Y + first.Y,
		},
		Size: Size{Width: first.W, Height: first.H},
	}, true
}

// CharacterIndexForPoint implements InputHandler::
// character_index_for_point: the closest caret's UTF-16 offset for a
// point. The reference passes the point it received straight to the
// document hit test (macos/window.rs converts the screen point to
// window-local first; document coordinates are element-local, so the
// reference's own adapter subtracts only the window frame — a latent
// asymmetry preserved here, not silently "fixed"; the Windows path
// never calls this member).
func (e *Editor) CharacterIndexForPoint(p Point, w *Window, app *App) (int, bool) {
	doc, err := e.currentDocument()
	if err != nil {
		return 0, false
	}
	hit, err := doc.HitTest(p.X, p.Y, e.lineHeight)
	if err != nil {
		return 0, false
	}
	return UTF8ToUTF16Offset(e.text, hit.Index), true
}

// caretPixelsEpsilon is CARET_PIXELS_EPSILON (state.rs: px(4.)).
const caretPixelsEpsilon float32 = 4.0
