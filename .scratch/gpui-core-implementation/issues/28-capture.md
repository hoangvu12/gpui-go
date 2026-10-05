# Capture and render frames using the renderer's D3D11 device

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
Blocked by: 16

## Question

A window displays captured content through the same-device adapter while producer frames remain valid through GPU copies.

## What to build

Deliver this complete observable path against the [implementation specification](../../gpui-core/spec.md). This ticket is planned work; its status is not evidence that implementation has begun. The selected contracts are [renderer contract](../../../docs/renderer-contract.md), [runtime ownership contract](../../../docs/runtime-ownership-contract.md). Preserve their complete requirements when implementing the bounded path below.

## Acceptance criteria

- [ ] Create the WinRT producer from IDXGIDevice queried from the actual D3D11 device; check HRESULTs and do not depend on an optional DirectComposition device field.
- [ ] Post free-threaded frame-arrival notifications to foreground native checkout/copy; protect the immediate context as specified.
- [ ] Retain the actual capture frame through copy completion, then retain immutable destination storage through later draws; pool reuse never mutates cached content.
- [ ] Exercise ContentSize/crop/resize, bounded pending frames, stop/close races, occlusion and explicit unsupported cross-device/external sources.
- [ ] Record real capture consent/availability and copy/draw completion traces. Missing permission is blocked environment, not passed capture behavior.
- [ ] Attach exact source/artifact/environment identities and executed results for this slice; update capability rows and preserve failures/limitations. Passing an earlier slice does not establish full-core parity.

## Blocking work

- [Render paths, surfaces and nested filters with the full scene plan](16-paths-filters.md)

## Comments

- 2026-10-05 — Created under the user's authorization to finish Wayfinder, the specification and local implementation tickets. Routine details are delegated; a discovered scope or architecture contradiction must reopen the affected decision with evidence.

