package gpui

// Base ticket 10 (rich text): the legacy LineWrapper Base's editor
// display map and inline flow wrap with (Base
// input/editor/display_map/text_wrapper.rs:296-335,
// text/inline_flow.rs:1170-1310). The core pin removed it (Parley wraps
// inside layout_text); the port restores the legacy greedy wrapper from
// the older gpui-ce checkout (~/.cargo/git/checkouts/
// gpui-ce-6758b1db058c6497/beb5a6d crates/gpui/src/text_system/
// line_wrapper.rs, "legacy" below) over per-character widths measured
// by shaping one character alone (legacy text_system.rs:745-760
// layout_width), cached per wrapper exactly like the legacy ASCII
// array + map (legacy line_wrapper.rs:17-24, 525-546).
//
// Ported: LineWrapper::MAX_INDENT, set_letter_spacing, wrap_line,
// is_word_char, width_for_char, LineFragment::{text, element} and
// Boundary (legacy line_wrapper.rs:26-158, 481-546, 671-742). Not
// ported: the truncation family (should_truncate_line, truncate_line,
// truncate_wrapped_line and the run-update helpers, legacy
// line_wrapper.rs:160-480, 573-669) — Base never calls it.
//
// Go adaptation: the legacy wrap_line returns a lazy iterator; Base
// always collects it, so WrapLine returns the boundaries as a slice.

import (
	"fmt"
	"unicode/utf8"
)

// LineWrapperMaxIndent is the maximum indent that can be applied to a
// wrapped line (legacy line_wrapper.rs:27-28 LineWrapper::MAX_INDENT).
const LineWrapperMaxIndent uint32 = 256

// LineWrapper wraps lines of text to a width with one font and font size
// (legacy line_wrapper.rs:16-24). Create one with
// WindowTextSystem.LineWrapper. A LineWrapper is not safe for concurrent
// use (the legacy wrapper is handed out mutably from a pool).
type LineWrapper struct {
	// Font is the wrapper's font.
	Font FontDescriptor
	// FontSize is the wrapper's font size in pixels.
	FontSize float32

	seam          windowTextSystem
	letterSpacing *float32
	asciiWidths   [128]float32
	asciiKnown    [128]bool
	otherWidths   map[rune]float32
}

// LineWrapper returns a line wrapper for the font and font size (legacy
// text_system.rs:332 TextSystem::line_wrapper). The legacy call resolves
// the font to an id and pools wrappers per (font id, size); the port
// hands out a fresh wrapper whose width cache lives with it.
func (t *WindowTextSystem) LineWrapper(font FontDescriptor, fontSize float32) (*LineWrapper, error) {
	seam, err := t.seam()
	if err != nil {
		return nil, err
	}
	return &LineWrapper{Font: font, FontSize: fontSize, seam: seam}, nil
}

// SetLetterSpacing sets the letter spacing applied between text
// characters (legacy line_wrapper.rs:41-43); nil clears it.
func (lw *LineWrapper) SetLetterSpacing(letterSpacing *float32) {
	if letterSpacing == nil {
		lw.letterSpacing = nil
		return
	}
	spacing := *letterSpacing
	lw.letterSpacing = &spacing
}

// LineFragmentKind distinguishes the two legacy LineFragment variants.
type LineFragmentKind uint8

const (
	// LineFragmentKindText is a text fragment consisting of characters.
	LineFragmentKindText LineFragmentKind = iota
	// LineFragmentKindElement is a non-text element with a fixed width.
	LineFragmentKindElement
	// LineFragmentKindTab is one hard tab whose advance the caller
	// resolved (a display map's tab stop): it wraps exactly like the
	// '\t' text character, only its width is given instead of measured.
	LineFragmentKindTab
)

// LineFragment is one fragment of a line to wrap (legacy
// line_wrapper.rs:670-684): text, or a non-text element with a fixed
// width occupying LenUTF8 bytes of the source.
type LineFragment struct {
	// Kind selects the variant.
	Kind LineFragmentKind
	// Text is the text content of a text fragment.
	Text string
	// Width is the width of an element fragment in pixels.
	Width float32
	// LenUTF8 is the UTF-8 length an element fragment stands for.
	LenUTF8 int
}

// LineFragmentText creates a text fragment (legacy line_wrapper.rs:688
// LineFragment::text).
func LineFragmentText(text string) LineFragment {
	return LineFragment{Kind: LineFragmentKindText, Text: text}
}

// LineFragmentElement creates a non-text element fragment with a width
// and UTF-8 length (legacy line_wrapper.rs:693 LineFragment::element).
func LineFragmentElement(width float32, lenUTF8 int) LineFragment {
	return LineFragment{Kind: LineFragmentKindElement, Width: width, LenUTF8: lenUTF8}
}

// LineFragmentTab creates a hard-tab fragment of a resolved width: a
// '\t' character (one byte) that breaks and indents like the text
// character but advances by width. A '\t' inside a text fragment keeps
// the legacy width, the tab shaped alone (one full tab stop, as the
// legacy layout_width measured it). Base ticket 44: Base's input expands
// tabs to its tab-size columns (Zed's tab map), whose width depends on
// the tab's column.
func LineFragmentTab(width float32) LineFragment {
	return LineFragment{Kind: LineFragmentKindTab, Width: width, LenUTF8: 1}
}

// Boundary is a boundary between two wrapped lines (legacy
// line_wrapper.rs:729-742).
type Boundary struct {
	// Ix is the byte index where the next line starts.
	Ix int
	// NextIndent is the indent of the next line, in spaces.
	NextIndent uint32
}

// wrapCandidate is one wrap-boundary candidate (legacy
// line_wrapper.rs:715-727 WrapBoundaryCandidate).
type wrapCandidate struct {
	element bool
	char    rune
	width   float32
	lenUTF8 int
	// fixed marks a text character whose width is given (a resolved
	// tab, LineFragmentTab) instead of measured.
	fixed bool
}

// candidates expands the fragments into candidates (legacy
// line_wrapper.rs:697-712): one per character of a text fragment, one
// per element fragment (the legacy iterates the "\0" stand-in).
func wrapCandidates(fragments []LineFragment) []wrapCandidate {
	var out []wrapCandidate
	for _, fragment := range fragments {
		if fragment.Kind == LineFragmentKindElement {
			out = append(out, wrapCandidate{element: true, width: fragment.Width, lenUTF8: fragment.LenUTF8})
			continue
		}
		if fragment.Kind == LineFragmentKindTab {
			out = append(out, wrapCandidate{char: '\t', width: fragment.Width, lenUTF8: 1, fixed: true})
			continue
		}
		for _, ch := range fragment.Text {
			out = append(out, wrapCandidate{char: ch, lenUTF8: utf8.RuneLen(ch)})
		}
	}
	return out
}

// WrapLine wraps a line made of fragments to a width with the wrapper's
// font and size (legacy line_wrapper.rs:45-158 wrap_line): a greedy
// pass over per-character widths that breaks at the last word boundary
// candidate (a word character after a space, or any non-word non-space
// character) and indents continuation lines by the line's leading
// whitespace, capped at LineWrapperMaxIndent.
func (lw *LineWrapper) WrapLine(fragments []LineFragment, wrapWidth float32) []Boundary {
	var boundaries []Boundary
	width := float32(0)
	firstNonWhitespaceIx := -1
	indent := -1
	lastCandidateIx := 0
	lastCandidateWidth := float32(0)
	lastWrapIx := 0
	prevC := '\x00'
	index := 0
	previousTextCharacter := false
	spacing := float32(0)
	if lw.letterSpacing != nil {
		spacing = *lw.letterSpacing
	}

	for _, candidate := range wrapCandidates(fragments) {
		ix := index
		index += candidate.lenUTF8
		newPrevC := prevC
		itemHadSpacing := false
		var itemWidth float32
		if !candidate.element {
			c := candidate.char
			if c == '\n' {
				previousTextCharacter = false
				continue
			}
			if isWordChar(c) {
				if prevC == ' ' && c != ' ' && firstNonWhitespaceIx >= 0 {
					lastCandidateIx = ix
					lastCandidateWidth = width
				}
			} else if c != ' ' && firstNonWhitespaceIx >= 0 {
				// CJK may not be space separated.
				lastCandidateIx = ix
				lastCandidateWidth = width
			}
			if c != ' ' && firstNonWhitespaceIx < 0 {
				firstNonWhitespaceIx = ix
			}
			newPrevC = c
			charWidth := candidate.width
			if !candidate.fixed {
				charWidth = lw.widthForChar(c)
			}
			charSpacing := float32(0)
			if previousTextCharacter {
				charSpacing = spacing
			}
			itemHadSpacing = previousTextCharacter
			previousTextCharacter = true
			itemWidth = charWidth + charSpacing
		} else {
			if prevC == ' ' && firstNonWhitespaceIx >= 0 {
				lastCandidateIx = ix
				lastCandidateWidth = width
			}
			if firstNonWhitespaceIx < 0 {
				firstNonWhitespaceIx = ix
			}
			itemWidth = candidate.width
		}

		width += itemWidth
		if width > wrapWidth && ix > lastWrapIx {
			if indent < 0 && firstNonWhitespaceIx >= 0 {
				indent = int(min(LineWrapperMaxIndent, uint32(firstNonWhitespaceIx-lastWrapIx)))
			}
			if lastCandidateIx > 0 {
				lastWrapIx = lastCandidateIx
				width -= lastCandidateWidth
				width -= spacing
				lastCandidateIx = 0
			} else {
				lastWrapIx = ix
				if itemHadSpacing {
					width = itemWidth - spacing
				} else {
					width = itemWidth
				}
			}
			nextIndent := uint32(0)
			if indent >= 0 {
				width += lw.widthForChar(' ') * float32(indent)
				nextIndent = uint32(indent)
			}
			boundaries = append(boundaries, Boundary{Ix: lastWrapIx, NextIndent: nextIndent})
		}
		prevC = newPrevC
	}
	return boundaries
}

// isWordChar reports whether a character belongs to a word that should
// not be wrapped (legacy line_wrapper.rs:481-523 is_word_char).
func isWordChar(c rune) bool {
	switch {
	// ASCII alphanumeric characters, for English, numbers: Hello123.
	case c < 0x80 && (c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'):
		return true
	// Latin-1 Supplement.
	case c >= 0x00C0 && c <= 0x00FF:
		return true
	// Latin Extended-A.
	case c >= 0x0100 && c <= 0x017F:
		return true
	// Latin Extended-B.
	case c >= 0x0180 && c <= 0x024F:
		return true
	// Cyrillic.
	case c >= 0x0400 && c <= 0x04FF:
		return true
	// Vietnamese: Latin Extended Additional, Combining Diacritical Marks.
	case c >= 0x1E00 && c <= 0x1EFF, c >= 0x0300 && c <= 0x036F:
		return true
	// Bengali.
	case c >= 0x0980 && c <= 0x09FF:
		return true
	}
	switch c {
	// Word-internal and trailing punctuation kept with its word.
	case '-', '_', '.', '\'', '’', '‘', '$', '%', '@', '#', '^', '~', ',', '=', ':', ';':
		return true
	// Closing punctuation never starts a line (UAX #14 LB13).
	case '!', ')', ']', '}', '"', '”', '»', '…':
		return true
	// The Zed end-of-line ellipsis.
	case '⋯':
		return true
	// Non-breaking glue characters.
	case ' ', ' ', '‑':
		return true
	}
	return false
}

// widthForChar returns a character's shaped width, cached (legacy
// line_wrapper.rs:525-546).
func (lw *LineWrapper) widthForChar(c rune) float32 {
	if c >= 0 && c < 128 {
		if lw.asciiKnown[c] {
			return lw.asciiWidths[c]
		}
		width := lw.measure(c)
		lw.asciiWidths[c] = width
		lw.asciiKnown[c] = true
		return width
	}
	if width, ok := lw.otherWidths[c]; ok {
		return width
	}
	width := lw.measure(c)
	if lw.otherWidths == nil {
		lw.otherWidths = make(map[rune]float32)
	}
	lw.otherWidths[c] = width
	return width
}

// measure shapes one character alone (legacy layout_width). The legacy
// shaping call is infallible; a native failure here panics.
func (lw *LineWrapper) measure(c rune) float32 {
	width, err := layoutWidthOf(lw.seam, lw.Font, lw.FontSize, c)
	if err != nil {
		panic(fmt.Sprintf("gpui: LineWrapper width of %q: %v", c, err))
	}
	return width
}
