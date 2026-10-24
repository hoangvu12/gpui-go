# 46 - Scroll anchoring

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 8 (Claude Code)
Blocked by: 01
Triage: ready-for-agent

**What to build:** Inserting content above the visible area keeps the visible content in place, as Chrome's scroll anchoring does.

**Blocked by:** 01 (Browser-parity harness (tracer))

**Status:** ready-for-agent

- [ ] Fixtures inserting rows above the viewport match Chrome's resulting scroll offsets.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer

- **Behaviour.** A scroll container scrolled off rest keeps its visible content in place when content is inserted above it: the offset follows the insertion by the anchor's content-space delta, exactly as Chrome's scroll anchoring adjusts it. A user scroll between frames adjusts nothing (the anchor's position is measured in content space, which a scroll does not move); at a zero block offset the anchor is cleared (no anchoring from rest); the anchor axis is Y (the port's only writing mode); the delta rounds to whole pixels.
- **Files.** `gpui/scroll_anchor.go` (new): the engine's anchoring walk — `ScrollAnchorFacts` (the unpruned preorder enumeration that gives every occupying node its `(element id path, occurrence)` identity and resolves the saved one, plus the pruned DFS selection) — and the element-side `applyScrollAnchoring` (the prepaint adjustment, before the clamp); `gpui/scroll_element.go` + `gpui/scroll.go` (the `scrollAnchorState` cell lives in the same persistent state as the offset: `divScrollElementState`/`scrollHandleState`, threaded as `elementScroll.anchor`); `gpui/div.go:1584-1594` (a scroller's request turns the node-path bookkeeping on before its subtree is walked) and `gpui/div.go:1900-1906` (the prepaint calls the adjustment before the clamp); `gpui/layout.go:668-704`/`gpui/containingblock.go:186-206` (the node-path recording, `nodePaths`/`RequestAnchorBookkeeping`/`recordAnchorPath` — one small string per node per frame, only for windows with scroll containers).
- **The selection follows Chromium's DFS** (`scroll_anchor.cc:397-475` FindAnchor/FindAnchorRecursive, `:360-395` Examine): the first DOM-order box whose rect (the border box, bottom extended by its content overflow) occupies space and intersects the scrollport; a fully-contained box stops the walk, a partial one is descended into (a deeper fully-visible descendant is a better anchor) and ends the sibling examination; sticky subtrees, non-occupying and non-intersecting boxes are skipped with their subtrees. Measured: with rows inside a wrapper, Chrome anchors the first visible ROW (the walk descends past the partially-visible wrapper) — the fixture reproduces this.
- **The adjustment follows NotifyBeforeLayout/Adjust** (`scroll_anchor.cc:613-651`, `:713-760`, ComputeAdjustment `:653-695`): the saved position is the anchor's content-space top under the previous frame's layout (the port's equivalent of the before-layout save, since its layout runs in the request phase), the delta is rounded and applied to the block axis, a dead anchor re-selects without adjusting, and the zero-offset rule clears at rest.
- **Fixtures.** `internal/parity/fixtures/anchoring` (registered in `internal/parity/replay_test.go` and `cmd/parityrecord/main.go`): `anchoring` — a 240x200 scroller over 9 rows, scrolled to the end with the End key, then a 50px row inserted above the viewport through the harness's new `parity.Script` step (the recorder evaluates the page's `insertBefore` on the settled page; the replay flips the view's per-log state and refreshes). Tier A exact: the anchor row `r6` stays at the scrollport's top edge (y=10) after the insertion, `r7`/`r8` likewise, `inserted` sits above the viewport at y=-290 — Chrome's record matches the port bit-for-bit (recorded with Chrome 155.0.8059.85). Tier C pixel-exact (0.0000 mean). No wrong-view fixture test: the anchoring cannot be disabled view-side, so the discrimination is the library-level red/green (`gpui/scroll_anchor_test.go`) and the record's Tier A gate — both verified to fail with the anchoring call removed (the boxes land 50px low).
- **Tests.** `gpui/scroll_anchor_test.go`: the insertion-above case (the offset -200 → -250, the anchor row's bounds stay at y=0, the inserted row at -250) and the suppression rules (no anchoring from rest; user scrolls pass through untouched).
- **Key citations** (%TEMP%
esearch-src\chromium, third_party/blink/renderer/core/layout/scroll_anchor.cc): `:397-475` (FindAnchor/FindAnchorRecursive), `:360-395` (Examine: the intersection test, the overflow-extended rect, sticky skip), `:613-651` (NotifyBeforeLayout: the zero-offset clear, the save), `:653-695` (ComputeAdjustment: the rounding, the block axis only), `:713-760` (Adjust: the offset move, the dead-anchor re-selection).
- **Limitations:** the anchor's cross-frame identity is the element id path plus the occurrence index among same-path nodes (Chromium's LayoutObject pointer is exact; unnamed rows inserted above shift the occurrences and can misresolve — the serialized-selector approximation); the walk is O(subtree) per scrolled scroller per frame (twice on selection frames; Chromium's steady state is O(1)); no `overflow-anchor: none` opt-out, no scroll-anchor-disabling style suppression, no priority candidates (focus/find-in-page); the rect's bottom extension ignores clipping; auto end margins resolve to zero; horizontal-tb only.
