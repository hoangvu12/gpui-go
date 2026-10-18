# Decide the third-party dependency policy for Base

Parent: ../map.md
Type: grilling
Labels: wayfinder:grilling
Triage: ready-for-human
Status: open
Assignee: unassigned
Blocked by: none

## Question

Base's Rust crate uses markdown-rs, html5ever, ropey, unicode-segmentation, chrono, regex, sum-tree, lsp-types and more; gpui-go's go.mod has zero requirements. Per dependency: add a Go module (golang.org/x or third-party) or hand-port?

## Scope

- The one-look table in [deps-recommendation](../research/deps-recommendation.md).
- The open calls: markdown (goldmark + mdast adapter vs hand-port markdown-rs), HTML (x/net/html module vs vendoring its BSD-3 `html` package into `internal/`), segmentation (clipperhouse/uax29 vs hand-port UAX #29 with generated tables), whether golang.org/x is a separate policy class.

## Acceptance criteria

- [ ] The user's choice per dependency is recorded under `## Answer`; dependent tickets (15, 24, 27, 30) are updated accordingly.

## Research

- [deps-recommendation.md](../research/deps-recommendation.md)
- [slice-06-text-selection.md](../research/slice-06-text-selection.md)
- [slice-05-input-editor.md](../research/slice-05-input-editor.md)
- Executed fit research: [deps-01-markdown](../research/deps-01-markdown.md), [deps-02-segmentation-html](../research/deps-02-segmentation-html.md), [deps-03-rope-lsp-misc](../research/deps-03-rope-lsp-misc.md); the Rust reference programs + recorded outputs are kept in [research/oracles/](../research/oracles/) for later golden tests.

## Comments

- 2026-10-19 — Created by the GPUI Base port chat from the merged slice inventories (R: = `reference/ce-source/crates/gpui/src`).
- 2026-10-19 — Executed library-fit research (prototypes in a temp dir; Rust oracles built on the exact Cargo.lock versions). Reuse vs recreate:
  - **markdown → reuse goldmark v1.8.6** (MIT, zero deps) + an in-repo mdast adapter (~1.2–1.5k lines) that rebuilds container end offsets; hand-write math (~200) + frontmatter (~60); defer MDX. Oracle diff vs markdown-rs 1.0.0 with Base's own parse options: 55/57 Base tests reproducible (the 2 misses are MDX), 80/82 inputs exact; CommonMark+GFM corpus 565/700 exact spans so far (8/700 tree differences, none in Base tests). Hand-port alternative: ~14–18k Go lines, 3–5 weeks. goldmark-mathjax has no license → not usable.
  - **segmentation → reuse clipperhouse/uax29/v2** (MIT, zero deps, Unicode 17 = the resolved unicode-segmentation 1.13.3's version): official tests 1944/1944 word + 766/766 grapheme; Base cases 19/19; differential vs the Rust crate on 52,719 strings: graphemes identical, words differ on 5 ZWJ-emoji edge cases where the Rust crate deviates from UAX #29 (record as deviation).
  - **HTML → reuse golang.org/x/net/html** (BSD-3; module adds 2 go.sum lines — the earlier "pulls x/crypto/sys/term/text" note was wrong — or vendor its stdlib-only `html` package, ~8.4k lines): 36/36 Base inputs give trees identical to html5ever 0.27; html5lib 1534/1559 identical, all 25 diffs are `<select>` (2025 spec change; Base never renders it).
  - **recreate (no usable Go library):** ropey rope (~1.3–1.7k + tests; candidates are LGPL/GPL/bytes-only/no UTF-16), sum-tree (~700–900, or the slice pattern from `gpui/listelement.go`), lsp-types structs (~400; go.lsp.dev/protocol drags ~230 modules into go.sum), strftime subset (~150, prototype matches chrono on 45/45; best lib itchyny/timefmt-go misses 2), aho-corasick single-pattern (~40), data-url (~140).
  - **stdlib:** regexp, time, encoding/json, log/slog. **not needed:** syntect, palette, rust_decimal, schemars, smallvec, itertools, num-traits, anyhow, futures/smol.
  - **gpui macros/derives:** IntoElement/actions!/#[gpui::test] need nothing new; the Refineable/style-macro vocabulary (~175 distinct methods Base calls), Interactive/Parent/FluentBuilder and accesskit properties are core gaps already owned by tickets 05, 06, 11 (a maintainer `go:generate` for the style methods is allowed by `research/09-fluent-authoring-methods.md:52`).
