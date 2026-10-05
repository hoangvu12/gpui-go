# Complete native dialogs and credential storage operations

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: claimed
Assignee: pi-implementer 386673cc (successor chat; ticket25 subagent)
Blocked by: 04, 05

## Question

An app prompts for files or confirmation and reads/writes/deletes credentials without blocking foreground progress or leaking resources.

## What to build

Deliver this complete observable path against the [implementation specification](../../gpui-core/spec.md). This ticket is planned work; its status is not evidence that implementation has begun. The selected contracts are [windows platform contract](../../../docs/windows-platform-contract.md), [conformance contract](../../../docs/conformance-contract.md). Preserve their complete requirements when implementing the bounded path below.

## Acceptance criteria

- [ ] Implement source-supported prompt/open/save/path operations and cancellation/result delivery through the selected modal and async contracts.
- [ ] Demonstrate nested modal pumping while another window remains responsive and late results are discarded after owner closure.
- [ ] Implement Windows Credential Manager operations independently of clipboard, using caller-supplied test entries and explicit error/absence results, including maximum credential-blob-size rejection.
- [ ] Round trip temporary test data and clean up only those test-created entries; never enumerate or print unrelated credentials.
- [ ] Verify COM allocations/apartment release and source-supported errors using real host calls plus deterministic cancellation traces.
- [ ] Attach exact source/artifact/environment identities and executed results for this slice; update capability rows and preserve failures/limitations. Passing an earlier slice does not establish full-core parity.

## Blocking work

- [Deliver scoped async results and cancel safely](04-scoped-tasks.md)
- [Open and close a DPI-aware window on the foreground thread](05-win32-window.md)

## Comments

- 2026-10-05 — Created under the user's authorization to finish Wayfinder, the specification and local implementation tickets. Routine details are delegated; a discovered scope or architecture contradiction must reopen the affected decision with evidence.
