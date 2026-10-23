# gpui-fast: what it is, why it beats GPUI, why zgui still beats it

Researched 2026-10-23. Sources were read from shallow clones of
`longbridge/gpui-fast` at `91f60ec0bc32358acecfe2abad660f9db8d7a518` (HEAD, 2026-10-22)
and `zeronsh/zgui` at `8b290e50ad711bb50b67d62ae4b489ed26d899de`. Nothing was built.

Link shorthand:
- `GF` = https://github.com/longbridge/gpui-fast/blob/91f60ec0bc32358acecfe2abad660f9db8d7a518
- `UP` = gpui-fast's commit `1089f40b8b5d66ca92d4d9b261c0803dd2e23202`. Per its `UPSTREAM` file, this commit holds
  zed-industries/zed `b4ecc454225fc1819e37e890b62df5473b2ba95b` unchanged, so `UP` line numbers are upstream GPUI's:
  https://github.com/longbridge/gpui-fast/blob/1089f40b8b5d66ca92d4d9b261c0803dd2e23202
  (the same paths exist under https://github.com/zed-industries/zed/blob/b4ecc454225fc1819e37e890b62df5473b2ba95b).
- `ZG` = https://github.com/zeronsh/zgui/blob/8b290e50ad711bb50b67d62ae4b489ed26d899de

## 1. Identity (confidence: very high)

**Verified**
- Repo: https://github.com/longbridge/gpui-fast. Description: "GPUI extracted from the Zed monorepo as a standalone workspace, with the layout engine reworked to keep what did not change between frames." It was created 2026-09-24, had about 270 stars, Apache-2.0 license, "experimental".
- Crates: `gpui-fast`, `gpui-fast-macros` and `gpui-fast-platform` on crates.io. Versions run from 0.1.0 (2026-10-06) to 0.1.10 (2026-10-19). The crates.io owner is `huacnlee` (Jason Lee, Longbridge; also the author of gpui-component / GPUI Kit). You use it by aliasing the crates: `gpui = { package = "gpui-fast", ... }`.
- Upstream base: Zed's GPUI, synced periodically. The current base is zed `b4ecc45422` (`GF/UPSTREAM`). The README benchmarks against `gpui-pre` 0.3.7. All fork logic lives in `crates/gpui/src/fast/` and `crates/gpui_windows/src/fast/`, with small hooks into upstream files that are enforced by `script/check-upstream` (`GF/docs/architecture.md` §"Staying close to upstream").
- It is **not** gpui-ce. It is a fork of zed's GPUI.

**The chart's source (confidence: high for the CPU/GPU numbers; low for startup and size)**
- zgui is **https://github.com/zeronsh/zgui** ("retained Rust GUI framework ... fine-grained updates, bounded rendering resources, low idle CPU"; wgpu renderer, winit host). Note: `zortax/zgui` and `zig-gamedev/zgui` are unrelated.
- zgui's repo has a gpui-fast adapter, `ZG/comparisons/gpui-fast/Cargo.toml`, pinned to `gpui-fast = "=0.1.4"` with `gpui-fast-platform` feature `font-kit`. Its sources are `src/main.rs` (the "lab" scene), `src/bin/heavy.rs` and `src/bin/animate.rs`.
- Published page: https://zgui-performance.pages.dev/compare/ ("Damage-based rendering, measured ... measured against GPUI and GPUI Fast"). Built by `ZG/scripts/build_compare_site.py` from `ZG/site/compare/data.json`. The runs are `docs/results/latest-macos-2026-10-12-5493829`, `latest-macos-heavy-2026-10-11-5493829` and `latest-macos-animate-2026-10-11-5493829`, all at zgui commit `5493829`.
- **The platform is macOS (Mac17,6, Apple M5 Max, macOS 26.4, Metal), not Windows.**

**Checking the chart's numbers against `site/compare/data.json` (medians)**

| Chart claim | Matching data | Status |
| --- | --- | --- |
| Idle CPU 11x less | lab idle: zgui 0.0316% vs gpui-fast 0.3473% of one core = 11.0x. Heavy idle is 8.2x. | Reproduced |
| CPU under load 16-28% less | animate 3.75 vs 4.44 (-15.6%); heavy both 8.45 vs 11.73 (-28.0%); lab both 3.09 vs 4.18 (-26%) | Reproduced |
| GPU 12-63% less | Metal-trace GPU busy, animate: 15.6 vs 17.8 ms/s (-12.3%). IOKit megacycles, animate: 7.62 vs 20.82 (-63.4%). | Reproduced |
| Smoothness 16-19% | p99 frame interval: lab both 23.0 vs 29.7 ms; heavy both 23.4 vs 29.6 ms. Late frames (>25 ms): zgui 0%, gpui-fast 3.7-16.6%. | Same direction; exact 16-19% not reproduced from this snapshot |
| Startup +10% (zgui worse) | zgui added `scripts/startup_probe.swift` on 2026-10-23 (commit `55ea732c3`). It measures instructions retired to first content (`ZG/docs/benchmarking.md` §"Startup on macOS"). No published startup numbers in the repo. | Source not found; probably newer than the repo data |
| App size 2.7x bigger | No size data found in the repo or on the site | Source not found |

**Methodology (verified, from the compare page and `ZG/docs/benchmarking.md`)**
- Every framework runs the same UI from a shared `comparisons/workload` crate. There are three scenes:
  - lab: a streaming transcript plus a 100k-row virtual list, 960×720.
  - heavy: a 1280×800 dashboard where most values change every tick.
  - animate: draws at display rate.
- Updates are driven by a 60 Hz timer. A run counts only if it delivers 58-61 updates/s.
- Each trial is 20 s: 5 s warmup, then 15 s sampled every 50 ms. Three repeats per framework and mode, in rotated order.
- Frame pacing comes from eight 8 s window captures. GPU numbers come from Metal System Trace (5 s), plus IOKit per-app GPU time × clock.
- Memory is physical footprint.
- The authors flag that the machine was not isolated: a personal Mac with a live wallpaper and the GPU about 45% busy. They say GPU readings vary 2-3x between runs.

## 2. gpui-fast's techniques (verified from source and docs unless marked)

The core idea: GPUI is immediate-mode. Every frame it re-renders views, rebuilds the Taffy tree, reshapes text and repaints the scene, except for `AnyView::cached` subtrees. gpui-fast generalizes the cached-view mechanism to every view, keyed by dependency tracking. See `GF/docs/architecture.md` (§"The problem", §"GPUI's frame") and `GF/docs/retained-mode.md`.

- **Retained views (`fast/retained.rs`, `fast/dependencies.rs`)**
  - Each view records the entities, globals, `ScrollHandle`/`ListState` versions, pointer, modifiers and hovers it read.
  - If none of them changed and the view's bounds, mask, text style and opacity are the same, its prepaint and paint ranges (hitboxes, dispatch nodes, listeners, primitives) are copied from the last frame.
  - There are five paths: Reused, Spliced, Built-at-retained-layout, Prebuilt, Built (architecture.md §"Drawing a view from the last frame").
- **Splicing (`fast/splice.rs`)**: when only a nested view changed, the parent's output is copied in stretches and only the nested view is rebuilt.
- **Retained layout (`fast/layout.rs`, `fast/layout_key.rs`)**
  - Upstream clears the whole Taffy tree every frame. gpui-fast keeps the nodes.
  - It writes style, children and context only when a fingerprint differs, so Taffy's layout cache survives.
- **Text measurement carry and replay (`fast/text.rs`)**
  - The same text, runs and style reuse the old measurement.
  - Changed text is re-measured under the constraints Taffy logged. If the sizes match, the node stays clean.
- **Scene ordering**
  - Upstream uses an R-tree `BoundsTree` (`UP/crates/gpui/src/scene.rs:43`). gpui-fast replaces it with a uniform grid (`fast/bounds_tree.rs`).
  - Orderings for reused subtrees are replayed rather than recomputed.
- **Other retained work**:
  - dispatch nodes are copied in bulk (`fast/dispatch.rs`);
  - path tessellation is cached (`fast/path_cache.rs`);
  - per-run rather than per-glyph glyph setup (`fast/glyphs.rs`);
  - element ids and bounds are cached (`fast/global_id.rs`, `fast/layout_bounds.rs`).
- **Scroll layers (`fast/layers/`)**
  - Scrolled content is painted once into tiles (viewport plus 2 viewports of overscan), then composited at the new offset.
  - Only dirty tiles are re-rasterized, with a 64 MB tile budget.
  - D3D11 support: `GF/crates/gpui_windows/src/fast/layers/` (added in #26).
  - Lists (`uniform_list`/`list`) have layers **off** (`LIST_LAYERS`).
- **D3D11 state filtering (Windows only)**
  - Source: `GF/crates/gpui_windows/src/fast/draw_state.rs`, `fast/frame.rs`.
  - Upstream rebinds shaders, blend state, topology, SRVs and sampler, and maps the constant buffer, for every batch. gpui-fast skips redundant binds.
  - It reuses path vertex and sprite `Vec`s across frames instead of allocating them per batch.
- **Batching/atlas**: sprites are grouped by atlas texture, with sort keys colocated. This is upstream zed #65814 (`1089f40`), so GPUI also has it.
- **Fork's own numbers (Linux, CPU per frame)**
  - Unchanged window: 3.31 → 0.19 ms. Sidebar scroll: 5.95 → 0.79 ms. Streaming quotes: 5.06 → 1.35 ms (README).
  - All 60 panels changing each frame: only -29% (architecture.md §Results).

### What gpui-fast does NOT change (verified)

These are the same as upstream GPUI.
- **Idle frame clock.** The diff of `crates/gpui_windows` against upstream touches `platform.rs` by 1 line (class name). The vsync thread and frame loop are upstream's (see §3).
- **No damage tracking or partial present.** Every drawn frame re-encodes the **whole scene** and does a full `Present(0, 0)`:
  - `GF/crates/gpui_windows/src/directx_renderer.rs:250-258`
  - no `Present1`, dirty rects or scissoring were found in `gpui_windows/src`.
  - Retention saves CPU on scene *construction*, not GPU work on the scene.
- **Same request_frame semantics.**
  - `request_animation_frame` = `on_next_frame(|cx| cx.notify(view))` (`GF/crates/gpui/src/window.rs:2767-2771`).
  - The callback draws only if `invalidator.is_dirty() || force_render` (`:1919`).
  - Otherwise it re-presents only if `needs_present` is set or the input-rate tracker reports high-rate input. That tracker sustains presentation for 1 s after input of 60 events/s or more (`window.rs:1348-1388`, `:1914-1941`).

### Why zgui is still ahead (inference, with supporting evidence)

1. **Idle (11x).** GPUI and gpui-fast keep a per-vsync frame source alive while the window is visible, and each tick runs the main-thread `request_frame` closure even when nothing is dirty:
   - macOS: the display link starts whenever the window is visible (`GF/crates/gpui_macos/src/window.rs:839-865`, `:3023-3036`), and `step` fires each refresh.
   - Windows: a DwmFlush thread, see §3.
   - Data supports this: gpui-fast idle (0.347%) ≈ GPUI idle (0.331%). The fork did nothing for idle.
   - zgui describes itself as wake-driven ("idle schedules only the exit deadline", `ZG/docs/benchmarking.md`). The details are for the zgui researcher.
2. **GPU (12-63%).** zgui advertises "persistent damage rendering". gpui-fast redraws and presents the full window every dirty frame.
   - In the lab scene, gpui-fast's GPU use was *higher* than GPUI's: stream 16.5 vs 12.4 Mcycles/s; trace 44 ms/s vs zgui 5.9 ms/s.
   - Probable cause (unverified): scroll-layer tile rasterization/compositing, plus full-frame redraw.
3. **CPU under load (16-28%).** The benchmark adapter favors neither framework's strengths:
   - The whole lab UI is **one** `Lab` view, notified every tick (`ZG/comparisons/gpui-fast/src/main.rs`), so retained views and splicing cannot skip anything. Only layout-node and text carry help.
   - Scrolling is a manual `scroll_offset` with absolutely positioned rows, not a `ScrollHandle`, so scroll layers likely never engage.
   - gpui-fast still rebuilds the element tree for the notified view each frame. zgui updates only the bound targets (fine-grained signals).
4. **Smoothness.** gpui-fast had 3.7-16.6% late frames (>25 ms) against zgui's 0%. Likely cause (inference): frame-pacing interaction between GPUI's vsync-callback draw and the app's 60 Hz timer, plus heavier frames. Not root-caused.
5. **Where gpui-fast wins (from the chart).** Startup and binary size are slightly smaller. Plausible reason (inference): zgui ships wgpu, cosmic-text, AccessKit and Luau, while GPUI on macOS uses Metal and CoreText directly.

## 3. Upstream GPUI baseline on Windows (verified, zed `b4ecc45422`)

- **The vsync thread runs continuously.** `begin_vsync_thread` (`UP/crates/gpui_windows/src/platform.rs:433-495`) spawns a "VSyncProvider" thread that loops forever:
  - `DwmFlush()` (`UP/crates/gpui_windows/src/vsync.rs:37`). If it returns in under 1 ms, it falls back to `sleep(interval)`.
  - Then, **for every window**, `RedrawWindow(hwnd, RDW_INVALIDATE)` (`platform.rs:487`).
  - So every visible window gets a `WM_PAINT` at refresh rate forever. That is about 2 thread wakeups per vsync even when idle.
- **WM_PAINT → `draw_window`** (`UP/crates/gpui_windows/src/events.rs:1340`) calls the `request_frame` callback, then `ValidateRect`. The callback in `UP/crates/gpui/src/window.rs:1772-1950`:
  - checks thermal state;
  - throttles inactive windows to an interval;
  - runs next-frame callbacks;
  - draws only if dirty (`:1896`);
  - otherwise presents if `needs_present` or high-rate input (`:1894`).
  - `Window::present` (`:3622`) calls `platform_window.draw(&scene)`. On Windows that means **full re-render plus Present**, not just a present of the last buffer.
- **Renderer** (`UP/crates/gpui_windows/src/directx_renderer.rs`):
  - D3D11 with a flip-sequential swap chain, 3 buffers (`BUFFER_COUNT = 3`, `:1695`).
  - DirectComposition by default: `CreateSwapChainForComposition` plus `comp_device.Commit()` (`:1031`, `:1314-1317`). The opt-out is `GPUI_DISABLE_DIRECT_COMPOSITION`.
  - `Present(0, DXGI_PRESENT(0))`, i.e. sync interval 0 (`:245-251`). Pacing comes from the DwmFlush thread, not from Present.
  - No `SetMaximumFrameLatency` or waitable swap chain was found.
  - Each frame clears and draws every batch: shadows, quads, paths via a 4x MSAA intermediate texture, underlines, mono/subpixel/poly sprites. Pipeline state is rebound per batch; gpui-fast removes those redundant binds.
- **Text**
  - DirectWrite shaping. Glyphs are rasterized with `IDWriteGlyphRunAnalysis` (`UP/crates/gpui_windows/src/direct_write.rs:738`, `:957`).
  - Color glyph layers are composited on the GPU with D3D11 (`:1060-1181`).
  - Results are cached in a D3D11 texture atlas (`directx_atlas.rs:76`, `get_or_insert_with`).
- **Frame construction (all platforms)**: GPUI rebuilds views, Taffy and scene every dirty frame, with an R-tree for ordering. This is the cost gpui-fast attacks.

## 4. What is still open

- The source for the startup (+10%) and app-size (2.7x) numbers. Probably a post made after zgui's startup probe landed on 2026-10-23.
- The exact "smoothness 16-19%" metric.
- Whether any of the chart was measured on Windows. All published compare data is macOS.
