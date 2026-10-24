# 68 - Codemod: explicit Flex where the default was relied on

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 10 (Claude Code)
Blocked by: none
Triage: ready-for-agent

**What to build:** A codemod adds .Flex() to every element that relies on the current default display, so flipping the default later changes nothing for existing code (expand step of the D6 flip).

**Blocked by:** None - can start immediately

**Status:** ready-for-agent

- [x] The codemod is idempotent and leaves elements with an explicit display unchanged.
- [x] Running it on a sample package produces no behaviour change (offscreen captures identical).

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer

- **The rule ("relies on the default")**: a ROOT is a call to a gpui constructor whose element style starts at `Style::default()` with an empty caller refinement — `Div`, `Img`, `Canvas` (the three `BindStyled` element constructors; `gpui.Styles()` builds a bare refinement, not an element; `Anchored`/`TopLayer`/`Empty` pin display internally; `Svg`, `text`, `List`, `UniformList` have no caller-facing Styled surface, see limitations). The CHAIN is the maximal run of selector method calls built directly on the root's result. If no link selects display, `.Flex()` is inserted immediately after the root call. Insertion at the root is the earliest possible point, so every later display choice (method, Class, refinement merge, a helper that styles the element, a caller of a helper-returned element) overwrites the pin exactly as before; when nothing else sets display, the element stays Flex across ticket 71's flip. Custom components are covered transitively: their render builds on a `Div/Img/Canvas` somewhere, and that construction site is pinned (Base's gap-2b `.Block()`-first convention is untouched; its pin-default sites — `base/button.go` NewButton's base, `base/tabs.go` NewTab, `base/root.go`'s root, `base.HFlex` inner div, … — are exactly what gets pinned).
- **Explicit-display forms recognized (chain left byte-for-byte alone)**: `.Flex()`, `.Block()`, `.Grid()`, `.Inline()`, `.InlineFlex()`, `.InlineBlock()`, `.InlineGrid()`, `.Contents()`, `.FlowRoot()`, `.Hidden()`, `.Display(...)` — at any position of the chain, including multi-line chains — and `.Class("...")` with a literal containing a Tailwind display class (`flex`, `block`, `grid`, `hidden`, `inline`, `inline-flex`, `inline-block`, `inline-grid`, `contents`, `flow-root`, with or without a state-variant prefix). Flex-direction/grow vocabulary (`.FlexCol()`, `.Flex1()`, `.FlexWrap()`, …) is deliberately NOT display and still gets pinned.
- **Usage**: `go run ./cmd/displaycodemod [patterns]` (default `./...`, resolved through `go list` so testdata/ignored dirs are skipped like any go command). Default prints a unified diff + per-directory summary; `-w` writes (gofmt-formatted); `-check` exits 1 when anything would change — the gate ticket 71 runs after 69/70 apply it. No new module dependencies (go/parser + go/ast only).
- **Idempotence / no behaviour change**: table tests (cmd/displaycodemod/rewrite_test.go) cover every explicit form → unchanged, second run → no diff, chains split across lines/statements, elements passed through helpers, aliases/dot-imports, package-internal `Div()`, comments preserved. E2E (cmd/displaycodemod/e2e_test.go): the sample package under testdata/sample is copied into a dot-dir temp package, rendered offscreen through a TestApp + hostless test window (deterministic text system, `DrawWindowFrame`, `WindowDebugBounds` — the inlinespec/parity pattern), `go run` before and after the codemod: 11 elements pinned, bounds identical line-for-line, second pass a no-op. Whole-tree proof on a pristine `git archive` copy in %TEMP%: `-w` applied, `-check` then reports 0 pending, `gofmt -l` clean, `go build ./...` green, and `go test ./internal/parity -run TestFixtures` 3/3 ok plus `go test ./base` ok on the codemod'd copy (same outcomes as the untouched baseline; inlinespec/listspec fail identically pre-codemod on this machine — environmental, not from the codemod).
- **Dry-run counts (HEAD + this tool, no writes)**: 1088 files scanned, 235 would change, 1341 insertions (931 in 150 `_test.go` files). Per top-level dir: gpui 77 files / 687 insertions (73 test; non-test is just `gpui/tooltip.go`, 1), base 74 / 232 (56 test), examples 25 / 185 (1 test; zterm 11/43, base-gallery 11/115, base-showcase 3/27), internal 59 / 237 (20 test; parity fixtures 38/182 is the bulk). `gpui/script` 4 / 14.
- **TSX and Tailwind for 69/70**: TSX never needs rewriting — every `<div>` lowers through the single `gpui.Div()` in `gpui/script/lower.go`; ticket 69's codemod run pins that one call (`.Flex()`), which keeps all TSX behavior across the flip; alternatively the host can set its own explicit default if TSX should follow the browser default instead. TSX `style={{display:"flex"/"block"}}` already maps to `.Flex()`/`.Block()` (node.go apply). Tailwind strings: `Class` literals containing a display class are already respected by the tool; a non-literal Class string whose runtime value sets display still works (later-wins), so 69/70 need nothing extra — but a follow-up lint could report them.
- **Files**: `cmd/displaycodemod/{main,rewrite,walk,diff}.go`, `cmd/displaycodemod/{rewrite,e2e}_test.go`, `cmd/displaycodemod/testdata/sample/main.go`.
- **Limitations**: (1) non-Styled elements whose display comes from `DefaultStyle()` cannot be pinned by a chain codemod — `Svg()` (style built internally, no Display fluent), `text()`/`StyledText`, `List`/`UniformList` (full Style, not a refinement) — they are leaf/virtualized elements where Flex vs Block is not observed today, but ticket 71 should pin them explicitly in the constructors to be safe; (2) `gpui.Div` taken as a function value (`f := gpui.Div; f()`) is not rewritten (no such use in the tree); (3) a `Class` string whose display class only appears at runtime (non-literal) does not mark the chain explicit, so a redundant `.Flex()` may be added (harmless, later class wins); (4) a display set only through a state-style builder (`Hover(Styles().Hidden())`) does not make the base display explicit — the tool pins the base, which is the intent; (5) refinements read back for exact-equality assertions in tests could see the new `Display: Some(Flex)` (none found in the tree; 69/70 run the full suite anyway).
