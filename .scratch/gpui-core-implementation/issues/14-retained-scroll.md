# Preserve keyed state and cached frames while scrolling lists

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
Blocked by: 11, 13

## Question

A scrollable/virtualized view reuses retained content while callbacks, focus and dependencies stay valid across frames.

## What to build

Deliver this complete observable path against the [implementation specification](../../gpui-core/spec.md). This ticket is planned work; its status is not evidence that implementation has begun. The selected contracts are [runtime ownership contract](../../../docs/runtime-ownership-contract.md), [renderer contract](../../../docs/renderer-contract.md), [conformance contract](../../../docs/conformance-contract.md). Preserve their complete requirements when implementing the bounded path below.

## Acceptance criteria

- [ ] Implement keyed element state and frame publication; removal followed by reinsertion starts fresh, while an abandoned build preserves the prior published frame without rolling back application mutations.
- [ ] Retain replayed paint, hit-test/dispatch callbacks, focus and model/resource dependencies in frame/cache ownership scopes.
- [ ] Implement scroll state, list/virtualized-list visible-range behavior and pointer hit testing/styles needed by these views; cover stable keys and content insertion/removal.
- [ ] Compare cached versus uncached scene/behavior transcripts, invalidation and dependency changes; test stale captures and callbacks after close.
- [ ] Demonstrate a large representative list and trace visible stutter/input lag if present, without introducing an unapproved numerical benchmark gate.
- [ ] Attach exact source/artifact/environment identities and executed results for this slice; update capability rows and preserve failures/limitations. Passing an earlier slice does not establish full-core parity.

## Blocking work

- [Run the typed counter and custom-component authoring path](11-authoring-counter.md)
- [Route keyboard input through focus, actions and key contexts](13-focus-keyboard.md)

## Comments

- 2026-10-05 — Created under the user's authorization to finish Wayfinder, the specification and local implementation tickets. Routine details are delegated; a discovered scope or architecture contradiction must reopen the affected decision with evidence.

