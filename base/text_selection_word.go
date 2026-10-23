package base

// Port of U:text/selection.rs: the word a TextView double click selects
// (word_range_at, used by U:text/inline.rs:25 selection_for_multi_click).
// It differs from U:text_boundary.rs in clipping: an offset at the end of
// the text clips to the last character, so a click past the last glyph
// still selects the last word.
//
// It replaces ticket 28's textWordRangeAt default (text_state.go) in init.

import (
	"unicode"
	"unicode/utf8"

	"github.com/hoangvu12/gpui-go/gpui"
)

func init() {
	textWordRangeAt = textSelectionWordRangeAt
}

// textCharType is CharType (U:text/selection.rs:3-13).
type textCharType uint8

const (
	// textCharWord is a-z, A-Z, 0-9, _ and the Latin, Cyrillic and
	// Vietnamese letters is_word_char admits.
	textCharWord textCharType = iota
	// textCharWhitespace is '\t', ' ', ' ' and the rest of the
	// White_Space property but the line breaks.
	textCharWhitespace
	// textCharNewline is \n and \r.
	textCharNewline
	// textCharOther is punctuation, CJK characters, emoji and the like.
	textCharOther
)

// textIsWordChar is is_word_char (U:text/selection.rs:15-29).
func textIsWordChar(c rune) bool {
	return c == '_' ||
		c < utf8.RuneSelf && (c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') ||
		c >= 0x00C0 && c <= 0x00FF ||
		c >= 0x0100 && c <= 0x017F ||
		c >= 0x0180 && c <= 0x024F ||
		c >= 0x0400 && c <= 0x04FF ||
		c >= 0x1E00 && c <= 0x1EFF ||
		c >= 0x0300 && c <= 0x036F
}

// textCharTypeOf is `impl From<char> for CharType`
// (U:text/selection.rs:31-40). Go's unicode.IsSpace is the White_Space
// property Rust's char::is_whitespace tests.
func textCharTypeOf(c rune) textCharType {
	switch {
	case textIsWordChar(c):
		return textCharWord
	case c == '\n' || c == '\r':
		return textCharNewline
	case unicode.IsSpace(c):
		return textCharWhitespace
	}
	return textCharOther
}

// isConnectable is CharType::is_connectable (U:text/selection.rs:42-49).
func (t textCharType) isConnectable(c rune) bool {
	next := textCharTypeOf(c)
	return t == textCharWord && next == textCharWord || t == textCharWhitespace && next == textCharWhitespace
}

// textWordRangeFromChars is word_range_from_chars
// (U:text/selection.rs:51-78): prev yields the characters before offset
// (nearest first) and next those after c; at most 128 of each are taken.
func textWordRangeFromChars(offset int, c rune, prev, next func(yield func(rune) bool)) gpui.TextRange {
	charType := textCharTypeOf(c)
	start, end := offset, offset+utf8.RuneLen(c)
	taken := 0
	for p := range prev {
		if taken == 128 || !charType.isConnectable(p) {
			break
		}
		start -= utf8.RuneLen(p)
		taken++
	}
	taken = 0
	for n := range next {
		if taken == 128 || !charType.isConnectable(n) {
			break
		}
		end += utf8.RuneLen(n)
		taken++
	}
	return gpui.TextRange{Start: start, End: end}
}

// textSelectionWordRangeAt is word_range_at (U:text/selection.rs:80-93):
// the word around offset, false for empty text.
func textSelectionWordRangeAt(text string, offset int) (gpui.TextRange, bool) {
	if text == "" {
		return gpui.TextRange{}, false
	}
	offset = textClipOffset(text, offset)
	c, size := utf8.DecodeRuneInString(text[offset:])
	if size == 0 {
		return gpui.TextRange{}, false
	}
	return textWordRangeFromChars(offset, c,
		func(yield func(rune) bool) {
			for s := text[:offset]; s != ""; {
				r, n := utf8.DecodeLastRuneInString(s)
				if !yield(r) {
					return
				}
				s = s[:len(s)-n]
			}
		},
		func(yield func(rune) bool) {
			for _, r := range text[offset+size:] {
				if !yield(r) {
					return
				}
			}
		}), true
}

// textClipOffset is clip_offset (U:text/selection.rs:95-110): the end of
// the text clips to its last character; any other offset to the
// character boundary at or before it.
func textClipOffset(text string, offset int) int {
	offset = min(max(offset, 0), len(text))
	if offset == len(text) {
		_, n := utf8.DecodeLastRuneInString(text)
		return len(text) - n
	}
	for offset > 0 && !utf8.RuneStart(text[offset]) {
		offset--
	}
	return offset
}
