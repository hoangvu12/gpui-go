package rope

import "unicode/utf8"

// Iterators follow ropey's model (ropey/src/iter.rs:1-63): an iterator sits
// between elements like a text cursor; Next jumps forward over one element
// and returns it, Prev jumps backward, and Reversed swaps the two directions
// without moving. An iterator created "at" a position keeps access to the
// whole slice, so Prev can walk back past where it started.

// Chars iterates the chars of a slice (ropey/src/iter.rs:407-552).
type Chars struct {
	s          RopeSlice
	pos        int // slice byte offset of the cursor
	chunk      string
	chunkStart int
	reversed   bool
}

// Chars returns an iterator over the chars, starting at the beginning.
func (s RopeSlice) Chars() Chars { return s.CharsAt(0) }

// CharsAt returns an iterator positioned before the char at byte byteIdx.
// Panics if byteIdx > Len() or is not a char boundary
// (ropey/src/shared_impl.rs:593-625).
func (s RopeSlice) CharsAt(byteIdx int) Chars {
	if !s.IsCharBoundary(byteIdx) {
		panic(ErrNonCharBoundary)
	}
	return Chars{s: s, pos: byteIdx, chunkStart: -1}
}

// Next advances in the iterator's direction; ok is false at the end.
func (c *Chars) Next() (rune, bool) {
	if c.reversed {
		return c.prev()
	}
	return c.next()
}

// Prev moves against the iterator's direction; ok is false at the end.
func (c *Chars) Prev() (rune, bool) {
	if c.reversed {
		return c.next()
	}
	return c.prev()
}

// Reversed returns the iterator with its direction swapped, at the same
// position.
func (c Chars) Reversed() Chars {
	c.reversed = !c.reversed
	return c
}

func (c *Chars) load(i int) {
	if c.chunkStart < 0 || i < c.chunkStart || i >= c.chunkStart+len(c.chunk) {
		c.chunk, c.chunkStart = c.s.chunk(i)
	}
}

func (c *Chars) next() (rune, bool) {
	if c.pos >= c.s.Len() {
		return 0, false
	}
	c.load(c.pos)
	r, n := utf8.DecodeRuneInString(c.chunk[c.pos-c.chunkStart:])
	c.pos += n
	return r, true
}

func (c *Chars) prev() (rune, bool) {
	if c.pos <= 0 {
		return 0, false
	}
	c.load(c.pos - 1)
	r, n := utf8.DecodeLastRuneInString(c.chunk[:c.pos-c.chunkStart])
	c.pos -= n
	return r, true
}

// CharIndices iterates chars with the byte index where each starts
// (ropey/src/iter.rs:588-665).
type CharIndices struct{ c Chars }

// CharIndices returns an iterator over (byte index, char) pairs.
func (s RopeSlice) CharIndices() CharIndices { return CharIndices{s.Chars()} }

// CharIndicesAt is CharIndices positioned at byte byteIdx (a char boundary).
func (s RopeSlice) CharIndicesAt(byteIdx int) CharIndices {
	return CharIndices{s.CharsAt(byteIdx)}
}

// Next returns the next char and the byte index where it starts.
func (it *CharIndices) Next() (int, rune, bool) {
	if it.c.reversed {
		r, ok := it.c.Next()
		return it.c.pos, r, ok
	}
	off := it.c.pos
	r, ok := it.c.Next()
	return off, r, ok
}

// Prev returns the previous char and the byte index where it starts.
func (it *CharIndices) Prev() (int, rune, bool) {
	if it.c.reversed {
		off := it.c.pos
		r, ok := it.c.Prev()
		return off, r, ok
	}
	r, ok := it.c.Prev()
	return it.c.pos, r, ok
}

// Reversed swaps the iteration direction.
func (it CharIndices) Reversed() CharIndices { return CharIndices{it.c.Reversed()} }

// Bytes iterates the bytes of a slice (ropey/src/iter.rs:256-379).
type Bytes struct {
	s        RopeSlice
	pos      int
	reversed bool
}

// Bytes returns an iterator over the bytes.
func (s RopeSlice) Bytes() Bytes { return s.BytesAt(0) }

// BytesAt returns a byte iterator positioned at byteIdx. Panics if
// byteIdx > Len().
func (s RopeSlice) BytesAt(byteIdx int) Bytes {
	if byteIdx < 0 || byteIdx > s.Len() {
		oob()
	}
	return Bytes{s: s, pos: byteIdx}
}

// Next advances in the iterator's direction.
func (b *Bytes) Next() (byte, bool) {
	if b.reversed {
		return b.prev()
	}
	return b.next()
}

// Prev moves against the iterator's direction.
func (b *Bytes) Prev() (byte, bool) {
	if b.reversed {
		return b.next()
	}
	return b.prev()
}

// Reversed swaps the iteration direction.
func (b Bytes) Reversed() Bytes {
	b.reversed = !b.reversed
	return b
}

func (b *Bytes) next() (byte, bool) {
	v, ok := b.s.GetByte(b.pos)
	if ok {
		b.pos++
	}
	return v, ok
}

func (b *Bytes) prev() (byte, bool) {
	if b.pos <= 0 {
		return 0, false
	}
	b.pos--
	return b.s.Byte(b.pos), true
}

// Chunks iterates the non-empty contiguous string chunks of a slice
// (ropey/src/iter.rs:72-209). Chunk boundaries carry no meaning.
type Chunks struct {
	s        RopeSlice
	pos      int
	reversed bool
}

// Chunks returns an iterator over the chunks.
func (s RopeSlice) Chunks() Chunks { return Chunks{s: s} }

// ChunksAt returns an iterator starting at the chunk containing byteIdx and
// that chunk's start byte (Len() when byteIdx == Len()). Panics if
// byteIdx > Len() (ropey/src/shared_impl.rs:764-797).
func (s RopeSlice) ChunksAt(byteIdx int) (Chunks, int) {
	if byteIdx < 0 || byteIdx > s.Len() {
		oob()
	}
	if byteIdx == s.Len() {
		return Chunks{s: s, pos: byteIdx}, byteIdx
	}
	_, start := s.chunk(byteIdx)
	return Chunks{s: s, pos: start}, start
}

// Next advances in the iterator's direction.
func (c *Chunks) Next() (string, bool) {
	if c.reversed {
		return c.prev()
	}
	return c.next()
}

// Prev moves against the iterator's direction.
func (c *Chunks) Prev() (string, bool) {
	if c.reversed {
		return c.next()
	}
	return c.prev()
}

// Reversed swaps the iteration direction.
func (c Chunks) Reversed() Chunks {
	c.reversed = !c.reversed
	return c
}

func (c *Chunks) next() (string, bool) {
	if c.pos >= c.s.Len() {
		return "", false
	}
	text, start := c.s.chunk(c.pos)
	c.pos = start + len(text)
	return text, true
}

func (c *Chunks) prev() (string, bool) {
	if c.pos <= 0 {
		return "", false
	}
	text, start := c.s.chunk(c.pos - 1)
	c.pos = start
	return text, true
}

// Lines iterates the lines of a slice, each including its line break; the
// last line is yielded even when empty (ropey/src/iter.rs:669-760).
type Lines struct {
	s        RopeSlice
	lineType LineType
	line     int
	reversed bool
}

// Lines returns an iterator over the lines.
func (s RopeSlice) Lines(lineType LineType) Lines { return s.LinesAt(0, lineType) }

// LinesAt returns a line iterator positioned before line lineIdx. Panics if
// lineIdx > LenLines().
func (s RopeSlice) LinesAt(lineIdx int, lineType LineType) Lines {
	if lineIdx < 0 || lineIdx > s.LenLines(lineType) {
		oob()
	}
	return Lines{s: s, lineType: lineType, line: lineIdx}
}

// Next advances in the iterator's direction.
func (l *Lines) Next() (RopeSlice, bool) {
	if l.reversed {
		return l.prev()
	}
	return l.next()
}

// Prev moves against the iterator's direction.
func (l *Lines) Prev() (RopeSlice, bool) {
	if l.reversed {
		return l.next()
	}
	return l.prev()
}

// Reversed swaps the iteration direction.
func (l Lines) Reversed() Lines {
	l.reversed = !l.reversed
	return l
}

func (l *Lines) next() (RopeSlice, bool) {
	line, ok := l.s.GetLine(l.line, l.lineType)
	if ok {
		l.line++
	}
	return line, ok
}

func (l *Lines) prev() (RopeSlice, bool) {
	if l.line <= 0 {
		return RopeSlice{}, false
	}
	l.line--
	return l.s.Line(l.line, l.lineType), true
}
