# Compute styled layout through the pinned Taffy service

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: pi-implementer subagents (parallel), session 065e52f5
Blocked by: 02

## Question

A Go-authored style tree produces full-node geometry matching the independent reference through the real DLL.

## What to build

Deliver this complete observable path against the [implementation specification](../../gpui-core/spec.md). This ticket is planned work; its status is not evidence that implementation has begun. The selected contracts are [layout contract](../../../docs/layout-contract.md). Preserve their complete requirements when implementing the bounded path below.

## Acceptance criteria

- [x] Extract locked Taffy with the recorded effective feature graph and rounding disabled; translate GPUI refinement, tagged units, known dimensions and per-axis available space explicitly.
- [x] Preserve f32 arithmetic, rem/DPI, stroke minimum, nonnegative measurement ceil, proportional padding and absolute edge rounding; compare every node and callback query with the independent oracle.
- [x] Implement generation-checked engine/node handles, a fixed synchronous measurement trampoline, active pins and panic/error propagation without retaining Go pointers.
- [x] Reject same-engine reentry before native entry; permit independent-engine nested measurement without corrupting either state. Test forced GC, failed builds and reset/recompute invalidation.
- [x] Cover flex/grid/block/float/calc/content-size behavior with deterministic metrics. Keep actual-font and GPUI inline fragmentation acceptance assigned to later slices.
- [x] Attach exact source/artifact/environment identities and executed results for this slice; update capability rows and preserve failures/limitations. Passing an earlier slice does not establish full-core parity.

## Blocking work

- [Load one exact native artifact from a Go-only consumer](02-native-bootstrap.md)

## Answer

Resolved 2026-10-05 by the implementation chat (Roboco `065e52f5`, pi / iroha/dashscope/glm-5.3), implemented by parallel background subagents and independently re-verified by the orchestrator. Complete record: [evidence/ticket06-layout-roundtrip.json](../../../evidence/ticket06-layout-roundtrip.json).

- Native layout service (slot 1) with taffy exactly 0.13.0 (checksum-verified against the CE lock, default features, rounding disabled) and generation-checked engine/node handles, fixed Go trampoline (syscall.NewCallback) with panic containment and latched callback failure; rebuilt DLL 630,784 bytes static CRT, re-measured and re-embedded (capability mask 3). Go adapter (gpui/style.go, layout.go, layout_context.go) ports the pinned taffy.rs semantics exactly: rounding helpers (midpoint toward zero, signed zero, stroke minimum, measured ceil), proportional padding snap, the ordinary outer-bounds device round-trip, parent-relative bounds, measure logical conversion and snapping, rem scoping and the TestWindow scale 2.0. GATES: the port reproduces the ENTIRE recorded reference trace for fx-0001 (54/54 events) and fx-0002 layout-metrics (63/63 events including all measure-query/result pairs with exact logical values and the cross-axis stretch fact) — bit-exact f32, empty diffs. A block-display case was added to the fixture by the orchestrator for block-layout coverage. Float/calc retained as taffy features but not expressible in the GPUI style surface (recorded); actual-font and inline fragmentation deferred to tickets 09/15.

## Comments

- 2026-10-05 — Created under the user's authorization to finish Wayfinder, the specification and local implementation tickets. Routine details are delegated; a discovered scope or architecture contradiction must reopen the affected decision with evidence.

