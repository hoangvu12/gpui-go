package gpui

// The platform-independent IME surface (ticket20): the UTF-16 input
// seam between the platform host's IMM32 handling and the editor-side
// document model, ported from the pinned CE reference
// 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a:
//
//   - crates/gpui/src/input.rs — utf8_to_utf16_offset /
//     utf16_to_utf8_offset (clamping and mid-character snapping
//     semantics) and the EntityInputHandler shape behind
//     ElementInputHandler (element bounds carried at paint).
//   - crates/gpui/src/platform.rs — the InputHandler trait's IME
//     members (selected/marked ranges, text-for-range with the
//     adjusted range, replacement, marked replacement, unmark,
//     bounds-for-range, point lookup, selection set, text length),
//     UTF16Selection (range + reversed), and
//     PlatformInputHandler::compute_ime_candidate_bounds (the
//     marked-range line-start walk-back).
//   - crates/gpui/src/window.rs — invalidate_character_coordinates
//     (the next-frame push of candidate bounds to the platform).
//   - crates/gpui_windows/src/events.rs — the composition message
//     routing policies (zero-lParam path, GCS_RESULTSTR-then-GCS_COMPSTR
//     ordering, composition cursor selection with the
//     ATTR_INPUT-adjacency rule), expressed here as the pure
//     imeCompositionDriver over two seams so the routing is testable
//     without IMM32.
//
// The Win32/IMM32 side (ImmGetContext/ImmGetCompositionStringW,
// WM_IME_* wiring, COMPOSITIONFORM/CANDIDATEFORM) lives in
// ime_win32.go behind a windows build tag; this file carries only the
// platform-independent contract.
//
// Coordinate convention (the documented discrepancy, resolved from
// pinned source, see ime_win32_test.go and internal/imespec's
// nonzero-origin fixture): InputHandler bounds are WINDOW-LOCAL
// LOGICAL pixels. The trait doc comment in the pin says "screen
// coordinates" (crates/gpui/src/platform.rs, bounds_for_range — the
// NSTextInputClient firstRect contract), but that comment describes
// what the macOS ADAPTER produces by adding the NSWindow frame origin
// (crates/gpui_macos/src/window.rs first_rect_for_character_range:
// frame.origin + bounds.origin, y-flipped). The Windows consumer never
// translates: events.rs retrieve_caret_position multiplies the same
// window-local bounds by the scale factor and hands the result to
// ImmSetCompositionWindow/ImmSetCandidateWindow, whose
// COMPOSITIONFORM/CANDIDATEFORM ptCurrentPos is client-area relative
// (imm.h; the platform contract states this). The port therefore
// treats the comment as a stale description of the macOS seam and the
// Windows behavior as the parity target: no origin offset, exactly one
// logical-to-device conversion at the IMM32 boundary.

import "unicode/utf16"

// ---------------------------------------------------------------------------
// UTF-16 offsets (input.rs utf8_to_utf16_offset / utf16_to_utf8_offset)
// ---------------------------------------------------------------------------

// UTF16LengthOf returns the number of UTF-16 code units of r (1, or 2
// for supplementary-plane runes).
func UTF16LengthOf(r rune) int {
	if r > 0xFFFF {
		return 2
	}
	return 1
}

// UTF8ToUTF16Offset converts a UTF-8 byte offset into a UTF-16
// code-unit offset (input.rs utf8_to_utf16_offset). Offsets past the
// end of text clamp to its UTF-16 length; an offset inside a UTF-8
// character snaps to the end of that character (the character whose
// start precedes the offset is counted).
func UTF8ToUTF16Offset(text string, utf8Offset int) int {
	consumed := 0
	for offset, r := range text {
		if offset >= utf8Offset {
			break
		}
		consumed += UTF16LengthOf(r)
	}
	return consumed
}

// UTF16ToUTF8Offset converts a UTF-16 code-unit offset into a UTF-8
// byte offset (input.rs utf16_to_utf8_offset). Offsets past the end
// clamp to the UTF-8 length; an offset inside a surrogate pair snaps
// to the end of the corresponding character.
func UTF16ToUTF8Offset(text string, utf16Offset int) int {
	consumed := 0
	for offset, r := range text {
		if consumed >= utf16Offset {
			return offset
		}
		consumed += UTF16LengthOf(r)
	}
	return len(text)
}

// UTF16DecodeLossy mirrors Rust's String::from_utf16_lossy for the
// composition strings the IME returns (invalid surrogates become
// U+FFFD; the lossy form is what events.rs hands the editor).
func UTF16DecodeLossy(units []uint16) string {
	return string(utf16.Decode(units))
}

// ---------------------------------------------------------------------------
// UTF16Selection (platform.rs UTF16Selection)
// ---------------------------------------------------------------------------

// UTF16Selection is a selection in a text document, in UTF-16 code
// units (platform.rs UTF16Selection): the selected range plus whether
// the head is at the range's start (false) or end (true) — the
// reference's `reversed` (endpoint ordering of caret versus anchor).
type UTF16Selection struct {
	// Range is the selected UTF-16 range.
	Range UTF16Range
	// Reversed reports that the selection's head (active caret) is at
	// the range's end rather than its start.
	Reversed bool
}

// ---------------------------------------------------------------------------
// The IME input handler (platform.rs InputHandler's IME members)
// ---------------------------------------------------------------------------

// IMEInputHandler is the IME extension of InputHandler: the
// composition-facing members the keyboard path does not consume
// (input.go documents that this ticket extends the handler surface
// with composition and bounds queries). The reference's InputHandler
// trait is closed and every handler implements all of it; the Go port
// splits the base interface (keyboard/WM_CHAR) from this extension
// because the base interface is shared with earlier tickets' tests.
// A window's active handler that does not implement IMEInputHandler
// still receives WM_CHAR text through the base interface, but IME
// composition messages that need these members fall through to
// DefWindowProc (documented in the ticket).
type IMEInputHandler interface {
	InputHandler

	// SelectedTextRange returns the current selection in UTF-16 units
	// (InputHandler::selected_text_range); ignoreDisabledInput follows
	// the reference's flag.
	SelectedTextRange(ignoreDisabledInput bool, w *Window, app *App) (UTF16Selection, bool)
	// MarkedTextRange returns the marked (composing) range in UTF-16
	// units, when composition is active (InputHandler::marked_text_range).
	MarkedTextRange(w *Window, app *App) (UTF16Range, bool)
	// TextForRange returns the text of a UTF-16 range with the
	// clamped range the handler actually used (InputHandler::
	// text_for_range; the adjusted range is the surrounding-text
	// contract the IME-facing mirror consumes).
	TextForRange(rng UTF16Range, w *Window, app *App) (text string, adjusted *UTF16Range, ok bool)
	// ReplaceAndMarkTextInRange replaces a UTF-16 range with text and
	// marks the result as composing, moving the selection to
	// newSelectedRange when it is given (relative to the inserted
	// text; InputHandler::replace_and_mark_text_in_range).
	ReplaceAndMarkTextInRange(rng *UTF16Range, text string, newSelectedRange *UTF16Range, w *Window, app *App)
	// UnmarkText clears the composing state (InputHandler::unmark_text).
	UnmarkText(w *Window, app *App)
	// BoundsForRange returns the bounds of a UTF-16 range in
	// window-local logical pixels (InputHandler::bounds_for_range; see
	// the coordinate-convention note in the file header).
	BoundsForRange(rng UTF16Range, w *Window, app *App) (Bounds, bool)
	// CharacterIndexForPoint returns the UTF-16 offset for a point
	// (InputHandler::character_index_for_point).
	CharacterIndexForPoint(p Point, w *Window, app *App) (int, bool)
	// SetSelectedTextRange moves the selection on the platform's
	// behalf (InputHandler::set_selected_text_range).
	SetSelectedTextRange(rng UTF16Range, w *Window, app *App)
	// TextLengthUTF16 returns the document length in UTF-16 units,
	// when known (InputHandler::text_length_utf16).
	TextLengthUTF16(w *Window, app *App) (int, bool)
}

// ---------------------------------------------------------------------------
// Candidate bounds (platform.rs compute_ime_candidate_bounds)
// ---------------------------------------------------------------------------

// ComputeIMECandidateBounds ports PlatformInputHandler::
// compute_ime_candidate_bounds: during composition the candidate
// anchor is the caret's line start (walking back from the caret until
// the bounds' y changes by more than 0.1px — a visual-line break),
// otherwise the active selection endpoint.
func ComputeIMECandidateBounds(h IMEInputHandler, w *Window, app *App) (Bounds, bool) {
	markedRange, hasMarked := h.MarkedTextRange(w, app)
	selection, ok := h.SelectedTextRange(true, w, app)
	if !ok {
		return Bounds{}, false
	}
	if hasMarked {
		// Default to the start of the marked (composing) range.
		lineStart := markedRange.Start
		// Walk backward from the caret looking for a line break. A
		// change in the Y coordinate means the caret crossed into the
		// previous visual line, so the line start is one position
		// after the break point.
		caret := selection.Range.End
		if caretBounds, ok := h.BoundsForRange(UTF16Range{Start: caret, End: caret}, w, app); ok {
			for i := caret - 1; i >= markedRange.Start; i-- {
				if b, ok := h.BoundsForRange(UTF16Range{Start: i, End: i}, w, app); ok {
					if abs32f(b.Origin.Y-caretBounds.Origin.Y) > 0.1 {
						lineStart = i + 1
						break
					}
				}
			}
		}
		return h.BoundsForRange(UTF16Range{Start: lineStart, End: lineStart}, w, app)
	}
	// No active composition: use the selection endpoint.
	offset := selection.Range.End
	if selection.Reversed {
		offset = selection.Range.Start
	}
	return h.BoundsForRange(UTF16Range{Start: offset, End: offset}, w, app)
}

// abs32f is the float32 absolute value (the pin's (a - b).abs()).
func abs32f(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// ---------------------------------------------------------------------------
// The push path (window.rs invalidate_character_coordinates)
// ---------------------------------------------------------------------------

// InvalidateCharacterCoordinates pushes new IME panel position
// suggestions to the platform window (window.rs
// invalidate_character_coordinates: on_next_frame, take the input
// handler, selected_bounds, update_ime_position, set the handler
// back). The reference defers to the next frame so paint-time state is
// consistent; this port defers through the host's foreground queue
// (the next safe foreground entry) because the frame loop arrives with
// the renderer tickets — a documented bounded deviation, cited in the
// ticket. The pull path (WM_IME_STARTCOMPOSITION) is unaffected and
// remains the parity target.
//
// Only a real (host-backed) window can push; a test window's call is a
// no-op.
func (w *Window) InvalidateCharacterCoordinates() {
	if w == nil {
		return
	}
	host := w.handle.host
	if host == nil {
		return
	}
	lw := w
	host.runForegroundAsync(func() {
		// take_input_handler / set_input_handler (with the
		// take-while-running semantics the nested-message reentry
		// contract needs).
		handler := imeTakeHandler(lw)
		if handler == nil {
			return
		}
		defer imeRestoreHandler(lw, handler)
		if ime, ok := handler.(IMEInputHandler); ok {
			if bounds, ok := ComputeIMECandidateBounds(ime, lw, lw.app); ok {
				lw.handle.UpdateIMEPosition(bounds)
			}
		}
	})
}

// imeTakeHandler takes the window's active input handler out of its
// slot (the reference's state.input_handler.take()), returning nil
// when none is installed. Foreground thread only.
func imeTakeHandler(w *Window) InputHandler {
	if w == nil {
		return nil
	}
	fs := focusState(w)
	handler := fs.inputHandler
	fs.inputHandler = nil
	return handler
}

// imeRestoreHandler returns a taken handler to the window's slot
// (state.input_handler.set). Foreground thread only.
func imeRestoreHandler(w *Window, handler InputHandler) {
	if w == nil || handler == nil {
		return
	}
	focusState(w).inputHandler = handler
}

// ---------------------------------------------------------------------------
// The composition events (browser-core ticket 18)
// ---------------------------------------------------------------------------

// IMECompositionHandler is the optional composition-event extension of
// InputHandler (browser-core ticket 18): the compositionstart /
// compositionupdate / compositionend callbacks the DOM fires on the
// focused editable element, delivered in Chromium's order around the
// text mutations the IME composition driver performs (third_party/
// blink/renderer/core/editing/ime/input_method_controller.cc):
//
//   - compositionstart fires when the first non-empty composition
//     string marks text (SetComposition's start arm, :1020-1022), its
//     data the selection the composition replaces
//     (DispatchCompositionStartEvent :935-948 passes
//     GetFrame().SelectedText());
//   - compositionupdate fires before every text mutation of the
//     session — the start, each update, the commit
//     (InsertTextDuringCompositionWithEvents :391) and the cancel —
//     its data the new composition string (the commit's data is the
//     committed text);
//   - compositionend fires after the commit's or cancel's mutation
//     (ReplaceComposition :728 — "No DOM update after
//     'compositionend'"; CancelComposition :932), its data the
//     committed text for a commit, the empty string for a cancel.
//
// The order and data were verified against headless Chrome through
// CDP Input.imeSetComposition / Input.insertText (the imecompose
// parity fixture's record). A handler that does not implement this
// interface still receives the text mutations through the base
// interface; the composition events are skipped (like the base/
// IMEInputHandler split, this keeps earlier tickets' handlers
// compiling).
type IMECompositionHandler interface {
	InputHandler

	// CompositionStart reports that a composition session started; its
	// data is the selection text the composition replaces ("" for an
	// empty selection).
	CompositionStart(data string, w *Window, app *App)
	// CompositionUpdate reports a composition-string change (including
	// the one that starts the session, the commit's final string and
	// the empty string of a cancel); its data is the new string.
	CompositionUpdate(data string, w *Window, app *App)
	// CompositionEnd reports that the session ended: committed (the
	// data is the committed text) or cancelled (empty data).
	CompositionEnd(data string, w *Window, app *App)
}

// compositionEventKind is one of the DOM composition event kinds.
type compositionEventKind uint8

const (
	compositionStart compositionEventKind = iota
	compositionUpdate
	compositionEnd
)

// deliverCompositionEvent delivers one composition callback to the
// handler's optional IMECompositionHandler surface (the DOM event the
// page's focused editable element receives; a handler that does not
// implement the surface still gets the text mutations).
func deliverCompositionEvent(h IMEInputHandler, kind compositionEventKind, data string, w *Window, app *App) {
	c, ok := h.(IMECompositionHandler)
	if !ok {
		return
	}
	switch kind {
	case compositionStart:
		c.CompositionStart(data, w, app)
	case compositionUpdate:
		c.CompositionUpdate(data, w, app)
	case compositionEnd:
		c.CompositionEnd(data, w, app)
	}
}

// compositionStartData is the text the composition replaces
// (DispatchCompositionStartEvent's GetFrame().SelectedText()): the
// handler's current selection text, "" when the queries fail.
func compositionStartData(h IMEInputHandler, w *Window, app *App) string {
	selection, ok := h.SelectedTextRange(true, w, app)
	if !ok {
		return ""
	}
	text, _, ok := h.TextForRange(selection.Range, w, app)
	if !ok {
		return ""
	}
	return text
}

// imeHandlerComposing reports whether the handler holds an active
// composition (Chromium's InputMethodController::HasComposition, the
// checks the composition paths branch on): the marked range's
// presence in the port's model.
func imeHandlerComposing(h IMEInputHandler, w *Window, app *App) bool {
	_, composing := h.MarkedTextRange(w, app)
	return composing
}

// imeCompositionActive reports whether the window's active input
// handler holds an active composition — the window-level form
// Chromium's KeyboardEvent constructor reads (HasCurrentComposition,
// core/events/keyboard_event.cc:72-78: the frame's
// InputMethodController::HasComposition). A handler that does not
// implement the IME extension has no composition state.
func imeCompositionActive(w *Window, app *App) bool {
	if w == nil {
		return false
	}
	handler := focusState(w).inputHandler
	ime, ok := handler.(IMEInputHandler)
	if !ok || ime == nil {
		return false
	}
	return imeHandlerComposing(ime, w, app)
}

// markKeyEventComposition stamps the DOM's isComposing flag onto a
// keyboard event at dispatch time (keyboard_event.cc:122 sets
// is_composing_ from HasCurrentComposition when the DOM event is
// constructed, which is the dispatch the listeners observe). Both
// halves of a press carry it; the event's own field is ignored on
// input.
func markKeyEventComposition(event any, w *Window, app *App) {
	composing := imeCompositionActive(w, app)
	switch e := event.(type) {
	case *KeyDownEvent:
		e.IsComposing = composing
	case *KeyUpEvent:
		e.IsComposing = composing
	}
}

// attrsUseIMECursorPosition is events.rs's
// should_use_ime_cursor_position adjacency test over the GCS_COMPATTR
// bytes: the suggested cursor position is used only when it sits
// adjacent to unconverted (ATTR_INPUT) composition text.
func attrsUseIMECursorPosition(attrs []byte, cursorPos int) bool {
	// Keep the cursor adjacent to the inserted text by only using the
	// suggested position if it's adjacent to unconverted text.
	atCursorIsInput := cursorPos >= 0 && cursorPos < len(attrs) && attrs[cursorPos] == ATTRInput
	beforeCursorIsInput := cursorPos > 0 && cursorPos-1 < len(attrs) && attrs[cursorPos-1] == ATTRInput
	return atCursorIsInput || beforeCursorIsInput
}

// ---------------------------------------------------------------------------
// The simulated composition seam (browser-core ticket 18)
// ---------------------------------------------------------------------------

// IMECompositionMessage is one simulated WM_IME_COMPOSITION message:
// the recorded composition data a test drives through the ported
// composition driver against a window's active input handler, the
// test-window seam of the host's real IMM32 path (ime_win32.go
// handleIMEComposition, which reads the same data out of the IME
// context).
type IMECompositionMessage struct {
	// Flags are the message's GCS_* flags (GCSCompStr, GCSResultStr,
	// GCSCursorPos, or 0 for the Japanese zero-lParam message).
	Flags uint32
	// Composition is the GCS_COMPSTR string (UTF-8; the session's new
	// composition string).
	Composition string
	// Result is the GCS_RESULTSTR string (UTF-8; the committed text).
	Result string
	// Cursor is the GCS_CURSORPOS position in UTF-16 units within the
	// composition string; the driver only reads it when Flags carries
	// GCSCursorPos.
	Cursor int
	// Attrs are the GCS_COMPATTR bytes (the ATTR_INPUT adjacency test
	// the cursor policy applies).
	Attrs []byte
}

// SimulateIMEComposition drives one simulated WM_IME_COMPOSITION
// message through the composition driver against the window's active
// input handler (the test-window seam of the host's IMM32 path).
// It reports whether the message was handled: false is the reference's
// Option::None (no handler, or a handler without the IME extension —
// DefWindowProc). The driver derives and delivers the DOM composition
// events (IMECompositionHandler) around the text mutations, exactly
// like the host path. The dispatch runs inside one application update.
func (w *Window) SimulateIMEComposition(msg IMECompositionMessage, cx AppContext) bool {
	if w == nil {
		panic("gpui: SimulateIMEComposition requires a live window")
	}
	app := appFrom(cx)
	var handled bool
	app.Update(func(a *App) {
		handled = w.simulateIMEComposition(msg, a)
	})
	return handled
}

// simulateIMEComposition is the body; it runs inside an update on the
// foreground thread.
func (w *Window) simulateIMEComposition(msg IMECompositionMessage, app *App) bool {
	driver := &imeCompositionDriver{
		seam: windowIMESeam{w: w},
		src:  simulatedIMECompositionSource{msg: msg},
	}
	_, handled := driver.handleComposition(uintptr(msg.Flags))
	return handled
}

// windowIMESeam is the test window's imeHandlerSeam: the active input
// handler is taken out for the callback (the take/put discipline the
// host seam applies, so nested composition messages observe no
// handler) and the callback runs with the window's own application.
type windowIMESeam struct {
	w *Window
}

func (s windowIMESeam) withIMEHandler(f func(h IMEInputHandler, w *Window, app *App)) bool {
	w := s.w
	if w == nil || w.app == nil {
		return false
	}
	handler := imeTakeHandler(w)
	ime, ok := handler.(IMEInputHandler)
	if !ok {
		imeRestoreHandler(w, handler)
		return false
	}
	defer imeRestoreHandler(w, handler)
	f(ime, w, w.app)
	return true
}

// simulatedIMECompositionSource is an imeCompositionSource over a
// simulated message's recorded data (the fake of
// imeContextSource's ImmGetCompositionStringW reads: an absent or
// empty buffer is ok=true with no units).
type simulatedIMECompositionSource struct {
	msg IMECompositionMessage
}

func (s simulatedIMECompositionSource) compositionString(compType uint32) ([]uint16, bool) {
	var text string
	switch compType {
	case GCSResultStr:
		text = s.msg.Result
	case GCSCompStr:
		text = s.msg.Composition
	default:
		return nil, false
	}
	if text == "" {
		return nil, true
	}
	return utf16.Encode([]rune(text)), true
}

func (s simulatedIMECompositionSource) cursorPosition() int {
	return s.msg.Cursor
}

func (s simulatedIMECompositionSource) shouldUseIMECursorPosition(cursorPos int) bool {
	return attrsUseIMECursorPosition(s.msg.Attrs, cursorPos)
}

// ---------------------------------------------------------------------------
// The composition message driver (events.rs handle_ime_composition)
// ---------------------------------------------------------------------------

// Composition-string query flags (imm.h IME_COMPOSITION_STRING values,
// the GCS_* constants events.rs tests).
const (
	// GCSCompReadStr is GCS_COMPREADSTR.
	GCSCompReadStr uint32 = 0x0001
	// GCSCompReadAttr is GCS_COMPREADATTR.
	GCSCompReadAttr uint32 = 0x0002
	// GCSCompReadClause is GCS_COMPREADCLAUSE.
	GCSCompReadClause uint32 = 0x0004
	// GCSCompStr is GCS_COMPSTR: the composition string changed.
	GCSCompStr uint32 = 0x0008
	// GCSCompAttr is GCS_COMPATTR: the per-character composition
	// attributes (ATTR_*).
	GCSCompAttr uint32 = 0x0010
	// GCSCompClause is GCS_COMPCLAUSE.
	GCSCompClause uint32 = 0x0020
	// GCSCursorPos is GCS_CURSORPOS: the composition cursor position.
	GCSCursorPos uint32 = 0x0080
	// GCSDeltaStart is GCS_DELTASTART.
	GCSDeltaStart uint32 = 0x0100
	// GCSResultReadStr is GCS_RESULTREADSTR.
	GCSResultReadStr uint32 = 0x0200
	// GCSResultReadClause is GCS_RESULTREADCLAUSE.
	GCSResultReadClause uint32 = 0x0400
	// GCSResultStr is GCS_RESULTSTR: the final result string (commit).
	GCSResultStr uint32 = 0x0800
	// GCSResultClause is GCS_RESULTCLAUSE.
	GCSResultClause uint32 = 0x1000
)

// ATTRInput is imm.h ATTR_INPUT: the unconverted (typed) composition
// character class (shouldUseIMECursorPosition's adjacency test).
const ATTRInput uint8 = 0x00

// imeCompositionSource reads composition data from an IME context
// (events.rs's parse_ime_composition_string /
// retrieve_composition_cursor_position / should_use_ime_cursor_position
// over a HIMC; the Win32 adapter implements it with
// ImmGetCompositionStringW, tests with recorded data).
type imeCompositionSource interface {
	// compositionString reads one GCS_* string buffer, reporting the
	// UTF-16 code units (parse_ime_composition_string: a negative
	// ImmGetCompositionStringW length is a failure).
	compositionString(compType uint32) ([]uint16, bool)
	// cursorPosition reads GCS_CURSORPOS (the composition cursor in
	// UTF-16 units within the composition string).
	cursorPosition() int
	// shouldUseIMECursorPosition reports the cursor-adjacency test:
	// the suggested position is used only when the cursor sits
	// adjacent to unconverted (ATTR_INPUT) composition text
	// (should_use_ime_cursor_position over the GCS_COMPATTR bytes).
	shouldUseIMECursorPosition(cursorPos int) bool
}

// imeHandlerSeam runs a callback with the window's active input
// handler under the reference's take/put discipline (events.rs
// with_input_handler: the handler is taken out for the call, so nested
// messages dispatched during the callback observe no handler and fall
// to DefWindowProc). ok=false is the reference's None: no handler.
type imeHandlerSeam interface {
	withIMEHandler(f func(h IMEInputHandler, w *Window, app *App)) bool
}

// imeCompositionDriver is the WM_IME_COMPOSITION routing (events.rs
// handle_ime_composition / handle_ime_composition_inner), separated
// from IMM32 so the message policies are testable deterministically.
type imeCompositionDriver struct {
	seam imeHandlerSeam
	src  imeCompositionSource
}

// handleComposition processes one WM_IME_COMPOSITION message. It
// returns (result, handled): handled=false is the reference's
// Option::None (DefWindowProcW); a handled composition returns 0.
//
// Policies (events.rs handle_ime_composition_inner), with the DOM
// composition events (browser-core ticket 18) derived around the
// mutations in Chromium's order (input_method_controller.cc; see
// IMECompositionHandler's citations):
//
//   - lParam == 0: a Japanese IME signals that there is no composition
//     string; the current composition is replaced with empty text and
//     the message is consumed (when an input handler exists). With an
//     active session this is a cancel: compositionupdate("") before
//     the mutation, compositionend("") after it (CancelComposition:
//     :920-932).
//   - GCS_RESULTSTR: the committed result replaces the text (sent
//     BEFORE the composition update, so a commit followed by a new
//     composition mark lands in the right order). With an active
//     session the commit fires compositionupdate(result) before the
//     mutation and compositionend(result) after it (ReplaceComposition
//     :718-728); without one it is a plain insert — no composition
//     events (CommitText's InsertText arm, :630-635).
//   - GCS_COMPSTR: the composition string replaces-and-marks; the
//     composition cursor becomes the marked selection when
//     GCS_CURSORPOS is set and the composition is non-empty, using the
//     IME's cursor position only when it is adjacent to unconverted
//     (ATTR_INPUT) text, else the composition string's length. A
//     non-empty string starting a session fires compositionstart
//     (then compositionupdate) before the mutation (SetComposition's
//     start arm, :1020-1022); a non-empty string over an active session
//     fires compositionupdate; an EMPTY string over an active session
//     cancels it (SetComposition's empty-text arm, :1003-1015 —
//     compositionupdate("") before the mutation, compositionend("")
//     after it), and without a session mutates without events
//     (:1009-1012, "without sending extra events").
//   - Other flag combinations are not consumed ("currently, we don't
//     care other stuff").
func (d *imeCompositionDriver) handleComposition(lparam uintptr) (uintptr, bool) {
	flags := uint32(lparam)
	if flags == 0 {
		// Japanese IME zero-lParam path.
		if !d.seam.withIMEHandler(func(h IMEInputHandler, w *Window, app *App) {
			composing := imeHandlerComposing(h, w, app)
			if composing {
				deliverCompositionEvent(h, compositionUpdate, "", w, app)
			}
			h.ReplaceTextInRange(nil, "", w, app)
			if composing {
				deliverCompositionEvent(h, compositionEnd, "", w, app)
			}
		}) {
			return 0, false
		}
		return 0, true
	}
	if flags&GCSResultStr != 0 {
		result, ok := d.src.compositionString(GCSResultStr)
		if !ok {
			return 0, false
		}
		resultText := UTF16DecodeLossy(result)
		if !d.seam.withIMEHandler(func(h IMEInputHandler, w *Window, app *App) {
			composing := imeHandlerComposing(h, w, app)
			if composing {
				deliverCompositionEvent(h, compositionUpdate, resultText, w, app)
			}
			h.ReplaceTextInRange(nil, resultText, w, app)
			if composing {
				deliverCompositionEvent(h, compositionEnd, resultText, w, app)
			}
		}) {
			return 0, false
		}
	}
	if flags&GCSCompStr != 0 {
		comp, ok := d.src.compositionString(GCSCompStr)
		if !ok {
			return 0, false
		}
		var caret *UTF16Range
		if len(comp) > 0 && flags&GCSCursorPos != 0 {
			pos := len(comp)
			cursorPos := d.src.cursorPosition()
			if d.src.shouldUseIMECursorPosition(cursorPos) {
				pos = cursorPos
			}
			caret = &UTF16Range{Start: pos, End: pos}
		}
		compText := UTF16DecodeLossy(comp)
		if !d.seam.withIMEHandler(func(h IMEInputHandler, w *Window, app *App) {
			composing := imeHandlerComposing(h, w, app)
			if compText != "" {
				if !composing {
					// A session starts: compositionstart (data: the
					// selection it replaces) then compositionupdate
					// (SetComposition's start arm).
					deliverCompositionEvent(h, compositionStart, compositionStartData(h, w, app), w, app)
				}
				deliverCompositionEvent(h, compositionUpdate, compText, w, app)
			} else if composing {
				// An empty composition string cancels the session.
				deliverCompositionEvent(h, compositionUpdate, "", w, app)
			}
			h.ReplaceAndMarkTextInRange(nil, compText, caret, w, app)
			if compText == "" && composing {
				deliverCompositionEvent(h, compositionEnd, "", w, app)
			}
		}) {
			return 0, false
		}
	}
	if flags&(GCSResultStr|GCSCompStr) != 0 {
		return 0, true
	}
	return 0, false
}
