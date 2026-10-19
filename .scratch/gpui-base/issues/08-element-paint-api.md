# Let external elements paint, defer, anchor, animate and show images

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: claimed
Assignee: base-port orchestrator (wave 1c)
Blocked by: 05

## Question

A package outside `gpui` can write custom elements that paint quads/paths/underlines/images/layers, and can use `Canvas`, `Deferred`, `Anchored`, `Img` and `with_animation`.

## Scope

- The frame scene is reachable only through unexported `currentFrame(w)`; only `PaintSvg` is exported (`gpui/svgelement.go:383`).
- Port: `Window.PaintQuad/PaintPath/PaintUnderline/PaintStrikethrough/PaintImage/PaintLayer` (R:window.rs:4339,4634,4732,4771,5057); `Canvas` (R:elements/canvas.rs); `Deferred` + `DeferDraw(priority)` over the existing order floor (R:elements/deferred.rs; R:window.rs:4310); `Anchored` + viewport/layout-bounds exports (R:elements/anchored.rs; R:window.rs:2779,5303); `img()` + `ImageSource` + MIME formats over the ticket-18 cache (R:elements/img.rs; R:platform.rs:3508); `AnimationElement`/`with_animation` (R:elements/animation.rs).

- From ticket 34 (render.go ownership): drag-preview prepaint/paint in `drawRoots` via ticket 06's `prepaintActiveDragPreview`/`paintActiveDragPreview` (window.rs:3451-3456,3474-3475) and `w.resetCursorStyle(app)` at frame completion (window.rs:3277).

## Acceptance criteria

- [ ] Scene transcripts for each element; deferred priority ordering; anchored flip/snap; image element over embedded and in-memory sources.
- [ ] Every ported behavior cites the pinned Base source (`upstream-base/ce/crates/base/src/...`, c0820693) or the core pin (`reference/ce-source/crates/gpui/src/...`, 254b5dbd); no invented APIs.
- [ ] Upstream tests in scope are ported (count ported / deviated / not-applicable recorded with reasons); real-window tests FAIL rather than skip.
- [ ] No native artifact change (rev 11 stays frozen); any genuine native need is stopped and surfaced to the user with evidence.
- [ ] `CGO_ENABLED=0 go test` for the touched packages is green, `go vet` + gofmt clean, and the core regression packages stay green; executed evidence in `evidence/base/ticketNN-*.json`.

## Research

- [slice-01-infra-overlays.md §C3-C6](../research/slice-01-infra-overlays.md)
- [slice-06-text-selection.md §2.1,2.5](../research/slice-06-text-selection.md)

## Comments

- 2026-10-19 — Created by the GPUI Base port chat from the merged slice inventories (R: = `reference/ce-source/crates/gpui/src`).
