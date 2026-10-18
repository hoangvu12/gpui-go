# Capture and render frames using the renderer's D3D11 device

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: wave-4 orchestrator chat (Roboco)
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
- 2026-10-06 — DEFERRED (successor chat): the WinRT capture frame pool needs the renderer's actual D3D11 device (same-device adapter), but the renderer ABI currently exposes no device/shared-texture handle and external SurfaceSource is the pinned Unsupported stand-in (ERR_UNSUPPORTED_SOURCE). Needs a DLL addition (device or shared-texture export), blocked by the no-cargo/DLL-rebuild constraint. Alternative to propose: a one-time approved DLL rebuild adding the device export + shared surface source, OR a CPU staging-readback capture path if the ABI were extended. No work started.

## Answer

RESOLVED (2026-10-11, wave 4, batches 8+10). The capture chain is live end-to-end over the rev-11 native artifact:

- **Native (batch 8)**: the Windows Graphics Capture producer in reserved slot 10 (capability bit 11) created ON THE RENDERER'S ACTUAL D3D11 DEVICE (the ticket's first criterion: the IDXGIDevice → CreateDirect3D11DeviceFromDXGIDevice → free-threaded frame pool → session chain), monitor source enumeration, the MTA housekeeping thread, bounded frame-arrival notifications, the foreground checkout/copy under the renderer's device lock into immutable destination textures (pool reuse never mutates published content), the content-size watch with the pool Recreate, stop/destroy lifecycle. The renderer's scene draw path imports the tag-1 surfaces (the pinned draw_surfaces with the view cache and per-scene retention).
- **Go (batch 9)**: the capture model (sources, stream, the bounded poll with superseded-frame release, stats), the Surface element port (ObjectFit crop) and PaintCaptureSurface.
- **Rev-11 fixes**: the scene surface export gate accepts the tag-1 payload, and the 64-bit frame handle packs across the two reserved words — the full loop unblocked.
- **Gates**: internal/capturespec — 11 tests over REAL monitor capture, INCLUDING the full capture→poll→surface→render→readback→present loop with pixel verification (1187 captured-content pixels in the painted half; the ledger 3/3; the pinned unsupported-source bail). The bounded pending frames (drop-oldest, counted), stop/close races, occlusion-agnostic delivery, the typed stale/bad-handle records.

Evidence: [evidence/ticket28-capture.json](../../../evidence/ticket28-capture.json) (executed results, the consent/availability record, the honest deviations).
