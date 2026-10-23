# Stop re-rasterizing every glyph every frame

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
Blocked by: 01

## Question

`gpui/draw_text.go` `paintGlyphFromAtlas` (around :688; deviation note at :34-41) rasterizes each glyph on every frame: about 6 native calls per glyph, plus a fresh pixel buffer (`internal/native/glyph.go:430-470`). The atlas deduplicates afterwards.

Port the pinned GPUI behaviour:

- Cache raster bounds and metadata by the render-glyph key, as the pin's raster_metadata cache does.
- Look up the atlas tile before rasterizing, so a cached glyph costs only a sprite insert.
- Reuse pixel buffers.

The glyphspec/textspec bit-exact fixtures must stay unchanged. Report before and after with ticket 01's benchmark.

## Comments

## Answer
