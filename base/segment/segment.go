// Package segment provides the Unicode text segmentation GPUI Base gets from
// the unicode-segmentation crate (Base's Cargo.lock resolves 1.13.3, Unicode
// 17), on top of github.com/clipperhouse/uax29/v2 v2.7.0 (Unicode 17), plus a
// port of Base's own ASCII/Latin/Cyrillic word scanner, text_boundary.rs.
//
// Base call sites (upstream-base/ce/crates/base/src):
//
//   - input/base/state.rs:1460,1477 split_word_bound_indices (word motion);
//   - text/inline_flow.rs:1483,1488 split_word_bounds and graphemes(true);
//   - input/base/inline_tokens.rs:198 grapheme_indices(true);
//   - text_selection.rs:16 and input/base/selection.rs:9 text_boundary.rs.
//
// Recorded deviation (ticket 01 policy, research deps-02-segmentation-html.md):
// for AHLetter followed by MidLetter/MidNumLet (such as ' or .), ZWJ and an
// Extended_Pictographic char, e.g. "q.‍👨", unicode-segmentation 1.13.3
// yields one word segment while uax29 (and rivo/uniseg) split after the
// letter, as UAX #29 WB6/WB4 require. 5 of 52,719 differential strings show
// this; Base inputs never contain the pattern. It is not emulated.
//
// Only extended grapheme clusters exist here: Base always passes
// `is_extended = true`, and uax29 implements no legacy clusters.
package segment

import (
	"iter"

	"github.com/clipperhouse/uax29/v2/graphemes"
	"github.com/clipperhouse/uax29/v2/words"
)

// SplitWordBoundIndices yields every UAX #29 word-boundary segment of s with
// its byte offset, including whitespace runs and single punctuation marks
// (UnicodeSegmentation::split_word_bound_indices).
func SplitWordBoundIndices(s string) iter.Seq2[int, string] {
	return func(yield func(int, string) bool) {
		it := words.FromString(s)
		for it.Next() {
			if !yield(it.Start(), it.Value()) {
				return
			}
		}
	}
}

// SplitWordBounds yields every UAX #29 word-boundary segment of s
// (UnicodeSegmentation::split_word_bounds).
func SplitWordBounds(s string) iter.Seq[string] {
	return func(yield func(string) bool) {
		it := words.FromString(s)
		for it.Next() {
			if !yield(it.Value()) {
				return
			}
		}
	}
}

// Graphemes yields the extended grapheme clusters of s
// (UnicodeSegmentation::graphemes(s, true)).
func Graphemes(s string) iter.Seq[string] {
	return func(yield func(string) bool) {
		it := graphemes.FromString(s)
		for it.Next() {
			if !yield(it.Value()) {
				return
			}
		}
	}
}

// GraphemeIndices yields the extended grapheme clusters of s with their byte
// offsets (UnicodeSegmentation::grapheme_indices(s, true)).
func GraphemeIndices(s string) iter.Seq2[int, string] {
	return func(yield func(int, string) bool) {
		it := graphemes.FromString(s)
		for it.Next() {
			if !yield(it.Start(), it.Value()) {
				return
			}
		}
	}
}
