//go:build windows

// The browser-core ticket 43 (rev 16) browser text fields at the ABI
// seam: word-break and overflow-wrap drive the native range styles and
// the run record's text_align tag drives Parley's alignment (justify
// stretches the lines) — the vendored adapter's side-channel
// (TEXT_ABI.md's rev-16 section). The nowrap tag's gate is
// rev15_1_nowrap_test.go. The CJK keep-all and Hebrew direction gates
// live in the gpui package (gpui/text_browser_test.go): their shaping
// discovers fallback faces, and this package's recorded interning
// sequence (the pinned FontIds of textspec_test.go) is order-sensitive.
package textspec

import (
	"testing"

	"github.com/hoangvu12/gpui-go/internal/native"
)

// rev16Run returns one plain run with the rev-16 browser text fields
// (rev15_run_fields_test.go's browserRun carries every rev-15 field at
// once).
func rev16Run(text string, wordBreak, overflowWrap, textAlign uint32) native.TextRunSpec {
	return native.TextRunSpec{
		Len:          len(text),
		Family:       "Segoe UI",
		Weight:       400,
		WordBreak:    wordBreak,
		OverflowWrap: overflowWrap,
		TextAlign:    textAlign,
	}
}

// layoutSummaryOf reads one handle's layout summary.
func layoutSummaryOf(t *testing.T, svc *native.TextService, handle native.ShapingID) native.TextLayout {
	t.Helper()
	layout, err := svc.LayoutInfo(handle)
	if err != nil {
		t.Fatalf("LayoutInfo: %v", err)
	}
	return layout
}

// TestWordBreakAndOverflowWrapDriveLineBreaks pins the wired tags: a
// long URL has no break opportunities, so under a narrow wrap it stays
// one overflow line at normal, breaks with overflow-wrap: anywhere (and
// break-word, the same emergency class at the shaper), and breaks with
// word-break: break-all.
func TestWordBreakAndOverflowWrapDriveLineBreaks(t *testing.T) {
	svc := textService(t)
	const text = "https://example.com/a/very/long/unbreakable/url/path/segment"
	wrap := float32(150)

	shape := func(wordBreak, overflowWrap uint32) native.ShapingID {
		t.Helper()
		handle, err := svc.Shape(native.ShapeRequest{
			Text:      text,
			FontSize:  16,
			Runs:      []native.TextRunSpec{rev16Run(text, wordBreak, overflowWrap, 0)},
			WrapWidth: &wrap,
		})
		if err != nil {
			t.Fatalf("Shape(word-break %d, overflow-wrap %d): %v", wordBreak, overflowWrap, err)
		}
		t.Cleanup(func() { _ = svc.Dispose(handle) })
		return handle
	}

	plain := shape(0, 0)
	if got := lineCountOf(t, svc, plain); got != 1 {
		t.Fatalf("normal line count = %d, want 1 (no break opportunities)", got)
	}
	summary := layoutSummaryOf(t, svc, plain)
	if summary.Width <= wrap {
		t.Errorf("the unbroken URL must overflow the wrap width (width %v, wrap %v)", summary.Width, wrap)
	}

	anywhere := shape(0, native.TextOverflowWrapAnywhere)
	if got := lineCountOf(t, svc, anywhere); got <= 1 {
		t.Fatalf("overflow-wrap: anywhere line count = %d, want > 1 (emergency breaks)", got)
	}
	breakWord := shape(0, native.TextOverflowWrapBreakWord)
	if got := lineCountOf(t, svc, breakWord); got != lineCountOf(t, svc, anywhere) {
		t.Errorf("overflow-wrap: break-word line count = %d, want the anywhere count %d (the same emergency class)", got, lineCountOf(t, svc, anywhere))
	}
	breakAll := shape(native.TextWordBreakBreakAll, 0)
	if got := lineCountOf(t, svc, breakAll); got <= 1 {
		t.Fatalf("word-break: break-all line count = %d, want > 1 (every-cluster boundaries)", got)
	}

	// RunInfo echoes the tags exactly as they crossed the ABI.
	echoed, err := svc.RunInfo(breakAll, 0)
	if err != nil {
		t.Fatalf("RunInfo: %v", err)
	}
	if echoed.WordBreak != native.TextWordBreakBreakAll || echoed.TextAlign != 0 {
		t.Errorf("RunInfo echo = word-break %d align %d, want break-all / none", echoed.WordBreak, echoed.TextAlign)
	}
}

// TestTextAlignJustifyStretchesWrappedRows pins the align tag's
// behaviour: a justified document's stretched rows reach the wrap
// width (the pre-justification advances the line records report stay
// shorter), while the LAST row of the paragraph is start-aligned.
func TestTextAlignJustifyStretchesWrappedRows(t *testing.T) {
	svc := textService(t)
	const text = "The quick brown fox jumps over the lazy dog again and again"
	wrap := float32(150)

	shape := func(textAlign uint32) native.ShapingID {
		t.Helper()
		handle, err := svc.Shape(native.ShapeRequest{
			Text:      text,
			FontSize:  16,
			Runs:      []native.TextRunSpec{rev16Run(text, 0, 0, textAlign)},
			WrapWidth: &wrap,
		})
		if err != nil {
			t.Fatalf("Shape(align %d): %v", textAlign, err)
		}
		t.Cleanup(func() { _ = svc.Dispose(handle) })
		return handle
	}

	plain := shape(0)
	if summary := layoutSummaryOf(t, svc, plain); summary.IsRTL {
		t.Errorf("an ASCII document must report LTR")
	}

	justified := shape(native.TextTextAlignJustify)
	summary := layoutSummaryOf(t, svc, justified)
	if summary.LineCount < 3 {
		t.Fatalf("justify line count = %d, want >= 3 (the wrapped rows)", summary.LineCount)
	}
	glyphs, err := svc.Glyphs(justified)
	if err != nil {
		t.Fatalf("Glyphs: %v", err)
	}
	fragments, err := svc.Fragments(justified)
	if err != nil {
		t.Fatalf("Fragments: %v", err)
	}
	lastLine := summary.LineCount - 1
	// The justified rows' glyph positions carry the stretch (the line
	// records' advances stay the pre-justification content advances —
	// the stretch lives in the glyph stream). firstRowMaxX reads one
	// row's painted extent: its line record's fragment range indexes
	// the fragment dump, each fragment's glyph range the glyph dump.
	firstRowMaxX := func(handle native.ShapingID) float32 {
		glyphs, err := svc.Glyphs(handle)
		if err != nil {
			t.Fatalf("Glyphs: %v", err)
		}
		fragments, err := svc.Fragments(handle)
		if err != nil {
			t.Fatalf("Fragments: %v", err)
		}
		line, err := svc.Line(handle, 0, 24)
		if err != nil {
			t.Fatalf("Line(0): %v", err)
		}
		maxX := float32(0)
		for index := line.FragmentStart; index < line.FragmentEnd; index++ {
			fragment := fragments[index]
			for _, glyph := range glyphs[fragment.GlyphStart : fragment.GlyphStart+fragment.GlyphCount] {
				if glyph.X > maxX {
					maxX = glyph.X
				}
			}
		}
		return maxX
	}
	plainRowMax := firstRowMaxX(plain)
	justifiedRowMax := firstRowMaxX(justified)
	if justifiedRowMax <= plainRowMax+1 {
		t.Fatalf("justify must stretch row 0 past its unjustified extent (%v -> %v)", plainRowMax, justifiedRowMax)
	}
	if justifiedRowMax < wrap-4 || justifiedRowMax > wrap+4 {
		t.Errorf("justified row 0 extent = %v, want ≈ the wrap width %v", justifiedRowMax, wrap)
	}
	// The last row stays start-aligned: it ends short of the wrap width.
	lastLineHandle, err := svc.Line(justified, lastLine, 24)
	if err != nil {
		t.Fatalf("Line(last): %v", err)
	}
	lastMaxX := float32(0)
	for index := lastLineHandle.FragmentStart; index < lastLineHandle.FragmentEnd; index++ {
		fragment := fragments[index]
		for _, glyph := range glyphs[fragment.GlyphStart : fragment.GlyphStart+fragment.GlyphCount] {
			if glyph.X > lastMaxX {
				lastMaxX = glyph.X
			}
		}
	}
	if lastMaxX >= wrap-10 {
		t.Errorf("the last row must stay start-aligned (extent %v, wrap %v)", lastMaxX, wrap)
	}

	// RunInfo echoes the align tag.
	echoed, err := svc.RunInfo(justified, 0)
	if err != nil {
		t.Fatalf("RunInfo: %v", err)
	}
	if echoed.TextAlign != native.TextTextAlignJustify {
		t.Errorf("RunInfo align echo = %d, want justify", echoed.TextAlign)
	}
}
