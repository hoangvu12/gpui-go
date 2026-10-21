# Slice 06: rich text (TextView / markdown / HTML) and cross-element selection

Upstream: `upstream-base/ce/crates/base/src` @ c0820693 (abbreviated `B:`). Rust core pin: `gpui-go/reference/ce-source/crates/gpui/src` (abbreviated `R:`). Go core: `gpui-go/gpui` (abbreviated `G:`). Native ABI source: `gpui-go/reference/native/src` (abbreviated `N:`). This is a read-only inventory. It agrees with `E1-text-selection.md` and `deps-recommendation.md`, and adds per-subsystem classes and a ticket split.

**Size:** 28,151 Rust lines and 329 `#[test]`/`#[gpui::test]` functions. That is 23,064 lines in `text/**` and 5,087 in the four top-level files. About 10k of the lines are inline tests.

## 0. Headline findings

1. **Base targets a different core text API than our pin.**
   - Base builds against crates.io `gpui-ce 0.2.2` (`ce/Cargo.lock:3057-3060`, registry source), which still has the Zed-classic text system. Our pin is the Parley-based tree, also versioned 0.2.2 (`R:../Cargo.toml:3`).
   - Base uses APIs that **do not exist in R**:
     - `TextSystem::line_wrapper(font, size)` and `wrap_line(&[LineFragment], w)` (`B:text/inline_flow.rs:1177-1179, 1271-1281`)
     - `gpui::LineFragment` (`inline_flow.rs:12`)
     - `WrappedLineLayout{unwrapped_layout, wrap_boundaries}` (`B:text/inline.rs:690-707, 1192-1257`; `B:text_selection.rs:530`)
     - `LineLayout::x_for_index` (`inline.rs:707`, `inline_flow.rs:1411`)
     - the 4-arg `layout_line(.., None)` (`inline_flow.rs:1466`, `node.rs:2616`)
     - `text_system().cap_height(font, size)` (`inline_flow.rs:1126`)
   - A grep of R for `LineFragment|line_wrapper|wrap_line|unwrapped_layout|x_for_index` finds nothing. R has `WrappedLineLayout{layout, wrap_width}` with `visual_lines`, `byte_index_for_pixel_point`, `visual_position_for_byte_index` and `selection_bounds` (`R:text_system/line_layout.rs:631-898`). These map 1:1 onto Go's `*WrappedLine` (`G:text.go:721-1150`: `Line`, `Caret`, `HitTest`, `SelectionRects`, `Clusters`).
   - **Geometry code must be re-expressed, not transliterated.**
2. **Go has no rich-text element and no public paint API for third-party elements.**
   - There is no `StyledText`, `TextLayout` handle, `HighlightStyle` or `img()`. There is only `Text(string)` (`G:element.go:811`), which paints one ambient color (`G:inline.go:907`).
   - `Scene.PaintQuad` (`G:scene.go:521`) and `PaintShapedText` (`G:draw_text.go:734`) need the private frame. The only exported element painter is `PaintSvg` (`G:svgelement.go:383`).
   - Every custom element in this slice is blocked on this: Inline, InlineFlow, InlineObject, the TextView element, SelectableText, the TextSelection layer and touch handles.
   - Layout entry points *are* public: `RequestElementLayout` and `RequestElementMeasuredLayout` (`G:element.go:1530-1536`).
3. **There is no hard native gap.** Per-run paint styles (color, background, underline, strike) are not carried natively, but Go-side workarounds exist (see section 3).
4. **syntect, regex and aho-corasick are not used in this slice.** Code highlighting is a callback, `CodeBlockHighlighterFn = Fn(&CodeBlock) -> Vec<(Range, HighlightStyle)>` (`B:text/text_view.rs:24-28, 50-55`; `node.rs:1841-1857`).
5. **The public API leaks markdown-rs types.** `pub use markdown::mdast as markdown_ast` (`B:text/markdown_ext.rs:21`), and plugins receive `&mdast::Node` (`markdown_ext.rs:28-62, 411-432`). A Go port needs its own mdast-shaped node type.

## 1. Summary table

**Class key:**
- **PURE-GO**: no core change needed.
- **CORE+**: needs a Go-side core addition.
- **NAT**: native gap.

**Missing-API key** (details in section 2):
- **ST**: StyledText, TextLayout and runs
- **HS**: HighlightStyle, Underline and Strikethrough styles
- **WP**: window paint API
- **TS**: window text-system access (shape_line/layout_line)
- **IMG**: `img()` element
- **MIME**: `ImageFormat::from_mime_type`

| Subsystem | Files | Lines | Tests | Class | Missing core APIs | 3rd-party |
|---|---|---|---|---|---|---|
| A. Markdown parse to node tree | format/markdown.rs 2284, markdown_ext.rs 524, document.rs 246, utils.rs 125, format/mod.rs 3 | 3182 | 59 | PURE-GO (MIME is trivial) | MIME (`R:platform.rs:3508`) | markdown-rs (heavy), html5ever `NAMED_ENTITIES` (1 call), data-url (1 call) |
| B. HTML parse | format/html.rs 821, format/html5minify/mod.rs 899 | 1720 | 8 | PURE-GO, **deferrable** | none | html5ever and markup5ever_rcdom (parse plus DOM walk) |
| C. Node tree and block render (headings, lists, tables, code, images, blockquote, hr, task lists) | node.rs 4501, style.rs 380 | 4881 | 43 | CORE+ | IMG, TS (`layout_line` for table column widths, `node.rs:2615`), plus Div styling (overflow-x scroll, borders) from other slices | none |
| D. Inline text render (runs, highlights, links, inline code chips, inline objects and images, own wrapping) | inline.rs 2418, inline_flow.rs 2139, inline_object.rs 485, inline_element.rs 74, inline_virtual_tests.rs 170 | 5286 | 35 | CORE+ (soft native limit) | ST, HS, WP, TS, IMG; LineWrapper (rewrite on WrappedLine or G:inline_real.go) | unicode-segmentation (`split_word_bounds`, `graphemes`) |
| E. TextView element and state (async parse, streaming `push_str`, `max_lines` clamp, scroll, list virtualization, copy, select-all, reveal) | text_view.rs 2575, state.rs 3187, mod.rs 112 | 5874 | 119 | CORE+ | WP (element paint and ContentMask), ST (via D) | none (markdown through A) |
| F. Range highlight and stream fade | range_highlight.rs 1019, stream_fade.rs 642 | 1661 | 12 | PURE-GO, **stream fade deferrable** | none (`G:imagecache.go:958 RequestAnimationFrame` exists) | none (`web_time::Instant` becomes `time.Now`) |
| G. Cross-element selection (registry entity, per-frame registration, projection, copy, drag auto-scroll, word/line selection, touch handles, edit menu) | text_selection.rs 4391, text/selection.rs 172, text/selection_adapter.rs 288, selectable_text.rs 317, touch_selection.rs 297, text_boundary.rs 82 | 5547 | 53 | CORE+ | ST (`TextLayout::position_for_index`/`index_for_position`, used about 40 times), WP (selection quads, `B:selectable_text.rs`, `touch_selection.rs:170`) | none |
| **Total** | 25 files | **28,151** | **329** | | | |

### Core APIs used vs. Go presence

Pin locations are `R:`, Go locations are `G:`.

**Missing from Go:**

| API used by Base | Pin location | Go status |
|---|---|---|
| `StyledText::new`, `with_runs`, `with_highlights`, `layout()` | `R:elements/text.rs:391-540` | **missing** |
| `TextLayout`: `position_for_index`, `index_for_position`, `line_layouts`, `bounds`, `line_height`, `len`, `text` | `R:elements/text.rs:618, 1063-1137` | **missing**. The backing exists (`G:text.go:876 Caret`, `:928 HitTest`, `:801 Line`) |
| `HighlightStyle`, `UnderlineStyle`, `StrikethroughStyle`, and the paint fields of `TextRun` | `R:style.rs:751`; `R:text_system.rs:491-503` | **missing**. `G:text.go:453 TextRun` has only `Len`, `Font` and `LetterSpacing` |
| `window.text_system().shape_line`, `layout_line`, `resolve_font` | `R:text_system.rs:325, 375, 129`; `R:window.rs:2373` | **partial**. `G:text.go:403, 483` are on `*TextSystem`, which elements cannot get from a `Window`. `G:testwindow.go:548 Window.ShapeText` returns facts only |
| `window.paint_quad`, `paint_underline`, `paint_strikethrough`, `paint_glyph`, `paint_image`, `ShapedLine::paint`/`paint_background` | `R:window.rs:4634, 4732, 4771, 4806, 5057`; `R:text_system/line.rs:34, 58` | **missing publicly**. Only `G:scene.go:521` and `G:draw_text.go:734, 813` exist, behind the private frame |
| `img()`, `StyledImage`, `ObjectFit`, `ImageSource::{Image, Resource}` | `R:elements/img.rs:148, 201` | **element missing**. Present: the image cache (`G:imagecache.go:210, 751`), `NewImage` (`G:image.go:55`) and `ObjectFit` (`G:capture.go:582`) |
| `ImageFormat::from_mime_type` | `R:platform.rs:3508` | missing (trivial) |
| `deferred`/`anchored` (edit-menu overlay) | `R:elements/` | missing. These belong to the overlays slice |

**Present in Go:**

| API used by Base | Pin location | Go status |
|---|---|---|
| `cap_height`, ascent/descent | `R:text_system.rs:971` | `G:text.go:258 FontMetrics.CapHeight` |
| `ClipboardItem::new_string`, `write_to_clipboard` (`B:text/text_view.rs:687`) | `R:platform.rs:3329`; `R:app.rs:1529` | `G:clipboard.go:180, 434` |
| `insert_hitbox`, `on_mouse_event`, MouseDown/Move/Up (with click count), `ScrollWheelEvent`/`ScrollDelta`, `TouchPhase` | `R:window.rs:5426, 5578`; `R:interactive.rs:88, 554` | `G:mouseevent.go:102-164, 459, 477` |
| `ListState` and `list()` (`B:text/document.rs`, `state.rs`) | `R:elements/list.rs` | `G:listelement.go:134, 713` |
| `ScrollHandle`, `FocusHandle`, `KeyBinding`, actions, `Global`, `Event`/`Subscription`, `WeakEntity` | | `G:scroll.go:63`, `keymap.go`, `app.go`, `event.go:36-145`, `entity.go` |
| `Role` (`B:inline_object.rs`) | `R:gpui.rs:96` | `G:element.go:292` |
| `pixel_snap`, `rem_size`, `with_content_mask`, `request_animation_frame` | `R:window.rs:2989, 2920, 3880, 2633` | `G:element.go:1226, 1251, 1277`; `G:imagecache.go:958` |
| `TextAlign`, `WhiteSpace`, `TextStyle`/`TextStyleRefinement` | | `G:div.go:70-140`. Narrow: no underline, strike or background in `TextStyle` |

## 2. Core additions needed (Go-side; no native code)

Listed in dependency order.

1. **Exported element paint API (WP).** Add `Window.PaintQuad(PaintQuad)` (`R:window.rs:4634`), `PaintUnderline`/`PaintStrikethrough` (`R:window.rs:4732, 4771`; Go has `Scene.InsertUnderline`, `G:scene.go:365`) and `PaintImage` (`R:window.rs:5057`; atlas plus `InsertPolychromeSprite`, `G:draw_text.go:176`). Each wraps `currentFrame(w).scene` and `.paint`, the way `PaintSvg` does (`G:svgelement.go:383`). This is cross-slice; every Base custom element needs it.
2. **Window text-system access (TS).** Add `WindowTextSystem(w)` (`R:window.rs:2373`) that returns the frame's `*TextSystem`, and/or `ShapeLine(text, size, runs)` returning a `*WrappedLine` (`R:text_system.rs:325, 375`). Today only `G:div.go:264 windowTextSystemOf` (unexported) and the facts-only `G:testwindow.go:548` exist.
3. **Paint-style runs (HS).** Add a `TextRunStyle{Color, Background *Hsla, Underline *UnderlineStyle, Strikethrough *StrikethroughStyle}` overlay next to `G:text.go:453 TextRun`, plus `HighlightStyle` and its `Highlight`/refine semantics (`R:style.rs:751`). Paint is Go-side: see section 3 for how colors are recovered from native geometry.
4. **StyledText and TextLayout (ST).** Port `R:elements/text.rs:391-1150`:
   - `StyledText` with `with_runs`/`with_highlights`, and a `TextLayout` handle that keeps the prepainted `*WrappedLine` and its bounds.
   - `position_for_index` maps to `WrappedLine.Caret` (`G:text.go:876`). `index_for_position` maps to `HitTest`/`ByteIndexForPixelPoint` (`:928, :961`).
   - Base's `wrap_boundaries`/`x_for_index` loops (`B:inline.rs:690-707, 1192-1257`; `B:text_selection.rs:521-530`) become `WrappedLine.Line(i)` (TextStart/TextEnd/AdvanceWidth) plus `Caret`.
   - Base never uses `InteractiveText` (the only hit is a comment, `B:format/markdown.rs:351`), so skip it.
5. **`img()` element (IMG).** Port `R:elements/img.rs:148-201`: an ObjectFit-sized element over `UseImageAsset` (`G:imagecache.go:751`) or an in-memory `Image`. Add `ImageFormat` from MIME (`R:platform.rs:3508`). This is shared with other slices (avatar and so on).
6. **No LineWrapper port.** `R` has no LineWrapper, so do not add one. Re-express `B:inline_flow.rs:1170-1310, 1435-1490` (wrap fragments with atomic element widths) in one of two ways:
   - on the existing Go inline-assembly algorithm (`G:inline_real.go:157 layoutInline`, which places atomic boxes between greedy shaped segments; `R:text_system.rs:367 layout_inline`, `523 InlineBoxRequest`), or
   - Base-locally, on `ShapeText` with a WrapWidth budget.

   Either path is Go-side.

## 3. Native gaps

**None hard.** Evidence for the near-miss:

- **What the native service exposes** (`gpui-go/internal/native/text.go`, `TextService`): `Shape` (:925), `Relayout` (:1069), `LayoutInfo`, `LineCount`/`Line` (:1150-1167), `Caret` (:1203), `HitTest` (:1249), `SelectionRects` (:1280), `Cluster` (:1330), `Fragments` (:1363), `Glyphs` (:1409), `ResolveFont`/`FontMetrics` (:815, :887). That covers everything selection and hit-testing needs.
- **Soft limit: no per-run paint style.**
  - `TextRunSpec` carries only font, features, fallbacks and letter-spacing (`internal/native/text.go:415-431`). The native side builds `TextRun{.., ..TextRun::default()}` (`N:text.rs:1099-1109`).
  - Fragments split only on font, and records have no style or byte range (`N:text.rs:517-533`). Glyphs have no byte index (`G:text.go:646`).
  - Pin's `PaintFragment.style` and underline/strike offsets (`R:text_system/line_layout.rs:420-435`) therefore do not cross the ABI.
- **Workarounds, all Go-side:**
  - (a) Shape per paint-style segment. `G:inline_real.go:445, 472` already does this for inline. It loses kerning and ligatures across style boundaries.
  - (b) Shape once, then paint the line once per distinct color, clipped to that run's `SelectionRects` (`WithContentMaskVoid`, `G:element.go:1251`). This is bidi-safe and keeps cross-run shaping.
  - Backgrounds and range highlights come from `SelectionRects`. Underlines use `FontMetrics.UnderlinePosition`/`Thickness` (`G:text.go:253-256`). There is no strikeout metric, so approximate it as `x_height/2` and record the deviation.
  - **Recommend (b) for StyledText**; keep (a) where inline boxes already split segments.
- **Clipboard, images and mouse/touch** are all already wired natively (clipboard_windows, image service, directmanipulation).

## 4. Third-party usage (exact surface)

### markdown (markdown-rs 1.0, `features=["serde"]`; serde not used in this slice)

- **Entry points:**
  - `markdown::to_mdast(source, &ParseOptions)` (`B:format/markdown.rs:23, 78`)
  - `ParseOptions::gfm()`, mutating `constructs.{frontmatter, math_text, math_flow, html_flow, html_text, mdx_expression_flow/text, mdx_jsx_flow/text}` (`B:markdown_ext.rs:392-409`)
  - `unist::Point` and `node.position()` start/end offsets
- **Options in effect:**
  - GFM always: tables, strikethrough, autolink literals, task lists, footnotes.
  - `math_text` and `math_flow` always on. Unclaimed `$..$` spans are re-parsed with math off, then spliced back with shifted positions (`markdown.rs:23-90`).
  - Frontmatter is opt-in (`enable_frontmatter`).
  - MDX is opt-in; it turns raw HTML off.
- **mdast nodes consumed** (`markdown.rs:695-1093`):
  - Block: Root, Paragraph, Blockquote, List (ordered, start, spread), ListItem (checked, spread), Code (lang, meta), Heading (depth), Math, Html, ThematicBreak, Table (align: AlignKind), TableRow, TableCell, FootnoteDefinition, Definition, Yaml, Toml, MdxFlowExpression, MdxJsxFlowElement, MdxJsxTextElement.
  - Inline: Text, Emphasis, Strong, Delete, InlineCode, Link (url, title), Image (url, alt, title), Break, InlineMath, MdxTextExpression, Html (sent to the HTML parser), FootnoteReference, LinkReference (identifier, label).
- **Not consumed:** ImageReference.
- **Hard requirement: an exact byte span on every node.** Selection's `SelectionFormat::Source` (`B:state.rs:74-85`) and the source-segment alignment (`markdown.rs:263, 382-440`) depend on it. There are about 55 `position` uses in markdown.rs, and many of the 57 tests assert source ranges.
- **Public surface:** `markdown_ast` re-export; plugin parsers take `&mdast::Node` plus `MarkdownParseContext{source, offset, node_source}` (`markdown_ext.rs:78-105`).
- **Go options:**
  - (i) goldmark plus an adapter into a Go `mdast` type. Goldmark has GFM and footnotes built in. Math and frontmatter would be hand-written extensions. **No MDX.** Inline nodes lack end positions, so spans must be rebuilt from segments.
  - (ii) Hand-port markdown-rs. It is very large, but it gives exact mdast parity.
  - **Recommendation:** go with (i) behind a seam, defer MDX, and convert the tests at the level of Base nodes and spans.

### html5ever 0.27 + markup5ever_rcdom 0.3

- **`html.rs`:**
  - `parse_document(RcDom::default(), ParseOpts::default()).from_utf8().read_from(cursor)` (`B:format/html.rs:58-69`)
  - `NodeData::{Document, Element{name.local, attrs}, Text{contents}}`
  - `local_name!` and `html5ever::Attribute`
- **Tags consumed:**
  - Inline: a, b, strong, i, em, u, s, del, mark, code, br, img.
  - Block: p, div-like containers (the list at `html.rs:~28-53`: header, main, nav, section, pre, hr and so on), h1-h6, ul, ol, li, blockquote, table, thead, tbody, th, td.
  - script, style and title are dropped.
- **Attributes consumed:** href, title, src, alt, width, height, and `style` (a hand-rolled `k:v;` parser covering width, height and color, `html.rs:143-205`). `mark` color uses named colors or hex (via `color_picker::parse_hex`).
- **`html5minify` (fork):** reads `parse_document` into RcDom, then serializes a minified form using `QualName`, `Attribute`, `Tendril`. Its only consumer is `cleanup_html` (`html.rs:83-93`).
- **`markdown.rs:567`:** `html5ever::data::NAMED_ENTITIES` for entity decoding. Go's stdlib `html.UnescapeString` covers it.
- **Go:** `golang.org/x/net/html` (`html.Parse`, `Node{Type, DataAtom, Attr}`), or vendor it into `internal/`. The minifier is about 750 lines of pure DOM walk, ported directly. This whole area is **deferrable**: it is only reached by `TextView::html` and inline HTML inside markdown (`markdown.rs:814, 980`). Fallback: render inline HTML as text.

### Smaller crates

- **data-url 0.3:** one call site, `DataUrl::process`, `.mime_type()` and `.decode_to_vec()` (`B:text/utils.rs:60-66`). Go: about 40 lines of stdlib (`strings`, `encoding/base64`, `net/url`). Hand-port it; no module needed.
- **unicode-segmentation 1.12:** `split_word_bounds()` and `graphemes(true)` (`B:inline_flow.rs:7, 1483-1488`), used for wrap opportunities inside code and link spans. Go: `clipperhouse/uax29/v2` (`words` plus `graphemes`), or reuse native `WrappedLine.Clusters()` (`G:text.go:1110`, which gives graphemes) and add a small hand-ported word-boundary routine. `text_boundary.rs` uses only an ASCII/Latin/Cyrillic char-class table (`B:text_boundary.rs:12-25`), which is pure Go.
- **syntect, regex, aho-corasick:** **0 uses in this slice.** syntect appears in input/editor and color_picker only (slice D).
- **Other:** web_time (becomes `time`), futures and async channels (TextViewState background parse, `B:state.rs:20-22`, which becomes a goroutine plus `Task`), smallvec and itertools (not needed).

## 5. Core vs. deferrable

- **Core:**
  - Markdown TextView: parse (A), node render (C), Inline and InlineFlow (D) with runs, links, inline code and images.
  - TextViewState: set_text, push_str streaming, selection, copy, select-all, max_lines, scroll and list (E).
  - Mouse selection plus SelectableText (G). Word/line double and triple click.
  - Range highlight (F), used by search and reveal.
- **Deferrable:**
  - HTML format and the minifier (B: 1720 lines, 8 tests).
  - Stream fade (`stream_fade.rs`: 642 lines, 6 tests). `TextViewMotion` can be a no-op at first.
  - MDX constructs.
  - Touch handles and the touch edit menu (`touch_selection.rs`, plus the touch paths in `text_selection.rs` around :895-960 and :1593-1640). Windows touch is rare.
  - Math rendering. Math already falls back to a code block (`markdown.rs:973-998`), so this only means a math plugin.

## 6. Suggested sub-ticket split (dependency order)

1. **T1 Core: rich-text foundations (Go core, cross-slice).**
   - Exported `Window` paint API (quad, underline, strike, image, shaped text).
   - `WindowTextSystem`/`ShapeLine`.
   - `HighlightStyle` plus a paint-style run overlay with the clip-by-`SelectionRects` color painter.
   - `StyledText` plus a `TextLayout` handle on `*WrappedLine`.
   - `img()` and `ImageFormat` from MIME.

   About 1.5-2.5k Go lines. Unblocks everything else. Tests: port `R:elements/text.rs` and `img.rs` tests, plus golden scenes.
2. **T2 Markdown parse layer (A), PURE-GO.**
   - A Go `mdast` package (the node types listed in section 4, each with a byte `Position`).
   - A goldmark adapter with math and frontmatter extensions, or a markdown-rs port; decide in this ticket.
   - `format/markdown.rs`, `markdown_ext.rs` (plugin API), `document.rs`, `utils.rs` (data URL, list prefixes).
   - Exit criterion: the 57 markdown tests, converted to node-tree and span assertions.
3. **T3 Inline rendering (D), needs T1.**
   - `inline.rs` (runs, highlights, link hit and hover, selection and range paint), `inline_object.rs`, `inline_element.rs`.
   - `inline_flow.rs` re-expressed on `WrappedLine` and the `inline_real.go` style of box placement (no LineWrapper).
   - Word bounds via uax29 or a hand port.
   - 35 tests. The inline.rs tests use a fake PlatformTextSystem (`B:inline.rs:1783-1925`); Go needs a fake-shaper seam or must run against the real native service.
4. **T4 Node tree and TextView (C+E), needs T1-T3.**
   - `node.rs` block render (tables measured with `ShapeLine`, code blocks with the highlighter callback, task lists, images), `style.rs`.
   - `text_view.rs`, `state.rs` (async parse, streaming append, max_lines, list virtualization, copy and select-all actions, link click), `mod.rs`.
   - About 10.7k lines and 162 tests. This is the largest ticket; split C and E if needed.
5. **T5 Cross-element selection (G), needs T1 and T4's registration points.**
   - `text_selection.rs` (handle entity, registry, projection, auto-scroll, word/line via `text_boundary.rs`), `selection.rs`, `selection_adapter.rs`, `selectable_text.rs`.
   - Touch: `touch_selection.rs` and the edit-menu path, optionally as T5b.
   - 53 tests.
6. **T6 Range highlight and stream fade (F), needs T4.**
   - `range_highlight.rs` (core: RenderedText, reveal).
   - `stream_fade.rs` (deferrable; needs the motion slice's `Easing`/`Timing`).
   - 12 tests.
7. **T7 HTML format (B), deferrable, needs T2's node types.**
   - `x/net/html` (or a vendored copy) plus the html5minify port and `html.rs`.
   - Until it lands, inline HTML in markdown renders as text.
   - 8 tests.

**Intra-crate dependencies to schedule first** (from other slices):
- motion::Easing/Timing
- AutoScroll
- GlobalState
- StyledExt, h_flex/v_flex
- scrollable_mask::horizontal_scroll_area
- theme::ActiveTheme
- input::{Copy, SelectAll} actions
- async_util
- color_picker::parse_hex (only `html.rs` uses it)
