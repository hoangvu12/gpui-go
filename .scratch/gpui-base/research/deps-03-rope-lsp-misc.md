# GPUI Base deps, part 3: rope, sum-tree, lsp-types, chrono, aho-corasick, data-url, macros

Researched 2026-10-19. This builds on `deps-recommendation.md` and `slice-05-input-editor.md` (ropey, sum-tree and LSP API surface). Base source: `upstream-base/ce/crates/base/src` (`U/`). gpui-go core: `gpui-go/gpui` (`G/`) and `gpui-go/authoring`.
Repo facts (license, stars, last push) come from the GitHub API on 2026-10-19.
Prototype: `C:/Users/ADMIN/AppData/Local/Temp/gpuibase-proto/misc` (data-url, ASCII search, strftime; `CGO_ENABLED=0 go test` passes). Strftime library comparison: `.../gpuibase-proto/misc-cmp`.

## 1. Rope (ropey `=2.0.0-beta.1`, features metric_chars + metric_lines_lf + metric_utf16)

Base needs:
- **byte-indexed** positions, with char, UTF-16 and LF-line metrics;
- `insert/remove/append/slice/line`, the conversions `byte<->line`, `byte<->utf16` and `char->byte`, and the `is/floor/ceil_char_boundary` checks;
- iterators: `chars`, `chars_at(..).reversed()`, `char_indices`, `bytes`, `chunks`, `lines`;
- **O(1) `Clone`**: there are 27 `text.clone()` sites in `U/input` (undo, search snapshots, display map). A Go port must therefore be persistent (immutable nodes with path copying), not a mutable buffer.

| Go candidate | License | Last push / stars | Metrics | Verdict |
|---|---|---|---|---|
| deadpixi/rope | **LGPL-2.1** | 2022 / 89 | bytes only | no (license, metrics) |
| zyedidia/rope (micro author) | MIT | 2021 / 8 | bytes, plus `Count(sep)` for lines | no: no char or UTF-16 metrics, unbalanced |
| liamg/rope | Unlicense | 2023 / 8 | rune-indexed `[]rune`, newline count | no: rune indexing, no bytes or UTF-16 |
| chewxy/skiprope | MIT | 2017 / 27 | runes | no |
| vinzmay/go-rope | BSD-2 | 2014 / 24 | bytes | no |
| reusee/rope, eugene-eeo/rope, hhhhhhhhhn/rope, larsmans/rope | MIT/none | 2015-2022 / ≤10 | bytes or runes | no |
| phroun/garland | MIT | 2026-07 / **0** | byte, rune and line (balanced binary tree, 128KB leaves) | no: no UTF-16; a one-author editor framework (versioning, cursors, backups) |
| aretext `text.Tree` | **GPL-3** | 2026 / 288 | chars and lines (cache-line B+tree) | no: license, char-at-a-time API, no bytes or UTF-16 |
| micro `internal/buffer/line_array.go` | MIT | active / 29.7k | `[][]byte` lines | no: internal package, not a rope |
| gopls `protocol.Mapper` | BSD-3 | internal | []byte + line table + UTF-16 conversion | no: not importable, not a rope (good reference for UTF-16 math) |
| ropee (kebe7jun) | - | - | - | false hit: a Prometheus adapter |

Piece-table searches (GitHub "piece table" lang:go) found only toy projects (≤3 stars).
**Verdict: HAND-PORT.** No Go rope has byte + UTF-16 metrics, and none is persistent with a permissive license and active maintenance.

Design: a persistent B-tree of string chunks (≤1 KiB, split only on char boundaries, never inside CRLF). Each node holds `TextInfo{bytes, chars, utf16, lf}` (ropey `tree/text_info.rs`). `Rope` and `RopeSlice` are value types. Ropey's 12.2k Rust lines include docs, tests and the metrics Base doesn't use (CRLF/Unicode lines, chunk cursor).
- Estimate: rope core + slice + iterators **≈1,300-1,700 Go lines**, plus ≈600 lines of tests (property tests against a `string` oracle).
- `rope_ext.rs` (719 lines, 15 tests) ports on top at ≈450 lines.
- Interim fallback (Input MVP only): an immutable `string` plus a line-start table behind the same API. That is about 300 lines with O(n) edits, so it is not usable for the code editor.

## 2. sum-tree (zed-sum-tree 0.2.0, Apache-2.0)

- Used by `text_wrapper.rs` (LineItem/LineSummary, BufferRows/WrapRows dimensions) and `diagnostics.rs` (DiagnosticEntry, SeekTarget). The `Bias` enum appears at ~30 sites.
- API used: `push`, `append`, `from_iter`, `summary`, `iter`, `first/last`, and on the cursor `seek`, `seek_forward`, `slice`, `suffix`, `item`, `next`, `start`, plus `Dimensions<A,B>`.
- Go search results:
  - tidwall/btree (MIT) and google/btree (archived) are keyed trees with no custom summaries.
  - ajwerner/btree `aug` (Apache-2.0, beta, 28 stars) is an augmented **keyed** B-tree. SumTree is a **positional sequence** that seeks by any dimension, so it doesn't fit without contortion.
  - No Go library offers monoid summaries with dimension seeks.
- **Verdict: HAND-PORT** a generic `SumTree[T Item[S], S Summary[S]]`, with `Dimension[S]` and `SeekTarget` as interfaces. Zed source: `sum_tree.rs` (1,444 lines) and `cursor.rs` (817 lines), mostly docs and tests. `tree_map.rs` is not needed.
  - Estimate: **≈700-900 Go lines** plus ≈300 test lines. Apache-2.0 source must be credited in THIRD_PARTY_NOTICES.
  - Cheaper path, already recorded as a deviation in `G/listelement.go:20`: a slice plus prefix sums (≈150 lines) for both trees. Wrap rows are re-wrapped per changed range, so O(n) splices are fine below roughly 10^5 lines. Pick one approach in the editor ticket; `Bias` lives in the rope package either way.

## 3. lsp-types 0.97 (`proposed`)

- Base uses ≈28 data-only types: Position, Range, Diagnostic(+Severity, Tag, RelatedInformation, CodeDescription), NumberOrString, CompletionItem/Response/Context/TriggerKind/TextEdit (Edit or InsertAndReplace), TextEdit, InlineCompletionItem/Response/Context/TriggerKind, Hover/HoverContents/MarkedString, CodeAction, LocationLink, ShowDocumentParams (Uri), SemanticToken(s)/TokenType/Legend, ColorInformation, Color.
- There is no transport. The only JSON is `Diagnostic.data: Option<serde_json::Value>` (`diagnostics.rs:51`).

| Go module | License | InlineCompletion* | Weight | Verdict |
|---|---|---|---|---|
| go.lsp.dev/protocol v1.0.1 (go-language-server/protocol, 2026-06) | BSD-3 | **yes** (`inline_completion.gen.go`, from the 3.18 metamodel) | 3.2 MB of source. go.mod requires go-json-experiment, jsonrpc2, uri and go-cmp, **plus a `tool` block listing about 230 indirect modules** (golangci-lint and others), all of which land in consumers' module graph and go.sum | no: far too heavy for ~28 structs |
| myleshyson/lsprotocol-go | MIT | yes (generated 3.18 `types.go`, 666 KB) | no runtime dependencies; 9 stars, last push 2025-07 | possible copy source, but not worth adding as a dependency |
| tliron/glsp `protocol_3_16`/`_3_17` | Apache-2.0 | **no** | 288 stars, last push 2025-06 | no |
| sourcegraph/go-lsp | MIT | no | archived | no |
| gopls `internal/protocol` | BSD-3 | yes | **not importable** (internal) | reference only |

**Verdict: HAND-PORT** about 28 structs and enums (≈350-450 Go lines with JSON tags; `Diagnostic.Data` becomes `json.RawMessage`; `Uri` becomes `string`).
- Unions need explicit Go shapes: `NumberOrString`, `CompletionTextEdit`, `CompletionResponse` (Array or List), `HoverContents`, `InlineCompletionResponse`.
- Match **lsp-types 0.97**, not LSP 3.18. For example, 0.97 `InlineCompletionItem.insert_text` is a `String`, while the 3.18 metamodel (go.lsp.dev) makes it `string | StringValue`.
- `Position` keeps UTF-16 semantics.

## 4. chrono 0.4

Only `calendar.rs` and `time_field.rs` import chrono. `date_picker.rs` (112 lines) uses only the Calendar types. Exact API surface:
- `NaiveDate::from_ymd_opt` (15), `.year/.month/.day/.weekday()`, `Weekday::{Sun..}` + `num_days_from_sunday`, `Local::now().date_naive()`, `chrono::Duration::days(n)`, `date - date -> .num_days()`, `NaiveDate` `Display` (`format!("calendar-{date}")` gives `%Y-%m-%d`).
- `NaiveTime::from_hms_opt`, `NaiveTime::MIN`, `Timelike::{hour,minute,second}`.
- `.format(str)`: public `CalendarDate::format(&str)` (`calendar.rs:61`, user-supplied layout) and `time_field.rs:47-50` (`%H:%M`, `%H:%M:%S`, `%I:%M %p`, `%I:%M:%S %p`).

Go `time` covers all the arithmetic: `time.Date` normalises, `AddDate`, `Weekday`, `Sub`. Prototype `TestDaysInMonthGrid` ports `days_in_month` (calendar.rs:831-846). `NaiveDate`/`NaiveTime` should be small Go value types (year, month, day / seconds since midnight), not `time.Time`, so zone handling can't leak in. Only `Local::now()` touches the zone.

Strftime libraries, checked against a 45-case table taken from chrono 0.4.45's own tests (`format/strftime.rs:918+`). Note that chrono's **doc table** lists `%U`=28, but its test asserts 27; 27 is correct.

| Library | License / deps | Size | Mismatches vs chrono (45) |
|---|---|---|---|
| itchyny/timefmt-go v0.1.9 | MIT, no dependencies | 24 KB (also has strptime) | 2: `%f` gives 6 digits (chrono: 9); `%q` unsupported |
| ncruces/go-strftime v1.1.0 | MIT, no dependencies | 16 KB | 4: `%f`, `%q`, `%_m`, `%0e` (no `_`/`0` padding modifiers) |
| lestrrat-go/strftime v1.2.0 | MIT; requires envload and testify (yaml indirect) | 34 KB | 5: compile errors on `%P`, `%f`, `%q`, `%_`, `%0` |
| prototype `misc/strftime.go` | in-repo | **139 lines** | 0 |

All three libraries format Base's own four layouts correctly.
**Verdict: STDLIB `time` + HAND-PORT the strftime formatter** (≈150 lines). It is cheaper than a dependency and reaches 45/45 chrono parity, including `%-`/`%_`/`%0`, `%P`, `%q`, `%e`, `%k`, `%l` and `%f`.
- Unknown specifiers: chrono's `Display` fails (`to_string` panics). Pick a Go behaviour and record it as a deviation; the prototype writes them out verbatim.
- If parsing is ever needed (not today), itchyny is the best fallback.

## 5. aho-corasick and data-url: trivial hand-ports, confirmed

- **aho-corasick**: `search.rs:337-418` builds one pattern with `ascii_case_insensitive`, then takes `stream_find_iter` byte ranges over `rope.to_string()`. With a single pattern, Aho-Corasick's standard non-overlapping semantics are exactly "leftmost match, then continue at its end".
  - Prototype `misc/search.go` is 42 lines. Tests cover `aaaa`/`aa` giving `[0,2),[2,4)`, and ASCII-only folding (`Ä` is not folded, the same as the crate).
  - Faster alternative: `bytes.Index` on a pre-lowered copy. Lowering is byte-length-preserving for ASCII, so offsets stay valid.
- **data-url**: `text/utils.rs:60` runs `DataUrl::process`, then `mime_type()`, then `decode_to_vec()`. Prototype `misc/dataurl.go` (137 lines) implements the WHATWG processor: case-insensitive scheme, fragment split, first comma, `;base64` with optional spaces, MIME fallback to `text/plain;charset=US-ASCII`, percent-decoding and forgiving-base64.
  - It passes all of Base's `test_data_url_image` cases plus whitespace, fragment and uppercase variants.
  - The `image/jpg` to JPEG alias belongs to `ImageFormat::from_mime_type`, which is gpui-go's job.

## 6. Macros, derives and gpui-provided libraries

Counts are from a grep of `U/`.

| Rust construct (count) | gpui-go today | Needed for Base |
|---|---|---|
| `#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash, Default, Ord)]` (≈1,200) | language | Nothing. Value structs give `==`; write explicit `Clone()` only where a type holds slices or maps; Debug becomes `%+v`/`String()`. |
| `#[derive(IntoElement)]` (76) on RenderOnce types | `RenderOnce` (`G/view.go:42`); `TryIntoElement` accepts RenderOnce, `IntoElement`, View and string (`G/element.go:682-720`) | **Nothing to generate.** Each component implements `RenderOnce(w, app)`. |
| `actions!(ns, [A, B, ..])` (4 calls, 11 + 45 + 2 + 2 names) | `DefineUnitAction[A ~struct{}]("ns::A")` (`G/action.go:482`) | Nothing. One `var X = DefineUnitAction[XPayload]("ui::Cancel")` per name. |
| `#[derive(Action)] #[action(namespace=…, no_json)]` (2: `Confirm`, `Enter`) | `DefineAction[A](ActionSpec{Name, Clone, Equal})`. Omitting `FromJSON` gives the NoJSON error, which equals `no_json` (`G/action.go:451-466`) | Nothing. |
| `Refineable` derive (Base calls `.refine()` 13 times, and `refine_style`; it never derives Refineable itself) | `StyleRefinement` = `authoring.StyleRefinement` with **5 fields** (`authoring/style.go:47`); `TextStyleRefinement` with 8 (`G/div.go:117`) | **RECREATE in core**: a full `StyleRefinement` mirroring `Style`, the text, background, border, radii and shadow fields, and the nested Edges/Size/Corners refinements, plus `Refine`. Upstream derives Refineable on 8 gpui types. Either hand-write it (≈400-500 lines) or generate it from `Style` with go/types. |
| `gpui_macros::style_helpers!`, `margin_/padding_/position_/overflow_/cursor_/border_/box_shadow_/visibility_style_methods!` (styled.rs:36-44, styles.rs 1,967 lines) | about 40 hand-written `DivElement` methods; `authoring.Styled[Self]` has 6 | **RECREATE in core.** Base itself calls about 175 distinct styled-family names. The upstream tables (box 24 + margin 14 + padding 14 + position 10 prefixes × up to 92 suffixes, corners 18×18, borders 14×34) expand to thousands of methods. Recommendation: a maintainer-side `go:generate` generator in gpui-go (≈700 lines, a port of the styles.rs tables) that emits methods on `authoring.Styled[Self]` and the Div, with the output committed. `research/09-fluent-authoring-methods.md:52` already allows maintainer generation; consumers never run it. Interim option: hand-write the ≈175 names Base uses (≈700 lines). Same gap as A-infra-overlays.md §0.3 and B1-controls.md. |
| `InteractiveElement` / `ParentElement` / `FluentBuilder` traits (not macros) | none (no `Interactivity[Self]`, `Children[Self]`, `When`) | RECREATE in core as mixins (≈600-900 lines); see A-infra-overlays.md. |
| `gpui_macros::derive_inspector_reflection` (styled.rs:76, inspector or debug only) | none | Defer. Inspector-only. |
| `#[gpui::test]` (659), `#[test]` (567) | `NewTestApp` with RunUntilParked/AdvanceClock (`G/testapp.go:25-69`) | No macro. `func TestX(t *testing.T)` plus TestApp. Still missing: VisualTestContext, SimulateKeystrokes and SimulateMouse (slice-05 CORE 7). |
| `gpui::accesskit::{Node::new, supports_action, Toggled, Orientation, Action::{Click,Focus}}` (≈70, mostly test probes; `write_a11y_info(&mut Node)` hook at `test_support.rs:379` and `inline_object.rs:224`) plus 18 `.aria_*` setters | `A11ySpec{Role, Label, Description, Value, Hidden, AuthorID, Focus}` and `A11yAction` (`G/accessibility.go:128,305`); 8 native roles | **RECREATE in core** (Go-side, no library). Grow `A11ySpec` with toggled, expanded, selected, orientation, numeric value/min/max/step, level, set position/size and row/column index/count; add an inspectable node snapshot for tests and `aria_*` setters. Est. ≈400-600 lines. Native UIA exposure of the new properties is blocked by the frozen ABI (B1-controls.md §4). No pure-Go AccessKit exists, and the C bindings are ruled out by `CGO_ENABLED=0`. |
| `gpui::{Hsla, Rgba, SharedString, …}` and the palette `IntoColor` | present in G/ | Nothing new. |
| sum_tree via its crate (not a gpui re-export in Base) | see §2 | hand-port (§2) |
| `smol::channel::{unbounded, Sender, Receiver}` (`async_util.rs:4`, a public re-export); `futures::Stream`/`noop_waker` (2 files) | goroutines, channels, `App.Spawn` (`G/executor.go:625`) | Nothing. Re-export a tiny unbounded `Channel[T]` (≈60 lines; Go channels are bounded, so add a slice-backed queue) or document `chan T`. |
| Base-local `macro_rules!` (`alert_part`, `alert_close_part`, `dialog_part`, `progress_part`, `table_part`, `table_cell`, `token_api`, `content_from_text`, `font_weight`) | - | Expand by hand, or write one generic part type each (B1 notes: "one generic part type replaces the macros"). |
| `local_name!` (42, html5ever) | - | `golang.org/x/net/html/atom` constants (deps-recommendation.md) |
| `include_str!` (16), `indoc!`, `json!`, `smallvec!`, `thread_local!`, `cfg!`/`#[cfg]` | - | `//go:embed`, raw strings, literals, slices, package vars or `sync`, build tags |
| Windows `SystemParametersInfoW` (reduce_motion, through the `windows` crate) | `syscall`/`x/sys/windows` (works with CGO_ENABLED=0) | ≈20 lines with `syscall.NewLazyDLL("user32.dll")`; no new module needed |

## 7. Summary

| Item | Recreate? | Reuse what | Est. Go lines | Notes |
|---|---|---|---|---|
| ropey Rope/RopeSlice | **yes** | nothing usable (best: deadpixi LGPL, zyedidia bytes-only, garland no UTF-16) | 1,300-1,700 + 600 tests; rope_ext +450 | persistent chunked B-tree, TextInfo{bytes, chars, utf16, lf}; O(1) clone required |
| zed-sum-tree | **yes** (or slice + prefix sums) | `G/listelement.go` deviation pattern | 700-900 + 300 tests (slice variant ≈150) | Apache-2.0 notice; `Bias` with the rope |
| lsp-types 0.97 | **yes** | none (go.lsp.dev has InlineCompletion but a ~230-module graph; lsprotocol-go is a copy reference) | 350-450 | follow 0.97 shapes, not 3.18 unions; `Data` as json.RawMessage |
| chrono | partly | stdlib `time` for all date math | NaiveDate/Time ≈150 + strftime ≈150 | prototype 45/45 parity; itchyny/timefmt-go (MIT, no deps) is the fallback |
| aho-corasick | **yes** (trivial) | `bytes` | ≈40 | prototyped |
| data-url | **yes** (trivial) | `encoding/base64` | ≈140 | prototyped; passes Base's test |
| derive(IntoElement) | no | `RenderOnce` + `TryIntoElement` | 0 | |
| actions! / derive(Action) | no | `DefineUnitAction` / `DefineAction` | ≈1 per action | `no_json` = omit FromJSON |
| Refineable / StyleRefinement | **yes (core)** | `authoring.Optional`, `merge` | 400-500 (or generated) | today 5 fields |
| styled method macros | **yes (core)** | `authoring.Styled[Self]` embedding | generator ≈700 (+ generated output) or ≈700 hand-written subset | go:generate is maintainer-only, which is allowed |
| Interactive/Parent/FluentBuilder | **yes (core)** | Div listeners | 600-900 | see A-infra-overlays.md |
| accesskit Node/properties | **yes (core, Go-side)** | `A11ySpec`, `A11yAction` | 400-600 | native exposure blocked by ABI |
| smol channel / futures | no | goroutines/chan + gpui-go executor | ≈60 (unbounded channel) | |
| #[gpui::test] | no macro | `TestApp` | (VisualTestContext is a separate core ticket) | |
| derive_inspector_reflection | no (defer) | - | 0 | inspector feature only |
| Base-local macro_rules | expand | generics | small | |
| windows reduce-motion | no module | `syscall` | ≈20 | |

## Unverified / caveats
- The go.lsp.dev module-graph cost was inferred from its go.mod `tool`/`require` blocks; it was not measured with `go mod graph` in a consumer.
- myleshyson/lsprotocol-go was checked only for `type InlineCompletion*` names (10 hits); its union shapes were not reviewed.
- The total styled-method count is an upper bound from the table sizes; not every prefix gets every suffix.
- Rope and sum-tree line estimates come from the ropey/zed source sizes and the API subset; no rope or sum-tree was prototyped.
- Web search: [garland](https://pkg.go.dev/github.com/phroun/garland), [ajwerner/btree](https://beta.pkg.go.dev/github.com/ajwerner/btree), [google/btree generic](https://fuchsia.googlesource.com/third_party/github.com/google/btree/+/8e29150ba321eef204059de2ab494f179b6cff2c/btree_generic.go).
