package gpui

// zterm ticket 11: text truncation with an ellipsis (R:elements/text.rs:
// 764-885 TruncateFrom / TextLayoutTruncation / evaluate_overflow /
// apply_truncation and 1164-1369 truncate_to_shaped_layout and its
// candidate and run helpers, R: = gpui-go/reference/ce-source/crates/
// gpui/src). The pin truncates by shaping: it binary-searches the
// largest grapheme count whose candidate (kept text plus the affix)
// shapes within the width and line clamp. Both text elements (Text,
// div.go textElement, and StyledText) truncate through it.
//
// Go adaptation: graphemes are the extended grapheme clusters of
// github.com/clipperhouse/uax29/v2 (the pin's unicode-segmentation
// grapheme_indices(true)).

import (
	"strings"
	"unicode"

	"github.com/clipperhouse/uax29/v2/graphemes"
)

// TruncateFrom is which part of overflowing text is removed
// (R:elements/text.rs:764-773).
type TruncateFrom uint8

const (
	// TruncateFromStart removes text from the start.
	TruncateFromStart TruncateFrom = iota
	// TruncateFromEnd removes text from the end.
	TruncateFromEnd
	// TruncateFromMiddle removes text from the middle, keeping both ends.
	TruncateFromMiddle
)

// TextLayoutTruncation is how text is truncated, generated during text
// layout by EvaluateTextOverflow (R:elements/text.rs:775-783).
type TextLayoutTruncation struct {
	// Width is the width the text can occupy before it is truncated (nil:
	// no truncation).
	Width *float32
	// Affix is the text affixed to the displayed text when truncating
	// (e.g. an ellipsis).
	Affix string
	// Source is which side of the text is removed when it does not fit.
	Source TruncateFrom
}

// EvaluateTextOverflow is the pinned TextLayout::evaluate_overflow
// (R:elements/text.rs:829-860): with a text overflow set, the width is
// the known width, else the definite available width (times the line
// clamp when set); content-based sizing does not truncate.
func EvaluateTextOverflow(textStyle TextStyle, req MeasureRequest) TextLayoutTruncation {
	if textStyle.TextOverflow == nil {
		return TextLayoutTruncation{Source: TruncateFromEnd}
	}
	var width *float32
	if req.KnownWidthPresent {
		known := req.KnownWidth
		width = &known
	} else if req.AvailWidth.Kind == AvailDefinite {
		available := req.AvailWidth.Definite
		if textStyle.LineClamp != nil {
			available *= float32(*textStyle.LineClamp)
		}
		width = &available
	}
	overflow := *textStyle.TextOverflow
	truncation := TextLayoutTruncation{Width: width, Affix: overflow.Ellipsis}
	switch overflow.Kind {
	case TextOverflowTruncateStart:
		truncation.Source = TruncateFromStart
	case TextOverflowTruncateMiddle:
		truncation.Source = TruncateFromMiddle
	default:
		truncation.Source = TruncateFromEnd
	}
	return truncation
}

// textLineClamp is the style's line clamp as the shaping request's
// optional row cap (a clamp below one is no clamp).
func textLineClamp(textStyle TextStyle) *uint32 {
	if textStyle.LineClamp == nil || *textStyle.LineClamp <= 0 {
		return nil
	}
	clamp := uint32(*textStyle.LineClamp)
	return &clamp
}

// applyTextTruncation is the pinned TextLayout::apply_truncation
// (R:elements/text.rs:862-885): without a truncation width the text and
// runs pass through; otherwise truncateToShapedLayout. shape shapes one
// candidate under the wrap width with no line clamp.
func applyTextTruncation(text string, runs []TextRun, wrapWidth *float32, lineClamp *int, truncation TextLayoutTruncation, shape func(text string, runs []TextRun, wrapWidth *float32) (shapedText, error)) (string, []TextRun) {
	if truncation.Width == nil {
		return text, runs
	}
	return truncateToShapedLayout(text, wrapWidth, *truncation.Width, lineClamp, truncation.Affix, runs, truncation.Source, shape)
}

// truncateToShapedLayout is the pinned truncate_to_shaped_layout
// (R:elements/text.rs:1164-1215): text that fits returns unchanged;
// otherwise the largest kept grapheme count whose candidate fits (at
// least one grapheme is removed; zero kept leaves only the affix).
func truncateToShapedLayout(text string, wrapWidth *float32, truncateWidth float32, maxLines *int, affix string, runs []TextRun, direction TruncateFrom, shape func(text string, runs []TextRun, wrapWidth *float32) (shapedText, error)) (string, []TextRun) {
	fits := func(candidate string, candidateRuns []TextRun) bool {
		document, err := shape(candidate, candidateRuns, wrapWidth)
		if err != nil {
			return false
		}
		width := truncateWidth
		if wrapWidth != nil {
			width = *wrapWidth
		}
		if maxLines != nil && document.lineCount() > max(*maxLines, 1) {
			return false
		}
		for i := 0; i < document.lineCount(); i++ {
			if document.lineAdvance(i) > width+0.01 {
				return false
			}
		}
		return true
	}

	if fits(text, runs) {
		return text, runs
	}

	boundaries := graphemeBoundaries(text)
	graphemeCount := len(boundaries) - 1
	low, high := 0, max(graphemeCount-1, 0)
	for low < high {
		middle := low + (high-low+1)/2
		candidateText, candidateRuns := makeTruncationCandidate(text, boundaries, middle, affix, runs, direction)
		if fits(candidateText, candidateRuns) {
			low = middle
		} else {
			high = middle - 1
		}
	}
	return makeTruncationCandidate(text, boundaries, low, affix, runs, direction)
}

// graphemeBoundaries returns the byte offset of every grapheme start
// plus the text length (the pin's grapheme_indices(true) collected, then
// text.len() pushed).
func graphemeBoundaries(text string) []int {
	var boundaries []int
	it := graphemes.FromString(text)
	for it.Next() {
		boundaries = append(boundaries, it.Start())
	}
	return append(boundaries, len(text))
}

// makeTruncationCandidate is the pinned make_truncation_candidate
// (R:elements/text.rs:1217-1258): keep graphemes from the start (End),
// the end (Start) or two thirds front / one third back (Middle), with
// the affix at the cut and the runs cut to cover the result.
func makeTruncationCandidate(text string, boundaries []int, keep int, affix string, runs []TextRun, direction TruncateFrom) (string, []TextRun) {
	graphemeCount := max(len(boundaries)-1, 0)
	keep = min(keep, graphemeCount)
	candidateRuns := append([]TextRun(nil), runs...)
	switch direction {
	case TruncateFromEnd:
		prefix := strings.TrimRightFunc(text[:boundaries[keep]], func(ch rune) bool {
			return unicode.IsSpace(ch) || isASCIIPunctuation(ch)
		})
		result := prefix + affix
		return result, updateRunsAfterTruncation(result, affix, candidateRuns, direction)
	case TruncateFromStart:
		result := affix + text[boundaries[graphemeCount-keep]:]
		return result, updateRunsAfterTruncation(result, affix, candidateRuns, direction)
	default:
		frontCount := (keep*2 + 2) / 3
		backCount := keep - frontCount
		frontEnd := boundaries[frontCount]
		backStart := boundaries[graphemeCount-backCount]
		result := text[:frontEnd] + affix + text[backStart:]
		return result, updateRunsAfterMiddleTruncation(affix, candidateRuns, frontEnd, backStart)
	}
}

// isASCIIPunctuation is char::is_ascii_punctuation.
func isASCIIPunctuation(ch rune) bool {
	return (ch >= '!' && ch <= '/') || (ch >= ':' && ch <= '@') || (ch >= '[' && ch <= '`') || (ch >= '{' && ch <= '~')
}

// updateRunsAfterTruncation is the pinned update_runs_after_truncation
// (R:elements/text.rs:1260-1290): the run reaching the cut grows by the
// affix and the runs beyond it (End) or before it (Start) drop.
func updateRunsAfterTruncation(result, affix string, runs []TextRun, direction TruncateFrom) []TextRun {
	retained := max(len(result)-len(affix), 0)
	switch direction {
	case TruncateFromStart:
		for runIndex := len(runs) - 1; runIndex >= 0; runIndex-- {
			if runs[runIndex].Len <= retained {
				retained -= runs[runIndex].Len
			} else {
				runs[runIndex].Len = retained + len(affix)
				return runs[runIndex:]
			}
		}
	case TruncateFromEnd:
		for runIndex := range runs {
			if runs[runIndex].Len <= retained {
				retained -= runs[runIndex].Len
			} else {
				runs[runIndex].Len = retained + len(affix)
				return runs[:runIndex+1]
			}
		}
	default:
		panic("gpui: updateRunsAfterTruncation: middle truncation")
	}
	return runs
}

// updateRunsAfterMiddleTruncation is the pinned
// update_runs_after_middle_truncation (R:elements/text.rs:1292-1328):
// the runs cut to the kept front, the last of them grown by the affix
// (or an affix run styled like the first run), then the runs cut to the
// kept back.
func updateRunsAfterMiddleTruncation(affix string, runs []TextRun, frontEnd, backStart int) []TextRun {
	result := make([]TextRun, 0, len(runs))
	byteOffset := 0
	for _, run := range runs {
		runEnd := byteOffset + run.Len
		if byteOffset < frontEnd {
			retained := run
			retained.Len = min(runEnd, frontEnd) - byteOffset
			result = append(result, retained)
		}
		byteOffset = runEnd
	}
	if len(result) > 0 {
		result[len(result)-1].Len += len(affix)
	} else if len(runs) > 0 {
		affixRun := runs[0]
		affixRun.Len = len(affix)
		result = append(result, affixRun)
	}
	byteOffset = 0
	for _, run := range runs {
		runEnd := byteOffset + run.Len
		if runEnd > backStart {
			retained := run
			retained.Len = runEnd - max(backStart, byteOffset)
			result = append(result, retained)
		}
		byteOffset = runEnd
	}
	return result
}
