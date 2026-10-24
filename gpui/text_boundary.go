package gpui

// The multi-click and drag-granularity boundaries of the window text
// selection (browser-core ticket 19): the word a double-click selects, the
// paragraph a triple-click selects, and the word/paragraph ends a drag
// after a multi-click extends to. The model calls them through a seam —
// Base installs its upstream-derived pair (base/text_boundary.go,
// U:text_boundary.rs over base/segment) so its behaviour is unchanged,
// while the core default is the browser's.
//
// The Chromium reference (0d4dcd4f, %TEMP%\research-src\chromium):
//
//   - The word granularity is the ICU word-break iterator's segmentation
//     (UAX #29). Blink's StartOfWord/EndOfWord walk
//     WordBreakIterator(text) — icu::BreakIterator::createWordInstance,
//     third_party/blink/renderer/platform/text/text_break_iterator_icu.cc:
//     637-692 (WordBreakIteratorPool::Create) — with the boundary side
//     chosen by ChooseWordSide (selection_adjuster.cc:357-363:
//     kNextWordIfOnBoundary except at the end of a line or content, where
//     the last word is taken); a caret exactly on a boundary therefore
//     selects the segment AFTER it: StartOfWordPosition spells that side
//     as preceding(offset+1) and EndOfWordPosition as following(offset)
//     (third_party/blink/renderer/core/editing/position_units_word.cc:
//     67-79 and 269-281, FindInternal). The double-click itself is
//     SelectClosestWordFromHitTestResult → ExpandWithGranularity(
//     collapsed at the hit, kWord) — selection_controller.cc:796-800 —
//     and on Windows the selection is then extended over trailing
//     whitespace (AdjustSelectionWithTrailingWhitespace,
//     selection_controller.cc:187-209, gated by the embedder's
//     select_trailing_whitespace_enabled setting).
//   - The triple click is HandleTripleClick (selection_controller.cc:
//     1110-1155): ExpandWithGranularity(collapsed at the hit,
//     TextGranularity::kParagraph) — StartOfParagraph through EndOfPara-
//     graph, with ComputeEndRespectingGranularityAlgorithm's kParagraph
//     arm INCLUDING the paragraph break when a next position exists
//     (selection_adjuster.cc:318-334: "Include the paragraph break (the
//     space from the end of this paragraph to the start of the next one)
//     in the selection"). In a plain text run the hard line break \n is
//     the paragraph separator, so the triple click takes the line plus
//     its break when another line follows.
//   - A drag after a multi-click extends by the same granularity:
//     HandleMouseDragged keeps the selection's granularity
//     (selection_controller.cc:693-699, Selection().Granularity() — the
//     value the double/triple click's SetSelectionOptions carried) and
//     ExtendSelectionAsDirectional snaps the moving endpoint to the
//     granularity's boundary while the anchor's own end is kept whole
//     (selection_controller.cc:316-375): extending backward takes the
//     start of the word under the pointer and keeps the anchor word's
//     end; extending forward keeps the anchor word's start and takes the
//     word's end under the pointer.
//
// The port's default pair implements the UAX #29 rules for Latin text
// (see wordClassOf): letters, digits, the underscore, the MidLetter/
// MidNum/MidNumLet/Single_Quote joins and space runs. Documented
// limitations (the ticket's): CJK and emoji are not segmented like ICU's
// dictionary tailoring (each Han/kana character is its own segment — the
// un-tailored UAX #29 result), and no dictionary or custom-rule
// tailoring runs at all; Go's standard library has no UAX #29
// segmenter, so the Latin subset is hand-rolled.

import (
	"unicode"
	"unicode/utf8"
)

// TextSelectionWordBoundary computes the word segment containing a byte
// offset of text, in source offsets: the range a double-click selects
// (before any trailing-whitespace extension) and the range a
// granularity drag snaps to. false when text has no segment at offset.
type TextSelectionWordBoundary func(text string, offset int) (TextRange, bool)

// TextSelectionLineBoundary computes the paragraph a triple-click
// selects at a byte offset of text, in source offsets: the logical line
// plus its line break when another paragraph follows (Chromium's
// kParagraph expansion).
type TextSelectionLineBoundary func(text string, offset int) TextRange

// The seam: Base installs its upstream pair (U:text_boundary.rs) in init;
// the core default is the browser pair below. Writes happen at init time,
// before any window exists.
var (
	textSelectionWordRangeAt = chromeWordRangeAt
	textSelectionLineRangeAt = chromeParagraphRangeAt
)

// SetTextSelectionBoundaries replaces the boundary pair the window
// selection model uses for multi-click and drag-granularity expansion
// (browser-core 19). Base calls it in init with its upstream-derived
// pair; applications that want the browser rules leave the default.
func SetTextSelectionBoundaries(word TextSelectionWordBoundary, line TextSelectionLineBoundary) {
	if word != nil {
		textSelectionWordRangeAt = word
	}
	if line != nil {
		textSelectionLineRangeAt = line
	}
}

// textSelectionExtendsWordTrailingWhitespace reports whether a
// double-click's word selection extends over the whitespace following the
// word (Chromium's Windows SelectTrailingWhitespace,
// selection_controller.cc:187-209): the default follows the browser on
// Windows (the port is Windows-only); Base's upstream model does not
// extend and opts out in init.
var textSelectionExtendsWordTrailingWhitespace = true

// SetTextSelectionWordTrailingWhitespace opts the process in or out of
// the double-click's trailing-whitespace extension (Base opts out).
func SetTextSelectionWordTrailingWhitespace(enabled bool) {
	textSelectionExtendsWordTrailingWhitespace = enabled
}

// ---------------------------------------------------------------------------
// The word segments (UAX #29, Latin subset)
// ---------------------------------------------------------------------------

// wordClass is one UAX #29 word-break class of the Latin subset the port
// segments.
type wordClass uint8

const (
	// wordClassOther is everything without a join rule (WB999): each
	// character its own segment.
	wordClassOther wordClass = iota
	// wordClassLetter is ALetter: letters (the Latin and other alphabetic
	// scripts unicode.L covers, minus the Han ideographs and kana ICU
	// excludes — those stay Other, the documented CJK limitation).
	wordClassLetter
	// wordClassNumeric is Numeric: decimal digits (Nd).
	wordClassNumeric
	// wordClassExtendNumLet is ExtendNumLet: the connector punctuation
	// (Pc — underscore and friends) that joins words on either side
	// (WB13a/WB13b).
	wordClassExtendNumLet
	// wordClassMidLetter is MidLetter: U+003A COLON, U+00B7 MIDDLE DOT,
	// U+2027 HYPHENATION POINT, U+FF1A FULLWIDTH COLON — joined only
	// between letters (WB6/WB7).
	wordClassMidLetter
	// wordClassMidNum is MidNum: U+002C COMMA, U+003B SEMICOLON,
	// U+2044 FRACTION SLASH — joined only between digits (WB11/WB12).
	wordClassMidNum
	// wordClassMidNumLet is MidNumLet: U+002E FULL STOP, U+2018/U+2019
	// single quotation marks, U+FF0E/U+FF07 — joined between letters and
	// between digits (WB6/WB7 and WB11/WB12).
	wordClassMidNumLet
	// wordClassSingleQuote is Single_Quote: U+0027 APOSTROPHE — joined
	// between letters (WB6/WB7; the Hebrew gershayim rules WB7a-c are a
	// documented limitation).
	wordClassSingleQuote
	// wordClassSpace is WSegSpace: the space separators minus NBSP —
	// runs of spaces are one segment (WB3d).
	wordClassSpace
	// wordClassExtend is Extend: combining marks (Mn/Mc/Me), which join
	// to the character they follow (WB4).
	wordClassExtend
)

// isMidWord reports the mid-punctuation classes the letter rules join
// (MidLetter | MidNumLet | Single_Quote).
func isMidWord(c wordClass) bool {
	return c == wordClassMidLetter || c == wordClassMidNumLet || c == wordClassSingleQuote
}

// isMidNumeric reports the mid-punctuation classes the digit rules join
// (MidNum | MidNumLet).
func isMidNumeric(c wordClass) bool {
	return c == wordClassMidNum || c == wordClassMidNumLet
}

// wordClassOf classifies one rune into the Latin-subset classes.
func wordClassOf(r rune) wordClass {
	switch {
	case r == '\'':
		return wordClassSingleQuote
	case r == ':' || r == '·' || r == '\u2027' || r == '：':
		return wordClassMidLetter
	case r == ',' || r == ';' || r == '⁄':
		return wordClassMidNum
	case r == '.' || r == '‘' || r == '’' || r == '．' || r == '＇':
		return wordClassMidNumLet
	case unicode.Is(unicode.Zs, r) && r != '\u00A0':
		return wordClassSpace
	case unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Mc, r) || unicode.Is(unicode.Me, r):
		return wordClassExtend
	case unicode.Is(unicode.Pc, r):
		return wordClassExtendNumLet
	case unicode.Is(unicode.Nd, r):
		return wordClassNumeric
	case unicode.IsLetter(r) && !unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana):
		return wordClassLetter
	}
	return wordClassOther
}

// wordBreaks computes the UAX #29 word-break boundaries of text: the
// returned slice has one entry per rune plus the end of text, true where
// a boundary sits before that rune (the first and the last are always
// boundaries — sot/eot). The byte offsets of the runes ride along, the
// last entry being len(text). Combining marks fold into the character
// they follow (WB4) by never breaking after it.
func wordBreaks(text string) (boundaries []bool, offsets []int) {
	runes := []rune(text)
	n := len(runes)
	offsets = make([]int, n+1)
	for i, r := range runes {
		offsets[i+1] = offsets[i] + utf8.RuneLen(r)
	}
	// effectiveClass folds WB4 (Extend joins the preceding character):
	// the class a rune acts as, walking back over combining marks. An
	// index past the end (the lookahead at the last rune) has no class.
	classes := make([]wordClass, n)
	for i, r := range runes {
		classes[i] = wordClassOf(r)
	}
	effectiveClass := func(i int) wordClass {
		if i >= n {
			return wordClassOther
		}
		for i >= 0 && classes[i] == wordClassExtend {
			i--
		}
		if i < 0 {
			return wordClassOther
		}
		return classes[i]
	}
	boundaries = make([]bool, n+1)
	boundaries[0], boundaries[n] = true, true
	// noBreak(i) reports whether UAX #29 puts NO break between
	// runes[i-1] and runes[i].
	noBreak := func(i int) bool {

		if classes[i] == wordClassExtend {
			return true // WB4: an Extend never breaks from what precedes it.
		}
		cPrev := effectiveClass(i - 1)
		cCur := effectiveClass(i)
		hasNext := i+1 < n
		cNext := wordClassOther
		if hasNext {
			cNext = effectiveClass(i + 1)
		}
		cBefore := effectiveClass(i - 2) // the lookbehind of WB7/WB11

		switch {
		// WB5: ALetter × ALetter; WB9: ALetter × Numeric; WB10:
		// Numeric × ALetter; WB8: Numeric × Numeric.
		case cPrev == wordClassLetter && cCur == wordClassLetter,
			cPrev == wordClassLetter && cCur == wordClassNumeric,
			cPrev == wordClassNumeric && cCur == wordClassLetter,
			cPrev == wordClassNumeric && cCur == wordClassNumeric:
			return true
		// WB6: ALetter × (MidLetter | MidNumLet | Single_Quote) when the
		// next character is an ALetter.
		case cPrev == wordClassLetter && isMidWord(cCur) && hasNext && cNext == wordClassLetter:
			return true
		// WB7: (MidLetter | MidNumLet | Single_Quote) × ALetter when the
		// character before the mid punctuation is an ALetter.
		case isMidWord(cPrev) && cCur == wordClassLetter && cBefore == wordClassLetter:
			return true
		// WB12: Numeric × (MidNum | MidNumLet) when the next character is
		// a Numeric.
		case cPrev == wordClassNumeric && isMidNumeric(cCur) && hasNext && cNext == wordClassNumeric:
			return true
		// WB11: (MidNum | MidNumLet) × Numeric when the character before
		// the mid punctuation is a Numeric.
		case isMidNumeric(cPrev) && cCur == wordClassNumeric && cBefore == wordClassNumeric:
			return true
		// WB13a: (ALetter | Numeric | ExtendNumLet) × ExtendNumLet, and
		// WB13b: ExtendNumLet × (ALetter | Numeric).
		case (cPrev == wordClassLetter || cPrev == wordClassNumeric || cPrev == wordClassExtendNumLet) && cCur == wordClassExtendNumLet:
			return true
		case cPrev == wordClassExtendNumLet && (cCur == wordClassLetter || cCur == wordClassNumeric):
			return true
		// WB3d: WSegSpace × WSegSpace.
		case cPrev == wordClassSpace && cCur == wordClassSpace:
			return true
		}
		// WB999: break.
		return false
	}
	for i := 1; i < n; i++ {
		boundaries[i] = !noBreak(i)
	}
	return boundaries, offsets
}

// chromeWordRangeAt is the core default word boundary: the UAX #29
// segment containing offset, with a caret exactly on a boundary taking
// the segment AFTER it (StartOfWordPosition's kNextWordIfOnBoundary,
// position_units_word.cc:269-281) and the end of the text taking the last
// segment (the clipped caret past the final character).
func chromeWordRangeAt(text string, offset int) (TextRange, bool) {
	if text == "" {
		return TextRange{}, false
	}
	boundaries, offsets := wordBreaks(text)
	n := len(boundaries) - 1
	offset = min(max(offset, 0), len(text))
	// The rune index the offset falls in (a mid-rune byte clips into the
	// rune, like the caret offsets the model feeds in).
	index := 0
	for index < n && offsets[index+1] <= offset {
		index++
	}
	if offset == len(text) {
		// The caret past the last character: the last segment (the
		// kPreviousWordIfOnBoundary edge).
		start := 0
		for i := 1; i < n; i++ {
			if boundaries[i] {
				start = i
			}
		}
		return TextRange{Start: offsets[start], End: len(text)}, true
	}
	// The boundary at or before the rune (a caret exactly on a boundary
	// takes the segment that starts there).
	start := index
	for !boundaries[start] {
		start--
	}
	end := start + 1
	for end < n && !boundaries[end] {
		end++
	}
	if offsets[start] >= offsets[end] {
		return TextRange{}, false
	}
	return TextRange{Start: offsets[start], End: offsets[end]}, true
}

// chromeParagraphRangeAt is the core default line boundary: the logical
// line containing offset plus its line break when another line follows
// (Chromium's kParagraph expansion, selection_adjuster.cc:318-334 — the
// paragraph break is included in the selection). \r\n is one break.
func chromeParagraphRangeAt(text string, offset int) TextRange {
	offset = min(max(offset, 0), len(text))
	// Clip to a rune boundary at or before the offset; the end of the
	// text clips to its last character.
	for offset > 0 && offset < len(text) && !utf8.RuneStart(text[offset]) {
		offset--
	}
	if offset == len(text) && offset > 0 {
		_, size := utf8.DecodeLastRuneInString(text)
		offset -= size
	}
	start := offset
	for start > 0 {
		r, size := utf8.DecodeLastRuneInString(text[:start])
		if r == '\n' || r == '\r' {
			// \r\n is one separator: the line starts after the pair.
			if r == '\n' && start > size {
				if prev, _ := utf8.DecodeLastRuneInString(text[:start-size]); prev == '\r' {
					size *= 2
				}
			}
			break
		}
		start -= size
	}
	end := offset
	for end < len(text) {
		r, size := utf8.DecodeRuneInString(text[end:])
		if r == '\n' || r == '\r' {
			// Include the paragraph break, and \r\n whole.
			if r == '\r' && end+size < len(text) {
				if next, _ := utf8.DecodeRuneInString(text[end+size:]); next == '\n' {
					size *= 2
				}
			}
			end += size
			break
		}
		end += size
	}
	return TextRange{Start: start, End: end}
}
