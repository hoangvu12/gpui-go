package base

// Port of U:input/base/selection.rs: CursorId and TextSelector. The
// select_word/select_line methods on InputBaseState (U:input/base/
// selection.rs:21-56) belong to the input state ticket and are not ported
// here.

import (
	"unicode/utf8"

	"github.com/hoangvu12/gpui-go/base/rope"
	"github.com/hoangvu12/gpui-go/base/segment"
	"github.com/hoangvu12/gpui-go/base/sumtree"
)

// cursorID uniquely identifies a cursor/selection
// (U:input/base/selection.rs:11-19).
type cursorID int

// newCursorID is CursorId::new (U:input/base/selection.rs:16-18).
func newCursorID(id int) cursorID { return cursorID(id) }

// textSelectorLineRange returns the line around offset (clipped left),
// without its line break (TextSelector::line_range,
// U:input/base/selection.rs:60-72).
func textSelectorLineRange(text *rope.Rope, offset int) (start, end int) {
	ext := RopeExtOf(text)
	offset = ext.ClipOffset(offset, sumtree.Left)
	row := ext.OffsetToPoint(offset).Row
	return ext.LineStartOffset(row), ext.LineEndOffset(row)
}

// textSelectorWordRange returns the word or whitespace run around offset
// (clipped left); ok is false at the end of the text
// (TextSelector::word_range, U:input/base/selection.rs:74-89).
func textSelectorWordRange(text *rope.Rope, offset int) (start, end int, ok bool) {
	ext := RopeExtOf(text)
	offset = ext.ClipOffset(offset, sumtree.Left)
	char, ok := ext.CharAt(offset)
	if !ok {
		return 0, 0, false
	}
	charEnd := offset + utf8.RuneLen(char)
	// segment.WordRangeFromChars applies the upstream take(128) itself.
	prev := func(yield func(rune) bool) {
		it := text.CharsAt(offset).Reversed()
		for c, more := it.Next(); more && yield(c); c, more = it.Next() {
		}
	}
	next := func(yield func(rune) bool) {
		it := text.CharsAt(charEnd)
		for c, more := it.Next(); more && yield(c); c, more = it.Next() {
		}
	}
	start, end = segment.WordRangeFromChars(offset, char, prev, next)
	return start, end, true
}
