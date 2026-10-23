package rope

import (
	"strings"
	"unicode/utf8"
)

// RopeSlice is an immutable view of a byte range of a rope
// (ropey/src/slice.rs). Its methods are the ones ropey shares between Rope and
// RopeSlice (ropey/src/shared_impl.rs); all indices are relative to the
// slice. The zero value is an empty slice.
type RopeSlice struct {
	root       *node
	start, end int
}

// SliceFromStr is `RopeSlice::from(&str)` (used by Base's indent tests,
// input/editor/indent.rs:482-488).
func SliceFromStr(s string) RopeSlice {
	r := FromStr(s)
	return r.RopeSlice
}

func (s RopeSlice) tree() *node {
	if s.root == nil {
		return emptyLeaf
	}
	return s.root
}

func (s RopeSlice) full() bool { return s.start == 0 && s.end == s.tree().info.bytes }

func oob() { panic(ErrOutOfBounds) }

// --- queries (ropey/src/shared_impl.rs:18-198) ------------------------------

// Len is the length of the text in bytes.
func (s RopeSlice) Len() int { return s.end - s.start }

// LenChars is the length of the text in chars.
func (s RopeSlice) LenChars() int {
	if s.full() {
		return s.tree().info.chars
	}
	return s.tree().prefix(s.end).chars - s.tree().prefix(s.start).chars
}

// LenUTF16 is the number of UTF-16 code units the text would encode to.
func (s RopeSlice) LenUTF16() int {
	if s.full() {
		return s.tree().info.utf16
	}
	return s.tree().prefix(s.end).utf16 - s.tree().prefix(s.start).utf16
}

// LenLines is the number of lines (line breaks + 1) for lineType.
func (s RopeSlice) LenLines(lineType LineType) int {
	mustLF(lineType)
	if s.full() {
		return s.tree().info.lf + 1
	}
	return s.tree().prefix(s.end).lf - s.tree().prefix(s.start).lf + 1
}

// IsCharBoundary reports whether byteIdx is a char boundary. Panics if
// byteIdx > Len() (ropey/src/shared_impl.rs:101-119).
func (s RopeSlice) IsCharBoundary(byteIdx int) bool {
	if byteIdx < 0 || byteIdx > s.Len() {
		oob()
	}
	if byteIdx == s.Len() {
		return true
	}
	text, off := s.tree().chunkAt(s.start + byteIdx)
	return isBoundary(text, s.start+byteIdx-off)
}

// FloorCharBoundary returns the closest char boundary <= byteIdx. Panics if
// byteIdx > Len() (ropey/src/shared_impl.rs:121-140).
func (s RopeSlice) FloorCharBoundary(byteIdx int) int {
	if byteIdx < 0 || byteIdx > s.Len() {
		oob()
	}
	for byteIdx > 0 && !s.IsCharBoundary(byteIdx) {
		byteIdx--
	}
	return byteIdx
}

// CeilCharBoundary returns the closest char boundary >= byteIdx. Panics if
// byteIdx > Len() (ropey/src/shared_impl.rs:142-161).
func (s RopeSlice) CeilCharBoundary(byteIdx int) int {
	if byteIdx < 0 || byteIdx > s.Len() {
		oob()
	}
	for byteIdx < s.Len() && !s.IsCharBoundary(byteIdx) {
		byteIdx++
	}
	return byteIdx
}

// --- fetching (ropey/src/shared_impl.rs:200-289, 985-1089) ------------------

// Byte returns the byte at byteIdx. Panics if byteIdx >= Len().
func (s RopeSlice) Byte(byteIdx int) byte {
	b, ok := s.GetByte(byteIdx)
	if !ok {
		oob()
	}
	return b
}

// GetByte is the non-panicking Byte; ok is false when out of bounds.
func (s RopeSlice) GetByte(byteIdx int) (byte, bool) {
	if byteIdx < 0 || byteIdx >= s.Len() {
		return 0, false
	}
	text, off := s.tree().chunkAt(s.start + byteIdx)
	return text[s.start+byteIdx-off], true
}

// Char returns the char starting at byte index byteIdx. Panics if byteIdx is
// out of bounds (>= Len()) or not a char boundary.
func (s RopeSlice) Char(byteIdx int) rune {
	r, err := s.GetChar(byteIdx)
	if err != nil {
		panic(err)
	}
	return r
}

// GetChar is the non-panicking Char: ErrOutOfBounds when byteIdx >= Len(),
// ErrNonCharBoundary when byteIdx is inside a char.
func (s RopeSlice) GetChar(byteIdx int) (rune, error) {
	if byteIdx < 0 || byteIdx >= s.Len() {
		return 0, ErrOutOfBounds
	}
	text, off := s.tree().chunkAt(s.start + byteIdx)
	local := s.start + byteIdx - off
	if !isBoundary(text, local) {
		return 0, ErrNonCharBoundary
	}
	r, _ := utf8.DecodeRuneInString(text[local:])
	return r, nil
}

// Line returns line lineIdx including its line break. Panics if
// lineIdx >= LenLines().
func (s RopeSlice) Line(lineIdx int, lineType LineType) RopeSlice {
	l, ok := s.GetLine(lineIdx, lineType)
	if !ok {
		oob()
	}
	return l
}

// GetLine is the non-panicking Line; ok is false when out of bounds.
func (s RopeSlice) GetLine(lineIdx int, lineType LineType) (RopeSlice, bool) {
	if lineIdx < 0 || lineIdx >= s.LenLines(lineType) {
		return RopeSlice{}, false
	}
	a := s.LineToByteIdx(lineIdx, lineType)
	b := s.LineToByteIdx(lineIdx+1, lineType)
	return s.Slice(a, b), true
}

// Chunk returns the chunk containing byteIdx and the chunk's start byte
// index; byteIdx == Len() returns the last chunk. Panics if byteIdx > Len().
func (s RopeSlice) Chunk(byteIdx int) (string, int) {
	if byteIdx < 0 || byteIdx > s.Len() {
		oob()
	}
	return s.chunk(byteIdx)
}

// chunk returns the leaf at slice offset i trimmed to the slice, and its start
// in slice coordinates (ropey/src/shared_impl.rs:1061-1089).
func (s RopeSlice) chunk(i int) (string, int) {
	text, off := s.tree().chunkAt(s.start + i)
	lo := max(s.start-off, 0)
	hi := min(s.end-off, len(text))
	if hi < lo {
		hi = lo
	}
	return text[lo:hi], max(off-s.start, 0)
}

// --- metric conversions (ropey/src/shared_impl.rs:291-523) ------------------

// ByteToCharIdx returns the index of the char that byteIdx belongs to;
// byteIdx == Len() gives LenChars(). Panics if byteIdx > Len().
func (s RopeSlice) ByteToCharIdx(byteIdx int) int {
	if byteIdx < 0 || byteIdx > s.Len() {
		oob()
	}
	if byteIdx == s.Len() {
		return s.LenChars()
	}
	return s.tree().prefix(s.start+byteIdx).chars - s.tree().prefix(s.start).chars
}

// CharToByteIdx returns the byte index of char charIdx; charIdx == LenChars()
// gives Len(). Panics if charIdx > LenChars().
func (s RopeSlice) CharToByteIdx(charIdx int) int {
	if charIdx < 0 || charIdx > s.LenChars() {
		oob()
	}
	base := s.tree().prefix(s.start).chars
	return min(s.tree().charStart(base+charIdx), s.end) - s.start
}

// ByteToUTF16Idx returns the UTF-16 index of the char that byteIdx belongs
// to. Panics if byteIdx > Len().
func (s RopeSlice) ByteToUTF16Idx(byteIdx int) int {
	if byteIdx < 0 || byteIdx > s.Len() {
		oob()
	}
	if byteIdx == s.Len() {
		return s.LenUTF16()
	}
	return s.tree().prefix(s.start+byteIdx).utf16 - s.tree().prefix(s.start).utf16
}

// UTF16ToByteIdx returns the byte index of the char that UTF-16 unit
// utf16Idx belongs to (the char start when it falls between a surrogate
// pair). Panics if utf16Idx > LenUTF16().
func (s RopeSlice) UTF16ToByteIdx(utf16Idx int) int {
	if utf16Idx < 0 || utf16Idx > s.LenUTF16() {
		oob()
	}
	base := s.tree().prefix(s.start).utf16
	return min(s.tree().utf16Start(base+utf16Idx), s.end) - s.start
}

// ByteToLineIdx returns the index of the line byteIdx belongs to, i.e. the
// number of line breaks before it. Panics if byteIdx > Len().
func (s RopeSlice) ByteToLineIdx(byteIdx int, lineType LineType) int {
	mustLF(lineType)
	if byteIdx < 0 || byteIdx > s.Len() {
		oob()
	}
	return s.tree().prefix(s.start+byteIdx).lf - s.tree().prefix(s.start).lf
}

// LineToByteIdx returns the byte index of the start of line lineIdx;
// lineIdx == LenLines() gives Len(). Panics if lineIdx > LenLines().
func (s RopeSlice) LineToByteIdx(lineIdx int, lineType LineType) int {
	if lineIdx < 0 || lineIdx > s.LenLines(lineType) {
		oob()
	}
	first := s.tree().prefix(s.start).lf
	b := s.tree().lineStart(first+lineIdx) - s.start
	return min(max(b, 0), s.Len())
}

// --- slicing (ropey/src/slice.rs:116-200) -----------------------------------

// Slice returns the sub-slice [start, end). Panics if start > end, end >
// Len(), or either end is not a char boundary.
func (s RopeSlice) Slice(start, end int) RopeSlice {
	sl, err := s.TrySlice(start, end)
	if err != nil {
		panic(err)
	}
	return sl
}

// TrySlice is the non-panicking Slice. Bounds are checked before char
// boundaries; ropey's try_slice would instead panic inside is_char_boundary
// for an out-of-bounds end (ropey/src/rope.rs:693-703).
func (s RopeSlice) TrySlice(start, end int) (RopeSlice, error) {
	if start < 0 || end > s.Len() || start > s.Len() {
		return RopeSlice{}, ErrOutOfBounds
	}
	if start > end {
		return RopeSlice{}, ErrInvalidRange
	}
	if !s.IsCharBoundary(start) || !s.IsCharBoundary(end) {
		return RopeSlice{}, ErrNonCharBoundary
	}
	return RopeSlice{root: s.root, start: s.start + start, end: s.start + end}, nil
}

// --- conversions and comparisons (ropey/src/shared_impl.rs:1096-1343) -------

// String returns the text (ropey's Display / `to_string`).
func (s RopeSlice) String() string {
	if s.Len() == 0 {
		return ""
	}
	if s.full() {
		var b strings.Builder
		b.Grow(s.Len())
		s.tree().appendTo(&b)
		return b.String()
	}
	var b strings.Builder
	b.Grow(s.Len())
	it := s.Chunks()
	for c, ok := it.Next(); ok; c, ok = it.Next() {
		b.WriteString(c)
	}
	return b.String()
}

// Equal reports whether the two slices hold the same text (PartialEq).
func (s RopeSlice) Equal(other RopeSlice) bool {
	if s.Len() != other.Len() {
		return false
	}
	a, b := s.Chunks(), other.Chunks()
	ca, _ := a.Next()
	cb, _ := b.Next()
	for {
		n := min(len(ca), len(cb))
		if ca[:n] != cb[:n] {
			return false
		}
		ca, cb = ca[n:], cb[n:]
		if ca == "" {
			var ok bool
			if ca, ok = a.Next(); !ok {
				return true
			}
		}
		if cb == "" {
			var ok bool
			if cb, ok = b.Next(); !ok {
				return true
			}
		}
	}
}

// EqualString reports whether the slice holds exactly text
// (PartialEq<&str>, ropey/src/shared_impl.rs:1150-1169).
func (s RopeSlice) EqualString(text string) bool {
	if s.Len() != len(text) {
		return false
	}
	idx := 0
	it := s.Chunks()
	for c, ok := it.Next(); ok; c, ok = it.Next() {
		if c != text[idx:idx+len(c)] {
			return false
		}
		idx += len(c)
	}
	return true
}

// AsStr returns the text without copying when it lives in a single chunk
// (ropey/src/shared_impl.rs:849-861); ok is false otherwise.
func (s RopeSlice) AsStr() (string, bool) {
	text, off := s.tree().chunkAt(s.start)
	if s.start-off+s.Len() <= len(text) {
		return text[s.start-off : s.start-off+s.Len()], true
	}
	return "", false
}
