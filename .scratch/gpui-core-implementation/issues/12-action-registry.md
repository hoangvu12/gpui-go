# Register, serialize and bind typed actions

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: pi-implementer subagents (parallel), session 065e52f5
Blocked by: 03

## Question

An app defines unit and rich actions, loads a key binding payload and dispatches the correctly cloned bound value.

## What to build

Deliver this complete observable path against the [implementation specification](../../gpui-core/spec.md). This ticket is planned work; its status is not evidence that implementation has begun. The selected contracts are [authoring decision round](../../../docs/authoring-decision-round.md), [conformance contract](../../../docs/conformance-contract.md). Preserve their complete requirements when implementing the bounded path below.

## Acceptance criteria

- [x] Implement canonical once-per-type action descriptors separately from explicit named setup-time registration, names/aliases and duplicate diagnostics.
- [x] Require explicit Clone/Equal for rich actions, constrain unit factories to empty structs and preserve NoAction/Unbind semantics.
- [x] Implement optional JSON/schema capabilities and distinct NoJSON/unregistered/invalid-payload failures. Select and record any schema dependency against pinned source fixtures, without broadening public consumer tooling.
- [x] Verify a boxed binding delivers its captured cloned payload even if a different dispatch instance is supplied; keep callback action values read-only by contract.
- [x] Cover external typed callbacks, wrong types, registration order/duplicates and JSON/schema round trips with oracle cases and selected Go-adaptation assertions.
- [x] Attach exact source/artifact/environment identities and executed results for this slice; update capability rows and preserve failures/limitations. Passing an earlier slice does not establish full-core parity.

## Blocking work

- [Run scoped entities, typed access and ordered effects headlessly](03-scoped-entities-effects.md)

## Answer

Resolved 2026-10-05 by the implementation chat (Roboco `065e52f5`, pi / iroha/dashscope/glm-5.3), implemented by a parallel background subagent and independently re-verified by the orchestrator. Complete record: [evidence/ticket12-action-registry.json](../../../evidence/ticket12-action-registry.json).

- gpui/action.go implements canonical once-per-type descriptors with duplicate diagnostics, DefineUnitAction[~struct{}] with unit defaults, rich-action Clone/Equal requirements, per-app named registration separate from definitions, alias resolution, BuildAction with three distinct error categories (unregistered / invalid payload / NoJSON), BoxedAction clone-at-capture and clone-on-delivery (bind-time clone wins over dispatched instance, read-only by copy), NoAction/Unbind builtins with semantics quoted from the pinned action.rs/keymap.rs, and the binding core for later element dispatch. 27 tests (21 in-package + 6 external-package). Schema dependency decision: none selected (stdlib encoding/json; schemars aggregation deferred to the keymap/schema owner).

## Comments

- 2026-10-05 — Created under the user's authorization to finish Wayfinder, the specification and local implementation tickets. Routine details are delegated; a discovered scope or architecture contradiction must reopen the affected decision with evidence.

