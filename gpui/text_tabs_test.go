package gpui

import (
	"math"
	"slices"
	"testing"
)

// Base ticket 44: a hard tab paints nothing and advances to the next
// incremental tab stop, as DirectWrite drew it for Base's 0.2.2 line.
// These shape through the real text service; no window is shown.

func tabTestShape(t *testing.T, system *TextSystem, text, family string, size float32, wrap *float32) *WrappedLine {
	t.Helper()
	line, err := system.ShapeText(TextLayoutRequest{
		Text: text, FontSize: size, WrapWidth: wrap,
		Runs: []TextRun{{Len: len(text), Font: FontDescriptor{Family: family, Weight: 400}}},
	})
	if err != nil {
		t.Fatalf("ShapeText(%q): %v", text, err)
	}
	t.Cleanup(func() { _ = line.Dispose() })
	return line
}

func tabTestCaret(t *testing.T, line *WrappedLine, index int) TextRect {
	t.Helper()
	caret, err := line.Caret(index, CaretAffinityDownstream, 1)
	if err != nil || !caret.Present {
		t.Fatalf("Caret(%d): %+v, %v", index, caret, err)
	}
	return caret.Bounds
}

func approx(a, b float32) bool { return math.Abs(float64(a-b)) < 0.02 }

// The widths IDWriteTextLayout gives the same strings with the 0.2.2
// line's text format (no SetIncrementalTabStop; gpui_windows
// direct_write.rs:536-555), measured through dwrite.dll on the dev
// machine (ticket 44's probe): the tab reaches 4 × the font size.
func TestShapeTextHardTabMatchesDirectWriteTabStops(t *testing.T) {
	system, err := DefaultTextSystem()
	if err != nil {
		t.Fatalf("DefaultTextSystem: %v", err)
	}
	cases := []struct {
		family string
		size   float32
		text   string
		width  float32
	}{
		{"Consolas", 14, "a\tb", 63.697},
		{"Consolas", 14, "\t\tb", 119.697},
		{"Consolas", 14, "abcdefghij\tb", 119.697},
		{"Consolas", 14, "ab\tc\td", 119.697},
		{"Consolas", 16, "a\tb", 72.797},
		{"Segoe UI", 14, "a\tb", 64.230},
		{"Segoe UI", 14, "\t\tb", 120.230},
		{"Segoe UI", 14, "abcdefghij\tb", 120.230},
		{"Segoe UI", 14, "ab\tc\td", 120.244},
		{"Segoe UI", 16, "a\tb", 73.406},
	}
	for _, c := range cases {
		line := tabTestShape(t, system, c.text, c.family, c.size, nil)
		summary, err := line.Summary()
		if err != nil {
			t.Fatal(err)
		}
		if !approx(summary.Width, c.width) {
			t.Errorf("%s %vpx %q: width %v, DirectWrite %v", c.family, c.size, c.text, summary.Width, c.width)
		}
		// Every tab ends on a stop.
		stop := DefaultTabStop(c.size)
		for i := 0; i < len(c.text); i++ {
			if c.text[i] != '\t' {
				continue
			}
			end := tabTestCaret(t, line, i+1).X
			if k := end / stop; !approx(k, float32(math.Round(float64(k)))) {
				t.Errorf("%s %q: tab %d ends at %v, not on a %v stop", c.family, c.text, i, end, stop)
			}
		}
	}
}

// The tab paints a blank glyph (the space's), never glyph 0, the
// missing-glyph box the pin's Parley shaping emits for '\t'
// (gpui_ce_parley text_system.rs:1273-1285).
func TestShapeTextHardTabPaintsNoMissingGlyph(t *testing.T) {
	system, err := DefaultTextSystem()
	if err != nil {
		t.Fatalf("DefaultTextSystem: %v", err)
	}
	for _, family := range []string{"Consolas", "Segoe UI"} {
		space, err := tabTestShape(t, system, " ", family, 14, nil).Glyphs()
		if err != nil || len(space) != 1 {
			t.Fatalf("space glyphs: %v, %v", space, err)
		}
		glyphs, err := tabTestShape(t, system, "a\tb", family, 14, nil).Glyphs()
		if err != nil || len(glyphs) != 3 {
			t.Fatalf("glyphs: %v, %v", glyphs, err)
		}
		if glyphs[1].ID == 0 || glyphs[1].ID != space[0].ID {
			t.Errorf("%s: tab glyph %d, want the space glyph %d (not the missing glyph 0)", family, glyphs[1].ID, space[0].ID)
		}
	}
}

// The tab stays one cluster, so a caret steps over it at once.
func TestShapeTextHardTabIsOneCluster(t *testing.T) {
	system, err := DefaultTextSystem()
	if err != nil {
		t.Fatalf("DefaultTextSystem: %v", err)
	}
	clusters, err := tabTestShape(t, system, "a\tb", "Consolas", 14, nil).Clusters()
	if err != nil {
		t.Fatal(err)
	}
	want := []TextRange{{0, 1}, {1, 2}, {2, 3}}
	if !slices.Equal(clusters, want) {
		t.Errorf("clusters %v, want %v", clusters, want)
	}
}

// Stops count from each visual row's start, as DirectWrite's did from
// its line's; a relayout re-fits them to the new rows.
func TestShapeTextHardTabStopsCountFromEachWrappedRow(t *testing.T) {
	system, err := DefaultTextSystem()
	if err != nil {
		t.Fatalf("DefaultTextSystem: %v", err)
	}
	const text = "aaaaaaaaaa bb\tc"
	const c = 14 // 'c'
	wrap := float32(90)
	line := tabTestShape(t, system, text, "Consolas", 14, &wrap)
	if n, _ := line.LineCount(); n != 2 {
		t.Fatalf("wrapped: %d rows, want 2", n)
	}
	if got := tabTestCaret(t, line, c); !approx(got.X, 56) || got.Y != 1 {
		t.Errorf("wrapped: 'c' at %+v, want x 56 on row 1", got)
	}

	// Unwrapped, the tab follows "aaaaaaaaaa bb" (13 × 7.70 = 100.06) to
	// 112.
	if err := line.Relayout(nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := tabTestCaret(t, line, c); !approx(got.X, 112) || got.Y != 0 {
		t.Errorf("relayout unwrapped: 'c' at %+v, want x 112 on row 0", got)
	}
	if err := line.Relayout(&wrap, nil); err != nil {
		t.Fatal(err)
	}
	if got := tabTestCaret(t, line, c); !approx(got.X, 56) || got.Y != 1 {
		t.Errorf("relayout wrapped: 'c' at %+v, want x 56 on row 1", got)
	}
}

// The legacy single-line facade on the window's real text seam: the tab
// is one wide cluster (a hit inside it snaps to its nearer edge, as on
// DirectWrite's tab cluster), and the legacy wrapper measures a lone tab
// as one full stop (0.2.2's layout_width of "\t").
func TestShapedLineHardTabGeometry(t *testing.T) {
	ta := NewTestApp()
	window := NewTestWindow(ta.App(), Size{Width: 400, Height: 200})
	t.Cleanup(func() { CloseTestWindow(window) })
	if err := UseRealTextSystem(window); err != nil {
		t.Fatalf("UseRealTextSystem: %v", err)
	}
	font := FontDescriptor{Family: "Consolas", Weight: 400}
	line, err := window.TextSystem().ShapeLine("a\tb", 14, []TextRun{{Len: 3, Font: font}}, nil)
	if err != nil {
		t.Fatalf("ShapeLine: %v", err)
	}
	if x := line.XForIndex(2); !approx(x, 56) {
		t.Errorf("XForIndex(2) = %v, want 56", x)
	}
	// The tab spans 7.70..56: its middle is 31.85.
	for _, c := range []struct {
		x    float32
		want int
	}{{10, 1}, {31, 1}, {33, 2}, {55, 2}} {
		if got := line.ClosestIndexForX(c.x); got != c.want {
			t.Errorf("ClosestIndexForX(%v) = %d, want %d", c.x, got, c.want)
		}
	}
	if ix, ok := line.IndexForX(40); !ok || ix != 1 {
		t.Errorf("IndexForX(40) = %d, %v, want the tab (1)", ix, ok)
	}
	if got := window.TextSystem().LayoutWidth(font, 14, '\t'); !approx(got, 56) {
		t.Errorf("LayoutWidth('\\t') = %v, want one stop (56)", got)
	}
}
