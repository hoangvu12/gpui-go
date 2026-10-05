# Verify cross-service close, cancellation and shutdown

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
Blocked by: 19, 20, 22, 24, 25, 26, 27, 29

## Question

A multiwindow app closes while tasks, modal operations, UIA, capture and rendering are active, and every owner reaches its specified terminal state.

## What to build

Deliver this complete observable path against the [implementation specification](../../gpui-core/spec.md). This ticket is planned work; its status is not evidence that implementation has begun. The selected contracts are [runtime ownership contract](../../../docs/runtime-ownership-contract.md), [conformance contract](../../../docs/conformance-contract.md). Preserve their complete requirements when implementing the bounded path below.

## Acceptance criteria

- [ ] Run deterministic adversarial schedules for shared entities, scope cancellation, result handoff, effect flushing, focus release and abandoned frames across completed services.
- [ ] Run real multiwindow close/reopen/modal/provider/capture cases with delayed GPU completion and callbacks arriving after window removal.
- [ ] Verify exactly-once cleanup and explicit unresolved quarantine records; neither GC, elapsed time nor process-resident modules substitutes for logical release.
- [ ] Compare reference-preserved traces separately from documented Go adaptations; retain failed seeds and minimized reproducers.
- [ ] Demonstrate representative app responsiveness and investigate visible regressions. This integration ticket does not introduce a comparative benchmark requirement.
- [ ] Attach exact source/artifact/environment identities and executed results for this slice; update capability rows and preserve failures/limitations. Passing an earlier slice does not establish full-core parity.

## Blocking work

- [Render SVGs with lazy fonts and correct alpha conversion](19-svg-fonts.md)
- [Compose and edit text through IMM32](20-ime-editor.md)
- [Expose the published tree to Windows UI Automation](22-uia-provider.md)
- [Handle OLE drag/drop and precision touchpad gestures](24-drop-touchpad.md)
- [Complete native dialogs and credential storage operations](25-dialogs-credentials.md)
- [Implement remaining Windows app and desktop operations](26-desktop-operations.md)
- [Inspect live element state and expose debug diagnostics](27-inspector.md)
- [Recover device loss and rebuild retained GPU resources](29-device-loss.md)

## Comments

- 2026-10-05 — Created under the user's authorization to finish Wayfinder, the specification and local implementation tickets. Routine details are delegated; a discovered scope or architecture contradiction must reopen the affected decision with evidence.

