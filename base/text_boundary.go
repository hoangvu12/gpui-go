package base

// Port of U:text_boundary.rs (pub(crate)): the word and line ranges
// window text selection expands a double or triple click to. With the
// selection model in the core (browser-core 19, gpui/text_boundary.go),
// these install Base's upstream-derived pair over the core's boundary
// seam — the core's default is the browser pair (ICU's UAX #29 word
// segmentation and the paragraph-with-break triple click), while Base's
// selection keeps the upstream rules: the input's word boundary
// (base/segment) and the logical line without its break.
//
// The character-class scanner itself (CharacterKind, word_range_from_chars,
// clip_offset_left; U:text_boundary.rs:3-53, 76-82) is already ported in
// base/segment (segment/text_boundary.go), where the input's word motion
// shares it.

import (
	"github.com/hoangvu12/gpui-go/base/segment"
	"github.com/hoangvu12/gpui-go/gpui"
)

func init() {
	gpui.SetTextSelectionBoundaries(baseWordRangeAt, baseLineRangeAt)
	// Chromium's Windows rule (a double-clicked word takes its trailing
	// whitespace) is the core's default; the upstream model Base ports
	// does not extend.
	gpui.SetTextSelectionWordTrailingWhitespace(false)
}

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

// baseWordRangeAt adapts textBoundaryWordRangeAt to the core's seam.
func baseWordRangeAt(text string, offset int) (gpui.TextRange, bool) {
	return textBoundaryWordRangeAt(text, offset)
}

// baseLineRangeAt adapts textBoundaryLineRangeAt to the core's seam.
func baseLineRangeAt(text string, offset int) gpui.TextRange {
	return textBoundaryLineRangeAt(text, offset)
}
