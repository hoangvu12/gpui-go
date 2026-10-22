# Wire text selection into Root and ElementExt; paint with prepaint bounds

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: claimed
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
