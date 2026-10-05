# Lay out inline text and embedded elements across fragments

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned (worker aborted on connection errors before writing files; orchestrator takes it next)
Blocked by: 11

## Question

A paragraph mixes shaped text and custom inline boxes while painting and hit testing follow the reference fragments.

## What to build

Deliver this complete observable path against the [implementation specification](../../gpui-core/spec.md). This ticket is planned work; its status is not evidence that implementation has begun. The selected contracts are [layout contract](../../../docs/layout-contract.md), [windows platform contract](../../../docs/windows-platform-contract.md). Preserve their complete requirements when implementing the bounded path below.

## Acceptance criteria

- [ ] Implement Go-owned Inline/InlineFlex orchestration, atomic boxes and multi-fragment placement over the Taffy/text seams; retain separate GPUI semantics from internal Block/Flex mapping.
- [ ] Compare full geometry and measurement-query histories for wrapping, mixed fonts, baseline/alignment, proportional padding, text parent-relative snapping and DPI/rem changes.
- [ ] Exercise refinement order and exposed flex/grid/float/calc/content-size combinations in the real authoring path, closing the remaining layout capability rows.
- [ ] Verify painting, clipping, hit testing and selection use the same fragment geometry, including empty runs and boxes crossing wrap boundaries.
- [ ] Test recompute/reset/cache histories and failed/reentrant measurements; record exact oracle outputs and controlled captures.
- [ ] Attach exact source/artifact/environment identities and executed results for this slice; update capability rows and preserve failures/limitations. Passing an earlier slice does not establish full-core parity.

## Blocking work

- [Run the typed counter and custom-component authoring path](11-authoring-counter.md)

## Comments

- 2026-10-05 — Created under the user's authorization to finish Wayfinder, the specification and local implementation tickets. Routine details are delegated; a discovered scope or architecture contradiction must reopen the affected decision with evidence.

