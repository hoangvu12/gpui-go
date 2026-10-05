# Render styled boxes through the scene kernel

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: pi-implementer subagents (parallel), session 065e52f5
Blocked by: 06, 07

## Question

A nested Go scene of boxes, borders, shadows and underlines renders at oracle-checked bounds.

## What to build

Deliver this complete observable path against the [implementation specification](../../gpui-core/spec.md). This ticket is planned work; its status is not evidence that implementation has begun. The selected contracts are [renderer contract](../../../docs/renderer-contract.md), [layout contract](../../../docs/layout-contract.md). Preserve their complete requirements when implementing the bounded path below.

## Acceptance criteria

- [x] Translate semantic paint commands into the finite native scene kernel; preserve BoundsTree insertion, order floors, layers, finish/sorting, batching and paired opacity.
- [x] Implement the box/shadow/underline primitive path and initial atlas resources; compare scene records and plan independently before screenshot assessment.
- [x] Generate and embed pinned shader output with recorded Rust/WGSL/Naga/HLSL/DXBC provenance; validate private wire records separately from GPU buffer layouts.
- [x] Render both composition modes with clipping, color/blend rules and varying DPI; save raw controlled captures without inventing visual thresholds.
- [x] Carry scene/cache ownership separately from submission ownership. Failure and replay tests retain referenced resources until both owners and completion obligations end.
- [x] Attach exact source/artifact/environment identities and executed results for this slice; update capability rows and preserve failures/limitations. Passing an earlier slice does not establish full-core parity.

## Blocking work

- [Compute styled layout through the pinned Taffy service](06-layout-roundtrip.md)
- [Present a clear frame and retire its resources on GPU completion](07-present-and-retire.md)

## Answer

Resolved 2026-10-05 by the implementation chat (Roboco `065e52f5`, pi / iroha/dashscope/glm-5.3), implemented by parallel background subagents and independently re-verified. Complete record: [evidence/ticket08-scene-kernel.json](../../../evidence/ticket08-scene-kernel.json).

- Native scene kernel service (slot 3, capability mask 0xF, native revision 4): the pinned scene.rs/plan.rs semantics — bounds-tree draw orders (max-intersecting+1 with order floors), deferred/after-layer queues, the exact finish sort comparator (stable per-array order sort; boundaries (order, !is_start); surfaces zipped with paired opacities), batch merging with the pinned precedes_limit and PrimitiveKind discriminant order, MAX_FILTER_GROUP_DEPTH=2, nested-opacity multiplication (never merges), the border-split and shadow/underline paint conversions from the pinned window.rs. DLL 783,360 bytes static CRT, no new imports. - Reference oracle fx-0003 scene-painting (142 events, byte-deterministic): the highest public boundary (the pinned Scene public API) records 6 cases of boxes/borders/shadows/underlines/layers/filter boundaries incl. colors as stored HSLA f32 bits. THE GATE: the port reproduces ALL 142 events exactly through its real scene API + native kernel (CompareTraces equal, empty diff); the port's plan is self-consistent and its sorted order matches. 53 Rust tests (incl. ports of the pinned scene/plan tests) + 17 scenespec tests; full suite green. Paths/sprite primitives bounded out of this slice (slots reserved); surfaces opacity covered by kernel tests (the reference insert_surface is crate-private).

## Comments

- 2026-10-05 — Created under the user's authorization to finish Wayfinder, the specification and local implementation tickets. Routine details are delegated; a discovered scope or architecture contradiction must reopen the affected decision with evidence.

