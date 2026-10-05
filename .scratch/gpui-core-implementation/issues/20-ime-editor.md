# Compose and edit text through IMM32

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
Blocked by: 13

## Question

A focused text editor accepts native composition and positions its candidate UI correctly when moved, scrolled or rescaled.

## What to build

Deliver this complete observable path against the [implementation specification](../../gpui-core/spec.md). This ticket is planned work; its status is not evidence that implementation has begun. The selected contracts are [windows platform contract](../../../docs/windows-platform-contract.md), [conformance contract](../../../docs/conformance-contract.md). Preserve their complete requirements when implementing the bounded path below.

## Acceptance criteria

- [ ] Implement UTF-16 document/selection/marked ranges over byte/cluster-aware text geometry, including surrounding text, replacement and selection queries.
- [ ] Handle IMM32 composition/result data, zero-lParam Japanese paths, surrogates, commit/cancel/focus loss and active HWND ownership.
- [ ] Run the independent nonzero-window-origin caret fixture, then move/scroll/DPI cases, to resolve the documented coordinate discrepancy before fixing the adapter convention.
- [ ] Record actual Japanese/Chinese/Korean sessions where installed; preserve unsupported/unavailable cases separately and do not install input packages implicitly.
- [ ] Test nested query/reentry replies and window teardown mid-composition. A source bug requires a narrow evidence-backed parity-policy disposition, not a silently guessed coordinate fix.
- [ ] Attach exact source/artifact/environment identities and executed results for this slice; update capability rows and preserve failures/limitations. Passing an earlier slice does not establish full-core parity.

## Blocking work

- [Route keyboard input through focus, actions and key contexts](13-focus-keyboard.md)

## Comments

- 2026-10-05 — Created under the user's authorization to finish Wayfinder, the specification and local implementation tickets. Routine details are delegated; a discovered scope or architecture contradiction must reopen the affected decision with evidence.

