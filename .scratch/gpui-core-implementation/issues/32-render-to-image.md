# Render scenes to owned images and account for headless APIs

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
Blocked by: 16, 29

## Question

Can a caller capture a scene as an owned CPU image through the reference-supported test/headless interfaces, with explicit profile and failure behavior?

## What to build

Deliver the pinned Windows render-to-image path from a Go scene to immutable owned pixels, under the [renderer](../../../docs/renderer-contract.md) and [conformance](../../../docs/conformance-contract.md) contracts. This is a separate observable rendering capability, not proof of a second GPU backend. Follow the [specification](../../gpui-core/spec.md).

## Acceptance criteria

- [ ] Trace PlatformHeadlessRenderer, platform creation/availability and PlatformWindow render_to_image independently for native Windows, test-support and alternate backend profiles. Record source-supported errors separately from an implemented offscreen path; applicable missing work blocks closure.
- [ ] Implement the pinned native Windows test-support render-to-image path with source-equivalent dimensions, background appearance, alpha/channel representation and current device-loss error behavior.
- [ ] Read back only completed GPU work and return owned immutable CPU bytes. Validate staging row pitch/format, delayed completion, cancellation, partial failures and device loss without releasing in-flight resources.
- [ ] Compare a deterministic scene's full CPU image against the independent reference, retaining raw pixels and exact format metadata. Integrate the path into capture tooling without silently changing the reference/port capture stage.
- [ ] Test repeated readback, two windows/devices where supported, opaque/blurred/transparent backgrounds and teardown with a pending readback; document any source-supported unavailable profile.
- [ ] Attach source/artifact/environment identities and fixture results, update the capability ledger and require a bounded follow-up for any applicable headless API beyond this selected adapter.

## Blocking work

- [Render paths, surfaces and nested filters with the full scene plan](16-paths-filters.md)
- [Recover device loss and rebuild retained GPU resources](29-device-loss.md)

## Comments

- 2026-10-05 — Added after GLM-5.3's final read-only audit identified the headless/render-to-image family. Main confirmed PlatformHeadlessRenderer and the Windows test-support render_to_image path in pinned snapshots. No implementation or readback has executed.
