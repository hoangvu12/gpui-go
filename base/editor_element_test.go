package base

// Ported editor tests of U:input/base/element.rs (the 9 ticket 20 left for
// the editor: the gutter, geometric decorations, decoration precedence and
// the editor scrollbar layout). The decoration harness is a 240x140
// window holding an EditorState (U:input/base/element.rs:3467-3493).

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// decorationEditor is decoration_editor (U:input/base/element.rs:3475-3493).
func decorationEditor(t *testing.T, text string, wrap bool) *inputView[EditorMode] {
	t.Helper()
	return buildEditorWith(t, gpui.Size{Width: 240, Height: 140}, func(s EditorState) EditorState {
		return s.SoftWrap(wrap).Folding(true).DefaultValue(text)
	})
}

// drawEditor is window.draw(cx).clear(cx): notify and run the frame.
func drawEditor(v *inputView[EditorMode]) {
	v.update(func(_ *EditorState, _ *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) { cx.Notify() })
	v.cx.RunUntilParked()
}

func editorScaleFactor(v *inputView[EditorMode]) float32 {
	scale, err := v.cx.Window().ScaleFactor()
	if err != nil || scale <= 0 {
		return 1
	}
	return scale
}

// U:input/base/element.rs:3495-3538
func TestEditorLineNumberGutterResizesWithDocumentLines(t *testing.T) {
	v := decorationEditor(t, strings.Repeat("x\n", 8), false)
	width := func() float32 {
		var w float32
		v.read(func(s *EditorState) { w = s.lastLayout.lineNumberWidth })
		return w
	}
	drawEditor(v)
	narrow := width()
	v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
		s.SetValue(strings.Repeat("x\n", 99), w, cx)
	})
	drawEditor(v)
	if middle := width(); middle != narrow {
		t.Fatalf("one to three digits must share a width: %v != %v", middle, narrow)
	}
	v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
		s.SetValue(strings.Repeat("x\n", 999), w, cx)
	})
	drawEditor(v)
	if wide := width(); !(wide > narrow) {
		t.Fatalf("more line-number digits must widen the gutter: %v <= %v", wide, narrow)
	}
}

// U:input/base/element.rs:3540-3592
func TestEditorGeometricDecorationsClipScrolledViewportAndCullOffscreenRanges(t *testing.T) {
	text := strings.Repeat("abcdefghij\n", 100)
	v := decorationEditor(t, text, false)
	v.update(func(s *EditorState, _ *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
		s.CreateRangeDecorationsCollection([]RangeDecoration{
			NewRangeDecoration(NewInputSelection(0, len(text))).WithStyle(RangeDecorationFill),
			NewRangeDecoration(NewInputSelection(0, len(text))),
			NewRangeDecoration(NewInputSelection(0, 3)),
			NewRangeDecoration(NewInputSelection(len(text)-3, len(text))),
		}, cx)
	})
	drawEditor(v)
	v.update(func(s *EditorState, _ *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
		s.SetScrollOffset(gpui.Point{Y: -s.lastLayout.lineHeight * 40}, cx)
	})
	// The deferred scroll is committed by prepaint and reflected next frame.
	drawEditor(v)
	drawEditor(v)
	scale := editorScaleFactor(v)
	v.read(func(s *EditorState) {
		layout := s.lastLayout
		if !(layout.visibleRangeOffset.Start > 0) || !(layout.visibleRangeOffset.End < len(text)) {
			t.Fatalf("visible range %v must be scrolled inside the text", layout.visibleRangeOffset)
		}
		all := layoutRangeCorners(0, len(text), layout)
		clipped := layoutRangeCorners(layout.visibleRangeOffset.Start, layout.visibleRangeOffset.End, layout)
		if len(all) == 0 || len(all) != len(clipped) {
			t.Fatalf("all %d corners, clipped %d", len(all), len(clipped))
		}
		for i := range all {
			if all[i] != clipped[i] {
				t.Fatalf("corner %d: %v != %v", i, all[i], clipped[i])
			}
		}
		if len(layoutRangeCorners(0, 3, layout)) != 0 {
			t.Fatal("an offscreen range has no corners")
		}
		fills, frames := layoutRangeDecorations(s, layout, s.inputBounds, scale, s.inputBounds)
		if len(fills) != 1 || len(frames) != 1 {
			t.Fatalf("fills %d frames %d, want 1 1", len(fills), len(frames))
		}
	})
}

// U:input/base/element.rs:3594-3649
func TestEditorGeometricDecorationsUseShapedWrapBoundariesAndNewlineCells(t *testing.T) {
	text := "    héllo world héllo world héllo world héllo world\n\nlast"
	v := decorationEditor(t, text, true)
	drawEditor(v)
	v.read(func(s *EditorState) {
		layout := s.lastLayout
		first := layout.lines[0]
		if len(first.wrappedLines) <= 1 || !(first.wrapIndent > 0) {
			t.Fatalf("first line must wrap with an indent: %d rows, indent %v", len(first.wrappedLines), first.wrapIndent)
		}
		boundary := first.wrappedLines[0].len
		firstRow := layoutRangeCorners(0, boundary, layout)
		if len(firstRow) != 1 {
			t.Fatalf("a wrap end must not open the next visual row: %d rows", len(firstRow))
		}
		if firstRow[0].topRight.X != first.wrappedLines[0].width {
			t.Fatalf("first row right %v, want %v", firstRow[0].topRight.X, first.wrappedLines[0].width)
		}
		rest := layoutRangeCorners(boundary, first.len(), layout)
		if len(rest) != len(first.wrappedLines)-1 {
			t.Fatalf("rest rows %d, want %d", len(rest), len(first.wrappedLines)-1)
		}
		if rest[0].topLeft.Y != layout.visibleTop+layout.lineHeight {
			t.Fatalf("rest top %v", rest[0].topLeft.Y)
		}
		for i, c := range rest {
			if c.topRight.X != first.wrapIndent+first.wrappedLines[i+1].width {
				t.Fatalf("row %d right %v", i+1, c.topRight.X)
			}
			if c.bottomLeft.Y-c.topLeft.Y != layout.lineHeight {
				t.Fatalf("row %d height %v", i+1, c.bottomLeft.Y-c.topLeft.Y)
			}
		}
		blankOffset := strings.Index(text, "\n\n") + 1
		blank := layoutRangeCorners(blankOffset, blankOffset+1, layout)
		if len(blank) != 1 || blank[0].topRight.X-blank[0].topLeft.X != layout.spaceWidth {
			t.Fatalf("a blank line's newline cell is one space wide: %v", blank)
		}
		lastOffset := strings.LastIndex(text, "last")
		last := layoutRangeCorners(lastOffset, len(text), layout)
		if len(last) != 1 || !(last[0].topLeft.Y > rest[len(rest)-1].topLeft.Y) {
			t.Fatalf("a later-line decoration cannot paint the first row: %v", last)
		}
	})
}

// U:input/base/element.rs:3651-3694
func TestEditorGeometricDecorationsIncludeCrlfBoundaries(t *testing.T) {
	text := "one\r\ntwo\r\nthree"
	v := decorationEditor(t, text, false)
	v.update(func(s *EditorState, _ *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
		var ds []RangeDecoration
		for _, r := range [][2]int{{0, 8}, {3, 5}, {4, 5}, {5, 8}, {9, 10}} {
			ds = append(ds, NewRangeDecoration(NewInputSelection(r[0], r[1])))
		}
		s.CreateRangeDecorationsCollection(ds, cx)
	})
	drawEditor(v)
	scale := editorScaleFactor(v)
	v.read(func(s *EditorState) {
		layout := s.lastLayout
		// Shaping keeps '\r'; only '\n' needs an extra newline cell.
		if layout.lines[0].len() != len("one\r") {
			t.Fatalf("line 0 len %d", layout.lines[0].len())
		}
		if got := layout.visibleLineByteOffsets; len(got) != 3 || got[0] != 0 || got[1] != 5 || got[2] != 10 {
			t.Fatalf("line offsets %v, want [0 5 10]", got)
		}
		if n := len(layoutRangeCorners(0, 8, layout)); n != 2 {
			t.Fatalf("spanning rows %d, want 2", n)
		}
		for _, r := range [][2]int{{3, 5}, {4, 5}, {9, 10}} {
			c := layoutRangeCorners(r[0], r[1], layout)
			if len(c) != 1 || !(c[0].topRight.X > c[0].topLeft.X) {
				t.Fatalf("range %v corners %v", r, c)
			}
		}
		fills, frames := layoutRangeDecorations(s, layout, s.inputBounds, scale, s.inputBounds)
		if len(fills) != 0 || len(frames) != 5 {
			t.Fatalf("fills %d frames %d, want 0 5", len(fills), len(frames))
		}
	})
}

// U:input/base/element.rs:3696-3746
func TestEditorGeometricDecorationsProjectFoldsWithoutChangingTrackedRanges(t *testing.T) {
	text := "top\nhidden one\nhidden two\nend\nlast"
	v := decorationEditor(t, text, false)
	hidden := NewInputSelection(4, strings.Index(text, "end"))
	var collection RangeDecorationCollection
	v.update(func(s *EditorState, _ *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
		s.ApplyHighlighterFoldCandidates([]FoldRange{NewFoldRange(0, 3)}, cx)
		s.displayMap.setFolded(0, true)
		collection = s.CreateRangeDecorationsCollection([]RangeDecoration{NewRangeDecoration(hidden)}, cx)
	})
	drawEditor(v)
	scale := editorScaleFactor(v)
	v.read(func(s *EditorState) {
		layout := s.lastLayout
		if got := layout.visibleBufferLines; len(got) != 3 || got[0] != 0 || got[1] != 3 || got[2] != 4 {
			t.Fatalf("visible lines %v, want [0 3 4]", got)
		}
		if len(layoutRangeCorners(hidden.Start, hidden.End, layout)) != 0 {
			t.Fatal("a folded range has no corners")
		}
		if _, frames := layoutRangeDecorations(s, layout, s.inputBounds, scale, s.inputBounds); len(frames) != 0 {
			t.Fatalf("frames %d, want 0", len(frames))
		}
		spanning := layoutRangeCorners(0, len(text), layout)
		if len(spanning) != 3 || spanning[1].topLeft.Y != spanning[0].bottomLeft.Y {
			t.Fatalf("spanning %v", spanning)
		}
	})
	app := v.cx.App()
	if got := collection.GetRanges(app); len(got) != 1 || got[0] != hidden {
		t.Fatalf("tracked ranges %v, want [%v]", got, hidden)
	}
	v.update(func(s *EditorState, _ *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
		s.displayMap.setFolded(0, false)
		cx.Notify()
	})
	drawEditor(v)
	v.read(func(s *EditorState) {
		if n := len(layoutRangeCorners(hidden.Start, hidden.End, s.lastLayout)); n != 2 {
			t.Fatalf("revealed rows %d, want 2", n)
		}
	})
	if got := collection.GetRanges(app); len(got) != 1 || got[0] != hidden {
		t.Fatalf("tracked ranges %v, want [%v]", got, hidden)
	}
}

// U:input/base/element.rs:3748-3789
func TestEditorGeometricDecorationsTrackEditsHistoryReplacementAndOwnerLifetime(t *testing.T) {
	v := decorationEditor(t, "abc def", false)
	want := func(c RangeDecorationCollection, ranges ...InputSelection) {
		t.Helper()
		got := c.GetRanges(v.cx.App())
		if len(got) != len(ranges) {
			t.Fatalf("ranges %v, want %v", got, ranges)
		}
		for i := range got {
			if got[i] != ranges[i] {
				t.Fatalf("ranges %v, want %v", got, ranges)
			}
		}
	}
	r := NewInputSelection
	var first, second RangeDecorationCollection
	v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
		first = s.CreateRangeDecorationsCollection([]RangeDecoration{NewRangeDecoration(r(4, 7))}, cx)
		second = s.CreateRangeDecorationsCollection([]RangeDecoration{NewRangeDecoration(r(0, 3))}, cx)
		s.SetSelectedRange(r(0, 0), cx)
		s.ReplaceTextInRange(nil, "\n", w, cx)
	})
	want(first, r(5, 8))
	v.update(editorUndo)
	want(first, r(4, 7))
	v.update(editorRedo)
	want(first, r(5, 8))
	first.Clear(v.cx.App())
	want(second, r(1, 4))
	first.Append([]RangeDecoration{NewRangeDecoration(r(5, 8))}, v.cx.App())
	v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
		s.ReplaceAll("formatted", w, cx)
	})
	want(first, r(0, 9))
	v.update(editorUndo)
	// Annotations are transformed, not snapshotted in undo history.
	want(first, r(0, 8))
	v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
		s.SetValue("new", w, cx)
	})
	want(first, r(0, 3))
	clone := first
	first.Dispose(v.cx.App())
	clone.Append([]RangeDecoration{NewRangeDecoration(r(0, 1))}, v.cx.App())
	want(clone)
	want(second, r(0, 3))
	v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
		s.SetValue("", w, cx)
	})
	want(second)
	drawEditor(v)
}

// U:input/base/element.rs:3920-3942
func TestEditorFirstDecorationCollectionHasPrecedence(t *testing.T) {
	red, blue := gpui.Red(), gpui.Hsla{H: 0.6, S: 1, L: 0.5, A: 1}
	first := []TextDecoration{NewTextDecoration(NewInputSelection(0, 4), gpui.HighlightStyle{BackgroundColor: &red})}
	second := []TextDecoration{NewTextDecoration(NewInputSelection(0, 4), gpui.HighlightStyle{BackgroundColor: &blue})}
	styles := composeDecorationCollections(nil, [][]TextDecoration{first, second}, gpui.TextRange{Start: 0, End: 4})
	if len(styles) != 1 || styles[0].Style.BackgroundColor == nil || *styles[0].Style.BackgroundColor != red {
		t.Fatalf("styles = %+v, want one red background", styles)
	}
}

// U:input/base/element.rs:3966-3993
func TestEditorScrollbarLayoutUsesCurrentScrollSize(t *testing.T) {
	inputBounds := gpui.Bounds{Origin: gpui.Point{X: 10, Y: 20}, Size: gpui.Size{Width: 300, Height: 80}}
	paddings := gpui.Edges{Top: 2, Right: 3, Bottom: 5, Left: 7}
	layout := newEditorScrollbarLayout(inputBounds, 40, gpui.Size{Width: 1000, Height: 200}, paddings)
	if want := (gpui.Bounds{Origin: gpui.Point{X: 51, Y: 18}, Size: gpui.Size{Width: 262, Height: 87}}); layout.bounds != want {
		t.Fatalf("bounds %v, want %v", layout.bounds, want)
	}
	if want := (gpui.Size{Width: 972, Height: 200}); layout.scrollSize != want {
		t.Fatalf("scroll size %v, want %v", layout.scrollSize, want)
	}
	layout = newEditorScrollbarLayout(inputBounds, 0, gpui.Size{Width: 500, Height: 120}, paddings)
	if want := (gpui.Bounds{Origin: gpui.Point{X: 10, Y: 18}, Size: gpui.Size{Width: 303, Height: 87}}); layout.bounds != want {
		t.Fatalf("bounds without gutter %v, want %v", layout.bounds, want)
	}
	if want := (gpui.Size{Width: 513, Height: 120}); layout.scrollSize != want {
		t.Fatalf("scroll size without gutter %v, want %v", layout.scrollSize, want)
	}
}

// U:input/base/element.rs:3995-4013
func TestEditorGutterCoversTheCompleteFixedColumn(t *testing.T) {
	inputBounds := gpui.Bounds{Origin: gpui.Point{X: 10, Y: 20}, Size: gpui.Size{Width: 300, Height: 80}}
	got := editorGutterBounds(inputBounds, 48, 16, gpui.Edges{Top: 2, Right: 3, Bottom: 5, Left: 7})
	if want := (gpui.Bounds{Origin: gpui.Point{X: 3, Y: 18}, Size: gpui.Size{Width: 55, Height: 103}}); got != want {
		t.Fatalf("gutter %v, want %v", got, want)
	}
}
