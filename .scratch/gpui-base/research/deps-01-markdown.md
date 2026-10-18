# deps-01: markdown, use a Go library or hand-port markdown-rs?

**Verdict: USE A LIBRARY.** Use `github.com/yuin/goldmark` v1.8.6 (MIT, no deps) behind an in-repo adapter that produces mdast. Hand-write the math and frontmatter extensions (about 250 lines). Defer MDX. Production adapter size: about 1.2–1.5k Go lines plus a Go `mdast` package, which either option needs.
Evidence: a differential test against real markdown-rs 1.0.0 output, run with Base's exact `parse_options()`.
- **Base's own inputs:** 80/82 inputs match exactly. Of the 53 parser-dependent tests, 51 match. The 2 misses are both MDX.
- **CommonMark spec plus GFM fixtures:** 565/700 match exactly, and only 8/700 have a different tree shape.
A hand-port is about 14k lines of Rust code, which would make it the largest single item in the Base port.

## 1. What Base needs from the parser (read from source)
- **Options** (`markdown_ext.rs:392-409`): `ParseOptions::gfm()` (tables, tasklists, strikethrough, autolink literals, footnotes, GFM tag filter), with `math_text` and `math_flow` always on and `frontmatter` optional.
  - `mdx()` turns off `html_flow`/`html_text` and turns on `mdx_expression_*` and `mdx_jsx_*`.
  - The "unclaimed math" path re-parses a node's source slice with math off (`markdown.rs:72-96`).
- **Node kinds consumed** (`ast_to_node`, `parse_paragraph`):
  - Paragraph, Blockquote, List(ordered, start), ListItem(spread, checked), Break, Code(lang, value), Heading(depth), Math, Html, MdxFlowExpression, Yaml, Toml, MdxJsx{Text,Flow}Element, ThematicBreak, Table(align)/TableRow/TableCell, FootnoteDefinition(identifier), Definition(identifier, url, title).
  - Inline: Text, Emphasis, Strong, Delete, InlineCode, Link(url, title), Image(url, alt, title), InlineMath, MdxTextExpression, Html, FootnoteReference, LinkReference(identifier, label).
- **Positions:** `start.offset` and `end.offset` are read for **every** node: `new_span`, `mapped_inline`, `paragraph.set_span`, `MarkdownParseContext::node_source`, and `shift_positions` on re-parse.
  - Text spans must cover the raw source, including escapes, entities and soft-break line prefixes. `source_segments` re-aligns rendered text inside `[start, end)`.
  - Code, InlineCode and Math spans must include the fences and backticks, because `code_source_segments` strips them itself.
  - Row and cell spans are never read.
- **Public API:** plugins receive `&mdast::Node` (`markdown_ext.rs:21,57`). The Go port needs its own `mdast` type in either case.

## 2. Candidates (verified 2026-10-19 through the Go proxy, crates.io and the GitHub API)
| Lib | Version / date | License | Positions | GFM | Math | Frontmatter | MDX | Verdict |
|---|---|---|---|---|---|---|---|---|
| yuin/goldmark v1 | v1.8.6, 2026-09-03; pushed 2026-10-01; 5.06k stars | MIT, no deps | v1.8.0+ gives **every node** `Pos()` (start). Block nodes have `Lines()`, Text has `Segment{Start,Stop}`, RawHTML has `Segments`. **No end offset on containers.** | built in: table, strikethrough, linkify, tasklist, footnote | no | no | no | **use** |
| yuin/goldmark/v2 | v2.1.6, 2026-09-27 | MIT | start only ("all nodes hold the start position"). README: "still in the early stages" | | | | | not yet |
| litao91/goldmark-mathjax | pseudo-version from 2021-02-17; last push 2021-11 | **no LICENSE file** (GitHub: NOASSERTION) | InlineMath and MathBlock work with the adapter | | yes | | | **legally unusable**, so hand-write it (its semantics matched markdown-rs on every math input tested) |
| go.abhg.dev/goldmark/frontmatter | v0.3.0, 2025-11-17 | BSD-3 | `Close()` **removes its node** (`parse.go:118`), so it must be rebuilt | | | yes | | works, but pulls in yaml.v3 and BurntSushi/toml. Hand-write about 60 lines instead |
| gomarkdown/markdown | pseudo-version from 2026-10-11; 1.7k stars | BSD-2 | **no source offsets** at all (`ast.Leaf`/`Container` hold only Literal/Content) | own | | | | rejected |
| Go ports of mdast, micromark or markdown-rs | none found (web search: none exist) | | | | | | | n/a |

## 3. Prototype (throwaway, outside the repo)
Path: `C:/Users/ADMIN/AppData/Local/Temp/gpuibase-proto/markdown/`. The module is `proto/markdown`, go1.27.1, built with `CGO_ENABLED=0`.

| File | Purpose |
|---|---|
| `oracle/` (Rust, `markdown = "=1.0.0"`, the version in Base's Cargo.lock) | Dumps `kind start..end detail` for each node, using Base's `parse_options()` copied exactly (`default`, `fm`, `mdx`, `nomath`) |
| `cases.go` | 66 inputs taken verbatim from markdown.rs tests t01–t57, plus 16 extra GFM/mdast coverage inputs (`x_*`) |
| `spec.go` | CommonMark 0.31.2 `spec.json` (652 examples) plus goldmark's GFM fixtures (table, strikethrough, tasklist, linkify, footnote), 700 in all |
| `goldmark.go` | Builds the parser: GFM, Footnote, mathjax inline (prio 501) and block (701), frontmatter |
| `adapter.go` (680 lines) | goldmark AST to mdast-shaped tree with start and end offsets. Each reconstruction step raises a named flag |
| `compare.go` | Line-by-line diff against the oracle, with a per-test tally |

Commands run:
```
go mod init proto/markdown; go get github.com/yuin/goldmark@v1.8.6 go.abhg.dev/goldmark/frontmatter@latest \
  github.com/litao91/goldmark-mathjax@latest github.com/gomarkdown/markdown@latest
go run . emit > cases.json; (cd oracle && cargo build --release); oracle.exe < cases.json > oracle.json
go build -o proto.exe . && ./proto.exe compare -v
./proto.exe emit-spec > cases-spec.json; oracle.exe < cases-spec.json > oracle-spec.json; ./proto.exe compare-spec -v
go test github.com/yuin/goldmark -run Spec   # PASS: all 652 spec examples, no skip list
```
Key output:
```
cases: 80/82 identical mdast (kind+start+end+value)
parser-dependent tests: 51 all-cases-identical, 2 with a diff: t14 t15      (both MDX)
spec corpus: 565/700 identical (543 at first run; 553 after an oracle-format fix; 565 after one emphasis-start rule)
spec diffs by class: 8 tree-structure | ~102 offset-only | ~26 value-only | ~11 offset+value
```

## 4. Tally: Base's 57 markdown.rs tests
"Reproducible" means that for every input in the test, the adapter's mdast is identical to markdown-rs's: same kinds, same nesting, start and end offsets, and values (text, code value and lang, url, title, alt, ids, checked, spread, align, depth). Base's `markdown.rs` logic is ported on top of that mdast unchanged, so each test's asserted facts follow from it.

| Class | Tests | Count |
|---|---|---|
| Parser-independent: pure `aligned_source_segments`/`source_char_offset` helpers | t04 t05 t06 t07 | 4 |
| Reproducible **directly**: goldmark segments give the spans, only kind mapping needed | t39 t40 (inline `<img>` RawHTML) | 2 |
| Reproducible **with adapter logic** | t01–03, t08–13, t16–38, t41–57 | 49 |
| **Not reproducible** | t14 (MdxTextExpression), t15 (MdxFlowExpression): goldmark has no MDX | 2 |

Other spot checks:
- The 16 extra inputs all match after the fixes: table, tasklist, strike, autolinks (www, mailto, `<..>`), reference links and definition, link and image titles, HTML block, ATX, setext and thematic break, ordered start=3, both frontmatter forms, unclosed fence, multi-line math, multi-line footnote, nested quote/list, CRLF.
- The re-parse path works: math-off parsing is just a second goldmark instance without the math parsers, and `$5 and $` pairing behaves like markdown-rs (t51, t52, t54).

**Adapter logic each test needed (flag, then the number of the 82 Base cases that used it):**
- `text-merge` (27): mdast merges the adjacent Text that goldmark splits at escapes, soft breaks and unresolved `[`.
- `softbreak-value` (12): appends the source line ending into the Text value.
- `end-derive:emphasis` (14): end = last child end + Level.
- `start-fix:emphasis-run`: goldmark's Pos is the start of the whole delimiter run.
- `end-scan:codespan` (8): backtick-run match.
- `end-scan:closing-fence` (10): scans the next line after the container prefix for the fence, or `$$`.
- `end-scan:link-tail` (8): `](dest "title")` scanner. Goldmark's `Reference` field drives Link to LinkReference.
- `end-derive:inline-math` (10).
- `end-scan:break` (4): Break covers the trailing spaces or `\` through the EOL.
- `end-derive:container` (9): Blockquote, List, ListItem and FootnoteDefinition end = last child end.
- `spread-recompute` (5): mdast list spread means a gap *between items*, and item spread means a gap *between children*. goldmark has only `IsTight`.
- `footnote-reorder` (2): goldmark moves definitions into a trailing FootnoteList.
- `frontmatter-rebuild` (3).
- `start-fix:indented-code` (2).
- `pos-fix:linkify`: goldmark's linkify `Pos` is off by one.
- `tasklist-paragraph-start`: drop TaskCheckBox and start the Paragraph at the text.
- `drop-padding-cell`: goldmark pads short rows (Pos -1); mdast does not.
- markdown-rs quirks copied:
  - Text that **starts** with an escape begins *after* the backslash (t25 `\*` gives 1..2; t26 `\\\\b` gives 1..5).
  - A FootnoteDefinition followed by more input keeps its EOL.

**Goldmark's segment model and how the end offsets are rebuilt:**
- **Leaves that store their own end:** Text, RawHTML, the Lines of Paragraph, CodeBlock, HTMLBlock and LinkReferenceDefinition, and table rows (through the line end).
- **Containers derive their end bottom-up:** inline containers from last child end + closing delimiter (Emphasis Level, `~` run, link tail), block containers from last child end.
- **Blocks whose closing syntax goldmark discards:** fence and math closers, the setext underline and ATX trailing text. These are rescanned from source. The rescan is about 1 helper per construct.

## 5. Spec-corpus residue: what the risk actually is
- **Tree-structure disagreements (8/700, parser semantics, cannot be fixed in the adapter):**
  - cm602 `<https://foo.bar/baz bim>`: markdown-rs's GFM literal autolinks the URL inside it.
  - gfm-linkify-07: goldmark links `ftp://`, which markdown-rs (GFM) does not.
  - gfm-linkify-14: markdown-rs autolinks inside link text.
  - 4 table edge cases: excess cells, a table-vs-paragraph boundary, and `--` under a list item read as a table versus setext.
  - gfm-footnote-02: an odd `[^000]:0\t[^]:` definition.
  - None of the Base tests hit these.
- **Offset and value gaps (about 127):**
  - Mostly **column-based container prefixes**: tabs, indented and fenced code inside list items, list or item end on lazy or blank lines, and block starts including indentation. These need the adapter to track each container's content column; goldmark's `ListItem.Offset` and the line `Padding` provide it.
  - Smaller groups:
    - link-label id normalization: markdown-rs ids look whitespace-stripped, e.g. `foo*bar*`.
    - hard-break spans in odd positions.
    - entity and escape value decoding in edge cases.
    - oracle formatting artifacts: Rust `\u{a0}` versus Go `\u00a0`.
  - All of these are deterministic adapter rules, not parser differences. Three generic rules took the corpus from 543 to 565.
- **Projection:** a production adapter of about 1.2–1.5k lines should reach about 98% exact span parity on the spec corpus. The ceiling is set by the 8 structural cases, about 1.1%. That number is an estimate from this convergence trend, not a measurement.
- **Conformance:**
  - goldmark: "compliant with CommonMark 0.31.2" (README), and its `TestSpec` passed all 652 examples locally (html output). GFM extensions are covered by its own fixtures, which are not cmark-gfm's spec file.
  - markdown-rs: "100% CommonMark, GFM, MDX", "2300+ tests, 100% coverage" (README).
  - A hand-port only inherits that conformance if its tests are ported too.

## 6. The hand-port alternative, measured
Measured on markdown-rs 1.0.0 (crate source in the cargo registry; MIT, © 2022 Titus Wormer). Last commit and release 2025-04-23, so the project has been idle for about 18 months; 1.58k stars.

Total: 42.5k physical lines and **27.6k code lines**, counted without comments or blank lines.

| Part | Code lines | Needed? |
|---|---|---|
| Tokenizer, state machine, events, subtokenize, resolve, parser, config | about 2.9k | yes |
| CommonMark + GFM + math + frontmatter constructs (`construct/*` minus mdx) | about 9.6k | yes |
| `to_mdast.rs` + `mdast.rs`/`unist.rs` (types) | 1.49k + about 1.7k | yes (part of the types is needed either way) |
| MDX constructs + util | 1.26k | only for `mdx()` |
| `to_html` and HTML-only utils | 1.45k | no |
| `util/unicode.rs` + `util/constant.rs` (Unicode punctuation/whitespace tables, entity table) | 11.9k | replace with Go `unicode` plus a generated entity map |
| **Needed total (no MDX, HTML or tables)** | **14.2k Rust** | |

- **Size estimate:** about **14–18k Go lines**. This is a mechanical port of a resumable state machine (`State::Next(StateName)` dispatch across about 120 states). The repo's 2300+ tests (their own `tests/` dir, not in the crate) would also need porting to get the conformance claim.
- **Effort estimate:** about 3–5 engineer-weeks, against about 1 week for the adapter plus extensions. The payoff is exact mdast parity for free, including MDX if it is ported (+1.3k).

## 7. Recommendation and risk
- **USE LIBRARY: goldmark v1.8.6.** Put it behind `internal/mdparse`, which emits a Go `mdast` package (Base's plugin API surface).
  - The adapter is about 1.2–1.5k lines and adds container-column tracking to the prototype.
  - Hand-written goldmark extensions: math (`$`/`$$`, markdown-rs `math_text`/`math_flow` semantics, about 200 lines) and frontmatter (`---`/`+++` at offset 0, about 60 lines). Both are needed because of the license gap and the dependency weight of the existing ones.
  - go.mod gains one third-party module (goldmark, no dependencies).
- **Port the tests:** all 57 Base tests port unchanged at the mdast/BlockNode level, except t14 and t15. Add this prototype's oracle diff as a golden test: commit `oracle-spec.json` as testdata, regenerated with the Rust oracle.
- **Risk: MEDIUM-LOW.**
  - (a) About 1% GFM edge-case tree divergence from markdown-rs: autolink literal rules and table boundaries. Record it in the conformance ledger.
  - (b) The adapter's span reconstruction is the fragile part. Mitigate with the differential golden test.
  - (c) **MDX is unsupported.** `mdx()` and t14/t15 are deferred. If MDX is required later:
    - Expressions alone are about 200 lines of goldmark extension, because markdown-rs does brace-balancing only when no JS parser is configured.
    - JSX would need about 1.1k more (`partial_mdx_jsx.rs`).
  - (d) goldmark v2 migration later; it still has no end offsets.
- **Revisit HAND-PORT only if** exact markdown-rs parity, including MDX JSX, becomes a hard requirement.

Sources:
- [goldmark](https://github.com/yuin/goldmark)
- [newreleases: goldmark v1.8.0 "position information to all nodes"](https://newreleases.io/project/github/yuin/goldmark/release/v1.8.0)
- [markdown-rs](https://github.com/wooorm/markdown-rs)
- crates.io API `crates/markdown` (1.0.0, 2025-04-23)
- the Go module proxy (`go list -m -json ...@latest`)
- `gh api repos/{litao91/goldmark-mathjax, gomarkdown/markdown, yuin/goldmark, wooorm/markdown-rs}`
- [gomarkdown](https://pkg.go.dev/github.com/gomarkdown/markdown) (no position fields; its CommonMark mode turns off extensions, README §CommonMark)
