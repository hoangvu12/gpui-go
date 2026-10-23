# zgui vs GPUI Fast: what "zgui" is and why it wins (and loses)

Researched 2026-10-23. Sources were read-only clones:
- `zeronsh/zgui` @ `8b290e5` (2026-10-22), cloned to `/tmp/zgui-research/zgui`. Paths below marked `zgui:` are relative to that repo.
- `longbridge/gpui-fast` @ `91f60ec` (2026-10-22, shallow), cloned to `/tmp/gpuifast/gpui-fast`. Paths marked `gpui-fast:`. **Caveat:** the benchmark pinned `gpui-fast =0.1.4` (`zgui: comparisons/gpui-fast/Cargo.toml:8-10`). HEAD may differ from 0.1.4.
- Local `C:\Users\ADMIN\Desktop\nguyenvu\zui` (zeronsh's GPUI fork). Paths marked `zui:`.

Labels: **[V]** = verified from source, data or docs. **[I]** = my inference.

---

## 1. Identification

**zgui = `github.com/zeronsh/zgui`.** I'm highly confident (~95%) about the project. I'm only moderately confident that the user's specific chart comes from its dataset (see 1.3).

### 1.1 Evidence
- [V] `gh search repos zgui` returns `zeronsh/zgui`: "Retained Rust GUI framework with fine-grained updates for Linux and macOS". It was created 2026-09-26, is MIT-licensed, and its README says it supports Linux, macOS and Windows. Same org as the local `zui` (origin `zeronsh/zui`). Its README credits GPUI and zui as design references (`zgui: README.md:159`).
- [V] It ships a GPUI Fast adapter in-tree: `zgui: comparisons/gpui-fast/Cargo.toml:8-10` (`gpui = { package = "gpui-fast", version = "=0.1.4" }`, with the `font-kit` feature). It also has `comparisons/gpui`, `comparisons/quickgui` and a shared `comparisons/workload` crate.
- [V] Its published dashboard has a page titled **"zgui on macOS — measured against GPUI and GPUI Fast"**, with the hero "Damage-based rendering, measured." The page is `zgui: site/compare/index.html` (lines 7, 18), live at https://zgui-performance.pages.dev/compare/ (fetched 2026-10-23; its `data.json` is byte-identical in size, 53,306 B, to the repo copy). The root page links "zgui vs GPUI Fast on macOS →". Commit `8d4acdc` (2026-10-20) is "Publish newer macOS runs that include GPUI Fast on the dashboard", and `e104310` (2026-10-10) is "Measure macOS lab, heavy and animate against GPUI and GPUI Fast".
- [V] "GPUI Fast" = `longbridge/gpui-fast`: "GPUI extracted from the Zed monorepo … with the layout engine reworked to keep what did not change between frames". It has retained views, layout and text, plus cached GPU scroll tiles (`gpui-fast: README.md`).
- **Ruled out:**
  - `zig-gamedev/zgui` is Zig bindings for Dear ImGui and has no GPUI comparison.
  - `zortax/zgui` is a different "reactive, retained rust ui framework". Its repo listing has no comparisons dir, and a code search for `gpui-fast` there returns 0 hits.
  - The crates.io `zgui` 0.1.x is a placeholder.
  - The local `zui` is not zgui. It is zeronsh's GPUI fork (`zui: README.md`: "Zeron's fork of GPUI"). zgui only cites it as a reference.

### 1.2 Methodology of the source chart (zgui compare page) [V]
From `zgui: site/compare/index.html:47-51` and `site/compare/data.json` → `run`:
- **Machine:** Apple M5 Max (Mac17,6), 18 cores, 128 GB, macOS 26.4, AC power, rustc 1.96.0, 120 Hz display. Windows were on screen and presented through Core Animation. **Only macOS/Metal was measured. There is no Windows measurement in this chart.**
- **Source:** commit `5493829` with a dirty tree (diff hash recorded). Runs were 2026-10-11/12, with 51 + 39 + 12 trials and 96 + 72 + 24 frame-pacing captures.
- **Scenes** (same UI built in each framework from the shared workload crate):
  - **lab:** a streaming transcript plus a 100,000-row virtual list. Modes are idle, stream, scroll and both. "Busy" in the user's chart most likely means `both`.
  - **heavy:** a 1280×800 dashboard with 6 tiles, 30 cards of 26 sparkline bars, a status table and a log. Most values change 60 times/s and the table scrolls.
  - **animate:** 48 blinking dots and a shimmering text line, animated at display rate (120 Hz). Each framework draws as often as it can present.
- **Sampling:** each trial runs 20 s; the first 5 s are excluded and the next 15 s sampled every 50 ms. There are 3 repeats in rotated order, and medians are reported.
  - **Acceptance:** lab and heavy runs must deliver 58–61 logical updates/s.
  - **CPU:** process user+sys time.
  - **Memory:** physical footprint, including process-owned GPU memory. About 180–190 MB of it is Metal driver memory in every app.
  - **GPU:** two measures. (a) "GPU work" is vertex+fragment execution from Metal System Trace (5 s captures). (b) "GPU time (IOKit)" is per-app GPU time × mean active clock, in megacycles/s. The page itself says (b) "favours drawing fewer frames".
  - **Frame pacing:** eight 8 s window captures per framework/mode, reporting the p99 frame interval and "late frames" (intervals over 25 ms).
- **Stated caveats:** "A personal Mac in use … GPU about 45% busy … per-app GPU readings vary two- to threefold between runs".
- **Startup** is not on the compare page. It comes from `scripts/startup_probe.swift`, described in `zgui: docs/benchmarking.md:38-51` and PR #8 / #33 (2026-10-23). The probe spawns the binary, waits until the header text has drawn, and reads instructions retired. Over 10 alternating launches on the M5 Max, zgui `component_workload` took **924 M instructions vs 840 M for the GPUI adapter (+10%)**. Before PR #8 zgui took 1,111 M.

### 1.3 How the user's chart numbers line up with the published data [V numbers, I mapping]
The figures below are zgui vs gpui-fast medians, computed from `site/compare/data.json`:

| Chart claim | Published data | Match? |
|---|---|---|
| Idle CPU 11× less | lab idle: 0.0316 vs 0.3473 % of a core = 11.0× (heavy idle 8.2×) | exact |
| Busy/heavy/animation CPU 16–28% less | lab both −26%, heavy both −28% (stream −30%), animate −16% | yes |
| GPU animation 63% less | animate IOKit megacycles: 7.62 vs 20.82 = −63% | exact |
| GPU busy 30%, heavy 12% less | lab both megacycles −40% (trace −76%); heavy both −25% (trace −45%); heavy stream −18% (gpu% −14%) | approximate only |
| Memory up to 8% less | lab idle footprint 102.8 vs 112.0 MB = −8%; active modes −1 to −5% | exact |
| Smoothness 16–19% better busy/heavy, tie in animation | p99 interval: lab both 23.0 vs 29.7 ms (−23%), heavy both 23.4 vs 29.6 ms (−21%); animate 12.8 vs 13.0 ms (tie). Late frames: zgui 0% vs 3.7–9.1% | direction yes, magnitude slightly off |
| Startup work 10% more | 924 M vs 840 M instructions (+10%), **but the reference was the GPUI adapter, not gpui-fast** (PR #8 table) | exact vs GPUI |
| App size 2.7× bigger | Lab binaries in `docs/results/latest-macos-2026-10-12-5493829/audit.json`: zgui 11.06 MB, GPUI 4.59 MB (2.41×), gpui-fast 7.48 MB (1.48×) | not matched |

**Conclusion [I]:** the chart is derived from zgui's macOS compare dataset, or from a slightly newer run that isn't checked into `main`. Idle, CPU, animation GPU, memory and startup match exactly. GPU busy/heavy, smoothness and app size don't match any checked-in run; I scanned all five gpui-fast result sets dated 10-08 to 10-12.
- Possible sources for the mismatches: a later rerun, a different metric (for example, the "busy" scene could be the separate `busy_workload` example, "Busy window + one spinner", `zgui: crates/zgui-desktop/examples/busy_workload.rs:1-2`), or a summariser rounding.
- I couldn't find a tweet or blog post with the exact chart.

---

## 2. Why zgui wins: techniques, with citations

zgui is **not a GPUI fork**. It is a separate retained-mode framework with its own core, a renderer built on wgpu, and a winit host (`zgui: docs/architecture.md:3`). GPUI Fast **is** a GPUI fork that adds retention on the CPU side (`gpui-fast: docs/retained-mode.md:13-26`). So the comparison is between *retained, damage-driven rendering through to the pixels* and *GPUI's per-frame scene rebuild with CPU-side caching added*.

### 2.1 Idle CPU (11×): event-driven loop, no per-vsync callback when clean
- [V] **zgui:** winit runs `ControlFlow::Wait` (or `WaitUntil` the next timer or frame deadline). It uses `Poll` only when work is ready (`zgui: crates/zgui-desktop/src/application.rs:3104-3105, 3177-3183`).
  - The macOS `CVDisplayLink` is created only "on first demand, so windows that never animate hold none". It runs only while frames are wanted (`application.rs:2511-2580`, comment at ~2531).
  - For steady 60 Hz app updates, a timer aligned to the link's refresh grid paces frames, and the link "runs again only to resync" about once a second (`VSYNC_RESYNC`, `frame_source.rs:64-66`; `application.rs:955-960, 2545-2575`).
  - An idle `flush` is constant time with no tree traversal (`docs/architecture.md:25, 49`). "Idle renders submit no GPU work" (`crates/zgui-gpu/README.md:24`).
  - The async executor is wake-driven and doesn't poll sleeping tasks (`docs/architecture.md:9, 92`).
- [V] **gpui-fast:** the display link runs whenever the window is visible. It stops only on occlusion (`gpui-fast: crates/gpui_macos/src/window.rs:3023-3036`). Every tick calls the frame callback (`window.rs:3374-3396`), which then checks for dirtiness (`crates/gpui/src/window.rs:1795+`). That costs a main-thread wakeup every refresh (120/s) while idle.
  - The same team fixed exactly this in their GPUI fork: `zui` commit `8a32c0e` "Park macOS display links while windows are idle" and `0003b92` "Reduce idle macOS frame callbacks" (`zui: docs/macos-resource-usage.md:3-9`: "A clean native window previously dispatched an application callback on every CVDisplayLink tick").
- [I] This difference alone explains the order-of-magnitude idle gap: about 0.03% vs 0.35% of a core.

### 2.2 Busy/heavy CPU (−16 to −28%): fine-grained signals, retained scene, damage-culled paint walk, direct Metal encoding
- [V] **Fine-grained reactivity:** signal writes update only their bound retained node. Equal writes are no-ops, and component functions don't rerun (`zgui: docs/architecture.md:7, 15, 68`; `README.md:159`: "direct dependency subscriptions instead of rerunning whole component functions").
  - gpui-fast instead re-renders each *view* whose dependencies changed (`gpui-fast: docs/retained-mode.md:19-26`). Its unit of work is coarser: a whole view subtree, plus layout and prepaint.
- [V] **Separate dirty classes:** layout, paint and composition are tracked separately. Fixed-size text writes queue one paint node with no layout. Translate, opacity and transform changes are composition-only (`docs/architecture.md:25, 47-60`).
  - Layout boundaries: a fixed-size container re-lays out only its subtree (`architecture.md:19`).
- [V] **Paint walk culled by damage:** "the GPU renderer visits only subtrees whose cached bounds meet the frame's damage … per-frame work follows what changed rather than the size of the scene" (`architecture.md:31`).
  - gpui-fast still rebuilds and uploads a full `Scene` every frame. `write_instances(scene, …)` covers all primitives (`gpui-fast: crates/gpui_apple/src/metal_renderer.rs:569-590`).
- [V] **Bypassing wgpu's validation on macOS:** `zgui: crates/zgui-gpu/src/metal_frame.rs:1-13` says wgpu-core's validation, tracking and replay was "about half of an animating window's presentation CPU". Common frames are therefore encoded straight into Metal, using pipelines compiled from the same WGSL through naga, on wgpu's own queue.
- [V] **One submission per frame:** render and present-blit share one command buffer, vertices go through reused upload memory, and consecutive primitives with the same texture and clip merge into one instanced draw (`architecture.md:31`; `zgui-gpu/README.md:44`).
- [I] The animation scene gains least (−16%) because almost everything that's visible changes each frame, so damage covers most of the work. The lab and heavy scenes gain more because only small regions change.

### 2.3 GPU (−12 to −63%): persistent target, damage-scissored repaint, no full-frame copy on present
- [V] **zgui:** "Ordinary frames replay only intersecting primitives into merged damage scissors on a persistent render texture" (`zgui-gpu/README.md:24`). The code uses `LoadOp::Load` and `set_scissor_rect` per damage region (`crates/zgui-gpu/src/lib.rs:3788-3826`).
  - Damage merges overlapping rects, rounds them out to pixels, and collapses to the full viewport above 64 regions (`architecture.md:27`).
- [V] **macOS presentation through renderer-owned IOSurfaces instead of `CAMetalLayer` drawables** (`zgui: crates/zgui-gpu/src/layer_present.rs:1-23`). The file explains: "A drawable's contents are not kept between frames, so presenting one means filling all of it: copying the whole retained frame every frame. A surface of the renderer's own keeps what it held … each frame repaints what changed since that surface was last shown, and showing it copies nothing."
  - At full rate it uses `AVSampleBufferDisplayLayer`; slower frames become plain layer contents.
- [V] **gpui-fast:** acquires a new `CAMetalLayer` drawable each frame and clears it (`metal_renderer.rs:542`, `MTLLoadAction::Clear` at `:881`). It redraws every primitive, with `maximum_drawable_count(3)` (`:219`).
  - Its scroll layers cache GPU tiles for scrolling only (`gpui-fast: README.md`, `docs/scroll-layers.md`). This fits gpui-fast's GPU cost being closest to zgui's in scroll-heavy scenes, while lab stream GPU trace is 5.9 vs 44.4 ms/s.
- [V] **Bounded effects:** blur is reconstructed only over dependency regions with halo, and scratch textures are lazy (`zgui-gpu/README.md:24`; `architecture.md:29`). The benchmark disables blur by default (`docs/benchmarking.md:9`).
- [V] **Transform and opacity animation reuses cached layer textures:** a change of N matrices produces N composite instances in one pass (`zgui-gpu/README.md:54-56`). [I] This is likely why the animation scene's IOKit GPU megacycles fall 63%. Its Metal-trace GPU work per command buffer is 130 µs vs 182 µs. The IOKit metric also counts per-command-buffer fixed cost, and zgui submits exactly one buffer per frame.

### 2.4 Smoothness (fewer late frames, better p99)
- [V] zgui paces from `CVDisplayLink`, which fires at the *start* of each refresh. It avoids `CADisplayLink`, which fires about 2 ms before the deadline, so "any hiccup then misses the compositor's latch" (`zgui: crates/zgui-desktop/src/frame_source.rs:75-90`).
- [V] App-timed frames are handed to the window server about 2 ms before a refresh, on the exact refresh grid (`Latch::LEAD`). Without this, 1–20% of intervals at 120 Hz were missed. With it, 99.9% were on time (`application.rs:2470-2487`).
- [V] There are two drawables on macOS (`desired_maximum_frame_latency = 1`), "measured no frame-rate or CPU cost against three" (`zgui: crates/zgui-gpu/src/lib.rs:1496-1504`). gpui-fast uses 3 (`metal_renderer.rs:219`).
- [I] The main driver is probably simply less CPU per frame (2.2). Less work per frame means fewer misses; zgui recorded 0% late frames vs 3.7–9.1% for gpui-fast. The animation tie follows from both being GPU-bound at 120 Hz with headroom.

### 2.5 Memory (up to −8%)
- [V] **Lazy glyph atlas:** a 1-pixel placeholder, then 1 MiB on the first glyph, growing to a 16 MiB maximum. It resets at 75% occupancy (`zgui-gpu/README.md:22, 69`). The eager-vs-lazy change saved 13–15 MiB RSS on Linux (`docs/results/README.md`).
- [V] **Hard bounds:** 1,024 shaped nodes and 8 MiB; 8,192 glyph-metadata entries; a 32 MiB vertex ceiling with reused storage; 64 MiB image and layer textures; 32 MiB canvas and SVG raster caches (`zgui-gpu/README.md:44, 54`; `docs/performance-feature-audit.md:16-25`).
- [V] **Arena scene:** generational `NodeId`s with reused slots (`architecture.md:13`). Only primary wgpu backends are enabled; a GL/EGL init removed about 22 MiB and 49 threads on Linux (`zgui-gpu/README.md:74-76`). Two drawables instead of three saves about 13 MB at 5K (`lib.rs:1496-1499`).
- [V] After the first frame, freed startup heap is trimmed with `malloc_trim` on glibc only (`application.rs:898-905, 3362-3375`).
- [I] The idle gap (−8%) is larger than the active gaps (−1 to −5%), which fits atlas and drawable savings being a fixed amount on top of roughly 180–190 MB of Metal driver memory.

### 2.6 Threading [V/I]
- [V] zgui is single-UI-thread. CVDisplayLink and the compositor clock fire on their own threads and post events to the winit proxy (`application.rs:2531-2536`; `frame_source.rs:498-606`). Background work runs on bounded workers (`README.md`, feature list).
- [I] Threading isn't a distinguishing factor in these results; both frameworks render on the main thread.

---

## 3. Why zgui loses

### 3.1 Startup work (+10% instructions)
- [V] zgui builds a **wgpu device and pipelines from WGSL compiled at runtime through naga**. That covers both the wgpu pipelines and the direct-Metal pipelines (`metal_frame.rs:7-9`: "built here from the same WGSL through naga").
  - GPUI compiles `shaders.metal` into a **precompiled `.metallib` at build time** with `xcrun` (`gpui-fast: crates/gpui_apple/build.rs:153-190`). Runtime compilation is opt-in through `runtime_shaders`.
- [V] **Font discovery:** zgui uses cosmic-text/fontdb, which scanned and parsed every installed font file. That was about 100 M instructions and 10 ms for 370 files, and PR #8 now caches it on disk. GPUI on macOS uses CoreText through font-kit (`comparisons/gpui-fast/Cargo.toml:9-10`).
- [V] PR #8 also turned off wgpu's indirect-draw validation, which compiled a compute shader at device creation. With these fixes zgui still sits at 924 M vs 840 M instructions to first frame (PR #8 table; `docs/benchmarking.md:38-51`).
- [I] The rest is wgpu instance, adapter and device setup plus naga translation, which GPUI's direct `metal` crate path doesn't pay.

### 3.2 App size (2.7× in the chart; 1.5× vs gpui-fast and 2.4× vs GPUI in checked-in audits)
- [V] Measured lab binaries (`docs/results/latest-macos-2026-10-12-5493829/audit.json`): zgui 11.06 MB, GPUI 4.59 MB, gpui-fast 7.48 MB, QuickGUI 12.97 MB. The older Linux binaries (`site/measurement.json`) were zgui 21.6 MB vs GPUI 17.8 MB.
- [V] **zgui's statically linked stack** (`zgui: crates/zgui-gpu/Cargo.toml:9-36`, `crates/zgui-desktop/Cargo.toml:15-27`):
  - **GPU:** wgpu 29 (wgpu-core, wgpu-hal, **naga** with WGSL front end and MSL back end). It also has a hand-written objc2 Metal, IOSurface and CoreAnimation path on top of that.
  - **Text:** cosmic-text with swash, fontdb and a shaper.
  - **Images:** resvg, image (png/jpeg) and gif.
  - **Platform services:** accesskit plus accesskit_winit, rfd, arboard and muda.
  - **Software reference renderer:** softbuffer plus fontdue.
  - **Benchmark:** `zgui-workload` is even a normal dependency of `zgui-desktop`.
- [V] GPUI on macOS links the thin `metal` crate, CoreText and font-kit, and a prebuilt metallib, with no naga or wgpu (`gpui-fast: crates/gpui_apple/Cargo.toml:33-46`).
- [I] In short, zgui carries a cross-platform shader compiler and two renderers (wgpu plus direct Metal, and a CPU reference renderer) in every binary. No fonts are embedded in the benchmark binaries; only example themes `include_bytes!` Geist (`examples/support/zeron_theme.rs:108-114`). The release profile for the comparison adapters uses thin LTO and `codegen-units = 1` (`comparisons/gpui-fast/Cargo.toml:12-14`); I didn't check zgui's own profile.

---

## 4. Windows-specific notes (relevant to gpui-go; not measured in the chart)
- [V] zgui on Windows uses wgpu DX12 with `PresentMode::Mailbox`: "DXGI's flip model at sync interval 0 composes the newest, without tearing … frames are paced by the compositor's clock". It keeps the default two frames of latency (`zgui: crates/zgui-gpu/src/lib.rs:1489-1495, 1500-1502`).
- [V] Frame pacing comes from a thread waiting on `DCompositionWaitForCompositorClock` (Windows 11), falling back to `DwmFlush` on Windows 10 (`zgui: crates/zgui-desktop/src/frame_source.rs:498-606`). Input arriving between ticks draws immediately (`application.rs:945-946`, commit `b6169b0` "a key typed during an animation shows on the next refresh").
- [V] A DXGI frame-statistics logger checks for repeated refreshes (`crates/zgui-gpu/src/dxgi.rs:1-25`).
- [I] There is no DirectComposition visual tree or IOSurface-equivalent for retaining the swapchain contents on Windows. The persistent wgpu render texture is copied into the swapchain each frame ("present by copy when the surface allows it", `lib.rs:1514-1520`). So the full-frame present copy that macOS avoids still happens on Windows. Expect smaller GPU wins there than in the macOS chart.

## 5. Takeaways for a GPUI-derived renderer [I]
In rough order of payoff for the chart's metrics:
1. **Park the vsync source when clean.** Don't dispatch per-refresh callbacks to check dirtiness. This gives the idle 11×.
2. **Keep a persistent back buffer and repaint damage rects with scissors** (`LoadOp::Load`). Present without a full-frame copy where the OS allows it: IOSurface on macOS, or on Windows a DXGI flip swapchain with `Present1` dirty rects, or a retained intermediate texture.
3. **Cull the paint walk by damage** and cache subtree paint bounds.
4. **Track composition-only changes** (transform, opacity) separately, and composite cached layers for them.
5. **Pace frames to the start of the refresh** and hand over about 2 ms before latch. Use two drawables, not three.
6. **Use lazy, bounded atlases and caches.**

The costs are a bigger binary and startup if you adopt wgpu+naga; GPUI's prebuilt-shader, native-API approach is lighter on both.
