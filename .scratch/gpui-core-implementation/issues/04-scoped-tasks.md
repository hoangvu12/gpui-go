# Deliver scoped async results and cancel safely

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: pi-implementer subagents (parallel), session 065e52f5
Blocked by: 03

## Question

An app starts background work, closes its owner, and safely delivers or discards the eventual result.

## What to build

Deliver this complete observable path against the [implementation specification](../../gpui-core/spec.md). This ticket is planned work; its status is not evidence that implementation has begun. The selected contracts are [runtime ownership contract](../../../docs/runtime-ownership-contract.md). Preserve their complete requirements when implementing the bounded path below.

## Acceptance criteria

- [x] Implement deterministic foreground/background scheduling seams, cooperative cancellation and an app registry for detached tasks.
- [x] Retain execution resources until worker acknowledgment; transfer explicitly owned output into a result-message scope with exactly one delivery or discard cleanup.
- [x] Gate late results by scope/entity/window generations even when another owner retains the entity; cancellation never implies worker completion.
- [x] Test canceled-before-start, canceled-during-work, result-before-close, close-before-result, detach, panic and nested scheduling with recorded schedules/seeds.
- [x] Verify app shutdown drains acknowledged tasks while unresolved execution resources remain tracked; never substitute finalizers or a timeout for release acknowledgment.
- [x] Attach exact source/artifact/environment identities and executed results for this slice; update capability rows and preserve failures/limitations. Passing an earlier slice does not establish full-core parity.

## Blocking work

- [Run scoped entities, typed access and ordered effects headlessly](03-scoped-entities-effects.md)

## Answer

Resolved 2026-10-05 by the implementation chat (Roboco `065e52f5`, pi / iroha/dashscope/glm-5.3), implemented by a parallel background subagent and independently re-verified by the orchestrator. Complete record: [evidence/ticket04-scoped-tasks.json](../../../evidence/ticket04-scoped-tasks.json).

- gpui/executor.go extended with seeded/recorded schedules, execution registry, panic containment and worker acknowledgment; gpui/task_ownership.go adds owned result-message handoff with exactly-one delivery-or-discard, generation gating (scope close + entity identity, entity-survives-via-other-lease discard case), detached task registry, panic hooks, UnresolvedExecutions and App.Shutdown drain. 24 tests in internal/taskspec cover canceled-before-start/during-work, result-before-close, close-before-result, detach, panic, nested scheduling, shutdown drain with tracked unresolved resources, and reproducible seeded schedules. Regression gate for fx-0001 stays green.

## Comments

- 2026-10-05 — Created under the user's authorization to finish Wayfinder, the specification and local implementation tickets. Routine details are delegated; a discovered scope or architecture contradiction must reopen the affected decision with evidence.

