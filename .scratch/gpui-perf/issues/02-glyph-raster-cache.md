# Stop re-rasterizing every glyph every frame

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: gpui-perf W1
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
The pin's behaviour is ported in `gpui/glyph_cache.go`. `paintGlyphFromAtlas` in `gpui/draw_text.go` now works the way the pin's `paint_glyph_from_atlas` does: it reads `raster_bounds` from the raster metadata cache, skips empty glyphs, and gets the tile with `get_or_insert_with`. It rasterizes only on an atlas miss.

- **Raster metadata cache:** `TextSystem.raster` holds the bounds, buffer size and format, keyed by the render-glyph key. The key uses the bit patterns of the float size and scale, as the native atlas key does. It's filled by the first rasterization and is unbounded, as in the pin.
- **Atlas tile map (`Atlas.glyphTiles`):** a Go mirror of the native key → tile map for glyph keys. It's filled by `InsertGlyph` and `GlyphTile`. `RemoveGlyph`, a `GlyphTile` miss, `NotifyDeviceLost` and `Dispose` clear it. The native atlas never evicts on its own (ATLAS_ABI.md: the generation changes only through `notify_device_lost`), so a cached tile is always live.
- **Memoized answers:** `PrepareRasterStyle`, `RecommendedRenderingMode` and `FontMetrics` are memoized. The first two are pure functions of the rasterizer's construction state (`glyph.rs` `prepare_style`/`recommended_mode`). `FontMetrics` mirrors the pin's `font_metrics` map. Only successful answers are cached.
- **Pixel buffers:** rasterizations that still run use pooled 16 KB buffers through the new `native.GlyphService.RasterizeGlyphInto`. When the raster fits, it takes 1 native call instead of 2; the capacity protocol had been rasterizing every glyph twice. The atlas insert copies the bytes, so the buffer is reused. `RasterizeGlyph` (nil buffer) is unchanged.

Tests in `gpui/glyph_cache_test.go`:

- A differential test: frames painted through the caches have exactly the sprites of frames painted the old way (`glyphPaintCachesOff`). That covers bounds, masks, colors, tiles and order, for 3 colors, 3 sizes and fractional offsets.
- Raster-into-buffer bytes and metadata equal the plain raster for fitting, small, nil and oversize (200px at 2x) cases.
- The tile cache follows remove, device loss and dispose.

glyphspec, textspec, scenespec, textframespec, richtextspec and native all pass, and no fixture changed.

Before and after, with ticket 01's benchmark (`evidence/perf/ticket02-after.json`, best of 5):

| scene | ms/frame | allocs/frame | B/frame | native calls/frame |
|---|---|---|---|---|
| FrameText | 200.1 → 82.6 | 81,583 → 17,412 | 18.7 MB → 3.5 MB | 129,292 → 23,530 |
| FrameRefreshText | 200.8 → 84.5 | 81,574 → 17,405 | 18.7 MB → 3.5 MB | 129,292 → 23,530 |
| FrameDivs / RefreshDivs | unchanged (no text) | | | |

What remains per glyph is the sprite insert: 18,252 of the 23,530 calls, which belongs to ticket 05's batched insert. A profile shows that 55% of the remaining text frame is re-shaping. The page's 300 lines exceed the 256 native shaping-handle bound, so parked lines re-shape at every paint. Ticket 06 takes that up.
