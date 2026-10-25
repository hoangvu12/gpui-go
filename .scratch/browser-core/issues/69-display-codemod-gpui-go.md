# 69 - Codemod applied in gpui-go

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: resolved
Assignee: orchestrator 10 (Claude Code)
Blocked by: 68
Triage: ready-for-agent

**What to build:** All elements in gpui-go's own packages, examples and tests that relied on the default display state Flex explicitly.

**Blocked by:** 68 (Codemod: explicit Flex where the default was relied on)

**Status:** ready-for-agent

- [x] The full headless test suite passes with identical fixtures.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

- 2026-10-25, orchestrator 10: verified and merged (ef745a5 + 93e34d8, with 69 and 70 together). `displaycodemod -check ./...` on the merged tree: 0 pending. The diff audit shows only `.Flex()` insertions, plus the TSX pin comment in gpui/script/lower.go and the class_test hover-want fix. `go vet ./...` is clean. The broad batch set plus inlinespec/listspec/textframespec: all ok except 78's four (fixed on its branch), spellspec TestSpellcheckCheckNeverBlocksTheUIThread (load; ok on re-run) and gpui/script TestFrameOverheadIsUnder1msAtTheProbeSize (the known load flake). The worker's capture comparison: zterm 15/19 and the gallery 66/69 byte-identical; every difference also occurs between two runs of the same code (spinner/reveal animation phase, one AA rounding step).

## Answer

- **Applied**: `GOFLAGS=-p=4 go run ./cmd/displaycodemod -w ./...` — 1093 files scanned, 236 changed, 1344 `.Flex()` insertions (934 in 151 `_test.go` files); `go run ./cmd/displaycodemod -check ./...` then reports **0 pending** (exit 0). Dry run read first; the diff was exactly the pin set 68 described.
- **Per area (git numstat)**: gpui 77 files / 659 insertions (non-test: `gpui/tooltip.go`, 1); gpui/script 4 / 15 (lower.go, plugin.go, view.go, timing_test.go); internal 59 / 237 — parity fixtures 38 files / 182 insertions (37 view files + selection_test.go), the rest spread over authorspec, capturespec, devicelossspec, dropspec, focusspec, inlinespec, inspectorspec, layoutspec, listspec, scenespec, svgspec, textframespec. (69's own file counts; base/examples are in 70.)
- **Generated files**: none were touched (the codemod run changed zero files with a `// Code generated ... DO NOT EDIT.` header), so nothing was reverted or regenerated — tsxbundle embeds compiled JS (no element calls), stylegen/tailwindgen emit style/class tables. The tool gained a generated-file skip anyway (commit ef745a5: `isGenerated` + `TestGeneratedFilesSkipped`, single-line and split-header spellings, build-tag-tolerant), so the `-check` gate can never demand a hand edit on generated output.
- **TSX pin**: the one `gpui.Div()` every TSX `<div>` lowers through (`Host.lower` in `gpui/script/lower.go`) is pinned `.Flex()`, with the comment `// display pinned by 69; 71 decides whether TSX <div> follows the block default`. **Flag for 71**: TSX is browser-like, so `<div>` arguably should follow the block default at the flip — 71 decides whether to move the pin (a node's `style={{display:...}}`/class always overwrote it, so only the default-reliant divs change). Element()/errorBanner()/plugin banner divs are pinned like everything else.
- **One test expectation adjusted** (68's limitation 5, the only one hit in the whole tree): `gpui TestClassCachedSetIsNotAliased` compared the hover refinement parsed from a class string (which cannot carry a display pin) against a Div-chain want whose new pin added `Display: Some(Flex)`; the want is now a standalone `Styles().ShadowLg().Refinement()`, which is what the class parse actually produces.
- **Suite**: `go vet ./...` clean, `gofmt -l` clean on all changed files, `go build ./...` green, and `go test -p 1 -count=1 ./...` (whole repo, foreground): **99 packages ok**, failing only the known pre-existing set — inlinespec TestInlineDivPlacesElementChildrenInTextFlow, listspec TestUniformListRetainsFocusedItemDispatch, textframespec TestTextUnderAnimationRedrawsBounded + TestResizingWindowDoesNotLeakSupersededLines (ticket 78), and portfixture TestFx0004. `./internal/parity/...` fully ok with **zero record.json changes**.
- **For 71 — what the tool could not pin** (non-Styled roots; 71 should pin display in the constructors): `Svg()` (gpui/svgelement.go), `Text` (gpui/element.go) and `NewStyledText` (gpui/styled_text.go), `List` (gpui/listelement.go) and `UniformList` (gpui/list.go). Anchored/TopLayer/Empty already pin display internally. Minor: `gpui.Div` taken as a function value is not rewritten (no such use in the tree); a non-literal `Class` string can still earn a redundant pin (harmless — the later class wins).
