# zgui vs GPUI Fast: why zgui wins, and what it means for gpui-go

Researched 2026-10-23 for the user's chart (`Roboco uploads/9665e9a9-image.png`). Details and citations: [zgui.md](zgui.md), [gpui-fast.md](gpui-fast.md). Recommendations below are proposals until the user accepts them.

## The projects

- **zgui** is `github.com/zeronsh/zgui`: a retained-mode Rust UI framework, not a GPUI fork. It is from the same org as the local `zui` (a Zed/GPUI fork), which is a different project. Its comparison page is https://zgui-performance.pages.dev/compare/.
- **GPUI Fast** is `github.com/longbridge/gpui-fast` (crates.io `gpui-fast`, by huacnlee of Longbridge / GPUI Kit). It is a fork of Zed's GPUI with:
  - reusable views across frames
  - cached layout and text measurement
  - tiled scroll caching
  - Windows D3D11 state dedup

## The measurements

- Everything was measured on macOS/Metal on an Apple M5 Max: 3 runs × 15 s over three scenes (lab, heavy and animate). None of it is Windows.
- These numbers match the checked-in data:
  - idle CPU (11×)
  - CPU under load (−16 to −28%)
  - animation GPU (−63%)
  - memory (−8%)
- Startup +10% was measured against plain GPUI, not GPUI Fast.
- These numbers match no checked-in run, so the chart is probably from a newer run:
  - GPU busy/heavy (30%/12%)
  - smoothness (16–19%)
  - app size (2.7×)
- The audited binaries are 11.06 MB for zgui and 7.48 MB for GPUI Fast.

## Why each difference happens

| Chart row | zgui technique | GPUI Fast behaviour |
|---|---|---|
| Idle CPU 11× | Event loop sleeps; vsync timer stops when nothing changed | Unchanged GPUI frame clock: a callback on every vsync while visible (0.35% vs upstream 0.33%) |
| GPU −12 to −63% | Keeps the last frame, redraws only damaged regions, presents without a full copy on macOS | Clears and redraws the full frame into a fresh drawable every frame |
| CPU −16 to −28% | Fine-grained bindings update only affected nodes; paint skips clean subtrees; Metal encoded directly (no wgpu) | View reuse can't help when the benchmark's single root view changes every tick |
| Smoothness | Frames paced from the vsync start, double-buffered | Triple-buffered, deeper queue |
| Memory −8% | Small, hard-capped glyph and texture caches | Grow-only atlases |
| Startup +10% | Compiles shaders at runtime; used to scan every installed font | Prebuilt shaders; CoreText |
| Size 2.7× | Ships wgpu plus its shader compiler, its own text and image stacks, and a software renderer | Smaller native stack |

## Mapping to gpui-go (measured from source on 2026-10-23)

| Area | gpui-go today | Gap vs zgui |
|---|---|---|
| Idle | Already invalidation-driven: the vblank waker exits after one idle vblank, and a clean `WM_PAINT` draws nothing (`gpui/frameloop_windows.go:79-141`, `gpui/frameloop.go:330-354`) | None. gpui-go already has the main idle win that GPUI Fast lacks. Inactive-window throttling isn't ported. |
| Scene rebuild | Full layout/prepaint/paint and a new scene every frame; `Scene.Replay` exists but is unused (`gpui/render.go:90-104`, `gpui/scene.go:398`) | Biggest CPU gap. View caching (upstream GPUI's `cached()` / GPUI Fast's reuse) would cut it. |
| GPU / present | Full redraw; `MAP_WRITE_DISCARD` of all instance buffers; DComp flip-sequential, 3 buffers, `Present(0,0)`; no `Present1` dirty rects; no waitable swapchain (`reference/native/src/renderer.rs:1070-1085`, `draw.rs:2436`) | Damage rects + `Present1` and a waitable / max-latency-1 swapchain are native changes (DLL rev 12) |
| FFI per primitive | One `SyscallN` per quad/sprite insert and per layout node (`internal/native/scene_windows.go:47-110`, `layout_windows.go:48-143`); the draw itself is a single call | Batch inserts into one Go-side buffer per frame. Go-side buffering is free; the bulk entry point needs rev 12. |
| Glyphs | Every glyph re-rasterized every frame, about 6 native calls each (`gpui/draw_text.go:688`) | Highest-value Go-side fix: honour the raster_metadata cache and skip rasterize when the atlas already holds the key |
| Shaping cap | Cap is 256 (not 128); since ticket 40 it evicts LRU lines instead of failing (`gpui/text.go:383-396`) | Only a re-shape cost on pages with more than 256 lines |
| Allocations | "~60k allocs/frame at 1,000 elements" has no benchmark or evidence file behind it | Measure first (`testing.B` with `ReportAllocs`) |
| Startup | `native.Load` isn't memoized: about 10 services each re-parse the manifest and SHA-256 the 18.9 MB DLL (`internal/native/native.go:228`) | Easy Go-side fix: load once with `sync.Once` |
| Size | 18.9 MB embedded DLL + 1.1 MB manifest; system fonts; prebuilt DXBC | Already smaller in kind than zgui's approach (no runtime shader compiler) |

## Proposed order (for the user to accept or reject)

1. Go-only, no DLL change:
   - glyph raster caching
   - memoized `native.Load`
   - an allocation/frame benchmark
2. Go-only, larger: view caching / scene replay for unchanged views.
3. Native rev 12, bundled with the weight-500 fake-bold fix:
   - a bulk scene-insert entry point
   - a waitable swapchain with max latency 1
   - optional `Present1` dirty rects
