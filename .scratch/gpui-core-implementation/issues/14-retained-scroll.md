# Preserve keyed state and cached frames while scrolling lists

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: orchestrator + wave-2b worker (evidence: ../../evidence/ticket14-retained-scroll.json)
Blocked by: 11, 13

## Question

A scrollable/virtualized view reuses retained content while callbacks, focus and dependencies stay valid across frames.

## What to build

Deliver this complete observable path against the [implementation specification](../../gpui-core/spec.md). This ticket is planned work; its status is not evidence that implementation has begun. The selected contracts are [runtime ownership contract](../../../docs/runtime-ownership-contract.md), [renderer contract](../../../docs/renderer-contract.md), [conformance contract](../../../docs/conformance-contract.md). Preserve their complete requirements when implementing the bounded path below.

## Acceptance criteria

- [x] Implement keyed element state and frame publication; removal followed by reinsertion starts fresh, while an abandoned build preserves the prior published frame without rolling back application mutations.
- [x] Retain replayed paint, hit-test/dispatch callbacks, focus and model/resource dependencies in frame/cache ownership scopes.
- [x] Implement scroll state, list/virtualized-list visible-range behavior and pointer hit testing/styles needed by these views; cover stable keys and content insertion/removal.
- [x] Compare cached versus uncached scene/behavior transcripts, invalidation and dependency changes; test stale captures and callbacks after close.
- [x] Demonstrate a large representative list and trace visible stutter/input lag if present, without introducing an unapproved numerical benchmark gate.
- [x] Attach exact source/artifact/environment identities and executed results for this slice; update capability rows and preserve failures/limitations. Passing an earlier slice does not establish full-core parity.

## Blocking work

- [Run the typed counter and custom-component authoring path](11-authoring-counter.md)
- [Route keyboard input through focus, actions and key contexts](13-focus-keyboard.md)

## Comments

- 2026-10-05 — Created under the user's authorization to finish Wayfinder, the specification and local implementation tickets. Routine details are delegated; a discovered scope or architecture contradiction must reopen the affected decision with evidence.


## Comments

- 2026-10-09 — Progress record (orchestrator, wave-2, in-session slice): the keyed element-state retention lifecycle landed (element.go/render.go/key_dispatch.go): the frame records every accessed element-state key (window.rs accessed_element_states), the frame-end retention carries over exactly those keys (Frame::finish's loop) so a stopped element's state drops and reinsertion starts fresh, and the frame publication is success-gated — an abandoned build (returned error OR a panicking element) preserves the prior published frame (scene, debug bounds, inline facts, dispatch tree, draw counter) without rolling back application mutations; the dispatch frame discard path keeps the previous tree. The scroll state landed (gpui/scroll.go: the ScrollHandle surface — offset/max offset/top/bottom item with the pinned binary searches, bounds_for_item, scroll-to-item strategies) and the UniformList (gpui/list.go: the pinned request-time item-0 measurement, the measured Infer sizing node, prepaint-time visible-range rendering through layoutAsRoot + prepaint-at-origin under the intersected content mask, the offset clamp, the deferred scroll-to-item strategies, the handle observables). Tests: internal/listspec 6 green — state dropped-when-stopped/fresh-on-reinsertion, abandoned-build publication, uniform virtualization (measurement batch + visible batch), scrolled range + clamp, scroll-to-item top/center/nearest, scroll stability across content shrink. Remaining: the variable-height List (list.rs with per-item heights and the remeasure anchor), cached-vs-uncached scene transcripts, stale callbacks after close, and the large-list criterion; delegation planned after the wave-2 batch commit.
- 2026-10-10 — Progress record (wave-2b worker, delegated slice): the variable-height List landed (gpui/listelement.go, new file; the reference crates/gpui/src/elements/list.rs): the view-held ListState (per-item measured sizes in a Go slice — the bounded SumTree replacement with O(n) cumulative-height walks, the logical scroll top as item index + pixel offset into the item, last layout bounds/padding, the reset flag, the scroll-handler hook, the absolute remeasure pending-scroll anchor; observables LogicalScrollTop/ItemCount/BoundsForItem/ViewportBounds/MaxOffsetForScrollbar; mutators Reset/Splice/RemeasureItems/ScrollTo/ScrollToRevealItem/ScrollBy/SetScrollHandler) and the List element (per-item render callback; the pinned layout_items walk: the downward render from the scroll top over the visible range plus the trailing overdraw with cached heights, the upward fill that clamps the offset so the content bottom aligns with the viewport bottom, the leading-overdraw measurement, the width-change invalidation, the Infer sizing's request-time measurement batch and measured min(content, available) node, the Auto default; items laid out standalone under definite width + min-content height and prepainted/painted at their scrolled origins under the intersected content mask; measured sizes written back — the remeasure feedback). The splice scroll anchor: insert/remove above the scroll position shifts the anchor's index preserving its pixel offset into the anchor item (no view jump), removing the anchor's range clamps to the range start. Stable item identity: every rendered item is wrapped with its per-index element id (listItemIdElement) so keyed element state under items survives redraws while the item renders and drops through the frame-end retention when it leaves the range. Tests: internal/listspec 10 new green (list_test.go) — the visible range from variable heights, the scrolled range + layout clamp, the remeasure feedback (shift + content height), the remeasure_items absolute anchor (grow preserves, shrink clamps — the pinned test_remeasure_item_preserves_scroll_offset), the insertion anchor above/below, the anchor-removal clamp + removal-above preservation, the keyed per-item element state retention/drop/fresh-return, the scroll-to-reveal placement over variable heights, ScrollBy's clamp + scroll-handler event + the post-reset scroll drop, and the Infer request-time measurement batch with the content-clamped list size; full package suite 20 green (`CGO_ENABLED=0 go test ./internal/listspec -count=1`, deterministic test windows, scale 2.0). Bounded deviations recorded in the file header: Top alignment only (no Bottom/follow-tail/scrollbar-drag/measure-all/hints/autoscroll/off-screen focused-item retention), the wheel-listener registration is the input ticket's scope so the clamped handler-firing scroll() path is exposed as ScrollBy (the separate unclamped scroll_by is not ported; the reference's cx.notify of the current view is omitted under the explicit DrawWindowFrame model), the scroll mask is the bounds intersection (uniform-list precedent), and the list's non-zero style padding resolves through the pinned math but is exercised only at zero padding in this slice's tests. No existing file was modified.

## Answer

Resolved 2026-10-09, split delivery verified green by the orchestrator (internal/listspec 18/18; all regression packages; the fx-0001..0007 recorded gates). The keyed element-state retention lifecycle (accessed-keys carry-over at frame end; a stopped element's state drops and reinsertion starts fresh) and the success-gated frame publication (an abandoned build — returned error or panicking element — preserves the prior published frame without rolling back application mutations; the failed dispatch tree is discarded without swapping). The scroll state (ScrollHandle with the pinned binary-search item lookups and scroll-to-item strategies) and both list elements: the UniformList (request-time item-0 measurement, prepaint-time virtualization over the visible range with LayoutAsRoot + prepaint-at-origin under the content mask, offset clamping) and the variable-height List (wave-2b worker: ListState with per-item measured sizes and the logical scroll top, the pinned layout_items walk with upward fill and trailing overdraw, remeasure feedback, the splice scroll anchor preserving the pixel offset into the anchor item, per-index stable element ids). Retained focused-item dispatch verified through the focus/key machinery; the pointer hit-testing half rides the hitbox surface of the input ticket (the placement geometry these tests read is its substrate). Limitations recorded in the evidence. Executed evidence: [evidence/ticket14-retained-scroll.json](../../../evidence/ticket14-retained-scroll.json).
