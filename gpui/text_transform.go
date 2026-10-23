package gpui

// Ticket 06 (browser-core): text-transform is applied. This file is
// the port of the pin's apply_text_transform_preserving_byte_len
// (ce:crates/gpui/src/elements/text.rs:636-691), called from the two
// layout entry points (the text element, div.go textElement.
// RequestLayout, and TextLayout::layout, styled_text.go) exactly where
// the pin calls it (text.rs:895, inside TextLayout::layout, before
// shaping and before the inline content is published).
//
// The rule (a port gap, not a divergence: the pin transforms, the port
// only cascaded the style):
//
//   - Uppercase/Lowercase map each character through its single
//     character case mapping, keeping the mapping only when it
//     preserves the character's UTF-8 length (Rust pushes the mapped
//     string only when mapped.len() == character.len_utf8() and it is
//     one character; Go's unicode.ToUpper/ToLower always answer one
//     rune, so the length check carries the rule): "ß" stays "ß" and
//     "İ" stays "İ" under lowercase, because their full case mappings
//     change length.
//   - Capitalize uppercases the first ALPHABETIC character of every
//     UAX #29 word (Rust's split_word_bounds; this port iterates
//     github.com/clipperhouse/uax29/v2/words, whose tokens concatenate
//     back to the source exactly), leaving every other character
//     alone — no folding of the rest, letters after digit prefixes
//     untouched ("123BAZ" stays "123BAZ", "foo2BAR" becomes "Foo2BAR").
//
// Byte length is preserved in all cases, so the caller's style runs
// (byte lengths over the source) keep covering the transformed text.

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/clipperhouse/uax29/v2/words"
)

// applyTextTransformPreservingByteLen maps the text through its style's
// text-transform, preserving the UTF-8 byte length (the pinned
// apply_text_transform_preserving_byte_len, elements/text.rs:636-691).
// A nil or None transform returns the text unchanged.
func applyTextTransformPreservingByteLen(text string, transform *TextTransform) string {
	if transform == nil || *transform == TextTransformNone {
		return text
	}

	var output strings.Builder
	output.Grow(len(text))
	switch *transform {
	case TextTransformUppercase:
		for _, character := range text {
			pushCaseMappedCharacter(&output, character, unicode.ToUpper)
		}
	case TextTransformLowercase:
		for _, character := range text {
			pushCaseMappedCharacter(&output, character, unicode.ToLower)
		}
	case TextTransformCapitalize:
		for _, piece := range wordBoundPieces(text) {
			seenFirstLetter := false
			for _, character := range piece {
				if !seenFirstLetter && isAlphabeticRune(character) {
					pushCaseMappedCharacter(&output, character, unicode.ToUpper)
					seenFirstLetter = true
				} else {
					output.WriteRune(character)
				}
			}
		}
	default:
		return text
	}
	return output.String()
}

// pushCaseMappedCharacter writes character's case mapping when the
// mapping is one character of the same UTF-8 length, else the character
// itself (the pinned push_case_mapped_character, text.rs:683-692).
func pushCaseMappedCharacter(output *strings.Builder, character rune, mapCase func(rune) rune) {
	mapped := mapCase(character)
	if utf8.RuneLen(mapped) == utf8.RuneLen(character) {
		output.WriteRune(mapped)
	} else {
		output.WriteRune(character)
	}
}

// wordBoundPieces splits the text at UAX #29 word boundaries (the
// pin's split_word_bounds, unicode-segmentation). The pieces
// concatenate back to the text byte for byte, including whitespace and
// punctuation, which the capitalize rule needs: a word's "first
// letter" search starts at each piece boundary.
func wordBoundPieces(text string) []string {
	var pieces []string
	tokens := words.FromString(text)
	for tokens.Next() {
		pieces = append(pieces, tokens.Value())
	}
	return pieces
}

// isAlphabeticRune reports the Unicode Alphabetic property (Rust's
// char::is_alphabetic): the letter categories, letter numbers, and the
// Other_Alphabetic marks.
func isAlphabeticRune(r rune) bool {
	return unicode.In(r, unicode.L, unicode.Nl, unicode.Other_Alphabetic)
}
