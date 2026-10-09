package gpui

// Ticket20's platform-independent unit tests: the UTF-16 offset
// conversions (input.rs), the composition message routing policies
// (events.rs handle_ime_composition_inner, exercised through the
// imeCompositionDriver with a fake source/seam), and the candidate
// bounds computation (platform.rs compute_ime_candidate_bounds).

import (
	"fmt"
	"testing"
)

// ---------------------------------------------------------------------------
// UTF-16 offsets (input.rs utf8_to_utf16_offset / utf16_to_utf8_offset)
// ---------------------------------------------------------------------------

func TestUTF16OffsetConversions(t *testing.T) {
	// "A😀B": A at byte 0/UTF-16 0, 😀 bytes 1..5/UTF-16 1..3, B byte
	// 5/UTF-16 3. Mid-surrogate and mid-UTF-8 offsets snap to the
	// character's end; past-the-end offsets clamp.
	const text = "A😀B"
	cases := []struct {
		utf8, utf16 int
	}{
		{0, 0},
		{1, 1}, // boundary before 😀
		{3, 3}, // inside 😀's UTF-8 bytes -> counts it
		{5, 3}, // boundary after 😀
		{6, 4}, // one past the end -> clamps to the UTF-16 length
		{9, 4}, // far past the end -> clamps
	}
	for _, c := range cases {
		if got := UTF8ToUTF16Offset(text, c.utf8); got != c.utf16 {
			t.Errorf("UTF8ToUTF16Offset(%q, %d) = %d, want %d", text, c.utf8, got, c.utf16)
		}
	}
	back := []struct {
		utf16, utf8 int
	}{
		{0, 0},
		{1, 1}, // boundary before 😀
		{2, 5}, // inside 😀's surrogate pair -> snaps to its end
		{3, 5}, // boundary after 😀
		{4, 6}, // one past the end -> clamps to the UTF-8 length
		{9, 6}, // far past the end -> clamps
	}
	for _, c := range back {
		if got := UTF16ToUTF8Offset(text, c.utf16); got != c.utf8 {
			t.Errorf("UTF16ToUTF8Offset(%q, %d) = %d, want %d", text, c.utf16, got, c.utf8)
		}
	}
	if got := UTF8ToUTF16Offset("", 5); got != 0 {
		t.Errorf("UTF8ToUTF16Offset(empty, 5) = %d, want 0", got)
	}
	if got := UTF16ToUTF8Offset("", 5); got != 0 {
		t.Errorf("UTF16ToUTF8Offset(empty, 5) = %d, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// Fakes for the driver tests
// ---------------------------------------------------------------------------

// fakeIMESeam is an imeHandlerSeam with one handler (nil = none).
type fakeIMESeam struct {
	handler IMEInputHandler
	// active counts the take/put discipline's nested use.
	active int
}

func (s *fakeIMESeam) withIMEHandler(f func(h IMEInputHandler, w *Window, app *App)) bool {
	if s.handler == nil || s.active > 0 {
		// No handler, or the outer callback already took it: the
		// reference's take() leaves the slot empty during the call.
		return false
	}
	s.active++
	defer func() { s.active-- }()
	f(s.handler, nil, nil)
	return true
}

// fakeIMECompositionSource is an imeCompositionSource with recorded
// composition data (nil result = parse failure).
type fakeIMECompositionSource struct {
	result      []uint16
	resultOK    bool
	comp        []uint16
	compOK      bool
	cursor      int
	attrs       []byte
	cursorCalls int
}

func (s *fakeIMECompositionSource) compositionString(compType uint32) ([]uint16, bool) {
	switch compType {
	case GCSResultStr:
		return s.result, s.resultOK
	case GCSCompStr:
		return s.comp, s.compOK
	}
	return nil, false
}

func (s *fakeIMECompositionSource) cursorPosition() int {
	s.cursorCalls++
	return s.cursor
}

func (s *fakeIMECompositionSource) shouldUseIMECursorPosition(cursorPos int) bool {
	if len(s.attrs) == 0 {
		return false
	}
	atCursor := cursorPos >= 0 && cursorPos < len(s.attrs) && s.attrs[cursorPos] == ATTRInput
	before := cursorPos > 0 && cursorPos-1 < len(s.attrs) && s.attrs[cursorPos-1] == ATTRInput
	return atCursor || before
}

// recordingEditor wraps an Editor with a call log (the fake shape
// avoids the native text stack: model operations need no geometry).
type recordingEditor struct {
	*Editor
	calls []string
}

func newRecordingEditor(text string, caret int) *recordingEditor {
	e := &recordingEditor{Editor: NewEditor(text, caret, EditorOptions{
		Shape: func(string) (*WrappedLine, error) { return nil, errNoEditorLayout },
	})}
	return e
}

var errNoEditorLayout = &simpleError{msg: "no layout in the model fixture"}

type simpleError struct{ msg string }

func (e *simpleError) Error() string { return e.msg }

func (e *recordingEditor) ReplaceTextInRange(rng *UTF16Range, text string, w *Window, app *App) {
	e.calls = append(e.calls, "replace:"+rangeString(rng)+"="+text)
	e.Editor.ReplaceTextInRange(rng, text, w, app)
}

func (e *recordingEditor) ReplaceAndMarkTextInRange(rng *UTF16Range, text string, newSelected *UTF16Range, w *Window, app *App) {
	e.calls = append(e.calls, "mark:"+rangeString(rng)+"="+text+":sel="+rangeString(newSelected))
	e.Editor.ReplaceAndMarkTextInRange(rng, text, newSelected, w, app)
}

func rangeString(rng *UTF16Range) string {
	if rng == nil {
		return "nil"
	}
	return fmt.Sprintf("%d..%d", rng.Start, rng.End)
}

// ---------------------------------------------------------------------------
// Composition message routing (events.rs handle_ime_composition_inner)
// ---------------------------------------------------------------------------

func TestIMECompositionZeroLParamClearsComposition(t *testing.T) {
	// A Japanese IME sends WM_IME_COMPOSITION with lParam 0 to report
	// that there is no composition string; the current composition is
	// replaced with empty text and the message is consumed.
	e := newRecordingEditor("AにほんB", 0)
	// Start a composition first so the zero-lParam path has a marked
	// range to clear.
	e.ReplaceAndMarkTextInRange(nil, "にほん", nil, nil, nil)
	if _, ok := e.MarkedTextRange(nil, nil); !ok {
		t.Fatal("expected a marked range before the zero-lParam message")
	}
	seam := &fakeIMESeam{handler: e}
	driver := &imeCompositionDriver{seam: seam, src: &fakeIMECompositionSource{}}

	result, handled := driver.handleComposition(0)
	if !handled || result != 0 {
		t.Fatalf("zero-lParam composition: handled=%v result=%d, want handled=true result=0", handled, result)
	}
	// The fallback chain resolved the marked range, so the empty
	// replacement cleared it.
	if _, ok := e.MarkedTextRange(nil, nil); ok {
		t.Fatal("the zero-lParam path must clear the marked range")
	}
	if e.Text() != "AにほんB" {
		// The zero-lParam replacement cleared the composition inserted
		// at the caret, restoring the pre-composition text.
		t.Errorf("text after the zero-lParam clear = %q, want %q", e.Text(), "AにほんB")
	}
}

func TestIMECompositionZeroLParamWithoutHandlerFallsThrough(t *testing.T) {
	// With no input handler, with_input_handler returns None and the
	// message falls to DefWindowProc (handle_ime_composition_inner's
	// early `?`).
	driver := &imeCompositionDriver{seam: &fakeIMESeam{}, src: &fakeIMECompositionSource{}}
	_, handled := driver.handleComposition(0)
	if handled {
		t.Fatal("zero-lParam without a handler must fall through to DefWindowProc")
	}
}

func TestIMECompositionResultThenMarkOrder(t *testing.T) {
	// GCS_RESULTSTR and GCS_COMPSTR in one message: the committed
	// result replaces the text FIRST, then the new composition string
	// replaces-and-marks (events.rs order; the ticket's "result-string
	// replacement followed by composition/mark update").
	e := newRecordingEditor("", 0)
	seam := &fakeIMESeam{handler: e}
	src := &fakeIMECompositionSource{
		result:   []uint16{'尼', '崎'}, // committed text
		resultOK: true,
		comp:     []uint16{'に', 'ほ', 'ん'}, // new composition
		compOK:   true,
	}
	driver := &imeCompositionDriver{seam: seam, src: src}

	result, handled := driver.handleComposition(uintptr(GCSResultStr | GCSCompStr))
	if !handled || result != 0 {
		t.Fatalf("handled=%v result=%d, want true/0", handled, result)
	}
	wantCalls := []string{
		"replace:nil=尼崎",
		"mark:nil=にほん:sel=nil",
	}
	if len(e.calls) != len(wantCalls) {
		t.Fatalf("calls = %v, want %v", e.calls, wantCalls)
	}
	for i, want := range wantCalls {
		if e.calls[i] != want {
			t.Errorf("call %d = %q, want %q", i, e.calls[i], want)
		}
	}
	if e.Text() != "尼崎にほん" {
		t.Errorf("text = %q, want the committed result followed by the new composition %q", e.Text(), "尼崎にほん")
	}
	// The order proof: with mark-then-result the result replacement
	// would target the just-marked range and clear it, leaving %q with
	// no marked range. Result-then-mark leaves the composition marked
	// after the committed text.
	if marked, ok := e.MarkedTextRange(nil, nil); !ok || marked != (UTF16Range{Start: 2, End: 5}) {
		t.Errorf("marked range = %+v ok=%v, want 2..5 (the new composition after the commit)", marked, ok)
	}
}

func TestIMECompositionCursorPositionPolicy(t *testing.T) {
	// The composition cursor becomes the marked selection only for a
	// non-empty composition with GCS_CURSORPOS, using the IME's
	// position only when it is adjacent to ATTR_INPUT text, else the
	// composition string's length.
	cases := []struct {
		name    string
		comp    []uint16
		flags   uint32
		cursor  int
		attrs   []byte
		wantSel UTF16Range
	}{
		{
			name:  "cursor-adjacent-to-input-uses-ime-position",
			comp:  []uint16{'に', 'ほ', 'ん'},
			flags: GCSCompStr | GCSCursorPos,
			// attrs: [converted, input, input] — cursor at 2 is
			// adjacent to input at 2.
			attrs:   []byte{0x02, 0x00, 0x00},
			cursor:  2,
			wantSel: UTF16Range{Start: 2, End: 2},
		},
		{
			name:  "cursor-not-adjacent-uses-length",
			comp:  []uint16{'に', 'ほ', 'ん'},
			flags: GCSCompStr | GCSCursorPos,
			// attrs: [converted, converted, converted] — no input
			// adjacency, so the composition length (3) wins.
			attrs:   []byte{0x02, 0x02, 0x02},
			cursor:  0,
			wantSel: UTF16Range{Start: 3, End: 3},
		},
		{
			name:  "empty-composition-ignores-cursor",
			comp:  []uint16{},
			flags: GCSCompStr | GCSCursorPos,
			attrs: []byte{0x00},
			// No caret range is passed for an empty composition.
			wantSel: UTF16Range{Start: 0, End: 0},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newRecordingEditor("", 0)
			seam := &fakeIMESeam{handler: e}
			src := &fakeIMECompositionSource{comp: tc.comp, compOK: true, cursor: tc.cursor, attrs: tc.attrs}
			driver := &imeCompositionDriver{seam: seam, src: src}
			if _, handled := driver.handleComposition(uintptr(tc.flags)); !handled {
				t.Fatal("composition must be handled")
			}
			sel, ok := e.SelectedTextRange(false, nil, nil)
			if !ok {
				t.Fatal("no selection after composition")
			}
			if tc.name == "empty-composition-ignores-cursor" {
				// No selection request: the caret lands at the end of
				// the (empty) insert — range 0..0.
				if sel.Range != tc.wantSel {
					t.Errorf("selection = %+v, want %+v", sel.Range, tc.wantSel)
				}
				return
			}
			if sel.Range != tc.wantSel {
				t.Errorf("selection = %+v, want %+v", sel.Range, tc.wantSel)
			}
		})
	}
}

func TestIMECompositionUnhandledFlagCombination(t *testing.T) {
	// "currently, we don't care other stuff": flags without
	// GCS_RESULTSTR/GCS_COMPSTR are not consumed.
	e := newRecordingEditor("", 0)
	driver := &imeCompositionDriver{
		seam: &fakeIMESeam{handler: e},
		src:  &fakeIMECompositionSource{},
	}
	if _, handled := driver.handleComposition(uintptr(GCSCursorPos)); handled {
		t.Fatal("GCS_CURSORPOS alone must fall through to DefWindowProc")
	}
	if _, handled := driver.handleComposition(uintptr(GCSCompAttr)); handled {
		t.Fatal("GCS_COMPATTR alone must fall through to DefWindowProc")
	}
	if len(e.calls) != 0 {
		t.Errorf("unexpected handler calls: %v", e.calls)
	}
}

func TestIMECompositionParseFailureFallsThrough(t *testing.T) {
	// parse_ime_composition_string's None (a negative
	// ImmGetCompositionStringW length) aborts the message to
	// DefWindowProc.
	e := newRecordingEditor("", 0)
	driver := &imeCompositionDriver{
		seam: &fakeIMESeam{handler: e},
		src:  &fakeIMECompositionSource{compOK: false, comp: nil},
	}
	if _, handled := driver.handleComposition(uintptr(GCSCompStr)); handled {
		t.Fatal("a failed composition-string read must fall through")
	}
	if len(e.calls) != 0 {
		t.Errorf("unexpected handler calls: %v", e.calls)
	}
}

func TestIMECompositionReentrySeesNoHandler(t *testing.T) {
	// The take/put discipline: during a composition callback the
	// handler is out of its slot, so a nested query observes no
	// handler. This is the deterministic core of the reentry contract;
	// the real-window reentry test lives in internal/imespec.
	e := newRecordingEditor("", 0)
	seam := &fakeIMESeam{handler: e}
	var nestedSawHandler bool
	outer := &recordingEditor{Editor: e.Editor}
	outer.calls = e.calls
	// Replace the seam's handler with a wrapper that re-queries the
	// seam during the callback.
	seam.handler = &nestedProbeEditor{Editor: e.Editor, seam: seam, saw: &nestedSawHandler}
	src := &fakeIMECompositionSource{comp: []uint16{'に'}, compOK: true}
	driver := &imeCompositionDriver{seam: seam, src: src}
	if _, handled := driver.handleComposition(uintptr(GCSCompStr)); !handled {
		t.Fatal("the outer composition must be handled")
	}
	if nestedSawHandler {
		t.Fatal("the nested query must observe no handler while the outer callback runs")
	}
}

// nestedProbeEditor re-enters the seam during its marked replacement.
type nestedProbeEditor struct {
	*Editor
	seam *fakeIMESeam
	saw  *bool
}

func (e *nestedProbeEditor) ReplaceAndMarkTextInRange(rng *UTF16Range, text string, newSelected *UTF16Range, w *Window, app *App) {
	// Nested message during the callback: the handler was taken, so
	// the seam reports none.
	*e.saw = e.seam.withIMEHandler(func(h IMEInputHandler, w *Window, app *App) {})
	e.Editor.ReplaceAndMarkTextInRange(rng, text, newSelected, w, app)
}

// ---------------------------------------------------------------------------
// Candidate bounds (platform.rs compute_ime_candidate_bounds)
// ---------------------------------------------------------------------------

// fakeGeometryEditor answers bounds queries from a table.
type fakeGeometryEditor struct {
	*recordingEditor
	bounds map[UTF16Range]Bounds
}

func (e *fakeGeometryEditor) BoundsForRange(rng UTF16Range, w *Window, app *App) (Bounds, bool) {
	b, ok := e.bounds[rng]
	return b, ok
}

func TestComputeIMECandidateBounds(t *testing.T) {
	// Composition active: the anchor is the caret's line start — the
	// walk-back finds the visual-line break by the y delta and the
	// line start is one position after the break.
	e := &fakeGeometryEditor{recordingEditor: newRecordingEditor("", 0)}
	e.bounds = map[UTF16Range]Bounds{
		{Start: 0, End: 0}: {Origin: Point{X: 0, Y: 0}},  // previous line
		{Start: 1, End: 1}: {Origin: Point{X: 0, Y: 0}},  // previous line
		{Start: 2, End: 2}: {Origin: Point{X: 4, Y: 20}}, // line start (same line as caret)
		{Start: 3, End: 3}: {Origin: Point{X: 5, Y: 20}}, // caret (range end)
	}
	// "abc" inserted with the composition selection 2..3 (relative):
	// the marked range is 0..3 and the selection's range end is 3.
	e.Editor.ReplaceAndMarkTextInRange(nil, "abc", &UTF16Range{Start: 2, End: 3}, nil, nil)

	got, ok := ComputeIMECandidateBounds(e, nil, nil)
	if !ok {
		t.Fatal("no candidate bounds")
	}
	// The caret (range end 3, y 20); walking back, position 1 sits on
	// the previous line (y 0), so the line start is 2 — NOT 0.
	if got.Origin != (Point{X: 4, Y: 20}) {
		t.Errorf("candidate bounds origin = %+v, want the line-start-after-break {4 20}", got.Origin)
	}
}

func TestComputeIMECandidateBoundsWithoutComposition(t *testing.T) {
	// No marked range: the selection endpoint (end for a forward
	// selection, start for a reversed one).
	e := &fakeGeometryEditor{recordingEditor: newRecordingEditor("abcdefg", 0)}
	e.bounds = map[UTF16Range]Bounds{
		{Start: 7, End: 7}: {Origin: Point{X: 9, Y: 4}},
		{Start: 3, End: 3}: {Origin: Point{X: 2, Y: 4}},
	}
	// Forward: the active caret is at the range's end (7).
	e.setSelection(EditorSelection{Anchor: EditorCaretAttachedToNext(3), Caret: EditorCaretAttachedToNext(7)})
	if got, ok := ComputeIMECandidateBounds(e, nil, nil); !ok || got.Origin != (Point{X: 9, Y: 4}) {
		t.Errorf("forward selection candidate = %+v ok=%v, want {9 4}", got, ok)
	}
	// Reversed: the active caret is at 3, anchor at 7 — the endpoint is
	// the range's start.
	e.setSelection(EditorSelection{Anchor: EditorCaretAttachedToNext(7), Caret: EditorCaretAttachedToNext(3)})
	e.bounds[UTF16Range{Start: 3, End: 3}] = Bounds{Origin: Point{X: 2, Y: 4}}
	if got, ok := ComputeIMECandidateBounds(e, nil, nil); !ok || got.Origin != (Point{X: 2, Y: 4}) {
		t.Errorf("reversed selection candidate = %+v ok=%v, want {2 4}", got, ok)
	}
}
