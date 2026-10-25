//go:build windows

package richtextspec

// The real-stack slice of Base ticket 10: the native Parley shaping
// seam, a REAL Win32 window and its real glyph atlas. The exact advances
// depend on the machine's resolved system font, so these tests assert
// the structural guarantees the port makes — every run's glyphs are
// painted in that run's color under a content mask bounded by the run's
// selection rectangles, the decorations and backgrounds follow the run
// rectangles, and the legacy facade's x/index answers agree with the
// adapter's WrappedLine.Caret / HitTest — with the expected boundaries
// computed independently through the adapter. Environment expectation
// (the winhostspec / inlinespec pattern): this session creates real
// windows; failures fail the tests, never skip.
//
// Upstream tests ported here:
//
//   - legacy text_system/line_wrapper.rs:779-823
//     test_tracking_changes_measured_width (ported over the system font;
//     the legacy uses the bundled .ZedMono — deviated);
//   - gpui_ce_parley text_system.rs:2638-2711
//     styled_document_uses_parley_paint_runs_without_changing_geometry
//     (ported over the system font and the port's shaping seam:
//     decorated and plain runs shape to identical geometry — deviated:
//     the pin's bundled Source Serif / IBM Plex fonts and its
//     fragment-style assertions do not exist in the port, whose paint
//     styles never cross the shaping seam).

import (
	"math"
	"runtime"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

// bootRealWindow opens a shown app window on a started host with the
// given root view builder (the inlinespec boot pattern).
func bootRealWindow(t *testing.T, title string, build func(*gpui.Window, *gpui.App) gpui.View) (*gpui.Host, *gpui.Window) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Fatalf("the real rich-text slice requires the Windows host")
	}
	app := gpui.NewApp()
	host := gpui.NewHost()
	if err := app.Attach(host); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if err := host.Start(); err != nil {
		t.Fatalf("host start: %v", err)
	}
	runDone := make(chan error, 1)
	go func() { runDone <- app.Run(host) }()
	t.Cleanup(func() {
		if err := host.Stop(); err != nil {
			t.Errorf("host stop: %v", err)
		}
		<-runDone
	})
	var window *gpui.Window
	var openErr error
	onHost(t, host, func() {
		window, openErr = app.OpenWindowView(gpui.WindowOptions{
			Title:     title,
			Show:      true,
			Resizable: true,
			Bounds:    gpui.Bounds{Size: gpui.Size{Width: 480, Height: 200}},
		}, build)
	})
	if openErr != nil {
		t.Fatalf("app.OpenWindow(%q): %v — this session is expected to create real windows", title, openErr)
	}
	t.Cleanup(func() {
		if window.Alive() {
			window.Close()
			deadline := time.Now().Add(10 * time.Second)
			for window.Alive() && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			if window.Alive() {
				t.Errorf("the window was not destroyed")
			}
		}
	})
	return host, window
}

// onHost runs f on the foreground host thread and waits for it.
func onHost(t *testing.T, host *gpui.Host, f func()) {
	t.Helper()
	done := make(chan struct{})
	if !host.Post(func() {
		defer close(done)
		f()
	}) {
		t.Fatalf("foreground post was dropped: the host loop exited")
	}
	<-done
}

// realView renders a three-run StyledText above a two-run "office"
// StyledText (a color boundary inside the "ffi" cluster) and remembers
// both layouts.
type realView struct {
	runs      []gpui.TextRun
	text      string
	layout    *gpui.TextLayout
	ligRuns   []gpui.TextRun
	ligLayout *gpui.TextLayout
}

// ligText is the cross-boundary ligature corpus: the runs split it at
// byte 2, between the two f's.
const ligText = "office"

// Render implements gpui.Render[realView].
func (v *realView) Render(w *gpui.Window, cx *gpui.Context[realView]) gpui.AnyElement {
	styled := gpui.NewStyledText(v.text).WithRuns(v.runs)
	v.layout = styled.Layout()
	lig := gpui.NewStyledText(ligText).WithRuns(v.ligRuns)
	v.ligLayout = lig.Layout()
	return gpui.Div().Flex().FlexCol().Child(styled).Child(lig).IntoElement()
}

// TestRealRichText draws a multi-run StyledText in a real window and
// pins the per-run paint transcript, then checks the ShapedLine facade
// against the adapter's own geometry on the same window text system.
// (The LineWrapper subtest moved to the gpui-go-base repository with
// Base in browser-core ticket 75.)
func TestRealRichText(t *testing.T) {
	gray := gpui.Hsla{H: 0, S: 0, L: 0.5, A: 1}
	const text = "Hello brave new world"
	base := gpui.DefaultTextStyle()
	r1, r2, r3 := base.ToRun(6), base.ToRun(6), base.ToRun(9)
	r1.Color = red
	r2.Color, r2.Underline = green, &gpui.UnderlineStyle{Thickness: 1}
	r3.Color, r3.Strikethrough, r3.BackgroundColor = blue, &gpui.StrikethroughStyle{Thickness: 1}, ptr(gray)
	runs := []gpui.TextRun{r1, r2, r3}
	purple := gpui.Hsla{H: 0.8, S: 1, L: 0.5, A: 1}
	orange := gpui.Hsla{H: 0.08, S: 1, L: 0.5, A: 1}
	l1, l2 := base.ToRun(2), base.ToRun(len(ligText)-2)
	l1.Color, l2.Color = purple, orange
	// Prefer an installed face whose default features form the "ffi"
	// ligature (fewer glyphs than characters), so the boundary really
	// falls inside one glyph; the clip assertions hold either way.
	if family, ok := ligatureFamily(t); ok {
		l1.Font.Family, l2.Font.Family = family, family
	}
	ligRuns := []gpui.TextRun{l1, l2}

	var state *realView
	host, window := bootRealWindow(t, "richtextspec runs", func(w *gpui.Window, app *gpui.App) gpui.View {
		return gpui.ViewOf(gpui.NewEntity(app, w.Scope(), func(v *realView, cx *gpui.Context[realView]) {
			v.text, v.runs, v.ligRuns = text, runs, ligRuns
			state = v
		}))
	})
	scale, err := window.ScaleFactor()
	if err != nil {
		t.Fatalf("window scale: %v", err)
	}

	var scene *gpui.Scene
	var bounds, ligBounds gpui.Bounds
	var lineHeight float32
	onHost(t, host, func() {
		defer func() {
			if scene != nil && state != nil && state.layout != nil && state.ligLayout != nil {
				bounds, lineHeight = state.layout.Bounds(), state.layout.LineHeight()
				ligBounds = state.ligLayout.Bounds()
			}
		}()
		s, err := gpui.DrawWindowFrame(window)
		if err != nil {
			t.Errorf("DrawWindowFrame: %v", err)
			return
		}
		scene = s
	})
	if scene == nil {
		t.FailNow()
	}

	// The run boundaries, computed independently through the adapter
	// with the same text, font size and fonts (paint styles never change
	// shaped geometry).
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
	mid := rectOf(6, 12)
	tail := rectOf(12, 21)

	t.Run("paint transcript", func(t *testing.T) {
		if lineHeight <= 0 {
			t.Fatalf("the StyledText layout was not measured and prepainted (line height %v)", lineHeight)
		}
		b1 := (bounds.Origin.X + mid.X) * scale
		b2 := (bounds.Origin.X + mid.X + mid.W) * scale
		end := (bounds.Origin.X + tail.X + tail.W) * scale

		monos, err := scene.MonochromeSprites()
		if err != nil {
			t.Fatalf("mono sprites: %v", err)
		}
		subs, err := scene.SubpixelSprites()
		if err != nil {
			t.Fatalf("subpixel sprites: %v", err)
		}
		sprites := sceneSprites(monos, subs)
		rowBottom := (bounds.Origin.Y + lineHeight) * scale
		counts := map[string]int{}
		for i, s := range sprites {
			if s.bounds.Origin.Y+s.bounds.Size.Height/2 > rowBottom {
				continue // the ligature row below
			}
			left, right := s.mask.Origin.X, s.mask.Origin.X+s.mask.Size.Width
			switch {
			case colorNear(s.color, red):
				counts["red"]++
				if right > b1+1.5 {
					t.Errorf("red sprite %d mask right %v crosses the run boundary %v", i, right, b1)
				}
			case colorNear(s.color, green):
				counts["green"]++
				if left < b1-1.5 || right > b2+1.5 {
					t.Errorf("green sprite %d mask [%v,%v] leaves the run extent [%v,%v]", i, left, right, b1, b2)
				}
			case colorNear(s.color, blue):
				counts["blue"]++
				if left < b2-1.5 {
					t.Errorf("blue sprite %d mask left %v crosses the run boundary %v", i, left, b2)
				}
			default:
				t.Errorf("sprite %d color %+v is none of the run colors", i, s.color)
			}
		}
		if counts["red"] == 0 || counts["green"] == 0 || counts["blue"] == 0 {
			t.Fatalf("sprites per run color = %v, want every run painted (scene sprites %d)", counts, len(sprites))
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
		if math.Abs(float64(under.Bounds.Origin.X-b1)) > 1.5 || math.Abs(float64(under.Bounds.Size.Width-(b2-b1))) > 2 {
			t.Errorf("underline x/width = %v/%v, want ~%v/%v (run 2's rectangle)", under.Bounds.Origin.X, under.Bounds.Size.Width, b1, b2-b1)
		}
		if math.Abs(float64(strike.Bounds.Origin.X-b2)) > 1.5 || math.Abs(float64(strike.Bounds.Size.Width-(end-b2))) > 2 {
			t.Errorf("strikethrough x/width = %v/%v, want ~%v/%v (run 3's rectangle)", strike.Bounds.Origin.X, strike.Bounds.Size.Width, b2, end-b2)
		}
		top, bottom := bounds.Origin.Y*scale, (bounds.Origin.Y+lineHeight)*scale
		if !(strike.Bounds.Origin.Y < under.Bounds.Origin.Y) || strike.Bounds.Origin.Y < top || under.Bounds.Origin.Y > bottom {
			t.Errorf("decoration rows: strikethrough y %v, underline y %v, want strike above underline inside the row [%v,%v]",
				strike.Bounds.Origin.Y, under.Bounds.Origin.Y, top, bottom)
		}

		grays := quadsColored(t, scene, gray)
		if len(grays) != 1 {
			t.Fatalf("background quads = %d, want 1 (run 3)", len(grays))
		}
		if q := grays[0].Bounds; math.Abs(float64(q.Origin.X-b2)) > 1.5 || math.Abs(float64(q.Origin.X+q.Size.Width-end)) > 1.5 {
			t.Errorf("run 3 background = %v, want x [%v,%v]", q, b2, end)
		}
	})

	// The recorded cross-boundary deviation (ticket 02, rich_paint.go):
	// the document is shaped once, so a color boundary inside the "ffi"
	// cluster does not split the shaping; each side's glyph ink is
	// clipped at the boundary's selection-rectangle x.
	t.Run("color boundary inside a ligature cluster", func(t *testing.T) {
		if ligBounds.Size.Width <= 0 {
			t.Fatalf("the ligature row was not laid out")
		}
		lig, err := ts.ShapeText(gpui.TextLayoutRequest{Text: ligText, FontSize: 16, Runs: ligRuns})
		if err != nil {
			t.Fatalf("ShapeText: %v", err)
		}
		defer func() { _ = lig.Dispose() }()
		summary, err := lig.Summary()
		if err != nil {
			t.Fatalf("summary: %v", err)
		}
		rects, err := lig.SelectionRects(0, 2, 26)
		if err != nil || len(rects) != 1 {
			t.Fatalf("SelectionRects(0,2) = %v, %v", rects, err)
		}
		boundary := (ligBounds.Origin.X + rects[0].X + rects[0].W) * scale
		t.Logf("%q shaped to %d glyphs for %d characters (fewer glyphs = a ligature formed); boundary at device x %v",
			ligText, summary.GlyphCount, len(ligText), boundary)

		monos, err := scene.MonochromeSprites()
		if err != nil {
			t.Fatalf("mono sprites: %v", err)
		}
		subs, err := scene.SubpixelSprites()
		if err != nil {
			t.Fatalf("subpixel sprites: %v", err)
		}
		rowTop := ligBounds.Origin.Y * scale
		seen := map[string]int{}
		for i, s := range sceneSprites(monos, subs) {
			if s.bounds.Origin.Y+s.bounds.Size.Height/2 < rowTop {
				continue // the first row
			}
			left, right := s.mask.Origin.X, s.mask.Origin.X+s.mask.Size.Width
			switch {
			case colorNear(s.color, purple):
				seen["purple"]++
				if right > boundary+1.5 {
					t.Errorf("purple sprite %d mask right %v crosses the boundary %v", i, right, boundary)
				}
			case colorNear(s.color, orange):
				seen["orange"]++
				if left < boundary-1.5 {
					t.Errorf("orange sprite %d mask left %v crosses the boundary %v", i, left, boundary)
				}
			default:
				t.Errorf("ligature-row sprite %d color %+v is neither run color", i, s.color)
			}
		}
		if seen["purple"] == 0 || seen["orange"] == 0 {
			t.Fatalf("ligature-row sprites per color = %v, want both runs painted", seen)
		}
	})

	t.Run("ShapedLine agrees with the adapter", func(t *testing.T) {
		const line = "Hello, wörld office"
		lineRuns := []gpui.TextRun{base.ToRun(7), base.ToRun(len(line) - 7)}
		lineRuns[1].Color = red
		reference, err := ts.ShapeText(gpui.TextLayoutRequest{Text: line, FontSize: 16, Runs: lineRuns})
		if err != nil {
			t.Fatalf("ShapeText: %v", err)
		}
		defer func() { _ = reference.Dispose() }()
		summary, err := reference.Summary()
		if err != nil {
			t.Fatalf("summary: %v", err)
		}
		clusters, err := reference.Clusters()
		if err != nil {
			t.Fatalf("clusters: %v", err)
		}
		onHost(t, host, func() {
			shaped, err := window.TextSystem().ShapeLine(line, 16, lineRuns, nil)
			if err != nil {
				t.Errorf("ShapeLine: %v", err)
				return
			}
			if shaped.Len() != len(line) || shaped.Width() != summary.Width {
				t.Errorf("len/width = %d/%v, want %d/%v", shaped.Len(), shaped.Width(), len(line), summary.Width)
			}
			if got := shaped.XForIndex(len(line)); got != summary.Width {
				t.Errorf("XForIndex(len) = %v, want the width %v", got, summary.Width)
			}
			for _, cluster := range clusters {
				caret, err := reference.Caret(cluster.Start, gpui.CaretAffinityDownstream, 26)
				if err != nil {
					t.Errorf("Caret(%d): %v", cluster.Start, err)
					return
				}
				x := shaped.XForIndex(cluster.Start)
				if x != caret.Bounds.X {
					t.Errorf("XForIndex(%d) = %v, want the adapter caret x %v", cluster.Start, x, caret.Bounds.X)
				}
				hit, err := reference.HitTest(x, 13, 26)
				if err != nil {
					t.Errorf("HitTest: %v", err)
					return
				}
				if got := shaped.ClosestIndexForX(x); got != cluster.Start || hit.Index != cluster.Start {
					t.Errorf("at x %v: ClosestIndexForX = %d, HitTest = %d, want both %d", x, got, hit.Index, cluster.Start)
				}
				if got, ok := shaped.IndexForX(x); !ok || got != cluster.Start {
					t.Errorf("IndexForX(%v) = (%d,%v), want (%d,true)", x, got, ok, cluster.Start)
				}
			}
		})
	})

}

// ligatureFamily returns the first candidate family present in the font
// catalog that shapes "office" to fewer glyphs than characters.
func ligatureFamily(t *testing.T) (string, bool) {
	t.Helper()
	ts, err := gpui.DefaultTextSystem()
	if err != nil {
		t.Fatalf("text system: %v", err)
	}
	for _, family := range []string{"Calibri", "Cambria", "Candara", "Constantia", "Corbel"} {
		font := gpui.FontDescriptor{Family: family, Weight: gpui.DefaultFontWeight}
		identity, err := ts.ResolveFont(font)
		if err != nil || !identity.FamilyFound {
			continue
		}
		line, err := ts.ShapeText(gpui.TextLayoutRequest{Text: ligText, FontSize: 16, Runs: []gpui.TextRun{{Len: len(ligText), Font: font}}})
		if err != nil {
			continue
		}
		summary, err := line.Summary()
		_ = line.Dispose()
		if err == nil && summary.GlyphCount < len(ligText) {
			return family, true
		}
	}
	return "", false
}

// paintedSprite is one glyph sprite's color, bounds and content mask
// (device pixels).
type paintedSprite struct {
	color  gpui.Hsla
	bounds gpui.Bounds
	mask   gpui.Bounds
}

// sceneSprites flattens the monochrome and subpixel glyph sprites (the
// class follows the platform's recommended rendering mode).
func sceneSprites(monos []gpui.MonochromeSprite, subs []gpui.SubpixelSprite) []paintedSprite {
	var out []paintedSprite
	for _, s := range monos {
		out = append(out, paintedSprite{s.Color, s.Bounds, s.ContentMask})
	}
	for _, s := range subs {
		out = append(out, paintedSprite{s.Color, s.Bounds, s.ContentMask})
	}
	return out
}

// TestRealTrackingChangesMeasuredWidth is the port of legacy
// line_wrapper.rs:779-823 over the system font: positive letter spacing
// widens the shaped line, negative spacing does not widen it.
func TestRealTrackingChangesMeasuredWidth(t *testing.T) {
	ts, err := gpui.DefaultTextSystem()
	if err != nil {
		t.Fatalf("text system: %v", err)
	}
	font := gpui.DefaultTextStyle().FontDescriptorOf()
	width := func(spacing *float32) float32 {
		t.Helper()
		line, err := ts.ShapeText(gpui.TextLayoutRequest{Text: "TEST", FontSize: 16, Runs: []gpui.TextRun{{Len: 4, Font: font, LetterSpacing: spacing}}})
		if err != nil {
			t.Fatalf("ShapeText: %v", err)
		}
		defer func() { _ = line.Dispose() }()
		summary, err := line.Summary()
		if err != nil {
			t.Fatalf("summary: %v", err)
		}
		return summary.Width
	}
	noTracking, wide, tight := width(nil), width(ptr(float32(2))), width(ptr(float32(-0.5)))
	if !(wide > noTracking) || !(tight <= noTracking) {
		t.Fatalf("widths: none %v, +2 %v, -0.5 %v; want wide > none >= tight", noTracking, wide, tight)
	}
}

// TestRealDecorationsKeepShapedGeometry is the deviated port of
// gpui_ce_parley styled_document_uses_parley_paint_runs_without_changing_geometry:
// decorated runs (background, wavy underline, strikethrough) shape to
// the same width, fragments and glyphs as the plain runs.
func TestRealDecorationsKeepShapedGeometry(t *testing.T) {
	ts, err := gpui.DefaultTextSystem()
	if err != nil {
		t.Fatalf("text system: %v", err)
	}
	const text = "office café world"
	firstEnd := len("office ")
	secondEnd := firstEnd + len("café ")
	font := gpui.DefaultTextStyle().FontDescriptorOf()
	bold := font
	bold.Weight = 700
	plain := []gpui.TextRun{
		{Len: firstEnd, Font: font, LetterSpacing: ptr(float32(0.4)), Color: gpui.Hsla{H: 0, S: 0.8, L: 0.4, A: 1}},
		{Len: secondEnd - firstEnd, Font: bold, Color: gpui.Hsla{H: 0.35, S: 0.7, L: 0.35, A: 1}},
		{Len: len(text) - secondEnd, Font: font, Color: gpui.Hsla{H: 0.6, S: 0.8, L: 0.45, A: 1}},
	}
	decorated := append([]gpui.TextRun(nil), plain...)
	decorated[0].BackgroundColor = ptr(gpui.Hsla{H: 0.1, S: 0.5, L: 0.5, A: 0.3})
	decorated[0].Underline = &gpui.UnderlineStyle{Thickness: 1.5, Wavy: true}
	decorated[1].Strikethrough = &gpui.StrikethroughStyle{Thickness: 1}

	type facts struct {
		width     float32
		fragments []gpui.TextFragment
		glyphs    []gpui.TextGlyph
	}
	shape := func(runs []gpui.TextRun) facts {
		t.Helper()
		line, err := ts.ShapeText(gpui.TextLayoutRequest{Text: text, FontSize: 20, Runs: runs})
		if err != nil {
			t.Fatalf("ShapeText: %v", err)
		}
		defer func() { _ = line.Dispose() }()
		summary, err := line.Summary()
		if err != nil {
			t.Fatalf("summary: %v", err)
		}
		fragments, err := line.Fragments()
		if err != nil {
			t.Fatalf("fragments: %v", err)
		}
		glyphs, err := line.Glyphs()
		if err != nil {
			t.Fatalf("glyphs: %v", err)
		}
		return facts{summary.Width, fragments, glyphs}
	}
	a, b := shape(plain), shape(decorated)
	if a.width != b.width || len(a.fragments) != len(b.fragments) || len(a.glyphs) != len(b.glyphs) {
		t.Fatalf("decorated geometry differs: width %v vs %v, fragments %d vs %d, glyphs %d vs %d",
			a.width, b.width, len(a.fragments), len(b.fragments), len(a.glyphs), len(b.glyphs))
	}
	for i := range a.fragments {
		if a.fragments[i] != b.fragments[i] {
			t.Fatalf("fragment %d differs: %+v vs %+v", i, a.fragments[i], b.fragments[i])
		}
	}
	for i := range a.glyphs {
		if a.glyphs[i] != b.glyphs[i] {
			t.Fatalf("glyph %d differs: %+v vs %+v", i, a.glyphs[i], b.glyphs[i])
		}
	}
}
