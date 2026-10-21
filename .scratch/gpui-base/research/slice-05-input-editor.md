# Slice 05: input/editor engine (`crates/base/src/input/**`)

Upstream: `upstream-base/ce/crates/base/src/input` @ c0820693 (abbreviated `U/`).
Go core: `gpui-go/gpui` (abbreviated `G/`). Core pin: `gpui-go/reference/ce-source/crates/gpui/src` @ 254b5db (abbreviated `P/`).
Older gpui-ce (same `version = "0.2.2"`, has the legacy text API): `~/.cargo/git/checkouts/gpui-ce-6758b1db058c6497/beb5a6d/crates/gpui/src` (abbreviated `OLD/`).

Totals: 45 files, **28,204 lines, 294 tests** (`#[test]`/`#[gpui::test]`). About 9.6k of those lines are `#[cfg(test)]` bodies (state.rs alone has 5,623 test lines). The production code is about 18.6k lines.

## HEADLINE FINDING: the text API doesn't match the pin

Base compiles against **crates.io gpui-ce 0.2.2** (`upstream-base/ce/Cargo.lock:3057-3060`, registry source, no `[patch]`). That crate has the **legacy Zed text API**. The core pin at 254b5db, which the Go port mirrors (`G/draw_text.go:3-5`), **removed** that API in its backend-owned Parley rewrite, even though `Cargo.toml` still says 0.2.2. Base uses these APIs, and **none of them exist in either P/ or G/**:

| API base uses | where (U/) | OLD/ definition | P/ replacement | G/ today |
|---|---|---|---|---|
| `text_system().line_wrapper(font,size)` + `LineWrapper::wrap_line(&[LineFragment], w) -> Iterator<Boundary>` | editor/display_map/text_wrapper.rs:296-335 | text_system.rs:332, text_system/line_wrapper.rs:17,46,671 (`LineFragment`),731 (`Boundary`) | none (Parley wraps inside `layout_text`) | none |
| `LineFragment::element(width,len)` (inline token boxes inside wrapping) | text_wrapper.rs:324 | line_wrapper.rs:671 | `layout_inline` P/text_system.rs:367 | inline layout is composed in Go per segment (`G/inline_real.go:13-30`) |
| `ShapedLine::x_for_index / closest_index_for_x / index_for_x` | element.rs:724,730,1797,1970; state.rs:3040; display_map/inline_line.rs:45-86 | text_system/line_layout.rs:58,75,105 | `LineLayout::visual_position_for_byte_index` P/text_system/line_layout.rs:745, `closest_byte_index_for_pixel_point` :716, `byte_index_for_pixel_point` :698 | `WrappedLine.Caret` G/text.go:876, `HitTest` :928, `ByteIndexForPixelPoint` :961 |
| `text_system().layout_width(font_id,size,ch)` | element.rs:2396-2397 | text_system.rs:745 (shapes 1 char) | `advance()` P/text_system.rs:177 (glyph advance, platform) | none (no advance in the native ABI) |
| `shape_line(text,size,&[TextRun{color,bg,underline,strike}],None) -> ShapedLine` with `paint`/`paint_background` | element.rs:1049-1957, 2624, 2798, 2951, 3031, 3130 | text_system.rs:430 | P/text_system.rs:325 + `PaintStyle` per fragment P/text_system/line_layout.rs:407-435 | `ShapeText` (G/text.go:483) has **font-only runs** (G/text.go:453-462) and paints in **one color** (`Scene.PaintShapedText` G/draw_text.go:734) |

So the port needs a Go-side **"legacy text facade"** (ShapedLine, LineWrapper, per-run styles) built on top of `ShapeText`. This doesn't touch the native code. Details are in the next section.

## Summary table

Class key: **PG** = pure Go once deps exist. **CORE** = needs a Go-side core addition. **NG** = native gap. Lines and tests are per file (code+tests).

| Subsystem (U/) | lines | tests | class | missing core APIs | 3rd-party |
|---|---|---|---|---|---|
| base/rope_ext.rs (RopeExt: line/offset/point/LSP Position/UTF-16/word/clip) | 719 | 15 | PG | – | ropey, sum_tree::Bias, lsp_types::Position |
| base/cursor.rs, selection.rs, change.rs, layout.rs | 617 | 10 | PG (layout.rs needs ShapedLine) | ShapedLine | ropey |
| base/undo_manager.rs | 599 | 8 | PG | – | ropey |
| base/mask_pattern.rs (masks + number normalize, hand-written matcher, **no regex**) | 725 | 9 | PG | – | – |
| base/blink_cursor.rs (500ms blink, pause on type) | 246 | 5 | PG* | `cx.spawn` + `background_executor().timer` -> Go `App.Spawn`/`TaskRun.Sleep` (G/executor.go:625,742) ok | – |
| base/movement.rs (vertical/word/line moves over layout) | 463 | 0 | PG | ShapedLine x<->index | sum_tree::Bias |
| base/mode.rs (LayoutMode single/multi/code, wrap, line numbers) | 501 | 4 | PG | – | ropey |
| base/kind.rs (InputModeKind seam: Input/Textarea/Editor hooks, InputExtras) | 377 | 0 | PG | – | lsp_types::InlineCompletionItem |
| base/inline_tokens.rs + token_presentation.rs (atomic inline chips) | 619 | 0 | PG | AnyElement layout (G has) | unicode-segmentation (grapheme_indices) |
| base/touch.rs (touch selection/edit menu) | 331 | 0 | PG (needs slice `touch_selection`, `AutoScroll`) | TouchPhase ok (G/mouseevent.go:70) | – |
| base/native.rs (macOS content type; NativeMenu builder) | 154 | 0 | PG (Windows: no-op) | – | objc2 (mac only) |
| base/mod.rs (InputBase element wrapper) | 281 | 2 | PG (needs slice `ObservedElement`, `state_style`) | Role ok (G/element.go:292) | – |
| **base/state.rs** (InputBaseState: editing, actions/keymap, IME, clipboard, scroll, search glue, validation, render) | 10,137 (≈4.9k code) | 169 | CORE | EntityInputHandler (G equivalent: IMEInputHandler G/ime.go:135, ok), `cx.on_focus/on_blur` P/app/context.rs:547,596; `observe_window_activation` :444; `spawn_in` :676; `defer_in` :305; `window.modifiers()` P/window.rs:3127; `line_height()` :2983; test `simulate_keystrokes/mouse` P/app/test_app.rs:399-443 | ropey, regex (`pattern`), unicode-segmentation (`split_word_bound_indices`) |
| **base/element.rs** (TextElement: layout/prepaint/paint, gutter, selections, caret, ghost text, fold icons, scrollbar) | 4,497 (≈3.4k code) | 33 | CORE | ShapedLine + styled paint, `combine_highlights` P/style.rs:1236, HighlightStyle :751, `set_cursor_style(style,&hitbox)` P/window.rs:3844, `handle_input`+ElementInputHandler P/input.rs:154 (G: `Window.HandleInput` G/input.go:60, ok), PathBuilder/paint_path ok (G/path.go:193-217,455), paint_quad ok, paint_svg ok (G/svgelement.go:383) | ropey, smallvec |
| editor/display_map/* (text_wrapper SumTree wrap rows, fold_map, wrap_map, inline_line) | 2,917 | 14 | CORE | LineWrapper/LineFragment/Boundary, ShapedLine | sum_tree (SumTree/Dimension/Cursor), ropey, smallvec |
| editor/highlighting.rs (InputHighlighter + HighlightStyleResolver traits; parser-agnostic) | 258 | 4 | CORE (HighlightStyle) | HighlightStyle | ropey |
| editor/decorations.rs (range/text decorations) | 755 | 6 | CORE (HighlightStyle) | HighlightStyle, UnderlineStyle ok (G/paint.go:284) | ropey, sum_tree::Bias |
| editor/diagnostics.rs (DiagnosticSet over SumTree) | 319 | 1 | PG | – | sum_tree (Item/Summary/SeekTarget/cursor), lsp_types::Diagnostic* |
| editor/indent.rs (+language_config.rs, language.rs, auto_close.rs) (tab size, indent guides, bracket pairs, regex indent rules) | 891 | 2 | PG (indent guides use PathBuilder + TextRun, both ok) | – | regex (`is_match` only), ropey |
| editor/search.rs (SearchSession/Matcher) | 557 | 6 | PG | – | aho-corasick |
| editor/lsp/* (provider traits + completion/hover/code-action/definition/doc-color/semantic-token state and overlays) | 1,736 | 6 | PG | Task, Hitbox, MouseDownEvent (G has) | lsp_types (data only) |
| editor/mod.rs, input/mod.rs, textarea/mod.rs, mod.rs (Editor/Input/Textarea RenderOnce wrappers, re-exports) | 505 | 0 | PG | – | – |

## Core additions needed (Go-side, no native work)

1. **Styled text runs + styled paint** (the biggest one; other slices like Text/Label probably need it too).
   - Pin: `TextRun{color, background_color, underline, strikethrough}` P/text_system.rs:489-504. `PaintStyle` P/text_system/line_layout.rs:407. `HighlightStyle` P/style.rs:751. `combine_highlights` P/style.rs:1236. `UnderlineStyle`/`StrikethroughStyle` P/style.rs:1088,1101. `ShapedLine::paint/paint_background` P/text_system/line.rs:34,58.
   - Go today: `G/text.go:453` TextRun = Len/Font/LetterSpacing only; `G/draw_text.go:734` takes one `color`. No HighlightStyle anywhere in G/.
   - The native `TextGlyph` has no byte index (`internal/native/text.go:527-532`), and fragments have no byte range (`G/text.go:627-631`). So the Go painter has to map glyphs to runs with `WrappedLine.Clusters()` + `Caret` x-extents (G/text.go:1110,876), or with `SelectionRects` per run range (G/text.go:989). Then it paints each glyph in its run's color and draws bg quads and underline/strikethrough quads (`underlineFor` G/paint.go:545). Caveat: a ligature that spans a color boundary gets one color.
2. **Legacy `ShapedLine` facade** (single-line, no wrap): `shape_line` + `len/width/x_for_index/closest_index_for_x/index_for_x/paint/paint_background`, on top of `ShapeText` + `Caret`/`HitTest`. OLD/text_system/line_layout.rs:58-105 for semantics, P/text_system.rs:325 for the signature.
3. **`LineWrapper` port** (OLD/text_system/line_wrapper.rs, ~650 code lines): greedy wrap over per-char widths, plus `LineFragment::element` and `Boundary{ix, next_indent}`. Width source: shape one char via `ShapeText` and read `Summary().Width`, with an ASCII cache. This is exactly what OLD `layout_width` does (OLD/text_system.rs:745-760). It also gives `layout_width` (item 2 of the headline table) for free. Alternative: shape each buffer line with `WrapWidth` and read `Line(i).TextStart` (G/text.go:801). The catch is that inline element boxes then need segment composition like `G/inline_real.go`. I recommend the char-width port because it is deterministic and testable with the fixed-advance test text system (`G/testwindow.go:118`).
4. **Focus/activation/async context helpers**: `Context.OnFocus/OnBlur` (P/app/context.rs:547,596; Go only has `Window.OnFocusLost` G/focus.go:486), `ObserveWindowActivation` (P/app/context.rs:444; Go has `Window.IsActive` G/window.go:288 but no observer), `SpawnIn(window)` (P/app/context.rs:676; Go `AsyncApp` only has `App/Update/Await` G/executor.go:701-723), `DeferIn` (P/app/context.rs:305).
5. **Per-hitbox cursor style**: `Window.SetCursorStyle(style, hitbox)` P/window.rs:3844. Go only has global `App.SetCursorStyle` G/app.go:993, and says the per-frame reset is deferred (G/mouseevent.go:563). Base uses it for I-beam and pointing-hand on links and fold icons (element.rs:3176). A div `.cursor()` is likely a shared need.
6. Small items: `Window.Modifiers()` (P/window.rs:3127), `Window.LineHeight()` (P/window.rs:2983; derivable from `WindowTextStyle` G/element.go + `RemSize`).
7. **Test support** (needed to port the 169 state.rs tests): VisualTestContext-style `SimulateKeystrokes("cmd-a backspace")`, `SimulateMouseDown/Move/Up/Click` (P/app/test_app.rs:399,422,443; P/app/test_context.rs:520). Go `TestApp` (G/testapp.go:32-87) has `RunUntilParked/AdvanceClock` but no input simulation.

Already present in G/, no work needed: IME handler surface (`IMEInputHandler` G/ime.go:135-178, `ComputeIMECandidateBounds` :181, `UTF16Selection` :112, `UTF16Range` G/input.go:31, `Window.HandleInput` :60). UTF-8/16 helpers (G/ime.go:59-100). `ShowCharacterPalette` (G/window.go:304). Clipboard with image entries (G/clipboard.go:138-217, `App.Read/WriteClipboard` :418,434). Keystroke interceptor (`App.OnKeystrokeInterceptor` G/key_dispatch.go:1200, maps to `intercept_keystrokes` state.rs:665). Actions/KeyBinding/KeyContext (G/action.go:465, G/input.go `BindKeys`). PathBuilder fill/stroke + PaintPath. PaintQuad/Fill. ScrollHandle (G/scroll.go:58). TouchPhase. Hitbox (G/mouseevent.go:227, `InsertHitbox`). `WithContentMask`. `PaintSvg`. `Role`. `Event.Emit/Subscribe`. `cx.Observe/Notify/Listener`.

`G/editor.go` (the core's editable_text port) is a single-`WrappedLine` document. Base replaces it with a rope + display map. Only its IME/UTF-16 conventions carry over (`editor.go:371-431` resolve/mark helpers are a good template for state.rs:3856-4300).

## Native gaps

**None blocking.** Reasoning:
- Everything the input engine needs from text decomposes into `Shape`/`Caret`/`HitTest`/`SelectionRects`/`Cluster`/`Fragments`/`Glyphs`/`Line`/`Relayout` (internal/native/text.go:925-1409), plus glyph raster/paint, which already ships.
- The soft gaps all have Go workarounds:
  - (a) no glyph advance query (`TextSystem::advance` P/text_system.rs:177 -> platform `advance` P/platform.rs:1207 isn't in the ABI). Workaround: single-char shaping, which is also what OLD `layout_width` does.
  - (b) no style/color in `ShapeRequest` (internal/native/text.go:415-444) and no glyph->byte index (:527). Workaround: x-range/cluster mapping in Go (core addition 1).
  - (c) no inline boxes in shaping. Workaround: the per-segment composition Go already uses (G/inline_real.go:13-30).
- Per-character shaping cost goes over FFI. It's cached per (font, size, char), so ASCII is a one-time cost of about 95 calls per font.
- Risk: very long lines (code editor) re-shape whole lines through FFI on every edit. The text_wrapper only re-wraps the changed rows (text_wrapper.rs:503-509), so the risk is acceptable. A benchmark belongs in the editor ticket.

## Third-party usage (exact APIs)

- **ropey `=2.0.0-beta.1`**, features `metric_chars, metric_lines_lf, metric_utf16` (upstream-base/ce/Cargo.toml:89-93). **Byte-indexed** 2.0 API.
  - `Rope::from(&str)`, `len()` (bytes), `len_chars`, `len_lines(LineType::LF)`, `len_utf16`, `slice(byte range) -> RopeSlice`, `line(row, LineType::LF)`, `line_to_byte_idx`, `byte_to_line_idx` (rope_ext.rs:284-326), `byte_to_utf16_idx`/`utf16_to_byte_idx`, `char_to_byte_idx`, `char(..)`/`get_char`, `insert(byte,&str)`, `remove(range)`, `append`, `is_char_boundary`, `floor_/ceil_char_boundary`.
  - Iteration: `chars()`, `chars_at()`, `char_indices()`, `lines()`, `to_string()`.
  - No chunk iteration.
  - Everything goes through the `RopeExt` trait (rope_ext.rs:93-274, 20 methods). **Go decision:** implement a small Go rope, or start with a `[]byte`/piece-table plus a line-start index behind a `Rope` interface that mirrors RopeExt. No mature Go rope matches ropey 2's byte+UTF-16+LF metrics. Porting a minimal B-tree rope is about 600 lines. `Rope` and `RopeExt` are public re-exports (mod.rs `pub use ropey::Rope`).
- **zed-sum-tree 0.2.0**:
  - `Bias` enum (≈30 sites, trivial).
  - `SumTree<T>` with `Item`/`Summary`/`Dimension`/`Dimensions<A,B>`/`SeekTarget`, and `cursor().seek/seek_forward/slice/suffix/item/next`, `append`, `from_iter`, `summary()`. Used in exactly two places: text_wrapper.rs:59-236,501-509 (wrap rows: buffer_rows/wrap_rows/longest_row) and diagnostics.rs:144-246.
  - **Go:** a generic B+tree with summaries, about 400-600 lines. Or a Fenwick tree/prefix array for wrap rows. Diagnostics can be a sorted slice plus binary search.
- **lsp-types 0.97 (`proposed`)**: **data types only**, no transport. Types used: `Position, Range, Diagnostic, DiagnosticSeverity, DiagnosticTag, DiagnosticRelatedInformation, NumberOrString, CompletionItem, CompletionResponse, CompletionContext, CompletionTextEdit, TextEdit, InlineCompletionItem/Response/Context/TriggerKind, Hover, CodeAction, LocationLink, ShowDocumentParams, SemanticToken(s), SemanticTokenType, SemanticTokensLegend, ColorInformation, Color`. **Go:** about 25 hand-written structs, or `go.lsp.dev/protocol` (check that the proposed InlineCompletion types exist there).
- **regex 1**: only `Regex::is_match` for (a) user input validation `InputState::pattern(Regex)` (state.rs:397, 9761) and (b) `IndentationRules` increase/decrease patterns (language_config.rs:58-63, 164-172). Tests use `RegexBuilder` options (state.rs:8686). Not used by search or masks. **Go `regexp` (RE2) is equivalent** (neither supports lookaround).
- **aho-corasick 1.1.3**: search.rs:337-418 only, via `AhoCorasick::builder().ascii_case_insensitive(b).build([query])` + `stream_find_iter`. **Go:** `strings.Index` loop, or `bytes.EqualFold`-based ASCII case-insensitive scan (single pattern, so no automaton is needed).
- **unicode-segmentation 1.12**: `split_word_bound_indices` (word movement state.rs:1460,1477) and `grapheme_indices(true)` (inline_tokens.rs:198). **Go:** `github.com/rivo/uniseg` (word + grapheme). Grapheme boundaries could also come from native `Clusters()`, but uniseg is cleaner. Also note `crate::text_boundary::word_range_from_chars` (selection.rs:9), which belongs to another slice.
- **smallvec**: `SmallVec<[ShapedLine;1]>`/`[InputLine;1]` (element.rs:1734,1937,2156,2794). Go: plain slices.
- **syntect**: **not used in this slice.** It's only in `examples/showcase/syntect_highlighter.rs` (264 lines: `SyntaxSet::load_defaults_newlines` bundled dumps via the `default-syntaxes` + `regex-fancy` features, `ParseState`, `ScopeStack`; parsing only, no ThemeSet) and in `crates/story`. Base ships only the `InputHighlighter`/`HighlightStyleResolver` traits (highlighting.rs:13-60). Go equivalent for the showcase: `alecthomas/chroma`. Defer it.
- Cross-slice crate deps (not 3rd-party): `crate::Scrollbar` (element.rs:18), `ObservedElement`/`state_style` (base/mod.rs:128,175), `touch_selection` + `AutoScroll` (touch.rs:18,263), `text_boundary` (selection.rs:9), `global_state` (state.rs:2287), `actions::Select*` (state.rs:34), `number_input::NumberStep` (mod.rs:81), `SemanticThemeTokens` (highlighting.rs:7), `StepAction`.

## Deferrable vs core

- **Core (Input/Textarea MVP):** rope_ext, cursor/selection/change/undo, mask_pattern, blink, movement, mode (single + multi line, soft wrap), kind (hooks with no-op Input/Textarea impls), state minus editor features, element minus gutter/fold/diagnostic/LSP paints, display_map (wrap rows needed for Textarea; fold_map can be stubbed), input/textarea wrappers.
- **Deferrable (EditorMode / code editor):** editor/lsp/* (1.7k), diagnostics, semantic tokens, document colors, search (search is small though, and Textarea find may want it), indent guides + language_config/auto_close, fold_map, line-number gutter, inline completion ghost text, touch.rs (desktop Windows first), native.rs (mac only).
- **Seam:** `InputModeKind`/`InputExtras` (kind.rs:80-334) already routes every editor feature through hooks with no-op defaults. Port kind.rs early with the full hook surface so the later editor tickets plug in without touching state.rs.

## Suggested sub-ticket split (dependency order)

1. **T0 core: text facade** (CORE 1-3, 6). Styled TextRun/HighlightStyle/combine_highlights, ShapedLine (x<->index, paint, paint_background), LineWrapper + LineFragment + Boundary, layout_width. Lives in `gpui/` with Go tests against the fixed-advance test text system. Probably shared with the Text/Label slices.
2. **T0b core: context/test helpers** (CORE 4, 5, 7). OnFocus/OnBlur, ObserveWindowActivation, SpawnIn/DeferIn, per-hitbox cursor style, SimulateKeystrokes/Mouse.
3. **T1 data layer, pure Go:** Rope + RopeExt (with ropey's byte/LF/UTF-16 metrics), Bias, SumTree, uniseg wiring; plus rope_ext/cursor/selection/change/undo_manager/mask_pattern. About 3k lines, ~65 tests. No UI.
4. **T2 single-line Input:** kind.rs (full hook surface), mode.rs (single line), blink_cursor, movement, state.rs core (editing, actions/keymap, IME via IMEInputHandler, clipboard, validation pattern/mask/number), element.rs single-line paint (selection paths, caret, placeholder), base/mod.rs InputBase, input/mod.rs. Needs T0, T0b, T1 and the scrollbar/state_style slices.
5. **T3 Textarea / multiline:** display_map (text_wrapper + wrap_map + inline_line + display_map; fold_map stub), multi-line movement/scroll, inline_tokens + token_presentation, textarea/mod.rs. Ports the bulk of the 169 state.rs tests.
6. **T4 editor basics:** EditorMode, line-number gutter, fold_map + fold icons, indent/tab size + indent guides, language_config/language/auto_close (regexp), highlighting traits + decorations, search (SearchSession).
7. **T5 diagnostics + LSP:** lsp_types Go structs, diagnostics SumTree, completions/hover/code actions/definitions/document colors/semantic tokens/overlay, inline completion ghost text.
8. **T6 (optional) touch + platform:** touch.rs (needs the touch_selection slice), native.rs no-op on Windows, plus the showcase highlighter (chroma).
