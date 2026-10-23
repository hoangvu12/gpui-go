package rope

import (
	"strings"
	"unicode/utf8"
)

// Rope is a UTF-8 text rope (ropey/src/rope.rs:96-106). It embeds the
// whole-text RopeSlice, so every read method of RopeSlice (Len, LenLines,
// Slice, Chars, ByteToLineIdx, ...) is available on a Rope with the same
// meaning. A Rope is a value: assignment is ropey's O(1) `clone`, and later
// edits to either copy never affect the other. The zero value is empty.
type Rope struct {
	RopeSlice
}

// New creates an empty Rope (ropey/src/rope.rs:112-120).
func New() Rope { return Rope{} }

// FromStr creates a Rope from text (Rope::from_str / `Rope::from(&str)`,
// ropey/src/rope.rs:122-129). Invalid UTF-8 is replaced with U+FFFD.
func FromStr(text string) Rope {
	text = validUTF8(text)
	root := fromString(text)
	return Rope{RopeSlice{root: root, end: root.info.bytes}}
}

// FromSlice copies a slice into a new Rope (`impl From<RopeSlice> for Rope`,
// ropey/src/rope.rs:818-826).
func FromSlice(s RopeSlice) Rope { return FromStr(s.String()) }

func validUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	return strings.ToValidUTF8(s, "�")
}

func (r *Rope) setRoot(root *node) {
	root = pullUp(root)
	r.RopeSlice = RopeSlice{root: root, end: root.info.bytes}
}

// Insert inserts text at byte index byteIdx. Panics if byteIdx > Len() or is
// not a char boundary (ropey/src/rope.rs:251-277).
func (r *Rope) Insert(byteIdx int, text string) {
	if err := r.TryInsert(byteIdx, text); err != nil {
		panic(err)
	}
}

// TryInsert is the non-panicking Insert; on failure the rope is untouched
// (ropey/src/rope.rs:526-615).
func (r *Rope) TryInsert(byteIdx int, text string) error {
	if byteIdx < 0 || byteIdx > r.Len() {
		return ErrOutOfBounds
	}
	if !r.IsCharBoundary(byteIdx) {
		return ErrNonCharBoundary
	}
	if text == "" {
		return nil
	}
	text = validUTF8(text)
	r.setRoot(buildRoot(r.tree().insert(byteIdx, text)))
	return nil
}

// Remove removes the bytes [start, end). Panics if start > end, end > Len(),
// or either end is not a char boundary (ropey/src/rope.rs:303-335).
func (r *Rope) Remove(start, end int) {
	if err := r.TryRemove(start, end); err != nil {
		panic(err)
	}
}

// TryRemove is the non-panicking Remove; on failure the rope is untouched
// (ropey/src/rope.rs:627-680).
func (r *Rope) TryRemove(start, end int) error {
	if start < 0 || start > end {
		return ErrInvalidRange
	}
	if end > r.Len() {
		return ErrOutOfBounds
	}
	if !r.IsCharBoundary(start) || !r.IsCharBoundary(end) {
		return ErrNonCharBoundary
	}
	if start == end {
		return nil
	}
	if start == 0 && end == r.Len() {
		// ropey/src/rope.rs:763-770: removing everything yields a fresh rope.
		*r = Rope{}
		return nil
	}
	root := r.tree().remove(start, end)
	if root == nil {
		root = emptyLeaf
	}
	r.setRoot(root)
	return nil
}

// Equal reports whether both ropes hold the same text (PartialEq<Rope>).
func (r Rope) Equal(other Rope) bool { return r.RopeSlice.Equal(other.RopeSlice) }
