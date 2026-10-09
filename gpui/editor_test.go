package gpui

// The editor-side model tests (ticket20): the port of the reference's
// ime_composition_uses_relative_utf16_ranges_around_surrogate_pairs
// (crates/gpui_elements/src/editable_text/state.rs) plus the bounded
// model behaviors the IME relies on (replacement fallback chain,
// single-line validation, adjusted ranges, point lookup). The model
// operations need no text geometry: the shape provider is a stub, so
// these tests run without the native text stack (geometry tests live
// in internal/imespec with the real shaping).

import "testing"

// stubShape is the no-geometry layout provider.
func stubShape(string) (*WrappedLine, error) {
	return nil, errNoEditorLayout
}

func newModelEditor(text string, caret int) *Editor {
	return NewEditor(text, caret, EditorOptions{Shape: stubShape})
}

// TestEditorIMECompositionRelativeUTF16RangesAroundSurrogatePairs is
// the port of the reference test with the same name: marked ranges are
// document UTF-16 offsets while composition selections are relative to
// the inserted text.
func TestEditorIMECompositionRelativeUTF16RangesAroundSurrogatePairs(t *testing.T) {
	// "A😀B": A is UTF-16 0..1, 😀 (surrogate pair) 1..3, B 3..4.
	input := newModelEditor("A😀B", 0)

	input.ReplaceAndMarkTextInRange(&UTF16Range{Start: 1, End: 3}, "にほん", &UTF16Range{Start: 1, End: 2}, nil, nil)
	if input.Text() != "AにほんB" {
		t.Fatalf("as_str() = %q, want %q", input.Text(), "AにほんB")
	}
	// The reference's accessibility metrics (character UTF-8 lengths
	// [1,3,3,3,1], byte offsets [0,1,4,7,8]) pin the same storage
	// facts; assert them directly.
	var lengths []int
	var offsets []int
	for i, r := range input.Text() {
		lengths = append(lengths, len(string(r)))
		offsets = append(offsets, i)
	}
	if equalInts(lengths, []int{1, 3, 3, 3, 1}) {
		// lengths: A(1), に/ほ/ん(3 each), B(1)
	} else if !equalInts(lengths, []int{1, 3, 3, 3, 1}) {
		t.Errorf("character UTF-8 lengths = %v, want [1 3 3 3 1]", lengths)
	}
	offsets = append(offsets, len(input.Text()))
	if !equalInts(offsets, []int{0, 1, 4, 7, 10, 11}) {
		t.Errorf("character byte offsets = %v, want [0 1 4 7 10 11]", offsets)
	}
	marked, ok := input.MarkedTextRange(nil, nil)
	if !ok || marked != (UTF16Range{Start: 1, End: 4}) {
		t.Errorf("marked_text_range = %+v ok=%v, want 1..4 (document UTF-16 offsets)", marked, ok)
	}
	sel, _ := input.SelectedTextRange(false, nil, nil)
	if sel.Range != (UTF16Range{Start: 2, End: 3}) {
		t.Errorf("selected_text_range = %+v, want 2..3 (relative to the inserted text)", sel.Range)
	}

	input.ReplaceAndMarkTextInRange(nil, "日本", nil, nil, nil)
	if input.Text() != "A日本B" {
		t.Fatalf("as_str() = %q, want %q", input.Text(), "A日本B")
	}
	if marked, ok := input.MarkedTextRange(nil, nil); !ok || marked != (UTF16Range{Start: 1, End: 3}) {
		t.Errorf("marked_text_range = %+v ok=%v, want 1..3", marked, ok)
	}
	sel, _ = input.SelectedTextRange(false, nil, nil)
	if sel.Range != (UTF16Range{Start: 3, End: 3}) {
		t.Errorf("selected_text_range = %+v, want 3..3 (caret at the insert's end)", sel.Range)
	}

	input.UnmarkText(nil, nil)
	if _, ok := input.MarkedTextRange(nil, nil); ok {
		t.Error("marked_text_range after unmark must be none")
	}
	sel, _ = input.SelectedTextRange(false, nil, nil)
	if sel.Range != (UTF16Range{Start: 3, End: 3}) {
		t.Errorf("selected_text_range after unmark = %+v, want 3..3 (unmark keeps the selection)", sel.Range)
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestEditorReplaceRangeFallbackChain pins ime_resolve_range: the
// IME-provided range wins, then the marked range, then the selection.
func TestEditorReplaceRangeFallbackChain(t *testing.T) {
	e := newModelEditor("hello world", 0)

	// No marked range: nil replaces the selection (2..6 = "llo ").
	e.setSelection(EditorSelection{Anchor: EditorCaretAttachedToNext(2), Caret: EditorCaretAttachedToNext(6)})
	e.ReplaceTextInRange(nil, "XY", nil, nil)
	if e.Text() != "heXYworld" {
		t.Fatalf("selection fallback text = %q, want heXYworld", e.Text())
	}

	// With a marked range: nil replaces the marked range even though
	// the selection sits elsewhere (mark "world", select "he", then
	// replace nil — the marked range, not the selection, goes).
	e3 := newModelEditor("hello world", 0)
	e3.ReplaceAndMarkTextInRange(&UTF16Range{Start: 6, End: 11}, "MARK", nil, nil, nil)
	e3.setSelection(EditorSelectionFromRange(0, 2))
	e3.ReplaceTextInRange(nil, "-", nil, nil)
	if e3.Text() != "hello -" { // "hello " + "-" for the marked "MARK"
		t.Fatalf("marked-range fallback text = %q, want the marked range replaced (hello -)", e3.Text())
	}

	// An explicit range always wins.
	e2 := newModelEditor("abcdef", 0)
	e2.ReplaceTextInRange(&UTF16Range{Start: 1, End: 3}, "Z", nil, nil)
	if e2.Text() != "aZdef" {
		t.Errorf("explicit range text = %q, want aZdef (bc replaced by Z)", e2.Text())
	}
}

// TestEditorReplaceClampsOutOfRangeRanges pins ime_resolve_range's
// storage clamp: past-the-end UTF-16 offsets clamp to the length.
func TestEditorReplaceClampsOutOfRangeRanges(t *testing.T) {
	e := newModelEditor("abc", 3)
	e.ReplaceTextInRange(&UTF16Range{Start: 2, End: 100}, "X", nil, nil)
	if e.Text() != "abX" {
		t.Errorf("text = %q, want abX (end clamped to the length)", e.Text())
	}
	// Inside a surrogate pair: the conversion snaps to the character
	// end, so the replacement never cuts a pair.
	e2 := newModelEditor("A😀B", 0)
	e2.ReplaceTextInRange(&UTF16Range{Start: 1, End: 2}, "X", nil, nil)
	if e2.Text() != "AXB" {
		t.Errorf("text = %q, want AXB (mid-pair end snapped past 😀)", e2.Text())
	}
}

// TestEditorSingleLineValidation pins validate_incoming_text:
// single-line fields strip CR/LF, multiline keeps them.
func TestEditorSingleLineValidation(t *testing.T) {
	one := NewEditor("", 0, EditorOptions{Shape: stubShape})
	one.ReplaceTextInRange(nil, "a\r\nb", nil, nil)
	if one.Text() != "ab" {
		t.Errorf("single-line text = %q, want ab", one.Text())
	}
	multi := NewEditor("", 0, EditorOptions{Shape: stubShape, Multiline: true})
	multi.ReplaceTextInRange(nil, "a\r\nb", nil, nil)
	if multi.Text() != "a\r\nb" {
		t.Errorf("multiline text = %q, want a\\r\\nb", multi.Text())
	}
}

// TestEditorInsertAffinity pins replace_text's selection affinity: the
// caret after inserted text attaches upstream, except after hard line
// breaks (CaretAffinity::for_inserted_text).
func TestEditorInsertAffinity(t *testing.T) {
	e := newModelEditor("", 0)
	e.ReplaceTextInRange(nil, "abc", nil, nil)
	if e.Caret().Affinity != CaretAffinityUpstream {
		t.Errorf("affinity after plain text = %v, want upstream", e.Caret().Affinity)
	}
	// A single-line editor strips the newline before it lands, so the
	// affinity follows the plain text; a multiline editor sees the hard
	// line break and places the caret downstream.
	multi := NewEditor("", 0, EditorOptions{Shape: stubShape, Multiline: true})
	multi.ReplaceTextInRange(nil, "x\n", nil, nil)
	if multi.Caret().Affinity != CaretAffinityDownstream {
		t.Errorf("affinity after a newline = %v, want downstream", multi.Caret().Affinity)
	}
}

// TestEditorTextForRangeAdjustedRange pins text_for_range: the clamped
// range is reported back through the adjusted range (the
// surrounding-text contract).
func TestEditorTextForRangeAdjustedRange(t *testing.T) {
	e := newModelEditor("A😀B", 0)
	// 0..9 clamps to the length; the adjusted range reports it.
	text, adjusted, ok := e.TextForRange(UTF16Range{Start: 0, End: 9}, nil, nil)
	if !ok || text != "A😀B" {
		t.Fatalf("text = %q ok=%v, want the whole document", text, ok)
	}
	if adjusted == nil || *adjusted != (UTF16Range{Start: 0, End: 4}) {
		t.Errorf("adjusted = %+v, want 0..4", adjusted)
	}
	// A range ending inside the surrogate pair snaps to the pair's
	// end; the adjusted range reports the snapped (whole-character)
	// range.
	text, adjusted, ok = e.TextForRange(UTF16Range{Start: 0, End: 2}, nil, nil)
	if !ok || text != "A😀" {
		t.Fatalf("text = %q ok=%v, want A😀 (the end snaps past the pair)", text, ok)
	}
	if adjusted == nil || *adjusted != (UTF16Range{Start: 0, End: 3}) {
		t.Errorf("adjusted = %+v, want 0..3", adjusted)
	}
}

// TestEditorTextLengthUTF16 pins the UTF-16 length report.
func TestEditorTextLengthUTF16(t *testing.T) {
	e := newModelEditor("A😀B", 0)
	if n, ok := e.TextLengthUTF16(nil, nil); !ok || n != 4 {
		t.Errorf("text_length_utf16 = %d ok=%v, want 4", n, ok)
	}
}

// TestEditorSelectionQueries pins the selection query surface: the
// reversed flag and UTF-16 conversion.
func TestEditorSelectionQueries(t *testing.T) {
	e := newModelEditor("A😀B", 0)
	e.setSelection(EditorSelection{Anchor: EditorCaretAttachedToNext(5), Caret: EditorCaretAttachedToNext(1)})
	sel, ok := e.SelectedTextRange(false, nil, nil)
	if !ok || sel.Range != (UTF16Range{Start: 1, End: 3}) || !sel.Reversed {
		t.Errorf("selected = %+v ok=%v, want 1..3 reversed", sel, ok)
	}
	// SetSelectedTextRange is the reference's default no-op.
	e.SetSelectedTextRange(UTF16Range{Start: 0, End: 1}, nil, nil)
	if sel, _ := e.SelectedTextRange(false, nil, nil); sel.Range != (UTF16Range{Start: 1, End: 3}) {
		t.Errorf("set_selected_text_range must stay a no-op in this slice, got %+v", sel.Range)
	}
}

// TestEditorZeroLParamCancelAndCommitFlow drives the full model flow
// the host's zero-lParam and commit paths produce.
func TestEditorZeroLParamCancelAndCommitFlow(t *testing.T) {
	// Composition in progress: にほん inserted at the caret (5).
	e := newModelEditor("hello world", 5)
	e.ReplaceAndMarkTextInRange(nil, "にほん", &UTF16Range{Start: 0, End: 3}, nil, nil)
	if e.Text() != "helloにほん world" {
		t.Fatalf("composed text = %q, want helloにほん world", e.Text())
	}
	// Cancel (zero-lParam): the marked range is replaced with empty.
	e.ReplaceTextInRange(nil, "", nil, nil)
	if e.Text() != "hello world" {
		t.Errorf("cancelled text = %q, want the pre-composition text", e.Text())
	}
	if _, ok := e.MarkedTextRange(nil, nil); ok {
		t.Error("cancel must clear the marked range")
	}
	// Commit (GCS_RESULTSTR): the marked composition is replaced by
	// the plain committed text and the marked range clears.
	e.ReplaceAndMarkTextInRange(nil, "日本", nil, nil, nil)
	e.ReplaceTextInRange(nil, "日本", nil, nil)
	if e.Text() != "hello日本 world" {
		t.Errorf("committed text = %q, want hello日本 world", e.Text())
	}
	if _, ok := e.MarkedTextRange(nil, nil); ok {
		t.Error("commit must clear the marked range")
	}
}

// TestEditorGeometryUnavailable pins current_document's error path:
// bounds and point queries report not-ok when the layout provider
// fails (the reference's Err(OldDocumentVersion) -> None).
func TestEditorGeometryUnavailable(t *testing.T) {
	e := newModelEditor("abc", 0)
	if _, ok := e.BoundsForRange(UTF16Range{Start: 0, End: 0}, nil, nil); ok {
		t.Error("bounds must be unavailable without a layout")
	}
	if _, ok := e.CharacterIndexForPoint(Point{X: 1, Y: 1}, nil, nil); ok {
		t.Error("point lookup must be unavailable without a layout")
	}
}

// TestEditorVersionAndChangeNotifications pins the storage version and
// the text-changed callback (bounded TextChanged port).
func TestEditorVersionAndChangeNotifications(t *testing.T) {
	e := newModelEditor("abc", 0)
	version := e.Version()
	changes := 0
	e.OnTextChanged(func() { changes++ })
	e.ReplaceTextInRange(nil, "d", nil, nil)
	if e.Version() == version {
		t.Error("the storage version must advance on mutation")
	}
	if changes != 1 {
		t.Errorf("text-changed callbacks = %d, want 1", changes)
	}
	e.UnmarkText(nil, nil)
	if changes != 1 {
		t.Errorf("unmark must not fire text-changed, got %d", changes)
	}
}
