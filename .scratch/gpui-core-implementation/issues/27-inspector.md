# Inspect live element state and expose debug diagnostics

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
Blocked by: 14, 21

## Question

A developer inspects the live element tree and diagnoses ownership or layout problems without altering release behavior.

## What to build

Deliver this complete observable path against the [implementation specification](../../gpui-core/spec.md). This ticket is planned work; its status is not evidence that implementation has begun. The selected contracts are [authoring decision round](../../../docs/authoring-decision-round.md), [conformance contract](../../../docs/conformance-contract.md). Preserve their complete requirements when implementing the bounded path below.

## Acceptance criteria

- [ ] Implement the applicable interactive inspector UI and element/global/inspector ID paths, including debug_assertions behavior separately from feature flags.
- [ ] Expose reference debug/test diagnostics and leak/cycle reports using semantic labels; preserve release-profile behavior and explicit capability queries.
- [ ] Inspect a cached/virtualized custom-component tree and verify selecting nodes corresponds to current bounds/state after removal or reparenting.
- [ ] Exercise diagnostics during abandoned frames and window closure without reviving dead entities or keeping unwanted strong cycles.
- [ ] Close the debug/test/inspector ledger rows with fixtures; Rust-specific instrumentation mechanisms need recorded applicability, not an invented port feature exemption.
- [ ] Attach exact source/artifact/environment identities and executed results for this slice; update capability rows and preserve failures/limitations. Passing an earlier slice does not establish full-core parity.

## Blocking work

- [Preserve keyed state and cached frames while scrolling lists](14-retained-scroll.md)
- [Publish semantic accessibility trees from real elements](21-accessibility-tree.md)

## Comments

- 2026-10-05 — Created under the user's authorization to finish Wayfinder, the specification and local implementation tickets. Routine details are delegated; a discovered scope or architecture contradiction must reopen the affected decision with evidence.

