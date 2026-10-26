package gpui

// text-wrap: balance — the Go-side bisection over the shaping handle's
// relayouts (browser-core ticket 43, CSS text-wrap: balance). The
// rev-15 record's contract: the native side exposes no balance
// algorithm (TEXT_ABI.md — "Go-side by design"); the port reproduces
// Chromium's balancing as the bisection the rev-15 record named, over
// text_layout relayouts of the SAME shaping handle.
//
// The Chromium rule this implements
// (third_party/blink/renderer/core/layout/inline/paragraph_line_breaker.cc
// at 0d4dcd4f, `ParagraphLineBreaker::AttemptParagraphBalancing`):
//
//   - "Find the desired available width by bisecting the maximum
//     available width that produces `num_lines`" — the bisection
//     (`BisectAvailableWidth`) halves [lower, upper] while
//     `lower + epsilon < upper`, taking `upper = middle` when the
//     middle width still finishes in num_lines and `lower = middle`
//     otherwise, and answers `upper` — the NARROWEST width with the
//     same line count (lines ~119-145, 220-230).
//   - "Balancing not needed for single line paragraphs" (num_lines <= 1
//     → nullopt).
//   - The line-count bound: `kMaxLinesForBalance = 6`
//     (score_line_break_context.h:17) — a paragraph that breaks into
//     more than 6 lines is not balanced.
//   - The epsilon is at least 1 CSS pixel ("The bisect less than 1
//     pixel is worthless, so ignore").
//   - The lower bound starts at the average line width × 0.8 ("with 20%
//     buffer for potential edge cases").
//
// The port's epsilon is 1 CSS pixel like Chrome's, and the lower bound
// is the same 0.8 × average rule. Chrome's ScoreLineBreaker (its newer
// primary path, inline_layout_algorithm.cc's LineBreakStrategy) can
// supersede the bisection; the fixtures record whichever Chrome chose,
// and the recorded deviation is that the port always bisects.

// balanceLine re-shapes an already-shaped wrapped line at the narrowest
// width that keeps its current line count (Chromium's bisection; see the
// file header). wrapWidth is the width the line was shaped under (the
// document's wrap constraint). The line's own request is updated by the
// relayouts, so a parked line re-shapes at the balanced width and
// reproduces the same rows.
//
// Single-line paragraphs and paragraphs over Chromium's balancing bound
// (6 lines) are left as shaped — Chrome's own guards
// (paragraph_line_breaker.cc:188-190 and score_line_break_context.h:17).
// A relayout failure leaves the line at its unbalanced shape and
// reports the error to the caller, which falls back the same way.
func balanceLine(line *WrappedLine, wrapWidth float32) error {
	summary, err := line.Summary()
	if err != nil {
		return err
	}
	lines := summary.LineCount
	if lines <= 1 || lines > maxBalanceLines {
		// Balancing not needed (or Chrome's bound): the greedy shape.
		return nil
	}
	// The lower bound: the average line width with Chrome's 20% buffer
	// (paragraph_line_breaker.cc:226-228).
	lower := averageLineWidth(summary, lines, wrapWidth) * 0.8
	upper := wrapWidth
	const epsilon = 1.0 // CSS pixels (paragraph_line_breaker.cc:199-202)
	for lower+epsilon < upper {
		middle := (lower + upper) / 2
		count, err := relayoutLineCount(line, middle)
		if err != nil {
			// A failed probe leaves the bisection; the caller keeps the
			// greedy shape (restore it before reporting).
			_, _ = relayoutLineCount(line, upper)
			return err
		}
		if count == lines {
			upper = middle
		} else {
			lower = middle
		}
	}
	// The narrowest width with the same line count: shape at `upper`.
	if _, err := relayoutLineCount(line, upper); err != nil {
		// Restore the greedy shape before reporting.
		_, _ = relayoutLineCount(line, wrapWidth)
		return err
	}
	return nil
}

// maxBalanceLines is Chromium's kMaxLinesForBalance
// (score_line_break_context.h:17): balancing is capped at six lines.
const maxBalanceLines = 6

// relayoutLineCount re-shapes the line at a candidate width and reports
// the resulting visual line count (the bisection's probe). The line
// keeps the layout of the LAST relayout, so the caller restores the
// chosen width with a final relayout.
func relayoutLineCount(line *WrappedLine, width float32) (int, error) {
	boxed := width
	if err := line.Relayout(&boxed, nil); err != nil {
		return 0, err
	}
	count, err := line.LineCount()
	if err != nil {
		return 0, err
	}
	return count, nil
}

// averageLineWidth estimates the bisection's lower bound from the
// summary: the content width (the widest line's advance) scaled to the
// line count, clamped to the wrap width. Chrome divides the sum of the
// line widths; the summary only carries the widest one, and the
// scaled-widest estimate is within the 20% buffer's purpose (a lower
// bound the true answer cannot undercut).
func averageLineWidth(summary TextLayoutSummary, lines int, wrapWidth float32) float32 {
	if lines <= 0 {
		return 0
	}
	average := summary.Width / float32(lines)
	if average > wrapWidth {
		average = wrapWidth
	}
	if average < 0 {
		average = 0
	}
	return average
}
