# 75 - Split out gpui-go-base (local)

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 10 (Claude Code)
Blocked by: 71, 72, 73, 74
Triage: ready-for-agent

**What to build:** Base, zterm and the gallery move into a new local gpui-go-base repository that depends only on gpui-go's public API; the line wrapper moves with Base; gpui-go keeps one core-only example. zterm and the gallery are test apps and are never pushed. Creating the public GitHub repo and any push need the user's go-ahead at that step.

**Blocked by:** 71 (Flip the default display to block), 72 (Base overlays rebuilt on core primitives), 73 (Base focus, selection and editing on core primitives), 74 (Base motion on the core engine)

**Status:** ready-for-agent

- [x] gpui-go builds and passes its suites with no Base inside.
- [x] gpui-go-base builds against gpui-go and its tests pass; zterm and the gallery run offscreen.
- [ ] The user's go-ahead for creating the GitHub repo is recorded before any remote step.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer

**New repository**

- `C:\Users\ADMIN\Desktop\nguyenvu\gpui-go-base`, `git init -b main`, clean initial import (commit `5c4aa9b`), naming the gpui-go source commit `c51ab63` (this worktree's HEAD).
- Module `github.com/hoangvu12/gpui-go-base`; Base stays package `base` **at the module root** (import is just `github.com/hoangvu12/gpui-go-base` — the simplest user import; nothing broke, so no `base/` subdir). Subpackages `rope/`, `sumtree/`, `segment/`, `dataurl/`, `strftime/`, `testdata/`; `examples/{zterm,base-gallery,base-showcase}`; `conformance/cmd/basecoverage`; `evidence/base/`; LICENSE (Apache-2.0), NOTICE, THIRD_PARTY_NOTICES.md, README, AGENTS.md + CLAUDE.md (zterm and the gallery are local test apps, never pushed; the README and AGENTS.md say so at the top).

**What moved (copied into the new repo; `git rm`'d here)**

- `base/` (323 files with the moved wrapper), `examples/zterm`, `examples/base-gallery`, `examples/base-showcase`, `conformance/cmd/basecoverage` (+ its ledger/overrides), `evidence/base/` (ledger, manifest, 44 ticket evidence files). `examples/hello` and `examples/tsx-hello` stay. `evidence/zterm/` stays in gpui-go: it is native-DLL/font-weights evidence referenced by `internal/consumerspec/evidence.go`, not Base code.
- The gpui-component (crates/base), zed-sum-tree, ropey, data-url/chrono/unicode-segmentation, web-platform-tests and UCD-test-data notice entries moved to the new repo's THIRD_PARTY_NOTICES.md; gpui-go keeps its own credits (GPUI-CE port, Tailwind, uax29, the DLL crates). gen.py no longer exists (superseded); only the notices file itself needed editing.

**The line wrapper (moved) and other Base-only code**

- `gpui/line_wrapper.go` was gpui code existing only for Base (used by `base/input_text_wrapper.go`, `base/text_inline_flow.go` and its own tests; no gpui production code called it). It moved to the new repo as `line_wrapper.go`: `NewLineWrapper(w, font, size)`, `WrapLine`, `LineFragment{Text,Element,Tab}`, `Boundary`, `SetLetterSpacing`, `LineWrapperMaxIndent` — exported exactly as Base needs; the word table and algorithm are byte-identical.
- Moved with it: `TestIsWordChar` (gpui/rich_text_test.go), `TestLineWrapperTabFragmentTakesItsWidth` (gpui/text_tabs_test.go), `TestWrapLine`/`TestLineWrapperLetterSpacing` and the "LineWrapper over the real font" subtest (internal/richtextspec), now `line_wrapper_test.go` + `line_wrapper_real_windows_test.go` in the new repo.
- **Not moved (listed as follow-up): the legacy single-line facade `gpui/shaped_line.go`** (`WindowTextSystem.ShapeLine`/`ShapedLine`/`LayoutWidth`/`NormalLineHeight` + the x/index queries) — it exists for Base and is public gpui API, but its production users are Base-only while gpui's own tests (text_cache, text_tabs, richtextspec, fontspec) exercise it; moving it is a larger seam (it wraps the private shaping/text-cache layer), so it stays in gpui-go for now. Everything else gpui Base uses (TopLayer, Anchored, tooltip hook, edit actions, motion engine, selection…) is core public API from 12-74 and stays.

**Public-API gaps**

- The old wrapper used two private gpui internals (`windowTextSystem`, `layoutWidthOf`). No new core API was needed: the public `WindowTextSystem.LayoutWidth` (already there, ticket 10) covers per-character measurement, so Base's wrapper resolves the window's text system at construction and measures through it. One recorded behaviour nuance: a native text-system failure now surfaces as `LayoutWidth`'s panic at first measure (the legacy infallible policy) where the old constructor returned an error; the two Base call sites dropped their error arms (`input_text_wrapper.go`, `text_inline_flow.go`).
- No Base code needed any other `internal/...` package or unexported symbol (verified: the new repo imports only `github.com/hoangvu12/gpui-go/gpui`).

**go.mod changes**

- New repo: `require github.com/hoangvu12/gpui-go v0.2.0` + goldmark, uax29, x/net, with `replace github.com/hoangvu12/gpui-go => ../gpui-go-wt-base` (commented LOCAL-ONLY; must become a tagged require before any publish; after 75 merges, main there has no Base). Go floor `go 1.27.0` as gpui-go.
- gpui-go: `go mod tidy` dropped `yuin/goldmark` and `golang.org/x/net` (Base-only); `uax29` stays (gpui/text_transform.go, text_truncate.go), goja/esbuild unchanged (tsx). go.sum trimmed accordingly.

**basecoverage in the new repo**

- The tool now scans the new repo's root package (skipping examples/conformance/evidence/docs) and the gpui-go checkout's `gpui/` test citations through the replace target (`-gpui-root` / `$GPUI_GO_ROOT`), so the 55 upstream tests whose ports live in the core (selection 44, touch 3, selectable 3, auto-scroll 2, motion, visualtest/button) stay credited as `gpui-go/gpui/...` citations.
- The pre-split ledger was stale (72/74 deleted focus_trap/positioner/reduce-motion/tooltip-provider tests without regenerating; `-check` failed in gpui-go before the move). The regenerated ledger dispositions those as not-applicable (24 new overrides + 2 module dispositions citing 72/74's Answers) and reads **1238 tests, 0 GAPS**; `go run ./conformance/cmd/basecoverage -check` → "ledger current", exit 0.

**Tests and captures**

- gpui-go (post-split): `GOFLAGS=-p=4 go build ./...` ok; `go vet ./...` ok; `go run ./cmd/displaycodemod -check ./...` → "694 files scanned; 0 would change", exit 0; `GOFLAGS=-p=4 go test -p 1 -count=1 ./...` → 93 ok, the only failure the known pre-existing `portfixture TestFx0004` (font-family count 94/95; not investigated per instructions). All the other known flakes passed this run.
- gpui-go-base (against ../gpui-go-wt-base, tree-identical to c51ab63): `go vet ./...`, `go build ./...` ok; `GOFLAGS=-p=4 go test -p 1 -count=1 ./...` → all 10 packages ok.
- Captures before (gpui-go, pre-split, `%TEMP%\split-before`) vs after (new repo, `%TEMP%\split-after`), plus control runs (`split-before2` in the untouched gpui-go-wt-base, `split-after2` in the new repo):
  - zterm: 15/19 byte-identical; `tab-drag/tab-menu/tab-slot/tabs` differ only inside the tab strip's spinner region (x 45-55, y 245-258) — the known spinner noise.
  - gallery: every file differs only in the header label region (x 365-388, y 19-28): the gallery header *displays its own module import path*, which correctly changed from `github.com/hoangvu12/gpui-go/base` to `github.com/hoangvu12/gpui-go-base`. Outside that region: `motion`/`motion-open` (the known noise; they also differ run-to-run within one repo) and `tree.png` — exactly one pixel (337,148), red channel 195→194 on a ClearType subpixel-AA text edge (same text, same bounds, stable per-repo; the same raster-rounding noise class as the known plot-radial step, which itself only appeared between the two before-runs). Nothing else differs.

**GitHub step (NOT done — needs the user's go-ahead)**

- [ ] The GitHub repository `gpui-go-base` has NOT been created; nothing was pushed, tagged or given a remote, in either repo. Both commits are local only.

**Files**

- gpui-go: deleted `base/`, `examples/{zterm,base-gallery,base-showcase}`, `conformance/cmd/basecoverage`, `evidence/base`, `gpui/line_wrapper.go`; edited `go.mod`/`go.sum`, `README.md` (Base section → pointer), `NOTICE`, `THIRD_PARTY_NOTICES.md` (moved entries; uax29 reason updated), `PROJECT.md` (one line), `gpui/interact.go` + `gpui/shaped_line.go` (moved-evidence/API notes), `gpui/rich_text_test.go`, `gpui/text_tabs_test.go`, `internal/richtextspec/{richtext_test,richtext_real_test}.go` (moved tests; package docs updated).
- gpui-go-base: everything listed above plus the new `line_wrapper*.go` files, `go.mod`, notices and agent/readme docs.

**Limitations**

- The replace is a path dependency on the sibling worktree; a clean clone needs the gpui-go checkout beside it (documented in go.mod/README/AGENTS.md).
- The gallery capture set is not byte-identical to the pre-split set by construction: the header labels the (new) module path — a deliberate label, explained above.
- `gpui/shaped_line.go` (the legacy single-line facade) remains in gpui-go although only Base uses it in production; moving it is a follow-up (needs either exporting the shaping seam or duplicating the facade in Base).
- The 3 unused overrides the ledger records (`text/selection.rs`'s two stale-citation entries plus `input/base/state.rs::test_input_does_not_invalidate_cached_parent_during_paint`, now resolved by a gpui-go citation) are the pre-split state carried over.
- Historical prose that names `base/...` files (docs/browser-parity-harness.md, `internal/parity/capture.go`'s zterm credit, evidence/zterm) intentionally stays as history.
