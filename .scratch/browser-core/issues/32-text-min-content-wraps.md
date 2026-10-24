# 32 - Text min-content wraps

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: resolved
Assignee: orchestrator 8 (Claude Code)
Blocked by: 01
Triage: ready-for-agent

**What to build:** Text min-content wraps at every break opportunity, so text in a flex row shrinks and wraps instead of overflowing; the text measure cache is keyed by sizing mode (D2).

**Blocked by:** 01 (Browser-parity harness (tracer))

**Status:** ready-for-agent

- [x] Fixtures: flex rows with long text match Chrome's wrapping and boxes exactly.
- [x] A test proves the cache doesn't return a wrapped size for a max-content query (spec risk).
- [x] Changed fixtures carry divergence notes.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

- 2026-10-24, orchestrator 8 (verification): merged 82703e2 into main with no conflicts. Build, vet and gofmt are clean. With GOFLAGS=-p=4, `go test -p 1 -count=1` passes for ./internal/parity/... (mincontent-wrap; every existing fixture unchanged), layoutspec, textspec, ./base, richtextspec, authorspec, examples/zterm and tsxgen. `./gpui/` failed once on TestFrameLoopPreferredFrameIntervalDrawsEveryOtherVblank (the load-sensitive vblank ratio, also seen by 28 and 36), then passed 5/5 isolated plus a full package run. The handle-bound tests pass 5/5. Citation checked: inline_node.cc:2314-2323 (min-content = lines laid out at 0 available inline size). Watch: the measure cache now holds separate min-content and max-content shapes, so flex rows may shape text twice; that matters for the >128-distinct-text handle cap noted in the resume Build speed section.

## Answer

- **Rule (D2, no new API):** every measured text path now wraps at width 0 under a min-content available width, so min-content is the widest unbreakable segment (the longest word), and each measure cache is keyed by sizing mode — a max-content query reuses only an unconstrained entry, never a wrapped one.
- **Files:**
	- `gpui/div.go:612-706` — the Text element's measured leaf: the min-content arm (`div.go:629-631`), the sizing-mode-keyed `shapedCell` reuse (`div.go:540`, `div.go:641-660`) and the min-content size `shaped.width() × lineCount` (`div.go:688-691`).
	- `gpui/styled_text.go:270-355` — StyledText's `TextLayout` measure: the same arm, `textLayoutInner.minContent` and size.
	- `gpui/inline.go:699-713` — the Block div's inline paragraph measure: min-content wraps at 0; its per-wrap-width cache (nil vs 0 vs definite) is already mode-keyed.
	- `gpui/text_mincontent_test.go` — the three path tests; each measures a child under min-content then max-content on the same requested tree (the cache-risk sequence) and asserts the max-content size stays the unwrapped one.
	- `internal/parity/fixtures/mincontent/` + `internal/parity/testdata/mincontent-wrap/` — the fixture (registered in `internal/parity/replay_test.go` and `cmd/parityrecord/main.go`): the paragraph, text-leaf and StyledText rows, the flex-shrink row and the long unbreakable word; one wrong-view test (`WhitespaceNowwrap` = the pre-ticket pin).
- **Chrome citations:** `third_party/blink/renderer/core/layout/inline/inline_node.cc:2314-2323` (ComputeMinMaxSizes: min-content is "the max of inline sizes of all line boxes with 0 available inline size … the width where lines wrap at every break opportunity"), `inline_node.cc:1966-1967` (ComputeContentSize, kMinContent → available inline size `LayoutUnit()` = 0), `inline_node.cc:2265` (the per-line `max`), `line_breaker.h:37` (LineBreakerMode).
- **Divergence notes:** no existing fixture or test changed (they pin fixed sizes or `flex-shrink: 0`, so no text ever shrank in them); `internal/parity/fixtures/textstyle/textstyle.go`'s row comment now records that a flex row wraps too since D2, and `docs/browser-parity-harness.md` carries the ticket-32 entry.
- **Cache/perf:** the element caches stay one entry per sizing mode (replaced, never grown); the wrap-0 shapes are ordinary `shapedLineCache` entries (keyed by wrap width 0), rotated by the two-frame cache and parked under the 256-handle bound — `TestShapedLineCacheParksPastTheHandleBound` and `TestHiddenWindowShapedTextsPastTheHandleBoundKeepDrawing` still pass.
- **Limitations:** min-content ignores `text-overflow` truncation (Chrome ignores it for intrinsic sizes too); `line-clamp` still caps the wrap-0 shaping's rows; the longword span's own box (its width is the word's shaped advance, not layout-integral) is deliberately uninstrumented — the row box and the painted extent pin it; `WhiteSpace: nowrap` keeps min-content = max-content, like Chrome.
