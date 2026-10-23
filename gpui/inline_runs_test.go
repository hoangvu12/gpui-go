package gpui

// Base ticket 35's in-package tests: a multi-run StyledText inside an
// inline paragraph paints each run's glyphs in the run's own color
// (R:text_system/line.rs:195-319 paint_inline_layout →
// paint_text_fragment with fragment.style.color). The deterministic test
// text system's glyph rasters are empty (no sprites), so the glyph
// transcript is read from a recording wrapper around the test system's
// paint pieces: every paintLine call's color and content mask. The
// public decoration/background transcript and the real-window sprites
// live in internal/richtextspec.

import (
	"testing"
)

// recordedLinePaint is one recorded paintLine call.
type recordedLinePaint struct {
	color Hsla
	mask  Bounds
}

// recordingTextSystem is the deterministic test text system with every
// inline piece's shaped document wrapped in a paint recorder.
type recordingTextSystem struct {
	*testTextSystem
	paints *[]recordedLinePaint
}

// layoutInline implements windowTextSystem: the test system's layout
// with recording pieces.
func (r *recordingTextSystem) layoutInline(request inlineLayoutRequest) (inlineLayout, error) {
	layout, err := r.testTextSystem.layoutInline(request)
	for i := range layout.pieces {
		layout.pieces[i].shaped = &recordingShapedText{shapedText: layout.pieces[i].shaped, paints: r.paints}
	}
	return layout, err
}

// recordingShapedText records paintLine calls.
type recordingShapedText struct {
	shapedText
	paints *[]recordedLinePaint
}

// paintLine implements shapedText.
func (r *recordingShapedText) paintLine(w *Window, lineIndex int, origin Point, baseline float32, color Hsla) error {
	*r.paints = append(*r.paints, recordedLinePaint{color: color, mask: ContentMask(w)})
	return r.shapedText.paintLine(w, lineIndex, origin, baseline, color)
}

// inlineRunsView renders a Block paragraph: a plain "ab " text, then a
// three-run StyledText.
type inlineRunsView struct {
	ambient Hsla
	runs    []TextRun
}

// Render implements Render[inlineRunsView].
func (v *inlineRunsView) Render(w *Window, cx *Context[inlineRunsView]) AnyElement {
	return Div().Block().W(PxLength(400)).TextColor(v.ambient).
		Child("ab ").
		Child(NewStyledText("aaabbbccc").WithRuns(v.runs)).
		IntoElement()
}

// TestInlineParagraphPaintsStyledRunColors draws the paragraph on a test
// window and pins the glyph paint transcript: the plain text in the
// ambient color, then one masked paint per run color, each mask bounded
// at the run's selection-rectangle edges (em 9.6 at 16px: "ab " is
// [0,28.8), the runs [28.8,57.6), [57.6,86.4), [86.4,115.2)); the
// paragraph's outer edges stay open (the current content mask).
func TestInlineParagraphPaintsStyledRunColors(t *testing.T) {
	ambient := Hsla{H: 0, S: 0, L: 0.2, A: 1}
	red := Hsla{H: 0, S: 1, L: 0.5, A: 1}
	green := Hsla{H: 120.0 / 360.0, S: 1, L: 0.25, A: 1}
	blue := Hsla{H: 240.0 / 360.0, S: 1, L: 0.5, A: 1}
	base := DefaultTextStyle()
	r1, r2, r3 := base.ToRun(3), base.ToRun(3), base.ToRun(3)
	r1.Color, r2.Color, r3.Color = red, green, blue

	ta := NewTestApp()
	app := ta.App()
	window := NewTestWindow(app, Size{Width: 600, Height: 200})
	t.Cleanup(func() { CloseTestWindow(window) })
	var paints []recordedLinePaint
	drawState(window).text = &recordingTextSystem{testTextSystem: &testTextSystem{}, paints: &paints}
	view := NewEntity(app, window.Scope(), func(v *inlineRunsView, cx *Context[inlineRunsView]) {
		v.ambient, v.runs = ambient, []TextRun{r1, r2, r3}
	})
	window.SetRootView(ViewOf(view))
	if _, err := DrawWindowFrame(window); err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}
	if facts := WindowInlineFacts(window); len(facts) != 1 || facts[0].Text != "ab aaabbbccc" {
		t.Fatalf("inline facts = %+v, want one paragraph %q", facts, "ab aaabbbccc")
	}

	want := []struct {
		color       Hsla
		left, right float32 // NaN-free sentinels: -1 = open edge
	}{
		{ambient, -1, 28.8},
		{red, 28.8, 57.6},
		{green, 57.6, 86.4},
		{blue, 86.4, -1},
	}
	if len(paints) != len(want) {
		t.Fatalf("glyph paints = %+v, want %d (one per color span)", paints, len(want))
	}
	near := func(a, b float32) bool { return abs32(a-b) <= 1e-3 }
	for i, w := range want {
		got := paints[i]
		if got.color != w.color {
			t.Errorf("paint %d color = %+v, want %+v", i, got.color, w.color)
		}
		if w.left >= 0 && !near(got.mask.Left(), w.left) {
			t.Errorf("paint %d mask left = %v, want %v", i, got.mask.Left(), w.left)
		}
		if w.left < 0 && got.mask.Left() > 0 {
			t.Errorf("paint %d mask left = %v, want the open content-mask edge", i, got.mask.Left())
		}
		if w.right >= 0 && !near(got.mask.Right(), w.right) {
			t.Errorf("paint %d mask right = %v, want %v", i, got.mask.Right(), w.right)
		}
		if w.right < 0 && got.mask.Right() < 115.2 {
			t.Errorf("paint %d mask right = %v, want the open content-mask edge", i, got.mask.Right())
		}
	}
}

// TestCutPaintRuns pins the piece run cut: runs rebased to the range
// start, clipped at both ends, paint fields kept.
func TestCutPaintRuns(t *testing.T) {
	red := Hsla{H: 0, S: 1, L: 0.5, A: 1}
	under := &UnderlineStyle{Thickness: 1}
	runs := []TextRun{{Len: 3}, {Len: 4, Color: red, Underline: under}, {Len: 2}}
	got := cutPaintRuns(runs, 2, 8)
	if len(got) != 3 || got[0].Len != 1 || got[1].Len != 4 || got[2].Len != 1 {
		t.Fatalf("cutPaintRuns(2,8) = %+v, want lengths 1,4,1", got)
	}
	if got[1].Color != red || got[1].Underline != under {
		t.Fatalf("cut run lost its paint style: %+v", got[1])
	}
	if got := cutPaintRuns(runs, 3, 7); len(got) != 1 || got[0].Len != 4 || got[0].Color != red {
		t.Fatalf("cutPaintRuns(3,7) = %+v, want the single red run", got)
	}
	if got := cutPaintRuns(runs, 9, 9); got != nil {
		t.Fatalf("cutPaintRuns(9,9) = %+v, want nil", got)
	}
}
