# GPUI Base deps, part 2: replacing unicode-segmentation and html5ever with Go libraries (executed evidence)

Date: 2026-10-19. Go 1.27.1, `CGO_ENABLED=0`, rustc/cargo 1.97.0.
All prototypes live in `C:/Users/ADMIN/AppData/Local/Temp/gpuibase-proto/` (outside the repo; the gpui-go go.mod was not touched).
This follows up `deps-recommendation.md` and corrects two points in it (see "Corrections" at the end).

## Resolved versions (from `upstream-base/ce/Cargo.lock`)

| Crate | Base `Cargo.toml` | Locked | Unicode / spec |
|---|---|---|---|
| unicode-segmentation | `1.12.0` | **1.13.3** | `tables.rs`: `UNICODE_VERSION = (17, 0, 0)`. 1.12.0 was Unicode 16.0 (checked in the 1.12.0 .crate) |
| html5ever | `0.27` | 0.27.0 (0.29.1 is also locked, for another crate) | WHATWG parser, markup5ever 0.12.1 |
| markup5ever_rcdom | `0.3.0` | 0.3.0 | |

The Go standard library's `unicode.Version` on Go 1.27.1 is `17.0.0`.

## (a) Segmentation

### What Base uses

- `input/base/state.rs:1460` `previous_start_of_word_at`: `split_word_bound_indices(left).rfind(|s| !s.trim_start().is_empty())`, then `token_boundary(.., Left)`.
- `input/base/state.rs:1477` `next_end_of_word_at`: `split_word_bound_indices(right).find(non-blank)`, end offset, then `token_boundary(.., Right)`.
- `text/inline_flow.rs:1483`: `split_word_bounds()`, and `graphemes(true)` for words wider than the wrap width.
- `input/base/inline_tokens.rs:198`: `grapheme_indices(true)` to validate token range boundaries (error `InvalidBoundary`).
- `text_boundary.rs` does **not** use the crate. It is an 82-line hand-written ASCII/Latin/Cyrillic character-class scan (`word_range_at`), so it ports as-is with no library.

Semantics: `split_word_bound_indices` yields **every** UAX #29 segment, including whitespace runs and each punctuation mark, not just "words". `uax29/v2/words.FromString` has the same shape: its iterator returns every segment, with `Start()`/`End()` byte offsets. Executed example:
`"foo  bar, baz's 3.14 @x!"` gives `"foo" "  " "bar" "," " " "baz's" " " "3.14" " " "@" "x" "!"`.
So the Base call sites map 1:1: iterate the segments, apply `strings.TrimLeftFunc(seg, unicode.IsSpace) == ""` as the blank test, then take the first or last non-blank segment. `rivo/uniseg.FirstWordInString` also returns all segments.

### Candidates

| Module | Version / date | License | Unicode | Deps | Maintenance |
|---|---|---|---|---|---|
| github.com/clipperhouse/uax29/v2 | v2.7.0, 2026-02-16 (proxy.golang.org) | MIT | 17.0.0 (headers of `words/trie.go` and `graphemes/trie.go`) | none (go.mod has no `require`) | active; 124 stars; not archived |
| github.com/rivo/uniseg | v0.4.7, 2024-02-08 | MIT | 15.0.0 | none | last push 2024-05 |
| golang.org/x/text | — | — | — | — | has no UAX #29 word or grapheme segmenter |

### Commands run

```
cd gpuibase-proto/segment   # go mod init proto/segment; go get uax29/v2@latest uniseg@latest
curl -O https://www.unicode.org/Public/{17.0.0,15.0.0}/ucd/auxiliary/{Word,Grapheme}BreakTest.txt
go build -o seg.exe . && ./seg.exe conf ucd17/WordBreakTest.txt word   (and the other 3 files)
./seg.exe base                                   # Base test cases
go run ./gen ucd17/*.txt > corpus.jsonl          # conformance strings + Base strings + 50,000 seeded random mixed-script strings
rust-oracle/target/release/oracle.exe < corpus.jsonl > rust.jsonl   # unicode-segmentation =1.13.3 (split_word_bound_indices, grapheme_indices(true))
./seg.exe dump corpus.jsonl > go.jsonl; python diff
```

### Results

| Test | uax29/v2 v2.7.0 | uniseg v0.4.7 |
|---|---|---|
| WordBreakTest-17.0.0 | **1944/1944** | 1939/1944 |
| GraphemeBreakTest-17.0.0 | **766/766** | 749/766 |
| WordBreakTest-15.0.0 (control) | 1821/1823 | 1823/1823 |
| GraphemeBreakTest-15.0.0 (control) | 601/602 | 602/602 |
| Base word-motion cases (9) | **9/9** | 9/9 |
| Base grapheme-boundary cases (10) | **10/10** | 10/10 |
| Differential vs Rust 1.13.3, graphemes (52,719 strings) | **52,719/52,719 identical** | — |
| Differential vs Rust 1.13.3, word bounds (52,719 strings) | **52,714/52,719 identical** | — |

Each library passes 100% of the conformance file for its own Unicode version. The failures in the other version's file are the expected changes between versions (for example, the GB9c/ZWJ and ExtPict rules).

Base cases. Expected values come from the Rust test assertions. For token tests, the raw value before `token_boundary` is used.
- `test_multi_cursor_word_movement`: `"one two three"` next(2)=3, next(5)=7, prev(3)=0, prev(7)=4.
- `test_masked_input_collapses_word_boundaries` (unmasked half): `"aaa bbb ccc"` prev(11)=8.
- `test_undo_manager_word_and_line_deletes_do_not_coalesce`: `"one two three\nfour five"` prev(13)=8.
- `test_word_delete_undo_restores_caret`: `"hello"` prev(5)=0.
- `test_inline_token_edit_history_and_validation`: `"问 @alice!"` raw next(4)=5 and raw prev(10)=5. `token_boundary` then widens these to 10 and 4, matching the asserts.
- Graphemes: `"a\u0301"` at offset 1 is not a boundary (the `InvalidBoundary` assert). Also checked: `"@a@a"`, `"中@b\n后面"`, a ZWJ family emoji, regional-indicator pairs, and `\r\n`.

The 5 word-bound differences from Rust all have the same shape: AHLetter + (MidLetter|MidNumLet such as `'` or `.`) + ZWJ + Extended_Pictographic, for example `"q.\u200d👨"`.
- Rust 1.13.3 gives `[q.‍👨]` as one segment.
- uax29 and uniseg (two independent implementations) both give `[q][.‍👨]`.

By the UAX #29 rules, WB6 needs an AHLetter after the `.`. ZWJ is absorbed by WB4, and 👨 is not an AHLetter, so the rules call for a break after `q`. The Rust crate deviates here, which looks like a WB4/WB3c lookahead quirk. This input is unreachable in practice: a letter, then an apostrophe or period, then an emoji ZWJ sequence. Record it as a known deviation in the conformance ledger. Do not emulate it.

### Can the core's `Clusters()` cover graphemes?

No, not as a replacement. `gpui/text.go:1110` `(*WrappedLine).Clusters()` and `ClusterBefore`/`ClusterAfter` need a live, shaped `WrappedLine` from the native text service. Base needs graphemes in two places that have no layout object:
- `InputContent::with_token` validation runs with no window.
- `inline_flow` splits graphemes *before* measuring them.

Backend cluster definitions can also differ from UAX #29 (DirectWrite/CoreText vs ICU). Keep `Clusters()` for caret stepping in the rendered text and use uax29 for string-level segmentation.

## (b) HTML: html5ever 0.27 + RcDom vs golang.org/x/net/html

### What Base uses

- Both `html.rs` and `html5minify/mod.rs` call `parse_document(RcDom::default(), ParseOpts::default()).from_utf8().read_from(..)`.
  - `html.rs` parses the *minified* output again.
- They then walk `NodeData::{Document, Doctype, Text, Comment, Element{name, attrs}, ProcessingInstruction}` and use the children and siblings of each node, plus `name.local`.
- The minifier's output depends on the exact implicit `<html>/<head>/<body>` structure and on where whitespace text and comments are placed. Examples from its tests: a comment after `</html>` goes onto the Document, and `<body>` is implied. Exact DOM parity therefore matters.
- x/net/html covers all of this: `*html.Node{Type, DataAtom, Data, Namespace, Attr[]{Namespace,Key,Val}, FirstChild, NextSibling, Parent}`. `atom.Td`, `atom.Mark` and so on replace `local_name!`. Template contents are not used by Base.

### Module facts

- golang.org/x/net v0.61.0, 2026-10-09, BSD-3.
- `go list -f '{{.Imports}}'`:
  - `html` imports only `bufio bytes cmp errors fmt io iter slices strconv strings unicode/utf8` plus `golang.org/x/net/html/atom`.
  - `atom` imports nothing.
- **Vendoring into `internal/` is clean.** Copy `html/*.go` and `html/atom/*.go` (non-test, without `atom/gen.go`; 8,432 lines, of which `entity.go` is a 2,252-line table) and keep the BSD-3 notice in THIRD_PARTY_NOTICES.
- If used as a module instead: after `go mod tidy` in the prototype, **go.sum has only the 2 x/net lines**. Module graph pruning means x/crypto, x/sys, x/term and x/text get no go.sum entries, even though they appear in x/net's go.mod.

### Commands run

```
cd gpuibase-proto/html     # go mod init proto/html; go get golang.org/x/net/html@latest (v0.61.0)
go test -count=1 -json -run 'TestParser$' golang.org/x/net/html     # x/net's own html5lib run
rust-html/: html5ever =0.27.0 + markup5ever_rcdom =0.3.0 oracle (cargo build --release); dumps the RcDom tree in html5lib-like form
go build -o h.exe . ; ./h.exe extract testdata/html5lib-tests/tree-construction/*.dat > h5l.jsonl   # document cases (no fragment, scripting on)
python base_html.py > base_html.jsonl        # 36 inputs: every HTML string in html.rs + html5minify tests + Base-relevant extras
./h.exe dump < X.jsonl ; htmloracle.exe < X.jsonl ; python diff (byte-identical canonical dumps)
```

Canonical dump: one line per node with indentation, namespace prefixes (`svg `, `math `), attributes sorted, text in quotes, comments, doctype name/public/system, and a `content` node for template contents.

### Results

| Test | Result |
|---|---|
| x/net/html `TestParser` (html5lib tree-construction + x/net's own `testdata/go`) | **1783 pass, 0 fail** (1778 html5lib `#data` cases; 6 skipped by x/net's `parseTestBlacklist`: 2 template-in-foreign-content cases and 4 `<selectedcontent>` "element insertion steps" cases) |
| Base inputs (36): html.rs tests (`cleanup_html`, `mark`, `keep_spaces`, `image` x2), every html5minify `test_omit_tags` input, and the minified forms that html.rs re-parses, plus tables, lists, headings, br, blockquote, entities, misnested `<b><i>`, implied `</p>`, and doctype | **36/36 identical trees** vs html5ever 0.27 |
| All html5lib document cases, x/net vs html5ever 0.27 | **1534/1559 identical** |

All 25 differing html5lib cases involve `<select>`. For example, `<select><svg></svg></select>` keeps `<svg>` in x/net, but html5ever 0.27 drops it. These cases come from the 2025 WHATWG "customizable select" parser change (`<select>` may now contain `<button>`, `<div>`, svg, math and `<selectedcontent>`). x/net v0.61 implements the change and passes 21 of those cases against the html5lib expectations. html5ever 0.27 (2024) predates it.
- This difference is a spec-version difference, not a bug in the port.
- Base never renders `<select>` from HTML: `parse_node` falls through to its children for unknown tags.
- Record it in the ledger as "newer WHATWG select parsing". If byte-for-byte parity with 0.27 is ever required, pin an older x/net or patch `inSelectIM` in the vendored copy.

Both parsers are WHATWG implementations tested against html5lib.

## Verdicts

| Area | Verdict | Notes |
|---|---|---|
| Graphemes (`graphemes(true)`, `grapheme_indices`) | **USE LIBRARY: `github.com/clipperhouse/uax29/v2` v2.7.0 as a module** | 100% Unicode 17 conformance; identical to Rust 1.13.3 on 52,719 strings |
| Word bounds (`split_word_bound_indices`, `split_word_bounds`) | **USE LIBRARY: same module, `words` package** | 100% Unicode 17 conformance; 9/9 Base cases; 5/52,719 differences, all a Rust-crate quirk on letter+`'`/`.`+ZWJ+emoji, where uax29 follows the spec |
| `text_boundary.rs` | hand-port (no dependency) | 82 lines, no library involved |
| HTML parse (html.rs and html5minify) | **USE LIBRARY: `golang.org/x/net/html`**, either as a module (2 go.sum lines) or vendored into `internal/html` (stdlib-only imports, about 8.4k lines, BSD-3 notice) | 36/36 Base inputs give identical trees; 1534/1559 html5lib documents identical; differences only in the newer `<select>` spec |
| html5minify (899 lines) and html.rs walker (821 lines) | port the Base code itself on top of `*html.Node` | These are Base logic, not dependencies |

Effort if we chose to recreate:
- **UAX #29 words and graphemes:** a UCD table generator (about 250 lines), generated property tables (about 1.5 to 3k lines of data), and two rule state machines (about 500 to 700 lines, WB4 skipping and the regional-indicator, emoji and ZWJ lookahead being the tricky parts). Roughly 3 to 5 engineer-days to reach 100% on the conformance files. For scale, uax29's words+graphemes+internal is 6,829 non-test lines.
- **HTML:** the WHATWG tokenizer and tree builder are about 8k lines and take weeks to reach html5lib parity. Not justified when x/net is BSD-3 and stdlib-only.

If the project insists on "zero third-party modules", vendor rather than hand-write:
- uax29 is MIT with no dependencies and can be copied into `internal/uax29` (words, graphemes and internal/iterators) with its MIT notice.
- x/net/html can be copied into `internal/html` with its BSD-3 notice.

## Corrections to deps-recommendation.md

1. Base resolves unicode-segmentation **1.13.3 (Unicode 17)**, not 1.12 (Unicode 16). Because of that, uax29 v2.7.0 is on the same Unicode version, and the "Unicode version can differ" risk does not apply.
2. A module dependency on x/net does **not** add x/crypto, x/sys, x/term or x/text lines to go.sum (verified with `go mod tidy`: only 2 lines).

## Unverified

- The Rust word-bound quirk was not checked against ICU or traced to a specific upstream issue. Two Go implementations agree with each other, and my reading of WB6 with WB4 favors them.
- html5ever 0.27's own html5lib skip list was not fetched. The differential test against x/net stands in for it.
