# img() renders SVG through the native SVG service

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: base-port-2 orchestrator (2026-10-23)
Blocked by: none

## Question

`img()` and the image cache render SVG bytes and SVG assets the way the pin's img.rs does: un-sniffed bytes go to the SVG renderer. Today the cache still reports "SVG rendering is a deferred service", even though core ticket 19 shipped the native SVG service (slot 8, rev 9+) and the Go SvgRenderer model.

## Scope

- gpui/imagecache.go:68, gpui/image.go:38,247, and the img element: route ErrImageFormatUnknown bytes and ImageFormatSVG through the existing SvgRenderer, as the pinned img.rs does (reference/ce-source/crates/gpui/src/elements/img.rs and assets.rs).
- Found in the showcase log: "Failed to load asset: svg error: gpui: SVG rendering is a deferred service".

## Acceptance criteria

- [ ] Every change cites the pinned sources; no invented APIs; no native change (rev 11).
- [ ] gpui, assetspec, imagespec, svgspec and `go test ./base` stay green; a test shows an SVG through img() producing pixels or a sprite.
- [ ] Evidence in `evidence/base/ticket38-*.json`.

## Comments

- 2026-10-23 — Created and claimed by the base-port-2 orchestrator from ticket 32's showcase run.
- 2026-10-23 — Worker finished. The orchestrator updated the assetspec test that pinned the old error and verified on a snapshot.

## Answer

`img()` and the image cache render SVG through ticket 19's SvgRenderer, following the pinned img.rs:
- asset bytes the codec can't sniff, and in-memory `ImageFormatSVG` images, go to `RenderSingleFrame`
- `LoadedImage` carries the 2x smooth scale factor, so layout uses pixels divided by the scale
- failures map to the pinned error variants

The assetspec test that pinned the old "deferred service" error is replaced by a render test and a non-SVG error test.

Orchestrator re-run on a snapshot: gpui, base, assetspec, imagespec and svgspec are ok. Evidence: [ticket38-img-svg-fallback.json](../../../evidence/base/ticket38-img-svg-fallback.json).
