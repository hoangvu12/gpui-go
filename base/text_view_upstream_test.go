package base

import (
	"slices"
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ports of the U:text/text_view.rs tests (860-2575). Of 37: 16 ported, 2
// deviated, 19 deferred:
//   - ported by ticket 29 in text_view_selection_test.go (they drag,
//     double/triple click or copy through the window TextSelection layer
//     and the selection adapter):
//     inline_plugin_reuses_render_and_preserves_native_child_events,
//     inline_hover_card_is_lazy_and_retains_atomic_copy,
//     inline_plugins_drag_and_copy_across_formulas_in_both_directions,
//     inline_resource_size_update_reflows_without_changing_selection,
//     triple_click_on_formula_selects_its_entire_mixed_line,
//     double_click_on_formula_selects_only_that_object,
//     replacing_a_document_with_an_equal_block_count_remeasures,
//     table_drag_selection_settles_without_requesting_idle_frames,
//     inline_image_keeps_surrounding_text_on_same_line (reads
//     selection_adapter.text_bounds),
//     inline_object_inherits_bold_and_link_clicks_without_opening_on_drag,
//     clipped_markdown_cannot_start_selection,
//     selection_band_beyond_clip_copies_offscreen_text,
//     double_click_selects_word, triple_click_selects_paragraph,
//     outer_list_content_total_stable_while_scrolling.
//   - ported by ticket 30: inline_html_image_after_newline_does_not_panic
//     (inline HTML, U:text/format/html.rs), at the end of this file.
//   - ported by ticket 37 (found by the coverage audit, outside the 37
//     counted above): linked_image_handler_receives_left_middle_and_right_
//     clicks, deviated like the link tests (no opened_url), at the end.
//   - deferred (gaps outside this ticket): list_item_renders_fenced_code_
//     block_at_document_width needs h_resizable/resizable_panel (not
//     ported); clipped_markdown_link_does_not_open and right_click_does_not
//     _open_url_without_handler assert cx.opened_url(), which gpui's TestApp
//     lacks.
//   - deviated: markdown_link_opens_url_without_handler and
//     link_handler_receives_button_and_modifiers record the click through
//     OnLinkClick (no opened_url in the Go TestApp).

type dummyTextViewPlugin struct{}

func (dummyTextViewPlugin) Setup(v *TextView) *TextView { v.selectable = true; return v }

// U:text/text_view.rs:1406-1414
func TestTextViewConstructorsAreSelectableByDefault(t *testing.T) {
	ta := textTestApp(t)
	state := newTextViewStateEntity(ta, "state")
	if !NewTextView(state).selectable || !TextViewMarkdown("markdown", "text").selectable || !TextViewHTML("html", "<p>text</p>").selectable {
		t.Fatal("a constructor is not selectable")
	}
}

// U:text/text_view.rs:1416-1435 (a generated multi-block document stands
// in for the repository README).
func TestStatelessMarkdownWithRebuiltParserSettles(t *testing.T) {
	ta := textTestApp(t)
	source := strings.Repeat("## Section\n\nSome *text* with `code`.\n\n- item\n\n", 120)
	renders := 0
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		renders++
		return gpui.Div().Block().Child(TextViewMarkdown("stateless-markdown", source).
			MarkdownBlockParser(func(*MdastNode, *MarkdownParseContext) *MarkdownNode { return nil })).IntoElement()
	})
	drawText(t, cx)
	after := renders
	cx.RunUntilParked()
	if renders != after {
		t.Fatalf("an unchanged view rendered %d more times", renders-after)
	}
}

// U:text/text_view.rs:1437-1466
func TestMarkdownDataURLImageIsDecodedInline(t *testing.T) {
	const png = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGP4z8DwHwAFAAH/iZk9HQAAAABJRU5ErkJggg=="
	ta := textTestApp(t)
	state := newTextViewStateEntity(ta, "Inline ![dot](data:image/png;base64,"+png+") image")
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.Div().Block().W(gpui.Px(160)).Child(NewTextView(state)).IntoElement()
	})
	drawText(t, cx)
	image := dataURLImage("data:image/png;base64," + png)
	if !gpui.ImageSourceFromImage(*image).IsAssetCached(ta.App()) {
		t.Fatal("the data URL image was not handed to gpui as decoded bytes")
	}
}

// U:text/text_view.rs:1468-1485
func TestUnstyledTextViewUsesBaseTokensForLinkAndInputSelection(t *testing.T) {
	ta := textTestApp(t)
	ta.Update(func(app *gpui.App) {
		colors := &ThemeGlobalMut(app).Tokens.Colors
		colors.Primary = gpui.Rgb(0x55aaff)
		colors.Selection = gpui.Rgb(0x335577)
	})
	state := newTextViewStateEntity(ta, "[link](url)")
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.Div().Block().W(gpui.Px(160)).Child(NewTextView(state).Selectable(true)).IntoElement()
	})
	drawText(t, cx)
	readState(ta, state, func(s *TextViewState) struct{} {
		if s.textViewStyle.Link() != gpui.Rgb(0x55aaff) || s.textViewStyle.Selection() != gpui.Rgb(0x335577) {
			t.Fatal("theme tokens not used")
		}
		return struct{}{}
	})
}

// U:text/text_view.rs:1679-1766
func TestTableActionsRenderBelowTheTable(t *testing.T) {
	for _, scroll := range []bool{false, true} {
		ta := textTestApp(t)
		var captured []TableData
		_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
			var tableStyle gpui.StyleRefinement
			if scroll {
				tableStyle.OverflowX = gpui.Some(gpui.OverflowScroll)
			}
			return gpui.Div().Block().W(gpui.Px(320)).Child(
				TextViewMarkdown("table-actions", "| Name | Age |\n|:--|--:|\n| Alice | 30 |\n| Bob | 41 |").
					Style(DefaultTextViewStyle().WithTable(tableStyle)).
					TableActions(func(table *TableData, _ *gpui.Window, _ *gpui.App) gpui.AnyElement {
						captured = append(captured, *table)
						return gpui.Div().Flex().DebugSelector("table-action").Child("Copy").IntoElement()
					})).IntoElement()
		})
		drawText(t, cx)
		bounds := debugBounds(t, cx, "table-action")
		if len(captured) == 0 {
			t.Fatal("the hook did not run")
		}
		data := captured[len(captured)-1]
		if bounds.Top() <= 40 {
			t.Errorf("scroll %v: actions at %v, not below the table", scroll, bounds.Top())
		}
		if !slices.Equal(data.Headers, []string{"Name", "Age"}) || len(data.Rows) != 2 || !slices.Equal(data.Rows[0], []string{"Alice", "30"}) ||
			!slices.Equal(data.Rows[1], []string{"Bob", "41"}) || data.Markdown != "| Name | Age |\n| :-- | --: |\n| Alice | 30 |\n| Bob | 41 |" ||
			data.Span == nil || *data.Span != mdRange(0, 52) {
			t.Errorf("scroll %v: data %+v", scroll, data)
		}
	}
}

// U:text/text_view.rs:1768-1773
func TestPluginAcceptsTextViewPluginsBeyondMarkdown(t *testing.T) {
	if !TextViewMarkdown("plugin-test", "").WithPlugin(dummyTextViewPlugin{}).selectable {
		t.Fatal("plugin not applied")
	}
}

// U:text/text_view.rs:1775-1790
func TestSyntaxHighlightingIsOptIn(t *testing.T) {
	view := TextViewMarkdown("default-code", "```rust\nfn main() {}\n```")
	if view.codeBlockHighlighter != nil {
		t.Fatal("highlighting on by default")
	}
	view = view.CodeBlockHighlighter(func(b *CodeBlock) []gpui.HighlightRange {
		return []gpui.HighlightRange{{Range: mdRange(0, len(b.Code())), Style: gpui.HighlightStyleFromColor(gpui.Rgb(0x3366ff))}}
	})
	if view.codeBlockHighlighter == nil {
		t.Fatal("highlighter not set")
	}
}

func clip(spans []textLineSpan, box, content float32) *float32 {
	if c, ok := lineSafeClipBottom(spans, box, content); ok {
		return &c
	}
	return nil
}

func clipEq(got, want *float32) bool {
	return (got == nil) == (want == nil) && (got == nil || *got == *want)
}

// U:text/text_view.rs:1828-1876
func TestTheClipOnlyMovesForAStraddlingGlyphLine(t *testing.T) {
	spans := []textLineSpan{{0, 60, 20}, {68, 128, 20}}
	f := func(v float32) *float32 { return &v }
	for _, c := range []struct {
		spans        []textLineSpan
		box, content float32
		want         *float32
	}{
		{spans, 100, 400, f(88)}, {spans, 88, 400, nil}, {spans, 64, 400, f(60)},
		{[]textLineSpan{{0, 60, 20}}, 200, 400, nil}, {spans, 130, 128, nil},
	} {
		if got := clip(c.spans, c.box, c.content); !clipEq(got, c.want) {
			t.Errorf("box %v: %v, want %v", c.box, got, c.want)
		}
	}
}

// U:text/text_view.rs:1878-1891
func TestALineTallerThanTheBudgetKeepsThePartThatFits(t *testing.T) {
	if got := clip([]textLineSpan{{70, 98, 28}}, 96, 400); got != nil {
		t.Fatalf("clip %v", *got)
	}
}

// U:text/text_view.rs:1893-1919
func TestTheClipDoesNotStopOnARowOfBorderAndPadding(t *testing.T) {
	want := float32(126)
	if got := clip([]textLineSpan{{100, 126, 26}, {135, 161, 26}}, 148, 400); !clipEq(got, &want) {
		t.Fatalf("clip %v", got)
	}
}

func isClamped(ta *gpui.TestApp, state gpui.Entity[TextViewState]) bool {
	return readState(ta, state, func(s *TextViewState) bool { return s.IsClamped() })
}

// U:text/text_view.rs:1958-1976
func TestMaxLinesMeasuresOverflowInsideASizedPage(t *testing.T) {
	ta := textTestApp(t)
	state := newTextViewStateEntity(ta, "first\n\nsecond\n\nthird\n\nfourth\n\nfifth\n\nsixth\n\nseventh")
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return VFlex().SizeFull().P4().Gap4().
			Child(HFlex().MaxW(gpui.Px(480)).Gap3().Child("header")).
			Child(VFlex().Flex1().MinH0().Gap4().ID("clamped-page-scroll").OverflowYScroll().
				Child(VFlex().MaxW(gpui.Px(480)).P3().Gap2().Child(NewTextView(state).MaxLines(3)))).
			IntoElement()
	})
	drawText(t, cx)
	if !isClamped(ta, state) {
		t.Fatal("not clamped")
	}
}

func maxLinesRoot(t *testing.T, ta *gpui.TestApp, state gpui.Entity[TextViewState], n int) {
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.Div().Block().W(gpui.Px(200)).Child(NewTextView(state).MaxLines(n)).IntoElement()
	})
	drawText(t, cx)
}

// U:text/text_view.rs:1978-1991
func TestMaxLinesClampsOverflowingContent(t *testing.T) {
	ta := textTestApp(t)
	state := newTextViewStateEntity(ta, "first\n\nsecond\n\nthird\n\nfourth\n\nfifth\n\nsixth")
	maxLinesRoot(t, ta, state, 2)
	if !isClamped(ta, state) {
		t.Fatal("not clamped")
	}
}

// U:text/text_view.rs:1993-2000
func TestMaxLinesLeavesShortContentUnclamped(t *testing.T) {
	ta := textTestApp(t)
	state := newTextViewStateEntity(ta, "only line")
	maxLinesRoot(t, ta, state, 3)
	if isClamped(ta, state) {
		t.Fatal("clamped")
	}
}

// U:text/text_view.rs:2002-2056
func TestHeadingRefinementChangesRenderedHeadingGeometry(t *testing.T) {
	ta := textTestApp(t)
	custom := DefaultTextViewStyle().WithHeading(func(level uint8) gpui.StyleRefinement {
		if level == 1 {
			return NewStateStyle().Pb(gpui.RemsOf(2)).intoRefinement()
		}
		return gpui.StyleRefinement{}
	})
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		cell := func(selector string, view *TextView) *gpui.DivElement {
			return gpui.Div().Block().DebugSelector(selector).Child(view)
		}
		return gpui.Div().Flex().ItemsStart().
			Child(cell("default-h1", TextViewMarkdown("default-h1-view", "# Heading"))).
			Child(cell("custom-h1", TextViewMarkdown("custom-heading-view", "# Heading").Style(custom))).
			Child(cell("default-h2", TextViewMarkdown("default-h2-view", "## Heading"))).
			Child(cell("custom-h2", TextViewMarkdown("custom-h2-view", "## Heading").Style(custom))).
			IntoElement()
	})
	drawText(t, cx)
	if debugBounds(t, cx, "custom-h1").Size.Height <= debugBounds(t, cx, "default-h1").Size.Height {
		t.Error("the level-1 refinement did not grow the heading")
	}
	if debugBounds(t, cx, "custom-h2").Size.Height != debugBounds(t, cx, "default-h2").Size.Height {
		t.Error("the level-1 refinement changed a level-2 heading")
	}
}

// linkRoot is a TextViewTestRoot (U:text/text_view.rs:1487-1507) whose
// view records link clicks.
func linkRoot(t *testing.T, ta *gpui.TestApp, text string, clicks *[]gpui.ClickEvent) *gpui.VisualTestContext {
	state := newTextViewStateEntity(ta, text)
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.Div().Block().W(gpui.Px(160)).
			Child(gpui.Div().Block().H(gpui.Px(24)).OverflowHidden().Child(NewTextView(state).Selectable(true).
				OnLinkClick(func(url string, event *gpui.ClickEvent, _ *gpui.Window, _ *gpui.App) {
					if url == "https://example.com" {
						*clicks = append(*clicks, *event)
					}
				}))).
			Child(gpui.Div().Flex().H(gpui.Px(40)).Child("footer")).IntoElement()
	})
	drawText(t, cx)
	return cx
}

// U:text/text_view.rs:2058-2068 (deviated: the click is recorded through
// OnLinkClick).
func TestMarkdownLinkOpensURL(t *testing.T) {
	ta := textTestApp(t)
	var clicks []gpui.ClickEvent
	cx := linkRoot(t, ta, "[example](https://example.com)", &clicks)
	cx.SimulateClick(tpt(10, 10), gpui.Modifiers{})
	if len(clicks) != 1 {
		t.Fatalf("%d link clicks", len(clicks))
	}
}

// U:text/text_view.rs:2091-2159 (deviated as above; the handler sees the
// button and modifiers of the click).
func TestLinkHandlerReceivesButtonAndModifiers(t *testing.T) {
	ta := textTestApp(t)
	var clicks []gpui.ClickEvent
	cx := linkRoot(t, ta, "[example](https://example.com)", &clicks)
	cx.SimulateMouseDown(tpt(10, 10), gpui.MouseButtonMiddle, gpui.Modifiers{Control: true})
	cx.SimulateMouseUp(tpt(10, 10), gpui.MouseButtonMiddle, gpui.Modifiers{Control: true})
	if len(clicks) != 1 || clicks[0].Kind != gpui.ClickEventMouse || clicks[0].Mouse.Up.Button != gpui.MouseButtonMiddle || !clicks[0].Mouse.Up.Modifiers.Control {
		t.Fatalf("clicks %+v", clicks)
	}
}

// U:text/text_view.rs:1609-1624 (ticket 30: inline HTML through
// U:text/format/html.rs).
func TestInlineHTMLImageAfterNewlineDoesNotPanic(t *testing.T) {
	ta := textTestApp(t)
	_, cx := textViewTestRoot(t, ta, "Hi\n[<img src=\"https://example.com/image.svg\">](https://google.com/)")
	cx.RunUntilParked()
	if _, err := gpui.DrawWindowFrame(cx.Window()); err != nil {
		t.Fatal(err)
	}
}

// U:text/text_view.rs:2227-2303 (deviated: cx.opened_url() is not in the
// Go TestApp, as for the link tests above; the handler records the
// clicks). A linked inline image passes left, middle and right clicks to
// the link handler.
func TestLinkedImageHandlerReceivesLeftMiddleAndRightClicks(t *testing.T) {
	ta := textTestApp(t)
	type click struct {
		url   string
		event gpui.ClickEvent
	}
	var clicks []click
	state := newTextViewStateEntity(ta, `Before [<img src="https://example.com/image.svg" width="32" height="32">](https://example.com/image-link) after.`)
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.Div().Block().W(gpui.Px(160)).Child(NewTextView(state).Selectable(true).
			OnLinkClick(func(url string, event *gpui.ClickEvent, _ *gpui.Window, _ *gpui.App) {
				clicks = append(clicks, click{url: url, event: *event})
			})).IntoElement()
	})
	drawText(t, cx)

	b := textViewTextBounds(ta, state)
	if len(b) < 2 {
		t.Fatalf("linked image needs text bounds on both sides: %v", b)
	}
	if b[1].Left()-b[0].Right() < 24 {
		t.Fatalf("linked image did not reserve the expected click target: %v", b)
	}
	position := gpui.Point{X: b[0].Right() + (b[1].Left()-b[0].Right())*0.5, Y: b[0].Top() + 8}
	for _, button := range []gpui.MouseButton{gpui.MouseButtonLeft, gpui.MouseButtonMiddle, gpui.MouseButtonRight} {
		cx.SimulateMouseDown(position, button, gpui.Modifiers{})
		cx.SimulateMouseUp(position, button, gpui.Modifiers{})
	}

	if len(clicks) != 3 {
		t.Fatalf("%d clicks: %+v", len(clicks), clicks)
	}
	for _, c := range clicks {
		if c.url != "https://example.com/image-link" {
			t.Errorf("url %q", c.url)
		}
	}
	if clicks[0].event.IsRightClick() || clicks[0].event.IsMiddleClick() {
		t.Errorf("first click %+v", clicks[0].event)
	}
	if !clicks[1].event.IsMiddleClick() {
		t.Errorf("second click %+v", clicks[1].event)
	}
	if !clicks[2].event.IsRightClick() {
		t.Errorf("third click %+v", clicks[2].event)
	}
}
