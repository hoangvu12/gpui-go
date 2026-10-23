package main

import (
	"strings"
	"time"

	"github.com/hoangvu12/gpui-go/base"
	"github.com/hoangvu12/gpui-go/gpui"
)

var textPages = []page{
	{"text-markdown", "TextView: Markdown", "text/format/markdown.rs", (*gallery).markdownPage},
	{"text-html", "TextView: HTML", "text/format/html.rs", (*gallery).htmlPage},
	{"selectable-text", "SelectableText", "selectable_text.rs", (*gallery).selectableTextPage},
	{"stream-fade", "Streaming fade", "text/stream_fade.rs", (*gallery).streamFadePage},
	{"range-highlight", "Range highlight", "text/range_highlight.rs", (*gallery).rangeHighlightPage},
}

const markdownSample = "# Release notes\n\n" +
	"GPUI Base for Go ships **unstyled** components: they own *behavior*, " +
	"and the application owns every pixel. Read more on " +
	"[GitHub](https://github.com/hoangvu12/gpui-go) or call `base.Init(app)` to start.\n\n" +
	"## What's included\n\n" +
	"- Inputs: `Input`, `Textarea` and a code `Editor`\n" +
	"- Overlays such as **Dialog**, *Popover* and Sheet\n" +
	"  - nested list items keep their indent\n" +
	// A checked task item ("- [x]") draws its tick as an SVG image, and
	// image sprites live in the app image atlas, which the present loop
	// cannot pass alongside the window's glyph atlas: only unchecked here.
	"- [ ] Task list items render a box\n\n" +
	"1. Ordered lists count\n2. from one\n3. upward\n\n" +
	"### Status\n\n" +
	"| Component | Group | Status |\n|---|---|:---:|\n" +
	"| TextView | Text | ported |\n| Plot | Charts | ported |\n| Dock | Layout | ported |\n\n" +
	"> Block quotes render with a rule on the left.\n\n" +
	"```go\nfunc main() {\n    app := gpui.NewApp()\n    base.Init(app)\n    fmt.Println(\"hello, base\")\n}\n```\n\n" +
	"---\n\nThe end of the document. Drag across it to select text.\n"

const htmlSample = `<h2>HTML rendering</h2>
<p>TextView also parses <strong>HTML</strong>: <em>emphasis</em>, <code>inline code</code>,
and <a href="https://github.com/hoangvu12/gpui-go">links</a>.</p>
<ul><li>Unordered item</li><li>Another item with <b>bold</b> text</li></ul>
<ol><li>First</li><li>Second</li></ol>
<blockquote>A quoted paragraph.</blockquote>
<table><thead><tr><th>Tag</th><th>Supported</th></tr></thead>
<tbody><tr><td>table</td><td>yes</td></tr><tr><td>pre</td><td>yes</td></tr></tbody></table>
<pre><code>SELECT name FROM components WHERE unstyled = true;</code></pre>`

var streamChunks = []string{
	"## Streaming answer\n\n",
	"Text appended to a TextView ",
	"fades in as it arrives, ",
	"word by word, ",
	"the way a chat model's answer streams.\n\n",
	"- Each update is compared with the last rendered text\n",
	"- so `**bo` growing into **bold** fades only the new glyphs\n\n",
	"Press **Append** again to keep going, or **Reset** to start over.",
}

const highlightSample = "# Search results\n\n" +
	"GPUI Base is a set of unstyled components. Base owns behavior; the application owns " +
	"every visual. The Go port of Base keeps upstream names and follows the GPUI core.\n\n" +
	"A range highlight paints a background under a byte range of the **rendered** text, " +
	"not of the Markdown source, so formatting markers never shift the result. " +
	"Highlights survive re-layout and scrolling, and Base can reveal a range by scrolling " +
	"the view to its line.\n\n" +
	"- components are unstyled\n- Base is accessible\n- GPUI draws on the GPU\n"

var selectionParagraphs = []string{
	"Selection should feel like a natural part of reading. Start in this paragraph, continue into the next one, and the three runs behave as one document because they share one selection handle.",
	"This second paragraph is long enough to wrap. Drag across the boundary to see one continuous highlight, then copy with Ctrl+C: the copied text follows the reading order.",
	"International text stays predictable when a line mixes café, déjà vu, Kraków, naïve and résumé; UTF-8 byte ranges map back to whole characters.",
}

type textState struct {
	markdown      gpui.Entity[base.TextViewState]
	html          gpui.Entity[base.TextViewState]
	stream        gpui.Entity[base.TextViewState]
	streamNext    int
	highlight     gpui.Entity[base.TextViewState]
	highlightTerm string
	highlightNote string
	selection     base.TextSelectionHandle
}

func (g *gallery) initText(w *gpui.Window, cx *gpui.Context[gallery]) {
	app := cx.App()
	newView := func(build func(cx *gpui.Context[base.TextViewState]) base.TextViewState) gpui.Entity[base.TextViewState] {
		return gpui.NewEntity(app, w.Scope(), func(st *base.TextViewState, tcx *gpui.Context[base.TextViewState]) {
			*st = build(tcx)
		})
	}
	g.markdown = newView(func(cx *gpui.Context[base.TextViewState]) base.TextViewState {
		return base.TextViewStateMarkdown(markdownSample, cx).Selectable(true)
	})
	g.html = newView(func(cx *gpui.Context[base.TextViewState]) base.TextViewState {
		return base.TextViewStateHTML(htmlSample, cx).Selectable(true)
	})
	g.stream = newView(func(cx *gpui.Context[base.TextViewState]) base.TextViewState {
		return base.TextViewStateMarkdown("", cx).Motion(base.DefaultTextViewMotion().
			WithStreamFade(700 * time.Millisecond).
			WithStreamFadeStagger(70 * time.Millisecond))
	})
	g.highlight = newView(func(cx *gpui.Context[base.TextViewState]) base.TextViewState {
		return base.TextViewStateMarkdown(highlightSample, cx)
	})
	g.highlightNote = "Pick a term to highlight every match."

	g.selection = base.NewTextSelectionHandle("", app, w.Scope())
	g.selection.RefreshWindowOnChange(w, app).Detach()
}

// textViewStyle is the rich-text style for the current theme.
func textViewStyle(app *gpui.App) base.TextViewStyle {
	return base.TextViewStyleFromTheme(base.ThemeGlobal(app)).
		WithLink(pal.accent).
		WithCodeBackground(pal.elevated)
}

// textBox frames a scrollable TextView at a fixed height.
func textBox(id string, state gpui.Entity[base.TextViewState], height float32, app *gpui.App) *gpui.DivElement {
	return gpui.Div().ID(id).W(gpui.Px(640)).H(gpui.Px(height)).
		Border1().BorderColor(pal.border).RoundedLg().Bg(pal.surface).OverflowHidden().
		Child(base.NewTextView(state).SizeFull().Px4().Scrollable(true).Style(textViewStyle(app)))
}

func (g *gallery) markdownPage(_ *gpui.Window, cx *gpui.Context[gallery]) any {
	app := cx.App()
	return col(
		demo("Headings, lists, task items, a table, a quote, a code block, links and inline code",
			textBox("markdown-view", g.markdown, 520, app)),
		demo("Inline, clamped to two lines (TextView.MaxLines)", gpui.Div().W(gpui.Px(640)).
			Child(base.TextViewMarkdown("markdown-inline",
				"**Clamped.** "+strings.Repeat("This paragraph keeps going so the clamp has something to cut. ", 6)).
				MaxLines(2).Style(textViewStyle(app)))),
	)
}

func (g *gallery) htmlPage(_ *gpui.Window, cx *gpui.Context[gallery]) any {
	return col(
		demo("Parsed HTML (headings, inline styles, lists, a table, pre)", textBox("html-view", g.html, 420, cx.App())),
	)
}

func (g *gallery) selectableTextPage(w *gpui.Window, cx *gpui.Context[gallery]) any {
	app := cx.App()
	text := gpui.Div().W(gpui.Px(560)).Flex().FlexCol().Gap3().TextSm()
	for ix, paragraph := range selectionParagraphs {
		text = text.Child(base.SelectableTextWithHandle(gpui.NamedIntegerElementID("selectable-paragraph", uint64(ix)), g.selection, paragraph).
			DocumentOrder(uint64(ix)))
	}
	selected := base.TextSelectionSelectedText(w, app)
	active := base.TextSelectionHasSelection(w, app)
	preview := "Drag across any paragraphs to select text."
	if active {
		preview = selected
	}
	return col(
		demo("Three runs sharing one selection handle", text),
		demo("Selection", col(
			gpui.Div().FontWeight(600).Child(map[bool]string{true: "Selection active", false: "No selection"}[active]),
			gpui.Div().W(gpui.Px(560)).TextXs().TextColor(pal.muted).Child(preview),
			row(btn("clear-selection", "Clear selection", outline, sm).OnClick(func(_ *gpui.ClickEvent, w *gpui.Window, app *gpui.App) {
				base.TextSelectionClear(w, app)
			})),
		)),
		demo("An independent run with its own selection colour", gpui.Div().W(gpui.Px(560)).TextSm().
			Child(base.NewSelectableText("independent-run", "This run owns its selection; selecting here clears the document above.").
				SelectionColor(pal.series[2].Opacity(0.35)))),
	)
}

func (g *gallery) streamFadePage(_ *gpui.Window, cx *gpui.Context[gallery]) any {
	stream := g.stream
	this := cx.Entity().Downgrade()
	next := g.streamNext
	appendChunk := func(_ *gpui.ClickEvent, _ *gpui.Window, app *gpui.App) {
		chunk := streamChunks[next%len(streamChunks)]
		if next > 0 && next%len(streamChunks) == 0 {
			chunk = "\n\n" + chunk
		}
		stream.Update(app, func(st *base.TextViewState, tcx *gpui.Context[base.TextViewState]) { st.PushStr(chunk, tcx) })
		set(this, app, func(g *gallery) { g.streamNext = next + 1 })
	}
	reset := func(_ *gpui.ClickEvent, _ *gpui.Window, app *gpui.App) {
		stream.Update(app, func(st *base.TextViewState, tcx *gpui.Context[base.TextViewState]) { st.SetText("", tcx) })
		set(this, app, func(g *gallery) { g.streamNext = 0 })
	}
	return col(
		demo("Append chunks: new words fade in (700 ms, 70 ms stagger)", col(
			row(
				btn("stream-append", "Append", primary, md).OnClick(appendChunk),
				btn("stream-reset", "Reset", outline, md).OnClick(reset),
				note(itoa(g.streamNext)+" chunks appended"),
			),
			textBox("stream-view", g.stream, 260, cx.App()),
		)),
	)
}

func (g *gallery) rangeHighlightPage(_ *gpui.Window, cx *gpui.Context[gallery]) any {
	view := g.highlight
	this := cx.Entity().Downgrade()
	highlight := func(term string) func(*gpui.ClickEvent, *gpui.Window, *gpui.App) {
		return func(_ *gpui.ClickEvent, _ *gpui.Window, app *gpui.App) {
			var noteText string
			view.Update(app, func(st *base.TextViewState, tcx *gpui.Context[base.TextViewState]) {
				if term == "" {
					st.ClearRangeHighlights(tcx)
					noteText = "Highlights cleared."
					return
				}
				rendered := st.RenderedText().String()
				var highlights []base.RangeHighlight
				lower, needle := strings.ToLower(rendered), strings.ToLower(term)
				for from := 0; ; {
					at := strings.Index(lower[from:], needle)
					if at < 0 {
						break
					}
					start := from + at
					highlights = append(highlights, base.NewRangeHighlight(
						gpui.TextRange{Start: start, End: start + len(term)}, pal.series[2].Opacity(0.45)))
					from = start + len(term)
				}
				if err := st.SetRangeHighlights(highlights, tcx); err != nil {
					noteText = "Rejected: " + err.Error()
					return
				}
				if len(highlights) > 0 {
					_ = st.RevealRange(highlights[len(highlights)-1].Range(), tcx)
				}
				noteText = itoa(len(highlights)) + " matches for “" + term + "” (scrolled to the last one)"
			})
			set(this, app, func(g *gallery) {
				g.highlightTerm = term
				g.highlightNote = noteText
			})
		}
	}
	terms := row()
	for _, term := range []string{"Base", "unstyled", "range", "GPU"} {
		v := outline
		if g.highlightTerm == term {
			v = primary
		}
		terms = terms.Child(btn("highlight-"+term, term, v, sm).OnClick(highlight(term)))
	}
	terms = terms.Child(btn("highlight-clear", "Clear", ghost, sm).OnClick(highlight("")))
	return col(
		demo("Highlight every match of a term in the rendered text", col(
			terms,
			note(g.highlightNote),
			textBox("highlight-view", g.highlight, 220, cx.App()),
		)),
	)
}
