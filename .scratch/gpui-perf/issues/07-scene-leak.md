# Free each frame's native scene once it is replaced

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
Blocked by: 04, 05

## Question

`DrawWindowFrame` (`gpui/render.go:90`) creates a native scene every frame. `completeFrame` (`render.go:209`) overwrites `ds.lastScene` without disposing the old one. Abandoned frames never dispose theirs, and nothing in gpui or internal registers a finalizer. The native kernel has no live-scene cap, so native memory grows with every drawn frame. W3 found this during ticket 05; it predates this effort.

Dispose scenes at the right time:

- a replaced `lastScene`, but only after this frame's `Scene.Replay` from it (ticket 04's `view_cache.go:190`) is done
- an abandoned frame's scene
- the window's last scene on close or teardown

Check every reader of the last scene: `render.go:376-408`, the `OnRedraw`/`DrawScene` presenters, capture, headless render-to-image, the inspector and tests. None may use a disposed scene, and any API that hands the scene out must document its lifetime.

Add a test showing the live native scene count (or native memory) stays flat across many frames, including cached-view frames and an abandoned frame.

## Comments

- 2026-10-23: created by the orchestrator after merging W1-W3 into base-port-2 (beded3c).

## Answer
