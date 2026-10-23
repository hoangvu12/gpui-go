// Package richtextspec holds Base ticket 10's rich-text tests: the
// pinned highlight semantics, the legacy LineWrapper and ShapedLine
// facade, StyledText/TextLayout and the per-run paint transcripts.
//
// This file is the deterministic slice: test windows drawn through the
// real element pipeline with the deterministic test text system (the
// pinned TestTextSystem port: em width = font size * 0.6 per UTF-16
// unit, ascent 1.025 / descent -0.275 em, empty glyph rasters), at the
// test window's fixed profile (scale 2.0, rem 16). The real text stack
// slice (glyph sprites per run, facade/adapter geometry agreement and
// the shaping facts) lives in richtext_real_test.go.
//
// Upstream sources (R: = gpui-go/reference/ce-source/crates/gpui/src at
// 254b5dbd; legacy = ~/.cargo/git/checkouts/gpui-ce-6758b1db058c6497/
// beb5a6d/crates/gpui/src):
//
//   - R:style.rs:1633-1715 test_basic_highlight_style_combination and
//     R:style.rs:1718-1804 test_combine_highlights (ported);
//   - legacy text_system/line_wrapper.rs:836-982 test_wrap_line (ported;
//     the legacy .ZedMono advance is 600/1000 em, exactly the test text
//     system's em width, so the boundaries are the legacy values);
//   - R:elements/text.rs:1849-1899
//     centered_text_keeps_its_device_pixel_offset_when_its_parent_moves
//     (ported at the test window's fixed scale 2.0; the pin also runs
//     1.0 and 1.5 through TestWindow::set_scale_factor, which the port's
//     test window does not expose: deviated);
//   - R:elements/text.rs:1694-1755
//     text_measurement_replaces_state_when_wrap_width_changes (deviated:
//     the pin inspects TextLayout's private state; the port asserts the
//     public Len/Bounds/LineHeight across the same width sequence).
package richtextspec

import (
	"math"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// The pinned named colors (R:color.rs:93-110).
var (
	red    = gpui.Hsla{H: 0, S: 1, L: 0.5, A: 1}
	blue   = gpui.Hsla{H: 240.0 / 360.0, S: 1, L: 0.5, A: 1}
	green  = gpui.Hsla{H: 120.0 / 360.0, S: 1, L: 0.25, A: 1}
	yellow = gpui.Hsla{H: 60.0 / 360.0, S: 1, L: 0.5, A: 1}
)

func ptr[T any](v T) *T { return &v }

// near compares two floats within a tolerance.
func near(a, b, tolerance float32) bool {
	return float32(math.Abs(float64(a-b))) <= tolerance
}

// colorNear compares two colors through their RGB form (hue
// normalization differs between paths; the visible color is what
// matters).
func colorNear(a, b gpui.Hsla) bool {
	ar, ag, ab := hslToRGB(a)
	br, bg, bb := hslToRGB(b)
	return near(ar, br, 2e-3) && near(ag, bg, 2e-3) && near(ab, bb, 2e-3) && near(a.A, b.A, 2e-3)
}

// hslToRGB is the standard HSL to RGB conversion (test-side oracle).
func hslToRGB(c gpui.Hsla) (float32, float32, float32) {
	h := float64(c.H) - math.Floor(float64(c.H))
	s, l := float64(c.S), float64(c.L)
	chroma := (1 - math.Abs(2*l-1)) * s
	hp := h * 6
	x := chroma * (1 - math.Abs(math.Mod(hp, 2)-1))
	var r, g, b float64
	switch {
	case hp < 1:
		r, g, b = chroma, x, 0
	case hp < 2:
		r, g, b = x, chroma, 0
	case hp < 3:
		r, g, b = 0, chroma, x
	case hp < 4:
		r, g, b = 0, x, chroma
	case hp < 5:
		r, g, b = x, 0, chroma
	default:
		r, g, b = chroma, 0, x
	}
	m := l - chroma/2
	return float32(r + m), float32(g + m), float32(b + m)
}

// ---------------------------------------------------------------------------
// HighlightStyle and combine_highlights (R:style.rs)
// ---------------------------------------------------------------------------

// TestBasicHighlightStyleCombination is the port of
// R:style.rs:1633-1715.
func TestBasicHighlightStyleCombination(t *testing.T) {
	styleA := gpui.HighlightStyle{}.Highlight(gpui.HighlightStyle{})
	if !styleA.Equal(gpui.HighlightStyle{}) {
		t.Fatalf("combining empty styles produced a non-empty style: %+v", styleA)
	}

	styleB := gpui.HighlightStyle{
		Color:           ptr(red),
		Strikethrough:   &gpui.StrikethroughStyle{Thickness: 2, Color: ptr(blue)},
		FadeOut:         ptr(float32(0)),
		FontStyle:       ptr(gpui.FontStyleItalic),
		FontWeight:      ptr(float32(300)),
		BackgroundColor: ptr(yellow),
		Underline:       &gpui.UnderlineStyle{Thickness: 2, Color: ptr(red), Wavy: true},
	}
	expected := styleB

	if got := styleA.Highlight(styleB); !got.Equal(expected) {
		t.Fatalf("blending an empty style with another style should return the other style: %+v", got)
	}
	if got := styleB.Highlight(gpui.HighlightStyle{}); !got.Equal(expected) {
		t.Fatalf("blending a style with an empty style should not change the style: %+v", got)
	}

	styleD := gpui.HighlightStyle{
		Color:           ptr(blue.Alpha(0.7)),
		Strikethrough:   &gpui.StrikethroughStyle{Thickness: 4, Color: ptr(red)},
		FadeOut:         ptr(float32(0)),
		FontStyle:       ptr(gpui.FontStyleOblique),
		FontWeight:      ptr(float32(800)),
		BackgroundColor: ptr(green),
		Underline:       &gpui.UnderlineStyle{Thickness: 4},
	}
	want := gpui.HighlightStyle{
		Color:           ptr(red.Blend(blue.Alpha(0.7))),
		Strikethrough:   &gpui.StrikethroughStyle{Thickness: 4, Color: ptr(red)},
		FadeOut:         ptr(float32(0)),
		FontStyle:       ptr(gpui.FontStyleOblique),
		FontWeight:      ptr(float32(800)),
		BackgroundColor: ptr(green),
		Underline:       &gpui.UnderlineStyle{Thickness: 4},
	}
	got := expected.Highlight(styleD)
	if !got.Equal(want) {
		t.Fatalf("blending styles should blend properties where possible and override all others:\n got %+v\nwant %+v", got, want)
	}
	// The palette blend of opaque red under 70% blue (equations.rs: the
	// source-alpha parameters) is blue at alpha 0.7.
	if !colorNear(*got.Color, blue.Alpha(0.7)) {
		t.Fatalf("red.Blend(blue@0.7) = %+v, want blue at alpha 0.7", *got.Color)
	}
}

// TestCombineHighlights is the port of R:style.rs:1718-1804.
func TestCombineHighlights(t *testing.T) {
	r := func(start, end int) gpui.TextRange { return gpui.TextRange{Start: start, End: end} }
	got := gpui.CombineHighlights(
		[]gpui.HighlightRange{
			{Range: r(0, 5), Style: gpui.HighlightStyleFromColor(green)},
			{Range: r(4, 10), Style: gpui.HighlightStyleFromFontWeight(700)},
			{Range: r(15, 20), Style: gpui.HighlightStyleFromColor(yellow)},
		},
		[]gpui.HighlightRange{
			{Range: r(2, 6), Style: gpui.HighlightStyleFromFontStyle(gpui.FontStyleItalic)},
			{Range: r(1, 3), Style: gpui.HighlightStyleFromColor(blue)},
			{Range: r(21, 23), Style: gpui.HighlightStyleFromColor(red)},
		},
	)
	want := []gpui.HighlightRange{
		{Range: r(0, 1), Style: gpui.HighlightStyle{Color: ptr(green)}},
		{Range: r(1, 2), Style: gpui.HighlightStyle{Color: ptr(blue)}},
		{Range: r(2, 3), Style: gpui.HighlightStyle{Color: ptr(blue), FontStyle: ptr(gpui.FontStyleItalic)}},
		{Range: r(3, 4), Style: gpui.HighlightStyle{Color: ptr(green), FontStyle: ptr(gpui.FontStyleItalic)}},
		{Range: r(4, 5), Style: gpui.HighlightStyle{Color: ptr(green), FontWeight: ptr(float32(700)), FontStyle: ptr(gpui.FontStyleItalic)}},
		{Range: r(5, 6), Style: gpui.HighlightStyle{FontWeight: ptr(float32(700)), FontStyle: ptr(gpui.FontStyleItalic)}},
		{Range: r(6, 10), Style: gpui.HighlightStyle{FontWeight: ptr(float32(700))}},
		{Range: r(15, 20), Style: gpui.HighlightStyle{Color: ptr(yellow)}},
		{Range: r(21, 23), Style: gpui.HighlightStyle{Color: ptr(red)}},
	}
	if len(got) != len(want) {
		t.Fatalf("combined %d ranges, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].Range != want[i].Range {
			t.Errorf("range %d = %v, want %v", i, got[i].Range, want[i].Range)
		}
		// Opaque colors blend to the later color exactly in the pin; the
		// port's f32 round trip through Rgba is compared visibly.
		g, w := got[i].Style, want[i].Style
		if (g.Color == nil) != (w.Color == nil) || (g.Color != nil && !colorNear(*g.Color, *w.Color)) {
			t.Errorf("range %d color = %v, want %v", i, g.Color, w.Color)
		}
		g.Color, w.Color = nil, nil
		if !g.Equal(w) {
			t.Errorf("range %d style = %+v, want %+v", i, g, w)
		}
	}
}

// TestTextStyleHighlightAndToRun pins TextStyle::highlight and to_run
// (R:style.rs:675-705, 724-740): the highlight's paint fields land on
// the run, the color blends, the fade-out multiplies the alpha.
func TestTextStyleHighlightAndToRun(t *testing.T) {
	base := gpui.DefaultTextStyle()
	style := base.Highlight(gpui.HighlightStyle{
		Color:           ptr(red),
		FadeOut:         ptr(float32(0.5)),
		FontWeight:      ptr(float32(700)),
		BackgroundColor: ptr(yellow),
		Underline:       &gpui.UnderlineStyle{Thickness: 1},
	})
	run := style.ToRun(4)
	if run.Len != 4 || run.Font.Weight != 700 || run.Font.Family != base.FontFamily {
		t.Fatalf("run = %+v, want len 4, weight 700, the default family", run)
	}
	if !colorNear(run.Color, red.Alpha(0.5)) {
		t.Fatalf("run color = %+v, want red faded to alpha 0.5", run.Color)
	}
	if run.BackgroundColor == nil || *run.BackgroundColor != yellow || run.Underline == nil || run.Strikethrough != nil {
		t.Fatalf("run paint fields = bg %v underline %v strike %v", run.BackgroundColor, run.Underline, run.Strikethrough)
	}
}

// ---------------------------------------------------------------------------
// The legacy LineWrapper (legacy text_system/line_wrapper.rs)
// ---------------------------------------------------------------------------

// newTestWindow opens a deterministic test window with an empty root.
func newTestWindow(t *testing.T) *gpui.Window {
	t.Helper()
	ta := gpui.NewTestApp()
	window := gpui.NewTestWindow(ta.App(), gpui.Size{Width: 400, Height: 200})
	t.Cleanup(func() { gpui.CloseTestWindow(window) })
	return window
}

// TestWrapLine is the port of legacy line_wrapper.rs:836-982
// test_wrap_line.
func TestWrapLine(t *testing.T) {
	window := newTestWindow(t)
	wrapper, err := window.TextSystem().LineWrapper(gpui.FontDescriptor{Family: ".ZedMono", Weight: 400}, 16)
	if err != nil {
		t.Fatalf("LineWrapper: %v", err)
	}
	text := gpui.LineFragmentText
	element := gpui.LineFragmentElement
	b := func(ix int, indent uint32) gpui.Boundary { return gpui.Boundary{Ix: ix, NextIndent: indent} }
	cases := []struct {
		name      string
		fragments []gpui.LineFragment
		want      []gpui.Boundary
	}{
		{"words", []gpui.LineFragment{text("aa bbb cccc ddddd eeee")}, []gpui.Boundary{b(7, 0), b(12, 0), b(18, 0)}},
		{"long word", []gpui.LineFragment{text("aaa aaaaaaaaaaaaaaaaaa")}, []gpui.Boundary{b(4, 0), b(11, 0), b(18, 0)}},
		{"indented", []gpui.LineFragment{text("     aaaaaaa")}, []gpui.Boundary{b(7, 5), b(9, 5), b(11, 5)}},
		{"spaces", []gpui.LineFragment{text("                            ")}, []gpui.Boundary{b(7, 0), b(14, 0), b(21, 0)}},
		{"deep indent", []gpui.LineFragment{text("          aaaaaaaaaaaaaa")}, []gpui.Boundary{b(7, 0), b(14, 3), b(18, 3), b(22, 3)}},
		{"multiple text fragments", []gpui.LineFragment{text("aa bbb "), text("cccc ddddd eeee")}, []gpui.Boundary{b(7, 0), b(12, 0), b(18, 0)}},
		{"text and elements", []gpui.LineFragment{text("aa "), element(20, 1), text(" bbb "), element(30, 1), text(" cccc")}, []gpui.Boundary{b(5, 0), b(9, 0), b(11, 0)}},
		{"leading element", []gpui.LineFragment{element(50, 1), text(" aaaa bbbb cccc dddd")}, []gpui.Boundary{b(2, 0), b(7, 0), b(12, 0), b(17, 0)}},
		{"large element", []gpui.LineFragment{text("short text "), element(100, 1), text(" more text")}, []gpui.Boundary{b(6, 0), b(11, 0), b(12, 0), b(18, 0)}},
		{"glue characters", []gpui.LineFragment{text("a b c‑d e")}, []gpui.Boundary{b(12, 0)}},
	}
	for _, c := range cases {
		got := wrapper.WrapLine(c.fragments, 72)
		if len(got) != len(c.want) {
			t.Errorf("%s: boundaries = %v, want %v", c.name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: boundaries = %v, want %v", c.name, got, c.want)
				break
			}
		}
	}
}

// TestLineWrapperLetterSpacing pins the wrapper's tracking arithmetic
// (legacy line_wrapper.rs:96-104, 135-143): letter spacing widens every
// character after the first, so the same text wraps earlier.
func TestLineWrapperLetterSpacing(t *testing.T) {
	window := newTestWindow(t)
	wrapper, err := window.TextSystem().LineWrapper(gpui.FontDescriptor{Family: ".ZedMono", Weight: 400}, 16)
	if err != nil {
		t.Fatalf("LineWrapper: %v", err)
	}
	fragments := []gpui.LineFragment{gpui.LineFragmentText("aaaaaaaaaaaa")}
	// 9.6 per char: 7 chars fit 72 (67.2), the 8th overflows.
	if got := wrapper.WrapLine(fragments, 72); len(got) == 0 || got[0].Ix != 7 {
		t.Fatalf("untracked boundaries = %v, want the first at 7", got)
	}
	// +2 per following char: 9.6 + 6*11.6 = 79.2 > 72, so 6 chars fit.
	wrapper.SetLetterSpacing(ptr(float32(2)))
	if got := wrapper.WrapLine(fragments, 72); len(got) == 0 || got[0].Ix != 6 {
		t.Fatalf("tracked boundaries = %v, want the first at 6", got)
	}
	wrapper.SetLetterSpacing(nil)
	if got := window.TextSystem().LayoutWidth(gpui.FontDescriptor{Family: ".ZedMono"}, 16, 'm'); !near(got, 9.6, 1e-4) {
		t.Fatalf("LayoutWidth('m') = %v, want the em width 9.6", got)
	}
}

// ---------------------------------------------------------------------------
// The ShapedLine facade on the deterministic text system
// ---------------------------------------------------------------------------

// TestShapedLineGeometryTestSystem pins the legacy x/index semantics
// (legacy line_layout.rs:56-114) over the deterministic glyph table:
// one stop per character at 9.6px.
func TestShapedLineGeometryTestSystem(t *testing.T) {
	window := newTestWindow(t)
	style := gpui.DefaultTextStyle()
	line, err := window.TextSystem().ShapeLine("abc", 16, []gpui.TextRun{style.ToRun(3)}, nil)
	if err != nil {
		t.Fatalf("ShapeLine: %v", err)
	}
	if line.Len() != 3 || !near(line.Width(), 28.8, 1e-4) {
		t.Fatalf("len/width = %d/%v, want 3/28.8", line.Len(), line.Width())
	}
	for i, want := range []float32{0, 9.6, 19.2, 28.8, 28.8} {
		if got := line.XForIndex(i); !near(got, want, 1e-4) {
			t.Errorf("XForIndex(%d) = %v, want %v", i, got, want)
		}
	}
	for _, c := range []struct {
		x    float32
		want int
	}{{0, 0}, {4, 0}, {6, 1}, {9.6, 1}, {15, 2}, {19.2, 2}, {25, 3}, {100, 3}} {
		if got := line.ClosestIndexForX(c.x); got != c.want {
			t.Errorf("ClosestIndexForX(%v) = %d, want %d", c.x, got, c.want)
		}
	}
	for _, c := range []struct {
		x    float32
		want int
		ok   bool
	}{{-1, 0, true}, {0, 0, true}, {9.5, 0, true}, {9.6, 1, true}, {28, 2, true}, {line.Width(), 0, false}} {
		got, ok := line.IndexForX(c.x)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("IndexForX(%v) = (%d, %v), want (%d, %v)", c.x, got, ok, c.want, c.ok)
		}
	}
	// A one-byte line splits at half its width (legacy line_layout.rs:93-99).
	one, err := window.TextSystem().ShapeLine("a", 16, []gpui.TextRun{style.ToRun(1)}, nil)
	if err != nil {
		t.Fatalf("ShapeLine(a): %v", err)
	}
	if one.ClosestIndexForX(5) != 1 || one.ClosestIndexForX(4) != 0 {
		t.Errorf("one-byte line: ClosestIndexForX(5/4) = %d/%d, want 1/0", one.ClosestIndexForX(5), one.ClosestIndexForX(4))
	}
	if got := line.WithLen(1).Len(); got != 1 {
		t.Errorf("WithLen(1).Len() = %d, want 1", got)
	}
	// The legacy contract: no newlines; the port rejects the force width
	// it cannot honor.
	if _, err := window.TextSystem().ShapeLine("a\nb", 16, []gpui.TextRun{style.ToRun(3)}, nil); err == nil {
		t.Errorf("ShapeLine with a newline succeeded, want an error")
	}
	if _, err := window.TextSystem().ShapeLine("ab", 16, []gpui.TextRun{style.ToRun(2)}, ptr(float32(10))); err == nil {
		t.Errorf("ShapeLine with a force width succeeded, want the recorded rejection")
	}
	if _, err := window.TextSystem().ShapeLine("ab", 16, []gpui.TextRun{style.ToRun(1)}, nil); err == nil {
		t.Errorf("ShapeLine with runs not covering the text succeeded, want an error")
	}
}

// ---------------------------------------------------------------------------
// StyledText paint transcripts (deterministic decorations)
// ---------------------------------------------------------------------------

// styledView renders whatever its build function returns.
type styledView struct {
	build func(v *styledView) gpui.AnyElement
	// layout is the last rendered StyledText's layout handle.
	layout *gpui.TextLayout
	extent float32
}

// Render implements gpui.Render[styledView].
func (v *styledView) Render(w *gpui.Window, cx *gpui.Context[styledView]) gpui.AnyElement {
	return v.build(v)
}

// openStyled opens a test window rendering build and draws one frame.
func openStyled(t *testing.T, build func(v *styledView) gpui.AnyElement) (*gpui.App, *gpui.Window, gpui.Entity[styledView], *gpui.Scene) {
	t.Helper()
	ta := gpui.NewTestApp()
	app := ta.App()
	window := gpui.NewTestWindow(app, gpui.Size{Width: 600, Height: 400})
	t.Cleanup(func() { gpui.CloseTestWindow(window) })
	view := gpui.NewEntity(app, window.Scope(), func(v *styledView, cx *gpui.Context[styledView]) { v.build = build })
	window.SetRootView(gpui.ViewOf(view))
	scene, err := gpui.DrawWindowFrame(window)
	if err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}
	return app, window, view, scene
}

// redraw notifies the view and draws a frame.
func redraw(t *testing.T, app *gpui.App, window *gpui.Window, view gpui.Entity[styledView], update func(v *styledView)) *gpui.Scene {
	t.Helper()
	view.Update(app, func(v *styledView, cx *gpui.Context[styledView]) {
		update(v)
		cx.Notify()
	})
	scene, err := gpui.DrawWindowFrame(window)
	if err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}
	return scene
}

// quadsColored returns the scene quads with a (visibly) matching solid
// background.
func quadsColored(t *testing.T, scene *gpui.Scene, color gpui.Hsla) []gpui.Quad {
	t.Helper()
	quads, err := scene.Quads()
	if err != nil {
		t.Fatalf("scene quads: %v", err)
	}
	var out []gpui.Quad
	for _, q := range quads {
		if colorNear(q.Background.Solid, color) {
			out = append(out, q)
		}
	}
	return out
}

// TestStyledTextRunDecorationsTranscript paints a three-run StyledText
// on the deterministic test window and pins the per-run background,
// underline and strikethrough records (R:text_system/line.rs:421-464 per
// fragment; the port per run rectangle). Geometry at scale 2, 16px:
// em 9.6, line height pixel_snap(16 * phi) = 26, ascent 16.4, descent
// -4.4, baseline 23.4; underline at baseline + descent * 0.618 and the
// derived strikethrough at baseline - ascent * 0.5 (rich_paint.go).
func TestStyledTextRunDecorationsTranscript(t *testing.T) {
	gray := gpui.Hsla{H: 0, S: 0, L: 0.5, A: 1}
	_, _, _, scene := openStyled(t, func(v *styledView) gpui.AnyElement {
		base := gpui.DefaultTextStyle()
		r1, r2, r3 := base.ToRun(3), base.ToRun(3), base.ToRun(3)
		r1.Color, r1.BackgroundColor = red, ptr(yellow)
		r2.Color, r2.Underline = green, &gpui.UnderlineStyle{Thickness: 1}
		r3.Color, r3.Strikethrough, r3.BackgroundColor = blue, &gpui.StrikethroughStyle{Thickness: 1}, ptr(gray)
		styled := gpui.NewStyledText("aaabbbccc").WithRuns([]gpui.TextRun{r1, r2, r3})
		v.layout = styled.Layout()
		return gpui.Div().Flex().Child(styled).IntoElement()
	})
	const scale = 2

	// Backgrounds: one quad per background run, over the run's x extent
	// and the full line height.
	yellowQuads := quadsColored(t, scene, yellow)
	grayQuads := quadsColored(t, scene, gray)
	if len(yellowQuads) != 1 || len(grayQuads) != 1 {
		t.Fatalf("background quads yellow=%d gray=%d, want 1 each", len(yellowQuads), len(grayQuads))
	}
	checkBounds := func(what string, got gpui.Bounds, x, y, w, h float32) {
		t.Helper()
		if !near(got.Origin.X, x*scale, 1) || !near(got.Origin.Y, y*scale, 1) || !near(got.Size.Width, w*scale, 1) || !near(got.Size.Height, h*scale, 1) {
			t.Errorf("%s bounds = %v, want ~(%v,%v %vx%v) logical at scale 2", what, got, x, y, w, h)
		}
	}
	checkBounds("run 1 background", yellowQuads[0].Bounds, 0, 0, 28.8, 26)
	checkBounds("run 3 background", grayQuads[0].Bounds, 57.6, 0, 28.8, 26)

	// Decorations: the run-2 underline and the run-3 strikethrough, each
	// over its run's extent, colored by its run.
	underlines, err := scene.Underlines()
	if err != nil {
		t.Fatalf("scene underlines: %v", err)
	}
	if len(underlines) != 2 {
		t.Fatalf("underline records = %d, want 2 (one underline, one strikethrough): %+v", len(underlines), underlines)
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
	if !near(under.Bounds.Origin.X, 28.8*scale, 1) || !near(under.Bounds.Size.Width, 28.8*scale, 1) ||
		!near(under.Bounds.Origin.Y, (baseline+(-4.4)*0.618)*scale, 1) {
		t.Errorf("underline bounds = %v, want x 28.8, width 28.8, y baseline + descent*0.618 (logical, scale 2)", under.Bounds)
	}
	if !near(strike.Bounds.Origin.X, 57.6*scale, 1) || !near(strike.Bounds.Size.Width, 28.8*scale, 1) ||
		!near(strike.Bounds.Origin.Y, (baseline-16.4*0.5)*scale, 1) || strike.Wavy {
		t.Errorf("strikethrough bounds = %v (wavy %v), want x 57.6, width 28.8, y baseline - ascent*0.5, not wavy", strike.Bounds, strike.Wavy)
	}
}

// TestStyledTextLayoutGeometry pins TextLayout's index/position mapping
// (R:elements/text.rs:1062-1161) over the deterministic layout: the
// element at the window origin, 9.6px stops, the pinned test layout's
// byte_index_from_pixel_point (platform.rs:1282-1302).
func TestStyledTextLayoutGeometry(t *testing.T) {
	app, _, view, _ := openStyled(t, func(v *styledView) gpui.AnyElement {
		styled := gpui.NewStyledText("hello").WithHighlights([]gpui.HighlightRange{
			{Range: gpui.TextRange{Start: 1, End: 3}, Style: gpui.HighlightStyleFromColor(red)},
		})
		v.layout = styled.Layout()
		return gpui.Div().Flex().Child(styled).IntoElement()
	})
	layout := view.Read(app, func(v *styledView, _ *gpui.App) *gpui.TextLayout { return v.layout })
	if layout.Len() != 5 || layout.Text() != "hello" || layout.WrappedText() != "hello" {
		t.Fatalf("len/text/wrapped = %d/%q/%q", layout.Len(), layout.Text(), layout.WrappedText())
	}
	if got := layout.LineHeight(); got != 26 {
		t.Fatalf("line height = %v, want 26 (pixel-snapped 16 * phi at scale 2)", got)
	}
	bounds := layout.Bounds()
	if !near(bounds.Size.Width, 48, 1e-3) || !near(bounds.Size.Height, 26, 1e-3) {
		t.Fatalf("bounds = %v, want 48x26", bounds)
	}
	for i, want := range []float32{0, 9.6, 19.2, 28.8, 38.4, 48} {
		p, ok := layout.PositionForIndex(i)
		if !ok || !near(p.X, bounds.Origin.X+want, 1e-3) || p.Y != bounds.Origin.Y {
			t.Errorf("PositionForIndex(%d) = (%v, %v), want x %v", i, p, ok, want)
		}
	}
	if _, ok := layout.PositionForIndex(6); ok {
		t.Errorf("PositionForIndex past the end answered a position")
	}
	o := bounds.Origin
	for _, c := range []struct {
		x, y float32
		want int
		ok   bool
	}{{0, 5, 0, true}, {10, 5, 1, true}, {47, 5, 4, true}, {60, 5, 5, false}, {10, -1, 0, false}} {
		got, ok := layout.IndexForPosition(gpui.Point{X: o.X + c.x, Y: o.Y + c.y})
		if got != c.want || ok != c.ok {
			t.Errorf("IndexForPosition(%v,%v) = (%d,%v), want (%d,%v)", c.x, c.y, got, ok, c.want, c.ok)
		}
	}
}

// TestCenteredStyledTextKeepsItsDevicePixelOffset is the port of
// R:elements/text.rs:1849-1899 at the test window's scale 2.0: a
// highlighted StyledText centered in a moving container keeps its
// device-pixel offset from the container.
func TestCenteredStyledTextKeepsItsDevicePixelOffset(t *testing.T) {
	containerColor := gpui.Hsla{H: 0.72, S: 0.45, L: 0.32, A: 1}
	textBackground := gpui.Hsla{H: 0.37, S: 0.65, L: 0.42, A: 1}
	app, window, view, scene := openStyled(t, func(v *styledView) gpui.AnyElement {
		return gpui.Div().
			Flex().ItemsCenter().JustifyCenter().
			W(gpui.PxLength(320 + v.extent)).H(gpui.PxLength(160 + v.extent)).
			Child(gpui.Div().
				Flex().ItemsCenter().JustifyCenter().
				W(gpui.PxLength(118)).H(gpui.PxLength(31)).
				Bg(containerColor).
				TextSize(gpui.Px(14)).
				Child(gpui.NewStyledText("x").WithHighlights([]gpui.HighlightRange{
					{Range: gpui.TextRange{Start: 0, End: 1}, Style: gpui.HighlightStyle{BackgroundColor: ptr(textBackground)}},
				}))).
			IntoElement()
	})
	only := func(scene *gpui.Scene, color gpui.Hsla, what string) gpui.Bounds {
		t.Helper()
		quads := quadsColored(t, scene, color)
		if len(quads) != 1 {
			t.Fatalf("%s: %d quads, want exactly one", what, len(quads))
		}
		return quads[0].Bounds
	}
	container := only(scene, containerColor, "container")
	background := only(scene, textBackground, "text background")
	offsetX := background.Origin.X - container.Origin.X
	offsetY := background.Origin.Y - container.Origin.Y
	origins := map[[2]int]bool{{int(container.Origin.X), int(container.Origin.Y)}: true}
	for step := 1; step <= 32; step++ {
		scene := redraw(t, app, window, view, func(v *styledView) { v.extent = float32(step) })
		container := only(scene, containerColor, "container")
		background := only(scene, textBackground, "text background")
		if background.Origin.X-container.Origin.X != offsetX || background.Origin.Y-container.Origin.Y != offsetY {
			t.Fatalf("text moved within its parent at step %d: offset (%v,%v), want (%v,%v)", step,
				background.Origin.X-container.Origin.X, background.Origin.Y-container.Origin.Y, offsetX, offsetY)
		}
		origins[[2]int{int(container.Origin.X), int(container.Origin.Y)}] = true
	}
	if len(origins) <= 8 {
		t.Fatalf("fixture crossed %d device pixel origins, want more than 8", len(origins))
	}
}

// TestStyledTextMeasurementFollowsWrapWidth is the deviated port of
// R:elements/text.rs:1694-1755: the same text laid out at 240, 240 and
// 80px reports its full length, the pinned line height and bounds
// within the wrap width each time.
func TestStyledTextMeasurementFollowsWrapWidth(t *testing.T) {
	const text = "Measure café over multiple words"
	app, window, view, _ := openStyled(t, func(v *styledView) gpui.AnyElement {
		styled := gpui.NewStyledText(text)
		v.layout = styled.Layout()
		width := v.extent
		if width == 0 {
			width = 240
		}
		return gpui.Div().Flex().FlexCol().W(gpui.PxLength(width)).Child(styled).IntoElement()
	})
	for _, width := range []float32{240, 240, 80} {
		redraw(t, app, window, view, func(v *styledView) { v.extent = width })
		layout := view.Read(app, func(v *styledView, _ *gpui.App) *gpui.TextLayout { return v.layout })
		if layout.Len() != len(text) || layout.Text() != text {
			t.Fatalf("width %v: len/text = %d/%q, want %d/%q", width, layout.Len(), layout.Text(), len(text), text)
		}
		if layout.LineHeight() != 26 {
			t.Fatalf("width %v: line height = %v, want 26", width, layout.LineHeight())
		}
		if got := layout.Bounds().Size.Width; !near(got, width, 1e-3) {
			t.Fatalf("width %v: bounds width %v, want the stretched wrap width", width, got)
		}
	}
}
