//go:build windows

// The browser-core ticket 43 (rev 16) Go styling gates: the text style's
// word-break / overflow-wrap / text-wrap / text-align fields reach the
// shaping request's runs, text-wrap: balance bisects the wrapped line to
// the narrowest width with the same line count (Chromium's
// ParagraphLineBreaker::AttemptParagraphBalancing — text_balance.go),
// and the direction-aware alignments resolve against the shaped
// document's base direction.
package gpui

import (
	"testing"
)

// TestBrowserTextFieldsRideTheStyleRun pins the wiring: ToRunAt carries
// the style's word-break, overflow-wrap and text-wrap tags, so every
// shaping path (the text leaf, StyledText, the inline paragraph's
// segments) pushes them into the native range styles.
func TestBrowserTextFieldsRideTheStyleRun(t *testing.T) {
	style := DefaultTextStyle()
	style.WordBreak = WordBreakBreakAll
	style.OverflowWrap = OverflowWrapAnywhere
	style.TextWrap = TextWrapBalance
	run := style.ToRunAt(7, 16)
	if run.WordBreak != WordBreakBreakAll {
		t.Errorf("run word-break = %v, want break-all", run.WordBreak)
	}
	if run.OverflowWrap != OverflowWrapAnywhere {
		t.Errorf("run overflow-wrap = %v, want anywhere", run.OverflowWrap)
	}
	if run.TextWrap != TextWrapBalance {
		t.Errorf("run text-wrap = %v, want balance", run.TextWrap)
	}
	if got := textWrapTag(run.TextWrap); got != 2 {
		t.Errorf("textWrapTag(balance) = %d, want 2 (the native balance tag)", got)
	}
}

// TestTextWrapBalanceBisectsTheWrappedWidth pins the bisection through
// the real text system: a wrapped line balanced to the narrowest width
// that keeps its line count — the narrowed active wrap constraint, the
// unchanged rows, and the single-line control Chrome leaves alone
// ("Balancing not needed for single line paragraphs"). This test needs
// the native text service.
func TestTextWrapBalanceBisectsTheWrappedWidth(t *testing.T) {
	ts, err := DefaultTextSystem()
	if err != nil {
		t.Fatalf("text system: %v", err)
	}
	const fontSize = 16.0
	const text = "Consequences determination possibilities of extraordinary architecture"
	wrap := float32(360)

	shape := func() *WrappedLine {
		t.Helper()
		run := TextRun{Len: len(text), Font: FontDescriptor{Family: "Segoe UI", Weight: DefaultFontWeight}}
		line, err := ts.ShapeText(TextLayoutRequest{
			Text:      text,
			FontSize:  fontSize,
			Runs:      []TextRun{run},
			WrapWidth: &wrap,
		})
		if err != nil {
			t.Fatalf("ShapeText: %v", err)
		}
		t.Cleanup(func() { _ = line.Dispose() })
		return line
	}

	// The control shape: the greedy wrap at 360px.
	line := shape()
	summary, err := line.Summary()
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	rows := summary.LineCount
	if rows < 2 {
		t.Fatalf("the text must wrap to 2+ rows under %vpx (got %d)", wrap, rows)
	}

	// Balancing: the same line count at the narrowest width.
	if err := balanceLine(line, wrap); err != nil {
		t.Fatalf("balanceLine: %v", err)
	}
	balanced, err := line.Summary()
	if err != nil {
		t.Fatalf("Summary (balanced): %v", err)
	}
	if balanced.LineCount != rows {
		t.Errorf("balanced line count = %d, want the unbalanced %d", balanced.LineCount, rows)
	}
	if balanced.WrapWidth == nil || *balanced.WrapWidth >= wrap {
		t.Fatalf("balanced wrap = %+v, want a narrowed width under %v", balanced.WrapWidth, wrap)
	}
	balancedWidth := *balanced.WrapWidth
	// The bisection's answer is minimal: a width a hair below it must
	// produce MORE rows (the next break opportunity engages).
	narrower := balancedWidth - 2
	if err := line.Relayout(&narrower, nil); err != nil {
		t.Fatalf("Relayout(%v): %v", narrower, err)
	}
	below, err := line.Summary()
	if err != nil {
		t.Fatalf("Summary (below): %v", err)
	}
	if below.LineCount <= rows {
		t.Errorf("a width below the balanced minimum kept %d rows, want more", below.LineCount)
	}

	// Chrome's single-line guard: a one-row paragraph is not balanced
	// (paragraph_line_breaker.cc — "Balancing not needed for single line
	// paragraphs").
	single := float32(500)
	one, err := ts.ShapeText(TextLayoutRequest{
		Text:      "One line",
		FontSize:  fontSize,
		Runs:      []TextRun{{Len: len("One line"), Font: FontDescriptor{Family: "Segoe UI", Weight: DefaultFontWeight}}},
		WrapWidth: &single,
	})
	if err != nil {
		t.Fatalf("ShapeText (single): %v", err)
	}
	t.Cleanup(func() { _ = one.Dispose() })
	if err := balanceLine(one, single); err != nil {
		t.Fatalf("balanceLine (single): %v", err)
	}
	oneSummary, err := one.Summary()
	if err != nil {
		t.Fatalf("Summary (single): %v", err)
	}
	if oneSummary.WrapWidth == nil || *oneSummary.WrapWidth != single {
		t.Errorf("a single-line document must keep its wrap width (got %+v)", oneSummary.WrapWidth)
	}
}

// TestWordBreakKeepAllSuppressesCJKBreaks pins the CJK gate through
// the real text system: ideographs are break opportunities at
// word-break: normal and stay unbroken at keep-all. (Lives here, not
// in textspec: the CJK fallback discovery interns a face, and
// textspec's recorded interning sequence is order-sensitive.)
func TestWordBreakKeepAllSuppressesCJKBreaks(t *testing.T) {
	ts, err := DefaultTextSystem()
	if err != nil {
		t.Fatalf("text system: %v", err)
	}
	const text = "東京都渋谷区神南"
	wrap := float32(90)
	shape := func(wordBreak WordBreak) *WrappedLine {
		t.Helper()
		line, err := ts.ShapeText(TextLayoutRequest{
			Text:      text,
			FontSize:  16,
			Runs:      []TextRun{{Len: len(text), Font: FontDescriptor{Family: "Segoe UI", Weight: DefaultFontWeight}, WordBreak: wordBreak}},
			WrapWidth: &wrap,
		})
		if err != nil {
			t.Fatalf("ShapeText(word-break %v): %v", wordBreak, err)
		}
		t.Cleanup(func() { _ = line.Dispose() })
		return line
	}
	if summary, err := shape(WordBreakNormal).Summary(); err != nil || summary.LineCount <= 1 {
		t.Fatalf("normal CJK line count = %d (err %v), want > 1 (ideograph breaks)", summary.LineCount, err)
	}
	keepAll := shape(WordBreakKeepAll)
	summary, err := keepAll.Summary()
	if err != nil {
		t.Fatalf("Summary (keep-all): %v", err)
	}
	if summary.LineCount != 1 {
		t.Fatalf("keep-all CJK line count = %d, want 1 (the run stays unbroken)", summary.LineCount)
	}
	if summary.Width <= wrap {
		t.Errorf("the kept CJK run must overflow the wrap width (width %v)", summary.Width)
	}
}

// TestTextAlignDirectionIsReported pins the is_rtl fact through the
// real text system: a Hebrew document's layout summary reports RTL (the
// paragraphs[0] base direction), the signal the paint layer resolves
// start/end against.
func TestTextAlignDirectionIsReported(t *testing.T) {
	ts, err := DefaultTextSystem()
	if err != nil {
		t.Fatalf("text system: %v", err)
	}
	const hebrew = "שלום עולם טוב מאוד"
	line, err := ts.ShapeText(TextLayoutRequest{
		Text:     hebrew,
		FontSize: 16,
		Runs:     []TextRun{{Len: len(hebrew), Font: FontDescriptor{Family: "Segoe UI", Weight: DefaultFontWeight}}},
	})
	if err != nil {
		t.Fatalf("ShapeText: %v", err)
	}
	t.Cleanup(func() { _ = line.Dispose() })
	summary, err := line.Summary()
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if !summary.IsRTL {
		t.Fatalf("a Hebrew document must report the RTL base direction")
	}
}

// TestTextAlignStartEndResolveAgainstDirection pins the direction-aware
// alignment resolution (the painted alignment the shaped summary's
// IsRTL resolves) and the native tag mapping.
func TestTextAlignStartEndResolveAgainstDirection(t *testing.T) {
	cases := []struct {
		align TextAlign
		isRTL bool
		want  TextAlign
	}{
		{TextAlignStart, false, TextAlignLeft},
		{TextAlignStart, true, TextAlignRight},
		{TextAlignEnd, false, TextAlignRight},
		{TextAlignEnd, true, TextAlignLeft},
		{TextAlignJustify, false, TextAlignJustify},
		{TextAlignLeft, true, TextAlignLeft},
		{TextAlignRight, true, TextAlignRight},
	}
	for _, tc := range cases {
		if got := resolvedTextAlign(tc.align, tc.isRTL); got != tc.want {
			t.Errorf("resolvedTextAlign(%v, rtl %v) = %v, want %v", tc.align, tc.isRTL, got, tc.want)
		}
	}
	// The native tag mapping: left/center/right stay paint-time (0);
	// start/end/justify carry the tag.
	for _, tc := range []struct {
		align TextAlign
		want  uint32
	}{
		{TextAlignLeft, 0},
		{TextAlignCenter, 0},
		{TextAlignRight, 0},
		{TextAlignStart, 1},
		{TextAlignEnd, 2},
		{TextAlignJustify, 3},
	} {
		if got := textAlignTag(tc.align); got != tc.want {
			t.Errorf("textAlignTag(%v) = %d, want %d", tc.align, got, tc.want)
		}
	}
}
