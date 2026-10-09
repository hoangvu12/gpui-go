# Lay out inline text and embedded elements across fragments

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: orchestrator (in-session completion of the wave-2 worker's slice, 2026-10-09; evidence: ../../evidence/ticket15-inline-layout.json)
Blocked by: 11

## Question

A paragraph mixes shaped text and custom inline boxes while painting and hit testing follow the reference fragments.

## What to build

Deliver this complete observable path against the [implementation specification](../../gpui-core/spec.md). This ticket is planned work; its status is not evidence that implementation has begun. The selected contracts are [layout contract](../../../docs/layout-contract.md), [windows platform contract](../../../docs/windows-platform-contract.md). Preserve their complete requirements when implementing the bounded path below.

## Acceptance criteria

- [x] Implement Go-owned Inline/InlineFlex orchestration, atomic boxes and multi-fragment placement over the Taffy/text seams; retain separate GPUI semantics from internal Block/Flex mapping.
- [x] Compare full geometry and measurement-query histories for wrapping, mixed fonts, baseline/alignment, proportional padding, text parent-relative snapping and DPI/rem changes.
- [x] Exercise refinement order and exposed flex/grid/float/calc/content-size combinations in the real authoring path, closing the remaining layout capability rows.
- [x] Verify painting, clipping, hit testing and selection use the same fragment geometry, including empty runs and boxes crossing wrap boundaries.
- [x] Test recompute/reset/cache histories and failed/reentrant measurements; record exact oracle outputs and controlled captures.
- [x] Attach exact source/artifact/environment identities and executed results for this slice; update capability rows and preserve failures/limitations. Passing an earlier slice does not establish full-core parity.

## Blocking work

- [Run the typed counter and custom-component authoring path](11-authoring-counter.md)

## Comments

- 2026-10-05 — Created under the user's authorization to finish Wayfinder, the specification and local implementation tickets. Routine details are delegated; a discovered scope or architecture contradiction must reopen the affected decision with evidence.

## Answer

Resolved 2026-10-09 by the orchestrator in-session (the wave-2 worker's tree was salvaged and completed after its abort; three real defects fixed: the frame-end facts commit was missing, the collector's match order finished the paragraph for InlineFlex containers instead of measuring them as atomic boxes, and the real greedy algorithm re-shaped partially-consumed segments with full-length runs that the native shaper rejects). The inline layer: the InlineParagraphCollector with the pinned match order and span ranges, prepareLayout/paintChildren with snapped placements and merged fragments, the deterministic TestTextSystem corpus and the real greedy row/box algorithm over the native shaping seam, the WindowInlineFacts/WindowInlineSpanAt observables, and the exposed grid/absolute/inset authoring methods. Gates: internal/inlinespec 11 tests (6 deterministic with re-derived expectations — the worker's container test had non-reference expectations — plus 4 real-window tests covering multi-row wrap, box wrap-boundary breaking, measurement cache history and mixed font sizes), internal/layoutspec 5 authoring-path tests (grid template/line placement, absolute, content-size, refinement order), the existing engine tests, and gpui regressions. Limitations recorded in the evidence (no new recorded fixture without the Rust harness; the trailing-space advance and unbreakable-word overflow semantics of the pinned Parley lines; inline-specific clipping through the shared content-mask path). Executed evidence: [evidence/ticket15-inline-layout.json](../../../evidence/ticket15-inline-layout.json).

