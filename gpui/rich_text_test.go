package gpui

// Base ticket 10's in-package tests: the unexported pieces of the rich
// text port (the run computation of StyledText and the per-run painter's
// segment merge). The public behavior lives in internal/richtextspec.
// (The legacy LineWrapper and its word-character table moved to the
// gpui-go-base repository with Base in browser-core ticket 75.)

import (
	"strings"
	"testing"
)

// TestComputeStyledRunsFillsGaps pins compute_runs (R:elements/text.rs:
// 452-477): default runs fill the gaps, highlighted runs carry the
// highlighted default style, the tail is a default run.
func TestComputeStyledRunsFillsGaps(t *testing.T) {
	text := "hello brave world"
	base := DefaultTextStyle()
	red := Hsla{H: 0, S: 1, L: 0.5, A: 1}
	runs := computeStyledRuns(text, base, []HighlightRange{
		{Range: TextRange{Start: 6, End: 11}, Style: HighlightStyleFromColor(red)},
	}, 16)
	if len(runs) != 3 {
		t.Fatalf("runs = %d, want 3 (gap, highlight, tail)", len(runs))
	}
	if runs[0].Len != 6 || runs[1].Len != 5 || runs[2].Len != 6 {
		t.Fatalf("run lengths = %d/%d/%d, want 6/5/6", runs[0].Len, runs[1].Len, runs[2].Len)
	}
	if runs[0].Color != base.Color || runs[2].Color != base.Color {
		t.Fatalf("default runs take the default color")
	}
	if want := base.Color.Blend(red); runs[1].Color != want {
		t.Fatalf("highlighted run color = %+v, want the blended %+v", runs[1].Color, want)
	}
	if err := validateRuns(text, runs); err != nil {
		t.Fatalf("computed runs do not cover the text: %v", err)
	}
}

// TestApplyFontFamilyOverrides pins apply_font_family_overrides
// (R:elements/text.rs:502-521): only runs entirely inside an override
// take its family.
func TestApplyFontFamilyOverrides(t *testing.T) {
	runs := []TextRun{{Len: 3, Font: FontDescriptor{Family: "A"}}, {Len: 3, Font: FontDescriptor{Family: "A"}}, {Len: 3, Font: FontDescriptor{Family: "A"}}}
	applyFontFamilyOverrides(runs, []FontFamilyOverride{{Range: TextRange{Start: 3, End: 9}, Family: "Mono"}})
	got := []string{runs[0].Font.Family, runs[1].Font.Family, runs[2].Font.Family}
	if strings.Join(got, ",") != "A,Mono,Mono" {
		t.Fatalf("families = %v, want A,Mono,Mono", got)
	}
	runs = []TextRun{{Len: 4, Font: FontDescriptor{Family: "A"}}}
	applyFontFamilyOverrides(runs, []FontFamilyOverride{{Range: TextRange{Start: 1, End: 3}, Family: "Mono"}})
	if runs[0].Font.Family != "A" {
		t.Fatalf("a run straddling the override changed family to %q", runs[0].Font.Family)
	}
}

// TestValidateRunsRejectsBadCoverage pins the run contract
// (R:elements/text.rs:540-553): short, long and mid-character runs are
// rejected.
func TestValidateRunsRejectsBadCoverage(t *testing.T) {
	text := "café" // é is two bytes
	cases := []struct {
		name string
		runs []TextRun
		ok   bool
	}{
		{"exact", []TextRun{{Len: 5}}, true},
		{"split on boundary", []TextRun{{Len: 3}, {Len: 2}}, true},
		{"short", []TextRun{{Len: 4}}, false},
		{"long", []TextRun{{Len: 6}}, false},
		{"mid character", []TextRun{{Len: 4}, {Len: 1}}, false},
		{"none", nil, false},
	}
	for _, c := range cases {
		err := validateRuns(text, c.runs)
		if (err == nil) != c.ok {
			t.Errorf("%s: validateRuns error = %v, want ok=%v", c.name, err, c.ok)
		}
	}
}

// TestMergeDecorationRuns pins the legacy shape_line merge (legacy
// text_system.rs:442-460): equal paint styles merge, any difference
// splits.
func TestMergeDecorationRuns(t *testing.T) {
	red := Hsla{H: 0, S: 1, L: 0.5, A: 1}
	blue := Hsla{H: 0.6, S: 1, L: 0.5, A: 1}
	underline := UnderlineStyle{Thickness: 1}
	merged := mergeDecorationRuns([]TextRun{
		{Len: 1, Color: red},
		{Len: 2, Color: red, Font: FontDescriptor{Family: "Other"}},
		{Len: 3, Color: red, Underline: &underline},
		{Len: 4, Color: blue},
	})
	if len(merged) != 3 || merged[0].Len != 3 || merged[1].Len != 3 || merged[2].Len != 4 {
		t.Fatalf("merged = %+v, want lengths 3/3/4", merged)
	}
}
