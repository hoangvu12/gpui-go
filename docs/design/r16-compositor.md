# R16 compositor design

Ticket: [49](../../.scratch/browser-core/issues/49-r16-design.md) · Spec: [browser-parity, "Native artifact" R16](../../.scratch/browser-parity/spec.md) · Branch: `core/49-r16-design`
Status: **proposed — for user review. No native code is written by this ticket.**

R16 is the compositor rebuild. It gives the renderer the missing browser paint model: real group opacity, transforms, rounded clipping, masks, clip-path, blend modes, full gradients, drop-shadow, property-only animation frames, and damage-limited redraw with partial present. R15 (ticket 39, in flight on another branch) adds the gradient record and colour-matrix filters; **R16 records extend R15 append-only — R15 records keep their exact meaning and outputs.**

Everything below follows one borrowed model: **Chromium's paint property trees + cc render surfaces + viz damage/present** (Chromium `0d4dcd4ff`, Skia `e71ecd07`, both in `%TEMP%\research-src\`). Citations are `file:line`; every cited Chromium file is in a `BUILD.gn`/`build.gni` sources list compiled on Windows (spot-checked: `cc/BUILD.gn:403-457`, `components/viz/service/BUILD.gn:67,134,489`, `components/viz/common/BUILD.gn:187`, blink `core/paint/build.gni:133`, `core/css/build.gni:172`, `core/paint/build.gni:57`, `core/animation/BUILD.gn:52`, `core/view_transition/build.gni:24`, `platform/BUILD.gn:1148`, `cc/paint/BUILD.gn:33`; `software_output_device_win_swapchain.cc` is inside the `is_win` block `components/viz/service/BUILD.gn:473-495`).

## 1. How it works today (the baseline we change)

- The scene is a flat ordered primitive list; every record carries a **rectangular** content mask (`reference/native/src/scene.rs:210-211`), clipped by four rect distances in the shader (`reference/ce-source/crates/gpui_render/src/shaders/common.rs:419-434`).
- Group machinery exists only for content **filters**: `FilterBoundary` records pair up, the plan assigns the first two nested groups dedicated full-window offscreen targets (`MAX_FILTER_GROUP_DEPTH = 2`, `scene.rs:150`, plan `scene.rs:1770-1790`), deeper groups render **inline — unfiltered** (`renderer/draw.rs:1587-1588`, `scene.rs:1778-1785`). The pool is one full-window texture per depth, pre-allocated (`draw.rs:769-806`).
- Element opacity is multiplied into every primitive's colour (`gpui/scene.go:594-596`), not applied to the group (D9 changes this).
- No transforms except sprites' 2×3 matrix (`scene.rs:536-539`); the shader can already build a transformed textured quad (`shaders/common.rs:385-401` `transformed_rectangle_vertex`).
- Every frame: build a fresh scene, clear the **whole** target (`draw.rs:1289`), redraw everything, `Present(0, 0)` (`draw.rs:2436`) on a 3-buffer flip-sequential chain (`renderer.rs:178,1125-1132`).
- Blur runs full-window half-res passes (`draw.rs:2130-2286`); every batch rebinds all state (`draw.rs:462-527`); paths re-tessellate per build through `path_script` (`SCENE_ABI.md`, `gpui/path.go:12-23`).
- The **pin's shader `ContentMask` already has `fade_out: Edges`** with a linear per-edge ramp (`shaders/common.rs:145-170`) and intersection semantics (`ce-source/crates/gpui/src/window.rs:2105-2170`) — the ABI just never carries it. Edge fades are nearly free.

## 2. The core decision: one effect-group mechanism

Today there is one group kind (filter). R16 replaces the two-target cap with a **pool**, and adds ONE new scene primitive class — the **effect group** — that covers opacity, transforms, rounded clipping, masks, clip-path and blend modes. Chromium's property trees do exactly this: one effect node carries `opacity`, `blend_mode` and mask together, and cc allocates a render surface for whichever reason fires (`cc/trees/property_tree_builder.cc:368-403`: `kRoundedCorner`, `kBlendMode`, `kOpacity` all read the same `num_descendants_that_draw_content` test).

- **Record:** `GpuiGoSceneEffectRecord` (appendix A.1) — open/close markers like filter boundaries, carrying a 2×3 matrix, opacity, blend mode, mask reference, corner radii, and a `node_id` for property nodes. The scene service pairs them with the existing boundary machinery (start inserts above all prior content, end raises the order floor; `scene.rs` doc header). Filter boundaries stay for blur/colour-matrix chains (R15 semantics untouched); both kinds share one group stack.
- **Plan:** `MAX_FILTER_GROUP_DEPTH` is removed. Every matched group gets an isolated target index; a new sanity bound `MAX_SCENE_GROUPS = 1024` returns `ERR_LIMIT` (an ABI bound, not a semantic cap). Plan command kinds grow to 3 = begin effect group, 4 = end effect group, reusing the existing command record (52 bytes, unchanged: `command_kind`, `boundary_index`, `filter_target`, `target_index` already fit).
- **Renderer:** the composite pass generalizes `dx_blur_and_composite` (`draw.rs:2130`) into `composite_group`: optional blur (R15 chain), then one textured composite quad under the group's matrix, opacity, blend mode, mask sample and rrect clip. The pin's `transformed_rectangle_vertex` is the vertex shape.
- **Target pool:** offscreen textures sized to the group's dilated bounds, bucketed (256-px granularity, clamped to window size), allocated lazily, **retained across frames** and dropped when unused or too small — viz's render-pass backing model (`components/viz/service/display/skia_renderer.cc:3571-3610` `UpdateRenderPassTextures`: keep backings still in frame, delete stale/insufficient ones). Pool cap 64 targets / 256 MB per surface; over the cap the group renders **inline** (the pin's existing degradation: children draw, ungrouped) and a debug counter reports it.

## 3. Feature designs

Each entry: native record → renderer change → Go change → bit-exactness → tests. Record field layouts live in the appendix.

### 3.1 Group target pool (no nesting cap)

Native: plan assigns `target_index` per group into the pool; `requirements.isolated_target_count` becomes the pool high-water mark. Renderer: `BlurResources.group_rtvs` (full-window, per-depth) becomes a `GroupTargetPool` (bucketed, free-list). Resize drops and rebuilds the pool (the pin's `recreate_resources` rule, already followed by `DrawTargets`).
Bit-exactness: scenes with ≤ 2 nested blur groups composite through the same 8-bit offscreen path as today — pixels unchanged. Depth > 2 changes **on purpose** (previously unfiltered → now filtered); that divergence is the point of the ticket.
Tests (scenespec): plan pairing at depth 0-6; `isolated_target_count` = max nesting; pool reuse across frames (rendererspec: readback of frame N and N+1 with the same groups); `MAX_SCENE_GROUPS` limit error.

### 3.2 Opacity groups

Native: an effect group with `has_opacity` and no filter composites its subtree once at `opacity` — the isolated-group path with the blur passes skipped (identity filter fast path; today `BlurKernel::for_radius(0)` returns early and nothing composites, so the renderer grows an explicit no-blur composite).
**Single-primitive fold:** a group whose subtree is exactly one primitive is planned inline and the opacity multiplies that primitive's colour — today's per-primitive behaviour, which is *correct* for one primitive. Chromium skips the surface the same way: opacity forces a render surface only when "at least two layers in subtree draw content" (`cc/trees/property_tree_builder.cc:395-403`).
Go: `WithElementOpacity` (`gpui/element.go:1355-1364`, used at `gpui/div.go:1781`) gains a group path: when the element's subtree draws more than one primitive, it brackets the subtree in `BeginEffectGroup`/`EndEffectGroup` instead of multiplying the paint context; the single-primitive case keeps the multiply (the fold above). D9 is the decision.
Bit-exactness: translucent elements with **overlapping** children change (the D9 divergence, fixtures updated); single-child translucent elements are pixel-identical via the fold.
Tests: scenespec (fold rule: one primitive → inline + multiplied; two overlapping → group); harness fixtures (translucent card with overlapping children, matching Chrome — the classic overlap artifact of report 01 §2).

### 3.3 Rounded content mask (rrect in the shader + coverage-mask stack)

Native: every record's content mask grows from 16 to **52 bytes**: bounds + `fade_out` edges + corner radii + smoothing (appendix A.2) — the pin's own shader struct, finally carried. The shader clips by the rounded-corner SDF the quad pipeline already computes (`quads.rs:62-101`, `quad_geometry`'s per-corner radius distance); the mask reuses that distance test. The **innermost** rounded clip is therefore free, inline, in every pipeline.
**Nesting:** a rounded clip that has descendants that clip themselves becomes an effect group with `rounded_clip_only` — the subtree renders to a target and composites clipped by the rrect. This is Chromium's rule: rounded corners need a render surface only when `num_descendants_that_draw_content > 1` and it is not a "fast" corner (`property_tree_builder.cc:368-374`; `cc/trees/draw_property_utils.cc:760-775` carries `mask_filter_info` + `is_fast_rounded_corner`). Edge fades ride the same `fade_out` fields (the pin's `ContentMask.alpha`, `shaders/common.rs:151-168`).
Go: `Style.OverflowMask` returns a mask with radii (border-box inset by border widths always — CSS), and the paint context intersects masks (bounds intersect; fades follow the pin's stronger-edge rule, `window.rs:2131-2170` `ContentMask::intersect`; radii: the innermost non-zero radii win, ancestors' rounded clips become groups).
Bit-exactness: children poking out of rounded corners change **on purpose** (fixture divergence, report 01 §1). Rect-only scenes are unchanged.
Tests: scenespec (mask validation, fold to group when nested, fade propagation); harness fixtures (rounded card with an image child; nested rounded scroller in a rounded card; scroll-edge fade — `OverflowFade` finally paints).

### 3.4 Transformed group composite

Native: effect group with `has_transform` — the subtree renders into a target in its **local** coordinate space (origin at the group bounds), then one composite quad draws under the 2×3 matrix (translate/scale/rotate/skew, transform-origin resolved in Go: `50% 50%` of the border box, the CSS initial — `core/css/css_properties.json5:6747` initial `TransformOrigin(50%, 50%, 0)`, [V] in report 01 §3). Report 01 §3 option (b), which is how cc composites transformed layers anyway.
**Fast path:** translation-only transforms skip the group (the existing element offset) — the same economy Chromium applies (translate is the cheap case in `compositor_animations.cc` transform handling). Linear filtering on the composite; mipmaps stay R17+.
Go: a transform stack in the paint context; hit testing maps through the inverse matrix (ticket 56, Go-side only — the native record doesn't participate).
Bit-exactness: no existing fixture uses transforms (the feature doesn't exist today), so nothing changes.
Tests: scenespec (record validation, local-space rendering, matrix composite bounds); rendererspec (readback of a scaled/rotated subtree: pixel compare against a CPU-computed reference at the same sampling); harness fixtures (box + pixel fixtures per transform, ticket 56's event-log fixtures for hit testing).

### 3.5 Mask composite (edge fades, gradient and image masks, alpha and luminance)

Native: an effect group with `has_mask` composites as `dst_in` against a mask source. Chromium: the mask is an effect node with `SkBlendMode::kDstIn` (`paint_property_tree_builder.cc:2378`), masked content is clipped to the mask's bounding box, and an invalid/unloaded mask is transparent black — the element is hidden (`:2230-2245`, `:2265-2286`). Luminance mode wraps the layer in a luminance-to-alpha filter (`core/paint/svg_mask_painter.cc:70-118`, `ColorFilter::MakeLuma()`).
Mask sources (appendix A.3/A.4): **gradient** (a gradient-table entry — edge fades are a linear alpha ramp), **image** (an atlas tile, alpha or luminance), **coverage** (an A8 texture — see 3.6). The mask rect is the group bounds. The composite shader samples the source, converts luminance→alpha when `mask_mode = 1`, multiplies the group colour, and composites with `dst_in` semantics over the backdrop (the D3D11 path: multiply alpha in the shader; premultiplied everywhere as the pin already is).
Go: `mask-image` styles paint the mask subtree description; edge fades fall out of 3.3's `fade_out` for the pure four-edge case and of a gradient mask for asymmetric fades.
Bit-exactness: new feature; nothing changes.
Tests: scenespec (mask record validation, dump); rendererspec (alpha vs luminance readback); harness fixtures (gradient mask fade, masked icon, luminance mask).

### 3.6 clip-path coverage

Native: the coverage mask — Go tessellates the path (existing `PathBuilder` → `path_script`, `gpui/path.go`), the scene stores it in a scene-level **coverage array**, and the renderer rasterizes the filled path into an A8 target once per frame (the existing MSAA path pipeline rendered to a single-channel target). The effect group references it by index. Chromium's fast path is a real clip; the mask fallback is the general case (`core/paint/clip_path_clipper.cc:552` path clip; `paint_property_tree_builder.cc:2874` mask fallback — both cited in report 01 §9).
Go: `clip-path` styles build the path in element-local coordinates and reference the coverage entry; hit testing is point-in-path (ticket 57).
Bit-exactness: new feature.
Tests: scenespec (coverage entry, reference); rendererspec (rasterized coverage readback); harness fixtures (circle/polygon clip-path).

### 3.7 Blend modes, including plus-lighter

Native: an effect group with `has_blend` composites with the mode instead of source-over. D3D11 has no programmable blending, so the composite samples a **backdrop copy** (the offscreen scene texture the backdrop filter already uses — blend groups force the offscreen route, `use_offscreen`) and blends in the shader, the same trick as `dx_blur_and_composite`'s backdrop. Chromium forces a render surface for any non-src-over blend (`property_tree_builder.cc:376-384`) and viz applies it on the render-pass draw quad's paint (`components/viz/service/display/skia_renderer.cc:1854` `paint->setBlendMode(params->blend_mode)`).
Modes: the CSS separable set plus the non-separable hue/saturation/color/luminosity, and **plus-lighter**: premultiplied `clamp(s + d)` (Skia `kPlus`, "clamp(s+d)", `src/core/SkBlendMode.cpp:39`), the mode Chromium's own view transitions use with an isolating group (`core/view_transition/view_transition_style_builder.cc:73-81`). `isolation` falls out of any group for free (the group's parent is the blend boundary).
Go: `mix-blend-mode` styles emit the group; `isolation: isolate` emits an empty effect group.
Bit-exactness: new feature.
Tests: scenespec (mode tags validate); rendererspec (each mode's blend readback against CPU reference); harness fixtures (multiply/screen/plus-lighter pairs).

### 3.8 n-stop, radial, conic and repeating gradients

Native: a scene-level **gradient table** (appendix A.3): kind (linear/radial/conic), geometry, tile mode (pad/repeat/reflect), colour space, and up to 32 stops. Stops are pre-resolved in Go per CSS color-stop fixup, then baked into a 1024×1 **premultiplied sRGB ramp row** per gradient; the ramp texture is uploaded with the scene buffers and sampled with linear filtering (hardware interpolation of premultiplied sRGB bytes *is* CSS's interpolation rule — `platform/graphics/gradient.cc:247` sRGB legacy + `:287` premultiplied, both [V] in report 01 §6). The shader computes `t` per kind: linear along the CSS gradient line (endpoint math in Go, Chromium's `EndPointsFromAngle`, `core/css/css_gradient_value.cc:1284-1350`), radial by radius, conic by angle; repeating wraps `t`.
**R15 compatibility:** the 2-stop linear sRGB path keeps R15's record and shader *unchanged* (bit-exact with R15 fixtures). The table path is used for > 2 stops, radial, conic, repeating, and any non-sRGB interpolation. Quads reference a table entry through the background tag (R15's tags stay; a new tag points at the gradient index).
Go: `Background` grows the gradient kinds; the class layer (`bg-linear/radial/conic`, `from/via/to`) un-blocks once R15 + R16 land (tickets 40/57).
Bit-exactness: R15 2-stop fixtures unchanged; new fixtures only.
Tests: scenespec (table insert/validate/dump); rendererspec (ramp upload + each kind's readback); harness fixtures (n-stop, radial, conic, repeating vs Chrome).

### 3.9 drop-shadow filter

Native: a new **filter op** in the chain (appendix A.5): offset, blur radius, colour. Render order per CSS: the shadow is the group's alpha, blurred, tinted, offset, composited *under* the group content (Chromium builds `FEDropShadow(blur, offset, color)` — `core/paint/filter_effect_builder.cc:293-301`). Implementation: render the group, extract alpha in a first composite (colour = white, alpha = group alpha), blur it (existing passes), tint and offset, draw beneath the content quad.
Record compatibility: R15's filter record fields are untouched; R16 appends an op-chain reference (`op_chain_index`, `op_chain_count`, both 0 = R15 legacy semantics). Exact offsets finalize against the merged R15 record; the rule is append-only.
Go: `filter: drop-shadow(...)` appends the op (ticket 57 exposes the style).
Bit-exactness: R15 records with no op chain are unchanged.
Tests: scenespec (op chain validation); rendererspec (drop-shadow readback); harness fixture (drop-shadow on a rounded, translucent element).

### 3.10 Scene property nodes (transform and opacity) — property-only frames

Native: an effect group with `property_node` and a stable `node_id` can be updated **after** the scene is finished: `scene_update_property(handle, node_id, kind, values…)` mutates the retained scene's effect records in place — no re-sort, no re-plan (orders don't change: paint order is tree order, and a transform/opacity change never reorders, the same invariant cc relies on when ticking compositor animations) — and returns the group's old and new device-space bounds (mapped by old/new matrix) for damage. The Go frame loop then re-draws the **same scene handle** through the damage entry (3.11). This is Chromium's compositor-only frame: only `opacity`, `transform`/`translate`/`rotate`/`scale`, `filter` and `backdrop-filter` are compositable properties (`core/animation/compositor_animations.cc:396-452`); everything else falls back to a full frame.
Go (ticket 60, after R16): the frame loop takes a fast path — a transform/opacity transition tick updates the node in the window's last scene, presents with damage = old ∪ new mapped bounds (dilated by any enclosing blur), and skips render/layout/paint entirely. Elements opt in by animating only those properties (the `will-change` analogue).
Bit-exactness: none (no consumer yet); the values the node carries are the same ones a full frame would emit.
Tests: scenespec (update mutates records, bounds out, plan stable, stale node_id error); rendererspec (draw after update: readback differs only inside old ∪ new bounds); the zero-view-renders-per-frame count is ticket 60's Go test.

### 3.11 Retained frame target, scissored damage redraw, dirty-rect present, blur damage expansion

Native (renderer service v5, new entry — appendix B):
- The offscreen scene texture becomes the **retained frame target**: it persists per surface across frames and always holds the last presented frame (it exists today only when filters route through it, `draw.rs:1321-1370`).
- `renderer_surface_draw_scene_damage(surface, scene, appearance, atlas, damage_rects, count, flags, out_submission)`: intersect nothing, trust nothing — the entry draws the scene **scissored** to the union of the damage rects (clamped to the viewport), then copies each damage rect from the retained target into the back buffer (`CopySubresourceRegion`) and calls **`Present1`** with those dirty rects. Full-present fallback when damage covers ≥ the threshold (start: half the window area), when the surface was just created/resized/recovered, or when `Present1` fails. The existing `surface_draw_scene` remains = full damage.
- **Why the copy:** DXGI requires the app to have written every pixel in each dirty rect and guarantees nothing outside them ("An application must update every single pixel in each rectangle", DXGI present parameters docs, cited in report 02 §1). The retained target makes us independent of buffer age (3 buffers rotate: `renderer.rs:178`). This is Chromium's own Windows pattern: copy the damage into the swap-chain buffer, then `Present1` with one dirty rect (`components/viz/service/display_embedder/software_output_device_win_swapchain.cc:241-273`, flip-sequential, `is_win`-gated `BUILD.gn:489`).
- **DComp mode caveat:** Chromium's GPU DComp path ignores update rects ("the SharedImage backing already knows the area to be swapped", `components/viz/service/display_embedder/skia_output_device_dcomp.cc:213-219`). A composition swap chain still accepts `Present1`; we verify with a readback test and fall back to full present in mode 0 if it misbehaves (open question Q5).
- **Blur damage expansion:** damage that intersects a group with a pixel-moving filter (blur, drop-shadow) or a mask/transform expands to that group's dilated bounds — viz's rule (root damage ∪ frame-buffer damage ∪ the expansion, `components/viz/service/display/direct_renderer.cc:1041-1042`; the expansion itself, `direct_renderer.cc:964-993`, maps rects through the filter, `components/viz/common/viz_utils.cc:78-92`), with blur dilation = **3σ** (`cc/paint/filter_operation.cc:333-341`, `MapStdDeviation` returns `sigma * 3`; the pin already uses 3σ for composite dilation, `draw.rs:2237-2243`).
- Blur passes become **region-limited**: today they run full-window half-res (`draw.rs:2130-2286`); R16 binds their viewport to the dilated composite region (reading the full source, writing only the region) — same math in-region, big cost cut.
Go: damage *computation* is ticket 58 (reused view ranges at the same bounds and mask are undamaged — Chrome's chunk rule, `platform/graphics/paint/raster_invalidator.cc:242-249`, cited [V] in report 02 §1); R16 native only receives rects. The paint-flashing overlay (58) is the visual check.
Bit-exactness: a full-damage draw through the retained target must be readback-identical to the current direct path for group-free scenes (spec test); damaged-vs-full-redraw equivalence is a required test (below).
Tests (rendererspec): (1) full damage ≡ legacy path pixels; (2) damaged redraw ≡ full redraw, bit-exact, over a scripted sequence (background change, scroll, transform tick); (3) pixels outside damage unchanged; (4) `Present1` accepted in both surface modes (or the fallback fires); (5) blur-group damage expansion bounds.

### 3.12 Redundant state-bind removal and a path tessellation cache

- **State binds:** `draw_with_variant` rebinds topology, viewport, shaders, cbuffers, blend state and samplers on every batch (`draw.rs:462-527`). Add a dirty-state tracker in the draw global: bind only what changed since the previous command. Pure renderer-internal change; zero pixel impact; observable only through the debug stats counter.
- **Path tessellation cache:** `path_script` re-runs lyon for the same command stream every frame. Add an LRU cache in the scene service keyed by the exact command+pool byte hash (size 128, keyed also on fill tolerance from the style): identical input → the cached vertex array, skipping tessellation and the copy. Deterministic input means deterministic output — bit-exact by construction.
Tests: rendererspec (state-bind counter drops, pixels unchanged); scenespec (tess cache hit/miss, identical vertices, LRU eviction).

## 4. Order of work: tickets 50-53 on one integration branch

One branch off main (created after R15 merges), e.g. `core/r16-integration`. Green is promised only at 54; each ticket's own native spec tests must pass as it lands.

| Order | Ticket | Lands | Why this position |
|---|---|---|---|
| 1 | **50** groups, opacity, rounded mask | scene v5 effect record + plan pairing + target pool; renderer `composite_group` + identity filter; the 52-byte content mask everywhere; the single-primitive fold | Everything else rides the group mechanism and the mask record. |
| 2 | **51** transforms, property nodes | matrix on the composite; `scene_update_property` | Extends 50's composite pass; small delta once 50 is in. |
| 3 | **52** masks, clip-path, blend, gradients, drop-shadow | mask/gradient/coverage/op tables; blend in the composite; drop-shadow op | Riders on the effect group; independent of each other, can interleave. |
| 4 | **53** retained frame, damage, present, caching | retained target; `draw_scene_damage`; `Present1`; region-limited blur; state binds; tess cache | Last because it restructures `render_scene` and the present path — do it once, against the final composite shape. The state-bind and tess-cache items are independent and can land any time inside 53. |

Alternative considered: pulling 53 forward to de-risk the present path early. Rejected: 53's retained-target and region-limited-blur work would be done twice as 50/52 restructure the composite passes it wraps.

Ticket 54 then integrates, rebuilds rev 16 (user approval required), re-runs the measure tool, and updates the pins and evidence in every place rev 14 touched.

## 5. Risks

1. **GPU memory (target pool).** Bucketed group targets can add up on deep nesting. Mitigations: bounds-sized (not full-window), pool cap with inline fallback (never a failed frame), pool stats in the debug counters, measured before/after at 54. Chromium's retained-backing model (`skia_renderer.cc:3571`) shows the reuse rate is high.
2. **Damage correctness.** Missed damage = stale pixels, an invisible bug class. Mitigations: damage never shrinks below the group expansions (native-side, conservative); the damaged-vs-full equivalence spec test over scripted sequences; paint flashing (ticket 58) for the Go-side rule; any plan feature not yet damage-aware forces full damage (a per-requirements flag, like `uses_offscreen_target` today).
3. **Region-limited blur.** Reading the full source but writing a region must match full blur in-region (gaussian is local; edge handling mirrors the source bounds). Covered by the equivalence test with a blur group straddling the damage edge.
4. **Property-node vs rebuilt scenes.** A normal frame rebuilds the scene (cached views replay ranges); the transition's current value must be re-inserted at build time so a fallback full frame shows the same value as the property-only frames. Go-side contract, documented on ticket 60.
5. **8-bit group quantization.** Compositing through a BGRA8 intermediate quantizes opacity groups (as the pin's filter path already does). Folded single-primitive groups avoid it; nested groups inherit it. Recorded as a known limit, matching the pin.
6. **ABI growth / R15 rebase.** Scene service v5 and renderer v5 grow tables and records; every record keeps `record_size` self-checks (the ABI's existing mismatch catcher). R15 fields are append-only. The integration branch is cut after R15 merges to avoid a parallel-ABI fork.
7. **`Present1` on a composition swap chain** (mode 0). Verified by test; full-present fallback if it fails (open question Q5).

## 6. Measured before and after (recorded at ticket 54, `evidence/perf/`)

Baseline first (before the integration branch is cut), then after rev 16, same harness:

- Frame time: the gpui-perf pages (1,000 divs; the text page) — the existing baseline numbers are 8.5 ms / 31 ms (report 02 §2).
- Property-only frame: cost of a transform/opacity transition tick through `scene_update_property` + `draw_scene_damage` (target: no render/layout/paint; frame cost ≪ baseline frame).
- Damage: rect count and area fraction for the hover-change and scroll cases (ticket 58's tests pin the bounds).
- GPU: target pool high-water mark (count + bytes) and total committed textures, from the new debug stats entry.
- Renderer internals: state-bind count per frame, tessellation cache hit rate — the debug counters.
- Present: full vs partial present counts and CPU time per present.

## 7. Proposed changes to tickets 50-53 (listed here; not edited into the tickets)

- **50:** add the effect-group record and plan pairing (one mechanism, not a filter-only extension); the 52-byte content mask with `fade_out` (edge falls out here); the single-primitive opacity fold; the target pool with cap + inline fallback; an explicit "R15's gradient record and 2-stop path stay bit-exact" gate.
- **51:** add `scene_update_property` and the retained-scene re-present (the native half of property-only frames); note that the Go half (frame-loop fast path, zero view renders) stays in ticket 60, which should be re-pointed at "needs 51+53, not just 54".
- **52:** add the filter-op chain record with the R15 append-only compatibility rule; the gradient stop table + ramp texture (R15 2-stop path untouched); the coverage-mask array; "blend groups force the offscreen route".
- **53:** add the damaged-vs-full-redraw bit-exact equivalence test and the full-damage ≡ legacy-path test; the Present1-in-both-modes verification with the fallback rule; the full-present damage threshold; state-bind and tess-cache items marked independent (can land first within the ticket).

## 8. Open questions for the user

1. **Target pool budget.** 64 targets / 256 MB per surface, degrading to inline (ungrouped, unfiltered) beyond — is silent degradation acceptable, or should overflow be a visible debug-state/error?
2. **Property-only frames opt-in.** Should animated transform/opacity elements declare it (a `will-change`-like style, cheaper to detect), or should the core auto-detect (transition-property ∈ {transform, opacity} is enough)? Proposal: auto-detect from the transition property, no new style.
3. **Full-present threshold.** Present full when damage ≥ 50% of the window area — OK as the starting constant?
4. **Gradient stop cap.** 32 stops per gradient (more stops resolved down in Go with a warning) — OK?
5. **`Present1` in DComp mode.** Keep dirty-rect partial present for composition swap chains after the verification test, or restrict partial present to the hwnd mode and always present full in mode 0 (Chromium's DComp path ignores update rects)?
6. **Blend-group cost cap.** Blending needs a backdrop copy per group; above some size it is a full-window pass. Cap it (e.g. blend groups > 25% of window area render via the full-window route) or leave unbounded?

---

## Appendix A: record layouts

All `#[repr(C)]`, u32 words, f32 values as IEEE-754 bits, alignment 4, every record ends with a `record_size` self-check (the ABI's existing convention). Offsets in words. Scene service version bumps to **5**; renderer service to **5**; native revision to **16** at ticket 54.

### A.1 `GpuiGoSceneEffectRecord` — the effect group marker (new primitive class 10)

| Field | Offset | Size | Meaning |
|---|---|---|---|
| `order` | 0 | 1 | 0 on insert; kernel-assigned |
| `flags` | 4 | 1 | bit 0 `is_start`; 1 `has_transform`; 2 `has_opacity`; 3 `has_blend`; 4 `has_mask`; 5 `property_node`; 6 `rounded_clip_only`; 7 `clip_to_bounds` (composite clips to the group's rounded bounds — masks and rounded clips; 0 lets content bleed, like content filters today) |
| `bounds` | 8 | 4 | group bounds, device px |
| `content_mask` | 24 | 4 | the group's own clip, as `GpuiGoSceneBounds` (a group clip is the rrect from `corner_radii`/`clip_to_bounds`; per-edge fades on a group are expressed as a gradient mask, 3.5) |
| `transform` | 40 | 6 | `[m00, m01, m10, m11, tx, ty]` f32, row-major (the sprite matrix shape, `scene.rs:536-539`) |
| `opacity` | 64 | 1 | f32 |
| `blend_mode` | 68 | 1 | tag: 0 src-over, 1 multiply, 2 screen, 3 overlay, 4 darken, 5 lighten, 6 color-dodge, 7 color-burn, 8 hard-light, 9 soft-light, 10 difference, 11 exclusion, 12 hue, 13 saturation, 14 color, 15 luminosity, 16 plus-lighter |
| `mask_kind` | 72 | 1 | 0 none; 1 gradient; 2 atlas tile; 3 coverage |
| `mask_reference` | 76 | 1 | gradient table index / atlas tile id / coverage index |
| `mask_mode` | 80 | 1 | 0 alpha; 1 luminance |
| `node_id` | 84 | 1 | property node id; 0 = none |
| `corner_radii` | 88 | 4 | f32 ×4 (TL, TR, BR, BL) |
| `corner_smoothing` | 104 | 1 | f32 |
| `reserved` | 108 | 1 | 0 |
| `record_size` | 112 | 1 | **size 116 bytes** |

Validation: start/end pairing follows the boundary rules (start inserts above all prior content; end raises the floor; unmatched ends render inline). `has_transform` requires an invertible 2×2. `property_node` requires `node_id ≠ 0` and one of `has_transform`/`has_opacity`.

### A.2 The extended content mask (grows in every record that has one)

The pin's shader struct (`shaders/common.rs:145-148`), finally carried by the ABI, plus radii:

| Field | Size | Meaning |
|---|---|---|
| `bounds` | 4 | x, y, w, h f32 (device px) |
| `fade_out` | 4 | left, top, right, bottom f32 — the pin's linear edge ramp; 0 = opaque edge |
| `corner_radii` | 4 | f32 ×4 |
| `corner_smoothing` | 1 | f32 |

Mask = 13 words = **52 bytes** (from 16). Grows on: quad, shadow, underline, filter, surface, both sprite records, path (132→168, 120→156, 72→108, 88→124, 56→92, 120→156, 100→136, 64→100). Old/new mismatch is caught by `record_size`. Interaction: rect intersection is Go's; the innermost radii apply inline; ancestor rounded clips become groups (3.3).

### A.3 `GpuiGoSceneGradientRecord` + stop rows (scene-level table)

| Field | Offset | Size | Meaning |
|---|---|---|---|
| `kind` | 0 | 1 | 0 linear; 1 radial; 2 conic |
| `tile` | 4 | 1 | 0 pad; 1 repeat; 2 reflect |
| `color_space` | 8 | 1 | 0 sRGB (CSS default); 1 Oklab (reserved) |
| `geometry` | 12 | 8 | linear: `[x0, y0, x1, y1]`; radial: `[cx, cy, r0, r1, aspect]`; conic: `[cx, cy, start_angle, rotation]`; rest 0 |
| `stop_count` | 44 | 1 | 1..=32 |
| `stop_row` | 48 | 1 | assigned row index in the ramp texture |
| `record_size` | 52 | 1 | **size 56 bytes** |

Stops are inserted as `position f32 + RGBA8 straight` triples with the record; the service bakes each gradient into one 1024×1 premultiplied sRGB ramp row (CSS fixup applied in Go before insert). The renderer uploads the rows with the scene instance buffers.

### A.4 Coverage mask entries

`GpuiGoSceneCoverageRecord`: `path_index` @0 (an index into the scene's path records), `bounds` @4 (4 words, the path's device bounds), `record_size` @20 — **size 24 bytes**. The renderer fills the referenced path into an A8 target when the frame references it.

### A.5 Filter op chain (drop-shadow), R15-append

`scene_insert_filter_op_chain(ops, count) -> chain_index`; the boundary record gains two appended words: `op_chain_index`, `op_chain_count` (0/0 = R15 legacy chain, bit-exact).

`GpuiGoSceneFilterOpRecord`:

| Field | Offset | Size | Meaning |
|---|---|---|---|
| `kind` | 0 | 1 | 2 = drop-shadow (0 blur, 1 colour-matrix are R15's, re-stated here for reference only) |
| `offset` | 4 | 2 | dx, dy f32 (device px) |
| `blur_radius` | 12 | 1 | f32 — the CSS blur radius; σ = radius/2, the shadow rule (Chromium `shadow_data.h:76-88` `BlurAsSigma`, [V] report 01 §4), applied in Go |
| `color` | 16 | 5 | `GpuiGoSceneColor` |
| `record_size` | 36 | 1 | **size 40 bytes** |

### A.6 Plan command kinds (unchanged 52-byte command record)

`command_kind`: 0 batch, 1 begin filter, 2 end filter, **3 begin effect group, 4 end effect group**. Effect commands carry the effect record index in `boundary_index`/`closing_boundary_index` and the pool target in `filter_target`/`target_index`.

### A.7 `scene_update_property`

`i32 scene_update_property(u64 scene, u32 node_id, u32 kind /* 0 matrix, 1 opacity */, const u32* values, u32 value_count, GpuiGoSceneBounds* out_old, GpuiGoSceneBounds* out_new)`

Mutates every finished effect record with `node_id`; recomputes nothing else; returns the group's device bounds mapped by the old and new value (matrix-mapped for kind 0, bounds unchanged for kind 1). `ERR_NOT_FINISHED` before finish; `ERR_BAD_VALUE` for a stale id.

## Appendix B: ABI entries (scene v5, renderer v5)

Scene service table (from 27 pointers): `scene_insert_effect`, `scene_effect_dump`, `scene_insert_gradient`, `scene_gradient_dump`, `scene_insert_coverage`, `scene_insert_filter_op_chain`, `scene_update_property` — 34 pointers, and the `insert_batch` op stream gains kinds for each (12/13 effect start/end, 14 gradient, 15 coverage, 16 op chain).

Renderer service table: `renderer_surface_draw_scene_damage(u64 surface, u64 scene, u32 appearance, u64 atlas, const GpuiGoRectRecord* rects, u32 count, u32 flags /* 0 partial, 1 force full */, u64* out_submission)`, with `GpuiGoRectRecord = {x, y, w, h} i32 = 16 bytes`; plus a `renderer_surface_debug_stats` entry reporting per-surface pool bytes/targets, state-bind count, tess-cache hits, damage fraction (the measurement vehicle for §6).

Capabilities: scene bit 3/7 semantics extend (no new bit needed — service versions gate it); the bootstrap `native_revision` becomes 16 at ticket 54.

---

*Sources: Chromium `0d4dcd4ff` and Skia `e71ecd07` (`%TEMP%\research-src\`), the pinned CE source in `reference/ce-source/` (`254b5dbd`), the native scene/renderer in `reference/native/src/`, the Go scene in `gpui/`, and reports [01](../../research/browser-parity/01-visual-primitives.md) and [02](../../research/browser-parity/02-motion-and-compositing.md). Chromium/Skia citations were re-verified at the cited lines on this checkout unless marked as report-verified; all cited files are in Windows-compiled `BUILD.gn` sources lists (§ intro).*
