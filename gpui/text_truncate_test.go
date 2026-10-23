package gpui

// zterm ticket 11: text truncation with an ellipsis. The candidate test
// is the pin's truncation_candidates_keep_complete_graphemes_and_cover_
// output_with_runs (R:elements/text.rs:1330-1369); the element tests
// draw Text and StyledText under Truncate / TextEllipsisStart /
// TextEllipsisMiddle on a test window (em 9.6 at 16px, so a 100px box
// holds ten one-unit glyphs).

import (
	"strings"
	"testing"
)

func TestTruncationCandidatesKeepCompleteGraphemesAndCoverOutputWithRuns(t *testing.T) {
	const family = "👩‍👩‍👧‍👦"
	text := "Aé" + family + "Z"
	split := len("Aé")
	runs := []TextRun{{Len: split}, {Len: len(text) - split}}
	boundaries := graphemeBoundaries(text)
	for _, c := range []struct {
		direction TruncateFrom
		keep      int
		want      string
	}{
		{TruncateFromStart, 2, "…" + family + "Z"},
		{TruncateFromEnd, 2, "Aé…"},
		{TruncateFromMiddle, 3, "Aé…Z"},
	} {
		candidate, candidateRuns := makeTruncationCandidate(text, boundaries, c.keep, "…", runs, c.direction)
		if candidate != c.want {
			t.Errorf("direction %d: candidate %q, want %q", c.direction, candidate, c.want)
		}
		total := 0
		for _, run := range candidateRuns {
			total += run.Len
		}
		if total != len(candidate) {
			t.Errorf("direction %d: style runs cover %d bytes of %q (%d)", c.direction, total, candidate, len(candidate))
		}
	}
	if len(runs) != 2 || runs[0].Len != split {
		t.Errorf("source runs mutated: %+v", runs)
	}
}

// The End candidate trims trailing whitespace and ASCII punctuation
// before the affix (R:elements/text.rs:1228-1233).
func TestTruncationEndCandidateTrimsWhitespaceAndPunctuation(t *testing.T) {
	text := "ab, cd"
	candidate, runs := makeTruncationCandidate(text, graphemeBoundaries(text), 4, "…", []TextRun{{Len: len(text)}}, TruncateFromEnd)
	if candidate != "ab…" || len(runs) != 1 || runs[0].Len != len("ab…") {
		t.Errorf("candidate %q runs %+v, want %q in one run", candidate, runs, "ab…")
	}
}

// truncatePaintSystem is the deterministic test text system recording
// the text of every document painted through the ordinary text paint.
type truncatePaintSystem struct {
	*testTextSystem
	painted *[]string
}

func (r *truncatePaintSystem) shape(text string, fontSize float32, font FontDescriptor, wrapWidth *float32, lineClamp *uint32) (shapedText, error) {
	shaped, err := r.testTextSystem.shape(text, fontSize, font, wrapWidth, lineClamp)
	if err != nil {
		return nil, err
	}
	return &truncatePaintRecorder{shapedText: shaped, text: text, painted: r.painted}, nil
}

type truncatePaintRecorder struct {
	shapedText
	text    string
	painted *[]string
}

func (r *truncatePaintRecorder) paint(w *Window, origin Point, lineHeight float32, color Hsla) error {
	*r.painted = append(*r.painted, r.text)
	return r.shapedText.paint(w, origin, lineHeight, color)
}

type truncateView struct {
	style func(*DivElement) *DivElement
	child func() AnyElement
}

func (v *truncateView) Render(w *Window, cx *Context[truncateView]) AnyElement {
	return Div().Child(v.style(Div().FlexCol().W(PxLength(100))).Child(v.child())).IntoElement()
}

func drawTruncateView(t *testing.T, style func(*DivElement) *DivElement, child func() AnyElement) []string {
	t.Helper()
	ta := NewTestApp()
	app := ta.App()
	window := NewTestWindow(app, Size{Width: 600, Height: 200})
	t.Cleanup(func() { CloseTestWindow(window) })
	var painted []string
	drawState(window).text = &truncatePaintSystem{testTextSystem: &testTextSystem{}, painted: &painted}
	view := NewEntity(app, window.Scope(), func(v *truncateView, _ *Context[truncateView]) {
		v.style, v.child = style, child
	})
	window.SetRootView(ViewOf(view))
	for frame := range 2 {
		painted = painted[:0]
		if _, err := DrawWindowFrame(window); err != nil {
			t.Fatalf("frame %d: DrawWindowFrame: %v", frame, err)
		}
	}
	return painted
}

func TestTextElementTruncatesWithEllipsis(t *testing.T) {
	const long = "Hello wonderful world"
	for _, c := range []struct {
		name  string
		style func(*DivElement) *DivElement
		want  string
	}{
		{"end", func(d *DivElement) *DivElement { return d.Truncate() }, "Hello won…"},
		{"start", func(d *DivElement) *DivElement { return d.OverflowHidden().WhitespaceNowrap().TextEllipsisStart() }, "…ful world"},
		{"middle", func(d *DivElement) *DivElement { return d.OverflowHidden().WhitespaceNowrap().TextEllipsisMiddle() }, "Hello …rld"},
		{"fits", func(d *DivElement) *DivElement { return d.Truncate() }, ""},
		{"no overflow style", func(d *DivElement) *DivElement { return d.OverflowHidden().WhitespaceNowrap() }, long},
	} {
		t.Run(c.name, func(t *testing.T) {
			text, want := long, c.want
			if c.name == "fits" {
				text, want = "Hello", "Hello"
			}
			painted := drawTruncateView(t, c.style, func() AnyElement { return Text(text) })
			if len(painted) != 1 || painted[0] != want {
				t.Errorf("painted %q, want [%q]", painted, want)
			}
		})
	}
}

func TestStyledTextTruncatesRunsWithEllipsis(t *testing.T) {
	red := Hsla{H: 0, S: 1, L: 0.5, A: 1}
	base := DefaultTextStyle()
	r1, r2 := base.ToRun(len("Hello ")), base.ToRun(len("wonderful world"))
	r2.Color = red
	var styled *StyledText
	painted := drawTruncateView(t, func(d *DivElement) *DivElement { return d.Truncate() }, func() AnyElement {
		styled = NewStyledText("Hello wonderful world").WithRuns([]TextRun{r1, r2})
		return styled.IntoElement()
	})
	_ = painted
	if got := styled.Layout().Text(); got != "Hello won…" {
		t.Fatalf("Text() = %q, want %q", got, "Hello won…")
	}
	inner := styled.Layout().measured()
	if len(inner.runs) != 2 || inner.runs[0].Len != len("Hello ") || inner.runs[1].Len != len("won…") || inner.runs[1].Color != red {
		t.Errorf("runs = %+v, want [6, %d red]", inner.runs, len("won…"))
	}
	if inner.size == nil || inner.size.Width > 100+0.01 {
		t.Errorf("size = %+v, want width <= 100", inner.size)
	}
	if !strings.HasSuffix(styled.Layout().Text(), Ellipsis) {
		t.Errorf("text %q lacks the ellipsis", styled.Layout().Text())
	}
}

// With a line clamp the wrapped text keeps at most the clamped rows and
// ends in the ellipsis (R:elements/text.rs:829-860 width x line_clamp,
// 1176-1189 the row check). Shapes through the real text service (no
// window is shown): the test stub never wraps.
func TestStyledTextLineClampTruncatesWrappedText(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	window := NewTestWindow(app, Size{Width: 600, Height: 200})
	t.Cleanup(func() { CloseTestWindow(window) })
	text, err := windowTextSystemOf()
	if err != nil {
		t.Fatalf("real text system: %v", err)
	}
	drawState(window).text = text
	var styled *StyledText
	view := NewEntity(app, window.Scope(), func(v *truncateView, _ *Context[truncateView]) {
		v.style = func(d *DivElement) *DivElement { return d.OverflowHidden().TextEllipsis().LineClamp(2) }
		v.child = func() AnyElement {
			styled = NewStyledText("one two three four five six seven eight nine ten eleven twelve")
			return styled.IntoElement()
		}
	})
	window.SetRootView(ViewOf(view))
	for frame := range 2 {
		if _, err := DrawWindowFrame(window); err != nil {
			t.Fatalf("frame %d: DrawWindowFrame: %v", frame, err)
		}
	}
	got := styled.Layout().Text()
	if !strings.HasSuffix(got, Ellipsis) {
		t.Errorf("Text() = %q, want an ellipsis suffix", got)
	}
	if rows := strings.Count(styled.Layout().WrappedText(), "\n") + 1; rows != 2 {
		t.Errorf("WrappedText() = %q: %d rows, want 2", styled.Layout().WrappedText(), rows)
	}
}
