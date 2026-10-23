# gpui-go vs gpui-fast: what gpui-go is missing for building apps

Researched 2026-10-23 from source only. I did not use gpui-fast's README or docs as evidence.

- **gpui-fast:** a shallow clone of https://github.com/longbridge/gpui-fast at
  **`124f79b0f14c688ae87877e370c015a9dc9d7ce5`** (2026-10-23 16:43 +0800, "fast: schedule Windows frames on demand (#84)").
  It was cloned into `%TEMP%\research-src\gpui-fast-head` and only read. Nothing was built or run.
- **`GF:`** marks a path inside that clone. `GF:fast/` is `crates/gpui/src/fast/` and `GF:wfast/` is `crates/gpui_windows/src/fast/`.
- **gpui-go:** this worktree (branch `research/app-gaps`). Its paths are repo-relative. `N:` is `reference/native/src/`.
- **Tags:** **[V]** means I read the code at the cited lines. **[I]** means it is inferred from names, comments or surrounding code without tracing it fully.
- **Sizes:** S ≈ under 1 day, M ≈ a few days, L ≈ 1–2 weeks, XL ≈ several weeks.
- **Prior work:** I re-checked `research/zgui-vs-gpui-fast/gpui-fast.md`, which used gpui-fast `91f60ec`, one day older. Two of its claims are now **out of date**; see "Surprises".

## Summary: top 10 gaps, ranked by how much they matter for real apps

| # | Gap | Kind | Fix needs | Size |
|---|---|---|---|---|
| 1 | **Automatic view retention.** gpui-fast reuses *every* entity view from last frame when nothing it read changed. gpui-go has only opt-in `CachedView`, and no production code calls it. | perf | Go (plus a native scene-order replay to get the full win) | XL |
| 2 | **Splicing.** When only a nested view changes, gpui-fast copies the parent's output around it and rebuilds just the child. gpui-go rebuilds the whole ancestor chain. | perf | Go | L (needs #1) |
| 3 | **Retained layout and text-measurement carry.** gpui-fast keeps Taffy nodes and reuses measurements. gpui-go resets the layout engine every frame and re-lays-out the whole tree. | perf | native layout ABI + Go | L |
| 4 | **Gradients and patterns** (`linear_gradient`, `checkerboard`, `pattern_slash`). gpui-go backgrounds are solid only. | feature | native shader + Go API | M |
| 5 | **Rounded overflow clipping.** gpui-fast clips children to the parent's corner radius. gpui-go's content mask is a plain rectangle, so children spill past rounded corners. | feature | native shader + Go | M |
| 6 | **Scroll layers and list-row retention.** gpui-fast paints scrolled content into 512-px tiles once, composites them while scrolling, and keeps list rows. gpui-go rebuilds and repaints everything visible on each scroll frame. | perf | native (tile textures, composite) + Go (policy, input carry) | XL |
| 7 | **Window kinds and native prompt.** gpui-go has no `PopUp`, `Floating` or `Dialog` window kinds and no `window.prompt` (TaskDialog message box). | feature | Go only (Win32 via syscall) | M |
| 8 | **Accessibility under reuse.** gpui-fast replays a reused subtree's AccessKit nodes. A gpui-go `CachedView` contributes *no* UIA nodes on frames where it is reused. This becomes a correctness bug as soon as #1 is adopted. | correctness | Go | M |
| 9 | **D3D11 redundant-state filtering.** gpui-fast skips binds that are already set and skips unchanged global-buffer writes. gpui-go rebinds everything for every batch. | perf | native | S |
| 10 | **Path tessellation cache and bounds-order replay.** gpui-fast caches tessellated paths and replays draw orders for reused ranges. gpui-go re-tessellates every path and re-inserts replayed ranges through the R-tree. | perf | Go + native | M |

Smaller gaps that apps still notice:

- **Missing `gpui` element APIs** (Go only, S each). Tooltips, `on_mouse_exit` and `on_mouse_pressure` are not ported in `gpui` (`base/tooltip.go` exists). `container_query` is also missing.
- **Springs only in Base.** The spring animation half is not in `gpui` core; it exists only as `base/motion.go`.
- **CJK line breaking** (Go, S). gpui-fast does not wrap before a closing bracket or after an opening one.
- **Number shaping** (Go, M). gpui-fast shapes numbers from cached per-digit glyphs and kerning instead of calling the platform each time.
- **Hover freeze while wheel-scrolling** (Go, S).
- **WebView2 / native-surface composition** (native + Go, L).

### Where gpui-go is ahead or equal

- **Frame latency** [V]. gpui-go has `SetMaximumFrameLatency(1)` with a waitable object (`N:renderer.rs:174`, `:641-648`). gpui-fast has no waitable swap chain (`GF:crates/gpui_windows/src/directx_renderer.rs:250-258`, `Present(0,0)`).
- **Styling extras from the GPUI-CE pin** [V]. Backdrop blur and filters (`gpui/style.go:1035-1041`, `gpui/div.go:1896-1898`) and corner smoothing (`gpui/scene.go:61`) have no gpui-fast counterpart. gpui-fast's only blur is the box-shadow `blur_radius` (`GF:crates/gpui/src/style.rs:377`).
- **Batched native scene inserts** [V] (`internal/native/scene_batch.go:1-20`). This has no counterpart in gpui-fast, because gpui-fast has no FFI boundary.
- **Idle behaviour is now equal** [V]. Both are demand driven: gpui-go's pacer (`gpui/frameloop_windows.go:19-25`, `:83-99`) and gpui-fast's frame schedule (`GF:wfast/frame_schedule.rs:30-87`, `:144-162`).

### Surprises

1. **gpui-fast's Windows frame loop changed today.**
   - The prior note said gpui-fast "did nothing for idle" and kept upstream's per-vsync `RedrawWindow` thread. That was true at `91f60ec`.
   - HEAD `124f79b` (#84) adds `GF:wfast/frame_schedule.rs`. A `FrameWake` condvar makes the VSync worker sleep until a window requests a frame (`:42-61`, `:116-123`). Input requests a frame (`:144-162`). Device loss wakes it through `GF:wfast/device_wake.rs`.
   - So gpui-go's demand-driven pacer is no longer an advantage over gpui-fast. Its only remaining edge here is the latency waitable.
2. **List layers are now ON.** `LIST_LAYERS: bool = true` (`GF:fast/layers/lists.rs:78`). The prior note said they were off.
3. **gpui-go's `CachedView` is dead code in practice.** The only non-test references are its definition (`gpui/view_cache.go:410`), `gpui/view.go` and `gpui/titlebar.go`. `base/` never calls it; `base/input_cached_view_test.go` is a test. In gpui-fast, *every* `Entity`/`AnyView` placed as a child goes through `splice::rebuildable` (`GF:crates/gpui/src/view.rs:111`, `:124`). Retention is on by default and can be turned off with `GPUI_VIEW_RETENTION=0` (`GF:fast/retained.rs:292`).
4. **gpui-fast adds native composition.** It has a public API to put platform surfaces such as WebView2 between GPUI's base content and its overlays (`GF:fast/composition.rs:1-35`, `:1198`, `:1215`; DirectComposition side `GF:wfast/composition.rs`). This is new app capability, not just speed.
5. **Native popups: both reject `AnchoredPopup` on Windows** (`GF:crates/gpui_windows/src/window.rs:447-449`, `gpui/desktop.go:316-340`). gpui-fast, however, still has `PopUp` (topmost tool window, `window.rs:496`), `Dialog` (modal with the parent disabled, `:468-480`, `:508-510`) and `Floating`. gpui-go has only `Normal`.

## Full table

| Area | gpui-fast (file:line) | gpui-go (file:line) | Status | Fix needs | Size |
|---|---|---|---|---|---|
| **Retained views (automatic)** | Every entity view is retained. `GF:crates/gpui/src/view.rs:111,124` (`rebuildable`), `:340-387` (phases route to `fast::retained`). Reuse is decided at `GF:fast/retained.rs:587-626` (`reusable_record`: dependencies, hovers, offsets, refreshing, drag, a11y staleness) and `:1472-1530` (`request_view_layout_inner`: Retained, Kept, Spliced, Prebuilt, Built). The record keeps prepaint and paint ranges, deps, own deps, hovers and layout keys (`:86-124`). Default on (`:292`). [V] | `gpui/view_cache.go:410` `CachedView` is opt-in and needs a definite size (`:403`). Bounds, mask and text style form the key (`:387`). Invalidation is dirty-view plus ancestors (`:302`). No callers in `base/`. [V] | partial: mechanism present, opt-in only, unused | Go | XL |
| **Dependency tracking** | Records entities, globals, `StateVersion` (scroll and list state), pointer, keys and hovers read while building. `GF:fast/dependencies.rs:99-129,193-246,512-544,567-579,726-775`. [V] | `detectAccessedEntities` covers entity reads only (`gpui/view_cache.go:339-362`). Globals, scroll-offset reads and hover reads are not tracked. [V] | partial | Go | L |
| **Splicing** (rebuild only the nested view that changed) | `GF:fast/splice.rs:1-21` (design), `:50-66` (`cached`/`rebuildable`), `:101` (`rebuild_here`). Used from `retained.rs:1514-1518`. [V] | none. A notify dirties the view and all its ancestors, which are rebuilt (`gpui/view_cache.go:302-326`). [V] | missing | Go | L |
| **Retained layout** | Taffy nodes are kept across frames and matched by key path (`GF:fast/layout_key.rs`). Style and children are rewritten only when the fingerprint differs (`GF:fast/layout.rs:1`, `:998` `request_retained_layout`, `:866` `relayout_in_place`, `:923` `release_unclaimed_nodes`). [V] | The engine is reset every frame (`gpui/render.go:103-110` → `gpui/layout.go:927-943` `Reset`). [V] | missing | native layout ABI (keep nodes, set-style diff) + Go | L |
| **Text measurement carry** | Same text, runs and style reuse last frame's measurement. Changed text is re-measured under the constraints Taffy logged (`GF:fast/text.rs:570` `carry_measurement`, `:609` `request_carried_measured_layout`; `GF:fast/layout.rs:271` `MEASURE_LOG_CAPACITY`, `:1221` `replay_pending_measures`). [V] | Two-frame shaped-line cache at parity with upstream (`gpui/text_cache.go:1-40`). Measurement itself runs every frame because layout resets. [V] | partial | Go (+ layout from the row above) | M |
| **Bounds tree / draw order** | A grid replaces the R-tree, and the orderings of reused ranges are replayed (`GF:fast/bounds_tree.rs:1-4`, `:480` `replay`). Counting sort for draw order with scratch kept between frames (`GF:fast/scene.rs:1-30`). [V] | Upstream R-tree port (`N:scene.rs:940-1107`). `Scene.Replay` re-inserts every op through the tree (`N:scene.rs:1687-1700`). A fresh `NewScene` is made per frame (`gpui/render.go:95`). [V] | partial | native | M |
| **Dispatch subtree bulk copy** | `GF:fast/dispatch.rs:1-12` (`copy_nodes`). [V] | Node-by-node reuse as in upstream (`gpui/view_cache.go:234-259`). [V] | partial (only matters once #1 lands) | Go | S |
| **Global id / layout-bounds caches** | `GF:fast/global_id.rs:1`, `GF:fast/layout_bounds.rs:1-12` (slot-indexed `Vec` instead of a hash map). [V] | not ported [I] | missing | Go / native | S |
| **Glyph paint per run** | Rendering decided once per run, not per glyph. Integer-shift-stable quantization for tiles (`GF:fast/glyphs.rs:1-30`). [V] | Raster metadata and atlas tile cache per glyph (`gpui/glyph_cache.go:3-10`; ticket 02). [V] | partial: cache present; per-run setup and tile-stable quantization missing | Go | S |
| **Path tessellation cache** | LRU of 2048 shapes keyed by the path relative to its first point (`GF:fast/path_cache.rs:1-22`, `:23`, `:58`). [V] | Every `Build()` goes to native lyon (`gpui/path.go:12-21`). No cache. [V] | missing | Go (key + cache) + native (accept cached triangles) | M |
| **Scroll layers (tiles)** | 512-px tiles (`GF:fast/layers/paint.rs:42`), 2 viewports of overscan (`:862`), 64 MB budget (`GF:fast/layers/policy.rs:55`), promotion and demotion policy (`policy.rs:1-17`), tile hashing and dirty tiles (`tiles.rs`), hitbox and listener carry (`reuse.rs`, `input.rs`), paths drawn as an overlay above the tiles (`overlay.rs`). D3D11 tile cache, raster and composite in `GF:wfast/layers/{tile_cache,raster,composite}.rs`. [V] | none; scroll repaints its content (`gpui/scroll.go`, `gpui/scroll_element.go`; no tile code). [V] | missing | native (tile textures, raster to texture, composite) + Go (policy, carry) | XL |
| **List-row retention in layers** | `LIST_LAYERS = true`. Rows kept within the overscan are not re-rendered on scroll-only frames (`GF:fast/layers/lists.rs:1-30`, `:78`). [V] | `UniformList` (`gpui/list.go:169`) and `List` (`gpui/listelement.go:882`) render the visible range every frame. [V] | missing | Go (+ the layers row above) | L |
| **Hover freeze on wheel** | 100 ms (`GF:fast/layers/hover_freeze.rs:1-26`). [V] | none [I] | missing | Go | S |
| **Wheel notify accounting** | `GF:fast/layers/wheel.rs`, `invalidate.rs`. [V] | n/a without layers | missing | Go | S |
| **D3D11 state dedup** | `DrawState` skips redundant shader, blend, topology, SRV and sampler binds, and the batch constant buffer (`GF:wfast/draw_state.rs:1-15`, `:29-113`). `fast::frame` reuses path vertex and sprite buffers (`GF:wfast/frame.rs:1-12`, `:87`). [V] | Full rebind for each batch: shaders, blend, topology, viewport, CBs, SRVs and sampler (`N:renderer/draw.rs:500-526`). [V] | missing | native | S |
| **Global CB write-on-change** | `GF:wfast/globals.rs:1-7`, `:26`. [V] | `MAP_WRITE_DISCARD` per frame (`N:renderer/draw.rs:1028-1037`). [I: exact call site of globals not traced] | missing | native | S |
| **Frame loop / idle** | Demand-driven since #84: `FrameWake` condvar, per-window `FrameRequest`, input requests a frame (`GF:wfast/frame_schedule.rs:30-87`, `:116-123`, `:144-162`). Device-loss wake in `GF:wfast/device_wake.rs`. [V] | Demand-driven pacer that exits one vblank after demand ends (`gpui/frameloop_windows.go:19-25`, `:67-99`). [V] | **present (equal)** | n/a | n/a |
| **Present / latency** | `Present(0,0)`, `FLIP_SEQUENTIAL`, 3 buffers, no waitable (`GF:crates/gpui_windows/src/directx_renderer.rs:250-258`, `:1330-1362`). [V] | Same present, plus `SetMaximumFrameLatency(1)` and a waitable (`N:renderer.rs:174,178,641-648,1746`; `gpui/frameloop_windows.go:155-178`). [V] | **present (gpui-go ahead)** | n/a | n/a |
| **Damage / partial present** | none; full scene each dirty frame (`directx_renderer.rs:250`). [V] | none [V] | equal (neither) | — | — |
| **Gradients and patterns** | `linear_gradient` (`GF:crates/gpui/src/color.rs:865`), `linear_color_stop` (`:893`), `pattern_slash` (`:827`), `checkerboard` (`:841`). HLSL `gradient_color` (`GF:crates/gpui_windows/src/shaders.hlsl:317-356`). [V] | `Background` is solid only (`gpui/paint.go:167-178`). The Tailwind `bg-gradient`/`bg-linear` classes are rejected with "the native renderer is solid-only" (`gpui/class.go:1119`). [V] | missing | native shader + Go API | M |
| **Rounded overflow clip** | Rounded scopes evaluated in the colour shader from a padding-edge geometry texture (`GF:fast/rounded_clip.rs:1-8`, `GF:wfast/rounded_clip.rs:1-3`, wired at `directx_renderer.rs:1849`). [V] | `OverflowMask` returns a rectangle (`gpui/style.go:1181-1196`). [V] | missing | native shader + Go | M |
| **Transforms** | SVG-only `TransformationMatrix` (`GF:crates/gpui/src/elements/svg.rs:232-277`). No element transforms. [V] | `SvgTransformation` (`gpui/svgelement.go:44-64`). [V] | present (equal) | — | — |
| **Blur / filters** | shadow `blur_radius` only (`GF:crates/gpui/src/style.rs:377`) [V] | backdrop blur and filter chains (`gpui/style.go:1035-1041`, `gpui/div.go:1896-1990`) [V] | **present (gpui-go ahead)** | — | — |
| **Masks** | `ContentMask` rectangles plus rounded scopes (above) [V] | rectangles [V] | partial (see rounded clip) | native | M |
| **Animation (duration)** | `with_animation(s)` and easings (`GF:crates/gpui/src/elements/animation.rs:82-102`, `:516-542`) [V] | `WithAnimation(s)` (`gpui/animation.go:493-505`) [V] | present | — | — |
| **Animation (spring)** | `with_spring` (`animation.rs:124`), `SpringConfig`/`SpringAnimation` (`GF:crates/gpui/src/spring.rs:13,472-538`) [V] | not in gpui core (`gpui/animation.go:12-13`). Base has `MotionSpring` (`base/motion.go:4,23-33`). [V] | partial | Go | S |
| **Drag and drop (in-app)** | `on_drag` (`div.rs:615`), `on_drag_move` (`:360`), `on_drop`, `drag_over`, `can_drop` (`:579`, `:1191-1239`). No drag-leave or drag-end callbacks. [V] | `OnDragAlt`/drag (`gpui/interact.go:1011`), `OnDragMove` (`gpui/interact.go:243`), `OnDrop` (`gpui/div.go:1194`), `CanDropWithin` (`gpui/drop_listener.go:31-33`). [V] | present (equal; neither has drag-leave/drag-end) | Go if wanted | S |
| **File drop (OS)** | `on_file_drop_exit` (`div.rs:338`) [V] | OLE drop target with DragEnter, Over, Leave, Drop (`gpui/dragdrop_windows.go:14,292,567-616`). `FileDropExited` event (`gpui/filedrop.go:58-61`). No div-level `OnFileDropExit` sugar. [V] | present (API sugar partial) | Go | S |
| **Pointer capture** | Implicit `SetCapture` on press (upstream Windows). `capture_mouse_pressure` (`div.rs:194`). No explicit capture API. [V] | implicit `SetCapture` (`gpui/desktop_input_windows.go:26,45`). No pressure. [V] | present (pressure missing) | Go | S |
| **Mouse exit / pressure listeners** | `on_mouse_exit` (`div.rs:319`), `on_mouse_pressure` [V] | "mouse pressure / exit listeners and tooltips are not ported" (`gpui/interact.go:1029-1031`, `:1224-1227`) [V] | missing | Go | S |
| **Tooltips (core)** | `tooltip`, `tooltip_show_delay` (`div.rs:698,735`) [V] | not in gpui core (`gpui/interact.go:1031`). Base component `base/tooltip.go`. [V] | partial | Go | S |
| **Container query** | `GF:crates/gpui/src/elements/container_query.rs:18-25` [V] | none [V] | missing | Go | S |
| **Touch gestures** | `GF:crates/gpui/src/gestures.rs:45-448` (TouchDrag, LongPress, tuning), `on_pinch` (`div.rs:405`), fling velocity (`GF:fast/misc/touch_fling.rs:18`), Direct Manipulation (`crates/gpui_windows/src/direct_manipulation.rs`) [V] | `OnPinch` (`gpui/div.go:1180`), LongPress (`gpui/interact.go:93`), Direct Manipulation (`gpui/directmanipulation_windows.go:9-16,121,247`) [V] | present (TouchDrag and fling not checked) [I] | Go | S |
| **Text: font fallback** | DirectWrite fallback builder (`crates/gpui_windows/src/direct_write.rs:398-451`). Fallbacks keep the requested weight and features (`GF:fast/text.rs:1115-1140`). [V] | `FontFallbacks` passed to native (`gpui/div.go:86-87`; `N:text.rs:255-284`) [V] | present (weight/feature-preserving resolution not checked) [I] | native | S |
| **Text: CJK line breaking** | `allows_break_between` (`GF:fast/line_break.rs:1-18`) [V] | only "CJK may not be space separated" (`gpui/line_wrapper.go:207`) [V] | missing | Go | S |
| **Text: number shaping** | `NumberShaping` (`GF:fast/number_shaping.rs:1-20`, `:102`) [V] | none [V] | missing | Go (needs per-glyph advance queries, which exist natively) | M |
| **Lists / virtualization** | `uniform_list` (`GF:crates/gpui/src/elements/uniform_list.rs:22`), `list` (`list.rs:24`) + row retention (above) [V] | `UniformList` (`gpui/list.go:169`), `List` (`gpui/listelement.go:882`), `base/virtual_list.go`, `base/table.go` [V] | present (perf partial) | — | — |
| **Scroll** | `overflow_*_scroll`, `ScrollHandle`, `scroll_to_bottom` fast path (`GF:fast/layers/scroll_to_bottom.rs`) [V] | `ScrollHandle` (`gpui/div.go:835-837`), `gpui/scroll*.go`, base scrollbar and bounce [V] | present | — | — |
| **Titlebar / custom chrome** | upstream titlebar options (`window.rs:481-494`) [V] | `gpui/titlebar.go`, `gpui/titlebar_windows.go` [V] | present | — | — |
| **Window kinds** | `Normal`, `PopUp`, `AnchoredPopup` (rejected on Windows), `Floating`, `LayerShell`, `Dialog` (`GF:crates/gpui/src/platform.rs:2791-2818`). Windows styles at `crates/gpui_windows/src/window.rs:447-510`. [V] | `WindowKindNormal`, `WindowKindAnchoredPopup` (rejected) only (`gpui/desktop.go:316-330`) [V] | partial | Go (Win32 styles, `EnableWindow` on the owner) | M |
| **Multi-window** | upstream `open_window` (`crates/gpui_windows/src/platform.rs:761`) [V] | `App.OpenWindow` (`gpui/window.go:716`), `App.Windows` (`:594`) [V] | present | — | — |
| **Message prompt** | `PlatformWindow::prompt` → `dialog::show_dialog` (`crates/gpui_windows/src/window.rs:725-737`, `dialog.rs`) [V] | path prompts only (`gpui/dialogs.go:617,633`). No message/button prompt. [V] | missing | Go | S–M |
| **File dialogs** | `prompt_for_paths` / `prompt_for_new_path` (`platform.rs:803,816`) [V] | `gpui/dialogs.go:617,633`, `gpui/dialogs_windows.go` [V] | present | — | — |
| **Menus / jump list** | `set_menus` stores menus only on Windows (`platform.rs:937-947`). Jump list in `destination_list.rs`, `platform.rs:1140`. [V] | `SetMenus` (`gpui/app.go:1374`), `SetDockMenu` (`:1394`), `UpdateJumpList` (`:1406`) [V] | present (equal) | — | — |
| **Tray icon** | none in any crate (grep for `tray`/`StatusItem` finds nothing) [V] | none [V] | equal (neither) | Go | M |
| **System notifications** | toasts (`crates/gpui_windows/src/system_notifications.rs:1`) [V] | `App.ShowSystemNotification` (`gpui/app.go:1334-1339`) [V] | present | — | — |
| **IME** | upstream `InputHandler`, IMM in `events.rs` [I] | `gpui/ime.go`, `gpui/ime_win32.go:40-68,189` [V] | present | — | — |
| **Accessibility** | AccessKit (`GF:crates/gpui/src/gpui.rs:101-103`). Reused subtrees replay their a11y log (`GF:fast/a11y.rs:1-30`). [V] | Native UIA provider (`gpui/a11yprovider.go:3-26`, `N:a11y.rs`), `OnA11yAction` (`gpui/div_a11y.go:338-343`). A reused `CachedView` carries **no** a11y nodes (`gpui/view_cache.go:54-58`). [V] | partial (breaks under caching) | Go | M |
| **Images** | `img`, object fit, fallback/loading, GIF and WebP frames (`GF:crates/gpui/src/elements/img.rs:14,159-173,201,215,254-298`) [V] | `Img` (`gpui/img.go:322-331,460`), formats (`gpui/image.go:31-32,71-84`) [V] | present | — | — |
| **SVG** | `svg()` (`svg.rs:27`) [V] | `Svg()` (`gpui/svgelement.go:212`), `N:svg.rs` [V] | present | — | — |
| **Surface element / native composition** | `surface()` (`GF:crates/gpui/src/elements/surface.rs:35`). `Window::enable_window_composition` / `with_composition_surface` for WebView2 between base and overlays (`GF:fast/composition.rs:1-35,1198,1215`; `GF:wfast/composition.rs:1-15`). [V] | `Scene.InsertSurface` (`gpui/scene.go:404`). No composition tree; `CompositionRecord` is a DWM diagnostic (`gpui/desktop_windows.go:789-792`). [V] | missing (composition) | native (DComp visual tree) + Go | L |
| **Anchored / deferred / canvas** | `anchored.rs:27`, `deferred.rs:7`, `canvas.rs:10` [V] | `gpui/anchored.go:91`, `gpui/deferred.go:274`, `gpui/canvas.go:27` [V] | present | — | — |
| **Inspector / profiler** | `inspector.rs`, `profiler.rs`, `debug_overlay.rs` (profiler feature) [V] | `gpui/inspector.go`, `gpui/debugoverlay.go` [V] | present (no per-view profiler) [I] | Go | S |
| **Upstream-sync discipline** | All fork logic is in `fast/`, with upstream files only calling in (`GF:fast/mod.rs:1-15`, `GF:wfast/mod.rs:1-8`) [V] | n/a | — | — | — |

## How to read the performance gaps

- **gpui-fast saves CPU, not GPU.** Retention (rows 1–3) cuts the cost of *building* a frame. GPU work still covers the full scene on every dirty frame, except where scroll layers composite tiles. gpui-go's frame cost is dominated by the same build work and also pays a Go→native crossing per element, so retention would likely help it at least as much. [I]
- **`CachedView` is the right seam to grow from.** It already has prepaint and paint range reuse, the dispatch subtree copy and scene replay (`gpui/view_cache.go:130-199`). Matching gpui-fast's default behaviour needs three changes:
  - apply it to every `ViewOf`/`AnyView` child automatically;
  - record globals, scroll offsets and hovers as dependencies;
  - carry a11y nodes.

  Retained layout (#3) is the prerequisite for reusing *without* a definite size, because `CachedView` today needs a sized root (`gpui/view_cache.go:403`).
- **Cheapest native wins:** #9 (state dedup) and the global CB skip are small and confined to `N:renderer/draw.rs:485-526`.
