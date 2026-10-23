package base

// Port of U:text_boundary.rs (pub(crate)): the word and line ranges window
// text selection expands a double or triple click to
// (U:text_selection.rs:16, 545-549).
//
// The character-class scanner itself (CharacterKind, word_range_from_chars,
// clip_offset_left; U:text_boundary.rs:3-53, 76-82) is already ported in
// base/segment (segment/text_boundary.go), where the input's word motion
// shares it; these are the crate-internal byte-range forms over it.
// U:text_boundary.rs has no tests (segment/text_boundary_test.go carries
// Go-own ones).

import (
	"github.com/hoangvu12/gpui-go/base/segment"
	"github.com/hoangvu12/gpui-go/gpui"
)

// textBoundaryWordRangeAt is word_range_at (U:text_boundary.rs:55-65): the
// word (or whitespace run) containing offset, clipped left to a character
// boundary; false at the end of the text.
func textBoundaryWordRangeAt(text string, offset int) (gpui.TextRange, bool) {
	start, end, ok := segment.WordRangeAt(text, offset)
	return gpui.TextRange{Start: start, End: end}, ok
}

// textBoundaryLineRangeAt is line_range_at (U:text_boundary.rs:67-74): the
// logical line containing offset, without its line break.
func textBoundaryLineRangeAt(text string, offset int) gpui.TextRange {
	start, end := segment.LineRangeAt(text, offset)
	return gpui.TextRange{Start: start, End: end}
}
