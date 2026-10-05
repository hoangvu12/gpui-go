# Render paths, surfaces and nested filters with the full scene plan

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: pi-implementer subagents (parallel), session 065e52f5
Blocked by: 08

## Question

An application paints every remaining scene primitive and composes nested filters and surfaces without ordering or opacity errors.

## What to build

Deliver this complete observable path against the [implementation specification](../../gpui-core/spec.md). This ticket is planned work; its status is not evidence that implementation has begun. The selected contracts are [renderer contract](../../../docs/renderer-contract.md). Preserve their complete requirements when implementing the bounded path below.

## Acceptance criteria

- [x] Complete the scene primitive inventory, path targets and reference MSAA behavior, sprite/surface opacity pairing, clipping and advanced atlas formats.
- [x] Match independent insertion/layer/order-floor/replay/batch/filter-plan fixtures, including overlap, cached ranges and filter nesting beyond depth two.
- [x] Implement isolated-content/backdrop/offscreen transitions and the reference inline handling for deeper filter groups; compare intermediate targets as well as final images.
- [x] Validate GPU buffer layouts, shader hashes, blend/color/gamma/alpha behavior and both composition modes on the actual adapter; unsupported feature-level results remain explicit.
- [x] Stress temporary target reuse and delayed GPU retirement. Keep capture producer ownership assigned to its separate slice.
- [x] Attach exact source/artifact/environment identities and executed results for this slice; update capability rows and preserve failures/limitations. Passing an earlier slice does not establish full-core parity.

## Blocking work

- [Render styled boxes through the scene kernel](08-paint-boxes.md)

## Answer

Resolved 2026-10-06 by the implementation chat (Roboco `065e52f5`), implemented by a background subagent (resumed once after a mid-work crash and completed) and independently re-verified (2/2 full-suite runs). Complete record: [evidence/ticket16-paths-filters.json](../../../evidence/ticket16-paths-filters.json).

- Scene service v3 (paths: the pinned PathBuilder with tessellation, fills/strokes/caps/joins/dash/transforms) and renderer service v2 (the full pinned draw path: 68 prebuilt DXBC shader blobs compiled at maintainer build exactly like the pinned gpui_render build chain; instance pipelines, two-pass MSAA paths, sprites via atlas SRVs, the SURFACES pipeline, the pinned blur/composite filter chain with isolated group targets and deeper groups inline, staging readback). Native revision 7, mask 0xFF, DLL 12,489,728 bytes static CRT.
- THE GATE: fx-0007-paths-filters (428 events) reproduced bit-exactly — every tessellation vertex bit, batch structure, and the depth-3 filter-target plan. fx-0001..0006 unchanged.
- Real-window composite draw with PIXEL VERIFICATION via staging readback: quad + blur filter group + red path + real text sprite, presented and retired through the GPU ledger.
- Honest boundary: imported-surface pixel output awaits ticket28 (the pinned draw_surfaces only imports WindowsCapture; the Unsupported stand-in draws through the pinned error path).

## Comments

- 2026-10-05 — Created under the user's authorization to finish Wayfinder, the specification and local implementation tickets. Routine details are delegated; a discovered scope or architecture contradiction must reopen the affected decision with evidence.

