# Present text drawn by the element tree through a consumer-owned present loop

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: pi worker
Blocked by: none

## Question

A consumer app that opens a window with a root view containing `Text`
elements and presents frames through its own present loop (`OnRedraw`
→ `LastDrawnScene` → `Surface.DrawScene`, the README's documented
pattern) cannot show text. The scene's glyph sprites reference the
private atlas the window's text system rasterized them into: `DrawScene`
/ `RenderScene` with a nil atlas fail with a native bad-handle error
when the scene holds sprites, and a fresh public `NewAtlas()` fails the
same way because the sprite tiles belong to the private atlas. How does
the public API let a consumer present element-tree text?

Verified from a consumer program (mordbard's readiness spike, 2026-10-19):
a 7-`Text` window shaped 65 subpixel sprites into its scene;
`RenderScene(scene, nil)` → "renderer status -2 (bad handle):
renderer_surface_render_scene"; `DrawScene(scene, nil)` → the same on
`renderer_surface_draw_scene`. Hand-built scenes with a public atlas
(`Scene.PaintShapedText` + `NewAtlas()`) present text fine, but bypass
the element tree.

## What to build

Expose the atlas that holds a window's glyphs to the present path, or
present through it. Candidate shapes (pick one or a pair):

- `Window.GlyphAtlas() *Atlas` returning the window text system's atlas
  (the consumer then passes it to `Surface.DrawScene` / `RenderScene`);
- or a public present helper (e.g. an overload of `PresentWindow` /
  `DrawScene` for windows) that uses the internal atlas;
- or a public `Window.PaintShapedText`-style paint API that rasterizes
  into a consumer-supplied `Atlas` so the element tree and the present
  path agree on one atlas.

Update the hello example to include text, and the README's statement
that the public present loop cannot show text.

## Acceptance criteria

- [x] The README's `OnRedraw` present loop presents a window whose root
      view contains `Text` elements; the glyphs are visible on screen
      and in a staging readback (`RenderScene` + `ReadPixels`).
- [x] Presenting a text scene with a nil or foreign atlas either
      documents the error or draws nothing (no native bad-handle
      failure), with a typed, actionable error.
- [x] Non-text scenes keep presenting exactly as today (hello example
      unchanged in behavior).

## Answer

Resolved on branch `consumer-gaps/01-present-element-tree-text` by
exposing the window's glyph atlas on the present path and turning the
missing/foreign-atlas failure into a typed, actionable error. The
change is confined to the atlas/present path (no text-element or
shaping changes — ticket 02 owns those):

- **`Window.GlyphAtlas() (*Atlas, error)`** (gpui/render.go) returns the
  per-window atlas the window's real text system rasterized its glyph
  sprites into — the same `realWindowTextSystem.atlas` the frame draw,
  `Window.ShapeText` and the device-loss recovery already use. It is
  created lazily like the other text-system accessors (so a present
  loop may ask before the first frame), is stable for the window's
  lifetime, and answers the typed `ErrNoGlyphAtlas` on deterministic
  test windows (which shape no sprites). Foreground-thread use, like
  every window accessor.
- **`ErrSceneSpriteAtlas`** (gpui/renderer.go) is the typed refusal for
  presenting a sprite scene without its atlas. `Surface.DrawScene`,
  `Surface.RenderScene` and `Surface.RenderToImage` check it BEFORE the
  native draw: a nil atlas with sprite batches is refused outright, and
  a non-nil atlas that cannot resolve the scene's sprite batches (a
  fresh `NewAtlas()`, another window's atlas) is refused by
  `sceneAtlasServesSprites`, which verifies each sprite batch's pool
  and texture index (the compiled plan's `SpriteSceneCommand` records)
  against the atlas's live texture counts — the native draw resolves
  exactly those views, so the refusal can never reject a drawable
  frame. A native bad-handle from a disposed atlas is wrapped into the
  same typed error with the native record kept in the chain. Sprite-free
  scenes are untouched: a nil atlas keeps presenting exactly as before
  (hello's pre-text shape).
- The hello example now shows a `Text` label and passes
  `shown.GlyphAtlas()` in its `OnRedraw` loop; the README's present-loop
  paragraph documents the atlas and the typed error instead of claiming
  text cannot be presented, and its snippet matches the example.
- New real-window gate: `internal/presentspec`
  (TestConsumerPresentLoopShowsElementTreeText) opens a window whose
  root view contains a `Text` element, presents it through the exact
  README loop (OnRedraw → LastDrawnScene → GetOrCreateSurface →
  DrawScene with GlyphAtlas), verifies the glyphs in a staging readback
  (RenderScene + ReadPixels; 233 bright glyph pixels on the recorded
  run) and on screen (the presented submission retires through the real
  GPU ledger), pins the typed nil/foreign refusals (no submission
  accepted across them), and proves a sprite-free scene still presents
  with a nil atlas (pixel-verified). The test-window accessor refusal
  is pinned by TestGlyphAtlasTestWindowTypedRefusal.

Consumer shape (also in the README): pass `w.GlyphAtlas()` with every
`LastDrawnScene(w)` present; a same-shaped foreign atlas is not
detectable (the guard is a precondition, not proof of ownership — the
owning atlas is the only guaranteed-correct one), and a nil atlas only
presents sprite-free scenes.

Native code: none rebuilt — the fix is entirely on the Go side of the
existing ABI (the atlas, renderer and scene services already expose
everything needed).

Verification: build, vet, gofmt clean; the new gate plus the affected
suites green (presentspec, gpui, glyphspec, pathspec, rendererspec,
devicelossspec, headlessspec, shutdownspec, authorspec, capturespec);
the full `go test ./...` runs with three known non-regressions —
`internal/desktopspec`'s cursor-hide test fails identically at the
base commit on this machine (the documented corrupted post-fullscreen
cursor/shell state), and the parallel-suite failures of
capturespec/a11yspec/gpui-cursor are monitor-content/focus contention
flakes reproduced with untouched package pairs (e.g. capturespec +
pathspec fails the same blue-pixel assertion); every affected package
passes standalone.
