//go:build windows

package base

// Real-window slice of ticket 30: the five U:text/inline.rs tests ticket
// 28 deferred (line_bounds_tests::row_walk_matches_the_character_walk and
// the range_highlight_tests module, U:text/inline.rs:1403-1709). Upstream
// lays a StyledText out under its WideMonoTextSystem fake, where every
// glyph is 8px wide and text wraps; gpui gives tests no text-system seam
// and its TestApp text system never wraps, so these run on the machine's
// fonts in a real window (the ticket-28 inRealPrepaint pattern), with
// Consolas as the monospace family and widths measured from it rather
// than fixed at 8px. Environment expectation: this session creates real
// windows; failures fail, never skip.

import (
	"fmt"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

const realMonoFamily = "Consolas"

// realLaidOut lays text out at wrapWidth in the mono family at 16px and
// prepaints it at origin, returning its layout and the shaped runs.
func realLaidOut(w *gpui.Window, app *gpui.App, text string, wrapWidth float32, origin gpui.Point) (*gpui.TextLayout, []gpui.TextRun, float32) {
	style := bodyStyle(16)
	style.FontFamily = realMonoFamily
	// Divergence note (browser-core 38): TextView renders verbatim
	// (inlineText pins white-space: pre-wrap, base/text_inline.go), so
	// the layout under test here must pin it too — the core's normal
	// would collapse the newlines these walk tests exercise.
	style.WhiteSpace = gpui.WhiteSpacePreWrap
	runs := textRuns(len(text), style, nil)
	if len(runs) == 0 {
		// gpui's StyledText rejects a text without runs; the empty text
		// gets one empty run.
		runs = []gpui.TextRun{style.ToRun(0)}
	}
	styled := gpui.NewStyledText(text).WithRuns(runs)
	layout := styled.Layout()
	refinement := textStyleSubtractDefault(style)
	gpui.WithTextStyleVoid(w, &refinement, func(w *gpui.Window) {
		styled.IntoElement().PrepaintAsRoot(origin, gpui.AvailableSize{
			Width:  gpui.DefiniteAvailableSpace(wrapWidth),
			Height: gpui.MinContentAvailableSpace(),
		}, w, app)
	})
	return layout, runs, style.FontSizePixels(gpui.RemSize(w))
}

// realGlyphWidth is the advance of one mono glyph at 16px.
func realGlyphWidth(w *gpui.Window) float32 {
	style := bodyStyle(16)
	style.FontFamily = realMonoFamily
	width, _, _ := shapedLineMetrics("aaaaaaaaaa", 16, []gpui.TextRun{style.ToRun(10)}, w)
	return width / 10
}

// byCharacter is the character walk the row walk replaced, kept as the
// oracle (U:text/inline.rs:1412-1457).
func byCharacter(text string, layout *gpui.TextLayout, lineHeight float32, mask gpui.Bounds) []gpui.Bounds {
	var lines []gpui.Bounds
	var current *gpui.Bounds
	var currentY float32
	offset := 0
	for _, c := range text {
		next := offset + len(string(c))
		pos, ok := layout.PositionForIndex(offset)
		if !ok {
			offset = next
			continue
		}
		charWidth := lineHeight / 2
		if nextPos, ok := layout.PositionForIndex(next); ok && nextPos.Y == pos.Y {
			charWidth = nextPos.X - pos.X
		}
		b := textBoundsIntersect(textBoundsFromCorners(pos, gpui.Point{X: pos.X + charWidth, Y: pos.Y + lineHeight}), mask)
		if b.Size.Width > 0 && b.Size.Height > 0 {
			if current != nil && currentY == pos.Y {
				u := textBoundsFromCorners(
					gpui.Point{X: min(current.Left(), b.Left()), Y: min(current.Top(), b.Top())},
					gpui.Point{X: max(current.Right(), b.Right()), Y: max(current.Bottom(), b.Bottom())})
				current = &u
			} else {
				if current != nil {
					lines = append(lines, *current)
				}
				currentY = pos.Y
				current = &b
			}
		}
		offset = next
	}
	if current != nil {
		lines = append(lines, *current)
	}
	return lines
}

var realWalkTexts = []string{
	"one row",
	"a long paragraph that wraps onto several rows of eight pixel glyphs",
	"first line\nsecond line that also wraps around\n\nfourth",
	"中文与 English 混排的一段文字也会换行",
	"trailing newline\n",
}

// U:text/inline.rs:1459-1540
func TestRealRowWalkMatchesTheCharacterWalk(t *testing.T) {
	inRealPrepaint(t, func(w *gpui.Window, app *gpui.App) {
		origin := gpui.Point{X: 7, Y: 11}
		mask := gpui.Bounds{Size: gpui.Size{Width: 1000, Height: 1000}}
		clipped := gpui.Bounds{Origin: gpui.Point{X: 20, Y: 30}, Size: gpui.Size{Width: 50, Height: 60}}
		for _, text := range append([]string{""}, realWalkTexts...) {
			for _, wrap := range []float32{40, 100, 1000} {
				layout, _, _ := realLaidOut(w, app, text, wrap, origin)
				lineHeight := layout.LineHeight()
				for _, m := range []gpui.Bounds{mask, clipped} {
					expected := byCharacter(text, layout, lineHeight, m)
					actual := inlineTextLineBounds(text, layout, lineHeight, m)
					context := fmt.Sprintf("%q at %vpx in %v", text, wrap, m)
					// The row walk covers every row from its start, so it may
					// see more rows than the character walk, never fewer.
					if len(actual) < len(expected) {
						t.Errorf("rows of %s: %d < %d", context, len(actual), len(expected))
						continue
					}
					for row, e := range expected {
						var a *gpui.Bounds
						for i := range actual {
							if actual[i].Top() == e.Top() {
								a = &actual[i]
								break
							}
						}
						if a == nil {
							t.Errorf("row %d of %s is missing", row, context)
							continue
						}
						if a.Bottom() != e.Bottom() || a.Right() != e.Right() || a.Left() > e.Left() {
							t.Errorf("row %d of %s: %v, walk %v", row, context, *a, e)
						}
					}
					for _, a := range actual {
						if a.Left() != max(origin.X, m.Left()) {
							t.Errorf("left of %s: %v", context, a.Left())
						}
					}
				}
			}
		}
	})
}

// U:text/inline.rs:1593-1647 (text_rows_extent is inlineRowsExtent here).
func TestRealTextRowsExtentMatchesTheCharacterWalk(t *testing.T) {
	inRealPrepaint(t, func(w *gpui.Window, app *gpui.App) {
		origin := gpui.Point{X: 7, Y: 11.3}
		for _, text := range realWalkTexts {
			for _, wrap := range []float32{40, 100, 1000} {
				layout, _, _ := realLaidOut(w, app, text, wrap, origin)
				// Taller than the layout's rows, as a window line height may be.
				lineHeight := layout.LineHeight() + 3
				top, bottom := float32(0), float32(0)
				found := false
				for offset := range text {
					pos, ok := layout.PositionForIndex(offset)
					if !ok {
						continue
					}
					if !found || pos.Y < top {
						top = pos.Y
					}
					if !found || pos.Y > bottom {
						bottom = pos.Y
					}
					found = true
				}
				bottom += lineHeight
				rowsTop, rowsBottom := inlineRowsExtent(text, layout, lineHeight)
				if rowsTop != top || rowsBottom < bottom {
					t.Errorf("%q at %vpx: extent %v..%v, walk %v..%v", text, wrap, rowsTop, rowsBottom, top, bottom)
				}
			}
		}
	})
}

// realBoxes lays text out at wrapWidth and returns the highlight boxes of
// rng with rows aligned by align: left and right past the text's origin,
// and the row (U:text/inline.rs:1552-1591).
func realBoxes(w *gpui.Window, app *gpui.App, text string, wrapWidth float32, align gpui.TextAlign, rng gpui.TextRange) ([]rowBox, float32) {
	layout, runs, fontSize := realLaidOut(w, app, text, wrapWidth, gpui.Point{X: 7, Y: 11})
	glyphs := glyphBoxes(text, runs, fontSize, layout, align, layout.Bounds().Size.Width, w)
	return rangeBoxes(glyphs, rng), layout.Bounds().Size.Width
}

// realWrap is a wrap width that holds five mono glyphs but not six, the
// role 45px plays for the fake's 8px glyphs.
func realWrap(w *gpui.Window) float32 { return realGlyphWidth(w) * 5.5 }

// U:text/inline.rs:1649-1667
func TestRealAHighlightStartingAWrappedRowPaintsOnlyThatRow(t *testing.T) {
	inRealPrepaint(t, func(w *gpui.Window, app *gpui.App) {
		// Three rows of "aaaa ", "bbbb ", "cccc".
		const text = "aaaa bbbb cccc"
		wrap := realWrap(w)
		rows, _ := realBoxes(w, app, text, wrap, gpui.TextAlignLeft, mdRange(0, len(text)))
		if len(rows) != 3 {
			t.Errorf("rows %+v", rows)
			return
		}
		highlight, _ := realBoxes(w, app, text, wrap, gpui.TextAlignLeft, mdRange(5, 9))
		if len(highlight) != 1 || highlight[0].left != 0 || highlight[0].row != 1 {
			t.Errorf("highlight %+v", highlight)
		}
		// Across the wrap, each row only as far as its text.
		across, _ := realBoxes(w, app, text, wrap, gpui.TextAlignLeft, mdRange(2, 7))
		if len(across) != 2 || across[0].right != rows[0].right || across[1].left != 0 || across[1].row != 1 {
			t.Errorf("across %+v, rows %+v", across, rows)
		}
	})
}

// U:text/inline.rs:1669-1681
func TestRealHighlightsAfterAHardLineBreakStartOnItsRow(t *testing.T) {
	inRealPrepaint(t, func(w *gpui.Window, app *gpui.App) {
		// Rows "aa", "bbb cc": the break is one byte of the text, not a glyph.
		const text = "aa\nbbb cc"
		aa, _ := realBoxes(w, app, text, 1000, gpui.TextAlignLeft, mdRange(0, 2))
		highlight, _ := realBoxes(w, app, text, 1000, gpui.TextAlignLeft, mdRange(3, 6))
		if len(aa) != 1 || len(highlight) != 1 || highlight[0].left != 0 || highlight[0].row != 1 {
			t.Errorf("aa %+v, highlight %+v", aa, highlight)
			return
		}
		// Three glyphs as wide as the two of "aa" and a half.
		if want := (aa[0].right - aa[0].left) * 1.5; highlight[0].right != want {
			t.Errorf("highlight right %v, want %v", highlight[0].right, want)
		}
	})
}

// U:text/inline.rs:1683-1708 (deviated: the align width, below).
func TestRealHighlightsFollowCenteredAndRightAlignedRows(t *testing.T) {
	inRealPrepaint(t, func(w *gpui.Window, app *gpui.App) {
		// Rows "aaaa ", "bbbb ", "cc": the last is narrower than the text,
		// so alignment moves it.
		const text = "aaaa bbbb cc"
		cc := mdRange(10, len(text))
		wrap := realWrap(w)
		widest, _ := realBoxes(w, app, text, wrap, gpui.TextAlignLeft, mdRange(0, 5))
		left, laidOutWidth := realBoxes(w, app, text, wrap, gpui.TextAlignLeft, cc)
		center, _ := realBoxes(w, app, text, wrap, gpui.TextAlignCenter, cc)
		right, _ := realBoxes(w, app, text, wrap, gpui.TextAlignRight, cc)
		if len(widest) == 0 || len(left) == 0 || len(center) == 0 || len(right) == 0 {
			t.Errorf("boxes %+v %+v %+v %+v", widest, left, center, right)
			return
		}
		width := left[0].right - left[0].left
		if !(width > 0 && width < widest[0].right) {
			t.Errorf("%+v in %v", left[0], widest[0].right)
		}
		// GPUI aligns each row in the width of the laid-out text. Deviation:
		// under the pin that is the wrap width (WrappedLineLayout::width is
		// min(wrap width, unwrapped width), R:text_system/line_layout.rs:
		// 647-651); gpui-go's real shaped text measures the widest row
		// (gpui/div.go realShapedText.size over the native summary), so the
		// rows align in that width, and so do the highlights.
		alignWidth := laidOutWidth
		if alignWidth > wrap {
			t.Errorf("laid-out width %v exceeds the wrap width %v", alignWidth, wrap)
		}
		if left[0].left != 0 {
			t.Errorf("left %+v", left[0])
		}
		if want := (rowBox{row: left[0].row, left: (alignWidth - width) / 2, right: (alignWidth + width) / 2}); center[0] != want {
			t.Errorf("center %+v, want %+v", center[0], want)
		}
		if want := (rowBox{row: left[0].row, left: alignWidth - width, right: alignWidth}); right[0] != want {
			t.Errorf("right %+v, want %+v", right[0], want)
		}
	})
}
