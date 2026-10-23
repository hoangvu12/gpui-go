# Cut per-frame allocations

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
Blocked by: 01, 02

## Question

Use ticket 01's benchmark and `go test -memprofile` to find the top allocation sources per frame, then remove the cheap ones. Suspected sources, all unmeasured:

- tracked-entity maps rebuilt every frame (`gpui/frameloop.go:477`)
- the per-frame scene and frame state (`gpui/render.go:90-120`)
- per-glyph pixel buffers
- per-element slices and maps in the fluent builders

The approach is to reuse buffers across frames (clear instead of remake) and pool them where the lifetime is frame-scoped. Behaviour must not change, and the GPUI-parity tests stay green. Don't touch the files ticket 04 owns (`gpui/view.go`, the view-cache code paths in `render.go`) without coordinating through the orchestrator. Report the before/after table.

## Comments

## Answer
