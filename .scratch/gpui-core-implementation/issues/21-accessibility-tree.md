# Publish semantic accessibility trees from real elements

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
Blocked by: 11, 13

## Question

A custom view publishes stable semantic nodes and responds to accessibility actions through foreground dispatch.

## What to build

Deliver this complete observable path against the [implementation specification](../../gpui-core/spec.md). This ticket is planned work; its status is not evidence that implementation has begun. The selected contracts are [windows platform contract](../../../docs/windows-platform-contract.md), [authoring decision round](../../../docs/authoring-decision-round.md). Preserve their complete requirements when implementing the bounded path below.

## Acceptance criteria

- [ ] Implement type-asserted element accessibility companions, stable node identity/hashing, hidden/property changes, synthetic children and focus semantics.
- [ ] Specify node-ID determinism/collision handling from semantic identity and exercise removal/reinsertion and per-window generation separation.
- [ ] Build immutable published snapshots independent of mutable app state and compare activation/update/action traces with reference fixtures.
- [ ] Route semantic actions through generation-checked foreground tokens; stale actions after close are rejected without reviving entities.
- [ ] Exercise a real counter/editor and custom Element phases; record tree snapshots and action results without claiming native UIA interoperability yet.
- [ ] Attach exact source/artifact/environment identities and executed results for this slice; update capability rows and preserve failures/limitations. Passing an earlier slice does not establish full-core parity.

## Blocking work

- [Run the typed counter and custom-component authoring path](11-authoring-counter.md)
- [Route keyboard input through focus, actions and key contexts](13-focus-keyboard.md)

## Comments

- 2026-10-05 — Created under the user's authorization to finish Wayfinder, the specification and local implementation tickets. Routine details are delegated; a discovered scope or architecture contradiction must reopen the affected decision with evidence.

