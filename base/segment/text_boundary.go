package segment

import (
	"iter"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Port of upstream-base/ce/crates/base/src/text_boundary.rs (pub(crate) in
// Base; exported here because the Go callers live in other packages).

type characterKind uint8

const (
	kindWord characterKind = iota
	kindWhitespace
	kindNewline
	kindOther
)

// kindOf is `impl From<char> for CharacterKind` (text_boundary.rs:11-26).
// Go's unicode.IsSpace is the White_Space property that Rust's
// char::is_whitespace uses.
func kindOf(c rune) characterKind {
	switch {
	case c == '_' || c < utf8.RuneSelf && (c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'),
		c >= 0x00C0 && c <= 0x024F, c >= 0x0400 && c <= 0x04FF, c >= 0x1E00 && c <= 0x1EFF, c >= 0x0300 && c <= 0x036F:
		return kindWord
	case c == '\n' || c == '\r':
		return kindNewline
	case unicode.IsSpace(c):
		return kindWhitespace
	}
	return kindOther
}

// WordRangeFromChars returns the byte range of the word or whitespace run
// around character, which starts at byte offset; previous yields the chars
// before it (nearest first) and following the chars after it. At most 128
// chars are taken from each side (text_boundary.rs:28-53).
func WordRangeFromChars(offset int, character rune, previous, following iter.Seq[rune]) (start, end int) {
	kind := kindOf(character)
	connects := func(next rune) bool {
		k := kindOf(next)
		return kind == kindWord && k == kindWord || kind == kindWhitespace && k == kindWhitespace
	}
	start = offset
	n := 0
	for c := range previous {
		if n == 128 || !connects(c) {
			break
		}
		start -= utf8.RuneLen(c)
		n++
	}
	end = offset + utf8.RuneLen(character)
	n = 0
	for c := range following {
		if n == 128 || !connects(c) {
			break
		}
		end += utf8.RuneLen(c)
		n++
	}
	return start, end
}

// WordRangeAt returns the word range containing byte offset of text
// (clipped left to a char boundary); ok is false at the end of the text
// (text_boundary.rs:55-65).
func WordRangeAt(text string, offset int) (start, end int, ok bool) {
	offset = clipOffsetLeft(text, offset)
	character, size := utf8.DecodeRuneInString(text[offset:])
	if size == 0 {
		return 0, 0, false
	}
	start, end = WordRangeFromChars(offset, character,
		func(yield func(rune) bool) {
			for s := text[:offset]; s != ""; {
				c, n := utf8.DecodeLastRuneInString(s)
				if !yield(c) {
					return
				}
				s = s[:len(s)-n]
			}
		},
		func(yield func(rune) bool) {
			for _, c := range text[offset+size:] {
				if !yield(c) {
					return
				}
			}
		})
	return start, end, true
}

// LineRangeAt returns the byte range of the line containing offset, without
// its line break (text_boundary.rs:67-74).
func LineRangeAt(text string, offset int) (start, end int) {
	offset = clipOffsetLeft(text, offset)
	start = strings.LastIndexByte(text[:offset], '\n') + 1
	if i := strings.IndexByte(text[offset:], '\n'); i >= 0 {
		return start, offset + i
	}
	return start, len(text)
}

// clipOffsetLeft is text_boundary.rs:76-82.
func clipOffsetLeft(text string, offset int) int {
	offset = min(max(offset, 0), len(text))
	for offset < len(text) && offset > 0 && !utf8.RuneStart(text[offset]) {
		offset--
	}
	return offset
}
