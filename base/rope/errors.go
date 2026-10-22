package rope

import "errors"

// Error causes, mirroring ropey::Error (ropey/src/lib.rs:285-309).
var (
	// ErrNonCharBoundary: the byte index(es) given were not on a char
	// boundary, but needed to be for the given operation.
	ErrNonCharBoundary = errors.New("byte index is not on a char boundary")
	// ErrOutOfBounds: the index(es) given exceeded the size of the text.
	ErrOutOfBounds = errors.New("index is out of bounds")
	// ErrInvalidRange: the range given was intrinsically invalid (end < start).
	ErrInvalidRange = errors.New("index range is invalid: end < start")
)

// LineType selects the line-break convention for line-based APIs
// (ropey/src/lib.rs:248-277). Base enables only `metric_lines_lf`, so LF is the
// only variant: CRLF is recognized implicitly because CR is ignored.
type LineType uint8

// LF recognizes only U+000A as a line break.
const LF LineType = 0

func mustLF(lt LineType) {
	if lt != LF {
		panic("rope: only LineType LF is supported (Base enables metric_lines_lf only)")
	}
}
