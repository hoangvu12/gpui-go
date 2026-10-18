# E1: TextView, markdown/HTML formats, and cross-element text selection

Upstream: gpui-component @ c0820693, `crates/base/src` (crate `gpui_ce_components_base`, lib `gpui_base`). Go: gpui-go @ 2fb9ee2, package `gpui`. This is read-only research. Paths without a prefix are relative to `upstream-base/ce/crates/base/src` or `gpui-go/gpui`.

Slice total: **28,151 Rust lines**. About **17.8k are non-test code** and about **10.4k are inline tests** (329 `#[test]`/`#[gpui::test]` fns).

---

## 0. Headline findings (read first)

1. **Base targets a different core API than our pin.** Base's lockfile pins `gpui-ce 0.2.2` **from crates.io** (`ce/Cargo.lock:3057-3060`, `source = registry`). Our pin is git `254b5dbd` (the Parley backend, also versioned 0.2.2). The crates.io crate still uses the Zed-classic text system, and base uses parts of it that **do not exist at 254b5dbd**:
   - `TextSystem::line_wrapper(font, size)` and `LineWrapperHandle::wrap_line(&[LineFragment], width)` (`text/inline_flow.rs:1177-1179, 1271-1281`)
   - `gpui::LineFragment` (`inline_flow.rs:12`)
   - `TextLayout::line_layouts()`, which returns `WrappedLineLayout{unwrapped_layout, wrap_boundaries}` (`text/inline.rs:690-707, 1192-1257`, `text_selection.rs:524`)
   - `LineLayout::x_for_index` (`inline.rs:707`, `inline_flow.rs:1411`)
   - a 4-arg `layout_line(text, size, runs, None)` (`inline_flow.rs:1466`, `node.rs:2616`)
   - `TextSystem::cap_height(font_id, size)` (`inline_flow.rs:1126`)

   A grep for `LineFragment|line_wrapper|wrap_line|unwrapped_layout` in `reference/ce-source/crates/gpui/src` finds nothing. The pinned equivalents are `WrappedLineLayout::visual_lines()` / `fragments_for_line()` / `byte_index_for_pixel_point` / `visual_position_for_byte_index` (`text_system/line_layout.rs:631-886`). **Every geometry routine in inline.rs, inline_flow.rs and text_selection.rs must be re-expressed, not transliterated.** This is the main porting risk in the slice.
2. **Go has no rich-text element.** There is no `StyledText`, `InteractiveText`, `TextLayout` handle or `HighlightStyle`/`StrikethroughStyle`. Only `Text(string)` (`element.go:811`) and the private `textElement` (`div.go:403`), which paints with one ambient color. The usable geometry primitive is `*WrappedLine` (`text.go:721`): `Caret`, `HitTest`, `ByteIndexForPixelPoint`, `SelectionRects`, `Line`, `Fragments`, `Glyphs`, `Clusters` (`text.go:801-1110`).
3. **Third-party elements have no public paint API in Go.** There is no `Window.PaintQuad`, paint text, paint underline/strikethrough or paint image. `Scene.PaintQuad` (`scene.go:521`) needs the private frame (`frame.scene`, `frame.paint`; see `div.go:363, 1660-1670`). The only public element painter is `PaintSvg` (`svgelement.go:383`). There is also **no `img()` element** in Go. This blocks every custom element in the slice (InlineFlow, TextView element, SelectableText, the TextSelectionLayer touch handles). It is a cross-slice MISSING-GO.
4. **Per-run paint styles are MISSING-NATIVE for exact parity.**
   - The native `TextRunRecord` has only font, weight, style, features and letter-spacing (`internal/native/text.go:242-255`).
   - The native code builds `TextRun{.., ..TextRun::default()}` (`reference/native/src/text.rs:1099-1109`), so color, background, underline and strikethrough are always default.
   - `GpuiGoTextFragmentRecord` carries no style, run index or decoration offsets (`reference/native/src/text.rs:518-533`). Pinned `PaintFragment` has `style`, `underline_offset` and `strikethrough_offset` (`ce-source/.../line_layout.rs:407-435`).
   - `GpuiGoTextGlyphRecord` carries no source byte index (`text.rs:543-554`).
   - `FontMetrics` has no strikeout position or size (`text.go:244-272`). Parley uses `run_metrics.strikethrough_offset` (`gpui_ce_parley/src/text_system.rs:1257-1264`).

   Go can approximate these without new native code: `inline_real.go` already shapes per style segment, run-range quads can come from `SelectionRects`, and underlines can use `FontMetrics.UnderlinePosition`. The cost is lost cross-run shaping (kerning and ligatures across style boundaries) and a guessed strikethrough offset. **Decision needed**: accept the deviation, or treat it as a blocker.
5. **syntect is not used by base `src`.** It appears only in `Cargo.toml` and `examples/showcase/syntect_highlighter.rs`. Code highlighting is a user callback, `CodeBlockHighlighterFn = Fn(&CodeBlock) -> Vec<(Range, HighlightStyle)>` (`text/text_view.rs:27-28`). No Go highlighter dependency is needed.
6. **Markdown is mdast-shaped in the public API.** `pub use markdown::mdast as markdown_ast` and `MarkdownPlugin::parse(&mdast::Node, ..)` (`text/markdown_ext.rs:19-62`). Parse options are GFM plus math (text and flow), optional frontmatter, and **optional MDX** (`markdown_ext.rs:392-409`). Source mapping depends on exact node positions: about 90 position/offset uses in `format/markdown.rs`, which has 57 tests, most on source alignment.

---

## 1. Per-module table

Columns:
- **Non-test** is Rust lines before the first `mod tests`.
- **Go size** is S <300, M <1000, L <3000, XL above that.
- **Base deps** are crate-internal modules outside this slice.

| Module | LOC (non-test) | Public items (main) | Tests | Ext crate deps | Base deps | Go size |
|---|---|---|---|---|---|---|
| text/mod.rs | 112 (106) | `markdown()`, `html()`, `Text` enum, re-exports, `init` | 0 | – | – | S |
| text/text_view.rs | 2575 (860) | `TextView` (builder: markdown/html/new, style, selectable, code_block_highlighter, link handler, plugins), `TextViewDefaults` (Global), `TextViewFormat` | 36 (31 gpui) | data_url, markdown | StyledExt, GlobalState, TextSelection, input::Copy, h_resizable/resizable_panel (tests), init | L |
| text/state.rs | 3187 (1283) | `TextViewState` entity (set_text/push_str streaming, async parse, selection, list_state, range highlights, reveal), `SelectionFormat`, `LineSpan` | 83 (78 gpui) | futures, markdown, web_time | AutoScroll, ElementExt, TextSelection, async_util (unbounded), input::{SelectAll}, motion::Easing | L |
| text/document.rs | 246 | `ParsedDocument`, `NodeRenderOptions`; renders root via `list(ListState)` | 0 | – | – | S |
| text/node.rs | 4501 (3437) | `BlockNode`/`InlineNode`/`Paragraph`/`Table`/`CodeBlock`/`TableData`/`Span`/`TextMark`/`LinkMark`; per-node render; markdown reconstruction for copy | 37 | markdown (mdast), tracing | StyledExt, h_flex, scrollable_mask::horizontal_scroll_area, theme::ActiveTheme | XL |
| text/inline.rs | 2418 (1364) | `Inline` element (StyledText + highlights + hitbox + link click/hover + selection paint), `combine_highlights`, `text_runs`, `text_size_ranges`, glyph boxes, row bounds | 20 | anyhow | touch_selection::caret_line_box | L |
| text/inline_flow.rs | 2139 (1644) | `InlineFlow` element: mixed text, inline objects and images with its own wrapping (LineWrapper + fragments), baseline alignment, inline-code chips, link hit regions | 10 | unicode_segmentation | – | L |
| text/inline_object.rs | 485 (410) | `InlineObject`, `MeasuredInlineObject` (atomic inline elements; a11y Role::GenericContainer) | 3 | – | – | M |
| text/inline_element.rs | 74 | `InlineElement`, `InlineRenderContext` | 0 | – | – | S |
| text/inline_virtual_tests.rs | 170 (test-only) | – | 2 | markdown | init | (tests) |
| text/markdown_ext.rs | 524 (~460) | `MarkdownPlugin` trait, `MarkdownExtensions`, `MarkdownNode`, `MarkdownParseContext`, `markdown_ast` re-export, block/inline parser and renderer fn types | 0 (+test helper) | markdown | – | M |
| text/format/markdown.rs | 2284 (1084) | `parse()`: mdast to document; math flattening/re-parse; source segments/alignment; entity decoding | 57 | markdown, html5ever (NAMED_ENTITIES only, :567), tracing | – | L (excl. parser) |
| text/format/html.rs | 821 (673) | `parse()`: html5ever DOM to document; inline CSS color parse | 6 | html5ever, markup5ever_rcdom, tracing | color_picker::parse_hex | M |
| text/format/html5minify/mod.rs | 899 (762) | HTML minifier over RcDom (whitespace collapse, attribute/void handling) | 2 | html5ever, markup5ever_rcdom | – | M |
| text/range_highlight.rs | 1019 (683) | `RangeHighlight`, `RangeHighlightError`, `RenderedText`, `RenderedIndex`, `LeafRemap`, `PendingReveal`, `RevealRequest` | 6 | markdown, web_time | – | M |
| text/stream_fade.rs | 642 (562) | `TextViewMotion` (stream-fade duration/stagger/easing), `StreamFadeTracker`, `TextLeafKey` | 6 | web_time | motion::Easing | M |
| text/style.rs | 380 (278) | `TextViewStyle` (+ heading/code/table/link styling) | 6 | – | – | S |
| text/selection.rs | 172 (112) | `CharType` word classes | 2 | – | – | S |
| text/selection_adapter.rs | 288 | `TextViewSelectionAdapter` (virtual-block endpoint cache to TextSelectionHandle) | 0 | – | – | S |
| text/utils.rs | 125 (68) | `data_url_image`, list prefixes/ordinals | 2 | data_url | – | S |
| text_selection.rs | 4391 (2792) | 97 pub items: `TextSelection` statics (selected_text/has/clear/end/select_all/edge drag/activate_scope/touch), `TextSelectionLayer` element, `TextSelectionHandle`, `TextSelectionRegistration`/`Run`/`Projection`/`Snapshot`/`Endpoint`/`ScopeId`/`ContentKey`/`Coverage`/`Event` | 45 | – | AutoScroll, GlobalState, touch_selection, text_boundary | XL |
| touch_selection.rs | 297 (239) | `SelectionEdge`, `TouchSelectionSnapshot`, `TouchHandle` (hit_bounds/paint), `EdgeDrag`, `caret_in_view`, `caret_line_box` | 3 | – | – | S |
| selectable_text.rs | 317 (239) | `SelectableText` element (plain selectable string, PaintQuad highlight) | 3 | – | TextSelection* | S |
| text_boundary.rs | 82 | `word_range_at`, `line_range_at` | 0 | – | – | S |

What the tests cover:
- **text_selection**: scope stacks and re-entrancy, per-window isolation, double/triple-click word and line expansion, multi-run projection, Unicode and reversed ranges, events, auto-scroll and touch.
- **state**: stream fades (push_str, staggering, reduced motion, timers), async parse settling and range-highlight reveal.
- **text_view**: inline plugins, hover cards, drag/copy across formulas, data-URL images, table drag, constructor defaults.
- **node**: copy reconstruction to markdown, inline-object atomic copy, table column metrics, highlighter cache.
- **format/markdown**: source alignment and selected-source-range mapping, math, entities, soft breaks.
- **inline / inline_flow**: wrapped-row highlight geometry, RTL range boxes, alignment, baseline sharing across font sizes, image intrinsic sizing, and inline code wrapping.

**Test-harness hazard**: about 15 inline/node tests install a fake `PlatformTextSystem` (`WideMonoTextSystem`, `inline.rs:1765-1925`) via `TestApp::with_text_system`. Go has no public text-system injection, only the fixed private `testTextSystem` (`testwindow.go:103`). Expected pixel values must be re-derived against Go's test system or real DirectWrite.

---

## 2. Core API matrix (Q1 and Q3)

Status values: PRESENT, MISSING-GO (native supports it or no native involvement) and MISSING-NATIVE (frozen DLL lacks it).

| Upstream core API used | Where used | Go status |
|---|---|---|
| `StyledText::new(..).with_runs(runs)` | inline.rs:875-885, 1482 | **MISSING-GO**. No StyledText. Nearest is `TextSystem.ShapeText(TextLayoutRequest)` text.go:483 plus private `textElement` |
| `TextLayout` handle: `index_for_position`, `position_for_index`, `line_height`, `line_layouts()` | inline.rs:427-767, 1308; text_selection.rs:371-527 (`TextSelectionRun{layout: TextLayout}`) | **MISSING-GO** (no element-level layout handle). Substitutes: `WrappedLine.HitTest` text.go:928, `ByteIndexForPixelPoint` :961, `Caret` :876, `Line` :801 |
| `wrap_boundaries` / `unwrapped_layout` / `x_for_index` | inline.rs:690-707, 1192-1257 | **Absent at pin** (API skew, §0.1). Re-express with `WrappedLine.Line(i)` (TextStart/TextEnd/AdvanceWidth) + `Caret` x |
| `InteractiveText` (on_click/on_hover/tooltip) | not used; base hand-rolls via hitbox + on_mouse_event (inline.rs:1024-1135) | n/a. Hand-rolled path needs the hooks below |
| `HighlightStyle` (color/bg/underline/strikethrough/font weight/style), `combine_highlights` | inline.rs:70-160, node.rs, CodeBlockHighlighterFn | **MISSING-GO** type. Painting needs per-run paint (row below) |
| Per-run color / background / underline / strikethrough in `TextRun` | inline.rs `text_runs` | `TextRun` is font + letter-spacing only (text.go:453-462). Background quads and underline are Go-paintable (`Underline` scene.go:92, `UnderlineStyle` paint.go:284) once a public paint API exists. **Glyph color per run and the strikethrough metric are MISSING-NATIVE** (§0.4). Go workaround: shape per color segment (as inline_real.go:21-31) |
| `StrikethroughStyle` | inline.rs | **MISSING-GO** type. Thickness/offset metric is **MISSING-NATIVE** |
| `window.text_system().shape_line(text, size, runs, None)` returning `ShapedLine` (width, paint, descent) | inline_flow.rs:1599-1605, inline_object.rs:62 | **MISSING-GO** (window-scoped). `DefaultTextSystem()` text.go:364 + `ShapeText` give a `*WrappedLine` but there is no public paint (`Scene.PaintShapedText` draw_text.go:734 needs private frame). `Window.ShapeText` testwindow.go:548 returns facts only |
| `line_wrapper(font, size).wrap_line(&[LineFragment::{text, element(width,len)}], width)` | inline_flow.rs:1177-1281 | **Absent at pin**. Box-aware shaping is **MISSING-NATIVE** (inline.go:40-45, inline_real.go:14-19). Go route already exists: the Go greedy row/box algorithm `realWindowTextSystem.layoutInline` (inline_real.go:157) behind private `inlineLayoutRequest`. Exposing it publicly is **MISSING-GO** |
| `resolve_font` + `cap_height(font, size)` | inline_flow.rs:1125-1126 | PRESENT via `TextSystem.ResolveFont` text.go:403 + `FontMetrics.CapHeight` text.go:244ff (scale manually; no `CapHeightPx` helper) |
| `window.line_height()` | inline.rs:578, inline_flow.rs:340 | **MISSING-GO** as a Window method. Derivable: `WindowTextStyle(w)` element.go:1154 + `LineHeightPixels` div.go:173 + `RemSize` element.go:1226 |
| `window.insert_hitbox(bounds, Normal/BlockMouse)` + `hitbox.is_hovered` | inline.rs:927, text_view.rs:764, selectable_text.rs:190, text_selection.rs:904 | PRESENT `Window.InsertHitbox` mouseevent.go:477, `Hitbox.IsHovered` :249, `HitboxBehavior` :194 |
| `window.on_mouse_event::<MouseDown/Move/Up/ScrollWheel>` (window-level, phased) | text_selection.rs:2635-2741, inline.rs:1024-1135 | PRESENT `Window.OnMouseEvent(func(event any, phase, w, app))` mouseevent.go:459 (type-switch on `event`); `DispatchPhase` key_dispatch.go:45 |
| `window.set_cursor_style(IBeam/PointingHand, &hitbox)` | inline.rs:980-987 | **MISSING-GO**. Only global `App.SetCursorStyle` app.go:993; no hitbox-scoped cursor, no div cursor style |
| `window.with_element_state` | text_selection.rs:2586 | PRESENT `WithElementState` element.go:1379 |
| `window.request_layout(Style, [])` | text_selection.rs:2541 | PRESENT `RequestElementLayout` element.go:1530 |
| `window.defer(cx, ..)` | text_selection.rs:2605 | **MISSING-GO** on Window. `App.Defer` app.go:326 / `Context.Defer` exist (re-acquire window via handle) |
| `window.mouse_position()` | text_selection.rs:2617, 2755 | PRESENT `Window.TrackedMousePosition` mouseevent.go:531 (also `MousePosition` window.go:296, platform/error) |
| `window.window_handle().window_id()` | text_selection.rs:2464+ | PRESENT `Window.ID` window.go:226 |
| `cx.write_to_clipboard(ClipboardItem::new_string)` | text_view.rs:687 | PRESENT `App.WriteClipboard` clipboard.go:434, `NewClipboardStringItem` clipboard.go:180 |
| Globals (`set_global`, `global_mut`) | text_selection.rs:2594-2596, GlobalState | PRESENT `App.SetGlobal` app.go:561, `Global[T]` app.go:575 |
| `EventEmitter` / `cx.emit` / `subscribe` | text_selection.rs:603-760 | PRESENT `Event[S,P].Emit/Subscribe` event.go:36,111 |
| `cx.spawn` / `background_spawn` / `background_executor().now()` | state.rs:178-232, 662 | PRESENT `App.Spawn` executor.go:742, `BackgroundExecutor.Spawn` :663, `App.Now` |
| `ListState::new(n, Top, overdraw).measure_all()`, `list()`, `ListOffset` | state.rs:251, document.rs:191 | PARTIAL. `NewListState(count, overdraw)` listelement.go:134, `List` :713; Top alignment only (listelement.go:22). **`measure_all` MISSING-GO** |
| `img(ImageSource)` + `ObjectFit` + `Image::from_bytes(Svg)` | inline_flow.rs (images), utils.rs data_url_image, node.rs check SVG | **MISSING-GO** img element. `NewImage` image.go:55, image cache imagecache.go:210, `ObjectFit` capture.go:582 exist; native image decode exists (internal/native/image.go) |
| `div` fluent: `overflow`, `id`, `on_click`, `text_color`, hover/active, scroll | node.rs, text_view.rs | Mostly PRESENT (div.go:678 ID, :888 TextColor, :940 OnClick, :1084 OnScrollWheel); cursor styles **MISSING-GO** |
| `PaintQuad` / `fill` / `quad` from a custom element | selectable_text.rs, touch_selection.rs:170, inline.rs selection/bg | **MISSING-GO** (no public paint entry; `Fill` paint.go:256 builds a PaintQuad but cannot be submitted) |
| `ContentMask`, `with_content_mask`, element offset | text_view.rs | PRESENT `WithContentMask` element.go:1165, `ContentMask` :1222 |
| `TouchPhase`, `ScrollDelta` | text_selection.rs | PRESENT mouseevent.go:70, :142 |
| accessibility `Role::GenericContainer`, `accesskit::Node` | inline_object.rs:221-225, 479 | PRESENT `Accessibility[L,P]` accessibility.go:441, `A11yRole/WriteA11yInfo` pattern (div.go:423-432) |
| `TextStyle` + `WhiteSpace`, `TextAlign`, `font()` | everywhere | PRESENT div.go:54-175 (no underline/strikethrough/background fields; **MISSING-GO** for TextStyle decorations) |
| `TestApp::with_text_system(Arc<dyn PlatformTextSystem>)` | inline.rs:1461+, node.rs:3612 | **MISSING-GO** (test only) |
| Grapheme clusters for selection / word stepping | inline_flow.rs (unicode_segmentation) | PRESENT natively: `WrappedLine.Clusters` text.go:1110 (graphemes per native doc). Pure-Go segmentation would need a dep or hand port |

### Q3: hooks needed for cross-element selection (text_selection.rs)
- **Architecture**: one `TextSelectionLayer` element per window (or per scope) registers window-level `on_mouse_event` listeners for MouseDown, Move (two of them), Up (two of them) and ScrollWheel (`text_selection.rs:2635-2741`). Participants (Inline, SelectableText, TextView) register each frame with a `TextSelectionRegistration{hitbox, bounds, text_bounds, runs: Vec<TextSelectionRun{text, TextLayout, bounds}>, scroll_offset, scope, document_order}`.
- **State**: a per-window `Entity<WindowSelectionState>` in element state, plus a global `SelectionStateRegistry`. Touch suppression comes from `GlobalState` (base).
- **Present in Go**: window mouse listeners, hitboxes and hover, element state, globals, entities/events, clipboard write, tracked mouse position, window id, and App-level defer.
- **Blocking gaps (all MISSING-GO)**: (a) a `TextLayout`-like handle (hit-test and position per run). `*WrappedLine` has the needed operations but no element publishes one. (b) A public paint API for highlight quads and touch handles. (c) A hitbox-scoped cursor. (d) `Window.Defer`. No native gaps for selection itself: `SelectionRects`, `HitTest` and `Caret` are native-backed.

---

## 3. Dependencies (Q2): Go routes and fidelity

go.mod has **zero** third-party requires (`go.mod`: `module …; go 1.27.0`).

| Rust dep | Use in slice | stdlib route | Non-stdlib route | Hand-port | Fidelity notes / recommendation |
|---|---|---|---|---|---|
| `markdown` 1.0 (markdown-rs, mdast + GFM + math + frontmatter + MDX) | format/markdown.rs, node.rs, markdown_ext.rs (public `markdown_ast`), range_highlight.rs, state.rs | none | **goldmark** (`github.com/yuin/goldmark`, MIT, CommonMark 0.31 + GFM ext: table, strikethrough, linkify, tasklist; footnote ext). Math and frontmatter need extra third-party modules. **No MDX.** | markdown-rs is roughly 20k+ Rust lines (tokenizer state machine + `to_mdast`); XL+ | goldmark's AST is not mdast. We would need our own Go mdast types (required anyway, since the plugin API exposes them) and an adapter. **Positions**: goldmark gives byte segments for block lines and `Text`, but no start/end spans for Emphasis/Strong/Link/Code inlines, and markdown.rs relies on `node.position()` everywhere (about 90 uses; :73 re-parses by position slice). Spans would have to be reconstructed by scanning delimiters, which puts the 57 source-alignment tests at risk. Edge-case divergences: GFM autolink-literal, footnote definitions, `$`/`$$` math semantics (micromark-extension-math), html_flow vs html_text chunking. MDX (`enable_mdx`, markdown_ext.rs:400-406) is **lost** with goldmark. Choice: (A) goldmark + adapter + drop MDX (M–L), or (B) hand-port markdown-rs (XL, exact positions, matches upstream tests). |
| `html5ever` 0.27 + `markup5ever_rcdom` 0.3 | format/html.rs, html5minify | none (stdlib `html` only escapes/unescapes) | **golang.org/x/net/html** (Go team, BSD; WHATWG tree-construction like html5ever; `*html.Node` DOM like RcDom) | html5ever tree builder + tokenizer is about 15k lines; XL | x/net/html matches html5ever closely (same spec algorithm, `Parse` = `parse_document`). The minifier ports 1:1 over `*html.Node`. Recommended. |
| `html5ever::data::NAMED_ENTITIES` | format/markdown.rs:567 (entity lookup) | **stdlib `html.UnescapeString("&name;")`** (full HTML5 entity table) | – | – | No dep needed. |
| `syntect` | **not used in src** (only examples/showcase) | – | chroma only if we port the example | – | No dependency for the base port. Highlighting is the `CodeBlockHighlighterFn` callback (text_view.rs:27). |
| `unicode-segmentation` | inline_flow.rs graphemes/words | none | `github.com/rivo/uniseg` | port UAX#29 (L) | Prefer native `WrappedLine.Clusters` (grapheme per native doc) where a shaped line exists. Word boundaries already use the custom ASCII/Latin classifier (text_boundary.rs, selection.rs), which is stdlib-portable. |
| `data-url` | utils.rs data_url_image | `encoding/base64` + `net/url` (hand-roll, about 60 lines) | `github.com/vincent-petithory/dataurl` | S | Hand-roll. |
| `web_time`, `futures` (Stream, channels), `anyhow`, `tracing` | state/stream_fade/range_highlight | `time`, chans, `errors`, `log/slog` | – | – | Trivial. |

---

## 4. Proposed ticket slices (bounded)

Prerequisites (core MISSING-GO; may be shared with other slices):
- **T0a Core public paint API.** `Window`-level paint quad, underline, strikethrough, shaped text and image for custom elements during Paint (wrap `frame.scene` + `frame.paint`). M. Blocks everything below.
- **T0b Public rich text.** `StyledText`-equivalent element with `TextRun{Len, Font, Color, Background, Underline, Strikethrough}` and a `TextLayout` handle (`IndexForPosition`, `PositionForIndex`, `LineHeight`, `Lines()` with row ranges and widths) over `*WrappedLine`. Per-run color through segment shaping or glyph x-binning, with the deviation recorded. L.
- **T0c Small hooks.** Hitbox-scoped cursor style (`Window.SetCursorStyle(style, hitbox)` + div `Cursor*`), `Window.Defer`, `ListState.MeasureAll`. S–M.
- **T0d Img element.** `Img(ImageSource)` + `ObjectFit`. M. Likely owned by another slice.
- **T0e Public inline layout.** Expose the box-aware inline wrapper (`inlineLayoutRequest`/`layoutInline`) as a public API usable by InlineFlow, replacing `LineWrapper`/`LineFragment`. M.

Base tickets:

| # | Ticket | Modules | Needs | Size |
|---|---|---|---|---|
| E1-1 | Word/line boundaries + touch geometry | text_boundary.rs, text/selection.rs, touch_selection.rs (pure parts) | – | S |
| E1-2 | Selection core model (no rendering) | text_selection.rs: ids, endpoints, registration, projection, snapshot, events, `WindowSelectionState`, registry | GlobalState, AutoScroll (base) | L |
| E1-3 | Selection layer + window hooks | text_selection.rs: `TextSelectionLayer` element, mouse/scroll listeners, multi-click, auto-scroll, touch handles paint | E1-2, T0a, T0b (TextLayout), T0c | L |
| E1-4 | SelectableText element | selectable_text.rs | E1-3 | S |
| E1-5 | Go mdast types + markdown parser decision/integration | markdown_ext.rs types; parser adapter (goldmark) **or** markdown-rs port | dependency decision | M (goldmark) / XL (port) |
| E1-6 | Document model + markdown to document | node.rs data model (no render), format/markdown.rs, utils.rs, document.rs | E1-5 | L |
| E1-7 | HTML format + minifier | format/html.rs, html5minify | x/net/html decision, color_picker::parse_hex | M |
| E1-8 | Inline text element | inline.rs, inline_element.rs, inline_object.rs | T0a, T0b, T0c, E1-3 | L |
| E1-9 | InlineFlow (mixed text/object/image wrapping) | inline_flow.rs | T0e, T0d, E1-8 | L |
| E1-10 | Node rendering | node.rs render half (blocks, tables, code blocks, lists, task checks) | E1-6, E1-8/9, theme, StyledExt, h_flex, scrollable_mask | L |
| E1-11 | Range highlight + stream fade | range_highlight.rs, stream_fade.rs | E1-6, motion::Easing | M |
| E1-12 | TextViewState + TextView + selection adapter | state.rs, text_view.rs, style.rs, selection_adapter.rs, mod.rs | all above, input actions, async_util, ElementExt | L (+ about 110 tests) |

Recommended order: T0a → T0b/T0c → E1-1 → E1-2 → E1-3 → E1-4, which gives a usable selection stack early. In parallel: E1-5 → E1-6 → E1-7. Then E1-8 → E1-9 → E1-10 → E1-11 → E1-12.

---

## 5. Deduplicated MISSING-GO

1. Public paint API for custom elements: quad/fill, underline, strikethrough, shaped text, image (only `PaintSvg` is public, svgelement.go:383).
2. `StyledText` element with styled runs (`TextRun` lacks color/background/underline/strikethrough, text.go:453).
3. `TextLayout` handle on a text element: `IndexForPosition`/`PositionForIndex`/`LineHeight`/row layouts. Underlying `*WrappedLine` ops exist (text.go:801-1110).
4. `HighlightStyle`, `StrikethroughStyle`, combine-highlights helper; TextStyle decoration fields.
5. Window-scoped text system access: `shape_line`/`layout_line` returning a paintable line (only `DefaultTextSystem()` + private `windowTextSystemOf`; `Window.ShapeText` is facts-only, testwindow.go:548).
6. Public box-aware inline wrapping (replacement for `LineWrapper`/`LineFragment`). The algorithm exists privately (inline_real.go:157).
7. `Window.LineHeight()` convenience (derivable).
8. Hitbox-scoped cursor style plus div cursor styles (only `App.SetCursorStyle`, app.go:993).
9. `Window.Defer` (App/Context Defer exist).
10. `ListState.MeasureAll`, and non-Top `ListAlignment`.
11. `Img` element (image decode and cache exist).
12. Test-only: injectable fake text system (`TestApp::with_text_system`).

## 6. Dependency questions (need a decision)

1. **Markdown parser**: allow `github.com/yuin/goldmark` as the first third-party module, which loses MDX and gives approximate inline positions? Or hand-port markdown-rs (XL, exact)? Is MDX support (`enable_mdx`) in scope?
2. **HTML**: allow `golang.org/x/net/html`? Recommended: it is quasi-stdlib and spec-equivalent to html5ever.
3. **Graphemes**: rely on native `Clusters()` plus the existing hand-rolled word classifier, or add `github.com/rivo/uniseg`?
4. **Highlighting**: confirm no highlighter ships in base (upstream ships none in src). Chroma is only for an optional example.
5. **Per-run glyph color**: accept a Go segment-shaping deviation (loses cross-run kerning/ligatures, already accepted in inline_real.go:40-48), or block on native?

## 7. MISSING-NATIVE (frozen DLL) with evidence

| Item | Evidence | Impact | Go-only mitigation |
|---|---|---|---|
| Per-run paint style in shaping (color/background/underline/strikethrough) | `TextRunRecord` has no style fields (internal/native/text.go:242-255); native builds runs with `..TextRun::default()` (reference/native/src/text.rs:1099-1109) | Single-document multi-color text cannot be painted from fragments | Shape per style segment (as inline_real.go). Backgrounds and decorations via `SelectionRects` + Go quads |
| Fragment to style/run mapping, decoration offsets | `GpuiGoTextFragmentRecord` = font_id, size, x range, glyph range only (reference/native/src/text.rs:518-533) vs pinned `PaintFragment.style/underline_offset/strikethrough_offset` (ce-source line_layout.rs:419-435) | Cannot recover which run a fragment belongs to | x-range vs run `Caret` positions (breaks under bidi) |
| Glyph to source byte index | `GpuiGoTextGlyphRecord` = id, x, y, is_emoji (reference/native/src/text.rs:543-554) | Per-glyph coloring/fade (stream_fade per-range opacity) needs index mapping | Segment shaping; or bin glyph x into `SelectionRects` |
| Strikethrough metrics | Go/native `FontMetrics` lacks strikeout offset/size (text.go:244-272); Parley uses `run_metrics.strikethrough_offset/size` (gpui_ce_parley/src/text_system.rs:1257-1264) | Strike line position approximate | `XHeight/2` heuristic |
| Inline boxes in shaping (`push_inline_box`) | Already documented, inline.go:40-45, inline_real.go:14-19 | Inline objects and images wrap via the Go greedy algorithm (bounded deviations listed there) | Already implemented privately; expose it (MISSING-GO 6) |

No native gap was found for selection itself (hit-test, caret, selection rects and clusters are native-backed), for clipboard, or for hitboxes and mouse events.
