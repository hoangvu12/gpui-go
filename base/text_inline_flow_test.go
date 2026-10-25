package base

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ports of the U:text/inline_flow.rs tests (1644-2139): 10 of 10; 5
// ported, 5 deviated. The deviated ones run under the deterministic test
// text system (gpui/testwindow.go: every glyph 0.6 em per UTF-16 unit,
// ascent 1.025 em, descent 0.275 em, every family alike) instead of Base's
// WideMonoTextSystem fake, whose Mono family shapes twice as wide as the
// body: gpui exposes no platform text-system seam to tests. They keep each
// test's property with the widths that system gives; the family-specific
// width checks (a span measured in its own family) cannot be told apart
// from body measurement there and are recorded as such per test.

// inPrepaint runs f inside a real prepaint pass (U:text/inline.rs:1739-1759
// test_draw::in_prepaint).
func inPrepaint[R any](t *testing.T, f func(w *gpui.Window, app *gpui.App) R) R {
	t.Helper()
	ta := textTestApp(t)
	var out R
	ran := false
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.Canvas(func(_ gpui.Bounds, w *gpui.Window, app *gpui.App) struct{} {
			if !ran {
				ran = true
				out = f(w, app)
			}
			return struct{}{}
		}, func(gpui.Bounds, struct{}, *gpui.Window, *gpui.App) {}).Flex().W(gpui.Px(1000)).H(gpui.Px(1000)).IntoElement()
	})
	drawText(t, cx)
	if !ran {
		t.Fatal("prepaint probe did not run")
	}
	return out
}

// testWidthOf is the test text system's width of text at fontSize.
func testWidthOf(text string, fontSize float32) float32 {
	n := 0
	for _, r := range text {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return fontSize * 600 / 1000 * float32(n)
}

func flowText(text string, highlights ...inlineHighlightRange) inlineFlowItem {
	return inlineFlowItem{kind: inlineFlowText, text: text, highlights: highlights, state: &inlineState{}}
}

func bodyStyle(fontSize float32) gpui.TextStyle {
	style := gpui.DefaultTextStyle()
	style.FontFamily = "Body"
	style.FontSize = gpui.Px(fontSize)
	return style
}

type answerView struct {
	state gpui.Entity[TextViewState]
	width float32
}

func openAnswer(t *testing.T, ta *gpui.TestApp, view *answerView) *gpui.VisualTestContext {
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.Div().Block().W(gpui.Px(view.width)).Child(NewTextView(view.state)).IntoElement()
	})
	return cx
}

// U:text/inline_flow.rs:1676-1695
func TestAnUnchangedFlowIsNotLaidOutAgain(t *testing.T) {
	ta := textTestApp(t)
	cx := openAnswer(t, ta, &answerView{state: newTextViewStateEntity(ta, "Call `foo` now."), width: 300})
	drawText(t, cx)
	after := flowLayouts
	for range 3 {
		drawText(t, cx)
	}
	if flowLayouts != after {
		t.Fatalf("a frame that changes nothing laid the flow out %d more times", flowLayouts-after)
	}
}

// U:text/inline_flow.rs:1697-1712
func TestAFlowIsLaidOutAgainAtAnotherWidth(t *testing.T) {
	ta := textTestApp(t)
	view := &answerView{state: newTextViewStateEntity(ta, "Call `foo` now."), width: 300}
	root, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.Div().Block().W(gpui.Px(view.width)).DebugSelector("answer").Child(NewTextView(view.state)).IntoElement()
	})
	drawText(t, cx)
	before := flowLayouts
	view.width = 120
	root.Update(ta, func(_ *testView, cx *gpui.Context[testView]) { cx.Notify() })
	drawText(t, cx)
	if flowLayouts <= before {
		b, _ := cx.DebugBounds("answer")
		t.Fatalf("a narrower flow was not laid out again (%d, root %v)", flowLayouts, b)
	}
}

// U:text/inline_flow.rs:1714-1734
func TestAFlowIsLaidOutAgainWhenItsTextChanges(t *testing.T) {
	ta := textTestApp(t)
	view := &answerView{state: newTextViewStateEntity(ta, "Call `foo` now."), width: 300}
	cx := openAnswer(t, ta, view)
	drawText(t, cx)
	before := flowLayouts
	updateState(ta, view.state, func(s *TextViewState, cx *gpui.Context[TextViewState]) { s.SetText("Call `foo` and `bar` now.", cx) })
	drawText(t, cx)
	if flowLayouts <= before {
		t.Fatal("a new text was not laid out")
	}
}

// U:text/inline_flow.rs:1737-1808 (deviated: test text system).
func TestAtomicObjectsWrapAndShareABaselineAtMultipleFontSizes(t *testing.T) {
	inPrepaint(t, func(w *gpui.Window, app *gpui.App) struct{} {
		for _, scale := range []float32{1, 1.5, 2} {
			style := bodyStyle(16 * scale)
			object := func(text string, id int) inlineFlowItem {
				return inlineFlowItem{kind: inlineFlowObject, text: text, id: id, selected: &sharedFlag{}}
			}
			items := []inlineFlowItem{flowText("中文 "), object("x²", 0), object("y²", 1), flowText(" English")}
			for _, width := range []float32{1, 30, 80, 500} {
				layout := layoutFlow(items, make([]*gpui.Size, 4), style, &width, w, app)
				var objects []positionedFragment
				for _, f := range layout.fragments {
					if f.kind == positionedObject {
						objects = append(objects, f)
					}
				}
				if len(objects) != 2 {
					t.Fatalf("%d objects", len(objects))
				}
				for _, o := range objects {
					if o.object.metrics.size.Width > width || o.origin.Y < 0 || o.origin.Y+o.object.metrics.size.Height > layout.size.Height {
						t.Errorf("scale %v width %v: object %+v outside the flow %v", scale, width, o.origin, layout.size)
					}
				}
				if width == 500 && objects[0].origin.Y+objects[0].object.metrics.baseline != objects[1].origin.Y+objects[1].object.metrics.baseline {
					t.Errorf("scale %v: objects on one line do not share a baseline", scale)
				}
			}
		}
		return struct{}{}
	})
}

// U:text/inline_flow.rs:1810-1818
func TestInlineImageWithoutExplicitSizeScalesIntrinsicRatioToLineHeight(t *testing.T) {
	if got := inlineImageSizeForLine(&gpui.Size{Width: 160, Height: 40}, 20); got != (gpui.Size{Width: 60, Height: 15}) {
		t.Fatal(got)
	}
}

// U:text/inline_flow.rs:1820-1825
func TestInlineImageWithoutIntrinsicSizeUsesCompactSquareFallback(t *testing.T) {
	if got := inlineImageSizeForLine(nil, 20); got != (gpui.Size{Width: 15, Height: 15}) {
		t.Fatal(got)
	}
}

func monoFamily() inlineHighlight { m := "Mono"; return inlineHighlight{fontFamily: &m} }

// U:text/inline_flow.rs:1827-1933 (deviated: under the test text system
// the mono span shapes as wide as the body, so the wrap width is computed
// from equal widths; the overflow and line-break properties hold as
// upstream).
func TestInlineCodeNearTheWrapWidthDoesNotOverflowTheFlow(t *testing.T) {
	inPrepaint(t, func(w *gpui.Window, app *gpui.App) struct{} {
		const fontSize = 10
		tail := " with code_span_here end"
		code := strings.Index(tail, "code_span_here")
		items := []inlineFlowItem{
			flowText("See "),
			{kind: inlineFlowImage, source: gpui.ImageSourceFromString("https://example.com/badge.png")},
			flowText(tail, inlineHighlightRange{mdRange(code, code+len("code_span_here")), monoFamily()}),
		}
		imageSize := gpui.Size{Width: 10, Height: 10}
		wrap := testWidthOf("See  with  ", fontSize) + imageSize.Width + testWidthOf("code_span_here", fontSize)
		layout := layoutFlow(items, []*gpui.Size{nil, &imageSize, nil}, bodyStyle(fontSize), &wrap, w, app)
		if layout.size.Width > wrap {
			t.Fatalf("flow width %v exceeds wrap width %v", layout.size.Width, wrap)
		}
		var firstY float32
		spanOnFirst, endBelow := false, false
		for i, f := range layout.fragments {
			if f.kind != positionedText {
				continue
			}
			if i == 0 {
				firstY = f.origin.Y
			}
			if strings.Contains(f.text, "code_span_here") {
				if f.size.Width < testWidthOf("code_span_here", fontSize) {
					t.Error("the code span fragment is narrower than its text")
				}
				spanOnFirst = f.origin.Y == firstY
			}
			if strings.TrimSpace(f.text) == "end" && f.origin.Y > firstY {
				endBelow = true
			}
		}
		if !spanOnFirst || !endBelow {
			t.Fatalf("span on first line %v, trailing word wrapped %v", spanOnFirst, endBelow)
		}
		return struct{}{}
	})
}

// U:text/inline_flow.rs:1934-1984
func TestLongInlineCodeWrapsInMixedFlow(t *testing.T) {
	inPrepaint(t, func(w *gpui.Window, app *gpui.App) struct{} {
		for _, text := range []string{"one two three four five six", "very_long_unbroken_identifier", "你好世界你好世界你好世界", strings.Repeat("é", 6)} {
			items := []inlineFlowItem{
				{kind: inlineFlowImage, source: gpui.ImageSourceFromString("https://example.com/icon.png")},
				flowText(text, inlineHighlightRange{mdRange(0, len(text)), monoFamily()}),
			}
			wrap := float32(100)
			layout := layoutFlow(items, []*gpui.Size{{Width: 10, Height: 10}, nil}, bodyStyle(10), &wrap, w, app)
			if layout.size.Width > 100 {
				t.Errorf("%q: %v", text, layout.size)
			}
			var b strings.Builder
			for _, f := range layout.fragments {
				if f.kind == positionedText {
					b.WriteString(f.text)
				}
			}
			if b.String() != text {
				t.Errorf("reconstructed %q, want %q", b.String(), text)
			}
		}
		return struct{}{}
	})
}

func codeHighlight() inlineHighlight {
	h := monoFamily()
	scale := float32(0.875)
	h.fontSizeScale = &scale
	return h
}

// U:text/inline_flow.rs:1986-2018
func TestInlineCodeLineIsAsTallAsAPlainLineWhenGlyphsOverflowIt(t *testing.T) {
	inPrepaint(t, func(w *gpui.Window, app *gpui.App) struct{} {
		style := bodyStyle(40)
		style.LineHeight = gpui.DefinitePx(30)
		items := []inlineFlowItem{flowText("a code z", inlineHighlightRange{mdRange(2, 6), codeHighlight()})}
		layout := layoutFlow(items, []*gpui.Size{nil}, style, nil, w, app)
		if layout.size.Height != gpui.PixelSnap(w, 30) {
			t.Fatalf("height %v", layout.size.Height)
		}
		return struct{}{}
	})
}

// U:text/inline_flow.rs:2020-2138 (deviated: the code width is the test
// text system's width at 0.875 of the body size).
func TestInlineCodeSizeIsRelativeAndUsesTheNativeBodyBaseline(t *testing.T) {
	inPrepaint(t, func(w *gpui.Window, app *gpui.App) struct{} {
		for _, bodySize := range []float32{16, 20, 24} {
			style := bodyStyle(bodySize)
			items := []inlineFlowItem{flowText("a code z", inlineHighlightRange{mdRange(2, 6), codeHighlight()})}
			layout := layoutFlow(items, []*gpui.Size{nil}, style, nil, w, app)
			var frags []positionedFragment
			for _, f := range layout.fragments {
				if f.kind == positionedText {
					frags = append(frags, f)
				}
			}
			if len(frags) != 3 || frags[0].fontSize != bodySize || frags[1].text != "code" || frags[1].fontSize != bodySize*0.875 {
				t.Fatalf("%v: fragments %+v", bodySize, frags)
			}
			if frags[1].origin.Y < 0 || frags[1].origin.Y+frags[1].size.Height > layout.size.Height {
				t.Errorf("%v: the code box leaves its line", bodySize)
			}
			if want := testWidthOf("code", bodySize*0.875) + inlineCodePadding*2; frags[1].size.Width != want {
				t.Errorf("%v: code width %v, want %v", bodySize, frags[1].size.Width, want)
			}
			ascent, descent := shapedMetrics("a", bodySize, textRuns(1, style, nil), w)
			lineHeight := plainLineHeight(style, gpui.RemSize(w), w)
			plainBaseline := (lineHeight-ascent-descent)/2 + ascent
			if layout.size.Height != lineHeight {
				t.Errorf("%v: line height %v, want %v", bodySize, layout.size.Height, lineHeight)
			}
			baseline := func(f positionedFragment, runs []gpui.TextRun) float32 {
				a, d := shapedMetrics(f.text, f.fontSize, runs, w)
				return f.origin.Y + (f.size.Height-a-d)/2 + a
			}
			near := func(a, b float32) bool { return a-b < 1e-3 && b-a < 1e-3 }
			if !near(frags[0].origin.Y, 0) || !near(baseline(frags[0], textRuns(len(frags[0].text), style, nil)), plainBaseline) {
				t.Errorf("%v: body origin %v baseline %v, want %v", bodySize, frags[0].origin.Y, baseline(frags[0], textRuns(len(frags[0].text), style, nil)), plainBaseline)
			}
			codeRuns := textRuns(4, style, []inlineHighlightRange{{mdRange(0, 4), codeHighlight()}})
			if got := baseline(frags[1], codeRuns); !near(got, plainBaseline) {
				t.Errorf("%v: code baseline %v, want %v", bodySize, got, plainBaseline)
			}
		}
		return struct{}{}
	})
}
