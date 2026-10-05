# Handle OLE drag/drop and precision touchpad gestures

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
Blocked by: 13, 23

## Question

A view accepts native drops and scroll/gesture input with correct targeting and cancellation.

## What to build

Deliver this complete observable path against the [implementation specification](../../gpui-core/spec.md). This ticket is planned work; its status is not evidence that implementation has begun. The selected contracts are [windows platform contract](../../../docs/windows-platform-contract.md), [runtime ownership contract](../../../docs/runtime-ownership-contract.md). Preserve their complete requirements when implementing the bounded path below.

## Acceptance criteria

- [ ] Implement OLE source/target adapters and drag data/effect negotiation across native and internal views.
- [ ] Route pointer/drag hit tests and focus through the real element tree; test leave/cancel/drop and window closure during a nested OLE loop.
- [ ] Integrate Direct Manipulation gesture/scroll updates and callback scheduling on the required apartment/thread.
- [ ] Verify clipboard-compatible formats and retained drag data survive asynchronous COM callbacks without retaining expired Go borrows.
- [ ] Compare native interaction transcripts and record actual touchpad availability; a simulated gesture validates dispatch only, not device interoperability.
- [ ] Attach exact source/artifact/environment identities and executed results for this slice; update capability rows and preserve failures/limitations. Passing an earlier slice does not establish full-core parity.

## Blocking work

- [Route keyboard input through focus, actions and key contexts](13-focus-keyboard.md)
- [Exchange clipboard text, files, images and metadata](23-clipboard.md)

## Comments

- 2026-10-05 — Created under the user's authorization to finish Wayfinder, the specification and local implementation tickets. Routine details are delegated; a discovered scope or architecture contradiction must reopen the affected decision with evidence.

