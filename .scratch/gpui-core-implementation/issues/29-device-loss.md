# Recover device loss and rebuild retained GPU resources

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: wave-4 orchestrator chat (Roboco)
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

## Answer

RESOLVED (2026-10-11, wave 4, batches 8+10). The device-loss recovery is live over the rev-11 native artifact:

- **Native (batch 8)**: the renderer v3 device-loss entries — the state check (real vs injected loss recorded separately with the removal reason and the device generation), the test injection, the bounded-retry rebuild (the pin's 350ms settle + 5-attempt backoff; every surface's swap chain rebuilt in place with handles persisting; in-flight submissions aborted with stale handles — the safe-abort release path; the draw globals and the capture view cache dropped; the recovery gate armed until mark_drawable) and mark_drawable.
- **Go (batch 9)**: the deviceLoss controller — pacing-cadence detection, the once-only auto-trigger with the 3-sequence terminal bound, the recovery sequence (rebuild → ledger abort bookkeeping → atlas invalidation over the registered + text-system atlases → gate probes → mark → the fresh-surface fallback → the forced redraws), and the ledger skip semantics ("skipped, redraw pending" — never a silent success).
- **Rev-11 fixes**: the rebuild takes the old composition/views/targets BEFORE recreating (the pin's order — the in-place recreation now succeeds: Rebuilt=1/Failed=0 verified) and the ERR_RECOVERING status split to -13.
- **Gates**: internal/devicelossspec — 10 tests with injected loss over real windows: the full end-to-end (refused retire → abort → atlas invalidation → re-insert → the gate skip → mark → the fresh red→blue pixel change → the ledger), multiple windows, the 64-bound pressure freed by the abort, close/reopen and resize during recovery, shutdown mid-recovery.

Evidence: [evidence/ticket29-device-loss.json](../../../evidence/ticket29-device-loss.json) (executed results; the real-vs-injected separation; the honest not-forceable paths).
