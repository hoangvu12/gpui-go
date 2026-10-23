# Wire text selection into Root and ElementExt; paint with prepaint bounds

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: base-port-2 orchestrator (2026-10-23)
Blocked by: 29

## Question

Root mounts the text selection layer and copies the selection, `ElementExt` offers `TextSelectionScope`, and elements paint with the bounds they were prepainted at (as the pin does). Ticket 29 found all three.

## Scope

- base/root.go:
  - mount `TextSelectionLayer{}` as the root's first child (U:root.rs:293)
  - the ctrl-c binding and `on_action_copy` through `TextSelectionSelectedText` (U:root.rs:17-21, 264-273)
- base/element_ext.go: `TextSelectionScope(scope)` (U:element_ext.rs:7-13)
- gpui/element.go:612: `drawable.paint` recomputes `layoutBoundsOf` at paint time. The pin stores the bounds from prepaint and paints with them (R:element.rs:537-545, 557-600). The fix stores the bounds at prepaint, then lets base/text_inline_object.go paint with the correct bounds and retires 28's `paintOrigin` workaround if possible.

## Acceptance criteria

- [ ] Every change cites the pinned sources; no invented APIs.
- [ ] The core fix keeps the gpui and core regression packages green (gpui, the spec packages that paint).
- [ ] `CGO_ENABLED=0 go test ./base` is green, vet and gofmt are clean, and evidence is in `evidence/base/ticket36-*.json`.

## Comments

- 2026-10-23 — Created by the base-port-2 orchestrator from ticket 29's report, then claimed and delegated.
- 2026-10-23 — Worker finished. The orchestrator verified it on a snapshot.

## Answer

All three parts are done:
- Root mounts `TextSelectionLayer` as its first child and copies the selection on ctrl-c (cmd-c on macOS), propagating when the selection is blank.
- `ElementExt.TextSelectionScope` is added.
- Core fix: `drawable` stores its bounds at prepaint, and paint passes them to `Element.Paint` instead of re-querying the layout (R:element.rs:537-600). Elements prepainted at an offset now paint at that offset. Inline objects drop 28/29's `hitbox.Bounds` workaround.

28's `paintOrigin` is upstream behaviour, so it stays.

Orchestrator re-run on a snapshot of HEAD plus 36's files:
- vet, gofmt and build are clean
- `go test ./base` has 1017 pass and 3 skip, and `-count=2` is ok
- gpui and 9 spec packages that paint are ok

Follow-up: some manual offset re-application around paint is now redundant but harmless; the evidence lists the sites. Evidence: [ticket36-selection-wiring-paint-bounds.json](../../../evidence/base/ticket36-selection-wiring-paint-bounds.json).
