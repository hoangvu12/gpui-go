//go:build windows

package richtextspec

// Base ticket 35's real-stack slice: a multi-run StyledText inside a
// Block paragraph drawn into a REAL Win32 window through the real inline
// layout (gpui/inline_real.go pieces over the native shaping seam). The
// paragraph paints each piece with the document's runs cut to the piece
// (R:text_system/line.rs:195-319 paint_inline_layout: every fragment in
// its own PaintStyle): every glyph sprite carries its run's color under
// a content mask bounded by the run's selection rectangle, and the
// decorations and background follow the run rectangles. Exact advances
// depend on the machine's system font, so the run boundaries are
// computed independently through the adapter. Environment expectation:
// this session creates real windows; failures fail, never skip.

import (
	"math"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// inlineRealView renders a Block paragraph: a plain prefix in the
// ambient color, then the three-run StyledText.
type inlineRealView struct {
	ambient gpui.Hsla
	prefix  string
	text    string
	runs    []gpui.TextRun
}

// Render implements gpui.Render[inlineRealView].
func (v *inlineRealView) Render(w *gpui.Window, cx *gpui.Context[inlineRealView]) gpui.AnyElement {
	return gpui.Div().Block().W(gpui.PxLength(460)).TextColor(v.ambient).
		Child(v.prefix).
		Child(gpui.NewStyledText(v.text).WithRuns(v.runs)).
		IntoElement()
}

// TestRealInlineStyledTextRuns draws the paragraph in a real window and
// pins the per-run paint transcript of the StyledText piece.
func TestRealInlineStyledTextRuns(t *testing.T) {
	ambient := gpui.Hsla{H: 0, S: 0, L: 0.2, A: 1}
	gray := gpui.Hsla{H: 0, S: 0, L: 0.5, A: 1}
	const prefix = "ab "
	const text = "Hello brave new world"
	base := gpui.DefaultTextStyle()
	r1, r2, r3 := base.ToRun(6), base.ToRun(6), base.ToRun(9)
	r1.Color = red
	r2.Color, r2.Underline = green, &gpui.UnderlineStyle{Thickness: 1}
	r3.Color, r3.Strikethrough, r3.BackgroundColor = blue, &gpui.StrikethroughStyle{Thickness: 1}, ptr(gray)
	runs := []gpui.TextRun{r1, r2, r3}

	host, window := bootRealWindow(t, "richtextspec inline runs", func(w *gpui.Window, app *gpui.App) gpui.View {
		return gpui.ViewOf(gpui.NewEntity(app, w.Scope(), func(v *inlineRealView, cx *gpui.Context[inlineRealView]) {
			v.ambient, v.prefix, v.text, v.runs = ambient, prefix, text, runs
		}))
	})
	scale, err := window.ScaleFactor()
	if err != nil {
		t.Fatalf("window scale: %v", err)
	}

	var scene *gpui.Scene
	var facts []gpui.InlineParagraphFacts
	onHost(t, host, func() {
		s, err := gpui.DrawWindowFrame(window)
		if err != nil {
			t.Errorf("DrawWindowFrame: %v", err)
			return
		}
		scene = s
		facts = gpui.WindowInlineFacts(window)
	})
	if scene == nil {
		t.FailNow()
	}
	if len(facts) != 1 || facts[0].Text != prefix+text || len(facts[0].Rows) != 1 || len(facts[0].Spans) != 2 {
		t.Fatalf("inline facts = %+v, want one one-row paragraph %q with two spans", facts, prefix+text)
	}
	paragraph := facts[0]
	row := paragraph.Rows[0]
	styledSpan := paragraph.Spans[1]
	if styledSpan.Text != text || len(styledSpan.Fragments) != 1 {
		t.Fatalf("StyledText span = %+v, want one fragment of %q", styledSpan, text)
	}

	// The run boundaries inside the StyledText piece, computed through the
	// adapter with the same text, size and fonts (paint styles never
	// change shaped geometry), placed at the piece's (snapped) origin.
	ts, err := gpui.DefaultTextSystem()
	if err != nil {
		t.Fatalf("text system: %v", err)
	}
	doc, err := ts.ShapeText(gpui.TextLayoutRequest{Text: text, FontSize: 16, Runs: runs})
	if err != nil {
		t.Fatalf("ShapeText: %v", err)
	}
	defer func() { _ = doc.Dispose() }()
	rectOf := func(start, end int) gpui.TextRect {
		rects, err := doc.SelectionRects(start, end, 26)
		if err != nil || len(rects) != 1 {
			t.Fatalf("SelectionRects(%d,%d) = %v, %v; want one rectangle", start, end, rects, err)
		}
		return rects[0]
	}
	mid, tail := rectOf(6, 12), rectOf(12, 21)
	pieceX := paragraph.Origin.X + styledSpan.Fragments[0].Origin.X
	start := pieceX * scale
	b1 := (pieceX + mid.X) * scale
	b2 := (pieceX + mid.X + mid.W) * scale
	end := (pieceX + tail.X + tail.W) * scale
	const tol = 2.0

	monos, err := scene.MonochromeSprites()
	if err != nil {
		t.Fatalf("mono sprites: %v", err)
	}
	subs, err := scene.SubpixelSprites()
	if err != nil {
		t.Fatalf("subpixel sprites: %v", err)
	}
	counts := map[string]int{}
	for i, s := range sceneSprites(monos, subs) {
		left, right := s.mask.Origin.X, s.mask.Origin.X+s.mask.Size.Width
		center := s.bounds.Origin.X + s.bounds.Size.Width/2
		switch {
		case colorNear(s.color, ambient):
			counts["ambient"]++
			if center > start+tol {
				t.Errorf("ambient sprite %d (center %v) sits inside the StyledText (starts %v)", i, center, start)
			}
		case colorNear(s.color, red):
			counts["red"]++
			if right > b1+tol || center < start-tol {
				t.Errorf("red sprite %d mask right %v / center %v leaves run 1 [%v,%v]", i, right, center, start, b1)
			}
		case colorNear(s.color, green):
			counts["green"]++
			if left < b1-tol || right > b2+tol {
				t.Errorf("green sprite %d mask [%v,%v] leaves the run extent [%v,%v]", i, left, right, b1, b2)
			}
		case colorNear(s.color, blue):
			counts["blue"]++
			if left < b2-tol {
				t.Errorf("blue sprite %d mask left %v crosses the run boundary %v", i, left, b2)
			}
		default:
			t.Errorf("sprite %d color %+v is neither the ambient nor a run color", i, s.color)
		}
	}
	if counts["ambient"] == 0 || counts["red"] == 0 || counts["green"] == 0 || counts["blue"] == 0 {
		t.Fatalf("sprites per color = %v, want the prefix and every run painted in its own color", counts)
	}

	underlines, err := scene.Underlines()
	if err != nil {
		t.Fatalf("underlines: %v", err)
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
	if len(underlines) != 2 || under == nil || strike == nil {
		t.Fatalf("decorations = %+v, want a green underline and a blue strikethrough", underlines)
	}
	if math.Abs(float64(under.Bounds.Origin.X-b1)) > tol || math.Abs(float64(under.Bounds.Size.Width-(b2-b1))) > tol {
		t.Errorf("underline x/width = %v/%v, want ~%v/%v (run 2's rectangle)", under.Bounds.Origin.X, under.Bounds.Size.Width, b1, b2-b1)
	}
	if math.Abs(float64(strike.Bounds.Origin.X-b2)) > tol || math.Abs(float64(strike.Bounds.Size.Width-(end-b2))) > tol {
		t.Errorf("strikethrough x/width = %v/%v, want ~%v/%v (run 3's rectangle)", strike.Bounds.Origin.X, strike.Bounds.Size.Width, b2, end-b2)
	}
	top := (paragraph.Origin.Y + row.Origin.Y) * scale
	bottom := top + row.Size.Height*scale
	if !(strike.Bounds.Origin.Y < under.Bounds.Origin.Y) || strike.Bounds.Origin.Y < top || under.Bounds.Origin.Y > bottom {
		t.Errorf("decoration rows: strikethrough y %v, underline y %v, want strike above underline inside the row [%v,%v]",
			strike.Bounds.Origin.Y, under.Bounds.Origin.Y, top, bottom)
	}

	grays := quadsColored(t, scene, gray)
	if len(grays) != 1 {
		t.Fatalf("background quads = %d, want 1 (run 3)", len(grays))
	}
	if q := grays[0].Bounds; math.Abs(float64(q.Origin.X-b2)) > tol || math.Abs(float64(q.Origin.X+q.Size.Width-end)) > tol ||
		math.Abs(float64(q.Origin.Y-top)) > tol || math.Abs(float64(q.Size.Height-row.Size.Height*scale)) > tol {
		t.Errorf("run 3 background = %v, want x [%v,%v] over the row [%v, +%v]", q, b2, end, top, row.Size.Height*scale)
	}
}
