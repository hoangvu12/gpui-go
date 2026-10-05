# Present a clear frame and retire its resources on GPU completion

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: pi-implementer subagents (parallel), session 065e52f5
Blocked by: 05

## Question

A window presents a color frame while a resource ledger proves that accepted work retires only after actual GPU completion.

## What to build

Deliver this complete observable path against the [implementation specification](../../gpui-core/spec.md). This ticket is planned work; its status is not evidence that implementation has begun. The selected contracts are [renderer contract](../../../docs/renderer-contract.md), [runtime ownership contract](../../../docs/runtime-ownership-contract.md). Preserve their complete requirements when implementing the bounded path below.

## Acceptance criteria

- [x] Create the pinned D3D11 device and both DComp premultiplied/HWND alpha-ignore swap-chain modes with the expected format, buffering and feature-level selection.
- [x] Submit a real clear/present and insert D3D11_QUERY_EVENT after the final use. Keep acceptance separate from retirement; GetData S_OK plus true retires, S_FALSE remains pending.
- [x] Ensure the marker is actually submitted during hidden/upload-only/quiescing work, using the selected Flush policy without a foreground busy wait; Present or Flush alone is never completion.
- [x] Exercise delayed completion, resize, minimized/occluded windows and partial-submit failure. A proved safe abort may retire; uncertain loss quarantines dependencies and keeps the module resident.
- [x] Verify native immediate-context serialization, bounded pending submissions and pacing-worker handoff with two windows. Save real device/environment identities and protocol traces.
- [x] Attach exact source/artifact/environment identities and executed results for this slice; update capability rows and preserve failures/limitations. Passing an earlier slice does not establish full-core parity.

## Blocking work

- [Open and close a DPI-aware window on the foreground thread](05-win32-window.md)

## Answer

Resolved 2026-10-05 by the implementation chat (Roboco `065e52f5`, pi / iroha/dashscope/glm-5.3), implemented by a background subagent and independently re-verified. Complete record: [evidence/ticket07-present-and-retire.json](../../../evidence/ticket07-present-and-retire.json).

- Native renderer service (slot 2, capability mask 7, native revision 3): the pinned D3D11 device semantics (feature levels 11_1/11_0, BGRA support, first-success adapter, no WARP), BOTH swap-chain modes (DirectComposition PREMULTIPLIED + HWND alpha-ignore), DXGI_FORMAT_B8G8R8A8_UNORM, 3 buffers FLIP_SEQUENTIAL, the pinned resize path, and a D3D11_QUERY_EVENT retirement protocol: marker after the last command, GetData S_OK+true retires, S_FALSE stays pending, exactly-once retire, bounded 64 pending with typed backpressure, immediate-context serialization, GetDeviceRemovedReason quarantine semantics. DLL 675,328 bytes static CRT (d3d11/dcomp/dxgi imports only, OS components).
- Real-GPU proof on this machine (NVIDIA GeForce RTX 5050, FL 11_1, driver 610.74): present -> poll -> retire flows in both modes; S_FALSE pending observations captured; the resource ledger proves retirement only after a completed poll (pending-then-completed traces, never completed-before-poll); two windows serialized; resize through the real path; hidden-window submission retired via a real poll; backpressure and pacing-worker foreground dispatch verified; forced GC clean. Full suite green.
- Honest limitations: device removal not forceable on healthy hardware (paths implemented and unit-validated; ticket29 owns the forced-loss harness); minimized/occluded and partial-submit failure paths typed but not exercised end-to-end; per-submission Flush and the retirement protocol are documented service-seam additions per the contract.

## Comments

- 2026-10-05 — Created under the user's authorization to finish Wayfinder, the specification and local implementation tickets. Routine details are delegated; a discovered scope or architecture contradiction must reopen the affected decision with evidence.

