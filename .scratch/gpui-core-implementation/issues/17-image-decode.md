# Decode CPU images and draw their selected frames

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: orchestrator (the approved single-rebuild slice, 2026-10-10; evidence: ../../evidence/ticket17-image-decode.json)
Blocked by: 08

## Question

A Go app displays decoded image bytes with correct orientation, alpha and animation metadata.

## What to build

Deliver this complete observable path against the [implementation specification](../../gpui-core/spec.md). This ticket is planned work; its status is not evidence that implementation has begun. The selected contracts are [distribution contract](../../../docs/distribution-contract.md), [renderer contract](../../../docs/renderer-contract.md). Preserve their complete requirements when implementing the bounded path below.

## Acceptance criteria

- [x] Expose pinned image codec services with explicit clipboard-image versus resource-image entry modes and checked owned result buffers.
- [x] Record the effective codec graph and test actual supported formats, every EXIF orientation, invalid/truncated inputs and size/error behavior.
- [x] Verify static clipboard WebP versus resource animated WebP and GIF animation metadata in both relevant paths; preserve rational frame delays.
- [x] Compare deterministic dimensions/frame pixels/metadata against the independent oracle before GPU rendering; do not infer formats from extension lists.
- [x] Render through the polychrome atlas with correct BGRA/alpha handling and retain pixels/resources through actual completion. Return clear failure for unavailable codecs.
- [x] Attach exact source/artifact/environment identities and executed results for this slice; update capability rows and preserve failures/limitations. Passing an earlier slice does not establish full-core parity.

## Blocking work

- [Render styled boxes through the scene kernel](08-paint-boxes.md)

## Comments

- 2026-10-05 — Created under the user's authorization to finish Wayfinder, the specification and local implementation tickets. Routine details are delegated; a discovered scope or architecture contradiction must reopen the affected decision with evidence.
- 2026-10-06 — DEFERRED (successor chat): the user's hard constraint forbids cargo/DLL rebuilds without explicit approval, and this ticket's pinned image-codec service requires native work. Proposed pure-Go alternative for user decision: Go stdlib image codecs + pure-Go WebP decode (e.g. x/image/webp) with the clipboard-vs-resource entry modes in Go, animated-WebP recorded as an explicit unavailable-codec row, and oracle fixtures recorded only after a one-time approved harness build. No work started.


## Answer

Resolved 2026-10-10 under the user's option-A approval (one bounded DLL rebuild for this ticket's codecs). The native image codec service landed in reserved slot 7 (capability bit 8, native revision 8, artifact 14,453,248 bytes / sha256 29781ff7…, static CRT preserved): the pinned image-crate graph with both entry modes (resource sniffing with the SVG-fallback signal preserved; clipboard known-format), EXIF orientation, the GIF/animated-WebP frame walks with rational delays, the BGRA swap, the codec graph, and the capacity-protocol frame dumps — plus the pinned AtlasKey::Image extension of the atlas service (polychrome pool), built in the same approval window. The Go client (internal/native/image.go, the typed error surface) and the gpui model (Image with the content-hash id, RenderImage frames, the ImageCodec seam, InsertImageFrame/RemoveImageFrame) gate through 20 tests: the native suite (codec graph, PNG/WebP/GIF/EXIF-180 oracles, error surface, clipboard modes, handle lifecycle) and imagespec (both entry modes, every EXIF orientation 1..8 against the stdlib-transform oracle verified with Pillow ground truth, the SVG signal, the polychrome atlas identity with cache-hit retention and removal). The fx-0008 reference-harness recording is the recorded follow-up gate (bounded harness-crate addition); the img element's scene painting composes through the ticket10/16 sprite machinery (the image cache is ticket18). Dependents unblocked: 18 (asset loading), 23 (clipboard), 24 (drop/touchpad). Executed evidence: [evidence/ticket17-image-decode.json](../../../evidence/ticket17-image-decode.json).
