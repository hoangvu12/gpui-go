//go:build windows

package inlinespec

import (
	"runtime"
	"testing"
	"time"

	"gpui-go/gpui"
)

// This file is the ticket15 real-stack slice: inline paragraphs through
// the REAL text system (the native Parley shaping seam driving the Go
// greedy row/box algorithm in gpui/inline_real.go) drawn into a REAL
// Win32 window on the foreground host.
//
// The deterministic corpus lives in inline_test.go through the pinned
// TestTextSystem port; the real stack's exact advances are
// machine-dependent (the resolved system font), so this slice asserts
// the structural invariants the pinned algorithm guarantees (row
// assembly, box placement, wrap clamping, vertical growth, fragment
// snapping at the window's real scale, hit geometry) plus
// redraw-stability of the measurement cache. Environment expectation
// (the winhostspec/authorspec pattern): this session creates real
// windows — failures fail the tests, never skip.

// bootInlineRealWindow opens a shown app window on a started host (the
// rendererspec boot pattern) with the given root view builder.
func bootInlineRealWindow(t *testing.T, title string, w, h float32, build func(*gpui.Window, *gpui.App) gpui.View) (*gpui.App, *gpui.Host, *gpui.Window) {
	t.Helper()
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
	sync := func(f func()) {
		done := make(chan struct{})
		if !host.Post(func() {
			defer close(done)
			f()
		}) {
			t.Fatalf("foreground post was dropped: the host loop exited")
		}
		<-done
	}
	sync(func() {
		window, openErr = app.OpenWindowView(gpui.WindowOptions{
			Title:     title,
			Show:      true,
			Resizable: true,
			Bounds:    gpui.Bounds{Size: gpui.Size{Width: w, Height: h}},
		}, build)
	})
	if openErr != nil {
		t.Fatalf("app.OpenWindow(%q): %v — this session is expected to create real windows", title, openErr)
	}
	t.Cleanup(func() {
		if window.Alive() {
			window.Close()
			waitReal(t, 10*time.Second, "the window to be destroyed", func() bool { return !window.Alive() })
		}
	})
	return app, host, window
}

// waitReal polls a condition until the timeout (fail, never skip).
func waitReal(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out after %v waiting for %s", timeout, what)
}

// realWrapView is the wrapping corpus: a 120px block with a long text
// (wrapping to several rows), a middle-aligned 40px box (growing its
// row beyond the line height) and trailing text.
type realWrapView struct {
	width float32
}

// Render implements gpui.Render[realWrapView].
func (v *realWrapView) Render(w *gpui.Window, cx *gpui.Context[realWrapView]) gpui.AnyElement {
	return gpui.Div().
		Block().
		W(gpui.PxLength(v.width)).
		DebugSelector("wrap-root").
		Child("alpha bravo charlie delta echo foxtrot golf hotel india juliet ").
		Child(gpui.Div().InlineFlex().Size(gpui.PxLength(18), gpui.PxLength(40)).AlignMiddle().ID("grow-box").DebugSelector("grow-box").Bg(gpui.Hsla{H: 0.1, S: 0.5, L: 0.5, A: 1})).
		Child("kilo lima mike november oscar papa quebec romeo sierra tango").
		IntoElement()
}

// drawInlineRealFrame redraws the window on the foreground thread and
// returns the frame's inline facts.
func drawInlineRealFrame(t *testing.T, host *gpui.Host, window *gpui.Window) []gpui.InlineParagraphFacts {
	t.Helper()
	var facts []gpui.InlineParagraphFacts
	done := make(chan struct{})
	if !host.Post(func() {
		defer close(done)
		if _, err := gpui.DrawWindowFrame(window); err != nil {
			t.Errorf("DrawWindowFrame: %v", err)
			return
		}
		facts = gpui.WindowInlineFacts(window)
	}) {
		t.Fatalf("foreground post was dropped: the host loop exited")
	}
	<-done
	return facts
}

// TestRealInlineWrapsRowsAndPlacesBoxes exercises the real greedy
// row/box algorithm: the long document wraps to several rows within the
// wrap width, the middle-aligned tall box grows its row beyond the line
// height, every span fragment is placed on the device grid at the
// window's real scale, and the hit observable resolves points inside
// the first fragment.
func TestRealInlineWrapsRowsAndPlacesBoxes(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Fatalf("the real inline slice requires the Windows host")
	}
	app, host, window := bootInlineRealWindow(t, "inlinespec real wrap", 240, 400, func(w *gpui.Window, app *gpui.App) gpui.View {
		return gpui.ViewOf(gpui.NewEntity(app, w.Scope(), func(v *realWrapView, cx *gpui.Context[realWrapView]) {
			v.width = 120
		}))
	})
	_ = app

	facts := drawInlineRealFrame(t, host, window)
	if len(facts) != 1 {
		t.Fatalf("inline paragraphs = %d, want 1 (the whole corpus is one paragraph)", len(facts))
	}
	p := facts[0]

	// The wrap invariant: several rows, none wider than the wrap width
	// (the first-word overflow deviation only applies to a single-item
	// row, which this multi-word corpus never produces).
	if len(p.Rows) < 3 {
		t.Fatalf("rows = %d, want >= 3 (the document wraps at 120px)", len(p.Rows))
	}
	// The pinned line width semantics (line_layout.rs / the parley
	// backend): a row's width is the line's full advance, which KEEPS
	// the trailing break space — a wrapped row can exceed the wrap
	// width by up to one space advance (the pin itself tolerates
	// font_size-level slack on line extents, line.rs:1718). Bound the
	// excess at half the font size.
	for i, row := range p.Rows {
		if row.Size.Width > 120+8 {
			t.Fatalf("row %d width = %v, want <= 128 (the wrap constraint plus one trailing space)", i, row.Size.Width)
		}
	}
	if p.WrapWidth == nil || *p.WrapWidth != 120 {
		t.Fatalf("paragraph wrap width = %v, want 120", p.WrapWidth)
	}
	// The paragraph height is the sum of the row heights.
	var height float32
	for _, row := range p.Rows {
		height += row.Size.Height
	}
	if !f32eq(p.Size.Height, height) {
		t.Fatalf("paragraph height = %v, want %v (the rows stack)", p.Size.Height, height)
	}

	// The tall middle-aligned box grows its row beyond the line height
	// (26 at the default text style).
	if len(p.Boxes) != 1 {
		t.Fatalf("boxes = %d, want 1", len(p.Boxes))
	}
	box := p.Boxes[0]
	if box.LineIndex < 0 || box.LineIndex >= len(p.Rows) {
		t.Fatalf("box line index = %d, out of the paragraph rows", box.LineIndex)
	}
	if row := p.Rows[box.LineIndex]; row.Size.Height <= 26 {
		t.Fatalf("box row height = %v, want > 26 (the 40px middle box grows the row)", row.Size.Height)
	}
	if !f32eq(box.Bounds.Size.Width, 18) || !f32eq(box.Bounds.Size.Height, 40) {
		t.Fatalf("box size = %+v, want 18x40 (the standalone atomic measurement)", box.Bounds.Size)
	}

	// Every span fragment is pixel-snapped at the window's real scale:
	// its corners sit on the device grid.
	scale, err := window.ScaleFactor()
	if err != nil {
		t.Fatalf("window scale: %v", err)
	}
	fragments := 0
	for _, span := range p.Spans {
		for _, fragment := range span.Fragments {
			fragments++
			for _, corner := range []float32{
				fragment.Origin.X, fragment.Origin.Y,
				fragment.Origin.X + fragment.Size.Width,
				fragment.Origin.Y + fragment.Size.Height,
			} {
				device := corner * scale
				if diff := device - float32(int(device)); diff > 1e-3 || diff < -1e-3 {
					t.Fatalf("fragment corner %v (device %v) is off the device grid (scale %v)", corner, device, scale)
				}
			}
		}
	}
	if fragments == 0 {
		t.Fatal("the paragraph placed no span fragments")
	}

	// The hit observable resolves a point inside the first span's first
	// fragment to that span's text.
	first := p.Spans[0].Fragments[0]
	hitX := p.Origin.X + first.Origin.X + first.Size.Width/2
	hitY := p.Origin.Y + first.Origin.Y + first.Size.Height/2
	if span, ok := gpui.WindowInlineSpanAt(window, hitX, hitY); !ok || span == "" {
		t.Fatalf("hit (%v,%v) resolved to span %q ok=%v, want the first span's text", hitX, hitY, span, ok)
	}
	// A point well below the paragraph resolves to nothing.
	below := p.Origin.Y + p.Size.Height + 50
	if span, ok := gpui.WindowInlineSpanAt(window, p.Origin.X+10, below); ok {
		t.Fatalf("hit below the paragraph resolved to span %q, want none", span)
	}
}

// realBoundaryView is the wrap-boundary corpus: a 60px block whose box
// is wider than the remaining row, so the box crosses to a fresh row.
type realBoundaryView struct{}

// Render implements gpui.Render[realBoundaryView].
func (v *realBoundaryView) Render(w *gpui.Window, cx *gpui.Context[realBoundaryView]) gpui.AnyElement {
	return gpui.Div().
		Block().
		W(gpui.PxLength(60)).
		Child("word ").
		Child(gpui.Div().InlineFlex().Size(gpui.PxLength(70), gpui.PxLength(12)).AlignBaseline().ID("wide-box").Bg(gpui.Hsla{H: 0.8, S: 0.5, L: 0.5, A: 1})).
		Child(" tail").
		IntoElement()
}

// TestRealInlineBoxCrossesWrapBoundary checks the box row break: a box
// that cannot fit the row's remaining budget breaks the row and lands
// on a fresh row (boxes never split across rows).
func TestRealInlineBoxCrossesWrapBoundary(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Fatalf("the real inline slice requires the Windows host")
	}
	_, host, window := bootInlineRealWindow(t, "inlinespec real boundary", 200, 200, func(w *gpui.Window, app *gpui.App) gpui.View {
		return gpui.ViewOf(gpui.NewEntity(app, w.Scope(), func(v *realBoundaryView, cx *gpui.Context[realBoundaryView]) {}))
	})

	facts := drawInlineRealFrame(t, host, window)
	if len(facts) != 1 {
		t.Fatalf("inline paragraphs = %d, want 1", len(facts))
	}
	p := facts[0]
	if len(p.Boxes) != 1 {
		t.Fatalf("boxes = %d, want 1", len(p.Boxes))
	}
	box := p.Boxes[0]
	// The leading text ("word ") fills row 0 partially; the 70px box
	// cannot fit 60px, so it breaks to a fresh row (index 1).
	if box.LineIndex != 1 {
		t.Fatalf("box line index = %d, want 1 (the wide box breaks the row)", box.LineIndex)
	}
	if box.Bounds.Origin.X != 0 {
		t.Fatalf("box x = %v, want 0 (the box starts its fresh row)", box.Bounds.Origin.X)
	}
}

// TestRealInlineMeasurementHistory exercises the paragraph measurement
// cache: redrawing the same corpus under the same wrap width reproduces
// the same layout bit-for-bit, and narrowing the wrap width re-measures
// (the rows re-wrap under the new budget).
func TestRealInlineMeasurementHistory(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Fatalf("the real inline slice requires the Windows host")
	}
	var view gpui.Entity[realWrapView]
	app, host, window := bootInlineRealWindow(t, "inlinespec real history", 240, 400, func(w *gpui.Window, app *gpui.App) gpui.View {
		view = gpui.NewEntity(app, w.Scope(), func(v *realWrapView, cx *gpui.Context[realWrapView]) {
			v.width = 120
		})
		return gpui.ViewOf(view)
	})
	_ = app

	first := drawInlineRealFrame(t, host, window)
	if len(first) != 1 {
		t.Fatalf("inline paragraphs = %d, want 1", len(first))
	}
	// The cached measurement: the second draw at the same width answers
	// identically.
	second := drawInlineRealFrame(t, host, window)
	if len(second) != 1 {
		t.Fatalf("inline paragraphs = %d, want 1", len(second))
	}
	a, b := first[0], second[0]
	if a.Text != b.Text || len(a.Rows) != len(b.Rows) || a.Size != b.Size || a.AlignmentOffset != b.AlignmentOffset {
		t.Fatalf("redraw at the same wrap width changed the layout: %+v vs %+v", a, b)
	}
	for i := range a.Rows {
		if a.Rows[i] != b.Rows[i] {
			t.Fatalf("redraw row %d changed: %+v vs %+v", i, a.Rows[i], b.Rows[i])
		}
	}
	for i := range a.Boxes {
		if a.Boxes[i] != b.Boxes[i] {
			t.Fatalf("redraw box %d changed: %+v vs %+v", i, a.Boxes[i], b.Boxes[i])
		}
	}

	// Narrow the wrap width: the measurement re-runs under the new
	// budget and the document re-wraps into more rows. 80 stays above
	// the corpus's longest word (~68px at the default 16px font), so
	// every row respects the new wrap constraint (the greedy breaker's
	// unbreakable-word overflow never triggers).
	view.Update(app, func(v *realWrapView, cx *gpui.Context[realWrapView]) {
		v.width = 80
	})
	narrowed := drawInlineRealFrame(t, host, window)
	if len(narrowed) != 1 {
		t.Fatalf("inline paragraphs = %d, want 1", len(narrowed))
	}
	n := narrowed[0]
	if len(n.Rows) <= len(a.Rows) {
		t.Fatalf("rows after narrowing = %d, want > %d (the document re-wraps under the 80px budget)", len(n.Rows), len(a.Rows))
	}
	for i, row := range n.Rows {
		if row.Size.Width > 80+8 {
			t.Fatalf("row %d width = %v, want <= 88 (the new wrap constraint plus one trailing space)", i, row.Size.Width)
		}
	}
	if n.WrapWidth == nil || *n.WrapWidth != 80 {
		t.Fatalf("paragraph wrap width = %v, want 80 after the width change", n.WrapWidth)
	}
}
