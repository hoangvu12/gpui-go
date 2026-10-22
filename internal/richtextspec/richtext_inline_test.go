package richtextspec

// Base ticket 35: a multi-run StyledText inside an inline (Block)
// paragraph keeps its per-run backgrounds and decorations. The paragraph
// owns the text's paint (R:elements/text.rs:1002-1024) and paints each
// fragment with its own PaintStyle (R:text_system/line.rs:195-225
// paint_inline_layout → paint_visual_line → paint_text_fragment →
// paint_fragment_decorations_at, line.rs:247-319,421-464), the
// background pass first (R:elements/div/inline.rs:475-499). The glyph
// colors (empty rasters on the test text system) are pinned in-package
// (gpui/inline_runs_test.go) and by the real window
// (richtext_inline_real_test.go).

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// TestInlineStyledTextRunDecorationsTranscript draws a Block paragraph
// of a plain "ab " text followed by the three-run StyledText of
// TestStyledTextRunDecorationsTranscript, and pins the same per-run
// records shifted by the plain text's 28.8px advance. Row geometry
// (scale 2, 16px, test layout_inline): row height 26, baseline 23.4
// (align_inline_boxes over ascent 16.4 / descent -4.4 with half leading
// 7); the background quad spans the row height, the underline sits at
// baseline + descent * 0.618 and the strikethrough at baseline - ascent
// * 0.5.
func TestInlineStyledTextRunDecorationsTranscript(t *testing.T) {
	gray := gpui.Hsla{H: 0, S: 0, L: 0.5, A: 1}
	_, window, _, scene := openStyled(t, func(v *styledView) gpui.AnyElement {
		base := gpui.DefaultTextStyle()
		r1, r2, r3 := base.ToRun(3), base.ToRun(3), base.ToRun(3)
		r1.Color, r1.BackgroundColor = red, ptr(yellow)
		r2.Color, r2.Underline = green, &gpui.UnderlineStyle{Thickness: 1}
		r3.Color, r3.Strikethrough, r3.BackgroundColor = blue, &gpui.StrikethroughStyle{Thickness: 1}, ptr(gray)
		styled := gpui.NewStyledText("aaabbbccc").WithRuns([]gpui.TextRun{r1, r2, r3})
		return gpui.Div().Block().W(gpui.PxLength(400)).Child("ab ").Child(styled).IntoElement()
	})
	const scale = 2

	facts := gpui.WindowInlineFacts(window)
	if len(facts) != 1 || facts[0].Text != "ab aaabbbccc" || len(facts[0].Rows) != 1 {
		t.Fatalf("inline facts = %+v, want one one-row paragraph %q", facts, "ab aaabbbccc")
	}
	if row := facts[0].Rows[0]; row.Size.Height != 26 || row.Baseline != 23.4 {
		t.Fatalf("row = %+v, want height 26, baseline 23.4", row)
	}

	checkBounds := func(what string, got gpui.Bounds, x, y, w, h float32) {
		t.Helper()
		if !near(got.Origin.X, x*scale, 1) || !near(got.Origin.Y, y*scale, 1) || !near(got.Size.Width, w*scale, 1) || !near(got.Size.Height, h*scale, 1) {
			t.Errorf("%s bounds = %v, want ~(%v,%v %vx%v) logical at scale 2", what, got, x, y, w, h)
		}
	}
	yellowQuads := quadsColored(t, scene, yellow)
	grayQuads := quadsColored(t, scene, gray)
	if len(yellowQuads) != 1 || len(grayQuads) != 1 {
		t.Fatalf("background quads yellow=%d gray=%d, want 1 each (the paragraph lost its run backgrounds)", len(yellowQuads), len(grayQuads))
	}
	checkBounds("run 1 background", yellowQuads[0].Bounds, 28.8, 0, 28.8, 26)
	checkBounds("run 3 background", grayQuads[0].Bounds, 86.4, 0, 28.8, 26)

	underlines, err := scene.Underlines()
	if err != nil {
		t.Fatalf("scene underlines: %v", err)
	}
	if len(underlines) != 2 {
		t.Fatalf("underline records = %d, want 2 (run 2's underline, run 3's strikethrough): %+v", len(underlines), underlines)
	}
	var under, strike *gpui.Underline
	for i := range underlines {
		switch {
		case colorNear(underlines[i].Color, green):
			under = &underlines[i]
		case colorNear(underlines[i].Color, blue):
			strike = &underlines[i]
		}
	}
	if under == nil || strike == nil {
		t.Fatalf("decoration colors = %+v, want the run colors green (underline) and blue (strikethrough)", underlines)
	}
	baseline := float32(23.4)
	if !near(under.Bounds.Origin.X, 57.6*scale, 1) || !near(under.Bounds.Size.Width, 28.8*scale, 1) ||
		!near(under.Bounds.Origin.Y, (baseline+(-4.4)*0.618)*scale, 1) {
		t.Errorf("underline bounds = %v, want x 57.6, width 28.8, y baseline + descent*0.618 (logical, scale 2)", under.Bounds)
	}
	if !near(strike.Bounds.Origin.X, 86.4*scale, 1) || !near(strike.Bounds.Size.Width, 28.8*scale, 1) ||
		!near(strike.Bounds.Origin.Y, (baseline-16.4*0.5)*scale, 1) || strike.Wavy {
		t.Errorf("strikethrough bounds = %v (wavy %v), want x 86.4, width 28.8, y baseline - ascent*0.5, not wavy", strike.Bounds, strike.Wavy)
	}
}
