package base

// Port of U:input/base/rope_ext.rs (RopeExt and its value types).
//
// Naming (doc.go): Rust's `input::Point` and `input::Position` (the
// re-exported lsp_types::Position, U:input/mod.rs:113,119) take the Input
// prefix as InputPoint and InputPosition because Point and Position are
// plausible crate-root names. RopeExt is an extension trait on the foreign
// ropey::Rope, so it becomes the wrapper type built by RopeExtOf(rope).

import (
	"unicode"

	"github.com/hoangvu12/gpui-go/base/rope"
	"github.com/hoangvu12/gpui-go/base/sumtree"
)

// InputPoint is the parser-independent byte row/column position used for
// incremental edits (`input::Point`, U:input/base/rope_ext.rs:6-17). Column
// is in bytes.
type InputPoint struct {
	Row    int
	Column int
}

// NewInputPoint is Point::new (U:input/base/rope_ext.rs:14-16).
func NewInputPoint(row, column int) InputPoint { return InputPoint{Row: row, Column: column} }

// InputPosition is the line/character position re-exported from
// lsp_types::Position (U:input/mod.rs:113): zero-based line and character
// offsets, both u32 upstream. RopeExt counts Character in chars
// (U:input/base/rope_ext.rs:190-198).
type InputPosition struct {
	Line      uint32
	Character uint32
}

// NewInputPosition is lsp_types::Position::new.
func NewInputPosition(line, character uint32) InputPosition {
	return InputPosition{Line: line, Character: character}
}

// InputEdit is the parser-independent description of an incremental text
// replacement (U:input/base/rope_ext.rs:19-28).
type InputEdit struct {
	StartByte      int
	OldEndByte     int
	NewEndByte     int
	StartPosition  InputPoint
	OldEndPosition InputPoint
	NewEndPosition InputPoint
}

// RopeLines iterates the lines of a rope; each line includes `\r` when
// present but not `\n` (U:input/base/rope_ext.rs:32-78). It holds an O(1)
// clone of the rope, so later edits to the source do not affect it.
type RopeLines struct {
	rope   rope.Rope
	row    int
	endRow int
}

// NewRopeLines is RopeLines::new (U:input/base/rope_ext.rs:39-49).
func NewRopeLines(r *rope.Rope) *RopeLines {
	return &RopeLines{rope: *r, endRow: RopeExtOf(r).LinesLen()}
}

// Next returns the next line; ok is false at the end
// (U:input/base/rope_ext.rs:53-62).
func (l *RopeLines) Next() (line rope.RopeSlice, ok bool) {
	if l.row >= l.endRow {
		return rope.RopeSlice{}, false
	}
	line = RopeExtOf(&l.rope).SliceLine(l.row)
	l.row++
	return line, true
}

// Nth skips n lines and returns the next one (U:input/base/rope_ext.rs:64-68).
func (l *RopeLines) Nth(n int) (rope.RopeSlice, bool) {
	l.row += n
	return l.Next()
}

// Len is the number of lines left (size_hint / ExactSizeIterator,
// U:input/base/rope_ext.rs:70-77).
func (l *RopeLines) Len() int { return max(l.endRow-l.row, 0) }

// RopeExt is the RopeExt extension trait (U:input/base/rope_ext.rs:80-275)
// over one rope. All offsets are bytes unless the method says otherwise.
type RopeExt struct {
	rope *rope.Rope
}

// RopeExtOf wraps a rope for the RopeExt methods. Replace edits r in place.
func RopeExtOf(r *rope.Rope) RopeExt { return RopeExt{rope: r} }

// SliceLine returns the line at row (0-based), including `\r` if present
// but not `\n`; empty when row is out of bounds
// (U:input/base/rope_ext.rs:278-293).
func (e RopeExt) SliceLine(row int) rope.RopeSlice {
	r := e.rope
	if row >= e.LinesLen() {
		return r.Slice(0, 0)
	}
	line := r.Line(row, rope.LF)
	if line.Len() > 0 {
		lineEnd := line.Len() - 1
		if line.IsCharBoundary(lineEnd) && line.Char(lineEnd) == '\n' {
			return line.Slice(0, lineEnd)
		}
	}
	return line
}

// SliceLines returns rows [start, end) clamped to the valid range
// (U:input/base/rope_ext.rs:295-299).
func (e RopeExt) SliceLines(start, end int) rope.RopeSlice {
	s := e.LineStartOffset(start)
	t := e.LineEndOffset(max(end-1, 0))
	return e.rope.Slice(s, t)
}

// IterLines returns an iterator over all lines
// (U:input/base/rope_ext.rs:301-303).
func (e RopeExt) IterLines() *RopeLines { return NewRopeLines(e.rope) }

// LineLen is the byte length of row, including `\r` but not `\n`; 0 when
// out of bounds (U:input/base/rope_ext.rs:305-307).
func (e RopeExt) LineLen(row int) int { return e.SliceLine(row).Len() }

// LineStartOffset is the start offset of row
// (U:input/base/rope_ext.rs:309-311).
func (e RopeExt) LineStartOffset(row int) int { return e.PointToOffset(NewInputPoint(row, 0)) }

// OffsetToPoint converts a byte offset (clipped left) to a byte
// row/column (U:input/base/rope_ext.rs:313-319).
func (e RopeExt) OffsetToPoint(offset int) InputPoint {
	offset = e.ClipOffset(offset, sumtree.Left)
	row := e.rope.ByteToLineIdx(offset, rope.LF)
	lineStart := e.rope.LineToByteIdx(row, rope.LF)
	return NewInputPoint(row, max(offset-lineStart, 0))
}

// PointToOffset converts a byte row/column to a byte offset; the rope
// length when the row is out of bounds (U:input/base/rope_ext.rs:321-328).
func (e RopeExt) PointToOffset(point InputPoint) int {
	if point.Row >= e.LinesLen() {
		return e.rope.Len()
	}
	return e.rope.LineToByteIdx(point.Row, rope.LF) + point.Column
}

// PositionToOffset converts a line/char-column position to a byte offset,
// clamping the column to the line (U:input/base/rope_ext.rs:330-335).
func (e RopeExt) PositionToOffset(pos InputPosition) int {
	line := e.SliceLine(int(pos.Line))
	return e.LineStartOffset(int(pos.Line)) +
		line.CharToByteIdx(min(int(pos.Character), line.LenChars()))
}

// OffsetToPosition converts a byte offset to a line/char-column position
// (U:input/base/rope_ext.rs:337-343).
func (e RopeExt) OffsetToPosition(offset int) InputPosition {
	point := e.OffsetToPoint(offset)
	line := e.SliceLine(point.Row)
	off := line.UTF16ToByteIdx(line.ByteToUTF16Idx(point.Column))
	character := line.Slice(0, off).LenChars()
	return NewInputPosition(uint32(point.Row), uint32(character))
}

// LineEndOffset is the end offset of row, excluding `\n`; the rope length
// when row > LinesLen (U:input/base/rope_ext.rs:345-351).
func (e RopeExt) LineEndOffset(row int) int {
	if row > e.LinesLen() {
		return e.rope.Len()
	}
	return e.LineStartOffset(row) + e.LineLen(row)
}

// LinesLen is the number of LF lines (U:input/base/rope_ext.rs:353-355).
func (e RopeExt) LinesLen() int { return e.rope.LenLines(rope.LF) }

// CharAt returns the char at offset; ok is false when offset is out of
// bounds (U:input/base/rope_ext.rs:357-363). Upstream panics inside a
// multi-byte char; the Go rope's GetChar reports that as not ok.
func (e RopeExt) CharAt(offset int) (rune, bool) {
	if offset > e.rope.Len() {
		return 0, false
	}
	c, err := e.rope.GetChar(offset)
	return c, err == nil
}

// isAlphanumeric is Rust's char::is_alphanumeric: the Alphabetic property
// (letters, Nl, Other_Alphabetic) or the Numeric categories (Nd, Nl, No).
func isAlphanumeric(c rune) bool {
	return unicode.IsLetter(c) || unicode.IsNumber(c) || unicode.Is(unicode.Other_Alphabetic, c)
}

// WordRange returns the byte range of the alphanumeric/underscore word
// around offset; ok is false at or past the end or when no word touches
// offset (U:input/base/rope_ext.rs:365-389).
func (e RopeExt) WordRange(offset int) (start, end int, ok bool) {
	if offset >= e.rope.Len() {
		return 0, 0, false
	}
	isWord := func(c rune) bool { return isAlphanumeric(c) || c == '_' }
	offset = e.ClipOffset(offset, sumtree.Left)
	left := 0
	back := e.rope.CharsAt(offset).Reversed()
	for c, more := back.Next(); more && isWord(c); c, more = back.Next() {
		left += len(string(c))
	}
	start = max(offset-left, 0)
	right := 0
	fwd := e.rope.CharsAt(offset)
	for c, more := fwd.Next(); more && isWord(c); c, more = fwd.Next() {
		right += len(string(c))
	}
	end = offset + right
	if start == end {
		return 0, 0, false
	}
	return start, end, true
}

// WordAt returns the word at offset, or "" (U:input/base/rope_ext.rs:391-397).
func (e RopeExt) WordAt(offset int) string {
	if s, t, ok := e.WordRange(offset); ok {
		return e.rope.Slice(s, t).String()
	}
	return ""
}

// OffsetUTF16ToOffset converts a UTF-16 offset to a byte offset; the rope
// length when out of bounds (U:input/base/rope_ext.rs:399-406).
func (e RopeExt) OffsetUTF16ToOffset(offsetUTF16 int) int {
	if offsetUTF16 > e.rope.LenUTF16() {
		return e.rope.Len()
	}
	return e.rope.UTF16ToByteIdx(offsetUTF16)
}

// OffsetToOffsetUTF16 converts a byte offset to a UTF-16 offset; the UTF-16
// length when out of bounds (U:input/base/rope_ext.rs:408-415).
func (e RopeExt) OffsetToOffsetUTF16(offset int) int {
	if offset > e.rope.Len() {
		return e.rope.LenUTF16()
	}
	return e.rope.ByteToUTF16Idx(offset)
}

// Replace replaces [start, end) with newText, clipping start left and end
// right to char boundaries (U:input/base/rope_ext.rs:417-422).
func (e RopeExt) Replace(start, end int, newText string) {
	start, end = e.ClipOffset(start, sumtree.Left), e.ClipOffset(end, sumtree.Right)
	e.rope.Remove(start, end)
	e.rope.Insert(start, newText)
}

// ClipOffset moves offset off the inside of a char: to its start for
// sumtree.Left, its end for sumtree.Right; offsets past the end clip to the
// rope length (U:input/base/rope_ext.rs:424-438).
func (e RopeExt) ClipOffset(offset int, bias sumtree.Bias) int {
	if offset > e.rope.Len() {
		return e.rope.Len()
	}
	if e.rope.IsCharBoundary(offset) {
		return offset
	}
	if bias == sumtree.Left {
		return e.rope.FloorCharBoundary(offset)
	}
	return e.rope.CeilCharBoundary(offset)
}

// CharIndexToOffset converts a char index to a byte offset, O(n)
// (U:input/base/rope_ext.rs:440-442).
func (e RopeExt) CharIndexToOffset(charIndex int) int {
	n := 0
	it := e.rope.Chars()
	for ; charIndex > 0; charIndex-- {
		c, ok := it.Next()
		if !ok {
			break
		}
		n += len(string(c))
	}
	return n
}

// OffsetToCharIndex converts a byte offset (clipped right) to a char index,
// O(n) (U:input/base/rope_ext.rs:444-447).
func (e RopeExt) OffsetToCharIndex(offset int) int {
	offset = e.ClipOffset(offset, sumtree.Right)
	return e.rope.Slice(0, offset).LenChars()
}
