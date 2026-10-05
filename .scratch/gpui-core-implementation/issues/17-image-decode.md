# Decode CPU images and draw their selected frames

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
Blocked by: 08

## Question

A Go app displays decoded image bytes with correct orientation, alpha and animation metadata.

## What to build

Deliver this complete observable path against the [implementation specification](../../gpui-core/spec.md). This ticket is planned work; its status is not evidence that implementation has begun. The selected contracts are [distribution contract](../../../docs/distribution-contract.md), [renderer contract](../../../docs/renderer-contract.md). Preserve their complete requirements when implementing the bounded path below.

## Acceptance criteria

- [ ] Expose pinned image codec services with explicit clipboard-image versus resource-image entry modes and checked owned result buffers.
- [ ] Record the effective codec graph and test actual supported formats, every EXIF orientation, invalid/truncated inputs and size/error behavior.
- [ ] Verify static clipboard WebP versus resource animated WebP and GIF animation metadata in both relevant paths; preserve rational frame delays.
- [ ] Compare deterministic dimensions/frame pixels/metadata against the independent oracle before GPU rendering; do not infer formats from extension lists.
- [ ] Render through the polychrome atlas with correct BGRA/alpha handling and retain pixels/resources through actual completion. Return clear failure for unavailable codecs.
- [ ] Attach exact source/artifact/environment identities and executed results for this slice; update capability rows and preserve failures/limitations. Passing an earlier slice does not establish full-core parity.

## Blocking work

- [Render styled boxes through the scene kernel](08-paint-boxes.md)

## Comments

- 2026-10-05 — Created under the user's authorization to finish Wayfinder, the specification and local implementation tickets. Routine details are delegated; a discovered scope or architecture contradiction must reopen the affected decision with evidence.
- 2026-10-06 — DEFERRED (successor chat): the user's hard constraint forbids cargo/DLL rebuilds without explicit approval, and this ticket's pinned image-codec service requires native work. Proposed pure-Go alternative for user decision: Go stdlib image codecs + pure-Go WebP decode (e.g. x/image/webp) with the clipboard-vs-resource entry modes in Go, animated-WebP recorded as an explicit unavailable-codec row, and oracle fixtures recorded only after a one-time approved harness build. No work started.

