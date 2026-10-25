# 78 - Spec regressions on main outside the batch set

Parent: ../map.md
Type: task
Labels: wayfinder:task
Status: resolved
Assignee: orchestrator 10 (Claude Code)
Blocked by: none
Triage: ready-for-agent

**What to build:** Four tests fail on main (d7a5b80) in packages that the per-batch set doesn't run. Find the commit that broke each, then fix the cause, or update the test when the new behaviour is the intended browser rule.

- internal/inlinespec `TestInlineDivPlacesElementChildrenInTextFlow`: paragraph text = "prefix suffix", want "prefix  suffix" (likely 38's white-space collapse).
- internal/listspec `TestUniformListRetainsFocusedItemDispatch`: key deliveries after the item returns = 2, want 3.
- internal/textframespec `TestTextUnderAnimationRedrawsBounded`: LiveShapingHandles after the first frame = 21, want <= 14 (two per text).
- internal/textframespec `TestResizingWindowDoesNotLeakSupersededLines`: 9, want <= 6.

- [x] Each failure is bisected to a commit, and the cause is fixed or the test updated with the reason.
- [x] inlinespec, listspec and textframespec pass; the batch set gains them.

Found by orchestrator 10 while merging 68 (the 37 and 68 workers saw them on their baselines too).

## Comments

- 2026-10-25, orchestrator 10: verified and merged (fa5030f) after 37 and 69+70. On the merged tree, inlinespec TestInlineDivPlacesElementChildrenInTextFlow got past the text check and then failed on the row baseline (20.4, want 23.4), because 37's normal line height landed after 78 branched (37 couldn't update a test that already failed). The test is the port of the reference div.rs corpus, whose geometry derives from the 1.618 line height, so its view now pins `PhiLineHeight()` (an `inlineView.phi` flag set by `drawCorpus`; the sibling TestInlineTopAndBottomAlignmentGrowsBothExtents keeps 37's normal-line-height expectations). `go test -p 1 -count=1 ./gpui/ ./internal/parity/... ./internal/inlinespec ./internal/listspec ./internal/textframespec ./internal/richtextspec ./internal/textspec ./internal/layoutspec ./internal/focusspec ./base ./examples/zterm`: all ok after that fix; `displaycodemod -check ./...`: 0 pending. Spot-check: inline_items_builder.cc:1281-1288 at 0d4dcd4f, `AppendAtomicInline` uses plain `Append` (not opaque to collapsing) after `RestoreTrailingCollapsibleSpaceIfRemoved`, matching the fix.

## Answer

- **inlinespec `TestInlineDivPlacesElementChildrenInTextFlow`** — broke at `d4ea518` (38, white-space). Cause: the port's collapser let the collapse state carry ACROSS an atomic box ("a sub-run starting at a box consults the state"), but in Blink an atomic inline is the last item to collapse with and ends kNotCollapsible (`AppendAtomicInline` appends through `Append`, not `AppendOpaque`; only tags/bidi/out-of-flow are `kOpaqueToCollapsing`, `inline_items_builder.cc:218-228`, `:1281-1289`), so the space after a box keeps its one space and a space before a trailing box survives the final removal. Verified in headless Chrome 155: `a <box> b` renders one space each side of the box; the corpus collapses to the identity map and the test passes with its original expectations. Fix: `gpui/white_space.go` — a sub-run starting at a box never consults the state (`processSpaceRun`), and the trailing-space removal (`finish`, pre-line newlines) stops at a box (`removeTrailingKeptSpaceBefore` + `boxAfter`); the table's three box cases in `gpui/white_space_test.go` now pin the Chrome-measured behavior. Files: `gpui/white_space.go`, `gpui/white_space_test.go`.
- **listspec `TestUniformListRetainsFocusedItemDispatch`** — broke at `976cd25` (14, focus fixup). Cause: intended — the end-of-frame fixup blurs the focused handle when the item scrolls out of the virtualized range (the node-remove focus fixup), and like a re-inserted DOM node the returning item is not refocused, so the key falls to the body (2 deliveries, not 3). Test updated with a comment naming 14, plus a re-focus step that still proves the retained callback returns with the item (3rd delivery after `window.Focus`). Files: `internal/listspec/retained_test.go`.
- **textframespec `TestTextUnderAnimationRedrawsBounded`** — broke at `82703e2` (32, min-content). Cause: bounded cost, not a leak — D2's min-content query shapes each text at wrap width 0, a third distinct `shapedLineKey` next to the unwrapped (max-content) and wrapped (definite-width) shapes; all three are re-requested by every frame's measure passes, so the two-frame `shapedLineCache` holds three live handles per text at the steady state (7 texts → 21). The 10,000-frame run confirms zero growth from the first frame and the fresh-shape probe still succeeds. Bound updated to three handles per text with the measured reason. Files: `internal/textframespec/textframespec_test.go`.
- **textframespec `TestResizingWindowDoesNotLeakSupersededLines`** — same commit (`82703e2`, 32): the first-frame baseline is the same three keys per text (3 texts → 9); the resize loop's no-leak checks (max ≤ 24, final ≤ 12, superseded widths released) still pass unchanged. Baseline bound updated to three handles per text with the same comment. Files: `internal/textframespec/textframespec_test.go`.
- Bisects ran in a temporary detached worktree under %TEMP% (`7459558` good → `d7a5b80` bad); no parity fixture changes (the whitespace fixtures have no atomic-box rows, and they still pass).
