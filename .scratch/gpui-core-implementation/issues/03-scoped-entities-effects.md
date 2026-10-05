# Run scoped entities, typed access and ordered effects headlessly

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: pi-implementer subagents (parallel), session 065e52f5
Blocked by: 01

## Question

A model shared by independent owners receives typed updates and events, then releases deterministically under a controlled foreground dispatcher.

## What to build

Deliver this complete observable path against the [implementation specification](../../gpui-core/spec.md). This ticket is planned work; its status is not evidence that implementation has begun. The selected contracts are [runtime ownership contract](../../../docs/runtime-ownership-contract.md), [authoring decision round](../../../docs/authoring-decision-round.md). Preserve their complete requirements when implementing the bounded path below.

## Acceptance criteria

- [x] Implement generation-stamped entities/scopes, independent retain and transfer, alias release, borrowed access and weak upgrade. Verify logical zero is irreversible and active callback pins delay physical reclamation.
- [x] Exercise typed reads/updates and sealed AppContext access, reentry restrictions and expiry diagnostics from an external package; preserve declared event-payload identity including nil interface payloads.
- [x] Run reference traces for release-before-effect, registration order, deferred activation, cancel-next, pending-only notify coalescing/re-notify and every FIFO emit.
- [x] Scope subscriptions even if their handle is ignored; detached subscriptions have weak endpoints. Freeze or explicitly own payload dependencies through retirement and schedule arena-tail release progress.
- [x] Verify two scopes sharing one entity, entity-dependent resources, explicit cycle diagnostics and scope close ordering using virtual time; document Go adaptations beside source-preserved behavior.
- [x] Attach exact source/artifact/environment identities and executed results for this slice; update capability rows and preserve failures/limitations. Passing an earlier slice does not establish full-core parity.

## Blocking work

- [Record independent reference fixtures and the core capability ledger](01-reference-harness.md)

## Answer

Resolved 2026-10-05 by the implementation chat (Roboco `065e52f5`, pi / iroha/dashscope/glm-5.3), implemented by a parallel background subagent and independently re-verified by the orchestrator. Complete record: [evidence/ticket03-scoped-entities.json](../../../evidence/ticket03-scoped-entities.json).

- Real runtime of the root `gpui` package (previously empty): App with reference flush semantics (outermost update boundary, release-before-effects, FIFO effects, pending-only notify coalescing, deferred registration activation, arena-tail retirement), generation-stamped Entity/WeakEntity with the ownership contract's lease semantics (alias, RetainInto/TransferInto/Release idempotence, irreversible logical zero, reentry guards with diagnostics, borrowed facades, construction-failure disposal), Scope tree with the documented close order, typed Event[S,P] with declared payload identity (including nil interface payloads), scope-owned-when-ignored subscriptions, weak endpoints, minimal deterministic executors with virtual clock. - The gate: internal/portfixture reproduces the recorded reference trace fx-0001 events 1-47 EXACTLY (CompareTraces Equal, empty diff, 10x repeat) using the real API; 39 ownership-rule tests from the contract pass. Layout-bounds events remain pending for ticket06 as planned. Full suite, vet and gofmt pass with CGO_ENABLED=0.

## Comments

- 2026-10-05 — Created under the user's authorization to finish Wayfinder, the specification and local implementation tickets. Routine details are delegated; a discovered scope or architecture contradiction must reopen the affected decision with evidence.

- 2026-10-05 — Claimed by the implementation chat and delegated to a parallel background subagent (Roboco session 065e52f5, pi / iroha/dashscope/glm-5.3), ticket01 resolved as its blocker.
