# Present text drawn by the element tree through a consumer-owned present loop

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
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

- [ ] The README's `OnRedraw` present loop presents a window whose root
      view contains `Text` elements; the glyphs are visible on screen
      and in a staging readback (`RenderScene` + `ReadPixels`).
- [ ] Presenting a text scene with a nil or foreign atlas either
      documents the error or draws nothing (no native bad-handle
      failure), with a typed, actionable error.
- [ ] Non-text scenes keep presenting exactly as today (hello example
      unchanged in behavior).
