# GPUI Base: dependency choices for each Rust crate (add a Go module or hand-port)

Researched 2026-10-19. Base source checked: `upstream-base/ce/crates/base/src` (191 `.rs` files, about 108k lines).
Usage counts come from `grep` of `use` lines and `crate::` paths (rough numbers). Repo facts come from the GitHub API on 2026-10-19.
gpui-go today: Go 1.27, `CGO_ENABLED=0`, go.mod has **no requirements**.

Legend: **STDLIB** = Go standard library. **X-MODULE** = `golang.org/x/*`. The Go team maintains these, but each one still adds a go.mod/go.sum entry. **THIRD-PARTY** = a module from outside the Go team. **HAND-PORT** = write it in-repo. **NOT NEEDED** = a Go language feature or an existing gpui-go piece covers it.

## Decision table

| Rust crate | Base usage size | Recommendation | Go option | License | Faithfulness risk |
|---|---|---|---|---|---|
| markdown (markdown-rs 1.0, mdast, GFM, math, frontmatter, MDX) | **Large**: 7 files; `text/format/markdown.rs` is 2.3k lines with 57 tests; uses `to_mdast`, about 34 `mdast::Node` variants, byte offsets from `position()` (start and end) | **THIRD-PARTY `github.com/yuin/goldmark` v1.8.x** plus an in-repo goldmark-to-Base-node adapter. Hand-port math and frontmatter. Defer MDX. Fallback if tests diverge too much: HAND-PORT markdown-rs (very large) | goldmark v1.8.6 (2026-09); v2.1.6 exists but upstream calls it "early stages" | MIT | **HIGH**: the AST shape differs from mdast. Node end offsets must be rebuilt. Math, frontmatter and MDX are not built in. Many of the 57 tests need rewritten expectations |
| html5ever 0.27 + markup5ever_rcdom 0.3 | **Medium**: 4 files (`html.rs` 0.8k lines, `html5minify` 0.9k lines); only `parse_document` into RcDom, then a node and attribute walk | **X-MODULE `golang.org/x/net/html`** | x/net (only the `html` and `html/atom` packages are imported) | BSD-3 | **LOW**: both follow the WHATWG tree-construction algorithm and both test against html5lib, so the DOMs should match. Note: x/net's go.mod requires x/crypto, x/sys, x/term and x/text, so their go.sum lines appear even though they are not compiled in |
| syntect 5.3 | **None in `src/`**. Only `examples/showcase/syntect_highlighter.rs` uses it. Base exposes a pluggable `InputHighlighter` trait | **NOT NEEDED** for the library. For the showcase only, optionally THIRD-PARTY `github.com/alecthomas/chroma/v2` | chroma v2 (v3 is alpha) | MIT | N/A for Base. For the showcase it is MEDIUM: chroma uses Pygments-style lexers and token types, not Sublime scopes, so colors differ from syntect |
| ropey (=2.0.0-beta.1) | **Large**: 22 files; `Rope` and `RopeSlice` are the text-buffer type of every input. `rope_ext.rs` (0.7k lines, 15 tests) wraps a byte-indexed API with `LineType::LF` | **HAND-PORT** a byte-indexed B-tree-of-chunks rope with only the subset Base uses | none suitable: the best Go ropes are deadpixi/rope (LGPL-2.1, 2022), kebe7jun/ropee (2023), and others from 2014-2018 | (ropey is MIT if you consult it) | LOW if the port follows ropey 2's semantics (byte offsets, LF line breaks). The `rope_ext` tests port directly |
| regex 1 | **Small**: 3 files; HEX input pattern, `InputState::pattern(Regex)`, and two indent patterns in `language_config.rs` | **STDLIB `regexp`** | `regexp` (RE2) | BSD-3 | LOW: both engines are non-backtracking, leftmost-first, with no lookaround or backrefs. One catch: `\d \w \b \s` are Unicode-aware in Rust but ASCII-only in Go. Base's patterns are ASCII, so this is fine, but public-API users would see the difference |
| aho-corasick 1.1 | **Tiny**: 1 file (`search.rs`); a single pattern with `ascii_case_insensitive`, non-overlapping `stream_find_iter` | **HAND-PORT** (about 30 lines: an ASCII-folded `bytes.Index` loop) | (cloudflare/ahocorasick is BSD-3 if multi-pattern search is ever needed) | n/a | LOW: for one pattern, Aho-Corasick's standard non-overlapping matches are the same as a repeated leftmost search |
| chrono 0.4 | **Small**: `calendar.rs` and `time_field.rs`; `NaiveDate`, `NaiveTime`, `Weekday`, `Local::now().date_naive()`, `Duration::days`, `.format(strftime)` | **STDLIB `time`** plus a small HAND-PORT of a strftime subset | `time` | BSD-3 | LOW-MED: the date arithmetic maps directly. The public `format` string is chrono `%Y-%m-%d` syntax, so a strftime-subset formatter is needed to keep the API and tests |
| palette 0.7 | **Tiny**: 4 files; only `IntoColor` (Hsla to Rgba) and `WithAlpha`, through gpui's color types | **NOT NEEDED**: use gpui-go's existing Hsla/Rgba conversions | (go-colorful, MIT, if more color spaces are ever needed) | n/a | LOW, provided gpui-go's HSL-to-RGB conversion matches gpui's |
| rust_decimal (optional `decimal` feature) | **Tiny**: one marker `impl PlotValue for Decimal` in `plot/scale.rs` | **NOT NEEDED**: drop the feature; users can implement the `PlotValue` interface themselves | shopspring/decimal (MIT, 7.5k stars, v1.6.0 2026-10) if it must ship | MIT | none |
| unicode-segmentation 1.12 | **Small but core**: 3 files; `split_word_bound_indices` (word movement), `split_word_bounds` and `graphemes(true)` (inline flow) | **THIRD-PARTY `github.com/clipperhouse/uax29/v2`**. The zero-dependency alternative is to HAND-PORT the algorithm plus tables generated from UCD | uax29 v2.7.0: Unicode 17, no dependencies | MIT | LOW-MED: same UAX #29, but the Unicode version can differ from unicode-segmentation 1.12. `rivo/uniseg` is Unicode 15.0 with its last commit in 2024, so not chosen. x/text has **no** grapheme or word segmentation |
| serde / serde_json | **Medium**: about 8 files of derives; `serde_json::Value` inside `PanelInfo`; dock-layout fixtures (`fixtures/layout.json`) | **STDLIB `encoding/json`** with hand-written `MarshalJSON`/`UnmarshalJSON` where serde enum tagging or renames need them | `encoding/json` (`json.RawMessage` for `Value`) | BSD-3 | MED: serde's enum representations and `#[serde(default/rename)]` must be reproduced by hand. With that done, the fixture round-trip tests can be ported |
| schemars 1 | **Tiny**: `#[derive(JsonSchema)]` on about 17 types (theme, geometry, scrollbar, list settings). No `schema_for` call in Base | **NOT NEEDED** for the port (hand-write a schema later if theme tooling wants one) | (invopop/jsonschema, MIT, if required) | n/a | none: no behavior depends on it |
| lsp-types 0.97 | **Medium, data only**: 14 files, about 25 types (Position, Range, Diagnostic*, CompletionItem, Hover, CodeAction, SemanticTokens*, ColorInformation, TextEdit, LocationLink, ...) | **HAND-PORT** only those structs, with JSON tags | go.lsp.dev/protocol (BSD-3) pulls in jsonrpc2, uri and go-json-experiment | n/a | LOW: plain data. Keep LSP's UTF-16 `Position` semantics |
| sum-tree (zed-sum-tree 0.2) | **Small**: 2 trees (`diagnostics.rs`, `text_wrapper.rs`); `Bias`, `SeekTarget`, `Dimensions`; `Bias` is also used in 6 movement files | **HAND-PORT** a generic `SumTree[T, S]` (Zed's sum_tree is Apache-2.0). Alternatively, follow gpui-go's existing "bounded deviation" approach of a slice plus prefix sums, as done in `gpui/listelement.go` | none in Go | Apache-2.0 (source) | LOW for behavior; only complexity differs if slices are used |
| smallvec | 62 sites, a performance detail only | **NOT NEEDED**: use Go slices (optionally `make([]T, 0, N)`) | language | n/a | none |
| itertools 0.13 | 1 file (`plot/scale/linear.rs`) | **NOT NEEDED**: loops, plus stdlib `slices` and `iter` | language / `slices` | n/a | none |
| num-traits 0.2 | 2 files; the `PlotValue: Num + ToPrimitive + Zero` bound | **NOT NEEDED**: a generic constraint (`~int… \| ~float…`) or a small interface | generics (`cmp.Ordered`-style) | n/a | none |
| anyhow | 10 files | **NOT NEEDED**: `error`, `fmt.Errorf("%w")`, `errors.Join` | stdlib `errors`, `fmt` | n/a | none |
| futures / smol (+ async-channel on wasm) | `smol::channel::unbounded` re-export in `async_util.rs`; `futures::Stream` in 2 files | **NOT NEEDED**: Go channels and goroutines, or gpui-go's executor | language | n/a | LOW; keep task ordering on the gpui-go executor |
| tracing 0.1 | 5 files, 10 macro calls (9 `warn!`, 1 `trace!`) | **STDLIB `log/slog`** | `log/slog` | BSD-3 | none |
| data-url 0.3 | 2 files; `DataUrl::process(url)` to decode a base64 image | **HAND-PORT** (about 40 lines using `encoding/base64` and `net/url` unescaping) | stdlib (`vincent-petithory/dataurl` exists but is not worth a dependency) | n/a | LOW: follow the WHATWG fetch "data: URL processor" for the base64 path |
| web-time | 15 files; `Instant` and `Duration` | **NOT NEEDED / STDLIB `time`** | `time` | BSD-3 | none |

**Net effect if accepted:** go.mod gains **1 X-module** (`golang.org/x/net`) and **2 third-party modules** (`github.com/yuin/goldmark`, `github.com/clipperhouse/uax29/v2`). All three are pure Go with no cgo, and goldmark and uax29 have no dependencies of their own. Chroma would be a dev/example-only module. To stay at zero third-party modules, the cost is hand-porting uax29 (moderate) and markdown (very large).

## Cross-cutting: golang.org/x vs true third-party

- `golang.org/x/net/html` is developed in the Go repo's review process and licensed BSD-3 like the stdlib. It is still a module requirement, so it **breaks "zero go.mod requirements"** even though it brings no new trust domain. Recommendation: allow x-modules as a policy class separate from third-party.
- x/net's go.mod lists x/crypto, x/sys, x/term and x/text. Module graph pruning keeps them out of the build, but go.sum gets entries. Alternatives: vendor or fork `html` into `internal/` (BSD-3 allows it; keep the notice), or accept the entries.

## Per-dependency notes

### markdown (markdown-rs): the only hard call
- Base use: `markdown::to_mdast(source, &ParseOptions::gfm())` with `math_text/flow = true`, optional `frontmatter`, and optional MDX JSX/expression constructs (MDX turns off raw HTML). It re-parses "unclaimed math" spans with math turned off, using `position().start/end.offset`. Each Base node carries a source `Span` (start and end byte offsets), which selection and source-range mapping depend on. 57 tests, many asserting source-range maps (`selected_source_range_maps_math_block_body`, ...).
- **goldmark** v1.8.6 (2026-09-03) and v2.1.6 (2026-09-27). About 5.1k stars, MIT, pure Go, no dependencies, CommonMark 0.31.2. GFM (table, strikethrough, linkify, tasklist) and footnotes are built in. Math and frontmatter come only from third-party extensions (goldmark-katex, goldmark-mathjax from 2021, goldmark-frontmatter), which are weakly maintained, so those should be hand-written as goldmark extensions. **No MDX.**
- Source positions: in v1, block nodes have `Lines()` segments and inline text has `Segment`, but most inline containers have no offsets, so spans must be derived from children or delimiters. In v2, "all nodes hold the start position" (`Pos()`), but there is still **no end offset**. v2 is self-described as early (sources: README).
- markdown-rs itself is about 2.2 MB of Rust (GitHub language bytes, including tests), MIT, 100% CommonMark plus GFM plus MDX. A faithful hand-port would be the biggest single item in the Base port.
- Suggested path: goldmark v1 now behind an internal `mdparse` seam that produces Base's own node tree, with hand-written math and frontmatter extensions. Port the 57 tests at the level of the Base node tree and source spans, and record divergences in the conformance ledger. Revisit a markdown-rs port only if MDX or exact mdast parity is required.
- https://github.com/yuin/goldmark · https://github.com/wooorm/markdown-rs

### html5ever to x/net/html
- Base parses a whole document (`parse_document`, default `ParseOpts`) into RcDom and walks `NodeData::{Element,Text,...}` plus attributes (`style`, color). The minifier is a fork of the `html5minify` crate (899 lines) and walks the same DOM.
- `x/net/html` provides `html.Parse` to `*html.Node` (Type, Data, Attr, FirstChild/NextSibling) and `atom` names in place of `local_name!`. It runs the html5lib tree-construction tests, skipping a few cases that are outside the parse algorithm (`<plaintext>` in select/table, `selectedcontent`). Its trees match html5ever's for the inputs Base handles.
- https://pkg.go.dev/golang.org/x/net/html · https://go.googlesource.com/net/+/e0495509cf55f38c29ab7df4182e0a06de64c055%5E%21/

### syntect / chroma
- Not a Base library dependency; it appears only in the showcase example. If a Go showcase wants highlighting, chroma v2 is MIT, about 5.1k stars, and active (v3.0.0-alpha.5 was released 2026-07). It depends on `dlclark/regexp2`. Its token types are Pygments-style (`Keyword`, `NameFunction`), not TextMate/Sublime scopes, so theme colors will not match syntect. https://github.com/alecthomas/chroma

### ropey
- Base pins ropey `=2.0.0-beta.1`, which is byte-indexed and selects line breaks with `LineType::LF`. The heavy use goes through `rope_ext.rs` (`offset_to_point` ×32, `point_to_offset`, `line_len`, `slice`, `replace`, `chars_at`, `len` …). There is no maintained, permissively licensed Go rope: the GitHub search's top hits are deadpixi/rope (LGPL-2.1, 2022), kebe7jun/ropee (2023), chewxy/skiprope (2017), and vinzmay/go-rope (2014). Hand-port a chunked B-tree, with a line-count summary per node, for only the needed API. https://github.com/cessen/ropey

### regex
- Go `regexp` and Rust `regex` share the finite-automata design, leftmost-first semantics and missing features (no backrefs or lookaround). Differences: Rust's Perl classes are Unicode by default while Go's are ASCII, and flag syntax overlaps but is not identical (`(?x)` is unsupported in Go). This is fine for Base's patterns. Document the difference on the public `Pattern(*regexp.Regexp)` API.

### aho-corasick
- One literal pattern with an ASCII case-insensitive flag. Hand-port it as a fold-aware search over the rope's bytes. If multi-pattern search arrives later, cloudflare/ahocorasick is BSD-3 with 727 stars, but it is not needed now.

### chrono
- `NaiveDate` maps to a `civil.Date`-like struct or `time.Time` at UTC midnight. `Weekday`, `Local::now()` and `Duration::days` map to `time`. `Date::format(format)` takes chrono strftime strings, so hand-port `%Y %m %d %b %B %a %A %H %M %S %p %I` (only the specifiers that Base tests and docs use).

### palette
- Used only through gpui's `Hsla`/`Rgba` trait impls. gpui-go already owns its color types, so no dependency is needed. go-colorful (MIT, 1.26k stars, v1.4.1 2026-08) is there if Oklab/Lab are ever required.

### unicode-segmentation
- Used for word-boundary movement in the input and for word and grapheme splitting in inline flow. The Go stdlib has no UAX #29, and x/text does not either (it has norm, width and bidi, but no segmenter).
- clipperhouse/uax29/v2 v2.7.0: MIT, no dependencies, Unicode 17, active (pushed 2026-02); it provides `graphemes`, `words` and `sentences`. rivo/uniseg v0.4.7: MIT, 735 stars, Unicode 15.0, last commit 2024-04, so not chosen.
- Check that word-bound output matches unicode-segmentation for the strings in Base's tests. Both libraries follow UAX #29 exactly, so differences would only show up for code points that changed between Unicode versions.
- https://github.com/clipperhouse/uax29 · https://github.com/rivo/uniseg

### serde / serde_json / schemars
- `encoding/json` covers it. The work is matching serde's wire shapes (externally tagged enums, `rename_all`, `default`) with custom marshalers, so that `fixtures/layout.json` round-trips byte-for-byte where tests assert it. `serde_json::Value` maps to `json.RawMessage` or `any`. Whether `encoding/json/v2` has left `GOEXPERIMENT` by Go 1.27 is **unverified**, so do not rely on it. schemars only derives schemas that Base never emits.

### lsp-types
- About 25 plain types. go.lsp.dev/protocol (BSD-3, 138 stars, v1.0.1 2026-06) would add jsonrpc2, uri and go-json-experiment for structs worth about 300 lines. Hand-port them.

### sum-tree
- Zed's `sum_tree` crate is Apache-2.0, the same license as gpui-go, so a direct generic port with attribution in THIRD_PARTY_NOTICES is clean. gpui-go has already replaced SumTree with slices in `gpui/listelement.go` as a recorded deviation; the same approach would work for `text_wrapper` (line summaries) and diagnostics if the sizes are small.

### Language-covered: smallvec, itertools, num-traits, anyhow, futures/smol, web-time, tracing
- These all map directly: slices; `slices`/`iter` and loops; generic type constraints; `error`/`errors.Join`; channels and the gpui-go executor; `time`; `log/slog`.

### data-url
- `DataUrl::process` then `decode_to_vec`. Hand-port it: parse `data:[mediatype][;base64],payload`, percent-decode, and decode with `base64.StdEncoding`, tolerating whitespace and missing padding the way the WHATWG forgiving-base64 rules require.

### Unverified / caveats
- Star counts and dates come from the GitHub API on 2026-10-19. The chroma and shopspring licenses show as NOASSERTION in the API, but their LICENSE files are MIT (checked).
- goldmark v2 stability and its end-position support are taken from the README; the v2 source was not read.
- Rust regex vs Go RE2 behavior is from general knowledge and was not re-checked against current docs.
- The match between uax29 and unicode-segmentation 1.12 has not been tested.
