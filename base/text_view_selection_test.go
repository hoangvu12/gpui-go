package base

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// The 16 selection tests ticket 28 deferred to ticket 29: 15 of
// U:text/text_view.rs (860-2575) and 1 of U:text/inline_virtual_tests.rs.
// Each window renders its TextView inside a Block parent (ticket 28's
// deviation: a TextView wraps only there).

// textViewTextBounds is selection_adapter.text_bounds() of a view.
func textViewTextBounds(ta *gpui.TestApp, state gpui.Entity[TextViewState]) []gpui.Bounds {
	var out []gpui.Bounds
	ta.Update(func(app *gpui.App) {
		if adapter, ok := textViewBridgeOf(state, app); ok {
			out = adapter.textBoundsForTest()
		}
	})
	return out
}

func selectedViewText(ta *gpui.TestApp, state gpui.Entity[TextViewState]) string {
	return readState(ta, state, func(s *TextViewState) string { return s.SelectedText() })
}

// textViewTestRoot is TextViewTestRoot (U:text/text_view.rs:1487-1507).
func textViewTestRoot(t *testing.T, ta *gpui.TestApp, text string) (gpui.Entity[TextViewState], *gpui.VisualTestContext) {
	state := newTextViewStateEntity(ta, text)
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.Div().Block().W(gpui.Px(160)).
			Child(gpui.Div().Block().H(gpui.Px(24)).OverflowHidden().Child(NewTextView(state).Selectable(true))).
			Child(gpui.Div().Flex().H(gpui.Px(40)).Child("footer")).IntoElement()
	})
	return state, cx
}

func dragSelect(t *testing.T, cx *gpui.VisualTestContext, start, end gpui.Point, drawBetween bool) {
	t.Helper()
	left := gpui.MouseButtonLeft
	cx.SimulateMouseDown(start, left, gpui.Modifiers{})
	if drawBetween {
		drawText(t, cx)
	}
	cx.SimulateMouseMove(end, &left, gpui.Modifiers{})
	if drawBetween {
		drawText(t, cx)
	}
	cx.SimulateMouseUp(end, left, gpui.Modifiers{})
	if drawBetween {
		drawText(t, cx)
	}
}

func multiClick(cx *gpui.VisualTestContext, position gpui.Point, count int) {
	cx.SimulateEvent(&gpui.MouseDownEvent{Position: position, Button: gpui.MouseButtonLeft, ClickCount: count})
	cx.SimulateEvent(&gpui.MouseUpEvent{Position: position, Button: gpui.MouseButtonLeft, ClickCount: count})
}

// controlPlugin is ControlPlugin (U:text/text_view.rs:941-975).
type controlPlugin struct{ clicks *[]string }

func (controlPlugin) Name() string { return "control" }

func (controlPlugin) Parse(node *MdastNode, _ *MarkdownParseContext) *MarkdownNode {
	if node.Kind != MdastLink {
		return nil
	}
	label, ok := strings.CutPrefix(node.URL, "control:")
	if !ok {
		return nil
	}
	n := NewMarkdownNode("control", nil).Text(label)
	return &n
}

func (p controlPlugin) Render(node *MarkdownNode, _ *gpui.Window, _ *gpui.App) gpui.AnyElement {
	label := node.AsText()
	return gpui.Div().Flex().ID(gpui.NameElementID("same-control-id")).W(gpui.Px(60)).H(gpui.Px(24)).
		OnMouseDown(gpui.MouseButtonLeft, func(_ *gpui.MouseDownEvent, _ *gpui.Window, app *gpui.App) { app.StopPropagation() }).
		OnClick(func(*gpui.ClickEvent, *gpui.Window, *gpui.App) { *p.clicks = append(*p.clicks, label) }).
		Child(label).IntoElement()
}

// U:text/text_view.rs:939-1009
func TestInlinePluginReusesRenderAndPreservesNativeChildEvents(t *testing.T) {
	ta := textTestApp(t)
	var clicks []string
	state := newTextViewStateEntity(ta, "[one](control:one)[two](control:two)")
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.Div().Block().W(gpui.Px(300)).Child(TextSelectionLayer{}).
			Child(NewTextView(state).Plugin(controlPlugin{&clicks})).IntoElement()
	})
	drawText(t, cx)
	bounds := textViewTextBounds(ta, state)
	if len(bounds) != 2 {
		t.Fatalf("%d text bounds", len(bounds))
	}
	for _, b := range bounds {
		cx.SimulateClick(b.Center(), gpui.Modifiers{})
		drawText(t, cx)
	}
	if strings.Join(clicks, ",") != "one,two" {
		t.Fatalf("clicks %v (bounds %v)", clicks, bounds)
	}
}

// U:text/text_view.rs:1011-1075 over InlineHoverTestRoot (881-937).
func TestInlineHoverCardIsLazyAndRetainsAtomicCopy(t *testing.T) {
	ta := textTestApp(t)
	builds := 0
	source := "[@member](mention:member)"
	format := SelectionFormatPlain
	state := newTextViewStateEntity(ta, source)
	plugin := textRenderPlugin{
		testInlinePlugin: newTestInlinePlugin("mention").parseWith(func(node *MdastNode, _ *MarkdownParseContext) *MarkdownNode {
			if node.Kind != MdastLink {
				return nil
			}
			handle, ok := strings.CutPrefix(node.URL, "mention:")
			if !ok {
				return nil
			}
			n := NewMarkdownNode("mention", nil).Text("@" + handle)
			return &n
		}),
		render: func(*MarkdownNode, *InlineRenderContext, *gpui.Window, *gpui.App) (InlineElement, bool) {
			card := NewHoverCard("mention-hover").Anchor(gpui.AnchorTopCenter).
				Trigger(gpui.Div().Flex().Child("@member")).
				Content(func(*HoverCardState, *gpui.Window, *gpui.Context[HoverCardState]) *gpui.DivElement {
					builds++
					return gpui.Div().Flex().ID(gpui.NameElementID("hover-content")).Child(
						gpui.Div().Flex().DebugSelector("inline-test-card").W(gpui.Px(120)).H(gpui.Px(40)).Child("Member profile"))
				})
			return NewInlineElement(card), true
		},
	}
	root, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.Div().Block().Pl(gpui.Px(150)).W(gpui.Px(300)).TextSize(gpui.Px(16)).Child(TextSelectionLayer{}).
			Child(NewTextView(state).SelectionFormat(format).Plugin(plugin)).IntoElement()
	})
	drawText(t, cx)
	if builds != 0 {
		t.Fatal("the card built before hover")
	}
	bounds := textViewTextBounds(ta, state)[0]
	cx.SimulateMouseMove(gpui.Point{X: bounds.Right() - 1, Y: bounds.Center().Y}, nil, gpui.Modifiers{})
	ta.RunUntilParked()
	ta.AdvanceClock(1000)
	ta.RunUntilParked()
	drawText(t, cx)
	if builds == 0 {
		t.Fatal("hover did not build the card")
	}
	drawText(t, cx)
	card := debugBounds(t, cx, "inline-test-card")
	if math.Abs(float64(card.Center().X-bounds.Center().X)) >= 1 {
		t.Errorf("card %v must be centered on mention %v", card, bounds)
	}
	if card.Top() < bounds.Bottom() {
		t.Errorf("card %v should be below the mention %v", card, bounds)
	}
	for _, c := range []struct {
		format SelectionFormat
		want   string
	}{{SelectionFormatPlain, "@member"}, {SelectionFormatSource, source}} {
		format = c.format
		root.Update(ta, func(_ *testView, cx *gpui.Context[testView]) { cx.Notify() })
		state.Update(ta, func(s *TextViewState, cx *gpui.Context[TextViewState]) {
			s.SetSelectionFormat(c.format, cx)
			s.SelectAll(cx)
		})
		drawText(t, cx)
		if got := strings.TrimSpace(selectedViewText(ta, state)); got != c.want {
			t.Errorf("format %v: %q, want %q", c.format, got, c.want)
		}
	}
}

// inlinePluginRoot is InlinePluginTestRoot (U:text/text_view.rs:1066-1097).
type inlinePluginRoot struct {
	width, fontSize float32
	sourceFormat    bool
	preparedSize    *int
}

func (r *inlinePluginRoot) open(t *testing.T, ta *gpui.TestApp, state gpui.Entity[TextViewState]) (gpui.Entity[testView], *gpui.VisualTestContext) {
	plugin := textRenderPlugin{
		testInlinePlugin: newTestInlinePlugin("math").parseWith(func(node *MdastNode, _ *MarkdownParseContext) *MarkdownNode {
			if node.Kind != MdastInlineMath {
				return nil
			}
			n := NewMarkdownNode("math", nil).Text(node.Value + "²")
			return &n
		}),
		render: func(_ *MarkdownNode, context *InlineRenderContext, _ *gpui.Window, _ *gpui.App) (InlineElement, bool) {
			if r.preparedSize == nil {
				return InlineElement{}, false
			}
			value := float32(*r.preparedSize)
			img := gpui.Img(gpui.NewImage(gpui.ImageFormatSVG, []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40"><path d="M0 0L40 40" stroke="black"/></svg>`))).Flex()
			unit := context.FontSize() / 16
			return NewInlineElement(img.W(gpui.Px(unit * value)).H(gpui.Px(unit * value))).WithBaseline(unit * value * 0.75), true
		},
	}
	return openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		format := SelectionFormatPlain
		if r.sourceFormat {
			format = SelectionFormatSource
		}
		return gpui.Div().Block().W(gpui.Px(r.width)).TextSize(gpui.Px(r.fontSize)).Child(TextSelectionLayer{}).
			Child(NewTextView(state).SelectionFormat(format).Plugin(plugin)).IntoElement()
	})
}

func boundsMidLeft(b gpui.Bounds) gpui.Point {
	return gpui.Point{X: b.Left() + 0.1, Y: b.Top() + b.Size.Height/2}
}

func boundsMidRight(b gpui.Bounds) gpui.Point {
	return gpui.Point{X: b.Right() - 0.1, Y: b.Top() + b.Size.Height/2}
}

// U:text/text_view.rs:1099-1158
func TestInlinePluginsDragAndCopyAcrossFormulasInBothDirections(t *testing.T) {
	ta := textTestApp(t)
	state := newTextViewStateEntity(ta, "中文 $x$ $y$ English")
	root := &inlinePluginRoot{width: 420, fontSize: 16}
	view, cx := root.open(t, ta, state)
	for _, fontSize := range []float32{16, 24, 32} {
		for _, width := range []float32{60, 160, 420} {
			for _, prepared := range []*int{nil, func() *int { v := 32; return &v }()} {
				for _, sourceFormat := range []bool{false, true} {
					root.sourceFormat, root.width, root.fontSize, root.preparedSize = sourceFormat, width, fontSize, prepared
					view.Update(ta, func(_ *testView, cx *gpui.Context[testView]) { cx.Notify() })
					drawText(t, cx)
					textBounds := textViewTextBounds(ta, state)
					if len(textBounds) == 0 {
						t.Fatalf("no text bounds (font %v width %v)", fontSize, width)
					}
					left, right := boundsMidLeft(textBounds[0]), boundsMidRight(textBounds[len(textBounds)-1])
					want := "中文 x² y² English"
					if sourceFormat {
						want = "中文 $x$ $y$ English"
					}
					for _, ends := range [][2]gpui.Point{{left, right}, {right, left}} {
						dragSelect(t, cx, ends[0], ends[1], true)
						if got := strings.TrimSpace(selectedViewText(ta, state)); got != want {
							t.Fatalf("start=%v end=%v width=%v font=%v prepared=%v source=%v: %q, want %q",
								ends[0], ends[1], width, fontSize, prepared != nil, sourceFormat, got, want)
						}
					}
				}
			}
		}
	}
}

// U:text/text_view.rs:1160-1207
func TestInlineResourceSizeUpdateReflowsWithoutChangingSelection(t *testing.T) {
	ta := textTestApp(t)
	state := newTextViewStateEntity(ta, "中文 $x$ $y$ English")
	prepared := 16
	root := &inlinePluginRoot{width: 160, fontSize: 16, preparedSize: &prepared}
	_, cx := root.open(t, ta, state)
	drawText(t, cx)
	before := readState(ta, state, func(s *TextViewState) gpui.Bounds { return s.Bounds() })
	regions := textViewTextBounds(ta, state)
	first, last := regions[0], regions[len(regions)-1]
	dragSelect(t, cx, gpui.Point{X: first.Left() + 0.1, Y: first.Top() + 10}, gpui.Point{X: last.Right() - 0.1, Y: last.Top() + 10}, true)
	const want = "中文 x² y² English"
	if got := strings.TrimSpace(selectedViewText(ta, state)); got != want {
		t.Fatalf("selected %q", got)
	}
	prepared = 100
	state.Update(ta, func(s *TextViewState, cx *gpui.Context[TextViewState]) { s.InvalidateInlineLayout(cx) })
	drawText(t, cx)
	after := readState(ta, state, func(s *TextViewState) gpui.Bounds { return s.Bounds() })
	if !(after.Size.Height > before.Size.Height*2) {
		t.Fatalf("height %v -> %v", before.Size.Height, after.Size.Height)
	}
	if got := strings.TrimSpace(selectedViewText(ta, state)); got != want {
		t.Fatalf("selection changed to %q", got)
	}
}

// U:text/text_view.rs:1209-1256
func TestTripleClickOnFormulaSelectsItsEntireMixedLine(t *testing.T) {
	ta := textTestApp(t)
	state := newTextViewStateEntity(ta, "before $x$ after")
	root := &inlinePluginRoot{width: 420, fontSize: 16}
	view, cx := root.open(t, ta, state)
	drawText(t, cx)
	regions := textViewTextBounds(ta, state)
	if len(regions) < 2 {
		t.Fatalf("%d regions", len(regions))
	}
	multiClick(cx, regions[1].Center(), 3)
	drawText(t, cx)
	if got := strings.TrimSpace(selectedViewText(ta, state)); got != "before x² after" {
		t.Fatalf("selected %q", got)
	}
	root.sourceFormat = true
	view.Update(ta, func(_ *testView, cx *gpui.Context[testView]) { cx.Notify() })
	drawText(t, cx)
	if got := strings.TrimSpace(selectedViewText(ta, state)); got != "before $x$ after" {
		t.Fatalf("source selected %q", got)
	}
}

// U:text/text_view.rs:1258-1298
func TestDoubleClickOnFormulaSelectsOnlyThatObject(t *testing.T) {
	ta := textTestApp(t)
	state := newTextViewStateEntity(ta, "before $x$ after")
	root := &inlinePluginRoot{width: 420, fontSize: 16}
	_, cx := root.open(t, ta, state)
	drawText(t, cx)
	regions := textViewTextBounds(ta, state)
	if len(regions) < 2 {
		t.Fatalf("%d regions", len(regions))
	}
	multiClick(cx, regions[1].Center(), 2)
	drawText(t, cx)
	// Two clicks stop at the object; only three take the whole line.
	if got := strings.TrimSpace(selectedViewText(ta, state)); got != "x²" {
		t.Fatalf("selected %q", got)
	}
}

// documentOf is document_of (U:text/text_view.rs:1313-1320), deviated:
// the test window's text system lays every text out on one line (gpui
// testShapedText), so a paragraph cannot grow with its word count. Each
// block is a heading paragraph and a list of `words` items instead; the
// paragraphs keep the lists apart, so both documents have 2*count blocks
// and the tall one is far taller.
func documentOf(count, words int) string {
	blocks := make([]string, count)
	for i := range blocks {
		items := strings.TrimSuffix(strings.Repeat("- lorem ipsum dolor\n", words), "\n")
		blocks[i] = fmt.Sprintf("Block %d:\n\n%s", i+1, items)
	}
	return strings.Join(blocks, "\n\n")
}

// U:text/text_view.rs:1322-1380 over ScrollExtentTestRoot (1298-1311);
// deviated through documentOf.
func TestReplacingADocumentWithAnEqualBlockCountRemeasures(t *testing.T) {
	ta := textTestApp(t)
	const blocks = 24
	state := newTextViewStateEntity(ta, documentOf(blocks, 1))
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.Div().Block().W(gpui.Px(400)).H(gpui.Px(200)).OverflowHidden().Child(NewTextView(state).Scrollable(true)).IntoElement()
	})
	drawText(t, cx)
	extent := func() float32 {
		return readState(ta, state, func(s *TextViewState) float32 { return s.ListState().MaxOffsetForScrollbar().Y })
	}
	short := extent()
	state.Update(ta, func(s *TextViewState, cx *gpui.Context[TextViewState]) { s.SetText(documentOf(blocks, 60), cx) })
	drawText(t, cx)
	if n := readState(ta, state, func(s *TextViewState) int { return s.ListState().ItemCount() }); n != 2*blocks {
		t.Fatalf("%d blocks: the replacement must keep the block count", n)
	}
	if tall := extent(); !(tall > short*5) {
		t.Fatalf("the scroll extent went from %v to %v", short, tall)
	}
}

// U:text/text_view.rs:1523-1551 over TableSelectionTestRoot (1509-1521).
func TestTableDragSelectionSettlesWithoutRequestingIdleFrames(t *testing.T) {
	ta := textTestApp(t)
	state := newTextViewStateEntity(ta, "| Header 1 | Header 2 |\n| --- | --- |\n| Cell A | Cell B |\n| Cell C | Cell D |")
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.Div().Block().DebugSelector("table-selection-root").W(gpui.Px(520)).
			Child(TextSelectionLayer{}).Child(NewTextView(state)).IntoElement()
	})
	drawText(t, cx)
	bounds := debugBounds(t, cx, "table-selection-root")
	dragSelect(t, cx, gpui.Point{X: bounds.Left() + 24, Y: bounds.Top() + 16}, gpui.Point{X: bounds.Right() - 24, Y: bounds.Bottom() - 16}, false)
	cx.Update(func(w *gpui.Window, app *gpui.App) {
		if !TextSelectionHasSelection(w, app) {
			t.Fatal("no selection")
		}
		if n := w.SimulateNextFrame(app); n != 0 {
			t.Fatalf("finished table selection requested %d frames", n)
		}
	})
}

// U:text/text_view.rs:1578-1607 over InlineImageTextViewTestRoot
// (1553-1576).
func TestInlineImageKeepsSurroundingTextOnSameLine(t *testing.T) {
	ta := textTestApp(t)
	state := newTextViewStateEntity(ta, "Build Status ![inline image](https://example.com/image.svg) after")
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.Div().Block().W(gpui.Px(420)).Child(NewTextView(state).Selectable(true)).IntoElement()
	})
	drawText(t, cx)
	b := textViewTextBounds(ta, state)
	if len(b) != 2 {
		t.Fatalf("%d inline bounds: %v", len(b), b)
	}
	if b[0].Top() != b[1].Top() {
		t.Errorf("text before and after an inline image should share a rendered line: %v", b)
	}
	if gap := b[1].Left() - b[0].Right(); !(gap > 8) || !(gap < 40) {
		t.Errorf("the image reserves %v px", gap)
	}
}

// U:text/text_view.rs:2160-2226
func TestInlineObjectInheritsBoldAndLinkClicksWithoutOpeningOnDrag(t *testing.T) {
	ta := textTestApp(t)
	var clicks []string
	state := newTextViewStateEntity(ta, "**[$x$](https://example.com)**")
	plugin := textRenderPlugin{
		testInlinePlugin: newTestInlinePlugin("math").parseWith(func(node *MdastNode, _ *MarkdownParseContext) *MarkdownNode {
			if node.Kind != MdastInlineMath {
				return nil
			}
			n := NewMarkdownNode("math", nil).Text("formula")
			return &n
		}),
		render: func(_ *MarkdownNode, context *InlineRenderContext, _ *gpui.Window, _ *gpui.App) (InlineElement, bool) {
			if context.TextStyle().FontWeight != 700 {
				t.Errorf("weight %v", context.TextStyle().FontWeight)
			}
			return NewInlineElement(gpui.Div().Flex().Child("formula")), true
		},
	}
	extensions := MarkdownExtensions{}.Plugin(plugin)
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.Div().Block().W(gpui.Px(300)).Child(TextSelectionLayer{}).Child(
			NewTextView(state).MarkdownExtensions(extensions).OnLinkClick(func(url string, _ *gpui.ClickEvent, _ *gpui.Window, _ *gpui.App) {
				clicks = append(clicks, url)
			})).IntoElement()
	})
	drawText(t, cx)
	bounds := textViewTextBounds(ta, state)[0]
	for _, button := range []gpui.MouseButton{gpui.MouseButtonLeft, gpui.MouseButtonMiddle, gpui.MouseButtonRight} {
		cx.SimulateMouseDown(bounds.Center(), button, gpui.Modifiers{})
		cx.SimulateMouseUp(bounds.Center(), button, gpui.Modifiers{})
	}
	if len(clicks) != 3 {
		t.Fatalf("%d clicks", len(clicks))
	}
	dragSelect(t, cx, gpui.Point{X: bounds.Left() + 1, Y: bounds.Center().Y}, gpui.Point{X: bounds.Right() - 1, Y: bounds.Center().Y}, true)
	if len(clicks) != 3 {
		t.Fatalf("a drag opened the link: %d clicks", len(clicks))
	}
}

// U:text/text_view.rs:2305-2333
func TestClippedMarkdownCannotStartSelection(t *testing.T) {
	ta := textTestApp(t)
	state, cx := textViewTestRoot(t, ta, "visible\n\nhidden selection text")
	drawText(t, cx)
	dragSelect(t, cx, tpt(10, 34), tpt(90, 34), false)
	if got := selectedViewText(ta, state); got != "" {
		t.Fatalf("unexpected selection: %q", got)
	}
}

// U:text/text_view.rs:2370-2427 over ClippedTallTextViewTestRoot
// (2335-2368).
func TestSelectionBandBeyondClipCopiesOffscreenText(t *testing.T) {
	ta := textTestApp(t)
	state := newTextViewStateEntity(ta, "alpha\n\nbravo\n\ncharlie\n\ndelta")
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.Div().Block().W(gpui.Px(200)).Child(TextSelectionLayer{}).
			Child(gpui.Div().Block().H(gpui.Px(40)).OverflowHidden().Child(NewTextView(state).Selectable(true))).
			Child(gpui.Div().Flex().H(gpui.Px(160))).IntoElement()
	})
	drawText(t, cx)
	dragSelect(t, cx, tpt(2, 8), tpt(180, 150), true)
	got := selectedViewText(ta, state)
	if !strings.Contains(got, "delta") || !strings.Contains(got, "charlie") {
		t.Fatalf("clipped-out text was not copied: %q", got)
	}
}

// U:text/text_view.rs:2429-2460
func TestDoubleClickSelectsWord(t *testing.T) {
	ta := textTestApp(t)
	state, cx := textViewTestRoot(t, ta, "quick select value")
	drawText(t, cx)
	multiClick(cx, tpt(10, 16), 2)
	drawText(t, cx)
	if got := strings.TrimSpace(selectedViewText(ta, state)); got != "quick" {
		t.Fatalf("selected %q", got)
	}
}

// U:text/text_view.rs:2462-2494
func TestTripleClickSelectsParagraph(t *testing.T) {
	ta := textTestApp(t)
	state, cx := textViewTestRoot(t, ta, "quick select value")
	drawText(t, cx)
	multiClick(cx, tpt(10, 10), 3)
	drawText(t, cx)
	if got := strings.TrimSpace(selectedViewText(ta, state)); got != "quick select value" {
		t.Fatalf("selected %q", got)
	}
}

// U:text/text_view.rs:2496-2574
func TestOuterListContentTotalStableWhileScrolling(t *testing.T) {
	items := []string{
		"# Heading\n\nA paragraph long enough to wrap across several lines and produce a non-trivial height.",
		"Short.",
		"Paragraph A\n\nParagraph B\n\nParagraph C with more words to increase the height.",
		"## Subheading\n\n- One\n- Two\n- Three\n\nClosing paragraph.",
		"Only one line.",
		"**Bold**: medium length text with `code` mixed with regular words.",
		"1. First\n2. Second\n3. Third\n\nA short closing paragraph.",
		"A long message with enough words to wrap across multiple lines, create a taller item, and verify that off-screen measurement matches visible measurement.",
	}
	const n = 40
	ta := textTestApp(t)
	list := gpui.NewListState(n, 2048).MeasureAll()
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.Div().Flex().W(gpui.Px(360)).H(gpui.Px(500)).Child(
			gpui.List(list, func(ix int, _ *gpui.Window, _ *gpui.App) gpui.AnyElement {
				return gpui.Div().Block().WFull().Child(TextViewMarkdown(gpui.NamedIntegerElementID("md", uint64(ix)), items[ix%len(items)])).IntoElement()
			}).SizeFull()).IntoElement()
	})
	drawText(t, cx)
	drawText(t, cx)
	total := func() float32 { return list.MaxOffsetForScrollbar().Y + list.ViewportBounds().Size.Height }
	totals := []float32{total()}
	for range 20 {
		cx.Update(func(_ *gpui.Window, app *gpui.App) { list.ScrollBy(150, nil, app) })
		drawText(t, cx)
		totals = append(totals, total())
	}
	lo, hi := totals[0], totals[0]
	for _, v := range totals {
		lo, hi = min(lo, v), max(hi, v)
	}
	if hi-lo >= 2 {
		t.Fatalf("list content total jittered while scrolling: min=%v max=%v totals=%v", lo, hi, totals)
	}
}

// U:text/inline_virtual_tests.rs:61-133 over VirtualInlineRoot (15-46).
// Deviation: ListState has no scroll_to_end; scrolling to the item count
// is what it does (R:elements/list.rs scroll_to_end).
func TestInlineVirtualResourceGrowthRemeasuresAllBlocksAndPreservesCopy(t *testing.T) {
	ta := textTestApp(t)
	state := newTextViewStateEntity(ta, virtualDocument())
	prepared := 16
	sourceFormat := false
	plugin := textRenderPlugin{
		testInlinePlugin: newTestInlinePlugin("math").parseWith(func(node *MdastNode, _ *MarkdownParseContext) *MarkdownNode {
			if node.Kind != MdastInlineMath {
				return nil
			}
			n := NewMarkdownNode("math", nil).Text(node.Value + "²")
			return &n
		}),
		render: func(_ *MarkdownNode, context *InlineRenderContext, _ *gpui.Window, _ *gpui.App) (InlineElement, bool) {
			extent := context.FontSize() * float32(prepared) / 16
			img := gpui.Img(gpui.NewImage(gpui.ImageFormatSVG, []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40"><path d="M0 0L40 40" stroke="black"/></svg>`))).Flex().W(gpui.Px(extent)).H(gpui.Px(extent))
			return NewInlineElement(img).WithBaseline(extent * 0.75), true
		},
	}
	root, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		format := SelectionFormatPlain
		if sourceFormat {
			format = SelectionFormatSource
		}
		return gpui.Div().Block().W(gpui.Px(400)).H(gpui.Px(200)).OverflowHidden().TextSize(gpui.RemsOf(1)).
			Child(TextSelectionLayer{}).
			Child(NewTextView(state).Scrollable(true).SelectionFormat(format).Plugin(plugin)).IntoElement()
	})
	settle := func() { drawText(t, cx) }
	totalHeight := func() float32 {
		return readState(ta, state, func(s *TextViewState) float32 {
			if s.listState.ItemCount() != virtualBlocks {
				t.Fatalf("%d blocks", s.listState.ItemCount())
			}
			return s.listState.MaxOffsetForScrollbar().Y + s.listState.ViewportBounds().Size.Height
		})
	}
	settle()
	before := totalHeight()
	bounds := textViewTextBounds(ta, state)
	left := gpui.MouseButtonLeft
	cx.SimulateMouseDown(boundsMidLeft(bounds[0]), left, gpui.Modifiers{})
	settle()
	cx.SimulateMouseMove(boundsMidRight(bounds[len(bounds)-1]), &left, gpui.Modifiers{})
	settle()
	state.Update(ta, func(s *TextViewState, _ *gpui.Context[TextViewState]) {
		s.listState.ScrollTo(gpui.ListOffset{ItemIx: s.listState.ItemCount()})
	})
	root.Update(ta, func(_ *testView, cx *gpui.Context[testView]) { cx.Notify() })
	settle()
	bounds = textViewTextBounds(ta, state)
	end := boundsMidRight(bounds[len(bounds)-1])
	cx.SimulateMouseMove(end, &left, gpui.Modifiers{})
	settle()
	cx.SimulateMouseUp(end, left, gpui.Modifiers{})
	settle()
	all := [2]int{0, virtualBlocks - 1}
	copyText := func() string {
		return readState(ta, state, func(s *TextViewState) string { return s.selectedTextIn(&all) })
	}
	copyBefore := copyText()
	if !strings.Contains(copyBefore, "第12段 x² y² English") {
		t.Fatalf("offscreen covered blocks must use their logical text: %q", copyBefore)
	}
	prepared = 96
	state.Update(ta, func(s *TextViewState, cx *gpui.Context[TextViewState]) { s.InvalidateInlineLayout(cx) })
	settle()
	if after := totalHeight(); !(after > before*2) {
		t.Fatalf("before=%v after=%v", before, after)
	}
	if got := copyText(); got != copyBefore {
		t.Fatalf("copy changed: %q -> %q", copyBefore, got)
	}
	sourceFormat = true
	root.Update(ta, func(_ *testView, cx *gpui.Context[testView]) { cx.Notify() })
	settle()
	if source := copyText(); !strings.Contains(source, "第12段 $x$ $y$ English") {
		t.Fatalf("source copy %q", source)
	}
}
