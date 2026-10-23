# Free each frame's native scene once it is replaced

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: gpui-perf W1
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
Every native scene is now freed once nothing can read it:

- **Replaced scene:** `completeFrame` (`gpui/render.go`) disposes the previous `ds.lastScene` when it publishes the new one. That runs after `drawRoots`, so this frame's cached views have already replayed from it (`view_cache.go` `reusePaint`, during paint).
- **Abandoned frame:** `DrawWindowFrame`'s deferred abandon branch disposes that frame's scene. It covers both a returned error and a panicking build, and the published scene is kept.
- **Window teardown:** `dropDrawState` (`gpui/element.go`) disposes the window's last scene on close.
- **Live count:** `gpui.LiveSceneCount()` counts live scenes Go-side (`NewScene` +1, the first `Dispose` −1), because the native kernel has no live-scene count or cap.

Readers I checked:

- `PresentWindow` and `WindowPrimitiveCounts` read the current `lastScene`.
- The `OnRedraw` presenters (examples, portfixture) and device-loss recovery (`deviceloss.go:639`) draw, then present that same frame's scene synchronously.
- `Surface.DrawScene`/`RenderScene` and headless `RenderToImage` take the scene from their caller.
- The view cache replays only from the current `lastScene`.

The lifetime is documented on `DrawWindowFrame` and `LastDrawnScene`. The scene stays valid until the window's next completed frame or its close; after that every read reports `ErrSceneClosed`, never a crash.

Test: `gpui/scene_leak_test.go` `TestLiveScenesStayFlatAcrossFrames`.

- Cached-view window (ticket 04's partial-update scene): 60 changed frames plus 20 refreshes leave exactly 1 live scene. A replaced scene reads `ErrSceneClosed`, and the next cached frame still paints all 1,000 quads.
- Abandoned (panicking) frame: the live count is unchanged and the published scene stays readable.
- Recovery, then close: closing the window frees its scene.

`TestPerfViewCacheScenesMatch` (cached = plain) still passes. The full `CGO_ENABLED=0 go test -p 1 ./... -count=1` run passed 51 packages with 0 failures, including headlessspec now that ce-source is in place.
