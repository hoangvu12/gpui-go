# Recover device loss and rebuild retained GPU resources

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
Blocked by: 14, 28

## Question

An app survives device loss or reports an explicit terminal failure while retaining or safely retiring every in-flight dependency.

## What to build

Deliver this complete observable path against the [implementation specification](../../gpui-core/spec.md). This ticket is planned work; its status is not evidence that implementation has begun. The selected contracts are [renderer contract](../../../docs/renderer-contract.md), [runtime ownership contract](../../../docs/runtime-ownership-contract.md). Preserve their complete requirements when implementing the bounded path below.

## Acceptance criteria

- [ ] Inject loss during upload, draw, present and capture copy, including accepted partial submissions and delayed query retirement.
- [ ] Invalidate device generations and GPU cache entries without treating invalidation as completion; preserve CPU/model state needed to reconstruct.
- [ ] Rebuild atlas/targets/cached scenes on the replacement device and redraw multiple windows with fresh generation handles.
- [ ] Prove safe abort where the backend guarantees no further use; otherwise quarantine dependencies and keep the DLL resident with diagnostic accounting.
- [ ] Verify cancellation/resize/shutdown interactions and bounded submission pressure without dropping observable app effects. Record real and injected device identities separately.
- [ ] Attach exact source/artifact/environment identities and executed results for this slice; update capability rows and preserve failures/limitations. Passing an earlier slice does not establish full-core parity.

## Blocking work

- [Preserve keyed state and cached frames while scrolling lists](14-retained-scroll.md)
- [Capture and render frames using the renderer's D3D11 device](28-capture.md)

## Comments

- 2026-10-05 — Created under the user's authorization to finish Wayfinder, the specification and local implementation tickets. Routine details are delegated; a discovered scope or architecture contradiction must reopen the affected decision with evidence.

